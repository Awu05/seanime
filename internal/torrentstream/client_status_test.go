package torrentstream

import (
	"encoding/json"
	"seanime/internal/events"
	"seanime/internal/util"
	"sync"
	"testing"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
)

type clientRecordingWS struct {
	*events.MockWSEventManager
	mu   sync.Mutex
	sent map[string][]TorrentStatus
}

func (w *clientRecordingWS) SendEventTo(clientId string, _ string, payload interface{}, _ ...bool) {
	var decoded struct {
		State string        `json:"state"`
		Data  TorrentStatus `json:"data"`
	}
	b, _ := json.Marshal(payload)
	_ = json.Unmarshal(b, &decoded)
	if decoded.State != eventTorrentStatus {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sent[clientId] = append(w.sent[clientId], decoded.Data)
}

func addSizedTestTorrent(t *testing.T, tc *torrent.Client, name string, length int64) *torrent.Torrent {
	t.Helper()
	infoBytes, err := bencode.Marshal(metainfo.Info{
		Name:        name,
		Length:      length,
		PieceLength: length,
		Pieces:      make([]byte, metainfo.HashSize),
	})
	require.NoError(t, err)
	tor, err := tc.AddTorrent(&metainfo.MetaInfo{InfoBytes: infoBytes})
	require.NoError(t, err)
	return tor
}

// TestReportStreamStatusesSendsEachClientItsOwnStream guards the progress shown to two devices
// on one profile streaming different torrents: every device used to receive whichever stream the
// status map happened to iterate last, so both progress displays flickered between the two.
func TestReportStreamStatusesSendsEachClientItsOwnStream(t *testing.T) {
	tc := newTestTorrentClient(t)
	tvTorrent := addSizedTestTorrent(t, tc, "tv-episode.mkv", 1<<14)
	phoneTorrent := addSizedTestTorrent(t, tc, "phone-episode.mkv", 1<<15)

	ws := &clientRecordingWS{MockWSEventManager: events.NewMockWSEventManager(util.NewLogger()), sent: map[string][]TorrentStatus{}}
	repo := &Repository{logger: util.NewLogger(), wsEventManager: ws}
	c := NewClient(repo)
	repo.client = c
	t.Cleanup(func() { unregisterClient(c) })
	c.torrentClient.Store(tc)

	c.SetActiveStream("tv", tvTorrent, tvTorrent.Files()[0])
	c.SetActiveStream("phone", phoneTorrent, phoneTorrent.Files()[0])
	c.currentTorrent = mo.Some(phoneTorrent)

	c.reportStreamStatuses()

	require.Len(t, ws.sent["tv"], 1)
	require.Len(t, ws.sent["phone"], 1)
	require.Equal(t, util.Bytes(1<<14), ws.sent["tv"][0].Size)
	require.Equal(t, util.Bytes(1<<15), ws.sent["phone"][0].Size)
	require.Equal(t, ws.sent["phone"][0], c.currentTorrentStatus, "the admin view's status must follow the current stream")
}
