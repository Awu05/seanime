package handlers

import (
	"net/http/httptest"
	"seanime/internal/core"
	"seanime/internal/events"
	"seanime/internal/security"
	"seanime/internal/util"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// TestTerminateClientNativePlayerStreamDispatchesVideoTerminatedEvent guards the fix for a
// reported bug: closing the native (in-browser) player normally sends a "video-terminated"
// event over the websocket, which stops any active torrent/debrid/local stream for that client.
// But an abrupt disconnect (tab closed, browser crash, network loss) never sent that event at
// all, leaving the stream running until the next one started or a multi-hour idle timeout.
//
// terminateClientNativePlayerStream synthesizes the same event a graceful close would send, so
// the websocket disconnect handler (and the read-deadline liveness check) can route through the
// exact same, already-proven cleanup path instead of needing new coupling to torrentstream/
// directstream. This test only verifies the synthesized event reaches the right subscriber with
// the right shape - the downstream handling of "video-terminated" itself is covered elsewhere.
func TestTerminateClientNativePlayerStreamDispatchesVideoTerminatedEvent(t *testing.T) {
	logger := util.NewLogger()
	wsEventManager := events.NewWSEventManager(logger)
	h := &Handler{App: &core.App{WSEventManager: wsEventManager, Logger: logger}}

	subscriber := wsEventManager.SubscribeToClientVideoCoreEvents("client-1")

	h.terminateClientNativePlayerStream("client-1")

	select {
	case event := <-subscriber.Channel:
		require.Equal(t, events.VideoCoreEventType, event.Type)
		require.Equal(t, "client-1", event.ClientID)

		payload, ok := event.Payload.(map[string]interface{})
		require.True(t, ok, "expected a map payload")
		require.Equal(t, "video-terminated", payload["type"])
		require.Equal(t, "client-1", payload["clientId"])

		inner, ok := payload["payload"].(map[string]interface{})
		require.True(t, ok, "expected a nested payload matching the real dispatchTerminatedEvent shape")
		require.Equal(t, "client-1", inner["clientId"])
	case <-time.After(time.Second):
		t.Fatal("expected a video-terminated event to be dispatched to the client-1 videocore subscriber")
	}
}

// TestWebSocketDeadConnectionTriggersNativePlayerTermination is the end-to-end regression test
// for the actual reported bug: a client that stops responding entirely (network loss, crash,
// force-quit - anything short of a graceful close, which the browser doesn't always send) used
// to leave the server with no way to ever notice, so any active native-player stream for that
// client just kept running. It connects a real websocket client, sends nothing, and asserts the
// server-side read deadline fires and dispatches the same video-terminated cleanup a graceful
// close would.
func TestWebSocketDeadConnectionTriggersNativePlayerTermination(t *testing.T) {
	security.SetSecureMode("lax")
	t.Cleanup(func() { security.SetSecureMode("") })

	previousDeadline := wsReadDeadline
	wsReadDeadline = 150 * time.Millisecond
	t.Cleanup(func() { wsReadDeadline = previousDeadline })

	previousGrace := wsDisconnectGracePeriod
	wsDisconnectGracePeriod = 150 * time.Millisecond
	t.Cleanup(func() { wsDisconnectGracePeriod = previousGrace })

	logger := util.NewLogger()
	wsEventManager := events.NewWSEventManager(logger)
	subscriber := wsEventManager.SubscribeToClientVideoCoreEvents("videocore")

	h := &Handler{App: &core.App{
		Logger:         logger,
		WSEventManager: wsEventManager,
		Config:         &core.Config{},
	}}

	e := echo.New()
	e.GET("/ws", h.webSocketEventHandler)
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	// Deliberately send nothing and let the server-side read deadline expire, simulating a
	// connection that died without a graceful close ever reaching the server.
	select {
	case event := <-subscriber.Channel:
		require.Equal(t, events.VideoCoreEventType, event.Type)
		payload, ok := event.Payload.(map[string]interface{})
		require.True(t, ok, "expected a map payload")
		require.Equal(t, "video-terminated", payload["type"], "expected the dead connection to trigger the same cleanup a graceful close would")
	case <-time.After(3 * time.Second):
		t.Fatal("expected the server to detect the dead connection (via the shrunk read deadline) and dispatch video-terminated")
	}
}
