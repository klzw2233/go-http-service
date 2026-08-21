# 在现有 HomeLab 框架上学习后端开发

> 环境：Win10 + VMware Ubuntu（NAT8，固定 IP 192.168.170.128）+ Docker + nginx HTTPS 反向代理 + 子域名
> 主题：如何在这样的环境中继续后端开发，使其贴近生产环境

---

## 一、当前环境的优势

这个环境已经接近小型生产系统：

```text
Win10 浏览器
    ↓
https://api.ubuntu.test
    ↓
nginx 容器（HTTPS 终止、路由）
    ↓
Go 服务 / PostgreSQL / 其他服务
```

关键组件都已经具备：

| 组件 | 作用 |
|------|------|
| VMware Ubuntu | Linux 服务器环境 |
| Docker | 容器化运行服务 |
| nginx | HTTPS 反向代理、子域名路由 |
| hosts 本地域名 | 模拟真实域名解析 |
| 子域名 | 区分不同服务 |

这不是玩具环境，而是真实家庭服务器 / 自托管系统的标准结构。

---

## 二、推荐的目标架构

把 Go 后端服务作为标准容器接入现有体系：

```text
用户浏览器
    ↓
subdomain.ubuntu.test
    ↓
nginx 容器（Docker）
    ↓
    ├─ api.ubuntu.test    → go-http-service 容器
    ├─ app.ubuntu.test    → Vue 前端容器（未来）
    ├─ portainer.ubuntu.test → Portainer（已有）
    └─ grafana.ubuntu.test   → 监控（未来）
```

关键原则：

- Go 后端运行在 Ubuntu Docker 中
- PostgreSQL 也运行在 Docker 中
- nginx 统一做 HTTPS 入口
- 服务间通过 Docker 容器名通信
- 域名用子域名区分服务

---

## 三、三种 Go 服务运行方式对比

### 方案 A：Go 服务作为 Docker 容器运行（最推荐）

```text
Docker on Ubuntu
├── nginx
├── go-http-service
└── postgres
```

**优点**：最生产化，一键部署，网络隔离好，可原样搬到云服务器  
**缺点**：开发时每次改代码要重新构建镜像  
**优化**：用 `docker compose watch` 或 volume 挂载实现热重载

### 方案 B：Go 服务直接运行在 Ubuntu 上（适合调试）

```text
Ubuntu
└── go run cmd/server/main.go

Docker
├── nginx
└── postgres
```

**优点**：修改代码立即生效，调试方便  
**缺点**：和最终部署方式不一致  
**适用**：短期调试阶段

### 方案 C：Go 服务运行在 Win10 上（不推荐）

**缺点**：
- 和最终部署方式脱节
- Win10 访问 Ubuntu Docker 中数据库网络复杂
- 不够生产化

---

## 四、服务间通信方式

### Docker 网络

创建统一 Docker 网络：

```bash
docker network create goapp-network
```

### 容器间通信

| 源 | 目标 | 连接方式 |
|----|------|----------|
| nginx 容器 | go-http-service 容器 | `http://go-http-service:8080` |
| go 容器 | postgres 容器 | `postgres://goapp:password@postgres:5432/...` |

容器之间用**容器名**作为 hostname，这是 Docker 服务发现的常见做法。

### nginx 反向代理示例

```nginx
server {
    listen 443 ssl http2;
    server_name api.ubuntu.test;

    ssl_certificate /etc/nginx/ssl/ubuntu.test.crt;
    ssl_certificate_key /etc/nginx/ssl/ubuntu.test.key;

    location / {
        proxy_pass http://go-http-service:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

---

## 五、配置管理生产化

### 环境变量

```bash
DB_HOST=postgres
DB_PORT=5432
DB_USER=goapp
DB_PASSWORD=your_password
DB_NAME=go_http_service
JWT_SECRET=your_secret_key
APP_PORT=8080
```

### docker-compose.yml

```yaml
services:
  app:
    build: .
    environment:
      - DB_HOST=${DB_HOST}
      - DB_PASSWORD=${DB_PASSWORD}
      - JWT_SECRET=${JWT_SECRET}
    networks:
      - goapp-network
```

这就是工作中标准的配置管理方式。

---

## 六、子域名规划建议

| 子域名 | 服务 | 状态 |
|--------|------|------|
| `api.ubuntu.test` | Go 后端 API | 新增 |
| `app.ubuntu.test` | Vue/React 前端 | 未来 |
| `portainer.ubuntu.test` | Portainer | 已有 |
| `pgadmin.ubuntu.test` | pgAdmin | 可选 |
| `grafana.ubuntu.test` | Grafana 监控 | 未来 |

---

## 七、生产化能力清单

| 生产化能力 | 当前环境实现方式 |
|------------|------------------|
| HTTPS | mkcert + nginx |
| 反向代理 | nginx |
| 域名路由 | 子域名 + hosts |
| 环境隔离 | 不同 `.env` 文件 |
| 日志查看 | `docker logs` |
| 健康检查 | `/api/health` + Docker healthcheck |
| 优雅关闭 | Go 信号监听 |
| 数据库迁移 | golang-migrate / goose |
| CI/CD | GitHub Actions |
| 监控 | Prometheus + Grafana |

---

## 八、为什么这个学习方式更有效

这个环境模拟了真实互联网系统的流量入口结构：

```text
用户浏览器 → HTTPS 域名 → 反向代理 → 业务服务 → 数据库
```

学习的不仅是 Go 编程，还包括：

- 反向代理配置
- HTTPS 终止
- 容器间网络
- 服务发现
- 域名与 DNS
- 日志和监控
- 环境变量与配置管理

这些都是 Backend Infrastructure Engineer / Platform Engineer 的核心技能。

---

## 九、推荐实施顺序

1. 添加 Dockerfile
2. 添加 docker-compose.yml（Go 服务 + PostgreSQL）
3. 通过 `api.ubuntu.test` 访问 Go 服务
4. 实现 PostgreSQL 用户注册/登录
5. 完善子域名 nginx 路由规则
6. 添加 GitHub Actions CI/CD

---

## 十、总结

> 当前的 VMware + Ubuntu + Docker + nginx + HTTPS + 子域名环境，非常适合学习后端开发。它不是玩具环境，而是小型生产系统的缩影。继续在这个框架上开发，能同时学习 Go 后端、基础设施、网络、部署、安全，对 Backend Infrastructure Engineer 方向非常有价值。
