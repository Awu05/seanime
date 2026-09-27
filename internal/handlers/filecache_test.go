package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"seanime/internal/core"
	"seanime/internal/imagecache"
	"seanime/internal/security"
	"seanime/internal/util"
	"seanime/internal/util/diskstore"
	"seanime/internal/util/filecache"
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

// The offline copies (saved episode info + cached images) live under the same cache directory the
// old cache card totals up, but they're shown and cleared from their own "Offline copies" card, so
// the old card's total shouldn't double-count them.
func TestHandleGetFileCacheTotalSizeExcludesOfflineCopies(t *testing.T) {
	dir := t.TempDir()
	fileCacher, err := filecache.NewCacher(dir)
	require.NoError(t, err)

	regularContents := []byte("regular-cache-file-contents")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "somebucket_a-key.json"), regularContents, 0o644))

	episodeInfoStore, err := diskstore.New(filepath.Join(dir, core.OfflineCopiesDirName, "episode-info"), func() int64 { return 1 << 30 }, util.NewLogger())
	require.NoError(t, err)
	require.NoError(t, episodeInfoStore.Put("anime-1", []byte("saved-episode-info-bytes")))

	cfg := &core.Config{}
	cfg.Cache.Dir = dir
	h := &Handler{App: &core.App{FileCacher: fileCacher, EpisodeInfoStore: episodeInfoStore, Config: cfg}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/filecache/total-size", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h.HandleGetFileCacheTotalSize(echo.New().NewContext(req, rec)))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp SeaResponse[string]
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, util.Bytes(uint64(len(regularContents))), resp.Data)
}
