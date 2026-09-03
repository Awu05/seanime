package sync

import (
	"seanime/internal/api/anilist"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapAnilistStatusToSimkl(t *testing.T) {
	cases := map[anilist.MediaListStatus]string{
		anilist.MediaListStatusCurrent:   "watching",
		anilist.MediaListStatusPlanning:  "plantowatch",
		anilist.MediaListStatusCompleted: "completed",
		anilist.MediaListStatusDropped:   "dropped",
		anilist.MediaListStatusPaused:    "hold",
		anilist.MediaListStatusRepeating: "watching",
	}
	for status, want := range cases {
		assert.Equal(t, want, MapAnilistStatusToSimkl(status), "status %s", status)
	}
}

func TestMapAnilistScoreToSimklRating(t *testing.T) {
	t.Run("zero means unscored, should remove any existing rating", func(t *testing.T) {
		rating, shouldRemove := MapAnilistScoreToSimklRating(0)
		assert.True(t, shouldRemove)
		assert.Equal(t, 0, rating)
	})

	t.Run("converts AniList's 0-100 scale to SIMKL's 1-10 scale", func(t *testing.T) {
		rating, shouldRemove := MapAnilistScoreToSimklRating(85)
		assert.False(t, shouldRemove)
		assert.Equal(t, 9, rating) // round(85/10) = 9 (rounds to nearest, not floor)
	})

	t.Run("clamps to the 1-10 range", func(t *testing.T) {
		rating, shouldRemove := MapAnilistScoreToSimklRating(3) // rounds to 0, must clamp up to 1
		assert.False(t, shouldRemove)
		assert.Equal(t, 1, rating)

		rating, shouldRemove = MapAnilistScoreToSimklRating(100)
		assert.False(t, shouldRemove)
		assert.Equal(t, 10, rating)
	})
}
