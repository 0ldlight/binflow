# BinFlow 设计系统方案（design-system-plan）

> 轨道 B-3 产出：Penpot-inspired 设计系统方案，Phase 1 Core UI Foundation 的设计输入。
> 上游：用户新宪章（UI/UX 现代化阶段，20 节，2026-09-14）§8~§14；`docs/reverse/frontend/parity-capture/`（Artifactory 7.161.20 实测证据）。
> 效力序：宪章 > 本文 > `frontend-rewrite-architecture.md`（30 节总令架构落点——本文是其视觉层升级细化，**不触碰其 §0 不可变契约**：挂载 /binflow/ui+/binflow/assets、构建链五步语义、i18n 三道闸、testid 锚 874 家族、Go 零改）。
> 红线（ADR-0029）：UX 心智模型对齐 Artifactory，视觉走自有皮肤——本文只升级视觉层，不动 IA/操作流/URL 深链。

## 0. 输入与证据基线

| 输入 | 现状 | 本文消费方式 |
|---|---|---|
| `parity-capture/tokens.json` | 骨架（palette/typography/radii/shadows/spacing/raw 六面为空数组；实测值经 penpot-sync 注入 Penpot Design Tokens 板，`tools/penpot-sync/penpot-spec.json` meta.palette 有抽样） | 参照 Artifactory 灰阶/密度节奏，**不复制其视觉**（clean-room UI 域细则，ADR-0029） |
| `parity-capture/dom-snapshots/sidebar.html` | Artifactory 侧栏 `max-width: 260px` | 佐证宪章 240px 是 BinFlow 自选值而非翻拍 |
| `parity-capture/states-gaps.md` | `dark-mode: open`——7.161.20 实例无 UI 主题切换控件 | **BinFlow 暗色无上游参照**，必须锚定自有 token 基线（本文 §3.2 双谱一等公民） |
| `web/src/styles/tw/{tokens.css,tailwind.css}` | 新栈 token 层 + @theme inline 桥接（142+107 行） | 现有缝的直接扩展位 |
| `web/scripts/assert-tokens.mjs` | 编译期色值断言（styles/tw/ 整层豁免） | 闸随 token 层迁移同步改路径与覆盖族 |
| `web/package.json` / `components.json` | Tailwind v4 + shadcn(new-york) + 19 个 ui/ 原语 | 迁移策略的 as-built 基线 |

## 1. 现状 → 目标差距（五点）

**G1 token 族不全（对照宪章 token 六族）**
现役 `tw/tokens.css` 五个族：色板（含侧栏身份）/间距（sp-1~7）/字号（**六级**：aux 12·body 13·form 14·h3 16·h2 20·h1 24，**system-ui 栈**）/圆角（3 档）/阴影（3 档）+ z（4 层）。缺口：动效族（时长/缓动/prefers-reduced-motion）**整族缺席**（现靠 Tailwind transition-colors 默认 150ms 隐式值）；焦点环未 token 化（focus-visible 直接引 --bf-accent）；控件高度无档（Button h-7/8/9 与 Input 高度各自为政）；布局尺寸是字面量（侧栏 w-56、顶栏 h-12、内容 max-w-[1440px]）；字阶未达宪章 Inter 九级。

**G2 双 token 层并存（as-built 漂移，登记技术债）**
`main.tsx:29-36` 同时加载 `styles/tokens.css`（旧 MUI 时代，147 行）与 `styles/tw/tokens.css`（新栈，142 行，值"原样迁移"）。tw 层头注释承诺"终验删旧文件后本文件成为唯一事实源"——MUI=0 终验已封板（`7c8bfaba`），**旧文件仍在模块图**。同名同值暂不冲突，但双事实源=每次改值要改两处，漂移只是时间问题。

**G3 样式范式分裂（CSS 分散）**
两套范式同页：Tailwind 语义类（新栈）与旧全局 CSS 类（`.badge`/`.filter-bar`/`.mono`/`.text-2`/`.text-muted`——`styles/{base,pages,governance}.css` 共 1,126 行 + `components/{dialogs,pager,pkg-icon}.css` 旁挂）。消费面：~68 个 TSX 用旧 utility、13 个用 `.badge`、7 个用 `.filter-bar`。设计系统层组件（`components/layout/bits.tsx` 的 Badge）本身也走旧 CSS 类——"design-system 组件靠 MUI 时代全局类"是分裂的实锤。宪章要求 design-system/ 收敛。

**G4 壳尺寸与宪章不符且无 token 承载**
侧栏 `w-56`（224px）vs 宪章 240px；顶栏 `h-12`（48px）vs 宪章 64px；字面量散在 `AppShell.tsx:125`/`Topbar.tsx:248`。改尺寸要 grep 全仓，且无文档化事实源。

**G5 状态覆盖缺口（宪章"状态全覆盖"）**
Button 无 loading 态（无 spinner+禁用语义）；Input 无 error/affix 规范（aria-invalid 仅个别页面手写）；Table 四态（loading/empty/error+retry/skeleton）不齐；暗色是"亮色整组换值"的从属形态而非双谱一等公民。Artifactory 本身无暗色（§0），BinFlow 暗色只能自有基线——状态矩阵必须双主题 × 七态显式定义（本文 §4）。

## 2. 迁移策略裁定：shadcn 原语保留 + 重 token 化

| 候选 | 内容 | 评估 |
|---|---|---|
| **A. 全重写组件层** | 弃 shadcn/Radix，自研全套原语 | 触面=100% TSX；Radix 的焦点管理/键盘语义重造，直接威胁 47 个 axe spec 与键盘腿；874 testid 锚与 URL 深链全量重验；工期数倍于 B；**parity 收益为零**（对齐的是 Artifactory UX 心智，不是它的 DOM） |
| **B. shadcn 保留 + 重 token 化 + 状态补全**（**选定**） | 原语留在 `components/ui/`（shadcn 源码进仓、无运行时锁定，components.json CLI 再生流程不断）；视觉升级走 token 缝——`@theme inline` 桥接已验证"改一层值、全站生效"（软缝协议：机制=token 槽位+桥接，字面值=批内冻结）；缺态逐件补（loading/error/affix）；缺件补齐（checkbox/switch/textarea/radio-group）；旧 CSS 类分批退役 | 触面=token 层重写+每件状态增量+~68 文件机械类替换；e2e 锚不动；axe 基线只升不降（Radix a11y 既有） |
| C. 换头他家 kit（Base UI/HeroVars 等） | 换基础库重生成 | 等于 A 的变体再付一次迁移税；Radix 现役无痛点证据；否决 |

**决策**：候选 B。
**后果**：① 视觉升级的杠杆全压在 token 层与组件 variant 定义——这正是设计系统"一处定义、处处生效"的本义；② shadcn CLI `add` 再生流程保留（components.json alias 不动），后续补原语零手工；③ 旧 CSS 类退役是**必须付的清偿**（否则 G3 分裂永存），分批计划 §6 承载；④ 验证载体=真实浏览器（Playwright e2e 全量 + 双主题 axe 47 spec + 截图差分，批 0 起建立基线）——不以"看起来对"收票。

## 3. Token 六族规格

### 3.0 机制层（沿用已验证缝，零新机制）

CSS custom properties 为唯一事实源；Tailwind v4 `@theme inline` 桥接为唯一消费缝（工具类直取 `var(--bf-*)`，`[data-theme]` 整组覆盖即时生效）；`assert-tokens.mjs` 继续做编译期闸（TSX 零色值字面量、零 `var(--bf-色彩系)`，token 定义层豁免）。目录收敛（宪章 §8）：

```
web/src/design-system/          # 收敛位（替代 styles/tw/ 两文件）
├── tokens/
│   ├── color.css               # 族 1 亮暗双谱
│   ├── typography.css          # 族 2 Inter 九级 + 行高/字重/特性
│   ├── spacing.css             # 族 3 间距 + 控件高度 + 布局尺寸
│   ├── radius.css              # 族 4
│   ├── shadow.css              # 族 5（含 ring）
│   ├── motion.css              # 族 6 z-index + 时长/缓动
│   └── index.css               # @import 聚合（唯一入口）
├── tailwind.css                # @theme inline 桥接层（唯一 Tailwind 入口）
└── fonts.css                   # Inter @font-face（§5）
```

`components/ui/` **维持不动**（shadcn CLI alias 依赖）；`components/layout/` 中的设计系统件（bits/fields/states）逐步升格进 `ui/` 或域目录（§6 批 5）。`assert-tokens.mjs` 豁免谓词 `styles/tw/` → `design-system/tokens/` 同批改。若宪章 §8 对目录字面另有指定，以宪章为准（本文只定机制：**token 定义层单一目录 + 唯一 Tailwind 入口**）。

### 3.1 族 1 — Color（亮暗双谱，各 27 变量）

语义角色不变（ADR-0029 §5.1 三阶纵深维持：侧栏最暗 < 内容底 < 卡片），新增状态软底/焦点环两槽。accent 色相=**品牌槽位**：机制由本文冻结，字面值待 B-1 品牌方向（brand/*.svg 十方向）终裁；缺省维持现役蓝 `#0b6bcb`（双主题对比度已验证）；约束写入闸：**禁 JFrog 橙族**（#F5A623/#FF6C00 邻域）、双谱 WCAG AA、与状态四色可区分。

| 角色 | 变量 | light | dark | 消费面 |
|---|---|---|---|---|
| 侧栏身份 | `--bf-sidebar{,-2,-3,-border,-text,-text-2}` | `#1b2430`/`#242f3e`/`#2c3949`/`#293545`/`#eef2f7`/`#a3b1c2` | `#0b0e13`/`#151b25`/`#1d2532`/`#1e2634`/`#e9edf3`/`#9aa4b2` | 侧栏/顶栏深底区（6） |
| 内容底 | `--bf-bg` | `#f3f5f7` | `#12161d` | 页面背景（1） |
| 面 | `--bf-surface-{1,2,3}` | `#ffffff`/`#e9edf1`/`#dfe5ec` | `#1a202a`/`#202834`/`#262f3d` | 卡片/悬停行/输入底（3） |
| 边 | `--bf-border{,-strong}` | `#d3dae2`/`#9aa6b4` | `#2b3442`/`#414c5e` | 分隔线/悬停边（2） |
| 文字 | `--bf-text{,-2,-muted}` | `#1d232c`/`#55606e`/`#646f7b` | `#e9edf3`/`#9aa4b2`/`#7f8a99` | 正文/次要/弱化（3，对比度注记在案） |
| 主色 | `--bf-accent{,-fg}` | `#0b6bcb`/`#ffffff` | `#4aa3ff`/`#0b0e13` | 主操作/链接/选中（2，**品牌槽位**） |
| 焦点环 | `--bf-ring` **新** | `#0b6bcb` | `#4aa3ff` | focus-visible outline（1） |
| 状态 | `--bf-{success,warning,danger,info}{,-fg}` | `#157f3d`/`#946200`/`#c9372f`/`#0b6bcb` + fg `#ffffff` | `#3fbf6f`/`#d9a23a`/`#f0605d`/`#6cb2ff` + fg `#0b0e13` | 徽章/状态灯/按钮（8） |
| 状态软底 | `--bf-{success,warning,danger,info}-surface` **新** | `#e8f5ec`/`#fdf3e0`/`#fdeceb`/`#e8f1fc` | `#12241a`/`#2a2214`/`#2a1a19`/`#14202e` | 软底徽章/alert/toast 底（4） |
| 遮罩 | `--bf-scrim` | `rgb(9 14 20/45%)` | `rgb(0 0 0/60%)` | modal 背板（1） |

计 27×2（暗谱另含 `--bf-pkgicon-*` 11 个既有包型图标档，原样保留不重设计）。悬停一律 `色/90`（opacity 派生），不设独立 hover 变量——槽位减法。

### 3.2 族 2 — Typography（Inter 九级，22 变量）

Inter 本地打包（§5）；CJK 回退栈**原样保留**（Inter 无 CJK 字形，zh 文本回退 PingFang SC/Microsoft YaHei——`--bf-font-sans` 头部前置 `'Inter'` 即可）。

| 级 | 变量 | 值 | 行高 | 用途（迁移映射） |
|---|---|---|---|---|
| 2xs | `--bf-fs-2xs` | 11px | 1.4（`--bf-lh-2xs`） | overline/kbd/侧栏分组标签 |
| xs | `--bf-fs-xs` | 12px | 1.4 | 辅助列/徽章（←fs-aux） |
| sm | `--bf-fs-sm` | 13px | 1.45 | **表格密度主档**（←fs-body） |
| base | `--bf-fs-base` | 14px | 1.5 | 表单/正文默认（←fs-form） |
| md | `--bf-fs-md` | 16px | 1.45 | 卡片标题/tab（←fs-h3） |
| lg | `--bf-fs-lg` | 18px | 1.4 | 弹窗标题（**新**——现 16/20 之间缺档） |
| xl | `--bf-fs-xl` | 20px | 1.35 | 页面标题（←fs-h2） |
| 2xl | `--bf-fs-2xl` | 24px | 1.3 | 登录品牌（←fs-h1） |
| 3xl | `--bf-fs-3xl` | 30px | 1.2 | 空态/登录展示位（**新**） |

配套：字重 `--bf-weight-{regular,medium,semibold}` = 400/500/600（Inter 可变轴覆盖）；`--bf-font-feature` = `"tnum" 0`（默认关）+ 表格/时间戳列局部 `font-variant-numeric: tabular-nums`（Inter 默认比例数字会让 sha256/时间列抖动——mono 列已有 `--bf-mono`，数字表格列补 tnum）；≥18px 级 letter-spacing -0.011em、2xs 大写 +0.05em。旧名（fs-aux 等 6 个）在 `tokens/typography.css` 尾部**别名保留**，批 5 清偿后删。mono 栈 `--bf-mono` 不变。

### 3.3 族 3 — Spacing（14 变量）

4px 基数维持：`--bf-sp-1~7` = 4/8/12/16/24/32/48px 原样 + **新 `--bf-sp-8` = 64px**（区块节奏）。控件高度档 **新**：`--bf-ctl-{sm,md,lg}` = 28/32/36px（Button/Input/Select 共用，替代各自 h-7/h-8/h-9 字面量）。布局尺寸 **新**：`--bf-sidebar-w` = **240px**（宪章 §13；Artifactory 260px 不翻拍）、`--bf-topbar-h` = **64px**、`--bf-content-max` = 1440px（现 max-w-[1440px] 字面量收编）。

### 3.4 族 4 — Radius（5 变量）

`--bf-r-{xs,sm,md,lg,full}` = 2/4/6/8/9999px。新增 xs（tag/kbd 微圆角）与 full（pill 徽章/toggle）。控制台密度维持小圆角——不追 Penpot 大圆角（那是画布工具的视觉语态，仓库控制台信息密度优先）。

### 3.5 族 5 — Shadow（4 变量）

`--bf-shadow-{1,2,3}` 原样（卡片/浮层/modal 三档；暗色弱影+边框补偿规则维持）+ **新 `--bf-ring-width`** = 2px（焦点环宽度统一，配合族 1 `--bf-ring` 色）。

### 3.6 族 6 — Elevation & Motion（9 变量）

z 维持四层：`--bf-z-{nav-sticky,dropdown,modal,toast}` = 70/80/90/100。**新**动效三档：`--bf-dur-{fast,base,slow}` = 120/160/240ms + `--bf-ease` = `cubic-bezier(0.2, 0, 0, 1)`（Linear 式减速出）；全局 `@media (prefers-reduced-motion: reduce)` 降为 1ms（axe/可达性硬要求，宪章状态全覆盖的可及性腿）。

**规模合计**：27×2（色双谱）+ 22（字）+ 14（距）+ 5（角）+ 4（影）+ 9（层动）+ 11（pkgicon 暗谱，存量）≈ **119 个 CSS 变量**；Tailwind 桥接 `@theme inline` 映射约 70 条（色/字/距/角/影/动效语义类）。

## 4. 组件清单与优先级

### 4.1 清单与分层

| 层 | 件（状态矩阵覆盖度：★=全矩阵 §4.2 / ☆=紧凑矩阵） | 批次 |
|---|---|---|
| **P0 通用原语（20）** | Button★ Input（含 affix 前后缀）★ Textarea☆ Label☆ Checkbox☆ Switch☆ RadioGroup☆ Select☆ Badge/StatusPill（软底+dot 变体）☆ Table=DataTable（轻量表格壳）★ Tabs☆ Breadcrumb☆ Modal=Dialog★ Drawer（drawer/sheet 二合一收敛）★ Toast=sonner 主题化★ Tooltip☆ Skeleton☆ Spinner**新**☆ EmptyState★ ErrorState★ | 批 1~4 |
| **P1 核心域件（11）** | ArtifactTree★（TanStack Virtual+懒加载+右键菜单） ChecksumBlock★（sha256/sha1/md5 行+copy+截断reveal） PathBreadcrumb★（repo 段+路径段+溢出省略+整径复制） PropertiesTable☆ CopyButton☆（已存在升格） PkgIcon（存量，品牌 B-1 联动） Pager（存量升格） DropdownMenu☆ ContextMenu☆ Popover☆ FormField 胶水（RHF+zod 错误态规范）☆ | 批 6 |
| **P2 管理面/打磨（8）** | Command/palette 重皮、AG Grid 主题（主题变量对齐双谱）、Card、ScrollArea、Separator、ConfirmDialog 升格、TransferBox 重皮、列选器 | 批 6+ |

### 4.2 状态矩阵（七态：light/dark × hover/active/disabled/loading/error——矩阵内为态的实现载体与验收锚）

模板（每件一表，通用列裁剪到该件适用的态）：

| 态 | 载体（token/机制） | 验收锚 |
|---|---|---|
| light/dark | 双谱 token 即时切换（`[data-theme]`） | axe 双主题扫描 0 serious |
| hover | `色/90`（实底件）或 surface-2（描边/ghost 件） | 截图差分 |
| active/focus-visible | `--bf-ring` 2px outline-offset 2px（不可 `outline:none`） | 键盘 Tab 走查 e2e |
| disabled | `disabled:opacity-50 pointer-events-none`（统一） | axe |
| loading | Spinner + aria-busy + 真禁用（防双击提交） | e2e 提交竞态腿 |
| error | aria-invalid + border→danger + 错误文案 text-danger + 关联 aria-describedby | axe + RHF 腿 |

**Button★**：variant 六档（default/secondary/outline/ghost/destructive/link）× size 三档（ctl-sm/md/lg）× 七态。loading=左 spinner+文字保留+disabled 语义；link 变体无 loading。锚：既有 button testid 家族不动。
**Input★**：default/hover（border-strong）/focus（ring）/error（danger 边+文案）/disabled/read-only/affix 变体（mono 后缀如单位、copy 按钮）。高度=ctl 档。
**Table=DataTable★**：壳态=loading（skeleton 行×8）/empty（EmptyState）/error（ErrorState+retry）/就绪；行态=hover（surface-2）/selected（accent-surface 10% 派生）/disabled 行（批量操作进行中）；列态=排序指示/列选器。AG Grid 面的主题对齐在 P2。
**Modal=Dialog★**：open/close（dur-fast+fade，scrim 点击关）、destructive 头（danger 标题+主钮 destructive）、表单提交中（footer 钮 loading+防 esc 关——dirty 表单守卫存量语义保留）、焦点陷阱（Radix）。
**Drawer★**：右抽屉（≥768px）/底部 sheet（移动）双形态收敛为一个 API；sticky footer（主/次钮）；内容滚动+头固定。
**Toast★**：success/error/warning/info 四底（*-surface 软底+状态色文字+左缘条）+action 链接+自动消隐 dur-slow；双主题各验。
**Tabs☆**：active（accent 下划线指示+semibold）/hover（surface-2）/disabled；键盘左右箭头（Radix 内建）。
**Breadcrumb☆**：末段实色、中间段 hover accent、溢出省略（Popover 展开）；PathBreadcrumb 加 mono 段+整径 copy。
**EmptyState★**：图标（lucide，text-muted）+标题（fs-md）+描述（fs-sm text-2）+可选主操作钮；空数据/无权限两文案面。
**ErrorState★**：错误图标（danger）+标题+错误细节（mono 折叠，fs-xs）+retry 钮；401/403/5xx/网络四文案面（既有 error-card 锚不动）。
**ArtifactTree★**：节点态=loading（子级展开中 spinner 行）/error（行内 retry）/懒加载 more/空；行态=hover/selected（accent 左缘 2px+surface 软底）/cut 目标（copy/move 拖悬）；右键菜单（Radix ContextMenu）。
**ChecksumBlock★**：行态=copied（icon 切换 1.2s）/copy-fail（danger toast）；长哈希截断+reveal（mono，tnum）；缺失 checksum=「—」占位不伪造。

## 5. 字体接入方案：Inter 本地打包（CDN 否决）

**抉择：`@fontsource-variable/inter`（npm 依赖，vite 管道收编）**。

- **CDN 否决**（Google Fonts/自建 CDN 均同）：① BinFlow 是自托管制品仓库控制台，**离线/气隙部署是一等场景**——CDN=外网依赖+隐私外泄（Referer）+CSP 面+渲染不确定；② go:embed 单二进制约束下，任何运行时外取资源都破坏"单二进制即可用"的部署承诺（ADR-0014 精神）；③ 字体加载失败时 FUA 回退不可控。
- **包与体积**（维护状态实查：Fontsource 5 系列在册，`@fontsource-variable/inter@5.3.0`；文件大小经 jsdelivr API 实测）：`inter-latin-wght-normal.woff2` = **48,256B**（单文件覆盖全字重轴 100~900，比 3 个静态字重 ~90KB 更小）；`inter-latin-ext-wght-normal.woff2` = 85,068B（en locale 扩展拉丁——批 3 评估 en 文案实际字符集后决定是否引入）。预算：latin 必选 +48KB，占 5MB 警戒位 <1%，`make console-size` 无压力。
- **管道零改动论证**：`@fontsource` 的 CSS `url(woff2)` 经 vite 打包落 `dist/assets/<hash>.woff2`，属 `relink-assets.mjs` **face 2**（CSS url() 重写 /binflow/ui/assets→/binflow/assets）既有覆盖面——指纹资产+immutable 缓存直接继承，**无脚本改动**。引 `design-system/fonts.css` 单点（`@import '@fontsource-variable/inter/latin.css'` 或 wght 单轴 CSS），`font-display: swap`。
- **CJK**：不引中文 webfont（数 MB，无收益）——栈=`'Inter', system-ui, -apple-system, 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif`；中英混排的基线/字重差异由 line-height 与 -0.011em 微调吸收，批 3 截图验收专门看 zh 段。

## 6. 迁移分批计划（每批可验证：build + e2e + 截图 + axe）

| 批 | 内容 | 验收门（全批共通四门：typecheck/lint/build+console-size；下表为增量门） | 回滚 |
|---|---|---|---|
| **批 0 基线** | Playwright 截图基线：核心页 × 亮暗 × 关键态（复用 parity-capture 截图基建）；建立当前值 golden | 基线集落库（tests  fixture 或 reports，不进二进制）；axe 双主题现值记录 | 无需（纯增量） |
| **批 1 token 收敛** | `styles/tw/` → `src/design-system/`（六族分文件+聚合入口+桥接层+fonts.css 骨架）；**删旧 `styles/tokens.css`**（G2 清偿——同名同值，删后 grep 零断链）；旧字号名别名保留；assert-tokens 豁免谓词同步；新槽位（ring/状态软底/ctl/布局尺寸/motion）**只定义不消费** | 四门 + 全量 e2e + assert-tokens + 截图差分≈0（值未变）+ grep 旧 tokens.css 引用=0 | git revert 单提交 |
| **批 2 壳与布局** | 侧栏 240/顶栏 64/内容 max 进 token；侧栏分组/active 指示重皮（Penpot 式）；间距节奏（sp-8 区块） | e2e shell 族 + 截图（**基线预期翻新——本批集中承受 G4 视觉变更**）+ axe 双主题 | 恢复 w-56/h-12 字面量（token 值回滚） |
| **批 3 Inter 上线** | fonts.css 引入 latin woff2；九级字阶生效（别名映射旧级）；表格/审计列 tabular-nums；zh 回退验收 | console-size（+≤50KB 预算腿）+ 截图 zh/en 双语 + e2e 文本断言全绿（防长度敏感断言） | 去掉 import 即回 system 栈 |
| **批 4 P0 原语补全** | 缺件四件（checkbox/switch/textarea/radio-group）+ Button loading/Input error-affix/Badge 软底/Toast 主题/Tabs 指示/Breadcrumb/Empty+ErrorState 统一；**内嵌 styleguide 路由**（`/binflow/ui/dev/styleguide`，仅 dev+e2e fixture 可达，生产路由不注册）承载 §4.2 全矩阵可视化 | styleguide 截图=矩阵逐态 golden + axe + 涉及页 e2e | 单件独立提交，逐件 revert |
| **批 5 旧 CSS 退役** | `.badge/.filter-bar/.mono/.text-2/.text-muted` → Tailwind 语义类（~68 文件机械替换，bits.tsx Badge 升格进 ui/）；`base/pages/governance.css`+`dialogs/pager/pkg-icon.css` 中类族逐段迁移或删除；旧字号别名删 | 四门 + e2e（类断言腿同步迁移）+ 截图差分≈0（纯等价替换）+ grep 旧类名=0 | 按类族分提交逐族 revert |
| **批 6 域件与打磨** | ArtifactTree/ChecksumBlock/PathBreadcrumb/PropertiesTable 重皮；AG Grid 主题双谱；motion token 应用到浮层/抽屉/toast；P2 件 | Explorer 核心流 e2e（树/上传/下载/深链族）+ 截图 + axe 全量 + prefers-reduced-motion 腿 | 域件独立提交 |

批 1~6 串行（每批出票走 IMPLEMENTING→REVIEW→QA 八态）；批内多票可按 area 并行（token 层/ui 原语/页面替换三区互斥）。

## 7. 验证与守门（对 §6 的收口定义）

1. **编译门**：四门绿（typecheck/lint/build/assert-tokens+assert-i18n+console-size）逐批执行，输出进票日志。
2. **结构门**：`design-system/tokens/` 外零 CSS 变量定义（assert-tokens 扩腿）；TSX 零色值字面量、零 `var(--bf-*)` 直引（既有腿维持，白名单=z-nav-sticky 类布局例外收敛进语义类）。
3. **引用门**：shadcn 原语再生成走 components.json alias 不漂移；旧 tokens.css/旧类名删除后 grep 零残留。
4. **出处门**：视觉参数以宪章为权威；Artifactory 参照只引 `docs/reverse/frontend/parity-capture/` 实测（不引反编译样式表结构）；Inter 体积/许可（SIL OFL，可自由嵌入分发）出处=Fontsource/jsdelivr 实查（本文 §5 留痕）。
5. **真实载体**：每批验收必须含真实浏览器证据——Playwright e2e 全量 + 双主题 axe + 截图差分；批 4 styleguide 是状态矩阵的九宫格验收面。

## 8. 风险与已知妥协

| # | 风险/妥协 | 处置 |
|---|---|---|
| R1 | accent 色相悬置（待 B-1 品牌终裁） | 槽位机制已冻结，缺省蓝可交付；换值=改 2 变量+截图基线翻新一晚 |
| R2 | 240/64+Inter 三连改引发截图基线大面积翻新 | 集中在批 2/3 承受，批 4 起基线稳定 |
| R3 | e2e 类断言（.badge 等）迁移面 | 批 5 按类族分票，锚 testid 优先原则不变 |
| R4 | 旧 tokens.css 删除的隐性引用（CSS 内 var 链） | 批 1 内 grep+构建双验；同名同值故风险=级联丢失，构建即红 |
| R5 | zh/en 混排基线抖动（Inter×PingFang） | 批 3 专项截图+line-height 微调；不稳则 zh 段落降级 system 栈（栈序已保证回退） |
| R6 | latin-ext 是否引入 | 批 3 按 en 文案字符集实测决定（缺字符才引，+85KB） |
| 妥协 | AG Grid 主题双谱对齐延到批 6（P2） | Explorer 主面先用轻量 DataTable 矩阵承载；AG Grid 是皮肤非交互面，风险低 |
| 妥协 | 多主题/换肤系统不做（YAGNI） | 双谱即全集；charter 未要求第三主题 |

## 9. 技术债登记（as-built，本文派生）

| 债 | 现状 | 清偿位 |
|---|---|---|
| 双 token 层 | styles/tokens.css 与 tw/tokens.css 并存（G2） | 批 1 |
| 设计系统件依赖旧全局类 | bits.tsx Badge 走 base.css .badge（G3） | 批 5 |
| 布局尺寸字面量 | w-56/h-12/max-w-[1440px] 散在壳组件（G4） | 批 2 |
| 动效隐式值 | Tailwind 默认 150ms 无 token（G1） | 批 1 定义/批 6 消费 |
