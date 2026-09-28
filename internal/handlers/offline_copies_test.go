package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"seanime/internal/api/anilist"
	"seanime/internal/core"
	"seanime/internal/imagecache"
	"seanime/internal/platforms/shared_platform"
	"seanime/internal/util"
	"seanime/internal/util/diskstore"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func newOfflineCopiesHandler(t *testing.T, multiUser bool) *Handler {
	t.Helper()
	logger := util.NewLogger()
	episodeInfo, err := diskstore.New(t.TempDir(), func() int64 { return 1 << 30 }, logger)
	require.NoError(t, err)
	images, err := imagecache.New(t.TempDir(), imagecache.DefaultMaxMB, logger)
	require.NoError(t, err)
	titles, err := shared_platform.NewTitleCache(t.TempDir(), 1<<30, logger)
	require.NoError(t, err)
	return &Handler{App: &core.App{Logger: logger, MultiUserEnabled: multiUser, EpisodeInfoStore: episodeInfo, ImageCache: images, TitleCache: titles}}
}

// Saved title records live under offline-copies too, so the card must count and clear them.
func TestOfflineCopiesIncludeTitleCache(t *testing.T) {
	h := newOfflineCopiesHandler(t, false)
	h.App.TitleCache.PutAnime(&anilist.BaseAnime{ID: 1})
	require.Positive(t, h.App.TitleCache.Size())

	c, rec := offlineCopiesContext(http.MethodGet, "/api/v1/filecache/offline-copies", nil, false)
	require.NoError(t, h.HandleGetOfflineCopies(c))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), `"totalSize":"0 B"`)

	c, rec = offlineCopiesContext(http.MethodDelete, "/api/v1/filecache/offline-copies", nil, false)
	require.NoError(t, h.HandleClearOfflineCopies(c))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, h.App.TitleCache.Size())
}

func offlineCopiesContext(method, path string, body []byte, isAdmin bool) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.Set("isAdmin", isAdmin)
	return c, rec
}

// Every profile shares these caches, so on a multi-profile server only the admin manages them.
func TestOfflineCopiesRequireAdminWithProfiles(t *testing.T) {
	h := newOfflineCopiesHandler(t, true)

	c, rec := offlineCopiesContext(http.MethodGet, "/api/v1/filecache/offline-copies", nil, false)
	require.NoError(t, h.HandleGetOfflineCopies(c))
	require.Equal(t, http.StatusForbidden, rec.Code)

	c, rec = offlineCopiesContext(http.MethodDelete, "/api/v1/filecache/offline-copies", nil, false)
	require.NoError(t, h.HandleClearOfflineCopies(c))
	require.Equal(t, http.StatusForbidden, rec.Code)

	c, rec = offlineCopiesContext(http.MethodPost, "/api/v1/filecache/offline-copies/limit", []byte(`{"imageCacheMaxMB":2048}`), false)
	require.NoError(t, h.HandleSetImageCacheLimit(c))
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, imagecache.DefaultMaxMB, h.App.ImageCache.MaxMB())
}

// Single-user installs have no admin flag, so they must still be allowed.
func TestOfflineCopiesAllowedOnSingleUser(t *testing.T) {
	h := newOfflineCopiesHandler(t, false)
	require.NoError(t, h.App.EpisodeInfoStore.Put("k", make([]byte, 2048)))

	c, rec := offlineCopiesContext(http.MethodGet, "/api/v1/filecache/offline-copies", nil, false)
	require.NoError(t, h.HandleGetOfflineCopies(c))
	require.Equal(t, http.StatusOK, rec.Code)

	var res struct {
		Data OfflineCopiesInfo `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	require.Equal(t, imagecache.DefaultMaxMB, res.Data.ImageCacheMaxMB)
	require.NotEmpty(t, res.Data.TotalSize)
}

func TestClearOfflineCopiesEmptiesBothStores(t *testing.T) {
	h := newOfflineCopiesHandler(t, true)
	require.NoError(t, h.App.EpisodeInfoStore.Put("k", []byte("episode info")))

	c, rec := offlineCopiesContext(http.MethodDelete, "/api/v1/filecache/offline-copies", nil, true)
	require.NoError(t, h.HandleClearOfflineCopies(c))
	require.Equal(t, http.StatusOK, rec.Code)
	require.EqualValues(t, 0, h.App.EpisodeInfoStore.Size())
	require.EqualValues(t, 0, h.App.ImageCache.Size())
}

func TestSetImageCacheLimit(t *testing.T) {
	h := newOfflineCopiesHandler(t, true)

	c, rec := offlineCopiesContext(http.MethodPost, "/api/v1/filecache/offline-copies/limit", []byte(`{"imageCacheMaxMB":50}`), true)
	require.NoError(t, h.HandleSetImageCacheLimit(c))
	require.NotEqual(t, http.StatusOK, rec.Code, "below the minimum is refused")
	require.Equal(t, imagecache.DefaultMaxMB, h.App.ImageCache.MaxMB())

	c, rec = offlineCopiesContext(http.MethodPost, "/api/v1/filecache/offline-copies/limit", []byte(`{"imageCacheMaxMB":2048}`), true)
	require.NoError(t, h.HandleSetImageCacheLimit(c))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 2048, h.App.ImageCache.MaxMB())
}
