# 项目环境说明

> 文件位置：`go-http-service/CLAUDE.md`
> 用途：告知 Claude Code 本项目的运行环境、约定与注意事项
> 最后更新：2026-08-22（从 Win10 + Git Bash 改写为 Ubuntu 虚拟机环境）

---

## 一、运行环境

| 项目 | 值 |
|------|-----|
| 操作系统 | Ubuntu 虚拟机（Linux 6.8.0） |
| Shell | bash |
| 项目根目录 | `~/workspace/go-http-service` |
| Go 版本 | go1.26.5 |
| 编辑方式 | 常从 Win10 通过 VS Code Remote SSH 连入 |

这是标准 Linux 环境，**直接使用 Linux 命令即可**，不需要考虑 Windows 路径或
PowerShell 兼容性。旧版本本文件描述的 `E:\Program Files\...` 路径已经作废。

---

## 二、常用命令

```bash
# 运行服务（默认 8080）
go run cmd/server/main.go

# 换端口
PORT=9000 go run cmd/server/main.go

# 构建
go build -o server ./cmd/server

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

# 带数据库运行
DATABASE_URL="postgres://app:devsecret@127.0.0.1:5433/go_http_service?sslmode=disable" \
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
| `REQUEST_TIMEOUT` | `8s` | 单请求处理超时，必须小于 `WRITE_TIMEOUT` |
| `DATABASE_URL` | 空（不连库） | PostgreSQL 连接串，**目前可选** |
| `LOG_LEVEL` / `LOG_FORMAT` | `info` / `json` | 日志级别与格式 |

`TRUSTED_PROXIES` 留空时 `c.ClientIP()` 取 TCP 真实对端地址，客户端无法通过
`X-Forwarded-For` 伪造自身 IP。只有确实部署在反向代理后面时才设置它。

### 5.1 DSN 绝不能进日志

`DATABASE_URL` 里带口令。`config.LogValue()` 会在**每次启动**时把配置写进日志，
所以那里必须走 `redactDSN()`。新增任何含凭据的配置项时同理：
在 `LogValue` 里脱敏，不要在调用处。

`internal/db` 的 `ErrInvalidDSN` 也是同一个原因——pgx 的解析错误会带上原始连接串，
而这个错误会被 `main` 写进日志，所以**不包装原错误**。

---

## 六、项目结构

```
cmd/server/main.go          装配依赖、构造 logger、信号监听、优雅关闭
internal/config/            所有环境变量的读取与校验、DSN 脱敏
internal/db/                PostgreSQL 连接池与就绪探针
internal/handler/           HTTP 层
  api.go                    API 结构体：依赖注入的载体
  router.go                 路由注册、中间件顺序、404/405/panic
  health.go                 /api/health  liveness，不查依赖
  ready.go                  /api/ready   readiness，并发跑依赖检查
  info.go / echo.go
  errors.go                 绑定错误 -> 统一错误响应的翻译层
  requestid.go              Request ID 生成、校验、context 传递
  logging.go                slog 访问日志中间件
  middleware.go             请求体上限、单请求超时
internal/model/             响应模型与错误码
notes/                      中文学习笔记
```

### 中间件顺序（改动前先读这段）

`router.go` 里的注册顺序是有讲究的，改错会静默产生错误的日志：

```
requestID      →  最先，后续所有日志才带得上 ID
requestLogger  →  紧随其后，才能量到完整耗时
CustomRecovery →  在 logger 内层
timeout        →  设置 deadline
limitBodySize  →  最后
```

**Recovery 故意不放最外层**，这与 gin 的常规写法相反。若 Recovery 在最外层，
handler panic 时异常会穿过 `requestLogger` 的 `c.Next()`，此时 Recovery 还没
把状态码置为 500，访问日志会记成错误的状态码。

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
4. 接 PostgreSQL 步骤 B：迁移机制 + `users` 表 + 注册接口
5. 接 PostgreSQL 步骤 C：登录 + JWT 认证
6. 补充中间件：CORS、限流、安全响应头
7. 使用 Docker 容器化部署
8. 尝试 Kubernetes 部署

### 步骤 B 要做什么

步骤 A 只做了「连得上」，刻意不含任何业务逻辑，也**刻意没有引入迁移机制**——
当时一张表都没有，引入就是没有迁移文件的空脚手架。

1. **迁移机制**：`goose` 或 `golang-migrate` + `embed.FS`，第一个迁移是 `users` 表
2. **`internal/repository` / `internal/service`**：按 `notes/分层架构.md` 的四层结构新建，
   Handler → Service → Repository，**不得反向依赖**
3. **`POST /api/users`**：注册接口，密码用 bcrypt 或 argon2 哈希，绝不明文入库
4. **`DATABASE_URL` 改为必需**：那时候服务真的离不开数据库了。
   需要同步改 `config.validate()`，并给 e2e 测试配上数据库

写查询时记住 4.6：**`pool.Query` 必须接收 `c.Request.Context()`**，
否则 `REQUEST_TIMEOUT` 对它无效。

### 已经立好的规矩（步骤 B 直接沿用）

- 关闭顺序：`defer pool.Close()` 注册在 `srv` 之前，实际执行是
  「排空 HTTP 请求 → 关连接池」。反过来会切断正在执行的查询。
  `cmd/server/e2e_test.go` 有断言守着这个顺序，别改坏它
- 依赖检查：`handler.WithReadyCheck(name, fn)` 挂到 `/api/ready`，
  检查函数必须尊重传入的 context
- 检查失败只返回通用原因，原始错误只写日志（见 4.4）
