# VS Code Remote SSH 配置教程

> 环境：Win10 + VMware Ubuntu（IP：192.168.170.128）
> 目标：在 Win10 上用 VS Code 编辑 Ubuntu 上的 Go 项目代码

---

## 一、整体架构

```text
Win10
  ├── VS Code 编辑器
  ├── Remote - SSH 扩展
  └── 浏览器访问 https://api.ubuntu.test

VMware Ubuntu 192.168.170.128
  ├── /home/jimmy/workspace/go-http-service  ← 代码仓库
  ├── Go 环境
  ├── Docker（PostgreSQL、nginx、Go 服务）
  └── SSH 服务
```

代码只存在于 Ubuntu，Win10 只做编辑和访问。

---

## 二、Ubuntu 侧准备

### 2.1 安装并启用 SSH

```bash
sudo apt update
sudo apt install -y openssh-server
sudo systemctl enable ssh
sudo systemctl start ssh
```

验证状态：

```bash
sudo systemctl status ssh
```

应显示 `active (running)`。

### 2.2 确认用户名和 IP

查看 IP：

```bash
ip addr show
```

假设用户名为 `jimmy`，IP 为 `192.168.170.128`。

### 2.3 创建工作目录

```bash
mkdir -p ~/workspace
```

---

## 三、Win10 侧准备

### 3.1 安装 VS Code

从 https://code.visualstudio.com/ 下载安装。

### 3.2 安装 Remote - SSH 扩展

1. 打开 VS Code
2. 点击左侧 Extensions 图标
3. 搜索 `Remote - SSH`
4. 安装 Microsoft 官方版本

---

## 四、配置 SSH 连接

### 4.1 打开 SSH 配置文件

按 `Ctrl+Shift+P` 打开命令面板，输入：

```text
Remote-SSH: Connect to Host...
```

选择 `Configure SSH Hosts...`，选择第一个配置文件（通常为 `C:\Users\Hasee\.ssh\config`）。

### 4.2 添加主机配置

```text
Host ubuntu-go
    HostName 192.168.170.128
    User jimmy
    Port 22
```

> 注意替换 `jimmy` 为实际 Ubuntu 用户名。

保存文件。

### 4.3 连接 Ubuntu

再次按 `Ctrl+Shift+P`，输入：

```text
Remote-SSH: Connect to Host...
```

选择 `ubuntu-go`，输入 Ubuntu 密码。

---

## 五、打开项目目录

连接成功后：

1. 点击 `Open Folder`
2. 输入路径：`/home/jimmy/workspace`
3. 选择或创建项目目录

如果项目已在 Win10 上，可通过 GitHub 或 SCP 迁移到 Ubuntu。

### 通过 GitHub 迁移

Win10：

```bash
cd "E:\Program Files\Claude Code\BackendEngineer\go-http-service"
git remote add origin https://github.com/yourname/go-http-service.git
git push -u origin main
```

Ubuntu：

```bash
cd ~/workspace
git clone https://github.com/yourname/go-http-service.git
```

### 通过 SCP 直接拷贝

Win10 PowerShell：

```powershell
scp -r "E:\Program Files\Claude Code\BackendEngineer\go-http-service" jimmy@192.168.170.128:~/workspace/
```

---

## 六、配置 Go 开发环境

### 6.1 安装 Go（Ubuntu 上）

```bash
wget https://go.dev/dl/go1.26.5.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.26.5.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
go version
```

### 6.2 安装 VS Code Go 扩展（Remote 模式）

1. 左侧 Extensions 搜索 `Go`
2. 选择 `Install in SSH: ubuntu-go`
3. 等待安装完成

### 6.3 打开 Ubuntu 终端

按 `Ctrl + ~` 打开终端，此时终端即为 Ubuntu bash。

验证：

```bash
go version
go test ./...
```

---

## 七、日常工作流

### Win10 侧

1. 打开 VS Code
2. `Ctrl+Shift+P` → `Remote-SSH: Connect to Host...` → `ubuntu-go`
3. 打开 `/home/jimmy/workspace/go-http-service`
4. 正常编辑代码

### Ubuntu 侧

在 VS Code 集成终端中：

```bash
cd ~/workspace/go-http-service
go build ./cmd/server
go run cmd/server/main.go
go test ./...
```

### Win10 浏览器验证

```text
https://api.ubuntu.test/api/health
```

---

## 八、可选：SSH 免密登录

### Win10 生成密钥

```powershell
ssh-keygen -t ed25519 -C "jimmy@win10"
```

按回车使用默认路径。

### 复制公钥到 Ubuntu

```powershell
type $env:USERPROFILE\.ssh\id_ed25519.pub | ssh jimmy@192.168.170.128 "cat >> ~/.ssh/authorized_keys"
```

之后连接 Ubuntu 无需输入密码。

---

## 九、验证清单

| 检查项 | 状态 |
|--------|------|
| Ubuntu SSH 已运行 | ✅ |
| VS Code Remote-SSH 扩展已安装 | ✅ |
| SSH config 已配置 ubuntu-go | ✅ |
| 可成功连接 Ubuntu | ✅ |
| 项目目录在 Ubuntu 上存在 | ✅ |
| Go 扩展安装到 Remote | ✅ |
| `go version` 在终端正常输出 | ✅ |
