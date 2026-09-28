package scanner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// Closing the page that started a scan must not cut its AniList lookups short, since the scan's
// files are still saved afterwards.
func TestScanContextOutlivesCancelledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.NoError(t, scanContext(ctx).Err())
}
