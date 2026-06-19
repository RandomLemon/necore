package middleware

import (
	"necore/config"

	jwtware "github.com/gofiber/contrib/jwt"
	"github.com/gofiber/fiber/v2"
)

func AuthNeeded() fiber.Handler {
	return jwtware.New(jwtware.Config{
		SigningKey:   jwtware.SigningKey{Key: []byte(config.Config("SECRET"))},
		ErrorHandler: jwtError,
		SuccessHandler: func(c *fiber.Ctx) error {
			return validateTokenVersion(c)
		},
	})
}

func jwtError(c *fiber.Ctx, err error) error {
	if err.Error() == "Missing or malformed JWT" {
		return c.Status(fiber.StatusBadRequest).
			JSON(fiber.Map{"error": "Missing or malformed JWT", "err": nil})
	}
	return c.Status(fiber.StatusUnauthorized).
		JSON(fiber.Map{"error": "Invalid or expired JWT", "err": nil})
}
