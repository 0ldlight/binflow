# M16 拆票日志（tech-lead，2026-09-02）

> 输入：docs/prd/milestone-16.md v1.1（conductor 已审定转正——七裁定落章 + LC-80~98 零待裁 + 两程结构）+ BOARD.md「M16 票据」段（T-434 登记 + 七终裁）+ reports/m16-parity-audit-material.md（A/B/C/D 四件套——批次②③④素材源）+ reports/agents/T-434.md（批次① as-built——遗留〔?focus= 发射端等〕入后续票）+ docs/M15-SPLIT.md 体例先例 + 代码锚点核实（DECISIONS.md 最新 ADR-0043 → **ADR-0044 空位**、internal/ 无 scheduler 包〔新包定名归 ADR〕、internal/httpapi/replication_block.go Test 路由族在案、web/src 页面族 = repositories/security/artifacts/governance/monitoring/admin/search、web/e2e/m16/ 已建）。
> 产出：BOARD.md「### M16 票批 v1（tech-lead）」节（T-435~T-468，本日志为拆票依据与派发建议）。
> 票数对齐：PRD §1.3 估 30~40（含条件票 slot），实拆 **34 票**（P0×4 / P1×25 / P2×5——含波外条件票 T-467/T-468；E7 toast / license 公钥 ADR / t381 清理〔Q12〕不占号）+ 批次① T-434（已落）= 里程碑 35 票——PRD 线内。对账：FR-150 拆三（引擎/三消费面 BE/FE 消费面）+ FR-149 拆二 + FR-148 副线三（QRL+UI 族+dates 合一收尾——M15 T-417 同面合票先例）抵消 FR-143/145 各拆四的上浮。

## 0. 范围与拆票基线

**M16 = 控制台 full-parity 收口大程（两程结构之前程）**：批次① 已落（T-434——四项全对齐 + 锚册 v1.31）；余量 = 前置锚三票（规格增量 / ADR-0044+双会签 / parity 册 v1.2）+ 主轴批次②③④ FE（12 票）+ 后端小域（统计基建/Annotate/Last Login）+ cron 调度域（三票）+ 远端浏览（三票）+ AQL 副线（三票）+ i18n 双语（两票·独占波）+ 收口四票（QA 中期/文档/PM/release）+ QA 终验 + 条件池。断言反转归属：②搜索列集→T-449、③modal 880/重置钮→T-441/T-439、④用户组内联卡→T-453、⑤write 动词→T-444、⑥E6 双语→T-463/464、⑦cron 推翻→T-446/450（①已落 T-434）。

拆票纪律：
- **B0 前置锚双票并行**（conductor 指令）：T-435 规格增量段三份（reverse）+ T-436 ADR-0044（architect——K68/K69 双会签锚同场）——软协作缝 = cron 表达式子集锚（Accepted 前对齐，不设硬 dep，M15 T-407/T-408 先例）。B1 紧随：T-437 parity 册 v1.2（批次②~④断言地基——Q8/Q9 断言冻结前落）+ T-438 统计基建（**先行**——批次③ Downloads 字段族与 AQL usage 共依赖，K69 定案即动工）。
- **全宽 2**（沿 M11~M15 口径），波内 area 互斥；**FE 主线一波一票错峰**（批次②③④串行：B2~B4 → B5~B8 → B9~B12；FR-147/150 FE 腿 B13~B14；i18n B15~B16）；**i18n 独占波**（全树文案外提——与所有 FE 票互斥；非 FE 票不冲突可占 lane 2）。FE 主线 15 波串行链（B2→B16）为决定性路径。
- **area 族**：web/src 按页面组分票（repositories 表单族/列表族、artifacts NodeDetail/PropertiesTab、search+AppShell、security 用户组/权限、ProfilePage+AppShell 帮助、monitoring+admin、governance、共享分页组件、i18n 全站）｜ internal/storage+httpapi（统计基建）/ internal/auth+httpapi+migrate（Annotate）/ internal/audit+httpapi（Last Login）/ internal/scheduler（新包，定名从 ADR-0044）+ httpapi 维护·备份·复制 REST（cron 消费面）/ internal/adapter（helm/debian/rpm 远端浏览 + remote Test）/ internal/repo（可选档接线 + §8.5 口径扩面）/ internal/search+httpapi（AQL statistics/usage、QRL、UI 族）。同族跨波串行，异族同波并行。
- **行为面票必须 dep 规格票**：T-440/T-452 dep T-435（aql.md 增量段）；T-442/T-448 dep T-435（remote-browsing.md）；T-446 dep T-435+T-436（cron 锚 + ADR-0044）；T-439/T-441/T-443/T-453/T-455 dep T-437（断言冻结——Q8/Q9 已裁倾向在册，册落笔即锚）。
- **FE 票四件套**（每票硬 AC）：锚族冻结（既有锚零改名 + 新锚入册 0 断链）+ 四闸门 + axe 双主题 serious=0 + 服务端 diff=0（BE 腿票除外）+ SPA ≤10KB/票（累计趋势观察——NFR-P72）。**BE 票三件套**：table-driven 单测 + ACL 零泄漏探针（NFR-S76——usage 系泄漏面次宽处）+ `make test`（race）全绿。协议面票必须真实客户端（八型 roundtrip / helm·deb·rpm 自指上游夹具）。

## 1. 票据表

### 1.1 FR → 票映射

| FR | 票（P） | 承载要点 | 裁决/登记锚 |
|---|---|---|---|
| FR-141 | T-435（P0，B0 规格增量三份）/ T-437（P0，B1 parity 册 v1.2 + K67 冻结）——141.3 facet 回填已随 T-434 落 | aql.md 增量 + remote-browsing.md + cron 锚 / E5·E1 修正 + 三例翻案双留痕 + stay-out 登记 + B 47 项四态预归属表 | L35a；K67~K72 |
| （前置）ADR-0044 | T-436（P0，B0） | cron 调度域架构 + K68 Annotate 迁移会签 + K69 统计 schema 会签（双会签锚同场） | §4.10；NFR-P74/S79 |
| FR-142（已落） | T-434 done | 批次① 四项 + 锚册 v1.31；遗留归位：?focus= 发射端→T-449 | L35 |
| FR-143 | T-439（三段+字段域）/ T-441（modal 880+8 开禁）/ T-443（列表/入口/dirty/Test 消费）；Test 端点 BE 腿归 T-442 | B-1.5 + B-2.5/6 + B-3.6/7/8/9/11/12；形态翻转③归属 T-439/T-441 | LC-83/84；L36；Q8/Q9 |
| FR-144 | T-445（页签序+元数据字段族+Downloads 渲染）/ T-447（属性编辑+下载形态）/ T-449（搜索栈+?focus= 发射端）/ T-451（LC-98 分页 ×9） | B-2.1~4/7~11/13/14 + B-3.14/15/16；断言反转②归属 T-449 | LC-85/86/98；L37；dep T-438 |
| FR-145 | T-453（路由表单化+能力位）/ T-455（两步弹窗+五列）/ T-457（profile 自助+帮助/About）/ T-459（监控面+导航分组——P2） | B-1.7/8/11 + B-2.15~18；断言反转④归属 T-453 | LC-89/90；L40a；Q5 已裁出口① |
| FR-146 | T-438（统计基建——一鱼两吃单源）/ T-444（Annotate+迁移）/ T-454（Last Login BE，P2） | 迁移⑤归属 T-444；K69/K68 定案前置 | LC-87/88；L38/L39 |
| FR-147 | T-442（三型回源枚举+remote Test 端点）/ T-448（可选档接线+§8.5 口径扩面+降级）/ T-461（FE 树消费） | M15 Q4 出口 C 批 1（helm classic+deb+rpm） | LC-92；L41；K71 |
| FR-148 | T-440（statistics/usage 域）/ T-452（QRL+UI 搜索族+dates 合一收尾，P2） | dep T-438 单源 + T-435 增量段；§5.7 全景表 M16 行对账 | LC-93/94/95；L42；K72 |
| FR-149 | T-463（框架+全树文案外提+CI 断言）/ T-464（双包+切换器+断言双语化）——**独占波 B15~B16** | 断言反转⑥归属两票；E6 翻案双留痕 | LC-97；L47；NFR-P73 |
| FR-150 | T-446（调度引擎）/ T-450（三消费面 BE+audit）/ T-462（FE 消费面——FR-145.7 呈现承载） | 断言反转⑦归属 T-446/T-450；ADR-0044 Accepted 前置 | LC-91；L48；NFR-P74/S79 |
| QA/文档/收口 | T-456（中期 P1）/ T-466（终验 P0）；T-458（tech-writer 一票两腿 P1）；T-460（PM Q 终裁联动 P1）；T-465（release+UAT P1） | L36~L48 逐批复核 / L44 收口审计（B 47 项四态零无主）+ DoD 八条 | §8 剧本 |
| 条件票 | T-467（NuGet symbol——Q10 终裁触发）/ T-468（L1 列选器推广+Last Login 列）；E7 toast / license 公钥 ADR / t381〔Q12〕不占号 | 未触发不构成 DoD 缺口，BOARD 留痕 | Q10/Q12；L43 |

优先级统计：**P0×4**（T-435/436/437/466）；**P1×25**（T-438/439/440/441/442/443/444/445/446/447/448/449/450/451/453/455/456/457/458/460/461/462/463/464/465）；**P2×5**（T-452/454/459/467/468）。

### 1.2 票据明细（AC 全文——派单直接引用；`BASE=http://127.0.0.1:8080` `ADMIN=admin:password`；FE 验收 = Playwright `web/e2e/m16/`）

**T-435 [P0] FR-141.4 规格增量段三份（aql.md 增量 + remote-browsing.md + cron 表达式子集锚）**
role: reverse-engineer ｜ area: docs/reverse/（aql.md 增量段 + remote-browsing.md 新建 + cron 锚落 ADR 协作节） ｜ dep: —
AC:
 1. aql.md 增量段：statistics/usage 域字段集（downloaded/downloaded_by/remote_downloaded 族——t226 OSS 活体腿，容器恢复沿 T-381 §0 先例）+ QRL 全量锚（inv-1 §E + 官方——K72：三态语义/指标 job 周期/REST wire）+ dates/creation（K65 承接：404 `No results found.` 逐字空集族 + uri 瘦行 + epoch-ms）+ UI 搜索族四端点 wire（inv-2 §1.C）——逐条出处 + 置信度，「以核验为准」零静默升格。
 2. remote-browsing.md 新建（T-425 §1/§2 直接成稿——conductor 编排确认口径）：`listRemoteFolderItems` 可选档语义（默认 false）/三型枚举形态（helm index 全树、deb/rpm 元数据）/上游故障降级/virtual 口径扩面（§8.5 联动）；t226 Pro 建仓面补拍腿不可活体则官方文档单源 + 附注留痕。
 3. cron 表达式子集锚（Artifactory〔Quartz〕对拍——官方文档主源）：支持域/拒绝形态/next-run 语义（K70 归位）交 ADR-0044 对齐（差异留痕）；tech-lead 就绪度确认（解锁 T-440/T-442/T-446/T-452）。

**T-436 [P0] ADR-0044 cron 调度域 + K68/K69 双会签锚**
role: architect ｜ area: DECISIONS.md（ADR-0044）+ docs/design/architecture.md（调度节 + nodes 扩列增补） ｜ dep: —（软协作：T-435 cron 锚，Accepted 前对齐）
AC:
 1. ADR-0044 Accepted：调度数据模型（schedule 实体：表达式/next-run/last-run/状态/所属域）/ cron 表达式子集与 next-run 计算器 / 触发器挂点（1min sweep 或独立 ticker）/ **与事件驱动+outbox 并存语义**（调度只触发全量类任务〔GC 全量/备份/复制全量〕，事件驱动仍是增量唯一引擎，零重复投递边界）/ 误触发防护（过去时间拒配/每域并发上限）；internal/scheduler 新包定名与边界（不跨包摸内部结构）。
 2. K68 会签：write→deploy-cache 迁移映射 + annotate↔M10 属性动词对位（ADR-0026 增补评审留痕——零提权断言口径）；K69 会签：nodes 扩列 schema（四列）+ 三分计数口径（直连/经 virtual/remote 缓存命中）+ `last_downloaded_by` 可见性门——architecture.md 增补节。
 3. 与 T-435 cron 锚一致性核对（差异留痕）；FR-150 三票拆分边界（引擎/消费面 BE/FE）确认为可实现粒度。

**T-437 [P0] FR-141.1/.2 parity 册 v1.2 修订 + K67 冻结 + B 47 项四态预归属**
role: ux-designer（PM 会签） ｜ area: docs/design/console-artifactory-parity.md（+ 锚册 console-ux.md §0 版本史补记——T-434 遗留） ｜ dep: —（Q 七项已落章 v1.1）
AC:
 1. v1.2 落盘（版本递增 + 修订记录）：E5 前提修正（Q5 出口①路由化改写）/ E1 范围修正（Q2 出口①——管理列表 vs 浏览器表分治文本 + T-434 children 表收窄 as-built 对账）/ 三例翻案双留痕（E6→双语 Q3 / E2→页码 Q4 / cron 推翻 Q1——T-402a 勘误标注）/ stay-out 登记（A8 清单 + 仓库详情中间页/E4 测试器/快搜范围页签/结果计数一致性）。
 2. K67 冻结：树头工具带形态断言锚（T-434 as-built 对照 + t226——含 rclass 三态〔Cache=remote 缓存子集不伪造〕注记）+ 分页控件形态（Q4 出口①——LC-98 断言锚）；B 47 项四态预归属表初稿（翻正/豁免翻案〔Q 编号可回溯〕/stay-out 确认/候裁挂起——供 QA 终验收口审计对账）。
 3. `make docs` 零断链 + PM 会签两行（Q1~Q13 归位核对）+ tech-lead 就绪度确认（批次②~④可拆性复核）。

**T-438 [P1] FR-146.2 per-node 下载计数基建（一鱼两吃单源）**
role: dev-go-core ｜ area: internal/storage（nodes 扩列 + 埋点）+ internal/httpapi（FileInfo 投影） ｜ dep: T-436（K69 schema 定案）
AC:
 1. nodes 扩列四列（download_count/last_downloaded_at/last_downloaded_by/remote_download_count——照 K69 定案 + 启动迁移幂等）+ 下载路径埋点三分口径（直连/经 virtual/remote 缓存命中）+ FileInfo 投影扩字段：`curl -u user1 "$BASE/api/storage/<repo>/<path>"` 含 `downloads`/`last_downloaded_by`；下载 3 次（直连 2 + virtual 1）→ 计数 = K69 定案值（table-driven 单测覆盖三分口径与边界）。
 2. 下载路径 p95 前后对比零可感知尾延（NFR-P71 埋点腿）+ `statisticsEnabled` 开关行为化 + `sourceOrigin` 落库（A2 同族顺车）；ACL——越权仓字段零泄漏探针 + `last_downloaded_by` 可见性门（K69——默认 admin/manager 档）。
 3. `make test`（race）全绿；零第二计数通道（批次③字段族与 FR-148 usage 域唯一数据源——票内声明单源契约）。

**T-439 [P1] FR-143.1/.2 FE 表单三段结构 + 字段域补齐**
role: dev-frontend ｜ area: web/src/pages/repositories（RepositoryFormPage + ReplicationsSection 迁载体） ｜ dep: T-437（Q8/Q9 断言冻结）
AC:
 1. Playwright：Basic|Advanced|Replications 步进条三段导航 + 复制配置移第三步（M6 能力语义零变化）；字段域表驱动 spec——maxUniqueSnapshots/repoLayoutRef/blackedOut/archiveBrowsingEnabled/Environments 多选/Public·Internal 描述拆分/Force Authentication（virtual）/Suppress POM（maven）逐字段**提交-回显-行为三链**（blackedOut=true 拒写行为断言——curl 对位配置端点回显）。
 2. 表单 footer 重置钮移除（Q9 PM 倾向——M1 锚点 Cancel+Create/Save 对齐）+ parity 册 M1 行联动留痕；repoLayoutRef 与 layout 解析联动评估 K 项票内登记（Q8 出口①全补口径）。
 3. 四闸门 + axe 双主题 serious=0 + 服务端 diff=0 + SPA ≤10KB + 新锚入册 0 断链。

**T-440 [P1] FR-148.1 AQL statistics/usage 域 + /api/search/usage**
role: dev-go-core ｜ area: internal/search（statistics 域增量）+ internal/httpapi search 面（usage 端点） ｜ dep: T-435, T-438
AC:
 1. statistics 域查询绿（字段集照 aql.md 增量段定案）：下载夹具（先下载 N 次）→ `items.find({"$and":[{"statistics.downloaded":{"$lt":"..."}},{"repo":{"$match":"*-local"}}]})` 类查询 + include/sort 联动；`curl "$BASE/api/search/usage?usageSince=..."` 命中已下载制品且**计数与 FileInfo 单源一致**（一鱼两吃断言）+ envelope/usageSince 时间语义照锚。
 2. ACL 越权探针（受限用户越权仓行零出现——§1.4 条款 5 硬 AC）+ K63 资源门沿用（上限/并发/超时三件对查询面零豁免）+ 与 T-438 零第二通道复核。
 3. statistics 域查询 P95 ≤800ms 量级（万节点 + 计数列，本机 SSD+WAL——NFR-P71）+ `make test`（race）全绿。

**T-441 [P1] FR-143.3 FE 包类型弹窗 880px + 8 包型开禁**
role: dev-frontend ｜ area: web/src/pages/repositories（包类型选择 modal） ｜ dep: T-439（同文件族先后脚）
AC:
 1. Playwright：modal 880px 居中断言（M1 锚点 440px 紧凑档升级——parity 册 M1 行再修订留痕，形态翻转③）+ tiles 维持 BinFlow 实有 13 型（不伪造未实现型——Artifactory ~33 全型 dep A4 让位）+ 8 包型（go/nuget/cargo/conan/helm/helmoci/rpm/debian）tile 无禁用态。
 2. 八型逐型经控制台建仓 + 真实客户端 roundtrip（`go install`/`dotnet nuget push`/`cargo publish`/`conan upload`/`helm push`/`helm push oci://`/`dnf`/`apt`——既有八型回归矩阵复用跑绿；纯前端门，addon 槽定义核对票内留痕，若需调整走 ADR-0033 增补）。
 3. 四闸门 + axe 双主题 + SPA ≤10KB + 锚册纪律。

**T-442 [P1] FR-147.1 远端浏览批 1 三型回源枚举 + remote Test 端点**
role: dev-registry-adapter ｜ area: internal/adapter（helm/debian/rpm 三型）+ internal/remote + httpapi Test 路由（wire 歧义⑥——票内核定） ｜ dep: T-435（remote-browsing.md）
AC:
 1. `listRemoteFolderItems` 可选档对位（**默认 false**——off 行为 diff=0）：helm classic（index.yaml 全树解析）/ deb（元数据枚举）/ rpm（元数据枚举）三型回源枚举——table-driven 单测 + 自指上游夹具（M6 复制夹具复用）逐型枚举断言。
 2. 上游不可达降级（该层错误态不整树塌——形态照 remote-browsing.md）+ 探测只读接触零副作用 + ACL 同 allow() 源（越权仓零枚举）。
 3. remote Test 端点（FR-143.5 消费——Engine.TestTarget 同构复用：上游可达 + 认证探测零副作用）；wire 归属（歧义⑥：与 T-422 replication Test 同族或 repositories test 新路由）票内与 conductor 核定后落 LC 登记——零私加口径不破。

**T-443 [P1] FE FR-143.4/.5 列表列集 + 入口分路由 + dirty-gating + Test 消费**
role: dev-frontend ｜ area: web/src/pages/repositories（RepositoriesPage 列表与入口拓扑） ｜ dep: T-441, T-442
AC:
 1. Playwright：Add Repositories 下拉三预选（Local/Remote/Virtual）分路由深链（/new 直链兼容映射）+ 表单内 rclass 控件移除；列表列集对齐（冗余「类型」列收敛〔Q9〕+ Remote 页签 Replications 列〔push-only——ADR-0021/R10 口径注记〕+ Project 列缺位登记不伪造）。
 2. dirty-gating（进入编辑 Save disabled/变更后 enabled/无变更提交不可达）+ remote Test 三臂（正确凭据成功/错误凭据内联失败/不可达超时呈现——消费 T-442 端点）+ 零副作用断言（上游只读接触）。
 3. 行操作超集维持已豁免（L2/E1 零倒退断言）+ 四闸门 + axe + SPA ≤10KB。

**T-444 [P1] FR-146.1 Annotate 动词 + write→deploy-cache 拆分与迁移**
role: dev-go-core ｜ area: internal/auth（动词域）+ internal/httpapi（properties 写门）+ internal/migrate（迁移脚本） ｜ dep: T-436（K68 定案）
AC:
 1. 动词域扩列（annotate = 属性写权限——M10 三动词对位映射照 K68）+ write→deploy-cache 拆分：迁移脚本 dry-run 报告（write 行→deploy-cache 映射率 100%）+ 迁移后 M7 RBAC 全量 spec 零回归（**零提权**——NFR-S77）+ 迁移可回滚。
 2. annotate 位控制属性写（无位用户 PUT properties → 403 探针）+ 既有动词（read/delete/manage）行为零回归 + FE 兼容期（旧 write 消费面映射呈现——T-455 五列消费前置）。
 3. `make test`（race）全绿 + table-driven（动词 × 权限矩阵 × 端点门）。

**T-445 [P1] FR-144.1/.2/.3 FE 详情页签序 + 元数据字段族 + Downloads 渲染**
role: dev-frontend ｜ area: web/src/pages/artifacts（NodeDetail） ｜ dep: T-438（字段族端到端）, T-443（FE lane 先后脚）
AC:
 1. Playwright：三级（仓/目录/文件）页签序断言（权限在属性前——admin-only 门控维持）+ File URL 复制（剪贴板/回显）+ Downloads/Last Downloaded/Last Downloaded By/Remote Downloads 渲染——curl 下载 3 次 → 详情 General `Downloads=3` + `Last Downloaded By=<调用者>`（dep T-438 端到端）。
 2. 仓视图补 Repository Layout/Description/Created/Artifact Count·Size(Show)（与 T-434 目录直系概要对账）；目录视图补 File URL；Package Information·Dependency Declaration / Virtual Repository Associations / Included Repositories 块（virtual 关联 = listVirtual 成员清单呈现）；mimeType/校验块处置（Q9——校验能力收进下载伴随）+ Module ID 不建（stay-out 缺位登记——dep Build-info）。
 3. 详情 General ISO 日期格式化（T/.000Z 不裸显）+ 四闸门 + axe + SPA ≤10KB。

**T-446 [P1] FR-150.1/.2 cron 调度引擎（internal/scheduler 新包）**
role: dev-go-core ｜ area: internal/scheduler（定名从 ADR-0044）+ storage schedule 存储 ｜ dep: T-435（cron 锚）, T-436（ADR-0044 Accepted）
AC:
 1. schedule 实体 CRUD + cron 表达式子集解析（Quartz 对拍锚——支持域/拒绝形态，参数化解析零注入 NFR-S79）+ next-run 计算器（与表达式对拍断言——NFR-P74）+ 触发器（挂点照 ADR）+ 误触发防护（过去时间拒配 400 形态/next-run 不可达校验/每域并发上限）。
 2. 并存语义（断言反转⑦）：调度只触发全量类任务 + 事件驱动 outbox 仍是增量唯一引擎（webhook/replication outbox 引擎文件 diff=0 审计——复用非重构）；同任务载体幂等收敛（与 Replicate Now 同载体）。
 3. 到点触发精度 ±分钟级（测试加速：手动触发等价腿）+ audit 词表三域补词（maintenance.schedule.*/backup.schedule.*/replication.schedule.*）+ `make test`（race）全绿。

**T-447 [P1] FR-144.4/.5 FE 属性编辑解剖 + 下载形态**
role: dev-frontend ｜ area: web/src/pages/artifacts（PropertiesTab + NodeDetail 下载带） ｜ dep: T-445
AC:
 1. Playwright：属性编辑常显 Property/Value 输入 + Add 按钮 + 网格搜索（M10 属性夹具数据腿）——隐藏「+ 新增属性」表单 + 逐行 ✎/🗑 退役；行内删除与 E1 关系统一（Q2 出口①——删除走确认/收进详情，E1 不倒退）。
 2. 下载形态单 24px 图标钮（两带文字按钮收敛 + 校验能力收进伴随菜单——Q2/Q9 处置）+ 下载动作计数联动（T-438 埋点单源）。
 3. Property|Property Set 分段候裁臂（K68——裁做须 BE 属性集小域扩列另立票，不做缺位登记）+ 四闸门 + axe + SPA ≤10KB。

**T-448 [P1] FR-147.2 可选档 repo 接线 + §8.5 口径扩面 + 降级**
role: dev-go-core ｜ area: internal/repo（children/listVirtual 可选档接线）+ docs/reverse/repo-semantics.md §8.5 回写 ｜ dep: T-442
AC:
 1. repo service 可选档接线（配置面 `listRemoteFolderItems` 开关默认 false——off 行为 diff=0 既有断言零回归）；口径扩面：可选档 on 时 virtual 树含远端成员行（repo-semantics §8.5 对账回写——T-412「仅缓存行」口径扩面）。
 2. 未缓存路径点击 → 触发回源拉取 + 下载计数埋点联动（T-438 单源）+ 上游停机降级（远端层错误态 + 已缓存行可用——L41 BE 腿）。
 3. 越权仓远端行零泄漏探针（allow() 同源）+ `make test`（race）全绿。

**T-449 [P1] FR-144.6 FE 搜索栈（列集归一 + 快搜 + 日期 + ?focus= 发射端翻新）**
role: dev-frontend ｜ area: web/src/pages/search + web/src/AppShell（快搜）+ DashboardPage/AqlPanel（发射端） ｜ dep: T-447（FE lane 先后脚）
AC:
 1. Playwright：结果列集 = Artifact(name 链接)|Path|Repository|Modified + 选择列（**断言反转②**——三源不一致归一；大小+sha256 移入列选器可选项不默认呈现）+ 行导航仅 name 单元格深链（行体 inert）+ 顶栏驻留查询 + 网格内快滤（AQL 模式编辑器共存形态票内设计）。
 2. 快搜空历史占位恒渲染（AppShell.tsx:628——「No recent searches yet」对位）+ 结果表日期格式 `dd-MM-yy HH:mm:ss +ZZZZ` 对位（正则断言含时区偏移）；**?focus= 发射端翻新**（T-434 遗留——DashboardPage/SearchPage/AqlPanel 改发路径段深链，兼容重定向维持一轮）。
 3. M15 AQL 模式 spec 零回归（t419/搜索页族——列框架收敛后 AQL 行复用锚不破）+ 四闸门 + axe + SPA ≤10KB。

**T-450 [P1] FR-150.3/.4 cron 三消费面 BE + audit 三事件**
role: dev-go-core ｜ area: internal/scheduler 消费接线 + internal/httpapi（维护/备份/复制 REST 面——调度 REST 形态照 ADR-0044） ｜ dep: T-446
AC:
 1. GC 定时（维护面 cron 字段 + Cleanup 两族触发接线——手动 dry-run/apply 并存维持）+ 备份定时 CRUD REST（New Backup/cron/next-run/列表）+ import/export 管理面 BE 支撑（M6 模型迁移载体）。
 2. 复制配置 cron 字段（用户级 cron 对位——M15 Q5 推翻兑现面）：双实例夹具（T-420 形态复用）按点触发全量同步（目标逐路径 sha256 一致）+ **调度窗内同制品增量事件不双推**（零重复投递断言——L48）+ Replicate Now 与调度并存幂等。
 3. audit 三事件在场（调度/执行/失败）+ 调度 CRUD admin/manager 门（非授权 403）+ `make test`（race）全绿。

**T-451 [P1] FR-144.7/LC-98 FE 分页控件 ×9 统一（E2 翻案）**
role: dev-frontend ｜ area: web/src 共享分页组件 + ×9 消费点（票内盘点清单入锚册） ｜ dep: T-449（搜索结果列表先行收敛）
AC:
 1. 共享分页组件页码控件形态（Q4 出口①——Artifactory 控件形态照 K67/T-437 冻结锚）+ ×9 消费点逐一迁移（票内先 grep 盘点「加载更多」族全量清单——若实际多于 9 以盘点为准，锚册登记）；后端维持 keyset 游标页窗映射（呈现对齐、语义自有 C 注留痕——深翻页 offset 扫描成本规避）。
 2. Playwright：页码跳转/每页行数/首末页禁置 + 深翻页行为（keyset 页窗断言）；E2 豁免翻案落 parity 册 v1.2（T-437 联动留痕）。
 3. 九消费点既有 spec 翻新零断链 + 四闸门 + axe + SPA ≤10KB。

**T-452 [P2] FR-148.2/.3 AQL 副线收尾：QRL 全量 + UI 搜索族 + dates/creation**
role: dev-go-core ｜ area: internal/search + internal/httpapi（v1 system 面 + search 面——T-440 先后脚） ｜ dep: T-435, T-440
AC:
 1. QRL 全量：`v1/system/query_rate_limiter` 三态 REST（admin 门——非 admin 403）+ 指标 job 采样呈现 + K63 门行为与 REST 面读数一致（默认值维持 K63 定案 1000/4/10s——K72）。
 2. UI 搜索族四端点（artifactsearch/stashResults/packagesSearch/syntax-search——wire 照 aql.md 增量段锚；Smart Searches 保存面 pro 档不做）+ dates/creation 双端点（K65 承接：404 `No results found.` 逐字空集族 + uri 瘦行 + epoch-ms 参数）+ ACL 同源探针。
 3. **M15 §5.7 全景表 M16 行逐条对账**（实现/维持 404 与表一致——未列端点仍 404）+ t92/aql 既有 spec 零回归 + `make test`（race）全绿。

**T-453 [P1] FR-145.1/.3 FE 用户/组路由表单化 + 能力位三旗（断言反转④）**
role: dev-frontend ｜ area: web/src/pages/security（UsersPage/GroupsPage + /new 路由） ｜ dep: T-451（FE lane 先后脚）
AC:
 1. Playwright：/users/new、/groups/new 深链整页表单（Cancel/Reset〔Q9 处置〕/Save）+ 创建-列表-编辑闭环 + 列表内联展开卡退役断言（断言反转④——Q5 出口①路由化；E5 条目改写对账 T-437 同场）。
 2. 能力位三旗（Can Update Profile/Disable UI Access/Disable Internal Password）勾选后行为联动（Disable UI Access 用户登录被拒臂/Disable Internal Password 密码改道臂）；Administer Platform + Manage Resources 双布尔 vs 三值枚举候裁臂（ADR-0026——v1.0 暂行维持枚举 + 差异登记，候裁挂附注）。
 3. M7/M9 users/groups spec 翻新零断链 + 四闸门 + axe + SPA ≤10KB。

**T-454 [P2] FR-146.3 Last Login 派生（audit → users 投影）**
role: dev-go-core ｜ area: internal/audit（登录事件派生）+ internal/httpapi（users 列表投影） ｜ dep: —（audit 登录事件既有）
AC:
 1. 登录事件后 users 列表 API 投影字段断言（lastLogin 呈现 + 派生时延口径——查询时派生或物化，票内定案留痕）；「无端点列不伪造」解除依据（数据源已备）留痕。
 2. ACL（自身/管理员可见性门——NFR-S76）+ 既有 users 列表端点零回归 + `make test`（race）全绿。FE 列消费归 T-468（L1 列选器推广条件票——dep 本票）。

**T-455 [P1] FR-145.2 FE 权限编辑两步弹窗 + 矩阵五列**
role: dev-frontend ｜ area: web/src/pages/security（PermissionEditorPage） ｜ dep: T-444（五列动词消费）, T-453（FE lane 先后脚）
AC:
 1. Playwright：两步弹窗（① Select Repositories 双列 + Any Local/Any Remote 预置勾选 → 权限面覆盖本地全仓断言；Any Distribution 缺位登记——dep Release Bundle 不伪造；② Set Patterns include/exclude——路径模式 textarea 迁第二步）+ 内联「＋添加仓库…」/内联添加用户组收敛。
 2. 权限矩阵五列（Read/Deploy-Cache/Delete/Annotate/Manage——消费 T-444 迁移面）+ include/exclude 生效探针（无权限用户 403/不可见）。
 3. E4 整页编辑器 + 模式测试器/diff 维持自有（A8 零删除断言）+ 四闸门 + axe + SPA ≤10KB。

**T-456 [P1] QA 中期回归（逐批 V 式复核）**
role: qa-engineer ｜ area: 测试矩阵（L36~L42/L48 已落面 + t226 对照） ｜ dep: T-434, T-439~T-452 已合入面
AC:
 1. 逐批复核：L36（三段+字段域表驱动+八型 roundtrip 矩阵）/ L37（页签序+字段族+属性编辑+搜索栈）/ L38（统计基建单源）/ L39（Annotate 迁移零提权）/ L41 BE 腿（可选档双态+降级）/ L42 statistics·usage 腿 / L48 引擎腿（next-run 对拍+零重复投递双实例腿——若 T-450 已合入）+ t226 活体对照关键页复跑（树/表单/详情/安全）。
 2. M1~M15 P0 双形态增量窗口回归 + 断言反转①~⑤现值核实 + E1/E6 零倒退检查（Q2/Q3 出口后新文本口径）；双 locale 抽样腿预留（i18n 后置——本期不跑）。
 3. 缺陷登记与回归报告落盘（reports/）——诚实汇报（跳过/在途注明）。

**T-457 [P1] FE FR-145.4/.6a profile 自助 token/SSH + 帮助下拉/About**
role: dev-frontend ｜ area: web/src/ProfilePage + web/src/AppShell（? 帮助钮） ｜ dep: T-455（FE lane 先后脚）
AC:
 1. Playwright：Profile 页 identity token 生成（一次性明文呈现 + 即时可用 curl 断言）+ SSH key 增删（「签发指到 admin Tokens 页」文档化设计推翻——B-1.8 补齐；Access Tokens 持久化牵 L1 列选器重评登记 A7 联动）。
 2. ? 帮助下拉（Documentation/Online Training/Release Notes/About——链接形态照 ux 定案）+ About 版本弹窗（版本/构建信息——侧栏脚注 vdev 升格）。
 3. 四闸门 + axe 双主题 + SPA ≤10KB。

**T-458 [P1] 文档票（两腿——票内先后笔）**
role: tech-writer ｜ area: docs/user/（树深链/表单三段/详情字段族/AQL 高级面/安全面 + i18n·cron 增量 + 翻案公告） ｜ dep: 腿① T-440/T-443/T-445；腿②候 T-459/T-461~T-464 合入
AC:
 1. 腿①：树深链与工具带 / 表单三段与 8 包型开禁 / 详情字段族与统计（Downloads 族 + `/api/search/usage`「90 天未下载」示例）/ 权限动词与安全面增量——客户端命令实测可复跑。
 2. 腿②：监控面 / 远端浏览可选档 / cron 调度与定时备份指南 / i18n 双语切换 / **豁免翻案用户可见变化公告**（E 系翻案 + cron 推翻 M15 裁定 + i18n 双语逐一明示——L46）+ `make docs` 零断链。

**T-459 [P2] FE FR-145.5/.6b 监控面 + 导航分组/侧栏过滤**
role: dev-frontend ｜ area: web/src/pages/monitoring（新页）+ web/src/pages/admin/SystemInfoPage 归位 + AppShell 导航 ｜ dep: T-457（AppShell 先后脚）
AC:
 1. Playwright：System Logs 查看器（尾随刷新/过滤/下载——载体票内定案，零新端点优先，若需端点走 PM FR 增补流程）+ Service Status 页（对位既有 metrics/health 端点）+ SystemInfoPage 归位服务节点组（监控组三页）。
 2. Admin 导航分组对齐（Webhooks 归常规组/维护·备份归服务节点组〔与 T-462 挂靠一致〕/认证组子项形态——HTTP SSO/Crowd/JIRA 缺位系域不存在登记）+ 侧栏 Search Admin Resources 过滤框。
 3. 四闸门 + axe 双主题 + SPA ≤10KB。

**T-460 [P1] PM Q 终裁联动收口笔**
role: product-manager ｜ area: docs/prd/milestone-16.md（收口笔）+ ROADMAP（M16 未纳入项 + M17 预立项段衔接） ｜ dep: T-437 + 各批次 as-built 素材
AC:
 1. Q8~Q12 逐项归位（Q8/Q9 断言冻结前已裁或落章核对 / Q10 条件窗终裁〔NuGet symbol——材料上 BOARD〕/ Q11 尾波〔webhook 触发源裁剪〕/ Q12 conductor 决定项移交）+ §7 总账回写。
 2. **B 47 项收口审计表四态归属核对**（与 T-437 预归属表 + 各票 as-built 对账——零无主项移交 QA 终验）；K67~K72 回填；ROADMAP「M16 未纳入项」起草（沿 T-395/T-427 先例）+ M17 预立项段衔接核对（PRODUCT.md 修订未发生即不启动）。

**T-461 [P1] FR-147.3 FE 远端浏览树消费（可选档双态）**
role: dev-frontend ｜ area: web/src/pages/artifacts（树消费）+ repositories 表单可选档开关（Advanced 段） ｜ dep: T-448
AC:
 1. Playwright：可选档 off（默认）→ remote 树仅缓存行（既有断言零回归）；on → 树展开含未缓存远端目录（helm index 全树臂/deb·rpm 元数据臂——自指上游夹具）+ 点击未缓存路径触发回源拉取 + 下载计数联动（T-438 单源）。
 2. 上游停机 → 远端层错误态 + 已缓存行可用；virtual 含 remote 成员行断言（§8.5 对账——T-412 口径扩面消费）。
 3. 四闸门 + axe + SPA ≤10KB + 锚册纪律。

**T-462 [P1] FE FR-145.7 GC/备份 cron 消费面 + import/export 管理页**
role: dev-frontend ｜ area: web/src/pages/governance（GCPage/BackupPage/MigrationPanel） ｜ dep: T-450（BE 面）, T-459（导航挂靠先行）
AC:
 1. Playwright：GC 维护面扩展（Cleanup Unused Cached Artifacts/Cleanup Virtual Repositories/Compress Internal Database/Prune Unreferenced Data/Quota 百分比 + cron 字段保存-回显-next-run 呈现）+ 备份定时 CRUD（New Backup/cron/next-run/列表）+ import/export 管理页（CLI 引导卡升格）；手动 dry-run/apply 并存维持。
 2. 调度状态呈现（next-run 倒计时/失败态/禁用态——形态票内设计）；audit 行入口（若锚册登记）。
 3. 四闸门 + axe 双主题 + SPA ≤10KB + 服务端 diff=0（纯 FE 票——消费 T-450 REST）。

**T-463 [P1] FR-149.1 i18n 框架接入 + 全树文案外提 + CI 断言（独占波）**
role: dev-frontend ｜ area: web/src 全站（文案键化）+ web/scripts（扫描脚本）+ 构建链 ｜ dep: T-462（**全部 FE 票收口后——独占波**）
AC:
 1. i18n 框架接入（react-i18next 或票内 architect/ux 会签定案——轻量 ADR 或票内定案留痕）+ **文案外提 100%**（组件层零硬编码中文——namespace 按页面域切分 + 懒加载）；zh 包 = 现行文案迁移零语义变化（「英文术语保真」维持）。
 2. CI 断言脚本（组件层硬编码中文零命中——扫描归档）+ missing key 零运行时告警；既有全部 FE spec 零回归（默认 locale zh 断言维持——文案断言零翻新成本）。
 3. 锚 id 与文案解耦（data-testid 不随 locale 变——锚册零改名 0 断链）+ i18n 资源包 gzip 增量单独登记（NFR-P73——功能代码 SPA ≤10KB 口径维持）。

**T-464 [P1] FR-149.2/.3/.4 双资源包 + 语言切换器 + 断言双语化**
role: dev-frontend ｜ area: web/src（切换器 + en 资源包）+ web/e2e（en 抽样腿） ｜ dep: T-463
AC:
 1. en 资源包（术语对齐 Artifactory——repo key/node/checksum/Deploy/Set Me Up 等英文术语原样）+ 语言切换器（侧栏脚或 Profile——位置照 ux 定案）+ 持久化（localStorage）。
 2. Playwright：切换 zh↔en 全站文案翻转（抽样 ≥6 页面：树/表单/详情/搜索/安全/监控）+ reload 持久 + 术语两包一致断言（zh 包英文术语原样呈现）。
 3. 断言双语化策略：en 抽样腿（每批次核心 spec 一条 en 断言——文案断言改键断言）+ axe 双主题双 locale 抽样 serious=0 + locale 化格式（日期/数字——FR-144.6 日期格式 en 变体，zh 维持现行）。

**T-465 [P1] release 烟测 + UAT 随里程碑 PR**
role: release-engineer ｜ area: deploy/ + charts/ + CD 链 ｜ dep: 全部实现票 + T-458
AC:
 1. 部署烟测（compose/k8s/systemd 矩阵）+ UAT 随里程碑 PR——8 包型开禁 / 统计基建 / cron 调度在 UAT 链取证 + version sha 翻转 + Chart bump 判据（M16 behavior 变化多处——预计 bump）。
 2. 资源门三连（footprint ≤100MB / check-size ≤120MB / 冷启动 <2s）+ **F1 六平台趋势登记**（104.52MB 基线——i18n 资源包与 FE 四批次净增量须可解释）+ server 40.59MB 原始线观察（T-429 遗留 M16 观察）+ SPA 累计趋势复核（NFR-P72）；t381 事故残留清理〔Q12——conductor 决定随本票处置或 BOARD 留痕〕。

**T-466 [P0] QA 终验**
role: qa-engineer ｜ area: 全量矩阵 ｜ dep: 全部 + T-465
AC:
 1. L35~L48 全量 + **断言反转①~⑦归属审计 100% M16 票**（含 E6 双语/cron 推翻两例 v1.1 翻案——①落 T-434）+ **B 47 项收口审计表**（四态归属零无主项——与 T-460 对账）+ parity 册 v1.2 终评 + FE 票服务端 diff=0 复核（BE 腿票除外）。
 2. DoD 八条逐条 + NFR-P70~P73/S76~S79 达标归档（树栈 p95 基线 / statistics P95 / 资源门三连 / SPA 累计 + i18n 资源包 / 调度精度与零重复投递 / Annotate 迁移零提权）+ M1~M15 P0 双形态全量复跑 + m16-done 就绪判定。

**T-467 [P2·条件·Q10] NuGet symbol server 六承转正**
role: dev-registry-adapter ｜ area: internal/adapter（nuget symbol 面——.pdb/GUID 路径） ｜ dep: Q10 终裁（PM 倾向转正——材料窗归 T-460）；插空 BE lane（i18n 波 lane 2 可承载）
AC:
 1. mini as-built 规格随票（T-293 终裁口径）+ symbol 上传/拉取真实腿（.pdb GUID 命中——真实 nuget/dotnet 客户端）+ 既有 nuget 面零回归。未触发不构成 DoD 缺口（BOARD 留痕）。

**T-468 [P2·条件] L1 列选器推广 + Users Last Login 列**
role: dev-frontend ｜ area: web/src（repos/users/groups/permissions 四列表列选器） ｜ dep: T-454 + 批次④ FE 收口；FE lane 插空（i18n 前空位）或尾波
AC:
 1. columnPrefs 共享层接入四页（开合/显隐持久 reload 保持/全选复位——T-387 spec 形态复用，LC-96）+ Users Last Login 列（T-454 投影消费）+「无端点列不伪造」维持；四闸门 + axe + SPA ≤10KB。未触发不构成 DoD 缺口（BOARD 留痕）。

**不占号 slot（M14 D3 先例）**：E7 toast 锚位（候用户信号——F1 微调一行级）/ license 公钥 config 覆盖 ADR（候立项）/ t381 残留清理（Q12 conductor——建议随 T-465 处置）。

## 2. 波次与并行分区（全宽 2；波内 area 互斥；FE 主线一波一票错峰；i18n 独占波）

| 波 | lane 1 | lane 2 | 要点 |
|---|---|---|---|
| **B0** | T-435 [P0] 规格增量三份（rev） | T-436 [P0] ADR-0044+双会签锚 | 前置锚双票并行（conductor 指令）；软协作 cron 锚 |
| **B1** | T-437 [P0] parity 册 v1.2+K67 | T-438 [P1] 统计基建 | 册落笔 = 批次②~④断言地基；统计基建先行（一鱼两吃单源） |
| **B2** | T-439 [P1] FE②-a 三段+字段域 | T-440 [P1] AQL statistics/usage | FE 主线开工（dep T-437）；usage dep T-438 |
| **B3** | T-441 [P1] FE②-b modal+8 开禁 | T-442 [P1] 远端浏览三型+Test 端点 | web/src repositories modal vs internal/adapter |
| **B4** | T-443 [P1] FE②-c 列表/入口/dirty/Test | T-444 [P1] Annotate BE+迁移 | FE② 收口（dep T-442 Test）；auth/migrate 域 |
| **B5** | T-445 [P1] FE③-a 页签序+字段族 | T-446 [P1] cron 调度引擎 | 字段族 dep T-438 端到端；scheduler 新包 |
| **B6** | T-447 [P1] FE③-b 属性编辑+下载 | T-448 [P1] 可选档接线+§8.5 扩面 | artifacts PropertiesTab vs internal/repo |
| **B7** | T-449 [P1] FE③-c 搜索栈（断言反转②） | T-450 [P1] cron 三消费面 BE+audit | ?focus= 发射端归位；dep T-446 |
| **B8** | T-451 [P1] FE LC-98 分页 ×9 | T-452 [P2] QRL+UI 族+dates | E2 翻案跨页面波；副线 P2 收尾 |
| **B9** | T-453 [P1] FE④-a 路由表单化+能力位 | T-454 [P2] Last Login BE | 批次④开工（断言反转④） |
| **B10** | T-455 [P1] FE④-b 两步弹窗+五列 | T-456 [P1] QA 中期回归 | 五列 dep T-444；中期复核窗（L36~L42/L48 已落面） |
| **B11** | T-457 [P1] FE④-c profile+帮助/About | T-458 [P1] 文档票（两腿） | 腿①动笔；AppShell 帮助钮 |
| **B12** | T-459 [P2] FE④-d 监控面+导航分组 | T-460 [P1] PM 收口笔 | 批次④收口；Q8~Q12 归位窗 |
| **B13** | T-461 [P1] FE 远端浏览树消费 | （插空窗：T-467 条件票〔Q10 触发〕） | dep T-448；lane 2 容纳条件 BE 票 |
| **B14** | T-462 [P1] FE cron 消费面 | （插空窗：T-467 续 / 波外筹备） | dep T-450+T-459；最后一张常规 FE 票 |
| **B15** | T-463 [P1] **i18n-a 外提（独占波）** | —（非 FE 条件票可插；FE 互斥） | 全树文案外提——与所有 FE 票互斥 |
| **B16** | T-464 [P1] **i18n-b 双包+切换器（独占波）** | —（同上） | L47 断言双语化 |
| **B17** | T-465 [P1] release 烟测+UAT | — | 单票波（尾部） |
| **B18** | T-466 [P0] QA 终验 | — | 单票波；m16-done 就绪判定 |
| 波外 | T-467 [P2·条件 Q10] NuGet symbol / T-468 [P2·条件] L1 列选器+Last Login 列 | — | 未触发不构成 DoD 缺口；T-468 避开 FE 主线波 |

**插空纪律**：任一波提前收口时补位序 = T-468（FE 空位——批次④后任意 FE 空档，i18n 前）→ T-467（BE lane 任意空位，Q10 终裁即插）→ T-452（P2 副线可前移至 T-440 收口后任意 BE 空位）→ T-454（P2 零依赖可前移）。B13/B14 lane 2 为常设插空窗。

## 3. 依赖图与关键路径

```
B0  T-435(规格增量三份) ──软协作cron锚── T-436(ADR-0044+K68/K69 会签锚)
     │ dep ┌──────────────┴───────────────┐
B1  │     T-437(parity 册 v1.2+K67)        T-438(统计基建 ← T-436 K69)
     │        │                                │
B2  │     T-439(FE②-a 三段+字段域)         T-440(AQL statistics/usage ← T-435+T-438)
     │        │                                │
B3  │     T-441(FE②-b modal+8 开禁)        T-442(远端浏览三型+Test ← T-435)
     │        │                                │
B4  │     T-443(FE②-c ← T-441+T-442)       T-444(Annotate BE ← T-436 K68)
     │        │                                │
B5  │     T-445(FE③-a 字段族 ← T-438+T-443) T-446(cron 引擎 ← T-435+T-436)
     │        │                                │
B6  │     T-447(FE③-b 属性编辑+下载)        T-448(可选档接线+§8.5 ← T-442)
     │        │                                │
B7  │     T-449(FE③-c 搜索栈·断言反转②)     T-450(cron 三消费面 ← T-446)
     │        │                                │
B8  │     T-451(FE 分页 ×9)                 T-452(QRL+UI 族+dates ← T-435+T-440)  [P2]
     │        │
B9  │     T-453(FE④-a 路由表单化·断言反转④)  T-454(Last Login BE)  [P2]
     │        │                                │
B10 │     T-455(FE④-b 两步弹窗+五列 ← T-444) T-456(QA 中期 ← 已落面)
     │        │
B11 │     T-457(FE④-c profile+帮助)         T-458(文档票两腿)
     │        │
B12 │     T-459(FE④-d 监控+导航)            T-460(PM 收口笔 ← T-437)
     │        │
B13 │     T-461(FE 远端浏览树消费 ← T-448)   （插空窗 T-467〔Q10〕）
     │        │
B14 │     T-462(FE cron 消费面 ← T-450+T-459)（插空窗续）
     │        │
B15 │     T-463(i18n-a 外提——独占波 ← 全部 FE 收口)
     │        │
B16 │     T-464(i18n-b 双包+切换器+双语断言)
B17 T-465(release ← 全部实现票 + T-458)
B18 T-466(QA 终验 ← 全部 + T-465) → m16-done
波外 T-467(Q10 条件)/T-468(列选器+Last Login 列——FE 空位插空)；E7/license ADR/t381 不占号
```

**关键路径 = FE 主线 15 波串行链**（B2 批次② → B5~B8 批次③ → B9~B12 批次④ → B13/B14 远端浏览与 cron FE 腿 → B15~B16 i18n 独占）→ B17 release → B18 终验。BE 全链（统计基建 → usage / Annotate / 调度引擎 → 三消费面 / adapter → 接线）挂 lane 2 与 FE 天然错峰，**无任何 BE 票反压 FE 主线**（唯二 FE 侧等待点：T-443 dep T-442 Test 端点、T-445 dep T-438——均已前置化解）。i18n 独占波为尾部刚性段（全树文案外提不可拆）——见 R2/R3 压缩选项。

## 4. 风险登记

| # | 风险 | 缓解 |
|---|---|---|
| R1 | **前置锚三票延期平移**——T-435（规格增量）延误阻塞 T-440/T-442/T-446/T-452；T-436（ADR-0044）延误阻塞 FR-150 全链；T-437（parity 册）延误挂批次②~④断言冻结 | B0/B1 三票均零外部依赖即可开工；t226 活体腿降级路径内置（容器恢复沿 T-381 §0 → 官方文档单源 + 附注零静默升格）；ADR 与 cron 锚软协作不互锁（M15 T-407/T-408 先例）；T-437 延误时批次主体可先行（PRD 允许——Q8/Q9 细节候裁附注，断言冻结滞后补） |
| R2 | **FE 主线 15 波串行链工期**——一波一票纪律下任一票回炉平移全链（B2→B16 决定性路径） | 压缩选项三处 conductor 裁量：① T-461/T-462 与批次④ c/d 换序（零依赖腿——147 FE dep T-448 B6 即备、150 FE dep T-450 B7 即备）；② T-451 分页拆「组件+核心三消费点」先行余点随后票；③ i18n-a 外提与 i18n-b 并票（省一波——粒度变大仅工期告急时取）；qa 打回 ≥3 次回炉评估（SPRINT-LOOP） |
| R3 | **i18n 独占波后置 vs PRD「先于/伴随批次票」提示**——终态文案一次外提 vs 批次票期间新增硬编码债 | 取舍留痕（歧义②）：后置外提避免批次改造前先行键化返工（树/表单/详情将被重写——键先行即扔）；缓解：FE 票派单附「新增文案集中组件常量」纪律条款（降外提成本）+ T-463 CI 扫描脚本收口；i18n 延期即 L47 挂起（P1 DoD 项——收口前必须落地） |
| R4 | **Annotate 迁移零提权**（NFR-S77）——write→deploy-cache 映射错即权限面破坏 | K68 映射 T-436 会签前置 + dry-run 报告（映射率 100%）+ M7 RBAC 全量回归 + 可回滚；与 ADR-0026 闭集冲突走增补评审（票内留痕） |
| R5 | **统计基建单源纪律**——批次③字段族与 AQL usage 共依赖，两消费面若各建计数即分裂 | T-438 先行（B1）声明唯一数据源契约 + T-440 AC「零第二通道」复核 + QA 中期单源断言（L38/L42 计数一致）；三分口径 K69 定案前置；埋点尾延 NFR-P71 前后对比 |
| R6 | **远端浏览上游依赖与 wire 歧义⑥**——Test 端点归属未定（LC-83 未列 + §5.3 零私加口径） | BE 腿暂归 T-442（adapter/remote 域——Engine.TestTarget 复用），wire 形态票内与 conductor 核定后 LC 登记；上游夹具自指复用（M6）；五型无根 API 物理不做面维持（§2.2 核对位） |
| R7 | **cron 与事件驱动并存（断言反转⑦）**——双引擎重复投递是架构级风险 | outbox 引擎文件 diff=0 审计断言（T-446 硬 AC）+ 零重复投递双实例腿（T-450/L48）+ 推翻 M15 Q5 留痕链三处核对（BOARD/PRD §5.4/parity 册 v1.2）；调度只触发全量类任务边界写进 ADR-0044 |
| R8 | **条件票与 Q 裁点触发态**——T-467（Q10）/T-468/E7/t381 未触发即 DoD 外 | 未触发 BOARD 留痕（PRD §2.1 行 I 口径）；Q10 材料窗归 T-460；t381 随 T-465 处置或留痕（Q12 conductor）；全触发不增波（B13/B14 插空窗消化） |
| R9 | **八型 roundtrip 与 e2e 容量**——真实客户端矩阵 + 全批次 spec 翻新（flaky 家族） | 八型矩阵复用既有回归（M10~M13 各型腿）；T-434 三线错峰 + 净实例姿态沿用（累积实例假阳性甄别——T-414/T-434 家族在案）；QA 中期集中甄别 |
| R10 | **SPA 累计与资源门**——四批次 FE 量大 + i18n 资源包 + server 40.59MB 原始线（T-429 观察） | 每票 ≤10KB + 累计趋势红旗线（NFR-P72）；i18n 资源包按包口径单独登记（NFR-P73）；release 票 F1 六平台 104.52MB 基线复核 + server 原始线观察归位 |

## 5. 首派建议（B0/B1）

- **B0（即派，conductor 已定前置锚先行）**：**T-435**（reverse-engineer——规格增量三份；派单附 T-425 §1/§2 素材索引 + aql.md 既有体例 + inv-1 §E/inv-2 §1.C 锚点行 + t226 容器恢复动作链〔T-381 §0〕+ 差集法只读纪律）+ **T-436**（architect——ADR-0044 + K68/K69 双会签；派单附 PRD §4.10 全项 + T-402a 勘误原文 + ADR-0026/0023 相关节 + nodes 列表现状 + 与 T-435 软协作注记）。
- **B1（B0 收口即派）**：**T-437**（ux-designer + PM 会签——parity 册 v1.2；派单附七终裁出口清单 + T-434 as-built 注记〔children 表收窄/rclass 三态〕+ B 47 项清单）+ **T-438**（dev-go-core——统计基建；派单附 K69 定案节 + 下载路径三分口径清单 + NFR-P71 计时腿要求）。
- 插空候选（全程）：T-454（P2 零依赖可前移）/ T-452（P2 副线——T-440 收口后任意 BE 空位）/ T-468（批次④后 FE 空档）。
- 派单纪律（全程）：no-git 条款维持；FE 票逐张附「锚族冻结 + 新锚入册 + 四闸门 + axe 双主题 + 服务端 diff=0 + SPA ≤10KB + **新增文案集中常量（i18n 外提降本）**」五件套；BE 票逐张附「table-driven + ACL 零泄漏探针 + race 全绿」三件套；查询面票（T-440/T-452）加「K63 门沿用 + 诚实拒绝」；T-444 加「迁移 dry-run + 零提权 + 可回滚」；T-446 加「outbox 引擎 diff=0 红线」；T-463 加「默认 locale 断言零翻新 + 锚 id 解耦」。

## 6. 估票对比与取舍留痕（对 PRD §1.3 分票提示逐条）

| PRD 提示 | 拆票处置 | 取舍理由 |
|---|---|---|
| FR-141 拆 1~2 票 | **3 票**（T-437 parity 册〔ux 主笔+PM 会签〕+ T-435 规格增量三份〔reverse〕+ ADR-0044 归 T-436——FR-150 前置） | 三份新规格锚（aql 增量/remote-browsing/cron）独立成票（clean-room 规格先行 + M15 T-407 先例）；ADR-0044 按 conductor 指令 B0 前置锚；141.3 facet 回填已随 T-434 落（as-built 注记） |
| FR-142 拆 2~3 票 | **已由 T-434 一票兑现**（插空即发形态） | as-built 四项全落（B2 可 B1 起步的判断被 conductor 提前派发消化）；遗留归位：?focus= 发射端→T-449、锚册 §0 版本史→T-437 |
| FR-143 拆 3~4 票 | **3 FE 票 + Test 端点 BE 腿归 T-442** | 三段+字段域 / modal+开禁 / 列表+dirty+Test 消费（PRD 三分法）；Test 端点系 BE 面（Engine.TestTarget 复用）归 adapter 域——歧义⑥留痕 |
| FR-144 拆 3~4 票 | **4 票**（字段族 / 属性编辑+下载 / 搜索栈 / 分页 ×9） | 分页 ×9 跨页面独立波（LC-98 范式项——与任何页面组票共写即冲突）；日期格式拆归搜索栈（结果表）与字段族（详情 ISO）两腿 |
| FR-145 拆 3~4 票 | **4 FE 票**（路由表单化+能力位 / 两步弹窗+五列 / profile+帮助 / 监控+导航）+ cron 消费面 FE 腿归 T-462 | PRD 四分法照拆；145.7 GC/备份呈现归 FR-150 承载（PRD 明示「消费 FR-150 调度域」）；五列 dep T-444 排 B10 |
| FR-146 拆 3 票 | **3 票**（统计基建 / Annotate / Last Login——P2） | 照 PRD；统计基建 B1 先行（conductor 指令——双消费共依赖）；Last Login FE 列消费归 T-468 条件票 |
| FR-147 拆 ~3 票 | **3 票**（三型+Test / 接线+扩面 / FE 树消费） | 照 PRD（M15 登记口径）；FE 腿因一波一票纪律后置 B13（dep B6 即备——压缩选项 R2①） |
| FR-148 拆 3~4 票 | **3 票**（statistics/usage P1 / QRL+UI 族+dates 合一 P2 收尾） | QRL 与 UI 族同 internal/search+httpapi 面拆开徒增先后脚（M15 T-417 三端点同票先例）；dates 顺车并入收尾票 |
| FR-149 拆 2~3 票 | **2 票**（外提+框架 / 双包+切换器+断言） | 外提与断言策略强耦合分票即先后脚；切换器+en 包+抽样腿天然同票；独占波 B15~B16 |
| FR-150 拆 2~3 票 | **3 票**（引擎 / 三消费面 BE+audit / FE 消费面） | 引擎纯新包独立；三消费面跨 GC/备份/复制三域但共享调度 REST 面（拆开即三票同面先后脚）；FE 呈现归 145.7 口径 |
| QA 两票 / tech-writer 一~两票 / release 一票 / PM 一票 | T-456+T-466 / **T-458 一票两腿** / T-465 / T-460 | tech-writer 两腿解锁时序差票内先后笔吸收（M15 T-426 先例）；QA 中期 B10（批次②③+BE 域收口窗） |
| 条件票 slot | T-467/T-468 占号；E7/license ADR/t381 不占号 | T-467（Q10 PM 倾向转正——触发路径明确）/ T-468（LC-96 条件池在册）为实体条件票（插空形态）；余三项无触发信号不立实体（M14 D3 先例） |

## 7. 歧义与口径登记（不阻塞拆票，交 conductor/PM）

1. **remote Test 端点归属**：FR-143.5 要求 Test 连通性，但 LC-83 契约面未列 + §5.3「新端点全集 = LC-93/94/95 + 调度 REST 面 + 零私加」未含该端点。处置：BE 腿暂归 T-442（adapter/remote 域——Engine.TestTarget 同构复用），wire 形态（replication Test 族复用 vs repositories test 新路由）票内与 conductor 核定后落 LC 登记；PM 下版统一措辞。
2. **i18n 外提时点**：PRD §1.3「文案外提集中票〔先于/伴随批次票〕」vs conductor「i18n 波独占——与所有 FE 票互斥」。处置：**独占波后置**（B15——批次②~④改造完成后的终态文案一次外提，避免先行键化随组件重写报废）；FE 票派单附「新增文案集中常量」纪律降本；工期告急时压缩选项 = 批次③④间插半波（conductor 裁量）。
3. **FR-150 FE 消费面归属**：PRD 145.7（安全/shell 栈）与 150.3（三消费面）双载。处置：BE 归 T-450、FE 归 T-462（一波一票纪律下排批次④后）；PRD 145.7 文本「呈现消费 FR-150 调度域」与本拆一致——PM 下版加互指。
4. **QA 中期 L48 覆盖**：T-456（B10）时 T-450（B7）已合入——引擎腿+零重复投递双实例腿纳入中期；T-462 FE 消费面（B14）晚于中期——next-run 呈现归终验 L40b/L48 全量。
5. **分页 ×9 消费点清单**：审计 B-3.3 未列全 9 处。处置：T-451 票内先 grep 盘点「加载更多」族全量清单入锚册再迁移；实际多于 9 以盘点为准（LC-98「×9」以 B-3.3 实测为基）。
6. **System Logs 数据源**：日志尾随载体（文件尾随 vs API）未定。处置：T-459 票内定案——零新端点优先；若需端点走 PM FR 增补流程（§1.4 条款 6，§5.3 零私加口径）。
7. **T-434 遗留三条归位**：?focus= 发射端翻新→T-449 AC2；文件深链多一请求→NFR-P70 观察项维持（非热路径）；锚册 v1.30 §0 版本史行缺席→T-437 AC1（ux 票内补记）。
