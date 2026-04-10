package handlers

import (
	"seanime/internal/core"
	"seanime/internal/util"

	"github.com/labstack/echo/v4"
)

func (h *Handler) getStreamSession(c echo.Context) *core.ProfileStreamSession {
	profileID := core.GetProfileIDFromContext(c)
	session, created := h.App.StreamSessionManager.GetOrCreateSession(profileID, h.App.CreateStreamSession)
	if created {
		// Seed the anime collection outside the lock, in the background, because
		// GetAnimeCollection may fall back to a network request on cache miss.
		go func() {
			defer util.HandlePanicThen(func() {})
			h.App.SeedSessionCollection(session)
		}()
	}
	return session
}
