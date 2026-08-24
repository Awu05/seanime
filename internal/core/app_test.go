package core

import (
	"seanime/internal/library/anime"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWithEpisodeAvailabilityReturnsEpisodesUnchangedWhenMonitorNotWired guards a production
// panic: the "Show torrent availability" library setting routes every /library/missing-episodes
// (and library collection) request through App.WithEpisodeAvailability, which unconditionally
// called a.episodeAvailability.WithEpisodes(...). Nothing in the codebase ever constructs an
// availability.Monitor and assigns it to App.episodeAvailability - the field is permanently nil -
// so any user with that setting enabled got a nil pointer dereference on every request.
//
// Until the monitor is actually wired up, WithEpisodeAvailability must degrade gracefully and
// return the episodes unchanged instead of panicking.
func TestWithEpisodeAvailabilityReturnsEpisodesUnchangedWhenMonitorNotWired(t *testing.T) {
	a := &App{}

	episodes := []*anime.Episode{{}, {}}

	ret := a.WithEpisodeAvailability(episodes)

	require.Equal(t, episodes, ret)
}
