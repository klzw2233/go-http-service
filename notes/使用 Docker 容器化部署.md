# 使用 Docker 容器化部署

> 项目：`go-http-service`
> 主题：步骤 E——多阶段 distroless 镜像 + compose 编排
> 日期：2026-08-23

之前服务只能 `go run` 或 `go build` 出二进制在本机跑，依赖手动起 Postgres 容器、
手动 export 环境变量。这一步把 app + 数据库打成一套 compose，一键拉起完整链路。

记录这一步的决策和坑点。

---

## 一、多阶段构建：为什么用 distroless

### 1. 三个选项对比

| 镜像 | 大小 | 有 shell | 攻击面 |
|------|------|----------|--------|
| `golang:1.26`（直接跑） | ~850MB | 有 | 大 |
| `alpine:3.20` | ~8MB | 有 | 小 |
| `gcr.io/distroless/static-debian12:nonroot` | ~7MB | **无** | 最小 |

选 distroless 的理由：

1. **Go 服务天生适合**。`CGO_ENABLED=0` 编译出的是静态二进制，不依赖 glibc，
   正好能跑在 distroless/static（连 C 库都没有）上。pgx v5 是纯 Go 驱动，
   不需要 CGO。
2. **最小攻击面**。无 shell 意味着即便容器被攻破，攻击者也无法 `exec` 进去
   探索文件系统、执行任意命令。alpine 有 busybox shell，是个明显的降级。
3. **非 root 内置**。`:nonroot` 变体自带 uid 65532 用户，不用 `useradd`
   （distroless 也没 shell 跑 `useradd`）。

「图方便能 exec 进去调试」不是选 alpine 的好理由——要调试就挂个临时 sidecar
容器，或用 `distroless` 的 `:debug` 变体，不该让生产镜像常驻一个 shell。

### 2. Dockerfile 结构

```dockerfile
# syntax=docker/dockerfile:1

# ---- build stage ----
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download          # 单独一层，源码改动不重下依赖
COPY . .
ARG VERSION=dev              # version 在此注入，distroless 阶段没 git
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags "-s -w -X go-http-service/internal/model.Version=${VERSION}" \
      -o /out/server ./cmd/server

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
USER nonroot
EXPOSE 8080
ENTRYPOINT ["/server"]        # exec 数组形式，distroless 无 shell
```

三个 build flag 各有作用：

| flag | 作用 |
|------|------|
| `CGO_ENABLED=0` | 产出静态二进制，distroless/static 才能跑 |
| `-trimpath` | 去掉二进制里的构建机路径前缀（如 `/home/klzw2233/...`），不泄露 |
| `-s -w` | 去符号表和 DWARF 调试信息，镜像体积减半（服务端不需要） |
| `-X .../model.Version=${VERSION}` | 编译期注入 version 到 `/api/info` 字段 |

### 3. version 必须在 build 阶段注入

这是个真实的坑：`internal/model/info.go` 里 `Version` 是包级变量，通过
`-ldflags -X` 注入。但 distroless 运行阶段**没有 git**，没法在运行时算
`git describe`。所以 version 必须在 `golang:1.26` 那层算好，通过 build-arg
传进 `go build`。

```bash
docker build --build-arg VERSION="$(git describe --tags --always)" -t go-http-service:dev .
```

不传 `VERSION` 时默认 `dev`，本地开发够用。

### 4. distroless 无 shell 的两个后果

- `ENTRYPOINT` 必须是 **exec 数组形式** `["/server"]`，不能用 shell 形式
  `CMD server`（后者需要 `/bin/sh -c`，distroless 没有）
- 不能在容器里 `docker exec -it ... sh` 排障。要排障用 `:debug` 变体
  （`gcr.io/distroless/static-debian12:nonroot` 换成 `:debug-nonroot`），
  或挂临时 sidecar

---

## 二、docker-compose：app + db 编排

### 1. 关键点：DATABASE_URL 的 host 是服务名

```yaml
services:
  app:
    environment:
      DATABASE_URL: postgres://app:devsecret@db:5432/go_http_service?sslmode=disable
    depends_on:
      db:
        condition: service_healthy
```

`db` 是 compose 的服务名，**不是 `127.0.0.1`**。容器之间走 compose 网络，
`127.0.0.1` 在 app 容器里指的是「app 容器自己」，连不到数据库。这是容器化
最常踩的坑之一——本地 `go run` 时 host 是 `127.0.0.1`，进了 compose 就得
换服务名。

### 2. JWT_SECRET 不写进文件

```yaml
JWT_SECRET: "${JWT_SECRET:?JWT_SECRET must be set; ...}"
```

`JWT_SECRET` 是签名密钥，**绝不能进 git**。用 `${JWT_SECRET:?...}` 从
宿主机环境变量读。`:?` 的语义是「未设置就报错退出」，比默认空值安全——
空值会让 `config.Load` 拒绝启动，但报错信息在这里更直接。

> 这里有个 YAML 坑：值里含 `:` 时，整个值必须用双引号包起来，否则 YAML
> 把冒号当 mapping 分隔符报 `mapping values are not allowed`。错误信息
> 里也尽量别用冒号，免得再加引号。

### 3. depends_on + service_healthy 防启动竞态

```yaml
depends_on:
  db:
    condition: service_healthy
```

app 启动时如果 db 还没 ready，首次连接就失败、进程退出。`condition:
service_healthy` 让 app 等 db 的 `pg_isready` healthcheck 通过后再启动，
避免这个竞态。`docker compose up --wait` 会等到所有服务的依赖条件满足。

### 4. db 端口映射绑回环

```yaml
db:
  ports:
    - "127.0.0.1:5433:5432"
```

`127.0.0.1:5433` 只绑回环，本机工具（psql、`go test` 用 `TEST_DATABASE_URL`）
能连，但**不暴露到其他主机**。如果不写 `127.0.0.1` 前缀，默认绑
`0.0.0.0`，数据库端口就公开了。5433 是为了避开宿主机已有的 5432 PostgreSQL。

### 5. image: 和 build: 共存

```yaml
app:
  image: go-http-service:dev
  build:
    context: .
    args:
      VERSION: dev
```

两个同时写时，compose 的行为是：**镜像已存在就复用，不存在才 build**
（build 后打上 `image:` 指定的 tag）。这样：

- 本地 `docker compose up`：镜像不存在 → 自动 build `go-http-service:dev`
- CI：先 `docker build` 出 `go-http-service:dev`（注入 `ci-<sha>`）→
  compose 复用，不重建。冒烟测试能断言 version 是 `ci-<sha>`

---

## 三、.dockerignore：裁剪构建上下文

`.dockerignore` 决定哪些文件进 build 上下文。忽略掉无关文件能：

1. **加速**：上下文小，`COPY . .` 快
2. **避免缓存失效**：`.git` 一改 hash 就变，会让 build 层失效
3. **不泄露**：把 `.git`、笔记、`.claude` 挡在外面

本项目忽略：`server`（本地二进制）、`.git`、`.github`、`notes/`、`.claude/`、
IDE 文件。注意 `Dockerfile` 和 `docker-compose.yml` 本身也忽略——它们不需要
进镜像。

---

## 四、CI 加 docker job

在 `.github/workflows/ci.yml` 加独立 job（和 `check` 并行）：

```yaml
docker:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v7
    - uses: docker/setup-buildx-action@v3
    - uses: docker/build-push-action@v6
      with:
        context: .
        load: true                              # 加载到本地 docker，不推送
        tags: go-http-service:dev               # 和 compose 的 image: 一致
        build-args: VERSION=ci-${{ github.sha }}
    - name: 启动 compose 并等待就绪
      run: |
        export JWT_SECRET="$(openssl rand -base64 48)"
        docker compose up -d --wait
        # 轮询 /api/ready 直到数据库就绪
        ...
    - name: 冒烟测试
      run: |
        curl -fs .../api/health | grep '"status":"ok"'
        curl -fs .../api/info | grep "ci-"     # version 注入生效
    - name: 清理
      if: always()
      run: docker compose down -v
```

要点：

- **独立 job**：自己起 compose，不依赖 `check` 的 postgres service
- **`load: true`**：构建后加载到本地，不推 registry（没配 registry）
- **冒烟测试验证三件事**：health 存活、ready 报告数据库依赖、info 的 version
  含 `ci-` 标记（证明 ldflags 注入链路通）
- **`if: always()` 清理**：无论成败都 `compose down -v`，不残留容器和卷

---

## 五、本地验证流程

```bash
# 1. 构建镜像
docker build --build-arg VERSION=dev -t go-http-service:dev .

# 2. compose 一键起
export JWT_SECRET="$(openssl rand -base64 48)"
docker compose up -d --wait

# 3. 冒烟
curl -fs http://localhost:8080/api/health | grep '"status":"ok"'
curl -fs http://localhost:8080/api/ready | grep '"name":"database"'
curl -fs http://localhost:8080/api/info            # version 应是 dev

# 4. 优雅关闭验证（Docker stop 发 SIGTERM）
docker compose stop app    # 不应卡住，SHUTDOWN_TIMEOUT 内退出

# 5. 清理
docker compose down -v

# 6. 看镜像体积（应在 ~10MB 量级）
docker image ls go-http-service
```

---

## 六、没改 Go 代码

容器化这一步**没动任何 Go 代码**，因为：

- 服务器绑 `:PORT`（`cfg.Addr() = ":" + Port`），绑全部接口，Docker 端口
  映射能正常工作
- `main.go` 已经处理 `SIGINT`/`SIGTERM` 优雅关闭——Docker stop 发的就是
  SIGTERM
- 配置全走环境变量，`DATABASE_URL`/`JWT_SECRET` 必需，容器化天然契合

这正是前期把配置、信号处理、端口绑定做对的红利：容器化成了纯打包 + 编排，
不碰业务代码。

---

## 七、小结

两个原则值得记牢：

1. **distroless + 静态二进制是 Go 服务的最佳实践**。`CGO_ENABLED=0` 让
   Go 二进制能跑在最小攻击面的镜像上，不需要为了「能 exec 进去」降级到 alpine。
2. **配置错误要 fail-loud**。compose 里 `JWT_SECRET` 用 `:?` 未设置即报错、
   `depends_on: service_healthy` 防启动竞态，都是让问题在最早暴露，而不是
   变成运行时的玄学故障。

下一步是 Kubernetes 部署：`/api/health`、`/api/ready` 已可直接对接探针，
`SIGTERM` 优雅关闭已就绪，滚动更新可直接用。
