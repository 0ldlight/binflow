# BinFlow 控制台 × JFrog Artifactory 交互对齐规格（console-artifactory-parity）

| 项 | 值 |
|---|---|
| 文档 | `docs/design/console-artifactory-parity.md` |
| 票据 | UX-1（插空票：品牌资产 + Artifactory 交互对齐规格） |
| 状态 | v1.0（2026-08-30） |
| 维护者 | ux-designer |
| 上游依据 | 用户指令 2026-08-30（① 前端交互体验与 JFrog Artifactory 完全一致，含弹窗、抽屉等）；`docs/design/console-ux.md` v1.15（IA/四态/token 母册）；`docs/design/mui-native-visual.md`（MUI 原生视觉基线）；`web/src/` 现状逐一核对（见各模式的「BinFlow 载体」列） |
| 下游消费者 | **PM——M14 UI-parity PRD 直接引用 §7 差距矩阵**；FE 拆票；qa-engineer 验收 |
| 置信度声明 | Artifactory 7.x 行为描述基于本票作者的产品知识（clean-room：无反编译 UI 代码消费）；本票无浏览器，**中/低置信项一律列入 §8 活体核验清单**，核验前不得作为验收断言 |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-30 | UX-1 初版：交互模式 catalog（导航 N1~N3 / 弹窗 M1~M4 / 抽屉 D1~D3 / 列表 L1~L4 / 反馈 F1~F3），逐项置信度标注 + MUI 映射 + BinFlow 载体对照；§7 页面×模式差距矩阵；§8 活体核验清单；§9 不做清单；§10 落地批次建议 |

## 1. 目标、边界与置信度标尺

### 1.1 对齐什么

**交互形态对齐**：弹窗/抽屉的品种、出现位置、尺寸档、关闭方式、按钮位、步骤结构——迁移用户（Artifactory 老手）在 BinFlow 里「不用重新学操作手势」。**不是**像素复刻、**不是**功能对齐（BinFlow 没有 Builds/Federation/Lifecycles，也永远不做 Xray——见 §9）。

### 1.2 置信度标尺（每项 Artifactory 行为都标）

| 标 | 含义 | 处置 |
|---|---|---|
| **高** | 7.x 多版本稳定存在、我有把握 | 可直接进 PRD 验收标准 |
| **中** | 方向确定、细节（尺寸/文案/分组名）凭记忆 | 进 PRD 时标注「以核验为准」 |
| **低** | 凭记忆推断，可能是版本差异或记错 | 只进 §8 核验清单，**不进** PRD 断言 |

### 1.3 差距三档

- ✅ **已有**：现载体与 Artifactory 形态一致（或 BinFlow 形态更优且属有意设计，注记豁免理由）；
- △ **需改造**：载体存在但形态不同，需 FE 票改造；
- ✗ **缺失**：无载体，需新建（并过 PM 立项判断）。

### 1.4 不可倒退项（对齐的边界）

BinFlow 已有的**安全设计优于 Artifactory** 的地方不因对齐而倒退（详见 §9）：删除类动作收危险区/二次确认、token 明文仅展示一次、403 分层收敛、中文 UI 文案 + Artifactory 英文术语保留（console-ux §1.2 命名对齐条款）。

---

## 2. 全局导航与信息架构（N 系）

### N1 双域壳：Application / Administration

| | |
|---|---|
| Artifactory 行为（置信度：**高**） | 单侧栏两域切换：默认应用域（Welcome/Artifacts、Searches 等）；经用户菜单或顶栏入口进入 Administration 域（General / Repositories / Security / …分组），侧栏随域整体更换；顶栏恒有全局搜索、帮助、用户菜单。 |
| MUI 映射 | `Drawer(variant="permanent")` + `AppBar` + 两组 `List`（域切换换数据源） |
| BinFlow 载体 | `web/src/components/AppShell.tsx`——`APP_NAV`/`ADMIN_NAV` 双模式 + 侧栏底 `nav-mode-switch` 常驻切换项 + 顶栏搜索（真输入框 + 最近词）/帮助/主题/用户菜单 |
| 差距 | ✅ 已有（形态一致；BinFlow 的切换入口在侧栏底而 Artifactory 在用户菜单/顶栏——位置差异不影响肌肉记忆，**豁免不改**） |

### N2 侧栏分组与条目形态

| | |
|---|---|
| Artifactory 行为（置信度：**中**） | 分组标题（不可折叠的节标签）+ 条目；条目带小图标（low-medium 置信：图标有无随版本）；active 行高亮带左侧指示。 |
| MUI 映射 | `List` + `ListItemButton`（dense）+ `Typography variant="overline"` 分组标签 |
| BinFlow 载体 | `AppShell.tsx`——分组标签是节标签非折叠项（15 条目，console-ux §3.1 纪律）；active 行 = `--bf-sidebar-3` 底 + 2px primary 左指示条 |
| 差距 | △ 需改造（**低优先**）：条目加 16px 图标槽（mono 包型图标/通用符号；Artifactory 同款信息密度不受影响）。若 §8 核验发现当前主流版本侧栏无条目图标，则降级为「不做」。 |

### N3 顶栏面包屑与页面标题

| | |
|---|---|
| Artifactory 行为（置信度：**中**） | Administration 页顶部有层级面包屑（如 Administration → Repositories → <key>）；应用域制品树路径为面包屑式可点路径。 |
| MUI 映射 | `Breadcrumbs` + `Link` |
| BinFlow 载体 | `AppShell.tsx` `adminCrumbs()`——管理域五分组面包屑挂顶栏（`topbar-breadcrumb`）；制品树页内路径面包屑（`ArtifactsBrowser`） |
| 差距 | ✅ 已有 |

---

## 3. 弹窗族（M 系）——modal

**族通用规格（Artifactory 通行形态，置信度：高）**：居中 modal；标题栏右上 `X` 关闭；**动作按钮右下角，Cancel 在左、主按钮（Save/Create，primary）在右**；ESC = 取消关闭；点遮罩 = 取消关闭（危险确认除外，见 M2）；焦点入框、Tab 循环、关闭回焦启动元素。

BinFlow 族基座：`web/src/components/ConfirmDialog.tsx`（MUI `Dialog`，Esc/backdrop 双通道、`focusCancel` 回调 ref、`confirmDisabled` 前置）+ 各页 `Dialog` 消费——**族行为已对齐**，以下逐个实体。

### M1 New Repository 向导（建仓）

| | |
|---|---|
| Artifactory 行为 | ① 首屏：**全包型 logo 网格**（大图标 + 包名卡片，网格铺满 modal 宽）——置信度**高**；② 选包型后同 modal 内出现 local/remote/virtual 类型选择（顶部三 Tab 或分段控件——形态细节置信度**中**）；③ 表单分节（Basic Settings / Advanced…可折叠节），底部右 Cancel/Save——置信度**中**；modal 宽约 900px 级（置信度**低**，不进断言）。 |
| MUI 映射 | `Dialog(maxWidth="lg")` + 步骤 0 `radiogroup` 网格（现 `pkg-grid`）+ `Tabs`（rclass）+ `Accordion`/分节 `Paper` |
| BinFlow 载体 | `web/src/pages/repositories/RepositoryFormPage.tsx`——已是两段式：`pkg-grid` Dialog（选择包类型，带 `pkg-tier-*` 档位徽章）→ **路由页** `/admin/repositories/new` 单页分区表单（常规/来源/成员/策略/治理/高级六节 `Paper`）；rclass 由 URL query 预选 + 单选组。 |
| 差距 | △ **需改造（决策项 A，§10 批 1）**：Artifactory 全程在单 modal 内完成；BinFlow 是「网格 Dialog → 路由页」。建议改法：网格步与表单步收进**同一个 `Dialog(maxWidth="lg")`**（网格步 → 表单步在 modal 内切换），路由 `/admin/repositories/new` 保留为深链入口（进入即开该 Dialog，URL 仍可分享/回退）。编辑态 `/admin/:key/edit` 维持整页表单（编辑是长任务，整页更合理——与 Artifactory 的差异注记豁免）。 |

### M2 删除确认（危险确认族）

| | |
|---|---|
| Artifactory 行为（置信度：**高**） | 删仓库等破坏性操作弹居中小 modal，**输入 repository key 确认**；主按钮红色 danger 文案。 |
| MUI 映射 | `Dialog` + `TextField`（`confirm-input` 类钩子）+ `Button(color="error")` |
| BinFlow 载体 | `ConfirmDialog.tsx`（`danger` 红边 + `confirmDisabled` 前置）+ `RepoDeleteConfirm.tsx`（非空仓 `deleteContent` 复选 + 影响 N 制品摘要 + 输入 key）；GC apply/清空回收站同款输入确认。 |
| 差距 | ✅ 已有（BinFlow 语义更严谨：`deleteContent` 开关把 API 的 400 路径走通，豁免不改） |

### M3 实体表单（User / Group / Token / Permission Target）

| | |
|---|---|
| Artifactory 行为（置信度：**中**） | 建/编用户、组、token 以 modal 表单承载（字段分节，右下 Cancel/Save）；Access Token 创建成功后在结果面板展示一次性明文。 |
| MUI 映射 | `Dialog(maxWidth="sm")` + 分节表单；token 结果面板复用 SetMeUp 的 `smu-token-panel` 形态 |
| BinFlow 载体 | 用户：`UsersPage`（列表页内建表单）+ `UserDetailPage`（路由页编辑）；组：`GroupsPage`（同列表页形态）；权限：`PermissionEditorPage`（路由页——BinFlow 自有的模式测试器/diff 确认是增强，**豁免**）；Tokens：`PlaceholderPage`（P2 占位）。 |
| 差距 | △ **需改造（决策项 B，§10 批 1）**：用户/组的**创建**改 modal（`Dialog sm`，对齐 Artifactory 手势）；**编辑**保留路由页（BinFlow 有分组表单/穿梭列/成员矩阵等重内容，塞 modal 反而伤可用性——有意偏离，注记豁免）。Tokens 页 ✗ **缺失**（已有 P2 票位，随本规格的 M3 规格落真身：创建 modal + 一次性明文面板 + 吊销确认）。 |

### M4 弹窗族行为细节（ESC / 遮罩 / 焦点）

| | |
|---|---|
| Artifactory 行为（置信度：**高**） | 见本节族通用规格。 |
| BinFlow 载体 | MUI `Dialog` 全量承载（T-344 批 B）；`ConfirmDialog` 文档级 Esc 兜底（焦点掉 body 的边角）已有。 |
| 差距 | ✅ 已有 |

---

## 4. 抽屉族（D 系）——drawer

**族通用规格**：右侧滑入（anchor right）、宽 480px 档（内容为主的信息面板）、无遮罩点击关闭（或轻遮罩）、右上 `X` + ESC 关闭、内部滚动。**这是 BinFlow 当前最大的形态差距**——现网仅侧栏用了 `Drawer`，业务面板全是居中 `Dialog`。

### D1 Set Me Up（客户端接入向导）

| | |
|---|---|
| Artifactory 行为（置信度：**中高**；右侧滑入形态按用户指令 2026-08-30 给定） | 仓库行/详情的「Set Me Up」打开**右侧抽屉**：标题含包类型；按用途分 Tab（Configure / Deploy），面板内按工具折叠的代码片段（逐段 Copy），凭据区（Generate Token / 输口令），生成后片段带真实凭据。 |
| MUI 映射 | `Drawer(anchor="right", variant="temporary")` + `Tabs` + `Accordion`（每工具一节，右缘 Copy）+ 宽 `min(480px, 100vw-32px)` |
| BinFlow 载体 | `web/src/components/SetMeUpDialog.tsx`——功能面**超集**（包型网格步 0 / 仓库下拉 / Configure-Deploy 双 Tab / 铸币 + step-up 双腿 / OIDC 续铸 / 一次性明文面板 / 命令块与 docs/user 同源），但壳是**居中 Dialog 720px**。 |
| 差距 | △ **需改造（§10 批 1，本规格最高优先 FE 票）**：`Dialog` → 右 `Drawer` 480px。注意保留：`smu-*` 锚族全量（壳替换不动锚）、Esc/backdrop 关闭链、关闭回焦、OIDC 续铸重开链路（`AppShell` 的 `resumeOpen` 分支）、step-up 内联面板（抽屉内不弹二级框的纪律不变）。抽屉内命令块宽度变窄——`pre` 换行策略改 `overflow-x: auto` 保持（mono 不折断，P2 原则）。 |

### D2 制品详情面板

| | |
|---|---|
| Artifactory 行为（置信度：**中**） | Artifacts 视图 = 左树右详情的**页内分栏**（非浮层）：选中节点右栏出详情（概要/校验和/属性等页签），非 overlay，因此不抢树的操作上下文。 |
| MUI 映射 | 页内 flex 分栏（`Box` + 可折叠 `Drawer(variant="permanent")` 皆可）；**不是** temporary drawer |
| BinFlow 载体 | `web/src/pages/artifacts/ArtifactsBrowser.tsx`——左树 + 右 `NodeDetail` 页内面板（General/Props/Perms 三 Tab） |
| 差距 | ✅ 已有（页内分栏即 Artifactory 形态；勿改成浮层抽屉） |

### D3 依赖树

| | |
|---|---|
| Artifactory 行为（置信度：**低**) | 部分包型/构建视图有依赖树面板（Builds → 依赖列表；制品页签级依赖视图形态记不清）。 |
| MUI 映射 | `Treeview`（若立项） |
| BinFlow 载体 | 无——且**无后端面**（依赖解析不是 BinFlow 已建能力，PRODUCT.md 未列）。 |
| 差距 | ✗ 缺失（**暂缓**：§9 登记为「待 PM 定案的非承诺项」——非 Non-goal 硬线，但 M14 前不建；若立项需先出后端解析能力票，UI 形态届时按 D2 的页内分栏/右抽屉补 spec） |

---

## 5. 列表页模式（L 系）

### L1 表格工具栏（搜索 / 过滤 / 列选 / 刷新）

| | |
|---|---|
| Artifactory 行为（置信度：**中**） | 列表页工具栏：搜索框、过滤下拉、**列选择器**（选择显示列）、刷新按钮；右下或工具栏尾部有计数。 |
| MUI 映射 | `Toolbar` + `TextField`/`Select` + `IconButton(Refresh)` + 自建列选 `Menu`（checkbox 列表） |
| BinFlow 载体 | `RepositoriesPage`（搜索 key + 三 Tab 类型过滤 + 计数行）、`AuditPage`（时间/操作者/仓库/动作过滤 + 路径搜索）、`SearchPage`（仓库/类型过滤） |
| 差距 | △ 需改造（**低优先，§10 批 2**）：补**列选器**（首期只给仓库列表与审计页——列多者受益）与**刷新**按钮（轮询页已有自刷新，静态列表补手动刷新）。 |

### L2 行内溢出菜单（kebab ⋮）

| | |
|---|---|
| Artifactory 行为（置信度：**中**） | 行尾 `⋮` 菜单收纳行操作（编辑/删除等），行点击进详情。 |
| MUI 映射 | `IconButton(MoreVert)` + `Menu` |
| BinFlow 载体 | 仓库列表行点击进详情、无行内菜单（**删除有意收详情危险区**，console-ux §4.3）；树页有右键菜单（`TreeContextMenu`）——形态上已是行内操作的自有实现。 |
| 差距 | △ 需改造（§10 批 1）：仓库/用户/组/权限列表行加 `⋮`：菜单收 **详情 / 编辑 / 复制 key / Set Me Up（仓库行）**；**删除不进菜单**（保留危险区姿态——有意偏离 Artifactory，§9 豁免登记）。 |

### L3 面包屑（见 N3）

✅ 已有。

### L4 分页形态

| | |
|---|---|
| Artifactory 行为（置信度：**中**） | 管理列表多用页码分页控件；制品树浏览为增量加载。 |
| BinFlow 载体 | 全站「加载更多」增量（console-ux §6：keyset 游标 + 工程师往下翻心智）。 |
| 差距 | ✅ 已有（**有意偏离**：keyset 分页对页码不友好，BinFlow 形态保留；豁免登记 §9） |

---

## 6. 反馈模式（F 系）

### F1 toast / snackbar

| | |
|---|---|
| Artifactory 行为（置信度：**低**——位置记忆不确定，列 §8 核验） | 操作结果以 snackbar 通知，自动消失；具体锚位（右下/底部居中/顶部）需活体核验。 |
| MUI 映射 | `Snackbar` + `Alert` |
| BinFlow 载体 | `web/src/app/ToastContext.tsx`——右下堆叠、success 5s / error 常驻、可带动作链接、`aria-live` 播报 |
| 差距 | ✅ 已有（自有标准完整；若 §8 核验出 Artifactory 锚位显著不同且用户在意，再开微调票——默认**不改**） |

### F2 空态

| | |
|---|---|
| Artifactory 行为（置信度：**中**） | 空态带插画位 + 说明 + 主行动。 |
| BinFlow 载体 | `EmptyState`（message + hint + action，console-ux §5.1 双空态纪律） |
| 差距 | △ 需改造（**低优先**）：加 40px **插画槽位**（可选渲染），图形语言用 logo mark 的容器+箭隐喻线稿（候选 1 派生），不引第三方插画库。 |

### F3 加载骨架

| | |
|---|---|
| Artifactory 行为（置信度：**中**） | 列表/面板骨架占位。 |
| BinFlow 载体 | `Skeleton.tsx`（150ms 防闪烁 + `prefers-reduced-motion` 关动画，console-ux §5.2 大目录专项策略齐备） |
| 差距 | ✅ 已有 |

---

## 7. 差距矩阵（页面 × 模式）——PM 起 M14 UI-parity PRD 直接引用

图例：✅ 已对齐 ｜ △ 需改造 ｜ ✗ 缺失 ｜ `·` 不适用 ｜（豁）= BinFlow 有意偏离且豁免（§9）

| 页面 | 弹窗向导 M1 | 确认框 M2 | 表单 modal M3 | 抽屉 D1 | 详情分栏 D2 | 工具栏 L1 | 行内菜单 L2 | 空态/骨架/反馈 F 系 |
|---|---|---|---|---|---|---|---|---|
| 登录 | `·` | `·` | `·` | `·` | `·` | `·` | `·` | ✅ |
| 仪表盘 `/dashboard` | `·` | `·` | `·` | `·` | `·` | `·` | `·` | ✅ |
| 制品树 `/artifacts` | `·` | ✅（删 manifest 确认） | `·` | △（Deploy 对话框随 D1 同步评估抽屉化——**决策项 C**） | ✅ | ✅（过滤/复位） | ✅（右键菜单自有形态） | ✅ |
| 搜索 `/search` | `·` | `·` | `·` | `·` | `·` | ✅ | `·` | ✅ |
| 仓库列表 `/admin/repositories/*` | `·` | `·` | `·` | △（行内 Set Me Up 入口待 D1 抽屉化） | `·` | △（列选/刷新缺） | △（⋮ 缺） | ✅ |
| 建仓/编辑 `/admin/repositories/new` | △（**决策项 A**：网格+表单收单 Dialog） | `·` | `·` | `·` | `·` | `·` | `·` | ✅ |
| 仓库详情 `/admin/repositories/:key` | `·` | ✅（删仓危险区） | `·` | △（Set Me Up 入口） | `·` | `·` | `·` | ✅ |
| 用户 `/admin/security/users` | `·` | ✅ | △（**决策项 B**：创建 modal 化） | `·` | `·` | △ | △ | ✅ |
| 组 `/admin/security/groups` | `·` | ✅ | △（同 B） | `·` | `·` | △ | △ | ✅ |
| 权限 `/admin/security/permissions` | `·` | ✅（diff 确认，增强豁免） | ✅（豁：编辑整页 + 测试器/diff 为 BinFlow 增强） | `·` | `·` | △ | △ | ✅ |
| Access Tokens `/admin/security/tokens` | `·` | `·` | ✗（整页占位，M3 规格待落） | `·` | `·` | ✗ | ✗ | ✗ |
| 认证配置 `/admin/security/auth/*` | `·` | ✅（SAML 证书重生成） | `·` | `·` | `·` | `·` | `·` | ✅ |
| 审计 `/admin/governance/audit` | `·` | `·` | `·` | `·` | `·` | △（列选缺） | `·` | ✅ |
| 治理族（GC/配额/复制/备份/回收站） | `·` | ✅（GC apply/清空输入确认） | `·` | `·` | `·` | `·` | `·` | ✅ |
| 存储/系统信息/License `/admin/{monitoring,general}/*` | `·` | `·` | `·` | `·` | `·` | `·` | `·` | ✅ |
| 全局壳（N 系） | `·` | `·` | `·` | `·` | `·` | `·` | `·` | N1 ✅ / N2 △（图标槽，低）/ N3 ✅ |
| 依赖树 D3 | —（全站 ✗ 暂缓，待 PM 定案，§9） | | | | | | | |

**模式级汇总（供排优先级）**：最大差距 = **D1 抽屉族**（Set Me Up 居中 Dialog → 右抽屉）与 **M1 建仓单 modal**；次级 = **M3 用户/组创建 modal 化 + Tokens 页真身**、**L2 行内 ⋯**；低优先 = L1 列选/刷新、N2 侧栏图标、F2 空态插画位。

## 8. 活体核验清单（低/中置信项；核验后才可升格为 PRD 断言）

| # | 模式 | 待核验点 | 核验方法 |
|---|---|---|---|
| V1 | D1 | Set Me Up 的确切锚侧/宽度/Tab 命名（Configure/Deploy 的实际标签）、片段折叠形态 | JFrog Cloud 免费实例或 7.x 官方文档站截图/视频 |
| V2 | M1 | 向导第 ② 步 rclass 选择的确切控件（Tab vs 分段）、表单节名与折叠默认态、modal 宽度档 | 同上 |
| V3 | F1 | snackbar 锚位（右下/底部居中/顶部）与堆叠方向、时长 | 同上 |
| V4 | L2 | 仓库列表行菜单的确切动作集（是否含删除） | 同上 |
| V5 | N2 | 侧栏条目是否带图标（版本相关） | 同上 |
| V6 | M3 | 用户/组/Token 创建表单的字段集与分节；Token 结果面板形态 | 同上 |
| V7 | D3 | 依赖树视图的存在面与入口（Builds？制品页签？） | 同上（若确认 BinFlow 不建，本项直接关闭） |
| V8 | M2 | 删仓确认的确切文案模式（type-the-key 的提示语形） | 同上 |

核验纪律：本表核对结果回写本文档对应模式的置信度列（改置信度=改契约）；不新增 Artifactory 功能面的「顺带发现」——那些走 PM 的 Non-goal 过滤（§9）。

## 9. 不做清单与豁免登记

**永不建**（PRODUCT.md Non-goal，本规格不为其留位）：
- 洞察报表/趋势分析图表（dashboard 去重率只给数字与横条的口径不变）；
- 漏洞扫描/合规 UI（Xray 对位能力）；
- Artifactory 的 Builds、Federation、Lifecycles、Release Lifecycle、Repository Path Map 管理面（BinFlow 产品范围外）。

**有意偏离 Artifactory 且豁免**（对齐评审时不判为差距）：

| # | 偏离点 | 理由 |
|---|---|---|
| E1 | 删除类动作不进列表行内菜单/不做一键删 | 危险动作显式化（P5）；制品不可变，无撤销 |
| E2 | 「加载更多」增量分页（非页码控件） | keyset 游标 + 工程师心智（console-ux §6） |
| E3 | 仓库类型 badge 用中性色 | 颜色预算留给状态（console-ux §7.1） |
| E4 | 权限编辑器为整页 + 模式测试器 + diff 确认 | BinFlow 增强面，Artifactory 无对位 |
| E5 | 用户/组**编辑**保留路由页 | 重内容表单（穿梭列/成员矩阵），modal 伤可用性 |
| E6 | UI 文案中文 + 术语保留英文原词（repository key/deployment 等） | console-ux §1.2 命名对齐条款 |
| E7 | toast 锚位暂保留右下 | F1 置信度低，核验后再议（V3） |

## 10. 落地批次建议（供 PM 排 M14 拆票；全部为 FE 票，不动后端）

- **批 1（高置信形态对齐，PRD 可直接断言）**：① D1 Set Me Up 抽屉化（含 Deploy 对话框评估——决策项 C：建议 Deploy 保持居中 Dialog，上传进度表在抽屉里过窄）；② M1 建仓向导单 Dialog 化 + 深链兼容；③ M3 用户/组创建 modal 化；④ L2 列表行内 ⋯ 菜单（不含删除）。
- **批 2（补缺与低优先）**：⑤ Tokens 页真身（M3 规格）；⑥ L1 列选器 + 刷新（仓库/审计先行）；⑦ F2 空态插画槽位；⑧ N2 侧栏图标槽（待 V5）。
- **批 3（核验后微调）**：F1 toast 锚位（V3）、M1/M3 细节修正（V2/V6）。
- 品牌资产接线（UX-1 另两交付）随批 2 并行：logo 落地清单见 `brand/logo/README.md` §4；包型图标接线注意见 `brand/package-icons/README.md` §6。
