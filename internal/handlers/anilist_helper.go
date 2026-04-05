package handlers

import (
	"seanime/internal/core"
	"seanime/internal/platforms/platform"

	"github.com/labstack/echo/v4"
)

// getAnilistPlatform returns the AniList platform for the current profile.
// In multi-user mode, returns a per-profile platform from the pool.
// In single-user mode, returns the global platform.
func (h *Handler) getAnilistPlatform(c echo.Context) platform.Platform {
	if !h.App.MultiUserEnabled || h.App.AnilistPool == nil {
		return h.App.AnilistPlatformRef.Get()
	}
	profileID := core.GetProfileIDFromContext(c)
	return h.App.AnilistPool.GetPlatformForProfile(profileID)
}
