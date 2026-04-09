# Multi-Session Transcoder Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow multiple users to transcode debrid streams simultaneously by replacing singleton state with per-client routing.

**Architecture:** Keep a single long-lived transcoder but replace the singleton `currentMediaContainer` with a per-client map. Stop reinitializing the transcoder on every request. Route HLS segment requests by clientId passed via query param. The transcoder's internal Tracker already handles multi-client — we only change the layers above it.

**Tech Stack:** Go (backend), Echo framework, HLS.js (frontend, no changes needed — auto-propagates query params)

---

### Task 1: Per-Client Media Containers in PlaybackManager

**Files:**
- Modify: `internal/mediastream/playback.go`

- [ ] **Step 1: Replace singleton with per-client map**

In `internal/mediastream/playback.go`, replace the `currentMediaContainer` field in the `PlaybackManager` struct:

Replace:
```go
	PlaybackManager struct {
		logger                *zerolog.Logger
		currentMediaContainer mo.Option[*MediaContainer] // The current media being played.
		repository            *Repository
		mediaContainers       *result.Map[string, *MediaContainer] // Temporary cache for the media containers.
	}
```

With:
```go
	PlaybackManager struct {
		logger              *zerolog.Logger
		clientContainers    *result.Map[string, *MediaContainer] // Per-client active media containers, keyed by clientId.
		repository          *Repository
		mediaContainers     *result.Map[string, *MediaContainer] // Cache for media containers, keyed by hash.
	}
```

- [ ] **Step 2: Update NewPlaybackManager**

Replace:
```go
func NewPlaybackManager(repository *Repository) *PlaybackManager {
	return &PlaybackManager{
		logger:          repository.logger,
		repository:      repository,
		mediaContainers: result.NewMap[string, *MediaContainer](),
	}
}
```

With:
```go
func NewPlaybackManager(repository *Repository) *PlaybackManager {
	return &PlaybackManager{
		logger:           repository.logger,
		repository:       repository,
		clientContainers: result.NewMap[string, *MediaContainer](),
		mediaContainers:  result.NewMap[string, *MediaContainer](),
	}
}
```

- [ ] **Step 3: Update KillPlayback to be per-client**

Replace:
```go
func (p *PlaybackManager) KillPlayback() {
	p.logger.Debug().Msg("mediastream: Killing playback")
	if p.currentMediaContainer.IsPresent() {
		p.currentMediaContainer = mo.None[*MediaContainer]()
		p.logger.Trace().Msg("mediastream: Removed current media container")
	}
}
```

With:
```go
func (p *PlaybackManager) KillPlayback(clientId string) {
	p.logger.Debug().Str("clientId", clientId).Msg("mediastream: Killing playback for client")
	p.clientContainers.Delete(clientId)
}

func (p *PlaybackManager) KillAllPlayback() {
	p.logger.Debug().Msg("mediastream: Killing all playback")
	p.clientContainers.Clear()
}
```

- [ ] **Step 4: Update RequestPlayback to accept clientId**

Replace:
```go
func (p *PlaybackManager) RequestPlayback(filepath string, streamType StreamType) (ret *MediaContainer, err error) {

	p.logger.Debug().Str("filepath", filepath).Any("type", streamType).Msg("mediastream: Requesting playback")

	// Create a new media container
	ret, err = p.newMediaContainer(filepath, streamType)

	if err != nil {
		p.logger.Error().Err(err).Msg("mediastream: Failed to create media container")
		return nil, fmt.Errorf("failed to create media container: %v", err)
	}

	// Set the current media container.
	p.currentMediaContainer = mo.Some(ret)

	p.logger.Info().Str("filepath", filepath).Msg("mediastream: Ready to play media")

	return
}
```

With:
```go
func (p *PlaybackManager) RequestPlayback(filepath string, streamType StreamType, clientId string) (ret *MediaContainer, err error) {

	p.logger.Debug().Str("filepath", filepath).Str("clientId", clientId).Any("type", streamType).Msg("mediastream: Requesting playback")

	// Create a new media container
	ret, err = p.newMediaContainer(filepath, streamType)

	if err != nil {
		p.logger.Error().Err(err).Msg("mediastream: Failed to create media container")
		return nil, fmt.Errorf("failed to create media container: %v", err)
	}

	// Store the media container for this client.
	p.clientContainers.Set(clientId, ret)

	p.logger.Info().Str("filepath", filepath).Str("clientId", clientId).Msg("mediastream: Ready to play media")

	return
}
```

- [ ] **Step 5: Add GetMediaContainer method**

Add after `RequestPlayback`:

```go
// GetMediaContainer returns the media container for the given client, or false if not found.
func (p *PlaybackManager) GetMediaContainer(clientId string) (*MediaContainer, bool) {
	return p.clientContainers.Get(clientId)
}

// HasActiveSessions returns true if any client has an active media container.
func (p *PlaybackManager) HasActiveSessions() bool {
	count := 0
	p.clientContainers.Range(func(_ string, _ *MediaContainer) bool {
		count++
		return false // stop after first
	})
	return count > 0
}
```

- [ ] **Step 6: Remove the `mo` import if unused**

Remove `"github.com/samber/mo"` from the import block since `currentMediaContainer` no longer uses `mo.Option`.

- [ ] **Step 7: Verify it compiles**

Run: `go build ./internal/mediastream/...`
Expected: Compilation errors in callers — that's expected, we fix those in subsequent tasks.

- [ ] **Step 8: Commit**

```bash
git add internal/mediastream/playback.go
git commit -m "feat: replace singleton media container with per-client map"
```

---

### Task 2: Stable Transcoder Lifecycle in Repository

**Files:**
- Modify: `internal/mediastream/repository.go`

- [ ] **Step 1: Add settingsDirty flag and initMu**

In the `Repository` struct, replace `reqMu sync.Mutex` with:

```go
	initMu        sync.Mutex // Protects transcoder initialization only
	settingsDirty bool       // True when settings changed while sessions are active
```

- [ ] **Step 2: Update RequestTranscodeStream to reuse transcoder**

Replace:
```go
func (r *Repository) RequestTranscodeStream(filepath string, clientId string) (ret *MediaContainer, err error) {
	r.reqMu.Lock()
	defer r.reqMu.Unlock()

	r.logger.Debug().Str("filepath", filepath).Msg("mediastream: Transcode stream requested")

	if !r.IsInitialized() {
		return nil, errors.New("module not initialized")
	}

	// Reinitialize the transcoder for each new transcode request
	if ok := r.initializeTranscoder(r.settings); !ok {
		return nil, errors.New("real-time transcoder not initialized, check your settings")
	}

	ret, err = r.playbackManager.RequestPlayback(filepath, StreamTypeTranscode)

	return
}
```

With:
```go
func (r *Repository) RequestTranscodeStream(filepath string, clientId string) (ret *MediaContainer, err error) {
	r.logger.Debug().Str("filepath", filepath).Str("clientId", clientId).Msg("mediastream: Transcode stream requested")

	if !r.IsInitialized() {
		return nil, errors.New("module not initialized")
	}

	// Initialize or reinitialize the transcoder if needed
	r.initMu.Lock()
	if !r.transcoder.IsPresent() || (r.settingsDirty && !r.playbackManager.HasActiveSessions()) {
		if ok := r.initializeTranscoder(r.settings); !ok {
			r.initMu.Unlock()
			return nil, errors.New("real-time transcoder not initialized, check your settings")
		}
		r.settingsDirty = false
	}
	r.initMu.Unlock()

	ret, err = r.playbackManager.RequestPlayback(filepath, StreamTypeTranscode, clientId)

	return
}
```

- [ ] **Step 3: Update PreloadFirstSegments to use clientId**

Replace:
```go
func (r *Repository) PreloadFirstSegments(filepath string, clientId string) {
	if !r.IsInitialized() || !r.transcoder.IsPresent() {
		return
	}

	mc, ok := r.playbackManager.currentMediaContainer.Get()
	if !ok || mc.MediaInfo == nil {
		return
	}
```

With:
```go
func (r *Repository) PreloadFirstSegments(filepath string, clientId string) {
	if !r.IsInitialized() || !r.transcoder.IsPresent() {
		return
	}

	mc, ok := r.playbackManager.GetMediaContainer(clientId)
	if !ok || mc.MediaInfo == nil {
		return
	}
```

- [ ] **Step 4: Update ShutdownTranscodeStream to be per-client**

Replace the entire `ShutdownTranscodeStream` method in `transcode.go`:

```go
func (r *Repository) ShutdownTranscodeStream(clientId string) {
	if !r.IsInitialized() {
		return
	}

	r.logger.Warn().Str("clientId", clientId).Msg("mediastream: Shutting down transcode stream for client")

	// Remove only this client's media container
	r.playbackManager.KillPlayback(clientId)

	// Send event
	r.wsEventManager.SendEvent(events.MediastreamShutdownStream, nil)
}
```

- [ ] **Step 5: Add ShutdownAllTranscode method**

Add to `repository.go` after `ClearTranscodeDir`:

```go
// ShutdownAllTranscode destroys the transcoder and all sessions. Used on app shutdown or full reset.
func (r *Repository) ShutdownAllTranscode() {
	r.initMu.Lock()
	defer r.initMu.Unlock()

	if !r.IsInitialized() {
		return
	}

	r.logger.Warn().Msg("mediastream: Shutting down all transcode sessions")

	r.playbackManager.KillAllPlayback()

	if r.transcoder.IsPresent() {
		r.transcoder.MustGet().Destroy()
		r.transcoder = mo.None[*transcoder.Transcoder]()
	}

	r.wsEventManager.SendEvent(events.MediastreamShutdownStream, nil)
}
```

- [ ] **Step 6: Add MarkSettingsDirty method**

Add to `repository.go`:

```go
// MarkSettingsDirty flags that settings have changed. The transcoder will be reinitialized
// on the next new session when no active sessions exist.
func (r *Repository) MarkSettingsDirty() {
	r.initMu.Lock()
	defer r.initMu.Unlock()
	r.settingsDirty = true
	r.logger.Info().Msg("mediastream: Settings marked dirty, will reinitialize transcoder when all sessions end")
}
```

- [ ] **Step 7: Update RequestOptimizedStream, RequestDirectPlay, RequestPreloadDirectPlay**

Update all `RequestPlayback` calls to pass a clientId. For optimized/direct, use `"default"` since these are not multi-session yet:

In `RequestOptimizedStream`:
```go
ret, err = r.playbackManager.RequestPlayback(filepath, StreamTypeOptimized, "default")
```

In `RequestDirectPlay`:
```go
ret, err = r.playbackManager.RequestPlayback(filepath, StreamTypeDirect, clientId)
```

In `PreloadPlayback` calls (both `RequestPreloadTranscodeStream` and `RequestPreloadDirectPlay`):
```go
_, err = r.playbackManager.PreloadPlayback(filepath, StreamTypeTranscode)
```
Leave `PreloadPlayback` unchanged — it doesn't set a client container, just caches media info.

- [ ] **Step 8: Update ClearTranscodeDir**

Replace `r.reqMu.Lock()` / `r.reqMu.Unlock()` with `r.initMu.Lock()` / `r.initMu.Unlock()`.

- [ ] **Step 9: Update initializeTranscoder to not clear client containers**

In `initializeTranscoder`, replace:
```go
	r.playbackManager.mediaContainers.Clear()
```

With:
```go
	r.playbackManager.mediaContainers.Clear()
	r.playbackManager.KillAllPlayback()
```

This ensures client containers are cleared when the transcoder is fully reinitialized (settings change), but not when a single client request comes in.

- [ ] **Step 10: Verify it compiles**

Run: `go build ./internal/mediastream/...`
Expected: May have errors in handlers — fixed in next task.

- [ ] **Step 11: Commit**

```bash
git add internal/mediastream/repository.go internal/mediastream/transcode.go
git commit -m "feat: stable transcoder lifecycle with per-client sessions and dirty settings flag"
```

---

### Task 3: HLS URL Routing by Client ID

**Files:**
- Modify: `internal/mediastream/transcode.go`
- Modify: `internal/mediastream/attachments.go`
- Modify: `internal/mediastream/directplay.go`
- Modify: `internal/handlers/mediastream.go`
- Modify: `internal/directstream/debridstream.go`

- [ ] **Step 1: Update ServeEchoTranscodeStream to route by clientId**

In `internal/mediastream/transcode.go`, replace the mediaContainer lookup:

Replace:
```go
	mediaContainer, found := r.playbackManager.currentMediaContainer.Get()
	if !found {
		return errors.New("no file has been loaded")
	}
```

With:
```go
	mediaContainer, found := r.playbackManager.GetMediaContainer(clientId)
	if !found {
		return errors.New("no file has been loaded for this client")
	}
```

- [ ] **Step 2: Update handler to read clientId from query param**

In `internal/handlers/mediastream.go`, update `HandleMediastreamTranscode`:

Replace:
```go
func (h *Handler) HandleMediastreamTranscode(c echo.Context) error {
	client := "1"
	return h.App.MediastreamRepository.ServeEchoTranscodeStream(c, client)
}
```

With:
```go
func (h *Handler) HandleMediastreamTranscode(c echo.Context) error {
	clientId := c.QueryParam("clientId")
	if clientId == "" {
		clientId = "default"
	}
	return h.App.MediastreamRepository.ServeEchoTranscodeStream(c, clientId)
}
```

- [ ] **Step 3: Update HandleMediastreamDirectPlay**

Replace:
```go
func (h *Handler) HandleMediastreamDirectPlay(c echo.Context) error {
	client := "1"
	return h.App.MediastreamRepository.ServeEchoDirectPlay(c, client)
}
```

With:
```go
func (h *Handler) HandleMediastreamDirectPlay(c echo.Context) error {
	clientId := c.QueryParam("clientId")
	if clientId == "" {
		clientId = "default"
	}
	return h.App.MediastreamRepository.ServeEchoDirectPlay(c, clientId)
}
```

- [ ] **Step 4: Update HandleMediastreamShutdownTranscodeStream**

Replace:
```go
func (h *Handler) HandleMediastreamShutdownTranscodeStream(c echo.Context) error {
	client := "1"
	h.App.MediastreamRepository.ShutdownTranscodeStream(client)
	return h.RespondWithData(c, true)
}
```

With:
```go
func (h *Handler) HandleMediastreamShutdownTranscodeStream(c echo.Context) error {
	type body struct {
		ClientId string `json:"clientId"`
	}
	var b body
	_ = c.Bind(&b)
	clientId := b.ClientId
	if clientId == "" {
		clientId = "default"
	}
	h.App.MediastreamRepository.ShutdownTranscodeStream(clientId)
	return h.RespondWithData(c, true)
}
```

- [ ] **Step 5: Update attachments.go to route by clientId**

In `internal/mediastream/attachments.go`, update both methods to accept and use clientId:

Replace `ServeEchoExtractedSubtitles`:
```go
func (r *Repository) ServeEchoExtractedSubtitles(c echo.Context, clientId string) error {
```

And replace the mediaContainer lookup:
```go
	mediaContainer, found := r.playbackManager.GetMediaContainer(clientId)
```

Replace `ServeEchoExtractedAttachments`:
```go
func (r *Repository) ServeEchoExtractedAttachments(c echo.Context, clientId string) error {
```

And replace the mediaContainer lookup:
```go
	mediaContainer, found := r.playbackManager.GetMediaContainer(clientId)
```

- [ ] **Step 6: Update attachment handlers**

In `internal/handlers/mediastream.go`, update subtitle and attachment handlers:

```go
func (h *Handler) HandleMediastreamGetSubtitles(c echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Response().Header().Set("Pragma", "no-cache")
	c.Response().Header().Set("Expires", "0")
	clientId := c.QueryParam("clientId")
	if clientId == "" {
		clientId = "default"
	}
	return h.App.MediastreamRepository.ServeEchoExtractedSubtitles(c, clientId)
}

func (h *Handler) HandleMediastreamGetAttachments(c echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Response().Header().Set("Pragma", "no-cache")
	c.Response().Header().Set("Expires", "0")
	clientId := c.QueryParam("clientId")
	if clientId == "" {
		clientId = "default"
	}
	return h.App.MediastreamRepository.ServeEchoExtractedAttachments(c, clientId)
}
```

- [ ] **Step 7: Update directplay.go to route by clientId**

In `internal/mediastream/directplay.go`, replace the mediaContainer lookup:

```go
	mediaContainer, found := r.playbackManager.GetMediaContainer(clientId)
```

- [ ] **Step 8: Append clientId to HLS URL in debridstream.go**

In `internal/directstream/debridstream.go`, find where the playback URL is set (around line 193):

Replace:
```go
						playbackInfo.StreamUrl = "{{SERVER_URL}}/api/v1/mediastream/transcode/master.m3u8"
```

With:
```go
						playbackInfo.StreamUrl = "{{SERVER_URL}}/api/v1/mediastream/transcode/master.m3u8?clientId=" + s.clientId
```

- [ ] **Step 9: Verify it compiles**

Run: `go build ./...`
Expected: Success

- [ ] **Step 10: Commit**

```bash
git add internal/mediastream/transcode.go internal/mediastream/attachments.go internal/mediastream/directplay.go internal/handlers/mediastream.go internal/directstream/debridstream.go
git commit -m "feat: route HLS requests by clientId for multi-session support"
```

---

### Task 4: Settings Dirty Integration

**Files:**
- Modify: `internal/handlers/mediastream.go` (or wherever settings are saved)

- [ ] **Step 1: Find where mediastream settings are saved**

Search for where `InitializeModules` is called after settings change:

```bash
grep -rn "InitializeModules\|MediastreamSettings" internal/handlers/ internal/core/
```

- [ ] **Step 2: Call MarkSettingsDirty when settings change**

In the settings save handler, after saving mediastream settings, add:

```go
h.App.MediastreamRepository.MarkSettingsDirty()
```

This replaces any existing call to reinitialize the transcoder inline. The transcoder will reinitialize on the next new session when no active sessions exist.

- [ ] **Step 3: Verify it compiles**

Run: `go build ./...`
Expected: Success

- [ ] **Step 4: Commit**

```bash
git add internal/handlers/mediastream.go internal/core/modules.go
git commit -m "feat: mark transcoder settings dirty on change instead of immediate reinit"
```

---

### Task 5: Full Build & Integration Test

- [ ] **Step 1: Full build**

Run: `go build -o /dev/null .`
Expected: Success

- [ ] **Step 2: Docker build and test**

```bash
docker compose build && docker compose up -d
```

Test scenarios:
1. Single user debrid transcode stream — should work as before
2. Two users streaming different debrid files simultaneously — both should play independently
3. One user stops stream — other user's stream continues
4. Change transcoder settings while a stream is active — stream continues, settings apply on next session
5. All streams stop, start new stream — should use updated settings if changed

- [ ] **Step 3: Final commit with any fixes**

```bash
git add -A
git commit -m "feat: multi-session transcoder support for concurrent debrid streaming"
```
