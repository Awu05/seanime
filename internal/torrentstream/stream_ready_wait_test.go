package torrentstream

import (
	"seanime/internal/util"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
)

func newTestRepositoryWithClient(t *testing.T) *Repository {
	t.Helper()
	repo := &Repository{logger: util.NewLogger()}
	repo.client = NewClient(repo)
	t.Cleanup(func() { unregisterClient(repo.client) })
	return repo
}

// TestWaitUntilReadyToStreamReturnsSupersededWhenGenerationChanges guards against the stale-
// goroutine bug: a rapid second StartStream call (episode switch) used to leave the first call's
// readiness-polling goroutine running, which could later fire stale playback logic (e.g. signal
// an external player using the now-superseded episode's data) once it happened to observe the
// *new* torrent become ready.
func TestWaitUntilReadyToStreamReturnsSupersededWhenGenerationChanges(t *testing.T) {
	previousPollInterval := waitReadyPollInterval
	waitReadyPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { waitReadyPollInterval = previousPollInterval })

	// A present-but-never-ready torrent, so the loop keeps polling (rather than returning
	// immediately on the "dropped" check) long enough to observe the generation change.
	const (
		pieceLen = int64(1 << 20)
		pieces   = 256
	)
	infoBytes, err := bencode.Marshal(metainfo.Info{
		Name:        t.Name() + ".mkv",
		Length:      pieceLen * pieces,
		PieceLength: pieceLen,
		Pieces:      make([]byte, metainfo.HashSize*pieces),
	})
	require.NoError(t, err)

	cfg := torrent.TestingConfig(t)
	cfg.DisableTCP = true
	cfg.DisableUTP = true
	tc, tcErr := torrent.NewClient(cfg)
	require.NoError(t, tcErr)
	t.Cleanup(func() { tc.Close() })

	tor, tcErr := tc.AddTorrent(&metainfo.MetaInfo{InfoBytes: infoBytes})
	require.NoError(t, tcErr)
	file := tor.Files()[0]

	repo := newTestRepositoryWithClient(t)
	repo.client.torrentClient = mo.Some(tc)
	repo.client.currentTorrent = mo.Some(tor)
	repo.client.currentFile = mo.Some(file)

	myGeneration := repo.startStreamGeneration.Add(1)

	done := make(chan error, 1)
	go func() { done <- repo.waitUntilReadyToStream(myGeneration) }()

	// Give the goroutine a couple of poll cycles to start, then simulate a second, newer
	// StartStream call superseding this one.
	time.Sleep(30 * time.Millisecond)
	repo.startStreamGeneration.Add(1)

	select {
	case waitErr := <-done:
		require.ErrorIs(t, waitErr, errStreamSuperseded)
	case <-time.After(2 * time.Second):
		t.Fatal("waitUntilReadyToStream did not return after being superseded")
	}
}

// TestWaitUntilReadyToStreamReturnsErrorWhenTorrentDropped guards against the readiness-polling
// loops spinning forever (or, worse, silently returning with no error surfaced anywhere) after
// the torrent they were waiting on gets dropped out from under them.
func TestWaitUntilReadyToStreamReturnsErrorWhenTorrentDropped(t *testing.T) {
	previousPollInterval := waitReadyPollInterval
	waitReadyPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { waitReadyPollInterval = previousPollInterval })

	repo := newTestRepositoryWithClient(t)
	// torrentClient is absent (never initialized), simulating a dropped/uninitialized client.

	myGeneration := repo.startStreamGeneration.Add(1)

	err := repo.waitUntilReadyToStream(myGeneration)
	require.Error(t, err)
	require.NotErrorIs(t, err, errStreamSuperseded)
}

// TestWaitUntilReadyToStreamReturnsErrorOnStall guards against a torrent with no seeders (or a
// stalled download) leaving the user staring at an indefinite loading spinner with no timeout,
// no retry, and no error - readiness-polling now gives up and surfaces a clear error after a
// bounded period of no download progress.
func TestWaitUntilReadyToStreamReturnsErrorOnStall(t *testing.T) {
	previousPollInterval := waitReadyPollInterval
	previousStallDeadline := waitReadyStallDeadline
	waitReadyPollInterval = 10 * time.Millisecond
	waitReadyStallDeadline = 50 * time.Millisecond
	t.Cleanup(func() {
		waitReadyPollInterval = previousPollInterval
		waitReadyStallDeadline = previousStallDeadline
	})

	const (
		pieceLen = int64(1 << 20)
		pieces   = 256
	)
	infoBytes, err := bencode.Marshal(metainfo.Info{
		Name:        t.Name() + ".mkv",
		Length:      pieceLen * pieces,
		PieceLength: pieceLen,
		Pieces:      make([]byte, metainfo.HashSize*pieces),
	})
	require.NoError(t, err)

	cfg := torrent.TestingConfig(t)
	cfg.DisableTCP = true
	cfg.DisableUTP = true
	tc, tcErr := torrent.NewClient(cfg)
	require.NoError(t, tcErr)
	t.Cleanup(func() { tc.Close() })

	tor, tcErr := tc.AddTorrent(&metainfo.MetaInfo{InfoBytes: infoBytes})
	require.NoError(t, tcErr)
	file := tor.Files()[0]

	repo := newTestRepositoryWithClient(t)
	repo.client.torrentClient = mo.Some(tc)
	repo.client.currentTorrent = mo.Some(tor)
	repo.client.currentFile = mo.Some(file)

	myGeneration := repo.startStreamGeneration.Add(1)

	done := make(chan error, 1)
	go func() { done <- repo.waitUntilReadyToStream(myGeneration) }()

	select {
	case waitErr := <-done:
		require.Error(t, waitErr)
		require.NotErrorIs(t, waitErr, errStreamSuperseded)
	case <-time.After(2 * time.Second):
		t.Fatal("waitUntilReadyToStream did not give up on a stalled, no-progress download")
	}
}
