package db_bridge

import (
	"errors"
	"seanime/internal/database/db"
	"seanime/internal/database/models"
	"seanime/internal/library/anime"
	"sync"

	"github.com/goccy/go-json"
	"gorm.io/gorm"
)

var (
	localFilesCache   = make(map[string]cachedLocalFiles)
	localFilesCacheMu sync.RWMutex
)

type cachedLocalFiles struct {
	files []*anime.LocalFile
	dbID  uint
}

func ClearAllLocalFilesCache() {
	localFilesCacheMu.Lock()
	localFilesCache = make(map[string]cachedLocalFiles)
	localFilesCacheMu.Unlock()
}

func GetLocalFiles(db *db.Database, profileID string) ([]*anime.LocalFile, uint, error) {
	localFilesCacheMu.RLock()
	if cached, ok := localFilesCache[profileID]; ok {
		localFilesCacheMu.RUnlock()
		return cached.files, cached.dbID, nil
	}
	localFilesCacheMu.RUnlock()

	var res models.LocalFiles
	var err error
	if profileID != "" {
		err = db.Gorm().Where("profile_id = ?", profileID).Last(&res).Error
	} else {
		// Empty profileID means the legacy/unowned bucket — never fall back to
		// another profile's rows (that would leak one profile's library to another).
		err = db.Gorm().Where("profile_id = '' OR profile_id IS NULL").Last(&res).Error
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			empty := make([]*anime.LocalFile, 0)
			localFilesCacheMu.Lock()
			localFilesCache[profileID] = cachedLocalFiles{files: empty, dbID: 0}
			localFilesCacheMu.Unlock()
			return empty, 0, nil
		}
		return nil, 0, err
	}

	var lfs []*anime.LocalFile
	if err := json.Unmarshal(res.Value, &lfs); err != nil {
		return nil, 0, err
	}

	db.Logger.Debug().Str("profileId", profileID).Msg("db: Local files retrieved")

	localFilesCacheMu.Lock()
	localFilesCache[profileID] = cachedLocalFiles{files: lfs, dbID: res.ID}
	localFilesCacheMu.Unlock()

	return lfs, res.ID, nil
}

func SaveLocalFiles(db *db.Database, profileID string, lfsId uint, lfs []*anime.LocalFile) ([]*anime.LocalFile, error) {
	marshaledLfs, err := json.Marshal(lfs)
	if err != nil {
		return nil, err
	}

	ret, err := db.UpsertLocalFiles(&models.LocalFiles{
		BaseModel: models.BaseModel{ID: lfsId},
		Value:     marshaledLfs,
		ProfileID: profileID,
	})
	if err != nil {
		return nil, err
	}

	var retLfs []*anime.LocalFile
	if err := json.Unmarshal(ret.Value, &retLfs); err != nil {
		return lfs, nil
	}

	localFilesCacheMu.Lock()
	localFilesCache[profileID] = cachedLocalFiles{files: retLfs, dbID: ret.ID}
	localFilesCacheMu.Unlock()

	return retLfs, nil
}

func InsertLocalFiles(db *db.Database, profileID string, lfs []*anime.LocalFile) ([]*anime.LocalFile, error) {
	bytes, err := json.Marshal(lfs)
	if err != nil {
		return nil, err
	}

	ret, err := db.InsertLocalFiles(&models.LocalFiles{
		Value:     bytes,
		ProfileID: profileID,
	})
	if err != nil {
		return nil, err
	}

	localFilesCacheMu.Lock()
	localFilesCache[profileID] = cachedLocalFiles{files: lfs, dbID: ret.ID}
	localFilesCacheMu.Unlock()

	return lfs, nil
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

func GetShelvedLocalFiles(db *db.Database, profileID string) ([]*anime.LocalFile, error) {
	var res models.ShelvedLocalFiles
	var err error
	// First (lowest ID) to match the row SaveShelvedLocalFiles targets
	if profileID != "" {
		err = db.Gorm().Where("profile_id = ?", profileID).First(&res).Error
	} else {
		err = db.Gorm().Where("profile_id = '' OR profile_id IS NULL").First(&res).Error
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	var lfs []*anime.LocalFile
	if err := json.Unmarshal(res.Value, &lfs); err != nil {
		return nil, err
	}

	db.Logger.Debug().Msg("db: Shelved local files retrieved")
	return lfs, nil
}

func SaveShelvedLocalFiles(db *db.Database, profileID string, lfs []*anime.LocalFile) error {
	marshaledLfs, err := json.Marshal(lfs)
	if err != nil {
		return err
	}

	// Look up the row belonging to this profile (or the legacy/unowned bucket for "").
	// Never assume a fixed row ID: an UpdateAll upsert against someone else's row
	// would overwrite their shelved files and reassign the row.
	var existing models.ShelvedLocalFiles
	var dbID uint = 0
	query := db.Gorm()
	if profileID != "" {
		query = query.Where("profile_id = ?", profileID)
	} else {
		query = query.Where("profile_id = '' OR profile_id IS NULL")
	}
	if findErr := query.First(&existing).Error; findErr == nil {
		dbID = existing.ID
	}

	_, err = db.UpsertShelvedLocalFiles(&models.ShelvedLocalFiles{
		BaseModel: models.BaseModel{ID: dbID},
		Value:     marshaledLfs,
		ProfileID: profileID,
	})
	return err
}
