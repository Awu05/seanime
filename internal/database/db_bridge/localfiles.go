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
		err = db.Gorm().Last(&res).Error
	}
	if err != nil {
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
	if profileID != "" {
		err = db.Gorm().Where("profile_id = ?", profileID).Last(&res).Error
	} else {
		err = db.Gorm().Last(&res).Error
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

	var existing models.ShelvedLocalFiles
	var dbID uint = 1
	if profileID != "" {
		findErr := db.Gorm().Where("profile_id = ?", profileID).First(&existing).Error
		if findErr == nil {
			dbID = existing.ID
		} else {
			dbID = 0
		}
	}

	_, err = db.UpsertShelvedLocalFiles(&models.ShelvedLocalFiles{
		BaseModel: models.BaseModel{ID: dbID},
		Value:     marshaledLfs,
		ProfileID: profileID,
	})
	return err
}
