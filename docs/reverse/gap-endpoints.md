# 缺口端点行为规格（M9 服务端缺口群：用户回显 / 用户删除 / 组成员 / 权限列表过滤 / 仓库用量）

> 逆向基线：artifactory-pro 7.161.16（`reverse-src/artifactory/README.md`）。
> 置信度：`高` = 反编译代码 + 公开文档双证；`中` = 仅反编译代码可见；`低` = 推断待动态验证。
> 与既有规格的关系：`auth-model.md` §1（用户 CRUD 骨架）、`rbac-model.md` §1.2（组 CRUD）、`rest-api.md` §2（仓库 CRUD）已覆盖端点表骨架；本文补**字段级回显、删除级联、成员关系暴露面、过滤面有无、用量暴露面**五项，不重复既有内容。
> 官方参考：JFrog "Deprecated JFrog APIs" 页（Get Users / Get User Details / Delete User / Get Groups / Get Group Details / Get Permission Targets 条目）、JFrog Access API v2 文档（Get Group Details / Add or Remove a Group Member）、Get Storage Summary Info / Refresh Storage Summary Info 条目。

## 1. GET /api/security/users 与 GET /api/security/users/{name} 回显字段集

### 1.1 集合列表（GET /api/security/users）

| 项 | 行为 | 置信度 |
|---|---|---|
| 权限 | admin only；调用者 MFA 已验证时仅接受 Bearer token（否则 403，同 auth-model §0） | 高 |
| 响应形态 | 200 JSON **数组**（服务端用无序集合组装，元素顺序无保证） | 高 |
| 元素字段 | **仅 3 个**：`name`、`uri`（指向单用户详情的绝对链接）、`realm`。**没有** `enabled`、`groups`、`status`、`admin` 或任何其它字段 | 高 |
| 过滤参数 | **无任何 query 参数**（无过滤、无分页） | 高 |
| 隐藏项 | AOL 管理账号（托管云内部账号）不出现在列表 | 中（仅代码可见，自建实例观测不到） |

### 1.2 单用户详情（GET /api/security/users/{name}）字段级回显

wire 模型即用户配置类本身（同一类同时用于 PUT/POST 请求体与 GET 响应体）。GET 时由固定 populator 填充，行为如下：

| 字段 | GET 回显语义 | 置信度 |
|---|---|---|
| `name` | 总是有 | 高 |
| `email` | 总是有 | 高 |
| `admin` | **合并语义**：直接 admin **或** 所属 admin 组成员 → 均回显 `true`（组成员身份提升的 effective admin 不区分展示） | 高 |
| `groups` | 组名集合；**仅当非空时才设置**（无组的用户无此字段或空集，取决于序列化策略） | 高（字段）；中（空集缺省形态） |
| `realm` | 总是有（`internal` / `ldap` / `access`…） | 高 |
| `lastLoggedIn` | ISO8601 字符串；**仅当 lastLoginTimeMillis > 0** 才出现（从未登录过的用户无此字段） | 高 |
| `profileUpdatable` | 总是有 | 高 |
| `internalPasswordDisabled` | 总是有（禁用内部口令 = 只允许 token/外部 realm 登录） | 高 |
| `disableUIAccess` | 来自用户属性 `blockUiView`；**仅当该属性存在时才设置**（即显式禁用时为 true；未禁用时字段缺省） | 高 |
| `status` | 用户状态枚举字符串，闭集 `invited` / `enabled` / `disabled` / `locked` | 高 |
| `mfaStatus` | MFA 状态（未启用 MFA 时为缺省值） | 中 |
| `policyViewer` / `policyManager` / `watchManager` / `reportsManager` | Xray/观测类角色布尔，总有 | 中（license 未激活时无意义但字段仍在） |
| `password` / `groupAdmin` / `shouldInvite` / `source` / `offlineMode` / `lastLoggedInMillis` / `centralOAuthProviderUserIds` | **请求体可写**（部分），但 GET 响应 populator **不填充**——保留默认值/空。口令任何情况下不回显 | 高（不回显）；中（具体缺省序列化形态） |
| **`enabled`** | **不存在该字段**。启用/禁用状态完全由 `status`（`enabled`/`disabled`）表达，口令维度另有 `internalPasswordDisabled` | 高 |

> **对 BinFlow 的含义（事实陈述，非决策）**：Artifactory REST 面没有 `enabled` 布尔回显位；T-208 的 `Enabled *bool` 写侧扩展在 Artifactory 无 1:1 对应读侧字段。读侧若要对齐 Artifactory 形态，载体是 `status` 字符串；若回显自有 `enabled` 布尔则属 BinFlow 自有扩展（需 PM/ADR 定形）。

### 1.3 UI 面的用户回显（对照）

| 项 | 行为 | 置信度 |
|---|---|---|
| UI 列表/详情额外字段 | UI 模型继承上述 REST 字段，另附：`groupAdmin`（**独立布尔**，与直接 `admin` 分列——UI 不做合并语义）、`credentialsExpired`、`lastLoggedInMillis`（原始毫秒）、`existsInDB`、`numberOfGroups`、`proWithoutLicense`、`canDeploy`/`canManage` 等派生位、`externalRealmStatus`/`externalRealmLink`（外部 realm 用户标注「Check external status」链接） | 中 |
| UI 的 admin 语义 | UI 单用户回显 `admin` = 仅直接 admin；组 admin 走 `groupAdmin` 字段——**与 REST 面的合并语义不同**（REST 把两者并成 `admin:true`） | 高（两个 populator 代码直读） |

## 2. DELETE /api/security/users/{name} 行为规格

### 2.1 端点表

| 方法 | 路径 | 权限 | 成功 | 错误 | 置信度 |
|---|---|---|---|---|---|
| DELETE | `/api/security/users/{name}` | admin only（+MFA Bearer 断言） | 200 `text/plain`：`The user: '<name>' has been removed successfully.` | 404（无 body，用户不存在）；403（透传 Access 拒绝消息，见 2.3） | 高 |
| POST | `/ui/api/v1/security/users/userDelete`（UI 批量删除，body `{userNames:[...]}`） | admin only | 200：单个 `Successfully removed user '<name>'`；多个 `Successfully removed N users` | 空用户名 → 404；自删 → 403 `Action cancelled. You are logged-in as the user you have selected for removal`；Access 拒绝 → 403 `Action cancelled. <Access 消息>` | 高（代码；UI 路径前缀标中） |

### 2.2 删除级联（按代码执行顺序）

1. 删除前先查用户存在性：不存在 → 404（无 body），**不产生任何副作用**。
2. **剥离 ACE**：从所有 permission target 的 `principals.users` 中移除该用户的 ACE（该用户在所有权限目标上的授权随删随失）。
3. 转发 Access 服务删除用户本体（组成员关系、token 等 Access 侧数据由 Access 级联，反编译范围外）。
4. 触发用户删除拦截器（订阅方自行清理，如 API key 相关钩子）。
5. 认证缓存失效（该用户的缓存授权立即作废）。

置信度：高（Artifactory 侧链路代码直读）；Access 侧级联内容：中（外部服务，仅由调用形态推断）。

### 2.3 错误与守卫矩阵

| 条件 | REST 面（/api/security/users/{name}） | UI 面（userDelete） | 置信度 |
|---|---|---|---|
| 用户不存在 | 404 无 body（删除前显式探测） | 未显式探测（依赖 Access 404 吞掉，见下） | 高 |
| Access 返回 403 | 透传 403 + Access 错误体首条消息（`text/plain`） | 403 `Action cancelled. <消息>` | 高 |
| Access 返回 404 | **吞掉，视为成功**（幂等语义：并发/重复删除返回成功） | 同 | 高 |
| Access 其它 HTTP 错误 | 异常上抛（500 路径） | 同 | 中 |
| 删除自己 | **REST 面代码内无守卫**（见下「缺位」） | 403（显式守卫，文案见上） | 高（UI）；REST 面行为低（见 §6） |
| 删除最后一个 admin | **Artifactory 侧代码不可见**（对照：删除最后一个 admin **组**有显式守卫，见 rbac-model §1.2.2） | 同左 | 低（见 §6） |
| 删除内置 `admin` / `anonymous` | Artifactory 侧无显式守卫；预期 Access 侧拒绝 | 同左 | 低 |

## 3. 组成员关系端点（组成员怎么暴露）

### 3.1 暴露面总表

| 面 | 端点/载体 | 行为 | 置信度 |
|---|---|---|---|
| REST 组→用户 | `GET /api/security/groups/{name}?includeUsers=true` | `includeUsers` 默认 false；true 时响应附 `userNames[]`（组成员用户名数组）。**没有**独立的 `GET .../groups/{name}/members` 子资源 | 高 |
| REST 用户→组 | `GET /api/security/users/{name}` | 响应内嵌 `groups[]`（组名集合），无独立成员关系端点 | 高 |
| REST 集合过滤 | `GET /api/security/users` | **不支持按组过滤**（该端点无任何 query 参数，见 §1.1） | 高 |
| Access API v2（官方，服务在反编译范围外） | `GET /access/api/v2/groups/{name}` | 响应含 `members` 字段（组成员列表）——官方主推的组详情读法 | 高（官方文档）；字段细节中 |
| Access API v2 增量成员变更 | `PATCH /access/api/v2/groups/{name}/members` | body `{"add":[...],"remove":[...]}` 增量加/减成员；官方明确「组成员变更不再走组更新端点」 | 高（官方文档） |

### 3.2 组成员写语义（Artifactory 旧 REST 面，注意不对称）

| 端点 | body 提供组名列表 `userNames` 时 | 置信度 |
|---|---|---|
| `PUT /api/security/groups/{name}`（组已存在 → 更新路径） | **全量替换**组成员为目标列表 | 高 |
| `POST /api/security/groups/{name}`（部分更新） | **增量添加**（把列表中的用户加入组，不移除现有成员） | 高 |
| 用户创建/更新（PUT/POST users） | 用户侧 `groups` 全量替换该用户的组成员身份 | 高 |

辅助校验：组成员用户名大写校验开关 `validate.lowercase.username.on.group.association` **默认 false（关）**——开启后列表中出现非全小写用户名 → 400 `Only lowercase usernames are allowed`。置信度：高（代码 + 常量默认值）。

### 3.3 UI 面的「组成员汇总」（BinFlow 扇出缺口的对照答案）

| 端点 | 行为 | 置信度 |
|---|---|---|
| `GET /ui/api/v1/security/users`（UI 用户列表） | **一次请求返回全部用户**，每个用户对象内嵌 `groups`（组名集合）+ 组数派生位——UI 的「哪些用户属于哪个组」由该列表扇出承担，**不逐用户 GET 详情** | 高 |
| `GET /ui/api/v1/permissiontargets/allUsersGroups` | 权限编辑页的一次性主体清单：`{"allUsers":[{name,admin}], "allGroups":[{name,adminPrivileges}]}`；要求 MANAGE 权限（无 → 匿名 401 / 已认证 403，`MANAGE permission is required`） | 高 |
| UI 组资源 | 组侧**只有组名清单服务**，无组成员列表端点（组成员在 UI 上经组详情 `?includeUsers` 语义或 Access API 取） | 中 |

> Artifactory 侧没有「一次拿 group→members 映射全表」的扇出端点；组成员汇总的现实做法 = users 全列表内嵌 groups（§3.3 第一行）。

## 4. permission 列表的过滤/分页参数

| 端点 | query 参数 | 过滤/分页 | 置信度 |
|---|---|---|---|
| `GET /api/security/permissions`（v1） | **无**（路由签名不声明任何 @QueryParam） | 无。返回全量 `{name, uri}` 集（内部 ACL 被滤除，仅用户可见的 permission target）；admin only | 高 |
| `GET /api/v2/security/permissions` | **无** | 无。合并 repo/build/release-bundle 三类 ACL 为全量 `{name, uri}` 集；**路由角色为 admin+user**（非 admin 的已认证用户也能拿到列表——与 v1 的 admin-only 不同）；动作命名用展示名 | 高（路由+实现代码）；角色差异的运行时效果中 |
| `GET /ui/api/v1/permissiontargets`（UI） | **无集合列表路由**（只有 `/{name}` 详情及子查询）；详情族端点亦无过滤参数 | 无 | 高 |
| Access Permissions API（官方现行主推） | 官方文档层面亦无过滤/分页参数 | 无 | 中（官方文档页未能直接拉取，经搜索摘要印证） |

**结论：Artifactory 的 permission 列表在全部暴露面上都没有过滤/分页面。** BinFlow 的 `GET /v1/permissions` 若增加过滤参数（如按主体/仓库过滤），属**自有优化**而非 Artifactory 对齐项；同时 Artifactory 的事实是「v2 列表对非 admin 已认证用户开放（仅名称+链接，不含 principals 细节）」——m-holder 类主体可见性若要对齐，参照点是 v2 语义而非 v1 的 admin 闭集。置信度：高（无过滤面）；中（v2 角色开放语义）。

## 5. 仓库列表的用量/统计列

### 5.1 GET /api/repositories（列表）不带用量

| 项 | 行为 | 置信度 |
|---|---|---|
| 列表元素字段 | 闭集 6 个：`key`、`description`、`type`（local/remote/virtual/federated）、`url`、`packageType`、`configuration`（仅 remote 共享配置仓带）。**没有** size/usedSpace/filesCount/itemsCount 等任何用量或统计字段 | 高 |
| 既有过滤参数 | 仅 `type` / `packageType` / `project`（见 rest-api.md §2），与用量无关 | 高 |
| 非法过滤值 | `type` 非闭集值或 `packageType` 大小写不匹配（Generic 特例）→ **不报 400，返回空数组** | 高（代码直读，复核确认） |
| 数据来源与缓存头 | 列表完全由仓库**配置**组装，不触碰任何存储统计；响应带 `Cache-Control: no-store` 头 | 高（代码直读，复核确认） |

### 5.2 用量走独立 storage summary 端点

| 方法 | 路径 | 权限 | 行为 | 置信度 |
|---|---|---|---|---|
| GET | `/api/storageinfo` | admin only | 200：`binariesSummary` + `fileStoreSummary` + `repositoriesSummaryList[]`。**一次请求返回全部仓库的用量**——这是 Artifactory 的「仓库用量扇出」唯一 REST 载体。数据读自缓存 | 高 |
| POST | `/api/storageinfo/calculate` | admin only | 异步触发缓存重算（202/200 语义），不阻塞返回 | 高 |

`repositoriesSummaryList[]` 元素字段（`RepositorySummary`）：

| 字段 | 语义 | 置信度 |
|---|---|---|
| `repoKey` / `repoType` | 仓库键/类型 | 高 |
| `packageType` / `projectKey` / `projectName` | 包类型与项目归属 | 高 |
| `foldersCount` / `filesCount` / `itemsCount` | 目录数/文件数/条目总数 | 高 |
| `usedSpace` / `usedSpaceInBytes` | 人类可读用量字符串 + 原始字节 | 高 |
| `percentage` / `displayPercentage` | 占总用量百分比（数值 + 展示串） | 中 |

### 5.3 缓存生命周期（可观察行为）

1. `GET /api/storageinfo` 读的是**缓存快照**，不做在线聚合。
2. **冷缓存（启动后从未计算过）→ 503**，body：`Cache is being calculated for the first time, hence is not available at the moment`（`application/json` 类型的纯文本）。
3. 缓存由后台任务定期刷新 + `POST /api/storageinfo/calculate` 手动触发；重查询缓存（HQC）开启时同结果带约 24h 的缓存上限语义。
4. 官方文档另载有 `GET /api/repositories/{repoKey}/storage`（Get Storage Summary Info by Repo，单仓用量）——**本基线反编译代码内未定位到该路由**（见 §6 缺位）。

置信度：高（1–3，代码直读）；低（4，仅文档面存在）。

> **对 BinFlow 的含义（事实陈述）**：Artifactory 的 repos 列表页用量列不是靠列表字段，而是 UI 另调 `/api/storageinfo` 一次拿全表。BinFlow 现状「per-repo usage ~170 请求」的对齐目标形态 = 一个 storageinfo 式扇出端点（缓存快照 + 手动/定期刷新），而非给 repositories 列表加用量列。

## 6. 缺位与待验证清单

| # | 条目 | 状态 | 建议验证方式 |
|---|---|---|---|
| 1 | DELETE user 的**最后 admin 守卫**（删掉最后一个 admin 用户是否被拒、错误码与文案）——Artifactory 侧代码不可见，预期在 Access 服务侧 | 缺位（低） | 活体 Artifactory：造两个 admin，删至最后一个再删，记录响应 |
| 2 | DELETE user 的**自删行为**（REST 面）：REST 处理器无自删守卫（UI 面有），Access 服务侧是否兜底拒绝未知 | 低 | 活体：admin 用自己凭据 DELETE 自己 |
| 3 | DELETE 内置 `admin` / `anonymous` 的行为与错误码 | 低 | 活体直测 |
| 4 | `GET /api/repositories/{repoKey}/storage`（单仓用量）——官方文档记载，但 7.161 反编译代码未定位路由（可能已移除或由未逆向模块承载）。复核二次检索（枚举 `RepositoriesResource` 全部子路由、`SystemResource`→`StorageResource` 全部子路由、全局 `Path("storage")`/按 repoKey 的 summary 服务名搜索）仍未定位 | 低（缺位已加固：两轮独立检索均负） | 活体 curl 确认存在性；若存在补回本规格 |
| 5 | `groups`/`disableUIAccess`/`lastLoggedIn` 为空/缺省时 GET user 响应的精确 JSON 形态（字段缺失 vs null vs 空集合——取决于全局 Jackson NON_NULL 配置，未逆向序列化配置层） | 中 | 活体 GET 对照 |
| 6 | Access API v2 `GET /access/api/v2/groups/{name}` 的 `members` 字段精确形态（官方文档页本环境未能直接拉取，经搜索摘要印证） | 中 | 活体 / 官方文档复核 |
| 7 | `GET /api/v2/security/permissions` 对非 admin 已认证用户的实际可见内容（路由角色放行 admin+user，实现层无二次过滤可见——需确认非 admin 是否真能拿到全量列表） | 中 | 活体：普通用户 token 调用 |

## 7. 与公开规范的差异/补充（本文新增的「补充官方规范」条目）

- 官方 Deprecated APIs 页列了 Get Users / Get User Details 的字段示例，但**未写明**：`admin` 的合并语义（组 admin 也回显 true）、`lastLoggedIn`/`groups`/`disableUIAccess` 的条件出现规则、`status` 枚举闭集——本文以代码补齐。
- 官方 Delete User 条目只写「Requires an admin user」；**未写**删除级联（ACE 剥离先行）、Access 404 吞掉的幂等语义、Access 403 消息透传——本文补充。
- 官方 Get Group Details 条目提到 `?includeUsers`；**未写** PUT（全量替换）与 POST（增量添加）的组成员写语义不对称——本文补充。
- 官方 Get Storage Summary Info 条目**未写**冷缓存 503 行为与精确文案——本文补充。
