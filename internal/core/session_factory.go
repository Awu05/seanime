package core

import (
	"seanime/internal/directstream"
	"seanime/internal/library/playbackmanager"
	"seanime/internal/nativeplayer"
	"seanime/internal/videocore"
	"time"
)

// CreateStreamSession creates a new ProfileStreamSession with all streaming components initialized.
// This mirrors the initialization in initModulesOnce but creates per-profile instances.
func (a *App) CreateStreamSession(profileID string) *ProfileStreamSession {
	// Create VideoCore
	vc := videocore.New(videocore.NewVideoCoreOptions{
		WsEventManager:      a.WSEventManager,
		Logger:              a.Logger,
		ContinuityManager:   a.ContinuityManager,
		MetadataProviderRef: a.MetadataProviderRef,
		DiscordPresence:     a.DiscordPresence,
		PlatformRef:         a.AnilistPlatformRef,
		RefreshAnimeCollectionFunc: func() {
			_, _ = a.RefreshAnimeCollection()
		},
		IsOfflineRef: a.IsOfflineRef(),
	})

	// Create NativePlayer (depends on VideoCore)
	np := nativeplayer.New(nativeplayer.NewNativePlayerOptions{
		WsEventManager: a.WSEventManager,
		Logger:         a.Logger,
		VideoCore:      vc,
	})

	// Create PlaybackManager
	pm := playbackmanager.New(&playbackmanager.NewPlaybackManagerOptions{
		WSEventManager:      a.WSEventManager,
		Logger:              a.Logger,
		PlatformRef:         a.AnilistPlatformRef,
		MetadataProviderRef: a.MetadataProviderRef,
		Database:            a.Database,
		RefreshAnimeCollectionFunc: func() {
			_, _ = a.RefreshAnimeCollection()
		},
		DiscordPresence:   a.DiscordPresence,
		IsOfflineRef:      a.IsOfflineRef(),
		ContinuityManager: a.ContinuityManager,
	})

	// Create DirectStreamManager (depends on NativePlayer + VideoCore)
	dsm := directstream.NewManager(directstream.NewManagerOptions{
		Logger:              a.Logger,
		WSEventManager:      a.WSEventManager,
		ContinuityManager:   a.ContinuityManager,
		MetadataProviderRef: a.MetadataProviderRef,
		DiscordPresence:     a.DiscordPresence,
		PlatformRef:         a.AnilistPlatformRef,
		RefreshAnimeCollectionFunc: func() {
			_, _ = a.RefreshAnimeCollection()
		},
		IsOfflineRef: a.IsOfflineRef(),
		NativePlayer: np,
		VideoCore:    vc,
		HMACTokenFunc: func(endpoint string, symbol string) string {
			qp, err := a.GetServerPasswordHMACAuth().GenerateQueryParam(endpoint, symbol)
			if err != nil {
				return ""
			}
			return qp
		},
	})

	// Use the App's singleton TorrentstreamRepository — the torrent engine binds to a single
	// port and cannot be duplicated per session. Per-profile isolation happens at the
	// playback/tracking level (PlaybackManager, DirectStreamManager) not the torrent client.
	return &ProfileStreamSession{
		ProfileID:           profileID,
		LastActive:          time.Now(),
		VideoCore:           vc,
		NativePlayer:        np,
		PlaybackManager:     pm,
		DirectStreamManager: dsm,
		TorrentStream:       a.TorrentstreamRepository,
	}
}
