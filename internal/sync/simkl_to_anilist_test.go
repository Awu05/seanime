package sync

import (
	"seanime/internal/api/anilist"
	"seanime/internal/api/simkl"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAnimeCollectionFromSimkl(t *testing.T) {
	rating := 8
	entries := []simkl.AllItemsEntry{
		{
			Status:               "watching",
			WatchedEpisodesCount: 5,
			TotalEpisodesCount:   24,
			Show:                 simkl.AllItemsShow{Title: "Test Anime", Poster: "74/74415673dcdc9cdd", Year: 2024, AnimeType: "tv", Ids: simkl.Ids{Anilist: "101922"}},
		},
		{
			Status:               "completed",
			WatchedEpisodesCount: 12,
			TotalEpisodesCount:   12,
			UserRating:           &rating,
			Show:                 simkl.AllItemsShow{Title: "Finished Show", Poster: "/poster/def.jpg", Year: 2023, AnimeType: "tv", Ids: simkl.Ids{Anilist: "21"}},
		},
		{
			// No AniList mapping - must be skipped, not panic.
			Status: "watching",
			Show:   simkl.AllItemsShow{Title: "No AniList Match", Ids: simkl.Ids{Simkl: 999}},
		},
		{
			// Empty poster - must leave CoverImage nil, not build a URL from an empty path segment.
			Status: "plantowatch",
			Show:   simkl.AllItemsShow{Title: "No Poster", Poster: "", Year: 2022, AnimeType: "tv", Ids: simkl.Ids{Anilist: "5680"}},
		},
	}

	collection := BuildAnimeCollectionFromSimkl(entries)
	require.NotNil(t, collection.MediaListCollection)

	var all []*anilist.AnimeCollection_MediaListCollection_Lists_Entries
	for _, list := range collection.MediaListCollection.Lists {
		all = append(all, list.Entries...)
	}
	require.Len(t, all, 3, "the unmapped entry must be skipped, not panic")

	// The list-entry ID (AnimeCollection_..._Lists_Entries.ID) is a distinct field from the media
	// ID (BaseAnime.ID) keyed on below. SIMKL cannot supply a real AniList list-entry ID, so it
	// must be 0 - never the media ID, which would risk a real DeleteEntry(mediaID, listEntryID)
	// call (handlers.HandleDeleteAnilistListEntry) removing an unrelated real AniList entry once
	// AniList recovers.
	for _, e := range all {
		assert.Equal(t, 0, e.ID, "SIMKL-built entries must carry no fabricated AniList list-entry ID")
		assert.NotEqual(t, e.GetMedia().GetID(), e.ID, "the list-entry ID must never be set to the media ID")
	}

	byID := map[int]*anilist.AnimeCollection_MediaListCollection_Lists_Entries{}
	for _, e := range all {
		byID[e.GetMedia().GetID()] = e
	}

	watching := byID[101922]
	require.NotNil(t, watching)
	assert.Equal(t, anilist.MediaListStatusCurrent, *watching.Status)
	assert.Equal(t, 5, *watching.Progress)
	assert.Nil(t, watching.Score, "unrated entry must have a nil score, not a zero-value 0.0")
	assert.Equal(t, "Test Anime", *watching.GetMedia().GetTitle().Romaji)
	assert.Equal(t, 24, *watching.GetMedia().Episodes)
	require.NotNil(t, watching.GetMedia().GetCoverImage())
	require.NotNil(t, watching.GetMedia().GetCoverImage().Large)
	assert.Equal(t, "https://simkl.in/posters/74/74415673dcdc9cdd_c.webp", *watching.GetMedia().GetCoverImage().Large)
	require.NotNil(t, watching.GetMedia().GetCoverImage().Medium)
	assert.Equal(t, "https://simkl.in/posters/74/74415673dcdc9cdd_c.webp", *watching.GetMedia().GetCoverImage().Medium)
	assert.True(t, strings.HasPrefix(*watching.GetMedia().GetCoverImage().Large, "https://simkl.in/posters/"),
		"poster URLs must point straight at SIMKL's CDN - no third-party image proxy hop")
	assert.NotContains(t, *watching.GetMedia().GetCoverImage().Large, "wsrv.nl")
	assert.True(t, strings.HasSuffix(*watching.GetMedia().GetCoverImage().Large, "_c.webp"))

	completed := byID[21]
	require.NotNil(t, completed)
	assert.Equal(t, anilist.MediaListStatusCompleted, *completed.Status)
	assert.Equal(t, 12, *completed.Progress)
	require.NotNil(t, completed.Score)
	assert.Equal(t, float64(80), *completed.Score)

	noPoster := byID[5680]
	require.NotNil(t, noPoster)
	assert.Nil(t, noPoster.GetMedia().GetCoverImage(), "an empty poster fragment must leave CoverImage nil, not build a URL from an empty path segment")
}
