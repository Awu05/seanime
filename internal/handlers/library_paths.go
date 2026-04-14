package handlers

import (
	"net/http"
	"seanime/internal/core"
	"seanime/internal/database/models"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

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
	isAdmin := core.GetIsAdminFromContext(c)

	type body struct {
		Path    string `json:"path"`
		OwnerID string `json:"ownerId"`
		Shared  bool   `json:"shared"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	if !isAdmin {
		b.OwnerID = profileID
	}

	lp, err := h.App.Database.CreateLibraryPath(&models.LibraryPath{
		UUIDBaseModel: models.UUIDBaseModel{ID: uuid.New().String()},
		Path:          b.Path,
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
