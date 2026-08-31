# BinFlow

[English](README.md) | **简体中文**

BinFlow 是一个用 Go 从零实现的**云原生制品仓库**，架构与概念模型对标
JFrog Artifactory——仓库、存储、权限、REST 语义一一对应，Artifactory
用户迁移过来不用重学词汇。单静态二进制、零外部依赖，原生服务**十二个
包生态**（见下方矩阵），每种协议都支持 **local / remote（代理缓存）/
virtual（聚合）** 三种仓型（cargo 的 remote/virtual 待交付），并内嵌
Web 控制台。

M6 在其上补齐企业层：OIDC / LDAP 单点登录、S3 对象存储后端与本地→S3
在线迁移、单向 push 复制、Prometheus `/metrics` 指标端点、`bf` CLI 与
Artifactory 迁移工具（`bf-migrate`）。M7~M9 把它硬化到 pre-GA 完整形态：
细粒度 RBAC（`user` / `readonly_admin` / `admin` 三值角色 + `manage` 动作
下放仓库级管理权）、docker blob 上传**跨重启续传**（含 kill -9）、信息架构
与操作流**对齐 Artifactory 的 Web 控制台**——同一动作在同一位置，随附
24 任务操作路径对照表。服务端同步收口：用户/组生命周期端点、存储用量批量
查询、manage 过滤的权限列表、并发安全的 GC、OIDC 用户的 step-up 铸 Token；
发布镜像双架构（linux/amd64 + linux/arm64）。

M10 引入 **license / addon 档位体系**（community 地板 / pro / enterprise；
门控建仓与写动词，读永不劫持），go/nuget/cargo 为首批门控包型，另交付
属性系统。M11 把包型矩阵扩到十二个（conan/helm/rpm/debian 以 pro 档加入），
并新增**运行态认证配置面**（LDAP/OIDC/SAML 控制台或 REST 在线编辑、保存
即生效）、独立存储链配置文件 **`binstore.yaml`**（有序 provider 链 +
fail-fast 并存裁决）、debian/rpm 仓库元数据的 GPG **keypair 签名**，以及
remote 缓存的 **unused-cleanup 清理引擎**。

M12 落地**制品生命周期域**：copy/move/zip/`archive!`/explode
**制品操作族**与 **Trash can**（local 仓删除先捕获进内置 `auto-trashcan`
仓、逐节点带五元组溯源属性、REST 可恢复、默认 14 天保留期按小时清扫——
两者均为 pro 功能槽）。**NuGet 面补全**（v2 OData 全路由含 `$batch`；
v3 remote/virtual search 升为上游 SearchQueryService 实时代理 +
service index 动态解析）；dual-write 链在 S3 停机窗 **fail-open**（本地
优先写 + 持久重放队列、读回退磁盘超集、恢复后自动排空对账——ADR-0040）；
分块上传 REST 面已整体翻成 **Artifactory MPU 形状**（jfrog-cli 实测）；
控制台视觉层全面换装 **MUI 原生默认皮肤**（手写 base.css 983 → 443 行）。

M13（已完成）落地 **Webhook 统一事件面**：`/binflow/event/api/v1` 下的
官方七端点订阅族 + 13 域 66 型闭集（当前 9 型织入触发）、HMAC-SHA256
签名投递与官方重试语义（5 次首试计入、固定 10s 间隔、单次 30s 预算、
4xx 终态）、进程内排障环 + 五枚 Prometheus 指标族——pro 槽，SSRF 姿态
默认拒私网目标。**HelmOCI 仓型补全**（remote 代理：Bearer 上游认证 +
缓存优先降级；virtual 成员聚合：首见路由），helm remote 增
**`chartsBaseUrl` 分体回源基址**与 `_external`/`_transitive` 落盘缓存，
conan v1 recipe DELETE 翻转为**整树删**；**运行旋钮**落地
（`folder_download` 六字段 + `trashcan.retention_days`，重启生效，
`GET /api/v1/system/settings` 回显）。

## 包型矩阵（含档位）

| 档位 | 包型 | 说明 |
|---|---|---|
| **community**（地板——不装 license 也有） | generic、docker、maven、npm、pypi | 五核心：M1~M9 全部能力 + 属性系统 |
| **pro** | go、nuget、cargo（M10）· conan、helm、rpm、debian（M11）· helmoci、制品操作族 + 回收站功能槽（M12；trash 档位暂行）· webhook（M13） | 建仓/上传需 pro 及以上 license；license 失效后既有制品仍可读 |
| **enterprise** | （功能槽位：ha、xray-integration） | 占位槽位；本体 M14+ |

档位语义一句话：**读永不劫持**——license 缺失/过期只关闭建仓（400）与
写动词（403 + `X-Binflow-License-Required: <addon>`）；`GET /binflow/api/v1/addons`
返回逐槽位实时判定。完整指南：
[`docs/user/admin/license.md`](docs/user/admin/license.md)。

| 内容 | 位置 |
|---|---|
| 产品愿景与范围 | [`PRODUCT.md`](PRODUCT.md) |
| 里程碑（M1 内核 → M13 事件总线程；M1~M13 已完成） | [`ROADMAP.md`](ROADMAP.md) |
| M13 需求（PRD：Webhook 事件总线 / HelmOCI 补全 / 配置旋钮 / 行为债收口） | [`docs/prd/milestone-13.md`](docs/prd/milestone-13.md) |
| M12 需求（PRD：NuGet 补全 / 制品生命周期 / 行为债收口） | [`docs/prd/milestone-12.md`](docs/prd/milestone-12.md) |
| Artifactory 全量功能对照矩阵（213 条目——M10+ 路线图骨干） | [`docs/reverse/artifactory-full-feature-matrix.md`](docs/reverse/artifactory-full-feature-matrix.md) |
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
`uploads/` 都在里面——上传会话本身是 sqlite 里的行）。用配置文件或
环境变量覆盖——见[配置](#配置)。`Ctrl-C` 优雅退出（exit 0）。
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

## 不止 curl：M6~M9 能力与延伸阅读

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

### M7 —— 细粒度 RBAC、跨重启续传、step-up 铸 Token

- **三值角色**——每个用户携带 `user` / `readonly_admin` / `admin`
  （wire 字段 `adminRole`）。`readonly_admin` 可读全部管理面但永远
  不写；角色变更对该用户的**存量 Token 即时生效**（无需换发、无需
  重启）。角色分配仅 admin 可为。

  ```bash
  curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/users/auditor \
    -H 'Content-Type: application/json' \
    -d '{"name":"auditor","email":"auditor@t.io","password":"auditor-pw-1","adminRole":"readonly_admin"}' \
    -o /dev/null -w '%{http_code}\n'
  # 201
  curl -su admin:$ADMIN_PW $BASE/binflow/api/security/users/auditor | jq '{name,adminRole,enabled}'
  # {"name":"auditor","adminRole":"readonly_admin","enabled":true}
  ```

- **`manage` = 仓库级管理员**——permission target 的第四个动作。在某条
  target 上授出 `manage`，持有者即可管理覆盖到的仓库（编辑 target、
  配额、仓库配置），而无需是平台 admin：

  ```bash
  curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/users/carol \
    -H 'Content-Type: application/json' \
    -d '{"name":"carol","email":"carol@t.io","password":"carol-pw-123"}' -o /dev/null
  curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/app-local \
    -H 'Content-Type: application/json' \
    -d '{"rclass":"local","packageType":"generic"}' -o /dev/null
  curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/permissions \
    -H 'Content-Type: application/json' \
    -d '{"name":"t-app","repos":["app-local"],"includePatterns":["**"],
         "principals":{"users":{"carol":["read","write","delete","manage"]}}}' \
    -o /dev/null -w '%{http_code}\n'
  # 201 —— carol 自此管理 app-local（她授出的权限即时生效，全程不惊动平台 admin）
  ```

  指南：[RBAC 角色与仓库级管理员](docs/user/admin/rbac-roles.md)。
- **docker 上传跨重启续传**——分块 blob 上传被 `kill -9`、SIGTERM 或
  `docker compose restart` 打断后，服务端回来时从最后收到的字节继续
  （三条路径行为对称）。
- **Token 铸造 step-up**（可选，**默认关闭**）——开启后从控制台会话
  铸 Token 需要第二因子：本地/LDAP 用户重输口令，OIDC 用户走一次全新
  的 IdP 认证（`prompt=login`）。指南：
  [Token 铸造二次认证](docs/user/admin/token-step-up.md)。

### M8 —— 控制台对齐 Artifactory

- Web 控制台的信息架构与操作流跟随 Artifactory：双模式壳
  （应用 / 管理）、跨仓制品树与深链、Set Me Up 与 Deploy 对话框、
  全程键盘可达——自有皮肤，零复制资产。Artifactory 用户落地即知道
  每个东西在哪；逐任务的操作路径对照（24 个常见任务）见
  [Artifactory → BinFlow 操作路径对照表](docs/user/artifactory-path-map.md)，
  控制台指南见 [Web 控制台使用指南](docs/user/console.md)。

### M9 —— 服务端缺口收口

- **用户与组生命周期**——`enabled` 在所有读取面回显；
  `DELETE /binflow/api/security/users/{name}` 删除用户并全链级联（其
  API Token 与会话即刻 401；内置 admin 与自删被拒）；
  `GET /api/security/groups/{name}?includeUsers=true` 返回成员清单。

  ```bash
  curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/api/security/users/auditor \
    -o /dev/null -w '%{http_code}\n'
  # 200
  curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/api/security/users/admin
  # Cannot delete the built-in admin user.
  ```

- **一次请求代替 N 次**——`GET /api/v1/storage/usage` 一次拿全「已用」
  列（150 仓实例过去要发约 170 个请求）；`GET /api/v1/permissions?filter=manage`
  让 manage 持有者拿到恰好是自己在管、可编辑的 target——用上面 M7 例
  子里的 carol：

  ```bash
  curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/storage/usage | jq length
  # 2 —— 你可见的全部仓库，一次请求（generic-local + app-local）
  curl -su carol:carol-pw-123 "$BASE/binflow/api/v1/permissions?filter=manage" | jq '.[].name'
  # ["t-app"]
  curl -su carol:carol-pw-123 $BASE/binflow/api/v1/permissions -o /dev/null -w '%{http_code}\n'
  # 403 —— 不带 filter 的列表仍仅 admin/readonly_admin 可读
  ```

- **并发安全的 GC**——`gc --apply` 在每次物理删除前即时复核引用：
  并行 CI 推送与 `graceHours=0` 回收不再互相踩踏（验证套件恢复默认
  并发，`--workers=1` 权宜退役）。
- **npm CI 发布只需 `write`**——连发任意多新版本（含 dist-tag 移动）
  是标准路径、不需要 `delete`；仅 `npm deprecate` / 覆写已发布元数据
  才需要。指南：[npm 接入](docs/user/integrations/npm.md)。
  发布镜像双架构（linux/amd64 + linux/arm64 manifest）。

### M10/M11 —— 档位门控、四个新包型、配置面

- **license / addon 档位**——见上方矩阵。控制台 License & Add-ons 页
  粘贴装载，或 `POST /binflow/api/system/license`；
  `GET /binflow/api/v1/addons` 返回逐槽位实时判定。指南：
  [License 与 Add-ons 管理](docs/user/admin/license.md)。
- **四个 M11 包型（pro 档）**——全部真实客户端验证（conan 2.31/1.66、
  helm 4.2、Rocky 9 dnf、debian bookworm apt）：

  ```bash
  # conan：conan remote add + 修订链 upload/install
  conan remote add binflow $BASE/binflow/conan-local && conan remote login binflow admin -p "$ADMIN_PW"
  # helm：经典 chart 仓，index.yaml 自动重算
  helm repo add binflow $BASE/binflow/helm-local && helm install my-rel binflow/mychart
  # rpm/debian：repodata / dists 索引引擎 + GPG 元数据签名
  dnf install -y <pkg>   # baseurl=$BASE/binflow/rpm-local
  ```

  指南：[Conan](docs/user/integrations/conan.md) ·
  [Helm](docs/user/integrations/helm-charts.md) ·
  [RPM](docs/user/integrations/rpm.md) ·
  [Debian](docs/user/integrations/debian.md)。
- **认证配置面**——LDAP/OIDC/SAML 三段运行态在线编辑（控制台
  `/admin/security/auth` 或 `GET/PUT /binflow/api/v1/admin/security/{ldap,oauth,saml/config}`），
  **保存即生效**（无需重启）；secret 只写不读（脱敏回显）并经实例主密钥
  密封。指南：[认证配置](docs/user/admin/auth-config.md)。
- **`binstore.yaml`**——存储 provider 链独立成文件（与 binflow.yaml 同
  目录）：`[filestore]`、`[s3]` 或 `[filestore, s3]` 双写迁移链；与内嵌
  `storage:` 链键**语义分歧即拒启**（防静默择路）。指南：
  [存储配置](docs/user/admin/storage-config.md)。
- **GPG keypair 签名与 unused-cleanup**——服务端 keypair 管理面签 debian
  `InRelease`/`Release.gpg` 与 rpm `repomd.xml.asc`/`.key`（真实 apt/dnf
  gpgcheck 链验证过）；cleanup 引擎按小时 cron 回收闲置 remote 缓存
  （`POST /binflow/api/v1/system/cleanup` 手动 dry-run/apply）。

### M12 —— 制品生命周期、NuGet 补全、fail-open 双写（进行中）

- **copy / move / `archive!` / explode**——操作族共用一个 pro 功能槽
  （`repo-operations`）；树级拷贝零拷贝（blob 台账引用复用），带
  `X-Explode-Archive: true` 的 PUT 原地解包（归档原件不落库），
  `<archive>!/<entry>` 不解包直读归档成员。目录打包下载随
  `folderDownloadConfig` 交付（默认关，配置旋钮未落——见指南已知边界）。

  ```bash
  # community 实例：整族答 addon 门（真二进制 curl 实测）
  curl -u admin:$ADMIN_PW -X POST \
    "$BASE/binflow/api/copy/generic-local/src/a.bin?to=/dst/a.bin" -i | head -4
  # HTTP/1.1 403 Forbidden
  # X-Binflow-License-Required: repo-operations
  # {"errors":[{"status":403,"message":"license required: addon 'repo-operations' needs tier 'pro' (current: none)"}]}

  # pro：树级拷贝 / 干跑（响应 = Artifactory 的 CopyOrMoveResult 形）
  curl -su admin:$ADMIN_PW -X POST \
    "$BASE/binflow/api/copy/generic-local/acme?to=/staging/acme" | jq .
  # {"messages":[{"level":"INFO","message":"copying … completed successfully, N artifacts and M folders were copied"}]}

  # explode：上传归档并原地展开（201 + 条目计数头）
  curl -su admin:$ADMIN_PW -T bundle.zip -H 'X-Explode-Archive: true' \
    $BASE/binflow/generic-local/acme/bundle.zip -i | head -3
  ```

  指南：[制品操作族](docs/user/admin/artifact-operations.md)。
- **Trash can**——local 仓删除先捕获进内置 `auto-trashcan` 仓（五元组
  溯源属性），REST 可恢复，默认 14 天保留期按小时清扫（真二进制全链 +
  sha256 对账）：

  ```bash
  curl -su admin:$ADMIN_PW \
    "$BASE/binflow/api/storage/auto-trashcan/vlibs/com/acme/v.jar?properties" | jq .
  # {"properties":{"trash.time":["1787952557679"],"trash.deletedBy":["admin"],
  #   "trash.originalRepository":["vlibs"],"trash.originalRepositoryType":["local"],
  #   "trash.originalPath":["com/acme/v.jar"],"license":["apache-2.0"]}}
  curl -su admin:$ADMIN_PW -X POST \
    "$BASE/binflow/api/trash/restore/vlibs/com/acme/v.jar?transaction-size=100" | jq .
  curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/trash/empty | jq .
  # {"removed":2,"files":1,"folders":1,"bytes":3}
  ```

  指南：[Trash can 管理](docs/user/admin/trash-can.md)。
- **NuGet 补全**——v2 OData 面现在服务全路由集（`Search()` / `Packages()` /
  `GetUpdates()` / `$batch` / `Download` / DELETE / PUT 双形 + 409 重复臂）；
  v3 remote/virtual search 升为上游 SearchQueryService 实时代理
  （service index 动态解析、semver2 registration 路由族）。
  指南：[NuGet 接入](docs/user/integrations/nuget.md)。
- **fail-open 双写**——`[filestore, s3]` 链遇 S3 停机不再 500：上传本地
  优先落盘并进持久重放队列、读回退磁盘超集、恢复后自动排空 + 对账
  （`completed` 模式下队列非空拒启）。见
  [存储配置 · fail-open](docs/user/admin/storage-config.md)。

每种部署方式的安装指南（单二进制 / Docker / compose / Helm / K8s 清单 /
systemd / 离线 air-gapped / 升级）：[`docs/user/install/`](docs/user/install/)。
每协议客户端接入（docker/mvn/npm/pip/go/nuget/cargo/conan/helm/rpm/deb
配置片段）：[`docs/user/`](docs/user/README.md)。
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
  data_dir: "./data"        # blobs + uploads + sqlite 都在这里
  backend: "local"          # "s3" + storage.s3 段 → 对象存储（见 S3 指南）；
                            #   M11 起建议改用同目录的 binstore.yaml 链文件
security:
  anonymous_access: true    # 见安全须知
logging:
  level: "info"             # debug | info | warn | error
  format: "json"
auth:
  oidc: {}                  # auth.oidc 段 → SSO（见 OIDC 指南；首启种子进运行态配置面）
  ldap: {}                  # auth.ldap 段 → 目录登录（见 LDAP 指南）
```

M11 的两个配置面在此文件之外：

- **`binstore.yaml`**（同目录）——有序存储 provider 链：`[filestore]`、
  `[s3]`、或 `[filestore, s3]` + `migration.mode`（`bypass | dual-write |
  completed`）。文件缺席 = 零行为变化；与内嵌 `storage:` 链键分歧即拒启。
  指南：[存储配置](docs/user/admin/storage-config.md)。
- **运行态认证配置**——首启后 LDAP / OIDC / SAML 的权威配置是 DB 配置面
  （控制台或 REST），在线编辑即时生效。指南：
  [认证配置](docs/user/admin/auth-config.md)。

## 运维

```bash
./bin/binflow-server gc                 # 干跑：列出无引用 blob（默认）
./bin/binflow-server gc --apply         # 真正回收（有宽限期）
./bin/binflow-server export <dir>       # 在线备份（服务不停）
./bin/binflow-server import <dir>       # 恢复到空数据目录（服务停止）
./bin/binflow-server --version
```

`gc` 只删除无节点引用且超出宽限窗口（默认 24h）的 blob——中断的上传
不残留，删除制品的空间在宽限期后回收。M9 起 `--apply` 并发安全（每次
物理删除前即时复核引用），可与 CI 并行推送同时运行（`graceHours=0`
亦然）。备份/恢复手册：
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
`console`、`docs`）；业务包之间绝不互摸内部。CI
（`.github/workflows/ci.yml`）跑同一套 Makefile 目标——本地与 CI
同一入口。

## 许可 / 状态

pre-GA 软件。里程碑 M1~M11 已完成并打 tag（`m1-done` … `m11-done`）；
M12（NuGet 面补全、制品生命周期域——操作族与回收站——及行为债收口）
进行中。里程碑规划见 `ROADMAP.md`，当前进行中的工作见
`BOARD.md`，每轮迭代报告见 `reports/`。Artifactory 全量功能面的对齐
按条目跟踪于
[`docs/reverse/artifactory-full-feature-matrix.md`](docs/reverse/artifactory-full-feature-matrix.md)。
