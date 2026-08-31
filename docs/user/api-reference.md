---
title: API 参考
sidebar_position: 70
---

# API 参考

> 适用版本：M1~M13（端点引入里程碑标注于各表；M7 增补：用户角色字段 `adminRole`、permission target 动作 `manage`、docker 上传状态腿跨重启、token 铸造 step-up 可选门；**M9 增补**：usage 批量端点、users 列表加宽/enabled 回显/DELETE、groups `?includeUsers`、permissions `?filter=manage`——速览见[下文](#m9-增补速览)；**M11 增补**：认证配置面（含 SAML SP 证书三端点，T-331）、GPG keypair 族、cleanup 引擎、四包型 reindex 族、smart remote 两字段生效、MPU 面整体翻转（ADR-0039）与 cargo remote/virtual 仓型——见[M11 增补速览](#m11-增补速览t-328)；**M12 增补**：制品操作族（copy/move + 归档族）与 trash REST 族（NuGet v2 全路由/v3 代理属协议接入面，见 [NuGet 接入](integrations/nuget.md)）——见[M12 增补速览](#m12-增补速览t-347a)；**M13 增补**：webhook 订阅七端点族（`/event/api/v1`）+ `GET /api/v1/system/settings` 旋钮回显 + remote 仓 `chartsBaseUrl` 字段——见[M13 增补速览](#m13-增补速览t-375)）。Artifactory 兼容端点基于 REST 逆向规格 `docs/reverse/rest-api.md`（置信度高）。
> **M10 增补（T-293 部分回写，2026-08-26）**：`?properties` 族反转为 **GET/PUT/DELETE 三动词**（POST 增量动词不做——其余动词落 404 冻结姿态；原 M5 期表格把属性动词标为 M4/M1 系陈旧勘误）；上传路径 matrix 参数 M10 生效。M10 其余新端点（license/addons/uploads、Go/NuGet/Cargo 接入面）已随 T-296 补齐——速览见[下文](#m10-新增端点速览t-296)。
> BinFlow 自有端点以 `/api/v1` 前缀标记。

BinFlow 的 API 分为两个面：

1. **制品的容路径**（无 `/api` 前缀）：`/binflow/<repoKey>/<path>`——上传、下载、删除制品，各协议客户端走这里。
2. **管理面 API**（`/binflow/api/*`）：仓库 CRUD、用户/组/权限、审计、GC、Token、搜索、系统信息。
3. **Docker Registry 根级平面**（`/v2/*`）——独立路由，见[Docker 接入指南](docker-registry.md)。
4. **Webhook 事件面**（`/binflow/event/api/v1/*`，M13）——订阅 CRUD/试发/排障，见[Webhook 使用指南](admin/webhooks.md)。

---

## 兼容端点总表

按 PRD 兼容域标注：E（通用）、DE（Docker）、ME（Maven）、NE（npm）、PE（PyPI）、SE（安全）、SR（搜索）。

### E: 通用制品域

| 方法 | 路径 | 语义 | 里程碑 |
|---|---|---|---|
| PUT | `/binflow/{repoKey}/{path}` | 上传文件（body 为内容），checksum 头支持；**matrix 参数（`;k=v` 尾随成对序列剥离为部署属性）M10 起生效**——非成对 `;` 维持文件名字面 | M1/M10 |
| PUT | `/binflow/{repoKey}/{path}/` | 创建目录（尾斜杠） | M1 |
| PUT | `/binflow/{repoKey}/{path}.sha1\|.md5\|.sha256` | 上传校验和旁车文件 | M1 |
| GET | `/binflow/{repoKey}/{path}` | 下载文件（支持 Range/If-None-Match/ETag） | M1 |
| HEAD | `/binflow/{repoKey}/{path}` | 文件元信息（响应头同 GET 无 body） | M1 |
| DELETE | `/binflow/{repoKey}/{path}` | 删除文件或目录树 | M1 |
| DELETE | `/binflow/api/storage/{repoKey}/{path}?properties=k1,k2[&recursive=1]` | 删属性（幂等，不存在的键 204；`properties=*` 全删；folder + `recursive=1` 递归） | M10 |
| PUT | `/binflow/api/storage/{repoKey}/{path}?properties=k=v1,v2[&recursive=1]` | 写属性——**merge 语义**：同名键值集整体替换、异名键保留；node 须存在（404） | M10 |
| GET | `/binflow/api/storage/{repoKey}/{path}` | 取 FileInfo / FolderInfo JSON | M1 |
| GET | `/binflow/api/storage/{repoKey}/{path}?properties=K1,K2*` | 取属性（key 过滤 + 尾 `*` 通配；无命中 = 200 `{"properties":{}}`——BinFlow 自有裁定，非 Artifactory 的 404；node 不存在 = 404） | M10 |
| GET | `/binflow/api/storage/{repoKey}/{path}?stats` | 取下载统计 | M1 |
| GET | `/binflow/api/storage/{repoKey}/{path}?lastModified` | 取目录最新修改时间 | M1 |
| GET | `/binflow/api/storage/{repoKey}/{path}?permissions` | 取有效权限视图（admin only，仅 local 仓） | M4 |
| GET | `/binflow/api/storage/{repoKey}?list` | 流式文件清单（仅认证用户） | M1 |

### DE: Docker 域（独立 /v2 路由）

| 方法 | 路径 | 语义 | 里程碑 |
|---|---|---|---|
| GET | `/v2/` | API 版本检查（401 挑战，不随匿名开关变化） | M2 |
| GET | `/v2/_catalog` | 仓库目录 | M2 |
| GET | `/v2/{name}/tags/list` | 标签列表（空标签集返回 `"tags":null`） | M2 |
| GET | `/v2/{name}/manifests/{ref}` | 取 manifest（tag 或 digest） | M2 |
| PUT | `/v2/{name}/manifests/{ref}` | 上传 manifest（Content-Type 透传不白名单） | M2 |
| DELETE | `/v2/{name}/manifests/{digest}` | 删除 manifest（by-digest 仅） | M2 |
| POST | `/v2/{name}/blobs/uploads/` | 启动 blob 上传会话 | M2 |
| GET | `/v2/{name}/blobs/uploads/{uuid}` | 上传状态查询（**204 + `Range: 0-<offset-1>`** 权威断点；M7 起跨重启存活，续传见 [Docker 接入指南](docker-registry.md#大层上传中断续传跨重启)） | M2/M7 |
| PATCH | `/v2/{name}/blobs/uploads/{uuid}` | 上传 blob 分片（`Content-Range` 起点错位 → 416 空 body + 权威 `Range`） | M2 |
| PUT | `/v2/{name}/blobs/uploads/{uuid}` | 完成 blob 上传（`?digest=sha256:...`） | M2 |
| GET | `/v2/{name}/blobs/{digest}` | 下载 blob | M2 |
| HEAD | `/v2/{name}/blobs/{digest}` | blob 存在检测 | M2 |
| DELETE | `/v2/{name}/blobs/{digest}` | **405 UNSUPPORTED**—blob 删除仅 GC | M2 |
| GET/POST | `/v2/token` | Docker 认证 token 端点（distribution token 协议） | M2 |
| GET | `/v2/{name}/referrers/` | **404**—OCI referrers API 不做 | M2 |

### ME: Maven 域（内容路径）

| 方法 | 路径 | 语义 | 里程碑 |
|---|---|---|---|
| PUT | `/binflow/{repoKey}/{GAV路径}` | 部署 Maven 构件（layout 严格校验） | M3 |
| GET | `/binflow/{repoKey}/{GAV路径}` | 解析构件（含 `maven-metadata.xml`） | M3 |
| GET | `/binflow/{repoKey}/{GAV路径}.sha1\|.md5\|.sha256` | 取校验和 | M3 |

### NE: npm 域

| 方法 | 路径 | 语义 | 里程碑 |
|---|---|---|---|
| GET | `/binflow/api/npm/{repoKey}/-/${pkg}` | packument（包元数据） | M3 |
| PUT | `/binflow/api/npm/{repoKey}/-/${pkg}` | 发布包（十步 packument 链） | M3 |
| DELETE | `/binflow/api/npm/{repoKey}/-/${pkg}?rev=...` | unpublish（移除指定版本） | M3 |
| GET | `/binflow/{repoKey}/{name}/-/{name}-{v}.tgz` | 下载 tarball（内容路径直取） | M3 |
| PUT | `/binflow/{repoKey}/{name}/-/{name}-{v}.tgz` | **405**—npm 域仅认 packument PUT | M3 |

### PE: PyPI 域

| 方法 | 路径 | 语义 | 里程碑 |
|---|---|---|---|
| GET | `/binflow/api/pypi/{repoKey}/simple/` | 包列表（PEP 503/629） | M3 |
| GET | `/binflow/api/pypi/{repoKey}/simple/{pkg}/` | 单包索引页（HTML + JSON，Accept 驱动） | M3 |
| POST | `/binflow/api/pypi/{repoKey}/` | 上传（multipart `:action=file_upload`） | M3 |
| GET | `/binflow/api/pypi/{repoKey}/packages/{name}/{ver}/{file}` | 下载构件（twine 回显 URL） | M3 |

### SE: 安全域

| 方法 | 路径 | 语义 | 里程碑 |
|---|---|---|---|
| GET | `/binflow/api/security/users` | 用户列表（**M9 加宽**：条目增 `email`/`adminRole`/`enabled`/`groups`〔恒渲染，空组 `[]`〕，一次请求含全部列表所需字段） | M1 |
| GET | `/binflow/api/security/users/{name}` | 用户详情（无口令字段；M7 起回显 `adminRole`，**M9 起恒回显 `enabled`**） | M1 |
| PUT | `/binflow/api/security/users/{name}` | 创建或替换用户（create-or-replace，两态 201；M7 起 body 可含 `adminRole`，仅 admin 可写） | M1 |
| POST | `/binflow/api/security/users/{name}` | 部分更新用户（email/password/admin/groups/adminRole/enabled——`enabled` 为指针语义，显式 `false` 禁用登录） | M4 |
| DELETE | `/binflow/api/security/users/{name}` | **删除用户**（M9：三护栏 400、同事务级联、200 纯文本；重复删除**确定性 404**——见[M9 增补速览](#m9-增补速览)） | M9 |
| PUT | `/binflow/api/security/password` | 当前用户改密 | M1 |
| POST | `/binflow/api/security/users/authorization/changePassword` | 别名改密端点 | M4 |
| GET | `/binflow/api/security/groups` | 组列表 | M4 |
| GET | `/binflow/api/security/groups/{name}` | 组详情（**M9 增 `?includeUsers=true`**：响应附 `userNames: []`；字面 `true` 才开，其余拼法回无参形态；groups 列表端点不加宽） | M4 |
| PUT | `/binflow/api/security/groups/{name}` | 创建或更新组（创建 201 / 更新 200） | M4 |
| POST | `/binflow/api/security/groups/{name}` | 改描述 | M4 |
| DELETE | `/binflow/api/security/groups/{name}` | 删组（被 target 引用 → 409） | M4 |
| POST | `/binflow/api/security/token` | 签发 Access Token（admin 为任意用户签发；非 admin 限本人——M6 起；M7 起实例可开 step-up 二次认证，见 [step-up 指南](admin/token-step-up.md)） | M1 |
| POST | `/binflow/api/security/token/revoke` | 吊销 Token（admin only） | M1 |
| POST | `/binflow/api/v1/permissions` | 创建 Permission Target（create-or-replace；M7 起动作集含 `manage`，manage 持有者可编辑覆盖集内的 target） | M1 |
| GET | `/binflow/api/v1/permissions` | 列出 Permission Targets（M7 起 principals 回显 `manage` 位；**M9 增 `?filter=manage`**：manage 持有者可达的覆盖集内 target 子集——admin/readonly_admin 带参与无参响应逐字节一致；未知 filter 值 400） | M4 |
| DELETE | `/binflow/api/v1/permissions/{name}` | 删除 Permission Target（**204** 无 body；被删 target 的 repo 集取自存量行，manage 覆盖越界 → 403） | M1 |

### SR: 搜索域

| 方法 | 路径 | 参数 | 语义 | 里程碑 |
|---|---|---|---|---|
| GET | `/binflow/api/search/artifact` | `name=`（必填）、`repos=a,b` | 按名称子串搜索（SQL LIKE，权限过滤） | M4 |
| GET | `/binflow/api/search/checksum` | `sha1=/md5=/sha256=`（至少一）、`repos=a,b` | 按 checksum 精确搜索 | M1 |
| GET | `/binflow/api/search/props\|users\|artifactory\|pattern\|badge` | — | **404** 有意不做 | M1 |

### SR: 仓库管理域

| 方法 | 路径 | 语义 | 里程碑 |
|---|---|---|---|
| GET | `/binflow/api/repositories` | 仓库列表（admin / readonly_admin） | M1 |
| GET | `/binflow/api/repositories?type=&packageType=` | 过滤列表 | M1 |
| GET | `/binflow/api/repositories/{key}` | 单仓配置（M7 起 manage 持有者对覆盖仓亦可读） | M1 |
| PUT | `/binflow/api/repositories/{key}` | 建仓（创建）/ 替换既有仓（M7 起替换臂与配额字段对覆盖仓的 manage 持有者开放；**建仓臂仍 admin only**） | M1 |
| POST | `/binflow/api/repositories/{key}` | 改仓（更新配置，含 quotaBytes 配额写；M7 起 manage 持有者同上） | M1 |
| DELETE | `/binflow/api/repositories/{key}` | 删仓（含可选 `?deleteContent`；admin only，不下放） | M1 |

### 系统端点

| 方法 | 路径 | 语义 | 里程碑 |
|---|---|---|---|
| GET | `/binflow/api/system/ping` | 存活探测（免认证） | M1 |
| GET | `/binflow/api/system/version` | 版本信息（免认证） | M1 |
| GET | `/binflow/api/v1/health` | 健康面板（admin / readonly_admin） | M1 |
| GET | `/binflow/api/v1/storage/stats` | 全实例存储统计（admin / readonly_admin） | M1 |
| GET | `/binflow/api/v1/storage/usage/{repo}` | 单仓配额用量（admin / readonly_admin / 对该仓有 `read` **或** `manage` 授权者——M7 起 manage ∨-臂） | M4 |
| GET | `/binflow/api/v1/storage/usage` | **批量用量**（M9：bare array，行形与单仓同构；按调用者可见集过滤；`?repos=` 点名、`?include=counts` 附 `nodeCount`/`updatedAt`——见[M9 增补速览](#m9-增补速览)） | M9 |
| GET | `/binflow/api/v1/audit` | 审计日志查询（admin / readonly_admin） | M4 |
| POST | `/binflow/api/v1/system/gc` | 触发 GC（admin only；readonly_admin 403 **含 dry-run**） | M4 |

### 会话端点

| 方法 | 路径 | 语义 | 里程碑 |
|---|---|---|---|
| POST | `/binflow/api/v1/session` | 登录（JSON 或 form；免认证） | M4 |
| GET | `/binflow/api/v1/session` | Whoami（当前会话信息） | M4 |
| DELETE | `/binflow/api/v1/session` | 登出（吊销会话） | M4 |

### 已保留 / 未实现路径

| 路径 | 状态 | 说明 |
|---|---|---|
| `/binflow/api/v2/**` | 404 | Artifactory v2 权限 API 不实现（用 `/api/v1/permissions`） |
| `/binflow/api/export/**`, `/binflow/api/import/**` | 404 | 备份恢复仅 CLI |
| `/binflow/api/system/storage/prune/**` | 404 | 空间回收走 GC |
| `/binflow/v2/**` | 404 | Docker 端点不走 `/binflow` 前缀（见 /v2 根级例外） |

---

## M9 增补速览

六条 M9 端点/加宽的关键 wire 事实（全部在 HEAD 构建的 scratch 实例上 curl 实测，2026-08-25）：

### E1 · `GET /api/v1/storage/usage`（usage 批量）

- **bare array**（无信封、无分页；空可见集 `200 []` 恒非 null）；行形与单仓端点逐字段同构：`{"repo","usedBytes","quotaBytes"}`。
- **可见集**：admin / readonly_admin 全量；普通 user = 对该仓有 `read` **或** `manage` 的子集（与单仓端点同一 ∨-臂）；匿名 401。点名未知名的仓与点名无权限的仓**同形静默缺失**（无存在性信号）。

```bash
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/storage/usage
# [{"repo":"g-local","usedBytes":10,"quotaBytes":0}, ...]

curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/storage/usage?repos=g-local,no-such"   # 未知名静默缺失
# [{"repo":"g-local","usedBytes":10,"quotaBytes":0}]

curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/storage/usage?include=counts"
# 行增 {"nodeCount":1,"updatedAt":"2026-08-24T20:37:26Z"}
#   nodeCount 只计文件 node；updatedAt = 仓库配置变更时刻（非「最新制品时间」）

curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/storage/usage?include=bogus"          # 未知值显式拒绝
# 400 {"errors":[{"status":400,"message":"include must be \"counts\" (unknown include value: \"bogus\")"}]}
```

### E2/E3 · users 列表加宽与 `enabled` 回显

- 列表条目从 `{name,uri,realm,source}` 加宽为 `{name,uri,realm,source,email,adminRole,enabled,groups}`——`enabled`/`groups` 恒渲染（空组 `[]` 非 null）；控制台用户页由此单请求成表（M8 期 21 请求扇出退役）。
- 单查端点 `GET /api/security/users/{name}` 增 `enabled: bool` 恒渲染（DB 行事实）。写侧：`POST /api/security/users/{name}` body `{"enabled":false}` 禁用（禁用后该用户登录/既有会话 401），`{"enabled":true}` 复启——显式传值才生效，缺省不动。

### E4 · `DELETE /api/security/users/{name}`（删除用户）

**admin only**（`CapSecurityWrite`；非 admin 403、匿名 401）。成功 **200 纯文本**（`The user: '<name>' has been removed successfully.`），四道护栏全 **400 纯文本**，检查序固定：

| 序 | 护栏 | 响应（逐字） |
|---|---|---|
| 1 | 目标不存在 | **404** `User not found`（文本体，与 GET 单用户同形——注意不是 Artifactory 的无 body 404） |
| 2 | 内置 `admin` | 400 `Cannot delete the built-in admin user.` |
| 3 | 最后一个 admin | 400 `Cannot delete user '<name>'. There must be at least one user configured with admin privileges.` |
| 4 | 自删 | 400 `Cannot delete the current authenticated user.` |

- **级联（同事务）**：剥该用户在全部 permission target 的授权行（ACE）→ 删用户行 → FK 级联清组员关系、**吊销全部 token 与 web session**（已持有的 Bearer 即刻 401，实测）；审计历史保留。与组删除的 409 保护是**有意不对称**：组是多成员策略对象（静默剥夺全员授权 → 拒绝），用户是单主体（级联即删除意图本身）。
- **重复删除 = 确定性 404（有意非幂等）**：删除成功后同一请求再发得 404 `User not found` 文本体——不是 Artifactory「吞 404 视为成功」的幂等形态（那是其并发窗口产物）。调用方应把第二次 404 理解为「对象已被删」，不要重试。
- 审计：成功删除落 `user.delete`（detail 含 `user`）；**护栏拒绝不落审计**。
- 控制台对应面（M9）：列表行 + 编辑页危险区双入口，**输入用户名强确认**——见[控制台指南](console.md#用户与权限adminsecurity)。

### E5 · `GET /api/security/groups/{name}?includeUsers=true`

- 带参（字面 `true`，大小写敏感）：三字段之上附 `userNames: []string`（空组 `[]` 恒非 null）；其余拼法（`false`/`junk`/`TRUE`）回无参三字段形态 200，**不发明 400**。未知组带参 → 404 `Group not found`（与无参同文案）。
- `GET /api/security/groups` **列表不加宽**（K19 定案）：成员汇总的单一事实源是 user_groups 行 → users 列表 `groups[]` 投影（客户端过滤）+ 本参数按需取单组。

### E6 · `GET /api/v1/permissions?filter=manage`

- **无 filter：行为字节不变**（admin/readonly_admin 全量；其余含 manage 持有者一律 403 `administrator privileges required`）。
- `filter=manage` 三臂：admin/readonly_admin → 全量（与无参响应**逐字节一致**，实测 diff 为空）；manage 持有者且覆盖集非空 → 仅 `repos ⊆ 覆盖集` 的 target（条目字段与全量一致；**部分覆盖的 target 隐藏**）；覆盖集为空 → 403（与无参门同形同字节）。`?filter=`（空值）= 无 ask；未知值 → **400 errors[] 信封**（`filter must be "manage" (unknown filter value: "bogus")`）。
- 用途：仓库级管理员（manage 持有者）经此端点在控制台可达权限编辑器——见 [RBAC 指南](admin/rbac-roles.md#manage-能做什么--不能做什么)。

---

## M10 新增端点速览（T-296）

license / addons / uploads 三族与三个门控包型的接入面（依据 ADR-0032/0033/0034 as-built + T-285/T-287/T-289/T-294 实测，2026-08-26）：

### license 域（**单数**路径）

| 方法 | 路径 | 语义 | 里程碑 |
|---|---|---|---|
| GET | `/binflow/api/system/license` | 状态查询（CapSystemRead——admin/readonly_admin；body 永不含文档原文/签名） | M10 |
| POST | `/binflow/api/system/license` | 安装（body = 文档原文；成功 **201**；验签拒 **400**，wire 码 `LICENSE_EXPIRED`/`LICENSE_INVALID`，现证不动；CapSystemWrite） | M10 |
| DELETE | `/binflow/api/system/license` | 卸载（幂等 **200 纯文本** `License removed successfully.`；CapSystemWrite；降级不劫持数据） | M10 |
| * | `/binflow/api/system/licenses`（复数） | **404** 有意不做（Artifactory HA 多证语义不采纳；404 即引导） | M10 |

### addon 域

| 方法 | 路径 | 语义 | 里程碑 |
|---|---|---|---|
| GET | `/binflow/api/v1/addons` | 11 槽位清单实时求值（bare array：`id`/`kind`/`minTier`/`enabled`/`reason`/`displayName`/`description`；CapSystemRead；**无写面**——其余动词 404） | M10 |

### uploads 域（MPU；Artifactory 形——M11 T-332 整体翻转，ADR-0039；**数据端点仅纯 S3 后端**，filestore/双写实例回 **501 纯文本**非 404）

| 方法 | 路径 | 语义 | 里程碑 |
|---|---|---|---|
| POST | `/binflow/api/v1/uploads/create?repoKey=&repoPath=&partSizeMB=` | 开会话（QueryParam 非 JSON 体；认证 + admin/user 角色 + 目标仓 `w`；virtual 仓回落 defaultDeploymentRepo；不限包型）。**200 `{"token": ...}`**——会话能力凭据 | M11 |
| GET | `/binflow/api/v1/uploads/config` | 能力探测：**200 `{"supported": bool}`**（S3 栈 true / filestore **false**——探测端点不回 501）；带 jfrog-cli-go UA 版本门（低于 2.62.2 回 false） | M11 |
| POST | `/binflow/api/v1/uploads/urlPart?partNumber=N` | 第 n 片的上传 URL（Bearer 会话 token；**200 `{"url": ...}`**——URL 查询串自带 `?token=` 能力，PUT 可免 Authorization） | M11 |
| POST | `/binflow/api/v1/uploads/status` | 异步任务进度（Bearer）：**200 `{status, error, progress, checksumToken}`**；status ∈ PARTS/PROCESSING/**FINISHED**(progress 100 + checksumToken)/NON_RETRYABLE_ERROR | M11 |
| PUT | `/binflow/api/v1/uploads/part/{id}/{n}?token=` | 传片（urlPart 目标；**200** S3 PutObject 形；可乱序到达——有界重排暂存；服务端中继进 S3 multipart，checksum 服务端实测） | M11 |
| POST | `/binflow/api/v1/uploads/complete?sha1=` | 提交组装（Bearer；**sha1** 40 hex 必填）→ **202 受理**，任务异步；错配经 status 的 NON_RETRYABLE_ERROR 呈现 | M11 |
| POST | `/binflow/api/v1/uploads/abort` | 弃置会话（Bearer）→ 204 | M11 |

流程（jfrog-cli 实测 2.122.0）：create 拿 token → urlPart/PUT 分片（可并发乱序）→ `complete?sha1=` 202 → 轮询 status 至 **FINISHED** 拿 `checksumToken`（5 分钟）→ 客户端凭它做零传输 `X-Checksum-Deploy` PUT 落节点（节点由客户端落，服务端只组装+登记 blob）。会话跨重启存活：能力绑定持久化在引擎 upload_sessions 行，重启后同一 token 继续可用（T-323R 在新 wire 上保留）。旧形状（JSON 体 create/config 重分片、GET urlPart/status 清单、路径 id 四端点、complete 201/sha256）已退役 → 404。

### 门控包型接入面（pro 档槽位）

| 包型 | 挂载面 | 详见 |
|---|---|---|
| go | 内容面 `/binflow/<repoKey>/<module>/@v/...`（GET 五端点 + PUT 三件套厂商扩展；与五核心包型同构，无额外路径段） | [Go Modules 接入](integrations/golang.md) |
| nuget | 管理面 `/binflow/api/nuget/{v3,v2}/<repoKey>/...`（plane-aware 重写进 dispatchContent 链，ADR-0034） | [NuGet 接入](integrations/nuget.md) |
| cargo | 内容面 `/binflow/<repoKey>/`（`index/` sparse 索引 + `v1/crates/` 下载 + `api/v1/crates/` Web API） | [Cargo 接入](integrations/cargo.md) |

门控拒绝闭集（D1 读放行 / D2 写 403 + `X-Binflow-License-Required` / D3 建仓 400 / D6 到期即降级）与 `addons.disabled` 熔断见 [License 与 Add-ons 管理](admin/license.md)。

---

## M11 增补速览（T-328）

认证配置面（T-305 + SAML SP 证书三端点 T-331）、GPG keypair 族（T-319）、cleanup 引擎（T-324）、四包型 reindex 族（T-308/309/310/311）与 smart remote 两字段生效（T-317 L25 反转）；另 M11 交付 **cargo remote/virtual 仓型**（T-316/T-318——REST 建仓走通用 `PUT /binflow/api/repositories/{key}`，协议面语义见 [Cargo 接入](integrations/cargo.md)）。本节 curl 命令在 HEAD 构建 scratch 实例（`BINFLOW_REMOTE_CREDENTIALS_KEY` 已设）上实测（2026-08-28）；行为依据各票工作日志与 ADR-0035/0036/0038/0039。

### 认证配置域（`/api/v1/admin/security/*`；三段 × GET/PUT/test + SAML SP 证书三端点）

| 方法 | 路径 | 门 | 语义 |
|---|---|---|---|
| GET | `/binflow/api/v1/admin/security/ldap` | CapSecurityRead | LDAP 段（未设置回默认形） |
| PUT | `/binflow/api/v1/admin/security/ldap` | CapSecurityWrite | 整段替换，**保存即生效**（无需重启） |
| POST | `/binflow/api/v1/admin/security/ldap/test` | CapSecurityWrite | 测试连接（TestReport，见下） |
| GET / PUT / POST …/test | `…/admin/security/oauth` | 同上 | OIDC 段（snake_case wire） |
| GET / PUT / POST …/test | `…/admin/security/saml/config` | 同上 | SAML 段（未设置 GET 回 `{}`） |
| GET | `/binflow/api/v1/admin/security/saml/config/key/public` | CapSecurityRead | **当前 SP 加密证书 PEM**（text/plain）；未生成 404 `saml sp encryption certificate has not been generated`（T-331，实测） |
| PUT | `/binflow/api/v1/admin/security/saml/config/key/public/regenerate` | CapSecurityWrite | 轮换 SP 钥对（force 一对一替换、旧证书即刻失效），响应体 = 新证书 PEM |
| POST | `/binflow/api/v1/admin/security/saml/key` | CapSecurityWrite | 生成/替换 SP 钥对（BinFlow 原生面；与 regenerate 同机、审计动作分立：`auth.config.samlkey.{generate,regenerate}`，零密材落日志） |

- **secret 哨兵语义（write-only）**：GET 对已设置 secret 恒回 20 星 `********************`；PUT 键缺席 = 保持、`""` = 清除、新明文 = 替换；**回传哨兵 → 400** `refusing the masked placeholder — leave the field empty to keep the stored secret, or re-enter the value`（实测）。
- secret 落库前 enc:v1 密封（实例主密钥 `BINFLOW_REMOTE_CREDENTIALS_KEY`）；无主密钥时 secret 写拒绝。
- test 响应：`{"ok":bool,"phase":"…","category":"…","message":"…"}`，`ok:false` 时 HTTP 400（如 `{"ok":false,"phase":"dial","category":"unreachable","message":"could not connect to the target (dial failed or timed out)"}`，实测）。
- 审计：`auth.config.update`（detail 只含变更键名，值不落）/ `auth.config.test`。
- 字段表与控制台面：[认证配置指南](admin/auth-config.md)。

### keypair 域（`/api/security/keypair*`，Artifactory 兼容 + BinFlow 原生生成）

| 方法 | 路径 | 门 | 语义 |
|---|---|---|---|
| POST | `/binflow/api/security/keypair` | CapSecurityWrite | 导入（create-or-replace；201 回 KeyPairSummary） |
| PUT | `/binflow/api/security/keypair` | CapSecurityWrite | 更新（不存在 → 404；轮换面） |
| GET | `/binflow/api/security/keypair` | CapSecurityRead | 列表（bare array） |
| GET | `/binflow/api/security/keypair/{pairName}` | CapSecurityRead | 单查（KeyPairSummary；未知名 404） |
| DELETE | `/binflow/api/security/keypair/{pairName}` | CapSecurityWrite | 删除；200 纯文本 `OK`；**被仓引用 → 400 点名引用仓清单** |
| POST | `/binflow/api/security/keypair/verify` | CapSecurityWrite | 200 纯文本 `Key was verified.`；body 全量材料或（BinFlow 扩展）仅 `{"pairName":…}` 校验存量密封钥（实测） |
| GET | `/binflow/api/security/keypair/public/repositories/{repoKey}` | CapSecurityRead | 该仓关联 keypair 的 armored 公钥（text/plain） |
| POST | `/binflow/api/v1/admin/security/keypair/generate` | CapSecurityWrite | **BinFlow 原生服务端生成**（201 回 summary；重名 409）——Artifactory 官方 REST 无 keygen，此端点为自有管理面 |
| POST / DELETE | `/binflow/api/v2/repositories/{repoKey}/keyPairs[/{keyName}]` | CapSecurityWrite | 仓关联（text/plain body = 钥名）/解除——仅 local `debian`/`rpm` 仓接受 `keyPairName`，其余包型按名 400 |

- **私钥与口令永不出库**（无导出端点）；Summary 四字段 `{pairName, pairType, alias, publicKey}` + BinFlow additive 四字段（`algorithm`/`createdAt`/`updatedAt`/`updatedBy`/`repositories`，实测回显）。
- 生成入参：`{"pairName","alias","passphrase","keyBits","uidName","uidComment","uidEmail"}`；导入入参 = KeyPairInput（`pairName`/`pairType`("GPG")/`alias`/`privateKey`/`publicKey`/`passphrase`）。
- `X-GPG-PASSPHRASE` 头不收（D-8：口令随钥行密封）。
- 消费面：debian `InRelease`/`Release.gpg`、rpm `repomd.xml.asc`/`.key`（见[Debian 接入](integrations/debian.md)/[RPM 接入](integrations/rpm.md)）。

### cleanup 域（unused-cleanup 引擎；remote 缓存清理）

| 方法 | 路径 | 门 | 语义 |
|---|---|---|---|
| POST | `/binflow/api/v1/system/cleanup` | system:write（admin only） | 手动触发一次；body `{"apply":bool,"repo":string?}`——**dry-run 默认**；同步执行回 CleanupReport |
| GET | `/binflow/api/v1/system/cleanup` | system:read | 状态面：cron 节奏、累计计数、上次报告、各 remote 仓策略行 |

- 引擎三腿（单把维护锁，与 gc/export/import 互斥）：过期上传会话扫掠 → policy 删除（窗口内无下载事件的 remote 缓存 FILE node；**在用 oracle = 审计下载 trails ∪ 以该仓为成员的 virtual 仓下载**）→ GCSweep（ADR-0031 双门；grace 窗内递延计数进 `gracePending`）。
- 策略源：remote 仓配置 `unusedArtifactsCleanupPeriodHours`（**M11 起生效**；M10 仅落库）；cron 每小时 apply 一轮。
- `audit.enabled=false` 时 policy 腿拒绝运行（无下载痕迹就没有诚实的「未用」，宁可不删），session/gc 腿照跑。
- 报告字段（实测）：`trigger/apply/repos[{repo,periodHours,cutoff,keptByUse,candidates,deleted,bytes}]/gracePending/gcDeleted/sessionsSwept/objectsCleaned/bytesReclaimed/ok`；审计 `cleanup.run`；指标 `binflow_cleanup_objects` / `binflow_cleanup_bytes`（gauge）。

### 四包型 reindex 管理族（dispatchAPI，ADR-0034）

| 端点 | 语义 |
|---|---|
| `POST /binflow/api/conan/reindex[?repoKey=]` / `POST /binflow/api/conan/{repoKey}/reindex` | conan 修订索引重建（仅 local；同步；CanManageRepo） |
| `POST /binflow/api/helm/{repoKey}/reindex` / `…/reindex/{path}` | helm index.yaml 重算（异步全仓 / 同步部分） |
| `POST /binflow/api/deb/reindex/{repoKey}?async=0\|1` | debian 索引重算（virtual/remote 类 400） |
| `POST /binflow/api/yum/{repoKey}?path=&async=0\|1` | rpm repodata 重算；**virtual 仓 200/202 触发聚合重合并**（`path` 自动补 `/repodata`）；auto-async 仓同步请求 409 |

各端点语义详见对应接入指南（[Conan](integrations/conan.md) · [Helm](integrations/helm-charts.md) · [RPM](integrations/rpm.md) · [Debian](integrations/debian.md)）。

### smart remote 两字段生效（L25 反转，T-317）

M10 的「按名 400」退役——remote 仓配置现在**接受 + canonical 回显 + 行为生效**（实测）：

| 字段 | 类型 | 默认 | 行为 |
|---|---|---|---|
| `enableTokenAuthentication` | bool | false | `true` 时拉取侧对上游发 `Authorization: Bearer <password>`（无密码 = 匿名维持）；SSRF 链/凭据不泄漏规则不变 |
| `contentSynchronisation.enabled` | bool | false | 开启拉取侧内容同步 |
| `contentSynchronisation.propertiesEnabled` | bool | false | 内容类节点落地后从上游 `/api/storage/{repo}/{path}?properties=` 附着属性（best-effort，失败仅 WARN） |
| `contentSynchronisation.statisticsEnabled` | bool | false | 接受 + 回显；统计上报协议无公开规格，暂无行为 |
| `contentSynchronisation.sourceOrigin` | bool | false | 接受 + 回显；origin 标记面未落地，暂无行为 |

推送侧属性携带（generic 平面）随复制引擎默认开启：源节点属性在推送时合并到目标（幂等重传触发零传输收敛）。replica 虚仓隔离维持 ADR-0025 决策 1 现状（面只读 405 + 逐字文案；backing 直写仍开放）。

---

## M12 增补速览（T-347A）

三个族（行为细节与逐字报错见[制品操作族](admin/artifact-operations.md)与[Trash can 管理](admin/trash-can.md)；依据 T-339/T-343/T-345 真二进制与 httptest 真服务端栈实测）：

### 制品操作域（`/api/copy|move` + `/api/archive/download` + 内容面两形态；**整族 pro 槽 `repo-operations`**）

| 方法 | 路径 | 门 | 语义 |
|---|---|---|---|
| POST | `/binflow/api/copy/{srcRepo}[/{srcPath}]?to=/{targetRepo}[/{targetPath}]` | 认证 + 逐文件管线（源 read/目标 write）+ license | 树级复制（零拷贝）；`dry=1` 干跑；响应 200 + `messages[]`，Content-Type 为 vendor 形 `application/vnd.org.jfrog.artifactory.storage.CopyOrMoveResult+json`；状态 = 最后一条 error 的码（无码 409 兜底） |
| POST | `/binflow/api/move/{srcRepo}[/{srcPath}]?to=…` | 同上 + 源 `delete` | 树级搬移（copy + 源删除 + 目录剪除） |
| GET | `/binflow/api/archive/download/{repo}[/{path}]?archiveType=zip\|tar\|tar.gz\|tgz` | 读权限（匿名 401 先于参数解析） | 目录/整仓流式打包（不落盘）；`includeChecksumFiles=true` 附 checksum 伴随条目；**默认关**（`folder_download.enabled=false`，**M13 起六字段可配**，重启生效——见[制品操作族](admin/artifact-operations.md)） |
| GET | `/binflow/{repo}/{archive}!/{entry}`（内容面） | 归档路径读门 | 归档内成员直读（首个 `!/` 切分、嵌套递归、`.sha1/.md5/.sha256` 后缀回裸 hex）；非 GET 405 |
| PUT | `/binflow/{repo}/{path}` + `X-Explode-Archive[: true]`（或 `X-Explode-Archive-Atomic: true`） | 目标父目录 `w` | 解包部署：白名单 zip/tar/tar.gz/tgz；成功 **201 空体** + `X-Binflow-Exploded-Files: <n>` 计数头；归档原件不落库 |

注：`/api/flat/copy|move` 不实现（404）；community 实例整族答 403 + `X-Binflow-License-Required: repo-operations`（真二进制实测）。

### trash 域（`/api/trash/*`；门 = system:write（**仅全量 admin**）+ pro 槽 `trashcan`〔暂行〕）

| 方法 | 路径 | 语义 |
|---|---|---|
| POST | `/binflow/api/trash/restore/{path}?to=&transaction-size=` | 恢复（`to` 覆盖 > 五元组 > 路径首段；剥 `trash.*` 标记、原属性保留）；响应 = copy/move 的 `messages[]` 同构 |
| POST | `/binflow/api/trash/empty` | 清空整个 can；回 JSON 摘要 `{"removed","files","folders","bytes"}` |
| DELETE | `/binflow/api/trash/clean/{path}` | 单条（子树）永久清除；摘要同上 |

浏览不是第四路由：骑既有 `GET /api/storage/auto-trashcan[...][?properties|?list]`（五元组断言面 = `?properties`）。捕获/保留期（默认 14 天、小时 cron；**M13 起保留期经 `trashcan.retention_days` 可配**，重启生效）语义见 [Trash can 管理](admin/trash-can.md)。

---

## M13 增补速览（T-375）

webhook 订阅七端点族、system/settings 旋钮回显与 remote 仓 `chartsBaseUrl` 字段（依据 T-362~T-368 as-built + T-375 双实例实测，2026-08-30；完整语义见 [Webhook 使用指南](admin/webhooks.md)与 [Helm Chart 仓库接入](integrations/helm-charts.md)）：

### webhook 域（`/binflow/event/api/v1/**`；注意**不在** `/binflow/api` 下；**整族 pro 槽 `webhook`**——写动词 community 403 + `X-Binflow-License-Required: webhook`，读面不设门）

| 方法 | 路径 | 门 | 语义 |
|---|---|---|---|
| GET | `/binflow/event/api/v1/subscriptions` | system:read（readonly_admin 可见） | 订阅列表（bare array） |
| POST | `/binflow/event/api/v1/subscriptions` | system:write + license | 创建；**201** 回显 SubscriptionView（`secret` 恒掩码 `********`） |
| GET | `/binflow/event/api/v1/subscriptions/{key}` | system:read | 单查；miss **404 `Subscription not found`** |
| PUT | `/binflow/event/api/v1/subscriptions/{key}` | system:write + license | 全量更新；**204 无体**；key 不可改 |
| DELETE | `/binflow/event/api/v1/subscriptions/{key}` | system:write + license | 删除（级联删投递行）；**204**；再删 404 |
| POST | `/binflow/event/api/v1/subscriptions/test` | system:write + license | **试发草稿**（吃完整订阅体，非 key 引用）；同步单发不入箱；200 TestOutcome（`ok`/`attempt{status_code,elapsed_millis,error}`——**失败也是 200，看 body**） |
| GET | `/binflow/event/api/v1/troubleshooting` | system:read | 排障记录环（query：`subscription`/`target`/`start`/`end`/`count`；失败必录、`debug:true` 成功也录；进程内环 10000 条/30s 修剪，重启失史） |

- 请求体 = 订阅一形：`key`（`^[A-Za-z][A-Za-z0-9_-]+$` ≤500）/`project_key`/`description`/`enabled`（**默认 false**）/`event_filter{domain,event_types[],criteria}`（strict——未知键 400）/`handlers[]`（**恰 1 个**；`webhook` 或 `custom-webhook` 两型）/`debug`。
- 投递：HMAC-SHA256 hex 于 `X-JFrog-Event-Auth`（`use_secret_for_signing=false` 时为 secret 明文直传）；重试 **5 次首试计入 / 固定 10s / 单次 30s 超时 / 仅发送失败或 ≥500**（4xx 一步终态）；死信落审计 `webhook.dead_letter` + 指标族 `binflow_webhook_*`。
- SSRF：目标默认拒 loopback/私网；`webhook.allow_private_target`（默认 false，重启生效）放行。

### system 域：旋钮回显

| 方法 | 路径 | 门 | 语义 |
|---|---|---|---|
| GET | `/binflow/api/v1/system/settings` | system:read（admin / readonly_admin） | 回显**已解析**的运行旋钮（YAML+env+defaults 合流值）：`folder_download` 六字段 + `trashcan.retention_days`。**knob-scoped 裁量**——只回行为旋钮，永不携带 secret/DSN/路径；只读（其余动词 E-26 404）；旋钮本身重启生效 |

```bash
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/system/settings
# {"folder_download":{"enabled":false,"enabled_for_anonymous":false,
#   "max_download_size_mb":1024,"max_files":5000,"max_concurrent_requests":10,
#   "enabled_empty_directories":false},
#  "trashcan":{"retention_days":14}}
```

### 仓库域：remote 配置增量字段

| 字段 | 类型 | 适用 | 行为 |
|---|---|---|---|
| `chartsBaseUrl` | string | **仅 `packageType=helm` 的 remote** | content 类回源（tgz/.prov/`_external` 折叠路径）的分体基址；metadata（index.yaml）恒走仓 URL；缺省回退仓 URL。绝对 http(s) URL，`""` = 清除。**其它包型携带 → 400 点名字段**；异 host 时**无凭据**出站（仓凭据不发第三方）。virtual 聚合 index 的改写识别基址同此字段（M13） |

（HelmOCI remote/virtual 仓型与 `_external` 落盘缓存同为本里程碑增量，建仓走通用 `PUT /api/repositories/{key}`，协议面语义见 [Helm Chart 仓库接入](integrations/helm-charts.md#helmoci-仓型oci-形态m13local--remote--virtual)。）

---

## `/api/v1` 自有端点

BinFlow 在 Artifactory 兼容端点之外增加了一批自有端点（以 `/api/v1` 前缀标记），实现差异化能力：

| 端点 | 方法 | 说明 |
|---|---|---|
| `/binflow/api/v1/health` | GET | 实例健康详情（admin / readonly_admin） |
| `/binflow/api/v1/storage/stats` | GET | 全实例 blob/字节统计（admin / readonly_admin） |
| `/binflow/api/v1/storage/usage/{repo}` | GET | 单仓配额用量（admin / readonly_admin / 该仓 read 或 manage 授权者） |
| `/binflow/api/v1/storage/usage` | GET | 批量配额用量（M9：可见集过滤的 bare array，替代逐仓 N 次轮询） |
| `/binflow/api/v1/audit` | GET | 审计日志查询（admin / readonly_admin） |
| `/binflow/api/v1/system/gc` | POST | 触发 GC（同步执行，dry-run/apply；admin only） |
| `/binflow/api/v1/system/cleanup` | POST/GET | unused-cleanup 引擎手动触发（dry-run 默认）与状态面（M11） |
| `/binflow/api/v1/system/settings` | GET | 运行旋钮回显（folder_download 六字段 + trashcan.retention_days 的解析值；M13） |
| `/binflow/api/v1/session` | POST/GET/DELETE | 控制台会话管理（whoami/登录回显 `adminRole` 与 `source`） |
| `/binflow/api/v1/permissions` | POST/GET/DELETE | Permission Target CRUD（动作集 r/w/d/manage；GET 带 `?filter=manage` 时 manage 持有者可达覆盖集内子集——M9） |

---

## 三种认证方式

BinFlow 支持三种认证凭据，适用于不同的使用场景：

### 1. Basic 认证（HTTP Basic Auth）

适用于 curl、CI 脚本、客户端凭据配置（maven settings.xml、`.pypirc`、npm `_auth`）。REST API 层面要求凭据始终在请求中且正确——**不存在「先访问后挑战」**（与 HTTP 标准 401 挑战不同——一次性认证失败直接返回 401）：

```bash
# 全局选项形式（推荐 -u 简写）
curl -su admin:<口令> $BASE/binflow/api/system/ping

# 等价的显式 Authorization 头
curl -s -H "Authorization: Basic $(printf 'admin:<口令>' | base64 -w0)" \
  $BASE/binflow/api/system/ping
```

所有 `/binflow/api/*` 基础路由均支持 Basic 认证。口令哈希为 **argon2id**（memory-hard），高并发场景请使用 Access Token。

### 2. Token 认证（Bearer Token）

适用于高 QPS、CI/CD 流水线和无浏览器场景。Token 校验不触发 argon2 哈希计算，性能远优于 Basic：

```bash
# 签发 token（admin 可指名替目标用户签发；非 admin 免 username 自铸）
TOKEN=$(curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/token \
  -d 'grant_type=client_credentials&username=ci-bot' | jq -r '.access_token')
# 200: {"access_token":"<64hex>","token_id":"<id>","expires_in":2592000,"scope":"api:*"}

# 使用 token
curl -s -H "Authorization: Bearer $TOKEN" $BASE/binflow/api/v1/storage/stats

# 吊销 token
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/token/revoke \
  -d "token_id=<上面的 token_id>"
```

| 属性 | 值 |
|---|---|
| token 长度 | 64 位 hex（256-bit） |
| 默认 TTL | 2592000 秒（30 天）；`auth__token_default_ttl_hours` 可调 |
| 签发 | admin 为任意用户签发；非 admin 限本人（M6 起）；body 可选 `step_up_password` / `step_up_grant`（M7，仅 `auth.token_step_up` 开启时的非 admin session 臂要求，见 [step-up 指南](admin/token-step-up.md)） |
| 吊销 | admin only；所有 token 同表管理 |
| 审计签发 | M4 登记缺口（无审计事件），token.revoke 日志可见 |
| docker token 流 | 也走同表——管理面吊销对 docker token 即时生效 |

### 3. 会话 Cookie（浏览器端）

适用于 Web 控制台。服务端签发的 `binflow_session` cookie（HttpOnly; Path=/binflow; SameSite=Lax），自动随同源请求发送：

```bash
# 登录（JSON）→ Set-Cookie: binflow_session=<id>; HttpOnly; Path=/binflow; SameSite=Lax
curl -s -c jar.txt -X POST $BASE/binflow/api/v1/session \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<口令>"}'
# 200 {"username":"admin","admin":true}

# whoami（会话有效时）
curl -s -b jar.txt $BASE/binflow/api/v1/session
# 200 {"username":"admin","admin":true}

# 登出（服务端吊销会话）
curl -s -b jar.txt -X DELETE $BASE/binflow/api/v1/session -o /dev/null -w '%{http_code}'
# 204

# 会话过期后同一 cookie 重放 → 401
```

会话 TTL 默认为 24 小时，**活跃不能续期**（滑动续期被绝对 TTL 封顶吞没），详见[控制台使用指南](console.md#登录与会话)。

> **CSRF 防护**：会话 cookie 认证的非 GET/HEAD 写请求，携带非同源 `Origin` 头 → **403**。Basic/Token 认证天然免疫。

---

## 错误响应：三种格式

### 1. errors[] 信封（主要格式）

制品域和管理面 API 的主要错误格式（对应 E-01 统一信封）：

```json
// 404 — 制品不存在
HTTP/1.1 404 Not Found
Content-Type: application/json

{"errors":[{"status":404,"message":"Unable to find the requested resource 'generic-local/missing.jar'."}]}

// 413 — 配额超限
HTTP/1.1 413 Request Entity Too Large
Content-Type: application/json

{"errors":[{"status":413,"message":"Repository 'tiny' quota exceeded: used 800 of 934 bytes; the write to 'b.bin' needs 800 more bytes."}]}

// 409 — checksum 不匹配
HTTP/1.1 409 Conflict
Content-Type: application/json

{"errors":[{"status":409,"message":"Checksum error for 'maven-local/com/example/demo/1.0.0/demo-1.0.0.jar': received 'abc123' but actual is 'def456'."}]}

// 400 — 参数非法
HTTP/1.1 400 Bad Request
Content-Type: application/json

{"errors":[{"status":400,"message":"Repository key must be at least 2 characters: 'x'"}]}

// 403 — 无权限
HTTP/1.1 403 Forbidden
Content-Type: application/json

{"errors":[{"status":403,"message":"permission denied"}]}

// 401 — 未认证或凭据无效（制品域）
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Basic realm="BinFlow"
Content-Type: application/json

{"errors":[{"status":401,"message":"invalid credentials"}]}
```

### 2. 纯文本错误（用户管理域）

用户、组、Token 端点的部分错误使用纯文本响应体：

```bash
# 404 — 组不存在
HTTP/1.1 404 Not Found
Content-Type: text/plain; charset=utf-8

Unable to find group by name 'nonexistent-group'.

# 400 — 建组参数错误
HTTP/1.1 400 Bad Request
Content-Type: text/plain; charset=utf-8

Unable to create group: name must match [a-z][a-z0-9._-]* but it starts with uppercase 'X'.

# 400 — 创建用户缺 email
HTTP/1.1 400 Bad Request
Content-Type: text/plain; charset=utf-8

Please provide a valid user email.

# 400 — 用户引用了不存在的组
HTTP/1.1 400 Bad Request
Content-Type: text/plain; charset=utf-8

Unable to find group by name 'devs'. Please make sure the group exists before adding users to it.

# 400 — adminRole 与 admin 布尔矛盾（M7；仅 admin 可写角色字段）
HTTP/1.1 400 Bad Request
Content-Type: text/plain; charset=utf-8

conflicting 'admin' and 'adminRole' fields: admin=false is incompatible with adminRole="admin" (admin=true is equivalent to adminRole=admin)

# 409 — 删除的组被权限引用
HTTP/1.1 409 Conflict
Content-Type: text/plain; charset=utf-8

Cannot delete group 'devs': it is referenced by permission target(s): devs-rw, jane-rd. Remove the group from those targets first.

# M9 起：DELETE /api/security/users/{name} 同属纯文本家族
# 成功 200 / 护栏 400 / 不存在 404（文本体「User not found」）——全部文案逐字见「M9 增补速览」E4
```

### 3. OAuth 风格错误（Token 端点和 docker 域）

Token 签发/吊销端点与 `/v2/token` 按 OAuth 2.0 错误规范响应：

```bash
# Token 签发：管理面错误
HTTP/1.1 400 Bad Request
Content-Type: application/json

{"error":"invalid_request","error_description":"missing grant_type parameter"}

# Token 吊销：未知 token
HTTP/1.1 403 Forbidden
Content-Type: application/json

{"error":"access_denied","error_description":"token not found"}

# Token 签发：认证失败
HTTP/1.1 401 Unauthorized
Content-Type: application/json
WWW-Authenticate: Basic realm="BinFlow"

{"error":"invalid_client","error_description":"authentication failed"}

# Token 签发：step-up 两形态（M7，开关 auth.token_step_up 开启时的非 admin session 臂）
# 401 — 所欠二次凭据缺失（本地/LDAP 缺 step_up_password；OIDC 缺 step_up_grant；含错腿凭据）
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{"error":"step_up_required","error_description":"step-up authentication required to mint a token"}

# 401 — 二次凭据失验 / grant 过期 / grant 复用（单次消费即删）/ 服务重启丢台账
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{"error":"step_up_invalid","error_description":"step-up credential rejected, expired, or already used"}

# Docker /v2 面 401 挑战
HTTP/1.1 401 Unauthorized
Content-Type: application/json
Docker-Distribution-Api-Version: registry/2.0
WWW-Authenticate: Bearer realm="http://localhost:8080/v2/token",service="binflow"

{"errors":[{"code":"UNAUTHORIZED","message":"authentication required","detail":null}]}

# Docker /v2 面权限不足
HTTP/1.1 403 Forbidden
Content-Type: application/json
Docker-Distribution-Api-Version: registry/2.0

{"errors":[{"code":"DENIED","message":"requested access to the resource is denied","detail":null}]}
```

---

## 请求/响应头参考

### 通用请求头

| 头 | 适用场景 | 说明 |
|---|---|---|
| `Authorization: Basic <base64>` | 全部管理面 + 内容路径 | Basic 认证 |
| `Authorization: Bearer <token>` | 全部管理面 + 内容路径 + docker | Token 认证 |
| `X-Checksum-Sha1` / `X-Checksum-Sha256` / `X-Checksum-Md5` | PUT 上传 | 客户端声明校验和 |
| `X-Checksum` | PUT 上传 | 无类型标记的校验和（按长度自动识别） |
| `X-Checksum-Deploy: true` | PUT 上传 | checksum-only 部署（不传 body） |
| `Expect: 100-continue` | PUT 上传 | 去重加速：先查 blob 是否存在 |
| `Content-Type` | manifest PUT | 透传不白名单（docker 域） |

### 通用响应头

| 头 | 适用场景 | 说明 |
|---|---|---|
| `X-Checksum-Sha1` / `X-Checksum-Sha256` / `X-Checksum-Md5` | GET 下载 | 服务端实测校验和（有值才发） |
| `ETag: <sha1>` | GET 下载 | 不包围引号；条件请求 `If-None-Match` |
| `Last-Modified` | GET 下载 | RFC1123 格式 |
| `Accept-Ranges: bytes` | GET 下载 | Range 请求支持 |
| `X-Artifactory-Filename` | GET 下载 | URL-encoded 文件名 |
| `Location` | PUT 上传成功 | 新资源 URL |
| `Cache-Control: no-store` | 仓库列表等敏感数据 | 禁止缓存 |

---

## 审计日志查询参数

`GET /binflow/api/v1/audit`（admin only）：

| 参数 | 类型 | 说明 |
|---|---|---|
| `repo` | string | 按仓库等值过滤 |
| `actor` | string | 按操作者等值过滤 |
| `action` | string | 按动作等值过滤（见审计词表） |
| `since` | RFC3339 | 起始时间（闭） |
| `until` | RFC3339 | 结束时间（开） |
| `limit` | int | 默认 100，上限 1000（超限 400） |
| `cursor` | string | 游标分页（不透明，`nextCursor` 回传） |

响应格式：

```json
{
  "events": [
    {
      "id": 42,
      "time": "2026-08-21T12:34:56.789Z",
      "actor": "admin",
      "action": "deploy",
      "repo": "generic-local",
      "path": "a/b/w.bin",
      "detail": null
    }
  ],
  "nextCursor": "43"
}
```

结果按 **时间倒序**（最新在前）。`detail` 为可选的附加上下文对象（如 `quota.exceeded` 的 `{used, quota}`）。

---

## GC 触发参数

`POST /binflow/api/v1/system/gc`（admin only）：

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `apply` | bool | false | false=dry-run（只报告不删除）；true=实际执行 |
| `graceHours` | number | 配置值（默认24） | 宽限期；0=无宽限；负值或 >876000 → 400 |

响应：

```json
// dry-run
{"candidateCount":5,"candidateBytes":204800,"deletedCount":0}

// apply
{"candidateCount":5,"candidateBytes":204800,"deletedCount":3}
```

---

## 下一步

- 各协议接入指南：[Docker](docker-registry.md) · [Maven](integrations/maven.md) · [npm](integrations/npm.md) · [PyPI](integrations/pypi.md) · [Go](integrations/golang.md) · [NuGet](integrations/nuget.md) · [Cargo](integrations/cargo.md) · [Conan](integrations/conan.md) · [Helm](integrations/helm-charts.md) · [RPM](integrations/rpm.md) · [Debian](integrations/debian.md)
- 管理操作：[治理指南](admin/governance.md) · [权限管理](admin/groups-permissions.md) · [RBAC 角色与仓库级管理员](admin/rbac-roles.md) · [Token 铸造 step-up](admin/token-step-up.md) · [备份恢复](admin/backup-restore.md) · [License 与 Add-ons](admin/license.md) · [属性系统](properties.md) · [认证配置](admin/auth-config.md) · [存储配置](admin/storage-config.md) · [Webhook 使用指南](admin/webhooks.md)
- 常见问题与排障：[FAQ](faq.md)