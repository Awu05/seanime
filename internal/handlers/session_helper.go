package handlers

import (
	"seanime/internal/core"

	"github.com/labstack/echo/v4"
)

func (h *Handler) getStreamSession(c echo.Context) *core.ProfileStreamSession {
	profileID := core.GetProfileIDFromContext(c)
	return h.App.StreamSessionManager.GetOrCreateSession(profileID)
}
