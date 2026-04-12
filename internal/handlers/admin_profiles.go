package handlers

import (
	"net/http"
	"seanime/internal/core"
	"seanime/internal/database/models"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

// HandleCreateProfile
//
//	@summary creates a new profile (admin only).
//	@route /api/v1/admin/profiles [POST]
//	@returns models.Profile
func (h *Handler) HandleCreateProfile(c echo.Context) error {
	if !core.GetIsAdminFromContext(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "Admin access required"})
	}

	type body struct {
		Name   string `json:"name"`
		Avatar string `json:"avatar"`
		Pin    string `json:"pin"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	profile := &models.Profile{
		UUIDBaseModel: models.UUIDBaseModel{ID: uuid.New().String()},
		Name:          b.Name,
		Avatar:        b.Avatar,
	}

	if b.Pin != "" {
		pinHash, err := bcrypt.GenerateFromPassword([]byte(b.Pin), bcrypt.DefaultCost)
		if err != nil {
			return h.RespondWithError(c, err)
		}
		profile.PinHash = string(pinHash)
	}

	created, err := h.App.Database.CreateProfile(profile)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	// Clone global settings for the new profile
	_, _ = h.App.Database.CloneSettingsForProfile(created.ID)
	h.App.Database.CloneMediastreamSettingsForProfile(created.ID)
	h.App.Database.CloneTorrentstreamSettingsForProfile(created.ID)
	h.App.Database.CloneDebridSettingsForProfile(created.ID)

	return h.RespondWithData(c, created)
}

// HandleDeleteProfile
//
//	@summary deletes a profile by ID (admin only).
//	@route /api/v1/admin/profiles/:id [DELETE]
//	@returns map[string]interface{}
func (h *Handler) HandleDeleteProfile(c echo.Context) error {
	if !core.GetIsAdminFromContext(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "Admin access required"})
	}

	id := c.Param("id")
	if id == "" {
		// Fallback: read from body (for POST-based deletion)
		type bodyStruct struct {
			ID string `json:"id"`
		}
		var b bodyStruct
		if err := c.Bind(&b); err == nil && b.ID != "" {
			id = b.ID
		}
	}

	profile, err := h.App.Database.GetProfileByID(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Profile not found"})
	}
	if profile.IsAdmin {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "Cannot delete admin profile"})
	}

	if err := h.App.Database.DeleteProfile(id); err != nil {
		return h.RespondWithError(c, err)
	}

	return h.RespondWithData(c, map[string]interface{}{"success": true})
}

// HandleSetAccessCode
//
//	@summary sets or updates the instance access code (admin only).
//	@route /api/v1/admin/access-code [POST]
//	@returns map[string]interface{}
func (h *Handler) HandleSetAccessCode(c echo.Context) error {
	if !core.GetIsAdminFromContext(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "Admin access required"})
	}

	type body struct {
		AccessCode string `json:"accessCode"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	var accessCodeHash string
	if b.AccessCode != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(b.AccessCode), bcrypt.DefaultCost)
		if err != nil {
			return h.RespondWithError(c, err)
		}
		accessCodeHash = string(hash)
	}

	_, err := h.App.Database.UpsertInstanceConfig(&models.InstanceConfig{
		AccessCodeHash: accessCodeHash,
	})
	if err != nil {
		return h.RespondWithError(c, err)
	}

	return h.RespondWithData(c, map[string]interface{}{"success": true})
}

func (h *Handler) HandleUpdateProfilePin(c echo.Context) error {
	profileID := core.GetProfileIDFromContext(c)
	isAdmin := core.GetIsAdminFromContext(c)
	targetID := c.Param("id")

	if profileID != targetID && !isAdmin {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "Not authorized"})
	}

	type body struct {
		Pin string `json:"pin"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	profile, err := h.App.Database.GetProfileByID(targetID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Profile not found"})
	}

	if b.Pin != "" {
		pinHash, err := bcrypt.GenerateFromPassword([]byte(b.Pin), bcrypt.DefaultCost)
		if err != nil {
			return h.RespondWithError(c, err)
		}
		profile.PinHash = string(pinHash)
	} else {
		profile.PinHash = ""
	}

	_, err = h.App.Database.UpdateProfile(profile)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	return h.RespondWithData(c, map[string]interface{}{"success": true})
}

// HandleUpdateProfileName
//
//	@summary updates a profile's display name.
//	@desc Profile owner or admin can update.
//	@route /api/v1/profiles/:id/name [POST]
//	@returns map[string]interface{}
func (h *Handler) HandleUpdateProfileName(c echo.Context) error {
	profileID := core.GetProfileIDFromContext(c)
	isAdmin := core.GetIsAdminFromContext(c)
	targetID := c.Param("id")

	if profileID != targetID && !isAdmin {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "Not authorized"})
	}

	type body struct {
		Name string `json:"name"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	if b.Name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Name is required"})
	}

	profile, err := h.App.Database.GetProfileByID(targetID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Profile not found"})
	}

	profile.Name = b.Name
	_, err = h.App.Database.UpdateProfile(profile)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	return h.RespondWithData(c, map[string]interface{}{"success": true})
}
