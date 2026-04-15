package db

import (
	"seanime/internal/database/models"

	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// CleanupManager manages database cleanup operations to prevent concurrent access issues
type CleanupManager struct {
	gormdb *gorm.DB
	logger *zerolog.Logger
}

func NewCleanupManager(gormdb *gorm.DB, logger *zerolog.Logger) *CleanupManager {
	return &CleanupManager{
		gormdb: gormdb,
		logger: logger,
	}
}

func (cm *CleanupManager) RunAllCleanupOperations() {
	cm.logger.Debug().Msg("database: Starting cleanup operations")

	cm.trimScanSummaryEntries()
	cm.trimLocalFileEntries()
	cm.trimTorrentstreamHistory()

	cm.logger.Debug().Msg("database: Cleanup operations completed")
}

// trimScanSummaryEntries trims scan summary entries
func (cm *CleanupManager) trimScanSummaryEntries() {
	var count int64
	err := cm.gormdb.Model(&models.ScanSummary{}).Count(&count).Error
	if err != nil {
		cm.logger.Error().Err(err).Msg("database: Failed to count scan summary entries")
		return
	}
	if count > 10 {
		var idsToDelete []uint
		err = cm.gormdb.Model(&models.ScanSummary{}).
			Select("id").
			Order("id ASC").
			Limit(int(count-5)).
			Pluck("id", &idsToDelete).Error
		if err != nil {
			cm.logger.Error().Err(err).Msg("database: Failed to get scan summary IDs to delete")
			return
		}

		if len(idsToDelete) > 0 {
			batchSize := 900
			for i := 0; i < len(idsToDelete); i += batchSize {
				end := i + batchSize
				if end > len(idsToDelete) {
					end = len(idsToDelete)
				}
				batch := idsToDelete[i:end]
				err = cm.gormdb.Delete(&models.ScanSummary{}, batch).Error
				if err != nil {
					cm.logger.Error().Err(err).Msg("database: Failed to delete old scan summary entries")
					return // Exit on first error
				}
			}
			cm.logger.Debug().Int("deleted", len(idsToDelete)).Msg("database: Deleted old scan summary entries")
		}
	}
}

// trimLocalFileEntries trims local file entries per profile, keeping up to 5 entries per profile.
func (cm *CleanupManager) trimLocalFileEntries() {
	// Get distinct profile IDs (including empty string for legacy/single-user)
	var profileIDs []string
	err := cm.gormdb.Model(&models.LocalFiles{}).Distinct("profile_id").Pluck("profile_id", &profileIDs).Error
	if err != nil {
		cm.logger.Error().Err(err).Msg("database: Failed to get profile IDs for local file cleanup")
		return
	}

	for _, pid := range profileIDs {
		var count int64
		query := cm.gormdb.Model(&models.LocalFiles{})
		if pid != "" {
			query = query.Where("profile_id = ?", pid)
		} else {
			query = query.Where("profile_id = '' OR profile_id IS NULL")
		}
		if err := query.Count(&count).Error; err != nil {
			continue
		}
		if count <= 10 {
			continue
		}

		var idsToDelete []uint
		delQuery := cm.gormdb.Model(&models.LocalFiles{})
		if pid != "" {
			delQuery = delQuery.Where("profile_id = ?", pid)
		} else {
			delQuery = delQuery.Where("profile_id = '' OR profile_id IS NULL")
		}
		if err := delQuery.Select("id").Order("id ASC").Limit(int(count - 5)).Pluck("id", &idsToDelete).Error; err != nil {
			continue
		}

		if len(idsToDelete) > 0 {
			batchSize := 900
			for i := 0; i < len(idsToDelete); i += batchSize {
				end := i + batchSize
				if end > len(idsToDelete) {
					end = len(idsToDelete)
				}
				if err := cm.gormdb.Delete(&models.LocalFiles{}, idsToDelete[i:end]).Error; err != nil {
					cm.logger.Error().Err(err).Msg("database: Failed to delete old local file entries")
					return
				}
			}
			cm.logger.Debug().Int("deleted", len(idsToDelete)).Str("profileId", pid).Msg("database: Deleted old local file entries")
		}
	}
}

// trimTorrentstreamHistory trims torrent stream history entries
func (cm *CleanupManager) trimTorrentstreamHistory() {
	var count int64
	err := cm.gormdb.Model(&models.TorrentstreamHistory{}).Count(&count).Error
	if err != nil {
		cm.logger.Error().Err(err).Msg("database: Failed to count torrent stream history entries")
		return
	}
	if count > 50 {
		var idsToDelete []uint
		err = cm.gormdb.Model(&models.TorrentstreamHistory{}).
			Select("id").
			Order("updated_at ASC").
			Limit(int(count-40)).
			Pluck("id", &idsToDelete).Error
		if err != nil {
			cm.logger.Error().Err(err).Msg("database: Failed to get torrent stream history IDs to delete")
			return
		}

		if len(idsToDelete) > 0 {
			batchSize := 900
			for i := 0; i < len(idsToDelete); i += batchSize {
				end := i + batchSize
				if end > len(idsToDelete) {
					end = len(idsToDelete)
				}
				batch := idsToDelete[i:end]
				err = cm.gormdb.Delete(&models.TorrentstreamHistory{}, batch).Error
				if err != nil {
					cm.logger.Error().Err(err).Msg("database: Failed to delete old torrent stream history entries")
					return // Exit on first error
				}
			}
			cm.logger.Debug().Int("deleted", len(idsToDelete)).Msg("database: Deleted old torrent stream history entries")
		}
	}
}
