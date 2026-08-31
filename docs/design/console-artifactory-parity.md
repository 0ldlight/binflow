# BinFlow 控制台 × JFrog Artifactory 交互对齐规格（console-artifactory-parity）

| 项 | 值 |
|---|---|
| 文档 | `docs/design/console-artifactory-parity.md` |
| 票据 | UX-1（插空票：品牌资产 + Artifactory 交互对齐规格） |
| 状态 | v1.1（2026-08-31，T-381 活体核验回写） |
| 维护者 | ux-designer |
| 上游依据 | 用户指令 2026-08-30（① 前端交互体验与 JFrog Artifactory 完全一致，含弹窗、抽屉等）；`docs/design/console-ux.md` v1.15（IA/四态/token 母册）；`docs/design/mui-native-visual.md`（MUI 原生视觉基线）；`web/src/` 现状逐一核对（见各模式的「BinFlow 载体」列） |
| 下游消费者 | **PM——M14 UI-parity PRD 直接引用 §7 差距矩阵**；FE 拆票；qa-engineer 验收 |
| 置信度声明 | Artifactory 7.x 行为描述基于本票作者的产品知识（clean-room：无反编译 UI 代码消费）。**V1~V8 已于 2026-08-31 由 qa-engineer 在活体实例上核验完毕**（源：t226-artifactory，Artifactory OSS 7.84.10 rev 78410900，DOM 实测 + 截图归档 `reports/agents/t381-evidence/`，详见 `reports/agents/T-381.md`）；核验结论已回写各模式置信度列与 §8。未入 §8 的中置信项（L1/L4/F2/F3/N3 等）维持原标注 |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-30 | UX-1 初版：交互模式 catalog（导航 N1~N3 / 弹窗 M1~M4 / 抽屉 D1~D3 / 列表 L1~L4 / 反馈 F1~F3），逐项置信度标注 + MUI 映射 + BinFlow 载体对照；§7 页面×模式差距矩阵；§8 活体核验清单；§9 不做清单；§10 落地批次建议 |
| v1.1 | 2026-08-31 | **T-381 活体核验回写**（qa-engineer 执行，源 = t226-artifactory OSS 7.84.10 活体实例，DOM 实测）。V1~V8 全部核验，改契约级结论：**M1 决策项 A 撤销**（7.84 建仓 = 网格 modal 880px + 整页表单两段式，BinFlow 现形态已一致）；**M3 决策项 B 撤销**（7.84 用户/组创建 = 整页路由表单，非 modal）；**L2 ⋮ 菜单无对位**（7.84 行内 = 直删 trash 图标按钮，删除确认为 520px 轻量 message-box、无 type-the-key、Delete 键为绿色主按钮）；**D1 宽度修正 480px→50vw**（800px@1600 实测，Tab = Configure/Deploy/Resolve 三枚）；**F1 锚位证实 = 顶部居中单条 ~2-3s**（E7 转入「再议」）；**V5 证实侧栏条目带图标**（N2 维持改造）；**V7 关闭**（Builds 面存在但 BinFlow 不建；制品详情页签无依赖视图）。§7 矩阵按此重印，§8 附核验结论列。证据：`reports/agents/T-381.md` + `reports/agents/t381-evidence/` |

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
| Artifactory 行为（置信度：**高**；V5 已核验 2026-08-31） | 分组标题（可展开的父级，展开后子项缩进）+ 条目；**条目带图标**——7.84 实测：应用域与管理域侧栏（宽 230px）每个一级条目带 1~2 枚 icon-font 图标（`i` 元素，无 svg/img），子项无图标；active 行高亮带左侧指示。 |
| MUI 映射 | `List` + `ListItemButton`（dense）+ `Typography variant="overline"` 分组标签 |
| BinFlow 载体 | `AppShell.tsx`——分组标签是节标签非折叠项（15 条目，console-ux §3.1 纪律）；active 行 = `--bf-sidebar-3` 底 + 2px primary 左指示条 |
| 差距 | △ 需改造（**低优先，V5 核验后维持不降级**）：条目加 16px 图标槽（mono 包型图标/通用符号）。核验细节修正：Artifactory 只给**一级条目**配图标、子项裸文本——BinFlow 接线时照此档位，不必每级都配。 |

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
| Artifactory 行为（**V2 已核验 2026-08-31，逐项实测**） | ① 入口：仓库列表页 **「Add Repositories」下拉**（Local / Remote / Virtual 三项）——rclass 在进向导前选定，**不在 modal 内以 Tab/分段控件出现**；② 「Select Package Type」**modal 网格**：宽 **880px** 居中、右上 X，90×90 包型磁贴（inline SVG 官方标）；③ 选磁贴后 **modal 关闭、落在整页路由表单**（`/ui/admin/repositories/local/new`，标题 "New Local Repository"）——**不是单 modal 全程**；④ 表单字段：Repository Key / Environments / Repository Layout / Public Description / Internal Description + 折叠高级节（Disable Artifact Resolution… / Allow Content Browsing / Enable Event Replication 等复选项）；⑤ 页脚：**Cancel（左，transparent）+ Create Local Repository（右，primary）**。 |
| MUI 映射 | 网格步 `Dialog`（宽 880px ≈ `maxWidth="lg"`）+ 表单步**路由页**分节 `Paper` |
| BinFlow 载体 | `web/src/pages/repositories/RepositoryFormPage.tsx`——已是两段式：`pkg-grid` Dialog（选择包类型，带 `pkg-tier-*` 档位徽章）→ **路由页** `/admin/repositories/new` 单页分区表单（常规/来源/成员/策略/治理/高级六节 `Paper`）；rclass 由 URL query 预选 + 单选组。 |
| 差距 | ✅ 已有（**v1.1 核验改判：决策项 A 撤销**——7.84 实测即「网格 modal → 整页表单」两段式，BinFlow 现形态与 Artifactory 一致，无需收单 Dialog）。保留微差注记：Artifactory 的 rclass 由入口下拉选定，BinFlow 用 URL query + 页内单选组——手势等价，不改。网格 modal 宽度参考 880px（BinFlow 现档位即可）。 |

### M2 删除确认（危险确认族）

| | |
|---|---|
| Artifactory 行为（置信度：**高**；**V8 已核验 2026-08-31，原文实测**） | 仓库行 trash 图标点击后弹**居中 message-box（520px）**：标题 **"Delete Repository"**（右上 X），正文 **"Are you sure you want to delete the {key} repository? All artifacts will be permanently deleted."**，按钮 **Cancel（左）/ Delete（右）**——**无 type-the-key 输入**（box 内 input 为隐藏元素）；**Delete 为绿色主按钮**（rgb(64,190,70)），非红色 danger；ESC 可安全关闭。 |
| MUI 映射 | `Dialog` + `TextField`（`confirm-input` 类钩子）+ `Button(color="error")` |
| BinFlow 载体 | `ConfirmDialog.tsx`（`danger` 红边 + `confirmDisabled` 前置）+ `RepoDeleteConfirm.tsx`（非空仓 `deleteContent` 复选 + 影响 N 制品摘要 + 输入 key）；GC apply/清空回收站同款输入确认。 |
| 差距 | ✅ 已有（**BinFlow 语义比 Artifactory 严两档，豁免不改、v1.1 有实证**：7.84 删仓仅一次单击确认、无输入校验、确认键还是绿色；BinFlow 的输入 key 确认 + 红色 danger + 影响面摘要是更安全的有意偏离，§9 E1 登记） |

### M3 实体表单（User / Group / Token / Permission Target）

| | |
|---|---|
| Artifactory 行为（置信度：**高**；**V6 已核验 2026-08-31，实测修正**） | 用户/组**创建 = 整页路由表单**（`/ui/admin/management/users/new`、`/groups/new`），**不是 modal**——用户表单字段：User Name / Email Address / Password / Retype Password + 复选项（Administer Platform / Manage Resources / Can Update Profile / Disable UI Access / Disable Internal Password）；组表单：Group Name / Description / External ID + 复选项 + 成员用户选择列表；页脚 **Cancel（最左）/ Reset / Save（右）**。Token 面：**用户 profile 页**（`/ui/user_profile`）的 "Authentication Settings → Generate an Identity Token" 区 + **Identity Tokens 表**（Description / Token ID / Issued At / Expiry Date，带列选器）；admin 侧集中 token 管理页在 OSS 7.84 导航不可达（可能为 Pro 门）。 |
| MUI 映射 | 路由页分节表单（`Paper` 节 + 页脚动作条）；token 面复用 SetMeUp 的 `smu-token-panel` 形态 |
| BinFlow 载体 | 用户：`UsersPage`（列表页内建表单）+ `UserDetailPage`（路由页编辑）；组：`GroupsPage`（同列表页形态）；权限：`PermissionEditorPage`（路由页——BinFlow 自有的模式测试器/diff 确认是增强，**豁免**）；Tokens：`PlaceholderPage`（P2 占位）。 |
| 差距 | ✅ 形态已对齐（**v1.1 核验改判：决策项 B 撤销**——7.84 用户/组创建即整页路由表单，BinFlow 无需 modal 化；BinFlow 列表页内建表单与路由页编辑的差异属同档形态，可保持）。Tokens 页 ✗ **缺失**（BinFlow 侧缺口维持，随 M3 规格落真身：token 列表表 + 铸币区 + 一次性明文面板 + 吊销确认；形态参照 7.84 profile 页的表+生成区，V6c 生成表单字段集因 profile 密码锁未核验，接线票时以 `smu-token-panel` 既有形态为准）。 |

### M4 弹窗族行为细节（ESC / 遮罩 / 焦点）

| | |
|---|---|
| Artifactory 行为（置信度：**高**） | 见本节族通用规格。 |
| BinFlow 载体 | MUI `Dialog` 全量承载（T-344 批 B）；`ConfirmDialog` 文档级 Esc 兜底（焦点掉 body 的边角）已有。 |
| 差距 | ✅ 已有 |

---

## 4. 抽屉族（D 系）——drawer

**族通用规格（v1.1 核验修订）**：右侧滑入（anchor right）、宽 **50vw 档（480~800px，1600 视口实测 800px）**、带轻遮罩、右上 `X` + ESC 关闭、内部滚动（V1 实测：`el-drawer rtl` + `v-modal` 遮罩 + "close drawer" 关闭钮）。**这是 BinFlow 当前最大的形态差距**——现网仅侧栏用了 `Drawer`，业务面板全是居中 `Dialog`。

### D1 Set Me Up（客户端接入向导）

| | |
|---|---|
| Artifactory 行为（置信度：**高**；**V1 已核验 2026-08-31，DOM 实测**） | 打开**右侧抽屉**（`el-drawer rtl`，带遮罩），宽 **50vw**（1600 视口实测 800px，1280 视口 640px）、全高；标题 **"Set Up a client"**（右缘 close，aria-label "close drawer"）；**步 0 = 包型药丸选择**（generic/docker/maven/npm… 横排）→ 选中后标题变 "Set Up A Generic Client"；**Tab 三枚：Configure / Deploy / Resolve**；仓选择器（该包型仓库下拉）；凭据区文案 "To create client setup instructions with embedded authentication, generate an identity token" + **Generate Token & Create Instructions**（brand）+ Dismiss；片段为**每 Tab 平铺代码块**（生成 token 前隐藏，DOM 内占位 `curl -uadmin:<PASSWORD> -T …`（Deploy）/`curl -uadmin:<PASSWORD> -L -O …`（Resolve）），**无按工具折叠的手风琴**；底栏 **Select a different package type（左，文本链接式）+ Done（右，primary）**。入口：制品页头部 Set Me Up 按钮、顶栏快捷下拉、仓库行/详情入口。 |
| MUI 映射 | `Drawer(anchor="right", variant="temporary")` + `Tabs`（3 枚）+ 宽 **50vw 上限 800px 下限 ~480px**（`clamp(480px, 50vw, 800px)`） |
| BinFlow 载体 | `web/src/components/SetMeUpDialog.tsx`——功能面**超集**（包型网格步 0 / 仓库下拉 / Configure-Deploy 双 Tab / 铸币 + step-up 双腿 / OIDC 续铸 / 一次性明文面板 / 命令块与 docs/user 同源），但壳是**居中 Dialog 720px**。 |
| 差距 | △ **需改造（§10 批 1，本规格最高优先 FE 票；v1.1 核验后维持并参数化）**：`Dialog` → 右 `Drawer`，**宽 50vw（480~800px），非固定 480px**；Tab 补第三枚 **Resolve**（与 Deploy 并列；BinFlow 的 Configure-Deploy 双 Tab 改三 Tab）。注意保留：`smu-*` 锚族全量（壳替换不动锚）、Esc/backdrop 关闭链、关闭回焦、OIDC 续铸重开链路（`AppShell` 的 `resumeOpen` 分支）、step-up 内联面板（抽屉内不弹二级框的纪律不变）。命令块维持平铺 + 右缘 Copy（BinFlow 的按工具折叠是超集增强，保留无妨）；`pre` 换行策略 `overflow-x: auto` 保持（mono 不折断，P2 原则）。底栏形态（左文本返回 + 右主按钮）对齐。 |

### D2 制品详情面板

| | |
|---|---|
| Artifactory 行为（置信度：**中**） | Artifacts 视图 = 左树右详情的**页内分栏**（非浮层）：选中节点右栏出详情（概要/校验和/属性等页签），非 overlay，因此不抢树的操作上下文。 |
| MUI 映射 | 页内 flex 分栏（`Box` + 可折叠 `Drawer(variant="permanent")` 皆可）；**不是** temporary drawer |
| BinFlow 载体 | `web/src/pages/artifacts/ArtifactsBrowser.tsx`——左树 + 右 `NodeDetail` 页内面板（General/Props/Perms 三 Tab） |
| 差距 | ✅ 已有（页内分栏即 Artifactory 形态；勿改成浮层抽屉） |

### D3 依赖树

| | |
|---|
| Artifactory 行为（置信度：**中**；**V7 已核验 2026-08-31**） | **Builds 面存在于 OSS 7.84**（应用域侧栏 Builds，列 Build Name / Build Repository / Last Build ID / Last Build Time，空态 "No results were found"；本实例无 build 数据，build 详情内的依赖列表形态不可观测）；**制品详情页签 = General / Effective Permissions / Properties / Followers——无依赖视图**（v1.0 的「制品页签级依赖视图」记忆证伪）。 |
| MUI 映射 | `Treeview`（若立项） |
| BinFlow 载体 | 无——且**无后端面**（依赖解析不是 BinFlow 已建能力，PRODUCT.md 未列）。 |
| 差距 | ✗ 缺失（**v1.1 关闭**：V7 核验后维持不建——依赖数据挂在 Builds 面下，而 Builds 属 §9 永不建清单；制品侧无对位视图已实测。若未来 PM 立项需先出后端解析能力票，届时再补 spec） |

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
| Artifactory 行为（置信度：**高**；**V4 已核验 2026-08-31，实测修正**） | **7.84 仓库列表与用户列表行内均无 `⋮` 菜单**——行尾是**单个直删图标按钮**（`icon-trash`，仓库行与用户行同款；另有包型 logo 列等非动作元素）。破坏性动作直接暴露在行尾，点击后弹 M2 的 520px 轻量确认框。行点击进详情。 |
| MUI 映射 | `IconButton(MoreVert)` + `Menu` |
| BinFlow 载体 | 仓库列表行点击进详情、无行内菜单（**删除有意收详情危险区**，console-ux §4.3）；树页有右键菜单（`TreeContextMenu`）——形态上已是行内操作的自有实现。 |
| 差距 | ✅（豁）（**v1.1 核验改判：原「加 ⋮ 对齐」的 parity 依据不成立**——Artifactory 根本没有行内菜单形态，其行内直删恰是 BinFlow E1 豁免要避开的激进设计）。原 △ 降为**可选自有增强**（复制 key / Set Me Up 行内快捷入口可留作 BinFlow 体验票，不挂 parity 旗），§10 批 1 ④ 撤销。 |

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
|---|
| Artifactory 行为（置信度：**高**；**V3 已核验 2026-08-31，实测**） | 操作结果 toast = **顶部居中单条**（`el-message`，横向居中、settle 于 y≈20px，从顶部滑入/向上淡出）；**自动消失，存活约 2~3s**；**无堆叠列表形态**（多事件顺序替代）。实测文案例：`Repository 't381-ui-probe' was created successfully`（success 绿色系）。 |
| MUI 映射 | `Snackbar` + `Alert` |
| BinFlow 载体 | `web/src/app/ToastContext.tsx`——右下堆叠、success 5s / error 常驻、可带动作链接、`aria-live` 播报 |
| 差距 | ✅ 已有（自有标准完整；**v1.1 核验证实两侧锚位显著不同**：Artifactory 顶部居中单条 vs BinFlow 右下堆叠。BinFlow 的堆叠 + error 常驻对长任务流更稳，**默认不改**；是否迁就对齐走 §9 E7「再议」出口，交 PM/用户定夺） |

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

## 7. 差距矩阵（页面 × 模式）——PM 起 M14 UI-parity PRD 直接引用（v1.1 核验后重印）

图例：✅ 已对齐 ｜ △ 需改造 ｜ ✗ 缺失 ｜ `·` 不适用 ｜（豁）= BinFlow 有意偏离且豁免（§9）
**v1.1 重印说明**：本版矩阵已吸收 T-381 活体核验结论（V1~V8）；与 v1.0 的差异均以「v1.1」标注。三出口判定见 §8 表下注。

| 页面 | 弹窗向导 M1 | 确认框 M2 | 表单 modal M3 | 抽屉 D1 | 详情分栏 D2 | 工具栏 L1 | 行内菜单 L2 | 空态/骨架/反馈 F 系 |
|---|---|---|---|---|---|---|---|---|
| 登录 | `·` | `·` | `·` | `·` | `·` | `·` | `·` | ✅ |
| 仪表盘 `/dashboard` | `·` | `·` | `·` | `·` | `·` | `·` | `·` | ✅ |
| 制品树 `/artifacts` | `·` | ✅（删 manifest 确认） | `·` | △（Deploy 对话框随 D1 同步评估抽屉化——**决策项 C**：v1.1 实测 7.84 制品页头部确有 Set Me Up + Deploy 并列按钮，维持「Deploy 保持居中 Dialog」建议） | ✅ | ✅（过滤/复位） | ✅（右键菜单自有形态） | ✅ |
| 搜索 `/search` | `·` | `·` | `·` | `·` | `·` | ✅ | `·` | ✅ |
| 仓库列表 `/admin/repositories/*` | `·` | `·` | `·` | △（行内 Set Me Up 入口待 D1 抽屉化） | `·` | △（列选/刷新缺） | ✅（豁）（v1.1：7.84 无 ⋮，行内=直删图标——L2 见模式节） | ✅ |
| 建仓/编辑 `/admin/repositories/new` | ✅（v1.1：7.84 即「网格 modal 880px + 整页表单」两段式，**决策项 A 撤销**） | `·` | `·` | `·` | `·` | `·` | `·` | ✅ |
| 仓库详情 `/admin/repositories/:key` | `·` | ✅（删仓危险区；7.84 原生仅轻量确认——BinFlow 更严，豁免） | `·` | △（Set Me Up 入口） | `·` | `·` | `·` | ✅ |
| 用户 `/admin/security/users` | `·` | ✅ | ✅（v1.1：7.84 用户创建=整页路由表单，**决策项 B 撤销**） | `·` | `·` | △ | ✅（豁）（v1.1：7.84 用户行=直删图标无 ⋮） | ✅ |
| 组 `/admin/security/groups` | `·` | ✅ | ✅（同 B 撤销） | `·` | `·` | △ | ✅（豁）（同上） | ✅ |
| 权限 `/admin/security/permissions` | `·` | ✅（diff 确认，增强豁免） | ✅（豁：编辑整页 + 测试器/diff 为 BinFlow 增强） | `·` | `·` | △ | ✅（豁） | ✅ |
| Access Tokens `/admin/security/tokens` | `·` | `·` | ✗（整页占位，M3 规格待落；v1.1 核验补充参照形态：7.84 = profile 页「生成区 + Identity Tokens 表」） | `·` | `·` | ✗ | ✗ | ✗ |
| 认证配置 `/admin/security/auth/*` | `·` | ✅（SAML 证书重生成） | `·` | `·` | `·` | `·` | `·` | ✅ |
| 审计 `/admin/governance/audit` | `·` | `·` | `·` | `·` | `·` | △（列选缺） | `·` | ✅ |
| 治理族（GC/配额/复制/备份/回收站） | `·` | ✅（GC apply/清空输入确认） | `·` | `·` | `·` | `·` | `·` | ✅ |
| 存储/系统信息/License `/admin/{monitoring,general}/*` | `·` | `·` | `·` | `·` | `·` | `·` | `·` | ✅ |
| 全局壳（N 系） | `·` | `·` | `·` | `·` | `·` | `·` | `·` | N1 ✅ / N2 △（图标槽；V5 已证 7.84 一级条目带图标，维持）/ N3 ✅ |
| 依赖树 D3 | —（v1.1 关闭：依赖视图挂 Builds 面（§9 永不建），制品页签无对位已实测） | | | | | | | |

**模式级汇总（v1.1 重排）**：核验后**唯一高优先 parity 差距 = D1 抽屉族**（Set Me Up 居中 Dialog → 右抽屉 50vw + 补 Resolve Tab）；**次级 = Tokens 页真身**（BinFlow 侧缺口）；原 M1 建仓单 modal 化、M3 用户/组创建 modal 化、L2 ⋮ 菜单三个改造项的 parity 依据经核验**不成立，撤销**（BinFlow 现形态即 Artifactory 形态，或 Artifactory 无对位）；低优先 = L1 列选/刷新、N2 侧栏图标（一级条目档位）、F2 空态插画位。

## 8. 活体核验清单（低/中置信项；核验后才可升格为 PRD 断言）——**v1.1：V1~V8 已全部核验**

核验源：t226-artifactory 活体实例（Artifactory OSS **7.84.10** rev 78410900，T-228 保留栈，2026-08-31 由 qa-engineer 恢复并驱动 Playwright 实测；DOM 结构化测量 + 截图归档 `reports/agents/t381-evidence/`；执行记录 `reports/agents/T-381.md`）。

| # | 模式 | 待核验点 | 核验结论（2026-08-31） | 置信度变化 |
|---|---|---|---|---|
| V1 | D1 | Set Me Up 的确切锚侧/宽度/Tab 命名（Configure/Deploy 的实际标签）、片段折叠形态 | **右侧抽屉（el-drawer rtl + 遮罩），宽 50vw（800px@1600 / 640px@1280 实测），全高；Tab = Configure / Deploy / Resolve 三枚**（比 v1.0 猜想多一枚 Resolve）；步 0 包型药丸选择；标题 "Set Up a client"→"Set Up A Generic Client"；凭据区 = Generate Token & Create Instructions + Dismiss；片段**每 Tab 平铺、无手风琴**（生成前隐藏，占位 `curl -uadmin:<PASSWORD> …`）；底栏「Select a different package type（左）+ Done（右 primary）」 | 中高 → **高**（宽度/Tab/折叠形态三处修正已回写 D1） |
| V2 | M1 | 向导第 ② 步 rclass 选择的确切控件（Tab vs 分段）、表单节名与折叠默认态、modal 宽度档 | **rclass 不在向导内**——列表页「Add Repositories」下拉（Local/Remote/Virtual）预选定；**Select Package Type modal 宽 880px**（90×90 磁贴）；选型后 modal 关闭落**整页路由表单**；字段实测（Repository Key/Environments/Repository Layout/Public+Internal Description + 折叠高级复选项）；页脚 Cancel（左 transparent）/ Create Local Repository（右 primary） | 中/低 → **高**（「全程单 modal」记忆证伪；**决策项 A 撤销**） |
| V3 | F1 | snackbar 锚位（右下/底部居中/顶部）与堆叠方向、时长 | **顶部居中单条**（el-message，横向正中、settle y≈20px，顶部滑入/向上淡出）；**自动消失 ~2~3s**；**无堆叠** | 低 → **高**（E7 转「再议」） |
| V4 | L2 | 仓库列表行菜单的确切动作集（是否含删除） | **7.84 仓库/用户列表行均无 ⋮ 菜单**：行尾 = 单个 `icon-trash` 直删图标按钮（点击弹 V8 确认框）；无编辑/复制类行内动作收纳 | 中 → **高**（「⋮ 收纳动作集」前提不成立；原改造项撤销，降为可选自有增强） |
| V5 | N2 | 侧栏条目是否带图标（版本相关） | **带**——应用域与管理域侧栏（230px）一级条目均带 icon-font 图标（`i` 元素；Environments 用 svg），**子项无图标** | 中 → **高**（N2 维持 △，不降级；接线档位=一级条目） |
| V6 | M3 | 用户/组/Token 创建表单的字段集与分节；Token 结果面板形态 | **用户/组创建 = 整页路由表单非 modal**（字段集已实测回写 M3；页脚 Cancel/Reset/Save）；Token 面 = profile 页生成区 + Identity Tokens 表（Description/Token ID/Issued At/Expiry Date）；**生成表单字段集与结果面板因 profile 密码锁未核验**（降级子项，见 T-381 报告） | 中 → **高**（形态面；**决策项 B 撤销**）/ token 明文面板子项维持「以核验为准」 |
| V7 | D3 | 依赖树视图的存在面与入口（Builds？制品页签？） | **Builds 面存在**（OSS 7.84 应用域，4 列表 + 空态；无 build 数据，依赖列表形态不可观测）；**制品详情页签 = General/Effective Permissions/Properties/Followers，无依赖视图** | 低 → **中**（面级）；**本项关闭**：依赖数据挂 Builds（§9 永不建），BinFlow 维持不建，D3 关闭 |
| V8 | M2 | 删仓确认的确切文案模式（type-the-key 的提示语形） | **无 type-the-key**——520px 居中 message-box："Delete Repository" / "Are you sure you want to delete the {key} repository? All artifacts will be permanently deleted." / Cancel（左）+ **Delete（右，绿色主按钮 rgb(64,190,70)）**；ESC 安全关闭 | 高（维持）+ 文案实证（BinFlow 的输入确认 = 更严的有意偏离，E1 豁免强化） |

**三出口判定（v1.1）**：
- **V5 → 不降级**：7.84 侧栏一级条目带图标实测成立，N2 维持「需改造（低优先）」，批 2 ⑧ 照排（档位：仅一级条目）。
- **V7 → 关闭**：D3 依赖树关闭（依赖视图挂 Builds 面，BinFlow 不建 Builds；制品侧无对位已实测），§7 矩阵 D3 行已改判。
- **E7 → 再议**：toast 锚位差异已实证（Artifactory 顶部居中单条 ~2-3s vs BinFlow 右下堆叠 + error 常驻）。BinFlow 形态对多任务流更稳，**默认不改**；若用户明确要求对齐 Artifactory 锚位，开 F1 微调票（`Snackbar anchorOrigin` 一行级改动，成本极低）——交 PM/用户定夺，不自动执行。

核验纪律：本表核对结果回写本文档对应模式的置信度列（改置信度=改契约）；不新增 Artifactory 功能面的「顺带发现」——那些走 PM 的 Non-goal 过滤（§9）。**v1.1 顺带观察（不改置信度列，供 PM 参考）**：① 用户/Builds 列表均见 "Customize Columns" 列选器（L1 的中置信前提获旁证）；② 7.84 顶栏有快捷下拉（Set Me Up / New Local/Remote/Virtual Repository 直达）；③ 制品页头部 Set Me Up 与 Deploy 并列（决策项 C 旁证）；④ 侧栏分组的父级可折叠展开（与 N2「节标签不折叠」的 v1.0 描述有出入——BinFlow 的节标签形态更简，维持不改，注记）。

## 9. 不做清单与豁免登记

**永不建**（PRODUCT.md Non-goal，本规格不为其留位）：
- 洞察报表/趋势分析图表（dashboard 去重率只给数字与横条的口径不变）；
- 漏洞扫描/合规 UI（Xray 对位能力）；
- Artifactory 的 Builds、Federation、Lifecycles、Release Lifecycle、Repository Path Map 管理面（BinFlow 产品范围外）。

**有意偏离 Artifactory 且豁免**（对齐评审时不判为差距）：

| # | 偏离点 | 理由 |
|---|---|---|
| E1 | 删除类动作不进列表行内/不做一键删 | 危险动作显式化（P5）；制品不可变，无撤销。**v1.1 实证加码**：7.84 行内直删 trash 图标 + 仅一次单击确认（V4/V8）——BinFlow 的输入 key 确认 + 危险区收束比 Artifactory 原生更严，豁免立场稳固 |
| E2 | 「加载更多」增量分页（非页码控件） | keyset 游标 + 工程师心智（console-ux §6） |
| E3 | 仓库类型 badge 用中性色 | 颜色预算留给状态（console-ux §7.1） |
| E4 | 权限编辑器为整页 + 模式测试器 + diff 确认 | BinFlow 增强面，Artifactory 无对位 |
| E5 | 用户/组**创建与编辑**均保留页面形态 | **v1.1 改写**（原条目仅覆盖编辑）：V6 实测 7.84 创建即整页路由表单，页面形态不再是偏离而是对齐；modal 化决策项 B 已撤销 |
| E6 | UI 文案中文 + 术语保留英文原词（repository key/deployment 等） | console-ux §1.2 命名对齐条款 |
| E7 | toast 锚位暂保留右下 | **v1.1 转「再议」**：V3 实证 Artifactory = 顶部居中单条 ~2-3s 无堆叠；BinFlow 右下堆叠 + error 常驻为有意设计，默认不改；是否对齐交 PM/用户（改动成本一行级，见 §8 三出口） |

## 10. 落地批次建议（供 PM 排 M14 拆票；全部为 FE 票，不动后端）——**v1.1 按核验结论重排**

- **批 1（高置信形态对齐，PRD 可直接断言）**：① **D1 Set Me Up 抽屉化**（唯一存活的批 1 parity 票：壳 `Dialog` → 右 `Drawer`，宽 `clamp(480px, 50vw, 800px)`，**Tab 补第三枚 Resolve**，底栏「返回链接 + Done」对齐；含 Deploy 对话框评估——决策项 C：建议 Deploy 保持居中 Dialog，上传进度表在抽屉里过窄，v1.1 实测 7.84 制品页 Set Me Up 与 Deploy 并列，建议维持）。~~② M1 建仓向导单 Dialog 化~~（**v1.1 撤销**：V2 证实现形态已对齐）；~~③ M3 用户/组创建 modal 化~~（**v1.1 撤销**：V6 证明 7.84 即整页表单）；~~④ L2 行内 ⋯ 菜单~~（**v1.1 撤销 parity 旗**：V4 证明 7.84 无 ⋮；复制 key/Set Me Up 行内快捷可留作可选自有增强票，不挂对齐目标）。
- **批 2（补缺与低优先）**：⑤ Tokens 页真身（M3 规格；v1.1 补参照形态：生成区 + token 表（Description/Token ID/Issued At/Expiry Date）+ 一次性明文面板 + 吊销确认）；⑥ L1 列选器 + 刷新（仓库/审计先行；v1.1 旁证：7.84 用户/Builds 列表均有 Customize Columns）；⑦ F2 空态插画槽位；⑧ N2 侧栏图标槽（**V5 已核验：照排**，档位=一级条目）。
- **批 3（核验后微调）**：**F1 toast 锚位——E7 再议中**（V3 实证差异，默认不改，待 PM/用户定夺）；~~M1/M3 细节修正~~（V2/V6 核验后无遗留修正项；唯一残留 = V6c token 生成表单字段集，随 ⑤ 接线票以 `smu-token-panel` 既有形态覆盖）。
- 品牌资产接线（UX-1 另两交付）随批 2 并行：logo 落地清单见 `docs/design/brand/logo/README.md` §4；包型图标接线注意见 `docs/design/brand/package-icons/README.md` §6（低置信三枚已于 2026-08-31 活体对照修正，见该 README §4）。
