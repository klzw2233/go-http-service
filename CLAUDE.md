# 项目环境说明

> 文件位置：`go-http-service/CLAUDE.md`
> 用途：告知 Claude Code 本项目的运行环境、约定与注意事项
> 最后更新：2026-08-25（本地 `go run` 示例带 DEV_AUTHOR_PASSWORD）

---

## 一、运行环境

| 项目 | 值 |
|------|-----|
| 操作系统 | Ubuntu 虚拟机（Linux 6.8.0） |
| Shell | bash |
| 项目根目录 | `~/workspace/go-http-service` |
| Go 版本 | go1.26.6 |
| 编辑方式 | 常从 Win10 通过 VS Code Remote SSH 连入 |

这是标准 Linux 环境，**直接使用 Linux 命令即可**，不需要考虑 Windows 路径或
PowerShell 兼容性。旧版本本文件描述的 `E:\Program Files\...` 路径已经作废。

---

## 二、常用命令

```bash
# 运行服务（DATABASE_URL、JWT_SECRET、AUTHOR_USERNAME 都是必需项）
# DEV_AUTHOR_PASSWORD 可选：空库时插入 Author，用来打开 /author/login
DATABASE_URL="postgres://app:devsecret@127.0.0.1:5433/go_http_service?sslmode=disable" \
JWT_SECRET="$(openssl rand -base64 48)" \
AUTHOR_USERNAME=jimmy \
DEV_AUTHOR_PASSWORD=correct-horse \
  go run cmd/server/main.go

# 换端口
PORT=9000 DATABASE_URL="..." JWT_SECRET="..." AUTHOR_USERNAME=jimmy \
DEV_AUTHOR_PASSWORD=correct-horse go run cmd/server/main.go

# 构建
go build -o server ./cmd/server

# Docker Compose 一键起 app + 数据库（镜像多阶段构建，distroless 终态）
cp .env.example .env                 # 填 JWT_SECRET；DEV_AUTHOR_PASSWORD 可自动建 Author
docker compose up -d --wait          # 起服务并等 db healthcheck 过
docker compose down -v               # 停并删数据卷（去掉 -v 保留数据）
# 单独构建镜像不走 compose：
docker build --build-arg VERSION=dev -t go-http-service:dev .

# 提交前的完整检查（与 CI 的检查项一致）
gofmt -l cmd internal      # 应无输出
go vet ./...
go test -race -cover ./...
```

测试接口：

```bash
curl http://localhost:8080/api/health   # 存活探针
curl http://localhost:8080/api/ready    # 就绪探针（含依赖检查）
curl http://localhost:8080/api/info
curl -X POST http://localhost:8080/api/echo \
  -H "Content-Type: application/json" -d '{"message":"hello"}'
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"jimmy","password":"correct-horse"}'
```

开发用数据库（独立容器，映射到 5433 避开宿主机的 PostgreSQL）：

```bash
# 首次创建
docker run -d --name go-http-service-db \
  -e POSTGRES_USER=app -e POSTGRES_PASSWORD=devsecret \
  -e POSTGRES_DB=go_http_service \
  -p 127.0.0.1:5433:5432 \
  -v go-http-service-pgdata:/var/lib/postgresql/data \
  --restart unless-stopped postgres:17

docker start go-http-service-db     # 之后只需启停
docker stop  go-http-service-db

# 带数据库运行（要登录 Author 区时加上 DEV_AUTHOR_PASSWORD）
DATABASE_URL="postgres://app:devsecret@127.0.0.1:5433/go_http_service?sslmode=disable" \
JWT_SECRET="$(openssl rand -base64 48)" \
AUTHOR_USERNAME=jimmy \
DEV_AUTHOR_PASSWORD=correct-horse \
  go run cmd/server/main.go

# 跑需要数据库的测试（不设置这个变量则相关测试自动跳过）
export TEST_DATABASE_URL="postgres://app:devsecret@127.0.0.1:5433/go_http_service?sslmode=disable"
go test -race -count=1 ./...
```

---

## 三、GOCACHE 权限问题（已于 2026-08-22 修复）

保留此节是为了记录成因，避免再次踩坑。**当前无需任何特殊处理**，直接用默认配置即可。

### 曾经的现象

不设置 `GOCACHE` 时，`go build` / `go vet` / `go test` 报：

```
open /home/klzw2233/.cache/go-build/xx/xxxx-d: permission denied
```

### 根因

`~/.cache/go-build` 的属主变成了 **root:root**（创建于 2026-07-23），
说明当时用 `sudo go ...` 执行过命令，缓存目录被 root 占据，普通用户随后写不进去。

### 修复方式

```bash
sudo chown -R "$USER:$USER" ~/.cache/go-build
```

已执行并验证：属主恢复为 `klzw2233`，目录内无 root 残留，
默认 `GOCACHE`（`~/.cache/go-build`）下 `go build`、`go vet`、
`go test -race -cover ./...` 全部通过，`go clean -cache` 也能正常执行。

### 预防

> **不要用 `sudo` 运行任何 `go` 命令。** 这是问题的唯一成因。
> Go 的构建、测试、模块下载都不需要 root 权限。

`.gitignore` 里的 `.go-cache/` 规则保留作为保险，但项目内缓存目录已不再需要，
也不必再手动 `export GOCACHE`。

---

## 四、代码与提交约定

### 4.1 换行符必须是 LF

项目在 Win10 与 Ubuntu 之间同步，`.gitattributes` 已强制所有文本文件使用 LF。

原因：CRLF 会让 `gofmt` 把**每一个** `.go` 文件都标记为未格式化（曾经 7/7 全中），
同时在 `git diff` 里产生大量纯空白的伪改动。

不要修改 `.gitattributes` 里的 `eol=lf` 规则。若发现 `git status` 显示大量文件
被修改但 `git diff --ignore-all-space` 是空的，就是换行符问题复发了。

### 4.2 提交前必须通过的检查

CI（`.github/workflows/ci.yml`）会跑这些，本地先跑一遍可以避免来回：

```bash
gofmt -l cmd internal && go vet ./... && go test -race ./...
```

`-race` 不是可选项：测试里用了 `t.Parallel` 和共享的 `testRouter`。

### 4.3 代码注释用英文，笔记与文档用中文

现有代码的注释全部是英文（`// SetupRouter configures and returns...`），
新代码保持一致。`notes/` 下的学习笔记和 `README.md` 用中文。

### 4.4 错误响应必须走统一契约

所有对外的错误响应使用 `model.ErrorResponse`，不要临时用 `gin.H{"error": ...}`。
详见 `README.md` 的「错误响应契约」一节。

**绝不要把原始 error 直接返回给客户端**——validator 和 `encoding/json` 的报错
里含有 Go 内部结构体名（如 `EchoRequest`），且措辞随依赖版本变化。
原始错误只写日志，对外返回稳定的错误码。`internal/handler/errors.go` 是翻译层。

同样的规则适用于依赖检查：`/api/ready` 的检查失败时只返回通用原因，
数据库驱动的错误里可能带主机名、用户名甚至密码。

### 4.5 handler 是 `*API` 的方法，不是包级函数

新增 handler 一律写成 `func (a *API) Xxx(c *gin.Context)`，放在
`internal/handler/` 下与路由同名的文件里，并在 `router.go` 注册。

依赖通过 `API` 结构体注入，不要新增包级变量。需要新依赖时加 `API` 字段，
在 `handler.New()` 里接收，`cmd/server/main.go` 装配。

时间一律用 `a.now()`，不要直接调 `time.Now()`——否则时间戳无法在测试里断言。

### 4.6 会阻塞的调用必须接收 `c.Request.Context()`

`REQUEST_TIMEOUT` 中间件把 deadline 放在请求 context 上。它**只传播 deadline，
不代替 handler 写响应**（抢先写会和 handler 争 `ResponseWriter`，是数据竞争）。

因此：**数据库查询、HTTP 调用、任何可能阻塞的操作，都必须把
`c.Request.Context()` 传下去**，否则这个超时对它完全无效，一条慢查询
会一直占着 goroutine。

```go
// 对
rows, err := pool.Query(c.Request.Context(), sql, args...)

// 错——超时管不到它
rows, err := pool.Query(context.Background(), sql, args...)
```

### 4.7 日志用 `slog`，不要用 `log` 或 `fmt.Println`

handler 内部用 `a.logFor(c)`，它返回带 `request_id` 的 logger，
这样一条请求的所有日志能串起来。其他地方用 `a.log`。

不要把查询串、请求体、凭据写进日志。

### 4.8 分层：Handler → Service → Repository，错误也不例外

依赖方向**严格单向**，不得反向，也不得跨层（Handler 不能直接调 Repository）。

| 层 | 位置 | 不该知道 |
|----|------|---------|
| Handler | `internal/handler` | SQL |
| Service | `internal/service` | gin、SQL |
| Repository | `internal/repository` | HTTP、业务规则 |

**错误值也遵守这个方向。** Repository 定义 `ErrUsernameTaken`，
Service 定义自己的同名错误并做翻译，Handler 只匹配 Service 的——
这样 handler 不需要 `import repository`。看着像重复，但那正是分层的代价与意义。

Service 依赖的是接口而非具体类型（如 `userStore`），
所以 service 的测试不需要数据库。

### 4.9 唯一性靠数据库约束，不要先查后插

```go
// 错：TOCTOU 竞态，两个并发请求都能通过检查
if exists, _ := repo.ExistsByUsername(name); exists { ... }
repo.Create(user)

// 对：直接插入，翻译 23505 唯一键冲突
```

`internal/repository/user_repository.go` 的 `translateCreateError` 按
`pgErr.ConstraintName` 区分是用户名还是邮箱冲突。索引名定义在迁移文件里，
**改索引名必须同步改这里**，否则精确的 409 会退化成 500。

### 4.10 密码相关

- bcrypt **只用前 72 字节**，超出静默截断。上限必须在 service 层按
  **字节**（`len()`）校验——binding tag 按字符计，挡不住中文密码
- 对外响应用 `model.UserResponse`，不要直接返回 `model.User`
- 新增用户相关字段前先想清楚它能不能公开

### 4.11 refresh token：哈希用 SHA-256，不要用 bcrypt

refresh token 和密码都是凭据，都不能明文入库。但 refresh token 是 32 字节
随机值，不存在离线爆破问题，用 bcrypt 会让每次刷新都付出 ~60ms，把热路径
变成 CPU 瓶颈。存 SHA-256 十六进制。

轮转必须原子：`TryRotate` 是 repository 的一个方法（事务不暴露给 service）。
重放已撤销的 token 会撤销该用户全部会话；对已撤销 token 再 logout **不得**
走这条路径，否则双击登出会踢掉其他设备。

对外一律 401 + `UNAUTHORIZED`。`ErrRefreshTokenReused` 只给日志用。

---

## 五、环境变量

所有配置集中在 `internal/config`，启动时一次性读取并校验，非法值直接让进程退出。
**不要在别处写 `os.Getenv`**——新增配置项就加 `Config` 字段并复用现有的
`envString / envDuration / envInt64 / envLogLevel` 助手。

完整列表见 `README.md` 的「环境变量」一节。最常用的几个：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PORT` | `8080` | 服务监听端口 |
| `TRUSTED_PROXIES` | 空（谁都不信任） | 逗号分隔的可信代理 IP / CIDR |
| `CORS_ALLOWED_ORIGINS` | 空（禁止所有跨域） | 逗号分隔的允许跨域来源，`*` 表示全部；留空 = fail-closed |
| `REQUEST_TIMEOUT` | `8s` | 单请求处理超时，必须小于 `WRITE_TIMEOUT` |
| `DATABASE_URL` | **必需** | PostgreSQL 连接串 |
| `JWT_SECRET` | **必需**，≥32 字节 | HS256 密钥，必须在 `LogValue()` 脱敏 |
| `AUTHOR_USERNAME` | **必需**，3–32 字母数字 | 可写 Posts 的 User 名；不是密钥，`LogValue()` 原样写出 |
| `DEV_AUTHOR_PASSWORD` | 空 | 可选开发引导：空库时插入 Author。`LogValue()` 脱敏 |
| `ACCESS_TOKEN_TTL` / `REFRESH_TOKEN_TTL` | `15m` / `720h` | token 有效期 |
| `LOG_LEVEL` / `LOG_FORMAT` | `info` / `json` | 日志级别与格式 |

`TRUSTED_PROXIES` 留空时 `c.ClientIP()` 取 TCP 真实对端地址，客户端无法通过
`X-Forwarded-For` 伪造自身 IP。只有确实部署在反向代理后面时才设置它。

### 5.1 DSN 绝不能进日志

`DATABASE_URL` 里带口令，`JWT_SECRET` 是签名密钥。`config.LogValue()` 会在
**每次启动**时把配置写进日志，所以那里必须走 `redactDSN()` / `redactSecret()`。
新增任何含凭据的配置项时同理：在 `LogValue` 里脱敏，不要在调用处。

`internal/db` 的 `ErrInvalidDSN` 也是同一个原因——pgx 的解析错误会带上原始连接串，
而这个错误会被 `main` 写进日志，所以**不包装原错误**。

---

## 六、项目结构

```
cmd/server/main.go          装配依赖、构造 logger、信号监听、优雅关闭
internal/config/            所有环境变量的读取与校验、DSN / JWT 脱敏
internal/auth/              JWT 签发解析、refresh token 生成与 SHA-256
internal/db/                PostgreSQL 连接池、就绪探针、手写迁移器
  migrate.go                embed + advisory lock + 每迁移一事务
  migrations/*.sql          迁移文件，按文件名顺序执行
internal/repository/        数据访问层：Go 结构体 <-> SQL
internal/service/           业务规则：bcrypt、校验、错误翻译、登录
internal/handler/           HTTP 层
  api.go                    API 结构体：依赖注入的载体
  router.go                 路由注册、中间件顺序、404/405/panic、限流挂载
  health.go                 /api/health  liveness，不查依赖
  ready.go                  /api/ready   readiness，并发跑依赖检查
  auth.go                   登录 / 刷新 / 登出 / me、认证中间件
  author.go                 关闭匿名注册、Author 校验（EqualFold）
  user.go                   POST /api/users，仅 Author
  post.go                   /api/posts Draft 增/查/列/改，仅 Author
  blog.go                   公开 HTML：Home、Post 页、站点风格 404
  author_html.go            /author/* 壳与 Preview JSON
  templates/                embed 进二进制的 HTML 模板与 author.js
  assets/site.css           手写站点样式，GET /site.css
  assets/theme.js           亮/暗切换，GET /theme.js
  ratelimit.go              按 IP 令牌桶 + 空闲淘汰
  cors.go                   fail-closed CORS（按 origin 精确匹配）
  headers.go                安全响应头（每个响应都带）
  info.go / echo.go
  errors.go                 绑定错误 -> 统一错误响应的翻译层
  requestid.go              Request ID 生成、校验、context 传递
  logging.go                slog 访问日志中间件
  middleware.go             请求体上限、单请求超时
internal/markdown/          安全 CommonMark 渲染（goldmark + scheme 白名单）
internal/model/             响应模型与错误码
notes/                      中文学习笔记
Dockerfile                  多阶段构建：golang:1.26 编译 → distroless static 运行
docker-compose.yml          app + postgres 一键编排
.dockerignore               构建上下文裁剪
```

### 中间件顺序（改动前先读这段）

`router.go` 里的注册顺序是有讲究的，改错会静默产生错误的日志：

```
requestID       →  最先，后续所有日志才带得上 ID
requestLogger   →  紧随其后，才能量到完整耗时
CustomRecovery  →  在 logger 内层
timeout         →  设置 deadline
limitBodySize   →  请求体上限
SecurityHeaders →  安全响应头，写在 c.Next() 之前，500/404 也带得上
CORS            →  最后；fail-closed，未配置时不加任何 Access-Control-* 头
```

**Recovery 故意不放最外层**，这与 gin 的常规写法相反。若 Recovery 在最外层，
handler panic 时异常会穿过 `requestLogger` 的 `c.Next()`，此时 Recovery 还没
把状态码置为 500，访问日志会记成错误的状态码。

**安全头与 CORS 都在 `limitBodySize` 之后注册**。两者都在 `c.Next()` 之前写头，
所以即便是超时后的 503、handler 提前 `Abort` 的 404、或 panic 后的 500，
响应也都带得上安全头；CORS 的 preflight 在 `c.Next()` 之前就 `AbortWithStatus(204)`。
CORS 默认 fail-closed：`CORS_ALLOWED_ORIGINS` 留空时不回任何 `Access-Control-*`，
浏览器拒绝全部跨域请求——同源与非浏览器客户端不受影响，不要为了「图省事」
把默认改成 `*`。

---

## 七、操作习惯

1. **优先使用 Claude Code 的文件工具**（Read / Write / Edit / Glob / Grep），
   比 shell 命令更安全可控。
2. **删除文件时只能逐个删除并写明路径**，禁止 `rm -rf`、`find | xargs rm`
   等批量删除。需要批量删除时应停下来让 Jimmy 手动处理。
3. **改动行为后同步更新 `README.md`**，不要留下与代码不符的文档。
4. **先讨论、再定方案、最后写代码**，不要一上来就改。

---

## 八、后续核心方向

1. ~~引入路由框架（Gin / chi）~~ 已完成
2. ~~接数据库前的地基：依赖注入、配置层、结构化日志、Request ID、请求超时、就绪探针~~ 已完成
3. ~~接 PostgreSQL 步骤 A：连接池 + 就绪探针接上真实数据库~~ 已完成
4. ~~接 PostgreSQL 步骤 B：迁移机制 + `users` 表 + 注册接口~~ 已完成
5. ~~接 PostgreSQL 步骤 C：登录 + JWT + refresh 轮转 + 限流~~ 已完成
6. ~~补充中间件：CORS、安全响应头~~ 已完成
7. ~~使用 Docker 容器化部署~~ 已完成
8. ~~尝试 Kubernetes 部署~~ 暂缓；当前方向是个人博客
9. 博客：#2 命名 Author 并关闭公开注册 → #3 Draft JSON → #4 Publish/HTML → #5 Author 区浏览器 → 站点样式（手写 CSS + 页脚亮/暗切换）

### 步骤 C 已落地的规矩（后续直接沿用）

- 登录失败（用户不存在 / 密码错误 / 无效 refresh / 重放）对外都是同一个 401
- 用户不存在时也跑一次假 bcrypt，两条路径耗时接近
- `JWT_SECRET` 必需、≥32 字节、`LogValue()` 脱敏
- refresh 存 SHA-256 不是 bcrypt；轮转走 `TryRotate`；重放才全家撤销
- 探针不限流；登录 / 刷新 / 登出走更严的桶；内存限流每副本独立
- 认证中间件在 timeout 之后、具体路由之前

### 步骤 D 已落地的规矩（后续直接沿用）

- **CORS fail-closed**：`CORS_ALLOWED_ORIGINS` 留空时不回任何 `Access-Control-*`，
  浏览器拒绝全部跨域。不要为了方便把默认改成 `*`
- CORS 按 origin **精确字符串匹配**，不做子串匹配；被命中的来源原样回显为
  `Access-Control-Allow-Origin`（非通配符），配合 `Allow-Credentials: true`
  让凭据类请求可用。`*` 仍是合法的显式「全部放开」选项
- 请求头白名单固定（`corsHeaders`），**不回显** `Access-Control-Request-Headers`
- 安全响应头**无配置项**，写在 `c.Next()` 之前，所以 500/404/超时都带得上
- `X-XSS-Protection` 设为 `0`（关闭旧审计器），不是 `1; mode=block`——
  现代浏览器已移除该机制，旧版本反而是攻击面
- API 响应一律 `Cache-Control: no-store`，因为常带凭据

### 步骤 E 已落地的规矩（后续直接沿用）

- **运行镜像用 distroless static nonroot**：无 shell、非 root（uid 65532）、
  ~10MB。`CGO_ENABLED=0` 出静态二进制才能跑在 distroless/static 上——pgx v5
  是纯 Go 驱动，天然满足。不要换成 alpine「图方便能 exec 进去」，
  镜像有 shell = 攻击面更大
- distroless **无 shell**，所以 `ENTRYPOINT` 必须是 exec 数组形式 `["/server"]`，
  不能用 shell 形式
- **version 在 build 阶段注入**：`-ldflags "-X go-http-service/internal/model.Version=${VERSION}"`。
  distroless 运行阶段没有 git，所以 version 必须在 golang:1.26 那层算出来，
  通过 build-arg 传入
- **compose 里 `DATABASE_URL` 的 host 是服务名 `db`**，不是 `127.0.0.1`——
  容器间走 compose 网络，`127.0.0.1` 指容器自己
- **`JWT_SECRET` 不进 compose 文件**，用 `${JWT_SECRET:?...}` 从宿主机读，
  `:?` 让未设置时直接报错而非静默空值。`AUTHOR_USERNAME` 同理：不是密钥，但仍是必需项
- `app.image:` 和 `build:` 同时写：compose 优先用已存在的镜像，不存在才 build。
  这样本地 `compose up` 自动构建，CI 可 pre-build 后复用、不重建

### 已经立好的规矩（后续直接沿用）

- **关闭顺序**：`defer pool.Close()` 注册在 `srv` 之前，实际执行是
  「排空 HTTP 请求 → 关连接池」。`cmd/server/e2e_test.go` 有断言守着，别改坏它
- **迁移**：加一个 `internal/db/migrations/000N_*.sql` 即可，启动时自动执行。
  改已提交的迁移文件是禁止的——已经跑过的环境不会重跑它
- **依赖检查**：`handler.WithReadyCheck(name, fn)` 挂到 `/api/ready`，
  检查函数必须尊重传入的 context
- **错误分层**：Repository 错误 → Service 错误 → HTTP 状态码，逐层翻译（见 4.8）
- **唯一性靠数据库约束**，不要先查后插（见 4.9）
- 原始错误只写日志，对外只给稳定错误码（见 4.4）

### 端到端测试需要数据库

`DATABASE_URL` 现在是必需项，所以 `cmd/server` 的端到端测试整体需要
`TEST_DATABASE_URL`，未设置就 skip。CI 里配了 postgres service 容器，
并额外跑一遍不带数据库的测试，确保「本地无库也能跑单元测试」不退化。

---

## Agent skills

### Issue tracker

Issues and specs live in this repo's GitHub Issues (`gh`). See `docs/agents/issue-tracker.md`.

### Triage labels

Default five roles, label string equals the role name. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: root `CONTEXT.md` and `docs/adr/`. See `docs/agents/domain.md`.
