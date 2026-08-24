package handlers

import (
	"net/http"
	"seanime/internal/core"
	"seanime/internal/util"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

var publicPaths = []string{
	"/api/v1/status",
	"/api/v1/auth/admin-login",
	"/api/v1/auth/access-code",
	"/api/v1/auth/setup",
	"/api/v1/auth/setup-check",
}

// authTokenLifetime is the duration a freshly (re)issued seanime-auth session token is valid for.
var authTokenLifetime = 24 * time.Hour

// authTokenRenewalThreshold: renewAuthCookieIfNeeded reissues the session once less than this
// much of the token's lifetime remains, so a session in active use (streaming, polling,
// websocket traffic) never hits a hard expiry mid-use - only a session with zero requests for a
// full authTokenLifetime ever needs to re-login.
var authTokenRenewalThreshold = 12 * time.Hour

// renewAuthCookieIfNeeded reissues the seanime-auth cookie with a fresh authTokenLifetime if the
// current token is within authTokenRenewalThreshold of expiring. Called after claims have already
// been validated (and, where applicable, the profile confirmed to still exist) by the caller.
func (h *Handler) renewAuthCookieIfNeeded(c echo.Context, claims *core.AuthClaims) {
	if claims.ExpiresAt == nil || time.Until(claims.ExpiresAt.Time) >= authTokenRenewalThreshold {
		return
	}

	newToken, err := core.GenerateToken(h.App.JWTSecret, claims.ProfileID, claims.IsAdmin, claims.Scope, authTokenLifetime)
	if err != nil {
		return
	}

	c.SetCookie(&http.Cookie{
		Name:     "seanime-auth",
		Value:    newToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(authTokenLifetime.Seconds()),
	})
}

func (h *Handler) MultiUserAuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// A Nakama watch-party peer is a different person's Seanime instance, not a profile on
		// this one - it authenticates with its own host-password token (checked further down the
		// chain in OptionalAuthMiddleware), never a profile JWT. Exempt these paths from the JWT
		// requirement so peer connections aren't rejected before that token is ever checked.
		if isNakamaPeerPath(c.Request().URL.Path) && isAuthenticatedNakamaPeer(h.App, c.Request()) {
			c.Set("profileId", "")
			c.Set("isAdmin", false)
			c.Set("authScope", "nakama-peer")
			return next(c)
		}

		// In Electron sidecar mode without multi-user, still allow public paths
		// (setup-check, setup) so first-time registration works.
		// Only auto-grant admin for non-public paths after admin exists.
		if h.App.IsDesktopSidecar && !h.App.MultiUserEnabled {
			path := c.Request().URL.Path
			isPublic := false
			for _, p := range publicPaths {
				if path == p {
					isPublic = true
					break
				}
			}
			if isPublic {
				h.tryExtractProfile(c)
				return next(c)
			}
			c.Set("profileId", "")
			c.Set("isAdmin", true)
			c.Set("authScope", "admin")
			c.SetRequest(c.Request().WithContext(util.ContextWithProfileID(c.Request().Context(), "")))
			return next(c)
		}

		path := c.Request().URL.Path
		for _, p := range publicPaths {
			if path == p {
				// For public paths, still try to extract profile from JWT if present
				// This allows status endpoint to return per-profile settings
				if authenticated := h.tryExtractProfile(c); !authenticated && h.App.MultiUserEnabled {
					// No valid profile/admin token: strip secrets from responses (e.g. /status)
					c.Set("unauthenticated", true)
				}
				return next(c)
			}
		}

		var tokenString string
		cookie, err := c.Cookie("seanime-auth")
		if err == nil && cookie.Value != "" {
			tokenString = cookie.Value
		} else {
			auth := c.Request().Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				tokenString = strings.TrimPrefix(auth, "Bearer ")
			}
		}

		if tokenString == "" {
			tokenString = c.QueryParam("auth_token")
		}

		if tokenString == "" {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "UNAUTHENTICATED"})
		}

		claims, err := core.ParseToken(h.App.JWTSecret, tokenString)
		if err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "INVALID_TOKEN"})
		}

		// Reject tokens whose profile no longer exists — otherwise a deleted
		// profile's JWT keeps working until it expires.
		if claims.ProfileID != "" {
			if _, err := h.App.Database.GetProfileByID(claims.ProfileID); err != nil {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "PROFILE_NOT_FOUND"})
			}
		}

		// Keep an actively-used session alive instead of hard-expiring it exactly
		// authTokenLifetime after login regardless of activity (e.g. mid-stream).
		h.renewAuthCookieIfNeeded(c, claims)

		if path == "/api/v1/auth/select-profile" || path == "/api/v1/auth/profiles" || path == "/api/v1/auth/create-profile" {
			if claims.Scope == "access" || claims.Scope == "admin" || claims.Scope == "profile" {
				c.Set("profileId", claims.ProfileID)
				c.Set("isAdmin", claims.IsAdmin)
				c.Set("authScope", claims.Scope)
				c.SetRequest(c.Request().WithContext(util.ContextWithProfileID(c.Request().Context(), claims.ProfileID)))
				return next(c)
			}
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "INSUFFICIENT_SCOPE"})
		}

		if claims.Scope != "profile" && claims.Scope != "admin" {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "PROFILE_NOT_SELECTED"})
		}

		c.Set("profileId", claims.ProfileID)
		c.Set("isAdmin", claims.IsAdmin)
		c.Set("authScope", claims.Scope)
		c.SetRequest(c.Request().WithContext(util.ContextWithProfileID(c.Request().Context(), claims.ProfileID)))

		return next(c)
	}
}

// tryExtractProfile attempts to read the JWT from the request and set profile context.
// Used on public paths so endpoints like /status can return per-profile data when authenticated.
// Returns true only when a valid profile/admin-scoped token was found.
func (h *Handler) tryExtractProfile(c echo.Context) bool {
	var tokenString string
	cookie, err := c.Cookie("seanime-auth")
	if err == nil && cookie.Value != "" {
		tokenString = cookie.Value
	} else {
		auth := c.Request().Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			tokenString = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	if tokenString == "" {
		return false
	}
	claims, err := core.ParseToken(h.App.JWTSecret, tokenString)
	if err != nil {
		return false
	}
	if claims.Scope == "profile" || claims.Scope == "admin" {
		c.Set("profileId", claims.ProfileID)
		c.Set("isAdmin", claims.IsAdmin)
		c.Set("authScope", claims.Scope)
		c.SetRequest(c.Request().WithContext(util.ContextWithProfileID(c.Request().Context(), claims.ProfileID)))
		return true
	}
	return false
}
