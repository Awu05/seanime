package anilist

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goccy/go-json"
	"github.com/stretchr/testify/require"
)

// One failed batch must not cost the titles in the batches after it.
func TestCompleteAnimeByIDsKeepsBatchesAfterAFailure(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":{"Page":{"media":[{"id":99}]}}}`)
	}))
	defer server.Close()

	prevProvider := CurrentRequestProvider()
	t.Cleanup(func() { require.NoError(t, SetRequestProvider(prevProvider)) })
	require.NoError(t, UseCustomAPI(CustomClientConfig{Name: "batch-test", Endpoint: server.URL}))

	media, err := NewAnilistClient("", t.TempDir()).CompleteAnimeByIDs(context.Background(), make([]int, 60))
	require.Error(t, err)
	require.Len(t, media, 1)
	require.Equal(t, 2, requests)
}

func TestCompleteAnimeByIDsSplitsIntoBatchesOf50(t *testing.T) {
	var batches [][]int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string `json:"query"`
			Variables struct {
				IDs []int `json:"ids"`
			} `json:"variables"`
		}
		require.NoError(t, json.Unmarshal(body, &req))
		require.True(t, strings.HasPrefix(req.Query, "query CompleteAnimeByIds"))
		batches = append(batches, req.Variables.IDs)

		media := make([]string, len(req.Variables.IDs))
		for i, id := range req.Variables.IDs {
			media[i] = fmt.Sprintf(`{"id":%d}`, id)
		}
		_, _ = fmt.Fprintf(w, `{"data":{"Page":{"media":[%s]}}}`, strings.Join(media, ","))
	}))
	defer server.Close()

	prevProvider := CurrentRequestProvider()
	t.Cleanup(func() { require.NoError(t, SetRequestProvider(prevProvider)) })
	require.NoError(t, UseCustomAPI(CustomClientConfig{Name: "batch-test", Endpoint: server.URL}))

	ids := make([]int, 120)
	for i := range ids {
		ids[i] = i + 1
	}
	media, err := NewAnilistClient("", t.TempDir()).CompleteAnimeByIDs(context.Background(), ids)
	require.NoError(t, err)
	require.Len(t, media, 120)
	require.Equal(t, 120, media[119].ID)
	require.Len(t, batches, 3)
	require.Len(t, batches[0], 50)
	require.Len(t, batches[2], 20)
}
