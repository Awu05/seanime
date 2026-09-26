package torrentstream

import (
	"seanime/internal/events"
	"seanime/internal/util"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRetryAfterPlaybackFailure guards the fallback for a browser that can't decode an
// auto-selected release (e.g. 10-bit H.264 on a TV without a hardware decoder for it): the failed
// release must be excluded from exactly one retry, its codecs reported back for the client to
// remember, and nothing retried for streams the report doesn't belong to.
func TestRetryAfterPlaybackFailure(t *testing.T) {
	const release = "[Group] Show - 01 [1080p][Hi10P].mkv"

	newRepo := func(opts *StartStreamOptions) *Repository {
		logger := util.NewLogger()
		repo := &Repository{logger: logger, wsEventManager: events.NewMockWSEventManager(logger)}
		repo.setPreviousStreamOptions(opts)
		repo.setAutoSelectedRelease(opts, release)
		return repo
	}
	autoOpts := func() *StartStreamOptions {
		return &StartStreamOptions{MediaId: 1, AutoSelect: true, PlaybackType: PlaybackTypeNativePlayer, ClientId: "tv", UnsupportedVideoCodecs: []string{"HEVC"}}
	}

	t.Run("retries auto-select with the failed release excluded and its codec deprioritized", func(t *testing.T) {
		repo := newRepo(autoOpts())

		ret := repo.RetryAfterPlaybackFailure("tv")
		assert.True(t, ret.Retrying)
		assert.Equal(t, []string{"Hi10P"}, ret.UnsupportedVideoCodecs)

		// The retry runs StartStream in the background, which records its options before doing
		// anything else (it then fails harmlessly on this bare repository).
		require.Eventually(t, func() bool {
			opts, _ := repo.GetPreviousStreamOptions()
			return len(opts.ExcludedReleases) == 1
		}, time.Second, 10*time.Millisecond)
		opts, _ := repo.GetPreviousStreamOptions()
		assert.Equal(t, []string{release}, opts.ExcludedReleases)
		assert.ElementsMatch(t, []string{"HEVC", "Hi10P"}, opts.UnsupportedVideoCodecs)
	})

	t.Run("reports codecs without retrying once the retry budget is spent", func(t *testing.T) {
		opts := autoOpts()
		opts.ExcludedReleases = []string{"[Other] Show - 01 [1080p].mkv"}

		ret := newRepo(opts).RetryAfterPlaybackFailure("tv")
		assert.False(t, ret.Retrying)
		assert.Equal(t, []string{"Hi10P"}, ret.UnsupportedVideoCodecs)
	})

	t.Run("a duplicate report for the same stream is a no-op", func(t *testing.T) {
		opts := autoOpts()
		opts.ExcludedReleases = []string{"[Other] Show - 01 [1080p].mkv"}
		repo := newRepo(opts)

		repo.RetryAfterPlaybackFailure("tv")
		assert.Equal(t, &PlaybackFailureResponse{}, repo.RetryAfterPlaybackFailure("tv"))
	})

	t.Run("ignores a failure reported by a different client", func(t *testing.T) {
		assert.Equal(t, &PlaybackFailureResponse{}, newRepo(autoOpts()).RetryAfterPlaybackFailure("phone"))
	})

	t.Run("a superseded stream can't record its release over the newer stream's", func(t *testing.T) {
		older := autoOpts()
		repo := newRepo(older)
		repo.setPreviousStreamOptions(autoOpts())

		repo.setAutoSelectedRelease(older, "[Stale] Show - 01 [1080p].mkv")
		release, _ := repo.takeAutoSelectedRelease()
		assert.Empty(t, release)
	})

	t.Run("ignores manually selected streams", func(t *testing.T) {
		opts := autoOpts()
		opts.AutoSelect = false
		assert.Equal(t, &PlaybackFailureResponse{}, newRepo(opts).RetryAfterPlaybackFailure("tv"))
	})
}
