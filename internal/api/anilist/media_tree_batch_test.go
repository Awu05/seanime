package anilist

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/gqlgo/gqlgenc/clientv2"
	"github.com/stretchr/testify/require"
)

type treeTestClient struct {
	AnilistClient
	mu       sync.Mutex
	media    map[int]*CompleteAnime
	batchErr error
	batches  [][]int
	singles  int
}

func (c *treeTestClient) CompleteAnimeByID(_ context.Context, id *int, _ ...clientv2.RequestInterceptor) (*CompleteAnimeByID, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.singles++
	return &CompleteAnimeByID{Media: c.media[*id]}, nil
}

func (c *treeTestClient) CompleteAnimeByIDs(_ context.Context, ids []int) ([]*CompleteAnime, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.batches = append(c.batches, ids)
	if c.batchErr != nil {
		return nil, c.batchErr
	}
	ret := make([]*CompleteAnime, 0, len(ids))
	for _, id := range ids {
		ret = append(ret, c.media[id])
	}
	return ret, nil
}

// treeTestAnime returns a finished TV title with sequel edges to related.
func treeTestAnime(id int, related ...int) *CompleteAnime {
	edges := make([]*CompleteAnime_Relations_Edges, len(related))
	for i, r := range related {
		edges[i] = &CompleteAnime_Relations_Edges{
			RelationType: new(MediaRelationSequel),
			Node:         &BaseAnime{ID: r, Status: new(MediaStatusFinished), Format: new(MediaFormatTv)},
		}
	}
	return &CompleteAnime{ID: id, Relations: &CompleteAnime_Relations{Edges: edges}}
}

func TestFetchMediaTreeBatchesEachNodesRelations(t *testing.T) {
	client := &treeTestClient{media: map[int]*CompleteAnime{2: treeTestAnime(2, 4), 3: treeTestAnime(3), 4: treeTestAnime(4)}}
	tree := NewCompleteAnimeRelationTree()

	require.NoError(t, treeTestAnime(1, 2, 3).FetchMediaTree(context.Background(), FetchMediaTreeAll, client, tree, NewCompleteAnimeCache()))

	require.Len(t, client.batches, 2)
	require.ElementsMatch(t, []int{2, 3}, client.batches[0])
	require.Equal(t, []int{4}, client.batches[1])
	require.Zero(t, client.singles)
	require.ElementsMatch(t, []int{1, 2, 3, 4}, tree.Keys())
}

func TestBaseAnimeFetchMediaTreeUsesCache(t *testing.T) {
	client := &treeTestClient{}
	cache := NewCompleteAnimeCache()
	cache.Set(1, treeTestAnime(1))
	tree := NewCompleteAnimeRelationTree()

	require.NoError(t, (&BaseAnime{ID: 1}).FetchMediaTree(context.Background(), FetchMediaTreeAll, client, tree, cache))
	require.Zero(t, client.singles)
	require.True(t, tree.Has(1))
}

func TestFetchMediaTreeFallsBackToSingleLookupsWhenBatchFails(t *testing.T) {
	client := &treeTestClient{media: map[int]*CompleteAnime{2: treeTestAnime(2), 3: treeTestAnime(3)}, batchErr: errors.New("batch failed")}
	tree := NewCompleteAnimeRelationTree()

	require.NoError(t, treeTestAnime(1, 2, 3).FetchMediaTree(context.Background(), FetchMediaTreeAll, client, tree, NewCompleteAnimeCache()))

	require.Equal(t, 2, client.singles)
	require.ElementsMatch(t, []int{1, 2, 3}, tree.Keys())
}
