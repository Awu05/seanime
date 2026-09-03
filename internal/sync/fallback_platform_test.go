package sync

import (
	"context"
	"errors"
	"seanime/internal/api/anilist"
	"seanime/internal/api/simkl"
	"testing"

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
		func() bool { return true },  // simklAvailable = true
		func() bool { return false }) // anilistHealthy = false

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
		func() bool { return true }, func() bool { return false })

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
		func() bool { return true }, func() bool { return true })

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
		func() bool { return false }) // anilistHealthy = false

	_, err := fp.GetAnimeCollection(context.Background(), false)
	require.ErrorIs(t, err, wantErr, "with no SIMKL fallback available, the real error must surface")
	assert.Zero(t, simklClient.getAllCalls)
}

func TestFallbackPlatform_GetAnimeCollection_SimklAlsoFails_SurfacesAnilistError(t *testing.T) {
	wantErr := errors.New("anilist down")
	inner := &fakeCollectionPlatform{err: wantErr}
	simklClient := &fakeAllItemsClient{getAllErr: errors.New("simkl down too")}
	fp := NewFallbackPlatform(inner, simklClient, &fakeQueue{}, "_default",
		func() bool { return true }, func() bool { return false })

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
		func() bool { return true }, func() bool { return false })

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
		func() bool { return true }, func() bool { return true })

	_, err := fp.GetRawAnimeCollection(context.Background(), false)
	require.NoError(t, err)
	assert.Zero(t, simklClient.getAllCalls)
}

// --- RawPlatform must unwrap both wrapper layers ---

func TestFallbackPlatform_RawPlatform_UnwrapsBothLayers(t *testing.T) {
	// Wrapping order is raw -> MirroringPlatform -> FallbackPlatform. The retry Worker delivers
	// queued rows through RawPlatform specifically to avoid re-triggering either wrapper's
	// interception, so RawPlatform must strip both layers, not just the outermost one.
	inner := &fakePlatform{}
	mp := NewMirroringPlatform(inner, &fakeSimklClient{}, &fakeQueue{}, "_default", func() bool { return true })
	fp := NewFallbackPlatform(mp, &fakeAllItemsClient{}, &fakeQueue{}, "_default",
		func() bool { return true }, func() bool { return true })

	assert.Same(t, inner, RawPlatform(fp), "RawPlatform must unwrap both MirroringPlatform and FallbackPlatform")
}
