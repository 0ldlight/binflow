# BinFlow

[English](README.md) | **简体中文**

BinFlow 是一个用 Go 从零实现的**云原生制品仓库**，架构与概念模型对标
JFrog Artifactory——仓库、存储、权限、REST 语义一一对应，Artifactory
用户迁移过来不用重学词汇。单静态二进制、零外部依赖，原生服务五个包生态：
**Generic（raw HTTP）、Docker Registry v2（镜像/OCI，经 oras 可承载 Helm
chart）、Maven、npm、PyPI**——每种协议都支持 **local / remote（代理缓存）/
virtual（聚合）** 三种仓型，并内嵌 Web 控制台。

M6（当前里程碑）在其上补齐企业层：OIDC / LDAP 单点登录、S3 对象存储后端
与本地→S3 在线迁移、单向 push 复制、Prometheus `/metrics` 指标端点、
`bf` CLI 与 Artifactory 迁移工具（`bf-migrate`）。

| 内容 | 位置 |
|---|---|
| 产品愿景与范围 | [`PRODUCT.md`](PRODUCT.md) |
| 里程碑（M1 内核 → M6 企业就绪，M1–M5 已完成） | [`ROADMAP.md`](ROADMAP.md) |
| M6 需求（PRD v1.2：S3 / OIDC+LDAP / 复制 / 指标 / `bf` / `bf-migrate`） | [`docs/prd/milestone-6.md`](docs/prd/milestone-6.md) |
| 帮助文档中心（安装 / 接入 / 管理 / API / FAQ） | [`docs/user/README.md`](docs/user/README.md) |
| 架构规范 | [`docs/design/architecture.md`](docs/design/architecture.md) |
| 逆向行为规格 | [`docs/reverse/`](docs/reverse/) |
| 决策记录（ADR） | [`DECISIONS.md`](DECISIONS.md) |
| 任务看板 | [`BOARD.md`](BOARD.md) |
| 迭代报告 | [`reports/`](reports/) |

分割线以下全部**可直接复制执行**。所有 URL 使用统一的 `/binflow` 前缀：
管理/兼容端点在 `/binflow/api/...`，内容在 `/binflow/<repo>/<path>`
（两个根级例外：`/metrics` 与 docker 的 `/v2/...` 路由）。

## 快速开始

五步：**构建 → 启动 → 建仓 → 上传 → 下载 → 校验**。
给出两条可互换的路径——裸二进制（无需 Docker）与 docker compose。
示例假定 `bash`/`zsh`、`curl`、`jq`、`sha256sum` 可用
（macOS 用 `shasum -a 256` 等价替换）。

常量设置一次即可（改过端口就调整 `BASE`）：

```bash
export BASE=http://localhost:8080
export ADMIN_PW=password    # 评估默认口令；改法见下方第 0 步
```

> **第 0 步（推荐）：首次启动前设置真正的 admin 口令。**
> BinFlow 首次启动会种子化 `admin` 账号。未设置 `BINFLOW_ADMIN_PASSWORD`
> 时口令为文档化默认值 `password`——**仅限评估**，绝不上生产。默认口令
> 生效期间服务端持续打 WARN 日志。首次启动后再设该变量不会重新种子化；
> 轮换口令用
> `curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/password ...`
> （见下方「安全须知」）。

### 路径 A —— 裸二进制（无需 Docker）

**1. 构建**

```bash
make build
# 产出三个二进制：bin/binflow-server、bin/bf（CLI）、bin/bf-migrate（迁移工具）
```

**2. 启动**

```bash
./bin/binflow-server serve
# ... binflow starting ... binflow listening addr=:8080
curl -s $BASE/binflow/api/system/ping
# OK
```

默认：监听 `:8080`，数据落在 `./data`（按需创建；sqlite + `blobs/` +
`sessions/` 都在里面）。用配置文件或环境变量覆盖——见
[配置](#配置)。`Ctrl-C` 优雅退出（exit 0）。
另有一个一次性自测脚本：`scripts/smoke.sh` 起临时实例并跑
ping → 建仓 → 上传 → 下载 链路。

**3. 建仓**

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/generic-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"generic","description":"quick start"}'
# Successfully created repository 'generic-local'
```

**4. 上传**（服务端计算并回显校验和）

```bash
echo "hello binflow" > hello.txt
SHA=$(sha256sum hello.txt | cut -d' ' -f1)
curl -su admin:$ADMIN_PW -T hello.txt $BASE/binflow/generic-local/acme/hello.txt | jq .
# {
#   "repo": "generic-local",
#   "path": "/acme/hello.txt",
#   ...
#   "checksums": { "sha1": "...", "md5": "...", "sha256": "<与 $SHA 一致>" },
#   ...
# }
```

**5. 下载并校验**（注意：这个 GET 是匿名的——见下文）

```bash
curl -s -o hello.dl.txt $BASE/binflow/generic-local/acme/hello.txt
diff hello.txt hello.dl.txt && echo "content identical"
sha256sum hello.dl.txt   # == $SHA
```

### 路径 B —— docker compose

**0. 配置**（一次性；`.env` 已 gitignore）

```bash
cd deploy/dev
cp .env.example .env
# 编辑 BINFLOW_ADMIN_PASSWORD —— 不设 compose 拒绝启动
```

**1+2. 构建并启动**（回到仓库根目录；首次构建拉取 Go 工具链镜像，
之后增量构建）

```bash
docker compose -f deploy/dev/docker-compose.yml up -d --build
docker compose -f deploy/dev/docker-compose.yml ps
# NAME           STATUS                   PORTS
# binflow-dev    Up ... (healthy)         127.0.0.1:8080->8080/tcp
curl -s $BASE/binflow/api/system/ping
# OK
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/health | jq .
# { "status": "ok", ... }   （该端点需要认证）
```

**3–5 步**（建仓/上传/下载）与路径 A 的命令逐字节相同——这正是统一
`/binflow` 前缀的意义。

数据持久化在挂载于 `/var/lib/binflow` 的命名卷 `binflow_binflow-data`：
`docker compose restart` 与 `docker compose down && up -d`（不带 `-v`）
都保留制品。`down -v` 清空。

宿主机 8080 被占用时，在 `deploy/dev/.env` 设 `BINFLOW_BIND_PORT=18080`，
重新 `up -d`，并 export `BASE=http://localhost:18080`。

停止 / 清理：

```bash
docker compose -f deploy/dev/docker-compose.yml down       # 保留数据
docker compose -f deploy/dev/docker-compose.yml down -v    # 清空数据
```

### 刚才跑通了什么

- ping、建仓、带校验和协商的上传、带校验和验证的下载、匿名读——
  M1 验收链，至今仍是任意实例最快的健康检查（完整矩阵见
  `docs/prd/milestone-1.md` §5.3）。
- 上传是原子的（中断的上传永不可见），blob 按 sha256 内容寻址、跨路径
  跨仓去重，删除幂等。

## 不止 curl：M6 能力与延伸阅读

同一个实例无需改变形态即可长成企业面：

- **Web 控制台**——浏览器打开 `http://localhost:8080/binflow/ui/`
  （仓库 CRUD、制品树、上传/下载、搜索、用户/组、审计、GC、配额面板）。
  指南：[`docs/user/console.md`](docs/user/console.md)。
- **SSO 能力探测**——登录页（以及任何客户端）可以在没有任何凭据的
  情况下先探明实例提供哪些登录入口：

  ```bash
  curl -s $BASE/binflow/api/v1/auth/methods
  # { "password": true, "oidc": false, "ldap": false }
  ```

  启用 OIDC 或 LDAP 后对应位翻转为 true；配置指南：
  [OIDC 单点登录](docs/user/guides/oidc-config.md)（Keycloak/Okta/Azure AD、
  PKCE、组与管理员映射）与 [LDAP 目录认证](docs/user/guides/ldap-config.md)
  （OpenLDAP/AD、先本地后目录回退、ldaps/StartTLS）。
- **S3 对象存储**——`storage.backend: s3`（AWS S3、MinIO），blob 布局与
  本地一致；已运行的实例可走**在线双写迁移**无痛切换（断点续传，
  `GET /api/v1/storage/migration` 查进度）。
  指南：[S3 对象存储后端与在线迁移](docs/user/guides/s3-config.md)。
- **push 复制**——上传单向复制到目标实例（五协议全覆盖），事件驱动 +
  指数退避重试 + 定时兜底扫描。状态：`GET /api/v1/replication/status`。
- **Prometheus 指标**——`curl -s $BASE/metrics`（根级路径，默认匿名）：
  HTTP、存储、认证、复制四类指标族。
  参考：[Prometheus 指标参考](docs/user/metrics/prometheus-reference.md)。
- **`bf` CLI**——建仓/传制品/建用户/发 Token，不用手写 JSON：

  ```bash
  export BF_BASE_URL=$BASE BF_USERNAME=admin BF_PASSWORD=$ADMIN_PW
  bf repo create demo --type local --package-type generic
  bf artifact upload hello.txt --repo demo --path acme/hello.txt
  bf token create          # token 值仅显示一次
  ```

  手册：[`docs/user/guides/bf-cli.md`](docs/user/guides/bf-cli.md)
  （`~/.bf/config.yaml` 多 profile，密钥只留在环境变量）。
- **`bf-migrate`**——把 Artifactory 实例的仓库、用户、token 台账搬进
  全新 BinFlow（先 `--dry-run` 摸底，中断后 `--resume` 续传，产出
  `migration_report.json`）。指南：
  [从 Artifactory 迁移](docs/user/guides/migrate-artifactory.md)。

每种部署方式的安装指南（单二进制 / Docker / compose / Helm / K8s 清单 /
systemd / 离线 air-gapped / 升级）：[`docs/user/install/`](docs/user/install/)。
每协议客户端接入（docker/mvn/npm/pip 配置片段）：[`docs/user/`](docs/user/README.md)。
API 参考：[`docs/user/api-reference.md`](docs/user/api-reference.md)。
FAQ 与故障排查（含 Artifactory→BinFlow 概念对照表）：
[`docs/user/faq.md`](docs/user/faq.md)。

## 安全须知（暴露到网络前先读）

- BinFlow **只提供明文 HTTP**——TLS 终结交给前面的反向代理
  （`deploy/nginx/` 附带 nginx TLS 模板）。compose 默认绑定 `127.0.0.1`——
  非可信网络上请保持不动。
- **匿名读默认开启**（沿用 Artifactory 传统）：内容路径
  （`/binflow/<repo>/<path>`）的 `GET`/`HEAD` 无需凭据。写操作
  （`PUT`/`DELETE`）与全部 `/binflow/api/**` **永远**要求认证，不受此
  开关影响。关闭匿名读：

  ```bash
  # 环境变量（compose：写进 deploy/dev/.env）
  BINFLOW_SECURITY_ANONYMOUS_ACCESS=false
  # 或 binflow.yaml
  security:
    anonymous_access: false
  ```

  重启后未认证的内容 GET 返回 401。让本机以外的任何东西触达实例前，
  先关掉这个。
- **默认 admin 口令**：`admin` / `password` 的存在是为了开箱即用——
  仅是评估便利。首次启动前设置 `BINFLOW_ADMIN_PASSWORD`（环境变量，
  绝不写 YAML——密钥不进配置文件，ADR-0009），或启动后轮换：

  ```bash
  curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/password \
    -H 'Content-Type: application/json' \
    -d '{"oldPassword":"password","newPassword":"a-real-secret"}' \
    && export ADMIN_PW=a-real-secret
  # Password has been successfully changed
  ```

- 改口令、给每条 CI 发独立 API Token（`POST /binflow/api/security/token`）、
  吊销它们、把用户限定到路径模式（`/binflow/api/v1/permissions`）。
  OIDC/LDAP 用户与本地用户走同一套权限模型。

## 配置

一个 YAML 文件（`binflow.yaml`）加 `BINFLOW_` 前缀环境变量覆盖
（`__` 下钻一层，如 `BINFLOW_SERVER__LISTEN=:9090`）；环境变量优先于
YAML。密钥只走环境变量。完全不带配置文件启动则用文档化默认值。
完整字段表：`docs/design/architecture.md` §8。

```bash
./bin/binflow-server serve -c /path/to/binflow.yaml   # 显式文件（必须存在）
BINFLOW_HOME=/var/lib/binflow ./bin/binflow-server    # 默认值锚定在 $BINFLOW_HOME 下
BINFLOW_SERVER__LISTEN=:9090 ./bin/binflow-server     # 单旋钮覆盖
```

```yaml
# binflow.yaml —— 最可能碰的旋钮
server:
  listen: ":8080"
storage:
  data_dir: "./data"        # blobs + sessions + sqlite 都在这里
  backend: "local"          # "s3" + storage.s3 段 → 对象存储（见 S3 指南）
security:
  anonymous_access: true    # 见安全须知
logging:
  level: "info"             # debug | info | warn | error
  format: "json"
auth:
  oidc: {}                  # auth.oidc 段 → SSO（见 OIDC 指南）
  ldap: {}                  # auth.ldap 段 → 目录登录（见 LDAP 指南）
```

## 运维

```bash
./bin/binflow-server gc                 # 干跑：列出无引用 blob（默认）
./bin/binflow-server gc --apply         # 真正回收（有宽限期）
./bin/binflow-server export <dir>       # 在线备份（服务不停）
./bin/binflow-server import <dir>       # 恢复到空数据目录（服务停止）
./bin/binflow-server --version
```

`gc` 只删除无节点引用且超出宽限窗口（默认 24h）的 blob——中断的上传
不残留，删除制品的空间在宽限期后回收。备份/恢复手册：
[`docs/user/admin/backup-restore.md`](docs/user/admin/backup-restore.md)。

## 开发

工具链：Go 1.26（见 `go.mod`），golangci-lint 版本钉在 `.tool-versions`。
受限网络设 `GOPROXY=https://goproxy.cn,direct`（Makefile 已导出；ADR-0005）。

```bash
make dev   # vet + lint + test + build，push 前门禁
```

常用目标（`make help` 列全）：

| 目标 | 作用 |
|---|---|
| `make build` | CGO 禁用编译 `bin/binflow-server` + `bin/bf` + `bin/bf-migrate`（零 CGO 基线，ADR-0005）；打印二进制体积 |
| `make test` | `go test -race ./...` |
| `make test-cov` | 带覆盖率跑测试（`coverage.out`） |
| `make lint` | golangci-lint（缺失时安装钉定版本） |
| `make fmt` / `make vet` | gofmt / go vet |
| `make tidy` | 同步 `go.mod` / `go.sum` |
| `make run` | 构建后以 `./data` 在 `:8080` 起 `serve` |
| `make dev` | vet + lint + test + build（push 前门禁） |
| `make docs` | 构建 Docusaurus 帮助站点（嵌入 `/binflow/docs/`） |
| `make tools` | 打印工具链版本 |
| `make clean` | 清除 `bin/` 与覆盖率产物 |

目录布局遵循 `docs/design/architecture.md` §2：`cmd/`
（`binflow-server`、`bf`、`bf-migrate`）架在 `internal/` 各包之上
（`config`、`storage`、`metadata`、`auth`、`audit`、`repo`、`remote`、
`adapter`、`replication`、`migrate`、`metrics`、`client`、`httpapi`、
`console`）；业务包之间绝不互摸内部。CI（`.github/workflows/ci.yml`）
跑同一套 Makefile 目标——本地与 CI 同一入口。

## 许可 / 状态

积极开发中的 pre-GA 软件（M6）。M1–M5 已完成并打 tag（`m1-done` …
`m5-done`）；里程碑规划见 `ROADMAP.md`，当前进行中的工作见 `BOARD.md`，
每轮迭代报告见 `reports/`。
