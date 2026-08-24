package torrentstream

import (
	"net/http"
	"seanime/internal/util/torrentutil"
	"strconv"
	"time"

	"github.com/anacrolix/torrent"
)

var _ = http.Handler(&handler{})

type (
	// handler serves the torrent stream
	handler struct {
		repository *Repository
	}
)

func newHandler(repository *Repository) *handler {
	return &handler{
		repository: repository,
	}
}

// resolveStreamTarget picks which torrent/file to serve for this request. If the URL carries a
// clientId matching a registered activeStreams entry, that client's own stream is used - so two
// devices/tabs on the same profile playing different episodes don't cross-wire. Otherwise (no
// clientId, or no matching entry - e.g. an older external-player URL generated before this
// field existed) it falls back to the shared legacy currentTorrent/currentFile fields.
func (h *handler) resolveStreamTarget(r *http.Request) (*torrent.Torrent, *torrent.File, bool) {
	if clientId := r.URL.Query().Get("clientId"); clientId != "" {
		if active := h.repository.client.GetActiveStream(clientId); active != nil && active.Torrent != nil && active.File != nil {
			return active.Torrent, active.File, true
		}
	}

	torrentOpt, fileOpt := h.repository.client.currentTorrentAndFile()
	if fileOpt.IsAbsent() || torrentOpt.IsAbsent() {
		return nil, nil, false
	}
	return torrentOpt.MustGet(), fileOpt.MustGet(), true
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.repository.logger.Trace().Str("range", r.Header.Get("Range")).Msg("torrentstream: Stream endpoint hit")

	t, file, ok := h.resolveStreamTarget(r)
	if !ok {
		h.repository.logger.Error().Msg("torrentstream: No torrent to stream")
		http.Error(w, "No torrent to stream", http.StatusNotFound)
		return
	}

	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", strconv.FormatInt(file.Length(), 10))
		w.Header().Set("Content-Disposition", "inline; filename="+file.DisplayPath())
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		return
	}

	h.repository.logger.Trace().Str("file", file.DisplayPath()).Msg("torrentstream: New reader")
	tr := file.NewReader()
	defer func(tr torrent.Reader) {
		h.repository.logger.Trace().Msg("torrentstream: Closing reader")
		_ = tr.Close()
	}(tr)

	tr.SetResponsive()
	// Read ahead 5MB for better streaming performance
	// DEVNOTE: Not sure if dynamic prioritization overwrites this but whatever
	tr.SetReadahead(5 * 1024 * 1024)

	// If this is a range request for a later part of the file, prioritize those pieces
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		// Attempt to prioritize the pieces requested in the range
		torrentutil.PrioritizeRangeRequestPieces(rangeHeader, t, file, h.repository.logger)
	}

	h.repository.logger.Trace().Str("file", file.DisplayPath()).Msg("torrentstream: Serving file content")
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(
		w,
		r,
		file.DisplayPath(),
		time.Now(),
		tr,
	)
	h.repository.logger.Trace().Msg("torrentstream: File content served")
}
