package torrentstream

import (
	"net/url"
	"seanime/internal/directstream"
	"seanime/internal/util"
	"strings"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
)

// TestExternalPlayerStreamingUrlCarriesTheStreamToken guards external player links (VLC, mpv):
// they can't send the login cookie, so the URL itself must carry the profile's stream token -
// and a well-formed query even when no server-password HMAC token is added.
func TestExternalPlayerStreamingUrlCarriesTheStreamToken(t *testing.T) {
	tc := newTestTorrentClient(t)
	tor := addTestTorrent(t, tc, "Show - 01.mkv")

	repo := &Repository{
		logger:              util.NewLogger(),
		directStreamManager: &directstream.Manager{},
		streamTokenFunc:     func() string { return "stream-token" },
	}
	repo.client = NewClient(repo)
	t.Cleanup(func() { unregisterClient(repo.client) })
	repo.client.torrentClient.Store(tc)
	repo.client.currentFile = mo.Some(tor.Files()[0])

	raw := repo.client.GetExternalPlayerStreamingUrl("living-room-tv")
	u, err := url.Parse(strings.NewReplacer("{{SCHEME}}", "http", "{{HOST}}", "seanime.local").Replace(raw))
	require.NoError(t, err)
	require.Equal(t, "/api/v1/torrentstream/stream/Show - 01.mkv", u.Path)
	require.Equal(t, "stream-token", u.Query().Get("auth_token"))
	require.Equal(t, "living-room-tv", u.Query().Get("clientId"))
}
