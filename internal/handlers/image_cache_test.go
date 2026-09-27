package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"seanime/internal/core"
	"seanime/internal/imagecache"
	"seanime/internal/util"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func imageCacheRequest(t *testing.T, h *Handler, rawURL string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/image-cache?url="+url.QueryEscape(rawURL), nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h.HandleGetCachedImage(echo.New().NewContext(req, rec)))
	return rec
}

func newImageCacheHandler(t *testing.T, disabled ...core.FeatureKey) *Handler {
	t.Helper()
	logger := util.NewLogger()
	cache, err := imagecache.New(t.TempDir(), imagecache.DefaultMaxMB, logger)
	require.NoError(t, err)
	return &Handler{App: &core.App{
		Logger:         logger,
		ImageCache:     cache,
		FeatureManager: core.NewFeatureManager(logger, core.SeanimeFlags{DisableFeatures: disabled}),
	}}
}

func TestHandleGetCachedImageRejectsBadURLs(t *testing.T) {
	h := newImageCacheHandler(t)
	require.Equal(t, http.StatusBadRequest, imageCacheRequest(t, h, "").Code)
	require.Equal(t, http.StatusBadRequest, imageCacheRequest(t, h, "file:///etc/passwd").Code)
}

// A private address can't be fetched, so it fails as an upstream error, not a crash or a fetch.
func TestHandleGetCachedImageRefusesPrivateAddresses(t *testing.T) {
	h := newImageCacheHandler(t)
	require.Equal(t, http.StatusBadGateway, imageCacheRequest(t, h, "http://127.0.0.1:1/a.jpg").Code)
}

// With proxying disabled the server doesn't fetch; it sends the browser to the image itself.
func TestHandleGetCachedImageRedirectsWhenProxyDisabled(t *testing.T) {
	h := newImageCacheHandler(t, core.Proxy)
	rec := imageCacheRequest(t, h, "https://s4.anilist.co/cover.jpg")
	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t, "https://s4.anilist.co/cover.jpg", rec.Header().Get(echo.HeaderLocation))

	require.Equal(t, http.StatusBadRequest, imageCacheRequest(t, h, "javascript:alert(1)").Code)
}
