package simkl

import (
	"context"
	"encoding/json"
	"net/http"
)

// AllItemsShow is the show-metadata block nested in AllItemsEntry - named (not an anonymous
// inline struct) specifically so test files across multiple tasks in this plan can construct it
// as `simkl.AllItemsShow{...}` without each having to retype an identical anonymous struct shape
// (anonymous struct literals must match field-for-field including tags to compile, which is
// fragile to duplicate by hand across files).
type AllItemsShow struct {
	Title     string `json:"title"`
	Poster    string `json:"poster"`
	Year      int    `json:"year"`
	AnimeType string `json:"anime_type"`
	Ids       Ids    `json:"ids"`
}

// AllItemsEntry is one item from GET /sync/all-items/anime - the profile's current SIMKL
// watchlist entry for a single anime, used to build a fallback collection when AniList is
// unreachable. Field names match SIMKL's documented response schema (api.simkl.org).
type AllItemsEntry struct {
	Status               string `json:"status"`
	WatchedEpisodesCount int    `json:"watched_episodes_count"`
	TotalEpisodesCount   int    `json:"total_episodes_count"`
	// The three date fields below are decoded for wire-shape completeness and a future
	// date-mapping pass (startedAt/completedAt/updatedAt); date mapping was explicitly out of
	// scope here, so nothing currently reads them. This is intentional, not an oversight.
	AddedToWatchlistAt *string `json:"added_to_watchlist_at,omitempty"`
	LastWatchedAt      *string `json:"last_watched_at,omitempty"`
	UserRatedAt        *string `json:"user_rated_at,omitempty"`

	UserRating *int         `json:"user_rating,omitempty"`
	Show       AllItemsShow `json:"show"`
}

// allItemsResponse is the raw wire shape: an object keyed by status bucket, not a flat array.
type allItemsResponse map[string][]AllItemsEntry

// GetAllItems fetches every anime on the profile's SIMKL watchlist across all status buckets in
// one call, flattened into a single slice.
func (c *APIClient) GetAllItems(ctx context.Context) ([]AllItemsEntry, error) {
	res, err := c.do(ctx, http.MethodGet, "/sync/all-items/anime?extended=full", nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var wire allItemsResponse
	if err := json.NewDecoder(res.Body).Decode(&wire); err != nil {
		return nil, err
	}

	var entries []AllItemsEntry
	for _, bucket := range wire {
		entries = append(entries, bucket...)
	}
	return entries, nil
}
