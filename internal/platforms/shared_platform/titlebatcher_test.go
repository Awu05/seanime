package shared_platform

import (
	"context"
	"seanime/internal/api/anilist"
	"seanime/internal/util"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// lookUpAnime runs one BaseAnimeByID per id on its own goroutine and waits for all of them.
func lookUpAnime(t *testing.T, ctx context.Context, cacheLayers []*CacheLayer, ids []int) {
	t.Helper()
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Go(func() {
			if _, err := cacheLayers[i%len(cacheLayers)].BaseAnimeByID(ctx, &id); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}

func TestTitleLookupsForTheSameTitleShareOneRequest(t *testing.T) {
	installTestTitleCache(t)
	first := &cacheLayerTestClient{cacheDir: t.TempDir()}
	second := &cacheLayerTestClient{cacheDir: t.TempDir()}

	lookUpAnime(t, context.Background(), []*CacheLayer{newTestCacheLayer(t, first), newTestCacheLayer(t, second)}, []int{7, 7})
	require.EqualValues(t, 1, atomic.LoadInt32(&first.baseAnimeCalls)+atomic.LoadInt32(&second.baseAnimeCalls))
}

func TestTitleLookupsWithinTheWindowShareOneBatch(t *testing.T) {
	installTestTitleCache(t)
	client := &cacheLayerTestClient{cacheDir: t.TempDir()}

	lookUpAnime(t, context.Background(), []*CacheLayer{newTestCacheLayer(t, client)}, []int{1, 2, 3})
	require.Len(t, client.baseAnimeBatches, 1)
	require.ElementsMatch(t, []int{1, 2, 3}, client.baseAnimeBatches[0])
}

func TestTitleMissingFromBatchGetsItsOwnLookup(t *testing.T) {
	tc := installTestTitleCache(t)
	client := &cacheLayerTestClient{cacheDir: t.TempDir(), baseAnimeMissing: map[int]bool{9: true}}
	id := 9

	res, err := newTestCacheLayer(t, client).BaseAnimeByID(context.Background(), &id)
	require.NoError(t, err)
	require.Equal(t, 9, res.GetMedia().ID)
	require.Equal(t, [][]int{{9}}, client.baseAnimeBatches)
	require.EqualValues(t, 2, atomic.LoadInt32(&client.baseAnimeCalls), "one batch, then the single lookup")
	_, _, ok := tc.GetAnime(9)
	require.True(t, ok)
}

func TestCancelledTitleLookupLeavesTheBatchRunning(t *testing.T) {
	tc := installTestTitleCache(t)
	client := &cacheLayerTestClient{cacheDir: t.TempDir(), batchRelease: make(chan struct{})}
	cacheLayer := newTestCacheLayer(t, client)
	ctx, cancel := context.WithCancel(context.Background())
	id := 4

	done := make(chan error, 1)
	go func() {
		_, err := cacheLayer.BaseAnimeByID(ctx, &id)
		done <- err
	}()
	require.Eventually(t, func() bool {
		client.mu.Lock()
		defer client.mu.Unlock()
		return len(client.baseAnimeBatches) == 1
	}, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)

	close(client.batchRelease)
	require.Eventually(t, func() bool {
		_, _, ok := tc.GetAnime(4)
		return ok
	}, time.Second, time.Millisecond, "the batch still fills the cache")
}

func TestTitleBatchUsesBrowsingLaneUnlessAllBackground(t *testing.T) {
	installTestTitleCache(t)
	client := &cacheLayerTestClient{cacheDir: t.TempDir()}
	cacheLayer := []*CacheLayer{newTestCacheLayer(t, client)}
	background := anilist.WithBackgroundPriority(util.ContextWithProfileID(context.Background(), "p1"))

	lookUpAnime(t, background, cacheLayer, []int{11})
	var wg sync.WaitGroup
	wg.Go(func() { lookUpAnime(t, background, cacheLayer, []int{12}) })
	wg.Go(func() { lookUpAnime(t, context.Background(), cacheLayer, []int{12}) })
	wg.Wait()

	require.Len(t, client.batchContexts, 2)
	require.True(t, anilist.IsBackgroundPriority(client.batchContexts[0]))
	require.Equal(t, "p1", util.ProfileIDFromContext(client.batchContexts[0]))
	require.False(t, anilist.IsBackgroundPriority(client.batchContexts[1]), "a browsing lookup makes the batch browsing")
}
