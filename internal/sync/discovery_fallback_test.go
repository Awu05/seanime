package sync

import (
	"testing"
	"time"

	"seanime/internal/api/anilist"
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
	assert.Equal(t, "Frieren", *mapped[0].Title.UserPreferred, "frontend cards read title.userPreferred, not title.romaji")
	assert.Equal(t, "Frieren", *mapped[0].Title.English)
	assert.Equal(t, 2023, *mapped[0].SeasonYear)
	require.NotNil(t, mapped[0].IsAdult)
	assert.False(t, *mapped[0].IsAdult, "schedule containers filter on isAdult === false; nil fails that check in JS")
	require.NotNil(t, mapped[0].Type)
	assert.Equal(t, anilist.MediaTypeAnime, *mapped[0].Type)
	require.NotNil(t, mapped[0].CountryOfOrigin)
	assert.Equal(t, "JP", *mapped[0].CountryOfOrigin)
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
	assert.Equal(t, "Sample Anime", *mapped[0].Title.UserPreferred, "frontend cards read title.userPreferred, not title.romaji")
	assert.Equal(t, "Sample Anime", *mapped[0].Title.English)
	require.NotNil(t, mapped[0].IsAdult)
	assert.False(t, *mapped[0].IsAdult)
	require.NotNil(t, mapped[0].Type)
	assert.Equal(t, anilist.MediaTypeAnime, *mapped[0].Type)
	require.NotNil(t, mapped[0].CountryOfOrigin)
	assert.Equal(t, "JP", *mapped[0].CountryOfOrigin)
}

func TestMapCalendarToBaseAnime(t *testing.T) {
	entries := []simkl.CalendarEntry{
		{SimklID: 500, Date: "2026-09-04T12:00:00Z"},
		{SimklID: 501, Date: "2026-09-05T12:00:00Z"},
	}
	resolved := map[int]*simkl.AnimeDetail{
		500: {Title: "Airing Now", Year: 2024, Poster: "14/14625673bbdc6b52ea", Ids: simkl.FullIds{Anilist: "300000"}},
	} // 501 deliberately absent - unresolved

	mapped := MapCalendarToBaseAnime(entries, resolved)

	require.Len(t, mapped, 1)
	assert.Equal(t, 300000, mapped[0].ID)
	assert.Equal(t, "Airing Now", *mapped[0].Title.Romaji)
	assert.Equal(t, "Airing Now", *mapped[0].Title.UserPreferred, "frontend cards read title.userPreferred, not title.romaji")
	assert.Equal(t, "Airing Now", *mapped[0].Title.English)
	require.NotNil(t, mapped[0].IsAdult)
	assert.False(t, *mapped[0].IsAdult)
	require.NotNil(t, mapped[0].Type)
	assert.Equal(t, anilist.MediaTypeAnime, *mapped[0].Type)
	require.NotNil(t, mapped[0].CountryOfOrigin)
	assert.Equal(t, "JP", *mapped[0].CountryOfOrigin)
	require.NotNil(t, mapped[0].CoverImage, "CalendarEntry itself has no poster - this must come from the resolved AnimeDetail's Poster field")
	assert.Contains(t, *mapped[0].CoverImage.Large, "14/14625673bbdc6b52ea")
}

// TestMapCalendarToBaseAnime_DedupesByResolvedAnilistID covers the case where the calendar feed's
// per-episode rows (distinct SimklIDs) resolve to the same AniList show - e.g. dedup upstream in
// dedupeSimklIDs missed it, or two different SIMKL ids happen to map to the same AniList entry.
// The "This Season" tab renders this output with key={media.id}, so duplicate ids would either
// break React's reconciliation or silently render the same card twice.
func TestMapCalendarToBaseAnime_DedupesByResolvedAnilistID(t *testing.T) {
	entries := []simkl.CalendarEntry{
		{SimklID: 600, Date: "2026-09-04T12:00:00Z"},
		{SimklID: 601, Date: "2026-09-05T12:00:00Z"},
	}
	resolved := map[int]*simkl.AnimeDetail{
		600: {Title: "Same Show", Year: 2024, Ids: simkl.FullIds{Anilist: "400000"}},
		601: {Title: "Same Show", Year: 2024, Ids: simkl.FullIds{Anilist: "400000"}}, // different SimklID, same AniList id
	}

	mapped := MapCalendarToBaseAnime(entries, resolved)

	require.Len(t, mapped, 1, "two SimklIDs resolving to the same AniList id must produce exactly one BaseAnime")
	assert.Equal(t, 400000, mapped[0].ID)
}

func TestMapCalendarToAiringSchedules(t *testing.T) {
	entries := []simkl.CalendarEntry{
		{SimklID: 500, Date: "2026-09-04T12:00:00Z", Episode: simkl.CalendarEpisode{Episode: 5}},
		{SimklID: 501, Date: "2026-09-05T12:00:00Z", Episode: simkl.CalendarEpisode{Episode: 1}}, // unresolved
		{SimklID: 502, Date: "not-a-real-date", Episode: simkl.CalendarEpisode{Episode: 2}},      // unparseable date
	}
	resolved := map[int]*simkl.AnimeDetail{
		500: {Title: "Airing Now", Year: 2024, Poster: "14/14625673bbdc6b52ea", Ids: simkl.FullIds{Anilist: "300000"}},
		502: {Title: "Bad Date", Year: 2024, Ids: simkl.FullIds{Anilist: "300002"}},
	} // 501 deliberately absent - unresolved

	mapped := MapCalendarToAiringSchedules(entries, resolved)

	require.Len(t, mapped, 1, "unresolved and unparseable-date entries must be dropped")
	assert.Equal(t, 300000, mapped[0].Media.ID)
	assert.Equal(t, "Airing Now", *mapped[0].Media.Title.Romaji)
	assert.Equal(t, "Airing Now", *mapped[0].Media.Title.UserPreferred, "schedule containers filter/read title.userPreferred, not title.romaji")
	assert.Equal(t, "Airing Now", *mapped[0].Media.Title.English)
	require.NotNil(t, mapped[0].Media.IsAdult)
	assert.False(t, *mapped[0].Media.IsAdult, "schedule containers filter on isAdult === false && type === ANIME && countryOfOrigin === JP")
	require.NotNil(t, mapped[0].Media.Type)
	assert.Equal(t, anilist.MediaTypeAnime, *mapped[0].Media.Type)
	require.NotNil(t, mapped[0].Media.CountryOfOrigin)
	assert.Equal(t, "JP", *mapped[0].Media.CountryOfOrigin)
	require.NotNil(t, mapped[0].Media.CoverImage, "CalendarEntry itself has no poster - this must come from the resolved AnimeDetail's Poster field")
	assert.Contains(t, *mapped[0].Media.CoverImage.Large, "14/14625673bbdc6b52ea")
	assert.Equal(t, 5, mapped[0].Episode)
	assert.Equal(t, 500, mapped[0].ID)
	expectedAiredAt, err := time.Parse(time.RFC3339, "2026-09-04T12:00:00Z")
	require.NoError(t, err)
	assert.Equal(t, int(expectedAiredAt.Unix()), mapped[0].AiringAt)
}

func TestMapAnimeDetailToAnilist(t *testing.T) {
	detail := &simkl.AnimeDetail{
		Title:         "Frieren",
		Overview:      "A story about elves.",
		Genres:        []string{"Adventure", "Drama"},
		TotalEpisodes: 28,
		Ids:           simkl.FullIds{Simkl: 1990194, Anilist: "154587"},
	}

	mapped := MapAnimeDetailToAnilist(154587, detail)

	require.NotNil(t, mapped)
	assert.Equal(t, 154587, mapped.ID)
	assert.Equal(t, "A story about elves.", *mapped.Description)
	require.Len(t, mapped.Genres, 2)
	assert.Equal(t, "Adventure", *mapped.Genres[0])
}

func TestDiscoveryAvailable(t *testing.T) {
	assert.True(t, DiscoveryAvailable("some-client-id"))
	assert.False(t, DiscoveryAvailable(""))
}

func TestFilterAiringSchedulesByWindow(t *testing.T) {
	mkSchedule := func(airingAt int) *anilist.ListRecentAnime_Page_AiringSchedules {
		return &anilist.ListRecentAnime_Page_AiringSchedules{AiringAt: airingAt}
	}
	schedules := []*anilist.ListRecentAnime_Page_AiringSchedules{
		mkSchedule(100),
		mkSchedule(200),
		mkSchedule(300),
	}

	t.Run("both bounds nil returns input unchanged", func(t *testing.T) {
		filtered := FilterAiringSchedulesByWindow(schedules, nil, nil)
		assert.Equal(t, schedules, filtered)
	})

	t.Run("only lower bound drops entries with AiringAt < bound", func(t *testing.T) {
		lower := 150
		filtered := FilterAiringSchedulesByWindow(schedules, &lower, nil)
		require.Len(t, filtered, 2)
		assert.Equal(t, 200, filtered[0].AiringAt)
		assert.Equal(t, 300, filtered[1].AiringAt)
	})

	t.Run("only upper bound drops entries with AiringAt > bound", func(t *testing.T) {
		upper := 250
		filtered := FilterAiringSchedulesByWindow(schedules, nil, &upper)
		require.Len(t, filtered, 2)
		assert.Equal(t, 100, filtered[0].AiringAt)
		assert.Equal(t, 200, filtered[1].AiringAt)
	})

	t.Run("both bounds keeps only entries within the inclusive range", func(t *testing.T) {
		lower, upper := 100, 200
		filtered := FilterAiringSchedulesByWindow(schedules, &lower, &upper)
		require.Len(t, filtered, 2)
		assert.Equal(t, 100, filtered[0].AiringAt)
		assert.Equal(t, 200, filtered[1].AiringAt)
	})
}
