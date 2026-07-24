package directstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

const DefaultMaxDownloadSize int64 = 10 * 1024 * 1024 * 1024 // 10GB

// windowsReservedNames are device names Windows reserves regardless of extension.
var windowsReservedNames = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[0-9]|lpt[0-9])(\..*)?$`)

// sanitizeFilename strips characters os.Create would reject on Windows and
// avoids reserved device names, so a download can't silently fail to create
// its destination file.
func sanitizeFilename(name string) string {
	invalid := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
	cleaned := invalid.ReplaceAllString(name, "_")
	cleaned = strings.TrimRight(cleaned, " .")
	if cleaned == "" || windowsReservedNames.MatchString(cleaned) {
		cleaned = "_" + cleaned
	}
	return cleaned
}

// downloaderRegistry deduplicates background downloads by URL so two clients
// streaming the same debrid URL share one download instead of truncating and
// overwriting the same destination file concurrently.
var (
	downloaderRegistryMu sync.Mutex
	downloaderRegistry   = make(map[string]*sharedDownloader)
)

type sharedDownloader struct {
	d      *DebridDownloader
	refs   int
	cancel func()
}

// AcquireDebridDownloader returns the shared downloader for streamUrl,
// starting it on first acquisition. Release must be called exactly once per
// Acquire when the caller no longer needs it; the underlying download is
// only cleaned up when the last reference is released.
func AcquireDebridDownloader(streamUrl, hash, downloadDir string, contentLength int64, logger *zerolog.Logger, parentCtx context.Context) (*DebridDownloader, bool) {
	downloaderRegistryMu.Lock()
	defer downloaderRegistryMu.Unlock()

	if existing, ok := downloaderRegistry[streamUrl]; ok {
		existing.refs++
		return existing.d, true
	}

	d := NewDebridDownloader(streamUrl, hash, downloadDir, contentLength, logger)
	if !d.ShouldDownload() {
		return d, false
	}
	d.Start(parentCtx)
	downloaderRegistry[streamUrl] = &sharedDownloader{d: d, refs: 1}
	return d, true
}

// ReleaseDebridDownloader drops one reference; the download is torn down
// (files removed) only when the last referencing client releases it.
func ReleaseDebridDownloader(streamUrl string) {
	downloaderRegistryMu.Lock()
	entry, ok := downloaderRegistry[streamUrl]
	if !ok {
		downloaderRegistryMu.Unlock()
		return
	}
	entry.refs--
	if entry.refs > 0 {
		downloaderRegistryMu.Unlock()
		return
	}
	delete(downloaderRegistry, streamUrl)
	downloaderRegistryMu.Unlock()

	entry.d.Cleanup()
}

// downloaderClient is used for background full-file downloads. It has no overall
// request Timeout because downloads can take many minutes; stalls are detected via
// ResponseHeaderTimeout on connect and the parent context's cancellation path.
var downloaderClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ForceAttemptHTTP2:     false,
	},
}

// DebridDownloader downloads a debrid stream URL to local temp storage in the background.
// Files under the size cap are downloaded; larger files are skipped.
type DebridDownloader struct {
	url           string
	hash          string
	downloadDir   string
	contentLength int64 // immutable after construction
	maxSize       int64 // immutable after construction

	mu         sync.RWMutex
	localPath  string // guarded by mu
	complete   bool
	err        error
	downloaded atomic.Int64

	cancel      context.CancelFunc // guarded by mu
	done        chan struct{}      // closed when the download goroutine exits
	cleanupOnce sync.Once
	logger      *zerolog.Logger
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

	dir := filepath.Join(d.downloadDir, "downloads", d.hash)
	if err := os.MkdirAll(dir, 0755); err != nil {
		d.mu.Lock()
		d.err = err
		d.mu.Unlock()
		d.logger.Error().Err(err).Msg("downloader: Failed to create download directory")
		cancel()
		return
	}

	// Preserve the original filename from the URL
	filename := "video"
	if parsed, err := url.Parse(d.url); err == nil {
		base := path.Base(parsed.Path)
		if base != "" && base != "." && base != "/" {
			filename = base
		}
	}
	filename = sanitizeFilename(filename)

	done := make(chan struct{})

	d.mu.Lock()
	d.cancel = cancel
	d.localPath = filepath.Join(dir, filename)
	d.done = done
	d.mu.Unlock()

	go func() {
		defer close(done)
		d.download(ctx)
	}()
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

	resp, err := downloaderClient.Do(req)
	if err != nil {
		d.setError(err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		d.setError(fmt.Errorf("unexpected status: %d", resp.StatusCode))
		return
	}

	d.mu.RLock()
	localPath := d.localPath
	d.mu.RUnlock()

	f, err := os.Create(localPath)
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

	// Verify the full file was actually received when the server advertised a
	// length — a short body from a chunked response or mismatched Content-Length
	// would otherwise be trusted as complete, and GetInputPath would read a
	// truncated file unconditionally.
	if d.contentLength > 0 && d.downloaded.Load() != d.contentLength {
		d.setError(fmt.Errorf("incomplete download: got %d of %d bytes", d.downloaded.Load(), d.contentLength))
		return
	}

	d.mu.Lock()
	d.complete = true
	d.mu.Unlock()

	d.logger.Info().Str("path", localPath).Msg("downloader: Background download complete")
}

func (d *DebridDownloader) setError(err error) {
	d.mu.Lock()
	d.err = err
	d.mu.Unlock()
	// context.Canceled may be wrapped (e.g. *url.Error from the http client),
	// so a normal user-initiated stop doesn't get logged as a failure.
	if !errors.Is(err, context.Canceled) {
		d.logger.Error().Err(err).Msg("downloader: Download failed")
	}
}

// IsComplete returns true when the download has finished successfully.
func (d *DebridDownloader) IsComplete() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.complete
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

// FilePath returns the download file path regardless of completion state.
// Returns "" if Start() hasn't been called yet.
func (d *DebridDownloader) FilePath() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.localPath
}

// Cleanup cancels the download, waits for the download goroutine to exit, and
// removes the downloaded file. Safe to call concurrently and multiple times.
func (d *DebridDownloader) Cleanup() {
	d.cleanupOnce.Do(func() {
		d.mu.Lock()
		cancel := d.cancel
		done := d.done
		d.mu.Unlock()

		if cancel != nil {
			cancel()
		}
		// Wait for the download goroutine to exit so os.RemoveAll doesn't race
		// an in-flight f.Write (particularly important on Windows).
		if done != nil {
			<-done
		}
		dir := filepath.Join(d.downloadDir, "downloads", d.hash)
		_ = os.RemoveAll(dir)
		d.logger.Debug().Str("dir", dir).Msg("downloader: Cleaned up download")
	})
}
