package middleware

import (
	"errors"
	"necore/database"
	"necore/model"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

func validateTokenVersion(c *fiber.Ctx) error {
	token, ok := c.Locals("user").(*jwt.Token)
	if !ok || token == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Unauthorized",
		})
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Invalid token",
		})
	}

	username, ok := claims["name"].(string)
	if !ok || username == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Invalid token",
		})
	}

	tokenVersionFloat, ok := claims["ver"].(float64)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Invalid token",
		})
	}

	var user model.User
	err := database.GetUserDatabase().
		Select("username", "token_version", "group", "tags").
		Where("username = ?", username).
		First(&user).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "User no longer exists",
		})
	}

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal server error",
		})
	}

	if uint(tokenVersionFloat) != user.TokenVersion {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Token has been revoked",
		})
	}

	// 将数据库中的最新用户信息放入 Locals，
	// 后续权限中间件直接使用，不再信任 JWT 中的 group。
	c.Locals("currentUser", user)

	return c.Next()
}
