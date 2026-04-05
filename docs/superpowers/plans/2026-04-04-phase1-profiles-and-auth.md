# Phase 1: Profiles & Instance Auth — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add admin accounts, instance access codes, Netflix-style profiles, JWT-based auth, and request context threading to seanime — while preserving Electron single-user mode unchanged.

**Architecture:** New GORM models for `Admin`, `Profile`, and `InstanceConfig` tables with UUID primary keys. JWT auth middleware extracts profile context from cookies/headers and injects it into echo.Context. Desktop sidecar mode skips auth and uses an implicit default profile. Docker mode requires admin login + access code + profile selection.

**Tech Stack:** Go (Echo, GORM, SQLite), bcrypt (`golang.org/x/crypto/bcrypt`), JWT (`github.com/golang-jwt/jwt/v5`), React (TanStack Router, Jotai, axios)

---

## File Structure

### Backend — New Files

| File | Responsibility |
|------|---------------|
| `internal/database/models/admin.go` | Admin GORM model (UUID PK, username, password_hash, profile_id FK) |
| `internal/database/models/profile.go` | Profile GORM model (UUID PK, name, pin_hash, is_admin, avatar) |
| `internal/database/models/instance_config.go` | InstanceConfig GORM model (singleton, access_code hash) |
| `internal/database/db/admin.go` | Admin CRUD operations on Database receiver |
| `internal/database/db/profile.go` | Profile CRUD operations on Database receiver |
| `internal/database/db/instance_config.go` | InstanceConfig CRUD operations on Database receiver |
| `internal/core/auth.go` | JWT helpers: GenerateToken, ParseToken, secret management |
| `internal/core/auth_middleware.go` | Echo middleware: extract JWT, attach profile to context, skip in sidecar mode |
| `internal/handlers/user_auth.go` | Auth endpoints: admin-login, access-code, select-profile, me |
| `internal/handlers/admin_profiles.go` | Admin profile management endpoints: CRUD profiles, set access code |

### Backend — Modified Files

| File | Change |
|------|--------|
| `internal/database/db/db.go` | Add new models to `migrateTables()` |
| `internal/database/models/models.go` | Add `UUIDBaseModel` struct |
| `internal/handlers/routes.go` | Register new auth routes and middleware |
| `internal/core/app.go` | Add `JWTSecret` field, `MultiUserEnabled` field |
| `internal/core/modules.go` | Bootstrap admin from env vars on startup |
| `go.mod` / `go.sum` | Add jwt and bcrypt dependencies |

### Frontend — New Files

| File | Responsibility |
|------|---------------|
| `seanime-web/src/app/(main)/_atoms/auth.atoms.ts` | Auth state atoms (JWT token, current profile, auth status) |
| `seanime-web/src/app/(main)/_hooks/use-auth.ts` | Auth hook: login, access-code, select-profile, logout |
| `seanime-web/src/routes/_auth.tsx` | Auth layout route (no sidebar, centered) |
| `seanime-web/src/routes/_auth/login.tsx` | Admin login page |
| `seanime-web/src/routes/_auth/access.tsx` | Instance access code page |
| `seanime-web/src/routes/_auth/profiles.tsx` | Profile picker page |
| `seanime-web/src/routes/_auth/setup.tsx` | First-run admin setup page |
| `seanime-web/src/api/hooks/auth.hooks.ts` | TanStack Query mutation hooks for auth endpoints |

### Frontend — Modified Files

| File | Change |
|------|--------|
| `seanime-web/src/api/generated/endpoints.ts` | Add auth endpoint definitions |
| `seanime-web/src/api/client/requests.ts` | Send JWT cookie/header on requests |
| `seanime-web/src/routes/__root.tsx` | Wrap app in auth context, redirect unauthenticated |

---

## Tasks

### Task 1: Add Go Dependencies

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add JWT and bcrypt dependencies**

```bash
cd /c/Users/awu05/OneDrive/Documents/Github/seanime
go get github.com/golang-jwt/jwt/v5
go get golang.org/x/crypto/bcrypt
```

- [ ] **Step 2: Verify dependencies installed**

Run: `go mod tidy`
Expected: Clean output, no errors

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add jwt and bcrypt dependencies for multi-user auth"
```

---

### Task 2: UUID Base Model

**Files:**
- Modify: `internal/database/models/models.go`

- [ ] **Step 1: Add UUIDBaseModel struct**

Add this after the existing `BaseModel` struct in `internal/database/models/models.go`:

```go
type UUIDBaseModel struct {
	ID        string    `gorm:"primarykey;type:text" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/database/models/models.go
git commit -m "feat: add UUIDBaseModel for multi-user tables"
```

---

### Task 3: Profile Model

**Files:**
- Create: `internal/database/models/profile.go`

- [ ] **Step 1: Create profile model**

Create `internal/database/models/profile.go`:

```go
package models

// Profile represents a Netflix-style user profile within the seanime instance.
// Each profile has its own AniList account, watch progress, and settings overrides.
type Profile struct {
	UUIDBaseModel
	Name    string `gorm:"column:name;uniqueIndex;not null" json:"name"`
	PinHash string `gorm:"column:pin_hash" json:"-"`
	IsAdmin bool   `gorm:"column:is_admin;default:false" json:"isAdmin"`
	Avatar  string `gorm:"column:avatar" json:"avatar"`
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/database/models/profile.go
git commit -m "feat: add Profile model"
```

---

### Task 4: Admin Model

**Files:**
- Create: `internal/database/models/admin.go`

- [ ] **Step 1: Create admin model**

Create `internal/database/models/admin.go`:

```go
package models

// Admin represents the instance administrator who authenticates with username/password.
// There is exactly one admin per instance, linked to a profile.
type Admin struct {
	UUIDBaseModel
	Username     string `gorm:"column:username;uniqueIndex;not null" json:"username"`
	PasswordHash string `gorm:"column:password_hash;not null" json:"-"`
	ProfileID    string `gorm:"column:profile_id;not null" json:"profileId"`
	Profile      Profile `gorm:"foreignKey:ProfileID" json:"profile,omitempty"`
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/database/models/admin.go
git commit -m "feat: add Admin model"
```

---

### Task 5: InstanceConfig Model

**Files:**
- Create: `internal/database/models/instance_config.go`

- [ ] **Step 1: Create instance config model**

Create `internal/database/models/instance_config.go`:

```go
package models

// InstanceConfig stores instance-wide configuration such as the household access code.
// This is a singleton row (ID = "1").
type InstanceConfig struct {
	ID             string `gorm:"primarykey;type:text;default:'1'" json:"id"`
	AccessCodeHash string `gorm:"column:access_code_hash" json:"-"`
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/database/models/instance_config.go
git commit -m "feat: add InstanceConfig model"
```

---

### Task 6: Register Models in Auto-Migration

**Files:**
- Modify: `internal/database/db/db.go`

- [ ] **Step 1: Add new models to migrateTables**

In `internal/database/db/db.go`, add the three new models to the `db.AutoMigrate(...)` call inside `migrateTables()`. Add them after the last existing model (`&models.MediaMetadataParent{}`):

```go
		&models.MediaMetadataParent{},
		&models.Profile{},
		&models.Admin{},
		&models.InstanceConfig{},
```

Note: `Profile` must come before `Admin` because `Admin` has a foreign key to `Profile`.

- [ ] **Step 2: Commit**

```bash
git add internal/database/db/db.go
git commit -m "feat: register Profile, Admin, InstanceConfig in auto-migration"
```

---

### Task 7: Profile Database Operations

**Files:**
- Create: `internal/database/db/profile.go`

- [ ] **Step 1: Create profile CRUD operations**

Create `internal/database/db/profile.go`:

```go
package db

import (
	"seanime/internal/database/models"

	"github.com/google/uuid"
)

func (db *Database) CreateProfile(profile *models.Profile) (*models.Profile, error) {
	if profile.ID == "" {
		profile.ID = uuid.New().String()
	}
	err := db.gormdb.Create(profile).Error
	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to create profile")
		return nil, err
	}
	return profile, nil
}

func (db *Database) GetProfileByID(id string) (*models.Profile, error) {
	var profile models.Profile
	err := db.gormdb.Where("id = ?", id).First(&profile).Error
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (db *Database) GetProfileByName(name string) (*models.Profile, error) {
	var profile models.Profile
	err := db.gormdb.Where("name = ?", name).First(&profile).Error
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (db *Database) GetAllProfiles() ([]*models.Profile, error) {
	var profiles []*models.Profile
	err := db.gormdb.Order("created_at ASC").Find(&profiles).Error
	if err != nil {
		return nil, err
	}
	return profiles, nil
}

func (db *Database) UpdateProfile(profile *models.Profile) (*models.Profile, error) {
	err := db.gormdb.Save(profile).Error
	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to update profile")
		return nil, err
	}
	return profile, nil
}

func (db *Database) DeleteProfile(id string) error {
	err := db.gormdb.Where("id = ?", id).Delete(&models.Profile{}).Error
	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to delete profile")
	}
	return err
}

func (db *Database) CountProfiles() (int64, error) {
	var count int64
	err := db.gormdb.Model(&models.Profile{}).Count(&count).Error
	return count, err
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/database/db/profile.go
git commit -m "feat: add Profile database operations"
```

---

### Task 8: Admin Database Operations

**Files:**
- Create: `internal/database/db/admin.go`

- [ ] **Step 1: Create admin CRUD operations**

Create `internal/database/db/admin.go`:

```go
package db

import (
	"seanime/internal/database/models"

	"github.com/google/uuid"
)

func (db *Database) CreateAdmin(admin *models.Admin) (*models.Admin, error) {
	if admin.ID == "" {
		admin.ID = uuid.New().String()
	}
	err := db.gormdb.Create(admin).Error
	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to create admin")
		return nil, err
	}
	return admin, nil
}

func (db *Database) GetAdmin() (*models.Admin, error) {
	var admin models.Admin
	err := db.gormdb.Preload("Profile").First(&admin).Error
	if err != nil {
		return nil, err
	}
	return &admin, nil
}

func (db *Database) GetAdminByUsername(username string) (*models.Admin, error) {
	var admin models.Admin
	err := db.gormdb.Preload("Profile").Where("username = ?", username).First(&admin).Error
	if err != nil {
		return nil, err
	}
	return &admin, nil
}

func (db *Database) AdminExists() (bool, error) {
	var count int64
	err := db.gormdb.Model(&models.Admin{}).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (db *Database) UpdateAdminPassword(id string, passwordHash string) error {
	err := db.gormdb.Model(&models.Admin{}).Where("id = ?", id).Update("password_hash", passwordHash).Error
	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to update admin password")
	}
	return err
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/database/db/admin.go
git commit -m "feat: add Admin database operations"
```

---

### Task 9: InstanceConfig Database Operations

**Files:**
- Create: `internal/database/db/instance_config.go`

- [ ] **Step 1: Create instance config operations**

Create `internal/database/db/instance_config.go`:

```go
package db

import (
	"seanime/internal/database/models"

	"gorm.io/gorm/clause"
)

func (db *Database) GetInstanceConfig() (*models.InstanceConfig, error) {
	var config models.InstanceConfig
	err := db.gormdb.Where("id = ?", "1").First(&config).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (db *Database) UpsertInstanceConfig(config *models.InstanceConfig) (*models.InstanceConfig, error) {
	config.ID = "1"
	err := db.gormdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		UpdateAll: true,
	}).Create(config).Error
	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to upsert instance config")
		return nil, err
	}
	return config, nil
}

func (db *Database) HasAccessCode() (bool, error) {
	config, err := db.GetInstanceConfig()
	if err != nil {
		return false, nil // No config row = no access code
	}
	return config.AccessCodeHash != "", nil
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/database/db/instance_config.go
git commit -m "feat: add InstanceConfig database operations"
```

---

### Task 10: JWT Helpers

**Files:**
- Create: `internal/core/auth.go`

- [ ] **Step 1: Create JWT helper functions**

Create `internal/core/auth.go`:

```go
package core

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type AuthClaims struct {
	ProfileID string `json:"profileId,omitempty"`
	IsAdmin   bool   `json:"isAdmin"`
	// "admin" = full access, "access" = access code validated (can pick profile), "profile" = profile selected
	Scope string `json:"scope"`
	jwt.RegisteredClaims
}

// GenerateJWTSecret creates a random 32-byte hex string for signing JWTs.
// Called once on first startup, stored in instance config or app state.
func GenerateJWTSecret() (string, error) {
	bytes := make([]byte, 32)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// GenerateToken creates a signed JWT with the given claims.
func GenerateToken(secret string, profileID string, isAdmin bool, scope string, duration time.Duration) (string, error) {
	claims := AuthClaims{
		ProfileID: profileID,
		IsAdmin:   isAdmin,
		Scope:     scope,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseToken validates and parses a JWT string into AuthClaims.
func ParseToken(secret string, tokenString string) (*AuthClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &AuthClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*AuthClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/core/auth.go
git commit -m "feat: add JWT helper functions"
```

---

### Task 11: Auth Middleware

**Files:**
- Create: `internal/core/auth_middleware.go`
- Modify: `internal/core/app.go`

- [ ] **Step 1: Add JWTSecret and MultiUserEnabled fields to App struct**

In `internal/core/app.go`, add these fields to the `App` struct:

```go
	JWTSecret        string
	MultiUserEnabled bool
```

- [ ] **Step 2: Create auth middleware**

Create `internal/core/auth_middleware.go`:

```go
package core

// GetProfileIDFromContext extracts the profile ID from the echo context.
// Returns empty string if no profile is set (e.g. desktop sidecar single-user mode).
func GetProfileIDFromContext(c interface{ Get(string) interface{} }) string {
	v := c.Get("profileId")
	if v == nil {
		return ""
	}
	return v.(string)
}

// GetIsAdminFromContext extracts the admin flag from the echo context.
func GetIsAdminFromContext(c interface{ Get(string) interface{} }) bool {
	v := c.Get("isAdmin")
	if v == nil {
		return false
	}
	return v.(bool)
}

// GetAuthScopeFromContext extracts the auth scope from the echo context.
func GetAuthScopeFromContext(c interface{ Get(string) interface{} }) string {
	v := c.Get("authScope")
	if v == nil {
		return ""
	}
	return v.(string)
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/core/auth_middleware.go internal/core/app.go
git commit -m "feat: add auth context helpers and App auth fields"
```

---

### Task 12: Auth Middleware in Handlers

**Files:**
- Modify: `internal/handlers/routes.go`

- [ ] **Step 1: Create the auth middleware handler method**

Add a new file `internal/handlers/auth_middleware.go`:

```go
package handlers

import (
	"net/http"
	"seanime/internal/core"
	"strings"

	"github.com/labstack/echo/v4"
)

// publicPaths are paths that don't require authentication.
var publicPaths = []string{
	"/api/v1/status",
	"/api/v1/auth/admin-login",
	"/api/v1/auth/access-code",
	"/api/v1/auth/setup",
	"/api/v1/auth/setup-check",
}

func (h *Handler) MultiUserAuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Skip auth in desktop sidecar single-user mode
		if h.App.IsDesktopSidecar && !h.App.MultiUserEnabled {
			// Set implicit admin context
			c.Set("profileId", "")
			c.Set("isAdmin", true)
			c.Set("authScope", "admin")
			return next(c)
		}

		// Skip auth for public paths
		path := c.Request().URL.Path
		for _, p := range publicPaths {
			if path == p || strings.HasPrefix(path, p) {
				return next(c)
			}
		}

		// Also allow WebSocket events endpoint and static assets
		if path == "/events" || strings.HasPrefix(path, "/_next") || strings.HasPrefix(path, "/icons") {
			return next(c)
		}

		// Extract JWT from cookie first, then Authorization header
		var tokenString string
		cookie, err := c.Cookie("seanime-auth")
		if err == nil && cookie.Value != "" {
			tokenString = cookie.Value
		} else {
			auth := c.Request().Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				tokenString = strings.TrimPrefix(auth, "Bearer ")
			}
		}

		// Also check query parameter (for WebSocket and streaming URLs)
		if tokenString == "" {
			tokenString = c.QueryParam("auth_token")
		}

		if tokenString == "" {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "UNAUTHENTICATED"})
		}

		// Parse and validate JWT
		claims, err := core.ParseToken(h.App.JWTSecret, tokenString)
		if err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "INVALID_TOKEN"})
		}

		// Profile selection endpoints only need "access" or "admin" scope
		if path == "/api/v1/auth/select-profile" || path == "/api/v1/auth/profiles" {
			if claims.Scope == "access" || claims.Scope == "admin" || claims.Scope == "profile" {
				c.Set("profileId", claims.ProfileID)
				c.Set("isAdmin", claims.IsAdmin)
				c.Set("authScope", claims.Scope)
				return next(c)
			}
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "INSUFFICIENT_SCOPE"})
		}

		// All other endpoints require "profile" or "admin" scope (profile must be selected)
		if claims.Scope != "profile" && claims.Scope != "admin" {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "PROFILE_NOT_SELECTED"})
		}

		c.Set("profileId", claims.ProfileID)
		c.Set("isAdmin", claims.IsAdmin)
		c.Set("authScope", claims.Scope)

		return next(c)
	}
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/handlers/auth_middleware.go
git commit -m "feat: add multi-user auth middleware"
```

---

### Task 13: Auth Handler Endpoints

**Files:**
- Create: `internal/handlers/user_auth.go`

- [ ] **Step 1: Create auth handler endpoints**

Create `internal/handlers/user_auth.go`:

```go
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
//	@summary checks if the instance has been set up (admin exists).
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
//	@summary creates the initial admin account during first-run setup.
//	@route /api/v1/auth/setup [POST]
//	@returns map[string]interface{}
func (h *Handler) HandleAdminSetup(c echo.Context) error {
	type body struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		AccessCode string `json:"accessCode"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	// Check if admin already exists
	exists, _ := h.App.Database.AdminExists()
	if exists {
		return c.JSON(http.StatusConflict, map[string]string{"error": "Admin already exists"})
	}

	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(b.Password), bcrypt.DefaultCost)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	// Create admin profile
	profile, err := h.App.Database.CreateProfile(&models.Profile{
		UUIDBaseModel: models.UUIDBaseModel{ID: uuid.New().String()},
		Name:          b.Username,
		IsAdmin:       true,
	})
	if err != nil {
		return h.RespondWithError(c, err)
	}

	// Create admin
	_, err = h.App.Database.CreateAdmin(&models.Admin{
		UUIDBaseModel: models.UUIDBaseModel{ID: uuid.New().String()},
		Username:      b.Username,
		PasswordHash:  string(passwordHash),
		ProfileID:     profile.ID,
	})
	if err != nil {
		return h.RespondWithError(c, err)
	}

	// Set access code if provided
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
//	@summary authenticates the admin with username/password and returns a JWT.
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

	// Generate JWT with admin scope — admin goes directly to their profile
	token, err := core.GenerateToken(h.App.JWTSecret, admin.ProfileID, true, "admin", 24*time.Hour)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	// Set httpOnly cookie
	c.SetCookie(&http.Cookie{
		Name:     "seanime-auth",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400, // 24 hours
	})

	return h.RespondWithData(c, map[string]interface{}{
		"token":   token,
		"profile": admin.Profile,
	})
}

// HandleAccessCode
//
//	@summary validates the household access code and returns a limited JWT for profile selection.
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

	// Generate limited JWT — only allows profile selection
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
//	@summary returns all profiles (for the profile picker).
//	@desc Requires at least "access" scope.
//	@route /api/v1/auth/profiles [GET]
//	@returns []*models.Profile
func (h *Handler) HandleGetProfiles(c echo.Context) error {
	profiles, err := h.App.Database.GetAllProfiles()
	if err != nil {
		return h.RespondWithError(c, err)
	}
	return h.RespondWithData(c, profiles)
}

// HandleSelectProfile
//
//	@summary selects a profile and returns a full JWT with profile context.
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

	// Verify PIN if set
	if profile.PinHash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(profile.PinHash), []byte(b.Pin)); err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid PIN"})
		}
	}

	// Determine if this profile is admin
	isAdmin := core.GetIsAdminFromContext(c) || profile.IsAdmin

	// Generate full JWT with profile scope
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
//	@summary returns the current auth session info (profile, scope, admin status).
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

// HandleLogoutAuth
//
//	@summary clears the auth cookie.
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
```

- [ ] **Step 2: Commit**

```bash
git add internal/handlers/user_auth.go
git commit -m "feat: add auth handler endpoints (setup, login, access-code, profiles, me)"
```

---

### Task 14: Admin Profile Management Endpoints

**Files:**
- Create: `internal/handlers/admin_profiles.go`

- [ ] **Step 1: Create admin profile management handlers**

Create `internal/handlers/admin_profiles.go`:

```go
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
//	@returns *models.Profile
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

	return h.RespondWithData(c, created)
}

// HandleDeleteProfile
//
//	@summary deletes a profile (admin only, cannot delete admin profile).
//	@route /api/v1/admin/profiles/:id [DELETE]
//	@returns map[string]interface{}
func (h *Handler) HandleDeleteProfile(c echo.Context) error {
	if !core.GetIsAdminFromContext(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "Admin access required"})
	}

	id := c.Param("id")

	// Prevent deleting admin profile
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

// HandleUpdateProfilePin
//
//	@summary sets or clears the PIN on a profile.
//	@desc Profile owner or admin can update. Empty pin clears it.
//	@route /api/v1/profiles/:id/pin [POST]
//	@returns map[string]interface{}
func (h *Handler) HandleUpdateProfilePin(c echo.Context) error {
	profileID := core.GetProfileIDFromContext(c)
	isAdmin := core.GetIsAdminFromContext(c)
	targetID := c.Param("id")

	// Only the profile owner or admin can set a PIN
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
```

- [ ] **Step 2: Commit**

```bash
git add internal/handlers/admin_profiles.go
git commit -m "feat: add admin profile management endpoints"
```

---

### Task 15: Register Routes and Bootstrap

**Files:**
- Modify: `internal/handlers/routes.go`
- Modify: `internal/core/modules.go`

- [ ] **Step 1: Register auth routes**

In `internal/handlers/routes.go`, add the new auth routes. After the existing `v1.POST("/auth/logout", h.HandleLogout)` line, add:

```go
	// Multi-user auth routes
	v1.GET("/auth/setup-check", h.HandleSetupCheck)
	v1.POST("/auth/setup", h.HandleAdminSetup)
	v1.POST("/auth/admin-login", h.HandleAdminLogin)
	v1.POST("/auth/access-code", h.HandleAccessCode)
	v1.GET("/auth/profiles", h.HandleGetProfiles)
	v1.POST("/auth/select-profile", h.HandleSelectProfile)
	v1.GET("/auth/me", h.HandleGetMe)
	v1.POST("/auth/logout-session", h.HandleLogoutAuth)

	// Admin profile management
	v1.POST("/admin/profiles", h.HandleCreateProfile)
	v1.DELETE("/admin/profiles/:id", h.HandleDeleteProfile)
	v1.POST("/admin/access-code", h.HandleSetAccessCode)
	v1.POST("/profiles/:id/pin", h.HandleUpdateProfilePin)
```

- [ ] **Step 2: Add JWT secret initialization and env var bootstrap to modules.go**

In `internal/core/modules.go`, inside `InitOrRefreshModules()`, after `a.Settings = settings`, add this block to initialize the JWT secret and bootstrap admin from env vars:

```go
	// Initialize JWT secret if not set
	if a.JWTSecret == "" {
		secret, err := GenerateJWTSecret()
		if err != nil {
			a.Logger.Error().Err(err).Msg("app: Failed to generate JWT secret")
		} else {
			a.JWTSecret = secret
		}
	}

	// Bootstrap admin from environment variables (Docker)
	if !a.IsDesktopSidecar {
		adminExists, _ := a.Database.AdminExists()
		if !adminExists {
			adminUser := os.Getenv("SEANIME_ADMIN_USERNAME")
			adminPass := os.Getenv("SEANIME_ADMIN_PASSWORD")
			if adminUser != "" && adminPass != "" {
				a.bootstrapAdminFromEnv(adminUser, adminPass, os.Getenv("SEANIME_INSTANCE_ACCESS_CODE"))
			}
		}
		exists, _ := a.Database.AdminExists()
		a.MultiUserEnabled = exists
	} else {
		// Desktop sidecar: check if multi-user was opted into
		exists, _ := a.Database.AdminExists()
		a.MultiUserEnabled = exists
		if !exists {
			// Create implicit default profile for single-user mode
			a.ensureDefaultProfile()
		}
	}
```

Add the `"os"` import to the imports in `modules.go`.

Also add these helper methods at the bottom of `modules.go`:

```go
func (a *App) bootstrapAdminFromEnv(username, password, accessCode string) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		a.Logger.Error().Err(err).Msg("app: Failed to hash admin password from env")
		return
	}

	profile, err := a.Database.CreateProfile(&models.Profile{
		UUIDBaseModel: models.UUIDBaseModel{ID: uuid.New().String()},
		Name:          username,
		IsAdmin:       true,
	})
	if err != nil {
		a.Logger.Error().Err(err).Msg("app: Failed to create admin profile from env")
		return
	}

	_, err = a.Database.CreateAdmin(&models.Admin{
		UUIDBaseModel: models.UUIDBaseModel{ID: uuid.New().String()},
		Username:      username,
		PasswordHash:  string(passwordHash),
		ProfileID:     profile.ID,
	})
	if err != nil {
		a.Logger.Error().Err(err).Msg("app: Failed to create admin from env")
		return
	}

	if accessCode != "" {
		codeHash, err := bcrypt.GenerateFromPassword([]byte(accessCode), bcrypt.DefaultCost)
		if err == nil {
			_, _ = a.Database.UpsertInstanceConfig(&models.InstanceConfig{
				AccessCodeHash: string(codeHash),
			})
		}
	}

	a.Logger.Info().Str("username", username).Msg("app: Admin account bootstrapped from environment variables")
}

func (a *App) ensureDefaultProfile() {
	count, _ := a.Database.CountProfiles()
	if count > 0 {
		return
	}
	_, err := a.Database.CreateProfile(&models.Profile{
		UUIDBaseModel: models.UUIDBaseModel{ID: uuid.New().String()},
		Name:          "Default",
		IsAdmin:       true,
	})
	if err != nil {
		a.Logger.Error().Err(err).Msg("app: Failed to create default profile")
	}
}
```

Add imports for `"os"`, `"golang.org/x/crypto/bcrypt"`, `"github.com/google/uuid"`, and `"seanime/internal/database/models"` to `modules.go`.

- [ ] **Step 3: Verify the project compiles**

```bash
cd /c/Users/awu05/OneDrive/Documents/Github/seanime
go build ./...
```

Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/handlers/routes.go internal/core/modules.go
git commit -m "feat: register auth routes and add admin env var bootstrap"
```

---

### Task 16: Add Auth Middleware to Route Chain

**Files:**
- Modify: `internal/handlers/routes.go`

- [ ] **Step 1: Add multi-user auth middleware to the v1 route group**

In `internal/handlers/routes.go`, add the `MultiUserAuthMiddleware` to the v1 group. Find this section:

```go
	v1.Use(h.OptionalAuthMiddleware)
	v1.Use(h.FeaturesMiddleware)
```

Add the new middleware before `OptionalAuthMiddleware`:

```go
	v1.Use(h.MultiUserAuthMiddleware)
	v1.Use(h.OptionalAuthMiddleware)
	v1.Use(h.FeaturesMiddleware)
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/handlers/routes.go
git commit -m "feat: add multi-user auth middleware to route chain"
```

---

### Task 17: Frontend Auth Endpoints

**Files:**
- Modify: `seanime-web/src/api/generated/endpoints.ts`

- [ ] **Step 1: Add auth endpoint definitions**

Add this section to `API_ENDPOINTS` in `seanime-web/src/api/generated/endpoints.ts`:

```typescript
    AUTH: {
        SetupCheck: {
            key: "AUTH-setup-check",
            methods: ["GET"],
            endpoint: "/api/v1/auth/setup-check",
        },
        Setup: {
            key: "AUTH-setup",
            methods: ["POST"],
            endpoint: "/api/v1/auth/setup",
        },
        AdminLogin: {
            key: "AUTH-admin-login",
            methods: ["POST"],
            endpoint: "/api/v1/auth/admin-login",
        },
        AccessCode: {
            key: "AUTH-access-code",
            methods: ["POST"],
            endpoint: "/api/v1/auth/access-code",
        },
        GetProfiles: {
            key: "AUTH-get-profiles",
            methods: ["GET"],
            endpoint: "/api/v1/auth/profiles",
        },
        SelectProfile: {
            key: "AUTH-select-profile",
            methods: ["POST"],
            endpoint: "/api/v1/auth/select-profile",
        },
        Me: {
            key: "AUTH-me",
            methods: ["GET"],
            endpoint: "/api/v1/auth/me",
        },
        LogoutSession: {
            key: "AUTH-logout-session",
            methods: ["POST"],
            endpoint: "/api/v1/auth/logout-session",
        },
        CreateProfile: {
            key: "AUTH-create-profile",
            methods: ["POST"],
            endpoint: "/api/v1/admin/profiles",
        },
        SetAccessCode: {
            key: "AUTH-set-access-code",
            methods: ["POST"],
            endpoint: "/api/v1/admin/access-code",
        },
    },
```

- [ ] **Step 2: Commit**

```bash
git add seanime-web/src/api/generated/endpoints.ts
git commit -m "feat: add auth API endpoint definitions"
```

---

### Task 18: Frontend Auth Hooks

**Files:**
- Create: `seanime-web/src/api/hooks/auth.hooks.ts`

- [ ] **Step 1: Create auth mutation hooks**

Create `seanime-web/src/api/hooks/auth.hooks.ts`:

```typescript
import { API_ENDPOINTS } from "@/api/generated/endpoints"
import { useServerMutation, useServerQuery } from "@/api/client/requests"

// Setup check — does admin exist?
export function useAuthSetupCheck() {
    return useServerQuery<{
        needsSetup: boolean
        hasAccessCode: boolean
        multiUser: boolean
        sidecar: boolean
    }>({
        endpoint: API_ENDPOINTS.AUTH.SetupCheck.endpoint,
        method: "GET",
        queryKey: [API_ENDPOINTS.AUTH.SetupCheck.key],
        enabled: true,
    })
}

// Initial admin setup
export function useAuthSetup() {
    return useServerMutation<
        { success: boolean },
        { username: string; password: string; accessCode?: string }
    >({
        endpoint: API_ENDPOINTS.AUTH.Setup.endpoint,
        method: "POST",
    })
}

// Admin login
export function useAuthAdminLogin() {
    return useServerMutation<
        { token: string; profile: any },
        { username: string; password: string }
    >({
        endpoint: API_ENDPOINTS.AUTH.AdminLogin.endpoint,
        method: "POST",
    })
}

// Access code validation
export function useAuthAccessCode() {
    return useServerMutation<
        { token: string },
        { accessCode: string }
    >({
        endpoint: API_ENDPOINTS.AUTH.AccessCode.endpoint,
        method: "POST",
    })
}

// Get all profiles
export function useAuthGetProfiles() {
    return useServerQuery<any[]>({
        endpoint: API_ENDPOINTS.AUTH.GetProfiles.endpoint,
        method: "GET",
        queryKey: [API_ENDPOINTS.AUTH.GetProfiles.key],
        enabled: true,
    })
}

// Select profile
export function useAuthSelectProfile() {
    return useServerMutation<
        { token: string; profile: any },
        { profileId: string; pin?: string }
    >({
        endpoint: API_ENDPOINTS.AUTH.SelectProfile.endpoint,
        method: "POST",
    })
}

// Get current session info
export function useAuthMe() {
    return useServerQuery<{
        profile?: any
        isAdmin: boolean
        scope: string
    }>({
        endpoint: API_ENDPOINTS.AUTH.Me.endpoint,
        method: "GET",
        queryKey: [API_ENDPOINTS.AUTH.Me.key],
        enabled: true,
    })
}

// Logout
export function useAuthLogout() {
    return useServerMutation<{ success: boolean }, void>({
        endpoint: API_ENDPOINTS.AUTH.LogoutSession.endpoint,
        method: "POST",
    })
}
```

- [ ] **Step 2: Commit**

```bash
git add seanime-web/src/api/hooks/auth.hooks.ts
git commit -m "feat: add frontend auth API hooks"
```

---

### Task 19: Frontend Auth Pages (Login, Access Code, Setup, Profile Picker)

This task creates the frontend auth pages. Since the frontend is a large body of UI code and depends on the project's existing component library (shadcn-style components under `@/components/ui/`), the pages should follow the existing patterns in `seanime-web/src/routes/`.

**Files:**
- Create: `seanime-web/src/routes/_auth.tsx`
- Create: `seanime-web/src/routes/_auth/login.tsx`
- Create: `seanime-web/src/routes/_auth/access.tsx`
- Create: `seanime-web/src/routes/_auth/profiles.tsx`
- Create: `seanime-web/src/routes/_auth/setup.tsx`

- [ ] **Step 1: Create auth layout route**

Create `seanime-web/src/routes/_auth.tsx`:

```typescript
import { createFileRoute, Outlet } from "@tanstack/react-router"

export const Route = createFileRoute("/_auth")({
    component: AuthLayout,
})

function AuthLayout() {
    return (
        <div className="min-h-screen flex items-center justify-center bg-gray-950">
            <div className="w-full max-w-md p-8">
                <Outlet />
            </div>
        </div>
    )
}
```

- [ ] **Step 2: Create login page**

Create `seanime-web/src/routes/_auth/login.tsx`:

```typescript
import { createFileRoute, useNavigate } from "@tanstack/react-router"
import { useAuthAdminLogin } from "@/api/hooks/auth.hooks"
import React from "react"

export const Route = createFileRoute("/_auth/login")({
    component: LoginPage,
})

function LoginPage() {
    const navigate = useNavigate()
    const { mutate: login, isPending } = useAuthAdminLogin()
    const [username, setUsername] = React.useState("")
    const [password, setPassword] = React.useState("")
    const [error, setError] = React.useState("")

    function handleSubmit(e: React.FormEvent) {
        e.preventDefault()
        setError("")
        login({ username, password }, {
            onSuccess: () => {
                navigate({ to: "/" })
            },
            onError: () => {
                setError("Invalid credentials")
            },
        })
    }

    return (
        <div className="space-y-6">
            <div className="text-center">
                <h1 className="text-2xl font-bold text-white">Admin Login</h1>
                <p className="text-gray-400 mt-2">Sign in to manage your seanime instance</p>
            </div>
            <form onSubmit={handleSubmit} className="space-y-4">
                <div>
                    <label className="block text-sm text-gray-300 mb-1">Username</label>
                    <input
                        type="text"
                        value={username}
                        onChange={e => setUsername(e.target.value)}
                        className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white"
                        required
                    />
                </div>
                <div>
                    <label className="block text-sm text-gray-300 mb-1">Password</label>
                    <input
                        type="password"
                        value={password}
                        onChange={e => setPassword(e.target.value)}
                        className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white"
                        required
                    />
                </div>
                {error && <p className="text-red-400 text-sm">{error}</p>}
                <button
                    type="submit"
                    disabled={isPending}
                    className="w-full py-2 bg-brand-500 hover:bg-brand-600 text-white rounded-lg font-medium disabled:opacity-50"
                >
                    {isPending ? "Signing in..." : "Sign In"}
                </button>
            </form>
            <div className="text-center">
                <button
                    onClick={() => navigate({ to: "/access" })}
                    className="text-sm text-gray-400 hover:text-white"
                >
                    Enter with access code instead
                </button>
            </div>
        </div>
    )
}
```

- [ ] **Step 3: Create access code page**

Create `seanime-web/src/routes/_auth/access.tsx`:

```typescript
import { createFileRoute, useNavigate } from "@tanstack/react-router"
import { useAuthAccessCode } from "@/api/hooks/auth.hooks"
import React from "react"

export const Route = createFileRoute("/_auth/access")({
    component: AccessCodePage,
})

function AccessCodePage() {
    const navigate = useNavigate()
    const { mutate: submitCode, isPending } = useAuthAccessCode()
    const [accessCode, setAccessCode] = React.useState("")
    const [error, setError] = React.useState("")

    function handleSubmit(e: React.FormEvent) {
        e.preventDefault()
        setError("")
        submitCode({ accessCode }, {
            onSuccess: () => {
                navigate({ to: "/profiles" })
            },
            onError: () => {
                setError("Invalid access code")
            },
        })
    }

    return (
        <div className="space-y-6">
            <div className="text-center">
                <h1 className="text-2xl font-bold text-white">Welcome</h1>
                <p className="text-gray-400 mt-2">Enter the household access code</p>
            </div>
            <form onSubmit={handleSubmit} className="space-y-4">
                <div>
                    <input
                        type="password"
                        value={accessCode}
                        onChange={e => setAccessCode(e.target.value)}
                        placeholder="Access code"
                        className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white text-center text-lg tracking-widest"
                        required
                    />
                </div>
                {error && <p className="text-red-400 text-sm text-center">{error}</p>}
                <button
                    type="submit"
                    disabled={isPending}
                    className="w-full py-2 bg-brand-500 hover:bg-brand-600 text-white rounded-lg font-medium disabled:opacity-50"
                >
                    {isPending ? "Verifying..." : "Continue"}
                </button>
            </form>
            <div className="text-center">
                <button
                    onClick={() => navigate({ to: "/login" })}
                    className="text-sm text-gray-400 hover:text-white"
                >
                    Admin login
                </button>
            </div>
        </div>
    )
}
```

- [ ] **Step 4: Create profile picker page**

Create `seanime-web/src/routes/_auth/profiles.tsx`:

```typescript
import { createFileRoute, useNavigate } from "@tanstack/react-router"
import { useAuthGetProfiles, useAuthSelectProfile } from "@/api/hooks/auth.hooks"
import React from "react"

export const Route = createFileRoute("/_auth/profiles")({
    component: ProfilePickerPage,
})

function ProfilePickerPage() {
    const navigate = useNavigate()
    const { data: profiles } = useAuthGetProfiles()
    const { mutate: selectProfile, isPending } = useAuthSelectProfile()
    const [pinFor, setPinFor] = React.useState<string | null>(null)
    const [pin, setPin] = React.useState("")
    const [error, setError] = React.useState("")

    function handleSelect(profileId: string, hasPin: boolean) {
        if (hasPin) {
            setPinFor(profileId)
            setPin("")
            setError("")
            return
        }
        selectProfile({ profileId }, {
            onSuccess: () => navigate({ to: "/" }),
            onError: () => setError("Failed to select profile"),
        })
    }

    function handlePinSubmit(e: React.FormEvent) {
        e.preventDefault()
        if (!pinFor) return
        selectProfile({ profileId: pinFor, pin }, {
            onSuccess: () => navigate({ to: "/" }),
            onError: () => setError("Invalid PIN"),
        })
    }

    if (pinFor) {
        return (
            <div className="space-y-6">
                <div className="text-center">
                    <h1 className="text-2xl font-bold text-white">Enter PIN</h1>
                </div>
                <form onSubmit={handlePinSubmit} className="space-y-4">
                    <input
                        type="password"
                        value={pin}
                        onChange={e => setPin(e.target.value)}
                        placeholder="PIN"
                        className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white text-center text-2xl tracking-widest"
                        maxLength={6}
                        autoFocus
                    />
                    {error && <p className="text-red-400 text-sm text-center">{error}</p>}
                    <div className="flex gap-2">
                        <button
                            type="button"
                            onClick={() => setPinFor(null)}
                            className="flex-1 py-2 bg-gray-800 text-white rounded-lg"
                        >
                            Back
                        </button>
                        <button
                            type="submit"
                            disabled={isPending}
                            className="flex-1 py-2 bg-brand-500 hover:bg-brand-600 text-white rounded-lg disabled:opacity-50"
                        >
                            Confirm
                        </button>
                    </div>
                </form>
            </div>
        )
    }

    return (
        <div className="space-y-6">
            <div className="text-center">
                <h1 className="text-2xl font-bold text-white">Who's watching?</h1>
            </div>
            <div className="grid grid-cols-2 gap-4">
                {profiles?.map(profile => (
                    <button
                        key={profile.id}
                        onClick={() => handleSelect(profile.id, !!profile.pinHash)}
                        className="flex flex-col items-center gap-2 p-4 rounded-lg border border-gray-700 hover:border-brand-500 transition-all"
                    >
                        <div className="w-16 h-16 rounded-full bg-gradient-to-br from-brand-500 to-brand-700 flex items-center justify-center text-white text-xl font-bold">
                            {profile.name[0]?.toUpperCase()}
                        </div>
                        <span className="text-white font-medium">{profile.name}</span>
                    </button>
                ))}
            </div>
        </div>
    )
}
```

- [ ] **Step 5: Create setup page**

Create `seanime-web/src/routes/_auth/setup.tsx`:

```typescript
import { createFileRoute, useNavigate } from "@tanstack/react-router"
import { useAuthSetup } from "@/api/hooks/auth.hooks"
import React from "react"

export const Route = createFileRoute("/_auth/setup")({
    component: SetupPage,
})

function SetupPage() {
    const navigate = useNavigate()
    const { mutate: setup, isPending } = useAuthSetup()
    const [username, setUsername] = React.useState("")
    const [password, setPassword] = React.useState("")
    const [accessCode, setAccessCode] = React.useState("")
    const [error, setError] = React.useState("")

    function handleSubmit(e: React.FormEvent) {
        e.preventDefault()
        setError("")
        setup({ username, password, accessCode: accessCode || undefined }, {
            onSuccess: () => {
                navigate({ to: "/login" })
            },
            onError: () => {
                setError("Failed to create admin account")
            },
        })
    }

    return (
        <div className="space-y-6">
            <div className="text-center">
                <h1 className="text-2xl font-bold text-white">Welcome to Seanime</h1>
                <p className="text-gray-400 mt-2">Create your admin account to get started</p>
            </div>
            <form onSubmit={handleSubmit} className="space-y-4">
                <div>
                    <label className="block text-sm text-gray-300 mb-1">Admin Username</label>
                    <input
                        type="text"
                        value={username}
                        onChange={e => setUsername(e.target.value)}
                        className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white"
                        required
                    />
                </div>
                <div>
                    <label className="block text-sm text-gray-300 mb-1">Admin Password</label>
                    <input
                        type="password"
                        value={password}
                        onChange={e => setPassword(e.target.value)}
                        className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white"
                        required
                    />
                </div>
                <div>
                    <label className="block text-sm text-gray-300 mb-1">Household Access Code (optional)</label>
                    <p className="text-xs text-gray-500 mb-1">Other household members use this to access the profile picker</p>
                    <input
                        type="text"
                        value={accessCode}
                        onChange={e => setAccessCode(e.target.value)}
                        className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white"
                    />
                </div>
                {error && <p className="text-red-400 text-sm">{error}</p>}
                <button
                    type="submit"
                    disabled={isPending}
                    className="w-full py-2 bg-brand-500 hover:bg-brand-600 text-white rounded-lg font-medium disabled:opacity-50"
                >
                    {isPending ? "Setting up..." : "Create Admin Account"}
                </button>
            </form>
        </div>
    )
}
```

- [ ] **Step 6: Commit**

```bash
git add seanime-web/src/routes/_auth.tsx seanime-web/src/routes/_auth/
git commit -m "feat: add frontend auth pages (login, access code, profiles, setup)"
```

---

### Task 20: Integrate Auth Redirect in Root Route

**Files:**
- Modify: `seanime-web/src/routes/__root.tsx`

- [ ] **Step 1: Add auth check redirect logic**

This task depends on how the root route currently works. The auth redirect should:

1. On app load, call `GET /api/v1/auth/setup-check`
2. If `needsSetup` is true → redirect to `/setup`
3. If `sidecar` is true and `multiUser` is false → skip auth (current behavior)
4. If no auth cookie present and `hasAccessCode` → redirect to `/access`
5. If no auth cookie present and no access code → redirect to `/login`

This integration point depends heavily on the existing root route structure. The implementation should be done by adding a check in the `ServerDataWrapper` component or the `_main` layout route that checks auth status before rendering the main app.

The exact implementation will need to be adapted to the existing routing structure during development. The key principle: **if not authenticated and not in sidecar single-user mode, redirect to the appropriate auth page**.

- [ ] **Step 2: Commit**

```bash
git add seanime-web/src/routes/__root.tsx
git commit -m "feat: add auth redirect logic to root route"
```

---

### Task 21: Verify Full Build

- [ ] **Step 1: Verify Go backend compiles**

```bash
cd /c/Users/awu05/OneDrive/Documents/Github/seanime
go build ./...
```

Expected: No errors

- [ ] **Step 2: Verify frontend compiles (if node_modules available)**

```bash
cd seanime-web
npm run build
```

Expected: No errors

- [ ] **Step 3: Final commit**

```bash
git add -A
git commit -m "feat: Phase 1 complete — profiles and instance auth"
```
