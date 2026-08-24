package handlers

import (
	"net"
	"net/http"
	"seanime/internal/core"
	"seanime/internal/events"
	"seanime/internal/security"
	"time"

	"github.com/goccy/go-json"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

// wsReadDeadline bounds how long the server waits for ANY message (the client pings every 15s,
// see websocket-provider.tsx) before treating a native-player connection as dead. This exists
// because ws.ReadMessage() alone only reliably detects a *graceful* close (TCP FIN arrives
// promptly) - a hard crash, force-quit, or lost network can leave the connection half-open with
// no error ever surfacing, silently leaving any active torrent/debrid stream running forever
// (previously: until the next stream started, or a 2h idle-session timeout). A var (not const)
// so tests can shrink it instead of waiting out the real duration.
var wsReadDeadline = 60 * time.Second

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return false },
	}
)

// webSocketEventHandler creates a new websocket handler for real-time event communication
func (h *Handler) webSocketEventHandler(c echo.Context) error {
	req := c.Request()
	if !websocketUpgradeRateLimits.allow(websocketUpgradeRateLimitKey(req), maxWebsocketAttemptsPerWindow, websocketUpgradeWindow) {
		return c.JSON(http.StatusTooManyRequests, NewErrorResponse(errTooManyRequests))
	}

	// When a server password is set, require auth via query param
	if h.App.Config.Server.Password != "" {
		token := c.QueryParam("token")
		if token != h.App.ServerPasswordHash {
			authKey := authFailureRateLimitKey(req)
			if !authFailureRateLimits.allow(authKey, maxAuthFailuresPerWindow, authFailureWindow) {
				return c.JSON(http.StatusTooManyRequests, NewErrorResponse(errTooManyAuthenticationAttempts))
			}
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}

		authFailureRateLimits.reset(authFailureRateLimitKey(req))
	}

	contextClientId := getContextClientId(c)

	// Extract profile identity before upgrading.
	// Browsers send the session cookie on same-origin upgrade requests; query params are a fallback.
	profileID := ""
	authenticated := false
	tokenString := ""
	if cookie, err := c.Cookie("seanime-auth"); err == nil && cookie.Value != "" {
		tokenString = cookie.Value
	}
	if tokenString == "" {
		tokenString = c.QueryParam("auth_token")
	}
	if tokenString == "" {
		tokenString = c.QueryParam("token")
	}
	if tokenString != "" && h.App.JWTSecret != "" {
		claims, err := core.ParseToken(h.App.JWTSecret, tokenString)
		if err == nil && (claims.Scope == "profile" || claims.Scope == "admin") {
			profileID = claims.ProfileID
			authenticated = true
		}
	}

	// An authenticated multi-user session carries the same trust as a configured server
	// password - without this, a valid session hosted through a reverse proxy/custom domain
	// would still get rejected here since the origin isn't local/private/tailscale.
	if h.App.Config.Server.Password == "" && !authenticated {
		if !security.IsLax() && reqHasOriginMetadata(req) && !isRequestFromTrustedOrigin(req) && !isRequestFromAllowlistedOrigin(req, h.App.Config.Server.AccessAllowlist) {
			return c.JSON(http.StatusForbidden, NewErrorResponse(errPrivilegedExecutionDenied))
		}
	}

	// In multi-user mode, only authenticated profiles may connect:
	// broadcasts carry playback/scan/notification data and client events feed plugin handlers.
	if h.App.MultiUserEnabled && !authenticated {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}

	requestUpgrader := upgrader
	requestUpgrader.CheckOrigin = func(r *http.Request) bool {
		return isRequestPermitted(r, h.App.Config.Server.Password, h.App.Config.Server.AccessAllowlist, isAuthenticatedMultiUserSession(h.App, r))
	}

	ws, err := requestUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()

	// Get connection ID from query parameter
	id := contextClientId
	if id == "" {
		id = "0"
	}
	platform := getClientPlatformFromContext(c)

	// Add connection to manager
	h.App.WSEventManager.AddConn(id, profileID, ws, platform)
	h.App.Logger.Debug().Str("id", id).Str("profileID", profileID).Str("platform", platform).Msg("ws: Client connected")
	h.App.WSEventManager.SendEventTo(id, events.ClientIdentity, map[string]string{
		"clientId": id,
		"proof":    generateClientIdentityProof(h.App, id),
		"platform": platform,
	}, true)

	_ = ws.SetReadDeadline(time.Now().Add(wsReadDeadline))

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				h.App.Logger.Debug().Str("id", id).Msg("ws: Client disconnected")
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				h.App.Logger.Debug().Str("id", id).Msg("ws: Client connection timed out (no messages received)")
			} else {
				h.App.Logger.Debug().Str("id", id).Msg("ws: Client disconnection")
			}
			h.App.WSEventManager.RemoveConn(id)
			h.terminateClientNativePlayerStream(id)
			break
		}

		_ = ws.SetReadDeadline(time.Now().Add(wsReadDeadline))

		event, err := UnmarshalWebsocketClientEvent(msg)
		if err != nil {
			h.App.Logger.Error().Err(err).Msg("ws: Failed to unmarshal message sent from webview")
			continue
		}

		event.ClientID = id
		event.Payload = addClientIdToPayload(event.Payload, id)

		// Handle ping messages
		if event.Type == "ping" {
			timestamp := int64(0)
			if payload, ok := event.Payload.(map[string]interface{}); ok {
				if ts, ok := payload["timestamp"]; ok {
					if tsFloat, ok := ts.(float64); ok {
						timestamp = int64(tsFloat)
					} else if tsInt, ok := ts.(int64); ok {
						timestamp = tsInt
					}
				}
			}

			// Send pong response back to the same client
			h.App.WSEventManager.SendEventTo(id, "pong", map[string]int64{"timestamp": timestamp})
			continue // Skip further processing for ping messages
		}

		// Handle main-tab-claim messages by broadcasting to all clients
		if event.Type == "main-tab-claim" {
			h.App.WSEventManager.SendEvent("main-tab-claim", event.Payload)
			continue
		}

		h.HandleClientEvents(event)

		// h.App.Logger.Debug().Msgf("ws: message received: %+v", msg)

		// // Echo the message back
		// if err = ws.WriteMessage(messageType, msg); err != nil {
		// 	h.App.Logger.Err(err).Msg("ws: Failed to send message")
		// 	break
		// }
	}

	return nil
}

// terminateClientNativePlayerStream synthesizes a "video-terminated" client event for a
// disconnected websocket connection, reusing the exact same handling path a real
// dispatchTerminatedEvent() call from the frontend would take (see video-core-events.ts) - so an
// abruptly closed tab (browser crash, network loss, force-quit) stops any active native-player
// torrent/debrid/local stream the same way an in-app close does, instead of leaving it running
// until the next stream starts or a multi-hour idle timeout.
//
// The nested payload's id/playerType/playbackType are left empty on purpose: videocore.go fills
// them in from its own tracked playback state for this client when a real value isn't provided,
// exactly like it already does for a genuine (possibly partial) client-sent event.
func (h *Handler) terminateClientNativePlayerStream(clientId string) {
	if h.App.WSEventManager == nil {
		return
	}
	h.HandleClientEvents(&events.WebsocketClientEvent{
		ClientID: clientId,
		Type:     events.VideoCoreEventType,
		Payload: map[string]interface{}{
			"clientId": clientId,
			"type":     "video-terminated",
			"payload": map[string]interface{}{
				"id":           "",
				"clientId":     clientId,
				"playerType":   "",
				"playbackType": "",
			},
		},
	})
}

func UnmarshalWebsocketClientEvent(msg []byte) (*events.WebsocketClientEvent, error) {
	var event events.WebsocketClientEvent
	if err := json.Unmarshal(msg, &event); err != nil {
		return nil, err
	}
	return &event, nil
}

func addClientIdToPayload(value interface{}, clientID string) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, nested := range typed {
			if key == "clientId" {
				typed[key] = clientID
				continue
			}
			typed[key] = addClientIdToPayload(nested, clientID)
		}
		return typed
	case []interface{}:
		for index, nested := range typed {
			typed[index] = addClientIdToPayload(nested, clientID)
		}
		return typed
	default:
		return value
	}
}
