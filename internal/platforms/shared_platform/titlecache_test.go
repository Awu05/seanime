package shared_platform

import (
	"seanime/internal/api/anilist"
	"seanime/internal/util"
	"testing"
	"time"

	"github.com/goccy/go-json"
	"github.com/stretchr/testify/require"
)

func newTestTitleCache(t *testing.T) *TitleCache {
	t.Helper()
	tc, err := NewTitleCache(t.TempDir(), 1<<20, util.NewLogger())
	require.NoError(t, err)
	return tc
}

func TestTitleCacheRoundTrip(t *testing.T) {
	tc := newTestTitleCache(t)
	tc.PutAnime(&anilist.BaseAnime{ID: 1})
	tc.PutManga(&anilist.BaseManga{ID: 1})

	anime, fresh, ok := tc.GetAnime(1)
	require.True(t, ok)
	require.True(t, fresh)
	require.Equal(t, 1, anime.ID)

	manga, _, ok := tc.GetManga(1)
	require.True(t, ok)
	require.Equal(t, 1, manga.ID)

	// An anime record must never come back from a manga lookup.
	tc.PutAnime(&anilist.BaseAnime{ID: 2})
	_, _, ok = tc.GetManga(2)
	require.False(t, ok)
}

func TestTitleCacheFreshnessExpiresAfterADay(t *testing.T) {
	tc := newTestTitleCache(t)
	start := time.Now()
	tc.now = func() time.Time { return start }
	tc.PutAnime(&anilist.BaseAnime{ID: 1})

	tc.now = func() time.Time { return start.Add(longCacheTTL - time.Minute) }
	_, fresh, ok := tc.GetAnime(1)
	require.True(t, ok)
	require.True(t, fresh)

	tc.now = func() time.Time { return start.Add(longCacheTTL + time.Minute) }
	_, fresh, ok = tc.GetAnime(1)
	require.True(t, ok)
	require.False(t, fresh)
}

func TestTitleCacheSkipsNilAndZeroID(t *testing.T) {
	tc := newTestTitleCache(t)
	tc.PutAnime(nil, &anilist.BaseAnime{ID: 0})
	require.Zero(t, tc.Size())
}

// Bulk results can be old stored copies while AniList is down, so they must not be saved as fresh.
func TestTitleCacheSkipsSavesWhenAniListDownOrCachingDisabled(t *testing.T) {
	tc := newTestTitleCache(t)
	t.Cleanup(func() {
		IsWorking.Store(true)
		ShouldCache.Store(true)
	})

	IsWorking.Store(false)
	tc.PutAnime(&anilist.BaseAnime{ID: 1})
	IsWorking.Store(true)
	ShouldCache.Store(false)
	tc.PutAnime(&anilist.BaseAnime{ID: 2})

	require.Zero(t, tc.Size())
}

func listQueryBody(t *testing.T, query string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": query, "variables": map[string]any{"page": 1}})
	require.NoError(t, err)
	return body
}

func TestSaveListTitlesSavesSeanimeListQueries(t *testing.T) {
	tc := newTestTitleCache(t)

	saveListTitles(tc, listQueryBody(t, anilist.ListAnimeDocument), map[string]any{"Page": map[string]any{"media": []any{map[string]any{"id": 1}}}})
	saveListTitles(tc, listQueryBody(t, anilist.ListMangaDocument), map[string]any{"Page": map[string]any{"media": []any{map[string]any{"id": 2}}}})
	saveListTitles(tc, listQueryBody(t, anilist.ListRecentAiringAnimeQuery), map[string]any{"Page": map[string]any{"airingSchedules": []any{
		map[string]any{"episode": 1, "media": map[string]any{"id": 3}},
		map[string]any{"episode": 2, "media": map[string]any{"id": 3}},
	}}})

	_, _, ok := tc.GetAnime(1)
	require.True(t, ok)
	_, _, ok = tc.GetManga(2)
	require.True(t, ok)
	_, _, ok = tc.GetAnime(3)
	require.True(t, ok)
}

// A plugin query can share an operation name but select other fields, so only Seanime's own list
// documents are saved.
func TestSaveListTitlesIgnoresOtherQueries(t *testing.T) {
	tc := newTestTitleCache(t)

	saveListTitles(tc, listQueryBody(t, "query ListAnime { Page { media { id } } }"), map[string]any{"Page": map[string]any{"media": []any{map[string]any{"id": 1}}}})

	require.Zero(t, tc.Size())
}

func TestTitleCacheCorruptEntryIsAMiss(t *testing.T) {
	tc := newTestTitleCache(t)
	require.NoError(t, tc.store.Put(titleKey("anime", 1), []byte("not json")))
	_, _, ok := tc.GetAnime(1)
	require.False(t, ok)
}

func TestNilTitleCacheIsANoOp(t *testing.T) {
	var tc *TitleCache
	tc.PutAnime(&anilist.BaseAnime{ID: 1})
	_, _, ok := tc.GetAnime(1)
	require.False(t, ok)
	require.Zero(t, tc.Size())
}
