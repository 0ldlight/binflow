# T-UIB2 — UI Phase 1 批 2：壳与布局

```
Ticket:       T-UIB2 UI Phase 1 批 2：壳与布局（P0）
Role:         dev-frontend（area = web/src/app/shell + design-system/tailwind.css 桥接层）
Area:         web/src/app/shell/、web/src/design-system/tailwind.css、web/e2e/shell-layout.spec.ts
Input:        docs/design/design-system-plan.md §3.3（布局尺寸槽位 240/64/1440 + sp-8）、§6 批 2 行（壳 token 收编 + 侧栏重皮 + 间距节奏 + e2e shell 族 + axe 双主题）、§7 验证门；docs/reverse/frontend/parity-capture/（nav-tree.json、screenshots/screens/repos-local.png——分组标签 uppercase muted / active 亮底参照）；宪章 §13（240px 自选值，Artifactory 260 不翻拍）；批 1 产出 spacing.css 新槽位（只定义未消费）
Changes:      ① tailwind.css @theme inline 增 3 条布局桥接（--spacing-sidebar/--spacing-topbar/--container-content → var(--bf-*)），生成 w-sidebar/h-topbar/max-w-content 语义类——批 1 槽位自此进消费缝；② AppShell.tsx：aside w-56→w-sidebar（224→240px）、main px-5 py-5→px-6 py-6（20→24px 上 sp 档）、max-w-[1440px]→max-w-content；③ Topbar.tsx：h-12→h-topbar（48→64px）、px-5→px-6（与 main 左缘对齐）；④ Sidebar.tsx 重皮（Penpot 式、零新色值）：品牌行 py-4→h-topbar（与顶栏 64 线对齐）、分组标题 pt-3 pb-1 font-medium→pt-4 pb-2 font-semibold（fs-2xs 位 + §3.2 semibold 字重族）、条目 py-1.5 gap-2→py-2 gap-3（~34px 行高）+ 默认文字 sidebar-foreground/80 提亮层级、active = 左缘 2px primary 指示条 + sidebar-3 软底 + font-medium + 文字全亮（既有 token 族组合）；⑤ 新增 e2e/shell-layout.spec.ts 三腿（布局 token 端到端 / active 指示 / axe 双主题）
Files:        修改：web/src/design-system/tailwind.css、web/src/app/shell/AppShell.tsx、web/src/app/shell/Sidebar.tsx、web/src/app/shell/Topbar.tsx；新增：web/e2e/shell-layout.spec.ts；（注：web/scripts/seed-m{8,9,10}.* 的工作区变更是并发票所为——ADR-0050 PUT create-only/listFolders 注释与逻辑，本票未触碰）
Tests:        e2e/shell-layout.spec.ts（新）：腿 1 布局 token 端到端（根 token 取值 240px/64px/1440px + aside 宽 240 + topbar 高 64 + 内容 max-width 1440——w-56/h-12 字面量回归即翻红）；腿 2 active 指示（active 条目左缘 2px 非 transparent + 非透明底、idle 条目 transparent 左缘）；腿 3 axe 双主题（亮/暗各一次全页扫描，serious/critical=0——对照 binflow-baseline/axe-current-*.json 现值零新增）；四态面：壳层本批无数据态变化（登录/404/守卫态由 auth-shell 既有腿覆盖）
Commands:     cd web && npm run typecheck；npm run lint；npm run build；npm run assert:tokens；make console（仓库根）；make lint（仓库根）；go build -o /tmp/bf-tuib2 ./cmd/binflow-server && BINFLOW_HOME=/tmp/bf-tuib2-home /tmp/bf-tuib2 serve；cd web && BASE=http://127.0.0.1:8080 npx playwright test e2e/auth-shell.spec.ts e2e/console-smoke.spec.ts e2e/shell-layout.spec.ts；playwright 截图亮暗双主题目检（/tmp/tuib2-{light,dark}.png）
Outputs:      typecheck 0 错；lint 45 warnings 0 errors（全为存量 react-hooks 警告）+ assert-i18n OK（3118 调用点/2345 键同构）；build ✓ 8.21s + relink-assets verified；assert-tokens OK（css 14 + tsx 97 零违规）；make console：payload gzip 1,015,795B（<5MB）；make lint：0 issues；e2e 10 passed（auth-shell 5 + console-smoke 2 + shell-layout 3，7.8s）；dist CSS 实证语义类生成：.w-sidebar{width:var(--bf-sidebar-w)} / .h-topbar{height:var(--bf-topbar-h)} / .max-w-content{max-width:var(--bf-content-max)}；截图目检：亮暗双主题侧栏 240、品牌行/顶栏 64 对齐、active 左缘蓝条+软底、内容 24px 节奏
Compatibility: 锚册对照结论——侧栏分组标题/active 形态参照 parity-capture nav-tree.json 与 repos-local.png（uppercase muted 分组标签 + active 亮底），为 clean-room 自有皮肤实现（ADR-0029：不翻拍 Artifactory 260px/DOM）；宽度 240 取宪章 §13 自选值；契约漂移：无（纯前端壳层，无 REST 面变更）
Security:     本批零新增用户内容渲染点；零敏感信息进前端日志；e2e 服务跑在 /tmp 独立 home（BINFLOW_HOME=/tmp/bf-tuib2-home），未触碰 repo ./data 与 172.16.58.130 dev 实例
Performance:  无大数据面变更（壳层固定尺寸 token 化）；console payload 1.02MB gzip 持平（<5MB 门内）
Risks:        ① 截图 golden 基线将大面积翻新（侧栏 224→240、顶栏 48→64、active 重皮）——预期内（design-system-plan R2：批 2 集中承受 G4 视觉变更，基线翻新归 conductor 封票流程，本票未跑 design-baseline.spec.ts）；② 域页面内 w-56/w-[240px] 等字面量未动（批 6 域件范围）；③ 字号字面量（text-[11px]/text-[15px]/text-[17px]）留待批 3 九级字阶收编
Blockers:     无
Next:         ① 建议锚册无需更新（本批未触碰 IA/操作流/URL——ADR-0029 红线内）；② design-baseline golden 翻新建议在 dev 实例按 binflow-baseline/README.md 刷新纪律执行（conductor）；③ 批 3 Inter 上线时把侧栏分组标题 text-[11px] 收编到 fs-2xs 语义类、品牌名/页面标题字阶归位
```
