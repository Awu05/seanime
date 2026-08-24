package torrentstream

import (
	"seanime/internal/util"
	"sync"
	"testing"

	"github.com/anacrolix/torrent"
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
