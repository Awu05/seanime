package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func newMediastreamContext(contextClientID string, query string) echo.Context {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/mediastream/direct"+query, nil)
	c := echo.New().NewContext(req, httptest.NewRecorder())
	if contextClientID != "" {
		c.Set(clientIdCookieName, contextClientID)
	}
	return c
}

func loaded(ids ...string) func(string) bool {
	return func(id string) bool {
		for _, l := range ids {
			if l == id {
				return true
			}
		}
		return false
	}
}

// The file is loaded under the client ID of the request that asked for it. Stream,
// subtitle and attachment requests used to look it up by the clientId query parameter
// alone, falling back to "default", so URLs without that parameter (direct play,
// subtitles, fonts, HLS segments) always failed with "no file has been loaded".
func TestMediastreamClientId(t *testing.T) {
	t.Run("uses the client cookie when the URL has no clientId", func(t *testing.T) {
		c := newMediastreamContext("browser-1", "?token=abc")
		assert.Equal(t, "browser-1", mediastreamClientId(c, loaded("browser-1")))
	})

	t.Run("prefers the URL's clientId, which is per tab, over the shared cookie", func(t *testing.T) {
		// tab A loaded a file, then tab B did, so the shared cookie now holds tab B
		c := newMediastreamContext("tab-b", "?clientId=tab-a")
		assert.Equal(t, "tab-a", mediastreamClientId(c, loaded("tab-a", "tab-b")))
	})

	t.Run("falls back to the cookie when the URL's clientId has nothing loaded", func(t *testing.T) {
		c := newMediastreamContext("browser-1", "?clientId=stale")
		assert.Equal(t, "browser-1", mediastreamClientId(c, loaded("browser-1")))
	})

	t.Run("falls back to default", func(t *testing.T) {
		c := newMediastreamContext("fresh-uuid", "")
		assert.Equal(t, "default", mediastreamClientId(c, loaded("default")))
	})

	t.Run("nothing loaded returns the first client ID present", func(t *testing.T) {
		assert.Equal(t, "tab-a", mediastreamClientId(newMediastreamContext("browser-1", "?clientId=tab-a"), loaded()))
		assert.Equal(t, "browser-1", mediastreamClientId(newMediastreamContext("browser-1", ""), loaded()))
		assert.Equal(t, "default", mediastreamClientId(newMediastreamContext("", ""), loaded()))
	})
}
