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

// calendarURL is the CDN-hosted anime airing calendar feed - a different host from
// APIClient.baseURL (api.simkl.com), so GetAnimeCalendar bypasses c.do() and fetches it directly.
// Live-verified (docs/superpowers/plans/simkl-endpoint-findings.md): this feed needs no
// client_id and covers a real ~5-week rolling window. The alternative considered,
// api.simkl.com/anime/airing, requires client_id and only ever returns a single day - date/
// range query params (date, days, start_date/end_date, range) are all silently ignored - so it
// cannot serve a season-length view and is deliberately NOT used here. A package-level var so
// tests can point it at an httptest server.
var calendarURL = "https://data.simkl.in/calendar/v2/anime.json"

// CalendarEpisode is the nested episode info in a CalendarEntry.
type CalendarEpisode struct {
	Episode int    `json:"episode"`
	Title   string `json:"title"`
	URL     string `json:"url"`
}

// CalendarEntry is one item from the anime airing calendar feed. Live-verified: unlike every
// other endpoint in this file, SimklID is a bare top-level field, not nested under "ids" - and
// there is no title/poster here at all, only date + episode number/title. Component 3's mapping
// (Task 6/9) must get title/poster/AniList id from the same per-item enrichment call
// (GetAnimeDetails) used for id resolution - it cannot read a title off this struct because
// there isn't one.
type CalendarEntry struct {
	SimklID    int             `json:"simkl_id"`
	Date       string          `json:"date"` // ISO-8601
	FinaleType *string         `json:"finale_type"`
	Episode    CalendarEpisode `json:"episode"`
}

// calendarResponse is the wire shape of the CDN calendar feed - a single object with the
// entries under a "calendar" key, not a bare array like the other list endpoints.
type calendarResponse struct {
	Calendar []CalendarEntry `json:"calendar"`
}

// GetAnimeCalendar returns the anime airing calendar - used by Component 3 (Schedule page
// fallback) for both the "This Season" tab and the recent/upcoming airing section.
func (c *APIClient) GetAnimeCalendar(ctx context.Context) ([]CalendarEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, calendarURL, nil)
	if err != nil {
		return nil, fmt.Errorf("simkl: build calendar request: %w", err)
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("simkl: get anime calendar: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("simkl: calendar request failed with status %d", res.StatusCode)
	}

	var parsed calendarResponse
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("simkl: decode calendar results: %w", err)
	}
	results := parsed.Calendar
	return results, nil
}

// FullIds is the complete cross-service id crosswalk, only returned by the single-item
// /anime/{id}?extended=full endpoint (never by the list endpoints above). Live-verified
// (docs/superpowers/plans/simkl-endpoint-findings.md): every foreign id EXCEPT "simkl" itself
// comes back as a quoted JSON string, not a number (e.g. "anilist":"154587") - only Simkl is a
// bare int. Anilist/Mal are declared as string here to match the wire format; callers needing an
// int must strconv.Atoi, treating "" or a parse error as "no mapping" (not an error) - a title
// with no AniList crosswalk simply omits or empties the field rather than erroring.
type FullIds struct {
	Simkl   int    `json:"simkl"`
	Anilist string `json:"anilist"`
	Mal     string `json:"mal"`
}

// AnimeDetail is the response from GET /anime/{simklID}?extended=full.
type AnimeDetail struct {
	Title         string   `json:"title"`
	Year          int      `json:"year"`
	Overview      string   `json:"overview"`
	Genres        []string `json:"genres"`
	TotalEpisodes int      `json:"total_episodes"`
	Ids           FullIds  `json:"ids"`
}

// GetAnimeDetails fetches the full record for one title by its SIMKL id - the only SIMKL
// endpoint confirmed to return the AniList id crosswalk (Ids.Anilist), used both directly
// (Component 4) and as the enrichment step for Components 2-3's list results that only carry a
// bare simkl_id (see internal/sync/discovery_fallback.go).
func (c *APIClient) GetAnimeDetails(ctx context.Context, simklID int) (*AnimeDetail, error) {
	path := fmt.Sprintf("/anime/%d?extended=full", simklID)
	res, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("simkl: get anime details: %w", err)
	}
	defer res.Body.Close()

	var detail AnimeDetail
	if err := json.NewDecoder(res.Body).Decode(&detail); err != nil {
		return nil, fmt.Errorf("simkl: decode anime details: %w", err)
	}
	return &detail, nil
}

// searchIDResult is the wire shape of one /search/id result item.
type searchIDResult struct {
	Ids FullIds `json:"ids"`
}

// SearchIDByAnilist resolves a SIMKL id from an AniList id, for Component 4's
// FallbackPlatform.GetAnimeDetails override (the handler only has an AniList id to look up with,
// but GetAnimeDetails needs a SIMKL id). ok is false, err is nil when SIMKL has no mapping for
// this AniList id - a legitimate "not found", not a request failure.
func (c *APIClient) SearchIDByAnilist(ctx context.Context, anilistID int) (simklID int, ok bool, err error) {
	path := "/search/id?" + url.Values{"anilist": {fmt.Sprintf("%d", anilistID)}}.Encode()
	res, doErr := c.do(ctx, http.MethodGet, path, nil)
	if doErr != nil {
		return 0, false, fmt.Errorf("simkl: search id by anilist: %w", doErr)
	}
	defer res.Body.Close()

	var results []searchIDResult
	if decodeErr := json.NewDecoder(res.Body).Decode(&results); decodeErr != nil {
		return 0, false, fmt.Errorf("simkl: decode search id results: %w", decodeErr)
	}
	if len(results) == 0 {
		return 0, false, nil
	}
	return results[0].Ids.Simkl, true, nil
}
