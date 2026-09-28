package handlers

import (
	"net/http"
	"net/http/httptest"
	"seanime/internal/core"
	"seanime/internal/util"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func runSingleUserOnly(t *testing.T, multiUser bool) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	h := &Handler{App: &core.App{Logger: util.NewLogger(), MultiUserEnabled: multiUser}}
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/api/v1/local/offline", nil), rec)
	called := false
	err := h.singleUserOnlyMiddleware(func(echo.Context) error {
		called = true
		return nil
	})(c)
	require.NoError(t, err)
	return rec, called
}

// The old offline mode syncs only the main account and switches every profile at once.
func TestLocalEndpointsRefusedWithProfiles(t *testing.T) {
	rec, called := runSingleUserOnly(t, true)
	require.False(t, called)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "Offline mode isn't available when profiles are enabled")
}

func TestLocalEndpointsAllowedOnSingleUser(t *testing.T) {
	_, called := runSingleUserOnly(t, false)
	require.True(t, called)
}
