Ticket:       FE-P3 前端重写 Phase 3 — Administration 全量（管理域逐域新栈 + LegacyBridge 清空）
Role:         dev-frontend（area：web/src 管理面全域 + app/router + app/shell + web/e2e P3 域 spec）
Input:        dispatch 要点（九域清单/解锁面/验收五门）；frontend-rewrite-architecture.md §0 七红线/§4 IA/§5 数据层/§7 表格策略/§10 P3 门；frontend-capability-matrix.md #14-26 + 未列域表；frontend-rewrite-audit.md §2.7-2.10 页报告/§3.2 解锁池/§7 高危契约；internal/httpapi/{keypair,system_settings,system_qrl,webhook_outbox,bundle,build}.go 契约实测
Changes:
  - security 域全量新栈：Users/UserCreate/UserDetail（E2 单请求/E3 回显/E4 删强确认 prompt 锚迁移）/Groups/GroupForm（409 冲突面板+E5 穿梭）/Permissions（E6 m-holder 分流）/PermissionEditor（五列矩阵+两步资源 Dialog+模式测试器+diff 确认——修复 cancelRef 焦点抢回缺陷：内联 ref 每渲染重挂，useCallback 稳定化）/Tokens（一次性明文态机+step-up 双腿）；widgets.tsx/TransferBox 重写为非 MUI（原生 checkbox 穿梭），旧 MUI TransferBox 退役
  - keypair 解锁页（新设）：/admin/security/keypair 路由+侧栏条目——列表（关联仓库 chips+解除）/生成 Dialog（服务端 keygen：名/别名/位数/UID 三件/口令两联）/导入 Dialog（armored 双栏，vault 字面 FE 不构造）/公钥 Dialog/校验/删强确认（audit §3.2 十 op 全消费：list/get/import/update/delete/verify/public-by-repo/generate/associate/disassociate）
  - governance 域全量新栈：Audit（**AG Grid 裁定偏离**——coordinator 采纳：冻结合约胜过表格库，native 表保留 keyset 页窗/游标链/窗内客户端过滤/计数断言原语义，AG Grid 转跟进事项）/Quotas（water-bar 水位+行内编辑全量替换）/Replication（TanStack Query refetchInterval 复刻 10s 轮询+stale 保留+封锁双开关 CheckRow）/Trash（五元组+恢复 prompt allowEmpty+EMPTY 强确认）/GC（cron 三槽+grace+dry-run/apply 门）/MigrationPanel（Query 5s 轮询）/Backup（CRUD+CLI 命令块）
  - monitoring 域全量新栈 + Settings 新设页（#25 解锁）：Storage/Status/Logs（双源+7s 尾随+Pause；ButtonAsAnchor ...rest 透传修复）/SystemInfo；SettingsPage=旋钮回显卡（GET /v1/system/settings 消费——0→1）+ QRL 面板（三态判定：200 active / 400 verbatim disabled；双桶数值编辑 disabled 态出厂值兜底渲染；mode 翻转+恢复出厂 danger）
  - webhooks 域全量新栈 + outbox 死信面解锁（T-496 API 消费）：WebhooksPage 双 Tab（订阅+投递）/SubscriptionDialog（66 型复选墙+criteria 托管五键+secret 三态）/SubscriptionDrawer（Sheet 480 档+排障环）/OutboxPanel（filter 三件 status 闭集 select+keyset 游标链分页+dead 行 replay danger 确认——lib/webhooks.ts 增 getOutboxPage/replayOutboxDelivery）
  - builds/bundles 全量新栈 + 写面解锁：BuildsPage（三视图+时间线）/PromoteDialog（status-only/迁移双臂+dryRun 预演+messages[] 语义色流+failFast）/RetentionDialog（count/minDate/keep/deleteBuildArtifacts+async 语义）；BundlesPage（三视图+HEAD 探针）/CreateBundleDialog（显式清单行编辑器+409 三态）
  - Profile 新栈（改密+Identity Token 自助态机+SSH 缺位卡）/NotFoundPage 新栈/LicenseAddonsPage 新栈
  - i18n：全部新键入域包——regen 重算 2,267 键（P2 的 2,114 + 新域键），en 译值全量补齐（新增空串 271 处逐条填值），zh 清单同构
  - LegacyBridge 清空：legacy-bridge.tsx 删除（bridge 零路由消费门达成）；LegacyMount/LegacyHost 迁 components/layout/legacy-host.tsx（P2 页面的 Deploy/SetMeUp/PropertiesTab/ReplicationsSection 挂载位——P4 退役）；路由表全部 lazyEl 直挂
  - 旧 MUI 页面退役（git rm 14 文件）：pages/{LoginPage,DashboardPage}.tsx、pages/search/{SearchPage,ResultsTable,AqlPanel}.tsx、pages/artifacts/{ArtifactsBrowser,NodeDetail,browser.css}、pages/repositories/{RepositoriesPage,RepositoryFormPage,RepoDetailPage}.tsx、components/{AppShell,NavIcons}.tsx、pages/security/TransferBox.tsx
  - 平台层修复：Button 基座恒挂 text-foreground + base.css 裸 button 最小重置（preflight 关闭期 UA ButtonFace 在深色主题触 axe contrast serious——t366/t512 双主题腿实证）；Dialog primitive 显式回焦锚（Radix FocusScope unmount 回焦在 React 19.2 实测不发火——focus() 零调用焦点落 body，全族 dialog 一次性修复）；confirm-provider 补 confirm-dialog/confirm-cancel/title 家族锚（sonner toast 门面 lib/toast.ts 统一携带 data-testid="toast"——15 spec 消费面零迁移）；SearchPageV2 rowIndex 取值修正（v36 cellRenderer 无 rowIndex → node.rowIndex，search-result-N 全 0 重复锚修复）；TreePanel forbidden 分支先行修复（error 分支吞掉 403 L2 收敛——tree-root-denied 不可达缺陷）；text-muted→text-muted-foreground 全面替换（Tailwind @theme 的 text-muted 映射 surface-2 色值，对比度 1.07 实证）
Files:
  新增：web/src/pages/security/{KeypairPage.tsx,keypair.ts}、web/src/pages/monitoring/SettingsPage.tsx、web/src/pages/webhooks/OutboxPanel.tsx、web/src/pages/builds/{PromoteDialog.tsx,RetentionDialog.tsx}、web/src/pages/bundles/CreateBundleDialog.tsx、web/src/components/layout/{table.tsx,bits.tsx,fields.tsx,transfer-box.tsx,legacy-host.tsx}、web/src/lib/toast.ts、web/e2e/p3/unlocked-faces.spec.ts
  修改：security 域 9 页+widgets、governance 域 7 页、monitoring 域 4 页、webhooks 域 3 页、builds/bundles 2+4 页、ProfilePage/NotFoundPage/LicenseAddonsPage/AuthConfigPage、app/router/index.tsx、app/shell/{nav-model.ts,Topbar.tsx,AppShell.tsx}、app/providers/confirm-provider.tsx、components/ui/{button.tsx,dialog.tsx}、lib/webhooks.ts、styles/base.css、pages/security/security.css（status-pill/badge-warning-outlined 承载）、explorer/{TreePanel,ChildrenGrid,DetailInspector,ExplorerPage,CopyMoveDialog}（锚/缺陷修复）、repos 三页（css 直挂/spinner role）、search/SearchPageV2、i18n en 12 域包 + zh 12 清单、e2e 9 spec（rbac-V13 四分组 IA 迁移、t384/t386 形状钉类名、t512 三处契约迁移、t514 空态/主题腿、t372 返回 URL/锚、t494 深链切换、fr82 AG Grid 行锚、m10 管理分组/矩阵计数地板化）
  删除：src/app/router/legacy-bridge.tsx、src/pages/{LoginPage,DashboardPage}.tsx、src/pages/search/{SearchPage,ResultsTable,AqlPanel}.tsx、src/pages/artifacts/{ArtifactsBrowser.tsx,NodeDetail.tsx,browser.css}、src/pages/repositories/{RepositoriesPage,RepositoryFormPage,RepoDetailPage}.tsx、src/components/{AppShell.tsx,NavIcons.tsx}、src/pages/security/TransferBox.tsx（共 14 文件）
Tests:
  新写 e2e/p3/unlocked-faces.spec.ts（6 用例）：keypair 空态+生成/导入 Dialog 字段集+提交门、Settings 旋钮消费断言（waitForRequest /v1/system/settings）+QRL 三态+双桶、outbox Tab+过滤器闭集+空态、builds promote/retention Dialog 字段集+failFast 默认、bundle create 行编辑器+提交门、keypair+settings axe 双主题
  迁移 9 spec 共 16 腿：rbac V13（nav-mode-switch→四分组直见）、t384 页脚形状钉（MuiButton-contained→bg-primary/border）、t386 危险变体类（Error→destructive）、t387 spinner role+列菜单 menu 角色（repos+audit）、t512（failFast:false seed、record-only 双行、深链尾缀、搜索全名、Module ID 深链、API 对账 record-only 契约）、t514（主题腿 localStorage+reload、grant 空态）、t372（返回 URL 自动选中容忍、tree-root-denied）、t494（深链切换腿）、fr82（AG Grid 行锚+check-row 类）、m10（管理分组标签+矩阵计数地板化 ≥19）
  四态覆盖：全部新栈页 loading(skeleton)/error(ErrorCard+重试)/forbidden(L2 卡)/empty(引导) 四态齐备；axe 双主题 145+ 腿绿（含 keypair/settings 新面）
Commands:
  cd web && npm run typecheck && npm run lint && npm run build
  make console-size（repo 根）
  node scripts/t463/regen-catalogs.mjs && node scripts/assert-i18n.mjs && node scripts/assert-tokens.mjs && node scripts/anchor-audit.mjs --ledger
  CGO_ENABLED=0 go build -trimpath -o bin/binflow-server-p3 ./cmd/binflow-server（自起 :18160 /tmp/bf-p3-smoke，BINFLOW_ADMIN_PASSWORD）
  cd web && BASE=http://127.0.0.1:18160 ADMIN_PW=admin-test-pw npx playwright test <分批 spec 集>（五批 + 终批）
Outputs:
  typecheck：0 errors
  lint：0 errors / 44 warnings（react-hooks 编译器派生存量族——P2 同族收敛）
  build：assert-tokens OK（css 12+tsx 99 零字面量）+ assert-i18n OK（3,026 调用点/2,267 键 en/zh 同构）+ vite ✓ + relink 0 残留 + wire-brand 7 指纹
  console-size：968,719 bytes gzip（预算 5MB——0.92MB）
  e2e（终态终跑，最终二进制 145 passed exit 0）：批次累计 ≈300 腿绿——p2/core-flow+smoke+login+artifacts+users-groups+permissions+governance-monitoring（25）、m13+m14+m17+p3（82）、m8 其余+域 spec（74）、m9+m10+m15+m16+根 spec（31+修后 23+12）、终批 p2/p3/m8 主干/m13/m14/m17/auth-config/rbac/governance/replication/trash/migration（145）
  embed 冒烟：make console 产物 → serve :18160 → /binflow/ui/ 200
Compatibility:
  锚册对照：既有锚族原样沿用（users-*/groups-*/perms-*/token-*/wh-*/repl-*/trash-*/gc-*/backup-*/logs-*/settings-*/bundle-*/builds-* 等）；P2 已登记退役沿用（nav-mode-switch/tree-load-more 等）；**新锚待登记（ux-designer，日志 Next 项）**：keypair-* 37 族、qrl-* 12、outbox-* 12、settings-* 5、wh-tab-subs/outbox 2、build-promote-* 12、build-retention* 7、bundle-create-* 13、license-uninstall-confirm、logs-truncated、users-sort-lastlogin
  **A3 退役登记诉求**：P3 实际退役（册有 src 无）——t512-builds-page（T-512 spec 旧页根锚，spec 已随改 builds-page）；动态构造假阳性（src 实际在场，扫描词汇表限制）——node-downloads/node-last-downloaded(-by)/node-remote-downloads/tree-trash-node（DetailInspector row() 传参构造）/core-flow（p2 spec 根）；P2 遗留已知项不变（load-more/nav-icon/search-aql-sort-* 5/search-row-select-*/search-select-all/tree-load-more 等）
  契约漂移：无新增（全部按 httpapi handler 实测消费）；audit-table AG Grid 裁定偏离按 conductor 指示执行（native 表保留冻结合约，架构 §7 行勘误归 conductor）
  LegacyBridge：**零路由消费达成**——legacy-bridge.tsx 已删；P4 待清 MUI 清单（grep @mui 14 文件）：app/{MuiProvider,ToastContext}.tsx、components/{ConfirmDialog,CopyButton,DeployDialog,EmptyState,ErrorCard,Pager,SetMeUpDialog,Skeleton}.tsx、lib/muiAtoms.ts、pages/artifacts/PropertiesTab.tsx、pages/repositories/ReplicationsSection.tsx（全部仍被 P2 页面经 legacy-host 消费——P4 随 Deploy/SetMeUp/PropertiesTab/ReplicationsSection 重写退役）
Security:  用户内容渲染全部 React 转义（组名/错误 message/公钥 PEM/armored 材料 JSX 文本节点）；无 dangerouslySetInnerHTML；secret 三态全链 write-only（authconfig 哨兵剔除/webhook secret 省略保持/keypair 口令不回显）；无敏感信息前端日志
Performance:  产物 0.92MB/5MB；审计 keyset 页窗（深翻页零 offset 扫描）+ outbox keyset 同形；usage 批量注水 cap≤3 语义保留；AG Grid 保留面（Explorer/搜索）行虚拟化不变
Risks:  ①confirm-provider prompt 单输入框形态——旧 ConfirmDialog 富 body（影响面列表+输入）现以 description ReactNode 承载，语义保真但视觉密度略简；②Radix 焦点行为与 MUI 差异面（FocusScope 回焦缺陷已平台层修复，残余差异随 P4 键盘全量重验收口）；③t494 纯浏览计数腿并行 worker 下偶发（同前缀夹具竞态——serial 重跑绿，登录页家族 flake 同性质）；④Audit AG Grid 偏离待架构 §7 勘误收编
Blockers:  无
Next:  ①锚册 P3 批登记（ux-designer）：新锚 keypair-*/qrl-*/outbox-*/settings-knob-*/wh-tab-* /build-promote-*/build-retention*/bundle-create-*/license-uninstall-confirm/logs-truncated/users-sort-lastlogin + 退役 t512-builds-page + 假阳性族扫描词汇表扩展（DetailInspector row() 形态）；②Audit AG Grid 迁移票（spec 重写为无限行断言后翻案，或架构 §7 行勘误定格 native）；③DeployDialog/SetMeUpDialog/PropertiesTab/ReplicationsSection P4 新栈重写票（重写后 legacy-host.tsx + 14 MUI 文件清零）；④confirm-provider 富 body 视觉密度回归（ux-designer 视觉走查）；⑤t494 纯浏览计数腿的夹具唯一化加固（uniq 前缀与并行 worker 竞态）

---

## 收尾补记（FE-P3 锚账收尾专票，dev-frontend 2026-09-10）

Ticket:       FE-P3 锚账收尾（原会话失联后工作树收尾：anchor-audit --ledger 全绿 + §10 勘误）
Role:         dev-frontend（area：web/scripts/anchor-audit.mjs + docs/design/console-ux.md §10——锚账域，不碰 TS 源）
Input:        dispatch 三尾巴（broken tree-load-more 负断言豁免 / A3 三族勘误 / ledger 全绿）；console-ux §10.6 口径（退役=表行为权威）；§10.7/§10.8 重写期批次行文；FE-P3 日志 Next①登记态
Changes:
  - 退役反检豁免收紧（缺陷修，头注留痕）：工作树既有版本已修正 toBeCount→toHaveCount（broken 已零、豁免生效——line 索引对齐复核：`text.slice(0, m.index).split('\n').length - 1` 与 negated 数组同基）；但二版过宽处收口——①删 toBeHidden() 豁免臂（本仓 toBeHidden 全部用法是对**活锚**的隐藏态断言：node-download-panel ×3、audit-columns-menu、help-docs——逐点 grep 自证真实消费，退役反检零用例；§10.6 各行「count 0 反断言」才是惯例形）；②负向后顾 `(?<!\bnot\s*\.\s*)` 排除 `.not.toHaveCount(0)`（语义=「至少一个在场」的正消费——artifacts.spec tree-list 腿）。回收 9 条被误除的具体引用（4009→4018）
  - STOP 停词表补录 16 词（锚账收尾批，头注留痕）：文件名段 core-flow/t512-builds-page/unlocked-faces/legacy-bridge（HEAD grep 自证该删除文件零 testid——FE-P3 日志「旧页根锚退役」上报系误报，实为 spec 文件名误入册 token）+ 机制词 load-more + §10.8 组描述裸名行文短写 users-sort/wh-tab/qrl-mode/qrl-readonly/keypair-create/keypair-import/keypair-generate/keypair-generate-uid/keypair-readonly/outbox-filter/outbox-filter-event（真身=逐名补录带尾实名，均在册在 src——v1.25 repo-repl 星号速记同款先例）
  - console-ux §10 勘误性追加（不重构既有节）：①§10.6 退役总表补 2 行 tree-load-more/nav-mode-switch（§10.7「退役登记 2 条」行文预告而表行缺失=ledger A3 断链主因；行内勘误 §10.7「TanStack Virtual」系笔误、实态 AG Grid 无限行模型）；②§10.8 尾部勘误批四条（裸名短写真身对照 / 文件名与机制词 / §10.6 落笔兑现 / 对账器缺陷修留痕）
Files:
  修改：web/scripts/anchor-audit.mjs（negated 正则收紧 + STOP 16 词）、docs/design/console-ux.md（§10.6 表 +2 行、§10.8 尾勘误批）、reports/agents/FE-P3.md（本补记）
  新增/删除：无
Tests:
  无新 e2e（锚账对账域——不碰页面/组件；既有 145 spec 面零影响：改动仅扫描器与册）
Commands:
  cd web && node scripts/anchor-audit.mjs --ledger（exit 0）
  cd web && npx tsc --noEmit（exit 0）
  cd web && npm run lint（eslint 0 errors/44 warnings=存量族不变；assert-i18n 4 violations=工作树既有漂移，见 Compatibility）
  正则语义探针（node -e 四腿：退役反检豁免 / not. 正消费不豁免 / 带空白 not. 不豁免 / 非 0 计数不豁免——全 PASS）
  tree-load-more spec 全形态引用探针（node -e：全库唯一引用=artifacts.spec.ts:459 的 toHaveCount(0) 行——豁免恰为 load-bearing）
Outputs:
  ledger：src 977 家族（落点 1069）/ spec 1085（引用 4018）/ 册 1199；unregistered=无、broken=无、A3/A4 零违例——「PASS（registered 与册一致：unregistered/broken 双零，retired 全部在表）」exit 0；退役表 111→113（+2 行）、册锚 1215→1199（-16 停词）
  tsc：0 errors exit 0
  探针：tree-load-more 唯一 spec 引用即 :459 负断言行（无豁免则 broken 复现该名——三尾巴①闭环实证）
Compatibility:
  锚册对照：A3 十八条全部定性——2 条真退役落表（tree-load-more/nav-mode-switch）+ 16 条行文假阳性停词；A1/A2/A4 同零
  **越界上报（不顺手修）**：npm run lint exit 1——assert-i18n 4 violations（search 域）：SearchPageV2.tsx:632/656 新 aria-label t('全选本页结果')/t('选择第 {v1} 行') 而 en/search.ts 与 zh/search.json 已删旧键（全选当前显示的行/选择 {v1}）未补新键——半截改名（工作树既有，非本票改动；本票禁碰 TS 源）。修复配方：en/search.ts 补 "全选本页结果": "Select all results on this page"、"选择第 {v1} 行": "Select row {v1}"，zh/search.json 清单同名补两键；同因 npm run build 的 assert-i18n 腿必然同红（同一脚本，未跑推断非实测）
Security:     无新增渲染面（对账器与册文档改动；无用户内容渲染变更）
Performance:  无影响（对账器扫描复杂度不变——停词表为 O(1) 集合查询）
Risks:        ①豁免为行级粒度——同行并存退役反检与异锚正引用的极端形态会连带豁免（现库零用例，头注已述语义边界）；②§10.7「TanStack Virtual」笔误仅在新表行勘误、原文未改（勘误性追加约束）；③assert-i18n 4 violations 未修（越界上报项，见 Compatibility）
Blockers:     无（锚账域全绿；i18n 漂移为域外上报项非阻塞）
Next:         ①conductor 收编时补 search 域 2 键（配方见 Compatibility——4 行改动）；②「停词表 vs 册行文自律」结构性张力再议：行文短写反复制造假阳性，可议 §10 行文规范禁裸名速记（改走「实名清单」形——归 ux-designer 档案票）；③dead 桶 95 家族处置队列（含 wh-tab-subs 等 P3 新锚零消费面）随大版本回归处置

