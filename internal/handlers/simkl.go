package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"seanime/internal/api/simkl"
	"seanime/internal/core"
	"seanime/internal/database/models"
	"seanime/internal/platforms/platform"
	syncpkg "seanime/internal/sync"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// simklProfileID returns the profile ID SIMKL rows are keyed on for this request. It must be
// normalized: GetProfileIDFromContext returns "" in single-user/sidecar mode, while the core
// wiring (platform wrapper, sync worker resolvers) keys the default profile on "_default" —
// without this, rows written here would be invisible to the wiring meant to read them.
func simklProfileID(c echo.Context) string {
	return core.NormalizeSimklProfileID(core.GetProfileIDFromContext(c))
}

// HandleSimklConnectStart
//
//	@summary starts the SIMKL PIN authorization flow.
//	@route /api/v1/simkl/connect/start [POST]
//	@returns simkl.PinResponse
func (h *Handler) HandleSimklConnectStart(c echo.Context) error {
	profileID := simklProfileID(c)
	settings, err := h.App.Database.GetSimklSettings(profileID)
	if err != nil {
		return h.RespondWithError(c, err)
	}
	if settings.ClientId == "" {
		return h.RespondWithError(c, errors.New("set a SIMKL Client ID in SIMKL settings first"))
	}

	pin, err := simkl.RequestPin(c.Request().Context(), simkl.DefaultHTTPClient, settings.ClientId)
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

	profileID := simklProfileID(c)
	settings, err := h.App.Database.GetSimklSettings(profileID)
	if err != nil {
		return h.RespondWithError(c, err)
	}
	if settings.ClientId == "" {
		return h.RespondWithError(c, errors.New("set a SIMKL Client ID in SIMKL settings first"))
	}

	token, done, err := simkl.PollPin(c.Request().Context(), simkl.DefaultHTTPClient, settings.ClientId, b.UserCode)
	if err != nil {
		return h.RespondWithError(c, err)
	}
	if !done {
		return h.RespondWithData(c, false)
	}

	client := simkl.NewAPIClient(simkl.DefaultHTTPClient, token, settings.ClientId)
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
	profileID := simklProfileID(c)
	if err := h.App.Database.DeleteSimklAccount(profileID); err != nil {
		return h.RespondWithError(c, err)
	}
	return h.RespondWithData(c, true)
}

// SimklSettingsResponse is what the settings endpoint returns. Connected is separate from
// Enabled so the UI can distinguish "no account linked yet" from "linked but mirroring off".
type SimklSettingsResponse struct {
	Enabled   bool   `json:"enabled"`
	Connected bool   `json:"connected"`
	ClientId  string `json:"clientId"`
}

// HandleGetSimklSettings
//
//	@summary returns the SIMKL sync settings and connection state for the current profile.
//	@route /api/v1/simkl/settings [GET]
//	@returns handlers.SimklSettingsResponse
func (h *Handler) HandleGetSimklSettings(c echo.Context) error {
	profileID := simklProfileID(c)
	settings, err := h.App.Database.GetSimklSettings(profileID)
	if err != nil {
		return h.RespondWithError(c, err)
	}
	_, accountErr := h.App.Database.GetSimklAccount(profileID)
	return h.RespondWithData(c, SimklSettingsResponse{
		Enabled:   settings.Enabled,
		Connected: accountErr == nil,
		ClientId:  settings.ClientId,
	})
}

// HandleSaveSimklSettings
//
//	@summary updates the SIMKL sync settings for the current profile.
//	@route /api/v1/simkl/settings [PATCH]
//	@returns models.SimklSettings
func (h *Handler) HandleSaveSimklSettings(c echo.Context) error {
	// Enabled/ClientId are pointers so the enable toggle and the Client ID field can each be
	// saved independently (two separate UI actions) without one PATCH's omitted field wiping
	// out what the other already saved - a plain bool/string would default to false/"" when
	// left out of the request body.
	type body struct {
		Enabled  *bool   `json:"enabled"`
		ClientId *string `json:"clientId"`
	}
	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	profileID := simklProfileID(c)
	current, err := h.App.Database.GetSimklSettings(profileID)
	if err != nil {
		return h.RespondWithError(c, err)
	}
	if b.Enabled != nil {
		current.Enabled = *b.Enabled
	}
	if b.ClientId != nil {
		current.ClientId = strings.TrimSpace(*b.ClientId)
	}
	current.ProfileID = profileID

	settings, err := h.App.Database.UpsertSimklSettings(current)
	if err != nil {
		return h.RespondWithError(c, err)
	}
	return h.RespondWithData(c, settings)
}

// HandleSimklSyncNow
//
//	@summary seeds SIMKL with the user's entire current AniList collection.
//	@desc Without this, SIMKL only mirrors changes made after connecting - existing entries never reach it otherwise.
//	@desc Seeding runs in the background after this request returns, and rows are batch-inserted rather than
//	@desc one at a time, so a large library neither blocks this request for minutes nor loses failed entries permanently.
//	@route /api/v1/simkl/sync-now [POST]
//	@returns bool
func (h *Handler) HandleSimklSyncNow(c echo.Context) error {
	profileID := simklProfileID(c)

	// Fail loudly if there's no account: the worker would otherwise drain these rows straight
	// into "not connected" retries.
	if _, err := h.App.Database.GetSimklAccount(profileID); err != nil {
		return h.RespondWithError(c, err)
	}

	plat := h.getAnilistPlatform(c)
	go h.seedSimklPendingSyncs(plat, profileID)

	return h.RespondWithData(c, true)
}

// SimklSyncStatusResponse reports how many SIMKL sync rows are still queued for delivery.
type SimklSyncStatusResponse struct {
	Pending int64 `json:"pending"`
}

// HandleGetSimklSyncStatus
//
//	@summary returns how many SIMKL sync rows are still queued for delivery for the current profile.
//	@desc The UI polls this after Sync Now to show a "syncing..." indicator until it reaches zero -
//	@desc seeding and delivery both happen in the background (delivery on the retry worker's own
//	@desc tick), so there is nothing else that reports completion.
//	@route /api/v1/simkl/sync-status [GET]
//	@returns handlers.SimklSyncStatusResponse
func (h *Handler) HandleGetSimklSyncStatus(c echo.Context) error {
	profileID := simklProfileID(c)
	pending, err := h.App.Database.CountPendingSyncs(profileID, syncpkg.TargetSimkl)
	if err != nil {
		return h.RespondWithError(c, err)
	}
	return h.RespondWithData(c, SimklSyncStatusResponse{Pending: pending})
}

// seedSimklPendingSyncs runs in the background so HandleSimklSyncNow can return immediately -
// a multi-thousand-entry library would otherwise hold the HTTP request open for the entire
// seed. The AniList platform must be resolved from the request (via getAnilistPlatform) before
// the handler returns, since echo.Context is invalid once that happens; everything after that
// uses a background context.
func (h *Handler) seedSimklPendingSyncs(plat platform.Platform, profileID string) {
	collection, err := plat.GetAnimeCollection(context.Background(), false)
	if err != nil {
		h.App.Logger.Err(err).Str("profileID", profileID).Msg("simkl: sync-now failed to fetch the AniList collection")
		return
	}
	if collection == nil || collection.MediaListCollection == nil {
		return
	}

	var rows []*models.PendingSync
	for _, list := range collection.MediaListCollection.Lists {
		for _, entry := range list.Entries {
			if entry.Status == nil || entry.GetMedia() == nil {
				continue
			}
			mediaID := entry.GetMedia().GetID()

			entryPayload := syncpkg.UpdateEntryPayload{MediaID: mediaID, Status: entry.Status}
			if entry.Score != nil && *entry.Score > 0 {
				scoreRaw := int(*entry.Score)
				entryPayload.ScoreRaw = &scoreRaw
			}
			if row := h.buildSimklSeedRow(profileID, syncpkg.OpUpdateEntry, mediaID, entryPayload); row != nil {
				rows = append(rows, row)
			}

			if entry.Progress != nil && *entry.Progress > 0 {
				progressPayload := syncpkg.UpdateProgressPayload{MediaID: mediaID, Progress: *entry.Progress}
				if row := h.buildSimklSeedRow(profileID, syncpkg.OpUpdateProgress, mediaID, progressPayload); row != nil {
					rows = append(rows, row)
				}
			}
		}
	}

	if err := h.App.Database.EnqueuePendingSyncBatch(rows); err != nil {
		h.App.Logger.Err(err).Str("profileID", profileID).Int("rows", len(rows)).Msg("simkl: sync-now failed to enqueue seeded rows")
	}
}

// buildSimklSeedRow encodes one seed entry as a *models.PendingSync, or nil (logged) if the
// payload can't be encoded. A single bad entry shouldn't abandon the rest of the collection.
func (h *Handler) buildSimklSeedRow(profileID string, operation string, mediaID int, payload interface{}) *models.PendingSync {
	encoded, err := json.Marshal(payload)
	if err != nil {
		h.App.Logger.Err(err).Int("mediaID", mediaID).Str("operation", operation).
			Msg("simkl: failed to encode sync-now payload")
		return nil
	}
	return &models.PendingSync{
		ProfileID:     profileID,
		Target:        syncpkg.TargetSimkl,
		Operation:     operation,
		Payload:       encoded,
		NextAttemptAt: time.Now(),
	}
}
