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
