package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"necore/dao"
	"necore/model"
	"necore/util"
	"necore/ws"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

func generateStoredFilename(original string) (string, error) {
	safeName, err := util.SafeFilename(original)
	if err != nil {
		return "", err
	}

	extension := strings.ToLower(filepath.Ext(safeName))

	allowedExtensions := map[string]bool{
		".png":  true,
		".jpg":  true,
		".jpeg": true,
		".webp": true,
		".pdf":  true,
		".txt":  true,
	}

	if !allowedExtensions[extension] {
		return "", errors.New("unsupported file extension")
	}

	return uuid.NewString() + extension, nil
}

func checkNewsPermission(c *fiber.Ctx) bool {
	// Check if user is admin or news_admin
	user := c.Locals("currentUser").(model.User)
	isAdmin := dao.ContainsGroup(user.Group, "admin")
	isNewsAdmin := dao.ContainsGroup(user.Group, "news_admin")
	if isAdmin || isNewsAdmin {
		return false
	}
	return true
}

func CreateArticle(c *fiber.Ctx) error {
	if checkNewsPermission(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}

	// Create new article
	id := uuid.New().String()
	err := dao.CreateArticle(id)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err,
		})
	}
	return c.JSON(fiber.Map{
		"id": id,
	})
}

func UpdateArticle(c *fiber.Ctx) error {
	if checkNewsPermission(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	user := c.Locals("currentUser").(model.User)
	author := user.Username

	id := c.Params("id")
	// Parse
	type PayloadEntity struct {
		Pin     bool   `json:"pin"`
		Title   string `json:"title"`
		Brief   string `json:"brief"`
		Date    string `json:"date"`
		EndDate string `json:"endDate"`
		Image   string `json:"image"`
	}
	type PayloadContent struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	type Payload struct {
		Entity           PayloadEntity    `json:"entity"`
		Content          []PayloadContent `json:"content"`
		Category         string           `json:"category"`
		DoesNotify       bool             `json:"doesNotify"`
		NotifySessionIDs []string         `json:"notifySessionIds"`
	}
	payload := new(Payload)
	if err := c.BodyParser(payload); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	newContent, err := json.Marshal(payload.Content)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	newArticle := model.Article{
		Id:       id,
		Pin:      payload.Entity.Pin,
		Title:    payload.Entity.Title,
		Brief:    payload.Entity.Brief,
		Date:     payload.Entity.Date,
		EndDate:  payload.Entity.EndDate,
		Image:    payload.Entity.Image,
		Category: payload.Category,
		Content:  string(newContent),
		Author:   author,
	}

	if err := dao.UpdateArticle(newArticle); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if payload.DoesNotify {
		message := fiber.Map{
			"event": "article_updated",
			"data":  newArticle,
		}

		if len(payload.NotifySessionIDs) > 0 {
			go ws.GlobalHub.BroadcastToSessions(message, payload.NotifySessionIDs)
		} else {
			go ws.GlobalHub.Broadcast(message)
		}
	}

	return c.SendStatus(fiber.StatusOK)
}

func GetArticleById(c *fiber.Ctx) error {
	id := c.Params("id")

	article, err := dao.GetArticle(id)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	type PayloadEntity struct {
		Pin     bool   `json:"pin"`
		Title   string `json:"title"`
		Brief   string `json:"brief"`
		Date    string `json:"date"`
		EndDate string `json:"endDate"`
		Image   string `json:"image"`
	}
	type PayloadContent struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	type Payload struct {
		Entity   PayloadEntity    `json:"entity"`
		Content  []PayloadContent `json:"content"`
		Category string           `json:"category"`
		Author   string           `json:"author"`
	}
	payloadEntity := PayloadEntity{
		Pin:     article.Pin,
		Title:   article.Title,
		Brief:   article.Brief,
		Date:    article.Date,
		EndDate: article.EndDate,
		Image:   article.Image,
	}
	var payloadContent []PayloadContent
	json.Unmarshal([]byte(article.Content), &payloadContent)
	payload := Payload{
		Entity:   payloadEntity,
		Content:  payloadContent,
		Category: article.Category,
		Author:   article.Author,
	}
	return c.JSON(payload)
}

func GetArticleCountByCategory(c *fiber.Ctx) error {
	category := c.Params("target")
	// target: "information" | "magazine" | "notice" | "activity" | "document"
	count, err := dao.GetArticleCountByCategory(category)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"total": count})
}

func GetArticleList(c *fiber.Ctx) error {
	type Payload struct {
		Target   string `json:"target"`
		Page     int    `json:"page"`
		PageSize int    `json:"page_size"`
		Pin      bool   `json:"pin"`
	}
	payload := new(Payload)
	if err := c.BodyParser(payload); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	articles, err := dao.GetArticleList(payload.Target, payload.Page, payload.PageSize, payload.Pin)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	type Entity struct {
		Id      string `json:"id"`
		Pin     bool   `json:"pin"`
		Title   string `json:"title"`
		Brief   string `json:"brief"`
		Date    string `json:"date"`
		EndDate string `json:"endDate"`
		Image   string `json:"image"`
	}
	var entities []Entity
	for _, article := range articles {
		entities = append(entities, Entity{
			Id:      article.Id,
			Pin:     article.Pin,
			Title:   article.Title,
			Brief:   article.Brief,
			Date:    article.Date,
			EndDate: article.EndDate,
			Image:   article.Image,
		})
	}
	return c.JSON(fiber.Map{"list": entities})
}

func UploadArticleFile(c *fiber.Ctx) error {
	if checkNewsPermission(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}

	id := c.Params("id")
	file, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	if err := os.MkdirAll(fmt.Sprintf("./contents/%s", id), 0o750); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	storedName, err := generateStoredFilename(file.Filename)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	contentPath, err := util.SafeContentPath("./contents", id, storedName)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if err := c.SaveFile(file, contentPath); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"url": fmt.Sprintf("/contents/%s/%s", id, storedName)})
}

func DeleteArticleFile(c *fiber.Ctx) error {
	if checkNewsPermission(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}

	id := c.Params("id")

	type Payload struct {
		Filename string `json:"filename"`
	}

	var payload Payload
	if err := c.BodyParser(&payload); err != nil {
		return c.Status(fiber.StatusBadRequest).
			JSON(fiber.Map{"error": "Invalid request body"})
	}

	target, err := util.SafeContentPath("./contents", id, payload.Filename)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).
			JSON(fiber.Map{"error": "Invalid filename"})
	}

	if err := os.Remove(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c.Status(fiber.StatusNotFound).
				JSON(fiber.Map{"error": "File not found"})
		}

		return c.Status(fiber.StatusInternalServerError).
			JSON(fiber.Map{"error": "Internal server error"})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func DeleteArticle(c *fiber.Ctx) error {
	if checkNewsPermission(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}

	id := c.Params("id")
	if err := dao.DeleteArticle(id); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.SendStatus(fiber.StatusOK)
}
