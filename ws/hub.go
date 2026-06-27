package ws

import (
	"fmt"
	"html"
	"necore/config"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofiber/contrib/websocket"
)

// escapeLogText escapes dynamic text before it is placed inside an HTML log
// fragment. The dashboard currently receives logs as HTML strings, so every
// untrusted value must be escaped before it is wrapped by the colored span.
func escapeLogText(text string) string {
	return html.EscapeString(text)
}

func coloredLogMsg(color string, text string) string {
	return "<span style=\"color: " + color + ";\">" + escapeLogText(text) + "</span>"
}

func INFLogMsg(text string) string {
	return coloredLogMsg("#409EFF", text)
}

func SUCLogMsg(text string) string {
	return coloredLogMsg("#67C23A", text)
}

func WRNLogMsg(text string) string {
	return coloredLogMsg("#E6A23C", text)
}

func ERRLogMsg(text string) string {
	return coloredLogMsg("#F56C6C", text)
}

func DBGLogMsg(text string) string {
	return coloredLogMsg("#909399", text)
}

type Client struct {
	SessionID         string          `json:"session_id"`
	Identifier        string          `json:"identifier"`
	TokenID           uint            `json:"token_id"`
	TokenName         string          `json:"token_name"`
	Connected         string          `json:"connected"`
	LastHeartbeat     string          `json:"last_heartbeat"`
	LastHeartbeatUnix int64           `json:"-"`
	Conn              *websocket.Conn `json:"-"`
}

func (c *Client) TouchHeartbeat() {
	atomic.StoreInt64(&c.LastHeartbeatUnix, time.Now().Unix())
}

func (c *Client) HeartbeatAge() time.Duration {
	last := atomic.LoadInt64(&c.LastHeartbeatUnix)
	if last == 0 {
		return 0
	}
	return time.Since(time.Unix(last, 0))
}

type Hub struct {
	Clients map[string]*Client
	mu      sync.RWMutex

	Logs       []string
	logMu      sync.Mutex
	lastLogKey string
	lastLogAt  time.Time
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
	if BOT_LOG_BUFFER_SIZE <= 0 {
		BOT_LOG_BUFFER_SIZE = 1000
	}

	now := time.Now()

	h.logMu.Lock()
	defer h.logMu.Unlock()

	logKey := fmt.Sprintf("%d|%s", level, msg)

	// 5 分钟内，连续且完全相同的日志只记录一次。
	// 注意：这里故意不在抑制时刷新 lastLogAt。
	// 这样持续刷屏时，每 5 分钟最多重新出现一次，而不是永远不再出现。
	if h.lastLogKey == logKey && now.Sub(h.lastLogAt) < 5*time.Minute {
		return
	}

	h.lastLogKey = logKey
	h.lastLogAt = now

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
		now.Format("2006-01-02 15:04:05"),
		logLevelStr,
		msg,
	)

	h.Logs = append(h.Logs, message)

	if len(h.Logs) > BOT_LOG_BUFFER_SIZE {
		h.Logs = h.Logs[len(h.Logs)-BOT_LOG_BUFFER_SIZE:]
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

func (h *Hub) BroadcastToSessions(message interface{}, sessionIDs []string) int {
	if len(sessionIDs) == 0 {
		return 0
	}

	targets := make(map[string]bool, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		targets[sessionID] = true
	}

	sent := 0

	h.mu.RLock()
	defer h.mu.RUnlock()

	for sessionID, client := range h.Clients {
		if !targets[sessionID] {
			continue
		}

		if err := client.Conn.WriteJSON(message); err == nil {
			sent++
		}
	}

	return sent
}

// safeClientForDashboard returns a shallow copy whose display fields are safe
// for HTML rendering. It keeps the JSON field names and types unchanged while
// avoiding mutation of the internal Client object used by the WebSocket hub.
func safeClientForDashboard(c *Client) *Client {
	if c == nil {
		return nil
	}

	copied := *c
	copied.Identifier = escapeLogText(copied.Identifier)
	copied.TokenName = escapeLogText(copied.TokenName)

	last := atomic.LoadInt64(&c.LastHeartbeatUnix)
	if last > 0 {
		copied.LastHeartbeat = time.Unix(last, 0).Format("2006-01-02 15:04:05")
	}

	return &copied
}

func (h *Hub) GetDashboardStats() ([]*Client, []string) {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.Clients))
	for _, c := range h.Clients {
		clients = append(clients, safeClientForDashboard(c))
	}
	h.mu.RUnlock()

	h.logMu.Lock()
	logsCopy := make([]string, len(h.Logs))
	copy(logsCopy, h.Logs)
	h.logMu.Unlock()

	return clients, logsCopy
}
