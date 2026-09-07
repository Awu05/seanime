package sync

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"seanime/internal/api/simkl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIDResolutionCache_ResolveMany_CapsAndResolves(t *testing.T) {
	var calls int64
	resolver := func(ctx context.Context, simklID int) (*simkl.AnimeDetail, error) {
		atomic.AddInt64(&calls, 1)
		if simklID%2 == 0 {
			// pretend even simklIDs have an anilist mapping - Ids.Anilist is a string, per
			// Task 3's live-verified FullIds shape.
			return &simkl.AnimeDetail{Title: "Even", Ids: simkl.FullIds{Anilist: fmtItoa(simklID * 1000)}}, nil
		}
		return &simkl.AnimeDetail{Ids: simkl.FullIds{Anilist: ""}}, nil // no mapping
	}
	cache := NewIDResolutionCache(4, time.Hour)

	simklIDs := make([]int, 50)
	for i := range simklIDs {
		simklIDs[i] = i + 1
	}

	result := cache.ResolveMany(context.Background(), resolver, simklIDs, 20)

	assert.LessOrEqual(t, calls, int64(20), "must not resolve more than the cap")
	require.Contains(t, result, 20)
	assert.Equal(t, "20000", result[20].Ids.Anilist)
	_, hasOdd := result[1]
	assert.False(t, hasOdd, "entries with no AniList mapping must be absent, not zero-valued")
}

func TestIDResolutionCache_CachesSuccessfulResolutions(t *testing.T) {
	var calls int64
	resolver := func(ctx context.Context, simklID int) (*simkl.AnimeDetail, error) {
		atomic.AddInt64(&calls, 1)
		return &simkl.AnimeDetail{Ids: simkl.FullIds{Anilist: fmtItoa(simklID * 1000)}}, nil
	}
	cache := NewIDResolutionCache(4, time.Hour)

	cache.ResolveMany(context.Background(), resolver, []int{1, 2, 3}, 10)
	cache.ResolveMany(context.Background(), resolver, []int{1, 2, 3}, 10)

	assert.Equal(t, int64(3), calls, "second call must be served entirely from cache")
}

func TestIDResolutionCache_DoesNotCacheFailures(t *testing.T) {
	var calls int64
	resolver := func(ctx context.Context, simklID int) (*simkl.AnimeDetail, error) {
		atomic.AddInt64(&calls, 1)
		return nil, errors.New("simkl unavailable") // never resolves
	}
	cache := NewIDResolutionCache(4, time.Hour)

	cache.ResolveMany(context.Background(), resolver, []int{1}, 10)
	cache.ResolveMany(context.Background(), resolver, []int{1}, 10)

	assert.Equal(t, int64(2), calls, "a failed resolution must be retried, not cached as absent")
}

func TestIDResolutionCache_DifferentResolversDoNotCrossContaminate(t *testing.T) {
	// Guards the reason `resolve` is a ResolveMany parameter, not a cache field: two "requests"
	// using the same shared cache but different resolvers (simulating two profiles' SIMKL
	// clients) must each get results from their OWN resolver, never the other's.
	resolverA := func(ctx context.Context, simklID int) (*simkl.AnimeDetail, error) {
		return &simkl.AnimeDetail{Title: "A", Ids: simkl.FullIds{Anilist: fmtItoa(simklID + 100)}}, nil
	}
	resolverB := func(ctx context.Context, simklID int) (*simkl.AnimeDetail, error) {
		return &simkl.AnimeDetail{Title: "B", Ids: simkl.FullIds{Anilist: fmtItoa(simklID + 200)}}, nil
	}
	cache := NewIDResolutionCache(4, time.Hour)

	resultA := cache.ResolveMany(context.Background(), resolverA, []int{1}, 10)
	resultB := cache.ResolveMany(context.Background(), resolverB, []int{2}, 10)

	assert.Equal(t, "101", resultA[1].Ids.Anilist)
	assert.Equal(t, "202", resultB[2].Ids.Anilist)
}

// fmtItoa is a tiny local helper so these tests don't need to import strconv just for one-line
// int-to-string conversions when building fixture AnimeDetail.Ids.Anilist values.
func fmtItoa(n int) string {
	return strconv.Itoa(n)
}
