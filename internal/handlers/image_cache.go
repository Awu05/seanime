package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"seanime/internal/core"
	"seanime/internal/imagecache"

	"github.com/labstack/echo/v4"
)

// HandleGetCachedImage
//
//	@summary returns an image through the server's image cache.
//	@desc Serves the saved copy when there is one, otherwise fetches, saves and serves the image, so images still show during an internet outage.
//	@route /api/v1/image-cache [GET]
//	@param url - string - true - "The absolute http(s) image URL"
//	@returns string
func (h *Handler) HandleGetCachedImage(c echo.Context) error {
	rawURL := c.QueryParam("url")
	if !isHTTPURL(rawURL) {
		return c.String(http.StatusBadRequest, "An http(s) image URL is required")
	}

	// The cache fetches outside URLs for the browser, so it follows the proxy feature switch.
	if h.App.FeatureManager != nil && h.App.FeatureManager.IsDisabled(core.Proxy) {
		return c.Redirect(http.StatusFound, rawURL)
	}

	body, contentType, err := h.App.ImageCache.Get(c.Request().Context(), rawURL)
	if err != nil {
		if errors.Is(err, imagecache.ErrInvalidURL) {
			return c.String(http.StatusBadRequest, "An http(s) image URL is required")
		}
		// A private-network source (e.g. a LAN manga server) or a non-image response (e.g. an
		// extension source without a proper Content-Type) can't be cached, but the browser can
		// still load it directly like it did before this route existed, so send it there instead
		// of failing the image outright.
		if errors.Is(err, imagecache.ErrPrivateAddress) || errors.Is(err, imagecache.ErrNotImage) {
			return c.Redirect(http.StatusFound, rawURL)
		}
		h.App.Logger.Debug().Err(err).Str("url", rawURL).Msg("image cache: Could not serve image")
		return c.String(http.StatusBadGateway, "Could not load image")
	}

	header := c.Response().Header()
	header.Set(echo.HeaderCacheControl, "public, max-age=31536000")
	header.Set("X-Content-Type-Options", "nosniff")
	return c.Blob(http.StatusOK, contentType, body)
}

func isHTTPURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
