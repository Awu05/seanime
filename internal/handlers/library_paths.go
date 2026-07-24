package handlers

import (
	"net/http"
	"path/filepath"
	"seanime/internal/core"
	"seanime/internal/database/models"
	"seanime/internal/util"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// validateLibraryPath rejects paths that would expose the Seanime data
// directory (SQLite DB with credential hashes and AniList tokens) to
// scanning/streaming, in either direction of containment.
func (h *Handler) validateLibraryPath(path string) (string, bool) {
	cleaned := filepath.Clean(path)
	if cleaned == "" || cleaned == "." {
		return "", false
	}
	if h.App.Config == nil || h.App.Config.Data.AppDataDir == "" {
		return cleaned, true
	}
	norm := func(p string) string {
		return strings.TrimSuffix(util.NormalizePath(filepath.Clean(p)), "/") + "/"
	}
	np, nd := norm(cleaned), norm(h.App.Config.Data.AppDataDir)
	if strings.HasPrefix(nd, np) || strings.HasPrefix(np, nd) {
		return "", false
	}
	return cleaned, true
}

func (h *Handler) HandleGetLibraryPaths(c echo.Context) error {
	profileID := core.GetProfileIDFromContext(c)
	isAdmin := core.GetIsAdminFromContext(c)

	var paths []*models.LibraryPath
	var err error

	if isAdmin {
		paths, err = h.App.Database.GetAllLibraryPaths()
	} else {
		paths, err = h.App.Database.GetLibraryPathsForProfile(profileID)
	}

	if err != nil {
		return h.RespondWithError(c, err)
	}

	return h.RespondWithData(c, paths)
}

func (h *Handler) HandleAddLibraryPath(c echo.Context) error {
	profileID := core.GetProfileIDFromContext(c)

	// Registering server filesystem paths is an admin capability: a non-admin
	// profile could otherwise add the config dir (or /) and have it scanned
	// and streamed back to them.
	if !core.GetIsAdminFromContext(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "Admin access required"})
	}

	type body struct {
		Path    string `json:"path"`
		OwnerID string `json:"ownerId"`
		Shared  bool   `json:"shared"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	cleaned, ok := h.validateLibraryPath(b.Path)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid library path"})
	}

	lp, err := h.App.Database.CreateLibraryPath(&models.LibraryPath{
		UUIDBaseModel: models.UUIDBaseModel{ID: uuid.New().String()},
		Path:          cleaned,
		OwnerID:       b.OwnerID,
		Shared:        b.Shared,
	})
	if err != nil {
		return h.RespondWithError(c, err)
	}

	h.App.InitOrRefreshModules(profileID)

	return h.RespondWithData(c, lp)
}

func (h *Handler) HandleDeleteLibraryPath(c echo.Context) error {
	profileID := core.GetProfileIDFromContext(c)
	isAdmin := core.GetIsAdminFromContext(c)
	id := c.Param("id")

	lp, err := h.App.Database.GetLibraryPathByID(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Library path not found"})
	}

	if !isAdmin && lp.OwnerID != profileID {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "Not authorized to delete this path"})
	}

	if err := h.App.Database.DeleteLibraryPath(id); err != nil {
		return h.RespondWithError(c, err)
	}

	h.App.InitOrRefreshModules(profileID)

	return h.RespondWithData(c, map[string]interface{}{"success": true})
}
