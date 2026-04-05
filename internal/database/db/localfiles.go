package db

import (
	"seanime/internal/database/models"

	"gorm.io/gorm/clause"
)

// TrimLocalFileEntries will trim the local file entries if there are more than 10 entries.
func (db *Database) TrimLocalFileEntries() {
	db.cleanupManager.trimLocalFileEntries()
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

func (db *Database) UpsertLocalFiles(lfs *models.LocalFiles) (*models.LocalFiles, error) {
	err := db.gormdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		UpdateAll: true,
	}).Create(lfs).Error

	if err != nil {
		return nil, err
	}
	return lfs, nil
}

func (db *Database) InsertLocalFiles(lfs *models.LocalFiles) (*models.LocalFiles, error) {
	err := db.gormdb.Create(lfs).Error

	if err != nil {
		return nil, err
	}
	return lfs, nil
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

func (db *Database) UpsertShelvedLocalFiles(lfs *models.ShelvedLocalFiles) (*models.ShelvedLocalFiles, error) {
	err := db.gormdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		UpdateAll: true,
	}).Create(lfs).Error

	if err != nil {
		return nil, err
	}
	return lfs, nil
}

func (db *Database) GetLocalFilesByProfileID(profileID string) (*models.LocalFiles, error) {
	var lfs models.LocalFiles
	err := db.gormdb.Where("profile_id = ?", profileID).Last(&lfs).Error
	if err != nil {
		return nil, err
	}
	return &lfs, nil
}

func (db *Database) InsertLocalFilesForProfile(lfs *models.LocalFiles, profileID string) (*models.LocalFiles, error) {
	lfs.ProfileID = profileID
	err := db.gormdb.Create(lfs).Error
	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to insert local files for profile")
		return nil, err
	}
	return lfs, nil
}

func (db *Database) GetShelvedLocalFilesByProfileID(profileID string) (*models.ShelvedLocalFiles, error) {
	var lfs models.ShelvedLocalFiles
	err := db.gormdb.Where("profile_id = ?", profileID).Last(&lfs).Error
	if err != nil {
		return nil, err
	}
	return &lfs, nil
}

func (db *Database) UpsertShelvedLocalFilesForProfile(lfs *models.ShelvedLocalFiles, profileID string) (*models.ShelvedLocalFiles, error) {
	lfs.ProfileID = profileID
	var existing models.ShelvedLocalFiles
	err := db.gormdb.Where("profile_id = ?", profileID).First(&existing).Error
	if err != nil {
		err = db.gormdb.Create(lfs).Error
	} else {
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
