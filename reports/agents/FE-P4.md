# FE-P4 工作日志 — 前端重写 Phase 4（高级 UX + MUI 清场）

Ticket:       FE-P4 前端重写 Phase 4 — Advanced UX + MUI 清场（P1~P3 已落库 c7a56465；本票 = dispatch 两段 A/B）
Role:         dev-frontend（area = web/src 全域收尾 + web/e2e/p4 新面；终验前哨）
Area:         A：app/shell（CommandPalette/Topbar）+ explorer/repos 页面组；B：components 四件 + app 壳层 + 构建链
Input:        docs/design/frontend-rewrite-architecture.md §0/§8/§10-P4 门；frontend-rewrite-audit.md §3 能力 13-15（顶栏搜索/管理过滤/快捷键）+ §2.1 SetMeUp/Deploy 行为 + §2.17 reindex 七端点 + §3.2 copy/move/archive 解锁清单；console-ux.md §10.9 P4 批锚册（本票登记）；契约实测：internal/httpapi router.go/archive.go/search_ui.go + handlers（helm/yum/deb/conan reindex、archive/download 必填 archiveType、artifactsearch/quick body）
Changes:
  A 段（高级 UX）：
  1. Command Palette（A1）：新组件 app/shell/CommandPalette.tsx（⌘/Ctrl+K 开合、cmdk 过滤）；导航四分组全条目（NAV_GROUPS 同源、权限门控）/ 动作组（建仓三预选·上传·新建用户/组/权限——仅全量 admin）/ 偏好组（主题切换·语言切换 setLocale+reload）/ AI 占位条目（P5 接管，恒禁用）；管理资源过滤语义并入（/admin 条目全量进导航组，Topbar admin-filter 原样保留）；stores/command-palette-store 接线（open/closePalette）；Topbar ⌘K 让位（快捷键独占权归 palette——双监听对撞实证后收口），`/` 聚焦当前模式的框不破
  2. 全局搜索快速结果（A2）：Topbar 输入 ≥2 字符防抖 300ms 打 POST /api/artifactsearch/quick（lib/api.ts 新增 quickArtifactSearch，silent401 静默、AbortController 在飞取消）；top 5 制品行进下拉首段（repo/path mono），点击深链 /artifacts/{repo}/{path}（文件末段即选中态）；Enter 提交搜索/recent 通道/Esc 两段语义零变化；kbd 提示 ⌘K→/
  3. 键盘全集（A3）：ChildrenGrid 补 onCellContextMenu（P2 换 AG Grid 时右键腿漏接——m8 契约复修）；RepoDetailPage 八 Tab 条补 tablist 方向键链（DetailInspector 同款）；confirm-provider prompt 对话框补「打开即聚焦取消」（§3.5）；ExplorerPage 展开集序列化 bug 修复（join('\n')/split('\n') 复合集错切——≥2 展开键时二级树展开永久失效，P2 遗留）
  4. 暗色打磨（A4）：LoginPage 根补 bg-background/text-foreground（透明底上 muted 色对比 serious——双主题 axe 复扫发现的真缺口）；DetailInspector 下载伴随 Popover 补 aria-label（aria-dialog-name serious）
  5. 解锁面（A5）：RepoDetailPage 新增高级动作卡（repo-reindex-card/run/result/note）——helm/yum(rpm)/deb(debian)/conan 四型呈现（lib/repos.ts 新增 reindexRepository，URL 分岔按 router.go 实测：/helm/{r}/reindex、/yum/{r}、/deb/reindex/{r}、/conan/{r}/reindex）；501 如实 toast；generic 等四型外不渲染
  6. 批量归档（A6）：Explorer 多选工具条 tree-bulk-archive（恰一目录时启用——API 单路径语义不虚构多选合并）+ 目录/仓节点右键「下载归档（zip）」（tree-context-archive）；pages/artifacts/lib.ts 新增 downloadArchive（?archiveType=zip 必填——空值 400 wire 实测；文件名取 Content-Disposition）
  B 段（MUI 清场）：
  7. 四件迁新栈：SetMeUpDialog（MUI Drawer→Radix 右向 sheet，clamp(480,50vw,800) 宽 + box-border 修边界、smu-* 全锚保真、OIDC step-up 双腿原样）；DeployDialog（MUI Dialog→Radix Dialog，720px 版、deploy-* 全锚保真、队列泵/XHR abort/E-11 错误语义零改）；PropertiesTab（TextField/Table/Tooltip→.field 族+原生表，node-props-* 全锚保真）；ReplicationsSection（Paper/Switch/Checkbox/TextField→Tailwind+原生控件〔ToggleSwitch role=switch〕，repl-* 全锚保真、Test/重建语义零改）
  8. 共享件：ConfirmDialog 迁 Radix（confirm-dialog/cancel/accept + .modal 类 + confirmDisabled() bump 缝逐字保真；onOpenAutoFocus 让位后聚焦取消钮）；ToastContext 降为 sonner 桥（useToast API 零改点——AuthContext/RepoDeleteConfirm 消费面零改动；success 5s/error 常驻/动作链接语义保持；Toaster 开 closeButton）；旧 CopyButton/EmptyState/ErrorCard/Skeleton 消费面切至 layout 新栈件后删除
  9. 退役：legacy-host.tsx / MuiProvider.tsx / ThemeContext.tsx / muiAtoms.ts / 旧五组件共 9 文件删除；BrandLogo 切新栈 theme provider；main.tsx 树根收为 AppProviders > ConfirmProvider > RouterProvider
  10. 依赖清零：package.json 删 @mui/material + @emotion/react + @emotion/styled（package-lock 同步）；assert-tokens.mjs 移除 MuiProvider 豁免层（TSX 面零文件级豁免——闸语义只升不降）
Files:
  新增：web/src/app/shell/CommandPalette.tsx；web/e2e/p4/{command-palette,quick-search,mui-migration,unlock-reindex-archive}.spec.ts
  修改（src）：app/shell/{AppShell,Topbar}.tsx；app/providers/confirm-provider.tsx；app/ToastContext.tsx；components/{ConfirmDialog,DeployDialog,SetMeUpDialog,BrandLogo}.tsx；components/ui/sonner.tsx；components/layout/states.tsx；lib/{api,repos}.ts；main.tsx；pages/artifacts/{PropertiesTab,lib}.ts*；pages/explorer/{ChildrenGrid,DetailInspector,ExplorerPage}.tsx；pages/login/LoginPage.tsx；pages/repos/{RepoDetailPage,RepositoriesPage,RepositoryFormPage}.tsx；pages/repositories/ReplicationsSection.tsx；styles 相关零（ dialogs.css 原样续用）
  修改（配置/锚册/e2e 存量）：web/package.json + package-lock.json；web/scripts/assert-tokens.mjs；docs/design/console-ux.md §10.9（P4 批 12 名锚入册——P2/P3 收编先例同款）；i18n：en/{console,repositories,artifacts,search,common}.ts + manifests/zh 同域（净 +26 键：palette 族/reindex 族/archive 族/quick-results；清 orphan 5 键）；e2e 存量 6 spec 选择器随契约翻新（m8/keyboard·setmeup-deploy·artifacts-tree、m9/console-fr82、m14/t404、repositories）
  删除：web/src/components/layout/legacy-host.tsx；web/src/app/{MuiProvider,ThemeContext}.tsx；web/src/lib/muiAtoms.ts；web/src/components/{CopyButton,EmptyState,ErrorCard,Skeleton,Pager}.tsx（合计 9 文件，13 个 @mui import 面 + legacy-host = 14 残留文件清零）
Tests:
  新 spec 四件（e2e/p4/）：command-palette（4 腿：⌘K 开合往返 + `/` 不破 + 导航过滤 Enter 落位 + 动作/主题/AI 禁用 + RBAC 零动作组/管理条目 + axe 双主题）；quick-search（3 腿：≥2 字符防抖命中行深链树页 + Enter 仍是提交搜索 + <2 字符零快速段）；mui-migration（4 腿：SetMeUp sheet 焦点圈进/Tab 方向键/回焦 + Deploy 候选回显/拖拽区/axe + Properties 加删过危险确认 + Replications ?section 直落三态/表单字段/预留位禁用）；unlock-reindex-archive（2 腿：generic 负向 + helm 四型卡运行/结果行；归档 zip 落盘事件 + toast + 批量钮——pro license 自铸〔--addons repo-operations,helm——allowlist 语义实测〕+ 租约文件协调 + folder_download 总闸探针 skip 留痕）
  既有键盘/四态回归：m8/keyboard（树方向键·表格 Enter·tablist·对话框陷阱·badge axe）全绿——其中 3 腿按 P2 后契约翻新（八 Tab 序/AG Grid 单元焦点形态/prompt 聚焦取消）
  axe 双主题全路由：m8/a11y-sweep 全绿（serious+critical=0——login 底色与 download popover 名两处修补后达成）
  终批（当前 build 实跑）：batch A 53 passed + 1 skipped（m8 setmeup OIDC 环境腿，既有 skip）+ batch B 29 passed = 82 绿 0 红
Commands:
  cd web && npm run typecheck && npm run lint && npm run build && node scripts/anchor-audit.mjs --ledger
  make console-size
  npm ls @mui/material @emotion/react @emotion/styled；grep -rn "@mui\|@emotion" src/（0 命中）
  BINFLOW scratch 实例（127.0.0.1:18170，folder_download.enabled 配置）+ BASE=... npx playwright test e2e/p4/ e2e/p2/core-flow.spec.ts e2e/m8/{keyboard,shell,setmeup-deploy,artifacts-tree}.spec.ts e2e/m9/console-fr82.spec.ts（53+1s）；e2e/m10/properties-matrix e2e/m16/t447-props-download e2e/m14/t404-replication-crud e2e/m8/a11y-sweep e2e/rbac e2e/repositories e2e/console-smoke e2e/login（29）
Outputs:
  typecheck：tsc --noEmit 零输出（过）
  lint：0 errors / 45 warnings（P3 存量 44 同类 react-hooks set-state-in-effect 警告 + 本票 quick-search effect 1 条同类；零 error）
  build：assert-tokens OK（css 12 + tsx 93——FE-P4 起零文件级豁免）+ assert-i18n OK（3053 调用点 / 2295 键同构）+ vite ✓ built 5.77s + relink-assets verified + wire-brand-assets 7 件
  console-size：console SPA payload (gzip, js+css): 876,407 bytes（<5MB 预算 17.5%）
  npm ls @mui/material @emotion/react @emotion/styled → └── (empty)；src @mui/@emotion refs = 0
  anchor-audit --ledger → PASS（unregistered/broken 双零，retired 全在表）
  e2e 终批：82 passed / 0 failed / 1 skipped（环境腿）
Compatibility:
  锚册对照：§10.9 P4 批 12 名新锚入册（palette-root/-input/-item-<id>/palette-ai；topbar-search-quick(-item-<i>)；repo-reindex-{card,run,result,note}；tree-bulk-archive；tree-context-archive〔tree-context-<id> 族成员〕）——按 P2/P3「FE 批入册、conductor 收编」先例直接落 console-ux.md §10.9，行级诉求即该节本身；empty-state 冻结锚载体自旧组件移至 layout/states（prop 缺省值形态保持扫描可见性——ledger A3 修补）
  契约漂移（实测回填诉求，无行为虚构）：
  D-P4-1 archive/download 的 archiveType 必填——空值 400 "Unsupported archive type: '' of possible types : 'zip, tar, tar.gz, tgz'"；FE 固定 zip。fern 文档若标可选请回填必填语义
  D-P4-2 archive/download 前置三闸：pro license + repo-operations addon（allowlist 未列名即 400 "not named in the license addon allowlist"）+ binflow.yaml folder_download.enabled（缺省 off→403 "Download Folder functionality is disabled."）——三层前置建议入 fern/部署文档
  D-P4-3 建仓 pro 门按包型 addon 放行（helm 等 allowlist 制）——bf license issue --addons 语义，e2e 自铸路径留痕
  D-P4-4 artifactsearch/quick 端点「API 在而未用」解锁：body {searchTerm, repos?} → {results: fileInfoBody[]}，认证面（匿名 401）——与 aql.md §14.5 一致，无漂移
Security:     XSS 面：palette/quick-results/reindex 文案全部 React 文本节点渲染（无 dangerouslySetInnerHTML）；quick 命中行 repo/path 为服务端数据经 JSX 转义；token 明文面板语义原样（SetMeUp 一次性显示 + 服务端只存指纹）；敏感信息不落前端日志（quick-search 失败静默不 toast 不 console）
Performance:  palette = cmdk 客户端过滤（条目 <60，零虚拟化诉求）；quick-search 防抖 300ms + AbortController 在飞取消 + silent 静默失败；树/表大数据面零改动（既有 AG Grid 无限行 + 树虚拟化保持）；bundle：MUI 清出后主壳 index 529KB gzip 170KB（theme 分片 312KB gzip 为共享 chunk，含 AG Grid/Monico 桥）；console-size 0.88MB
Risks:
  R1 树页 children 网格在短视口（720p 缺省）下 flex 高度链塌缩（单行网格尤甚）——P2 布局债，P4 e2e 以 1600x900 视口绕行实证；建议独立布局票（main overflow-y-auto 与 flex-1 链的整改涉及全页面滚动语义，不宜顺手改）
  R2 m8 setmeup「回根不重复自动选中」前提（T-492 注记）在快实例上不可复现——auto-select 与 click 竞速，P3 基线二进制同样失手（实证）；spec 已改 quick-set-me-up 确定性入口，ExplorerPage 的 hadSelectionRef 语义建议复核
  R3 lint 45 warnings（react-hooks set-state-in-effect 类）为存量风格债——0 error，建议独立清理票
  R4 confirm-provider（新栈 prompt/confirmPhrase）与 components/ConfirmDialog（深形 body+confirmDisabled）双确认层并存——消费面各司其政（新页/旧 API 共用件），终验后可评估合并
  R5 45 分钟配额窗内完成终批 82 绿——未跑全量 145+ 套件（m11-m17 其余域零改动面 + 本票四门绿；收编时建议全量 e2e 复跑一轮坐实）
Blockers:     无（A/B 两段全部落地；R1-R5 为登记项非阻塞）
Next:
  1. 布局票：树页/主内容 flex 高度链短视口塌缩（R1——ChildrenGrid tree-list 在 720p 下被压至 ~0px）
  2. spec 迁移票（残余 P2-stale 腿已在本票清 6 spec；建议对 m11-m17 存量 spec 做一轮「旧栈选择器审计」）
  3. 确认层合并票（R4）：components/ConfirmDialog 的 body/confirmDisabled 深形 API 并入 providers/confirm-provider
  4. 锚册：§10.9 P4 批已入册（conductor 收编时随本票 commit 落库即可）
  5. 契约回写诉求：D-P4-1/2/3 三条交 compatibility-engineer 入 fern（archive 族参数与前置闸）
  6. lint 存量 warnings 清理票（R3）
断点快照:   无（两段完成，终验五门全绿，停手等收编；零 git 操作——工作树保持未提交）
