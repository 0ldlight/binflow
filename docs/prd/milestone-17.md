# PRD — M17 产品域扩张专程（Q6 两程终裁之后程）：Build-info 域主程 + Release Bundle 最小面 + 洞察报表（主轴候裁层）+ 「M16 未纳入项」滚程收纳（确定层）

> **PRD 状态：v1.0 立项骨架稿（2026-09-06 PM 起草——m16-done 收口窗同日开启〔auto-next-milestone〕，候 conductor 审定 + 用户裁定清单 Q0~Q12 终裁）**。定程依据：M16 Q6/Q13 两程结构终裁（2026-09-02 用户裁「产品域全部进、按两程承接」——**M17 = 产品域扩张专程**；须 PRODUCT.md 修订 + ADR 群）+ ROADMAP M17 预立项段（conductor 2026-09-02 立段，五要素全量吸收）+ BOARD m16-done 总账（滚程五项：BE round-trip 缺口 / B-3.2 / NuGet symbol 七承 / webhook 触发源 / build-info dormant→wired）+ ROADMAP「M16 未纳入项」（T-460 备稿，随本立项稿启用）。
> **两层结构（本稿最高位范围裁定）**：**主轴候裁层**（FR-152~155 产品域四主目——Q6 终裁已裁「进」，但**PRODUCT.md「明确不做」五条仍未修订〔用户动作未发生〕**，故候 **Q0 修订终裁 + Q1 定容终裁**后方可拆票）+ **确定层**（FR-156~161 滚程收纳——无产品边界翻案面，可先行）。**Xray 集成面维持唯一不做**（intake ⑤ 明示排除，Q6 终裁不改变）。
> **骨架级声明**：本稿为立项骨架——行为规格要点与 AC 骨架给到「可拆票、可裁定」粒度；端点子集边界候规格票（build-info.md / release-bundle.md，官方文档为唯一行为基准），数据模型与架构决策候 ADR 群（ADR-0045/0046 占位），实现细节候 tech-lead 拆票。**零票号新散布**——新事项一律 FR/LC/L/K/Q 编号承载。

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/milestone-17.md` |
| 里程碑 | M17 — 产品域扩张专程（两程结构之后程）——主轴候裁层（Build-info 域主程 P0 候定 / Release Bundle 最小面 / 洞察报表 / Federation·Lifecycles 候裁）+ 确定层（BE 表单域承接与权限域收口 / 契约漂移勘误族 / cron·i18n 遗留 / webhook 域二程 + Replay 并轨 / 工程债包 / 同型推广候裁）+ 前置锚（PRODUCT.md 修订稿 + 规格票 + ADR 群）+ 条件票池 |
| 状态 | v1.0 立项骨架稿：FR-151~FR-161 十一条（前置锚 1 / 主轴候裁层 4 / 确定层 6）；契约矩阵 LC-99~LC-114 估 16 条（**A 12 / C 2 / 候裁 2**——候裁终裁后归位）；L49~L63 验收线骨架；K74~K76 候校准；**用户裁定清单 Q0~Q12（必答前置 Q0/Q1，余按建议裁点）**；票量级估 **32~40 票**（PM 定容建议态——FR-155 裁出 + HA/全量 REST 不做；四域全进则 40~48 超带，候 Q1 终裁） |
| 上游依据 | PRODUCT.md（「明确不做」五条——Q0 修订对象；第三行 LDAP/SAML/OIDC 已被 M6〔FR-54~56〕/M11〔FR-92〕事实推翻，文档滞后修正项）、ROADMAP M17 预立项段（转正——主目/随域联动/追加候补/维持不做/容量注五要素）、ROADMAP「M16 未纳入项」（T-460 起草 + m16-done 收口窗启用笔）、ROADMAP「M15 未纳入项」（续滚工程债八项）、BOARD.md m16-done 总账（挂账三项 + 滚程五项）、docs/prd/milestone-16.md（§0.2/§0.3 两程结构 / §2.2 Non-goals 滚程留痕 / §7 v1.2 收口总账〔勘误五条 + 移交六项〕/ K68 write 别名「M17 别名移除评估」登记 / K73 repoLayoutRef「候 M16 未纳入项 BE 承接票立项定案」）、docs/prd/milestone-15.md（AQL 九域分阶段——build/module/dependency/promotion/releasebundle/sensitive 域 dep 域本体；§5.7 全景表对账基线）、reports/m16-parity-audit-material.md（A1 域清单 66 项〔xray_tied 已剔〕+ 随域联动清单 + D3 依赖树前置注）、ADR-0041（webhook 36 型 wired/dormant 现行态 + 决策 7 翻转路径——dormant→wired schema 零变化）、ADR-0044（K69 统计基建单源契约——洞察报表 dep；调度域三消费面——gc-cron-gap 承接）、ADR-0033（addon 注册表——Builds/Bundles 档位核验联动）、ADR-0026（RBAC 角色闭集——B-1.7 增补评审前置）、docs/reverse/webhook.md + aql.md（「官方文档为唯一行为基准」先例——规格票取证路径）、reports/agents/T-460.md（B 49 行终态 + Q10/Q11/Q12 归位）、T-466/T-465 日志（m16-done 证据链 + 收口清单） |
| 下游消费者 | conductor（审定窗 + Q0~Q12 终裁组织 + tag m17-done）、tech-lead（拆票——票号自 M16 收口后顺延；**主轴候裁层候 Q0/Q1 终裁后拆**，确定层可先行；宽度 ≤2 内建）、reverse-engineer（build-info.md / release-bundle.md 规格票新建 + aql.md build 域增量段 + 档位核验腿）、architect（**ADR-0045 Build-info 域 / ADR-0046 Release Bundle 域占位** + 洞察聚合层会签 + ADR-0026 增补评审〔Q11〕）、dev-go-core（build 数据模型与 REST / 聚合快照 / 契约勘误 / webhook 二程——主 lane）、dev-registry-adapter（docker promote / 同型推广矩阵腿）、dev-frontend（Builds 页 / Release Bundle 面 / 洞察图表 / B-3.2 与 Last Login 列收尾）、qa-engineer（L49~L63 + 缺位解除归位审计 + M1~M16 双形态全量回归）、tech-writer（三新域指南 + 缺位解除公告）、release-engineer（烟测 + UAT） |

---

## 0. 范围定界（置于最前——沿 M16 §0 形制）

### 0.1 两层结构（本程范围总裁定）

| 层 | 内涵 | 拆票条件 | 工程形态 |
|---|---|---|---|
| **主轴候裁层**（FR-152~155） | 产品域四主目：Build-info 域 / Release Bundle / 洞察报表 / Federation·Lifecycles——**Q6 终裁已裁「进」M17**，但触及 PRODUCT.md「明确不做」五条中的三条（HA·联邦复制 / 洞察报表 / 全量 REST 关联面） | **候 Q0（PRODUCT.md 修订稿终裁——用户动作）+ Q1（定容与分期）**；ADR-0045/0046 Accepted 前置 | 候裁层内含数据模型 + REST 族 + FE 面 + 随域联动解锁（AQL 域 / webhook 事件 / Module ID / Any Distribution / Builds 页签） |
| **确定层**（FR-156~161） | 「M16 未纳入项」滚程收纳：BE 表单域承接 / 契约漂移勘误 / cron·i18n 遗留 / webhook 域二程 + Replay / 工程债包 / 同型推广（候容量）——**零产品边界翻案面** | 无前置——审定后即可拆票先行 | BE 小域为主 + FE 收尾腿；与主轴 lane 错峰 |

**常设纪律**：主轴候裁层在 Q0/Q1 终裁前**不派票、不写实现**（翻案纪律 §1.4 条款 1——PRODUCT.md 修订未落笔即边界未翻，PM 只出修订稿候裁）；确定层不候裁、不被主轴挤占（滚程五项为 m16-done 总账点名移交，连续两程滚程即升级为债）。

### 0.2 前置门槛：PRODUCT.md「明确不做（第一版）」五条修订（Q0——用户动作）

五条现状与 PM 处置建议（修订稿全文随 FR-151.1 拟出，候 Q0 终裁后由用户/conductor 落笔 PRODUCT.md——PM 不越权改产品宪法）：

| # | 现行文本 | 事实状态 | PM 修订建议（候裁） |
|---|---|---|---|
| 1 | 不做 HA 集群与联邦复制（active-active） | 复制已实现（M6 FR-57~60 push/pull + M15 包 B）；Federation（镜像联邦）未做；HA 本体未做 | 拆行改写：复制/推送分发维持既有「已实现」事实；**Federation 候 Q1 裁程；HA 本体建议维持不做**（候 Q3——PM 倾向 M18+ 单列专程候选） |
| 2 | 不做 Xray 式漏洞扫描 / 许可证合规平台 | 维持 | **原文维持**（intake ⑤ 唯一排除项——license 识别 licences.xml 91 模式等 xray_tied 族连带维持，Q9 关联） |
| 3 | 不做 LDAP / SAML / OIDC | **已被事实推翻**：M6 FR-54~56（OIDC+LDAP）+ M11 FR-92（认证配置前端化含 SAML） | 滞后修正：改写为「已实现（M6 起）」或移除该行——文档与现实对齐（非翻案，是补账） |
| 4 | 不做 UI 高级分析与洞察报表 | 未做；Q6 终裁已裁「进」M17 | 解禁改写：洞察报表/趋势分析进产品范围（M17 承载，FR-154） |
| 5 | 不做 Artifactory 全量 REST 兼容（只做高频子集） | 维持（M15 AQL 只承诺语言子集等既定落点） | **PM 倾向原文维持**（子集承诺系工程纪律——全量兼容面 = 无限回归矩阵；候 Q4 终裁确认） |

### 0.3 维持不做（本程 Non-goals 宪法级）

**Xray 集成面**（漏洞扫描 / 合规平台 / license 识别 xray_tied 族）= intake ⑤ 明示唯一排除，Q6 终裁不改变，本程零承载。连带：`/api/search/license` 腿无 license 识别数据源即缺位登记不伪造（Q9——dependency/buildArtifacts 两腿随 build-info 域做）。

### 0.4 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-09-06 | 初版立项骨架稿（候 conductor 审 + 用户裁定清单 Q0~Q12 终裁）：§0 两层范围定界 + PRODUCT.md 五条修订建议表（Q0）；FR-151~FR-161（前置锚 1 + 主轴候裁层 4 + 确定层 6）；契约矩阵 LC-99~LC-114 估 16 条（A 12 / C 2 / 候裁 2）；L49~L63 骨架；K74~K76；Q0~Q12（必答前置 Q0/Q1）；票量级估 32~40。随稿完成 ROADMAP 四处修订（当前里程碑头切 M17 / M16 章 m16-done 标记 / M17 预立项段转正为正式章 / 「M16 未纳入项」启用笔三行增补）——PM 职责内，沿 M13~M16 先例 |

---

## 1. 背景与目标

### 1.1 背景（五股输入）

M16 以 `m16-done`（2026-09-06 13:0x，35/35 + T-466 P0 终验 PASS with notes）收官：全前端 parity（B 矩阵 49 行零无主）+ i18n 双语 + cron 调度域 + 远端浏览 + AQL 副线全栈闭合；挂账三项（CircleCI re-run 补证 / DNS+SG 用户前置 / M17 预立项窗开）。M17 面对五股输入：

1. **Q6 两程终裁（主轴定音）**：用户 2026-09-02 裁「产品域全部进、按两程承接」——M16 = 交互 parity + i18n + cron；**M17 = 产品域扩张专程**：Builds/Build-info 域、Release Bundle、洞察报表、Federation/Lifecycles（追加候补：Repository Path Map 管理面 / HA / 全量 REST 退让边界）。前置 = PRODUCT.md 修订（用户动作）+ ADR 群——**修订至今未发生**（T-460 M17 衔接核对在案），故主轴候裁。
2. **m16-done 总账滚程五项（确定层骨干）**：BE round-trip 缺口（configJSON 四域）/ B-3.2 初始态 / NuGet symbol 七承（条件池——用户点名即翻）/ webhook 触发源（M16 Q11 裁点滚 M17+，与 build-info dormant→wired 联动）/ build-info dormant→wired。
3. **「M16 未纳入项」启用（候选池收编）**：T-460 备稿随本立项稿启用——parity 残留（B-1.5/B-3.12 承接 / B-2.16 通配桶 / B-1.7 闭集增补 / B-3.18~19 列集）、契约漂移登记族七条、cron/i18n 遗留、工程票外系收尾残留——PM 逐条归入确定层 FR 或条件池（§2.1 留痕，勿重复立项）。
4. **M15 续滚工程债八项**：aql.md V-a~V-h 活体 / 相对时间 `"1d"` 空格冲突 / compact 非空行体 / 429 Retry-After 不上 UI / virtual echo config blob / 大仓 limit 化 / eslint ratchet 37 / vite 8 + MUI 段二——连续两程滚程，本程打包承载。
5. **A1 域清单与随域联动（不发明范围的上界）**：审计 A1（66 项，xray_tied 已剔）+ 预立项段联动面（AQL 六域中的 build/module/dependency/promotion/releasebundle/sensitive、`/api/search/buildArtifacts·dependency`、D3 依赖树〔须先出后端依赖解析域票〕、Module ID、Any Distribution、Repository Path Map）——**全部随域本体落地解锁，不提前不伪造**（M16「无数据源不伪造」纪律延续）。

**不贪多**：M17 以「一个前置锚（PRODUCT.md 修订 + 规格票 + ADR 群）+ 主轴候裁层（PM 定容建议 = Build-info 主程 P0 + Release Bundle 最小面 P1 + 洞察报表 P1；Federation/Lifecycles 候 Q1 裁程——PM 倾向滚 M18）+ 确定层（滚程收纳六 FR）+ 条件票池」为形态；四域全进则超 30~40 票带宽，Q1 必答。

### 1.2 M17 目标与量化门槛（骨架）

> 一句话：把 Q6 终裁的产品域从「预立项登记」变成**可验收的域本体**——Build-info 域全链（真实 CI 上传 build → promote → docker 镜像随迁 → webhook 事件 → AQL 可查）走通，Release Bundle 最小面与 Any Distribution 预置落地，洞察报表以统计单源出趋势；同时把 m16-done 总账滚程五项与两程滚程债**零再滚**收口。

| 指标 | M17 门槛（骨架——细则候规格票/ADR 定案后补门） | 来源 |
|---|---|---|
| 前置锚 | PRODUCT.md 修订稿 Q0 终裁落笔（五条处置留痕）+ build-info.md / release-bundle.md 规格票（官方文档锚 + 活体核验 + 档位核验）+ ADR-0045/0046 Accepted | FR-151 |
| Build-info 全链 | curl PUT/append/查询/promote/retention 全绿 + **docker promote 腿**（build 关联镜像跨仓晋升后 `docker pull` 新仓成功——删缓存后复拉仍成功）+ jf CLI 条件腿（环境可得时 `jf` build 上传/promote 真实走通） | FR-152 |
| webhook wired | build 域事件 dormant→wired：真实消费者（local receiver）收到 payload + 签名验证通过；36 型注册表 wired/dormant 标注与实际一致（零伪造） | FR-152/159 |
| AQL build 域 | `builds.find` 类查询绿 + dependency include + ACL 越权探针零泄漏；未实现域（promotion/releasebundle/sensitive）400 诚实拒绝零伪空集 | FR-152 |
| Release Bundle | bundle 创建/查询 curl 全链 + **Any Distribution 预置**在权限两步弹窗可选且生效（探针） | FR-153 |
| 洞察报表 | 图表数据与 per-node 统计**单源一致**（与 FileInfo `downloads` 对账）；空数据空态；快照可测加速（手动触发腿） | FR-154 |
| BE 承接 round-trip | configJSON 四域 PUT → GET 回显零丢失（M16 T-439 tripwire 翻红转绿）+ blackedOut=true 拒写行为断言 + Stage 域承接 | FR-156 |
| 契约勘误 | 勘误族逐条 curl 归位（url 前缀 / downloadUri / System Logs 端点 / useAsync 双计清零）+ 文档反转登记（T-458「如实注记」解除） | FR-157 |
| 回归与缺位解除 | M1~M16 P0 双形态全量回归零回退；**四缺位解除归位审计**（Module ID / Any Distribution / Builds 页签 / System Logs 注记）；断言反转新增项 100% M17 豁免票 | §5.4 / DoD |
| 资源与预算门 | footprint ≤100MB / check-size ≤120MB / 冷启动 <2s 三连维持；SPA 每票增量 ≤10KB（Builds/图表 FE 面大——累计趋势观察） | §6.2 |

### 1.3 上游依赖与并行关系（含分票提示）

- **前置产物（拆票前）**：① Q0/Q1 终裁包（conductor 组织——**必答前置**）；② PRODUCT.md 修订稿（FR-151.1——PM 拟稿，终裁后落笔）；③ 规格票两份（build-info.md / release-bundle.md——reverse-engineer；官方文档为唯一行为基准 + t226〔OSS 7.84.10〕与 7.161 活体核验 + Builds/Bundles 档位核验〔ADR-0033 槽联动〕）；④ **ADR-0045（Build-info 域：数据模型/REST 子集/权限面/与 webhook-AQL 联动）+ ADR-0046（Release Bundle 域：模型/端点族/深度边界——Q2 终裁输入）**；⑤ 洞察聚合层架构会签（时序快照模型——归 architect，不单列 ADR 亦须会签留痕）；⑥ 7.161 参照容器修复（Q10——conductor 决策项移入：M17 活体参照基线依赖）。
- **依赖声明**：FR-152 dep FR-151 规格票 + ADR-0045 + Q0/Q1 终裁；FR-153 dep Q2 深度终裁 + ADR-0046 + 规格票（Any Distribution 预置腿 dep FR-156 通配桶同场裁）；FR-154 dep Q0（第四行解禁）+ M16 per-node 统计基建〔已落〕+ 聚合会签；FR-155 dep Q1/Q8；FR-159 build 腿与 FR-152 联动（dormant→wired）、裁剪出口 dep Q5 终裁；FR-161 dep Q6 容量终裁。
- **依赖序**：FR-151 → FR-152（BE 模型+REST → append/promotion/retention → docker promote → webhook wired → AQL 域 → FE Builds 页）→ FR-153（Any Distribution 腿与 FR-156 同场）；FR-154（聚合层 → 图表）与主轴 BE 错峰并行；**确定层不候裁**：FR-156/157/158 可 B1 起步，FR-159 build 腿候 FR-152、Replay 腿可先行，FR-160 全程尾波弹性。
- **分票提示（宽度 ≤2 内建）与票量级**：FR-151 拆 ~3 票（修订稿随 PM 裁定票 + 规格票 2）；FR-152 拆 8~10 票；FR-153 拆 4~5 票；FR-154 拆 3~4 票；FR-155 = 0（PM 建议态）或 5~8（裁进）；FR-156 拆 3~4 票；FR-157 拆 1~2 票；FR-158 拆 1~2 票；FR-159 拆 2~3 票；FR-160 拆 3~4 票；FR-161 拆 1~2 票；QA 2 / tech-writer 1~2 / release 1 / PM 1。**PM 定容建议态合计 32~40 票**（FR-155 裁出 + HA/全量 REST 不做）；若 Q1 裁四域全进 → 40~48 超带，建议拆程或裁确定层尾部（FR-160/161 降级条件票）。
- **area 错峰**：主轴 BE = `internal/build`（新包定名归 ADR-0045）+ `internal/httpapi` + `internal/search`（AQL build 域增量）；FR-153 = `internal/bundle`（新包定名归 ADR-0046）；FR-154 = 聚合层（新包或 storage 增量——归会签）；确定层 = `internal/httpapi` + `internal/repo` + `web/src`（与主轴 BE 不同文件族错峰，httpapi 共享面由 tech-lead 排波次）；FE = Builds 页 / bundle 面 / 图表 / 收尾腿四族分波。
- **QA 并行面**：build 夹具（CI 风格多 build 多 promotion 数据腿）；docker promote 双仓夹具；洞察多日快照测试加速腿（手动触发等价）；Replay/死信重放消费者夹具（M13 dogfood 形态复用）；十协议矩阵复跑复用 M16 基建。

### 1.4 全程工作方式条款（M11~M16 制度延续 + M17 特化）

1. **翻案纪律（宪法级）**：PRODUCT.md 修订未落笔（Q0 未终裁）即边界未翻——**主轴候裁层不派票、不写实现、不设回归断言**；修订落笔 = 契约变更双留痕（PRODUCT.md 修订记录 + 本 PRD §7 Q0 出口）。「连续两程滚程即升级为债」——滚程项收纳进确定层后不再无界续滚，做不完走「M17 未纳入项」显式登记 + 归属审计。
2. **无数据源不伪造（M16 条款延续）**：AQL promotion/releasebundle/sensitive 域、Module ID、Any Distribution、Builds 页签——随域本体落地解锁，不提前渲染；`/api/search/license` 无 license 识别（xray_tied 维持不做）即缺位登记（Q9）。
3. **clean-room 红线（A0 永久）**：Build-info/Release Bundle 系有官方文档的公开 REST 面——**官方文档为唯一行为基准**（webhook.md/aql.md 先例），reverse-src 只补文档空白；UI 像素复刻/logo 矢量源零触碰。
4. **低置信不进断言**：本稿端点形态均为骨架示意——逐端点置信度标注与子集边界冻结归规格票；规格票回写前，PRD 暂行值不得进 Playwright/curl 硬断言（K74~K76 承载）。效力序：用户裁决（Q 表）> ADR/规格票 > 本 PRD 暂行值。
5. **ACL 零泄漏（T-92 血统）**：build/bundle 数据权限面与既有内容面同一 `allow()` 源——越权探针为每票硬 AC（build 数据系新泄漏面，权限模型归 ADR-0045 定案）。
6. **流程条款**：新端点走 PM FR + ADR 流程（本程新端点族 = Build-info REST + bundle 端点族 + Replay/outbox 行级 REST + System Logs 端点 + 聚合查询面——均 §5.3 矩阵与规格票承载，零私加）；拆票宽度 ≤2；FE 票四闸门 + axe 双主题双 locale 全绿；每票 AC 附可执行验收命令。

---

## 2. 范围

### 2.1 In scope

| # | 来源（终裁/总账/登记） | 本 PRD 功能需求 | 优先级 |
|---|---|---|---|
| A | Q0/Q1 必答前置 + ADR 群 + 规格票 | FR-151（前置锚：PRODUCT.md 修订稿候裁 + build-info.md/release-bundle.md 规格票 + ADR-0045/0046 + 活体基线修复 Q10） | P0（前置锚） |
| B | Q6 终裁主目① + 滚程「build-info dormant→wired」+ A1 联动 | FR-152（Build-info 域主程：数据模型 + REST 全链 + docker promote + webhook wired + AQL build/module/dependency 域 + `/api/search/buildArtifacts·dependency` + FE Builds 页 + Module ID 解锁） | P0（候 Q0/Q1） |
| C | Q6 终裁主目② + M16 B-2.16 缺位登记 | FR-153（Release Bundle 最小面：模型 + 创建/查询端点族 + Any Distribution 预置解锁——深度候 Q2） | P1（候 Q0~Q2） |
| D | Q6 终裁主目③ + ADR-0044 K69 统计单源 | FR-154（洞察报表：聚合快照层 + dashboard 图表族——dep 统计基建单源；既有 dashboard 去重率口径不变） | P1（候 Q0） |
| E | Q6 终裁主目④（追加候补族） | FR-155（Federation/Lifecycles/Repository Path Map——**候 Q1/Q8 裁程，PM 倾向滚 M18 单列专程**；裁进则最小面承载） | 候裁 |
| F | 滚程五项①② + M16 未纳入项 parity 残留 | FR-156（BE 表单域承接与权限域收口：configJSON 四域 round-trip + 行为联动 + Stage 域 + K73 落定 + 通配桶 + B-1.7 评审腿 + B-3.2 + Users Last Login 列） | P1（确定层，先行） |
| G | M16 未纳入项契约漂移登记族 | FR-157（契约漂移勘误族：url 前缀 / downloadUri / System Logs 端点 / ?properties 注记 / useAsync 双计） | P1（确定层） |
| H | M16 未纳入项 cron/i18n 遗留 | FR-158（gc-cron-gap 三载体 + TTL wire 可调 + 切换器位置复核 + faq 两问） | P2（确定层） |
| I | 滚程五项④⑤（webhook 触发源 + Replay） | FR-159（webhook 域二程 + Replay/outbox 行级 REST 并轨：dormant→wired 翻转 + 裁剪出口〔候 Q5〕+ 死信重放 REST） | P1（确定层） |
| J | M15 续滚工程债八项 + T-444/T-452 等遗留 | FR-160（工程债与续滚收尾包：八项 + Annotate 别名移除评估 + dry-run CLI + QRL 三遗留 + migration 011 flake + X-Explode staging + migrate CLI 低危 + 文档漂移四条核对） | P2（确定层，尾波弹性） |
| K | M16 容量注滚程项 | FR-161（virtual 成员同型全包型推广：13 包型 × 三 rclass 全量回归矩阵——候 Q6 容量裁定） | 候裁（确定层弹性位） |
| L | 条件池裁量（不设 FR） | 条件票池：NuGet symbol server **七承**（用户点名即翻）/ E7 toast（候用户信号）/ Tokens 字段核验（候活体源）/ license 公钥 config 覆盖 ADR（候立项）/ B-2.8 有效权限 AG 网格（候用户信号） | —（未触发不构成 DoD 缺口，BOARD 留痕） |
| M | 追加候补（A1 同族，预立项段列定候定容） | **HA 本体（候 Q3——PM 倾向 M18+ 单列专程）/ Artifactory 全量 REST 兼容退让边界（候 Q4——PM 倾向维持子集承诺）/ Go·Terraform·GitLFS·AI-ML 包型域（候 Q7——PM 倾向滚 M18+）** | —（Q 表承载，终裁前零排期） |

### 2.2 Non-goals — M17 明确不做

| 不做项 | 隔离边界 / 去向（留痕） |
|---|---|
| **Xray 集成面**（漏洞扫描 / 合规平台 / license 识别 licences.xml 91 模式） | 宪法级维持（§0.3）——intake ⑤ 唯一排除；`/api/search/license` 缺位登记候 Q9 |
| **Distribution 服务对接全量面**（Release Bundle v2 signing / 跨实例分发编排） | 越单仓产品边界风险（类 Xray 集成面）——Release Bundle 深度候 Q2 终裁，PM 倾向最小面（local bundle + 查询 + Any Distribution 预置） |
| **HA 高可用本体** | 候 Q3——引擎级架构决策 + 容量/风险双高，PM 倾向 M18+ 单列专程（须独立 ADR 群 + 部署矩阵全量回归） |
| **Artifactory 全量 REST 承诺退让** | 候 Q4——PM 倾向维持高频子集承诺（工程纪律；全量兼容面 = 无限回归矩阵） |
| **Go 深化 / Terraform / GitLFS / AI-ML 13 型包型域** | 候 Q7——包型本体扩张各自专程，M17 容量被产品域占满；滚 M18+ |
| **A0 永久红线族** | clean-room 三线 + 商标复查族——零触碰，仅登记不议 |
| **协议物理不可行面 / deb bz2 / Smart Searches / by-digest 强刷 / Tokens 字段核验排期** | 沿 M16 §2.2 维持（翻案语境不覆盖物理不可行；bz2 ruled-out 唯一通道 = 纯 Go bzip2 写入器路径） |
| **主轴候裁层的提前实现** | Q0/Q1 终裁前不派票不写实现（§1.4 条款 1）——含 FE 提前渲染 Module ID/Any Distribution 等联动面 |

---

## 3. 用户与场景（M17 视角，骨架）

- **场景 A（CI/CD 工程师，Build-info）**：流水线把 build 元数据（模块/依赖/环境）推给 BinFlow，jf/curl 上传 → 构建「工件 ↔ 镜像 ↔ 依赖」关联；一键 promote（staging→release 仓）后 docker 镜像随迁——`docker pull` release 仓即得。
- **场景 B（发布管理员，Release Bundle）**：把一批已验证制品打成 bundle 记录（版本化、可查询）；权限面预置 Any Distribution 桶可授予发布组。
- **场景 C（平台负责人，洞察）**：dashboard 看下载趋势/存储增长/Top 仓——冷热判断与容量规划的数据地基（与制品详情 Downloads 同源）。
- **场景 D（webhook 消费者/运维）**：CI 推 build 后 Jenkins/消费者收到 build 事件；死信可经 Replay REST 重放；触发源注册表如实标注 wired/dormant。
- **场景 E（仓管理员，确定层收口）**：blackedOut/repoLayoutRef 等字段提交回显不丢；System Logs 页从「审计承载」升为进程日志真身；纯浏览不再虚增下载计数。

---

## 4. 功能需求（骨架——行为规格要点 + AC 骨架；细则候规格票/ADR/拆票）

约定：`BASE=http://127.0.0.1:8080`；`ADMIN=admin:password`；FE 验收 = Playwright（`web/e2e/m17/`）+ 四闸门 + axe 双主题双 locale；BE 验收 = curl 真实命令 + 真实客户端（docker/jf 条件腿）；**端点形态均为骨架示意——子集边界与逐端点置信度归规格票冻结（K74~K76），冻结前不进硬断言**。

### 4.1 前置锚（P0）

#### FR-151 PRODUCT.md 修订稿 + 规格票两份 + ADR 群 + 活体基线（PM + reverse-engineer + architect + conductor〔Q10〕）

**用户故事**：作为本团队，产品域开工前宪法对齐——PRODUCT.md 五条修订（Q0 终裁）、行为规格冻结（官方文档锚）、架构定案（ADR 群），三缺一不拆主轴票。

行为规格（骨架）：
- **151.1 PRODUCT.md 修订稿**：§0.2 五条处置表拟出全文（含第三行滞后补账），候 Q0 终裁后落笔（修订记录留痕——**翻案双留痕纪律**）。
- **151.2 规格票两份**：build-info.md（端点族/数据模型字段/append 合并语义/promotion 状态机/retention 语义/权限面/与 webhook·AQL 联动清单 + 档位核验〔Builds 面 OSS 档可用性活体核验〕）+ release-bundle.md（端点族/模型/深度边界选项〔Q2 材料〕/Any Distribution 预置语义 + 档位核验〔Release Bundle 商业档位——ADR-0033 槽联动〕）。
- **151.3 ADR-0045/0046 占位**：Build-info 域（`internal/build` 包边界/数据模型/权限面〔build 数据 ACL 模型〕/REST 子集/与 outbox-AQL 织入）+ Release Bundle 域（模型/端点族/最小面 vs 全量面边界）——主轴拆票前 Accepted。
- **151.4 活体基线修复（Q10）**：7.161 参照容器双损坏（pro router 不就绪 / oss 进程死）修复归 conductor 决策——M17 规格票活体核验腿的基线依赖，未修复则规格票降级为 t226〔7.84.10〕单源 + 官方文档（留痕）。

验收标准（AC 骨架）：
- **AC1**：Q0 终裁 → PRODUCT.md 修订落笔（五条处置与 §0.2 表一致，修订记录在案）；Q1 终裁 → 定容结论落 §7（裁出项去向留痕）。
- **AC2**：两份规格票落盘（逐端点置信度标注 + 子集边界表 + tech-lead 就绪度确认）；ADR-0045/0046 Accepted。
- **AC3**：活体基线处置结论在案（修复 / 降级单源留痕二选一——Q10 出口）。

### 4.2 主轴候裁层（候 Q0/Q1——PM 定容建议态：FR-152 P0 / FR-153~154 P1 / FR-155 候裁滚 M18）

#### FR-152 Build-info 域主程（internal/build 新包 + httpapi + search + web/src；P0 候定第一顺位）

**用户故事**：作为 CI/CD 工程师，流水线推 build 元数据到 BinFlow（curl/jf），工件-镜像-依赖关联可查；promote 一键把 build 及其镜像迁到 release 仓；webhook 消费者收到 build 事件；AQL 一句话查「某 build 的依赖里有没有某制品」。

行为规格（骨架——端点子集归 build-info.md 冻结）：
- **152.1 数据模型**：builds/modules/dependencies/promotions/properties 表族（ADR-0045 DDL）；与 nodes 关联（build ↔ 制品/镜像工件）。
- **152.2 REST 全链**：`PUT /api/build/<name>/<number>`（上传/段 append——分段合并不覆盖语义照规格票）+ 查询族（单 build/列表/最新）+ `POST /api/build/promote/<name>/<number>`（状态机 + 目标仓迁移 + docker promote 腿）+ retention（保留窗删除——形态归规格票）。
- **152.3 随域联动**：webhook build 域事件 dormant→wired（ADR-0041 schema 零变化——滚程五项兑现）；AQL build/module/dependency 域（aql.md 增量段冻结字段集）+ `/api/search/buildArtifacts` + `/api/search/dependency`；FE Builds 页 + 搜索范围页签 Builds 缺位解除（M15 既定 R2 缺位其一）；详情 Module ID 字段解锁（M16 stay-out 登记）。
- **152.4 权限面**：build 数据 ACL（模型归 ADR-0045——仓库级/独立 build 权限面二选一候 ADR）；promote 动词权限门。

AC 骨架：
- **AC1（REST 全链）**：`curl -X PUT -u $ADMIN "$BASE/api/build/myapp/1" -H 'Content-Type: application/json' -d @build.json` → 2xx；`curl -u $ADMIN "$BASE/api/build/myapp/1"` 回显模块/依赖段；再次 PUT 带 dependencies 段 → 合并不覆盖（append 语义断言）；列表/最新端点绿。
- **AC2（promote + docker）**：`curl -X POST -u $ADMIN "$BASE/api/build/promote/myapp/1" -d '{"status":"released","targetRepo":"rel-docker"}'` → 200 + 状态翻转 + audit 行；build 关联镜像晋升后 `docker pull $BASE/rel-docker/myapp:1` 成功且**删除本地缓存后复拉仍成功**；`curl -X DELETE`（retention）删保留窗外 build（形态照规格票）。
- **AC3（webhook wired）**：真实消费者（local receiver）收到 build 域事件 payload + HMAC 签名验证通过；注册表 wired/dormant 标注与实际一致（零伪造审计）。
- **AC4（AQL + ACL）**：`builds.find(...)` 类查询绿 + dependency include；未支持域 400 诚实拒绝；无权限用户查 build → 403/零出现探针（allow() 同源）。
- **AC5（FE + jf 条件腿）**：Playwright——Builds 页进搜索范围页签、build 详情/模块列表渲染、详情 Module ID 字段呈现；（环境可得时）`jf` 真实上传 + promote 走通——四闸门 + axe 双主题双 locale。

#### FR-153 Release Bundle 最小面（internal/bundle 新包 + web/src；候 Q2 深度裁定）

**用户故事**：作为发布管理员，把通过验证的一批制品打成版本化 bundle 记录（可查询可追溯）；权限两步弹窗的 Any Distribution 预置不再缺位。

行为规格（骨架）：
- **153.1 模型与端点族**：bundle 实体（名字/版本/制品清单/创建者/状态）+ 创建/查询端点族（形态归 release-bundle.md 冻结——v2 signing/Distribution 对接系全量面，候 Q2 裁，PM 倾向不做）。
- **153.2 Any Distribution 预置解锁**：权限两步弹窗选仓步 Any Distribution 预置桶可用且生效（M16 B-2.16 缺位登记解除——与 FR-156 Any Local/Any Remote 通配桶同场同语义）。
- **153.3 FE 最小面**：bundle 列表/详情页（形态照规格票 + t226 对照）。

AC 骨架：
- **AC1**：curl 创建 bundle（含制品清单）→ 2xx + 查询回显逐项一致；重复版本冲突语义照规格票。
- **AC2**：Playwright——权限两步弹窗 Any Distribution 勾选 → 授予面覆盖断言（被授权用户可见/可操作探针）；bundle 列表/详情渲染。
- **AC3**：ACL 探针（越权用户零出现）。

#### FR-154 洞察报表/趋势分析（聚合层 + web/src；候 Q0 第四行解禁）

**用户故事**：作为平台负责人，dashboard 看下载趋势/存储增长/Top 仓——洞察系「UI 高级分析」解禁后的第一程（Q6 主目③）。

行为规格（骨架）：
- **154.1 聚合快照层**：时序聚合表（每日/周期快照——触发机制〔挂 cron 调度域 or 独立 ticker〕与数据模型归架构会签）；数据源 = M16 per-node 统计基建（K69 单源契约——零第二计数通道）。
- **154.2 图表族**：下载趋势 / 存储增长 / Top 仓 / Top 制品（形态沿 dashboard 既有自有形态——Artifactory Insights 系独立商业产品无 OSS 对位，C 层留痕）；既有 dashboard 去重率口径不变（A1 注记）。
- **154.3 locale/主题**：图表标签随 i18n 双 locale；亮暗双主题。

AC 骨架：
- **AC1（单源一致）**：下载夹具 N 次 → 图表计数与 FileInfo `downloads` 对账一致；快照测试加速（手动触发等价腿——cron 域消费面联动留痕）。
- **AC2**：Playwright——四图表渲染 + 空数据空态 + 双 locale 双主题；四闸门 + axe。

#### FR-155 Federation/Lifecycles/Repository Path Map（候 Q1/Q8——PM 倾向滚 M18 单列专程）

**用户故事**：（若裁进）作为多站点运维，Federation 镜像联邦把中心仓内容自动分发到边缘仓（pull 模式联邦）；Lifecycles 为制品/Release 定义状态机生命周期。

行为规格（骨架）：Federation 系引擎级架构（镜像联邦——与 M6 push/pull 复制、M15 包 B 的边界归 ADR 评估）；Lifecycles（Release Lifecycle 细分面 + Cleanup-Retention 策略引擎作消费面——Q8 联动）；Repository Path Map 管理面（路径重写映射——A1 联动）。**PM 倾向滚 M18 单列专程**：容量（+5~8 票超带）+ 引擎级决策深度；裁进则本程只承载最小面（评估票 + Federation 单向最小腿或 Lifecycles 状态机最小面——Q1 出口②定容）。

AC 骨架（若裁进——候裁不设硬断言）：Federation 边缘仓拉齐（逐路径 sha256 对账）；Lifecycle 状态迁移端到端；Path Map 重写生效 curl 断言。

### 4.3 确定层（滚程收纳——审定后可先行，不候 Q0/Q1）

#### FR-156 BE 表单域承接与权限域收口（internal/httpapi + internal/repo + web/src；P1）

**用户故事**：作为仓管理员，M16「PUT 全收但 configJSON 静默丢弃」的漂移收口——四域提交即生效即回显；权限面 Any Local/Any Remote 通配桶有 BE 语义真身；进制品浏览首仓库自动选中。

行为规格（骨架）：
- **156.1 configJSON 四域 round-trip**（滚程五项①）：maxUniqueSnapshots/repoLayoutRef/blackedOut/archiveBrowsingEnabled 转发落库 + GET 回显 + 行为联动（blackedOut=true 拒写等）+ Stage〔原 Environments，7.161 更名〕域承接（wire 键归票定案）；K73 repoLayoutRef 布局引擎接线评估落定（接线布局引擎 or 钉协议默认值）。
- **156.2 通配桶 BE 语义**（M16 B-2.16）：Any Local/Any Remote 预置桶通配语义（or 与缺位登记同口径终裁——候票内/architect 小评）；与 FR-153 Any Distribution 同场同语义。
- **156.3 B-1.7 能力位候裁腿**：双布尔 vs 三值枚举——ADR-0026 闭集增补评审（M17-Q11）；FE tripwire 已备，BE 落地日翻红即转正。
- **156.4 收尾腿**：B-3.2 初始态（首仓库自动选中 + item view——滚程五项②）+ Users Last Login FE 列（T-468 条件票收编——BE 投影已备）。

AC 骨架：
- **AC1**：表驱动——`curl -X PUT "$BASE/binflow/api/repositories/<key>"`（body 含四域）→ GET 回显逐域一致（T-439 tripwire 转绿）+ blackedOut=true 时 `docker push` → 拒写断言。
- **AC2**：通配桶——权限面勾 Any Local → 无权限用户对本地仓 403/不可见探针（覆盖断言）；Stage 域往返。
- **AC3**：Playwright——制品浏览进入即首仓库自动选中 + item view 呈现；users 列表 Last Login 列渲染（T-454 投影消费）。

#### FR-157 契约漂移勘误族（internal/httpapi + web/src；P1）

行为规格（骨架）：① `GET /api/repositories` `url` 补 `/binflow` 前缀（M1 既有——与 StorageInfo baseUrl 族对齐）；② FileInfo.downloadUri 下载语义 URI 归位（或语义注记终裁——票内二选一留痕）；③ **System Logs 服务进程日志端点**（BE 端点票——T-459 以审计承载收口、文档如实注记，本票补真身；端点形态归票 + 文档反转登记）；④ `?properties` 分号矩阵语法注记（REST 只认逗号配对——文档面）；⑤ NodeDetail useAsync 重复发射纯浏览多计下载（FE 缝票——统计计数口径修正，K69 单源契约不破）。

AC 骨架：
- **AC1**：`curl -u $ADMIN "$BASE/api/repositories" | jq '.[0].url'` → 含 `/binflow` 前缀；downloadUri 处置结论留痕 + 断言。
- **AC2**：System Logs 端点 curl 尾随/过滤可用 + Playwright SystemLogsPage 数据源切换（审计承载 → 进程日志真身，降级路径保留）；`whats-new`/console.md 注记反转归位。
- **AC3**：Playwright——纯浏览（只选中不下载）后 `downloads` 计数不变（useAsync 缝修正）+ 既有下载计数断言零回归。

#### FR-158 cron/i18n 遗留（internal/scheduler + web/src；P2）

行为规格（骨架）：gc-cron-gap 三载体（Quota 百分比 / Compress Internal Database / Prune Unreferenced Data——M16 诚实缺位登记的后端载体补齐，挂调度域消费面）；远端枚举快照 TTL 600s wire 可调（K71 登记）；语言切换器位置 ux 复核 + faq 两问入册。

AC 骨架：
- **AC1**：三载体 cron 配置 CRUD + 到点 dry-run 断言（调度引擎复用——ADR-0044 消费面扩列）；TTL PUT → 回显生效。
- **AC2**：切换器位置复核结论落 parity 册；faq 两问落 docs（`make docs` 零断链）。

#### FR-159 webhook 域二程 + Replay/outbox 行级 REST 并轨（internal/webhook/outbox + httpapi；P1——候 M17-Q5 终裁）

**用户故事**：作为 webhook 消费运维，死信可重放（行级 REST），触发源注册表与实际 wired 状态一致——M16 Q11 裁点在本程兑现（build 域随 FR-152 wired）。

行为规格（骨架）：
- **159.1 dormant→wired 翻转与裁剪出口（M17-Q5）**：build 域随 FR-152 wired（schema 零变化）；非 xray 域触发源裁剪（ADR-0041 决策 7——仅注册有源域 vs 全量维持 dormant，候终裁）；注册表 wired/dormant 如实标注零伪造。
- **159.2 Replay/死信重放行级 REST**：outbox 行级查询 + 重放端点（形态归票——机制已备翻转面小，M13 登记的运营增强面）。

AC 骨架：
- **AC1**：构造死信（消费者 500）→ `POST`（Replay 端点，形态照票）重放 → 消费者收到 + 行状态翻转 + audit 行。
- **AC2**：裁剪出口（若 Q5 裁剪）——无源域触发源注销/标注断言 + ADR-0041 决策 7 修订留痕；若裁维持全量——dormant 标注审计一致。

#### FR-160 工程债与续滚收尾包（P2——尾波弹性，做不完显式登记不无界滚）

行为规格（骨架）：M15 续滚八项（aql.md V-a~V-h 活体核验归档 / 相对时间 `"1d"` 空格 vs 官方后缀表冲突处置〔lexer 翻转点或规格注记——票内裁〕/ compact 非空行体 415 实核 / 429 Retry-After 上 UI〔ApiError 响应头承载〕/ virtual echo 原始 config blob〔成员级联删除后回显失效成员——echo 改造〕/ 大仓 limit 化全枚举缝 / eslint ratchet 37 条清零或阈值收紧 / vite 8 + MUI v7→v9 段二〔触全页面，最后位〕）+ Annotate write 别名移除评估（K68 登记——移除则 PUT write → 400 + 迁移窗，票内裁并留痕）+ dry-run CLI 挂点（cmd/ 小票）+ QRL 三遗留（audit 词 / LOW_PRIORITY 桶 / V-m 对拍）+ migration 011 墙钟阈值 flake + X-Explode-Archive staging 同族迁移（internal/repo/archive.go:1082——spool 家族后续）+ migrate CLI 低危登记 + 文档漂移四条核对（m16-done 尾波文档票可能已顺带清偿——票内核对增删）。

AC 骨架：逐债项一断言（curl/Playwright/make 门）——骨架不逐条展开，票面 AC 全文候 tech-lead 拆票（沿 M16-SPLIT 体例）。

#### FR-161 virtual 成员同型全包型推广（internal/adapter/* + repo service；候 M17-Q6 容量裁定）

行为规格（骨架）：virtual 成员同型约束推广至全部 13 包型 × 三 rclass（M16 容量注滚程项——现态缺陷面窄但语义不一致）；复用 M16 十协议矩阵基建。

AC 骨架（若承载）：13 包型 × 三 rclass 全量回归矩阵绿（真实客户端矩阵——docker/mvn/npm/pip/go/cargo/conan/helm/dnf/apt/dotnet/oras 复用既有）；未承载则「M17 未纳入项」显式登记。

---

## 5. 兼容性矩阵（M17——产品域 wire 契约 + 勘误归位）

### 5.1 层级定义（沿 M13~M16 四层）

A 兼容（对齐 Artifactory 端点/语义——规格票置信度冻结后生效）/ C 自有 / D 有意不兼容或不做 / 待裁（终裁后归位）。

常设纪律：① 翻案双留痕（PRODUCT.md 修订 + Q 出口）；② 无数据源不伪造；③ ACL 零泄漏（build/bundle 系新泄漏面——探针硬 AC）；④ 规格票冻结前 PRD 暂行端点形态不进硬断言（K74~K76）。

### 5.2 档位 × addon 解锁矩阵（M17 增量 0~1 行——候规格票档位核验）

Builds 面 OSS 档可用性 + Release Bundle 商业档位 = 规格票活体核验腿定案；若需新槽走 ADR-0033 增补（票内核对）。Build-info 数据面若系核心能力（oss 档类比 AQL）则无槽。三态叠加规则与 `addons.disabled` 熔断沿 M10 §5.2 不变。

### 5.3 契约矩阵（估 16 条，LC-99~LC-114 续接 M16 编号——骨架估值，候裁项终裁后归位）

| # | 契约面 | Artifactory 对应 / 取证锚 | 层级 | 优先级 | 候裁联动 | 验收 |
|---|---|---|---|---|---|---|
| LC-99 | Build-info REST 域——PUT/append/查询/promote/retention/docker promote（端点子集归规格票） | 官方文档 Build Info 域 + t226/7.161 活体 + inv 补白 | **A（子集注记）** | P0（候 Q0/Q1） | K74 | L50/L51 |
| LC-100 | webhook build 域事件 dormant→wired（ADR-0041 schema 零变化） | webhook.md 36 事件注册表 + 决策 7 | A | P0（候 Q0/Q1） | Q5 | L52 |
| LC-101 | AQL build/module/dependency 域 + `/api/search/buildArtifacts·dependency` | aql.md 增量段（M15 §5.7 全景表随域行兑现） | **A（子集注记）** | P1（候 Q0/Q1） | K74 / Q9（license 腿缺位） | L53 |
| LC-102 | Builds FE 页 + 搜索范围页签 Builds + 详情 Module ID 字段 | t226 Builds 页对照（M15 R2 缺位其一解除 + M16 stay-out 解除） | A | P1 | — | L53 |
| LC-103 | Release Bundle 最小面——模型 + 创建/查询 + Any Distribution 预置 | release-bundle.md 规格票（档位核验）+ M16 B-2.16 缺位登记 | **A（子集注记——深度候 Q2）** | P1（候 Q0~Q2） | Q2 / K76 | L54 |
| LC-104 | 洞察报表聚合快照层 + 图表族 | Artifactory Insights 系独立商业产品无 OSS 对位——BinFlow 自有实现；dep K69 统计单源 | **C（自有——呈现沿 dashboard 既有形态）** | P1（候 Q0） | K75 | L55 |
| LC-105 | configJSON 四域 round-trip + 行为联动 + Stage 域 | M16 T-439 漂移钉 tripwire + K73 + 7.161 更名 | A | P1 | K73 落定 | L56 |
| LC-106 | Any Local/Any Remote 通配桶 BE 语义（Any Distribution 同场） | M16 B-2.16（FE 不伪造候裁在案） | A | P1 | 与 FR-153 同场 | L56 |
| LC-107 | 契约漂移勘误族——url 前缀 / downloadUri / System Logs 端点 / ?properties 注记 / useAsync 双计 | M16 未纳入项登记族七条（M1 既有 url 行） | **A（勘误归位）** | P1 | **T-493 票内裁留痕（2026-09-07）：downloadUri 二选一 = ①下载语义 URI 归位**（uri=api/storage 视图 / downloadUri=直取 `/binflow/<repo>/<path>`——官方 FileInfo 示例 + 上传 201 体内核佐证；rest-compat-matrix D01 行 1 登记）；System Logs 端点形态 = BinFlow 形 GET `/api/v1/system/logs`（C 层——limit/filter/download 三臂，system:read 门；rest-compat-matrix D06 行 13 ◐）；url 前缀与 ?properties wire 断言同票落地，useAsync 双计 + FE 数据源切换归 T-494，文档面归 T-518 | L57 |
| LC-108 | gc-cron-gap 三载体 + 枚举 TTL wire 可调 | ADR-0044 消费面扩列（M16 诚实缺位登记解除） | A | P2 | — | L58 |
| LC-109 | Replay/outbox 行级 REST（死信重放） | M13 登记（T-364/T-366）+ outbox 引擎既有 | **A（子集注记）** | P1 | — | L52 |
| LC-110 | webhook 触发源裁剪出口（dormant 家族处置） | ADR-0041 决策 7 翻转路径（M16 Q11 承接） | **待裁（Q5——终裁归 A/C）** | P1 | Q5 | L52 |
| LC-111 | virtual 成员同型全包型推广（13 包型 × 三 rclass） | T-367 登记（M15/M16 滚程） | A | P2（候 Q6 容量） | Q6 | L60 |
| LC-112 | Federation/Lifecycles/Repository Path Map | 预立项段主目④ + Q8（Cleanup 策略引擎联动） | **待裁（Q1/Q8——裁进归 A 子集 / 裁出滚 M18）** | 候裁 | Q1/Q8 | L59（若裁进） |
| LC-113 | B-3.2 初始态 + Users Last Login FE 列 + 列集收尾 | M16 B 偏差滚程 + T-468 条件票收编 | A | P2 | — | L56 |
| LC-114 | i18n 切换器位置复核 + faq 两问 | M16 未纳入项 i18n 遗留 | C（minor） | P2 | — | L58 |

> 计数（骨架估值）：**16 条 = A 12（LC-99〔子集〕/100/101〔子集〕/102/103〔子集〕/105/106/107/108/109〔子集〕/111/113）+ C 2（LC-104/114）+ 待裁 2（LC-110/112）**；D 层 0 新增行（Non-goals 维持项不占矩阵行——§2.2 留痕）。规格票冻结后逐行回填置信度与 as-built 状态（沿 M15 v1.1 体例）。

### 5.4 回归基线（断言反转与缺位解除登记——逐条归属 M17 豁免票）

| 既有断言/形态 | M17 期望 | 归属 |
|---|---|---|
| M1~M16 全部 P0 序列（双形态） | 零回归（十协议矩阵复跑 + 全量双形态终验） | 各票 + QA 终验 |
| `/api/repositories` url 无前缀（M1 as-built） | **勘误归位①**：url 含 `/binflow` 前缀（baseUrl 族对齐） | FR-157 豁免票 |
| System Logs 审计承载 + 文档「如实注记」（T-459 as-built） | **缺位解除②**：进程日志端点真身 + 文档注记反转 | FR-157 豁免票 |
| Module ID / Any Distribution / Builds 页签缺位登记（M16 stay-out） | **缺位解除③④**：随 build-info / bundle 域落地解除（候 Q0/Q1） | FR-152/153 豁免票 |
| webhook build 域 dormant 标注（M16 as-built） | **断言翻转⑤**：build 域 wired（真实消费者断言） | FR-152/159 豁免票 |
| Annotate write 别名收 PUT（K68 as-built） | **候裁翻转⑥**：别名移除（→400 + 迁移窗）或维持登记——票内裁留痕 | FR-160 豁免票 |
| 事件驱动增量唯一引擎口径（ADR-0044 as-built） | 零倒退（调度仍只触发全量类任务；聚合快照若挂 cron 归全量类——会签留痕） | FR-154 |
| E1/E6/smu-*/anchor ledger | 零倒退维持（新 FE 面入册流程照旧） | 每票硬 AC |

### 5.5 M17 核心验收命令（L 序列骨架，QA 直接引用——续接 M16 L48 起）

```bash
BASE=http://127.0.0.1:8080; ADMIN=admin:password
# ========== FR-151 前置锚 ==========
# L49 Q0 终裁 → PRODUCT.md 修订落笔（五条处置留痕）+ Q1 定容落章 + build-info.md/release-bundle.md
#    规格票落盘（逐端点置信度 + 子集边界表 + tech-lead 就绪度确认）+ ADR-0045/0046 Accepted +
#    活体基线处置结论（Q10——修复 or t226 单源降级留痕）

# ========== FR-152 Build-info 主程（候 Q0/Q1） ==========
# L50 REST 全链：curl PUT /api/build/myapp/1（上传）→ GET 回显 → 段 PUT append 合并不覆盖 →
#    列表/最新端点 → ACL 探针（无权限用户 403/零出现）
# L51 promote + docker：POST promote（状态翻转 + audit）→ docker pull <rel-repo>/<img>（删缓存复拉仍成功）+
#    retention 删除保留窗外 build（形态照规格票）；jf CLI 条件腿（环境可得时真实走通）
# L52 webhook wired + Replay：local receiver 收 build 事件（HMAC 签名验证）+ 注册表 wired/dormant
#    标注审计一致 + 死信构造 → Replay 端点重放 → 消费者收到 + 行状态翻转；触发源裁剪出口（候 Q5 终裁）
# L53 AQL build 域 + FE：builds.find 查询绿 + dependency include + 未支持域 400 诚实拒绝 +
#    /api/search/buildArtifacts·dependency + Playwright Builds 页（范围页签/详情/Module ID）+
#    四闸门 + axe 双主题双 locale

# ========== FR-153 Release Bundle（候 Q2） ==========
# L54 curl 创建 bundle → 查询回显逐项一致 + 重复版本冲突语义 + Playwright Any Distribution
#    预置勾选（授予面覆盖探针）+ bundle 列表/详情 + ACL 探针

# ========== FR-154 洞察报表（候 Q0） ==========
# L55 下载夹具 → 图表计数与 FileInfo downloads 单源对账 + 快照手动触发等价腿 +
#    Playwright 四图表渲染/空态/双 locale 双主题

# ========== FR-156 BE 承接（确定层） ==========
# L56 表驱动 round-trip：PUT repositories（四域 body）→ GET 回显逐域一致（T-439 tripwire 转绿）+
#    blackedOut=true 拒写断言 + Stage 域往返 + 通配桶 Any Local 覆盖探针 +
#    Playwright 首仓库自动选中（B-3.2）+ Users Last Login 列

# ========== FR-157 契约勘误 ==========
# L57 GET /api/repositories → url 含 /binflow 前缀 + downloadUri 处置结论断言 +
#    System Logs 端点 curl 尾随/过滤 + SystemLogsPage 数据源切换（文档注记反转）+
#    纯浏览 downloads 不增（useAsync 缝）+ 既有下载计数零回归

# ========== FR-158 cron/i18n 遗留（LC-108/LC-114） ==========
# L58 gc 三载体 cron CRUD + 到点 dry-run + TTL wire 可调往返 + 切换器位置复核落册 + faq 两问（make docs 零断链）

# ========== FR-160 工程债包 ==========
# L59 逐债项一断言（aql V-a~V-h 活体归档 / "1d" 冲突处置 / compact 415 / Retry-After 上 UI /
#    virtual echo 改造 / limit 化缝 / eslint ratchet / vite8+MUI 段二全量 e2e 复跑 /
#    write 别名评估结论留痕 / QRL 三遗留 / migration 011 / X-Explode staging / 文档四条核对）

# ========== FR-161 同型推广（候 Q6） ==========
# L60 13 包型 × 三 rclass 全量回归矩阵（真实客户端矩阵复用）；未承载则「M17 未纳入项」显式登记

# ========== 回归与收口 ==========
# L61 全量回归：M1~M16 P0 双形态复跑 + 缺位解除归位审计（Module ID/Any Distribution/Builds 页签/
#     System Logs 注记四项）+ 断言翻转①~⑥归属审计 100% M17 豁免票 + FE 票服务端 diff=0（BE 腿除外）
# L62 NFR：Build-info 写入/查询 P95 + 聚合查询 P95（万节点量级——细则候 ADR/规格票）+
#     资源门三连（footprint ≤100MB / check-size ≤120MB / 冷启动 <2s）+ SPA ≤10KB/票 + F1 趋势登记
# L63 文档：tech-writer（Build-info/Release Bundle/洞察指南 + 缺位解除用户可见公告 + cron/i18n 增量）
#     客户端命令实测可复跑
```

### 5.6 待校准项（K74~K76 新起——骨架暂行；效力序 §1.4 条款 4）

| # | 项 | 暂行值 / 校准路径 |
|---|---|---|
| K74 | Build-info 数据模型与 REST 子集（端点族取舍 / append 合并语义 / promotion 状态机 / retention 形态 / build 数据 ACL 模型——仓库级 vs 独立 build 权限面） | build-info.md 规格票 + ADR-0045 冻结后回填 |
| K75 | 洞察聚合口径（快照周期 / 触发机制〔cron 域 vs 独立 ticker〕/ 指标集 / 与 K69 单源契约的对账面） | 架构会签 + FR-154 票内定案后回填 |
| K76 | Release Bundle 深度边界（最小面端点集 / v2 signing 与 Distribution 对接的进退 / 档位映射） | Q2 终裁 + release-bundle.md + ADR-0046 定案后回填 |

---

## 6. 非功能需求（NFR）

### 6.1 与已有 ADR / 规范的冲突/补充标注

| ADR/规范 | 冲突/补充点 | 本 PRD 立场 | 所需动作 |
|---|---|---|---|
| PRODUCT.md「明确不做」 | 三条触及（HA·联邦 / 洞察 / 全量 REST）+ 一条滞后（LDAP/SAML/OIDC 已实现） | Q0 修订稿候裁（§0.2）——落笔前主轴不启动 | FR-151.1 / Q0 |
| ADR-0041（webhook）决策 7 | dormant 家族处置 + build 域 wired | Q5 终裁出口；wired 标注如实零伪造 | FR-159 / Q5 |
| ADR-0044（cron）K69 统计单源 | 洞察报表数据源 + 聚合快照触发机制 | 单源契约不破（零第二计数通道）；快照若挂调度归全量类任务 | FR-154 会签 |
| ADR-0026（RBAC 闭集） | B-1.7 双布尔 vs 三值枚举 | 评审候裁（Q11）——tripwire 已备 | FR-156.3 |
| ADR-0033（addon 注册表） | Builds/Bundles 档位 | 规格票档位核验；需新槽走增补 | FR-151.2 |
| ADR-0001（clean-room） | 官方文档基准 | build-info/bundle 均公开 REST 面——官方文档唯一基准先例沿用 | §1.4 条款 3 |

### 6.2 性能与资源（骨架——精确门候 K74/K75 定案）

| NFR | 指标与验收方式（暂行） | 优先级 |
|---|---|---|
| NFR-P80 Build-info 链路 | 写入/查询 P95 ≤300ms 量级（单 build 十模块百依赖量级——精确门候 ADR-0045）；promotion 大 build 迁移零 5xx | P1 |
| NFR-P81 聚合查询 | 洞察快照查询 P95 ≤800ms 量级（万节点）；快照任务不阻塞在线路径 | P1 |
| NFR-P82 资源门维持 | footprint ≤100MB / check-size ≤120MB / 冷启动 <2s 三连；SPA 每票 ≤10KB（Builds/图表 FE 面大——累计趋势观察）；F1 六平台趋势登记 | P0 |

### 6.3 安全底线（骨架）

| NFR | 要求 | 验收 |
|---|---|---|
| NFR-S80 ACL 零泄漏 | build/bundle 数据面与内容面同一 allow() 源——越权探针逐票硬 AC（新泄漏面最宽处） | L50/L54 |
| NFR-S81 promotion/分发权限门 | promote 动词权限 + bundle 管理 admin/manager 门 + audit 全记 | L51/L54 |
| NFR-S82 门槛纪律 | Q0 终裁前主轴零实现（流程安全条款——审计点：主轴票派发时间 ≥ PRODUCT.md 修订落笔时间） | DoD#5 |

### 6.4 可观测性（骨架）

build 域指标族（上传/promote 计数与耗时——Prometheus 既有口径延伸）；webhook build 事件投递指标；Replay 操作 audit；聚合快照任务执行/失败事件（挂 maintenance.* 词族）；QRL/搜索域指标零回退。

---

## 7. 用户裁定清单（Q0~Q12——范围边界候选；必答前置 Q0/Q1。每项：边界描述 / 两出口 / PM 倾向 / 建议裁点）

| # | 边界描述 | 出口 | PM 倾向 | 建议裁点 |
|---|---|---|---|---|
| **Q0** | **PRODUCT.md 五条修订**（宪法级门槛——主轴四主目触及三条 + 一条滞后）：修订稿见 §0.2 表 | ① 按建议稿修订（Federation 候 Q1 / HA 候 Q3 / 洞察解禁 / Xray 维持 / 第三行补账）② 用户改稿终裁 | 建议稿（含全量 REST 行维持——联动 Q4） | **立项窗必答**（conductor 组织——落笔前主轴不派票） |
| **Q1** | **四主目定容与分期**：Build-info + Release Bundle + 洞察 + Federation·Lifecycles 全进一程 = 40~48 票超 30~40 带宽 | ① 裁减分期（PM 建议：前三域进 M17，Federation/Lifecycles 滚 M18 单列专程——32~40 票）② 四域全进（超带——建议拆 M17a/M17b 或裁确定层尾部） | ①裁减分期——「一个域做深 > 四个域 happy path」准绳 | **立项窗必答**（tech-lead 拆票前置） |
| **Q2** | **Release Bundle 深度**：最小面（local bundle + 查询 + Any Distribution 预置）vs 全量分发面（v2 signing / Distribution 服务对接——越单仓边界风险类 Xray 集成面） | ① 最小面（PM 建议）② 全量面（须先出 Distribution 服务可行性评估票） | ①最小面 + 规格票边界留痕 | FR-153 断言冻结前（规格票产出后） |
| **Q3** | **HA 高可用本体**（追加候补）：进 M17 vs 维持不做/滚 M18 单列专程 | ① 进 M17（+5~8 票 + 部署矩阵全量回归——超带）② M18+ 单列专程候选（须独立 ADR 群） | ②滚 M18——引擎级决策 + 容量/风险双高 | 立项窗（随 Q1） |
| **Q4** | **全量 REST 承诺退让边界**（追加候补）：高频子集承诺是否退让 | ① 维持子集承诺（PM 建议——工程纪律；AQL/Build-info 均只承诺子集）② 扩面承诺（须给端点覆盖率目标 + 回归矩阵预算） | ①维持子集——PRODUCT.md 第五行建议原文保留 | 立项窗（随 Q0） |
| **Q5** | **webhook 触发源裁剪**（M16 Q11 承接——ADR-0041 决策 7；build 域随 FR-152 wired 后裁点已至） | ① 裁剪（仅注册有源域——PM 倾向，与 ADR-0041 防线一致）② 全量注册维持 dormant 标注 | ①裁剪 + wired/dormant 如实标注审计 | FR-159 拆票前 |
| **Q6** | **virtual 同型全包型推广排期**（M16 容量注滚程）：M17 承载 vs 续滚 | ① M17 承载（P2 尾波——矩阵基建已备，PM 倾向若容量余）② 续滚 M18（升级为连续三程滚程债——须显式登记） | ①承载（连续两程滚程即升级为债纪律） | Q1 定容后随拆票 |
| **Q7** | **Go/Terraform/GitLFS/AI-ML 包型域**（M16 容量注滚程项） | ① 滚 M18+（包型本体扩张各自专程）② M17 挤入（PM 不建议——容量被产品域占满） | ①滚 M18+ | 立项窗（随 Q1） |
| **Q8** | **Cleanup-Retention 策略引擎 / 冷存储分层**（与 Lifecycles 域关联） | ① 随 Federation/Lifecycles 同裁（M18 专程消费面）② M17 独立小程（PM 不建议——策略引擎本体体量） | ①随 Q1 联动（M18） | 随 Q1 |
| **Q9** | **`/api/search/license` 腿**（预立项段联动面列定，但 license 识别系 xray_tied 剔除族——无数据源） | ① 缺位登记不做（PM 建议——无数据源不伪造；dependency/buildArtifacts 两腿照做）② 先立 license 识别域（**越 Xray 排除边界——须用户明示推翻 intake ⑤**） | ①缺位登记 | FR-152 断言冻结前 |
| **Q10** | **7.161 参照容器双损坏修复**（conductor 决策项移入——M17 活体参照基线依赖） | ① 修复（pro router + oss 进程）② 降级 t226〔7.84.10〕单源 + 官方文档（规格票置信度注记降级留痕） | ①修复（7.161 对照价值高）——归 conductor 裁量 | FR-151 规格票启动前 |
| **Q11** | **B-1.7 双布尔 vs 三值枚举**（ADR-0026 闭集增补评审——FE tripwire 已备） | ① 增补闭集（BE 双布尔落地 + ADR 增补）② 维持枚举 + 差异登记（tripwire 翻红即转正触发器） | ②维持现行 + 触发器机制（M16 v1.2 核定延续） | FR-156 拆票时（architect 评审） |
| **Q12** | **NuGet symbol server 七承**（条件池维持确认——零用户触发信号） | ① 维持条件池（用户点名即翻）② M17 转正排期 | ①维持条件池（沿 M16 Q10 终裁口径） | 无裁点（登记确认项） |

---

## 8. M17 验收剧本（QA 总纲，骨架）

1. **前置锚先行**：L49（Q0/Q1 终裁落笔 + 规格票 + ADR 群 + 活体基线处置）——主轴断言地基。
2. **回归硬门槛**：M1~M16 P0 双形态复跑全绿（缺位解除项在解除票前后各跑一轮——登记态/实态双留痕）。
3. **确定层先行票**：L56（BE 承接 + 通配桶 + B-3.2）→ L57（契约勘误族）→ L58（cron/i18n 遗留）——不候主轴。
4. **主轴（候 Q0/Q1 后）**：L50（Build-info REST）→ L51（promote + docker 真实腿）→ L52（webhook wired + Replay）→ L53（AQL build 域 + FE Builds 页）→ L54（Release Bundle + Any Distribution）→ L55（洞察报表单源对账）。
5. **候裁腿**：L59 若裁进（Federation/Lifecycles 最小面）；L60（同型推广，候 Q6）。
6. **收口**：L61（全量回归 + 缺位解除归位审计 + 断言翻转归属审计）→ L62（NFR + 资源门）→ L63（文档实测复跑）。
7. **文档**：tech-writer——Build-info 接入指南（CI 集成/jf/curl）/ Release Bundle / 洞察报表 / 缺位解除用户可见公告 / cron·i18n 增量。
8. **release**：部署烟测 + UAT 随里程碑 PR（Build-info 与洞察在 UAT 链取证）。

---

## 9. M17 DoD（骨架——沿 M16 形制）

1. §4 全部 P0 AC（FR-151 前置锚 + FR-152 主程——候裁层终裁后）经 qa 验证全绿；P1（FR-153/154/156/157/159 主体 / FR-158）全绿；P2 与候裁腿（FR-155 / FR-160 / FR-161 / 条件池）按条件条款——未触发不构成 DoD 缺口，须 BOARD 留痕；
2. §8 剧本全绿；§1.2 量化门槛表逐行达标（全部有 Playwright/curl/真实客户端实测证据归档）；
3. **产品域翻案收口**：PRODUCT.md 修订落笔（Q0 终裁后——五条处置与修订记录双留痕）；裁定清单 Q0~Q12 逐项归位零滞留；**缺位解除四项归位审计**（Module ID / Any Distribution / Builds 页签 / System Logs 注记——解除票与登记态可回溯）；
4. 回归硬门槛：M1~M16 全部 P0 序列双形态复跑全绿；断言翻转/勘误归位（§5.4 ①~⑥）归属审计 100% M17 豁免票；FE 变更面 100% 归属 M17 票 + FE 票服务端 diff=0（BE 腿除外）；
5. 前置产物与门槛纪律：规格票两份 + ADR-0045/0046 Accepted + 洞察聚合会签 + 活体基线处置结论（Q10）齐备；**NFR-S82 门槛纪律审计**（主轴票派发时间 ≥ PRODUCT.md 修订落笔时间）；
6. NFR-P80~P82 / NFR-S80~S82 达标归档；`make test`（race）全树绿 / `make lint` 0 issues / gofmt 空；资源门三连 + SPA ≤10KB/票 + F1 趋势登记；
7. §2.2 Non-goals 与候选池对账：滚入 M18+ 项在 ROADMAP「M17 未纳入项」登记（备稿沿 T-395/T-427/T-460 先例收口窗启用）；连续滚程项显式登记（不无界续滚）；§1.4 出处义务在全部 M17 票可审计；
8. 主会话 git tag `m17-done`（对外发布任何制品先经用户确认；PR 化合并沿既定 gitflow 程序；UAT 证据随里程碑 PR 归档）。
