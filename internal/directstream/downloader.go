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

// FilePath returns the download file path regardless of completion state.
// Returns "" if Start() hasn't been called yet.
func (d *DebridDownloader) FilePath() string {
	return d.localPath
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
