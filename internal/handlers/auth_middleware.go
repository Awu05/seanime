package handlers

import (
	"net/http"
	"seanime/internal/core"
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
		if h.App.IsDesktopSidecar && !h.App.MultiUserEnabled {
			c.Set("profileId", "")
			c.Set("isAdmin", true)
			c.Set("authScope", "admin")
			return next(c)
		}

		path := c.Request().URL.Path
		for _, p := range publicPaths {
			if path == p || strings.HasPrefix(path, p) {
				return next(c)
			}
		}

		if path == "/events" || strings.HasPrefix(path, "/_next") || strings.HasPrefix(path, "/icons") {
			return next(c)
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

		if path == "/api/v1/auth/select-profile" || path == "/api/v1/auth/profiles" {
			if claims.Scope == "access" || claims.Scope == "admin" || claims.Scope == "profile" {
				c.Set("profileId", claims.ProfileID)
				c.Set("isAdmin", claims.IsAdmin)
				c.Set("authScope", claims.Scope)
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

		return next(c)
	}
}
