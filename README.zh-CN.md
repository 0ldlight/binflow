# BinFlow

[English](README.md) | **简体中文**

BinFlow 是一个用 Go 从零实现的**云原生制品仓库**，架构与概念模型对标
JFrog Artifactory——仓库、存储、权限、REST 语义一一对应，Artifactory
用户迁移过来不用重学词汇。单静态二进制、零外部依赖、内嵌 Web 控制台，
原生服务**十三个包生态**——每种协议都支持 **local / remote（代理缓存）/
virtual（聚合）** 三种仓型。本文适用于 BinFlow v1.0.0；完整文档见
[binflow.docs.buildwithfern.com](https://binflow.docs.buildwithfern.com)
（源在 [`docs/user/`](docs/user/README.md)）。

## 包型矩阵（含档位）

| 档位 | 包型 |
|---|---|
| **community**（地板——不装 license 也有） | [generic](docs/user/integrations/generic.md) · [docker](docs/user/docker-registry.md) · [maven](docs/user/integrations/maven.md) · [npm](docs/user/integrations/npm.md) · [pypi](docs/user/integrations/pypi.md) |
| **pro** | [go](docs/user/integrations/golang.md) · [nuget](docs/user/integrations/nuget.md) · [cargo](docs/user/integrations/cargo.md) · [conan](docs/user/integrations/conan.md) · [helm](docs/user/integrations/helm-charts.md)（charts + helmoci）· [rpm](docs/user/integrations/rpm.md) · [debian](docs/user/integrations/debian.md)，另含功能槽：制品操作族（copy/move/zip/`archive!`/explode）、回收站、webhook |
| **enterprise** | 功能槽：ha、xray-integration（占位） |

档位语义一句话：**读永不劫持**——license 缺失/过期只关闭建仓（400）与
写动词（403 + `X-Binflow-License-Required: <addon>` 头）；`GET
/binflow/api/v1/addons` 返回逐槽位实时判定。完整指南：
[`docs/user/admin/license.md`](docs/user/admin/license.md)。
所有 URL 使用统一的 `/binflow` 前缀——管理/兼容端点在 `/binflow/api/...`，
内容在 `/binflow/<repo>/<path>`（根级例外：`/metrics`、docker `/v2/...`），
控制台在 `/binflow/ui/`。

## 快速开始

五步：**构建 → 启动 → 建仓 → 上传 → 下载 → 校验**。给出两条可互换的
路径——裸二进制（无需 Docker）与 docker compose。示例假定 `bash`/`zsh`、
`curl`、`jq`、`sha256sum` 可用（macOS 用 `shasum -a 256` 等价替换）。

```bash
export BASE=http://localhost:8080
export ADMIN_PW=password    # 评估默认口令；轮换：PUT /binflow/api/security/password
```

> **第 0 步（推荐）：**首次启动前用环境变量设置 `BINFLOW_ADMIN_PASSWORD`
> （绝不写 YAML）。未设时种子化的 `admin` 口令为文档化默认值
> `password`——**仅限评估**，绝不上生产。首启后再设不会重新种子化；
> 轮换用 `curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/password ...`。

### 路径 A —— 裸二进制（无需 Docker）

```bash
make build   # 产出三个二进制：binflow-server、bf（CLI）、bf-migrate（迁移工具）
./bin/binflow-server serve
# ... binflow listening addr=:8080
curl -s $BASE/binflow/api/system/ping
# OK
```

默认监听 `:8080`，数据落在 `./data`（按需创建；sqlite + `blobs/` +
`uploads/` 都在里面）。用配置文件或环境变量覆盖——见[配置](#配置)。
`Ctrl-C` 优雅退出。

```bash
# 1. 建仓
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/generic-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"generic","description":"quick start"}'
# Successfully created repository 'generic-local'

# 2. 上传（服务端计算并回显校验和）
echo "hello binflow" > hello.txt
SHA=$(sha256sum hello.txt | cut -d' ' -f1)
curl -su admin:$ADMIN_PW -T hello.txt $BASE/binflow/generic-local/acme/hello.txt | jq .
# { "repo": "generic-local", "path": "/acme/hello.txt",
#   "checksums": { "sha1": "...", "md5": "...", "sha256": "<与 $SHA 一致>" }, ... }

# 3. 下载并校验（内容 GET 默认匿名可读）
curl -s -o hello.dl.txt $BASE/binflow/generic-local/acme/hello.txt
diff hello.txt hello.dl.txt && echo "content identical"
sha256sum hello.dl.txt   # == $SHA
```

### 路径 B —— docker compose

```bash
cd deploy/dev
cp .env.example .env      # 编辑 BINFLOW_ADMIN_PASSWORD —— 不设 compose 拒绝启动
cd -
docker compose -f deploy/dev/docker-compose.yml up -d --build
docker compose -f deploy/dev/docker-compose.yml ps
# binflow-dev    Up ... (healthy)    127.0.0.1:8080->8080/tcp
```

上面 1~3 步的命令逐字节相同——统一 `/binflow` 前缀的意义。数据持久化
在挂载 `/var/lib/binflow` 的命名卷：`restart` 与 `down && up -d`（不带
`-v`）保留制品，`down -v` 清空。8080 被占用时在 `deploy/dev/.env` 设
`BINFLOW_BIND_PORT=18080` 并调整 `BASE`。

刚才跑通了：ping、建仓、带校验和协商的上传、带校验和验证的下载、匿名
读。上传是原子的，blob 按 sha256 内容寻址、跨路径跨仓去重，删除幂等。

## 核心能力

- **Web 控制台**——仓库 CRUD、跨仓制品树、用户/组、审计、GC、配额面板：[`docs/user/console.md`](docs/user/console.md)
- **单点登录**——OIDC / LDAP，运行态配置保存即生效：[OIDC](docs/user/guides/oidc-config.md) · [LDAP](docs/user/guides/ldap-config.md) · [认证配置](docs/user/admin/auth-config.md)
- **存储**——磁盘或 S3（AWS/MinIO）、在线双写迁移、`binstore.yaml` provider 链：[S3](docs/user/guides/s3-config.md) · [存储配置](docs/user/admin/storage-config.md)
- **复制**——事件驱动单向 push、按需全量重同步、全局封锁闸：[治理](docs/user/admin/governance.md)
- **搜索**——AQL（`items.find({...})`）+ gavc/prop/pattern 端点，另有属性系统：[AQL](docs/user/aql.md) · [属性](docs/user/properties.md)
- **访问控制**——`user`/`readonly_admin`/`admin` 三值角色、`manage` 仓库级下放、API Token 与可选 step-up：[RBAC](docs/user/admin/rbac-roles.md) · [step-up](docs/user/admin/token-step-up.md)
- **制品生命周期**——copy/move/zip/`archive!`/explode 操作族与可恢复、带保留期的回收站：[操作族](docs/user/admin/artifact-operations.md) · [回收站](docs/user/admin/trash-can.md)
- **Webhook**——HMAC-SHA256 签名投递与重试语义：[`docs/user/admin/webhooks.md`](docs/user/admin/webhooks.md)
- **运维与可观测**——并发安全 GC、在线 export/import 备份、审计、配额、Prometheus `/metrics`：[备份](docs/user/admin/backup-restore.md) · [治理](docs/user/admin/governance.md) · [指标](docs/user/metrics/prometheus-reference.md)
- **工具**——`bf` CLI 与 `bf-migrate`（Artifactory 搬迁）：[bf CLI](docs/user/guides/bf-cli.md) · [迁移](docs/user/guides/migrate-artifactory.md)

## 部署形态

| 形态 | 指南 |
|---|---|
| 单二进制（linux/darwin/windows × amd64/arm64） | [`docs/user/install/binary.md`](docs/user/install/binary.md) |
| Docker 镜像（多架构，distroless/alpine） | [`docs/user/install/docker.md`](docs/user/install/docker.md) |
| docker compose（TLS 反代、MinIO profile） | [`docs/user/install/compose.md`](docs/user/install/compose.md) |
| Helm Chart（[`charts/binflow/`](charts/binflow)） | [`docs/user/install/helm.md`](docs/user/install/helm.md) |
| 原生 K8s 清单（[`deploy/k8s/`](deploy/k8s)） | [`docs/user/install/k8s.md`](docs/user/install/k8s.md) |
| systemd 服务（[`contrib/systemd/`](contrib/systemd)） | [`docs/user/install/systemd.md`](docs/user/install/systemd.md) |
| 离线安装包（[`deploy/offline/`](deploy/offline)） | [`docs/user/install/offline.md`](docs/user/install/offline.md) |
| 升级 | [`docs/user/install/upgrade.md`](docs/user/install/upgrade.md) |

## 安全须知（暴露到网络前先读）

- BinFlow **只提供明文 HTTP**——TLS 终结交给前置反向代理
  （[`deploy/nginx/`](deploy/nginx) 附 nginx TLS 模板）；compose 产物默认
  绑定 `127.0.0.1`。
- **匿名读默认开启**：内容路径的 `GET`/`HEAD` 无需凭据。写操作
  （`PUT`/`DELETE`）与全部 `/binflow/api/**` 永远要求认证。关闭匿名读：
  `BINFLOW_SECURITY_ANONYMOUS_ACCESS=false` 或在 binflow.yaml 设
  `security.anonymous_access: false`。
- 密钥只走环境变量（绝不写 YAML）。给每条 CI 发独立 API Token
  （`POST /binflow/api/security/token`），把用户限定到路径模式
  （`/binflow/api/v1/permissions`）。

## 配置

一个 YAML 文件（`binflow.yaml`）加 `BINFLOW_` 前缀环境变量覆盖
（`__` 下钻一层，如 `BINFLOW_SERVER__LISTEN=:9090`）；环境变量优先于
YAML，完全不带配置文件启动则用文档化默认值。完整字段表：
`docs/design/architecture.md` §8。两个配置面在此文件之外：
`binstore.yaml`（同目录——有序存储 provider 链）与 DB 承载的运行态认证面
（LDAP/OIDC/SAML 控制台或 REST 在线编辑，保存即生效）。

```yaml
server:
  listen: ":8080"
storage:
  data_dir: "./data"        # blobs + uploads + sqlite 都在这里
  backend: "local"          # "s3" → 对象存储（见 S3 指南）
security:
  anonymous_access: true    # 见安全须知
logging:
  level: "info"             # debug | info | warn | error
  format: "json"
```

## 运维

```bash
./bin/binflow-server gc                 # 干跑：列出无引用 blob（默认）
./bin/binflow-server gc --apply         # 真正回收（默认 24h 宽限期）
./bin/binflow-server export <dir>       # 在线备份（服务不停）
./bin/binflow-server import <dir>       # 恢复到空数据目录（服务停止）
./bin/binflow-server --version
```

GC 在每次物理删除前即时复核引用，可与 CI 并行推送同时运行。手册：
[`docs/user/admin/backup-restore.md`](docs/user/admin/backup-restore.md)。

## 开发

工具链：Go 1.26（`go.mod`），golangci-lint 版本钉在 `.tool-versions`。
`make dev` 是 push 前门禁（vet + lint + test + build）；`make help` 列全
部目标。目录布局遵循 `docs/design/architecture.md` §2：`cmd/`
（`binflow-server`、`bf`、`bf-migrate`）架在 `internal/` 各包之上；CI
跑同一套 Makefile 目标。

## 文档与链接

| 内容 | 位置 |
|---|---|
| 文档站（安装 / 接入 / 管理 / API / FAQ） | [binflow.docs.buildwithfern.com](https://binflow.docs.buildwithfern.com) · 源 [`docs/user/`](docs/user/README.md) |
| API 参考（Artifactory 兼容子集 + `/api/v1`） | [`docs/user/api-reference.md`](docs/user/api-reference.md) |
| FAQ 与故障排查（含 Artifactory→BinFlow 对照表） | [`docs/user/faq.md`](docs/user/faq.md) |
| 产品愿景与范围 | [`PRODUCT.md`](PRODUCT.md) |
| 架构规范 | [`docs/design/architecture.md`](docs/design/architecture.md) |
| 决策记录（ADR） | [`DECISIONS.md`](DECISIONS.md) |

## 状态

pre-GA 软件，持续开发中。尚未声明开源许可（仓库内无 `LICENSE` 文件）；
产品自带 community / pro / enterprise 档位体系——见上方矩阵。
