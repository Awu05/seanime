package models

type Admin struct {
	UUIDBaseModel
	Username     string  `gorm:"column:username;uniqueIndex;not null" json:"username"`
	PasswordHash string  `gorm:"column:password_hash;not null" json:"-"`
	ProfileID    string  `gorm:"column:profile_id;not null" json:"profileId"`
	Profile      Profile `gorm:"foreignKey:ProfileID" json:"profile,omitempty"`
}
