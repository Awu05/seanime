package core

import (
	"seanime/internal/api/simkl"
	"seanime/internal/platforms/platform"
	"seanime/internal/platforms/shared_platform"
	syncpkg "seanime/internal/sync"
	"seanime/internal/util"
)

// DefaultProfileID is the sentinel profile ID used for the app-level default profile
// (single-user / sidecar mode, and the admin's global platform in multi-user mode). SIMKL
// account/settings rows and pending-sync rows for that profile are keyed on this string,
// never on the empty string - see NormalizeSimklProfileID. Also used wherever else in package
// core the same "no specific profile" sentinel is needed (e.g. stream sessions), so there is
// exactly one name for it rather than a second hand-typed "_default" literal to keep in sync.
const DefaultProfileID = "_default"

// simklTokenLookup returns the SIMKL access token stored for profileID, if any.
func (a *App) simklTokenLookup(profileID string) (string, bool) {
	account, err := a.Database.GetSimklAccount(profileID)
	if err != nil {
		return "", false
	}
	return account.AccessToken, true
}

// simklEnabledFor returns whether SIMKL mirroring should be active for profileID: both an
// active connection (access token present) and the user's enabled toggle.
func (a *App) simklEnabledFor(profileID string) func() bool {
	return func() bool {
		settings, err := a.Database.GetSimklSettings(profileID)
		if err != nil {
			return false
		}
		_, connectErr := a.Database.GetSimklAccount(profileID)
		return connectErr == nil && settings.Enabled
	}
}

// wrapAnilistPlatform wraps raw with MirroringPlatform, then FallbackPlatform, for the given
// profile. This is the ONLY place that should ever construct either wrapper - every code path
// that installs or hands out an AniList platform for a profile must go through this, or SIMKL
// mirroring, AniList-retry-queueing, and the AniList-outage fallback all silently stop working
// for that platform.
//
// FallbackPlatform wraps OUTSIDE MirroringPlatform: if AniList is down, there is no point
// letting MirroringPlatform attempt (and fail, and queue) a doomed AniList call before
// FallbackPlatform gets a turn to redirect to SIMKL instead. Both wrappers share the same
// simklClient instance and the same simklEnabledFor(profileID) closure, so their view of
// "is SIMKL connected/enabled for this profile" can never drift between the two layers.
func (a *App) wrapAnilistPlatform(raw platform.Platform, profileID string) platform.Platform {
	simklClient := syncpkg.NewResolvingSimklClient(simkl.DefaultHTTPClient, profileID, a.simklTokenLookup)
	simklEnabled := a.simklEnabledFor(profileID)
	mirrored := syncpkg.NewMirroringPlatform(raw, simklClient, a.Database, profileID, simklEnabled)
	return syncpkg.NewFallbackPlatform(mirrored, simklClient, a.Database, profileID, simklEnabled, func() bool {
		return shared_platform.IsWorking.Load()
	})
}

// setDefaultAnilistPlatform is the ONLY place that should call a.AnilistPlatformRef.Set(...)
// for the app-level default profile. It keeps rawAnilistPlatformRef in sync so the SIMKL
// worker always resolves the live, currently-active raw platform, and ensures the wrapper is
// never silently stripped by a re-login, platform switch, or offline-mode toggle.
//
// Guarded by platformSwitchMu: rawAnilistPlatformRef and AnilistPlatformRef must always be set
// as one atomic pair - two concurrent callers (e.g. a login racing an offline-mode toggle)
// interleaving their two separate .Set() calls could otherwise leave the pair pointing at
// different platform instances, and the SIMKL worker would then resolve a raw platform that
// doesn't match the one actually installed.
func (a *App) setDefaultAnilistPlatform(raw platform.Platform) {
	a.platformSwitchMu.Lock()
	defer a.platformSwitchMu.Unlock()

	// Idempotent: if a caller ever hands back an already-wrapped platform (e.g. reinstalling
	// AnilistPlatformRef.Get()), unwrap first rather than nesting wrappers, which would
	// double-mirror every mutation and double-enqueue every failure.
	raw = syncpkg.RawPlatform(raw)
	if a.rawAnilistPlatformRef == nil {
		a.rawAnilistPlatformRef = util.NewRef[platform.Platform](raw)
	} else {
		a.rawAnilistPlatformRef.Set(raw)
	}
	a.AnilistPlatformRef.Set(a.wrapAnilistPlatform(raw, DefaultProfileID))
}
