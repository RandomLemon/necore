package service

import (
	"necore/ws"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

func HandleWSConnection(c *websocket.Conn) {
	tokenId := c.Locals("token_id").(uint)
	tokenName := c.Locals("token_name").(string)
	identifier := c.Locals("identifier").(string)

	sessionID := uuid.New().String()
	client := &ws.Client{
		SessionID:  sessionID,
		Identifier: identifier,
		TokenID:    tokenId,
		TokenName:  tokenName,
		Connected:  time.Now().Format("2006-01-02 15:04:05"),
		Conn:       c,
	}

	ws.GlobalHub.Register(client)
	reason, unexpected := "正常退出", false
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			reason = "连接中断"
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
			break
		}
	}
	ws.GlobalHub.Unregister(sessionID, reason, unexpected)
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
