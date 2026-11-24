package handlers

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"seanime/internal/util"
	"strconv"

	"github.com/labstack/echo/v4"
)

// HandleVideoCoreInSightGetCharacterDetails
//
//	@summary returns the character details.
//	@param malId - int - true - "The MAL character ID"
//	@returns videocore.InSightCharacterDetails
//	@route /api/v1/videocore/insight/character/{malId} [GET]
func (h *Handler) HandleVideoCoreInSightGetCharacterDetails(c echo.Context) error {
	malId, err := strconv.Atoi(c.Param("malId"))
	if err != nil {
		return h.RespondWithError(c, err)
	}

	session := h.getStreamSession(c)

	ret, err := session.VideoCore.InSight().GetCharacterInfo(malId)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	return h.RespondWithData(c, ret)
}

// HandleVideoCoreSaveScreenshot
//
//	@summary saves a screenshot to a local directory.
//	@route /api/v1/videocore/screenshot [POST]
//	@returns bool
func (h *Handler) HandleVideoCoreSaveScreenshot(c echo.Context) error {
	type body struct {
		Dir        string `json:"dir"`
		Filename   string `json:"filename"`
		Base64Data string `json:"base64Data"`
	}

	var req body
	if err := c.Bind(&req); err != nil {
		return h.RespondWithError(c, err)
	}

	if req.Dir == "" || req.Filename == "" || req.Base64Data == "" {
		return h.RespondWithError(c, fmt.Errorf("missing required fields"))
	}

	// A filename with a path could write outside the screenshot folder.
	if filepath.Base(req.Filename) != req.Filename {
		return h.RespondWithError(c, fmt.Errorf("filename must not contain a path"))
	}

	if err := h.guardPrivilegedLocalExecution(c); err != nil {
		return err
	}

	settings, err := h.getSettings(c)
	if err != nil {
		return h.RespondWithError(c, err)
	}
	// Screenshots only go to the folder set in settings, never to one the request names.
	configuredDir := settings.GetMediaPlayer().ScreenshotDir
	if configuredDir == "" {
		return h.RespondWithError(c, fmt.Errorf("set a screenshot folder in settings first"))
	}
	dir := resolveScreenshotDir(configuredDir)
	if !util.IsSameDir(resolveScreenshotDir(req.Dir), dir) {
		return h.RespondWithError(c, fmt.Errorf("screenshots must be saved to the configured folder"))
	}

	if err := h.guardStrictFilesystemPath(c, dir); err != nil {
		return err
	}

	data, err := base64.StdEncoding.DecodeString(req.Base64Data)
	if err != nil {
		return h.RespondWithError(c, fmt.Errorf("failed to decode base64 data: %w", err))
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return h.RespondWithError(c, fmt.Errorf("failed to create directory: %w", err))
	}

	// os.Root refuses writes that resolve outside dir, e.g. through a symlink in it.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return h.RespondWithError(c, fmt.Errorf("failed to open screenshot directory: %w", err))
	}
	defer root.Close()
	if err := root.WriteFile(req.Filename, data, 0644); err != nil {
		return h.RespondWithError(c, fmt.Errorf("failed to write file: %w", err))
	}

	return h.RespondWithData(c, true)
}

// resolveScreenshotDir makes a screenshot folder absolute: a relative one is under the home folder.
func resolveScreenshotDir(dir string) string {
	dir = util.ResolvePhysicalPath(dir)
	if !filepath.IsAbs(dir) {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, dir)
		} else if absDir, err := filepath.Abs(dir); err == nil {
			dir = absDir
		}
	}
	return filepath.Clean(dir)
}
