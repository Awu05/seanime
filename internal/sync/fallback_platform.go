package sync

import (
	"context"
	"seanime/internal/api/anilist"
	"seanime/internal/api/simkl"
	"seanime/internal/platforms/platform"
	"seanime/internal/platforms/shared_platform"
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
// unchanged) and overrides three reads: the two collection reads, GetAnimeCollection and
// GetRawAnimeCollection (Component 1, gated on simklAvailable - a completed SIMKL OAuth
// connection, since these read the user's personal watchlist), and GetAnimeDetails (Component 4,
// gated on the lighter discoveryAvailable - just a configured client_id, since it only reads a
// public SIMKL endpoint). All three serve SIMKL-sourced data only when the wrapped read actually
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

	// discoveryAvailable and discoverySimklClient back GetAnimeDetails below (Component 4). They
	// are kept separate from simklAvailable/simklClient above: Component 1's tracking fallback
	// needs a completed SIMKL OAuth connection (it reads the user's personal watchlist), while
	// GetAnimeDetails only reads a public SIMKL endpoint and needs just a configured client_id -
	// see syncpkg.DiscoveryAvailable and the spec's "revised gating" note.
	discoveryAvailable   func() bool
	discoverySimklClient discoverySimklClient

	cacheMu  sync.Mutex
	cached   *anilist.AnimeCollection
	cachedAt time.Time
}

// discoverySimklClient is the narrow SIMKL surface GetAnimeDetails needs - kept separate from
// the existing simkl.Client interface (which covers Component 1's sync operations) since these
// two public-endpoint methods need no OAuth access token, unlike everything in simkl.Client.
type discoverySimklClient interface {
	SearchIDByAnilist(ctx context.Context, anilistID int) (simklID int, ok bool, err error)
	GetAnimeDetails(ctx context.Context, simklID int) (*simkl.AnimeDetail, error)
}

func NewFallbackPlatform(inner platform.Platform, simklClient simkl.Client, queue PendingSyncEnqueuer, profileID string, simklAvailable func() bool, anilistHealthy func() bool, discoveryAvailable func() bool, discoverySimklClient discoverySimklClient) platform.Platform {
	return &FallbackPlatform{
		Platform:             inner,
		simklClient:          simklClient,
		queue:                queue,
		profileID:            profileID,
		simklAvailable:       simklAvailable,
		anilistHealthy:       anilistHealthy,
		discoveryAvailable:   discoveryAvailable,
		discoverySimklClient: discoverySimklClient,
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

// GetAnimeDetails is Component 4's fallback: unlike the tracking methods above, this one is
// gated on discoveryAvailable (client_id present) rather than the stricter simklAvailable
// (completed OAuth connection) - see the spec's "revised gating" note. id here is an AniList id
// (this method's contract, inherited from platform.Platform), so SIMKL must be looked up in
// reverse via SearchIDByAnilist before its GetAnimeDetails(simklID) call can be made.
//
// shared_platform.ForceSimklFallback (a manual testing override, Settings > App > Metadata
// Providers) makes this engage even when the real call above succeeds - unlike every other gate
// here, forcing does NOT require err != nil. If the SIMKL side then fails for any reason, this
// still falls back to whatever the real platform returned (details, err) rather than losing a
// perfectly good result just because the forced test path didn't pan out.
func (f *FallbackPlatform) GetAnimeDetails(ctx context.Context, id int) (*anilist.AnimeDetailsById_Media, error) {
	details, err := f.Platform.GetAnimeDetails(ctx, id)
	forced := shared_platform.ForceSimklFallback.Load()
	shouldTryFallback := forced || (err != nil && !f.anilistHealthy())
	if !shouldTryFallback || !f.discoveryAvailable() || f.discoverySimklClient == nil {
		return details, err
	}

	simklID, ok, lookupErr := f.discoverySimklClient.SearchIDByAnilist(ctx, id)
	if lookupErr != nil || !ok {
		return details, err // fall back to whatever the real platform returned - a success, if forced
	}

	detail, detailErr := f.discoverySimklClient.GetAnimeDetails(ctx, simklID)
	if detailErr != nil {
		return details, err
	}

	return MapAnimeDetailToAnilist(id, detail), nil
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
