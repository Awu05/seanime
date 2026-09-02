package handlers

import (
	"errors"
	"seanime/internal/database/models"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldTestQbittorrentConnection(t *testing.T) {
	t.Run("qbittorrent is default and host is set", func(t *testing.T) {
		assert.True(t, shouldTestQbittorrentConnection(&models.TorrentSettings{
			Default:         "qbittorrent",
			QBittorrentHost: "127.0.0.1",
		}))
	})

	t.Run("re-saving unchanged settings still tests the connection", func(t *testing.T) {
		// A host that was already saved (and worked before) but is unreachable right now
		// must still block the save - otherwise a save while qBittorrent is down goes
		// through silently with no feedback, which is the bug this guards against.
		unchanged := &models.TorrentSettings{
			Default:         "qbittorrent",
			QBittorrentHost: "192.168.68.65",
			QBittorrentPort: 8084,
		}
		assert.True(t, shouldTestQbittorrentConnection(unchanged))
	})

	t.Run("transmission is default", func(t *testing.T) {
		assert.False(t, shouldTestQbittorrentConnection(&models.TorrentSettings{
			Default:         "transmission",
			QBittorrentHost: "127.0.0.1",
		}))
	})

	t.Run("host is empty", func(t *testing.T) {
		assert.False(t, shouldTestQbittorrentConnection(&models.TorrentSettings{
			Default:         "qbittorrent",
			QBittorrentHost: "",
		}))
	})
}

func TestQbittorrentTestConnectionBody_ToTorrentSettings(t *testing.T) {
	b := qbittorrentTestConnectionBody{
		Host:     "192.168.68.65",
		Port:     8084,
		Username: "admin",
		Password: "secret",
		Path:     "/downloads",
	}
	got := b.toTorrentSettings()
	assert.Equal(t, "192.168.68.65", got.QBittorrentHost)
	assert.Equal(t, 8084, got.QBittorrentPort)
	assert.Equal(t, "admin", got.QBittorrentUsername)
	assert.Equal(t, "secret", got.QBittorrentPassword)
	assert.Equal(t, "/downloads", got.QBittorrentPath)
}

func TestTestQbittorrentConnection(t *testing.T) {
	t.Run("returns the login error unchanged", func(t *testing.T) {
		wantErr := errors.New("invalid status 403 Forbidden")
		err := testQbittorrentConnection(func() error { return wantErr })
		require.ErrorIs(t, err, wantErr)
	})

	t.Run("succeeds when login succeeds", func(t *testing.T) {
		err := testQbittorrentConnection(func() error { return nil })
		require.NoError(t, err)
	})

	t.Run("fails closed when login hangs past the timeout", func(t *testing.T) {
		previousTimeout := qbittorrentConnectionTestTimeout
		qbittorrentConnectionTestTimeout = 50 * time.Millisecond
		t.Cleanup(func() { qbittorrentConnectionTestTimeout = previousTimeout })

		block := make(chan struct{})
		t.Cleanup(func() { close(block) })

		err := testQbittorrentConnection(func() error {
			<-block
			return nil
		})
		require.Error(t, err)
	})
}
