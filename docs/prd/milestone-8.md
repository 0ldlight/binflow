# PRD — M8 控制台对齐 Artifactory（IA 重构 / 制品浏览器 / 管理面形态 / 皮肤与 token / 债务收编）

> **PRD 状态：v1.0 草案（待 conductor 审）**。主轴来自用户指令（2026-08-23 原话）：「前端 UI 和交互逻辑要求和 JFrog 一样」。conductor 执行口径（BOARD 状态区，可推翻）：对齐 = **信息架构 + 交互逻辑 + 操作流**（Artifactory 用户零学习成本），视觉近似但**自有皮肤**；clean-room 铁律（ADR-0001）对 UI 生效——产出只能是行为规格，JFrog 图标/样式/代码零复制；**服务端契约零改动**（M7 的 RBAC/manage/step-up/续传语义全保留）；范围 = BinFlow 已有功能面。

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/milestone-8.md` |
| 里程碑 | M8 — 控制台对齐 Artifactory（对应 ROADMAP.md「M8」条目；用户指令「前端 UI 和交互逻辑要求和 JFrog 一样」+ M7 完结节 M8 债券） |
| 状态 | **v1.0 草案，待 conductor 审**（FR-71~FR-77 七条需求，UI 兼容矩阵 24 条，U01~U24 验收命令骨架；开放问题 Q1~Q6 带暂行） |
| 上游依据 | PRODUCT.md（核心能力 5「Web 控制台」/Non-goals）、用户指令 2026-08-23、BOARD.md M8 指令节（conductor 口径全文）、docs/prd/milestone-7.md（§9 DoD 体例 + M8 债券源）、docs/prd/milestone-4.md（FR-23~FR-26 控制台基线 + W 序列）、docs/design/console-ux.md v1.3（现役 IA/路由表/242 testid 锚）、docs/design/architecture.md §7（/binflow 挂载与 session）、docs/reverse/（REST/存储/权限行为规格——UI 行为规格待建）、T-228 保留栈（真实 Artifactory OSS 7.84.10，UI 行为参照实例）、DECISIONS.md ADR-0001（clean-room）/ ADR-0014（console/session） |
| 下游消费者 | tech-lead（拆票）、architect（前端重排 ADR 候选：路由迁移/主题 token/组件结构）、reverse-engineer（docs/reverse/ui-console.md UI 行为规格）、ux-designer（console-ux v2.0：IA/组件清单/状态矩阵/设计 token）、web 前端 dev（AppShell 与页面组票）、qa-engineer（U 序列 + W 序列锚迁移回归）、tech-writer（控制台指南改版） |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-23 | 初版草案（待 conductor 审）：M8 范围（用户指令 + conductor 五条口径 + M8 债券七项全覆盖）、FR-71~FR-77（IA 双模式壳 / 制品浏览器 / 管理面表格与编辑器 / 导航与面包屑 / 键盘与批量 / 自有皮肤与 token / 债务包打包）、UI 兼容矩阵 24 条（对齐 8 / 形态不同 3 / 子集 8 / 有意差异 5）、U01~U24 验收命令骨架（Playwright 交互断言为主，明确非像素对比判定口径）、开放问题 Q1~Q6 带暂行 |

---

## 1. 背景与目标

### 1.1 背景

M1~M7 交付了完整的制品仓库能力面（五协议、remote/virtual、RBAC、复制、S3、续传、step-up），但控制台是 M4 时期按「BinFlow 自有 IA」设计的：单模式左侧导航（仪表盘/仓库/搜索/安全/治理/设置）、制品树藏在仓库详情页内、管理面表单自有形态。与 Artifactory 的差异集中在三层：

1. **信息架构**：Artifactory 是 Application / Administration 双模式（普通用户工作区 vs 管理员配置区），制品浏览器以左树为主角、选中即右侧详情；BinFlow 是单模式扁平导航，管理动作与浏览动作混在同一棵树里。
2. **交互逻辑**：Artifactory 的列表页（工具栏过滤 + New）、建仓分组表单、Set-Me-Up 接入命令对话框、删除时输入 repo key 确认等「肌肉记忆级」交互，BinFlow 各有等价能力但形态各异。
3. **操作流**：同一任务（建仓→拿接入命令→授权→查审计）的步骤数与路径组织不同，Artifactory 老用户需要重新学习。

用户指令明确要求对齐。**这是承载层重排，不是后端重写**：全部既有 REST/协议端点、M7 的 RBAC/manage/step-up/续传语义零改动。

同时收编 M7 完结节登记的 M8 债券（七项，见 FR-77）：T-231 上传路径 percent-encode 缺陷、B-1 bf-migrate --skip-users 降级、UI 打磨 4 条、CI -timeout 20m、附录 V28 证据移植 docs、ROADMAP M7 勾账同步（本 PRD 随稿完成）、dialer 样板 13 处。

### 1.2 M8 目标

> 一句话：把控制台重排为 Artifactory 的信息架构、交互逻辑与操作流（Application/Administration 双模式 + 制品浏览器左树 + 管理面标准形态），穿 BinFlow 自有皮肤，服务端契约一行不改——Artifactory 用户不查文档就能完成全部日常任务。

量化门槛（未达即里程碑不完成）：

| 指标 | M8 门槛 | 来源 |
|---|---|---|
| 对齐覆盖面 | 现役全部页面/路由（console-ux §3.2 路由表 20 条 + AppShell 导航）在新 IA 中有唯一归属，且逐条给出 Artifactory 对应物映射（对齐/子集/形态不同/自有），映射表在 docs/reverse/ui-console.md 冻结 | FR-71~FR-73 |
| 零学习成本剧本 | ≥ 8 个「Artifactory 用户无文档完成」剧本全 PASS（§8：建仓 / Set-Me-Up 拿命令 / 浏览制品到下载 / 上传 / 给组授权 / 设 readonly_admin / 查审计 / 跑 GC dry-run） | §8 U 序列 |
| 服务端契约零改动 | M1~M7 全部 P0 序列（C/D/H/W/V 等）复跑全绿；唯一允许的行为变化 = FR-77 修复项本身（T-231 / B-1） | 回归硬门槛 |
| Playwright | M4 W 序列 + console-ux §10 的 242 testid 锚零回归（允许带迁移映射表）；新增 IA/交互断言 ≥ 40 条 | FR-71~FR-75 |
| 体积与性能 | console 前端产物 gzip ≤ 350KB；二进制 < 40MB 维持；本地登录到首屏可交互 ≤ 1.5s | NFR-P36 |
| 债券 | FR-77 六项（勾账项随本 PRD 完成）全部收口 | FR-77 |

### 1.3 上游依赖与并行关系

- **逆向参考（reverse-engineer，前置产物）**：新建 `docs/reverse/ui-console.md`——Artifactory UI 行为规格（布局分区描述、交互流步骤、组件清单、状态矩阵），**不含任何 JFrog 资产本体**。参照源：T-228 保留的真实 Artifactory OSS 7.84.10 实例（行为可实证）+ JFrog 公开文档；reverse-src 前端资源只读参考、零复制。置信度中以下的矩阵行以「待 ui-console.md 校准」标注。
- **架构依赖（architect）**：前端重排 ADR 候选（ADR-0029+）：SPA 路由迁移与旧路由 redirect 策略、主题/设计 token 体系、组件结构（是否引框架不预设，NFR-P36 为硬门）。本 PRD 只约束可观察行为，与 ADR 冲突时以 ADR 为准。
- **UX 规范（ux-designer）**：console-ux.md 升 v2.0（IA 重排 + 组件清单 + 状态矩阵 + token 修订），消费本 PRD §4/§5。
- **实现分区**：AppShell/路由/模式切换（FR-71）先行；制品浏览器（FR-72）与管理面（FR-73）area 不重叠可并行；皮肤 token（FR-76）横切，需在 FR-71 落地前定 token 骨架；FR-77 债务包与前端无 area 冲突，随时可插队。
- **QA 并行面**：U 序列以 Playwright 为主 + FR-77 的 curl/CLI 腿；W 序列锚迁移回归在 FR-71 合入后跑首轮。

---

## 2. 范围

### 2.1 In scope

| # | 来源 | 本 PRD 功能需求 | 优先级 |
|---|---|---|---|
| A | 用户指令（IA 对齐） | FR-71（Application/Administration 双模式壳与导航树重排，含 URL 深链与旧路由迁移） | P0 |
| B | 用户指令（浏览体验） | FR-72（制品浏览器：左树右详情、rclass 分组、树内过滤、制品详情面板） | P0 |
| C | 用户指令（管理面形态） | FR-73（管理面统一表格与编辑器形态 + Set-Me-Up 式接入命令对话框） | P0 |
| D | 用户指令（导航交互） | FR-74（面包屑/全局搜索/深链状态保持） | P1 |
| E | 用户指令（效率交互） | FR-75（键盘可达矩阵 + 表格多选批量动作） | P1 |
| F | conductor 口径（自有皮肤） | FR-76（设计 token 与自有皮肤，零复制合规） | P1 |
| G | M8 债券（七项） | FR-77（T-231 percent-encode + B-1 --skip-users + UI 打磨 4 条 + CI timeout + V28 附录移植 + dialer 样板；勾账项随本 PRD 完成） | P1 |

前置产物（非 FR，拆票依赖）：docs/reverse/ui-console.md（reverse-engineer）、前端重排 ADR（architect）、console-ux v2.0（ux-designer）。

### 2.2 Non-goals — M8 明确不做

**产品级（继承 PRODUCT.md，不变）**：不做 HA、不做 Xray 式扫描、不做 LDAP/SAML（OIDC/LDAP 认证能力为 M6 已有，登录页入口形态对齐即可）、不做 UI 高级洞察报表、不做 Artifactory 全量 REST 兼容。

**M8 里程碑级 Non-goals**：

| 不做项 | 隔离边界 |
|---|---|
| 任何服务端行为/契约变更 | 本里程碑后端零票（FR-77 修复项除外）；新端点、字段、语义变化一律拒绝——UI 只重组既有能力的呈现 |
| Xray / Builds / Release Bundles / Pipelines / Insight 等 JFrog 独立产品的 UI | 导航树**不出现**这些节点（无对应功能面，出占位节点 = 欺骗性 IA） |
| 像素级复刻 / JFrog 品牌资产 | 图标、配色值、字体、logo、CSS 零复制（clean-room）；验收禁止像素 diff（判定口径见 §5.4 头注） |
| Access Tokens 管理页 | 现状占位维持（M4 R6 P2 未排；conductor 范围清单未列）——仅对齐其占位态的呈现位置 |
| 国际化 / 多语言 | UI 文案中文 + 英文术语保留（console-ux §1.2 约定不变），不做 i18n 框架 |
| 移动端 / 触屏优化 | 管理面桌面优先，不投入响应式移动布局 |
| 前端单元测试框架选型 / 组件库选型 | architect ADR 领地，本 PRD 只给行为与体积门（Q5） |

---

## 3. 用户与场景（M8 视角）

- **场景 A（Artifactory 老管理员迁移）**：团队从 Artifactory 平迁到 BinFlow。管理员的肌肉记忆是「右上角切 Administration → 左树 Security → Users → 点用户改角色」——他在 BinFlow 里按同样的路径走，能一步不错地完成。
- **场景 B（开发者拿接入命令）**：开发者第一次用 BinFlow，在 Application 模式树里选中仓库，点 Set Me Up，拷走 docker login / .npmrc 命令回终端——不需要看文档。
- **场景 C（内审员巡视）**：readonly_admin 账号登录，Application 树按 ACL 收窄、Administration 只读可看不可改（M7 语义零改动，呈现按新 IA 重排）。
- **场景 D（团队自管权限）**：持有 manage 的 app-admins 组成员，在 Administration > Security > Permissions 里编辑 t-app 加成员——表单分区与 Artifactory permission target 编辑器同构。
- **场景 E（值班排障）**：SRE 从审计日志定位到一条删除记录，顺着面包屑跳到制品树对应路径核对 checksum，再下载留证——全程键盘可达。

---

## 4. 功能需求

约定：`BASE=http://localhost:8080`，控制台挂 `/binflow/`（不变）；Playwright spec 放 `web/tests/m8/`；`data-testid` 沿用 console-ux §10 命名规则，新增锚随票登记。**服务端契约零改动是全 FR 公共前置**——任何 AC 不得要求新端点/新字段。

### 4.1 IA 重构（种子 A，P0）

#### FR-71 双模式壳与导航树重排（web 前端：AppShell + 路由层）

**用户故事**：
- 作为 Artifactory 迁移用户，我打开 BinFlow 控制台看到的组织方式与 Artifactory 一致——浏览制品在 Application 模式、管配置在 Administration 模式——我的操作路径记忆直接可用。
- 作为非 admin 用户，我不应看到任何管理入口被「藏起来又找不到」的困惑：不可用的模式/分组整体不出现（console-ux §3.6 分层规则维持）。

行为规格：

- **双模式壳**：顶栏右侧模式切换（Application ⇄ Administration，admin 与 readonly_admin 可切；普通用户无切换入口）。默认落点：admin 登录后进 Application；带 `return` 参数时回跳原路由。
- **Application 模式**：左侧 = 制品浏览器树（FR-72，repo 按 LOCAL / REMOTE / VIRTUAL 三组节点分组，树内过滤框）；主区 = 选中实体的详情。仪表盘/搜索保留为 Application 的辅助入口（顶栏全局搜索，FR-74）。
- **Administration 模式**：左侧 = 管理树，分组对齐 Artifactory 心智内的 BinFlow 已有功能面：`Repositories`（仓库列表/新建）、`Security`（Users / Groups / Permissions）、`Governance`（Audit Log / Storage & GC / Quotas / Backup & Restore / Replication——BinFlow 自有分组承载，挂载位置 Q4 暂行）、`System`（实例信息/设置）。导航层级 ≤ 两级。
- **URL 深链**：每个树节点有稳定路由；刷新/回退/分享链接后树展开态与选中态恢复；旧路由（console-ux §3.2 现表）按映射表 redirect（Q3 暂行：M8 全量保留，M9 移除）。
- **角色可见性**：维持 M4/M7 路由门事实（whoami 预收敛 + 各请求 403 终裁），本条只重排呈现位置，不放宽不收紧。
- **testid 迁移**：受路由重排影响的锚出具「旧 → 新」映射表并入 console-ux §10；W 序列断言按映射表更新后零回归。

验收标准（AC）：

- **AC1** Playwright：admin 登录 → 默认 Application 模式（树可见）→ 切 Administration → 左树为管理树四分组 → 切回，状态不丢；URL 直开 Administration 深链 → 模式与选中态正确恢复。
- **AC2** Playwright：非 admin 登录 → 无模式切换入口；直开 `/binflow/#/admin/**` 深链 → 按现役 403 收敛规则呈现（不改 M7 语义）。
- **AC3** Playwright：旧路由 20 条逐条 redirect 到新路由且功能等价（映射表驱动）。
- **AC4** 零学习成本剧本①：Artifactory 用户（不查任何文档、仅凭 Artifactory 经验）30s 内从登录到达「建仓入口」——测试记录操作路径与耗时。
- **AC5** 服务端零改动审计：`git diff --stat internal/ cmd/`（前端票）为空。

### 4.2 制品浏览器（种子 A/B，P0）

#### FR-72 制品浏览器：左树右详情（web 前端：TreePage/NodeDetail 重排）

**用户故事**：
- 作为开发者，我在左树里逐层展开仓库找到制品，单击即在右侧看到概要（路径/大小/更新时间/checksum）并一键下载或拷贝 checksum——与 Artifactory Artifacts 浏览器的操作完全同构。
- 作为大 repo（万级节点）的用户，树懒加载、过滤框即输即筛，首屏不被拖死。

行为规格：

- **树**：三组节点（LOCAL/REMOTE/VIRTUAL）→ repo 节点 → 路径层级懒加载（现役 `GET /api/storage/{repo}/{path}` 等数据源不变）；树内过滤框（输即筛已加载层 + 前缀提示未加载匹配）；REMOTE 仓保留「刷新缓存」动作位（有权限时）。
- **文件节点详情面板**：tab 形态——概要（路径 mono+拷贝、大小、更新时间、sha256/sha1/md5 三值 mono+逐值拷贝、下载按钮）；packageType 特化视图（docker tags / npm packument / pypi simple 等 M4 语义原样迁入，位置从「仓库详情页内」移到「树选中详情」）。
- **文件夹节点**：children 表格（名称/大小/更新时间，列排序），排序纯前端。
- **危险操作**：删除制品/文件夹 = 危险确认（输入路径确认 + 影响面摘要），逐条调用既有删除端点。
- **状态矩阵**：loading（骨架屏）/ empty / error / success 四态按 console-ux §5 通用原则迁入新布局。

验收标准（AC）：

- **AC1** Playwright：展开 generic-local 三层 → 选中文件 → 右侧出现三 checksum 且逐值拷贝成功（剪贴板断言）→ 点下载得到内容与 curl `GET /binflow/{repo}/{path}` 逐字节一致。
- **AC2** Playwright：docker 仓选中 manifest 节点 → tags 特化视图数据与 `/v2/<name>/tags/list` 一致。
- **AC3** Playwright：树内过滤输入子串 → 树收敛到匹配路径；清空恢复。
- **AC4** 性能腿：预置 ≥ 10,000 节点目录结构（脚本灌库），首屏可交互 ≤ 1.5s、展开任一层 ≤ 200ms（P95，本地）。
- **AC5** 零学习成本剧本②：Artifactory 用户无文档完成「浏览到指定制品 → 拷贝 sha256 → 下载」。

### 4.3 管理面形态（种子 C，P0）

#### FR-73 管理面统一表格与编辑器 + Set-Me-Up 对话框（web 前端：Repositories/Users/Groups/Permissions 页组）

**用户故事**：
- 作为管理员，所有管理列表页长得一样：顶部工具栏（过滤 + New 按钮）、表格（列排序、行内动作），我学会一个页面就学会全部。
- 作为开发者，我在仓库上点 Set Me Up，弹出的对话框按协议分 tab 给我客户端接入命令，复制即用——和 Artifactory 的 Set Me Up 用法一致。

行为规格：

- **列表页标准形态**（repositories / users / groups / permissions / replications 五处统一）：工具栏（搜索/过滤 chip + New 主按钮）→ 表格（列头排序、行悬停显动作、空态引导）；分页/滚动按现役大目录策略。
- **仓库新建/编辑表单**：分组表单，分组与字段顺序对齐 Artifactory New Repository 心智（Basic：repo key / rclass / packageType；后续按 rclass 特化分组），字段集 = BinFlow 既有配置面零增减（wire 不动：建仓臂 `PUT /api/repositories/{key}`、更新臂 `POST /api/repositories/{key}`——M7 v1.2 勘误口径）。
- **用户/组/权限编辑器**：分区表单（Details / 权限矩阵），权限矩阵 = principals（users × groups）× 动作（read/write/delete/manage，M7 闭集）复选网格 + include/exclude patterns 编辑（M4 语义零变更）；readonly_admin 登录时编辑器整体只读态（M7 FR-66 行为迁入新布局）。
- **Set-Me-Up 式对话框**：仓库行/详情动作位弹模态（非页面跳转），按协议 tab 给接入命令块（docker login / settings.xml / .npmrc / pip.conf / curl + `bf` CLI，命令内容沿用现役 commands.ts 生成逻辑），带复制按钮。触发点：仓库列表行动作 + 树中 repo 节点动作。

验收标准（AC）：

- **AC1** Playwright：建 docker local 仓（分组表单走完）→ 列表出现 → Set Me Up 对话框拷出的 `docker login` 命令执行成功（真实 docker 客户端腿，接 M4 W 序列口径）。
- **AC2** Playwright：权限编辑器给组 app-admins 勾 read+write → 保存（`POST /api/v1/permissions` wire 逐字段与 M7 V07 一致）→ 成员用户 GET 制品 200。
- **AC3** Playwright：readonly_admin 打开用户编辑器 → 全表单 disabled、保存按钮不存在；admin 打开 → 角色下拉三值闭集（user/readonly_admin/admin）。
- **AC4** 零学习成本剧本③④：无文档完成「建仓并拿到 mvn settings.xml」「给组授权」。
- **AC5** 表格形态一致性：五列表页共享同一表格组件族（review 断言，无复制粘贴的表格实现 ×5）。

### 4.4 导航与面包屑（种子 D，P1）

#### FR-74 面包屑、全局搜索与深链状态（web 前端：AppShell + SearchPage）

**用户故事**：作为管理员，我在任何页面都知道自己在哪（面包屑）、怎么来的（回退不丢状态）、怎么直达（URL 可分享）。

行为规格：全局面包屑规则（模式 > 分组 > 实体 > 上下文，逐级可点回跳）；顶栏全局搜索框（回车/点击 → 搜索页带 query，结果按 ACL 过滤——现役语义）；SPA 路由状态（过滤、展开、tab 选中）编码进 URL，刷新不丢；404 页保留导航壳。

验收标准（AC）：
- **AC1** Playwright：Security > Users > alice 面包屑逐级点击回跳，目标页状态正确。
- **AC2** Playwright：搜索页过滤条件刷新后保持；结果集与 `GET /api/search` 同参数 curl 结果一致。
- **AC3** Playwright：未匹配路由 → 404 壳 + 返回入口。

### 4.5 键盘与批量（种子 E，P1）

#### FR-75 键盘可达与批量动作（web 前端：表格组件族 + 树）

**用户故事**：作为键盘党，我从登录到删制品全程不碰鼠标；作为清理者，我勾选多个过期制品路径一次删除。

行为规格：表格多选（表头全选/反选 + 行勾选）→ 批量动作条（当前仅删除；逐条调用既有删除端点，进度 + 成功/失败汇总，失败可重试单条——纯前端编排，无批量端点）；键盘矩阵：Tab 顺序、Enter 激活、Esc 关对话框、树方向键导航（↑↓ 移动、→ 展开、← 收起）；对话框焦点陷阱 + 关闭后焦点归还。

验收标准（AC）：
- **AC1** Playwright：仅用键盘完成「登录 → 建仓 → 上传 → 删除」全链（`page.keyboard` only，无 `click`）。
- **AC2** Playwright：勾选 3 个制品路径 → 批量删除 → 逐条 `GET` 均 404、汇总报告 3/3 成功；构造 1 条失败（权限）→ 汇总 2/3 且失败条目可识别。
- **AC3** Tab/Esc/焦点归还逐控件断言（对话框族抽测 ≥ 3 个）。

### 4.6 皮肤与设计 token（种子 F，P1）

#### FR-76 自有皮肤：设计 token 体系与零复制合规（web 前端：styles/tokens.css + ux-designer 共票）

**用户故事**：作为用户，我得到的观感与 Artifactory 同量级（信息密度、层级、组件形态熟悉），但一眼可辨这是 BinFlow——不是换了 logo 的 JFrog。

行为规格：token 全语义化（色彩/间距/字号/圆角/阴影/层级），亮暗双主题零样式分叉；信息密度维持 console-ux P1（表格行高 32px、13px 起步）；**视觉近似的判定口径 = 行为规格核对**（组件清单存在性、布局分区位置关系、交互流步骤、四态矩阵），**不是像素对比**——截图仅归档不作断言；品牌资产（logo/图标/favicon）自绘 SVG 源码入库，禁止引用 JFrog 任何资源；依赖清单不含 JFrog 发行包。

验收标准（AC）：
- **AC1** 组件清单核对：ui-console.md 冻结的组件清单 × 实现逐项存在（review 记录，非截图 diff）。
- **AC2** 零复制合规：`npm ls --all | grep -iE 'jfrog|artifactory'` 为空；`web/src` 与 `web/public` 无二进制图片资源入库（图标全 SVG 源码）；review 抽查无 JFrog CSS/图标复用。
- **AC3** 双主题等价：同一 spec 在亮/暗两主题各跑一遍全绿（样式分叉 = 编译期检查 token 引用）。
- **AC4** 对比度：核心文本/背景组合 ≥ 4.5:1（axe 或等价工具扫描主要页面零严重违例）。

### 4.7 债务包打包（种子 G，P1，单条 FR）

#### FR-77 M8 债券收编（六项打包；票面细目以 BOARD M7 完结节为准）

**用户故事**：作为 Artifactory 迁移团队，带特殊字符路径的制品在 UI 和 curl 里行为一致；作为运维，迁移降级路径、CI 时限、证据归档不再是悬账。

行为规格与验收标准：

- **AC1（T-231 percent-encode）**：internal/client 上传路径 percent-encode 缺陷修复；回归矩阵五腿——路径含 `%`、`#`、`?`、空格、UTF-8 中文，每腿两姿势：UI 上传对话框（Playwright `setInputFiles` 文件名带特殊字符）与 curl `PUT $BASE/binflow/generic-local/<encoded>`，均上传成功且树中呈现、下载、`GET .../api/storage/...` checksum 与源文件一致（矩阵 5×2 全绿）。
- **AC2（B-1）**：`bf migrate --skip-users` 降级语义正确（跳过用户面并明确告警），不带该旗标行为零回归；命令与期望输出记入 U 序列。
- **AC3（CI -timeout 20m）**：CI workflow 的 Go test `-timeout 20m` 落地，主干 CI 绿（附 run 链接）。
- **AC4（V28 附录移植）**：M7 V28（真实 Artifactory 实腿）证据按 real-env-appendix 模板移植入 `docs/user/` 附录，与 T-228 归档件交叉引用。
- **AC5（dialer 样板）**：13 处 dialer 样板收敛为单一构造（lint/review 断言样板 grep 为 0），行为零变化（既有 D/H 序列抽测绿）。
- **AC6（UI 打磨 4 条）**：BOARD M7 完结节所列 4 条逐条验证通过（命令随票登记，验收以 BOARD 细目为准——PRD 不重复展开）。
- 「ROADMAP M7 勾账同步」项随本 PRD 发稿完成（ROADMAP.md M7 段 checkbox + PRD 版本引用 v1.1→v1.2），不另立票。

---

## 5. 兼容性矩阵（M8 核心——对 Artifactory UI 的对齐分级）

### 5.1 层级定义（UI 域，M8 专用）

| 层级 | 定义 |
|---|---|
| **A 对齐** | 交互流与操作语义等同 Artifactory，且在 IA 中位置等同（Artifactory 用户零学习成本） |
| **B 语义等同但实现形态不同** | 交互流等同，但承载形态/位置/技术实现自有（如 CLI 承载 + UI 状态面板） |
| **C 子集对齐** | 在 BinFlow 已有功能面内对齐；Artifactory 有而 BinFlow 没有的维度不出现、不出占位 |
| **D 有意不兼容 / 自有** | 无 Artifactory 对应（BinFlow 自有能力）、或有意差异（自有皮肤）、或显式不做 |

「置信度」：高 = T-228 实例或 JFrog 公开文档双证；中 = 待 docs/reverse/ui-console.md 校准。

### 5.2 UI 对齐矩阵（24 条）

| # | 界面/交互流 | 行为要点 | 层级 | 优先级 | 置信度 | 验收 |
|---|---|---|---|---|---|---|
| UI-01 | 登录流 | 用户名+口令+错误提示+`return` 回跳；OIDC/LDAP 入口形态对齐（能力 = M6 已有） | A | P0 | 高 | U01 |
| UI-02 | 双模式壳（Application/Administration） | admin/readonly_admin 可切；普通用户无入口；深链恢复 | A | P0 | 高 | U02 |
| UI-03 | 制品浏览器左树 | rclass 三组节点/懒加载/树内过滤；无 Builds 等节点 | A | P0 | 高 | U03 |
| UI-04 | 制品详情面板 | 概要+checksum 拷贝+下载；packageType 特化视图迁入 | A | P0 | 高 | U04 |
| UI-05 | 文件夹 children 表格 | 列排序/空态/大目录分页 | A | P0 | 高 | U05 |
| UI-06 | Deploy 上传对话框 | 拖拽+进度+校验和；目标路径可改 | A | P0 | 高 | U06 |
| UI-07 | 删除确认模式 | 输入 repo key/路径确认+影响面摘要（危险区语义维持） | A | P0 | 高 | U07 |
| UI-08 | 仓库列表页 | 工具栏过滤（type/packageType）+ New 入口+行内动作 | A | P0 | 高 | U08 |
| UI-09 | 建仓/编辑分组表单 | 分组与字段顺序对齐；字段集 = BinFlow 既有面零增减 | C | P0 | 中（分组清单待校准） | U08 |
| UI-10 | Set-Me-Up 式对话框 | 模态+协议 tab+命令复制（五协议+bf CLI） | C | P0 | 高 | U09 |
| UI-11 | 顶栏全局搜索 | 跳搜索页带 query；结果按 ACL | C | P1 | 高 | U10 |
| UI-12 | 用户列表/编辑 | 角色下拉三值闭集（自有超集：readonly_admin）；replace 语义表单提示（M7 T-224 注记 UI 化） | C | P0 | 高 | U11 |
| UI-13 | 组管理 | 组 CRUD+成员编辑 | C | P1 | 中（嵌套组有无待校准） | U11 |
| UI-14 | 权限 target 编辑器 | principals×动作（r/w/d/m）矩阵+include/exclude patterns | C | P0 | 高 | U12 |
| UI-15 | 审计日志页 | 过滤（actor/action/repo/since）+表格；位置挂 Governance（Q4 暂行） | B | P1 | 高 | U13 |
| UI-16 | 备份/恢复面板 | export/import 由 CLI 承载，UI 为说明+最近结果状态（形态不同） | B | P1 | 高 | U13 |
| UI-17 | 复制页 | 目标表+事件状态轮询；CRUD UI 维持 M6「另票」口径不做 | C | P1 | 高 | U13 |
| UI-18 | GC / 存储治理页 | dry-run/apply 危险区分区；Artifactory OSS 无对应 apply UI | D（自有） | P1 | 中 | U13 |
| UI-19 | 配额页 | per-repo quotaBytes（自有） | D（自有） | P1 | 高 | U13 |
| UI-20 | 系统信息/设置 | version/健康/匿名读状态/改密（信息子集） | C | P1 | 高 | U13 |
| UI-21 | 主题切换 | 亮/暗双主题等价（默认主题 Q2 暂行） | B | P1 | 高 | U14 |
| UI-22 | 视觉皮肤 | 配色/图标/字体/品牌自绘——**有意差异，零复制** | D（有意差异） | P1 | 高 | U15 |
| UI-23 | Access Tokens 页 | 现状占位维持（非 M8 对齐面；仅归位到 Security 分组） | D（暂缓） | — | — | U02（仅位置断言） |
| UI-24 | Xray/Builds/Release Bundles/Pipelines 节点 | **不出现**（Non-goal；无占位） | D（不做） | — | 高 | U02（负向断言） |

> 计数：**24 条 = A 8（UI-01~08）+ B 3（UI-15/16/21）+ C 8（UI-09~14、17、20）+ D 5（UI-18/19/22/23/24）**。M8 无「路径不同」类（路由为 SPA 内部结构，不属对外契约）；对外契约面（REST/协议）本里程碑零改动。

### 5.3 验收方式分级矩阵（M8 判定标准）

| 验收面 | 工具 | 分级 |
|---|---|---|
| IA/交互流（UI-01~08、10~14、17、20、21） | Playwright（真实 Chromium） | **P0/P1 必须全过** |
| 视觉近似（UI-22） | 行为规格清单核对（review 记录）+ 零复制扫描 | P1（**禁止像素 diff**） |
| 服务端零改动 | M1~M7 P0 序列复跑 + `git diff` 审计 | **硬门槛** |
| W 序列锚回归 | Playwright（映射表驱动） | **硬门槛** |
| 债务包（FR-77） | curl + Playwright + CLI + CI | P1 全过 |
| 真实客户端腿 | docker login（Set-Me-Up 命令真跑） | P0 |

### 5.4 M8 核心验收命令（U 序列骨架，QA 直接引用）

> **判定口径（头注）**：视觉「近似」以行为规格核对判（组件存在/分区位置关系/交互步骤/四态），截图仅归档；**任何 AC 不得以像素 diff 或截图逐位对比作判据**。spec 置 `web/tests/m8/`，`-g` 过滤。

```bash
# ========== IA（FR-71） ==========
# U01 登录/回跳（UI-01）
npx playwright test tests/m8/login.spec.ts -g "login and return-url"
#   断言：错误口令提示不泄露存在性；成功后回跳 return；OIDC 入口呈现（不点穿）
# U02 双模式壳（UI-02/23/24）
npx playwright test tests/m8/ia.spec.ts -g "mode-switch, deep-link, negative-nav"
#   断言：admin 可切两模式；普通用户无切换入口；Administration 树四分组齐；
#   负向：树中不出现 Builds/Xray/Pipelines 等节点（UI-24）；Tokens 占位仅归位（UI-23）
# U03 旧路由 redirect：映射表驱动 20 条逐条断言落点与功能等价

# ========== 制品浏览器（FR-72） ==========
# U04 树+详情（UI-03/04）：展开三层→选中文件→三 checksum 逐值剪贴板断言
#   下载内容与 curl 逐字节一致：
curl -su admin:$ADMIN_PW $BASE/binflow/generic-local/<path> -o /tmp/a && cmp /tmp/a /tmp/uploaded   # 一致
# U05 children 表格排序/空态；U06 上传对话框特殊文件名腿见 U20；U07 删除确认（输错路径不执行，输对执行+树消失）
# U08 大 repo 性能腿（AC4）：脚本灌 10k 节点，playwright trace 记录首屏 ≤1.5s、层展开 ≤200ms P95

# ========== 管理面（FR-73） ==========
# U09 建仓→Set-Me-Up（UI-08/09/10）：
npx playwright test tests/m8/admin-console.spec.ts -g "create-repo-and-setmeup"
#   尾腿真实客户端（P0）：拷出的命令真跑——
docker login $BASE -u admin -p $ADMIN_PW && docker pull $BASE/<repo>/hello:t   # 命令来自对话框拷贝
# U10 全局搜索/深链（FR-74）；U11 用户/组（UI-12/13）：readonly_admin 只读态 + admin 角色三值
# U12 权限编辑器（UI-14）：勾选→保存→wire 与 M7 V07 逐字段一致（网络断言）→成员 GET 制品 200
# U13 治理五页四态（UI-15~20）：GC dry-run 出报告不 apply；配额/备份/复制/审计过滤各一腿
# U14 主题等价（UI-21）：亮/暗各跑核心 spec 全绿

# ========== 皮肤合规（FR-76） ==========
# U15 零复制扫描（AC2）：
npm ls --all | grep -iE 'jfrog|artifactory' ; echo "exit=$?"   # 期望无匹配
git ls-files 'web/public/**/*.{png,jpg,gif,ico}'               # 期望空（图标全 SVG 源码）
#   + axe 扫描主要页面，serious 违例 = 0

# ========== 债务包（FR-77） ==========
# U20 T-231 percent-encode 矩阵（5×2）：
for name in 'a%b' 'a#b' 'a?b' 'a b' '中文文件'; do
  curl -su admin:$ADMIN_PW -T /tmp/f "$BASE/binflow/generic-local/enc/$name" -o /dev/null -w "%{http_code} $name\n"; done  # 5×201
#   UI 腿：playwright setInputFiles 文件名同矩阵 → 树可见+下载 cmp 一致+checksum 一致
# U21 bf-migrate --skip-users（B-1）：输出含降级告警、用户面未迁移；无旗标腿零回归
# U22 CI -timeout 20m：主干 run 链接归档（绿）
# U23 V28 附录移植：docs/user/ 附录存在且与 T-228 归档件交叉引用
# U24 dialer 样板：grep -rn '<样板签名>' internal/ | wc -l   # 0；D/H 序列抽测绿

# ========== 回归硬门槛（全里程碑收口跑） ==========
# W 序列锚迁移回归（映射表驱动）+ M1~M7 P0 序列复跑（本地 filestore 全绿）
```

### 5.5 待校准项（ui-console.md 落地后回写）

| # | 项 | v1.0 暂行值 | 校准来源 |
|---|---|---|---|
| K14 | 建仓表单分组清单与字段顺序（UI-09） | Basic（key/rclass/packageType）+ rclass 特化分组，字段子集 | docs/reverse/ui-console.md（7.84.10 实例） |
| K15 | 组管理是否含嵌套组呈现（UI-13） | 无嵌套（现役模型）；若 Artifactory 心智强依赖则记 B 级差异不实现 | 同上 + M4 组模型 |
| K16 | Governance 域挂载位置与命名（UI-15~19，Q4） | 保留 BinFlow「Governance」分组（语义等同、标签自有） | 同上（Artifactory 对应物清单） |
| K17 | 树内过滤的最小匹配半径与未加载层提示形态 | 已加载层即时筛 + 计数提示 | 同上 |

### 5.6 回归基线（M8 不反转既有断言）

| 既有断言 | M8 期望 |
|---|---|
| W 序列（M4）+ 242 testid 锚 | 零回归；路由重排影响的锚按映射表更新（断言语义不变） |
| M7 V01~V35（RBAC/续传/step-up） | 零回归（服务端契约零改动） |
| console-ux §3.3 角色可见性（whoami+403 分层） | 行为零变更，仅呈现位置重排 |
| 上传路径特殊字符（现状缺陷） | **反转**：T-231 修复后 5×2 矩阵全绿（现状 UI 腿失败属已知缺陷） |

---

## 6. 非功能需求（NFR）

### 6.1 与已有 ADR / 规范的冲突/补充标注

| ADR/规范 | 冲突/补充点 | 本 PRD 立场 | 所需动作 |
|---|---|---|---|
| ADR-0001（clean-room） | 对 UI 生效的执行细则（conductor 口径） | 产出限行为规格；JFrog 图标/样式/代码零复制；参照 = T-228 实例 + 公开文档 + reverse-src 只读 | reverse-engineer 在 ui-console.md 头部重申边界 |
| ADR-0014（console/session） | IA 重排不得动 session/cookie 语义 | 零改动；登录流仅重排呈现 | — |
| console-ux P4（暗色优先） | 默认主题可能与 Artifactory 心智（亮色）不一致 | Q2 暂行：亮色默认 + 暗色切换；终裁后 ux-designer 回写 console-ux v2.0 | Q2 处置 |
| console-ux §3.1/3.2（现役 IA/路由） | 全量重排 | 旧路由 redirect 映射表（Q3）；console-ux 升 v2.0 承载新 IA | ux-designer 票 |

### 6.2 性能（M8 增量）

| NFR | 指标与验收方式 | 优先级 |
|---|---|---|
| NFR-P36 体积与首屏 | console 产物 gzip ≤ 350KB（vite build 报告归档为基线）；二进制 < 40MB 维持；本地登录→首屏可交互 ≤ 1.5s | P0 |
| NFR-P37 浏览器交互延迟 | 树层展开/表格排序/过滤即输即筛 ≤ 200ms（P95，10k 节点数据集，playwright trace 记录） | P1 |
| NFR-P38 零服务端开销 | 服务端 P95（M7 基线：匿名 6.8ms）偏差 < 10%——UI 重排不增加管理面请求扇出（首屏管理面请求数 ≤ 现役 +2） | P1 |

### 6.3 安全底线（M8 增量）

| NFR | 要求 | 验收 |
|---|---|---|
| NFR-S45 前端注入面 | 制品路径/名称/repo key/user 名在 DOM 全部转义渲染；特殊字符路径（T-231 矩阵）不产生逃逸 | U20 + code review |
| NFR-S46 资源自包含 | 无外链 CDN/字体/脚本（全 embed）；CSP 基线维持现役 | `grep -rn 'http://\|https://' web/src | grep -v '//.*注释'` 审查 + CSP 头回归 |
| NFR-S47 权限呈现不扩大 | 可见性收敛维持 whoami+403 双信号；新 IA 不得以「隐藏入口」替代服务端门（直开深链仍 403 收敛） | U02 |

### 6.4 可观测性（M8 增量）

- 前端不新增上报端点（零服务端改动）；结构化 console 错误日志维持现役风格。
- /metrics 零新增（管理面指标族继续冻结，同 M7 口径）。

---

## 7. 开放问题（Q1~Q6，均带暂行；需用户/conductor 决策，PM 不代拍）

| # | 问题 | 影响面 | 暂行口径（v1.0） |
|---|---|---|---|
| Q1 | **对齐基线版本**：Artifactory UI 取哪个版本形态作对齐参照（self-hosted OSS vs Cloud/SaaS；7.8x vs 7.9x） | 全部矩阵行；逆向规格范围 | 以 **T-228 保留的真实 Artifactory OSS 7.84.10 实例** + JFrog 公开文档为准；SaaS-only 特性（Insight/Xray 入口等）不对齐。终裁：用户如指定其他版本，reverse-engineer 重取基线 |
| Q2 | **默认主题**：暗色优先（console-ux v1.3 P4）还是亮色默认（Artifactory 观感心智） | FR-76；console-ux v2.0 | 暂行**亮色默认 + 暗色一键切换**（贴近「和 JFrog 一样」的指令预期）；终裁后回写 console-ux（推翻 P4 须 ux-designer 记录） |
| Q3 | **旧路由兼容期**：现役 20 条 SPA 路由的 redirect 保留多久（外部书签/脚本锚点影响） | FR-71-AC3；W 序列锚迁移 | M8 全量 redirect 映射 + M9 移除；映射表入 console-ux §10。终裁：用户如要求更长兼容期，改 FR-71 行为规格 |
| Q4 | **Governance 域挂载**：GC/配额/备份/复制/审计照 Artifactory 的域结构重命名挂载，还是保留 BinFlow「Governance」分组 | UI-15~19；K16 | 暂行保留 Governance 分组（B 级语义等同）；待 ui-console.md 给出 Artifactory 对应物清单后再定是否重挂 |
| Q5 | **前端技术选型**：现 vanilla TS + vite，IA 重排是否引框架/组件库 | NFR-P36 达成路径；票量 | PRD 不预设（architect ADR 领地）；硬门 = 体积预算 + 行为规格，选型失败以 NFR 兜底 |
| Q6 | **UI 打磨 4 条的票面展开**：并入 FR-77 打包票还是独立票 | 拆票结构 | 暂行并入 FR-77（AC6 以 BOARD M7 完结节细目为准，PRD 不重复展开）；tech-lead 拆票时如认为 area 冲突可独立成票 |

---

## 8. M8 验收剧本（QA 总纲）

1. **回归基线（硬门槛先行）**：M1~M7 全部 P0 序列本地 filestore 复跑全绿；W 序列按锚映射表跑零回归。
2. **IA**：U01（登录）→ U02（双模式/深链/负向导航）→ U03（旧路由 redirect）。
3. **制品浏览器**：U04~U08（树/详情/checksum 拷贝/下载对照/上传/删除确认/大 repo 性能腿）。
4. **管理面**：U09（建仓+Set-Me-Up+真实 docker 客户端腿）→ U10~U13（搜索/用户/组/权限编辑器 wire 对照/治理五页）。
5. **键盘与批量**：键盘全链 + 批量删除成功/失败汇总（FR-75）。
6. **皮肤合规**：U14（双主题等价）→ U15（零复制扫描 + axe）+ 组件清单核对（review 记录，非像素）。
7. **债务包**：U20（percent-encode 5×2 矩阵）→ U21（--skip-users）→ U22（CI）→ U23（V28 附录）→ U24（dialer 样板）+ UI 打磨 4 条。
8. **零学习成本剧本① ~ ⑧**：每剧本一名「Artifactory 经验、零 BinFlow 文档」的执行者（qa 担任），记录操作路径与耗时，无卡壳完成即 PASS（卡壳点登记为缺陷或开放问题）。
9. **文档**：tech-writer 控制台指南按新 IA 改版（截图更新）+ 迁移用户「操作路径对照表」（Artifactory 路径 → BinFlow 路径）。

---

## 9. M8 DoD

1. §4 全部 P0 AC（FR-71/FR-72/FR-73）经 qa 验证全绿；P1（FR-74~FR-77）全绿；
2. §8 剧本全绿（含 8 个零学习成本剧本，卡壳点清零或降级为已裁开放问题）；§5.3 分级矩阵各 P0/P1 面全过；
3. 回归硬门槛：M1~M7 全部 P0 序列复跑全绿；W 序列 242 锚（映射后）零回归；服务端 `git diff` 审计确认零契约改动（FR-77 修复项除外）；
4. 前置产物齐备：docs/reverse/ui-console.md 冻结（行为规格制，clean-room 边界重申）；前端重排 ADR Accepted；console-ux v2.0（新 IA + 组件清单 + token 修订）发布；
5. tech-writer 控制台指南改版 + 操作路径对照表交付；
6. NFR-P36 体积基线归档；lint/test/CI 绿（含 `-timeout 20m`）；全仓 golangci-lint 0 issues、gofmt 空维持；
7. FR-77 六项全收口（V28 附录、--skip-users、T-231 矩阵、CI、dialer、UI 打磨 4 条）；
8. 主会话 git tag `m8-done`（对外发布任何制品先经用户确认）。
