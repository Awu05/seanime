package anilist

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/goccy/go-json"
	"github.com/stretchr/testify/require"
)

type savedEntriesRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// useSaveEntriesServer points the client at a test server that answers with status and body, and
// returns the requests it received.
func useSaveEntriesServer(t *testing.T, status int, body string) *[]savedEntriesRequest {
	t.Helper()
	var requests []savedEntriesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req savedEntriesRequest
		require.NoError(t, json.Unmarshal(raw, &req))
		requests = append(requests, req)
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	prevProvider := CurrentRequestProvider()
	t.Cleanup(func() { require.NoError(t, SetRequestProvider(prevProvider)) })
	require.NoError(t, UseCustomAPI(CustomClientConfig{Name: "save-test", Endpoint: server.URL}))
	return &requests
}

func TestSaveMediaListEntriesSendsEachUpdatesOwnFields(t *testing.T) {
	requests := useSaveEntriesServer(t, http.StatusOK, `{"data":{"u0":{"id":1},"u1":{"id":2}}}`)
	status := MediaListStatusCompleted

	results, err := NewAnilistClient("token", t.TempDir()).SaveMediaListEntries(context.Background(), []MediaListEntryUpdate{
		{MediaID: 10, Status: &status, ScoreRaw: new(80), Progress: new(12)},
		{MediaID: 20, Progress: new(3), ProgressOnly: true},
	})
	require.NoError(t, err)
	require.Equal(t, []error{nil, nil}, results)
	require.Len(t, *requests, 1)
	req := (*requests)[0]
	require.Contains(t, req.Query, "u0: SaveMediaListEntry(mediaId: $mediaId0, status: $status0, scoreRaw: $scoreRaw0, progress: $progress0, startedAt: $startedAt0, completedAt: $completedAt0)")
	require.Contains(t, req.Query, "u1: SaveMediaListEntry(mediaId: $mediaId1, progress: $progress1, status: $status1)")
	require.EqualValues(t, 10, req.Variables["mediaId0"])
	require.Equal(t, "COMPLETED", req.Variables["status0"])
	require.EqualValues(t, 80, req.Variables["scoreRaw0"])
	require.EqualValues(t, 3, req.Variables["progress1"])
	require.NotContains(t, req.Variables, "scoreRaw1", "a progress-only update sends only its own fields")
}

func TestSaveMediaListEntriesReportsRejectedUpdates(t *testing.T) {
	useSaveEntriesServer(t, http.StatusOK, `{"data":{"u0":{"id":1},"u1":null},"errors":[{"message":"Media not found","path":["u1"]}]}`)

	results, err := NewAnilistClient("token", t.TempDir()).SaveMediaListEntries(context.Background(), []MediaListEntryUpdate{
		{MediaID: 1, Progress: new(1), ProgressOnly: true},
		{MediaID: 2, Progress: new(1), ProgressOnly: true},
	})
	require.NoError(t, err)
	require.NoError(t, results[0])
	require.ErrorContains(t, results[1], "Media not found")
}

func TestSaveMediaListEntriesFailsRequestOnErrorWithoutPath(t *testing.T) {
	useSaveEntriesServer(t, http.StatusBadRequest, `{"errors":[{"message":"Invalid token"}]}`)

	_, err := NewAnilistClient("token", t.TempDir()).SaveMediaListEntries(context.Background(), []MediaListEntryUpdate{
		{MediaID: 1, Progress: new(1), ProgressOnly: true},
	})
	require.ErrorContains(t, err, "Invalid token")
}

func TestSaveMediaListEntriesFailsRequestOnServerError(t *testing.T) {
	useSaveEntriesServer(t, http.StatusServiceUnavailable, `{}`)

	_, err := NewAnilistClient("token", t.TempDir()).SaveMediaListEntries(context.Background(), []MediaListEntryUpdate{
		{MediaID: 1, Progress: new(1), ProgressOnly: true},
	})
	require.ErrorContains(t, err, "503")
}
