package autoscanner

import (
	"seanime/internal/database/models"
	"seanime/internal/platforms/platform"
	"seanime/internal/testmocks"
	"seanime/internal/util"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each profile's scan must match against that profile's own AniList collection.
func TestAutoScannerPlatformRefFor(t *testing.T) {
	globalRef := util.NewRef[platform.Platform](testmocks.NewFakePlatformBuilder().Build())
	andyRef := util.NewRef[platform.Platform](testmocks.NewFakePlatformBuilder().Build())

	as := New(&NewAutoScannerOptions{
		PlatformRef: globalRef,
		ProfilePlatformRef: func(profileID string) *util.Ref[platform.Platform] {
			switch profileID {
			case "andy":
				return andyRef
			case "empty":
				return util.NewRef[platform.Platform](nil)
			}
			return nil
		},
	})
	require.Same(t, andyRef, as.platformRefFor("andy"))
	require.Same(t, globalRef, as.platformRefFor("other"))
	require.Same(t, globalRef, as.platformRefFor("empty"), "a ref holding no platform falls back")

	noResolver := New(&NewAutoScannerOptions{PlatformRef: globalRef})
	require.Same(t, globalRef, noResolver.platformRefFor("andy"))
}

// Autoscans must use the same matching settings a manual scan from the profile would.
func TestAutoScannerLibrarySettingsFor(t *testing.T) {
	global := models.LibrarySettings{ScannerMatchingThreshold: 0.1}

	as := New(&NewAutoScannerOptions{
		ProfileLibrarySettings: func(profileID string) *models.LibrarySettings {
			if profileID == "andy" {
				return &models.LibrarySettings{ScannerMatchingThreshold: 0.9, ScannerUseLegacyMatching: true}
			}
			return nil
		},
	})
	andy := as.librarySettingsFor("andy", global)
	assert.Equal(t, 0.9, andy.ScannerMatchingThreshold)
	assert.True(t, andy.ScannerUseLegacyMatching)
	assert.Equal(t, global, as.librarySettingsFor("other", global))

	assert.Equal(t, global, New(&NewAutoScannerOptions{}).librarySettingsFor("andy", global))
}

// Autoscans must walk the profile's own library folders too, or the scanner drops
// or shelves the files a manual scan found there.
func TestAutoScannerOtherLibraryPathsIncludesProfilePaths(t *testing.T) {
	h := newAutoScannerTestWrapper(t, false, 0)
	_, err := h.database.CreateProfile(&models.Profile{UUIDBaseModel: models.UUIDBaseModel{ID: "andy"}, Name: "Andy"})
	require.NoError(t, err)
	for _, p := range []string{"/andy-only", "/downloads", "/anime"} {
		_, err := h.database.CreateLibraryPath(&models.LibraryPath{Path: p, OwnerID: "andy"})
		require.NoError(t, err)
	}

	library := &models.LibrarySettings{LibraryPath: "/anime", LibraryPaths: []string{"/downloads"}}

	assert.ElementsMatch(t, []string{"/downloads", "/andy-only"}, h.autoScanner.otherLibraryPaths("andy", library),
		"profile paths are added once, without repeating the main or global paths")
	assert.Equal(t, []string{"/downloads"}, h.autoScanner.otherLibraryPaths("", library))
	assert.Equal(t, models.LibraryPaths{"/downloads"}, library.LibraryPaths, "settings slice is not modified")
}
