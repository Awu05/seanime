package debrid_client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"seanime/internal/database/models"
	"seanime/internal/debrid/debrid"
	"seanime/internal/events"
	"seanime/internal/hook"
	"seanime/internal/notifier"
	"seanime/internal/util"
	"seanime/internal/util/result"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	DownloadAttempts         = 3
	DownloadRetryDelay       = func(n int) time.Duration { return time.Duration(n) * 2 * time.Second }
	ErrDownloadAlreadyActive = errors.New("debrid: download already active")
	isMobileDownload         = util.IsMobile
)

func (r *Repository) launchDownloadLoop(ctx context.Context) {
	r.logger.Trace().Msg("debrid: Starting download loop")
	go func() {
		for {
			select {
			case <-ctx.Done():
				r.logger.Trace().Msg("debrid: Download loop destroy request received")
				// Destroy the loop
				return
			case <-time.After(time.Minute * 1):
				provider, found := r.provider.Get()
				if !found {
					continue
				}

				r.processQueuedDownloads(provider)

			}
		}
	}()
}

func (r *Repository) processQueuedDownloads(provider debrid.Provider) {
	dbItems, err := r.db.GetDebridTorrentItems()
	if err != nil {
		r.logger.Err(err).Msg("debrid: Failed to get debrid torrent items")
		return
	}

	providerId := provider.GetSettings().ID
	for _, dbItem := range dbItems {
		if dbItem.Provider != "" && dbItem.Provider != providerId {
			continue
		}
		if r.ctxMap != nil {
			if _, found := r.ctxMap.Get(dbItem.TorrentItemID); found {
				continue
			}
		}

		item, err := provider.GetTorrent(dbItem.TorrentItemID)
		if err != nil {
			r.logger.Err(err).Str("torrentItemId", dbItem.TorrentItemID).Msg("debrid: Failed to get queued torrent")
			continue
		}
		if item == nil || !item.IsReady {
			continue
		}

		r.logger.Debug().Str("torrentItemId", dbItem.TorrentItemID).Msg("debrid: Torrent is ready for download")
		if err = r.downloadTorrentItemThen(item.ID, item.Name, item.Hash, dbItem.Destination, func(ok bool) {
			if !ok {
				return
			}

			if updateErr := r.db.MarkAutoDownloaderItemsDownloaded(dbItem.MediaId, item.Hash); updateErr != nil {
				r.logger.Err(updateErr).Msg("debrid: Failed to update auto downloader item")
			}

			if deleteErr := r.db.DeleteDebridTorrentItemByDbId(dbItem.ID); deleteErr != nil {
				r.logger.Err(deleteErr).Msg("debrid: Failed to remove debrid torrent item")
			}
		}); err != nil {
			r.logger.Err(err).Msg("debrid: Failed to download torrent")
			continue
		}
	}
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

func (r *Repository) DownloadTorrent(item debrid.TorrentItem, destination string) error {
	return r.downloadTorrentItem(item.ID, item.Name, item.Hash, destination)
}

type downloadStatus struct {
	TotalBytes int64
	TotalSize  int64
}

func (r *Repository) downloadTorrentItem(tId string, torrentName string, torrentHash string, destination string) (err error) {
	return r.downloadTorrentItemThen(tId, torrentName, torrentHash, destination, nil)
}

func (r *Repository) downloadTorrentItemThen(tId string, torrentName string, torrentHash string, destination string, onDone func(bool)) (err error) {
	defer util.HandlePanicInModuleWithError("debrid/client/downloadTorrentItem", &err)

	ctx, cancel := context.WithCancel(context.Background())
	if r.ctxMap != nil {
		if _, loaded := r.ctxMap.LoadOrStore(tId, cancel); loaded {
			cancel()
			return ErrDownloadAlreadyActive
		}
	}
	defer func() {
		if err != nil {
			cancel()
			if r.ctxMap != nil {
				r.ctxMap.Delete(tId)
			}
		}
	}()

	provider, err := r.GetProvider()
	if err != nil {
		return err
	}

	r.logger.Debug().Str("torrentName", torrentName).Str("destination", destination).Msg("debrid: Downloading torrent")

	// Get the download URL
	downloadUrl, err := provider.GetTorrentDownloadUrl(debrid.DownloadTorrentOptions{
		ID: tId,
	})
	if err != nil {
		return err
	}
	if downloadUrl == "" {
		return fmt.Errorf("debrid: download URL is empty")
	}

	event := &DebridLocalDownloadRequestedEvent{
		TorrentName: torrentName,
		Destination: destination,
		DownloadUrl: downloadUrl,
	}
	err = hook.GlobalHookManager.OnDebridLocalDownloadRequested().Trigger(event)
	if err != nil {
		return err
	}

	if event.DefaultPrevented {
		r.logger.Debug().Msg("debrid: Download prevented by hook")
		if onDone != nil {
			onDone(true)
		}
		return nil
	}

	if err := r.sendDownloadStartedEvent(tId, torrentName, destination, downloadUrl); err != nil {
		cancel()
		if r.ctxMap != nil {
			r.ctxMap.Delete(tId)
		}
		return err
	}

	go func(ctx context.Context) {
		defer func() {
			cancel()
			r.ctxMap.Delete(tId)
		}()

		var failed bool
		var failedMu sync.Mutex
		wg := sync.WaitGroup{}
		downloadUrls := strings.Split(downloadUrl, ",")
		downloadMap := result.NewMap[string, downloadStatus]()
		var successCount atomic.Int32
		var createdMu sync.Mutex
		createdPaths := make([]string, 0)

		// For batch torrents (multiple URLs), the torrent name must not be used
		// as the filename — every file would get the same name and overwrite
		// the others. Fall back to Content-Disposition / URL basename per file.
		nameForFile := torrentName
		if len(downloadUrls) > 1 {
			nameForFile = ""
		}

		for _, url := range downloadUrls {
			url = strings.TrimSpace(url)
			wg.Add(1)
			go func(ctx context.Context, url string) {
				defer wg.Done()

				paths, ok := r.downloadFileR(ctx, tId, url, destination, nameForFile, downloadMap)
				if !ok {
					failedMu.Lock()
					failed = true
					failedMu.Unlock()
					return
				}
				createdMu.Lock()
				createdPaths = append(createdPaths, paths...)
				createdMu.Unlock()
				successCount.Add(1)
			}(ctx, url)
		}
		wg.Wait()

		failedMu.Lock()
		hasFailed := failed
		failedMu.Unlock()
		if hasFailed {
			r.logger.Warn().Str("torrentItemId", tId).Msg("debrid: Download did not complete")
			if onDone != nil {
				onDone(false)
			}
			return
		}

		// Record the local download in the DB if at least one file was downloaded successfully.
		// The UI uses this to show a "downloaded" indicator and offer "play locally".
		// LocalPath records the actual created path — never the raw destination,
		// which may be the library root (deleting it would delete the library).
		if successCount.Load() > 0 {
			localPath := destination
			if len(createdPaths) == 1 {
				localPath = createdPaths[0]
			}
			pathsJson, _ := json.Marshal(createdPaths)
			if err := r.db.UpsertDebridLocalDownload(&models.DebridLocalDownload{
				TorrentItemID: tId,
				TorrentName:   torrentName,
				TorrentHash:   torrentHash,
				LocalPath:     localPath,
				LocalPaths:    string(pathsJson),
			}); err != nil {
				r.logger.Err(err).Str("torrentItemId", tId).Msg("debrid: Failed to record local download")
			}
		}

		r.sendDownloadCompletedEvent(tId, torrentName, destination)
		notifier.GlobalNotifier.Notify(notifier.Debrid, fmt.Sprintf("Downloaded %q", torrentName))
		if onDone != nil {
			onDone(true)
		}
	}(ctx)

	return nil
}

// downloadFileR retries downloadFile up to DownloadAttempts times, returning the
// created paths from the attempt that succeeded (for exact deletion later).
func (r *Repository) downloadFileR(ctx context.Context, tId string, downloadUrl string, destination string, torrentName string, downloadMap *result.Map[string, downloadStatus]) ([]string, bool) {
	for attempt := 1; attempt <= DownloadAttempts; attempt++ {
		if paths, ok := r.downloadFile(ctx, tId, downloadUrl, destination, torrentName, downloadMap); ok {
			return paths, true
		}
		if ctx.Err() != nil || attempt == DownloadAttempts {
			return nil, false
		}

		r.logger.Warn().Int("attempt", attempt+1).Int("maxAttempts", DownloadAttempts).Str("downloadUrl", downloadUrl).Msg("debrid: Retrying download")
		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(DownloadRetryDelay(attempt)):
		}
	}

	return nil, false
}

// downloadFile downloads one file into destination and returns the top-level
// destination paths it created (for exact deletion later).
func (r *Repository) downloadFile(ctx context.Context, tId string, downloadUrl string, destination string, torrentName string, downloadMap *result.Map[string, downloadStatus]) (createdPaths []string, ok bool) {
	defer util.HandlePanicInModuleThen("debrid/client/downloadFile", func() {
		ok = false
	})

	isMobile := isMobileDownload()

	// Create a cancellable HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadUrl, nil)
	if err != nil {
		r.logger.Err(err).Str("downloadUrl", downloadUrl).Msg("debrid: Failed to create request")
		return nil, false
	}

	if isMobile {
		if err := os.MkdirAll(destination, os.ModePerm); err != nil {
			r.logger.Err(err).Str("destination", destination).Msg("debrid: Failed to create destination folder")
			r.wsEventManager.SendEvent(events.ErrorToast, fmt.Sprintf("debrid: Failed to create destination folder: %v", err))
			return nil, false
		}
	} else {
		_ = os.MkdirAll(destination, os.ModePerm)
	}

	// Desktop stages in the destination. Mobile stages in the app temp dir.
	tmpDirPath, err := createDownloadTempDir(destination)
	if err != nil {
		r.logger.Err(err).Str("destination", destination).Msg("debrid: Failed to create temp folder")
		r.wsEventManager.SendEvent(events.ErrorToast, fmt.Sprintf("debrid: Failed to create temp folder: %v", err))
		return nil, false
	}
	defer os.RemoveAll(tmpDirPath) // Clean up temp folder on exit

	if runtime.GOOS == "windows" {
		r.logger.Debug().Str("tmpDirPath", tmpDirPath).Msg("debrid: Hiding temp folder")
		_, _ = util.HideFile(tmpDirPath)
		time.Sleep(time.Millisecond * 500)
	}

	// Execute the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		r.logger.Err(err).Str("downloadUrl", downloadUrl).Msg("debrid: Failed to execute request")
		r.wsEventManager.SendEvent(events.ErrorToast, fmt.Sprintf("debrid: Failed to execute download request: %v", err))
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		r.logger.Warn().Int("statusCode", resp.StatusCode).Str("downloadUrl", downloadUrl).Msg("debrid: Download request failed")
		r.wsEventManager.SendEvent(events.ErrorToast, fmt.Sprintf("debrid: Download failed / Unexpected status code: %d", resp.StatusCode))
		r.sendDownloadCancelledEvent(tId, downloadUrl, downloadMap)
		return nil, false
	}

	// Determine the filename for the downloaded file.
	// Priority: torrent name > Content-Disposition header > URL path > fallback
	filename := ""
	ext := ""

	// Helper: extract extension from Content-Disposition header of the GET response
	getExtFromContentDisposition := func() string {
		cd := resp.Header.Get("Content-Disposition")
		if cd == "" {
			return ""
		}
		_, params, cdErr := mime.ParseMediaType(cd)
		if cdErr != nil {
			// Fallback: try regex for filename="..." or filename=...
			re := regexp.MustCompile(`filename\*?=(?:UTF-8''|"?)([^";\s]+)`)
			if matches := re.FindStringSubmatch(cd); len(matches) > 1 {
				decoded, _ := url.PathUnescape(matches[1])
				if e := filepath.Ext(decoded); e != "" {
					return e
				}
			}
			return ""
		}
		if fn, ok := params["filename"]; ok {
			if e := filepath.Ext(fn); e != "" {
				return e
			}
		}
		return ""
	}

	// Helper: get full filename from Content-Disposition header
	getFilenameFromContentDisposition := func() string {
		cd := resp.Header.Get("Content-Disposition")
		if cd == "" {
			return ""
		}
		_, params, cdErr := mime.ParseMediaType(cd)
		if cdErr != nil {
			re := regexp.MustCompile(`filename\*?=(?:UTF-8''|"?)([^";\s]+)`)
			if matches := re.FindStringSubmatch(cd); len(matches) > 1 {
				decoded, _ := url.PathUnescape(matches[1])
				return decoded
			}
			return ""
		}
		if fn, ok := params["filename"]; ok {
			return fn
		}
		return ""
	}

	// 1. Use the torrent name from the debrid provider (most reliable for the base name)
	if torrentName != "" {
		filename = torrentName
		// Ensure the torrent name has a file extension
		if filepath.Ext(filename) == "" {
			// Try Content-Disposition from the GET response (most reliable for extension)
			ext = getExtFromContentDisposition()
			if ext == "" {
				// Try Content-Type
				if ct := resp.Header.Get("Content-Type"); ct != "" {
					mediaType, _, ctErr := mime.ParseMediaType(ct)
					if ctErr == nil {
						switch mediaType {
						case "application/zip":
							ext = ".zip"
						case "application/x-rar-compressed":
							ext = ".rar"
						case "video/x-matroska":
							ext = ".mkv"
						case "video/mp4":
							ext = ".mp4"
						case "video/webm":
							ext = ".webm"
						case "video/x-msvideo":
							ext = ".avi"
						}
					}
				}
			}
			if ext == "" {
				// Try URL path extension
				if parsed, urlErr := url.Parse(downloadUrl); urlErr == nil {
					urlExt := filepath.Ext(parsed.Path)
					if urlExt != "" {
						ext = urlExt
					}
				}
			}
			if ext != "" {
				filename = filename + ext
			}
		}
	}

	// 2. Try full filename from Content-Disposition header
	if filename == "" {
		if cdFilename := getFilenameFromContentDisposition(); cdFilename != "" {
			filename = cdFilename
			r.logger.Debug().Str("filename", filename).Msg("debrid: Filename found in Content-Disposition header")
		}
	}

	// 3. Try URL path
	if filename == "" {
		if parsed, urlErr := url.Parse(downloadUrl); urlErr == nil {
			base := filepath.Base(parsed.Path)
			if base != "" && base != "." && base != "/" && filepath.Ext(base) != "" {
				filename, _ = url.PathUnescape(base)
				r.logger.Debug().Str("filename", filename).Msg("debrid: Filename extracted from URL path")
			}
		}
	}

	// 4. Fallback
	if filename == "" {
		filename = "downloaded_torrent"
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			mediaType, _, ctErr := mime.ParseMediaType(ct)
			if ctErr == nil {
				switch mediaType {
				case "application/zip":
					ext = ".zip"
				case "application/x-rar-compressed":
					ext = ".rar"
				}
			}
		}
		if ext != "" {
			filename = filename + ext
		}
	}

	r.logger.Debug().Str("filename", filename).Str("ext", ext).Msg("debrid: Starting download")

	// Create a file in the temporary folder to store the download.
	tmpDownloadedFilePath := filepath.Join(tmpDirPath, filename)
	file, err := os.Create(tmpDownloadedFilePath)
	if err != nil {
		r.logger.Err(err).Str("tmpDownloadedFilePath", tmpDownloadedFilePath).Msg("debrid: Failed to create temp file")
		r.wsEventManager.SendEvent(events.ErrorToast, fmt.Sprintf("debrid: Failed to create temp file: %v", err))
		return nil, false
	}

	totalSize := resp.ContentLength
	speed := 0

	lastSent := time.Now()

	// Copy response body to the temporary file
	buffer := make([]byte, 32*1024)
	var totalBytes int64
	var lastBytes int64
	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			_, writeErr := file.Write(buffer[:n])
			if writeErr != nil {
				_ = file.Close()
				r.logger.Err(writeErr).Str("tmpDownloadedFilePath", tmpDownloadedFilePath).Msg("debrid: Failed to write to temp file")
				r.wsEventManager.SendEvent(events.ErrorToast, fmt.Sprintf("debrid: Download failed / Failed to write to temp file: %v", writeErr))
				r.sendDownloadCancelledEvent(tId, downloadUrl, downloadMap)
				return nil, false
			}
			totalBytes += int64(n)
			if totalSize > 0 {
				speed = int((totalBytes - lastBytes) / 1024) // KB/s
				lastBytes = totalBytes
			}

			downloadMap.Set(downloadUrl, downloadStatus{
				TotalBytes: totalBytes,
				TotalSize:  totalSize,
			})

			if time.Since(lastSent) > time.Second*2 {
				_totalBytes := uint64(0)
				_totalSize := uint64(0)
				downloadMap.Range(func(key string, value downloadStatus) bool {
					_totalBytes += uint64(value.TotalBytes)
					_totalSize += uint64(value.TotalSize)
					return true
				})
				// Notify progress
				r.wsEventManager.SendEvent(events.DebridDownloadProgress, map[string]interface{}{
					"status":     "downloading",
					"itemID":     tId,
					"totalBytes": util.Bytes(_totalBytes),
					"totalSize":  util.Bytes(_totalSize),
					"speed":      speed,
				})
				lastSent = time.Now()
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			if errors.Is(err, context.Canceled) {
				_ = file.Close()
				r.logger.Debug().Msg("debrid: Download cancelled")
				r.sendDownloadCancelledEvent(tId, downloadUrl, downloadMap)
				return nil, false
			}
			_ = file.Close()
			r.logger.Err(err).Str("downloadUrl", downloadUrl).Msg("debrid: Failed to read from response body")
			r.wsEventManager.SendEvent(events.ErrorToast, fmt.Sprintf("debrid: Download failed / Failed to read from response body: %v", err))
			r.sendDownloadCancelledEvent(tId, downloadUrl, downloadMap)
			return nil, false
		}
	}

	if totalSize > 0 && totalBytes != totalSize {
		_ = file.Close()
		r.logger.Warn().Int64("totalBytes", totalBytes).Int64("totalSize", totalSize).Str("downloadUrl", downloadUrl).Msg("debrid: Download ended before expected size")
		r.wsEventManager.SendEvent(events.ErrorToast, fmt.Sprintf("debrid: Download failed / Expected %s but received %s", util.Bytes(uint64(totalSize)), util.Bytes(uint64(totalBytes))))
		r.sendDownloadCancelledEvent(tId, downloadUrl, downloadMap)
		return nil, false
	}

	_ = file.Close()

	downloadMap.Delete(downloadUrl)

	if len(downloadMap.Values()) == 0 {
		r.wsEventManager.SendEvent(events.DebridDownloadProgress, map[string]interface{}{
			"status":     "downloading",
			"itemID":     tId,
			"totalBytes": "Extracting...",
			"totalSize":  "-",
			"speed":      "",
		})
	}

	r.logger.Debug().Msg("debrid: Download completed")

	switch runtime.GOOS {
	case "windows":
		time.Sleep(time.Second * 1)
	}

	// Extract the downloaded file
	var extractedDir string
	switch ext {
	case ".zip":
		extractedDir, err = unzipFile(tmpDownloadedFilePath, tmpDirPath)
		r.logger.Debug().Str("extractedDir", extractedDir).Msg("debrid: Extracted zip file")
	case ".rar":
		extractedDir, err = unrarFile(tmpDownloadedFilePath, tmpDirPath)
		r.logger.Debug().Str("extractedDir", extractedDir).Msg("debrid: Extracted rar file")
	default:
		// No extraction needed which means we downloaded a file.
		r.logger.Debug().Str("tmpDownloadedFilePath", tmpDownloadedFilePath).Str("destination", destination).Msg("debrid: No extraction needed, moving file directly")
		createdPaths, err = moveContentsToTracked(filepath.Dir(tmpDownloadedFilePath), destination, isMobile)
		if err != nil {
			r.logger.Err(err).Str("tmpDownloadedFilePath", tmpDownloadedFilePath).Str("destination", destination).Msg("debrid: Failed to move downloaded file")
			r.wsEventManager.SendEvent(events.ErrorToast, fmt.Sprintf("debrid: Failed to move downloaded file: %v", err))
			r.sendDownloadCancelledEvent(tId, downloadUrl, downloadMap)
			return nil, false
		}
		return createdPaths, true
	}
	if err != nil {
		r.logger.Err(err).Str("tmpDownloadedFilePath", tmpDownloadedFilePath).Msg("debrid: Failed to extract downloaded file")
		r.wsEventManager.SendEvent(events.ErrorToast, fmt.Sprintf("debrid: Failed to extract downloaded file: %v", err))
		r.sendDownloadCancelledEvent(tId, downloadUrl, downloadMap)
		return nil, false
	}

	r.logger.Debug().Msg("debrid: Extraction completed, deleting temporary files")

	// Delete the archive before moving the extracted files.
	err = os.Remove(tmpDownloadedFilePath)
	if err != nil {
		r.logger.Err(err).Str("tmpDownloadedFilePath", tmpDownloadedFilePath).Msg("debrid: Failed to delete downloaded file")
		// Do not stop here, continue with the extracted files
	}

	r.logger.Debug().Str("extractedDir", extractedDir).Str("destination", destination).Msg("debrid: Moving extracted files to destination")

	// Move the extracted files to the destination
	// /path/to/destination/.tmp-123456789/extracted-1234/{files} -> /path/to/destination/{files}
	createdPaths, err = moveContentsToTracked(extractedDir, destination, isMobile)
	if err != nil {
		r.logger.Err(err).Str("extractedDir", extractedDir).Str("destination", destination).Msg("debrid: Failed to move downloaded files")
		r.wsEventManager.SendEvent(events.ErrorToast, fmt.Sprintf("debrid: Failed to move downloaded files: %v", err))
		r.sendDownloadCancelledEvent(tId, downloadUrl, downloadMap)
		return nil, false
	}

	return createdPaths, true
}

func createDownloadTempDir(destination string) (string, error) {
	if isMobileDownload() {
		return os.MkdirTemp("", "seanime-debrid-")
	}
	return os.MkdirTemp(destination, ".tmp-")
}

func moveDownloadedContentsTo(src, dest string, isMobile bool) error {
	if isMobile {
		return moveContentsToMobile(src, dest)
	}
	return moveContentsTo(src, dest)
}

func (r *Repository) sendDownloadCancelledEvent(tId string, url string, downloadMap *result.Map[string, downloadStatus]) {
	downloadMap.Delete(url)

	if len(downloadMap.Values()) == 0 {
		r.wsEventManager.SendEvent(events.DebridDownloadProgress, map[string]interface{}{
			"status": "cancelled",
			"itemID": tId,
		})
	}
}

func (r *Repository) sendDownloadStartedEvent(tId string, torrentName string, destination string, downloadURL string) error {
	event := &DebridLocalDownloadStartedEvent{
		TorrentItemID: tId,
		TorrentName:   torrentName,
		Destination:   destination,
		DownloadUrl:   downloadURL,
	}

	if err := hook.GlobalHookManager.OnDebridLocalDownloadStarted().Trigger(event); err != nil {
		return err
	}

	r.wsEventManager.SendEvent(events.DebridDownloadProgress, map[string]interface{}{
		"status":     "downloading",
		"itemID":     tId,
		"totalBytes": "0 B",
		"totalSize":  "-",
		"speed":      "",
	})

	return nil
}

func (r *Repository) sendDownloadCompletedEvent(tId string, torrentName string, destination string) {
	r.wsEventManager.SendEvent(events.DebridDownloadProgress, map[string]interface{}{
		"status": "completed",
		"itemID": tId,
	})

	event := &DebridLocalDownloadCompletedEvent{
		TorrentItemID: tId,
		TorrentName:   torrentName,
		Destination:   destination,
	}
	if err := hook.GlobalHookManager.OnDebridLocalDownloadCompleted().Trigger(event); err != nil {
		r.logger.Err(err).Str("torrentItemId", tId).Msg("debrid: Failed to trigger local download completed hook")
	}
}

func getFilenameFromHeaders(url string) (string, error) {
	resp, err := http.Head(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Get the Content-Disposition header
	contentDisposition := resp.Header.Get("Content-Disposition")
	if contentDisposition == "" {
		return "", fmt.Errorf("no Content-Disposition header found")
	}

	// Use a regex to extract the filename from Content-Disposition
	re := regexp.MustCompile(`filename="(.+)"`)
	matches := re.FindStringSubmatch(contentDisposition)
	if len(matches) > 1 {
		return matches[1], nil
	}
	return "", fmt.Errorf("filename not found in Content-Disposition header")
}
