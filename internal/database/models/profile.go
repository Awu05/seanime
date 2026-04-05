package models

type Profile struct {
	UUIDBaseModel
	Name    string `gorm:"column:name;uniqueIndex;not null" json:"name"`
	PinHash string `gorm:"column:pin_hash" json:"-"`
	IsAdmin bool   `gorm:"column:is_admin;default:false" json:"isAdmin"`
	Avatar  string `gorm:"column:avatar" json:"avatar"`
}
