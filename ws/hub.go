package ws

import (
	"fmt"
	"necore/config"
	"strconv"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
)

func INFLogMsg(text string) string {
	return "<span style=\"color: #409EFF;\">" + text + "</span>"
}

func SUCLogMsg(text string) string {
	return "<span style=\"color: #67C23A;\">" + text + "</span>"
}

func WRNLogMsg(text string) string {
	return "<span style=\"color: #E6A23C;\">" + text + "</span>"
}

func ERRLogMsg(text string) string {
	return "<span style=\"color: #F56C6C;\">" + text + "</span>"
}

func DBGLogMsg(text string) string {
	return "<span style=\"color: #909399;\">" + text + "</span>"
}

type Client struct {
	SessionID  string          `json:"session_id"`
	Identifier string          `json:"identifier"`
	TokenID    uint            `json:"token_id"`
	TokenName  string          `json:"token_name"`
	Connected  string          `json:"connected"`
	Conn       *websocket.Conn `json:"-"`
}

type Hub struct {
	Clients map[string]*Client
	mu      sync.RWMutex

	Logs  []string
	logMu sync.Mutex
}

var GlobalHub = &Hub{
	Clients: make(map[string]*Client),
	Logs:    make([]string, 0),
}

type LogLevel int

const (
	DEBUG   LogLevel = 0
	INFO    LogLevel = 1
	WARNING LogLevel = 2
	ERROR   LogLevel = 3
	SUCCESS LogLevel = 4
)

func (h *Hub) AddLog(msg string, level LogLevel) {
	BOT_LOG_BUFFER_SIZE, _ := strconv.Atoi(config.Config("BOT_LOG_BUFFER_SIZE"))
	h.logMu.Lock()
	defer h.logMu.Unlock()
	logLevelStr := ""
	switch level {
	case DEBUG:
		logLevelStr = DBGLogMsg("DBG")
	case INFO:
		logLevelStr = INFLogMsg("INF")
	case WARNING:
		logLevelStr = WRNLogMsg("WRN")
	case ERROR:
		logLevelStr = ERRLogMsg("ERR")
	case SUCCESS:
		logLevelStr = SUCLogMsg("SUC")
	}
	message := fmt.Sprintf(
		"[%v] %s | %s",
		time.Now().Format("2006-01-02 15:04:05"),
		logLevelStr,
		msg,
	)
	h.Logs = append(h.Logs, message)
	if len(h.Logs) > BOT_LOG_BUFFER_SIZE {
		h.Logs = h.Logs[:BOT_LOG_BUFFER_SIZE]
	}
}

func (h *Hub) Register(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Clients[client.SessionID] = client
	h.AddLog(
		fmt.Sprintf(
			"✅ %s 已连接，使用密钥：%s",
			WRNLogMsg(client.Identifier),
			INFLogMsg(client.TokenName),
		),
		SUCCESS,
	)
}

func (h *Hub) Unregister(sessionID, reason string, unexpected bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if client, ok := h.Clients[sessionID]; ok {
		client.Conn.Close()
		delete(h.Clients, sessionID)
		if unexpected {
			h.AddLog(
				fmt.Sprintf(
					"❌ %s 异常断开连接，原因：%s，使用密钥：%s",
					WRNLogMsg(client.Identifier),
					ERRLogMsg(reason),
					INFLogMsg(client.TokenName),
				),
				ERROR,
			)
		} else {
			h.AddLog(
				fmt.Sprintf(
					"❌ %s 断开连接，原因：%s，使用密钥：%s",
					WRNLogMsg(client.Identifier),
					ERRLogMsg(reason),
					INFLogMsg(client.TokenName),
				),
				INFO,
			)
		}
	}
}

func (h *Hub) KickByTokenID(tokenID uint) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sessionID, client := range h.Clients {
		if client.TokenID == tokenID {
			client.Conn.Close()
			delete(h.Clients, sessionID)
			h.AddLog(
				fmt.Sprintf(
					"⚠️ %s 因为密钥删除被踢出，使用密钥：%s",
					WRNLogMsg(client.Identifier),
					INFLogMsg(client.TokenName),
				),
				WARNING,
			)
		}
	}
}

func (h *Hub) Broadcast(message interface{}) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, client := range h.Clients {
		_ = client.Conn.WriteJSON(message)
	}
}

func (h *Hub) GetDashboardStats() ([]*Client, []string) {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.Clients))
	for _, c := range h.Clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	h.logMu.Lock()
	logsCopy := make([]string, len(h.Logs))
	copy(logsCopy, h.Logs)
	h.logMu.Unlock()

	return clients, logsCopy
}
