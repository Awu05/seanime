package sync

import (
	"context"
	"errors"
	"seanime/internal/api/anilist"
	"seanime/internal/api/simkl"
	"seanime/internal/platforms/shared_platform"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAllItemsClient struct {
	fakeSimklClient
	entries     []simkl.AllItemsEntry
	getAllErr   error
	getAllCalls int
}

func (f *fakeAllItemsClient) GetAllItems(ctx context.Context) ([]simkl.AllItemsEntry, error) {
	f.getAllCalls++
	if f.getAllErr != nil {
		return nil, f.getAllErr
	}
	return f.entries, nil
}

// fakeCollectionPlatform is a minimal platform.Platform for collection-read tests, separate from
// fakePlatform (mirror_platform_test.go) since that one has no GetAnimeCollection override.
type fakeCollectionPlatform struct {
	fakePlatform
	collection *anilist.AnimeCollection
	err        error
}

func (f *fakeCollectionPlatform) GetAnimeCollection(ctx context.Context, bypassCache bool) (*anilist.AnimeCollection, error) {
	return f.collection, f.err
}

func (f *fakeCollectionPlatform) GetRawAnimeCollection(ctx context.Context, bypassCache bool) (*anilist.AnimeCollection, error) {
	return f.collection, f.err
}

// --- Writes are NOT intercepted ---

func TestFallbackPlatform_Writes_PassThroughEvenWhenUnhealthy(t *testing.T) {
	// FallbackPlatform is a read-only decorator. The outage write path is already handled below
	// it: shared_platform.CacheLayer queues the AniList write locally and patches the cached
	// collection so the user's own UI updates immediately, and MirroringPlatform performs the live
	// SIMKL mirror. Intercepting writes here would bypass both and open a second retry queue that
	// can race CacheLayer's and replay AniList writes out of order.
	inner := &fakePlatform{}
	simklClient := &fakeAllItemsClient{}
	queue := &fakeQueue{}
	fp := NewFallbackPlatform(inner, simklClient, queue, "profile-1",
		func() bool { return true },   // simklAvailable = true
		func() bool { return false },  // anilistHealthy = false
		func() bool { return false }, nil) // discoveryAvailable / discoverySimklClient: unused by this test

	require.NoError(t, fp.UpdateEntryProgress(context.Background(), 101922, 5, nil))
	status := anilist.MediaListStatusCompleted
	score := 85
	require.NoError(t, fp.UpdateEntry(context.Background(), 101922, &status, &score, nil, nil, nil))
	require.NoError(t, fp.DeleteEntry(context.Background(), 101922, 555))

	assert.Equal(t, 1, inner.updateProgressCalls, "writes must pass straight through to the wrapped platform")
	assert.Equal(t, 1, inner.updateEntryCalls)
	assert.Equal(t, 1, inner.deleteEntryCalls)

	assert.Zero(t, simklClient.markProgressCalls, "FallbackPlatform must not write to SIMKL itself")
	assert.Zero(t, simklClient.addToListCalls)
	assert.Zero(t, simklClient.setRatingCalls)
	assert.Zero(t, simklClient.removeEntryCalls)
	assert.Empty(t, queue.enqueued, "FallbackPlatform must not open a second retry queue")
}

// --- GetAnimeCollection / GetRawAnimeCollection ---

func TestFallbackPlatform_GetAnimeCollection_Unhealthy_BuildsFromSimkl(t *testing.T) {
	inner := &fakeCollectionPlatform{err: errors.New("anilist down")}
	simklClient := &fakeAllItemsClient{entries: []simkl.AllItemsEntry{
		{Status: "watching", WatchedEpisodesCount: 3, TotalEpisodesCount: 24,
			Show: simkl.AllItemsShow{Title: "Fallback Anime", Ids: simkl.Ids{Anilist: "101922"}}},
	}}
	fp := NewFallbackPlatform(inner, simklClient, &fakeQueue{}, "_default",
		func() bool { return true }, func() bool { return false },
		func() bool { return false }, nil)

	collection, err := fp.GetAnimeCollection(context.Background(), false)
	require.NoError(t, err)
	require.NotNil(t, collection.MediaListCollection)

	var found bool
	for _, list := range collection.MediaListCollection.Lists {
		for _, e := range list.Entries {
			if e.GetMedia().GetID() == 101922 {
				found = true
			}
		}
	}
	assert.True(t, found, "SIMKL-sourced entry must appear in the fallback collection")
}

func TestFallbackPlatform_GetAnimeCollection_Healthy_NeverCallsSimkl(t *testing.T) {
	inner := &fakeCollectionPlatform{collection: &anilist.AnimeCollection{}}
	simklClient := &fakeAllItemsClient{}
	fp := NewFallbackPlatform(inner, simklClient, &fakeQueue{}, "_default",
		func() bool { return true }, func() bool { return true },
		func() bool { return false }, nil)

	_, err := fp.GetAnimeCollection(context.Background(), false)
	require.NoError(t, err)
	assert.Zero(t, simklClient.getAllCalls)
}

func TestFallbackPlatform_GetAnimeCollection_Unhealthy_NoSimkl_ErrorSurfaces(t *testing.T) {
	wantErr := errors.New("anilist down")
	inner := &fakeCollectionPlatform{err: wantErr}
	simklClient := &fakeAllItemsClient{}
	fp := NewFallbackPlatform(inner, simklClient, &fakeQueue{}, "_default",
		func() bool { return false }, // simklAvailable = false: not connected
		func() bool { return false }, // anilistHealthy = false
		func() bool { return false }, nil)

	_, err := fp.GetAnimeCollection(context.Background(), false)
	require.ErrorIs(t, err, wantErr, "with no SIMKL fallback available, the real error must surface")
	assert.Zero(t, simklClient.getAllCalls)
}

func TestFallbackPlatform_GetAnimeCollection_SimklAlsoFails_SurfacesAnilistError(t *testing.T) {
	wantErr := errors.New("anilist down")
	inner := &fakeCollectionPlatform{err: wantErr}
	simklClient := &fakeAllItemsClient{getAllErr: errors.New("simkl down too")}
	fp := NewFallbackPlatform(inner, simklClient, &fakeQueue{}, "_default",
		func() bool { return true }, func() bool { return false },
		func() bool { return false }, nil)

	_, err := fp.GetAnimeCollection(context.Background(), false)
	require.ErrorIs(t, err, wantErr, "the original AniList error is what the caller asked for")
}

func TestFallbackPlatform_GetRawAnimeCollection_Unhealthy_BuildsFromSimkl(t *testing.T) {
	inner := &fakeCollectionPlatform{err: errors.New("anilist down")}
	simklClient := &fakeAllItemsClient{entries: []simkl.AllItemsEntry{
		{Status: "watching", WatchedEpisodesCount: 3, TotalEpisodesCount: 24,
			Show: simkl.AllItemsShow{Title: "Fallback Anime", Ids: simkl.Ids{Anilist: "101922"}}},
	}}
	fp := NewFallbackPlatform(inner, simklClient, &fakeQueue{}, "_default",
		func() bool { return true }, func() bool { return false },
		func() bool { return false }, nil)

	collection, err := fp.GetRawAnimeCollection(context.Background(), false)
	require.NoError(t, err)
	require.NotNil(t, collection.MediaListCollection)

	var found bool
	for _, list := range collection.MediaListCollection.Lists {
		for _, e := range list.Entries {
			if e.GetMedia().GetID() == 101922 {
				found = true
			}
		}
	}
	assert.True(t, found, "SIMKL-sourced entry must appear in the fallback raw collection")
}

func TestFallbackPlatform_GetRawAnimeCollection_Healthy_NeverCallsSimkl(t *testing.T) {
	inner := &fakeCollectionPlatform{collection: &anilist.AnimeCollection{}}
	simklClient := &fakeAllItemsClient{}
	fp := NewFallbackPlatform(inner, simklClient, &fakeQueue{}, "_default",
		func() bool { return true }, func() bool { return true },
		func() bool { return false }, nil)

	_, err := fp.GetRawAnimeCollection(context.Background(), false)
	require.NoError(t, err)
	assert.Zero(t, simklClient.getAllCalls)
}

func TestFallbackPlatform_GetAnimeCollection_CachesWithinTTL(t *testing.T) {
	inner := &fakeCollectionPlatform{err: errors.New("anilist down")}
	simklClient := &fakeAllItemsClient{entries: []simkl.AllItemsEntry{
		{Status: "watching", WatchedEpisodesCount: 3, TotalEpisodesCount: 24,
			Show: simkl.AllItemsShow{Title: "Fallback Anime", Ids: simkl.Ids{Anilist: "101922"}}},
	}}
	fp := NewFallbackPlatform(inner, simklClient, &fakeQueue{}, "_default",
		func() bool { return true }, func() bool { return false },
		func() bool { return false }, nil)

	_, err := fp.GetAnimeCollection(context.Background(), false)
	require.NoError(t, err)
	// GetRawAnimeCollection shares the same cache as GetAnimeCollection - both serve the same
	// SIMKL-built data, so a second read through either method must be a cache hit too.
	_, err = fp.GetRawAnimeCollection(context.Background(), false)
	require.NoError(t, err)
	_, err = fp.GetAnimeCollection(context.Background(), false)
	require.NoError(t, err)

	assert.Equal(t, 1, simklClient.getAllCalls, "repeated reads within the TTL must not re-fetch from SIMKL")
}

func TestFallbackPlatform_GetAnimeCollection_RefetchesAfterTTLExpires(t *testing.T) {
	inner := &fakeCollectionPlatform{err: errors.New("anilist down")}
	simklClient := &fakeAllItemsClient{entries: []simkl.AllItemsEntry{
		{Status: "watching", WatchedEpisodesCount: 3, TotalEpisodesCount: 24,
			Show: simkl.AllItemsShow{Title: "Fallback Anime", Ids: simkl.Ids{Anilist: "101922"}}},
	}}
	fpAny := NewFallbackPlatform(inner, simklClient, &fakeQueue{}, "_default",
		func() bool { return true }, func() bool { return false },
		func() bool { return false }, nil)
	fp := fpAny.(*FallbackPlatform)

	_, err := fp.GetAnimeCollection(context.Background(), false)
	require.NoError(t, err)
	// Backdate the cache instead of sleeping the real TTL - this test only needs to prove a
	// stale cache entry is not served, not wait out fallbackCacheTTL in real time.
	fp.cacheMu.Lock()
	fp.cachedAt = time.Now().Add(-fallbackCacheTTL - time.Second)
	fp.cacheMu.Unlock()

	_, err = fp.GetAnimeCollection(context.Background(), false)
	require.NoError(t, err)

	assert.Equal(t, 2, simklClient.getAllCalls, "an expired cache entry must trigger a real refetch")
}

// --- GetAnimeDetails ---

// fakeDetailsSimklClient is a minimal discoverySimklClient stand-in for GetAnimeDetails tests.
type fakeDetailsSimklClient struct {
	simklID    int
	found      bool
	detail     *simkl.AnimeDetail
	searchErr  error
	detailsErr error
}

func (f *fakeDetailsSimklClient) SearchIDByAnilist(ctx context.Context, anilistID int) (int, bool, error) {
	return f.simklID, f.found, f.searchErr
}

func (f *fakeDetailsSimklClient) GetAnimeDetails(ctx context.Context, simklID int) (*simkl.AnimeDetail, error) {
	return f.detail, f.detailsErr
}

// fakeDetailsPlatform is a minimal platform.Platform stand-in for GetAnimeDetails tests, reusing
// fakePlatform's embedding-of-nil-interface pattern (mirror_platform_test.go) so only
// GetAnimeDetails needs overriding here.
type fakeDetailsPlatform struct {
	fakePlatform
	details *anilist.AnimeDetailsById_Media
	err     error
	called  bool
}

func (f *fakeDetailsPlatform) GetAnimeDetails(ctx context.Context, id int) (*anilist.AnimeDetailsById_Media, error) {
	f.called = true
	if f.err != nil {
		return nil, f.err
	}
	return f.details, nil
}

func TestFallbackPlatform_GetAnimeDetails_HealthyPassesThrough(t *testing.T) {
	inner := &fakeDetailsPlatform{details: &anilist.AnimeDetailsById_Media{ID: 1}}
	fp := &FallbackPlatform{
		Platform:           inner,
		anilistHealthy:     func() bool { return true },
		discoveryAvailable: func() bool { return true },
	}

	details, err := fp.GetAnimeDetails(context.Background(), 1)

	require.NoError(t, err)
	assert.Same(t, inner.details, details, "must pass through unchanged when AniList is healthy")
	assert.True(t, inner.called, "the wrapped platform's GetAnimeDetails must still be called first")
}

func TestFallbackPlatform_GetAnimeDetails_FallsBackWhenUnhealthy(t *testing.T) {
	inner := &fakeDetailsPlatform{err: errors.New("anilist down")}
	fp := &FallbackPlatform{
		Platform:           inner,
		anilistHealthy:     func() bool { return false },
		discoveryAvailable: func() bool { return true },
	}
	fp.discoverySimklClient = &fakeDetailsSimklClient{
		simklID: 46994,
		found:   true,
		detail:  &simkl.AnimeDetail{Title: "Frieren", Ids: simkl.FullIds{Anilist: "154587"}},
	}

	details, err := fp.GetAnimeDetails(context.Background(), 154587)

	require.NoError(t, err)
	require.NotNil(t, details)
	assert.Equal(t, 154587, details.ID)
}

func TestFallbackPlatform_GetAnimeDetails_SurfacesOriginalErrorIfSimklAlsoFails(t *testing.T) {
	inner := &fakeDetailsPlatform{err: errors.New("anilist down")}
	fp := &FallbackPlatform{
		Platform:           inner,
		anilistHealthy:     func() bool { return false },
		discoveryAvailable: func() bool { return true },
	}
	fp.discoverySimklClient = &fakeDetailsSimklClient{found: false}

	_, err := fp.GetAnimeDetails(context.Background(), 154587)

	require.Error(t, err)
	assert.Equal(t, "anilist down", err.Error(), "must surface the original AniList error, not a SIMKL-side one")
}

func TestFallbackPlatform_GetAnimeDetails_NotDiscoveryAvailable_ErrorSurfaces(t *testing.T) {
	inner := &fakeDetailsPlatform{err: errors.New("anilist down")}
	fp := &FallbackPlatform{
		Platform:           inner,
		anilistHealthy:     func() bool { return false },
		discoveryAvailable: func() bool { return false }, // no client_id configured
	}
	fp.discoverySimklClient = &fakeDetailsSimklClient{
		simklID: 46994,
		found:   true,
		detail:  &simkl.AnimeDetail{Title: "Frieren", Ids: simkl.FullIds{Anilist: "154587"}},
	}

	_, err := fp.GetAnimeDetails(context.Background(), 154587)

	require.Error(t, err)
	assert.Equal(t, "anilist down", err.Error(), "with discovery unavailable, SIMKL must never be consulted")
}

// --- GetAnimeDetails: manual "force SIMKL fallback" testing override ---

func TestFallbackPlatform_GetAnimeDetails_ForcedEngagesEvenWhenHealthy(t *testing.T) {
	shared_platform.ForceSimklFallback.Store(true)
	t.Cleanup(func() { shared_platform.ForceSimklFallback.Store(false) })

	inner := &fakeDetailsPlatform{details: &anilist.AnimeDetailsById_Media{ID: 1}}
	fp := &FallbackPlatform{
		Platform:           inner,
		anilistHealthy:     func() bool { return true }, // AniList is genuinely fine
		discoveryAvailable: func() bool { return true },
		discoverySimklClient: &fakeDetailsSimklClient{
			simklID: 46994,
			found:   true,
			detail:  &simkl.AnimeDetail{Title: "Frieren", Ids: simkl.FullIds{Anilist: "154587"}},
		},
	}

	details, err := fp.GetAnimeDetails(context.Background(), 154587)

	require.NoError(t, err)
	require.NotNil(t, details)
	assert.True(t, inner.called, "the real platform is still called first even when forced")
	assert.Equal(t, 154587, details.ID, "must be the SIMKL-derived result, not the real platform's healthy one")
}

func TestFallbackPlatform_GetAnimeDetails_ForcedButSimklFailsFallsBackToRealResult(t *testing.T) {
	shared_platform.ForceSimklFallback.Store(true)
	t.Cleanup(func() { shared_platform.ForceSimklFallback.Store(false) })

	inner := &fakeDetailsPlatform{details: &anilist.AnimeDetailsById_Media{ID: 1}}
	fp := &FallbackPlatform{
		Platform:             inner,
		anilistHealthy:       func() bool { return true },
		discoveryAvailable:   func() bool { return true },
		discoverySimklClient: &fakeDetailsSimklClient{found: false}, // SIMKL has no crosswalk for this id
	}

	details, err := fp.GetAnimeDetails(context.Background(), 1)

	require.NoError(t, err, "the real call succeeded - a failed forced SIMKL attempt must not turn that into an error")
	assert.Same(t, inner.details, details, "must fall back to the real platform's successful result, not nil")
}

func TestFallbackPlatform_GetAnimeDetails_ForcedButNotDiscoveryAvailable_RealResultPassesThrough(t *testing.T) {
	shared_platform.ForceSimklFallback.Store(true)
	t.Cleanup(func() { shared_platform.ForceSimklFallback.Store(false) })

	inner := &fakeDetailsPlatform{details: &anilist.AnimeDetailsById_Media{ID: 1}}
	fp := &FallbackPlatform{
		Platform:           inner,
		anilistHealthy:     func() bool { return true },
		discoveryAvailable: func() bool { return false }, // no client_id configured
	}

	details, err := fp.GetAnimeDetails(context.Background(), 1)

	require.NoError(t, err)
	assert.Same(t, inner.details, details, "forcing must still respect discoveryAvailable - no client_id, nothing to force")
}

// --- RawPlatform must unwrap both wrapper layers ---

func TestFallbackPlatform_RawPlatform_UnwrapsBothLayers(t *testing.T) {
	// Wrapping order is raw -> MirroringPlatform -> FallbackPlatform. The retry Worker delivers
	// queued rows through RawPlatform specifically to avoid re-triggering either wrapper's
	// interception, so RawPlatform must strip both layers, not just the outermost one.
	inner := &fakePlatform{}
	mp := NewMirroringPlatform(inner, &fakeSimklClient{}, &fakeQueue{}, "_default", func() bool { return true })
	fp := NewFallbackPlatform(mp, &fakeAllItemsClient{}, &fakeQueue{}, "_default",
		func() bool { return true }, func() bool { return true },
		func() bool { return false }, nil)

	assert.Same(t, inner, RawPlatform(fp), "RawPlatform must unwrap both MirroringPlatform and FallbackPlatform")
}
