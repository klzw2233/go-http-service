# 接入 PostgreSQL 步骤B：注册接口与分层落地

> 项目：`go-http-service`
> 主题：第一个垂直切片——从 HTTP 请求一路穿到 SQL，以及路上的五个坑
> 完成日期：2026-08-23
> 提交：`f8de8c1` `5d9195e` `55f23d0` `655b61c` `86cd0df` `d87d2d7` `446ea6a` `1e0e5f4`

---

## 一、这一步做了什么

步骤 A 只做了「连得上」——连接池接上了、探针在探测，但**一张表都没有**。

步骤 B 是第一个**垂直切片**：

```
POST /api/users
      ↓  Handler    解析请求、映射错误
      ↓  Service    校验密码长度、bcrypt 哈希
      ↓  Repository 拼 SQL、翻译唯一键冲突
      ↓  PostgreSQL users 表
```

`notes/分层架构.md` 讲了很久的四层结构，到这一步才真正落地。

同时把 `DATABASE_URL` 从可选改成了必需——有了依赖数据库的接口，可选就说不通了。

---

## 二、手写迁移器：为什么不用 goose

选型时才发现一个不明显的连带影响：**goose 和 golang-migrate 的库 API 都建立在
`database/sql` 之上**。而步骤 A 选的是 pgxpool 原生 API。

用它们意味着要额外通过 `pgx/v5/stdlib` 再开一条 `database/sql` 连接通道，
专供迁移使用。不算错（很多项目就这么干），但为了 80 行的功能多背一套驱动栈不值。

### 三个关键设计

**1. `go:embed` 把迁移打进二进制**

```go
//go:embed migrations/*.sql
var migrationsFS embed.FS
```

部署时不需要额外分发 SQL 文件，也不可能出现「二进制更新了但迁移目录忘了更新」
这种最难排查的状况。

**2. 每个迁移一个事务——这一条靠的是 PostgreSQL 的特性**

```go
tx, _ := conn.Begin(ctx)
tx.Exec(ctx, m.sql)                                    // DDL
tx.Exec(ctx, "INSERT INTO schema_migrations ...")      // 记账
tx.Commit(ctx)
```

**PostgreSQL 支持事务性 DDL**：`CREATE TABLE` 可以回滚。所以迁移和记账要么都成功、
要么都回滚，数据库永远不会处于「以为迁移过了其实没有」的状态。

**这是这个迁移器能写得这么简单的唯一原因。** MySQL 的 DDL 是隐式提交的，
迁移失败会留下半成品，工具必须额外维护修复逻辑——那才是 goose 这类工具复杂的来源。

**3. advisory lock 防止多副本撞车**

```go
conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID)
defer conn.Exec(..., "SELECT pg_advisory_unlock($1)", migrationLockID)
```

上 K8s 之后，两个 Pod 同时启动是**常态**而非例外。没有这把锁，
两个进程会同时执行 `CREATE TABLE users`，其中一个报 `relation already exists` 然后崩溃。

测试用 4 个 goroutine 并发调用 `Migrate`，断言全部成功且
`schema_migrations` 里只有一条记录。

### 一个测试上的坑：advisory lock 是可重入的

最初我这样测「锁有没有释放」：

```go
Migrate(ctx, pool, log)
// 用同一个 pool 尝试获取锁
conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", migrationLockID)
```

**这是无效的测试。** advisory lock 在**持有它的 session 内部是可重入的**，
连接池可能把刚才那条连接还回来复用——即使锁泄露了，同一个 session 也能再次获取成功。

必须用**另一个 session** 来验证：

```go
other := migrateTestPool(t)   // 独立的 pool = 独立的连接 = 独立的 session
```

> 另外，`pg_locks` 里 64 位的 advisory key 是拆成 `classid` + `objid` 两个 int32 存的，
> 直接 `WHERE objid = $1` 传一个 bigint 会溢出。用 `pg_try_advisory_lock` 探测更简单可靠。

### 没有 down 迁移

手写方案的取舍，但也符合实际：生产环境里回滚一个已经 `DROP COLUMN` 的迁移
**根本救不回数据**。正确做法是写一个新的前向迁移。

---

## 三、bcrypt 的 72 字节陷阱

这是本轮最值得记住的一条。

**bcrypt 只使用密码的前 72 字节，超出部分静默丢弃**——不报错，不警告。
后果是：两个前 72 字节相同的密码会被判为同一个，用任意一个都能登录。

麻烦在于这条限制**无法用 binding tag 表达**：

```go
Password string `json:"password" binding:"required,min=8,max=72"`   // 挡不住
```

gin 的 validator 对字符串用 `utf8.RuneCountInString`，按**字符**计数。
一个 72 个汉字的密码：

| 度量 | 值 |
|------|-----|
| 字符数 | 72 → 通过 `max=72` |
| 字节数 | **216** → 被 bcrypt 砍掉 144 字节 |

所以上限必须在 service 层按**字节**校验：

```go
if len(in.Password) > model.MaxPasswordBytes {   // len() 返回字节数
    return nil, ErrPasswordTooLong
}
```

测试直接用 `strings.Repeat("密", 72)` 钉住这个行为，并断言校验失败时
**不触碰数据库**。

> 更一般的教训：**Go 里 `len(string)` 是字节数，`utf8.RuneCountInString` 才是字符数**。
> 任何和外部系统（加密算法、数据库列宽、协议字段）对接的长度限制，
> 都要先搞清楚对方数的是哪个。

---

## 四、唯一性检查：先查后插是竞态

直觉写法：

```go
// 错误
if exists, _ := repo.ExistsByUsername(name); exists {
    return ErrUsernameTaken
}
repo.Create(user)
```

这是 **TOCTOU（time-of-check-to-time-of-use）竞态**：两个并发注册请求
都能通过 `ExistsByUsername`（那一刻确实还没人占），然后都去插入。
其中一个会拿到数据库的原始约束错误——而代码声称已经处理过这种情况了，
于是它变成一个 500。

正确做法是**直接插入，让数据库判定**：

```go
err := r.pool.QueryRow(ctx, `INSERT INTO users ... RETURNING id`, ...).Scan(...)

var pgErr *pgconn.PgError
if errors.As(err, &pgErr) && pgErr.Code == "23505" {   // unique_violation
    switch pgErr.ConstraintName {
    case "users_username_key": return ErrUsernameTaken
    case "users_email_key":    return ErrEmailTaken
    }
}
```

数据库在写入时**原子地**判定唯一约束，没有窗口期。

`ConstraintName` 还能区分是用户名冲突还是邮箱冲突——这是
「换个用户名」和「你已经有账号了」两种完全不同的提示。

### 用测试证明它

```go
// 8 个 goroutine 同时注册同一个用户名
assert.Equal(t, 1, succeeded, "只应有一个并发写入成功")
assert.Equal(t, racers-1, taken, "其余都应得到 ErrUsernameTaken，而不是 500 级错误")
```

如果改回「先查后插」，这条测试会看到多个成功、或者出现未翻译的原始错误。

> **索引名成了代码契约。** `users_username_key` 这个名字同时出现在迁移文件和
> repository 里。改索引名不同步改代码，精确的 409 会静默退化成 500——
> 所以常量旁边写了注释说明这一点。

---

## 五、大小写不敏感的唯一约束

`Jimmy` 和 `jimmy` 应该是同一个人，否则同一个人能注册两次。

两种做法：

| 做法 | 代价 |
|------|------|
| 存进去之前转小写 | 丢失用户输入的大小写，显示时不好看 |
| **索引建在 `lower(username)` 上** | 查询必须写 `WHERE lower(username) = lower($1)` 才命中索引 |

选了后者——**保留显示用的大小写，同时防止重复**：

```sql
CREATE UNIQUE INDEX users_username_key ON users (lower(username));
```

这叫**函数索引（functional index）**。代价要记住：普通的
`WHERE username = $1` 用不上这个索引，会退化成全表扫描。

---

## 六、分层：错误也要逐层翻译

`notes/分层架构.md` 第七节第 3 条写着「错误处理要分层返回」，
这一步才明白它具体是什么意思。

```
repository.ErrUsernameTaken
      ↓  service 翻译
service.ErrUsernameTaken
      ↓  handler 映射
409 CONFLICT
```

看起来是无谓的重复——为什么不让 handler 直接匹配 `repository.ErrUsernameTaken`？

因为那样 **handler 就得 `import repository`**，依赖方向就破了。
Handler → Service → Repository 这条线一旦在错误值上开个口子，
以后 handler 想拿 repository 的别的东西就没有理由拒绝了。

**这份重复就是分层的代价，也是它的意义。**

### 副产品：service 的测试不需要数据库

service 依赖的是接口而非具体类型：

```go
type userStore interface {
    Create(ctx context.Context, u *model.User) error
}
```

所以 `internal/service` 的全部测试用一个 20 行的 stub 就能跑，
不需要 `TEST_DATABASE_URL`。同理 handler 依赖 `userRegistrar` 接口，
handler 测试也不需要数据库。

**真正需要数据库的只有 repository 和端到端。**

---

## 七、密码哈希绝不外泄：两道防线

```go
// 防线 1：标签级
type User struct {
    PasswordHash string `json:"-"`
}

// 防线 2：构造级
type UserResponse struct {
    ID        int64     `json:"id"`
    Username  string    `json:"username"`
    Email     string    `json:"email"`
    CreatedAt time.Time `json:"created_at"`
    // 根本没有 PasswordHash 这个字段
}
```

只靠防线 1 的问题是：**struct tag 在重构中很容易丢**，而丢了的后果是
密码哈希上了公网，代码看起来还完全正常。

handler 返回的永远是 `UserResponse`。三个层次各有一个测试断言响应里
不出现哈希、不出现 `password`、不出现 `$2a$`。

---

## 八、我自己犯的两个错

### 8.1 前提失效的测试（不是 bug）

步骤 A 写的这条断言：

```go
assert.Contains(t, body, `"checks":[]`, "没有依赖时应是空数组而不是 null")
```

步骤 B 把数据库改成必需之后，服务**永远**有一个 database 检查，
`checks` 不可能为空了。

**这不是 bug，是作用域变更让测试的前提消失了。** 处理方式是把它改成断言现在的事实，
并把「空数组序列化成 `[]` 而非 `null`」这条性质留在 `internal/handler`
的单元测试里——那里可以真的不注册任何检查。

> 这类失败其实是好事：说明测试在检查真实行为，而不是跟着代码一起变的空壳。
> 真正要警惕的是反过来——改了行为但测试还是绿的。

### 8.2 `os/exec` 的管道竞态（真 bug，而且一直在侥幸通过）

端到端测试里验证关闭顺序的那条突然失败了，日志里**完全没有关闭相关的记录**。

根因是 `os/exec` 的一条约束：

> `Wait` will close the pipe after seeing the command exit, so it is
> incorrect to call `Wait` before all reads from the pipe have completed.

我的 `terminate()` 发完 SIGTERM 就调 `cmd.Wait()`，跟读日志的 goroutine 抢：
`Wait` 一看到进程退出就关掉管道，扫描器还没把最后几行写进缓冲区。

**它在步骤 A 是侥幸通过的**——那时关闭阶段只写一行日志，窗口小到没撞上。
步骤 B 加了 `closing database pool`，窗口一变就露馅。

修法是按文档要求的顺序：

```go
// 先等扫描器读到 EOF（进程退出时管道自然关闭）
select {
case <-s.logsDone:
case <-time.After(20 * time.Second):
    ...
}
// 再 Wait
s.cmd.Wait()
```

> 讽刺的是：这个 bug 恰好发生在**专门用来验证「关闭顺序」的那条测试**上——
> 它自己的关闭顺序就是错的。

---

## 九、成果

| 项 | 状态 |
|---|---|
| 迁移器 | 手写 80 行，embed + advisory lock + 每迁移一事务 |
| `users` 表 | 大小写不敏感唯一约束（函数索引） |
| 分层 | Handler → Service → Repository 三层落地，错误逐层翻译 |
| 注册接口 | `POST /api/users`，201 / 409 / 400 |
| 密码 | bcrypt，按字节校验上限，响应两道防线 |
| `DATABASE_URL` | 可选 → **必需** |
| 测试 | 180 个用例，`-race` 干净，带库和不带库两种情况 |

### 需要数据库的测试分布

| 包 | 需要数据库 | 原因 |
|----|-----------|------|
| `internal/model` | 否 | 纯数据结构 |
| `internal/service` | 否 | 依赖 `userStore` 接口，用 stub |
| `internal/handler` | 否 | 依赖 `userRegistrar` 接口，用 stub |
| `internal/config` | 否 | 只读环境变量 |
| `internal/db` | **是** | 迁移器要真的建表 |
| `internal/repository` | **是** | SQL 和约束必须真的执行 |
| `cmd/server` | **是** | 服务已要求数据库才能启动 |

**接口化的分层带来的直接好处**：三个包的测试完全不需要数据库，跑得飞快。

---

## 十、步骤 C：登录 + JWT

1. `POST /api/auth/login`：查用户 → `bcrypt.CompareHashAndPassword` → 签发 JWT
2. JWT 密钥进 config，**必需项且必须脱敏**（和 `DATABASE_URL` 同等对待）
3. 认证中间件：解析 `Authorization: Bearer`，用户 ID 注入 context
4. `GET /api/auth/me`：第一个受保护接口
5. Repository 加 `FindByUsername`，记得写 `WHERE lower(username) = lower($1)`

### 登录接口的两个安全点

**1. 用户不存在和密码错误必须返回完全相同的响应。**
否则接口就成了用户名枚举器——攻击者可以先批量确认哪些用户名存在，再针对性爆破。

**2. 两条路径的耗时也要接近。**
这一条更容易被忽略：用户不存在时直接返回，比查到用户后跑一次 bcrypt 比对
**明显更快**（bcrypt 默认成本约 60ms）。这个时间差足以用计时区分出来，
枚举照样成立。

做法是即使用户不存在，也拿一个预置的假哈希跑一次 `CompareHashAndPassword`，
把两条路径的耗时拉平。
