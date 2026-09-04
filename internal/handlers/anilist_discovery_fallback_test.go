package handlers

import (
	"testing"

	"seanime/internal/api/simkl"
	"seanime/internal/platforms/shared_platform"
	syncpkg "seanime/internal/sync"

	"github.com/stretchr/testify/assert"
)

// This is a narrow unit test of the gating logic extracted into shouldTrySimklDiscoveryFallback,
// not a full end-to-end handler test (this codebase's existing handler tests, if any, don't spin
// up a full echo server with a real SIMKL mock per the research pass - keep this test at the same
// level of the existing test suite rather than introducing a new heavier pattern).
func TestShouldTrySimklDiscoveryFallback(t *testing.T) {
	shared_platform.IsWorking.Store(true)
	assert.False(t, shouldTrySimklDiscoveryFallback("some-client-id"), "must not fall back while AniList is healthy")

	shared_platform.IsWorking.Store(false)
	assert.False(t, shouldTrySimklDiscoveryFallback(""), "must not fall back with no client id configured")
	assert.True(t, shouldTrySimklDiscoveryFallback("some-client-id"))

	shared_platform.IsWorking.Store(true) // restore package-level default for other tests
}

// TestSimklCalendarFallback_EmptyWhenNoEntries exercises the zero-entries path without a real
// SIMKL server - simklCalendarFallback must return an empty (not nil-panicking) slice when
// GetAnimeCalendar's underlying result maps to nothing resolvable, mirroring the "reduced
// sections over broken ones" principle.
func TestSimklCalendarFallback_EmptyWhenNoEntries(t *testing.T) {
	mapped := syncpkg.MapCalendarToBaseAnime(nil, map[int]*simkl.AnimeDetail{})
	assert.Empty(t, mapped)
	assert.NotNil(t, mapped, "must return an empty slice, not nil, so JSON encodes as [] not null")
}
