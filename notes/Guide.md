# Go HTTP Service 学习总纲

> 项目：`go-http-service`
> 用途：一份能从头读到尾的地图。细节在各篇专题笔记和 `docs/adr/` 里，这里只讲「整张图长什么样、为什么这样长、下一步该看哪篇」。
> 日期：2026-08-28
> 对应代码：当前 `main` 上的单作者公开博客（含 Author 区、站点样式、认证后续 ADR 0016–0021）

---

## 0. 怎么用这份总纲

这不是接口手册，也不是把现有笔记再抄一遍。读它是为了三件事：

1. **定位**：打开一个文件时，知道它属于哪一层、不该知道什么。
2. **顺序**：这个项目是按「地基 → 垂直切片 → 产品」长出来的，跳着读会觉得规矩突兀。
3. **取舍**：很多「看起来可以更简单」的写法，是故意不那么写。总纲把理由钉在一张表上，细节再下钻。

配套材料：

| 想查什么 | 去哪 |
|----------|------|
| 领域用词（Author / Post / Draft / Slug…） | 根目录 `CONTEXT.md` |
| 一条产品决策为什么这样 | `docs/adr/` |
| 某一步是怎么做出来的 | 下文「笔记索引」里的专题笔记 |
| 怎么跑、环境变量、错误码 | `README.md` |
| 给 Claude / 自己的开发约定 | `CLAUDE.md` |

早期笔记（`Go-HTTP-Service-架构讲解.md`、`分层架构.md`、`Go-HTTP-Service-后续扩展方向.md`）写于「只有 `/api/health`」的阶段，**结构思想仍对，文件树和接口列表已经过时**。以本总纲和 `README.md` 为准。

---

## 1. 这是什么项目

一句话：一个 **单进程 Go HTTP 服务**，对外同时提供

- 访客可读的 **公开 HTML 博客**（`GET /`、`GET /posts/{slug}`）
- Author 用来写稿的 **JSON API**（`/api/auth/*`、`/api/posts*`）
- Author 区的 **HTML 壳**（`/author/*`，首屏不含 Draft Body，凭据在浏览器 JS 里）

产品定义见 ADR 0001：一个人写，任何人不用登录就能读已发布的 Post。不是多作者 CMS，也不是私密笔记本。

它同时是 Backend Engineer 学习路线的第一个实战项目：从「一个 health 接口」长成「带数据库、认证、限流、容器、CI 的个人博客」。学习价值不在框架用法，而在 **每一层为什么这样切、每一个安全默认为什么 fail-closed**。

```text
访客浏览器                    Author 浏览器                 curl / 脚本
    │                              │                            │
    │  GET /  GET /posts/{slug}    │  GET /author/*             │  POST /api/auth/login
    │  GET /site.css  /theme.js    │  sessionStorage + Bearer   │  自己的 refresh，禁止共用
    ▼                              ▼                            ▼
┌─────────────────────────────────────────────────────────────────┐
│                     一个 Go 进程（Gin）                          │
│  公开 HTML  ←── 同一套中间件 ──→  JSON 写 API  ←── Author 壳    │
└──────────────────────────────┬──────────────────────────────────┘
                               │ pgxpool
                               ▼
                         PostgreSQL
                    users / refresh_tokens / posts
```

三个「不要搞混」：

- **公开读是 HTML，不是 JSON。** 未认证的 `GET /api/posts` 是 401，不是公开 feed（ADR 0007）。
- **Author 区不是 cookie session。** 登录仍走 `POST /api/auth/login`，token 放在 `sessionStorage`（ADR 0008、0016）。
- **HTML 和 API 在同一个进程里。** 将来若做独立 Author SPA，只配 `CORS_ALLOWED_ORIGINS`，公开页仍由 Go 渲染（ADR 0002、0021）。

---

## 2. 它是怎么长出来的（学习顺序）

真实顺序比「先学框架再学数据库」更有用：先把 **接库之前改不动的地基** 打好，再做一条穿到 SQL 的垂直切片，再把认证和限流绑在一起上，最后才是产品（博客）。

```text
① 最小 HTTP 服务          /api/health，标准库 → Gin
② 代码审查与加固          统一错误契约、超时、优雅关闭、CI
③ 接库前的地基            依赖注入、config、slog、Request ID、双探针
④ PostgreSQL 步骤 A       连接池 + 就绪探针（一张表都没有）
⑤ PostgreSQL 步骤 B       迁移 + users + POST /api/users（分层真正落地）
⑥ PostgreSQL 步骤 C       登录 + JWT + refresh 轮转 + 限流（三件必须一起上）
⑦ 步骤 D                  CORS fail-closed + 安全响应头
⑧ 步骤 E                  多阶段 distroless + compose
⑨ 博客                    关公开注册 → Draft JSON → Publish/HTML → Author 区 → 站点样式
⑩ 认证后续（已决定未写）   login_failed 日志、撤全部 refresh；公网上线前再做 TOTP
```

为什么步骤 A 不写业务：地基（`WithReadyCheck`、`defer pool.Close()`、`c.Request.Context()`）当时只被测试验证过。若直接上注册，凭据泄露和「注册为什么 500」会搅在一起。步骤 A 单独暴露了 2 个凭据进日志的洞。

为什么限流和登录同一天：没有限流的登录接口就是在线爆破靶子。中间态不允许上线。

对应专题笔记见第 14 节。产品决策不在笔记里长篇论证，而在 ADR。

---

## 3. 代码地图

```text
cmd/server/main.go          只做装配：读配置、连库、迁移、接线、监听信号、优雅关闭
internal/config/            全部环境变量，启动时一次读完；非法值拒绝启动
internal/auth/              JWT 签发解析；refresh 生成与 SHA-256。不碰库、不碰 HTTP
internal/db/                pgxpool、就绪探针、手写迁移器（embed + advisory lock）
  migrations/*.sql          按文件名顺序执行；已提交的文件禁止改
internal/repository/        Go 结构体 ↔ SQL；翻译 23505；事务不露出这一层
internal/service/           业务规则：bcrypt、slug 形状、登录计时拉平、错误翻译
internal/handler/           HTTP：路由、中间件、JSON、HTML 模板、静态资源 embed
internal/markdown/          安全 CommonMark（goldmark + scheme 白名单）
internal/model/             请求/响应/错误码；带密码哈希的结构体禁止直接 JSON 出去
```

`cmd/` 是可执行入口，`internal/` 是对外不可 import 的实现。新功能的落点几乎总是：

1. 需要配置？`internal/config` 加字段，`LogValue()` 里决定是否脱敏。
2. 需要表？`internal/db/migrations/000N_*.sql`，**新文件**，不改旧文件。
3. 需要规则？`service`；需要 SQL？`repository`；需要 HTTP？`handler` 的 `*API` 方法。

Handler 一律是 `func (a *API) Xxx(c *gin.Context)`，依赖是 `API` 的字段，在 `handler.New()` 用 Option 注入，`main` 装配。不要新增包级变量，不要在 handler 里写 SQL。

---

## 4. 分层：依赖单向，错误也单向

```text
Handler  ──知道──→  gin、HTTP 状态码、model 的请求/响应
    │               不知道 SQL
    ▼
Service  ──知道──→  业务规则、bcrypt、自己的 error
    │               不知道 gin、不知道 SQL 文本
    ▼
Repository ──知道──→  pgx、约束名、SQLSTATE
                    不知道 HTTP、不知道「什么叫 Author」
```

Service 依赖的是 **接口**（`userStore`、`refreshStore`），不是 `*UserRepository`。所以 service 测试不需要数据库。

错误值遵守同一方向，看起来像重复，那正是分层的代价：

| 层 | 定义 | 谁匹配 |
|----|------|--------|
| Repository | `ErrUsernameTaken`（由约束名 `users_username_key` 翻译 23505） | Service |
| Service | 自己的 `ErrUsernameTaken`（翻译 repository 的） | Handler |
| Handler | 映射成 `409` + `CONFLICT` | 客户端 |

Handler **不得** `import repository`。改了迁移里的索引名却忘了改 repository 常量，精确的 409 会退化成 500——这是刻意把约束名写进代码的原因。

唯一性靠数据库约束，不要先查后插（TOCTOU）。密码长度在 service 按 **字节** `len()` 校验：bcrypt 只用前 72 字节，binding tag 按字符计，挡不住中文密码。

时间一律 `a.now()`，不要 `time.Now()`，否则测试无法钉死时间戳。

---

## 5. 一次请求怎么走

### 5.1 中间件顺序（改错会静默记错日志）

`router.go` 的顺序是有意的，和「Gin 教程把 Recovery 放最外层」相反：

```text
requestID        最先，后面所有日志才带得上 ID
requestLogger    紧随其后，能量到完整耗时；必须在 Recovery 外面
CustomRecovery   在 logger 内层，先把 panic 变成 500，logger 才能记到正确状态码
timeout          只把 deadline 放进 context，自己不写响应
limitBodySize    请求体上限
SecurityHeaders  在 c.Next() 之前写头，所以 500/404/超时也带得上
CORS             最后；fail-closed，未配置不加任何 Access-Control-*
```

若 Recovery 在最外层：handler panic → 异常穿过 logger 的 `c.Next()` → 此时状态码还不是 500 → 访问日志撒谎。

`timeout` **只传播 deadline，不代替 handler 写响应**。抢先写会和 handler 争 `ResponseWriter`，是数据竞争。因此所有可能阻塞的调用必须接 `c.Request.Context()`：

```go
// 对
rows, err := pool.Query(c.Request.Context(), sql, args...)

// 错——超时管不到它
rows, err := pool.Query(context.Background(), sql, args...)
```

### 5.2 路由分三组

| 组 | 路径 | 限流 | 认证 |
|----|------|------|------|
| 探针 | `/api/health`、`/api/ready` | 无（编排器用，不能被自己打满） | 无。health 不查依赖；ready 并发跑检查 |
| 业务 JSON | `/api/info`、`/echo`、`/users`、`/posts*` | 全局桶 | posts / users 要 Bearer + Author |
| 认证 | `/api/auth/login|refresh|logout` | **更严的登录桶** | login 无；me 要 Bearer |
| 公开 HTML | `/`、`/posts/{slug}`、`/site.css`、`/theme.js` | 无（在限流组外） | 无。Draft 与未知 slug 同一 HTML 404 |
| Author 壳 | `/author/*` | 无 | GET 本身无 Bearer；Draft Body 只走 JSON |

`POST /api/users` 的中间件是 `forbidAnonymous` → `requireAuth` → `requireAuthor`：没带 token 是 **403**（权限拒绝，不是请去登录），带了坏 token 仍是 401。

### 5.3 错误响应只有一种形状

```json
{"code":"VALIDATION_FAILED","message":"...","fields":[{"field":"title","reason":"..."}]}
```

`code` 给程序 switch，`message` 给人看、可以改措辞。**绝不要把原始 `error` 返回给客户端**：validator 和 `encoding/json` 会带出 `EchoRequest` 这种内部类型名。原始错误只写日志，翻译在 `internal/handler/errors.go`。`/api/ready` 失败原因同样只给通用字，数据库驱动错误里可能有主机名甚至密码。

成功响应用专门的 `XxxResponse`，不要直接 `c.JSON(user)` / `c.JSON(post)`。`model.User` / `model.Post` 故意不打 json 标签，误用时字段名会很难看，而不是干净地泄出去。

---

## 6. 启动、关闭、配置

`main` 几乎不做业务，只接线。顺序本身就是规矩：

```text
config.Load()          坏值直接退出，不带默认值「先跑起来」
newLogger              之后所有日志走 slog；gin 默认 ReleaseMode，避免 debug 文本打进 JSON 流
signal.NotifyContext   SIGINT / SIGTERM（Docker/K8s 先发 SIGTERM）
db.Connect             DATABASE_URL 必需
defer pool.Close()     注册在 srv 之前 → 实际执行是「先排空 HTTP，再关池」
db.Migrate             接流量之前跑完；多副本用 advisory lock 串行
EnsureAuthor           仅当 DEV_AUTHOR_PASSWORD 非空，且库里还没有这个用户
装配 repo → service → API
ListenAndServe         四项 http.Server 超时 + 单请求 REQUEST_TIMEOUT
Shutdown               默认 15s；排空期间再 Ctrl-C 立即退出
```

`defer pool.Close()` 必须在构造 `srv` **之前**注册：Go 的 defer 是 LIFO，而 `Shutdown` 是普通调用。先关池会把还在跑的查询砍断。`cmd/server/e2e_test.go` 守着这个顺序。

配置集中在 `internal/config`，别处不要 `os.Getenv`。`config.LogValue()` 每次启动都会把配置打进日志，所以：

- `DATABASE_URL` → `redactDSN()`
- `JWT_SECRET`、`DEV_AUTHOR_PASSWORD` → `redactSecret()`
- `AUTHOR_USERNAME` 不是密钥，原样写出

`internal/db` 的 `ErrInvalidDSN` 不包装 pgx 原错误，因为原错误带连接串，而这个 error 会被 `main` 记日志。

常用必需项：`DATABASE_URL`、`JWT_SECRET`（≥32 字节）、`AUTHOR_USERNAME`。本地要打开 `/author/login` 再加 `DEV_AUTHOR_PASSWORD`（空库插 Author，已存在不改密；生产不要设）。

`TRUSTED_PROXIES` 默认空：谁都不信，`c.ClientIP()` 取 TCP 对端。只有前面真有反向代理时才填，否则客户端能用 `X-Forwarded-For` 伪造 IP，进而打穿按 IP 限流。

---

## 7. 认证：两枚 token，三种失败都是 401

### 7.1 登录

`POST /api/auth/login` → access（JWT HS256，默认 15 分钟）+ refresh（32 字节随机，默认 30 天）。

对外永远同一句 401 + `UNAUTHORIZED`：用户不存在、密码错误、refresh 无效、refresh 重放，形状完全一样。用户不存在时也跑一次假 bcrypt，避免用耗时当用户名神谕。

`ErrRefreshTokenReused` 只给日志用（假定 token 被盗），handler 仍然映射成同一个 401。

### 7.2 为什么 refresh 用 SHA-256 不用 bcrypt

| | 密码 | refresh token |
|--|------|----------------|
| 熵 | 低（人选的） | 高（32 字节随机） |
| 威胁 | 离线爆破 | 泄露后直接用 |
| 该用 | bcrypt（慢） | SHA-256（快） |

用 bcrypt 存 refresh 是真实的性能错误：每次刷新 ~60ms，热路径变 CPU 瓶颈，而对高熵输入毫无收益。库里只有哈希，客户端只看到原文。

### 7.3 轮转与重放

每次刷新：原子地作废旧哈希、插入新哈希（`TryRotate`，事务留在 repository）。

重放已撤销的 token → 撤销该用户 **全部** 未撤销会话。这是「这枚 token 流到了两双手里」的保守反应。

对已撤销 token 再 `logout` **不得** 走重放路径，否则双击登出会踢掉其他设备。

Access token 签发后无法作废，所以 TTL 短；连续性靠 refresh。撤全部会话（ADR 0019）也只撤 refresh，已发出的 access 活到过期（默认 15 分钟），这个窗口被接受。

### 7.4 Author 是配置名，不是角色列

`AUTHOR_USERNAME` 指出谁能写。比较用 `strings.EqualFold`，与 `lower(username)` 唯一索引一致。不引入 `role` 列：这个产品没有权限模型。

第一个 Author：本地用 `DEV_AUTHOR_PASSWORD` 引导插入，或运维 SQL 插入。公开 `POST /api/users` 已关闭。

### 7.5 浏览器与脚本

- Author 区：token 对放在 **`sessionStorage`**（`authorTokens`）。同标签刷新还在，新标签没有，关浏览器即丢。不放 `localStorage`（XSS 面更大，且别人用这台电脑还能进）。跨标签同步等真痛了再用 `BroadcastChannel`，现在不做（ADR 0016）。
- 脚本：自己 login，每次轮转把新 refresh 写回，**禁止和浏览器共用同一枚 refresh**。两个人持有同一枚，`TryRotate` 会看成重放，全家踢掉。不另做 PAT（ADR 0018）。
- 将来 TOTP 只挡 **密码登录**，不挡 refresh。所以一枚已签发的 refresh = 完整 Author 能力（ADR 0017）。

---

## 8. 数据：三张表，迁移只向前

| 表 | 要点 |
|----|------|
| `users` | 唯一索引在 `lower(username)` / `lower(email)`，保留用户输入的大小写用于显示 |
| `refresh_tokens` | 存 SHA-256 hex；`ON DELETE CASCADE`；`revoked_at` 可空 |
| `posts` | `slug` 全局唯一（含 Draft）；`published` 默认 false；`published_at` 首次 Publish 写入，Unpublish 后保留 |

手写迁移器，不用 goose / golang-migrate：那两家的库 API 建立在 `database/sql` 上，而本服务用 pgxpool 原生 API，为 80 行功能再开一条驱动栈不值。

关键设计：

- `go:embed` 把 SQL 打进二进制，部署不用另带目录。
- `pg_advisory_lock` 串行化多副本同时启动。
- **没有 down migration。** 回滚 = 写一条新的向前迁移。生产上「把已经 DROP 的列再变回来」也恢复不了数据。
- 禁止改已提交的迁移文件：跑过的环境不会重跑它。

pgxpool 是惰性的：`NewWithConfig` 成功不代表数据库连得上，所以启动时要真正 ping，就绪探针也要真正 ping。

---

## 9. 博客领域（读 `CONTEXT.md` 比读代码快）

用词是产品边界，写代码和写 issue 都用这些词，不要发明同义词。

| 词 | 含义 | 刻意不用 |
|----|------|----------|
| User | 能登录的账号 | account, member |
| Author | 写 Post 的那个 User，全站一个 | admin, editor |
| Post | 一篇，有 Title / Slug / Body | article, page, content |
| Body | Markdown 源，不是 HTML；源里的 HTML 当文本 | content, html |
| Draft | 创建时的状态；Publish 是之后的动作 | unpublished, hidden |
| Published | 任何人不用登录就能读 | live, public |
| Slug | 作者选定的 ASCII 公开名，创建时定、永不改 | permalink, 从标题生成 |
| Preview | 编辑器里看渲染结果，**不是一个 URL** | draft URL |
| Destroy | 本博客不做 | delete |

几条会反复绊人的规则：

- **创建永远是 Draft**，没有「创建即发布」。
- **Draft 没有公开 URL。** `GET /posts/{slug}` 对 Draft 和未知 slug 都是同一张 HTML 404。地址栏里贴 slug 也一样。Preview 走 `POST /api/posts/preview`，服务端渲染，避免 Go 公开页和 JS 预览两套方言（ADR 0009）。
- **Unpublish 不清 `published_at`。** 「从未发布」和「曾上线后撤下」不是同一事实（ADR 0005）。没有 Destroy、没有回收站。
- **已发布的 Post 就地改。** 改 Title/Body 不会自动 Unpublish；没有修订历史。半成品保存会被读者看到——作者是一个人，应保存完成的文字（ADR 0010）。
- **并发编辑 last-save-wins。** 无 version token、无编辑锁（ADR 0015）。
- **图片是 `https://` URL**，本服务不存文件。distroless 非 root、无 shell、磁盘是临时的，对象存储是另一套系统（ADR 0013）。
- **Markdown 是安全 CommonMark 子集**：标题/列表/强调/引用/代码块；链接仅 http/https/mailto；图片仅 https；原始 HTML 转义。不为 GFM。Author 区浏览器里有 Bearer，Post 里可执行 HTML = 偷 token（ADR 0011）。
- **公开 HTML 也是 `Cache-Control: no-store`。** Unpublish 后不能在浏览器或共享缓存里留下一份可读副本（ADR 0014）。

站点名是常量 `model.SiteName`（`Personal Blog - klzw2233`），不是服务名 `go-http-service`。

---

## 10. 安全默认：没配置 = 拒绝，不是放开

这是整份代码里最值得当成肌肉记忆的一组选择。

| 点 | 默认 | 为什么 |
|----|------|--------|
| CORS | 空 = 不加任何 `Access-Control-*` | 跨域不是这个 API 的正常用法；`*` 和 Credentials 互斥；忘了配应该表现为被拒，而不是「莫名其妙能跨域」 |
| CORS 匹配 | 精确字符串，不子串 | `https://evil.example.com` 不能靠包含 `example.com` 混进去 |
| 可信代理 | 空 = 不信 `X-Forwarded-For` | 否则限流 key 可被伪造 |
| 安全头 | 无配置项，每个响应都有 | 含 404/500/超时；`X-XSS-Protection: 0` 是关掉旧审计器，不是开启 |
| 限流 | 内存、每副本独立；空闲桶淘汰 | 按 IP 的 map 本身是内存耗尽向量，必须扫 |
| 探针 | 不限流 | 编排器探活不能被自己打满 |
| 登录/刷新/登出 | 更严的桶（默认 5 次/分钟） | 人一天登几次；脚本想试几千次 |
| 密码猜测 | 将来打专用日志，不上 CAPTCHA | 已有登录桶；机器人挑战是第三方依赖（ADR 0020） |

独立 Author SPA 只把精确 origin 写入 `CORS_ALLOWED_ORIGINS`（例如 `http://localhost:5173`），不要改中间件，不要给 localhost 开后门，不要把默认改成 `*`（ADR 0021）。

运行镜像是 **distroless static nonroot**：无 shell、uid 65532、~10MB。`CGO_ENABLED=0` 静态二进制才能跑上去（pgx v5 纯 Go，天然满足）。不要换成 alpine「图方便能 exec」——镜像有 shell = 攻击面。`ENTRYPOINT` 必须是 exec 数组 `["/server"]`。version 在 **build 阶段** 用 `-ldflags` 注入，因为运行阶段没有 git。

compose 里 `DATABASE_URL` 的 host 是服务名 `db`，不是 `127.0.0.1`。`JWT_SECRET` 不进 compose 文件，用 `${JWT_SECRET:?...}` 从宿主机读，未设置直接失败而不是空值。

---

## 11. 测试与 CI

三层，缺一不可：

| 层 | 位置 | 测什么 |
|----|------|--------|
| 单元 | 各包 `_test.go` | service 用假 store；handler 用假 service；不需要数据库 |
| 需要库的测试 | 同包，看 `TEST_DATABASE_URL` | 未设置则 skip，所以笔记本上 `go test ./...` 仍全绿 |
| 端到端 | `cmd/server/e2e_test.go` | 编译真二进制、听真端口、发真 SIGTERM。单元测试在 `gin.TestMode` 里观察不到「进程跑在 debug 模式、把非 JSON 警告打进日志」这种问题 |

提交前与 CI 对齐：

```bash
gofmt -l cmd internal && go vet ./... && go test -race ./...
```

`-race` 不是可选项：测试里有 `t.Parallel` 和共享的 `testRouter`。

CI（`.github/workflows/ci.yml`）额外做：换行符（守的是「`.gitattributes` 被删」这种回归）、`govulncheck`（调用图分析，只报真正调用到的漏洞函数）、镜像构建。同一分支新推送会取消旧运行。

Win10 与 Ubuntu 同步，**所有文本文件 LF**（`.gitattributes`）。CRLF 会让 `gofmt` 把每个 `.go` 都标成未格式化，`git diff` 出现大量空白伪改动。不要改 `eol=lf`。

---

## 12. 已决定、尚未写代码

认证后续（ADR 0016–0020）已经拍板，实现另开：

| 做 | 不做 |
|----|------|
| `login_failed` 专用结构化日志（Warn，含 IP / request id / username，不含密码） | 跨标签同步 token |
| 撤全部 refresh：密码 step-up，Author 区一个按钮（ADR 0019） | PAT、Passkey |
| 公网上线前：TOTP + 一次性备份码（SHA-256，不是 bcrypt） | access token 黑名单 |
| | 登录页机器人挑战（CAPTCHA / Turnstile） |
| | 把公开博客改成 SPA / 公开 JSON feed |

Homelab 继续密码登录，直到安排 TOTP。紧急情况仍可 SSH 进库把 `refresh_tokens` 标撤销。

若加独立 Author SPA：只配 CORS；Preview 仍走服务端；不开放公开 JSON；不删 Go 的 `/` 与 `/posts/{slug}`；Go 的 `/author/*` 壳留到 SPA 真正替换那天（ADR 0021）。

Kubernetes 暂缓。当前方向是个人博客，不是编排演练。

---

## 13. 核心规矩速查（写代码前扫一眼）

1. Handler → Service → Repository，错误也是；handler 不 import repository。
2. 新 handler 是 `*API` 的方法；新依赖加字段，在 `New` / `main` 接线。
3. 阻塞调用传 `c.Request.Context()`；时间用 `a.now()`；日志用 `a.logFor(c)`（带 request_id）。
4. 对外错误只有 `model.ErrorResponse`；原始 error 只进日志。
5. 唯一性靠约束，先插再翻译 23505；改索引名必须改 repository 常量。
6. 密码 bcrypt + 字节上限；refresh SHA-256；轮转走 `TryRotate`；重放才全家撤销。
7. 登录失败对外同一 401；用户不存在也假 bcrypt。
8. DSN / JWT / 开发口令进 `LogValue` 必须脱敏；查询串、请求体、凭据不进日志。
9. CORS 默认拒绝；安全头无开关；限流表必须淘汰空闲桶。
10. 迁移只加新文件；公开读是 HTML；Draft 没有公开 URL；token 在 sessionStorage。
11. 注释英文，笔记/README 中文；换行 LF；提交前 `gofmt` / `vet` / `test -race`。

---

## 14. 笔记索引

按「现在还该不该读」分组。过时的不是废物，是当时的思考，但不要用它们当接口清单。

### 14.1 地基与垂直切片（建议按序）

| 笔记 | 读它为了什么 |
|------|----------------|
| `接入数据库前的地基.md` | 为什么接库前先做 DI / config / slog / Request ID / 超时 / 双探针 |
| `接入 PostgreSQL 步骤A-连接池与就绪探针.md` | pgxpool 惰性、DSN 进日志、关闭顺序 |
| `接入 PostgreSQL 步骤B-注册接口与分层落地.md` | 第一条穿到 SQL 的路径；手写迁移器；错误分层 |
| `接入 PostgreSQL 步骤C-登录JWT与限流.md` | refresh 为何不用 bcrypt；重放；限流 map 是 DoS 向量 |
| `接入安全中间件-CORS与响应头.md` | fail-closed CORS；`X-XSS-Protection: 0` |
| `使用 Docker 容器化部署.md` | 为什么 distroless 而不是 alpine；compose 的 host 是 `db` |

### 14.2 仍有用、但部分过时

| 笔记 | 注意 |
|------|------|
| `Go-HTTP-Service-架构讲解.md` | 单体进程、`cmd/` 约定仍对；「没有数据库/没有 Docker」已过时 |
| `分层架构.md` | 四层职责仍对；文中的 `main.go` 直接处理请求已不是现状 |
| `添加中间件.md` | 中间件是什么仍对；CORS「允许全部」的示例不要学，以步骤 D 为准 |
| `添加测试用例.md` | `testing` 基础仍对；现在还有 skip 库测试和真进程 e2e |
| `实现用户注册登录接口.md` | 文首已更正：注册是 `POST /api/users` 不是 `/api/auth/register`；细节看步骤 B/C |
| `连接 PostgreSQL 数据库.md` | 概念向；落地看步骤 A/B |
| `代码审查问题清单与改进计划.md` | 历史审查；第 1–8 步已完成，当考古 |

### 14.3 环境与工作方式

| 笔记 | 内容 |
|------|------|
| `go-framework-guides.md` | Gin 在 Go 框架版图里站哪一层；Echo/Chi/Fiber/Kratos/go-zero 等对照；为什么不是 go-spring |
| `在现有HomeLab框架上学习后端开发.md` | Win10 → nginx HTTPS → Ubuntu 上的 Go |
| `Win10与Ubuntu开发环境选择.md` | 为什么主开发在 Ubuntu |
| `VS Code Remote SSH 配置教程.md` | 日常编辑方式 |
| `GitHub作为Win10与Ubuntu代码同步方案分析.md` | 同步策略 |
| `Claude Code 在 Win10 与 Ubuntu 环境下的使用方案.md` | 本仓库的 AI 工作流 |

### 14.4 方向类（当历史，不当 roadmap）

| 笔记 | 注意 |
|------|------|
| `Go-HTTP-Service-后续扩展方向.md` | 五个方向里 1–4 和测试都已做完；当前方向是博客 + 认证后续 ADR |
| `项目成熟度与实际工作差距分析.md` | 步骤 C/D/E 之前的自评；读完本总纲再看，避免用旧能力表衡量现在 |

### 14.5 产品决策（ADR，英文短文）

`docs/adr/0001`–`0021`。写功能前先读相关编号，比在代码里猜意图快。认证后续 0016–0020 是「已决定未实现」；0021 是 Author SPA 的边界。

---

## 15. 建议的阅读路径（按你现在的目标）

**只想把服务跑起来**

`README.md` 的「如何运行」→ 本总纲第 6、7 节 → 打开 `/author/login`。

**想理解「为什么文件要这么切」**

本总纲第 3、4 节 → `接入数据库前的地基.md` → `步骤B` → 打开 `internal/handler/api.go` 和 `cmd/server/main.go` 对照。

**想理解认证**

本总纲第 7 节 → `步骤C` → ADR 0016–0020 → `internal/service/auth_service.go` + `repository/refresh_token_repository.go` 的 `TryRotate`。

**想改博客行为（slug、发布、预览、样式）**

`CONTEXT.md` → 相关 ADR（0004–0015）→ `internal/handler/blog.go` / `author_html.go` / `post.go`。不要从「给 Draft 一个预览 URL」或「公开 JSON feed」开始，那两件已经被拒绝过。

**想理解「为什么是 Gin、要不要换框架」**

`notes/go-framework-guides.md` → 本总纲第 3、4 节（分层和手工装配）→ `cmd/server/main.go`。结论先说：Gin 只是 HTTP 层；IoC / 微服务套件是另一层，这个体量用不上。

**想加一个新 JSON 接口**

扫第 13 节 → 在 `service` 写规则并用接口注入 → `handler` 加 `*API` 方法并在 `router.go` 按现有中间件组挂上 → 错误走 `ErrorResponse` → 补测试（能 mock 的不要碰库）→ 行为变了就改 `README.md`。

---

这份总纲会过时——过时的信号是：它和 `README.md` / `CONTEXT.md` / 最新 ADR 打架。以代码和 ADR 为准，再回来改这一页。
