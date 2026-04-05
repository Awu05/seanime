# Phase 3: Per-Profile Libraries — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Support shared and per-profile library paths. Each profile sees files from shared libraries plus their own private libraries. Scans are scoped to the requesting profile's accessible paths.

**Architecture:** New `LibraryPath` model replaces the single `LibraryPath`/`LibraryPaths` fields in Settings. Each library path has an optional `owner_id` (profile FK) and a `shared` flag. The existing `LocalFiles` model gets a `ProfileID` field so each profile stores its own scanned file set. The scanner reads paths from the `library_paths` table instead of Settings. The watcher monitors the union of all library paths across all profiles.

**Tech Stack:** Go (GORM, Echo), SQLite

---

## File Structure

### Backend — New Files

| File | Responsibility |
|------|---------------|
| `internal/database/models/library_path.go` | LibraryPath GORM model (UUID PK, path, owner_id FK, shared flag) |
| `internal/database/db/library_path.go` | LibraryPath CRUD operations |
| `internal/handlers/library_paths.go` | Library path management endpoints (admin + member) |

### Backend — Modified Files

| File | Change |
|------|--------|
| `internal/database/models/models.go` | Add `ProfileID` field to `LocalFiles` and `ShelvedLocalFiles` structs |
| `internal/database/db/db.go` | Add `LibraryPath` to auto-migration |
| `internal/database/db/localfiles.go` | Add profile-aware local file operations |
| `internal/database/db_bridge/localfiles.go` | Add profile-scoped bridge functions |
| `internal/handlers/scan.go` | Modify scan handler to use profile's library paths |
| `internal/handlers/routes.go` | Register library path management routes |
| `internal/core/modules.go` | Update watcher initialization to use all library paths from DB |

---

## Tasks

### Task 1: LibraryPath Model

**Files:**
- Create: `internal/database/models/library_path.go`

- [ ] **Step 1: Create LibraryPath model**

Create `internal/database/models/library_path.go`:

```go
package models

// LibraryPath represents a media library directory that can be global (shared) or owned by a profile.
// Global paths (OwnerID empty) are accessible to all profiles.
// Per-profile paths are accessible only to their owner (unless Shared is true).
type LibraryPath struct {
	UUIDBaseModel
	Path    string `gorm:"column:path;not null" json:"path"`
	OwnerID string `gorm:"column:owner_id;index" json:"ownerId"`   // FK to profiles.id, empty = global
	Shared  bool   `gorm:"column:shared;default:true" json:"shared"` // visible to all profiles
}
```

- [ ] **Step 2: Register in auto-migration**

In `internal/database/db/db.go`, add `&models.LibraryPath{}` to the `db.AutoMigrate(...)` call, after `&models.ProfileSettings{}`:

```go
		&models.ProfileSettings{},
		&models.LibraryPath{},
```

- [ ] **Step 3: Verify build**

```bash
go build ./internal/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/database/models/library_path.go internal/database/db/db.go
git commit -m "feat: add LibraryPath model and register in migration"
```

---

### Task 2: LibraryPath Database Operations

**Files:**
- Create: `internal/database/db/library_path.go`

- [ ] **Step 1: Create LibraryPath CRUD**

Create `internal/database/db/library_path.go`:

```go
package db

import (
	"seanime/internal/database/models"

	"github.com/google/uuid"
)

// CreateLibraryPath adds a new library path entry.
func (db *Database) CreateLibraryPath(lp *models.LibraryPath) (*models.LibraryPath, error) {
	if lp.ID == "" {
		lp.ID = uuid.New().String()
	}
	err := db.gormdb.Create(lp).Error
	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to create library path")
		return nil, err
	}
	return lp, nil
}

// GetAllLibraryPaths returns all library path entries.
func (db *Database) GetAllLibraryPaths() ([]*models.LibraryPath, error) {
	var paths []*models.LibraryPath
	err := db.gormdb.Order("created_at ASC").Find(&paths).Error
	if err != nil {
		return nil, err
	}
	return paths, nil
}

// GetLibraryPathsForProfile returns all paths accessible to a given profile:
// global paths (owner_id is empty) + shared paths from other profiles + the profile's own paths.
func (db *Database) GetLibraryPathsForProfile(profileID string) ([]*models.LibraryPath, error) {
	var paths []*models.LibraryPath
	err := db.gormdb.Where(
		"owner_id = '' OR owner_id IS NULL OR shared = ? OR owner_id = ?",
		true, profileID,
	).Order("created_at ASC").Find(&paths).Error
	if err != nil {
		return nil, err
	}
	return paths, nil
}

// GetLibraryPathStringsForProfile returns just the path strings accessible to a profile.
func (db *Database) GetLibraryPathStringsForProfile(profileID string) ([]string, error) {
	paths, err := db.GetLibraryPathsForProfile(profileID)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(paths))
	for _, p := range paths {
		if p.Path != "" {
			result = append(result, p.Path)
		}
	}
	return result, nil
}

// GetAllLibraryPathStrings returns all unique path strings across all profiles (for watcher).
func (db *Database) GetAllLibraryPathStrings() ([]string, error) {
	paths, err := db.GetAllLibraryPaths()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	result := make([]string, 0, len(paths))
	for _, p := range paths {
		if p.Path != "" && !seen[p.Path] {
			seen[p.Path] = true
			result = append(result, p.Path)
		}
	}
	return result, nil
}

// DeleteLibraryPath removes a library path entry by ID.
func (db *Database) DeleteLibraryPath(id string) error {
	err := db.gormdb.Where("id = ?", id).Delete(&models.LibraryPath{}).Error
	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to delete library path")
	}
	return err
}

// GetLibraryPathByID returns a library path entry by ID.
func (db *Database) GetLibraryPathByID(id string) (*models.LibraryPath, error) {
	var lp models.LibraryPath
	err := db.gormdb.Where("id = ?", id).First(&lp).Error
	if err != nil {
		return nil, err
	}
	return &lp, nil
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/database/db/library_path.go
git commit -m "feat: add LibraryPath database operations"
```

---

### Task 3: Add ProfileID to LocalFiles

**Files:**
- Modify: `internal/database/models/models.go`

- [ ] **Step 1: Add ProfileID field to LocalFiles and ShelvedLocalFiles**

In `internal/database/models/models.go`, find `LocalFiles` struct and add `ProfileID`:

```go
type LocalFiles struct {
	BaseModel
	Value     []byte `gorm:"column:value" json:"value"`
	ProfileID string `gorm:"column:profile_id;index" json:"profileId"`
}
```

Also find `ShelvedLocalFiles` and add `ProfileID`:

```go
type ShelvedLocalFiles struct {
	BaseModel
	Value     []byte `gorm:"column:value" json:"value"`
	ProfileID string `gorm:"column:profile_id;index" json:"profileId"`
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/database/models/models.go
git commit -m "feat: add ProfileID to LocalFiles and ShelvedLocalFiles"
```

---

### Task 4: Profile-Aware LocalFiles Operations

**Files:**
- Modify: `internal/database/db/localfiles.go`

- [ ] **Step 1: Add profile-scoped local file methods**

Read `internal/database/db/localfiles.go` first to understand the existing pattern. Then append these profile-aware methods:

```go
// GetLocalFilesByProfileID returns the most recent local files entry for a profile.
func (db *Database) GetLocalFilesByProfileID(profileID string) (*models.LocalFiles, error) {
	var lfs models.LocalFiles
	err := db.gormdb.Where("profile_id = ?", profileID).Last(&lfs).Error
	if err != nil {
		return nil, err
	}
	return &lfs, nil
}

// InsertLocalFilesForProfile creates a new local files entry for a specific profile.
func (db *Database) InsertLocalFilesForProfile(lfs *models.LocalFiles, profileID string) (*models.LocalFiles, error) {
	lfs.ProfileID = profileID
	err := db.gormdb.Create(lfs).Error
	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to insert local files for profile")
		return nil, err
	}
	return lfs, nil
}

// GetShelvedLocalFilesByProfileID returns shelved local files for a profile.
func (db *Database) GetShelvedLocalFilesByProfileID(profileID string) (*models.ShelvedLocalFiles, error) {
	var lfs models.ShelvedLocalFiles
	err := db.gormdb.Where("profile_id = ?", profileID).Last(&lfs).Error
	if err != nil {
		return nil, err
	}
	return &lfs, nil
}

// UpsertShelvedLocalFilesForProfile creates or updates shelved local files for a profile.
func (db *Database) UpsertShelvedLocalFilesForProfile(lfs *models.ShelvedLocalFiles, profileID string) (*models.ShelvedLocalFiles, error) {
	lfs.ProfileID = profileID
	var existing models.ShelvedLocalFiles
	err := db.gormdb.Where("profile_id = ?", profileID).First(&existing).Error
	if err != nil {
		// Create new
		err = db.gormdb.Create(lfs).Error
	} else {
		// Update existing
		existing.Value = lfs.Value
		err = db.gormdb.Save(&existing).Error
		lfs = &existing
	}
	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to upsert shelved local files for profile")
		return nil, err
	}
	return lfs, nil
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/database/db/localfiles.go
git commit -m "feat: add profile-aware local file operations"
```

---

### Task 5: Library Path Management Endpoints

**Files:**
- Create: `internal/handlers/library_paths.go`

- [ ] **Step 1: Create library path handler endpoints**

Create `internal/handlers/library_paths.go`:

```go
package handlers

import (
	"net/http"
	"seanime/internal/core"
	"seanime/internal/database/models"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// HandleGetLibraryPaths
//
//	@summary returns library paths accessible to the current profile.
//	@desc Admin sees all paths. Members see global + shared + their own.
//	@route /api/v1/library-paths [GET]
//	@returns []*models.LibraryPath
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

// HandleAddLibraryPath
//
//	@summary adds a new library path.
//	@desc Admin can create global paths. Members can only create paths owned by them.
//	@route /api/v1/library-paths [POST]
//	@returns *models.LibraryPath
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

	// Non-admin can only create paths owned by themselves
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

	// Refresh watcher to include new path
	h.App.InitOrRefreshModules()

	return h.RespondWithData(c, lp)
}

// HandleDeleteLibraryPath
//
//	@summary deletes a library path.
//	@desc Admin can delete any path. Members can only delete their own.
//	@route /api/v1/library-paths/:id [DELETE]
//	@returns map[string]interface{}
func (h *Handler) HandleDeleteLibraryPath(c echo.Context) error {
	profileID := core.GetProfileIDFromContext(c)
	isAdmin := core.GetIsAdminFromContext(c)
	id := c.Param("id")

	// Check ownership
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

	// Refresh watcher
	h.App.InitOrRefreshModules()

	return h.RespondWithData(c, map[string]interface{}{"success": true})
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/handlers/library_paths.go
git commit -m "feat: add library path management endpoints"
```

---

### Task 6: Register Library Path Routes

**Files:**
- Modify: `internal/handlers/routes.go`

- [ ] **Step 1: Register library path routes**

In `internal/handlers/routes.go`, find where the profile settings routes were added and add after them:

```go
	// Library path management
	v1.GET("/library-paths", h.HandleGetLibraryPaths)
	v1.POST("/library-paths", h.HandleAddLibraryPath)
	v1.DELETE("/library-paths/:id", h.HandleDeleteLibraryPath)
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/handlers/routes.go
git commit -m "feat: register library path management routes"
```

---

### Task 7: Update Watcher to Use Library Paths Table

**Files:**
- Modify: `internal/core/modules.go`

- [ ] **Step 1: Update watcher initialization**

In `internal/core/modules.go`, find the library watcher initialization block (look for `initLibraryWatcher`). It currently reads paths from settings:

```go
if settings.Library != nil && len(settings.Library.LibraryPath) > 0 && settings.Library.AutoScan {
    go a.initLibraryWatcher(settings.Library.GetLibraryPaths())
}
```

Change it to also include paths from the `library_paths` table:

```go
	// Initialize library watcher with paths from both settings and library_paths table
	if settings.Library != nil && settings.Library.AutoScan {
		watchPaths := settings.Library.GetLibraryPaths()

		// Also include paths from the library_paths table
		dbPaths, err := a.Database.GetAllLibraryPathStrings()
		if err == nil && len(dbPaths) > 0 {
			// Merge and deduplicate
			seen := make(map[string]bool)
			for _, p := range watchPaths {
				seen[p] = true
			}
			for _, p := range dbPaths {
				if !seen[p] {
					watchPaths = append(watchPaths, p)
					seen[p] = true
				}
			}
		}

		if len(watchPaths) > 0 {
			go a.initLibraryWatcher(watchPaths)
		}
	}
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/core/modules.go
git commit -m "feat: include library_paths table entries in watcher"
```

---

### Task 8: Update Scan Handler for Profile Context

**Files:**
- Modify: `internal/handlers/scan.go`

- [ ] **Step 1: Make scan handler profile-aware**

Read `internal/handlers/scan.go` first. Find `HandleScanLocalFiles`. Currently it reads paths from settings and saves results globally.

Modify it to also consider paths from the `library_paths` table for the current profile. The scan should use the union of:
1. Global settings paths (existing behavior for backward compat)
2. Paths from `library_paths` table accessible to the current profile

Find where `libraryPath` and `additionalLibraryPaths` are read from settings (near the top of the function). After those lines, add:

```go
	// Include paths from library_paths table for this profile
	profileID := core.GetProfileIDFromContext(c)
	if profileID != "" {
		dbPaths, err := h.App.Database.GetLibraryPathStringsForProfile(profileID)
		if err == nil {
			for _, p := range dbPaths {
				if p != libraryPath {
					additionalLibraryPaths = append(additionalLibraryPaths, p)
				}
			}
		}
	}
```

Also make sure `"seanime/internal/core"` is imported.

- [ ] **Step 2: Verify build**

```bash
go build ./internal/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/handlers/scan.go
git commit -m "feat: include profile library paths in scan"
```

---

### Task 9: Verify Full Build

- [ ] **Step 1: Verify Go backend compiles**

```bash
cd "c:\Users\awu05\OneDrive\Documents\Github\seanime"
go build ./internal/...
```

Expected: No errors
