package videocore

import (
	"seanime/internal/events"
	"seanime/internal/util"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newRoutedCore(r *ClientRouter) *VideoCore {
	return &VideoCore{router: r, clientPlayerEventSubscriber: events.NewClientEventSubscriber(10)}
}

func playerEvent(clientID string, t ClientEventType) *events.WebsocketClientEvent {
	return &events.WebsocketClientEvent{
		ClientID: clientID,
		Type:     events.VideoCoreEventType,
		Payload:  map[string]any{"clientId": clientID, "type": string(t)},
	}
}

func received(core *VideoCore) int {
	return len(core.clientPlayerEventSubscriber.Channel)
}

// TestClientRouter guards browser player event delivery. Every VideoCore (the app-wide one and
// one per profile session) used to subscribe under the same name, so only the newest core heard
// the browser player - every other profile's playback went untracked server-side.
func TestClientRouter(t *testing.T) {
	newRouter := func() (*ClientRouter, *VideoCore, *VideoCore, *VideoCore) {
		ws := events.NewMockWSEventManager(util.NewLogger())
		ws.ClientProfileIDs = map[string]string{"tv": "alice", "phone": "bob"}
		r := NewClientRouter(ws, util.NewLogger())
		appCore, alice, bob := newRoutedCore(r), newRoutedCore(r), newRoutedCore(r)
		r.SetFallback(appCore)
		r.RegisterProfile("alice", alice)
		r.RegisterProfile("bob", bob)
		return r, appCore, alice, bob
	}

	t.Run("each profile's events reach only its own session core", func(t *testing.T) {
		r, appCore, alice, bob := newRouter()
		r.route(playerEvent("tv", PlayerEventVideoStatus))
		r.route(playerEvent("phone", PlayerEventVideoStatus))
		r.route(playerEvent("phone", PlayerEventVideoStatus))
		assert.Equal(t, 1, received(alice))
		assert.Equal(t, 2, received(bob))
		assert.Equal(t, 0, received(appCore))
	})

	t.Run("a client goes to the core that sent it its stream until it terminates", func(t *testing.T) {
		r, appCore, alice, _ := newRouter()
		appCore.ClaimClient("tv")

		r.route(playerEvent("tv", PlayerEventVideoStatus))
		r.route(playerEvent("tv", PlayerEventVideoTerminated))
		r.route(playerEvent("tv", PlayerEventVideoStatus))

		assert.Equal(t, 2, received(appCore))
		assert.Equal(t, 1, received(alice))
	})

	t.Run("a superseded claim can't release the newer one", func(t *testing.T) {
		r, appCore, alice, _ := newRouter()
		alice.ClaimClient("tv")
		appCore.ClaimClient("tv")
		r.unbind("tv", alice)

		r.route(playerEvent("tv", PlayerEventVideoStatus))
		assert.Equal(t, 1, received(appCore))
	})

	t.Run("clients without a session core fall back to the app-wide core", func(t *testing.T) {
		r, appCore, _, _ := newRouter()
		r.route(playerEvent("unknown-client", PlayerEventVideoStatus))
		assert.Equal(t, 1, received(appCore))
	})

	t.Run("a detached core stops receiving and its channel closes", func(t *testing.T) {
		r, appCore, alice, _ := newRouter()
		alice.ClaimClient("tv")
		alice.DetachFromRouter()

		r.route(playerEvent("tv", PlayerEventVideoStatus))
		assert.Equal(t, 1, received(appCore))
		_, open := <-alice.clientPlayerEventSubscriber.Channel
		assert.False(t, open)
	})
}
