# Go HTTP Service

> Backend Engineer 学习路线的第一个实战项目：从零搭建一个最小化的 Go HTTP 服务。
> 当前版本已引入 Gin 框架，并按分层架构组织代码。

---

## 项目目标

建立一个可用于后续项目演进的 Go 后端服务基础骨架：

- 使用 [Gin](https://github.com/gin-gonic/gin) Web 框架启动 HTTP 服务
- 按分层架构组织代码
- 为后续添加 REST API、数据库、中间件等能力打下基础

---

## 项目结构

```
go-http-service/
├── cmd/
│   └── server/
│       └── main.go              # 装配依赖、构造 logger、信号监听、优雅关闭
├── internal/
│   ├── config/
│   │   └── config.go            # 所有环境变量的读取与校验
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
│   │   └── middleware.go        # 请求体上限、单请求超时
│   └── model/
│       ├── health.go            # HealthResponse
│       ├── ready.go             # ReadyResponse、CheckResult
│       ├── info.go              # InfoResponse、服务名与版本号
│       ├── echo.go              # EchoRequest, EchoResponse
│       └── error.go             # ErrorResponse、错误码常量
├── notes/                       # 学习笔记
├── .github/workflows/ci.yml     # CI：gofmt / vet / build / race test
├── .gitattributes               # 换行符规范（LF）
├── go.mod                       # Go 模块定义
├── go.sum                       # 依赖校验和
└── README.md                    # 项目说明
```

> 依赖注入说明：handler 是 `*API` 的方法而非包级函数，`API` 持有配置、
> logger、时间源。接入 PostgreSQL 时，连接池作为 `API` 的又一个字段传入，
> 不需要再动路由和 handler 的结构。

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
等待在途请求处理完毕（最多 15 秒）后再退出。

### 3. 测试接口

#### 健康检查

```bash
curl http://localhost:8080/api/health
```

预期返回：

```json
{"status":"ok","timestamp":"..."}
```

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

显示详细输出：

```bash
go test -v ./...
```

只运行某个测试：

```bash
go test -v ./internal/handler -run TestEchoEndpoint
```

查看覆盖率：

```bash
go test -cover ./...
```

竞态检测（测试中使用了 `t.Parallel`，建议提交前跑一次）：

```bash
go test -race ./...
```

当前覆盖率：`internal/model` 100%，`internal/handler` 82.9%。

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

| 限制 | 取值 | 说明 |
|------|------|------|
| 请求体总大小 | 1 MiB | 由 `limitBodySize` 中间件对全局路由生效 |
| `/api/echo` 的 `message` 长度 | 4096 字符 | 由 binding tag `max=4096` 约束 |

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

使用标准库 `log/slog`，默认输出 JSON，每个请求一条访问日志：

```json
{"time":"2026-08-22T12:00:00Z","level":"INFO","msg":"request","method":"GET",
 "path":"/api/health","status":200,"duration_ms":0,"client_ip":"127.0.0.1",
 "bytes":58,"request_id":"3f2a...c81"}
```

日志级别随状态码变化，便于只筛异常：2xx/3xx 记 `INFO`，4xx 记 `WARN`，5xx 记 `ERROR`。

**查询串不写入日志**——它经常携带 token 和 API key。

### Request ID

每个请求都会分配一个关联 ID，写入响应头 `X-Request-Id`，并注入
`c.Request.Context()`，后续的数据库查询可以直接携带它。

客户端可以自带 `X-Request-Id` 以支持跨服务链路追踪，但**会先做校验**：
长度须 ≤ 64 且仅含 `[A-Za-z0-9_-]`。不合规就丢弃并重新生成——
否则客户端可以往日志里注入换行、控制字符或伪造的 JSON 字段。

---

## 下一步计划

1. 连接 PostgreSQL 数据库
2. 实现用户注册/登录接口
3. 添加中间件（日志、恢复、CORS）
4. 使用 Docker 容器化部署
