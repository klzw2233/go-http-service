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
cd "E:\Program Files\Claude Code\BackendEngineer\go-http-service"
```

### 2. 运行服务

```bash
go run cmd/server/main.go
```

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

- 服务监听端口：**8080**
- 基础路径：**/api**

---

## 下一步计划

1. 连接 PostgreSQL 数据库
2. 实现用户注册/登录接口
3. 添加中间件（日志、恢复、CORS）
4. 使用 Docker 容器化部署
