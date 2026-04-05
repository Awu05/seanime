package db

import (
	"seanime/internal/database/models"

	"github.com/google/uuid"
)

func (db *Database) CreateAdmin(admin *models.Admin) (*models.Admin, error) {
	if admin.ID == "" {
		admin.ID = uuid.New().String()
	}
	err := db.gormdb.Create(admin).Error
	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to create admin")
		return nil, err
	}
	return admin, nil
}

func (db *Database) GetAdmin() (*models.Admin, error) {
	var admin models.Admin
	err := db.gormdb.Preload("Profile").First(&admin).Error
	if err != nil {
		return nil, err
	}
	return &admin, nil
}

func (db *Database) GetAdminByUsername(username string) (*models.Admin, error) {
	var admin models.Admin
	err := db.gormdb.Preload("Profile").Where("username = ?", username).First(&admin).Error
	if err != nil {
		return nil, err
	}
	return &admin, nil
}

func (db *Database) AdminExists() (bool, error) {
	var count int64
	err := db.gormdb.Model(&models.Admin{}).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (db *Database) UpdateAdminPassword(id string, passwordHash string) error {
	err := db.gormdb.Model(&models.Admin{}).Where("id = ?", id).Update("password_hash", passwordHash).Error
	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to update admin password")
	}
	return err
}
