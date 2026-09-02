package handlers

import (
	"errors"
	"seanime/internal/database/models"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQbittorrentConnectionSettingsChanged(t *testing.T) {
	base := &models.TorrentSettings{
		QBittorrentHost:     "127.0.0.1",
		QBittorrentPort:     8081,
		QBittorrentUsername: "admin",
		QBittorrentPassword: "secret",
		QBittorrentPath:     "",
	}

	t.Run("no previous settings", func(t *testing.T) {
		assert.True(t, qbittorrentConnectionSettingsChanged(nil, base))
	})

	t.Run("identical settings", func(t *testing.T) {
		same := *base
		assert.False(t, qbittorrentConnectionSettingsChanged(base, &same))
	})

	t.Run("host changed", func(t *testing.T) {
		next := *base
		next.QBittorrentHost = "192.168.1.5"
		assert.True(t, qbittorrentConnectionSettingsChanged(base, &next))
	})

	t.Run("port changed", func(t *testing.T) {
		next := *base
		next.QBittorrentPort = 9999
		assert.True(t, qbittorrentConnectionSettingsChanged(base, &next))
	})

	t.Run("credentials changed", func(t *testing.T) {
		next := *base
		next.QBittorrentPassword = "different"
		assert.True(t, qbittorrentConnectionSettingsChanged(base, &next))
	})

	t.Run("unrelated field changed", func(t *testing.T) {
		next := *base
		next.QBittorrentTags = "seanime"
		assert.False(t, qbittorrentConnectionSettingsChanged(base, &next),
			"tags/category don't affect connectivity and shouldn't trigger a retest")
	})
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
