package core

import (
	"seanime/internal/api/simkl"
	"seanime/internal/platforms/platform"
	syncpkg "seanime/internal/sync"
	"seanime/internal/util"
)

// DefaultSimklProfileID is the sentinel profile ID used for the app-level default profile
// (single-user / sidecar mode, and the admin's global platform in multi-user mode). SIMKL
// account/settings rows and pending-sync rows for that profile are keyed on this string,
// never on the empty string - see NormalizeSimklProfileID.
const DefaultSimklProfileID = "_default"

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

// wrapAnilistPlatform wraps raw with MirroringPlatform for the given profile. This is the
// ONLY place that should ever construct a MirroringPlatform - every code path that installs
// or hands out an AniList platform for a profile must go through this, or SIMKL mirroring
// and AniList-retry-queueing silently stop working for that platform.
func (a *App) wrapAnilistPlatform(raw platform.Platform, profileID string) platform.Platform {
	return syncpkg.NewMirroringPlatform(
		raw,
		syncpkg.NewResolvingSimklClient(simkl.DefaultHTTPClient, profileID, a.simklTokenLookup),
		a.Database,
		profileID,
		a.simklEnabledFor(profileID),
	)
}

// setDefaultAnilistPlatform is the ONLY place that should call a.AnilistPlatformRef.Set(...)
// for the app-level default profile. It keeps rawAnilistPlatformRef in sync so the SIMKL
// worker always resolves the live, currently-active raw platform, and ensures the wrapper is
// never silently stripped by a re-login, platform switch, or offline-mode toggle.
func (a *App) setDefaultAnilistPlatform(raw platform.Platform) {
	// Idempotent: if a caller ever hands back an already-wrapped platform (e.g. reinstalling
	// AnilistPlatformRef.Get()), unwrap first rather than nesting wrappers, which would
	// double-mirror every mutation and double-enqueue every failure.
	raw = syncpkg.RawPlatform(raw)
	if a.rawAnilistPlatformRef == nil {
		a.rawAnilistPlatformRef = util.NewRef[platform.Platform](raw)
	} else {
		a.rawAnilistPlatformRef.Set(raw)
	}
	a.AnilistPlatformRef.Set(a.wrapAnilistPlatform(raw, DefaultSimklProfileID))
}
