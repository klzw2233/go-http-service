# 连接 PostgreSQL 数据库

> 项目：`go-http-service`  
> 主题：如何在 Go 项目中连接 PostgreSQL 数据库并实现数据持久化

---

## 一、为什么要连接数据库

当前项目的数据都是临时生成的，比如：

- `/api/health` 返回当前时间戳
- `/api/info` 返回版本信息
- `/api/echo` 把请求数据原样返回

这些数据有一个共同点：**不保存**。程序一关闭，所有数据都消失。

如果要做用户系统、博客系统、下载管理器，就必须把数据持久化保存到数据库中。

---

## 二、为什么选择 PostgreSQL

PostgreSQL 是一个开源的关系型数据库，特点：

| 特点 | 说明 |
|------|------|
| 功能强大 | 支持复杂查询、事务、视图、索引 |
| 开源免费 | 企业和个人都可用 |
| 标准 SQL | 语法通用，容易迁移到其他关系型数据库 |
| 生态成熟 | Go 驱动完善，文档丰富 |
| 与当前环境匹配 | 你之前已经接触过 PostgreSQL |

对于后端项目来说，PostgreSQL 是最佳首选之一。

---

## 三、数据库在项目中的位置

连接 PostgreSQL 后，项目架构变成：

```text
HTTP Handler
      ↓
   Service
      ↓
 Repository
      ↓
 PostgreSQL
      ↓
   硬盘
```

| 层级 | 作用 |
|------|------|
| Handler | 接收请求，解析参数 |
| Service | 处理业务规则 |
| Repository | 把 Go 结构体转换成 SQL 语句，与数据库交互 |
| PostgreSQL | 持久化存储数据 |

---

## 四、需要学习的核心概念

### 4.1 Go 数据库驱动

Go 标准库 `database/sql` 提供了统一的数据库接口，但需要安装对应数据库的驱动。

PostgreSQL 常用驱动：

| 驱动 | 特点 |
|------|------|
| `lib/pq` | 老牌驱动，但现在维护较少 |
| `pgx` | 性能更好，功能更强，推荐优先使用 |

安装命令：

```bash
go get github.com/jackc/pgx/v5
```

### 4.2 基本 CRUD 操作

| 操作 | 对应 API |
|------|----------|
| 查询 | `db.Query()` / `db.QueryRow()` |
| 插入 | `db.Exec()` + `INSERT INTO` |
| 更新 | `db.Exec()` + `UPDATE` |
| 删除 | `db.Exec()` + `DELETE` |

### 4.3 连接池

Go 的 `sql.DB` 内置连接池，会复用数据库连接，不需要每次请求都新建连接。

```go
db, err := sql.Open("pgx", "postgres://user:password@localhost:5432/dbname")
if err != nil {
    log.Fatal(err)
}
defer db.Close()
```

### 4.4 数据库迁移（Migration）

项目开发过程中，表结构会不断变化。迁移工具可以管理这些变化。

常用工具：

| 工具 | 特点 |
|------|------|
| `golang-migrate/migrate` | 最常用，Go 项目首选 |
| `pressly/goose` | 轻量级，语法简单 |

迁移文件示例：

```sql
-- +migrate Up
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(50) NOT NULL UNIQUE,
    email VARCHAR(100) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- +migrate Down
DROP TABLE users;
```

---

## 五、应用到当前项目

### 第一步：调整项目结构

新增 `internal/repository/` 和 `internal/service/`：

```
go-http-service/
├── cmd/server/
│   └── main.go
├── internal/
│   ├── handler/
│   ├── service/
│   │   └── user_service.go
│   ├── repository/
│   │   └── user_repository.go
│   ├── model/
│   │   ├── health.go
│   │   ├── echo.go
│   │   └── user.go
│   └── db/
│       └── db.go              # 数据库连接初始化
```

### 第二步：创建数据库连接

```go
package db

import (
    "database/sql"
    _ "github.com/jackc/pgx/v5/stdlib"
)

func NewConnection(connString string) (*sql.DB, error) {
    db, err := sql.Open("pgx", connString)
    if err != nil {
        return nil, err
    }

    if err := db.Ping(); err != nil {
        return nil, err
    }

    return db, nil
}
```

### 第三步：定义用户模型

```go
package model

type User struct {
    ID           int64  `json:"id"`
    Username     string `json:"username"`
    Email        string `json:"email"`
    PasswordHash string `json:"-"`
    CreatedAt    string `json:"created_at"`
}
```

### 第四步：创建 Repository

```go
package repository

import (
    "database/sql"
    "go-http-service/internal/model"
)

type UserRepository struct {
    db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
    return &UserRepository{db: db}
}

func (r *UserRepository) Create(user *model.User) error {
    query := `
        INSERT INTO users (username, email, password_hash)
        VALUES ($1, $2, $3)
        RETURNING id, created_at
    `
    return r.db.QueryRow(query, user.Username, user.Email, user.PasswordHash).
        Scan(&user.ID, &user.CreatedAt)
}
```

### 第五步：创建 Service

```go
package service

import (
    "go-http-service/internal/model"
    "go-http-service/internal/repository"
)

type UserService struct {
    repo *repository.UserRepository
}

func NewUserService(repo *repository.UserRepository) *UserService {
    return &UserService{repo: repo}
}

func (s *UserService) Register(user *model.User) error {
    return s.repo.Create(user)
}
```

### 第六步：创建 Handler

```go
package handler

import (
    "net/http"
    "github.com/gin-gonic/gin"
)

func (h *Handler) RegisterUser(c *gin.Context) {
    var req struct {
        Username string `json:"username" binding:"required"`
        Email    string `json:"email" binding:"required,email"`
        Password string `json:"password" binding:"required,min=6"`
    }

    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": "user registered"})
}
```

---

## 六、数据库配置

建议把数据库连接信息放到环境变量或配置文件中：

```bash
export DB_HOST=localhost
export DB_PORT=5432
export DB_USER=postgres
export DB_PASSWORD=secret
export DB_NAME=go_http_service
```

连接字符串：

```
postgres://postgres:secret@localhost:5432/go_http_service?sslmode=disable
```

---

## 七、开发环境 PostgreSQL

### 7.1 用 Docker 启动 PostgreSQL

```bash
docker run -d \
  --name go-http-service-db \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=secret \
  -e POSTGRES_DB=go_http_service \
  -p 5432:5432 \
  postgres:16
```

### 7.2 用 Docker Compose 管理

后续可以把 PostgreSQL 直接加入到项目的 `docker-compose.yml` 中。

---

## 八、常见坑

| 坑 | 原因 | 解决方案 |
|----|------|----------|
| 连接后立即查询失败 | `sql.Open` 不验证连接，需要 `db.Ping()` | 启动时调用 `db.Ping()` |
| 忘记关闭连接 | `defer db.Close()` 写错位置 | 在初始化成功后才 `defer` |
| SQL 注入 | 拼接 SQL 字符串 | 使用参数化查询 `$1, $2` |
| 空指针 | 查询无结果时返回 `sql.ErrNoRows` | 显式判断错误类型 |
| 连接泄漏 | 没有关闭 `Rows` | 使用 `defer rows.Close()` |

---

## 九、什么时候引入 PostgreSQL

建议在以下时机引入：

- 需要持久化数据时（如用户注册、文章存储）
- 做博客系统、用户系统之前
- 学习完分层架构和测试用例之后

对于当前项目，下一步就是引入 PostgreSQL，然后实现用户表和文章表。
