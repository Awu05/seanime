# Deferred Issues — 2026-04-10

Issues identified during the Phase 4 per-profile streaming race condition review that were intentionally deferred for later work.

**Status:** Both deferred issues were addressed on 2026-04-11. See resolution notes inline below.

---

## 1. Unsynchronized field writes on PlaybackManager and DirectStreamManager

**Severity:** IMPORTANT (pre-existing data race, not introduced by Phase 4)

**What:** The `Set*` methods on `PlaybackManager` and `DirectStreamManager` do plain field assignments without a mutex:

- `internal/library/playbackmanager/playback_manager.go` around line 240:
  - `pm.settings = s`
  - `pm.animeCollection = mo.Some(ac)`
- `internal/directstream/manager.go` around line 117:
  - `m.settings = s`
  - `m.animeCollection = mo.Some(ac)`

**Problem:** Concurrent readers (e.g., the progress tracker at `playback_manager.go:535`) race against these writes. Under `go test -race` this would flag. The `StreamSessionManager` lock serializes writers against new session creation, but it does NOT protect concurrent readers inside the PlaybackManager/DirectStreamManager itself.

**Why it was deferred:** Pre-existing issue, out of scope for the session manager race fix. Fixing requires touching multiple packages and adding mutex/atomic wrapping to the setters and readers.

**Suggested fix:**
- Wrap `settings` and `animeCollection` fields in `atomic.Pointer[T]` for lock-free reads
- OR add an internal `sync.RWMutex` and make `Set*`/getters acquire it
- Must be done consistently across both `playbackmanager` and `directstream` packages

**Files to touch:**
- `internal/library/playbackmanager/playback_manager.go`
- `internal/directstream/manager.go`
- Anywhere that reads `pm.settings`, `pm.animeCollection`, `m.settings`, `m.animeCollection`

---

## 2. Session cleanup hooks for torrent/videocore resources

**Severity:** SUGGESTION (resource leak on eviction, low impact in practice)

**What:** When `StreamSessionManager.cleanupLoop` expires an inactive session (30 min idle timeout), it just `delete`s the map entry. It does not call any shutdown or teardown hooks on the session's components:

```go
// internal/core/stream_session.go:77-93
func (sm *StreamSessionManager) cleanupLoop() {
    for {
        select {
        case <-sm.cleanupTicker.C:
            sm.mu.Lock()
            now := time.Now()
            for id, session := range sm.sessions {
                if now.Sub(session.LastActive) > sm.inactivityTimeout {
                    delete(sm.sessions, id)  // <-- just delete, no cleanup
                }
            }
            sm.mu.Unlock()
        case <-sm.cleanupDone:
            return
        }
    }
}
```

**Potential leaks:**
- `TorrentStream` wrapper's per-session `Client` state (currentTorrent, currentFile) — though the shared anacrolix engine stays alive on the App singleton
- `VideoCore` subscriber goroutines and event listeners
- `NativePlayer` WebSocket handlers tied to the profile
- `DirectStreamManager` stream tracking state

**Why it was deferred:** Per-stream contexts in `directstream` already handle client disconnect cleanly. Idle cleanup is 30 min which is generous; resources get GC'd eventually. Impact is low for a household instance with 2-5 profiles.

**Why it could matter:** If a profile is evicted while a torrent is still "being tracked" (even if the user's UI has closed), the cleanup goroutines in the session's components may never be notified. Over weeks of uptime this could accumulate.

**Suggested fix:**
Add a `Shutdown()` method on `ProfileStreamSession` that calls cleanup methods on each component:
```go
func (s *ProfileStreamSession) Shutdown() {
    if s.TorrentStream != nil {
        s.TorrentStream.StopStream()  // or similar
    }
    if s.DirectStreamManager != nil {
        s.DirectStreamManager.TerminateAllStreams()
    }
    if s.NativePlayer != nil {
        s.NativePlayer.Reset()
    }
    // VideoCore cleanup if needed
}
```

Then in `cleanupLoop`, call `session.Shutdown()` before `delete`. Careful: must not call `Shutdown()` on the session's torrent wrapper in a way that drops the shared anacrolix client — need to verify `StopStream()` only affects per-session state.

**Files to touch:**
- `internal/core/stream_session.go` (add Shutdown call to cleanupLoop)
- `internal/core/session_factory.go` (add Shutdown method on ProfileStreamSession, or wire up existing cleanup methods)
- Possibly `internal/torrentstream/repository.go` to add a per-session-safe cleanup method

---

---

## Resolution notes (2026-04-11)

**Issue 1 resolved:** `PlaybackManager.settings`, `PlaybackManager.animeCollection`, `directstream.Manager.settings`, and `directstream.Manager.animeCollection` were converted to `atomic.Pointer[T]`. Writers use `Store`, readers use `Load`. `nil` from `Load()` represents absent. The constructors seed settings with an empty struct so reads never see nil. All 6 readers across utils.go, progress_tracking.go, playlist.go, debridstream.go, localfile.go, nakama.go, torrentstream.go were updated to the new pattern. Zero data race potential remains on these fields.

**Issue 2 resolved:** Added `ProfileStreamSession.Shutdown()` which calls:
- `DirectStreamManager.TerminateAllStreams()` — new method that iterates the streams map, calls `Terminate()` on each, and deletes them
- `TorrentStream.CleanupSession()` — new per-session method on `torrentstream.Repository` that drops ONLY this session's torrent (via `currentClientId → activeStreams` lookup), removes the session's activeStreams entry, cancels preloaded stream, and resets per-session playback state. Uses `c.mu.Lock()` to match existing lock ordering in the monitor loop. Crucially does NOT call `Shutdown`/`DropTorrent` which would close the shared anacrolix client.

The cleanupLoop now collects evicted sessions under the lock, releases the lock, and calls `Shutdown()` on each outside the lock (preventing deadlock and not blocking other profiles). `Stop()` also shuts down all remaining sessions on app exit.

**Follow-ups shipped:**

- **VideoCore subscriber leak fixed:** `directstream.Manager` used to spawn a `listenToPlayerEvents` goroutine that blocked on `select { case <-m.videoCoreSubscriber.Events() }` forever. Switched to `for event := range ...` so the loop exits cleanly when the channel is closed, and added `Manager.Shutdown()` which calls `videoCore.Unsubscribe(...)` to close the channel. `ProfileStreamSession.Shutdown()` now calls `Manager.Shutdown()` instead of just `TerminateAllStreams()`, so evicted sessions no longer leak subscriber goroutines.
- **`PlayLocalFile` lazy-load fallback:** when `animeCollection` is nil (background seed failed or still in-flight), `PlayLocalFile` now tries `platformRef.GetAnimeCollection(ctx, false)` first, then falls back to an empty collection and resolves media via the existing `getAnime()` path (which has its own platform fallback). A failed seed no longer bricks local file streaming for the session.

---

## Other Pending Work (not from review)

These are longer-standing items tracked in memory that remain on the list:

- **AIOStreams integration** — Stream search provider from Stremio addon aggregator
- **Play from downloaded file** — Use local file if debrid torrent is already downloaded
- **Backend architecture cleanup** — Split modules.go, typed errors, extract PlaybackManager concerns, consolidate event system, break App god object
- **Phase 4 Docker integration test** — Formality; code is complete
