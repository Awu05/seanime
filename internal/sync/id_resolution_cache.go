package sync

import (
	"context"
	"strconv"
	"sync"
	"time"

	"seanime/internal/api/simkl"
)

// IDResolver looks up the full SIMKL detail record for a single SIMKL id - in practice this is
// *simkl.APIClient's GetAnimeDetails method itself (its signature already matches exactly, see
// Task 8), not a wrapper. Returning the whole AnimeDetail rather than just an AniList id lets
// Task 6/9's calendar mapping source title/year from the same call this cache already makes to
// resolve the id - the calendar feed itself has no title/poster (see Task 3).
type IDResolver func(ctx context.Context, simklID int) (*simkl.AnimeDetail, error)

type idResolutionEntry struct {
	detail   *simkl.AnimeDetail
	cachedAt time.Time
}

// IDResolutionCache resolves SIMKL ids to full AnimeDetail records (only entries with a valid
// numeric ids.anilist survive - see ResolveMany), bounded to maxConcurrency in-flight resolver
// calls at a time and caching successful resolutions for ttl (this crosswalk is static, so a long
// ttl - e.g. 24h - is appropriate, independent of any short list-freshness cache the caller
// applies separately). Failed/unresolved lookups are never cached, so a title that gains an
// AniList mapping later, or a transient SIMKL error, is retried next call.
//
// The resolver function is deliberately NOT stored on this struct - it is passed fresh to each
// ResolveMany call. This cache is shared process-wide (one instance serves every profile's
// requests), but the resolver a caller needs is request-scoped (bound to that profile's SIMKL
// client/client_id). Storing a mutable "current resolver" field would let one profile's
// in-flight resolution race onto a different profile's client the moment a second request swapped
// it mid-call. Only the cache entries are safe to share (that crosswalk isn't profile-specific).
type IDResolutionCache struct {
	maxConcurrency int
	ttl            time.Duration

	mu      sync.Mutex
	entries map[int]idResolutionEntry
}

func NewIDResolutionCache(maxConcurrency int, ttl time.Duration) *IDResolutionCache {
	return &IDResolutionCache{
		maxConcurrency: maxConcurrency,
		ttl:            ttl,
		entries:        make(map[int]idResolutionEntry),
	}
}

// ResolveMany resolves the first `cap` of simklIDs (in order) using `resolve`, bounding how many
// outbound calls one render can trigger during an outage. Returns a map of only the entries that
// both (a) resolved without error and (b) carry a valid, non-empty, numeric ids.anilist - callers
// drop anything missing from the result, matching the existing "no AniList id, nothing to
// represent it with" rule used elsewhere in this package. ids.anilist arrives as a JSON string
// (Task 3's live-verified FullIds shape), so validity is checked here via strconv.Atoi once,
// rather than repeating that parse in every caller.
func (c *IDResolutionCache) ResolveMany(ctx context.Context, resolve IDResolver, simklIDs []int, cap int) map[int]*simkl.AnimeDetail {
	if cap < len(simklIDs) {
		simklIDs = simklIDs[:cap]
	}

	result := make(map[int]*simkl.AnimeDetail, len(simklIDs))
	var resultMu sync.Mutex

	toResolve := make([]int, 0, len(simklIDs))
	now := time.Now()
	c.mu.Lock()
	for _, id := range simklIDs {
		if entry, ok := c.entries[id]; ok && now.Sub(entry.cachedAt) < c.ttl {
			result[id] = entry.detail
			continue
		}
		toResolve = append(toResolve, id)
	}
	c.mu.Unlock()

	sem := make(chan struct{}, c.maxConcurrency)
	var wg sync.WaitGroup
	for _, id := range toResolve {
		wg.Add(1)
		sem <- struct{}{}
		go func(simklID int) {
			defer wg.Done()
			defer func() { <-sem }()

			detail, err := resolve(ctx, simklID)
			if err != nil || detail == nil {
				return
			}
			if anilistID, convErr := strconv.Atoi(detail.Ids.Anilist); convErr != nil || anilistID <= 0 {
				return
			}

			resultMu.Lock()
			result[simklID] = detail
			resultMu.Unlock()

			c.mu.Lock()
			c.entries[simklID] = idResolutionEntry{detail: detail, cachedAt: time.Now()}
			c.mu.Unlock()
		}(id)
	}
	wg.Wait()

	return result
}
