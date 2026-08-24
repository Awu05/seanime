package torrentutil

import (
	"context"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func newTestTorrent(t *testing.T) (*torrent.Torrent, *torrent.File) {
	t.Helper()

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
	client, err := torrent.NewClient(cfg)
	require.NoError(t, err)

	tor, err := client.AddTorrent(&metainfo.MetaInfo{InfoBytes: infoBytes})
	require.NoError(t, err)
	require.Len(t, tor.Files(), 1)

	file := tor.Files()[0]
	file.Download()

	t.Cleanup(func() {
		tor.Drop()
		client.Close()
	})

	return tor, file
}

func TestPriorityManagerOnlyUpdatesActiveWindows(t *testing.T) {
	tor, file := newTestTorrent(t)
	logger := zerolog.Nop()
	pm := &priorityManager{
		readers:   make(map[string]*readerInfo),
		torrent:   tor,
		file:      file,
		logger:    &logger,
		createdAt: time.Now().Add(-2 * time.Minute),
		set:       make(map[int64]torrent.PiecePriority),
	}

	const farPiece = 200
	pm.registerReader("reader", 0)
	require.NotContains(t, pm.set, int64(farPiece))
	require.Equal(t, torrent.PiecePriorityNow, pm.set[0])

	pm.updateReaderPosition("reader", 128<<20)
	require.NotContains(t, pm.set, int64(0))
	require.Equal(t, torrent.PiecePriorityNow, pm.set[127])

	pm.unregisterReader("reader")
	require.Empty(t, pm.set)
}

func TestReadSeekerStopsOnContextCancel(t *testing.T) {
	tor, file := newTestTorrent(t)
	reader := NewReadSeeker(tor, file)
	ctx, cancel := context.WithCancel(context.Background())
	reader.SetContext(ctx)

	result := make(chan error, 1)
	go func() {
		_, err := reader.Read(make([]byte, 1))
		result <- err
	}()

	cancel()

	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("expected torrent read to stop")
	}

	require.NoError(t, reader.Close())
	require.NoError(t, reader.Close())
}

func TestImmediatePlaybackPieceRangeMatchesPrioritizedNowWindow(t *testing.T) {
	tor, file := newTestTorrent(t)

	// newTestTorrent: 1MB pieces, 256 pieces, single file starting at offset 0.
	// The "Now" window is the first 8MB -> pieces [0, 8].
	first, immediateEnd, ok := immediatePlaybackPieceRange(tor, file)
	require.True(t, ok)
	require.EqualValues(t, 0, first)
	require.EqualValues(t, 8, immediateEnd)
}

func TestImmediatePiecesCompleteFalseWhenNothingDownloaded(t *testing.T) {
	tor, file := newTestTorrent(t)

	// A freshly added torrent with no peers has downloaded nothing, so the pieces needed to
	// start playback aren't done - this must not report ready.
	require.False(t, ImmediatePiecesComplete(tor, file))
}
