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
	"path/filepath"
	"seanime/internal/mediaplayers/mediaplayer"
	"seanime/internal/util"
	"seanime/internal/util/torrentutil"
	"strings"
	"sync"
	"sync/atomic"
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

		// torrentClient is read without mu, including by code that already holds it.
		torrentClient        atomic.Pointer[torrent.Client]
		currentTorrent       mo.Option[*torrent.Torrent]
		currentFile          mo.Option[*torrent.File]
		currentTorrentStatus TorrentStatus
		cancelFunc           context.CancelFunc // stops monitorLoop; guarded by mu

		activeStreams map[string]*ActiveStream // keyed by client ID
		streamsMu     sync.RWMutex

		mu                          sync.Mutex
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

// A torrent is only registered as an active/claimed stream (see SetActiveStream) once
// StartStream has fully selected it - which can be well after the torrent handle already
// exists in the shared engine (addTorrentMagnet, for one, blocks on t.GotInfo() first). A
// concurrent drop (e.g. a StopStream for an unrelated, already-finished stream) run during that
// window sees the still-being-set-up torrent as unclaimed and drops it out from under the
// in-flight StartStream call, which then plays on with a torrent whose storage was just closed -
// surfacing much later as a stuck "Loading metadata..." that eventually fails with
// "reading from closed torrent: file already closed". recentlyAdded grants every newly added
// torrent a grace period against drops regardless of claim state, closing that window.
var (
	recentlyAddedMu sync.Mutex
	recentlyAdded   = make(map[metainfo.Hash]time.Time)
)

// A var (not const) so tests can shrink it instead of waiting out the real grace period.
var recentlyAddedGracePeriod = 2 * time.Minute

// markRecentlyAdded should be called as soon as a torrent handle is obtained from the underlying
// engine (AddMagnet/AddTorrentFromFile), before waiting on its info/metadata.
func markRecentlyAdded(h metainfo.Hash) {
	recentlyAddedMu.Lock()
	defer recentlyAddedMu.Unlock()
	recentlyAdded[h] = time.Now()

	// Opportunistic cleanup so this map doesn't grow unbounded over a long-running server.
	for hash, addedAt := range recentlyAdded {
		if time.Since(addedAt) >= recentlyAddedGracePeriod {
			delete(recentlyAdded, hash)
		}
	}
}

func isWithinAddGracePeriod(h metainfo.Hash) bool {
	recentlyAddedMu.Lock()
	defer recentlyAddedMu.Unlock()
	addedAt, ok := recentlyAdded[h]
	if !ok {
		return false
	}
	return time.Since(addedAt) < recentlyAddedGracePeriod
}

// clearRecentlyAdded should be called once a torrent's in-flight StartStream call has reached
// SetActiveStream - the exact point the race recentlyAdded exists to cover (a drop landing
// between the torrent handle existing and the claim being registered) closes. Without this, the
// grace period is a flat time.Now()-based timer that keeps protecting the torrent from a
// legitimate drop (e.g. the user terminating the stream seconds after starting it) for up to
// recentlyAddedGracePeriod after it was added, even though the claim is already fully
// established and normal claim-based protection (activeStreams/currentTorrent) has taken over.
func clearRecentlyAdded(h metainfo.Hash) {
	recentlyAddedMu.Lock()
	defer recentlyAddedMu.Unlock()
	delete(recentlyAdded, h)
}

func NewClient(repository *Repository) *Client {
	ret := &Client{
		repository:                  repository,
		currentFile:                 mo.None[*torrent.File](),
		currentTorrent:              mo.None[*torrent.Torrent](),
		activeStreams:               make(map[string]*ActiveStream),
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

// GetActiveStreamInfo returns the name and live status of the torrent currently streaming
// for this client, or ok=false if nothing is actively streaming right now. Used by the
// admin activity view — deliberately reads the legacy currentTorrent/currentTorrentStatus
// pair (the "one active stream per profile session" view), not the newer activeStreams map,
// since the admin view shows one row per profile regardless of how many tabs that profile
// has open.
func (c *Client) GetActiveStreamInfo() (name string, status TorrentStatus, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.currentTorrent.IsAbsent() {
		return "", TorrentStatus{}, false
	}
	return c.currentTorrent.MustGet().Name(), c.currentTorrentStatus, true
}

// claimedHashes returns the infohashes any live wrapper still uses:
// per-client active streams, the legacy current torrent, and preloaded streams.
//
// selfHeld, if non-nil, is the Client whose c.mu the caller already holds (e.g.
// dropUnclaimedTorrentsLocked, called from inside initializeClient/StopStream/CleanupSession's
// own c.mu.Lock()). For that one client, currentTorrent is read directly instead of through
// currentTorrentAndFile(), which would re-lock the same non-reentrant mutex and deadlock the
// caller against itself. Every other client's mutex is a different instance, so locking it via
// currentTorrentAndFile() is always safe regardless of selfHeld.
func claimedHashes(selfHeld *Client) map[metainfo.Hash]bool {
	keep := make(map[metainfo.Hash]bool)
	allClientsMu.RLock()
	defer allClientsMu.RUnlock()
	for cl := range allClients {
		// Only used for diagnostic logging below - identifies which session's claim kept a
		// torrent alive, so a torrent that unexpectedly survives a drop can be traced back to
		// its source instead of guessing.
		clientLabel := "unknown"
		if cl.repository != nil {
			cl.repository.currentClientIdMu.RLock()
			clientLabel = cl.repository.currentClientId
			cl.repository.currentClientIdMu.RUnlock()
		}
		logClaim := func(infoHash metainfo.Hash, reason string) {
			if cl.repository == nil {
				return
			}
			cl.repository.logger.Debug().
				Str("infoHash", infoHash.String()).
				Str("client", clientLabel).
				Str("reason", reason).
				Msg("torrentstream: torrent kept alive by claim")
		}

		cl.streamsMu.RLock()
		for sessionID, stream := range cl.activeStreams {
			if stream.Torrent != nil {
				keep[stream.Torrent.InfoHash()] = true
				logClaim(stream.Torrent.InfoHash(), "activeStreams["+sessionID+"]")
			}
		}
		cl.streamsMu.RUnlock()
		if cl == selfHeld {
			if t, ok := cl.currentTorrent.Get(); ok {
				keep[t.InfoHash()] = true
				logClaim(t.InfoHash(), "currentTorrent (self)")
			}
		} else if torrentOpt, _ := cl.currentTorrentAndFile(); torrentOpt.IsPresent() {
			keep[torrentOpt.MustGet().InfoHash()] = true
			logClaim(torrentOpt.MustGet().InfoHash(), "currentTorrent")
		}
		if cl.repository != nil {
			if ps, ok := cl.repository.getPreloadedStream(); ok && ps.Torrent != nil {
				keep[ps.Torrent.InfoHash()] = true
				logClaim(ps.Torrent.InfoHash(), "preloadedStream")
			}
		}
	}
	return keep
}

// SyncSharedTorrentClient points this repository at source's engine. It's called on every
// settings broadcast, not just at session creation, so a session created before the engine
// existed - or kept after it was recreated - picks up the current one instead of staying stale.
func (r *Repository) SyncSharedTorrentClient(source *Repository) {
	if source == nil || source.client == nil || r.client == nil {
		return
	}
	if tc := source.client.torrentClient.Load(); tc != nil {
		r.client.UseSharedTorrentClient(tc)
	}
}

// UseSharedTorrentClient makes this wrapper use an existing engine and (re)starts its monitor
// loop. A no-op if it already uses tc.
func (c *Client) UseSharedTorrentClient(tc *torrent.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.torrentClient.Load() == tc {
		return
	}
	c.torrentClient.Store(tc)
	c.restartMonitorLoopLocked()
}

func (c *Client) restartMonitorLoopLocked() {
	if c.cancelFunc != nil {
		c.cancelFunc()
	}
	var ctx context.Context
	ctx, c.cancelFunc = context.WithCancel(context.Background())
	go c.monitorLoop(ctx)
}

// initializeClient will create and torrent client.
// The client is designed to support only one torrent at a time, and seed it.
// Upon initialization, the client will drop all torrents.
func (c *Client) initializeClient() error {
	settings, ok := c.repository.getSettings()
	if !ok {
		return errNoSettings
	}

	// Define torrent client settings
	cfg := torrent.NewDefaultClientConfig()
	cfg.Seed = true
	cfg.DisableIPv6 = settings.DisableIPV6
	cfg.Logger = alog.Logger{}

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
	c.torrentClient.Store(client)
	c.dropUnclaimedTorrentsLocked()
	c.restartMonitorLoopLocked()
	c.mu.Unlock()

	return nil
}

// monitorLoop reports each active stream's download progress to its client, and tells the client
// when an external media player has started playing.
func (c *Client) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.repository.logger.Debug().Msg("torrentstream: Context cancelled, stopping monitor loop")
			return
		case status := <-c.mediaPlayerPlaybackStatusCh:
			_, fileOpt := c.currentTorrentAndFile()
			if fileOpt.IsPresent() && status.Duration > 0 && c.repository.playback.currentVideoDuration.CompareAndSwap(0, int64(status.Duration)) {
				c.repository.logger.Debug().Msg("torrentstream: Media player started playing the video, sending event")
				c.repository.sendStateEvent(eventTorrentStartedPlaying)
			}
		case <-ticker.C:
			c.reportStreamStatuses()
			c.logSeedingTorrents()
		}
	}
}

func (c *Client) reportStreamStatuses() {
	c.mu.Lock()
	current, hasCurrent := c.currentTorrent.Get()
	statuses := make(map[string]TorrentStatus)
	now := time.Now()
	c.streamsMu.Lock()
	for clientId, stream := range c.activeStreams {
		if stream.Torrent == nil || stream.File == nil {
			continue
		}
		stream.refreshStatus(now)
		statuses[clientId] = stream.Status
		if hasCurrent && stream.Torrent == current {
			c.currentTorrentStatus = stream.Status
		}
	}
	c.streamsMu.Unlock()
	stopped := len(statuses) == 0 && !hasCurrent && c.currentTorrentStatus != (TorrentStatus{})
	if stopped {
		c.currentTorrentStatus = TorrentStatus{}
	}
	c.mu.Unlock()

	for clientId, status := range statuses {
		c.repository.sendStateEventTo(clientId, eventTorrentStatus, status)
	}
	if stopped {
		c.repository.sendStateEvent(eventTorrentStopped, nil)
	}
}

func (c *Client) logSeedingTorrents() {
	tc := c.torrentClient.Load()
	if tc == nil || time.Since(c.timeSinceLoggedSeeding) <= 20*time.Second {
		return
	}
	c.timeSinceLoggedSeeding = time.Now()
	for _, t := range tc.Torrents() {
		if t.Seeding() {
			c.repository.logger.Trace().Msgf("torrentstream: Seeding torrent, %d peers", t.Stats().ActivePeers)
		}
	}
}

// refreshStatus recomputes s.Status, measuring speeds since the previous refresh.
func (s *ActiveStream) refreshStatus(now time.Time) {
	stats := s.Torrent.Stats()
	downloaded := s.Torrent.BytesCompleted()
	uploaded := stats.BytesWrittenData.Int64()
	elapsed := now.Sub(s.LastSpeedCheck).Seconds()

	progress := 0.0
	if length := s.File.Length(); length > 0 {
		progress = float64(s.File.BytesCompleted()) / float64(length) * 100
	}

	s.Status = TorrentStatus{
		Size:               util.Bytes(uint64(s.File.Length())),
		UploadProgress:     uploaded,
		DownloadSpeed:      formatSpeed(downloaded-s.LastBytesCompleted, elapsed),
		UploadSpeed:        formatSpeed(uploaded-s.LastBytesWrittenData, elapsed),
		DownloadProgress:   downloaded,
		ProgressPercentage: progress,
		Seeders:            stats.ConnectedSeeders,
	}
	s.LastBytesCompleted, s.LastBytesWrittenData, s.LastSpeedCheck = downloaded, uploaded, now
}

func formatSpeed(bytes int64, seconds float64) string {
	if bytes <= 0 || seconds <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/s", util.Bytes(uint64(float64(bytes)/seconds)))
}

// GetStreamingUrl returns the URL for the legacy external-player HTTP stream endpoint.
// clientId is embedded as a query param so the handler can serve this specific client's
// active stream (via activeStreams) instead of falling back to whichever torrent happens to be
// "current" for the profile - which, without it, two devices/tabs on the same profile playing
// different episodes could cross-wire.
func (c *Client) GetStreamingUrl(clientId string) string {
	if c.torrentClient.Load() == nil {
		return ""
	}
	_, fileOpt := c.currentTorrentAndFile()
	if fileOpt.IsAbsent() {
		return ""
	}
	settings, ok := c.repository.getSettings()
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
	if c.torrentClient.Load() == nil {
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
	tc := c.torrentClient.Load()
	if tc == nil {
		return nil, errors.New("torrent client is not initialized")
	}

	// Drop torrents except current stream and prepared stream
	c.dropUnclaimedTorrents()

	if strings.HasPrefix(id, "magnet") {
		return c.addTorrentMagnet(tc, id)
	}

	if strings.HasPrefix(id, "http") {
		return c.addTorrentFromDownloadURL(tc, id)
	}

	return c.addTorrentFromFile(tc, id)
}

func (c *Client) addTorrentMagnet(tc *torrent.Client, magnet string) (*torrent.Torrent, error) {
	t, err := tc.AddMagnet(magnet)
	if err != nil {
		return nil, err
	}
	markRecentlyAdded(t.InfoHash())

	c.repository.logger.Trace().Msgf("torrentstream: Waiting to retrieve torrent info")
	select {
	case <-t.GotInfo():
		break
	case <-t.Closed():
		return nil, errors.New("torrent closed")
	case <-time.After(1 * time.Minute):
		t.Drop()
		return nil, errors.New("timeout waiting for torrent info")
	}
	c.repository.logger.Info().Msgf("torrentstream: Torrent added: %s", t.InfoHash().HexString())
	return t, nil
}

func (c *Client) addTorrentFromFile(tc *torrent.Client, fp string) (*torrent.Torrent, error) {
	t, err := tc.AddTorrentFromFile(fp)
	if err != nil {
		return nil, err
	}
	markRecentlyAdded(t.InfoHash())
	c.repository.logger.Trace().Msgf("torrentstream: Waiting to retrieve torrent info")
	<-t.GotInfo()
	c.repository.logger.Info().Msgf("torrentstream: Torrent added: %s", t.InfoHash().AsString())
	return t, nil
}

// torrentURLFetchTimeout bounds how long addTorrentFromDownloadURL waits on the .torrent-file
// host (connection + headers + body). A var (not const) so tests can shrink it instead of
// waiting out the real deadline.
var torrentURLFetchTimeout = 1 * time.Minute

func (c *Client) addTorrentFromDownloadURL(tc *torrent.Client, url string) (*torrent.Torrent, error) {
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

	t, err := tc.AddTorrentFromFile(file.Name())
	if err != nil {
		return nil, err
	}
	markRecentlyAdded(t.InfoHash())
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

// Shutdown drops unclaimed torrents, stops the monitor loop and closes the engine. Only for the
// wrapper that owns the engine; sessions sharing it use Repository.CleanupSession instead.
func (c *Client) Shutdown() (errs []error) {
	if c.torrentClient.Load() == nil {
		return
	}
	c.dropUnclaimedTorrents()

	c.mu.Lock()
	if c.cancelFunc != nil {
		c.cancelFunc()
		c.cancelFunc = nil
	}
	c.currentTorrent = mo.None[*torrent.Torrent]()
	c.currentTorrentStatus = TorrentStatus{}
	tc := c.torrentClient.Swap(nil)
	c.mu.Unlock()

	if tc == nil {
		return
	}
	c.repository.logger.Debug().Msg("torrentstream: Closing torrent client")
	return tc.Close()
}

// RemoveTorrent drops a torrent the caller no longer needs, unless a session is streaming it.
func (c *Client) RemoveTorrent(infoHash string) error {
	tc := c.torrentClient.Load()
	if tc == nil {
		return errors.New("torrent client is not initialized")
	}

	c.repository.logger.Trace().Msgf("torrentstream: Removing torrent: %s", infoHash)

	for _, t := range tc.Torrents() {
		if t.InfoHash().AsString() == infoHash {
			if c.dropIfUnclaimed(t) {
				c.repository.logger.Debug().Msgf("torrentstream: Removed torrent: %s", infoHash)
			}
			return nil
		}
	}
	return fmt.Errorf("no torrent found")
}

// dropUnclaimedTorrents drops every torrent in the shared engine that no session claims, and
// deletes their data. Must not be called while holding c.mu (claimedHashes would re-lock it) -
// use dropUnclaimedTorrentsLocked instead.
func (c *Client) dropUnclaimedTorrents() {
	c.dropUnclaimedTorrentsWithClaims(claimedHashes(nil))
}

// dropUnclaimedTorrentsLocked is dropUnclaimedTorrents for callers that hold c.mu.
func (c *Client) dropUnclaimedTorrentsLocked() {
	c.dropUnclaimedTorrentsWithClaims(claimedHashes(c))
}

func (c *Client) dropUnclaimedTorrentsWithClaims(keepHashes map[metainfo.Hash]bool) {
	tc := c.torrentClient.Load()
	if tc == nil {
		return
	}

	droppedCount := 0
	for _, t := range tc.Torrents() {
		infoHash := t.InfoHash()
		if keepHashes[infoHash] {
			continue
		}
		if isWithinAddGracePeriod(infoHash) {
			// Still being set up by an in-flight StartStream call that hasn't reached
			// SetActiveStream yet - see recentlyAdded's doc comment.
			c.repository.logger.Debug().Str("infoHash", infoHash.String()).Msg("torrentstream: torrent kept alive by add grace period")
			continue
		}
		c.repository.logger.Trace().Msgf("torrentstream: Dropping unclaimed torrent: %s", infoHash)
		c.dropTorrentAndData(t)
		droppedCount++
	}

	if droppedCount > 0 {
		c.repository.logger.Debug().Msgf("torrentstream: Dropped %d unclaimed torrent(s)", droppedCount)
	}
}

// dropIfUnclaimed drops a torrent a caller added for its own short-lived use (file previews, a
// rejected auto-select candidate, a cancelled preload), unless a session is streaming it: the
// shared engine returns the same handle for an infohash that's already loaded. Must not be called
// while holding c.mu (see claimedHashes).
func (c *Client) dropIfUnclaimed(t *torrent.Torrent) bool {
	if claimedHashes(nil)[t.InfoHash()] {
		return false
	}
	c.dropTorrentAndData(t)
	return true
}

func (c *Client) dropTorrentAndData(t *torrent.Torrent) {
	infoHash := t.InfoHash()
	t.Drop()
	// Data lives in DownloadDir/<infohash> (storage.NewFileByInfoHash), never under the torrent's
	// name, which comes from untrusted metadata.
	if settings, ok := c.repository.getSettings(); ok {
		_ = os.RemoveAll(filepath.Join(settings.DownloadDir, infoHash.HexString()))
	}
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// SetActiveStream sets the torrent and file for a session.
// Legacy fields (currentTorrent/currentFile) are maintained by StartStream/StopStream
// under c.mu — this method only manages the per-session activeStreams map.
func (c *Client) SetActiveStream(sessionID string, t *torrent.Torrent, f *torrent.File) {
	// The claim is now tracked below, so the add-grace-period stopgap (which exists only to
	// bridge the window before this point) is no longer needed for this torrent.
	if t != nil {
		clearRecentlyAdded(t.InfoHash())
	}

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
