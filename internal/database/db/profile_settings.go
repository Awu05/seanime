package db

import (
	"seanime/internal/database/models"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

func (db *Database) GetProfileSettings(profileID string) (*models.ProfileSettings, error) {
	var ps models.ProfileSettings
	err := db.gormdb.Where("profile_id = ?", profileID).First(&ps).Error
	if err != nil {
		return nil, err
	}
	return &ps, nil
}

func (db *Database) UpsertProfileSettings(profileID string, overrides string) (*models.ProfileSettings, error) {
	ps := &models.ProfileSettings{
		UUIDBaseModel: models.UUIDBaseModel{ID: uuid.New().String()},
		ProfileID:     profileID,
		Overrides:     overrides,
	}

	var existing models.ProfileSettings
	err := db.gormdb.Where("profile_id = ?", profileID).First(&existing).Error
	if err == nil {
		existing.Overrides = overrides
		err = db.gormdb.Save(&existing).Error
		if err != nil {
			db.Logger.Error().Err(err).Msg("db: Failed to update profile settings")
			return nil, err
		}
		return &existing, nil
	}

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

func (db *Database) DeleteProfileSettings(profileID string) error {
	return db.gormdb.Where("profile_id = ?", profileID).Delete(&models.ProfileSettings{}).Error
}
