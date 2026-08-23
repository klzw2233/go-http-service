# 接入 PostgreSQL 步骤C：登录、JWT 与限流

> 项目：`go-http-service`
> 主题：注册出来的账号终于能用了——登录、JWT、refresh 轮转，以及限流为什么必须和登录一起上
> 完成日期：2026-08-23

---

## 一、这一步做了什么

步骤 B 交付了注册接口，但**注册出来的账号还没法用**。没有登录，也就没有任何
受保护的接口。同时登录接口是全站最需要保护的入口：一个没有限流的登录接口
就是在线爆破靶子。

所以这一步把三件事一起做：

| 子步骤 | 内容 |
|--------|------|
| C1 | `govulncheck` 进 CI |
| C2 | 按 IP 的内存限流，探针豁免 |
| C3 | 登录 + JWT + `GET /api/auth/me` |
| C4 | refresh token 轮转 + 重放检测 |

顺序有讲究：**限流必须和登录同时上线**。否则中间那段时间线上有一个可被
无限次尝试的密码校验接口。`govulncheck` 放在认证代码之前：马上要引入 JWT 库，
而认证相关依赖恰恰是漏洞影响最大的。

---

## 二、三个容易做错的点

### 1. refresh token 要哈希入库，但绝不能用 bcrypt

refresh token 和密码都是凭据，泄露数据库都会导致账号沦陷，所以都不能明文存。

但两者性质不同：

| | 密码 | refresh token |
|---|------|--------------|
| 熵 | 低（人选的） | 高（32 字节随机） |
| 威胁 | 离线爆破 | 仅泄露后直接使用 |
| 该用 | **bcrypt**（慢，抗爆破） | **SHA-256**（快即可） |

用 bcrypt 存 refresh token 是个真实的性能错误：每次刷新都要哈希一次，
默认成本 ~60ms 会让刷新接口变成 CPU 瓶颈。而 32 字节随机值根本不存在
「猜出原文」的问题，慢哈希毫无收益。

生成：`crypto/rand` 取 32 字节 → `base64.RawURLEncoding` 给客户端 →
SHA-256 十六进制存库。客户端永远只看到原文；库里永远只有哈希。

### 2. 按 IP 的限流表本身是内存耗尽向量

```go
limiters map[string]*rate.Limiter   // key 是客户端 IP
```

攻击者轮换源 IP 就能让这个 map 无限增长。必须有淘汰机制，否则限流中间件
自己成了 DoS 入口。

做法是机会式清扫：每个桶记 `lastSeen`，请求路径上顺带扫一遍空闲超过
`bucketIdleTTL` 的桶。不起后台 goroutine——路由没有生命周期管理，起了就得
管停止。

### 3. K8s 探针会被全局限流打死

`/api/health` 和 `/api/ready` 由 kubelet 高频探测，且全部来自同一个 IP。
被限流会直接导致容器被反复重启——限流中间件把服务搞挂了。

**探针必须豁免。** 登录走单独的、更严的桶。

---

## 三、登录的两个安全点

用户不存在和密码错误必须返回**完全相同**的 `ErrInvalidCredentials`，
否则接口就是用户名枚举器。

只统一错误还不够。用户不存在时直接返回会比跑一次 bcrypt 快约 60ms，
这个差值足以被计时区分出来。做法是即使用户不存在也执行一次假的哈希比对。

假哈希在构造时用当前配置的 cost 生成，不是硬编码常量——否则测试用
`MinCost` 时两条路径的耗时又对不上了。

JWT 本身：

- HS256，算法钉死，拒绝 `alg: none` 和算法混淆
- Claims 只用 `RegisteredClaims`（`sub` 放 user id），**不放用户名或邮箱**——
  JWT 的 payload 只是 base64，任何拿到 token 的人都能读
- `JWT_SECRET` 至少 32 字节，且必须在 `LogValue()` 里脱敏，和 DSN 同等对待

认证失败一律 401 + `UNAUTHORIZED`，不区分「没带 token」「格式错」「过期」
「签名无效」——区分了等于告诉攻击者他离成功还有多远。

---

## 四、refresh 轮转与重放检测

每次刷新都作废旧 token、签发新的一对。没有轮转，被窃取的 token 可以一直用
到过期。

重放检测是这套方案的核心价值：

```
收到一个已经被 revoke 的 refresh token
        ↓
说明它被用过第二次
        ↓
要么是客户端 bug，要么是 token 被窃取后攻击者在用
        ↓
保守处理：撤销该用户的全部 refresh token，强制重新登录
```

没有这一步，被窃取的 token 可以一直轮转下去，永不失效。

对外仍然是同一个 401。`ErrRefreshTokenReused` 只存在于内部，好让访问日志
记下「假定发生了窃取」——这和打错密码不是一类事件。

### 登出不是重放

对已经撤销的 token 再 logout，**不能**触发全家撤销。否则用户双击登出、
或客户端重试一次，就会把其他设备全部踢下线。`RevokeByHash` 对已撤销 /
过期 / 不存在一律返回 NotFound，由 service 译成 `ErrInvalidCredentials`。

### 事务放在 repository，不暴露给 service

轮转必须是「锁行 → 判断 → 撤旧插新」的原子操作。service 层不能 import
pgx 的事务类型，handler 更不能。所以 repository 提供一个 `TryRotate`，
重放策略也写在那里。看起来业务规则下沉了一点，但这是分层不把事务类型
泄漏上去的代价。

并发双刷新：第二个请求看到的是已被撤销的行，按重放处理、全家撤销。
这是保守的，可能误伤「两个标签页同时刷新」的用户，但计划如此。

---

## 五、限流的局限（必须写进文档）

内存限流是**每副本独立**的。N 个副本时实际限额是 N 倍。真要精确限流
需要 Redis，属于后续工作。本机没有 Redis，为这一步引入一个新容器不值。

客户端 IP 取自 `c.ClientIP()`，它的正确性依赖步骤 A 修好的 `TRUSTED_PROXIES`。
没有那一步，攻击者伪造 `X-Forwarded-For` 就能绕过限流。

超限返回 429 + `RATE_LIMITED`，带 `Retry-After`（RFC 6585）。

---

## 六、接口

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/auth/login` | 返回 access + refresh |
| POST | `/api/auth/refresh` | 用 refresh 换新的一对 |
| POST | `/api/auth/logout` | 撤销该 refresh token，204 |
| GET | `/api/auth/me` | 受保护接口的第一个例子 |

login / refresh / logout 共用登录限流桶。`/me` 走全局限流——能打到它的人
已经持有有效 token。

---

## 七、配置

| 变量 | 默认 | 说明 |
|------|------|------|
| `JWT_SECRET` | **必需**，≥32 字节 | HS256 密钥，启动日志脱敏为 `(set)` |
| `ACCESS_TOKEN_TTL` | `15m` | access token 有效期。短是因为无法撤销 |
| `REFRESH_TOKEN_TTL` | `720h`（30 天） | refresh token 有效期 |
| `RATE_LIMIT_RPS` / `BURST` | `20` / `40` | 全局（探针除外） |
| `LOGIN_RATE_LIMIT_RPM` / `BURST` | `5` / `5` | 登录相关接口，按分钟计 |

access token 短寿命是故意的：它一旦签发就无法撤销，寿命就是攻击者从
偷到的 token 上拿到的窗口。连续性靠 refresh。

---

## 八、沿用的规矩

- 错误分层：Repository → Service → HTTP，handler 不 import repository
- 阻塞调用传 `c.Request.Context()`
- 原始错误只写日志，对外稳定错误码
- 时间用注入的时钟，测试不 sleep
- 改已提交的迁移文件是禁止的；新表就是加 `0002_*.sql`
- `ON DELETE CASCADE`：删用户时自动清掉他的会话
