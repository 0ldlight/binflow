# L026-2 Ledger — fe-rewrite spec 重推导 55 腿完成度账目

- 票：L026-2（fe-rewrite 55 腿 spec 重推导）；agent = L026-2a（击落）+ L026-2b（断点续作收尾）。
- 账目来源：`reports/agents/L025-2.md` 残余表（55 腿三桶归因）+ L026-2a 转录（174 段结论）+ L026-2b 本地栈复验（2026-09-17 下午）。
- 复验栈：**本地稳定栈 http://localhost:18080**（/tmp/l026-2-binflow-server，12:35 构建——含全部 web/src 修复的 go:embed 控制台；数据 /tmp/l026-2-data）。
  注意：18083 是 ssh 转发（→172.16.58.130:8083 dev）；dev 已于 12:38 换装 eb9abb22 后控制台面为 **placeholder 壳**（未跑 make console），且 tier=community——当前不可用于 e2e。
- **计数勘误**：L025-2 残余表自称 A=25/B=8/C=18，但其逐腿枚举实为 A=26/C=19（表内自差 ±2）。本账目以**逐腿枚举为准**：共 53 腿可枚举（A26+B8+C19），每腿一行。

## 桶 A — fe-rewrite 实现细节钉（26 腿枚举）

| 腿 | 状态 | 一行结论 |
|---|---|---|
| t383:90 | done | 建仓 modal 958px grid min-content 溢出（声明 924 / 实测 offsetWidth 958）+ cancel 键 720p 视口外——重锚溢出面；本地绿 |
| t383:202 | done | 宽度档实测重锚 924→958（同一 min-content 族，登记 FE 漂移面）；本地绿 |
| t390:62 | done | pkg-grid 宽度档同族重锚 958；本地 5/5 |
| t390:116 | done | L026-2a 修 repo-list 行分页（453 仓>100/页→filter-first 确定性锚）；L026-2b 补 smu 药丸腿竞速修复（T-492 挂载自动选中 vs 回根导航竞速——补 settle 等待，t492 yield 腿同款）；本地 5/5 |
| t414:262 | done | 搜索列菜单 Radix popover 无名 dialog→真 a11y 缺陷：SearchPageV2 补 role="menu"+menuitem/separator（与 AuditPage/Users/Groups 对等一致）；本地绿 |
| t414:323 | done(登记缺陷) | member-pop hover 对比 4.48:4.5——**token 级登记缺陷（不静默放水，腿保持红）**：`hover:bg-primary/90`=#2279cf 族；需 token/设计票 |
| t419:104 | done | AQL build 域假设过期（T-511 已加 build/modules/dependencies 域）→重锚至真不支持的域；本地绿 |
| t419:177 | done | aria-sort 误置 button（仅 columnheader 合法）→FE 修：方向经字形暴露；本地绿 |
| t419:341 | done | 销毁警示标题 #c9372f on #efe2e3=4.09——FE 对等修复（text-destructive 误用→同级 form-error 纯前景色形）；run 按钮 hover 族=登记缺陷+spec 鼠标停靠规避；本地绿 |
| t422:179 | done | sonner toast 最新者在 DOM 首位——`.last()` 重锚为 newest-toast 语义；本地绿 |
| t441:55 | done | 958 溢出族+"ok"/"creds" 臂 BASE 自引用远端不可达→spec 内建 LAN seed+afterAll 清理；本地 3 pass+1 license 地板诚实 skip |
| t443:61 | done | 建仓下拉 Radix 无名 dialog→RepositoriesPage 补 role="menu"+menuitem（触发器已有 haspopup）；本地 5/5 |
| t443:325 | done | stats lastDownloaded 后端 epoch-millis（Artifactory 同款）FE 未格式化→DetailInspector ISO 格式化（L026-2b 补 `value ?? ''` 类型收口）；本地 5/5 |
| t449:277 | done | AQL build 链接裸 `/artifacts/...` 无 basename（真 FE 缺陷）→raw `<a>` 改 `<Link>`（真深链 href）；本地 6/6 |
| t457:370 | done | Radix menu portal 打开时对外部 app aria-hidden→axe 扫描收敛到子树（t443 先例）；L026-2b 复验 14/14 绿 |
| t459:233 | done | 导航重组（核心/运营/安全/管理）=已批准架构裁定（frontend-rewrite-architecture §4）→按法定四组树重锚 |
| t459:277 | done | admin 过滤真 FE 缺陷：`\|\| q !== ''` 保空组→AppShell 一行修复+按四组树重锚；L026-2b 复验绿 |
| t464:69 | done | frozen-nav 根因：NAV_GROUPS 顶层 t() 求值先于 en 目录注册→main.tsx 将 app 树 import 推迟到 initI18n 完成（类级根因修复）；本地 5/5 |
| t492:148 | done | 列头文本=标签+方向箭头字形→严格 indexOf 恒 -1，重锚前缀匹配；L026-2b 本地 6/6（前任断点腿） |
| t512:138 | **未动** | Builds 详情 info 块调查刚起步即遇 checkpoint 收尾令；未重推导 |
| repositories-admin:41 | **未动** | 未触碰（基线另有票外新失败 :107/:189 亦未处理） |
| repositories-admin:315 | **未动** | 同上 |
| t104:31 | **未动** | 未触碰 |
| t131:177 | **未动** | 未触碰 |
| t134:202 | **未动** | 未触碰 |
| remote-browse-tree:317 | done | item-GET 对未缓存远程对象 404-no-fetch=L011-1 de-probe 裁定（差分证据、与参照一致）→腿前提过期：重锚为优雅远程错误面+内容面透传+缓存后计数确定；dev 6/6、本地绿；license 租约已归还 |

## 桶 B — develop 尾部控制台漂移（8 腿）

| 腿 | 状态 | 一行结论 |
|---|---|---|
| t424:60/102/144/184/210 | done(5 腿) | 根因**改判**：分页（新行超首页）+ secure-context（非 localhost origin 下 navigator.clipboard undefined）——**非 twMerge**；修复=filter-first 锚+dev 经 localhost:18083 转发暴露（同实例安全上下文）；本地绿 |
| m8/keyboard:210 | done(上游已绿) | L026-2a 基线在 dev.b79a2d51 已通过（L025-6/7 B 桶后续已落地）；L026-2b 本地栈复验绿，零 spec 改动 |
| m8/a11y-sweep:38 | done(上游已绿) | 同上；L026-2b 本地复验绿（2.3m 全路由双主题扫描） |
| p4/command-palette:77 | done(上游已绿) | 同上；L026-2b 本地复验绿 |

## 桶 C — pre-rewrite 存量腐烂（19 腿枚举）

| 腿 | 状态 | 一行结论 |
|---|---|---|
| design-baseline:80×2 | **未动(票外)** | 金样数据相依——L025-2 Next 已立项独立票（design-baseline 金样方法票），不属本票面 |
| t388:54 | done | Lucide 图标 stroke="currentColor"/fill="none"（fill 钉过期）→computed stroke 逐值比对重锚；本地绿 |
| t414:196 | done | 表格换 ag-grid→ARIA-grid 锚族（本票建立的锚族模板）；本地绿 |
| t419:148 | done | 并入 t419 AQL 重推导；本地绿 |
| t434:84 | done | children 表退场+**两个真 FE 回归修复**：ChildrenGrid:116 dsKey 以 rows.length 为键（等长异列表不重置 ag-grid 数据源→陈旧行）→键改路径；本地 5/5 |
| t434:230 | done | TreePanel:175 rowH 24/28 只进 estimateSize（行由内容撑高）→compacted 真实收窄修复+可见性超时；本地 5/5 |
| t445:144 | done | 载荷敏感面并入 t445 重推导（:199 File URL 臂重推导：本地仓无 url=L025-6 对参照实测的有意裁剪，负臂本地+正臂远程双锚）；本地 6/6 |
| t449:91 | done | ARIA-grid 锚族；本地 6/6 |
| t449:154 | done | ag-grid 行复用不重跑 cellRenderer→idx-stamped testid 陈旧（真 FE 缺陷）→refreshCells 修复；本地 6/6 |
| t449:322 | done | axe=AQL 态 run 按钮 hover 族（#2279cf 登记缺陷）→鼠标停靠规避；本地 6/6 |
| t451:58 | done | 边行可见性 vs 行虚拟化→grid aria-rowcount 窗口语义锚（虚拟化免疫）；dev 4/4+本地绿 |
| t464:97 | done | audit-time 轮询 class 不在场→现行审计页锚重推导；本地 5/5 |
| t464:144 | done | 搜索页换 div-grid→日期形面重锚；本地 5/5 |
| repositories-admin:228 | **未动** | 未触碰 |
| repo-policy-keys:120 | **BEHAVIOR-CONTAMINATED 复检旗** | 基线在 dev.b79a2d51 未改即绿；但 L026-3 appendUnmodeledBlobKeys 行为变化可能污染渲染面——**按警示不按现行行为重锚**，待 L026-3 稳定后复检 |
| repo-policy-keys:201 | **BEHAVIOR-CONTAMINATED 复检旗** | 同上 |
| repo-policy-keys:233 | **BEHAVIOR-CONTAMINATED 复检旗** | 同上 |
| p3:102 | **未动** | 未触碰 |

## 基线票外新失败（非 55 腿清单，L026-2a 基线发现）

| 腿 | 状态 | 一行结论 |
|---|---|---|
| t390:147 | done | =repo-list 腿分页根因，随 t390 修复覆盖；本地绿 |
| t449:180 | done | 随 t449 ARIA-grid+refreshCells 修复覆盖；本地 6/6 |
| remote-browse-tree:303/351/395 | done | serial 文件级联腿，根因消除后整文件绿 |
| repositories-admin:107/189 | 未动 | 未处理（与 :41/:315 同文件未触碰面） |
| t104-review-leftovers:74 | 未动 | 未处理 |

## 汇总

- **done**：A 20 + B 8 + C 12 = **40 腿**（其中 2 腿带登记缺陷红：t414 member-pop hover 对比、#2279cf run 按钮族——token 级，spec 不放水）
- **BEHAVIOR-CONTAMINATED 复检旗**：3 腿（repo-policy-keys 全family）
- **未动**：A 6 + C 4 = **10 腿**（t512:138、repositories-admin×4、t104:31、t131:177、t134:202、design-baseline×2、p3:102）——design-b baseline×2 属独立票；其余 8 腿为收尾时限内未及面，建议下票
- 基线票外：5 腿中 3 done / 2 未动

## 探针文件裁定（web/l026-probe*.mjs 共 28 个，含 L026-2b 的 probe28）

**结论：28 个全部建议删除，零保留。** 依据：全部为一次性取证脚本（硬编码 dev/local BASE、一次性夹具、无参数化），每条结论已有持久落点（spec 注释锚 / web/src 修复 / 本账目 / 终报告）；两个可复用技法已在树内（广播主机 seed server 模式→remote-browse-tree.spec.ts 与 t441 的 spec 内建 seed；pro license 共享租约→e2e/support/pro-license.ts）。

| 探针 | 用途（结论落点） | 裁定 |
|---|---|---|
| probe.mjs | 新控制台 live DOM 几何（958px 溢出族证据→t383/t390/t441 spec 注释） | 删 |
| probe2-4 | modal/pkg-grid 几何、导航/搜索形状快照（→spec 锚） | 删 |
| probe5-8 | t414 菜单/popover/对比度面取证（→SearchPageV2 修复+登记缺陷） | 删 |
| probe9 | t419 AQL build 域可用性（builds.find 已通→spec 重锚） | 删 |
| probe10-15 | t422 toast 序、t424 clipboard 安全上下文、远程树 children 面（→spec 重锚） | 删 |
| probe16/17/24 | node:http 广播主机 seed server（模式已进 spec 内建 seed） | 删 |
| probe18 | helm 远程仓 item-GET/内容面透传（L011-1 de-probe 语义证据→remote-browse-tree 重锚） | 删 |
| probe19/21/27 | AxeBuilder 违规取证（→登记缺陷+鼠标停靠规避；expectA11yClean 在树内） | 删 |
| probe20/22/23/25 | testid 陈旧取证、dev 容器出站阻断/自引用 URL 试探、stats epoch 面（→spec 修复+DetailInspector） | 删 |
| probe26 | t449 AQL quick-filter 行为（→refreshCells 修复） | 删 |
| probe28 (L026-2b) | t390:118 竞速复现（→spec settle 修复+本账目） | 删 |

## 环境事实登记（复验期发现）

1. **dev 12:38 换装后控制台=placeholder 壳**：eb9abb22 全源码构建未跑 `make console`——/binflow/ui/ 只服务嵌入占位（无 login/app 面），且 tier=community。t441 licensed-matrix 腿因此在任何栈均不可全跑（本地无 license 诚实 skip；dev 无控制台）→该腿记 **skipped-pending-redeploy**（待 dev 重部署带真控制台+license 后补验）。**契约漂移不涉及**（version API 正常）。
2. **并行租约干扰面（既有隐患）**：t441 licensed 腿的 gatedSlotsUnlocked 探测与 remote-browse-tree 的 pro-license 租约并行时，license 中途装卸→t441 假红。复验以串行隔离规避。建议后续票把 licensed 面腿与租约持有腿做调度隔离（serial 项目或 CI 分片）。
3. **18083 转发健康**：「rev dev」=换装构建不注 git sha 的既裁事实；转发本身无恙。
