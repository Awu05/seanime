package db

import (
	"seanime/internal/database/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
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
	profile.HasPin = profile.PinHash != ""
	return &profile, nil
}


func (db *Database) GetAllProfiles() ([]*models.Profile, error) {
	var profiles []*models.Profile
	err := db.gormdb.Order("created_at ASC").Find(&profiles).Error
	if err != nil {
		return nil, err
	}
	for _, p := range profiles {
		p.HasPin = p.PinHash != ""
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

// DeleteProfileAndData deletes a profile and every row it owns in one
// transaction: settings, local files, shelved files, the AniList account
// (token), and its library paths. Partial deletion would leave orphaned
// tokens and paths behind, so all-or-nothing.
func (db *Database) DeleteProfileAndData(id string) error {
	err := db.gormdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", id).Delete(&models.Profile{}).Error; err != nil {
			return err
		}
		for _, model := range []interface{}{
			&models.LocalFiles{},
			&models.ShelvedLocalFiles{},
			&models.Settings{},
			&models.MediastreamSettings{},
			&models.TorrentstreamSettings{},
			&models.DebridSettings{},
			&models.ProfileSettings{},
			&models.Account{},
		} {
			if err := tx.Where("profile_id = ?", id).Delete(model).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("owner_id = ?", id).Delete(&models.LibraryPath{}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		db.Logger.Error().Err(err).Str("profileId", id).Msg("db: Failed to delete profile and its data")
	}
	return err
}

func (db *Database) CountProfiles() (int64, error) {
	var count int64
	err := db.gormdb.Model(&models.Profile{}).Count(&count).Error
	return count, err
}
