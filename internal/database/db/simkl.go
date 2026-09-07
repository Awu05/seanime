package db

import (
	"errors"
	"seanime/internal/database/models"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// maxPendingSyncAttempts caps how many times a row is retried. Without it a permanently
// undeliverable row (a deleted profile, a revoked token) is re-attempted every 30 minutes
// for the lifetime of the install.
const maxPendingSyncAttempts = 20

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

// UpsertSimklAccount inserts or updates the profile's SIMKL account. The OnConflict clause
// (backed by SimklAccount.ProfileID's uniqueIndex) makes this atomic under concurrent calls
// for the same profile - e.g. two PIN-poll requests both completing at once - so they settle
// on a single row instead of racing into duplicates.
//
// "username" is only included in the update when account.Username is non-empty: no current
// caller populates it (HandleSimklConnectPoll only has an access token to save), so
// unconditionally assigning it would blank out any previously-stored username on every
// reconnect once something does start populating it.
func (db *Database) UpsertSimklAccount(account *models.SimklAccount) (*models.SimklAccount, error) {
	columns := []string{"access_token"}
	if account.Username != "" {
		columns = append(columns, "username")
	}
	err := db.gormdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "profile_id"}},
		DoUpdates: clause.AssignmentColumns(columns),
	}).Create(account).Error
	if err != nil {
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

// UpdateSimklSettings atomically applies only the given fields (nil = "leave unchanged") to the
// profile's SIMKL settings row, inserting a default row first if none exists. This replaces the
// previous read-then-write pattern (GetSimklSettings, mutate, UpsertSimklSettings): with two
// separate PATCH requests able to arrive close together - the enable toggle and the Client ID
// field save are two independent UI actions - a read-then-write lets whichever write commits
// second silently discard the other's change, since it still carries the stale copy of the field
// it wasn't touching. Restricting the OnConflict DoUpdates to only the columns actually supplied
// makes each PATCH a single atomic UPDATE of just its own field(s), closing that race.
func (db *Database) UpdateSimklSettings(profileID string, enabled *bool, clientID *string) (*models.SimklSettings, error) {
	base := &models.SimklSettings{ProfileID: profileID}
	var columns []string
	if enabled != nil {
		base.Enabled = *enabled
		columns = append(columns, "enabled")
	}
	if clientID != nil {
		base.ClientId = *clientID
		columns = append(columns, "client_id")
	}
	if len(columns) == 0 {
		return db.GetSimklSettings(profileID)
	}

	err := db.gormdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "profile_id"}},
		DoUpdates: clause.AssignmentColumns(columns),
	}).Create(base).Error
	if err != nil {
		return nil, err
	}
	return db.GetSimklSettings(profileID)
}

func (db *Database) EnqueuePendingSync(item *models.PendingSync) error {
	return db.gormdb.Create(item).Error
}

// EnqueuePendingSyncBatch inserts many rows in chunked batches instead of one INSERT per row -
// used by the SIMKL "sync now" seed, which can enqueue thousands of rows at once.
func (db *Database) EnqueuePendingSyncBatch(items []*models.PendingSync) error {
	if len(items) == 0 {
		return nil
	}
	return db.gormdb.CreateInBatches(items, 100).Error
}

// CountPendingSyncs returns how many profileID+target rows are still actively being retried -
// used by the UI to show a "syncing..." indicator after Sync Now, polling until this reaches
// zero. Excludes rows that hit maxPendingSyncAttempts: those are permanently given up on (see
// IncrementPendingSyncAttempt) but never deleted, so counting them would leave the indicator
// stuck forever on an install with even one row that failed for good (e.g. a deleted anime).
func (db *Database) CountPendingSyncs(profileID, target string) (int64, error) {
	var count int64
	err := db.gormdb.Model(&models.PendingSync{}).
		Where("profile_id = ? AND target = ? AND attempts < ?", profileID, target, maxPendingSyncAttempts).
		Count(&count).Error
	return count, err
}

func (db *Database) GetDuePendingSyncs(target string, limit int) ([]*models.PendingSync, error) {
	var res []*models.PendingSync
	err := db.gormdb.Where("target = ? AND next_attempt_at <= ? AND attempts < ?", target, time.Now(), maxPendingSyncAttempts).
		Order("created_at asc").
		Limit(limit).
		Find(&res).Error
	return res, err
}

// IncrementPendingSyncAttempt records a failed delivery attempt. Once a row's attempts reach
// maxPendingSyncAttempts, GetDuePendingSyncs stops returning it forever - so that transition is
// logged here, otherwise a permanently-undeliverable row (revoked token, deleted profile) would
// just silently stop syncing with no trace anywhere.
func (db *Database) IncrementPendingSyncAttempt(id uint, nextAttemptAt time.Time, lastErr string) error {
	err := db.gormdb.Model(&models.PendingSync{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"attempts":        gorm.Expr("attempts + 1"),
			"next_attempt_at": nextAttemptAt,
			"last_error":      lastErr,
		}).Error
	if err != nil {
		return err
	}

	if db.Logger == nil {
		return nil
	}
	var row models.PendingSync
	if fetchErr := db.gormdb.Select("attempts", "target", "operation", "profile_id").First(&row, id).Error; fetchErr == nil && row.Attempts >= maxPendingSyncAttempts {
		db.Logger.Warn().Uint("id", id).Str("target", row.Target).Str("operation", row.Operation).Str("profileID", row.ProfileID).
			Str("lastError", lastErr).Msg("pending sync: row exhausted its retry budget and will no longer be retried")
	}
	return nil
}

func (db *Database) DeletePendingSync(id uint) error {
	return db.gormdb.Delete(&models.PendingSync{}, id).Error
}
