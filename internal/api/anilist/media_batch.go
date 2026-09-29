package anilist

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
)

// mediaBatchSize is the largest page AniList serves; full records at this size stay within its query
// complexity limit.
const mediaBatchSize = 50

// mediaByIDsDocument reuses a generated single-record document's fragments so it stays in step
// with them.
func mediaByIDsDocument(operation, mediaType, fragment, singleDocument string) string {
	return `query ` + operation + ` ($ids: [Int]) {
	Page(perPage: ` + strconv.Itoa(mediaBatchSize) + `) {
		media(id_in: $ids, type: ` + mediaType + `) {
			... ` + fragment + `
		}
	}
}
` + singleDocument[strings.Index(singleDocument, "fragment "+fragment+" on"):]
}

var (
	baseAnimeByIDsDocument     = mediaByIDsDocument("BaseAnimeByIds", "ANIME", "baseAnime", BaseAnimeByIDDocument)
	baseMangaByIDsDocument     = mediaByIDsDocument("BaseMangaByIds", "MANGA", "baseManga", BaseMangaByIDDocument)
	completeAnimeByIDsDocument = mediaByIDsDocument("CompleteAnimeByIds", "ANIME", "completeAnime", CompleteAnimeByIDDocument)
)

type mediaPage[M any] struct {
	Page *struct {
		Media []*M "json:\"media,omitempty\" graphql:\"media\""
	} "json:\"Page,omitempty\" graphql:\"Page\""
}

// mediaByIDs returns what AniList has for ids. A failed batch doesn't stop the rest; the error
// reports every failed batch.
func mediaByIDs[M any](ctx context.Context, ac *AnilistClientImpl, operation, document string, ids []int) ([]*M, error) {
	ret := make([]*M, 0, len(ids))
	var errs []error
	for batch := range slices.Chunk(ids, mediaBatchSize) {
		var res mediaPage[M]
		if err := ac.Client.Client.Post(ctx, operation, document, &res, map[string]any{"ids": batch}); err != nil {
			errs = append(errs, err)
			continue
		}
		if res.Page != nil {
			ret = append(ret, res.Page.Media...)
		}
	}
	return ret, errors.Join(errs...)
}

func (ac *AnilistClientImpl) BaseAnimeByIDs(ctx context.Context, ids []int) ([]*BaseAnime, error) {
	ac.logger.Debug().Int("count", len(ids)).Msg("anilist: Fetching anime batch")
	return mediaByIDs[BaseAnime](ctx, ac, "BaseAnimeByIds", baseAnimeByIDsDocument, ids)
}

func (ac *AnilistClientImpl) BaseMangaByIDs(ctx context.Context, ids []int) ([]*BaseManga, error) {
	ac.logger.Debug().Int("count", len(ids)).Msg("anilist: Fetching manga batch")
	return mediaByIDs[BaseManga](ctx, ac, "BaseMangaByIds", baseMangaByIDsDocument, ids)
}

func (ac *AnilistClientImpl) CompleteAnimeByIDs(ctx context.Context, ids []int) ([]*CompleteAnime, error) {
	ac.logger.Debug().Int("count", len(ids)).Msg("anilist: Fetching complete media batch")
	return mediaByIDs[CompleteAnime](ctx, ac, "CompleteAnimeByIds", completeAnimeByIDsDocument, ids)
}
