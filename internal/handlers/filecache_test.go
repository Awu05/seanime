package handlers

import (
	"net/http"
	"net/http/httptest"
	"seanime/internal/core"
	"seanime/internal/imagecache"
	"seanime/internal/security"
	"seanime/internal/util"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// HandleSetImageCacheLimit changes a cache every profile shares, so strict mode restricts it to
// trusted local requests exactly like HandleClearOfflineCopies.
func TestHandleSetImageCacheLimitStrictMode(t *testing.T) {
	t.Cleanup(func() {
		security.SetSecureMode("")
	})

	logger := util.NewLogger()
	cache, err := imagecache.New(t.TempDir(), imagecache.DefaultMaxMB, logger)
	require.NoError(t, err)
	h := &Handler{App: &core.App{Config: &core.Config{}, Logger: logger, ImageCache: cache}}
	e := echo.New()

	t.Run("rejects requests without a trusted local origin in strict mode", func(t *testing.T) {
		security.SetSecureMode(security.SecureModeStrict)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/filecache/offline-copies/limit", nil)
		req.Host = "127.0.0.1:43211"
		req.RemoteAddr = "203.0.113.10:51111"
		req.Header.Set("Origin", "http://127.0.0.1:43211")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		require.ErrorIs(t, h.HandleSetImageCacheLimit(c), errGuardResponseWritten)
		require.Equal(t, http.StatusForbidden, rec.Code)
		require.Equal(t, imagecache.DefaultMaxMB, cache.MaxMB())
	})

	t.Run("allows trusted local origins in strict mode", func(t *testing.T) {
		security.SetSecureMode(security.SecureModeStrict)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/filecache/offline-copies/limit", strings.NewReader(`{"imageCacheMaxMB": 200}`))
		req.Header.Set(echo.HeaderContentType, "application/json")
		req.Host = "127.0.0.1:43211"
		req.RemoteAddr = "127.0.0.1:51111"
		req.Header.Set("Origin", "http://127.0.0.1:43211")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		require.NoError(t, h.HandleSetImageCacheLimit(c))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, 200, cache.MaxMB())
	})
}
