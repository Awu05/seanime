package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"seanime/internal/core"
	"seanime/internal/events"
	"seanime/internal/util"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleGetAdminActivityRequiresAdmin(t *testing.T) {
	e := echo.New()
	h := &Handler{App: &core.App{}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/activity", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("isAdmin", false)

	err := h.HandleGetAdminActivity(c)
	assert.NoError(t, err) // handler writes the response itself, doesn't return an error
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestHandleTerminateProfileStreamRequiresAdmin(t *testing.T) {
	e := echo.New()
	h := &Handler{App: &core.App{}}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/activity/terminate", bytes.NewReader([]byte(`{"profileId":"profile-1"}`)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("isAdmin", false)

	err := h.HandleTerminateProfileStream(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestHandleTerminateProfileStreamNoSessionIsSuccess(t *testing.T) {
	e := echo.New()
	sm := core.NewStreamSessionManager(0)
	t.Cleanup(sm.Stop)
	h := &Handler{App: &core.App{StreamSessionManager: sm}}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/activity/terminate", bytes.NewReader([]byte(`{"profileId":"no-such-profile"}`)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("isAdmin", true)

	err := h.HandleTerminateProfileStream(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleGetAdminActivityBuildsConnectionsSnapshot(t *testing.T) {
	e := echo.New()
	wsManager := events.NewWSEventManager(util.NewLogger())
	wsManager.AddConn("client-1", "profile-1", nil, "web")

	h := &Handler{App: &core.App{WSEventManager: wsManager}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/activity", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("isAdmin", true)

	err := h.HandleGetAdminActivity(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data AdminActivitySnapshot `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	require.Len(t, resp.Data.Connections, 1)
	require.Equal(t, "profile-1", resp.Data.Connections[0].ProfileID)
	require.Equal(t, "profile-1", resp.Data.Connections[0].ProfileName) // h.App.Database is nil -> resolveProfileName falls back to the raw ID
	require.Equal(t, "web", resp.Data.Connections[0].Platform)
	require.False(t, resp.Data.Connections[0].ConnectedAt.IsZero())
	require.Empty(t, resp.Data.Streams) // no StreamSessionManager set on this App -> guarded nil-check short-circuits, Streams stays empty
}
