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
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/mo"
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

		settings *Settings

		isOfflineRef    *util.Ref[bool]
		animeCollection mo.Option[*anilist.AnimeCollection]
		animeCache      *result.Cache[int, *anilist.BaseAnime]

		parserCache        *result.Cache[string, *mkvparser.MetadataParser]
		transcodeRequester TranscodeRequester
		//playbackStatusSubscribers *result.Map[string, *PlaybackStatusSubscriber]
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
		videoCore:                  options.VideoCore,
		transcodeRequester:         options.TranscodeRequester,
	}
	ret.videoCoreSubscriber = ret.videoCore.Subscribe("directstream")
	ret.listenToPlayerEvents()

	return ret
}

func (m *Manager) SetAnimeCollection(ac *anilist.AnimeCollection) {
	m.animeCollection = mo.Some(ac)
}

func (m *Manager) SetSettings(s *Settings) {
	m.settings = s
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
	animeCollection, ok := m.animeCollection.Get()
	if ok {
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
