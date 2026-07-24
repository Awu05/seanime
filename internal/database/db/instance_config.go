package db

import (
	"seanime/internal/database/models"
)

func (db *Database) GetInstanceConfig() (*models.InstanceConfig, error) {
	var config models.InstanceConfig
	err := db.gormdb.Where("id = ?", "1").First(&config).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}

// setInstanceConfig loads the singleton config row, applies modify, and saves it.
// Field-scoped setters below must be used instead of a whole-struct upsert so that
// updating one field (e.g. the access code) can never zero out the others (e.g. the JWT secret).
func (db *Database) setInstanceConfig(modify func(*models.InstanceConfig)) error {
	var config models.InstanceConfig
	err := db.gormdb.Where("id = ?", "1").First(&config).Error
	if err != nil {
		config = models.InstanceConfig{ID: "1"}
		modify(&config)
		err = db.gormdb.Create(&config).Error
	} else {
		modify(&config)
		err = db.gormdb.Save(&config).Error
	}
	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to update instance config")
	}
	return err
}

func (db *Database) SetInstanceAccessCodeHash(hash string) error {
	return db.setInstanceConfig(func(c *models.InstanceConfig) { c.AccessCodeHash = hash })
}

func (db *Database) SetInstanceJWTSecret(secret string) error {
	return db.setInstanceConfig(func(c *models.InstanceConfig) { c.JWTSecret = secret })
}

func (db *Database) HasAccessCode() (bool, error) {
	config, err := db.GetInstanceConfig()
	if err != nil {
		return false, nil
	}
	return config.AccessCodeHash != "", nil
}
