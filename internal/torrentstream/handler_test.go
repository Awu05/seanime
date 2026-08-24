package torrentstream

import (
	"net/http"
	"net/http/httptest"
	"seanime/internal/util"
	"testing"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
)

func addTestTorrent(t *testing.T, tc *torrent.Client, name string) *torrent.Torrent {
	t.Helper()
	const (
		pieceLen = int64(1 << 20)
		pieces   = 4
	)
	infoBytes, err := bencode.Marshal(metainfo.Info{
		Name:        name,
		Length:      pieceLen * pieces,
		PieceLength: pieceLen,
		Pieces:      make([]byte, metainfo.HashSize*pieces),
	})
	require.NoError(t, err)
	tor, err := tc.AddTorrent(&metainfo.MetaInfo{InfoBytes: infoBytes})
	require.NoError(t, err)
	return tor
}

func newTestHandler(t *testing.T) (*handler, *torrent.Client) {
	t.Helper()
	repo := &Repository{logger: util.NewLogger()}
	repo.client = NewClient(repo)
	repo.handler = newHandler(repo)
	t.Cleanup(func() { unregisterClient(repo.client) })

	cfg := torrent.TestingConfig(t)
	cfg.DisableTCP = true
	cfg.DisableUTP = true
	tc, err := torrent.NewClient(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { tc.Close() })
	repo.client.torrentClient = mo.Some(tc)

	return repo.handler, tc
}

// TestResolveStreamTargetPrefersClientScopedStream guards against the cross-wire bug: two
// devices/tabs on the same profile playing different episodes shared one legacy
// currentTorrent/currentFile pair, so the handler could serve the wrong client's episode. When
// the request carries a clientId that has a registered activeStream, that must win over
// whatever the shared legacy fields currently point to.
func TestResolveStreamTargetPrefersClientScopedStream(t *testing.T) {
	h, tc := newTestHandler(t)

	legacyTorrent := addTestTorrent(t, tc, "legacy-current.mkv")
	clientTorrent := addTestTorrent(t, tc, "client-a-episode.mkv")

	// Simulates another tab/device having since become "current" for the profile.
	h.repository.client.currentTorrent = mo.Some(legacyTorrent)
	h.repository.client.currentFile = mo.Some(legacyTorrent.Files()[0])

	// This client's own stream, registered when its StartStream call ran.
	h.repository.client.SetActiveStream("client-a", clientTorrent, clientTorrent.Files()[0])

	req := httptest.NewRequest(http.MethodGet, "/api/v1/torrentstream/stream/x?clientId=client-a", nil)
	resolvedTorrent, resolvedFile, ok := h.resolveStreamTarget(req)

	require.True(t, ok)
	require.Equal(t, clientTorrent.InfoHash(), resolvedTorrent.InfoHash(), "expected the client-scoped stream to win over the shared legacy fields")
	require.Equal(t, clientTorrent.Files()[0].DisplayPath(), resolvedFile.DisplayPath())
}

// TestResolveStreamTargetFallsBackToLegacyFieldsWithoutClientId guards backward compatibility:
// requests with no clientId (or an unregistered one - e.g. an external-player URL generated
// before this field existed) must still work by falling back to the shared legacy fields.
func TestResolveStreamTargetFallsBackToLegacyFieldsWithoutClientId(t *testing.T) {
	h, tc := newTestHandler(t)

	legacyTorrent := addTestTorrent(t, tc, "legacy-current.mkv")
	h.repository.client.currentTorrent = mo.Some(legacyTorrent)
	h.repository.client.currentFile = mo.Some(legacyTorrent.Files()[0])

	req := httptest.NewRequest(http.MethodGet, "/api/v1/torrentstream/stream/x", nil)
	resolvedTorrent, _, ok := h.resolveStreamTarget(req)

	require.True(t, ok)
	require.Equal(t, legacyTorrent.InfoHash(), resolvedTorrent.InfoHash())
}

// TestResolveStreamTargetFallsBackWhenClientIdNotRegistered guards the same backward-
// compatibility path when a clientId is present but has no matching activeStream entry.
func TestResolveStreamTargetFallsBackWhenClientIdNotRegistered(t *testing.T) {
	h, tc := newTestHandler(t)

	legacyTorrent := addTestTorrent(t, tc, "legacy-current.mkv")
	h.repository.client.currentTorrent = mo.Some(legacyTorrent)
	h.repository.client.currentFile = mo.Some(legacyTorrent.Files()[0])

	req := httptest.NewRequest(http.MethodGet, "/api/v1/torrentstream/stream/x?clientId=unknown-client", nil)
	resolvedTorrent, _, ok := h.resolveStreamTarget(req)

	require.True(t, ok)
	require.Equal(t, legacyTorrent.InfoHash(), resolvedTorrent.InfoHash())
}

// TestResolveStreamTargetNotOkWhenNothingToServe guards the 404 path when neither a
// client-scoped stream nor the legacy fields have anything to serve.
func TestResolveStreamTargetNotOkWhenNothingToServe(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/torrentstream/stream/x", nil)
	_, _, ok := h.resolveStreamTarget(req)

	require.False(t, ok)
}
