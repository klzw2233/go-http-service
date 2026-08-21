# 项目环境说明

> 文件位置：`go-http-service/CLAUDE.md`
> 说明：本项目级规则用于告知 Claude Code 当前会话的运行环境特征。

---

## 一、运行环境

本项目在以下环境中进行开发和操作：

- **操作系统**：Windows 10 Home China
- **终端工具**：Windows Terminal
- **Shell 环境**：Git for Windows Bash（基于 MSYS2 / MinGW）
- **Go 环境**：已安装 `go` 命令，可用于编译、运行、模块管理

这意味着在这个项目里，**可以同时使用 Windows 风格命令和大量 Linux / Unix 风格命令**。

---

## 二、可用命令风格

### 2.1 Linux / Unix 风格命令可用

由于 Git for Windows Bash 的存在，以下命令通常可用：

```bash
ls                # 列出文件
cd /d/path        # 切换目录（注意 Windows 盘符前缀 /d/）
pwd               # 显示当前路径
mkdir -p          # 递归创建目录
touch file.txt    # 创建空文件
rm file.txt       # 删除文件
rm -rf dir/       # 递归删除目录（谨慎使用）
cat file.txt      # 查看文件内容
head -n 10 file   # 查看前 10 行
tail -n 10 file   # 查看后 10 行
grep "pattern"    # 文本搜索
curl url          # HTTP 请求
```

### 2.2 Windows 风格命令也可用

必要时仍可使用 PowerShell 或 CMD 风格命令：

```powershell
Get-ChildItem            # 等同 ls
Set-Location             # 等同 cd
New-Item -ItemType File  # 等同 touch
Remove-Item              # 等同 rm
```

### 2.3 Go 相关命令

```bash
go version
go run cmd/server/main.go
go build ./cmd/server
go mod init
go mod tidy
```

---

## 三、路径注意事项

在 Git for Windows Bash 中，路径表示有两种方式：

### 方式一：Linux 风格路径（带盘符前缀）

```bash
cd /e/Program\ Files/Claude\ Code/BackendEngineer/go-http-service
```

### 方式二：Windows 原生路径（需要加引号）

```bash
cd "E:\Program Files\Claude Code\BackendEngineer\go-http-service"
```

> 注意：路径中如果包含空格，必须加引号或转义。

---

## 四、推荐操作习惯

1. **优先使用 Go 标准工具**（如 `go run`、`go build`、`go mod tidy`），它们跨平台且行为一致。
2. **文件操作尽量使用 Claude Code 专用工具**（Read / Write / Edit / Glob / Grep），比直接执行 shell 命令更安全、可控。
3. **必要时可用 Bash 命令**做快速检查（如 `ls`、`curl`），但避免复杂的 `rm -rf`、`find | xargs rm` 等批量删除操作。
4. **服务启动测试**建议手动执行：
   ```bash
   go run cmd/server/main.go
   ```
   然后在另一个终端窗口测试：
   ```bash
   curl http://localhost:8080/api/health
   ```

---

## 五、项目关键路径

| 路径 | 说明 |
|------|------|
| `E:\Program Files\Claude Code\BackendEngineer\go-http-service` | 项目根目录 |
| `cmd/server/main.go` | HTTP 服务入口 |
| `notes/` | 学习笔记目录 |

---

## 六、后续核心方向

本项目后续将按以下顺序扩展：

1. 引入路由框架（Gin / chi）
2. 添加 PostgreSQL 数据库连接
3. 实现用户注册 / 登录 / JWT 认证
4. 添加日志、配置、错误处理模块
5. 使用 Docker 容器化部署
6. 尝试 Kubernetes 部署
