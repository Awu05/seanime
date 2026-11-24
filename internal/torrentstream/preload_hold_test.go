package torrentstream

import (
	"seanime/internal/util"
	"testing"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/require"
)

func newHoldTestTorrent(t *testing.T, name string) *torrent.Torrent {
	t.Helper()
	const (
		pieceLen = int64(1 << 20)
		pieces   = 4
	)
	infoBytes, err := bencode.Marshal(metainfo.Info{
		Name:        t.Name() + name + ".mkv",
		Length:      pieceLen * pieces,
		PieceLength: pieceLen,
		Pieces:      make([]byte, metainfo.HashSize*pieces),
	})
	require.NoError(t, err)

	cfg := torrent.TestingConfig(t)
	cfg.DisableTCP = true
	cfg.DisableUTP = true
	tc, err := torrent.NewClient(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { tc.Close() })

	tor, err := tc.AddTorrent(&metainfo.MetaInfo{InfoBytes: infoBytes})
	require.NoError(t, err)
	return tor
}

func holdCount(t *torrent.Torrent) (int, bool) {
	preloadHolds.Lock()
	defer preloadHolds.Unlock()
	n, ok := preloadHolds.count[t.InfoHash()]
	return n, ok
}

// A hold that ends must not resume a torrent another hold still waits on.
func TestPreloadHoldsAreCounted(t *testing.T) {
	tor := newHoldTestTorrent(t, "a")

	holdTorrentDownload(tor)
	holdTorrentDownload(tor)
	releaseTorrentDownload(tor)
	n, ok := holdCount(tor)
	require.True(t, ok)
	require.Equal(t, 1, n)

	releaseTorrentDownload(tor)
	_, ok = holdCount(tor)
	require.False(t, ok)
}

func TestPreloadHoldReleaseAfterDrop(t *testing.T) {
	tor := newHoldTestTorrent(t, "a")

	holdTorrentDownload(tor)
	tor.Drop()
	releaseTorrentDownload(tor)
	_, ok := holdCount(tor)
	require.False(t, ok)
}

// Another session's stream that still needs data keeps preloads held, and is never held itself.
func TestStreamingFilesCompleteCoversEverySession(t *testing.T) {
	tor := newHoldTestTorrent(t, "a")
	other := newHoldTestTorrent(t, "b")

	require.True(t, streamingFilesComplete())

	repo := &Repository{logger: util.NewLogger()}
	c := NewClient(repo)
	repo.client = c
	t.Cleanup(func() { unregisterClient(c) })
	c.SetActiveStream("other-session", tor, tor.Files()[0])

	require.False(t, streamingFilesComplete())
	require.True(t, streamedByAnySession(tor))
	require.False(t, streamedByAnySession(other))

	c.RemoveActiveStream("other-session")
	require.True(t, streamingFilesComplete())
	require.False(t, streamedByAnySession(tor))
}
