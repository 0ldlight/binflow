# PRD — M16 控制台 full-parity 收口大程：制品树栈（P0）+ 仓库表单栈 + 详情/搜索栈 + 安全/shell 栈 + AQL 高级面副线 + 远端浏览可选档

> **PRD 状态：v1.1 裁定回填版（2026-09-02——七项终裁落章：用户四项 Q1 引入 cron 调度域〔推翻 M15 Q5 终裁，BOARD 留痕〕/ Q3 双语可切换〔E6 锁死项翻案〕/ Q4 页码控件 / Q6 产品域进〔两程结构〕，conductor 三项 Q2 E1 修文本收紧不倒退 / Q5 路由化 / Q7 Annotate 加；v1.0 立项稿同日 PM 起草——范围基线与五股输入见下）**。主轴定音：**用户指令 intake ⑤（2026-09-02 00:1x，BOARD 在档）**三指令——①「现在制品树展示仍然和 artifactory 的逻辑严重偏离」（**用户第三次 UI 加码**，M14/M15 parity 后仍不满）；②「下个里程碑需要完全检查整个前端，对齐 artifactory 的所有内容」；③「现阶段除了 xray 暂时不做，剩余产品文档中明确不做（第一版）的都要做」——**不做清单全面翻案（除 Xray）**，且与 conductor 先前裁定冲突处（如 Q5 cron 双轨已裁不引入）**立项稿列冲突点交用户确认而非默默翻转**（intake ⑤ 原文要求，本 PRD §7 承载）。范围基线：**M16 全量审计 workflow 产出** `reports/m16-parity-audit-material.md`（2026-09-02 10:0x 收官——8/8 agent 齐：A 翻案清单 8 域 66 项〔xray_tied 已剔〕/ B 活体偏差 47 项〔logic 11 · visual 17 · minor 19，t226 逐页实测带代码行锚〕/ C 冲突点 13 项 / D 骨架建议）+ ROADMAP「M15 未纳入项」（T-427 备稿，逐条〔M16 吸收预期〕标注——**双源对账，勿重复立项**）+ M15 Q4/Q6 终裁承接（远端浏览出口 C 批 1 / docker virtual 开禁）。**并行声明**：本稿与 M15 收尾（T-423 busy 在途 + T-429 release → T-430 终验 → m15-done 收口窗）并行起草，纯文档零冲突；M15 in-flight 票的遗留以届时收口笔为准增删。**v1.1 两程结构（Q6 终裁落章）**：M16 = 交互 parity 主轴（批次①~④）+ i18n 双语可切换（FR-149）+ cron 调度域（FR-150——ADR-0044 占位）+ Annotate 动词 + 远端浏览可选档 + AQL 高级面副线；**M17 预立项段（ROADMAP 新增）** = Builds/Build-info 域 + Release Bundle + 洞察报表 + Federation/Lifecycles——须 PRODUCT.md 修订 + ADR 群，候 M16 收口后 PM 正式立项（§0.3）。

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/milestone-16.md` |
| 里程碑 | M16 — 控制台 full-parity 收口大程（两程结构之前程）——§0 范围定界（两程结构：交互 parity 收口落 M16 / 产品域扩张落 M17 预立项段）+ 主轴四批次（① 制品树栈 P0 先行 ② 仓库管理表单栈 ③ 详情/搜索栈 ④ 安全/shell 栈）+ 后端配合小域（Annotate 动词〔已裁加〕/ per-node 下载计数 / Last Login 派生）+ **i18n 双语可切换（Q3 终裁新增——FR-149）** + **cron 调度域（Q1 终裁新增——FR-150，ADR-0044 占位，GC/备份/复制三消费面）** + remote 远端浏览可选档（M15 Q4 承接）+ AQL 高级面副线（statistics/usage 域 + QRL 全量 + UI 搜索族）+ 条件小票池 |
| 状态 | v1.1 裁定回填版：FR-141~FR-150 十条需求（**新增 FR-149 i18n / FR-150 cron 调度域**）；契约矩阵 LC-80~LC-98 估 19 条（**A 17 / C 2 / 待裁 0**——v1.1 归位：LC-88 Annotate 归 A〔Q7 终裁加〕/ LC-91 cron 归 A 新增域〔Q1 终裁引入〕；新增 LC-97 i18n〔C〕/ LC-98 页码控件〔A〕）；L35~L48 验收命令骨架；K67~K72 待校准项；**用户确认清单 Q1~Q13——必答七项 Q1~Q7 已裁落章（§7）+ Q13 随 Q6 两程结构落定**，余 Q8~Q12 可后裁；A1 产品域扩张族已终裁进 M17 预立项段（§0.3——M16 不承载） |
| 上游依据 | 用户指令 intake ⑤（2026-09-02 00:1x——BOARD 原文在档）、reports/m16-parity-audit-material.md（**本 PRD 骨架素材**——A0~A8 / B-1~B-3 / C1~C13 / D 骨架）、ROADMAP「M15 未纳入项」（T-427 备稿——M16 语境总注 + 逐条吸收预期标注）、docs/prd/milestone-15.md（体例先例 + §5.7 搜索域端点全景表 M16 行 + Q4/Q6 终裁在案）、BOARD.md（intake ⑤ 原文 + M16 全量审计收官条目 + M15 尾波态势）、docs/design/console-artifactory-parity.md（E1~E7 豁免登记 + §9 不做清单——翻案对象三源之一）、PRODUCT.md（不做清单第一版——翻案对象三源之二）、各里程碑 PRD Non-goals 段（翻案对象三源之三）、docs/reverse/inv-1-core.md §E + inv-2-surface.md §1.C/§3（AQL statistics/QRL/UI 搜索族锚点 + AddonType oss 档）、aql.md（M15 交付——statistics 域字段排 M16 系自有基建缺失非 parity 档位）、ADR-0001/0029（clean-room——UI 零复制红线）、ADR-0026（RBAC 角色闭集——能力位翻案的评审前置）、ADR-0032/0033（license 门控——8 包型开禁的槽定义复核）、ADR-0041（webhook 57 型休眠裁剪——决策 7 翻转路径在案）、repo-semantics §8.5（远端浏览牵连口径）、T-425 13 型能力矩阵（远端浏览规格可成稿源）、T-387/T-367/T-364/T-366 等票报告（候选池登记源） |
| 下游消费者 | conductor（审定窗 + Q 终裁组织〔**v1.1：必答七项已落章**〕+ tag m16-done）、tech-lead（拆票——票号自 M15 收口后顺延；宽度 ≤2 内建，§1.3 分票提示）、ux-designer（parity 册 v1.2 修订共笔 + 树/表单/详情形态断言冻结协作 + i18n 语言切换器位置/en 包文案审校）、qa-engineer（逐批 V 式活体复核 + L35~L48 + B 47 项收口审计 + t226 对照 + 双 locale 抽样腿）、reverse-engineer（reverse §3.2 facet 回填 + aql.md statistics/usage 增量段 + remote-browsing.md 新建〔T-425 §1/§2 成稿〕+ QRL/usage/dates 规格锚 + cron 表达式子集锚〔Artifactory Quartz 对拍〕）、architect（**ADR-0044（cron 调度域——Q1 已裁引入）**+ Annotate 动词迁移会签〔Q7 已裁〕+ 统计基建数据模型会签）、dev-frontend（主轴四批次 FE 主 lane + i18n 文案外提/切换器）、dev-go-core（Annotate/统计基建/Last Login/QRL/usage 端点 + 调度引擎）、dev-registry-adapter（远端浏览三型回源枚举）、tech-writer（树/表单/详情/安全/远端浏览/AQL 高级面 + i18n 与 cron 调度文档增量）、release-engineer（烟测 + UAT 随里程碑 PR） |

---

## 0. 范围定界（置于最前——C13「全做」语义总裁定；本稿最高位前提）

### 0.1 两口径（冲突点 C13 原文浓缩）

intake ⑤ 指令③「剩余产品文档中明确不做（第一版）的都要做（除 Xray）」中的「全做」存在两个互斥解读，决定 M16 是一个什么里程碑：

| 口径 | 内涵 | 工程形态 | 前置条件 |
|---|---|---|---|
| **口径一（PM 推荐）：控制台交互 parity 收口** | 「所有内容」= Artifactory 控制台的**交互形态与信息呈现**——B 偏差清单 47 项全数处置 + A2/A4/A5/A6/A7 域的**前端可达部分**翻案（统计基建/QRL/远端浏览等 BE 配合面随需而建） | 一个 **full-parity 收口大程**：web 为主、Go 后端小域配合，批次即非重叠分区（D 骨架主轴） | 无 PRODUCT.md 修订；E1/E6 锁死项零倒退（翻案走 Q 表显式裁定） |
| 口径二：产品域扩张 | 「不做清单全面翻案」= 连 **A1 产品级 Non-goal**（HA 本体 / Builds / Federation / Lifecycles / Release Lifecycle / Repository Path Map / 洞察报表 / 全量 REST）一并做 | **多个专程里程碑群**（HA + Build-info + Release Bundle + AQL 六域随域 + 报表——每个都须 ADR 群 + 数据模型 + 回归矩阵），非一个里程碑可承载 | **须 PRODUCT.md「明确不做」修订解禁（用户动作）+ 用户终裁 + 建议专程里程碑不混编** |

### 0.2 v1.1 终裁落章：两程结构（两口径并行采纳、按程拆解）

**v1.0 按口径一起草（推荐）；v1.1 终裁（Q6/Q13，2026-09-02）两口径并行采纳、两程拆解**——口径一落 **M16（本 PRD）**：交互 parity 主轴（批次①~④）+ i18n 双语（Q3）+ cron 调度域（Q1）+ Annotate（Q7）+ 远端浏览 + AQL 副线；口径二落 **M17 预立项段（ROADMAP 新增）**：Builds/Build-info 域 + Release Bundle + 洞察报表 + Federation/Lifecycles——**须 PRODUCT.md 修订 + ADR 群，候 M16 收口后 PM 正式立项**（解禁系产品宪法级变更，不与 parity 收口混编——M14「交互完全一致不解锁功能本体」防线在两程边界上延续：M16 内仍不建功能本体，M17 专程建）。v1.0 三条理由（UI 加码信号主诉在交互面 / 指令②指向前端全域 / 宪法级变更须专程）全部被两程结构吸收。**A8 stay-out 清单**（Artifactory 无对位 / 对位即要避开的形态）在 M16 内逐项确认维持或收敛（Q9）。

### 0.3 A1 域处置：已终裁进 M17 预立项段（v1.1——Q6 两程结构）

以下 A1 族**已终裁「全部进」但落 M17 预立项段**（ROADMAP 新增段）——本 PRD 不承载、不排期、不设 FR；解禁路径 = **PRODUCT.md 修订（用户动作，M16 期间不发生）+ M17 正式立项（候 M16 收口后 PM 起草）+ ADR 群**，且 Build-info 域 / Release Bundle 域 / AQL 六域（build/module/dependency/promotion/releasebundle/sensitive）/ `/api/search/license·dependency·buildArtifacts` / D3 依赖树 / Module ID 字段 / Any Distribution 预置**随域联动**（M16 内维持「无数据源不伪造」——各 FR 中的 stay-out 条目在 M17 域落地后解除）：

- **M17 预立项段主目**（conductor 两程结构指令列定）：Builds / Build-info 域、Release Bundle 域、洞察报表 / 趋势分析图表、Federation / Lifecycles（含 Release Lifecycle 细分面）；
- **追加候补**（A1 同族、本轮指令未点名——M17 立项稿定容时裁）：Repository Path Map 管理面、HA 高可用本体、Artifactory 全量 REST 兼容（高频子集承诺的退让边界）。

**Xray 集成面为 intake ⑤ 明示的唯一维持不做项**（漏洞扫描/合规平台/license 识别 licences.xml 91 模式等 xray_tied 族——审计已从翻案清单剔除；Q6 终裁不改变其排除地位）。

### 0.4 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-09-02 | 初版立项稿（待 conductor 审 + 用户确认清单终裁）：§0 范围定界（C13 两口径，按推荐口径 = 控制台交互 parity 收口起草，A1 扩张选项隔离 §0.3）；主轴四批次 FR-142~FR-145（逐批附 B-x.y 偏差条目引用）+ 后端配合小域 FR-146 + 远端浏览 FR-147 + AQL 高级面副线 FR-148 + 前置锚 FR-141；契约矩阵 LC-80~LC-96 估 17 条（A 14 / C 1 / 待裁 2）；L35~L46；K67~K72；用户确认清单 Q1~Q13（必答七项 + 可后裁六项）；随稿完成 ROADMAP M16 立项行 + 当前里程碑头切换（PM 职责内两处，沿 M13/M14/M15 v1.0 先例——头内保留 M15 收尾状态可见） |
| v1.1 | 2026-09-02 | **七项终裁落章 + 两程结构重排（conductor 回填指令——用户四项 Q1/Q3/Q4/Q6 + conductor 三项 Q2/Q5/Q7）**：① §0 改写（两程结构：M16 交互 parity + i18n + cron 域 / M17 预立项段——§0.2/§0.3）；② 新增 FR-149 i18n 双语可切换（Q3——E6 翻案）+ FR-150 cron 调度域（Q1——推翻 M15 Q5 终裁留痕，ADR-0044 占位）+ FR-144.7/145.1/145.7/146.1 候裁解除（Q4 页码 / Q5 路由化 / Q7 Annotate 加 / Q2 收紧不倒退）；③ 契约矩阵 LC-88/LC-91 待裁归 A + 新增 LC-97 i18n（C）/ LC-98 分页（A）——计数 A 17 / C 2 / 待裁 0；④ §5.4 新增断言反转⑥⑦（E6 双语 / cron 推翻）+ L47/L48 + NFR/S 增补；⑤ §7 七项标已裁+出口（Q13 随 Q6 落定）；⑥ ROADMAP M16 立项行更新 + **M17 预立项段新增** |

---

## 1. 背景与目标

### 1.1 背景

M15 收尾中（T-423 busy 专项在途 + T-429 release → T-430 终验 → m15-done 收口窗；AQL 六环 / 老搜索 / 搜索面 / virtual 聚合 / 复制包 B / 文档票已归档——19/25 done 基线）。用户 intake ⑤（2026-09-02 00:1x）在 M15 尾波投下 M16 定向三指令（原文见 §PRD 状态引言），conductor 即发起 **M16 全量审计 workflow**（不做清单三源枚举 + t226 逐页活体对照，树为最高优先），产出 `reports/m16-parity-audit-material.md` 四件套——本 PRD 的直接素材。M16 面对五股输入的汇合：

1. **树偏离主诉（P0 先行复查区）**：M14/M15 两程 parity 后，审计坐实制品树栈四项 logic 偏差（B-1.1~1.4）——左树 folders-only 无文件叶子（仅含文件的目录渲染误导性「（空）」占位，`ArtifactsBrowser.tsx:1270`）、选择即展开（Select ≠ 纯 select，:1106）、页签不进 URL + 文件是 `?focus=` 查询参数非路径段（`NodeDetail.tsx:67/:267-274`——Artifactory = `/ui/repos/tree/<TAB>/<repo>/<path>`，深链不可分享）、树头工具带（包类型 facet / rclass 组 / Sort-by / 紧凑视图 / My Favorites）全缺（仅单个过滤文本框 :498-519）。「严重偏离」的用户判断与审计结论一致——批次① P0 的依据。
2. **全前端对齐（B 偏差清单 47 项）**：logic 11 / visual 17 / minor 19，t226 逐页实测带代码行锚——其中仓库表单族（表单藏字段：maxUniqueSnapshots / repoLayoutRef / blackedOut / archiveBrowsingEnabled 全表单 grep=0 但 PUT 全收——API 可达控制台不可达）、详情字段族（File URL / Downloads 族 / 属性编辑解剖）、安全/shell 族（用户/组内联卡 vs 路由整页、权限动词无 Annotate、监控仅存储页）为主要收口面。
3. **不做清单翻案（A 清单 8 域 66 项，除 Xray）**：翻案语境下 E1~E7 豁免族 / parity §9 永不建 / 各 PRD Non-goals / 滚程项全部重开复审——但**翻案 ≠ 默认推翻**：与 conductor 既有裁定冲突处（cron 双轨 T-402a 勘误已裁不引入）与锁死项（E1 删除防线 / E6 中文）必须列冲突点交用户确认（C 表 13 项 → §7 Q1~Q13）。
4. **M15 终裁承接（双源对账腿）**：Q4 出口 C 批 1（远端浏览可选档 helm+deb+rpm，~3 票 M16 登记）+ Q6（docker virtual 开禁，T-431 若 m15-done 前未插空滚 M16 首票）+ Q5 已终裁不引入 cron（intake ⑤ 语境下如重开须列冲突点——正是 C1/Q1 的样板情形）+ AQL 高级面（M15 §5.7 全景表 M16 行：statistics/usage 域〔dep per-node 下载计数基建〕/ QRL 全量 / UI 搜索族 / dates/creation）。
5. **容量与让位（D 骨架）**：Replay+outbox 行级 REST、virtual 同型全包型推广、Go/Terraform/GitLFS/AI-ML 包型、Cleanup/冷存储等继续滚 M17+（dev-go-core/httpapi 容量被主轴 Annotate / 统计基建 / 条件 cron 域占用）；HA/Xray/Build-info 族维持 PRODUCT.md 门。

**v1.1 裁定注（2026-09-02）**：必答七项终裁落章——Q1 引入 cron 调度域（**推翻 M15 Q5 终裁**——intake ⑤ 冲突点确认机制的首次兑现，BOARD 留痕）、Q3 双语可切换（E6 锁死项翻案——非倒退而是超集：中文包维持 + en 包对齐）、Q4 页码控件翻正（×9 处）、Q6/Q13 两程结构（产品域进 M17 预立项段）、Q2 E1 修文本+收紧不倒退、Q5 路由化、Q7 Annotate 加；主轴四批次不变，新增 i18n（FR-149）与 cron 调度域（FR-150）两个独立域。

**不贪多**：M16 以「一条主轴（四批次 FE 收口 + BE 配合小域 + i18n + cron 调度域）+ 一条副线（AQL 高级面——M15 既定第一顺位，视 lane 容量裁剪）+ 一个远端浏览实现段 + 一个前置锚（Q 终裁 + parity 册 v1.2 + ADR-0044）+ 条件小票池」为形态；余下可后裁项（Q8~Q12）按 §7 建议裁点推进，终裁前对应断言挂「候裁」。

### 1.2 M16 目标与量化门槛

> 一句话：把「与 Artifactory 完全一致」从口号变成**逐条可审计的收口账**——B 偏差 47 项全数处置（翻正 / 豁免翻案 / stay-out 确认 / 候裁挂起四态），制品树栈四项 P0 兑现（文件叶子进树、Select 即所见、URL 可分享、树头工具带齐），8 个已实现包型解除 pro 门，统计基建一鱼两吃（详情字段族 + AQL usage 域），豁免翻案全部走显式 Q 裁定零默默翻转。

量化门槛（未达即里程碑不完成）：

| 指标 | M16 门槛 | 来源 |
|---|---|---|
| 树栈 P0 四项 | 文件叶子进树（「（空）」误导占位退役）/ 单击选择不展开（expand 独立于 select）/ 页签进 URL + 文件选择路径段化（深链可分享可重开）/ 树头工具带（包类型 facet + rclass 组 + Sort-by + Compacted 单选 + My Favorites）——Playwright 逐项断言绿 + 10,291 节点仓首屏 p95 不显著回退（基线 734ms） | FR-142 |
| B 偏差收口率 | 47 项逐条四态归属（翻正 / 豁免翻案〔Q 裁定留痕〕/ stay-out 确认 / 候裁挂起〔Q 编号在册〕）——收口审计表归档，零无主项 | §5.4 / DoD |
| 表单栈 | Basic/Advanced/Replications 三段结构 + 表单域提交后 PUT body 逐字段断言（maxUniqueSnapshots/repoLayoutRef/blackedOut/archiveBrowsingEnabled/Environments/Force Auth/Suppress POM）+ dirty-gating + 入口分路由；**8 包型开禁后八型真实客户端 roundtrip**（go/nuget/cargo/conan/helm/helmoci/rpm/debian——既有回归矩阵复用） | FR-143 |
| 统计基建与字段族 | curl 下载 N 次 → FileInfo `downloads=N` + `last_downloaded_by` 正确 + `/api/search/usage` 命中同值 + AQL statistics 域查询绿（一鱼两吃单源） | FR-146/148 |
| 详情/搜索栈 | 页签序（权限在属性前）/ File URL 复制 / Downloads/Last Downloaded 族渲染 / 属性编辑解剖 / 下载形态 / 日期格式 / 搜索列集（Artifact-name 链接/Path/Repository/Modified + 选择列）/ 行导航仅 name 单元格 / 快搜空历史占位 / **页码控件 ×9 处统一（Q4 已裁翻正）**——逐项 Playwright 断言 | FR-144 |
| 安全/shell 栈 | 用户/组创建路由整页表单（/users/new /groups/new 可深链）/ 权限两步弹窗（Any Local/Any Remote 预置）/ 能力位三旗 / profile 自助 token / 监控 System Logs + Service Status / 帮助下拉 + About——逐项断言 + 权限数据迁移零回归（**Q7 已裁加 Annotate**） | FR-145/146 |
| 远端浏览 | 可选档 on → 树回源枚举远端目录（helm index / deb / rpm）且 off → 缓存浏览既有断言零回归；上游不可达降级语义在案 | FR-147 |
| AQL 副线 | statistics/usage 域 + `/api/search/usage` + QRL 三态 REST + UI 搜索族 + dates/creation（§5.7 全景表 M16 行逐条对账——未实现端点仍 404） | FR-148 |
| i18n 双语切换 | 语言切换器（zh↔en）全站生效 + 文案外提 100%（组件零硬编码中文——CI 断言脚本）+ en/zh 两包（en 术语对齐 Artifactory）+ 切换持久化 + 断言双语化策略（默认 locale 全量 + en 抽样腿） | FR-149 |
| cron 调度域 | 调度数据模型 + cron 表达式子集 + next-run 计算 + 三消费面（GC 定时〔含 Cleanup 两族〕/ 备份定时 CRUD / 复制配置 cron 字段）+ 与事件驱动引擎并存零重复投递 + audit 三事件 | FR-150 |
| 资源与预算门 | footprint ≤100MB / check-size ≤120MB / 冷启动 <2s 三连维持；SPA 每票增量 ≤10KB（四批次 FE 量大——累计趋势观察项登记）；F1 六平台聚合趋势登记 | §6.2 |

### 1.3 上游依赖与并行关系（含依赖声明与分票提示）

- **前置产物（拆票前/B1 首波）**：① **Q 终裁包**（conductor 组织用户确认——**v1.1：必答七项已落章**；余 Q8/Q9 建议在批次②①断言冻结前裁、Q10~Q12 可尾波）；② **parity 册 v1.2**（FR-141 承载——E5 前提修正 / E1 范围修正 / 新增豁免与 stay-out 登记 / reverse §3.2 facet 回填 / **v1.1 三例翻案条目落笔〔E6 双语 / E2 页码 / cron 推翻〕**）；③ **规格增量段**（reverse-engineer：aql.md statistics/usage/QRL/dates 增量段 + remote-browsing.md〔T-425 §1/§2 可直接成稿〕+ 树头工具带 facet 形态锚 + cron 表达式子集锚〔Artifactory Quartz 对拍〕——审计材料 B 项已带代码行锚与 t226 实测，规格票做置信度归档与断言冻结）；④ **ADR-0044（编号占位——cron 调度域）**：调度数据模型 / cron 表达式子集 / next-run 计算 / 与事件驱动 + 1min sweep 引擎并存语义（调度只触发全量类任务、零重复投递）/ 误触发防护——architect，FR-150 拆票前 Accepted 目标。
- **依赖声明（本 PRD 显式三条 + Q 联动五条）**：
  - **批次③ Downloads/Last Downloaded 字段族 dep FR-146.2 per-node 下载计数基建**——与 AQL usage 域（FR-148.1）**共基建一鱼两吃**（A2 素材原文口径）；基建先行（B 波）、字段族与 usage 域消费（后波）。
  - **批次② 的 8 包型开禁为纯前端门**——后端八型已实现（go/nuget/cargo/conan/helm/helmoci/rpm/debian），零 BE 依赖；开禁后复用 13 包型既有回归矩阵冒烟；addon 槽定义若需调整走 ADR-0033 增补（票内核对）。
  - **Q5（E5 重裁）联动批次④**——FR-145.1 用户/组路由表单化腿在 Q5 终裁前断言挂「候裁」附注；裁定出口①（路由化）即本 PRD 现稿形态，出口②（修登记维持内联卡）则该腿转为登记票。
  - **Q1（cron）已裁引入（v1.1）**——FR-150 调度域承载（ADR-0044 占位，architect 前置 Accepted；FR-145.7/146.4 消费面转正）；Q2（E1）已裁出口①——批次① children 表收窄 + FR-141 E1 文本修正落定；Q7（Annotate）已裁加——FR-146.1 + LC-88 归 A；Q4（E2 分页）已裁翻正——×9 处页码控件（FR-144.7/LC-98）；Q13（范围定界）已随 Q6 落定两程结构——M17 预立项段承载产品域，M16 主轴零影响。
  - FR-147 dep repo-semantics §8.5 口径扩面（T-412 listVirtual「remote 成员仅缓存行」牵连）+ remote-browsing.md 规格落盘。
- **依赖序**：FR-141（前置锚）→ 批次① FR-142 可 B1 起步（P0——树栈断言中文件叶子/URL 模型两项不候 Q；children 表收窄细节候 Q2/Q9）→ 批次② FR-143（字段域细节候 Q8，主体可并行——area 为 web/src 表单族，与树栈不同文件族）；FR-146.2 统计基建 BE 域（internal/storage + httpapi）与 FE 批次天然错峰，B1~B2 起步 → FR-144 批次③ dep FR-146.2 落地（字段族腿）；FR-145 批次④（Q5 终裁后主体开工，监控/帮助/导航腿零依赖任意波次）；FR-147（BE 侧独立 lane——internal/adapter + repo service）；FR-148 副线 dep FR-146.2（statistics 域）+ aql.md 增量段（QRL/usage/dates 锚）。
- **实现分区与分票提示（宽度 ≤2 内建）**：FR-141 拆 1~2 票（parity 册修订共笔 + 规格增量段归 reverse）；FR-142 拆 2~3 票（文件叶子+选择语义 / URL 模型 / 树头工具带——ArtifactsBrowser.tsx 重载体，票间串行）；FR-143 拆 3~4 票（三段结构+字段域 / 包型 modal+开禁 / 列表与入口+dirty-gating+Test）；FR-144 拆 3~4 票（页签序+元数据字段族 / 属性编辑+下载形态 / 搜索栈 / 日期格式+杂项）；FR-145 拆 3~4 票（路由表单化 / 权限两步弹窗+能力位 / profile+帮助 / 监控面+导航分组）；FR-146 拆 3 票（Annotate〔候 Q7〕/ 统计基建 / Last Login）+ 条件 cron 1~2 票；FR-147 拆 ~3 票（M15 登记口径：三型可选档 + 口径扩面 + FE 树消费）；FR-148 拆 3~4 票（statistics/usage / QRL / UI 搜索族 / dates 顺车）；**FR-149 拆 2~3 票（文案外提集中票〔全站键化——先于/伴随批次票〕+ 双资源包与切换器 + 断言双语化策略）**；**FR-150 拆 2~3 票（调度引擎〔ADR-0044 落地〕+ 三消费面 + audit 与防护）**；QA 两票（逐批 V 式复核 + 终验——含 B 47 项收口审计与双 locale 抽样腿）；tech-writer 1~2 票；release 一票；PM 裁定票一票（Q 终裁联动回写）。估 **30~40 票**（v1.1 增 i18n 2~3 票 + cron 调度域 2~3 票；含条件票 slot：E7 toast / NuGet symbol Q10 / Tokens 核验候源 / license 公钥 ADR / t381 清理 Q12）。
- **area 错峰**：主轴 FE = `web/src` 四批次各占不同页面族（树/表单/详情/安全——tech-lead 排波次防重载体文件撞车：ArtifactsBrowser.tsx 与 NodeDetail.tsx 归批次①③ 分波）；BE 小域 = `internal/httpapi` + `internal/storage`（统计基建/Annotate/usage）与 `internal/adapter/*`（远端浏览）错峰；AQL 副线 = `internal/search` 既有包增量（与 M15 零冲突——引擎不动只扩域）。**注意**：Annotate 动词迁移牵权限数据（users/groups/permissions 三面）——若裁做须 architect 会签迁移方案，不跨包摸内部结构（Go 规范）。
- **QA 并行面**：t226 活体对照腿（树/表单/详情/安全逐页——审计 workflow 已建对照基础，V 式复核复跑关键页）；统计基建需下载编排夹具（多用户多路径下载计数）；Annotate 迁移需权限夹具快照前后对照；远端浏览需 helm/deb/rpm 上游夹具（自指上游可复用 M6 复制夹具）；B 47 项收口审计表 = QA 终验硬 AC。

### 1.4 全程工作方式条款（M11~M15 §1.4 制度延续 + M16 翻案语境专项——常设准绳，全程生效）

1. **翻案纪律（本主轴的宪章级纪律）**：intake ⑤ 翻案语境**不等于默认推翻**——每条豁免/裁定的翻案必须走 §7 Q 表显式裁定并在 parity 册留痕（豁免倒退 = 缺陷的 M14 条款，在 M16 反转为：**豁免翻案 = 改契约，须 Q 裁定 + 册修订双留痕**）；与 conductor 既有裁定冲突处（cron 双轨等）列冲突点交用户，**不默默翻转**（intake ⑤ 原文要求）——**v1.1 三例翻案落章为该纪律的样板**：E6→双语（Q3）/ cron→引入（Q1，含 M15 Q5 推翻留痕）/ E2→页码（Q4），均 Q 裁定 + parity 册 v1.2 双留痕。
2. **范围定界纪律（§0 同源）**：交互 parity 收口不解锁功能本体（M14 防线延续）——Module ID 字段（dep Build-info）/ Any Distribution 预置（dep Release Bundle）/ 洞察图表等**无数据源不伪造**（「无端点列不伪造」纪律同源），登记 stay-out 候 Q6/Q13 扩张随域解禁。
3. **clean-room 红线（A0 永久）**：逐行翻译 Java→Go / 复制 JFrog license 密钥格式 / 像素级复刻 UI 与前端资产 / 官方协议 logo 矢量源复用——零触碰；Artifactory 行为基准 = 审计 workflow 的 t226 活体对照 + 官方文档 + parity 册（置信度列），**不是** reverse-src UI 代码。
4. **低置信不进断言（M14/M15 条款延续）**：审计 B 项虽带代码行锚与 t226 实测，细节断言（facet 复选组形态 / Sort-by 选项集 / 字段文案逐字）仍须 V 式复核后冻结；核验回写 = 改契约（parity 册 v1.2 修订留痕）；效力序：用户裁决（BOARD/Q 表）> parity 册〔v1.2 回写后〕> 本 PRD 暂行值。
5. **ACL 零泄漏（T-92 血统延续）**：全部新端点（usage / QRL / Last Login）与新增字段（`last_downloaded_by` 可见性）与内容面同一 `allow()` 源；越权探针为每票硬 AC——usage 搜索系泄漏面次宽处（仅次于 M15 搜索域）。
6. **流程条款（延续）**：新端点走 PM FR + ADR 流程（本程新增端点族：`/api/search/usage` + QRL REST + UI 搜索族四枚 + dates/creation 两枚——均 §5.3 矩阵与 M15 §5.7 全景表承载，规格增量段随票；cron 域若建须独立 ADR）；拆票宽度 ≤2；FE 票四闸门 + axe 双主题全绿为合入条件；每票 AC 附可执行验收命令（Playwright spec 名或真实客户端命令）。

---

## 2. 范围

### 2.1 In scope

| # | 来源（指令/审计/登记） | 本 PRD 功能需求 | 优先级 |
|---|---|---|---|
| A | intake ⑤ 指令①③ + 审计 C 表 + D 骨架前置 | FR-141（前置锚：Q 终裁回写 + parity 册 v1.2〔E5 前提修正/E1 范围修正/stay-out 登记〕+ reverse §3.2 facet 回填 + 规格增量段） | P0（前置锚） |
| B | intake ⑤ 指令① + B-1.1~1.4（树栈 P0 四项坐实） | FR-142（批次① 制品树栈：文件叶子进树 / 选择≠展开 / URL 模型段化 / 树头工具带 + children 表收窄〔Q2/Q9 联动〕） | P0 |
| C | B-1.5 + B-2.5/6 + B-3.6/7/8/9/11/12 | FR-143（批次② 仓库管理表单栈：三段结构 / 字段域补齐 / 包型 modal 880 + 8 包型开禁 / 列表列集与入口拓扑 / dirty-gating / remote Test） | P1 |
| D | B-2.1~4/7~11/13/14 + B-3.14/15/16 | FR-144（批次③ 详情/搜索栈：页签序 / File URL / Downloads 字段族〔dep 统计基建〕/ 仓库目录元数据 / 属性编辑解剖 / 下载形态 / 日期格式 / 搜索列集/行导航/快搜） | P1 |
| E | B-1.7/8/11 + B-2.15~18 | FR-145（批次④ 安全/shell 栈：用户/组路由表单化〔**Q5 已裁路由化**〕/ 权限两步弹窗 / 能力位 / profile 自助 / 监控 System Logs+Service Status / 帮助下拉+About / 导航分组——GC/备份 cron 消费面转归 FR-150） | P1（监控/帮助 P2） |
| F | D 骨架后端配合 + A2/A6 部分翻案 | FR-146（后端配合小域：Annotate 动词与 write 拆分〔**Q7 已裁加**〕/ per-node 下载计数基建〔一鱼两吃〕/ Last Login 派生——cron 调度域拆出归 FR-150） | P1（Last Login P2） |
| G | M15 Q4 终裁承接（LC-76 实现段） | FR-147（remote 远端浏览可选档：批 1 = helm classic + deb + rpm，`listRemoteFolderItems` 对位，默认 false 维持缓存浏览） | P1 |
| H | M15 既定第一顺位 + A2 域 | FR-148（AQL 高级面副线：statistics/usage 域 + `/api/search/usage`〔dep FR-146.2〕+ QRL 全量三态 + UI 搜索族 + dates/creation） | P1（P0 腿 statistics/usage；UI 族/dates P2 顺车） |
| **K** | **用户裁决 Q3**（intake ⑤ E6 翻案——双语可切换） | FR-149（i18n 双语可切换：框架接入 + 文案外提 100% + 中英两包 + 语言切换器 + 断言双语化策略〔§4.9〕） | P1 |
| **L** | **用户裁决 Q1**（推翻 M15 Q5——intake ⑤ 冲突点确认机制兑现） | FR-150（cron 调度域：调度数据模型 + 表达式子集 + next-run + 三消费面〔GC 定时/备份定时/复制 cron 字段〕+ 与事件驱动并存语义〔§4.10〕；ADR-0044 占位） | P1 |
| I | 条件池裁量（不设 FR） | 条件小票池：NuGet symbol 六承转正〔Q10〕/ docker virtual 开禁（T-431 滚入首票）/ E7 toast 锚位（候用户信号）/ L1 列选器推广（E-04 + users/groups/permissions + Users Last Login 列——A6 部分翻案）/ license 公钥 config 覆盖 ADR / t381 事故残留清理〔Q12——conductor 决定〕 | —（未触发不构成 DoD 缺口，BOARD 留痕） |
| J | A1 域（Q6 已裁进——两程结构落 M17） | **M17 预立项段承载**（§0.3/ROADMAP）：Builds/Build-info 域 + Release Bundle + 洞察报表 + Federation/Lifecycles（追加候补：Repository Path Map / HA / 全量 REST）——须 PRODUCT.md 修订 + ADR 群，候 M16 收口后 PM 正式立项；M16 零承载 | — |

前置产物（非 FR 行）：Q 终裁包（= FR-141.1 承载）、parity 册 v1.2（FR-141.2）、规格增量段三份（aql.md 增量 / remote-browsing.md / 树头工具带锚——随 FR-141/147/148 票）。

### 2.2 Non-goals — M16 明确不做

**产品级（§0.3——Q6 已裁两程结构：产品域扩张落 M17 预立项段，M16 不承载）**：Builds/Build-info 域、Release Bundle 域、洞察报表/趋势图表、Federation/Lifecycles（含 Release Lifecycle）——须 PRODUCT.md 修订 + ADR 群，候 M16 收口后 PM 正式立项（ROADMAP M17 预立项段在案）；HA 本体 / 全量 REST / Repository Path Map 为追加候补（M17 立项稿定容）。**Xray 集成面 = intake ⑤ 明示唯一维持不做**。

**M16 里程碑级 Non-goals（含让位与登记——去向全部留痕）**：

| 不做项 | 隔离边界 / 去向（PM 判定理由留痕） |
|---|---|
| **A0 永久红线族** | clean-room 三线（逐行翻译 / 密钥格式 / 像素复刻与 logo 矢量源）+ nuget/conan/docker 鲸腹三枚低置信图标商业化分发前商标复查——零触碰，仅登记不议 |
| **Module ID 字段 / Any Distribution 预置** | dep Build-info / Release Bundle 域（M17 预立项段）——M16 内无数据源不伪造；随 M17 域解禁 |
| **Replay + outbox 行级 REST 面** | 滚 M17+ 维持（D 骨架容量判定：httpapi/dev-go-core lane 被主轴 Annotate/统计基建/条件 cron 占用）；机制已备翻转面小不返工 |
| **virtual 成员同型全包型推广**（T-367） | 滚 M17+ 维持——13 包型 × 三 rclass 回归矩阵，容量让位主轴；现态缺陷面窄 |
| **Go 深化 / Terraform / GitLFS / AI-ML 13 型包型** | 滚 M17+（A4 域——包型本体扩张非 parity 面；8 包型开禁系「已实现解锁」不同族） |
| **Cleanup-Retention 策略引擎 / 冷存储分层** | 滚 M17+（A3 域——策略引擎本体）；**v1.1 注**：GC 维护面 cron 字段与备份定时 CRUD 已随 Q1 终裁转正入 FR-150（调度消费面），策略引擎本体仍滚 M17+ |
| **deb bz2 压缩档** | ruled-out 维持——唯一翻案通道 = 用户提供纯 Go bzip2 写入器实现路径 |
| **Smart Searches（保存搜索）** | pro 档 addon 槽——不对齐（M15 §5.2 既定）；UI 搜索族四端点不含保存面 |
| **协议物理不可行面** | npm/pypi 根 / goproxy / cargo / conan 根树远端浏览——协议无根级枚举 API，做成即假树（M15 未纳入项明确不做面，intake ⑤ 翻案不覆盖）；docker tags 远端浏览首采被否维持（Artifactory 官方设置面未开放该型——做即超 parity） |
| **maven/generic HTML 目录抓取族** | 官方未写算法，中置信无锚——不做（Q4 终裁附带结论维持） |
| **by-digest 拉取强刷**（Q7/M15） | 维持 as-built 登记（TTL 统一）——缓存语义细面非前端对齐主诉，用户点名才开票 |
| **Tokens 生成表单字段集补核验** | 候商业版/云实例活体源条件票维持（源可得即翻正为高置信规格）——不排期 |
| **t381 事故残留清理**（VM 快照 15MB + 空仓） | conductor 决定项（非 PM 裁量）——Q12 登记，建议尾波随 release 票处置 |

---

## 3. 用户与场景（M16 视角）

- **场景 A（Artifactory 迁移用户，制品树）**：树里直接见文件叶子（不再「右侧表格二段跳」）、点仓库名只选中不炸开、浏览器地址栏可复制分享「某仓某页签某文件」深链、树头勾包类型/rclass 过滤 + 排序 + 收藏——「严重偏离」主诉的四项收口。
- **场景 B（仓管理员，表单栈）**：Basic|Advanced|Replications 三段步进（复制配置不再是内联子表单）；blackedOut / snapshot 上限 / repo layout 等字段不再「API 有 UI 无」；conan/rpm/debian 等 8 型建仓不再被 pro 标记挡路；进编辑页未改东西时 Save 灰置。
- **场景 C（制品消费者/审计员，详情与统计）**：文件详情见 Downloads / Last Downloaded / Last Downloaded By；AQL 一句话答「哪些制品 90 天无人下载」（statistics 域 + `/api/search/usage`）——统计基建一鱼两吃。
- **场景 D（安全管理员）**：权限矩阵五动词（含 Annotate——候 Q7）、Any Local/Any Remote 预置两步弹窗、用户能力位三旗（Can Update Profile / Disable UI Access / Disable Internal Password）、profile 自助生成 token 不再跳 admin 页。
- **场景 E（运维）**：System Logs 查看器 + Service Status 页；（候 Q1）GC 维护面 cron 调度与定时备份 CRUD。
- **场景 F（QA/规格维护者）**：parity 册 v1.2 = 豁免翻案后的新基线（E5 前提修正 / E1 范围修正 / stay-out 登记）；B 47 项收口审计表逐条可查四态归属；每条豁免翻案均有 Q 编号与 BOARD 留痕可回溯。

---

## 4. 功能需求

约定：`BASE=http://127.0.0.1:8080`；`ADMIN=admin:password`；FE 验收 = Playwright spec（`web/e2e/m16/`，沿 M8/M14 目录先例）+ 四闸门 + axe 双主题；BE 验收 = curl 真实命令；**Artifactory 行为基准 = 审计材料 B 项（t226 逐页实测 + 代码行锚）+ parity 册 v1.2（置信度回写后）**，效力序见 §1.4 条款 4；候裁项（Q 编号在册）终裁前断言挂「候裁」附注，**不裁不建**。

### 4.1 前置锚：范围定界落章 + parity 册 v1.2 + 规格增量段（P0）

#### FR-141 Q 终裁回写 + parity 册 v1.2 修订 + reverse §3.2 回填（PM + ux-designer + reverse-engineer 共笔）

**用户故事**：
- 作为本团队，翻案语境下每条豁免的推翻必须有据可查——Q 表终裁结论回写 PRD §7 与 parity 册，形成「旧豁免 → Q 编号 → 新形态/新登记」的完整审计链。
- 作为后续里程碑的消费者，parity 册 v1.2 是豁免翻案后的唯一豁免基线（E5 前提已被 V6 实测推翻、E1 文本范围与 as-built 矛盾——两处不修则全流程断言失锚）。

行为规格：

- **141.1 Q 终裁回写**：§7 Q1~Q13 逐项归位（终裁落章 / 维持暂行 / 撤销归位三态——沿 M15 v1.1 §7 体例）；翻案项在 parity 册对应豁免条目标注「翻案（Q-x，2026-09-xx）」——**豁免翻案 = 改契约双留痕**（§1.4 条款 1）。
- **141.2 parity 册 v1.2**：① E5 前提修正（「BinFlow 已用路由实体表单」依据被推翻——条目改写为 Q5 终裁出口）；② E1 范围修正（Q2 出口落笔——管理列表 vs 浏览器表分治文本）；③ 新增豁免/stay-out 登记（A8 清单确认后：仓库详情中间页/children 表/E4 测试器/快搜范围页签缺位/结果计数一致性等逐项登记）；④ E2/E6/E7 翻案或维持的对应条目更新。
- **141.3 reverse §3.2 facet 回填**：Filter-by / Sort-by facets 漏记补笔（B-1.4 附带发现——逆向规格欠账随本程清偿）；树头工具带断言锚（facet 复选组形态 / Sort-by 选项集 / 收藏持久化——V 式复核后冻结入 K67）。
- **141.4 规格增量段**：aql.md statistics/usage 域 + QRL + dates/creation 增量段（M15 §5.7 M16 行的断言地基）；remote-browsing.md 新建（T-425 §1/§2 直接成稿——归 conductor 编排确认）。

验收标准（AC）：

- **AC1（裁决归位）**：§7 Q1~Q13 逐项带终裁结论与依据锚归位（v1.1 回写）；翻案项 parity 册留痕 100%（抽查：每条翻案可回溯 Q 编号 + BOARD 行）。
- **AC2（册修订）**：parity 册 v1.2 落盘（版本递增 + 修订记录）；E5/E1 两处矛盾文本清零；reverse §3.2 facet 条目在册；`make docs` 零断链。
- **AC3（就绪度）**：tech-lead 就绪度确认（四批次 + 副线可拆性 / 裁决点清单 / 缺项是否阻塞）。

### 4.2 批次① 制品树栈（主轴 P0——tree-deep 先行）

#### FR-142 文件叶子进树 + 选择≠展开 + URL 模型段化 + 树头工具带（web/src；B-1.1~1.4 全量）

**用户故事**：
- 作为 Artifactory 迁移用户，左树展开即见文件（与 Artifactory 同形态）；单击仓库名/目录只是选中（展开由 chevron 控制）；复制浏览器地址能给同事一个「页签 + 仓库 + 路径 + 文件」全态深链；树头能按包类型/rclass 过滤、排序、切紧凑视图、收藏仓库。
- 作为深链消费者，`?focus=` 查询参数退役为路径段——URL 即状态（刷新/重开/分享三态一致）。

行为规格：

- **142.1 文件叶子进树（B-1.1）**：树节点含文件叶子（filter `n.folder` 移除——`ArtifactsBrowser.tsx:1270`）；仅含文件的目录不再渲染「（空）」占位（:1272 症状连带消除）；children 表处置随 Q9 + **Q2 已裁出口①**（E1 文本范围收窄〔管理列表 vs 浏览器表分治——FR-141.2 承载〕+ 浏览器面行内删除件收紧：红色「删除」钮走确认/收进详情，**E1 不倒退**）；大目录性能护栏维持（TREE_LEVEL_CAP=300 / BIG_DIR=2000 警示——文件节点计入后的预算复核归票内）。
- **142.2 选择≠展开（B-1.2）**：单击 = 纯 select（`isOpen = expanded.has(key) || selectedRepo === repo.key` 的强制并集 :1106 解除）；展开独立（chevron/双击）；深链祖先链自动展开维持（对齐项不动——含被选目录自身的链语义复核）。
- **142.3 URL/状态模型（B-1.3）**：活跃页签进 URL 路径段（Artifactory `/ui/repos/tree/<TAB>/<repo>/<path>` 对位——BinFlow 路由形态归实现票，语义对齐：页签即段、文件即路径非查询参数）；`?focus=` 退役为路径段（旧 URL 301/兼容映射一轮——书签不猝死）；刷新/重开/分享三态一致。
- **142.4 树头工具带（B-1.4）**：包类型 facet 复选组 + Local/Remote/Cache/Virtual 复选组 + Sort-by 下拉 + Compacted/Non-Compacted 单选 + My Favorites；与既有「过滤仓库…」文本框并存（Artifactory 形态）；选项集与形态断言候 K67（V 式复核冻结）。
- **142.5 附带收口**：初始态首仓库自动选中 + item view 呈现（B-3.2——对齐）；右键菜单集差异维持大半豁免（Pro 门控 + E1——B-3.1）；Deploy 对话框居中 vs 抽屉化（决策项 C——M14 终裁窗遗留开放项）随本批次断言冻结前裁。

验收标准（AC）：

- **AC1（文件叶子）**：Playwright——含文件目录展开见文件叶子（非「（空）」）；仅含文件目录非空断言；文件叶子点击 → 右侧 item view（General/页签族）；children 表收窄后（Q9 出口落定）文件主路径可达断言。
- **AC2（选择语义）**：Playwright——单击仓库名：选中态变化 + 展开态不变（expand 断言前后对照）；chevron 展开/收起独立；深链自动展开祖先链维持（既有 spec 零回归）。
- **AC3（URL 模型）**：Playwright——页签切换 URL 段变化；文件选择后 URL 含文件路径段（非 `?focus=`）；copy URL → 新 context 打开 → 页签/仓库/路径/文件四态一致；旧 `?focus=` URL 兼容映射一轮。
- **AC4（工具带）**：Playwright——包类型 facet 勾选过滤（仅勾 docker → 树仅 docker 仓）；rclass 组过滤；Sort-by 切换序变；Compacted 切换形态；My Favorites 标记持久（reload 保持）。
- **AC5（性能与回归）**：10,291 节点仓首屏 p95 ≤ 734ms 基线不显著回退（文件节点计入后的实测归档）；`artifacts-tree` 既有 10 spec 翻新零断链；四闸门 + axe 双主题。

### 4.3 批次② 仓库管理表单栈（P1）

#### FR-143 三段结构 + 字段域补齐 + 包型 modal 880 与 8 包型开禁 + 列表/入口/dirty-gating/remote Test（web/src + 表单域行为联动；B-1.5 + B-2.5/6 + B-3.6/7/8/9/11/12）

**用户故事**：
- 作为仓管理员，建仓/编辑表单与 Artifactory 同构：Basic | Advanced | Replications 步进条，字段不再藏（API 可达 UI 不可达的四个字段补位），包类型选择器 880px 全景、已实现的 8 个包型不再带 pro 禁用标。
- 作为谨慎的操作者，未修改任何字段时 Save 不可点（dirty-gating）；remote 仓配置期能 Test 上游连通性。

行为规格：

- **143.1 三段结构（B-2.5）**：Basic | Advanced | Replications 步进条；复制配置从内联「＋新建复制配置」子表单移第三步（M6 既有复制配置能力迁移载体，语义零变化）。
- **143.2 字段域补齐（B-1.5 + B-3.12，候 Q8）**：maxUniqueSnapshots / repoLayoutRef / blackedOut / archiveBrowsingEnabled（PUT `/binflow/api/repositories` 已收——`internal/httpapi/repositories.go` L55-61，表单补位 + 回显）；Environments 多选 / Public·Internal 描述拆分 / Force Authentication（virtual）/ Suppress POM Consistency Checks（maven）；字段行为联动（blackedOut=true 拒写语义 / repoLayoutRef 与 layout 解析关系）归票内实现 + K 项登记（repoLayoutRef 行为联动评估）。
- **143.3 包类型弹窗（B-2.6）**：modal 880px 居中（M1 锚点 440px 紧凑档升级——parity 册 M1 行再修订留痕）；tiles 维持 BinFlow 实有 13 型（**8 个已实现包型解除 pro 禁用标——纯前端门**：go/nuget/cargo/conan/helm/helmoci/rpm/debian）；Artifactory ~33 型全量开放 dep 包型本体（§2.2 A4 让位）——modal 形态对齐、型录维持实有（不伪造未实现型）。
- **143.4 列表与入口（B-3.8/9/11）**：创建入口拓扑 = Add Repositories 下拉（Local/Remote/Virtual 预选）+ rclass 分路由（表单内 rclass 控件移除）；仓库列表列集对齐（缺 Project/Environment/Shared With 列的处置——Project 系 Artifactory 项目域概念，BinFlow 无域则缺位登记不伪造；冗余「类型」列收敛候 Q9）；Remote 页签 Replications 列（push-only 模型呈现——ADR-0021/R10 口径注记）；表单 footer 重置钮处置随 Q9（PM 倾向移除——对齐 M1 锚点 Cancel + Create/Save）。
- **143.5 dirty-gating + remote Test（B-3.7 + B-3.6）**：编辑表单进入即 Save disabled，变更后 enabled；remote 表单 Test 连通性（上游可达 + 认证探测，零副作用——FR-138.2 Engine.TestTarget 同构复用）。

验收标准（AC）：

- **AC1（三段+字段域）**：Playwright——步进条三段导航；blackedOut 勾选提交 → `curl GET /api/repositories/<key>`（或对位配置端点）回显 `blackedOut:true` + 仓拒写行为断言；maxUniqueSnapshots/repoLayoutRef/archiveBrowsingEnabled/Environments/Force Auth/Suppress POM 逐字段提交-回显-行为三链（表驱动 spec）。
- **AC2（包型 modal + 开禁）**：Playwright——modal 宽度档断言（880px 居中）；8 包型 tile 无禁用态；逐型经控制台建仓 + 真实客户端 roundtrip（go/nuget/cargo/conan/helm/helmoci/rpm/debian——`go install`/`dotnet nuget push`/`cargo publish`/`conan upload`/`helm push`/`helm push oci://`/`dnf`/`apt` 既有八型回归矩阵复用跑绿）。
- **AC3（入口与列表）**：Playwright——Add Repositories 下拉三预选分路由深链（/new 直链兼容映射）；列表列集断言（含 Remote 页签 Replications 列呈现）；行操作超集维持已豁免（L2/E1——零倒退断言）。
- **AC4（dirty-gating + Test）**：Playwright——进入编辑 Save disabled / 变更后 enabled / 无变更提交不可达；remote Test 三臂（正确凭据成功 / 错误凭据内联失败 / 不可达超时呈现）+ 零副作用断言（上游只读接触）。

### 4.4 批次③ 详情/搜索栈（P1；Downloads 字段族 dep FR-146.2）

#### FR-144 页签序统一 + File URL + Downloads 字段族 + 元数据补齐 + 属性编辑解剖 + 搜索栈（web/src；B-2.1~4/7~11/13/14 + B-3.14/15/16）

**用户故事**：
- 作为制品消费者，详情页签序与 Artifactory 一致（权限在属性前）、File URL 一键复制、Downloads/Last Downloaded 族在 General 页可见、属性编辑常显输入框 + Add（不再是隐藏表单 + 逐行小钮）。
- 作为搜索用户，结果列 Artifact(name 链接)|Path|Repository|Modified + 选择列；行导航仅 name 单元格深链；快搜空历史有「No recent searches yet」占位；日期格式带时区偏移。

行为规格：

- **144.1 页签序统一（B-2.1/7）**：General / Effective Permissions / Properties 顺序（权限在属性前——Artifactory 序）；Followers/Xray 维持 Non-goal 豁免（§0.3）；逐级页签集差异收敛（节点级/仓库级统一形态，admin-only 权限页签维持门控）。
- **144.2 文件元数据字段集（B-2.3/10）**：File URL + 复制钮 / Downloads / Last Downloaded / Last Downloaded By / Remote Downloads（**dep FR-146.2 统计基建**——字段渲染与数据端到端）；Package Information·Dependency Declaration / Virtual Repository Associations / Included Repositories 块（virtual 关联块 = 成员清单呈现——数据源既有 listVirtual，形态对齐）；多出的 mimeType/checksum 徽标/下载校验块处置随 Q9（自有增强去留——PM 倾向校验能力收进下载伴随菜单）。**Module ID 不建**（dep Build-info——§2.2 stay-out）。
- **144.3 仓库/目录元数据（B-2.4）**：仓视图补 Repository Layout / Description / Created / Artifact Count·Size(Show)；目录视图补 File URL；多出的类型/子项/修改时间列处置随 Q9。
- **144.4 属性编辑解剖（B-2.9）**：常显 Property/Value 输入 + Add 按钮 + 网格搜索（隐藏「+ 新增属性」表单 + 逐行 ✎/🗑 收敛——行内删除动作与 E1 的关系随 Q2 出口统一处理）；Property|Property Set 分段 = **候裁小域**（BinFlow 无 property set 域——M10 属性系统无 set 概念；裁做须 BE 属性集配置小域随 FR-146 扩列，不做则分段缺位登记 K68 关联）。
- **144.5 下载形态（B-2.12）**：单 24px 图标钮（两个带文字按钮收敛——校验能力收进伴随形态，处置随 Q2/Q9）。
- **144.6 搜索栈（B-2.11/13/14 + B-3.14/15/16）**：结果列集 = Artifact(name 链接)|Path|Repository|Modified + 行选择列（现行五列收敛——与 T-414 注释口径、console-m8 §6.4 口径**双不一致**一并归位）；大小+sha256 列移入列选器可选项（不默认呈现）；行导航仅 name 单元格深链（行体 inert）；查询位置 = 顶栏驻留 + 网格内快滤（页内输入形态收敛——M15 AQL 模式编辑器共存形态归票内设计）；快搜空历史恒渲染占位（`AppShell.tsx:628`）；日期格式 `dd-MM-yy HH:mm:ss +ZZZZ` 对位 + 详情 General ISO 格式化（T/.000Z 不裸显）；范围页签（Artifacts/Packages/Builds）缺位维持（R2 类型化落地后再现——既定设计，Builds 页签 dep §0.3）。
- **144.7 E2 分页范式（B-3.3 ×9 处——Q4 已裁翻正，LC-98）**：统一页码控件（共享分页组件一处改 + ×9 消费点）；后端维持 keyset 游标映射页窗——呈现对齐、语义自有 C 注留痕（深翻页 offset 扫描成本规避）；E2 豁免条目翻案落 parity 册 v1.2。

验收标准（AC）：

- **AC1（页签序+字段族）**：Playwright——三级（仓/目录/文件）页签序断言；File URL 复制（剪贴板/回显断言）；curl 下载文件 3 次 → 详情 General `Downloads=3` + `Last Downloaded By=<调用者>`（dep FR-146.2 落地后端到端）；Artifact Count·Size(Show) 展开。
- **AC2（属性编辑）**：Playwright——常显输入 + Add 添加（M10 属性夹具数据腿）；网格搜索过滤；（候裁臂）Property Set 分段处置断言。
- **AC3（搜索栈）**：Playwright——列集断言（name 链接深链 / 行体 inert / 选择列勾选）；顶栏驻留查询 + 网格快滤；空历史占位渲染；日期格式正则断言（含时区偏移）；详情 ISO 格式化断言。
- **AC4（回归）**：M15 搜索页 AQL 模式 spec 零回归（列框架收敛后 AQL 行复用锚不破）；四闸门 + axe。

### 4.5 批次④ 安全/shell 栈（P1；监控/帮助 P2；用户/组路由化候 Q5）

#### FR-145 用户/组路由表单化 + 权限两步弹窗 + 能力位 + profile 自助 + 监控面 + 帮助/导航（web/src + 小 BE 腿；B-1.7/8/11 + B-2.15~18）

**用户故事**：
- 作为安全管理员，用户/组创建走路由整页表单（/users/new 可深链、Cancel/Save 同构）；权限创建两步弹窗（选仓双列 + Any Local/Any Remote 预置 → 模式 include/exclude）；用户表单有能力位三旗。
- 作为普通用户，Profile 页可自助生成 identity token、管理 SSH key（不再被指到 admin Tokens 页）。
- 作为运维，服务节点组下有 System Logs 查看器与 Service Status；帮助 ? 有下拉与 About 版本弹窗。

行为规格：

- **145.1 用户/组创建路由表单化（B-2.15——Q5 已裁出口①路由化）**：/users/new、/groups/new 路由整页表单（Cancel/Reset/Save——Reset 钮处置随 Q9）；列表页内联展开卡退役；BinFlow 内部不一致（权限创建已是路由页）顺势统一；E5 条目按终裁改写落 parity 册 v1.2（FR-141.2 同场）。
- **145.2 权限编辑两步弹窗（B-2.16）**：① Select Repositories 双列 + Any Local / Any Remote 预置（Any Distribution 缺位登记——dep Release Bundle 域 §0.3，不伪造）；② Set Patterns include/exclude（路径模式 textarea 维持，载体迁入第二步）；内联「＋添加仓库…」/内联添加用户组收敛。
- **145.3 用户能力位（B-1.7，部分候裁）**：Can Update Profile / Disable UI Access / Disable Internal Password 三旗（A 腿——用户表单布尔位 + 行为联动）；Administer Platform + Manage Resources 双布尔 vs 三值角色枚举（admin/readonly_admin/user）= **候裁**（牵 ADR-0026 角色闭集——若并存须 ADR 增补评审，v1.0 暂行：维持枚举 + 登记差异）。
- **145.4 profile 自助（B-1.8）**：Profile 页 identity token 生成 + SSH key 管理（token 面持久化牵 Access Tokens L1 列选器重评——A7 架构约束条目联动）；「签发指到 admin Tokens 页」的文档化设计推翻（B-1.8 非正式豁免——翻案语境补齐）。
- **145.5 监控面（B-1.11）**：System Logs 查看器（日志尾随/过滤/下载——载体归实现票）+ Service Status 页；SystemInfoPage 归位服务节点组；监控组从「仅存储页」扩为三页。
- **145.6 帮助与导航（B-2.17/18）**：? 帮助下拉（Documentation / Online Training / Release Notes / About）+ About 版本弹窗（版本/构建信息——侧栏脚注 vdev 升格）；Admin 导航分组对齐（认证组子项形态——HTTP SSO/Crowd/JIRA 缺位系域不存在登记；Webhooks 归常规组；维护/备份归服务节点组；侧栏 Search Admin Resources 过滤框）。
- **145.7 GC/备份 cron 消费面（B-1.9/10——Q1 已裁引入，调度本体归 FR-150）**：GC 维护面扩展（Cleanup Unused Cached Artifacts / Cleanup Virtual Repositories / Compress Internal Database / Prune Unreferenced Data / Quota 百分比）+ 备份定时 CRUD（New Backup/cron/next-run/列表）+ import/export 管理页——cron 字段/next-run 呈现消费 FR-150 调度域；手动 dry-run/apply 维持并存。

验收标准（AC）：

- **AC1（路由表单化）**：Playwright——/users/new 深链直达整页表单；创建-列表-编辑闭环；内联卡退役断言；（候裁臂）Q5 出口②则反转为本 AC 的登记票形态。
- **AC2（权限两步弹窗）**：Playwright——两步导航（① 双列选仓 + Any Local 勾选 → 权限面覆盖本地全仓断言；② include/exclude 模式生效——无权限用户 403/不可见探针）；E4 整页编辑器 + 测试器/diff 维持自有（A8——零删除断言）。
- **AC3（能力位+profile）**：Playwright——三旗勾选后行为联动（Disable UI Access 用户登录被拒臂 / Disable Internal Password 密码改道臂）；Profile 生成 token（一次性明文呈现 + 即时可用 curl 断言）+ SSH key 增删。
- **AC4（监控+帮助+导航）**：Playwright——System Logs 尾随刷新 + 过滤；Service Status 页数据断言（对位既有 metrics/health 端点——零新端点优先）；? 下拉四项 + About 弹窗版本断言；导航分组断言（Webhooks 常规组 / 维护·备份服务节点组 / 侧栏过滤框过滤生效）。
- **AC5（cron 消费面）**：GC/备份 cron 字段保存-回显-next-run 呈现（引擎断言归 FR-150 AC）；ADR-0044 Accepted 前置。

### 4.6 后端配合小域（P1；Annotate 已裁加；Last Login P2——cron 调度域拆出归 FR-150）

#### FR-146 Annotate 动词与 write 拆分（Q7 已裁加）+ per-node 下载计数基建 + Last Login 派生（internal/httpapi + internal/storage + internal/auth 域）

**用户故事**：
- 作为安全管理员，权限动词与 Artifactory 对位（Read / Deploy-Cache / Delete / Annotate / Manage 五列——候 Q7）；既有 write 权限语义平滑迁移零提权。
- 作为审计员，每个节点的下载计数/最近下载者可查（详情字段族与 AQL usage 域同一数据源——一鱼两吃）。

行为规格：

- **146.1 Annotate 动词与 write 拆分（Q7 已裁加——LC-88 归 A）**：动词域扩列（annotate = 属性写权限——M10 属性系统三动词对位映射）；write → deploy-cache 拆分（Artifactory Deploy/Cache 合并列形态——拆分粒度与呈现归 K68 定案）；既有权限数据迁移（write → deploy-cache 映射 + 迁移脚本 + 回归）；FE 权限矩阵五列（FR-145.2 消费）。
- **146.2 per-node 下载计数基建（一鱼两吃——批次③字段族 + FR-148 usage 域单源）**：nodes 表扩列（download_count / last_downloaded_at / last_downloaded_by / remote_download_count——schema 变更归 architect 会签）；下载路径埋点（直连 / 经 virtual / remote 缓存命中三分计数口径归 K69）；FileInfo 扩字段（详情消费）；`statisticsEnabled` 开关行为化 + `sourceOrigin` 落库（A2 同族——stats 基建落成后随票激活，P2 顺车）。
- **146.3 Last Login 派生（P2）**：audit 登录事件派生用户列表列（端点/投影形态归票——「无端点列不伪造」解除因数据源已备）；users 列表消费。
- **146.4 cron 调度域——已拆出归 FR-150（v1.1）**：调度引擎（数据模型/表达式子集/next-run/与事件驱动 sweep 并存语义/误触发防护）独立成域承载（ADR-0044 占位）；本 FR 不再承载。

验收标准（AC）：

- **AC1（Annotate——已裁落定）**：迁移脚本 dry-run 报告（write 行 → deploy-cache 映射率 100%）+ 迁移后既有权限行为零回归（M7 RBAC 全量 spec）+ annotate 位控制属性写（无位用户 PUT properties → 403 探针）+ FE 权限矩阵五列（FR-145.2 消费）。
- **AC2（统计基建）**：`curl -u user1 "$BASE/api/storage/<repo>/<path>"` → FileInfo 含 `downloads`/`last_downloaded_by`；下载 3 次（直连 2 + 经 virtual 1）→ 计数口径断言（K69 定案值）；ACL——受限用户不可见仓的字段零泄漏（探针）。
- **AC3（Last Login）**：登录事件后 users 列表 Last Login 列呈现（audit 派生时延口径断言）；列表 API 投影字段断言。

### 4.7 remote 远端浏览可选档（M15 Q4 终裁承接——LC-76 实现段）

#### FR-147 批 1 三型可选档：helm classic + deb + rpm（internal/adapter/* + repo service + web/src；remote-browsing.md 规格前置）

**用户故事**：作为 remote 仓用户，勾选远端浏览可选档后，树可回源枚举远端目录（不限于已缓存）；默认关闭时与现行缓存浏览完全一致（T-406 as-built 同形态——不欠默认 parity）。

行为规格：

- **147.1 可选档语义（Q4 终裁口径逐字）**：`listRemoteFolderItems` 对位可选档（**默认 false**）；批 1 = helm classic（index.yaml 全树）+ deb + rpm（元数据枚举）；批 2（docker/helmoci tags 层 + maven metadata 版本层）**条件不排**（批 1 验证用户真实使用后裁）。
- **147.2 牵连面**：T-412 `listVirtual`「remote 成员仅缓存行」口径扩面（repo-semantics §8.5——可选档 on 时 virtual 树含远端行）；上游故障降级（不可达 → 该层错误态不整树塌）；t226 Pro 建仓面活体补拍腿（M15 登记的规格票内容）。
- **147.3 明确不做面维持**：generic/maven HTML 抓取族 + 无根级枚举 API 五型 + docker tags 首采被否——§2.2 登记（翻案语境核对位）。

验收标准（AC）：

- **AC1（可选档双态）**：off（默认）→ remote 树仅缓存行（既有断言零回归）；on → `Playwright/curl` 树展开含未缓存远端目录（helm index 全树臂 / deb·rpm 元数据臂）；点击未缓存路径 → 触发回源拉取（下载计数埋点联动 FR-146.2）。
- **AC2（降级与 ACL）**：上游停机 → 远端层错误态 + 已缓存行可用；越权仓远端行零泄漏（探针）；virtual 含 remote 成员口径扩面断言（§8.5 对账）。

### 4.8 AQL 高级面副线（M15 既定第一顺位；P0 腿 statistics/usage，余 P1/P2）

#### FR-148 statistics/usage 域 + /api/search/usage + QRL 全量 + UI 搜索族 + dates/creation（internal/search 增量 + httpapi；dep FR-146.2 + aql.md 增量段）

**用户故事**：
- 作为治理员，AQL 一句话答「90 天未下载的制品」（statistics 域）+ REST `usage?usageSince=` 直查——冷数据清理的数据地基。
- 作为 SRE，QRL 三态可查可调（限流行为透明化——M15 简化门的运营面升级）。

行为规格：

- **148.1 statistics/usage 域（P0——dep FR-146.2）**：AQL statistics 域字段（downloaded/downloaded_by/remote_downloaded 族——字段集归 aql.md 增量段定案）；`GET /api/search/usage?usageSince=`（M15 §5.7 全景表 M16 行兑现——usageSince 时间语义 + envelope 照锚）；与主轴字段族同一计数源（一鱼两吃断言）。
- **148.2 QRL 全量（P1）**：`v1/system/query_rate_limiter` 三态 + 指标 job + REST 面（M15 LC-73 简化门〔内部常量〕升级——参数可配置化边界归 K72：默认值维持 K63 定案 1000/4/10s，REST 面读写权限 admin 门）。
- **148.3 UI 搜索族 + dates/creation（P2 顺车）**：artifactsearch / stashResults / packagesSearch / syntax-search 四端点（inv-2 §1.C UI 族——形态归增量段；Smart Searches 保存面 pro 档不做 §2.2）；dates/creation（K65 判定承接：404 `No results found.` 空集族 + uri 瘦行 + epoch-ms 参数）。
- **148.4 顺车项**：statisticsEnabled/sourceOrigin 行为化（FR-146.2 附带——A2 域清单同族）。

验收标准（AC）：

- **AC1（statistics/usage）**：curl——`items.find({"$and":[{"statistics.downloaded":{"$lt":"..."}},{"repo":{"$match":"*-local"}}]})` 类查询绿（下载夹具数据腿：先下载 N 次再查）；`curl "$BASE/api/search/usage?usageSince=..."` 命中已下载制品且计数与 FileInfo 一致（单源断言）；ACL 越权探针（§1.4 条款 5）。
- **AC2（QRL）**：curl——三态查询/设置/复位（admin 门：非 admin 403）；指标 job 采样呈现；K63 门行为与 REST 面读数一致性断言。
- **AC3（UI 族 + dates）**：四端点 curl wire 断言（envelope/错误码照增量段锚）；dates/creation 双端点（含 404 空集族臂——未命中 `No results found.` 逐字）；M15 §5.7 全景表 M16 行逐条对账（实现/维持 404 与表一致）。

### 4.9 i18n 双语可切换（Q3 用户终裁新增——E6 翻案，P1）

#### FR-149 i18n 框架 + 中英两包 + 语言切换器 + 断言双语化策略（web/src 全站 + 构建链）

**用户故事**：
- 作为中文用户，界面维持中文（零语义变化）；作为英文环境用户/海外协作者，一键切换 en——「完全一致」在语言维度的**超集**兑现（非单语倒退）。
- 作为 QA，断言成本不因双语翻倍——默认 locale 全量维持 + en 抽样腿的策略在册。

行为规格：

- **149.1 框架接入**：i18n 框架（react-i18next 或 architect/ux 会签定——轻量 ADR 或票内定案）；**文案外提 100%**（组件层零硬编码中文——CI 断言脚本扫描）；中英两资源包（namespace 按页面域切分——懒加载）；语言切换器（侧栏脚或 Profile——位置归 ux 定，K67 关联）；持久化（localStorage；用户偏好落库候裁——v1.1 简行 localStorage）。
- **149.2 术语保真两包一致**：en 包术语对齐 Artifactory（repo key / node / checksum / Deploy / Set Me Up 等英文术语原样）；zh 包 = 现行文案迁移（零语义变化——「英文术语保真」在两包中均成立）；E6 条目改写落 parity 册 v1.2（「UI 中文 + 英文术语保真」→「双语可切换、术语两包保真」）。
- **149.3 断言双语化策略**：默认 locale（zh）全量 spec 维持（零翻新成本）；en 抽样腿（每批次核心 spec 一条 en 断言——文案断言改键断言）；锚 id 与文案解耦（data-testid 不随 locale 变——既有锚零改名纪律维持，锚册零翻新）；axe 双主题扩双 locale 抽样。
- **149.4 locale 化格式**：日期/数字格式随 locale（en 呈现 Artifactory 形态——FR-144.6 日期格式对位的 en 变体；zh 维持现行）——细节归 K 项随票。

验收标准（AC）：

- **AC1（切换全链）**：Playwright——切换器 zh↔en 全站文案翻转（抽样 ≥6 页面：树/表单/详情/搜索/安全/监控）；reload 持久；术语两包一致（repo key 等英文术语在 zh 包中原样呈现断言）。
- **AC2（文案外提）**：CI 断言脚本——组件层硬编码中文零命中（扫描归档）；missing key 零运行时告警。
- **AC3（断言策略）**：en 抽样 spec 绿（每批次一条）；既有锚零改名（anchor ledger 0 断链维持）；四闸门 + axe 双主题双 locale 抽样 serious=0；i18n 资源包 gzip 增量观察（NFR-P73）。

### 4.10 cron 调度域（Q1 用户终裁新增——推翻 M15 Q5，P1；ADR-0044 占位前置）

#### FR-150 调度数据模型 + 表达式子集 + 三消费面 + 与事件驱动并存语义（internal/scheduler〔新包定名归 ADR〕+ httpapi + web/src）

**用户故事**：
- 作为运维，GC 与备份按 cron 定时执行（New Backup / next-run 可见），不用手动点 dry-run/apply。
- 作为复制运维，可为 push replication 配 cron 表达式（Artifactory 用户级 cron 对位）——定时全量与事件驱动增量并存不重复。

行为规格：

- **150.1 调度引擎（ADR-0044 占位——拆票前 Accepted）**：调度数据模型（schedule 实体：表达式/next-run/last-run/状态/所属域）；cron 表达式子集（Artifactory〔Quartz〕兼容——支持域与拒绝形态归 ADR/规格票）；next-run 计算器 + 触发器（挂 1min sweep 或独立 ticker——归 ADR）；误触发防护（过去时间拒配 / next-run 不可达校验 / 每域并发上限）。
- **150.2 与事件驱动并存语义（ADR 核心命题——推翻 M15 Q5 的边界补偿）**：调度域**只触发全量类任务**（GC 全量 / 备份 / 复制全量同步）；事件驱动 + outbox 仍是**增量唯一引擎**；零重复投递断言（同制品不双推——调度触发的全量走 Replicate Now 同任务载体幂等收敛）；T-402a 勘误「事件驱动唯一引擎」改写为「增量唯一引擎 + 全量调度并存」落 BOARD/文档留痕。
- **150.3 三消费面**：① GC 维护面 cron 字段 + Cleanup 两族（FR-145.7 呈现消费）；② 备份定时 CRUD（New Backup / cron / next-run / 列表 + import/export 管理页——B-1.10）；③ 复制配置 cron 字段（Artifactory 用户级 cron 对位——M15 Q5 推翻的兑现面；与 Replicate Now 并存幂等）。
- **150.4 可观测与审计**：调度/执行/失败三事件 audit 词表（maintenance.schedule.* / backup.schedule.* / replication.schedule.* 补词）；调度状态 REST/控制台呈现（next-run 倒计时——形态归票）。

验收标准（AC）：

- **AC1（调度全链）**：curl/Playwright——建备份调度（cron 每小时）→ next-run 计算断言（与表达式对拍）+ 到点触发（测试加速：手动触发等价腿 + audit 行）；GC cron 字段保存-回显-到点 dry-run 断言。
- **AC2（复制 cron + 零重复）**：双实例夹具（T-420 形态复用）——复制配 cron → 按点触发全量同步（目标逐路径 sha256 一致）+ 调度窗内同制品增量事件不双推（零重复投递断言）；手动 Replicate Now 与调度并存幂等。
- **AC3（防护与审计）**：过去时间表达式 → 拒配（400 形态）；畸形表达式拒绝（子集校验零注入——NFR-S79）；audit 三事件在场；ADR-0044 Accepted 核查（DoD 前置）。

---

## 5. 兼容性矩阵（M16——控制台交互 parity 面 + 统计/搜索 wire 契约）

### 5.1 层级定义（沿 M13~M15 四层）

| 层级 | 定义 |
|---|---|
| **A 兼容** | 交互形态/信息呈现/端点语义对齐 Artifactory（审计 B 项 t226 实测锚 + parity 册 v1.2——子集以「A（子集注记）」标示） |
| **C 自有** | BinFlow 自有设计（无 Artifactory 对应或有意自有——基建/增强面） |
| **D 有意不兼容 / 不做** | 显式裁决不做（clean-room / Non-goal / 防线 / 物理不可行），矩阵留痕防再议 |
| **待裁** | 存在可观测语义决策，须用户/conductor 终裁——终裁后归 A/C/D 并回写 |

**常设纪律（M16 特化）**：① **豁免翻案双留痕**（翻案 = Q 裁定 + parity 册修订，零默默翻转——§1.4 条款 1）；② **无数据源不伪造**（Module ID / Any Distribution / Project 列等 dep 域未建则缺位登记，§1.4 条款 2）；③ **ACL 零泄漏**（新端点/新字段同 allow() 源——探针为硬 AC）；④ E1/E6 锁死项零倒退检查（终裁翻案前维持现行文本与行为）。

### 5.2 档位 × addon 解锁矩阵（M16 增量 0 行预期）

M16 不新增 addon 槽：**8 包型开禁 = 门控解除非新槽**（go/nuget/cargo/conan/helm/helmoci/rpm/debian 后端已实现——'pro' 前端标记移除；addon 槽定义若需联动调整走 ADR-0033 增补，票内核对归档）；AQL 高级面沿 M15 §5.2（oss 档核心能力无槽）；Smart Searches 维持 pro 档不对齐。三态叠加规则、`addons.disabled` 熔断语义沿 M10 §5.2 不变。

### 5.3 契约矩阵（估 17 条，LC-80~LC-96 续接 M15 编号——v1.0 立项估值，候裁项终裁后归位）

| # | 契约面 | Artifactory 对应 / 取证锚 | 层级 | 优先级 | 候裁联动 | 验收 |
|---|---|---|---|---|---|---|
| LC-80 | 制品树——文件叶子进树 + 选择≠展开 + 深链祖先链语义 | B-1.1/B-1.2（`ArtifactsBrowser.tsx:1270/:1106/:1272` + t226 树对照） | A | P0 | Q2/Q9（children 表收窄） | L35 |
| LC-81 | 树 URL/状态模型——页签进 URL 段 + 文件路径段化（`?focus=` 退役 + 兼容映射） | B-1.3（Artifactory `/ui/repos/tree/<TAB>/<repo>/<path>`——BinFlow 路由形态自有，语义 A） | **A（路径形态 C 注——路由前缀自有）** | P0 | — | L35 |
| LC-82 | 树头工具带——包类型 facet / rclass 组 / Sort-by / Compacted 单选 / My Favorites | B-1.4 + reverse §3.2 回填（漏记补笔） | A | P0 | K67（选项集冻结） | L35 |
| LC-83 | 仓库表单——Basic\|Advanced\|Replications 三段 + 字段域补齐（四藏字段 + Environments + 描述拆分 + Force Auth + Suppress POM）+ dirty-gating + 入口分路由 | B-1.5 + B-2.5 + B-3.7/8/12（PUT 全收 L55-61 实证） | A | P1 | Q8（补齐范围）/ Q9（重置钮） | L36 |
| LC-84 | 包类型弹窗 880px 居中 + 8 已实现包型开禁（go/nuget/cargo/conan/helm/helmoci/rpm/debian） | B-2.6（440px/13 tiles/8 禁用 vs 880px 居中）——型录维持 BinFlow 实有 13 型（33 全型 dep A4） | **A（modal 形态）/ C 注（开禁系 BinFlow 自有门控解除）** | P1 | — | L36 |
| LC-85 | 详情栈——页签序（权限在属性前）+ File URL + 元数据字段族（Repository Layout/Description/Created/Artifact Count）+ 属性编辑解剖 + 下载形态 | B-2.1/2/3/4/7/9/10/12 | A | P1 | Q2（行内删除）/ Q9（自有增强块）/ K68（Property Set） | L37 |
| LC-86 | 搜索面——结果列集（Artifact-name 链接\|Path\|Repository\|Modified + 选择列）/ 行导航 name 单元格 / 顶栏驻留查询 / 快搜空历史占位 / 日期格式 | B-2.11/13/14 + B-3.14/15/16（T-414 注释与 console-m8 §6.4 双不一致归位） | A | P1 | Q4（E2 分页 ×9 联动） | L37 |
| LC-87 | per-node 下载统计基建——nodes 扩列（download_count/last_downloaded_*/remote_download_count）+ FileInfo 扩字段 + statisticsEnabled 行为化 | A2/A6 素材（Artifactory 字段面 A 对位；数据模型 BinFlow 自有） | **C（自有基建——字段呈现 A 对位）** | P1 | K69（计数口径） | L38 |
| LC-88 | 权限动词——Annotate 加入 + write→Deploy/Cache 拆分 + 既有数据迁移 | B-1.6 + C7（Artifactory 5 列 Read/Deploy-Cache/Delete/Annotate/Manage） | **A（v1.1 归位——Q7 终裁加）** | P1 | 已裁（Q7）/ K68 | L39 |
| LC-89 | 安全/shell 栈——用户/组路由表单化 + 权限两步弹窗（Any Local/Any Remote 预置）+ 能力位三旗 + profile 自助 token/SSH + 帮助下拉/About + 导航分组 | B-1.7/8 + B-2.15/16/17/18（Any Distribution 缺位登记 dep Release Bundle） | A | P1 | Q5（E5 重裁）/ 双布尔 vs 枚举候裁 | L39 |
| LC-90 | 监控面——System Logs 查看器 + Service Status 页（服务节点组归位） | B-1.11 | A | P2 | — | L40 |
| LC-91 | cron 调度域——调度数据模型 + GC 定时（Cleanup 两族）/ 备份定时 CRUD / import/export 页 + 复制用户级 cron 字段 | B-1.9/10 + A3 + C1（**Q1 终裁引入——推翻 T-402a 勘误「事件驱动唯一引擎」裁定，BOARD 留痕**；ADR-0044 承载并存语义） | **A（v1.1 归位——Q1 终裁新增域）** | P1 | 已裁（Q1）/ K70 / ADR-0044 | L48 |
| LC-92 | remote 远端浏览可选档——批 1 = helm classic + deb + rpm（`listRemoteFolderItems` 对位，默认 false） | M15 Q4 终裁承接（LC-76 实现段；T-425 13 型矩阵 + 官方设置面五型） | A | P1 | K71 / repo-semantics §8.5 扩面 | L41 |
| LC-93 | AQL statistics/usage 域 + `GET /api/search/usage?usageSince=` | aql.md 增量段（stats 字段 t226 OSS 活体可用——排 M16 系自有基建缺失非 parity 档位）+ M15 §5.7 M16 行 | **A（子集注记——域字段集归增量段）** | P1（P0 腿） | dep LC-87 | L42 |
| LC-94 | QRL 全量——`v1/system/query_rate_limiter` 三态 + 指标 job + REST 面 | inv-1 §E QRL 锚 + M15 LC-73 简化门升级 | A | P2 | K72 | L42 |
| LC-95 | UI 搜索族（artifactsearch/stashResults/packagesSearch/syntax-search）+ dates/creation 老搜索 | inv-2 §1.C UI 族 + K65 承接（404 空集族 + 瘦行 + epoch-ms） | A | P2 | — | L42 |
| LC-96 | L1 列选器推广（repos 扩列 + users/groups/permissions）+ Users Last Login 列 | A6（columnPrefs 共享层已就绪 + audit 派生） | A（列选器沿 LC-61 形态） | P2（条件池） | Q10 | L43 |
| LC-97 | i18n 双语可切换——框架 + 中英两包 + 语言切换器 + 文案外提 100% + 断言双语化策略 | Q3 终裁（Artifactory 全英文——en 包对齐其单语形态；zh 包维持产品定位） | **C（自有增强——en 腿 A 注：术语对齐 Artifactory）** | P1 | — | L47 |
| LC-98 | 分页范式——页码控件 ×9 处统一（共享分页组件 + keyset 页窗映射） | Q4 终裁（Artifactory 分页控件形态——E2 翻案） | A | P1 | 已裁（Q4） | L37/L43 |

> 计数（v1.1 终裁后）：**19 条 = A 17（LC-80/81〔路径 C 注〕/82/83/84〔开禁 C 注〕/85/86/88〔Q7 归位〕/89/90/91〔Q1 归位新增域〕/92/93〔子集〕/94/95/96/98）+ C 2（LC-87 统计基建 / LC-97 i18n〔en 腿 A 注〕）+ 待裁 0**——零滞留（LC-56/M15 Q1 教训防范兑现）。既有契约面（五基础包型、九 addon 包型、配置域、操作族/回收站、webhook、helmoci/docker remote、replication、AQL item+property 域）M16 对 M15 as-built 的行为变化全部经本矩阵承载（新端点全集 = LC-93/94/95 所列 + 调度 REST 面〔LC-91/ADR-0044〕+ 零私加）；FE 票「服务端 diff=0」复核沿 M8/M15 先例（FR-146/147/148/150 BE 腿除外）。

### 5.4 回归基线（M16 断言反转与翻案登记——逐条归属 M16 豁免票；终裁翻案项在 Q 出口落笔后计入）

| 既有断言/形态 | M16 期望 | 归属 |
|---|---|---|
| M1~M15 全部 P0 序列（双形态：无 license 默认 + pro） | 零回归（四批次 FE 改造面大——树/表单/详情/安全各批专项复跑 + 全量双形态终验） | 各批次票 + QA 终验 |
| 「（空）」占位 / `?focus=` 查询参数 / 树选择即展开（as-built 形态） | **断言反转①**：文件进树后占位退役 + URL 路径段化（旧 URL 兼容映射一轮）+ select≠expand（既有 spec 翻新——锚册版本递增） | FR-142 豁免票 |
| 搜索结果五列（现行）/ T-414 注释口径 / console-m8 §6.4 口径 | **断言反转②**：三源不一致归一为 Artifactory 列集（name 链接/Path/Repository/Modified + 选择列） | FR-144 豁免票 |
| 表单 footer 重置钮 / 包型 modal 440px 紧凑档（parity 册 M1 行） | **形态翻转③**：重置钮移除（候 Q9）+ modal 880px（M1 行再修订留痕——FR-141.2） | FR-143 豁免票 |
| 用户/组创建内联卡（as-built） | **断言反转④**：路由整页表单化（候 Q5——终裁出口①）；出口②则转登记票 | FR-145 豁免票 |
| write 动词语义（M7 RBAC 闭集 as-built） | **迁移⑤（已裁 Q7）**：write → deploy-cache 映射 + annotate 扩列；迁移零提权断言（M7 全量 spec） | FR-146 豁免票 |
| E6 中文锁死豁免（M14 豁免族 as-built） | **断言反转⑥（已裁 Q3）**：双语可切换——默认 zh 断言维持 + en 抽样腿；E6 条目改写（「UI 中文 + 英文术语保真」→「双语可切换、术语两包保真」）落 parity 册 v1.2 | FR-149 豁免票 |
| 事件驱动唯一引擎（T-402a 勘误 as-built——无 cron） | **断言反转⑦（已裁 Q1——推翻 M15 Q5 终裁）**：调度域并存——cron 触发全量类任务（GC/备份/复制全量）与事件驱动增量零重复投递断言；M15 PRD Q5 行与 T-402a 勘误标注推翻留痕（BOARD + 本表） | FR-150 豁免票 |
| E1 锁死项 / smu-* 锚族 / tree-empty 两态锚 | **零倒退检查**：E1 依 Q2 出口①收紧不倒退（更严形态维持 + 文本范围修正）；E6 已翻案为双语（断言反转⑥——zh 包语义零变化）；smu-* 零改名；M15 断言反转②的两态锚不回退 | 每票硬 AC |
| `make test`（race）/ footprint ≤100MB / check-size ≤120MB / 冷启动 <2s / SPA ≤10KB/票 | 维持（FE 四批量大——SPA 累计趋势观察项 §6.2；F1 六平台趋势登记） | QA 终验 |
| 四闸门 + axe 双主题 serious=0 / anchor ledger 0 断链 | 维持全绿（树改造牵 artifacts-tree 全族 spec 翻新——新锚入册流程） | FE 每票 |

### 5.5 M16 核心验收命令（L 序列骨架，QA 直接引用——续接 M15 L34 起）

```bash
BASE=http://127.0.0.1:8080; ADMIN=admin:password
# ========== FR-141 前置锚 ==========
# L35a parity 册 v1.2 落盘（E5 前提修正/E1 范围修正/stay-out 登记/翻案 Q 编号留痕）+
#    reverse §3.2 facet 回填 + Q1~Q13 归位核查 + tech-lead 就绪度确认；make docs 零断链

# ========== FR-142 批次① 树栈（P0） ==========
# L35 Playwright——文件叶子（含文件目录非「（空）」+ 叶子点击出 item view）+ 单击选择不展开
#    （expand 前后对照）+ URL 模型（页签段/文件路径段/刷新重开/分享深链三态一致 + ?focus= 兼容映射）+
#    树头工具带（facet 过滤/rclass 组/Sort-by/Compacted/My Favorites reload 持久）+
#    10,291 节点首屏 p95 ≤734ms 基线 + artifacts-tree 10 spec 翻新零断链 + 四闸门 + axe

# ========== FR-143 批次② 表单栈 ==========
# L36 Playwright——三段步进 + 字段域表驱动（blackedOut 提交-回显-拒写三链 /
#    maxUniqueSnapshots/repoLayoutRef/archiveBrowsing/Environments/ForceAuth/SuppressPOM）+
#    modal 880px + 8 包型开禁逐型：控制台建仓 → 真实客户端 roundtrip（go install / dotnet nuget push /
#    cargo publish / conan upload / helm push / helm push oci:// / dnf install / apt install——八型矩阵）+
#    入口分路由深链 + Remote 页签 Replications 列 + dirty-gating + remote Test 三臂（零副作用）

# ========== FR-144 批次③ 详情/搜索栈 ==========
# L37 Playwright——页签序（权限在属性前，三级统一）+ File URL 复制 + Downloads/Last Downloaded By 渲染
#    （dep L38 基建）+ Artifact Count·Size(Show) + 属性编辑常显输入+Add+搜索 + 下载形态 +
#    搜索列集（name 链接深链/行体 inert/选择列）+ 顶栏驻留查询 + 空历史占位 + 日期格式正则（含时区）+
#    M15 AQL 模式 spec 零回归 + 四闸门 + axe

# ========== FR-146 后端小域 ==========
# L38 统计基建：curl -u user1 "$BASE/api/storage/<repo>/<path>" → FileInfo downloads/last_downloaded_by 在场；
#    下载 3 次（直连 2 + virtual 1）→ 计数口径断言（K69 定案值）+ 越权仓字段零泄漏探针；
#    Last Login：登录后 users 列表列呈现（audit 派生）
# L39 Annotate（候 Q7）：迁移 dry-run 报告（write→deploy-cache 100%）+ 迁移后 M7 RBAC 全量零回归 +
#    annotate 位控制属性写（无位用户 PUT properties → 403）+ FE 矩阵五列断言；Q7 否决则转登记票

# ========== FR-145 批次④ 安全/shell 栈 ==========
# L40a Playwright——/users/new /groups/new 深链整页表单 + 权限两步弹窗（Any Local 覆盖断言 +
#    include/exclude 生效探针）+ 能力位三旗行为联动 + Profile token 生成（一次性明文 + curl 即用）+
#    SSH key 增删 + System Logs 尾随/过滤 + Service Status + ? 下拉 + About 版本 + 导航分组/侧栏过滤
# L40b （条件，候 Q1）cron：GC/备份调度 CRUD + next-run 计算 + audit 断言（独立 ADR Accepted 前置）

# ========== FR-147 远端浏览 ==========
# L41 可选档双态：off → 仅缓存行（既有断言零回归）；on → helm index 全树 + deb/rpm 元数据枚举 +
#    未缓存路径点击触发回源（下载计数联动）+ 上游停机降级（远端层错误态/缓存行可用）+
#    virtual 含 remote 成员口径扩面对账（repo-semantics §8.5）

# ========== FR-148 AQL 副线 ==========
# L42 statistics/usage：下载夹具 → AQL statistics 域查询绿 + curl "$BASE/api/search/usage?usageSince=..."
#    计数与 FileInfo 单源一致 + ACL 探针；QRL：三态 REST（admin 门：非 admin 403）+ 指标 job +
#    K63 门行为一致性；UI 四端点 + dates/creation wire（含 404 No results found. 逐字）+
#    M15 §5.7 全景表 M16 行逐条对账（未实现端点仍 404）

# ========== 条件池与杂项 ==========
# L43 L1 列选器推广（repos/users/groups/permissions reload 持久）+ Last Login 列 + E7 toast（若触发）+
#     NuGet symbol（若转正：.pdb/GUID 路径真实腿）/ docker virtual 开禁（建仓矩阵 + docker 三态回归）

# ========== FR-149 i18n（Q3 终裁新增） ==========
# L47 Playwright——语言切换器 zh↔en 全站翻转（抽样 ≥6 页面）+ reload 持久 + 术语两包一致断言 +
#     文案外提 CI 断言（组件层零硬编码中文——脚本扫描归档）+ missing key 零告警 +
#     en 抽样 spec（每批次核心一条——文案断言改键断言）+ 锚 id 与文案解耦（既有锚零改名）+
#     axe 双主题双 locale 抽样 serious=0 + i18n 资源包 gzip 增量观察（NFR-P73）

# ========== FR-150 cron 调度域（Q1 终裁新增——ADR-0044 Accepted 前置） ==========
# L48 curl/Playwright——建备份调度（cron 每小时）→ next-run 计算对拍 + 到点触发（测试加速：手动触发
#     等价腿）+ audit 三事件；GC cron 字段保存-回显-到点 dry-run；复制 cron 双实例夹具（T-420 形态复用）：
#     按点触发全量（目标逐路径 sha256 一致）+ 调度窗内增量事件不双推（零重复投递）+
#     Replicate Now 与调度并存幂等；过去时间表达式拒配 + 畸形表达式拒绝（子集校验零注入）

# ========== 回归与收口 ==========
# L44 全量回归：M1~M15 P0 双形态复跑 + 断言反转①~⑦归属审计（100% M16 豁免票——含 E6 双语/cron 推翻两例 v1.1 翻案）+
#     B 47 项收口审计表（四态归属零无主项）+ parity 册 v1.2 终评 + FE 票服务端 diff=0 复核
# L45 NFR：10,291 节点首屏/层展开基线 + statistics 域查询 P95（万节点 ≤800ms 量级——随 aql.md 增量段）+
#     资源门三连（footprint ≤100MB / check-size ≤120MB / 冷启动 <2s）+ SPA ≤10KB/票 + F1 趋势登记
# L46 文档：tech-writer（树深链/表单三段/详情字段族/权限动词/监控面/远端浏览可选档/AQL 高级面 +
#     豁免翻案用户可见变化公告）客户端命令实测可复跑
```

### 5.6 待校准项（K67~K72 新起——v1.0 暂行值；效力序 §1.4 条款 4：parity 册 v1.2 / 增量段核验回写后 > 本 PRD）

| # | 项 | v1.0 暂行值 / 校准路径 |
|---|---|---|
| K67 | 树头工具带断言锚（facet 复选组形态 / Sort-by 选项集 / Compacted 呈现差异 / My Favorites 持久化载体） | V 式复核（t226 树页）+ reverse §3.2 回填后冻结——低置信细节核验前只作「以核验为准」附注 |
| K68 | Annotate 语义映射与 Property Set 域（annotate ↔ M10 属性三动词对位 / write→deploy-cache 迁移映射与呈现粒度 / Property|Property Set 分段的 BE 属性集配置小域去留） | Q7 终裁 + architect 会签（ADR-0026 增补评审）；Property Set 裁做则随 FR-146 扩列，不做登记缺位 |
| K69 | 统计基建计数口径（直连/经 virtual/remote 缓存命中三分——remote_download_count 区分边界；last_downloaded_by 可见性权限面） | architect 会签 schema 时定案；可见性默认 admin/manager 门（与 audit 同档），放宽须裁 |
| K70 | cron 调度域形态（表达式子集 / next-run 计算 / 与事件驱动 sweep 引擎边界 / 消费方注册面） | Q1 终裁引入后独立 ADR 承载——PRD 只留接口位 |
| K71 | 远端浏览可选档语义细节（`listRemoteFolderItems` 对位 wire / 三型枚举形态 / 上游故障降级 / virtual 口径扩面） | remote-browsing.md（T-425 §1/§2 成稿）+ t226 Pro 建仓面补拍腿 |
| K72 | QRL 全量参数域（三态语义 / 指标 job 周期 / REST wire / 可配置化边界——默认值维持 K63 定案 1000/4/10s） | aql.md 增量段（inv-1 §E QRL 锚 + 官方文档） |

---

## 6. 非功能需求（NFR）

### 6.1 与已有 ADR / 规范的冲突/补充标注

| ADR/规范 | 冲突/补充点 | 本 PRD 立场 | 所需动作 |
|---|---|---|---|
| ADR-0001/0029（clean-room） | 翻案语境 vs UI 零复制红线 | A0 永久红线零触碰（§1.4 条款 3）；行为基准 = t226 活体 + parity 册，非 reverse-src UI 代码 | §1.4 条款 3 |
| parity 册 E1~E7 豁免族 | 翻案对象（intake ⑤） | 逐条走 Q 表显式裁定 + 册 v1.2 双留痕（E5 前提修正 / E1 范围修正必改——否则断言失锚） | FR-141.2 / §7 |
| ADR-0026（RBAC 角色闭集） | 能力位双布尔 vs 三值枚举（B-1.7） | v1.0 暂行：维持枚举 + 差异登记；并存须 ADR 增补评审 | Q7/K68 关联 |
| ADR-0032/0033（license 门控） | 8 包型开禁的槽定义联动 | 开禁 = 门控解除非新槽；槽定义调整走 ADR-0033 增补（票内核对） | §5.2 |
| ADR-0041（webhook outbox）决策 7 | 57 型休眠触发源裁剪（C11/Q11） | 非 xray 域触发源激活清单随票出——「仅注册有源域」防线维持 | Q11 |
| architecture nodes 表 | 统计基建扩列（schema 变更） | 归 architect 会签（K69）——不为字段先改表 | FR-146.2 |
| T-402a 勘误（cron 不引入） | **Q1 终裁推翻（v1.1）**——intake ⑤ 冲突点确认机制兑现 | 调度域引入（FR-150/ADR-0044）+ 推翻留痕（§5.4 断言反转⑦）；并存语义 = 调度只触发全量类任务、事件驱动仍为增量唯一引擎 | FR-150 |
| **ADR-0044（编号占位）** | cron 调度域架构（Q1 终裁催生） | 调度数据模型 / 表达式子集 / next-run / 并存语义 / 误触发防护——architect 前置 Accepted | FR-150 |
| console-ux 锚册 v1.29 | 树/URL 改造牵 artifacts-tree 全族 | 断言反转①留痕 + 新锚入册（0 断链维持）；smu-* 零改名 | FR-142 AC5 |

### 6.2 性能与资源（M16 增量）

| NFR | 指标与验收方式 | 优先级 |
|---|---|---|
| NFR-P70 树栈性能 | 10,291 节点仓首屏 p95 ≤734ms / 层展开 ≤144ms 基线不显著回退（文件叶子计入后实测归档）；TREE_LEVEL_CAP/BIG_DIR 护栏复核（文件节点计数口径） | P0 |
| NFR-P71 查询与统计 | statistics 域 / usage 搜索 P95 ≤800ms 量级（万节点 + 计数列，本机 SSD+WAL——精确门随 aql.md 增量段）；下载计数埋点零可感知尾延（下载路径 p95 对比前后） | P1 |
| NFR-P72 资源门维持 | footprint ≤100MB / check-size ≤120MB / 冷启动 <2s 三连；SPA 每票 ≤10KB（四批次量大——**累计趋势观察项**：显著上浮即红旗，M15 基线 377,868B + 增量）；F1 六平台趋势登记 | P0 |
| NFR-P73 i18n 资源与性能 | 中英两资源包 gzip 增量单独观察（SPA ≤10KB/票口径对功能代码维持——资源包按包口径登记，显著上浮即红旗）；语言包懒加载切换零可感知卡顿；文案键查找零运行时缺失（missing key CI 告警） | P1 |
| NFR-P74 调度精度与并存 | next-run 计算正确（与表达式对拍断言）；到点触发精度对齐 1min sweep（±分钟级）；**与事件驱动零重复投递**（同制品不双推——断言归 L48）；误触发防护（过去时间拒配 + 每域并发上限） | P1 |

### 6.3 安全底线（M16 增量）

| NFR | 要求 | 验收 |
|---|---|---|
| NFR-S76 ACL 零泄漏 | usage/搜索新端点 + `last_downloaded_by` 字段 + Last Login 列与内容面同一 allow() 源；越权探针逐票硬 AC | L38/L42 |
| NFR-S77 权限迁移零提权 | Annotate/write 拆分迁移（若 Q7 引入）：dry-run 报告 + M7 RBAC 全量回归 + 迁移可回滚 | L39 |
| NFR-S78 E1 家族零倒退 | children 表/属性行/权限行的删除动作依 Q2 出口①收紧（确认/收进详情——更严形态维持）；parity 册 E1 文本范围修正后按新文本复核全部删除面 | L35/L37/L44 |
| NFR-S79 调度面防护 | cron 表达式子集校验（拒绝任意代码/畸形表达式——参数化解析零注入）；调度 CRUD admin/manager 门 + audit 三事件全记 | L48 |

### 6.4 可观测性（M16 增量）

- 统计基建埋点进既有 metrics 口径（download family 新组——Prometheus 三态口径延伸）；远端浏览回源计数（remote fetch family 归并复核——RE-11 同族归并纪律）。
- cron 域若建：调度/执行/失败三事件入 audit 词表（maintenance.* / backup.* 补词）。
- QRL 指标 job 采样呈现（LC-94）。

---

## 7. 用户确认清单（Q1~Q13——C 表 13 项浓缩；**v1.1：必答七项 Q1~Q7 已终裁落章（2026-09-02——用户四项 + conductor 三项）+ Q13 随 Q6 落定**，余 Q8~Q12 可后裁按建议裁点推进。每项：冲突描述 / 两出口 / PM 倾向〔v1.0〕/**终裁出口〔v1.1〕**）

| # | 冲突描述 | 两出口 | PM 倾向 | 建议裁点 / 影响面 |
|---|---|---|---|---|
| **Q1**（C1） | **cron 双轨**：conductor 曾裁不引入（T-402a 勘误——事件驱动唯一引擎、Replicate Now 承接手动全量）vs intake ⑤「不做清单全面翻案」覆盖 GC cron 字段 / Cleanup 两族 / 备份定时 CRUD / 复制用户级 cron | ① 引入调度范式（独立 ADR + 维护/调度专域）② 维持不引入（手动面 + CLI 引导维持，豁免登记延续） | 〔v1.0〕②维持不引入——调度范式系引擎级架构决策非前端 parity 面；intake ⑤ 原文要求冲突处交用户确认，本条即样板情形 | **【已裁——用户，出口①引入】**FR-150 承载（ADR-0044 占位 + 三消费面 + 与事件驱动并存语义）；**推翻 M15 Q5 终裁——BOARD 留痕 + §5.4 断言反转⑦**；LC-91 归 A |
| **Q2**（C2） | **E1 双冲突**：(a) Artifactory 行内 trash 直删 + 绿色 Delete 主按钮 vs E1 反向更严（输入 key 强确认 + 危险区）；(b) 内部矛盾：children 表每行红色「删除」钮已违反 E1 现行文本 | ① E1 文本范围收窄（管理列表 vs 浏览器表分治）+ 收紧 children 表删除件（走确认/收进详情）② 倒退 E1 对齐 Artifactory 直删形态 | 〔v1.0〕①——E1 系安全底线同源（误删制品不可逆）；修文本 + 收紧件，不做倒退 | **【已裁——conductor，出口①】**E1 文本范围修正 + 浏览器面删除件收紧（FR-141.2/142.1/144.4 承载）；**零倒退**——「翻案不等于倒退」的基准样例（§1.4 条款 1 留痕） |
| **Q3**（C3） | **E6 语言**：锁死「UI 中文 + 英文术语保真」vs「完全一致」（Artifactory 全英文） | ① 语言豁免维持（中文 UI 不变）② 引入 i18n/en 面（专程国际化——全站文案 + 锚册/断言策略） | 〔v1.0〕①维持——E6 锁死项；en 面若做须单列 | **【已裁——用户，出口②超集】双语可切换**（FR-149——zh 包维持 + en 包对齐，非单语倒退；文案外提 + 断言双语化策略 §4.9）；E6 翻案双留痕（§5.4 断言反转⑥）；LC-97 登记 C 层（en 腿 A 注） |
| **Q4**（C4） | **E2 分页范式**：「加载更多」×9 处 vs Artifactory 页码控件——范式级翻转 | ① 翻正页码控件（共享组件一处改 + ×9 消费点；后端可维持 keyset 游标映射页窗——呈现对齐语义自有 C 注）② 维持 E2 豁免 | 〔v1.0〕倾向①翻正——用户三次 UI 加码信号明确；深翻页 offset 成本经 keyset 页窗映射规避 | **【已裁——用户，出口①】页码控件 ×9 处统一**（FR-144.7/LC-98——keyset 页窗映射，呈现对齐语义自有 C 注留痕）；E2 翻案落 parity 册 v1.2 |
| **Q5**（C5） | **E5 重裁**：V6 撤销决策 B 的依据「BinFlow 已用路由实体表单」被审计推翻（用户/组创建实为内联卡）；E5 条目与现状矛盾 | ① 改路由整页表单（/users/new /groups/new——FR-145.1 承载 + E5 文本改写）② 修登记（内联卡维持，E5 改写为内联卡豁免） | 〔v1.0〕①路由化——BinFlow 内部已不一致（权限创建是路由页），统一路由形态 | **【已裁——conductor，出口①路由化】**FR-145.1 承载；E5 条目改写落 parity 册 v1.2（FR-141.2 同场） |
| **Q6**（C6） | **永不建边界**：§9 永不建 vs「全做」——洞察报表 / 漏洞合规 UI（xray 侧暂缓）/ Builds·Federation·Lifecycles·Release Lifecycle·Repository Path Map 管理面是否进 M16 | ① 不进——PRODUCT.md 门维持（解禁走专程）② 进——须先改 PRODUCT.md + Build-info/Release Bundle/AQL 六域随域 + 建议专程里程碑 | 〔v1.0〕①不进——与 Q13 同源；parity 收口程不被功能本体稀释（M14 防线延续） | **【已裁——用户，出口②进·两程结构】产品域全部进但拆两程**：M16 = 交互 parity + i18n + cron + Annotate + 远端浏览 + AQL 副线；**M17 预立项段** = Builds/Build-info + Release Bundle + 洞察报表 + Federation/Lifecycles（须 PRODUCT.md 修订 + ADR 群，候 M16 收口后 PM 正式立项——§0.3/ROADMAP）；Xray 维持唯一不做 |
| **Q7**（C7） | **Annotate 动词**：M1 子集豁免（read/write/delete/manage）被 intake ⑤ 重开——Artifactory 5 列（含 Annotate + write 拆 Deploy/Cache） | ① 加 Annotate + write 拆分（BE 动词域 + 数据迁移 + FE 矩阵——FR-146.1/LC-88）② 维持四动词子集（豁免登记翻案被否留痕） | 〔v1.0〕倾向①加——M10 属性系统已有动词基础，映射成本可控；迁移方案须 architect 会签零提权 | **【已裁——conductor，出口①加】**FR-146.1 承载（迁移零提权 architect 会签）；LC-88 归 A |
| Q8（C8） | 表单域豁免缺口：maxUniqueSnapshots/repoLayoutRef（M4 延期裁定）+ blackedOut/archiveBrowsingEnabled（无豁免条目）——补齐范围 | ① 全补（表单域 + 行为语义随票——FR-143.2）② 只补 UI 域（行为化另票） | **①全补**——API 已收全，行为联动随票评估（repoLayoutRef 留 K 项） | 批次②断言冻结前；FR-143 |
| Q9（C9） | 自有增强层去留：仓库详情中间页 / children 表复合 / E4 测试器·diff / 快搜范围页签缺位 / 重置钮 / mimeType·校验块 | 逐项三态（维持自有登记 / 收敛对齐 / 条件保留） | children 表**收窄**（主路径走树叶子）/ 详情中间页**维持** / E4 **维持**（A8）/ 重置钮**移除** / 快搜范围页签**维持缺位**（R2 既定）/ 校验块收进伴随形态 | 批次①③断言冻结前；FR-142/144 |
| Q10（C10） | 条件票机制 vs 直接排期：NuGet symbol（五承未触发）/ docker virtual（Q6 已裁开禁）/ Tokens 字段核验（候活体源） | M16 转正排期 or 续滚 | docker virtual **滚入 M16 首票**（已终裁）/ NuGet symbol **六承转正排期**（P2 尾波——mini as-built 规格随票）/ Tokens 核验**维持候源不排期** | 尾波条件窗 |
| Q11（C11） | webhook 57 型休眠裁剪（ADR-0041 决策 7 翻转路径在案）——剔 xray 域后非 xray 触发源裁剪待裁 | ① 裁剪（仅注册有源域——非 xray 域触发源激活清单随票）② 全量注册（含无事件休眠型） | **①裁剪**——与 ADR-0041「有源域」防线一致 | 尾波（随 webhook 域二程评估票） |
| Q12（C12） | t381 事故残留（VM 取证快照 15MB + 空仓；REST 删除被 OSS license 门挡） | M16 清理窗处置 or 维持登记 | conductor 决定项（非 PM 裁量）——建议尾波随 release 票处置 | 尾波（conductor） |
| Q13（C13） | **「全做」语义总界定**（= §0 两口径） | ① 控制台交互 parity 收口（推荐——v1.0 已按此起草）② 产品域扩张（A1 族——须 PRODUCT.md 修订 + 专程里程碑 + ADR 群） | 〔v1.0〕①——推翻仅影响 §0.3 A1 域归属与 §2.1 J 行，不影响 B 清单主轴 | **【已裁——随 Q6，两口径并行·两程拆解】**§0.2 终裁落章：M16 承载交互 parity（+i18n/cron/Annotate），M17 预立项段承载产品域；主轴四批次零影响 |

> **v1.1 总账**：必答七项 Q1~Q7 已全部终裁落章（用户四项：Q1 引入 cron / Q3 双语 / Q4 页码 / Q6 两程结构——均为翻案或超推荐口径出口；conductor 三项：Q2 收紧不倒退 / Q5 路由化 / Q7 加——均按 PM 倾向出口）；Q13 随 Q6 落定；**「待裁」层零滞留**（LC-88/LC-91 已归位——LC-56/M15 Q1 教训防范兑现）。余 Q8~Q12 可后裁按建议裁点推进（Q8/Q9 批次②①断言冻结前 / Q10 尾波条件窗 / Q11 尾波 / Q12 conductor）。

---

## 8. M16 验收剧本（QA 总纲）

1. **前置锚先行**：L35a（Q 终裁归位〔**v1.1：必答七项已落章**〕+ parity 册 v1.2 + reverse §3.2 回填 + 就绪度确认）——四批次断言的地基；规格增量段（aql.md / remote-browsing.md / cron 表达式子集锚）与 **ADR-0044（cron 调度域——Accepted 前置）**、Annotate 迁移会签核查。
2. **回归基线（硬门槛先行）**：M1~M15 全部 P0 序列双形态复跑全绿；断言反转①~⑤现值预核实（在案——as-built 形态审计材料 B 项即基线）。
3. **批次①（P0）**：L35（文件叶子/选择语义/URL 模型/工具带 + 性能基线 + spec 翻新）——tree-deep 先行，用户主诉第一收口。
4. **批次②**：L36（三段结构 + 字段域表驱动 + 8 包型八型真实客户端矩阵 + 入口/列集/dirty-gating/Test）。
5. **BE 小域穿插**：L38（统计基建——批次③前置）→ L39（Annotate 候裁臂）。
6. **批次③**：L37（页签序/字段族端到端/属性编辑/搜索栈 + M15 AQL 面零回归）。
7. **批次④**：L40a（路由表单化/两步弹窗/能力位/profile/监控/帮助导航）→ L40b（cron 消费面——引擎断言归 L48/FR-150）。
8. **远端浏览**：L41（可选档双态 + 降级 + 口径扩面对账）。
9. **副线**：L42（statistics/usage 单源一致 + QRL + UI 族/dates + §5.7 对账）。
10. **新增域**：L47（i18n——切换器/文案外提 CI 断言/双语断言策略/axe 双 locale）→ L48（cron 调度域——三消费面/next-run/零重复投递/audit——ADR-0044 Accepted 前置）。
11. **条件池**：L43（按触发逐项——未触发 BOARD 留痕非 DoD 缺口）。
12. **收口**：L44（全量回归 + 断言反转①~⑦归属审计 + **B 47 项收口审计表** + parity 册终评）→ L45（NFR 与性能门槛 + F1 趋势登记）→ L46（文档实测复跑）。
13. **文档**：tech-writer——树深链与工具带 / 表单三段与 8 包型开禁 / 详情字段族 / 权限动词与安全面 / 监控面 / 远端浏览可选档 / AQL 高级面 / **i18n 双语切换与 cron 调度指南** / **豁免翻案用户可见变化公告**（E 系翻案项 + cron 推翻 M15 裁定逐一明示）。
14. **release**：部署烟测 + UAT 随里程碑 PR；8 包型开禁面、统计基建与 cron 调度在 UAT 链取证。

---

## 9. M16 DoD

1. §4 全部 P0 AC（FR-141 前置锚 / FR-142 批次①）经 qa 验证全绿；P1（FR-143/144/145 主体 / FR-146 含 Annotate / **FR-149 i18n / FR-150 cron 调度域** / FR-147 / FR-148 P0 腿）全绿；P2 与条件腿（监控/帮助 / UI 搜索族 / dates / NuGet symbol Q10 / E7 / Tokens / t381）按条件条款——未触发不构成 DoD 缺口，须 BOARD 留痕；
2. §8 剧本全绿；§1.2 量化门槛表逐行达标（树栈四项 / 收口率 / 八型 roundtrip / 统计单源 / 远端浏览双态 / 副线对账——全部有 Playwright/curl 实测证据归档）；
3. **parity 专项收口**：B 偏差 47 项收口审计表归档（四态归属零无主项——翻正/豁免翻案〔Q 编号可回溯〕/stay-out 确认/候裁挂起）；parity 册 v1.2 终版（E5/E1 矛盾清零 + 翻案双留痕 100%——含 E6 双语 / E2 页码 / cron 推翻三例 v1.1 翻案）；豁免翻案全部有 Q 终裁依据（零默默翻转审计）；
4. 回归硬门槛：M1~M15 全部 P0 序列双形态复跑全绿；M15 as-built 零行为变化（豁免票归属外——AQL 引擎/复制包 B/搜索面）；断言反转①~⑤归属审计 100% M16 豁免票；FE 变更面 100% 归属 M16 票 + FE 票服务端 diff=0（BE 腿票除外）；
5. 前置产物与 Q 归位：Q1~Q13 逐项归位（**v1.1：必答七项 + Q13 已落章，「待裁」层零滞留**；余 Q8~Q12 按建议裁点）；parity 册 v1.2 + 规格增量段（aql.md / remote-browsing.md / cron 表达式子集锚）+ **ADR-0044（cron 调度域——Accepted）** + Annotate 迁移会签齐备；
6. NFR-P70~P72 / NFR-S76~S78 达标归档；`make test`（race）全树绿维持 / `make lint` 0 issues / gofmt 空；资源门三连 + SPA ≤10KB/票 + F1 趋势登记；
7. §2.2 Non-goals 与候选池对账完成：滚入 M17+ 项在 ROADMAP「M16 未纳入项」登记（备稿沿 T-395/T-427 先例收口窗启用）；**M17 预立项段衔接**——产品域四主目 + 追加候补在 ROADMAP 预立项段在案（候 M16 收口后 PM 正式立项——PRODUCT.md 修订前置未发生即不启动）；§1.4 出处义务在全部 M16 票可审计（审计材料 B 项/parity 册锚点逐条）；
8. 主会话 git tag `m16-done`（对外发布任何制品先经用户确认；PR 化合并沿既定 gitflow 程序；UAT 证据随里程碑 PR 归档）。
