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
	TLSStateLoading                    TorrentLoadingStatusState = "LOADING"
	TLSStateSearchingTorrents          TorrentLoadingStatusState = "SEARCHING_TORRENTS"
	TLSStateCheckingTorrent            TorrentLoadingStatusState = "CHECKING_TORRENT"
	TLSStateAddingTorrent              TorrentLoadingStatusState = "ADDING_TORRENT"
	TLSStateSelectingFile              TorrentLoadingStatusState = "SELECTING_FILE"
	TLSStateStartingServer             TorrentLoadingStatusState = "STARTING_SERVER"
	TLSStateSendingStreamToMediaPlayer TorrentLoadingStatusState = "SENDING_STREAM_TO_MEDIA_PLAYER"
)

type TorrentStreamState struct {
	State string `json:"state"`
}

func (r *Repository) sendStateEvent(event string, data ...interface{}) {
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
	// Send to the specific client if we have a client ID, otherwise broadcast
	r.currentClientIdMu.RLock()
	clientId := r.currentClientId
	r.currentClientIdMu.RUnlock()
	if clientId != "" {
		r.wsEventManager.SendEventTo(clientId, events.TorrentStreamState, payload)
	} else {
		r.wsEventManager.SendEvent(events.TorrentStreamState, payload)
	}
}

//func (r *Repository) sendTorrentLoadingStatus(event TorrentLoadingStatusState, checking string) {
//	r.wsEventManager.SendEvent(eventTorrentLoadingStatus, &TorrentLoadingStatus{
//		TorrentBeingChecked: checking,
//		State:               event,
//	})
//}
