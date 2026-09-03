package sync

import (
	"seanime/internal/api/anilist"
	"seanime/internal/api/simkl"
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
			Show: simkl.AllItemsShow{Title: "Test Anime", Poster: "/poster/abc.jpg", Year: 2024, AnimeType: "tv", Ids: simkl.Ids{Anilist: "101922"}},
		},
		{
			Status:               "completed",
			WatchedEpisodesCount: 12,
			TotalEpisodesCount:   12,
			UserRating:           &rating,
			Show: simkl.AllItemsShow{Title: "Finished Show", Poster: "/poster/def.jpg", Year: 2023, AnimeType: "tv", Ids: simkl.Ids{Anilist: "21"}},
		},
		{
			// No AniList mapping - must be skipped, not panic.
			Status: "watching",
			Show:   simkl.AllItemsShow{Title: "No AniList Match", Ids: simkl.Ids{Simkl: 999}},
		},
	}

	collection := BuildAnimeCollectionFromSimkl(entries)
	require.NotNil(t, collection.MediaListCollection)

	var all []*anilist.AnimeCollection_MediaListCollection_Lists_Entries
	for _, list := range collection.MediaListCollection.Lists {
		all = append(all, list.Entries...)
	}
	require.Len(t, all, 2, "the unmapped entry must be skipped, not panic")

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

	completed := byID[21]
	require.NotNil(t, completed)
	assert.Equal(t, anilist.MediaListStatusCompleted, *completed.Status)
	assert.Equal(t, 12, *completed.Progress)
	require.NotNil(t, completed.Score)
	assert.Equal(t, float64(80), *completed.Score)
}
