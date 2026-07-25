package handlers

import (
	"net/http"
	"seanime/internal/core"
	"seanime/internal/util"
	"strings"

	"github.com/labstack/echo/v4"
)

var publicPaths = []string{
	"/api/v1/status",
	"/api/v1/auth/admin-login",
	"/api/v1/auth/access-code",
	"/api/v1/auth/setup",
	"/api/v1/auth/setup-check",
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
