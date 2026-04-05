# Phase 4: Per-Profile Streaming Sessions — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor streaming singletons (NativePlayer, PlaybackManager, DirectStreamManager, TorrentStream, VideoCore) into per-profile session pools so multiple profiles can stream simultaneously.

**Architecture:** New `StreamSessionManager` on the App struct replaces individual singleton fields. It maintains a `map[profileId]*ProfileStreamSession` where each session holds its own NativePlayer, PlaybackManager, DirectStreamManager, and VideoCore. Sessions are created lazily on first stream request and cleaned up after inactivity. The underlying anacrolix torrent engine remains shared (single process). Handlers call `h.App.StreamSessionManager.GetOrCreateSession(profileID)` instead of `h.App.NativePlayer` etc.

**Tech Stack:** Go (sync.RWMutex, time.Ticker for cleanup)

---

## File Structure

### New Files

| File | Responsibility |
|------|---------------|
| `internal/core/stream_session.go` | ProfileStreamSession struct and StreamSessionManager with lazy creation, cleanup, and accessor methods |

### Modified Files

| File | Change |
|------|--------|
| `internal/core/app.go` | Add `StreamSessionManager` field |
| `internal/core/modules.go` | Initialize StreamSessionManager, keep singletons for backward compat |
| `internal/handlers/mediaplayer.go` | Use session-scoped PlaybackManager |
| `internal/handlers/directstream.go` | Use session-scoped DirectStreamManager/NativePlayer |
| `internal/handlers/torrentstream.go` | Use session-scoped TorrentStream |
| `internal/handlers/mediastream.go` | Use session-scoped instances |

---

## Tasks

### Task 1: StreamSessionManager and ProfileStreamSession

**Files:**
- Create: `internal/core/stream_session.go`

- [ ] **Step 1: Create the session manager**

Create `internal/core/stream_session.go`:

```go
package core

import (
	"sync"
	"time"
)

// ProfileStreamSession holds per-profile streaming state.
// Each profile gets its own session when they start streaming.
type ProfileStreamSession struct {
	ProfileID  string
	LastActive time.Time
	// Streaming components will be added here as they're refactored.
	// For now this is the scaffolding.
}

// StreamSessionManager manages per-profile streaming sessions.
type StreamSessionManager struct {
	sessions       map[string]*ProfileStreamSession
	mu             sync.RWMutex
	cleanupTicker  *time.Ticker
	cleanupDone    chan struct{}
	inactivityTimeout time.Duration
}

// NewStreamSessionManager creates a new session manager with periodic cleanup.
func NewStreamSessionManager(inactivityTimeout time.Duration) *StreamSessionManager {
	sm := &StreamSessionManager{
		sessions:          make(map[string]*ProfileStreamSession),
		inactivityTimeout: inactivityTimeout,
		cleanupTicker:     time.NewTicker(5 * time.Minute),
		cleanupDone:       make(chan struct{}),
	}

	go sm.cleanupLoop()
	return sm
}

// GetOrCreateSession returns the session for a profile, creating one if needed.
func (sm *StreamSessionManager) GetOrCreateSession(profileID string) *ProfileStreamSession {
	if profileID == "" {
		profileID = "_default"
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[profileID]
	if !exists {
		session = &ProfileStreamSession{
			ProfileID:  profileID,
			LastActive: time.Now(),
		}
		sm.sessions[profileID] = session
	} else {
		session.LastActive = time.Now()
	}

	return session
}

// GetSession returns the session for a profile if it exists, nil otherwise.
func (sm *StreamSessionManager) GetSession(profileID string) *ProfileStreamSession {
	if profileID == "" {
		profileID = "_default"
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return sm.sessions[profileID]
}

// RemoveSession removes a profile's session.
func (sm *StreamSessionManager) RemoveSession(profileID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.sessions, profileID)
}

// GetActiveSessions returns all active sessions.
func (sm *StreamSessionManager) GetActiveSessions() []*ProfileStreamSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sessions := make([]*ProfileStreamSession, 0, len(sm.sessions))
	for _, s := range sm.sessions {
		sessions = append(sessions, s)
	}
	return sessions
}

// cleanupLoop periodically removes inactive sessions.
func (sm *StreamSessionManager) cleanupLoop() {
	for {
		select {
		case <-sm.cleanupTicker.C:
			sm.mu.Lock()
			now := time.Now()
			for id, session := range sm.sessions {
				if now.Sub(session.LastActive) > sm.inactivityTimeout {
					delete(sm.sessions, id)
				}
			}
			sm.mu.Unlock()
		case <-sm.cleanupDone:
			return
		}
	}
}

// Stop stops the cleanup loop.
func (sm *StreamSessionManager) Stop() {
	sm.cleanupTicker.Stop()
	close(sm.cleanupDone)
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/core/stream_session.go
git commit -m "feat: add StreamSessionManager for per-profile streaming sessions"
```

---

### Task 2: Register StreamSessionManager on App

**Files:**
- Modify: `internal/core/app.go`
- Modify: `internal/core/modules.go`

- [ ] **Step 1: Add StreamSessionManager field to App struct**

In `internal/core/app.go`, add to the App struct near the streaming fields:

```go
	StreamSessionManager *StreamSessionManager
```

- [ ] **Step 2: Initialize StreamSessionManager in modules.go**

In `internal/core/modules.go`, in the `initModulesOnce()` function (or `InitOrRefreshModules`), add initialization:

```go
	// Initialize stream session manager
	if a.StreamSessionManager == nil {
		a.StreamSessionManager = NewStreamSessionManager(30 * time.Minute)
	}
```

Place this early in the initialization, before the streaming singletons.

- [ ] **Step 3: Verify build**

```bash
go build ./internal/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/core/app.go internal/core/modules.go
git commit -m "feat: register StreamSessionManager on App struct"
```

---

### Task 3: Add Handler Helper for Session Access

**Files:**
- Create: `internal/handlers/session_helper.go`

- [ ] **Step 1: Create session helper**

Create `internal/handlers/session_helper.go`:

```go
package handlers

import (
	"seanime/internal/core"

	"github.com/labstack/echo/v4"
)

// getStreamSession returns the streaming session for the current profile.
// Falls back to the default session for desktop sidecar single-user mode.
func (h *Handler) getStreamSession(c echo.Context) *core.ProfileStreamSession {
	profileID := core.GetProfileIDFromContext(c)
	return h.App.StreamSessionManager.GetOrCreateSession(profileID)
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/handlers/session_helper.go
git commit -m "feat: add handler helper for stream session access"
```

---

### Task 4: Verify Full Build

- [ ] **Step 1: Verify Go backend compiles**

```bash
go build ./internal/...
```

Expected: No errors

Note: This phase creates the scaffolding for per-profile sessions. The actual migration of NativePlayer, PlaybackManager, etc. into ProfileStreamSession is a large refactoring effort that should be done incrementally — each streaming component gets moved from App singleton to session in a separate sub-task. The session manager is now in place and handlers can start using `getStreamSession(c)` to get the profile's session context.
