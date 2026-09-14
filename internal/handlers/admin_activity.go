package handlers

import (
	"errors"
	"net/http"
	"seanime/internal/core"
	"seanime/internal/torrentstream"
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

	if h.App.WSEventManager != nil {
		for _, conn := range h.App.WSEventManager.GetConnections() {
			snapshot.Connections = append(snapshot.Connections, AdminActivityConnection{
				ProfileID:   conn.ProfileID,
				ProfileName: h.resolveProfileName(conn.ProfileID),
				Platform:    conn.Platform,
				ConnectedAt: conn.ConnectedAt,
			})
		}
	}

	type streamEntry struct {
		profileID string
		info      torrentstream.ActiveStreamInfo
	}

	var entries []streamEntry
	if h.App.StreamSessionManager != nil {
		h.App.StreamSessionManager.WithSessionsLocked(func(sessions []*core.ProfileStreamSession) {
			for _, session := range sessions {
				if session == nil || session.TorrentStream == nil {
					continue
				}
				info, ok := session.TorrentStream.GetActiveStreamInfo()
				if !ok {
					continue
				}
				entries = append(entries, streamEntry{profileID: session.ProfileID, info: info})
			}
		})
	}

	for _, entry := range entries {
		title := entry.info.Title
		if title == "" {
			title = entry.info.TorrentName
		}
		snapshot.Streams = append(snapshot.Streams, AdminActivityStream{
			ProfileID:          entry.profileID,
			ProfileName:        h.resolveProfileName(entry.profileID),
			MediaID:            entry.info.MediaID,
			EpisodeNumber:      entry.info.EpisodeNumber,
			Title:              title,
			ProgressPercentage: entry.info.Status.ProgressPercentage,
			DownloadSpeed:      entry.info.Status.DownloadSpeed,
			UploadSpeed:        entry.info.Status.UploadSpeed,
			Size:               entry.info.Status.Size,
			Seeders:            entry.info.Status.Seeders,
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

	if err := session.TorrentStream.DropTorrent(); err != nil {
		return h.RespondWithError(c, err)
	}

	return h.RespondWithData(c, true)
}
