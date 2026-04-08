# Debrid Stream Smoothness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce debrid HLS stream initial load time from 15-60s to 3-8s and improve continuous playback smoothness by downloading to local storage in the background.

**Architecture:** Three optimizations layered together: (1) estimated keyframes skip the slow remote keyframe extraction for instant start, (2) a background downloader fetches the debrid file to local temp storage, (3) when download completes, new ffmpeg encoder heads read from the local file instead of the remote URL. Files under 10GB are downloaded; larger files skip the download but still benefit from estimated keyframes.

**Tech Stack:** Go (backend), ffmpeg/ffprobe, HLS transcoding

---

### Task 1: DebridDownloader Component

**Files:**
- Create: `internal/directstream/downloader.go`

- [ ] **Step 1: Create the downloader file**

Create `internal/directstream/downloader.go` with the full `DebridDownloader` implementation:

```go
package directstream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
)

const DefaultMaxDownloadSize int64 = 10 * 1024 * 1024 * 1024 // 10GB

// DebridDownloader downloads a debrid stream URL to local temp storage in the background.
// Files under the size cap are downloaded; larger files are skipped.
type DebridDownloader struct {
	url           string
	hash          string
	downloadDir   string
	localPath     string
	contentLength int64
	maxSize       int64

	mu         sync.RWMutex
	complete   bool
	err        error
	downloaded atomic.Int64

	cancel context.CancelFunc
	logger *zerolog.Logger
}

func NewDebridDownloader(url, hash, downloadDir string, contentLength int64, logger *zerolog.Logger) *DebridDownloader {
	return &DebridDownloader{
		url:           url,
		hash:          hash,
		downloadDir:   downloadDir,
		contentLength: contentLength,
		maxSize:       DefaultMaxDownloadSize,
		logger:        logger,
	}
}

// ShouldDownload returns true if the file is under the size cap.
func (d *DebridDownloader) ShouldDownload() bool {
	return d.contentLength > 0 && d.contentLength <= d.maxSize
}

// Start begins the background download. Call ShouldDownload() first.
func (d *DebridDownloader) Start(parentCtx context.Context) {
	ctx, cancel := context.WithCancel(parentCtx)
	d.cancel = cancel

	dir := filepath.Join(d.downloadDir, "downloads", d.hash)
	if err := os.MkdirAll(dir, 0755); err != nil {
		d.mu.Lock()
		d.err = err
		d.mu.Unlock()
		d.logger.Error().Err(err).Msg("downloader: Failed to create download directory")
		return
	}

	d.localPath = filepath.Join(dir, "video")

	go d.download(ctx)
}

func (d *DebridDownloader) download(ctx context.Context) {
	d.logger.Info().
		Str("url", d.url).
		Int64("size", d.contentLength).
		Msg("downloader: Starting background download")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.url, nil)
	if err != nil {
		d.setError(err)
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		d.setError(err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		d.setError(fmt.Errorf("unexpected status: %d", resp.StatusCode))
		return
	}

	f, err := os.Create(d.localPath)
	if err != nil {
		d.setError(err)
		return
	}
	defer f.Close()

	buf := make([]byte, 256*1024) // 256KB buffer
	for {
		select {
		case <-ctx.Done():
			d.setError(ctx.Err())
			return
		default:
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				d.setError(writeErr)
				return
			}
			d.downloaded.Add(int64(n))
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			d.setError(readErr)
			return
		}
	}

	d.mu.Lock()
	d.complete = true
	d.mu.Unlock()

	d.logger.Info().Str("path", d.localPath).Msg("downloader: Background download complete")
}

func (d *DebridDownloader) setError(err error) {
	d.mu.Lock()
	d.err = err
	d.mu.Unlock()
	if err != context.Canceled {
		d.logger.Error().Err(err).Msg("downloader: Download failed")
	}
}

// IsComplete returns true when the download has finished successfully.
func (d *DebridDownloader) IsComplete() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.complete
}

// LocalPath returns the path to the downloaded file, or "" if not yet complete.
func (d *DebridDownloader) LocalPath() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.complete {
		return d.localPath
	}
	return ""
}

// Progress returns download progress as a float64 between 0.0 and 1.0.
func (d *DebridDownloader) Progress() float64 {
	if d.contentLength <= 0 {
		return 0
	}
	return float64(d.downloaded.Load()) / float64(d.contentLength)
}

// Error returns any download error.
func (d *DebridDownloader) Error() error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.err
}

// Cleanup cancels the download and removes the downloaded file.
func (d *DebridDownloader) Cleanup() {
	if d.cancel != nil {
		d.cancel()
	}
	dir := filepath.Join(d.downloadDir, "downloads", d.hash)
	_ = os.RemoveAll(dir)
	d.logger.Debug().Str("dir", dir).Msg("downloader: Cleaned up download")
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/directstream/...`
Expected: Success (no other package references DebridDownloader yet)

- [ ] **Step 3: Commit**

```bash
git add internal/directstream/downloader.go
git commit -m "feat: add DebridDownloader for background file download"
```

---

### Task 2: Estimated Keyframes + FileStream Local Path

**Files:**
- Modify: `internal/mediastream/transcoder/keyframes.go` (add `GetEstimatedKeyframes`)
- Modify: `internal/mediastream/transcoder/filestream.go` (estimated keyframes for URLs, `LocalPath` field, `GetInputPath`)

- [ ] **Step 1: Add `GetEstimatedKeyframes` to keyframes.go**

Add this function at the end of `internal/mediastream/transcoder/keyframes.go`:

```go
// GetEstimatedKeyframes generates evenly-spaced keyframe timestamps for fast start.
// Used for remote URLs where real keyframe extraction would block for 10-60+ seconds.
// FFmpeg's segment muxer with -c:v copy auto-adjusts to actual keyframes in the source.
func GetEstimatedKeyframes(duration float64, interval float64, hash string) *Keyframe {
	count := int(duration/interval) + 1
	kfs := make([]float64, count)
	for i := 0; i < count; i++ {
		kfs[i] = float64(i) * interval
	}
	return &Keyframe{
		Sha:       hash,
		Keyframes: kfs,
		IsDone:    true,
		info:      &KeyframeInfo{},
	}
}
```

- [ ] **Step 2: Modify FileStream to use estimated keyframes for URLs**

In `internal/mediastream/transcoder/filestream.go`, add `"strings"` to the import block, then add local path fields/methods and modify `NewFileStream`:

Add fields to the `FileStream` struct after `settings  *Settings`:

```go
	localPathMu sync.RWMutex
	localPath   string
```

Add these methods after the `FileStream` struct definition (before `NewFileStream`):

```go
// SetLocalPath sets a local file path for ffmpeg to use instead of the remote URL.
func (fs *FileStream) SetLocalPath(path string) {
	fs.localPathMu.Lock()
	defer fs.localPathMu.Unlock()
	fs.localPath = path
	fs.logger.Info().Str("localPath", path).Msg("filestream: Local path set, new encoder heads will use local file")
}

// GetInputPath returns the local path if available, otherwise the original remote path.
func (fs *FileStream) GetInputPath() string {
	fs.localPathMu.RLock()
	localPath := fs.localPath
	fs.localPathMu.RUnlock()
	if localPath != "" {
		return localPath
	}
	return fs.Path
}

func isRemoteURL(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}
```

Modify `NewFileStream` — replace the keyframe goroutine block:

Replace:
```go
	ret.ready.Add(1)
	go func() {
		defer ret.ready.Done()
		ret.Keyframes = GetKeyframes(path, sha, logger, settings)
	}()
```

With:
```go
	// Use estimated keyframes for remote URLs to skip the slow 10-60s keyframe extraction
	if isRemoteURL(path) && mediaInfo.Duration > 0 {
		logger.Info().Float64("duration", mediaInfo.Duration).Msg("filestream: Using estimated keyframes for remote URL (fast start)")
		ret.Keyframes = GetEstimatedKeyframes(mediaInfo.Duration, 4.0, sha)
	} else {
		ret.ready.Add(1)
		go func() {
			defer ret.ready.Done()
			ret.Keyframes = GetKeyframes(path, sha, logger, settings)
		}()
	}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/mediastream/...`
Expected: Success

- [ ] **Step 4: Commit**

```bash
git add internal/mediastream/transcoder/keyframes.go internal/mediastream/transcoder/filestream.go
git commit -m "feat: estimated keyframes for fast start + FileStream local path support"
```

---

### Task 3: Stream Uses Local Path for FFmpeg Input

**Files:**
- Modify: `internal/mediastream/transcoder/stream.go:450`

- [ ] **Step 1: Change ffmpeg input path in `run()`**

In `internal/mediastream/transcoder/stream.go`, find line 450 where `-i` is set:

Replace:
```go
	args = append(args,
		"-i", ts.file.Path,
```

With:
```go
	args = append(args,
		"-i", ts.file.GetInputPath(),
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/mediastream/...`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/mediastream/transcoder/stream.go
git commit -m "feat: ffmpeg uses local file path when available"
```

---

### Task 4: Transcoder Download Notification + Cleanup

**Files:**
- Modify: `internal/mediastream/transcoder/transcoder.go` (add `NotifyDownloadComplete`, cleanup downloads dir)
- Modify: `internal/mediastream/repository.go` (expose `NotifyDownloadComplete`, `GetTranscodeDir`)

- [ ] **Step 1: Add `NotifyDownloadComplete` to Transcoder**

In `internal/mediastream/transcoder/transcoder.go`, add this method after `Destroy()`:

```go
// NotifyDownloadComplete updates the FileStream for the given remote path to use a local file.
func (t *Transcoder) NotifyDownloadComplete(remotePath string, localPath string) {
	stream, ok := t.streams.Get(remotePath)
	if !ok {
		t.logger.Warn().Str("remotePath", remotePath).Msg("transcoder: FileStream not found for download notification")
		return
	}
	stream.SetLocalPath(localPath)
	t.logger.Info().Str("localPath", localPath).Msg("transcoder: Download complete, new encoder heads will use local file")
}
```

- [ ] **Step 2: Extend `NewTranscoder` to clean downloads directory**

In `NewTranscoder`, after the existing loop that clears the streams directory, add:

```go
	// Clear old downloads
	downloadDir := filepath.Join(opts.TempOutDir, "downloads")
	_ = os.RemoveAll(downloadDir)
```

- [ ] **Step 3: Add repository methods**

In `internal/mediastream/repository.go`, add these methods after `RequestPreloadDirectPlay`:

```go
// NotifyDownloadComplete forwards download completion to the active transcoder.
func (r *Repository) NotifyDownloadComplete(remotePath string, localPath string) {
	if tc, ok := r.transcoder.Get(); ok {
		tc.NotifyDownloadComplete(remotePath, localPath)
	}
}

// GetTranscodeDir returns the transcode directory path.
func (r *Repository) GetTranscodeDir() string {
	return r.transcodeDir
}
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./internal/mediastream/...`
Expected: Success

- [ ] **Step 5: Commit**

```bash
git add internal/mediastream/transcoder/transcoder.go internal/mediastream/repository.go
git commit -m "feat: transcoder download notification and cleanup"
```

---

### Task 5: Extend TranscodeRequester Interface + Adapter

**Files:**
- Modify: `internal/directstream/manager.go:77-79` (extend interface)
- Modify: `internal/core/modules.go:951-963` (extend adapter)

- [ ] **Step 1: Extend `TranscodeRequester` interface**

In `internal/directstream/manager.go`, replace the `TranscodeRequester` interface:

```go
	// TranscodeRequester is an interface for requesting transcode streams.
	// This avoids a direct dependency on the mediastream package.
	TranscodeRequester interface {
		RequestTranscodeStream(filepath string, clientId string) error
		PreloadFirstSegments(filepath string, clientId string)
		NotifyDownloadComplete(remotePath string, localPath string)
		GetTranscodeDir() string
	}
```

- [ ] **Step 2: Extend adapter in modules.go**

In `internal/core/modules.go`, add these methods to `mediastreamTranscodeAdapter`:

```go
func (a *mediastreamTranscodeAdapter) NotifyDownloadComplete(remotePath string, localPath string) {
	a.repo.NotifyDownloadComplete(remotePath, localPath)
}

func (a *mediastreamTranscodeAdapter) GetTranscodeDir() string {
	return a.repo.GetTranscodeDir()
}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./...`
Expected: Success (all interface implementations now satisfy the extended interface)

- [ ] **Step 4: Commit**

```bash
git add internal/directstream/manager.go internal/core/modules.go
git commit -m "feat: extend TranscodeRequester interface for download coordination"
```

---

### Task 6: Wire Up Downloader in Debrid Stream

**Files:**
- Modify: `internal/directstream/debridstream.go` (start downloader, monitor, cleanup)

- [ ] **Step 1: Add imports and downloader field**

In `internal/directstream/debridstream.go`, add to the import block:

```go
	"crypto/sha1"
	"encoding/hex"
```

Add `downloader` field to `DebridStream` struct (after `cacheMu`):

```go
	downloader *DebridDownloader
```

- [ ] **Step 2: Add `startBackgroundDownload` method**

Add this method after `needsAudioTranscode`:

```go
// startBackgroundDownload begins downloading the debrid file to local storage.
// When complete, it notifies the transcoder so new encoder heads use the local file.
func (s *DebridStream) startBackgroundDownload() {
	if s.manager.transcodeRequester == nil {
		return
	}

	downloadDir := s.manager.transcodeRequester.GetTranscodeDir()
	if downloadDir == "" {
		return
	}

	hashBytes := sha1.Sum([]byte(s.streamUrl))
	hash := hex.EncodeToString(hashBytes[:])

	d := NewDebridDownloader(s.streamUrl, hash, downloadDir, s.contentLength, s.logger)
	if !d.ShouldDownload() {
		s.logger.Info().Int64("size", s.contentLength).Msg("directstream(debrid): File exceeds download cap, using remote-only with estimated keyframes")
		return
	}

	s.downloader = d
	d.Start(s.manager.playbackCtx)

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.manager.playbackCtx.Done():
				return
			case <-ticker.C:
				if d.IsComplete() {
					s.manager.transcodeRequester.NotifyDownloadComplete(s.streamUrl, d.LocalPath())
					s.logger.Info().Msg("directstream(debrid): Background download complete, transcoder notified")
					return
				}
				if d.Error() != nil {
					s.logger.Warn().Err(d.Error()).Msg("directstream(debrid): Background download failed, continuing with remote stream")
					return
				}
				s.logger.Debug().Msgf("directstream(debrid): Download progress: %.1f%%", d.Progress()*100)
			}
		}
	}()
}
```

- [ ] **Step 3: Call `startBackgroundDownload` in `LoadPlaybackInfo`**

In the `LoadPlaybackInfo` method, find the block after `PreloadFirstSegments` (around line 183). After the `PreloadFirstSegments` call and before the subtitle stream goroutine, add:

```go
						// Start background download for local file switchover
						s.startBackgroundDownload()
```

The relevant section should look like:

```go
					} else {
						// Preload the first segments so they're ready when the player starts
						s.manager.transcodeRequester.PreloadFirstSegments(s.streamUrl, s.clientId)
						playbackInfo.StreamUrl = "{{SERVER_URL}}/api/v1/mediastream/transcode/master.m3u8"
						playbackInfo.MimeType = "application/x-mpegURL"

						// Start background download for local file switchover
						s.startBackgroundDownload()

						// Start subtitle stream for the HLS transcode path
						go func() {
```

- [ ] **Step 4: Add downloader cleanup to `Terminate`**

In the `Terminate()` method, add downloader cleanup before the HTTP cache cleanup:

Replace:
```go
func (s *DebridStream) Terminate() {
	// Clean up HTTP cache first
	if err := s.Close(); err != nil {
```

With:
```go
func (s *DebridStream) Terminate() {
	// Clean up background downloader
	if s.downloader != nil {
		s.downloader.Cleanup()
		s.downloader = nil
	}
	// Clean up HTTP cache first
	if err := s.Close(); err != nil {
```

- [ ] **Step 5: Verify it compiles**

Run: `go build ./...`
Expected: Success

- [ ] **Step 6: Commit**

```bash
git add internal/directstream/debridstream.go
git commit -m "feat: wire up background download and local file switchover for debrid streams"
```

---

### Task 7: Build & Integration Test

- [ ] **Step 1: Full build**

Run: `go build -o /dev/null .`
Expected: Success — clean compilation with no errors

- [ ] **Step 2: Docker build and test**

```bash
docker compose build && docker compose up -d
```

Test the following scenarios:
1. Play a debrid stream — should start in ~3-8 seconds (estimated keyframes)
2. Watch download progress in logs: `docker compose logs -f seanime | grep downloader`
3. After download completes, seeking should be faster
4. Close the stream — download file should be cleaned up
5. Play a file over 10GB (if available) — should skip download, still start quickly with estimated keyframes

- [ ] **Step 3: Final commit with any fixes**

```bash
git add -A
git commit -m "feat: debrid stream smoothness - background download + estimated keyframes"
```
