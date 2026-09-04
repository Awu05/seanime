package sync

import (
	"strconv"

	"seanime/internal/api/anilist"
	"seanime/internal/api/simkl"
)

// DiscoveryAvailable reports whether Components 2-4's SIMKL fallback can run for a profile.
// Unlike Component 1 (FallbackPlatform's tracking fallback, which needs a completed OAuth
// connection because it reads the user's personal watchlist), Discover/Search/Calendar/Details
// fallback needs only a client_id - see the spec's "revised gating" note.
func DiscoveryAvailable(clientID string) bool {
	return clientID != ""
}

// mapDiscoveryEntry builds a thin BaseAnime from the fields common to search/trending results,
// mirroring BuildAnimeCollectionFromSimkl's convention in simkl_to_anilist.go: only fields SIMKL
// can reliably supply are populated, every other BaseAnime field stays nil (every consumer's
// generated Get* accessors are nil-safe).
func mapDiscoveryEntry(anilistID int, title string, year int, poster string) *anilist.BaseAnime {
	var coverImage *anilist.BaseAnime_CoverImage
	if posterURL := buildSimklPosterURL(poster); posterURL != nil {
		coverImage = &anilist.BaseAnime_CoverImage{Large: posterURL, Medium: posterURL}
	}
	yearCopy := year
	return &anilist.BaseAnime{
		ID:         anilistID,
		Title:      &anilist.BaseAnime_Title{Romaji: &title},
		CoverImage: coverImage,
		SeasonYear: &yearCopy,
	}
}

// MapSearchResultsToBaseAnime converts SIMKL search results into AniList-shaped BaseAnime, using
// each result's own Title/Year/Poster (search results already carry these) but sourcing the
// AniList id from `resolved` (simklID -> AnimeDetail, from IDResolutionCache - see Task 4).
// Entries with no resolved AniList id, or with an unparseable one, are dropped: nothing in
// Seanime's AniList-ID-keyed data model can represent them, matching BuildAnimeCollectionFromSimkl's
// existing rule for Component 1.
func MapSearchResultsToBaseAnime(results []simkl.SearchResult, resolved map[int]*simkl.AnimeDetail) []*anilist.BaseAnime {
	mapped := make([]*anilist.BaseAnime, 0, len(results))
	for _, r := range results {
		detail, ok := resolved[r.Ids.SimklID]
		if !ok {
			continue
		}
		anilistID, err := strconv.Atoi(detail.Ids.Anilist)
		if err != nil {
			continue
		}
		mapped = append(mapped, mapDiscoveryEntry(anilistID, r.Title, r.Year, r.Poster))
	}
	return mapped
}

// MapTrendingToBaseAnime converts SIMKL trending/best entries into AniList-shaped BaseAnime.
// Same drop-if-unresolved rule as MapSearchResultsToBaseAnime.
func MapTrendingToBaseAnime(entries []simkl.TrendingEntry, resolved map[int]*simkl.AnimeDetail) []*anilist.BaseAnime {
	mapped := make([]*anilist.BaseAnime, 0, len(entries))
	for _, e := range entries {
		detail, ok := resolved[e.Ids.SimklID]
		if !ok {
			continue
		}
		anilistID, err := strconv.Atoi(detail.Ids.Anilist)
		if err != nil {
			continue
		}
		mapped = append(mapped, mapDiscoveryEntry(anilistID, e.Title, e.Year, e.Poster))
	}
	return mapped
}
