# BinFlow 控制台 MUI 原生视觉规范（mui-native-visual）

| 项 | 值 |
|---|---|
| 文档 | `docs/design/mui-native-visual.md` |
| 票据 | T-344A（M12 升格 · P1：MUI 原生视觉升级规范；用户 2026-08-29 指令「既然接入了 MUI，就要使用 MUI 的原生组件去让 UI 变得更好看」） |
| 状态 | v1.0（2026-08-29） |
| 维护者 | ux-designer |
| 上游依据 | 用户指令（BOARD M12 节）、docs/design/console-ux.md（v1.13：锚册 §10 / token §7 / 可达性 §8）、docs/design/console-m8.md §5（token 三阶纵深与亮色默认终裁）、reports/agents/T-299.md / T-300.md（MUI 批一/批二换装与 §3 样式层实证）、web/src/styles/*.css 与 pages/**.css 全量实读、web/e2e 类钩子 grep 实证（§3.8）、web/scripts/assert-tokens.mjs · anchor-audit.mjs（闸门口径）、web/src/app/MuiProvider.tsx · ThemeContext.tsx · AppShell.tsx |
| 下游消费者 | dev-frontend（实施波次 §6）、qa-engineer（验收口径 §5）、conductor（分派与 PR 收口） |

---

## 0. 背景与问题诊断

M10~M12 的 MUI 迁移（T-291 / T-299 / T-300）只换了**组件底座**：DOM 落在 MUI 组件上，但旧 CSS（base.css / pages.css / 页面 css）以更高特异性把 MUI 默认皮肤压回旧观感——`denseInputSx` 把 OutlinedInput 压平成 surface-3 直角输入、`.badge.*` 复合类 (0,2,0) 压过 Chip 默认色、`.table` 类续挂压掉 MUI Table 密度与字重、卡片仍是 border+shadow-1 的平贴 div。T-300 §3 的实证是本规范的地基：

1. **emotion 注入序在 base.css 之后**：同特异性 (0,1,0) 时 MUI 赢；**复合类 (0,2,0) 仍然旧 CSS 赢**。因此「让 MUI 默认皮肤生效」的正解不是调注入序，而是**删除压制规则本身**。
2. 迁移期靠 `className` 续挂 + 复合类压制维持「零视觉变化」——这是迁移策略，不是终态。终态（本规范）：MUI 组件按主题渲染默认皮肤，类名只剩两类合法残留——**spec 断言钩子（保名去规，§3.8）**与**无 MUI 等价物的自定义语义 DOM（§3.7）**。

**「丑」的根因清单**（走查对照）：输入框无浮标 label、直角+灰底平贴；按钮无涟漪无 elevation、密度被手写 32px 压扁；徽章是方角 tint 块而非胶囊；卡片无层次（elevation 全被 shadow-1 弱影+边框拉平）；Tab 条是自绘下划线；对话框直角硬边；全局 13px 字级偏小、行带过紧——整套是「div+CSS 手作感」，MUI 只当了无皮骨架。

## 1. 目标与边界

**目标**：控制台呈现 MUI 默认设计语言（Material Design 3 取向的 MUI v7 默认主题）——组件皮肤、密度档、动效（涟漪/焦点/过渡）交还 MUI 主题系统；品牌只保留**主色**与**有限微调**（§2）。

**不动什么**（红线，波次全程有效）：

| 红线 | 内容 |
|---|---|
| R1 锚册零变化 | `docs/design/console-ux.md` §10 锚册**零改动**；全部 testid 落在与旧 DOM 同型元素上（tr/th/input/select/button/a + Chip/MenuItem/Tab 根），`node scripts/anchor-audit.mjs --ledger` A1~A4 四零 |
| R2 交互逻辑零变化 | 路由、数据流、键盘导航（树 onTreeKeys / combobox ↑↓Esc / tablist 方向键 / 菜单循环）、四态收敛（loading/empty/error/403 分层）、权限姿态——全部不动；只换呈现层 |
| R3 spec 类钩子保全 | e2e 以 CSS 类定位的断言（§3.8 全表）——**类名留在 DOM 上**（inert 标记），CSS 规则可退役 |
| R4 自定义语义 DOM 保留 | T-300 §2 刻意不迁清单为基线（§3.7 可扩不可缩的无等价物清单） |
| R5 token 双源同值 | tokens.css 保留（theme-smoke 断言 `--bf-sidebar/bg/surface-1/text/accent/shadow-1..3` 存在）+ MuiProvider 调色板字面量同值同步（既有文件头契约） |
| R6 Non-goals 延续 | 无洞察报表/漏洞扫描 UI；不引 Roboto 网络字体（SPA 预算与离线部署） |

**与 console-ux 的关系**：console-ux §7.2 的「13px 主字号 / 32px 硬密度带」由本规范修订为「14px 主字号 / MUI small 密度档」（§2.3）；修订回写 console-ux 正文归 ux-designer 下一版（v1.14），**本票不改 console-ux.md**（见 §7 登记）。P1 信息密度原则本身不变——密度仍优先，只是实现档位换 MUI 原生档。

---

## 2. 主题定义（MuiProvider 目标态）

改写 `web/src/app/MuiProvider.tsx`。基线 = `createTheme()` 默认值（MUI v7 默认设计语言）；以下仅列**偏离默认的项**，未列项一律用默认。

### 2.1 palette（明暗双主题，跟随既有 ThemeContext 重建）

| 槽位 | 亮色 | 暗色 | 决策 |
|---|---|---|---|
| `mode` | `'light'` | `'dark'` | 跟 ThemeContext（`data-theme`），不引 MUI colorSchemes 状态机 |
| `primary.main` | `#0b6bcb`（= --bf-accent） | `#4aa3ff` | **品牌微调（唯一主品牌项）**；light/dark/contrastText/hover 全交 MUI augmentColor 派生，不再手写 |
| `secondary.main` | `#55606e`（--bf-text-2） | `#9aa4b2` | 占位防默认紫（#9c27b0）误用；**组件禁用 secondary** |
| `error / success / warning / info .main` | 沿用 tokens 值（#c9372f / #157f3d / #946200 / #0b6bcb） | 沿用（#f0605d / #3fbf6f / #d9a23a / #6cb2ff） | 保留双主题 axe 达标记录；hover/focus 派生交 MUI |
| `background.default / paper` | `#f3f5f7` / `#ffffff`（--bf-bg / surface-1） | `#12161d` / `#1a202a` | 与 MUI 默认（#fafafa/#fff、#121212）近邻域，保留以维持 R5 双源同值 |
| `text.primary / secondary` | `#1d232c` / `#55606e` | `#e9edf3` / `#9aa4b2` | tokens 同值 |
| `divider` | `#d3dae2` | `#2b3442` | tokens 同值 |
| `action` | **删除手写覆写**（active/hoverOpacity 回归 MUI 按 mode 默认） | 同左 | 悬停/激活反馈交还 MUI |

### 2.2 typography

- `fontFamily`：系统栈（现 FONT_STACK 原样）——**偏离 MUI 默认 Roboto 的必要项**（R6：不加载网络字体，离线部署/SPA 预算）。
- `fontSize: 14`——**自 13 上调至 MUI 默认档**（「像 MUI」的核心一步；表格/表单正文随之 14px，caption 12px 档自动对齐）。
- `button: { textTransform: 'none' }` 保留（控制台惯例）。
- 其余 variant（h1~h6 / subtitle / body / caption / overline）全默认；页面标题用 `variant="h6"`（20px），卡片标题 `variant="subtitle2"`（14px/600）。

### 2.3 shape / spacing / 密度档

- `shape: { borderRadius: 6 }`——品牌微调（MUI 默认 4；6 与存量 --bf-r-md 一致，暗色边框补偿下卡片更柔和）。Chip 圆角用组件默认胶囊（不覆写）。
- `spacing`：MUI 默认 8px 网格，不改。sx 内布局间距允许 `theme.spacing(n)` 或 `var(--bf-sp-*)` 双轨（sp-2=8=spacing(1)，同值）。
- **密度档 = MUI `size="small"` 全家**（经 components 默认值，§2.4）：Button ≈31px、TextField small、Chip small、`<Table size="small">` 行高 ≈33px（cell 6px 上下 padding + 14px 行 + 1px 边框）——工程密度带成立，**废除一切手写 32px 压制 sx**（nav-item height 32、denseInputSx minHeight 32 等全删）。

### 2.4 components 默认值（主题内收口，替代 muiAtoms 手写配方）

```tsx
components: {
  MuiButton:        { defaultProps: { size: 'small' } },          // 对话框主按钮显式 size="medium"
  MuiTextField:     { defaultProps: { size: 'small' } },
  MuiFormControl:   { defaultProps: { size: 'small' } },
  MuiChip:          { defaultProps: { size: 'small' } },
  MuiTable:         { defaultProps: { size: 'small' } },
  MuiListItemButton:{ defaultProps: { dense: true } },
  MuiButtonBase:    { defaultProps: { disableRipple: false } },    // 涟漪保留（显式声明：像 MUI）
}
```

- `CssBaseline` 保留；base.css 的 body/box-sizing 基线块随之退役（§3.2）。
- **muiAtoms 处置**（`web/src/lib/muiAtoms.ts`）：`denseInputSx` / `badgeChipSx` **删除**（压制配方，本规范的天敌）；`monoInputSx` **保留**（mono 字体非皮肤诉求，'(0,2,0) 落 mono 栈' 的修法仍必要）；`quietBtnSx` / `rowBtnSx` / `dangerBtnSx` **改薄**——只留布局项（minWidth/padding），色彩项删（`color="error"` / 默认 variant 承载），三配方归一为一个 `cellBtnSx`。

### 2.5 双源契约（不变，重申）

`createTheme` 拒绝 `var()` 引用 → MuiProvider 内以字面量复刻 token 值；**tokens.css 仍是唯一权威**（残余 CSS + theme-smoke 断言的消费面），改 token 必须同步 MuiProvider（文件头契约延续）。MuiProvider 是 TSX 侧唯一色值字面量豁免层（§5.2 新断言的豁免名单）。

---

## 3. 旧 CSS 退役清单（逐文件逐块）

处置枚举：**删**（规则+类名一并移除）｜**保名去规**（类名留 DOM 作 inert 钩子，CSS 规则删——凡 §3.8 钩子一律此形态）｜**换装**（承载组件换 MUI，类名按 §3.8）｜**留**（无 MUI 等价物/纯布局/性能规则）｜**改薄**（规则瘦身保留）。波次见 §6。

### 3.1 退役判定四问（后续增补 CSS 也按此审）

① 服务的 DOM 是否已换/将换 MUI 组件？→ 删压制规则；② 是否 §3.8 spec 类钩子？→ 保名去规；③ 是否 §3.7 语义 DOM？→ 留；④ 是否纯布局（grid/flex/间距/mono）？→ 留。

### 3.2 base.css（`web/src/styles/base.css`）

| 块（选择器） | 处置 | 去向 / 理由 | 波次 |
|---|---|---|---|
| `*` / `html,body` / `body`（背景/字体/字号） | 删 | CssBaseline + typography 承载（body 背景 = `background.default`） | A |
| `#root` min-height | 留 | 布局 | — |
| `a` / `a:hover` | 留 | 残余裸链接（面包屑/帮助/文档/field-hint a）；D 波评估 MuiLink | D |
| `:focus-visible` | 留 | 服务语义 DOM 族；MUI 组件走 ButtonBase focusVisible，两者并存 | — |
| `.mono` / `.text-2` / `.text-muted` | 留 | utility；`.mono` 是 §3.8 钩子（span.mono） | — |
| `.app` | 删 | Drawer 化后由 Box sx 承载（§4.1） | A |
| `.app-nav` 族（.app-nav / -brand / -items / .nav-group-label(+.first) / .nav-item(+hover/.active) / .nav-mode-switch(+hover) / -footer / -license） | 保名去规 + 换装 | 壳重构（§4.1）：`.app-nav`/`.nav-item`/`.nav-group-label` 类名留 DOM（§3.8 计数钩子），皮肤规则删——背景/active 指示条换 Drawer PaperProps sx + ListItemButton selected（消费 --bf-sidebar 系 token） | A |
| `.session-box` / `.session-toggle`(+hover/.who) / `.topbar-session` | 删 | Button sx 化；无 spec 类钩子（锚 session-toggle/session-user 在元素上不动） | A |
| `.menu-label` | 留 | Menu 内非交互分组行 | A |
| `.topbar-breadcrumb` 族 | 换装 | MUI Breadcrumbs + Link；锚 `topbar-breadcrumb` 在容器、类名留 | A |
| `.app-main` / `.app-topbar`(+.page-title/.spacer) | 删 | AppBar(position=sticky, elevation=0) + Toolbar(minHeight 48) + Container 承载 | A |
| `.search-entry` 族（容器/input/kbd） | 改薄 | 容器换 MUI 文档站搜索形态（`Paper elevation={0}` + InputBase + sx 圆角/焦点环）；`kbd` 提示块留；键盘链路不动 | A |
| `.topbar-search-recent` 族 | 留 | combobox 红线（T-300：键盘链路零变化）；视觉随主题 token 自动跟随 | — |
| 960px 媒体查询（search-entry/kbd） | 留 | 布局 | — |
| `.icon-btn` | 删 | IconButton size small 默认 | A |
| `.app-content` | 改薄 | `Container component="main" maxWidth={false} sx={{ maxWidth: 1440, px: 'var(--bf-sp-5)' }}`；max-width 语义保留 | A |
| `.card`(+h3/.degraded/.down) / `.card-grid` | 换装 | `Paper elevation={1}`（+ sx outlined 变体：degraded=warning 边、down=error 边）；h3 → Typography subtitle2；`.card` 类名留 DOM（§3.8 `.card.section` + 45 落点渐进迁移）；grid 留 | B |
| `.kv` / `.stat-row` | 留 | 布局 utility | — |
| `.badge` 族（base/neutral/success/warning/danger/tier-pro/tier-enterprise） | 保名去规 + 换装 | Chip `color="info|default|success|warning|error"`（filled 默认）；tint 配方规则删；**类名组合原样留**（§3.8 `.badge.warning` 类组合断言 + hasText 定位）；tier-pro→info、tier-enterprise→warning、neutral→default | C |
| `.status-dot` 族 | 留 | 自定义语义（色点+文字非唯一信号），无 MUI 等价小件 | — |
| `.table` 族（.table/th/td/hover/.wrap） | 删（类名一并摘） | MUI Table 默认 + size small；**无 spec 类钩子**（grep 实证）——T-300 的续挂使命终结 | C |
| `.field`（布局）/ `.field label` | 改薄 | TextField `label` 属性浮标化后 label 行退役；`.field` 布局壳（间距/max-width）暂留 | C |
| `.field > input`(+focus) / `.field > textarea` / `.field input.mono` | 删 | TextField/multiline 承载；mono 走 `monoInputSx` | C |
| `.field-error` / `.field-hint`(+a) | 保名去规 | §3.8 钩子（security.spec / a11y-sweep）；视觉并入 FormHelperText（error 属性）| C |
| `.btn` 族（.btn/primary/danger/hover/disabled） | 删 | Button variant 谱（contained/outlined/text + color） | C |
| `.skeleton`(+line/block/@keyframes/reduced-motion) | 换装 | 组件 `Skeleton.tsx` 内部换 MUI Skeleton（`skeleton` 锚不动）；CSS 删 | B |
| `.error-card` 族 | 换装 | 组件 `ErrorCard.tsx` 内部换 `Alert severity="error"` + Collapse（原文折叠保留）；`error-card`/`error-retry` 锚不动 | B |
| `.empty-state`(+.hint) | 保名去规 | §3.8 钩子；EmptyState 内部 Stack + Typography | B |
| `.toast-stack` / `.toast`(+success/error/.msg/.close) | 保名去规 + 换装 | ToastContext 内部换 Snackbar + Alert；`.toast` 类名留（§3.8）；toast/toast-stack 锚不动 | B |
| `.modal-backdrop` / `.modal`(+danger/h2/.modal-body/.modal-actions) | 保名去规 + 换装 | ConfirmDialog 换 Dialog/DialogTitle/DialogContent/DialogActions（danger variant）；`.modal` 类名留（§3.8 fallback 选择器）、`.modal-backdrop` 删（MUI Backdrop） | B |
| `.login-*` 族（page/brand/card/actions/divider/sso-error/note） | 换装（.login-divider 保名） | Stack + Paper + TextField(label 浮标) + Button + Divider；§3.8 钩子 | B |
| `.boot-screen` / `.route-fallback` | 改薄 | CircularProgress 居中（可选） | A |
| `.page-header`(+h2) / `.section` | 留 | 布局；标题 Typography h6 化随波次 C 页面走 | C |
| `.copy-btn` | 删 | CopyButton 内部换 IconButton（aria-label 纪律不变，§8） | B |
| `.water-mark` | 留 | — | — |

### 3.3 pages.css（`web/src/styles/pages.css`）

| 块 | 处置 | 去向 | 波次 |
|---|---|---|---|
| `.filter-bar`(+.count) | 留（类名=§3.8 钩子） | 布局保留；内部输入已 TextField | C |
| `.row-link` | 留 | 行链接样式（或 Typography sx，保守留） | — |
| `.member-pop` 族 | 留 | §3.8 钩子（summary/.pop 定位）+ 零 JS 浮层红线 | — |
| `.cell-pending` | 换装 | MUI Skeleton（width 48 height 10 sx） | C |
| `.water-bar` 族（bar/fill/warn/full）/ `.water-line` | 保名去规 + 换装 | LinearProgress(determinate) + color（primary→warning→error）；`.water-bar`+warn/full 类名留（§3.8 toHaveClass）；water-line 布局留 | C |
| `.cmd-block` 族 | 留 | MUI 无「命令块」等价物（P3 核心）；D 波可选 Paper 外壳 | D |
| `.danger-zone` 族 | 换装 | `Paper variant="outlined"` + sx borderColor `error.main`；类名留 | C |
| `.tabs` 族 | 换装 | MUI Tabs（RepositoriesPage 详情之外的通用 tab 位） | C |
| `.detail-head`/`.detail-desc`/`.detail-grid` | 留 | 布局 | — |
| `.form-layout` | 留 | 布局 | — |
| `.radio-row`(+disabled) / `.check-row` | 换装（.check-row 保名） | RadioGroup + Radio/Checkbox + FormControlLabel；§3.8 钩子 `.filter-bar .check-row input` | C |
| `.form-actions` | 留 | 布局 | — |
| `.form-error`(+headline/.raw) | 换装 | Alert severity="error"（无类钩子，类名可摘） | C |
| `.warn-box` | 保名去规 + 换装 | Alert severity="warning" + className 续挂；§3.8 钩子（`.tree-page .warn-box`） | C |
| `.summary-box` 族 / `.member-pick` / `.chip-list` / `.chip-item` 族 | 留 | 表单自定义（成员序/穿梭）；D 波可选 List 化 | D |
| `.field-note` | 留 | FormHelperText 视觉等价 | — |
| `.field input.mono-input` / `textarea.mono-input` | 留 | mono 语义（(0,2,1) 后代形态天然赢——T-300 注记） | — |
| fieldset / legend 复位 / `.section-title` / `.form-layout .field` | 留 | 表单排版 | — |
| `.confirm-input` | 删 | ConfirmDialog 输入 TextField(mono) 化 | B |
| `.modal .server-reason` | 删 | Dialog 化后 Alert sx 承载 | B |
| `.storage-refresh-row` / `.storage-total` | 留 | 布局/表内强调（TableRow sx 化可选） | D |

### 3.4 governance.css（`web/src/styles/governance.css`）

| 块 | 处置 | 去向 | 波次 |
|---|---|---|---|
| `.audit-time` | 留 | mono 时间列 | — |
| `.filter-bar .time-field`(+input) | 改薄 | input 皮肤删（TextField 化）；label 行布局留 | C |
| `.detail-pop` 族 | 留 | 审计 JSON 折叠（details/summary 零 JS）红线 | — |
| `.more-row` / `.gc-actions` / `.grace-details` | 留 | 布局 | — |
| `.gc-error` | 保名去规 | T-300 已 Alert 化续挂；CSS 块删（Alert 承载） | C |
| `.gc-result`(+h4) | 换装 | Paper | D |
| `.gc-empty-ok` | 留 | 绿色空态（好消息语义） | — |
| `.quota-bar-cell` / `.quota-bar`(+pct warn/full) | 保名去规 + 换装 | LinearProgress 同 water-bar；pct 数字留 | C |
| `.quota-input` | 删 | 已 TextField，残留规则清 | C |
| `td .field-error` | 留 | §3.8 关联钩子 | — |

### 3.5 页面级 css（7 文件）

**repositories.css**：`.repos-tabs`/`.repos-tab` 族 → **MUI Tabs**（锚 repos-tab-* 在 Tab 根、aria-current=page 语义保留）【C】；`.th-sort`/`.th-sort-arrow` → **TableSortLabel**（aria-sort 三态内建；锚 users-sort-*/repos-sort-* 在 th 不动）【C】；`.repo-tabs`/`.repo-tab` 族 → MUI Tabs（锚 repo-tab-*）【C】；`.pkg-grid-*` → 留（自定义网格；D 波可选 Card+CardActionArea）；`.repo-form-section`(+h3/.section-sub) → Paper + Typography【C】；`.page-note`/`.table-foot`/`.repos-head-*`/`.detail-head-actions`/`.quota-edit` → 留（布局）；`.table tbody tr:hover .row-link:hover` 对比度修法 → MUI TableRow hover + sx 同义重落【C】。

**tree.css**：`.tree-layout`/`.tree-pane` → 布局留（D 波 tree-pane 可选 Paper 化，**content-visibility 性能规则必须原样保留**）；`.tree-node` 族 / `.tree-skel`/`.tree-denied`/`.tree-empty-level` → **留**（核心语义 DOM + 键盘链路红线）；`.tree-table td .badge`(+warning) → 随 badge 族退役、tag 徽标 Chip color 化【C】；`.tree-breadcrumb` 族 → 留（交互语义强，D 波评估 Breadcrumbs）；`.tree-table` content-visibility/`.selected` → 留（§6.4 性能闸）。

**browser.css**：`.browser-toolbar`/`.browser-actions`/`.browser-filter`/`.browser-layout` → 留（布局）；`.tree-node.repo-node`/`.selected`/`.on-chain` → 留（语义 DOM）；`.node-tabs`/`.node-tab` → **D 波谨慎评估** MUI Tabs（onTablistKeys 键盘链路 → MUI 内建等价需核；锚 node-tab-* 不动）；`.checksum-badge`(+ok-badge/warning) → Chip color 化【C】；`.context-item` → **留**（spec 断言 button 计数红线）；`.browser-footer`/`.tree-empty-instance` → 留；`.browser-readonly-note` → Alert warning 化保名【C】。

**search.css**：`.search-headline`/`.search-sub`/`.search-count` → 留（Typography 化随 C 微调）；`.search-box` → 留；`.search-recent` 族 → **留**（combobox 红线）；`.search-result-sub`/`.search-footer` → 留。

**security.css**：`.inline-form`/`.form-section` → 留（分区排版）；`.transfer` 族 → **留**（布局 + §3.8 transfer-item 钩子）；`.th-sort` → TableSortLabel【C】；`.perm-summary`/`.sec-pick`/`.sec-chips`/`.cell-stack`/`.cell-inline`/`.admin-note`/`.diff-list`/`.matrix-cell`/`.matrix-user-cell` → 留；`.pattern-chip` / `.principal-remove` → **留**（spec 类钩子红线）；`.tester` 族 → 留（§4.9 灵魂件；输入已 TextField）；`.pattern-add`(+input) / `.matrix-add`(+select) → 改薄（原生控件皮肤规则删，已 MUI）；`.conflict-panel` → Alert warning 化【C】；`.pattern-cols`/`.pattern-col` → Paper 化可选【D】；`.modal.perm-res-modal`/`.perm-res-*` → ResourceDialog 换 MUI Dialog（自由焦点陷阱交 MUI）【B】；`.status-pill`(+on/off) → Chip color success/error（类名留）【C】；`.sec-danger-zone` 族 → Paper+error sx（类名留）【C】。

**license.css**：`.license-note`/`.license-install-block` → 留；`.addons-table tr.is-locked/.is-disabled` → 留（灰显语义，TableRow sx 化可选）【D】。

**authconfig.css**：`.authcfg-tabs`/`.authcfg-tab` → MUI Tabs（锚 authcfg-tab-*）【C】；`.authcfg-group`(+h3/hint) → **Paper 化且类名留**（§3.8 `.authcfg-group` 钩子）【C】；`.authcfg-grid`/`.authcfg-field`(+label/.full/.hint) → 留（数据驱动表单布局，sections.ts anchor 契约不动）；`.authcfg-secret-set`/`.authcfg-test-actions`/`.authcfg-cert-*`/`.authcfg-report-meta` → 留（cert-fp span.mono 是 §3.8 钩子）。

**components/dialogs.css**：`.smu-modal`/`.deploy-modal` → 删（Dialog slotProps.paper sx 承载宽/高）【B】；`.smu-grid-*` → 留；`.smu-back` → Button text 化可摘【B】；`.smu-tabs`/`.smu-tab` → **留**（D 波谨慎：`smu-tab-configure` querySelector 自消费 + Tab 焦点管理，换 MUI Tabs 须同步改写选择器逻辑）；`.smu-stepup`(+field/field-hint) → 留（警示面板语义）或 Paper 化【B 微调】；`.smu-error-inline`/`.smu-error-raw` → 留；`.smu-token-panel` 族 → Paper/Alert 化可选【D】；`.deploy-field-grid` → 留；`.deploy-drop` → 留（拖拽语义 DOM；`Paper variant="outlined"` sx 可选）；`.deploy-progress`(+bar/fill) → **LinearProgress** 换装【C】；`.deploy-echo`/`.deploy-error` → 留。

### 3.6 退役统计

| 文件 | 删/换装块 | 保名去规 | 留 | 合计处置块 |
|---|---|---|---|---|
| base.css | 18 | 8 | 13 | 39 |
| pages.css | 8 | 5 | 12 | 25 |
| governance.css | 4 | 3 | 5 | 12 |
| repositories.css | 5 | 0 | 6 | 11 |
| tree.css | 1 | 0 | 5 | 6 |
| browser.css | 2 | 1 | 5 | 8 |
| search.css | 0 | 0 | 4 | 4 |
| security.css | 5 | 2 | 12 | 19 |
| license.css | 0 | 0 | 2 | 2 |
| authconfig.css | 1 | 1 | 4 | 6 |
| dialogs.css | 3 | 0 | 7 | 10 |
| **合计** | **47** | **20** | **73** | **142** |

（口径：一个「块」= 上表一行选择器族；多文件同处置只计一次。）

### 3.7 保留清单（无 MUI 等价物 / 红线，扩不可缩）

1. **树本体**：`.tree-node` 族（role=treeitem、twisty、onTreeKeys ↑↓→←/Enter/Shift+F10）、`.tree-pane` content-visibility 性能规则、`.repo-node`/`.on-chain` 链高亮。
2. **combobox 族**：`.search-recent` / `.topbar-search-recent`（最近词下拉——自定义键盘链路红线）。
3. **零 JS 浮层**：`.member-pop` / `.detail-pop`（details/summary）。
4. **tablist 自持**：`.node-tabs`（D 波评估前不动）、`.smu-tabs`（querySelector 自消费）。
5. **spec 类钩子件**：`.pattern-chip` / `.principal-remove` / `.context-item`（button 计数断言）。
6. **自定义灵魂件**：`.tester` 模式测试器、`.diff-list` 变更摘要、`.perm-summary` 矩阵标记、`.cmd-block` 命令块、`.status-dot`、`.deploy-drop` 拖拽区。
7. **布局 utility**：`.kv`/`.stat-row`/`.detail-grid`/`.form-layout`/`.transfer`/各 `*-toolbar`/`*-foot`/mono 族。
8. **语义注记行**：`.page-note`/`.field-note`/`.license-note`（FormHelperText 视觉等价，MUI 化无增益）。

### 3.8 spec 类钩子保全表（保名去规硬约束——类名留在 DOM，规则可退役）

grep 实证（web/e2e 全量，console 侧）：

| 类钩子 | spec 消费点 | 保全形态 |
|---|---|---|
| `.app-nav` / `.nav-item` / `a.nav-item` | shell（计数 3/14）、governance、auth-config、m10、login | nav 根与 `<a>` 条目类名原样；含 `button.nav-mode-switch` |
| `.nav-group-label` | shell ×6、rbac、security、m10 | 分组标签类名原样 |
| `.badge` + `.badge.warning` 等类组合（toHaveClass 正则） | permissions（badge success/danger/warning）、rbac、replication、m9/m8 users-groups、m10、governance-monitoring | Chip 根**类名组合原样续挂**（`className="badge warning"` inert），色由 Chip color 承载；computed 色值仅 attach 不断言（实证），换配方安全 |
| `.toast` | artifacts ×2、artifacts-tree | Snackbar/Alert 根类名续挂 |
| `.modal` | artifacts-tree:141（`[data-testid="confirm-dialog"], .modal` fallback） | Dialog paper 根类名续挂 |
| `.empty-state` | repositories-admin ×2 | EmptyState 根类名原样 |
| `.water-bar` + `warn`/`full`（toHaveClass） | governance:299/307 | LinearProgress 根类名续挂 |
| `.member-pop`（summary/.pop） | repositories ×2、repositories-admin ×2 | 原样保留 |
| `.warn-box` | artifacts:188、artifacts-tree:218（`.tree-page .warn-box`） | Alert 续挂类名 |
| `.field-error` / `.field-hint a` | security ×2、a11y-sweep | 类名留 DOM（视觉并入 FormHelperText） |
| `.login-divider` | login:142 | Divider 续挂类名 |
| `.filter-bar` + `.check-row input` | m9 console-fr82:139 | FormControlLabel 续挂 check-row |
| `.authcfg-group` / `.authcfg-cert-fp > span.mono` | auth-config ×2 | Paper 续挂类名；mono span 不动 |
| `.card.section` | auxiliary:126（dashboard） | Paper 续挂 card 类名 |
| `.transfer-item` | m9 users-groups:143 | 条目类名原样 |
| `.mono` | auth-config（span.mono） | utility 保留 |

---

## 4. MUI 原生形态清单（页面 → 现状 → 目标组件）

### 4.1 布局骨架（AppShell）重构

```
┌────────────────┬─────────────────────────────────────────────────────────┐
│ Drawer         │ AppBar position="sticky" elevation={0}（borderBottom）   │
│ variant=       │ ┌─────────────────────────────────────────────────────┐ │
│ "permanent"    │ │ Toolbar disableGutters（minHeight 48）               │ │
│ slotProps.paper│ │ [Breadcrumbs｜页面标题 Typography]   …spacer…        │ │
│  sx: width 224 │ │ [Paper+InputBase 搜索(topbar-search) ⌘K] [?] [◐]    │ │
│  bg --bf-sidebar│ │ [用户 ▾ Chip 徽章 + Menu]                           │ │
│ ┌────────────┐ │ └─────────────────────────────────────────────────────┘ │
│ │ brand 行    │ │ Container component="main" maxWidth={false}            │
│ ├────────────┤ │  sx={{ maxWidth: 1440, px: 'var(--bf-sp-5)' }}         │
│ │ List       │ │  ┌─ Outlet ─────────────────────────────────────────┐  │
│ │ ListItemB- │ │  │ Paper 卡 / Tabs / Table(small) / Alert / Chip …  │  │
│ │ utton ×N   │ │  │（页面内容，§4.3 逐页）                            │  │
│ │ a.nav-item │ │  └──────────────────────────────────────────────────┘  │
│ ├────────────┤ │                                                         │
│ │ 模式切换    │ │                                                         │
│ │ license 行  │ │                                                         │
│ └────────────┘ │                                                         │
└────────────────┴─────────────────────────────────────────────────────────┘
```

- 侧栏身份保留（深底 + --bf-sidebar 系 token），**实现换 Drawer**：`<nav class="app-nav" data-testid="app-nav">` 落在 Drawer Paper 上（或其内 nav 元素——保持 `nav` 标签语义）；条目 `ListItemButton component={NavLink}`，DOM 仍 `<a class="nav-item active">`（NavLink 自动追加 active 类；selected 视觉经 sx `'&.active'` 消费 primary 淡底）。
- 会话菜单/主题钮/帮助链接：摘 T-300 的 paper sx 复刻块，交 MUI 默认（Menu paper elevation / MenuItem 密度经主题 small 档）。
- `boot-screen`/`route-fallback`：CircularProgress。

### 4.2 共享组件层（六件套 + 对话框族 + Toast）

| 组件（文件） | 现状 | 目标 | 波次 |
|---|---|---|---|
| `Skeleton.tsx` | div.skeleton + CSS 动画 | MUI Skeleton（variant text/rectangular；`skeleton` 锚、`prefers-reduced-motion` MUI 内建） | B |
| `ErrorCard.tsx` | div.error-card + details | Alert severity="error" + Collapse（重试钮 Button；error-card/error-retry 锚不动） | B |
| `EmptyState.tsx` | div.empty-state | Stack + Typography + Button（类名保 §3.8） | B |
| `ConfirmDialog.tsx` | div.modal-backdrop + 自有焦点陷阱 | Dialog + DialogTitle/Content/Actions（confirm-dialog/accept/cancel 锚不动；confirm-input → TextField mono） | B |
| `CopyButton.tsx` | button.copy-btn | IconButton（aria-label 纪律不变） | B |
| `DeployDialog.tsx` / `SetMeUpDialog.tsx` | 自有 modal 壳 | Dialog（slotProps.paper sx 承载 720px 宽）；deploy-progress → LinearProgress；smu-tabs 保留 | B |
| `ToastContext.tsx` | div.toast-stack + .toast | Snackbar + Alert（success 5s / error 常驻语义不变；toast/toast-stack 锚与 .toast 类名保） | B |
| `ResourceDialog`（PermissionEditorPage 内） | .modal-backdrop + 自有焦点陷阱 | Dialog（两步流程与 perm-res-* 锚不动） | B |

### 4.3 逐页清单

| 页面/域 | 现状（div+CSS） | 目标 MUI 原生组件 | 波次 |
|---|---|---|---|
| AppShell | div.app flex + header + nav（base.css 壳族） | Drawer / AppBar / Toolbar / Container / Breadcrumbs / List+ListItemButton | A |
| 登录 | .login-card 手作卡 | Stack + Paper(elevation 3) + TextField(label 浮标) + Button(contained, medium) + Divider | B |
| 仪表盘 | section.card ×5 + .kv | Paper + Typography subtitle2 + kv 留（degraded/down = Paper sx 边色） | B |
| 仓库列表 | Table 续挂 .table + .th-sort + badge 续挂 | Table(small) 默认 + TableSortLabel + Tabs(repos-tab) + Chip color + TablePagination 语义行（.table-foot 留文案） | C |
| 建仓/编辑表单 | .repo-form-section div + .field label + TextField 裸 | Paper 分区卡 + TextField(label) + RadioGroup + FormControlLabel + Button 族 | C |
| 包型网格对话框 | .pkg-grid-item 手作 | （可选 D 波）Card + CardActionArea；保守保留 | D |
| 仓库详情 | .card / .detail-head / .repo-tabs | Paper + Typography(h6:key mono) + Tabs(repo-tab) + Alert(readonly 注记) | C |
| 制品浏览树 | .tree-pane + Table 续挂 + .node-tabs | 树本体不动；Table(small) 默认；node-tabs D 波评估；checksum-badge→Chip | C/D |
| 搜索 | TextField 裸 + .search-recent | TextField(label) + 结果 Table(small)；recent 族不动 | C |
| 用户/组/权限 | Table 续挂 + .th-sort + badge/status-pill | Table(small) + TableSortLabel + Chip color + Tabs 无（本域无） | C |
| 权限编辑器 | .pattern-col + .tester + 矩阵表 | pattern-col→Paper（可选）；tester/矩阵标记不动；MatrixCell Checkbox 主题默认 | C/D |
| 审计 | 过滤 .time-field + Table 续挂 | TextField(label, type=datetime-local) + Table(small)；detail-pop 不动 | C |
| GC/迁移/配额 | .gc-error / .water-bar / .quota-bar | Alert(error/warning) + LinearProgress(color 三档) + Paper(gc-result) | C/D |
| 复制 | Table 续挂 + badge | Table(small) + Chip color | C |
| 备份 | 纯 CLI 引导块 | 无换装面（维持 T-300 登记） | — |
| 存储概要 | Table 续挂 + badge | Table(small) + Chip color | C |
| 系统信息 | .card + .kv | Paper + kv 留 | B |
| License & Add-ons | Table 续挂 + tier badge + Alert | Table(small) + Chip color(info/warning/default) + Alert | C |
| 认证配置 | .authcfg-tabs + .authcfg-group + 字段网格 | Tabs(authcfg-tab) + Paper(类名保) + TextField(label)；sections.ts anchor 契约不动 | C |
| 编辑档案 / 404 / 占位 | .card / 简单 div | Paper + Stack + Button（批三候选页随 B 波共享基元自动获益，页内残面 C 波清） | B/C |

---

## 5. 验收口径（每波次收口跑全量）

### 5.1 锚册（硬门）

`node scripts/anchor-audit.mjs --ledger` → **A1 unregistered=0 / A2 broken=0 / A3 retired 全在表 / A4 退役∩src=∅**；console-ux.md §10 锚册**零改动**。视觉换装不得移动锚的元素型（tr/th/input/select/button/a + Chip/MenuItem/Tab 根）——T-300 纪律延续。

### 5.2 色值纪律（assert-tokens 扩展，随波次 A 交付脚本腿）

1. 既有断言维持：src 全量 css 零硬编码色值（tokens.css 豁免）。
2. **新增 TSX 断言**（assert-tokens.mjs 扩展）：`web/src/**/*.tsx` 中 sx/style 的色值字面量（hex/rgb/hsl）仅允许出现在 `app/MuiProvider.tsx`（主题定义层，与 tokens.css 同级豁免）。
3. **主题优先规则**：MUI 组件的 sx 不得引用 `--bf-*` **色彩** token（bg/surface/text/accent/danger/success/warning/info/sidebar 系）——色板一律 `theme.palette`（`(t) => t.palette...`）或组件默认；布局 token（--bf-sp-* / --bf-mono / --bf-z-*）与 §3.7 保留清单的语义 DOM 样式不受限。断言形态：脚本扫描 sx 内 `var(--bf-(bg|surface|text|accent|danger|success|warning|info|scrim))` 命中即 FAIL（保留清单 css 文件不扫）。

### 5.3 可达性

axe-core 双主题（light+dark）serious+critical=0——T-300 口径复扫（≥12 代表页：users/groups/perms/perm-new/audit/gc/quotas/license/storage/search/artifacts/auth ×2 主题）；重点新面：Chip filled 对比度（MUI contrastText 承载）、Tabs indicator、Drawer 侧栏文字（--bf-sidebar-text 系 ≥4.5:1 维持）。

### 5.4 SPA 预算

单票增量 ≤ **+3.25%**（对前一票构建产物字节）；对 T-299 基线 **284,185B** 累计 ≤ **+25%**（NFR-P51）。新引 MUI 组件（Drawer/AppBar/Toolbar/Container/Breadcrumbs/Tabs/Snackbar/Alert/LinearProgress/Skeleton/TableSortLabel/Paper/Collapse）按路由懒加载分片摊薄；AppShell 域（A 波）进主包，注意 AppBar+Drawer 合并 chunk 体积。

### 5.5 Playwright

三项目全量绿（共居负载 flake 甄别沿 T-300 §4.4 先例：失败 spec `--workers=1` 串行复跑佐证）。**类钩子腿**（§3.8 表）是换装波次的重点回归面——任何「摘类名」动作前必查该表。

### 5.6 「看起来像 MUI」走查清单（人工，每波次收口过一遍）

1. 按钮：contained 有 elevation 与涟漪；hover 抬升；focusVisible 环为 MUI 形态；文字不大写。
2. 输入：TextField outlined 默认形态 + **浮标 label**；聚焦边框主色放大（legend 动画）；错误态红边 + helperText。
3. 徽章：Chip **胶囊圆角**、filled 语义色底；无方角 tint 块残留。
4. 卡片：Paper 有可辨 elevation（亮色 shadow / 暗色 elevation overlay）；圆角 6；无「border+平影」手作卡。
5. Tab：Tabs indicator 动画过渡；选中态主色。
6. 对话框：Dialog scrim + 圆角 + DialogActions 右对齐；Esc/焦点陷阱原生。
7. Snackbar：右下滑入；Alert 语义色边。
8. 表格：Table(small) 行 hover、表头字重/小字、行高 ≈33px；TableSortLabel 箭头。
9. 进度：LinearProgress 圆角条 + color 三档（水位条）。
10. 侧栏：Drawer 结构；active 项 primary 淡底；分组标签 overline 观感。
11. 全局字级 14px 主档；无 13px 手写密度残留。
12. 两主题各过一遍（含暗色 elevation 表达）。

---

## 6. 分批建议（dev-frontend 实施波次）

每波次独立成票、独立过 §5 全套闸门；波内 area 不重叠。

| 波 | 内容 | area | 关键产出 |
|---|---|---|---|
| **A 主题基座+壳** | MuiProvider 重写（§2 全量 + components 默认值）；muiAtoms 清理（denseInputSx/badgeChipSx 删、三 btn 配方归一改薄）；AppShell 重构（Drawer/AppBar/Toolbar/Container/Breadcrumbs/搜索 Paper）；base.css 壳族退役（§3.2 A 行）；assert-tokens TSX 扩展腿（§5.2） | web/src/app + components/AppShell.tsx + styles/base.css + scripts | 主题与注入序地基——**本波必须先行**（后续波的组件默认皮肤都靠它） |
| **B 共享基元层** | 六件套 + ToastContext + 三对话框 Dialog 化（§4.2）；login/NotFound/Placeholder/Profile/Dashboard/SystemInfo 残面（批三候选页，随基元换装自动获益）；base.css 四态/表单/登录段退役 | web/src/components + pages/{Login,Dashboard,Profile,Placeholder,NotFound,admin/SystemInfo} | 全站四态/对话框/toast 观感一次到位 |
| **C 表格/徽章/表单/Tabs 全面原生** | 摘 .table/.badge/.field 续挂（§3.8 保名除外）；Chip color 化；TableSortLabel；TextField label 浮标化；Tabs 换装（repos/repo/authcfg/tabs）；water-bar/quota-bar/deploy-progress→LinearProgress；warn-box/form-error/gc-error/conflict-panel/readonly-note→Alert；danger-zone/repo-form-section/authcfg-group→Paper；radio/check→RadioGroup+FormControlLabel | pages/repositories + security + governance + audit + search + monitoring + admin/{license,authconfig} + styles/{pages,governance}.css | 「像 MUI」的主体波（面最大） |
| **D 树域与收尾** | tree-pane/checksum-badge/status-pill 收尾；node-tabs/smu-tabs 换装评估（键盘链路核验先行，不过即保留）；cmd-block/token-panel/gc-result Paper 化可选项；死类清扫（每 css 逐类 grep src 零消费即删）；tokens.css 收缩评估（**theme-smoke 断言的 token 集必须保留**）；全量走查清单 + axe 全页双主题复扫 | pages/artifacts + components/dialogs + styles 收尾 | 长尾与清扫 |

依赖关系：A → (B, C) → D；B 与 C 可并行（area 不重叠：B=components+批三页，C=admin/治理/安全页）；D 收尾。

## 7. 登记与后续

1. **console-ux 修订建议（v1.14 回写，归 ux-designer 后续票）**：§7.2「13px 主字号/32px 硬带」→「14px 主字号（MUI 默认档）/ MUI small 密度档（表格行 ≈33px、控件 small 带）」；§7.1 token 表维持（tokens.css 不动）；本票不改 console-ux.md（纪律②）。
2. **theme-smoke 约束**：`--bf-sidebar/bg/surface-1/text/accent/shadow-1..3` 存在性断言（m8/theme-smoke.spec.ts）——tokens.css 任何收缩不得删这组。
3. **注入序事实**（T-300 §3）仍然成立，但退役后不再有「旧 CSS 压 MUI」战场；仅剩保留清单 CSS 与 MUI 无交叠。后续新增样式先过 §3.1 四问。
4. **预估风险**：AppBar/Drawer 入主包（A 波预算重点）；Chip filled 色与旧 tint 色差异大（C 波走查项 3）；TableSortLabel 的 aria-sort 表达与现 SortTh 等价（aria-sort 三态语义保留，spec 断言 users-sort-* aria-sort 不受影响）；datetime-local 的 TextField label 浮标在小屏的截断（C 波走查）。
