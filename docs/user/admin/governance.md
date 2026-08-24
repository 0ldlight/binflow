---
title: 治理：审计、GC 与配额
sidebar_position: 42
---

# 治理：审计、GC 与配额

> 适用版本：M4（治理面；PRD milestone-4 v1.2 FR-29/30/31/24、ADR-0015 勘误后）；**M9 增补**：用户删除闭环（DELETE 三护栏/级联/确定性 404，T-251/T-257）与 last-admin 竞窗运营提醒（T-273 候选背景）。
> 本文全部命令在本机 scratch 实例（commit `7593d8e`）上复跑：审计过滤/词表、GC dry-run→apply 与互斥 409、pattern 409 双态、配额 413 与幂等重传豁免均按预期（蓝本 T-103 W22~W27/W12a，报告见 `reports/agents/T-103-qa.md` §2.2/§2.5~§2.7）。M9 用户删除链（成功/四护栏/重复删 404/token 即时 401/user.delete 审计/级联组员清空）在 HEAD 构建的 scratch 实例（2026-08-25）上 curl 复验全过。

治理四件事：**审计**（谁在何时动了什么）、**GC**（回收无引用 blob）、**配额**（仓库容量上限）、**路径模式**（仓库接纳哪些路径）。前三个都有控制台页面；本文以 REST/CLI 为主面（脚本可完全等效），页面走查见[控制台指南](../console.md)。

## 审计

### 查询

`GET /api/v1/audit`（admin only；未认证 401、非 admin 403）：

```bash
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?action=gc.run&limit=2"
# {"events":[{id,time,actor,action,repo,path,detail},...],"nextCursor":"..."}
```

| 参数 | 说明 |
|---|---|
| `repo` / `actor` / `action` | 等值过滤 |
| `since` / `until` | 时间窗，RFC3339；**闭开区间** `[since, until)` |
| `limit` | 默认 100、上限 1000（超限 400） |
| `cursor` | keyset 游标（不透明，取上一页 `nextCursor`）；结果**倒序**（最新在前） |

`detail` 为对象（如 `quota.exceeded` 的 `{actor, repo, path, used, quota}`）；`time` 为 RFC3339 UTC。

### 词表与脱敏

M4 审计动作全集（可作 `action=` 过滤值；M7 增补 `user.role.change`）：

| 族 | 动作 |
|---|---|
| 制品 | `deploy`（上传/发布）、`download`、`delete` |
| 仓库 | `repo.create`、`repo.update`、`repo.delete` |
| 安全 | `group.create`、`group.update`、`group.delete`、`group.member`（成员集变更）、`permission.create`、`permission.update`、`permission.delete`、`password.change`、`user.role.change`（M7：角色分配/升降，detail 含 user/old/new）、`user.delete`（M9：仅成功删除记录，detail 含 user） |
| 治理 | `gc.run`、`quota.exceeded`、`export.run`、`import.run` |
| 会话 | `login.success`、`login.failed` |
| token | `token.issue`、`token.revoke`（detail 含指纹/subject/TTL；step-up 路径的 `token.issue` 另含 `step_up` 维度，见 [step-up 指南](token-step-up.md#审计)） |

历史注记：M4 曾登记「token 签发/吊销不落审计」缺口，现已修复（`token.issue` / `token.revoke` 均落审计，scratch 实例 2026-08-24 实测）。

脱敏红线（QA 全量导出 grep 验证）：口令字面量、token 明文、`Authorization` 头**零命中**——审计里永远看不到这些值。

### 只追加

审计没有写面：对 `/api/v1/audit*` 的 PUT/DELETE/POST 一律 **404**（路由层即无此面）；元数据层无 UPDATE/DELETE 代码路径。归档留存请走数据库备份（见[备份手册](backup-restore.md)），不要试图「清理」审计。

## 搜索

跨仓统一入口（结果**按调用者权限过滤**——无 read 的仓库不出现；admin 全见）：

```bash
# 名称子串（SQL LIKE；缺 name → 400）
curl -su admin:$ADMIN_PW "$BASE/binflow/api/search/artifact?name=w.bin" 
# {"results":[{repo,path,size,...}]}；repos= 逗号分隔限定仓

# checksum 精确反查（sha256/sha1/md5 至少一个；跨仓去重可见——同 blob 多路径全返回）
SHA=$(shasum -a 256 w.bin | cut -d' ' -f1)
curl -su admin:$ADMIN_PW "$BASE/binflow/api/search/checksum?sha256=$SHA"
```

M4 边界（**404，有意不做**）：`/api/search/props|users|artifactory|pattern|badge`、AQL、gavc 结构化检索、`*` 通配。匿名实例关闭时未认证搜索当前返回 403（与 /api 家族 401 姿态尚不统一，已登记勘误）。

## GC（垃圾回收）

GC 回收**无引用**的 blob（删除制品后，同内容仍被其它路径引用的 blob **幸存**——去重语义）。两步操作：**先 dry-run 看报告，再显式 apply**。

```bash
# dry-run（apply 缺省 false；只报告不动数据）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/system/gc \
  -H 'Content-Type: application/json' -d '{"apply":false,"graceHours":0}'
# {"candidateCount":1,"candidateBytes":24,"deletedCount":0}

# apply（真正删除；控制台侧对应「输入实例名」二次确认）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/system/gc \
  -H 'Content-Type: application/json' -d '{"apply":true,"graceHours":0}'
# {"candidateCount":1,"candidateBytes":24,"deletedCount":1}
```

| 面 | 行为 |
|---|---|
| admin only | 非 admin 403、未认证 401 |
| `graceHours` 缺省 | 用配置 `storage.gc_grace_hours`（默认 **24h**）——刚删的孤儿在宽限期内**不算候选**（基准 = blob mtime，防误删正在写的会话） |
| `graceHours: 0` | **无宽限**（REST 显式 0 = 立即可回收）。注意与 CLI 的差异：CLI `--grace-hours 0` 仍是「用配置值」（flag 哨兵语义），两面有意不同 |
| `graceHours` 越界 | 负值或 >876000 → 400 |
| dry-run | `deletedCount` 恒 0、stats 不变 |
| apply | `deletedCount` 为实际删除数；被引用 blob 幂等幸存；再 dry-run 应为 0 候选 |
| 审计 | 每次成功运行（含 dry-run）落 `gc.run`，detail 含 apply/graceHours/候选数/释放字节 |
| 同步执行 | M4 REST 触发为**同步**（请求返回即完成；大库请用 CLI 兜底，见下） |
| 状态查询端点 | **无**（`GET /api/v1/system/gc` 不做）——上次运行经审计查：`GET /api/v1/audit?action=gc.run` |

**互斥锁（409 的含义）**：GC、export、import 三个维护操作共用 data 目录级跨进程文件锁（`<data_dir>/.maintenance.lock`）。锁被他人持有时：

- export 运行中 POST gc → **409**，message 含 `export in progress` 与持锁进程信息；
- GC 运行中再触发 GC → **409**（`another gc run is in progress`）；
- 反向：GC/import 运行中执行 export/import CLI → **退出码非 0**。

409 不代表数据问题——等当前操作完成后重试即可。

CLI 兜底（超长库 / 排障；dry-run 缺省）：

```bash
binflow-server gc -c binflow.yaml                    # dry-run，grace 取配置（默认 24h）
binflow-server gc -c binflow.yaml --apply --grace-hours 1   # apply，grace 1 小时
```

## 仓库治理字段：路径模式与配额（仅 local 仓）

建仓/改仓时三个治理字段（remote / virtual 仓不适用——这两型不落自有内容）：

| 字段 | 默认 | 说明 |
|---|---|---|
| `includesPattern` | `**/*` | 接纳的路径模式；不匹配的上传 → **409** |
| `excludesPattern` | 空 | 排除的路径模式；**excludes 优先**——命中即拒，即使也匹配 includes |
| `quotaBytes` | `0`（不限） | 仓库逻辑字节上限（nodes.size 之和，非去重物理量）；正整数或 0；负值 → 400 |

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/pattern-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"generic",
       "includesPattern":"**/*.jar","excludesPattern":"secret/**","quotaBytes":1073741824}' \
  -o /dev/null -w '%{http_code}\n'    # 200
```

### 路径模式：409 / 404 双值码

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/pattern-local/a/t.txt --data-binary @t.txt -w '\n%{http_code}\n'
# 409: Repository 'pattern-local' rejected deployment of 'a/t.txt':
#      the path does not match includesPattern '**/*.jar' (excludesPattern 'secret/**').

curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/pattern-local/secret/x.jar --data-binary @x.jar -o /dev/null -w '%{http_code}\n'
# 409（excludes 优先命中）

curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/pattern-local/a/ok.jar --data-binary @ok.jar -o /dev/null -w '%{http_code}\n'
# 201
```

下载侧：命中 excludes 的路径与普通 miss **逐字相同**的 **404**（不泄露「存在但被拦」）。未配模式的仓库行为与历史版本逐字节一致。

### 配额：413 语义

```bash
# 建限额仓 → 上传至超限
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/tiny \
  -H 'Content-Type: application/json' -d '{"rclass":"local","packageType":"generic","quotaBytes":1024}' \
  -o /dev/null -w '%{http_code}\n'
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/tiny/a.bin --data-binary @800b.bin -o /dev/null -w '%{http_code}\n'  # 201
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/tiny/b.bin --data-binary @800b.bin -w '\n%{http_code}\n'
# 413: Repository 'tiny' quota exceeded: used 800 of 1024 bytes; the write to 'b.bin' needs 800 more bytes.
```

| 面 | 行为 |
|---|---|
| **原子拒绝** | 被拒路径 GET 404、usage 不变、无索引残留；docker 上传会话可弃零残留 |
| **幂等重传豁免** | 已在库内容的同路径同内容重传（delta=0）在**恰好顶满**时也放行 201——重试/断点续传不会因配额误伤 |
| 读/删不受限 | 超限后 GET 200、DELETE 204；删后 used 回落，再传即 201 |
| 秒传与 mount 同受限 | `X-Checksum-Deploy` 引用已有 blob 到新路径、docker cross-repo mount 同样过配额门 |
| 计量口径 | 本地仓 logical bytes；remote pull-through 落盘**不计量**（M4 暂行） |
| 审计 | 每次拒绝落 `quota.exceeded`（detail: actor/repo/path/used/quota）+ WARN 结构化日志 |
| 用量查询 | `GET /api/v1/storage/usage/{repo}` → `{repo, usedBytes, quotaBytes}`（admin 或有 read 授权用户） |

五协议客户端在 413 时的表现（真机实测）：docker push `exit 1` + `denied: Repository ... quota exceeded`；npm `E413`；mvn deploy `Failed ... status code: 413`；twine `HTTPError: 413 Error`；curl 直接呈现 message。**已知边界**（P2 观察）：docker 多层 push 中先成功落盘的小层（含 mount 的 config）、maven deploy 的 pom/metadata 可能先落——被拒的写本身原子、manifest 不落，但「先落且配额内的部分」属每写语义保留；此类残留不影响 catalog/索引可见性，需要时整仓删除回收。

## 控制台对应页面

M8 起治理域位于管理模式「治理」分组：审计日志 `/admin/governance/audit`（过滤 + 游标加载更多）、维护（GC）`/admin/governance/gc`（stats + dry-run 面板 + 输入实例名确认 apply）、配额 `/admin/governance/quotas`（每仓水位条 80% 黄/100% 红 + 行内编辑）、复制 `/admin/governance/replication`、备份/恢复 `/admin/governance/backup`——均消费与本文相同的 REST 面，脚本与界面行为可互证（页面测试即 API 测试）。页面走查见[控制台指南](../console.md#管理模式各域)；M7 及以前的 `/governance/*`、`/audit` 旧路径已随 M9 移除重定向窗口而失效——请改用上述新路径（对照表见[控制台指南 · 旧路径 → 新路径](../console.md#旧路径--新路径m9-起不再重定向)）。

## 用户管理

### 用户 CRUD

```bash
# 创建用户（PUT = create-or-replace，两态 201）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/users/jane \
  -H 'Content-Type: application/json' \
  -d '{"name":"jane","password":"<口令>","email":"jane@example.com","admin":false}' \
  -o /dev/null -w '%{http_code}\n'                     # 201

# 查询用户（返回不含口令字段）
curl -su admin:$ADMIN_PW $BASE/binflow/api/security/users/jane
# {"name":"jane","email":"jane@example.com","admin":false,"groups":[],"realm":"internal",...}

# 用户列表（M9 加宽：email/adminRole/enabled/groups 恒渲染）
curl -su admin:$ADMIN_PW $BASE/binflow/api/security/users
# [{"name":"admin","uri":"...","realm":"internal","source":"local","email":"","adminRole":"admin","enabled":true,"groups":[]}, ...]

# 部分更新（POST；指针区分缺省与显式空）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/users/jane \
  -H 'Content-Type: application/json' \
  -d '{"email":"jane@new.example.com","groups":["devs"]}' \
  -o /dev/null -w '%{http_code}\n'                     # 200
```

| 操作 | 注意 |
|---|---|
| 创建 | `email` **必填**（缺省 400）；`groups` 引用未知组 → 400 |
| 替换 | `PUT users` 已存在 → 201（create-or-replace），覆盖所有字段；未提供的字段不保留旧值 |
| 列表 | **M9 加宽**：条目含 `email`/`adminRole`/`enabled`/`groups`（恒渲染，空组 `[]`）——控制台用户页单请求成表 |
| 删除 | **M9 起提供**（三护栏/级联/确定性 404，见下节） |
| 禁用/复启 | `POST /api/security/users/{name}` body `{"enabled":false\|true}`——人员离场的**既定路径是禁用**（可逆、可审计），不是删除 |
| 改密 | 自己改：`PUT /api/security/password`；admin 改别人：`POST /api/security/users/{name}` 带 `password` 字段 |

### 删除用户（M9 起）

```bash
# 删除（admin only；成功 200 纯文本）
curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/api/security/users/victim
# The user: 'victim' has been removed successfully.

# 重复删除 → 确定性 404（有意非幂等）
curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/api/security/users/victim -w '\n%{http_code}\n'
# User not found
# 404
```

四道护栏（检查序固定，全 400 纯文本；第 1 道为 404 文本体——与 GET 单用户同形，**不是**无 body 404）：

| 护栏 | 文案（逐字） |
|---|---|
| 目标不存在 | 404 `User not found` |
| 内置 `admin` | 400 `Cannot delete the built-in admin user.`（种子账号是最终恢复路径，删除会使 `BINFLOW_ADMIN_PASSWORD` 重置路径失明） |
| 最后一个 admin | 400 `Cannot delete user '<name>'. There must be at least one user configured with admin privileges.` |
| 自删 | 400 `Cannot delete the current authenticated user.`（离场走禁用） |

**级联（同事务，不可恢复）**：剥该用户在全部 permission target 的授权行 → 删用户行 → 组员关系清空、**全部 token 与 web session 即时吊销**（已持有的 Bearer 下一次请求即 401，实测）；审计历史保留并新增 `user.delete` 事件（护栏拒绝不落审计）。与组删除的 409 保护是**有意不对称**：组是多成员策略对象，静默剥夺全员授权故拒绝；用户是单主体，级联即删除意图本身。

**重复删除 404 = 有意非幂等**（review 裁定）：Artifactory「重复删视为成功」的幂等形态是其并发窗口产物；BinFlow 取确定性 pre-probe——第二次 DELETE 得 404 就意味着「对象已被删」，调用方不要重试、不要把它当失败告警。控制台对应面为**输入用户名强确认**（列表行 + 编辑页危险区，文案明示级联不可恢复与非幂等），见[控制台指南](../console.md#用户与权限adminsecurity)。

### 用户管理风险：last-admin 竞窗（运营提醒）

「最后一个 admin 不可删」护栏的清点（census）在删除事务**之外**执行——两个 admin **并发互删**时，双方清点都看到「还有另一个 admin」，两笔删除可同时通过并提交，实例进入**零 admin** 状态（已登记 T-273 候选：census 折入单事务根治）。运营注意两点：

- **事前**：删除 admin 账号的变更窗口串行化（一次只删一个，删后确认仍有 admin 登录再进行下一笔）；日常避免把「唯一 admin」当常态。
- **事后恢复**：零 admin 后 `BINFLOW_ADMIN_PASSWORD` **重种无效**——种子逻辑只在 `admin` 行不存在时插入，既有行（即便已降权）永不覆盖。可恢复路径有二：① 停机后对元数据库做带外手术（SQLite：`UPDATE users SET role='admin', is_admin=1 WHERE username='admin';` 后重启，操作前先按[备份手册](backup-restore.md)留档）；② 从最近一次 export 备份恢复到空目录（用户/token/授权随行，会话需重登）。

> **重要**：用户详情响应**不含口令字段**（明文或哈希均不出现）。不存在 `hashedPassword` 字段。

### 组管理

组是权限模型的核心组织载体——admin 可以建组、入组、授权，但不能把组授予 admin 位（admin 是用户属性，不是可授予权限）。

```bash
# 建组（PUT 创建或更新）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/groups/devs \
  -H 'Content-Type: application/json' \
  -d '{"name":"devs","description":"开发组"}' -o /dev/null -w '%{http_code}\n'      # 201

# 创建与更新分态
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/groups/devs \
  -H 'Content-Type: application/json' \
  -d '{"name":"devs","description":"new desc"}' -o /dev/null -w '%{http_code}\n'   # 200（已存在，更新）

# 组列表
curl -su admin:$ADMIN_PW $BASE/binflow/api/security/groups
# [{"name":"devs","uri":"...","description":"new desc"}]

# 删除组
curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/api/security/groups/devs \
  -w '\n%{http_code}\n'
# 被权限引用 → 409: Cannot delete group 'devs': it is referenced by permission target(s): ...
# 无引用 → 200: The group: 'devs' has been removed successfully.

# 组员花名册（M9：单组按需查；字面 true 才开，空组 [] 恒非 null）
curl -su admin:$ADMIN_PW "$BASE/binflow/api/security/groups/devs?includeUsers=true"
# {"name":"devs","uri":"...","description":"...","userNames":["jane","u1"]}
```

组名规则：`[a-z][a-z0-9._-]*`（小写字母开头）；保留字 `anonymous` / `_system_` → 400。

### 权限模型

权限 target 定义**谁（principals）能对哪些仓库的哪些路径（patterns）执行哪些操作（actions）**：

```json
{
  "name": "devs-rw",
  "repos": ["generic-local"],
  "includePatterns": ["devs/**"],
  "excludePatterns": [],
  "principals": {
    "users": {},
    "groups": {
      "devs": ["read", "write"]
    }
  }
}
```

有效权限 = 用户直接授予 ∪ 所属各组授予（并集）。**逐请求现算**——移出组下一次请求即生效，无缓存窗口。

| 动作 | 说明 |
|---|---|
| `read` | 下载/查看/解析 |
| `write` | 上传/发布/部署 |
| `delete` | 删除/覆盖 |

> **组不能授予 admin 位**：admin 是用户属性（账户级），不是可授予的权限动作。admin 组成员中的非 admin 用户访问管理面仍是 403——这是有意设计，防止「建个组把自己提权」。

三步授权流：建组 → 建用户入组 → 建 permission target 引用组。完整操作与 curl 对账见[用户组与权限管理](groups-permissions.md)。

### 有效权限视图

`?permissions` 参数返回逐主体在指定路径上的有效权限（admin only，仅 local 仓）：

```bash
curl -su admin:$ADMIN_PW "$BASE/binflow/api/storage/generic-local/devs/w.bin?permissions"
# {"uri":"...","principals":{"users":{},"groups":{"devs":["r","w"]}}}
```

key 为主体名，value 为权限字母集合（r/w/d）。无任何权限的主体不出现。virtual/remote 仓 → 400。

## Token 审计

### Token 管理

```bash
# 签发 token（admin only，可指名替目标用户签发）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/token \
  -d 'grant_type=client_credentials&username=ci-bot'
# 200: {"access_token":"<64hex>","token_id":"<id>","expires_in":2592000,"scope":"api:*"}

# 吊销 token（admin only）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/token/revoke \
  -d "token_id=<上面的 token_id>"
# 200: {"message":"token revoked"}
```

| 属性 | 说明 |
|---|---|
| Token 格式 | 64 位 hex（256-bit），仅签发时明文返回一次 |
| 默认 TTL | 2592000 秒（30 天），`auth__token_default_ttl_hours` 可调 |
| Token 作用域 | M1 无 scope 模型——`api:*` 表示全量权限 |
| 吊销 | admin only，吊销后即时生效 |
| OAuth 错误体 | Token 端点的所有非 2xx 响应均为 OAuth 风格（`{"error":"...","error_description":"..."}`），非 E-01 errors[] 信封 |

### Token 审计现状

`token.issue` / `token.revoke` **均落审计事件**（M4 曾登记缺口，已修复并实测）：

```bash
# 签发事件（detail 含指纹 fingerprint、subject、ttl_seconds；step-up 路径另含 step_up/step_up_method）
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?action=token.issue&limit=2"
# 吊销事件（detail 含 token_id）
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?action=token.revoke&limit=2"
```

服务端结构化日志同时保留 token 操作行（WARN 级 `token revoked` 事件）——日志与审计双留痕。

### Docker Token 与 Token 同表

Docker 客户端经 `docker login` 换取的 distribution token 与管理面 `POST /api/security/token` 签发的 Access Token **同表存储**——管理面吊销对所有 token 即时生效（包括 docker 客户端的 Bearer token）。

## 下一步

- 备份与恢复（export/import 与 GC 的锁互斥关系）：[备份与恢复手册](backup-restore.md)
- 组与权限 target：[用户组与权限管理](groups-permissions.md)
- 各协议上传的客户端侧配置：[接入指南](/integrations)（docker/maven/npm/pypi/generic 各篇）
