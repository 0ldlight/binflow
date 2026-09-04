---
title: 用户组与权限管理
sidebar_position: 41
---

# 用户组与权限管理

> 适用版本：M4（groups 域 + 权限继承；PRD milestone-4 v1.2 FR-27/FR-28、ADR-0014 决策 2）；动作集 `manage` 与用户字段 `adminRole` 为 **M7 增补**（PRD milestone-7 v1.1 FR-64/FR-65、ADR-0026，见 [RBAC 指南](rbac-roles.md)）。
> 本文全部命令在本机 scratch 实例（commit `7593d8e`）上复跑：三步授权流、并集、即时生效、409 删除保护、`?permissions` 视图均按预期（蓝本 T-103 W17~W21/W40，报告见 `reports/agents/T-103-qa.md` §2.4）。

M4 起权限模型支持**组**：permission target 的 principals 双栏（users + groups），用户的有效权限 = 直接授予 ∪ 所属各组授予，逐请求现算——**移出组下一次请求即生效**，无重启无窗口。

模型与 Artifactory 一致（术语不变）：permission target = `{name, repos[], includePatterns[], excludePatterns[], principals{users, groups}}`，动作 read / deploy-cache / annotate / delete / **manage**（五值闭集——`write` 拆分为 `deploy-cache` 与 `annotate` 两个正交位，manage 是仓库级管理员派生位，不是内容读写，见[下文](#动作动词read--deploy-cache--annotate--delete--manage)）；**excludes 优先**；admin 隐式拥有全部权限（不进矩阵）。

## 三步授权流（组 → 用户入组 → target）

```bash
export BASE=http://localhost:8080
export ADMIN_PW=<你的管理员口令>

# 1. 建组（admin only；创建 201 无 body，已存在为更新 200）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/groups/devs \
  -H 'Content-Type: application/json' \
  -d '{"name":"devs","description":"开发组"}' -o /dev/null -w '%{http_code}\n'   # 201

# 2. 建用户并入组（email 必填；groups 引用不存在的组 → 400）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/users/jane \
  -H 'Content-Type: application/json' \
  -d '{"name":"jane","password":"<jane口令>","email":"jane@example.com","groups":["devs"]}' \
  -o /dev/null -w '%{http_code}\n'                                                # 201

# 3. 建 permission target：groups 走 principals.groups（动作数组 read/write/delete/manage）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/permissions \
  -H 'Content-Type: application/json' \
  -d '{"name":"devs-rw","repos":["generic-local"],"includePatterns":["devs/**"],
       "principals":{"users":{},"groups":{"devs":["read","write"]}}}' \
  -o /dev/null -w '%{http_code}\n'                                                # 201
```

验证：jane（devs 成员，本人无任何直接授权）现在可以写 `devs/**`：

```bash
curl -su jane:<jane口令> -X PUT $BASE/binflow/generic-local/devs/w.bin --data-binary @w.bin \
  -o /dev/null -w 'put: %{http_code}\n'    # 201（组授予 write）
curl -su jane:<jane口令> -X DELETE $BASE/binflow/generic-local/devs/w.bin \
  -o /dev/null -w 'del: %{http_code}\n'    # 403（组没给 delete）
curl -su jane:<jane口令> -X PUT $BASE/binflow/generic-local/other/o.bin --data-binary @o.bin \
  -o /dev/null -w 'put: %{http_code}\n'    # 403（不在 devs/** 模式内）
```

## 并集与即时生效

- **并集**：user 直接授予与所属组授予取并集。下例 jane 直接有 read+delete、经组有 write——三个动作全通：

```bash
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/permissions \
  -H 'Content-Type: application/json' \
  -d '{"name":"jane-rd","repos":["generic-local"],"includePatterns":["union/**"],
       "principals":{"users":{"jane":["read","delete"]},"groups":{"devs":["write"]}}}' \
  -o /dev/null -w '%{http_code}\n'    # 201；此后 jane 对 union/** put/get/delete 全通过
```

- **即时生效**：成员变更逐请求现算，无缓存窗口。

```bash
# 移出组（POST /{name} 部分更新：显式 groups:[] 清空；缺 groups 字段则不动）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/users/jane \
  -H 'Content-Type: application/json' -d '{"groups":[]}' -o /dev/null -w '%{http_code}\n'  # 200
# 同一刻起 jane 对 devs/** 的写立即 403（原先允许的读若仅来自组也一并 403）
curl -su jane:<jane口令> -X PUT $BASE/binflow/generic-local/devs/w2.bin --data-binary @w.bin \
  -o /dev/null -w '%{http_code}\n'    # 403
```

## 动作动词：read / deploy-cache / annotate / delete / manage

`write` 拆分为两个正交位：**`deploy-cache`** 管内容字节面（部署/上传），**`annotate`** 管元数据面（属性写）。一位不隐含另一位：

| 动词 | 控制什么 | 不控制什么 |
|---|---|---|
| `read` | 内容读（下载/解析） | — |
| `deploy-cache` | 内容写（部署/上传/覆盖） | **属性写**——无 annotate 时 `?properties` 的 PUT/DELETE 403 |
| `annotate` | 属性写（`?properties` PUT/DELETE 同一门；path-plane——include/exclude patterns 生效） | 内容字节面——纯 annotate 持有者不能部署/删除制品 |
| `delete` | 内容删 | — |
| `manage` | 仓库级管理员派生位（语义见 [RBAC 指南](rbac-roles.md)） | 不是数据面权限；**m 不隐含 a**（无特权链不变量）；docker token scope 词表不收 |

wire 规则（全部实测）：

- **写入收词**：五个正词 + `write` 别名收一轮（= `deploy-cache`，**不附带 annotate**）；未知词 400 `unknown permission action "..." (supported: read, deploy-cache, annotate, delete, manage; write is accepted as a deploy-cache alias)`。
- **GET 回显**：恒正名单单形，序固定 `read, deploy-cache, annotate, delete, manage`（与提交序无关）。
- **`?permissions` 视图字母集**：`r/w/d/m/a`——annotate 渲染为 `a`。
- **属性写 403 文案**：`permission denied: writing properties requires annotate access on the item`。

```bash
# ① write 别名收词、回显正名单（实测）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/permissions \
  -H 'Content-Type: application/json' \
  -d '{"name":"legacy-w","repos":["generic-local"],
       "principals":{"users":{"annie":["read","write"]}}}' -o /dev/null -w '%{http_code}\n'   # 201
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/permissions
# legacy-w 行回显：{"users":{"annie":["read","deploy-cache"]},"groups":{}} —— 别名收词、无 annotate

# ② deploy-cache 无 annotate → 属性写 403，内容写 201（实测）
curl -su annie:<口令> -X PUT "$BASE/binflow/api/storage/generic-local/legacy/old.bin?properties=env=dev" \
  -o /dev/null -w '%{http_code}\n'
# 403 {"errors":[{"status":403,
#       "message":"permission denied: writing properties requires annotate access on the item"}]}
curl -su annie:<口令> -X PUT $BASE/binflow/generic-local/legacy/old.bin --data-binary @old.bin \
  -o /dev/null -w '%{http_code}\n'    # 201

# ③ annotate 无 deploy-cache → 属性写 204，内容写 403（实测）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/permissions \
  -H 'Content-Type: application/json' \
  -d '{"name":"acme-deploy","repos":["generic-local"],"includePatterns":["acme/**"],
       "principals":{"users":{"annie":["read","deploy-cache"],"archie":["read","annotate"]}}}' \
  -o /dev/null -w '%{http_code}\n'    # 201
curl -su archie:<口令> -X PUT "$BASE/binflow/api/storage/generic-local/acme/app.bin?properties=env=prod" \
  -o /dev/null -w '%{http_code}\n'    # 204
curl -su archie:<口令> -X PUT $BASE/binflow/generic-local/acme/archie.bin --data-binary @x \
  -o /dev/null -w '%{http_code}\n'    # 403（permission denied）
```

**存量授权的迁移语义（零提权等价）**：升级到动词拆分版本时，存量 `write` 行**自动回填 `annotate`**——既有「write 覆盖属性写」的行为原样保持（上面 ② 的路径在回填后 204），迁移可回滚（can_write 全程不动）。注意分界：**升级前已存在的** write 授权带 annotate；**升级后新建的** `write` 别名授权不带——新脚本要属性写就显式写 `annotate`，别依赖别名。

readonly_admin 对全部写位（含 annotate）结构性恒拒——把它加进带 annotate 的组同样静默无效（角色短路，见 [RBAC 指南](rbac-roles.md)）。

## 组管理 API（admin only）

错误体为用户管理族**纯文本**（非 E-01 JSON 信封——与 users/token 族一致）。

| 操作 | 请求 | 行为 |
|---|---|---|
| 列组 | `GET /api/security/groups` | `[{name, uri, description}]` |
| 查单组 | `GET /api/security/groups/{name}` | `{name, uri, description}`；不存在 404 |
| 建组/更新 | `PUT /api/security/groups/{name}` body `{name, description}` | **创建 201 无 body**；已存在 → **200 更新**；body name ≠ 路径 → 400 |
| 改描述 | `POST /api/security/groups/{name}` body `{"description":"..."}` | 200；不存在 404 |
| 删除 | `DELETE /api/security/groups/{name}` | 200 纯文本 `The group: '<name>' has been removed successfully.`；成功删除联动解除全部成员；**被 permission target 引用 → 409**（见下） |

组名规则：非空、≤64、`[a-z][a-z0-9._-]*`（小写字母开头）；保留字 `anonymous` / `_system_` → 400。

```bash
# 删被引用的组 → 409 且列出引用方（防悬挂授权）
curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/api/security/groups/devs -w '\n%{http_code}\n'
# Cannot delete group 'devs': it is referenced by permission target(s): devs-rw, jane-rd. Remove the group from those targets first.
# 409

# 解除引用后可删；jane 的 groups 联动清空为 []
curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/api/v1/permissions/devs-rw -o /dev/null -w '%{http_code}\n'  # 204
```

## 用户与组成员维护

| 操作 | 请求 | 行为 |
|---|---|---|
| 建用户/替换 | `PUT /api/security/users/{name}` | create-or-replace，两态皆 **201 无 body**；`email` **必填**（缺省 400 `Please provide a valid user email.`）；`groups` 引用未知组 → 400 `Unable to find group by name '<g>'.` |
| 部分更新 | `POST /api/security/users/{name}` | 指针字段区分缺省/显式空：`"groups":[]` 清成员、缺 `groups` 不动；M9 起 `enabled` 同为指针字段（显式 `false` 禁用登录） |
| 用户详情 | `GET /api/security/users/{name}` | `{name, email, admin, adminRole, enabled, groups[], realm, ...}`——**无口令字段**（M9 起恒回显 `enabled`） |
| 用户列表 | `GET /api/security/users` | **M9 加宽**：`[{name, uri, realm, source, email, adminRole, enabled, groups}]`——单请求即含列表所需全部字段 |
| 删除用户 | `DELETE /api/security/users/{name}` | **M9 起提供**：admin only；三护栏 400（内置 admin / 最后一个 admin / 自删）、级联撤权吊销 token、成功 200 纯文本、**重复删除确定性 404**（有意非幂等）——语义全解见[治理指南 · 删除用户](governance.md#删除用户m9-起) |

M7 起用户行携带**角色**字段（仅 admin 可写）：

- body 可选字段 **`adminRole`**，闭集三值 `user`（缺省）/ `readonly_admin` / `admin`（snake 形；kebab 拼写 400）；与 `admin` 布尔等价映射（`admin=true ⇔ adminRole=admin`），同时出现且矛盾 → 400。
- 角色变更对该用户**存量 Token 即时生效**，并记审计 `user.role.change`。
- readonly_admin 的行为边界（管理读面全通 / 变更面全拒 / 数据面全域只读短路）与 curl 序列见 [RBAC 角色与仓库级管理员](rbac-roles.md)。

## 有效权限视图：`?permissions`

对 local 仓的制品路径请求 `?permissions`，返回逐主体的有效权限（admin only；**key = 主体名、value = 权限字母集合**，字母为 r/w/d/**m**/**a**——manage 渲染 `m`、annotate 渲染 `a`、deploy-cache 仍渲染 `w`）：

```bash
curl -su admin:$ADMIN_PW "$BASE/binflow/api/storage/generic-local/acme/app.bin?permissions"
# {"principals":{"users":{"annie":["r","w"],"archie":["r","a"]},"groups":{}}}   （实测）
```

virtual / remote 仓请求 → 400（`only supported on local repositories`）；local 仓但路径不存在 → 404。无任何权限的主体不出现在结果里。

## 控制台对应操作

安全域页面与上述 API 一一对应（位于管理模式「用户与权限」分组：`/admin/security/groups`、`/admin/security/users`、`/admin/security/permissions[/:name]`；M7 及以前的 `/security/*` 旧路径已随 M9 移除重定向窗口而失效，对照表见[控制台指南](../console.md#旧路径--新路径m9-起不再重定向)）：

- **用户与组的创建/编辑均为路由整页表单**（列表内联卡已退役）：用户创建 `/admin/security/users/new`（**Retype Password 双录**，两次不一致挡提交）、组创建 `/admin/security/groups/new`、组编辑 `/admin/security/groups/:name/edit`；页脚 `Cancel` | `Reset` | `Save`——Reset/Save 初始禁置（dirty 门 + 必填门）。用户表单的 Can Update Profile / Disable UI Access / Disable Internal Password 三开关为恒禁用预留位（服务端尚未承接，提交体零携带）；
- 权限编辑器为单页分区形态（名称 / 资源 / 用户 / 组）+ **两步资源对话框**（`编辑仓库…` → ① 选仓库〔双列穿梭〕→ ② 可选 include/exclude patterns）；主体矩阵为 **users + groups 双栏**，组行带图标前缀；动作矩阵随动词域五值化呈现更新中（REST 面已是五值闭集，见上节）；
- **模式测试器**：输入任意路径即时显示每条 include/exclude 的命中与最终判定（exclude 优先）——判定向量从服务端 ACL 的 table-driven 用例导出 fixtures 生成（CI 漂移即红），与保存后的实际判定同源；
- 保存前弹**变更摘要 diff**（逐条「+ 授予 / − 移除」），确认后才提交；
- 移出组后在另一浏览器上下文立刻复验 403（W33b 验收手法，可用于自证即时生效）。

## 有意不兼容（M4 权限域）

| 项 | BinFlow 行为 | 说明 |
|---|---|---|
| **组无 admin 位** | 组**不能**授予 admin 或角色（`adminRole` 仅在用户行，M7 起同样不可经组授予）；admin 组成员的非 admin 用户对管理面（用户/组/权限/审计/GC/token）仍是 **403** | BinFlow 有意不兼容（admin/角色是用户属性不是可授予权限；防「建个组把自己提权」） |
| `PUT groups` 已存在 → 200 | Artifactory 习惯为 201 | M4 定案（创建 201 / 更新 200 分态）；自动化脚本请以状态码区分而非假设恒 201 |
| `PUT users` 已存在 → 201（create-or-replace） | — | replace 覆盖 email/password/admin/groups；未提供的字段不保留旧值 |
| `/api/v2/security/permissions/**` | **404** | BinFlow 权限面是 `/api/v1/permissions`；Artifactory 的 v2 权限 API 不承诺 |

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 400 `Unable to find group by name 'nope'.` | 用户/组写入引用了不存在的组 | 先建组再入组（三步流顺序） |
| 400 `unknown permission action "..." (supported: read, deploy-cache, annotate, delete, manage; ...)` | 动作词拼错（如 `setX`、kebab 形） | 用五正词或 `write` 别名 |
| 403 `permission denied: writing properties requires annotate access on the item` | 有 deploy-cache 无 annotate 就想写属性 | 给 principal 追加 `annotate`（不需要 `delete`） |
| 409 `Cannot delete group 'devs': it is referenced by permission target(s): ...` | 组仍被 target 引用 | 按提示先删/改引用方 target |
| 400 `Unable to create group: name must match [a-z][a-z0-9._-]* ...` | 组名非法（大写/数字开头等） | 改名 |
| 400 `'anonymous' is a reserved name.` | 保留字 | 换名 |
| 400 `Please provide a valid user email.` | 建用户缺 email | email 必填 |
| 403（管理面） | 非 admin 用户访问 admin 域 | 组授予不能提权到 admin（见上表） |
| 404 `Unable to find item: ...` | `?permissions` 的路径不存在 | 核对路径；virtual/remote 仓不支持 |

## 下一步

- 角色（user/readonly_admin/admin）与 `manage` 派生的仓库级管理员：[RBAC 角色与仓库级管理员](rbac-roles.md)
- 仓库级治理字段与配额：[治理指南](governance.md)
- 控制台安全页走查：[Web 控制台使用指南](../console.md)；Artifactory 操作路径对照：[对照表](../artifactory-path-map.md)
- 权限语义总览与 Artifactory 对照：[FAQ](../faq.md)
