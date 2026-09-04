package sync

import (
	"fmt"
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

// buildSimklPosterURL turns SIMKL's poster field - a bare path fragment like
// "74/74415673dcdc9cdd", not a usable URL on its own - into a full, directly-loadable image URL
// on SIMKL's own CDN, per SIMKL's documented image-serving convention
// (https://api.simkl.org/conventions/images): images are served from
// simkl.in/{category}/{poster}_{size}.{ext}, here category "posters", size "_c" (the documented
// "compact card" size, 170x250px - the standard choice for list/card display) and extension
// ".webp". Callers must set this on CoverImage.ExtraLarge, not just Large/Medium:
// MediaEntryCard's actual poster element (media-entry-card.tsx:456) reads
// coverImage.extraLarge exclusively, with no fallback to the other sizes. No image proxy is
// used: the URL points straight at SIMKL, so there is no third-party hop to break precisely when
// the user is already degraded (AniList down). An empty poster fragment returns nil rather than a
// URL built from an empty path segment: a missing cover image (CoverImage is a pointer field every
// consumer already handles) is preferable to a garbage one.
func buildSimklPosterURL(poster string) *string {
	if poster == "" {
		return nil
	}
	url := fmt.Sprintf("https://simkl.in/posters/%s_c.webp", poster)
	return &url
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

		var coverImage *anilist.BaseAnime_CoverImage
		if posterURL := buildSimklPosterURL(e.Show.Poster); posterURL != nil {
			coverImage = &anilist.BaseAnime_CoverImage{ExtraLarge: posterURL, Large: posterURL, Medium: posterURL}
		}

		media := &anilist.BaseAnime{
			ID: mediaID,
			Title: &anilist.BaseAnime_Title{
				Romaji:        &title,
				English:       &title,
				UserPreferred: &title,
			},
			CoverImage: coverImage,
			Format:     mapSimklFormat(e.Show.AnimeType),
			Episodes:   &episodes,
			SeasonYear: &year,
		}

		// ID here is the AniList LIST-ENTRY id, not the media id - SIMKL cannot supply one. It is
		// deliberately left 0: consumers such as handlers.HandleDeleteAnilistListEntry read this
		// field straight off whatever collection is currently served and pass it to a real
		// DeleteEntry(mediaID, listEntryID) once AniList recovers. Media ids and early real
		// list-entry ids occupy overlapping small-integer ranges, so putting mediaID here risked
		// silently deleting an unrelated real entry from the user's AniList account. 0 is not a
		// valid list-entry id, so it fails safely (not-found) instead.
		byStatus[status] = append(byStatus[status], &anilist.AnimeCollection_MediaListCollection_Lists_Entries{
			ID:       0,
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
