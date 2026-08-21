# Go HTTP Service 后续扩展方向

> 项目：`go-http-service`
> 主题：最小化 Go HTTP 服务完成后的五个扩展方向

---

## 概述

当前项目已经完成一个最小化的 Go HTTP 服务，只有一个健康检查接口 `/api/health`。

下一步可以往五个方向扩展：

1. 引入 Gin 路由框架
2. 添加 PostgreSQL 数据库连接
3. 添加日志和配置模块
4. 写 Dockerfile 容器化
5. 添加测试用例

这五个方向的关系大致如下：

```
1. 引入 Gin 路由框架
        ↓
2. 添加 PostgreSQL 数据库连接
        ↓
3. 添加日志和配置模块
        ↓
4. 写 Dockerfile 容器化
        ↓
5. 添加测试用例（贯穿始终）
```

---

## 一、引入 Gin 路由框架

### 是什么

Gin 是 Go 语言中非常流行的 Web 框架，基于 Go 标准库 `net/http` 封装，提供更便捷的路由、中间件、参数绑定等功能。

### 当前项目的限制

当前项目使用标准库：

```go
http.HandleFunc("/api/health", healthHandler)
```

这个写法在接口较少时没问题，但接口一多就会遇到限制：

| 限制 | 说明 |
|------|------|
| 路由不灵活 | 无法处理 `/api/users/:id` 这种带参数的路由 |
| 中间件实现麻烦 | 日志、鉴权、错误恢复需要自己写 |
| 参数解析繁琐 | JSON body、query string、path 参数都要手动读取 |
| 响应格式重复 | 每个 handler 都要设置 `Content-Type`、编码 JSON |

### Gin 能带来的能力

```go
// 带参数的路由
r.GET("/api/users/:id", getUserHandler)

// POST 请求，自动把 JSON body 绑定到结构体
r.POST("/api/users", createUserHandler)

// 全局中间件
r.Use(loggerMiddleware, recoveryMiddleware)
```

### 什么时候引入

建议在接口数量超过 3 个，或者开始做博客系统之前引入。

### 重点学习内容

- `gin.Engine` 路由注册
- `gin.Context` 请求上下文
- JSON binding 和 validation
- Middleware 编写
- 路由分组（Group）

---

## 二、添加 PostgreSQL 数据库连接

### 是什么

PostgreSQL 是一个关系型数据库。让 Go 程序连接 PostgreSQL，可以把数据持久化保存到硬盘上。

### 当前项目的限制

当前项目的数据都写在代码里，比如 `HealthResponse`。程序一关闭，数据就消失了。

如果要做博客系统、用户系统、下载管理器，就必须把数据保存到数据库中。

### 数据库连接后的架构

```text
Go 程序 ──SQL──▶ PostgreSQL 数据库
                  │
                  ├── users 表
                  ├── posts 表
                  └── categories 表
```

### 需要学习的内容

| 主题 | 说明 |
|------|------|
| SQL 基础 | SELECT、INSERT、UPDATE、DELETE、JOIN、INDEX |
| Go 数据库驱动 | `database/sql`（标准库）或 `pgx`（性能更好） |
| Migration | 管理数据库表结构变更，如 `golang-migrate` |
| Transaction | 事务：保证多个操作原子性执行 |

### 什么时候引入

这是做博客系统的前提，建议在学完 Gin 之后引入。

---

## 三、添加日志和配置模块

### 日志模块

#### 是什么

日志模块用于记录程序运行过程中发生的事件，比如请求信息、错误信息、警告信息等。

#### 当前项目的限制

当前项目只使用了一个简单的 `log.Printf`：

```go
log.Printf("failed to encode health response: %v", err)
```

它的缺点：

- 没有日志级别（debug、info、warn、error）
- 格式不统一
- 不是结构化输出，不利于后续用日志系统分析

#### 生产环境的日志应该是什么样

```json
{"level":"error","time":"2026-08-21T10:00:00Z","msg":"db connection failed","error":"timeout"}
```

Go 常用日志库：

- 标准库 `log/slog`（Go 1.21+ 推荐）
- `uber-go/zap`（高性能）
- `sirupsen/logrus`（功能丰富）

### 配置模块

#### 是什么

配置模块把可变参数从代码里抽出来，放到环境变量或配置文件中。

#### 当前项目的限制

当前项目是硬编码：

```go
port := "8080"
```

在生产环境中，这些参数不应该写在代码里：

```bash
APP_PORT=8080
DB_HOST=localhost
DB_PASSWORD=secret
JWT_SECRET=xxx
```

#### 常用方式

| 方式 | 说明 |
|------|------|
| 环境变量 | 适合 Docker、Kubernetes 部署 |
| `.env` 文件 | 开发环境常用 |
| `config.yaml` / `config.json` | 本地配置文件 |

### 什么时候引入

建议在引入数据库之后、做用户认证之前引入。这是项目从「玩具」走向「工程」的重要标志。

---

## 四、写 Dockerfile 容器化

### 是什么

Dockerfile 是一个脚本，告诉 Docker 如何把 Go 程序打包成一个镜像。打包后可以通过命令运行：

```bash
docker run myapp
```

### 当前项目的限制

当前项目运行需要：

1. 安装 Go
2. 执行 `go run cmd/server/main.go`
3. 保持终端开启

这样做的问题：

| 问题 | 说明 |
|------|------|
| 环境依赖多 | 换机器要重新安装 Go |
| 启动不标准 | 不适合作为长期服务运行 |
| 部署复杂 | 无法快速复制运行环境 |

### 容器化后的好处

- 只要机器有 Docker，就能运行
- 一次构建，到处运行
- 可以用 `docker-compose.yml` 管理多个服务

### Dockerfile 典型结构

```dockerfile
# 第一阶段：编译
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o server cmd/server/main.go

# 第二阶段：运行
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/server .
CMD ["./server"]
```

### 什么时候引入

建议在完成一个能独立运行的版本（例如博客系统 + 数据库连接）之后引入。这是你 Docker 能力从「使用者」进阶到「构建者」的关键一步。

---

## 五、添加测试用例

### 是什么

测试用例是自动验证代码是否正确的代码。测试用例会在代码修改后自动检查是否引入了 bug。

Go 的测试文件以 `_test.go` 结尾：

```go
func TestHealthHandler(t *testing.T) {
    // 构造请求
    // 调用 healthHandler
    // 验证返回状态码和 JSON 内容
}
```

### 当前项目的限制

没有测试时，每次修改代码都要手动启动服务、手动调用接口验证。效率低，而且容易遗漏场景。

### 测试带来的好处

| 好处 | 说明 |
|------|------|
| 自动验证 | 修改代码后跑测试即可知道是否出错 |
| 防止回归 | 新功能不影响旧功能 |
| 作为文档 | 测试代码展示接口如何使用 |
| 重构信心 | 有测试才敢大胆重构 |

### 测试类型

| 类型 | 说明 |
|------|------|
| 单元测试 | 测试一个函数 |
| 集成测试 | 测试多个模块协作 |
| HTTP Handler 测试 | 直接调用 handler，无需启动真实服务 |

### 什么时候引入

建议从第一个 handler 开始写测试，但很多人是后补。

现在就可以为 `/api/health` 编写测试用例，作为测试学习的起点。

---

## 六、推荐推进顺序

对于本项目，推荐的扩展顺序如下：

| 顺序 | 方向 | 目标 |
|------|------|------|
| 1 | Gin 路由框架 | 扩展出多个 API 接口 |
| 2 | PostgreSQL | 实现数据持久化 |
| 3 | 日志和配置 | 项目工程化 |
| 4 | Dockerfile | 容器化部署能力 |
| 5 | 测试用例 | 贯穿整个开发过程 |

其中 **Gin 路由框架** 最值得先开始，因为它学习成本低、收益高，是后续做博客系统的基础。

---

## 七、下一步建议

如果从零开始扩展，推荐顺序：

1. 先用 Gin 重写 `/api/health`
2. 增加 `GET /api/info`、`POST /api/echo` 等接口练习
3. 引入 PostgreSQL，做用户表的增删改查
4. 添加日志和配置模块
5. 写 Dockerfile 跑起来
6. 给关键接口补测试

> 核心原则：**不要只学理论，每个方向都要落实在这个项目上**。
