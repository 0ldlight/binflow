---
title: FAQ 与故障排查
sidebar_position: 90
---

# FAQ 与故障排查

> 适用版本：M1~M9（各条目标注引入里程碑）。码值与文案以 M4（PRD milestone-4 v1.2）为基线，全部经 QA 真机验证（T-103/T-105 验收基线）；M7 增补条目（RBAC 只读短路 / S3 续传 404 / token step-up）以 ADR-0026/0027/0028 与 PRD milestone-7 v1.1 为准；M8 增补（控制台新路径重定向 / npm 发布权限语义）经 scratch 实例复跑（2026-08-24）；M9 增补（用户删除闭环 / npm 复制同口径）经 HEAD 构建 scratch 实例复验（2026-08-25）。

## 状态码信封解读

所有非 2xx 的制品域响应为统一信封（E-01）：

```json
{"errors":[{"status":413,"message":"Repository 'tiny' quota exceeded: used 800 of 1024 bytes; ..."}]}
```

用户管理/组/token 域为**纯文本**错误体，docker `/v2` 面为 spec 信封（`{"errors":[{"code":"DENIED",...}]}`）——三种格式并存是 Artifactory 兼容面，客户端按域名解析即可。

### 401（未认证）

| 场景 | 表现 | 处置 |
|---|---|---|
| 控制台会话过期/未登录 | `GET /api/v1/session` 401；界面 toast「登录已过期」+ 重登 | 重新登录；会话有绝对寿命（见[控制台指南](console.md#登录与会话)），**活跃也不能续命** |
| 登录口令错误 | `{"errors":[{"status":401,"message":"invalid credentials"}]}`——用户不存在与口令错**文案相同** | 核对凭据；连续失败落 `login.failed` 审计 |
| 匿名写操作（publish/push/deploy/upload） | `authentication required` | 配置客户端凭据（settings.xml / `_auth` / `.pypirc` / `docker login`） |
| docker 面未认证 | `401 + Www-Authenticate: Bearer realm=".../v2/token"`——这是**协议正常流程**，客户端自动协商 | 无需干预 |
| 已吊销/过期 token 或会话 cookie 重放 | 401（呈交但失效的凭据**绝不降级匿名**） | 换有效凭据 |

### 403（已认证但无权限 / CSRF）

| 场景 | 表现 | 处置 |
|---|---|---|
| 无 write 权限的路径上传 | 403 `permission denied`（各协议 verbatim 渲染） | 找 admin 加 permission target；组授权即时生效无需重启 |
| 覆盖已有制品但无 delete 权限 | 403 | 覆盖 = 对旧文件的删除，需 delete 权限。**npm 例外（M8 起）**：发布新版本/dist-tag 移动仅需 write（[npm 发布权限语义](integrations/npm.md#发布权限语义m8-起)） |
| **控制台会话 + 跨站 Origin 的写请求** | 403（E-01，CSRF Origin 防线） | 同源页面操作即可；curl/CI **不受影响**（Basic/token 免疫） |
| 非 admin 访问管理面（用户/组/权限/审计/GC/token/仓库列表） | 403 | 管理面恒为 admin-only；**组授予不能提权到 admin**（M4 有意设计） |
| 全局关匿名后的匿名读 | 401 + 挑战头（内容路径）/ 403（个别面） | 提供凭据 |
| npm 同版本重复 publish | 403 `Cannot modify pre-existing version '<v>' ...` | 升版本或先 unpublish |

### 404（不存在 / 有意不做的面）

| 场景 | 表现 | 处置 |
|---|---|---|
| 制品路径不存在 | 404（标准 miss 文案） | 核对路径；**命中仓库 `excludesPattern` 的下载与普通 miss 文案逐字相同**——先查仓配置再怀疑网络 |
| 未实现的端点族 | 404 + E-01：`/api/search/props|users|artifactory|pattern|badge`、`/api/v2/security/permissions/**`、`/api/export/**`、`/api/import/**`、`/api/system/storage/prune/**` | 有意不做（见下文不兼容清单），不是路由故障 |
| `?permissions` 于 virtual/remote 仓 | 400 `only supported on local repositories` | 该视图仅 local 仓 |

### 409（冲突 / 治理拒绝）

| 场景 | 表现 | 处置 |
|---|---|---|
| 路径不匹配仓库模式 | 409，message 含双 pattern：`rejected deployment of '<path>': the path does not match includesPattern '**/*.jar' (excludesPattern 'secret/**').` / `the path matches excludesPattern ...` | 改路径或改 `includesPattern`/`excludesPattern`（excludes 优先） |
| 删除被引用的组 | 409 `Cannot delete group '<g>': it is referenced by permission target(s): <t1>, <t2>. ...` | 先删/改列出的 target |
| GC 与 export/import 撞车 | 409 `gc rejected: export in progress ...`（message 含持锁进程） | 等待当前维护操作完成重试——**不是数据问题** |
| 客户端 checksum 与实测不符 | 409 `Checksum error for '<repo>/<path>': received '<x>' but actual is '<y>'` | 重传一致的构件；或仓配 `server-generated-checksums` |
| maven 仓关 SNAPSHOT/Release | 409 `handling of snapshots is disabled (handleSnapshots=false)` | 改仓配置或换仓 |

### 413（配额超限）

| 场景 | 表现 | 处置 |
|---|---|---|
| 仓库 `quotaBytes` 超限 | 413，message 含 used/quota 双值：`Repository '<repo>' quota exceeded: used 800 of 1024 bytes; the write to '<path>' needs 800 more bytes.` | 删旧腾空间（删后 used 即回落）或调高上限；被拒写**原子**——路径不留半截文件，**同内容重传不受误伤** |
| 五协议客户端侧 | docker `exit 1` + `denied:`；npm `E413`；mvn `status code: 413`；twine `HTTPError: 413` | 同上；每次拒绝落 `quota.exceeded` 审计（含 used/quota） |

## docker login 为什么不走控制台的会话？

控制台会话 cookie 的作用域是 `Path=/binflow`，而 docker 客户端按 Registry spec 固定向**根级 `/v2/...`** 发请求——cookie 结构性送不到。docker 面因此自持认证：`docker login <host>` 走 `/v2/token` 换取 Bearer token（对用户无感）。curl 手工协商与反代直通注意事项见 [Docker 接入](docker-registry.md#token-说明)。

## 高频场景：高 QPS 请用 Access Token

BinFlow 的口令哈希是 **argon2id（memory-hard）**——每请求 Basic 认证都要付出约 **64MB 瞬态内存**的工作集。高并发 Basic 认证（如 CI 集群大量短请求）实测会让服务进程 RSS 显著抬升（100 并发 Basic GET 可到数 GB，事后回落，非泄漏；匿名/Token 路径同负载零增长）。

**建议**：CI、脚本、监控探针等高频客户端一律使用 **API Token（Bearer）**——token 校验不触发 argon2，且天然免疫 CSRF。签发是 admin-only 操作（admin 可指名替目标用户签发，即「给 CI 账号发 token」）：

```bash
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/token \
  -d 'grant_type=client_credentials&username=ci-bot'
# 200 {"access_token":"<64hex>","token_id":...,"expires_in":...,"scope":...}
# access_token 仅此一次明文返回，妥善保存；使用：
curl -s -H "Authorization: Bearer <access_token>" $BASE/binflow/api/v1/storage/usage/<repo>
```

## M7 增补三问（RBAC / S3 续传 / step-up）

### readonly_admin 能写某个仓吗？给他授 write 的组也不行？

**不能**。readonly_admin 的内容面与仓库域求值**不查 permission targets**——「全域只读短路」是角色层的结构性设计（安全不变量：只读管理员还能删制品的矛盾配置在求值层被排除）。把 readonly_admin 加进带 `write` 的组**不会**让他能写：组合结果是**无效（静默无效果）**，不是报错，也不会有任何提示。管理面写操作（建仓/配额/GC〔含 dry-run〕/角色写等）同样恒 403。

需要写能力的用户应使用 `user` 角色 + permission target 授 `write`。见 [RBAC 角色与仓库级管理员](admin/rbac-roles.md)。

### S3 后端重启后，docker 上传 URL 为什么一律 404？

跨重启续传（M7）目前**仅本地 filestore 后端**。S3 后端的分块上传状态由 S3 multipart upload 服务端持有，BinFlow 对 S3 的续传查询恒答「会话不存在」——重启后旧 upload URL 一律 **404 `BLOB_UPLOAD_UNKNOWN`**，客户端从头重传（Q4 暂行口径，未纳入 M7）。这不是数据丢失：已 commit 的层与制品不受影响。本地 filestore 的三径（kill -9 / SIGTERM / compose restart）对称续传见 [Docker 接入指南](docker-registry.md#大层上传中断续传跨重启)。

### token 铸造报 401 step_up_required / step_up_invalid，怎么排查？

按四步走（详见 [step-up 指南](admin/token-step-up.md)）：

1. **开关**：`auth.token_step_up` 开了吗？默认 false——没开就不存在 step-up 报错。
2. **臂**：报错只可能来自 **web session 臂**。Basic / Bearer(token) / docker `/v2/token` 臂全部豁免——CI 里出现的 401 与 step-up 无关，查凭据本身。注意豁免的是 **admin 角色**：readonly_admin 的 session 自铸同样要过 step-up。
3. **错误码**：`step_up_required` = 所欠凭据缺失（本地/LDAP 缺 `step_up_password`、OIDC 缺 `step_up_grant`；**交了错腿的凭据也算缺失**）；`step_up_invalid` = 口令错 / grant 过期（TTL 默认 300s）/ **grant 已用过**（单次消费即删，第二次铸造即烧）/ 服务重启（grant 台账在进程内存）。
4. **另一种 401**：文案为 `The user: '...' can only create user token with expires in larger than 0 and smaller than 31536000 seconds ...` 的 401 是 **TTL 护栏**（非 admin 上限 365d），与 step-up 无关——step-up 已通过，调低 `expires_in` 即可。

## M9 增补两问（用户删除 / npm 复制凭据）

### 删除用户的脚本第二次跑同一条 DELETE，404 是失败吗？

**不是失败，是终态**。M9 起 `DELETE /api/security/users/{name}` 为**有意非幂等**：首次成功 200（纯文本 `The user: '<name>' has been removed successfully.`），对象已删后再发**确定性 404**（`User not found` 文本体）——调用方应把第二次 404 读作「已删除」，不要重试、不要当告警（Artifactory「重复删视为成功」的幂等形态是其并发窗口产物，BinFlow 不复刻）。四道护栏（不存在 404 / 内置 admin / 最后一个 admin / 自删，全 400）与级联语义见[治理指南 · 删除用户](admin/governance.md#删除用户m9-起)。

### 给 npm 复制任务配目标仓凭据，要授 delete 吗？

**不用**。复制引擎只发「目标所缺版本的单版本发布文档」与单 tag PUT，从不整包覆写——与 CI 连发同口径：`read` + `write` 即可（M8 起追加新版本仅需 write）；改既有版本数据（deprecate/篡改）才需要 `delete`，而复制引擎构造不出这种写（同版本不同数据按 first-write-wins 目标幸存）。见[npm 接入 · 发布权限语义](integrations/npm.md#发布权限语义m8-起)。

## M4 有意不兼容清单（里程碑级汇总）

从 Artifactory 迁移时的差异点（各域细节见对应指南；M1~M3 清单见 [remote/virtual 管理](admin/remote-virtual.md#m3-有意不兼容清单汇总)）：

| 不做项 | 表现 | 归属 |
|---|---|---|
| 组的 admin 位 | 组只能授 read/write/delete；admin 组成员的非 admin 用户对管理面仍 403 | M4 定案（防组内自提权） |
| `/api/v2/security/permissions/**`（Artifactory v2 权限 API） | 404——BinFlow 权限面是 `/api/v1/permissions` | M4 |
| Artifactory 搜索族（props/users/artifactory/pattern/badge、AQL） | 404——M4 仅 name 子串 + checksum 精确 | M4；gavc/props 后续评估 |
| `/api/system/storage/prune/**` | 404——空间回收走 GC | M4 |
| REST export/import | 404——备份恢复仅 CLI | M4 定案（高危操作带外） |
| 异步 GC 作业 / GC 状态端点 | 同步执行、无 `GET /api/v1/system/gc`（上次运行查审计 `gc.run`） | M4；异步框架 M6+ |
| 审计 CSV 导出 / token 列表 UI / `--tar` 备份单文件 | 控制台不渲染；CLI 显式报未实现 | M4 P2 债务 |
| SAML 登录、洞察报表、漏洞扫描 | 不做（产品 Non-goal）；OIDC/LDAP 登录 M6 已交付（见[专题指南](guides/oidc-config.md)/[LDAP](guides/ldap-config.md)） | SAML/报表/扫描永不 |

## 从 Artifactory 迁移对照表

概念一一对应，术语不变；**逐任务的控制台操作路径对照**（建仓/建用户/配权限/找制品/Set Me Up/GC/备份……）见 [Artifactory → BinFlow 操作路径对照表](artifactory-path-map.md)：

| Artifactory | BinFlow | 说明 |
|---|---|---|
| local / remote / virtual 仓 | 同名 rclass 三型 | 语义一致；建仓走 `PUT /api/repositories/{key}`（重复 PUT 为更新） |
| repo key / node / checksum | 同名 | node = 制品节点；checksum 族 sha256/sha1/md5 |
| deployment / resolution | 上传 / 解析 | UI 与文档保留 deployment 原词 |
| permission target、include/exclude patterns | 同名同构 | M4 起 principals 支持 groups；`?permissions` 视图同形（key=主体名、value=r/w/d 字母集） |
| groups / users / access tokens | 同名 | 组删除的 409 保护、`Unable to find group by name '<g>'.` 文案同款；用户删除（M9）三护栏 + 级联撤权、**重复删除 404**（Artifactory 视为成功——幂等 vs 有意非幂等） |
| （无内置实例级只读管理员；管理面 admin 为布尔） | `adminRole` 三值角色（`user`/`readonly_admin`/`admin`） | M7 起；`admin=true ⇔ adminRole=admin` 两写法等价。readonly_admin 为 BinFlow 自有（Artifactory 近似能力 = target 只授 read，无管理面只读） |
| `binflow_session` 控制台会话 | （本产品新增） | server-side session + CSRF Origin 校验；Artifactory 无对应面 |
| System YAML / storage GC / backup | `binflow.yaml` / `POST /api/v1/system/gc` / `export`/`import` CLI | GC 语义（mark-sweep + grace=mtime）同构 |

迁移注意事项（高频四问）：

1. **「我的 404 为什么在 Artifactory 是 200？」**——先查 BinFlow 仓库的 `excludesPattern`（拦截下载与 miss 同文案）与 remote 仓负缓存/assumed-offline（`X-Binflow-Cache` / `X-Binflow-Upstream-Error` 响应头）。
2. **「docker push 为什么报错别的协议都好？」**——docker 固定根级 `/v2`，前置反代必须原样直通（不能 rewrite 进 `/binflow`）；明文 HTTP 需配 daemon 的 insecure-registries。
3. **「脚本 401 但浏览器正常？」**——浏览器是会话 cookie，脚本用 Basic/token；确认没有把控制台 cookie 混进 CI（cookie 过期不受你控制）。
4. **「收藏夹里的 BinFlow 控制台旧路径失效了吗？（M8 引入 / M9 收紧）」**——M8 路由重排后旧路径曾**自动重定向**到新路径（如 `/security/users` → `/admin/security/users`）；**M9 起重定向已移除**（ADR-0029 Q3 终裁），旧路径直链落 404 页（提供「回主页」链接）——请按映射表更新书签，见[控制台指南 · 旧路径 → 新路径](console.md#旧路径--新路径m9-起不再重定向)。

## 排障信息收集

复现问题时附上这三样，基本可以定位大多数故障：

```bash
curl -s $BASE/binflow/api/system/version                     # 版本/修订
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?limit=20"  # 最近审计（谁在何时动了什么）
curl -s -D - -o /dev/null $BASE/binflow/<repo>/<path>        # 完整响应头（缓存/来源/校验和头都在）
```

服务端日志为结构化 JSON（level/msg/字段），无堆栈噪音；升级与已知边界见各指南「有意不兼容」小节。

## Docker 快速通道

### 如何推送镜像

```bash
# 1. 建仓（admin 操作）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/docker-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"docker"}' \
  -o /dev/null -w '%{http_code}\n'      # 200

# 2. 登录
echo "$ADMIN_PW" | docker login $REG -u admin --password-stdin

# 3. 推送（全名格式：<host>/<repoKey>/<image>:<tag>）
docker build -t $REG/docker-local/acme/app:v1 .
docker push $REG/docker-local/acme/app:v1
```

### 如何拉取

```bash
docker pull $REG/docker-local/acme/app:v1
docker run --rm $REG/docker-local/acme/app:v1
```

匿名拉取默认开启（`anonymous_access: true`）；`docker logout` 后 `docker pull` 仍可用（匿名 token 通道）。push **永远需要认证**。

### 认证问题

- `docker login` 成功后 credential store 记录凭据，push/pull 自动使用。
- 登录失败（口令错）→ `unauthorized: authentication required`，退出码 1。
- Docker 面认证走 `/v2/token` 分发 Bearer token（distribution token 协议），与 `/binflow` 管理面的 Basic/token 认证是不同的认证入口——同一用户、同一权限模型，但 token 形态不同。

### Tag 管理

```bash
# 查看所有 tag
curl -su admin:$ADMIN_PW $REG/v2/docker-local/acme/app/tags/list
# {"name":"docker-local/acme/app","tags":["v1","latest"]}

# 由 manifest 摘要删除 tag
DIGEST=$(docker manifest inspect --insecure $REG/docker-local/acme/app:v1 | jq -r '.config.digest')
# 通过 manifest digest 删除
curl -su admin:$ADMIN_PW -X DELETE $REG/v2/docker-local/acme/app/manifests/$DIGEST
```

> Tag 删除不支持 `DELETE /v2/<name>/manifests/<tag>`（spec 禁止），只能通过 manifest digest 删除。

### 明文 HTTP 问题

Docker daemon 对非 localhost 地址默认强制 HTTPS。遇到 `http: server gave HTTP response to HTTPS client`：

- `localhost:8080` 目标：**零配置**，daemon 默认把 `127.0.0.0/8` 视为 insecure
- 远程主机：配置 daemon 的 insecure-registries 列表（Docker Desktop → Settings → Docker Engine；Linux → `/etc/docker/daemon.json`）
- 或在前面加 TLS 反代（nginx/traefik），见 [Docker 接入指南](docker-registry.md#前置反向代理可选)

## 通用制品的上传与下载

### 上传（curl 单行）

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/generic-local/a/b/w.bin \
  --data-binary @w.bin -o /dev/null -w '%{http_code}\n'
# 201

# 带 checksum 声明
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/generic-local/x/ok.bin \
  --data-binary @ok.bin -H "X-Checksum-Sha256: $(shasum -a 256 ok.bin | cut -d' ' -f1)" \
  -o /dev/null -w '%{http_code}\n'
```

### 下载

```bash
curl -s -o dl.bin $BASE/binflow/generic-local/a/b/w.bin
# 匿名可读（默认 anonymous_access: true）

# 校验和头查看
curl -s -D - -o /dev/null $BASE/binflow/generic-local/a/b/w.bin | grep -i checksum
```

### 校验和验证

下载后对账：

```bash
curl -s -D headers.txt -o dl.bin $BASE/binflow/generic-local/a/b/w.bin
grep -i 'x-checksum-sha256' headers.txt | tail -c 65   # 服务端实测摘要
shasum -a 256 dl.bin | cut -d' ' -f1                    # 本地实测摘要
```

## Maven 客户端接入速查

### settings.xml 最小配置

```xml
<settings>
  <servers>
    <server>
      <id>binflow</id>                            <!-- 必须与 altDeploymentRepository 首段一致 -->
      <username>admin</username>
      <password>$ADMIN_PW</password>
    </server>
  </servers>
</settings>
```

### 发布

```bash
mvn -B -DskipTests deploy \
  -DaltDeploymentRepository=binflow::default::http://localhost:8080/binflow/maven-local
```

### 解析

```xml
<!-- 在项目的 pom.xml 中 -->
<repositories>
  <repository>
    <id>bf</id>
    <url>http://localhost:8080/binflow/maven-local</url>
  </repository>
</repositories>
```

### 全量收口（mirror 形态）

```xml
<settings>
  <mirrors>
    <mirror>
      <id>binflow</id>
      <mirrorOf>*</mirrorOf>
      <url>http://localhost:8080/binflow/maven-virtual</url>
    </mirror>
  </mirrors>
</settings>
```

`mirrorOf: *` 接管一切流量（含插件解析），virtual 必须含一个代理 Maven Central 的 remote 成员。详见 [Maven 接入](integrations/maven.md)。

## npm 客户端接入速查

### .npmrc 最小配置

```ini
registry=http://localhost:8080/binflow/api/npm/npm-local/
//localhost:8080/binflow/api/npm/npm-local/:_auth=<base64 of admin:口令>
always-auth=true
```

`_auth` 生成：`printf 'admin:%s' "$ADMIN_PW" | base64`。

> npm 10 必知：项目级 `.npmrc` 里裸 `_auth=` 会被 npm 拒绝，必须 `//<host>/<路径>/:_auth` 限定形态。

### 发布

```bash
npm publish        # 退出码 0；服务端 201 {"success":true}
npm whoami         # 期望 admin
```

### 安装

```bash
npm install demo-pkg
npm install @acme/util
```

匿名读默认开（`npm install` 免凭据）。每个项目目录都需要自己的 `.npmrc`（registry 配置不继承，缺省走 npmjs）。

详见 [npm 接入](integrations/npm.md)。

## PyPI 客户端接入速查

### pip.conf 安装侧

```ini
[global]
index-url = http://localhost:8080/binflow/api/pypi/pypi-local/simple
```

```bash
pip install demo-pkg
```

### .pypirc 发布侧

```ini
[distutils]
index-servers = binflow

[binflow]
repository = http://localhost:8080/binflow/api/pypi/pypi-local
username = admin
password = <你的管理员口令>
```

```bash
pip wheel . -w dist/
twine upload --repository binflow dist/*
```

`simple/` 尾斜杠不能省；包名大小写无关（PEP 503 归一化）。详见 [PyPI 接入](integrations/pypi.md)。

## 部署方式选择指南

| 场景 | 推荐方式 | 说明 |
|---|---|---|
| 本地开发/评估 | 单二进制 | `binflow-server -c binflow.yaml`，零依赖，3 秒启动 |
| 集成环境（单机） | docker-compose | 声明式编排，伴随 postgres 等配套服务 |
| 离线 / air-gapped | 离线安装包 | 预先打包二进制 + 配置模板 + 安装脚本 |
| 小组内网 | Docker 镜像 | distroless/alpine 双变体，各镜像仓库均可用 |
| Kubernetes 生产 | Helm Chart | 持久化、ingress、配置管理声明式 |
| 裸机 / 长期运行 | systemd | 服务守护、日志重定向、自动重启 |
| 无 K8s 的机群 | 原生 K8s 清单 | 不上 Helm 时直接用 Deployment + Service + PVC |

**选择关键**：
- 需要持久化、配置管理 → docker-compose 或 Helm
- 需要最大可移植性 → 单二进制
- 需要离线场景 → 离线安装包
- 需要服务守护 → systemd

所有部署方式共享同一份 `binflow.yaml` 配置模型，切换方式只需改部署面，应用层配置不变。

## 常见报错速查

### 401 认证类

| 错误 | 原因 | 解决 |
|---|---|---|
| `invalid credentials`（制品域） | 口令错或用户不存在 | 核对凭据；连续失败落 `login.failed` 审计 |
| `authentication required`（docker 面） | 未登录或口令错 | `docker login` 重试 |
| docker push 401 | 未登录或 token 过期 | `docker login`；token 过期无法续期，重新登录 |
| 脚本 401 但浏览器正常 | 浏览器用会话 cookie，脚本用 Basic/token | 确认脚本用 Basic/token 而非混用 cookie |
| 已吊销 token 401 | token 已被管理面吊销 | 签发新 token |
| 会话 cookie 401 | 会话过期或已登出 | 重新登录控制台 |
| 401 `step_up_required`（token 铸造，M7） | step-up 开启 + 非 admin session 臂缺二次凭据（或交了错腿凭据） | 按腿补 `step_up_password` / `step_up_grant` |
| 401 `step_up_invalid`（token 铸造，M7） | 口令错 / grant 过期 / **grant 已用过** / 服务重启丢台账 | grant 一次性，重走获取流 |

### 403 权限类

| 错误 | 原因 | 解决 |
|---|---|---|
| `permission denied`（制品上传） | 无 write 权限 | 找 admin 加 permission target |
| 覆盖已有制品 403 | 覆盖 = 删除旧文件，需 delete 权限（npm 追加新版本例外，M8 起仅 write） | 加 delete 权限；npm 见[发布权限语义](integrations/npm.md#发布权限语义m8-起) |
| 管理面 403（非 admin 用户） | 管理面恒为 admin-only | 组授予不能提权到 admin；换 admin 账号 |
| 控制台 + 跨站 Origin 写 403 | CSRF Origin 防线 | 同源页面操作；curl/CI 不受影响 |
| npm 同版本重复 publish 403 | `Cannot modify pre-existing version` | 升版本或先 unpublish（连发新版本 M8 起仅需 write——若你的 CI 连发 403，实例为旧版，见[npm 接入](integrations/npm.md#发布权限语义m8-起)） |
| 全局关匿名后的匿名读 | 403 或 401 + 挑战 | 提供凭据 |

### 404 不存在类

| 错误 | 原因 | 解决 |
|---|---|---|
| docker push/pull 404 + `NAME_UNKNOWN` | 单段 name（只有 repo key 无镜像名）或仓库未建 | 用 `<repoKey>/<image>:<tag>` 全名 |
| 制品路径 404 | 路径不存在或命中 excludesPattern | 先查仓库 `excludesPattern` 再怀疑网络 |
| 未实现的端点 404 | 搜索族 `/api/search/props` 等 | 有意不做，非路由故障 |
| docker 面 `GET /v2/<name>/referrers/` 404 | OCI referrers API 不做 | 使用 oras 自动回退 |

### 409 冲突类

| 错误 | 原因 | 解决 |
|---|---|---|
| 路径不匹配 includesPattern 或命中 excludesPattern | 仓库模式拒绝了该路径 | 改路径或改仓库模式配置 |
| `Checksum error ... received '<x>' but actual is '<y>'` | 客户端声明摘要与内容不符 | 重传一致的构件；或改仓配 `server-generated-checksums` |
| 删除组 409 `referenced by permission target(s)` | 组仍被权限引用 | 先删/改引用方 target |
| GC 与 export 撞车 409 | 维护锁互斥 | 等当前操作完成重试 |
| maven snapshot/release 禁写 409 | `handleSnapshots` / `handleReleases` 为 false | 改仓配置或换仓 |

### 413 配额

| 错误 | 原因 | 解决 |
|---|---|---|
| `Repository '...' quota exceeded` | 仓库配额超限 | 删旧腾空间或调高上限 |
| docker push 被拒 `exit 1` + `denied:` | 同上 | 同上 |
| npm E413 | 同上 | 同上 |

### 网络/基础设施

| 错误 | 原因 | 解决 |
|---|---|---|
| `http: server gave HTTP response to HTTPS client`（docker） | daemon 未把目标地址列入 insecure-registries | 配置 daemon 的 insecure-registries 或加 TLS 反代 |
| docker buildx push `unauthorized` | builder 容器缺登录态或缺 `http = true` 配置 | 宿主先 `docker login`；buildkitd.toml 配 `[registry."<REG>"] http = true` |
| helm push 401 | 明文 HTTP 下 `helm registry login` 不可用 | 写 `HELM_REGISTRY_CONFIG` 凭据文件 |
| maven `Could not find artifact`（修好配置后仍报） | 本地仓 `.lastUpdated` 负缓存 | 清 `maven.repo.local` 或 `mvn -U` |
| mirror 形态下默认插件解析失败 | virtual 无 Central 成员 | virtual 加代理 Maven Central 的 remote 成员 |
| npm install 报 404 / 装到了公网同名包 | 当前目录缺 `.npmrc` | 每个项目目录放 `.npmrc` |
| PyPI upload 400 `unknown action 'submit'` | multipart `:action` 不是 `file_upload` | 用 twine（自动携带正确 action） |
| `pip install` 404（包明明存在） | index-url 少了 `/simple` 后缀 | 核对 URL 形态 |

## 性能与资源

### 高 QPS 请用 Access Token

BinFlow 的口令哈希是 **argon2id（memory-hard）**——每请求 Basic 认证都要付出约 **64MB 瞬态内存**的工作集。100 并发 Basic GET 可让 RSS 显著抬升到数 GB（事后回落，非泄漏）。匿名/Token 路径同负载零增长。

**建议**：CI、脚本、监控探针等高频客户端一律使用 **API Token（Bearer）**——token 校验不触发 argon2 计算，且天然免疫 CSRF。

### 存储空间

- 去重生效：相同内容的 blob 只存一份，多个仓库/路径引用同一 blob 不增加存储。
- 删除制品后 blob 可能仍被其它路径引用——不被引用的孤儿 blob 由 GC 回收。
- 查看用量：`GET /binflow/api/v1/storage/usage/{repo}` → `{usedBytes, quotaBytes}`。

### 备份

- 在线 export：`binflow-server export -c binflow.yaml --output /backup/date`
- 恢复：`binflow-server import -c binflow.yaml --input /backup/date --verify full`
- 导出与 GC 互斥（同一 data 目录维护锁），建议排程错开。

## 日志与诊断

服务端日志为结构化 JSON（level/msg/字段），无堆栈噪音。快速诊断三件套：

```bash
# 1. 版本信息
curl -s $BASE/binflow/api/system/version

# 2. 最近审计（谁在何时动了什么）
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?limit=20"

# 3. 完整响应头（缓存/来源/校验和头都在）
curl -s -D - -o /dev/null $BASE/binflow/<repo>/<path>
```

## 从 Artifactory 迁移三步走

1. **概念对齐**：仓库模型（local/remote/virtual）、权限模型（permission target × path × principal）、checksum 去重——概念一一对应，术语不变。
2. **URL 映射**：`/artifactory/api/...` → `/binflow/api/...`；`/artifactory/<repo>/<path>` → `/binflow/<repo>/<path>`；docker 端 `/v2/` 地址不变。
3. **差异复核**：见上文「M4 有意不兼容清单」与各协议指南的「有意不兼容」小节——404 的搜索端点、404 的 REST export/import、组无 admin 位是三件最高频的差异点。

工具与实证：定义/用户/token 台账批量搬迁走 [bf-migrate](guides/migrate-artifactory.md)；真实 Artifactory OSS 源（7.84.10 + PostgreSQL）的整场迁移实录与差异清单见[附录 V28](admin/real-env-appendix.md#v28真实-artifactory-迁移实腿dep用户环境)。
