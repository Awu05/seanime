package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"seanime/internal/core"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
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
