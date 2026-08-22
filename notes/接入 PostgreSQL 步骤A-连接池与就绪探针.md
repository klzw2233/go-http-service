# 接入 PostgreSQL 步骤A：连接池与就绪探针

> 项目：`go-http-service`
> 主题：为什么把「接数据库」拆成三步、第一步只做「连得上」，以及这一步踩到的坑
> 完成日期：2026-08-23
> 提交：`740bac9` `d509214` `2ccddb4`

---

## 一、为什么第一步不写任何业务逻辑

上一轮做完了地基（依赖注入、配置层、slog、Request ID、单请求超时、双探针），
但那套东西**只被测试验证过，没有被任何真实依赖验证过**：

- `WithReadyCheck` 的扩展点——从来没挂过真的检查
- `defer pool.Close()` 的关闭顺序——从来没有池可关
- `c.Request.Context()` 的传播链——从来没有查询接收它

如果直接上「用户注册接口」，一旦地基有问题，排查时会和密码哈希、唯一约束、
迁移文件搅在一起，分不清是哪一层的毛病。

所以拆成三步，第一步只接连接池：

| 步骤 | 内容 |
|------|------|
| **A** | 连接池 + 就绪探针接上真实数据库 ← 本篇 |
| B | 迁移机制 + `users` 表 + 注册接口 |
| C | 登录 + JWT |

**这个拆法的收益是可以量化的**：步骤 A 一共发现了 3 个问题（2 个凭据泄露向量 +
1 个测试写法缺陷），全部与业务逻辑无关。如果混在步骤 B 里，它们会淹没在
「注册接口为什么报错」的排查中。

---

## 二、pgxpool 是惰性的——这是最容易踩的坑

```go
pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
// err == nil 并不代表数据库连得上！
```

`pgxpool.New` / `NewWithConfig` **只校验配置，不建立任何连接**。
连接是在第一次真正用到时才建立的。

这意味着：如果只写到这里，一个连不上的数据库会让服务**正常启动**，
然后在第一个请求时才报错。配置写错了主机名，你要等到有人访问才知道。

所以必须显式 ping 一次：

```go
pingCtx, cancel := context.WithTimeout(ctx, cfg.DBConnectTimeout)
defer cancel()

if err := pool.Ping(pingCtx); err != nil {
    pool.Close()          // 别忘了，否则泄露资源
    return nil, fmt.Errorf("ping database: %w", err)
}
```

测试 `TestConnect_UnreachableHost` 就是钉这个行为的：
用 `192.0.2.1`（RFC 5737 保留的不可路由地址）作主机，
断言 **Connect 阶段**就报错，而不是等到后面。

---

## 三、加数据库连接串，等于同时打开两个凭据泄露口

这是本步骤最有价值的部分。`DATABASE_URL` 长这样：

```
postgres://app:devsecret@127.0.0.1:5433/go_http_service?sslmode=disable
                 ^^^^^^^^^ 口令
```

它会从两条路径漏出去，而且**两条都是我上一轮自己埋下的**。

### 3.1 启动日志会打印全部配置

上一轮给 `Config` 加了 `LogValue()`，服务每次启动都把配置写进日志。
我当时留了一句注释：

```go
// When database credentials are added, redact them here rather than
// at each call site.
```

现在就是那个时候。不处理的话，数据库口令会**每次启动写一遍**，
并被日志采集系统长期留存——比写进代码还糟，因为日志通常保留更久、访问面更广。

修法用标准库就够：

```go
func redactDSN(dsn string) string {
    if dsn == "" {
        return "(unset)"
    }
    u, err := url.Parse(dsn)
    if err != nil || u.Scheme == "" {
        return "(set)"        // 关键：不回退到打印原文
    }
    return u.Redacted()       // 口令 -> xxxxx，主机和库名保留
}
```

实际效果：

```json
"database_url":"postgres://app:xxxxx@127.0.0.1:5433/go_http_service?sslmode=disable"
```

**保留主机和库名是有意的**——这条日志的用处正是确认「部署真的连到了预期的库」，
全部打码就没有价值了。

**`(set)` 那条分支是重点**。pgx 也接受另一种连接串格式：

```
host=db port=5432 user=app password=devsecret
```

`url.Parse` 解析这种字符串不会报错，但结果没有 scheme，`Redacted()` 也无从下手。
此时**绝不能回退到返回原文**——解析失败恰恰是最容易漏出口令的情形。
返回固定占位符是唯一安全的答案。

### 3.2 驱动的错误信息里带着连接串

```go
poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
if err != nil {
    return nil, fmt.Errorf("parse DSN: %w", err)   // ← 危险
}
```

pgx 的解析错误会**引用它解析失败的那个字符串**。而这个 error 会一路返回到
`main`，被 `slog.Error` 写进日志。包装它等于把口令落盘。

所以定义了一个不含原文的哨兵错误：

```go
var ErrInvalidDSN = errors.New("DATABASE_URL is not a valid connection string")
```

代价是丢失了「具体哪里语法错了」的细节。但变量名已经指明了要检查什么，
而口令泄露是不可逆的——这个取舍很清楚。

测试 `TestConnect_ErrorNeverLeaksPassword` 用三条不同的失败路径断言密码不出现。

> **可以推广的规律**：任何第三方库的 error，在写进日志之前都要想一下
> 「它会不会把我传进去的东西原样吐回来」。连接串、token、请求体都属于这一类。

---

## 四、关闭顺序：靠 defer 的 LIFO 保证

```
1. 收到 SIGTERM
2. srv.Shutdown(ctx)    先排空在途 HTTP 请求
3. pool.Close()         再关连接池
```

顺序反了会**切断正在执行的查询**——请求还在等结果，池已经关了。

实现上不需要额外机制，靠 `defer` 的执行顺序就够：

```go
func run() error {
    pool, err := db.Connect(ctx, cfg)
    // ...
    defer func() {                    // ← 注册得早，所以执行得晚
        logger.Info("closing database pool")
        pool.Close()
    }()

    srv := &http.Server{...}
    // ...
    srv.Shutdown(shutdownCtx)         // ← 普通调用，先执行
    return nil                        // ← 返回后 defer 才跑
}
```

**`defer` 是后进先出**，而 `Shutdown` 是普通调用。所以「注册在前」= 「执行在后」。
这一点反直觉，`main.go` 里写了注释说明。

### 怎么证明它真的是这个顺序

这是整个地基里**唯一无法用单元测试证明**的设计。做法是在 defer 里加一行日志，
让顺序变成可断言的：

```go
logger.Info("closing database pool")
```

然后端到端测试断言两条日志的先后：

```go
drained    := strings.Index(out, "server stopped cleanly")
poolClosed := strings.Index(out, "closing database pool")
assert.Less(t, drained, poolClosed)
```

那行日志不是装饰，是为了让不可见的执行顺序变得可观测。

---

## 五、liveness 与 readiness：这次拿到了实证

上一轮只是照着设计写，这次真的停了数据库来看：

```bash
docker stop go-http-service-db
curl -o /dev/null -w 'ready=%{http_code}\n'  localhost:8080/api/ready    # 503
curl -o /dev/null -w 'health=%{http_code}\n' localhost:8080/api/health   # 200
docker start go-http-service-db
curl -o /dev/null -w 'ready=%{http_code}\n'  localhost:8080/api/ready    # 200
```

| | ready | health |
|---|---|---|
| 正常 | 200，`checks:[{"name":"database","status":"ok"}]` | 200 |
| **停库** | **503**（编排系统摘流量） | **200**（不触发重启） |
| 恢复 | 200 | 200 |

**这就是分级的意义**：数据库抖动时，摘掉流量但不重启进程。
如果 `/api/health` 也查数据库，K8s 会认为进程死了并重启所有 Pod，
重启期间连接池重建，反而给数据库更大压力——把依赖的短暂故障放大成自身的雪崩。

同时验证了错误分层：服务端日志留了完整错误

```json
{"level":"ERROR","msg":"readiness check failed","check":"database",
 "error":"failed to connect to `user=app database=go_http_service`: ... connection refused"}
```

而客户端只拿到 `check failed`。**主机名、用户名不该出现在对外响应里。**

---

## 六、配置上的两个决定

### `DATABASE_URL` 目前是可选的

现有四个接口（health / ready / info / echo）**没有一个需要持久化**。
设成必需就是强制一个用不到的依赖，而且会让「404 返回 JSON」这类
与数据库毫无关系的测试也被连带要求配一个 PG。

步骤 B 加了注册接口之后再改成必需——那时服务真的离不开它了。

> 同样的理由，步骤 A **刻意没有引入迁移机制**。当时一张表都没有，
> 引入迁移工具就是一个没有任何迁移文件的空脚手架。和「不加没人读的配置字段」
> 是同一个原则：不要为想象中的需求写代码。

### `DB_MAX_CONNS` 显式固定

pgx 的默认池大小是 `max(4, CPU 核数)`。这意味着**同一个服务换台机器部署，
就会对数据库产生不同的压力**——4 核机器开 4 条连接，32 核机器开 32 条。
数据库的连接数是有上限的，这种随环境漂移的行为不该留着。

---

## 七、测试策略：本地无库也要能跑

数据库测试用 `TEST_DATABASE_URL` 控制，**未设置就 skip**：

```go
func requireTestDatabase(t *testing.T) string {
    dsn := os.Getenv("TEST_DATABASE_URL")
    if dsn == "" {
        t.Skip("TEST_DATABASE_URL 未设置，跳过需要真实数据库的测试")
    }
    return dsn
}
```

这样在没装 PostgreSQL 的机器上 `go test ./...` 依然全绿。

但光这样不够——如果 CI 也没有库，这些测试就永远不会跑，等于没写。
所以 CI 配了 service 容器：

```yaml
services:
  postgres:
    image: postgres:17
    env:
      POSTGRES_USER: app
      POSTGRES_PASSWORD: ci
      POSTGRES_DB: go_http_service
    ports:
      - 5432:5432
    # 没有这个 healthcheck，测试会在 initdb 完成前就开始连
    options: >-
      --health-cmd "pg_isready -U app -d go_http_service"
      --health-interval 5s
      --health-retries 10
```

`health-cmd` 不是可选的：容器在 `initdb` 跑完之前就会报告 running，
不等它就会遇到随机的连接失败。

CI 还**额外跑一遍不带数据库的测试**。否则「本地无库也能开发」这个性质
会悄悄退化——某天有人写了个不 skip 的数据库测试，所有人就都被迫装 PostgreSQL 了。

---

## 八、我自己犯的一个错误（值得记下来）

端到端测试里我写了：

```go
assert.Contains(t, srv.firstLogLine(t), "xxxxx")   // 断言第一行日志含脱敏占位符
```

结果失败了。因为我这次新加了一行启动日志：

```json
{"msg":"database pool ready","max_conns":10}       ← 它成了第一行
{"msg":"server listening","config":{...}}          ← 配置转储在第二行
```

**问题不是代码错了，是断言依赖了日志的位置。** 只要有人在前面多加一条日志，
这个测试就会失败——而它本该关心的是「配置里的口令有没有脱敏」，与位置无关。

改成按字段精确断言：

```go
rec := srv.recordWithMsg(t, "server listening")   // 按 msg 找，不看位置
cfg := rec["config"].(map[string]any)
assert.Contains(t, cfg["database_url"], "xxxxx")
```

比原来更严格：不仅要求脱敏存在，还要求它出现在正确的字段里。

> **这是同一轮里第二次犯这个错**。上一次是「启动日志是 JSON」也用了
> `firstLogLine`，当时改成了「每一行都必须是 JSON」。
> 教训是：**日志断言不要依赖行号或顺序**，按内容查找。

---

## 九、成果与下一步

| 项 | 状态 |
|---|---|
| 连接池 | `internal/db`，pgxpool v5.10.0 |
| 就绪探针 | `/api/ready` 真实探测数据库 |
| 分级探针 | 已用停库/起库实证 |
| 关闭顺序 | 有端到端断言守着 |
| 凭据泄露 | 两个向量都堵上，各有测试 |
| CI | postgres service 容器 + 无库回归 |
| 测试 | `-race` 全绿，带库和不带库两种情况 |

### 步骤 B 要做的

1. 迁移机制（`goose` 或 `golang-migrate` + `embed.FS`）
2. 第一个迁移：`users` 表
3. `internal/repository` / `internal/service`，
   依赖方向 Handler → Service → Repository，**不得反向**
4. `POST /api/users` 注册接口，密码用 bcrypt 或 argon2
5. `DATABASE_URL` 改为必需

写查询时记住：**`pool.Query` 必须接收 `c.Request.Context()`**，
否则 `REQUEST_TIMEOUT` 对它无效，一条慢查询会一直占着 goroutine。
这条约定写在 `CLAUDE.md` 4.6。
