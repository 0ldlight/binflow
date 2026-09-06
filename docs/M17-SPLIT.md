# M17 拆票日志（tech-lead，2026-09-06）

> 输入：docs/prd/milestone-17.md v1.0（FR-151~161 + LC-99~114 + Q0~Q12）+ BOARD.md 2026-09-06 双终裁段（**Q1 = 三域进**：Build-info P0 + Release Bundle 最小面 + 洞察报表；Federation/Lifecycles 滚 M18 单列专程；**Q0 = 除 Xray 外全解禁**——HA/Federation 进 M18+ 轨、**全量 REST 兼容进产品范围分程分批**〔超 PM 倾向的新跨切程〕；PRODUCT.md 已改写：唯一排除 = Xray + 范围演进记录段）+ BOARD m16-done 总账（挂账三项 + 滚程五项：BE round-trip 缺口 / B-3.2 / NuGet symbol 七承 / webhook 触发源 / build-info dormant→wired）+ ROADMAP「M16 未纳入项」（已启用）+「M15 未纳入项」续滚工程债八项 + docs/M16-SPLIT.md 体例先例 + 差距底稿实核（**Fern openapi = 112 路径 / 158 ops**、docs/reverse/rest-api.md〔M1 子集〕、gap-endpoints.md、aql.md〔build 系五入口现 400——§2.1 远期行〕、webhook.md〔§3.4 build 域 3 事件休眠〕）+ 代码锚点（DECISIONS.md 最新 ADR-0044 → **ADR-0045/0046 空位**、internal/ 无 build/bundle/insights 包〔定名归 ADR〕、internal/scheduler 已立、票号实占至 T-480）。
> 产出：本日志即拆票产出（票批节候选——conductor 收编入 BOARD「### M17 票批 v1（tech-lead）」节）。零 git 操作，完成后候收编。
> 编号：自 **T-488** 起（conductor 指定；T-481~T-487 为保留段，收编时若见占用顺延核）。
> 票数对齐：PRD §1.3 PM 定容建议态 32~40（Q1 裁定带），实拆 **35 票**（P0×8 / P1×18 / P2×9——含波外条件票 T-522；Q10 活体基线修复 / Federation·HA M18 预留 / E7 toast / Tokens 字段核验 / license 公钥 ADR / B-2.8 AG 网格不占号）。对账：FR-152 拆 6 + FR-151 拆 2 在 PRD 估 8~10 线内；FR-153 拆 2（PRD 估 4~5——Any Distribution 双载归 FR-156 通配桶票同场、FE 勾选腿并入 T-514，取舍见 §6）；FR-154 拆 2 + 聚合会签腿归 T-489；**全量 REST 跨切程首程定容 4 票**（清单 + 三带——Q0 终裁新增面，PRD v1.0 未列 FR 行，落位留痕见 §0.3）；FR-160 拆 5（PRD 估 3~4——vite8+MUI 段二触全页面独立独占波、CLI/词表债独立 area）。

## 0. 范围与拆票基线

**M17 = 产品域扩张专程（Q1 三域进 + 确定层收纳 + 全量 REST 兼容跨切程首程）**。三股主料：

1. **前置锚两票**（T-488 规格票两份 + T-489 ADR-0045/0046 + 洞察聚合层会签 + B-1.7 增补评审腿）——Q0/Q1 双终裁已落（PRODUCT.md 2026-09-06 14:0x 改写在案），**主轴候裁层已解锁**（NFR-S82 门槛纪律：主轴票派发时间 ≥ 修订落笔时间——QA 终验审计行）；锚仍前置：clean-room 无规格不开工（T-507/T-508/T-513 均 dep T-488+T-489）。
2. **确定层先行批**（滚程五项 + 债务，零翻案面，不候锚）：W1 起步——configJSON round-trip / 通配桶 / FE 收尾腿 / 契约勘误两票 / cron·i18n 遗留 / Replay / 债包五票。确定层不被主轴挤占（「连续两程滚程即升级为债」纪律——本程零再滚收口）。
3. **主轴三域**：Build-info 全链（模型→REST→promote→webhook wired→AQL 域→FE 页，六票串行链 = 本程决定性路径）+ Release Bundle 最小面（BE+FE 两票）+ 洞察报表（聚合层+图表两票，BE 新包与主 lane 错峰）。

### 0.1 拆票纪律（M16 制度延续 + M17 特化）

- **全宽 2**（沿 M11~M16 口径），波内 area 互斥；**FE 主线一波一票错峰**；**httpapi 共享面排波次**（repositories/permissions/storage/search/system/build/bundle/insights/outbox 各 router 文件族错峰——同族跨波串行、新 router 文件波内并行须注记）。BE 全链挂 lane 2 与主 lane 天然错峰，无 BE 票反压主链。
- **行为面票必须 dep 规格票**：T-507/T-508 dep T-488（build-info.md）+ T-489；T-513 dep T-488（release-bundle.md）+ T-489 + Q2 终裁；T-504~T-506 dep T-503（REST 差距清单——跨切程的「锚票前置」等价物）；T-511 dep T-488（aql.md build 域增量段）+ T-507。
- **FE 票五件套 + 双 locale 键化**（M16 i18n 落地后的 M17 新纪律）：锚族冻结 + 四闸门 + axe 双主题双 locale serious=0 + 服务端 diff=0（BE 腿除外）+ SPA ≤10KB/票，**新增文案 zh/en 双包同票键化**（assert-i18n 零新硬编码——M16 T-463 扫描闸收口常态）。**BE 票三件套**：table-driven 单测 + ACL 零泄漏探针（build/bundle 系新泄漏面——allow() 同源硬 AC）+ `make test`（race）全绿。协议/链路面票必须真实客户端（docker promote 删缓存复拉 / jf CLI 条件腿）。
- **断言反转/缺位解除归属（PRD §5.4 ①~⑥ + AQL 入口行）**：① url 前缀勘误→T-493；② System Logs 注记反转→T-493/T-494；③ Module ID / Builds 页签缺位解除→T-512、Any Distribution→T-491+T-514；④ webhook build 域 dormant→wired→T-510；⑤ Annotate write 别名候裁翻转→T-499；⑥ aql.md §2.1 build 系入口「远期 400」行翻转（builds/modules/dependencies 三入口）→T-511（promotion/releasebundle/sensitive 维持 400 诚实拒绝 + 注记）。
- **票号命名**：新测试文件以被测行为命名（CLAUDE.md 规范），票号进文件头注释。

### 0.2 条件票池与不占号 slot

- **占号条件票**：T-522（NuGet symbol 七承——用户点名即翻，插空 BE lane）。
- **不占号 slot（M14 D3 先例）**：Q10 活体基线修复（conductor 决策——未修复则 T-488 降级 t226〔7.84.10〕单源 + 官方文档，附注零静默升格）；E7 toast（候用户信号）；Tokens 字段核验（候商业版/云活体源）；license 公钥 config 覆盖 ADR（候立项）；B-2.8 有效权限 AG 网格（候用户信号）；**Federation/Lifecycles 与 HA 本体 = M18 预留登记不占号**（Q1 终裁滚 M18 单列专程 + Q0 终裁进路线——T-519 PM 收口笔在 ROADMAP「M17 未纳入项」起 M18 预立项段备稿）。

### 0.3 全量 REST 兼容跨切程落位（Q0 终裁 #5 新面——PRD v1.0 未列 FR 行的承接）

用户终裁「全量 REST 兼容进产品范围，分程分批交付、/api/v1 并行保留」超出 PRD v1.0 §2.1 行 M 与 §2.2（PM 倾向维持子集承诺被推翻）。落位形态：

- **差距清单驱动**：T-503 [P0] 建活体差距矩阵 `docs/reverse/rest-compat-matrix.md`——以 **Fern openapi 158 ops（BinFlow 现状底稿）** × **docs/reverse/ 全部既有行为规格 + 官方 REST reference 全集**（Artifactory 面底稿）逐域对账，四态标注（已兼容 / 部分兼容〔差异行级列出〕/ 缺位 / 不适用〔pro-only·Xray-tied·物理不可行——xray 族连带维持排除〕），产出自维护 backlog registry（后续程滚程留痕载体）。
- **按域渐进连续小票带**：首程定容 = 三带票（T-504 repositories+system 面 / T-505 storage+security 面 / T-506 search+老搜索面），每带票面行集以 T-503 冻结为准（票面给候选行集——rest-api.md §2〔configurations 全量配置〕/ §5〔serverTime·system/configuration〕、gap-endpoints.md §5〔storageinfo 扇出〕/ §3〔组成员 includeUsers〕/ §4〔v2 permissions 并集〕等已知缺口预列）；每带 8~15 端点量级，A 层逐端点置信度 + curl 断言 + ACL 探针三件套。
- **后续程滚程留痕**：清单中非首程行不丢——T-519 PM 收口笔在 ROADMAP 建「REST 兼容跨切程」registry 段（按域分批：M18+ 每程取一带），矩阵文档为唯一事实源；`/api/v1` 并行保留（零迁移零废弃——PRODUCT.md 范围演进记录口径）。
- **边界纪律**：license 面（xray_tied）缺位登记不伪造（Q9 维持）；pro-only 无 OSS 对位端点不造（不适用态）；既有端点行为差异修复优先于新端点扩张（差异行 = 各带票最高 P 行）。

## 1. 票据表

### 1.1 FR → 票映射

| FR / 面 | 票（P） | 承载要点 | 裁决/登记锚 |
|---|---|---|---|
| FR-151（前置锚） | T-488（P0，规格票两份 + aql.md build 域增量段）/ T-489（P0，ADR-0045/0046 + 洞察聚合会签 + B-1.7 评审腿） | build-info.md + release-bundle.md + 档位核验；`internal/build`·`internal/bundle` 包边界 + build ACL 模型 + insights 新包定名 | L49；K74~K76；Q2/Q10/Q11 |
| （前置）PRODUCT.md 修订 | 已随 Q0 终裁落笔（用户 2026-09-06 14:0x）——核对归位腿在 T-519 | 五条处置与 §0.2 表一致性 | NFR-S82 门槛审计 |
| FR-152（Build-info P0） | T-507（模型+存储）/ T-508（REST 上传·append·查询）/ T-509（promote+retention+docker）/ T-510（webhook wired·断言反转④）/ T-511（AQL build 域·断言反转⑥）/ T-512（FE Builds 页·缺位解除③） | 数据模型 → REST 全链 → promote 真实腿 → 事件 wired → AQL 三入口+两端点 → FE 面 | LC-99~102；L50~L53 |
| FR-153（Release Bundle） | T-513（BE 模型+端点族）/ T-514（FE 面 + Any Distribution 勾选腿） | 最小面（Q2 待 conductor 终裁确认——PM 倾向已载 PRD）；Any Distribution 预置 BE 语义归 T-491 同场 | LC-103；L54；K76 |
| FR-154（洞察报表） | T-515（聚合快照层+查询面）/ T-516（FE 图表族） | dep K69 单源零第二通道；快照挂调度域全量类（ADR-0044 并存语义扩列） | LC-104；L55；K75 |
| FR-155（Federation/Lifecycles） | **0 票**——Q1 终裁滚 M18 单列专程；M18 预立项段备稿归 T-519 | HA 同轨（Q0 终裁进路线） | §0.2 不占号 |
| FR-156（BE 承接+权限域） | T-490（configJSON 四域+Stage+K73）/ T-491（通配桶三预置 BE 语义）/ T-492（FE 收尾：B-3.2+Last Login 列+列集） | 滚程五项①② + B-1.5/B-3.12/B-2.16/B-3.18~19 + T-468 收编；B-1.7 评审腿归 T-489 | LC-105/106/113；L56 |
| FR-157（契约勘误） | T-493（BE：url 前缀+downloadUri+System Logs 端点+?properties 注记）/ T-494（FE 缝：useAsync 双计+SystemLogs 数据源切换） | 断言反转①②；K69 单源不破 | LC-107；L57 |
| FR-158（cron/i18n 遗留） | T-495（gc 三载体+TTL wire）；切换器位置复核+faq 两问归 T-518 腿 | ADR-0044 消费面扩列 | LC-108/114；L58 |
| FR-159（webhook 二程+Replay） | T-496（Replay 行级 REST + 触发源裁剪出口 Q5 + 注册表审计）；build 域 wired 腿归 T-510（PRD 双载明示） | 断言反转④同场；ADR-0041 决策 7 | LC-109/110；L52 |
| FR-160（工程债包） | T-497（AQL/search 债）/ T-498（repo/引擎债）/ T-499（CLI/词表债·断言反转⑤）/ T-500（FE 债：Retry-After+eslint ratchet）/ T-501（vite8+MUI 段二·独占波） | M15 续滚八项 + T-444/T-452/T-477 遗留 + 文档漂移核对（归 T-498 顺腿） | L59 |
| FR-161（同型推广） | T-502（13 包型 × 三 rclass 矩阵） | Q6 建议承载（Q1 三域进留容量）——conductor 定容确认 | LC-111；L60 |
| 全量 REST 兼容跨切程（Q0 新面） | T-503（P0 差距清单矩阵）/ T-504（带 1 repositories+system）/ T-505（带 2 storage+security）/ T-506（带 3 search+老搜索） | 首程定容 4 票；registry 滚程留痕归 T-519 | §0.3；L61 联动 |
| QA/文档/收口 | T-517（QA 中期 P1）/ T-521（QA 终验 P0）；T-518（tech-writer 一票两腿 P1）；T-519（PM 收口笔 P1）；T-520（release+UAT P1） | L49~L63 逐批复核 / 收口审计 + DoD 八条 | §8 剧本 |
| 条件票 | T-522（NuGet symbol 七承——用户点名即翻） | 未触发不构成 DoD 缺口，BOARD 留痕 | Q12 |

优先级统计：**P0×8**（T-488/489/503/507/508/509/510/521）；**P1×18**（T-490/491/492/493/494/496/504/505/511/512/513/514/515/516/517/518/519/520）；**P2×9**（T-495/497/498/499/500/501/502/506/522）。

### 1.2 票据明细（AC 全文——派单直接引用；`BASE=http://127.0.0.1:8080` `ADMIN=admin:password`；FE 验收 = Playwright `web/e2e/m17/`）

**T-488 [P0] FR-151.2 规格票两份（build-info.md + release-bundle.md）+ aql.md build 域增量段**
role: reverse-engineer ｜ area: docs/reverse/（build-info.md 新建 + release-bundle.md 新建 + aql.md 增量段） ｜ dep: —（Q10 活体基线处置结论为输入：修复则 7.161 活体腿可用，未修复降级 t226〔7.84.10〕单源 + 官方文档附注）
AC:
 1. build-info.md：端点族子集表（PUT 上传/段 append 合并语义/查询族单·列表·最新/promotion 状态机/retention 形态/docker promote 语义）+ 数据模型字段集（builds/modules/dependencies/promotions/properties 表族）+ 权限面选项（仓库级 vs 独立 build 权限——两出口并列交 ADR-0045）+ webhook·AQL 联动清单（webhook.md §3.4 三事件投影 + aql.md 三入口字段集）+ Builds 页 OSS 档可用性档位核验（ADR-0033 槽联动）——逐端点置信度标注，「以核验为准」零静默升格。
 2. release-bundle.md：端点族（创建/查询/冲突语义）+ bundle 模型（名字/版本/制品清单/创建者/状态）+ 深度边界选项面（v2 signing / Distribution 服务对接的进退——Q2 材料）+ Any Distribution 预置语义 + Release Bundle 商业档位核验（ADR-0033）。
 3. aql.md 增量段：builds/modules/dependencies 三入口字段集 + build 系 include/sort 联动 + `artifacts(build)`/`build.promotions` 入口处置注记（M17 面外维持 400——翻转点留痕）+ `/api/search/buildArtifacts·dependency` wire 锚；tech-lead 就绪度确认（解锁 T-507/508/511/513）。

**T-489 [P0] ADR-0045 Build-info 域 + ADR-0046 Release Bundle 域 + 洞察聚合层会签 + B-1.7 增补评审腿**
role: architect ｜ area: DECISIONS.md（ADR-0045/0046）+ docs/design/architecture.md（build/bundle/insights 节增补） ｜ dep: —（软协作：T-488 两规格锚，Accepted 前对齐——M15 T-407/T-408 先例）
AC:
 1. ADR-0045 Accepted：`internal/build` 包边界与 DDL（表族 + 与 nodes 关联）/ REST 子集对位 / **build 数据 ACL 模型定案**（仓库级 allow() 同源 vs 独立 build 权限面——二选一落定）/ promote 动词权限门 / 与 outbox·AQL 织入面（import 禁令式包边界）。
 2. ADR-0046 Accepted：`internal/bundle` 包边界与模型 / 端点族最小面 vs 全量面（Q2 终裁输入两出口并列）/ 档位映射（ADR-0033 增补若需新槽）；**洞察聚合层会签**（K75：快照周期/触发机制〔挂 scheduler 全量类——ADR-0044 并存语义扩列 vs 独立 ticker〕/指标集/K69 单源对账面 + insights 新包定名）；**B-1.7 增补评审腿**（Q11——Administer Platform/Manage Resources 双布尔 vs 三值枚举：维持现行 + tripwire 触发器机制登记，ADR-0026 增补评审留痕）。
 3. 与 T-488 两规格一致性核对（差异留痕）+ FR-152/153/154 六票拆分边界可实现粒度确认。

**T-490 [P1] FR-156.1 configJSON 四域 round-trip + Stage 域 + K73 落定（确定层先行——滚程五项①）**
role: dev-go-core ｜ area: internal/httpapi（repositories 配置面）+ internal/repo（config 落库/回显） ｜ dep: —
AC:
 1. 表驱动：`curl -X PUT -u $ADMIN "$BASE/api/repositories/<key>"`（body 含 maxUniqueSnapshots/repoLayoutRef/blackedOut/archiveBrowsingEnabled）→ GET 回显逐域一致（**M16 T-439 漂移钉 tripwire 转红→绿**）+ 行为联动断言：blackedOut=true 时 `docker push`/PUT 上传 → 拒写（404/文案照规格票勘误核）。
 2. Stage〔原 Environments，7.161 更名〕域 wire 键定案（规格/活体对拍留痕）+ 往返一致；K73 repoLayoutRef 落定：接线布局引擎 or 钉协议默认值——票内与 architect 核定后落 ADR 勘误行 + PRD K73 回填（T-519 对账）。
 3. `make test`（race）全绿 + 既有 repositories CRUD spec 零回归。

**T-491 [P1] FR-156.2 通配桶 BE 语义（Any Local / Any Remote / Any Distribution 三预置同场）**
role: dev-go-core ｜ area: internal/auth（通配权限语义）+ internal/httpapi（permissions 面） ｜ dep: —（与 T-513 Any Distribution 预置同场同语义——BE 语义先行，FE 勾选腿归 T-514）
AC:
 1. Any Local/Any Remote 预置桶 BE 通配语义落地（or 与缺位登记同口径终裁——票内/architect 小评留痕；M16 B-2.16 解除）：权限面勾 Any Local → 无权限用户对本地仓 403/不可见**覆盖探针**（新建仓自动纳入覆盖——通配生效断言）。
 2. Any Distribution 预置桶 BE 语义同场（bundle 域数据面授权通道——T-513 端点落地后联测腿留 dep 注记）；权限矩阵动词列不因通配桶扩权（r/w/d/m/a 逐动词探针——零提权）。
 3. `make test`（race）全绿 + M7 RBAC 全量 spec 零回归。

**T-492 [P1] FR-156.4 FE 收尾腿：B-3.2 初始态 + Users Last Login 列 + 列集差异（确定层先行——滚程五项② + T-468 收编）**
role: dev-frontend ｜ area: web/src/pages/artifacts（初始态）+ web/src/pages/security/UsersPage（列） ｜ dep: —（Last Login BE 投影 T-454 已落；columnPrefs 共享层 T-387 形态复用）
AC:
 1. Playwright：制品浏览进入即首仓库自动选中 + item view 呈现（B-3.2——M16 翻正未落项收口；空仓/无权限仓边界态处理）。
 2. Users 列表 Last Login 列渲染（T-454 投影消费）+ 列选器接入（columnPrefs 显隐持久 reload 保持）；「无端点列不伪造」维持（B-3.19 列集差异对账——差异项缺位登记）。
 3. FE 五件套 + 双 locale 键化（zh/en 同票）+ SPA ≤10KB。

**T-493 [P1] FR-157 BE 契约勘误：url 前缀 + downloadUri + System Logs 进程日志端点 + ?properties 注记（断言反转①②）**
role: dev-go-core ｜ area: internal/httpapi（repositories 列表 + storage 投影 + system 日志端点）+ internal/console 日志读取面（票内核定） ｜ dep: —
AC:
 1. `curl -u $ADMIN "$BASE/api/repositories" | jq '.[0].url'` → 含 `/binflow` 前缀（baseUrl 族对齐——M1 as-built 勘误）；FileInfo.downloadUri 处置票内二选一留痕（下载语义 URI 归位 or 语义注记终裁——与 rest-api.md §3 锚对拍后落 LC 登记）。
 2. System Logs 进程日志端点真身（尾随/过滤/下载三能力——端点形态照既有 router 惯例 + 文档反转登记位）；`?properties` 分号矩阵语法注记（REST 只认逗号配对——文档面 AC 归 T-518 联动，本票落 wire 行为不变断言）。
 3. 勘误族逐条既有 spec 断言翻新（curl_compat/routes_compat 族）+ `make test`（race）全绿 + M1 序列双形态零回归。

**T-494 [P1] FR-157 FE 缝：NodeDetail useAsync 双计修正 + SystemLogsPage 数据源切换**
role: dev-frontend ｜ area: web/src/pages/artifacts（NodeDetail useAsync）+ web/src/pages/monitoring/SystemLogsPage ｜ dep: T-493（System Logs 端点）
AC:
 1. Playwright：纯浏览（只选中不下载）后 `downloads` 计数不变（useAsync 重复发射缝修正——K69 单源契约不破）+ 既有下载计数断言零回归（下载 3 次 → 详情计数=3）。
 2. SystemLogsPage 数据源切换（审计承载 → 进程日志真身；端点不可用降级路径保留——T-459 as-built 形态延续）+ `whats-new`/console.md 注记反转位交 T-518（文档腿联动留痕）。
 3. FE 五件套 + 双 locale 键化 + SPA ≤10KB。

**T-495 [P2] FR-158 gc-cron-gap 三载体 + 枚举 TTL wire 可调**
role: dev-go-core ｜ area: internal/scheduler（消费面扩列）+ internal/httpapi（maintenance 面）+ internal/remote（TTL） ｜ dep: —（调度引擎 ADR-0044 已 Accepted）
AC:
 1. 三载体 cron 配置 CRUD + 到点 dry-run 断言（Quota 百分比 / Compress Internal Database / Prune Unreferenced Data——M16 诚实缺位登记解除；调度引擎复用 + Runner 注册闭集扩列 ADR-0044 勘误行留痕）。
 2. 远端枚举快照 TTL 600s wire 可调（PUT → 回显生效 + 边界值 400 臂）；maintenance.* audit 词族在场。
 3. `make test`（race）全绿 + readonly_admin 调度 CRUD 403 探针维持。

**T-496 [P1] FR-159.2 Replay/outbox 行级 REST + 触发源裁剪出口 Q5（确定层——候 Q5 终裁，裁点 = 本票派单前）**
role: dev-go-core ｜ area: internal/webhook（outbox 行级面）+ internal/httpapi（outbox REST 面） ｜ dep: —（build 域 wired 归 T-510——PRD 双载明示；Q5 终裁为前置小裁，conductor 组织）
AC:
 1. 构造死信（消费者 500×4 重试穷尽）→ `POST`（Replay 端点，形态照票与 conductor 核定后落 LC-109 登记——零私加口径）重放 → 消费者收到 + 行状态翻转 + audit 行；行级查询端点（过滤/分页——admin 门）。
 2. 裁剪出口 Q5 落地（若裁①仅注册有源域）：无源域触发源注销/标注断言 + ADR-0041 决策 7 修订留痕；若裁②维持全量——dormant 标注审计一致（两出口 AC 二选一，终裁归位）。
 3. `make test`（race）全绿 + 既有 webhook 域 e2e 零回归（M13 dogfood 栈复用）。

**T-497 [P2] FR-160 AQL/search 债收口（aql.md V-a~V-h + "1d" 冲突 + compact 415 + QRL 三遗留）**
role: dev-go-core ｜ area: internal/search（lexer/plan/qrl）+ docs/reverse/aql.md 回写 ｜ dep: —（与 T-511 同包族先后脚——排其后再派）
AC:
 1. aql.md V-a~V-h 待验证八项活体核验归档（429 活体/6000 现值/property 数据腿/unknown 脱敏现值/$eqic 族/virtual 对拍/pattern 空集——逐项勘误或升格回写规格，置信度标注）；相对时间 `"1d"` 空格 vs 官方后缀表冲突处置（lexer 翻转点 or 规格注记——票内裁留痕）；compact 非空行体 415 实核回写。
 2. QRL 三遗留（audit 词 / LOW_PRIORITY 桶 / V-m 对拍）逐项落地或登记；既有 t419/t440 族 spec 零回归 + K63 门沿用（上限/并发/超时三件零豁免）。
 3. `make test`（race）全绿。

**T-498 [P2] FR-160 repo/引擎债（virtual echo blob + 大仓 limit 化 + X-Explode staging + migration 011 flake）+ 文档漂移四条核对**
role: dev-go-core ｜ area: internal/repo（echo/limit/archive staging）+ internal/migrate（011 阈值） ｜ dep: —
AC:
 1. `GET /api/repositories/<virtual>` echo 原始 config blob 改造（成员级联删除后失效成员不再回显——T-416 软注记解除）+ 大仓 limit 化全枚举缝（T-420/T-423 同族收口——分页枚举零全表扫描）。
 2. X-Explode-Archive staging 同族迁移（internal/repo/archive.go:1082 spool 家族——T-477 候立兑现）；migration 011 墙钟阈值满载 flake 定谳（阈值校准或标记隔离，QA 登记在案项收口）。
 3. 文档漂移四条核对增删（docker-registry.md/golang.md/nuget 补文/fern docker.mdx——m16-done 尾波可能已清偿，票内核对后增删留痕；他域归位行交 T-518）。

**T-499 [P2] FR-160 CLI/词表债（dry-run CLI 挂点 + migrate CLI 低危 + Annotate write 别名移除——断言反转⑤）**
role: dev-go-core ｜ area: cmd/（bf-cli 子命令）+ internal/httpapi（permissions wire 面）+ internal/auth（词表） ｜ dep: —
AC:
 1. Annotate 迁移 dry-run CLI 挂点（cmd/ 5 行小票——T-444 遗留：AnnotateMappingDryRun 公共 API 已备）+ migrate CLI 低危登记项落地或登记留痕。
 2. write 别名移除评估落地（K68——票内裁：移除则 PUT `"write"` → 400 + 迁移窗公告〔T-518 承载〕+ 断言反转⑤归位；维持则差异登记翻新）；wire 五词 GET 回显正名单 spec 零回归。
 3. `make test`（race）全绿 + M7/T-444 族动词矩阵 spec 翻新零断链。

**T-500 [P2] FR-160 FE 债：429 Retry-After 上 UI + eslint ratchet 37 清零**
role: dev-frontend ｜ area: web/src/lib/api.ts（ApiError 响应头承载）+ web/ 全树 lint ｜ dep: —
AC:
 1. ApiError 承载响应头 → QRL 429 面 Retry-After 数值上 UI 呈现（锚册注记解除——T-452 登记）；既有 429 spec 翻新断言新呈现。
 2. eslint ratchet 37 条清零（或阈值收紧 + 剩余清单登记——票内裁留痕，T-432① 底稿）；lint 0 issues 维持。
 3. FE 五件套 + SPA ≤10KB（清零若引包须解释）。

**T-501 [P2] FR-160 vite 8 + MUI v7→v9 段二（触全页面——独占波）**
role: dev-frontend ｜ area: web/ 构建链 + 全树（触全页面，最后位） ｜ dep: 全部常规 FE 票收口（**独占波——与所有 FE 票互斥**；非 FE 票不冲突可占 lane 2）
AC:
 1. vite 8（Rolldown）+ plugin-react 6 + MUI v7→v9 升级：全量 e2e 复跑（385+ 基线零回归）+ 四闸门 + axe 双主题双 locale + 断言零翻新（升级不改行为面）。
 2. SPA/资源门复核（构建产物增量可解释 + catalogs chunk 趋势登记）+ F1 六平台联动观察（T-520 对账）。
 3. 回滚路径留痕（lockfile 快照 + 升级分支形态——conductor 裁量）。

**T-502 [P2] FR-161 virtual 成员同型约束全包型推广（13 包型 × 三 rclass）**
role: dev-go-core ｜ area: internal/repo（validate 同型约束）+ internal/adapter（矩阵核验只读接触） ｜ dep: —（Q6 容量裁定——conductor 定容确认承载；裁出则「M17 未纳入项」显式登记归 T-519）
AC:
 1. 同型约束推广至全部 13 包型 × 三 rclass（local/remote/virtual 成员校验语义一致化——现态缺陷面窄但语义不一致收口）。
 2. 全量回归矩阵绿（真实客户端矩阵复用 M16 十协议基建——docker/mvn/npm/pip/go/cargo/conan/helm/dnf/apt/dotnet/oras + remote/virtual 双面）。
 3. `make test`（race）全绿 + 十协议 CI 矩阵复跑全绿。

**T-503 [P0] 全量 REST 兼容跨切程：差距清单矩阵（rest-compat-matrix.md）**
role: reverse-engineer ｜ area: docs/reverse/rest-compat-matrix.md（新建，活体 registry） ｜ dep: —
AC:
 1. 矩阵落盘：**Fern openapi 158 ops（BinFlow 现状底稿）× Artifactory 全量 REST 面**（docs/reverse/ 既有全部规格 + 官方 REST reference 全集逐域盘点）——逐端点四态标注（已兼容/部分兼容〔差异行级〕/缺位/不适用〔pro-only·xray_tied·物理不可行——Q9 license 族连带缺位登记〕），域分组 + 每域优先级排序列。
 2. 首程三带行集冻结（repositories+system / storage+security / search+老搜索——各 8~15 端点，A 层置信度标注；差异修复行优先于新端点扩张）；后续程 backlog registry 段建立（按域分批，M18+ 滚程留痕载体）。
 3. tech-lead 就绪度确认（解锁 T-504/505/506）+ 与 T-488 规格票零重复对账（build/bundle 域行指向 T-488 不重写）。

**T-504 [P1] REST 兼容带 1：repositories + system 面增量**
role: dev-go-core ｜ area: internal/httpapi（repositories/system 面） ｜ dep: T-503
AC:
 1. 带 1 行集落地（候选预列、以 T-503 冻结为准）：`GET /api/repositories/configurations`（admin 全量配置分组排序）/ `GET /api/system/serverTime` / system/configuration 提交面核验 / repositories 列表过滤参数闭集（type/project）差异行修复——逐端点 curl 断言 + 差异行级对账。
 2. 逐端点 ACL 探针（admin 门/匿名 401 挑战/越权 403——rest-api.md §2/§5 锚）+ 错误体 envelope 统一（`{"errors":[{status,message}]}`）。
 3. `make test`（race）全绿 + Fern openapi 同步增量（binflow.json + 158→N ops 登记矩阵回写）。

**T-505 [P1] REST 兼容带 2：storage + security 面增量**
role: dev-go-core ｜ area: internal/httpapi（storage/security 面）+ internal/audit（storageinfo 缓存快照） ｜ dep: T-503
AC:
 1. 带 2 行集落地（候选预列、以 T-503 冻结为准）：`GET /api/storageinfo` 扇出端点（缓存快照 + `POST /api/storageinfo/calculate` 手动重算——gap-endpoints §5 锚：冷缓存 503 语义）/ `?list` 深度参数族全参 / `?lastModified` / security 面差异行（groups `?includeUsers` 组成员 / v2 permissions 并集端点评估行）。
 2. 逐端点 curl 断言 + ACL 探针（storageinfo admin 门 / list 仅认证用户匿名 403）+ 与洞察聚合层（T-515）零第二通道对账（若共享快照基建，票内核定归属留痕）。
 3. `make test`（race）全绿 + Fern openapi 同步 + 矩阵回写。

**T-506 [P2] REST 兼容带 3：search + 老搜索面补全**
role: dev-go-core ｜ area: internal/search + internal/httpapi（search 面） ｜ dep: T-503, T-511（dependency 数据源先行）
AC:
 1. 带 3 行集落地（以 T-503 冻结为准）：老搜索余量端点（badChecksum / versions / latestVersion / latestVersionByProperties / archive 条目搜索——aql.md §8 全量口径 16 枚对账）；`/api/search/dependency·buildArtifacts` 归 T-511 承载（票面去重注记）；`/api/search/license` 缺位登记维持（Q9——无数据源不伪造，矩阵不适用态）。
 2. M15 §5.7 全景表 + aql.md 端点计数（16 枚）逐条对账回写；逐端点 curl 断言 + K63 门沿用 + 诚实拒绝（未实现端点 400/404 逐字）。
 3. `make test`（race）全绿 + Fern openapi 同步 + 矩阵回写。

**T-507 [P0] FR-152.1 Build-info 数据模型 + 存储 + 迁移（internal/build 新包）**
role: dev-go-core ｜ area: internal/build（新包：模型/store/migration）+ internal/migrate ｜ dep: T-488（build-info.md）, T-489（ADR-0045 Accepted）
AC:
 1. builds/modules/dependencies/promotions/properties 表族落地（DDL 照 ADR-0045；启动迁移幂等——sqlite/postgres 双库）+ 与 nodes 关联（build ↔ 制品/镜像工件，module 段挂 nodes 路径）；Module ID 字段数据面就绪（T-512 消费）。
 2. build ACL 判定面（照 ADR-0045 定案：allow() 同源 or 独立面——越权探针三臂：无权限用户 build 查询 403/零出现/列表过滤零泄漏）；table-driven 单测覆盖表族关系与级联。
 3. `make test`（race）全绿 + 纯新包零既有引擎文件改动（cmd 装配行例外——ADR-0046 拆分论证同款）。

**T-508 [P0] FR-152.2 Build REST 上传 / append / 查询族**
role: dev-go-core ｜ area: internal/build（服务层）+ internal/httpapi（build 面新 router） ｜ dep: T-507
AC:
 1. `curl -X PUT -u $ADMIN "$BASE/api/build/myapp/1" -H 'Content-Type: application/json' -d @build.json` → 2xx；`curl -u $ADMIN "$BASE/api/build/myapp/1"` 回显模块/依赖段；**段 append 合并不覆盖语义**（再次 PUT 带 dependencies 段 → 合并断言——照 build-info.md 冻结）；列表/最新端点绿（分页/过滤照规格票）。
 2. 错误面诚实（畸形 body 400 逐字/不存在 404/越权 403）+ 上传计数与耗时指标（Prometheus 既有口径延伸——builds.put/builds.get 族）；(环境可得时) `jf rt build-upload` 条件腿真实走通——四门自测留痕。
 3. `make test`（race）全绿 + curl 契约测试（行为命名新文件）。

**T-509 [P0] FR-152.2 promotion + retention + docker promote 腿**
role: dev-go-core ｜ area: internal/build（promotion/retention）+ internal/repo（跨仓迁移载体——copy/move 复用非重构）+ internal/httpapi ｜ dep: T-508
AC:
 1. `curl -X POST -u $ADMIN "$BASE/api/build/promote/myapp/1" -d '{"status":"released","targetRepo":"rel-docker"}'` → 200 + 状态翻转 + audit 行 + promote 动词权限门（无位用户 403 探针）。
 2. **docker promote 真实腿**：build 关联镜像跨仓晋升后 `docker pull $BASE/rel-docker/myapp:1` 成功且**删除本地缓存后复拉仍成功**（manifest+blob 全量迁移 sha256 对账）；generic 制品 promote 同场（多包型 promote 语义照规格票）。
 3. retention 删除保留窗外 build（形态照规格票——保留窗参数/删除 audit）+ promotion 大 build 迁移零 5xx（NFR-P80 载体）；`make test`（race）全绿。

**T-510 [P0] FR-152.3/FR-159.1 webhook build 域 dormant→wired（断言反转④）**
role: dev-go-core ｜ area: internal/webhook（build 域织入 + 注册表）+ internal/build（事件发射缝） ｜ dep: T-508, T-509（promoted 事件 dep promote 落地）
AC:
 1. build 域三事件 wired（uploaded/deleted/promoted——webhook.md §3.4：data = build_name/build_number/build_started/build_repo）：真实消费者（local receiver）收到 payload + HMAC 签名验证通过（openssl 验签逐字——M13 T-362 形态复用）；**ADR-0041 schema 零变化**（envelope 七字段断言）。
 2. 13 域 66 事件型注册表 wired/dormant 标注与实际一致（零伪造审计——build 域翻 wired，其余维持 dormant 如实）；criteria 面 build scope（anyBuild/selectedBuilds/include-excludePatterns）生效断言。
 3. `make test`（race）全绿 + M13 webhook e2e 零回归。

**T-511 [P1] FR-152.3 AQL build/module/dependency 域 + /api/search/buildArtifacts·dependency（断言反转⑥）**
role: dev-go-core ｜ area: internal/search（build 域入口）+ internal/httpapi（search 面） ｜ dep: T-488（aql.md 增量段）, T-507（数据源）
AC:
 1. `builds.find(...)` 类查询绿（三入口字段集照 aql.md 增量段冻结：builds/modules/dependencies）+ dependency include 联动；`POST /api/search/buildArtifacts`（body 形态照锚）+ `GET /api/search/dependency` 落地；**aql.md §2.1「远期 400」行翻转**（三入口 400→查询绿——断言反转⑥登记）。
 2. 未支持域诚实拒绝：promotion/releasebundle/sensitive 入口 400 parse error 维持 + 注记（数据面已备 promotion 表——M18+ 翻转点留痕）；`/api/search/license` 缺位登记维持（Q9）。
 3. ACL 越权探针（builds 域要求 name/number/repo 字段行级过滤——aql.md §6 锚；越权 build 行零出现）+ K63 资源门沿用 + 查询 P95 ≤300ms 量级（单 build 十模块百依赖——NFR-P80）+ `make test`（race）全绿。

**T-512 [P1] FR-152.3 FE Builds 页 + 搜索范围页签 + Module ID 字段（缺位解除③）**
role: dev-frontend ｜ area: web/src/pages（BuildsPage 新建 + search 范围页签 + artifacts 详情 Module ID） ｜ dep: T-508（BE 面）, T-507（Module ID 数据）
AC:
 1. Playwright：Builds 页（列表/详情/模块列表渲染——t226 对照 + 断言地基照 build-info.md 档位核验）；搜索范围页签 Builds 缺位解除（M15 既定 R2 缺位其一——登记态/实态双留痕）；详情 Module ID 字段呈现（M16 stay-out 登记解除——无 build 关联时空态不伪造）。
 2. build promote 动作 FE 入口（若 ux 册定案承载——票内与 ux 核定；未定则列表/详情只读面先行留痕）；build 事件时间线（消费 audit 面——零新端点优先）。
 3. FE 五件套 + 双 locale 键化 + SPA ≤10KB + 缺位解除归位登记（解除票与登记态可回溯——QA 终验 L61 对账）。

**T-513 [P1] FR-153 Release Bundle 最小面：模型 + 创建/查询端点族（internal/bundle 新包）**
role: dev-go-core ｜ area: internal/bundle（新包）+ internal/httpapi（bundle 面新 router）+ internal/migrate ｜ dep: T-488（release-bundle.md）, T-489（ADR-0046）, Q2 终裁（conductor 确认最小面——PM 倾向已载 PRD，派单前小裁）
AC:
 1. bundle 实体（名字/版本/制品清单/创建者/状态）+ `curl` 创建（含制品清单）→ 2xx + 查询回显逐项一致；重复版本冲突语义照规格票；列表/单查端点绿。
 2. ACL 探针（越权用户零出现 + bundle 管理 admin/manager 门——NFR-S81）+ v2 signing/Distribution 对接**不做**留痕（Q2 出口①——release-bundle.md 深度边界节）。
 3. `make test`（race）全绿 + 纯新包零既有引擎文件改动。

**T-514 [P1] FR-153.2/.3 FE bundle 面 + Any Distribution 预置勾选（缺位解除③并腿）**
role: dev-frontend ｜ area: web/src/pages（BundlesPage 新建）+ web/src/pages/security（PermissionEditorPage Any Distribution 勾选腿） ｜ dep: T-513, T-491（通配桶 BE 语义同场）
AC:
 1. Playwright：bundle 列表/详情页渲染（形态照规格票 + t226 对照）；权限两步弹窗 Any Distribution 勾选 → 授予面覆盖断言（被授权用户可见/可操作探针——B-2.16 缺位登记解除，与 Any Local/Any Remote 同口径）。
 2. 缺位解除归位登记（登记态/实态双留痕）；无 bundle 数据时空态不伪造。
 3. FE 五件套 + 双 locale 键化 + SPA ≤10KB。

**T-515 [P1] FR-154.1 洞察聚合快照层 + 查询面（internal/insights 新包——定名从会签）**
role: dev-go-core ｜ area: internal/insights（新包：快照模型/任务/查询）+ internal/scheduler（消费接线——全量类闭集扩列 ADR-0044 勘误行）+ internal/httpapi（insights 面新 router） ｜ dep: T-489（K75 会签定案）
AC:
 1. 时序聚合快照层（周期/指标集照 K75 定案）：数据源 = M16 per-node 统计四列（K69 单源契约——**零第二计数通道**票内声明）+ 存储增长/Top 仓/Top 制品/下载趋势聚合任务；触发机制落地（挂调度域全量类 or 独立 ticker——照会签定案；手动触发等价腿测试加速）。
 2. 查询 REST 面（dashboard 图表数据源——形态照 K75）+ 快照任务不阻塞在线路径（NFR-P81）+ maintenance.* 词族事件（任务执行/失败）+ ACL（admin/manager 门——非授权 403）。
 3. 单源一致对账：下载夹具 N 次 → 快照计数与 FileInfo `downloads` 对账一致（table-driven）+ `make test`（race）全绿。

**T-516 [P1] FR-154.2/.3 FE 洞察图表族**
role: dev-frontend ｜ area: web/src/pages/DashboardPage（图表族）+ 新图表组件 ｜ dep: T-515
AC:
 1. Playwright：四图表渲染（下载趋势/存储增长/Top 仓/Top 制品）+ 空数据空态 + 双 locale 双主题（图表标签随 i18n——catalogs 双包键化）；数据与 BE 快照对账断言（下载 N 次后趋势计数=N——单源腿）。
 2. 既有 dashboard 去重率口径不变（A1 注记——零回归断言）；快照手动触发入口（若 ux 册承载）。
 3. FE 五件套 + SPA ≤10KB（图表库增量单独登记——若引第三方库须 F1 趋势解释 + 会签）。

**T-517 [P1] QA 中期回归（逐批复核）**
role: qa-engineer ｜ area: 测试矩阵（L50~L58 已落面 + t226/7.161 对照） ｜ dep: W1~W10 已合入面（T-490~T-516 主体）
AC:
 1. 逐批复核：L50/L51（Build REST 全链 + promote/docker 真实腿——删缓存复拉独立复验）/ L52（webhook wired + Replay 死信重放 + 注册表审计）/ L53（AQL build 域 + ACL 探针 + 诚实拒绝）/ L54（bundle 全链 + Any Distribution）/ L55（单源对账）/ L56~L58（确定层三线）+ jf CLI 条件腿（环境可得时）。
 2. M1~M16 P0 双形态增量窗口回归 + 断言反转①~⑥现值核实 + E1/E6/smu-* 锚册零倒退 + 双 locale a11y 抽样。
 3. 缺陷登记与回归报告落盘（reports/）——诚实汇报（跳过/在途注明）。

**T-518 [P1] 文档票（两腿——票内先后笔）**
role: tech-writer ｜ area: docs/user/ + fern/pages 双树（Build-info/Release Bundle/洞察指南 + 缺位解除公告 + cron·i18n 增量 + 勘误注记反转） ｜ dep: 腿① T-508/T-512/T-515；腿②候 T-514/T-516/T-501 合入
AC:
 1. 腿①：Build-info 接入指南（CI 集成/curl/jf/copy 语义/promote 与 docker 镜像随迁）+ Builds 页用户文档 + 洞察报表指南（单源口径说明）——客户端命令实测可复跑。
 2. 腿②：Release Bundle 指南 + **缺位解除用户可见公告**（Module ID / Any Distribution / Builds 页签 / System Logs 注记反转逐一明示——零票号零里程碑号）+ faq 两问入册 + 语言切换器位置复核结论落册（ux 会签）+ `?properties`/write 别名移除（若 T-499 裁移除）迁移窗公告 + `make docs` 零断链 + Fern 双树镜像。
 3. whats-new.md 增量（用户可见变化）+ REST 兼容跨切程增量端点入 api-reference（与 T-504~506 同步——矩阵对账）。

**T-519 [P1] PM Q 终裁联动收口笔**
role: product-manager ｜ area: docs/prd/milestone-17.md（收口笔）+ ROADMAP（M17 未纳入项 + M18 预立项段 + REST 跨切程 registry） ｜ dep: T-517 + 各域 as-built 素材
AC:
 1. Q2~Q12 逐项归位（Q2 最小面/Q5 裁剪出口/Q6 承载确认/Q9 缺位登记/Q10 活体处置/Q11 维持现行/Q12 条件池维持）+ §7 总账回写 + K74~K76 回填 + 断言反转①~⑥归属预对账（交 T-521 终审）。
 2. ROADMAP「M17 未纳入项」起草（沿 T-395/T-427/T-460 先例）：**Federation/Lifecycles 与 HA 本体 M18 预立项段备稿**（Q1 滚 M18 + Q0 进路线双依据）+ **REST 兼容跨切程 registry 段**（首程带 1~3 之外 backlog——T-503 矩阵为源）+ Go/Terraform/GitLFS/AI-ML 包型域滚程登记。
 3. PRODUCT.md 范围演进记录对账（Q0 终裁五条处置与 M17 实际承载一致性核对）+ 文档漂移归位行对账。

**T-520 [P1] release 烟测 + UAT 随里程碑 PR**
role: release-engineer ｜ area: deploy/ + charts/ + CD 链 ｜ dep: 全部实现票 + T-518
AC:
 1. 部署烟测（compose/k8s/systemd 矩阵）+ UAT 随里程碑 PR——**Build-info 全链与洞察图表在 UAT 链取证**（jf/curl build 上传/promote + 图表单源呈现）+ version sha 翻转 + Chart bump 判据（新域 behavior 多处——预计 bump）。
 2. 资源门三连（footprint ≤100MB / check-size ≤120MB / 冷启动 <2s）+ F1 六平台趋势登记（T-501 升级联动解释）+ SPA 累计趋势复核 + server 原始线观察；CircleCI 挂账 re-run 补证核对（m16-done 挂账项顺腿）。
 3. t381 残留处置核对（M16 Q12 移交项——随票处置或续登记 BOARD 留痕）。

**T-521 [P0] QA 终验**
role: qa-engineer ｜ area: 全量矩阵 ｜ dep: 全部 + T-520
AC:
 1. L49~L63 全量 + **断言反转①~⑥归属审计 100% M17 票** + **缺位解除四项归位审计**（Module ID / Any Distribution / Builds 页签 / System Logs 注记——解除票与登记态可回溯）+ REST 兼容矩阵带 1~3 端点逐条对账（T-503 registry 终态）。
 2. DoD 八条逐条 + NFR-P80~P82/S80~S82 达标归档（**NFR-S82 门槛纪律审计**：主轴票派发时间 ≥ PRODUCT.md 修订落笔时间〔2026-09-06 14:0x〕）+ M1~M16 P0 双形态全量复跑 + FE 票服务端 diff=0 复核（BE 腿除外）+ m17-done 就绪判定。

**T-522 [P2·条件] NuGet symbol server 七承转正（用户点名即翻）**
role: dev-registry-adapter ｜ area: internal/adapter（nuget symbol 面——.pdb/GUID 路径） ｜ dep: 用户点名触发；插空 BE lane（P2 独占波 lane 2 可承载）
AC:
 1. mini as-built 规格随票（T-293 终裁口径沿用）+ symbol 上传/拉取真实腿（.pdb GUID 命中——真实 nuget/dotnet 客户端）+ 既有 nuget 面零回归。未触发不构成 DoD 缺口（BOARD 留痕）。

**不占号 slot**：Q10 活体基线修复（conductor 决策）/ Federation·HA M18 预留登记（T-519 承载）/ E7 toast（候用户信号）/ Tokens 字段核验（候活体源）/ license 公钥 config 覆盖 ADR（候立项）/ B-2.8 有效权限 AG 网格（候用户信号）。

## 2. 波次与并行分区（全宽 2；波内 area 互斥；FE 主线一波一票错峰；vite8+MUI 独占波）

| 波 | lane 1 | lane 2 | 要点 |
|---|---|---|---|
| **W0** | T-488 [P0] 规格票两份 + aql 增量段（rev） | T-489 [P0] ADR-0045/0046 + 会签锚 | 前置锚双票并行；软协作 build-info 锚；Q10 活体处置为 T-488 输入 |
| **W1** | T-490 [P1] configJSON round-trip（确定层先行） | T-503 [P0] REST 差距清单（rev——零代码冲突） | 确定层开工不候锚；跨切程锚票同步起步 |
| **W2** | T-491 [P1] 通配桶三预置 BE | T-492 [P1] FE 收尾腿（B-3.2+Last Login） | auth/permissions 面 vs FE artifacts/security 页 |
| **W3** | T-493 [P1] BE 契约勘误（断言反转①②） | T-507 [P0] build 数据模型（internal/build 纯新包） | W3 主轴开工（dep W0 锚） |
| **W4** | T-508 [P0] Build REST（httpapi build 新面） | T-494 [P1] FE 勘误缝（dep T-493） | 新 router 文件 vs FE |
| **W5** | T-509 [P0] promote+retention+docker | T-515 [P1] 聚合层（internal/insights 新包 + 新 router——与 build 面零共享文件注记） | promote 载体复用 copy/move；洞察 BE 错峰 |
| **W6** | T-510 [P0] webhook build wired（断言反转④） | T-513 [P1] bundle BE（internal/bundle 新包；Q2 派单前小裁） | webhook+build 织入 vs 新包 |
| **W7** | T-511 [P1] AQL build 域（断言反转⑥） | T-495 [P2] gc 三载体+TTL（P2 插空） | internal/search vs scheduler/remote |
| **W8** | T-512 [P1] FE Builds 页（缺位解除③） | T-496 [P1] Replay REST（Q5 派单前小裁） | FE 新页 vs webhook/outbox 面 |
| **W9** | T-514 [P1] FE bundle 面 + Any Distribution 腿 | T-517 [P1] QA 中期回归 | FE vs QA（L50~L58 已落面复核窗） |
| **W10** | T-516 [P1] FE 洞察图表（dep T-515） | T-497 [P2] AQL/search 债（与 T-511 同族先后脚 ✓） | FE vs search 债 |
| **W11** | T-504 [P1] REST 带 1（repositories+system） | T-498 [P2] repo/引擎债 | httpapi 面 vs internal/repo |
| **W12** | T-505 [P1] REST 带 2（storage+security） | T-499 [P2] CLI/词表债（断言反转⑤） | httpapi 面 vs cmd/+auth 词表 |
| **W13** | T-506 [P2] REST 带 3（search+老搜索，dep T-511 ✓） | T-500 [P2] FE 债（Retry-After+eslint） | search 面 vs FE |
| **W14** | T-502 [P2] 同型推广 13 包型矩阵 | T-518 [P1] 文档票（两腿） | repo/adapter 矩阵 vs docs |
| **W15** | T-501 [P2] **vite8+MUI 段二（独占波——与所有 FE 票互斥）** | T-519 [P1] PM 收口笔（非 FE 可占） | 触全页面收口位 |
| **W16** | T-520 [P1] release 烟测+UAT | — | 单票波（尾部） |
| **W17** | T-521 [P0] QA 终验 | — | 单票波；m17-done 就绪判定 |
| 波外 | T-522 [P2·条件] NuGet symbol 七承（用户点名即翻——插空 BE lane） | — | 未触发不构成 DoD 缺口；BOARD 留痕 |

**插空纪律**：任一波提前收口时补位序 = T-522（条件触发——BE lane 任意空位）→ T-495/T-497/T-498/T-499（P2 零依赖或先后脚已备可前移）→ T-502（P2 尾波弹性——超载首裁出，显式登记）。W11~W13 REST 带票可整体前移至 T-503 收口后任意 BE 空位（均 dep 单锚）。

## 3. 依赖图与关键路径

```
W0  T-488(规格两份+aql增量) ──软协作── T-489(ADR-0045/0046+洞察会签+B-1.7)
     │ dep            │ dep
W1  T-490(configJSON) T-503(REST差距清单)      [确定层/跨切程并行起步]
     │                │
W2  T-491(通配桶)      T-492(FE收尾)            │
     │                │
W3  T-493(BE勘误)     T-507(build模型 ← T-488+T-489)
     │                │                          │
W4  T-508(Build REST ← T-507)   T-494(FE缝 ← T-493)
     │                │
W5  T-509(promote+docker ← T-508)  T-515(聚合层 ← T-489会签)
     │                │                │
W6  T-510(webhook wired ← T-508+T-509)  T-513(bundle BE ← T-488+T-489+Q2)
     │                │                │
W7  T-511(AQL build域 ← T-488+T-507)  T-495(gc三载体 P2)   │
     │                │                │
W8  T-512(FE Builds ← T-508)  T-496(Replay ← Q5)           │
     │                │                │
W9  T-514(FE bundle+AnyDist ← T-513+T-491)  T-517(QA中期 ← 已落面)
     │                │
W10 T-516(FE 图表 ← T-515)  T-497(AQL债 P2)
     │
W11 T-504(REST带1 ← T-503)  T-498(repo债 P2)
     │
W12 T-505(REST带2 ← T-503)  T-499(CLI债 P2)
     │
W13 T-506(REST带3 ← T-503+T-511)  T-500(FE债 P2)
     │
W14 T-502(同型推广 P2)  T-518(文档两腿)
     │
W15 T-501(vite8+MUI 独占波 ← 全部FE收口)  T-519(PM收口笔)
     │
W16 T-520(release ← 全部实现票 + T-518)
W17 T-521(QA 终验 ← 全部 + T-520) → m17-done
波外 T-522(NuGet symbol——用户点名)；Q10/Federation·HA/E7/Tokens/license ADR/B-2.8 不占号
```

**关键路径 = Build-info 六票串行链**（T-488/489 锚 → T-507 → T-508 → T-509 → T-510 → [T-511/T-512 分支]）→ FE 收口 → T-501 独占波 → T-520/521。洞察（T-515→516）与 bundle（T-513→514）两支挂 lane 2 与主链错峰，**无任何支线反压主链**（T-514 dep T-491 已在 W2 化解；T-506 dep T-511 波序已排）。REST 跨切程三带（W11~W13）与债包（W7/W10~W13 lane 2）为纯尾部弹性段——超载时首裁出（显式登记不无界滚）。

## 4. 风险登记

| # | 风险 | 缓解 |
|---|---|---|
| R1 | **前置锚双票延期平移**——T-488 延误阻塞 FR-152/153 全链（T-507/508/511/513 全 dep）；T-489 同；活体基线 Q10 未修复降级 t226 单源 | W0 双票零外部依赖即可开工；Q10 降级路径内置（附注零静默升格）；锚延误时确定层（W1~W2）+ REST 清单（T-503）先行消化——lane 排布已保证锚前有 2 波确定层缓冲 |
| R2 | **Build-info 六票串行链工期**——W3→W8 决定性路径，任一票回炉平移全链（docker promote 真实腿最重） | 压缩选项三处 conductor 裁量：① T-511/T-512 与 T-510 并行提前（T-511 dep T-507 非 T-509——promoted 事件仅 T-510 依赖）；② T-509 retention 腿可拆尾腿随 T-511 波消化；③ FE 三页（T-512/514/516）若有空档可与 QA 中期换序；qa 打回 ≥3 次回炉评估（SPRINT-LOOP） |
| R3 | **Q2/Q5/Q6 三个小裁点滞留**——Q2（bundle 深度）阻塞 T-513 派单、Q5（触发源裁剪）阻塞 T-496、Q6（同型推广承载）阻塞 T-502 | 三裁均为 PM 倾向已载（最小面/裁剪/承载），conductor 随派单组织确认即可（AskUserQuestion 一窗三裁建议）；未裁先派单的票以倾向态开工 + 终裁归位（差异腿补做） |
| R4 | **洞察单源纪律**——聚合快照若另建计数通道即破 K69（M16 一鱼两吃架构债复发） | T-489 会签 K75 定案前置（数据源=nodes 四列唯一）+ T-515 AC 票内声明单源契约 + 对账断言（夹具 N 次 → 三面同值）+ QA 中期 L55 复核；与 T-505 storageinfo 快照的共享面票内核定归属 |
| R5 | **build ACL 新泄漏面**（NFR-S80）——build/bundle 数据系最宽新泄漏面 | ADR-0045 定案 allow() 同源优先 + 每票硬 AC 越权探针三臂（403/零出现/列表过滤）+ QA 终验 L61 归位审计；AQL builds 域行级过滤字段要求（aql.md §6 锚）进 T-511 AC |
| R6 | **REST 兼容跨切程范围失控**（Q0 全量解禁 vs 单程容量）——全量面 = 无限回归矩阵（PM 原顾虑） | 首程定容 4 票硬边界（清单 + 三带 8~15 端点/带）+ 矩阵 registry 四态标注（不适用态消化 pro-only/xray_tied）+ 后续程分批留痕（T-519 registry 段 + 每程一带）；差异修复行优先——不盲目扩张新端点 |
| R7 | **httpapi 共享面冲突**——本程 httpapi 腿 12 张（各面 router 文件族） | 波次表已按文件族错峰（W1 repositories 配置 / W2 permissions / W3 repositories·storage·system / W4 build 新面 / W5 insights 新面注记 / W6 bundle 新面 / W7 search·maintenance / W8 outbox / W11~W13 各面错峰）；新 router 文件波内并行均注记零共享文件 |
| R8 | **vite8+MUI 独占波后置风险**——升级触全页面，e2e 复跑量大且可能反压 release | 独占波 W15 固定（全部 FE 收口后）+ 回滚路径留痕（lockfile 快照）；若升级失败回退维持 v7 现栈 → 「M17 未纳入项」显式登记（不阻塞 m17-done——P2 弹性位） |
| R9 | **资源门与 SPA 累计**——三新 FE 页 + 图表库 + server 新增三包（build/bundle/insights） | FE 每票 ≤10KB + 图表库引包须 F1 解释与会签；BE 三新包纯 Go 零重依赖自查进票 AC；release 票 F1 六平台趋势 + server 原始线观察（T-429 遗留延续） |
| R10 | **条件票与挂账触发态**——T-522/E7/Tokens/license ADR/B-2.8 未触发即 DoD 外；Q10/CircleCI/DNS+SG 挂账 | 未触发 BOARD 留痕（PRD §2.1 行 L 口径）；Q10 归 conductor（不占号）；CircleCI re-run 补证与 DNS/SG 两前置顺腿 T-520 核对 |

## 5. 首派建议（W0/W1）

- **W0（即派）**：**T-488**（reverse-engineer——派单附 PRD §4.2 行为规格要点 + webhook.md §3.4/aql.md §2.1 既有锚 + Fern openapi 158 ops 索引 + t226 容器恢复动作链〔T-381 §0 先例〕+ Q10 处置结论〔conductor 先裁〕+ 官方文档唯一行为基准先例注记）+ **T-489**（architect——派单附 PRD §4 全项 + ADR-0044 体例先例 + internal/ 包结构现状 + K74~K76 三行 + B-1.7 评审材料〔M16 T-453 tripwire as-built〕+ 与 T-488 软协作注记）。
- **W1（W0 收口即派，确定层不候锚）**：**T-490**（dev-go-core——派单附 M16 T-439 tripwire as-built + K73 两出口 + 7.161 Stage 更名证据行）+ **T-503**（reverse-engineer——派单附 Fern openapi binflow.json 全量 + rest-api.md/gap-endpoints.md 既有缺口预列 + 官方 REST reference 索引〔docs.jfrog.com llms.txt〕+ 四态标注体例）。
- 插空候选（全程）：T-495/T-497/T-498/T-499（P2 零依赖可前移）/ T-502（尾波弹性）/ T-522（条件触发即插 BE 空位）。
- 派单纪律（全程）：no-git 条款维持；FE 票逐张附「五件套 + **双 locale 键化**（zh/en catalogs 同票——assert-i18n 闸零新硬编码）」；BE 票逐张附「table-driven + ACL 零泄漏探针 + race 全绿」三件套；T-507/508/509 加「真实客户端腿（docker 删缓存复拉 + jf 条件腿）」；T-509 加「promote 载体复用 copy/move 非重构 + 零 5xx」；T-510 加「ADR-0041 schema 零变化 + 注册表零伪造」；T-515 加「K69 单源契约票内声明 + 对账断言」；T-504~506 加「逐端点置信度 + Fern openapi 同步 + 矩阵回写」；T-501 加「回滚路径 + 断言零翻新」。

## 6. 估票对比与取舍留痕（对 PRD §1.3 分票提示逐条）

| PRD 提示 | 拆票处置 | 取舍理由 |
|---|---|---|
| FR-151 拆 ~3 票 | **2 票**（T-488 规格两份合一 + T-489 ADR 双合一 + 会签/评审腿并入） | Q0 已终裁（PRODUCT.md 已落笔）——151.1 修订稿票退役，核对腿归 T-519；规格两份同 reverse 域同体例合一（M16 T-435 三份一票先例）；洞察会签不单列 ADR（PRD 明示） |
| FR-152 拆 8~10 票 | **6 票**（含锚 T-488/489 则 8——线内） | 主链六票各为可独立 review 面；webhook wired 独立（断言反转④ + 双 FR 承载 PRD 明示）；jf 条件腿归 T-508/509 AC 非独立票 |
| FR-153 拆 4~5 票 | **2 票 + Any Distribution 双载归位** | 最小面（Q2 出口①）本体小：BE 一票（模型+端点族）+ FE 一票；Any Distribution BE 语义归 T-491（与 Any Local/Remote 同场同语义——PRD 明示「同场」）、FE 勾选腿归 T-514——拆通配桶独立票即破「同场」 |
| FR-154 拆 3~4 票 | **2 票 + 会签腿归 T-489** | 聚合层与查询面同包同票（拆开即先后脚）；图表族 FE 一票；档位/触发机制等架构项全会签承载 |
| FR-155 = 0 或 5~8 | **0 票** | Q1 终裁裁出（三域进）——滚 M18；HA 同轨 Q0 终裁；M18 预立项段备稿归 T-519（不占号） |
| FR-156 拆 3~4 票 | **3 票**（round-trip / 通配桶 / FE 收尾） | B-1.7 评审腿归 T-489（architect 面——FE tripwire 已备仅评审）；B-3.2+Last Login+列集合一 FE 票（M15 T-417 同面合票先例） |
| FR-157 拆 1~2 票 | **2 票**（BE 勘误 / FE 缝） | System Logs 端点（BE）与 SystemLogsPage 切换（FE）分 role 必拆；url/downloadUri/?properties 三小项并 BE 票 |
| FR-158 拆 1~2 票 | **1 票 + 文档腿归 T-518** | gc 三载体+TTL 同 scheduler 面；切换器复核+faq 两问系文档/ux 面归 T-518（tech-writer + ux 会签） |
| FR-159 拆 2~3 票 | **1 票 + build wired 腿归 T-510** | PRD 明示双载（「build 域随 FR-152 wired」）；Replay+裁剪出口同 webhook/outbox 域一票 |
| FR-160 拆 3~4 票 | **5 票** | vite8+MUI 触全页面必须独占波独立；CLI/词表债 area 独立（cmd/+auth）；AQL/repo/FE 债按包族三票——五票均 P2 尾波弹性，超载可裁（显式登记） |
| FR-161 拆 1~2 票 | **1 票** | 同型推广=validate 语义一点 + 矩阵复跑（基建已备） |
| REST 兼容（Q0 新面，PRD 未列） | **4 票首程定容** | 清单票（锚）+ 三带票（按域渐进）；后续程滚程留痕 registry（§0.3）——超带则带 3（P2）首裁出 |
| QA 2 / tech-writer 1~2 / release 1 / PM 1 | T-517+T-521 / **T-518 一票两腿** / T-520 / T-519 | tech-writer 两腿解锁时序差票内先后笔吸收（M15/M16 先例）；QA 中期 W9（主轴+确定层已落面复核窗） |
| 条件票 slot | T-522 占号；E7/Tokens/license ADR/B-2.8 不占号 | 仅 NuGet symbol 有明确触发路径（用户点名即翻——Q12 维持确认）立实体条件票；余四项无触发信号不立（M14 D3 先例） |

## 7. 歧义与口径登记（不阻塞拆票，交 conductor/PM）

1. **REST 兼容跨切程的 FR/LC 归位**：Q0 终裁新增面超出 PRD v1.0 的 FR 行集（行 M/§2.2 被推翻）——本拆以「跨切程首程」落位 4 票（T-503~506），**建议 PM 下版 PRD 增补 FR-162（全量 REST 兼容首程）+ LC-115~117 行**；效力序：用户终裁（BOARD 2026-09-06 段）> 本拆票落位。
2. **Q2/Q5/Q6 三小裁的裁点前移**：PRD 建议 Q2 裁点 = 规格票产出后（T-488 已含深度边界选项面）、Q5 = T-496 拆票前、Q6 = Q1 定容后随拆票——本拆已按倾向态排票（最小面/裁剪/承载），conductor 可在 W0 派单同时一窗三裁（R3）。
3. **洞察快照与 storageinfo 快照的归属**：T-515（K69 nodes 四列源）与 T-505（storageinfo 用量扇出——gap-endpoints §5）若共享缓存快照基建，归属与包边界票内核定（T-489 会签预留；两票 AC 均注记对账）。
4. **build promote 的 FE 入口承载**：PRD FR-152 AC5 未明示 FE promote 动作按钮——本拆归 T-512 票内与 ux 核定（未定则只读面先行）；若 ux 册定案承载完整操作面，建议补 FE 小票（插空 FE 空位）。
5. **`/api/search/dependency·buildArtifacts` 双 FR 承载**：FR-152（随域联动）与 REST 带 3（T-506 老搜索补全）交集——本拆归 T-511（build 数据源同场），T-506 票面去重注记；PM 下版加互指。
6. **票号保留段核对**：编号自 T-488 起（conductor 指定）——T-481~T-487 未见 BOARD 占用；收编时若有在途票占用该段，本批顺延核（结构不变）。
7. **vite8+MUI 失败回退的 DoD 口径**：升级票（T-501）为 P2 弹性位——若升级失败回退 v7 现栈，「M17 未纳入项」显式登记**不阻塞 m17-done**（conductor 裁量确认；PRD DoD 未明示该口径）。

