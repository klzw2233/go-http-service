# 项目环境说明

> 文件位置：`go-http-service/CLAUDE.md`
> 用途：告知 Claude Code 本项目的运行环境、约定与注意事项
> 最后更新：2026-08-22（从 Win10 + Git Bash 改写为 Ubuntu 虚拟机环境）

---

## 一、运行环境

| 项目 | 值 |
|------|-----|
| 操作系统 | Ubuntu 虚拟机（Linux 6.8.0） |
| Shell | bash |
| 项目根目录 | `~/workspace/go-http-service` |
| Go 版本 | go1.26.5 |
| 编辑方式 | 常从 Win10 通过 VS Code Remote SSH 连入 |

这是标准 Linux 环境，**直接使用 Linux 命令即可**，不需要考虑 Windows 路径或
PowerShell 兼容性。旧版本本文件描述的 `E:\Program Files\...` 路径已经作废。

---

## 二、常用命令

```bash
# 运行服务（默认 8080）
go run cmd/server/main.go

# 换端口
PORT=9000 go run cmd/server/main.go

# 构建
go build -o server ./cmd/server

# 提交前的完整检查（与 CI 的检查项一致）
gofmt -l cmd internal      # 应无输出
go vet ./...
go test -race -cover ./...
```

测试接口：

```bash
curl http://localhost:8080/api/health
curl http://localhost:8080/api/info
curl -X POST http://localhost:8080/api/echo \
  -H "Content-Type: application/json" -d '{"message":"hello"}'
```

---

## 三、GOCACHE 权限问题（已于 2026-08-22 修复）

保留此节是为了记录成因，避免再次踩坑。**当前无需任何特殊处理**，直接用默认配置即可。

### 曾经的现象

不设置 `GOCACHE` 时，`go build` / `go vet` / `go test` 报：

```
open /home/klzw2233/.cache/go-build/xx/xxxx-d: permission denied
```

### 根因

`~/.cache/go-build` 的属主变成了 **root:root**（创建于 2026-07-23），
说明当时用 `sudo go ...` 执行过命令，缓存目录被 root 占据，普通用户随后写不进去。

### 修复方式

```bash
sudo chown -R "$USER:$USER" ~/.cache/go-build
```

已执行并验证：属主恢复为 `klzw2233`，目录内无 root 残留，
默认 `GOCACHE`（`~/.cache/go-build`）下 `go build`、`go vet`、
`go test -race -cover ./...` 全部通过，`go clean -cache` 也能正常执行。

### 预防

> **不要用 `sudo` 运行任何 `go` 命令。** 这是问题的唯一成因。
> Go 的构建、测试、模块下载都不需要 root 权限。

`.gitignore` 里的 `.go-cache/` 规则保留作为保险，但项目内缓存目录已不再需要，
也不必再手动 `export GOCACHE`。

---

## 四、代码与提交约定

### 4.1 换行符必须是 LF

项目在 Win10 与 Ubuntu 之间同步，`.gitattributes` 已强制所有文本文件使用 LF。

原因：CRLF 会让 `gofmt` 把**每一个** `.go` 文件都标记为未格式化（曾经 7/7 全中），
同时在 `git diff` 里产生大量纯空白的伪改动。

不要修改 `.gitattributes` 里的 `eol=lf` 规则。若发现 `git status` 显示大量文件
被修改但 `git diff --ignore-all-space` 是空的，就是换行符问题复发了。

### 4.2 提交前必须通过的检查

CI（`.github/workflows/ci.yml`）会跑这些，本地先跑一遍可以避免来回：

```bash
gofmt -l cmd internal && go vet ./... && go test -race ./...
```

`-race` 不是可选项：测试里用了 `t.Parallel` 和共享的 `testRouter`。

### 4.3 代码注释用英文，笔记与文档用中文

现有代码的注释全部是英文（`// SetupRouter configures and returns...`），
新代码保持一致。`notes/` 下的学习笔记和 `README.md` 用中文。

### 4.4 错误响应必须走统一契约

所有对外的错误响应使用 `model.ErrorResponse`，不要临时用 `gin.H{"error": ...}`。
详见 `README.md` 的「错误响应契约」一节。

**绝不要把原始 error 直接返回给客户端**——validator 和 `encoding/json` 的报错
里含有 Go 内部结构体名（如 `EchoRequest`），且措辞随依赖版本变化。
原始错误只写日志，对外返回稳定的错误码。`internal/handler/errors.go` 是翻译层。

---

## 五、环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PORT` | `8080` | 服务监听端口 |
| `TRUSTED_PROXIES` | 空（谁都不信任） | 逗号分隔的可信代理 IP / CIDR |

`TRUSTED_PROXIES` 留空时 `c.ClientIP()` 取 TCP 真实对端地址，客户端无法通过
`X-Forwarded-For` 伪造自身 IP。只有确实部署在反向代理后面时才设置它。

---

## 六、项目结构

```
cmd/server/main.go          服务入口：超时配置、信号监听、优雅关闭
internal/handler/           HTTP 层
  router.go                 路由注册、代理信任、404/405/panic 处理
  health.go / info.go / echo.go
  errors.go                 绑定错误 -> 统一错误响应的翻译层
  middleware.go             请求体大小限制
  clock.go                  可注入的时间源（便于测试）
internal/model/             响应模型与错误码
notes/                      中文学习笔记
```

---

## 七、操作习惯

1. **优先使用 Claude Code 的文件工具**（Read / Write / Edit / Glob / Grep），
   比 shell 命令更安全可控。
2. **删除文件时只能逐个删除并写明路径**，禁止 `rm -rf`、`find | xargs rm`
   等批量删除。需要批量删除时应停下来让 Jimmy 手动处理。
3. **改动行为后同步更新 `README.md`**，不要留下与代码不符的文档。
4. **先讨论、再定方案、最后写代码**，不要一上来就改。

---

## 八、后续核心方向

1. ~~引入路由框架（Gin / chi）~~ 已完成
2. 添加 PostgreSQL 数据库连接
3. 实现用户注册 / 登录 / JWT 认证
4. 添加日志、配置、错误处理模块
5. 使用 Docker 容器化部署
6. 尝试 Kubernetes 部署

> 在做第 2 步之前，建议先把 handler 改成结构体 + 依赖注入
> （目前是包级函数，拿不到连接池）。理由与具体做法见
> `notes/代码审查问题清单与改进计划.md` 第六节。
