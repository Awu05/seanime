package sync

import (
	"strconv"
	"time"

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

// MapCalendarToBaseAnime converts SIMKL airing-calendar entries into AniList-shaped BaseAnime,
// for Component 3 (Schedule page fallback - both the "This Season" tab, reduced to whatever
// falls within the calendar's rolling window, and the recent/upcoming airing section, which is
// already window-based in its normal form so this is a closer fit). Title/Year come from the
// resolved AnimeDetail, not the calendar entry itself - CalendarEntry has neither (see Task 3).
// Same drop-if-unresolved rule as the other Map*ToBaseAnime functions.
func MapCalendarToBaseAnime(entries []simkl.CalendarEntry, resolved map[int]*simkl.AnimeDetail) []*anilist.BaseAnime {
	mapped := make([]*anilist.BaseAnime, 0, len(entries))
	for _, e := range entries {
		detail, ok := resolved[e.SimklID]
		if !ok {
			continue
		}
		anilistID, err := strconv.Atoi(detail.Ids.Anilist)
		if err != nil {
			continue
		}
		mapped = append(mapped, mapDiscoveryEntry(anilistID, detail.Title, detail.Year, ""))
	}
	return mapped
}

// MapCalendarToAiringSchedules converts SIMKL airing-calendar entries into
// ListRecentAnime_Page_AiringSchedules-shaped entries, for HandleAnilistListRecentAiringAnime's
// fallback specifically (contrast with MapCalendarToBaseAnime, used by the "This Season" tab's
// fallback, which only needs bare BaseAnime). Title/Year come from the resolved AnimeDetail, not
// the calendar entry itself (see MapCalendarToBaseAnime's doc comment, Task 6 - CalendarEntry has
// neither). date is parsed from SIMKL's ISO-8601 string; a parse failure drops that entry rather
// than fabricating an airingAt of 0 (which would sort as "already aired long ago" and misplace
// the entry).
func MapCalendarToAiringSchedules(entries []simkl.CalendarEntry, resolved map[int]*simkl.AnimeDetail) []*anilist.ListRecentAnime_Page_AiringSchedules {
	mapped := make([]*anilist.ListRecentAnime_Page_AiringSchedules, 0, len(entries))
	for _, e := range entries {
		detail, ok := resolved[e.SimklID]
		if !ok {
			continue
		}
		anilistID, err := strconv.Atoi(detail.Ids.Anilist)
		if err != nil {
			continue
		}
		airedAt, err := time.Parse(time.RFC3339, e.Date)
		if err != nil {
			continue
		}
		media := mapDiscoveryEntry(anilistID, detail.Title, detail.Year, "")
		mapped = append(mapped, &anilist.ListRecentAnime_Page_AiringSchedules{
			AiringAt: int(airedAt.Unix()),
			Episode:  e.Episode.Episode,
			ID:       e.SimklID, // SIMKL has no separate "airing schedule id" - simkl_id is the closest stable identifier available
			Media:    media,
		})
	}
	return mapped
}

// MapAnimeDetailToAnilist converts a SIMKL anime detail record into AnimeDetailsById_Media - the
// "extra fields" shape HandleGetAnilistAnimeDetails returns on top of whatever BaseAnime the
// frontend already has cached from a list query. anilistID is supplied by the caller (see this
// task's Interfaces note on why) rather than parsed from detail.Ids.Anilist here. Only
// genres/description map cleanly; SIMKL's characters/staff/relations/rankings/recommendations/
// studios/trailer shapes differ enough from AniList's that this pass leaves those nil rather than
// attempt a lossy best-effort mapping - the frontend's existing nil-safe accessors already handle
// an AnimeDetailsById_Media with only some fields populated (this is the same convention
// Component 1's BuildAnimeCollectionFromSimkl uses for BaseAnime).
func MapAnimeDetailToAnilist(anilistID int, detail *simkl.AnimeDetail) *anilist.AnimeDetailsById_Media {
	genres := make([]*string, len(detail.Genres))
	for i, g := range detail.Genres {
		g := g
		genres[i] = &g
	}
	description := detail.Overview
	return &anilist.AnimeDetailsById_Media{
		ID:          anilistID,
		Description: &description,
		Genres:      genres,
	}
}
