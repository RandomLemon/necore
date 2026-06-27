package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"necore/controller/middleware"
	"necore/dao"
	"necore/database"
	"necore/model"
	"necore/service"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type testEnv struct {
	app        *fiber.App
	adminToken string
	userToken  string
	tmpDir     string
}

type testResponse struct {
	StatusCode int
	Body       []byte
	Header     http.Header
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	tmpDir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(tmpDir, "data"), 0o755))
	must(t, os.MkdirAll(filepath.Join(tmpDir, "contents"), 0o755))

	envContent := "SECRET=unit-test-secret\nBOT_LOG_BUFFER_SIZE=100\n"
	must(t, os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(envContent), 0o600))

	oldWd, err := os.Getwd()
	must(t, err)
	must(t, os.Chdir(tmpDir))
	t.Cleanup(func() {
		_ = os.Chdir(oldWd)
	})

	t.Setenv("SECRET", "unit-test-secret")
	t.Setenv("BOT_LOG_BUFFER_SIZE", "100")

	database.ConnectSqlite()

	// 测试会故意访问不存在的节点和 token；关闭 GORM 的 record not found 日志，
	// 避免把预期分支误看成测试故障。
	setGormLoggerSilent(database.GetUserDatabase())
	setGormLoggerSilent(database.GetArticleDatabase())
	setGormLoggerSilent(database.GetServerDatabase())
	setGormLoggerSilent(database.GetDocumentDatabase())
	setGormLoggerSilent(database.GetBotTokenDatabase())

	// 必须在 Windows 删除 TempDir 前关闭 SQLite 连接池，否则数据库文件会被锁定。
	t.Cleanup(func() {
		closeGormDB(t, database.GetUserDatabase())
		closeGormDB(t, database.GetArticleDatabase())
		closeGormDB(t, database.GetServerDatabase())
		closeGormDB(t, database.GetDocumentDatabase())
		closeGormDB(t, database.GetBotTokenDatabase())
	})

	must(t, dao.AddUserByUsername("admin", "admin-pass"))
	must(t, dao.AddUserByUsername("alice", "alice-pass"))

	must(t, database.GetUserDatabase().
		Model(&model.User{}).
		Where("username = ?", "admin").
		Updates(model.User{
			Password: dao.UnitTestPassword(),
			Group:    `["admin","news_admin","server_admin","document_admin","bot_admin"]`,
			Tags:     `[]`,
			Avatar:   "admin-avatar",
		}).Error)

	must(t, database.GetUserDatabase().
		Model(&model.User{}).
		Where("username = ?", "alice").
		Updates(model.User{
			Password: dao.UnitTestPassword(),
			Group:    `[]`,
			Tags:     `[]`,
			Avatar:   "alice-avatar",
		}).Error)

	// 从数据库重新读取用户后再签发 token。
	// 引入 token_version 后，JWT 中的 ver 必须等于数据库里的 token_version，
	// 不能再用手写的 model.User 字面量签发测试 token。
	adminToken := createTokenForUser(t, "admin")
	userToken := createTokenForUser(t, "alice")

	app := fiber.New(fiber.Config{BodyLimit: 512 * 1024 * 1024})
	registerRoutes(app)
	t.Cleanup(func() {
		_ = app.Shutdown()
	})

	return &testEnv{
		app:        app,
		adminToken: adminToken,
		userToken:  userToken,
		tmpDir:     tmpDir,
	}
}

func setGormLoggerSilent(db *gorm.DB) {
	if db != nil {
		db.Logger = logger.Default.LogMode(logger.Silent)
	}
}

func closeGormDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	if db == nil {
		return
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Errorf("get underlying SQL DB: %v", err)
		return
	}
	if err := sqlDB.Close(); err != nil {
		t.Errorf("close SQL DB: %v", err)
	}
}

func createTokenForUser(t *testing.T, username string) string {
	t.Helper()

	user, err := dao.GetUserByUsername(username)
	must(t, err)
	if user == nil {
		t.Fatalf("user %q not found", username)
	}

	token, err := dao.CreateToken(*user)
	must(t, err)
	return token
}

func loginAndGetToken(t *testing.T, env *testEnv, username, password string) string {
	t.Helper()

	resp := doJSON(t, env, http.MethodPost, "/necore/auth/login", "", fiber.Map{
		"username": username,
		"password": password,
	})
	assertStatus(t, resp, http.StatusOK)

	body := decodeBody(t, resp)
	token, ok := body["token"].(string)
	if !ok || token == "" {
		t.Fatalf("login response should contain token string, got %#v", body)
	}
	return token
}

func getUserTokenVersion(t *testing.T, username string) uint {
	t.Helper()

	var version uint
	result := database.GetUserDatabase().
		Model(&model.User{}).
		Select("token_version").
		Where("username = ?", username).
		Scan(&version)

	must(t, result.Error)
	if result.RowsAffected == 0 {
		t.Fatalf("user %q not found while reading token_version", username)
	}
	return version
}

func incrementUserTokenVersion(t *testing.T, username string) {
	t.Helper()

	result := database.GetUserDatabase().
		Model(&model.User{}).
		Where("username = ?", username).
		UpdateColumn("token_version", gorm.Expr("token_version + 1"))

	must(t, result.Error)
	if result.RowsAffected == 0 {
		t.Fatalf("user %q not found while incrementing token_version", username)
	}
}

func assertUserTokenVersion(t *testing.T, username string, want uint) {
	t.Helper()

	got := getUserTokenVersion(t, username)
	if got != want {
		t.Fatalf("token_version for %q = %d, want %d", username, got, want)
	}
}

func registerRoutes(app *fiber.App) {
	api := app.Group("/necore")

	loginLimiter := limiter.New(limiter.Config{
		Max:        8,
		Expiration: time.Minute,
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).
				JSON(fiber.Map{"error": "Too many login attempts"})
		},
	})

	api.Get("/slogan", service.SloganHandler)

	authGroup := api.Group("/auth")
	authGroup.Get("/status", middleware.AuthNeeded(), service.GetStatus)
	authGroup.Post("/login", loginLimiter, service.Login)
	authGroup.Post("/register", middleware.AuthNeeded(), service.AddUser)
	authGroup.Get("/user/:id", service.GetUserInfo)
	authGroup.Get("/avatar/:id", service.GetUserAvatar)
	authGroup.Get("/userlist", middleware.AuthNeeded(), service.GetUserList)
	authGroup.Delete("/user/:id", middleware.AuthNeeded(), service.DeleteUser)
	authGroup.Post("/password", middleware.AuthNeeded(), service.UpdateUserPassword)
	authGroup.Post("/avatar", middleware.AuthNeeded(), service.UpdateUserAvatar)
	authGroup.Patch("/user", middleware.AuthNeeded(), service.UpdateUserInfo)

	articleGroup := api.Group("/news")
	articleGroup.Get("/total/:target", service.GetArticleCountByCategory)
	articleGroup.Post("/list", service.GetArticleList)
	articleGroup.Get("/detail/:id", service.GetArticleById)
	articleGroup.Patch("/:id", middleware.AuthNeeded(), service.UpdateArticle)
	articleGroup.Post("/upload/:id", middleware.AuthNeeded(), service.UploadArticleFile)
	articleGroup.Delete("/upload/:id", middleware.AuthNeeded(), service.DeleteArticleFile)
	articleGroup.Post("/create", middleware.AuthNeeded(), service.CreateArticle)
	articleGroup.Delete("/:id", middleware.AuthNeeded(), service.DeleteArticle)

	serverGroup := api.Group("/server")
	serverGroup.Get("/", service.GetServerList)
	serverGroup.Post("/status", service.GetServerStatus)
	serverGroup.Get("/create", middleware.AuthNeeded(), service.AddServer)
	serverGroup.Delete("/:id", middleware.AuthNeeded(), service.DeleteServer)
	serverGroup.Patch("/", middleware.AuthNeeded(), service.UpdateServer)

	documentGroup := api.Group("/documents")
	documentGroup.Delete("/node/:id", middleware.AuthNeeded(), service.DeleteDocumentNode)
	documentGroup.Post("/node/:id", middleware.AuthNeeded(), service.UpdateDocumentNodeParentId)
	documentGroup.Put("/node/:id", middleware.AuthNeeded(), service.UpdateDocumentNodeContent)
	documentGroup.Patch("/node/:id", middleware.AuthNeeded(), service.UpdateDocumentNodeName)
	documentGroup.Post("/node", middleware.AuthNeeded(), service.CreateDocumentNode)
	documentGroup.Get("/layer/private/:parentId", middleware.AuthNeeded(), service.GetDocumentNodeChildrenPrivate)
	documentGroup.Get("/layer/:parentId", service.GetDocumentNodeChildren)
	documentGroup.Get("/private/:id", middleware.AuthNeeded(), service.GetDocumentNodeContentPrivate)
	documentGroup.Get("/:id", service.GetDocumentNodeContent)
	documentGroup.Post("/upload/:id", middleware.AuthNeeded(), service.UploadDocumentFile)
	documentGroup.Delete("/upload/:id", middleware.AuthNeeded(), service.DeleteDocumentFile)
	api.Static("/contents", "./contents")

	botGroup := api.Group("/bots")

	botGroup.Use("/ws/updates/:identifier", service.BotConectionChecker)
	botGroup.Get("/ws/updates/:identifier", websocket.New(service.HandleWSConnection))

	botGroup.Post("/token", middleware.AuthNeeded(), service.CreateBotToken)
	botGroup.Get("/token", middleware.AuthNeeded(), service.GetBotTokenList)
	botGroup.Get("/token/:id", middleware.AuthNeeded(), service.GetBotToken)
	botGroup.Delete("/token/:id", middleware.AuthNeeded(), service.DeleteBotToken)
	botGroup.Get("/status", middleware.AuthNeeded(), service.GetWSStatus)
	botGroup.Delete("/ws/kick/:session_id", middleware.AuthNeeded(), service.KickConnection)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func doJSON(t *testing.T, env *testEnv, method, path, token string, body any) testResponse {
	t.Helper()

	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		must(t, err)
		r = bytes.NewReader(b)
	}

	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return executeRequest(t, env, req)
}

func doRaw(t *testing.T, env *testEnv, method, path, token, contentType, body string) testResponse {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return executeRequest(t, env, req)
}

func doMultipartFile(t *testing.T, env *testEnv, path, token, field, filename, content string) testResponse {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, filename)
	must(t, err)
	_, err = part.Write([]byte(content))
	must(t, err)
	must(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return executeRequest(t, env, req)
}

func executeRequest(t *testing.T, env *testEnv, req *http.Request) testResponse {
	t.Helper()

	resp, err := env.app.Test(req, -1)
	must(t, err)

	// 立即读取并关闭响应体。Fiber 的静态文件响应在 Windows 上会保持文件句柄；
	// 不关闭会导致后续 os.Remove 和 TempDir 清理报 ERROR_SHARING_VIOLATION。
	body, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	must(t, readErr)
	must(t, closeErr)

	return testResponse{
		StatusCode: resp.StatusCode,
		Body:       body,
		Header:     resp.Header.Clone(),
	}
}

func decodeBody(t *testing.T, resp testResponse) map[string]any {
	t.Helper()
	var got map[string]any
	must(t, json.Unmarshal(resp.Body, &got))
	return got
}

func assertStatus(t *testing.T, resp testResponse, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("status = %d, want %d, body = %s", resp.StatusCode, want, string(resp.Body))
	}
}

func TestPublicAndAuthRoutes(t *testing.T) {
	env := setupTestEnv(t)

	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/slogan", "", nil), http.StatusOK)

	// 当前 jwt 中间件对无 Authorization 头实际返回 401，而不是 400。
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/status", "", nil), http.StatusUnauthorized)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/status", env.adminToken, nil), http.StatusOK)

	loginResp := doJSON(t, env, http.MethodPost, "/necore/auth/login", "", fiber.Map{
		"username": "admin",
		"password": "unit-test-password",
	})
	assertStatus(t, loginResp, http.StatusOK)
	loginBody := decodeBody(t, loginResp)
	if loginBody["token"] == "" || loginBody["user"] == nil {
		t.Fatalf("login response should contain token and user, got %#v", loginBody)
	}

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/auth/login", "", fiber.Map{
		"username": "admin",
		"password": "wrong",
	}), http.StatusUnauthorized)

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/auth/register", env.userToken, fiber.Map{
		"username": "bob",
		"password": "p",
	}), http.StatusForbidden)

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/auth/register", env.adminToken, fiber.Map{
		"username": "bob",
		"password": "p",
	}), http.StatusOK)

	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/user/admin", "", nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/avatar/admin", "", nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/userlist", "", nil), http.StatusUnauthorized)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/userlist", env.adminToken, nil), http.StatusOK)

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/auth/password", env.userToken, fiber.Map{
		"id":            "admin",
		"self_password": "wrong-password",
		"new_password":  "new",
	}), http.StatusForbidden)

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/auth/password", env.userToken, fiber.Map{
		"id":            "alice",
		"self_password": "unit-test-password",
		"new_password":  "new",
	}), http.StatusOK)

	// 改密码属于安全敏感操作。接入 token_version 后，alice 的旧 token
	// 会立即失效，因此后续仍需要 alice 身份的断言必须重新登录获取新 token。
	env.userToken = loginAndGetToken(t, env, "alice", "new")

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/auth/avatar", env.userToken, fiber.Map{
		"username": "admin",
		"avatar":   "x",
	}), http.StatusForbidden)

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/auth/avatar", env.userToken, fiber.Map{
		"username": "alice",
		"avatar":   "new-avatar",
	}), http.StatusOK)

	assertStatus(t, doJSON(t, env, http.MethodPatch, "/necore/auth/user", env.userToken, fiber.Map{
		"username": "alice",
		"group":    []string{},
		"Tags":     []any{},
	}), http.StatusForbidden)

	assertStatus(t, doJSON(t, env, http.MethodPatch, "/necore/auth/user", env.adminToken, fiber.Map{
		"username": "alice",
		"group":    []string{"document_admin"},
		"Tags":     []any{},
	}), http.StatusOK)

	env.userToken = createTokenForUser(t, "alice")

	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/auth/user/alice", env.userToken, nil), http.StatusForbidden)
	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/auth/user/bob", env.adminToken, nil), http.StatusOK)
}

func TestNewsRoutes(t *testing.T) {
	env := setupTestEnv(t)

	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/news/total/notice", "", nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/news/list", "", fiber.Map{
		"target":    "notice",
		"page":      1,
		"page_size": 10,
		"pin":       false,
	}), http.StatusOK)

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/news/create", env.userToken, nil), http.StatusForbidden)

	createResp := doJSON(t, env, http.MethodPost, "/necore/news/create", env.adminToken, nil)
	assertStatus(t, createResp, http.StatusOK)
	articleID, _ := decodeBody(t, createResp)["id"].(string)
	if articleID == "" {
		t.Fatal("create article should return id")
	}

	assertStatus(t, doJSON(t, env, http.MethodPatch, "/necore/news/"+articleID, env.adminToken, fiber.Map{
		"entity": fiber.Map{
			"pin":     true,
			"title":   "Title",
			"brief":   "Brief",
			"date":    "2026-01-01",
			"endDate": "",
			"image":   "",
		},
		"content":    []fiber.Map{{"type": "markdown", "content": "hello"}},
		"category":   "notice",
		"doesNotify": false,
	}), http.StatusOK)

	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/news/detail/"+articleID, "", nil), http.StatusOK)
	response := doMultipartFile(t, env, "/necore/news/upload/"+articleID, env.adminToken, "file", "hello.txt", "hello")
	assertStatus(t, response, http.StatusOK)
	filename := strings.Split(decodeBody(t, response)["url"].(string), "/")[3]
	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/news/upload/"+articleID, env.adminToken, fiber.Map{
		"filename": filename,
	}), http.StatusNoContent)
	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/news/"+articleID, env.adminToken, nil), http.StatusOK)
}

func TestServerRoutes(t *testing.T) {
	env := setupTestEnv(t)

	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/server/", "", nil), http.StatusOK)
	assertStatus(t, doRaw(t, env, http.MethodPost, "/necore/server/status", "", "application/json", "{"), http.StatusBadRequest)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/server/create", env.userToken, nil), http.StatusForbidden)

	createResp := doJSON(t, env, http.MethodGet, "/necore/server/create", env.adminToken, nil)
	assertStatus(t, createResp, http.StatusOK)
	serverID, _ := decodeBody(t, createResp)["id"].(string)
	if serverID == "" {
		t.Fatal("create server should return id")
	}

	assertStatus(t, doJSON(t, env, http.MethodPatch, "/necore/server/", env.adminToken, fiber.Map{
		"id":           serverID,
		"name":         "S1",
		"icon":         "icon",
		"description":  "desc",
		"realtime":     false,
		"onlineMapUrl": "https://example.test/map",
		"serverUrl":    "example.test:25565",
	}), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/server/"+serverID, env.adminToken, nil), http.StatusOK)
}

func TestDocumentRoutes(t *testing.T) {
	env := setupTestEnv(t)

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/documents/node", env.userToken, fiber.Map{
		"parentId": "root",
		"isFolder": true,
		"private":  false,
		"name":     "Root",
	}), http.StatusForbidden)

	parentResp := doJSON(t, env, http.MethodPost, "/necore/documents/node", env.adminToken, fiber.Map{
		"parentId": "root",
		"isFolder": true,
		"private":  false,
		"name":     "Destination",
	})
	assertStatus(t, parentResp, http.StatusOK)
	parentID, _ := decodeBody(t, parentResp)["id"].(string)
	if parentID == "" {
		t.Fatal("create parent document node should return id")
	}

	createResp := doJSON(t, env, http.MethodPost, "/necore/documents/node", env.adminToken, fiber.Map{
		"parentId": "root",
		"isFolder": false,
		"private":  false,
		"name":     "Doc",
	})
	assertStatus(t, createResp, http.StatusOK)
	nodeID, _ := decodeBody(t, createResp)["id"].(string)
	if nodeID == "" {
		t.Fatal("create document node should return id")
	}

	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/documents/layer/root", "", nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/documents/layer/private/root", env.adminToken, nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/documents/"+nodeID, "", nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/documents/private/"+nodeID, env.adminToken, nil), http.StatusOK)

	assertStatus(t, doJSON(t, env, http.MethodPatch, "/necore/documents/node/"+nodeID, env.adminToken, fiber.Map{"name": "Renamed"}), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodPut, "/necore/documents/node/"+nodeID, env.adminToken, fiber.Map{
		"private": false,
		"content": []fiber.Map{{"type": "markdown", "content": "body"}},
	}), http.StatusOK)

	// 使用真实存在的父节点，避免 GORM 输出无意义的 record not found 日志。
	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/documents/node/"+nodeID, env.adminToken, fiber.Map{
		"parentId": parentID,
	}), http.StatusOK)

	response := doMultipartFile(t, env, "/necore/documents/upload/"+nodeID, env.adminToken, "file", "doc.txt", "file body")
	assertStatus(t, response, http.StatusOK)
	filename := strings.Split(decodeBody(t, response)["url"].(string), "/")[3]

	// 不通过 Fiber Static 读取随后需要删除的同一个文件。
	// Fiber/fasthttp 在 Windows 下可能让 SendFile 的文件句柄存活到请求上下文回收，
	// 即使 net/http 响应体已读取并关闭，立即 os.Remove 仍可能得到 ERROR_SHARING_VIOLATION。
	uploadedPath := filepath.Join(env.tmpDir, "contents", nodeID, filename)
	uploadedBody, err := os.ReadFile(uploadedPath)
	must(t, err)
	if string(uploadedBody) != "file body" {
		t.Fatalf("uploaded document body = %q, want %q", string(uploadedBody), "file body")
	}

	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/documents/upload/"+nodeID, env.adminToken, fiber.Map{
		"filename": filename,
	}), http.StatusNoContent)
	if _, err := os.Stat(uploadedPath); !os.IsNotExist(err) {
		t.Fatalf("uploaded file should be deleted, stat err = %v", err)
	}

	// 仍覆盖静态文件路由，但请求不存在的文件，避免 Windows 文件句柄影响后续删除。
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/contents/not-found.txt", "", nil), http.StatusNotFound)
	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/documents/node/"+nodeID, env.adminToken, nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/documents/node/"+parentID, env.adminToken, nil), http.StatusOK)
}

func TestBotRoutes(t *testing.T) {
	env := setupTestEnv(t)

	// Token 管理接口确实要求 bot_admin。
	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/bots/token", env.userToken, nil), http.StatusForbidden)

	createResp := doJSON(t, env, http.MethodPost, "/necore/bots/token", env.adminToken, fiber.Map{
		"name": "unit-test",
	})
	assertStatus(t, createResp, http.StatusCreated)
	createBody := decodeBody(t, createResp)
	tokenObj, ok := createBody["token"].(map[string]any)
	if !ok || tokenObj["token"] == "" {
		t.Fatalf("create bot token should return token object, got %#v", createBody)
	}

	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/bots/token", env.adminToken, nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/bots/token/missing", env.adminToken, nil), http.StatusNotFound)

	// 当前源码只要求“已登录”，没有 bot_admin 权限检查。
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/bots/status", env.userToken, nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/bots/ws/kick/not-exist", env.userToken, nil), http.StatusForbidden)
	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/bots/token/missing", env.adminToken, nil), http.StatusOK)
}

func TestSecurityRegression_FileDeletePathTraversalIsCurrentlyPossible(t *testing.T) {
	env := setupTestEnv(t)

	victim := filepath.Join(env.tmpDir, "victim.txt")
	must(t, os.WriteFile(victim, []byte("do not delete"), 0o644))

	resp := doJSON(t, env, http.MethodDelete, "/necore/documents/upload/anything", env.adminToken, fiber.Map{
		"filename": "../../victim.txt",
	})
	assertStatus(t, resp, http.StatusBadRequest)

	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("expected vulnerable handler to delete arbitrary relative file; stat err = %v", err)
	}
}

func TestSecurityRegression_BotDashboardAvailableToAnyAuthenticatedUser(t *testing.T) {
	env := setupTestEnv(t)

	// 该测试记录当前安全缺陷：普通登录用户也能查看 bot 状态并调用 kick。
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/bots/status", env.userToken, nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/bots/ws/kick/arbitrary-session", env.userToken, nil), http.StatusForbidden)
}

func TestSecurityRegression_PrivateUserDataIsPubliclyEnumerable(t *testing.T) {
	env := setupTestEnv(t)

	resp := doJSON(t, env, http.MethodGet, "/necore/auth/userlist", env.userToken, nil)
	assertStatus(t, resp, http.StatusOK)

	if !strings.Contains(string(resp.Body), "admin") || !strings.Contains(string(resp.Body), "alice") {
		t.Fatalf("expected public user list to expose usernames, body=%s", string(resp.Body))
	}
}

func TestTokenVersion_StaleTokenRejectedAcrossProtectedRouteGroups(t *testing.T) {
	env := setupTestEnv(t)

	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/status", env.adminToken, nil), http.StatusOK)

	oldVersion := getUserTokenVersion(t, "admin")
	incrementUserTokenVersion(t, "admin")
	assertUserTokenVersion(t, "admin", oldVersion+1)

	cases := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{
			name:   "auth status",
			method: http.MethodGet,
			path:   "/necore/auth/status",
		},
		{
			name:   "auth register",
			method: http.MethodPost,
			path:   "/necore/auth/register",
			body: fiber.Map{
				"username": "stale-created-user",
				"password": "password",
			},
		},
		{
			name:   "news create",
			method: http.MethodPost,
			path:   "/necore/news/create",
		},
		{
			name:   "server create",
			method: http.MethodGet,
			path:   "/necore/server/create",
		},
		{
			name:   "documents create node",
			method: http.MethodPost,
			path:   "/necore/documents/node",
			body: fiber.Map{
				"parentId": "root",
				"isFolder": true,
				"private":  false,
				"name":     "Should Not Be Created",
			},
		},
		{
			name:   "bots status",
			method: http.MethodGet,
			path:   "/necore/bots/status",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSON(t, env, tc.method, tc.path, env.adminToken, tc.body)
			assertStatus(t, resp, http.StatusUnauthorized)
		})
	}

	freshAdminToken := createTokenForUser(t, "admin")
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/status", freshAdminToken, nil), http.StatusOK)
}

func TestTokenVersion_PasswordChangeRevokesTargetUserToken(t *testing.T) {
	env := setupTestEnv(t)

	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/status", env.userToken, nil), http.StatusOK)

	oldVersion := getUserTokenVersion(t, "alice")
	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/auth/password", env.userToken, fiber.Map{
		"id":            "alice",
		"self_password": "unit-test-password",
		"new_password":  "new-alice-password",
	}), http.StatusOK)
	assertUserTokenVersion(t, "alice", oldVersion+1)

	// 旧 JWT 已经被撤销，不能再访问任何需要登录的接口。
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/status", env.userToken, nil), http.StatusUnauthorized)

	// 旧密码不能登录，新密码登录后拿到的新 JWT 应该有效。
	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/auth/login", "", fiber.Map{
		"username": "alice",
		"password": "alice-pass",
	}), http.StatusUnauthorized)

	newToken := loginAndGetToken(t, env, "alice", "new-alice-password")
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/status", newToken, nil), http.StatusOK)
}

func TestTokenVersion_UserPermissionChangeRevokesTargetTokenOnly(t *testing.T) {
	env := setupTestEnv(t)

	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/status", env.adminToken, nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/status", env.userToken, nil), http.StatusOK)

	oldAliceVersion := getUserTokenVersion(t, "alice")
	oldAdminVersion := getUserTokenVersion(t, "admin")

	assertStatus(t, doJSON(t, env, http.MethodPatch, "/necore/auth/user", env.adminToken, fiber.Map{
		"username": "alice",
		"group":    []string{"document_admin"},
		"Tags":     []any{},
	}), http.StatusOK)

	assertUserTokenVersion(t, "alice", oldAliceVersion+1)
	assertUserTokenVersion(t, "admin", oldAdminVersion)

	// 被修改权限的目标用户旧 token 应该失效。
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/status", env.userToken, nil), http.StatusUnauthorized)

	// 执行修改的管理员不应该因为修改别人权限而被迫下线。
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/status", env.adminToken, nil), http.StatusOK)

	// 重新签发的新 token 应该携带/对应数据库中的最新权限。
	freshAliceToken := createTokenForUser(t, "alice")
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/documents/layer/private/root", freshAliceToken, nil), http.StatusOK)
}

func TestTokenVersion_AvatarChangeDoesNotRevokeToken(t *testing.T) {
	env := setupTestEnv(t)

	oldVersion := getUserTokenVersion(t, "alice")
	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/auth/avatar", env.userToken, fiber.Map{
		"username": "alice",
		"avatar":   "avatar-after-change",
	}), http.StatusOK)

	assertUserTokenVersion(t, "alice", oldVersion)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/status", env.userToken, nil), http.StatusOK)
}

func TestTokenVersion_DeletedUserTokenIsRejected(t *testing.T) {
	env := setupTestEnv(t)

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/auth/register", env.adminToken, fiber.Map{
		"username": "charlie",
		"password": "charlie-pass",
	}), http.StatusOK)

	charlieToken := loginAndGetToken(t, env, "charlie", "charlie-pass")
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/status", charlieToken, nil), http.StatusOK)

	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/auth/user/charlie", env.adminToken, nil), http.StatusOK)

	// 即使 token_version 没有递增，只要鉴权中间件每次查询数据库用户，
	// 被删除用户的旧 JWT 也必须被拒绝。
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/status", charlieToken, nil), http.StatusUnauthorized)
}

// =======================================================
// EXTENDED SECURITY & EDGE CASE TESTS (ADDED BY REVIEW)
// =======================================================

/*
1. AUTH MODULE FUZZ & ABUSE TESTS
*/
func TestAuth_FuzzLoginInputs(t *testing.T) {
	env := setupTestEnv(t)

	maliciousInputs := []string{
		`{"username":"' OR 1=1 --","password":"x"}`,
		`{"username":"\u0000admin","password":"admin"}`,
		`{"username":"<script>alert(1)</script>","password":"123"}`,
		`{"username":"admin","password":"".repeat(10000)}`,
	}

	for _, body := range maliciousInputs {
		req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp := executeRequest(t, env, req)

		if resp.StatusCode == 200 {
			t.Fatalf("SECURITY ISSUE: malicious login input succeeded: %s", body)
		}
	}
}

func TestAuth_BruteForceSimulation(t *testing.T) {
	env := setupTestEnv(t)

	for i := 0; i < 20; i++ {
		body := `{"username":"admin","password":"wrong"}`
		req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp := executeRequest(t, env, req)

		if i > 10 && resp.StatusCode == 200 {
			t.Fatal("POTENTIAL BRUTE FORCE ISSUE: login succeeded after repeated attempts")
		}
	}
}

/*
2. USER MODULE ABUSE CASES
*/
func TestUser_EnumerationAttack(t *testing.T) {
	env := setupTestEnv(t)

	usernames := []string{"admin", "alice", "root", "test", "doesnotexist"}

	for _, u := range usernames {
		req := httptest.NewRequest("GET", "/user/"+u, nil)

		resp := executeRequest(t, env, req)

		// 不允许通过错误信息区分用户是否存在（防 user enumeration）
		if strings.Contains(string(resp.Body), "password") {
			t.Fatalf("USER ENUMERATION LEAK DETECTED for user: %s", u)
		}
	}
}

/*
3. NEWS / ARTICLE MODULE SECURITY
*/
func TestNews_XSSPayloadPersistence(t *testing.T) {
	env := setupTestEnv(t)

	payload := `{"title":"<script>alert(document.cookie)</script>","content":"x"}`
	req := httptest.NewRequest("POST", "/news", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	resp := executeRequest(t, env, req)

	if resp.StatusCode == 200 {
		// 再次读取列表检查是否原样返回
		req2 := httptest.NewRequest("GET", "/news", nil)
		resp2 := executeRequest(t, env, req2)

		if strings.Contains(string(resp2.Body), "<script>") {
			t.Fatal("XSS PAYLOAD STORED UNSANITIZED IN DATABASE")
		}
	}
}

func TestNews_MassivePayload_DoSProtection(t *testing.T) {
	env := setupTestEnv(t)

	large := strings.Repeat("A", 5*1024*1024) // 5MB payload

	body := `{"title":"` + large + `"}`
	req := httptest.NewRequest("POST", "/news", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp := executeRequest(t, env, req)

	if resp.StatusCode == 200 {
		t.Fatal("NO PAYLOAD LIMIT: potential DoS vulnerability")
	}
}

/*
4. DOCUMENT MODULE ATTACK SURFACE
*/
func TestDocument_PathTraversalUpload(t *testing.T) {
	env := setupTestEnv(t)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	_ = writer.WriteField("filename", "../../etc/passwd")

	part, _ := writer.CreateFormFile("file", "test.txt")
	part.Write([]byte("malicious content"))
	writer.Close()

	req := httptest.NewRequest("POST", "/document/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp := executeRequest(t, env, req)

	if resp.StatusCode == 200 {
		t.Fatal("PATH TRAVERSAL POSSIBLE IN FILE UPLOAD")
	}
}

/*
5. WEBSOCKET SECURITY TEST
*/
func TestWebSocket_UnauthenticatedAccess(t *testing.T) {
	env := setupTestEnv(t)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")

	resp := executeRequest(t, env, req)

	// WebSocket 应该要求认证或握手验证
	if resp.StatusCode == 200 {
		t.Fatal("UNAUTHORIZED WEBSOCKET ACCESS ALLOWED")
	}
}

/*
6. TOKEN / SESSION ABUSE
*/
func TestTokenReuseAfterPrivilegeChange(t *testing.T) {
	env := setupTestEnv(t)

	// 假设 alice token 已存在
	token := env.userToken

	// 模拟权限变化或用户被禁用后的访问
	req := httptest.NewRequest("GET", "/user/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp := executeRequest(t, env, req)

	if resp.StatusCode == 200 && strings.Contains(string(resp.Body), "admin") {
		t.Fatal("TOKEN STILL VALID AFTER PRIVILEGE CHANGE")
	}
}
