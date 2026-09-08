# BinFlow 前端重写 Phase 0 审计（frontend-rewrite-audit）

> 30 节重写总令 §二十三 Phase 0 产出。四路并行审计（架构与依赖 / 页面与能力 / API 契约 / 质量资产）+ 完备性批评家交叉核查，全部数字来自实测（grep/wc/脚本运行），基线 = develop@74174a07（T-512 已入库后的完整基线）。
> 配套：frontend-capability-matrix.md（能力三面对账）/ frontend-rewrite-architecture.md（新架构设计）。

## 0. 执行摘要（conductor）

**规模事实**：58 页面文件 23,788 行 + 15 共享组件 + 15 lib 模块 + 4 Context；84 e2e spec ~423 用例；i18n 2,054 键 2,713 调用点（自研 zh-as-key 内核）；testid 锚 874 src 家族 / 4,218 spec 引用（76/84 spec 覆盖）。
**依赖事实**：运行时仅 6 包（MUI+Emotion×3 占一半）——新栈全部组件/状态/数据/表单/表格/虚拟化/图表/AI 库均为**净新增**；SPA gzip 预算 5MB（make console-size 警告位）。
**绝对约束链**（重写保真清单）：① /binflow/ui + /binflow/assets 双挂载与 relink/wire 两脚本语义；② build 五步链（assert-tokens → assert-i18n → vite build → relink-assets → wire-brand-assets）各自检；③ i18n zh-as-key+common 路由+en 懒 chunk+reload 切换+三道闸；④ testid 锚制（874 家族原样迁入——§10.5 先例证明换栈零锚改名可行）；⑤ 种子/mock-idp/roles 等 e2e 资产栈无关可整体保留；⑥ dev/build/typecheck/lint/e2e 入口名。
**重大解锁面**（API 有而 FE 未用）：copy/move/archive UI（0 调用——最大交互缺口）、auth/methods（替换登录页 302 探测 hack）、keypair 管理页、webhook outbox 死信面、builds 写面（promote/retention）、QRL/settings、UI search 家族、reindex 动作、MPU 分片上传。
**契约漂移**（重写前回填 fern）：D1 system/logs、D2 builds 全域、D3 bundles HEAD+status、D4 OIDC 族——四处 FE 在用而 OpenAPI 未收。
**盲区裁定**（批评家发现，架构文档承址）：dev proxy 缺口（event/内容面未代理）；assistant-ui 零后端（Phase 5 只搭壳）；AG Grid 授权（社区版特性集内实现）；Monaco worker 与 relink 的资产路径风险。

## 0.1 报告间矛盾裁决表（批评家实测，本表为唯一口径）

| # | 争议 | 裁决（实测） |
|---|---|---|
| 1 | License GET 路径 | `api/system/license`（页面报告误记 /v1/license） |
| 2 | lazy 页面数 | **33**（main.tsx 实数） |
| 3 | 显式 Route 条数 | **57** |
| 4 | 兼容重定向 | **4 条** |
| 5 | testid 锚量级 | **874 家族 / 4,218 引用**（质量报告为准；242 系 M8 时代旧数） |
| 6 | replication 资源标识 | 路径参数统一为 `{key}`（PUT 启停/DELETE/run） |
| 7 | 全局封锁端点 | `POST …/block`、`…/unblock`（GET `/v1/system/replications` 只读） |
| 8 | components 计数 | 12 tsx + 3 css = 15 文件（BrandLockup 在 BrandLogo.tsx 内） |
| 9 | Roles 能力形态 | **闭集 N/A 裁定**——adminRole 三值闭集+无管理页+无独立 API（matrix 单列行） |
| 10 | copy/move 语义区分 | 树右键「复制」=剪贴板复制路径（客户端）；`api/copy|move` =服务端制品操作（FE 0 调用）——matrix 分立两行 |

---

# 一、架构与依赖面审计报告

# BinFlow web/ 控制台架构与依赖面审计报告

审计基线：develop 工作树（含未提交 M17 builds 票增量），2026-09-08。全部数字来自实际 grep/wc 实测。
路径约定：省略前缀均为 `/Users/lzw/dev-center/web/`。

---

## 1. web/package.json 全量

`/Users/lzw/dev-center/web/package.json`（name=binflow-console, private, 0.1.0, ESM）

### 1.1 dependencies（6 条）

| 包 | 版本 | 用途 | 重写处置 |
|---|---|---|---|
| react | ^19.1.0 | UI 运行时 | 保留（新栈 React19） |
| react-dom | ^19.1.0 | DOM 渲染 | 保留 |
| react-router-dom | ^7.6.0 | 路由（声明式 `<Routes>`，非 data router） | 保留（新栈 ReactRouter 同族） |
| @mui/material | ^7.3.11 | 组件库全站唯一 UI 库 | **重写退役** |
| @emotion/react | ^11.14.0 | MUI v7 样式引擎（**src/ 零直接 import**，纯传递依赖） | **重写退役** |
| @emotion/styled | ^11.14.1 | 同上，纯传递依赖 | **重写退役** |

关键事实：运行时依赖仅 6 个，无状态库（无 redux/zustand）、无数据层库（无 react-query）、无表单库（无 RHF）、无图标包（@mui/icons-material 未装，NavIcons.tsx 裸 SVG 内联）、无图表库、无 i18n 框架（自研 <1KB 内核）。新栈的 Zustand/TanStack Query/RHF+Zod/AG Grid/TanStack Virtual/Monaco/ECharts/assistant-ui/Lucide 全部是**净新增**。

### 1.2 devDependencies（12 条）

| 包 | 版本 | 用途 | 重写处置 |
|---|---|---|---|
| vite | ^7.3.6 | 构建/dev server | 保留 |
| @vitejs/plugin-react | ^5.2.0 | React fast-refresh/Babel | 保留 |
| typescript | ^5.8.0 | 类型（typecheck=tsc --noEmit；也被 assert-i18n.mjs 当解析器用） | 保留 |
| @types/react / @types/react-dom | ^19.1.0 | 类型 | 保留 |
| @types/node | ^22.15.0 | 构建脚本类型 | 保留 |
| eslint | ^9.28.0 | lint（flat config） | 保留 |
| @eslint/js | ^9.28.0 | JS recommended 基底 | 保留 |
| typescript-eslint | ^8.33.0 | TS recommended | 保留 |
| eslint-plugin-react-hooks | ^7.1.1 | hooks 规则（v7 编译器派生规则，5 条降 warn） | 保留 |
| @playwright/test | ^1.52.0 | e2e（84 个 spec） | 保留 |
| @axe-core/playwright | ^4.13.0 | a11y 扫描 | 保留 |

### 1.3 scripts（8 条）

| script | 命令链 | 说明 |
|---|---|---|
| dev | `vite` | 5173 端口，proxy 到 127.0.0.1:8080 |
| build | `node scripts/assert-tokens.mjs && node scripts/assert-i18n.mjs && vite build && node scripts/relink-assets.mjs && node scripts/wire-brand-assets.mjs` | **五段链，四段自研守卫/后处理，重写必须原样保留语义** |
| preview | `vite preview` | |
| typecheck | `tsc --noEmit` | |
| lint | `eslint . && node scripts/assert-i18n.mjs` | lint 尾部复挂 i18n 闸（CI 双路生效） |
| e2e | `playwright test` | 不自起服务器，需先 `./bin/binflow-server serve` |
| assert:tokens | 单独执行色彩闸 | |
| assert:i18n | 单独执行 i18n 闸 | |

自研脚本职责（`web/scripts/`）：

| 脚本 | 作用（重写约束） |
|---|---|
| assert-tokens.mjs | 编译期色彩闸：全部 css 禁色值字面量（tokens.css 唯一豁免）；TSX 的 sx/style 内 hex/rgb/hsl 仅豁免 src/app/MuiProvider.tsx；TSX 内禁 `var(--bf-色板)` 引用（必须走 theme.palette）。**退役 MUI 后此脚本的 TSX 腿语义需随 Tailwind 主题重定义** |
| assert-i18n.mjs | 三道闸：①src 禁硬编码 CJK（行尾 `// i18n-allow` 豁免）②t() 键集与 en 目录包双向同构 ③manifests/zh/*.json 清单一致。用 typescript 编译器 API 解析。**zh-as-key/catalog/lazy/build 检查的「build 检查」即此** |
| relink-assets.mjs | vite build 后把三处 `/binflow/ui/assets/`（index.html、CSS url()、JS chunk 内 assetsURL join）改写到共享挂载 `/binflow/assets/`——/binflow/ui/** 保持纯 SPA shell（dumb history fallback）。**绝对约束的机械化部分** |
| wire-brand-assets.mjs | 把 dist/brand/**（favicon/PWA/manifest）搬入 /binflow/assets/ 并 sha1 内容指纹化（immutable 缓存契约），自校验死 URL |
| gen-brand-assets.mjs | 品牌资产派生器（SVG 母版→favicon/PNG/ico，playwright chromium canvas 渲染），手动复跑 |
| seed-m8/m9/m10.mjs、mock-idp.mjs、gen-pathmatch-fixtures.mjs、anchor-audit.mjs、t463/regen-catalogs.mjs | e2e 播种/OIDC mock/fixture 生成/锚点审计/i18n 目录再生成 |

### 1.4 engines

`node: ^20.19.0 || >=22.12.0`（Makefile `make console` 同款门）。

### 1.5 嵌入流水线（make console，/Users/lzw/dev-center/Makefile:47-51）

`cd web && npm ci && npm run build` → `rm -rf internal/console/dist/assets` → `cp -R web/dist/. internal/console/dist/` → `console-size`（gzip js+css 汇总，>5MB 告警，PRD W37）。go:embed 落点 `internal/console/console.go:28 //go:embed dist`；git 仅跟踪 placeholder.html，产物不提交。**新栈体积红线：AG Grid+Monaco+ECharts 同床后 5MB gzip 预算是硬约束。**

---

## 2. vite.config.ts（33 行，/Users/lzw/dev-center/web/vite.config.ts）

| 项 | 值 |
|---|---|
| base | `/binflow/ui/`（SPA 挂载段；与 main.tsx 里 BrowserRouter basename 字符串 `"/binflow/ui"`（无尾斜杠）手工同步——注释明示契约） |
| plugins | 仅 `react()`，零自研插件 |
| build | outDir=dist，sourcemap=false，target=es2020 |
| 分块策略 | **无 manualChunks**——默认 Rollup：入口 vendor 单块 + 每个 `lazy(import)` 页面各一 chunk（main.tsx 约 40 个 lazy 页面 = 约 40 路由分片） |
| server | port 5173；proxy `/binflow/api` 与 `/binflow/assets` → `http://127.0.0.1:8080`（dev 时 API/资产走真实二进制，仅 SPA 热更） |

重写注意：base+basename 双真值、proxy 两条、无分块策略这三点是 dev/build 可用的最小闭环；新栈若引 Tailwind/Path 别名需在此扩，但 `/binflow/ui/` base 与两段挂载语义不可动。

## 3. tsconfig / eslint 形态

**tsconfig.json**（src+e2e 合一）：target ES2020、moduleResolution bundler、verbatimModuleSyntax、moduleDetection force、jsx react-jsx、**strict + noUnusedLocals + noUnusedParameters + noFallthroughCasesInSwitch 全开**、noEmit、allowImportingTsExtensions。无 path alias（全部相对导入）。
**tsconfig.node.json**：vite.config.ts + playwright.config.ts 专用（ES2022、composite、emitDeclarationOnly → .tsbuild-node）。
**eslint.config.js**（flat，eslint 9）：js.configs.recommended + tseslint.configs.recommended + reactHooks v7 `flat['recommended-latest']`（编译器派生规则 error），5 条现存故意模式规则降 warn（set-state-in-effect/refs/immutability/purity/preserve-manual-memoization）；scripts/**.mjs 与 e2e/**.mjs 各给 Node globals 白名单。忽略 dist/node_modules/playwright-report/test-results。

---

## 4. main.tsx + src/app/**（providers 与路由）

### 4.1 引导顺序（/Users/lzw/dev-center/web/src/main.tsx，304 行）

1. 模块顶层 import 四个全局 css（tokens/base/pages/governance）；
2. `consumeStepUpFragment()`（src/lib/stepUpGrant.ts）：渲染前同步消费 OIDC step-up 回跳 fragment（提取入内存 + history.replaceState 抹除）；
3. `initI18n().then(render)`：i18n 引导闸——同步 `<html lang>`，en 才懒载目录包，zh 零请求；
4. Provider 树（外→内）：

```
StrictMode
└ ThemeProvider            (app/ThemeContext.tsx 52行：light/dark，localStorage 'binflow-console-theme'，
│                            首访随 prefers-color-scheme，写 <html data-theme>；默认亮色=ADR-0029 Q2)
└ MuiProvider              (app/MuiProvider.tsx 109行：消费 useTheme 重建 createTheme；palette 字面量
│                            复刻 tokens.css 双主题（唯一色值豁免层）；small 密度全家；CssBaseline)【重写退役】
└ ToastProvider            (app/ToastContext.tsx 124行：右下 Snackbar+Alert 堆叠，success 5s/error 常驻，aria-live)
└ BrowserRouter            (basename="/binflow/ui")
  └ AuthProvider           (app/AuthContext.tsx 104行：挂载 GET /v1/session 探活；全局 401 监听→
    │                        「登录已过期」toast + /login?return= 重登；login/logout POST/DELETE session)
    └ ConfirmProvider      (components/ConfirmDialog.tsx：危险确认 modal 上下文)
      └ Suspense           (fallback=RouteFallback：MUI CircularProgress + 「加载中…」)
        └ Routes
```

要点：AuthProvider 在 Router 内（用 useNavigate/useLocation）；无 QueryClientProvider/StoreProvider（新栈需在此层插 Zustand+TanStack Query）；MuiProvider 必须在 ThemeProvider 内（重写时该层由 Tailwind dark mode + shadcn 主题替代，ThemeContext 的 data-theme 机制可直接复用）。

### 4.2 路由结构全图（react-router-dom v7 声明式；全部 lazy() 分片）

壳：`/login` 独立页；`/` = AppShell（Outlet 布局，内含认证守卫）。index → Navigate `/artifacts`（登录落点 §1.1）。

**应用模式（8 域）**：

| 路由 | 页面组件（src/pages/） |
|---|---|
| /dashboard | DashboardPage.tsx (333行) |
| /artifacts、/artifacts/:tab/:key/*、/artifacts/:key/* | artifacts/ArtifactsBrowser.tsx (2044行，三路由一组件，URL 即状态，TAB∈general/properties/permissions) |
| /search | search/SearchPage.tsx (463行) |
| /builds、/builds/:name、/builds/:name/:number | builds/BuildsPage.tsx (545行，?started= 消歧) |
| /bundles、/bundles/:name、/bundles/:name/:version | bundles/BundlesPage.tsx (387行) |
| /profile | ProfilePage.tsx (496行) |

**管理模式五分组**：

| 分组 | 路由 → 页面 |
|---|---|
| 仓库 | admin/repositories{,/local,/remote,/virtual}→RepositoriesPage(907行)；…/{local,remote,virtual}/new→RepositoryFormPage(1687行, mode+rclass props)；…/new→RepoCreateCompat(?rclass= 兼容重定向)；…/:key→RepoDetailPage(690行)；…/:key/edit→RepositoryFormPage(edit) |
| 用户与权限 | admin/security/users→UsersPage(370)；users/new→UserCreatePage(415)；users/:name→UserDetailPage(435)；groups→GroupsPage(441)；groups/new 与 groups/:name/edit→GroupFormPage(343)；permissions{,/new}→PermissionsPage(241)/PermissionEditorPage(1189, mode)；tokens→TokensPage(650)；auth→redirect ldap；auth/:proto→authconfig/AuthConfigPage(674) |
| 治理 | admin/governance/audit→AuditPage(512)；quotas→QuotasPage(284)；replication→ReplicationPage(506)；trash→TrashPage(564) |
| 监控 | admin/monitoring/storage→StorageSummaryPage(315)；status→ServiceStatusPage(237)；logs→SystemLogsPage(413)；system-info→SystemInfoPage(127)；gc→GCPage(616)；backup→BackupPage(504) |
| 常规 | admin/general/webhooks→WebhooksPage(402)；license→LicenseAddonsPage(418) |

兼容重定向 4 条（T-459 一轮窗）：general/settings→monitoring/system-info、governance/{gc,backup}→monitoring/{gc,backup}、governance/webhooks→general/webhooks。`*`→NotFoundPage(36行，M7 前旧路径不再兜底)。计：显式 `<Route>` 44 条 + lazy 页面组件 38 个。

---

## 5. src/components/** 全清单（12 tsx + 4 css）

| 组件 | 行数 | 职责一句话 | @mui import 行 | 重写处置 |
|---|---|---|---|---|
| AppShell.tsx | 1107 | 双模式壳：侧栏(2 应用+18 管理项/NavIcons)、顶栏搜索+最近搜索、⌘K·/·快捷键、面包屑、帮助菜单、主题切换、语言切换、用户菜单、OIDC step-up 续铸、SetMeUp/Deploy 弹窗挂载 | 27 | 重写（最大单件） |
| SetMeUpDialog.tsx | 791 | Set Me Up 抽屉：按包型生成分发配置片段（npm/mvn/docker/pip…）+ 拷贝 | 7 | 重写 |
| DeployDialog.tsx | 612 | 部署弹窗：拖拽上传 + Maven GAV 预检 + 进度 | 14 | 重写 |
| Pager.tsx (+pager.css 26) | 180 | 统一分页器（Pagination+每页条数 Select） | 4 | 重写 |
| ConfirmDialog.tsx | 166 | 危险确认 modal 基座 + useConfirm 上下文（焦点圈进/Esc/Tab 循环） | 5 | 重写 |
| PkgIcon.tsx (+pkg-icon.css 88) | 113 | 包型图标单点接线：import.meta.glob(?raw) 内联 30 枚 SVG | 0 | 可直迁 |
| NavIcons.tsx | 117 | 侧栏图标槽：Material path 几何裸 SVG 内联（拒绝 @mui/icons-material） | 0 | Lucide 替代 |
| EmptyState.tsx | 83 | 空态（说明+主行动+插画槽，禁裸「暂无数据」） | 3 | 重写 |
| ErrorCard.tsx | 68 | 错误卡（人话+原始 message 折叠+重试，ApiError 消费） | 6 | 重写 |
| BrandLogo.tsx | 67 | 品牌资产单点引用（4 枚 svg?url，双主题 lockup） | 0 | 可直迁 |
| CopyButton.tsx | 53 | 一键拷贝（值不截断纪律；.copy-btn 类=e2e 钩子） | 2 | 重写 |
| Skeleton.tsx | 29 | 150ms 防闪烁骨架 | 1 | 重写 |
| dialogs.css | 192 | 弹窗族样式（压皮肤旧 CSS 残留） | — | 退役 |

---

## 6. src/styles/** 与全局 CSS 策略

| 文件 | 行数 | 内容 |
|---|---|---|
| tokens.css | 147 | 设计 token 唯一权威：80 条 `--bf-*` 声明，亮色 ：root / 暗色 `[data-theme='dark']` 整组覆盖；三阶纵深（sidebar 最暗<bg<surface）；色板+间距+字号+圆角+阴影 |
| base.css | 453 | 元素基线/布局原语/滚动条等 |
| pages.css | 505 | 跨页通用形态 |
| governance.css | 156 | 治理组页面样式 |

页面局部 css 10 个：security.css 485、artifacts/browser.css 247、repositories/tree/tree.css 240、repositories.css 155、admin/authconfig/authconfig.css 109、search/search.css 101、admin/license.css 27、components/{dialogs 192, pager 26, pkg-icon 88}。合计 14 css 文件约 2,907 行。

**策略结论**：全局/局部均为普通 css 文件 + CSS 自定义属性 token；CSS-in-JS 仅以 MUI `sx` prop 形态存在（413 行/49 文件）；**零 `styled()` 调用、零 @emotion 直接 import**。token 纪律由 assert-tokens.mjs 编译期强制。重写映射：tokens.css → Tailwind 主题/CSS variables 可平移，assert-tokens 的豁免层语义需重定义。

---

## 7. MUI/Emotion 使用普查（重写工作量直接证据）

`grep -rc '@mui|@emotion' src/ --include='*.tsx' --include='*.ts'`：
- **总命中：501 行，分布于 56 个文件**（src 计 60 tsx + 44 ts）。
- `@mui/material` import 出现 498 次；`@mui/icons-material` 0 次（NavIcons.tsx:6 注释提及即唯一出现）；**@emotion 直接 import 0 次**——两个 @emotion 包纯为 MUI v7 传递依赖，随 MUI 一并消失。
- `sx={` prop：49 文件 413 行（Emotion 运行时样式的主要载体）。
- 每文件命中基本=该文件 import 的 MUI 组件个数（一行一组件），故 top 榜≈MUI 消费密度榜：

| # | 文件（src/） | 命中行 | # | 文件（src/） | 命中行 |
|---|---|---|---|---|---|
| 1 | components/AppShell.tsx | 27 | 11 | components/DeployDialog.tsx | 14 |
| 2 | pages/security/TokensPage.tsx | 19 | 12 | pages/search/SearchPage.tsx | 13 |
| 3 | pages/repositories/RepositoryFormPage.tsx | 18 | 13 | pages/repositories/RepoDetailPage.tsx | 13 |
| 4 | pages/security/PermissionEditorPage.tsx | 17 | 14 | pages/repositories/ReplicationsSection.tsx | 13 |
| 5 | pages/repositories/RepositoriesPage.tsx | 17 | 15 | pages/governance/TrashPage.tsx | 13 |
| 6 | pages/artifacts/ArtifactsBrowser.tsx | 17 | 16 | pages/audit/AuditPage.tsx | 13 |
| 7 | pages/ProfilePage.tsx | 15 | 17 | pages/webhooks/SubscriptionDrawer.tsx | 12 |
| 8 | pages/webhooks/WebhooksPage.tsx | 14 | 18 | pages/governance/BackupPage.tsx | 12 |
| 9 | pages/webhooks/SubscriptionDialog.tsx | 14 | 19 | pages/security/GroupsPage.tsx | 11 |
| 10 | pages/artifacts/PropertiesTab.tsx | 14 | 20 | pages/admin/authconfig/AuthConfigPage.tsx | 11 |

（21 名后依次：UsersPage 10、QuotasPage 10、GCPage 10、NodeDetail 10、ResultsTable 9、ServiceStatus 9、Dashboard 9、Bundles 9、Builds 9、LicenseAddons 9、UserDetail 8、ReplicationPage 8、widgets 7、UserCreate 7、Permissions 7、StorageSummary 7、LoginPage 7、SetMeUpDialog 7、ErrorCard 6、AqlPanel 5，余 ≤4。）
即：**56/104 个 TS 源文件含 MUI 引用，全部 43 个页面级组件无一例外**——不存在「可整文件平移躲开 MUI」的页面；仅 NavIcons/PkgIcon/BrandLogo/muiAtoms 之外的 lib 纯逻辑层（约 30 个 .ts）与 MUI 无关。

---

## 8. src/lib/** API client 层形态

**核心 `src/lib/api.ts`（492 行）**——单文件 fetch 封装，无 axios/无 react-query：

| 面 | 形态 |
|---|---|
| API_ROOT | `/binflow/api`（webhooks 例外，见下） |
| 请求信封 rawRequest | fetch + `credentials:'same-origin'`；固定头 `Accept` + `X-BinFlow-Console:1`（第二层习惯，非强制）；body 三形态：`body`(JSON)/`formBody`(urlencoded，OAuth 族)/`rawBody`(text/plain，license 装载)；AbortSignal 透传 |
| CSRF | 同源 cookie `binflow_session` + 浏览器自动同源 Origin（服务端主防线）；无 token 头 |
| 会话/401 | 模块级 `setUnauthorizedListener` 单播 → AuthContext：已认证态 401 = 会话死（TTL/吊销）→ toast+带 return 重登；`silent401` 豁免探活/登录 |
| 错误 | `ApiError{status,message,raw}`；toApiError 统一三种错误体（E-01 errors[] JSON / 纯文本 / OAuth 形）；`errText()` 组件层统一入口 |
| 出口 | apiJSON（204→undefined）/apiText/rawRequest |
| 类型方式 | 手写 interface 逐端点声明（Whoami/AdminRole 闭集/RepoListItem/AuditEvent/ScheduleStatus/Ldap·Oidc·SamlAuthConfig 等），无 codegen/schema |
| 内含业务族 | session 三动词、version/health/storage-stats/schedules、节点属性 GET/PUT/DELETE（encodePropsQuery 逗号文法）、authconfig 三协议 get/put/test+TestReport 400 例外解包、SAML SP 证书两端点 |

**域模块**（同样手写封装，全部复用 api.ts 信封除 webhooks 外）：

| 文件 | 行数 | 职责 |
|---|---|---|
| lib/webhooks.ts | 404 | Webhook 订阅 CRUD/test；**挂载面 /binflow/event/api/v1/ 不在 /binflow/api 下——自带同源 fetch 信封（ApiError 复用）**；重写时双 baseURL 必须保留 |
| lib/governance.ts | 318 | 审计查询（keyset 游标）/GC/cron 消费面 |
| lib/repos.ts | 306 | 仓库域 CRUD（写动词回纯文本体）+usage |
| lib/replications.ts | 256 | 复制配置 CRUD+启停 |
| lib/stepUpGrant.ts | 179 | OIDC step-up grant：fragment 消费/useSyncExternalStore store/abandon |
| lib/addons.ts | 122 | license 装卸+addons 清单 |
| lib/trash.ts | 93 | 回收站 restore/empty/clean |
| lib/columnPrefs.ts | 112 | 列显隐偏好（localStorage per-page） |
| lib/useAsync.ts | 63 | **服务端状态四态容器（loading/ok/error/forbidden）——现有「数据层」全部基础**；重写映射 TanStack Query + 403 语义保留 |
| lib/format.ts / maven.ts / keys.ts / useVersion.ts / muiAtoms.ts | 57/40/35/40/41 | 格式化/GAV 预检/键盘共享/版本缓存/MUI sx 配方（压制配方已退役，近空壳） |
| 页面级 api | pages/security/api.ts 394、pages/builds/api.ts 237、pages/bundles/api.ts 95 | 域内就近封装（重写可收编统一层） |

**无轮询库/无缓存层**：10s 轮询（Replication/日志尾随）为组件内 setTimeout 自研。

---

## 9. ~/.npmrc 影响判定

- 项目级：`web/.npmrc` 与仓库根 `.npmrc` **均不存在**。
- 全局 `~/.npmrc`（121 字节，单行，无 `registry=` 键）：仅一条 `//127.0.0.1:18503/binflow/api/npm/npm-local/:_authToken=<token>`——本地 BinFlow npm 私仓（e2e 残留实例）的路径限定凭证。
- `npm config get registry` = `https://registry.npmjs.org/`（公网默认）。
- **结论：不影响公网 npm install**——该 token 只在向 `127.0.0.1:18503` 该路径发请求时生效，无 registry 重定向。conductor 无需处置；重写 `npm ci` 新栈照常走 npmjs 公网。若该本地实例下线，此行仅成死配置。

---

## 附：重写约束锚点速查（本次审计交叉验证的不可破坏面）

1. **双挂载**：`/binflow/ui/**`（纯 SPA shell，dumb fallback）+ `/binflow/assets/**`（指纹化共享资产）——由 vite base、relink-assets、wire-brand-assets、Go internal/console 四方共守；basename 字符串与 base 必须同步。
2. **build 五段链 + lint 双闸**（assert-tokens/assert-i18n 语义需在 Tailwind+shadcn 栈下重新落点）。
3. **e2e 契约**：84 个 spec、testid 242 锚、`.copy-btn/.toast/.empty-state` 等类钩子、chromium+m9+m10 三 project + matrix 观察窗、axe 扫描。
4. **i18n**：自研零依赖内核、zh-as-key、12 域、en 懒载、manifests/zh 12 文件对账——「不改 Go API」前提下文案键集是重写回归底稿。
5. **体积预算**：gzip js+css ≤5MB（W37），新栈重组件（AG Grid/Monaco/ECharts）必须按路由懒载。
6. **localStorage 键族**：binflow-console-theme / -locale / -cols-* / -recent-searches（跨页共享语义，重写沿用或迁移）。
7. **双 API 根**：/binflow/api 与 /binflow/event/api/v1（webhooks）。
8. **认证语义**：cookie session + 全局 401 单播 + silent401 豁免 + OIDC step-up fragment 预渲染消费。

# 二、页面与能力面审计报告

# BinFlow web/ 控制台 · 页面与能力面审计报告（重写 Phase 0 输入）

审计范围：`web/src/pages/**`（58 文件 · 23,788 行）+ 承载它们的 `web/src/main.tsx`（304 行路由表）、`web/src/components/**`（13 文件）、`web/src/lib/**`（15 文件）、`web/src/app/**`（4 Context）。全部按盘上现状实读，未凭文件名推断。

**基线技术事实（package.json 实证）**：React 19.1 + react-router-dom 7.6 + @mui/material 7.3.11 + Vite 7.3.6 + TS 5.8 + Playwright 1.52 + @axe-core/playwright。运行时依赖仅 6 个（react/react-dom/router/mui/emotion×2）——**无** TanStack Query（自研 `lib/useAsync.ts` 四态容器）、**无**状态库（Context + URL state + local state）、**无** RHF/Zod（全部表单手写校验）、**无**表格/虚拟化库（MUI Table + 手写分页/切片）、**无** Monaco/ECharts。i18n 为自研 zh-as-key（`src/i18n/index.ts`，en 目录包 `locales/en/*.ts` 12 命名空间 + `manifests/zh/*.json` 12 清单，`tr('<domain>')` 取词）。

---

## 1. 路由总图（main.tsx，basename=/binflow/ui，lazy 组件 33 个，含 5 条兼容重定向 + `*` 404）

| 域 | 路由 | 挂载组件（pages/…） | 备注 |
|---|---|---|---|
| 顶层 | `/login` | LoginPage.tsx | 独立布局（无壳） |
| 顶层 | `/`（index） | → `/artifacts` replace | 登录落点 |
| 顶层 | `/dashboard`、`/profile`、`*` | DashboardPage / ProfilePage / NotFoundPage | |
| 应用 | `/artifacts`、`/artifacts/:tab/:key/*`、`/artifacts/:key/*` | artifacts/ArtifactsBrowser.tsx | 三路由一组件；TAB∈{general,properties,permissions}；`?focus=` 旧深链组件内一次性 replace |
| 应用 | `/search` | search/SearchPage.tsx | `?q=&mode=aql&scope=builds` |
| 应用 | `/builds`、`/builds/:name`、`/builds/:name/:number` | builds/BuildsPage.tsx | 三视图一组件；`?started=` 消歧 |
| 应用 | `/bundles`、`/bundles/:name`、`/bundles/:name/:version` | bundles/BundlesPage.tsx | 三视图一组件 |
| 仓库 | `/admin/repositories`→`/local`；`/{local,remote,virtual}`；`/{rclass}/new`×3；`/new`（?rclass= 兼容映射）；`/:key`；`/:key/edit` | repositories/RepositoriesPage / RepositoryFormPage / RepoDetailPage | 建仓分路由承 rclass prop；`?section=replications` 直落第三步 |
| 安全 | `/admin/security/users[/new|/:name]`、`/groups[/new|/:name/edit]`、`/permissions[/new|/:name]`、`/tokens`、`/auth`→`/auth/ldap`、`/auth/:proto` | security/ 下 9 页 | proto∈{ldap,oauth,saml}，非法段组件内重定向 |
| 治理 | `/admin/governance/{audit,quotas,replication,trash}` | audit/AuditPage、governance/ 四页 | |
| 监控 | `/admin/monitoring/{storage,status,logs,system-info,gc,backup}` | monitoring/ 四页 + governance/{GCPage,BackupPage} | |
| 常规 | `/admin/general/{webhooks,license}` | webhooks/WebhooksPage、admin/LicenseAddonsPage | |
| 兼容 | `general/settings→monitoring/system-info`、`governance/{gc,backup}→monitoring/{gc,backup}`、`governance/webhooks→general/webhooks` | Navigate replace | 一轮窗口 |

壳（components/AppShell.tsx，1107 行）：双模式侧栏（应用 4 条目 / 管理 5 分组 18 条目）+ sticky AppBar（面包屑-管理模式 / 标题-应用模式、搜索/管理过滤框、帮助、主题、用户菜单）+ Container(maxWidth 1440)。认证守卫：`checking`→boot-screen、`anonymous`→`/login?return=`。

---

## 2. 逐域页面能力矩阵

### 2.1 顶层页

| 路由 | 文件（行数） | 职责 | 核心交互 | API 端点 | 关键依赖 | 四态/a11y/i18n |
|---|---|---|---|---|---|---|
| /login | LoginPage.tsx (255) | 登录 + SSO 入口 | 原生 form（Enter 隐式提交）；SSO 三态探测（probing→on/off，点击时二次复核 `redirect:'manual'`）；401 统一文案不泄存在性 | POST `/api/v1/session`；GET `/binflow/api/v1/oidc/login`（探测+顶层导航，disabled=404） | AuthContext、BrandLockup、MUI TextField/Paper | error=Alert role=alert；autoComplete；testid login-* |
| /dashboard | DashboardPage.tsx (333) | 概览五卡 | 卡片独立到达；审计行点击/Enter 深链 `/artifacts/<repo>/<path>`；建仓 CTA（仅 admin） | GET `/api/v1/health`、`/api/v1/storage/stats`、`/api/repositories`、`/api/v1/audit?limit=8`、`/api/system/version`（useVersion 模块缓存） | useAsync×5、Skeleton/ErrorCard/EmptyState、MUI Table | 每卡独立四态；403 整卡隐藏（§3.6）；非 admin/readonly 收敛横幅 |
| /profile | ProfilePage.tsx (496) | 改密 + Identity Token 自助签发 + SSH 缺位卡 | 改密表单（旧/新/确认）；Token Dialog 态机 idle→minting→need-password/oidc-stepup→done（一次性明文+curl 样例） | PUT `/api/security/password`（apiText）；POST `/api/security/token`（silent401，携 `step_up_password`） | CopyButton、step-up 口令腿（ADR-0027）、Dialog | 口令 mismatch 行内；OIDC 臂诚实降级引导（不私建链）；`profile-ssh` 缺位登记不伪造 |
| * | NotFoundPage.tsx (36) | 404 | 保留导航壳；mono 回显原始 pathname；回 `/artifacts` | — | — | — |

### 2.2 artifacts 域（跨仓树浏览器，全局最重域）

| 项 | 内容 |
|---|---|
| 路由/文件 | `/artifacts[/:tab[/:key/*]]` · ArtifactsBrowser.tsx (2044) + NodeDetail.tsx (838) + PropertiesTab.tsx (375) + lib.ts (522) + sha256.ts (159) + detailCopy.ts (115) |
| 职责 | 仓库为顶层节点的左树 + 右详情面板 + 当前层 children 表；URL 即状态（页签段+文件是路径末段；B-3.2 首仓库自动选中 replace） |
| 核心交互 | ① 左树：懒展开一层、`role=tree/treeitem`、aria-expanded/level/selected、键盘 ↑↓→←/Enter/Space/Shift+F10/Menu、深链自动展开祖先链+scrollIntoView、选择≠展开；② 树头工具带（T-434）：过滤仓库框、包类型 facet（Popover 复选）、Local/Remote/Virtual 复选组、Sort-by（name/pkg/rclass）、Compacted/Non-Compacted 单选、My Favorites（localStorage `bf-tree-favorites`/`bf-tree-compacted`）；③ children 表：过滤当前层（仅已加载集）+只看文件+「加载更多」PAGE=100、docker 特化列（tags 徽标/digest）、BIG_DIR>2000 警告引导搜索；④ 右键菜单（MUI Menu 壳+原生 button 项）：文件=复制路径/下载/删除、目录=复制/删除/刷新、仓库=复制/收藏/刷新/在管理中打开；⑤ 面包屑（tree-breadcrumb）+ CopyButton；⑥ mkdir（ConfirmDialog 内嵌 input + validateNameSegment 预检）；⑦ 删除（危险确认、404 幂等文案、选中态回跳父目录、403 权限指引链接）；⑧ 下载两形态：直接下载 `<a download>` + 校验下载（tee 流式 sha256 对账，NodeDetail 伴随 Popover 承载 mimeType/checksums/校验结果）；⑨ SetMeUp/Deploy 页头入口；⑩ 树尾常驻回收站入口（canSeeAdmin 门） |
| API（元数据面 `/binflow/api`） | GET `/repositories`（清单，403→合成顶层节点）；GET `/repositories/{key}`（元数据→uploadable/mkdirable/isDocker/isVirtual/remoteBrowseOn）；GET `/storage/{repo}/{path}`（FolderInfo）、`…?list&depth=1`（文件元数据合并）、`…?docker_tags=1`；GET `…?permissions`（admin）；GET `…?stats`（下载四行）；GET `/v1/storage/usage?include=counts&repos=`（仓视图 counts）；GET `/search/checksum?sha256=`、`/search/artifact?name=`（**详情读改道零计数面** getItemForDetail 五级阶梯）；GET `…?properties`（Module ID 探测） |
| API（内容面 `/binflow/{repo}/{path}`） | GET 下载（streamSha256）；PUT 上传（DeployDialog，XHR 进度+X-Checksum-Sha256）；DELETE（目录尾斜杠）；PUT 尾斜杠空体 mkdir（E-15） |
| NodeDetail | 三形态（仓/目录/文件）×三页签（General→有效权限〔admin〕→属性〔节点〕，页签序 B-2.1）；仓视图字段族（Layout/Created 无源恒'—'+登记）；目录直系概要（Count/Size 由父 listing 推导）；FileStatsRows（?stats 下载四行，refreshKey 下载完成联动重读）；ModuleIdRow（build.name/number/timestamp 属性三键→getBuildRun→moduleIdsOf 路径精确匹配→run 深链，全链路空态不伪造）；PermsTab principals chips |
| PropertiesTab | 常显 Property/Value 表单+Add（PUT 单键 merge §11.40）+同名键=替换提示；网格搜索（键/值子串）；行内删除过危险确认；即时校验与服务端同口径（KEY_RE、控制字符、≤64 键、≤32 值/键、≤1024B） |
| 特殊状态 | 树/表逐 (repo,dir) 四态（loading 骨架/forbidden『⃠ 无权限』/error/ok）；virtual 成员并集空态（成员感知文案）；remote 缓存/远端浏览两态注记+remoteDegraded 降级横幅；远端派生行（`remote` 标记+点击回源）；单层渲染上限 TREE_LEVEL_CAP=300；过滤词跨层复位语义（QA-3）；readonly_admin 全写口禁用+注记 |
| 样式 | 自带 browser.css + 复用 `../repositories/tree/tree.css` |

### 2.3 search 域

| 路由/文件 | `/search` · SearchPage.tsx (463) + AqlPanel.tsx (319) + ResultsTable.tsx (356) + aql.ts (197) |
|---|---|
| 职责 | 全局制品/构建检索：基本模式（顶栏驻留查询 `?q=`）+ AQL 编辑器（`?mode=aql`）+ Builds 范围页签（`?scope=builds`）；查询面在 AppShell 顶栏（本页零本地查询态） |
| 核心交互 | 范围/模式 ToggleButtonGroup（URL replace 切换）；AQL=mono textarea ⌘/Ctrl+Enter 执行，**查询文本是唯一事实源**——表头排序/翻页 = 重写 `.sort()/.offset()/.limit()` 尾缀链再重放（splitTail/withTailClause，链序 include→transitive→sort→offset→limit→distinct）；400/408/429 错误分流提示；range 流式语义（total=本页行数、notification 截断通告、页数=前沿+1）；结果网格（ResultsTable 两模式共用）：选择列（批量复制路径 execCommand 降级）+快滤（name/dir/repo 三域）+列选器（6 列，size/sha256 defaultHidden）+基本模式客户端页窗/AQL footer Pager |
| API | GET `/api/search/artifact?name=`（基本，SR-01 全量无分页）；POST `/api/search/aql`（text/plain body；builds 范围 = 前端合成 `builds.find({"$or":[name,number $match]}).include(…).sort($desc started).limit(100)`） |
| 语义副行 | semanticOf：maven GAV / maven-metadata 推导 + property 投影 + virtual 成员 + type；formatStamp 双 locale 形态（zh `dd-MM-yy HH:mm:ss +ZZZZ` / en 12h）；treeUrl 深链 K67-3 规范形 |

### 2.4 builds 域【盘上现状 = T-512 已落库；会话开始时为未提交在途，审计期间已提交（commit 74174a07）——按盘上现状审计，见 §6】

| 路由/文件 | `/builds[/:name[/:number]]` · BuildsPage.tsx (545) + api.ts (237) |
|---|---|
| 职责 | build 域只读查询面：名单（构建名+最新启动）→号单（run 号倒序）→run 详情（info/statuses/modules/artifacts/dependencies/事件时间线）；promote/发布仅 API 注记（admin-note），不伪造表单 |
| 核心交互 | 三视图一组件（useParams 分派）；行/链接深链（`?started=` 消歧同名同号）；CopyButton（Module ID/sha256/CI URL）；403 与 404 分立承载（403=读门拒绝即答案 build-denied；号单 404=空可见集与不存在同形零泄漏） |
| API | GET `/api/build`、`/api/build/{name}`、`/api/build/{name}/{number}?started=&buildRepo=`；GET `/api/v1/audit?repo=<buildRepo>&limit=1000` + `?action=build.promote&limit=1000`（run 时间线：两查询去重+四元消歧，admin 面 403 整段隐藏） |
| 细节 | `ACTION_LABEL` 模块级 t()（locale reload 重求值）；record-only 制品行（path 空）如实无链接；dependencies/artifacts 表 module 展平 |

### 2.5 bundles 域

| 路由/文件 | `/bundles[/:name[/:version]]` · BundlesPage.tsx (387) + api.ts (95) |
|---|---|
| 职责 | Release Bundle 只读查询：名单→版本单（state 徽章 COMPLETE/INPROGRESS）→描述符（状态/创建/签名摘要/HEAD 校验和/制品清单）；创建走 API 注记 |
| 核心交互 | 三视图一组件；StateBadge 语义色；清单大表 useClientPager；pending 行（sha256=''）如实'—'；403（版本/描述符面）与 404 分立 |
| API | GET `/api/release/bundles[/{name}[/{version}]]`；HEAD 同址（X-Checksum-Sha256 探针，缺头'—'） |

### 2.6 repositories 域（管理面）

| 路由 | 文件（行数） | 职责/交互 | API | 备注 |
|---|---|---|---|---|
| /admin/repositories/{rclass} | RepositoriesPage.tsx (907) | 三 Tab 子路由列表：7 列（key 链接+拷贝/包型 Chip+PkgIcon/Replications〔local+remote Tab〕/上游或成员〔details 弹层〕/已用〔批量注水+tooltip 文件数+失败行内重试〕/描述/操作〔SetMeUp·Deploy·删除〕）；列头排序（key/pkg 三态循环）；key 子串过滤；客户端页窗；列选器；刷新钮；建仓下拉三预选 | GET `/repositories?type=`；GET `/v1/storage/usage?include=counts`（**单请求注水**）；GET `/v1/replications`；POST `/v1/replications/{id}/run`（Replicate Now，toast 带「查看任务」action 跳复制页） | usage 门控=列表 ok 后才发；删除走 useRepoDelete |
| /admin/repositories/{rclass}/new、/:key/edit | RepositoryFormPage.tsx (1687) | **三段步进**（Basic\|Advanced\|Replications，MUI Tabs，第三步仅编辑×local）；进页弹包类型网格 Dialog（924px，13 型=五核心+addons 动态，档位徽章，无前端 license 禁用门）；编辑态 rclass/packageType 锁定；**dirty-gating**（stableFormString 规范形 deep-equal 基线，零变更 Save 不可达）；右栏实时摘要；取消/创建\|保存两钮（Q9 无重置）；Test 连接（编辑态，草稿臂=与基线 diff，密码改 url/user 即匿名探测）；字段域：remote（url/user/pass 永不回显/4 TTL/hardFail/listRemoteFolderItems 仅批1型/allowPrivateUpstream 警示）、virtual（成员复选+↑↓排序+默认部署仓联动清空）、local（maven 策略族/governance quota+patterns/priorityResolution）、conan forceConanAuthentication、deb/rpm/helm 策略键（policyFields.ts 字段册 225 行）；**预留位字段族**（repoLayout/Environments/notes/blackedOut/archiveBrowsing/maxUniqueSnapshots/SuppressPOM）恒禁用零提交 | GET `/repositories`（候选）；GET `/repositories/{key}`；PUT/POST `/repositories/{key}`（POST=全量替换）；POST `/repositories/{key}/test`；GET `/v1/addons` | formValid() 手写门控见 §4 |
| /admin/repositories/:key | RepoDetailPage.tsx (690) | 三 Tab（概要/配置/Replications）：概要=接入命令块（clientCommands，pre tabIndex=0）+统计水位条（LinearProgress ≥80% warn ≥100% error）+remote/virtual 特化卡+危险区删仓；配置=QuotaEditor 行内编辑（保存前重取详情重组全量 body）+字段只读+编辑器深链；Replications=本仓配置摘要表+`?section=replications` 深链+全局复制页指针 | GET `/repositories/{key}`；GET `/v1/storage/usage/{key}`；GET `/v1/replications`；POST `/repositories/{key}` | CanManageRepo 门：m-holder（普通 user GET 通过）可编辑配置；删除仅 admin |
| （表单第三步） | ReplicationsSection.tsx (770) | 复制配置内嵌节（无 modal）：列表（启停 Switch/名称/目标/凭据/调度 cron+next/节流批量）+内嵌新建/编辑表单+Test（草稿/已存双臂）+删除（输入 name 强确认）；**编辑=删除+重建**（REST 无字段级 PUT，warn-box 明示未决任务级联清空）；预留位组（eventReplication/pathPrefix/sync 三开关）恒禁用 | GET/POST `/v1/replications`；DELETE `/v1/replications/{name}`；PUT `/v1/replications/{id}`（启停）；POST `/v1/replications/test`、`/test/{id}` | cron_exp 真字段（T-462） |
| 辅助 | commands.ts (403)：接入命令三侧（Configure/Deploy/Resolve）+clientCommands+CLIENT_PKG_META+占位凭据；formCopy.ts (117) 文案常量；policyFields.ts (225) 字段册；RepoDeleteConfirm.tsx (103)：useRepoDelete（输入 key+deleteContent 复选；非空仓 400 原因带回+预勾选两段流） | | | |

### 2.7 security 域（9 页 + 支撑 5 文件）

| 路由 | 文件（行数） | 职责/交互 | API |
|---|---|---|---|
| /admin/security/users | UsersPage.tsx (370) | 列表：7 列（name 链接+拷贝/email/groups 计数+Chip 明细/role 三值 badge/status/lastLogin/actions）；列头排序 6 键；客户端页窗；列选器（非 admin 视图剔除操作列同步清洗）；行键盘导航；删除（自删/内置 admin 预禁用+输入用户名确认 widgets.useUserDelete） | GET `/security/users`（E2 加宽单请求） |
| /admin/security/users/new | UserCreatePage.tsx (415) | 整页路由表单：用户名（blur 触发校验 validateUserName）/email/角色三值 select/enabled/口令+Retype（mismatch）/组穿梭 TransferBox；页脚 Cancel\|Reset(初始禁置)\|Save；能力位三旗（Can Update Profile/Disable UI/Disable Internal Password）**预留位恒禁用零提交** | PUT `/security/users/{name}`（create-or-replace，admin+adminRole 一致对） |
| /admin/security/users/:name | UserDetailPage.tsx (435) | 编辑器五区：设置（email/角色/enabled）/口令重置（无需旧口令）/相关组穿梭/权限矩阵只读（grantsOfUser 直接+经组并集）/账户信息+危险区删除；dirty 全字段比较；部分更新指针语义（enabled 恒携带） | GET `/security/users/{name}`；POST `/security/users/{name}`；GET `/security/groups`、`/v1/permissions`；DELETE |
| /admin/security/groups | GroupsPage.tsx (441) | 列表（组名+描述副行/权限数+manage 徽章/成员数〔E2 users.groups 客户端双索引推导〕/操作）；409 删除冲突面板（parseReferencedTargets 尾部锚定解析 target 名→链接直达权限编辑器解除） | 三请求并行：GET `/security/groups` + `/security/users` + `/v1/permissions`；DELETE `/security/groups/{name}` |
| /groups/new、/:name/edit | GroupFormPage.tsx (343) | 组名（编辑锁定）/描述/成员穿梭（候选=E2 全用户，选区=E5 按需单组读）；成员落盘=逐用户 updateUser（幂等护栏防重复入组；失败名单行内）；组权限矩阵只读；Cancel\|Reset\|Save | PUT `/security/groups/{name}`；GET `/security/groups/{name}?includeUsers=true`；POST `/security/users/{m}` 逐个 |
| /admin/security/permissions | PermissionsPage.tsx (241) | 列表 5 列计数（name+manage 徽章/仓库数/patterns ±/用户数/组数）；排序 5 键；页窗；m-holder 走 E6 filter=manage（403=覆盖集空友好空态） | GET `/v1/permissions` 或 `?filter=manage` |
| /permissions/new、/:name | PermissionEditorPage.tsx (1189) | 编辑器：目标信息（name 编辑锁定+repos chips）+**两步资源 Dialog**（①选仓穿梭含三通配桶 ANY LOCAL/ANY REMOTE/ANY DISTRIBUTION 与具体仓混选；m-holder 403 时降级手动录入；②patterns chip 逐行编辑）+**模式测试器**（evaluatePath 本地求值零端点，逐条命中明细+exclude 优先终判，aria-live）+用户/组两个五动作矩阵（read/annotate/write/delete/manage，列头 tooltip 7.161 对位；wire 双层 normalizePermActions/wireActions）+保存 diff 确认（buildTargetDiff ±行）+危险区删除 | POST `/v1/permissions`（create-or-replace）；DELETE `/v1/permissions/{name}`；GET `/repositories`、`/security/users`、`/security/groups`（m-holder 全 403→手动录入） |
| /admin/security/tokens | TokensPage.tsx (650) | 签发 Dialog（有效期预设 6 档+「永不过期」仅 admin；代人签发 username 仅 admin；scope 固定 api:* 只读说明；step-up 双腿：口令内联/OIDC 引导）+**会话台账**（内存态刷新即空——服务端无清单端点 §9-R6，如实注记）+行内吊销（danger 确认）+按 token_id 吊销历史令牌；指纹=sha256 前 8 hex（crypto.subtle，非安全上下文空串） | POST `/security/token`（E-17 JSON+silent401）；POST `/security/token/revoke`（E-18 **form-urlencoded** token_id） |
| /admin/security/auth/:proto | authconfig/AuthConfigPage.tsx (675) + sections.ts | 三协议 Tab=子路由；**数据驱动表单**（FieldDef 五 kind：text/textarea/number/list/check/secret/locked——secret 哨兵『20 星』语义：留空保持=提交剔除，绝不回传）；四陷阱处理（SAML noAutoUserCreation 取反、哨兵、SAML 空对象引导、OAuth persistUsers 摊平）；dirty=JSON.stringify 比较；测试连接双形态（当前表单候选/已存配置空体；LDAP 附 testUsername/testPassword 两半齐备门）；SAML SP 加密证书卡（下载 PEM Blob+指纹 PEM→DER sha256 复用 artifacts/Sha256+重生成 danger 确认） | GET/PUT `/v1/admin/security/{ldap|oauth}`、`/v1/admin/security/saml/config`；POST `…/test`（400 判定体从 raw 解回）；GET `…/saml/config/key/public`；PUT `…/key/public/regenerate` |
| 支撑 | api.ts (394)：users/groups/permissions REST 族+wire 动作词归一+grantsOf\* 客户端授权折叠+validateGroupName/UserName；TransferBox.tsx (107) 双列穿梭；widgets.tsx (275) SortTh/StatusLabel/PermSummaryTable/useTableSort/useUserDelete；pathmatch.ts (131)+fixtures：Ant 通配求值（与服务端同源）；targetdiff.ts (83)：快照比较+diff 行 | | |

### 2.8 governance 域

| 路由 | 文件（行数） | 职责/交互 | API |
|---|---|---|---|
| /admin/governance/audit | AuditPage.tsx (512) | 过滤器五件（repo 精确+datalist 建议/actor 精确/action 54 词表 select/since-until datetime-local→RFC3339/path 客户端子串）350ms 防抖；**keyset 游标→页码页窗映射**（useAuditPages：游标链 chainRef、页数=前沿+1 逐页揭示、epoch 变化回页 1）；6 列列选器+刷新；detail JSON `<details>` 折叠 | GET `/api/v1/audit`（repo/actor/action 精确+since/until+limit 1..1000+nextCursor；**无 path 参数**） |
| /admin/governance/quotas | QuotasPage.tsx (284) | 每仓一行（key 链接/类型/已用/配额/水位条/操作）；行内编辑（编辑上限→TextField+保存/取消，仅 local）；virtual 恒'—' | GET `/repositories`；逐仓 GET `/v1/storage/usage/{key}`；POST `/repositories/{key}`（buildLocalQuotaBody 全量重组） |
| /admin/governance/replication | ReplicationPage.tsx (506) | 全局封锁双开关卡（blockPush/blockPull，失败回读不乐观更新）+复制目标表（状态灯/调度列 join 配置面/计数列）+最近事件表（状态徽章/sha256 截断拷贝/重试）；**10s 轮询**（stale 保留不清屏）；404/501 降级提示 | GET `/v1/replication/status`（轮询）；GET `/v1/replications`（cron join）；GET/POST `/v1/system/replications`（封锁） |
| /admin/governance/trash | TrashPage.tsx (564) | trashcan 槽门控卡（pro 锁定态+License 深链）；浏览（面包屑下钻+行选中详情：属性五元组 trash.\*+随行属性）；动作：恢复（ConfirmDialog+可选 to 目的地覆盖）/单条永久清除/清空整罐（输入 EMPTY 强确认）；清剿摘要就地呈现；404=can 未落库懒落库空态 | GET `/v1/addons`（trashSlot）；GET `/storage/auto-trashcan`+`?list&depth=1`（listChildren 复用）；GET `?properties`；lib/trash.ts restore/clean/empty POST 族 |
| /admin/monitoring/gc | GCPage.tsx (616) + MigrationPanel.tsx (355) | 存储概况卡；**定时维护三槽表**（gc/cleanup 两族 cron 表达式行内编辑+保存/清除+下次/上次+Run Now——gc 槽滚动到危险区）；GC 危险区（dry-run 默认→apply 门控〔grace 变更即过期〕+输入 YES+409 互斥面板）；MigrationPanel（进度条=(migrated+failed)/total 口注+启动迁移 danger+输入 YES，5s 轮询，501 降级） | GET `/v1/storage/stats`；GET/PUT `/v1/system/maintenance`；POST `/v1/system/cleanup`；POST `/v1/system/gc`；GET/POST `/v1/storage/migration[/start]` |
| /admin/monitoring/backup | BackupPage.tsx (504) | ①定时备份 CRUD：列表（key/cron/next/启用/上次/路径）+New Backup 表单（Backup Key/Cron/Next datetime-local/Server Path/enabled；Artifactory 其余字段缺位注记）+删除（输入 key）；②导入/导出 CLI 命令块（只读+拷贝，/api/export/** 有意 404） | GET/PUT/DELETE `/v1/system/backups` |

### 2.9 monitoring 域

| 路由 | 文件（行数） | 职责/交互 | API |
|---|---|---|---|
| /admin/monitoring/storage | StorageSummaryPage.tsx (315) | 刷新行（时间戳+刷新）+汇总卡（stats 口径）+逐仓表（TOTAL 首行、占比、配额；Files/Folders 列契约冻结不渲染）；**逐仓 usage 串行拉取+进度提示**（>50 仓快照注记） | GET `/v1/storage/stats`、`/repositories`、逐仓 `/v1/storage/usage/{key}` |
| /admin/monitoring/status | ServiceStatusPage.tsx (237) | 总体卡（Online 徽标/版本/实例 URL=window.location.origin/单节点注记/Uptime 缺位登记）+子系统表（storage/metadata/registry）+调度服务台账表（只读投影，cron 编辑归维护/备份页）+刷新 | GET `/v1/health`、`/v1/system/schedules`、`/api/system/version` |
| /admin/monitoring/logs | SystemLogsPage.tsx (413) | 日志查看器：**双源**（主源=进程日志 GET limit 1..1000+服务端 filter 子串+download=1 附件臂；404→会话内粘性降级审计跟踪 newest-first+客户端过滤+Blob 导出；403 不降级两源同门）；**7s 尾随倒计时+Pause/立即刷新**；窗口档 100/200/500/1000；400ms 去抖；环形遥测（held/truncated 注记） | GET `/v1/system/logs?limit=&filter=[&download=1]`；降级 GET `/v1/audit` |
| /admin/monitoring/system-info | SystemInfoPage.tsx (127) | 只读实例信息（版本三值/发行行静态/当前用户）+健康摘要卡（403 L3 隐藏）；Server Name/Base URL 等契约冻结注记；页根锚=settings（路由迁移不改锚） | GET `/api/system/version`、`/v1/health` |

### 2.10 admin 域（常规组）

| 路由 | 文件（行数） | 职责/交互 | API |
|---|---|---|---|
| /admin/general/license | admin/LicenseAddonsPage.tsx (418) | License 状态卡（档位徽章三色/licensee/有效期倒计时负值文案/community 地板注记）+admin 装/卸（文本域贴 .lic 全文；卸载输入 UNINSTALL 强确认）+Addons 矩阵表（id/名称+PkgIcon/kind 徽章/最低档位/三态：已解锁·锁定需N档·配置熔断） | GET `/v1/license`、GET `/v1/addons`；POST `/api/system/license`（rawBody 纯文本）、DELETE（lib/addons.ts） |
| /admin/general/webhooks | webhooks/ 三文件（402+482+255） | 列表（启停 Switch/key/事件域+类型 Chip〔wired 绿/dormant 灰，前 3+N〕/URL/操作四图标钮：详情抽屉·试发·编辑·删除）+只读注记+槽锁定注记（读面不过 license 门）+行内试发结果 Alert；**SubscriptionDialog**（md 居中 Dialog：key 创建后锁/描述/enabled 默认 false/debug/事件域 select+域内 66 型复选墙〔wired/dormant 标注〕/criteria 表单〔anyLocal/anyRemote/repoKeys/include/exclude，仅 artifact 三域托管，其余 passthrough〕/handler URL+secret 三态〔留空保持·明文轮换·勾选清除=擦空串，哨兵绝不回传〕+use_secret_for_signing/发送测试吃草稿体）；**SubscriptionDrawer**（右 480px：详情 chips+criteria JSON pre+投递记录表〔时间/状态/事件/耗时/重试，行点击展开 payload 快照 pre+拷贝〕，失败必录语义注记） | `/binflow/event/api/v1/subscriptions` GET/POST/PUT/DELETE、`/test`（吃完整订阅体）、`/troubleshooting?subscription=`（自有 eventJSON 信封，不在 lib/api 的 API_ROOT 下）；GET `/v1/addons`（槽提示） |

---

## 3. 全局能力清单（跨页面，重写须逐一承接）

| # | 能力 | 载体 | 挂载页面/入口 | 要点 |
|---|---|---|---|---|
| 1 | 登录流 | LoginPage + app/AuthContext.tsx (104) | /login；全局 | GET `/v1/session` 探活（silent401）；login/logout POST/DELETE；**全局 401 监听**（已认证态任一请求 401=会话死→toast+return 重登）；刻意无保活 |
| 2 | OIDC SSO 登录 | LoginPage.probeOIDCLogin | /login | `redirect:'manual'` 探测三态；点击复核后 `window.location.assign` |
| 3 | step-up（口令腿） | SetMeUpDialog/ProfilePage/TokensPage 各自 MintState | 三载体同引擎 | 401 `step_up_required`→内联口令框；`step_up_invalid`→内联原文；silent401 |
| 4 | step-up（OIDC 重认证+续铸） | lib/stepUpGrant.ts (179) + AppShell + SetMeUpDialog | 全局 | fragment `#step_up_grant=` 挂载期消费即抹除；pending-mint sessionStorage `bf.pendingMint`；单次消费；useSyncExternalStore；AppShell resume 重开抽屉 |
| 5 | Profile | ProfilePage | /profile + 用户菜单「编辑档案」 | 见 §2.1 |
| 6 | 命令入口（接入命令） | pages/repositories/commands.ts | 仓库详情页/树页命令块/SetMeUp 三侧 | P3 纪律：与 docs/user 同源，UI 不发明命令；占位凭据→铸币回填 |
| 7 | Set Me Up | components/SetMeUpDialog.tsx (791) | 树页页头/仓列表行/仓库详情头/用户菜单 quick-set-me-up | 右抽屉 clamp(480,50vw,800)；步 0 包型药丸（实例实有型并集）；三 Tab（Configure/Deploy/Resolve，手写 tablist 方向键）；Token 铸币区（24h 自铸） |
| 8 | Deploy（浏览器上传） | components/DeployDialog.tsx (612) | 树页页头 tree-deploy（唯一入口）/仓列表行/仓库详情头 | 候选=local×{generic,maven}；队列泵（行串行 hash→PUT，XHR 进度+abort）；maven GAV 表单（lib/maven.ts 40 行 layout 生成+预检）；编码回显；409/403/413 错误语义分卡；双 403 降级按 generic 直传（如实标注） |
| 9 | 帮助/About | AppShell（help Menu + About Dialog） | 顶栏 ？ + 侧栏脚注 nav-about | 四项：Documentation（/binflow/docs/）/Online Training 禁用占位/Release Notes/About（useVersion）；testid help-\*/about-\* |
| 10 | 主题切换 | app/ThemeContext.tsx + app/MuiProvider.tsx | 顶栏钮+用户菜单 | localStorage `binflow-console-theme`；MUI palette 对齐 tokens.css；深浅跟随 |
| 11 | 语言切换 | AppShell 侧栏脚 ToggleButtonGroup | nav-locale | setLocale 持久化+**整页 reload**（模块级 t() 求值点重求值）；中文 endonym 豁免硬编码闸 |
| 12 | 面包屑 | AppShell.adminCrumbs | 管理模式顶栏 | 五分组层级（仓库/security 段名映射/proto→Tab 名）；应用模式=页面标题（appTitle） |
| 13 | 顶栏搜索 | AppShell（topbar-search） | 应用模式顶栏 | Enter→`/search?q=`（/search 上 replace）；驻留回显（URL q 事实源+draft 草稿层）；最近词下拉（localStorage `binflow-console-recent-searches` 8 条、空历史恒渲染占位、↑↓/两段 Esc/一键清除）；scope=builds 沿用 |
| 14 | 管理资源过滤 | AppShell（admin-filter） | 管理模式顶栏 | 340px 框客户端子串过滤管理侧栏条目；整组空注记 admin-filter-empty；⌘K 指向当前框 |
| 15 | 全局快捷键 | AppShell useEffect | 全局 | ⌘K/Ctrl+K（focus+select）、`/`（非输入框时）；modal 开时让位 |
| 16 | 分页 | components/Pager.tsx (180) | 14 个消费面 | MUI Pagination+页大小 select；档位 [20,50,100,200,1000]（100=家族页窗）；`useClientPager`（epoch 派生回页 1）；流式面 lastUnknown（页数=前沿+1） |
| 17 | 危险确认 | components/ConfirmDialog.tsx (166) | 删仓/删用户/删组/GC apply/迁移启动/清空回收站/卸载 license/删备份/删复制/删 webhook/吊销 token/删属性/删 target… | Promise 化 useConfirm；confirmDisabled 闭包缝（body 输入事件驱动重求值）；danger 红边；打开即聚焦取消（回调 ref+微任务）；文档级 Esc 兜底 |
| 18 | Toast | app/ToastContext.tsx (123) | 全局 | success/error+action 按钮（label+onClick，如「查看任务」） |
| 19 | 复制 | components/CopyButton.tsx (53) | 数十处（key/路径/sha/命令/token/URL/指纹） | clipboard API+execCommand 降级；成功态 |
| 20 | 四态三件套 | Skeleton.tsx (29)/ErrorCard.tsx (68)/EmptyState.tsx (83) | 全域 | useAsync {loading,ok,error,**forbidden**}；403 三层收敛律：L2 无权限卡、L3 整卡隐藏、L4 写入口不渲染；readonly_admin=禁用+注记（服务端 403 兜底） |
| 21 | 列选器 | lib/columnPrefs.ts (112) | 5 页：search/repos/users/groups/audit | localStorage per-page（`binflow-console-cols-<page>`）；读回清洗（未知 id 剔除/全隐回落）；defaultHidden（search size/sha256）；至少一列守卫 |
| 22 | 包型图标 | components/PkgIcon.tsx (113) + assets/pkg-icons | 树/列表/网格/License | mono/brand 两版 30 枚；BrandLogo/BrandLockup（app-nav-brand/login/About） |
| 23 | 模式切换 | AppShell 侧栏底 nav-mode-switch | 应用⇄管理 | canSeeAdmin=admin∪readonly_admin；APP_HOME=/artifacts ADMIN_HOME=/admin/repositories/local |
| 24 | 快速动作 | AppShell 用户菜单 | session-toggle | 仅全量 admin：Set Me Up/新建 Local·Remote·Virtual 仓（`/admin/repositories/new?rclass=`）/新建用户·组·权限；readonly 不见 |
| 25 | 版本 | lib/useVersion.ts (40) | About/侧栏脚/Dashboard/ServiceStatus/SystemInfo | 模块级缓存，GET `/api/system/version`（开放端点） |
| 26 | 流式 SHA-256 | pages/artifacts/sha256.ts | Deploy 上传对账/下载对账/SAML 指纹 | 零依赖 FIPS 转录（4MB 块，RSS 有界——刻意不用 crypto.subtle：1GB 整块 + 非安全上下文两因） |

---

## 4. 表单面盘点（全部手写验证，无 RHF/Zod/schema 库——重写迁移 RHF+Zod 时的规则事实源）

| 表单 | 文件 | 字段域 | 验证方式（现行） |
|---|---|---|---|
| 建仓/编辑仓库（三段步进） | RepositoryFormPage | key/url/user/pass/4×TTL/成员/patterns/quota/maven 族/policy 键/cron 等 30+ 字段 | `formValid()` 手写（必填+正则）；`validateRepoKey`/`validateUpstreamURL`（lib/repos 前端预检，服务端终裁）；数值字段 `isNonNegInt`；**dirty-gating**=stableFormString 键排序 deep-equal；服务端 400 文案行内原文 |
| remote Test | RepositoryFormPage.doTest | url/user/pass 草稿 | buildTestBody 与基线 diff（密码填→整组草稿；url/user 改→匿名探测；零改动→无 body 探存量） |
| 复制配置 | ReplicationsSection | name/url/repo/user/pass/带宽/批量/cron/enabled | `gate()`+`validateReplicationName`/`validateReplicationTargetURL`（lib/replications 256 行）；编辑=删除+重建语义注记 |
| 备份 | BackupPage | key/cron/next/path/enabled | `validateBackupKey`/`validateExportPath`/`localInputToRFC3339`（lib/governance）；400 点名原因行内 |
| 用户创建 | UserCreatePage | name/email/role/enabled/pass+retype/groups | `validateUserName`；blur touched（『请填写此字段』）；mismatch 置灰；canSubmit 复合 |
| 用户编辑 | UserDetailPage | email/role/enabled/pass/groups | dirty 逐字段；口令 mismatch；部分更新指针语义（absent=保持） |
| 组 | GroupFormPage | name/desc/成员 | `validateGroupName`；dirty=E5 偏差；成员逐用户落盘 |
| 权限 target | PermissionEditorPage | name/repos/patterns/矩阵 | nameValid+repos≥1+dirty（sameSnapshot 集合语义）；矩阵纯勾选；**diff 确认**（targetdiff） |
| Webhook 订阅 | SubscriptionDialog | key/enabled/domain/types/criteria×5/url/secret/sign/debug | KEY_RE+URL regex+`criteriaEmptyScope`（空范围警示）；secret 三态提交规则 |
| 认证配置×3 | AuthConfigPage+sections.ts | 数据驱动 FieldDef（~40 字段/段） | kind 级渲染；secret 哨兵剔除；dirty=JSON.stringify；SAML 取反语义 |
| Token 生成×2 | ProfilePage/TokensPage | ttl(+subject) | 预设档枚举；永不过期 admin 门 |
| GC/迁移 | GCPage/MigrationPanel | graceHours/YES 确认 | parseGrace（0~100 年整数）；confirmDisabled 输入匹配 |
| mkdir/属性 | ArtifactsBrowser/PropertiesTab | 目录名/键值 | `validateNameSegment`/KEY_RE/值集规则（与服务端同口径） |
| 配额行内 | RepoDetailPage.QuotaEditor/QuotasPage | quotaBytes | `^\d+$`+MAX_SAFE_INTEGER |
| Deploy | DeployDialog | 目标仓/模式/路径/GAV×5 | mavenTarget（lib/maven）layout 生成+error 预检；mavenReady 零坏请求 |
| 改密 | ProfilePage | 旧/新/确认 | mismatch 行内；服务端纯文本文案原样 |
| 登录 | LoginPage | user/pass | canSubmit 非空 |
| 审计时间窗 | AuditPage | since/until | localInputToRFC3339（无效仅提示不参与门控） |

**模式共性**：错误呈现一律「HTTP 状态+服务端 message 原文」行内（mono、`lang="en"`）；危险动作一律 ConfirmDialog+输入确认（repo key/用户名/组配置名/备份 key/YES/EMPTY/UNINSTALL）；secret 一律 write-only（哨兵语义三处：authconfig 20 星/webhook `********`/repo 密码空串剔除）。

---

## 5. 表格面盘点（现役 20+ 张表，全 MUI Table，无表格库/虚拟化）

| 表格 | 页面 | 数据量级假设 | 分页 | 过滤 | 排序 | 列选 |
|---|---|---|---|---|---|---|
| 审计卡 8 行 | Dashboard | 8 | 无 | 无 | 无 | 无 |
| children 表 | ArtifactsBrowser | 单目录可 2000+（BIG_DIR 警告） | 「加载更多」100/页（客户端） | 当前层子串+只看文件（客户端） | 服务端序（目录前+名序） | 无（docker 列特化） |
| 搜索结果 | Search/ResultsTable | basic=服务端全量单响应；AQL=页窗 | basic=useClientPager；AQL=.offset/.limit 重写+Pager | 快滤（客户端）+列选 | AQL=表头 .sort() 注入（aria-sort） | 6 列+defaultHidden |
| Builds 三表 | BuildsPage | 名单/号单=全量；清单=module 全展开 | 无 | 无 | 服务端（started 倒序） | 无 |
| bundle 清单 | BundlesPage | 清单可大 | useClientPager | 无 | wire 序 | 无 |
| 仓库列表 | RepositoriesPage | 实例仓规模（几十~百） | useClientPager | Tab=服务端 `?type=`；key=客户端子串 | 列头 2 键三态（aria-sort） | 7 列 |
| 用户/组/权限 | security 三列表 | 账号规模（全量在端） | useClientPager | 无 | 列头 6/3/5 键 | users 7/groups 4/perms 无 |
| Token 台账 | TokensPage | 本会话签发数 | useClientPager | 无 | 无 | 无 |
| 审计 | AuditPage | 无界（append-only） | **keyset 游标→页码**（limit 档 20..1000；末页未知） | 服务端精确×4+客户端 path | 无（时间倒序） | 6 列 |
| 复制目标/事件 | ReplicationPage | 近期 N 条 | 无（10s 轮询） | 无 | 服务端序 | 无 |
| 配额/存储 | Quotas/StorageSummary | 仓数 | 无 | 无 | 无 | 无 |
| GC cron/备份 | GCPage/BackupPage | 3 槽/配置数 | 无 | 无 | 无 | 无 |
| addons 矩阵/license | LicenseAddonsPage | 注册表槽位 | 无 | 无 | 无 | 无 |
| webhook 列表/记录 | Webhooks 两处 | 订阅数/记录环 20 | 无 | 无 | 无 | 无 |
| 回收站 | TrashPage | 单层目录 | 无 | 无 | listChildren 序 | 无 |
| Deploy 队列 | DeployDialog | 选择文件数 | 无 | 无 | 无 | 无 |

**重写注意**：三处手写上限兜底现存性能假设——树单层渲染上限 300（TREE_LEVEL_CAP）、children 客户端切片 100、>2000 警告引导搜索；AQL basic 全量单响应无服务端分页；审计 keyset 无总量。AG Grid/TanStack Virtual 替换时的边界即这三处+usage 批量注水（N 仓 1 请求）与 StorageSummary 串行逐仓拉取（>50 仓快照注记）。

---

## 6. Builds/Bundles 在途标注

会话开始时的 git 快照显示 T-512 Builds 相关面为未提交在途：`?? web/src/pages/builds/`、`?? web/src/i18n/locales/en/builds.ts`、`?? web/src/i18n/manifests/zh/builds.json`，以及 M `main.tsx`/`AppShell.tsx`/`NavIcons.tsx`/`NodeDetail.tsx`/`SearchPage.tsx`/`AqlPanel.tsx`/`aql.ts`/`i18n/index.ts` 等。**审计期间该批工作已被提交**（`74174a07 feat(T-512): the Builds page opens — list, detail, modules; the search scope tab and the Module ID field lift their long-registered absences`；Bundles 早已在 `003907f4 feat(T-514)` 落库）。本报告全部按盘上现状（=已提交的 T-512/T-514 完整实现）审计，§2.4/§2.5 即最终形态；当前工作树对 web/ 无未提交改动。

---

## 7. 重写必须保全的行为契约（审计提炼的高危清单）

1. **API 信封三形态**：errors[] JSON/纯文本/OAuth 形（lib/api.ts toApiError）+ webhook 族独立根 `/binflow/event/api/v1`（自带 eventJSON）+ 内容面 `/binflow/{repo}/{path}`（复刻信封）+ rawBody（license/AQL text/plain）+ formBody（token revoke）。
2. **403 四态收敛律**（useAsync forbidden 分离）与 readonly_admin/m-holder/admin 三角色姿态（禁用+注记 vs 隐藏 vs 手动录入降级）。
3. **URL 即状态**的既深链面：树页签段+文件末段（legacy ?focus= 一次性折入）、builds/bundles 三视图、search `?q/mode/scope`、repo 表单 `?section=`、`?started=` 消歧——书签不猝死是既有测试面。
4. 一次性明文令牌态机（三载体同源）、step-up 双腿与 grant 生命周期（fragment 抹除/单次消费/sessionStorage pending）。
5. 全量替换语义的保存链（repo 编辑/QuotaEditor/buildLocalQuotaBody 保全全部字段、组员逐用户落盘幂等）。
6. 零端点不伪造纪律：大量「缺位登记」注记（SSH Keys/Uptime/Remote 状态/Files 计数列/7.161 三选择器/promote·bundle 创建表单/预留位字段族）——文案本身是产品语义，重写需逐条带走或重新裁定。

# 三、API 契约面审计报告

# BinFlow Web 控制台重写——API 契约面审计报告

审计基线：`develop` 分支工作树（M17 进行中）；只读。核心证据文件：`/Users/lzw/dev-center/internal/httpapi/router.go`（1902 行）、`server.go`、`middleware.go`、`session.go`、`stepup.go`、`webhooks.go`、`webhook_outbox.go`、`envelope.go`、`auth_methods.go`、`audit.go`；前端消费面 `web/src/lib/*.ts` + `web/src/pages/**/{api,lib}.ts`；契约 `fern/openapi/binflow.json`（OpenAPI 3.1.0，**158 operationId / 112 paths / 20 tags**，jq 实测）。

---

## 1. 入口拓扑与中间件链（前端必须理解的骨架）

**顶层路由**（`server.go:528 route()`，手写 switch 绕开 ServeMux cleanPath 重定向）：

| 路径 | 归属 | 说明 |
|---|---|---|
| `/healthz` / `/readyz` | probe | 无认证，baseChain only；readyz 含 metadata ping + 存储可写探针 |
| `/metrics` | Prometheus | 根级抓取点；`metrics.require_auth` 可选门 |
| `/v2/**` | docker 面（ADR-0010 根级例外） | 整族交给 docker adapter，**不吃 /binflow 前缀**；含 `/v2/token` |
| `/binflow/**` | 产品面 | 下分 `ui` / `assets` / `docs` / `api` / `event` / 内容路径 |
| 其余 | 404 | E-26①：无根镜像，message 带 /binflow 前缀提示 |

**/binflow 内分支**（`router.go:108 dispatch`）：

| 段 | 消费者 | 认证 |
|---|---|---|
| `/binflow`、`/binflow/` | Console handler，301 → `/binflow/ui/` | 无 |
| `/binflow/ui/**`、`/binflow/assets/**` | SPA shell + 指纹资产（go:embed） | 无 |
| `/binflow/docs/**` | 内嵌帮助站（ADR-0011） | **匿名可读**（anonymous_access=false 也服务） |
| `/binflow/api/**` | 管理面 REST（本报告 §2 主体） | 见路由表 |
| `/binflow/event/api/v1/**` | webhook 订阅面（官方 Event 命名空间挂前缀） | system:read/write |
| `/binflow/v2` | **404**（ADR-0010 clause 2 禁双挂载） | — |
| `/binflow/{repoKey}/{path}` | 内容面 → 按 repo 行 packageType 派发 adapter | 内容面 r/w/d |

**中间件链**（顺序固定，`router.go:51 rootHandler` / `baseChain`）：
`requestID → accessLog → [metrics] → recoverPanic → CORS → authenticate → csrfGuard → deployScopeGuard → dispatch（按路由挂 authorize 门 → handler）`

- 手写 dispatcher over `EscapedPath`，无隐式 redirect —— **FE 深链/编码行为可依赖字面拼写直达**。
- 认证四臂：Basic / Bearer(API token) / docker token / `binflow_session` cookie（ADR-0014 第三臂）。presented-but-rejected **永不降级匿名** → 硬 401（豁免：`POST /api/v1/session`、`GET /api/v1/oidc/{login,callback}`、MPU capability 5 端点，`router.go:240/256`）。

---

## 2. 完整路由表（分域；≈156 个管理面动词字面量 + event 面 7 + 内容面/协议 mount）

认证记法：`匿名`｜`认证`（任意臂）｜`sys:R/W`=CapSystemRead/Write｜`sec:R/W`=CapSecurityRead/Write｜`repo:R/W`=CapRepoRead/Write｜`repoM(r/w)`=CanManageRepo(读/写)｜`内容r/w/d`=内容面 action｜`handler`=门在 handler 内（body/path 依赖）｜`MPU-cap`=上传会话 token capability 通道。所有路径省略前缀 `/binflow`。

### 2.1 system / 监控 / 治理

| 方法+路径 | handler（一句话） | 认证 |
|---|---|---|
| GET `api/system/ping` | handlePing 探活 | 匿名 |
| GET `api/system/version` | 产品名+构建 id 回显 | 匿名（刻意开放） |
| GET `api/system/license` | license 文档状态 | sys:R |
| POST `api/system/license` | 装载 license（纯文本 body） | sys:W |
| DELETE `api/system/license` | 卸载 | sys:W |
| GET `api/v1/addons` | 槽位矩阵+解锁评估（裸数组） | sys:R |
| GET `api/v1/health` | 三子系统健康 | sys:R |
| GET `api/v1/storage/stats` | 全实例 blob/字节统计 | sys:R |
| GET `api/v1/audit` | 审计 keyset 分页查询 | sys:R |
| GET `api/v1/system/logs` | 进程日志尾窗（`?limit=`1..1000 默认 200、`?filter=`、`?download=1` 附件） | sys:R |
| GET `api/v1/system/settings` | 运行时旋钮只读回显（folder_download 六字段+trashcan.retention_days） | sys:R |

### 2.2 maintenance / GC / cleanup / 备份 / 调度 / QRL

| 方法+路径 | handler | 认证 |
|---|---|---|
| POST `api/v1/system/gc` | 同步 mark-sweep（dry-run 参数也无例外） | sys:W |
| POST/GET `api/v1/system/cleanup` | 未用清理一次运行 / 状态 | sys:W / sys:R |
| GET/PUT `api/v1/system/maintenance` | 六 cron 槽（gc/cleanup-cache/cleanup-virtual/quota/compress/prune）投影/写 | sys:R / sys:W |
| GET `api/v1/system/backups`；PUT `api/v1/system/backups`；GET/PUT/DELETE `api/v1/system/backups/{key}` | 备份配置 CRUD（022 payload + 021 ledger） | sys:R / sys:W |
| GET `api/v1/system/schedules` | 台账只读投影（`?domain=` 窄化） | sys:R |
| GET/POST/DELETE `api/v1/system/query_rate_limiter/config` | AQL 限流三态读/合并写/恢复出厂 | sys:R / sys:W×2 |
| GET `api/v1/storage/migration`；POST `api/v1/storage/migration/start` | S3 迁移状态 / 启动 | sys:R / sys:W |

### 2.3 replication（含全局闸）

| 方法+路径 | handler | 认证 |
|---|---|---|
| GET/POST `api/v1/replications` | 列（secret 剔除）/ 建 | sys:R / sys:W |
| PUT `api/v1/replications/{key}` | 仅翻 enabled 位（启停开关） | sys:W |
| DELETE `api/v1/replications/{key}` | 删配置（任务行级联） | sys:W |
| POST `api/v1/replications/{key}/run` | 手动全量对账（异步） | sys:W |
| POST `api/v1/replications/{key}/test` | 探已存配置（body 可覆盖单字段） | sys:W |
| POST `api/v1/replications/test` | **草稿探测**（零落盘） | sys:W |
| GET `api/v1/replication/status` | 面板聚合状态（**FE 10s 轮询**） | sys:R |
| GET `api/v1/system/replications`；POST `…/block`、`…/unblock` | 全局 blockPush/blockPull 急刹（文本响应） | sys:R / sys:W |

### 2.4 repositories

| 方法+路径 | handler | 认证 |
|---|---|---|
| GET `api/repositories`（`?type=&packageType=` 过滤） | 仓库清单（remote/virtual 行带规范化 configuration） | repo:R |
| GET `api/repositories/{key}` | 单仓详情 | repoM(r) |
| PUT `api/repositories/{key}` | 创建/整体替换（**create 臂 family-6 在 handler 内分裂**） | repoM(w) |
| POST `api/repositories/{key}` | 局部更新 | repoM(w) |
| DELETE `api/repositories/{key}?deleteContent=true` | 删仓（非空需参数否则 400） | repo:W |
| POST `api/repositories/{key}/test` | remote 上游连通探测（草稿可携凭据） | repoM(w) |

### 2.5 storage / 制品元数据（同一 path 的 query-arm 家族）

path 形 `api/storage/{repoKey}[/{path}]`：

| Arm | handler | 认证 |
|---|---|---|
| GET（item info，可叠 `?docker_tags=1`） | FileInfo/FolderInfo | 内容 r（匿名随 flag） |
| GET `?list`（`deep=1`/`depth=N`） | 目录列表（根路径 400） | 空 route 门，**匿名 403 在 handler** |
| GET `?stats` | 节点下载统计（`lastDownloadedBy` 字段级 sys:R 门） | 内容 r |
| GET `?permissions` | 有效权限视图（枚举 principal） | repoM(r) |
| GET/PUT/DELETE `?properties=…` | 属性读（keys 过滤/尾 `*` 通配）/写（合并语义）/删（`*` 全删） | GET 内容 r；PUT/DELETE `认证`+handler 内 `a` 权限 |
| GET `api/v1/storage/usage`（`?repos=&include=counts`） | 批量配额用量（裸数组） | 认证（可见性静默过滤） |
| GET `api/v1/storage/usage/{repo}` | 单仓用量 | 认证（family-7 OR 在 handler） |

### 2.6 artifact-operations / trash

| 方法+路径 | handler | 认证 |
|---|---|---|
| POST `api/copy/{srcRepo}/{srcPath}`、`api/move/…` | 复制/移动（handler 逐项权限链 + license 门） | 认证 |
| GET `api/archive/download/{repoKey}[/{path}]` | 文件夹归档下载 | 空 route 门（handler 401/403 + license） |
| POST `api/trash/empty` | 清空回收站 | sys:W |
| POST `api/trash/restore/{path}`（`?to=&transactionSize=`） | 恢复 | sys:W |
| DELETE `api/trash/clean/{path}` | 彻底删除 | sys:W |
| （回收站**浏览**复用 storage 面：`GET api/storage/auto-trashcan` + `?list`） | — | 见 2.5 |

### 2.7 search（13 端点）

| 方法+路径 | handler | 认证 |
|---|---|---|
| POST `api/search/aql` | AQL 查询（text/plain body，`?query=` 回退、`?compact=`） | 空 route 门，401/403 在 handler |
| GET `api/search/artifact` / `checksum` | 名/校验和检索 | 同上 |
| GET `api/search/gavc` / `prop` / `pattern` | 老搜索三元组 | 同上 |
| GET `api/search/usage` / `creation` / `dates` | 用量/创建日期（epoch ms 闭开区间）/dateFields CSV | 同上（匿名 401） |
| POST `api/search/buildArtifacts`、GET `api/search/dependency` | 构建制品检索/校验和反查 | 同上 |

### 2.8 UI search 家族（BinFlow 把 Artifactory 的 `/ui/api` 面重定接到 api 树）

| 方法+路径 | 认证 |
|---|---|
| POST `api/artifactsearch/{quick,gavc,checksum,trash}` | 认证（admin,user 姿态） |
| POST `api/artifactsearch/pkg/tonative`；GET/POST `api/artifactsearch/pkg/{key}` | 认证 |
| POST `api/packagesSearch/{leadFile,artifacts}`；POST `api/syntax-search` | 认证 |
| POST/GET/DELETE `api/stashResults/**` | 认证但**工厂关闭**：恒答 verbatim 404 |

### 2.9 builds（M17 新域）

| 方法+路径 | handler | 认证 |
|---|---|---|
| PUT `api/build` | 构建信息上传（CI 面） | 认证（build 域 allow() 在 handler） |
| GET `api/build` / `api/build/{name}` / `api/build/{name}/{number}?started=&buildRepo=` | 名列表/号列表/详情 | 认证 |
| POST `api/build/append/{name}/{number}` | 模块数组追加（204） | 认证 |
| POST `api/build/promote/{name}/{number}` | 晋级（200 + messages[] 流） | 认证 |
| POST `api/build/retention/{name}?async=` | 保留策略（名级） | 认证 |

### 2.10 bundles（M17 新域，release-bundle 最小面）

| 方法+路径 | handler | 认证 |
|---|---|---|
| POST `api/release/bundle` | 显式清单建 bundle | sys:W + addon 门（handler 首行） |
| GET `api/release/bundles`；GET `…/{name}`；GET `…/{name}/{version}`；HEAD `…/{name}/{version}`（X-Checksum-Sha256 探针）；GET `…/{name}/{version}/status` | 名/版本/描述符/校验和/状态 | 认证（handler 内 `sys:R ∨ Any Distribution channel` 双门） |
| （`api/release/bundles/config` 字面量**保留**给官方端点 → 404） | — | — |

### 2.11 security——users / groups / tokens / 密码

| 方法+路径 | handler | 认证 |
|---|---|---|
| PUT `api/security/password` | 自改密 | 认证 |
| POST `api/security/users/authorization/changePassword` | 自改密别名 | 认证 |
| GET/POST `api/security/users`；GET/PUT/POST/DELETE `api/security/users/{name}` | 用户 CRUD + 局部更新（纯文本错误层） | sec:R / sec:W |
| GET `api/security/groups`；GET/PUT/POST/DELETE `api/security/groups/{name}` | 组 CRUD | sec:R / sec:W |
| POST `api/security/token` | 铸 token（JSON 投影；**step-up 门**） | 认证 + `oauth` 错误形 |
| POST `api/security/token/revoke` | 吊销（**form-only**） | sec:W + oauth 形 |

### 2.12 permissions（family-4 例外）

| 方法+路径 | 认证 |
|---|---|
| POST `api/v1/permissions` | `认证`（门=handler 内 `sec:W ∨ target.repos ⊆ 管辖覆盖` OR） |
| GET `api/v1/permissions`（无 filter） | sec:R |
| GET `api/v1/permissions?filter=manage` | `认证`（query 依赖，handler 内） |
| DELETE `api/v1/permissions/{name}` | `认证`（同写臂 OR） |

### 2.13 keypair（GPG 签名密钥对）

| 方法+路径 | 认证 |
|---|---|
| POST/PUT/GET `api/security/keypair`；POST `api/security/keypair/verify` | sec:W / sec:W / sec:R / sec:W |
| GET `api/security/keypair/public/repositories/{repoKey}`；GET/DELETE `api/security/keypair/{pairName}` | sec:R / sec:R / sec:W |
| POST `api/v1/admin/security/keypair/generate` | sec:W |
| POST `api/v2/repositories/{repoKey}/keyPairs`；DELETE `api/v2/repositories/{repoKey}/keyPairs/{keyName}` | sec:W |

### 2.14 auth-config（认证集成九端点 + SAML SP 密钥三端点）

| 方法+路径 | 认证 |
|---|---|
| GET/PUT/POST `api/v1/admin/security/ldap`、`…/oauth`、`…/saml/config`（读/写/test） | sec:R / sec:W / sec:W（test=写，因开外连） |
| GET `api/v1/admin/security/saml/config/key/public`（text/plain PEM） | sec:R |
| PUT `api/v1/admin/security/saml/config/key/public/regenerate`；POST `api/v1/admin/security/saml/key` | sec:W（旋转即换新对） |

### 2.15 session / oidc / auth-methods

| 方法+路径 | handler | 认证 |
|---|---|---|
| POST `api/v1/session` | 登录（JSON/form 双形态 body）+ Set-Cookie | 匿名（凭证在 body；自带 Origin 判定） |
| GET `api/v1/session` | whoami：`{username,admin,adminRole,source,groups[]}` | 认证 |
| DELETE `api/v1/session` | 登出（服务端 revoke + 清 cookie，幂等 204） | 认证 |
| GET `api/v1/oidc/login`（`?purpose=step_up`） | 302 IdP（state+PKCE；disabled 实例 404） | 匿名浏览器导航 |
| GET `api/v1/oidc/callback` | 授权码回跳（铸 session / 铸 step-up grant 回 `/binflow/ui/#step_up_grant=`） | 匿名 |
| GET `api/v1/auth/methods` | `{password:true,oidc:b,ldap:b}` 能力发现 | 匿名 |

### 2.16 uploads（S3 MPU，ADR-0039）

| 方法+路径 | 认证 |
|---|---|
| POST `api/v1/uploads/create`；GET `api/v1/uploads/config` | 认证 |
| POST `api/v1/uploads/{urlPart,status,complete,abort}`；PUT `api/v1/uploads/part/{id}/{partNumber}` | **MPU-cap**（会话 token 唯一凭证；dispatch 级豁免，handler 终裁 401） |

### 2.17 reindex（管理面）

| 方法+路径 | 认证 |
|---|---|
| POST `api/helm/{repo}/reindex[/；{path}】` | repoM(w)（`/api/helm/{repo}/**` 其余 = 只读内容别名，写 405） |
| POST `api/yum/{repo}`（空 key 400 臂 `api/yum`） | repoM(w) / 认证 |
| POST `api/deb/reindex/{repo}`（空 key 臂） | repoM(w) / 认证 |
| POST `api/conan/reindex`、`api/conan/{repo}[/{sub}]/reindex` | 认证（conan.ManagementHandler 自带门） |

### 2.18 webhooks（event 面，`/binflow/event/api/v1`，webhooks.go）

| 方法+路径 | handler | 认证 |
|---|---|---|
| GET/POST `/event/api/v1/subscriptions` | 裸数组 / 201 回显 | sys:R / sys:W + webhook addon 门 |
| POST `/event/api/v1/subscriptions/test` | 草稿体同步单发（200 恒定，判定在体） | sys:W + addon |
| GET/PUT/DELETE `/event/api/v1/subscriptions/{key}` | 回显 / 204 / 204（级联删投递） | sys:R / sys:W + addon |
| GET `/event/api/v1/troubleshooting`（`?subscription=&target=&start=&end=&count=`） | 投递排障记录 | sys:R |
| GET `api/v1/webhooks/outbox`（`?subscription=&status=&event_type=&limit=&cursor=`） | 死信分页 `{deliveries[],nextCursor}` | sys:R（**越过 addon 门**：锁实例仍可见） |
| POST `api/v1/webhooks/outbox/{id}/replay` | 重置死行（404 无行 / 409 非死态） | sys:W + addon 门 |

### 2.19 协议 mount 与内容面（前端第二入口）

- `apiProtocolMounts = {npm, pypi, nuget, helm}`：`/binflow/api/{proto}/**` **重写**到内容面 `/binflow/{repo}/{rest}`（nuget 平面感知 v3/v2 换位；helm 只读别名）。npm 老式 couch 登录 `PUT …/-/user/org.couchdb.user:{name}` 有专属免写法门（`router.go:1754 npmCouchLoginExempt`，仅 npm 仓）。
- 内容面 `/binflow/{repoKey}/{path}`：GET/HEAD=内容 r（匿名随 flag）、PUT/POST=写、DELETE=删；`!/` 归档成员拦截 + `X-Explode-Archive[: -Atomic]` 解包上传拦截；write 过 addon entitlement 门。**FE 的上传/下载/建目录/删除直接走这个面**（`web/src/pages/artifacts/lib.ts:387-485`）。
- `/v2/**` docker 全族（`/v2/`、`_catalog`、tags、manifests、blobs、`/v2/token`）由 docker adapter 拥有，401 为 Bearer challenge + spec body。

---

## 3. OpenAPI（158 ops/20 域）对照：FE 消费子集 vs 未消费面

### 3.1 FE 实际消费清单（运行时 fetch/xhr 实测，≈100 动词级 op）

| 域 | FE 消费端点 | 前端落点 |
|---|---|---|
| session | POST/GET/DELETE `/api/v1/session`；GET `/api/v1/oidc/login`（探测 + `?purpose=step_up`） | `lib/api.ts`、`LoginPage`、`SetMeUpDialog`、`lib/stepUpGrant.ts` |
| system | GET `system/version`、`v1/health`、`v1/storage/stats`、`v1/system/schedules`、`v1/addons`、GET/POST/DELETE `system/license` | `useVersion`、`DashboardPage`、`monitoring/*`、`LicenseAddonsPage` |
| repositories | 6 op 全消费（list+`type/packageType` 过滤、get、put、post、delete+`deleteContent`、test） | `lib/repos.ts` |
| storage | item info(+`docker_tags`)、`?list&depth=1`、`?stats`、`?permissions`、`?properties` GET/PUT/DELETE、`v1/storage/usage`(批+`include=counts`)、`v1/storage/usage/{repo}`、migration GET/POST | `pages/artifacts/lib.ts`、`QuotasPage`、`MigrationPanel` |
| 内容面 | GET/PUT/DELETE `/binflow/{repo}/{path}`（PUT 带 `X-Checksum-Sha256`；目录尾斜杠；mkdir 空 PUT） | `pages/artifacts/lib.ts:387-485`、`DeployDialog` |
| trash | empty/restore/clean + 浏览复用 `storage/auto-trashcan` | `lib/trash.ts`、`TrashPage` |
| search | POST `search/aql`、GET `search/artifact` | `search/aql.ts`、`SearchPage` |
| security | users 5、groups 5、permissions 4（含 `?filter=manage`）、token mint/revoke、PUT `security/password`、authconfig 9、SAML key 2 | `pages/security/*`、`ProfilePage`、`admin/authconfig` |
| replication | 10 op 全消费（含草稿 test、block/unblock、status） | `lib/replications.ts`、`ReplicationPage` |
| maintenance | gc、cleanup×2、maintenance×2、backups×5 | `lib/governance.ts`、`GCPage`、`BackupPage` |
| audit | GET `v1/audit`（dashboard 摘要 + 审计页 + **builds 事件时间线复用**） | `lib/governance.ts`、`builds/api.ts` |
| logs | GET `v1/system/logs?limit&filter&download=1` | `SystemLogsPage` |
| builds | GET `build`、`build/{name}`、`build/{name}/{number}`（**只读三连**） | `builds/api.ts` |
| bundles | GET 名/版本/描述符 + **HEAD 校验和探针** | `bundles/api.ts` |
| webhooks | 6 op（list/create/update/delete/test/troubleshooting；**未用单条 get**） | `lib/webhooks.ts` |

### 3.2 未消费面（API 有、FE 未用——重写可解锁的能力池）

| 域 | 端点 | 重写解锁机会 |
|---|---|---|
| system | GET `system/ping` | 探活冗余（version 已用） |
| **auth 发现** | GET `v1/auth/methods` | **LoginPage 现用「302 探测 hack」代餐**（见 §7 漂移 D1） |
| settings | GET `v1/system/settings` | folder_download/trashcan 旋钮回显面，无任何 UI |
| QRL | `query_rate_limiter/config` GET/POST/DELETE | AQL 限流管理 UI 全缺 |
| keypair | 全家族 10 op | FE 仅在审计筛选词表出现 `keypair.*` 动作名；无管理页 |
| search 老族 | checksum/gavc/prop/pattern/usage/creation/dates/buildArtifacts/dependency（9） | SearchPage 只用 2 内核；AqlPanel 手写 AQL |
| UI search | artifactsearch 7 + packagesSearch 2 + syntax-search（stashResults 恒 404） | BinFlow 已备好 Artifactory 的控制台搜索 API，旧 FE 未接 |
| copy/move/archive | 3 族 0 调用 | **控制台至今无 Move/Copy UI**（Artifactory 树右键标配） |
| uploads MPU | 7 op | 浏览器上传走单 PUT；大文件分片是解锁面 |
| builds 写面 | PUT upload/append/promote/retention | BuildsPage 明文「promote 走 API 无 UI」 |
| bundles | POST create、GET status | 只读面已用 |
| reindex | helm/yum/deb/conan 7 op | 无任何重索引 UI 入口 |
| webhooks outbox | GET list + POST replay（T-496 新） | 死信管理 UI 未跟（i18n 文案已提 outbox） |
| 协议 mount/npm/pypi/docker | 命令文案引用，无 FE 调用 | 属客户端面，非控制台面 |

### 3.3 域覆盖结论

OpenAPI 20 tags = `artifacts, artifact-operations, users, groups, tokens, permissions, keypairs, auth-config, system, maintenance, uploads, replication, session, search, repositories, reindex, webhooks, docker, npm, pypi`。**builds 与 bundles 两域整体未进 OpenAPI**（§7 漂移 D2）；控制台真实消费集中在 system/maintenance/replication/security/storage 三十端点核心环。

---

## 4. 认证与会话模型（ADR-0014 在 API 层的体现）

1. **登录**：`POST /api/v1/session`，body JSON 或 form 双形态；成功 → `Set-Cookie: binflow_session=<id>; Path=/binflow; HttpOnly; SameSite=Lax; Secure(条件); Max-Age=TTL` + whoami 体 `{username, admin, adminRole, source, groups[]}`（groups 恒数组非 null）。TTL=`console.session_ttl`（默认 24h，**绝对封顶**——T-110 塌缩句：活跃不续命）。失败统一 401 "invalid credentials"（不泄露哪半错）。
2. **CSRF**：**没有 CSRF token 端点**。ADR-0014 勘误④撤销了强制头设计，现形态 = `csrfGuard`（`middleware.go:554`）：session 臂 + 非 GET/HEAD + 携带非同源 `Origin` → 403；同源/无 Origin 放行（curl/CI 零感知）。第一层 SameSite=Lax；`X-BinFlow-Console: 1` 头是 SPA 习惯（服务端只记录不强制——**新前端建议延续发送**，与旧案权威等价）。登录端点自带独立 Origin 判定（login-CSRF）。
3. **会话等权**：cookie 臂与 Basic/Token 同链（`auth.Service.Authenticate` 第三臂），`Principal.ViaSession` 标记驱动 CSRF 门；whoami 是 SPA 路由守卫。
4. **step-up**（ADR-0027）：`POST /api/security/token` 对「web-session + 非 admin + `auth.token_step_up` 开」铸币要求第二凭证：401 OAuth 体 `{error:"step_up_required"}`；local/ldap 腿 body `step_up_password`，oidc 腿 `step_up_grant`（浏览器跳 `GET /api/v1/oidc/login?purpose=step_up` → IdP 强制 `prompt=login` → 回跳 fragment `#step_up_grant=<64hex>` 到 `/binflow/ui/`，grant 单次消费、错误码 `step_up_invalid`）。豁免臂：admin session、Basic、Bearer、/v2/token、匿名。
5. **能力门**（routeAuth 闭集）：`sys:R/W`、`sec:R/W`、`repo:R/W`、`repoM(r/w)`（单仓 manage，`m` action）、内容面 `r/w/d/a`；readonly_admin = 三个读能力。fail-closed（无 ManagementAuthorizer facet 即拒绝）。
6. **旁路窄化**：`deployScopeGuard`——checksum-deploy 5 分钟 token 仅许单个带 `X-Checksum-Deploy` 的目标 PUT，其余一律 403。

---

## 5. 错误信封 / 分页 / 响应包裹

**错误三形态 + registry 第四形态**（`envelope.go` + `middleware.go`）：

| 形态 | 体 | 适用面 |
|---|---|---|
| errors[]（默认） | `{"errors":[{"status":404,"message":"…"}]}`（2 空格缩进 JSON） | 全部 /binflow 面，含内容路径 |
| 纯文本 | 非信封文案 | 用户/组管理层错误（PUT/POST/DELETE users/groups 的 handler 内错误） |
| OAuth | `{"error":"…","error_description":"…"}` | `security/token` 族一切非 2xx（含 403） |
| docker spec | `{"errors":[{"code":"UNAUTHORIZED",…}]}` + Bearer challenge | /v2 面 |

**401/403 语义**：401 = 匿名撞 required 门，带 `WWW-Authenticate: Basic realm="BinFlow Realm"`；403 = 已认证但无能力（"administrator privileges required" / "permission denied"）；内容面 denied→匿名 401 / 已认证 403。E-26②：未实现端点 404 message 含 "is not implemented in BinFlow"；E-26① 根镜像 404 带 /binflow 提示。panic → 500 信封（响应已提交则截断不追写）。FE 侧（`lib/api.ts:53 toApiError`）三形态统一折成 `ApiError{status,message,raw}`——**新前端应保留这一收敛层**。

**分页**（均为 keyset cursor，无 offset 分页）：

| 面 | 参数 | 包裹 |
|---|---|---|
| audit | `?repo&actor&action&since&until&limit(1..1000 默认100)&cursor` | `{events[],nextCursor}`（恒在，尾页 ""） |
| webhook outbox | `?subscription&status&event_type&limit(默认50)&cursor` | `{deliveries[],nextCursor}` |
| storage `?list` | `deep=1`/`depth=N` 树形 | `{files[]}`，非分页 |
| AQL | 查询文本内 offset/limit；range 尾 `start_pos/end_pos/total`（**total=返回行数非全集**，流式约定） | Artifactory 逐字形态（前导 `\n`、`"results" : [`） |
| repos/users/groups/webhook subs/addons/builds/bundles | 无分页 | 全量裸数组 |

---

## 6. 实时 / 轮询面

**无 WebSocket/SSE，全轮询**。现存四个 interval（web/src 实测）：

| 页面 | 端点 | 周期 | 形态 |
|---|---|---|---|
| `governance/ReplicationPage.tsx:105` | GET `v1/replication/status` | **10s** | setInterval + 失败保留旧数据（stale 提示） |
| `governance/MigrationPanel.tsx:59` | GET `v1/storage/migration` | **5s** | 同款 stale 姿态 |
| `monitoring/SystemLogsPage.tsx:47` | GET `v1/system/logs` | **7s** 尾随 | 1s 计拍倒计时归零重取；可暂停；`download=1` 走 `<a href>` |
| `components/SetMeUpDialog.tsx:618` | （无请求） | 1s | step-up grant TTL 提示倒计时，纯展示 |

Dashboard/ServiceStatus/StorageSummary 均**手动刷新**（tick state，无自动轮询）。另有全局 401 监听：已认证态任何请求 401 → toast + 重登跳转（`silent401` 豁免探活/登录）。

---

## 7. 契约漂移清单（前端在用但 OpenAPI 未收 / 反向缺口）

**FE 在用、binflow.json 未收（D 类，须在重写前回填 fern）**：

| # | 端点 | FE 消费点 | 佐证 |
|---|---|---|---|
| D1 | GET `/api/v1/system/logs`（含 `download=1`） | SystemLogsPage（M17 T-493） | jq 全 paths 无此路径 |
| D2 | **builds 全域**：GET `/api/build`、`/api/build/{name}`、`/api/build/{name}/{number}`（以及 FE 未用的 PUT/append/promote/retention） | BuildsPage | spec 无任何 `build` path；20 tags 无 builds 域 |
| D3 | **bundles 域 5 缺 2**：HEAD `/api/release/bundles/{name}/{version}`、GET `…/{name}/{version}/status` 未收（spec 只收了 POST bundle + GET 3 条） | HEAD 被 BundlesPage 消费 | spec 两个 head 是 `/{repoKey}/{path}` 与 `/v2/.../blobs/{digest}` |
| D4 | OIDC 族：GET `/api/v1/oidc/login`（含 `?purpose=step_up`）、`/api/v1/oidc/callback` | LoginPage 探测 + SetMeUpDialog step-up | spec 无 oidc 路径 |

**API 有、OpenAPI 未收（FE 也未用，spec 欠账）**：GET `/api/v1/auth/methods`、GET/POST `/api/v1/webhooks/outbox[/{id}/replay]`、POST `/api/search/buildArtifacts`、GET `/api/search/dependency`、`api/release/bundles/config` 保留字面 404。query 级欠账：`/api/storage/{repoKey}` spec 仅收 `repoKey,list` 参数——`stats/permissions/properties/docker_tags` arm 未入参文档。

**FE 行为漂移（B 类，重写时修正）**：

| # | 现状 | 建议 |
|---|---|---|
| B1 | LoginPage 用 `fetch(oidc/login, redirect:'manual')` 探测 302 判断 SSO 开关（代码注释自认「后端补出显式端点后应迁移」——T-179 早已补出 `/api/v1/auth/methods`，FE 未迁移） | 新前端登录页直接消费 `GET /api/v1/auth/methods`（三布尔），顺带解锁 LDAP 位展示 |
| B2 | `?properties` 家族：api-reference.md 列有 POST 增量形态，router 只挂 GET/PUT/DELETE（T-291 注记） | 文档债：以 router 为准（PUT 合并语义已覆盖） |

---

## 8. 给架构设计的关键结论

1. **消费面本质是「~30 端点核心环 + 长尾管理面」**：重写 MVP 必须覆盖 §3.1 全表（≈100 op）；TanStack Query 的 key 设计应按域+keyset cursor 建模（audit/outbox 两处 cursor，其余全量数组）。
2. **认证层零 token 化**：cookie 会话 + Origin 同源 CSRF，无 CSRF token 获取端点——新栈无需 CSRF token 注入设施；`X-BinFlow-Console` 头保留为习惯层。
3. **双数据根**：管理面 `/binflow/api` 与事件面 `/binflow/event/api/v1` 两个 fetch 根（`lib/webhooks.ts` 自带信封）+ 内容面直 fetch `/binflow/{repo}/{path}`（上传需 XHR 进度、下载需流式 sha256 tee）——API client 需三入口一信封。
4. **错误解析必须是三形态折衷器**（errors[]/纯文本/OAuth），旧 `toApiError` 逻辑直接平移。
5. **轮询升级空间**：replication 10s / migration 5s / logs 7s 是既有节奏；新栈用 TanStack Query `refetchInterval` 等价复刻即可，无需推 WebSocket（Go 侧也无可消费的推送面）。
6. **重写解锁优先序建议**（依据 §3.2）：auth/methods（登录页正确化）→ copy/move UI（最大交互缺口）→ keypair 管理页 → outbox 死信面 → builds 写面（promote/retention）→ QRL/settings → UI search 家族 → reindex 动作。
7. **fern 回填先于重写**：§7 D1-D4 四处漂移中 D2/D3（builds/bundles 整域）是 M17 增量未入册，重写以 OpenAPI 为唯一契约源前必须补齐，否则 capability matrix 会漏两个新域。

# 四、质量资产面审计报告

# BinFlow web/ 控制台重写前置审计 —— 质量资产面（e2e / i18n / a11y / 构建链 / 锚册 / 性能）

审计基准：develop @ e299d9d4 + 工作区未提交变更（含在建的 `web/src/pages/builds/`）。全部数字来自实际执行（grep/脚本运行），可复验。

---

## 1. web/e2e/** 全量清单

### 1.1 目录分组统计（共 84 spec，约 423 个 test 用例）

| 目录 | spec 数 | 用例数（约） | 里程碑域 | 运行 project |
|---|---|---|---|---|
| `web/e2e/`（根） | 20 | 104 | M8 前后沉淀 + T-104 基座 + G 序列验收 | chromium |
| `web/e2e/m8/` | 13 | 66 | M8 交互断言基座（T-232 起） | chromium |
| `web/e2e/m9/` | 5 | 25 | M9 NFR/性能收敛 | **m9**（独立 project） |
| `web/e2e/m10/` | 8 | 21 | M10 license 门控/协议试点/属性 | **m10**（独立 project） |
| `web/e2e/m12/` | 1 | 4 | 回收站真栈 | chromium |
| `web/e2e/m13/` | 3 | 25 | Webhooks + 树尾回收站节点 | chromium |
| `web/e2e/m14/` | 8 | 43 | Artifactory parity 批（表单/列选/token/品牌/图标） | chromium |
| `web/e2e/m15/` | 5 | 26 | 列选推广/虚拟树/AQL/阻断/L2 内联 | chromium |
| `web/e2e/m16/` | 16 | 82 | 全前端对齐大里程（树栈/表单/pager/i18n） | chromium |
| `web/e2e/m17/` | 4 | 18 | 初始态/纯净计数/Builds/Bundles | chromium |
| `web/e2e/auth-config/` | 1 | 9 | 认证配置三 Tab | chromium |

### 1.2 逐 spec 能力断言（一句话/个）

**根目录（20）**

| spec | 能力断言 |
|---|---|
| artifacts.spec.ts | 通用仓全链：上传→树浏览→详情/下载 sha 对账→mkdir→面包屑→删除幂等；409/413 错误原文上屏；maven GAV 表单预检；对话框关闭止队列；大目录客户端 load-more |
| auth-shell.spec.ts | 认证壳：未登录重定向带 return、错误凭据内联、登录落地 shell+仪表盘+404 保持壳、登出服务端吊销会话、改密错误措辞内联 |
| console-smoke.spec.ts | SPA 壳挂在 /binflow/ui/ 且带 #root 挂载点；深链刷新回壳（段内 history fallback） |
| governance.spec.ts | 审计过滤器+keyset 页窗分页+路径客户端过滤+REST parity；GC 干跑→类型化确认→应用；配额水位/内联编辑/413 封顶；非 admin 导航隐藏+深链 L2 |
| login.spec.ts | OIDC 禁用 SSO 钮隐藏；SSO 点击顶层 GET /oidc/login 离开 SPA；SSO 再检 404/5xx 内联降级；LDAP 用户走同一表单 |
| pathmatch-parity.spec.ts | 前端路径匹配器与 internal/auth pathmatch_test.go 的 fixtures 同源 parity（纯单测腿，非浏览器） |
| rbac.spec.ts | UI 设 readonly_admin 角色+API snake_case 回显+审计；readonly 全站走查写重放 403；principals 矩阵 manage 位往返 |
| replication.spec.ts | 复制状态页：列表渲染+10s 自动刷新+空载荷/501/404/403/瞬断轮询五类降级态 |
| repo-policy-keys.spec.ts | deb/rpm/helm 策略键编辑器 wire 体对账与往返；readonly 禁用+表单注记；审计动作选择器全词汇（54+9） |
| repositories.spec.ts | local 全生命周期（建/列/改/删含内容）；remote url 往返密码不回显；表单预检零写请求；virtual 成员序+defaultDeploymentRepo+400 内联 |
| security.spec.ts | W33 三步建权限 target（组→成员→矩阵）+tester；组授予第二会话即时生效；被引用组删除 409 点名 target；用户/组编辑往返 |
| storage_migration.spec.ts | 存储迁移：进行中进度条 5s 自刷、完成态失败行、501/403 降级、启动确认 YES 门、409 冲突面板 |
| t104-crossbrowser.spec.ts | 三链冒烟（登录正/负、树浏览到文件行、上传到 checksum 徽标）——多浏览器引擎形态 |
| t104-perf.spec.ts | 1GB UI 上传完成且服务端 RSS 增量 <256MB；50 并发登录零会话串扰；SPA 冷载计时（record-only×3） |
| t104-review-leftovers.spec.ts | 上传 403 内联指引（只读用户）；hashing 阶段关闭零 PUT；sameSnapshot 交换序等价 |
| t104-supplements.spec.ts | docker UI 建仓+dind 真客户端种子；manifest 树节点+两段式删除；审计 actor 过滤=REST 结果集；DOM 无 token/密码明文 |
| t131-g30e.spec.ts | 树浏览全程零 /api/search 请求；typo 深链 404 空态；mkdir 单段单 PUT；>PAGE 大目录分页回归 |
| t134-g32.spec.ts | docker 树浏览零 /v2/* 请求；digest 行 tag 徽标；树 tag 集=crane tags/list；“摘要”列头；help 菜单文档入口 |
| t146-docs.spec.ts | /binflow/docs/ 文档站：200+品牌、301、离线零外链、搜索可用、全导航无 404、安装页可达、404 页 |
| trash-can.spec.ts | 回收站：根列表/下钻/五元组详情、恢复 REST 动词、清空 purge 摘要、typed EMPTY 门、community 锁卡、未用降级、三角色面+匿名重定向 |

**子目录（64）**

| spec | 能力断言 |
|---|---|
| auth-config/auth-config.spec.ts | 认证配置三 Tab（LDAP/OAuth/SAML）：字段清单=锚全渲染、往返、secret 省略/sentinel 拒绝、test connection 双模式、readonly/不可达、SP 证书再生、axe |
| m8/a11y-sweep.spec.ts | 31 路由×双主题=62 面 axe 全扫（serious/critical=0）+登录页 2 面；QA-1/QA-2 修复的元素级钉腿 |
| m8/artifacts-tree.spec.ts | 跨仓树：repo 顶层节点+过滤、深链自动展开祖先+滚动入视、右键菜单三形态+Shift+F10、复制全值、readonly/plain 面、回收站入口、axe、树展开计时 |
| m8/auxiliary.spec.ts | 仪表盘三角色数据面+快捷入口+审计行深链树；顶栏搜索链/recentSearches 持久/?q= 深链；登录内联/404/profile 拆分表单 |
| m8/governance-monitoring.spec.ts | 治理 5 页+存储摘要+系统信息 API parity；readonly 读面写禁重放 403；user 深链 L2/L3 收敛 |
| m8/helpers.spec.ts | 种子自证：≥10,000 节点经真 REST 物化；loginAs 三角色会话标记；OS 剪贴板全值；登录页 axe；first-interactive/树展开计时采集 |
| m8/keyboard.spec.ts | 全键盘面：键盘登录、侧栏 Enter、树箭头导航+Shift+F10、表格行箭头、tablist 箭头切页签、对话框焦点陷阱环绕+Esc |
| m8/permissions.spec.ts | 两步资源对话框全链+四动作矩阵+权限 tester+manage 徽章；readonly 走查；m-holder 覆盖进/出（201/403）；键盘循环；徽章对比度 |
| m8/repositories-admin.spec.ts | 包类型网格向导全链（?rclass= 预设+组合门控）；三 Tab 子路由+排序+计数分页；行级删除强确认（typed key+两段式）；readonly/m-holder 面 |
| m8/setmeup-deploy.spec.ts | Set Me Up 抽屉直开+token mint+命令内嵌复制；包类型=现有仓并集；step-up 内联密码表单（mock+真武装两腿）；拖拽上传特殊字符名+checksum 徽章 |
| m8/shell.spec.ts | 双模式侧栏（app 2 项/admin 5 组 18 项）全键盘切换；readonly 徽章无写入口；plain 无切换+L2；遗留路由全落 404 |
| m8/smoke.spec.ts | 三角色键盘冒烟：login→shell→logout 仅用 page.keyboard |
| m8/theme-smoke.spec.ts | 默认亮色；根节点自有 token computed 存在性+切换纯换值+localStorage 持久化；双主题核心页 axe |
| m8/users-groups.spec.ts | 用户建+成员转移+分区编辑+角色下拉+启用翻转；组编辑器逐用户写+矩阵+删除守卫措辞；readonly+键盘 |
| m9/console-fr82.spec.ts | 树过滤器钻入重置术语、跨仓切换清双过滤、过滤空态明确可清；顶栏搜索 Enter 提交+recentSearches 入库、共享 store 下拉、空词行为 |
| m9/oidc-stepup.spec.ts | OIDC step-up：required→re-auth 引导（无密码表单）+fragment grant 恰一次消费+重放拒绝；invalid grant 内联 ADR 原文；mock IdP 真武装全链 |
| m9/permissions-mholder.spec.ts | u9 列表恰一次 filter=manage 请求（环境请求零）；编辑器全字段水合+覆盖外保存 403；删除走 danger zone；u8 L2 友好收敛 |
| m9/usage-fanout.spec.ts | used 列整页一次批量注水（cap≤3 请求，排序/过滤零再发）+数值对账；readonly 50 行；user 可见集=恰可读十仓；批量挂/500/重试四态 |
| m9/users-groups.spec.ts | 用户页单 E2 请求零逐用户扇出；Status 真值=E3 回显；删除用户 typed 强确认+级联+404 原文；readonly 无删除入口 |
| m10/console-license-addons.spec.ts | license 页+社区地板卡+addons 活体矩阵；安装拒绝 400 wire 原文；repo 对话框 core/pro 徽章门 |
| m10/gating-matrix.spec.ts | 骨架占位（test.skip——FR-85/86 门控矩阵未填） |
| m10/go-pilot.spec.ts | Go 协议真客户端腿：PUT/list/sha 对账/！lower 转义、go mod download/build、remote pull-through 缓存二跳、virtual 本地优先 |
| m10/license-lifecycle.spec.ts | 骨架占位（test.skip） |
| m10/nuget-pilot.spec.ts | NuGet 真客户端腿：v3 push+index 对账、dotnet pack→push→restore→run、远端二跳缓存、v2 OData 形态、virtual 聚合 |
| m10/properties-matrix.spec.ts | 属性系统：矩阵参数部署剥离存储、REST 读写删族、400 校验臂、legacy 分号回归（FR-89-AC4）、UI 三角色+danger 确认 |
| m10/quickwins.spec.ts | 骨架占位（test.skip） |
| m10/regression-nfr.spec.ts | 骨架占位（test.skip） |
| m12/t351-qa-midterm.spec.ts | 回收站真栈双形态（license 自门控）：community 锁卡 / pro 捕获→浏览→五元组→恢复往返+审计词汇 |
| m13/t366-consumer.spec.ts | Webhook 消费端：UI 建签名订阅、PIPE 签名 7 字段信封、TEST 明文头、SSRF 拒绝、RETRY 重试链、DEAD 环记、HANG 30s 预算、抽屉死信链 |
| m13/t366-webhooks.spec.ts | Webhooks 管理页全链：空态→建对话框→行、闭集域、编辑保密、test 面板、启停全量 PUT、danger 删除、抽屉投递记录、readonly/锁槽/失败重试、axe 双主题 |
| m13/t372-trash-node.spec.ts | 树尾常驻回收站节点：深链往返、键盘可达、过滤下驻留、readonly 可见、plain 反断言、axe |
| m14/t383-newrepo-parity.spec.ts | 两段式建仓链（网格模态→整页表单→Save）；分路由深链六节矩阵；模态形钉（radiogroup tiles/924px/Esc）；axe |
| m14/t384-usergroup-parity.spec.ts | 用户/组整页路由表单链+设置/成员节+逐用户成员落地；footer 三钮 Cancel/Reset/Save 语义；axe |
| m14/t386-tokens-page.spec.ts | token mint 链（一次性明文/台账/关窗即失效/端点预算）、revoke 强确认、by-id、readonly 有限 TTL、step-up 两腿、焦点链、axe |
| m14/t387-l1-columns.spec.ts | repos/audit 列选择器（开合/显隐/守卫/重置/跨页持久）+刷新再取数；列选按页隔离；axe |
| m14/t388-f2n2.spec.ts | 一级导航 16px 单色图标封闭集；空态插图槽（40px/CTA 在下）三页+negative 面；axe |
| m14/t389-brand.spec.ts | 品牌槽：侧栏恒深 mark+登录 lockup 随主题；favicon ico 三档+PNG 档+apple 180+manifest 512 全部接线可服务 |
| m14/t390-pkg-icons.spec.ts | 包类型网格全品牌 marks+tier 徽章+零 <text>；Set Me Up pills 品牌标；仓列表 chip/树节点单色标；addons 矩阵行标；axe |
| m14/t404-replication-crud.spec.ts | 复制 CRUD：内联建只提交 wire 集、编辑=删+重建恰一对、typed 名删除、toggle PUT、readonly、列表列+Run 触发、降级注记、axe |
| m15/t414-columns-promo.spec.ts | users/groups/search 列选器推广（含 search size/sha256 默认 opt-in）；virtual 成员 pop 的 color-mix+hover 行 axe |
| m15/t416-virtual-tree.spec.ts | 虚拟树动态展开成员并集（深递归/同名目录合并）；空态语义；删除面收敛（405）；axe |
| m15/t419-aql-mode.spec.ts | AQL 模式：模式切换+深链、结果走列框、400 E-01 原文、列头点击注入/翻转/移除 .sort()、pager 重写 .offset()、429/408/K63 截断、axe |
| m15/t422-block-test.spec.ts | 全局阻断：表单 test-connection 三臂（草稿态零持久）、自实例拒绝、真双实例（BASE2）、双向翻转+配置面不受累、readonly |
| m15/t424-l2-inline.spec.ts | 行内 L2 快操作：copy-key aria-label+Space 激活+剪贴板全值；Set Me Up 行内直开同抽屉（smu-* 锚）；E1 无行尾菜单；readonly 读面；axe |
| m16/remote-browse-tree.spec.ts | 远端浏览可选档：advanced 复选默认关、off=仅缓存行、on=helm 全树/rpm·deb 枚举/virtual 远端成员、上游故障降级三面、a11y |
| m16/t434-tree-stack.spec.ts | 树栈：文件叶子进树、select≠expand、URL 模型（tab 段+文件末段+legacy 折入）、工具带（facet/rclass/sort/行高/favorites 持久）、axe |
| m16/t439-form-stepper.spec.ts | 表单分段器（Basic/Advanced/Replications）；七个保留字段域禁用零提交；forceConan 往返；footer 收敛（无 Reset）；API 漂移钉；axe |
| m16/t441-pkg-modal-open.spec.ts | 924px 网格模态 13 真实 tiles+门控八型放行+徽章；前端门开+后端 verdict 终决；licensed 矩阵一键一型；axe |
| m16/t443-list-entry-dirty-test.spec.ts | 列表入口三预设下拉分路由；列收敛；脏门控（Save 禁用/复原再禁/零净提交）；remote test 三臂零副作用；axe |
| m16/t445-detail-fields.spec.ts | 详情页签序（General→Effective Permissions→Properties）；文件 URL 复制+?stats 下载族；Module ID/虚拟关联如实缺席；plain 用户计数可见/lastDownloadedBy 扣发 |
| m16/t447-props-download.spec.ts | 属性常显表单+同键替换保兄弟+danger 删除；属性网格搜索；下载 24px 单图标+伴随菜单（verify/checksums）；axe |
| m16/t449-search-stack.spec.ts | 搜索列集（默认 5 列+size/sha256 opt-in+tz 偏移）；行体惰性仅名称深链；网格内快滤+选择列复制路径；顶栏驻留查询；AQL 共列框；axe |
| m16/t451-pager-unification.spec.ts | 统一分页：搜索页窗 pager（跳页/档位/边界禁置）；管理单页全量链禁置；AQL pager 重写 offset/limit；axe |
| m16/t453-route-forms.spec.ts | /users/new、/groups/new 深链路由表单：保留能力位+净载荷+建列改循环；readonly 深链只读；axe |
| m16/t455-perm-five-columns.spec.ts | 五列权限矩阵（read/annotate/write/delete/manage）+annotate 往返；legacy write/deploy-cache 兼容窗水合；两步对话框；readonly |
| m16/t457-profile-help-about.spec.ts | profile 自铸 token（一次性明文+Bearer/curl 即用+关窗失效+TTL 上限）；step-up；SSH 如实缺口；help 四项+About=version API；axe |
| m16/t459-monitoring-nav.spec.ts | 服务状态（badge/子系统/version 对账 health+version API）；系统日志（行=进程日志 API+尾随刷新+暂停+服务端过滤+下载附件+403 降级）；导航分组 6 页；admin 过滤器 |
| m16/t462-cron-backups.spec.ts | 维护 cron 三槽对账+gc 槽设/读回/清；Run Now 手动面；备份建/改/E1 删；readonly 门+注记；复制 cron 联动；plain L2；axe |
| m16/t463-i18n-skeleton.spec.ts | i18n 骨架：zh 默认键即文案原样+lang 同步+零告警；en 持久化引导+catalogs-*.js chunk 恰被请求；reload 持久 |
| m16/t464-i18n-bilingual.spec.ts | 双语：侧栏脚切换往返+reload 持久；en 采样七域；en 日期格式 MMM d, yyyy；术语 parity（Set Me Up/Deploy 原样）；双 locale axe |
| m17/t492-initial-state-lastlogin.spec.ts | 初始态：进 /artifacts 自动选首仓+item view；空仓/403 边界；回跨仓根保持根视图；users last login 列投影+列选集成 |
| m17/t494-pure-browse-count-logs-source.spec.ts | 纯浏览零下载计数（3 真下载恰 3）；系统日志进程源为主（对账+服务端过滤+附件）+404 降级审计载体 |
| m17/t512-builds-page.spec.ts | Builds 三视图（导航→名单→号单→run 详情含时间线）；搜索 Builds 档 AQL 合成+行深链；Module ID=build.* 属性族；读门 403/404 如实；axe |
| m17/t514-bundles-presets.spec.ts | Release Bundles：三预设桶与真仓联动+wire 对账；Any Distribution 授予覆盖（未授予 200 空集+404 同形）；admin 渲染+axe；versions 404 零泄漏措辞 |

### 1.3 现有 e2e 覆盖的能力集合（重写 capability matrix 的输入）

按能力簇归纳（≈22 簇）：

1. **认证/会话**：密码登录（正/负/内联）、OIDC SSO 顶层跳转+再检降级、LDAP、登出吊销、改密、step-up（密码/OIDC grant 两形态）、50 并发会话隔离。
2. **RBAC 三角色走查**：admin / readonly_admin / plain user 的 L1 导航门、L2 无权限卡、L3 隐藏、写重放 403、m-holder 覆盖面（filter=manage 恰一次请求）。
3. **树浏览**：跨仓树、懒加载单层展开、深链自动展开+滚动定位、文件叶子、select≠expand、URL 模型（tab 段+文件末段）、工具带（facet/sort/行高/favorites）、虚拟树成员并集、远端浏览可选档、过滤语义（钻入重置/跨仓清空）、树尾回收站节点。
4. **上传/部署**：对话框上传（队列、关闭止队列、hashing 阶段零 PUT）、maven GAV 表单、拖拽特殊字符名、1GB 大文件、409/413/403 错误原文。
5. **下载/校验**：下载 sha256 对账、verify 能力、checksums 面板、?stats 计数联动、纯浏览零计数。
6. **搜索**：顶栏驻留查询、recentSearches、?q= 深链、列框（默认列+opt-in）、快滤、AQL 模式（编辑器/错误原文/sort 注入/offset 重写/截断/429/408）、Builds 档。
7. **分页**：统一 Pager（页窗跳页/档位/边界禁置/keyset 流式末页未知/AQL offset）。
8. **仓库管理**：网格向导（两段式）、分路由表单（stepper、保留字段、脏门控、remote test 三臂）、策略键、列表列选/刷新、行内快操作、复制列、删除强确认（typed key+两段式）。
9. **权限 target 编辑器**：两步对话框、五列矩阵、tester、组/用户主体、dotted name、m-holder。
10. **用户/组/token**：路由表单 CRUD、成员转移、last login 列、token mint/revoke/step-up、一次性明文。
11. **治理**：审计（过滤/keyset/列选/词汇表）、GC（干跑/确认/审计）、配额（水位/413）、回收站（五元组/恢复/清空/锁卡）。
12. **复制**：CRUD（编辑=删+重建）、toggle、状态页轮询降级、全局阻断、cron。
13. **监控**：存储、服务状态、系统日志（进程源/过滤/下载/暂停）、系统信息、备份。
14. **Webhooks**：订阅 CRUD、签名投递（PIPE/TEST/RETRY/DEAD/HANG/SSRF）、抽屉投递记录、锁槽。
15. **license/addons 门控**：社区地板、pro 徽章、安装拒绝原文、tier 自门控形态。
16. **协议 pilot（真客户端）**：docker（dind push/pull）、go、nuget、maven、npm 等。
17. **品牌/图标**：favicon/PWA 全档、品牌 marks、单色图标集、插图槽。
18. **i18n**：zh 默认、en 持久化+懒 chunk、双语切换、日期格式、术语 parity、双 locale axe。
19. **主题**：默认亮、token 纯换值、持久化、双主题 axe。
20. **a11y**：31 路由×双主题全扫 + 47 spec 内嵌 axe 腿 + 键盘全套（树/表/对话框/tablist/焦点陷阱）+ 对比度族。
21. **性能/预算**：1GB 上传 RSS、冷载计时、树展开计时、零多余请求族（G30e/G32/N01/E1 批量注水 cap≤3）。
22. **文档站**：/binflow/docs/ 全链（离线、导航、搜索、404）。

### 1.4 playwright.config.ts（`web/playwright.config.ts`，112 行，注释即设计文档）

| 维度 | 值 | 备注 |
|---|---|---|
| testDir | `./e2e` | |
| projects | `chromium`（testIgnore `/(^|\/)(m9|m10)\//`）/ `m9`（testMatch m9/）/ `m10`（testMatch m10/） | m12~m17、auth-config 都走 chromium project；裸跑=三 project 全跑、每 spec 恰一次 |
| timeout | 180s/测试 | 并发时代预算；axe-heavy 腿自设 300s/600s |
| retries | `CI ? 2 : 0` | 本地必须响亮红（T-471） |
| expect.timeout | `CI ? 10s : 5s` | CI 慢 runner 吸收 |
| workers | 4（fullyParallel: true） | AC 地板 ≥4；CI 另用 CLI `--workers=2` 覆盖 |
| reporter | `CI ? 'github' : 'list'` | |
| trace | `retain-on-failure` | |
| baseURL | `$BASE ?? http://127.0.0.1:8080` | 不自启服务器；须先 `make build && ./bin/binflow-server serve` |
| globalSetup | `e2e/support/base-probe.ts` | 起跑前验 $BASE 是真 BinFlow（/api/system/version product=="BinFlow"），防打错实例 |

**CI 差异**（`.github/workflows/ci.yml` e2e job）：独立 runner（ubuntu-latest，45min），BASE=127.0.0.1:8080，`make console && make docs && make build` 新鲜二进制，默认裸配启动（admin/password、无 binflow.yaml），经 seed-m8/m9/m10 真 REST 种子后 `npx playwright test --workers=2`。本地协议=「串行重跑绿=过」（web/README.md:58）。全量跑前提=纯净 community 档新种子实例（pro 档会成片误红，e2e/README.md §1）；dind 需 `--feature containerd-snapshotter=false`（§2）。

### 1.5 seed 脚本形态（`web/scripts/seed-m{8,9,10}.mjs` + 各 `e2e/m*/support/seed.ts` 薄再导出）

| 脚本 | 规模 | 形态 |
|---|---|---|
| seed-m8.mjs（385 行，+seed-m8.d.mts 类型声明） | 10,291 节点 / 1,120 PUT | 确定性树计划（50 宽层×20 文件 + 120 深链×76 节点，祖先物化）；全走真 REST/内容面；`makeClient`/`converge`（并发首建冲突重试，T-326 D-9）被 m9/m10 复用；角色夹具三用户+读授予；CLI 可独立跑（--base/--admin/--password/--skip-tree/--plan-only） |
| seed-m9.mjs（442 行） | 50 仓 / 20 用户 / 10 组 / 4 覆盖夹具 | usage 内容=repo key 纯函数（可计算期望值、幂等重 PUT delta=0）；覆盖夹具 t-u8-r/t-in/t-out/t-grp 支撑 E1/E6/E7/E9 腿 |
| seed-m10.mjs（359 行） | 2 仓 / 2 用户 / 5 个含 `;` 文件 | FR-89-AC4 legacy 分号回归载体：字面路径字节回读断言的 fixture 底稿 |

模式总结：**seed 是 e2e 的数据地基且本身是 wire 的 workout**；support/seed.ts 仅做 TS 再导出 + `m8Client()` 惯用法。重写后 seed 脚本与 Go API 无关，可原样保留。

---

## 2. src/i18n/** 全机制（重写必须原样保住）

### 2.1 架构（`web/src/i18n/index.ts`，143 行，自研零依赖内核）

| 机制 | 细节 |
|---|---|
| 方案 | **不用 i18next/react-i18next**——自研 <1KB gz（域键查表+{var} 插值+回落链）；理由：go:embed 单二进制体积预算（SPA ≤5MB gzip） |
| zh-as-key | msgid = zh 文案原文（gettext 形）；键值恒等使外提零语义变化可机械证明；**zh 无运行时包**（回落即键，永不空渲染） |
| 目录结构 | `locales/en/<domain>.ts`（12 域 + `catalogs.ts` 索引）+ `manifests/zh/<domain>.json`（12 清单=zh 包物化形态/en 填译对照底稿） |
| 域 | console/repositories/artifacts/bundles/builds/search/security/governance/monitoring/webhooks/admin + **common**（≥2 域共用的键路由到 common——路由规则由工具执行） |
| 查表链 | en → 域包 → common 包 → 键本身（=zh 文案兜底）；空串=未填骨架→回落；DEV 态 missing 键 console.warn 去重 |
| 插值 | `{name}` 形；缺参保留占位符原文（不静默吞） |
| 切换载体 | `localStorage['binflow-console-locale']`；`setLocale()`=持久化+**整页 reload**（模块级 t() 顶层求值无需响应式；Artifactory 同款姿态）；`<html lang>` 同步 zh-CN/en |
| 懒载 | 仅 en 引导时 `initI18n()`（main.tsx:121 `initI18n().then(render)`）动态 `import('./locales/en/catalogs')` → vite 独立 chunk `assets/catalogs-<hash>.js`（t463 spec 断言该 chunk 恰被请求）；zh 用户零额外字节零请求；加载失败=告警+回落 zh |
| t() 用法约定 | 组件/常量模块**顶层** `const t = tr('domain')`，调用点 `t('文案键')` / `t('{n} 项', { n })`；绑定识别只认顶层 `const X = tr('域')`（assert-i18n 的解析前提） |

### 2.2 规模（`node scripts/assert-i18n.mjs --stats` 实跑）

2713 调用点 / 2054 键：console 215、repositories 393、artifacts 215、bundles 22、builds 30、search 54、security 334、governance 314、monitoring 88、webhooks 89、admin 149、common 151。

### 2.3 断言与再生成工具

| 工具 | 职责 |
|---|---|
| `scripts/assert-i18n.mjs`（174 行） | 三道闸（TS 编译器 AST 解析）：①硬编码闸——src 内（i18n 目录除外）JSX 文本/字符串/模板出现 CJK 且非 `tr()` 首参 → 违规（行尾 `// i18n-allow` 豁免）；②键集闸——t() 键按 ≥2 域→common 路由后与 en 目录包 registerEn 键集**双向同构**（missing/orphan 都算漂移）；③清单闸——zh manifest 与路由结果逐域一致。挂 `npm run build`（vite build 前）与 `npm run lint` 尾部，退出码 1 即红 |
| `scripts/t463/regen-catalogs.mjs` | 从源码 t() 调用重算键路由，重写 `locales/en/*.ts` + `manifests/zh/*.json` + `catalogs.ts`；**逐键保值**存量 en 译值（仅新键落空串）；先清场防 orphan。手工修键集后免重跑 codemod 用 |
| `scripts/t463/codemod.mjs` | 一次性 CJK→t() 全树转换（不可重放） |
| `scripts/t463/verify-extraction.mjs` | HEAD 基线 CJK 多重集 = 改后键集 的零语义变化机械证明 |
| `scripts/t463/astral-keys.mjs` | 列星面字符（emoji）垃圾键 |

**重写保命点**：zh-as-key + common 路由规则 + 三道闸 + en 懒 chunk + reload 式切换，这一整套是约束链；新栈无论用什么 i18n 库，`assert-i18n.mjs` 的检查语义（零硬编码 CJK、键集同构、manifest 对账）必须等价保留，否则 FR-149 断言链断。

---

## 3. a11y 资产

### 3.1 双主题 axe 断言面

| 项 | 数字/模式 |
|---|---|
| 核心 helper | `web/e2e/m8/support/a11y.ts`——`scanA11y`/`expectA11yClean`：tags=`[wcag2a, wcag2aa, wcag21a, wcag21aa, best-practice]`，默认 impact=**serious+critical**，违规 JSON（url/id/impact/target 前 5 节点/passes/incomplete）attach 到测试输出 |
| 调用规模 | `expectA11yClean` 104 处调用，分布在 **47/84 spec**（几乎每个功能 spec 尾部一带 axe 腿）；另有 3 spec 直用 AxeBuilder（theme-smoke、t366-webhooks、t372-trash-node） |
| 全量扫 | `m8/a11y-sweep.spec.ts`：31 路由×{light,dark}=62 面 + 登录页 2 面，timeout 600s；路由清单=console-m8 §1.3 全导航 + 深页 |
| 主题切换载体 | `localStorage['binflow-console-theme']` + `<html data-theme>` 属性断言；toggle 锚=`topbar-theme-toggle` |
| 双 locale axe | `m16/t464-i18n-bilingual.spec.ts` 尾腿（en 两页+zh 一页） |

### 3.2 断言模式（非像素）

- ADR-0029 决策 3：**交互断言制，禁截图/像素 diff**；样式面只做「根节点自有 token computed 存在性」（`--bf-sidebar/--bf-bg/--bf-surface-1/--bf-text/--bf-accent/--bf-shadow-1..3`）+ 切换=纯换值。
- 元素级钉腿先例：`repo-commands pre` tabindex=0 可聚焦（QA-1）、field-hint 链接恒下划线（QA-2）。
- 键盘面独立 spec（m8/keyboard、smoke、permissions 键盘腿）：树箭头/Shift+F10、表格行箭头、tablist、焦点陷阱 Esc。
- testid 约定：页面根 `<page>-page` 作为 settle 锚（如 `tree-page`/`repos-page`），axe include 可选 scope 到根锚。

---

## 4. 构建链脚本全清单（web/scripts/*.mjs + t463/）

| 脚本 | 行数 | 职责 | 链路位置 | 重写后保留 |
|---|---|---|---|---|
| assert-tokens.mjs | 98 | 编译期断言组件层零硬编码色值（ADR-0029）：CSS 全量（tokens.css 豁免）+ TSX sx/style 色值字面量（MuiProvider 豁免）+ TSX 禁 `var(--bf-色系)`（须走 theme.palette） | **`npm run build` 第 1 步** | 概念必须保留（新栈=Tailwind token 层的等价纪律，实现要重写） |
| assert-i18n.mjs | 174 | i18n 三道闸（见 §2.3） | **build 第 2 步 + lint 尾** | **必须保留**（语义原样） |
| relink-assets.mjs | 114 | vite build 后把三处 `/binflow/ui/assets/` 面（index.html、css url()、JS 预载 JOIN）改写到共享挂载 `/binflow/assets/`，自校验零残留 | **`npm run build` 第 4 步** | **必须保留**（/binflow/ui 纯 shell + dumb fallback 契约的守门人；若新栈能用 vite 原生 base/renderBuiltUrl 达成同效果可换实现，但验证逻辑必须等价） |
| wire-brand-assets.mjs | 109 | brand 静态面从 dist/brand/ 搬入 /binflow/assets/ 并按内容 sha1 指纹化（favicon/PWA/manifest），改写 html 引用，自校验 | **build 第 5 步** | **必须保留**（immutable 缓存契约） |
| gen-brand-assets.mjs | 226 | SVG 母版→PNG/ICO 派生器（playwright chromium canvas 渲染；favicon 16/32/48 + apple 180 + PWA 512） | 手动（换 logo 后） | 独立工具，保留 |
| anchor-audit.mjs | 493 | 锚册三方对账（src↔spec↔docs/design/console-ux.md §10）；`--ledger` 模式=qa 硬门（unregistered/broken=0、退役须登记） | 手动 + qa 流程 | **必须保留**（若重写延续 testid 锚制） |
| gen-pathmatch-fixtures.mjs | 74 | 解析 internal/auth/pathmatch_test.go 表→生成 `src/pages/security/pathmatch.fixtures.ts`；`--check` 漂移即红 | 手动/CI 挂钩 | 保留（Go↔FE 同源 fixtures） |
| mock-idp.mjs | 197 | 零依赖 OIDC IdP 模拟（discovery/authorize/token/jwks；fixture ssouser） | e2e 支撑服务（oidc-stepup 真武装腿） | 保留 |
| seed-m8/m9/m10.mjs（+.d.mts） | 385/442/359 | 见 §1.5 | e2e 种子 + QA 独立 CLI | 保留 |
| t463/{codemod,verify-extraction,astral-keys}.mjs | — | 一次性迁移工具 | 归档 | 可不迁移 |

**make console 链**（Makefile:47）：`cd web && npm ci && npm run build`（= assert-tokens → assert-i18n → vite build → relink-assets → wire-brand-assets）→ `rm -rf internal/console/dist/assets && cp -R web/dist/. internal/console/dist/` → `console-size`（gzip js+css 总量 vs 5MB 预算警告）。重写后这条链的**每一步顺序与自校验语义都必须等价**。

---

## 5. 锚册/parity 册与 e2e 的耦合

### 5.1 锚册（docs/design/console-ux.md §10，行 861–2724，占全文 2/3）

| 项 | 内容 |
|---|---|
| §10.1 命名规则 | kebab-case；页面根=`<page>`/`<page>-page`；动态段原值（`repos-row-<repoKey>`）；类段防碰撞（user/group 同名不合并）；四态缺省锚 `skeleton`/`error-card`+`error-retry`/`empty-state`/`toast`；锚按当前视图唯一（断言须 scope 页根） |
| §10.2/10.3 | 已落地锚批次清单（人写叙事：意图史+裁定） |
| §10.4 | 未落地白名单（4 条） |
| §10.5 | 路由重排锚保全映射（T-235 先例）：**M8 IA 重排 242 锚零改名**——「testid 锚不随路由改名」是成文决策（ADR-0029 决策 3），路径断言随路由改、锚断言不动 |
| §10.6 | 家族口径（前缀归一 `前缀-*`、掩蔽语义）+ 四桶（unregistered/retired/dead/broken）+ `--ledger` 硬门；退役总表现 111 条 |

### 5.2 实态数字（`node web/scripts/anchor-audit.mjs` 实跑）

- src 锚家族 **874**（落点 948）；spec 引用家族 **1056**（具体引用 **4218**）；册家族 1052 = registered 840 + 白名单 4 + 退役表 111。
- e2e 侧 `data-testid` 总引用 **4199** 处，覆盖 **76/84 spec**（唯一例外是纯 API/单测腿如 pathmatch-parity、go/nuget pilot 等）。
- 当前 unregistered ≈20+ 家族（全部来自在建 `web/src/pages/builds/`，T-512 未入册）；broken=1（`tree-children`）；dead=18。

### 5.3 parity 册（docs/design/console-artifactory-parity.md，544 行，v1.14）

N（导航）/M（弹窗）/D（抽屉）/L（列表）/F（反馈）/R（复制面）系列交互形态规格，每项带 Artifactory 置信度标尺；e2e 大量引用其为「形态锚」（如 t451 pager=parity §11.2 K67/T-437 冻结、t383/t384 表单链、t424 L2 行内）。**parity 册是栈无关的行为规格，重写不打破它；它是重写后行为的验收依据。**

### 5.4 重写打破量级

- **testid 即锚 = 断言地基**：若重写不保 testid，一次性打破 **~4200 处 spec 引用 / 874 个 src 家族 / 84 spec 中 76 个**——等价于全部 UI 断言重写。锚+wire 原文（错误消息 verbatim）+键盘行为是 e2e 的三根支柱。
- §10.5 先例已证明**换路由/换组件可以零锚改名**；新栈（shadcn/Radix）下 `data-testid` 仍是可挂属性，锚制可延续。建议：锚名与 §10 册原样迁入新栈，`anchor-audit.mjs` 改的只是 src 解析面（新增 testid prop 形态适配）。
- 附带耦合：`error-card`/`empty-state`/`toast` 等四态缺省锚、`copy-<field>`/aria-label 约定、`login-*`/`app-nav`/`session-user` 冻结锚（roles.ts 依赖）。

---

## 6. 现有性能实践（重写性能基线参考）

| 面 | 现状（路径+数字） |
|---|---|
| 路由级代码分割 | `src/main.tsx` 33 处 `lazy(() => import(...))`（每页一 chunk）+ en catalog 独立 chunk |
| 分页档位 | `src/components/Pager.tsx`：`PAGER_SIZE_OPTIONS=[20,50,100,200,1000]`，默认 **100**（docker n/audit/搜索同源家族页窗）；1000=AQL/audit 服务端上限；页窗映射 keyset 游标/AQL offset（深翻页规避 offset 扫描）；流式面末页未知=前沿+1 |
| 树懒加载 | 单层按需 loadDir（console-m8 §3.3 C2）；深链才整链展开；**TREE_LEVEL_CAP=300** 单层渲染上限+如实溢出注记（`ArtifactsBrowser.tsx:108,1740`） |
| 大目录列表 | 子表 `rows.slice(0, visible)` 客户端 load-more，PAGE=100 步进（`ArtifactsBrowser.tsx:104,486,1271`）；**无虚拟化库**（无 react-window/tanstack virtual）——当前是「分档+load-more」而非虚拟滚动 |
| 审计 | keyset cursor 分页，`AUDIT_PAGE_SIZE=100`（`src/lib/governance.ts:34`，服务端 limit 1..1000） |
| 搜索 | PAGE=100 页窗（`SearchPage.tsx:67,363`）；AQL builds 合成 limit 100 |
| 扇出治理 | E1 批量注水：整页恰一次 `GET /api/v1/storage/usage?include=counts`（全页请求 cap≤3，排序/过滤零再发，m9/usage-fanout 钉死）；N01 用户页单 E2 请求零逐用户扇出 |
| 多余请求族 | G30e 树浏览零 /api/search；G32 docker 树零 /v2/*；t494 纯浏览零下载计数 |
| 计时采集（record-only，非门） | `e2e/m8/support/timing.ts`（first-interactive+Long Tasks、树展开耗时）；t104-perf 冷载×3、1GB 上传 RSS<256MB、50 并发登录 |
| 体积预算 | make console-size：gzip js+css ≤5MB（PRD W37）硬警告 |

**基线结论**：重写（AG Grid + TanStack Virtual）在树/大列表上有机会超越现状（虚拟滚动替代 TREE_LEVEL_CAP+load-more），但必须以「不劣于」以下既有钉死项为准：单层懒加载、批量注水 cap、零多余请求族、Pager 语义（keyset/offset 页窗）、5MB 预算、t104-perf 三腿数字。

---

## 7. 给架构设计的保真清单（质量资产面）

1. **不可变契约**：/binflow/ui + /binflow/assets 双挂载、build 五步链及各自自校验、5MB 预算、CI e2e job 形态（fresh binary+seeds+workers=2）、`npm run dev/build/typecheck/lint/e2e` 入口名。
2. **必须等价重建**：i18n 三道闸语义、zh-as-key+common 路由、en 懒 chunk、reload 切换；assert-tokens 的零硬编码色纪律（Tailwind 化后的等价物）；anchor-audit（src 解析面适配新组件形态）。
3. **强烈建议原样保留**：全套 testid 锚名（874 家族）+ §10 册；e2e 种子脚本与 support helper；mock-idp；四态缺省锚；roles/loginAs 惯用法。
4. **重写即重做**（无法搬运）：76 个 spec 里约 4200 处锚引用中依赖 DOM 结构的选择器细节（MUI 特有层级）、axe 腿的 include scope、键盘腿焦点次序（Radix 焦点行为不同需重验）。
5. **风险量级**：锚不保 → 全套 UI e2e 失效（~423 用例中约 380+ 受影响）；i18n 闸不保 → FR-149 断言链断；relink/wire 不保 → 挂载契约破（懒路由白屏事故 D-104-1 复发）。

# 五、完备性批评（交叉核查）

## 完备性批评（四报告交叉核查，关键数字已对盘实测）

### 一、能力盘点缺口（30 节令清单 vs 四报告并集）
1. **Roles**：清单单列 Roles，但无任何报告显式裁定其形态（adminRole 闭集、无 Roles 管理页/无独立 API）——matrix 需一行「N/A/闭集」裁定，现缺。
2. **copy/move**：仅 API 报告登记「3 族 0 调用、无 UI」；页面/e2e 两面天然空缺未被显式记为条目，且树右键「复制」（剪贴板复制路径）与服务端 `api/copy` 的语义区分无报告点破，matrix 易混。
3. 其余 22 项（Login/Profile/Dashboard/Repo CRUD+detail/浏览+上传+下载+删除/Search/Users/Groups/Permissions/Tokens/Audit/Webhooks/Replication/Trash/Governance/Monitoring/Settings/Addons）三面均有落（Settings=redirect+未消费 `/v1/system/settings` 已登记）。

### 二、报告间矛盾（实测裁决附后）
1. **License GET 路径**：页面报告 `/v1/license` vs API 报告 `/api/system/license`——实测 `lib/addons.ts` 为后者，**页面报告错**。
2. **lazy 页面数**：架构报告自相矛盾（§1.3「约 40」vs §4.2「38」）且与页面/质量报告的 33 不符——实测 main.tsx `lazy(` = **33**。
3. **`<Route>` 条数**：架构报告「44 条显式」vs 实测 **57**。
4. **兼容重定向数**：页面报告称「5 条」，其自身表格及 main.tsx 实测均 **4 条**。
5. **testid 锚量级**：架构附录「242 锚」vs 质量报告 874 src 家族/4218 spec 引用——差一个量级，matrix 必须以质量报告为准。
6. **replication 资源标识**：页面报告用 `{id}`/`{name}`，API 报告用 `{key}`（PUT 启停/DELETE/run 三处）——重写需统一为 key。
7. **全局封锁端点**：页面报告「GET/POST `/v1/system/replications`」vs API 报告「POST `…/block`、`…/unblock`」。
8. **components 计数**：架构「12 tsx+4 css」/页面「13 文件」vs 实测 **12 tsx+3 css=15 文件**；BrandLockup 实在 BrandLogo.tsx 内，两报告清单均未单列。
9. **基线漂移**：架构/质量以「builds 未提交在途」为基线，页面报告称 74174a07 已落——git 实证 **74174a07 存在（页面报告正确）**；行数微漂（AuthConfig 674 vs 675、ToastContext 124 vs 123）同源。

### 三、重写风险盲区（四报告均未提）
1. **dev proxy 缺口**：vite 仅代理 `/binflow/api` 与 `/binflow/assets`——`/binflow/event/api/v1`（webhooks）与内容面 `/binflow/{repo}/{path}`（上传/下载/mkdir）未代理，`npm run dev` 下这些请求直撞 5173 即 404；重写 dev 工作流是否补代理/沿用缺口，无人登记。
2. **assistant-ui 无后端**：新栈指定 assistant-ui，但 API 报告证明 Go 侧零 LLM/chat 端点——与「不改 Go API」直接冲突，无报告裁定。
3. **新栈商业/授权面**：仅 5MB 预算被提，AG Grid enterprise 授权、Monaco worker 与 go:embed 的兼容（worker 资产经 relink-assets 改写后的路径正确性）未评估。
4. 其余指定盲区类别（embed 流水线、i18n 键量级 2054、锚量级 874/4218、品牌资产链）四报告均已覆盖，无补充。
