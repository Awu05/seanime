package core

import (
	"seanime/internal/api/anilist"
	"seanime/internal/platforms/anilist_platform"
	"seanime/internal/platforms/platform"
	"seanime/internal/util"
	"sync"
)

// AnilistClientPool manages per-profile AniList clients and platforms.
// Each profile gets its own client (with its own token) to avoid cross-profile data leaking.
type AnilistClientPool struct {
	clients   map[string]anilist.AnilistClient   // keyed by profileID
	platforms map[string]platform.Platform        // keyed by profileID
	mu        sync.RWMutex
	app       *App
}

func NewAnilistClientPool(app *App) *AnilistClientPool {
	return &AnilistClientPool{
		clients:   make(map[string]anilist.AnilistClient),
		platforms: make(map[string]platform.Platform),
		app:       app,
	}
}

// GetClientForProfile returns an AniList client for the given profile.
// Creates one lazily from the profile's stored token.
func (p *AnilistClientPool) GetClientForProfile(profileID string) anilist.AnilistClient {
	if profileID == "" {
		return p.app.AnilistClientRef.Get()
	}

	p.mu.RLock()
	if client, ok := p.clients[profileID]; ok {
		p.mu.RUnlock()
		return client
	}
	p.mu.RUnlock()

	// Create a new client from the profile's token
	token := p.app.Database.GetAnilistTokenForProfile(profileID)
	if token == "" {
		// Return an empty client — don't fall back to global (admin's client)
		return anilist.NewAnilistClient("", p.app.AnilistCacheDir)
	}

	client := anilist.NewAnilistClient(token, p.app.AnilistCacheDir)

	p.mu.Lock()
	p.clients[profileID] = client
	p.mu.Unlock()

	return client
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
		emptyClient := anilist.NewAnilistClient("", p.app.AnilistCacheDir)
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

	client := anilist.NewAnilistClient(token, p.app.AnilistCacheDir)
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
	p.clients[profileID] = client
	p.platforms[profileID] = plat
	p.mu.Unlock()

	return plat
}

// InvalidateProfile removes cached client/platform for a profile (e.g. on login/logout).
func (p *AnilistClientPool) InvalidateProfile(profileID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if plat, ok := p.platforms[profileID]; ok {
		plat.Close()
	}
	delete(p.clients, profileID)
	delete(p.platforms, profileID)
}
