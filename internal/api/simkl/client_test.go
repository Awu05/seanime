package simkl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) (*APIClient, *httptest.Server) {
	// postLimiter is process-global (see its doc comment in client.go) and would otherwise pace
	// this package's tests against SIMKL's real production rate limit for no reason - tests
	// don't hit the real API, so there's nothing to protect here.
	postLimiter = rate.NewLimiter(rate.Inf, 1)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c := NewAPIClient(server.Client(), "test-token", "test-client-id")
	c.baseURL = server.URL
	return c, server
}

func TestAPIClient_AddToList(t *testing.T) {
	var captured animeEnvelope
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/sync/add-to-list", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "test-client-id", r.Header.Get("simkl-api-key"))
		assert.NotEmpty(t, r.Header.Get("User-Agent"))
		// SIMKL's API rules require client_id, app-name and app-version on every request, not
		// just the simkl-api-key header - omitting these is grounds for the client_id itself
		// being suspended, per SIMKL's developer docs.
		assert.Equal(t, "test-client-id", r.URL.Query().Get("client_id"))
		assert.NotEmpty(t, r.URL.Query().Get("app-name"))
		assert.NotEmpty(t, r.URL.Query().Get("app-version"))
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"added": map[string]int{"shows": 1}})
	})

	err := c.AddToList(context.Background(), 101922, "completed")
	require.NoError(t, err)
	require.Len(t, captured.Anime, 1)
	assert.Equal(t, "101922", captured.Anime[0].Ids.Anilist)
	assert.Equal(t, "completed", captured.Anime[0].To)
}

func TestAPIClient_MarkProgress(t *testing.T) {
	var captured animeEnvelope
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/sync/history", r.URL.Path)
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"added": map[string]int{"episodes": 3}})
	})

	err := c.MarkProgress(context.Background(), 101922, 3)
	require.NoError(t, err)
	require.Len(t, captured.Anime, 1)
	require.Len(t, captured.Anime[0].Episodes, 3, "should send episodes 1..3, not just the latest one")
	assert.Equal(t, 1, captured.Anime[0].Episodes[0].Number)
	assert.Equal(t, 3, captured.Anime[0].Episodes[2].Number)
}

func TestAPIClient_AddToListBatch(t *testing.T) {
	var captured animeEnvelope
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/sync/add-to-list", r.URL.Path)
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"added": map[string]int{"shows": 2}})
	})

	err := c.AddToListBatch(context.Background(), []AddToListItem{
		{AnilistID: 101922, Status: "completed"},
		{AnilistID: 21, Status: "watching"},
	})
	require.NoError(t, err)
	require.Len(t, captured.Anime, 2, "multiple items must go out in a single request body, not one call each")
	assert.Equal(t, "101922", captured.Anime[0].Ids.Anilist)
	assert.Equal(t, "completed", captured.Anime[0].To)
	assert.Equal(t, "21", captured.Anime[1].Ids.Anilist)
	assert.Equal(t, "watching", captured.Anime[1].To)
}

func TestAPIClient_RemoveEntry(t *testing.T) {
	var captured animeEnvelope
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/sync/history/remove", r.URL.Path)
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"deleted": map[string]int{"shows": 1}})
	})

	err := c.RemoveEntry(context.Background(), 101922)
	require.NoError(t, err)
	require.Len(t, captured.Anime, 1)
	assert.Empty(t, captured.Anime[0].Episodes, "no episodes/seasons means remove the whole entry")
	assert.Empty(t, captured.Anime[0].Seasons)
}

func TestAPIClient_SetRating(t *testing.T) {
	var captured animeEnvelope
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/sync/ratings", r.URL.Path)
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"added": map[string]int{"shows": 1}})
	})

	err := c.SetRating(context.Background(), 101922, 8)
	require.NoError(t, err)
	require.Len(t, captured.Anime, 1)
	assert.Equal(t, 8, captured.Anime[0].Rating)
}

func TestAPIClient_RemoveRating(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/sync/ratings/remove", r.URL.Path)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"deleted": map[string]int{"shows": 1}})
	})

	err := c.RemoveRating(context.Background(), 101922)
	require.NoError(t, err)
}

func TestAPIClient_TestConnection(t *testing.T) {
	t.Run("succeeds on 200", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"account": map[string]string{"id": "1"}})
		})
		require.NoError(t, c.TestConnection(context.Background()))
	})

	t.Run("fails on 401", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		})
		require.Error(t, c.TestConnection(context.Background()))
	})
}
