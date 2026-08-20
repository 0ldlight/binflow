# T-97 架构评审报告 — groups 域与权限继承（双 reviewer 票之一）

- ticket: T-97 [P0] groups 域+权限继承（FR-27，SE-01~08）
- reviewer: architect（并行票另一位看正确性+安全）
- 日期: 2026-08-21
- 评审对象: commit 86f879b（与工作树一致）；`internal/httpapi/system_gc.go`、`t94_gc_test.go`、`harness_test.go` 的 GC 改动属并行票 T-94，不在本评审范围
- 输入: reports/agents/T-97.md（5 条遗留）、reports/agents/T-88.md T-97 节、DECISIONS.md ADR-0014、architecture.md §6/§7、docs/reverse/auth-model.md §1/§4、docs/reverse/rest-api.md §3

## 结论

**REQUEST_CHANGES** — 1 个 blocker（`?permissions` 视图映射方向与真实 API 相反，翻转点已预留、改动窄）；其余分层、004 表消费、users 语义演进、审计词表治理、嵌套组边界全部通过，遗留②裁定维持现实现。blocker 修复后无需二次架构评审（正确性/安全 reviewer 对该函数的既有意见不受影响）。

---

## Blocker（必须修）

### B1 `?permissions` 视图映射方向反了：应为「主体名 → 权限位列表」，现实现是「权限位 → 主体名列表」

- 位置: `/Users/lzw/dev-center/internal/httpapi/storage.go:134-153`（permissionsView 结构）、`:196-198`（装配）、`:202-233`（bitsToNames）；同批断言 `/Users/lzw/dev-center/internal/httpapi/t97_groups_test.go:552-563`（`users["r"]==["jane"]` 等）
- 证据（两路独立）：
  1. **官方生态类型定义**：Atlassian go-artifactory 客户端（按 Artifactory 官方 API 维护）的 `ItemPermissions`（即 `?permissions` 响应，媒体类型 `application/vnd.org.jfrog.artifactory.security.ItemPermissions+json`）**复用 permission target 的同一个 `Principals` 结构**：`Users/Groups *map[string][]string`，注释明示 `m=admin; d=delete; w=deploy; n=annotate; r=read`——**key = 主体名，value = 权限位字母列表**。真实样例形态 `{"users":{"alice":["r","w","d"]}}`。
  2. **BinFlow 自身内部一致性**：`internal/httpapi/permissions.go` 的 `permissionPrincipalsBody`（/api/v1/permissions 回显）就是 name → action 列表。同一实例两个 principals 形状若方向相反，客户端无法复用同一解析器——Artifactory 生态两处同构，我们也应同构。
- 判定: docs/reverse/rest-api.md §3 该行括注「key 为 r/w/d/a 权限位，value 为主体名集合」是逆向笔误（置信度高标错了方向）。ADR-0001 规定「有公开规范的协议以官方文档为准」，官方生态证据压倒逆向括注。
- 修复（窄，工作日志已预判翻转点）：
  - `bitsToNames` 改为 `namesToBits`：产出 `map[string][]string`（name → 按固定序 `["r","w","d"]` 子集）；
  - `permissionsView.Principals.Users/Groups` 语义注释同步；
  - `auth.Service.ItemPrincipals` 与 `permissionViewer` 接口（`map[string]auth.PrincipalBits`）**方向中立，零改动**——seam 设计在此兑现价值；
  - t97 测试 W21 断言翻转（test 现已 pin 死方向，遗留③「不敏感」仅指 curl 序列）；
  - **规格勘误**：docs/reverse/rest-api.md §3 该行括注改为「key 为主体名，value 为权限位列表（r/w/d，与 permission target 的 principals 同构）」——reverse-engineer 名下文件，请 conductor 转派；
  - 派生勘误：architecture §7 storage 行（见下文草案）。
- 为什么是 blocker 而非登记：wire 契约方向是兼容层（ADR-0003）的核心；T-101 FE 与 T-103 QA 尚未消费锁定，现在翻是 1 函数代价，qa 后翻是断供代价。

---

## 遗留裁定

### 遗留①（= B1）：映射方向

**裁定：翻转。** 现实现按逆向规格字面（位→名单）是错的，真实 API 为名单→位。证据与修复见 B1。本票把翻转点收敛在单函数+测试，工程姿势正确。

### 遗留②：匿名实例上 `?permissions` 对匿名调用开放

**裁定：维持现实现（读门过即出视图），不收紧。**

- 证据：JFrog 官方现行文档 Get Storage Item Information 页 Security 句：「Requires a privileged user (**can be anonymous**) for basic folder/file/**properties/permissions** info」——permissions 与 folder/file/properties 同档，明确允许匿名（实例开启匿名访问时）。老版客户端注释「requires admin」是 pre-7.x 语义，官方现行文档为准。
- 架构理由：
  1. **单门同源**：与 item GET 完全同一读门（reposvc Get/List，含 403/404 文案与匿名策略），无独立特权分叉面——少一个要测试、要文档、要解释的行为。
  2. **暴露面有界**：视图只列对该 path 持有授权的主体名；对开匿名读的仓库，能 pull 的人本就能推断 ACL 存在。`anonymous_access=false` 实例该门自然 401，无额外暴露。
  3. 不超出兼容目标（ADR-0003）：Artifactory 本身就这么做。
- 备忘：若安全 reviewer 基于威胁模型另有裁定，收紧点是一行（router.go 该分支 `routeAuth{required: true}`），架构上无异议——但默认姿态=不收紧。

### 遗留③④⑤：登记性，无架构异议

- ③ 随 B1 一并解决（测试断言同步翻转）。
- ④ user 删除端点缺位：确认 BOARD P2 登记（FR-28 UI 需要 `DELETE /api/security/users/{name}` + 审计 + `user.delete` 动作词）；组删除有 409 保护在前，无悬挂风险，判断正确。
- ⑤ `group.member` 值未变也记录：**接受现实现**。审计是 append-only 证据面，「多记可过滤、漏记不可补」是对的方向；为降噪加前置读是一次读放大换静音，不划算。登记为已接受取舍，T-105 若有审计量投诉再议（`action=group.member` 可过滤）。

---

## Non-blocking 清单

| # | 位置 | 问题 | 建议 |
|---|---|---|---|
| NB1 | migrations/sqlite/004_console_governance.sql:34-38 + architecture §6 user_groups DDL | `user_groups` 只有 `PRIMARY KEY (group_id, username)`，无 username 索引；`GroupsOfUser`（substores_console.go:169，`WHERE ug.username = ?`）在**每次认证**执行（fillGroups 逐请求），无索引=每次全表扫。表规模=用户×组，现实施小，但这是认证热路径，与 §6「热查找列必配索引」的自身惯例（idx_tokens_user/idx_web_sessions_user）不一致 | §6 DDL 增 `CREATE INDEX idx_user_groups_username ON user_groups(username);` 勘误；实现侧走新迁移文件（006，迁移只追加），可并入 T-90 勘误或独立小票；T-105 NFR-P17 性能门兜底 |
| NB2 | internal/httpapi/security.go:606-616（replace 路径） | PUT replace = UpdatePassword + UpdateProfile + SetUserGroups 三语句，非同一事务；中途失败留混合态（口令已换、组未设）且 500。管理面低频、SQLite 单写者下窗口极小 | 登记技术债；FR-28 的 user delete 票落 `DELETE /{name}`（级联 tokens/sessions/membership）时一并考虑 `Store` 级事务缝（UserStore.Replace/ Delete 单事务），不在本票扩 scope |
| NB3 | internal/auth/groups.go:26-33 | 嵌套组为 flat 单层（schema 强制：user_groups.username 外键 users 表，组入组不可表达；rowCoversPrincipal 单跳 slices.Contains），与 Artifactory 内部组平铺一致——但 **godoc 未显式声明单层边界** | 在 `GroupSource` 或 `fillGroups` 注释加一句「single level, no nested groups; a future nesting closure would live behind this seam（GroupsOfUser 返回闭包即可，rowCoversPrincipal 不动）」。演进缝本身已留对 |
| NB4 | internal/httpapi/groups.go:120-135 | groups PUT body name 不符 → 400，而 users 同型错误 409（auth-model §1.3-②）。差异是 AC 明文（K1 暂行），双处均有注明 | 维持；逆向扩编回写时对照真实 groups 端点（大概率 409 族文案），翻转集中在 validateGroupName/两 handler 单点 |
| NB5 | architecture.md §7.1:796/807 | 文档漂移（非本票引入）：users 行写 `/binflow/api/v1/users[/{name}]`，M1 起实现事实是 `/binflow/api/security/users`（E-16~E-19 兼容面）；且缺 POST /{name} 动词、DELETE 未实现未注明 | 按下方勘误句草案回写 |
| NB6 | internal/httpapi/storage.go:186-192 | 根路径（relPath==""）视图以 `path=""` 进 targetCovers：与 Can 同谓词故自洽，但非空 includes（如 `devs/**`）对空串不命中→根视图可能空。属语义边角，Artifactory 对根的口径未规格化 | 可不处理；建议 handler 注释一句「root view computes with path ""; 与 Can 对根的授权口径一致」，防后人误判为 bug |

---

## 重点审查区逐项结论

### 1. 分层与注入 — 通过

- `groupSource` 消费侧窄接口 + `GroupSource` 导出别名：与 userSource/webSessionSource 先例同构，「接口定义在消费方」惯例保持；auth 包对 metadata 的 import 仍收敛在 deps.go 组合点，边界未破。
- `WithGroups` nil=惰性 + `NewFromStore` 无条件接线：与 T-91 `WithSessions` 逐字同款（004 表恒在的论证也同款），旧测试零影响的姿态成立（`Service` 无 mutex，浅拷贝 clone 安全）。
- `permView` 以 `Deps.Authz.(permissionViewer)` 断言发现 + 503 兜底：与 `sessionRegistry` 断言先例一致，cmd/main 装配零改动（main.go 本批改动全部属 T-94）。`PrincipalBits` 方向中立，是 B1 翻转代价为一函数的直接原因——seam 选对了。
- `fillGroups` 组侧 fail-closed（富集失败带空组继续 + Error 日志）：爆炸半径分析成立——直接授权存活、组授权绝不因 join 故障放开；这是「降权不升级」的正确方向，与 Can 的 fail-closed（表坏即拒）不矛盾（那是判定面，这是富集面）。认可。

### 2. 004 表消费 — 通过

- groups/user_groups 用法与 DDL 逐列对齐（Create/Update 全列、Delete 靠 FK 级联、W20 实测 jane groups→[]）；`GroupReferences` JOIN 谓词 `principal_type='group' AND principal=?` 与 UNIQUE 约束一致，user 同名行正确排除，ORDER BY t.name 使 409 文案确定序。
- **GroupReferences 放 PermissionStore：归属正确。** 候选对比：放 GroupStore 需让组存储伸手查 permission 域两张表（跨域）；放 Store 根接口为一个端点扩总面。它本质是「权限域的被引用查询」，PermissionStore 是唯一不跨边界的家。
- **UserStore.UpdateProfile 接口扩张：必要。** 候选对比：复用 UpdateEmail+新增 UpdateAdminFlag=两条语句两次 updated_at 写；UpdateProfile 单语句覆盖 replace/partial 两形态的 email+admin 组合，口令仍走专用缝（argon2id 派生值与档案列语义不同，分开是对的）。接口注释把用途锚到 T-97 语义，无过度通用化。唯一残留=NB2 的事务边界。

### 3. users 语义演进 — 通过（附 NB5 勘误）

- PUT 两态皆 201 无 body：auth-model §1.3-⑪ 高置信度原文；Atlassian 客户端 `CreateOrReplaceUser`（PUT）/`UpdateUser`（POST）的动词分工双重佐证。M1 的 409 退役按 R5 流程执行、security_test 同批注明来源——流程正确。
- POST /{name} 部分更新用指针字段区分缺省/显式空（`groups:[]` 清成员 vs 缺 groups 不动）：正确实现了 §1.4「未提供的字段保持原值」，且是唯一能用 JSON 无哨兵值表达该区分的形态。集合 POST 维持 create-only 409（BinFlow 自有扩展，M1 语义）。
- enabled 不在 wire 体、replace 保持原值：合理（禁用面走未来专用端点），注释已写明。
- 与 K1/K3、ADR-0014 一致性：组无 admin 位（SE-07）在 groups.go 头注释、authorizer、TestGroupHasNoAdminBit 三处一致锚定；W19b 依赖链成立。

### 4. `?permissions` 视图 — 形态归属通过，方向= B1

- **归属正确**：作为 `GET /api/storage/{repo}/{path}` 查询参数（非独立权限端点）与真实 API 及 E-09 家族（?list/?stats/?properties 同挂法）一致；独立端点会偏离兼容目标。门序（未知仓 404 envelope → 非 local 400 **先判型防 remote 上游拉取** → 内容面读门；根走 serveRootFolder 同款 List 门）架构上干净且全部有 spec 锚。
- 视图与判定共用 `targetCovers`（从 Can 抽出的谓词）：**「视图不可能与判定分叉」是本票最好的一个设计决定**，TestItemPrincipals 的 "view agrees with Can on every action" 把它钉死。B1 只翻序列化方向，不动这个谓词。
- 遗留②裁定见上：维持匿名同门。

### 5. 审计词表治理 — 通过

- `group.member` 常量进 `internal/audit/api.go` 的 Actions()：**正确形态**。词表的单一事实源就是该常量面（GE-02 全集可查），T-93 是消费方不是定义方；若本票在别处定义会碎片化。常量注释写明派单来源与 Detail 形状，可追溯。
- 治理动作：conductor 在 GE-02 词表口径（PRD/T-93 文档面）回写 +1（group.member），T-107 用户文档的动作值清单同步；`permission.create/update/delete` 顺手补齐（NFR-S25 授权变更留审计）同批生效，无新增治理问题。
- 遗留⑤裁定见上：接受「携带 groups 字段即记录」。

### 6. 嵌套组边界 — 通过（附 NB3）

- flat 单层由 schema 强制（user_groups.username → users 外键，组入组不可表达），实现单跳（fillGroups 一次查询、rowCoversPrincipal 一次 contains），无隐藏递归。
- 未来嵌套的演进缝位置正确：闭包计算收敛在 GroupsOfUser（store 侧）或 fillGroups（服务侧），Principal.Groups 消费面与 Can/ItemPrincipals 均不需改。缺的只是 NB3 的一句显式 godoc 声明。

---

## architecture §7 勘误句草案

**① 替换 §7.1 line 807（users 行，修正路径漂移 + 补 POST + 注明 DELETE 债务）：**

```
GET/POST /binflow/api/security/users、GET/PUT/POST /binflow/api/security/users/{name}
                                                用户面（admin；兼容层路径非 /api/v1——E-16~E-19 既有事实，
                                                M1 实现即落 security 段，原行 /api/v1/users 系笔误）；PUT /{name}
                                                = create-or-replace，两态皆 201 无 body（auth-model §1.3-⑪，
                                                T-97 R5 翻转）；POST /{name} = 部分更新（指针字段区分缺省/显式空，
                                                groups[] 维护成员，200 无 body）；集合 POST = create-only 409；
                                                DELETE /{name} 未做（FR-28 UI 需求补票 + user.delete 审计，P2 登记）
```

（同批删除 line 796 的 `GET /binflow/api/v1/users` 行——M1 路由表的同源漂移，并入上行。）

**② §7.1 在 /api/storage 相关行增补（?permissions 视图，含 B1 方向勘误）：**

```
GET /binflow/api/storage/{repo}/{path}?permissions
                                                有效权限视图（M4 T-97，SE-08）；读门与 item GET 同源（匿名策略
                                                一致，官方文档 "can be anonymous" 档）；非 local → 400（先判型，
                                                remote 查询不触发上游拉取）；形状
                                                {"uri","principals":{"users":{"<name>":["r","w","d"]},"groups":{...}}}
                                                ——key=主体名、value=权限位列表，与 /api/v1/permissions 的
                                                principals 同构（T-97 架构 review 勘误：逆向规格 §3 括注方向反了，
                                                官方 ItemPermissions 复用 permission target 的 Principals 结构）
```

**③ §6 DDL 勘误（NB1，可与 T-90 勘误合并回写）：**

```
CREATE INDEX idx_user_groups_username ON user_groups(username);   -- 认证热路径（fillGroups 逐请求）
                                                                  -- 补索引；实现侧走新迁移文件（只追加）
```

---

## 证据附录（B1 / 遗留②）

- Atlassian go-artifactory 客户端 `artifactory/v1/security.go`：`type Principals struct { Users *map[string][]string; Groups *map[string][]string }` + 注释 `m=admin; d=delete; w=deploy; n=annotate; r=read`；`ItemPermissions{Uri, Principals}` 复用同一 `Principals`；`GetEffectiveItemPermissions` → `GET /api/storage/{repo}/{item}?permissions`。
- JFrog 官方文档 Get Storage Item Information（docs.jfrog.com/artifactory/reference/getstorageitem）：Security 句「Requires a privileged user (can be anonymous) for basic folder/file/properties/permissions info」。
- docs/reverse/auth-model.md §1.2/§1.3/§1.4（users CRUD 码值链）、docs/reverse/rest-api.md §3（?permissions 行）。
- 真机 Artifactory 样例形态（`"users":{"<name>":["r","w","d",...]}`）与上述类型定义互证；若逆向扩编票持反证（反编译响应序列化点），可推翻本裁定——翻转点两侧均收敛在 `namesToBits`/`bitsToNames` 单函数，代价对称。
