# Claude Code 在 Win10 + Ubuntu 环境下的使用方案

> 环境：Win10 开发机 + VMware Ubuntu（192.168.170.128）
> 主题：在 Remote SSH + Ubuntu 运行代码的方案下，如何使用 Claude Code

---

## 一、核心问题

> **Win10 上的 Claude Code CLI 不能直接编辑通过 VS Code Remote SSH 存放在 Ubuntu 上的文件。**

因为 Claude Code 的操作对象是当前机器的文件系统，而 Remote SSH 的文件只存在于 Ubuntu。

但仍有多种方式可以在这个环境下使用 Claude Code，只是需要选择一种合适的配合方式。

---

## 二、方案对比

### 方案 A：在 Ubuntu 上安装 Claude Code（最推荐）

直接在 Ubuntu 中安装 Claude Code CLI，通过 SSH 在 Ubuntu 终端中运行。

```bash
cd ~/workspace/go-http-service
claude
```

#### 优点

- 代码、运行环境、AI 助手都在 Ubuntu，完全一致
- 无需文件同步
- 可以直接让 Claude 操作 Docker、PostgreSQL、nginx 配置
- 最符合 Remote SSH + Ubuntu 的架构

#### 缺点

- 需要在 Ubuntu 上重新登录 Anthropic 账号
- Ubuntu 上浏览器认证不如 Win10 方便
- 需要重新配置 API / CLI 授权

#### 可行性

完全可行。Claude Code 有 Linux 版本，官方支持。

---

### 方案 B：保留 Win10 代码副本，用 GitHub 同步

代码保留一份在 Win10（用于 Claude Code 编辑），编辑完推送到 GitHub，再在 Ubuntu pull 运行。

```text
Win10 + Claude Code → 编辑本地代码 → push GitHub
                                                  ↓
Ubuntu → pull GitHub → go run / docker compose up
```

#### 优点

- 不需要在 Ubuntu 配置 Claude Code
- 保留 Win10 使用习惯

#### 缺点

- 回到 GitHub 同步方案，迭代速度慢
- 小改动也要 commit / push / pull
- 和 Remote SSH 高效开发的理念有些矛盾

---

### 方案 C：用 VMware 共享文件夹

把代码目录做成 Win10 和 Ubuntu 共享文件夹，两边都能看到。

#### 优点

- 不需要 GitHub 同步
- 不需要在 Ubuntu 安装 Claude Code

#### 缺点

- Go 项目文件多，共享文件夹编译性能差
- 路径权限、文件同步延迟可能出问题
- 不是最干净的方案

---

### 方案 D：通过 MCP 让 Win10 Claude Code 操作 Ubuntu 文件（高级）

理论上可以配置一个 MCP SSH 文件系统服务器，让 Win10 上的 Claude Code 通过 SSH 读写 Ubuntu 文件。

但这不是 Claude Code 原生能力，需要手动配置 MCP server。

**当前阶段不推荐**。

---

## 三、推荐方案

| 优先级 | 推荐方案 |
|--------|----------|
| 效率优先，完全贴近生产 | 在 Ubuntu 上安装 Claude Code |
| 不想重新配置，继续 Win10 习惯 | Win10 编辑 + GitHub 同步 |

**最推荐方案 A**：

> 既然已经决定用 Remote SSH + Ubuntu 运行代码，不如把 Claude Code 也放到 Ubuntu，让代码、运行环境、AI 助手三者统一。

---

## 四、Ubuntu 上安装 Claude Code 的步骤

```bash
# 在 Ubuntu 中运行
curl -sSL https://claude.ai/code/install.sh | sh
```

验证安装：

```bash
claude --version
```

启动：

```bash
cd ~/workspace/go-http-service
claude
```

第一次运行需要浏览器登录 Anthropic 账号获取授权码。

---

## 五、Win10 上继续使用的过渡策略

如果暂时不想在 Ubuntu 上重装 Claude Code，可以这样过渡：

1. 当前 Win10 上的代码继续维护
2. 用 GitHub 同步到 Ubuntu
3. 等熟悉后，再在 Ubuntu 上安装 Claude Code

---

## 六、重要提醒

**当前通过 Win10 运行的 Claude Code 会话，与 Ubuntu 上运行的 Claude Code 会话互相独立。**

如果未来主要在 Ubuntu 上使用 Claude Code，需要在新终端中运行 `claude` 开启新会话。两个会话的项目上下文是分开的。
