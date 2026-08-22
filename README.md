# Go HTTP Service

> Backend Engineer 学习路线的第一个实战项目：从零搭建一个 Go HTTP 服务。
> 当前已完成接入数据库前的全部地基——依赖注入、集中配置、结构化日志、
> 请求关联 ID、单请求超时、存活/就绪双探针，并有 CI 与端到端测试保障。

---

## 项目目标

建立一个可用于后续项目演进的 Go 后端服务基础骨架：

- 使用 [Gin](https://github.com/gin-gonic/gin) Web 框架启动 HTTP 服务
- 按分层架构组织代码，依赖通过构造函数注入而非包级变量
- 对外契约稳定：统一的错误响应结构，绝不泄露内部实现细节
- 具备生产服务的基本要素：超时、优雅关闭、结构化日志、探针
- 为下一步接入 PostgreSQL 铺好路（见文末「下一步计划」）

### 已完成的能力

| 能力 | 说明 |
|------|------|
| 依赖注入 | handler 是 `*API` 的方法，依赖由 `handler.New()` 注入 |
| 配置集中管理 | 全部环境变量在 `internal/config` 读取并校验，非法值拒绝启动 |
| 结构化日志 | `log/slog`，JSON / text 可切换，级别随状态码变化 |
| 请求关联 ID | `X-Request-Id`，注入 `context`，客户端传入值经过校验 |
| 两层超时 | `http.Server` 四项 + 单请求 `REQUEST_TIMEOUT` |
| 优雅关闭 | `SIGINT` / `SIGTERM` 排空在途请求后退出 |
| 探针分级 | `/api/health` 存活、`/api/ready` 就绪（可挂依赖检查） |
| 安全默认 | 不信任任何代理、请求体上限、panic 不泄露堆栈 |
| 测试 | 单元 + 端到端（启动真实进程走真实 TCP），CI 跑竞态检测 |

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
│   │   ├── config.go            # 所有环境变量的读取与校验
│   │   └── config_test.go
│   ├── handler/
│   │   ├── api.go               # API 结构体：依赖注入的载体
│   │   ├── router.go            # 路由注册、中间件顺序、404/405/panic
│   │   ├── health.go            # /api/health   liveness
│   │   ├── ready.go             # /api/ready    readiness（可挂依赖检查）
│   │   ├── info.go              # /api/info
│   │   ├── echo.go              # /api/echo
│   │   ├── errors.go            # 绑定错误 -> 统一错误响应的翻译层
│   │   ├── requestid.go         # Request ID 生成、校验、context 传递
│   │   ├── logging.go           # slog 访问日志中间件
│   │   ├── middleware.go        # 请求体上限、单请求超时
│   │   ├── main_test.go         # TestMain、测试夹具与请求助手
│   │   ├── handler_test.go      # 四个接口、路由、错误契约
│   │   ├── middleware_test.go   # Request ID、超时、日志中间件
│   │   └── ready_test.go        # 就绪检查：并发、失败、panic、不泄露
│   └── model/
│       ├── health.go            # HealthResponse
│       ├── ready.go             # ReadyResponse、CheckResult
│       ├── info.go              # InfoResponse、服务名与版本号
│       ├── echo.go              # EchoRequest, EchoResponse
│       ├── error.go             # ErrorResponse、错误码常量
│       └── model_test.go
├── notes/                       # 中文学习笔记
├── .github/workflows/ci.yml     # CI：换行符 / gofmt / vet / build / race / tidy
├── .gitattributes               # 换行符规范（LF）
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

```bash
go run cmd/server/main.go
```

默认监听 `8080`。需要换端口时通过 `PORT` 环境变量指定，无需重新编译：

```bash
PORT=9000 go run cmd/server/main.go
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

预期返回 `200`，当前尚未接入任何依赖，因此检查列表为空：

```json
{"status":"ready","timestamp":"...","checks":[]}
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
  "go_version": "go1.26.5",
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

### 5. 运行测试

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

---

## API 列表

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/health` | 存活探针（liveness） |
| GET | `/api/ready` | 就绪探针（readiness） |
| GET | `/api/info` | 返回服务元信息 |
| POST | `/api/echo` | 接收 JSON 并回显 |

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
  "timestamp": "2026-08-22T12:00:00Z",
  "checks": [
    { "name": "database", "status": "ok", "duration_ms": 3 }
  ]
}
```

任一检查失败时返回 **503**，`status` 变为 `not_ready`，对应检查的
`status` 为 `failed`。当前尚未接入任何依赖，因此 `checks` 是空数组、恒返回 200。

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
| `PAYLOAD_TOO_LARGE` | 413 | 请求体超过 `MAX_BODY_BYTES` 上限 |
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
| `READ_HEADER_TIMEOUT` | `5s` | 读取请求头的时长上限 |
| `READ_TIMEOUT` | `10s` | 读取完整请求的时长上限 |
| `WRITE_TIMEOUT` | `10s` | 写响应的时长上限 |
| `IDLE_TIMEOUT` | `60s` | keep-alive 空闲连接存活上限 |
| `SHUTDOWN_TIMEOUT` | `15s` | 优雅关闭时等待在途请求的上限 |
| `REQUEST_TIMEOUT` | `8s` | **单个请求内 handler 的处理时长上限** |
| `MAX_BODY_BYTES` | `1048576` | 请求体大小上限（1 MiB） |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `LOG_FORMAT` | `json` | `json` 或 `text` |
| `GIN_MODE` | 未设置时按 `release` 运行 | 由 gin 自身读取，见下 |

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

## 下一步计划

接数据库前的地基已经完成（见 `notes/接入数据库前的地基.md`），
接入 PostgreSQL 时只剩三件事：

1. `internal/config` 加 `DatabaseURL` 等字段，复用现有的 env 读取助手
2. 新建 `internal/db/db.go` 建连接池
3. `cmd/server/main.go` 装配：

```go
pool, err := db.Connect(ctx, cfg)
if err != nil {
    return fmt.Errorf("database: %w", err)
}
defer pool.Close()   // 必须写在 srv.Shutdown 之前注册，否则会切断在执行的查询

api := handler.New(cfg, logger,
    handler.WithReadyCheck("database", pool.Ping))   // 挂到 /api/ready
```

然后按 `notes/分层架构.md` 建 `internal/repository` 和 `internal/service`，
依赖方向 Handler → Service → Repository，不得反向。

### 之后

1. 实现用户注册 / 登录 / JWT 认证
2. 补充中间件：CORS、限流、安全响应头
3. 使用 Docker 容器化部署
4. 尝试 Kubernetes 部署（`/api/health` 与 `/api/ready` 已可直接对接探针）

---

## 相关笔记

| 笔记 | 内容 |
|------|------|
| `notes/接入数据库前的地基.md` | 本轮六项改造的完整推导与取舍 |
| `notes/代码审查问题清单与改进计划.md` | 上一轮代码审查的 15 个问题与修复记录 |
| `notes/分层架构.md` | 四层架构与依赖方向 |
| `notes/连接 PostgreSQL 数据库.md` | 驱动选择、连接池、迁移工具 |
| `notes/添加中间件.md` | 中间件原理与注册顺序 |
