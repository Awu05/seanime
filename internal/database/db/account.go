package db

import (
	"errors"
	"seanime/internal/database/models"

	"gorm.io/gorm/clause"
)

var accountCache *models.Account

func (db *Database) UpsertAccount(acc *models.Account) (*models.Account, error) {
	err := db.gormdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		UpdateAll: true,
	}).Create(acc).Error

	if err != nil {
		db.Logger.Error().Err(err).Msg("Failed to save account in the database")
		return nil, err
	}

	if acc.Username != "" {
		accountCache = acc
	} else {
		accountCache = nil
	}

	return acc, nil
}

func (db *Database) GetAccount() (*models.Account, error) {

	if accountCache != nil {
		return accountCache, nil
	}

	var acc models.Account
	err := db.gormdb.Last(&acc).Error
	if err != nil {
		return nil, err
	}
	if acc.Username == "" || acc.Token == "" || acc.Viewer == nil {
		return nil, errors.New("account not found")
	}

	accountCache = &acc

	return &acc, err
}

// GetAnilistToken retrieves the AniList token from the account or returns an empty string
func (db *Database) GetAnilistToken() string {
	acc, err := db.GetAccount()
	if err != nil {
		return ""
	}
	return acc.Token
}

func (db *Database) GetAccountByProfileID(profileID string) (*models.Account, error) {
	var acc models.Account
	err := db.gormdb.Where("profile_id = ?", profileID).First(&acc).Error
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

func (db *Database) UpsertAccountForProfile(profileID string, username string, token string, viewer []byte) (*models.Account, error) {
	var acc models.Account
	err := db.gormdb.Where("profile_id = ?", profileID).First(&acc).Error

	if err != nil {
		acc = models.Account{
			Username:  username,
			Token:     token,
			Viewer:    viewer,
			ProfileID: profileID,
		}
		err = db.gormdb.Create(&acc).Error
	} else {
		acc.Username = username
		acc.Token = token
		acc.Viewer = viewer
		err = db.gormdb.Save(&acc).Error
	}

	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to upsert account for profile")
		return nil, err
	}

	if accountCache != nil && accountCache.ID == acc.ID {
		accountCache = &acc
	}

	return &acc, nil
}

func (db *Database) GetAnilistTokenForProfile(profileID string) string {
	acc, err := db.GetAccountByProfileID(profileID)
	if err != nil || acc.Token == "" {
		return ""
	}
	return acc.Token
}

func (db *Database) ClearAccountForProfile(profileID string) error {
	return db.gormdb.Model(&models.Account{}).
		Where("profile_id = ?", profileID).
		Updates(map[string]interface{}{
			"username": "",
			"token":    "",
			"viewer":   nil,
		}).Error
}
