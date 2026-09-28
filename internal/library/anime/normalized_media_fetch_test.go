package anime

import (
	"context"
	"seanime/internal/api/anilist"
	"testing"

	"github.com/gqlgo/gqlgenc/clientv2"
	"github.com/stretchr/testify/require"
)

type countingCompleteAnimeClient struct {
	anilist.AnilistClient
	calls int
}

func (c *countingCompleteAnimeClient) CompleteAnimeByID(_ context.Context, id *int, _ ...clientv2.RequestInterceptor) (*anilist.CompleteAnimeByID, error) {
	c.calls++
	return &anilist.CompleteAnimeByID{Media: &anilist.CompleteAnime{ID: *id}}, nil
}

// A title the scan already holds must not be fetched again.
func TestFetchNormalizedMediaUsesScanCache(t *testing.T) {
	client := &countingCompleteAnimeClient{}
	cache := anilist.NewCompleteAnimeCache()
	cache.Set(1, &anilist.CompleteAnime{ID: 1})
	m := NewNormalizedMedia(&anilist.BaseAnime{ID: 1})

	require.NoError(t, FetchNormalizedMedia(context.Background(), client, cache, m))
	require.Zero(t, client.calls)
	require.Equal(t, 1, m.ID)
}

func TestNormalizedMediaFetchMediaTreeUsesScanCache(t *testing.T) {
	client := &countingCompleteAnimeClient{}
	cache := anilist.NewCompleteAnimeCache()
	cache.Set(1, &anilist.CompleteAnime{ID: 1})
	tree := anilist.NewCompleteAnimeRelationTree()

	require.NoError(t, NewNormalizedMedia(&anilist.BaseAnime{ID: 1}).FetchMediaTree(context.Background(), anilist.FetchMediaTreeAll, client, tree, cache))
	require.Zero(t, client.calls)
	require.True(t, tree.Has(1))
}
