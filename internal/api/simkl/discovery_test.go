package simkl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchAnime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/search/anime", r.URL.Path)
		assert.Equal(t, "frieren", r.URL.Query().Get("q"))
		assert.Equal(t, "test-client-id", r.URL.Query().Get("client_id"))
		w.Write([]byte(`[{"title":"Frieren: Beyond Journey's End","year":2023,"poster":"ab/abc123","ids":{"simkl_id":46994,"slug":"frieren"}}]`))
	}))
	defer server.Close()

	client := &APIClient{httpClient: server.Client(), clientID: "test-client-id", baseURL: server.URL}
	results, err := client.SearchAnime(context.Background(), "frieren")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Frieren: Beyond Journey's End", results[0].Title)
	assert.Equal(t, 46994, results[0].Ids.SimklID)
}

func TestGetTrendingAnime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"title":"Sample Anime","year":2024,"poster":"cd/cde456","ids":{"simkl_id":123,"slug":"sample-anime"}}]`))
	}))
	defer server.Close()

	client := &APIClient{httpClient: server.Client(), clientID: "test-client-id", baseURL: server.URL}
	results, err := client.GetTrendingAnime(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Sample Anime", results[0].Title)
}
