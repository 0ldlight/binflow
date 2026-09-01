# M15 拆票日志（tech-lead，2026-09-01）

> 输入：docs/prd/milestone-15.md v1.0（conductor 已审定转正——Q5 终裁不引入 cron 双轨 / Q6 即裁开禁列条件票 / Q1·Q3 暂行确认）+ BOARD.md「M15 票据」段审定记录 + reports/agents/M14-SPLIT.md 先例（格式/波次/风险体例）+ .claude/team/SPRINT-LOOP.md（宽度 ≤2 lane + 插空纪律）+ 代码锚点核实（internal/ 无 search 包〔新包〕、docs/reverse/ 无 aql.md、DECISIONS.md 最新 ADR-0042、internal/httpapi/search.go + t92_search_test.go 既有面、internal/repo/service.go:210 allow()、internal/metadata node_props 面）。
> 产出：BOARD.md「### M15 票批 v1（tech-lead）」节（T-407~T-431，本日志为拆票依据与派发建议）。
> 票数对齐：PRD §1.3 估 19~25（含条件票 slot），实拆 **25 票**（P0×8 / P1×11 / P2×6——波外条件票 T-431 计入 P2）——线内上沿。对账：FR-133 拆四（conductor「引擎内核/端点面/ACL+资源门」四分指令）上浮 1，由 FR-138 三件合二、FR-140 三段合二、tech-writer 二合一抵消；K65（dates/creation 顺车）为 T-417 票内余量条款不占号，Q4 实现段 / Q7 by-digest 不占号（终裁触发现场立票——M14 D3 先例）。

## 0. 范围与拆票基线

**M15 = 搜索基建专程（AQL 首程）**：一条 P0 主线（规格锚 → AQL 语言/内核/门/端点 → 老搜索并轨 → FE 搜索面）+ 三条副线（virtual 聚合 P1 / 复制包 B 首批 P1·P2 / 契约硬化小包 P1·P2）+ 评估票与债包（P2）+ 波外条件票（Q6 已裁开）。断言反转三处预归属：SR-03/04 → T-417、tree-empty-virtual → T-416、mint 500→400 → T-410；acl 零泄漏探针为每张查询面票硬 AC（§1.4 条款 4）。

拆票纪律：
- **B0 前置锚双票并行**（conductor 指令）：T-407 aql.md（reverse）+ T-408 ADR-0043（architect）——**L20 就绪 + ADR Accepted 前主轴实现票不开工**。
- **全宽 2**（沿 M11~M14 口径），波内 area 互斥；FE 主线 web/src 一波一票错峰（B3→B4→B6→B8）；AQL 主轴六波串行链（B0~B5）为决定性路径。
- **area 族**：internal/search（新包，AQL 引擎）/ internal/httpapi 搜索面（T-415/T-417 先后脚）/ internal/repo（virtual 聚合、建仓矩阵）/ internal/replication + httpapi 复制面 / internal/metadata（只读查询缝 + busy 面）/ internal/storage/remote / web/src（FE 分页面组）/ docs 规格域（docs/reverse + docs/user + docs/design + DECISIONS）/ deploy·charts。同族跨波串行，异族同波并行。
- **行为面票必须 dep 规格票**：T-409/411/413/415/417 dep T-407（+T-408）；T-420/422 dep T-418（replication.md 增量段——独立前置规格票，M14 T-393/T-394 先例，取舍见 §6）。

## 1. 票据表

### 1.1 FR → 票映射

| FR | 票（P） | 承载要点 | 裁决/登记锚 |
|---|---|---|---|
| FR-132 | T-407（P0，B0 前置锚） | aql.md 新建：官方文档逐条锚 + inv 补白 + t226 活体核验腿（oss 档可用）+ 口径归一（14-vs-13 勘误/子集边界表/K64 校准/基座映射表） | L20；K62/K64/K65；降级路径内置 |
| （前置）ADR-0043 | T-408（P0，B0） | EBNF 子集 / AST→参数化 SQL 映射 / ACL 织入点 / 资源门参数 / 错误形态与 E-01 关系 / WriteTimeout 交互归位（T-392 登记项） | §6.1；soft-dep T-407 132.4④ |
| FR-133 | T-409（语言）/T-411（内核）/T-413（ACL+资源门）/T-415（端点面）——四票 P0 串行 | lexer/parser/AST → planner/SQL 编译/投影 → allow() 织入+K63 三件门 → POST /api/search/aql+compact+E-01+metrics | LC-68/69/72/73；NFR-P67/S74 |
| FR-134 | T-417（P0 三端点同票） | gavc/prop/pattern + 断言反转① + K64 落笔 + K65 票内余量条款 | LC-70/71；L24 |
| FR-135 | T-414（列选器三页+member-pop P1 零依赖早波）/ T-419（AQL 模式 P1） | T-387 spec 形态复用 + T-391 L-a 清账 / 编辑器+错误内联+结果表 | LC-74；L25 |
| FR-136 | T-412（service 聚合 P1）/ T-416（FE 树消费 P1——断言反转②） | children 成员并集 + 解析同源 + ACL 同门 / tree-empty-virtual 退役 + RepoBranch 动态恢复 | LC-75；FR-21-AC8；L26 |
| FR-137 | T-425（P2 评估票） | 13 包型上游枚举能力矩阵 + 三出口材料 + Q4 材料包上 BOARD（不设实现断言） | LC-76；K66；L27 |
| FR-138 | T-418（replication.md 增量段 P1 前置规格）→ T-420（Replicate Now P1）/ T-422（Test+封锁合票 P2） | 双源材料（官方 REST 主源）→ 全量同步任务（outbox 复用 diff=0）+ ▶ 接线 / Test 探测零副作用 + 全局双开关 UI-API 不受门 + audit 补词 | LC-77；Q5 已终裁不引入 cron；L28 |
| FR-139 | T-410（mint P1 B1 插空——断言反转③）/ T-423（busy P2 B8） | unknown username 400 逐字文案 / 24 路 0.27% 边角清零 + 写路径专项回归 | LC-78；L29/L30 |
| FR-140 | T-424（L2 快捷+e2e 纪律成文合票 P2）/ T-428（文面回写簇 P2） | 复制 key/Set Me Up 直开（E1 不倒退）+ 三节纪律 + flake 注记 / 四处文面 + ux 会签两行 | LC-79；L31 |
| QA/文档/发布/裁定 | T-421（中期 P1）/ T-430（终验 P0）；T-426（tech-writer 一票两腿 P1）；T-429（release+UAT P1）；T-427（PM Q 终裁联动 P1） | L20~L34 + §5.7 逐行 + DoD 八条 / 两腿先后笔不阻塞 / F1 趋势登记 / Q1·Q4 收口窗必裁 | §8 剧本；K62~K66 回填 |
| 条件票 | T-431（Q6 docker virtual 开禁——conductor 已裁开，随时插空非 DoD）；K65=T-417 票内余量；Q4 实现段/Q7 不占号 | 未触发/未插空 BOARD 留痕非 DoD 缺口 | Q6/Q7/K65 条款 |

优先级统计：**P0×8**（T-407/408/409/411/413/415/417/430）；**P1×11**（T-410/412/414/416/418/419/420/421/426/427/429）；**P2×6**（T-422/423/424/425/428/431）。

### 1.2 票据明细（AC 全文——派单直接引用；`BASE=http://127.0.0.1:8080` `ADMIN=admin:password`）

**T-407 [P0] FR-132 aql.md 规格票 + t226 活体核验 + 口径归一**
role: reverse-engineer ｜ area: docs/reverse/aql.md（新建）+ inv-2/主矩阵勘误回写 ｜ dep: —
AC:
 1. aql.md 落盘（webhook.md 体例）：官方文档逐条锚点（域.字段路径/操作符集/$and·$or·$not/`.include().sort().offset().limit()`/compact/REST wire/envelope/错误码族）+ inv-1 §E、inv-2 §1.C 补白 + 置信度列（132.1/132.2）。
 2. t226 活体核验腿（oss 档——容器恢复沿 T-381 §0 先例；差集法只读 INC-1 纪律）：语言边角/错误文案逐字/上限行为/**virtual 仓在 AQL 中的语义**（133.5 锚）；核验源不可得项「以核验为准」零静默升格（L20）。
 3. 口径归一：14-vs-13 勘误定案回写主矩阵 + BinFlow 子集边界表 + K64 校准结论（翻转则登记断言反转归 T-417）+ 基座映射表（nodes 列面→item 字段）交 ADR-0043 + tech-lead 就绪度确认。

**T-408 [P0] ADR-0043 AQL 引擎架构**
role: architect ｜ area: DECISIONS.md（ADR-0043）+ docs/design/architecture.md 搜索域增量节 ｜ dep: —（软协作：T-407 132.4④ 基座映射表，Accepted 前对齐——不一致处留痕裁决）
AC:
 1. ADR-0043 Accepted：语言子集文法形式（EBNF）/ AST→参数化 SQL 编译映射（nodes+node_props join、name/depth 派生、sha1/md5 取数路径）/ ACL 谓词织入点（repo.Service allow() 同源不另建通道）/ 资源治理门参数（K63）/ 错误形态与 E-01 envelope 关系。
 2. internal/search 包边界与 metadata/repo 只读缝接口定案（不跨包摸内部结构）；httpapi WriteTimeout 与长查询交互随本 ADR 评审归位（T-392 登记项——不另开面）。
 3. 与 aql.md 基座映射表一致性核对（差异留痕）；FR-133 四票拆分边界（语言/内核/门/端点）确认为可实现粒度。

**T-409 [P0] FR-133.1 AQL 语言前端（lexer/parser/AST 校验）**
role: dev-go-core ｜ area: internal/search（新包语言前端——定名从 ADR-0043；纯函数零 IO 零 DB） ｜ dep: T-407, T-408
AC:
 1. `items.find(<criteria>)` 文法解析 table-driven 单测：item 域字段全集（aql.md 定案）/ property 域嵌套 / 操作符子集 / $and·$or·$not 任意复合 / 尾缀方法链 `.include().sort().offset().limit()`——合法与边角用例全覆盖。
 2. 语法错/非法字段/未支持域 → 结构化错误对象（envelope 前形态，含 domain/field 名——诚实拒绝零伪空集，文案照 aql.md 锚）；AST→SQL 编译消费接口定案（供 T-411）。
 3. gofmt + golangci-lint 零告警 + `make test`（race）全绿；零 DB 依赖（解析止步 AST）。

**T-410 [P1] FR-139.1 mint unknown username 400 修正（断言反转③）**
role: dev-go-core ｜ area: internal/httpapi auth 域（mint handler；internal/auth Tokens.Issue 错误分支最小改随票） ｜ dep: —（插空票——PRD「可 B1 插空」）
AC:
 1. `curl -u $ADMIN -X POST "$BASE/api/security/token" -d 'username=ghost&expires_in=300'` → **400** auth-model 3.1 逐字文案（非 500）——断言反转③归属本票（L29）。
 2. 既有 mint 全场景零回归（正确凭据/错误密码/step-up 链）+ T-386 FE 错误内联呈现复核（服务端修正后 FE 语义不劣化——错误面不泄漏用户存在性）。

**T-411 [P0] FR-133.2 AQL 执行内核（planner/SQL 编译/投影）**
role: dev-go-core ｜ area: internal/search（planner/编译器）+ internal/metadata 只读查询缝（接口驱动——新 read API 走 metadata api 面） ｜ dep: T-409
AC:
 1. AST→**参数化 SQL** 编译（nodes+node_props join、name/depth 派生、sha1/md5 取数照 ADR-0043；include 投影/sort/offset/limit 编译）——AST→SQL 快照 table-driven（L22 主腿）。
 2. 注入面红线：零字符串拼接——表驱动注入用例 + wildcard 转义核验（NFR-S74）；`idx_node_props_name` 预留索引消费核验（M10 兑现）。
 3. 万节点语料（M10 属性夹具扩展：多仓/多深度/多属性）item 域 P95 ≤500ms / property join P95 ≤800ms（NFR-P67 前置腿，本机 SSD+WAL）。

**T-412 [P1] FR-136.1/136.2/136.4 virtual 聚合 service（children 成员并集）**
role: dev-go-core ｜ area: internal/repo/service.go（List/Get virtual 臂——聚合浏览与 pull 解析同源一处实现） ｜ dep: —（零依赖；与 internal/search 天然错峰——repo.Service 读缝对齐经 ADR-0043）
AC:
 1. 双成员夹具（M3 FR-21 既有）→ `GET /api/storage/<virtual>/<path>` folder 面 200 children = 成员并集（逐名断言 + 同名路径合并 + folder/file 实态标记 + 深层递归臂）——FR-21-AC8 逐字（L26 BE 腿）。
 2. 解析顺序与 pull 聚合同源（成员顺序优先语义维持，不引入第二解析通道）+ ACL 沿内容面 allow()（成员级越权行零泄漏探针）+ 无成员/全空成员空态文案区分。
 3. M3 FR-21 解析序列 + M4 浏览 spec 零回归（go 全量 race）；t226 形态对照材料归 QA 中期腿。

**T-413 [P0] FR-133.2/133.4 ACL 织入 + 资源治理门**
role: dev-go-core ｜ area: internal/search 引擎 entry（ACL 谓词织入 + K63 门 + 流式装饰）——repo.Service allow() 只读消费（T-92 同源） ｜ dep: T-411
AC:
 1. ACL 织入：调用者可读仓集 → repo_key 谓词（与 T-92 SearchArtifacts 同一 allow() 源，不另建权限通道）——双用户夹具越权仓零出现（t92 探针形态服务端腿，L23）。
 2. K63 三件门（暂行值随 aql.md 回写归位）：结果上限 1,000 截断标记 + offset/limit 分页可达全量 / 并发上限 4 → 429 + Retry-After / 执行超时（形态照锚）——慢查询注入（全表 like）超时用例在场。
 3. 满载 8 路混合查询 + 写入负载零 5xx 零 OOM（结果集流式装饰）+ `make test`（race）全绿（NFR-P68 前置腿）。

**T-414 [P1] FR-135.2/135.3 FE 列选器三页推广 + member-pop hover**
role: dev-frontend ｜ area: web/src（users/groups/search 三列表页列选器 + repositories virtual Tab member-pop hover） ｜ dep: —（零 BE 依赖早落）
AC:
 1. 三页列选开合/显隐持久（reload 保持）/全选复位——T-387 spec 形态复用；per-page localStorage 键名沿 `binflow-console-cols-{users,groups,search}` 定式；「无端点列不伪造」纪律维持；其余列表页不受影响。
 2. member-pop hover 对比度 ≥4.5:1 双主题（T-391 color-mix 配方——L-a 遗留清账）。
 3. 四闸门 + axe 双主题 serious=0 + 服务端 diff=0 + SPA 增量 ≤10KB。

**T-415 [P0] FR-133.3/133.5 AQL 端点面**
role: dev-go-core ｜ area: internal/httpapi search 面（POST /api/search/aql + compact + 错误 envelope + metrics） ｜ dep: T-413
AC:
 1. L21 全链：curl `POST /api/search/aql`（操作符复合 + property 嵌套〔M10 夹具腿〕+ include/sort/offset/limit 逐字段断言）+ `?compact` 双形态对照；语法错/非法字段/未支持域（`build.find`/statistics 字段）→ 400 E-01（文案逐字照 aql.md 核验样本——domain-not-supported 零伪空集）。
 2. L23 e2e 腿：双用户探针（受限用户全域/逐仓查询越权仓行零出现——t92 探针形态 + AQL 腿）+ 匿名/未认证 401/403 照 T-92 既有门；virtual 仓语义照 aql.md 活体锚（133.5——暂行实际存储行）。
 3. metrics search family 新组（查询计数/时延 histogram/429 与超时计数 + 慢查询单行日志）+ 既有 `/api/search/artifact|checksum` 零回归（T-92 全量 spec）+ WriteTimeout 交互按 ADR-0043 落地。

**T-416 [P1] FR-136.3 virtual FE 树消费（断言反转②）**
role: dev-frontend ｜ area: web/src artifacts（ArtifactsBrowser——virtual 树动态展开 + 空态翻转） ｜ dep: T-412
AC:
 1. `tree-empty-virtual` 空态锚退役/翻转（有成员内容不再空态——锚册留痕断言反转②；无成员/全空维持空态）+ RepoBranch virtual 恢复动态展开（T-406 as-built 受限面解除）。
 2. Playwright：virtual 树展开实态（多成员夹具——children 并集逐名渲染 + 深层递归）+ M3/M4 浏览 FE spec 零回归。
 3. 四闸门 + axe 双主题 serious=0 + 服务端 diff=0（FE 面）+ SPA ≤10KB。

**T-417 [P0] FR-134 老搜索三端点 + 断言反转①（含 K65 余量条款）**
role: dev-go-core ｜ area: internal/httpapi search 面（gavc/prop/pattern 三端点——internal/search 匹配内核消费） ｜ dep: T-415（httpapi 搜索面先后脚）
AC:
 1. L24 三端点：gavc（mvn deploy 真实坐标数据腿命中 + v/c/repos 限定臂 + 未命中空集 200 + 坐标非法 400——M3 layout 解析器复用兼容子集）；prop（M10 属性夹具 `?props=license=Apache-2.0` 命中 + 键无值臂 + 未知键空集 200）；pattern（`maven-local:com/acme/**/*.jar` 树命中 + 跨仓通配臂 + 非法 400——与 AQL `$match` 共享匹配内核）。
 2. 断言反转①：t92_search_test.go SR-03/SR-04 三行 404 → 分派实现（其余未实现族 404 维持——§5.7 归属为准）+ 既有 artifact/checksum 全量零回归 + K64 落笔（aql.md 结论执行；翻转则断言更新票内归属留痕）。
 3. 共通语义：envelope `{"results":[FileInfo...]}`（E-09/T-92 形态复用）+ ACL 同源（同一 allow() 源越权探针）+ 结果上限沿 K63；**K65 余量条款**：dates/creation 若 T-407 判 trivial 且余量顺车（票内留痕），否则 M16 登记。

**T-418 [P1] replication.md 增量段（包 B 前置规格——双源材料）**
role: reverse-engineer ｜ area: docs/reverse/replication.md（增量段） ｜ dep: —
AC:
 1. 三面 wire 规格：executereplicationnow 对位端点 / blockPush·blockPull 全局封锁 / Test 连接——官方 REST 文档锚点为主源（t226 entitlement 锁不可活体——webhook.md 先例）+ T-402a R 系实测补白；路径/方法/参数/envelope/错误码/幂等语义逐条出处 + 置信度。
 2. Test 端点形态定案（官方 validate/test 面对位或 `/api/v1` 自有 C 层登记）+ 审计词表补词清单（replication.config.* 族——M14 T-405 遗留同场清）。
 3. tech-lead 就绪度确认（解锁 T-420/T-422）；clean-room 合规（官方文档优先）。

**T-419 [P1] FR-135.1 搜索页 AQL 模式**
role: dev-frontend ｜ area: web/src/pages/search（模式切换 + AQL 编辑器 + 结果表） ｜ dep: T-414, T-415（端点稳定）
AC:
 1. L25：模式切换（既有基本表单维持不动）/ 合法查询结果渲染 / 语法错内联呈现（400 E-01 文案透传）/ 分页排序交互；`smu/search` 既有锚零改名 + 新锚入册（anchor-audit 0 断链）。
 2. AQL 编辑器（textarea mono）+ 只读面零新端点（消费 T-415 既有端点）+ 结果表复用既有列框架与列选器（T-414 交付面）。
 3. 四闸门 + axe 双主题 serious=0 + 服务端 diff=0 + SPA 增量 ≤10KB。

**T-420 [P1] FR-138.1 Replicate Now（全量同步任务 + ▶ 接线）**
role: dev-go-core ｜ area: internal/replication + httpapi 复制面（+ web/src RepositoriesPage ▶ 接线小腿——与 T-419 文件不相交，conductor 裁量并行/先后脚） ｜ dep: T-418
AC:
 1. L28 主腿：executereplicationnow 对位（wire 照 T-418）触发指定仓 push 配置**全量同步**——双实例自指上游编排（M6 复制夹具）目标仓制品数/校验和与源一致；在途重复触发幂等（200/409 照锚）；enabled:false 触发拒绝语义照锚。
 2. 任务载体复用 ADR-0041 outbox/队列模式**非引擎重构**（webhook outbox 引擎文件 diff=0 审计断言）；任务状态可查（复用既有任务/投递观测面）。
 3. FE ▶ 动作接线（T-404 列表 Replications 列——真语义替换占位）→ 任务可见状态翻转；T-404/T-405 replication 面零回归（PUT enabled/列表投影）。

**T-421 [P1] QA 中期回归**
role: qa-engineer ｜ area: 测试矩阵（L20~L26/L29 已落面 + 归属审计） ｜ dep: T-407, T-410, T-412, T-415, T-416, T-417（T-419 随到随测）
AC:
 1. 主轴复核：L21/L22（语言全链 + 注入用例 + planner 快照）/ L23（ACL 探针 + 资源门满载）/ L24（三端点 + 断言反转①）/ L26（virtual 聚合并集 + t226 形态对照腿）/ L29（mint 归位）。
 2. M1~M14 P0 双形态增量窗口回归 + 断言反转三处现值预核实（SR-03/04 现值 404 / tree-empty-virtual 现态 / mint 现值 500 在案——反转后归属审计清晰）。
 3. 缺陷登记与回归报告落盘（reports/）——诚实汇报（跳过/在途注明）。

**T-422 [P2] FR-138.2/138.3 Test 连接 + blockPush/blockPull 全局封锁**
role: dev-go-core ｜ area: internal/replication + httpapi 复制面 + web/src 复制配置面（开关 + 表单 Test） ｜ dep: T-418, T-420（internal/replication 先后脚）
AC:
 1. Test：正确凭据成功 / 错误凭据·不可达内联失败原因 / 探测零副作用（上游只读接触 + 凭据不落日志——NFR-S75）。
 2. 封锁：blockPush=on 新复制事件不入队（源仓再部署目标零到达）+ REST 配置通道仍可用（**UI-API 不受门**——t226 实测语义）+ 在途推任务停发；blockPull=on 拉侧封锁（remote 回源/智能拉取照既有 remote 语义拒绝/降级）；双开关审计行在场（replication.config.* 补词）。
 3. 配置载体 binflow.yaml 全局段 + REST + 控制台开关三面一致 + FE 状态呈现（parity R 系锚定形态）+ T-404/T-405 零回归。

**T-423 [P2] FR-139.2 remote 缓存树 busy 重试预算**
role: dev-go-core ｜ area: internal/storage/remote 缓存写路径（fetcher 落盘 + node 行写）+ internal/metadata busy_timeout 面（architect 会签姿势） ｜ dep: —（B8 定位——与 AQL 满载回归同场）
AC:
 1. T-377 D1 复现脚本 24 路并发缓存树重放 → **零 5xx**（0.27% SQLITE_BUSY 边角清零）+ 8 路门内口径维持不倒退（L30）。
 2. SQLite 写路径专项回归：上传/GC/回收站/webhook outbox/复制入队既有写面全量零回归 + `make test`（race）全树。

**T-424 [P2] FR-140.1/140.2 L2 行内快捷 + e2e 纪律成文**
role: dev-frontend ｜ area: web/src repositories 行内 + web/e2e README/CONTRIBUTING ｜ dep: —（FE lane 排队——与 T-420 FE 小腿先后脚）
AC:
 1. L31 主腿：仓库列表行内「复制 key」（剪贴板/回显断言）+「Set Me Up」直开（抽屉复用零重复实现）+ **行尾无删除缺席断言**（E1 不倒退）+ a11y（aria-label + 键盘可达）。
 2. 三节纪律成文：破坏性动作默认禁点 + 共享 fixture 快照前置（INC-1 教训）/ pkill 按端口精确杀（T-382/T-384/T-389 三起误伤）/ assert-tokens 属性选择器豁免（T-390）；两条 flake 注记（m9 N01 spec 级竞态 + m9 seed 并行互撞 CI workers=2）。
 3. 四闸门 + axe 双主题 + e2e 全量绿（纪律票零行为改动——diff 审计）+ SPA ≤10KB。

**T-425 [P2] FR-137 remote 远端浏览评估票（不设实现断言）**
role: reverse-engineer（dev-registry-adapter 会签） ｜ area: docs/reverse/（能力矩阵 mini 规格——落盘形态 reverse 定） ｜ dep: —（零依赖；材料须于收口窗前上 BOARD）
AC:
 1. L27：13 包型逐行能力矩阵——上游目录/包列表枚举 API（helm index.yaml / docker tags/list / maven-metadata / npm packument / conan search / pypi simple / nuget v3 catalog 等）、翻页形态、全量成本、增量探测可行性、Artifactory 远端浏览对应行为（官方文档 + t226 活体）+ 置信度逐行。
 2. Q4 三出口材料包（全做/子集/维持缓存浏览——利弊 + 实现量级 + 上游压力/限流/降级风险 + PM 倾向）上 BOARD——终裁后实现段归 M16+ 或条件票（不代拍）。

**T-426 [P1] 文档票（两腿——票内先后笔）**
role: tech-writer ｜ area: docs/user/（AQL 指南 + 搜索 API 参考 + 增量 + FAQ） ｜ dep: T-415, T-417（腿①）；腿②候 T-416/T-419/T-420/T-422 合入
AC:
 1. L34 腿①：AQL 用户指南（**子集边界明示 + 未支持域 400 行为 + 迁移脚本对照**）+ 搜索 API 参考（AQL + 老搜索首批）——客户端命令实测可复跑。
 2. 腿②：搜索页 AQL 模式 / virtual 聚合浏览 / 复制包 B（Replicate Now·Test·封锁）增量 + FAQ（AQL 子集与迁移对照）；`make docs` 零断链。

**T-427 [P1] PM Q 终裁联动回写**
role: product-manager ｜ area: docs/prd/milestone-15.md（收口笔）+ ROADMAP「M15 未纳入项」 ｜ dep: T-407, T-425
AC:
 1. 收口窗必裁两项落章：Q1（AQL 分阶段边界——M16 立项前提）+ Q4（LC-76 离开「待裁」——三出口终裁）；Q2/Q3 随 aql.md 回写核对归位；Q5（已终裁）/Q6（已裁开）/Q7 落章核对。
 2. K62~K66 回填 + 断言反转三处回写核对 + 「M15 未纳入项」备稿启用（T-395 先例）+ §5.7 全景表 as-built 对账。

**T-428 [P2] FR-140.3 文面回写簇**
role: tech-writer（ux-designer 会签两行） ｜ area: docs/reverse/README.md + docs/design/parity 册 + brand/package-icons README + docs/user/migrate-artifactory.md ｜ dep: —（P2 尾波）
AC:
 1. 四处回写落笔：docs/reverse/README 补 npm.md 行（T-393 登记）/ parity 册 M1 行「定案 440px」升级 + M3 行 MUI Paper 代差描述（ux）/ package-icons helm·nuget 暗底提亮超 +10% 量级拍板（ux）/ migrate-artifactory.md 措辞陈旧清账（T-397 登记）+ `make docs` 零断链 + 修订留痕。

**T-429 [P1] release 烟测 + UAT 随里程碑 PR**
role: release-engineer ｜ area: deploy/ + charts/ + CD 链 ｜ dep: 全部实现票（T-409~T-424、T-431 若已执行）+ T-426
AC:
 1. 部署烟测（compose/k8s/systemd 矩阵）+ UAT 随里程碑 PR——Replicate Now 双实例腿在 UAT 链取证 + version sha 翻转。
 2. 资源门三连复核（footprint ≤100MB / check-size ≤120MB 调基门 / 冷启动 <2s）+ **F1 六平台趋势观察登记**（103.37MB 基线——AQL 纯 Go 净增量须可忽略，显著上浮即红旗）。

**T-430 [P0] QA 终验**
role: qa-engineer ｜ area: 全量矩阵 ｜ dep: 全部 + T-429
AC:
 1. L20~L34 全量 + **§5.7 端点全景表逐行核对**（实现/维持/断言反转/未实现 404 四态与表一致——未列端点仍 404）+ 断言反转三处归属审计 100% M15 豁免票 + FE 变更面归属 M15 票 100% / FE 票服务端 diff=0。
 2. DoD 八条逐条 + NFR-P67~P69/S73~S75 达标归档（万节点 P95 / 8 路混合零 5xx / 资源门三连 / 注入用例）+ aql.md 置信度列与「以核验为准」附注一一对应（零静默升格）+ m15-done 就绪判定。

**T-431 [P2·条件·Q6 已裁开] docker virtual 建仓矩阵开禁**
role: dev-go-core ｜ area: internal/repo 建仓矩阵（docker×virtual 组合行）+ web/src 门控一行 ｜ dep: —（**随时插空**——避开 T-412 波次〔internal/repo 共写〕与 FE 主轴票；非 DoD）
AC:
 1. docker virtual 建仓开禁（矩阵一行）+ FE 门控放开 + docker local/virtual/remote 三态回归零回归（M14 docker 序列）+ helmoci virtual 先例对照留痕。

## 2. 波次与并行分区（全宽 2；波内 area 互斥，跨波同 area/同角色串行；FE 主线一波一票错峰）

| 波 | lane 1 | lane 2 | 要点 |
|---|---|---|---|
| **B0** | T-407 [P0] aql.md 规格票 | T-408 [P0] ADR-0043 | 前置锚双票并行（conductor 指令）；L20 + Accepted 前主轴不开 |
| **B1** | T-409 [P0] AQL 语言前端 | T-410 [P1] mint 400（插空——断言反转③） | 主轴开工 + P1 小票补位（PRD「可 B1 插空」） |
| **B2** | T-411 [P0] AQL 执行内核 | T-412 [P1] virtual 聚合 BE | internal/search+metadata 缝 vs internal/repo 错峰 |
| **B3** | T-413 [P0] ACL 织入+资源门 | T-414 [P1] FE 列选器三页+member-pop | 引擎 lane 收窄；FE 链起步（零依赖早落） |
| **B4** | T-415 [P0] AQL 端点面 | T-416 [P1] virtual FE（断言反转②） | httpapi 搜索面 vs web/src artifacts |
| **B5** | T-417 [P0] 老搜索三端点（断言反转①） | T-418 [P1] replication.md 增量段 | httpapi 搜索面先后脚（T-415→T-417）；规格解锁包 B |
| **B6** | T-419 [P1] FE AQL 模式 | T-420 [P1] Replicate Now | T-420 FE ▶ 小腿与 T-419 文件不相交（conductor 裁量） |
| **B7** | T-421 [P1] QA 中期回归 | T-422 [P2] Test+封锁双开关 | 主轴 L21~L24/L26/L29 复核窗 |
| **B8** | T-423 [P2] busy 重试预算 | T-424 [P2] L2 快捷+e2e 纪律 | busy 与满载回归同场（PRD 定位） |
| **B9** | T-425 [P2] 远端浏览评估票 | T-426 [P1] 文档票（两腿） | Q4 材料窗 + 用户文档 |
| **B10** | T-427 [P1] PM Q 终裁联动 | T-428 [P2] 文面回写簇 | Q1/Q4 收口窗必裁 |
| **B11** | T-429 [P1] release 烟测+UAT | — | 单票波（尾部） |
| **B12** | T-430 [P0] QA 终验 | — | 单票波；m15-done 就绪判定 |
| 波外 | T-431 [P2·条件] Q6 docker virtual 开禁 | — | 已裁开——任意空位插空（避开 T-412 波与 FE 主轴票）；非 DoD |

**插空纪律**：任一波提前收口或轻量完成时，优先补位序 = T-431（已裁开条件票）→ T-425（P2 评估票可前移，早出 Q4 材料）→ T-428（P2 文面零依赖）；FE lane 空位仅补 FE 票（T-424 可前移至 B6 后任意 FE 空位——dep T-420 FE 小腿先行）。

## 3. 依赖图与关键路径

```
B0  T-407(aql.md) ──┬─软协作132.4④── T-408(ADR-0043)
     │ dep ┌────────┴─────────┐
B1  │     T-409(AQL 语言前端)   T-410(mint 400·插空——断言反转③)
     │        │
B2  │     T-411(执行内核) ←──┐  T-412(virtual 聚合 BE·零依赖)
     │        │              │      │
B3  │     T-413(ACL+资源门)  │  T-414(FE 列选器三页+member-pop·零依赖)
     │        │              │      │
B4  │     T-415(AQL 端点面)  │  T-416(virtual FE——断言反转② ← T-412)
     │        │              │      │
B5  │     T-417(老搜索三端点——断言反转①)   T-418(replication.md 增量段)
     │        │                            │
B6  │        │      T-419(FE AQL 模式 ← T-415+T-414)   T-420(Replicate Now ← T-418)
     │        │                                            │
B7  │     T-421(QA 中期 ← T-407/410/412/415/416/417)   T-422(Test+封锁 ← T-418+T-420)
     │
B8  T-423(busy 预算·零依赖 B8 定位) ｜ T-424(L2+纪律·FE lane)
B9  T-425(评估票·零依赖) → T-427(PM ← T-407+T-425) ｜ T-426(文档 ← T-415/T-417 + 腿②候 B6/B7)
B10 T-427(PM) ｜ T-428(文面)
B11 T-429(release ← 全部实现票 + T-426)
B12 T-430(QA 终验 ← 全部 + T-429) → m15-done
波外 T-431(Q6 已裁开·随时插空)；K65=T-417 票内余量条款；Q4 实现段/Q7 不占号
```

**关键路径 = AQL 主轴六波串行链**（B0 前置锚 → B1 语言 → B2 内核 → B3 ACL+门 → B4 端点 → B5 老搜索）→ B7 QA 中期 → 尾部 B8~B12（busy/债包 → 评估+文档 → PM/文面 → release → 终验）。三条副线全部挂在主轴空侧 lane：virtual（B2→B4 与引擎错峰）、复制包 B（B5 规格→B6→B7）、FE 链（B3→B4→B6→B8）；无任何副线反压主轴。**主轴任一票延期直接平移全链**——见 R1/R9 压缩选项。

## 4. 风险登记

| # | 风险 | 缓解 |
|---|---|---|
| R1 | **规格前置门与活体降级**——L20（aql.md）+ ADR-0043 双锚未齐主轴不开（conductor 指令）；t226 容器恢复失败即活体腿降级 | T-407 票内降级路径内置（容器 docker start 沿 T-381 §0 → 不可得走官方文档单源 + 「以核验为准」附注零静默升格——B0 不恋战，但 L20 交付以文档锚为底线）；B0 延期平移全链——主轴压缩选项见 R9；ADR 与 aql.md 并行互不阻塞（软协作缝仅 132.4④ 映射表，Accepted 前对齐） |
| R2 | **K62/K63/K64 低置信项**——未支持域文案/429 形态/超时形态/匹配语义核验前为暂行 | 效力序：用户裁决（BOARD）> aql.md（核验回写后）> PRD 暂行；核验回写 = 改契约（aql.md 修订留痕）；K64 翻转则断言反转预归属 T-417（豁免票在案）；K63 与 Artifactory 默认冲突处走 C 层留痕不硬仿（Q2） |
| R3 | **repo.Service 读缝共享**——T-413（ACL 织入消费 allow()）与 T-412（virtual 聚合）共享读面，跨包并行接口漂移 | 接口对齐经 ADR-0043 §织入点定案（接口驱动不跨包摸内部结构）；两票排不同波（B3 vs B2）合入序贯化；QA 中期（B7）双面联测兜底 |
| R4 | **httpapi 搜索面注册序贯**——T-415/T-417 同文件先后脚；T-410 mint 在 auth 域不冲突 | T-417 严格 dep T-415（B4→B5）；T-417 半程并行窗口（内核 T-411 出后理论可先行）默认不取——area 排他优先，登记为压缩选项；路由注册零冲突核查入两票 AC |
| R5 | **FE lane 一波一票 + 锚族纪律**——T-414→T-416→T-419→T-424 串行；T-420 FE ▶ 小腿（RepositoriesPage）与 T-424 共写面 | T-420 小腿与 T-419 文件不相交（conductor 裁量并行）但与 T-424 先后脚（B6→B8 天然错开）；锚族铁门槛逐票 AC：smu/search 既有锚零改名 + 新锚入册 + tree-empty-virtual 退役留痕（断言反转②锚册）+ 四闸门 + axe + 服务端 diff=0 |
| R6 | **资源门与满载回归**——K63 三件门 + 8 路混合零 5xx + 万节点 P95 门槛（NFR-P67）；busy 票 24 路清零与写路径专项 | 语料夹具 = M10 属性夹具扩展（多仓/多深度/多属性——PRD §1.3 QA 面既定）；T-413（门）与 T-423（busy）分置 B3/B8 与满载同场；T-421 中期 + T-430 终验双复核；用户实例数据副本 top-level 冒烟 <2s（沙箱复现纪律） |
| R7 | **复制包 B 双源规格置信度**——t226 entitlement 锁不可活体（T-402a 反面教训），wire 细节单文档源 | T-418 以官方 REST 文档为主源（webhook.md 先例）+ T-402a R 系已核形态（列表列/▶）补白；置信度逐条标注；outbox 引擎 diff=0 为 T-420 硬 AC（复用非重构）；Q5 已终裁零 cron 工作 |
| R8 | **条件票与余量触发态**——T-431（Q6 已裁开）/ K65（T-417 票内）/ Q4 实现段 / Q7 by-digest | T-431 随时插空（避开 T-412 波与 FE 主轴票）非 DoD，未插空 BOARD 留痕；K65 未触发维持 M16 登记；Q4/Q7 终裁触发现场立票（M14 D3 不占号先例）；全触发不增波（插空消化） |
| R9 | **主轴六波串行链工期**——B0~B5 为决定性路径，任一票回炉平移尾部三轮 | 压缩选项两处 conductor 裁量：① T-413 并入 T-411（ACL+门与内核合票省一波——粒度变大，仅在 B0 延期时取）；② mint 后移、T-412 前移 B1（若 T-408 早成）；qa 打回 ≥3 次的票按 SPRINT-LOOP 特殊情形回炉评估 |
| R10 | **断言反转三处归属审计**——SR-03/04（T-417）/ tree-empty-virtual（T-416）/ mint（T-410）+ K64 潜在第四处 | 三处预归属豁免票在案（PRD §5.4）；T-421 中期预核实现值 + T-430 终验归属审计 100%（L32）；K64 翻转断言更新归 T-417 票内留痕；§5.7 全景表逐行核对防未列端点误开 |

## 5. 首派建议（B0/B1）

- **B0（即派，conductor 已定双锚并行）**：**T-407**（reverse-engineer——aql.md 规格票；派单附 T-381 §0 容器恢复动作链 + INC-1 差集法只读纪律 + webhook.md 体例索引 + 132.4 四项口径归一清单）+ **T-408**（architect——ADR-0043；派单附 PRD §1.3 前置产物②全项 + T-392 WriteTimeout 登记原文 + nodes 列面/node_props 索引 architecture §15.3 引用 + 与 T-407 的 132.4④ 软协作注记）。
- **B1（L20 就绪度确认 + ADR Accepted 后开）**：**T-409**（dev-go-core——语言前端；派单附 aql.md 文法/字段/操作符定案节 + ADR-0043 包边界）+ **T-410**（mint 插空——auth-model 3.1 逐字文案 + T-386 漂移登记原文）。
- 插空候选（全程）：T-431（任意空位——已裁开）/ T-425（P2 评估票可前移出 Q4 材料，越早 PM 裁定越从容——不阻塞任何人）/ T-428（零依赖文面）。
- 派单纪律（全程）：no-git 条款维持；行为面票逐张附「dep 规格票结论或降级附注」；查询面票（T-413/415/417）逐张附「ACL 零泄漏探针 + 诚实拒绝（400 domain-not-supported）+ 结果上限 K63」三件套条款；FE 票逐张附「锚族冻结 + 新锚入册 + 四闸门 + axe + 服务端 diff=0」四件套；T-420 附「outbox 引擎 diff=0 红线」；T-417 附「K65 余量条款——非 DoD 留痕」。

## 6. 估票对比与取舍留痕（对 PRD §1.3 分票提示逐条）

| PRD 提示 | 拆票处置 | 取舍理由 |
|---|---|---|
| FR-132 拆 1~2 票 | **1 票**（T-407 含活体核验腿 + 口径归一） | PRD 明示「规格票 + 活体核验腿可同票」；核验结论→口径回写强耦合，一票自闭环（M14 T-381 同构） |
| FR-133 拆 2~3 票 | **4 票**（语言前端 / 执行内核 / ACL+资源门 / 端点面） | conductor 拆票指令明列「引擎内核/端点面/ACL+资源门」四分 + 语言前端独立（lexer/parser 单票粒度——合入内核则单票过重）；串行链每票可独立 review/qa，qa 打回局部化 |
| FR-134 拆 1~2 票 | **1 票**（三端点同票 + K64 + K65 余量条款票内） | 三端点同引擎同 httpapi 面拆开徒增先后脚；K64/K65 均其收尾面（PRD 134.5/134.6 归 FR-134） |
| FR-135 拆 1~2 票 | **2 票**（列选器三页+member-pop / AQL 模式） | 前者零 BE 依赖可 B3 早落（axe 基线干净），后者 dep 端点稳定——解锁时序天然两段（M14 T-391/T-382 同构） |
| FR-136 拆 1~2 票 | **2 票**（service 聚合 / FE 树消费） | BE（internal/repo）与 FE（web/src artifacts）异 area 异角色并行安全；断言反转② FE 腿含锚册纪律须 dev-frontend 承载 |
| FR-137 一票 | **1 票**（T-425） | 照 PRD；reverse 主笔 + adapter 会签（PRD §4.6 角色既定） |
| FR-138 拆 2~3 票 | **3 票**（T-418 规格前置 + T-420 Replicate Now + T-422 Test·封锁合票） | 规格独立票见 §7-1；Test 与封锁均 P2 配置域小面共用增量段与 FE 配置面，合票省一波（PRD 2~3 线内）；Replicate Now P1 独立（体量 + FE 接线腿） |
| FR-139 拆 2 票 | **2 票**（mint / busy） | 照 PRD；两者优先级异（P1/P2）且 area 异（httpapi auth vs storage/remote） |
| FR-140 拆 2 票 | **2 票**（L2 快捷+纪律成文合票 / 文面回写簇） | PRD 提示两票未提 L2 归属——L2 与纪律成文均 dev-frontend web 域同票（M14 T-391「FE 债 + README 注记」合票先例）；文面回写 tech-writer+ux 异角色独立 |
| QA 两票 / tech-writer 一~两票 / release 一票 / PM 一票 | T-421+T-430 / **T-426 一票两腿** / T-429 / T-427 | tech-writer 取一票：两腿解锁时序差由票内先后笔吸收（腿① B6 后即可动笔），合票省一 lane 且票数回线内上沿（25） |
| 条件票 slot | T-431 占号（Q6 已裁开）；K65 票内条款；Q4 实现段/Q7 不占号 | Q6 conductor 已裁开=实体条件票（插空形态）；K65 为 T-417 顺车条款（PRD「余量顺车」本义）；Q4/Q7 终裁未发生不立实体（M14 D3 先例） |

## 7. 歧义与口径登记（不阻塞拆票，交 conductor/PM）

1. **replication.md 增量段归属**：PRD §2.1「随 FR-138 票」vs §1.3「FR-138 dep replication.md 增量段」。处置：**独立前置规格票 T-418**（dep 链口径 + clean-room 规格先行纪律 + M14 T-393/T-394 先例）；PM 下版统一措辞。
2. **FR-140 拆票口径**：PRD §1.3「拆 2 票（测试基建纪律 / 文面回写簇）」未提 140.1 L2 快捷归属。处置：L2 与纪律成文合票（dev-frontend web 域同源），文面回写独立——总数仍 2 票。
3. **search 页列选器归属**：PRD 135.2 列三页推广、135.1 又含「列选器接入」。处置：三页列选器统一归 T-414（T-387 spec 形态复用一票到位），T-419 消费既有列框架——避免两票共写 search 页。
4. **mint/busy 优先级双值**：PRD §2.1 行 G 标「P1 / P2」复合。处置：139.1 mint = P1（B1 插空）、139.2 busy = P2（B8）——DoD#1 分项口径。
5. **ADR-0043 与 aql.md 并行的软协作缝**：132.4④ 基座映射表由 aql.md 产出、ADR 消费。处置：ADR 以 architecture §15.3 暂行映射起草，Accepted 前与 aql.md 映射表对齐（T-408 AC3）——不设硬 dep（保 B0 双票并行，conductor 指令）。
6. **T-420 FE ▶ 小腿并行口径**：B6 与 T-419（web/src/pages/search）文件不相交但同 FE 域。处置：票面注明 conductor 裁量并行或先后脚；与 T-424（RepositoriesPage 共写）严格先后脚（B6→B8）。
