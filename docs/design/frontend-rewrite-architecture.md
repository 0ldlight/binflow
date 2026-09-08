# BinFlow 前端重写架构设计（frontend-rewrite-architecture）

> Phase 0 产出之三。30 节总令的架构落点；事实输入=frontend-rewrite-audit.md（四报告+裁决表）+ frontend-capability-matrix.md。
> 本文是 Phase 1~5 的唯一架构规范（single source of truth）；冲突时以总令绝对约束 > 本文 > 实现便利为序。

## 0. 不可变契约（重写全程红线，违反即停）

1. **挂载**：`/binflow/ui/**`（SPA shell）+ `/binflow/assets/**`（共享指纹资产）；`vite base=/binflow/ui/` 与 `BrowserRouter basename=/binflow/ui` 双真值同步。
2. **构建链五步语义**：assert-tokens → assert-i18n → vite build → relink-assets → wire-brand-assets——每步自校验语义等价保留（实现可换，语义不可降）。
3. **入口名**：`npm run dev / build / typecheck / lint / e2e`；root `make console`（npm ci → build → 拷贝 → embed → console-size 5MB 警告）。
4. **i18n 机制**：zh-as-key + common 路由 + en 懒 chunk + reload 切换 + assert-i18n 三道闸（零硬编码 CJK / 键集同构 / manifest 对账）。
5. **URL 深链不猝死**：树页签段+文件末段、builds/bundles 三视图、search ?q/mode/scope、?started= 消歧、?section= 直落——新路由结构保持这些 URL 形态。
6. **testid 锚制**：874 家族锚名原样迁入；四态缺省锚（skeleton/error-card+error-retry/empty-state/toast）与冻结锚（login-*/app-nav/session-user）不动。
7. **Go 零改**：不改 API/DB/认证/协议（D 类契约漂移只回填 fern 文档侧）。

## 1. 技术栈集成决策（含授权/体积/兼容裁定）

| 层 | 选型 | 裁定与理由 |
|---|---|---|
| 核心 | React 19 + TS + Vite + React Router（data router 模式 `createBrowserRouter`） | router 升 data 模式（loader/action 可选渐进用）；basename 契约不变 |
| UI | shadcn/ui + Radix + Tailwind v4 + lucide-react | shadcn 源码进仓（components.json + CLI 生成），无运行时包锁定；**token 双主题迁移**（§3） |
| 状态 | Zustand | 仅 UI 态五仓（§6）；服务态一律 TanStack Query |
| 服务态 | @tanstack/react-query | key 按域+cursor 建模（audit/outbox keyset、其余全量数组）；refetchInterval 复刻 10s/5s/7s 三轮询；全局 401 监听挂 QueryClient 层 |
| 表单 | react-hook-form + zod | **验证规则迁移事实源=audit §4 表单盘点**（18 张表的现行手写规则逐条转 schema）；secret 哨兵三态语义保留 |
| 表格 | ag-grid-community + 自研轻量 table | AG Grid **社区版特性集内**（无限行模型/虚拟滚动/列拖选——企业特性 server-side row model/区间选择禁用，规避授权）；Explorer/Audit/搜索结果用 AG Grid，Users/Groups/Webhooks/Settings 用轻量 table（§7） |
| 虚拟化 | @tanstack/react-virtual | 树虚拟化（替代 TREE_LEVEL_CAP=300+load-more）；AG Grid 自带行虚拟 |
| 代码查看 | monaco-editor（动态 import 单路由 chunk） | **worker 资产风险裁定**：vite `worker.format:'es'`+worker 构建产物落 assets；relink-assets.mjs 扩展扫描 worker URL 面（新增自校验腿——不通过即红） |
| 图表 | echarts（按需 echarts/core+用到的图） | Dashboard/Storage/监控面；按路由懒载 |
| AI 基座 | assistant-ui + @ai-sdk/react | **Phase 5 只搭壳**：ChatPanel/ToolResult/ConfirmCard 抽象+mock provider；零 Go 改动、不虚构端点（audit 盲区②裁定） |
| 退役 | @mui/material、@emotion/react、@emotion/styled | 归零（终验 MUI=0/Emotion=0） |

**体积预算分解**（5MB gzip 警戒位，make console-size）：主壳+路由分片 ≤1.2MB｜AG Grid 路由 ≤600KB｜Monaco 路由 ≤1.5MB｜ECharts 路由 ≤800KB｜en catalog 懒 chunk 独立。超标=优化分块而非豁免。

## 2. 目录结构（总令 §十八落地）

```
web/src/
├── app/            # 应用层：router/（data router 路由表+守卫）、providers/（Query+Theme+Toast+Auth+Confirm 装配）、shell/（AppShell/Sidebar/Topbar/CommandPalette 骨架）、config/（常量/权限位表）
├── components/
│   ├── ui/         # shadcn primitives（Button/Input/Dialog/Drawer/Command/…）
│   ├── layout/     # 设计系统组件（PageHeader/DataToolbar/四态容器/确认层）
│   ├── artifact/ repository/ search/ security/ audit/ governance/ monitoring/ ai/   # 领域组件
├── features/       # 业务逻辑单元（hooks+api glue+领域 store；页面薄）
│   ├── artifacts/ repositories/ search/ security/ audit/ governance/ monitoring/ builds/ bundles/ settings/
├── pages/          # 路由页面（薄组装）
├── lib/
│   ├── api/        # API client 三入口一信封（§5）+ toApiError 折衷器平移
│   ├── query/      # queryClient + key 工厂 + 默认 staleTime 策略
│   ├── auth/       # session/step-up grant（fragment 消费/sessionStorage pending 平移）
│   ├── format/     # 日期双 locale/sha256 流式（FIPS 转录平移）/字节单位
│   └── utils/
├── hooks/          # 跨域通用 hooks
├── stores/         # Zustand 五仓（§6）
├── i18n/           # 原样迁移（内核/locales/manifests）
└── styles/         # Tailwind 入口 + tokens（§3）
```

## 3. Design Tokens 与主题（Tailwind 化）

- `styles/tokens.css` 保留为**唯一色值层**（值自现役 tokens.css 双主题原样迁移）——Tailwind v4 `@theme` 消费 CSS vars：`--color-surface-1: var(--bf-surface-1)` 式桥接。
- 双主题机制保持 `<html data-theme>` + `localStorage['binflow-console-theme']`；Tailwind dark 变体绑定 `[data-theme="dark"]` selector（非 class 策略——与现存储约定零迁移成本）。
- **assert-tokens 等价纪律**（新栈版）：TSX/CSS 禁硬编码色值（Tailwind 语义 token 白名单豁免）；`var(--bf-*)` 仅 tokens.css 与 styles/ 桥接层可用——脚本重写，闸语义不降。
- 视觉语言执行总令 §二十四：dense/calm/technical；信息密度对标 GitHub/Linear；禁大卡片堆叠/夸张渐变/装饰图形。

## 4. 布局与信息架构（总令 §七/§八）

- AppShell = 可折叠侧栏（Core/Operations/Security/Administration 四分组+badge+权限可见性）+ Topbar（全局搜索/command palette 入口/主题/语言/用户/AI）+ Main。
- 侧栏 IA（页面路由全部保持现 URL——IA 重组=侧栏分组重组非路由重组）：
  - **Core**：Dashboard / Artifacts（Explorer）/ Repositories / Search
  - **Operations**：Builds / Bundles / Replication / Webhooks / Trash
  - **Security**：Users / Groups / Tokens / Permissions / Auth Config / Audit
  - **Administration**：Governance（Quotas/GC/Backup/Migration）/ Monitoring（Storage/Status/Logs/System）/ Settings（**新设**）/ System（License+Addons）
- 应用⇄管理双模式概念并入四分组（去掉模式切换——分组即模式，权限门控可见性替代）。

## 5. 数据层（四段式）

```
lib/api/（三入口一信封：apiRoot=/binflow/api、eventRoot=/binflow/event/api/v1、content=/binflow/{repo}/{path}
         + toApiError 三形态折衷器 + X-BinFlow-Console 头 + silent401 约定）
  → lib/query/（key 工厂：['repo',key] / ['audit',filters,cursor] / ['tree',repo,path]…；QueryClient 默认 retry=1、refetchOnWindowFocus=false）
    → features/*/hooks（useRepositories/useRepository/useArtifactTree/useAuditEvents/useUsers…）
      → 组件（useQuery/useMutation + optimistic 策略仅限局部）
```
- 轮询：replication 10s / migration 5s / logs 7s → `refetchInterval` 等价复刻（stale 保留语义）。
- 401 全局监听：QueryClient 的 QueryCache onError + fetch 层拦截（已认证态 401→toast+重登），silent401 豁免集平移。
- 上传（XHR 进度+abort）与下载（流式 sha256 tee）保留原生实现于 lib/api/content.ts。

## 6. Zustand 五仓（仅 UI 态）

`ui-store`（侧栏折叠/主题面板态）｜`command-palette-store`（开闭/上下文注入）｜`session-store`（当前用户/权限位快照——与 AuthProvider 并轨）｜`preferences-store`（列选/行高/favorites——**localStorage key 原样迁移**：`binflow-console-cols-*`/`bf-tree-*`/recent-searches）｜`ai-store`（drawer 开闭/上下文）。服务数据入仓=违规。

## 7. 表格与虚拟化策略

| 面 | 实现 |
|---|---|
| Artifact Explorer | 左树 TanStack Virtual（行虚拟+懒单层加载语义保留）+ 右 AG Grid（children/大清单，无限行模型）；多选+批量 copy/move/delete |
| Audit | AG Grid + keyset 页窗映射（游标链 chainRef 语义平移） |
| Search 结果 | AG Grid（AQL offset/limit 尾缀链重写语义平移） |
| Users/Groups/Webhooks/Tokens/Repo 列表/Settings | 轻量 table（自研 `<DataTable>`——排序/过滤/分页 useClientPager 语义保留，档位 [20,50,100,200,1000]） |
| Inspector | 列表+右栏 inspector 形态（总令 §十二）：Artifacts/Users/Groups/Webhooks 先行 |

## 8. Command Palette 与全局搜索（Phase 4）

- Cmd/Ctrl+K palette（shadcn Command）：导航/动作/主题/语言/AI 入口；管理过滤框语义并入。
- 全局搜索框：Topbar 常驻（recent-searches 迁移），Enter→/search?q=；Phase 4 评估 artifactsearch 快速结果下拉（解锁面，后端在）。

## 9. i18n / e2e / 构建链迁移细则

- **i18n**：内核+目录+manifest 原样搬入 src/i18n/；组件消费面重写时键集用 regen-catalogs 重算（存量 2,054 键保值迁移——en 译值逐键保留）；三道闸挂 build/lint 不动。
- **e2e**：种子/mock-idp/roles/support 全保留；spec 断言按「锚不动、选择器细节重做」原则迁移——MUI 结构依赖的选择器重写为新 DOM；键盘腿 Radix 焦点重验；a11y 47 spec axe 腿+31 路由双主题全扫继续（Radix 组件 a11y 基线更优，预期收敛不劣化）。
- **构建链**：relink-assets 扩展 Monaco/AG Grid worker 资产扫描腿；wire-brand-assets 不动；assert-tokens 重写为 Tailwind 等价闸；console-size 预算分解入 §1。
- **dev 体验**：vite proxy 增补 `/binflow`（整前缀转发 8080——修复 event/内容面 dev 404 盲区，零后端改动）。

## 10. 阶段计划与验收门（映射总令 §二十三）

| 阶段 | 内容 | 验收门 |
|---|---|---|
| P1 基座 | 依赖装配+tokens/Tailwind+shadcn primitives+AppShell 骨架+router+Query/Zustand/form+api 层+构建链适配 | 四门绿（typecheck/lint/build+console-size）+dev/build 可跑+挂载契约 e2e 冒烟（console-smoke 迁移版绿） |
| P2 核心 UX | Login/Shell/Dashboard/Repository 全套/Explorer（第一优先）/Detail/Search | 核心流 e2e 绿（登录/树/上传/下载/搜索深链族）+锚迁移过账（anchor-audit --ledger=0） |
| P3 管理面 | Users/Groups/Tokens/Permissions/AuthConfig/Audit/Webhooks/Replication/Governance/Monitoring/Settings/Builds/Bundles | capability matrix 全行绿+三角色 RBAC 走查 e2e+i18n 双语全量+双主题 axe |
| P4 高级 UX | Palette/全局搜索/inspector/键盘全集/批量动作/解锁面（copy/move UI/outbox/keypair/Settings） | 键盘 spec 族绿+解锁面 e2e 新写 |
| P5 AI 基座 | assistant-ui 壳+上下文模型（route/artifact 感知）+tool result/confirm 渲染+mock provider | AI drawer 可开合+上下文注入正确+零后端虚构 |
| 终验 | MUI=0/Emotion=0 依赖清零 | `jq` package.json 零命中+全量 e2e+四门+embed 冒烟（make console→serve→/binflow/ui/ 200） |

## 11. 风险登记（audit 盲区承址）

| # | 风险 | 缓解 |
|---|---|---|
| R1 | Monaco worker × relink-assets 路径改写 | 构建链扩展腿+自校验（P1 首验项） |
| R2 | AG Grid 体积/授权 | 社区特性集约束+按路由分块+预算分解 |
| R3 | 键盘/焦点行为 Radix 差异 | P2 起键盘 spec 前置重验，差异清单回锚册 |
| R4 | 锚迁移遗漏 | anchor-audit --ledger 硬门全程挂 P2~P4 |
| R5 | i18n 键集漂移 | assert-i18n 闸持续在链上；regen 保值迁移 |
| R6 | assistant-ui 无后端 | Phase 5 mock provider 边界写入组件契约（禁真请求） |
| R7 | 5MB 预算突破 | console-size 分解预算+超标即优化分块 |
| R8 | e2e 全量红窗（新旧 DOM 断层期） | 分阶段灰度：P2 起旧 spec 逐域迁移，`web/e2e-legacy/` 归档未迁移面，P3 末清零 |
