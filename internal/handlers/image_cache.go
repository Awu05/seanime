package handlers

import (
	"net/http"
	"net/url"
	"seanime/internal/core"

	"github.com/labstack/echo/v4"
)

// HandleGetCachedImage
//
//	@summary returns an image through the server's image cache.
//	@desc Serves the saved copy when there is one, otherwise fetches, saves and serves the image, so images still show during an internet outage. If the fetch fails for any reason, redirects to the original URL so the browser loads it directly as before; during an outage that load fails too and the placeholder shows.
//	@route /api/v1/image-cache [GET]
//	@param url - string - true - "The absolute http(s) image URL"
//	@returns string
func (h *Handler) HandleGetCachedImage(c echo.Context) error {
	rawURL := c.QueryParam("url")
	if !isHTTPURL(rawURL) {
		return c.String(http.StatusBadRequest, "An http(s) image URL is required")
	}

	// A navigation (a clicked/typed link) has no business landing here - only an <img> or CSS load
	// ever hits this route. Browsers that don't send Sec-Fetch-Mode are unaffected.
	if c.Request().Header.Get("Sec-Fetch-Mode") == "navigate" {
		return c.String(http.StatusBadRequest, "This URL only serves images")
	}

	// The cache fetches outside URLs for the browser, so it follows the proxy feature switch.
	if h.App.FeatureManager != nil && h.App.FeatureManager.IsDisabled(core.Proxy) {
		return c.Redirect(http.StatusFound, rawURL)
	}

	body, contentType, err := h.App.ImageCache.Get(c.Request().Context(), rawURL)
	if err != nil {
		// Fall back to a direct load, same as before this route existed; during an outage that
		// fails too and the placeholder shows.
		h.App.Logger.Debug().Err(err).Str("url", rawURL).Msg("image cache: Could not serve image")
		return c.Redirect(http.StatusFound, rawURL)
	}

	return writeCachedImage(c, contentType, body)
}

func writeCachedImage(c echo.Context, contentType string, body []byte) error {
	header := c.Response().Header()
	// A week matches the store's 24h access refresh so trimming doesn't delete popular covers;
	// private since a shared proxy shouldn't cache a response that may carry a renewed Set-Cookie.
	header.Set(echo.HeaderCacheControl, "private, max-age=604800")
	header.Set("X-Content-Type-Options", "nosniff")
	return c.Blob(http.StatusOK, contentType, body)
}

func isHTTPURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
