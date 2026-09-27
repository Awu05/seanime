package torrentstream

import (
	"seanime/internal/events"
	"seanime/internal/mediaplayers/mediaplayer"
	"seanime/internal/nativeplayer"
	"seanime/internal/util"
	"seanime/internal/videocore"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDesktopPlayerListener guards desktop media player event handling, which every profile's
// repository shares one player for. All repositories subscribed under the same name, so only the
// newest heard the player; and StopStream cancelled the listener, so after the first stopped
// stream a repository never heard the player again.
func TestDesktopPlayerListener(t *testing.T) {
	logger := util.NewLogger()

	t.Run("each repository subscribes under its own name", func(t *testing.T) {
		a := NewRepository(&NewRepositoryOptions{Logger: logger})
		b := NewRepository(&NewRepositoryOptions{Logger: logger})
		t.Cleanup(func() { unregisterClient(a.client); unregisterClient(b.client) })
		require.NotEqual(t, a.mediaPlayerSubscriberID, b.mediaPlayerSubscriberID)
	})

	t.Run("StopStream keeps listening to the desktop player", func(t *testing.T) {
		ws := events.NewMockWSEventManager(logger)
		repo := &Repository{
			logger:                  logger,
			wsEventManager:          ws,
			mediaPlayerSubscriberID: "torrentstream-test",
			nativePlayer:            nativeplayer.New(nativeplayer.NewNativePlayerOptions{VideoCore: &videocore.VideoCore{}}),
		}
		repo.client = NewClient(repo)
		t.Cleanup(func() { unregisterClient(repo.client) })
		repo.SetMediaPlayerRepository(mediaplayer.NewRepository(&mediaplayer.NewRepositoryOptions{Logger: logger, WSEventManager: ws}))
		t.Cleanup(repo.stopMediaPlayerListener)

		require.NoError(t, repo.StopStream())

		repo.playback.listenerMu.Lock()
		listening := repo.playback.mediaPlayerCtxCancelFunc != nil
		repo.playback.listenerMu.Unlock()
		require.True(t, listening)
	})
}
