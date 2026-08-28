# Go 常用框架对照

> 项目：`go-http-service`
> 主题：Gin 在 Go 框架版图里站哪一层、常见替代品各自解决什么、和本仓库怎么对上
> 日期：2026-08-28
> 相关：`notes/Guide.md`（总纲）、`notes/接入数据库前的地基.md`（为什么手工注入、不用 IoC）

---

## 0. 先分类，再谈「常用框架」

「常用 Go 框架」容易混成一锅。圈子里叫 framework 的东西，至少三层：

| 层 | 解决什么 | 例子 |
|----|----------|------|
| HTTP 路由 / 中间件 | 请求怎么进 handler | Gin、Echo、Chi、Fiber、Hertz |
| 全栈 / 脚手架 | 路由 + ORM + 配置 + 代码生成 | Beego、Buffalo、go-zero（HTTP 模式） |
| 微服务套件 | 服务发现、配置中心、RPC、治理 | Kratos、go-zero、go-kit、Kitex |

本项目用的 **Gin 只属于第一层**。它不管数据库、不管 IoC、不管服务注册。

和 Gin 相邻、但不是同一层的还有：

- **`net/http`**：Gin 没有替换掉它。本项目 `cmd/server/main.go` 用的仍是标准库 `http.Server`（四项超时、`Shutdown`），Gin 只是 `Handler`。
- **go-spring**：IoC 容器，模仿 Java Spring。它可以底下接 Gin。本项目没有用它，也没有自己实现一套——`main.go` 里大约 30 行手工装配就是全部。详见第 6 节。

---

## 1. 标准库 `net/http`

Go 1.22 起标准库已经有方法路由、路径参数（`GET /posts/{slug}`），生产上完全能用。很多公司内部服务就是 `net/http` + 自己写的一小层。

**有什么**

- `http.Server`：监听、四项超时、`Shutdown` 优雅关闭
- `ServeMux`：路由
- 中间件没有官方框架，约定是 `func(http.Handler) http.Handler`

**缺什么**

- 没有开箱的 JSON 绑定 / validator
- 没有现成的中间件链 API
- 错误处理、CORS、限流都得手写

**什么时候直接用它**

内部工具、代理、gRPC-gateway 旁边的健康检查、想零依赖。本项目一开始就是标准库 `/api/health`，接口变多、绑定和中间件开始重复时才换 Gin。

记住一句：**换路由框架不会换掉进程模型。** 超时、优雅关闭、TLS 仍是 `http.Server` 的事。

---

## 2. HTTP 框架（和本项目同一层）

### 2.1 Gin

- 仓库：`github.com/gin-gonic/gin`
- 定位：最快上手、国内用得最多的 HTTP 框架
- 核心：`gin.Engine` 路由树、`gin.Context`、中间件、`ShouldBindJSON` + validator 标签

**强项**

- 文档和中文资料最多，招聘里出现频率最高
- 绑定、校验、分组路由、中间件，API 很短
- 生态：jwt、cors、pprof 等大量 `gin-contrib`

**代价**

- `gin.Context` 贯穿一切，业务一不小心就会把 gin 泄漏进 service。本项目用接口把 gin 挡在 handler 外，就是在防这个
- 默认 debug 模式会往 stdout 打非 JSON 文本，和 slog 抢流。本项目因此在 `GIN_MODE` 未设时强制 `ReleaseMode`
- 性能很好，但不是「唯一快」。对个人博客这种体量，快慢不是选型理由

**适合**：REST JSON API、需要快速出活、团队已经会 Gin。本仓库就是这个形状。

### 2.2 Echo

- 仓库：`github.com/labstack/echo`
- 定位：和 Gin 几乎同一赛道，API 更「标准库一点」

| | Gin | Echo |
|--|-----|------|
| Context | `*gin.Context`，自有一套 | `echo.Context` 接口，更贴近 `http.Request` / `ResponseWriter` |
| 错误 | 多靠自己 `c.JSON` | 中央 `HTTPErrorHandler`，handler 可以 `return err` |
| 中间件 | `gin.HandlerFunc` | `func(next HandlerFunc) HandlerFunc` |
| 绑定 | 强，和 validator 绑得紧 | 也有，风格更散 |

Echo 的错误处理对「统一错误契约」更顺手：handler `return` 一个 error，外层翻译成 `ErrorResponse`。本项目在 Gin 里是每个 handler 自己调 `a.respondError`，也能做到，只是没有 Echo 那种天然的 return 链。

**适合**：想要 Gin 同级能力、但更喜欢 `return err` 和少一点魔法。从 Gin 迁到 Echo 成本中等（路由表能对上，Context 要全换）。

### 2.3 Chi

- 仓库：`github.com/go-chi/chi`
- 定位：**故意很薄**，中间件签名就是 `func(http.Handler) http.Handler`

**强项**

- 和标准库 100% 兼容：第三方 `net/http` 中间件拿来就能用
- 路由分组、`chi.URLParam`、`Mount` 子路由，够用且清晰
- 没有自己的 Context 类型，测试用 `httptest` 最省事

**缺什么**

- 没有内置绑定/校验（自己用 `encoding/json` + validator）
- 没有渲染引擎、没有「生态全家桶」

**适合**：想靠近标准库、中间件要可复用到别的服务、讨厌框架锁定。如果本项目重写路由层，Chi 会是最不违背现有风格的替代——分层、手工注入都不用动，只换 router。

### 2.4 Fiber

- 仓库：`github.com/gofiber/fiber`
- 定位：Express 风格，基于 `fasthttp`，宣传高性能

**必须知道的限制**

- **不是** `net/http`。`fasthttp` 的 Request/Response 是对象池，不能随便把指针逃到 goroutine 外，也不能直接套标准库中间件
- 和 `http.Server` 的超时、`Shutdown`、各种 `http.Handler` 生态是另一条河
- 性能优势在「超高 QPS 的纯 JSON」才明显；有数据库的服务，瓶颈在 SQL 和 bcrypt，不在路由

**适合**：明确要压测打满 CPU、且接受离开 `net/http` 生态。个人博客、带 bcrypt 登录的服务，换 Fiber 几乎零收益，还丢掉标准库兼容。本项目不该换它。

### 2.5 Hertz（CloudWeGo）

- 仓库：`github.com/cloudwego/hertz`
- 定位：字节内部 HTTP 框架开源，可切 `net/http` 或自研网络库，强调大流量

**特点**

- 中间件、绑定、路由看起来像 Gin/Echo 的亲戚
- 和 Kitex（同公司的 RPC）配套时，治理、观测、代码生成是一条链
- 文档和国内大厂实践多，国外社区小于 Gin

**适合**：已经在 CloudWeGo 技术栈（Kitex + Hertz），或公司统一用这一套。单独一个小服务上 Hertz，收益不如 Gin/Chi 的生态。

### 2.6 这一层怎么选（和本项目对齐）

```text
要标准库兼容、中间件可搬          → Chi
要绑定/校验少写、国内资料多        → Gin（当前选择，合理）
要 return err 的错误链            → Echo
要 Express 手感、接受 fasthttp    → Fiber（本项目不推荐）
要进字节/CloudWeGo 体系           → Hertz
```

对学习后端：**先把 Gin 用到本仓库这种深度**（中间件顺序、错误契约、不把框架泄漏进 service），比平行再学三个路由框架有用。路由框架 80% 能力是重复的。

---

## 3. 全栈 / 带脚手架的框架

### 3.1 Beego

老牌国内全栈：MVC、自带 ORM、配置、session、swagger 生成。心智接近 PHP Laravel / 早期 Java SSH：一个框架管到数据库。

**现在的位置**：仍有存量项目和教程，新项目选它的少。ORM 和分层理念和本项目「Handler → Service → Repository + pgx」不是一条路。

**适合**：看懂老项目、接国内存量；新项目一般不作为第一选择。

### 3.2 Buffalo

「Go 的 Rails」：生成器、前端管道、Pop ORM、热重载。英文社区向，做传统服务端渲染网站很顺。

本项目的公开 HTML 是 Go `html/template` embed，没有走 Buffalo。若目标是「多页应用 + 表单 + cookie session」，Buffalo 比 Gin 更像那条产品；但本项目已拒绝 cookie session（ADR 0008），所以对不上。

### 3.3 go-zero 的 HTTP 模式

`goctl` 从 API 描述生成 handler/logic/types，看起来像全栈脚手架，骨子里是微服务套件。见第 4.2 节。

---

## 4. 微服务套件（比 Gin 高一层）

这类框架假设你有 **多个进程、要 RPC、要服务发现、要统一治理**。本项目是单进程 + 直连 Postgres，用它们会像用航母运快递。

### 4.1 go-kit

- 仓库：`github.com/go-kit/kit`
- 定位：**不是**开箱框架，是一套设计模式库（endpoint / transport / service）

```text
Transport（HTTP/gRPC/AMQP）
    → Endpoint（一个函数：request in, response out）
        → Service（业务接口）
```

中间件是装饰 endpoint：日志、metrics、熔断、限流都可以叠。和本项目「handler 调 service」是同一精神，但 go-kit 把「传输层可替换」做到了极致。代价是样板代码多，一个 Hello World 能写好几个文件。

**适合**：真正要同一套业务同时暴露 HTTP 和 gRPC；愿意手写接线。不想要代码生成器。学习价值很高，和生产落地成本也高。

### 4.2 go-zero

- 仓库：`github.com/zeromicro/go-zero`
- 定位：国内很常见的微服务框架，**代码生成是核心体验**

**你得到什么**

- `.api` 文件描述路由 → `goctl` 生成 handler / logic / types
- 自带：限流、熔断、自适应负载、服务发现（etcd/k8s）、jwt、日志、prometheus
- 同时支持 HTTP 和 gRPC

**代价**

- 目录和命名被生成器锁死（`internal/logic` 等），和本项目的 `handler/service/repository` 不是同一套词
- 「业务写在 logic 里」很容易变成大函数，分层要靠自己纪律
- 学习曲线在 **goctl 和工作流**，不在 HTTP 本身

**适合**：从零做一套内部微服务、团队接受生成代码、要开箱治理。不适合：想精确控制每一层、已经有自己的错误契约和迁移器（硬套会打架）。

### 4.3 Kratos（Bilibili）

- 仓库：`github.com/go-kratos/kratos`
- 定位：B 站开源的微服务框架，模块化，protobuf 优先

**特点**

- 一个服务可以同时挂 HTTP 和 gRPC，协议用 protobuf 定义
- 布局清晰：`api/`（proto）、`internal/service`、`internal/biz`、`internal/data` —— 和本项目分层 **非常像**，只是多了 proto 和「biz」这个名字
- 中间件、配置、注册发现、链路追踪都有官方插件
- 用 Wire 做编译期依赖注入（比 go-spring 的运行时 IoC 更「Go」）

**适合**：准备上 gRPC、想要官方推荐的 `biz/data/service` 切法、接受 protobuf。从本项目迁过去，心智负担比 go-zero 小，因为分层哲学接近。

### 4.4 Kitex + Hertz（CloudWeGo）

- Kitex：高性能 RPC（Thrift/gRPC）
- Hertz：HTTP
- 配套：注册、配置、观测（OpenTelemetry）

**适合**：已经在字节系、或明确要 Kitex 的性能/协议。HTTP 小服务单独引入整套 CloudWeGo，偏重。

### 4.5 Micro / go-micro

历史上「微服务框架」的代名词，仓库和公司换过几次，社区分裂过。新项目要谨慎核对当前维护状态。学习价值不如 go-kit（模式）或 Kratos（国内可运行的完整套件）。

---

## 5. RPC / API 协议层（常被误叫成框架）

这些经常和上面叠在一起用：

| 工具 | 做什么 |
|------|--------|
| **gRPC** (`google.golang.org/grpc`) | protobuf + HTTP/2，服务间调用的默认选择 |
| **Connect** (`connectrpc.com/connect`) | gRPC 的现代化替代，一条 handler 同时讲 gRPC、gRPC-Web、JSON |
| **gqlgen** | GraphQL，从 schema 生成 Go |
| **grpc-gateway** | 同一份 proto 再暴露 REST |

本项目没有 RPC：浏览器和 curl 都是 JSON/HTML。等出现「第二个后端要调博客」时，才需要在「公开仍是 HTML、对内是否 gRPC」上做 ADR，而不是先换框架。

---

## 6. 本项目为什么不是 go-spring

Gin 和 go-spring 不是同一层：

| | Gin | go-spring | 本项目 |
|--|-----|-----------|--------|
| 是什么 | HTTP 框架 | 模仿 Java Spring 的 **IoC 容器 + 自动装配** | Gin + 自己写的分层 |
| 解决什么 | 请求怎么进 handler | 对象谁创建、谁注入谁 | 请求怎么进 handler，以及 **main 里怎么把库/service 接到 handler** |

go-spring 往往 **底下仍然用 Gin**。所以「用了 Gin、没用 go-spring」很正常。

本项目实际做的是 Go 里很常见、也小得多的一件事：**构造函数注入 + 消费方定义接口 + functional options**。

```go
// handler.New 用 Option 注入，测试可换掉任何依赖
api := handler.New(cfg, logger,
    handler.WithUserService(users),
    handler.WithAuthService(authSvc),
    handler.WithTokenVerifier(tokens),
    handler.WithPostService(posts),
    handler.WithReadyCheck("database", db.HealthCheck(pool, cfg.DBConnectTimeout)),
)
```

`main.go` 从里往外接线，全部写死、一眼能读完：

```text
pool
  → UserRepository / RefreshTokenRepository / PostRepository
    → UserService / AuthService / PostService
      → handler.API
        → Gin router
```

Service 不依赖 `*UserRepository`，只依赖自己包里的小接口（`userStore`、`refreshStore`）。测试塞一个假实现，不用数据库。

go-spring 会再往上堆：bean 容器、反射/标签注入、条件装配、配置绑定、生命周期回调。本项目一样都没有——没有容器、没有扫描、没有 `autowire`。少一个依赖，就在 `main` 里少写一行 `NewXxx`。

对现在这个体量（一个进程、三张表、一个 Author），手工装配大约 30 行，比引入 IoC 更便宜，也更符合「依赖方向写在类型上，不写在标签上」。

如果以后对象多到 `main` 接线开始疼，更「Go」的下一步是 **Wire**（编译期生成注入代码）或 **uber/fx**（基于反射的生命周期），而不是先上 go-spring。现在远没到。

「分层（Handler / Service / Repository）」也不是 go-spring 特有的。那是架构切法，Spring、Gin、甚至没框架的 `net/http` 都能这么切。go-spring 卖的是 **谁来 new 这些层**；本项目的答案是：`cmd/server/main.go` 里手写。

---

## 7. 和「框架」相邻、本项目用手写替代的部分

选型时人们常把这些也算进框架，本仓库是拆开自己做的：

| 能力 | 常见框架/库 | 本项目 |
|------|-------------|--------|
| 路由/绑定 | Gin | Gin |
| IoC | go-spring、Wire、fx | `main.go` 手工装配 |
| ORM | GORM、ent、sqlc、Beego ORM | pgx + 手写 SQL |
| 迁移 | golang-migrate、goose、GORM AutoMigrate | embed + advisory lock |
| 配置 | Viper、Kratos config | `internal/config` 读环境变量 |
| 日志 | zap、logrus、zerolog | `log/slog` |
| 鉴权 | gin-jwt、casbin | 自己的 JWT + refresh 轮转 |
| 限流 | 各种中间件、redis 令牌桶 | 内存令牌桶（每副本独立） |

GORM / ent / sqlc 值得单独认一下，因为「要不要上 ORM」比「Gin 还是 Echo」更能改变项目形状：

- **GORM**：活得快，钩子和关联多，SQL 不可见，复杂查询容易失控
- **ent**：schema 即代码，类型安全，生成量大
- **sqlc**：写 SQL → 生成类型安全的 Go，和本项目「SQL 在仓库层」最同路，是以后若厌倦手写 Scan 时最顺的升级

---

## 8. 按目标选

| 你的目标 | 更合理的选择 | 别先碰 |
|----------|--------------|--------|
| 学 HTTP 服务、分层、认证、部署 | **继续 Gin**（或以后试 Chi） | 微服务套件、Fiber、go-spring |
| 同一套业务要 HTTP + gRPC | Kratos 或 go-kit | 只换一个更快的路由器 |
| 公司统一 goctl、要熔断限流开箱 | go-zero | 把本项目硬改成 goctl 目录 |
| 字节/CloudWeGo 技术栈 | Hertz + Kitex | 单独给博客换 Hertz |
| Java 转 Go、想保留 Spring 手感 | go-spring 或 fx | 以为这是 Go 的默认 |
| 极致 QPS、无标准库包袱 | Fiber / Hertz 自研网络 | 有 bcrypt、有 SQL 时幻想路由能救命 |
| 服务端渲染网站 + 表单 | Buffalo / 标准库 template（本项目已是） | 为 HTML 再上一套 SPA 框架（ADR 0021 已划界） |

---

## 9. 和学习路线的关系

对本仓库，框架知识的优先级建议是：

1. **把 Gin 当 `http.Handler` 看懂**（`SetupRouter` 的中间件顺序、`gin.Context` 为什么不能进 service）
2. **读 Chi 的 README 一小时**，建立「没有框架 Context 也能活」的对照
3. **需要第二个服务或 gRPC 时再看 Kratos 的 `biz/data/service`**，和现在的分层几乎能一一对应
4. go-zero / Hertz / Fiber 等你真的进了那个技术栈再学，否则全是 API 记忆

路由框架换来换去，学到的是同一套：路由、中间件、绑定、错误。真正拉开差距的是本项目已经在练的那些：**依赖怎么注入、错误怎么分层、认证怎么 fail-closed、进程怎么关干净**。那些在 Gin、Echo、Chi 里都能落地，换框架不会自动获得。

---

## 10. 延伸阅读（本仓库内）

| 想继续看 | 去哪 |
|----------|------|
| 本项目为什么从包级函数改成 `*API` | `notes/接入数据库前的地基.md` |
| 中间件顺序为什么和 Gin 教程相反 | `notes/Guide.md` 第 5 节；`internal/handler/router.go` |
| 分层和错误方向 | `notes/分层架构.md`、`notes/接入 PostgreSQL 步骤B-注册接口与分层落地.md` |
| 装配发生在哪 | `cmd/server/main.go`、`internal/handler/api.go` |
