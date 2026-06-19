package model

import (
	"gorm.io/gorm"
)

type User struct {
	gorm.Model

	Username     string `gorm:"uniqueIndex;not null" json:"username"`
	Password     string `gorm:"not null" json:"password"` // sha256 hashed
	Group        string `json:"group"`                    // json array: []string
	Tags         string `json:"tags"`                     // json array: []string
	Avatar       string `json:"avatar"`
	TokenVersion uint   `gorm:"not null;default:1" json:"-"`
}
