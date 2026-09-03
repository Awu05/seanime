package simkl

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIClient_GetAllItems(t *testing.T) {
	body := `{
		"watching": [
			{
				"status": "watching",
				"watched_episodes_count": 5,
				"total_episodes_count": 24,
				"added_to_watchlist_at": "2026-01-01T00:00:00Z",
				"last_watched_at": "2026-02-01T00:00:00Z",
				"user_rated_at": null,
				"user_rating": null,
				"show": {
					"title": "Test Anime",
					"poster": "/poster/abc.jpg",
					"year": 2024,
					"anime_type": "tv",
					"ids": {"simkl": 111, "anilist": "101922"}
				}
			}
		],
		"completed": [
			{
				"status": "completed",
				"watched_episodes_count": 12,
				"total_episodes_count": 12,
				"added_to_watchlist_at": "2025-06-01T00:00:00Z",
				"last_watched_at": "2025-07-01T00:00:00Z",
				"user_rated_at": "2025-07-02T00:00:00Z",
				"user_rating": 8,
				"show": {
					"title": "Finished Show",
					"poster": "/poster/def.jpg",
					"year": 2023,
					"anime_type": "tv",
					"ids": {"simkl": 222, "anilist": "21"}
				}
			}
		]
	}`

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/sync/all-items/anime", r.URL.Path)
		assert.Equal(t, "full", r.URL.Query().Get("extended"))
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(json.RawMessage(body))
	})

	entries, err := c.GetAllItems(context.Background())
	require.NoError(t, err)
	require.Len(t, entries, 2, "buckets must be flattened into one slice")

	byAnilist := map[string]AllItemsEntry{}
	for _, e := range entries {
		byAnilist[e.Show.Ids.Anilist] = e
	}

	watching := byAnilist["101922"]
	assert.Equal(t, "watching", watching.Status)
	assert.Equal(t, 5, watching.WatchedEpisodesCount)
	assert.Equal(t, 24, watching.TotalEpisodesCount)
	assert.Equal(t, "Test Anime", watching.Show.Title)
	assert.Equal(t, "/poster/abc.jpg", watching.Show.Poster)
	assert.Equal(t, 2024, watching.Show.Year)
	assert.Nil(t, watching.UserRating)

	completed := byAnilist["21"]
	assert.Equal(t, "completed", completed.Status)
	require.NotNil(t, completed.UserRating)
	assert.Equal(t, 8, *completed.UserRating)
}
