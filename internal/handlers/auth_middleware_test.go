package handlers

import (
	"net/http"
	"net/http/httptest"
	"seanime/internal/core"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenewAuthCookieIfNeeded guards the fix for a reported bug: the seanime-auth session JWT
// was minted once at login with a hard 24h expiry and never renewed, so an actively-used session
// (streaming, polling, websocket traffic) would hard-expire exactly 24h after login regardless of
// activity. renewAuthCookieIfNeeded should reissue the cookie with a fresh authTokenLifetime once
// less than authTokenRenewalThreshold remains, so the session stays alive as long as the browser
// keeps making requests - only a truly idle session should ever need to re-login.
func TestRenewAuthCookieIfNeeded(t *testing.T) {
	const secret = "test-jwt-secret"
	e := echo.New()
	h := &Handler{App: &core.App{JWTSecret: secret}}

	t.Run("renews when less than the threshold remains", func(t *testing.T) {
		claims := &core.AuthClaims{
			ProfileID: "profile-1",
			IsAdmin:   false,
			Scope:     "profile",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)), // well under the threshold
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		h.renewAuthCookieIfNeeded(c, claims)

		cookies := rec.Result().Cookies()
		require.Len(t, cookies, 1, "expected the session cookie to be reissued")
		assert.Equal(t, "seanime-auth", cookies[0].Name)
		assert.NotEmpty(t, cookies[0].Value)
		assert.NotEqual(t, "", cookies[0].Value)

		renewedClaims, err := core.ParseToken(secret, cookies[0].Value)
		require.NoError(t, err)
		assert.Equal(t, "profile-1", renewedClaims.ProfileID)
		assert.Equal(t, "profile", renewedClaims.Scope)
		assert.False(t, renewedClaims.IsAdmin)
		assert.WithinDuration(t, time.Now().Add(authTokenLifetime), renewedClaims.ExpiresAt.Time, time.Minute,
			"the reissued token should carry a fresh full-length expiry, not extend the old one")
	})

	t.Run("preserves admin scope on renewal", func(t *testing.T) {
		claims := &core.AuthClaims{
			ProfileID: "admin-profile",
			IsAdmin:   true,
			Scope:     "admin",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		h.renewAuthCookieIfNeeded(c, claims)

		cookies := rec.Result().Cookies()
		require.Len(t, cookies, 1)
		renewedClaims, err := core.ParseToken(secret, cookies[0].Value)
		require.NoError(t, err)
		assert.True(t, renewedClaims.IsAdmin)
		assert.Equal(t, "admin", renewedClaims.Scope)
	})

	t.Run("does not renew when plenty of time remains", func(t *testing.T) {
		claims := &core.AuthClaims{
			ProfileID: "profile-1",
			Scope:     "profile",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(23 * time.Hour)), // well over the threshold
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		h.renewAuthCookieIfNeeded(c, claims)

		assert.Empty(t, rec.Result().Cookies(), "should not reissue a cookie when the current token still has plenty of life left")
	})

	t.Run("does not renew right at the threshold boundary", func(t *testing.T) {
		claims := &core.AuthClaims{
			ProfileID: "profile-1",
			Scope:     "profile",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(authTokenRenewalThreshold + time.Minute)),
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		h.renewAuthCookieIfNeeded(c, claims)

		assert.Empty(t, rec.Result().Cookies())
	})
}
