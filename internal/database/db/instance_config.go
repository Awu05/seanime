package db

import (
	"seanime/internal/database/models"

	"gorm.io/gorm/clause"
)

func (db *Database) GetInstanceConfig() (*models.InstanceConfig, error) {
	var config models.InstanceConfig
	err := db.gormdb.Where("id = ?", "1").First(&config).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (db *Database) UpsertInstanceConfig(config *models.InstanceConfig) (*models.InstanceConfig, error) {
	config.ID = "1"
	err := db.gormdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		UpdateAll: true,
	}).Create(config).Error
	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to upsert instance config")
		return nil, err
	}
	return config, nil
}

func (db *Database) HasAccessCode() (bool, error) {
	config, err := db.GetInstanceConfig()
	if err != nil {
		return false, nil
	}
	return config.AccessCodeHash != "", nil
}
