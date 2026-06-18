package model

import "gorm.io/gorm"

type BotToken struct {
	gorm.Model

	Name      string `gorm:"uniqueIndex;not null" json:"name"`
	TokenHash string `gorm:"uniqueIndex;not null" json:"-"`
}
