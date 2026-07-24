package core

import (
	"path/filepath"
	"seanime/internal/api/anilist"
	"seanime/internal/platforms/anilist_platform"
	"seanime/internal/platforms/platform"
	"seanime/internal/util"
	"sync"
)

// AnilistClientPool manages per-profile AniList platforms.
// Each profile gets its own platform (with its own token and cache directory)
// to avoid cross-profile data leaking.
type AnilistClientPool struct {
	platforms map[string]platform.Platform // keyed by profileID
	mu        sync.RWMutex
	app       *App
}

func NewAnilistClientPool(app *App) *AnilistClientPool {
	return &AnilistClientPool{
		platforms: make(map[string]platform.Platform),
		app:       app,
	}
}

// profileCacheDir returns a per-profile cache directory. Profiles must not
// share a cache directory: collection cache keys are static, so a shared dir
// would serve one profile's cached collection to another on network fallback.
func (p *AnilistClientPool) profileCacheDir(profileID string) string {
	return filepath.Join(p.app.AnilistCacheDir, "profiles", profileID)
}

// GetPlatformForProfile returns a Platform for the given profile.
// Creates one lazily using the profile's AniList client.
func (p *AnilistClientPool) GetPlatformForProfile(profileID string) platform.Platform {
	if profileID == "" {
		return p.app.AnilistPlatformRef.Get()
	}

	p.mu.RLock()
	if plat, ok := p.platforms[profileID]; ok {
		p.mu.RUnlock()
		return plat
	}
	p.mu.RUnlock()

	// Create a new platform for this profile
	acc, _ := p.app.Database.GetAccountByProfileID(profileID)

	token := ""
	username := ""
	if acc != nil {
		token = acc.Token
		username = acc.Username
	}

	if token == "" {
		// No AniList linked for this profile — return a nil-token platform
		// that returns empty collections, NOT the admin's global platform
		emptyClient := anilist.NewAnilistClient("", p.profileCacheDir(profileID))
		emptyRef := util.NewRef[anilist.AnilistClient](emptyClient)
		plat := anilist_platform.NewAnilistPlatform(
			emptyRef,
			p.app.ExtensionBankRef,
			p.app.Logger,
			p.app.Database,
			func() {},
		)
		p.mu.Lock()
		p.platforms[profileID] = plat
		p.mu.Unlock()
		return plat
	}

	client := anilist.NewAnilistClient(token, p.profileCacheDir(profileID))
	clientRef := util.NewRef[anilist.AnilistClient](client)

	plat := anilist_platform.NewAnilistPlatform(
		clientRef,
		p.app.ExtensionBankRef,
		p.app.Logger,
		p.app.Database,
		func() {}, // no auto-logout for pool clients
	)

	// Set the username so collection fetches work
	plat.SetUsername(username)

	p.mu.Lock()
	p.platforms[profileID] = plat
	p.mu.Unlock()

	return plat
}

// InvalidateProfile removes the cached platform for a profile (e.g. on login/logout).
func (p *AnilistClientPool) InvalidateProfile(profileID string) {
	p.mu.Lock()
	plat := p.platforms[profileID]
	delete(p.platforms, profileID)
	p.mu.Unlock()

	// Close the platform outside the lock — Close may block on I/O or acquire other locks.
	if plat != nil {
		plat.Close()
	}
}
