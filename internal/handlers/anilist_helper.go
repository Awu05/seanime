package handlers

import (
	"seanime/internal/core"
	"seanime/internal/database/models"
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

// getSettings returns the settings for the current profile.
// In multi-user mode, returns per-profile settings.
// In single-user mode, returns global settings (ID=1).
func (h *Handler) getSettings(c echo.Context) (*models.Settings, error) {
	if h.App.MultiUserEnabled {
		profileID := core.GetProfileIDFromContext(c)
		if profileID != "" {
			return h.App.Database.GetSettingsForProfile(profileID)
		}
	}
	return h.App.Database.GetSettings()
}

// getMediastreamSettings returns mediastream settings for the current profile.
func (h *Handler) getMediastreamSettings(c echo.Context) *models.MediastreamSettings {
	if h.App.MultiUserEnabled {
		profileID := core.GetProfileIDFromContext(c)
		if profileID != "" {
			s, _ := h.App.Database.GetMediastreamSettingsForProfile(profileID)
			return s
		}
	}
	s, _ := h.App.Database.GetMediastreamSettings()
	return s
}

// getTorrentstreamSettings returns torrentstream settings for the current profile.
func (h *Handler) getTorrentstreamSettings(c echo.Context) *models.TorrentstreamSettings {
	if h.App.MultiUserEnabled {
		profileID := core.GetProfileIDFromContext(c)
		if profileID != "" {
			s, _ := h.App.Database.GetTorrentstreamSettingsForProfile(profileID)
			return s
		}
	}
	s, _ := h.App.Database.GetTorrentstreamSettings()
	return s
}

// getDebridSettings returns debrid settings for the current profile.
func (h *Handler) getDebridSettings(c echo.Context) *models.DebridSettings {
	if h.App.MultiUserEnabled {
		profileID := core.GetProfileIDFromContext(c)
		if profileID != "" {
			s, _ := h.App.Database.GetDebridSettingsForProfile(profileID)
			return s
		}
	}
	s, _ := h.App.Database.GetDebridSettings()
	return s
}
