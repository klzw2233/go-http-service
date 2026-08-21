# Gin 路由与参数绑定练习

> 项目：`go-http-service`  
> 主题：通过 `GET /api/info` 和 `POST /api/echo` 练习 Gin 路由与参数绑定

---

## 一、练习目标

当前项目已经引入 Gin，但只做了一个简单的健康检查接口：

```go
r.GET("/api/health", healthHandler)
```

下一步通过增加两个接口，练习 Gin 的核心能力：

| 接口 | 方法 | 目的 |
|------|------|------|
| `/api/info` | GET | 返回服务信息，练习基础路由和 JSON 响应 |
| `/api/echo` | POST | 接收客户端数据并返回，练习请求参数绑定 |

这两个接口完成后，你对 Gin 的路由、参数解析、响应返回会有一个完整的认识。

---

## 二、GET /api/info 设计

### 功能

返回当前服务的元信息，比如：

- 服务名称
- 版本号
- 当前时间
- Go 版本

### 练习点

- 基础的 `r.GET()` 路由注册
- 使用结构体返回 JSON
- 使用 `c.JSON()` 发送响应

### 预期请求与响应

**请求**：

```bash
GET http://localhost:8080/api/info
```

**响应**：

```json
{
  "name": "go-http-service",
  "version": "0.2.0",
  "go_version": "go1.26.5",
  "timestamp": "2026-08-21T10:00:00Z"
}
```

### 示例代码

```go
type InfoResponse struct {
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	GoVersion string    `json:"go_version"`
	Timestamp time.Time `json:"timestamp"`
}

func infoHandler(c *gin.Context) {
	c.JSON(http.StatusOK, InfoResponse{
		Name:      "go-http-service",
		Version:   "0.2.0",
		GoVersion: runtime.Version(),
		Timestamp: time.Now().UTC(),
	})
}
```

注册路由：

```go
r.GET("/api/info", infoHandler)
```

---

## 三、POST /api/echo 设计

### 功能

客户端发送一段数据，服务端原样返回，证明能正确接收和解析请求体。

### 练习点

- `r.POST()` 路由注册
- JSON Body 参数绑定：`c.ShouldBindJSON()`
- 结构体 tag：`json:"message"`
- 错误处理：请求格式错误时返回 400

### 预期请求与响应

**请求**：

```bash
POST http://localhost:8080/api/echo
Content-Type: application/json

{
  "message": "hello gin"
}
```

**响应**：

```json
{
  "message": "hello gin",
  "echoed_at": "2026-08-21T10:00:00Z"
}
```

### 示例代码

```go
type EchoRequest struct {
	Message string `json:"message" binding:"required"`
}

type EchoResponse struct {
	Message  string    `json:"message"`
	EchoedAt time.Time `json:"echoed_at"`
}

func echoHandler(c *gin.Context) {
	var req EchoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, EchoResponse{
		Message:  req.Message,
		EchoedAt: time.Now().UTC(),
	})
}
```

注册路由：

```go
r.POST("/api/echo", echoHandler)
```

---

## 四、Gin 参数绑定详解

Gin 的「参数绑定」指的是：**自动把 HTTP 请求中的数据解析到 Go 结构体中**。

不同类型的请求参数，Gin 提供了不同的绑定方式。

---

## 五、URL Path 参数

URL Path 参数是 URL 路径中的一部分。

```text
GET /api/users/123
```

这里 `123` 是用户 ID，就是 URL Path 参数。

### Gin 写法

```go
r.GET("/api/users/:id", func(c *gin.Context) {
    id := c.Param("id")
    c.JSON(http.StatusOK, gin.H{"user_id": id})
})
```

### 练习请求

```bash
curl http://localhost:8080/api/users/123
```

**响应**：

```json
{"user_id":"123"}
```

---

## 六、Query 参数

Query 参数是 URL `?` 后面的键值对。

```text
GET /api/search?q=gin&lang=go
```

### Gin 写法

```go
r.GET("/api/search", func(c *gin.Context) {
    q    := c.Query("q")
    lang := c.DefaultQuery("lang", "go")

    c.JSON(http.StatusOK, gin.H{
        "q":    q,
        "lang": lang,
    })
})
```

### 练习请求

```bash
curl "http://localhost:8080/api/search?q=gin&lang=go"
```

**响应**：

```json
{"q":"gin","lang":"go"}
```

---

## 七、JSON Body 参数

JSON Body 参数最常见，用于 `POST`、`PUT`、`PATCH` 请求。

```bash
POST /api/echo
Content-Type: application/json

{"message":"hello"}
```

### Gin 写法

```go
type EchoRequest struct {
    Message string `json:"message"`
}

r.POST("/api/echo", func(c *gin.Context) {
    var req EchoRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": req.Message})
})
```

### 练习请求

```bash
curl -X POST http://localhost:8080/api/echo \
  -H "Content-Type: application/json" \
  -d '{"message":"hello gin"}'
```

---

## 八、Form 参数

Form 参数通常用于网页表单提交。

```bash
POST /api/form
Content-Type: application/x-www-form-urlencoded

name=gin&age=10
```

### Gin 写法

```go
type FormRequest struct {
    Name string `form:"name"`
    Age  int    `form:"age"`
}

r.POST("/api/form", func(c *gin.Context) {
    var req FormRequest
    if err := c.ShouldBind(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    c.JSON(http.StatusOK, req)
})
```

### 练习请求

```bash
curl -X POST http://localhost:8080/api/form \
  -d "name=gin&age=10"
```

---

## 九、Header 参数

Header 参数从 HTTP Header 中读取。

```go
type HeaderRequest struct {
    Token string `header:"X-Token"`
}

r.GET("/api/header", func(c *gin.Context) {
    var req HeaderRequest
    c.ShouldBindHeader(&req)

    c.JSON(http.StatusOK, gin.H{"token": req.Token})
})
```

### 练习请求

```bash
curl -H "X-Token: secret123" http://localhost:8080/api/header
```

---

## 十、参数验证（validator）

Gin 内置了 `validator`，可以在结构体 tag 中加 `binding` 规则。

### 常用验证规则

| 规则 | 含义 | 示例 |
|------|------|------|
| `required` | 必填 | `binding:"required"` |
| `min=5` | 最小长度 5 | `binding:"min=5"` |
| `max=100` | 最大长度 100 | `binding:"max=100"` |
| `email` | 必须是邮箱 | `binding:"email"` |
| `gte=0` | 大于等于 0 | `binding:"gte=0"` |

### 示例

```go
type UserRequest struct {
    Name  string `json:"name" binding:"required,min=2,max=50"`
    Email string `json:"email" binding:"required,email"`
    Age   int    `json:"age" binding:"gte=0,lte=150"`
}
```

如果请求不满足规则，`c.ShouldBindJSON()` 会返回错误。

---

## 十一、建议的练习顺序

1. 先实现 `GET /api/info`
2. 再实现 `POST /api/echo`
3. 尝试加入 `GET /api/users/:id` 练习 Path 参数
4. 尝试 `GET /api/search?q=xxx` 练习 Query 参数
5. 尝试 `POST /api/form` 练习 Form 参数
6. 最后加入 validator 验证规则

---

## 十二、下一步扩展

完成这些接口练习后，可以继续：

1. **分层架构**：把 handler 拆到 `internal/handler/`
2. **数据库连接**：用 PostgreSQL 保存用户、文章等数据
3. **日志和配置模块**：让项目更工程化
4. **Docker 容器化**：把项目打包成镜像

> 核心原则：每个新功能都要实际落在这个项目上，不要只停留在理论。

