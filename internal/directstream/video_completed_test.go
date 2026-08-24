package directstream

import (
	"errors"
	"seanime/internal/library/anime"
	"seanime/internal/platforms/platform"
	"seanime/internal/testmocks"
	"seanime/internal/util"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHandleVideoCompletedRefreshesAnimeCollectionOnSuccess guards a regression where the
// native-player completion handler updated AniList's progress but never refreshed the app's own
// local AniList collection cache afterward - unlike the equivalent external-player path in
// playbackmanager, which calls its refresh function on success. Without it, "the watching
// tracker" (any UI reading the cached collection) never reflects a completed episode until some
// unrelated refresh happens to occur.
func TestHandleVideoCompletedRefreshesAnimeCollectionOnSuccess(t *testing.T) {
	fakePlatform := testmocks.NewFakePlatformBuilder().Build()
	refreshCalls := 0

	m := &Manager{
		Logger:                     util.NewLogger(),
		platformRef:                util.NewRef[platform.Platform](fakePlatform),
		refreshAnimeCollectionFunc: func() { refreshCalls++ },
	}

	media := testmocks.NewBaseAnimeBuilder(1, "Test Anime").WithEpisodes(12).Build()
	cs := &BaseStream{
		clientId: "client-1",
		media:    media,
		episode:  &anime.Episode{ProgressNumber: 5},
		manager:  m,
	}

	m.handleVideoCompleted(cs)

	calls := fakePlatform.UpdateEntryProgressCalls()
	require.Len(t, calls, 1)
	require.Equal(t, media.ID, calls[0].MediaID)
	require.Equal(t, 5, calls[0].Progress)
	require.NotNil(t, calls[0].TotalEpisodes)
	require.Equal(t, 12, *calls[0].TotalEpisodes)

	require.Equal(t, 1, refreshCalls, "expected the local AniList collection cache to be refreshed after a successful progress update")
}

// TestHandleVideoCompletedDoesNotRefreshOnFailure guards the other half: a failed AniList update
// must not refresh the (now-stale) local collection cache, and must not panic or hang even
// though it was previously silently discarded (`_ = ...UpdateEntryProgress(...)`).
func TestHandleVideoCompletedDoesNotRefreshOnFailure(t *testing.T) {
	fakePlatform := testmocks.NewFakePlatformBuilder().WithUpdateEntryProgressError(errors.New("anilist unreachable")).Build()
	refreshCalls := 0

	m := &Manager{
		Logger:                     util.NewLogger(),
		platformRef:                util.NewRef[platform.Platform](fakePlatform),
		refreshAnimeCollectionFunc: func() { refreshCalls++ },
	}

	media := testmocks.NewBaseAnimeBuilder(1, "Test Anime").WithEpisodes(12).Build()
	cs := &BaseStream{
		clientId: "client-1",
		media:    media,
		episode:  &anime.Episode{ProgressNumber: 5},
		manager:  m,
	}

	require.NotPanics(t, func() { m.handleVideoCompleted(cs) })

	require.Len(t, fakePlatform.UpdateEntryProgressCalls(), 1)
	require.Equal(t, 0, refreshCalls, "must not refresh the local collection cache when the AniList update itself failed")
}

// TestHandleVideoCompletedOnlyUpdatesOnce guards the existing sync.Once behavior: a stream
// firing multiple completion events (e.g. seeking back into the last 20% of the video after
// already completing once) must only push one progress update.
func TestHandleVideoCompletedOnlyUpdatesOnce(t *testing.T) {
	fakePlatform := testmocks.NewFakePlatformBuilder().Build()

	m := &Manager{
		Logger:                     util.NewLogger(),
		platformRef:                util.NewRef[platform.Platform](fakePlatform),
		refreshAnimeCollectionFunc: func() {},
	}

	media := testmocks.NewBaseAnimeBuilder(1, "Test Anime").WithEpisodes(12).Build()
	cs := &BaseStream{
		clientId: "client-1",
		media:    media,
		episode:  &anime.Episode{ProgressNumber: 5},
		manager:  m,
	}

	m.handleVideoCompleted(cs)
	m.handleVideoCompleted(cs)
	m.handleVideoCompleted(cs)

	require.Len(t, fakePlatform.UpdateEntryProgressCalls(), 1)
}
