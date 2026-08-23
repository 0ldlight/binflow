# BinFlow M8 控制台设计规格（console-m8）——Artifactory 对齐重排

| 项 | 值 |
|---|---|
| 文档 | `docs/design/console-m8.md` |
| 票据 | M8 规划（用户指令 2026-08-23：「前端 UI 和交互逻辑要求和 JFrog 一样」） |
| 状态 | v1.0（2026-08-23，ux-designer） |
| 上游依据 | `docs/reverse/console-ui.md`（Artifactory OSS 7.84.10 活体行为规格，**唯一对齐参照**）；`docs/design/console-ux.md` v1.3（M4 基线：四态原则/权限可见性/可达性/testid 纪律，继续有效）；`docs/prd/milestone-7.md` §4（FR-64~FR-68：角色/manage/step-up 语义）；`web/src/main.tsx` + `AppShell.tsx`（现役路由与壳事实）；BOARD.md M8 执行口径 |
| 下游消费者 | tech-lead（M8 拆票）、前端 dev（页面组票）、qa-engineer（Playwright 断言与四态验收） |

---

## 0. 定位、口径与 clean-room 声明

### 0.1 对齐口径（conductor 定案，本文件全程遵守）

- **对齐 = 信息架构 + 交互逻辑 + 操作流**：Artifactory 迁移用户零学习成本——同一个动作在同样的位置、走同样的步骤、得到同样形态的反馈。
- **视觉近似但自有皮肤**：信息密度与灰阶节奏近似（三阶纵深、密表格、小圆角），色值/字体/阴影全部自有取值（§5）。
- **clean-room（ADR-0001 对 UI 生效）**：本文件只含行为规格（布局描述/交互流/组件清单/状态矩阵）。JFrog 的图标、样式、代码、视觉资产、文案字符串**零复制**；reverse 规格中的英文文案仅作语义锚，BinFlow 皮肤下中文化但语义对齐。
- **服务端契约零改动**：M7 全部语义（RBAC 三角色 / manage 派生 / step-up / 续传 / 403 收敛四层）原样保留（§7）。所有新交互 = 现役端点的前端重排；无端点支撑的 Artifactory 形态**不建**（§1.3）。
- **范围 = BinFlow 已有功能面**。Xray / Pipelines / Distribution / Builds / Projects 等 JFrog 独立产品不进入（PRODUCT Non-goal）。

### 0.2 与 console-ux.md 的关系

console-m8 是 M8 **目标态**规范：IA、路由、页面形态、设计 token 以本文件为准。console-ux 的 §3.6（权限可见性矩阵与 403 收敛四层）、§5（四态通用原则与骨架屏策略）、§6（大目录策略）、§7.3（mono 应用规则）、§8（可达性）、§10（testid 命名纪律）**继续有效**，本文件引用不重复；两文冲突处（IA/路由/token 取值）以 console-m8 为准，落地时回写 console-ux 升版或标注被替代章节。

---

## 1. 信息架构：Artifactory → BinFlow 映射

### 1.1 一级结构对齐：双模式侧栏（对齐 reverse §1.1）

Artifactory 的核心 IA 事实是**按上下文切换的两种侧栏模式**，URL 前缀区分。BinFlow M8 采纳同一结构：

| 模式 | URL 前缀（应用内） | 侧栏分组 | 落地页 |
|---|---|---|---|
| 应用模式 | `/artifacts` `/dashboard` `/search` `/profile` | **应用**：仪表盘、制品 | `/artifacts`（跨仓制品树） |
| 管理模式 | `/admin/**` | **仓库** / **用户与权限** / **治理** / **监控** / **常规** | `/admin/repositories` |

- 模式切换：侧栏底部常驻切换项——应用模式显示「管理」（admin / readonly_admin 可见，L1 预收敛），管理模式显示「返回应用」。URL 进 `/admin/**` 即渲染管理模式侧栏（与 Artifactory `/ui/admin/*` 同构）。
- 登录成功落 `/artifacts`（Artifactory 落应用模式首页；其 Packages 页 BinFlow 不建，制品树是最近似落点）。**空实例引导**：仓库数为 0 时制品页内嵌引导卡「创建仓库」+「跳过」（等价 Artifactory onboarding 卡行为；不设独立路由，跳过状态存 localStorage）。
- 侧栏底部固定许可行：`BinFlow v<version> · 单二进制制品仓库`（对齐 Artifactory 侧栏底部署名行为；版本来自 `/api/system/version`）。
- Artifactory 的 `SERVICES → Artifactory` 三级服务配置树**不采纳**：BinFlow 是单服务产品，无微前端；其承载的功能（备份/维护）归入「治理」分组二级项。

### 1.2 Artifactory 页面 → BinFlow 对照表（对齐 / 重塑 / 不建）

出处 = reverse 规格 §1（IA）与 §3（页面骨架）。「对齐」= 同位置同交互流；「重塑」= 采纳其形态骨架但字段集按 BinFlow 契约收窄；「不建」= BinFlow 无此功能面且契约零改动。

**应用模式**

| Artifactory 目标 | BinFlow M8 | 口径 |
|---|---|---|
| Dashboard | `/dashboard` 仪表盘 | 保留 BinFlow 五卡形态（reverse §3.1 未深走 Dashboard，无对齐负担） |
| Artifacts（树浏览器） | `/artifacts/<repo>/<path…>` 跨仓树 | **对齐**（§6.3）：仓库为顶层节点、懒加载、URL 即状态、深链自动展开、右侧详情面板、三级右键菜单 |
| Packages（卡片流落地页） | — | 不建：无包索引聚合面（PRODUCT 无此功能）；落点让位制品树 |
| Builds / Xray / Distribution / Pipelines / Integrations | — | 不建：JFrog 独立产品（Non-goal） |
| 顶栏搜索（类型下拉 Packages/Artifacts/Builds + 搜索框 + 漏斗） | 顶栏搜索框 + 漏斗 | **重塑**：BinFlow 只有制品搜索——类型下拉退化为固定类型「制品」（单一值不渲染下拉，R2 类型化落地后再现）；placeholder「搜索制品」；Enter → `/search` |
| 用户菜单（Quick Repository Creation / New User·Group·Permission / Edit Profile / Logout） | 同构用户菜单 | **对齐**（§2.3）：快速建仓子菜单（Set Me Up / 新建 Local·Remote·Virtual 仓）、新建用户/组/权限、编辑档案、登出 |

**管理模式**

| Artifactory 目标 | BinFlow M8 | 口径 |
|---|---|---|
| Repositories → Repositories（Local/Remote/Virtual 三 Tab 列表） | `/admin/repositories`（三 Tab 子路由） | **对齐**（§6.7）：Tab + 「N 个仓库」计数 + 右上「添加仓库」+ 行 hover 删除图标 + 列头排序 + 底部计数行 |
| Repositories → Layouts | — | 不建：BinFlow 无自定义仓库布局模型 |
| User Management → Users | `/admin/security/users` | **对齐**（§6.9）：列表 + New user + 编辑表单（User Settings / Options / Password / 相关组穿梭 / 权限矩阵） |
| User Management → Groups | `/admin/security/groups` | **对齐**：列表 + 表单 + 组权限矩阵 |
| User Management → Global Roles | — | 不建：无全局角色模型（M7 角色挂在用户上） |
| User Management → Permissions | `/admin/security/permissions` | **对齐**（§6.11）：列表 + 单页分区编辑器（Name / Resources / Users / Groups）+ **两步资源对话框**（选仓库 → 可选 patterns） |
| User Management → Settings | — | 不建：并入 `/admin/general/settings` 只读展示 |
| User Management → Access Tokens | `/admin/security/tokens` | **重塑**：R6（token 列表/吊销管理面）未落地，保持 P2 占位（签发引导 + 输入 token_id 吊销）；落地后升为 Artifactory 形态 |
| Authentication Providers（LDAP/SAML/OAuth/HTTP SSO/Crowd） | — | 不建：PRODUCT 明确第一版不做（本地用户 + API Token） |
| General → Settings | `/admin/general/settings` 系统信息 | **重塑**：Server Name / Base URL 只读展示（YAML 配置面，无写端点）；Logo 上传 / Custom Message / Custom Login Dialog 不建 |
| General → Mail Server / Webhooks / Manage Integrations | — | 不建 |
| Proxies | — | 不建 |
| Monitoring → Storage Summary | `/admin/monitoring/storage` 存储概要 | **对齐**（§6.18）：刷新行 + 汇总卡 + 仓库表（TOTAL 首行） |
| Monitoring → Service Status | 系统信息页健康卡 | **重塑**：`/api/v1/health` 子系统卡（BinFlow 已有数据面） |
| Monitoring → System Logs | — | 不建：无日志查看端点（契约零改动） |
| SERVICES → Artifactory → Backups（计划列表 + 表单） | `/admin/governance/backup` | **重塑**：BinFlow 备份是 export/import 任务（R5），无 cron 计划端点——保留导出/导入双卡形态；**计划列表形态不建**（无 `POST /api/v1/backups` 类端点） |
| SERVICES → Advanced → Maintenance（GC/配额/清理分块） | `/admin/governance/gc` 维护 | **对齐**（§6.14）：自上而下分块（GC / 配额控制 / 清理）+ 底部 Reset/Save 的页面骨架；字段按现役 GC 端点收窄 |
| 配额独立页 | `/admin/governance/quotas` | **保留自有页**（OSS 仅 Maintenance 两个百分比字段；BinFlow 是 per-repo 配额模型 R7，一页装不下——记录为有意增强） |
| Import & Export（三卡） | 并入 `/admin/governance/backup` 导入/导出卡 | **对齐**：卡式布局语义一致 |
| Property Sets / Maven Indexer / Keys / Certificates / Config Descriptor / Log Analytics / UI Settings / HTTP Settings | — | 不建：无对应功能面 |
| （OSS 缺位）审计日志 | `/admin/governance/audit` | **保留自有页**（reverse §7：OSS 无路由） |
| （OSS 缺位）全局 Replication 配置 | `/admin/governance/replication` | **保留自有页**（OSS 404；T-180 CRUD 端点已落地，CRUD UI 另票） |

### 1.3 二级信息架构全图（导航树逐项）

```
── 应用模式 ──────────────────────────────────────────────
应用
├ 仪表盘                 /dashboard
└ 制品                   /artifacts（跨仓树，落地页）

── 管理模式（admin / readonly_admin 可见）────────────────
仓库
└ 仓库                   /admin/repositories
    ├ Tab Local          /admin/repositories/local
    ├ Tab Remote         /admin/repositories/remote
    └ Tab Virtual        /admin/repositories/virtual

用户与权限
├ 用户                   /admin/security/users
├ 组                     /admin/security/groups
├ 权限                   /admin/security/permissions
└ Access Tokens          /admin/security/tokens（P2 占位）

治理
├ 审计日志               /admin/governance/audit
├ 维护（GC）             /admin/governance/gc
├ 配额                   /admin/governance/quotas
├ 复制                   /admin/governance/replication
└ 备份 / 恢复            /admin/governance/backup

监控
└ 存储                   /admin/monitoring/storage

常规
└ 系统信息               /admin/general/settings

── 全局（两模式共享）─────────────────────────────────────
登录                     /login
搜索结果                 /search
编辑档案（改密）         /profile
404                      未匹配（保留导航壳）
```

导航条目合计 14（应用 2 + 管理 12）；分组标题是标签不是折叠项（沿 console-ux §3.1 纪律）。管理模式页面加**面包屑**（如 `仓库 / maven-remote`），对齐 Artifactory 管理页层级表达。

### 1.4 路由表与兼容重定向

| M8 路由 | 页面 | 现役路由 → 重定向 |
|---|---|---|
| `/login` | 登录 | 不变 |
| `/artifacts`、`/artifacts/:repo/*` | 跨仓制品树（URL 即状态） | `/` → `/artifacts`；`/repositories/:key/tree/*` → `/artifacts/:key/*` |
| `/dashboard` | 仪表盘 | `/` 已改指制品树；仪表盘仅侧栏入口 |
| `/search` | 搜索结果 | 不变 |
| `/profile` | 编辑档案（改密） | `/settings` → `/admin/general/settings`；改密块移 `/profile` |
| `/admin/repositories/{local\|remote\|virtual}` | 仓库管理三 Tab | `/repositories` → `/admin/repositories/local` |
| `/admin/repositories/new` | 新建仓库（进页弹包类型选择） | `/repositories/new` → 同左 |
| `/admin/repositories/:key` | 仓库详情（概要/接入命令/统计/危险区，BinFlow 自有增强页） | `/repositories/:key` → 同左 |
| `/admin/repositories/:key/edit` | 编辑仓库（Basic/Advanced 表单） | `/repositories/:key/settings` → 同左 |
| `/admin/security/users[/:name]` | 用户列表/编辑 | `/security/users*` → 同左 |
| `/admin/security/groups[/:name]` | 组列表/编辑 | `/security/groups*` → 同左 |
| `/admin/security/permissions[/new\|/:name]` | 权限列表/编辑器 | `/security/permissions*` → 同左 |
| `/admin/security/tokens` | Tokens（P2 占位） | `/security/tokens` → 同左 |
| `/admin/governance/audit` | 审计 | `/audit` → 同左 |
| `/admin/governance/gc` | 维护（GC） | `/governance/gc` → 同左 |
| `/admin/governance/quotas` | 配额 | `/governance/quotas` → 同左 |
| `/admin/governance/replication` | 复制 | `/governance/replication` → 同左 |
| `/admin/governance/backup` | 备份/恢复 | `/governance/backup` → 同左 |
| `/admin/monitoring/storage` | 存储概要 | 新页（现役端点编排：stats + usage/{key}） |
| `/admin/general/settings` | 系统信息 | `/settings` → 同左 |

重定向为客户端 replace（SPA 内），保留 e2e 旧路径兼容窗口；**testid 锚不随路由改名**（console-ux §10 纪律：已落地 242 锚是 QA 资产，改名视同破坏性变更需过 conductor）。新增页面组按「先入 §10 清单再落码」流程补锚（命名规则不变，前缀随页面根名更新，如 `tree-page` 跨仓化后仍为 `tree-page`）。

### 1.5 覆盖率结论

- BinFlow 侧：现役 20 个路由页面全部进入 M8 IA；16 个获得 Artifactory 对齐/重塑形态，4 个保留自有页（审计、复制、配额、仓库详情概要——后三者为 BinFlow 能力超出 OSS 的部分）。
- Artifactory 侧：约 40 个导航目标中 15 个有 BinFlow 对应，其余约 25 个显式不建（多为独立产品 / 认证提供方 / 无端点配置面），逐项登记于 §1.2 表，不留「影子入口」。

---

## 2. 全局框架

### 2.1 壳与双模式侧栏

```
┌──────────┬──────────────────────────────────────────────────────────────┐
│ 侧栏      │ 顶栏（48px）                                                  │
│ 224px    │  [面包屑/页面标题]        [⌕ 搜索制品 ⌘K] [漏斗] [?] [◐] [用户 ▾]          │
│ 可折叠    ├──────────────────────────────────────────────────────────────┤
│ 48px     │ 内容区（max-width 1440px 贴左；管理模式页面带面包屑行）           │
│ …        │                                                              │
│ (底栏)    │                                                              │
│ 版本行    │                                                              │
│ 模式切换  │                                                              │
│ 用户盒    │                                                              │
└──────────┴──────────────────────────────────────────────────────────────┘
```

- 侧栏三阶纵深的**最暗层**（§5 灰阶节奏）；导航项 32px 高；active 项左侧 2px accent 指示条。
- 顶栏元素（左→右）：页面标题/面包屑、全局搜索（placeholder「搜索制品」，管理模式不变——BinFlow 无管理资源搜索端点，不伪造 Artifactory 的 "Search Admin Resources"）、主题切换、帮助（`/binflow/docs/`）、用户菜单。
- 搜索旁**漏斗图标**：点击展开筛选浮层（仓库/包类型/仓型三过滤），等价 Artifactory filterIconButton；筛选变更即重查 `/search`（P2 polish，低置信度项按 BinFlow 自有过滤面板落地）。

### 2.2 非 admin 在新 IA 下的可见性（沿 §3.6 四层，不改姿态）

| 载体 | 非 admin 已登录 |
|---|---|
| 应用模式侧栏 | 仪表盘（实例卡 + 收敛说明）、制品（跨仓树**顶层仓库列表不可得**——`GET /api/repositories` admin 门（漂移 B 定案维持）→ 树区显示 L2 无权限卡 + 搜索/直链引导；**已知 repo key 的深链与搜索结果按自身路径 ACL 可达**）、搜索 |
| 管理模式入口 | 不渲染（readonly_admin 渲染——M7 扩展，§7.3） |
| `/admin/**` 直链 | 页面壳 + L2 无权限卡 |
| 顶栏快速建仓/新建用户等子菜单 | 不渲染（L4 写入口预收敛） |

### 2.3 用户菜单（对齐 reverse §1.5）

| 项 | 行为 |
|---|---|
| 快速建仓（子菜单） | **Set Me Up**（§4.1 向导）/ 新建 Local 仓库 / 新建 Remote 仓库 / 新建 Virtual 仓库 → `/admin/repositories/new?rclass=…` |
| 新建用户 / 新建组 / 新建权限 | 直达各自 `/new` |
| 编辑档案 | `/profile`（改密 + 自有 token 说明） |
| 切换主题 / 登出 | 现役行为保留；登出走确认对话框（BinFlow 保留，Artifactory 无此确认——服务端吊销语义值得显式化） |

菜单项按可见性规则裁剪（非 admin 只见编辑档案/主题/登出；readonly_admin 另见「管理」模式切换但不见快速建仓写入口）。

---

## 3. 交互四态与组件规范

### 3.1 四态基元（沿用 console-ux §5 原则 + M8 增补）

| 态 | 规范 |
|---|---|
| 加载 | 首屏/换页骨架屏（150ms 延迟防闪烁；表格 = 表头 + N 行灰块；树 = 可见区 ~15 节点占位）；局部动作 = 按钮 spinner + 禁用，不用全屏遮罩；>3s 次级提示、>15s 追加重试链接。轮询页（复制/迁移面板）瞬断不清屏：保留旧数据 + 行内「上次刷新失败」 |
| 空 | 一句话 + 一个主行动 + 一条文档链接；区分**从未有数据**（建仓 CTA）与**过滤后为空**（标准文案「未找到结果 · 尝试更改搜索条件」+ [清除过滤]——对齐 Artifactory LDAP 空态文案语义）；空仓库详情按协议给接入命令空态（docker 仓空态 = `docker push` 教学，BinFlow 既有价值保留） |
| 错误 | 区块错误卡（人话一句 + 原始 message 折叠 mono + 重试）；401 → 登录过期 toast + `/login?return=`；403 → 四层收敛（L1 不渲染 / L2 无权限卡 / L3 卡片隐藏 / L4 写入口不渲染；操作中 403 行内呈现并指向权限模型）；404 与空态区分；400 表单行内；5xx 错误卡 + 重试 + 健康链接。三种错误体格式统一提取 message |
| 成功 | toast（右下，成功 5s / 错误常驻；含对象 key mono）+ 就地跳转或刷新；上传成功必须展示 checksum 比对徽标；token 明文一次性面板；无庆祝动画 |

### 3.2 关键视图四态矩阵（M8 新增/重排页）

| 视图 | loading | empty | error | success |
|---|---|---|---|---|
| 跨仓制品树 | 树顶层骨架（仓库节点行）→ 展开节点子级骨架；右面板骨架 | 空实例：引导卡「创建仓库」+ 跳过；空目录：「此目录为空」+ 上传/刷新入口 | 顶层 403 → L2 无权限卡（非 admin）；子树 403 → 该子树无权限卡；404 路径不存在（面包屑可回退） | 选中即时联动右面板；删除行淡出 |
| 仓库管理三 Tab | 表格骨架 8 行 | 「还没有仓库」+「添加仓库」CTA；过滤空 → 清除过滤 | 错误卡 + 重试；403 → L2 | 新建 toast + 跳编辑页；删除行淡出 + toast |
| 仓库编辑表单 | 表单骨架 | 新建态 = 包类型对话框先行 | 400 行内（key 校验文案）；409 重名行内 | 保存 toast + 留在页（编辑）/ 跳详情（新建） |
| 用户/组/权限编辑器 | 表单骨架 | 新建态空表单 + 默认值 | 400/403 行内（服务端原文，FR-28 约定） | diff 确认 → 保存 toast |
| Set Me Up 向导 | 指令区骨架 | 零仓库 → 提示先建仓（不给空指令） | 口令失验 → 内联 `Incorrect password` 语义（服务端原文），不出对话框；401 step_up_invalid 同形 | 「令牌已生成」面板 + Copy + 指令块 |
| Deploy 上传对话框 | 行级进度 | 拖拽区引导态 | 409 双值 / 403 权限指引 / 网络中断重试 | checksum 比对徽标 + 201 |
| 存储概要 | 卡片独立骨架 | 空实例：0 值 + 建仓引导 | stats 403 → L3 卡片隐藏；失败卡 + 重试 | Refresh 即时重取 |
| 危险确认 | 确认按钮 spinner | — | 失败留在对话框 + 行内错误 | 对话框关 + toast + 列表刷新 |

### 3.3 组件规范（M8 组件清单 = 14 个）

| # | 组件 | 行为要点（对齐源 = reverse §5 矩阵） |
|---|---|---|
| C1 | 数据表 | 列头点击排序（asc/desc 图标）；行内 hover 动作（仓库表 = 删除图标）；底部计数行「显示 a – b / 共 c 项」+「加载更多」增量（**不采纳页码跳页**——R1 keyset 游标契约，记录为有意分歧）；行 = 链接（Enter 进入）；>500 行前端虚拟化 |
| C2 | 跨仓树 | 仓库为顶层节点（rclass×packageType 图标区分）+ 懒加载一层 + 单击选中右联动 + URL 同步 + 深链自动展开祖先；「过滤仓库」输入 + Clear（前端过滤已加载集，提示边界）；虚拟滚动（长树只渲染可视区） |
| C3 | 右键上下文菜单 | 文件：复制路径 / 下载 / 删除；文件夹：复制路径 / 删除 / 刷新；仓库：复制仓库路径 / 刷新 / 在仓库管理中打开。**Move/Copy（跨路径移动制品）不建**——服务端无端点，契约零改动。键盘可达（Shift+F10 / Menu 键打开） |
| C4 | 详情面板（Tab 式） | 仓库/文件夹/文件三形态 Tab：`常规`（字段序对齐 reverse §3.2：Name、包类型、Repository Path、File URL、布局、计数/大小（Show 展开）、Created）；`有效权限`（`?permissions` 视图，admin 渲染）；文件附加块：Checksums（sha256/sha1/md5，各带「（上传时提供：一致）」徽标——映射 originalChecksums 比对）、maven 的 GAV/声明片段、virtual 的成员来源列。`Properties`/`Followers`/`Xray` Tab 不建（无后端/Non-goal） |
| C5 | 双列穿梭 | Available N / Selected N + 空侧「未选择项」；用于：用户↔组、权限编辑器选仓库、（未来）备份仓库集 |
| C6 | 分步/两步对话框 | 步骤号 + 步骤标题；权限资源两步（选仓库 → 可选 patterns）；Set Me Up 包类型网格一步 |
| C7 | 包类型选择网格 | 新建仓库进页即弹；**5 项**（Generic/Docker/Maven/npm/PyPI）单选网格 + 图标 + 说明——不照搬 33 项全集 |
| C8 | 编辑器表单骨架 | 分区卡片（Section 标题 + 字段网格）；必填 blur 即校验、错误在字段下方；必填未满足时主按钮**禁用**（对齐 Artifactory 新建用户行为）；底部 `取消 / 重置 / 保存`（新建态主按钮 = `创建 …`） |
| C9 | 危险确认对话框 | 标题 + 后果说明 + Cancel/确认（danger 色）；焦点陷阱 + Esc；BinFlow 分级保留：删仓/GC apply/导入 = 额外**输入 key 确认** + 影响面摘要（P5 原则，Artifactory 无输入确认——有意增强） |
| C10 | badge | 语义色 15% 透明底 + 文字（≥4.5:1）；rclass/packageType 用中性 badge；assumed-offline 用 warning |
| C11 | mono 值 + 拷贝 | 沿 console-ux §7.3 全清单（路径/digest/checksum/repo key/tag/GAV/URL/命令/token/pattern 一律 mono）；>20 字符配拷贝按钮；截断展示拷贝完整值 |
| C12 | 骨架屏 / C13 错误卡+EmptyState / C14 toast | 现役组件原样沿用（`Skeleton.tsx` / `ErrorCard.tsx` / `EmptyState.tsx` / `ToastContext`），仅皮肤换 token |

### 3.4 键盘清单

| 键 | 行为 |
|---|---|
| Tab / Shift+Tab | 全页面自然序；对话框焦点陷阱 |
| Esc | 关闭对话框/菜单/浮层 |
| ↑ ↓ | 树节点移动；表格行移动 |
| → / ← | 树节点展开 / 收起 |
| Enter | 行/节点激活（进编辑器或详情）；菜单项确认 |
| Shift+F10 / Menu | 打开右键上下文菜单 |
| ⌘K / Ctrl+K / `/`（非输入态） | 全局搜索（BinFlow 自有保留项；Artifactory 无全局快捷键——不冲突的增强；modal 打开时让位，沿现役实现） |

### 3.5 批量操作清单

**无批量选择**（对齐 reverse §5「未观察到复选框列」且 BinFlow 无批量端点）：仓库/用户/组/权限/制品列表均不设复选框列；删除逐行进行；批量回收 = GC 页语义。此为对齐项而非裁剪项——**不得**为「看起来更强」添加批量 UI。

---

## 4. 关键交互流的 BinFlow 落地形态

### 4.1 Set Me Up（客户端接入向导，对齐 reverse §4.1）

```
入口：树页头 [Set Me Up] 按钮（选中仓库预选）｜用户菜单 → 快速建仓 → Set Me Up
  ↓
[步骤 0] 包类型网格对话框「选择客户端类型」
         · 只列**已有仓库的包类型**（对齐 Artifactory 行为：集合 = 实例内仓的包类型并集）
         · 零仓库 → 空态提示先建仓
  ↓
[主对话框]「配置 <PackageType> 客户端」  Tab: 配置 Configure ｜ 部署 Deploy
         · 仓库下拉（预选当前仓，仅该包类型仓）
         · [仅 admin 渲染] 口令框 + 「生成令牌并创建指引」
             - 错误口令 → 内联错误（服务端原文，等价 "Incorrect password"），不出对话框
             - step-up 开启时 401 step_up_required → 口令框聚焦重输（§7.4）
             - 成功 → 「令牌已生成」面板：token 明文（mono + Copy + 「关闭后不可再查看」）+ 24h 过期提示
         · 指令区（按客户端）：docker login / settings.xml / .npmrc / pip.conf / curl 片段
             - 内容与 docs/user/ 接入文档同源（UI 不发明命令）；每块独立 Copy
             - 非 admin：凭据位给 <USERNAME> / <TOKEN 或口令> 占位（D3：非 admin 自铸分支关闭——不渲染生成区）
  ↓
[完成] 关闭
```

约束：全程现役端点（`POST /api/security/token`）编排，契约零改动；指令内容源 = 仓库详情「接入命令」块（P3 命令优先原则的对话框化）。

### 4.2 Deploy（UI 上传，对齐 reverse §4.2）

对话框字段序：目标仓库（下拉）→ 包类型（只读回显）→ 部署模式（单个/多个）→ 拖拽区（`拖拽文件到此处` 或 `选择文件`）→ 目标路径（mono 可编辑 + Copy）→ `部署`。BinFlow 既有增强全保留：流式 sha256 预计算 + `X-Checksum-Sha256`、行级进度/速度、409 双值（received/actual）展示、403 权限指引、maven 表单生成路径（GAV 五输入 + layout 预检）、docker/npm/pypi 不出现上传入口（以接入命令块替代）。

### 4.3 树导航与深链（对齐 reverse §4.3/§4.4）

URL 即状态（`/artifacts/<repo>/<path>`）；深链自动展开祖先并选中；懒加载展开；过滤仓库输入 + Clear 复位。Trash Can 常驻节点**不建**（BinFlow 无回收站——删除即永久，危险确认文案明示）；My Favorites/星标**不建**（无后端）；Compacted/Non-Compacted 切换**不建**（懒加载一层 + 虚拟滚动已解决规模问题）。

### 4.4 新建仓库向导（对齐 reverse §4.7）

进页弹包类型网格（5 项，必选）→ 表单分区：**常规设置**（Repository Key\*〔blur 校验、规则 `[a-z][a-z0-9-]{1,62}` 前端预检 + 服务端终裁〕、Environments 不建）→ **来源/成员**（remote：上游 URL\*、凭据、□ 允许私网上游〔勾选即黄条 + 审计提示〕；virtual：成员多选 + 解析序 + 默认部署仓）→ **包类型专属**（maven 校验策略/snapshot 行为/handle 开关…）→ **高级**（TTL/超时/hardFail/优先解析…，字段集 = 现役表单不动）→ 底部 `取消 / 创建 <Rclass> 仓库`（必填未满足禁用）。编辑态：rclass/包类型锁定，主按钮 `保存`。BinFlow 分步表单收敛为**单页分区式**（对齐 Artifactory 形态）；侧栏实时摘要保留为页面右栏（增强，不冲突）。

### 4.5 权限两步资源对话框（对齐 reverse §4.9）

编辑器区块 `名称 / 资源 / 用户 / 组`（单页）；`编辑仓库` 按钮 → 两步对话框：① 选仓库（双列穿梭；预置 `任意本地仓`/`任意远程仓` 语义桶——映射 BinFlow repos[] 通配 `**` 约定，仅当契约支持时渲染，否则列实体仓）→ ② 可选 patterns（include/exclude 逐行 chip）→ `确定` 回填 → `保存`。BinFlow 增强保留：**模式测试器**（置于 patterns 区块内，输入路径即时显示逐条命中与最终判定，R8 同源保证）与**保存前 diff 确认**（安全面变更逐条列出）。动作列 = `read / write / delete / manage` 四列（Artifactory 五列裁剪：Annotate 不建；manage 为 M7 扩展，§7.2）。

### 4.6 删除确认（对齐 reverse §4.8 模式）

统一模式：**标题 + 后果说明 + Cancel/确认（danger）**。BinFlow 文案基线（语义对齐、中文化）：

| 对象 | 标题/后果 | 强度 |
|---|---|---|
| 用户 | 「确定要移除该用户吗？此操作不可撤销。」 | 双按钮 |
| 组 | 同上 + 「将解除 N 个成员的关联」 | 双按钮 |
| 权限 target | 「删除后其授权立即失效（影响 N 个仓库）」 | 双按钮 |
| 仓库 | 「将永久删除 `<key>` 仓库及其全部制品。」 + `□ 同时删除内容`（非空必勾，对齐 deleteContent 400 契约）+ **输入 key 确认** | 强确认 |
| 制品文件/目录 | 「将永久删除 `<path>`。制品不可变，删除无撤销。」 | 双按钮；docker by-digest 删除列明受影响 tags |
| GC apply / 导入 | 输入确认 + 影响面摘要 | 强确认 |

### 4.7 表格排序/分页、表单校验、会话过期

- 排序：列头点击循环 asc/desc/none；多列不建。
- 分页：计数行文案对齐（「显示 a – b / 共 c 项」）+ 加载更多（§3.3 C1 有意分歧注记）。
- 校验：blur 触发、字段下文案（「请填写此字段」= "You must fill in this field" 语义等价）；必填未满足主按钮禁用；`重置` 恢复初始值（有 Reset 的表单）。
- 会话过期：任意页 → toast + `/login?return=<原路由>`，登录后回跳（对齐 reverse §4.13）。

---

## 5. 设计 token（自有皮肤）

交付形态不变：CSS custom properties（`--bf-*`）一套语义 × 两主题。**取值为 BinFlow 自有**（近似 Artifactory 的信息密度与灰阶节奏：侧栏最暗、内容底次之、卡片最亮的三阶纵深；小圆角；密表格），与现役 GitHub 系取值整体偏移（冷石墨 + 自有蓝 accent），落地 = 换值 + 增补侧栏/阴影 token，组件结构零改。

### 5.1 色板

```
暗色（默认）                              亮色
--bf-sidebar:       #0b0e13              #1b2430     侧栏（三阶最暗）
--bf-bg:            #12161d              #f3f5f7     内容底
--bf-surface-1:     #1a202a              #ffffff     卡片/表格
--bf-surface-2:     #202834              #e9edf1     悬停行/次级面板
--bf-surface-3:     #262f3d              #dfe5ec     输入框/drop zone
--bf-border:        #2b3442              #d3dae2     分隔线/表格线
--bf-border-strong: #414c5e              #9aa6b4     悬停边框
--bf-text:          #e9edf3              #1d232c     正文（≥12:1）
--bf-text-2:        #9aa4b2              #55606e     次要（≥4.5:1）
--bf-text-muted:    #75808f              #7a8492     弱化（时间戳等）
--bf-accent:        #4aa3ff              #0b6bcb     主操作/链接/焦点
--bf-accent-fg:     #0b0e13              #ffffff     accent 上文字
--bf-success:       #3fbf6f              #157f3d
--bf-warning:       #d9a23a              #946200
--bf-danger:        #f0605d              #c9372f
--bf-info:          #6cb2ff              #0b6bcb
```

语义色使用规则沿 console-ux §7.1（四色只作状态表达；badge = 15% 透明底；danger 保留错误/删除/危险区；assumed-offline 用 warning；rclass badge 中性）。对比度按 §8 清单两主题分别核验（QA 必测项）。

### 5.2 间距 / 字号 / 圆角 / 密度 / 阴影 / 层级

```
间距（4px 基数）：--bf-sp-1:4  -2:8  -3:12  -4:16  -5:24  -6:32  -7:48
字号：  --bf-fs-aux:12（辅助列/标签）  -body:13（表格正文·密度主字号）
        -form:14（表单/段落）  -h3:16（卡片标题/tab）  -h2:20（页面标题）  -h1:24（登录品牌）
行高：正文 1.5 / 标题 1.25
圆角：  --bf-r-sm:4（输入/badge）  -md:6（按钮/卡片）  -lg:8（modal/drop zone）
密度：  表格行高 32px；导航项 32px；控件高 32px；内容区左右 padding 24px；列间距 12px
        危险区：1px danger 边框 + sp-4 内距
阴影（暗色弱影 + 边框补偿；亮色实影）：
        --bf-shadow-1: 0 1px 2px rgba(0,0,0,.35)      （卡片/下拉锚定）
        --bf-shadow-2: 0 4px 12px rgba(0,0,0,.45)     （下拉/浮层/右键菜单）
        --bf-shadow-3: 0 12px 32px rgba(0,0,0,.55)    （modal/向导）
层级：  z-toast 100 / z-modal 90 / z-dropdown 80 / z-nav-sticky 70
```

mono 应用规则沿 console-ux §7.3 全清单不变（路径/digest/checksum/repo key/tag/GAV/URL/命令块/token/pattern/时间戳一律 mono + 可复制）。

---

## 6. 页面级线框（逐页）

> 标注 `[n]` 对应框下说明；四态差异见 §3.2，仅画默认态。全局壳（§2.1）不再重复。

### 6.1 登录 `/login`

```
┌──────────────────────────────────────────────┐
│              BinFlow ◆                        │ [1] 品牌区（独立布局，无侧栏壳）
│           制 品 仓 库 控 制 台                 │
│   ┌──────────────────────────────────────┐   │
│   │ 用户名          [__________________] │   │ [2] autofocus，autocomplete=username
│   │ 密 码          [__________________] │   │ [3] type=password
│   │ (错误行：用户名或密码错误 [4])          │   │
│   │         [ 登 录 ]                     │   │ [5] 两字段非空才可用；提交中 spinner
│   └──────────────────────────────────────┘   │
│   管理面需认证。CI 与脚本请使用 API Token。     │ [6] 常驻说明 + 文档链接
└──────────────────────────────────────────────┘
```

对齐 reverse §2：居中大标题形态、单 submit 按钮、失败停留本页行内错误、无 Remember me / Forgot password。会话过期任意页重定向回此页（带 return）。

### 6.2 仪表盘 `/dashboard`

```
┌ 仪表盘 ─────────────────────────────────────────────────────────────────────┐
│ [1] 健康          [2] 存储                                [3] 仓库            │
│ ┌────────────┐   ┌──────────────────────────┐          ┌──────────────────┐ │
│ │ ● ok       │   │ blob 12,483 · 逻辑 842GB  │          │ Local 6 / Remote 3│ │
│ │ storage ok │   │ 物理 311GB · 去重率 63%    │          │ Virtual 2  [建仓→]│ │
│ └────────────┘   └──────────────────────────┘          └──────────────────┘ │
│ [4] Remote 状态（仅列非健康项；全健康收起为一行）                                │
│ [5] 最近审计（最新 8 条，行点击进对象）                        [查看全部 →]    │
└─────────────────────────────────────────────────────────────────────────────┘
```

保留 BinFlow 五卡形态（reverse 未深走 Dashboard，无对齐负担）；非 admin：[1][2][3][5] L3 隐藏、[4] 隐藏，仅实例卡 + 收敛说明（§3.6.4 姿势不变）。

### 6.3 制品浏览器 `/artifacts/<repo>/<path>`（核心页）

```
┌ 制品 ───────────────────────────────────────────────────────────────────────┐
│ [Set Me Up] [部署 Deploy] [管理仓库→]     过滤仓库 [__________] [清除]         │ [1]
├───────────────┬─────────────────────────────────────────────────────────────┤
│ 树 (300px)     │ 详情面板（右联）                                             │
│ ▾ ◼ docker-   │ ┌─ 常规 │ 有效权限 ───────────────────────────────────────┐ │ [2]
│    local   ◀  │ │ 名称 app-1.0.tar.gz        包类型 Generic               │ │
│ ▸ ◼ maven-    │ │ Repository Path  docker-local/acme/app-1.0.tar.gz [⧉]  │ │ [3]
│    remote     │ │ File URL https://…/binflow/docker-local/acme/… [⧉]      │ │
│ ▸ ◼ maven-    │ │ 大小 12 MB · 部署者 ci-bot · 修改 2 小时前               │ │
│    virtual    │ ├ Checksums ─────────────────────────────────────────────┤ │
│   (懒加载：    │ │ sha256 a3f5…9c2e [⧉] (上传时提供：一致 ✓)               │ │ [4]
│    点开拉一层) │ │ sha1   77d0…41   [⧉]   md5 …                 [⧉]       │ │
│               │ └─────────────────────────────────────────────────────────┘ │
│               │ 当前层 children 表（名称/类型/大小/修改/操作者/操作）          │ [5]
│               │                    [加载更多 (100/214)]                     │
├───────────────┴─────────────────────────────────────────────────────────────┤
│ 已服务 12,483 个制品                                                          │ [6]
└─────────────────────────────────────────────────────────────────────────────┘
右键菜单 [7]：文件=复制路径/下载/删除  文件夹=复制路径/删除/刷新  仓库=复制仓库路径/刷新/在仓库管理中打开
```

- [1] 页头动作区对齐 reverse §3.2；[2] 详情 Tab 与字段序对齐（Properties/Followers/Xray 不建）；[3] 路径/URL mono+拷贝（P2）；[4] `(上传时提供：一致)` 映射 originalChecksums；[5] 分页沿 §6 大目录策略；[6] 页脚标语行（stats admin 门，非 admin 隐藏）。
- 协议特化（docker 两级/tag 表/manifest 面板、maven GAV 树、npm 包/版本、pypi 归一名）沿 console-ux §3.4 矩阵不变，挂载点从「仓内树」平移为「跨仓树子树」。
- 非 admin：树顶层 L2 无权限卡 + 搜索/直链引导；深链子树按路径 ACL。

### 6.4 搜索结果 `/search`

```
┌ 搜索制品 ────────────────────────────────────────────────────────────────────┐
│ 搜索结果 – 1,203 项                                                            │ [1] 计数副标
│ [⌕ acme____________]  筛选：仓库 [全部▾] 包类型 [全部▾] 仓型 [全部▾]           │
│ ├──────┬──────────┬──────────────────────────────┬────────┬───────────────┤
│ │ 类型  │ 仓库      │ 路径 / 名称（+语义副行）        │ 版本    │ 修改时间        │
│ │ 🐳   │ docker-   │ acme/app (tags: v1.0.3 …)     │ v1.0.3 │ 2 小时前        │
│ └──────┴──────────┴──────────────────────────────┴────────┴───────────────┘
│                                   行点击 → /artifacts/<repo>/<path> 深链 [2]  │
│                                    [加载更多 (100/1,203)]                     │
└─────────────────────────────────────────────────────────────────────────────┘
```

对齐 reverse §3.3（标题/计数副标/四列/计数分页行）+ BinFlow 语义副行增强；**计数与行数一致性为验收项**（reverse 观察到 Artifactory 此处有显示怪癖——不对齐缺陷）。空关键词为引导态不查询。

### 6.5 编辑档案 `/profile`

```
┌ 编辑档案 ────────────────────────────────────────────────────────────────────┐
│ 认证设置                                                                       │
│   修改口令：当前口令 / 新口令 / 确认 [提交]                                      │ [1]
│ API Token                                                                      │
│   CI 与脚本请使用 API Token（管理面签发需管理员）。 [查看文档] [去 Tokens 页→]     │ [2]
└──────────────────────────────────────────────────────────────────────────────┘
```

[1] 从设置页平移（authenticated 可用）；[2] Identity Tokens 表不建（R6 未落地；readonly_admin 自铸 200 但管理面 Tokens 页 admin 门——按现役门呈现入口）。

### 6.6 仓库管理 `/admin/repositories/{local|remote|virtual}`

```
┌ 仓库 / 全部 ─────────────────────────────────────────────────────────────────┐
│ [Local] [Remote] [Virtual]                              5 个仓库  [+ 添加仓库] │ [1]
│ 过滤 key [__________]                                                          │
│ ├──────────────┬─────────┬─────────┬────────────┬──────────┬────────────────┤
│ │ Repository Key│ 类型     │ 包类型   │ 制品/缓存   │ 大小 🗑    │ 更新时间        │ [2]
│ ├──────────────┼─────────┼─────────┼────────────┼──────────┼────────────────┤
│ │ docker-local │ Local   │ Docker  │ 1,204      │ 412 GB   │ 2 小时前        │
│ │ maven-remote │ Remote⚠ │ Maven   │ 8,391      │ 220 GB   │ 5 分钟前        │
│ └──────────────┴─────────┴─────────┴────────────┴──────────┴────────────────┘
│ 显示 1 – 5 / 共 5 项                                    （列头点击排序 [3]）    │
└──────────────────────────────────────────────────────────────────────────────┘
```

[1] Tab = 子路由；计数标题 + 右上添加按钮对齐；[2] key 列 mono + 链接进编辑页，行尾 hover 垃圾桶 = 删除入口（确认走 §4.6）；BinFlow 保留列（大小/更新/上游状态）合并呈现；[3] 列头排序。Replications/Shared With 列不建（全局复制页承载）。

### 6.7 新建/编辑仓库 `/admin/repositories/new|/:key/edit`

```
┌ 新建 Local 仓库 ─────────────────────────────────────────────────┬ 摘要 ──────┐
│ ┌ 选择包类型（进页即弹，必选 [1]）────────────────────────────┐  │ key  …     │
│ │ [Generic] [Docker] [Maven] [npm] [PyPI]                     │  │ rclass …   │
│ └─────────────────────────────────────────────────────────────┘  │ url  …     │
│ 常规设置：Repository Key* [________] · 描述 [________]             │ TTL  …     │
│ 来源（Remote）：上游 URL* · 凭据 · □ 允许私网上游 ⚠               │ （实时）     │
│ 成员（Virtual）：多选 + 解析序 · 默认部署仓 [（未配置）▾]           │              │
│ 包类型专属：校验策略 / snapshot 行为 / handle 开关 …                │              │
│ 高级：TTL / 超时 / hardFail / 优先解析 …                            │              │
│                                    [取消] [重置] [创建 Local 仓库]  │              │
└─────────────────────────────────────────────────────────────────────────────┘
```

字段集 = 现役建仓表单契约（零改动），排布与按钮文案对齐 §4.4；编辑态 rclass/包类型锁定、主按钮 `保存`。

### 6.8 仓库详情 `/admin/repositories/:key`（BinFlow 自有增强页）

沿 console-ux §4.5 形态：头部 key + rclass/packageType badge + URL 拷贝；接入命令折叠面板（P3）/ 统计卡 / 本仓最近事件 / remote 特化（上游健康/assumed-offline 倒计时/命中率）/ virtual 特化（成员解析序可视化）/ 危险区（删仓强确认）。

### 6.9 用户 `/admin/security/users[/:name]`

```
列表：  [+ 新建用户]   列：Name │ Email │ Groups(计数|悬浮明细) │ Role │ Status    [1]
        底部「用户总数： N」；行 → /:name/edit
编辑：  ┌ 用户设置：User Name（编辑态锁定）· Email · 角色 [user ▾]（三值，仅 admin 可改 [2]）
        ├ 选项：□ 启用（enabled 翻转走全量体 [3]）
        ├ 口令 / 确认口令（编辑态留空 = 不改）
        ├ 相关组：Available N ─┨┠→ Selected N（双列穿梭 [4]）
        ├ 用户权限矩阵：列 = read │ write │ delete │ manage（只读汇总，来源 = 各 target [5]）
        └ [取消] [重置] [保存]   编辑页右上 Actions ▾ → 删除用户（§4.6 确认）
```

[1] Realm/Last Login/Admin 布尔列不建（本地 realm 恒定、无 lastLogin 字段；Role 列承载三值 badge）；[2] 角色下拉 = M7 FR-66 语义（§7.1）；[4] 穿梭对齐 Artifactory Related Groups；[5] 矩阵对齐 Artifactory User Permissions Tab（Builds/Release Bundles Tab 不建）。

### 6.10 组 `/admin/security/groups[/:name]`

列表：`[+ 新建组]`，列：Name │ 权限数 │ 成员数。表单：组设置（名称/描述）+ 成员穿梭（Available/Selected）+ 编辑态组权限矩阵（四动作列）。External ID / Auto Join 不建（无外部组模型）。

### 6.11 权限 `/admin/security/permissions[/new|/:name]`

```
列表：  [+ 新建权限]   列：Permission Name │ 仓库数 │ patterns 数 │ 用户数 │ 组数
编辑器：┌ [1] 名称（编辑态锁定）
        ├ [2] 资源：[✕generic-local] [✕npm-local] [+ 编辑仓库…]  → 两步对话框：
        │        ① 选仓库（双列穿梭） → ② 设置模式（可选，include/exclude chip）→ 确定
        │        模式测试器 [输入路径] [测试] → 逐条命中 + 最终判定（BinFlow 保留 [4]）
        ├ [3] 用户 / [3] 组：主体 + 动作矩阵（read ☑ │ write ☑ │ delete ☐ │ manage ☐ [5]）
        │        [+ 添加用户 ▾] [+ 添加组 ▾]   ⓘ admin 隐式全权，不列入
        └ [取消] [保存]  → 保存前 diff 确认（BinFlow 保留 [6]）
```

对齐 reverse §3.8 单页分区 + 两步对话框；BinFlow 保留测试器与 diff（安全面增强）。

### 6.12 Access Tokens `/admin/security/tokens`（P2）

占位页（R6 未落地）：说明 + 签发引导（`POST /api/security/token` 文档）+ 输入 token_id 吊销。落地后升 Artifactory 形态（Generate Token 表单 + 表：Description/Token ID/Issued/Expiry + 撤销）。

### 6.13 审计 `/admin/governance/audit`

沿 console-ux §4.10：过滤行（时间窗/操作者/仓库/动作/路径）+ 表（时间/操作者/动作〔enum 原样〕/对象 mono/来源）+ 加载更多 + 导出 CSV（R3）。动作闭集含 M7 新增 `user.role.change`（detail 含 old/new 角色，行展开显示）。

### 6.14 维护（GC）`/admin/governance/gc`

对齐 reverse §3.9 分块骨架（自上而下）：

```
┌ 维护 ────────────────────────────────────────────────────────────────────────┐
│ ① 垃圾回收 GC：状态（上次运行/回收量/grace）· [试运行 dry-run] · [执行 GC]（强确认）│
│ ② 存储迁移面板（本地→S3 只读进度，T-160 形态原样保留 [1]）                        │
│ ③ 危险区模式（P5）：红边框分区 + 影响面摘要 + 输入确认                            │
└──────────────────────────────────────────────────────────────────────────────┘
```

Artifactory 的 Cron 表达式/Next Run/配额百分比/清理虚拟仓分块**不建**（无对应端点——BinFlow GC 是手动 dry-run/apply 契约）；配额移独立页（§6.15）。

### 6.15 配额 `/admin/governance/quotas`

沿现役形态：每仓一行（key/已用/上限/水位条，>80% 黄 >100% 红）+ 行内编辑上限 + 总量行。manage 持有者可见其覆盖仓用量（FR-65 派生 4——路由可见性按 403 收敛）。

### 6.16 复制 `/admin/governance/replication`

沿 T-159/T-180 定稿形态（§4.11 回写）：复制目标表（状态/目标/仓库/pending/进行中/失败/累计/上次成功）+ 最近事件表，10s 轮询、四态收敛（403 L2 / 404·501 降级提示 / 瞬断不清屏）。CRUD UI 另票（端点已就绪）。

### 6.17 备份 / 恢复 `/admin/governance/backup`

导出（任务进度 + 产物下载）/ 导入（文件 + 校验和确认 + 覆盖警示双重确认）双卡，对齐 Import & Export 卡式布局；Artifactory 计划备份列表/表单不建（无 cron 备份端点）。

### 6.18 存储概要 `/admin/monitoring/storage`（新页，现役端点编排）

```
┌ 存储 ────────────────────────────────────────────────────────────────────────┐
│ 数据最近刷新于：<ts>  [刷新]                                                    │ [1]
│ [blob 大小/计数] [制品大小/计数] [优化率 %] [条目计数]          （汇总卡行 [2]）  │
│ ├──────────────┬─────────┬─────────┬──────┬─────────┬──────┬──────┬──────┤
│ │ TOTAL [3]     │ —       │ —       │ 100% │ 842 GB  │ …    │ …    │ …    │
│ │ docker-local │ Local   │ Docker  │ 49%  │ 412 GB  │ 812  │ 392  │ 1204 │
│ └──────────────┴─────────┴─────────┴──────┴─────────┴──────┴──────┴──────┘
│ 列：Repository Key │ 仓型 │ 包类型 │ 占比 │ 制品大小 │ 文件 │ 目录 │ 条目         │
└──────────────────────────────────────────────────────────────────────────────┘
```

数据 = `/api/v1/storage/stats` + 逐仓 `/api/v1/storage/usage/{key}`（仓库数大时串行拉取 + 进度提示；>50 仓给「部分数据」标注）。对齐 reverse §3.11 骨架（刷新行/汇总卡/TOTAL 首行/列序）。

### 6.19 系统信息 `/admin/general/settings`

沿现役设置页只读面：版本/修订、健康行（403 驱动 L3）、匿名读开关状态、数据目录、日志级别；Server Name / Base URL 只读展示（YAML 配置）。写入口（Logo/Custom Message 等）不建。改密移 `/profile`。

---

## 7. M7 交互语义保留清单（新 IA 融合方式）

| # | M7 语义（出处） | M8 融合方式 |
|---|---|---|
| 7.1 | **角色三值下拉** `user / readonly_admin / admin`（FR-64/FR-66；wire `adminRole`） | 用户编辑表单「用户设置」区块内的 `角色` select（§6.9[2]）；仅 admin 可见可改，readonly_admin 视角禁用 + 说明行；列表 Role 列 badge 呈现 |
| 7.2 | **manage 复选**（FR-65）：动作集 `read/write/delete/manage`，manage = 仓库配置派生权 | 权限编辑器与用户/组权限矩阵的第 4 动作列（§6.11[5]）；矩阵头 tooltip 说明「manage 不隐含读写删」；用户/组详情的权限汇总矩阵同步四列 |
| 7.3 | **readonly_admin 只读态**（FR-64 Q2）：读面全通（11 GET 端点）、写面 403、数据面全域只读 | 侧栏「管理」模式入口对 readonly_admin 可见（AppShell 现役扩展保留）；会话徽章「只读」；各管理页写入口禁用 + 「服务端 403 兜底」说明（现役 users-readonly-note 模式推广到全部管理页）；提交后如实呈现 403 服务端原文（FR-28 约定） |
| 7.4 | **step-up**（FR-68，ADR-0027）：`auth.token_step_up` 开启时 web session 非 admin 铸 token 需 `step_up_password` | Set Me Up 向导与 Tokens 页的铸币路径：401 `step_up_required` → 对话框内口令框聚焦；`step_up_invalid` → 内联错误（等价 Incorrect password 形态）；Basic/Token 臂与 admin 臂不受限（UI 无感） |
| 7.5 | **PUT replace 语义**（FR-64 臂注记）：角色变更/enabled 翻转须 PUT 全量体（email+password）或 POST 部分更新臂 | 用户表单提交姿势不变：前端按服务端臂契约组包（编辑 = 全量 replace 体；`enabled` 翻转同）——纯调用层约定，UI 形态无感 |
| 7.6 | **403 收敛四层 + whoami 预收敛**（console-ux §3.6，M7 扩展 readonly_admin） | 全部新页遵守 §2.2 表；admin 位仅用于 L1/L4 预收敛，数据面一律 403 驱动；readonly_admin ⊆ admin 可见面 |
| 7.7 | **续传语义**（FR-67） | 控制台无上传会话面（docker blob 走客户端）；Deploy 对话框为单体 PUT，与续传无交集——零影响，登记防回归 |
| 7.8 | **审计 `user.role.change`** | 审计页动作过滤闭集含该值；行展开显示 old/new 角色（§6.13） |
| 7.9 | **会话过期重定向**（ADR-0028 语境） | 沿 §4.7：401 → toast + `/login?return=`（新路由同样适用） |
| 7.10 | **manage 派生的可见面**（单仓配置/permissions 覆盖集/`?permissions`/配额用量） | manage 持有者（非 admin）：仓库详情直链可达（单仓 GET 200）、权限编辑器可达（覆盖集校验在服务端）、树详情「有效权限」Tab 渲染、配额页其覆盖仓行可见——全部 403 驱动呈现，UI 不自行判定覆盖集 |

---

## 8. M8 债券承接（UI 相关）

| 债券 | 本规格承接方式 |
|---|---|
| T-231 上传路径 percent-encode 缺陷（% / # / ? / 空格 / UTF-8） | 制品树/Deploy 的**行为规格约束**：节点名展示用服务端原值（不二次编解码）；URL 即状态的路由段用 encodeURI 每段编码；Deploy 目标路径输入框同时显示原值（可编辑）与请求编码值（只读 mono 回显）；上传成功后树定位按编码 URL 深链回解。回归矩阵（%/#/?/空格/中文文件名）进 qa 断言清单 |
| UI 打磨 4 条 | 并入本规格各页四态/密度/对比度验收项（qa 按 §3.2 矩阵 + §5 token 核验） |
| 其余（B-1 迁移降级 / CI timeout / V28 附录 / ROADMAP 勾账 / dialer 样板） | 非 UI 面，本文件不涉及（conductor 另派） |

---

## 9. 可达性（沿 console-ux §8 + M8 增补）

沿全清单：对比度（正文 ≥7:1、次要/语义 ≥4.5:1 两主题核验）、`:focus-visible` 2px accent、树方向键、对话框焦点陷阱 + Esc、语义标签（nav/main/table scope/图标按钮 aria-label——拷贝按钮标注被拷对象）、状态不以颜色为唯一信号、`prefers-reduced-motion` 禁骨架动画、`lang="zh-CN"` + mono 值 `lang="en"`。增补：右键菜单键盘可达（Shift+F10，§3.4）；双模式侧栏切换项是 `<button>` 并带 `aria-current`；Tab 组件用 `role="tablist/tab"` + 方向键切换。

---

## 10. 验收要点（供 qa 拆解，DoD 体例参照 milestone-7 §9）

1. §1.3 导航树 14 条目逐项可达；旧路由 20 条重定向全绿（e2e 兼容窗口）。
2. §1.2「不建」清单零影子入口（侧栏/用户菜单/右键菜单 grep 断言）。
3. §3.2 四态矩阵逐格 Playwright 断言（复用 §10 锚体系，新增页先补锚再落码）。
4. §4 六条交互流走查（Set Me Up 含错误口令/step-up 双态；Deploy 含 409/403；权限两步对话框回填）。
5. §7 十条 M7 语义回归（V12~V14 序列复跑 + 新 IA 下角色下拉/manage 复选/readonly 只读态断言）。
6. §5 token 两主题对比度全组合核验；T-231 编码矩阵（%/#/?/空格/UTF-8）在树与 Deploy 双面通过。
