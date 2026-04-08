# Debrid Stream Smoothness — Hybrid Adaptive Optimization

## Problem

HLS transcoding from remote debrid URLs is significantly slower than local torrent streaming. The root cause is that ffprobe and ffmpeg both perform HTTP range requests to the debrid CDN for every read operation:

| Stage | Local File | Debrid URL | Gap |
|-------|-----------|------------|-----|
| FFprobe metadata | ~1s | 5-15s | Large |
| Keyframe extraction | ~2-5s | 10-60s+ | Very large |
| FFmpeg segment encode | ~1-3s | 5-15s | Large |
| Segment serving | Instant | Instant | None |

The keyframe extraction is the biggest bottleneck — it scans every packet header in the entire file over HTTP.

## Solution: Hybrid Adaptive

Combine three optimizations:

1. **Estimated keyframes** for fast initial start (skip 10-60s blocking wait)
2. **Background file download** so ffmpeg can switch to local reads
3. **Local file switchover** for ongoing smooth playback and seeking

### Expected improvement

| Stage | Current | After |
|-------|---------|-------|
| Initial load | 15-60s | 3-8s |
| Continuous playback | Jittery (HTTP per segment) | Smooth after download completes |
| Seeking | 5-25s | 1-5s once file is local |

---

## Component 1: Background File Downloader

**New file:** `internal/directstream/downloader.go`

### Behavior

- When a debrid transcode stream starts, kick off a background goroutine to download the full file via HTTP GET
- Save to `{transcodeDir}/downloads/{hash}/video` temp path
- Track download progress (bytes downloaded / total bytes)
- Thread-safe status queries: `IsComplete() bool`, `LocalPath() string`, `Progress() float64`

### Size cap

- Only download files under a configurable threshold (default 10GB)
- Files over the cap skip the download entirely — they still benefit from estimated keyframes
- Typical episode (2-3GB) downloads in 2-5 minutes on a 50-100 Mbps connection

### Lifecycle

- Created when debrid stream routes through HLS transcode path in `debridstream.go`
- Runs in background goroutine until complete or cancelled
- Cancelled/cleaned up when stream ends

---

## Component 2: Estimated Keyframes for Fast Start

**Modified file:** `internal/mediastream/transcoder/keyframes.go`

### Behavior

- New function: `GetEstimatedKeyframes(duration float64, interval float64) *Keyframe`
- Generates evenly-spaced keyframe timestamps (every 4 seconds) based on video duration from MediaInfo
- `ready` WaitGroup satisfied immediately — no blocking
- Used when real keyframes are not yet cached

### Compatibility with `-c:v copy`

- FFmpeg's segment muxer with `-c:v copy` auto-adjusts segment boundaries to actual keyframes in the source
- Estimated cut points result in segments starting at the nearest real keyframe
- HLS playlist segment durations will be approximate, but HLS.js handles mismatches via `maxBufferHole` and `nudgeMaxRetry` (already configured)

### Refinement

- Once background download completes (files under cap): run real keyframe extraction on local file (~1-2 seconds)
- Replace estimated keyframes with real ones in the FileStream
- New encoder heads use accurate data; existing heads continue with their current assignments
- Files over the cap: estimated keyframes remain permanent (works fine, slightly less precise seeking)

---

## Component 3: Local File Switchover

**Modified files:** `internal/mediastream/transcoder/transcoder.go`, `stream.go`

### Behavior

- `FileStream` gains a `LocalPath` field alongside existing `Path`
- When downloader reports complete, `FileStream.LocalPath` is set
- `stream.run()` checks at ffmpeg spawn time: if `LocalPath` is set, use it for `-i`; otherwise use `Path`
- Already-running ffmpeg processes continue with their original source (can't switch mid-encode)

### Timeline for a typical 2-3GB episode

- Minutes 0-3: ffmpeg reads from debrid CDN (same as current, but with fast start from estimated keyframes)
- Minutes 3+: new encoder heads read from local disk (matches torrent streaming speed)
- Seeking after download: near-instant (local file seeks)

---

## Component 4: Cleanup

### Triggers

1. **Stream ends** — `FileStream.Destroy()` deletes the corresponding download in `{transcodeDir}/downloads/{hash}`
2. **Transcoder reinitialized** — existing cleanup of `{transcodeDir}/streams/` extended to also wipe `{transcodeDir}/downloads/`
3. **Safety timeout** — download files cleaned up after 30 minutes of no stream activity, piggybacking on the existing tracker's inactivity monitoring

### No new goroutines — hooks into existing tracker cleanup lifecycle.

---

## Data flow (after changes)

```
PlayDebridStream(url)
  |
  +--> Start DebridDownloader(url, hash) [background goroutine]
  |      |
  |      +--> HTTP GET url -> {transcodeDir}/downloads/{hash}/video
  |      +--> Track progress, set IsComplete() when done
  |
  +--> RequestTranscodeStream(url)
         |
         +--> newMediaContainer(url)  [ffprobe for MediaInfo, cached]
         |
         +--> getFileStream(url, hash, mediaInfo)
                |
                +--> GetEstimatedKeyframes(duration, 4.0)  [instant, no blocking]
                +--> ready.Done() immediately
                |
                +--> PreloadFirstSegments()
                       |
                       +--> GetVideoSegment(0)  [ffmpeg -i <url>, estimated keyframes]
                       +--> GetAudioSegment(0)  [ffmpeg -i <url>]
                       |
                       ... playback starts in ~3-5s ...
                       |
                       ... download completes after ~2-5 min ...
                       |
                       +--> FileStream.LocalPath = local file path
                       +--> Run real keyframe extraction on local file (~1-2s)
                       +--> Replace estimated keyframes with real ones
                       |
                       +--> New GetSegment() calls:
                              ffmpeg -i <local file>  [fast local reads]
```

---

## Files changed

| File | Change |
|------|--------|
| `internal/directstream/downloader.go` | **NEW** — DebridDownloader component |
| `internal/directstream/debridstream.go` | Start downloader when transcode path taken |
| `internal/mediastream/transcoder/keyframes.go` | Add `GetEstimatedKeyframes()`, support keyframe replacement |
| `internal/mediastream/transcoder/transcoder.go` | Pass downloader reference, update FileStream with local path |
| `internal/mediastream/transcoder/stream.go` | Check `LocalPath` in `run()` for ffmpeg `-i` |
| `internal/mediastream/transcoder/filestream.go` | Add `LocalPath` field, cleanup download on Destroy |
| `internal/mediastream/transcoder/tracker.go` | Extend cleanup to downloads directory |
| `internal/directstream/manager.go` | Extend TranscodeRequester interface for downloader coordination |
