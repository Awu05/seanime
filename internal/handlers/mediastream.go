package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"seanime/internal/core"
	"seanime/internal/database/models"
	"seanime/internal/mediastream"
	"seanime/internal/mediastream/transcoder"
	"seanime/internal/util"
	"strings"

	"github.com/labstack/echo/v4"
)

// HandleGetMediastreamSettings
//
//	@summary get mediastream settings.
//	@desc This returns the mediastream settings.
//	@returns models.MediastreamSettings
//	@route /api/v1/mediastream/settings [GET]
func (h *Handler) HandleGetMediastreamSettings(c echo.Context) error {
	mediastreamSettings := h.getMediastreamSettings(c)
	if mediastreamSettings == nil {
		return h.RespondWithError(c, errors.New("media streaming settings not found"))
	}

	return h.RespondWithData(c, mediastreamSettings)
}

// HandleSaveMediastreamSettings
//
//	@summary save mediastream settings.
//	@desc This saves the mediastream settings.
//	@returns models.MediastreamSettings
//	@route /api/v1/mediastream/settings [PATCH]
func (h *Handler) HandleSaveMediastreamSettings(c echo.Context) error {
	type body struct {
		Settings models.MediastreamSettings `json:"settings"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	// Invalid custom hwaccel settings would otherwise only show up as a silent CPU fallback.
	if b.Settings.TranscodeHwAccel == "custom" {
		if err := transcoder.ValidateCustomHwAccelSettings(b.Settings.TranscodeHwAccelCustomSettings); err != nil {
			return h.RespondWithError(c, err)
		}
	}

	prevSettings := h.getMediastreamSettings(c)
	if err := h.guardStrictMediastreamRootMutation(c, prevSettings, &b.Settings); err != nil {
		return err
	}
	if err := h.guardPrivilegedMediastreamSettingsMutation(c, prevSettings, &b.Settings); err != nil {
		return err
	}

	b.Settings.FfmpegPath = strings.TrimSpace(strings.Trim(b.Settings.FfmpegPath, "\""))
	b.Settings.FfprobePath = strings.TrimSpace(strings.Trim(b.Settings.FfprobePath, "\""))

	var settings *models.MediastreamSettings
	var err error
	profileID := core.GetProfileIDFromContext(c)
	if h.App.MultiUserEnabled.Load() && profileID != "" {
		settings, err = h.App.Database.UpsertMediastreamSettingsForProfile(profileID, &b.Settings)
	} else {
		b.Settings.BaseModel = models.BaseModel{ID: 1}
		settings, err = h.App.Database.UpsertMediastreamSettings(&b.Settings)
	}
	if err != nil {
		return h.RespondWithError(c, err)
	}

	h.App.InitOrRefreshMediastreamSettings()

	return h.RespondWithData(c, settings)
}

// HandleRequestMediastreamMediaContainer
//
//	@summary request media stream.
//	@desc This requests a media stream and returns the media container to start the playback.
//	@returns mediastream.MediaContainer
//	@route /api/v1/mediastream/request [POST]
func (h *Handler) HandleRequestMediastreamMediaContainer(c echo.Context) error {
	if err := h.guardMediaConsumption(c); err != nil {
		return err
	}

	type body struct {
		Path             string                 `json:"path"`             // The path of the file.
		StreamType       mediastream.StreamType `json:"streamType"`       // The type of stream to request.
		AudioStreamIndex int                    `json:"audioStreamIndex"` // The audio stream index to use. (unused)
		ClientId         string                 `json:"clientId"`         // The session id
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	b.ClientId = getRequestClientId(c, b.ClientId)

	b.Path = util.ResolvePhysicalPath(b.Path)

	if err := h.guardStrictFilesystemPath(c, b.Path); err != nil {
		return err
	}

	if err := h.guardPrivilegedMediastream(c, h.App.SecondarySettings.Mediastream); err != nil {
		return err
	}

	var mediaContainer *mediastream.MediaContainer
	var err error

	switch b.StreamType {
	case mediastream.StreamTypeDirect:
		mediaContainer, err = h.App.MediastreamRepository.RequestDirectPlay(b.Path, b.ClientId)
	case mediastream.StreamTypeTranscode:
		mediaContainer, err = h.App.MediastreamRepository.RequestTranscodeStream(b.Path, b.ClientId)
	default:
		err = fmt.Errorf("stream type %s not implemented", b.StreamType)
	}
	if err != nil {
		return h.RespondWithError(c, err)
	}

	return h.RespondWithData(c, mediaContainer)
}

// HandlePreloadMediastreamMediaContainer
//
//	@summary preloads media stream for playback.
//	@desc This preloads a media stream by extracting the media information and attachments.
//	@returns bool
//	@route /api/v1/mediastream/preload [POST]
func (h *Handler) HandlePreloadMediastreamMediaContainer(c echo.Context) error {
	if err := h.guardMediaConsumption(c); err != nil {
		return err
	}

	type body struct {
		Path             string                 `json:"path"`             // The path of the file.
		StreamType       mediastream.StreamType `json:"streamType"`       // The type of stream to request.
		AudioStreamIndex int                    `json:"audioStreamIndex"` // The audio stream index to use.
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	b.Path = util.ResolvePhysicalPath(b.Path)

	if err := h.guardStrictFilesystemPath(c, b.Path); err != nil {
		return err
	}

	if err := h.guardPrivilegedMediastream(c, h.App.SecondarySettings.Mediastream); err != nil {
		return err
	}

	var err error

	switch b.StreamType {
	case mediastream.StreamTypeTranscode:
		err = h.App.MediastreamRepository.RequestPreloadTranscodeStream(b.Path)
	case mediastream.StreamTypeDirect:
		err = h.App.MediastreamRepository.RequestPreloadDirectPlay(b.Path)
	default:
		err = fmt.Errorf("stream type %s not implemented", b.StreamType)
	}
	if err != nil {
		return h.RespondWithError(c, err)
	}

	return h.RespondWithData(c, true)
}

func (h *Handler) HandleMediastreamGetSubtitles(c echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Response().Header().Set("Pragma", "no-cache")
	c.Response().Header().Set("Expires", "0")
	return h.App.MediastreamRepository.ServeEchoExtractedSubtitles(c, h.mediastreamClientId(c))
}

func (h *Handler) HandleMediastreamGetAttachments(c echo.Context) error {
	// tell the client not to cache the response
	c.Response().Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Response().Header().Set("Pragma", "no-cache")
	c.Response().Header().Set("Expires", "0")
	return h.App.MediastreamRepository.ServeEchoExtractedAttachments(c, h.mediastreamClientId(c))
}

// mediastreamClientId returns the client whose loaded file a stream request is for.
func (h *Handler) mediastreamClientId(c echo.Context) string {
	return mediastreamClientId(c, h.App.MediastreamRepository.HasMediaContainer)
}

// mediastreamClientId picks the client ID a stream, subtitle or attachment request
// is served for. The file was loaded under the client ID of the request that asked
// for it (see getRequestClientId).
//   - The stream URL carries it as ?clientId=, which is per tab, so it is tried first.
//   - Requests whose URLs cannot carry it (subtitles, fonts, HLS segments resolved
//     from a playlist) still send the Seanime-Client-Id cookie, which is tried next.
//     Tabs share that cookie, so it only identifies the tab that loaded a file last.
//   - "default" is the legacy fallback.
//
// When no candidate has a file loaded, the first one present is returned.
func mediastreamClientId(c echo.Context, loaded func(clientId string) bool) string {
	candidates := []string{
		strings.TrimSpace(c.QueryParam("clientId")),
		getContextClientId(c),
		"default",
	}
	for _, id := range candidates {
		if id != "" && loaded(id) {
			return id
		}
	}
	for _, id := range candidates {
		if id != "" {
			return id
		}
	}
	return "default"
}

//
// Direct
//

func (h *Handler) HandleMediastreamDirectPlay(c echo.Context) error {
	return h.App.MediastreamRepository.ServeEchoDirectPlay(c, h.mediastreamClientId(c))
}

//
// Transcode
//

func (h *Handler) HandleMediastreamTranscode(c echo.Context) error {
	return h.App.MediastreamRepository.ServeEchoTranscodeStream(c, h.mediastreamClientId(c))
}

// HandleMediastreamShutdownTranscodeStream
//
//	@summary shuts down the transcode stream
//	@desc This requests the transcoder to shut down. It should be called when unmounting the player (playback is no longer needed).
//	@desc This will also send an events.MediastreamShutdownStream event.
//	@desc It will not return any error and is safe to call multiple times.
//	@returns bool
//	@route /api/v1/mediastream/shutdown-transcode [POST]
func (h *Handler) HandleMediastreamShutdownTranscodeStream(c echo.Context) error {
	type body struct {
		ClientId string `json:"clientId"`
	}
	var b body
	_ = c.Bind(&b)
	clientId := getRequestClientId(c, b.ClientId)
	if clientId == "" {
		clientId = "default"
	}
	h.App.MediastreamRepository.ShutdownTranscodeStream(clientId)
	return h.RespondWithData(c, true)
}

//
// Serve file
//

func (h *Handler) HandleMediastreamFile(c echo.Context) error {
	client := "1"
	fp := c.QueryParam("path")
	fp = util.ResolvePhysicalPath(fp)
	var libraryPaths []string
	if currentSettings, settingsErr := h.getSettings(c); settingsErr == nil && currentSettings.GetLibrary() != nil {
		libraryPaths = currentSettings.GetLibrary().GetLibraryPaths()
	}
	return h.App.MediastreamRepository.ServeEchoFile(c, fp, client, libraryPaths)
}

// HandleMediastreamLocalSubtitles
//
//	@summary get local subtitle files.
//	@desc This returns same-directory subtitle files for a local video file.
//	@returns []util.LocalSubtitleFile
//	@route /api/v1/mediastream/local-subtitles [GET]
func (h *Handler) HandleMediastreamLocalSubtitles(c echo.Context) error {
	if err := h.guardMediaConsumption(c); err != nil {
		return err
	}

	rawPath := c.QueryParam("path")
	fp, _ := url.PathUnescape(rawPath)
	if util.IsBase64(rawPath) {
		decodedPath, err := util.Base64DecodeStr(rawPath)
		if err == nil {
			fp = decodedPath
		}
	}

	fp = util.ResolvePhysicalPath(fp)

	if err := h.guardStrictFilesystemPath(c, fp); err != nil {
		return err
	}

	libraryPaths := h.App.Settings.GetLibrary().GetLibraryPaths()
	inLibrary := false
	for _, libraryPath := range libraryPaths {
		if util.IsFileUnderDir(fp, libraryPath) {
			inLibrary = true
			break
		}
	}
	if !inLibrary {
		return c.NoContent(http.StatusNotFound)
	}

	files, err := util.FindLocalSubtitleFiles(fp)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	return h.RespondWithData(c, files)
}
