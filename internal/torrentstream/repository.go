package torrentstream

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"seanime/internal/api/anilist"
	"seanime/internal/api/metadata_provider"
	"seanime/internal/database/db"
	"seanime/internal/database/models"
	"seanime/internal/directstream"
	"seanime/internal/events"
	hibiketorrent "seanime/internal/extension/hibike/torrent"
	"seanime/internal/library/anime"
	"seanime/internal/library/playbackmanager"
	"seanime/internal/mediaplayers/mediaplayer"
	"seanime/internal/nativeplayer"
	"seanime/internal/platforms/platform"
	"seanime/internal/torrents/autoselect"
	"seanime/internal/torrents/torrent"
	"seanime/internal/util"
	"seanime/internal/util/result"
	"sync"
	"sync/atomic"

	itorrent "github.com/anacrolix/torrent"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/samber/mo"
)

type (
	Repository struct {
		client   *Client
		handler  *handler
		playback playback
		settings atomic.Pointer[Settings] // nil until set; replaced, never mutated (use getSettings)

		selectionHistoryMap *result.Map[int, *hibiketorrent.AnimeTorrent] // Key: AniList media ID

		autoSelect *autoselect.AutoSelect

		// Injected dependencies
		torrentRepository               *torrent.Repository
		baseAnimeCache                  *anilist.BaseAnimeCache
		completeAnimeCache              *anilist.CompleteAnimeCache
		platformRef                     *util.Ref[platform.Platform]
		wsEventManager                  events.WSEventManagerInterface
		metadataProviderRef             *util.Ref[metadata_provider.Provider]
		playbackManager                 *playbackmanager.PlaybackManager
		mediaPlayerRepository           *mediaplayer.Repository // guarded by playback.listenerMu; read via mediaPlayer()
		mediaPlayerSubscriberID         string                  // unique per repository, since all share one media player
		directStreamManager             *directstream.Manager
		nativePlayer                    *nativeplayer.NativePlayer
		logger                          *zerolog.Logger
		db                              *db.Database

		onEpisodeCollectionChanged func(ec *anime.EpisodeCollection)

		// previousStreamOptionsMu guards previousStreamOptions. StartStream writes it and
		// GetPreviousStreamOptions/GetActiveStreamInfo read it, from different goroutines (the
		// latter now polled every few seconds by the admin activity endpoint) - every access must
		// go through the getter/setter below rather than touching the field directly.
		previousStreamOptionsMu sync.RWMutex
		previousStreamOptions   mo.Option[*StartStreamOptions]
		// autoSelectedRelease is the release name auto-select picked for the current stream ("" if
		// it was picked manually), guarded by previousStreamOptionsMu.
		autoSelectedRelease string
		preloadedStream     mo.Option[*preloadedStream]
		// preloadedStreamMu guards preloadedStream. PreloadStream/StartStream/StopStream/
		// CancelPreparedStream/CleanupSession can all read or clear it from different
		// goroutines, and claimedHashes (client.go) reads it from yet another goroutine (another
		// session's) to decide what torrent data is safe to drop - so every access must go
		// through the getPreloadedStream/setPreloadedStream/takePreloadedStream helpers below
		// rather than touching the field directly.
		preloadedStreamMu     sync.Mutex
		shouldPreloadStream   atomic.Bool // Flag on whether the client should prepare a stream
		currentClientIdMu     sync.RWMutex
		currentClientId       string // Track the client ID of the current stream for session cleanup

		// startStreamGeneration is bumped by every StartStream call. The readiness-polling
		// goroutines it spawns capture the generation at start and abort once it's stale (a
		// newer StartStream call has superseded them), instead of running to completion against
		// whatever torrent/file happens to be "current" by the time they finish waiting.
		startStreamGeneration atomic.Int64
	}

	Settings struct {
		models.TorrentstreamSettings
		Host string
		Port int
	}

	preloadedStream struct {
		Torrent    *itorrent.Torrent
		File       *itorrent.File
		Options *StartStreamOptions
		Release string
	}

	NewRepositoryOptions struct {
		Logger              *zerolog.Logger
		TorrentRepository   *torrent.Repository
		BaseAnimeCache      *anilist.BaseAnimeCache
		CompleteAnimeCache  *anilist.CompleteAnimeCache
		PlatformRef         *util.Ref[platform.Platform]
		MetadataProviderRef *util.Ref[metadata_provider.Provider]
		PlaybackManager     *playbackmanager.PlaybackManager
		WSEventManager      events.WSEventManagerInterface
		Database            *db.Database
		DirectStreamManager *directstream.Manager
		NativePlayer        *nativeplayer.NativePlayer
	}
)

// NewRepository creates a new injectable Repository instance
func NewRepository(opts *NewRepositoryOptions) *Repository {
	ret := &Repository{
		client:                          nil,
		handler:                         nil,
		selectionHistoryMap:             result.NewMap[int, *hibiketorrent.AnimeTorrent](),
		torrentRepository:               opts.TorrentRepository,
		baseAnimeCache:                  opts.BaseAnimeCache,
		completeAnimeCache:              opts.CompleteAnimeCache,
		platformRef:                     opts.PlatformRef,
		wsEventManager:                  opts.WSEventManager,
		metadataProviderRef:             opts.MetadataProviderRef,
		playbackManager:                 opts.PlaybackManager,
		mediaPlayerSubscriberID:         "torrentstream-" + uuid.NewString(),
		logger:                          opts.Logger,
		db:                              opts.Database,
		directStreamManager:             opts.DirectStreamManager,
		nativePlayer:                    opts.NativePlayer,
		previousStreamOptions:           mo.None[*StartStreamOptions](),
		preloadedStream:                 mo.None[*preloadedStream](),
	}

	ret.autoSelect = autoselect.New(&autoselect.NewAutoSelectOptions{
		Logger:            opts.Logger,
		TorrentRepository: opts.TorrentRepository,
		MetadataProvider:  opts.MetadataProviderRef,
		Platform:          opts.PlatformRef,
	})

	ret.client = NewClient(ret)
	ret.handler = newHandler(ret)
	return ret
}

// getPreloadedStream returns the currently preloaded stream, if any, without clearing it.
func (r *Repository) getPreloadedStream() (*preloadedStream, bool) {
	r.preloadedStreamMu.Lock()
	defer r.preloadedStreamMu.Unlock()
	return r.preloadedStream.Get()
}

// setPreloadedStream stores a newly prepared stream, replacing any previous one.
func (r *Repository) setPreloadedStream(ps *preloadedStream) {
	r.preloadedStreamMu.Lock()
	defer r.preloadedStreamMu.Unlock()
	r.preloadedStream = mo.Some(ps)
}

// takePreloadedStream atomically returns and clears the preloaded stream, so callers that mean
// to consume or cancel it can't race each other into double-cancelling or losing it.
func (r *Repository) takePreloadedStream() (*preloadedStream, bool) {
	r.preloadedStreamMu.Lock()
	defer r.preloadedStreamMu.Unlock()
	ps, ok := r.preloadedStream.Get()
	if ok {
		r.preloadedStream = mo.None[*preloadedStream]()
	}
	return ps, ok
}

func (r *Repository) IsEnabled() bool {
	settings, ok := r.getSettings()
	return ok && settings.Enabled && r.client != nil
}

func (r *Repository) getSettings() (Settings, bool) {
	if s := r.settings.Load(); s != nil {
		return *s, true
	}
	return Settings{}, false
}

// GetClient returns the underlying torrent client wrapper.
func (r *Repository) GetClient() *Client {
	return r.client
}

// GetAutoSelect returns the underlying auto-select instance.
func (r *Repository) GetAutoSelect() *autoselect.AutoSelect {
	return r.autoSelect
}

// SetSettings sets the torrentstream settings without initializing the torrent client.
// Used by per-session repositories that share the anacrolix engine.
func (r *Repository) SetSettings(settings *models.TorrentstreamSettings, host string, port int) {
	if settings != nil {
		s := *settings
		if s.DownloadDir == "" {
			s.DownloadDir = r.getDefaultDownloadPath()
		}
		r.settings.Store(&Settings{
			TorrentstreamSettings: s,
			Host:                  host,
			Port:                  port,
		})
	}
}

func (r *Repository) GetPreviousStreamOptions() (*StartStreamOptions, bool) {
	r.previousStreamOptionsMu.RLock()
	defer r.previousStreamOptionsMu.RUnlock()
	return r.previousStreamOptions.OrElse(nil), r.previousStreamOptions.IsPresent()
}

// setPreviousStreamOptions is the only writer of previousStreamOptions - see
// previousStreamOptionsMu's doc comment on the field. It also clears autoSelectedRelease, which
// belongs to the previous stream until the new one's selection finishes.
func (r *Repository) setPreviousStreamOptions(opts *StartStreamOptions) {
	r.previousStreamOptionsMu.Lock()
	defer r.previousStreamOptionsMu.Unlock()
	r.previousStreamOptions = mo.Some(opts)
	r.autoSelectedRelease = ""
}

// setAutoSelectedRelease records the release picked for opts, unless a newer StartStream has
// replaced opts meanwhile - selection is slow, so a superseded call can finish after the new one.
func (r *Repository) setAutoSelectedRelease(opts *StartStreamOptions, release string) {
	r.previousStreamOptionsMu.Lock()
	defer r.previousStreamOptionsMu.Unlock()
	if r.previousStreamOptions.OrElse(nil) == opts {
		r.autoSelectedRelease = release
	}
}

// takeAutoSelectedRelease returns the current stream's auto-selected release together with the
// options it was started with, and clears the release so a duplicate failure report for the same
// stream can't trigger a second retry.
func (r *Repository) takeAutoSelectedRelease() (string, *StartStreamOptions) {
	r.previousStreamOptionsMu.Lock()
	defer r.previousStreamOptionsMu.Unlock()
	release := r.autoSelectedRelease
	r.autoSelectedRelease = ""
	return release, r.previousStreamOptions.OrElse(nil)
}

// ActiveStreamInfo is a snapshot of the torrent currently streaming for a profile's
// session, used by the admin activity view. MediaID/EpisodeNumber/Title are best-effort:
// they come from the most recently started stream's options and the in-memory anime
// cache, not a fresh lookup — a cache miss just leaves Title empty (TorrentName still
// identifies the stream).
//
// Known limitation: StartStream sets previousStreamOptions immediately (so watch-party sync
// can see the new episode right away), but currentTorrent/currentTorrentStatus on Client
// aren't updated until torrent selection finishes, which can take seconds. A poll landing in
// that window pairs the new episode's title with the previous episode's progress/speed/seeders.
// This is a narrow, self-correcting display quirk (resolves on the next poll once selection
// completes), not a functional or data-safety issue, and isn't eliminated here since the two
// fields live on different structs (Repository vs Client) updated by design at different points
// in StartStream for reasons unrelated to this admin view.
type ActiveStreamInfo struct {
	MediaID       int
	EpisodeNumber int
	TorrentName   string
	Title         string
	Status        TorrentStatus
}

// GetActiveStreamInfo returns a snapshot of the torrent this repository's client is
// currently streaming, or ok=false if nothing is actively streaming.
func (r *Repository) GetActiveStreamInfo() (ActiveStreamInfo, bool) {
	torrentName, status, ok := r.client.GetActiveStreamInfo()
	if !ok {
		return ActiveStreamInfo{}, false
	}

	info := ActiveStreamInfo{TorrentName: torrentName, Status: status}
	if opts, hasOpts := r.GetPreviousStreamOptions(); hasOpts && opts != nil {
		info.MediaID = opts.MediaId
		info.EpisodeNumber = opts.EpisodeNumber
		if r.baseAnimeCache != nil {
			if anime, found := r.baseAnimeCache.Get(opts.MediaId); found && anime != nil {
				info.Title = anime.GetTitleSafe()
			}
		}
	}
	return info, true
}

// SetMediaPlayerRepository sets the desktop media player and listens to its events. Must be called
// after instantiating the repository, even if the module is disabled.
func (r *Repository) SetMediaPlayerRepository(mediaPlayerRepository *mediaplayer.Repository) {
	r.playback.listenerMu.Lock()
	defer r.playback.listenerMu.Unlock()
	if r.mediaPlayerRepository != nil && r.mediaPlayerRepository != mediaPlayerRepository {
		r.mediaPlayerRepository.Unsubscribe(r.mediaPlayerSubscriberID)
	}
	r.mediaPlayerRepository = mediaPlayerRepository
	r.listenToMediaPlayerEventsLocked()
}

// InitModules sets the settings for the torrentstream module.
// It should be called before any other method, to ensure the module is active.
func (r *Repository) InitModules(settings *models.TorrentstreamSettings, host string, port int) (err error) {
	r.client.Shutdown()

	defer util.HandlePanicInModuleWithError("torrentstream/InitModules", &err)

	if settings == nil {
		r.logger.Error().Msg("torrentstream: Cannot initialize module, no settings provided")
		r.settings.Store(nil)
		return errors.New("torrentstream: Cannot initialize module, no settings provided")
	}

	s := *settings

	if !s.Enabled {
		r.logger.Info().Msg("torrentstream: Module is disabled")
		r.settings.Store(nil)
		return nil
	}

	// Set default download directory, which is a temporary directory
	if s.DownloadDir == "" {
		s.DownloadDir = r.getDefaultDownloadPath()
		_ = os.MkdirAll(s.DownloadDir, os.ModePerm) // Create the directory if it doesn't exist
	}

	if s.StreamingServerPort == 0 {
		s.StreamingServerPort = 43214
	}
	if s.TorrentClientPort == 0 {
		s.TorrentClientPort = 43213
	}
	if s.StreamingServerHost == "" {
		s.StreamingServerHost = "127.0.0.1"
	}

	// Set the settings
	r.settings.Store(&Settings{
		TorrentstreamSettings: s,
		Host:                  host,
		Port:                  port,
	})

	// Initialize the torrent client
	err = r.client.initializeClient()
	if err != nil {
		return err
	}

	// Start listening to native player events
	r.ListenToNativePlayerEvents()

	r.logger.Info().Msg("torrentstream: Module initialized")
	return nil
}

func (r *Repository) HTTPStreamHandler() http.Handler {
	return r.handler
}

var errNoSettings = errors.New("torrentstream: no settings provided, the module is dormant")

func (r *Repository) FailIfNoSettings() error {
	if r.settings.Load() == nil {
		return errNoSettings
	}
	return nil
}

// Shutdown closes the engine. Only for the app-wide repository that owns it.
func (r *Repository) Shutdown() {
	r.logger.Debug().Msg("torrentstream: Shutting down module")
	r.client.Shutdown()
}

// CleanupSession releases per-session resources without touching the shared
// anacrolix torrent engine or other sessions' state. Safe to call from the
// StreamSessionManager cleanup loop on idle session eviction.
//
// It drops the torrent started by this session (if any), removes the session's
// activeStreams entry, cancels any preloaded stream, and resets playback state.
func (r *Repository) CleanupSession() {
	// Belt-and-braces: torrent.Torrent.Drop() can panic if the underlying
	// anacrolix client was already closed (shouldn't happen since we never
	// close the shared engine from here, but recover keeps session eviction
	// robust against any future regression in the shared-client lifecycle).
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Warn().Interface("panic", rec).Msg("torrentstream: Panic in CleanupSession")
		}
	}()
	r.logger.Debug().Msg("torrentstream: Cleaning up session resources")

	if r.client == nil {
		return
	}

	if r.nativePlayer != nil {
		r.nativePlayer.VideoCore().Unsubscribe("torrentstream")
	}
	r.stopMediaPlayerListener()

	r.client.mu.Lock()
	defer r.client.mu.Unlock()

	// Release this session's claims: remove its activeStreams entry and
	// clear the legacy current torrent/file.
	r.currentClientIdMu.Lock()
	clientId := r.currentClientId
	r.currentClientId = ""
	r.currentClientIdMu.Unlock()
	if clientId != "" {
		r.client.RemoveActiveStream(clientId)
	}
	r.client.currentTorrent = mo.None[*itorrent.Torrent]()
	r.client.currentFile = mo.None[*itorrent.File]()

	// Unclaim this session's preloaded stream, so the drop below releases it
	r.takePreloadedStream()

	r.playback.currentVideoDuration.Store(0)
	r.playback.desktopPlayerStream.Store(false)

	// Stop this wrapper's monitor goroutine (previously leaked on every
	// session eviction) and remove it from the shared registry so its
	// claims are released.
	if r.client.cancelFunc != nil {
		r.client.cancelFunc()
		r.client.cancelFunc = nil
	}
	unregisterClient(r.client)

	// Drop torrents that no remaining session claims (only this session's
	// torrents can become unclaimed here)
	r.client.dropUnclaimedTorrentsLocked()
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

func (r *Repository) GetDownloadDir() string {
	if settings, ok := r.getSettings(); ok && settings.DownloadDir != "" {
		return settings.DownloadDir
	}
	return r.getDefaultDownloadPath()
}

func (r *Repository) getDefaultDownloadPath() string {
	tempDir := os.TempDir()
	downloadDirPath := filepath.Join(tempDir, "seanime", "torrentstream")
	return downloadDirPath
}
