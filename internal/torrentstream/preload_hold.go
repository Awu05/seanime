package torrentstream

import (
	"seanime/internal/util"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

// A preloaded torrent has its own peers, so piece priorities can't stop it from taking bandwidth
// from a stream that still needs data, e.g. one that resumed mid-file with nothing downloaded
// there yet. So its data download is paused (held) until every file that's streaming is fully
// downloaded. A streamed torrent is never held.

const preloadHoldPollEvery = 2 * time.Second

// preloadHolds counts the holds on each torrent. A torrent's download is paused from its first
// hold until its last release, so a hold that ends can't resume a torrent another hold still waits on.
var preloadHolds = struct {
	sync.Mutex
	count map[metainfo.Hash]int
}{count: make(map[metainfo.Hash]int)}

func holdTorrentDownload(t *torrent.Torrent) {
	preloadHolds.Lock()
	defer preloadHolds.Unlock()
	preloadHolds.count[t.InfoHash()]++
	t.DisallowDataDownload()
}

func releaseTorrentDownload(t *torrent.Torrent) {
	preloadHolds.Lock()
	defer preloadHolds.Unlock()
	infoHash := t.InfoHash()
	preloadHolds.count[infoHash]--
	if preloadHolds.count[infoHash] > 0 {
		return
	}
	delete(preloadHolds.count, infoHash)
	select {
	case <-t.Closed():
	default:
		t.AllowDataDownload()
	}
}

// resumeStreamedTorrent ends the hold on t because a session streams it, which must download
// regardless of any other holds on it.
func resumeStreamedTorrent(t *torrent.Torrent) {
	releaseTorrentDownload(t)
	t.AllowDataDownload()
}

// holdPreloadDownload pauses ps's download until streamingFilesComplete, releasing the hold early
// if ps is consumed, replaced or cancelled.
func (r *Repository) holdPreloadDownload(ps *preloadedStream) {
	t := ps.Torrent
	holdTorrentDownload(t)
	// StartStream registers its stream before allowing the download, so checking after holding
	// can't miss a stream that started in between.
	if streamedByAnySession(t) {
		resumeStreamedTorrent(t)
		return
	}
	r.logger.Debug().Str("torrent", t.Name()).Msg("torrentstream: Holding preload download until streaming files are downloaded")

	go func() {
		defer util.HandlePanicInModuleThen("torrentstream/preload_hold/holdPreloadDownload", func() {})
		ticker := time.NewTicker(preloadHoldPollEvery)
		defer ticker.Stop()
		for range ticker.C {
			if streamedByAnySession(t) {
				resumeStreamedTorrent(t)
				return
			}
			if current, ok := r.getPreloadedStream(); !ok || current != ps {
				releaseTorrentDownload(t)
				return
			}
			if streamingFilesComplete() {
				releaseTorrentDownload(t)
				r.logger.Debug().Str("torrent", t.Name()).Msg("torrentstream: Streaming files are downloaded, preload hold released")
				return
			}
		}
	}()
}

// streamingFiles returns the files every session streams right now (preloads don't count).
func streamingFiles() []*torrent.File {
	var files []*torrent.File
	for _, cl := range snapshotClients() {
		cl.streamsMu.RLock()
		for _, stream := range cl.activeStreams {
			if stream.File != nil {
				files = append(files, stream.File)
			}
		}
		cl.streamsMu.RUnlock()
		if _, fileOpt := cl.currentTorrentAndFile(); fileOpt.IsPresent() {
			files = append(files, fileOpt.MustGet())
		}
	}
	return files
}

func streamedByAnySession(t *torrent.Torrent) bool {
	infoHash := t.InfoHash()
	for _, f := range streamingFiles() {
		if f.Torrent().InfoHash() == infoHash {
			return true
		}
	}
	return false
}

// streamingFilesComplete reports whether every streaming file is fully downloaded. Unlike reader
// positions, this also covers the external player, whose handler reads without a priority manager.
func streamingFilesComplete() bool {
	for _, f := range streamingFiles() {
		if f.BytesCompleted() < f.Length() {
			return false
		}
	}
	return true
}
