# 使用 Docker 容器化部署

> 项目：`go-http-service`  
> 主题：如何使用 Docker 打包和部署 Go 服务

---

## 一、为什么要容器化

当前项目运行需要：

1. 安装 Go 环境
2. 执行 `go run cmd/server/main.go`
3. 在终端保持运行

这种方式有几个问题：

| 问题 | 说明 |
|------|------|
| 环境依赖多 | 换一台机器要重新安装 Go |
| 部署不标准 | 不适合作为长期后台服务运行 |
| 缺少进程管理 | 崩溃后不会自动重启 |
| 环境不一致 | 开发环境和生产环境配置容易不同 |

Docker 容器化后：

- 一次构建，到处运行
- 只要机器有 Docker，无需安装 Go
- 可以用 Docker Compose 管理多个服务
- 方便和 PostgreSQL、Redis 等服务一起部署

---

## 二、Docker 核心概念

| 概念 | 说明 |
|------|------|
| Dockerfile | 描述如何构建镜像的脚本 |
| Image | 打包好的应用运行环境 |
| Container | 运行中的镜像实例 |
| Volume | 容器外挂载的存储，用于持久化数据 |
| Network | 容器之间通信的网络 |

---

## 三、编写 Dockerfile

### 3.1 多阶段构建

推荐用多阶段构建，最终镜像只包含编译好的二进制文件，体积小：

```dockerfile
# 第一阶段：编译
FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum . /
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server cmd/server/main.go

# 第二阶段：运行
FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/server .

EXPOSE 8080

CMD ["./server"]
```

### 3.2 关键说明

| 指令 | 作用 |
|------|------|
| `FROM golang:1.26-alpine AS builder` | 编译环境 |
| `go mod download` | 提前下载依赖，利用缓存 |
| `CGO_ENABLED=0` | 禁用 CGO，可以生成静态二进制 |
| `FROM alpine:latest` | 运行环境，非常轻量 |
| `COPY --from=builder` | 只拷贝编译产物 |
| `EXPOSE 8080` | 声明服务端口 |
| `CMD ["./server"]` | 容器启动命令 |

---

## 四、构建和运行

### 4.1 构建镜像

```bash
docker build -t go-http-service:latest .
```

### 4.2 运行容器

```bash
docker run -d \
  --name go-http-service \
  -p 8080:8080 \
  go-http-service:latest
```

### 4.3 测试接口

```bash
curl http://localhost:8080/api/health
```

### 4.4 停止和删除

```bash
docker stop go-http-service
docker rm go-http-service
```

---

## 五、使用 Docker Compose

当项目依赖 PostgreSQL 等服务时，用 Docker Compose 更方便。

### 5.1 docker-compose.yml

```yaml
version: "3.8"

services:
  app:
    build: .
    container_name: go-http-service
    ports:
      - "8080:8080"
    environment:
      - DB_HOST=db
      - DB_PORT=5432
      - DB_USER=postgres
      - DB_PASSWORD=secret
      - DB_NAME=go_http_service
    depends_on:
      - db

  db:
    image: postgres:16
    container_name: go-http-service-db
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: secret
      POSTGRES_DB: go_http_service
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data

volumes:
  postgres_data:
```

### 5.2 一键启动

```bash
docker compose up -d
```

### 5.3 一键停止

```bash
docker compose down
```

---

## 六、不同环境的配置

### 6.1 环境变量

Docker 容器中通常通过环境变量传配置：

```go
port := os.Getenv("APP_PORT")
if port == "" {
    port = "8080"
}
```

### 6.2 .env 文件

开发时可以用 `.env` 文件：

```bash
APP_PORT=8080
DB_HOST=db
DB_PASSWORD=secret
JWT_SECRET=your-secret-key
```

docker-compose.yml 会自动读取 `.env` 文件中的变量。

---

## 七、容器化时的注意事项

| 事项 | 说明 |
|------|------|
| 不要提交数据库密码 | 用 `.env` 或 Docker secrets |
| 数据库连接用服务名 | 容器内通过 `db` 而不是 `localhost` 访问 PostgreSQL |
| 数据持久化 | 用 volumes 保存数据库数据 |
| 日志输出 | 容器中的日志应输出到 stdout |
| 健康检查 | Dockerfile 中可以用 `HEALTHCHECK` |

---

## 八、为当前项目准备的步骤

### 第一步：添加 Dockerfile

在 `go-http-service/` 根目录创建 `Dockerfile`。

### 第二步：添加 docker-compose.yml

创建 `docker-compose.yml`，包含 app 和 db 服务。

### 第三步：改代码支持环境变量

修改 `cmd/server/main.go` 中的端口和数据库连接字符串，支持从环境变量读取。

### 第四步：构建并运行

```bash
docker compose up --build -d
```

### 第五步：验证

```bash
curl http://localhost:8080/api/health
```

---

## 九、Docker 镜像优化

### 9.1 减小镜像体积

| 技巧 | 效果 |
|------|------|
| 多阶段构建 | 最终镜像只保留二进制 |
| 使用 alpine 镜像 | 基础镜像小 |
| 只 copy 必要文件 | 避免源码进入运行镜像 |

### 9.2 加速构建

| 技巧 | 效果 |
|------|------|
| 提前 `go mod download` | 依赖不变时不用重新下载 |
| 分层 COPY | 频繁改动的代码放在后面 copy |

---

## 十、什么时候容器化

建议在以下时机容器化：

- 项目功能基本可用，需要稳定运行时
- 需要和别人共享部署方式时
- 需要和其他服务（PostgreSQL、Redis）一起运行时
- 准备上云或 CI/CD 时

对于当前项目，Docker 容器化应该放在 **数据库连接 + 用户系统** 之后做，这样容器化的价值最大。
