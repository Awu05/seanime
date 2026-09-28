package anilist

import (
	"context"
	"slices"
	"strings"
)

// completeAnimeBatchSize is the page size AniList serves for full records without exceeding its
// query complexity limit.
const completeAnimeBatchSize = 50

// CompleteAnimeByIDsDocument reuses the generated fragments so it stays in step with them.
var CompleteAnimeByIDsDocument = `query CompleteAnimeByIds ($ids: [Int]) {
	Page(perPage: 50) {
		media(id_in: $ids, type: ANIME) {
			... completeAnime
		}
	}
}
` + CompleteAnimeByIDDocument[strings.Index(CompleteAnimeByIDDocument, "fragment completeAnime"):]

type CompleteAnimeByIDs struct {
	Page *CompleteAnimeByIDs_Page "json:\"Page,omitempty\" graphql:\"Page\""
}

type CompleteAnimeByIDs_Page struct {
	Media []*CompleteAnime "json:\"media,omitempty\" graphql:\"media\""
}

func (t *CompleteAnimeByIDs) GetPage() *CompleteAnimeByIDs_Page {
	if t == nil {
		t = &CompleteAnimeByIDs{}
	}
	return t.Page
}

func (t *CompleteAnimeByIDs_Page) GetMedia() []*CompleteAnime {
	if t == nil {
		t = &CompleteAnimeByIDs_Page{}
	}
	return t.Media
}

func (ac *AnilistClientImpl) CompleteAnimeByIDs(ctx context.Context, ids []int) ([]*CompleteAnime, error) {
	ac.logger.Debug().Int("count", len(ids)).Msg("anilist: Fetching complete media batch")
	ret := make([]*CompleteAnime, 0, len(ids))
	for batch := range slices.Chunk(ids, completeAnimeBatchSize) {
		var res CompleteAnimeByIDs
		if err := ac.Client.Client.Post(ctx, "CompleteAnimeByIds", CompleteAnimeByIDsDocument, &res, map[string]any{"ids": batch}); err != nil {
			return ret, err
		}
		ret = append(ret, res.GetPage().GetMedia()...)
	}
	return ret, nil
}
