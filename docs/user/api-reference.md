---
title: API 参考
sidebar_position: 70
---

# API 参考

> 适用版本：M1~M4（端点引入里程碑标注于各表）。Artifactory 兼容端点基于 REST 逆向规格 `docs/reverse/rest-api.md`（置信度高）。
> BinFlow 自有端点以 `/api/v1` 前缀标记。

BinFlow 的 API 分为两个面：

1. **制品的容路径**（无 `/api` 前缀）：`/binflow/<repoKey>/<path>`——上传、下载、删除制品，各协议客户端走这里。
2. **管理面 API**（`/binflow/api/*`）：仓库 CRUD、用户/组/权限、审计、GC、Token、搜索、系统信息。
3. **Docker Registry 根级平面**（`/v2/*`）——独立路由，见[Docker 接入指南](docker-registry.md)。

---

## 兼容端点总表

按 PRD 兼容域标注：E（通用）、DE（Docker）、ME（Maven）、NE（npm）、PE（PyPI）、SE（安全）、SR（搜索）。

### E: 通用制品域

| 方法 | 路径 | 语义 | 里程碑 |
|---|---|---|---|
| PUT | `/binflow/{repoKey}/{path}` | 上传文件（body 为内容），matrix 参数与 checksum 头支持 | M1 |
| PUT | `/binflow/{repoKey}/{path}/` | 创建目录（尾斜杠） | M1 |
| PUT | `/binflow/{repoKey}/{path}.sha1\|.md5\|.sha256` | 上传校验和旁车文件 | M1 |
| GET | `/binflow/{repoKey}/{path}` | 下载文件（支持 Range/If-None-Match/ETag） | M1 |
| HEAD | `/binflow/{repoKey}/{path}` | 文件元信息（响应头同 GET 无 body） | M1 |
| DELETE | `/binflow/{repoKey}/{path}` | 删除文件或目录树 | M1 |
| DELETE | `/binflow/{repoKey}/{path}?properties=k1,k2` | 删除制品属性 | M4 |
| PUT | `/binflow/api/storage/{repoKey}/{path}?properties=k=v` | 设置制品属性 | M4 |
| POST | `/binflow/api/storage/{repoKey}/{path}?properties=k=v` | 增量修改制品属性 | M4 |
| GET | `/binflow/api/storage/{repoKey}/{path}` | 取 FileInfo / FolderInfo JSON | M1 |
| GET | `/binflow/api/storage/{repoKey}/{path}?properties=K1,K2*` | 取属性（key 过滤 + 通配） | M1 |
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
| PATCH | `/v2/{name}/blobs/uploads/{uuid}` | 上传 blob 分片 | M2 |
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
| GET | `/binflow/api/security/users` | 用户列表（简洁形态） | M1 |
| GET | `/binflow/api/security/users/{name}` | 用户详情（无口令字段） | M1 |
| PUT | `/binflow/api/security/users/{name}` | 创建或替换用户（create-or-replace，两态 201） | M1 |
| POST | `/binflow/api/security/users/{name}` | 部分更新用户（email/password/admin/groups） | M4 |
| PUT | `/binflow/api/security/password` | 当前用户改密 | M1 |
| POST | `/binflow/api/security/users/authorization/changePassword` | 别名改密端点 | M4 |
| GET | `/binflow/api/security/groups` | 组列表 | M4 |
| GET | `/binflow/api/security/groups/{name}` | 组详情 | M4 |
| PUT | `/binflow/api/security/groups/{name}` | 创建或更新组（创建 201 / 更新 200） | M4 |
| POST | `/binflow/api/security/groups/{name}` | 改描述 | M4 |
| DELETE | `/binflow/api/security/groups/{name}` | 删组（被 target 引用 → 409） | M4 |
| POST | `/binflow/api/security/token` | 签发 Access Token（admin only） | M1 |
| POST | `/binflow/api/security/token/revoke` | 吊销 Token（admin only） | M1 |
| POST | `/binflow/api/v1/permissions` | 创建 Permission Target | M1 |
| GET | `/binflow/api/v1/permissions` | 列出 Permission Targets | M4 |
| DELETE | `/binflow/api/v1/permissions/{name}` | 删除 Permission Target | M1 |

### SR: 搜索域

| 方法 | 路径 | 参数 | 语义 | 里程碑 |
|---|---|---|---|---|
| GET | `/binflow/api/search/artifact` | `name=`（必填）、`repos=a,b` | 按名称子串搜索（SQL LIKE，权限过滤） | M4 |
| GET | `/binflow/api/search/checksum` | `sha1=/md5=/sha256=`（至少一）、`repos=a,b` | 按 checksum 精确搜索 | M1 |
| GET | `/binflow/api/search/props\|users\|artifactory\|pattern\|badge` | — | **404** 有意不做 | M1 |

### SR: 仓库管理域

| 方法 | 路径 | 语义 | 里程碑 |
|---|---|---|---|
| GET | `/binflow/api/repositories` | 仓库列表（admin only） | M1 |
| GET | `/binflow/api/repositories?type=&packageType=` | 过滤列表 | M1 |
| GET | `/binflow/api/repositories/{key}` | 单仓配置 | M1 |
| PUT | `/binflow/api/repositories/{key}` | 建仓（创建） | M1 |
| POST | `/binflow/api/repositories/{key}` | 改仓（更新配置） | M1 |
| DELETE | `/binflow/api/repositories/{key}` | 删仓（含可选 `?deleteContent`） | M1 |

### 系统端点

| 方法 | 路径 | 语义 | 里程碑 |
|---|---|---|---|
| GET | `/binflow/api/system/ping` | 存活探测（免认证） | M1 |
| GET | `/binflow/api/system/version` | 版本信息（免认证） | M1 |
| GET | `/binflow/api/v1/health` | 健康面板（admin only） | M1 |
| GET | `/binflow/api/v1/storage/stats` | 全实例存储统计（admin only） | M1 |
| GET | `/binflow/api/v1/storage/usage/{repo}` | 单仓配额用量（admin 或有 read 授权） | M4 |
| GET | `/binflow/api/v1/audit` | 审计日志查询（admin only） | M4 |
| POST | `/binflow/api/v1/system/gc` | 触发 GC（admin only，同步执行） | M4 |

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

## `/api/v1` 自有端点

BinFlow 在 Artifactory 兼容端点之外增加了一批自有端点（以 `/api/v1` 前缀标记），实现差异化能力：

| 端点 | 方法 | 说明 |
|---|---|---|
| `/binflow/api/v1/health` | GET | 实例健康详情（admin only） |
| `/binflow/api/v1/storage/stats` | GET | 全实例 blob/字节统计（admin only） |
| `/binflow/api/v1/storage/usage/{repo}` | GET | 单仓配额用量（admin 或有 read 授权） |
| `/binflow/api/v1/audit` | GET | 审计日志查询（admin only） |
| `/binflow/api/v1/system/gc` | POST | 触发 GC（同步执行，dry-run/apply） |
| `/binflow/api/v1/session` | POST/GET/DELETE | 控制台会话管理 |
| `/binflow/api/v1/permissions` | POST/GET/DELETE | Permission Target CRUD |

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
# 签发 token（admin only，可指名替目标用户签发）
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
| 签发 | admin only（M1），命名替目标用户签发 |
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

# 409 — 删除的组被权限引用
HTTP/1.1 409 Conflict
Content-Type: text/plain; charset=utf-8

Cannot delete group 'devs': it is referenced by permission target(s): devs-rw, jane-rd. Remove the group from those targets first.
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

- 各协议接入指南：[Docker](docker-registry.md) · [Maven](integrations/maven.md) · [npm](integrations/npm.md) · [PyPI](integrations/pypi.md)
- 管理操作：[治理指南](admin/governance.md) · [权限管理](admin/groups-permissions.md) · [备份恢复](admin/backup-restore.md)
- 常见问题与排障：[FAQ](faq.md)