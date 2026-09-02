# BinFlow 控制台 × JFrog Artifactory 交互对齐规格（console-artifactory-parity）

| 项 | 值 |
|---|---|
| 文档 | `docs/design/console-artifactory-parity.md` |
| 票据 | UX-1（插空票：品牌资产 + Artifactory 交互对齐规格） |
| 状态 | v1.5（2026-09-03，T-437 M16 翻案修订：E5/E1/E2/E6/R4 五处翻案改写 + §9A stay-out 登记 + §11 K67 冻结 + §12 B 47 项四态预归属表——ux 主笔 + **PM 会签位**〔按 PRD v1.1 七裁定代笔〕；**版本对账：PRD/审计材料所称「parity 册 v1.2」即本版 v1.5**——实序为准，§0 留痕；此前 v1.4 T-428 文面回写 / v1.3 T-400 终评落档） |
| 维护者 | ux-designer |
| 上游依据 | 用户指令 2026-08-30（① 前端交互体验与 JFrog Artifactory 完全一致，含弹窗、抽屉等）；用户指令 intake ⑤（2026-09-02，BOARD 在档——M16 全前端对齐 + 不做清单全面翻案除 Xray）；`docs/prd/milestone-16.md` v1.1（§7 Q1~Q7/Q13 七+一裁定——v1.5 翻案依据）；`reports/m16-parity-audit-material.md`（A/B/C/D 四件套——B 47 项偏差与 A8 清单）；`docs/design/console-ux.md` v1.15+（IA/四态/token 母册；锚登记以 §10.5 为锚名权威源）；`docs/design/mui-native-visual.md`（MUI 原生视觉基线）；`web/src/` 现状逐一核对（见各模式的「BinFlow 载体」列）；`reports/agents/T-434.md`（树栈 as-built——§11 K67 冻结底稿） |
| 下游消费者 | **PM——M14 UI-parity PRD 直接引用 §7 差距矩阵**；FE 拆票；qa-engineer 验收 |
| 置信度声明 | Artifactory 7.x 行为描述基于本票作者的产品知识（clean-room：无反编译 UI 代码消费）。**V1~V8 已于 2026-08-31 由 qa-engineer 在活体实例上核验完毕**（源：t226-artifactory，Artifactory OSS 7.84.10 rev 78410900，DOM 实测 + 截图归档 `reports/agents/t381-evidence/`，详见 `reports/agents/T-381.md`）；核验结论已回写各模式置信度列与 §8。未入 §8 的中置信项（L1/L4/F2/F3/N3 等）维持原标注。**R 系（复制面）于 2026-09-01 由 qa-engineer 活体锚定**（同源实例，差集法只读探测 + 服务端下发的前端静态资产行为事实提取，证据 `reports/agents/t402-evidence/`，详见 `reports/agents/T-402a.md`）：活体可达项标高置信；OSS license 门后不可达项以 bundle 实证 + 公开 REST 文档双源标中/中高置信，**未静默升格** |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-30 | UX-1 初版：交互模式 catalog（导航 N1~N3 / 弹窗 M1~M4 / 抽屉 D1~D3 / 列表 L1~L4 / 反馈 F1~F3），逐项置信度标注 + MUI 映射 + BinFlow 载体对照；§7 页面×模式差距矩阵；§8 活体核验清单；§9 不做清单；§10 落地批次建议 |
| v1.1 | 2026-08-31 | **T-381 活体核验回写**（qa-engineer 执行，源 = t226-artifactory OSS 7.84.10 活体实例，DOM 实测）。V1~V8 全部核验，改契约级结论：**M1 决策项 A 撤销**（7.84 建仓 = 网格 modal 880px + 整页表单两段式，BinFlow 现形态已一致）；**M3 决策项 B 撤销**（7.84 用户/组创建 = 整页路由表单，非 modal）；**L2 ⋮ 菜单无对位**（7.84 行内 = 直删 trash 图标按钮，删除确认为 520px 轻量 message-box、无 type-the-key、Delete 键为绿色主按钮）；**D1 宽度修正 480px→50vw**（800px@1600 实测，Tab = Configure/Deploy/Resolve 三枚）；**F1 锚位证实 = 顶部居中单条 ~2-3s**（E7 转入「再议」）；**V5 证实侧栏条目带图标**（N2 维持改造）；**V7 关闭**（Builds 面存在但 BinFlow 不建；制品详情页签无依赖视图）。§7 矩阵按此重印，§8 附核验结论列。证据：`reports/agents/T-381.md` + `reports/agents/t381-evidence/` |
| v1.2 | 2026-09-01 | **T-402a 复制（replication）交互面锚定增补**（qa-engineer 执行，用户指令 2026-08-31 23:2x「replication 的交互要和 Artifactory 一致」）。新增 **§6A R 系条目 R1~R10**（逐项置信度）：活体实测高置信 = R1 入口拓扑（**仓级表单步骤节**，仓库编辑页 jf-steps 三步 Basic/Advanced/**Replications** 等宽 387px 横排；OSS 无全局复制管理页）、R2 OSS license 门形态（Replications 步 `is-disabled` 点击 no-op、REST `/api/replications*` 一律 400 Pro-only、UI-API `global/replications/config` 反而不受门）、R5 列表列形态（本地仓列表 **Replications 列**，OSS 每行 cell=「0」；启用态=bundle 实证 `icon-run` 图标链 + 三态 tooltip）、R7/R9 状态呈现与容器形态；OSS 门后不可达项中/中高置信（bundle 静态资产行为事实 + 公开 REST 双源，**未静默升格**）= R3 字段集（enabled/cronExp/enableEventReplication/pathPrefix/syncDeletes/syncProperties/syncStatistics）、R4 cron 校验（`GET /crontime?isReplication=`，Quartz 格式）、R6 Test 动作、R8 全局封锁开关（blockPush/blockPull）。§7 矩阵增「复制」专用行。**注意**：BinFlow 复制为事件驱动（无用户级 cron）——R4 对齐是后端语义决策非纯 FE parity。证据：`reports/agents/T-402a.md` + `reports/agents/t402-evidence/` |
| v1.3 | 2026-09-01 | **T-400 终验 L16 矩阵逐格终评落档**（qa-engineer 执行——T-381 共笔先例）。§7 矩阵正文保持 v1.2 原样（历史基线），其后新增 **§7A 终评表**：v1.2 时点仍标 △/✗ 的全部格子（D1 双入口 / L1 三处 / N2 / Tokens 四格 / 复制 M2·M3）按 M14 落地票据（T-382/T-386/T-387/T-388/T-404）翻 ✅ 或（豁·登记），**终评覆盖率 100%**；三出口（V5 不降级 / V7 关闭 / E7 再议）落档；E1~E7 逐条复核零倒退。证据：`reports/agents/T-400.md` |
| v1.4 | 2026-09-02 | **T-428 文面回写簇**（tech-writer 执笔，M1/M3 两处 ux 口径按 ux 册既有定案代笔——**ux 会签位**，未自创设计值；T-398 §4-2 登记）。**M1 行升级**：差距行尾注「网格 modal 宽度参考 880px（BinFlow 现档位即可）」升级为 **440px 紧凑档定案**（T-381 实测 880px 系 33 包型 90×90 大磁贴档 → T-383 票内定案不追平并写进 e2e 断言；T-390 磁贴卡面复活后新形态刷新——`.pkg-grid-item` 类名复线、宽度档复证不破）。**M3 行增补**：MUI 映射格补 **MUI Paper 代差注记**（repositories 域六节 Paper〔缺省 elevation 1〕vs security 域 `.form-section` CSS 留——mui-native-visual 换装分期既定口径，非 parity 缺口）。证据：`reports/agents/T-383.md`/`T-390.md`/`T-398.md` + `web/src/pages/repositories/RepositoryFormPage.tsx`、`web/src/pages/security/UsersPage.tsx` as-built 核对 |
| v1.5 | 2026-09-03 | **T-437 M16 翻案修订版**（ux-designer 主笔 + PM 会签位代笔——M16-SPLIT B1 前置锚票，FR-141.1/.2）。**版本对账**：PRD FR-141.2 与 `reports/m16-parity-audit-material.md` D 骨架所称「parity 册 v1.2」即本版——审计材料起草时点未同步本册自身版本线（彼时已至 v1.4），本版起以实序 **v1.5** 为准，映射留痕。修订六项：① **E5 前提修正**（Q5 出口①路由化——「BinFlow 已用路由实体表单」前提被 M16 审计推翻〔B-2.15〕，条目注销 + §3 M3 勘误）；② **E1 范围修正**（Q2 出口①——管理列表 vs 浏览器表分治文本 + T-434 children 表收窄 as-built 对账）；③ **三例翻案双留痕**：E6→双语可切换（Q3）/ E2→页码控件（Q4，§5 L4 联动注）/ R4 cron 引入（Q1——**推翻 M15 Q5 终裁与 T-402a 勘误，勘误原文存档不删**）；④ **§9A stay-out 登记**（A8 清单 + 仓库详情中间页/E4 测试器/快搜范围页签/结果计数一致性等八项 + 候裁子项）；⑤ **§11 K67 冻结**（树栈四项 as-built + TAB 省略规范形 + rclass 三态 + 分页控件形态锚——LC-98/T-451 断言地基）；⑥ **§12 B 47 项四态预归属表**（48 行全列含 3 对账去重行——零无主，供 QA 终验收口审计对账）。证据：`reports/agents/T-437.md` + `docs/prd/milestone-16.md` v1.1 §7 + `reports/m16-parity-audit-material.md` + `reports/agents/T-434.md` |

**v1.5 会签与就绪度（T-437，2026-09-03——PM 会签位代笔〔按 PRD v1.1 七裁定；PM 亲笔复核窗随 conductor 派单〕）**：

1. **PM 会签 · 行一（Q1~Q7 + Q13 归位核对）**：Q1 cron 引入（推翻 M15 Q5——§6A R4 翻案标注）/ Q2 E1 收紧不倒退（§9 E1 分治文本 + §9A-S5 as-built 对账）/ Q3 双语超集（§9 E6 改写）/ Q4 页码翻正（§9 E2 改写 + §5 L4 联动注 + §11.2 分页锚）/ Q5 路由化（§9 E5 注销 + §3 M3 勘误）/ Q6 两程结构（§9A-S7/S8 的 M17 预立项段联动条目）/ Q7 Annotate 加（§12 B-1.6 归属 T-444/T-455）——七项终裁在本册对应条目 **100% 留痕，Q 编号 ↔ 条目号双向可回溯**。
2. **PM 会签 · 行二（Q8~Q12 候裁核对）**：Q8 表单域全补倾向（B-1.5/B-3.12 → T-439，断言挂候裁附注）/ Q9 自有增强层逐项三态（§9A 八项 + 候裁子项清单——批次②③断言冻结前裁，裁毕回写本表）/ Q10 条件票不占 DoD（B-3.18 Last Login 列 → T-468 条件票）/ Q11 webhook 触发源（非本册面——PRD/ADR-0041 域）/ Q12 t381（conductor 决定项，非本册面）——候裁项**零冒进冻结**，断言面全部挂「候裁」附注。
3. **tech-lead 就绪度（ux 侧复核；正式确认候 B1 收口窗）**：批次②~④ + FE 副线断言地基齐备——§11 K67（树栈 as-built + 分页控件锚）供 dep 本票的九张 FE 票（T-439/T-441/T-443/T-445/T-447/T-449/T-451/T-453/T-455）直接引用；§12 归属表 48 行零无主（候裁 4 项均有 Q 编号或建议票 + 收口审计对账点）；批次②③④可拆性无阻塞项。

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
| MUI 映射 | 网格步 `Dialog`（Artifactory 实测 880px；**BinFlow 定案 440px 紧凑档不追平**——paper sx `min(440px, calc(100vw - 48px))` 钉死，v1.4 定案见差距行）+ 表单步**路由页**分节 `Paper` |
| BinFlow 载体 | `web/src/pages/repositories/RepositoryFormPage.tsx`——已是两段式：`pkg-grid` Dialog（选择包类型，带 `pkg-tier-*` 档位徽章；**T-390 卡面复活后形态**：`.pkg-grid-item` 类名复线，卡面边框/surface 底/hover/禁用置灰生效，可选磁贴 brand 图标 22px、门控磁贴 mono + opacity 0.4 + 徽章）→ **路由页** `/admin/repositories/new` 单页分区表单（常规/来源/成员/策略/治理/高级六节 `Paper`）；rclass 由 URL query 预选 + 单选组。 |
| 差距 | ✅ 已有（**v1.1 核验改判：决策项 A 撤销**——7.84 实测即「网格 modal → 整页表单」两段式，BinFlow 现形态与 Artifactory 一致，无需收单 Dialog）。保留微差注记：Artifactory 的 rclass 由入口下拉选定，BinFlow 用 URL query + 页内单选组——手势等价，不改。网格 modal 宽度**定案升级（v1.4，ux 会签位）**：Artifactory 880px 系 33 包型 90×90 大磁贴档位；BinFlow 5 核心 + 8 门控槽位、190×44 高密度卡磁贴（icon+名+述+徽章）——**定案 440px 紧凑档、不追平 880px**（追平即大面积留白；T-383 票内定案并写进 e2e 断言 `toBeCloseTo(min(440, vw-48))`，T-390 磁贴卡面复活后宽度档复证不破——宽度由 paper sx 钉死，磁贴形态变化不外溢）。原「宽度参考 880px（BinFlow 现档位即可）」措辞据此收口。**v1.5 注（T-439 as-built，B-3.11/Q9 兑现 + B-2.5 三段）**：表单已升级 **Basic | Advanced | Replications 三段步进条**（Artifactory 7.161.20 :8082 活体复核——jf-steps 三步条形态证实，页脚 = Cancel + Create 两钮**无 Reset**）；BinFlow footer 重置钮随票移除（页脚两钮对齐 M1 锚点）；复制配置内嵌节迁第三步（M6 语义零变化）。 |

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
| MUI 映射 | 路由页分节表单（`Paper` 节 + 页脚动作条）；token 面复用 SetMeUp 的 `smu-token-panel` 形态。**MUI Paper 代差注记（v1.4，ux 会签位）**：分节容器的 Paper 化按域分期——建仓/编辑表单六节已是 `<Paper component="section">`（缺省 elevation 1 档；T-344 批 D 换装、T-383 加 `form-section-*` 锚），用户/组表单维持 CSS 分区排版（`security.css` `.form-section`——mui-native-visual 批次标「留」）。该代差是 MUI 换装的既定分期，非 Artifactory parity 缺口（Artifactory 自绘 jf 表单面板、无 elevation 概念，节界视觉不进对齐面）。 |
| BinFlow 载体 | 用户：`UsersPage`（列表页内建表单）+ `UserDetailPage`（路由页编辑）；组：`GroupsPage`（同列表页形态）；权限：`PermissionEditorPage`（路由页——BinFlow 自有的模式测试器/diff 确认是增强，**豁免**）；Tokens：`PlaceholderPage`（P2 占位）。 |
| 差距 | ✅ 形态已对齐（**v1.1 核验改判：决策项 B 撤销**——7.84 用户/组创建即整页路由表单，BinFlow 无需 modal 化；BinFlow 列表页内建表单与路由页编辑的差异属同档形态，可保持）。**v1.5 勘误（Q5，2026-09-02 终裁路由化）**：上句「同档形态，可保持」的 BinFlow 侧前提不实——用户/组创建实为**列表页内联展开卡**（M16 审计 B-2.15；v1.1 的 V6 核验只证 Artifactory 侧为路由表单，未核 BinFlow 侧载体），E5 豁免前提随之失效（§9 E5 注销）。Q5 出口①：`/users/new`、`/groups/new` 整页路由表单（FR-145.1/T-453 承载——断言反转④），内联卡退役；BinFlow 内部形态统一（权限创建已是路由页）；编辑面路由页维持对齐不变。Tokens 页 ✗ **缺失**（BinFlow 侧缺口维持，随 M3 规格落真身：token 列表表 + 铸币区 + 一次性明文面板 + 吊销确认；形态参照 7.84 profile 页的表+生成区，V6c 生成表单字段集因 profile 密码锁未核验，接线票时以 `smu-token-panel` 既有形态为准）。 |

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
| 差距 | ~~✅ 已有（有意偏离：keyset 分页对页码不友好，BinFlow 形态保留；豁免登记 §9）~~ **v1.5 翻案（Q4，2026-09-02 用户终裁，出口①翻正）**：E2 豁免注销——**分治口径 = 「管理列表/结果表用页码控件，制品树/大目录深浏览维持增量加载」**（Artifactory 同为树增量 + 表页码双轨）。管理列表与结果表页码控件 ×9 处统一（LC-98 / T-451——断言锚见 §11.2；后端维持 keyset 游标 + 页窗映射，呈现对齐语义自有 C 注）；console-ux §6 大目录策略条款的回写归 T-451 票内（锚册 v1.32 预登记）。 |

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

## 6A. 复制面（R 系）——T-402a 活体锚定（2026-09-01）

**核验源与证据等级**：t226-artifactory（Artifactory OSS **7.84.10** rev 78410900，T-228 保留栈）差集法只读探测（INC-1 纪律：永不点确认/保存/触发类按钮）。**关键前提**：该实例 license 为 OSS——`REPO_REPLICATION`/`MULTIPUSH_REPLICATION`/`EVENT_BASED_PULL_REPLICATION` entitlements 全 false，复制 REST（`/api/replications*`）一律 400 Pro-only，UI 的 Replications 步被硬禁用——**字段级表单形态在活体上不可达**。因此 R 系分两档置信度：**高** = 活体 DOM/网络实测；**中/中高** = 实例公开下发的前端静态资产行为事实提取（clean-room：只析标签/字段/端点等行为事实，零代码拷贝——T-381 内联 SVG 分析同例）+ JFrog 公开 REST 文档双源，**不以记忆单源升格**。测量数据全量见 `reports/agents/t402-evidence/t402-measurements.json`。

### R1 复制配置入口拓扑（仓级表单步骤节，非全局页）

| | |
|---|---|
| Artifactory 行为（置信度：**高**——活体实测） | 复制配置入口在**仓库编辑页的左轨步骤条**：`/ui/admin/repositories/local/{key}/edit` 顶部 `jf-steps` 横排三步 **Basic（is-active）/ Advanced / Replications**，各步等宽 **387px**、y≈146、高 40px（1600 视口），`data-cy="panel-<Title>"`；Replications 是**仓库表单的一个节**（表单步骤常量 `{BASIC:'basic',ADVANCED:'advanced',REPLICATIONS:'replications',…}`，节间切换走 `repo-form-change` 事件），**不是独立路由页**（`/replication` 子路由变体全 404）。OSS 管理域侧栏（五组：Projects/Environments/Repositories/User Management/General+SERVICES）**无全局复制管理入口**；MC「Replication」页在导航词表内但 `HIDDEN_WHEN_SERVICE_NOT_INSTALLED`（OSS 无 MC）。编辑页加载即拉 `GET /ui/api/v1/ui/global/replications/config`（全局封锁态，见 R8）。 |
| MUI 映射 | 仓库表单页内 `Stepper(non-linear)` 或节导航 + Replications 节内嵌面板（非 Dialog/Drawer） |
| BinFlow 载体 | 仓库详情 `RepoDetailPage.tsx` 有 `replications` Tab——但是**降级指针卡**（「本仓的复制配置由全局复制页承载…前往复制管理 →」，`repo-repl-degraded`）；真正的面在全局治理页 `/admin/governance/replication`（`ReplicationPage.tsx`，只读状态）。 |
| 差距 | △ 需改造（**拓扑与 Artifactory 相反**：仓级表单节 vs 全局页模型——②实现段二选一：A. 仓详情 Tab 升级为该仓的配置列表+表单（对齐拓扑）；B. 保持全局页但仓 Tab 呈现该仓过滤视图。交 PM/用户裁）。 |

### R2 OSS/Pro license 门形态（BinFlow 不适用，留档）

| | |
|---|---|
| Artifactory 行为（置信度：**高**——活体实测） | OSS 下 Replications 步 = `jf-steps__item is-disabled`（文本 40% 透明、`cursor:default`、指示条灰 `#f1f3f8`；**无 `aria-disabled`**、hover **无任何门文案 tooltip** 渲染）；点击 **no-op**；REST `/api/replications`、`/api/replications/{key}`、`/api/replication/configs` 一律 **400** `"This REST API is available only in Artifactory Pro…"`。反差：UI-API `GET /ui/api/v1/ui/global/replications/config` **不受门**（200，见 R8）；本地仓表单模型携带 `replications:[]`（未被 license 剥字段）。 |
| BinFlow 载体 | 无 license 概念；复制有实例配置段开关（段缺失 → 状态页 501 降级卡）。 |
| 差距 | `·` 不适用（BinFlow 开源对标，无 Pro 门可对齐；对齐目标即 Pro 形态——按 R3~R8 的 bundle 锚定执行）。 |

### R3 复制表单字段集（push/pull 目标配置）

| | |
|---|---|
| Artifactory 行为（置信度：**中**——bundle 字段模型实证 + 公开 REST 文档双源；活体 OSS 不可达，表单面板永不渲染） | 复制配置字段族：**enabled**（启停开关）、**cronExp**（cron 表达式，见 R4）、**enableEventReplication**（事件复制开关——cron 兜底之外即时触发）、**pathPrefix**（路径前缀过滤）、**syncDeletes**（**删除同步/enabled 无仓剔除**——目标端同步删源端已删路径）、**syncProperties**（属性同步）、**syncStatistics**（统计同步）、**type**（push/remote=pull）+ 凭据 username/password + 目标 URL/仓对。remote（pull）仓默认对象（bundle 原文）：`{enabled:!1, cronExp:"", enableEventReplication:!1, pathPrefix:"", syncDeletes:!1, syncProperties:!1, syncStatistics:!1, type:"remote"}`；本地（push）仓默认 `replications:[]`（数组——multipush 多目标）。 |
| MUI 映射 | 表单节内 `TextField`（URL/仓对/凭据/pathPrefix）+ `TextField`（cron）+ `Switch/Checkbox` 族（enabled/事件/三个 sync） |
| BinFlow 载体 | 后端 `ReplicationConfig`（internal/replication/model.go）：Name/SourceRepo/TargetURL/TargetRepo/TargetUsername/TargetPasswordEnc/**MaxBandwidthBytesPerSec/MaxItemsPerPush**/Enabled——**无 cronExp、无 pathPrefix、无 sync 族、无事件开关**（纯事件驱动）；UI **无任何 CRUD 表单**（REST-only，代码注释明示）。 |
| 差距 | △ 字段集双向错位：BinFlow 缺 cron/pathPrefix/sync 族（对齐需后端模型扩展）；带宽节流/批量上限是 Artifactory 无的**超集**（保留）。表单 UI 本身 ✗ 缺失（②段主工面）。**v1.5 对账注（T-437）**：表单 UI 已随 T-404 落（仓编辑页 `form-section-replications` 内嵌节——锚册 v1.25）；六字段中 **cronExp 预留位随 Q1 终裁转正为真字段**（FR-150.3 ③ 复制用户级 cron——T-450 承载），enableEventReplication/pathPrefix/sync 三开关**维持预留缺位**（BinFlow 事件驱动语义自有，候裁不伪造——零静默升格纪律同源）。 |

### R4 cron 表达式形态与校验

| | |
|---|---|
| Artifactory 行为（置信度：**中高**——bundle 端点 + 帮助文案实证；crontime 活体直连未复现成功，未验成功不升格） | cron 为 **Quartz 格式**（帮助文案直链 quartz-scheduler.org CronTrigger 2.3.0 教程）；**服务端校验**：`GET {api}/crontime?cron=<表达式>&isReplication=<bool>`（isReplication 区分复制调度语义）；校验失败文案 `"The cron expression is invalid"` / `"Please enter a valid cron expression"`。复制为 **cron 定时 + 事件（enableEventReplication）双轨**。 |
| BinFlow 载体 | **无用户级 cron**——引擎事件驱动（上传 hook 入队）+ 固定间隔 sweep 兜底（`Engine.SweepInterval` 默认 1m，崩溃恢复用）。 |
| 差距 | ~~✗ 缺失（语义级——PM 立项评审项，见 T-402a 报告工料包 B）~~ **v1.5 翻案（Q1，2026-09-02 用户终裁引入——推翻 M15 Q5 终裁与 T-402a 勘误）**：cron 调度域转正——**FR-150 / LC-91 / ADR-0044** 承载（T-446 引擎 + T-450 三消费面 BE + T-462 FE；表达式子集 = 本条 Quartz 锚对拍，T-435 规格票）。**T-402a 勘误标注（推翻留痕——勘误原文存档不删，效力自此以本条为准）**：勘误「事件驱动唯一引擎、无用户级 cron、Replicate Now 承接手动全量」被 Q1 终裁推翻，改写为「**事件驱动 + outbox 仍是增量唯一引擎；调度域只触发全量类任务**（GC 全量/备份/复制全量同步），同制品不双推（NFR-P74 / L48 零重复投递断言）」——推翻链三处留痕：BOARD / PRD §5.4 断言反转⑦ / 本条。R 系对账：R5（Run/Replications 列）/ R6（Test）/ R8（全局封锁）已随 M14 T-404 与 M15 T-422 落地；**R 系仅 R4 域翻案在场**，R3 的 cronExp 预留位同场转正（见 R3 v1.5 对账注）。 |

### R5 Replicate Now 类动作形态

| | |
|---|---|
| Artifactory 行为（列表列形态置信度：**高**——活体 DOM + bundle 渲染器；面板内按钮形态：**中**——bundle，OSS 不可达） | ① **本地仓列表有 Replications 列**（列序 Repository Key/Type/Project/Environment/**Replications**/Shared With，表头 x=1224 w=140@1600 实测）；OSS 每行 cell = 纯文本 **「0」**（cell renderer 的未启用分支 `<span>0</span>`）。② 启用态 cell（bundle 实证）= `a.icon-run.replication-column-button`（`id="repositories-local-replicate"`，24px 图标，hover `#43a047`，disabled `opacity:.3`）+ el-tooltip 三态文案：全局封锁→**"Push Replication Is Blocked"**、已启用→**"Run Replication"**、未配置→**"No Replication Configured"**；点击 emit `execute-replications` → `POST {api}/admin/repositories/executereplicationnow?replicationUrl=<url>`（body=repoModel），响应 `info` → toast 通知。另有 multipush 全量 `executeall`（repoKey+replicationUrl 参数）与 remote 触发 `exeucteremotereplication`（**官方路径原文如此**）。 |
| MUI 映射 | 列表列 `IconButton(PlayArrow)` + `Tooltip` 三态 + 行内触发；或表单节内 Button |
| BinFlow 载体 | **无任何手动触发面**（引擎只吃上传事件 + sweep；REST 无 trigger 端点）。 |
| 差距 | ✗ 缺失（对齐需后端 trigger 端点 + FE 列内/按钮——工料包 B；纯 FE 无法先行）。 |

### R6 Test 连通性动作

| | |
|---|---|
| Artifactory 行为（置信度：**中**——bundle API 层实证；表单侧按钮形态 OSS 不可达） | 保存前可测目标连通性：`POST {api}/admin/repositories/testlocalreplication?replicationUrl=<url>`（body=repoModel 携 `replications:[cfg]`）；remote 对位 `/admin/repositories/testremotereplication`。 |
| BinFlow 载体 | 无（REST/引擎均无 test 面）。 |
| 差距 | ✗ 缺失（工料包 B：后端探测端点 + FE 表单侧 Test 按钮——凭据录错即配置全废的体验痛点，优先级中）。 |

### R7 状态/列表呈现（状态徽章/最后执行）

| | |
|---|---|
| Artifactory 行为（置信度：**中高**——列词表 bundle 实证 + OSS 活体列在场） | 词表含 **"Last Replication Status"** 列标签；dashboard widget 词表 `{LABEL:"Replications", DESCRIPTION:"Monitor the status of active replications occurring across your multiple deployments and review historical replication runs."}`（MC 数据支撑，OSS 不渲染）。OSS 活体实际呈现 = 列表 Replications 列的「0」。 |
| BinFlow 载体 | `ReplicationPage.tsx` 已是**超集**：目标表（状态点 已停用/异常/复制中/排队/正常 + URL + 源→目标仓对 + pending/进行中/失败/累计成功/上次成功）+ 事件表（时间/状态 badge/制品路径/sha256/尝试次数/错误原因），10s 轮询 + 四态收敛（loading/403/404+501/stale 保留）。 |
| 差距 | ✅ 已有（BinFlow 状态面比 OSS 实际可呈现的更全；Artifactory 完整状态页挂 MC（§9 不建），**不必倒退对齐**）。 |

### R8 全局封锁开关（blockPush/blockPull——应急刹车）

| | |
|---|---|
| Artifactory 行为（置信度：**中高**——UI-API 活体 200 + bundle 字段/tooltip 双源） | `GET /ui/api/v1/ui/global/replications/config` → `{blockPullReplications, blockPushReplications}`（**OSS 亦可达**）；两开关为 General Settings 表单字段族成员（与 fileUploadMaxSize/folderDownload*/globalOfflineMode 同页），tooltip：**"When set, push/pull replication will not be triggered regardless of configuration."**；仓库模型另有仓级 blockPush/blockPull（remote 仓）。 |
| BinFlow 载体 | 无全局封锁开关（仅 per-config Enabled）。 |
| 差距 | ✗ 缺失（**应急刹车型**功能：故障场景一键停所有出站复制。工料包 B，后端 config 面 + FE 开关，小面）。 |

### R9 面板容器形态（弹窗 vs 抽屉 vs 内嵌）

| | |
|---|---|
| Artifactory 行为（置信度：**高**——活体拓扑） | 复制配置 = **整页路由表单内的步骤节（内嵌面板）**；列表触发 = 行内图标；**无 modal/drawer 承载**、无独立复制路由。 |
| BinFlow 载体 | 全局治理整页 + 仓详情 Tab 内嵌卡——同为内嵌形态。 |
| 差距 | ✅ 已有（容器形态一致；②段补表单时**沿用内嵌/整页**，勿 modal 化——与 M1/M3 整页结论同向）。 |

### R10 语义模型（push 方向与仓对）

| | |
|---|---|
| Artifactory 行为（置信度：**高**——模型形状活体+bundle 双证） | push = 源仓（本地）→ 目标实例 URL + 目标仓 key，**multipush 多目标数组**；pull = remote 仓属性（`replications[0]` 单元素、`type:"remote"`）。 |
| BinFlow 载体 | push 目标模型 source_repo/target_url/target_repo 完全同构（ADR-0021）；无 pull 复制概念（remote 仓缓存拉取是另一能力域，已有）。 |
| 差距 | ✅ 已有（push 同构；pull 不建——remote 缓存已覆盖用例）。 |

---

## 7. 差距矩阵（页面 × 模式）——PM 起 M14 UI-parity PRD 直接引用（v1.1 核验后重印；v1.2 增复制行）

图例：✅ 已对齐 ｜ △ 需改造 ｜ ✗ 缺失 ｜ `·` 不适用 ｜（豁）= BinFlow 有意偏离且豁免（§9）
**v1.1 重印说明**：本版矩阵已吸收 T-381 活体核验结论（V1~V8）；与 v1.0 的差异均以「v1.1」标注。三出口判定见 §8 表下注。**v1.2 增**：复制面专用行（§6A R 系）。**v1.5 注**：用户/组行 M3 格的 ✅ 系 **Artifactory 侧**形态核验结论（矩阵正文保持历史基线不动）；BinFlow 侧内联卡载体经 Q5 终裁路由化收口（T-453——断言反转④）——见 §3 M3 v1.5 勘误与 §12 表 B-2.15 行。

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
| 复制 `/admin/governance/replication` + 仓详情 Replications Tab（**v1.2 R 系**） | `·` | △（②段补删除配置确认——沿 E1 输入 name 档，Artifactory OSS 无对位可观测） | △（②段补配置表单：**整页/内嵌节形态**〔R9〕，字段按现有 REST 集 + R3 缺口标注；仓 Tab 从指针卡升级需 R1 拓扑裁定） | `·` | `·` | ✅（R7 状态表+事件表超集） | `·`（R5 列内触发图标属后端 trigger 前置项，不挂 FE parity 旗） | ✅ |
| 存储/系统信息/License `/admin/{monitoring,general}/*` | `·` | `·` | `·` | `·` | `·` | `·` | `·` | ✅ |
| 全局壳（N 系） | `·` | `·` | `·` | `·` | `·` | `·` | `·` | N1 ✅ / N2 △（图标槽；V5 已证 7.84 一级条目带图标，维持）/ N3 ✅ |
| 依赖树 D3 | —（v1.1 关闭：依赖视图挂 Builds 面（§9 永不建），制品页签无对位已实测） | | | | | | | |

**模式级汇总（v1.1 重排；v1.2 增补）**：核验后**唯一高优先 parity 差距 = D1 抽屉族**（Set Me Up 居中 Dialog → 右抽屉 50vw + 补 Resolve Tab）；**次级 = Tokens 页真身**（BinFlow 侧缺口）；原 M1 建仓单 modal 化、M3 用户/组创建 modal 化、L2 ⋮ 菜单三个改造项的 parity 依据经核验**不成立，撤销**（BinFlow 现形态即 Artifactory 形态，或 Artifactory 无对位）；低优先 = L1 列选/刷新、N2 侧栏图标（一级条目档位）、F2 空态插画位。**v1.2 增（R 系，T-402a）**：复制面 = 状态呈现 ✅ 超集（R7）+ 容器形态 ✅（R9）+ push 模型 ✅（R10）；**UI CRUD 表单 ✗**（R1 拓扑裁定 + R3 字段集——FE 为主）；**cron/Replicate Now/Test/全局封锁四项 ✗ 且均为后端语义前置**（R4~R6/R8——超出 FE parity 票，交 PM 立项评审；BinFlow 事件驱动模型是否引入 cron 双轨是产品决策非纯对齐）。**v1.5 修正（T-437）**：R5（Run/列内触发）/ R6（Test）/ R8（全局封锁）已随 M14 T-404 与 M15 T-422 落地；R4 cron 经 **Q1 终裁翻正立项**（FR-150/ADR-0044——§6A R4 v1.5 翻案标注，推翻 M15 Q5 留痕）；E2/E6 翻案、E5 注销见 §9——翻案后唯一存活的「UI CRUD 缺口」面收敛为零。

### 7A. T-400 终评表（v1.3，2026-09-01——L16 逐格终评落档；矩阵正文保持 v1.2 历史基线不动）

上图 v1.2 矩阵中仍标 △/✗ 的格子，按 M14 落地票据逐格终评（**覆盖率 100%**；全部非豁免 △/✗ 已翻 ✅，范围外/有意保持者以（豁·登记）收口）。证据 = T-400 复跑或票据承证，逐格见 `reports/agents/T-400.md` §AC1。

| v1.2 格 | 终评 | 依据（票据 + T-400 复核） |
|---|---|---|
| 制品树 D1（决策项 C：Deploy 抽屉化评估） | **（豁·暂行定案）** | Q3-C 维持暂行（PRD §7）：Deploy 有意保持居中 Dialog——上传进度表在抽屉过窄；T-382 零改动遵守；终裁窗开放，用户推翻才开票 |
| 仓库列表 D1（行内 Set Me Up 入口待抽屉化） | ✅ | T-382 落地（行/详情入口均开右抽屉 50vw 档）；T-400 复跑 `setmeup-deploy` 7/7 绿（含 armed 真门腿） |
| 仓库列表 L1（列选/刷新缺） | ✅ | T-387 落地（列选 Menu + 全选复位 + 刷新钮 + per-page localStorage）；T-400 全量含 t387 spec 绿 |
| 仓库详情 D1（Set Me Up 入口） | ✅ | 同 T-382（详情入口同一抽屉） |
| 用户/组 L1、权限 L1（△） | **（豁·登记）** | L1 首期范围 = 仓库/审计两页（PRD FR-125.2「列多者受益」）；其余列表推广系 T-387 遗留候选（columnPrefs 共享层已就绪），非 M14 DoD |
| 审计 L1（列选缺） | ✅ | T-387（审计页列选 + 刷新，e2e 断言 reload 持久） |
| Access Tokens M3（✗ 占位） | ✅ | T-386 真身（创建 modal + 一次性明文仅展示一次 + 吊销双出口 + 会话台账；`PlaceholderPage` grep=0）；字段集 V6c 降级暂行（Q4 维持——OSS 无 admin token 面可核验） |
| Access Tokens L1（✗）/ L2（✗）/ F 系（✗） | **（豁·登记）**/ ✅ / ✅ | L1：台账为会话内存态（刷新即空），列选无对象——登记不建；L2：无 ⋮ 无行内直删（E1 家族对齐）；F：空态插画槽 T-388 落地（八列表页 15 落点） |
| 复制 M2（△ 补删除确认） | ✅ | T-404：E1 输入 name 强确认（错名禁用/对名放行 + 取消腿） |
| 复制 M3（△ 补配置表单） | ✅ | T-404：R1 裁定形态落地（仓编辑页 `form-section-replications` 内嵌节 + R3 字段两档 + 预留位恒禁用零提交）；T-405 PUT 启停联合腿 live 绿（T-396 L19 承证） |
| 全局壳 N2（△ 图标槽） | ✅ | T-388：18 条一级条目 16px mono currentColor（V5 实测档位——仅一级条目，子项/分组标签不配） |
| 依赖树 D3 | 关闭维持 | V7 出口（挂 Builds 面，§9 永不建）；BinFlow 维持不建 |

**三出口终评落档**：V5 **不降级**（T-388 已落）/ V7 **关闭**（D3 维持不建）/ E7 **再议维持不改**（BinFlow toast 右下堆叠为有意设计，待用户推翻信号）。**E1~E7 豁免终核：零倒退**（T-400 代码面逐条复核：`MoreVert` 全树 grep=0、无页码控件、`badge neutral` 在位、PermissionEditorPage/UserDetailPage 路由页在位、中英混排维持、ToastContext 右下锚位维持；详见 T-400 报告 §AC2）。

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
| E1 | 删除类动作不进**管理列表**行内 / 不做一键删（**v1.5 范围修正，Q2 出口①——管理列表 vs 浏览器表分治**） | 危险动作显式化（P5）；制品不可变，无撤销。v1.1 实证加码维持（7.84 行内直删 trash + 单击一次确认 + 绿色 Delete——V4/V8，Artifactory 原生更激进）。**v1.5 分治文本**：① **管理列表**（仓库/用户/组/权限/token 等实体列表）——删除不进行内、收详情危险区（输入 key 强确认 + 影响面摘要），现行态维持；② **浏览器面**（制品树/children 表/属性行）——v1.0~v1.4 期间 children 表每行红色「删除」钮与 ① 文本相抵（审计 C2-b 坐实），Q2 终裁**收紧不倒退**：浏览器面行内删除件须过危险确认（输入名档）或收进详情面板/右键菜单。**as-built 对账（T-434，2026-09-02）**：children 表操作列（详情/下载/删除三钮）已退役——删除收敛进详情面板与右键菜单（两者都过危险确认）、下载在右键与详情、详情入口经行点击/树叶子——新文本零违例；NFR-S78 全删除面按本文本复核（L35/L37/L44） |
| E2 | ~~「加载更多」增量分页（非页码控件）~~ **v1.5 翻案改写（Q4，2026-09-02 用户终裁，出口①翻正）——豁免注销** | **页码控件 ×9 处统一**：FR-144.7 / LC-98 承载（T-451 共享分页组件 + ×9 消费点迁移——消费点清单票内 grep 盘点入锚册，实际多于 9 以盘点为准）。**语义自有 C 注**：后端维持 keyset 游标 + 页窗映射（呈现对齐、深翻页 offset 扫描成本规避）；分治口径与控件形态断言锚见 §5 L4 v1.5 注 + §11.2；console-ux §6「加载更多」条款回写归 T-451 票内（锚册 v1.32 预登记——防失锚） |
| E3 | 仓库类型 badge 用中性色 | 颜色预算留给状态（console-ux §7.1） |
| E4 | 权限编辑器为整页 + 模式测试器 + diff 确认 | BinFlow 增强面，Artifactory 无对位（**stay-out 确认 §9A-S1**——A8/Q9；T-455 两步弹窗只换入口形态，编辑器本体零删除断言承载） |
| E5 | ~~用户/组创建与编辑均保留页面形态~~ **v1.5 注销（Q5，2026-09-02 终裁路由化）——豁免条目除名** | **前提失效**：v1.1 改写所依据的「BinFlow 已用路由实体表单」被 M16 审计推翻（用户/组创建实为列表页内联展开卡——B-2.15；V6 只核了 Artifactory 侧）。Q5 出口①：`/users/new`、`/groups/new` 整页路由表单（FR-145.1 / T-453——断言反转④），内联卡退役。**路由化兑现后该面为对齐项非偏离项**（Artifactory 创建即路由表单——V6）；编辑面路由页维持对齐不变；本条目自豁免清单除名留档，M3 勘误同场（§3） |
| E6 | ~~UI 文案中文 + 术语保留英文原词~~ **v1.5 翻案改写（Q3，2026-09-02 用户终裁——超集出口）——单语豁免注销** | **双语可切换**：zh 默认维持（零语义变化——现行文案原样迁移）+ en 资源包对齐 Artifactory 英文形态；**术语两包保真**（repo key / deployment / checksum / Set Me Up 等英文原词在两包中一致——console-ux §1.2 命名对齐条款升格为两包条款，升格回写归 T-463/T-464 票内，锚册 v1.32 预登记）。承载：FR-149 / LC-97（T-463 框架 + 文案外提 100% + CI 断言；T-464 双包 + 切换器 + 断言双语化）；断言反转⑥——默认 locale（zh）全量断言维持零翻新、en 抽样腿、锚 id 与文案解耦（锚册零改名）。**非单语倒退**：翻案出口 = 超集（可切换），中文产品定位维持 |
| E7 | toast 锚位暂保留右下 | **v1.1 转「再议」**：V3 实证 Artifactory = 顶部居中单条 ~2-3s 无堆叠；BinFlow 右下堆叠 + error 常驻为有意设计，默认不改；是否对齐交 PM/用户（改动成本一行级，见 §8 三出口） |

### 9A. stay-out 登记（M16 / A8 清单确认——v1.5，2026-09-03，T-437）

A8 = Artifactory 无对位（建了不对齐）或对位即要避开的形态。M16 翻案语境下逐项确认维持（PRD §0.2——Q9 裁点；已定项标依据，倾向项标「候 Q9」）。**登记即契约**：后续票不为其建载体；翻案须 Q 裁定 + 本册版本递增双留痕（§1.4 条款 1 同源纪律）。

| # | stay-out 项 | 处置 | 依据 / 候裁 |
|---|---|---|---|
| S1 | E4 权限编辑器整页 + 模式测试器 + diff 确认 | **维持**（BinFlow 增强面，Artifactory 无对位） | A8；T-455 只换「新建权限/添加仓库·用户组」入口为两步弹窗，编辑器本体 + 测试器/diff 零删除断言承载（T-455 AC3） |
| S2 | L2 行内 ⋮ 菜单 | **维持不建** | V4 实证 7.84 行尾无 ⋮（= trash 直删——恰是 E1 要避开的激进形态）；「复制 key / Set Me Up 行内快捷」留自有增强票，不挂 parity 旗 |
| S3 | 结果计数一致性（header/footer 恒一致） | **维持**（不对齐 Artifactory 实测 header 计数滞后错位缺陷——BinFlow 更优） | A8 明细；B-3.17 |
| S4 | 仓库详情中间页（`/:key` 概要/配置/Replications） | **维持自有**（候 Q9 终裁确认——PM 倾向维持） | B-3.13；Artifactory 行点击直进编辑、无详情层；BinFlow 附加层是配置/危险区/Set Me Up 的载体——拆层即信息架构重排，非对齐收益 |
| S5 | children 表复合形态 | **收窄已落**（T-434 as-built）：操作列退役（删除→详情面板/右键、均过危险确认；下载→右键/详情）；表本体保留为目录浏览增强（Artifactory 右侧纯 item view；BinFlow 目录给「直系概要 + 表」双呈现） | Q2 出口① + Q9 倾向收窄已兑现；E1 分治文本见 §9 E1 |
| S6 | disable 快照契约翻转 | **维持关闭** | T-364 钉死；无用户推翻信号（A8） |
| S7 | 快搜范围页签（Artifacts/Packages/Builds） | **维持缺位**（R2 类型化落地后再现——既定设计；Builds 页签 dep Build-info 域） | B-2.14 分拆注记；M17 预立项段随域解禁 |
| S8 | 无数据源不伪造族：Module ID 字段 / Any Distribution 预置 / Project·Environment·Shared With 列 / Realm 列 | **维持缺位登记**（域落地后随域解禁） | PRD §1.4 条款 2 / §0.3——Build-info / Release Bundle / Projects 域归 M17 预立项段；Environment 列与 FR-143.2 Environments 字域联动（T-439 票内核对，缺位不伪造） |

**候裁子项（非 stay-out——Q9 裁点挂起，批次②③断言冻结前裁，裁毕回写本表）**：表单 footer 重置钮（PM 倾向移除——T-439 兑现 + M1 行联动留痕）/ mimeType 与校验徽标块（PM 倾向收进下载伴随——T-447）/ 详情「类型/子项/修改时间」列去留（T-445 票内）/ 组·权限列表行内「编辑」钮超集（L2/E1 家族延伸——V4 实证 Artifactory 行尾仅 trash，候 Q9 复核收敛或维持）。

## 10. 落地批次建议（供 PM 排 M14 拆票；全部为 FE 票，不动后端）——**v1.1 按核验结论重排**

- **批 1（高置信形态对齐，PRD 可直接断言）**：① **D1 Set Me Up 抽屉化**（唯一存活的批 1 parity 票：壳 `Dialog` → 右 `Drawer`，宽 `clamp(480px, 50vw, 800px)`，**Tab 补第三枚 Resolve**，底栏「返回链接 + Done」对齐；含 Deploy 对话框评估——决策项 C：建议 Deploy 保持居中 Dialog，上传进度表在抽屉里过窄，v1.1 实测 7.84 制品页 Set Me Up 与 Deploy 并列，建议维持）。~~② M1 建仓向导单 Dialog 化~~（**v1.1 撤销**：V2 证实现形态已对齐）；~~③ M3 用户/组创建 modal 化~~（**v1.1 撤销**：V6 证明 7.84 即整页表单）；~~④ L2 行内 ⋯ 菜单~~（**v1.1 撤销 parity 旗**：V4 证明 7.84 无 ⋮；复制 key/Set Me Up 行内快捷可留作可选自有增强票，不挂对齐目标）。
- **批 2（补缺与低优先）**：⑤ Tokens 页真身（M3 规格；v1.1 补参照形态：生成区 + token 表（Description/Token ID/Issued At/Expiry Date）+ 一次性明文面板 + 吊销确认）；⑥ L1 列选器 + 刷新（仓库/审计先行；v1.1 旁证：7.84 用户/Builds 列表均有 Customize Columns）；⑦ F2 空态插画槽位；⑧ N2 侧栏图标槽（**V5 已核验：照排**，档位=一级条目）。
- **批 3（核验后微调）**：**F1 toast 锚位——E7 再议中**（V3 实证差异，默认不改，待 PM/用户定夺）；~~M1/M3 细节修正~~（V2/V6 核验后无遗留修正项；唯一残留 = V6c token 生成表单字段集，随 ⑤ 接线票以 `smu-token-panel` 既有形态覆盖）。
- **批 4（v1.2 增：复制面 T-402 ②实现段，候 FE lane）**：**⑨ 复制 CRUD UI**（包 A——表单〔现有 REST 字段集：name/源仓/目标 URL/目标仓/凭据/enabled/带宽/批量上限〕+ 删除 ConfirmDialog〔E1 输入 name 档〕+ 仓详情 Tab 升级〔R1 拓扑二选一，PM 裁〕；**阻塞点 = REST 无 PUT**——启停需先落 mini 后端票）；**⑩ cron/Replicate Now/Test/全局封锁**（包 B——四项均后端语义前置〔R4~R6/R8〕，PM 立项评审后再拆 FE 腿；工料详见 `reports/agents/T-402a.md` §5）。**v1.5 注（T-437）**：⑩ 已全部兑现/转正——Replicate Now/Test/全局封锁随 M14 T-404 与 M15 T-422 落地；cron 经 **Q1 终裁转正** FR-150（M16 T-446/T-450/T-462——ADR-0044 承载，§6A R4 翻案标注）。
- 品牌资产接线（UX-1 另两交付）随批 2 并行：logo 落地清单见 `docs/design/brand/logo/README.md` §4；包型图标接线注意见 `docs/design/brand/package-icons/README.md` §6（低置信三枚已于 2026-08-31 活体对照修正，见该 README §4）。

---

## 11. K67 冻结：树栈 as-built 断言锚 + 分页控件形态锚（v1.5，2026-09-03，T-437）

> K67（PRD §5.6）本义 = 树头工具带断言锚。本节冻结范围扩为**树栈全套 as-built 定案**（T-434 已落码 + t226 活体对照 + 锚册 v1.31 登记块）**+ 分页控件形态锚**（Q4 出口①——LC-98/T-451 断言地基）。**冻结即契约**：dep T-437 的 FE 票（T-439/441/443/445/447/449/451/453/455）断言直接引用本节；偏离 = 改契约，须回本册版本递增。锚名权威源 = console-ux §10.5 T-434 批登记块（本节不复列锚名清单）。

### 11.1 树栈四项 as-built（T-434 已落——断言反转①；spec = `web/e2e/m16/t434-tree-stack.spec.ts` 5 腿）

| # | 断言面 | as-built 定案（冻结） |
|---|---|---|
| K67-1 | 文件叶子进树 | 目录与文件同行渲染（**目录在前、文件在后**）；「（空）」占位仅在**真空目录**渲染；TREE_LEVEL_CAP 口径 = **目录 + 文件合计**（300 档维持——文件节点计入后的预算复核已随票归档，NFR-P70 绿）；文件叶子点击 → 右侧 item view；children 表收窄见 §9A-S5 |
| K67-2 | select ≠ expand | 单击仓库名 = 纯选中（`aria-expanded` 前后不变）；展开只由 expanded 集驱动（箭头 twisty / 键盘 → / 深链祖先链）；**深链祖先链含仓根 + 被选目录自身**（对齐项维持不动——Artifactory 同语义） |
| K67-3 | URL 模型（**TAB 省略规范形**） | `/artifacts/[<TAB>/]<repo>/<path>`；TAB ∈ {general\|properties\|permissions}，**省略 = general 规范形**——默认档不占段（全部既有仓/目录深链保持规范形零重定向；书签与既有 spec 断言面不猝死），**非默认页签恒占段**；文件 = 路径末段（末段文件/目录判别经父目录 listing，零额外请求）；旧 `?focus=` 与多段旧形一次性 replace 折入（**兼容重定向维持一轮**——发射端翻新归 T-449：DashboardPage/SearchPage/AqlPanel 改发路径段深链）；reload / 重开 / 分享三态一致 |
| K67-4 | 树头工具带 | 三行带形态、**不随树滚动**（`.browser-tree-scroll` 滚动区让位）：过滤仓库文本框（载体自页头迁树头，锚不变）+ 包类型 facet 复选组（**选项集 = 已加载清单实有型**，动态——不伪造未启用型）+ rclass 复选组（**三态：local / remote / virtual**）+ Sort-by（名称 / 包类型 / 仓库类型）+ Compacted/Non-Compacted 单选（紧凑行高档）+ My Favorites（前端态 localStorage 持久；标记入口 = 仓库右键 `tree-context-favorite`） |

**K67 附注三条（T-434 契约漂移注记升格冻结）**：

1. **rclass 三态形态**：Artifactory 的 Cache 位 = remote 仓**缓存子集视图**（reverse §3.2 回填口径）；BinFlow remote 浏览面本就是缓存落地行——**不伪造第四态**。远端浏览可选档（FR-147——T-448/T-461）on 时 remote 树含未缓存远端行，仍记 remote 态、不拆 Cache 态。
2. **TAB 词与 repo key 同名 known-edge**：repo key 与 TAB 词（general/properties/permissions）同名时按 TAB 解析——该名仓库经 `/artifacts/general/<key>` 仍可达（路由 `:tab/:key` 排位优先于 `:key/*`）。
3. **文件深链多付一次请求**（装载链乐观取全路径 → FileInfo GET + `?list` 400 快速失败）——正确性优先的已知代价，非热路径（NFR-P70 门未触）；优化窗口留后续性能票，**不进断言面**。

### 11.2 分页控件形态（Q4 出口①——LC-98 / T-451 断言锚）

| 断言面 | 冻结形态 | 置信度 |
|---|---|---|
| 控件品种 | 页码数字序列（当前页高亮）+ 首/上一页/下一页/末页按钮 + 每页行数选择器 | 形态**高**（Artifactory 管理列表通行形态 + t226 列表实测页码控件在场——L4 v1.1 中置信升格）；**每页行数档位候 T-451 票内 t226 复核后冻结，不静默升格** |
| 边界态 | 首页时「首页/上一页」禁置、末页时「下一页/末页」禁置；单页全量时控件整体呈现、全链禁置——**禁置不隐藏**（P7 键盘可达） | 高（本册族通用规格） |
| 语义 C 注 | 后端维持 keyset 游标 + **页窗映射**（页码跳转 = 前端游标链推进，深翻页 offset 扫描成本规避）；呈现对齐、语义自有——LC-98 C 注留痕 | 定案（Q4 终裁附带） |
| 适用范围 | 管理列表 + 结果表（×9 消费点）；**制品树/大目录深浏览维持增量加载**（分治口径见 §5 L4 v1.5 注——Artifactory 同为双轨） | 定案 |
| 消费点清单 | T-451 票内先 grep 盘点「加载更多」族全量清单入锚册（B-3.3 实测基数 9——实际多于 9 以盘点为准） | 待盘点（票内） |

---

## 12. B 47 项四态预归属表（v1.5，2026-09-03，T-437——供 QA 终验收口审计〔T-460/T-466〕对账）

> 源：`reports/m16-parity-audit-material.md` §B（t226 逐页实测 + 代码行锚）。四态 = **翻正**（对齐改造落票）/ **豁免·复核**（豁免条目经 Q 表或审计显式复核——维持或注销均留 Q 编号）/ **stay-out 确认**（§9A 登记）/ **候裁挂起**（Q 编号在册或无主候选待核定）。
> **行数对账**：材料自报 47（logic 11 / visual 17 / minor 19），实列行 **48**（B-2 实列 18 行——B-2.7/B-2.10 系同主题「独立取证口径」行，材料自计数按主题归并 1）；本表**按行全列 48**、同主题行标「对账去重行」——零丢项零无主，收口审计以本表行数为准确口径。
> **行级汇总**：翻正 36（已落 4——T-434；在途 32）/ 豁免·复核维持 3 / stay-out 确认 2 / 候裁挂起 4 / 对账去重行 3。

### B-1 logic（11 项——批次① + 表单栈 + 安全栈 + cron 域）

| # | 偏差点（摘） | 四态归属 | 承载票 / 依据 |
|---|---|---|---|
| B-1.1 | 左树无文件叶子（folders-only + 误导「（空）」） | **翻正 · 已落** | T-434 done（断言反转①；K67-1） |
| B-1.2 | 选择即展开（Select ≠ 纯 select） | **翻正 · 已落** | T-434 done（K67-2） |
| B-1.3 | URL/状态模型（页签不进 URL + `?focus=`） | **翻正 · 已落** | T-434 done（K67-3——TAB 省略规范形）；`?focus=` **发射端**翻新 → T-449 AC2 |
| B-1.4 | 树头工具带缺失 | **翻正 · 已落** | T-434 done（K67-4）；reverse §3.2 facet 回填同票已清偿（2026-09-02 补记在案） |
| B-1.5 | 表单藏字段（maxUniqueSnapshots/repoLayoutRef/blackedOut/archiveBrowsingEnabled——PUT 全收 UI 无） | **翻正 · 部分落（T-439 as-built 注）** | T-439 落**预留位形态**（恒禁用零提交）：活体对账证「PUT 全收」仅到**解码层**——四域 transport 解码不 400 但 `configJSON` 不转发、GET 回显缺失（decode-only 静默丢弃；契约漂移在案，API 漂移钉 tripwire 断言随 spec）；**提交-回显-行为三链须 BE 承接票**（configJSON 转发 + 行为联动）落地后逐域转正；repoLayoutRef 布局解析联动评估 = **K70**（BinFlow 布局由协议 adapter 固定、无 repoLayoutRef 消费方——承接时须接线布局引擎或钉协议默认值） |
| B-1.6 | 权限动词集（无 Annotate；write 未拆 Deploy/Cache） | **翻正**（Q7 已裁加） | T-444（BE 动词域 + 迁移零提权 NFR-S77）+ T-455（FE 矩阵五列——LC-88） |
| B-1.7 | 用户表单能力位（缺三旗；双布尔被三值枚举替） | **翻正** + 候裁臂 | T-453（三旗 A 腿 + 行为联动断言）；双布尔 vs 枚举 = **候裁挂起**（ADR-0026 闭集——v1.0 暂行维持枚举 + 差异登记） |
| B-1.8 | Profile 无自助 token/SSH（签发指到 admin 页） | **翻正**（文档化设计推翻——非正式豁免补齐） | T-457（一次性明文 + 即时可用 curl 断言；Access Tokens L1 重评联动 A7） |
| B-1.9 | GC 维护面（无 cron / Cleanup 两族 / Quota 百分比） | **翻正**（Q1 已裁） | T-446/T-450（引擎 + BE）+ T-462（FE 消费面——FR-145.7 呈现承载） |
| B-1.10 | 备份页（无定时 CRUD / import/export 页） | **翻正**（Q1 已裁） | T-450（BE：New Backup/cron/next-run/列表）+ T-462（FE；手动 dry-run/apply 并存维持） |
| B-1.11 | 监控组仅存储页（无 System Logs / Service Status） | **翻正**（P2） | T-459（SystemInfoPage 归位服务节点组；载体票内定案——零新端点优先） |

### B-2 visual（18 行——去重后 17 项；批次②③④主体）

| # | 偏差点（摘） | 四态归属 | 承载票 / 依据 |
|---|---|---|---|
| B-2.1 | 详情页签集逐级不同 + 顺序（属性在权限前） | **翻正** | T-445（三级页签序断言——权限在属性前；admin-only 门控维持） |
| B-2.2 | children 表复合 + 行内删除与 E1 相抵 | **翻正 · 部分已落** + 豁免·复核 | 操作列退役已落 T-434（Q2 出口①——E1 分治文本 §9 + as-built 对账）；表本体保留为增强（§9A-S5；候 Q9 确认） |
| B-2.3 | 文件元数据字段集（缺 File URL/Downloads 族/virtual 关联块；多出 mimeType/校验块） | **翻正** + stay-out 分拆 + 候裁 | 翻正：T-445（dep T-438 字段族端到端）；stay-out：Module ID（§9A-S8——dep Build-info，M17 解禁）；候裁：mimeType/校验块（Q9 倾向收进下载伴随——T-447） |
| B-2.4 | 仓库/目录元数据（仓缺 Layout/Description/Created/Count；目录缺 File URL） | **翻正** + 候裁 | T-445（与 T-434 目录直系概要对账）；「多出类型/子项/修改时间」处置候 Q9 |
| B-2.5 | 表单结构扁平（无 Basic\|Advanced\|Replications 步进） | **翻正 · 已落（T-439）** | T-439（复制配置移第三步——M6 能力语义零变化）：步进条三段 `form-step-*`（编辑态 × local；建仓态两段——仓尚不存在）；非活跃步整步卸载，六节锚零改名；7.161.20 :8082 活体复核证实 jf-steps 三步条形态（步进条无歧义） |
| B-2.6 | 包类型弹窗（440px/13 tiles/8 禁用 vs 880px 全可用） | **翻正** | T-441（modal 880px + 8 包型开禁——**M1 行再修订留痕归 T-441 落地时**，本版仅归属预告不预改 v1.4 定案文本；型录维持 13 实有不伪造） |
| B-2.7 | 制品详情页签（独立取证口径——同 B-2.1 主题） | **对账去重行** | 归 B-2.1 / T-445 |
| B-2.8 | 有效权限渲染（chip 列表 vs 分段开关 + 网格 + 列选；不显授予 target） | **候裁挂起（无主候选）** | PRD FR-144 未列该细分——分段开关 + AG 网格系重载体；建议 T-445 票内评估「授予 target 可见性」信息增补（轻腿），重载体候 PM 裁；**收口审计 T-460 对账点** |
| B-2.9 | 属性编辑解剖（隐藏表单 + 逐行 ✎/🗑 vs 常显输入 + Add + 网格搜索） | **翻正** + 候裁臂 | T-447（行内删除与 E1 关系统一——Q2 出口①）；Property\|Property Set 分段 = 候裁（K68——裁做须 BE 属性集小域另立票，不做缺位登记） |
| B-2.10 | General 字段集（独立取证口径——同 B-2.3 主题） | **对账去重行** | 归 B-2.3 / T-445 |
| B-2.11 | 搜索结果列集（三源不一致：现行五列 vs T-414 注释 vs console-m8 §6.4） | **翻正** | T-449（断言反转②——name 链接\|Path\|Repository\|Modified + 选择列；大小+sha256 移列选器可选项） |
| B-2.12 | 下载形态（两带文字按钮 vs 单 24px 图标钮） | **翻正** | T-447（Q2/Q9 处置——校验能力收进伴随形态） |
| B-2.13 | 查询位置（页内输入 vs 顶栏驻留 + 网格快滤） | **翻正** | T-449（AQL 模式编辑器共存形态票内设计） |
| B-2.14 | 快搜入口（253px 紧凑 vs 815px overlay + 范围页签） | **翻正** + stay-out 分拆 | 翻正：T-449（空历史「No recent searches yet」占位恒渲染——`AppShell.tsx:628` 对位）；stay-out：范围页签缺位维持（§9A-S7——R2 既定 + Builds dep M17） |
| B-2.15 | 用户/组创建内联卡（与 E5/V6 决策前提矛盾 + BinFlow 内部不一致） | **翻正**（Q5 已裁路由化） | T-453（断言反转④——/users/new、/groups/new 深链整页表单 + 内联卡退役断言；E5 注销 §9 + M3 勘误 §3） |
| B-2.16 | 权限编辑器内联形态（vs 两步弹窗） | **翻正** | T-455（① 选仓双列 + Any Local/Any Remote 预置〔Any Distribution 缺位登记 §9A-S8〕→ ② Set Patterns；E4 本体维持 §9A-S1） |
| B-2.17 | 帮助钮（纯链接 vs ? 下拉 + About 弹窗） | **翻正** | T-457（链接形态照 ux 定案；About 版本弹窗——侧栏脚注 vdev 升格） |
| B-2.18 | Admin 导航分组（认证单页三页签 / Webhooks·维护·备份挂治理 / 无侧栏过滤） | **翻正**（P2） | T-459（Webhooks 归常规组、维护·备份归服务节点组〔与 T-462 挂靠一致〕；HTTP SSO/Crowd/JIRA 缺位 = 域不存在登记不伪造） |

### B-3 minor（19 项）

| # | 偏差点（摘） | 四态归属 | 承载票 / 依据 |
|---|---|---|---|
| B-3.1 | 右键菜单集更小 | **豁免 · 复核维持** | 大半豁免（Pro 门控 Copy/Move/Versions 等族 + E1）；admin 深链 = 自有增强维持；A8 复核在案 |
| B-3.2 | 初始态（无选中 + 静态引导卡 vs 首仓库自动选中 + item view） | **候裁挂起（无主候选）** | FR-142.5 登记但 T-434 未承载（票面四项外）——建议 T-445 票内顺车（树 + 详情联动面）或 PM 收口笔 T-460 核定单列；**收口审计对账点** |
| B-3.3 | 空态/上限/分页（「（空）」占位 / CAP=300 / BIG_DIR=2000 / 表 100/页加载更多） | **翻正** + 豁免·复核 | 翻正：E2→页码控件 ×9（Q4——T-451，断言锚 §11.2）；豁免维持：TREE_LEVEL_CAP/BIG_DIR 自工程通知（无害自设计——K67-1 口径冻结） |
| B-3.4 | 系统节点（无 build-info 伪仓库；Trash 叶节点） | **豁免 · 复核维持** | build-info 不建（§9 永不建③ + M17 预立项段）；Trash 维持 T-372 最小入口形态；「回收站」中文文案随 i18n 键化（E6 两包条款） |
| B-3.5 | 仓库/目录字段（同族复核） | **对账去重行** | 归 B-2.4 / T-445（材料原文「原始归 minor」的归并在案） |
| B-3.6 | remote 表单无 Test 连通性（唯一 Test 在复制子表单） | **翻正** | T-442（BE 端点——Engine.TestTarget 同构复用；wire 归属〔歧义⑥〕票内与 conductor 核定落 LC）+ T-443（FE 三臂消费 + 零副作用） |
| B-3.7 | 编辑表单 Save 无 dirty-gating | **翻正** | T-443（进入 disabled / 变更 enabled / 无变更提交不可达） |
| B-3.8 | 创建入口拓扑（单「＋ 添加仓库」+ 表单内 rclass vs 下拉三预选分路由） | **翻正** | T-443（/new 直链兼容映射；表单内 rclass 控件移除） |
| B-3.9 | 仓库列表列集（缺 Project/Environment/Shared With；Remote 页签缺 Replications 列；多出冗余「类型」列） | **翻正** + stay-out 注记 | T-443（冗余「类型」列收敛〔候 Q9 附注〕+ Remote 页签 Replications 列——as-built 对账：列 + Run 动作已随 T-404 落，push-only 口径注记 ADR-0021/R10）；Project/Shared With 缺位登记不伪造（§9A-S8） |
| B-3.10 | 仓库行操作超集（copy-key + Set Me Up + 部署 vs 单 trash） | **豁免 · 复核维持** | L2 v1.1 + E1（T-443 AC3 零倒退断言承载） |
| B-3.11 | 表单 footer 多「重置」钮 | **已裁 · 已落（T-439）** | Q9 终裁移除（PM 倾向出口）——T-439 兑现：footer = Cancel + Create/Save 两钮（M1 行联动留痕在案；7.161.20 :8082 实测页脚无 Reset 佐证）；`form-reset` 锚退役入 §10.6（反断言翻新） |
| B-3.12 | 表单概念级缺口（Environments 多选 / 描述拆分 / Force Auth / Suppress POM） | **翻正 · 部分落（T-439 as-built 注）** | T-439 表驱动落地：**Force Auth 实字段**（forceConanAuthentication——local × conan，T-355A configJSON 全收 + adapter 401 挑战行为；PRD「（virtual）」注与后端实态不符——Artifactory 侧亦为 conan 域字段，7.161 实测 generic/virtual 表单无此标签）；Environments（7.161 已更名 **Stage**——Stages & Lifecycle）/ 描述拆分 notes / Suppress POM 三域后端无承接 → 预留位（Public Description 对位 = 既有描述字段，标签不改） |
| B-3.13 | 仓库详情中间页 | **stay-out 确认**（候 Q9 终裁） | §9A-S4——PM 倾向维持自有（Artifactory 无详情层） |
| B-3.14 | 搜索行导航（整行可点 vs 仅 name 单元格深链） | **翻正** | T-449（行体 inert；深链 = 路径段形——K67-3 联动） |
| B-3.15 | 日期格式（结果表无时区偏移；详情裸 ISO） | **翻正** | T-449（结果表 `dd-MM-yy HH:mm:ss +ZZZZ` 正则断言）+ T-445（详情 ISO 格式化——T/.000Z 不裸显）；en 变体归 T-464（locale 化，FR-149.4） |
| B-3.16 | 快搜空历史不渲染下拉 | **翻正** | T-449（与 B-2.14 翻正腿同源） |
| B-3.17 | 结果计数一致性 | **stay-out 确认** | §9A-S3——BinFlow header/footer 恒一致更优，不对齐 Artifactory 实测缺陷 |
| B-3.18 | 用户列表列（无 Realm/Last Login/网格搜索框） | **翻正** + stay-out 分拆 | 翻正：Last Login（T-454 BE 派生 + T-468 FE 列——**条件票**，Q10 裁点）；stay-out：Realm（无端点列不伪造——§9A-S8）；网格搜索框候 T-468 联动（列选器族推广） |
| B-3.19 | 组/权限列表（列集差异 + 行内「编辑」钮超集） | **候裁挂起**（部分） | 列集差异候 T-468（条件票——LC-96 列选器推广）；行内编辑钮 = L2/E1 家族豁免延伸（V4 实证行尾仅 trash）——候 Q9 复核收敛或维持（§9A 候裁子项） |

**表尾结论（收口审计输入）**：48 行四态齐备零无主——翻正 36（T-434 已落 4 / 在途 32 标票号）、豁免·复核维持 3、stay-out 确认 2（另有 5 行内分拆 stay-out 子项归 §9A）、候裁挂起 4（B-2.8、B-3.2 无主候选 → T-460 核定；B-3.11、B-3.19 候 Q9）、对账去重行 3。候裁项终裁后回写本表 + 版本递增。
