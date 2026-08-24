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
	"seanime/internal/util/torrentutil"
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

// Registry of every live Client wrapper sharing the anacrolix engine.
// Drop operations consult all wrappers' claims so one session can never drop
// a torrent (or delete its data) that another session is still streaming.
var (
	allClientsMu sync.RWMutex
	allClients   = make(map[*Client]struct{})
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

	allClientsMu.Lock()
	allClients[ret] = struct{}{}
	allClientsMu.Unlock()

	return ret
}

// unregisterClient removes a wrapper from the shared registry when its
// session is evicted, releasing its torrent claims.
func unregisterClient(c *Client) {
	allClientsMu.Lock()
	delete(allClients, c)
	allClientsMu.Unlock()
}

// currentTorrentAndFile returns a locked snapshot of the legacy currentTorrent/currentFile
// pair. StartStream/StopStream mutate both fields together under c.mu; every read outside of
// code that already holds c.mu must go through this instead of touching the fields directly,
// or it can observe a torn pair (one field updated, the other not yet).
func (c *Client) currentTorrentAndFile() (mo.Option[*torrent.Torrent], mo.Option[*torrent.File]) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentTorrent, c.currentFile
}

// claimedHashes returns the infohashes any live wrapper still uses:
// per-client active streams, the legacy current torrent, and preloaded streams.
func claimedHashes() map[metainfo.Hash]bool {
	keep := make(map[metainfo.Hash]bool)
	allClientsMu.RLock()
	defer allClientsMu.RUnlock()
	for cl := range allClients {
		cl.streamsMu.RLock()
		for _, stream := range cl.activeStreams {
			if stream.Torrent != nil {
				keep[stream.Torrent.InfoHash()] = true
			}
		}
		cl.streamsMu.RUnlock()
		if torrentOpt, _ := cl.currentTorrentAndFile(); torrentOpt.IsPresent() {
			keep[torrentOpt.MustGet().InfoHash()] = true
		}
		if cl.repository != nil {
			if ps, ok := cl.repository.getPreloadedStream(); ok && ps.Torrent != nil {
				keep[ps.Torrent.InfoHash()] = true
			}
		}
	}
	return keep
}

// GetTorrentClient returns the underlying anacrolix torrent client (if initialized).
func (c *Client) GetTorrentClient() mo.Option[*torrent.Client] {
	return c.torrentClient
}

// UseSharedTorrentClient sets this client wrapper to use an existing anacrolix torrent client
// instead of creating its own. This allows multiple session wrappers to share a single engine.
// Starts the monitoring goroutine for this wrapper's active streams.
func (c *Client) UseSharedTorrentClient(tc *torrent.Client) {
	if c.cancelFunc != nil {
		c.cancelFunc()
	}

	var ctx context.Context
	ctx, c.cancelFunc = context.WithCancel(context.Background())

	c.mu.Lock()
	c.torrentClient = mo.Some(tc)
	c.mu.Unlock()

	go c.monitorLoop(ctx)
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
	c.dropUnclaimedTorrents()
	c.mu.Unlock()

	go c.monitorLoop(ctx)

	return nil
}

// monitorLoop runs the background monitoring goroutine that tracks torrent download/upload
// progress for all active streams.
func (c *Client) monitorLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			c.repository.logger.Debug().Msg("torrentstream: Context cancelled, stopping monitor loop")
			return

		case status := <-c.mediaPlayerPlaybackStatusCh:
			_, fileOpt := c.currentTorrentAndFile()
			if status != nil && fileOpt.IsPresent() && c.repository.playback.currentVideoDuration == 0 {
				if c.repository.playback.currentVideoDuration == 0 && status.Duration > 0 {
					c.repository.logger.Debug().Msg("torrentstream: Media player started playing the video, sending event")
					c.repository.sendStateEvent(eventTorrentStartedPlaying)
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

				c.currentTorrentStatus = stream.Status
			}
			c.streamsMu.RUnlock()

			// Send state event only if there is an active torrent being streamed
			c.streamsMu.RLock()
			hasStreams := len(c.activeStreams) > 0
			c.streamsMu.RUnlock()
			hasTorrent := c.currentTorrent.IsPresent() && c.currentFile.IsPresent()
			if hasStreams || hasTorrent {
				c.repository.sendStateEvent(eventTorrentStatus, c.currentTorrentStatus)
			} else if c.currentTorrentStatus.ProgressPercentage > 0 {
				// Stream was stopped but status wasn't cleared — reset it
				c.currentTorrentStatus = TorrentStatus{}
				c.repository.sendStateEvent(eventTorrentStopped, nil)
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
}

// GetStreamingUrl returns the URL for the legacy external-player HTTP stream endpoint.
// clientId is embedded as a query param so the handler can serve this specific client's
// active stream (via activeStreams) instead of falling back to whichever torrent happens to be
// "current" for the profile - which, without it, two devices/tabs on the same profile playing
// different episodes could cross-wire.
func (c *Client) GetStreamingUrl(clientId string) string {
	if c.torrentClient.IsAbsent() {
		return ""
	}
	_, fileOpt := c.currentTorrentAndFile()
	if fileOpt.IsAbsent() {
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
	ret := fmt.Sprintf("http://%s/api/v1/torrentstream/stream/%s", address, url.PathEscape(fileOpt.MustGet().DisplayPath()))
	if strings.HasPrefix(ret, "http://http") {
		ret = strings.Replace(ret, "http://http", "http", 1)
	}
	ret += c.repository.directStreamManager.GetHMACTokenQueryParam("/api/v1/torrentstream/stream", "?")
	if clientId != "" {
		ret += "&clientId=" + url.QueryEscape(clientId)
	}
	return ret
}

// GetExternalPlayerStreamingUrl returns the URL template used by the desktop/systray external
// player integration. See GetStreamingUrl for why clientId is embedded.
func (c *Client) GetExternalPlayerStreamingUrl(clientId string) string {
	if c.torrentClient.IsAbsent() {
		return ""
	}
	_, fileOpt := c.currentTorrentAndFile()
	if fileOpt.IsAbsent() {
		return ""
	}

	ret := fmt.Sprintf("{{SCHEME}}://{{HOST}}/api/v1/torrentstream/stream/%s", url.PathEscape(fileOpt.MustGet().DisplayPath()))
	ret += c.repository.directStreamManager.GetHMACTokenQueryParam("/api/v1/torrentstream/stream", "?")
	if clientId != "" {
		ret += "&clientId=" + url.QueryEscape(clientId)
	}
	return ret
}

func (c *Client) AddTorrent(ctx context.Context, id string) (*torrent.Torrent, error) {
	if c.torrentClient.IsAbsent() {
		return nil, errors.New("torrent client is not initialized")
	}

	// Drop torrents except current stream and prepared stream
	c.dropUnclaimedTorrents()

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

// torrentURLFetchTimeout bounds how long addTorrentFromDownloadURL waits on the .torrent-file
// host (connection + headers + body). A var (not const) so tests can shrink it instead of
// waiting out the real deadline.
var torrentURLFetchTimeout = 1 * time.Minute

func (c *Client) addTorrentFromDownloadURL(url string) (*torrent.Torrent, error) {
	if c.torrentClient.IsAbsent() {
		return nil, errors.New("torrent client is not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), torrentURLFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	filename := path.Base(url)
	// os.CreateTemp (rather than os.Create with a fixed name) avoids two concurrent downloads
	// of URLs sharing a basename clobbering each other's file, and the deferred os.Remove below
	// avoids leaking one file into the OS temp directory per download-URL torrent added.
	file, err := os.CreateTemp(os.TempDir(), filename+"-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(file.Name()) }()
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
	c.dropUnclaimedTorrents()

	// Stop monitorLoop (started by initializeClient/UseSharedTorrentClient) so it doesn't
	// keep polling a closed torrent client every 3s, and take the same lock monitorLoop uses
	// so this doesn't race its reads/writes of currentTorrent/currentTorrentStatus.
	if c.cancelFunc != nil {
		c.cancelFunc()
	}

	c.mu.Lock()
	c.currentTorrent = mo.None[*torrent.Torrent]()
	c.currentTorrentStatus = TorrentStatus{}
	tc := c.torrentClient
	c.torrentClient = mo.None[*torrent.Client]()
	c.mu.Unlock()

	c.repository.logger.Debug().Msg("torrentstream: Closing torrent client")
	return tc.MustGet().Close()
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

// dropUnclaimedTorrents drops torrents that NO live session claims (across
// every wrapper sharing the engine) and deletes only those torrents'
// directories. Torrents another session is streaming are left untouched.
func (c *Client) dropUnclaimedTorrents() {
	if c.torrentClient.IsAbsent() {
		return
	}

	keepHashes := claimedHashes()

	droppedCount := 0
	for _, t := range c.torrentClient.MustGet().Torrents() {
		infoHash := t.InfoHash()
		if keepHashes[infoHash] {
			continue
		}
		name := t.Name()
		c.repository.logger.Trace().Msgf("torrentstream: Dropping unclaimed torrent: %s", infoHash)
		t.Drop()
		droppedCount++

		// Remove only this torrent's directory
		if c.repository.settings.IsPresent() && name != "" {
			torrentDir := path.Join(c.repository.settings.MustGet().DownloadDir, name)
			_ = os.RemoveAll(torrentDir)
		}
	}

	if droppedCount > 0 {
		c.repository.logger.Debug().Msgf("torrentstream: Dropped %d unclaimed torrent(s)", droppedCount)
	}
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// SetActiveStream sets the torrent and file for a session.
// Legacy fields (currentTorrent/currentFile) are maintained by StartStream/StopStream
// under c.mu — this method only manages the per-session activeStreams map.
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
// Legacy fields (currentTorrent/currentFile/currentTorrentStatus) are managed by
// StartStream/StopStream under c.mu — this method only touches the activeStreams map.
func (c *Client) RemoveActiveStream(sessionID string) {
	c.streamsMu.Lock()
	defer c.streamsMu.Unlock()
	delete(c.activeStreams, sessionID)
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
	torrentOpt, fileOpt := c.currentTorrentAndFile()
	if torrentOpt.IsAbsent() || fileOpt.IsAbsent() {
		return false
	}

	// Require the pieces actually needed to start playback (file headers, first cluster) to be
	// complete. An aggregate byte/percentage threshold isn't enough: under rarest-first-
	// influenced piece selection, a torrent can cross a byte/percentage threshold via pieces
	// scattered elsewhere in the file while these are still missing, reporting "ready" right
	// before a stall.
	return torrentutil.ImmediatePiecesComplete(torrentOpt.MustGet(), fileOpt.MustGet())
}

