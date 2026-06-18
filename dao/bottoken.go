package dao

import (
	"crypto/sha256"
	"encoding/hex"
	"necore/database"
	"necore/model"
	"necore/util"
)

func CreateBotToken(name string) (*model.BotToken, error) {
	tokenStr, err := util.GenerateSecureToken("bot", 64)
	if err != nil {
		return nil, err
	}

	sum := sha256.Sum256([]byte(tokenStr))
	tokenHash := hex.EncodeToString(sum[:])

	newToken := model.BotToken{
		Name:      name,
		TokenHash: tokenHash,
	}

	if err := database.GetBotTokenDatabase().
		Create(&newToken).Error; err != nil {
		return nil, err
	}

	return &newToken, nil
}

func GetBotTokens() []model.BotToken {
	var tokens []model.BotToken
	db := database.GetBotTokenDatabase()
	db.Find(&tokens)
	return tokens
}

func GetBotToken(name string) (*model.BotToken, error) {
	var token model.BotToken
	db := database.GetBotTokenDatabase()
	if err := db.Where(&model.BotToken{Name: name}).First(&token).Error; err != nil {
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
	return db.Where(&model.BotToken{Name: name}).Delete(&model.BotToken{}).Error
}
