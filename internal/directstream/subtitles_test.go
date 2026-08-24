package directstream

import (
	"context"
	"seanime/internal/util/result"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewSubtitleStreamCleanupRemovesEntryFromActiveStreams guards against a regression where
// activeSubtitleStreams entries were only ever added (in StartSubtitleStreamP), never removed
// on Stop(). Every seek starts a new subtitle stream, so a long, scrub-heavy session would
// accumulate unbounded entries that a background goroutine (in StartSubtitleStreamP) scans
// every second for the rest of the session. newSubtitleStreamCleanup is the exact function
// StartSubtitleStreamP wires up as a SubtitleStream's cleanupFunc.
func TestNewSubtitleStreamCleanupRemovesEntryFromActiveStreams(t *testing.T) {
	activeSubtitleStreams := result.NewMap[string, *SubtitleStream]()
	const streamId = "test-subtitle-stream-id"
	activeSubtitleStreams.Set(streamId, &SubtitleStream{})

	ctx, cancel := context.WithCancel(context.Background())
	cleanup := newSubtitleStreamCleanup(cancel, activeSubtitleStreams, streamId)

	require.True(t, activeSubtitleStreams.Has(streamId), "test setup: expected the stream to be registered before cleanup")

	cleanup()

	require.False(t, activeSubtitleStreams.Has(streamId), "expected cleanup to remove the subtitle stream from activeSubtitleStreams")
	require.Error(t, ctx.Err(), "expected cleanup to still cancel the subtitle stream's context")
}
