package sync

import (
	"math"
	"seanime/internal/api/anilist"
)

// MapAnilistStatusToSimkl converts an AniList list status to SIMKL's watchlist status enum
// ("watching", "plantowatch", "hold", "completed", "dropped"). SIMKL has no separate
// "repeating" status, so it maps to "watching" like everywhere else in this codebase treats
// rewatching as an in-progress state.
func MapAnilistStatusToSimkl(status anilist.MediaListStatus) string {
	switch status {
	case anilist.MediaListStatusCurrent, anilist.MediaListStatusRepeating:
		return "watching"
	case anilist.MediaListStatusPlanning:
		return "plantowatch"
	case anilist.MediaListStatusCompleted:
		return "completed"
	case anilist.MediaListStatusDropped:
		return "dropped"
	case anilist.MediaListStatusPaused:
		return "hold"
	default:
		return "watching"
	}
}

// MapAnilistScoreToSimklRating converts AniList's 0-100 raw score scale to SIMKL's 1-10
// rating scale. scoreRaw == 0 means "no score set" in Seanime's UI, which should remove
// any existing SIMKL rating rather than send a rating of 0 (not a valid SIMKL rating).
func MapAnilistScoreToSimklRating(scoreRaw int) (rating int, shouldRemove bool) {
	if scoreRaw <= 0 {
		return 0, true
	}
	rating = int(math.Round(float64(scoreRaw) / 10.0))
	if rating < 1 {
		rating = 1
	}
	if rating > 10 {
		rating = 10
	}
	return rating, false
}

// MapSimklStatusToAnilist converts a SIMKL watchlist status back to AniList's list status.
// SIMKL has no "repeating" status, so its "watching" always maps back to Current - a genuine
// rewatch-in-progress on SIMKL cannot be distinguished from a first watch, matching the same
// information loss MapAnilistStatusToSimkl already accepts in the other direction.
func MapSimklStatusToAnilist(status string) anilist.MediaListStatus {
	switch status {
	case "watching":
		return anilist.MediaListStatusCurrent
	case "plantowatch":
		return anilist.MediaListStatusPlanning
	case "completed":
		return anilist.MediaListStatusCompleted
	case "dropped":
		return anilist.MediaListStatusDropped
	case "hold":
		return anilist.MediaListStatusPaused
	default:
		return anilist.MediaListStatusCurrent
	}
}

// MapSimklRatingToAnilistScore converts SIMKL's 1-10 rating scale to AniList's 0-100 raw score
// scale. A nil rating (never rated on SIMKL) maps to 0, matching AniList's own "unscored"
// convention used throughout this codebase.
func MapSimklRatingToAnilistScore(rating *int) int {
	if rating == nil {
		return 0
	}
	score := *rating * 10
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}
