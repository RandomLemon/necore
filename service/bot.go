package service

import (
	"encoding/json"
	"fmt"
	"necore/config"
	"necore/dao"
	"necore/ws"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type botWSMessage struct {
	Type  string `json:"type"`
	Event string `json:"event"`
}

func botHeartbeatTimeout() time.Duration {
	seconds, err := strconv.Atoi(config.Config("BOT_HEARTBEAT_TIMEOUT_SECONDS"))
	if err != nil || seconds < 10 {
		seconds = 90
	}
	return time.Duration(seconds) * time.Second
}

func watchBotHeartbeat(
	client *ws.Client,
	timeout time.Duration,
	done <-chan struct{},
	unregister func(reason string, unexpected bool),
) {
	checkInterval := timeout / 3
	if checkInterval < 5*time.Second {
		checkInterval = 5 * time.Second
	}

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if client.HeartbeatAge() > timeout {
				unregister(
					fmt.Sprintf("心跳超时（超过 %s 未收到心跳）", timeout),
					true,
				)
				return
			}
		}
	}
}

func BotConectionChecker(c *fiber.Ctx) error {
	auth := strings.Clone(c.Get(fiber.HeaderAuthorization))
	identifier := strings.TrimSpace(strings.Clone(c.Params("identifier")))

	if identifier == "" {
		ws.GlobalHub.AddLog(
			"⚠️ 拒绝未知 Bot 连接：缺少 identifier",
			ws.ERROR,
		)

		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Missing bot identifier",
		})
	}

	if auth == "" {
		ws.GlobalHub.AddLog(
			fmt.Sprintf(
				"⚠️ 拒绝 %s 连接：缺少 Authorization",
				ws.WRNLogMsg(identifier),
			),
			ws.ERROR,
		)

		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Missing Authorization",
		})
	}

	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(auth, bearerPrefix) {
		ws.GlobalHub.AddLog(
			fmt.Sprintf(
				"⚠️ 拒绝 %s 连接：Authorization 格式错误",
				ws.WRNLogMsg(identifier),
			),
			ws.ERROR,
		)

		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Invalid Authorization format",
		})
	}

	plainToken := strings.TrimSpace(strings.Clone(strings.TrimPrefix(auth, bearerPrefix)))
	if plainToken == "" {
		ws.GlobalHub.AddLog(
			fmt.Sprintf(
				"⚠️ 拒绝 %s 连接：Token 为空",
				ws.WRNLogMsg(identifier),
			),
			ws.ERROR,
		)

		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Empty bot token",
		})
	}

	token, err := dao.GetBotTokenByPlainToken(plainToken)
	if err != nil {
		ws.GlobalHub.AddLog(
			fmt.Sprintf(
				"⚠️ 拒绝 %s 连接：无效 Token",
				ws.WRNLogMsg(identifier),
			),
			ws.ERROR,
		)

		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Invalid bot token",
		})
	}

	c.Locals("token_id", token.ID)
	c.Locals("token_name", strings.Clone(token.Name))
	c.Locals("identifier", strings.Clone(identifier))

	return c.Next()
}

func HandleWSConnection(c *websocket.Conn) {
	tokenId := c.Locals("token_id").(uint)
	tokenName := strings.Clone(c.Locals("token_name").(string))
	identifier := strings.Clone(c.Locals("identifier").(string))

	sessionID := uuid.New().String()
	client := &ws.Client{
		SessionID:  sessionID,
		Identifier: identifier,
		TokenID:    tokenId,
		TokenName:  tokenName,
		Connected:  time.Now().Format("2006-01-02 15:04:05"),
		Conn:       c,
	}
	client.TouchHeartbeat()

	ws.GlobalHub.Register(client)

	done := make(chan struct{})
	var unregisterOnce sync.Once

	unregister := func(reason string, unexpected bool) {
		unregisterOnce.Do(func() {
			close(done)
			ws.GlobalHub.Unregister(sessionID, reason, unexpected)
		})
	}

	go watchBotHeartbeat(client, botHeartbeatTimeout(), done, unregister)

	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			reason := "连接中断"
			unexpected := false

			if err.Error() == "websocket: close sent" {
				reason = "客户端主动断开"
			} else if err.Error() == "websocket: close received" {
				reason = "客户端被动断开"
			} else if err.Error() == "websocket: bad handshake" {
				reason = "握手失败"
			} else if err.Error() == "websocket: unexpected EOF" {
				reason = "连接中断"
				unexpected = true
			} else {
				reason = "未知错误"
			}

			unregister(reason, unexpected)
			return
		}

		var payload botWSMessage
		if err := json.Unmarshal(message, &payload); err == nil {
			if payload.Type == "heartbeat" || payload.Event == "heartbeat" {
				client.TouchHeartbeat()
				continue
			}
		}
	}
}

func GetWSStatus(c *fiber.Ctx) error {
	clients, logs := ws.GlobalHub.GetDashboardStats()
	return c.JSON(fiber.Map{
		"online_count": len(clients),
		"connections":  clients,
		"logs":         logs,
	})
}

func KickConnection(c *fiber.Ctx) error {
	if checkBotTokenPermission(c) {
		return c.SendStatus(fiber.StatusForbidden)
	}
	sessionID := c.Params("session_id")
	ws.GlobalHub.Unregister(sessionID, "强制断开连接", false)
	return c.SendStatus(fiber.StatusOK)
}
