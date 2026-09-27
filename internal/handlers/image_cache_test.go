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

func imageCacheNavigationRequest(t *testing.T, h *Handler, rawURL string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/image-cache?url="+url.QueryEscape(rawURL), nil)
	req.Header.Set("Sec-Fetch-Mode", "navigate")
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

// A private address can't be fetched by the cache, so the browser is sent to load it directly,
// same as before this route existed.
func TestHandleGetCachedImageRedirectsPrivateAddresses(t *testing.T) {
	h := newImageCacheHandler(t)
	rec := imageCacheRequest(t, h, "http://127.0.0.1:1/a.jpg")
	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t, "http://127.0.0.1:1/a.jpg", rec.Header().Get(echo.HeaderLocation))
}

// With proxying disabled the server doesn't fetch; it sends the browser to the image itself.
func TestHandleGetCachedImageRedirectsWhenProxyDisabled(t *testing.T) {
	h := newImageCacheHandler(t, core.Proxy)
	rec := imageCacheRequest(t, h, "https://s4.anilist.co/cover.jpg")
	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t, "https://s4.anilist.co/cover.jpg", rec.Header().Get(echo.HeaderLocation))

	require.Equal(t, http.StatusBadRequest, imageCacheRequest(t, h, "javascript:alert(1)").Code)
}

// A navigation (a clicked/typed link) never hits this route legitimately, so it's rejected before
// any fetch is attempted, without leaking a Location for the browser to follow.
func TestHandleGetCachedImageRejectsNavigations(t *testing.T) {
	h := newImageCacheHandler(t)
	rec := imageCacheNavigationRequest(t, h, "https://s4.anilist.co/cover.jpg")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, rec.Header().Get(echo.HeaderLocation))
}

// The success path can't be reached from this package (imagecache.New only fetches public
// addresses), so this exercises the response helper directly instead.
func TestWriteCachedImageSetsHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/image-cache?url=https://s4.anilist.co/cover.jpg", nil)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)

	body := []byte("fake-image-bytes")
	require.NoError(t, writeCachedImage(c, "image/jpeg", body))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "private, max-age=604800", rec.Header().Get(echo.HeaderCacheControl))
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "image/jpeg", rec.Header().Get(echo.HeaderContentType))
	require.Equal(t, body, rec.Body.Bytes())
}
