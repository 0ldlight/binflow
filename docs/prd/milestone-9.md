# PRD — M9 服务端缺口收口与运营硬化（SE 域端点群 / GC 竞态根治 / OIDC step-up 腿 / 债券池处置 / 运营 chores）

> **PRD 状态：v1.0 草案（待 conductor 审）**。主轴来源：M8 契约冻结（ADR-0029 决策 4「服务端契约冻结声明」——UI 对齐暴露的服务端缺口经熔断线排队）+ 用户授权 conductor 自主定方向（2026-08-24「剩下的你自己决定」）。conductor 种子 A~F 全量承载：A 服务端缺口端点群、B GC 并行竞态、C OIDC step-up 控制台腿、D replica 隔离（评估后建议延后 M10）、E 运营 chores、F M8 债券池 28 条逐条处置。**新端点全部走 PM FR + ADR 流程（ADR-0030 起）**，M8 的「零契约改动」门槛在 M9 反转为「变更面可枚举、可审计」。

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/milestone-9.md` |
| 里程碑 | M9 — 服务端缺口收口与运营硬化（对应 ROADMAP.md「M9」条目；M8 契约冻结排队缺口 + T-232/T-242 遗留 + ADR-0025 Q7 处置 + M8 债券池） |
| 状态 | **v1.0 草案，待 conductor 审**（FR-78~FR-83 六条需求，契约兼容矩阵 10 条〔兼容 4 / v1 自有 4 / 有意不兼容 2〕，N01~N28 验收命令骨架，开放问题 Q1~Q6 带暂行，F 池 28 条处置表〔收编 18 / 延后 3 / 关闭 7〕） |
| 上游依据 | PRODUCT.md（核心能力 4/6、Non-goals）、BOARD.md M8 完结节（closure 2026-08-24：22 票 done、m8-done tag、M9 候选 28 条债）、docs/prd/milestone-8.md（§9 DoD 体例 + §5.4 U20 勘误）、reports/agents/T-246-qa.md §八（28 条债全量）、T-237 §3（用户域契约漂移①~④）/ T-240 §4（repos 列表缺口）/ T-241 §3（m-holder 可达性）/ T-242 §7（OIDC 腿遗留）/ T-232（gc 竞态）/ T-244 §6（共享层余项）/ T-248 遗留（单架构镜像）/ T-249 §8（deprecate 裁量 + push_npm 自查）、DECISIONS.md ADR-0025（Q6/Q7 replica）/ ADR-0026（m 动作与覆盖集、§11.30 过滤列表）/ ADR-0027（step-up 契约，决策 4/8）/ ADR-0029（决策 4 熔断线例外通道）、docs/reverse/auth-model.md §1（users 端点表：DELETE 高置信）/ docs/reverse/rbac-model.md §1.2（groups ?includeUsers 高置信） |
| 下游消费者 | tech-lead（拆票）、architect（ADR-0030：SE 域端点群 wire 定案；ADR-0031 候选：GC 引用原子化）、dev-go-core（users/groups 端点）、devops/后端（usage 批量、permissions 过滤、GC）、web 前端 dev（消费新端点去扇出、m-holder 编辑器、OIDC 腿）、qa-engineer（N 序列 + 回归硬门槛）、tech-writer（SSO 铸 Token 路径、deprecate 口径）、conductor（git 瘦身授权门、tag） |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-24 | 初版草案（待 conductor 审）：M9 范围（conductor 种子 A~F 全承载 + D 项评估建议延后 M10）、FR-78~FR-83（用户组域端点缺口 / 扇出与 m-holder 可达 / GC 竞态根治 / OIDC step-up 腿 / 控制台工程债包 / 运营发布 chores）、兼容矩阵 10 条、N01~N28 验收命令骨架、F 池 28 条处置表（收编 18 / 延后 3 / 关闭 7）、开放问题 Q1~Q6 带暂行；随稿完成 milestone-8.md §0 勘误登记（U20 命令姿势）与 ROADMAP M8 勾账 |

---

## 1. 背景与目标

### 1.1 背景

M8 以「服务端契约零改动」为硬门槛（ADR-0029 决策 4）完成了控制台对齐。**契约冻结的代价是 UI 对齐过程中暴露的一批服务端缺口被熔断线排队**（决策 4 例外通道：行为规格发现「Artifactory 操作流在 BinFlow 既有 API 面上表达不了」时走 PM FR + architect 评审独立票——M9 即该通道的集中兑现窗口）。排队缺口按域归拢：

1. **用户与组域（T-237 §3 漂移①②③④）**：T-208 只落了 `enabled` 写侧（bool 指针），GET 无回显——用户列表 Status 列不可实现、编辑器只能「本页写过即已知」，带外禁用不可见；无 `DELETE /api/security/users/{name}`（Artifactory 有，高置信规格 docs/reverse/auth-model.md §1）；组无按组成员查询端点（Artifactory `?includeUsers=true` 有对应）——users/groups 页 N+1 逐用户 `getUser` 汇总。
2. **仓库/存储域（T-99 契约缺口 + T-246 QA-4）**：repos 列表「已用」列 per-repo `GET /v1/storage/usage/{key}`——N 仓 = N 请求（T-246 实测 ~170 请求/首屏）。
3. **权限域（T-241 §3.1）**：`GET /api/v1/permissions` 挂 CapSecurityRead（admin/readonly_admin 闭集），m-holder（普通 user 持 manage）读面全 403——控制台权限编辑器不可达，m-holder 只能走 API 路径（ADR-0026 §11.30 登记的过滤列表缺口）。

同时三类非契约缺口到期：

4. **GC 并行竞态（T-232 发现）**：`graceHours=0` 的 gc apply 可物理删除并行在途 blob（t134 实测 `manifest PUT 500 blob not found`）——e2e 全量被 `--workers=1` 权宜兜底至今，是所有并行验证的系统性瓶颈。
5. **OIDC step-up 控制台腿（T-242 §7，ADR-0027 决策 8 后半）**：服务端 mint grant 契约（`purpose=step_up` + `prompt=login`）M7 已落，console 消费面（401 → 引导 re-auth → 回跳续铸）缺位——OIDC 用户的 step-up 加固在真实部署不可用。
6. **运营债**：git 历史含 BOARD 巨 blob（BOARD.md 单文件已积至 MB 级且含行内重复注入串〔closure 段多轮重复〕，其全部历史版本 blob 累计为 clone/检索的系统性开销——精确体积与 blob 清单由 FR-83-AC1 dry-run 报告量化）；Jenkins release 只出 linux/amd64 单架构镜像（T-248 §遗留：2C VM 上 buildx+qemu 不现实，M5 buildx 多架构能力未进 CI）；npm deprecate 权限臂的产品裁量（T-249 遗留①）悬而未决。

M8 收官材料（T-246-qa §八）另登记 28 条候选债，需 PM 逐条定取舍（§4.8 处置表）。

**D 项（replica 隔离，ADR-0025 决策 1 遗留 / M7 PRD Q7 维持暂行）评估结论**：backing local 直写隔离需要「复制配置标记仓 + 写门拒非复制 principal」的联动语义，属复制域行为变更，宜独立 ADR 与专属验收面（真实双实例推流）。M9 服务端变更面已被 A/B 占满，**PM 建议：延后 M10「复制硬化」立项**（开放问题 Q5，维持 ADR-0025 已接受限制为暂行）。

### 1.2 M9 目标

> 一句话：兑现熔断线排队的服务端缺口（用户组域端点群 / usage 扇出 / m-holder 可达），根治两大可靠性缺口（GC 并行竞态 / OIDC step-up 控制台腿），把管理面从「权宜扇出 + 单并发验证」恢复到「契约干净 + 默认并发可信」，并清空 M8 债券池的到期项。

量化门槛（未达即里程碑不完成）：

| 指标 | M9 门槛 | 来源 |
|---|---|---|
| 管理面扇出 | repos 列表页首屏 XHR ≤ 3（现 ~171）；users 页 ≤ 2（现 1+N）；groups 页 ≤ 2（Playwright request 计数断言，50 仓 / 20 用户 / 10 组种子） | FR-78/FR-79 |
| 用户域读写闭环 | `enabled` 写→读回显一致；DELETE 用户全链（GET 404 / 既有 Token 即时 401 / 护栏拒内置 admin 与自删 / `user.delete` 审计） | FR-78 |
| m-holder 可达性 | 持 manage 的普通 user 可在控制台列表并编辑覆盖集内 permission target（L2 边界说明退役）；覆盖集外 target 零泄露（负向断言） | FR-79 |
| GC 并行正确性 | 压力腿（并发 PUT × grace=0 apply 交替）零误删零 blob-not-found；e2e 全量**默认并发**（≥4 workers）连续 3 轮全绿；`--workers=1` 兜底约定解除 | FR-80 |
| OIDC step-up | mock IdP 全链（401 → re-auth → 回跳续铸 → 令牌可用）+ grant 单次性重放 401 逐字 + local/LDAP 腿零回归 | FR-81 |
| 债券处置 | F 池 28 条全处置：收编 18（全部落地）/ 延后 3（M10+ 登记）/ 关闭 7（含 PM 裁定 2 条） | FR-82/FR-83 + §4.8 |
| 回归硬门槛 | M1~M8 全部 P0 序列复跑全绿；U 序列（含 a11y-sweep 52 扫描）零回归；服务端契约变更面 = `git diff m8-done..HEAD -- internal/ cmd/` 100% 归属 M9 豁免票（ADR-0030 域） | §8/§9 |

### 1.3 上游依赖与并行关系

- **ADR-0030（architect，前置产物）**：SE 域端点群 wire 定案——DELETE users 语义（响应形态 text/JSON、内置保护、principal 残留清理）、enabled 回显落点（列表 + 详情）、groups `?includeUsers` 与列表回显扩宽、`GET /api/v1/storage/usage` 批量形态、`GET /api/v1/permissions?filter=manage` 参数名与过滤语义。本 PRD 只约束行为与验收，wire 细节与 ADR 冲突时以 ADR 为准。
- **ADR-0031（候选）**：GC 引用原子化方案（候选：apply 物理删除前重验 upload_sessions 活跃引用 + blob 引用快照；或 storage 写锁协调）。方案选型归 architect，本 PRD 钉行为不变量。
- **实现分区**：FR-78（internal/auth + httpapi SE 域）与 FR-79（usage：storage/repo 域；permissions：auth 域只读面）area 不重叠可并行；FR-80（internal/storage/gc）独立；FR-81（web + oidc 回调缝）依赖零后端改动（ADR-0027 契约现成）；FR-82 多为 web/ 小票与 internal 小收敛，可插队；FR-83 为 release/运维票（git 瘦身 dry-run 不触碰工作树语义，多架构镜像在 CI 环境）。
- **QA 并行面**：N 序列按 FR 分组；扇出/竞态两条量化门槛需专门种子实例（50 仓 + 并发负载）。

---

## 2. 范围

### 2.1 In scope

| # | 来源 | 本 PRD 功能需求 | 优先级 |
|---|---|---|---|
| A（用户组域） | 种子 A + T-237 漂移①②③ + T-246 QA-4b | FR-78（enabled 回显 / DELETE users / groups `?includeUsers` + 列表回显扩宽——users 页 N+1 根治） | P0 |
| A（扇出/可达） | 种子 A + T-99 缺口 + T-241 §3.1 + T-246 QA-4a | FR-79（`/api/v1/storage/usage` 批量 + `/api/v1/permissions?filter=manage` 覆盖集过滤——m-holder 控制台可达） | P0 |
| B | 种子 B + T-232 | FR-80（GC graceHours=0 并行竞态根治——引用原子化，解除 `--workers=1` 兜底） | P0 |
| C | 种子 C + T-242 §7 + ADR-0027 决策 8 | FR-81（OIDC step-up 控制台腿：mint grant + prompt=login 回跳续铸） | P1 |
| E+F | 种子 E + F 池收编项 | FR-82（控制台与工程债包：redirect 移除〔Q3 终裁〕/ QA-3 / QA-5 / assert-tokens 扩面 / packument 收敛 / push_npm 自查 / 锚册口径 / 搜索框升级 / 文档措辞与 deprecate 口径 / pass-gate 登记 / U20 勘误） | P1 |
| E | 种子 E | FR-83（运营与发布 chores：git 历史瘦身〔force-push 须用户单独授权〕/ CI 多架构镜像 / 用户实例刷新提议〔dep:用户环境〕） | P1 |

前置产物（非 FR）：ADR-0030（SE 域 wire 定案）、ADR-0031 候选（GC 方案）。

### 2.2 Non-goals — M9 明确不做

**产品级（继承 PRODUCT.md，不变）**：不做 HA、不做 Xray 式扫描、不做 LDAP/SAML 新认证源、不做 UI 高级洞察、不做 Artifactory 全量 REST 兼容。

**M9 里程碑级 Non-goals**：

| 不做项 | 隔离边界 / 去向 |
|---|---|
| replica backing local 直写隔离（D 项 / ADR-0025 决策 1 遗留） | **建议延后 M10「复制硬化」**（开放问题 Q5；需复制配置标记 + 写门联动 + 双实例验收面，独立 ADR） |
| repos 列表/详情「制品/缓存」「更新时间」列（E-04 回显体扩字段） | 延后 M10+（`/api/repositories` wire 扩字段是另一契约变更面，M9 不叠加；F 池 #10） |
| 搜索漏斗三过滤 / 类型下拉 / 类型化结果列（R2 契约） | 延后 M10+（依赖 `/api/search` 扩参契约，不在种子内；F 池 #13） |
| Tokens 管理页（R6 落地票） | 延后 M10+（UI-23 占位态维持；F 池 #15） |
| 组 `adminPrivileges` 字段 | **有意不做**（ADR-0026 已裁「组不引入角色语义」；UI 以 manage 持有徽章同构；F 池 #9d，推翻须新 ADR——开放问题 Q4） |
| npm deprecate 权限放宽 | **PM 裁定维持 T-249 严格臂**（deprecate = 既有版本字段覆写 → 需 write+delete；理由见 §5.2 矩阵 EP-09 行；F 池 #5） |
| m-holder 的 repos 全局/过滤列表 | 不做（ADR-0026 §11.30 中 repos 侧过滤列表维持原判；M9 只落 permissions 侧——DeployDialog 双 403 降级臂继续承载） |
| git filter-repo 的 force-push 执行 | **未经用户单独授权不执行**（FR-83 只交付 dry-run + 备份 + 执行手册；授权门见 NFR-S50 / 开放问题 Q1） |

---

## 3. 用户与场景（M9 视角）

- **场景 A（大实例管理员）**：150 仓的实例上，管理员打开仓库列表页要发 ~170 个请求才能看全「已用」列；网络稍差页面就要等数秒。M9 后首屏 ≤ 3 请求，扇出与实例规模解耦。
- **场景 B（带外禁用用户的运维）**：安全事件后用 API 禁用账号，第二天在控制台用户列表应能看到「已禁用」状态（不再依赖「本页写过才可知」）；处置完毕删除账号，该用户的 API Token 即刻失效（不留季度级立足点——ADR-0027 威胁模型闭环）。
- **场景 C（仓库管理员 m-holder）**：应用组长 u9 持有 app 仓组的 manage，此前只能在权限编辑器页看到「无权限卡 + 边界说明」、靠 curl 维护 target；M9 后他在控制台直接列表并编辑覆盖集内的 target——服务端过滤保证他看不见也够不着覆盖集外的任何 target。
- **场景 D（SSO 企业的开发者）**：公司用 OIDC 登录，`auth.token_step_up=true` 开启；开发者在 Set Me Up 里铸 Token 被要求重新认证——跳 IdP 重新登录后自动回到铸造流程拿到令牌，全程不需要本地口令（OIDC 用户本来就没有）。
- **场景 E（值班 SRE）**：并行 CI 大批推镜像的同时跑 GC，不再出现「manifest PUT 500 blob not found」；e2e / 回归验证恢复默认并发，值班排障不被 `--workers=1` 的历史权宜误导。

---

## 4. 功能需求

约定：`BASE=http://127.0.0.1:8080`；`ADMIN="admin:password"`；Playwright spec 置 `web/e2e/m9/`；新端点 wire 细节以 ADR-0030 定案为准，本节 AC 钉行为与状态码。**全部新端点须过 architect 评审（ADR-0030），UI 票不得私加端点（ADR-0029 决策 4 纪律延续）。**

### 4.1 用户与组域服务端缺口（种子 A，P0）

#### FR-78 用户与组域端点补全：enabled 回显 / DELETE users / 组成员查询（internal/auth + httpapi SE 域 + web 消费面）

**用户故事**：
- 作为管理员，我在控制台或 API 禁用一个用户后，任何读取面（列表 Status 列、编辑器勾选框、GET 详情）都能如实看到禁用态——「写过的才知道」不可接受。
- 作为管理员，我能删除一个用户（Artifactory 同款能力），且删除后他的 API Token 与会话立刻失效——账号处置有终态。
- 作为管理员，用户列表页的 Groups 列不需要浏览器逐用户发请求汇总；组编辑器的成员穿梭直接来自组端点。

行为规格：

- **78.1 enabled 回显**：`GET /api/security/users`（列表）与 `GET /api/security/users/{name}`（详情）响应含 `enabled` 布尔；与 T-208 写侧（`userCreateBody/userUpdateBody` 的 `*bool`）闭环；默认 true。列表回显体**加宽**（additive）：补 `email`/`adminRole`/`groups`/`enabled`（Artifactory 列表是瘦回显 `{name,uri,realm}`——BinFlow 加宽为自有契约演化，Artifactory 客户端忽略多余字段零破坏）。UI 消费：用户列表 Status 列（启用/禁用 badge）；编辑器 `knownEnabled` 本地假设退役，改回显驱动。
- **78.2 DELETE /api/security/users/{name}**：门 = security:write（admin；readonly_admin 403）。护栏：内置 `admin` 不可删（4xx，原因文案含保护语义）；当前认证主体不可自删（4xx）。成功 → 200；随后 GET 404；该用户全部 API Token 即时失效（TokenRegistry.Verify 重查用户行——ADR-0025 护栏③同缝，行删除即失效）；活跃 session 失效；组员引用与 permission target principals 行清理（不留守护孤儿）。审计 `user.delete`。响应形态（text 对齐 Artifactory vs JSON）归 ADR-0030。
- **78.3 组成员查询**：`GET /api/security/groups/{name}?includeUsers=true` → 详情体附 `userNames[]`（对齐 Artifactory，高置信规格 rbac-model §1.2）；`GET /api/security/groups` 列表回显加宽：附 `membersCount`（或 `userNames`，归 ADR-0030）。UI 消费：组编辑器穿梭与 users 页 Groups 列改组端点/加宽列表数据源，N+1 链式并发退役。
- 组 `adminPrivileges` 字段**不实现**（§2.2；UI manage 徽章维持 T-237 同构形态）。

验收标准（AC）：

- **AC1（enabled 闭环）**：curl 建用户（`enabled:false`）→ `GET .../users/u9` 与列表 `jq '.enabled'` 均 false；禁用用户登录 401；重新启用（POST 部分 body `enabled:true`）→ 登录 200。
- **AC2（Status 列）**：Playwright——API 侧带外禁用用户 → 刷新用户列表 → Status 列呈「已禁用」；打开编辑器勾选框未勾（回显驱动断言）。
- **AC3（DELETE 全链）**：u9 自铸 Token → admin `DELETE .../users/u9` → 200；`GET .../users/u9` → 404；u9 的 Bearer Token 请求任意 API → 401；`DELETE .../users/admin` → 4xx；admin 会话自删 → 4xx；审计流含 `user.delete`（actor=admin, target=u9）；u9 原所属组的成员清单与 target principals 不再含 u9。
- **AC4（组成员端点）**：`GET .../groups/devs?includeUsers=true` → `userNames` 与逐用户详情汇总结果一致；无参数形态响应与现役兼容（不破坏既有消费者）。
- **AC5（扇出）**：Playwright request 计数——users 页首屏 XHR ≤ 2、groups 页 ≤ 2（20 用户 / 10 组种子；现状为 1+N 链式）。
- **AC6（回归）**：M7 V 序列用户族 + M8 users-groups.spec 复跑绿（wire 加宽对既有断言 additive）。

### 4.2 管理面扇出与 m-holder 可达性（种子 A，P0）

#### FR-79 usage 批量端点 + permissions 覆盖集过滤列表（storage/repo + auth 只读面 + web 消费面）

**用户故事**：
- 作为大实例管理员，仓库列表页的「已用」列一次请求拿全，页面加载时间与仓库数量解耦。
- 作为 m-holder（应用组长），我在控制台就能维护自己管辖仓的权限 target——不必学 curl；同时我绝对看不到管辖范围之外的任何 target。

行为规格：

- **79.1 usage 批量**：`GET /api/v1/storage/usage`（无参 = 调用者可见仓全量）或 `?repos=a,b,c`（点名子集）→ per-repo 用量结构（字段与单仓 `GET /api/v1/storage/usage/{key}` 同源）。权限：按单仓门（CanManageRepo(read) ∨ Can(r)）**在服务端过滤可见集**——无权仓不出现在响应（既不报错也不泄露存在性）。virtual 仓沿用「恒 — / 不适用」语义。UI 消费：repos 列表「已用」列改单请求注水，per-repo 请求退役。
- **79.2 permissions 覆盖集过滤**：`GET /api/v1/permissions?filter=manage`（参数名终裁归 ADR-0030）：admin/readonly_admin → 全量（与现役无 filter 行为等价）；普通 user → 仅返回 **repositories ⊆ 其 m 覆盖集** 的 target（即可编辑集，B1 union 语义的前置呈现），条目字段与现役列表一致。**信息隔离硬约束**：覆盖集外 target 的任何元数据（名称/仓库/patterns/principals）不出现在响应。无 filter 的 `GET /api/v1/permissions` 行为不变（CapSecurityRead 闭集，403 维持）。UI 消费：m-holder 直链权限列表/编辑器 → 覆盖集内 target 可列表、可进编辑器；L2 边界说明卡退役（编辑器内 B1 语义 admin-note 维持）。

验收标准（AC）：

- **AC1（usage 批量一致性）**：50 仓种子 → `GET /api/v1/storage/usage` 的 per-repo 值与逐仓单点端点逐仓一致；`?repos=` 点名子集只含点名仓。
- **AC2（usage 权限过滤）**：持部分仓 r 的用户 u8 调批量端点 → 响应仅含其可读仓；被排除仓的 key 不出现在响应 JSON 任何位置（负向 grep）。
- **AC3（扇出）**：Playwright——repos 列表页首屏 XHR ≤ 3（50 仓种子；现状 ~171）。
- **AC4（过滤列表正路）**：u9 持 target t-in（repos ⊆ 覆盖集）→ `filter=manage` 返回含 t-in 且字段完整；u9 控制台直链权限列表 → 页面渲染（非 L2）；进 t-in 编辑器 → 保存成功（wire 与现役 POST 一致）。
- **AC5（隔离负向）**：t-out（repos ⊄ 覆盖集）在 u9 的过滤响应中零出现；u9 对 t-out 的 POST/DELETE 维持 403（T-241 B1 腿复跑）；无 filter 端点对 u9 维持 403。
- **AC6（回归）**：admin/readonly_admin 两角色权限列表页与 V13/V14 复跑零变化。

### 4.3 GC 并行竞态根治（种子 B，P0）

#### FR-80 GC apply 引用原子化：graceHours=0 不得误删在途/已提交 blob（internal/storage + gc）

**用户故事**：作为 CI 并发推镜像的用户，管理员同时跑 GC 清理不会让我的 push 中途 500；作为管理员，我不必再要求 QA「跑测试请加 --workers=1」。

行为规格（行为不变量，方案归 ADR-0031）：

- gc **apply**（任何 `graceHours`，含 0）不得物理删除：(a) 仍被活跃 upload session 引用的 blob；(b) 候选集计算与物理删除之间新提交的 blob（引用重验——删除动作前以引用快照原子复核）。
- gc **dry-run** 在并行写负载下不得把 (a)/(b) 类 blob 报告为可删候选（或明确标注 `skipped: in-flight` 并不计入可回收量——形态归 ADR-0031）。
- 现役 GC 语义零回归：无并发时的 dry-run/apply 结果与 M4~M8 行为一致（候选判定、审计、stats 口径）。
- 解除权宜：e2e/CI/文档中 `--workers=1` 兜底约定移除（Makefile/脚本/README/报告口径）。

验收标准（AC）：

- **AC1（压力腿）**：Go race 测试——50 并发 PUT（generic + docker 腿）与 `graceHours=0` 的 dry-run/apply 循环交替 → 全部 PUT 成功、事后逐制品 GET 逐字节可下载、0 例 `blob not found`；`go test -race ./internal/storage/... ./internal/repo/...` 全绿。
- **AC2（默认并发全量）**：`npx playwright test`（默认并发，≥4 workers）连续 3 轮全绿（dind 腿按既有降级口径 skip）；t134 docker 腿无 500。
- **AC3（权宜解除）**：`grep -rn 'workers=1' Makefile scripts/ web/package.json docs/` 仅剩历史注记（无生效约定）。
- **AC4（回归）**：M4 GC 序列（W 系 governance 腿）与 M8 U13（dry-run 出报告不 apply）复跑绿。

### 4.4 OIDC step-up 控制台腿（种子 C，P1）

#### FR-81 OIDC mint grant 消费面：prompt=login 回跳续铸（web + oidc 回调缝；服务端契约零改动）

**用户故事**：作为 OIDC 登录的用户，在 `auth.token_step_up=true` 的实例上铸 Token 时，系统引导我重新认证一次——我用 IdP 账号登录后自动回到铸造流程，令牌到手；我不需要也不可能有「本地口令」。

行为规格（消费 ADR-0027 决策 4/8 既有契约，服务端零改动）：

- 触发：provider=oidc 的非 admin 会话在 SetMeUp 对话框/铸造面 `POST /api/security/token` → 401 `step_up_required` → UI 呈「重新认证」引导（替代 local/LDAP 腿的内联口令框），说明文案含「将跳转 IdP 重新登录」。
- 流：跳 OIDC authorize（`purpose=step_up`，强制 `prompt=login`）→ IdP 认证 → callback（不建新 session）→ 换发 mint grant → 302 回 console 铸造上下文（保留仓库/对话框上下文）→ 自动携带 `step_up_grant` 续铸 → 一次性令牌面板（T-242 形态）。
- 失败面：grant 过期/复用 → 401 `step_up_invalid` 逐字文案（ADR-0027 决策 5）内联呈现 + 「重新认证」重试入口；用户取消/Esc → 不铸、留在原上下文；mint 请求沿用 `silent401`（不触发全局会话跳转）。
- 零回归：admin 豁免臂、local/LDAP `step_up_password` 内联腿（T-242 已落）、`token_step_up=false` 默认关闭行为——全部原样。

验收标准（AC）：

- **AC1（全链）**：mock OIDC provider（T-158 mock 面先例）+ `BINFLOW_AUTH__TOKEN_STEP_UP=true` 实例——Playwright：OIDC 用户铸币 → 401 → 引导弹层 → mock IdP 登录 → 回跳 → 令牌面板；铸出 Token 以 Bearer `GET /api/system/version` = 200 对账；审计 `token.issue` 含 `step_up_method=oidc_reauth`。
- **AC2（单次性）**：spec 网络层重放同 `step_up_grant` → 401 `step_up_invalid` 且 `error_description` 与 ADR-0027 决策 5 逐字节一致。
- **AC3（回归）**：T-242 setmeup-deploy.spec（armed local 腿 + mock 腿）复跑绿；默认实例（step-up off）全量零变化。
- **AC4（文档）**：tech-writer 交付「SSO 用户铸 Token」路径（ADR-0027 后果项收口：OIDC 用户无本地口令，CLI 侧先经 console）。

### 4.5 控制台与工程债包（种子 E/F 收编，P1，单条 FR 打包）

#### FR-82 M9 债券池收编第一包：控制台与工程小票集（票面细目以 §4.8 处置表「收编 FR-82」行为准）

**用户故事**：作为 Artifactory 迁移用户，M8 发布的旧书签兼容期结束（Q3 终裁 M9 移除），路由表收敛为单一新 IA；作为开发者，e2e 缺省可跑、样式断言覆盖页面级 css、转义样板单源。

行为规格与验收标准（逐项 AC）：

- **AC1（redirect 移除，Q3 终裁）**：删 8 条 legacy `<Route>`/LegacyRedirect 与 20 条映射（`grep -rn 'LegacyRedirect' web/src` = 0）；shell.spec 映射腿退役；旧路由直开 → 404 壳（负向断言）；docs 操作路径对照表注记「旧书签失效公告」。
- **AC2（QA-3 树过滤复位）**：树「过滤当前层」下钻子层时过滤词复位（或清空提示）——Playwright：过滤 → 下钻 → 子层列表非零呈现，无需手动清空。
- **AC3（QA-5 e2e 缺省自洽）**：`t104`/`t146` 两 spec 缺省对齐 harness 约定（BASE 默认 `127.0.0.1:8080`、ADMIN_PW 默认 `password`，或 README 明示必导出）——裸跑 `npx playwright test e2e/t104-perf.spec.ts e2e/t146-docs.spec.ts` 按约定实例零环境失败。
- **AC4（assert-tokens 扩面）**：`scripts/assert-tokens.mjs` 扫描扩至 `pages/**/*.css`——注入非 token 色值的页面 css 使构建失败（负向验证）后复原。
- **AC5（packument 转义收敛）**：`internal/adapter/npm/packument.go` 第三份逐段转义同构体收敛至 `client.EscapePathSegments`——`grep` 同构体 = 0；npm 域 e2e/单测复跑绿（行为零变化）。
- **AC6（push_npm 覆写臂自查）**：`internal/replication/push_npm.go` 以非 admin principal 对目标 packument 合并写的追加臂验证（复用 T-249 判定器或等价语义）——测试覆盖「追加版本合并不因覆写臂 403」；若实测命中缺陷则同票修复并附真实双实例证据。
- **AC7（锚册口径统一 + 死锚退役）**：console-ux §10 只按家族口径（423）报数，旧落点口径（293/242）条目清理；死锚 112 家族按 §10.6 流程执行一轮退役（retired 表增量 + 对账器 `unregistered=0/broken=0` 维持）；§10.6 守卫规矩入票 AC 模板的建议移交 conductor（本 PRD 登记不代拍）。
- **AC8（共享层微清理）**：T-237/T-241 页面组自持对比度类（`role-warning`/`perm-verdict-*`）回退语义 badge（现值合规前提下的可选清理）；repos-deploy 入口 readonly_admin 预收敛与 tree-deploy 统一——两处 axe/行为断言随票。
- **AC9（顶栏搜索框升级）**：AppShell 顶栏搜索从「按钮跳 /search」升为真输入框（输入回车跳搜索页带 query，Esc 清空；`/search` autoFocus 行为维持）——键盘链 + 深链回归。
- **AC10（文档措辞两处）**：T-247 场景文档「npm CI principal 需 write+delete」→「write 即可（T-249 后）」；npm 接入文档明示 deprecate/unpublish 需 write+delete（PM 裁定口径，§5.2 EP-09）。
- **AC11（登记两腿）**：storage migration pass-gate 501 行「throwaway 配置无 dual-write」架构事实注记入 architecture.md（architect 执行）；milestone-8.md §0 U20 勘误已随本 PRD 发稿（percent-encode 预编码姿势）。

### 4.6 运营与发布 chores（种子 E，P1）

#### FR-83 M9 债券池收编第二包：git 瘦身 / 多架构镜像 / 实例刷新提议

**用户故事**：作为新clone仓库的贡献者，仓库不再携带 BOARD 巨 blob 的历史包袱；作为 arm64 集群用户，`docker pull` 拿到原生架构镜像而不是 cross-emu。

行为规格与验收标准：

- **AC1（git 瘦身 dry-run + 手册，授权门前置）**：`git filter-repo` dry-run 报告——`.git` 体积前后对比、BOARD 巨 blob 清单（含路径与大小）、m1~m8 tag 重打映射表；执行手册（镜像备份 → filter-repo → 校验 → force-push → tag 重推）成文入 `deploy/` 或 `docs/`；**force-push 执行须用户单独授权**（NFR-S50；本 AC 只交付 dry-run 产物，不触碰远端）。
- **AC2（多架构镜像）**：CI release 链产出 linux/amd64 + linux/arm64 双架构 manifest（暂行路线：goreleaser 双 arm 预编译二进制分别注入 Dockerfile，避免 2C VM 跑 qemu——Q6）；`docker manifest inspect` 显示两 platform；amd64 dogfood 腿维持（拉回 + readyz + 版本戳）；arm64 以 manifest/config `architecture` 字段校验 +（可行时）qemu-user 冒烟。
- **AC3（用户实例刷新提议，dep:用户环境）**：18080 实例（M7 期构建）刷新建议书（升级路径 + 数据兼容核对 + 回滚）提交用户决策——**非 M9 硬 DoD**，登记为发布检查单提示项。

### 4.7 D 项评估结论（不设 FR）

**replica backing local 直写隔离**：PM 评估结论 = **建议延后 M10「复制硬化」**。理由：① 需「复制配置标记仓 + 写门拒非复制 principal」的复制域行为变更 + 双实例真实推流验收面，独立 ADR 更干净；② M9 服务端变更面已被 FR-78/79/80 占满，塞入 D 项将挤占 P0 验收资源；③ ADR-0025 决策 1 的已接受限制（backing local 可直写）有虚拟只读门面兜底，风险敞口可控。维持暂行直至用户/conductor 终裁（开放问题 Q5）。

### 4.8 F 池处置表（T-246-qa §八 28 条，PM 逐条三选一）

| # | 债（源） | 判定 | 去向 / 理由 |
|---|---|---|---|
| 1 | 旧路由 redirect 移除（Q3 终裁；删 8 条路由 + shell 映射腿）〔T-235〕 | **收编** | FR-82-AC1（Q3 终裁即 M9 承诺） |
| 2 | 共享层余项：自持对比度类回退 badge；repos-deploy readonly 预收敛统一〔T-244〕 | **收编** | FR-82-AC8 |
| 3 | npm packument.go 第三份转义同构体收敛至 client.EscapePathSegments〔T-233〕 | **收编** | FR-82-AC5 |
| 4 | remote singleflight 负载敏感观察〔T-233〕 | **关闭** | 无缺陷实证（M3~M8 全绿）；转 NFR 观察口径登记，出现实证再立项 |
| 5 | npm deprecate 严格侧产品裁量〔T-249 遗留①〕 | **关闭** | **PM 裁定维持 T-249 严格臂**（deprecate=字段覆写→需 write+delete）：版本不可变性优先，npmjs 的 publish-only 语义与私服治理诉求冲突，标准发布路径 T-249 已保证 write-only 可用；文档明示（FR-82-AC10），推翻出口 = 用户终裁后新票 |
| 6 | replication/push_npm.go 非 admin principal 覆写臂自查〔T-249 遗留②〕 | **收编** | FR-82-AC6 |
| 7 | T-247 文档措辞更新（npm CI principal 需 write 即可）〔T-249→tech-writer〕 | **收编** | FR-82-AC10（与 #5 口径合并成文） |
| 8 | m-holder 覆盖集过滤列表端点（或 L2 定案回写 §7.10）〔T-241〕 | **收编** | FR-79-79.2（取「端点」分支，L2 边界卡随之退役） |
| 9 | 用户域契约缺口四条〔T-237〕：enabled 回显 / DELETE users / 组成员汇总 / 组 adminPrivileges | **收编**（3+1 拆判） | 前三条 → FR-78（78.1/78.2/78.3）；**adminPrivileges → 关闭**（ADR-0026「组不引入角色语义」有意不跟进，UI manage 徽章同构已落；推翻须新 ADR，开放问题 Q4） |
| 10 | repos 列表/详情「制品/缓存」「更新时间」列（等 E-04 扩字段）〔T-240〕 | **延后** | M10+：`/api/repositories` wire 扩字段是独立契约变更面，M9 不叠加（§2.2） |
| 11 | assert-tokens 扩展扫描 pages/**/**.css〔T-239〕 | **收编** | FR-82-AC4 |
| 12 | 顶栏搜索框升真输入框〔T-239〕 | **收编** | FR-82-AC9 |
| 13 | 搜索漏斗三过滤/类型下拉/类型化结果列（R2 契约后回填）〔T-239〕 | **延后** | M10+（R2 搜索契约票：`/api/search` 扩参设计 + UI 回填，非 M9 种子） |
| 14 | OIDC 腿 step-up（step_up_grant 回跳续铸）〔T-242〕 | **收编** | FR-81 |
| 15 | Tokens 页铸币路径联动（R6 落地票）〔T-242〕 | **延后** | M10+（R6 tokens 管理页整体落地，UI-23 占位维持） |
| 16 | 树全量 spacer 窗口化虚拟滚动（万级单层再升级）〔T-236〕 | **关闭** | P37 门已过（10k 实测 144ms），无更高规模需求输入；出现万级单层实证再立项 |
| 17 | 锚册家族口径统一 + §10.6 守卫入 AC 模板 + 死锚 112 家族退役流程〔T-244〕 | **收编** | FR-82-AC7（AC 模板化建议移交 conductor） |
| 18 | gc graceHours=0 并行竞态根治〔T-232〕 | **收编** | FR-80 |
| 19 | 存储 migration pass-gate 501 行架构事实登记〔RBAC 矩阵口径〕 | **收编** | FR-82-AC11（architect 注记腿） |
| 20 | 用户实例 18080 刷新提议（M7 期构建）〔T-245〕 | **收编** | FR-83-AC3（dep:用户环境，非硬 DoD） |
| 21 | QA-1 repo 详情 pre 滚动区键盘访问（axe serious）〔T-246〕 | **关闭** | 已随 T-244 §8 fix-forward 收口（a11y-sweep 52 扫描全零）；M9 仅作回归断言 |
| 22 | QA-2 GC 页 field-hint 链接下划线〔T-246〕 | **关闭** | 同上（T-244 §8 一行级修复 + 证明腿已绿） |
| 23 | QA-3 树「过滤当前层」跨层残留复位〔T-246〕 | **收编** | FR-82-AC2 |
| 24 | QA-4 管理面扇出两处端点候选〔T-246〕 | **收编** | repos usage → FR-79-79.1；users 组汇总 → FR-78-78.3（列表加宽） |
| 25 | QA-5 e2e 环境变量缺省对齐 chore〔T-246〕 | **收编** | FR-82-AC3 |
| 26 | PRD U20 骨架命令勘误（% 与空格腿需预编码姿势）〔T-246〕 | **收编** | 随本 PRD 发稿完成（milestone-8.md §0 勘误行 + 本 PRD §5.4 头注按预编码姿势书写） |
| 27 | console-ux「v2.0」版本口径分歧备案〔T-246〕 | **关闭** | 备案即收口：以实际 v1.x 增补版式为准（ADR-0029 转正勘误口径），本 PRD 引用实际版本号，不再追版本跳变 |
| 28 | UI-05 树 children 表列排序规格分歧备案（console-m8 §6.3 收窄）〔T-246〕 | **关闭** | conductor 已按冻结设计规格判过、备案成立（过滤 + 加载更多承载大目录）；管理面表格列排序已落，不再改树表 |

> 计数：**收编 18 / 延后 3 / 关闭 7**（#9 拆判 3 收编 + 1 关闭，行级计 1 收编；#5 含 PM 裁定 1 条、#9d 含 ADR 既裁 1 条）。延后 3 项（#10/#13/#15）进 M10+ 候选池登记于 ROADMAP。

---

## 5. 兼容性矩阵（M9 核心——新端点对 Artifactory 的对齐分级）

### 5.1 层级定义（契约域，沿用 M2~M7 分级口径）

| 层级 | 定义 |
|---|---|
| **A 兼容** | 端点路径/方法/语义对齐 Artifactory（高频子集承诺范围）；加宽回显为 additive、Artifactory 客户端零破坏 |
| **C 自有（/api/v1 或内部语义）** | 无 Artifactory 对应（BinFlow 自有概念如 m 覆盖集）或自有优化（扇出治理）；一律走 `/api/v1` 或内部行为修正 |
| **D 有意不兼容 / 不做** | 显式裁决不做（记 ADR 或 PM 裁定），矩阵留痕防再议 |

### 5.2 契约矩阵（10 条）

| # | 端点/契约面 | Artifactory 对应 | 层级 | 优先级 | 置信度 | 验收 |
|---|---|---|---|---|---|---|
| EP-01 | `GET /api/security/users` 列表回显加宽（email/adminRole/groups/enabled） | 列表为瘦回显 `{name,uri,realm}`；BinFlow 加宽 = additive 兼容 + Artifactory 用户禁用语义（enabled）对齐 | A | P0 | 高（auth-model §1 + T-208/237 实证） | N01/N02 |
| EP-02 | `GET /api/security/users/{name}` 详情 `enabled` 回显 | 同上（详情体补 BinFlow 自有 enabled 字段，语义对齐 Artifactory API 禁用用户行为） | A | P0 | 高 | N01 |
| EP-03 | `DELETE /api/security/users/{name}` | **有**：`200 text "The user: '<name>' has been removed successfully."`（auth-model §1 高置信）；BinFlow 护栏（内置 admin/自删拒删、Token 即时失效）为自有增强 | A | P0 | 高 | N03 |
| EP-04 | `GET /api/security/groups/{name}?includeUsers=true` → `userNames[]`；groups 列表回显加宽成员数 | **有**：`?includeUsers=true` 附 `userNames[]`（rbac-model §1.2 高置信）；列表加宽 additive | A | P0 | 高 | N04 |
| EP-05 | 组 `adminPrivileges` 字段 | 有（组级 admin 布尔，成员即 effective admin） | **D（不做）** | — | 高（rbac-model #6） | N05（负向：wire 零出现） |
| EP-06 | `GET /api/v1/storage/usage`（批量/点名） | 无对应（Artifactory 无列表级 usage 内联；BinFlow 扇出治理） | C（/api/v1） | P0 | — | N06 |
| EP-07 | `GET /api/v1/permissions?filter=manage`（覆盖集过滤） | 无对应（m 覆盖集为 ADR-0026 自有概念） | C（/api/v1） | P0 | — | N07 |
| EP-08 | OIDC step-up console 腿（mint grant 消费面） | 无对应（Artifactory 无 step-up mint） | C（自有，消费 ADR-0027 既有契约） | P1 | 高（ADR-0027 决策 4/8） | N10 |
| EP-09 | npm deprecate/unpublish 权限臂（write+delete） | npmjs.org 仅需 publish；Artifactory 覆写保护同族（repo-semantics §3） | **D（有意不兼容 npmjs；对齐 Artifactory 覆写臂）** | — | 中-高 | N20（文档断言 + T-249 既有测试族） |
| EP-10 | GC apply 引用原子化（内部语义修正） | 无对外契约对应（Artifactory GC 内部语义不可逆向） | C（内部不变量） | P0 | — | N08/N09 |

> 计数：**10 条 = A 4（EP-01~04）+ C 4（EP-06/07/08/10）+ D 2（EP-05/09）**。M9 无「语义等同但路径不同」类新增（EP-06/07 走 /api/v1 属自有优化而非 Artifactory 语义搬运）。对外既有契约面（/v2、五协议、session 族）本里程碑零改动（FR-82 工程收敛行为零变化）。

### 5.3 回归基线（M9 不反转既有断言）

| 既有断言 | M9 期望 |
|---|---|
| M1~M8 全部 P0 序列（C/D/H/W/V/U + a11y-sweep 52 扫描） | 零回归（新增面除外） |
| `GET /api/security/users` 既有消费（T-237 链式取数、whoami/session） | additive 加宽零破坏（既有断言不改动全绿） |
| `GET /api/v1/permissions` 无 filter 行为（CapSecurityRead 403 闭集） | 逐字节不变（EP-07 只增 filter 分支） |
| GC 无并发序列（M4 W / M8 U13） | 零回归（FR-80 只收紧误删面） |
| `--workers=1` 全量口径 | **反转**：默认并发 3 连绿后解除约定（现状权宜降级为历史注记） |
| L2 边界说明卡（m-holder 权限页） | **反转**：EP-07 落地后退役（可达性升级，编辑器内 B1 注记维持） |

### 5.4 M9 核心验收命令（N 序列骨架，QA 直接引用）

> **头注（承 U20 勘误）**：URL 含 `%`/空格/`#`/`?` 的腿一律预编码（`a%25b`/`a%20b`）——字面直传会在 curl 侧自伤（400/000），非服务端行为。

```bash
BASE=http://127.0.0.1:8080; ADMIN=admin:password
# ========== FR-78 用户与组域 ==========
# N01 enabled 闭环（EP-01/02）
curl -su $ADMIN -X POST $BASE/api/security/users -H 'content-type: application/json' \
  -d '{"name":"u9","email":"u9@x","adminRole":"user","password":"pw-u9-123","enabled":false}'
curl -su $ADMIN $BASE/api/security/users/u9   | jq '.enabled'    # false
curl -su $ADMIN $BASE/api/security/users      | jq '.[]|select(.name=="u9")|.enabled'   # false（列表回显）
curl -s -u u9:pw-u9-123 $BASE/api/system/version -o /dev/null -w '%{http_code}\n'      # 401（禁用拒登录）
# N02 UI Status 列（playwright）：API 禁用 → 列表「已禁用」badge + 编辑器勾选框未勾（回显驱动）
# N03 DELETE 全链（EP-03）
TOKEN=$(curl -su $ADMIN -X POST $BASE/api/security/token -H 'content-type: application/json' \
  -d '{"username":"u9","expires_in":3600}' | jq -r '.token')     # 字段名以现役 wire 为准
curl -su $ADMIN -X POST $BASE/api/security/users/u9 -H 'content-type: application/json' -d '{"enabled":true}'   # 先启用便于自铸（或 admin 代铸）
curl -su $ADMIN -X DELETE $BASE/api/security/users/u9 -o /dev/null -w '%{http_code}\n'  # 200
curl -su $ADMIN $BASE/api/security/users/u9 -o /dev/null -w '%{http_code}\n'            # 404
curl -s -H "Authorization: Bearer $TOKEN" $BASE/api/system/version -o /dev/null -w '%{http_code}\n'  # 401（护栏③）
curl -su $ADMIN -X DELETE $BASE/api/security/users/admin -o /dev/null -w '%{http_code}\n'  # 4xx（内置保护）
#   自删腿：admin 会话 DELETE /api/security/users/admin 同上；审计 jq 断言 user.delete 行存在
# N04 组成员（EP-04）
curl -su $ADMIN "$BASE/api/security/groups/devs?includeUsers=true" | jq '.userNames'    # 与逐用户汇总一致
#   扇出断言（playwright request 计数）：users 页 ≤2 / groups 页 ≤2（20 用户/10 组种子）
# N05 adminPrivileges 负向：组 GET/PUT wire 零出现该字段（jq 断言无键）

# ========== FR-79 扇出与 m-holder ==========
# N06 usage 批量（EP-06）：50 仓种子
curl -su $ADMIN $BASE/api/v1/storage/usage | jq '.repos["generic-local"].usedBytes'     # 与单点一致
curl -su $ADMIN $BASE/api/v1/storage/usage/generic-local | jq '.usedBytes'
#   权限过滤：u8（部分仓 r）→ 响应键集 ⊆ 其可读仓（负向 grep 排除仓名零出现）
#   扇出（playwright）：repos 列表页首屏 XHR ≤3
# N07 permissions 过滤（EP-07）：u9 持 t-in（repos⊆覆盖集）/ t-out（⊄）
curl -su u9:pw-u9-123 "$BASE/api/v1/permissions?filter=manage" | jq '.[].name'  # 含 t-in；t-out 零出现
curl -su u9:pw-u9-123 "$BASE/api/v1/permissions" -o /dev/null -w '%{http_code}\n'  # 403（闭集不动）
#   playwright：u9 直链 /admin/security/permissions 可达并保存 t-in（wire 与现役一致）；t-out POST 403（T-241 B1 腿复跑）

# ========== FR-80 GC ==========
# N08 压力腿：go test -race ./internal/storage/... -run 'GC.*(Parallel|Inflight)'   # 零误删/零 blob not found
# N09 默认并发全量：BASE=… ADMIN_PW=password npx playwright test   （无 --workers；连续 3 轮全绿）
grep -rn 'workers=1' Makefile scripts/ web/package.json docs/ | grep -v '#'   # 空（仅历史注记）

# ========== FR-81 OIDC step-up（EP-08）==========
# N10 mock IdP + BINFLOW_AUTH__TOKEN_STEP_UP=true 实例：
#   playwright e2e/m9/oidc-stepup.spec.ts：401→引导→mock IdP 登录→回跳→令牌面板；
#   Bearer GET /api/system/version=200 对账；审计 step_up_method=oidc_reauth
# N11 grant 单次性：网络层重放同 step_up_grant → 401 step_up_invalid（error_description 逐字）
# N12 回归：e2e/m8/setmeup-deploy.spec.ts（armed + mock 腿）复跑绿；默认实例全量零变化

# ========== FR-82 债包 ==========
# N13 redirect 移除：grep -rn 'LegacyRedirect' web/src   # 0；旧路由直开 → 404 壳（playwright 负向）
# N14 QA-3：树过滤下钻复位（playwright 子层非零呈现）
# N15 QA-5：裸跑 t104-perf + t146-docs（约定实例）零环境失败
# N16 assert-tokens：注入违规色值 pages css → build 失败（负向）→ 复原
# N17 packument 收敛：同构体 grep=0；npm 域测试复跑绿
# N18 push_npm 自查：非 admin principal 追加合并测试绿（或修复附双实例证据）
# N19 锚册：anchor-audit.mjs → unregistered=0/broken=0；死锚 retired 表增量落地
# N20 文档：t247 文档「write 即可」+ npm 接入文档 deprecate 需 write+delete（grep 断言）
# N21 搜索框：顶栏输入框回车跳 /search?q=…（playwright 键盘链）

# ========== FR-83 运营 ==========
# N22 git 瘦身 dry-run：filter-repo 报告（体积前后/blob 清单/tag 映射表）归档；远端零触碰
# N23 多架构：docker manifest inspect $IMG:$TAG → amd64+arm64 两 platform；amd64 dogfood 维持
# N24 实例刷新建议书成文（dep:用户环境，非 DoD 硬门）

# ========== 回归硬门槛（收口跑）==========
# M1~M8 全部 P0 序列复跑 + U 序列（含 a11y-sweep）零回归 + git diff m8-done..HEAD -- internal/ cmd/ 变更面 100% 归属 M9 豁免票
```

### 5.5 待校准项（ADR-0030 落地后回写）

| # | 项 | v1.0 暂行值 | 校准来源 |
|---|---|---|---|
| K18 | DELETE users 响应形态 | 200（text 对齐 Artifactory 或 BinFlow JSON 二选一） | ADR-0030 |
| K19 | users/groups 列表加宽字段清单 | email/adminRole/groups/enabled + membersCount | ADR-0030（与 EP-01/04 冻结） |
| K20 | usage 批量响应结构（map vs 数组）与 `?repos=` 参数名 | map（repo key → 用量结构）+ `repos` 逗号分隔 | ADR-0030 |
| K21 | permissions 过滤参数名与响应集语义（⊆ 覆盖集可编辑集） | `filter=manage` | ADR-0030 |
| K22 | gc dry-run 对在途 blob 的呈现 | 不列为候选（或 `skipped: in-flight` 标注） | ADR-0031 |

---

## 6. 非功能需求（NFR）

### 6.1 与已有 ADR / 规范的冲突/补充标注

| ADR/规范 | 冲突/补充点 | 本 PRD 立场 | 所需动作 |
|---|---|---|---|
| ADR-0029 决策 4（契约冻结） | M9 解冻 SE 域端点（正是决策 4 例外通道的兑现） | 变更面可枚举：全部新端点过 ADR-0030；`git diff` 归属审计为回归硬门槛 | ADR-0030 定案 |
| ADR-0026 §11.30（过滤列表 M8+） | EP-07 即该登记项的兑现 | 参数与语义按 §5.5 K21；CapSecurityRead 闭集不动 | ADR-0030 |
| ADR-0027（step-up） | 决策 8 后半（OIDC console 分流）此前无消费面 | FR-81 消费既有契约，服务端零改动；grant 台账进程内存维持 | — |
| ADR-0025 决策 1（replica 限制已接受） | Q7 处置到期 | 建议 M10 立项（§4.7）；M9 维持暂行 | 开放问题 Q5 |
| console-ux §3.2（旧路由 redirect） | Q3 终裁 M9 移除 | FR-82-AC1 执行；映射表与锚退役条目同步 v1.8 | ux-designer 回写 |

### 6.2 性能（M9 增量）

| NFR | 指标与验收方式 | 优先级 |
|---|---|---|
| NFR-P39 扇出预算 | repos/users/groups 列表页首屏 XHR ≤ 3/2/2（Playwright request 计数，50 仓/20 用户/10 组种子） | P0 |
| NFR-P40 批量端点性能 | `GET /api/v1/storage/usage` 100 仓 P95 ≤ 100ms（本机 scratch，与逐仓 N 请求总时延对比归档）；服务端匿名 P95 基线（M7 6.8ms / M8 0.606ms）偏差 < 10% 维持 | P1 |
| NFR-P41 GC 压力正确性 | FR-80-AC1 压力腿通过 + 数据不变量（全部已提交未删制品可完整下载） | P0 |
| NFR-P42 验证吞吐恢复 | e2e 默认并发全量单轮时长 ≤ 4 分钟量级（对比 --workers=1 的 6.6~7.6m，归档即可不设硬门） | P1 |

### 6.3 安全底线（M9 增量）

| NFR | 要求 | 验收 |
|---|---|---|
| NFR-S48 DELETE 护栏 | 内置 admin 与当前主体不可删（4xx）；被删用户 Token/Session 即时失效（Verify 重查缝）；principal/组引用清理无孤儿；审计 `user.delete` | N03 |
| NFR-S49 覆盖集信息隔离 | EP-06/EP-07 响应不得泄露无权仓/覆盖集外 target 的存在性（负向 grep + spec 断言）；错误面与 403 措辞不构成存在性预言 | N06/N07 |
| NFR-S50 git 瘦身授权门 | filter-repo 执行前全量镜像备份 + 对象计数校验；**force-push 与 tag 重推须用户单独授权**（BOARD 票面标注）；执行后 m1~m8 tag 映射校验 | N22 |
| NFR-S51 step-up OIDC 腿 | grant 不落 access log 明文（query 参数如经 302 携带需确认脱敏或改 body/fragment——归 ADR-0030 复核项）；grant 单次 + TTL ≤ 3600 维持 | N10/N11 + ADR-0030 |

### 6.4 可观测性（M9 增量）

- 新增审计事件：`user.delete`（actor/target/reason 摘要）；`token.issue` 的 `step_up_method=oidc_reauth` 分支（ADR-0027 决策 7 兑现）。
- gc apply 日志维持现役结构（candidates/deleted 计数）；FR-80 若引入 skipped-in-flight 计数则同步落日志（ADR-0031）。
- /metrics 指标族冻结口径维持（M7 起约定不变）。

---

## 7. 开放问题（Q1~Q6，均带暂行；需用户/conductor 决策，PM 不代拍）

| # | 问题 | 影响面 | 暂行口径（v1.0） |
|---|---|---|---|
| Q1 | **git force-push 授权与时机**：filter-repo 瘦身（去 BOARD 巨 blob）需要 force-push main + 重推全部 tag，何时执行 | FR-83-AC1；协作流（所有 clone 失效需 re-clone） | M9 内只交付 dry-run 报告 + 备份 + 执行手册；**执行等用户单独点头**（与「对外发布先确认」同级的授权门） |
| Q2 | **DELETE users 语义细节**：响应形态（Artifactory text vs BinFlow JSON）；除内置 admin/自删外是否保护「最后一个 admin」（Artifactory 组侧有先例 rbac-model §1.2.2） | EP-03 wire；QA 断言文案 | 暂行：内置 admin + 自删拒删（4xx）；「最后一个 admin」保护建议纳入（与组侧先例对称）；终裁归 ADR-0030 |
| Q3 | **m-holder 列表选型**：覆盖集过滤端点（/api/v1）vs security:read 降门 | EP-07；§7.10 措辞回写 | 暂行过滤端点（CapSecurityRead 闭集零改动——降门会扩大管理面读语义，风险不对称）；T-241 §7.10 回写随 EP-07 落地 |
| Q4 | **组 adminPrivileges 是否对齐**：Artifactory 有组级 admin 布尔（成员即 effective admin） | EP-05；rbac 域 | 维持不做（ADR-0026 既裁 + PM 复核：闭集角色与组 admin 布尔并存在提权审计上互为敌人）；如用户要求对齐须新 ADR + migration，M10+ |
| Q5 | **replica 隔离（M7 Q7 / ADR-0025 决策 1 遗留）归属**：M9 收编还是 M10 | §4.7；M10 规划 | **PM 建议 M10「复制硬化」立项**（复制配置标记 + 写门联动 + 双实例验收，独立 ADR）；M9 维持「已接受限制」暂行 |
| Q6 | **多架构镜像构建路线**：buildx+qemu（2C VM 不现实）vs 预编译双架构二进制注入 | FR-83-AC2 | 暂行预编译注入（goreleaser 六平台产物现成）；arm64 运行时冒烟以 qemu-user 可行为前提，不可行时以 manifest/config 校验承载 |

---

## 8. M9 验收剧本（QA 总纲）

1. **回归基线（硬门槛先行）**：M1~M8 全部 P0 序列复跑全绿；U 序列（含 a11y-sweep 52 扫描）零回归；`git diff m8-done..HEAD -- internal/ cmd/` 变更面 100% 归属 M9 豁免票（ADR-0030/0031 域内票）。
2. **用户组域**：N01（enabled 闭环）→ N02（Status 列回显驱动）→ N03（DELETE 全链 + 护栏 + 审计 + 引用清理）→ N04（组成员端点 + 扇出计数）→ N05（adminPrivileges 负向）。
3. **扇出与可达**：N06（usage 批量一致性 + 权限过滤 + repos 页计数）→ N07（过滤列表正路 + 隔离负向 + B1 复跑）。
4. **GC**：N08（压力腿 race）→ N09（默认并发全量 ×3 轮 + 权宜解除 grep）+ M4/M8 GC 序列回归。
5. **OIDC 腿**：N10~N12（mock IdP 全链 + 单次性 + armed 回归）。
6. **债包**：N13~N21 逐项（redirect/过滤复位/e2e 缺省/assert-tokens/收敛/自查/锚册/文档/搜索框）。
7. **运营**：N22（dry-run 报告归档，远端零触碰）→ N23（manifest 双架构 + dogfood）→ N24（建议书，非硬门）。
8. **文档**：tech-writer 交付——SSO 铸 Token 路径（AC4）、npm 权限口径（write 即可 / deprecate 需 write+delete）、用户管理增删闭环、操作路径对照表旧书签失效注记。
9. **F 池对账**：§4.8 处置表 28 条逐条核对——收编 18 项全绿、延后 3 项入 M10+ 候选池（ROADMAP 登记）、关闭 7 项留痕（含 PM 裁定 2 条与 ADR 既裁 1 条）。

---

## 9. M9 DoD

1. §4 全部 P0 AC（FR-78/FR-79/FR-80）经 qa 验证全绿；P1（FR-81/FR-82/FR-83）全绿（FR-83-AC3 建议书交付即可、AC1 以 dry-run 产物交付为限、AC2 以 manifest + amd64 dogfood 为限）；
2. §8 剧本全绿；§1.2 量化门槛表逐行达标（扇出/闭环/可达/竞态/OIDC 五项有实测数字归档）；
3. 回归硬门槛：M1~M8 全部 P0 序列复跑全绿；U/V/W 锚零回归（新增面与反转表所列除外）；契约变更面审计 100% 归属 M9 豁免票；
4. 前置产物齐备：ADR-0030 Accepted（SE 域 wire 定案，含 K18~K21 校准回写）；ADR-0031 Accepted 或架构注记（GC 方案 + K22）；console-ux v1.8（redirect 移除 + 锚册口径统一回写）；
5. tech-writer 四项文档交付（§8-8）；
6. NFR-P39/P41 达标归档；`make test`（race）/`make lint` 0 issues / gofmt 空维持；e2e 默认并发 3 连绿记录归档；
7. §4.8 处置表对账完成：收编 18 项全落地、延后 3 项登记 M10+、关闭 7 项留痕；
8. 主会话 git tag `m9-done`（对外发布任何制品、git force-push 均先经用户确认）。
