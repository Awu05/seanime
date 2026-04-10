package core

import (
	"seanime/internal/api/anilist"
	"seanime/internal/directstream"
	"seanime/internal/library/playbackmanager"
	"seanime/internal/nativeplayer"
	"seanime/internal/torrentstream"
	"seanime/internal/videocore"
	"time"
)

// CreateStreamSession creates a new ProfileStreamSession with all streaming components initialized.
// Each session gets its own per-profile instances. The TorrentstreamRepository gets its own Client
// wrapper (with its own currentTorrent/currentFile tracking) but shares the single anacrolix
// torrent engine from the App's singleton.
func (a *App) CreateStreamSession(profileID string) *ProfileStreamSession {
	refreshAnimeCollection := func() {
		_, _ = a.RefreshAnimeCollection()
	}

	// Create VideoCore
	vc := videocore.New(videocore.NewVideoCoreOptions{
		WsEventManager:             a.WSEventManager,
		Logger:                     a.Logger,
		ContinuityManager:          a.ContinuityManager,
		MetadataProviderRef:        a.MetadataProviderRef,
		DiscordPresence:            a.DiscordPresence,
		PlatformRef:                a.AnilistPlatformRef,
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
		PlatformRef:                a.AnilistPlatformRef,
		MetadataProviderRef:        a.MetadataProviderRef,
		Database:                   a.Database,
		RefreshAnimeCollectionFunc: refreshAnimeCollection,
		DiscordPresence:            a.DiscordPresence,
		IsOfflineRef:               a.IsOfflineRef(),
		ContinuityManager:          a.ContinuityManager,
	})

	// Create DirectStreamManager (depends on NativePlayer + VideoCore)
	dsm := directstream.NewManager(directstream.NewManagerOptions{
		Logger:                     a.Logger,
		WSEventManager:             a.WSEventManager,
		ContinuityManager:          a.ContinuityManager,
		MetadataProviderRef:        a.MetadataProviderRef,
		DiscordPresence:            a.DiscordPresence,
		PlatformRef:                a.AnilistPlatformRef,
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
	})

	// Create per-session TorrentstreamRepository with its own Client wrapper,
	// but sharing the anacrolix torrent engine from the App's singleton.
	tsr := torrentstream.NewRepository(&torrentstream.NewRepositoryOptions{
		Logger:              a.Logger,
		BaseAnimeCache:      anilist.NewBaseAnimeCache(),
		CompleteAnimeCache:  anilist.NewCompleteAnimeCache(),
		MetadataProviderRef: a.MetadataProviderRef,
		TorrentRepository:   a.TorrentRepository,
		PlatformRef:         a.AnilistPlatformRef,
		PlaybackManager:     pm,
		WSEventManager:      a.WSEventManager,
		Database:            a.Database,
		DirectStreamManager: dsm,
		NativePlayer:        np,
	})

	// Share the anacrolix engine from the App's singleton instead of creating a new one.
	// This avoids port conflicts while giving each session its own state tracking.
	if a.TorrentstreamRepository != nil {
		appClient := a.TorrentstreamRepository.GetClient()
		if appClient != nil {
			if tc := appClient.GetTorrentClient(); tc.IsPresent() {
				tsr.GetClient().UseSharedTorrentClient(tc.MustGet())
			}
		}
	}

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
		ProfileID:           profileID,
		LastActive:          time.Now(),
		VideoCore:           vc,
		NativePlayer:        np,
		PlaybackManager:     pm,
		DirectStreamManager: dsm,
		TorrentStream:       tsr,
	}
}
