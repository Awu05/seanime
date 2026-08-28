package handlers

import (
	"seanime/internal/continuity"
	"seanime/internal/core"
	"seanime/internal/database/models"
	"strconv"

	"github.com/labstack/echo/v4"
)

// continuityEnabledForRequest reports whether watch-continuity is enabled for the requester.
//
// In multi-user mode each non-admin profile has its own fully independent settings row (see
// Database.GetSettingsForProfile / UpsertSettingsForProfile), but InitOrRefreshModules ignores
// the profileID it's given and always reloads the global/admin settings row - so
// ContinuityManager's cached flag never reflects a non-admin profile's own "Enable Watch
// Continuity" toggle. Read that profile's row directly here instead of trusting the cached flag.
func (h *Handler) continuityEnabledForRequest(c echo.Context) bool {
	profileID := core.GetProfileIDFromContext(c)

	var profileSettings *models.Settings
	if h.App.MultiUserEnabled && profileID != "" {
		profileSettings, _ = h.App.Database.GetSettingsForProfile(profileID)
	}

	return continuityEnabled(h.App.MultiUserEnabled, profileID, profileSettings, h.App.ContinuityManager.GetSettings().WatchContinuityEnabled)
}

func continuityEnabled(multiUserEnabled bool, profileID string, profileSettings *models.Settings, globalFlagEnabled bool) bool {
	if !multiUserEnabled || profileID == "" {
		return globalFlagEnabled
	}
	if profileSettings == nil || profileSettings.Library == nil {
		return false
	}
	return profileSettings.Library.EnableWatchContinuity
}

// HandleUpdateContinuityWatchHistoryItem
//
//	@summary Updates watch history item.
//	@desc This endpoint is used to update a watch history item.
//	@desc Since this is low priority, we ignore any errors.
//	@route /api/v1/continuity/item [PATCH]
//	@returns bool
func (h *Handler) HandleUpdateContinuityWatchHistoryItem(c echo.Context) error {
	type body struct {
		Options continuity.UpdateWatchHistoryItemOptions `json:"options"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	if !h.continuityEnabledForRequest(c) {
		return h.RespondWithData(c, true)
	}

	err := h.App.ContinuityManager.UpdateWatchHistoryItem(&b.Options)
	if err != nil {
		// Ignore the error
		return h.RespondWithError(c, err)
	}

	return h.RespondWithData(c, true)
}

// HandleGetContinuityWatchHistoryItem
//
//	@summary Returns a watch history item.
//	@desc This endpoint is used to retrieve a watch history item.
//	@route /api/v1/continuity/item/{id} [GET]
//	@param id - int - true - "AniList anime media ID"
//	@returns continuity.WatchHistoryItemResponse
func (h *Handler) HandleGetContinuityWatchHistoryItem(c echo.Context) error {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return h.RespondWithError(c, err)
	}

	if !h.continuityEnabledForRequest(c) {
		return h.RespondWithData(c, &continuity.WatchHistoryItemResponse{
			Item:  nil,
			Found: false,
		})
	}

	resp := h.App.ContinuityManager.GetWatchHistoryItem(id)
	return h.RespondWithData(c, resp)
}

// HandleGetContinuityWatchHistory
//
//	@summary Returns the continuity watch history
//	@desc This endpoint is used to retrieve all watch history items.
//	@route /api/v1/continuity/history [GET]
//	@returns continuity.WatchHistory
func (h *Handler) HandleGetContinuityWatchHistory(c echo.Context) error {
	if !h.continuityEnabledForRequest(c) {
		ret := make(map[int]*continuity.WatchHistoryItem)
		return h.RespondWithData(c, ret)
	}

	resp := h.App.ContinuityManager.GetWatchHistory()
	return h.RespondWithData(c, resp)
}
