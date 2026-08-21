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
│       └── main.go              # 服务入口，只负责启动
├── internal/
│   ├── handler/
│   │   ├── health.go            # /api/health, /api/info handler
│   │   ├── echo.go              # /api/echo handler
│   │   └── router.go            # 路由注册
│   └── model/
│       ├── health.go            # HealthResponse, InfoResponse
│       └── echo.go              # EchoRequest, EchoResponse
├── notes/                       # 学习笔记
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

### 4. 运行测试

```bash
go test ./...
```

显示详细输出：

```bash
go test -v ./...
```

只运行某个测试：

```bash
go test -v ./internal/handler -run TestEchoHandler
```

---

## API 列表

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/health` | 健康检查 |
| GET | `/api/info` | 返回服务元信息 |
| POST | `/api/echo` | 接收 JSON 并回显 |

---

## 端口说明

- 默认监听端口：**8080**（可用 `PORT` 环境变量覆盖）
- 基础路径：**/api**

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
