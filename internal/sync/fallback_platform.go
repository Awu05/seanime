package sync

import (
	"context"
	"seanime/internal/api/anilist"
	"seanime/internal/api/simkl"
	"seanime/internal/platforms/platform"
	"sync"
	"time"
)

// fallbackCacheTTL bounds how often one FallbackPlatform instance re-fetches SIMKL's full
// watchlist. SIMKL's own guidance is built around apps that keep a persistent, continuously
// synced local mirror (via /sync/activities + date_from) - this isn't that: FallbackPlatform
// only reads on demand, while AniList is actively down, so there's no ongoing mirror to keep
// incrementally in sync. A short TTL cache is the right-sized version of "don't hammer the API"
// for that access pattern - it bounds repeated full fetches during one outage (e.g. several page
// loads in a row) without the complexity of tracking removals/incremental merges for a case that
// wouldn't benefit from it.
const fallbackCacheTTL = 60 * time.Second

// FallbackPlatform is a READ-ONLY fallback decorator. It wraps platform.Platform (embedding it, so
// every method not overridden below - including every write method - passes through completely
// unchanged) and overrides exactly two collection reads, GetAnimeCollection and
// GetRawAnimeCollection, serving a SIMKL-built collection only when the wrapped read actually
// failed AND AniList is known to be down (shared_platform.IsWorking, passed in as anilistHealthy
// so tests don't need global state). It never guesses proactively - a healthy AniList is
// byte-for-byte today's behavior.
//
// It deliberately does NOT override the write path (UpdateEntryProgress / UpdateEntry /
// DeleteEntry). Those are already handled correctly, end to end, one and two layers below:
//   - shared_platform.CacheLayer, when IsWorking is false, does not error on a write - it queues
//     the write locally (a retry row replayed by its own ticker), patches the cached
//     AnimeCollection in place so the user's own UI reflects the edit immediately, and returns a
//     synthetic nil.
//   - MirroringPlatform sees that nil, correctly skips its own AniList-retry enqueue (CacheLayer's
//     queue already owns that write), and then unconditionally attempts its best-effort SIMKL
//     mirror - which IS the "write live to SIMKL during an outage" behavior.
//
// So during an AniList outage a write already reaches SIMKL live and AniList locally-queued, with
// the user's own UI patched. Intercepting writes here would bypass CacheLayer's queue-and-patch
// (making the user's edit invisible for the whole outage) and would open a second, independent
// retry queue that can race CacheLayer's and replay AniList writes out of order.
//
// Install this wrapping OUTSIDE MirroringPlatform (raw -> MirroringPlatform -> FallbackPlatform).
type FallbackPlatform struct {
	platform.Platform
	simklClient simkl.Client
	// queue and profileID are unused by the read-only fallback path; they are retained on the
	// struct and in NewFallbackPlatform's signature so the wiring in internal/core/simkl_wiring.go
	// stays unchanged.
	queue          PendingSyncEnqueuer
	profileID      string
	simklAvailable func() bool
	anilistHealthy func() bool

	cacheMu  sync.Mutex
	cached   *anilist.AnimeCollection
	cachedAt time.Time
}

func NewFallbackPlatform(inner platform.Platform, simklClient simkl.Client, queue PendingSyncEnqueuer, profileID string, simklAvailable func() bool, anilistHealthy func() bool) platform.Platform {
	return &FallbackPlatform{
		Platform:       inner,
		simklClient:    simklClient,
		queue:          queue,
		profileID:      profileID,
		simklAvailable: simklAvailable,
		anilistHealthy: anilistHealthy,
	}
}

// canFallback reports whether this call is eligible to fall back to SIMKL: AniList must be known
// down AND this profile must have SIMKL connected+enabled. A profile without SIMKL just gets
// today's real error - there is nothing to fall back to.
func (f *FallbackPlatform) canFallback() bool {
	return !f.anilistHealthy() && f.simklAvailable()
}

// GetAnimeCollection calls the wrapped platform FIRST: this preserves AniList's cache-preferring
// read behavior, only genuinely falling back to SIMKL when there's no usable cache AND the
// underlying call fails.
//
// Accepted, documented limitation: a SIMKL-built collection contains only entries SIMKL knows
// about, so custom-source entries (customsource.IsExtensionId media, which live in a local manager
// rather than on any tracker) do not appear in it for the duration of the outage.
func (f *FallbackPlatform) GetAnimeCollection(ctx context.Context, bypassCache bool) (*anilist.AnimeCollection, error) {
	collection, err := f.Platform.GetAnimeCollection(ctx, bypassCache)
	if err == nil || !f.canFallback() {
		return collection, err
	}
	simklCollection, simklErr := f.simklCollection(ctx)
	if simklErr != nil {
		return nil, err // surface the original AniList error, not the SIMKL one - AniList is what the caller asked for
	}
	return simklCollection, nil
}

// GetRawAnimeCollection has no "custom lists" concept on SIMKL - the fallback collection is
// identical to GetAnimeCollection's.
func (f *FallbackPlatform) GetRawAnimeCollection(ctx context.Context, bypassCache bool) (*anilist.AnimeCollection, error) {
	collection, err := f.Platform.GetRawAnimeCollection(ctx, bypassCache)
	if err == nil || !f.canFallback() {
		return collection, err
	}
	simklCollection, simklErr := f.simklCollection(ctx)
	if simklErr != nil {
		return nil, err
	}
	return simklCollection, nil
}

// simklCollection returns the SIMKL-built collection, serving it from cache when the last
// successful fetch is still within fallbackCacheTTL. Only successful fetches are cached - a
// transient SIMKL error is never remembered, so the very next call retries for real.
func (f *FallbackPlatform) simklCollection(ctx context.Context) (*anilist.AnimeCollection, error) {
	f.cacheMu.Lock()
	if f.cached != nil && time.Since(f.cachedAt) < fallbackCacheTTL {
		cached := f.cached
		f.cacheMu.Unlock()
		return cached, nil
	}
	f.cacheMu.Unlock()

	entries, err := f.simklClient.GetAllItems(ctx)
	if err != nil {
		return nil, err
	}
	collection := BuildAnimeCollectionFromSimkl(entries)

	f.cacheMu.Lock()
	f.cached = collection
	f.cachedAt = time.Now()
	f.cacheMu.Unlock()

	return collection, nil
}
