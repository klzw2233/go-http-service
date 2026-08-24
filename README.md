# Go HTTP Service

> Backend Engineer 学习路线的第一个实战项目：从零搭建一个 Go HTTP 服务。
> 当前已完成登录、JWT、refresh 轮转、按 IP 限流、CORS 与安全响应头，
> 公开注册已关闭（仅配置的 Author 可建账号），并已容器化（多阶段 distroless 镜像 + compose 编排），有 CI 与端到端测试保障。

---

## 项目目标

建立一个可用于后续项目演进的 Go 后端服务基础骨架：

- 使用 [Gin](https://github.com/gin-gonic/gin) Web 框架启动 HTTP 服务
- 按分层架构组织代码，依赖通过构造函数注入而非包级变量
- 对外契约稳定：统一的错误响应结构，绝不泄露内部实现细节
- 具备生产服务的基本要素：超时、优雅关闭、结构化日志、探针、限流、CORS 与安全响应头
- 公开注册已关闭；第一个 Author 由运维插入。登录后受保护接口走 JWT；refresh token 带轮转与重放检测
- 已容器化：多阶段 distroless 镜像 + compose 编排，CI 全流程保障

### 已完成的能力

| 能力 | 说明 |
|------|------|
| 依赖注入 | handler 是 `*API` 的方法，依赖由 `handler.New()` 注入 |
| 配置集中管理 | 全部环境变量在 `internal/config` 读取并校验，非法值拒绝启动 |
| 结构化日志 | `log/slog`，JSON / text 可切换，级别随状态码变化 |
| 请求关联 ID | `X-Request-Id`，注入 `context`，客户端传入值经过校验 |
| 两层超时 | `http.Server` 四项 + 单请求 `REQUEST_TIMEOUT` |
| 优雅关闭 | `SIGINT` / `SIGTERM` 排空在途请求后退出 |
| 探针分级 | `/api/health` 存活、`/api/ready` 就绪（含数据库检查） |
| 安全默认 | 不信任任何代理、请求体上限、panic 不泄露堆栈 |
| 安全响应头 | 每个响应都带 `nosniff`、`DENY`、`HSTS`、`Cache-Control: no-store` 等 |
| CORS | fail-closed：未配置时拒绝所有跨域，配置后按 origin 精确匹配 |
| 关闭公开注册 | 未认证 `POST /api/users` 返回 403；仅 `AUTHOR_USERNAME` 指名的 Author 可建账号 |
| Draft JSON | Author 通过 `/api/posts` 增/查/列/改 Draft；slug 创建时选定、不可改、含 Draft 全局唯一 |
| 登录与 JWT | `POST /api/auth/login`，HS256，失败响应完全一致 |
| Refresh 轮转 | 每次刷新作废旧 token；重放则撤销该用户全部会话 |
| 按 IP 限流 | 全局宽松 + 登录严格，探针豁免；内存实现，每副本独立 |
| 容器化 | 多阶段 distroless 镜像（非 root、无 shell、~10MB）+ compose 编排 app+db |
| 测试 | 单元 + 端到端（启动真实进程走真实 TCP），CI 跑竞态检测与 govulncheck |

---

## 项目结构

```
go-http-service/
├── cmd/
│   └── server/
│       ├── main.go              # 装配依赖、构造 logger、信号监听、优雅关闭
│       ├── main_test.go         # newLogger 的格式与级别
│       └── e2e_test.go          # 端到端：编译并启动真实进程，走真实 TCP
├── internal/
│   ├── config/
│   │   ├── config.go            # 所有环境变量的读取与校验、DSN 脱敏
│   │   └── config_test.go
│   ├── auth/
│   │   ├── token.go             # JWT 签发与解析（HS256）
│   │   └── refresh.go           # refresh token 生成与 SHA-256
│   ├── db/
│   │   ├── db.go                # PostgreSQL 连接池与就绪探针
│   │   ├── migrate.go           # 手写迁移器（embed + advisory lock）
│   │   ├── migrations/          # SQL 迁移文件，按文件名顺序执行
│   │   │   ├── 0001_create_users.sql
│   │   │   ├── 0002_create_refresh_tokens.sql
│   │   │   └── 0003_create_posts.sql
│   │   └── db_test.go
│   ├── repository/
│   │   ├── user_repository.go        # users 表读写、唯一键冲突翻译
│   │   ├── refresh_token_repository.go  # refresh 哈希存取、轮转、重放
│   │   └── post_repository.go        # posts 表读写、slug 唯一键翻译
│   ├── service/
│   │   ├── user_service.go      # 注册业务规则、bcrypt
│   │   ├── auth_service.go      # 登录、刷新、登出、计时拉平
│   │   └── post_service.go      # Post 增/查/列/改、slug/title/body 校验
│   ├── handler/
│   │   ├── api.go               # API 结构体：依赖注入的载体
│   │   ├── router.go            # 路由注册、中间件顺序、404/405/panic
│   │   ├── health.go            # /api/health   liveness
│   │   ├── ready.go             # /api/ready    readiness（可挂依赖检查）
│   │   ├── info.go              # /api/info
│   │   ├── echo.go              # /api/echo
│   │   ├── user.go              # /api/users    建账号（仅 Author）
│   │   ├── author.go            # 关闭匿名注册、Author 校验
│   │   ├── post.go              # /api/posts    Draft 增/查/列/改（仅 Author）
│   │   ├── auth.go              # 登录 / 刷新 / 登出 / me、认证中间件
│   │   ├── ratelimit.go         # 按 IP 令牌桶 + 空闲淘汰
│   │   ├── cors.go              # fail-closed CORS（按 origin 精确匹配）
│   │   ├── headers.go           # 安全响应头（每个响应都带）
│   │   ├── errors.go            # 绑定错误 -> 统一错误响应的翻译层
│   │   ├── requestid.go         # Request ID 生成、校验、context 传递
│   │   ├── logging.go           # slog 访问日志中间件
│   │   └── middleware.go        # 请求体上限、单请求超时
│   └── model/
│       ├── health.go            # HealthResponse
│       ├── ready.go             # ReadyResponse、CheckResult
│       ├── info.go              # InfoResponse、服务名与版本号
│       ├── echo.go              # EchoRequest, EchoResponse
│       ├── auth.go              # LoginRequest、TokenPair、RefreshRequest
│       ├── refresh.go           # 入库的 RefreshToken（无 JSON 标签）
│       ├── post.go             # Post、PostRequest/Response、长度与 slug 常量
│       ├── error.go             # ErrorResponse、错误码常量
│       └── model_test.go
├── notes/                       # 中文学习笔记
├── .github/workflows/ci.yml     # CI：换行符 / gofmt / vet / build / race / tidy / 镜像构建
├── .gitattributes               # 换行符规范（LF）
├── Dockerfile                   # 多阶段构建：golang:1.26 编译 → distroless static 运行
├── docker-compose.yml           # app + postgres 一键编排
├── .dockerignore                # 构建上下文裁剪
├── CLAUDE.md                    # 项目环境与开发约定
├── go.mod                       # Go 模块定义
├── go.sum                       # 依赖校验和
└── README.md                    # 项目说明
```

> **依赖注入**：handler 是 `*API` 的方法而非包级函数，`API` 持有配置、
> logger、时间源。接入 PostgreSQL 时，连接池作为 `API` 的又一个字段传入，
> 路由和其他 handler 一行都不用改。

> **为什么有 `e2e_test.go`**：单元测试在 `TestMain` 里设了 `gin.TestMode`，
> 观察不到真实进程用的运行模式。这个测试编译出真正的二进制、在真实端口上
> 启动、发真实 SIGTERM——它上线第一次运行就发现服务跑在 gin debug 模式、
> 把非 JSON 的警告混进了结构化日志流。

---

## 如何运行

### 1. 进入项目目录

```bash
cd ~/workspace/go-http-service
```

### 2. 运行服务

`DATABASE_URL`、`JWT_SECRET` 和 `AUTHOR_USERNAME` 都是必需项，未设置进程会拒绝启动：

```bash
DATABASE_URL="postgres://app:devsecret@127.0.0.1:5433/go_http_service?sslmode=disable" \
JWT_SECRET="$(openssl rand -base64 48)" \
AUTHOR_USERNAME=jimmy \
  go run cmd/server/main.go
```

默认监听 `8080`。需要换端口时通过 `PORT` 环境变量指定，无需重新编译：

```bash
PORT=9000 DATABASE_URL="..." JWT_SECRET="..." AUTHOR_USERNAME=jimmy go run cmd/server/main.go
```

服务收到 `Ctrl+C`（SIGINT）或 `SIGTERM` 时会优雅关闭：停止接收新连接，
等待在途请求处理完毕后再退出。等待上限默认 15 秒，由 `SHUTDOWN_TIMEOUT` 控制。
排空期间再按一次 `Ctrl+C` 会立即强制退出。

配置有误时进程**不会带着默认值启动**，而是立即失败并说明原因：

```bash
$ PORT=abc go run cmd/server/main.go
server stopped with an error  error="config: PORT must be a number, got \"abc\""
```

### 3. 测试接口

#### 存活探针

```bash
curl http://localhost:8080/api/health
```

预期返回：

```json
{"status":"ok","timestamp":"..."}
```

#### 就绪探针

```bash
curl -i http://localhost:8080/api/ready
```

预期返回 `200`，并报告数据库依赖：

```json
{"status":"ready","timestamp":"...","checks":[{"name":"database","status":"ok","duration_ms":3}]}
```

任一依赖检查失败时会返回 `503`，`status` 变为 `not_ready`。
两个探针的区别见下文「health 与 ready 的区别」。

#### 服务信息

```bash
curl http://localhost:8080/api/info
```

预期返回：

```json
{
  "name": "go-http-service",
  "version": "0.2.0",
  "go_version": "go1.26.6",
  "timestamp": "..."
}
```

#### Echo 回显

```bash
curl -X POST http://localhost:8080/api/echo \
  -H "Content-Type: application/json" \
  -d '{"message":"hello"}'
```

预期返回：

```json
{"message":"hello","echoed_at":"..."}
```

#### 第一个 Author 账号（运维插入）

公开注册已关闭。未认证的 `POST /api/users` 返回 `403` + `FORBIDDEN`，不是 `401`：这是权限拒绝，不是提示去登录。第一个 Author 由运维直接插入 `users` 表（bcrypt 哈希，不是明文）：

```bash
# 生成哈希后插入。cost 用默认 10 即可。
HASH="$(python -c 'import bcrypt; print(bcrypt.hashpw(b"correct-horse", bcrypt.gensalt()).decode())')"
psql "$DATABASE_URL" -c "INSERT INTO users (username, email, password_hash) VALUES ('jimmy', 'jimmy@example.com', '$HASH')"
```

`AUTHOR_USERNAME` 必须与这行的 `username` 大小写不敏感地一致（唯一索引建在 `lower(username)` 上）。插好之后用登录接口换 token。已登录的 Author 仍可 `POST /api/users` 再建账号（运维便利）；已登录但不是 Author 的用户同样是 `403`。

**响应里没有任何密码字段**，连 `password_hash` 也没有。见下文「密码处理」。

重复用户名（包括大小写不同的同名）返回 `409`：

```json
{"code":"CONFLICT","message":"that username is already taken"}
```

#### 登录

```bash
curl -s -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"jimmy","password":"correct-horse"}'
```

预期返回 `200`：

```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "…",
  "token_type": "Bearer",
  "expires_at": "2026-08-23T12:15:00Z"
}
```

响应里没有用户名、邮箱或任何密码材料。用户信息在受保护接口里取。

用户不存在和密码错误返回**完全相同**的 `401`：

```json
{"code":"UNAUTHORIZED","message":"authentication required"}
```

#### 受保护接口

```bash
curl -s -H "Authorization: Bearer $ACCESS_TOKEN" \
  http://localhost:8080/api/auth/me
```

缺 token、格式错、过期、签名无效，全部是上面那条 401，外加
`WWW-Authenticate: Bearer realm="api"`。

#### 刷新与登出

```bash
curl -s -X POST http://localhost:8080/api/auth/refresh \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\":\"$REFRESH_TOKEN\"}"

curl -i -X POST http://localhost:8080/api/auth/logout \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\":\"$REFRESH_TOKEN\"}"
```

刷新会作废旧的 refresh token 并返回新的一对。把已经用过的 refresh token
再提交一次，服务会撤销该用户的全部会话，对外仍是同一个 401。
登出成功返回 `204`，无响应体。

---

### 4. 构建二进制

```bash
go build -o server ./cmd/server
```

`/api/info` 返回的 `version` 字段默认是源码里的 `0.2.0`。发布构建时可以用
`-ldflags -X` 在**编译期**注入真实版本，无需改动源码：

```bash
go build -ldflags "-X go-http-service/internal/model.Version=$(git describe --tags --always)" \
  -o server ./cmd/server
```

### 5. Docker Compose 一键启动

不想装 Go、也不想手动起 PostgreSQL，直接用 Compose 把 app + 数据库一起拉起来。
`Dockerfile` 是多阶段构建：第一阶段用 `golang:1.26` 编译静态二进制
（`CGO_ENABLED=0`，注入 version），第二阶段用
`gcr.io/distroless/static-debian12:nonroot`，镜像 ~10MB、无 shell、非 root 运行。

```bash
# JWT_SECRET 与 AUTHOR_USERNAME 都是必需项，从宿主机环境变量读，不写进 compose 文件
export JWT_SECRET="$(openssl rand -base64 48)"
export AUTHOR_USERNAME=jimmy

# --wait 会等到 db 的 healthcheck 通过
docker compose up -d --wait

curl http://localhost:8080/api/health          # 存活
curl http://localhost:8080/api/ready            # 就绪，应报告 database=ok
curl http://localhost:8080/api/info            # version 字段是注入的 dev
```

几个要点：

- `DATABASE_URL` 里 host 写的是 compose 服务名 `db`，不是 `127.0.0.1`——
  容器之间走 compose 网络，`127.0.0.1` 会指 app 容器自己，连不到数据库。
- `JWT_SECRET` 用 `${JWT_SECRET:?...}` 从宿主机读，**不写进文件**（会进 git）。
  未设置时 `compose up` 直接报错，而不是静默用空值。`AUTHOR_USERNAME` 同理：它不是密钥，但仍是必需项，未设置进程拒绝启动。
- `depends_on: db: condition: service_healthy` 让 app 等 db 就绪后启动，
  避免 app 首次连接就失败的启动竞态。
- db 映射 `127.0.0.1:5433:5432`，和本机开发库约定一致，只绑回环不暴露公网。
- 手动构建镜像（不走 compose）：`docker build --build-arg VERSION=dev -t go-http-service:dev .`

停掉并清理（连数据卷一起删）：

```bash
docker compose down -v
```

只停服务、保留数据：`docker compose down`（去掉 `-v`）。

### 6. 运行测试

```bash
go test ./...
```

只运行某个测试：

```bash
go test -v ./internal/handler -run TestEchoEndpoint
```

竞态检测（测试里用了 `t.Parallel`，就绪检查还会并发扇出，**提交前必须跑**）：

```bash
go test -race ./...
```

查看覆盖率：

```bash
go test -cover ./...
```

当前覆盖率（2026-08-23 实测）：

| 包 | 覆盖率 |
|----|--------|
| `internal/model` | 100.0% |
| `internal/config` | 96.6% |
| `internal/handler` | 92.5% |
| `cmd/server` | 17.1%（主体是 `run()` 的启动流程，由端到端测试覆盖） |

#### 端到端测试

`cmd/server/e2e_test.go` 会**编译出真实二进制并作为进程启动**，在真实端口上
用真实 HTTP 客户端发请求，最后发真实 `SIGTERM`。它覆盖单元测试碰不到的部分：
启动日志格式、配置解析、信号处理、进程退出码。

```bash
go test ./cmd/server -run TestEndToEnd -v
```

它会编译，比单元测试慢几秒。急着跑单元测试时可以跳过：

```bash
go test -short ./...
```

> 注意：Go 会缓存测试结果。改了非测试代码后想看真实输出，加 `-count=1`
> 强制重跑，否则可能看到的是上一次的缓存结果。

#### 需要数据库的测试

连接池、就绪探针、关闭顺序这些测试需要一个真实的 PostgreSQL。
它们由 `TEST_DATABASE_URL` 控制：**未设置就自动跳过**，
所以在没装数据库的机器上 `go test ./...` 依然全绿。

```bash
export TEST_DATABASE_URL="postgres://app:devsecret@127.0.0.1:5433/go_http_service?sslmode=disable"
go test -race -count=1 ./...
```

CI 里配了 postgres service 容器并设置了这个变量，因此每次推送都会真正跑到这些路径。
CI 还会**再跑一遍不带数据库的测试**，确保「本地无库也能开发」这件事不会悄悄退化。

---

## 开发用数据库

用独立容器，不要用宿主机上可能已有的 PostgreSQL——学习阶段会频繁建表删表，
和别的数据共用一个实例迟早出事。

```bash
docker run -d --name go-http-service-db \
  -e POSTGRES_USER=app \
  -e POSTGRES_PASSWORD=devsecret \
  -e POSTGRES_DB=go_http_service \
  -p 127.0.0.1:5433:5432 \
  -v go-http-service-pgdata:/var/lib/postgresql/data \
  --restart unless-stopped \
  postgres:17
```

两个刻意的选择：

- **映射到 `5433`**，避开宿主机 PostgreSQL 默认占用的 `5432`
- **绑定 `127.0.0.1` 而非 `0.0.0.0`**，只有本机能连。开发库的口令通常很弱，
  没有理由让它暴露在局域网里

常用操作：

```bash
docker stop go-http-service-db      # 停（用来验证就绪探针会变 503）
docker start go-http-service-db     # 起
docker logs go-http-service-db      # 看日志
psql "postgres://app:devsecret@127.0.0.1:5433/go_http_service"   # 连进去
```

连接服务：

```bash
DATABASE_URL="postgres://app:devsecret@127.0.0.1:5433/go_http_service?sslmode=disable" \
JWT_SECRET="$(openssl rand -base64 48)" \
AUTHOR_USERNAME=jimmy \
  go run cmd/server/main.go
```

> 上面的口令仅供本机开发使用。它不会出现在日志里——启动日志中的 `database_url`
> 是脱敏后的（见「日志」一节）。

---

## 数据库迁移

迁移器是手写的（`internal/db/migrate.go`，约 80 行），不依赖 goose 或
golang-migrate。原因是那两个库的 API 都建立在 `database/sql` 上，
而本项目用 pgxpool 原生 API——引入它们就得额外开一条 `database/sql` 通道专供迁移。

迁移文件放在 `internal/db/migrations/`，用 `go:embed` 打进二进制：
**部署时不需要额外分发 SQL 文件**，也不可能出现「二进制更新了但迁移目录没更新」。

```
internal/db/migrations/
├── 0001_create_users.sql
└── 0002_create_refresh_tokens.sql
```

新增迁移就是加一个文件，前缀数字递增。**按文件名排序执行**，所以前缀要补零对齐。

### 三个设计要点

**每个迁移一个事务。** DDL 和 `schema_migrations` 的插入在同一事务里，
要么都成功要么都回滚。PostgreSQL 支持事务性 DDL（`CREATE TABLE` 可回滚），
这是 MySQL 做不到的，也是这个迁移器能写得这么简单的原因——
数据库永远不会处于「以为迁移过了其实没有」的状态。

**启动时自动执行，并用 advisory lock 串行化。** 多副本同时启动是编排系统下的常态，
没有这把锁两个 Pod 会同时建同一张表，其中一个报 `relation already exists` 而崩溃。

**没有 down 迁移。** 手写方案的取舍，也符合实际：生产环境里回滚一个已经
`DROP COLUMN` 的迁移根本救不回数据，正确做法是写一个新的前向迁移。

### users 表

```sql
CREATE UNIQUE INDEX users_username_key ON users (lower(username));
CREATE UNIQUE INDEX users_email_key    ON users (lower(email));
```

唯一约束建在 `lower(username)` 上而不是 `username`：**`Jimmy` 和 `jimmy` 是同一个人**，
但用户输入的大小写要保留下来用于显示。代价是查询必须写成
`WHERE lower(username) = lower($1)` 才能命中索引。

索引名会被 repository 层用来区分「用户名冲突」还是「邮箱冲突」，改名要同步改代码。

---

## 密码处理

### 存的是 bcrypt 哈希，不是明文

```bash
psql "$DATABASE_URL" -c 'select username, left(password_hash, 7) from users;'
#  username | left
# ----------+---------
#  jimmy    | $2a$10$
```

### 72 字节这个坑

bcrypt **只使用密码的前 72 字节**，超出部分静默丢弃。也就是说，
两个前 72 字节相同的密码会被判为同一个——用任意一个都能登录。

麻烦在于这条限制**无法用 binding tag 表达**：gin 的 validator 对字符串用
`utf8.RuneCountInString`，按**字符**计数。一个 72 个汉字的密码是 216 字节，
能通过 `binding:"max=72"`，却会被 bcrypt 砍掉 144 字节。

所以密码长度上限在 service 层按**字节**校验：

```go
if len(in.Password) > model.MaxPasswordBytes {   // len() 返回字节数
    return nil, ErrPasswordTooLong
}
```

`internal/service` 里有一个用 72 个汉字的测试专门钉住这个行为。

### 响应绝不含密码材料

两道防线：

1. `model.User.PasswordHash` 带 `json:"-"`（标签级，改结构体时容易漏）
2. handler 返回的是 `model.UserResponse`，**结构体里根本没有这个字段**（构造级）

只靠第 1 条不够——struct tag 在重构中很容易丢，而丢了的后果是密码哈希上了公网。
`internal/model` 和 `internal/handler` 各有一个测试断言响应里不出现哈希。

---

## 分层架构

```
HTTP Handler  →  Service  →  Repository  →  PostgreSQL
```

| 层 | 位置 | 职责 | 不该知道 |
|----|------|------|---------|
| Handler | `internal/handler` | 解析请求、映射错误到 HTTP | SQL |
| Service | `internal/service` | 业务规则（哈希、校验） | gin、SQL |
| Repository | `internal/repository` | Go 结构体 ↔ SQL | HTTP、业务规则 |

**依赖方向单向，错误也是。** Repository 定义 `ErrUsernameTaken`，
Service 把它翻译成自己的同名错误，Handler 只匹配 Service 的错误——
这样 handler 不需要 import repository，依赖方向连错误值都不破例。

### 唯一约束优先于查重

注册时**不做「先查有没有重名，再插入」**。那是 TOCTOU 竞态：
两个并发请求都能通过查重，然后都去插入，其中一个会拿到数据库的约束错误——
而代码声称已经处理过这种情况了。

正确做法是直接插入，捕获 PostgreSQL 的 `23505` 唯一键冲突并按
`ConstraintName` 翻译。数据库在写入时原子地判定约束，没有窗口期。

`internal/repository` 里有一个 8 并发注册同名的测试：**恰好 1 个成功，
7 个拿到 `ErrUsernameTaken`**——不是 500。

---

## API 列表

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/health` | 存活探针（liveness） |
| GET | `/api/ready` | 就绪探针（readiness） |
| GET | `/api/info` | 返回服务元信息 |
| POST | `/api/echo` | 接收 JSON 并回显 |
| POST | `/api/users` | 建账号（仅 Author；未认证 / 非 Author 返回 403） |
| POST | `/api/auth/login` | 登录，返回 access + refresh |
| POST | `/api/auth/refresh` | 用 refresh 换新的一对 |
| POST | `/api/auth/logout` | 撤销该 refresh token |
| GET | `/api/auth/me` | 当前用户（需 `Authorization: Bearer`） |
| POST | `/api/posts` | 创建 Draft（仅 Author）。请求体 `{title, slug, body}`，201 返回 Post |
| GET | `/api/posts` | 列出全部含 Draft（仅 Author） |
| GET | `/api/posts/:slug` | 取一个 Post（仅 Author；不存在 404） |
| PATCH | `/api/posts/:slug` | 改 title 和/或 body（仅 Author）；slug 不可改 |

### health 与 ready 的区别

这两个探针**不能混用**，用错会造成完全不同的故障。

| | `/api/health` | `/api/ready` |
|---|---|---|
| 回答的问题 | 进程还活着吗 | 现在能处理请求吗 |
| 是否检查依赖 | **否** | 是 |
| 探测失败的后果 | 编排系统**重启**容器 | 编排系统**摘掉流量**，不重启 |
| 对应 K8s 探针 | `livenessProbe` | `readinessProbe` |

`/api/health` 刻意不检查数据库。如果它查了，数据库抖动一下就会让编排系统
把一个本来正常的进程重启掉——把依赖的短暂故障放大成自身的故障。

`/api/ready` 会并发执行所有已注册的依赖检查：

```json
{
  "status": "ready",
  "timestamp": "2026-08-23T12:00:00Z",
  "checks": [
    { "name": "database", "status": "ok", "duration_ms": 3 }
  ]
}
```

任一检查失败时返回 **503**，`status` 变为 `not_ready`，对应检查的
`status` 为 `failed`。`DATABASE_URL` 是必需项，就绪探针始终包含
`database` 这一项。

可以实地验证这套分级——停掉数据库，两个探针的反应完全不同：

```bash
docker stop go-http-service-db
curl -o /dev/null -w '%{http_code}\n' localhost:8080/api/ready    # 503：摘流量
curl -o /dev/null -w '%{http_code}\n' localhost:8080/api/health   # 200：不重启
docker start go-http-service-db
curl -o /dev/null -w '%{http_code}\n' localhost:8080/api/ready    # 恢复 200
```

> 检查失败时**不返回原始错误**，只返回 `check failed` / `check timed out` /
> `check panicked` 三种通用原因。数据库驱动的错误里可能含有主机名、用户名甚至
> 密码。完整错误只写日志；`name` 字段已经指明是哪个依赖出问题，那才是可操作的信息。

---

## 错误响应契约

所有接口的错误响应共用同一种结构，客户端只需实现一套解析逻辑：

```json
{
  "code": "VALIDATION_FAILED",
  "message": "one or more fields failed validation",
  "fields": [
    { "field": "message", "reason": "is required" }
  ]
}
```

- `code`：**稳定**的机器可读标识，客户端应基于它做分支判断
- `message`：给人看的说明，措辞可能变化，不要用于逻辑判断
- `fields`：仅在字段级校验失败时出现，`field` 是 JSON 字段名

| `code` | HTTP 状态码 | 触发条件 |
|--------|------------|---------|
| `INVALID_JSON` | 400 | 请求体不是合法 JSON（语法错误、被截断） |
| `VALIDATION_FAILED` | 400 | JSON 合法，但字段缺失、类型错误或超出约束 |
| `NOT_FOUND` | 404 | 路径不存在 |
| `METHOD_NOT_ALLOWED` | 405 | 路径存在，但不支持该 HTTP 方法（响应带 `Allow` 头） |
| `UNAUTHORIZED` | 401 | 缺少或无法使用的凭据。缺 token、格式错、过期、签名无效、用户不存在、密码错误、refresh 重放，全部是这一条 |
| `FORBIDDEN` | 403 | 已认证但无权执行该操作（例如非 Author 写接口）；未认证的 `POST /api/users` 也是这一条，不是 401 |
| `CONFLICT` | 409 | 与已有数据冲突，如注册了别人已占用的用户名 |
| `PAYLOAD_TOO_LARGE` | 413 | 请求体超过 `MAX_BODY_BYTES` 上限 |
| `RATE_LIMITED` | 429 | 超过该地址的请求预算，响应带 `Retry-After` |
| `INTERNAL_ERROR` | 500 | 服务端异常，详情只写日志 |

> 底层的原始错误（validator、`encoding/json` 的报错）只写入服务端日志，
> **不会返回给客户端**。这些报错里含有 Go 内部结构体名（如 `EchoRequest`），
> 且措辞会随依赖库版本变化，不适合作为对外契约。

**唯一的例外是 `/api/ready`**：它在 200 和 503 两种情况下都返回 `ReadyResponse`
而非 `ErrorResponse`。就绪探针由编排系统消费而不是 API 客户端，
保留「哪个依赖挂了」的明细比套用统一错误结构更有价值。

### 请求体限制

| 限制 | 默认值 | 说明 |
|------|--------|------|
| 请求体总大小 | 1 MiB | 由 `limitBodySize` 中间件对全局路由生效，可用 `MAX_BODY_BYTES` 调整 |
| `/api/echo` 的 `message` 长度 | 4096 字符 | 由 binding tag `max=4096` 约束，按**字符**而非字节计 |

---

## 端口说明

- 默认监听端口：**8080**（可用 `PORT` 环境变量覆盖）
- 基础路径：**/api**

## 环境变量

所有配置集中在 `internal/config`，启动时**一次性读取并校验**。
任何非法值都会让进程立即退出并说明原因，而不是静默回退到默认值：

```bash
$ PORT=abc go run cmd/server/main.go
server stopped with an error  error="config: PORT must be a number, got \"abc\""
```

多个配置同时出错时会**一次全部报出**，不用一个个试。

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PORT` | `8080` | 监听端口，1~65535 |
| `TRUSTED_PROXIES` | 空（谁都不信任） | 逗号分隔的可信代理 IP / CIDR |
| `CORS_ALLOWED_ORIGINS` | 空（禁止所有跨域） | 逗号分隔的允许跨域来源；`*` 表示允许全部（见下） |
| `READ_HEADER_TIMEOUT` | `5s` | 读取请求头的时长上限 |
| `READ_TIMEOUT` | `10s` | 读取完整请求的时长上限 |
| `WRITE_TIMEOUT` | `10s` | 写响应的时长上限 |
| `IDLE_TIMEOUT` | `60s` | keep-alive 空闲连接存活上限 |
| `SHUTDOWN_TIMEOUT` | `15s` | 优雅关闭时等待在途请求的上限 |
| `REQUEST_TIMEOUT` | `8s` | **单个请求内 handler 的处理时长上限** |
| `MAX_BODY_BYTES` | `1048576` | 请求体大小上限（1 MiB） |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `LOG_FORMAT` | `json` | `json` 或 `text` |
| `DATABASE_URL` | **必需** | PostgreSQL 连接串 |
| `DB_MAX_CONNS` | `10` | 连接池上限 |
| `DB_CONNECT_TIMEOUT` | `5s` | 建连超时，同时用作就绪探测的超时 |
| `JWT_SECRET` | **必需**，至少 32 字节 | HS256 签名密钥，启动日志脱敏为 `(set)` |
| `AUTHOR_USERNAME` | **必需**，3–32 位字母数字 | 可写 Posts 的 User 名，大小写不敏感；启动日志原样写出（不是密钥） |
| `ACCESS_TOKEN_TTL` | `15m` | access token 有效期 |
| `REFRESH_TOKEN_TTL` | `720h` | refresh token 有效期（30 天） |
| `RATE_LIMIT_RPS` | `20` | 全局限流（探针除外），每地址每秒 |
| `RATE_LIMIT_BURST` | `40` | 全局限流突发 |
| `LOGIN_RATE_LIMIT_RPM` | `5` | 登录 / 刷新 / 登出，每地址每分钟 |
| `LOGIN_RATE_LIMIT_BURST` | `5` | 登录限流突发 |
| `GIN_MODE` | 未设置时按 `release` 运行 | 由 gin 自身读取，见下 |

关于 `DATABASE_URL`：**现在是必需项**，未设置时进程拒绝启动。
在只有 health / info / echo 三个接口的阶段它是可选的——那时没有任何接口需要持久化，
设成必需等于强制一个用不到的依赖。加了 `POST /api/users` 之后就说不通了。

关于 `JWT_SECRET`：同样是必需项，长度至少 32 字节。HS256 的安全性直接取决于
密钥熵，短密钥可以离线爆破，之后攻击者能给任意用户签发 token。
生成一个：`openssl rand -base64 48`。启动日志里这个字段是 `(set)`，
连长度都不保留——长度会缩小爆破搜索空间。

关于 `AUTHOR_USERNAME`：必需，3–32 位 ASCII 字母或数字，与 `users.username` 大小写不敏感地比较。
它是名字不是密钥，启动日志原样写出。第一个匹配的 User 由运维 SQL 插入，本服务不提供 seed / 管理后台。

`DB_MAX_CONNS` 显式固定连接池上限，而不是用 pgx 的默认值（`max(4, CPU 核数)`）——
后者会让同一个服务换台机器部署就对数据库产生不同的压力。

关于 `GIN_MODE`：gin 的 debug 模式会把路由表和警告**直接打到 stdout**，
那些行不是 JSON，会混进结构化日志流让按行解析的采集器失败。因此本服务在
`GIN_MODE` 未设置时主动切到 `release`。想看 gin 的调试输出就显式设置
`GIN_MODE=debug`，它优先于默认行为。

> 这个问题是端到端测试上线第一次运行时发现的。单元测试发现不了——
> `TestMain` 里设了 `gin.TestMode`，观察不到真实进程用的模式。

关于 `TRUSTED_PROXIES`：gin 默认信任**所有**代理，这会让 `c.ClientIP()`
无条件采信客户端自己发来的 `X-Forwarded-For` 头，即客户端可以随意伪造自身 IP。
本服务默认不信任任何代理，`ClientIP()` 取 TCP 连接的真实对端地址。

只有当服务确实部署在反向代理（Nginx、负载均衡器）之后时，才需要设置：

```bash
TRUSTED_PROXIES="10.0.0.0/8,172.16.0.0/12" go run cmd/server/main.go
```

关于 `CORS_ALLOWED_ORIGINS`：**默认为空（fail-closed）**，任何跨域请求都会被
浏览器拒绝——这跟 `TRUSTED_PROXIES` 一样是有意的安全默认。CORS 只影响浏览器，
同源调用和非浏览器客户端（curl、服务端到服务端）不受影响，所以这不会挡住 API
的正常调用方。需要让某个前端站点跨域访问时才显式列出：

```bash
CORS_ALLOWED_ORIGINS="https://app.example.com,https://admin.example.com" \
  go run cmd/server/main.go
```

匹配是**精确字符串相等**，不是子串匹配，所以 `https://app.example.com` 不会
授权 `https://evilapp.example.com`。被命中的来源会原样回显为
`Access-Control-Allow-Origin`（而不是通配符 `*`），这样才能配合凭据（Cookie、
`Authorization`）使用——浏览器禁止通配符与凭据同时出现。`*` 仍可作为显式
「全部放开」选项，但它和「未配置」是两回事：未配置是默认拒绝，`*` 是主动放开。
来源必须是完整的 `http(s)://host` URL（`https://app.example.com`），裸主机名或
缺少协议会被启动校验拒绝——因为它们永远匹配不上浏览器实际发送的 `Origin`，
而一个静默不匹配的 CORS 配置是最难排查的。

## 安全响应头与 CORS 中间件

`internal/handler/headers.go` 的 `SecurityHeaders` 给**每一个响应**（包括 404、
500、panic 后的 500）都附加一组浏览器安全头：

| 头 | 值 | 作用 |
|----|----|------|
| `X-Content-Type-Options` | `nosniff` | 禁止 MIME 嗅探，防上传文件被当脚本执行 |
| `X-Frame-Options` | `DENY` | 禁止被任何页面嵌入，防点击劫持 |
| `X-XSS-Protection` | `0` | **关闭**旧版 IE/Edge 的 XSS 审计器（现代浏览器已移除，旧版反而是攻击面） |
| `Referrer-Policy` | `strict-origin-when-cross-origin` | 跨域时只发送 origin、剥掉路径与查询串 |
| `Strict-Transport-Security` | `max-age=2592000; includeSubDomains` | 30 天内强制 HTTPS |
| `Permissions-Policy` | `camera=(), microphone=(), geolocation=(), payment=()` | 禁用本 API 用不到的强权能 API |
| `Cache-Control` | `no-store` | API 响应常带凭据，绝不进缓存 |

这些头在 `c.Next()` 之前写入，所以即使 handler 提前 `Abort` 或超时，头也都在。
`X-XSS-Protection` 设为 `0` 而非 `1; mode=block` 是有意的：旧审计器本身有绕过
路径，现代浏览器已经移除它，开启它既无收益又有风险。

`internal/handler/cors.go` 的 `CORS(allowed []string)` 见上一节的配置说明，
同样在 `c.Next()` 之前完成 preflight 短路（OPTIONS 直接返回 204）。请求头白名单
固定为 `Origin, Content-Type, Authorization, X-Request-Id`，**不回显**
`Access-Control-Request-Headers`——回显等于「客户端要什么就批什么」，白名单形同虚设。

## 超时设置

分两层，管的事情不一样。

### 第一层：`http.Server` 的四项超时

服务不使用 `gin.Engine.Run()`，而是显式构造 `http.Server`。
`Run()` 内部创建的 server 四项超时全为零值（即永不超时），会留下 Slowloris 攻击面。

| 参数 | 默认值 | 作用 |
|------|--------|------|
| `ReadHeaderTimeout` | 5s | 限制客户端发送请求头的时长，防 Slowloris |
| `ReadTimeout` | 10s | 限制读取完整请求（含 body）的时长 |
| `WriteTimeout` | 10s | 限制写响应的时长 |
| `IdleTimeout` | 60s | 限制 keep-alive 连接的空闲存活时长 |

### 第二层：`REQUEST_TIMEOUT`（单请求处理超时）

**上面四项管不到 handler 内部。** 它们约束的是在 socket 上搬字节，
一个卡在慢查询上的 handler 不会被它们打断。

`REQUEST_TIMEOUT` 给 `c.Request.Context()` 加上 deadline，
handler 把这个 context 传给数据库查询，超时由查询自己返回错误。

> 中间件**只传播 deadline，不代替 handler 写响应**。
> 抢先写 504 会和 handler 争抢 `ResponseWriter`，那是数据竞争。
> 因此约定是：**任何会阻塞的调用都必须接收 `c.Request.Context()`**，
> 否则这个超时对它无效。

约束：`REQUEST_TIMEOUT` 必须**小于** `WRITE_TIMEOUT`，否则写超时总是先触发，
handler 的 deadline 永远没机会生效。配置校验会拒绝违反此约束的组合。

## 日志

使用标准库 `log/slog`，默认输出 JSON。

启动时打印一条包含全部生效配置的记录，用来确认部署真的读到了预期的值：

```json
{"time":"2026-08-23T00:20:26+08:00","level":"INFO","msg":"server listening",
 "addr":":8080","config":{"port":"8080","trusted_proxies":null,
 "read_header_timeout":"5s","read_timeout":"10s","write_timeout":"10s",
 "idle_timeout":"1m0s","shutdown_timeout":"15s","request_timeout":"8s",
 "max_body_bytes":1048576,"log_level":"INFO","log_format":"json"}}
```

> 时长渲染成 `"5s"` 而不是 `slog.Duration` 默认的纳秒整数 `5000000000`。
> 这条记录是给人看的，十位数的整数没法一眼看出配置对不对。
> 非时长字段保持原本类型，`max_body_bytes` 仍是数字，还能做数值比较。

> **`database_url` 和 `jwt_secret` 都是脱敏后的**。这条记录每次启动都会写一遍
> 并被日志系统长期留存，原样打印等于把凭据永久落盘。DSN 用标准库
> `url.Redacted()`，把口令替换成 `xxxxx` 但保留主机、端口和库名——那才是
> 这条日志的用处所在（确认部署真的连到了预期的库）。签名密钥没有任何
> 非敏感部分值得保留，所以整个值替换成 `(set)`，连长度都不给。
>
> 无法按 URL 解析的连接串（pgx 也接受 `host=... password=...` 这种形式）
> 一律输出固定的 `(set)`，**绝不回退到打印原文**——解析失败恰恰是最容易漏出口令的情形。

每个请求一条访问日志：

```json
{"time":"2026-08-23T00:20:27+08:00","level":"INFO","msg":"request","method":"GET",
 "path":"/api/health","status":200,"duration_ms":0,"client_ip":"127.0.0.1",
 "bytes":58,"request_id":"3f2a...c81"}
```

日志级别随状态码变化，便于只筛异常：2xx/3xx 记 `INFO`，4xx 记 `WARN`，5xx 记 `ERROR`。

**查询串不写入日志**——它经常携带 token 和 API key。

### Request ID

每个请求都会分配一个关联 ID，写入响应头 `X-Request-Id`，并注入
`c.Request.Context()`，后续的数据库查询可以直接携带它。

客户端可以自带 `X-Request-Id` 以支持跨服务链路追踪，但**会先做校验**：
长度须 ≤ 64 且仅含 `[A-Za-z0-9_-]`。不合规就丢弃并重新生成。

原因是这个值会被原样写进该请求的每一条日志。若不校验，客户端可以发送

```
X-Request-Id: x","level":"ERROR","msg":"payment approved
```

在你的日志里伪造出一条不存在的记录。

> 换行注入不在此列：HTTP 协议本身就用换行分隔头部，所以换行到不了字段值里
> （Go 的客户端更是直接拒绝发送这种头）。真正能穿过 HTTP 层的是引号、
> 花括号和制表符这类在头部值里合法的字符，那才是这条校验要挡的东西。
> 端到端测试逐个验证了这些载荷。

---

## 认证

登录成功返回一对 token：短寿命的 access JWT，长寿命的 refresh token。

**Access token** 是 HS256 JWT，payload 只有标准声明（`sub` 是用户 id、`exp`、`iat`）。
不放用户名或邮箱——JWT 的 payload 只是 base64，任何拿到它的人都能读。
它一旦签发就无法撤销，所以默认只有 15 分钟；连续性靠 refresh。

**Refresh token** 是 32 字节随机值，base64url 交给客户端，**SHA-256 十六进制入库**。
不能用 bcrypt：refresh token 是高熵随机数，不存在「猜出原文」的问题，
而每次刷新都跑一次慢哈希会把这个接口变成 CPU 瓶颈。

每次刷新都作废旧 token、签发新的一对。如果一个已经被作废的 refresh token
再次出现，服务假定它被窃取了，**撤销该用户的全部 refresh token**，强制重新登录。
对外仍然是同一个 401，好让攻击者分不清「打错了」和「已经被发现了」。

对已经撤销的 token 再 logout **不会**触发全家撤销——否则双击登出就会把
其他设备全部踢下线。

登录还有两个安全点：

1. 用户不存在和密码错误返回完全相同的响应，否则接口就是用户名枚举器
2. 两条路径的耗时也要接近。用户不存在时也跑一次假的 bcrypt 比对，
   否则大约 60ms 的差距足以被计时区分出来

`JWT_SECRET` 至少 32 字节。短密钥可以离线爆破，之后攻击者能给任意用户签发 token。

---

## 限流

按客户端 IP 分桶，用 `golang.org/x/time/rate` 的令牌桶。空闲桶会被机会式淘汰，
否则攻击者轮换源 IP 就能把这个 map 撑爆——限流中间件自己成了 DoS 入口。

| 作用域 | 默认 | 说明 |
|--------|------|------|
| `/api/health`、`/api/ready` | **不限流** | kubelet 从同一地址高频探测，限流会把健康容器重启掉 |
| 登录 / 刷新 / 登出 | 5 次/分钟，突发 5 | 攻击者从暴力猜密码上获益最大的入口 |
| 其余接口 | 20 次/秒，突发 40 | 普通使用感觉不到 |

超限返回 **429** + `RATE_LIMITED`，带 `Retry-After`。

IP 取自 `c.ClientIP()`，正确性依赖 `TRUSTED_PROXIES`。默认不信任任何代理，
客户端无法通过 `X-Forwarded-For` 伪造自身 IP 来绕过限流。

> **局限**：内存限流是**每副本独立**的。N 个副本时实际限额是 N 倍。
> 真要跨副本精确限流需要 Redis，属于后续工作。

---

## 下一步计划

接数据库这件事拆成了三步，**步骤 A、B、C 都已完成**，之后的中间件补充与容器化也已完成。

| 步骤 | 内容 | 状态 |
|------|------|------|
| A | 连接池、就绪探针接上真实数据库、验证关闭顺序 | **已完成** |
| B | 迁移机制 + `users` 表 + 注册接口 | **已完成** |
| C | 登录 + JWT + refresh 轮转 + 限流 | **已完成** |
| D | CORS（fail-closed）+ 安全响应头中间件 | **已完成** |
| E | Docker 容器化（多阶段 distroless 镜像 + compose 编排） | **已完成** |

### 之后

1. 尝试 Kubernetes 部署（`/api/health` 与 `/api/ready` 已可直接对接探针；
   `SIGTERM` 优雅关闭已就绪，K8s 滚动更新可直接用）

---

## 相关笔记

| 笔记 | 内容 |
|------|------|
| `notes/使用 Docker 容器化部署.md` | 本轮：多阶段 distroless 镜像、compose 编排、ldflags 注入 version 与坑 |
| `notes/接入安全中间件-CORS与响应头.md` | 本轮：CORS fail-closed、安全响应头、中间件顺序与坑 |
| `notes/接入 PostgreSQL 步骤C-登录JWT与限流.md` | 本轮：登录防枚举、JWT、refresh 哈希与轮转、限流 |
| `notes/接入 PostgreSQL 步骤B-注册接口与分层落地.md` | 手写迁移器、bcrypt 陷阱、TOCTOU、分层落地 |
| `notes/接入 PostgreSQL 步骤A-连接池与就绪探针.md` | 连接池、探针实证、两处凭据泄露的堵法 |
| `notes/接入数据库前的地基.md` | 依赖注入、配置层、日志、探针等六项改造 |
| `notes/代码审查问题清单与改进计划.md` | 代码审查的 15 个问题与修复记录 |
| `notes/分层架构.md` | 四层架构与依赖方向 |
| `notes/实现用户注册登录接口.md` | 用户系统的整体设计与安全要点 |
| `notes/连接 PostgreSQL 数据库.md` | 驱动选择、连接池、迁移工具 |
| `notes/添加中间件.md` | 中间件原理与注册顺序 |
