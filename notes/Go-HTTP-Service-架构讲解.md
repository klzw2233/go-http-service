# Go HTTP Service 架构讲解笔记

> 项目：`go-http-service`  
> 创建日期：2026-08-21  
> 主题：最小化 Go HTTP 服务的架构、模块、启动与通信机制

---

## 一、项目整体定位

`go-http-service` 是一个 **单体 Go HTTP 服务（Monolithic HTTP Service）**，运行在一个进程中。

当前版本没有数据库、没有前端、没有 Docker，只包含：

- 一个 HTTP Server
- 一个路由处理器（Handler）
- 一个响应结构体

它是一切后续扩展的基础骨架。

```text
┌─────────────────────────────────┐
│          用户 / 浏览器           │
│      curl / Postman / 前端       │
└──────────────┬──────────────────┘
               │ HTTP 请求
               ▼
┌─────────────────────────────────┐
│        Go HTTP Server           │
│      net/http 监听 :8080         │
│                                 │
│   ┌──────────────────────┐      │
│   │   路由 /api/health   │      │
│   │    → healthHandler   │      │
│   └──────────────────────┘      │
└──────────────┬──────────────────┘
               │
               ▼
┌─────────────────────────────────┐
│           JSON 响应             │
│   {"status":"ok","timestamp":...} │
└─────────────────────────────────┘
```

---

## 二、文件结构说明

```
go-http-service/
├── cmd/
│   └── server/
│       └── main.go          # 程序入口
├── go.mod                   # Go 模块定义
├── README.md                # 项目说明
└── notes/
    └── Go-HTTP-Service-架构讲解.md   # 本笔记
```

### 2.1 `go.mod`

**作用**：定义这是一个 Go Module，名称为 `go-http-service`。

它让 Go 编译器知道：

- 这个项目的模块名是什么
- 依赖从哪里下载
- 版本管理使用哪个 Go 版本

当前项目只依赖 Go 标准库，因此 `go.mod` 内容非常简洁。

### 2.2 `cmd/server/main.go`

这是整个项目唯一一个可执行文件。

Go 项目的常见目录约定：

| 路径 | 含义 |
|------|------|
| `cmd/` | 存放可执行程序的入口 |
| `cmd/server/main.go` | server 程序的入口 |
| `cmd/worker/main.go` | 后续可添加后台 worker |
| `cmd/cli/main.go` | 后续可添加命令行工具 |

**好处**：一个代码仓库里可以有多个可执行入口，分别对应不同的运行形态。

---

## 三、`main.go` 内部模块详解

`main.go` 可以拆分为四个核心模块来理解。

### 3.1 响应结构体 `HealthResponse`

```go
type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}
```

**作用**：定义 API 返回给客户端的数据结构。

`json:"status"` 是 Go 的 **struct tag**，它告诉 JSON 编码器：

> 把这个字段序列化成 JSON 时，名字用小写的 `status`，不要用 `Status`。

这是后端开发中最常见的数据流转模式：

```text
Go struct 定义数据结构 → JSON 编码器 → HTTP 响应体
```

### 3.2 请求处理器 `healthHandler`

```go
func healthHandler(w http.ResponseWriter, r *http.Request)
```

这是 HTTP 请求的实际处理函数。两个参数是 Go 标准库的固定约定：

| 参数 | 类型 | 含义 |
|------|------|------|
| `w` | `http.ResponseWriter` | 用于向客户端写响应 |
| `r` | `*http.Request` | 包含客户端请求的所有信息 |

当前 handler 完成三件事：

1. **检查请求方法**：只允许 `GET`，其他方法返回 `405 Method Not Allowed`
2. **设置响应头**：`Content-Type: application/json`
3. **构造并返回 JSON**：把 `HealthResponse` 编码后写入响应体

### 3.3 路由注册

```go
http.HandleFunc("/api/health", healthHandler)
```

这行代码把「路径 `/api/health`」和「处理函数 `healthHandler`」绑定起来。

当客户端访问 `http://localhost:8080/api/health` 时，Go 就会自动调用 `healthHandler`。

### 3.4 HTTP Server 启动

```go
server := &http.Server{
	Addr:         ":" + port,
	ReadTimeout:  5 * time.Second,
	WriteTimeout: 10 * time.Second,
	IdleTimeout:  60 * time.Second,
}
```

这里创建了一个真正的 HTTP Server 实例，并配置了三个超时参数：

| 超时参数 | 含义 |
|----------|------|
| `ReadTimeout` | 读取整个请求的最长时间，防止慢连接攻击 |
| `WriteTimeout` | 写入响应的最长时间 |
| `IdleTimeout` | 长连接保持空闲的最长时间 |

随后调用：

```go
server.ListenAndServe()
```

这行代码会让程序**阻塞**，持续监听端口，直到发生错误或被外部终止。

---

## 四、启动流程：从 `go run` 到返回响应

完整启动与响应流程如下：

| 步骤 | 动作 |
|------|------|
| 1 | 执行 `go run cmd/server/main.go`，Go 编译器编译生成临时可执行文件 |
| 2 | 程序入口 `func main()` 开始执行 |
| 3 | 设置监听端口：8080 |
| 4 | 注册路由：`/api/health` → `healthHandler` |
| 5 | 创建 HTTP Server 实例，配置超时参数 |
| 6 | 调用 `server.ListenAndServe()`，开始监听端口 |
| 7 | 客户端发起 `GET /api/health` 请求 |
| 8 | Go HTTP Server 接收连接并解析请求 |
| 9 | 根据路由表匹配到 `healthHandler` |
| 10 | 调用 `healthHandler(w, r)`，构造 JSON 响应 |
| 11 | 响应返回给客户端，连接关闭或复用 |

---

## 五、通信机制

### 5.1 外部通信

这个服务与外界的通信方式只有一种：**HTTP 协议**。

```text
客户端 ──HTTP/1.1──▶ Go Server ──HTTP 响应──▶ 客户端
```

这是前后端通信最基础的方式，也是 REST API 的起点。

### 5.2 内部通信

当前项目只有一个 handler、一个进程，因此**没有内部服务间通信**。

后续扩展后，项目内部会形成分层调用：

```text
HTTP Handler → Service Layer → Repository → PostgreSQL
            ↓
        Logger / Config / Error handling
```

每一层之间都是函数调用，不是网络通信。

---

## 六、当前代码体现的工程意识

虽然项目很小，但已经包含了一些好的工程习惯：

| 设计 | 作用 |
|------|------|
| `cmd/server/main.go` | 标准 Go 项目布局，支持多入口 |
| `HealthResponse` struct | 数据结构与 JSON 序列化格式分离 |
| 405 Method Not Allowed | 基础 API 安全性 |
| `Content-Type: application/json` | 明确声明响应类型 |
| Timeout 配置 | 防止慢连接攻击和资源泄露 |
| `log.Fatalf` | 启动失败时给出清晰错误信息 |
| UTC 时间戳 | 避免时区歧义 |

---

## 七、后续扩展方向

这个最小化服务可以作为起点，向以下方向扩展：

### 7.1 引入路由框架

如 Gin、Echo、chi，支持更复杂的路由：

```text
GET    /api/health
GET    /api/users
POST   /api/users
GET    /api/users/:id
DELETE /api/users/:id
```

### 7.2 分层架构

```
cmd/server/main.go          # 启动入口
internal/
├── handler/                # HTTP 处理器
├── service/                # 业务逻辑
├── repository/             # 数据访问
├── model/                  # 数据模型
└── config/                 # 配置读取
```

### 7.3 添加数据库

用 PostgreSQL 持久化数据，学习：

- `database/sql`
- `pgx`
- Migration 工具（如 golang-migrate）
- 事务（Transaction）

### 7.4 添加中间件

- Logging middleware：记录每个请求
- Recovery middleware：panic 时不崩溃
- CORS middleware：允许前端跨域访问
- Auth middleware：JWT 鉴权

### 7.5 配置文件管理

用 `.env` 或 `config.yaml` 管理端口、数据库地址、JWT 密钥等参数。

---

## 八、总结

> 这个最小化项目虽然代码量少，但已经覆盖了 **HTTP 服务的完整生命周期**：启动 → 监听 → 路由分发 → Handler 处理 → JSON 响应 → 错误处理。

它是后续开发博客系统、Docker 容器化、Kubernetes 部署的基础。

---

## 参考命令

启动服务：

```bash
cd go-http-service
go run cmd/server/main.go
```

测试接口：

```bash
curl http://localhost:8080/api/health
```

编译验证：

```bash
go build ./cmd/server
```
