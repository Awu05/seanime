package torrentstream

import (
	"seanime/internal/util"
	"testing"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
)

// TestReadyToStreamFalseWithoutCurrentTorrentOrFile guards the readyToStream() guard clauses
// that must keep holding after switching its readiness check from an aggregate byte/percentage
// threshold to torrentutil.ImmediatePiecesComplete (see internal/util/torrentutil for the piece-
// completion logic itself, tested there against real piece-index math).
func TestReadyToStreamFalseWithoutCurrentTorrentOrFile(t *testing.T) {
	repo := &Repository{logger: util.NewLogger()}
	c := NewClient(repo)
	t.Cleanup(func() { unregisterClient(c) })

	require.False(t, c.readyToStream(), "expected not ready with no current torrent/file set")
}

// TestReadyToStreamFalseForFreshlyAddedTorrent guards against readyToStream() reporting ready
// before the pieces needed to start playback are actually downloaded - a freshly added torrent
// with no peers has downloaded nothing.
func TestReadyToStreamFalseForFreshlyAddedTorrent(t *testing.T) {
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
	tc, err := torrent.NewClient(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { tc.Close() })

	tor, err := tc.AddTorrent(&metainfo.MetaInfo{InfoBytes: infoBytes})
	require.NoError(t, err)
	require.Len(t, tor.Files(), 1)
	file := tor.Files()[0]

	repo := &Repository{logger: util.NewLogger()}
	c := NewClient(repo)
	t.Cleanup(func() { unregisterClient(c) })
	c.torrentClient = mo.Some(tc)
	c.currentTorrent = mo.Some(tor)
	c.currentFile = mo.Some(file)

	require.False(t, c.readyToStream(), "expected not ready before any pieces are downloaded")
}
