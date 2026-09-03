package anilist_platform

import (
	"context"
	"seanime/internal/api/anilist"
	"seanime/internal/platforms/shared_platform"
	"seanime/internal/util"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gqlgo/gqlgenc/clientv2"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/singleflight"
)

// countingAnimeCollectionClient implements anilist.AnilistClient (embedding a nil interface so
// only AnimeCollection needs a real implementation - any other method would panic if called,
// which is fine since this test never exercises them). It counts calls and blocks on release so
// the test can force genuinely concurrent callers instead of racing serial ones.
type countingAnimeCollectionClient struct {
	anilist.AnilistClient
	calls   atomic.Int32
	release chan struct{}
}

func (c *countingAnimeCollectionClient) AnimeCollection(ctx context.Context, userName *string, interceptors ...clientv2.RequestInterceptor) (*anilist.AnimeCollection, error) {
	c.calls.Add(1)
	<-c.release
	return &anilist.AnimeCollection{
		MediaListCollection: &anilist.AnimeCollection_MediaListCollection{
			Lists: []*anilist.AnimeCollection_MediaListCollection_Lists{},
		},
	}, nil
}

func TestAnilistPlatform_GetAnimeCollection_CoalescesConcurrentNetworkFetches(t *testing.T) {
	client := &countingAnimeCollectionClient{release: make(chan struct{})}
	logger := util.NewLogger()

	ap := &AnilistPlatform{
		anilistClient:   client,
		logger:          logger,
		username:        mo.Some("testuser"),
		animeCollection: mo.None[*anilist.AnimeCollection](),
		helper:          &shared_platform.PlatformHelper{},
		refreshGroup:    &singleflight.Group{},
	}

	const concurrency = 10
	var wg sync.WaitGroup
	wg.Add(concurrency)
	errs := make([]error, concurrency)
	for i := 0; i < concurrency; i++ {
		go func(i int) {
			defer wg.Done()
			_, err := ap.GetAnimeCollection(context.Background(), false)
			errs[i] = err
		}(i)
	}

	// Wait for at least one goroutine to reach the network call before releasing it -
	// otherwise calls could finish serially without ever actually overlapping.
	require.Eventually(t, func() bool {
		return client.calls.Load() >= 1
	}, time.Second, time.Millisecond)

	close(client.release)
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}

	assert.EqualValues(t, 1, client.calls.Load(),
		"concurrent GetAnimeCollection calls on a cold cache should coalesce into a single network fetch")
}
