package directstream

import (
	"context"
	"seanime/internal/api/anilist"
	"seanime/internal/api/metadata_provider"
	"seanime/internal/continuity"
	discordrpc_presence "seanime/internal/discordrpc/presence"
	"seanime/internal/events"
	"seanime/internal/mkvparser"
	"seanime/internal/nativeplayer"
	"seanime/internal/platforms/platform"
	"seanime/internal/util"
	"seanime/internal/util/result"
	"seanime/internal/videocore"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

// Manager handles direct stream playback and progress tracking for the built-in video player.
// It is similar to playbackmanager.PlaybackManager.
type (
	Manager struct {
		Logger *zerolog.Logger

		// ------------ Modules ------------- //

		wsEventManager             events.WSEventManagerInterface
		continuityManager          *continuity.Manager
		metadataProviderRef        *util.Ref[metadata_provider.Provider]
		discordPresence            *discordrpc_presence.Presence
		platformRef                *util.Ref[platform.Platform]
		refreshAnimeCollectionFunc func()                                      // This function is called to refresh the AniList collection
		hmacTokenFunc              func(endpoint string, symbol string) string // Generates HMAC token query param for stream URLs

		nativePlayer *nativeplayer.NativePlayer

		videoCore           *videocore.VideoCore
		videoCoreSubscriber *videocore.Subscriber

		// --------- Playback Context -------- //

		playbackMu sync.Mutex

		// ---------- Playback State ---------- //

		streams *result.Map[string, Stream] // Active streams keyed by clientId

		// settings is read on every playback event, so it uses atomic.Pointer for lock-free reads.
		settings atomic.Pointer[Settings]

		isOfflineRef *util.Ref[bool]
		// animeCollection is read on every playback event and from background goroutines.
		// It uses atomic.Pointer for lock-free reads. nil means absent.
		animeCollection atomic.Pointer[anilist.AnimeCollection]
		animeCache      *result.Cache[int, *anilist.BaseAnime]

		parserCache        *result.Cache[string, *mkvparser.MetadataParser]
		transcodeRequester TranscodeRequester
	}

	Settings struct {
		AutoPlayNextEpisode bool
		AutoUpdateProgress  bool
	}

	// TranscodeRequester is an interface for requesting transcode streams.
	// This avoids a direct dependency on the mediastream package.
	TranscodeRequester interface {
		RequestTranscodeStream(filepath string, clientId string) error
		PreloadFirstSegments(filepath string, clientId string)
		NotifyDownloadComplete(remotePath string, localPath string, expectedSize int64)
		GetTranscodeDir() string
		ShutdownTranscodeStream(clientId string)
	}

	NewManagerOptions struct {
		Logger                     *zerolog.Logger
		WSEventManager             events.WSEventManagerInterface
		MetadataProviderRef        *util.Ref[metadata_provider.Provider]
		ContinuityManager          *continuity.Manager
		DiscordPresence            *discordrpc_presence.Presence
		PlatformRef                *util.Ref[platform.Platform]
		RefreshAnimeCollectionFunc func()
		IsOfflineRef               *util.Ref[bool]
		NativePlayer               *nativeplayer.NativePlayer
		VideoCore                  *videocore.VideoCore
		HMACTokenFunc              func(endpoint string, symbol string) string
		TranscodeRequester         TranscodeRequester // Optional: used for debrid stream audio transcoding
	}
)

func NewManager(options NewManagerOptions) *Manager {
	ret := &Manager{
		Logger:                     options.Logger,
		wsEventManager:             options.WSEventManager,
		metadataProviderRef:        options.MetadataProviderRef,
		continuityManager:          options.ContinuityManager,
		discordPresence:            options.DiscordPresence,
		platformRef:                options.PlatformRef,
		refreshAnimeCollectionFunc: options.RefreshAnimeCollectionFunc,
		hmacTokenFunc:              options.HMACTokenFunc,
		isOfflineRef:               options.IsOfflineRef,
		streams:                    result.NewMap[string, Stream](),
		nativePlayer:               options.NativePlayer,
		parserCache:                result.NewCache[string, *mkvparser.MetadataParser](),
		animeCache:                 result.NewCache[int, *anilist.BaseAnime](),
		videoCore:                  options.VideoCore,
		transcodeRequester:         options.TranscodeRequester,
	}
	ret.settings.Store(&Settings{})
	ret.videoCoreSubscriber = ret.videoCore.Subscribe("directstream")
	ret.listenToPlayerEvents()

	return ret
}

// TerminateAllStreams terminates every active stream in this manager and clears the streams map.
// Safe to call on session eviction; does not touch VideoCore or NativePlayer.
// Holds playbackMu to serialize against concurrent PlayLocalFile/PlayDebridStream which
// load new streams into the map.
func (m *Manager) TerminateAllStreams() {
	m.playbackMu.Lock()
	defer m.playbackMu.Unlock()
	m.streams.Range(func(clientId string, s Stream) bool {
		s.Terminate()
		m.streams.Delete(clientId)
		return true
	})
}

// Shutdown releases per-manager resources on session eviction. Terminates any
// active streams and unsubscribes from VideoCore so the listenToPlayerEvents
// goroutine exits cleanly (range loop terminates when the channel is closed).
// Safe to call multiple times.
func (m *Manager) Shutdown() {
	m.TerminateAllStreams()
	if m.videoCore != nil && m.videoCoreSubscriber != nil {
		m.videoCore.Unsubscribe(m.videoCoreSubscriber.GetId())
	}
}

func (m *Manager) SetAnimeCollection(ac *anilist.AnimeCollection) {
	m.animeCollection.Store(ac)
}

func (m *Manager) SetSettings(s *Settings) {
	m.settings.Store(s)
}

// GetHMACTokenQueryParam returns an HMAC token query param for the given endpoint, or empty string if not available.
func (m *Manager) GetHMACTokenQueryParam(endpoint string, symbol string) string {
	if m.hmacTokenFunc != nil {
		return m.hmacTokenFunc(endpoint, symbol)
	}
	return ""
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

func (m *Manager) getAnime(ctx context.Context, mediaId int) (*anilist.BaseAnime, error) {
	media, ok := m.animeCache.Get(mediaId)
	if ok {
		return media, nil
	}

	// Find in anime collection
	if animeCollection := m.animeCollection.Load(); animeCollection != nil {
		media, ok := animeCollection.FindAnime(mediaId)
		if ok {
			return media, nil
		}
	}

	// Find in platform
	media, err := m.platformRef.Get().GetAnime(ctx, mediaId)
	if err != nil {
		return nil, err
	}

	// Cache
	m.animeCache.SetT(mediaId, media, 1*time.Hour)

	return media, nil
}
