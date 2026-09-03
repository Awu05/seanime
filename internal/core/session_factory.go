package core

import (
	"context"
	"net/http"
	"seanime/internal/api/anilist"
	"seanime/internal/directstream"
	"seanime/internal/library/playbackmanager"
	"seanime/internal/nativeplayer"
	"seanime/internal/platforms/platform"
	syncpkg "seanime/internal/sync"
	"seanime/internal/torrentstream"
	"seanime/internal/util"
	"seanime/internal/videocore"
	"time"
)

// sessionPlatform returns the platform a profile's stream session must use.
// In multi-user mode this comes from the AnilistPool so progress updates and
// collection fetches go to the profile's own AniList account — never the
// global platform, which carries whichever account logged in last.
func (a *App) sessionPlatform(profileID string) (platform.Platform, bool) {
	if a.MultiUserEnabled && profileID != "" && profileID != "_default" && a.AnilistPool != nil {
		return a.AnilistPool.GetPlatformForProfile(profileID), true
	}
	return a.AnilistPlatformRef.Get(), false
}

// Shutdown releases per-session streaming resources without touching shared infrastructure
// (the anacrolix torrent engine, MediaPlayerRepository, etc.). Called when a session is evicted
// by the idle cleanup loop. Safe to call multiple times.
func (s *ProfileStreamSession) Shutdown() {
	defer util.HandlePanicInModuleThen("core/ProfileStreamSession.Shutdown", func() {})
	if s.DirectStreamManager != nil {
		s.DirectStreamManager.Shutdown()
	}
	if s.TorrentStream != nil {
		s.TorrentStream.CleanupSession()
	}
}

// SeedSessionCollection pulls the profile's anime collection from its own platform
// and seeds the session's PlaybackManager and DirectStreamManager. This is called
// outside the StreamSessionManager lock because GetAnimeCollection may fall back to
// a network request on cache miss, which must not block concurrent session creation
// or settings refresh. Safe to call multiple times (idempotent).
func (a *App) SeedSessionCollection(profileID string, session *ProfileStreamSession) {
	defer util.HandlePanicInModuleThen("core/SeedSessionCollection", func() {})
	plat, _ := a.sessionPlatform(profileID)
	if plat == nil {
		return
	}
	collection, err := plat.GetAnimeCollection(context.Background(), false)
	if err != nil || collection == nil {
		return
	}
	if session.PlaybackManager != nil {
		session.PlaybackManager.SetAnimeCollection(collection)
	}
	if session.DirectStreamManager != nil {
		session.DirectStreamManager.SetAnimeCollection(collection)
	}
}

// CreateStreamSession creates a new ProfileStreamSession with all streaming components initialized.
// Each session gets its own per-profile instances. The TorrentstreamRepository gets its own Client
// wrapper (with its own currentTorrent/currentFile tracking) but shares the single anacrolix
// torrent engine from the App's singleton.
func (a *App) CreateStreamSession(profileID string) *ProfileStreamSession {
	// Resolve the profile's own platform (pool-backed in multi-user mode).
	plat, isProfilePlatform := a.sessionPlatform(profileID)
	if isProfilePlatform {
		plat = syncpkg.NewMirroringPlatform(
			plat,
			syncpkg.NewResolvingSimklClient(http.DefaultClient, profileID, func(pid string) (string, bool) {
				account, err := a.Database.GetSimklAccount(pid)
				if err != nil {
					return "", false
				}
				return account.AccessToken, true
			}),
			a.Database,
			profileID,
			func() bool {
				settings, err := a.Database.GetSimklSettings(profileID)
				if err != nil {
					return false
				}
				_, connectErr := a.Database.GetSimklAccount(profileID)
				return connectErr == nil && settings.Enabled
			},
		)
	}
	platformRef := a.AnilistPlatformRef
	refreshAnimeCollection := func() {
		_, _ = a.RefreshAnimeCollection()
	}
	if isProfilePlatform {
		platformRef = util.NewRef[platform.Platform](plat)
		refreshAnimeCollection = func() {
			defer util.HandlePanicThen(func() {})
			_, _ = plat.GetAnimeCollection(context.Background(), true)
		}
	}

	// Create VideoCore
	vc := videocore.New(videocore.NewVideoCoreOptions{
		WsEventManager:             a.WSEventManager,
		Logger:                     a.Logger,
		ContinuityManager:          a.ContinuityManager,
		MetadataProviderRef:        a.MetadataProviderRef,
		DiscordPresence:            a.DiscordPresence,
		PlatformRef:                platformRef,
		RefreshAnimeCollectionFunc: refreshAnimeCollection,
		IsOfflineRef:               a.IsOfflineRef(),
	})

	// Create NativePlayer (depends on VideoCore)
	np := nativeplayer.New(nativeplayer.NewNativePlayerOptions{
		WsEventManager: a.WSEventManager,
		Logger:         a.Logger,
		VideoCore:      vc,
	})

	// Create PlaybackManager
	pm := playbackmanager.New(&playbackmanager.NewPlaybackManagerOptions{
		WSEventManager:             a.WSEventManager,
		Logger:                     a.Logger,
		PlatformRef:                platformRef,
		MetadataProviderRef:        a.MetadataProviderRef,
		Database:                   a.Database,
		RefreshAnimeCollectionFunc: refreshAnimeCollection,
		DiscordPresence:            a.DiscordPresence,
		IsOfflineRef:               a.IsOfflineRef(),
		ContinuityManager:          a.ContinuityManager,
		ProfileID:                  profileID,
	})

	// Create DirectStreamManager (depends on NativePlayer + VideoCore)
	dsm := directstream.NewManager(directstream.NewManagerOptions{
		Logger:                     a.Logger,
		WSEventManager:             a.WSEventManager,
		ContinuityManager:          a.ContinuityManager,
		MetadataProviderRef:        a.MetadataProviderRef,
		DiscordPresence:            a.DiscordPresence,
		PlatformRef:                platformRef,
		RefreshAnimeCollectionFunc: refreshAnimeCollection,
		IsOfflineRef:               a.IsOfflineRef(),
		NativePlayer:               np,
		VideoCore:                  vc,
		HMACTokenFunc: func(endpoint string, symbol string) string {
			qp, err := a.GetServerPasswordHMACAuth().GenerateQueryParam(endpoint, symbol)
			if err != nil {
				return ""
			}
			return qp
		},
		// Without this, debrid-torrent play through this session silently loses
		// HLS transcode, background download, and switchover support.
		TranscodeRequester: &mediastreamTranscodeAdapter{repo: a.MediastreamRepository},
	})

	// Create per-session TorrentstreamRepository with its own Client wrapper,
	// but sharing the anacrolix torrent engine from the App's singleton.
	tsr := torrentstream.NewRepository(&torrentstream.NewRepositoryOptions{
		Logger:              a.Logger,
		BaseAnimeCache:      anilist.NewBaseAnimeCache(),
		CompleteAnimeCache:  anilist.NewCompleteAnimeCache(),
		MetadataProviderRef: a.MetadataProviderRef,
		TorrentRepository:   a.TorrentRepository,
		PlatformRef:         platformRef,
		PlaybackManager:     pm,
		WSEventManager:      a.WSEventManager,
		Database:            a.Database,
		DirectStreamManager: dsm,
		NativePlayer:        np,
	})

	// Share the anacrolix engine from the App's singleton instead of creating a new one.
	// This avoids port conflicts while giving each session its own state tracking. If the
	// singleton's own engine hasn't finished initializing yet, this is a no-op for now -
	// InitOrRefreshTorrentstreamSettings re-attempts it on every settings broadcast so the
	// reference self-heals instead of staying stale/absent forever.
	tsr.SyncSharedTorrentClient(a.TorrentstreamRepository)

	// Copy settings from App singleton
	if a.SecondarySettings.Torrentstream != nil {
		tsr.SetSettings(a.SecondarySettings.Torrentstream, a.Config.Server.Host, a.Config.Server.Port)
	}

	// Set media player repository if available
	if a.MediaPlayerRepository != nil {
		tsr.SetMediaPlayerRepository(a.MediaPlayerRepository)
		pm.SetMediaPlayerRepository(a.MediaPlayerRepository)
	}

	return &ProfileStreamSession{
		LastActive:          time.Now(),
		VideoCore:           vc,
		PlaybackManager:     pm,
		DirectStreamManager: dsm,
		TorrentStream:       tsr,
	}
}
