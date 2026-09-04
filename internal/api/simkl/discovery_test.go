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

func TestGetAnimeCalendar(t *testing.T) {
	// NOTE: this hits data.simkl.in, a different host than api.simkl.com (APIClient.baseURL) -
	// confirmed by Task 1's live verification (docs/superpowers/plans/simkl-endpoint-findings.md).
	// It needs no client_id and its response is wrapped in a top-level "calendar" array, with
	// SimklID as a bare top-level field (not nested under "ids") and no title/poster at all.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"calendar":[{"simkl_id":123,"date":"2026-09-04T12:00:00Z","finale_type":null,"episode":{"episode":5,"title":"Episode 5","url":"https://simkl.com/anime/123/sample-anime/episode-5/"}}]}`))
	}))
	defer server.Close()

	originalCalendarURL := calendarURL
	calendarURL = server.URL
	defer func() { calendarURL = originalCalendarURL }()

	client := &APIClient{httpClient: server.Client(), clientID: "test-client-id", baseURL: "https://unused.invalid"}
	results, err := client.GetAnimeCalendar(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, 123, results[0].SimklID)
	assert.Equal(t, 5, results[0].Episode.Episode)
}

func TestGetAnimeDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/anime/46994", r.URL.Path)
		assert.Equal(t, "full", r.URL.Query().Get("extended"))
		// NOTE: anilist/mal are quoted JSON strings on the real API, not numbers - confirmed by
		// Task 1's live verification (docs/superpowers/plans/simkl-endpoint-findings.md). Only
		// "simkl" itself comes back as a bare int.
		w.Write([]byte(`{"title":"Frieren","year":2023,"poster":"14/14625673bbdc6b52ea","overview":"A story about elves.","genres":["Adventure","Drama"],"total_episodes":28,"ids":{"simkl":46994,"anilist":"154587","mal":"52991"}}`))
	}))
	defer server.Close()

	client := &APIClient{httpClient: server.Client(), clientID: "test-client-id", baseURL: server.URL}
	detail, err := client.GetAnimeDetails(context.Background(), 46994)
	require.NoError(t, err)
	assert.Equal(t, "Frieren", detail.Title)
	assert.Equal(t, "154587", detail.Ids.Anilist)
	assert.Equal(t, "14/14625673bbdc6b52ea", detail.Poster, "the calendar mapping path has no poster of its own and relies on this field")
}

func TestSearchIDByAnilist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "154587", r.URL.Query().Get("anilist"))
		w.Write([]byte(`[{"ids":{"simkl":46994,"anilist":"154587"}}]`))
	}))
	defer server.Close()

	client := &APIClient{httpClient: server.Client(), clientID: "test-client-id", baseURL: server.URL}
	simklID, ok, err := client.SearchIDByAnilist(context.Background(), 154587)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 46994, simklID)
}

func TestSearchIDByAnilist_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := &APIClient{httpClient: server.Client(), clientID: "test-client-id", baseURL: server.URL}
	_, ok, err := client.SearchIDByAnilist(context.Background(), 999999999)
	require.NoError(t, err)
	assert.False(t, ok)
}
