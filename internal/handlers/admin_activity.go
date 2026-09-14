package handlers

import (
	"errors"
	"net/http"
	"seanime/internal/core"
	"time"

	"github.com/labstack/echo/v4"
)

type (
	// AdminActivityConnection is one live WebSocket connection, joined against the profile
	// table for display.
	AdminActivityConnection struct {
		ProfileID   string    `json:"profileId"`
		ProfileName string    `json:"profileName"`
		Platform    string    `json:"platform"`
		ConnectedAt time.Time `json:"connectedAt"`
	}

	// AdminActivityStream is one profile's currently-active torrent stream.
	AdminActivityStream struct {
		ProfileID          string  `json:"profileId"`
		ProfileName        string  `json:"profileName"`
		MediaID            int     `json:"mediaId"`
		EpisodeNumber      int     `json:"episodeNumber"`
		Title              string  `json:"title"`
		ProgressPercentage float64 `json:"progressPercentage"`
		DownloadSpeed      string  `json:"downloadSpeed"`
		UploadSpeed        string  `json:"uploadSpeed"`
		Size               string  `json:"size"`
		Seeders            int     `json:"seeders"`
	}

	AdminActivitySnapshot struct {
		Connections []AdminActivityConnection `json:"connections"`
		Streams     []AdminActivityStream     `json:"streams"`
	}
)

// resolveProfileName returns the profile's display name, falling back to the raw ID if the
// profile can't be found (e.g. deleted between the snapshot being built and this lookup).
func (h *Handler) resolveProfileName(profileID string) string {
	if h.App.Database == nil {
		return profileID
	}
	profile, err := h.App.Database.GetProfileByID(profileID)
	if err != nil || profile == nil {
		return profileID
	}
	return profile.Name
}

// HandleGetAdminActivity
//
//	@summary returns live connections and active torrent streams across all profiles (admin only).
//	@route /api/v1/admin/activity [GET]
//	@returns handlers.AdminActivitySnapshot
func (h *Handler) HandleGetAdminActivity(c echo.Context) error {
	if !core.GetIsAdminFromContext(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "Admin access required"})
	}

	snapshot := AdminActivitySnapshot{
		Connections: make([]AdminActivityConnection, 0),
		Streams:     make([]AdminActivityStream, 0),
	}

	// Memoizes resolveProfileName within this single snapshot build, since a profile with
	// several open tabs/streams would otherwise trigger the same DB lookup repeatedly.
	profileNames := make(map[string]string)
	resolveProfileName := func(profileID string) string {
		if name, ok := profileNames[profileID]; ok {
			return name
		}
		name := h.resolveProfileName(profileID)
		profileNames[profileID] = name
		return name
	}

	if h.App.WSEventManager != nil {
		for _, conn := range h.App.WSEventManager.GetConnections() {
			snapshot.Connections = append(snapshot.Connections, AdminActivityConnection{
				ProfileID:   conn.ProfileID,
				ProfileName: resolveProfileName(conn.ProfileID),
				Platform:    conn.Platform,
				ConnectedAt: conn.ConnectedAt,
			})
		}
	}

	// Only collect session pointers while the manager's lock is held - GetActiveStreamInfo
	// below takes the client's own mutex, which a stalled websocket write can pin for an
	// unbounded time (see client.go's monitor loop). Calling it here, instead of inside
	// WithSessionsLocked, keeps that possible stall from also blocking session creation and
	// settings refreshes server-wide (see WithSessionsLocked's doc comment).
	var sessions []*core.ProfileStreamSession
	if h.App.StreamSessionManager != nil {
		h.App.StreamSessionManager.WithSessionsLocked(func(s []*core.ProfileStreamSession) {
			sessions = append(sessions, s...)
		})
	}

	for _, session := range sessions {
		if session == nil || session.TorrentStream == nil {
			continue
		}
		info, ok := session.TorrentStream.GetActiveStreamInfo()
		if !ok {
			continue
		}

		title := info.Title
		if title == "" {
			title = info.TorrentName
		}
		snapshot.Streams = append(snapshot.Streams, AdminActivityStream{
			ProfileID:          session.ProfileID,
			ProfileName:        resolveProfileName(session.ProfileID),
			MediaID:            info.MediaID,
			EpisodeNumber:      info.EpisodeNumber,
			Title:              title,
			ProgressPercentage: info.Status.ProgressPercentage,
			DownloadSpeed:      info.Status.DownloadSpeed,
			UploadSpeed:        info.Status.UploadSpeed,
			Size:               info.Status.Size,
			Seeders:            info.Status.Seeders,
		})
	}

	return h.RespondWithData(c, snapshot)
}

// HandleTerminateProfileStream
//
//	@summary hard-stops a profile's active torrent stream (admin only).
//	@desc Idempotent: if the profile has no active stream, this still returns success, since
//	@desc the desired end state (profile has no active stream) already holds.
//	@route /api/v1/admin/activity/terminate [POST]
//	@returns bool
//
// Known limitation (pre-existing in StopStream, not introduced here): StopStream stops the
// app-level (not per-profile) mediaPlayerRepository, so on a deployment where multiple profiles
// concurrently use an *external* media player (mpv/iina — the native browser player doesn't go
// through this path), terminating one profile's stream could stop a different profile's external
// player window. This is a property of the shared mediaPlayerRepository and out of scope here.
func (h *Handler) HandleTerminateProfileStream(c echo.Context) error {
	if !core.GetIsAdminFromContext(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "Admin access required"})
	}

	type body struct {
		ProfileID string `json:"profileId"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}
	if b.ProfileID == "" {
		return h.RespondWithError(c, errors.New("missing profileId"))
	}

	if h.App.StreamSessionManager == nil {
		return h.RespondWithData(c, true)
	}

	session, found := h.App.StreamSessionManager.PeekSession(b.ProfileID)
	if !found || session.TorrentStream == nil {
		return h.RespondWithData(c, true)
	}

	// StopStream, not DropTorrent: DropTorrent only drops torrents no session still claims, and
	// this session's own activeStreams/currentTorrent claim is still held at this point (nothing
	// released it), so the target torrent would never actually be dropped, currentTorrent would
	// never clear (the row would linger in the admin table), and playback would never stop.
	// StopStream releases this session's claim first, then drops the torrent, clears
	// currentTorrent, and stops the native player.
	if err := session.TorrentStream.StopStream(); err != nil {
		return h.RespondWithError(c, err)
	}

	return h.RespondWithData(c, true)
}
