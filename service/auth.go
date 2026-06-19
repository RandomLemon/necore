package service

import (
	"encoding/json"
	"necore/dao"
	"necore/model"

	"github.com/gofiber/fiber/v2"
)

// Handlers

func Login(c *fiber.Ctx) error {
	// Parse Request Body
	type LoginInput struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	input := new(LoginInput)
	if err := c.BodyParser(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Error on login request", "err": err})
	}

	// Get User
	userModel, err := dao.GetUserByUsername(input.Username)

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Server Error", "err": err})
	} else if userModel == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid identity or password", "err": err})
	}

	// Check Password
	if !dao.CheckUserPassword(input.Password, userModel.Password) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid identity or password"})
	}

	// Token
	t, err := dao.CreateToken(*userModel)
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	// User Info
	type TagEntity struct {
		Text     string `json:"text"`
		Color    string `json:"color"`
		TagColor string `json:"tagColor"`
	}
	type UserInfo struct {
		Username string      `json:"username"`
		Group    []string    `json:"group"`
		Tags     []TagEntity `json:"tags"`
	}

	var groups []string
	err = json.Unmarshal([]byte(userModel.Group), &groups)
	if err != nil {
		groups = []string{}
	}
	var tags []TagEntity
	err = json.Unmarshal([]byte(userModel.Tags), &tags)
	if err != nil {
		tags = []TagEntity{}
	}
	userInfo := UserInfo{
		Username: userModel.Username,
		Group:    groups,
		Tags:     tags,
	}
	return c.JSON(fiber.Map{
		"token": t,
		"user":  userInfo,
	})
}

// Register by admin
func AddUser(c *fiber.Ctx) error {
	// Check if user is admin
	currentUser := c.Locals("currentUser").(model.User)
	if !dao.ContainsGroup(currentUser.Group, "admin") {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}

	// Parse body
	type NewUser struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	user := new(NewUser)
	if err := c.BodyParser(user); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Review your input", "err": err})
	}

	if err := dao.AddUserByUsername(user.Username, user.Password); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err})
	}

	return c.JSON(fiber.Map{})
}

func GetStatus(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "alive"})
}
