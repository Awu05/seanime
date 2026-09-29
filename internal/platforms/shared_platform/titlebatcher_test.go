package shared_platform

import (
	"context"
	"errors"
	"seanime/internal/api/anilist"
	"seanime/internal/events"
	"seanime/internal/util"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// lookUpAnime runs one BaseAnimeByID per id on its own goroutine, waits for all of them and returns
// their errors.
func lookUpAnime(ctx context.Context, cacheLayers []*CacheLayer, ids []int) []error {
	var mu sync.Mutex
	var errs []error
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Go(func() {
			if _, err := cacheLayers[i%len(cacheLayers)].BaseAnimeByID(ctx, &id); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return errs
}

// waitForBatches waits until client has received n batch requests.
func waitForBatches(t *testing.T, client *cacheLayerTestClient, n int) {
	t.Helper()
	require.Eventually(t, func() bool {
		client.mu.Lock()
		defer client.mu.Unlock()
		return len(client.baseAnimeBatches) == n
	}, time.Second, time.Millisecond)
}

func TestTitleLookupsForTheSameTitleShareOneRequest(t *testing.T) {
	installTestTitleCache(t)
	first := &cacheLayerTestClient{cacheDir: t.TempDir()}
	second := &cacheLayerTestClient{cacheDir: t.TempDir()}

	require.Empty(t, lookUpAnime(context.Background(), []*CacheLayer{newTestCacheLayer(t, first), newTestCacheLayer(t, second)}, []int{7, 7}))
	require.EqualValues(t, 1, atomic.LoadInt32(&first.baseAnimeCalls)+atomic.LoadInt32(&second.baseAnimeCalls))
}

// A token AniList rejects must not fail, or log out, the other profiles sharing its batch.
func TestTitleBatchRejectedLoginLeavesOtherProfilesAlone(t *testing.T) {
	previousEventManager := events.GlobalWSEventManager
	events.GlobalWSEventManager = &events.GlobalWSEventManagerWrapper{}
	t.Cleanup(func() { events.GlobalWSEventManager = previousEventManager })
	installTestTitleCache(t)
	revoked := &cacheLayerTestClient{cacheDir: t.TempDir(), baseAnimeErr: errors.New("graphql: Invalid token"), batchRelease: make(chan struct{})}
	other := &cacheLayerTestClient{cacheDir: t.TempDir()}
	otherLayer := newTestCacheLayer(t, other)
	loggedOut := make(chan struct{}, 1)
	otherLayer.logoutFunc = func() { loggedOut <- struct{}{} }
	id := 7

	revokedErr := make(chan error, 1)
	go func() {
		_, err := newTestCacheLayer(t, revoked).BaseAnimeByID(context.Background(), &id)
		revokedErr <- err
	}()
	waitForBatches(t, revoked, 1)
	otherErr := make(chan error, 1)
	go func() {
		_, err := otherLayer.BaseAnimeByID(context.Background(), &id)
		otherErr <- err
	}()
	time.Sleep(20 * time.Millisecond) // let the other lookup join the batch in flight
	close(revoked.batchRelease)

	require.ErrorContains(t, <-revokedErr, "Invalid token")
	require.NoError(t, <-otherErr, "the other profile looks the title up with its own login")
	select {
	case <-loggedOut:
		t.Fatal("another profile's rejected token must not log this one out")
	case <-time.After(100 * time.Millisecond):
	}
}

// One failed batch request counts once toward marking AniList down, however many lookups it served.
func TestTitleBatchFailureCountsOnce(t *testing.T) {
	previousEventManager := events.GlobalWSEventManager
	events.GlobalWSEventManager = &events.GlobalWSEventManagerWrapper{}
	t.Cleanup(func() { events.GlobalWSEventManager = previousEventManager })
	installTestTitleCache(t)
	client := &cacheLayerTestClient{cacheDir: t.TempDir(), baseAnimeErr: errors.New("500 Internal Server Error")}

	require.Len(t, lookUpAnime(context.Background(), []*CacheLayer{newTestCacheLayer(t, client)}, []int{1, 2, 3, 4, 5}), 5)
	require.Len(t, client.baseAnimeBatches, 1)
	require.Equal(t, 1, getRecentFailureCount())
	require.True(t, IsWorking.Load())
}

// Someone opening a title a scan is fetching must not wait in the scan's background lane.
func TestBrowsingLookupDoesNotWaitBehindBackgroundBatch(t *testing.T) {
	installTestTitleCache(t)
	scanning := &cacheLayerTestClient{cacheDir: t.TempDir(), batchRelease: make(chan struct{})}
	browsing := &cacheLayerTestClient{cacheDir: t.TempDir()}
	id := 5

	scanDone := make(chan []error, 1)
	go func() {
		scanDone <- lookUpAnime(anilist.WithBackgroundPriority(context.Background()), []*CacheLayer{newTestCacheLayer(t, scanning)}, []int{id})
	}()
	waitForBatches(t, scanning, 1)
	t.Cleanup(func() { close(scanning.batchRelease); <-scanDone })

	browsingDone := make(chan []error, 1)
	go func() {
		browsingDone <- lookUpAnime(context.Background(), []*CacheLayer{newTestCacheLayer(t, browsing)}, []int{id})
	}()
	select {
	case errs := <-browsingDone:
		require.Empty(t, errs)
	case <-time.After(time.Second):
		t.Fatal("the browsing lookup waited behind the background batch")
	}
	require.Len(t, browsing.batchContexts, 1)
	require.False(t, anilist.IsBackgroundPriority(browsing.batchContexts[0]))
}

func TestTitleLookupsWithinTheWindowShareOneBatch(t *testing.T) {
	installTestTitleCache(t)
	client := &cacheLayerTestClient{cacheDir: t.TempDir()}

	require.Empty(t, lookUpAnime(context.Background(), []*CacheLayer{newTestCacheLayer(t, client)}, []int{1, 2, 3}))
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
	waitForBatches(t, client, 1)
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

	require.Empty(t, lookUpAnime(background, cacheLayer, []int{11}))
	var wg sync.WaitGroup
	wg.Go(func() { lookUpAnime(background, cacheLayer, []int{12}) })
	wg.Go(func() { lookUpAnime(context.Background(), cacheLayer, []int{12}) })
	wg.Wait()

	require.Len(t, client.batchContexts, 2)
	require.True(t, anilist.IsBackgroundPriority(client.batchContexts[0]))
	require.Equal(t, "p1", util.ProfileIDFromContext(client.batchContexts[0]))
	require.False(t, anilist.IsBackgroundPriority(client.batchContexts[1]), "a browsing lookup makes the batch browsing")
}
