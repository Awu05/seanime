# Phase 5: WebSocket Multi-Profile Isolation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Route WebSocket events to the correct profile's connected clients instead of broadcasting to all connections.

**Architecture:** Extend `WSConn` with a `ProfileID` field. Authenticate WebSocket connections via JWT during handshake. Add `SendToProfile(profileId, event)` alongside existing `SendEvent` (broadcast) and `SendEventTo` (client-targeted). Migrate the ~138 `SendEvent` call sites to use the appropriate routing mode based on event semantics (profile-scoped vs broadcast).

**Tech Stack:** Go (gorilla/websocket, JWT auth)

---

## File Structure

### Modified Files

| File | Change |
|------|--------|
| `internal/events/websocket.go` | Add ProfileID to WSConn, add SendToProfile method, authenticate on connect |
| `internal/handlers/websocket.go` | Extract JWT from query param during WebSocket handshake, tag connection with profileId |

---

## Tasks

### Task 1: Extend WSConn with ProfileID

**Files:**
- Modify: `internal/events/websocket.go`

- [ ] **Step 1: Add ProfileID field to WSConn**

In `internal/events/websocket.go`, find the `WSConn` struct and add `ProfileID`:

```go
type WSConn struct {
	ID        string
	ProfileID string
	Conn      *websocket.Conn
}
```

- [ ] **Step 2: Add SendToProfile method**

Add this method to `WSEventManager`:

```go
// SendToProfile sends an event to all connections belonging to a specific profile.
func (m *WSEventManager) SendToProfile(profileID string, t string, payload interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, conn := range m.Conns {
		if conn.ProfileID == profileID {
			err := conn.Conn.WriteJSON(struct {
				Type    string      `json:"type"`
				Payload interface{} `json:"payload"`
			}{t, payload})
			if err != nil {
				m.Logger.Err(err).Str("connId", conn.ID).Msg("ws: Failed to send profile event")
			}
		}
	}
}
```

- [ ] **Step 3: Update AddConn to accept ProfileID**

Find the `AddConn` method and update its signature:

```go
func (m *WSEventManager) AddConn(id string, profileID string, conn *websocket.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Conns = append(m.Conns, &WSConn{ID: id, ProfileID: profileID, Conn: conn})
	m.hasHadConnection = true
}
```

- [ ] **Step 4: Verify build — fix any compile errors from AddConn signature change**

All callers of `AddConn` need the new `profileID` parameter. Find them and add `""` as the default:

```bash
go build ./internal/...
```

Fix any compilation errors by adding `""` as the profileID argument to existing `AddConn` callers.

- [ ] **Step 5: Commit**

```bash
git add internal/events/websocket.go
git commit -m "feat: add ProfileID to WSConn and SendToProfile method"
```

---

### Task 2: Authenticate WebSocket Connections

**Files:**
- Modify: `internal/handlers/websocket.go`

- [ ] **Step 1: Extract profile from JWT during WebSocket handshake**

In `internal/handlers/websocket.go`, find where `AddConn` is called. Before that call, extract the JWT from the query params and parse the profile ID:

```go
	// Extract profile from auth token
	profileID := ""
	authToken := c.QueryParam("auth_token")
	if authToken == "" {
		authToken = c.QueryParam("token")
	}
	if authToken != "" && h.App.JWTSecret != "" {
		claims, err := core.ParseToken(h.App.JWTSecret, authToken)
		if err == nil {
			profileID = claims.ProfileID
		}
	}

	// Register connection with profile
	h.App.WSEventManager.AddConn(id, profileID, ws)
```

Make sure to import `"seanime/internal/core"`.

- [ ] **Step 2: Verify build**

```bash
go build ./internal/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/handlers/websocket.go
git commit -m "feat: authenticate WebSocket connections with profile context"
```

---

### Task 3: Add WSEventManagerInterface Update

**Files:**
- Modify: `internal/events/websocket.go`

- [ ] **Step 1: Add SendToProfile to the interface**

Find the `WSEventManagerInterface` (if it exists) and add the `SendToProfile` method. If it's just a struct without an interface, skip this step.

Search for `WSEventManagerInterface` in the events package. If found, add:

```go
	SendToProfile(profileID string, t string, payload interface{})
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/events/
git commit -m "feat: add SendToProfile to WSEventManager interface"
```

---

### Task 4: Verify Full Build

- [ ] **Step 1: Verify Go backend compiles**

```bash
go build ./internal/...
```

Expected: No errors

Note: The actual migration of ~138 `SendEvent` call sites to `SendToProfile` is a large mechanical refactoring. Each call site needs to be evaluated: playback events → SendToProfile, scan progress → SendEvent (broadcast), native player → SendEventTo (client). This migration can be done incrementally in future sub-tasks. The infrastructure (SendToProfile method, profile-tagged connections) is now in place.
