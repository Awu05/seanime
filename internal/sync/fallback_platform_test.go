package sync

import (
	"context"
	"errors"
	"seanime/internal/api/anilist"
	"seanime/internal/api/simkl"
	"seanime/internal/database/models"
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

// fakeFailingQueue wraps fakeQueue (mirror_platform_test.go) to let a test inject a durable-write
// failure on EnqueuePendingSync, without changing fakeQueue's shared zero-value behavior for
// every other test in the package.
type fakeFailingQueue struct {
	fakeQueue
	enqueueErr error
}

func (f *fakeFailingQueue) EnqueuePendingSync(item *models.PendingSync) error {
	if f.enqueueErr != nil {
		return f.enqueueErr
	}
	return f.fakeQueue.EnqueuePendingSync(item)
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

// --- UpdateEntryProgress ---

func TestFallbackPlatform_Healthy_PassesThrough(t *testing.T) {
	inner := &fakePlatform{}
	client := &fakeAllItemsClient{}
	fp := NewFallbackPlatform(inner, client, &fakeQueue{}, "_default",
		func() bool { return true }, func() bool { return true }) // anilistHealthy = true

	err := fp.UpdateEntryProgress(context.Background(), 101922, 5, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, inner.updateProgressCalls, "healthy AniList must go straight through")
	assert.Zero(t, client.getAllCalls, "must never touch SIMKL while healthy")
}

func TestFallbackPlatform_Unhealthy_NoSimkl_ErrorSurfaces(t *testing.T) {
	wantErr := errors.New("anilist down")
	inner := &fakePlatform{updateProgressErr: wantErr}
	fp := NewFallbackPlatform(inner, &fakeAllItemsClient{}, &fakeQueue{}, "_default",
		func() bool { return false }, // simklAvailable = false: not connected
		func() bool { return false }) // anilistHealthy = false

	err := fp.UpdateEntryProgress(context.Background(), 101922, 5, nil)
	require.ErrorIs(t, err, wantErr, "with no SIMKL fallback available, the real error must surface")
}

func TestFallbackPlatform_Unhealthy_WritesToSimklAndQueuesAnilist(t *testing.T) {
	inner := &fakePlatform{}
	simklClient := &fakeAllItemsClient{}
	queue := &fakeQueue{}
	fp := NewFallbackPlatform(inner, simklClient, queue, "profile-1",
		func() bool { return true },  // simklAvailable = true
		func() bool { return false }) // anilistHealthy = false

	err := fp.UpdateEntryProgress(context.Background(), 101922, 5, nil)
	require.NoError(t, err, "a successful SIMKL write must not surface an error")

	assert.Equal(t, 1, simklClient.markProgressCalls, "must write live to SIMKL")
	assert.Zero(t, inner.updateProgressCalls, "must not double-write: a successful SIMKL fallback must skip the wrapped AniList call entirely")
	require.Len(t, queue.enqueued, 1, "must queue the write for AniList to replay once healthy")
	assert.Equal(t, "anilist", queue.enqueued[0].Target)
	assert.Equal(t, "update_progress", queue.enqueued[0].Operation)
	assert.Equal(t, "profile-1", queue.enqueued[0].ProfileID)
}

func TestFallbackPlatform_UpdateEntryProgress_ZeroProgress_SkipsSimklFallsThrough(t *testing.T) {
	// MarkProgress(ctx, id, 0) would build an empty episode list - byte-identical to
	// RemoveEntry's request body - and would DELETE the SIMKL backup entry. A progress reset
	// must never redirect to SIMKL, even during an outage; it must fall through untouched.
	wantErr := errors.New("anilist down")
	inner := &fakePlatform{updateProgressErr: wantErr}
	simklClient := &fakeAllItemsClient{}
	queue := &fakeQueue{}
	fp := NewFallbackPlatform(inner, simklClient, queue, "_default",
		func() bool { return true }, func() bool { return false })

	err := fp.UpdateEntryProgress(context.Background(), 101922, 0, nil)
	require.ErrorIs(t, err, wantErr, "progress=0 must fall through to the wrapped platform's real error")

	assert.Zero(t, simklClient.markProgressCalls, "MarkProgress(0) would delete the SIMKL backup entry")
	assert.Equal(t, 1, inner.updateProgressCalls)
	assert.Empty(t, queue.enqueued)
}

func TestFallbackPlatform_UpdateEntryProgress_MangaMedia_SkipsSimklFallsThrough(t *testing.T) {
	wantErr := errors.New("anilist down")
	inner := &fakePlatform{updateProgressErr: wantErr}
	simklClient := &fakeAllItemsClient{}
	queue := &fakeQueue{}
	fp := NewFallbackPlatform(inner, simklClient, queue, "_default",
		func() bool { return true }, func() bool { return false })

	ctx := WithMangaMedia(context.Background())
	err := fp.UpdateEntryProgress(ctx, 101922, 5, nil)
	require.ErrorIs(t, err, wantErr, "manga must never be redirected to SIMKL's anime-only endpoints")

	assert.Zero(t, simklClient.markProgressCalls)
	assert.Equal(t, 1, inner.updateProgressCalls)
	assert.Empty(t, queue.enqueued)
}

func TestFallbackPlatform_UpdateEntryProgress_EnqueueFails_FallsThrough(t *testing.T) {
	wantErr := errors.New("anilist down")
	inner := &fakePlatform{updateProgressErr: wantErr}
	simklClient := &fakeAllItemsClient{}
	queue := &fakeFailingQueue{enqueueErr: errors.New("db locked")}
	fp := NewFallbackPlatform(inner, simklClient, queue, "_default",
		func() bool { return true }, func() bool { return false })

	err := fp.UpdateEntryProgress(context.Background(), 101922, 5, nil)
	require.ErrorIs(t, err, wantErr, "a failed durable enqueue must not be reported as success")

	assert.Equal(t, 1, simklClient.markProgressCalls, "the live SIMKL write itself did succeed")
	assert.Equal(t, 1, inner.updateProgressCalls, "must fall through to the wrapped platform since the catch-up row wasn't durable")
}

// --- UpdateEntry ---

func TestFallbackPlatform_UpdateEntry_Unhealthy_WritesToSimklAndQueuesAnilist(t *testing.T) {
	inner := &fakePlatform{}
	simklClient := &fakeAllItemsClient{}
	queue := &fakeQueue{}
	fp := NewFallbackPlatform(inner, simklClient, queue, "profile-1",
		func() bool { return true }, func() bool { return false })

	status := anilist.MediaListStatusCompleted
	score := 85
	err := fp.UpdateEntry(context.Background(), 101922, &status, &score, nil, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, simklClient.addToListCalls)
	assert.Equal(t, 1, simklClient.setRatingCalls)
	assert.Zero(t, inner.updateEntryCalls, "must not double-write: a successful SIMKL fallback must skip the wrapped AniList call entirely")
	require.Len(t, queue.enqueued, 1)
	assert.Equal(t, "update_entry", queue.enqueued[0].Operation)
}

func TestFallbackPlatform_UpdateEntry_PartialSimklFailure_FallsThrough(t *testing.T) {
	wantErr := errors.New("anilist down")
	inner := &fakePlatform{updateEntryErr: wantErr}
	simklClient := &fakeAllItemsClient{}
	simklClient.setRatingErr = errors.New("simkl rating failed")
	queue := &fakeQueue{}
	fp := NewFallbackPlatform(inner, simklClient, queue, "_default",
		func() bool { return true }, func() bool { return false })

	status := anilist.MediaListStatusCompleted
	score := 85
	err := fp.UpdateEntry(context.Background(), 101922, &status, &score, nil, nil, nil)
	require.ErrorIs(t, err, wantErr, "a partial SIMKL failure must fall through to the wrapped platform, not report false success")

	assert.Equal(t, 1, simklClient.addToListCalls, "the status write should still have been attempted")
	assert.Equal(t, 1, inner.updateEntryCalls, "must fall through to the wrapped platform on partial SIMKL failure")
	assert.Empty(t, queue.enqueued, "must not enqueue a catch-up row for a write that never fully succeeded on SIMKL")
}

func TestFallbackPlatform_UpdateEntry_MangaMedia_SkipsSimklFallsThrough(t *testing.T) {
	wantErr := errors.New("anilist down")
	inner := &fakePlatform{updateEntryErr: wantErr}
	simklClient := &fakeAllItemsClient{}
	queue := &fakeQueue{}
	fp := NewFallbackPlatform(inner, simklClient, queue, "_default",
		func() bool { return true }, func() bool { return false })

	status := anilist.MediaListStatusCompleted
	score := 85
	ctx := WithMangaMedia(context.Background())
	err := fp.UpdateEntry(ctx, 101922, &status, &score, nil, nil, nil)
	require.ErrorIs(t, err, wantErr)

	assert.Zero(t, simklClient.addToListCalls, "manga must never be redirected to SIMKL's anime-only endpoints")
	assert.Zero(t, simklClient.setRatingCalls)
	assert.Equal(t, 1, inner.updateEntryCalls)
	assert.Empty(t, queue.enqueued)
}

// --- DeleteEntry ---

func TestFallbackPlatform_DeleteEntry_Unhealthy_WritesToSimklAndQueuesAnilist(t *testing.T) {
	inner := &fakePlatform{}
	simklClient := &fakeAllItemsClient{}
	queue := &fakeQueue{}
	fp := NewFallbackPlatform(inner, simklClient, queue, "profile-1",
		func() bool { return true }, func() bool { return false })

	err := fp.DeleteEntry(context.Background(), 101922, 555)
	require.NoError(t, err)

	assert.Equal(t, 1, simklClient.removeEntryCalls)
	assert.Zero(t, inner.deleteEntryCalls, "must not double-write: a successful SIMKL fallback must skip the wrapped AniList call entirely")
	require.Len(t, queue.enqueued, 1)
	assert.Equal(t, "delete_entry", queue.enqueued[0].Operation)
}

func TestFallbackPlatform_DeleteEntry_SimklFails_FallsThrough(t *testing.T) {
	wantErr := errors.New("anilist down")
	inner := &fakePlatform{deleteEntryErr: wantErr}
	simklClient := &fakeAllItemsClient{}
	simklClient.removeEntryErr = errors.New("simkl remove failed")
	queue := &fakeQueue{}
	fp := NewFallbackPlatform(inner, simklClient, queue, "_default",
		func() bool { return true }, func() bool { return false })

	err := fp.DeleteEntry(context.Background(), 101922, 555)
	require.ErrorIs(t, err, wantErr, "a SIMKL failure must fall through to the wrapped platform, not report false success")

	assert.Equal(t, 1, inner.deleteEntryCalls)
	assert.Empty(t, queue.enqueued)
}

func TestFallbackPlatform_DeleteEntry_MangaMedia_SkipsSimklFallsThrough(t *testing.T) {
	wantErr := errors.New("anilist down")
	inner := &fakePlatform{deleteEntryErr: wantErr}
	simklClient := &fakeAllItemsClient{}
	queue := &fakeQueue{}
	fp := NewFallbackPlatform(inner, simklClient, queue, "_default",
		func() bool { return true }, func() bool { return false })

	ctx := WithMangaMedia(context.Background())
	err := fp.DeleteEntry(ctx, 101922, 555)
	require.ErrorIs(t, err, wantErr)

	assert.Zero(t, simklClient.removeEntryCalls, "manga must never be redirected to SIMKL's anime-only endpoints")
	assert.Equal(t, 1, inner.deleteEntryCalls)
	assert.Empty(t, queue.enqueued)
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
