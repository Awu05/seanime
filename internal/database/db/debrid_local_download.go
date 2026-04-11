package db

import (
	"seanime/internal/database/models"
)

func (db *Database) GetDebridLocalDownloads() ([]*models.DebridLocalDownload, error) {
	var res []*models.DebridLocalDownload
	err := db.gormdb.Find(&res).Error
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (db *Database) GetDebridLocalDownloadByTorrentItemId(tId string) (*models.DebridLocalDownload, error) {
	var res *models.DebridLocalDownload
	err := db.gormdb.Where("torrent_item_id = ?", tId).First(&res).Error
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (db *Database) UpsertDebridLocalDownload(item *models.DebridLocalDownload) error {
	// If an entry for this torrent already exists, update it; otherwise insert.
	var existing models.DebridLocalDownload
	err := db.gormdb.Where("torrent_item_id = ?", item.TorrentItemID).First(&existing).Error
	if err == nil {
		existing.TorrentName = item.TorrentName
		existing.TorrentHash = item.TorrentHash
		existing.LocalPath = item.LocalPath
		return db.gormdb.Save(&existing).Error
	}
	return db.gormdb.Create(item).Error
}

func (db *Database) DeleteDebridLocalDownloadByTorrentItemId(tId string) error {
	return db.gormdb.Where("torrent_item_id = ?", tId).Delete(&models.DebridLocalDownload{}).Error
}
