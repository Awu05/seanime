package models

type InstanceConfig struct {
	ID             string `gorm:"primarykey;type:text;default:'1'" json:"id"`
	AccessCodeHash string `gorm:"column:access_code_hash" json:"-"`
}
