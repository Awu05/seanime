package scanner

import (
	"context"
	"seanime/internal/api/anilist"
	"seanime/internal/customsource"
	"seanime/internal/platforms/platform"
	"seanime/internal/util"
	"testing"

	"github.com/stretchr/testify/require"
)

type prefetchTestClient struct {
	anilist.AnilistClient
	batches [][]int
}

func (c *prefetchTestClient) CompleteAnimeByIDs(_ context.Context, ids []int) ([]*anilist.CompleteAnime, error) {
	c.batches = append(c.batches, ids)
	ret := make([]*anilist.CompleteAnime, len(ids))
	for i, id := range ids {
		ret[i] = &anilist.CompleteAnime{ID: id}
	}
	return ret, nil
}

type prefetchTestPlatform struct {
	platform.Platform
	client anilist.AnilistClient
}

func (p *prefetchTestPlatform) GetAnilistClient() anilist.AnilistClient {
	return p.client
}

// Matched titles the scan doesn't hold yet are fetched together, skipping custom-source IDs.
func TestPrefetchCompleteAnimeFetchesOnlyUncachedAniListIDs(t *testing.T) {
	client := &prefetchTestClient{}
	cache := anilist.NewCompleteAnimeCache()
	cache.Set(1, &anilist.CompleteAnime{ID: 1})
	fh := &FileHydrator{
		CompleteAnimeCache: cache,
		PlatformRef:        util.NewRef[platform.Platform](&prefetchTestPlatform{client: client}),
		Logger:             util.NewLogger(),
	}
	extensionID := int(customsource.ExtensionIdOffset) + 5

	fh.prefetchCompleteAnime(context.Background(), []int{1, 2, 3, extensionID})

	require.Len(t, client.batches, 1)
	require.ElementsMatch(t, []int{2, 3}, client.batches[0])
	_, ok := cache.Get(3)
	require.True(t, ok)
}

// Platforms without an AniList client (test fakes) must be skipped, as FetchNormalizedMedia does.
func TestPrefetchCompleteAnimeSkipsMissingClient(t *testing.T) {
	fh := &FileHydrator{
		CompleteAnimeCache: anilist.NewCompleteAnimeCache(),
		PlatformRef:        util.NewRef[platform.Platform](&prefetchTestPlatform{}),
		Logger:             util.NewLogger(),
	}

	require.NotPanics(t, func() { fh.prefetchCompleteAnime(context.Background(), []int{1}) })
}
