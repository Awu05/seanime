package anilist

import (
	"context"
	"seanime/internal/util"
	"seanime/internal/util/result"
	"sync"

	"github.com/samber/lo"
)

type (
	CompleteAnimeRelationTree struct {
		*result.Map[int, *CompleteAnime]
	}

	FetchMediaTreeRelation = string
)

const (
	FetchMediaTreeSequels  FetchMediaTreeRelation = "sequels"
	FetchMediaTreePrequels FetchMediaTreeRelation = "prequels"
	FetchMediaTreeAll      FetchMediaTreeRelation = "all"
)

// NewCompleteAnimeRelationTree returns a new result.Map[int, *CompleteAnime].
// It is used to store the results of FetchMediaTree or FetchMediaTree calls.
func NewCompleteAnimeRelationTree() *CompleteAnimeRelationTree {
	return &CompleteAnimeRelationTree{result.NewMap[int, *CompleteAnime]()}
}

func (m *BaseAnime) FetchMediaTree(ctx context.Context, rel FetchMediaTreeRelation, anilistClient AnilistClient, tree *CompleteAnimeRelationTree, cache *CompleteAnimeCache) (err error) {
	if m == nil {
		return nil
	}

	defer util.HandlePanicInModuleWithError("anilist/BaseAnime.FetchMediaTree", &err)

	res, err := anilistClient.CompleteAnimeByID(ctx, &m.ID)
	if err != nil {
		return err
	}
	return res.GetMedia().FetchMediaTree(ctx, rel, anilistClient, tree, cache)
}

// FetchMediaTree populates the CompleteAnimeRelationTree with the given media's sequels and prequels.
// It also takes a CompleteAnimeCache to store the fetched media in and avoid duplicate fetches.
func (m *CompleteAnime) FetchMediaTree(ctx context.Context, rel FetchMediaTreeRelation, anilistClient AnilistClient, tree *CompleteAnimeRelationTree, cache *CompleteAnimeCache) (err error) {
	if m == nil {
		return nil
	}

	defer util.HandlePanicInModuleWithError("anilist/CompleteAnime.FetchMediaTree", &err)

	if tree.Has(m.ID) {
		cache.Set(m.ID, m)
		return nil
	}
	cache.Set(m.ID, m)
	tree.Set(m.ID, m)

	if m.Relations == nil {
		return nil
	}

	// Get all edges
	edges := m.GetRelations().GetEdges()
	// Filter edges
	edges = lo.Filter(edges, func(_edge *CompleteAnime_Relations_Edges, _ int) bool {
		return (*_edge.RelationType == MediaRelationSequel || *_edge.RelationType == MediaRelationPrequel) &&
			*_edge.GetNode().Status != MediaStatusNotYetReleased &&
			_edge.IsBroadRelationFormat() && !tree.Has(_edge.GetNode().ID)
	})

	if len(edges) == 0 {
		return nil
	}

	processEdges(ctx, edges, rel, anilistClient, tree, cache)
	return nil
}

// processEdges fetches the level's unknown nodes in one request, then walks each edge in parallel.
// processEdge still fetches a node on its own if the batch didn't return it.
func processEdges(ctx context.Context, edges []*CompleteAnime_Relations_Edges, rel FetchMediaTreeRelation, anilistClient AnilistClient, tree *CompleteAnimeRelationTree, cache *CompleteAnimeCache) {
	missing := make([]int, 0, len(edges))
	for _, edge := range edges {
		if _, ok := cache.Get(edge.GetNode().ID); !ok {
			missing = append(missing, edge.GetNode().ID)
		}
	}
	if len(missing) > 0 {
		media, _ := anilistClient.CompleteAnimeByIDs(ctx, missing)
		for _, m := range media {
			cache.Set(m.ID, m)
		}
	}

	var wg sync.WaitGroup
	for _, edge := range edges {
		wg.Go(func() { processEdge(ctx, edge, rel, anilistClient, tree, cache) })
	}
	wg.Wait()
}

func processEdge(ctx context.Context, edge *CompleteAnime_Relations_Edges, rel FetchMediaTreeRelation, anilistClient AnilistClient, tree *CompleteAnimeRelationTree, cache *CompleteAnimeCache) {
	defer util.HandlePanicInModuleThen("anilist/processEdge", func() {})
	cacheV, ok := cache.Get(edge.GetNode().ID)
	edgeCompleteAnime := cacheV
	if !ok {
		// Fetch the next node
		res, err := anilistClient.CompleteAnimeByID(ctx, &edge.GetNode().ID)
		if err == nil {
			edgeCompleteAnime = res.GetMedia()
			cache.Set(edgeCompleteAnime.ID, edgeCompleteAnime)
		}
	}
	if edgeCompleteAnime == nil {
		return
	}
	// Get the relation type to fetch for the next node
	edgeRel := getEdgeRelation(edge, rel)
	// Fetch the next node(s)
	err := edgeCompleteAnime.FetchMediaTree(ctx, edgeRel, anilistClient, tree, cache)
	if err != nil {
		return
	}
}

// getEdgeRelation returns the relation to fetch for the next node based on the current edge and the relation to fetch.
// If the relation to fetch is FetchMediaTreeAll, it will return FetchMediaTreePrequels for prequels and FetchMediaTreeSequels for sequels.
//
// For example, if the current node is a sequel and the relation to fetch is FetchMediaTreeAll, it will return FetchMediaTreeSequels so that
// only sequels are fetched for the next node.
func getEdgeRelation(edge *CompleteAnime_Relations_Edges, rel FetchMediaTreeRelation) FetchMediaTreeRelation {
	if rel == FetchMediaTreeAll {
		if *edge.RelationType == MediaRelationPrequel {
			return FetchMediaTreePrequels
		}
		if *edge.RelationType == MediaRelationSequel {
			return FetchMediaTreeSequels
		}
	}
	return rel
}
