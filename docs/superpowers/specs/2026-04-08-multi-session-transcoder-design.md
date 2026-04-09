# Multi-Session Transcoder Design

## Problem

The mediastream transcoder is a singleton — `RequestTranscodeStream` reinitializes the transcoder on every call, destroying the previous session. Only one user can transcode at a time. If user A is streaming and user B starts a debrid stream, user A's session is killed.

This affects debrid streams with AC3/EAC3/DTS/TrueHD audio (requires HLS transcoding). AAC audio uses the direct stream path and already supports concurrent users.

## Solution: Session-Aware Singleton

Keep a single long-lived transcoder but replace singleton state with per-client maps. The transcoder already handles multiple clients internally via its Tracker — the singleton problem is entirely in the routing layers above it.

**Key principle:** Two clients watching the same file share the `FileStream` (encode once, serve many). Different files get separate `FileStream` entries in the same transcoder.

---

## Component 1: Per-Client Media Containers

**File:** `internal/mediastream/playback.go`

Replace the singleton `currentMediaContainer` with a map keyed by clientId:

```
Before: currentMediaContainer mo.Option[*MediaContainer]
After:  mediaContainers *result.Map[string, *MediaContainer]
```

- `RequestPlayback(filepath, streamType)` → `RequestPlayback(filepath, streamType, clientId)` — stores under that clientId
- `KillPlayback()` → `KillPlayback(clientId)` — removes only that client's container
- New `GetMediaContainer(clientId) (*MediaContainer, bool)` — for serve handlers to look up by client

All callers updated: `ServeEchoTranscodeStream`, `PreloadFirstSegments`, attachments, subtitles, direct play.

---

## Component 2: Stable Transcoder Lifecycle

**File:** `internal/mediastream/repository.go`

Stop reinitializing the transcoder on every request:

- `RequestTranscodeStream(filepath, clientId)` reuses the existing transcoder if initialized. Only creates it on first use.
- `ShutdownTranscodeStream(clientId)` removes that client's media container and lets the Tracker clean up encoder heads. Does NOT destroy the transcoder.
- New `ShutdownAllTranscode()` for app shutdown / full reset — the only thing that destroys the transcoder.
- `settingsDirty bool` flag — set when transcoder settings change. On the next new session, if no active sessions exist in the media containers map, reinitialize the transcoder with new settings. Existing sessions keep running on old settings until they naturally end (same pattern as streaming services).
- Remove `reqMu` global mutex — the per-client map handles concurrency. Add a lighter mutex only around transcoder initialization.

---

## Component 3: HLS URL Routing by Client ID

**Files:** `internal/directstream/debridstream.go`, `internal/mediastream/transcode.go`

Pass clientId through HLS URLs so segment requests route to the right media container:

- Debrid stream sets playback URL to: `{{SERVER_URL}}/api/v1/mediastream/transcode/master.m3u8?clientId={clientId}`
- `ServeEchoTranscodeStream` reads `clientId` from the query param
- Looks up `playbackManager.GetMediaContainer(clientId)` instead of the singleton
- HLS.js automatically appends query params from the master URL to all subsequent segment requests (`.m3u8` and `.ts`), so `clientId` flows through every request without extra work

Attachment and subtitle endpoints also read `clientId` from query params.

---

## Component 4: Cleanup & Lifecycle

**Per-client cleanup:**
- `DebridStream.Terminate()` calls `ShutdownTranscodeStream(clientId)` — removes that client's media container
- The Tracker handles per-client encoder head cleanup: kills unused quality/audio streams and orphaned heads when a client disappears

**Transcoder lifecycle:**
- Created on first transcode request. Lives until all sessions end + settings are dirty, or app shutdown.
- `settingsDirty` flag set when settings change. On next new session request, if no active sessions exist, reinitialize with new settings.

**Download cleanup:**
- `DebridDownloader` is per-stream (on `DebridStream`), no change needed
- `NotifyDownloadComplete` uses filepath as key — shared FileStream means all clients watching that file benefit from local file switchover

**Stale session protection:**
- Tracker already has 1-hour inactivity timeout per client
- Add safety check: if a client's media container hasn't been accessed in 30 minutes, remove from map

---

## Files Changed

| File | Change |
|------|--------|
| `internal/mediastream/playback.go` | Replace `currentMediaContainer` with per-client map, update `RequestPlayback`, `KillPlayback`, add `GetMediaContainer` |
| `internal/mediastream/repository.go` | Stop reinitializing transcoder per request, add `settingsDirty`, per-client `ShutdownTranscodeStream`, new `ShutdownAllTranscode` |
| `internal/mediastream/transcode.go` | Route by clientId query param in all segment/master handlers |
| `internal/mediastream/attachments.go` | Route by clientId query param |
| `internal/mediastream/directplay.go` | Route by clientId query param |
| `internal/directstream/debridstream.go` | Append `clientId` to HLS URL |
| `internal/directstream/manager.go` | Update `TranscodeRequester` interface if needed |
| `internal/core/modules.go` | Update adapter if interface changes |

## What Stays the Same

- `transcoder/transcoder.go` — `streams` map stays keyed by filepath (shared FileStreams)
- `transcoder/tracker.go` — already tracks per-client, no changes needed
- `transcoder/stream.go`, `filestream.go`, `keyframes.go` — internal transcoder unchanged
- `DebridDownloader` — per-stream, no changes
- Frontend HLS.js — automatically propagates query params
