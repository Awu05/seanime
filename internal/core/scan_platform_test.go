package core

import (
	"seanime/internal/database/db"
	"seanime/internal/database/models"
	"seanime/internal/platforms/platform"
	"seanime/internal/testmocks"
	"seanime/internal/testutil"
	"seanime/internal/util"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type scanTestApp struct {
	app      *App
	database *db.Database
	global   platform.Platform
	built    []platform.Platform // every platform the pool constructed, in order
}

// newScanTestApp builds an App whose pool hands out fake platforms, with one
// stored profile "andy".
func newScanTestApp(t *testing.T, multiUser bool) *scanTestApp {
	t.Helper()
	env := testutil.NewTestEnv(t)
	database := env.MustNewDatabase(util.NewLogger())
	_, err := database.CreateProfile(&models.Profile{UUIDBaseModel: models.UUIDBaseModel{ID: "andy"}, Name: "Andy"})
	require.NoError(t, err)

	h := &scanTestApp{database: database, global: testmocks.NewFakePlatformBuilder().Build()}
	h.app = &App{
		Database:           database,
		AnilistPlatformRef: util.NewRef[platform.Platform](h.global),
	}
	h.app.MultiUserEnabled.Store(multiUser)
	h.app.AnilistPool = NewAnilistClientPool(h.app)
	h.app.AnilistPool.newPlatform = func(string) platform.Platform {
		p := testmocks.NewFakePlatformBuilder().Build()
		h.built = append(h.built, p)
		return p
	}
	return h
}

// A library scan builds its matching candidates from the AniList collection of the
// platform it is handed. In multi-user mode that must be the scanning profile's own
// platform: the global platform belongs to whichever account logged in last (or none),
// and scanning with it yields "[matcher] no media fed into the matcher".
func TestScanPlatformRefForProfile(t *testing.T) {
	t.Run("multi-user profile scans with its own platform", func(t *testing.T) {
		h := newScanTestApp(t, true)
		ref := h.app.ScanPlatformRef("andy")
		require.Len(t, h.built, 1)
		assert.Same(t, h.built[0], ref.Get())
		assert.NotSame(t, h.global, ref.Get())
	})

	t.Run("single-user mode keeps the live global ref", func(t *testing.T) {
		h := newScanTestApp(t, false)
		assert.Same(t, h.app.AnilistPlatformRef, h.app.ScanPlatformRef("andy"))
	})

	t.Run("default profile keeps the live global ref", func(t *testing.T) {
		h := newScanTestApp(t, true)
		assert.Same(t, h.app.AnilistPlatformRef, h.app.ScanPlatformRef(DefaultProfileID))
		assert.Same(t, h.app.AnilistPlatformRef, h.app.ScanPlatformRef(""))
	})
}

// A scan that is running when the profile logs in, out or switches accounts must
// continue with the new platform, not the closed one it started with.
func TestScanPlatformRefFollowsInvalidation(t *testing.T) {
	h := newScanTestApp(t, true)
	ref := h.app.ScanPlatformRef("andy")
	require.Same(t, ref, h.app.ScanPlatformRef("andy"), "one live ref per profile")

	h.app.AnilistPool.InvalidateProfile("andy")

	require.Len(t, h.built, 2)
	assert.Same(t, h.built[1], ref.Get())
}

func TestScanPlatformRefDroppedForDeletedProfile(t *testing.T) {
	h := newScanTestApp(t, true)
	_ = h.app.ScanPlatformRef("andy")
	require.NoError(t, h.database.DeleteProfile("andy"))

	h.app.AnilistPool.InvalidateProfile("andy")

	assert.Len(t, h.built, 1, "no platform is rebuilt for a deleted profile")
	h.app.AnilistPool.mu.RLock()
	defer h.app.AnilistPool.mu.RUnlock()
	assert.NotContains(t, h.app.AnilistPool.liveRefs, "andy")
	assert.NotContains(t, h.app.AnilistPool.platforms, "andy")
}

func TestScanLibrarySettings(t *testing.T) {
	seed := func(t *testing.T, h *scanTestApp) {
		_, err := h.database.UpsertSettings(&models.Settings{
			BaseModel: models.BaseModel{ID: 1},
			Library:   &models.LibrarySettings{ScannerMatchingThreshold: 0.1},
		})
		require.NoError(t, err)
		_, err = h.database.UpsertSettingsForProfile("andy", &models.Settings{
			Library: &models.LibrarySettings{ScannerMatchingThreshold: 0.9, ScannerUseLegacyMatching: true},
		})
		require.NoError(t, err)
	}

	t.Run("multi-user uses the profile's settings", func(t *testing.T) {
		h := newScanTestApp(t, true)
		seed(t, h)
		lib := h.app.ScanLibrarySettings("andy")
		assert.Equal(t, 0.9, lib.ScannerMatchingThreshold)
		assert.True(t, lib.ScannerUseLegacyMatching)
	})

	t.Run("single-user uses the global settings", func(t *testing.T) {
		h := newScanTestApp(t, false)
		seed(t, h)
		assert.Equal(t, 0.1, h.app.ScanLibrarySettings("andy").ScannerMatchingThreshold)
	})

	t.Run("profile without settings gets defaults, never nil", func(t *testing.T) {
		h := newScanTestApp(t, true)
		lib := h.app.ScanLibrarySettings("andy")
		require.NotNil(t, lib)
		assert.Zero(t, lib.ScannerMatchingThreshold)
	})
}
