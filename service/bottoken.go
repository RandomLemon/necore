package service

import (
	"errors"
	"necore/dao"
	"necore/model"
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func checkBotTokenPermission(c *fiber.Ctx) bool {
	user := c.Locals("currentUser").(model.User)
	isBotAdmin := dao.ContainsGroup(user.Group, "bot_admin") || dao.ContainsGroup(user.Group, "admin")
	if isBotAdmin {
		return false
	}
	return true
}

func validateBotTokenName(name string) error {
	name = strings.TrimSpace(name)
	if len(name) < 1 || len(name) > 64 {
		return errors.New("Invalid token name")
	}

	valid := regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	if !valid.MatchString(name) {
		return errors.New("Invalid token name")
	}

	return nil
}

func CreateBotToken(c *fiber.Ctx) error {
	if checkBotTokenPermission(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}

	type request struct {
		Name string `json:"name"`
	}

	var r request
	if err := c.BodyParser(&r); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request",
		})
	}

	name := strings.TrimSpace(r.Name)

	if err := validateBotTokenName(name); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	token, plainToken, err := dao.CreateBotToken(name)
	if err != nil {
		if errors.Is(err, dao.ErrBotTokenAlreadyExists) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error": "Bot token name already exists",
			})
		}

		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal server error",
		})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"token":       token,
		"plain_token": plainToken,
	})
}

func GetBotToken(c *fiber.Ctx) error {
	if checkBotTokenPermission(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}

	token, err := dao.GetBotToken(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{"token": token})
}

func GetBotTokenList(c *fiber.Ctx) error {
	if checkBotTokenPermission(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	tokens := dao.GetBotTokens()

	return c.JSON(fiber.Map{"tokens": tokens})
}

func DeleteBotToken(c *fiber.Ctx) error {
	if checkBotTokenPermission(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	if err := dao.DeleteBotToken(c.Params("id")); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.SendStatus(fiber.StatusOK)
}
