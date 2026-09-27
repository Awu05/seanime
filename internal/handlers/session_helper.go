package handlers

import (
	"seanime/internal/core"
	"seanime/internal/nakama"
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
			h.App.SeedSessionCollection(profileID, session)
		}()
	}
	return session
}

// evictStreamSession drops the profile's stream session so the next request rebuilds it. A watch
// party playing through it is moved to rebind()'s playback instead of staying on shut-down players.
func (h *Handler) evictStreamSession(profileID string, rebind func() nakama.PlaybackContext) {
	session, ok := h.App.StreamSessionManager.PeekSession(profileID)
	if !ok {
		return
	}
	partyPlaysThrough := h.App.NakamaManager != nil && h.App.NakamaManager.IsPartyPlayingThrough(session.VideoCore)
	h.App.StreamSessionManager.EvictSession(profileID)
	if partyPlaysThrough {
		h.App.NakamaManager.BindPartyPlayback(rebind())
	}
}

// sessionPlayback returns the requesting profile's playback context, for Nakama playback that
// must be tracked as that profile's.
func (h *Handler) sessionPlayback(c echo.Context) nakama.PlaybackContext {
	session := h.getStreamSession(c)
	return nakama.PlaybackContext{
		PlaybackManager:         session.PlaybackManager,
		VideoCore:               session.VideoCore,
		NativePlayer:            session.NativePlayer,
		TorrentstreamRepository: session.TorrentStream,
		DirectstreamManager:     session.DirectStreamManager,
		StreamToken:             session.StreamToken,
	}
}
