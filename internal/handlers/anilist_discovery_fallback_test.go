package handlers

import (
	"testing"

	"seanime/internal/api/anilist"
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

// TestShouldForceSimklFallback exercises the manual "force SIMKL fallback" testing override
// (Settings > App > Metadata Providers) independently of shouldTrySimklDiscoveryFallback: it must
// engage regardless of whether AniList is actually healthy, but still require a configured
// client_id - there's nothing to force without one.
func TestShouldForceSimklFallback(t *testing.T) {
	t.Cleanup(func() { shared_platform.ForceSimklFallback.Store(false) })

	shared_platform.IsWorking.Store(true) // AniList is genuinely fine
	shared_platform.ForceSimklFallback.Store(false)
	assert.False(t, shouldForceSimklFallback("some-client-id"), "must not force when the override is off")

	shared_platform.ForceSimklFallback.Store(true)
	assert.False(t, shouldForceSimklFallback(""), "must not force with no client id configured")
	assert.True(t, shouldForceSimklFallback("some-client-id"), "must force even though AniList is healthy")
}

// TestIsUpcomingOnlyStatus covers the gate that routes Discover's "Coming Soon" section to
// GetUpcomingAnime instead of silently falling into the trending branch - the bug that motivated
// adding this function in the first place.
func TestIsUpcomingOnlyStatus(t *testing.T) {
	notYetReleased := anilist.MediaStatusNotYetReleased
	releasing := anilist.MediaStatusReleasing

	assert.True(t, isUpcomingOnlyStatus([]*anilist.MediaStatus{&notYetReleased}),
		"exactly what useDiscoverUpcomingAnime sends")
	assert.False(t, isUpcomingOnlyStatus(nil), "Trending's request has no status filter at all")
	assert.False(t, isUpcomingOnlyStatus([]*anilist.MediaStatus{}))
	assert.False(t, isUpcomingOnlyStatus([]*anilist.MediaStatus{&releasing}))
	assert.False(t, isUpcomingOnlyStatus([]*anilist.MediaStatus{&notYetReleased, &releasing}),
		"a broader multi-status query has no single SIMKL equivalent to fall back to")
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

// TestDedupeSimklIDs covers dedupeSimklIDs, which collapses the calendar feed's one-row-per-episode
// shape (a weekly show contributes ~5 rows across its 5-week window, all sharing a SimklID) down to
// one id per show before resolve slots are spent on it - order-preserving, first occurrence wins
// position, so downstream consumers see a stable ordering.
func TestDedupeSimklIDs(t *testing.T) {
	tests := []struct {
		name    string
		entries []simkl.CalendarEntry
		want    []int
	}{
		{
			name:    "empty input",
			entries: nil,
			want:    []int{},
		},
		{
			name: "no duplicates",
			entries: []simkl.CalendarEntry{
				{SimklID: 1}, {SimklID: 2}, {SimklID: 3},
			},
			want: []int{1, 2, 3},
		},
		{
			name: "consecutive duplicates",
			entries: []simkl.CalendarEntry{
				{SimklID: 5}, {SimklID: 5}, {SimklID: 5},
			},
			want: []int{5},
		},
		{
			name: "interleaved duplicates preserve first-seen order",
			entries: []simkl.CalendarEntry{
				{SimklID: 10}, {SimklID: 20}, {SimklID: 10}, {SimklID: 30}, {SimklID: 20}, {SimklID: 10},
			},
			want: []int{10, 20, 30},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dedupeSimklIDs(tt.entries)
			assert.Equal(t, tt.want, got)
		})
	}
}
