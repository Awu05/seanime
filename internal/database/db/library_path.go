package db

import (
	"seanime/internal/database/models"

	"github.com/google/uuid"
)

func (db *Database) CreateLibraryPath(lp *models.LibraryPath) (*models.LibraryPath, error) {
	if lp.ID == "" {
		lp.ID = uuid.New().String()
	}
	err := db.gormdb.Create(lp).Error
	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to create library path")
		return nil, err
	}
	return lp, nil
}

func (db *Database) GetAllLibraryPaths() ([]*models.LibraryPath, error) {
	var paths []*models.LibraryPath
	err := db.gormdb.Order("created_at ASC").Find(&paths).Error
	if err != nil {
		return nil, err
	}
	return paths, nil
}

func (db *Database) GetLibraryPathsForProfile(profileID string) ([]*models.LibraryPath, error) {
	var paths []*models.LibraryPath
	err := db.gormdb.Where(
		"owner_id = '' OR owner_id IS NULL OR shared = ? OR owner_id = ?",
		true, profileID,
	).Order("created_at ASC").Find(&paths).Error
	if err != nil {
		return nil, err
	}
	return paths, nil
}

func (db *Database) GetLibraryPathStringsForProfile(profileID string) ([]string, error) {
	paths, err := db.GetLibraryPathsForProfile(profileID)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(paths))
	for _, p := range paths {
		if p.Path != "" {
			result = append(result, p.Path)
		}
	}
	return result, nil
}

func (db *Database) GetAllLibraryPathStrings() ([]string, error) {
	paths, err := db.GetAllLibraryPaths()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	result := make([]string, 0, len(paths))
	for _, p := range paths {
		if p.Path != "" && !seen[p.Path] {
			seen[p.Path] = true
			result = append(result, p.Path)
		}
	}
	return result, nil
}

func (db *Database) DeleteLibraryPath(id string) error {
	err := db.gormdb.Where("id = ?", id).Delete(&models.LibraryPath{}).Error
	if err != nil {
		db.Logger.Error().Err(err).Msg("db: Failed to delete library path")
	}
	return err
}

func (db *Database) GetLibraryPathByID(id string) (*models.LibraryPath, error) {
	var lp models.LibraryPath
	err := db.gormdb.Where("id = ?", id).First(&lp).Error
	if err != nil {
		return nil, err
	}
	return &lp, nil
}
