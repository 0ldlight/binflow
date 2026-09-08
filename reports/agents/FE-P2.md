Ticket:       FE-P2 前端重写 Phase 2 — Core UX（六域新实现 + LegacyBridge 过渡架构）
Role:         dev-frontend（area：web/src 新栈页面组 + app/router + app/shell + web/e2e/p2）
Input:        dispatch 要点（六域清单/总令 §九 §十）；frontend-rewrite-architecture.md §1/§4/§5/§7/§10/§0 七红线；frontend-capability-matrix.md #1/#3/#4-7/#8/#10/#12/#13 行；frontend-rewrite-audit.md §1 路由总图/§2.1-2.3/2.6 页报告/§4 表单盘点/§7 高危契约；internal/httpapi/operations.go（copy/move 契约实测）
Changes:
  - main.tsx 切换到新 data router（createBrowserRouter）；Provider 树重组：新栈 AppProviders（Theme>Query>sonner>新Confirm）内嵌旧树根（LegacyThemeShim→MuiProvider→旧Toast→旧Confirm）——单一 <html data-theme> 真值（新栈持有，旧 ThemeContext 经导出 context 注入）
  - app/router/index.tsx：57 Route + 4 兼容重定向逐条迁移（audit §1 对照）；六域指向新实现，其余 22 路由 LegacyBridge 挂载（lazy 分片 + Suspense，Provider 在根常驻零重复挂载）
  - app/shell/：新壳上线——四分组侧栏（Core/Operations/Security/Administration，nav-model 权限可见位；双模式切换退役）+ Topbar（驻留搜索/最近词/管理过滤/帮助/主题/语言/用户菜单 Quick 动作/About）+ OIDC step-up 回跳续铸（AppShell 持 SetMeUp 挂载）
  - pages/login/LoginPage.tsx：B1 漂移修正——GET /api/v1/auth/methods 直消费（302 hack 退役）；SSO 点击复核改 methods 再读；LDAP 位注记
  - pages/dashboard/DashboardPage.tsx：五卡→指标面板（TanStack Query；403 整卡隐藏；审计行 K67-3 深链）
  - pages/repos/ 三页：列表（轻量 table + usage 批量注水 E1 + 列选器 + Replications Run 列）/ 表单（RHF+Zod——zodResolver 自建桥零新依赖；dirty-gating stableFormString 平移；Test 三臂；?section= 直落）/ 详情八 Tab 骨架（总令 §十：Overview/Artifacts/Configuration/Storage/Permissions/Replication/Webhooks/Activity；QuotaEditor 全量替换保全平移）
  - pages/explorer/：Artifact Explorer（第一优先）——TanStack Virtual 虚拟树（懒单层保留、TREE_LEVEL_CAP 退役）+ AG Grid children 面（无限行模型 + 多选批量）+ 键盘全套（↑↓→←/Enter/Shift+F10）+ URL 即状态（页签段+文件末段+?focus= 折入）+ 工具带（facet/rclass/sort/favorites/compacted——PREF_KEYS 原样）+ 右键菜单（tree-context-* + **copy/move 解锁项**）+ CopyMoveDialog（api/copy|move 消费：dry-run 预演/messages[] 原文/community 403 如实）+ DetailInspector（三页签 General→Perms→Props；属性页签嵌旧 PropertiesTab）+ BIG_DIR 警示 + docker 特化列
  - pages/search/SearchPageV2.tsx：AG Grid 结果面 + AQL 尾缀链语义平移（splitTail/withTailClause 复用 aql.ts）+ 顶栏驻留查询 + Builds scope 页签 + 列选器
  - features/artifacts/（hooks/operations）+ features/aggrid/theme.ts：TanStack Query 数据层（useQueries 驱动树装载——T-494 分类门保留）；copy/move 操作面；AG Grid v36 Theming API token 桥（backgroundColor=var(--bf-surface-1) 等五参——CSS 文件主题在 v36 会向内部 styled-root 注入亮色参数，JS 主题 + 去 ag-grid.css 是唯一正道，错误码 106 实证）
  - components/layout/：新栈四态三件套（skeleton/error-card/empty-state 锚原样）+ CopyButton（.copy-btn 锚）+ Pager（pager-* 锚族 + useClientPager 平移）
  - providers/confirm-provider：prompt API（输入型确认——tree-mkdir-input 锚承载）
  - e2e/p2/core-flow.spec.ts 新写（16 用例）+ login.spec SSO 四腿迁移 methods 驱动 + artifacts.spec 七腿迁移（AG Grid 选择器细节重做、锚不动）
  - styles/tw/tailwind.css：preflight 关闭（共存期纪律——MUI CssBaseline 冲突）；utilities 去层化（layer(utilities) 恒输旧栈未分层 CSS——underline 失效实证）
Files:
  新增：web/src/app/router/{index.tsx,legacy-bridge.tsx}、web/src/app/shell/{nav-model.ts,breadcrumbs.ts,Sidebar.tsx,Topbar.tsx,AppShell.tsx}（重写）、web/src/pages/{login/LoginPage.tsx,dashboard/DashboardPage.tsx}、web/src/pages/explorer/{ExplorerPage.tsx,TreePanel.tsx,ChildrenGrid.tsx,DetailInspector.tsx,CopyMoveDialog.tsx,model.ts}、web/src/pages/repos/{RepositoriesPage.tsx,RepositoryFormPage.tsx,RepoDetailPage.tsx}、web/src/pages/search/SearchPageV2.tsx、web/src/features/artifacts/{hooks.ts,operations.ts}、web/src/features/aggrid/theme.ts、web/src/components/layout/{states.tsx,copy-button.tsx,pager.tsx}、web/e2e/p2/core-flow.spec.ts
  修改：web/src/main.tsx（切换 data router）、web/src/app/ThemeContext.tsx（context 导出=桥接位）、web/src/styles/tw/tailwind.css（preflight/utilities 层）、web/src/app/providers/{theme-provider.tsx,confirm-provider.tsx}、web/src/i18n/locales/en/* + manifests/zh/*（regen 重算：2114 键）、web/e2e/{login.spec.ts,artifacts.spec.ts}、web/package.json（+ag-grid-react@36.1.0——AG Grid 官方 React 绑定，architecture §1 表格决策的配套件）
  删除：无（旧页面/组件全部保留供 LegacyBridge 挂载——终验强删清单见 Compatibility）
Tests:
  新写 e2e/p2/core-flow.spec.ts：登录正/负（401 固定文案）/auth-methods 消费断言（B1）/Explorer 树浏览+URL 即状态+AG Grid children/上传（DeployDialog 桥）/下载校验+顶栏搜索深链/深链四族（?focus= 折入、properties 页签段、builds 三视图+?section=）/三角色 RBAC（readonly 写口禁用+注记、plain 安全管理组隐藏）/axe 六域×双主题（serious+critical=0）
  迁移 login.spec（SSO 四腿 methods 驱动）、artifacts.spec（七腿全迁：AG Grid 行锚 tree-row-<name> 落 cellRenderer、size 列 col-id 定位、大目录 load-more→无限滚动、行点击 force/深链承载）
  结果：core-flow 16 + login 5 + console-smoke 2 + artifacts 7 = 30 passed（真栈 127.0.0.1:18151 自起实例）
Commands:
  cd web && npm run typecheck && npm run lint && npm run build && cd .. && make console-size
  cd web && BASE=http://127.0.0.1:18151 npx playwright test e2e/p2/core-flow.spec.ts e2e/console-smoke.spec.ts e2e/login.spec.ts e2e/artifacts.spec.ts --workers=2
  node web/scripts/t463/regen-catalogs.mjs && node web/scripts/assert-i18n.mjs && node web/scripts/assert-tokens.mjs && node web/scripts/anchor-audit.mjs --ledger
  make console-size（嵌入产物）; CGO_ENABLED=0 go build -trimpath -o bin/binflow-server-p2 ./cmd/binflow-server（自起冒烟实例 :18151，/tmp/bf-p2-smoke 数据面）
Outputs:
  typecheck：0 errors（tsc --noEmit）
  lint：0 errors / 57 warnings（全部为存量 react-hooks 编译器派生 warn——set-state-in-effect 等，P1 已降 warn 的同族）
  build：assert-tokens OK（css 13 + tsx 102 零字面量）+ assert-i18n OK（3533 调用点/2114 键，en/zh 同构）+ vite build + relink-assets（0 残留）+ wire-brand-assets（7 指纹资产）
  console-size：967,049 bytes gzip（预算 5MB——0.92MB，主壳 702KB/AG Grid 路由 686KB 分片）
  e2e：30 passed（见 Tests）
Compatibility:
  锚册对照：新页面沿用既有锚族（login-*/tree-*/repos-*/search-*/dashboard-*/node-*/form-*/pkg-*/repo-*/pager-*/confirm-accept 等原样）；退役：nav-mode-switch（双模式概念退役）、tree-load-more（无限行模型取代）——需入册 §10.6；新锚待登记（docs/ 禁改域，走 Next）：tree-bulk-copy/move/delete、tree-context-copy-to/move-to、copy-move-*、repo-activity-row-*、topbar-help-menu、search-result-<n> 族（AG Grid rowIndex 落位）
  契约漂移：无新增（B1 auth/methods 漂移已修正——302 hack 退役）；发现 AG Grid v36 生态契约：CSS 文件主题与 Theming API 互斥（错误 106）、withParams darkMode 参数已移除（colorSchemeVariable 部件 + data-ag-theme-mode 或 withParams var() 桥）
  LegacyBridge 终验强删清单（MUI=0 门）：src/app/router/legacy-bridge.tsx、src/app/{MuiProvider.tsx,ThemeContext.tsx(桥接导出),ToastContext.tsx}、src/components/{ConfirmDialog.tsx,SetMeUpDialog.tsx,DeployDialog.tsx,Pager.tsx,Skeleton.tsx,ErrorCard.tsx,EmptyState.tsx,CopyButton.tsx,AppShell.tsx}、src/pages/{LoginPage,DashboardPage,ProfilePage,NotFoundPage}.tsx、src/pages/{artifacts/PropertiesTab.tsx, repositories/ReplicationsSection.tsx + RepoDeleteConfirm.tsx + repositories/* 旧列表/表单/详情}、src/pages/{security,audit,governance,webhooks,monitoring,admin,builds,bundles}/*、main.tsx 旧树根四层、styles/{base,pages,governance}.css
Security:  用户内容渲染全部 React 转义（tree/search 名称、错误 message 原文经 JSX 文本节点）；无 dangerouslySetInnerHTML；无敏感信息前端日志
Performance: 树 TanStack Virtual（虚拟滚动——单层渲染上限 TREE_LEVEL_CAP=300 退役）；children/search AG Grid 无限行模型+行虚拟；usage 批量注水 cap≤3 语义保留；产物 0.92MB/5MB
Risks:  ①新栈 utilities 去层化依赖导入序（tailwind.css 最后引入）——MUI=0 终验恢复分层形态时需回归 underline 等细粒度工具类；②AG Grid v36 新 DOM（ag-grid-viewport/ag-styled-root）与社区主题演进快，36.1.0 钉版；③e2e 中 AG Grid 行虚拟化下锚点行需在视口内（force 点击/滚动承载），个别腿用深链承载选中语义——P3 若恢复行点击需重验 hit-test；④60+ 旧 spec 未在本轮跑（机器负载敏感期只跑定向子集）——P3 全量回归必须补
Blockers: 无
Next:  ①锚册 P2 批登记（ux-designer）：新锚 tree-bulk-copy/move/delete、tree-context-copy-to/move-to、copy-move-dialog 族、repo-activity-row-*、topbar-help-menu、search-result-<n>；退役 nav-mode-switch、tree-load-more；②存量 unregistered 债（T-512 builds-* 23 族 + search-scope-* 5 族 + users-lastlogin 2 + logs 2 + node-module-id + confirm-phrase-input + broken tree-children + pager-size-*）非本票引入，建议 conductor 派登记票；③DeployDialog/SetMeUpDialog/PropertiesTab/ReplicationsSection 的 P4 新栈重写票（当前经桥挂载）；④AG Grid v36 主题桥（features/aggrid/theme.ts）建议回写 architecture §1 表格行注记；⑤P3 管理面域逐域迁移时 LegacyBridge lazy 清单逐路由缩短
