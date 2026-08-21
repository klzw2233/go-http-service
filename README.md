# Go HTTP Service

> Backend Engineer 学习路线的第一个实战项目：从零搭建一个最小化的 Go HTTP 服务。

---

## 项目目标

建立一个可用于后续项目演进的 Go 后端服务基础骨架：

- 使用 Go 标准库 `net/http` 启动 HTTP 服务
- 提供健康检查接口
- 为后续添加 REST API、数据库、中间件等能力打下基础

---

## 项目结构

```
go-http-service/
├── cmd/
│   └── server/
│       └── main.go          # 服务入口
├── go.mod                   # Go 模块定义
└── README.md                # 项目说明
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

打开浏览器或使用 curl：

```bash
curl http://localhost:8080/api/health
```

预期返回：

```json
{"status":"ok"}
```

---

## 端口说明

- 服务监听端口：**8080**
- 健康检查路径：**GET /api/health**

---

## 下一步计划

1. 添加更多基础 API（如 GET /api/info）
2. 引入路由框架（Gin / chi / Echo）
3. 添加中间件（日志、跨域、恢复）
4. 连接 PostgreSQL 数据库
5. 实现用户注册/登录接口
6. 使用 Docker 容器化部署
