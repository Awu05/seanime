package handlers

import (
	"seanime/internal/api/anilist"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Bulk list results must reach the shared title cache so later single-title lookups skip AniList.
func TestSaveTitlesStoresBulkResults(t *testing.T) {
	h := newOfflineCopiesHandler(t, false)

	h.saveAnimeTitles(&anilist.BaseAnime{ID: 1})
	h.saveMangaTitles(&anilist.BaseManga{ID: 2})

	require.Eventually(t, func() bool {
		_, _, animeOK := h.App.TitleCache.GetAnime(1)
		_, _, mangaOK := h.App.TitleCache.GetManga(2)
		return animeOK && mangaOK
	}, 5*time.Second, 10*time.Millisecond)
}
