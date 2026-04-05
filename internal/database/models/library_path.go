package models

type LibraryPath struct {
	UUIDBaseModel
	Path    string `gorm:"column:path;not null" json:"path"`
	OwnerID string `gorm:"column:owner_id;index" json:"ownerId"`
	Shared  bool   `gorm:"column:shared;default:true" json:"shared"`
}
