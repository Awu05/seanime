package models

type ProfileSettings struct {
	UUIDBaseModel
	ProfileID string `gorm:"column:profile_id;uniqueIndex;not null" json:"profileId"`
	Overrides string `gorm:"column:overrides;type:text" json:"overrides"`
}
