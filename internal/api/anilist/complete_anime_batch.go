package anilist

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
)

// completeAnimeBatchSize is the page size AniList serves for full records without exceeding its
// query complexity limit.
const completeAnimeBatchSize = 50

// completeAnimeByIDsDocument reuses the generated fragments so it stays in step with them.
var completeAnimeByIDsDocument = `query CompleteAnimeByIds ($ids: [Int]) {
	Page(perPage: ` + strconv.Itoa(completeAnimeBatchSize) + `) {
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

// CompleteAnimeByIDs returns what AniList has for ids. A failed batch doesn't stop the rest; the
// error reports every failed batch.
func (ac *AnilistClientImpl) CompleteAnimeByIDs(ctx context.Context, ids []int) ([]*CompleteAnime, error) {
	ac.logger.Debug().Int("count", len(ids)).Msg("anilist: Fetching complete media batch")
	ret := make([]*CompleteAnime, 0, len(ids))
	var errs []error
	for batch := range slices.Chunk(ids, completeAnimeBatchSize) {
		var res CompleteAnimeByIDs
		if err := ac.Client.Client.Post(ctx, "CompleteAnimeByIds", completeAnimeByIDsDocument, &res, map[string]any{"ids": batch}); err != nil {
			errs = append(errs, err)
			continue
		}
		ret = append(ret, res.GetPage().GetMedia()...)
	}
	return ret, errors.Join(errs...)
}
