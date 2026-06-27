package dao

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"necore/database"
	"necore/model"
	"necore/util"
	"time"

	"gorm.io/gorm"
)

var ErrBotTokenAlreadyExists = errors.New("bot token already exists")

func generateBotToken() (string, string, error) {
	tokenStr, err := util.GenerateSecureToken("bot", 64)
	if err != nil {
		return "", "", err
	}

	sum := sha256.Sum256([]byte(tokenStr))
	tokenHash := hex.EncodeToString(sum[:])

	return tokenStr, tokenHash, nil
}

func CreateBotToken(name string) (*model.BotToken, string, error) {
	tokenStr, tokenHash, err := generateBotToken()
	if err != nil {
		return nil, "", err
	}

	db := database.GetBotTokenDatabase()

	var existingToken model.BotToken

	err = db.Unscoped().
		Where("name = ?", name).
		First(&existingToken).Error

	if err == nil {
		if !existingToken.DeletedAt.Valid {
			return nil, "", ErrBotTokenAlreadyExists
		}

		now := time.Now()

		if err := db.Unscoped().
			Model(&existingToken).
			Updates(map[string]any{
				"token_hash": tokenHash,
				"deleted_at": nil,
				"created_at": now,
				"updated_at": now,
			}).Error; err != nil {
			return nil, "", err
		}

		var restoredToken model.BotToken
		if err := db.Where("name = ?", name).First(&restoredToken).Error; err != nil {
			return nil, "", err
		}

		return &restoredToken, tokenStr, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", err
	}

	newToken := model.BotToken{
		Name:      name,
		TokenHash: tokenHash,
	}

	if err := db.Create(&newToken).Error; err != nil {
		return nil, "", err
	}

	return &newToken, tokenStr, nil
}

func GetBotTokens() []model.BotToken {
	var tokens []model.BotToken
	db := database.GetBotTokenDatabase()
	db.Find(&tokens)
	return tokens
}

func GetBotToken(name string) (*model.BotToken, error) {
	var token model.BotToken

	err := database.GetBotTokenDatabase().
		Where("name = ?", name).
		First(&token).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("bot token '%s' not found", name)
	}

	if err != nil {
		return nil, err
	}

	return &token, nil
}

func GetBotTokenByToken(token string) (*model.BotToken, error) {
	var tokenModel model.BotToken

	db := database.GetBotTokenDatabase()

	if err := db.Where(&model.BotToken{TokenHash: token}).First(&tokenModel).Error; err != nil {
		return nil, err
	}

	return &tokenModel, nil
}

func GetBotTokenByPlainToken(token string) (*model.BotToken, error) {
	sum := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(sum[:])

	return GetBotTokenByToken(tokenHash)
}

func DeleteBotToken(name string) error {
	db := database.GetBotTokenDatabase()

	result := db.Unscoped().
		Where("name = ?", name).
		Delete(&model.BotToken{})

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("bot token '%s' not found", name)
	}

	return nil
}
