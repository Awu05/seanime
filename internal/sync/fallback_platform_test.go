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
	inner := &fakePlatform{updateProgressErr: errors.New("anilist down")}
	simklClient := &fakeAllItemsClient{}
	queue := &fakeQueue{}
	fp := NewFallbackPlatform(inner, simklClient, queue, "profile-1",
		func() bool { return true },  // simklAvailable = true
		func() bool { return false }) // anilistHealthy = false

	err := fp.UpdateEntryProgress(context.Background(), 101922, 5, nil)
	require.NoError(t, err, "a successful SIMKL write must not surface the AniList error")

	assert.Equal(t, 1, simklClient.markProgressCalls, "must write live to SIMKL")
	require.Len(t, queue.enqueued, 1, "must queue the write for AniList to replay once healthy")
	assert.Equal(t, "anilist", queue.enqueued[0].Target)
	assert.Equal(t, "update_progress", queue.enqueued[0].Operation)
	assert.Equal(t, "profile-1", queue.enqueued[0].ProfileID)
}

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
