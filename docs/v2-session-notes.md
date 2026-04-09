# V2 Branch Session Notes

## Branch: `v2`

## Summary

Multi-user support with per-profile settings, StremThru debrid integration, native browser player, and HLS transcoding for debrid streams. 33 files changed, +792 -167 lines.

---

## What Was Done

### 1. Per-Profile Settings (All Settings Save Per-Profile)

**Problem:** Settings were global (ID=1). All profiles shared the same torrent provider, media player, etc.

**Solution:**
- Added `h.getSettings(c)` helper in `internal/handlers/anilist_helper.go` — routes to `GetSettingsForProfile(profileID)` in multi-user mode, `GetSettings()` in single-user
- Migrated all handler call sites: `HandleGetSettings`, `HandleSaveSettings`, `HandleSaveAutoDownloaderSettings`, `HandleSaveMediaPlayerSettings`, 3 library explorer handlers, mediaplayer handler, announcements
- **Critical fix:** `/api/v1/status` is a public path (no auth middleware), so `profileID` was never set. Added `tryExtractProfile()` in `internal/handlers/auth_middleware.go` which optionally reads the JWT cookie on public paths to extract the profile context
- `NewStatus` in `internal/handlers/status.go` now returns per-profile settings

### 2. AniList Per-Profile Fixes

**Problem:** AniList pool created platforms without calling `SetUsername()`, causing "Username is not set" errors. Connect/disconnect caused page reload.

**Solution:**
- `internal/core/anilist_pool.go` — fetch account by profile ID, call `SetUsername()` on the platform
- `seanime-web/src/app/(main)/settings/page.tsx` — AniList connect uses inline mutation with `setServerStatus(data)` + query invalidation (no reload). Disconnect uses inline mutation instead of `useLogout` (avoids redirect to `/`)

### 3. StremThru Debrid Integration

**Problem:** No StremThru support as a debrid provider.

**Solution:**
- **New file:** `internal/debrid/stremthru/stremthru.go` — full `debrid.Provider` implementation
- Uses Store/magnets API (`/v0/store/magnets/*`) matching official JS/Python SDKs
- Auth: `X-StremThru-Authorization: Basic <base64(user:pass)>` (NOT `Authorization` — that gets forwarded to the store)
- Optional `X-StremThru-Store-Name` header for multi-store users
- `internal/database/models/models.go` — added `ApiUrl`, `StoreName` to `DebridSettings`
- `internal/debrid/client/repository.go` — added `stremthru` case to provider factory
- Frontend: added to getting-started page and debrid settings UI with conditional fields
- **New file:** `docker-compose.yml` — includes stremthru service

**Key learning:** `STREMTHRU_AUTH` username must match `STREMTHRU_STORE_AUTH` username or you get "missing store" error.

### 4. Debrid Torrent List Play Button

**Problem:** No way to play completed torrents from the debrid torrent list page.

**Solution:**
- `internal/handlers/debrid.go` — `HandleDebridPlayTorrent` endpoint (`POST /api/v1/debrid/torrents/play`)
- `internal/directstream/debridstream.go` — `PlayDebridStreamDirect()` creates stub media/episode metadata and feeds through the full native player pipeline
- `seanime-web/src/app/(main)/debrid/page.tsx` — blue play button on completed torrents, calls backend which opens native player via WebSocket

### 5. Native Player Browser Support

**Problem:** Native player only worked in Electron. Docker/browser users couldn't use it for torrent/debrid streaming.

**Solution:**
- `seanime-web/src/app/(main)/_atoms/playback.atoms.tsx` — added `NativePlayer` option to `playbackTorrentStreamingOptions` dropdown
- `handle-torrent-stream.ts` and `handle-debrid-stream.ts` — fixed `getPlaybackType` to check `PlaybackTorrentStreaming.NativePlayer` in non-Electron mode
- `handle-debrid-stream.ts` — fixed `isUsingNativePlayer` for non-Electron mode

### 6. FFmpeg HLS Transcoding for Debrid Streams

**Problem:** Browsers can't play MKV containers or AC3/EAC3/DTS audio. Debrid streams served raw video which failed.

**Solution:**
- `internal/directstream/debridstream.go`:
  - `needsAudioTranscode()` checks audio codec IDs against unsupported list
  - ALL MKV/EBML/octet-stream debrid content routes through HLS transcoder (not just bad audio)
  - Triggers `MediastreamRepository.RequestTranscodeStream()` with debrid URL
  - Sets `StreamUrl` to `/api/v1/mediastream/transcode/master.m3u8`
  - `PreloadFirstSegments` kicks off first video+audio segment generation in background
  - Subtitle stream started in goroutine for transcode path
- `internal/directstream/manager.go` — `TranscodeRequester` interface (avoids circular dep)
- `internal/core/modules.go` — `mediastreamTranscodeAdapter` wraps MediastreamRepository
- `internal/mediastream/repository.go` — `PreloadFirstSegments()` triggers segment 0 for video and audio
- `internal/mediastream/videofile/info_utils.go` — `GetHashFromPath()` supports URLs (hashes URL string)
- `internal/mediastream/videofile/info.go` — safe extension extraction (no panic on URLs without extension)
- `internal/mediastream/playback.go` — skips attachment extraction for remote URLs

**Architecture:** ffprobe and ffmpeg both accept HTTP URLs natively. The transcoder generates keyframe-aligned HLS segments on-demand. Full seeking works via HLS segment requests.

**Limitation:** Transcoder is a singleton — only one user can transcode at a time. See `memory/project_multi_transcode.md` for future plan.

### 7. HLS.js Player Improvements

**Problem:** First play always stalled because HLS.js auto-played before segments were ready. Fragment load timeout (10s) too short for remote transcoding.

**Solution in `video-core-hls.ts`:**
- `fragLoadPolicy`: 30s time-to-first-byte, 120s max load time, 5 retries with exponential backoff
- `maxBufferLength: 30`, `maxBufferHole: 2`, `highBufferWatchdogPeriod: 3`, `nudgeMaxRetry: 10`
- Deferred autoplay: waits for `FRAG_BUFFERED` event instead of `MANIFEST_PARSED` before calling `play()`
- Added `hlsAutoPlayTriggered` ref to prevent double-play

### 8. Video Cleanup on Stream Close

**Problem:** Audio continued playing after closing the video player.

**Solution in `native-player.tsx`:**
- `handleTerminateStream` now calls `videoElement.removeAttribute("src")` and `videoElement.load()` to force the browser to release the media resource immediately

### 9. Electron Multi-User

**Problem:** No way to enable multi-user profiles in the Electron desktop app.

**Solution:**
- `seanime-web/src/app/(main)/settings/_containers/denshi-settings.tsx` — `DenshiMultiUserSetup` component with inline admin setup form
- `internal/handlers/user_auth.go` — `HandleAdminSetup` reuses existing "Default" profile in Electron sidecar mode

---

### 10. Debrid Stream Smoothness — Hybrid Adaptive Optimization (Session 2)

**Problem:** HLS transcoding from remote debrid URLs was 15-60s initial load, jittery playback, 5-25s seeking.

**Solution — three optimizations:**
1. **Estimated keyframes** — generates evenly-spaced keyframe timestamps (every 10s) based on video duration, skipping the slow 10-60s remote ffprobe keyframe extraction. Added `IsEstimated` flag to skip the video midpoint seek hack that caused A/V desync with wide intervals.
2. **Background file downloader** — `DebridDownloader` in `internal/directstream/downloader.go` downloads the debrid file to local temp storage. 10GB size cap. Files under 5GB switch to local file at 5% downloaded; larger files wait for full completion.
3. **Local file switchover** — `FileStream.GetInputPath(seekTime)` returns the local path when the file is downloaded far enough, falls back to remote URL otherwise. New encoder heads read locally.

**Key files:**
- `internal/directstream/downloader.go` — NEW: DebridDownloader
- `internal/mediastream/transcoder/keyframes.go` — `GetEstimatedKeyframes()`, `IsEstimated` flag
- `internal/mediastream/transcoder/filestream.go` — `SetLocalPath()`, `GetInputPath(seekTime)`, estimated keyframes in `NewFileStream`
- `internal/mediastream/transcoder/stream.go` — `GetInputPath(startRef)` in `run()`, skip midpoint hack for estimated keyframes
- `internal/mediastream/transcoder/transcoder.go` — `NotifyDownloadComplete()`
- `internal/directstream/manager.go` — Extended `TranscodeRequester` interface
- `internal/directstream/debridstream.go` — Download lifecycle, subtitle extraction from local file

**Additional fixes:**
- Subtitle extraction deferred to local file (avoids range request errors from CDNs)
- Remote URL subtitle extraction still attempted immediately for partial coverage
- Segment timeout increased from 25s to 60s for remote URL seeking
- Estimated keyframe interval increased from 4s to 10s (prevents empty segments with long-GOP videos)
- Debrid download preserves original filename (torrent name + extension from Content-Disposition)

### 11. Debrid Download Filename Fix (Session 2)

**Problem:** Downloading debrid torrents from the UI saved with generic filename, no extension.

**Solution:** Changed filename priority to: torrent name → Content-Disposition header → URL path → fallback. Extension extracted from Content-Disposition of the GET response.

---

## Known Issues / Future Work

1. **Multi-session streaming** — Mediastream transcoder layer done. Directstream Manager done (per-client streams map, per-stream contexts). Needs end-to-end testing with multiple concurrent users.
2. **AIOStreams integration** — Stream search provider from Stremio addons. See `memory/project_aiostreams.md`
3. **Play from downloaded file** — When a debrid torrent has been downloaded locally, the play button on the debrid torrent list should stream from the local file instead of the remote debrid URL. Check the download destination for the torrent, and if the file exists locally, use the local file path for playback instead of calling `GetTorrentStreamUrl`. This avoids unnecessary network usage and gives instant, smooth playback with full seeking.
4. **`master.m3u8&thumbnail=true` 500 error** — Thumbnail preview generator appends invalid query param. Cosmetic, doesn't affect playback.
5. **Backend architecture cleanup** — Targeted improvements to codebase maintainability:
   - **Split `modules.go`** (977 lines) into domain-specific init files: `modules_streaming.go`, `modules_library.go`, `modules_players.go`. Easier to navigate and modify.
   - **Add typed errors** for HTTP status mapping. Currently all errors are string-based `error` interface — handlers can't distinguish bad input (400) from server failures (500) from external API errors (503). Define `AppError{Code, Status, Msg}` and use across packages.
   - **Extract `PlaybackManager` concerns** — currently handles stream playback + Discord presence + progress tracking + episode collection. Split into `PlaybackOrchestrator`, `ProgressTracker`, `PresenceUpdater`.
   - **Consolidate event system** — 5 separate subscriber maps (`clientEventSubscribers`, `clientNativePlayerEventSubscribers`, etc.) should be a single generic pub/sub with topic filtering.
   - **Long-term: break App god object** — 180+ field struct where every handler has access to every subsystem. Group into domain services (`StreamingService`, `LibraryService`, `DebridService`, `PlayerService`, `AccountService`). Handlers receive only what they need.

---

## Key Files Changed (33 files)

### Backend (Go)
| File | Changes |
|------|---------|
| `internal/core/anilist_pool.go` | Profile account lookup, SetUsername |
| `internal/core/modules.go` | mediastreamTranscodeAdapter, TranscodeRequester wiring |
| `internal/database/models/models.go` | ApiUrl, StoreName on DebridSettings |
| `internal/debrid/client/repository.go` | stremthru import and factory case |
| `internal/debrid/stremthru/stremthru.go` | **NEW:** Full StremThru provider |
| `internal/directstream/debridstream.go` | PlayDebridStreamDirect, needsAudioTranscode, HLS transcode, subtitle stream |
| `internal/directstream/manager.go` | TranscodeRequester interface |
| `internal/handlers/anilist_helper.go` | getSettings() helper |
| `internal/handlers/auth_middleware.go` | tryExtractProfile() for public paths |
| `internal/handlers/debrid.go` | HandleDebridPlayTorrent endpoint |
| `internal/handlers/library_explorer.go` | getSettings migration (3 handlers) |
| `internal/handlers/mediaplayer.go` | getSettings migration |
| `internal/handlers/routes.go` | New debrid play route |
| `internal/handlers/settings.go` | Per-profile save for all settings handlers |
| `internal/handlers/status.go` | Per-profile settings in NewStatus |
| `internal/handlers/user_auth.go` | Reuse existing profile in HandleAdminSetup |
| `internal/mediastream/playback.go` | Skip attachment extraction for URLs |
| `internal/mediastream/repository.go` | PreloadFirstSegments method |
| `internal/mediastream/videofile/info.go` | Safe extension extraction for URLs |
| `internal/mediastream/videofile/info_utils.go` | URL hash support |

### Frontend (TypeScript/React)
| File | Changes |
|------|---------|
| `src/api/generated/endpoint.types.ts` | debridApiUrl field |
| `src/api/generated/types.ts` | apiUrl, storeName on DebridSettings |
| `src/app/(main)/_atoms/playback.atoms.tsx` | NativePlayer option in dropdown |
| `src/app/(main)/_features/getting-started/getting-started-page.tsx` | StremThru setup fields |
| `src/app/(main)/_features/native-player/native-player.tsx` | Force media release on close |
| `src/app/(main)/_features/video-core/video-core-hls.ts` | Extended timeouts, buffered autoplay |
| `src/app/(main)/debrid/page.tsx` | Play button, PlayTorrentModal, StremThru name |
| `src/app/(main)/entry/_containers/debrid-stream/_lib/handle-debrid-stream.ts` | Native player fix |
| `src/app/(main)/settings/_containers/debrid-settings.tsx` | StremThru UI with store dropdown |
| `src/app/(main)/settings/_containers/denshi-settings.tsx` | Multi-user setup for Electron |
| `src/app/(main)/settings/page.tsx` | AniList connect/disconnect without reload |
| `src/lib/server/settings.ts` | debridApiUrl schema field |

### Docker
| File | Changes |
|------|---------|
| `docker-compose.yml` | **NEW:** stremthru service |

---

## Build & Deploy

### Docker
```bash
docker compose build && docker compose up -d
```

### Web frontend (denshi for Electron)
```bash
wsl -d Ubuntu -- bash -c "cd seanime-web && npm run build:denshi"
cp -r seanime-web/out-denshi seanime-denshi/web-denshi
```

### Go backend (for Electron)
```bash
mkdir -p web && echo "<html></html>" > web/index.html  # placeholder for embed
go build -o seanime-denshi/binaries/seanime-server-windows.exe -trimpath -ldflags="-s -w" -tags=nosystray .
```

### Important reminders
- Use `wsl -d Ubuntu` for all npm commands
- Never add Co-Authored-By to commits
- StremThru `STREMTHRU_AUTH` username must match `STREMTHRU_STORE_AUTH` username
