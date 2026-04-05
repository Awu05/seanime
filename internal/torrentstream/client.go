package torrentstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"seanime/internal/mediaplayers/mediaplayer"
	"seanime/internal/util"
	"strings"
	"sync"
	"time"

	alog "github.com/anacrolix/log"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/samber/mo"
	"golang.org/x/time/rate"
)

type (
	Client struct {
		repository *Repository

		torrentClient        mo.Option[*torrent.Client]
		currentTorrent       mo.Option[*torrent.Torrent]
		currentFile          mo.Option[*torrent.File]
		currentTorrentStatus TorrentStatus
		cancelFunc           context.CancelFunc

		activeStreams map[string]*ActiveStream // keyed by session/profile ID
		streamsMu    sync.RWMutex

		mu                          sync.Mutex
		stopCh                      chan struct{}                    // Closed when the media player stops
		mediaPlayerPlaybackStatusCh chan *mediaplayer.PlaybackStatus // Continuously receives playback status
		timeSinceLoggedSeeding      time.Time
		lastSpeedCheck              time.Time // Track the last time we checked speeds
		lastBytesCompleted          int64     // Track the last bytes completed
		lastBytesWrittenData        int64     // Track the last bytes written data
	}

	TorrentStatus struct {
		UploadProgress     int64   `json:"uploadProgress"`
		DownloadProgress   int64   `json:"downloadProgress"`
		ProgressPercentage float64 `json:"progressPercentage"`
		DownloadSpeed      string  `json:"downloadSpeed"`
		UploadSpeed        string  `json:"uploadSpeed"`
		Size               string  `json:"size"`
		Seeders            int     `json:"seeders"`
	}

	// ActiveStream represents a single active torrent streaming session.
	ActiveStream struct {
		Torrent              *torrent.Torrent
		File                 *torrent.File
		Status               TorrentStatus
		LastBytesCompleted   int64
		LastBytesWrittenData int64
		LastSpeedCheck       time.Time
	}

	NewClientOptions struct {
		Repository *Repository
	}
)

func NewClient(repository *Repository) *Client {
	ret := &Client{
		repository:                  repository,
		torrentClient:               mo.None[*torrent.Client](),
		currentFile:                 mo.None[*torrent.File](),
		currentTorrent:              mo.None[*torrent.Torrent](),
		activeStreams:                make(map[string]*ActiveStream),
		stopCh:                      make(chan struct{}),
		mediaPlayerPlaybackStatusCh: make(chan *mediaplayer.PlaybackStatus, 1),
	}

	return ret
}

// initializeClient will create and torrent client.
// The client is designed to support only one torrent at a time, and seed it.
// Upon initialization, the client will drop all torrents.
func (c *Client) initializeClient() error {
	// Fail if no settings
	if err := c.repository.FailIfNoSettings(); err != nil {
		return err
	}

	// Cancel the previous context, terminating the goroutine if it's running
	if c.cancelFunc != nil {
		c.cancelFunc()
	}

	// Context for the client's goroutine
	var ctx context.Context
	ctx, c.cancelFunc = context.WithCancel(context.Background())

	// Get the settings
	settings := c.repository.settings.MustGet()

	// Define torrent client settings
	cfg := torrent.NewDefaultClientConfig()
	cfg.Seed = true
	cfg.DisableIPv6 = settings.DisableIPV6
	cfg.Logger = alog.Logger{}

	// TEST ONLY: Limit download speed to 1mb/s
	// cfg.DownloadRateLimiter = rate.NewLimiter(rate.Limit(1<<20), 1<<20)

	if settings.SlowSeeding {
		cfg.DialRateLimiter = rate.NewLimiter(rate.Limit(1), 1)
		cfg.UploadRateLimiter = rate.NewLimiter(rate.Limit(1<<20), 2<<20)
	}

	if settings.TorrentClientHost != "" {
		cfg.ListenHost = func(network string) string { return settings.TorrentClientHost }
	}

	if settings.TorrentClientPort == 0 {
		settings.TorrentClientPort = 43213
	}
	cfg.ListenPort = settings.TorrentClientPort
	// Set the download directory
	// e.g. /path/to/temp/seanime/torrentstream/{infohash}
	cfg.DefaultStorage = storage.NewFileByInfoHash(settings.DownloadDir)

	c.mu.Lock()
	// Create the torrent client
	client, err := torrent.NewClient(cfg)
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("error creating a new torrent client: %v", err)
	}
	c.repository.logger.Info().Msgf("torrentstream: Initialized torrent client on port %d", settings.TorrentClientPort)
	c.torrentClient = mo.Some(client)
	c.dropTorrents()
	c.mu.Unlock()

	go func(ctx context.Context) {

		for {
			select {
			case <-ctx.Done():
				c.repository.logger.Debug().Msg("torrentstream: Context cancelled, stopping torrent client")
				return

			case status := <-c.mediaPlayerPlaybackStatusCh:
				// DEVNOTE: When this is received, "default" case is executed right after
				if status != nil && c.currentFile.IsPresent() && c.repository.playback.currentVideoDuration == 0 {
					// If the stored video duration is 0 but the media player status shows a duration that is not 0
					// we know that the video has been loaded and is playing
					if c.repository.playback.currentVideoDuration == 0 && status.Duration > 0 {
						// The media player has started playing the video
						c.repository.logger.Debug().Msg("torrentstream: Media player started playing the video, sending event")
						c.repository.sendStateEvent(eventTorrentStartedPlaying)
						// Update the stored video duration
						c.repository.playback.currentVideoDuration = status.Duration
					}
				}
			default:
				c.mu.Lock()
				// Monitor all active streams
				c.streamsMu.RLock()
				for _, stream := range c.activeStreams {
					if stream.Torrent == nil || stream.File == nil {
						continue
					}
					t := stream.Torrent
					f := stream.File

					now := time.Now()
					elapsed := now.Sub(stream.LastSpeedCheck).Seconds()

					downloadProgress := t.BytesCompleted()

					downloadSpeed := ""
					if elapsed > 0 {
						bytesPerSecond := float64(downloadProgress-stream.LastBytesCompleted) / elapsed
						if bytesPerSecond > 0 {
							downloadSpeed = fmt.Sprintf("%s/s", util.Bytes(uint64(bytesPerSecond)))
						}
					}
					size := util.Bytes(uint64(f.Length()))

					bytesWrittenData := t.Stats().BytesWrittenData
					uploadSpeed := ""
					if elapsed > 0 {
						bytesPerSecond := float64((&bytesWrittenData).Int64()-stream.LastBytesWrittenData) / elapsed
						if bytesPerSecond > 0 {
							uploadSpeed = fmt.Sprintf("%s/s", util.Bytes(uint64(bytesPerSecond)))
						}
					}

					stream.LastBytesCompleted = downloadProgress
					stream.LastBytesWrittenData = (&bytesWrittenData).Int64()
					stream.LastSpeedCheck = now

					stream.Status = TorrentStatus{
						Size:               size,
						UploadProgress:     (&bytesWrittenData).Int64(),
						DownloadSpeed:      downloadSpeed,
						UploadSpeed:        uploadSpeed,
						DownloadProgress:   downloadProgress,
						ProgressPercentage: c.getTorrentPercentage(mo.Some(t), mo.Some(f)),
						Seeders:            t.Stats().ConnectedSeeders,
					}

					// Also update legacy status for backward compat (use last session)
					c.currentTorrentStatus = stream.Status
				}
				c.streamsMu.RUnlock()

				// Legacy single-stream monitoring (when no active streams but legacy fields are set)
				if c.torrentClient.IsPresent() && c.currentTorrent.IsPresent() && c.currentFile.IsPresent() {
					c.streamsMu.RLock()
					hasActiveStreams := len(c.activeStreams) > 0
					c.streamsMu.RUnlock()

					if !hasActiveStreams {
						t := c.currentTorrent.MustGet()
						f := c.currentFile.MustGet()

						now := time.Now()
						elapsed := now.Sub(c.lastSpeedCheck).Seconds()

						downloadProgress := t.BytesCompleted()

						downloadSpeed := ""
						if elapsed > 0 {
							bytesPerSecond := float64(downloadProgress-c.lastBytesCompleted) / elapsed
							if bytesPerSecond > 0 {
								downloadSpeed = fmt.Sprintf("%s/s", util.Bytes(uint64(bytesPerSecond)))
							}
						}
						size := util.Bytes(uint64(f.Length()))

						bytesWrittenData := t.Stats().BytesWrittenData
						uploadSpeed := ""
						if elapsed > 0 {
							bytesPerSecond := float64((&bytesWrittenData).Int64()-c.lastBytesWrittenData) / elapsed
							if bytesPerSecond > 0 {
								uploadSpeed = fmt.Sprintf("%s/s", util.Bytes(uint64(bytesPerSecond)))
							}
						}

						c.lastBytesCompleted = downloadProgress
						c.lastBytesWrittenData = (&bytesWrittenData).Int64()
						c.lastSpeedCheck = now

						if t.PeerConns() != nil {
							c.currentTorrentStatus.Seeders = len(t.PeerConns())
						}

						c.currentTorrentStatus = TorrentStatus{
							Size:               size,
							UploadProgress:     (&bytesWrittenData).Int64() - c.currentTorrentStatus.UploadProgress,
							DownloadSpeed:      downloadSpeed,
							UploadSpeed:        uploadSpeed,
							DownloadProgress:   downloadProgress,
							ProgressPercentage: c.getTorrentPercentage(c.currentTorrent, c.currentFile),
							Seeders:            t.Stats().ConnectedSeeders,
						}
					}
				}

				// Send state event if there is any active torrent
				c.streamsMu.RLock()
				hasStreams := len(c.activeStreams) > 0
				c.streamsMu.RUnlock()
				if hasStreams || (c.currentTorrent.IsPresent() && c.currentFile.IsPresent()) {
					c.repository.sendStateEvent(eventTorrentStatus, c.currentTorrentStatus)
					c.repository.logger.Trace().Msgf("torrentstream: Progress: %.2f%%, Download speed: %s, Upload speed: %s, Size: %s",
						c.currentTorrentStatus.ProgressPercentage,
						c.currentTorrentStatus.DownloadSpeed,
						c.currentTorrentStatus.UploadSpeed,
						c.currentTorrentStatus.Size)
					c.timeSinceLoggedSeeding = time.Now()
				}

				c.mu.Unlock()
				if c.torrentClient.IsPresent() {
					if time.Since(c.timeSinceLoggedSeeding) > 20*time.Second {
						c.timeSinceLoggedSeeding = time.Now()
						for _, t := range c.torrentClient.MustGet().Torrents() {
							if t.Seeding() {
								c.repository.logger.Trace().Msgf("torrentstream: Seeding torrent, %d peers", t.Stats().ActivePeers)
							}
						}
					}
				}
				time.Sleep(3 * time.Second)
			}
		}
	}(ctx)

	return nil
}

func (c *Client) GetStreamingUrl() string {
	if c.torrentClient.IsAbsent() {
		return ""
	}
	if c.currentFile.IsAbsent() {
		return ""
	}
	settings, ok := c.repository.settings.Get()
	if !ok {
		return ""
	}

	host := settings.Host
	if host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	address := fmt.Sprintf("%s:%d", host, settings.Port)
	if settings.StreamUrlAddress != "" {
		address = settings.StreamUrlAddress
	}
	ret := fmt.Sprintf("http://%s/api/v1/torrentstream/stream/%s", address, url.PathEscape(c.currentFile.MustGet().DisplayPath()))
	if strings.HasPrefix(ret, "http://http") {
		ret = strings.Replace(ret, "http://http", "http", 1)
	}
	ret += c.repository.directStreamManager.GetHMACTokenQueryParam("/api/v1/torrentstream/stream", "?")
	return ret
}

func (c *Client) GetExternalPlayerStreamingUrl() string {
	if c.torrentClient.IsAbsent() {
		return ""
	}
	if c.currentFile.IsAbsent() {
		return ""
	}

	ret := fmt.Sprintf("{{SCHEME}}://{{HOST}}/api/v1/torrentstream/stream/%s", url.PathEscape(c.currentFile.MustGet().DisplayPath()))
	ret += c.repository.directStreamManager.GetHMACTokenQueryParam("/api/v1/torrentstream/stream", "?")
	return ret
}

func (c *Client) AddTorrent(id string) (*torrent.Torrent, error) {
	if c.torrentClient.IsAbsent() {
		return nil, errors.New("torrent client is not initialized")
	}

	// Drop torrents except current stream and prepared stream
	c.dropExcessTorrents()

	if strings.HasPrefix(id, "magnet") {
		return c.addTorrentMagnet(id)
	}

	if strings.HasPrefix(id, "http") {
		return c.addTorrentFromDownloadURL(id)
	}

	return c.addTorrentFromFile(id)
}

func (c *Client) addTorrentMagnet(magnet string) (*torrent.Torrent, error) {
	if c.torrentClient.IsAbsent() {
		return nil, errors.New("torrent client is not initialized")
	}

	t, err := c.torrentClient.MustGet().AddMagnet(magnet)
	if err != nil {
		return nil, err
	}

	c.repository.logger.Trace().Msgf("torrentstream: Waiting to retrieve torrent info")
	select {
	case <-t.GotInfo():
		break
	case <-t.Closed():
		//t.Drop()
		return nil, errors.New("torrent closed")
	case <-time.After(1 * time.Minute):
		t.Drop()
		return nil, errors.New("timeout waiting for torrent info")
	}
	c.repository.logger.Info().Msgf("torrentstream: Torrent added: %s", t.InfoHash().HexString())
	return t, nil
}

func (c *Client) addTorrentFromFile(fp string) (*torrent.Torrent, error) {
	if c.torrentClient.IsAbsent() {
		return nil, errors.New("torrent client is not initialized")
	}

	t, err := c.torrentClient.MustGet().AddTorrentFromFile(fp)
	if err != nil {
		return nil, err
	}
	c.repository.logger.Trace().Msgf("torrentstream: Waiting to retrieve torrent info")
	<-t.GotInfo()
	c.repository.logger.Info().Msgf("torrentstream: Torrent added: %s", t.InfoHash().AsString())
	return t, nil
}

func (c *Client) addTorrentFromDownloadURL(url string) (*torrent.Torrent, error) {
	if c.torrentClient.IsAbsent() {
		return nil, errors.New("torrent client is not initialized")
	}

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	filename := path.Base(url)
	file, err := os.Create(path.Join(os.TempDir(), filename))
	if err != nil {
		return nil, err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return nil, err
	}

	t, err := c.torrentClient.MustGet().AddTorrentFromFile(file.Name())
	if err != nil {
		return nil, err
	}
	c.repository.logger.Trace().Msgf("torrentstream: Waiting to retrieve torrent info")
	select {
	case <-t.GotInfo():
		break
	case <-t.Closed():
		t.Drop()
		return nil, errors.New("torrent closed")
	case <-time.After(1 * time.Minute):
		t.Drop()
		return nil, errors.New("timeout waiting for torrent info")
	}
	c.repository.logger.Info().Msgf("torrentstream: Added torrent: %s", t.InfoHash().AsString())
	return t, nil
}

// Shutdown closes the torrent client and drops all torrents.
// This SHOULD NOT be called if you don't intend to reinitialize the client.
func (c *Client) Shutdown() (errs []error) {
	if c.torrentClient.IsAbsent() {
		return
	}
	c.dropTorrents()
	c.currentTorrent = mo.None[*torrent.Torrent]()
	c.currentTorrentStatus = TorrentStatus{}
	c.repository.logger.Debug().Msg("torrentstream: Closing torrent client")
	return c.torrentClient.MustGet().Close()
}

func (c *Client) FindTorrent(infoHash string) (*torrent.Torrent, error) {
	if c.torrentClient.IsAbsent() {
		return nil, errors.New("torrent client is not initialized")
	}

	torrents := c.torrentClient.MustGet().Torrents()
	for _, t := range torrents {
		if t.InfoHash().AsString() == infoHash {
			c.repository.logger.Debug().Msgf("torrentstream: Found torrent: %s", infoHash)
			return t, nil
		}
	}
	return nil, fmt.Errorf("no torrent found")
}

func (c *Client) RemoveTorrent(infoHash string) error {
	if c.torrentClient.IsAbsent() {
		return errors.New("torrent client is not initialized")
	}

	c.repository.logger.Trace().Msgf("torrentstream: Removing torrent: %s", infoHash)

	torrents := c.torrentClient.MustGet().Torrents()
	for _, t := range torrents {
		if t.InfoHash().AsString() == infoHash {
			t.Drop()
			c.repository.logger.Debug().Msgf("torrentstream: Removed torrent: %s", infoHash)
			return nil
		}
	}
	return fmt.Errorf("no torrent found")
}

func (c *Client) dropTorrents() {
	if c.torrentClient.IsAbsent() {
		return
	}
	c.repository.logger.Trace().Msg("torrentstream: Dropping all torrents")

	for _, t := range c.torrentClient.MustGet().Torrents() {
		t.Drop()
	}

	if c.repository.settings.IsPresent() {
		// Delete all torrents
		fe, err := os.ReadDir(c.repository.settings.MustGet().DownloadDir)
		if err == nil {
			for _, f := range fe {
				if f.IsDir() {
					_ = os.RemoveAll(path.Join(c.repository.settings.MustGet().DownloadDir, f.Name()))
				}
			}
		}
	}

	c.repository.logger.Debug().Msg("torrentstream: Dropped all torrents")
}

// dropExcessTorrents drops all torrents except active streams, current stream, and prepared stream
func (c *Client) dropExcessTorrents() {
	if c.torrentClient.IsAbsent() {
		return
	}

	// Collect info hashes we want to keep
	keepHashes := make(map[metainfo.Hash]bool)

	// Keep all active stream torrents
	c.streamsMu.RLock()
	for _, stream := range c.activeStreams {
		if stream.Torrent != nil {
			keepHashes[stream.Torrent.InfoHash()] = true
		}
	}
	c.streamsMu.RUnlock()

	// Keep legacy current torrent
	if c.currentTorrent.IsPresent() {
		keepHashes[c.currentTorrent.MustGet().InfoHash()] = true
	}

	// Keep prepared torrent
	if c.repository.preloadedStream.IsPresent() {
		prepared := c.repository.preloadedStream.MustGet()
		keepHashes[prepared.Torrent.InfoHash()] = true
	}

	// Drop torrents that aren't in the keep list
	droppedCount := 0
	for _, t := range c.torrentClient.MustGet().Torrents() {
		infoHash := t.InfoHash()
		if !keepHashes[infoHash] {
			c.repository.logger.Trace().Msgf("torrentstream: Dropping excess torrent: %s", infoHash)
			t.Drop()
			droppedCount++

			// Also remove its directory
			if c.repository.settings.IsPresent() {
				torrentDir := path.Join(c.repository.settings.MustGet().DownloadDir, t.Name())
				_ = os.RemoveAll(torrentDir)
			}
		}
	}

	if droppedCount > 0 {
		c.repository.logger.Debug().Msgf("torrentstream: Dropped %d excess torrent(s)", droppedCount)
	}
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// SetActiveStream sets the torrent and file for a session.
func (c *Client) SetActiveStream(sessionID string, t *torrent.Torrent, f *torrent.File) {
	c.streamsMu.Lock()
	defer c.streamsMu.Unlock()
	if c.activeStreams == nil {
		c.activeStreams = make(map[string]*ActiveStream)
	}
	c.activeStreams[sessionID] = &ActiveStream{
		Torrent:        t,
		File:           f,
		LastSpeedCheck: time.Now(),
	}
	// Also set legacy fields for backward compat
	c.currentTorrent = mo.Some(t)
	c.currentFile = mo.Some(f)
}

// GetActiveStream returns the active stream for a session.
func (c *Client) GetActiveStream(sessionID string) *ActiveStream {
	c.streamsMu.RLock()
	defer c.streamsMu.RUnlock()
	if c.activeStreams == nil {
		return nil
	}
	return c.activeStreams[sessionID]
}

// RemoveActiveStream removes a session's active stream.
func (c *Client) RemoveActiveStream(sessionID string) {
	c.streamsMu.Lock()
	defer c.streamsMu.Unlock()
	delete(c.activeStreams, sessionID)
	// If no active streams remain, clear legacy fields
	if len(c.activeStreams) == 0 {
		c.currentTorrent = mo.None[*torrent.Torrent]()
		c.currentFile = mo.None[*torrent.File]()
		c.currentTorrentStatus = TorrentStatus{}
	}
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// getTorrentPercentage returns the percentage of the current torrent file
// If no torrent is selected, it returns -1
func (c *Client) getTorrentPercentage(t mo.Option[*torrent.Torrent], f mo.Option[*torrent.File]) float64 {
	if t.IsAbsent() || f.IsAbsent() {
		return -1
	}

	if f.MustGet().Length() == 0 {
		return 0
	}

	return float64(f.MustGet().BytesCompleted()) / float64(f.MustGet().Length()) * 100
}

// readyToStream determines if enough of the file has been downloaded to begin streaming
// Uses both absolute size (minimum buffer) and a percentage-based approach
func (c *Client) readyToStream() bool {
	if c.currentTorrent.IsAbsent() || c.currentFile.IsAbsent() {
		return false
	}

	file := c.currentFile.MustGet()

	// Always need at least 1MB to start playback (typical header size for many formats)
	const minimumBufferBytes int64 = 1 * 1024 * 1024 // 1MB

	// For large files, use a smaller percentage
	var percentThreshold float64
	fileSize := file.Length()

	switch {
	case fileSize > 5*1024*1024*1024: // > 5GB
		percentThreshold = 0.1 // 0.1% for very large files
	case fileSize > 1024*1024*1024: // > 1GB
		percentThreshold = 0.5 // 0.5% for large files
	default:
		percentThreshold = 0.5 // 0.5% for smaller files
	}

	bytesCompleted := file.BytesCompleted()
	percentCompleted := float64(bytesCompleted) / float64(fileSize) * 100

	// Ready when both minimum buffer is met AND percentage threshold is reached
	return bytesCompleted >= minimumBufferBytes && percentCompleted >= percentThreshold
}

// readyToStreamSession determines if enough of the file has been downloaded for a specific session
func (c *Client) readyToStreamSession(sessionID string) bool {
	stream := c.GetActiveStream(sessionID)
	if stream == nil || stream.File == nil {
		return c.readyToStream() // fallback to legacy
	}

	file := stream.File
	const minimumBufferBytes int64 = 1 * 1024 * 1024

	var percentThreshold float64
	fileSize := file.Length()

	switch {
	case fileSize > 5*1024*1024*1024:
		percentThreshold = 0.1
	case fileSize > 1024*1024*1024:
		percentThreshold = 0.5
	default:
		percentThreshold = 0.5
	}

	bytesCompleted := file.BytesCompleted()
	percentCompleted := float64(bytesCompleted) / float64(fileSize) * 100

	return bytesCompleted >= minimumBufferBytes && percentCompleted >= percentThreshold
}
