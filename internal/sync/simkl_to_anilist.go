package sync

import (
	"seanime/internal/api/anilist"
	"seanime/internal/api/simkl"
	"strconv"
)

// simklStatusBuckets is every AniList list status BuildAnimeCollectionFromSimkl produces a
// (possibly-empty) list for, so the resulting collection always has the same shape as a real
// AniList response regardless of which statuses the profile actually has entries in.
var simklStatusBuckets = []anilist.MediaListStatus{
	anilist.MediaListStatusCurrent,
	anilist.MediaListStatusPlanning,
	anilist.MediaListStatusCompleted,
	anilist.MediaListStatusDropped,
	anilist.MediaListStatusPaused,
}

// mapSimklFormat converts SIMKL's anime_type to AniList's MediaFormat. SIMKL's "special" and
// AniList's SPECIAL/OVA aren't a clean 1:1 split from anime_type alone, so "special" maps to
// AniList's SPECIAL as the closer of the two - this is a lossy, best-effort mapping, not a
// guarantee of matching AniList's own classification for the same title.
func mapSimklFormat(animeType string) *anilist.MediaFormat {
	var format anilist.MediaFormat
	switch animeType {
	case "movie":
		format = anilist.MediaFormatMovie
	case "ova":
		format = anilist.MediaFormatOva
	case "ona":
		format = anilist.MediaFormatOna
	case "special":
		format = anilist.MediaFormatSpecial
	case "music":
		format = anilist.MediaFormatMusic
	default:
		format = anilist.MediaFormatTv
	}
	return &format
}

// BuildAnimeCollectionFromSimkl converts a profile's SIMKL watchlist into an AniList-shaped
// collection for FallbackPlatform to serve when AniList itself is unreachable. Only fields SIMKL
// can reliably supply are populated - every other BaseAnime field is left nil, which every
// existing consumer of BaseAnime already tolerates (its generated Get* accessors are all
// nil-safe). Entries with no ids.anilist mapping are skipped: this codebase's data model is
// keyed entirely on AniList media IDs, so there is nowhere to put them.
func BuildAnimeCollectionFromSimkl(entries []simkl.AllItemsEntry) *anilist.AnimeCollection {
	byStatus := make(map[anilist.MediaListStatus][]*anilist.AnimeCollection_MediaListCollection_Lists_Entries)

	for _, e := range entries {
		mediaID, err := strconv.Atoi(e.Show.Ids.Anilist)
		if err != nil || mediaID == 0 {
			continue
		}

		status := MapSimklStatusToAnilist(e.Status)
		progress := e.WatchedEpisodesCount
		episodes := e.TotalEpisodesCount

		var scorePtr *float64
		if score := MapSimklRatingToAnilistScore(e.UserRating); score > 0 {
			s := float64(score)
			scorePtr = &s
		}

		title := e.Show.Title
		year := e.Show.Year
		media := &anilist.BaseAnime{
			ID:         mediaID,
			Title:      &anilist.BaseAnime_Title{Romaji: &title},
			CoverImage: &anilist.BaseAnime_CoverImage{Large: &e.Show.Poster, Medium: &e.Show.Poster},
			Format:     mapSimklFormat(e.Show.AnimeType),
			Episodes:   &episodes,
			SeasonYear: &year,
		}

		byStatus[status] = append(byStatus[status], &anilist.AnimeCollection_MediaListCollection_Lists_Entries{
			ID:       mediaID,
			Media:    media,
			Progress: &progress,
			Score:    scorePtr,
			Status:   &status,
		})
	}

	lists := make([]*anilist.AnimeCollection_MediaListCollection_Lists, 0, len(simklStatusBuckets))
	for _, status := range simklStatusBuckets {
		s := status
		lists = append(lists, &anilist.AnimeCollection_MediaListCollection_Lists{
			Status:  &s,
			Entries: byStatus[status],
		})
	}

	return &anilist.AnimeCollection{
		MediaListCollection: &anilist.AnimeCollection_MediaListCollection{Lists: lists},
	}
}
