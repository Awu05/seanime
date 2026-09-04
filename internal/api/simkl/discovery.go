package simkl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// DiscoveryIds is the ids shape returned by SIMKL's public list endpoints (search, trending,
// calendar) - unlike the authenticated sync endpoints, these do NOT include an "anilist" field;
// only the single-item /anime/{id}?extended=full details endpoint does (see AnimeDetail below).
// Resolving an AniList id from a DiscoveryIds requires the enrichment step in
// internal/sync/discovery_fallback.go.
type DiscoveryIds struct {
	SimklID int    `json:"simkl_id"`
	Slug    string `json:"slug"`
}

// SearchResult is one item from GET /search/anime.
type SearchResult struct {
	Title  string       `json:"title"`
	Year   int          `json:"year"`
	Poster string       `json:"poster"`
	Ids    DiscoveryIds `json:"ids"`
}

// SearchAnime performs a free-text search against SIMKL's anime catalog. Requires only a
// client_id (set automatically by APIClient.do) - no OAuth access token needed, so this works
// for any profile with a SimklSettings.ClientId configured, even without a completed SIMKL
// account connection.
func (c *APIClient) SearchAnime(ctx context.Context, query string) ([]SearchResult, error) {
	path := "/search/anime?" + url.Values{"q": {query}, "extended": {"full"}}.Encode()
	res, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("simkl: search anime: %w", err)
	}
	defer res.Body.Close()

	var results []SearchResult
	if err := json.NewDecoder(res.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("simkl: decode search results: %w", err)
	}
	return results, nil
}

// TrendingEntry is one item from GET /anime/best/{filter}.
type TrendingEntry struct {
	Title  string       `json:"title"`
	Year   int          `json:"year"`
	Poster string       `json:"poster"`
	Ids    DiscoveryIds `json:"ids"`
}

// GetTrendingAnime returns SIMKL's current most-watched anime list, used as the Discover
// "Trending" section's fallback candidate pool and as the browse-without-search-text fallback
// pool for Search (see spec Component 2).
func (c *APIClient) GetTrendingAnime(ctx context.Context) ([]TrendingEntry, error) {
	res, err := c.do(ctx, http.MethodGet, "/anime/best/all", nil)
	if err != nil {
		return nil, fmt.Errorf("simkl: get trending anime: %w", err)
	}
	defer res.Body.Close()

	var results []TrendingEntry
	if err := json.NewDecoder(res.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("simkl: decode trending results: %w", err)
	}
	return results, nil
}
