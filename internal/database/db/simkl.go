package db

import (
	"errors"
	"seanime/internal/database/models"
	"time"

	"gorm.io/gorm"
)

func (db *Database) GetSimklAccount(profileID string) (*models.SimklAccount, error) {
	var res models.SimklAccount
	err := db.gormdb.Where("profile_id = ?", profileID).First(&res).Error
	if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("SIMKL not connected")
	} else if err != nil {
		return nil, err
	}
	return &res, nil
}

func (db *Database) UpsertSimklAccount(account *models.SimklAccount) (*models.SimklAccount, error) {
	var existing models.SimklAccount
	err := db.gormdb.Where("profile_id = ?", account.ProfileID).First(&existing).Error
	if err == nil {
		account.BaseModel = existing.BaseModel
		if err := db.gormdb.Save(account).Error; err != nil {
			return nil, err
		}
		return account, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := db.gormdb.Create(account).Error; err != nil {
		return nil, err
	}
	return account, nil
}

func (db *Database) DeleteSimklAccount(profileID string) error {
	return db.gormdb.Where("profile_id = ?", profileID).Delete(&models.SimklAccount{}).Error
}

func (db *Database) GetSimklSettings(profileID string) (*models.SimklSettings, error) {
	var res models.SimklSettings
	err := db.gormdb.Where("profile_id = ?", profileID).First(&res).Error
	if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
		return &models.SimklSettings{ProfileID: profileID, Enabled: false}, nil
	} else if err != nil {
		return nil, err
	}
	return &res, nil
}

func (db *Database) UpsertSimklSettings(settings *models.SimklSettings) (*models.SimklSettings, error) {
	var existing models.SimklSettings
	err := db.gormdb.Where("profile_id = ?", settings.ProfileID).First(&existing).Error
	if err == nil {
		settings.BaseModel = existing.BaseModel
		if err := db.gormdb.Save(settings).Error; err != nil {
			return nil, err
		}
		return settings, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := db.gormdb.Create(settings).Error; err != nil {
		return nil, err
	}
	return settings, nil
}

func (db *Database) EnqueuePendingSync(item *models.PendingSync) error {
	return db.gormdb.Create(item).Error
}

func (db *Database) GetDuePendingSyncs(target string, limit int) ([]*models.PendingSync, error) {
	var res []*models.PendingSync
	err := db.gormdb.Where("target = ? AND next_attempt_at <= ?", target, time.Now()).
		Order("created_at asc").
		Limit(limit).
		Find(&res).Error
	return res, err
}

func (db *Database) IncrementPendingSyncAttempt(id uint, nextAttemptAt time.Time, lastErr string) error {
	return db.gormdb.Model(&models.PendingSync{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"attempts":        gorm.Expr("attempts + 1"),
			"next_attempt_at": nextAttemptAt,
			"last_error":      lastErr,
		}).Error
}

func (db *Database) DeletePendingSync(id uint) error {
	return db.gormdb.Delete(&models.PendingSync{}, id).Error
}
