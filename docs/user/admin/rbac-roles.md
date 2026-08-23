---
title: RBAC 角色与仓库级管理员
sidebar_position: 44
---

# RBAC 角色与仓库级管理员

> 适用版本：M7（角色三值模型 + `manage` 动作；PRD milestone-7 v1.1 FR-64/FR-65、ADR-0026/ADR-0028）。
> 本文全部命令在本机 scratch 实例（2026-08-23，`make build` 产物）上复跑：分配/回显/冲突、读面与变更面矩阵、数据面短路、同 Token 即时生效、manage 派生与覆盖集边界、审计、续传 curl 链均按预期（蓝本 T-221 QA V01~V11 全绿 + `make test-m7-resume` / `test-m7-resume-sigterm` 双臂 GREEN）。

M7 起权限模型分三层，各归其位：

| 层 | 承载 | 决定什么 |
|---|---|---|
| **角色**（users 行，闭集三值） | `adminRole` | 管理面（仓库/用户/组/权限/审计/GC/复制/配额）能看什么、能改什么 |
| **permission target 的 `manage` 动作** | target principals | 仓库级管理员——谁能管某个仓的配置与权限下放 |
| **permission target 的 `read/write/delete`** | target principals | 内容面——谁能读写删哪个仓的哪些路径（M1 起语义零变更） |

## 角色三值模型

角色是**闭集**，取值恰为三个（snake 形）：

| 角色 | 语义 | 典型使用者 |
|---|---|---|
| `user` | 缺省。管理面全拒，内容面与仓库域走 permission targets | 开发者、CI 账号 |
| `readonly_admin` | 管理读面全通、一切变更面 403；**数据面全域只读**（角色短路） | 值班 SRE、内审员 |
| `admin` | 全权（≡ 原 admin 布尔；隐式含全部能力与 manage） | 平台管理员 |

不存在自定义角色 / 角色继承——新增角色属架构变更。admin 布尔字段保留为兼容视图：`admin=true ⇔ adminRole=admin`。

### 角色 × 能力矩阵

| 能力面 | `user` | `readonly_admin` | `admin` |
|---|---|---|---|
| 管理读面（见下方 11 项清单） | 403 | **全通** | 全通 |
| 管理写面（建/删仓、用户/组/权限写、token 吊销、GC〔含 dry-run〕、配额写、复制写） | 403 | **全拒 403（零副作用）** | 全通 |
| 数据面读（任意仓制品 GET） | 走 target `read` | **恒放行**（全域只读短路） | 恒放行 |
| 数据面写/删（制品 PUT/DELETE、docker push） | 走 target `write`/`delete` | **恒拒 403**（docker push token 逐请求 403，与 REST 面同源） | 恒放行 |
| 仓库级管理派生（`manage`） | 走 target `manage` | 恒拒（w/d/m 硬拒） | 隐式全含 |
| 角色字段写（`adminRole`/`admin`） | 403 | 403 | **仅 admin** |
| 自助面（改密、自铸 token、whoami/session） | 可 | 可（自铸仅限本人；为他人铸 403） | 可 |

### readonly_admin 的短路语义（重要）

readonly_admin 的内容面与仓库域求值**不查 permission targets**——target 行对其无任何效果。把 readonly_admin 加进带 write 的组**不会**让他能写：组合的结果是**无效**（静默无效果），不是报错。这是有意设计：角色名是安全不变量，「只读管理员还能删」的矛盾配置在求值层被结构性排除。

```bash
# 数据面全域只读短路（readonly_admin 对从未授权的仓）：
curl -su alice:<alice口令> $BASE/binflow/generic-local/roa/x.bin -o /dev/null -w '%{http_code}\n'  # 200（读恒放行）
curl -su alice:<alice口令> -X PUT $BASE/binflow/generic-local/roa/y.bin --data-binary y \
  -o /dev/null -w '%{http_code}\n'                                                                  # 403（写恒拒）
curl -su alice:<alice口令> -X DELETE $BASE/binflow/generic-local/roa/x.bin -o /dev/null -w '%{http_code}\n'  # 403
```

readonly_admin 管理读面清单（11 项；全部为 GET）：

```text
/api/repositories、/api/repositories/{key}
/api/v1/health
/api/security/users（列表/单查）、/api/security/groups（列表/单查）
/api/v1/permissions、/api/v1/audit
/api/v1/replications、/api/v1/replication/status
/api/v1/storage/stats、/api/v1/storage/migration
```

> `storage/migration` 在未装配双写的实例上过授权门后返回 **501**（`migration is not configured`）——这已通过授权门，属正常形态。

变更面一律 403 且**零副作用**——包括 GC 的 dry-run（`apply=false` 也是写路由）。

## 分配角色：`adminRole` wire 字段

用户创建/替换（`PUT /api/security/users/{name}`）与部分更新（`POST /api/security/users/{name}`）body 增可选字段 **`adminRole`**（camelCase 字段名；值 snake 形与 DB 一致）。仅 admin 可写。

```bash
export BASE=http://localhost:8080
export ADMIN_PW=<你的管理员口令>

# 分配（建号即带角色；亦可在部分更新臂改角色）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/users/alice \
  -H 'Content-Type: application/json' \
  -d '{"name":"alice","email":"alice@t.io","password":"<alice口令>","adminRole":"readonly_admin"}' \
  -o /dev/null -w '%{http_code}\n'          # 201
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/users/alice \
  -H 'Content-Type: application/json' -d '{"adminRole":"readonly_admin"}' \
  -o /dev/null -w '%{http_code}\n'          # 200（部分更新臂）

# 回显：单查详情与登录/whoami 都带同名字段
curl -su admin:$ADMIN_PW $BASE/binflow/api/security/users/alice | jq '{name,admin,adminRole}'
# {"name":"alice","admin":false,"adminRole":"readonly_admin"}
curl -su alice:<alice口令> -X POST $BASE/binflow/api/v1/session \
  -H 'Content-Type: application/json' -d '{"username":"alice","password":"<alice口令>"}' | jq .adminRole
# "readonly_admin"

# 冲突与非法值 → 400 纯文本（用户管理族错误体）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/users/bob \
  -H 'Content-Type: application/json' \
  -d '{"name":"bob","email":"bob@t.io","password":"<bob口令>","admin":false,"adminRole":"admin"}' \
  -w '\n%{http_code}\n'
# conflicting 'admin' and 'adminRole' fields: admin=false is incompatible with adminRole="admin" (admin=true is equivalent to adminRole=admin)
# 400
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/users/bob \
  -H 'Content-Type: application/json' \
  -d '{"name":"bob","email":"bob@t.io","password":"<bob口令>","adminRole":"root"}' -w '\n%{http_code}\n'
# unknown adminRole "root" (supported: user, readonly_admin, admin)
# 400
```

规则要点：

- **两写法等价映射**：`adminRole` 缺省时从 `admin` 布尔推导（`admin=true ⇔ adminRole=admin`）；两者同时出现且矛盾 → 400（见上）。
- **即时生效**：角色变更对该用户的**存量 Token 同样立即生效**（无重启、无延迟窗口、无需换发 token）——同一条 Bearer：

```bash
TOK=$(curl -su victor:<victor口令> -X POST $BASE/binflow/api/security/token \
  -d 'grant_type=client_credentials' | jq -r .access_token)
curl -s -H "Authorization: Bearer $TOK" $BASE/binflow/api/security/users -o /dev/null -w '%{http_code}\n'  # 403（user 期）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/users/victor \
  -H 'Content-Type: application/json' -d '{"adminRole":"readonly_admin"}' -o /dev/null  # 200
curl -s -H "Authorization: Bearer $TOK" $BASE/binflow/api/security/users -o /dev/null -w '%{http_code}\n'  # 200（同一 token）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/users/victor \
  -H 'Content-Type: application/json' -d '{"adminRole":"user"}' -o /dev/null             # 200
curl -s -H "Authorization: Bearer $TOK" $BASE/binflow/api/security/users -o /dev/null -w '%{http_code}\n'  # 403
```

- **越权**：非 admin（含 readonly_admin）写任何用户的角色字段 → 403，目标角色不变。
- **组不带角色**：角色只在用户行，组 principals 永远不能授予 admin 或角色（M4「组无 admin 位」断言维持）。

## 审计：`user.role.change`

每次角色分配或升降（无论经 `adminRole` 还是 `admin` 布尔臂）记一条 `user.role.change`（actor + 目标用户 + old/new；新建带非缺省角色时 old 为空串）：

```bash
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?action=user.role.change&limit=4" | jq -c '.events[] | {actor,action,detail}'
# {"actor":"admin","action":"user.role.change","detail":{"new":"user","old":"readonly_admin","user":"victor"}}
# {"actor":"admin","action":"user.role.change","detail":{"new":"readonly_admin","old":"user","user":"victor"}}
# {"actor":"admin","action":"user.role.change","detail":{"new":"readonly_admin","old":"","user":"alice"}}
```

## 仓库级管理员：`manage` 动作

permission target 的 principals 动作集从 `read|write|delete` 扩为 **`read|write|delete|manage`**（对齐 Artifactory ACE 动作集子集）。给组或用户授 `manage` + target 命中某仓 = 该主体成为**这些仓的仓库级管理员**。既有 target 无 manage 位 = 行为逐字不变。

`manage` 的 target 匹配**只看 `repos[]`**——includes/excludes 不参与（manage 是仓库配置权，无路径子域）。

### 授权与下放（三步）

```bash
# 1. 建 target：app-admins 组对 app-local 全权 + manage
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/permissions \
  -H 'Content-Type: application/json' \
  -d '{"name":"t-app","repos":["app-local"],"includePatterns":["**"],
       "principals":{"users":{},"groups":{"app-admins":["read","write","delete","manage"]}}}' \
  -o /dev/null -w '%{http_code}\n'       # 201

# 2. carol（app-admins 成员，无需任何直接授权）自此可编辑覆盖集内的 target——
#    例如给 dave 开 app-local 的只读：
curl -su carol:<carol口令> -X POST $BASE/binflow/api/v1/permissions \
  -H 'Content-Type: application/json' \
  -d '{"name":"t-app","repos":["app-local"],"includePatterns":["**"],
       "principals":{"users":{"dave":["read"]},"groups":{"app-admins":["read","write","delete","manage"]}}}' \
  -o /dev/null -w '%{http_code}\n'       # 201（create-or-replace；权限下放不再需要平台 admin）

# 3. dave 立即可读（无需重启）：
curl -su dave:<dave口令> $BASE/binflow/app-local/lib.a -o /dev/null -w '%{http_code}\n'  # 200
curl -su dave:<dave口令> -X PUT $BASE/binflow/app-local/other.bin --data-binary z -o /dev/null -w '%{http_code}\n'  # 403（只有 read）
```

> permission target 的编辑臂是 **`POST /api/v1/permissions`（create-or-replace，201）**——没有 `PUT /api/v1/permissions/{name}` 路由（404）。

### 覆盖集规则

manage 持有者可编辑的 target 须满足：**target 的 `repositories` ⊆ 其 manage 覆盖仓集**。同名替换是整体替换，判定取 **body ∪ 存量 target 的 repos** 的并集——防「换壳吊销他人在越界仓上的既有授权」：

```bash
# 越界（引用覆盖集外的仓）→ 403：
curl -su carol:<carol口令> -X POST $BASE/binflow/api/v1/permissions \
  -H 'Content-Type: application/json' \
  -d '{"name":"t-other","repos":["other-local"],"includePatterns":["**"],"principals":{"users":{"dave":["read"]}}}' \
  -o /dev/null -w 'disjoint: %{http_code}\n'          # 403
# 部分交集（一半在覆盖集内）同样 403；空集 repos=[] 也 403（空集不构成「全在覆盖集内」）

# 同名替换并集腿：admin 先建 t-ent（repos=[other-local]），carol 用 body=[app-local] 同名替换
# → 403 且权限清单逐字不变（存量 other-local ⊄ 覆盖集，吊销洞被并集判定堵死）
```

DELETE 臂同理：被删 target 的 repo 集取自存量行，越界 → 403。

### manage 能做什么 / 不能做什么

| 能（覆盖集内） | 不能（全部 403） |
|---|---|
| 读单仓配置 `GET /api/repositories/{key}` | **建仓**（PUT 而 repo 不存在）与**删仓**（DELETE）——全局 admin-only |
| 编辑仓配置/配额（`POST /api/repositories/{key}`、PUT 替换臂） | 全局仓库列表 `GET /api/repositories`（过滤列表 M8+ 评估） |
| 编辑 ⊆ 覆盖集的 permission target（增删人、调权限） | 管理面一切：用户/组/角色写、token 吊销、GC（含 dry-run）、复制写 |
| 读配额用量 `GET /api/v1/storage/usage/{repo}` | — |
| 读制品授权位 `?permissions` 视图（字母集含 `m`；路径探针本身仍需内容 read） | — |

**`manage` 不是数据面权限**：不隐含 read/write/delete——制品读写仍需显式授予（与 Artifactory 动作正交语义一致）。仅授 manage 的用户：

```bash
# carol2 只在 t-mgmt 拿到 manage（无 r/w/d）：
curl -su carol2:<carol2口令> $BASE/binflow/api/repositories/app-local -o /dev/null -w 'repo-detail: %{http_code}\n'  # 200
curl -su carol2:<carol2口令> $BASE/binflow/app-local/lib.a -o /dev/null -w 'get-artifact: %{http_code}\n'             # 403
curl -su carol2:<carol2口令> -X PUT $BASE/binflow/app-local/x.bin --data-binary x -o /dev/null -w 'put-artifact: %{http_code}\n'  # 403
curl -su carol2:<carol2口令> $BASE/binflow/api/v1/storage/usage/app-local -o /dev/null -w 'usage: %{http_code}\n'      # 200
```

`manage` 永不进入 docker token 的 scope 词表——manage 持有者的 push/pull 仍由内容面 r/w/d 决定。

## OIDC / LDAP 的 readonly 组映射

联邦用户每次登录由 IdP 权威重写角色，本地手改会被下次登录覆盖——readonly 管理员组与 admin 组同构供给，配置键与既有 `admin_group` 同节同形：

| 键 | 值 | 说明 |
|---|---|---|
| `auth.oidc.<provider>.readonly_group` | 组名（group claim 值） | OIDC；环境变量 `BINFLOW_AUTH_OIDC_READONLY_GROUP` |
| `auth.ldap.readonly_group` | 组 **DN** | LDAP；环境变量 `BINFLOW_AUTH_LDAP_READONLY_GROUP` |

```yaml
auth:
  ldap:
    enabled: true
    admin_group: cn=binflow-admins,ou=groups,dc=example,dc=org      # 命中 → admin
    readonly_group: cn=binflow-auditors,ou=groups,dc=example,dc=org # 命中 → readonly_admin
```

求值阶梯（每次登录）：`admin_group` 命中 → `admin` **>** `readonly_group` 命中 → `readonly_admin` **>** 其余 → `user`。两键缺省不配置 = 行为与原先完全一致。详见 [OIDC 配置](../guides/oidc-config.md) / [LDAP 配置](../guides/ldap-config.md)。

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 400 `conflicting 'admin' and 'adminRole' fields: ...` | `admin` 布尔与 `adminRole` 矛盾（含 admin 缺省 false + adminRole=admin） | 二选一；`admin=true` 等价 `adminRole=admin` |
| 400 `unknown adminRole "..." (supported: user, readonly_admin, admin)` | 值拼错（如 kebab 形 `read-only-admin`） | 值用 snake 形 |
| 403（readonly_admin 的一切写操作） | 角色设计如此（含 GC dry-run、配额写） | 需要变更能力 → 提升为 admin |
| 403 `administrator privileges required`（user/readonly_admin 打管理面） | 管理面门（六能力）拒绝 | 按需授予角色或改用 manage 下放 |
| 403（manage 持有者建仓/删仓/编辑越界 target） | 建/删仓与越界不下放（无提权链不变量） | 由平台 admin 操作，或把仓纳入其覆盖集 |
| readonly_admin 仍被某 target 授了 write 却不能写 | 角色短路：target 对 readonly_admin 无效果（组合无效，静默） | 写需求 → 用 user 角色 + target |

## 下一步

- permission target 基础（principals/patterns/并集/即时生效）：[用户组与权限管理](groups-permissions.md)
- Token 铸造二次认证（session 臂铸 Token 的 step-up 门）：[step-up 指南](token-step-up.md)
- 审计查询与词表：[治理指南](governance.md)
- docker 上传跨重启续传（另一项 M7 能力）：[Docker 接入指南](../docker-registry.md#大层上传中断续传跨重启)
