# Necore

Necore 是 NMO Ecosystem 的后端服务，为前端项目 Neco 提供用户认证、文章管理、服务器状态同步、文档管理、文件上传、Bot WebSocket 推送等能力。

项目基于 Go + Fiber + GORM + SQLite 实现，默认 API 前缀为：

```text
/necore
```

## 功能概览

- **用户与鉴权**
  - JWT 登录认证；
  - 用户创建、删除、密码修改、头像修改；
  - 用户权限组与标签管理；
  - TokenVersion 机制，用于在用户权限/密码变化后使旧 JWT 失效。

- **文章/新闻管理**
  - 支持文章创建、编辑、删除；
  - 支持文章分类、置顶、活动起止时间；
  - 支持 Markdown、图片、PDF 等内容块；
  - 支持文章附件上传与删除；
  - 支持保存文章后通过 WebSocket 向 Bot 推送 `article_updated` 事件；
  - 支持按指定 WebSocket session 定向推送。

- **服务器列表与状态同步**
  - 管理服务器名称、图标、描述、在线地图、实时状态地址；
  - 查询 Minecraft 服务器状态；
  - 返回在线人数、容量、版本、延迟、图标；
  - 可返回玩家 sample 列表，供前端展示玩家头像。

- **文档管理**
  - 树形文档节点；
  - 文件夹/文档节点；
  - 公开/私有文档；
  - 文档内容、贡献者、更新时间维护；
  - 文档附件上传与删除。

- **Bot WebSocket**
  - Bot Token 创建、列表、删除；
  - WebSocket 鉴权连接；
  - 心跳检测与超时断开；
  - 在线连接状态查询；
  - 主动踢出连接；
  - 连接日志记录；
  - 5 分钟内连续相同日志去重，避免自动重连刷屏。

## 技术栈

- Go 1.25+
- Fiber v2
- gofiber/contrib/websocket
- gofiber/contrib/jwt
- GORM
- SQLite
- mcstatusgo
- bcrypt
- JWT HS256

## 项目结构

```text
.
├── app                         # Fiber App 初始化与启动
├── config                      # .env 查找、创建、加载与默认配置
├── controller
│   ├── middleware              # JWT 鉴权中间件
│   └── router                  # API 路由注册
├── contents                    # 上传文件保存目录，需要持久化备份
├── dao                         # 数据访问层
├── data                        # SQLite 数据库目录，需要持久化备份
├── database                    # SQLite 连接与 AutoMigrate
├── model                       # GORM 模型
├── service                     # 业务逻辑层
├── util                        # token、文件名安全处理等工具
├── ws                          # Bot WebSocket Hub
├── main.go
├── go.mod
└── routes_and_security_test.go
```

## 环境要求

- Go 1.25+
- CGO 可用环境：SQLite 驱动依赖 `github.com/mattn/go-sqlite3`

安装依赖：

```bash
go mod download
```

启动开发服务：

```bash
go run .
```

运行测试：

```bash
go test ./...
```

构建：

```bash
go build -o necore .
```

## 配置

配置文件使用 `.env`。服务启动时会按顺序查找或创建配置文件：

1. 环境变量 `NECORE_CONFIG_FILE` 指定的路径；
2. 当前工作目录下的 `.env`，仅当当前目录看起来像项目根目录时使用；
3. 可执行文件同目录下的 `.env`；
4. 用户配置目录中的 `necore/.env`。

环境变量优先级高于 `.env` 文件。

常用配置：

| 配置项 | 默认值 | 说明 |
|---|---:|---|
| `PORT` | `3000` | HTTP 服务端口 |
| `SECRET` | 自动生成 | JWT 签名密钥，修改后会使已有 JWT 全部失效 |
| `BOT_LOG_BUFFER_SIZE` | `1000` 或 `.env` 中配置值 | Bot 连接日志最多保留条数 |
| `BOT_HEARTBEAT_TIMEOUT_SECONDS` | `90` | Bot 心跳超时时间，超过该时间未收到心跳则主动断开 |

`.env` 示例：

```env
PORT=3000
SECRET=please-change-me
BOT_LOG_BUFFER_SIZE=2000
BOT_HEARTBEAT_TIMEOUT_SECONDS=90
```

## 数据与备份

运行时数据默认位于：

```text
data/*.sqlite3
contents/{objectId}/*
```

建议定期备份：

- `data/user.sqlite3`
- `data/article.sqlite3`
- `data/server.sqlite3`
- `data/document.sqlite3`
- `data/bot_connection.sqlite3`
- `contents/`
- `.env`

`SECRET` 必须保密。泄漏后应立即更换，并要求用户重新登录。

## 初始管理员

当前项目没有内置公开注册入口。可以通过以下方式创建初始管理员：

1. 临时启用或编写初始化脚本调用 `dao.AddUserByUsername`；
2. 手动向 SQLite 插入用户记录；
3. 使用已有管理员调用用户创建接口。

管理员用户的 `group` 字段应包含：

```json
["admin"]
```

密码需要使用 bcrypt 哈希保存。项目中的 `dao.DebugTestPassword()` 可用于开发时打印测试密码哈希，生产环境不要启用调试输出。

## 权限组

| 权限组 | 说明 |
|---|---|
| `admin` | 超级管理员 |
| `news_admin` | 文章/新闻管理 |
| `server_admin` | 服务器列表管理 |
| `document_admin` | 文档管理 |
| `bot_admin` | Bot Token 与连接管理 |

## API 概览

所有接口默认带 `/necore` 前缀。

### 基础

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/slogan` | 获取随机标语 |

### 认证与用户

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `POST` | `/auth/login` | 无 | 登录 |
| `GET` | `/auth/status` | 登录 | 校验登录状态 |
| `POST` | `/auth/register` | `admin` | 创建用户 |
| `GET` | `/auth/user/:id` | 无 | 获取用户公开信息 |
| `GET` | `/auth/avatar/:id` | 无 | 获取用户头像 |
| `GET` | `/auth/userlist` | 登录 | 获取用户列表 |
| `DELETE` | `/auth/user/:id` | `admin` | 删除用户 |
| `POST` | `/auth/password` | `admin` 或本人 | 修改密码 |
| `POST` | `/auth/avatar` | `admin` 或本人 | 修改头像 |
| `PATCH` | `/auth/user` | `admin` | 修改用户权限与标签 |

### 文章

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `GET` | `/news/total/:target` | 无 | 获取分类文章数量 |
| `POST` | `/news/list` | 无 | 获取文章列表 |
| `GET` | `/news/detail/:id` | 无 | 获取文章详情 |
| `POST` | `/news/create` | `admin`/`news_admin` | 创建空文章并返回 ID |
| `PATCH` | `/news/:id` | `admin`/`news_admin` | 更新文章，可选择 WebSocket 推送 |
| `POST` | `/news/upload/:id` | `admin`/`news_admin` | 上传文章附件 |
| `DELETE` | `/news/upload/:id` | `admin`/`news_admin` | 删除文章附件 |
| `DELETE` | `/news/:id` | `admin`/`news_admin` | 删除文章 |

文章更新推送字段：

```json
{
  "doesNotify": true,
  "notifySessionIds": ["session-id-1", "session-id-2"]
}
```

如果 `doesNotify` 为 `false`，不会推送。  
如果 `doesNotify` 为 `true` 且 `notifySessionIds` 为空，则广播到全部在线 Bot。  
如果 `notifySessionIds` 非空，则只推送到指定 session。

### 服务器

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `GET` | `/server/` | 无 | 获取服务器列表 |
| `POST` | `/server/status` | 无 | 查询服务器实时状态 |
| `GET` | `/server/create` | `admin`/`server_admin` | 创建服务器条目 |
| `PATCH` | `/server/` | `admin`/`server_admin` | 更新服务器条目 |
| `DELETE` | `/server/:id` | `admin`/`server_admin` | 删除服务器条目 |

`/server/status` 请求：

```json
{
  "serverUrl": "example.com:25565"
}
```

响应示例：

```json
{
  "online": true,
  "icon": "data:image/png;base64,...",
  "playerCount": 12,
  "capacity": 100,
  "latency": 42,
  "version": "1.20.1",
  "players": [
    {
      "name": "Steve",
      "uuid": "8667ba71-b85a-4004-af54-457a9734eed7"
    }
  ]
}
```

注意：`players` 来自 Minecraft 状态协议 sample，不保证包含全部在线玩家。

### 文档

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `GET` | `/documents/layer/:parentId` | 无 | 获取公开子节点 |
| `GET` | `/documents/layer/private/:parentId` | 登录 | 获取包含私有节点的子节点 |
| `GET` | `/documents/:id` | 无 | 获取公开文档内容 |
| `GET` | `/documents/private/:id` | 登录 | 获取私有文档内容 |
| `POST` | `/documents/node` | 登录 | 创建节点 |
| `POST` | `/documents/node/:id` | 登录 | 更新节点父级 |
| `PUT` | `/documents/node/:id` | 登录 | 更新文档内容 |
| `PATCH` | `/documents/node/:id` | 登录 | 重命名/更新节点属性 |
| `DELETE` | `/documents/node/:id` | 登录 | 删除节点 |
| `POST` | `/documents/upload/:id` | 登录 | 上传文档附件 |
| `DELETE` | `/documents/upload/:id` | 登录 | 删除文档附件 |

### 静态资源

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/contents/*` | 访问上传后的附件 |

上传文件允许后缀：

```text
.png, .jpg, .jpeg, .webp, .pdf, .txt
```

文件会被重命名为 UUID 文件名。

## Bot WebSocket

### Token 管理

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `POST` | `/bots/token` | `admin`/`bot_admin` | 创建 Bot Token |
| `GET` | `/bots/token` | `admin`/`bot_admin` | 获取 Token 列表 |
| `GET` | `/bots/token/:id` | `admin`/`bot_admin` | 获取 Token 记录 |
| `DELETE` | `/bots/token/:id` | `admin`/`bot_admin` | 删除 Token |
| `GET` | `/bots/status` | `admin`/`bot_admin` | 获取在线连接与日志 |
| `DELETE` | `/bots/ws/kick/:session_id` | `admin`/`bot_admin` | 踢出指定连接 |

创建 Token 请求：

```json
{
  "name": "astrbot-main"
}
```

建议响应包含明文 Token，并且明文只展示一次：

```json
{
  "token": {
    "name": "astrbot-main"
  },
  "plain_token": "bot_xxx"
}
```

### WebSocket 连接

推荐路由：

```text
GET /necore/bots/ws/updates/:identifier
```

请求头：

```http
Authorization: Bearer <bot-token>
```

`identifier` 是机器人连接标识，例如：

```text
astrbot-test
```

后端会为每个连接生成 `session_id`，管理端可通过该 ID 定向推送或踢出连接。

### 心跳

Bot 客户端应定期发送：

```json
{
  "type": "heartbeat",
  "identifier": "astrbot-test",
  "time": 1710000000
}
```

后端收到 `type=heartbeat` 或 `event=heartbeat` 后刷新最近心跳时间。超过 `BOT_HEARTBEAT_TIMEOUT_SECONDS` 未收到心跳，后端会主动断开连接并记录日志。

### 推送事件

文章更新事件：

```json
{
  "event": "article_updated",
  "data": {
    "id": "article-id",
    "title": "文章标题",
    "brief": "文章简介",
    "category": "information"
  }
}
```

## 安全注意事项

- `SECRET` 必须保密，泄漏后应更换并让用户重新登录。
- 用户密码使用 bcrypt 保存，不应记录明文密码。
- Bot Token 应只保存 hash，创建时明文只返回一次。
- WebSocket 中的 `identifier`、`token_name` 等动态字段写入 HTML 日志前必须进行转义。
- 从 Fiber `Ctx` 取得并跨 WebSocket 生命周期使用的字符串应使用 `strings.Clone` 复制，避免请求上下文复用导致内容污染。
- 文件上传必须使用安全文件名与后缀白名单。
- 对自动重连造成的重复日志，应在 Hub 层进行去重。

## 部署建议

### 直接运行

```bash
go build -o necore .
./necore
```

### systemd 示例

```ini
[Unit]
Description=Necore Backend
After=network.target

[Service]
WorkingDirectory=/opt/necore
ExecStart=/opt/necore/necore
Restart=always
Environment=NECORE_CONFIG_FILE=/opt/necore/.env

[Install]
WantedBy=multi-user.target
```

### Nginx 反向代理

```nginx
location /necore/ {
    proxy_pass http://127.0.0.1:3000/necore/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}

location /necore/bots/ws/ {
    proxy_pass http://127.0.0.1:3000/necore/bots/ws/;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
}
```

## 常用命令

```bash
# 下载依赖
go mod download

# 开发运行
go run .

# 测试
go test ./...

# 构建
go build -o necore .
```

## 故障排查

### 登录后很快失效

检查 `.env` 中 `SECRET` 是否每次启动都变化。生产环境必须固定 `SECRET`。

### 上传文件无法访问

确认：

- `contents/` 目录存在且进程有读写权限；
- 前端访问路径是否补齐 `/necore` 前缀；
- 反向代理是否转发 `/necore/contents/`。

### Bot 一直重连

检查：

- WebSocket 路由是否为 `/necore/bots/ws/updates/:identifier`；
- 请求头是否包含 `Authorization: Bearer <bot-token>`；
- Token 是否被删除或重新生成；
- 反向代理是否正确配置 `Upgrade` 和 `Connection`；
- 心跳间隔是否小于 `BOT_HEARTBEAT_TIMEOUT_SECONDS`。

### Bot 断开日志中的 identifier 异常变化

确认 WebSocket 鉴权中间件中从 Fiber `Ctx` 取得的字符串已经使用 `strings.Clone` 复制，例如：

```go
identifier := strings.TrimSpace(strings.Clone(c.Params("identifier")))
c.Locals("identifier", strings.Clone(identifier))
```

### 服务器状态查询返回离线

确认：

- `serverUrl` 格式是否正确，例如 `example.com:25565`；
- 服务器是否允许 Minecraft status ping；
- 后端所在机器能否访问目标服务器；
- 查询并发过高时可能返回 `429 Service busy`。
