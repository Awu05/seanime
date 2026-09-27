package torrentstream

import "seanime/internal/events"

const (
	eventLoading               = "loading"
	eventLoadingFailed         = "loading-failed"
	eventTorrentLoaded         = "loaded"
	eventTorrentStartedPlaying = "started-playing"
	eventTorrentStatus         = "status"
	eventTorrentStopped        = "stopped"
	// Tells the client to send a PreloadStream request for the next episode
	eventPreloadNextStream = "preload-next-stream"
)

type TorrentLoadingStatusState string

const (
	TLSStateSearchingTorrents          TorrentLoadingStatusState = "SEARCHING_TORRENTS"
	TLSStateAddingTorrent              TorrentLoadingStatusState = "ADDING_TORRENT"
	TLSStateSendingStreamToMediaPlayer TorrentLoadingStatusState = "SENDING_STREAM_TO_MEDIA_PLAYER"
)

// TorrentLoadingStatus is the data of a "loading" event, shown by the client's loading overlay.
type TorrentLoadingStatus struct {
	State              TorrentLoadingStatusState `json:"state"`
	TorrentBeingLoaded string                    `json:"torrentBeingLoaded,omitempty"`
}

// sendLoadingStatus tells clientId which step of starting its stream is running.
func (r *Repository) sendLoadingStatus(clientId string, state TorrentLoadingStatusState, torrentName string) {
	r.sendStateEventTo(clientId, eventLoading, TorrentLoadingStatus{State: state, TorrentBeingLoaded: torrentName})
}

// sendStateEvent sends to the client of the most recently started stream.
func (r *Repository) sendStateEvent(event string, data ...interface{}) {
	r.currentClientIdMu.RLock()
	clientId := r.currentClientId
	r.currentClientIdMu.RUnlock()
	r.sendStateEventTo(clientId, event, data...)
}

// sendStateEventTo sends to clientId, or broadcasts if it's empty.
func (r *Repository) sendStateEventTo(clientId string, event string, data ...interface{}) {
	var dataToSend interface{}
	if len(data) > 0 {
		dataToSend = data[0]
	}
	payload := struct {
		State string      `json:"state"`
		Data  interface{} `json:"data"`
	}{
		State: event,
		Data:  dataToSend,
	}
	if clientId != "" {
		r.wsEventManager.SendEventTo(clientId, events.TorrentStreamState, payload)
	} else {
		r.wsEventManager.SendEvent(events.TorrentStreamState, payload)
	}
}
