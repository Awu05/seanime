package handlers

import (
	"net/http"
	"net/http/httptest"
	"seanime/internal/core"
	"seanime/internal/database/db"
	"seanime/internal/database/models"
	"seanime/internal/util"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// TestExternalStreamToken guards torrent stream URLs opened by external players (VLC, mpv), which
// can't send the login cookie: with multi-user auth they were rejected outright, and even with a
// valid credential the request carried no profile, so it was served from the wrong session.
func TestExternalStreamToken(t *testing.T) {
	const secret = "test-jwt-secret"
	database, err := db.NewDatabase("", "test", util.NewLogger())
	require.NoError(t, err)
	profile, err := database.CreateProfile(&models.Profile{UUIDBaseModel: models.UUIDBaseModel{ID: "profile-1"}, Name: "Alice"})
	require.NoError(t, err)

	app := &core.App{Config: &core.Config{}, MultiUserEnabled: true, JWTSecret: secret, Database: database}
	h := &Handler{App: app}
	token, err := core.GenerateStreamToken(secret, profile.ID)
	require.NoError(t, err)

	request := func(path string) *http.Request {
		return httptest.NewRequest(http.MethodGet, path+"?auth_token="+token, nil)
	}
	serve := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		next := func(c echo.Context) error { return c.String(http.StatusOK, core.GetProfileIDFromContext(c)) }
		require.NoError(t, h.MultiUserAuthMiddleware(next)(echo.New().NewContext(request(path), rec)))
		return rec
	}

	t.Run("opens the profile's own torrent stream, without a login cookie", func(t *testing.T) {
		rec := serve("/api/v1/torrentstream/stream/Show%20-%2001.mkv")
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, profile.ID, rec.Body.String())
		require.Empty(t, rec.Header().Get("Set-Cookie"))
	})

	t.Run("opens a watch party stream played in a desktop player", func(t *testing.T) {
		require.Equal(t, http.StatusOK, serve("/api/v1/nakama/stream").Code)
	})

	t.Run("grants nothing beyond the stream", func(t *testing.T) {
		require.Equal(t, http.StatusForbidden, serve("/api/v1/settings").Code)
		require.Equal(t, http.StatusForbidden, serve("/api/v1/nakama/stream-settings").Code)
	})

	t.Run("passes the request boundary only on the stream path", func(t *testing.T) {
		require.True(t, isAuthenticatedMultiUserSession(app, request("/api/v1/torrentstream/stream/Show%20-%2001.mkv")))
		require.False(t, isAuthenticatedMultiUserSession(app, request("/api/v1/settings")))
	})
}
