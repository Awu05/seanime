package torrentstream

import (
	"seanime/internal/util"
	"testing"

	"github.com/anacrolix/torrent"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
)

// newTestTorrentClient creates a real but network-disabled anacrolix client, cleaned up on test
// exit, for exercising torrent-client-reference plumbing without touching the network.
func newTestTorrentClient(t *testing.T) *torrent.Client {
	t.Helper()
	cfg := torrent.TestingConfig(t)
	cfg.DisableTCP = true
	cfg.DisableUTP = true
	tc, err := torrent.NewClient(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tc.Close() })
	return tc
}

func newTestRepository(t *testing.T) *Repository {
	t.Helper()
	repo := &Repository{logger: util.NewLogger()}
	repo.client = NewClient(repo)
	t.Cleanup(func() { unregisterClient(repo.client) })
	return repo
}

// TestRepositorySyncSharedTorrentClient guards the fix for a reported bug: a per-profile
// session's torrentstream.Repository picks up the app singleton's shared anacrolix engine only
// once, at session-creation time (see session_factory.go). If that happened before the singleton
// had finished initializing its own engine (a startup race), or the singleton's engine was later
// torn down and recreated (e.g. a settings change reinitializing it), the session's reference
// went stale or stayed permanently absent, and every torrent-stream action for that profile
// failed with "torrent client is not initialized" - while everything else kept working, since
// nothing else depends on this per-session reference. SyncSharedTorrentClient is meant to be
// called on every settings broadcast so the reference self-heals instead of going stale forever.
func TestRepositorySyncSharedTorrentClient(t *testing.T) {
	t.Run("adopts the source's engine when it has none of its own", func(t *testing.T) {
		source := newTestRepository(t)
		tc := newTestTorrentClient(t)
		source.client.torrentClient = mo.Some(tc)

		target := newTestRepository(t)
		target.SyncSharedTorrentClient(source)

		got, ok := target.client.torrentClient.Get()
		require.True(t, ok, "expected the target to adopt the source's engine")
		require.Same(t, tc, got)
	})

	t.Run("does nothing when the source has no engine yet", func(t *testing.T) {
		source := newTestRepository(t)
		target := newTestRepository(t)

		target.SyncSharedTorrentClient(source)

		require.True(t, target.client.torrentClient.IsAbsent())
	})

	t.Run("re-adopts a new engine after the source's was reinitialized", func(t *testing.T) {
		source := newTestRepository(t)
		oldTc := newTestTorrentClient(t)
		source.client.torrentClient = mo.Some(oldTc)

		target := newTestRepository(t)
		target.SyncSharedTorrentClient(source)

		newTc := newTestTorrentClient(t)
		source.client.torrentClient = mo.Some(newTc)

		target.SyncSharedTorrentClient(source)

		got, ok := target.client.torrentClient.Get()
		require.True(t, ok)
		require.Same(t, newTc, got, "expected the target to pick up the source's reinitialized engine")
	})

	t.Run("does nothing when source is nil", func(t *testing.T) {
		target := newTestRepository(t)
		require.NotPanics(t, func() {
			target.SyncSharedTorrentClient(nil)
		})
		require.True(t, target.client.torrentClient.IsAbsent())
	})
}
