package handlers

import (
	"context"
	"fmt"
	"net/http"
	"seanime/internal/api/anilist"
	"seanime/internal/api/simkl"
	"seanime/internal/constants"
	"seanime/internal/core"
	"seanime/internal/database/models"
	syncpkg "seanime/internal/sync"

	"github.com/labstack/echo/v4"
)

// shouldEnableSimklSync reports whether SIMKL mirroring should actually be active: it
// requires both an active connection (access token present) and the user's enabled toggle.
func shouldEnableSimklSync(connected bool, enabledToggle bool) bool {
	return connected && enabledToggle
}

// HandleSimklConnectStart
//
//	@summary starts the SIMKL PIN authorization flow.
//	@route /api/v1/simkl/connect/start [POST]
//	@returns simkl.PinResponse
func (h *Handler) HandleSimklConnectStart(c echo.Context) error {
	pin, err := simkl.RequestPin(c.Request().Context(), http.DefaultClient, constants.SimklClientId)
	if err != nil {
		return h.RespondWithError(c, err)
	}
	return h.RespondWithData(c, pin)
}

// HandleSimklConnectPoll
//
//	@summary polls whether the user has approved the SIMKL PIN yet.
//	@route /api/v1/simkl/connect/poll [POST]
//	@returns bool
func (h *Handler) HandleSimklConnectPoll(c echo.Context) error {
	type body struct {
		UserCode string `json:"userCode"`
	}
	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	profileID := core.GetProfileIDFromContext(c)

	token, done, err := simkl.PollPin(c.Request().Context(), http.DefaultClient, constants.SimklClientId, b.UserCode)
	if err != nil {
		return h.RespondWithError(c, err)
	}
	if !done {
		return h.RespondWithData(c, false)
	}

	client := simkl.NewAPIClient(http.DefaultClient, token)
	if err := client.TestConnection(c.Request().Context()); err != nil {
		return h.RespondWithError(c, fmt.Errorf("connected but could not verify the SIMKL account: %w", err))
	}

	_, err = h.App.Database.UpsertSimklAccount(&models.SimklAccount{ProfileID: profileID, AccessToken: token})
	if err != nil {
		return h.RespondWithError(c, err)
	}

	return h.RespondWithData(c, true)
}

// HandleSimklDisconnect
//
//	@summary disconnects the SIMKL account.
//	@route /api/v1/simkl/disconnect [POST]
//	@returns bool
func (h *Handler) HandleSimklDisconnect(c echo.Context) error {
	profileID := core.GetProfileIDFromContext(c)
	if err := h.App.Database.DeleteSimklAccount(profileID); err != nil {
		return h.RespondWithError(c, err)
	}
	return h.RespondWithData(c, true)
}

// HandleGetSimklSettings
//
//	@summary returns the SIMKL sync settings for the current profile.
//	@route /api/v1/simkl/settings [GET]
//	@returns models.SimklSettings
func (h *Handler) HandleGetSimklSettings(c echo.Context) error {
	profileID := core.GetProfileIDFromContext(c)
	settings, err := h.App.Database.GetSimklSettings(profileID)
	if err != nil {
		return h.RespondWithError(c, err)
	}
	return h.RespondWithData(c, settings)
}

// HandleSaveSimklSettings
//
//	@summary updates the SIMKL sync settings for the current profile.
//	@route /api/v1/simkl/settings [PATCH]
//	@returns models.SimklSettings
func (h *Handler) HandleSaveSimklSettings(c echo.Context) error {
	type body struct {
		Enabled bool `json:"enabled"`
	}
	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	profileID := core.GetProfileIDFromContext(c)
	settings, err := h.App.Database.UpsertSimklSettings(&models.SimklSettings{ProfileID: profileID, Enabled: b.Enabled})
	if err != nil {
		return h.RespondWithError(c, err)
	}
	return h.RespondWithData(c, settings)
}

// HandleSimklSyncNow
//
//	@summary seeds SIMKL with the user's entire current AniList collection.
//	@desc Without this, SIMKL only mirrors changes made after connecting - existing entries never reach it otherwise.
//	@route /api/v1/simkl/sync-now [POST]
//	@returns bool
func (h *Handler) HandleSimklSyncNow(c echo.Context) error {
	profileID := core.GetProfileIDFromContext(c)

	account, err := h.App.Database.GetSimklAccount(profileID)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	collection, err := h.getAnilistPlatform(c).GetAnimeCollection(c.Request().Context(), false)
	if err != nil {
		return h.RespondWithError(c, err)
	}
	if collection == nil || collection.MediaListCollection == nil {
		return h.RespondWithData(c, true)
	}

	client := simkl.NewAPIClient(http.DefaultClient, account.AccessToken)
	ctx := context.Background()
	for _, list := range collection.MediaListCollection.Lists {
		for _, entry := range list.Entries {
			if entry.Status == nil || entry.GetMedia() == nil {
				continue
			}
			mediaID := entry.GetMedia().GetID()
			if err := client.AddToList(ctx, mediaID, simklStatusForEntry(entry)); err != nil {
				h.App.Logger.Err(err).Int("mediaID", mediaID).Msg("simkl: failed to add entry to list during sync-now")
			}
			if entry.Progress != nil && *entry.Progress > 0 {
				if err := client.MarkProgress(ctx, mediaID, *entry.Progress); err != nil {
					h.App.Logger.Err(err).Int("mediaID", mediaID).Msg("simkl: failed to mark progress during sync-now")
				}
			}
			if entry.Score != nil && *entry.Score > 0 {
				rating, shouldRemove := syncpkg.MapAnilistScoreToSimklRating(int(*entry.Score))
				if !shouldRemove {
					if err := client.SetRating(ctx, mediaID, rating); err != nil {
						h.App.Logger.Err(err).Int("mediaID", mediaID).Int("rating", rating).Msg("simkl: failed to set rating during sync-now")
					}
				}
			}
		}
	}

	return h.RespondWithData(c, true)
}

func simklStatusForEntry(entry *anilist.AnimeCollection_MediaListCollection_Lists_Entries) string {
	if entry.Status == nil {
		return "plantowatch"
	}
	return syncpkg.MapAnilistStatusToSimkl(*entry.Status)
}
