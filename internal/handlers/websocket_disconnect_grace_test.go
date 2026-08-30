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

// setupGraceTestServer starts a real websocket server backed by a fresh Handler/WSEventManager,
// mirroring the setup in TestWebSocketDeadConnectionTriggersNativePlayerTermination.
func setupGraceTestServer(t *testing.T) (wsURL string, subscriber *events.ClientEventSubscriber) {
	t.Helper()
	security.SetSecureMode("lax")
	t.Cleanup(func() { security.SetSecureMode("") })

	logger := util.NewLogger()
	wsEventManager := events.NewWSEventManager(logger)
	subscriber = wsEventManager.SubscribeToClientVideoCoreEvents("videocore")

	h := &Handler{App: &core.App{
		Logger:         logger,
		WSEventManager: wsEventManager,
		Config:         &core.Config{},
	}}

	e := echo.New()
	e.GET("/ws", h.webSocketEventHandler)
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)

	wsURL = "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	return wsURL, subscriber
}

// TestWebSocketDisconnect_ReconnectWithinGraceCancelsTermination guards the fix for a reported
// bug: a client's own heartbeat closes and reopens its websocket whenever it misses pongs for too
// long (a self-healing blip, usually resolved in 1-3s) - previously this abrupt disconnect was
// treated identically to a truly dead connection (crash, force-quit) and instantly tore down the
// client's active torrent/native-player stream, even though the client reconnected moments later.
// A brief grace period should let a quick reconnect (same clientId) cancel the pending teardown.
func TestWebSocketDisconnect_ReconnectWithinGraceCancelsTermination(t *testing.T) {
	previousGrace := wsDisconnectGracePeriod
	wsDisconnectGracePeriod = 300 * time.Millisecond
	t.Cleanup(func() { wsDisconnectGracePeriod = previousGrace })

	wsURL, subscriber := setupGraceTestServer(t)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	// Close() drops the underlying connection without a clean close frame, matching a client
	// heartbeat's abrupt reconnect rather than a graceful close.
	require.NoError(t, conn.Close())

	// Reconnect well within the grace period, using the same (default) clientId.
	time.Sleep(50 * time.Millisecond)
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn2.Close() })

	select {
	case <-subscriber.Channel:
		t.Fatal("expected no video-terminated event when the client reconnects within the grace period")
	case <-time.After(wsDisconnectGracePeriod + 300*time.Millisecond):
		// Grace period elapsed with no termination event - the reconnect cancelled it.
	}
}

// TestWebSocketDisconnect_NoReconnectTerminatesAfterGrace ensures the grace period is not an
// unconditional reprieve: a client that never comes back must still have its stream cleaned up,
// just after the grace window instead of instantly.
func TestWebSocketDisconnect_NoReconnectTerminatesAfterGrace(t *testing.T) {
	previousGrace := wsDisconnectGracePeriod
	wsDisconnectGracePeriod = 150 * time.Millisecond
	t.Cleanup(func() { wsDisconnectGracePeriod = previousGrace })

	wsURL, subscriber := setupGraceTestServer(t)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	select {
	case event := <-subscriber.Channel:
		require.Equal(t, events.VideoCoreEventType, event.Type)
		payload, ok := event.Payload.(map[string]interface{})
		require.True(t, ok, "expected a map payload")
		require.Equal(t, "video-terminated", payload["type"])
	case <-time.After(2 * time.Second):
		t.Fatal("expected video-terminated once the grace period elapsed with no reconnect")
	}
}
