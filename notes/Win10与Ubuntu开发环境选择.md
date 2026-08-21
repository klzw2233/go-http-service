# Win10 与 Ubuntu 开发环境选择

> 环境：Win10 开发机 + VMware Ubuntu 服务器（192.168.170.128）
> 主题：后端开发中，代码编辑、运行环境分别放在哪里更合适

---

## 一、为什么不建议在 Win10 上运行服务

Win10 作为开发环境运行 Go 服务和数据库，会遇到几个问题：

| 问题 | 说明 |
|------|------|
| 路径差异 | Windows 路径和 Linux 路径不一致，Docker 构建容易出错 |
| 双重虚拟化 | VMware + Docker Desktop 性能开销大 |
| 和部署脱节 | 真实后端都跑在 Linux 上 |
| 数据库访问复杂 | Win10 连 Ubuntu Docker 里的 PostgreSQL，网络绕路多 |
| HTTPS 域名冲突 | Win10 本地服务和 Ubuntu nginx 服务都占 443 端口 |

---

## 二、推荐的工作流

```text
Win10（开发工作站）
  ├── VS Code / Cursor / JetBrains
  ├── 浏览器访问 https://api.ubuntu.test
  └── SSH 客户端

VMware Ubuntu（Linux 服务器）
  ├── Docker（PostgreSQL、nginx、Go 服务）
  ├── Go 环境
  └── 项目代码仓库
```

这是真实后端开发的标准模式：**Mac/Windows 写代码，Linux 服务器运行**。

---

## 三、Win10 开发环境要不要保留？

**要保留，但角色要明确**：

| 用途 | 是否保留 |
|------|----------|
| 代码编辑器 | ✅ 保留 |
| AI 编程辅助 | ✅ 保留 |
| 浏览器访问服务 | ✅ 保留 |
| Git 客户端 | ✅ 保留 |
| 在 Win10 上跑服务 | ❌ 不建议 |
| 在 Win10 上跑 PostgreSQL | ❌ 不建议 |

Win10 是 IDE 和浏览器，不是服务器。

---

## 四、Ubuntu 上需要安装什么

```bash
# 基础工具
sudo apt update
sudo apt install -y git curl wget htop openssh-server

# 启用 SSH
sudo systemctl enable ssh
sudo systemctl start ssh

# 安装 Go
wget https://go.dev/dl/go1.26.5.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.26.5.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# 验证
go version
```

Docker 假设已经在 Ubuntu 上配置好。

---

## 五、代码放在哪里的三种方案

### 方案 1：代码放在 Ubuntu，用 VS Code Remote SSH 编辑（推荐）

**优点**：
- 文件系统、编译、运行都在 Linux，完全一致
- VS Code 体验接近本地
- 无共享文件夹性能问题
- 最接近生产环境

**缺点**：
- 需要 SSH
- 断网时无法编辑

### 方案 2：VMware 共享文件夹

Win10 编辑，Ubuntu 运行。

**缺点**：
- Go 编译大量小文件，共享文件夹性能差
- 路径权限容易出问题
- 不太稳定

### 方案 3：代码放 Win10，每次传送到 Ubuntu

用 rsync / FTP 同步。

**缺点**：
- 流程繁琐
- 容易遗漏文件
- 不符合现代开发习惯

---

## 六、推荐最终结构

| 组件 | 推荐位置 |
|------|----------|
| 代码编辑 | Win10 + VS Code Remote SSH |
| 代码仓库 | Ubuntu `/home/jimmy/...` |
| Go 运行环境 | Ubuntu |
| Docker 容器 | Ubuntu |
| PostgreSQL | Ubuntu Docker 容器 |
| nginx 反向代理 | Ubuntu Docker 容器 |
| 浏览器访问 | Win10 |

---

## 七、这样做的好处

1. **贴近生产**：99% 的后端服务都是这样运行的
2. **避免环境差异**：文件系统、路径、网络统一
3. **性能更好**：Ubuntu 原生运行 Docker
4. **加深 Linux 能力**：每天都在 Ubuntu 命令行操作
5. **一个 VS Code 搞定**：不需要反复切换

---

## 八、最小可行方案

如果实在不想装 VS Code Remote SSH，可以先用简单方案过渡：

- 代码通过 VMware 共享文件夹放在 Ubuntu 中
- Win10 编辑
- SSH 到 Ubuntu 执行 `go run` 和 `docker` 命令

但长远看还是建议直接上 **VS Code Remote SSH**。
