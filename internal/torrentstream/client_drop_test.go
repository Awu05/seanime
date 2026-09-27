package torrentstream

import (
	"os"
	"path/filepath"
	"seanime/internal/database/models"
	"seanime/internal/util"
	"testing"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
)

// TestDropUnclaimedTorrentsDeletesTheTorrentsData guards the disk cleanup that runs when a torrent
// is dropped: data lives under DownloadDir/<infohash> (storage.NewFileByInfoHash), so deleting
// DownloadDir/<name> left every stream's data on disk forever - which also made the playback
// error screen's "clear torrent cache" action a no-op.
func TestDropUnclaimedTorrentsDeletesTheTorrentsData(t *testing.T) {
	downloadDir := t.TempDir()

	const pieceLen = int64(1 << 14)
	infoBytes, err := bencode.Marshal(metainfo.Info{
		Name:        "[Group] Show - 01 [1080p].mkv",
		Length:      pieceLen,
		PieceLength: pieceLen,
		Pieces:      make([]byte, metainfo.HashSize),
	})
	require.NoError(t, err)

	st := storage.NewFileByInfoHash(downloadDir)
	t.Cleanup(func() { _ = st.Close() })
	cfg := torrent.TestingConfig(t)
	cfg.DisableTCP = true
	cfg.DisableUTP = true
	cfg.DefaultStorage = st
	tc, err := torrent.NewClient(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { tc.Close() })

	tor, err := tc.AddTorrent(&metainfo.MetaInfo{InfoBytes: infoBytes})
	require.NoError(t, err)

	dataDir := filepath.Join(downloadDir, tor.InfoHash().HexString())
	require.NoError(t, os.MkdirAll(dataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, tor.Name()), []byte("data"), 0o644))

	repo := &Repository{
		logger:   util.NewLogger(),
		settings: mo.Some(Settings{TorrentstreamSettings: models.TorrentstreamSettings{DownloadDir: downloadDir}}),
	}
	c := NewClient(repo)
	repo.client = c
	t.Cleanup(func() { unregisterClient(c) })
	c.torrentClient = mo.Some(tc)

	c.dropUnclaimedTorrents()

	require.NoDirExists(t, dataDir)
}

// TestDropIfUnclaimedSparesTorrentsOtherSessionsAreStreaming guards short-lived torrent users
// (file previews, rejected auto-select candidates, cancelled preloads): the shared engine returns
// the same handle for an infohash that's already loaded, so dropping "their" torrent used to kill
// whichever profile was streaming that same release (e.g. a batch) at the time.
func TestDropIfUnclaimedSparesTorrentsOtherSessionsAreStreaming(t *testing.T) {
	const pieceLen = int64(1 << 14)
	infoBytes, err := bencode.Marshal(metainfo.Info{
		Name:        "[Group] Show (Batch)",
		Length:      pieceLen,
		PieceLength: pieceLen,
		Pieces:      make([]byte, metainfo.HashSize),
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

	newClient := func() *Client {
		repo := &Repository{logger: util.NewLogger()}
		c := NewClient(repo)
		repo.client = c
		c.torrentClient = mo.Some(tc)
		t.Cleanup(func() { unregisterClient(c) })
		return c
	}
	previewer, streamer := newClient(), newClient()
	streamer.SetActiveStream("living-room-tv", tor, nil)

	require.False(t, previewer.dropIfUnclaimed(tor))
	select {
	case <-tor.Closed():
		t.Fatal("dropped a torrent another session is streaming")
	default:
	}

	streamer.RemoveActiveStream("living-room-tv")
	require.True(t, previewer.dropIfUnclaimed(tor))
	<-tor.Closed()
}
