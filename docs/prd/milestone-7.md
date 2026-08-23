# PRD — M7 权限细化与运营硬化（细粒度 RBAC / 上传续传 REST 化 / Token 铸造加固 / 条件腿执行 / 技术债收编）

> **PRD 状态：v1.2**（M7 执行期勘误回写——V 序列骨架对齐真实路由与 wire 拼写〔T-215/217/221〕、token 措辞限定〔T-215/221〕、NFR-S41 TTL 域与 NFR-P33 基线注记〔T-222/224〕、H04 export 口径〔T-226〕、Q6 执行态更新；三分歧已收敛——T-214 终裁 + ADR-0026/0027/0028 Accepted；§7 Q1~Q7 中 Q1/Q2/Q3/Q5 已定案，Q6 进入执行态（T-228 在跑、T-227 待环境），Q4/Q7 维持暂行待用户终裁，推翻出口保留；文本与 ADR 冲突时以 ADR 为准）。

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/milestone-7.md` |
| 里程碑 | M7 — 权限细化与运营硬化（对应 ROADMAP.md「M7」条目；M6 §7 Q4 延续项 + T-209 遗留债 N6/O-2/O-1 + Q11 留的 M7+ 可选加固 + ADR-0025 决策 3 条件腿） |
| 状态 | **v1.2**（v1.1 基线上按 M7 执行期勘误回写：V 序列骨架三处路由姿势与 wire 字段拼写（T-215/T-217 review + T-221 实证）、readonly_admin 变更面 token 措辞限定「为他人」、NFR-S41 TTL 域如实、NFR-P33 基线分母勘误 + T-222 新归档基线、PUT users replace 语义注记（T-224）、H04 export 口径按 T-201（T-226）、§7 Q6 执行态；FR-64~FR-70、端点矩阵 13 条、V01~V35 结构不变；Q1/Q2/Q3/Q5 已定案，Q4/Q7 仍开放，Q6 执行中） |
| 上游依据 | PRODUCT.md（愿景与 Non-goals）、ROADMAP.md M7 节、docs/prd/milestone-6.md（v1.3 基线：§7 Q4「细粒度角色归 M7+ RBAC 里程碑」、Q8/Q9 条件腿、Q11「M7+ 可选加固：SSO session 铸 Token 需二次认证」）、DECISIONS.md（ADR-0005 零 CGO、ADR-0006 blob 布局与会话〔决策 2 已被 T-209 修订〕、ADR-0009 匿名读、ADR-0010 docker token、ADR-0014 console/session、ADR-0020 OIDC/LDAP、ADR-0025 M6 终裁）、reports/iteration-386.md（M6 收官 + 遗留债清单）、reports/agents/T-209-review2.md（N1~N7）、reports/agents/T-209-qa.md（O-1/O-2）、docs/reverse/auth-model.md §4（Artifactory 动作集与 permission target 行为）、docs/reverse/docker-registry.md §2（blob upload 会话与 416 语义）、M4 交付基线（milestone-4.md FR-27/FR-28 权限模型） |
| 下游消费者 | tech-lead（拆票）、architect（ADR-0026~0028：角色模型与 manage 派生 / step-up 契约 / 会话 Close 语义）、reverse-engineer（Artifactory manage 动作与 scoped admin 行为校准）、dev-go-core（auth/metadata/httpapi）、dev-go-storage + dev-registry-adapter（续传接线）、web 前端（角色与权限页扩展）、qa-engineer（V 序列验收）、tech-writer（RBAC 指南 / 续传说明 / step-up 指南 / 真实环境附录） |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-23 | 初版草案：M7 范围（conductor 种子 A~E 全覆盖）、FR-64~FR-70（RBAC 三条 / 续传一条 / step-up 一条 / 条件腿一条 / 技术债打包一条）、端点矩阵 13 条、V01~V35 验收命令草案、开放问题 Q1~Q7 |
| v1.1 | 2026-08-23 | T-214 裁决回写（conductor 终审通过，依据 ADR-0026/0027/0028 Accepted 终版）：① FR-64 readonly_admin 数据面改全域只读短路（否决「同权走 target」），读面清单勘误（删 `/api/v1/stats` 与 token 列表、补 `/api/v1/replications`、`storage/migration` 过门 501 注记），wire 统一 `adminRole`/`readonly_admin`（snake）；② FR-67/DU-01 改「GET 状态腿重启前已在，M7 修复跨重启存活」（T-211 实测，T-216 收窄为会话重建接线）；③ FR-68 对齐 ADR-0027 修订版（作用域含本地 session 臂、`step_up_required`/`step_up_invalid`、`auth.token_step_up_grant_ttl_seconds`）；④ FR-65 全局列表不随 manage 开放、建仓臂改 PUT、K11 定案（配额读含）；⑤ §7 Q1/Q2/Q3/Q5 已定案、Q4/Q6/Q7 维持暂行（推翻出口保留）；§5.6 增 token 列表勘误行 |
| v1.2 | 2026-08-23 | M7 执行期勘误回写（conductor 终审通过，依据 T-215/T-217 review-a·c、T-221/T-222/T-224/T-226 各 review/qa 报告；FR 编号、V01~V35 结构、端点矩阵 13 条与其他事实不动）：① **V 序列骨架对齐真实路由**——建号 `PUT /api/security/users/{name}`（POST /{name} 实为部分更新臂，缺用户 404；JSON-only，form 400）、建仓 `PUT /api/repositories/{key}`（无 POST 建仓路由，E-26 口径）、权限 target 编辑臂 `POST /api/v1/permissions` create-or-replace 恒 201（无 PUT /{name} 路由；v1.0/v1.1 所写 `POST /api/repositories`、`PUT /api/v1/permissions/t1` 均 404）；wire 字段 `repos`/`includePatterns`（数组，非 repositories/includesPattern）；② readonly_admin 变更面「Token 签发与吊销」限定为「**为他人**签发/吊销面」——自铸 200 属族 8 required-only（Q11 口径，T-215 review §四.2 判 architecture 对 + T-221 负面矩阵实证）；③ NFR-S41「TTL ≤5min」与 ADR-0027 域 [60,3600] 不符，以 ADR 为准（默认 300s=5min 不变，上限域如实；T-224 实测 3600 可服务、5 拒启动）；NFR-P33 登记勘误注记——M6 G27 基线（T-172）未存 P95 分母（T-222 O-2），引 T-222 新归档基线（匿名 P95 6.8ms / 认证 2748ms）；④ FR-64/FR-66 加 PUT users replace 语义注记——只翻 `adminRole`/`enabled` 也须带 email+password（T-224 非缺陷②，影响 enabled 翻转姿势）；⑤ H04「S3 export 只导元数据」按 T-201 裁定回写为自包含包口径（T-226 B-3/d-1：blob 随行、import 不依赖原桶）；⑥ §7 Q6 更新执行态——T-228 环境 2026-08-23 解除（用户 VM + 真实 OSS 7.84.10）已在跑、T-227 仍 `dep:用户环境`（真实 AWS）；Q4/Q7 维持暂行 |

---

## 1. 背景与目标

### 1.1 背景

M6（`m6-done`，2026-08-23）交付了企业就绪基座：S3 后端、OIDC/LDAP、push 复制、Prometheus 指标、`bf` CLI、迁移工具。收官留下三类明确欠账，全部有溯源：

1. **权限粒度**（M6 §7 Q4 已裁）：现权限模型是 permission targets × repos × patterns（M4 FR-27/FR-28，动作集 read|write|delete）+ 用户级 admin 布尔——「能看全部治理面但不能改」与「团队自管仓库权限」两类高频企业诉求无法表达。ADR-0025 决策 1 也把「replica 端完整只读隔离」指派到「M7+ RBAC/复制里程碑」。
2. **能力接线缺口**（T-209 review2 N6 / qa O-2）：存储引擎 `ResumeSession` + `upload_sessions` 表（migration 010）已就位且被单测钉死，但 docker HTTP 层会话注册表是进程内存——重启后旧 upload URL 一律 404 `BLOB_UPLOAD_UNKNOWN`，客户端只能从零重传；引擎能力无生产调用方。
3. **Token 铸造面**（M6 §7 Q11 留的 M7+ 可选加固）：Q11 裁定开放非 admin 自助铸 Token 后，「session 被盗 → 铸长效 Token 持久化立足点」的威胁以护栏对冲，SSO session 铸管理 Token 的二次认证明确留 M7+。

另有 M6 收官遗留债一组（N3 ctx 取消窄窗、O-1 干净停机清会话、N2 restart 臂注释、internal/auth 53 条既有 lint、008/009 sql 行尾）与两条环境条件腿（Q8 真实 AWS S3、Q9 真实 Artifactory，ADR-0025 决策 3 已定 MinIO/OSS 等价口径）。

### 1.2 M7 目标

> 一句话：交付细粒度 RBAC（read-only admin / 仓库级 admin / 角色管理）、docker blob 上传跨重启续传的 REST 可见性、SSO session 铸 Token 的二次认证（可选增强），执行 Q8/Q9 条件腿，收编全部点名技术债——让 M6 的企业基座「管得住、传得稳、铸得安全」。

量化门槛（未达即里程碑不完成）：

| 指标 | M7 门槛 | 来源 |
|---|---|---|
| RBAC | read-only admin 治理读面全通、全部变更面 403 且零副作用；repo admin 经 `manage` 可管所属 permission target；M4 W 序列权限回归零回退 | FR-64/FR-65 |
| 续传 | 本地 filestore 后端下，docker blob upload 经 kill -9 **与** SIGTERM 两种重启后均可按 Docker Registry spec 续传（GET 204+Range / 错位 416+Range）；S3 后端契约零回归 | FR-67 |
| step-up | 开关开启时非 admin web session（含本地用户）无二次凭据铸管理 Token → 401；Basic/admin 臂不受影响；默认 false（Q5 已定案，ADR-0027） | FR-68 |
| 条件腿 | Q8/Q9 用户环境到位 → 真实环境记录归档；未到位 → 按 ADR-0025 决策 3 等价口径收口，不阻塞 DoD | FR-69 |
| 技术债 | 全仓 golangci-lint 0 issues（含 internal/auth 53 条清零）；全仓 `-race` 绿 | FR-70 |
| 既有零回归 | M1~M6 全部 P0 序列在本地 filestore 下复跑全绿 | 回归硬门槛 |

### 1.3 上游依赖与并行关系

- **架构依赖（architect）**：三条新 ADR 已定案（均 Accepted，2026-08-23，T-214）——ADR-0026（角色模型与 manage 派生能力清单）、ADR-0027（step-up 交互契约与 mint grant 形态，修订版）、ADR-0028（会话 Close 语义修订：牵 ADR-0006 决策 2 勘误④ 与 T-209 qa O-1 判定）。本 PRD 只约束可观察行为，与 ADR 文本冲突时以 ADR 为准。
- **逆向参考（reverse-engineer）**：docs/reverse/rbac-model.md（2026-08-23）已回答校准问题：实例级无角色层 / 无 read-only admin（#1/#2 高置信）、manage 为 ACE 动作而仓库级 admin 的正式对应物在 project 域——BinFlow 无 projects，target 的 `m` 是最小诚实同构（ADR-0026 已引用）；docker 续传语义以 Docker Registry HTTP API v2 公开规范为准（docs/reverse/docker-registry.md §2 已有分块/416 行为）。
- **实现分区**：RBAC 面（internal/auth + internal/metadata + internal/httpapi + web 用户/权限页）与续传面（internal/adapter/docker + internal/storage Close 语义）area 不重叠，可并行；step-up 依赖 RBAC 角色判定（非 admin 判定复用），排后。
- **QA 并行面**：V 序列以 curl + Playwright + docker 为主；条件腿需用户提供环境（FR-69 单列）。

---

## 2. 范围

### 2.1 In scope（与 conductor M7 种子 A~E 一一对应）

| # | 种子 | 本 PRD 功能需求 | 优先级 |
|---|---|---|---|
| A | 细粒度 RBAC（Q4 延续）：read-only admin / 仓库级 admin / 角色管理 | FR-64（角色模型与分配，含 read-only admin 行为）+ FR-65（`manage` 动作与仓库级 admin 派生）+ FR-66（控制台角色/权限管理扩展） | P0/P0/P1 |
| B | 上传续传 REST 化（N6/O-2） | FR-67（docker blob 上传跨重启续传 REST 化；范围裁定见行为规格） | P1 |
| C | Token 安全加固（Q11 留的 M7+ 可选项） | FR-68（SSO session 铸管理 Token 的二次认证，可选增强） | P2 |
| D | 条件腿（Q8/Q9） | FR-69（真实 AWS S3 / 真实 Artifactory 环境验收执行，`dep:用户环境`） | P2 |
| E | 技术债清理打包 | FR-70（N3 + O-1 + N2 + internal/auth lint + sql 行尾，单条打包） | P1 |

### 2.2 Non-goals — M7 明确不做

**产品级（继承 PRODUCT.md，不变）**：不做 HA 集群、不做 Xray 式扫描、不做 Artifactory 全量 REST 兼容、不做 UI 高级洞察报表。

**M7 里程碑级 Non-goals**（含对 M6 §2.2「归 M7+」条目的显式顺延声明）：

| 不做项 | 归属 | M7 的隔离边界 |
|---|---|---|
| 自定义角色 / 角色继承 / 角色层级树 | M8+ | 角色为**闭集**（`user / readonly_admin / admin` 三值，Q1 已定案——ADR-0026；新增角色 = 架构变更，须新 ADR）；「仓库级 admin」经 permission target 的 `manage` 动作派生，非独立角色实体 |
| permission target 之外的资源域授权（按 packageType / 按系统配置域） | M8+ | 授权单元仍是 repo × pattern（M4 语义零变更） |
| SAML / 多 IdP / LDAP 分页 / pull 复制 / 多源复制 / Grafana 预置 / bf 完整命令集 / 迁移工具多协议与在线迁移 | M8+ | M6 §2.2 将其排为「M7+」= M6 之后不特指 M7；M7 仅收编种子明列的 RBAC / 条件腿 / step-up，其余继续顺延，本表为防歧义声明 |
| S3 后端 blob 上传续传 | Q4（暂行不做） | S3 multipart 状态由 S3 服务端持有；`ResumeSession` 维持 hard 404 契约（`TestS3ResumeSessionNotSupported`） |
| generic / maven / npm / pypi 协议续传 | 不排期 | 四协议上传均为单体 PUT（无分块会话语义），无续传面可接（种子 B 的「可能含其他协议」裁定为否） |
| replica backing 仓直写隔离本体 | Q7（暂行延后） | ADR-0025 决策 1 指派「M7+ RBAC/复制里程碑」；M7 只交付 RBAC 使能位，本体开关默认不做，Q7 请用户终裁 |
| `/metrics` 新指标族 | — | M7 不新增必需指标族（FR-61 M7+ 增补项继续冻结） |

---

## 3. 用户与场景（M7 视角）

- **场景 A（值班与合规）**：SRE 值班与内审员需要看仓库清单、用户、权限、审计、复制状态来排查问题，但不应有改配置的能力——平台 admin 给其账号设 `readonly_admin`（wire 枚举 snake 形），而不是共享 admin 口令。
- **场景 B（团队自治）**：应用团队自管 `app-local` 仓库的权限（给 CI 账号开 write、给新成员开 read）。平台 admin 建一个含 `manage` 动作的 permission target 给 `app-admins` 组，之后权限调整不再需要平台 admin 介入。
- **场景 C（弱网大镜像））：CI 在跨国链路 push 2GB 镜像层，传到 70% 时服务器因升级重启（compose restart）——docker 客户端按 spec 查询 upload 状态拿到权威 offset 续传，而不是从零重传 2GB。
- **场景 D（session 被盗））：开发者的浏览器 session 被 XSS 窃取；攻击者想铸一枚长效管理 Token 持久化立足点——step-up 开启后被二次认证挡下（session 可用 ≠ 可铸 Token）。
- **场景 E（迁移收官）**：企业迁移团队拿到真实 AWS bucket 与真实 Artifactory 企业版实例，Q8/Q9 条件腿补上真实环境证据链。

---

## 4. 功能需求

约定：`BASE=http://localhost:8080`、`admin`/`$ADMIN_PW` 沿用 M6；`-n` curl 姿势沿用各序列既有写法。V 序列命令见 §5.4。P0/P1/P2 沿用 M1 定义。

### 4.1 细粒度 RBAC（种子 A，P0）

#### FR-64 角色模型与角色分配（dev-go-core：internal/auth + internal/metadata〔migration 011〕+ internal/httpapi）

**用户故事**：
- 作为平台 admin，我把内审员 alice 的账号设为 read-only admin——她能看全部治理面（仓库/用户/权限/审计/复制状态），但一切写操作被拒，且我不必为她单独维护一套「只读镜像权限」。
- 作为安全负责人，任何角色变更（谁被提为 read-only admin）都要留审计痕迹，且除 admin 外无人可改角色。

行为规格：

- **角色闭集（已定案：Q1，ADR-0026）**：`user`（缺省）< `readonly_admin` < `admin`。承载形态：`users` 表新增 `role` 列（migration 011），存量数据回填 `is_admin=1 → role=admin`、其余 → `user`；`is_admin` 布尔列保留为兼容视图（admin ⇔ role=admin，M8 移除）。**wire 字段名 = `adminRole`（camelCase）、DB 列名 = `role`，handler 一处映射（quotaBytes→quota_bytes 先例）；枚举值 snake 形 `user | readonly_admin | admin` 与 DB/代码常量同拼**（ADR-0026 决策 6，T-214 全局裁定）。
- **角色分配面**：兼容面 `PUT|POST /binflow/api/security/users/{name}` 增可选字段 `adminRole`（枚举，缺省 `user`）；`admin` 布尔语义不变（`admin=true` ⇔ `adminRole=admin`），两者冲突（如 `admin=false` + `adminRole=admin`）→ 400 纯文本。角色变更**即时生效**（含该用户存量 Token 的管理面权限——数据面权限仍只走 permission targets，不受角色影响）。**臂语义注记（v1.2 勘误，T-224 非缺陷②）**：`PUT /{name}` 为 **replace 语义**——只翻 `adminRole`/`enabled` 也须携带 `email`+`password` 全量体（缺 email → 400 "Please provide a valid user email."，Artifactory 兼容文案；**影响 enabled 翻转姿势**）；`POST /{name}` 为部分更新臂（缺用户 404）；建号臂 = PUT（create-or-replace，201）或集合 POST；两臂均 JSON-only（form 编码 400——T-221 实测）。
- **read-only admin 的行为边界（已定案：Q2，T-214① + ADR-0026 决策 1）**：
  - 读面全通（11 端点；T-214① 勘误定稿）：`GET /api/repositories`、`GET /api/repositories/{key}`、`GET /api/v1/health`、`GET /api/security/users`（列表/单查）、`GET /api/security/groups`、`GET /api/v1/permissions`、`GET /api/v1/audit`、`GET /api/v1/replication/status`、`GET /api/v1/replications`（配置列表，补入）、`GET /api/v1/storage/stats`、`GET /api/v1/storage/migration`（未装配双写的裸实例**过门后 501**，矩阵断言按「过门 501」判——T-211 实测佐证）。勘误：v1.0 所列 `GET /api/v1/stats` 实为 `/api/v1/storage/stats`（去重）；`GET /api/security/token`（列表）**端点不存在**（M1 E-17 有意不存在，T-211 实测 404 佐证），自清单删除。
  - 变更面全拒 403：仓库/用户/组/权限 target 的一切写法、Token **为他人**签发与吊销面（**自铸 200**——属族 8 required-only，Q11 口径；v1.2 措辞限定：T-215 review §四.2 判 architecture 对，T-221 负面矩阵实证 readonly_admin 自铸 200 / 为他人铸 403）、复制配置写、GC 触发（**全路由 `system:write`，含 dry-run/`apply=false`**——T-214① 否决 v1.0「dry-run 开放」暂行）、配额写、系统配置。
  - 数据面（制品读写）：**全域只读**——`r` 恒放行，`w`/`d`/`m` 恒拒；permission targets **不参与** readonly_admin 的任何求值（内容面与仓库域均角色短路）；与 target 组合 = **无效**而非非法（不引入 principals 校验报错，组合静默无效果）——ADR-0026 决策 1 / T-214①，否决 v1.0「同权走 target」措辞。
- **最小权限**：仅 `admin` 可写 `adminRole`/`admin` 字段；`readonly_admin` 改任何用户 → 403。groups 不引入角色语义（M4 FR-27-AC9 有意不兼容断言维持）。
- **审计**：角色变更记 `user.role.change`（actor + old/new + target user）。
- **docker token / 匿名臂零影响**：`/v2/token` 签发与 ADR-0009 匿名读不变；readonly_admin 的 push token 在 push 时逐请求 403（与 REST 面 403 同源，ADR-0026 决策 5）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-64-AC1 | V01：admin `curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/users/alice -H 'Content-Type: application/json' -d '{"email":"alice@t.io","password":"pw123","adminRole":"readonly_admin"}'` → **201**（建号臂 = PUT create-or-replace 或集合 POST；POST /{name} 为部分更新臂，缺用户 404；JSON-only，form 400——v1.2 勘误，T-221 实证）；`GET .../users/alice` 回显 `adminRole=readonly_admin`；DB `users.role` 列落值（sqlite 直查） | P0 |
| FR-64-AC2 | V02：alice（readonly_admin）读面全通——§行为规格所列 11 个 GET 端点逐一通过（200；`storage/migration` 裸实例按「过门 501」判）（curl 序列） | P0 |
| FR-64-AC3 | V03：alice 变更面全拒——`PUT /api/repositories/alice-repo`（建仓臂，无 POST 建仓路由——E-26 口径）、`PUT /api/security/users/bob`、`DELETE /api/security/groups/devs`、`POST /api/v1/permissions`（create-or-replace，无 PUT /{name} 路由）、`POST /api/security/token/revoke`、GC/配额写各一腿 → 全 403；且零副作用断言（写后再 GET 对应实体逐字未变）（v1.2 勘误：v1.0/v1.1 所写 `POST /api/repositories`、`PUT /api/v1/permissions/t1` 路由不存在 404，等效真实臂如上——T-221 实证） | P0 |
| FR-64-AC4 | V04：即时生效——alice 原为 user 时 `GET /api/security/users` 403 → admin 升角色后**同一 Token 不换发**重放 → 200 → 降回 user → 再 403（无重启、无延迟窗口） | P0 |
| FR-64-AC5 | V05：越权与冲突腿——alice 改 bob 角色 → 403；`admin=false`+`adminRole=admin` → 400；非 admin（普通 user）带 `adminRole` 字段建用户 → 403 | P0 |
| FR-64-AC6 | V06：审计——`GET /api/v1/audit?action=user.role.change` 可见事件（actor=admin、target=alice、old/new 角色）；M4 W 序列组权限回归（W17~W21）复跑零回退 | P0 |

#### FR-65 `manage` 动作与仓库级 admin 派生（dev-go-core：internal/auth + internal/httpapi）

**用户故事**：
- 作为平台 admin，我给 `app-admins` 组在 `app-local` 仓上授 `manage`——此后该团队自己维护这个仓的 permission target（加人/调权限），不再给我提工单。
- 作为应用团队负责人（app-admins 组），我能调权限，但建仓/删仓/管用户仍然只有平台 admin 能做——权限下放有精确边界。

行为规格：

- **动作集扩展**：permission target 的 principals 动作集从 `read|write|delete` 扩为 `read|write|delete|manage`（对齐 Artifactory `AceInfo` 动作集子集，docs/reverse/auth-model.md §4；`annotate/distribute/managedXrayMeta` 不跟进）。既有 target 与 AC 语义零变更（无 manage 位 = 行为不变）。
- **manage 派生能力（已定案：K11，ADR-0026 决策 3）**：对持有 `manage`（经 user 或 group principals）且 target 命中的 repo×pattern（`m` 的 target 匹配**只判 repos[]**，includes/excludes 不参与——manage 是仓库配置权、无路径子域）：
  1. 可读其 manage 覆盖仓库的**单仓配置**（`GET /api/repositories/{key}` 200）；全局列表 `GET /api/repositories` **不随 manage 开放**（CapRepoRead；过滤列表 M8+，architecture §11.30——T-214 P7）；
  2. 可创建/编辑/删除** repositories 集合 ⊆ 其 manage 覆盖 repo 集**的 permission target（覆盖集判定 = ⊆ 已定案；门 = CapSecurityWrite ∨ 覆盖集校验，body 依赖故在 handler 判定，越界 403——ADR-0026 决策 3；对齐 Artifactory「manage = 可管理该 target」语义）；
  3. 可读制品授权位（`GET /api/storage/{repo}/{path}?permissions` 视图，走 CanManageRepo）；
  4. 可读所属仓库的配额与用量（`GET /api/v1/storage/usage/{repo}` 归仓库域：CanManageRepo(read) ∨ Can(r)，W26b 既有 r 放行零回归；quota 配置与用量观测同域且 m-holder 可设 quotaBytes 不可不见用量——K11 定案，v1.0 暂行「不含配额」被推翻，T-214 P9）。
- **manage 不是数据面权限**：`manage` 不隐含 read/write/delete——制品读写仍需显式授予（与 Artifactory 动作正交语义一致）。
- **不可越界**：manage 持有者不可建/删仓库、不可管用户/组/角色/Token、不可触发 GC/配额/复制配置——全部 403。
- **admin 隐式含 manage**（admin 全权不变）。
- **replica 隔离**：不并入（Q7 暂行，见 §2.2）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-65-AC1 | V07：admin 建 target `t-app`（repos=[app-local]、includePatterns `**`、principals groups{app-admins:[read,write,delete,manage]}）+ 用户 carol 入组 → carol `POST $BASE/binflow/api/v1/permissions`（create-or-replace 同名 body，principals 增 user dave:[read]）→ **201**（无 PUT /{name} 路由——v1.2 勘误，T-217 review/T-221 实证；wire 键 `repos`/`includePatterns`）；dave `curl -su dave:... GET $BASE/binflow/app-local/lib.a`（预先放入制品）→ 200 | P0 |
| FR-65-AC2 | V08：边界腿——carol `PUT /api/repositories/new-repo`（建仓臂，repo 不存在；router 无 POST 建仓路由——T-214 P8）→ 403；`POST /api/v1/permissions`（create-or-replace，引用 other-repo，超出覆盖集）→ 403；`PUT /api/security/users/*` → 403；`POST /api/security/token/revoke` → 403 | P0 |
| FR-65-AC3 | V09：正交腿——target 只授 `manage`（无 r/w/d）的用户 carol2：`GET /api/repositories/app-local` 200（单仓详情；全局列表仍 403）、`POST /api/v1/permissions`（编辑 ⊆ 覆盖集的 target）→ 201，但 `PUT $BASE/binflow/app-local/x.bin`（上传）→ 403、GET 制品 → 403 | P0 |
| FR-65-AC4 | V10：零回归——M1 C22/C27、M4 W19/W19c/W21 权限序列复跑全绿（无 manage 位的行为逐字不变） | P0 |
| FR-65-AC5 | V11：真实客户端腿——carol 所在 app-admins 组授 r/w/d/manage 后：`docker push $BASE/app-local/myimg:t` 成功且 `docker pull` 成功；`mvn deploy`/`npm publish` 各一腿成功（manage 位存在不动摇协议行为） | P1 |

#### FR-66 控制台角色与权限管理扩展（web 前端 + dev-go-core 配合）

**用户故事**：作为管理员，我在 UI 用户页直接选角色（wire 三值 `user` / `readonly_admin` / `admin`），在权限页 principals 面板勾选 `manage` 动作——不用切到 curl。

行为规格：用户编辑页增「角色」下拉（三值，仅 admin 可见可改）；权限 target 页动作位扩展 `manage` 复选；read-only admin 登录后全部管理页可见但编辑动作禁用（或提交后如实呈现 403 服务端文案）；错误呈现服务端原文（沿用 M4 FR-28 约定）。**调用姿势注记（v1.2，T-224 非缺陷②）**：前端用户写操作按服务端臂契约——角色变更走 PUT 全量 replace 体（含 email+password）或 POST 部分更新臂，`enabled` 翻转同理须带全量体（勿只发改动字段）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-66-AC1 | V12：Playwright——admin 在 UI 把 alice 设为 readonly_admin → curl `GET /api/security/users/alice` 回显 `adminRole=readonly_admin` | P1 |
| FR-66-AC2 | V13：Playwright——alice 登录控制台：仓库/用户/权限/审计页可见；「新建仓库」等编辑动作禁用或提交后呈现 403；无任何 UI 路径能绕过服务端判定 | P1 |
| FR-66-AC3 | V14：Playwright——权限页编辑 `t-app` principals 勾选 manage 保存 → curl `GET /api/v1/permissions/t-app` 回显 manage 位 | P1 |

---

### 4.2 上传续传 REST 化（种子 B，P1）

#### FR-67 docker blob 上传跨重启续传（dev-registry-adapter：internal/adapter/docker + dev-go-storage 联动）

**用户故事**：
- 作为 CI 工程师，2GB 镜像层传到 70% 时服务器重启（升级或崩溃）——客户端向 upload URL 查询状态拿到权威 offset，从断点续传，省掉整个 2GB 重传。
- 作为运维，重启（含 compose restart 的干净停机）不再让在途上传作废——大上传跨维护窗口存活。

行为规格：

- **接线**：docker HTTP 层会话注册表不再仅是进程内存——上传会话以 `upload_sessions` 表（migration 010，T-209 交付）为事实源，注册表跨重启重建（T-216 范围已收窄为「会话重建接线」：lazy 重建 + `Offset()` 权威化，非状态腿补齐——T-211 实测确认）；upload URL 中的 session id 即表主键，跨重启稳定。
- **规格对齐**（Docker Registry HTTP API v2 公开规范 + docs/reverse/docker-registry.md §2）：
  - `GET /v2/<name>/blobs/uploads/<reference>` → **204 No Content + `Range: 0-<offset-1>`**（上传状态查询，续传前提；该 GET 状态腿**重启前已在且正确**——T-211 实测，v1.0「现缺失」系勘误；M7 修复项 = **跨重启存活**〔会话重建接线〕）；
  - `PATCH` 续块：`Content-Range` 起点 == 已存字节数 → 202 追加并回 `Range`；起点错位 → **416 + `Range` 权威 offset**（空 body，非 404 非 500）；
  - `PUT ?digest=` 收尾不变（digest 校验失败 400、成功 201、终态清行清目录）；
  - 会话过期（ttl 24h）或行不存在 → 404 `BLOB_UPLOAD_UNKNOWN`。
- **干净停机语义（已定案：Q3，ADR-0028 Accepted，T-214②）**：`Engine.Close()` = 停止接受变更 + 排空在途 Append/Commit + **不删除任何未过期会话行与数据文件、不做过期清理**（sweep + TTL 是会话行唯一回收路径；Close 打保留清单 INFO 替代原清册 INFO）——三径 kill -9 / SIGTERM / compose restart 对称可续传（现行为为 Close 全清；T-209 qa O-1 判定在 M6 语境仍正确，M7 起为目标行为变更）。
- **范围**：仅本地 filestore 后端。S3 后端维持现状（multipart 状态由 S3 持有，`ResumeSession` hard 404 契约与 `TestS3ResumeSessionNotSupported` 不变，Q4）。generic/maven/npm/pypi 均为单体 PUT，无续传面（§2.2 裁定）。
- **客户端注记**：docker CLI 自身中断后从头重传（客户端不实现续传），规格级续传以 curl 验证；真实客户端回归以 docker push/pull 全绿为准（D 序列）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-67-AC1 | V15：kill -9 腿——`curl -X POST $BASE/v2/qa-docker/myimg/blobs/uploads/` 202 取 Location → PATCH 512KiB → 202（`Range: 0-524287`）→ `kill -9` → 重启 → `curl $LOCATION` → **204 + `Range: 0-524287`** → PATCH 下一块（`Content-Range: 524288-...`）→ 202 → `PUT $LOCATION?digest=sha256:...` → 201 → `GET /v2/qa-docker/myimg/blobs/<digest>` 200 且 sha256 逐位一致 → PUT manifest 后 `docker pull` 成功 | P1 |
| FR-67-AC2 | V16：干净重启腿——同 V15 流程但以 SIGTERM 停机（`docker compose restart` 同口径）→ 续传同样成立（依赖 Close 语义变更，FR-70/O-1 联动） | P1 |
| FR-67-AC3 | V17：错位腿——重启后 PATCH `Content-Range` 起点错位（如 0- 重放首块）→ **416 + `Range` 当前权威 offset**（空 body）；伪造 digest 收尾 → 400（T-209 语义零回退） | P1 |
| FR-67-AC4 | V18：过期腿——停机后 sqlite 回填 `expires_at` 为过去 → 重启 → GET/PATCH upload URL → 404 `BLOB_UPLOAD_UNKNOWN`；`uploads/` 目录清空（T-209 sweep 语义零回退） | P1 |
| FR-67-AC5 | V19：S3 回归——S3 后端（MinIO）下 docker push/pull 全绿；`TestS3ResumeSessionNotSupported` 维持 hard 404 契约不改 | P1 |
| FR-67-AC6 | V20：M2 D 序列 P0 复跑全绿（docker push/pull/manifest/tag/catalog/Helm OCI 无回归）；S3 后端下同序列全绿 | P1 |

---

### 4.3 Token 铸造二次认证（种子 C，可选增强，P2）

#### FR-68 SSO session 铸管理 Token 的 step-up（dev-go-core：internal/auth + internal/httpapi）

**用户故事**：
- 作为安全负责人，开发者浏览器 session 被盗后，攻击者不能用该 session 无声铸造长效 API Token——铸造需要第二因子（重验口令或 IdP 新鲜重认证）。
- 作为 CI 工程师，我的流水线用 Basic/Token 认证铸 Token（M6 Q11 开放的自助面）完全不受影响——step-up 只作用于 web session 臂。

行为规格：

- **作用域（已定案：Q5，ADR-0027 Accepted，T-214③）**：`auth.token_step_up` 开关（默认 **false**，不破既有脚本，企业部署文档建议开启）开启时——经 **web session cookie 认证的非 admin 用户**（**含本地用户 session 臂**，T-214 扩围：威胁载体是 session 臂本身，与身份源无关）调用 `POST /api/security/token` 铸管理 Token 需 step-up；Basic / Token / Bearer 臂直接放行（首因子新鲜）；admin 臂不受限；匿名 401 挑战照旧。
- **交互形态（已定案：ADR-0027 修订版，腿选择 = `users.provider`）**：
  - 本地 / LDAP 用户：铸造请求携带重验口令（body 参数 `step_up_password`；本地 argon2 校验 / LDAP bind 重验，`users.provider` 选腿）；缺失 → 401 `step_up_required`（OAuth 形错误体）；口令失验 → 401 `step_up_invalid`。
  - OIDC 用户：login init 携 `purpose=step_up`（authorize 强制 `prompt=login`）→ callback 对该 purpose **不建新 session**、换发 **mint grant**（opaque 256-bit 随机串，服务端只存 sha256；绑定 {username, session_id}；**单次消费即删**），铸造请求 body `step_up_grant` 携带；TTL = `auth.token_step_up_grant_ttl_seconds`（默认 300s，域 [60,3600]；台账进程内存，重启丢失 = 重走一次 re-auth，零 schema）；grant 过期 / 失验 / 复用第二次 → 401 `step_up_invalid`。
- **审计**：step-up 路径铸造的 `token.issue` 事件 detail 增 `step_up` 标志与方式（password / oidc_reauth）。
- **不动的面**：Q11 四条护栏（仅限本人 / 有限 TTL / 主体行校验 / 审计）语义零变更；`/v2/token`（docker）不引入 step-up。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-68-AC1 | V21：开关开启 + OIDC session 用户（非 admin）无 grant `POST $BASE/binflow/api/security/token -d 'grant_type=client_credentials'` → 401 `step_up_required`（OAuth 形错误体 `{"error":"step_up_required", ...}`——v1.0 示例 `unauthorized` 系笔误，T-214 P11） | P2 |
| FR-68-AC2 | V22：口令重验腿——本地非 admin 用户 session + 正确 `step_up_password` → 200 铸得 Token → `curl -H "X-JFrog-Art-Api: $T" .../api/system/ping` 200；错误口令 → 401 `step_up_invalid` | P2 |
| FR-68-AC3 | V23：OIDC grant 腿——fresh re-auth（`prompt=login`）后 TTL 窗口内携 grant POST → 200；grant 复用第二次 → 401 `step_up_invalid`；超 TTL → 401 `step_up_invalid` | P2 |
| FR-68-AC4 | V24：豁免腿——开关开启下：Basic 认证（CI）POST token → 200（无需 step-up）；admin session POST → 200；`/v2/token` docker login 流零影响 | P2 |
| FR-68-AC5 | V25：开关默认关闭回归——默认配置下 M6 H26/Q11 四护栏序列复跑全绿（非 admin 本人铸造 / 指定他人 403 / TTL 上限 401 / 禁用即失效） | P2 |
| FR-68-AC6 | V26：审计——step-up 铸造后 `GET /api/v1/audit?action=token.issue` 事件 detail 含 `step_up` 与方式字段 | P2 |

---

### 4.4 条件腿执行（种子 D，P2，dep:用户环境）

#### FR-69 Q8/Q9 真实环境验收（qa-engineer + release-engineer）

**用户故事**：作为从 Artifactory 迁移、制品上 AWS 的企业用户，我想看到 BinFlow 在**真实** AWS S3 与**真实** Artifactory 企业版上的验收记录——MinIO 与 OSS 容器的等价结论我认，但最终拍板要真实环境证据。

行为规格：

- **Q8 真实 AWS S3 实腿**（`dep:用户环境`：用户提供 bucket + 凭据 + region）：S3 后端下 M1~M6 全部 P0 序列复跑（= M6 H69 口径在真实 AWS 上）+ FR-52 吞吐记录（1MB×100 / 1GB×1）；QA 报告注明 bucket/region/时间与序列结论。
- **Q9 真实 Artifactory 迁移实腿**（`dep:用户环境`：用户提供实例，含 ≥1 个 generic local 仓 100+ 制品 + 用户 + token 元数据，及 admin 凭据）：M6 FR-63 全序列（H62~H67 口径：占用守卫 / 制品迁移 sha256 抽样 / 报告 / 用户登录 / token 清点即止 / dry-run）在真实实例上复跑；差异如实记录（Artifactory 版本号注明）。
- **未到位时的等价验收口径（ADR-0025 决策 3 沿用，硬承诺）**：Q8 以 MinIO 本地容器全序列 PASS 为等价；Q9 以 Docker 自建 Artifactory OSS 容器 + 脚本填充为等价。条件腿仅是**补真实环境记录**，不新增功能面、不阻塞 M7 DoD（P2 延后记 BOARD）。
- **触发条件（暂行，Q6 终裁）**：环境到位即插队执行，无需等里程碑收官窗。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-69-AC1 | V27（`dep:用户环境`）：AWS S3 实腿——真实 bucket 上 M1~M6 P0 序列全绿，或差异清单如实归档（每条差异含序列号/现象/定性） | P2 |
| FR-69-AC2 | V28（`dep:用户环境`）：真实 Artifactory 实腿——H62~H67 口径全绿，或差异清单归档（含 Artifactory 版本号） | P2 |
| FR-69-AC3 | V29（等价口径腿，环境无关）：MinIO + Artifactory OSS 容器复跑 M6 H01~H05 汇总断言与 H63/H67——等价基线在 M7 代码上仍绿（M7 改动的回归面）。**v1.2 勘误（T-226 B-3）**：其中 H04 断言按 **T-201 自包含包口径**执行——S3 后端下 export 产出含 blob 的自包含包（import 不依赖原桶；T-226 d-1 实证 35 blob/73MB 随行），M6 PRD H04「S3 export 只导元数据」字面不作为断言依据 | P2 |
| FR-69-AC4 | V30：条件腿执行状态在 BOARD/迭代报告有明确记录（到位→证据链接；未到位→等价口径注记与延后去向） | P2 |

---

### 4.5 技术债清理打包（种子 E，P1，单条 FR）

#### FR-70 M6 收官遗留债收编（dev-go-core + dev-go-storage）

**用户故事**：作为 tech-lead，M7 收官时全仓 lint 归零、窄窗毒化消除、停机语义钉死——下一个里程碑不再背「既有债务」的包袱开工。

行为规格（五项打包，逐项溯源）：

1. **O-1 干净停机会话语义**：`Engine.Close()` 改为保留在册未过期上传会话（行+目录），孤儿由启动 sweep + TTL 清理——FR-67-V16 的前置；语义变更已落 **ADR-0028（Accepted，T-214②）**。T-209 qa O-1 的「异常中断后」措辞边界：**qa 报告为历史记录不回改**（其判定在 M6 语境仍正确），PRD §5.6 与 tech-writer 文档措辞以 ADR-0028 为准（不再写「异常中断后」限定）。防孤儿行回归钉死（干净停机后无孤儿行、无泄漏目录）。
2. **N3 ctx 取消窄窗**：`Append` 在 body 读完 EOF 与 `SetState` 之间 ctx 恰好取消会把完好会话毒化（SetState 用请求 ctx）——改 `context.WithoutCancel`（或等效），消除窄窗；钉死测试（EOF 后注入取消 → 会话不毒化、状态落库）。
3. **N2 restart 臂注释/测试空洞**：`TestV2BlobSessionSweepResidue` 的 restart 臂在 Close 语义变更后需重评——使该臂非空洞（Close 后重新种入过期行+目录再断言 sweep）或注释如实化。
4. **internal/auth 53 条既有 lint 清零**：仅修 lint 不改行为；如有个别条目需行为性处置，逐条记录理由（reviewer 复核）。
5. **sql 行尾**：008/009（及 010 复核）迁移 SQL 文件统一行尾换行，与兄弟文件一致（sqlite/postgres 两方言）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-70-AC1 | V31：全仓 `golangci-lint run ./...` → **0 issues（含 internal/auth）**；`gofmt -l` 空 | P1 |
| FR-70-AC2 | V32：Close 保留腿——在册未过期会话经 SIGTERM 停机后行+目录幸存（sqlite 直查 + 文件在）；无孤儿行/泄漏目录回归钉死；T-209 qa 时间线 4/5（kill -9 幸存 + 过期全清）复验通过 | P1 |
| FR-70-AC3 | V33：N3 钉死测试——Append 全量 EOF 后注入 ctx 取消 → 会话不毒化（后续 PATCH 正常、SetState 已落库）；测试存在且变异验证真实咬住 | P1 |
| FR-70-AC4 | V34：N2——restart 臂非空洞化后 `TestV2BlobSessionSweepResidue` 绿（或注释如实化，reviewer 认可） | P1 |
| FR-70-AC5 | V35：`migrations/{sqlite,postgres}/*.sql` 全部以换行结尾（`for f in ...; tail -c1` 检查脚本零输出）；全仓 `go test -race ./...` exit 0 | P1 |

---

## 5. 兼容性矩阵（M7 核心）

### 5.1 层级定义（沿用 M6 §5.1 五层）

兼容 / 兼容（子集）/ 语义等同但路径不同（/binflow/api/v1）/ 有意不兼容 / BinFlow 自有（无 Artifactory 对应）。

### 5.2 M7 端点/产物矩阵

编号前缀：RB = RBAC 面，DU = docker 上传面，TK = Token 加固面，CD = 条件腿，DB = 技术债。「置信度」：高 = 公开规范或 docs/reverse 双证；中 = PRD 暂行待 ADR/逆向校准。

| # | 端点 / 产物 | 行为要点 | 层级 | 优先级 | 置信度 | 验收 |
|---|---|---|---|---|---|---|
| RB-01 | `PUT\|POST /binflow/api/security/users/{name}` 增可选 `adminRole` | 枚举三值缺省 `user`；与 `admin` 布尔冲突 → 400；仅 admin 可写 | 兼容（超集扩展字段，缺省语义不变） | P0 | 高（自有扩展，Artifactory 用户面仅有 admin 布尔） | V01/V05 |
| RB-02 | `GET /binflow/api/security/users`（列表/单查）回显 `adminRole` | 列表简形态 + 单查回显；永不回显口令 | 兼容（超集字段） | P0 | 高 | V01 |
| RB-03 | readonly_admin 角色（治理读面全通 / 变更面 403 / 数据面全域只读） | 11 读端点通过（`storage/migration` 裸实例按「过门 501」判）；变更面全 403 零副作用（含 GC dry-run）；数据面 r 恒放行、w/d/m 恒拒（角色短路，ADR-0026） | **自有（有意差异）**——Artifactory 无内置只读 admin 角色（近似能力 = permission target 授 read，无管理面只读） | P0 | 高（ADR-0026 Accepted） | V02/V03 |
| RB-04 | permission target 动作集扩展 `manage` | `read\|write\|delete\|manage` 四值；既有 target 零变更 | 兼容（Artifactory `AceInfo` 动作集子集，auth-model.md §4） | P0 | 高 | V07 |
| RB-05 | manage 派生：被授权者可编辑覆盖集内的 permission target | 可管 target 的 repositories ⊆ manage 覆盖 repo 集；不越建仓/用户/token 面；manage 不隐含 r/w/d | 兼容（语义对齐 Artifactory「manage = 可管理该 target」） | P0 | 中（派生细则待逆向校准，K11） | V07~V09 |
| RB-06 | 控制台角色/权限管理扩展（角色下拉 + manage 复选 + 只读态） | 用户页/权限页扩展；错误呈现服务端原文 | 自有 | P1 | — | V12~V14 |
| DU-01 | `GET /v2/<name>/blobs/uploads/<reference>` → 204 + `Range` | 上传状态查询（续传前提）；GET 状态腿重启前已在（T-211 实测勘误），M7 修复项 = **跨重启存活缺失**（会话重建接线，T-216 收窄） | 兼容（Docker Registry HTTP API v2 spec；docs/reverse/docker-registry.md §2） | P1 | 高（公开规范 + T-211 实测） | V15 |
| DU-02 | PATCH 分块续传跨重启（错位 416 + `Range`） | Content-Range 起点校验；重启前后行为一致；过期/无行 404 `BLOB_UPLOAD_UNKNOWN` | 兼容（spec 分块上传语义；现状重启后 404 属缺陷，本条修复） | P1 | 高 | V15/V17/V18 |
| DU-03 | `PUT ?digest=` 收尾（digest 校验/201/终态清理） | T-209 语义零变更 | 兼容（spec；回归项） | P1 | 高 | V15/V17 |
| DU-04 | S3 后端 blob 上传续传 | multipart 状态由 S3 持有；`ResumeSession` hard 404 维持 | **有意不做**（Q4 暂行；ADR-0025 决策 5 勘误留的评估位） | — | — | V19（回归断言） |
| TK-01 | 非 admin web session 铸管理 Token 需 step-up | 开关 `auth.token_step_up`（默认 false）+ `auth.token_step_up_grant_ttl_seconds`（默认 300s）；口令重验 `step_up_password` / OIDC mint grant `step_up_grant`；错误码 `step_up_required`/`step_up_invalid`；Basic/Token/admin 臂豁免 | **自有（有意差异）**——Artifactory 无此要求；开启后对平移脚本有行为差异（401 `step_up_required`），文档标注 | P2 | 高（ADR-0027 Accepted，Q5 已定案） | V21~V26 |
| CD-01/02 | AWS S3 实腿 / 真实 Artifactory 实腿 | 环境验收（非端点）；未到位走 MinIO/OSS 等价口径（ADR-0025 决策 3） | 条件腿（`dep:用户环境`） | P2 | — | V27~V30 |
| DB-01 | 技术债打包（Close 语义 / ctx 窄窗 / N2 / lint / sql 行尾） | 行为变更仅 Close 保留在册会话（落 ADR-0028）；其余零行为变化 | 自有（非兼容面） | P1 | — | V31~V35 |

> 计数：**13 条**。RBAC 面 6 条（RB-01~06：兼容 4〔RB-01/02/04/05〕、自有/有意差异 2〔RB-03/06〕）、docker 上传面 4 条（DU-01~04：兼容 3、有意不做 1）、Token 面 1 条（TK-01 自有/有意差异）、条件腿 2、技术债 1。M7 无「语义等同但路径不同（/api/v1）」新增——RBAC 扩展落在既有兼容面与 `/api/v1` 既有端点之上。

### 5.3 客户端与部署形态分级矩阵（M7 判定标准）

「全过」定义同 M6 §5.3。

| 成员 | 必测面 | 分级 |
|---|---|---|
| curl（RBAC 矩阵） | V01~V11（角色分配/读面/变更面/越权/manage 派生/正交） | **P0 必须全过** |
| curl（docker 续传） | V15~V18（kill -9 / 干净重启 / 错位 / 过期） | **P1 必须全过** |
| docker（回归 + 续传端到端） | V15 尾腿 `docker pull` + V20 D 序列 P0 | **P1 必须全过** |
| mvn / npm（manage 真实客户端腿） | V11 | P1 |
| Playwright（角色 UI / 只读态 / manage 复选） | V12~V14 | P1 |
| curl（step-up，开关双态） | V21~V26 | P2（可选增强全量验证） |
| MinIO + Artifactory OSS 容器（等价口径回归） | V29 | P2 |
| AWS S3 / 真实 Artifactory（`dep:用户环境`） | V27/V28 | P2（条件腿） |

### 5.4 M7 核心验收命令（V 序列骨架，QA 直接引用）

> `$BASE/$ADMIN_PW` 沿用；`$LOC` 为 blob upload Location；OIDC/LDAP/Keycloak 环境沿用 M6 H 序列前置。

```bash
# ========== RBAC（FR-64） ==========
# V01 角色分配（RB-01/02）——v1.2 勘误：建号臂 = PUT create-or-replace（POST /{name} 为部分更新臂，缺用户 404；JSON-only，form 400）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/users/alice -H 'Content-Type: application/json' \
  -d '{"email":"alice@t.io","password":"pw123","adminRole":"readonly_admin"}' -o /dev/null -w '%{http_code}\n'  # 201
#   注：PUT users 为 replace 语义——只翻 adminRole/enabled 也须带 email+password（T-224 非缺陷②，影响 enabled 翻转姿势）
curl -su admin:$ADMIN_PW $BASE/binflow/api/security/users/alice | jq -r '.adminRole'   # "readonly_admin"
# V02 读面全通（11 端点；storage/migration 裸实例按「过门 501」判，RB-03——T-214 P2 勘误：删 v1/stats 与 token 列表、补 replications）
for p in api/repositories api/repositories/generic-local api/v1/health \
         api/security/users api/security/groups api/v1/permissions api/v1/audit \
         api/v1/replication/status api/v1/replications api/v1/storage/stats api/v1/storage/migration; do
  curl -su alice:pw123 $BASE/binflow/$p -o /dev/null -w "%{http_code} $p\n"; done        # 其余 10 项 200；migration 200（已装配双写）或 501（裸实例，过门即 PASS）
# V03 变更面全拒 + 零副作用（RB-03）——v1.2 勘误：建仓臂 = PUT /{key}（无 POST 建仓路由，E-26 口径）、
#   权限写臂 = POST create-or-replace（无 PUT /{name} 路由）；v1.0/v1.1 骨架两条路由均 404（T-221 实证）
curl -su alice:pw123 -X PUT $BASE/binflow/api/repositories/alice-repo -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"generic"}' -o /dev/null -w '%{http_code}\n'      # 403
#   …用户/组/权限（POST /api/v1/permissions）/token revoke/GC 各一腿同法；写后 GET 实体逐字未变
# V04 即时生效：alice 同一 Token，升角色前 403 → 后 200 → 降回 403
# V05 越权/冲突：alice 改 bob → 403；admin=false+adminRole=admin → 400
# V06 审计：curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?action=user.role.change"

# ========== manage 派生（FR-65） ==========
# V07 carol（app-admins 组）编辑 t-app 加 dave:[read] → dave GET 制品 200（RB-04/05）
#   v1.2 勘误：无 PUT /{name} 路由——编辑臂 = POST create-or-replace（201）；wire 键为 repos/includePatterns（数组）
curl -su carol:pw123 -X POST $BASE/binflow/api/v1/permissions -H 'Content-Type: application/json' \
  -d '{"name":"t-app","repos":["app-local"],"includePatterns":["**"],"principals":{"groups":{"app-admins":["read","write","delete","manage"]},"users":{"dave":["read"]}}}' \
  -o /dev/null -w '%{http_code}\n'                                                       # 201
# V08 边界：carol PUT repositories/new-repo（建仓臂，router 无 POST 建仓路由）→ 403；POST permissions 同名 t-other（超覆盖集）→ 403；users → 403；revoke → 403
# V09 正交：仅 manage 的 carol2：GET repositories/app-local 200（全局列表 403）/ POST permissions（⊆覆盖集）201 / PUT 制品 403 / GET 制品 403
# V10 零回归：M1 C22/C27 + M4 W19/W19c/W21 复跑全绿
# V11 真实客户端：docker push $BASE/app-local/myimg:t && docker pull …；mvn deploy；npm publish

# ========== 续传（FR-67） ==========
# V15 kill -9 腿（DU-01/02/03）
LOC=$(curl -su admin:$ADMIN_PW -X POST $BASE/v2/qa-docker/myimg/blobs/uploads/ -o /dev/null -D - | awk -F': ' 'tolower($1)=="location"{sub(/\r/,"",$2);print $2}')
head -c 524288 /dev/urandom > /tmp/chunk1; head -c 65536 /dev/urandom > /tmp/chunk2
curl -su admin:$ADMIN_PW -X PATCH -H 'Content-Type: application/octet-stream' --data-binary @/tmp/chunk1 $BASE$LOC \
  -o /dev/null -D - | grep -i '^range:'                              # Range: 0-524287
kill -9 <server_pid> && <restart>                                     # 崩溃重启
curl -su admin:$ADMIN_PW $BASE$LOC -o /dev/null -D - | grep -E '%{http_code} ' # 用 -w 取 204；头含 Range: 0-524287
DIG=sha256:$(cat /tmp/chunk1 /tmp/chunk2 | openssl dgst -sha256 -r | cut -d' ' -f1)
curl -su admin:$ADMIN_PW -X PATCH -H 'Content-Type: application/octet-stream' \
  -H "Content-Range: 524288-$((524288+65536-1))" --data-binary @/tmp/chunk2 $BASE$LOC -o /dev/null -w '%{http_code}\n'  # 202
curl -su admin:$ADMIN_PW -X PUT "$BASE$LOC?digest=$DIG" -o /dev/null -w '%{http_code}\n'   # 201
docker pull $BASE/qa-docker/myimg:<tag>                               # 端到端
# V16 干净重启腿：kill -9 换 SIGTERM / docker compose restart，同法
# V17 错位腿：PATCH Content-Range: 0-524287 重放 → 416 + Range 权威 offset；伪造 digest PUT → 400
# V18 过期腿：sqlite 回填 expires_at 过去 → 重启 → GET/PATCH 404 BLOB_UPLOAD_UNKNOWN
# V19 S3 回归 + TestS3ResumeSessionNotSupported 不变
# V20 M2 D 序列 P0 复跑（本地 + S3 双份）

# ========== step-up（FR-68，开关开启腿） ==========
# V21 OIDC session 无 grant → 401 step_up_required（OAuth 形错误体）
# V22 本地非 admin session + step_up_password 正确 → 200 → X-JFrog-Art-Api 可用；错误口令 → 401
# V23 OIDC prompt=login 取 mint grant → 窗口内 200；复用/过期 → 401
# V24 豁免：Basic POST token → 200；admin session → 200；/v2/token 流零影响
# V25 默认关闭回归：M6 H26/Q11 四护栏复跑绿
# V26 审计：token.issue detail 含 step_up

# ========== 条件腿（FR-69）与技术债（FR-70） ==========
# V27/V28 dep:用户环境（AWS S3 / 真实 Artifactory 实腿，或差异清单归档）
# V29 等价口径：MinIO + Artifactory OSS 容器复跑 M6 H01~H05 汇总 + H63/H67（H04 按 T-201 自包含包口径断言——v1.2 勘误）
# V31 golangci-lint run ./... → 0 issues；gofmt -l → 空
# V32/V33/V34/V35 见 FR-70 AC（sqlite 直查 / 钉死测试 / sql 行尾脚本 / -race 全绿）
```

### 5.5 待校准项（M7 落地后回写）

| # | 项 | v1.0 暂行值 | 校准来源 |
|---|---|---|---|
| K10 | 角色承载形态（`users.role` 列 vs 独立表）、`is_admin` 兼容视图维护方式、migration 011 细则 | **已定案（ADR-0026 Accepted）**：`users.role` 列 + `is_admin` 兼容镜像（M8 移除）+ `permission_principals.can_manage` 列；wire=`adminRole` / DB=`role`，handler 一处映射 | ADR-0026 决策 6（2026-08-23 定稿） |
| K11 | manage 派生能力闭集（可管 target 的 repo 覆盖判定；`?permissions` 可见范围；是否含配额读） | **已定案（ADR-0026 决策 3）**：覆盖集判定 = ⊆；含单仓配置读、target CRUD、`?permissions` 视图与配额/用量读（usage/{repo} 归仓库域）——v1.0「不含配额」被推翻（T-214 P9）；全局列表不开放（M8+ 过滤列表，architecture §11.30） | ADR-0026（Accepted）+ docs/reverse/rbac-model.md |
| K12 | step-up 契约（参数/头名、mint grant 形态与 TTL、开关键名与默认值、错误码文案） | **已定案（ADR-0027 Accepted 修订版）**：`step_up_password`/`step_up_grant`；grant 绑定 user+session、单次消费即删；`auth.token_step_up`（默认 false）+ `auth.token_step_up_grant_ttl_seconds`（默认 300s，域 [60,3600]）；错误码 `step_up_required`/`step_up_invalid` | ADR-0027 决策 3~6（Q5 已定案） |
| K13 | docker 续传注册表重建策略（启动全量 vs 惰性查询）与 Location 基址稳定性 | T-216 已收窄：**lazy 惰性重建** + `Offset()` 权威化（T-211 实测：GET 状态腿重启前已在）；Location 复用 M2 既有生成 | T-216 实现票（进行中口径） |

### 5.6 回归基线反转表（M7 起生效，qa 更新既有断言）

| 既有断言 | 来源 | M7 起的期望 |
|---|---|---|
| docker upload URL 重启后 404 `BLOB_UPLOAD_UNKNOWN` | T-209 qa O-2（现状） | 未过期会话 GET → 204+`Range`、可续传（DU-01/02）；仅过期/无行 404 |
| 干净停机（SIGTERM）清空在册上传会话 | T-209 qa O-1（判定为既有语义） | 保留未过期会话至 TTL（ADR-0028；V16/V32） |
| 用户权限为 admin 布尔二元 | M1 FR-5 / M4 FR-27 | role 闭集三值（`admin` 布尔保留为兼容视图，语义不变） |
| permission target 动作集 `read\|write\|delete` | M1/M4 | 增 `manage`（无 manage 位行为不变） |
| session 臂非 admin 铸 Token 直接 200 | M6 Q11 裁决 | 开关开启时 401 `step_up_required`/`step_up_invalid`（默认关闭不反转；Q5 已定案默认 false，ADR-0027） |
| internal/auth 53 条既有 lint 为已知债务 | T-208/T-209 qa 注记 | 0 issues（V31） |
| `GET /api/security/token`（token 列表） | M1 E-17（有意不存在；T-211 实测 404 佐证） | **勘误（非反转）**：端点从未存在，v1.0 readonly 读面清单误列已删（T-214 P2/P13） |

---

## 6. 非功能需求（NFR）与已有 ADR 冲突/补充

### 6.1 与已有 ADR 的冲突/补充标注

| ADR | 冲突/补充点 | 本 PRD 立场 | 所需 ADR |
|---|---|---|---|
| ADR-0006 决策 2（会话入 `upload_sessions` 表，T-209 修订版） | Close 现行清空在册会话（T-209 qa O-1 判非回归）与续传目标冲突 | 修订：Close 保留未过期会话，孤儿由 sweep+TTL 清理 | **ADR-0028 已定案**（Accepted，2026-08-23，T-214②；ADR-0006 追加勘误④） |
| ADR-0025 决策 1（replica 隔离归 M7+ RBAC/复制里程碑） | M7 是否收编 | 暂行不收编（Q7 请用户终裁）；M7 交付 RBAC 使能位 | Q7 处置 |
| ADR-0025 决策 5 勘误（S3 multipart 续传「另行评估 M7」） | M7 是否纳入 S3 续传 | 暂行不纳入（Q4）；hard 404 契约维持 | Q4 处置 |
| ADR-0014（console/session） | step-up 的 mint grant 是否动 session 模型 | 不动：grant 为短时独立凭据（进程内台账），session 机制零变更 | ADR-0027 已定案（Accepted；`purpose=step_up` callback 不建新 session） |
| ADR-0005（零 CGO 依赖基线） | M7 无新第三方依赖（RBAC/续传/step-up 全部内生实现） | 补充：go.mod 零新增（如实现中发现必须引入，走 architect 白名单流程） | — |
| M4 FR-27-AC9（组不带 admin 位，有意不兼容断言） | RBAC 是否给 groups 引入角色 | 不引入：角色仅在 user 行，断言维持并复跑 | — |

### 6.2 性能（M7 增量）

| NFR | 指标与验收方式 | 优先级 |
|---|---|---|
| NFR-P33 授权判定性能 | role/manage 判定并入 `Authorizer.Can` 后，1000 并发拉取（G27 口径）零错误且 P95 与基线偏差 < 10%（qa 复测记录）。**v1.2 勘误注记（T-222 O-2）**：M6 G27 基线（T-172）未归档 P95——分母不存在，v1.0/v1.1 的「与 M6 基线偏差 <10%」字面不可计算；T-222 判达标口径 = 零错误 + 同口径吞吐两臂均优于基线（匿名 +86% / 认证 +34%），并**归档 M7 新 P95 基线（匿名 6.8ms / 认证 2748ms）**供后续验收用作分母（此后性能基线归档 P50/P95/P99） | P0 |
| NFR-P34 续传恢复开销 | 重启后首条可服务请求 < 2s（冷启动门内含会话注册表重建）；1000 在途会话重建内存增量 < 10MB（注册表仅 id+offset） | P1 |
| NFR-P35 迁移与备份 | migration 011 在既有库（100 用户量级）执行 < 1s；export/import 往返含 `role` 列与 manage 位保真 | P1 |

### 6.3 安全底线（M7 增量）

| NFR | 要求 | 验收 |
|---|---|---|
| NFR-S40 提权矩阵 | read-only admin / manage 持有者对全部变更面 403 且零副作用；qa 负面矩阵全量跑（V03/V08 + 补充腿） | V03/V08 |
| NFR-S41 step-up 不可绕过 | 无 grant/错误口令 → 401；mint grant 单次有效 + TTL 域 **[60,3600]**（默认 300s=5min——ADR-0027 决策 4 为准；v1.1「TTL ≤5min」措辞系勘误，T-224 实测 3600 可启动可服务、5 拒启动）；开启后 Basic/Token 臂零影响 | V21~V24 |
| NFR-S42 角色最小权限 | 仅 admin 可写角色字段；角色变更全审计（`user.role.change`）；迁移 011 回填不产生意外提权（回填后全量用户 role 复核） | V05/V06 |
| NFR-S43 权限语义零回归 | M1 C22/C27、M4 W19~W21、M2 D 序列、M6 H26/Q11 护栏复跑零回退 | V10/V20/V25 |

### 6.4 可观测性（M7 增量）

- **审计新增**：`user.role.change`（actor/target/old/new）；`token.issue` detail 增 `step_up` 标志（仅 FR-68 开启时）；manage 派生的 permission target 变更沿用既有权限变更审计面（actor 即操作者，可归因）。
- **结构化日志**：无新增必需字段；续传命中日志（`upload resumed`）用既有 INFO/WARN 风格，含 session id 与 offset。
- **/metrics**：M7 不新增必需族；若实现中增管理面指标，role 标签限闭集（基数 3），FR-61 的 M7+ 增补项（`storage_logical_bytes`、`auth_sessions_active` 等）继续冻结。

---

## 7. 开放问题（Q1~Q7 状态：Q1/Q2/Q3/Q5 已定案，Q6 进入执行态，Q4/Q7 仍开放）

| # | 问题 | 影响面 | 状态与口径（v1.1 按 T-214/ADR-0026~0028 回写；v1.2 更新 Q6 执行态；「推翻出口」= 推翻已裁项须新 ADR 或用户显式要求） |
|---|---|---|---|
| Q1 | **RBAC 角色闭集与承载形态**：闭集取三值 + manage 派生，还是引入独立「repo-admin」第四角色？`users.role` 列 vs 独立角色表？兼容面 `admin` 布尔与 `adminRole` 的映射细则？ | FR-64/FR-65；migration 011；兼容面 RB-01/02 | **已定案（ADR-0026 Accepted，T-214）**：三值闭集 `{user, readonly_admin, admin}`（snake）+ `manage` 派生，无第四角色；`users.role` 列 + `is_admin` 兼容镜像（`admin` 布尔 ⇔ `role=admin`）；wire 字段 = `adminRole`（camelCase）/ DB 列 = `role`，handler 一处映射。推翻出口：新增角色/自定义角色 = 架构变更，须新 ADR（独立角色实体归 M8+） |
| Q2 | **read-only admin 的边界清单**：读端点清单与 GC dry-run（只读报告）是否开放？ | FR-64-AC2/V02；routeAuth 全部 admin-only 读面的授权口径 | **已定案（T-214① + ADR-0026 Accepted）**：读面 = 11 端点（勘误：`/api/v1/stats` 并入 `/api/v1/storage/stats`、token 列表端点不存在〔M1 E-17〕删除、补 `/api/v1/replications`；`storage/migration` 裸实例按「过门 501」判）；GC **全路由 `system:write`，readonly_admin 403 含 dry-run**（T-214 否决 v1.0 暂行「dry-run 开放」——readonly 角色不 POST 写路由；如需开放 M8+ 拆 GET 只读路由）；数据面全域只读（角色短路）。推翻出口：若用户显式要求 readonly_admin 可与 target 组合，须新 ADR 推翻（ADR-0026 已留此口径） |
| Q3 | **干净停机的会话语义**：Close 保留在册未过期会话（干净重启也可续传，孤儿靠 TTL sweep）vs 维持现状清空（续传仅限 kill -9/断电等异常中断，文档限定措辞）？ | FR-67-V16；FR-70/O-1；ADR-0028；运维心智（compose restart 后大上传存活 vs 停机后无孤儿） | **已定案（ADR-0028 Accepted，T-214②，选保留）**：Close 保留未过期会话（行+目录），孤儿回收唯一路径 = 启动 sweep + TTL；三径 kill -9/SIGTERM/compose restart 对称可续传；FR-67-V16/V32 照 PRD 执行；tech-writer 措辞不再限「异常中断后」（T-209 qa O-1 判定在 M6 语境仍正确、报告不回改）。推翻出口：若用户选维持现状，须新 ADR 推翻，且 FR-67-V16 与 §5.6 对应行改回「异常中断后」口径 |
| Q4 | **S3 后端续传是否纳入 M7**：multipart 状态由 S3 持有（upload ID 跨重启有效），BinFlow 可建 DB 影子表把 upload ID 接回 HTTP 层 | FR-67 范围；DU-04；S3 大对象中断重传成本 | **仍开放（暂行不纳入，architect 无异议）**：维持 hard 404 契约（`TestS3ResumeSessionNotSupported`）；前置条件（upload ID 落表 + S3 ResumeSession〔ListParts 重建〕）已登记 architecture §11.31，M8+ 新 ADR 评估。推翻出口：用户如要求纳入，FR-67 拆双后置票 |
| Q5 | **step-up 交互形态与默认值**：口令重验 + OIDC re-auth 双轨 vs 仅 OIDC 臂强制 vs 仅确认对话框（弱）；作用域（全部非 admin session vs 仅 SSO 臂）；`auth.token_step_up` 默认 on/off | FR-68；TK-01；既有 CI/脚本兼容（默认 on 会改变现状行为） | **已定案（ADR-0027 Accepted 修订版，T-214③）**：双轨（本地/LDAP `step_up_password` + OIDC `prompt=login` mint grant `step_up_grant`）；作用域 = **全部非 admin web session 臂（含本地用户，T-214 扩围）**；错误码 `step_up_required`/`step_up_invalid`；config = `auth.token_step_up`（**默认 off**，企业部署文档建议开启）+ `auth.token_step_up_grant_ttl_seconds`（默认 300s）；确认对话框与 id_token 新鲜窗口形态不采用（A' 否决）。推翻出口：M8+ 若落自有签发端点，ADR-0027 同缝适用防旁路 |
| Q6 | **条件腿触发条件与截止**：用户环境（AWS bucket / Artifactory 实例）到位的判定方式与执行窗口——M7 收官前必须？到位即插队？长期挂账？ | FR-69；M7 DoD 判定；qa 排期 | **执行态更新（v1.2，2026-08-23）——不再纯挂账**：T-228（Q9 真实 Artifactory 实腿/V28）`dep:用户环境` **已解除**——用户 VM + 真实 Artifactory OSS 7.84.10 栈（T-226 搭建并保留）**已在跑**，产出按 real-env-appendix 模板归档；T-227（Q8 真实 AWS S3 实腿/V27）仍 `dep:用户环境`（真实 AWS）。终裁口径维持暂行：到位即插队执行（不等收官窗）；M7 DoD 不被条件腿阻塞（P2 延后记 BOARD，沿用 ADR-0025 决策 3 与 M5 Q3 降级口径）；环境长期未到位则随 M8 PRD 重申。推翻出口：用户如指定收官前硬截止，改 FR-69 行为规格并重排 qa 窗口 |
| Q7 | **replica backing 仓直写隔离是否并入 M7**：ADR-0025 决策 1 将其指派「M7+ RBAC/复制里程碑」——M7 交付 RBAC 后已具备表达位（如 manage/角色 + repo 级 write-block 开关），是否顺势收编 | FR-65 范围；复制目标端数据一致性；票量 | **仍开放（暂行不并入，conductor 种子未列，避免无票面来源的扩项）**：M7 只交付 RBAC 本体与使能位（m 动作 + role，ADR-0026 已交付下放基座）；隔离本体（repo 级 write-block 配置或复制引擎标记）归 M8+ 复制里程碑。现状维持 ADR-0025 决策 1 的「已接受限制」。推翻出口：用户如要求并入，须 conductor 录票并扩 FR-65 范围 |

---

## 8. M7 验收剧本（QA 总纲）

1. **回归基线**：M1~M6 全部 P0 序列在本地 filestore 下复跑全绿（硬门槛）；S3（MinIO）后端下 D/H 序列复跑。
2. **RBAC 模型**：V01（分配）→ V02/V03（读面/变更面矩阵）→ V04（即时生效）→ V05/V06（越权/冲突/审计）。
3. **manage 派生**：V07（授权链）→ V08（边界）→ V09（正交）→ V10（零回归）→ V11（真实客户端）。
4. **控制台**：V12~V14（角色下拉/只读态/manage 复选，Playwright）。
5. **续传**：V15（kill -9 全链 + docker pull）→ V16（干净重启）→ V17（错位 416）→ V18（过期 404）→ V19（S3 契约）→ V20（D 序列回归）。
6. **step-up**：开关开启矩阵 V21~V24 + V26（审计）→ 开关默认关闭回归 V25。
7. **条件腿**：V29（等价口径回归，必跑）→ V27/V28（`dep:用户环境`，到位即跑）→ V30（状态归档）。
8. **技术债**：V31（lint/gofmt）→ V32（Close 保留 + 防孤儿）→ V33（N3 钉死）→ V34（N2）→ V35（sql 行尾 + -race）。
9. **文档**：tech-writer 产出 RBAC 指南（角色/manage/示例）、上传续传说明（措辞按 ADR-0028，不再限「异常中断后」）、step-up 指南（开关/双轨交互/CI 影响/SSO 用户 CLI 铸 Token 路径）、S3 与迁移真实环境附录（条件腿记录位）。

---

## 9. M7 DoD

1. §4 全部 P0 AC（FR-64/FR-65）经 qa 验证全绿；P1（FR-66/FR-67/FR-70）全绿；P2（FR-68/FR-69）中 step-up 双态验证通过、条件腿按 Q6 口径处置（未到位 → 等价口径归档 + BOARD 记录，不阻塞）；
2. §8 剧本全绿；§5.3 分级矩阵 curl/docker/mvn/npm/Playwright 各 P0/P1 面全过；
3. M1~M6 全部 P0 序列本地 filestore 复跑全绿（回归硬门槛）；S3（MinIO）下 D 序列与 H01~H05 汇总断言全绿；
4. tech-writer 文档 4 类齐备（RBAC 指南 / 续传说明 / step-up 指南 / 真实环境附录）；
5. architect ADR-0026（角色模型与 manage 派生）、ADR-0027（step-up 契约）、ADR-0028（会话 Close 语义修订）——已定案（均 Accepted，2026-08-23，T-214）；
6. 全仓 golangci-lint 0 issues（含 internal/auth 53 条清零）、全仓 `-race` 绿、gofmt 空；
7. 主会话完成 `m7-done` tag（push 须用户单独授权）。

---

*本 PRD v1.1 由 product-manager 依据 PRODUCT.md、ROADMAP.md、docs/prd/milestone-6.md（v1.3）§7 Q4/Q8/Q9/Q11 裁决与遗留债、DECISIONS.md（ADR-0006/0025 等）、reports/iteration-386.md、reports/agents/T-209-review2.md（N1~N7）、reports/agents/T-209-qa.md（O-1/O-2）、docs/reverse/auth-model.md §4、docs/reverse/docker-registry.md §2、M4 交付基线（FR-27/FR-28）撰写 v1.0，并按 T-214 裁决（ADR-0026/0027/0028 Accepted 终版 + T-211 实测基线，conductor 终审通过）回写 v1.1；v1.2 为 M7 执行期勘误回写（conductor 终审通过，依据 T-215/T-217 review-a·c、T-221/T-222/T-224/T-226 各 review/qa 报告：V 序列骨架路由与 wire 拼写、token 措辞限定、NFR-S41 TTL 域、NFR-P33 基线分母注记、PUT users replace 语义、H04 自包含包口径、Q6 执行态）；§7 Q1/Q2/Q3/Q5 已定案，Q4/Q7 待用户终裁，Q6 执行中（T-228 在跑、T-227 待环境；推翻出口保留）。与既有 ADR 冲突/补充点见 §6.1，文本冲突以 ADR 为准。*
