package torrentstream

import (
	"context"
	"seanime/internal/util"
	"testing"

	"github.com/anacrolix/torrent"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
)

// TestClientShutdownCancelsMonitorLoopAndResetsState guards against a regression where
// Shutdown() closed the underlying torrent client but left the monitor-loop goroutine
// (started by initializeClient/UseSharedTorrentClient) running forever against it, and
// wrote currentTorrent/currentTorrentStatus without the lock monitorLoop uses for the
// same fields.
func TestClientShutdownCancelsMonitorLoopAndResetsState(t *testing.T) {
	repo := &Repository{logger: util.NewLogger()}
	c := NewClient(repo)
	t.Cleanup(func() { unregisterClient(c) })

	cfg := torrent.TestingConfig(t)
	cfg.DisableTCP = true
	cfg.DisableUTP = true
	tc, err := torrent.NewClient(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	c.torrentClient = mo.Some(tc)
	c.cancelFunc = cancel

	errs := c.Shutdown()
	require.Empty(t, errs)

	require.True(t, c.torrentClient.IsAbsent(), "expected torrentClient to be reset to None after Shutdown")
	require.True(t, c.currentTorrent.IsAbsent(), "expected currentTorrent to be reset after Shutdown")
	require.Error(t, ctx.Err(), "expected Shutdown to cancel the monitor-loop context so it stops polling the closed client")
}
