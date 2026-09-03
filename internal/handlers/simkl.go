package handlers

import (
	"encoding/json"
	"fmt"
	"seanime/internal/api/anilist"
	"seanime/internal/api/simkl"
	"seanime/internal/constants"
	"seanime/internal/core"
	"seanime/internal/database/models"
	"time"

	"github.com/labstack/echo/v4"
)

// shouldEnableSimklSync reports whether SIMKL mirroring should actually be active: it
// requires both an active connection (access token present) and the user's enabled toggle.
func shouldEnableSimklSync(connected bool, enabledToggle bool) bool {
	return connected && enabledToggle
}

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
	pin, err := simkl.RequestPin(c.Request().Context(), simkl.DefaultHTTPClient, constants.SimklClientId)
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

	token, done, err := simkl.PollPin(c.Request().Context(), simkl.DefaultHTTPClient, constants.SimklClientId, b.UserCode)
	if err != nil {
		return h.RespondWithError(c, err)
	}
	if !done {
		return h.RespondWithData(c, false)
	}

	client := simkl.NewAPIClient(simkl.DefaultHTTPClient, token)
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
	Enabled   bool `json:"enabled"`
	Connected bool `json:"connected"`
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
	})
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

	profileID := simklProfileID(c)
	settings, err := h.App.Database.UpsertSimklSettings(&models.SimklSettings{ProfileID: profileID, Enabled: b.Enabled})
	if err != nil {
		return h.RespondWithError(c, err)
	}
	return h.RespondWithData(c, settings)
}

// simklEnqueuedEntryPayload mirrors the JSON shape of internal/sync's unexported
// updateEntryPayload. The worker only json.Unmarshals by key, so the tags — not the Go type —
// are what must stay in lockstep with internal/sync/mirror_platform.go.
type simklEnqueuedEntryPayload struct {
	MediaID  int                      `json:"mediaId"`
	Status   *anilist.MediaListStatus `json:"status,omitempty"`
	ScoreRaw *int                     `json:"scoreRaw,omitempty"`
}

// simklEnqueuedProgressPayload mirrors internal/sync's updateProgressPayload (totalEpisodes
// is omitempty there, and unknown for a seed, so it is left out).
type simklEnqueuedProgressPayload struct {
	MediaID  int `json:"mediaId"`
	Progress int `json:"progress"`
}

// HandleSimklSyncNow
//
//	@summary seeds SIMKL with the user's entire current AniList collection.
//	@desc Without this, SIMKL only mirrors changes made after connecting - existing entries never reach it otherwise.
//	@desc Entries are enqueued onto the durable pending-sync queue rather than pushed inline, so a large
//	@desc library neither blocks this request for minutes nor loses failed entries permanently.
//	@route /api/v1/simkl/sync-now [POST]
//	@returns bool
func (h *Handler) HandleSimklSyncNow(c echo.Context) error {
	profileID := simklProfileID(c)

	// Fail loudly if there's no account: the worker would otherwise drain these rows straight
	// into "not connected" retries.
	if _, err := h.App.Database.GetSimklAccount(profileID); err != nil {
		return h.RespondWithError(c, err)
	}

	collection, err := h.getAnilistPlatform(c).GetAnimeCollection(c.Request().Context(), false)
	if err != nil {
		return h.RespondWithError(c, err)
	}
	if collection == nil || collection.MediaListCollection == nil {
		return h.RespondWithData(c, true)
	}

	for _, list := range collection.MediaListCollection.Lists {
		for _, entry := range list.Entries {
			if entry.Status == nil || entry.GetMedia() == nil {
				continue
			}
			mediaID := entry.GetMedia().GetID()

			entryPayload := simklEnqueuedEntryPayload{MediaID: mediaID, Status: entry.Status}
			if entry.Score != nil && *entry.Score > 0 {
				scoreRaw := int(*entry.Score)
				entryPayload.ScoreRaw = &scoreRaw
			}
			h.enqueueSimklSeedRow(profileID, "update_entry", mediaID, entryPayload)

			if entry.Progress != nil && *entry.Progress > 0 {
				h.enqueueSimklSeedRow(profileID, "update_progress", mediaID,
					simklEnqueuedProgressPayload{MediaID: mediaID, Progress: *entry.Progress})
			}
		}
	}

	return h.RespondWithData(c, true)
}

// enqueueSimklSeedRow persists one pending-sync row for the seed. A failure here is logged
// per-entry rather than failing the whole request, matching how the inline version reported
// per-entry problems — one bad row shouldn't abandon the rest of the collection.
func (h *Handler) enqueueSimklSeedRow(profileID string, operation string, mediaID int, payload interface{}) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		h.App.Logger.Err(err).Int("mediaID", mediaID).Str("operation", operation).
			Msg("simkl: failed to encode sync-now payload")
		return
	}
	err = h.App.Database.EnqueuePendingSync(&models.PendingSync{
		ProfileID:     profileID,
		Target:        "simkl",
		Operation:     operation,
		Payload:       encoded,
		NextAttemptAt: time.Now(),
	})
	if err != nil {
		h.App.Logger.Err(err).Int("mediaID", mediaID).Str("operation", operation).
			Msg("simkl: failed to enqueue entry during sync-now")
	}
}
