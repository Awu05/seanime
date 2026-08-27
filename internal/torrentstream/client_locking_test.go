package torrentstream

import (
	"seanime/internal/util"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
)

// TestCurrentTorrentAndFileSnapshotReflectsAssignedValues guards the basic contract of the
// locked accessor: it must return exactly what was assigned, not stale/zero values.
func TestCurrentTorrentAndFileSnapshotReflectsAssignedValues(t *testing.T) {
	repo := &Repository{logger: util.NewLogger()}
	c := NewClient(repo)
	t.Cleanup(func() { unregisterClient(c) })

	torrentOpt, fileOpt := c.currentTorrentAndFile()
	require.True(t, torrentOpt.IsAbsent())
	require.True(t, fileOpt.IsAbsent())

	tor := &torrent.Torrent{}
	file := &torrent.File{}
	c.mu.Lock()
	c.currentTorrent = mo.Some(tor)
	c.currentFile = mo.Some(file)
	c.mu.Unlock()

	torrentOpt, fileOpt = c.currentTorrentAndFile()
	require.True(t, torrentOpt.IsPresent())
	require.True(t, fileOpt.IsPresent())
	require.Same(t, tor, torrentOpt.MustGet())
	require.Same(t, file, fileOpt.MustGet())
}

// TestCurrentTorrentAndFileConcurrentAccessNeverObservesTornPair is a regression test for
// unlocked reads of currentTorrent/currentFile racing StartStream/StopStream-style writers,
// which mutate both fields together under c.mu. A reader that doesn't go through a single
// locked accessor (currentTorrentAndFile) can observe one field updated and the other not -
// e.g. currentTorrent freshly set to Some while currentFile is still None from the previous
// reset, or vice versa. currentTorrentAndFile must never let a caller see that torn state:
// the pair is always both-present or both-absent.
func TestCurrentTorrentAndFileConcurrentAccessNeverObservesTornPair(t *testing.T) {
	repo := &Repository{logger: util.NewLogger()}
	c := NewClient(repo)
	t.Cleanup(func() { unregisterClient(c) })

	const iterations = 2000

	var wg sync.WaitGroup

	// Writer: mimics StartStream/StopStream toggling both fields together under c.mu.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			tor := &torrent.Torrent{}
			file := &torrent.File{}
			c.mu.Lock()
			c.currentFile = mo.Some(file)
			c.currentTorrent = mo.Some(tor)
			c.mu.Unlock()

			c.mu.Lock()
			c.currentTorrent = mo.None[*torrent.Torrent]()
			c.currentFile = mo.None[*torrent.File]()
			c.mu.Unlock()
		}
	}()

	// Readers: must always observe a self-consistent pair via the locked accessor.
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				torrentOpt, fileOpt := c.currentTorrentAndFile()
				require.Equal(t, torrentOpt.IsPresent(), fileOpt.IsPresent(),
					"currentTorrentAndFile returned a torn pair: torrent present=%v, file present=%v",
					torrentOpt.IsPresent(), fileOpt.IsPresent())
			}
		}()
	}

	wg.Wait()
}

// TestDropUnclaimedTorrentsLockedDoesNotDeadlockWhenCMuAlreadyHeld reproduces a real production
// incident: initializeClient, StopStream, and CleanupSession all call dropUnclaimedTorrents
// while already holding c.mu. dropUnclaimedTorrents calls claimedHashes(), which (after the
// currentTorrentAndFile refactor above) locks c.mu again for every client in the registry -
// including, for this call, the very client whose mu the caller already holds. sync.Mutex is not
// reentrant, so that second Lock() call blocks forever: the whole app hung on startup inside
// initializeClient, and identically on every StopStream/CleanupSession call, since none of those
// call sites' goroutines could ever make progress again.
//
// dropUnclaimedTorrentsLocked exists specifically for callers already holding c.mu - this test
// simulates exactly that calling convention and must complete promptly, not hang.
func TestDropUnclaimedTorrentsLockedDoesNotDeadlockWhenCMuAlreadyHeld(t *testing.T) {
	repo := &Repository{logger: util.NewLogger()}
	c := NewClient(repo)
	t.Cleanup(func() { unregisterClient(c) })

	done := make(chan struct{})
	go func() {
		c.mu.Lock()
		c.dropUnclaimedTorrentsLocked()
		c.mu.Unlock()
		close(done)
	}()

	select {
	case <-done:
		// completed without deadlocking - correct
	case <-time.After(2 * time.Second):
		t.Fatal("dropUnclaimedTorrentsLocked deadlocked while c.mu was already held by the caller " +
			"- this reproduces the production hang in initializeClient/StopStream/CleanupSession")
	}
}

// TestRecentlyAddedGracePeriodProtectsUnclaimedButInFlightTorrent reproduces a real production
// incident: a torrent is registered with the shared engine (addTorrentMagnet et al. call
// markRecentlyAdded as soon as they have a handle) well before StartStream finishes selecting it
// and calls SetActiveStream to register the real claim - addTorrentMagnet blocks on t.GotInfo()
// in between, which can take anywhere from milliseconds to the better part of a minute waiting on
// peers. A concurrent drop sweep (e.g. an unrelated StopStream for a different, already-finished
// session) landing in that window sees the still-being-set-up torrent as unclaimed by every
// session's activeStreams map and would otherwise drop it - closing the underlying storage out
// from under the in-flight StartStream call. That call then plays on with a torrent whose files
// were just closed, surfacing much later as a stream stuck on "Loading metadata..." that
// eventually fails with "reading from closed torrent: file already closed".
//
// The grace period must protect a freshly-added, not-yet-claimed hash, and must stop protecting
// it once the grace period elapses (or a genuinely orphaned torrent added at server startup would
// never be cleaned up).
func TestRecentlyAddedGracePeriodProtectsUnclaimedButInFlightTorrent(t *testing.T) {
	originalGracePeriod := recentlyAddedGracePeriod
	recentlyAddedGracePeriod = 50 * time.Millisecond
	t.Cleanup(func() { recentlyAddedGracePeriod = originalGracePeriod })

	var hash metainfo.Hash
	hash[0] = 1 // arbitrary non-zero hash so it doesn't collide with the zero-value default

	require.False(t, isWithinAddGracePeriod(hash), "a hash that was never added should never be protected")

	markRecentlyAdded(hash)
	require.True(t, isWithinAddGracePeriod(hash),
		"a torrent just registered with the engine (but not yet claimed via SetActiveStream) must "+
			"survive a concurrent drop sweep during selection")

	time.Sleep(2 * recentlyAddedGracePeriod)
	require.False(t, isWithinAddGracePeriod(hash),
		"the grace period must expire so a genuinely orphaned torrent is still eventually cleaned up")
}
