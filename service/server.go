package service

import (
	"necore/dao"
	"necore/model"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/millkhan/mcstatusgo/v2"
)

func checkServerPermission(c *fiber.Ctx) bool {
	// Check if user is admin or news_admin
	user := c.Locals("currentUser").(model.User)
	isAdmin := dao.ContainsGroup(user.Group, "admin")
	isNewsAdmin := dao.ContainsGroup(user.Group, "server_admin")
	if isAdmin || isNewsAdmin {
		return false
	}
	return true
}

func GetServerList(c *fiber.Ctx) error {
	servers, err := dao.GetServerList()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	type Response struct {
		Id           string `json:"id"`
		Name         string `json:"name"`
		Icon         string `json:"icon"`
		Description  string `json:"description"`
		OnlineMapUrl string `json:"onlineMapUrl"`
		Realtime     bool   `json:"realtime"`
		ServerUrl    string `json:"serverUrl"`
	}
	res := make([]Response, len(servers))
	for i, server := range servers {
		res[i] = Response{
			Id:           server.Id,
			Name:         server.Name,
			Icon:         server.Icon,
			Description:  server.Description,
			OnlineMapUrl: server.OnlineMapUrl,
			Realtime:     server.Realtime,
			ServerUrl:    server.ServerUrl,
		}
	}
	return c.JSON(fiber.Map{
		"servers": res,
	})
}

var statusSlots = make(chan struct{}, 16)

func GetServerStatus(c *fiber.Ctx) error {
	select {
	case statusSlots <- struct{}{}:
		defer func() { <-statusSlots }()
	default:
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
			"error": "Service busy",
		})
	}
	type Request struct {
		ServerUrl string `json:"serverUrl"`
	}
	var req Request
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	type Player struct {
		Name string `json:"name"`
		UUID string `json:"uuid"`
	}

	type Response struct {
		Online      bool     `json:"online"`
		Icon        string   `json:"icon"`
		PlayerCount int      `json:"playerCount"`
		Capacity    int      `json:"capacity"`
		Latency     int      `json:"latency"`
		Version     string   `json:"version"`
		Players     []Player `json:"players"`
	}

	results := strings.Split(req.ServerUrl, ":")
	var result string
	var port int
	if len(results) != 2 {
		result = results[0]
		port = 25565
	} else {
		result = results[0]
		port, _ = strconv.Atoi(results[1])
	}
	initialTimeout := time.Second * 10
	ioTimeout := time.Second * 5

	status, err := mcstatusgo.Status(result, uint16(port), initialTimeout, ioTimeout)
	if err != nil {
		res := Response{
			Online:      false,
			Icon:        "",
			PlayerCount: 0,
			Capacity:    0,
			Latency:     0,
			Version:     "",
			Players:     []Player{},
		}
		return c.Status(fiber.StatusInternalServerError).JSON(res)
	} else {
		players := make([]Player, 0, len(status.Players.Sample))

		for _, sample := range status.Players.Sample {
			name := strings.TrimSpace(sample["name"])
			uuid := strings.TrimSpace(sample["id"])

			if name == "" {
				continue
			}

			players = append(players, Player{
				Name: name,
				UUID: uuid,
			})
		}

		res := Response{
			Online:      true,
			Icon:        status.Favicon,
			PlayerCount: status.Players.Online,
			Capacity:    status.Players.Max,
			Latency:     int(status.Latency.Milliseconds()),
			Version:     status.Version.Name,
			Players:     players,
		}
		return c.Status(fiber.StatusOK).JSON(res)
	}
}

func AddServer(c *fiber.Ctx) error {
	if checkServerPermission(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "You are not allowed to add a server",
		})
	}
	id := uuid.New().String()
	var server model.Server
	server.Id = id
	if err := dao.AddServer(server); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	return c.JSON(fiber.Map{
		"id": id,
	})
}

func DeleteServer(c *fiber.Ctx) error {
	if checkServerPermission(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "You are not allowed to delete a server",
		})
	}
	if err := dao.DeleteServer(c.Params("id")); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	return c.SendStatus(fiber.StatusOK)
}

func UpdateServer(c *fiber.Ctx) error {
	if checkServerPermission(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "You are not allowed to update a server",
		})
	}
	var server model.Server
	if err := c.BodyParser(&server); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	if err := dao.UpdateServer(server); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	return c.SendStatus(fiber.StatusOK)
}
