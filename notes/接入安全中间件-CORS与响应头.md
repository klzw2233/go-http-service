# 接入安全中间件：CORS 与安全响应头

> 项目：`go-http-service`
> 主题：步骤 D——给所有响应加 CORS（fail-closed）与一组安全响应头
> 日期：2026-08-23

步骤 C 之后，roadmap 的下一项是「补充中间件：CORS、安全响应头」。限流已经在
步骤 C 一起上线了，所以这一步只剩 CORS 和安全响应头两块。

记录这一步的关键决策和踩坑点，而不是 API 用法。

---

## 一、CORS：默认 fail-closed，配置驱动

### 1. 为什么不直接 `Access-Control-Allow-Origin: *`

最省事的 CORS 是「允许全部」：

```go
c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
```

但本项目的安全默认和 `TRUSTED_PROXIES` 一样是 **fail-closed**：留空 = 拒绝。
理由有三条：

1. **跨域不是 API 的正常调用方式**。这个服务的调用方是后端或同源前端，不是任意
   网站。默认放开等于把接口暴露给任何能加载脚本的页面。
2. **通配符和凭据互斥**。浏览器禁止 `*` 与 `Access-Control-Allow-Credentials: true`
   同时出现。一旦前端需要带 Cookie 或 `Authorization`，通配符就用不了，必须回显
   具体来源。
3. **一个静默放开的 CORS 是最难排查的安全问题**。fail-closed 至少让「忘了配」
   直接表现为「跨域被拒」，而不是「莫名其妙能跨域」。

所以走配置：新增 `CORS_ALLOWED_ORIGINS`，逗号分隔，留空 = 拒绝全部跨域。
`*` 仍是一个**显式的**「全部放开」选项，但和「未配置」是两回事。

### 2. 精确匹配，不子串匹配

```go
allowSet := make(map[string]struct{}, len(allowed))
for _, o := range allowed {
    allowSet[o] = struct{}{}
}
_, ok := allowSet[origin]   // 精确字符串相等
```

如果用 `strings.HasPrefix`，`https://app.example.com` 会授权
`https://evilapp.example.com`——这是真实的 CORS 绕过姿势。map 精确匹配最简单
也最安全。

被命中的来源**原样回显**为 `Access-Control-Allow-Origin`（不是 `*`），
并加 `Access-Control-Allow-Credentials: true` 和 `Vary: Origin`。这样凭据类
请求能用，缓存也不会把针对一个来源的响应错发给另一个。

### 3. preflight 在 `c.Next()` 之前短路

```go
if c.Request.Method == http.MethodOptions {
    c.AbortWithStatus(http.StatusNoContent)
    return
}
```

OPTIONS 请求根本不需要走到 handler。直接 204 返回。对未授权来源的 preflight，
**也不回 ACAO**（仍 204），让浏览器拒绝——否则等于把「允许哪些方法」
泄露给了任意来源。

### 4. 请求头白名单固定，不回显

```go
const corsHeaders = "Origin, Content-Type, Authorization, X-Request-Id"
```

浏览器在 preflight 里会发 `Access-Control-Request-Headers` 列出它想带的头。
如果服务端原样回显，等于「客户端要什么就批什么」，白名单形同虚设。固定白名单
是正确的做法。

### 5. 没有 Origin 头就什么都不做

```go
if origin == "" {
    c.Next()
    return
}
```

没有 `Origin` 头的是同源请求或非浏览器客户端（curl、服务端到服务端）。
CORS 对它们根本不适用。这一步也顺便说明：**CORS 只影响浏览器**，
所以 fail-closed 不会挡住 API 的正常调用方。

---

## 二、安全响应头：无配置，写在 `c.Next()` 之前

### 1. 写在 `c.Next()` 之前是关键

```go
func SecurityHeaders() gin.HandlerFunc {
    return func(c *gin.Context) {
        h := c.Writer.Header()
        h.Set("X-Content-Type-Options", "nosniff")
        // ...
        c.Next()
    }
}
```

gin 的 `Header().Set()` 在 `c.Next()` 之前调用，这些头会在 handler 写 body
之前就挂在 `ResponseWriter` 上。所以即便是 handler 提前 `Abort` 的 404、
超时后的 503、或 panic 后 Recovery 兜底的 500，**响应都带得上安全头**。

这正是 `timeout` 中间件「只传播 deadline、不写响应」的设计带来的好处：
超时不会抢先写响应，handler 自己决定怎么响应，安全头始终在线。

### 2. `X-XSS-Protection` 设为 `0`，不是 `1; mode=block`

这是个反直觉的点。老教程会写 `X-XSS-Protection: 1; mode=block`，但：

- 现代浏览器（Chrome 78+、Edge、Firefox）**已经移除**了这个审计器
- 旧版本 IE/Edge 的审计器本身有**绕过路径**，开启反而引入风险

所以正确的值是 `0`——关闭它。这不是「不做防护」，而是「移除一个有漏洞的
旧机制」。真正防 XSS 的是 `Content-Security-Policy`（本 API 返回 JSON 不需要）。

### 3. `Cache-Control: no-store`

API 响应常带凭据（`Authorization`、用户数据），绝不能进缓存。`no-store` 是最
严格的一档：不存到磁盘、不存到内存、不进 bfcache。`no-cache` 不够——它只是
「用之前先 revalidate」，响应体仍可能被缓存。

### 4. 这一组头都做什么

| 头 | 作用 |
|----|------|
| `X-Content-Type-Options: nosniff` | 禁 MIME 嗅探，防上传文件被当脚本执行 |
| `X-Frame-Options: DENY` | 禁被任何页面嵌入，防点击劫持 |
| `X-XSS-Protection: 0` | 关闭旧版 XSS 审计器（见上） |
| `Referrer-Policy: strict-origin-when-cross-origin` | 跨域时只发 origin，剥路径与查询串 |
| `Strict-Transport-Security` | 30 天强制 HTTPS |
| `Permissions-Policy` | 禁用 camera/microphone/geolocation/payment 等强权能 API |
| `Cache-Control: no-store` | 不缓存 |

---

## 三、中间件顺序：加在 `limitBodySize` 之后

```
requestID       →  最先，后续所有日志才带得上 ID
requestLogger   →  紧随其后，才能量到完整耗时
CustomRecovery  →  在 logger 内层
timeout         →  设置 deadline
limitBodySize   →  请求体上限
SecurityHeaders →  安全响应头
CORS            →  最后
```

两个新中间件都放在 `limitBodySize` 之后：

- **SecurityHeaders 在 CORS 之前**：先写安全头，CORS 再补 `Access-Control-*`。
  两者都在 `c.Next()` 之前完成，所以顺序对最终响应头没有实质影响，但逻辑上
  「基础防护」先于「跨域放行」更清晰。
- **CORS 在最后**：preflight 要 `AbortWithStatus(204)`，放最后不影响它短路。

---

## 四、配置校验：origin 必须是完整 URL

```go
func validateOrigin(o string) error {
    if o == "*" {
        return nil
    }
    u, err := url.Parse(o)
    if err != nil || u.Host == "" {
        return fmt.Errorf("%q is not a valid origin; use a full URL such as https://app.example.com or *", o)
    }
    if u.Scheme != "http" && u.Scheme != "https" {
        return fmt.Errorf("%q must use an http or https scheme", o)
    }
    return nil
}
```

裸主机名（`app.example.com`）或缺协议（`app.example.com`）永远匹配不上浏览器
实际发送的 `Origin`，而一个**静默不匹配**的 CORS 配置是最难排查的——浏览器只
报「blocked by CORS」，不告诉你为什么。所以启动时就拒绝这种值，逼着写完整 URL。

这跟 `TRUSTED_PROXIES` 校验 IP/CIDR 是同一个思路：**配置错误要在启动时报出来，
不要让它变成运行时的玄学问题**。

---

## 五、测试策略

| 层次 | 关键用例 |
|------|---------|
| CORS 单元 | 未配置时 fail-closed（无 ACAO） |
| CORS 单元 | 无 Origin 头 = 不生效（同源/非浏览器） |
| CORS 单元 | 允许的来源被原样回显 + credentials=true |
| CORS 单元 | 子串相似的来源被拒（不子串匹配） |
| CORS 单元 | `*` 不启用 credentials |
| CORS 单元 | preflight 短路 204，未授权来源也不回 ACAO |
| CORS 单元 | 请求头白名单不回显 `Access-Control-Request-Headers` |
| 安全头单元 | 200 响应带头全集 |
| 安全头单元 | 500 响应也带头（写在 `c.Next()` 之前的价值） |
| 安全头单元 | `X-XSS-Protection` 是 `0` |
| 安全头单元 | 通过 `SetupRouter` 验证确实挂上了 |
| config | 默认 `CORSAllowedOrigins` 为 nil |
| config | 覆盖生效、逗号分隔、去空格 |
| config | 裸主机名 / 非 http(s) 协议被拒 |
| e2e | 真实进程上安全头齐全 |
| e2e | 未配置 CORS 时跨域被拒 |

CORS 单元测试用一个只挂 CORS 的最小 engine（不走限流/超时），避免无关中间件
干扰断言。安全头测试额外有一个 `TestSecurityHeaders_AppliedViaRouter`，用共享的
`testRouter`（完整 `SetupRouter`）确认中间件确实挂上了——这是防「写好了但忘接」。

---

## 六、验证

```bash
# 启动（默认 fail-closed，不带 CORS_ALLOWED_ORIGINS）
DATABASE_URL="postgres://app:devsecret@127.0.0.1:5433/go_http_service?sslmode=disable" \
JWT_SECRET="$(openssl rand -base64 48)" \
  go run cmd/server/main.go

# 安全头在真实响应上生效
curl -sI http://localhost:8080/api/health | grep -i 'x-content-type-options\|x-frame-options\|cache-control'

# 默认 fail-closed：带 Origin 但不配置 → 无 ACAO
curl -sI -H 'Origin: https://app.example.com' http://localhost:8080/api/health | grep -i 'access-control'

# 配置后允许跨域
CORS_ALLOWED_ORIGINS="https://app.example.com" ... go run cmd/server/main.go
curl -sI -H 'Origin: https://app.example.com' http://localhost:8080/api/health | grep -i 'access-control-allow-origin'

# preflight
curl -sI -X OPTIONS -H 'Origin: https://app.example.com' \
  -H 'Access-Control-Request-Method: POST' \
  -H 'Access-Control-Request-Headers: X-Exotic' \
  http://localhost:8080/api/health
# 期望 204，且 Allow-Headers 不含 X-Exotic
```

提交前检查：

```bash
gofmt -l cmd internal && go vet ./... && go test -race -count=1 ./...
/home/klzw2233/go/bin/govulncheck ./...
```

---

## 七、小结

这一步本身不复杂，但有两个原则值得记牢：

1. **安全默认 fail-closed**。CORS 留空 = 拒绝，和 `TRUSTED_PROXIES` 留空 = 不信任
   是同一种思维：宁可让调用方显式声明他要什么，也不要默认放开一个安全相关的开关。
2. **响应头写在 `c.Next()` 之前**，这样错误路径也带得上。这是 gin 中间件的一个
   小技巧：`Header().Set()` 是即时的，不像 `c.JSON()` 要等 handler 执行。
