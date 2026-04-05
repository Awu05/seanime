# Phase 2: Per-Profile AniList & Settings — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Each profile links their own AniList account and can override global settings. Admin sets global defaults, profiles override specific fields.

**Architecture:** Add `profile_id` FK to the `accounts` table so each profile has its own AniList token. Create a `ProfileSettings` model with a JSON `overrides` column for per-profile setting overrides. Settings resolution merges global defaults with profile overrides at read time. The existing singleton `AnilistClientRef` and `App.user` remain global but are swapped on each request based on the profile in the JWT context.

**Tech Stack:** Go (GORM, Echo), SQLite, JSON merge for settings inheritance

---

## File Structure

### Backend — New Files

| File | Responsibility |
|------|---------------|
| `internal/database/models/profile_settings.go` | ProfileSettings GORM model (profile_id FK, JSON overrides column) |
| `internal/database/db/profile_settings.go` | ProfileSettings CRUD operations |
| `internal/core/profile_context.go` | Helper to load AniList client + user for a profile from request context |
| `internal/handlers/profile_settings.go` | Endpoints: get/save profile settings overrides |

### Backend — Modified Files

| File | Change |
|------|--------|
| `internal/database/models/models.go` | Add `ProfileID` field to `Account` struct |
| `internal/database/db/account.go` | Add `GetAccountByProfileID`, `UpsertAccountForProfile` methods |
| `internal/database/db/db.go` | Add `ProfileSettings` to auto-migration |
| `internal/handlers/auth.go` | Modify `HandleLogin` to associate AniList token with current profile |
| `internal/handlers/routes.go` | Register new profile settings endpoints |
| `internal/core/anilist.go` | Add `GetUserForProfile`, `UpdateAnilistClientForProfile` methods |

---

## Tasks

### Task 1: Add ProfileID to Account Model

**Files:**
- Modify: `internal/database/models/models.go`

- [ ] **Step 1: Add ProfileID field to Account struct**

In `internal/database/models/models.go`, find the `Account` struct and add a `ProfileID` field:

```go
type Account struct {
	BaseModel
	Username  string `gorm:"column:username" json:"username"`
	Token     string `gorm:"column:token" json:"token"`
	Viewer    []byte `gorm:"column:viewer" json:"viewer"`
	ProfileID string `gorm:"column:profile_id;index" json:"profileId"`
}
```

- [ ] **Step 2: Verify build**

```bash
cd "c:\Users\awu05\OneDrive\Documents\Github\seanime"
go build ./internal/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/database/models/models.go
git commit -m "feat: add ProfileID to Account model"
```

---

### Task 2: Profile-Aware Account Operations

**Files:**
- Modify: `internal/database/db/account.go`

- [ ] **Step 1: Add profile-aware account methods**

Add these methods to `internal/database/db/account.go`:

```go
// GetAccountByProfileID returns the account linked to a specific profile.
func (db *Database) GetAccountByProfileID(profileID string) (*models.Account, error) {
	var acc models.Account
	err := db.gormdb.Where("profile_id = ?", profileID).First(&acc).Error
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

// UpsertAccountForProfile creates or updates the AniList account for a given profile.
func (db *Database) UpsertAccountForProfile(profileID string, username string, token string, viewer []byte) (*models.Account, error) {
	var acc models.Account
	err := db.gormdb.Where("profile_id = ?", profileID).First(&acc).Error

	if err != nil {
		// Create new account for this profile
		acc = models.Account{
			Username:  username,
			Token:     token,
			Viewer:    viewer,
			ProfileID: profileID,
		}
		err = db.gormdb.Create(&acc).Error
	} else {
		// Update existing account
		acc.Username = username
		acc.Token = token
		acc.Viewer = viewer
		err = db.gormdb.Save(&acc).Error
	}

	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to upsert account for profile")
		return nil, err
	}

	// Update legacy cache if this is the first account (backward compat)
	if accountCache != nil && accountCache.ID == acc.ID {
		accountCache = &acc
	}

	return &acc, nil
}

// GetAnilistTokenForProfile returns the AniList token for a specific profile.
func (db *Database) GetAnilistTokenForProfile(profileID string) string {
	acc, err := db.GetAccountByProfileID(profileID)
	if err != nil || acc.Token == "" {
		return ""
	}
	return acc.Token
}

// ClearAccountForProfile removes the AniList token and viewer data for a profile.
func (db *Database) ClearAccountForProfile(profileID string) error {
	return db.gormdb.Model(&models.Account{}).
		Where("profile_id = ?", profileID).
		Updates(map[string]interface{}{
			"username": "",
			"token":    "",
			"viewer":   nil,
		}).Error
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/database/db/account.go
git commit -m "feat: add profile-aware account operations"
```

---

### Task 3: ProfileSettings Model

**Files:**
- Create: `internal/database/models/profile_settings.go`

- [ ] **Step 1: Create ProfileSettings model**

Create `internal/database/models/profile_settings.go`:

```go
package models

// ProfileSettings stores per-profile setting overrides as a JSON blob.
// Fields not present in the overrides inherit from the global Settings (ID=1).
type ProfileSettings struct {
	UUIDBaseModel
	ProfileID string `gorm:"column:profile_id;uniqueIndex;not null" json:"profileId"`
	Overrides string `gorm:"column:overrides;type:text" json:"overrides"` // JSON blob
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/database/models/profile_settings.go
git commit -m "feat: add ProfileSettings model"
```

---

### Task 4: Register ProfileSettings in Migration

**Files:**
- Modify: `internal/database/db/db.go`

- [ ] **Step 1: Add ProfileSettings to migrateTables**

In `internal/database/db/db.go`, add `&models.ProfileSettings{}` to the `db.AutoMigrate(...)` call, after `&models.InstanceConfig{}`:

```go
		&models.InstanceConfig{},
		&models.ProfileSettings{},
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/database/db/db.go
git commit -m "feat: register ProfileSettings in auto-migration"
```

---

### Task 5: ProfileSettings Database Operations

**Files:**
- Create: `internal/database/db/profile_settings.go`

- [ ] **Step 1: Create ProfileSettings CRUD**

Create `internal/database/db/profile_settings.go`:

```go
package db

import (
	"seanime/internal/database/models"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

// GetProfileSettings returns the settings overrides for a specific profile.
func (db *Database) GetProfileSettings(profileID string) (*models.ProfileSettings, error) {
	var ps models.ProfileSettings
	err := db.gormdb.Where("profile_id = ?", profileID).First(&ps).Error
	if err != nil {
		return nil, err
	}
	return &ps, nil
}

// UpsertProfileSettings creates or updates the settings overrides for a profile.
func (db *Database) UpsertProfileSettings(profileID string, overrides string) (*models.ProfileSettings, error) {
	ps := &models.ProfileSettings{
		UUIDBaseModel: models.UUIDBaseModel{ID: uuid.New().String()},
		ProfileID:     profileID,
		Overrides:     overrides,
	}

	// Check if record exists
	var existing models.ProfileSettings
	err := db.gormdb.Where("profile_id = ?", profileID).First(&existing).Error
	if err == nil {
		// Update existing
		existing.Overrides = overrides
		err = db.gormdb.Save(&existing).Error
		if err != nil {
			db.Logger.Error().Err(err).Msg("db: Failed to update profile settings")
			return nil, err
		}
		return &existing, nil
	}

	// Create new
	err = db.gormdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "profile_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"overrides"}),
	}).Create(ps).Error
	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to create profile settings")
		return nil, err
	}
	return ps, nil
}

// DeleteProfileSettings removes the settings overrides for a profile.
func (db *Database) DeleteProfileSettings(profileID string) error {
	return db.gormdb.Where("profile_id = ?", profileID).Delete(&models.ProfileSettings{}).Error
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/database/db/profile_settings.go
git commit -m "feat: add ProfileSettings database operations"
```

---

### Task 6: Profile Context Helper

**Files:**
- Create: `internal/core/profile_context.go`

- [ ] **Step 1: Create profile context helper**

This file provides a helper that loads the correct AniList token and user data for a given profile ID. It doesn't swap global state — it returns the data for handlers to use.

Create `internal/core/profile_context.go`:

```go
package core

import (
	"seanime/internal/database/models"
	"seanime/internal/user"

	"github.com/goccy/go-json"
)

// ProfileContext holds the AniList-related data for a specific profile.
type ProfileContext struct {
	ProfileID string
	Account   *models.Account
	User      *user.User
	Token     string
}

// GetProfileContext loads the AniList account and user data for a given profile.
// Returns a context with a simulated user if the profile has no AniList account linked.
func (a *App) GetProfileContext(profileID string) *ProfileContext {
	if profileID == "" {
		// No profile = use current global user (backward compat / desktop sidecar)
		return &ProfileContext{
			User:  a.GetUser(),
			Token: a.GetUserAnilistToken(),
		}
	}

	acc, err := a.Database.GetAccountByProfileID(profileID)
	if err != nil || acc.Token == "" {
		return &ProfileContext{
			ProfileID: profileID,
			User:      user.NewSimulatedUser(),
		}
	}

	// Parse viewer data from account
	u, err := user.NewUser(acc)
	if err != nil {
		return &ProfileContext{
			ProfileID: profileID,
			Account:   acc,
			User:      user.NewSimulatedUser(),
			Token:     acc.Token,
		}
	}

	return &ProfileContext{
		ProfileID: profileID,
		Account:   acc,
		User:      u,
		Token:     acc.Token,
	}
}

// OverridableSettings are the settings fields that a profile can override.
// This is a subset of the full Settings struct — only personal preference fields.
type OverridableSettings struct {
	TorrentProvider        *string `json:"torrentProvider,omitempty"`
	HideAudienceScore      *bool   `json:"hideAudienceScore,omitempty"`
	EnableAdultContent     *bool   `json:"enableAdultContent,omitempty"`
	BlurAdultContent       *bool   `json:"blurAdultContent,omitempty"`
	AutoUpdateProgress     *bool   `json:"autoUpdateProgress,omitempty"`
	AutoPlayNextEpisode    *bool   `json:"autoPlayNextEpisode,omitempty"`
	EnableWatchContinuity  *bool   `json:"enableWatchContinuity,omitempty"`
	DisableAnimeCardTrailers *bool `json:"disableAnimeCardTrailers,omitempty"`
	EnableOnlinestream     *bool   `json:"enableOnlinestream,omitempty"`
	EnableManga            *bool   `json:"enableManga,omitempty"`
}

// GetMergedSettingsForProfile returns the global settings with profile overrides applied.
// If profileID is empty or has no overrides, returns the global settings unchanged.
func (a *App) GetMergedSettingsForProfile(profileID string) *models.Settings {
	settings := a.Settings
	if settings == nil || profileID == "" {
		return settings
	}

	ps, err := a.Database.GetProfileSettings(profileID)
	if err != nil || ps.Overrides == "" {
		return settings
	}

	// Parse overrides
	var overrides OverridableSettings
	if err := json.Unmarshal([]byte(ps.Overrides), &overrides); err != nil {
		a.Logger.Error().Err(err).Msg("app: Failed to parse profile settings overrides")
		return settings
	}

	// Deep copy settings to avoid mutating the global cache
	copied := *settings
	if copied.Library != nil {
		lib := *copied.Library
		copied.Library = &lib
	}
	if copied.Anilist != nil {
		ani := *copied.Anilist
		copied.Anilist = &ani
	}

	// Apply overrides
	if overrides.TorrentProvider != nil && copied.Library != nil {
		copied.Library.TorrentProvider = *overrides.TorrentProvider
	}
	if overrides.AutoUpdateProgress != nil && copied.Library != nil {
		copied.Library.AutoUpdateProgress = *overrides.AutoUpdateProgress
	}
	if overrides.AutoPlayNextEpisode != nil && copied.Library != nil {
		copied.Library.AutoPlayNextEpisode = *overrides.AutoPlayNextEpisode
	}
	if overrides.EnableWatchContinuity != nil && copied.Library != nil {
		copied.Library.EnableWatchContinuity = *overrides.EnableWatchContinuity
	}
	if overrides.DisableAnimeCardTrailers != nil && copied.Library != nil {
		copied.Library.DisableAnimeCardTrailers = *overrides.DisableAnimeCardTrailers
	}
	if overrides.EnableOnlinestream != nil && copied.Library != nil {
		copied.Library.EnableOnlinestream = *overrides.EnableOnlinestream
	}
	if overrides.EnableManga != nil && copied.Library != nil {
		copied.Library.EnableManga = *overrides.EnableManga
	}
	if overrides.HideAudienceScore != nil && copied.Anilist != nil {
		copied.Anilist.HideAudienceScore = *overrides.HideAudienceScore
	}
	if overrides.EnableAdultContent != nil && copied.Anilist != nil {
		copied.Anilist.EnableAdultContent = *overrides.EnableAdultContent
	}
	if overrides.BlurAdultContent != nil && copied.Anilist != nil {
		copied.Anilist.BlurAdultContent = *overrides.BlurAdultContent
	}

	return &copied
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/core/profile_context.go
git commit -m "feat: add profile context helper and settings merge logic"
```

---

### Task 7: Modify Login Handler for Per-Profile AniList

**Files:**
- Modify: `internal/handlers/auth.go`

- [ ] **Step 1: Update HandleLogin to associate AniList token with current profile**

The current `HandleLogin` saves the AniList token to `Account{ID: 1}`. Modify it to also save with the profile ID from the auth context.

Find the `UpsertAccount` call in `HandleLogin` (around line 56-64) and change it to use the profile-aware method. The key change: get the profile ID from the request context and call `UpsertAccountForProfile`.

Replace the `UpsertAccount` block:

```go
	// Get the profile ID from auth context
	profileID := core.GetProfileIDFromContext(c)

	// Save account data for this profile
	_, err = h.App.Database.UpsertAccountForProfile(
		profileID,
		getViewer.Viewer.Name,
		b.Token,
		bytes,
	)
```

Also keep the legacy `UpsertAccount` call with `ID: 1` for backward compatibility when no profile is active (desktop sidecar single-user mode):

```go
	if profileID == "" {
		// Legacy single-user mode
		_, err = h.App.Database.UpsertAccount(&models.Account{
			BaseModel: models.BaseModel{
				ID:        1,
				UpdatedAt: time.Now(),
			},
			Username: getViewer.Viewer.Name,
			Token:    b.Token,
			Viewer:   bytes,
		})
	} else {
		// Multi-user: save token for this profile
		_, err = h.App.Database.UpsertAccountForProfile(
			profileID,
			getViewer.Viewer.Name,
			b.Token,
			bytes,
		)
	}
```

- [ ] **Step 2: Update HandleLogout to be profile-aware**

In `HandleLogout`, instead of calling `h.App.LogoutFromAnilist()` (which clears Account ID=1), check for a profile and clear that profile's account:

```go
func (h *Handler) HandleLogout(c echo.Context) error {
	profileID := core.GetProfileIDFromContext(c)

	if profileID != "" {
		_ = h.App.Database.ClearAccountForProfile(profileID)
	} else {
		h.App.LogoutFromAnilist()
	}

	status := h.NewStatus(c)
	return h.RespondWithData(c, status)
}
```

- [ ] **Step 3: Verify build**

```bash
go build ./internal/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/handlers/auth.go
git commit -m "feat: make AniList login/logout profile-aware"
```

---

### Task 8: Profile Settings Handler Endpoints

**Files:**
- Create: `internal/handlers/profile_settings.go`

- [ ] **Step 1: Create profile settings endpoints**

Create `internal/handlers/profile_settings.go`:

```go
package handlers

import (
	"seanime/internal/core"

	"github.com/labstack/echo/v4"
)

// HandleGetProfileSettings
//
//	@summary returns the settings overrides for the current profile.
//	@route /api/v1/profile-settings [GET]
//	@returns map[string]interface{}
func (h *Handler) HandleGetProfileSettings(c echo.Context) error {
	profileID := core.GetProfileIDFromContext(c)
	if profileID == "" {
		return h.RespondWithData(c, map[string]interface{}{
			"overrides": "{}",
			"merged":    h.App.Settings,
		})
	}

	ps, err := h.App.Database.GetProfileSettings(profileID)
	overrides := "{}"
	if err == nil && ps != nil {
		overrides = ps.Overrides
	}

	merged := h.App.GetMergedSettingsForProfile(profileID)

	return h.RespondWithData(c, map[string]interface{}{
		"overrides": overrides,
		"merged":    merged,
	})
}

// HandleSaveProfileSettings
//
//	@summary saves the settings overrides for the current profile.
//	@route /api/v1/profile-settings [POST]
//	@returns map[string]interface{}
func (h *Handler) HandleSaveProfileSettings(c echo.Context) error {
	profileID := core.GetProfileIDFromContext(c)
	if profileID == "" {
		return h.RespondWithError(c, echo.ErrForbidden)
	}

	type body struct {
		Overrides string `json:"overrides"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	_, err := h.App.Database.UpsertProfileSettings(profileID, b.Overrides)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	return h.RespondWithData(c, map[string]interface{}{"success": true})
}

// HandleGetMergedSettings
//
//	@summary returns the global settings merged with the current profile's overrides.
//	@route /api/v1/profile-settings/merged [GET]
//	@returns *models.Settings
func (h *Handler) HandleGetMergedSettings(c echo.Context) error {
	profileID := core.GetProfileIDFromContext(c)
	merged := h.App.GetMergedSettingsForProfile(profileID)
	return h.RespondWithData(c, merged)
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/handlers/profile_settings.go
git commit -m "feat: add profile settings handler endpoints"
```

---

### Task 9: Register Profile Settings Routes

**Files:**
- Modify: `internal/handlers/routes.go`

- [ ] **Step 1: Register profile settings routes**

In `internal/handlers/routes.go`, add these routes after the admin profile management routes:

```go
	// Profile settings
	v1.GET("/profile-settings", h.HandleGetProfileSettings)
	v1.POST("/profile-settings", h.HandleSaveProfileSettings)
	v1.GET("/profile-settings/merged", h.HandleGetMergedSettings)
```

- [ ] **Step 2: Verify full build**

```bash
go build ./internal/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/handlers/routes.go
git commit -m "feat: register profile settings routes"
```

---

### Task 10: Verify Full Build

- [ ] **Step 1: Verify Go backend compiles**

```bash
cd "c:\Users\awu05\OneDrive\Documents\Github\seanime"
go build ./internal/...
```

Expected: No errors

- [ ] **Step 2: Commit any remaining changes**

```bash
git add -A
git commit -m "feat: Phase 2 complete — per-profile AniList and settings"
```
