package handlers

import (
	"net/http"
	"seanime/internal/core"
	"seanime/internal/database/models"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

// HandleSetupCheck
//
//	@summary checks if initial admin setup is needed.
//	@route /api/v1/auth/setup-check [GET]
//	@returns map[string]interface{}
func (h *Handler) HandleSetupCheck(c echo.Context) error {
	adminExists, _ := h.App.Database.AdminExists()
	hasAccessCode, _ := h.App.Database.HasAccessCode()
	return h.RespondWithData(c, map[string]interface{}{
		"needsSetup":    !adminExists,
		"hasAccessCode": hasAccessCode,
		"multiUser":     h.App.MultiUserEnabled,
		"sidecar":       h.App.IsDesktopSidecar,
	})
}

// HandleAdminSetup
//
//	@summary creates the initial admin account.
//	@route /api/v1/auth/setup [POST]
//	@returns map[string]interface{}
func (h *Handler) HandleAdminSetup(c echo.Context) error {
	type body struct {
		Username        string `json:"username"`
		Password        string `json:"password"`
		ConfirmPassword string `json:"confirmPassword"`
		AccessCode      string `json:"accessCode"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	if b.Username == "" || b.Password == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Username and password are required"})
	}

	if b.Password != b.ConfirmPassword {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Passwords do not match"})
	}

	exists, _ := h.App.Database.AdminExists()
	if exists {
		return c.JSON(http.StatusConflict, map[string]string{"error": "Admin already exists"})
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(b.Password), bcrypt.DefaultCost)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	// In Electron sidecar mode, ensureDefaultProfile may have already created a "Default" admin profile.
	// Reuse it instead of creating a duplicate.
	var profile *models.Profile
	if h.App.IsDesktopSidecar {
		existingProfiles, _ := h.App.Database.GetAllProfiles()
		for _, p := range existingProfiles {
			if p.IsAdmin {
				p.Name = b.Username
				profile, _ = h.App.Database.UpdateProfile(p)
				break
			}
		}
	}

	if profile == nil {
		profile, err = h.App.Database.CreateProfile(&models.Profile{
			UUIDBaseModel: models.UUIDBaseModel{ID: uuid.New().String()},
			Name:          b.Username,
			IsAdmin:       true,
		})
		if err != nil {
			return h.RespondWithError(c, err)
		}
		// Clone global settings for the new admin profile
		_, _ = h.App.Database.CloneSettingsForProfile(profile.ID)
		h.App.Database.CloneMediastreamSettingsForProfile(profile.ID)
		h.App.Database.CloneTorrentstreamSettingsForProfile(profile.ID)
		h.App.Database.CloneDebridSettingsForProfile(profile.ID)
	}

	_, err = h.App.Database.CreateAdmin(&models.Admin{
		UUIDBaseModel: models.UUIDBaseModel{ID: uuid.New().String()},
		Username:      b.Username,
		PasswordHash:  string(passwordHash),
		ProfileID:     profile.ID,
	})
	if err != nil {
		return h.RespondWithError(c, err)
	}

	if b.AccessCode != "" {
		accessCodeHash, err := bcrypt.GenerateFromPassword([]byte(b.AccessCode), bcrypt.DefaultCost)
		if err != nil {
			return h.RespondWithError(c, err)
		}
		_, err = h.App.Database.UpsertInstanceConfig(&models.InstanceConfig{
			AccessCodeHash: string(accessCodeHash),
		})
		if err != nil {
			return h.RespondWithError(c, err)
		}
	}

	h.App.MultiUserEnabled = true
	h.App.Logger.Info().Msg("app: Admin account created")

	return h.RespondWithData(c, map[string]interface{}{"success": true})
}

// HandleAdminLogin
//
//	@summary authenticates an admin user.
//	@route /api/v1/auth/admin-login [POST]
//	@returns map[string]interface{}
func (h *Handler) HandleAdminLogin(c echo.Context) error {
	type body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	admin, err := h.App.Database.GetAdminByUsername(b.Username)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid credentials"})
	}

	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(b.Password)); err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid credentials"})
	}

	token, err := core.GenerateToken(h.App.JWTSecret, admin.ProfileID, true, "admin", 24*time.Hour)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	c.SetCookie(&http.Cookie{
		Name:     "seanime-auth",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})

	return h.RespondWithData(c, map[string]interface{}{
		"token":   token,
		"profile": admin.Profile,
	})
}

// HandleAccessCode
//
//	@summary authenticates using an access code.
//	@route /api/v1/auth/access-code [POST]
//	@returns map[string]interface{}
func (h *Handler) HandleAccessCode(c echo.Context) error {
	type body struct {
		AccessCode string `json:"accessCode"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	config, err := h.App.Database.GetInstanceConfig()
	if err != nil || config.AccessCodeHash == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "No access code configured"})
	}

	if err := bcrypt.CompareHashAndPassword([]byte(config.AccessCodeHash), []byte(b.AccessCode)); err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid access code"})
	}

	token, err := core.GenerateToken(h.App.JWTSecret, "", false, "access", 24*time.Hour)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	c.SetCookie(&http.Cookie{
		Name:     "seanime-auth",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})

	return h.RespondWithData(c, map[string]interface{}{"token": token})
}

// HandleGetProfiles
//
//	@summary returns all profiles.
//	@route /api/v1/auth/profiles [GET]
//	@returns []models.Profile
func (h *Handler) HandleGetProfiles(c echo.Context) error {
	profiles, err := h.App.Database.GetAllProfiles()
	if err != nil {
		return h.RespondWithError(c, err)
	}
	return h.RespondWithData(c, profiles)
}

// HandleSelectProfile
//
//	@summary selects a profile and sets the auth cookie.
//	@route /api/v1/auth/select-profile [POST]
//	@returns map[string]interface{}
func (h *Handler) HandleSelectProfile(c echo.Context) error {
	type body struct {
		ProfileID string `json:"profileId"`
		Pin       string `json:"pin"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	profile, err := h.App.Database.GetProfileByID(b.ProfileID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Profile not found"})
	}

	if profile.PinHash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(profile.PinHash), []byte(b.Pin)); err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid PIN"})
		}
	}

	isAdmin := core.GetIsAdminFromContext(c) || profile.IsAdmin

	scope := "profile"
	if isAdmin {
		scope = "admin"
	}
	token, err := core.GenerateToken(h.App.JWTSecret, profile.ID, isAdmin, scope, 24*time.Hour)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	c.SetCookie(&http.Cookie{
		Name:     "seanime-auth",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})

	return h.RespondWithData(c, map[string]interface{}{
		"token":   token,
		"profile": profile,
	})
}

// HandleGetMe
//
//	@summary returns the current authenticated user info.
//	@route /api/v1/auth/me [GET]
//	@returns map[string]interface{}
func (h *Handler) HandleGetMe(c echo.Context) error {
	profileID := core.GetProfileIDFromContext(c)
	isAdmin := core.GetIsAdminFromContext(c)
	scope := core.GetAuthScopeFromContext(c)

	result := map[string]interface{}{
		"isAdmin": isAdmin,
		"scope":   scope,
	}

	if profileID != "" {
		profile, err := h.App.Database.GetProfileByID(profileID)
		if err == nil {
			result["profile"] = profile
		}
	}

	return h.RespondWithData(c, result)
}

// HandleSelfCreateProfile
//
//	@summary allows a household member (access code scope) to create their own profile.
//	@route /api/v1/auth/create-profile [POST]
//	@returns *models.Profile
func (h *Handler) HandleSelfCreateProfile(c echo.Context) error {
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

	profile, err := h.App.Database.CreateProfile(&models.Profile{
		UUIDBaseModel: models.UUIDBaseModel{ID: uuid.New().String()},
		Name:          b.Name,
	})
	if err != nil {
		return h.RespondWithError(c, err)
	}

	// Clone global settings for the new profile
	_, _ = h.App.Database.CloneSettingsForProfile(profile.ID)
	h.App.Database.CloneMediastreamSettingsForProfile(profile.ID)
	h.App.Database.CloneTorrentstreamSettingsForProfile(profile.ID)
	h.App.Database.CloneDebridSettingsForProfile(profile.ID)

	return h.RespondWithData(c, profile)
}

// HandleLogoutAuth
//
//	@summary logs out the current session by clearing the auth cookie.
//	@route /api/v1/auth/logout-session [POST]
//	@returns map[string]interface{}
func (h *Handler) HandleLogoutAuth(c echo.Context) error {
	c.SetCookie(&http.Cookie{
		Name:     "seanime-auth",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	return h.RespondWithData(c, map[string]interface{}{"success": true})
}
