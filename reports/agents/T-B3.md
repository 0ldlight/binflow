# T-B3 工作日志 — Penpot-inspired 设计系统方案

```
Ticket:        T-B3 [P0] 轨道 B-3：设计系统方案（Phase 1 Core UI Foundation 设计输入）
Role:          architect
Area:          docs/design/design-system-plan.md（新建全文）
Input:         conductor 派发（宪章 §8~§14 摘要）+ 实读：web/package.json、components.json、
               src/main.tsx:29-36、src/styles/{tokens,base,pages,governance}.css、src/styles/tw/{tokens,tailwind}.css、
               scripts/assert-tokens.mjs、scripts/relink-assets.mjs（face 1/2/3）、src/components/ui/（19 原语清单）、
               src/components/layout/bits.tsx、src/app/shell/{AppShell,Sidebar,Topbar}.tsx、
               docs/reverse/frontend/parity-capture/{tokens.json,states-gaps.md,dom-snapshots/sidebar.html,screens-catalog.json}、
               tools/penpot-sync/penpot-spec.json、docs/design/frontend-rewrite-architecture.md（§0 不可变契约对齐）、
               BOARD.md 2098-2119（LOOP 011~022 Penpot 管线脉络）
Changes:       新建设计系统方案：差距五点（G1 token 族不全/G2 双 token 层/G3 样式范式分裂/G4 壳尺寸无 token/G5 状态覆盖缺口）
               → 迁移策略裁定（候选 A 全重写 vs B shadcn 保留+重 token 化〔选定〕vs C 换头 kit——论证表+后果）
               → token 六族规格（color 27×2 双谱/typography Inter 九级 22 变量/spacing 14/radius 5/shadow 4/elevation+motion 9）
               → 组件清单（P0 通用 20 + P1 核心域 11 + P2 打磨 8）与七态矩阵（九件全矩阵+其余紧凑）
               → 字体抉择（Inter 本地打包 @fontsource-variable/inter，CDN 否决——离线单二进制约束）
               → 分批计划（批 0~6，每批四门+截图+axe）
Files:         docs/design/design-system-plan.md（新建，~230 行）；reports/agents/T-B3.md（本日志）
Tests:         结构门：markdown 表格列一致性+标题层级脚本实跑 → NONE 错误（20 标题）；
               引用门：方案内引用的 12 个仓内路径逐一存在性核实 → 12/12 OK；
               编译门：不涉及（纯文档产出，零码改）
Commands:      grep/find 定位宪章轨迹（BOARD/loop-state.yaml:68 user_directate_uiux）；
               grep -c 旧类消费面（badge 13 文件/filter-bar 7/mono·text-2·text-muted 68）；
               grep 侧栏/顶栏尺寸（AppShell.tsx:125 w-56 / Topbar.tsx:248 h-12）；
               curl jsdelivr API 实测 @fontsource-variable/inter@5.3.0 woff2 尺寸
               （inter-latin-wght-normal.woff2=48,256B / latin-ext=85,068B——包在册可维护）
Outputs:       设计方案 1 份（token ~119 变量含亮暗双谱 27×2；组件 39 件〔P0=20〕；分批 7 批）；
               接口契约 0（前端域，无 Go interface 面）；API 契约 0 端点（零 Go 改）
Compatibility: 与 docs/compatibility/contracts/ 无交集（纯前端视觉层，wire 面零触碰）；
               与 ADR-0014（挂载/构建链/embed）、ADR-0029（UX 对齐+自有皮肤）一致；
               与 frontend-rewrite-architecture §0 不可变契约逐条不冲突（方案头显式声明不触碰）
Security:      离线部署面：字体本地打包否决 CDN（外网依赖/CSP/隐私）；无新增外部运行时依赖
               （@fontsource 是构建期资产依赖，OFL 许可可嵌入）
Performance:   字体预算 +48KB（latin 可变轴单文件）< 5MB 警戒位 1%；latin-ext +85KB 按需（批 3 字符集实测决定）；
               动效三档 120/160/240ms + prefers-reduced-motion 降级；布局尺寸 token 化零运行时开销
Risks:         R1 accent 色相悬置待 B-1 品牌终裁（槽位机制已冻结，缺省蓝可交付）；
               R2 批 2/3 截图基线集中翻新；R5 Inter×PingFang 混排基线抖动（批 3 专项验收）；
               妥协登记：AG Grid 双谱主题延批 6、多主题系统不做（YAGNI）
Blockers:      无
Next:          ① B-1 品牌方向终裁后回填 accent 字面值（2 变量+基线翻新）；
               ② 批 0 截图基线票先行（Phase 1 Core UI Foundation 开工条件）；
               ③ 建议本方案经 conductor 呈用户确认后冻结为 Phase 1 设计输入（软缝：机制冻结、字面值批内可勘误）
```

## 断点快照（不适用）

一次性收口，无中断。
