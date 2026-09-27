package torrentstream

import (
	"seanime/internal/events"
	"seanime/internal/util"
	"testing"

	"github.com/goccy/go-json"
	"github.com/stretchr/testify/require"
)

// TestSendLoadingStatus guards the loading overlay's text: the client reads data.state and
// data.torrentBeingLoaded, and a bare state string left it blank for the whole load.
func TestSendLoadingStatus(t *testing.T) {
	logger := util.NewLogger()
	ws := events.NewMockWSEventManager(logger)
	repo := &Repository{logger: logger, wsEventManager: ws}

	repo.sendLoadingStatus("tv", TLSStateAddingTorrent, "[Group] Show - 01")

	sent := ws.Events()
	require.Len(t, sent, 1)
	payload, err := json.Marshal(sent[0].Payload)
	require.NoError(t, err)
	require.JSONEq(t, `{"state":"loading","data":{"state":"ADDING_TORRENT","torrentBeingLoaded":"[Group] Show - 01"}}`, string(payload))
}
