package service

import (
	"necore/dao"
	"necore/model"

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

func CreateBotToken(c *fiber.Ctx) error {
	if checkBotTokenPermission(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	type request struct {
		Name string `json:"name"`
	}
	r := new(request)
	if err := c.BodyParser(r); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request",
		})
	}
	token, err := dao.CreateBotToken(r.Name)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err})
	}
	return c.JSON(fiber.Map{"token": token})
}

func GetBotToken(c *fiber.Ctx) error {
	if checkBotTokenPermission(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	token, err := dao.GetBotToken(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err})
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
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err})
	}
	return c.SendStatus(fiber.StatusOK)
}
