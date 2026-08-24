package torrentstream

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"seanime/internal/util"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
)

// TestAddTorrentFromDownloadURLTimesOutOnHangingServer guards against a regression where
// addTorrentFromDownloadURL used a bare http.Get with no deadline, so a .torrent-file host
// that accepts the connection but never sends a response body could hang StartStream forever.
func TestAddTorrentFromDownloadURLTimesOutOnHangingServer(t *testing.T) {
	blockCh := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockCh // never respond until the test cleans up
	}))
	// t.Cleanup runs LIFO: srv.Close() blocks until in-flight handlers return, so blockCh
	// must be closed (registered second, runs first) before srv.Close() (registered first,
	// runs last) - otherwise Close() deadlocks waiting on the handler that's waiting on it.
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(blockCh) })

	previousTimeout := torrentURLFetchTimeout
	torrentURLFetchTimeout = 50 * time.Millisecond
	t.Cleanup(func() { torrentURLFetchTimeout = previousTimeout })

	repo := &Repository{logger: util.NewLogger()}
	c := NewClient(repo)
	t.Cleanup(func() { unregisterClient(c) })

	cfg := torrent.TestingConfig(t)
	cfg.DisableTCP = true
	cfg.DisableUTP = true
	tc, err := torrent.NewClient(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { tc.Close() })
	c.torrentClient = mo.Some(tc)

	done := make(chan error, 1)
	go func() {
		_, err := c.addTorrentFromDownloadURL(srv.URL)
		done <- err
	}()

	select {
	case err := <-done:
		require.Error(t, err, "expected a timeout error rather than success")
	case <-time.After(2 * time.Second):
		t.Fatal("addTorrentFromDownloadURL did not return within the deadline - it hung on the unresponsive server")
	}
}

// TestAddTorrentFromDownloadURLCleansUpTempFile guards against a regression where
// addTorrentFromDownloadURL wrote the fetched .torrent file into os.TempDir() and never removed
// it, leaking one file per download-URL torrent add for the life of the host process.
func TestAddTorrentFromDownloadURLCleansUpTempFile(t *testing.T) {
	infoBytes, err := bencode.Marshal(metainfo.Info{
		Name:        t.Name(),
		Length:      1024,
		PieceLength: 1024,
		Pieces:      make([]byte, metainfo.HashSize),
	})
	require.NoError(t, err)
	torrentBytes, err := bencode.Marshal(metainfo.MetaInfo{InfoBytes: infoBytes})
	require.NoError(t, err)

	// A basename unique to this test run, so the glob below can't match a leftover file from
	// some other test or a previous run.
	basename := t.Name() + "-unique.torrent"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(torrentBytes)
	}))
	t.Cleanup(srv.Close)

	repo := &Repository{logger: util.NewLogger()}
	c := NewClient(repo)
	t.Cleanup(func() { unregisterClient(c) })

	cfg := torrent.TestingConfig(t)
	cfg.DisableTCP = true
	cfg.DisableUTP = true
	tc, err := torrent.NewClient(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { tc.Close() })
	c.torrentClient = mo.Some(tc)

	_, err = c.addTorrentFromDownloadURL(srv.URL + "/" + basename)
	require.NoError(t, err)

	leftover, err := filepath.Glob(filepath.Join(os.TempDir(), "*"+basename+"*"))
	require.NoError(t, err)
	require.Empty(t, leftover, "addTorrentFromDownloadURL left a temp file behind: %v", leftover)
}
