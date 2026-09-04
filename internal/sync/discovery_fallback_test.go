package sync

import (
	"testing"

	"seanime/internal/api/simkl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapSearchResultsToBaseAnime(t *testing.T) {
	results := []simkl.SearchResult{
		{Title: "Frieren", Year: 2023, Poster: "ab/abc123", Ids: simkl.DiscoveryIds{SimklID: 1990194}},
		{Title: "No AniList Mapping", Year: 2020, Ids: simkl.DiscoveryIds{SimklID: 999}},
	}
	resolved := map[int]*simkl.AnimeDetail{
		1990194: {Ids: simkl.FullIds{Anilist: "154587"}},
	} // 999 deliberately absent - unresolved

	mapped := MapSearchResultsToBaseAnime(results, resolved)

	require.Len(t, mapped, 1, "unresolved entries must be dropped, not included with a zero id")
	assert.Equal(t, 154587, mapped[0].ID)
	assert.Equal(t, "Frieren", *mapped[0].Title.Romaji)
	assert.Equal(t, 2023, *mapped[0].SeasonYear)
}

func TestMapTrendingToBaseAnime(t *testing.T) {
	entries := []simkl.TrendingEntry{
		{Title: "Sample Anime", Year: 2024, Poster: "cd/cde456", Ids: simkl.DiscoveryIds{SimklID: 123}},
	}
	resolved := map[int]*simkl.AnimeDetail{123: {Ids: simkl.FullIds{Anilist: "200000"}}}

	mapped := MapTrendingToBaseAnime(entries, resolved)

	require.Len(t, mapped, 1)
	assert.Equal(t, 200000, mapped[0].ID)
	assert.Equal(t, "Sample Anime", *mapped[0].Title.Romaji)
}

func TestMapCalendarToBaseAnime(t *testing.T) {
	entries := []simkl.CalendarEntry{
		{SimklID: 500, Date: "2026-09-04T12:00:00Z"},
		{SimklID: 501, Date: "2026-09-05T12:00:00Z"},
	}
	resolved := map[int]*simkl.AnimeDetail{
		500: {Title: "Airing Now", Year: 2024, Ids: simkl.FullIds{Anilist: "300000"}},
	} // 501 deliberately absent - unresolved

	mapped := MapCalendarToBaseAnime(entries, resolved)

	require.Len(t, mapped, 1)
	assert.Equal(t, 300000, mapped[0].ID)
	assert.Equal(t, "Airing Now", *mapped[0].Title.Romaji)
}

func TestDiscoveryAvailable(t *testing.T) {
	assert.True(t, DiscoveryAvailable("some-client-id"))
	assert.False(t, DiscoveryAvailable(""))
}
