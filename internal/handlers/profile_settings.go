package handlers

import (
	"seanime/internal/core"

	"github.com/labstack/echo/v4"
)

func (h *Handler) HandleGetProfileSettings(c echo.Context) error {
	profileID := core.GetProfileIDFromContext(c)
	if profileID == "" {
		return h.RespondWithData(c, map[string]interface{}{
			"overrides": "{}",
			"merged":    h.App.Settings,
		})
	}

	ps, err := h.App.Database.GetProfileSettings(profileID)
	overrides := "{}"
	if err == nil && ps != nil {
		overrides = ps.Overrides
	}

	merged := h.App.GetMergedSettingsForProfile(profileID)

	return h.RespondWithData(c, map[string]interface{}{
		"overrides": overrides,
		"merged":    merged,
	})
}

func (h *Handler) HandleSaveProfileSettings(c echo.Context) error {
	profileID := core.GetProfileIDFromContext(c)
	if profileID == "" {
		return h.RespondWithError(c, echo.ErrForbidden)
	}

	type body struct {
		Overrides string `json:"overrides"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	_, err := h.App.Database.UpsertProfileSettings(profileID, b.Overrides)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	return h.RespondWithData(c, map[string]interface{}{"success": true})
}

func (h *Handler) HandleGetMergedSettings(c echo.Context) error {
	profileID := core.GetProfileIDFromContext(c)
	merged := h.App.GetMergedSettingsForProfile(profileID)
	return h.RespondWithData(c, merged)
}
