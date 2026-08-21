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
│       └── main.go              # 服务入口：超时配置、信号监听、优雅关闭
├── internal/
│   ├── handler/
│   │   ├── router.go            # 路由注册、代理信任、404/405/panic 处理
│   │   ├── health.go            # /api/health handler
│   │   ├── info.go              # /api/info handler
│   │   ├── echo.go              # /api/echo handler
│   │   ├── errors.go            # 绑定错误 -> 统一错误响应的翻译层
│   │   ├── middleware.go        # 请求体大小限制中间件
│   │   └── clock.go             # 可注入的时间源（便于测试）
│   └── model/
│       ├── health.go            # HealthResponse
│       ├── info.go              # InfoResponse、服务名与版本号
│       ├── echo.go              # EchoRequest, EchoResponse
│       └── error.go             # ErrorResponse、错误码常量
├── notes/                       # 学习笔记
├── .gitattributes               # 换行符规范（LF）
├── go.mod                       # Go 模块定义
├── go.sum                       # 依赖校验和
└── README.md                    # 项目说明
```

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
| GET | `/api/health` | 健康检查 |
| GET | `/api/info` | 返回服务元信息 |
| POST | `/api/echo` | 接收 JSON 并回显 |

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
| `PAYLOAD_TOO_LARGE` | 413 | 请求体超过 1 MiB 上限 |
| `INTERNAL_ERROR` | 500 | 服务端异常，详情只写日志 |

> 底层的原始错误（validator、`encoding/json` 的报错）只写入服务端日志，
> **不会返回给客户端**。这些报错里含有 Go 内部结构体名（如 `EchoRequest`），
> 且措辞会随依赖库版本变化，不适合作为对外契约。

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

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PORT` | `8080` | 服务监听端口 |
| `TRUSTED_PROXIES` | 空（谁都不信任） | 逗号分隔的可信代理 IP / CIDR 列表 |

关于 `TRUSTED_PROXIES`：gin 默认信任**所有**代理，这会让 `c.ClientIP()`
无条件采信客户端自己发来的 `X-Forwarded-For` 头，即客户端可以随意伪造自身 IP。
本服务默认不信任任何代理，`ClientIP()` 取 TCP 连接的真实对端地址。

只有当服务确实部署在反向代理（Nginx、负载均衡器）之后时，才需要设置：

```bash
TRUSTED_PROXIES="10.0.0.0/8,172.16.0.0/12" go run cmd/server/main.go
```

## 服务器超时设置

服务不使用 `gin.Engine.Run()`，而是显式构造 `http.Server` 以设置超时。
`Run()` 内部创建的 server 四项超时全为零值（即永不超时），会留下
Slowloris 攻击面。

| 参数 | 取值 | 作用 |
|------|------|------|
| `ReadHeaderTimeout` | 5s | 限制客户端发送请求头的时长，防 Slowloris |
| `ReadTimeout` | 10s | 限制读取完整请求（含 body）的时长 |
| `WriteTimeout` | 10s | 限制写响应的时长 |
| `IdleTimeout` | 60s | 限制 keep-alive 连接的空闲存活时长 |
| 关闭排空上限 | 15s | 优雅关闭时等待在途请求的上限 |

---

## 下一步计划

1. 连接 PostgreSQL 数据库
2. 实现用户注册/登录接口
3. 添加中间件（日志、恢复、CORS）
4. 使用 Docker 容器化部署
