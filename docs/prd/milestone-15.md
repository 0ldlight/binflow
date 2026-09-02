# PRD — M15 搜索基建专程（AQL 首程）：AQL 查询语言与执行引擎 + 老搜索首批端点 + virtual 聚合浏览收口 + 复制包 B 首批

> **PRD 状态：v1.1 收口笔（2026-09-02，T-427——PM 执行期 Q 终裁联动回写；终版归 m15-done 收口窗〔conductor〕。v1.0 已转正：conductor 2026-09-01 09:1x——Q5/Q6 即裁）**。主轴选题：**AQL 专程**——主矩阵十大缺口 2（「查询面：企业日常操作入口」），**两度让位后第三程兑现**（M13 主轴让位 webhook〔M13 PRD §2.2 列「M14 专程候选第一顺位」〕→ M14 主轴让位 UI-parity〔用户指令 2026-08-30，M14 PRD §2.2 留痕「滚 M15 专程候选第一顺位」〕——两次让位均非 PM 裁量，本程无用户指令不再旁移）。**取证路径特例（webhook.md 先例复用）**：AQL 有 JFrog 官方文档（语言/域/字段/分页/排序全覆盖）→ **官方文档为唯一行为基准**，inv-1 §E / inv-2 §1.C 反编译锚点补白（端点形态/域清单/QRL 并发上限）；**t226 活体核验腿可用**（AQL 系 AddonType `oss` 档——inv-2 §3 枚举，OSS 7.84.10 实例可活体对照，无 replication 式 entitlement 锁）。范围基线：ROADMAP「M14 未纳入项」（已启用候选池——PM 收编裁决 §2.2 逐条留痕）+ T-406 遗留两项 + 复制包 B 首批（T-402a ②段登记）。**体量判定：AQL 超单里程碑 → 分阶段建议（§1.1 输入 5 / §2.2 / Q1 终裁）——M15 = AQL 核心（item+property 域 + 引擎 + 分页）/ M16 = AQL 高级面（statistics/usage 域 + QRL 全量 + UI 搜索族）**。

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/milestone-15.md` |
| 里程碑 | M15 — 搜索基建专程（AQL 首程）——AQL 规格票与语言/执行引擎（item+property 域 + include/sort/offset/limit + ACL 同源过滤 + 资源治理门简化版）+ 老搜索首批端点（gavc/prop/pattern——SR-03/SR-04 断言反转）+ 控制台搜索面升级 + virtual 仓聚合浏览（FR-21-AC8 兑现）+ remote 远端浏览评估票 + 复制包 B 首批（Replicate Now / Test / blockPush·blockPull；cron 双轨 Q5）+ 契约漂移与后端硬化小包（mint 500→400 / remote busy 重试预算）+ 工程债与文面小包 |
| 状态 | **v1.1 收口笔（2026-09-02，T-427）**——Q 总账归位（Q4/Q5/Q6 终裁落章 / Q2·Q3 随 aql.md + ADR-0043 规格回写归位 / Q1 收口窗终裁〔材料已齐 §7〕/ Q7 维持登记）；契约矩阵 12 条终版〔**A 10 / C 2 / 待裁 0**——LC-76 归 A（Q4 终裁出口 C 批 1）〕+ §5.7 全景表 as-built 对账（+archive/latestVersionByProperties 两外挂端点补登）；K62~K66 全量回填实装值；断言反转三处已落 + K64 局部翻转留痕；ROADMAP「M15 未纳入项」备稿落笔（intake ⑤ M16 全翻案语境衔接标注）。（v1.0 基线：FR-132~FR-140 九条需求；档位矩阵增量 0 行〔19 槽维持〕；L20~L34 验收命令骨架） |
| 上游依据 | PRODUCT.md（高频子集承诺——AQL 只承诺语言子集；Non-goals 不越界：不为 AQL 而建 Build-info/Release Bundle 域）、ROADMAP「M14 未纳入项」（候选池已启用——主轴候选/滚程三项/执行期改判与出口登记/票级遗留三簇/条件票出口/范围增补留痕/T-406 收口笔并入两项）、docs/reverse/inv-1-core.md（§E AQL 行〔`POST /api/search/aql?compact` + 九域清单 + SQL builder/optimizer + AqlTooManyRequestsException 接 QRL——高置信〕+ QRL 行〔`v1/system/query_rate_limiter` 三态 + 指标 job〕）、docs/reverse/inv-2-surface.md（§1.C 14 种老搜索端点枚举 + UI 搜索族〔artifactsearch/stashResults/packagesSearch/syntax-search〕+ **§3 AddonType `oss` 档含 AQL**——档位映射与活体核验依据）、docs/reverse/artifactory-full-feature-matrix.md（十大缺口 2 + §338 子集建议〔「可先做 artifact/gavc/pattern 子集 + 简化查询语言」——本 PRD 分阶段策略的同源依据〕+ §限流 QRL 行）、docs/reverse/rest-api.md（§4 checksum 搜索高置信——既有面回归基线）、JFrog 官方文档（AQL 语言/域字段/操作符/include·sort·offset·limit + Artifactory REST Search 域——**唯一行为基准，webhook.md 先例**）、docs/design/architecture.md（nodes 表列面〔repo_key/path/sha256/size/mime/created_by/created_at/updated_at——name/depth 由 path 派生〕+ §15.3.2 node_props 关联表与 `idx_node_props_name` 预留索引〔**M10 预留索引的兑现票**〕）、docs/prd/milestone-3.md（FR-21-AC8——virtual 聚合浏览 P2 未兑）、docs/prd/milestone-4.md（SR-01/02 已有面 + SR-03 gavc「P2 still closed」+ SR-04 未实现族 404 断言 + §5.5 K2 匹配语义暂行〔「Artifactory 语义待逆向校准」——M4 起欠账本程清偿〕）、docs/prd/milestone-13.md（§2.2 AQL 滚程留痕 + webhook.md 官方文档基准先例 + 体例）、docs/prd/milestone-14.md（体例 + LC-67/Fr-131/L19/Q7 止编号续接 + §2.2 AQL 让位留痕）、BOARD.md M14 收口节（T-406 遗留两项 + 复制包 B 登记〔T-402a ②段——cron 双轨/Replicate Now/Test/全局封锁「产品语义决策非纯 parity，滚 M15 候选 + Q 项登记」〕+ T-386 mint 契约漂移〔500≠400〕+ T-399 F1 首红 103.36MB + m14-done 收口笔〔F1 调基 120MB / M15 候选池开局 AQL 第一顺位〕）、reports/agents/T-402a.md（R 系条目 + 包 B 双源材料 + t226 OSS entitlement 锁实证——AQL 无此锁的反面参照）、reports/agents/T-406.md（remote 缓存浏览/virtual 成员感知空态 as-built）、reports/agents/T-377.md（D1 busy——24 路 0.27% SQLITE_BUSY）、reports/agents/T-364.md §5-③ + T-366.md §4-2（Replay/outbox 行级登记）、reports/agents/T-367.md（成员同型）、reports/agents/T-386.md（mint 漂移登记 + Q4 Tokens 字段集降级）、docs/reverse/replication.md（包 B 域面基线）、docs/reverse/npm.md（K60 先例——端点族规格化体例）、ADR-0001（clean-room——AQL 公开文档优先条款）、ADR-0041（webhook.md「官方文档为唯一行为基准」先例 + outbox 引擎——Replicate Now 的任务载体参照） |
| 下游消费者 | tech-lead（拆票——票号 T-407 起；宽度 ≤2 内建，§1.3 分票提示）、reverse-engineer（**aql.md 新建**——官方文档逐条锚点 + inv 补白 + t226 活体核验腿 + 搜索域口径归一；replication.md 增量段——包 B 双源材料）、architect（**ADR-0043**：AQL 引擎架构〔文法子集/AST→参数化 SQL 映射/ACL 织入/资源治理/错误形态〕；httpapi WriteTimeout 与长查询的交互随本 ADR 评审——T-392 登记归 architect）、dev-go-core（AQL 引擎 + 老搜索端点 + virtual 聚合 + mint 修正 + busy 预算——主 lane）、dev-registry-adapter（remote 远端浏览评估票的 per-协议上游枚举能力面）、dev-frontend（搜索面升级 + L2 自有增强 + member-pop/列选器推广）、qa-engineer（t226 AQL 活体核验腿 + L20~L34 + 断言反转两处核实 + 双形态回归）、tech-writer（AQL 用户指南 + 搜索 API 参考 + 搜索页/浏览/复制文档增量）、release-engineer（烟测 + UAT 随里程碑 PR）、conductor（Q1~Q7 裁决窗 + tag m15-done） |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-09-01 | 初版草案（待 conductor 审）：M15 范围（主轴 AQL 专程首程 + 候选池收编〔virtual 聚合 P1 / 复制包 B 首批 P1/P2 / mint 修正 P1 / busy 预算 P2 / L2 增强 P2 / 债包 P2〕+ 评估票 remote 远端浏览）、FR-132~FR-140、契约矩阵 LC-68~LC-79（A 8 / C 3 / 待裁 1）+ 搜索域端点全景归属表（§5.7）、档位矩阵增量 0 行、L20~L34、开放问题 Q1~Q7 带暂行（AQL 分阶段边界为 Q1 首裁项）；随稿完成 ROADMAP M15 立项行 + 当前里程碑头切换（PM 职责内两处，沿 M13/M14 v1.0 先例）。**当日转正**：conductor 2026-09-01 09:1x（Q5 不引入 cron / Q6 docker virtual 开禁即裁；Q1/Q3 暂行确认） |
| v1.1 | 2026-09-02 | **T-427 PM Q 终裁联动回写（收口笔）**（依据：aql.md〔T-407 五定案〕+ ADR-0043 及三笔勘误 + T-425 Q4 材料与终裁 + 各实现票 as-built）：① §7 Q1~Q7 逐项归位（终裁落章 3〔Q4 出口 C 批 1——LC-76 归 A / Q5 cron 不引入 / Q6 开禁→T-431〕+ 规格回写归位 2〔Q2 K63 定案 / Q3 400 维持〕+ 维持暂行 2〔Q1 收口窗必裁材料齐 / Q7 登记型〕）；② §5.6 K62~K66 全量回填实装值（K63 定案 1000/4/10s/429+Retry-After/**408**——503 暂行作废；K64 局部翻转——大小写不敏感对齐；K65 不顺车判 M16）；③ 契约矩阵 LC-68~79 终版（LC-76 待裁→A；as-built 状态逐行回填——计数 A 10 / C 2 / 待裁 0）；④ FR-133.1/133.3/133.4/134.5/134.6 规格校准落笔（$not 不存在 / C 层增强文案 / K63 定案 / K65 判定 / K64 落笔）；⑤ §5.4 断言反转三处 + K64 as-built 回写核对、§5.7 全景表 as-built 对账（+两外挂端点补登）、§8/§9 执行态注；⑥ ROADMAP「M15 未纳入项」备稿段落笔（T-427 票面 AC——启用归 conductor 收口窗；**intake ⑤ M16 全翻案语境逐条衔接标注**） |
| v1.2 | 2026-09-02 | **m15-done 收口笔（conductor）**：① **Q1 终裁落章——维持 §1.1-5 分阶段切分**（T-430 证据链闭合：M15 六环 as-built 增量复核全绿〔L21 PRD 原样命令活体〕+ 满载性能余量〔item P95 331.8ms / join 496.8ms vs 预算 500/800〕+ M16 边界零渗漏〔statistics/usage/QRL/UI 搜索族 404 维持〕+ QA 与 PM 意见一致；**M16 statistics 域优先**——与 M16 PRD v1.1 FR-146/FR-148 对位）；② ROADMAP「M15 未纳入项」段启用 + T-430 §5 三处增补（40.59MB 观察项 / offline 三处候小票 / T-431 出口清理）；③ T-430 终验 PASS 归档（25 票全落 + DoD 八条 + 144 提交归属 100%——D-T421-1 缺陷票号留痕） |

---

## 1. 背景与目标

### 1.1 背景

M14 以 `m14-done`（2026-09-01，T-400 终验 PASS 五 AC 全绿）收官：UI-parity 主轴（形态对齐批 1/批 2 + 品牌 logo 转正 + 包型图标 30 枚接线 + 活体核验 V1~V8）、docker remote 首航（三态齐装）、replication 交互对齐增补（T-402/T-404/T-405）、T-406/T-406b P0 热修（remote/virtual 浏览红卡消除）。M15 面对五股输入的汇合：

1. **主轴兑现（AQL 专程，两度让位留痕）**：查询面是主矩阵十大缺口 2（「企业日常操作入口」），且是 M10 属性系统立项时点名的下游消费方（「属性作为一等查询维度——搜索/清理策略/复制同步三大后续消费方」，architecture §15.3）。BinFlow 现状：T-92 交付 `/api/search/artifact` + `/api/search/checksum` 两枚（M4，ACL 零泄漏），其余搜索族全部 404（SR-04 断言），AQL 无——「最大查询面差距」（inv-1 §E 原文）。**逆向规格完整度评估（PM 出具，任务要求）**：端点形态/域清单/QRL 关联三点高置信在案（inv-1 §E + inv-2 §1.C），但**语言文法（域.字段路径、操作符全集、$and/$or/$not 复合）、字段全集、include/sort/offset/limit 语义、错误形态、compact 变体均未覆盖**——须先出规格票（FR-132）；**有利条件两条**：① AQL 有官方文档全覆盖（webhook.md 先例：官方文档为唯一行为基准）；② AQL 系 `oss` 档（inv-2 §3）——t226 OSS 7.84.10 实例可活体核验，无 replication 式 entitlement 锁（T-402a 反面教训不复现）。
2. **候选池收编（ROADMAP「M14 未纳入项」已启用，PM 裁量 §2.2 留痕）**：T-406 遗留两项（virtual 聚合浏览〔FR-21-AC8 P2——用户主诉「无法展示制品」的 P0 热修收口后的自然补全〕+ remote 远端浏览〔评估票——per-协议上游枚举语义决策〕）；执行期候选（mint 500≠400 契约漂移修正〔T-386 登记〕、复制包 B 首批、L2 自有增强）；滚程三项维持滚程（Replay REST / busy 预算 / 成员同型——其中 busy 预算改判收编：BE 里程碑回归窗一次覆盖，§2.2 留痕）。
3. **老搜索并轨（同域合并评估，任务给定）**：inv-2 §1.C 枚举 14 种老搜索端点（BinFlow 已有 2、关闭 12）——与 AQL 共享同一查询引擎基座（nodes/node_props → SQL），并轨立项分批实现；M4 起的两笔欠账同场清偿：SR-03（gavc「P2 still closed」）与 §5.5 K2（artifact name 匹配语义「待逆向校准」）。
4. **M14 as-built 基线**：T-406 落地「remote 仓列缓存行（浏览不回源）+ virtual 成员感知空态」——FR-136 virtual 聚合落地后，`tree-empty-virtual` 空态锚将翻转为聚合实态（断言反转登记 §5.4）；T-405 落地 replication `PUT /{id}` enabled 最小面——包 B 首批在其上叠加，无返工。
5. **分阶段策略（PM 建议，Q1 终裁）**：AQL 九域中 BinFlow 基座就绪度分层清晰——item/property 域基座全备（nodes 表 + M10 node_props/预留索引）；statistics/usage 域 **dep 下载计数基建**（BinFlow 无 per-node 下载统计——M11 登记的 statisticsEnabled 行为化「待 stats 面立项」同族）；build/module/dependency/promotion/releasebundle/sensitive 域 **dep Build-info 域与 Release Bundle 域立项**（候选池在案未排期——不为 AQL 而建功能本体，与 M14 §2.2「不为对齐而建」同构防线）。故 **M15 = AQL 核心（item+property 域 + 语言/引擎/分页 + 老搜索首批 + 资源门简化版）/ M16 = AQL 高级面（statistics/usage 域〔dep stats 基建〕+ QRL 全量三态 + UI 搜索族〔stashResults/packagesSearch/syntax-search〕+ 剩余老搜索）**——「客户端真实可用」准绳：一个能被真实 curl/jf 风格脚本走通的 AQL 子集 > 九域只有 happy path。**（v1.1 注：M15 侧五环已全落 as-built——T-409/411/413/415/417/419；Q1 收口窗终裁材料见 §7）**

**不贪多**：M15 以「一条 P0 主线（AQL 规格票 → 引擎 → 老搜索首批 → 搜索面）+ 两条 P1 副线（virtual 聚合 / 复制包 B 首批）+ 小票与债包」为形态；任何 Q 触发的增项（dates/creation 余量顺车 / 远端浏览实现段 / docker virtual 矩阵开禁 / by-digest 强刷）必须走条件票/余量条款。

### 1.2 M15 目标与量化门槛

> 一句话：把查询面立起来——AQL 语言子集 + 执行引擎 + 分页排序全链（真实 curl 脚本可走通、ACL 零泄漏、资源有门），老搜索首批三端点并轨同一引擎，virtual 仓浏览收口聚合实态，复制包 B 兑现手动同步三件套——M10 属性系统预留索引的兑现程。

量化门槛（未达即里程碑不完成）：

| 指标 | M15 门槛 | 来源 |
|---|---|---|
| AQL 规格票 | aql.md 落盘：官方文档逐条锚点 + inv 补白 + t226 活体核验结论（语言/字段/分页/错误形态逐项置信度标注）+ 搜索域口径归一（14-vs-13 勘误定案 + BinFlow 子集边界 + M4 K2 匹配语义校准值）；tech-lead 就绪度确认 | FR-132 |
| AQL 全链 | curl `POST /api/search/aql`（+`?compact`）：item 域字段查询（repo/path/name/type/size/created/checksums 操作符复合）+ property 域嵌套 + `.include()/.sort()/.offset()/.limit()` 全链绿；语法错 → 400（errors[] envelope）；未支持域 → 400 诚实拒绝（零伪空集） | FR-133 |
| AQL ACL | 非 admin 调用者结果按可读仓过滤（与 T-92 同一 allow() 源）——越权仓零出现探针（t92_search_test.go 探针形态复用） | FR-133 |
| AQL 资源门 | 单查询结果上限截断语义（K63）+ 并发上限超限 → 429 + 执行超时；满载下零 5xx 零 OOM | FR-133 |
| 老搜索首批 | `GET /api/search/gavc`（mvn 坐标真实数据腿）/ `prop`（node_props 真实属性腿）/ `pattern`（通配路径腿）curl 全链绿；SR-03/SR-04 断言反转归位（404 → 分派）；既有 artifact/checksum 零回归 + K64 校准落笔 | FR-134 |
| 搜索面 FE | 搜索页 AQL 模式（编辑器 + 结果表）；列选器推广 users/groups/search 三页；member-pop hover ≥4.5:1 | FR-135 |
| virtual 聚合 | `GET /api/storage/<virtual>/<path>` → 200 children = 成员并集（FR-21-AC8 逐字兑现）；同名路径合并语义与 t226 对照；`tree-empty-virtual` 空态锚退役/翻转（锚册留痕）；M3 FR-21 序列零回归 | FR-136 |
| 远端浏览评估 | per-协议上游枚举能力矩阵归档（13 包型逐型：上游有无目录枚举 API/翻页形态/成本）+ 分期建议 + Q4 材料——评估票不设实现断言 | FR-137 |
| 复制包 B 首批 | Replicate Now：触发全量推送任务（幂等 + 任务状态可查）；blockPush/blockPull：全局封锁生效（新事件不入队/拉侧封锁）且 UI-API 不受门（t226 实测语义）；Test：上游可达+认证探测；cron 双轨 Q5 终裁留痕 | FR-138 |
| 契约与硬化 | mint unknown username → 400（auth-model 3.1 逐字文案——as-built 500 漂移修正）；busy 预算：24 路并发缓存树重放零 5xx（0.27% 边角清零）+ SQLite 写路径专项回归 | FR-139 |
| 资源与预算门 | footprint ≤100MB / check-size ≤120MB（m14-done 调基门）维持；冷启动 <2s；SPA 每票增量 ≤10KB；AQL 引擎净增量 footprint 可忽略（纯 Go 代码无新资产）；F1 六平台聚合趋势观察项登记（103.37MB → M15 不显著上浮） | §6.2 |

> **v1.1 执行态注**（2026-09-02 04:3x 基线，M15 18/25 + T-432①）：AQL 全链 / ACL / 资源门 / 老搜索首批 / 搜索面 FE / virtual 聚合 / 远端浏览评估 / 复制包 B 首批（Replicate Now + Test + 封锁双开关）/ mint 400 / 文档票**已达标归档**（T-409~T-417/T-419/T-420/T-422/T-425/T-426——done 条目 BOARD 在档）；busy 预算（T-423）/ L2 快捷 + e2e 纪律（T-424）/ 文面簇（T-428）/ release（T-429）/ 终验（T-430）在途；docker virtual 开禁（T-431）波外候插空；QA 中期回归（T-421）押后至审计 workflow 完结（串行净机——共租负载教训）。

### 1.3 上游依赖与并行关系（含分票提示）

- **前置产物（拆票前/B1 首波）**：① **aql.md 规格票**（reverse-engineer；官方文档逐条锚点 + inv-1 §E/inv-2 §1.C 补白 + t226 活体核验腿〔AQL oss 档可用——容器恢复动作沿 T-381 §0 先例〕+ 搜索域口径归一〔14-vs-13 勘误 + 子集边界 + K2/K64 匹配语义校准〕+ virtual 仓在 AQL 中的语义活体锚定）；② **ADR-0043**（architect——AQL 引擎架构：语言子集文法形式（EBNF）/AST→参数化 SQL 的编译映射（nodes + node_props join，name/depth 派生策略，sha1/md5 存储或即时计算）/ACL 谓词织入点/资源治理门参数（K63）/错误形态与 E-01 envelope 的关系/httpapi WriteTimeout 与长查询交互（T-392 登记项随本 ADR 评审归位））；③ replication.md 增量段（包 B——executereplicationnow/blockPush·blockPull/Test 三面双源材料，T-402a R 系在案补官方 REST 文档锚点）。
- **依赖序**：FR-132（规格）+ ADR-0043 → FR-133（引擎大票，窗口独占）→ FR-134（老搜索首批，同引擎消费——引警票先出查询内核，端点票可半程并行）；FR-135 dep FR-133/134 端点稳定（FE 后波）；FR-136 零依赖可 B1 起步（repo service 域，与 AQL 的 internal/search 新包天然错峰——序列化归 tech-lead）；FR-137 评估票零依赖任意波次；FR-138 dep replication.md 增量段（Replicate Now 任务载体消费 ADR-0041 outbox 模式参照，非引擎改动）；FR-139 零依赖任意波次（mint 小票可 B1 插空；busy 预算宜后波与 AQL 满载回归同场）；FR-140 零依赖任意波次。
- **实现分区与分票提示（宽度 ≤2 内建）**：FR-132 拆 1~2 票（规格票 + 活体核验腿可同票）；FR-133 拆 2~3 票（语言前端〔lexer/parser/AST 校验〕→ 执行内核〔planner/SQL 编译/ACL 织入/资源门〕→ 端点与 compact/错误面〔httpapi〕）；FR-134 拆 1~2 票（三端点同票或 gavc 独立）；FR-135 拆 1~2 票（搜索页 AQL 模式 / 列选器三页+member-pop）；FR-136 拆 1~2 票（service 聚合 + FE 树消费）；FR-137 一票（评估票）；FR-138 拆 2~3 票（Replicate Now / 封锁双开关 / Test）；FR-139 拆 2 票（mint / busy）；FR-140 拆 2 票（测试基建纪律 / 文面回写簇）；QA 两票（中期回归 + 终验）；tech-writer 一~两票；release 一票（烟测 + UAT 随里程碑 PR）；PM 裁定票一票。估 **19~25 票**（含条件票 slot：dates/creation 余量顺车 K65 / 远端浏览实现段 Q4 / docker virtual 矩阵开禁 Q6 / by-digest 强刷 Q7）。
- **area 错峰**：AQL 主线 = 新包 `internal/search`（或 architect 定名）+ httpapi——virtual 聚合 = `internal/repo/service.go` 域，**不同包可并行**，但均含 httpapi 注册面（路由注册不冲突，tech-lead 序贯化合入）；复制包 B = `internal/replication`（零重叠）；mint = `internal/httpapi` auth 域小改；FE 全部 `web/src`（搜索页 + 三列表页 + 增强，与 BE 零重叠）。**注意**：FR-136 与 FR-133 的 ACL/仓解析共享 `repo.Service` 读面——设计评审时对齐接口，不跨包摸内部结构（Go 规范）。
- **QA 并行面**：AQL 需查询语料夹具（结构化树：多仓/多深度/多属性——M10 属性票夹具可扩）+ t226 活体核验腿（AQL 查询对拍——oss 档可用）+ 越权探针（t92 形态复用）；busy 预算需 24 路并发编排（T-377 D1 复现脚本）；Replicate Now 需双实例编排（自指上游——M6 复制既有夹具）；virtual 聚合需多成员夹具（M3 FR-21 既有）；断言反转两处（SR-03/SR-04 404 → 分派 + tree-empty-virtual 空态翻转）须 PRD 回写核实。

### 1.4 全程工作方式条款（M11~M14 §1.4 制度延续 + M15 搜索域专项——常设准绳，全程生效）

1. **行为逐项对齐（搜索域公开文档特例）**：AQL 与老搜索的可观测行为（端点/参数/envelope/错误码/分页语义）100% 照 Artifactory；**AQL 域取证特例沿 webhook.md 先例**：官方文档为唯一行为基准（ADR-0001「有公开规范的以官方文档为准」），inv-1 §E/inv-2 §1.C 反编译锚点仅补文档空白（九域清单/QRL 关联/compact 变体）；t226 活体为置信度校验腿（oss 档可用——非 entitlement 锁面）。
2. **子集诚实边界（本主轴的宪章级纪律）**：BinFlow 实现的是 AQL 语言**子集**（item+property 域）——未支持域的查询 **400 诚实拒绝**（明确「domain not supported」语义，规格票定文案），**零伪空集零静默吞**；端点全景表（§5.7）逐条标注归属（已有/M15/M16+/远期 dep），不留模糊地带。
3. **低置信不进断言（M14 条款延续）**：官方文档未明示、inv 无锚、t226 未核验的细节（字段全集边角/错误文案逐字/上限默认值）核验前只作「以核验为准」附注；核验回写 = 改契约（aql.md 修订留痕）；**效力序：用户裁决（BOARD）> aql.md 规格票〔核验回写后〕> 本 PRD 暂行值**。
4. **ACL 零泄漏（T-92 血统延续）**：全部新查询面（AQL + 老搜索新增端点 + virtual 聚合浏览）与内容面同一 `allow()` 路径、按调用者过滤；越权仓零出现探针为每票硬 AC——搜索是泄漏面最宽的域，此条优先级最高。
5. **红线保留（ADR-0001 不变）**：不逐行翻译 Java→Go（AQL 的 SQL builder/optimizer 架构可参照、代码零翻译）；语言文法照官方公开契约（数据契约非代码）；QRL 全量版（三态 + 指标 job + REST）M16+，M15 只做资源门简化版（K63，自有设计 C 层登记）。
6. **流程条款（延续）**：新端点走 PM FR + ADR 流程（本程新增端点群：AQL 1 + 老搜索 3 + 复制包 B 3——均本 PRD 承载，ADR-0043 立项）；拆票宽度 ≤2；行为面票必须依赖规格票；每票 AC 附可执行验收命令（curl/Playwright）。

---

## 2. 范围

### 2.1 In scope

| # | 来源（指令/裁决/登记） | 本 PRD 功能需求 | 优先级 |
|---|---|---|---|
| A | 主轴（两度让位兑现 + 主矩阵缺口 2 + inv-1 §E/inv-2 §1.C + 官方文档） | FR-132（aql.md 规格票 + 搜索域口径归一 + t226 活体核验腿 + 基座就绪度核对）+ FR-133（AQL 语言与执行引擎——item+property 域 + include/sort/offset/limit + ACL 同源过滤 + 资源治理门 + `POST /api/search/aql`） | P0（前置锚 + 主线） |
| B | 老搜索并轨（inv-2 §1.C + M4 SR-03/SR-04 + 矩阵 §338 子集建议） | FR-134（老搜索首批端点：gavc / prop / pattern——同引擎；dates/creation 余量条件 K65；artifact/checksum 维持 + K64 匹配语义校准） | P0（dates 余量 P2） |
| C | 搜索面触达（FE 消费）+ 候选池 FE 类（列选器推广 T-387 遗留 + member-pop T-391 L-a） | FR-135（控制台搜索面升级：AQL 模式 + 列选器三页推广 + member-pop hover） | P1 |
| D | T-406 收口笔并入第一项（FR-21-AC8 P2 兑现） | FR-136（virtual 仓聚合浏览——children 并集，Artifactory 同形态） | P1 |
| E | T-406 收口笔并入第二项 | FR-137（remote 仓远端浏览**评估票**——per-协议上游枚举能力矩阵 + 分期建议 + Q4 材料） | P2（评估不设实现断言） |
| F | T-402a ②段登记（复制包 B——产品语义决策候选） | FR-138（复制包 B 首批：Replicate Now / Test 连接 / blockPush·blockPull 全局封锁；cron 双轨 Q5 不裁不建） | P1（Test/封锁 P2） |
| G | 执行期候选（T-386 mint 契约漂移 + 滚程改判收编 T-377 D1 busy） | FR-139（契约漂移与后端硬化小包：mint 500→400 + remote busy 重试预算） | P1 / P2 |
| H | 候选池（L2 自有增强 T-381 §V4 建议 + 票级遗留两簇） | FR-140（FE 自有增强小票 + 工程债与文面小包〔测试基建纪律成文 + 文面回写簇〕） | P2 |
| I | 候选池裁量（不设 FR） | Tokens 字段集补核验（Q4 残留——候商业版/云活体源**条件票**，未触发不排期）；NuGet symbol server 余量五承（T-403 延续）；E7 toast 锚位（候用户信号，维持不改） | — |

前置产物（非 FR 行）：aql.md 规格票（= FR-132 承载）、ADR-0043（architect，拆票前 Accepted 目标）、replication.md 增量段（随 FR-138 票）。

### 2.2 Non-goals — M15 明确不做

**产品级（继承 PRODUCT.md，不越界）**：不为 AQL 而建 Build-info / Release Bundle / 制品 license 识别域（AQL 九域中 build/module/dependency/promotion/releasebundle/sensitive 六域与 license/dependency/buildArtifacts 搜索**全部 dep 域本体立项**——候选池在案未排期，「行为对齐不自动解锁功能本体」防线维持，与 M14 §2.2 同构）。不做 Artifactory 全量 REST 兼容（高频子集承诺——AQL 只承诺语言子集）。

**M15 里程碑级 Non-goals（含候选池收编判定留痕——去向全部登记）**：

| 不做项 | 隔离边界 / 去向（PM 判定理由留痕） |
|---|---|
| **AQL statistics/usage 域 + usage(usageSince) 搜索** | **M16 AQL 高级面**——dep per-node 下载计数基建（BinFlow 无此统计面：M11 登记 statisticsEnabled/sourceOrigin 落库无行为「待 stats 面立项」同族）；不为查询域先建统计本体 |
| **QRL 全量版**（`v1/system/query_rate_limiter` 三态 + 指标 job） | M16——M15 落资源门简化版（K63：上限/429/超时三件，C 层自有设计）；三态仿真与 REST 面随高级面 |
| **UI 搜索族**（artifactsearch/stashResults/packagesSearch/syntax-search） | M16——控制台搜索页 M15 先升 AQL 模式（FR-135），结果暂存/包级索引族随高级面 |
| **剩余老搜索**（dates/creation/badChecksum/versions/latestVersion） | M16+（dates/creation 若规格票判 trivial 可余量顺车——K65 条件条款，非 DoD）；badChecksum dep 校验扫描遍历；versions/latestVersion dep 包型坐标语义细化 |
| **Replay + outbox 行级 REST 面**（T-364 §5-③ + T-366 §4-2） | **滚 M16（webhook 域二程）维持**——M14 判定理由仍立（运营增强非协议兼容面、机制已备翻转面小）；新增理由：与 AQL 同挤 httpapi/dev-go-core lane，AQL 专程优先 |
| **virtual 成员同型全包型推广**（T-367） | **滚 M16+ 维持**——需 13 包型 × 三 rclass 全量回归矩阵，dev-go-core 容量让位 AQL 专程；现态缺陷面窄 |
| **remote 远端浏览实现段** | **Q4 已终裁（conductor 2026-09-02 04:2x）——出口 C 批 1**：helm + deb + rpm（可选档 `listRemoteFolderItems` 对位语义，**默认 false 维持缓存浏览 = T-406 as-built 同形态——不欠默认 parity，欠可选档**）；docker tags 腿不采（Artifactory 官方设置面未开放该型——做即超 parity L2）；maven/generic HTML 抓取族不做（官方未写算法，中置信无锚）；LC-76 归 A；实现段 ~3 票 M16 登记（ROADMAP「M15 未纳入项」Q 实现段——牵连 T-412 listVirtual remote 成员口径扩面） |
| **cron 双轨**（Artifactory 用户级 cron 复制） | **Q5 已终裁（conductor 2026-09-01 09:1x）——不引入**：事件驱动 + 1min sweep 维持唯一引擎，手动全量场景 Replicate Now 承接（T-420 已落地：幂等收敛 + enabled:false → 409 + 双实例 sha256 一致实证）；M16 复制域二程不再列 cron 为实现项；**intake ⑤ 翻案语境下如重开，须列与本裁定冲突点交用户确认（BOARD 在档，不默默翻转）** |
| **HA 本体 + Xray 集成面** | 沿 M12~M14 Q 终裁维持「单列专程」——前置 PRODUCT.md「明确不做」修订解禁（用户动作，截稿未发生） |
| **E7 toast 锚位微调 / Q4 Tokens 字段集补核验** | 维持登记——E7 候用户信号（V3 实证显著不同但 BinFlow 既有有意设计，默认不改）；Tokens 候商业版/云活体源条件票（V6c 降级登记维持） |
| **t381 事故残留清理**（VM 取证快照 15MB + 空仓） | conductor 决定项（非 PM 裁量）——登记维持；REST 删被 OSS license 门挡的处置随 conductor |
| **逐行翻译 AqlOqlParser/SQL builder → Go** | **永久不做**（ADR-0001——官方文档契约对齐，实现自有） |

---

## 3. 用户与场景（M15 视角）

- **场景 A（平台工程师，仓治理）**：AQL 一句话答「哪些仓里 30 天前创建的 jar 超过 100MB」——`items.find({...}).include(...).sort(...)` curl 即得；迁移团队现成的 AQL 脚本改个 base URL 就能跑（语言子集内零学习成本）。
- **场景 B（Artifactory 迁移用户，脚本兼容）**：既有 CI 里 `jf rt search --spec` / curl 老搜索（gavc/prop/pattern）的脚本对 BinFlow 直接可用——高频子集承诺在查询域的兑现。
- **场景 C（管理员，浏览收口）**：virtual 仓树不再是「成员感知空态」——展开即成员并集（与 Artifactory 同形态、与 local 仓同手感）；T-406 热修后的最后一块浏览拼图。
- **场景 D（复制运维）**：配置完 push replication 后不用等事件——Replicate Now 立即全量同步；上游维护窗口一键 blockPush 封锁（API 调试通道不受门）；Test 连接配置期即知上游可达与认证对错。
- **场景 E（低权限用户/审计员）**：搜索结果永远只见有权见的仓——搜索是泄漏面最宽的域，ACL 零泄漏探针逐票在案。
- **场景 F（QA/维护者）**：aql.md 置信度列 = 可审计契约基线（t226 活体回写后）；断言反转两处（SR-03/SR-04）与空态锚翻转（tree-empty-virtual）归属清晰。

---

## 4. 功能需求

约定：`BASE=http://127.0.0.1:8080`；`ADMIN="admin:password"`；搜索域错误契约沿 M1 §5.1——search 域走 **E-01 `errors[]`** envelope（M4 §5 既定）；**Artifactory 行为基准 = JFrog 官方文档（AQL 语言 + REST Search 域）+ aql.md 规格票（inv 补白 + t226 活体核验回写后）**，效力序见 §1.4 条款 3；全部实现票不得私加端点（新增端点全集 = §5.7 全景表所列 M15 归属行）。

### 4.1 前置锚：aql.md 规格票与搜索域口径归一（主轴地基，P0）

#### FR-132 aql.md 新建 + t226 活体核验 + 口径归一（reverse-engineer；14-vs-13 勘误 + K64 校准 + 基座就绪度核对）

**用户故事**：
- 作为本团队，AQL 引擎的验收断言必须建立在规格锚上——语言文法/字段全集/分页语义官方文档逐条锚定，inv 补白，t226 活体校验（AQL 系 oss 档，活体腿可用——T-402a entitlement 锁教训不复现）。
- 作为后续里程碑的消费者，aql.md 的域/字段表（含「M15 子集 / M16 / 远期 dep」归属列）= 搜索域长期契约基线。

行为规格：

- **132.1 官方文档锚点**：AQL 语言全集（域.字段路径语法 / 操作符集 / `$and/$or/$not` 复合 / `.include()/.sort()/.offset()/.limit()` 尾缀方法链 / compact 变体）+ 九域清单（item/statistics/property/build/module/dependency/promotion/releasebundle(+file)/sensitive——inv-1 §E 高置信对拍）+ REST 端点 wire（`POST /api/search/aql`、请求 Content-Type、envelope 形态、错误码族）——逐条「官方文档锚点 + 行为描述」双列（webhook.md 体例）。
- **132.2 inv 补白**：inv-1 §E（AqlTooManyRequestsException→QRL 关联——资源门 429 语义的反编译锚）/ inv-2 §1.C（14 种老搜索端点参数面 + UI 搜索族清单）——官方文档未明示处以反编译类/方法锚点补。
- **132.3 t226 活体核验腿**：AQL 查询对拍（语言边角/错误文案逐字/上限行为/virtual 仓在 AQL 中的语义——返回实际存储行还是 virtual 视图）；**差集法只读**（INC-1 铁门槛——AQL 是只读查询面，天然低险，仍守探测纪律）。
- **132.4 口径归一**：① 14-vs-13 勘误（inv-2 枚举 14 种 vs 主矩阵「13 个」——计数差异定案并回写矩阵）；② BinFlow 子集边界表（item+property = M15；statistics/usage = M16 dep stats；六域 = 远期 dep 域立项）；③ **K64 校准**（M4 §5.5 K2 欠账：`/api/search/artifact` name 匹配语义「子串 LIKE / `*` 通配 P2 / 待逆向校准」——本票锚定 Artifactory 语义并给翻转/维持结论）；④ 基座就绪度核对（nodes 列面 → AQL item 字段映射表：name/depth 派生、sha1/md5 存储或即时计算——归 ADR-0043 消费）。

验收标准（AC）：

- **AC1（规格交付）**：aql.md 落盘——官方文档锚点逐条 + inv 补白标注 + 置信度列；tech-lead 就绪度确认（可拆、裁决点清单、缺项是否阻塞）。
- **AC2（活体核验）**：t226 AQL 对拍结论归档（含 virtual 语义锚 + 错误文案逐字样本）；核验源不可得项如实「以核验为准」标注（零静默升格——M14 条款延续）。
- **AC3（口径归一）**：14-vs-13 勘误定案回写主矩阵；子集边界表 + K64 校准结论落笔（翻转则登记断言反转票）；基座映射表交 ADR-0043。

### 4.2 AQL 语言与执行引擎（主轴 P0）

#### FR-133 AQL 语言子集（item+property 域）+ 执行引擎 + `POST /api/search/aql`（internal/search 新包 + httpapi；dep FR-132 + ADR-0043）

**用户故事**：
- 作为 CI 工程师，我用 AQL 按 repo/path/name/属性/时间/checksum 组合查制品——一次 curl 拿到结构化结果，迁移脚本零改写。
- 作为低权限用户，搜索结果只含我有权读的仓——与直连内容面同一权限语义，搜索不成为旁路。

行为规格：

- **133.1 语言子集（aql.md 定案为准，暂行如下）**：`items.find(<criteria>)` 查询本体——item 域字段（repo/path/name/type/size/created/modified/created_by/modified_by/checksums 族——全集归规格票）、property 域嵌套匹配（`{"$and":[{"@key":{"$eq":"license"}},...]}` 形态或官方等价——**M10 node_props 预留索引 `idx_node_props_name` 的兑现消费**）、操作符集（`$eq/$ne/$lt/$lte/$gt/$gte/$match/$nmatch` 官方 8 比较符 + `$last/$before` 相对时间——**v1.1 校准：`$contains` 不存在**，aql.md §2.4）、`$and/$or` 任意复合 + `$msp`（**v1.1 校准：`$not` 不存在**——官方 + 活体 400 双证，aql.md §0-1；v1.0「$not 任意复合」前提修正）；尾缀方法链 `.include(<fields>)` / `.sort({$asc|$desc:[...]})` / `.offset(n)` / `.limit(n)`（链序敏感——include→sort→offset→limit→distinct，aql.md §2.5 活体双证）。
- **133.2 执行内核（ADR-0043 定案为准）**：AST → **参数化 SQL** 编译（nodes + node_props join；name/depth 由 path 派生的策略、sha1/md5 取数路径归 ADR）——**零字符串拼接**（注入面唯一红线）；ACL 谓词在 planner 织入（repo_key 维度，与 T-92 `SearchArtifacts` 同一 allow() 源——不另建权限通道）；结果装饰器（include 字段投影 + envelope 组装）。
- **133.3 端点 wire**：`POST /api/search/aql`（body = AQL 文本）+ `?compact` 变体（紧凑结果形态——inv-1 §E 锚点，形态归规格票）；成功 200 envelope 照官方；语法错/非法字段/未支持域 → 400（E-01 errors[]；未支持域文案含 domain 名——诚实拒绝零伪空集，§1.4 条款 2）。**（v1.1 注：Artifactory 对未支持/未知域走通用 parse error 400、无专门域文案——aql.md §2.1 活体双证；BinFlow 域名提示系 C 层增强文案，码位 400 一致不违 parity；T-409 双轨落地——语法 E1 逐字 / 域·字段·操作符 C 层增强）**
- **133.4 资源治理门（K63 已定案并实装——v1.1 回填：ADR-0043 + 勘误 + T-413/T-415 as-built）**：单查询结果上限 **1,000 行**（截断语义：range.notification 官方逐字文案 + offset/limit 分页可达全量）；并发上限 **4**——超限 429 + Retry-After（内部常量零配置键）；执行超时 **10s → 408**（aql.md E7 官方错误码表 + ADR-0043 勘误定案——v1.0「400/503 形态待定」就此关闭，503 暂行作废）；查询长度 **6,000 字符门**（Parse 入口，字节 + rune 双条件）；满载下零 5xx 零 OOM（结果集流式装饰）。
- **133.5 virtual 仓语义**：照 aql.md 活体锚定结论（暂行：查询对象 = 实际存储行〔local + remote 缓存行〕，virtual 仓不作查询实体）。**（v1.1 定案：aql.md §7——暂行口径成立**，且 virtual key 须作合法查询值**编译期透明展开**〔ADR-0043 勘误三分支——展开≠权限通道〕；结果行 repo 字段 = 实际成员仓 key；不存在的 repo key → 200 空集。t226 无 virtual 仓活体对拍降级留痕——官方 + 反编译双源定案，BinFlow e2e 对拍腿归 T-415 归档）

验收标准（AC）：

- **AC1（语言全链）**：curl——`items.find({"repo":{"$match":"maven-local*"},"type":"file","$or":[{"size":{"$gt":104857600}},{"created":{"$lt":"2025-01-01T00:00:00Z"}}]}).include("repo","path","name","size","created").sort({"$desc":["created"]}).offset(0).limit(10)` → 200 结构化结果（排序/分页/投影逐字段断言）；property 嵌套匹配（M10 属性夹具数据腿）；`?compact` 变体双形态对照。
- **AC2（错误面）**：语法错 → 400 E-01；未支持域（如 `build.find` / statistics 字段）→ 400 domain-not-supported 语义；非法字段名 → 400——文案逐字照 aql.md 锚（核验样本对照）。
- **AC3（ACL 零泄漏）**：双用户夹具——受限用户 AQL 全域查询（无 repo 限定）与逐仓限定查询，越权仓行零出现（t92_search_test.go 探针形态复用+扩展 AQL 腿）；匿名/未认证 401/403 照 T-92 既有门。
- **AC4（资源门）**：超上限查询 → 截断标记 + 分页可达全量；并发 5 路（上限 4）→ 至少 1 路 429 + Retry-After；慢查询注入（全表 like）→ 超时形态；全程零 5xx 零 OOM。
- **AC5（回归）**：既有 `/api/search/artifact|checksum` 零回归（T-92 全量 spec）；`make test`（race）+ lint 全绿；k64 若翻转则 artifact 匹配语义断言更新归属本票豁免。

### 4.3 老搜索首批端点（同引擎收编，P0）

#### FR-134 gavc / prop / pattern 三端点 + dates 余量条件 + K64 校准落笔（httpapi + internal/search 消费；断言反转 SR-03/SR-04）

**用户故事**：迁移用户的既有脚本 `curl /api/search/gavc?g=&a=` / `?props=` / `?pattern=` 直接可用——M4 起排期两程的 P2 尾票（SR-03 gavc「still closed」）本程清账。

行为规格：

- **134.1 gavc**：`GET /api/search/gavc?g=&a=&v=&c=&repos=`——maven 坐标走 layout 结构匹配（M3 layout 解析器复用，兼容子集——M4 §5.2 既有口径）；mvn 真实部署数据腿（deploy 后按坐标检索命中）。
- **134.2 prop**：`GET /api/search/prop?props=<k>[=<v>]&repos=`——node_props 维度反查（M10 预留索引第二消费方）；参数形态照 aql.md 锚（官方 prop 搜索参数族）。
- **134.3 pattern**：`GET /api/search/pattern?pattern=<repo>:<path-glob>`——通配路径匹配（`**/*.jar` 形态；与 AQL `$match` 共享匹配内核）。
- **134.4 共通语义**：envelope `{"results":[FileInfo...]}`（FileInfo 形态复用 E-09/T-92 既有）；参数缺失/非法 → 400 E-01；ACL 同 FR-133（同一 allow() 源）；结果上限沿 K63（老搜索侧默认截断语义照 aql.md 锚）。
- **134.5 dates/creation 余量条件（K65——v1.1 判定：不顺车，M16）**：T-407 判「trivial」仅**数据腿**成立（created_at 单列区间直查）；**wire 腿非同构**（未命中 **404 `No results found.`** 空集族 + uri 瘦行〔非 E-09 FileInfo〕+ epoch 毫秒参数解析——aql.md §8.2），塞进本程只会做成半吊子（T-417 票内留痕）；M16 登记项含参数与行形态全集。
- **134.6 K64 校准落笔（v1.1 已落 T-417）**：`/api/search/artifact` name 匹配语义 aql.md §0-5 三源定案执行——子串语义本体**维持**（无族级断言反转）+ **大小写不敏感翻转对齐**（BinFlow 原区分大小写系漂移——LOWER LIKE 折叠转义，ASCII 折叠局限登记）+ `*` 通配维持字面（官方/反编译均按字面处理）；断言更新归属 T-417 豁免票**已执行**（M4 §5.5 K2 欠账就此清偿）。

验收标准（AC）：

- **AC1（gavc）**：mvn deploy 真实坐标数据 → `curl "$BASE/api/search/gavc?g=com.acme&a=demo"` 命中；v/c 限定臂 + repos 限定臂 + 未命中空集 200；坐标非法 400。
- **AC2（prop）**：M10 属性夹具（矩阵参数部署 + `?properties` 写入的数据）→ `curl "$BASE/api/search/prop?props=license=Apache-2.0"` 命中；键无值形态（该键任意值）臂；未知键空集 200。
- **AC3（pattern）**：`curl "$BASE/api/search/pattern?pattern=maven-local:com/acme/**/*.jar"` 命中树；跨仓通配臂 + 非法 pattern 400。
- **AC4（断言反转 + 回归）**：t92_search_test.go SR-03/SR-04 关闭断言（404）**反转为分派实现**（props/pattern/gavc 三行）——断言反转归属 M15 豁免票核实；既有 artifact/checksum 全量 spec 零回归（含 K64 落笔臂）。

### 4.4 控制台搜索面升级（FE 副线，P1）

#### FR-135 搜索页 AQL 模式 + 列选器三页推广 + member-pop 修（web/src；四闸门 + axe + 服务端 diff=0）

**用户故事**：控制台用户在搜索页切换「基本表单 / AQL 编辑器」两种姿势——AQL 结果表与既有搜索结果同框架；users/groups/search 三列表页获得与仓库/审计页同款列选器。

行为规格：

- **135.1 搜索页 AQL 模式**：模式切换（既有基本表单维持不动）；AQL 编辑器（textarea mono + 语法报错内联呈现——消费 400 E-01）；结果表复用既有列框架 + 列选器接入；**只读面零新端点**（消费 FR-133/134 既有端点）。
- **135.2 列选器推广（T-387 遗留——共享层 columnPrefs.ts 已就绪）**：users/groups/search 三页接入（per-page localStorage，键名沿 `binflow-console-cols-{users,groups,search}` 定式）；「无端点列不伪造」纪律维持。
- **135.3 member-pop hover（T-391 L-a）**：virtual Tab 浮层入口 hover 对比度同配方一行（亮暗双修，T-391 color-mix 配方现成）。

验收标准（AC）：

- **AC1（AQL 模式）**：Playwright——模式切换、合法查询结果渲染、语法错内联（400 文案透传）、分页/排序交互；`smu/search` 既有锚零改名 + 新锚入册。
- **AC2（列选器）**：三页列选开合/显隐持久（reload 保持）/全选复位——T-387 spec 形态复用；其余列表页不受影响。
- **AC3（闸门）**：四闸门 + axe 双主题 serious=0 + 服务端 diff=0 + SPA 每票增量 ≤10KB。

### 4.5 virtual 仓聚合浏览（T-406 遗留兑现，P1）

#### FR-136 virtual 聚合浏览——children 成员并集（internal/repo + web/src；FR-21-AC8 逐字兑现）

**用户故事**：作为用户，virtual 仓树展开即见全部成员仓制品（并集）——与 Artifactory 同形态、与 T-406 热修后的 remote/local 同手感，「无法展示制品」主诉的最终收口。

行为规格：

- **136.1 聚合语义**：`GET /api/storage/<virtual>/<path>` folder 面 → 200 children = 按成员顺序解析的全部成员 children **并集**（FR-21-AC8：「children 覆盖两成员的 artifactId 并集」）；同名路径合并为一行；folder/file 标记取实态；分页/排序沿 storage 列表既有口径。
- **136.2 解析顺序一致性**：聚合浏览与聚合解析（pull 路径）同源——成员顺序优先语义维持（FR-21 既有），浏览不引入第二解析通道（一处实现两消费）。
- **136.3 FE 消费**：`tree-empty-virtual` 成员感知空态**退役/翻转**（有成员内容时不再空态——锚册留痕，断言反转登记 §5.4）；RepoBranch virtual 静态化恢复动态展开（T-406 as-built 的受限面解除）。
- **136.4 边界**：virtual 无成员/全空成员 → 空态维持（文案区分「无成员」/「成员皆空」）；ACL——virtual 可见性沿内容面 allow() 既有（成员级越权行不泄漏——与 pull 解析同门）。

验收标准（AC）：

- **AC1（并集）**：双成员夹具（M3 FR-21 既有）→ `curl -u $ADMIN "$BASE/api/storage/maven-virtual/com/acme"` → 200 children 含两成员 artifactId 并集（逐名断言 + 同名合并臂 + folder/file 标记）；深层路径递归臂。
- **AC2（形态对照）**：t226 virtual 浏览形态对照（children 排序/标记/分页形态——aql.md 外的 storage 面核验腿，中置信项「以核验为准」纪律适用）。
- **AC3（FE + 回归）**：Playwright——virtual 树展开实态（空态锚退役翻转留痕）；M3 FR-21 解析序列 + M4 浏览 spec 零回归；四闸门 + axe + 服务端 diff 复核（FE 面）。

### 4.6 remote 仓远端浏览评估票（P2——不设实现断言）

#### FR-137 per-协议上游枚举能力矩阵 + 分期建议 + Q4 材料（reverse-engineer + dev-registry-adapter 会签）

**用户故事**：作为用户，remote 仓能否像 Artifactory 那样浏览**远端**目录（不限于已缓存）——需要先知道每个协议的上游有没有目录枚举能力、成本多大；本票出材料不做实现。

行为规格：

- **137.1 能力矩阵**：13 包型逐型——上游有无目录/包列表枚举 API（如 helm index.yaml / docker tags/list / maven-metadata / npm packument / conan search / pypi simple 上游 / nuget v3 catalog 等）、翻页形态、全量成本、增量探测可行性、Artifactory 远端浏览的对应行为（官方文档 + t226 活体——remote 浏览 UI 形态）。
- **137.2 分期建议 + Q4 材料**：三出口评估（全做 / 子集〔仅枚举 API 便宜的包型〕/ 维持缓存浏览——T-406 as-built）+ 实现量级估算 + 风险（上游压力/限流/降级语义）；**PM 出材料不代拍**（Q4 终裁）。

验收标准（AC）：

- **AC1（矩阵交付）**：能力矩阵落盘（docs/reverse/ 增量或独立 mini 规格——reverse-engineer 定）+ 包型逐行置信度标注；tech-lead 就绪度意见。
- **AC2（材料归位）**：Q4 材料包（三出口利弊 + PM 倾向）上 BOARD——终裁后实现段归 M16+ 或条件票。

### 4.7 复制包 B 首批（T-402a ②段兑现，P1/P2）

#### FR-138 Replicate Now + Test 连接 + blockPush/blockPull 全局封锁（internal/replication + httpapi + web/src；cron 双轨 Q5 不裁不建）

**用户故事**：
- 作为复制运维，配置完成不用等事件——Replicate Now 立即把仓全量推过去；上游维护窗口一键封锁推/拉；配置期 Test 就知道上游地址与凭据对不对。

行为规格：

- **138.1 Replicate Now（P1）**：`POST` executereplicationnow 对位端点（精确 wire 归 replication.md 增量段双源定案——官方 REST 文档 + t226 不可达〔OSS entitlement 锁〕文档源为主）；语义 = 对指定仓的 push 复制配置**立即触发全量同步任务**（任务载体消费 ADR-0041 outbox/队列模式参照——非引擎重构）；幂等（在途任务重复触发 → 200/409 语义照锚）；任务状态可查（复用既有任务/投递观测面）；FE 行级 Run 动作已有位（T-404 列表 Replications 列 ▶ 图标——本票接线真语义）。
- **138.2 Test 连接（P2）**：配置表单 Test 动作——上游可达性 + 认证探测（一次性 HEAD/GET 探测 + 结果内联呈现）；端点形态照锚（官方 validate/test 面若有则对位，无则 `/api/v1` 自有 C 层登记——规格票定）。
- **138.3 blockPush/blockPull 全局封锁（P2）**：全局双开关（Artifactory 实测语义——T-402a ②段：**UI-API 不受门**，仅引擎通道受门）：blockPush = 新复制事件不入队 + 在途推任务停发；blockPull = 拉侧（remote 回源/智能拉取视域）封锁；配置载体沿 binflow.yaml 全局段 + REST + 控制台开关；审计事件落 audit 词表（replication.config.* 族补词）。
- **138.4 cron 双轨（Q5）**：**不裁不建**——本票零 cron 工作；Q5 终裁若引入归 M16 复制域二程。

验收标准（AC）：

- **AC1（Replicate Now）**：双实例编排（自指上游——M6 复制夹具）→ 触发后目标仓制品数/校验和与源一致（全量同步实证）；在途重复触发幂等臂；FE ▶ 动作 → 任务可见（状态翻转）；启停态（enabled:false）触发 → 拒绝语义照锚。
- **AC2（Test）**：正确凭据 → 成功；错误凭据/不可达 → 内联失败原因；探测零副作用（上游只读接触）。
- **AC3（封锁）**：blockPush=on → push 事件不入队（源仓再部署，目标零到达）+ REST 配置通道仍可用（UI-API 不受门）；blockPull=on → remote 回源拉取拒绝/降级照既有 remote 语义；双开关审计行在场；FE 开关 + 状态呈现（parity R 系锚定形态）。
- **AC4（回归）**：M14 T-404/T-405 replication 面零回归（PUT enabled / 列表投影）；webhook outbox 引擎零改动（复用非重构断言——引擎文件 diff 审计）。

### 4.8 契约漂移与后端硬化小包（P1/P2）

#### FR-139 mint 500→400 修正 + remote 缓存树 busy 重试预算（internal/httpapi auth 域 + internal/storage/remote 缓存路径）

**用户故事**：`POST /api/security/token` 带不存在 username 应答 400「username is required or unknown」（auth-model 3.1）——as-built 500 是漂移不是契约；24 路并发拉缓存树不再出现 0.27% 的 SQLITE_BUSY 5xx 边角。

行为规格：

- **139.1 mint 修正（P1——T-386 登记契约漂移）**：`Tokens.Issue` subject 查找错误收编进 handler `ErrInvalidCredentials` 分支（或等效）→ unknown username 答 **400**（auth-model 3.1 逐字文案）；既有 mint 全场景零回归（正确凭据/错误密码/step-up 链）。
- **139.2 busy 重试预算（P2——T-377 D1，M14 滚程本程改判收编）**：remote 缓存树高并发写路径（fetcher 落盘 + node 行写）加 busy 重试预算（busy_timeout 调参与/或应用层有限重试——姿势归实现票 + architect 会签）；**SQLite 写路径专项回归**（既有写面全量：上传/GC/回收站/webhook outbox/复制入队零回归）；门内口径（8 路）维持零 5xx 不倒退。

验收标准（AC）：

- **AC1（mint）**：`curl -u $ADMIN -X POST "$BASE/api/security/token" -d 'username=ghost&...'` → 400（逐字文案断言）非 500；T-386 FE 面（错误内联呈现）行为复核——服务端修正后 FE 语义不劣化。
- **AC2（busy）**：T-377 D1 复现脚本（24 路并发缓存树重放）→ **零 5xx**（0.27% 边角清零）；8 路口径维持；写路径专项回归全绿（`make test` race 全树）。

### 4.9 FE 自有增强与工程债文面小包（P2）

#### FR-140 L2 行内快捷（复制 key / Set Me Up）+ 测试基建纪律成文 + 文面回写簇（web/src + web/e2e + docs）

**用户故事**：仓库列表行内一键复制 key、直开 Set Me Up——非 parity 面的自有增强（T-381 §V4 建议）；贡献者有 e2e 纪律成文可依；四处陈旧文面清账。

行为规格：

- **140.1 L2 行内快捷（FE 小票）**：仓库列表行加「复制 key」与「Set Me Up」快捷动作（**非 parity 面**——LC-79 C 层登记；E1 家族纪律不破：不引入删除进行内）；a11y（aria-label + 键盘可达）。
- **140.2 测试基建纪律成文**：`web/e2e` README/CONTRIBUTING 补三节——破坏性动作默认禁点 + 共享 fixture 快照前置（INC-1 教训成文——T-381 L06）；pkill 按端口精确杀纪律（T-382/T-384/T-389 三起误伤实例）；assert-tokens 属性选择器豁免规则（T-390）；顺带 m9 N01 flake spec 级竞态一行 + m9 seed 并行互撞注记（CI workers=2——T-391 L-b）。
- **140.3 文面回写簇**：`docs/reverse/README.md` 补 npm.md 行（T-393 登记）；parity 册 M1 行「定案 440px」升级 + M3 行 MUI Paper 代差描述（归 ux-designer）；package-icons helm/nuget 暗底提亮超 +10% 量级拍板（ux）；migrate-artifactory.md 措辞陈旧清账（tech-writer——T-397 登记）。

验收标准（AC）：

- **AC1（L2 快捷）**：Playwright——行内复制 key（剪贴板/回显断言）+ Set Me Up 直开（抽屉复用零重复实现）；行尾无删除动作断言（E1 不倒退）；四闸门 + axe。
- **AC2（纪律成文）**：三节成文落盘 + 两条 flake 注记；既有 e2e 全量绿（纪律票零行为改动——diff 审计）。
- **AC3（文面）**：四处回写落笔 + `make docs` 零断链；parity 册/README 修订留痕。

---

## 5. 兼容性矩阵（M15——AQL/老搜索 wire 契约 + 浏览/复制行为面）

### 5.1 层级定义（沿 M13/M14 四层）

| 层级 | 定义 |
|---|---|
| **A 兼容** | 端点/语义/envelope/错误码对齐 Artifactory（官方文档基准——子集以「A（子集注记）」标示，未覆盖面显式 400 拒绝） |
| **C 自有** | BinFlow 自有设计（无 Artifactory 对应或有意自有——资源门简化版/自有增强） |
| **D 有意不兼容 / 不做** | 显式裁决不做（clean-room / Non-goal / 防线），矩阵留痕防再议 |
| **待裁** | 存在可观测语义决策，须用户/conductor 终裁——终裁后归 A/C/D 并回写 |

**常设纪律（M15 特化）**：① **子集诚实边界**——A（子集）条目的未覆盖面必须显式 400（domain/field not supported），零伪空集（§1.4 条款 2）；② **ACL 零泄漏**——全部 A 层查询条目共享同一 allow() 源，越权零出现探针为硬 AC（§1.4 条款 4）；③ E1~E7 豁免族（M14 常设条款）对 FE 面继续生效（L2 快捷不破 E1）。

### 5.2 档位 × addon 解锁矩阵（M15 增量 0 行——19 槽维持）

M15 不新增 addon 槽：**搜索域（AQL + 老搜索）按 Artifactory AddonType `oss` 档映射为 BinFlow 核心能力**（community 全可用——inv-2 §3 枚举依据；与 docker remote 走既有 docker 槽同构，搜索无槽）；复制包 B 消费既有 replication 域（M6 FR-57~60 / M11 FR-101 域面——非 addon 槽，规格票内核对档位归属如有出入回写）；virtual 聚合/浏览为既有 storage 域增量（无槽）。三态叠加规则、`addons.disabled` 熔断语义沿 M10 §5.2 不变。**注意**：Artifactory「Smart Searches」（保存搜索）系 pro 档——BinFlow 保存搜索面若远期立项须按槽裁决，本程不含。

### 5.3 契约矩阵（12 条终版，LC-68~LC-79 续接 M14 编号——v1.1：LC-76 归 A，as-built 状态逐行回填）

| # | 契约面 | Artifactory 对应 / 公开规范 | 层级 | 优先级 | 置信度（v1.1 as-built） | 验收 |
|---|---|---|---|---|---|---|
| LC-68 | `POST /api/search/aql`（+`?compact`）端点 wire——text/plain 请求体 + `?query` 回退 + envelope/错误码族 | 官方 OpenAPI（唯一基准）+ inv-1 §E 端点锚 + t226 活体 36 探针（T-407） | A | P0 | 高——**已落地 T-415**（pretty/compact 双形态逐字节 + E-01/401/403/408/429 映射 + metrics 三组）；compact 非空行体中置信「规格待验证」留痕（活体实 415——aql.md §12 V 项） | L21 |
| LC-69 | AQL 语言子集——item+property 域 / **v1.1 校准：$and·$or·$msp（$not 不存在）** / 8 比较符 + $last·$before / 链序敏感尾缀链 / 6,000 门；**未支持域 → 400 诚实拒绝**（C 层增强文案，码位一致） | 官方 AQL 文档全集的 M15 子集 + aql.md 五定案；statistics/usage M16、六域远期 dep（§5.7） | **A（子集注记）** | P0 | 高——**已落地 T-409/T-411**（17+2 字段闭集注册表 + 12 未支持域提示；参数化 SQL 零拼接——注入红线 7 形 × 8 恶意值；`idx_node_props_name` 经 EXPLAIN 断言兑现） | L21/L22 |
| LC-70 | 老搜索首批——`GET /api/search/gavc / prop / pattern`（参数族〔prop 任意查询参数即属性键〕+ envelope + 200 空数组族 + 400 语义） | inv-2 §1.C（**14 计数定案**——T-407 勘误回写）+ 官方 Search 文档 + aql.md §8（pattern 对齐源 = 官方 Pro 文档语义，非 OSS 400 门行为） | A | P0 | 高——**已落地 T-417**（mvn 真坐标腿 / M10 属性腿 / 通配树；K63 截断同门；三端点 uri 瘦行族 vs BinFlow FileInfo 超集差异留痕不追改） | L24 |
| LC-71 | 既有 `artifact/checksum` 搜索语义维持 + name 匹配语义校准（**K64 落笔：子串维持 + 大小写不敏感翻转 + `*` 字面**） | T-92 as-built + aql.md §0-5（官方措辞 + 活体 + 反编译三源） | A | P0 | 高——**已落 T-417**（K64 两臂断言；T-92 全量 spec 零改动通过 = 零回归证据；M4 §5.5 K2 欠账清偿） | L24 |
| LC-72 | 搜索域 ACL——**两段织入**（SearchScope 集合谓词 + path-scoped 仓行级 CanRead 复核，同一 allow() 源，越权仓零出现） | aql.md §6（Artifactory 结果流侧行级过滤等价语义——BinFlow 走 SQL 谓词 + 行复核）；ADR-0043 pt4/5 | A | P0 | 高——**已落地 T-413**（auth.Authorizer include/exclude 模式**拒绝 SQL LIKE 翻译**——双真相源即泄漏，ADR 裁两段缝；virtual 展开≠权限通道） | L23 |
| LC-73 | 资源治理门 K63 定案——上限 1,000 截断（range.notification 官方逐字）/ 并发 4 → 429 + Retry-After / 超时 10s → **408** / 6,000 门；内部常量零配置键 | aql.md §5 校准表（码位/文案 A 对齐：429 `too many requests`、408、6,000、截断通告；值域 C 层——Artifactory self-managed 无默认上限 / 并发默认 3 / REST 超时 900s，不硬仿）+ inv-1 §E QRL 反编译锚；QRL 全量三态/REST/指标 M16+ | **C（简化自有——AQL 全量对齐面 M16）** | P0 | 中高——**已落地 T-413/T-415**（408/429/截断 wire 全链；V-a 429 活体触发系语料限制以核验为准——T-426 留痕；真门并发饱和不可确定性 stub 同口径） | L23 |
| LC-74 | 控制台搜索面——AQL 模式编辑器 + 结果表 + 列选器三页推广（users/groups/search）+ member-pop hover | 消费既有/本程端点**零新端点**；列选器形态沿 LC-61；AQL 编辑器 BinFlow 自有（Artifactory Smart Searches 系 pro 档保存搜索——不对齐） | A（列选器腿）/ C 注（AQL 编辑器腿） | P1 | 高——**已落地 T-414/T-419**（锚册 v1.27/v1.29；search 既有锚零改名——AQL 行复用；SPA +2,183B / +5,053B 预算内；服务端 diff=0） | L25 |
| LC-75 | virtual 聚合浏览——`GET /api/storage/<virtual>/<path>` children 成员并集（FR-21-AC8 逐字）+ 同名路径**首成员胜** + 解析顺序同源（virtualMemberOrder） | milestone-3 FR-21-AC8 + aql.md §7-2（结果行 repo = 实际成员仓 key 同姿态）；t226 对照腿**降级**（实例无 virtual 仓——官方 + 反编译双源定案，BinFlow e2e 对拍替代） | A | P1 | 高——**已落地 T-412/T-416**（断言反转②；remote 成员仅缓存行〔T-406 listing 口径〕——**Q4 开档后 M16 扩面牵连点**；display-only marker 零落库〔ADR-0013〕） | L26 |
| LC-76 | remote 远端浏览——**Q4 终裁（conductor 2026-09-02 04:2x）：出口 C 批 1 = helm + deb + rpm 可选档语义**（`listRemoteFolderItems` 对位，**默认 false = T-406 as-built 同形态，不欠默认 parity**） | Artifactory 官方设置面五型（Debian/Generic/Maven/Opkg/RPM）+《Browse Remote Repositories》专节（取决于上游支持——Maven Central 正例 / Docker Hub 反例）+ T-425 13 型能力矩阵；t226 活体腿结构性降级（OSS REST 建仓面 Pro 门 400——零残留留痕） | **A（可选档语义——v1.1 归位，离开「待裁」）** | P2（评估→实现段 M16） | 高（官方文档直读 + 矩阵双源）——**评估已落 T-425**；实现段 ~3 票 M16 登记、批 2（docker/helmoci tags 层 + maven metadata 层）条件、HTML 抓取族不做（§2.2） | L27 |
| LC-77 | 复制包 B——Replicate Now（`POST /api/v1/replications/{id}/run`）/ blockPush·blockPull 全局封锁（UI-API 不受门）/ Test 连接（零副作用、不看封锁态） | T-418 replication.md §9 增量段（**三源**：官方 OpenAPI 主源 + reverse-src Pro 实现 + T-402a 实测）；cron 双轨 Q5 终裁不引入 | A | P1/P2 | 高——**已落地 T-418/T-420/T-422**（双实例 sha256 逐路径一致 + 幂等收敛 + enabled:false → 409 + 封锁三面一致 + audit 词批次 + outbox 引擎 diff=0；V1~V4 Pro 抓包升格项不阻断） | L28 |
| LC-78 | mint unknown username 400——auth-model 3.1 既有语义归位（as-built 500 漂移修正，T-386 登记） | docs/reverse/auth-model.md 3.1（既有规格——非新增对齐面） | A | P1 | 高——**已落地 T-410**（断言反转③；400 逐字 + 非 admin 403 守卫防枚举次序锚 + t386 spec e2e 翻转） | L29 |
| LC-79 | L2 行内快捷（复制 key / Set Me Up 直开）——非 parity 面自有增强 | T-381 §V4 建议（Artifactory 行尾无 ⋮——LC-60 D 层维持不破）；E1 不倒退 | **C（自有增强）** | P2 | —（自有设计）——**在途 T-424**（B8 后 FE lane 空位） | L31 |

> 计数（v1.1 终版）：**12 条 = A 10（LC-68/69〔子集注记〕/70/71/72/74〔列选器腿；AQL 编辑器腿为 C 注〕/75/**76〔Q4 终裁归位**〕/77/78）+ C 2（LC-73/79）+ 待裁 0**——v1.0 待裁 1（LC-76）已清，零滞留（LC-56 教训防范）。既有契约面（五基础包型、九 addon 包型、配置域、操作族/回收站、webhook、helmoci/docker remote、replication M14 as-built）M15 对 M14 as-built 零行为变化（§5.4——断言反转三处 + K64 局部翻转均归属 M15 豁免票）；FE 票「服务端 diff=0」复核沿 M8 先例已过（T-414/T-416/T-419，T-424 归终验）。

### 5.4 回归基线（M15 断言反转三处设计——全部归属 M15 豁免票；其余零反转。v1.1：三处 + K64 局部翻转 as-built 回写核对）

| 既有断言 | M15 期望 | **as-built（v1.1 核对）** |
|---|---|---|
| M1~M14 全部 P0 序列（双形态：无 license 默认 + pro） | 零回归（AQL/老搜索只增不改既有面；virtual 聚合改变 List 行为面——M3 FR-21 序列与 M4 浏览 spec 专项复跑） | BE 面票内回归绿（T-412 repo 全包 race 454.8s / T-415 httpapi 427.4s 等）；**全量双形态复跑归 T-421/T-430**（T-421 押后至审计 workflow 完结——串行净机纪律）；**全树 race 判无效教训在册**（共租负载签名，D-413-2 登记归 T-421） |
| t92_search_test.go SR-03/SR-04（gavc/props/pattern 等未实现族 → 404） | **断言反转①**：gavc/prop/pattern 三行 404 → 分派实现（FR-134 豁免票归属；其余未实现族 404 维持——§5.7 归属为准） | **已落 T-417**（三行反转 + 其余未实现族 404 维持——§5.7 对账一致） |
| `tree-empty-virtual` 空态锚（T-406 as-built——virtual 成员感知空态） | **断言反转②**：有成员内容时翻转为聚合实态（FR-136 豁免票归属——锚册退役/翻转留痕；无成员/全空维持空态） | **已落 T-412（BE）+ T-416（FE）**——锚**不退役**（有内容并集表格 / 无成员空态两态文案），锚册 v1.28 留痕；M3 FR-21/M4 浏览序列零回归（票内 7 spec） |
| mint unknown username → 500（T-386 as-built 漂移登记） | **断言反转③**：→ 400（auth-model 3.1 归位——FR-139 豁免票归属） | **已落 T-410**（400 逐字 + t386 spec e2e 翻转 + armed 全链） |
| `/api/search/artifact` name 匹配语义（LIKE 子串——M4 K2 暂行） | K64 校准结论执行：维持 → 零变化；翻转 → 断言更新归属 FR-134 豁免票（aql.md 回写留痕） | **局部翻转已落 T-417**：大小写不敏感对齐（区分大小写系漂移——断言更新豁免票执行）；子串本体 + `*` 字面维持（aql.md §0-5 三源——M4 K2 欠账清偿） |
| `make test`（race）/ footprint ≤100MB / check-size ≤120MB（m14-done 调基门）/ 冷启动 <2s / SPA 每票 ≤10KB | 维持（AQL 引擎纯 Go 增量；F1 六平台聚合趋势观察项登记 §6.2） | 票内 race 全绿（TEST_TIMEOUT=20m 口径——T-420 勘误在册）；SPA 逐票 +470B~+5,053B 全过；F1 趋势终登记归 T-429 |
| 四闸门 + axe 双主题 serious=0 / anchor ledger 0 断链 | 维持全绿（新 FE 面走新锚入册流程） | **已落**（T-414/T-416/T-419 四门 + a11y 双主题 0 + 锚册 v1.27~v1.29 递增——T-424 归终验） |
| M14 replication 面三票 spec（T-404/T-405）+ webhook outbox 引擎 | 零回归（包 B 复用非重构——引擎文件 diff 审计；PUT enabled/列表投影不变化） | **已落 T-420/T-422**（outbox 引擎文件 diff=0 审计；409/enabled 语义 + 列表投影不变化；spec 终验归 T-421/T-430） |

### 5.5 M15 核心验收命令（L 序列骨架，QA 直接引用——续接 M14 L19 起）

```bash
BASE=http://127.0.0.1:8080; ADMIN=admin:password
# ========== FR-132 规格前置锚 ==========
# L20 aql.md 交付：官方文档锚点逐条 + inv 补白 + t226 活体核验结论（语言边角/错误文案逐字/virtual 语义）+
#    口径归一（14-vs-13 勘误回写主矩阵 + 子集边界表 + K64 校准值 + 基座映射表交 ADR-0043）+
#    tech-lead 就绪度确认（可拆/裁决点/缺项是否阻塞）；核验源不可得项「以核验为准」零静默升格

# ========== FR-133 AQL 引擎 ==========
# L21 语言全链：curl -u $ADMIN -X POST "$BASE/api/search/aql" -H 'Content-Type: text/plain' -d \
#    'items.find({"repo":{"$match":"maven-local*"},"type":"file","$or":[{"size":{"$gt":104857600}},
#    {"created":{"$lt":"2025-01-01T00:00:00Z"}}]}).include("repo","path","name","size","created")
#    .sort({"$desc":["created"]}).offset(0).limit(10)' → 200（排序/分页/投影逐字段断言）+
#    property 嵌套匹配（M10 属性夹具腿）+ ?compact 变体双形态 + 语法错/非法字段 → 400 E-01 +
#    未支持域（build.find/statistics 字段）→ 400 domain-not-supported（零伪空集）
# L22 执行内核：参数化 SQL 断言（注入样本查询零拼接逃逸——表驱动注入用例）+ planner 单测覆盖（AST→SQL 快照）
# L23 ACL + 资源门：双用户夹具——受限用户全域/逐仓 AQL 越权仓零出现（t92 探针形态+AQL 腿）+
#    匿名/未认证门维持 + 超上限截断标记+offset/limit 分页可达全量 + 并发 5 路 ≥1 路 429+Retry-After +
#    慢查询超时形态 + 满载零 5xx 零 OOM

# ========== FR-134 老搜索首批 ==========
# L24 三端点 + 断言反转①：mvn deploy 真实数据 → curl "$BASE/api/search/gavc?g=com.acme&a=demo" 命中
#    （v/c/repos 臂 + 坐标非法 400）+ curl "$BASE/api/search/prop?props=license=Apache-2.0"（M10 夹具腿 +
#    键无值臂 + 未知键空集 200）+ curl "$BASE/api/search/pattern?pattern=maven-local:com/acme/**/*.jar"
#    （跨仓臂 + 非法 400）+ t92 SR-03/SR-04 三行断言反转（404→分派）核实 + artifact/checksum 全量零回归
#    （K64 落笔臂）

# ========== FR-135 搜索面 FE ==========
# L25 Playwright——AQL 模式（切换/结果渲染/语法错内联/分页排序）+ 列选器三页（reload 持久/互不染）
#    + member-pop hover ≥4.5:1 + 四闸门 + axe + 服务端 diff=0 + SPA ≤10KB/票

# ========== FR-136 virtual 聚合 ==========
# L26 curl -u $ADMIN "$BASE/api/storage/maven-virtual/com/acme" → 200 children 两成员并集（逐名断言 +
#    同名合并臂 + folder/file 标记 + 深层递归臂）+ t226 形态对照（排序/分页）+ Playwright 树展开实态
#    （tree-empty-virtual 翻转②留痕）+ M3 FR-21/M4 浏览序列零回归

# ========== FR-137 远端浏览评估 ==========
# L27 能力矩阵交付（13 包型逐行：上游枚举 API/翻页/成本/Artifactory 对应行为 + 置信度）+
#    Q4 三出口材料包上 BOARD（不设实现断言）

# ========== FR-138 复制包 B ==========
# L28 双实例编排——Replicate Now 触发全量同步（目标仓校验和与源一致）+ 在途幂等 + FE ▶ 动作接线 +
#    enabled:false 拒绝臂 + Test 连接（正确/错误凭据/不可达三臂 + 零副作用）+
#    blockPush=on 新事件不入队（源再部署目标零到达）+ REST 通道不受门 + blockPull 拉侧封锁 +
#    审计行在场 + T-404/T-405 spec 零回归 + outbox 引擎文件 diff=0

# ========== FR-139 契约与硬化 ==========
# L29 mint：curl -u $ADMIN -X POST "$BASE/api/security/token" -d 'username=ghost&expires_in=300' →
#    400 逐字文案（非 500）+ 既有 mint 全场景回归（正确/错误密码/step-up）
# L30 busy：T-377 D1 复现脚本 24 路并发缓存树重放 → 零 5xx + 8 路口径维持 + SQLite 写路径专项回归
#    （上传/GC/回收站/webhook outbox/复制入队）+ make test race 全树

# ========== FR-140 债包 ==========
# L31 L2 快捷：Playwright——复制 key + Set Me Up 直开（抽屉复用）+ 行尾无删除（E1 不倒退）+ a11y；
#    三节纪律成文（INC-1/pkill/assert-tokens）+ 两条 flake 注记 + 四处文面回写 + make docs 零断链

# ========== 回归与收口 ==========
# L32 全量回归：M1~M14 全 P0 双形态复跑 + 断言反转三处归属审计（SR-03/04 + tree-empty-virtual + mint）
#    + FE 变更面归属 M15 票 100% + FE 票服务端 diff=0
# L33 NFR：万节点仓 item 域 AQL P95 ≤500ms / property join P95 ≤800ms（本机 SSD+WAL）+
#    8 路 AQL 混合并发零 5xx + 用户实例数据副本冒烟（top-level 查询 <2s）+ 资源门三连
#    （footprint ≤100MB / check-size ≤120MB / 冷启动 <2s）+ F1 趋势观察登记
# L34 文档：tech-writer（AQL 用户指南 + 搜索 API 参考〔AQL+老搜索〕+ 搜索页/虚拟浏览/复制包 B 增量 +
#    FAQ〔AQL 子集边界与迁移对照〕）客户端命令实测可复跑
```

### 5.6 待校准项（K62~K66——v1.1 全量回填实装值；「v1.0 暂行值」列为历史对照保留；效力序 §1.4 条款 3 就此闭环：aql.md / ADR-0043〔含勘误〕> 本 PRD）

| # | 项 | v1.0 暂行值 | **v1.1 归位值（实装 + 依据锚）** |
|---|---|---|---|
| K62 | AQL 语言子集边界（域/字段/操作符 + 未支持域文案） | item+property 域 + 官方操作符主列；未支持域 400「domain not supported」族 | **aql.md 定案 + T-409/T-411 实装**：item+property 域（property 嵌套 `@key`/`@*`/`property.key` 展开 + `$msp`）；操作符 = 官方 8 比较符 + `$last/$before`（**`$not`/`$contains` 不存在**——§0-1/§2.4）；尾缀链序敏感（include→sort→offset→limit→distinct——E1 逐字 + 链序 parse error）；`.sort()` 按官方全集 **A 层实现**（BinFlow 无许可门——OSS 门行为不仿，§0-2）；6,000 门在 Parse 入口（字节 + rune 双条件——T-433 排队注记）；字段闭集 17+2 注册表 + 12 未支持域提示（400 **C 层增强文案**——Artifactory 系通用 parse error 无域文案，码位一致） |
| K63 | 资源治理门参数（上限/并发/超时/429 形态） | 上限 1,000 行（截断+分页可达全量）/ 并发 4 / 超时 10s | **定案 = 暂行值全中 + 408 码位**（ADR-0043 + 勘误 + T-413/T-415 实装）：1,000 截断（range.notification 官方逐字）/ 并发 4 → 429 + Retry-After（**内部常量零配置键**）/ 超时 10s → **408**（aql.md E7 官方错误码表——v1.0「400/503 待定」关闭）/ 6,000 门；值域 vs Artifactory（self-managed 无默认上限 / 并发默认 3 / REST 超时 900s）冲突**C 层留痕不硬仿**——Q2 归位（§7） |
| K64 | `/api/search/artifact` name 匹配语义（M4 §5.5 K2 欠账） | 维持 LIKE 子串 + `*` 通配不做 | **aql.md §0-5 三源定案 + T-417 落笔**：子串本体**维持**（无族级断言反转）+ **大小写不敏感翻转**（原区分大小写系漂移——LOWER LIKE 折叠转义，ASCII 折叠局限登记）+ `*` **字面维持**（官方/反编译一致）；断言更新已归 T-417 豁免票执行——**M4 起欠账清偿** |
| K65 | 老搜索首批清单与 dates/creation 顺车判定 | gavc/prop/pattern 三枚；dates/creation 默认 M16（判 trivial 可顺车 P2） | **定案**：首批三枚已落（T-417——断言反转①）；**端点计数 14 定案**（SearchResource 铁证——inv-1/主矩阵已勘误回写，全量口径 = 官方 reference 16 枚外挂 + AQL）；**dates/creation 不顺车 → M16**（T-417 票内判定：数据腿 trivial 但 wire 腿非同构——404 空集族 + uri 瘦行 + epoch-ms）；usage dep stats 维持 M16 |
| K66 | remote 远端浏览 per-协议能力矩阵 | 评估票产出（PM 暂行倾向：仅上游有枚举 API 的包型子集化） | **T-425 交付 + Q4 终裁归位**：13 型矩阵三族分层（一文档全树族 helm S / deb·rpm M；per-实体族 docker·helmoci·maven·pypi·npm·conan·goproxy·cargo；无标准族 generic·maven 目录层）+ 官方设置面五型勘正（Debian/Generic/Maven/Opkg/RPM）——**PM 暂行倾向中 docker 腿系超 parity 错位（Artifactory 未开放该型），被终裁修正**；**出口 C 批 1 = helm+deb+rpm**（可选档 `listRemoteFolderItems` 语义，默认维持缓存浏览）；批 2 条件 + 不做面登记 ROADMAP「M15 未纳入项」；LC-76 归 A |

### 5.7 搜索域端点全景归属表（本 PRD 特有——逐条不留模糊地带；v1.1 as-built 对账列回填 + 两外挂端点补登〔aql.md §8.1 交 PM 重审项——已重审：远期登记〕）

| 端点 | v1.0 现状 → 处置 | **as-built（v1.1 对账）** | 归属层 |
|---|---|---|---|
| `POST /api/search/aql`（+`?compact`） | 404 → **FR-133 实现**（item+property 子集 + 资源门） | **已实现（T-409/T-411/T-413/T-415）**——语言前端/内核/门/端点四环全落；`?query` 回退 + 6,000 门 + metrics 三组；compact 行体中置信留痕 | A（子集）/ LC-68/69 |
| `GET /api/search/artifact` | 已有（T-92）→ 维持 + K64 落笔 | **已有 + K64 局部翻转（T-417）**——大小写不敏感对齐、`*` 字面；T-92 全量 spec 零回归 | A / LC-71 |
| `GET /api/search/checksum` | 已有（T-92）→ 维持 | **已有维持**（T-417 零改动通过） | A / LC-71 |
| `GET /api/search/gavc` | 404（SR-03）→ **FR-134 实现**（断言反转①） | **已实现（T-417）**——mvn 真坐标腿；v/c/repos 臂 | A / LC-70 |
| `GET /api/search/prop` | 404（SR-04）→ **FR-134 实现**（断言反转①） | **已实现（T-417）**——任意查询参数即属性键形态（aql.md §8.2 勘正后口径） | A / LC-70 |
| `GET /api/search/pattern` | 404（SR-04）→ **FR-134 实现**（断言反转①） | **已实现（T-417）**——通配树 + 跨仓臂；对齐源 = 官方 Pro 文档语义 | A / LC-70 |
| `GET /api/search/dates` / `creation` | 404 → 余量条件（K65） | **404 维持——K65 判定不顺车（T-417）**：404 空集族 + 瘦行 + epoch-ms 非同构 → **M16** | M16 |
| `GET /api/search/usage`（usageSince） | 404 → M16+（dep stats） | **404 维持**——dep per-node 下载计数基建（**aql.md 注：stats 字段 t226 OSS 活体可用——排 M16 系自有基建缺失，非 parity 档位**） | M16 |
| `GET /api/search/badChecksum` | 404 → M16+ | 404 维持（dep 校验扫描遍历） | M16+ |
| `GET /api/search/versions` / `latestVersion` | 404 → M16+ | 404 维持（dep 包型坐标语义细化） | M16+ |
| `GET /api/search/license` | 404 → 远期 | 404 维持（dep 制品 license 识别域——intake ⑤ 翻案语境：候选池续滚重标） | 远期 dep |
| `GET /api/search/dependency` / `buildArtifacts` | 404 → 远期 | 404 维持（dep Build-info 域——同上重标） | 远期 dep |
| （外挂）`GET /api/search/archive` | ——（v1.0 未列） | **v1.1 补登**（aql.md §8.1：SearchResource 外 REST reference 文档化端点，官方标注 deprecated；**PM 重审结论：远期登记不实现**）——404 维持 | 远期 dep |
| （外挂）`GET /api/search/latestVersionByProperties` | ——（v1.0 未列） | **v1.1 补登**（同上——反编译 RestAddon 方法面；PM 重审结论同：远期登记）——404 维持 | 远期 dep |
| UI 族 `artifactsearch`/`stashResults`/`packagesSearch`/`syntax-search` | 404 → M16 | 404 维持——搜索页 AQL 模式已落（T-419）承接控制台查询面；结果暂存/包级索引族 M16 | M16 |
| `v1/system/query_rate_limiter`（QRL REST） | 404 → M16 | 404 维持——M15 落 C 层资源门 LC-73（T-413 实装：1000/4/10s/429/408） | M16 |

> 本表 = 搜索域端点兼容承诺的唯一权威清单（高频子集承诺在查询域的落点）；**全量口径 = SearchResource 14 端点 + 官方 reference 2 外挂 + AQL**（aql.md §0-3 定案——本表 14+2+1 全数在册，零遗漏）；任何未列端点不承诺；「M16+/远期」行进任何里程碑前须 PM 重审（滚动候选池对账——M16 立项时与本表 + intake ⑤ 审计产出双源对账）。

---

## 6. 非功能需求（NFR）

### 6.1 与已有 ADR / 规范的冲突/补充标注

| ADR/规范 | 冲突/补充点 | 本 PRD 立场 | 所需动作 |
|---|---|---|---|
| ADR-0001（clean-room） | AQL 官方文档 vs 反编译取证位序；AqlOqlParser/SQL builder 零翻译 | 官方文档为唯一行为基准（webhook.md 先例）；inv 锚点补白；实现自有（Go） | §1.4 条款 1/5 |
| architecture §15.3.2（node_props 预留索引） | 属性搜索面的兑现时点 | 本程兑现（FR-133 property 域 + FR-134 prop 端点）；`idx_node_props_name` 消费核验入票 | FR-133 AC |
| architecture nodes 表（name/depth/sha1/md5 缺列） | AQL item 字段映射 | 派生/取数策略归 ADR-0043（不为 AQL 先改表——列扩展须 architect 裁） | ADR-0043 |
| httpapi 无 WriteTimeout（T-392 登记，归 architect） | AQL 长查询与写超时交互 | 随 ADR-0043 评审归位（超时治理统一在资源门 K63，不另开面） | ADR-0043 评审项 |
| ADR-0041（webhook outbox） | Replicate Now 任务载体 | 复用队列/outbox 模式参照，**引擎零重构**（diff 审计断言） | FR-138 AC4 |
| ADR-0032/0033（license 门控） | 搜索域档位归属 | oss 档映射 → 核心能力无槽（§5.2）；Smart Searches pro 档不对齐登记 | §5.2 |
| console-ux 锚册 v1.26 | tree-empty-virtual 退役/翻转 + 新 FE 面锚 | 断言反转②留痕 + 新锚入册（anchor-audit 0 断链） | FR-135/136 AC |
| M8/M14 FE 四闸门 + E1~E7 豁免族 | FE 票合入条件 | 全程维持（L2 快捷不破 E1——行尾无删除断言） | FR-135/140 AC |

### 6.2 性能与资源（M15 增量）

| NFR | 指标与验收方式 | 优先级 |
|---|---|---|
| NFR-P67 AQL 查询性能 | 万节点仓 item 域 P95 ≤500ms / property join P95 ≤800ms（本机 SSD + WAL，结构化语料夹具）；用户实例数据副本（260+ 仓）top-level 查询冒烟 <2s（报障先沙箱复现纪律——memory 口径） | P0 |
| NFR-P68 并发与满载 | 8 路 AQL 混合查询 + 写入负载（上传/GC 并行）零 5xx；busy 预算票后 24 路缓存树重放零 5xx（0.27% 边角清零——L30） | P0/P2 |
| NFR-P69 资源门维持 | footprint ≤100MB / check-size ≤120MB（m14-done 调基门）/ 冷启动 <2s 三连维持；SPA 每票增量 ≤10KB；**F1 观察项**：六平台聚合 103.37MB 趋势登记——M15 净增量须可忽略（AQL 纯 Go 无新资产），显著上浮即红旗 | P0 |

### 6.3 安全底线（M15 增量）

| NFR | 要求 | 验收 |
|---|---|---|
| NFR-S73 ACL 零泄漏 | 全部新查询面（AQL/老搜索/virtual 聚合）与内容面同一 allow() 源；越权仓行零出现——搜索域为泄漏面最宽处，探针逐票硬 AC | L23/L26 |
| NFR-S74 注入面 | AQL 文法 → 参数化 SQL 零字符串拼接（表驱动注入用例）；wildcard 转义核验 | L22 |
| NFR-S75 凭据与探测 | Test 连接零副作用（上游只读接触 + 凭据不落日志）；mint 400 修正后错误面不泄漏用户存在性（文案照 auth-model 3.1 逐字） | L28/L29 |

### 6.4 可观测性（M15 增量）

- AQL 进既有 metrics 口径：查询计数 / 时延 histogram / 429 与超时计数（remote family 之外的 search family 新组——Prometheus 既有三态口径延伸）；慢查询（>K63 阈值 1/2）单行日志。
- Replicate Now / blockPush·blockPull 落 audit 词表（replication.config.* 族补词——M14 登记 T-405 遗留同场清）。
- QRL 全量指标面（三态 + 指标 job）M16+，本程不建。

---

## 7. 开放问题（Q1~Q7——v1.1 逐项归位：终裁落章 3〔Q4/Q5/Q6〕· 规格回写归位 2〔Q2/Q3〕· 维持暂行 2〔Q1 收口窗必裁——材料已齐 / Q7 登记型〕；状态 + 依据锚；PM 出材料不代拍——终裁落章归 conductor）

| # | 问题 | 影响面 | **归位态（v1.1）——状态 + 依据锚** |
|---|---|---|---|
| Q1 | **AQL 分阶段边界终裁**（M15 核心 / M16 高级面切分是否成立；statistics/usage 域与 UI 搜索族是否 M16） | FR-132~134 范围锚；**M16 立项前提** | **【维持暂行——收口窗必裁，材料已齐】**M15 侧证据链闭合：语言前端（T-409）/ 执行内核（T-411——**P95 item 7.7/30.6/38.5ms〔预算 500ms〕、property join 37.5/90.7/107.1ms〔预算 800ms〕大幅余量**，scripts/m15-aql-perf.sh 可复跑）/ ACL+资源门（T-413）/ 端点（T-415）/ 老搜索三端点（T-417）/ FE AQL 模式（T-419）六环全落。M16 边界核对**维持 §1.1-5 建议**：statistics/usage 域〔dep per-node 下载计数基建——**aql.md 注：stats 字段 t226 OSS 活体可用〔v16〕，排 M16 系自有基建缺失而非 parity 档位**〕+ QRL 全量（三态 + REST + 指标 job）+ UI 搜索族 + 剩余老搜索（dates/creation〔T-417 判 M16〕/ badChecksum / versions / latestVersion / usage）。PM 建议：维持切分、M16 优先 statistics 域（用户价值最高）；**intake ⑤ 语境注：M16 主轴候选重排归 M16 立项稿与全量审计产出对账——AQL 高级面作为候选第一顺位维持登记（ROADMAP「M15 未纳入项」），不因翻案语境预先升降**。BOARD 留痕 = 各 done 条目。**conductor 落章动作：m15-done 收口窗终裁（DoD#5 硬项）** |
| Q2 | **资源治理门参数与 429/超时形态**（K63） | FR-133.4；LC-73 C 层定案 | **【规格回写归位】**K63 定案 = 上限 **1,000** 行（截断 + range.notification 官方逐字文案）/ 并发 **4** → 429 + Retry-After（**内部常量零配置键**）/ 超时 **10s → 408**（ADR-0043 勘误定案——503 暂行作废；aql.md E7 官方错误码表 408 非 503/504）/ 查询长度 **6,000**（A 层）。Artifactory 对照（aql.md §5 校准表）：self-managed **无默认上限**（SaaS 500k）/ 并发默认 3 / REST 超时 900s——值域冲突走 **C 层留痕不硬仿**（单机 SQLite 防护必要性更高），码位与文案（429 `too many requests`、408、6,000、截断通告）**A 层逐字对齐**。实装 T-413/T-415；V-a（429 活体触发）语料限制以核验为准（T-426 留痕）。BOARD 留痕 = T-408 + 三笔勘误 + T-413/T-415 done 条目 |
| Q3 | **未支持域错误形态**（400 诚实拒绝 vs 200 伪空集 vs 501） | FR-133.3；LC-69 | **【规格回写归位】**400 诚实拒绝**维持且升级定案**：Artifactory 对未支持/未知入口域与未知字段一律走**通用 parse error 400**（`Failed to parse query: ...`——活体 v14/v15 双证，**无专门 domain-not-supported 文案**，aql.md §2.1）；BinFlow 域/字段/操作符提示为 **C 层增强文案**（码位 400 一致、信息更明确——Artifactory 文案本身不携带 domain 名，不违反 parity）；零伪空集零静默吞宪章（§1.4 条款 2）如约兑现。实装 T-409（双轨文案：语法 E1 逐字 / 域·字段·操作符 C 层增强，均 400）。BOARD 留痕 = T-407 五定案 + T-409 done 条目 |
| Q4 | **remote 远端浏览去留**（三出口：全做/子集/维持缓存浏览——T-406 as-built） | FR-137 → LC-76 归位；M16+ 排期输入 | **【终裁归位——conductor 2026-09-02 04:2x】出口 C 批 1 = helm + deb + rpm**：可选档 `listRemoteFolderItems` 对位语义（**默认 false 维持缓存浏览 = T-406 as-built 同形态——parity 定性修正：不欠默认 parity，欠可选档**）；批 2（条件，批 1 验证用户真实使用后裁）= docker/helmoci tags 层（drill-down 定位，catalog 根不可达须向用户明示）+ maven metadata 版本层；**docker tags 腿不采**（Artifactory 官方设置面未开放该型——做即超 parity L2，T-425 如实标注）；**maven/generic HTML 抓取族不做**（官方未写算法，中置信无锚）；**LC-76 归 A（可选档语义）**；实现段 ~3 票 M16 登记（牵连 T-412 listVirtual remote 成员口径扩面〔repo-semantics §8.5〕；规格建议落 docs/reverse/remote-browsing.md——T-425 §1/§2 可直接成稿，归 conductor 编排）。**PM 暂行倾向（helm/docker 先行）被材料修正**——docker 腿超 parity 错位系 T-425 关键发现，终裁采纳 C2 序（协议廉价度 × 缝厚度），PM 复核认可。BOARD 留痕 = T-425 done 条目 + Q4 终裁行 |
| Q5 | **cron 双轨**（Artifactory 用户级 cron 复制 vs BinFlow 事件驱动 + 1min sweep） | FR-138.4；M16 复制域二程输入 | **【终裁归位——conductor 2026-09-01 09:1x】不引入 cron 双轨**：事件驱动 + 1min sweep 维持唯一引擎；手动全量场景 Replicate Now 承接——**T-420 已落地**（`POST /api/v1/replications/{id}/run`：双实例逐路径 sha256 一致 + 重复触发幂等收敛 + enabled:false → 409）；FR-138.4「不裁不建」兑现为「**裁不建**」；M16 复制域二程不再列 cron 为实现项。**intake ⑤ 注**：不做清单翻案语境下如重开 cron，须列与本裁定冲突点交用户确认（BOARD 在档裁定不默默翻转）。BOARD 留痕 = conductor 审定段 Q5 行 + T-420 done 条目 |
| Q6 | **docker virtual 建仓矩阵开禁**（T-397 登记 conductor 裁） | 建仓矩阵包型 × rclass 组合面 | **【终裁归位——conductor 2026-09-01 09:1x（即裁）】开禁**：对齐 Artifactory 组合完整性（M14 docker remote 首航 + M15 virtual 聚合语义既有 + helmoci virtual〔M13〕先例）；载体 = **T-431 条件票**（P2 波外插空，**非 DoD**；开禁面 = 建仓矩阵一行 + FE 门控 + docker 三态回归）。截至本笔（B13）未插空——若 m15-done 前未派则滚 M16 首票留痕（ROADMAP 条件票出口）。BOARD 留痕 = conductor 审定段 Q6 行 + 票批 v1 T-431 行 |
| Q7 | **by-digest 拉取强刷**（remote 缓存 TTL 语义对 by-digest 请求是否豁免） | remote 缓存语义面（小） | **【维持暂行——as-built 登记】**不强刷、TTL 统一维持（T-397 产品决策候选登记）；材料在案（Artifactory remote 缓存语义锚 + 分层缓存一致性代价）。翻转走条件小票（非 DoD）；**intake ⑤ 注**：缓存语义细面不属前端对齐主诉，默认不进 M16 翻案首批——用户点名才开票。BOARD 留痕 = conductor 审定段「Q7 维持 as-built」行 |

> **归位总账（v1.1）**：终裁落章 **3**（Q4 远端浏览出口 C 批 1——LC-76 归 A / Q5 cron 不引入 / Q6 docker virtual 开禁 → T-431 条件票）；规格回写归位 **2**（Q2 K63 定案 1,000/4/10s/429+Retry-After/408 / Q3 400 诚实拒绝 + C 层增强文案）；维持暂行 **2**（Q1 分阶段边界——**收口窗必裁，材料已齐**；Q7 by-digest——登记型）。**收口核查位**：T-430 AC「Q1~Q7 归位核查」+ DoD#5；LC-56 教训（M13 滞留待裁）防范——**唯一硬待裁 = Q1（M16 立项前提）**，零「待裁」层滞留（LC-76 已清）。

---

## 8. M15 验收剧本（QA 总纲）

1. **前置锚先行**：L20（aql.md 交付 + t226 活体核验 + 口径归一 + 就绪度确认）——FR-133/134 断言的规格地基；ADR-0043 Accepted 核查。
2. **回归基线（硬门槛先行）**：M1~M14 全部 P0 序列双形态复跑全绿；断言反转三处预核实（SR-03/SR-04 现值 404 在案 / tree-empty-virtual 现态在案 / mint 现值 500 在案——反转后归属审计清晰）。
3. **AQL 主线**：L21（语言全链 + compact + 错误面——**已落 T-409/T-411/T-415**）→ L22（执行内核注入用例 + planner 快照——**已落 T-411**：注入红线 7 形 × 8 恶意值 + IR→SQL 快照 17 形 + 万节点 P95 六形）→ L23（ACL 探针 + 资源门满载——**BE 已落 T-413**；真栈并发饱和与 AQL 腿 e2e 复核归 T-421/T-430）。
4. **老搜索并轨**：L24（**已落 T-417**：三端点 + mvn/M10 真实数据腿 + 断言反转① + K64 落笔——t92 全量 spec 零改动通过）。
5. **FE 副线**：L25（**已落 T-414/T-419**：列选器三页 + member-pop + 搜索页 AQL 模式——四闸门 + axe 双主题 0 + 服务端 diff=0 + SPA 预算内）。
6. **浏览收口**：L26（**已落 T-412/T-416**：聚合并集 + 断言反转②〔锚不退役两态〕——t226 对照腿降级〔实例无 virtual 仓，INC-1 禁建仓〕，官方 + 反编译双源 + BinFlow e2e 对拍替代留痕）。
7. **评估与复制**：L27（**已落 T-425**：13 型矩阵 + Q4 终裁出口 C 批 1）→ L28（**已落 T-418/T-420/T-422**：三件套 + 双实例编排 + outbox 引擎 diff=0；e2e 真栈腿/封锁门/toast 断言翻转与 FE R8 形态核验归 T-421）。
8. **契约与硬化**：L29（**已落 T-410**：mint 400 + 断言反转③）→ L30（busy 预算——**T-423 在途**：24 路清零 + 写路径专项回归）。
9. **债包**：L31（L2 快捷 + 纪律成文——**T-424 在途**；文面回写簇——**T-428 在途**）。
10. **收口**：L32（全量回归 + 归属审计）→ L33（NFR 与性能门槛 + F1 趋势登记）→ L34（文档实测复跑）。
11. **文档**：tech-writer——AQL 用户指南（子集边界明示 + 迁移脚本对照）、搜索 API 参考（AQL + 老搜索首批）、搜索页/虚拟浏览/复制包 B 增量、FAQ（AQL 子集与未支持域行为）。
12. **release**：部署烟测 + UAT 随里程碑 PR（M13 起常态）；Replicate Now 双实例腿在 UAT 链取证。

---

## 9. M15 DoD

1. §4 全部 P0 AC（FR-132 规格锚 / FR-133 AQL 引擎 / FR-134 老搜索首批）经 qa 验证全绿；P1（FR-135/136/138.1/139.1）全绿；P2（FR-137 评估票/138.2~138.3/139.2/140 全部）全绿；条件票（dates/creation 顺车 K65 / 远端浏览实现段 Q4 / docker virtual 开禁 Q6 / by-digest 强刷 Q7 / NuGet symbol server 余量五承 / Tokens 字段集补核验）按余量/条件条款——未触发不构成 DoD 缺口，须 BOARD 留痕；
2. §8 剧本全绿；§1.2 量化门槛表逐行达标（AQL 全链/ACL/资源门/老搜索/搜索面/virtual 聚合/评估材料/复制包 B/契约硬化——全部有实测数字或 curl/Playwright 证据归档）；
3. **搜索域专项收口**：§5.7 端点全景表逐行核对（实现/维持/断言反转/未实现 404 四态与表一致——未列端点仍 404）；断言反转三处（SR-03/SR-04 + tree-empty-virtual + mint）归属审计 100% M15 豁免票；aql.md 置信度列经 t226 回写（零静默升格——「以核验为准」附注与结论一一对应）；**v1.1 执行态**：三处反转已落（T-417 / T-412+T-416 / T-410——归属审计就绪）+ K64 局部翻转（大小写不敏感）同归 T-417 豁免票；§5.7 as-built 对账完成（v1.1——含 archive / latestVersionByProperties 两外挂端点补登，全量口径 14+2+1 零遗漏）；aql.md 置信度回写已落（T-407——高 ~14 / 中 ~6 / 低 8 全在册）；
4. 回归硬门槛：M1~M14 全部 P0 序列双形态复跑全绿；M14 as-built 零行为变化（豁免票归属外——replication 三票 spec / docker remote / npm login 面）；FE 变更面 100% 归属 M15 票 + FE 票服务端 diff=0；
5. 前置产物与 Q 归位：aql.md + ADR-0043（Accepted）+ replication.md 增量段齐备；Q1~Q7 归位——**收口窗必裁 Q1（分阶段边界）与 Q4（LC-76 离开「待裁」）**，其余随规格回写归位或维持暂行登记（每项归位路径在案，LC-56 教训防范）；**v1.1 归位基线（§7 总账）**：前置三产物齐备（T-407 / T-408 + 三笔勘误 / T-418）；**Q4 已终裁（出口 C 批 1——LC-76 归 A）**、Q5/Q6 终裁落章（不引入 cron / 开禁→T-431）、Q2/Q3 规格回写归位、Q7 维持登记——**唯一硬待裁 = Q1（收口窗必裁，材料已齐 §7）**；
6. NFR-P67~P69 / NFR-S73~S75 达标归档；`make test`（race）全树绿维持 / `make lint` 0 issues / gofmt 空；资源门三连（footprint ≤100MB / check-size ≤120MB 调基门 / 冷启动 <2s）+ SPA 每票 ≤10KB + F1 六平台趋势观察登记；
7. §2.2 Non-goals 与候选池对账完成：滚入 M16+ 项在 ROADMAP「M15 未纳入项」登记（备稿沿 T-395 先例，收口窗启用）；§1.4 出处义务在全部 M15 票可审计（aql.md/官方文档锚点逐条）；
8. 主会话 git tag `m15-done`（对外发布任何制品先经用户确认；PR 化合并沿既定 gitflow 程序；UAT 证据随里程碑 PR 归档）。
