# PRD — M12 Artifactory 对齐第三程：NuGet 面补全 + 制品生命周期域 + 行为债收口

> **PRD 状态：v1.0（2026-08-28，待 conductor 审）**。主轴来源：用户三项裁决（2026-08-28 07:5x）中的裁决①（NuGet 对齐 bundle → M12 立项）与裁决③（D-A dual-write S3 停机 → M12 补 fail-open）+ 收口裁定⑤（D-8R 瘦身票 M12 承载）+ T-329 登记（D-F conan v1 delete 状态码）；范围裁量：ROADMAP「M11 未纳入项」候选池 PM 聚类（Trash can / HelmOCI / MUI 批三 / 回写批 / 票级遗留收编；主轴增量选题 = 制品操作族——与 Trash can 同生命周期域）。**新端点全部走 PM FR + ADR 流程（ADR-0040 起）**；19:05 行为逐项对齐条款为常设工作准绳（M11 §1.4 制度延续）。

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/milestone-12.md` |
| 里程碑 | M12 — Artifactory 对齐第三程（NuGet v2/v3 search 补全 + 制品操作族与回收站 + dual-write fail-open + 瘦身与遗留收口） |
| 状态 | v1.0 草案（FR-103~FR-113 十一条需求；契约矩阵 15 条〔A 13 / C 1 / D 1 / 待裁 0〕+ 档位矩阵增量 2 行〔trashcan 新槽 Q3 / helmoci 转正〕；L01~L35 验收命令骨架；开放问题 Q1~Q7 带暂行） |
| 上游依据 | PRODUCT.md（Non-goals 不越界：HA/Xray 本体仍不做——Q1 维持）、BOARD.md（2026-08-28 07:5x 用户三项裁决 + 收口裁定⑤留痕；M11 票据节全程在档）、ROADMAP「M11 未纳入项」（范围基线）、docs/reverse/artifactory-full-feature-matrix.md（缺口 3/5 行 + §十大缺口）、reports/agents/T-304.md（§1.1 NuGet 七项复核 + §4.1 bundle 佐证 + §7 新发现——FR-103/104 行为出处直取）、reports/agents/T-327.md（D-A 证据链）、reports/agents/T-329.md（§7 D-F/D-8R 登记 + DoD 核）、reports/agents/T-300.md（批次三候选清单）、reports/agents/T-332.md（architecture §15.4/§23 转交 + token 窄域化候选）、reports/agents/T-318.md（cargo.md §8 回写转交）、reports/agents/T-312.md（conan D1/D5/D7/D8 升置信转交）、reports/agents/T-316.md（R-3/R-4 登记）、reports/agents/T-313.md（D-2/D-3/D-5 登记）、reports/agents/T-325.md（拒启序登记）、docs/prd/milestone-11.md（体例 + §5.6.1 T-290-2 登记）、docs/prd/milestone-10.md（§4.3 T-293 登记——nuget.md「立项随票补 as-built 规格」终裁口径 + 档位矩阵基线）、ADR-0001/0032/0039（clean-room / license 门控 / MPU 新 wire） |
| 下游消费者 | tech-lead（拆票——宽度 ≤2 内建，§1.3 分票提示）、architect（**ADR-0040**：dual-write fail-open 队列语义；视 Q3 终裁 ADR-0041 trashcan 槽；architecture.md §15.4/§23 回写票）、reverse-engineer（nuget.md 新建 + repo-operations.md mini 规格 + cargo.md §8 / conan.md 升置信回写票）、dev-go-core（internal/adapter/nuget v2 + 瘦身 + storage 面小票）、dev-registry-adapter（制品操作族联动 + HelmOCI + conan/cargo 收尾）、dev-go-storage（fail-open 队列 + 拒启序）、dev-go-core/dev-frontend（Trash can BE/FE + MUI 批三 + web 策略键表单）、qa-engineer（L 序列 + 断言反转三处 + 双形态回归）、tech-writer（五类文档增量）、release-engineer（部署烟测 + UAT 链）、conductor（裁决入口 + tag m12-done） |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-28 | 初版草案（待 conductor 审）：M12 范围（必须纳入四项用户裁决/裁定承载 + 候选池 PM 聚类收编 + 主轴增量选题制品操作族）、FR-103~FR-113、档位矩阵增量 2 行、契约矩阵 15 条、L01~L35、开放问题 Q1~Q7 带暂行；随稿完成 ROADMAP M12 立项行 + 主轴重排（PM 职责内两处） |
| v1.1 | 2026-08-30 | T-370 文面裁定包增订（FR-120.2/120.3 + D-356-3/4 残余收口；含收官笔头批 `346485e` 两处先行修正〔§1.2 量化门槛 cargo 行 + FR-113 AC5 拒启序〕并入本版记账）：① cargo 409 记账修正——FR-110.4/AC3/LC-43/L24 四处由「死上游 search → 409」单臂改为双姿态（默认 404 unfound / hardFail 409+errors 信封，照 cargo.md §8.1 R-3 登记；T-355A §2.3 修正案）；② FR-113.5 正文与 L31 命令注对齐 AC5 终验口径（binstore.yaml 在场时其链语义优先——T-349 设计化，D-356-4）；③ flat 措辞回写——八处「flat 扁平化/flat 模式」统一为「flat 折叠进 copy 主参数族（`/api/flat/*` 不实现 → 404）」口径（repo-operations.md §1.6 裁定；T-356 L12 文面债；artifact-operations.md L66 已是折叠口径无需改）；④ FR-107 AC2 加注——「停机窗内重启」解释空间收口（as-built boot 探针 fail-closed；加注不改行为、ADR-0040 零修改；T-356 观察⑨） |

---

## 1. 背景与目标

### 1.1 背景

M11 以 `m11-done`（2026-08-28）收官：四包型（conan/deb/rpm/helm）+ cargo 家族三态齐装、认证/存储配置化、MUI 两批、回头看 30 项 100% 落地、复制硬化与工程债主体清偿。M12 面对四股输入的汇合：

1. **用户裁决与收口裁定（必须承载，P0 锚）**：2026-08-28 07:5x 三项裁决中的①③——**NuGet 对齐 bundle**（v2 全面实装 + remote/virtual search 上游代理 + service index 动态解析；T-304 §1.1/§4.1 规格出处齐备可直取）与 **D-A dual-write S3 停机 fail-open**（本地优先写 + 异步 S3 重试队列；M6 PRD FR-50 文面维持，实现债 M12 补）；收口裁定⑤——**D-8R 空载 RSS 瘦身**（懒加载 embed，138.7MB→≤100MB，非 m11-done 阻塞的登记债）；T-329 登记——**D-F conan v1 delete 状态码小票**（`_/_` 坐标删树成功回 404 而非规格 200）。
2. **候选池同域聚类（PM 裁量收编）**：Trash can（Q7 未触发 → M12 与 Cleanup-Retention 同域立项窗口；主矩阵缺口 3 原文「删除即永久是数据安全事故源」）；HelmOCI（Q2 条件票未触发，成本低——docker 面复用 + 三 media type）；MUI 批次三（T-300 候选清单在案）；架构/规格回写批（T-332/T-318/T-312 转交项集中消化）；票级遗留高价值项（T-290-2 / byHash 枚举 / web 策略键表单 / token 窄域化等按域打包）。
3. **主轴增量选题（PM 选题，§2.2 留痕）**：制品操作族（copy/move/zip/`archive!/`/explode——主矩阵缺口 5「日常运维高频操作」）进 M12——与 Trash can 同属制品生命周期域（删除=move to 回收站，同一底座）、与 M11 复制硬化/属性系统基建直接衔接；**HA 本体不进**（Q1——大件 + PRODUCT.md Non-goal 未解禁，建议 M13 专程单列），AQL/Webhook/Build-info/Go 深化/license 公钥覆盖滚 M13+。
4. **NuGet 规格债的清偿窗口**：M10 T-280 规格票静默丢失、T-287 依官方规范合规实现（T-293 终裁「免补 retro 规格；**硬化或 symbol server 立项时随票补 as-built 规格**」）——M12 NuGet bundle 立项即该口径的兑现时点：nuget.md 以「as-built 面 + 增量对齐出处（v2 路由全集 / search 代理链 / 阶梯常量表）」形态前置。

**不贪多**：M12 无 M11 式四包型批量的体量红利，以「一条 P0 主线（NuGet bundle）+ 一个增量主轴（制品生命周期域）+ 债务与遗留系统性收口」为形态；任何 Q 触发的增项（HA 本体、symbol server 之外的新面）必须等量置换。

### 1.2 M12 目标与量化门槛

> 一句话：兑现用户裁决①③与收口裁定⑤（NuGet bundle / dual-write fail-open / RSS 瘦身），把制品生命周期域（copy/move/归档族/回收站）立起来，偿清 M11 转交的规格/架构/遗留债——全程延续行为逐项对齐制度。

量化门槛（未达即里程碑不完成）：

| 指标 | M12 门槛 | 来源 |
|---|---|---|
| NuGet v2 面覆盖 | T-304 §1.1-L2 路由全集逐端点断言绿（含负面臂）；M10「v2 仅 FindPackagesById」负向断言反转 | FR-103 |
| NuGet publish 重复臂 | 同 id+version 二次 push → 409 CONFLICT；d 权限主体重传 → 覆盖（T-304 §7 新发现语义） | FR-103 |
| NuGet search 时效 | remote 仓可搜到**上游未缓存**新发布版本（发布→可搜，无缓存落地前置）；virtual 合并两域并见 | FR-104 |
| service index 自适应 | 异构上游自定义 @id 路径 → flatcontainer/registration/search 三面 URL 零配置解析（前缀常量退役） | FR-104 |
| 制品操作 | copy/move 树级 + dryRun 零副作用 + flat 折叠进 copy 主参数族（`/api/flat/*` → 404）；属性/校验和随行；协议仓索引联动重算；万节点树 copy 零 5xx + sha256 对账 | FR-105 |
| Trash can | 删除→打标（四元组）→恢复 roundtrip sha256 一致；保留期自动清理；empty 审计 | FR-106 |
| dual-write fail-open | S3 停机窗 PUT/GET 零 5xx（T-327 D-A 两处 500 消除）+ 恢复后排空对账零缺 + 队列重启幸存 | FR-107 |
| 资源门转绿 | `make footprint` ≤100MB（D-8R 红→绿）；`make check-size` 六平台聚合 ≤100MiB 维持；冷启动 <2s | FR-108 |
| HelmOCI | `helm push/pull oci://` 全链绿 + manifest 三 media type 断言 + docker 面零回归 | FR-109 |
| 包型收尾 | D-F `_/_` 坐标 delete 200；forceConanAuthentication 生效；cargo 死上游 search 双姿态（默认 404 unfound / hardFail 409——终验修正：409 在 remote search 上游异常臂非 publish，T-355A 查证） | FR-110 |
| MUI 批三 | 四闸门 + axe 双主题 0 + SPA gzip 相对 T-291 基线累计 ≤25% 维持 | FR-111 |
| 回写批 | architecture §15.4/§23 + cargo.md §8 + conan 升置信三处落盘 + `make docs` 绿 + D-G/D-H grep 零残留 | FR-112 |
| 遗留小票 | §4.11 六项逐条 AC 绿（113.1~113.6） | FR-113 |

### 1.3 上游依赖与并行关系（含分票提示）

- **前置产物（拆票前）**：① **nuget.md**（reverse-engineer，新建——as-built 面 + 增量对齐出处双段：M10 v3/v2 as-built 行 + T-304 §1.1 出处列展开〔v2 路由全集逐端点 / FeedUtils 阶梯常量表 / search 代理链〕，tech-lead 就绪度确认）；② **repo-operations.md** mini 规格（reverse-engineer——copy/move wire〔树级/dryRun/flat/dry+failFast〕+ `archive!/`〔strictArchiveDotSlash〕+ 目录 zip〔folderDownloadConfig 四限参 + `/api/archive/download` + entry 抽取〕+ exploded 上传〔explodedArchiveExtensions + X-Explode-Archive〕，逐条附 Artifactory 出处——主矩阵 §C/归档族行锚点已在案）；③ **ADR-0040**（architect——dual-write fail-open：停机窗语义/队列持久化与退避/排空对账/与 M6 三模式状态机交互）；④ Q3 终裁后视需要 **ADR-0041**（trashcan 槽位归属，可并入 ADR-0040 批次）。Trash can mini 规格随票（T-330 既定模式，不单列前置）。
- **BOARD 既定票**：T-330（Trash can 条件票）转正进 M12（FR-106 承载）；T-320（HelmOCI 条件票）未派 → FR-109 承载（票号沿用由 conductor 定）。
- **依赖序**：FR-103/104 dep nuget.md；FR-105 dep repo-operations.md；FR-106 dep FR-105 move 底座（同一生命周期域，先后脚）；FR-107 dep ADR-0040；FR-112.1 的 remote 字段 as-built 回写在 FR-113.1 落地后以终态回写（两票时序协调，拆票注明）；FR-111 与 FR-106 FE 面（web/ 域）互斥分批。
- **实现分区与分票提示（宽度 ≤2 内建）**：FR-103 拆 2 票（规格票 → v2 实现大票〔internal/adapter/nuget，窗口独占〕）；FR-104 一票（dep 规格票，同 area 串行随 v2 票后）；FR-105 拆 3 票（规格票 → copy/move 核心票〔storage+httpapi〕→ 归档族票）；FR-106 拆 2 票（BE〔storage 统一删除 seam + cron〕→ FE 最小面〔web/〕）；FR-107 一票（dev-go-storage）；FR-108 一票（dev-go-core 懒加载）；FR-109 一票（P2 段并入）；FR-110 拆 1~2 票（conan / cargo 分 area）；FR-111 一票（web/）；FR-112 拆 2 票（architect / reverse-engineer，docs-only 可与实现票并行）；FR-113 拆 2~3 票（repo config 域 / auth+storage 域 / 测试基建）；QA 两票（中期回归 + 终验）；tech-writer 一~两票；release 一票（部署烟测 + UAT 链）。估 **22~28 票**（含条件票 slot）。
- **QA 并行面**：NuGet 需 dotnet 8 容器 + 自指/异构上游夹具（自定义 @id 的 mock service index）；fail-open 需 MinIO stop/start 编排；制品操作需多协议仓夹具（deb/rpm/conan/npm 各一）；Trash can 需短保留期时钟夹具；断言反转三处（X-Explode 400→接受 / footprint 红→绿 / nuget v2 404→全集）须 PRD 回写核实。

### 1.4 全程工作方式条款（M11 §1.4 制度延续——常设准绳，M12 全程生效）

1. **行为逐项对齐**：本里程碑全部范围（NuGet 面、制品操作族、Trash can、HelmOCI、包型收尾、遗留改回项）的可观测行为 / 命名 / 语义 / 错误码 / 交互 100% 照 Artifactory；公开规范覆盖处以官方规范为准（NuGet v2/v3 API、OData、crates.io 等），规范未覆盖或 Artifactory 特有扩展（管理面、默认值、错误码超出规范处）照 Artifactory。
2. **自有裁定权收归用户**：任何与 Artifactory 的行为分歧必须上 BOARD 请用户裁决——本 PRD 暂行值仅为开工口径，标注「暂行待裁」（§7 Q 表）。
3. **规格票出处义务**：nuget.md / repo-operations.md / Trash can mini 规格逐条给出「Artifactory 行为出处（反编译类/方法 + 行为描述）或官方规范锚点」。
4. **红线保留（ADR-0001 不变）**：不逐行翻译 Java→Go；license 文档格式维持自有 ed25519。
5. **流程条款（延续）**：新端点走 PM FR + ADR 流程，实现票不得私加端点；拆票宽度 ≤2；协议/行为面票必须依赖对应规格票；每票 AC 附真实客户端验收命令。

---

## 2. 范围

### 2.1 In scope

| # | 来源（指令/裁决/登记） | 本 PRD 功能需求 | 优先级 |
|---|---|---|---|
| A | 用户裁决①（07:5x）+ T-304 §1.1/§4.1 | FR-103（NuGet v2 数据面全面实装：路由全集 + OData 参数面 + publish 重复臂）+ FR-104（v3 search 上游代理 + service index 动态解析 + virtual 合并） | P0 |
| A | 用户裁决③（07:5x）+ T-327 D-A | FR-107（dual-write S3 停机 fail-open：本地优先写 + 异步重试队列 + 排空对账；M6 FR-50 文面维持） | P1 |
| A | 收口裁定⑤（BOARD 留痕）+ T-329 D-8R | FR-108（空载 RSS 瘦身：懒加载 embed，footprint 门红→绿） | P0 |
| A | T-329 D-F 登记 | FR-110（包型收尾小票包：conan D-F 状态码 + forceConanAuthentication + 活体互证条件腿；cargo R-3/R-4 + DELETE 收敛） | P1 / P2 |
| B | 候选池聚类（ROADMAP M11 未纳入项）+ T-330 定名 | FR-106（Trash can 回收站：auto-trashcan 内置仓 + 打标 + 保留期 + restore/empty/clean + 控制台最小面） | P1 |
| B | 主轴增量选题（PM，§2.2 留痕）+ 主矩阵缺口 5 | FR-105（制品操作族：copy/move〔树级/dryRun/flat 折叠进主参数族/属性与索引随行〕+ `archive!/` + 目录 zip + exploded 解包上传） | P0（copy/move）/ P1（归档族） |
| B | Q2 承接（T-320 未派）+ T-313 D-2/D-5 | FR-109（HelmOCI 分发 + oci:// 透传 + chartsBaseUrl P2） | P1 |
| B | T-300 候选清单 | FR-111（MUI 批次三：五页面 + 共享组件六件套 + combobox 统一化——交互零变化） | P1 |
| C | T-332/T-318/T-312 转交集中消化 | FR-112（架构/规格回写批：architecture §15.4/§23 + cargo.md §8 + conan.md 升置信 + D-G/D-H 校验） | P1 |
| C | 票级遗留按域聚类（T-304 §5.1/T-327R/T-332/T-305/T-325 登记） | FR-113（配置与治理域遗留小票打包：T-290-2 / byHash 枚举 + web 表单 / token 窄域化 / auth 尾巴 / 拒启序 / CI runner） | P1 / P2 |
| D | 候选池裁量 | （不设 FR）NuGet symbol server 余量条件票（Q2）；其余主轴候选滚 M13+（§2.2） | — |

前置产物（非 FR）：nuget.md、repo-operations.md、ADR-0040（、视 Q3 终裁 ADR-0041）。

### 2.2 Non-goals — M12 明确不做

**产品级（继承 PRODUCT.md，不越界）**：HA 集群本体与 Xray 集成面本体不做（槽位维持 enterprise 占位）——「行为逐项对齐」指令不自动解锁功能本体，须用户修订 PRODUCT.md（Q1）。不做 Artifactory 全量 REST 兼容（高频子集承诺维持）。

**M12 里程碑级 Non-goals（含主轴重排留痕——滚程去向全部登记）**：

| 不做项 | 隔离边界 / 去向 |
|---|---|
| **HA 本体**（集群心跳/推举/传播）+ Xray 集成面本体 | Q1——M12 不进（大件 + Non-goal 未解禁 + M12 已满载）；PM 建议 **M13 专程单列**（须先修订 PRODUCT.md + ADR 群）；用户可推翻插队（进则等量置换） |
| AQL + 13 老搜索族 | M13+（搜索基建专程；主矩阵缺口 2） |
| Cleanup/Retention **策略引擎**（治理面：规则/cron 策略/冷存储联动） | M13+（缺口 4）——与 FR-106 分界：M12 仅回收站语义（删除入站/保留期/恢复），策略引擎不混入；勿混淆 |
| Webhook / 统一事件总线（36 事件） | M13+（缺口 6，主矩阵判定可整体平移） |
| Build-info 域 | M13+（缺口「CI 集成半壁」，专程） |
| Go 深化（sumdb 代理 + external 重定向） | M13+（PM 排期留痕：非淘汰，M12 主轴容量让位 NuGet/生命周期域） |
| NuGet symbol server | **余量条件票**（Q2，非 DoD 硬门）——全部 P0/P1 收官且余量足则触发；mini as-built 规格随票（T-293 终裁口径）；未触发 M13+ |
| license 公钥 config 覆盖（运行时换钥） | M13+（T-293 终裁③走新 ADR——安全面专 ADR，不与对齐程混编；PM 留痕） |
| 制品 license 识别（licences.xml 91 模式）/ 冷存储分层 | M13+（冷存储随 Cleanup-Retention 域） |
| HuggingFace 等 AI/ML 13 型 | 远期分期（用户指令③远期主体） |
| Terraform / GitLFS 实现 | M13+（规格随 M13 规划另派——FR-91 余量第 6/7 份） |
| deb snapshot 族（T-310 §10 缓议） | M13+ 维持缓议 |
| deb Packages.bz2 压缩档 | **维持不做**（Q7——dsnet 依赖不可得实证在案〔T-314〕；plain+gz+xz/lzma 已落地，apt 功能面零损；LC-45 D 层留痕，用户可推翻） |
| statisticsEnabled / sourceOrigin 行为化 | M13+（T-317 差异 2——待 stats 面立项一并） |
| 属性复制协议面（conan/deb/rpm/helm/npm 属性随复制到协议仓） | M13+（T-317 差异 3——归各适配器，随各包型深化票；M12 FR-105 属性随行覆盖 generic + 索引联动重算路径） |
| crates.io 直连双主机（R-2） | 维持登记（结构性限制——文档说明即可，T-316 在案） |
| keypair T-319 D-1~D-8 / SAML T-331 D-1~D-5 差异族 | 维持在案（各票报告登记，低风险，用户可推翻）——除 SAML POST key FE 入口一点随 FR-113.7 待裁 |
| 逐行翻译 Java→Go / 复制 JFrog license 密钥格式 | **永久不做**（ADR-0001） |
| M11 未纳入项其余维持项（mc tag 锚定、观测类登记） | 维持在案（各票报告在档） |

---

## 3. 用户与场景（M12 视角）

- **场景 A（.NET 团队，NuGet 补全）**：内网既有 v2 老工具链（TeamCity 步骤/nuget.exe 老版）与 v3 新链（dotnet 8）并存——`nuget list`/`nuget install -Source`（v2）与 `dotnet add package`（v3）都全功能可用；remote 仓能搜到上游刚发布的版本，不等缓存落地。
- **场景 B（管理员，数据安全）**：同事误删了发布仓里的关键包——14 天内从回收站恢复（打标留删除人与原路径），sha256 逐位一致；「删除即永久」的事故源消除。
- **场景 C（迁移管理员，仓间搬运）**：从旧仓把制品树搬到新仓：`POST /api/copy` 先 dryRun 预演看冲突报告，确认后执行——属性、校验和、协议仓索引（Packages/repomd/index.json）随行重算；flat 需求由 copy 主参数族承载（`/api/flat/*` 不实现 → 404，Artifactory 默认部署同姿——repo-operations.md §1.6 裁定，v1.1 措辞回写）。
- **场景 D（运维，S3 检修窗）**：dual-write 迁移期撞上 S3 检修——上传照常 200（本地落盘 + 队列）、下载照常 200（存量 fallback）；S3 恢复后自动排空补齐，对账零缺。
- **场景 E（运维，资源基线）**：升级 M12 后空载 RSS 回到 ≤100MB（PRODUCT.md 基线），冷启动不见劣化。
- **场景 F（平台工程师，Helm OCI）**：`helm push oci://` 与经典仓同仓体验；虚仓里 Helm 与 HelmOCI 不混仓的边界清晰。

---

## 4. 功能需求

约定：`BASE=http://127.0.0.1:8080`；`ADMIN="admin:password"`；Playwright spec 置 `web/e2e/m12/`；NuGet v3 挂载沿 M10 as-built（`$BASE/binflow/api/nuget/v3/{repoKey}/index.json`）、v2 基址沿 M10 `FindPackagesById()` 既有挂载（全表以 nuget.md 锚点为准）；新端点 wire 细节以 nuget.md / repo-operations.md / ADR-0040 定案为准，本节 AC 钉行为与状态码；**行为基准冲突时的效力序：用户裁决（BOARD）> 规格票（含 as-built 段）> ADR > 本 PRD 暂行值**。全部实现票不得私加端点。

### 4.1 NuGet 对齐 bundle·上（用户裁决①，P0）

#### FR-103 NuGet v2 数据面全面实装（internal/adapter/nuget；T-304 §1.1-L2 改回兑现）

**用户故事**：
- 作为 v2 老客户端用户（nuget.exe 2.x/3.x、TeamCity NuGet 步骤、内网脚本），`nuget list` / `nuget install -Source` 走 v2 OData 面与官方源体验一致——不再撞 404。
- 作为 CI 管理员，v2 面的删除/下载/`$batch` 批查可脚本化运维。
- 作为包作者，push 撞重复版本得到与 Artifactory 一致的 409 与覆盖语义（有删除权限可重传覆盖）。

行为规格：

- **103.1 前置规格票（reverse-engineer）**：**docs/reverse/nuget.md 新建**（as-built + 增量对齐双段——M10 §4.3 T-293 终裁「立项随票补」口径兑现）：as-built 段固化 M10 v3/v2 既有面（含 7 项自有裁定维持项）；增量段逐条附 Artifactory 行为出处——v2 路由全集（T-304 §1.1-L2 出处列展开）、OData 查询参数支持面（`$filter`/`$orderby`/`$top`/`$skip`/`$inlinecount`——支持子集以取证为准）、FeedUtils service index 资源类型阶梯常量表（供 FR-104）；tech-lead 就绪度确认。
- **103.2 v2 路由全集**：`Search()` / `Search()/$count` / `FindPackagesById()`（±`/$count`）/ `Packages(id)` / `Packages(id)/Id` / `Packages()/$count` / `GetUpdates()`（±`/$count`）/ `$batch` / `Download/{id}/{ver}/{file}.nupkg` / `DELETE` / `PUT×2`；`$metadata` 维持既有；id 小写折叠与版本归一化沿 L1/L6 既有维持裁定。
- **103.3 publish 重复臂（T-304 §7 新发现）**：同 id+version 二次 push → **409 CONFLICT "Package already exist"**；主体有 d 权限 → 覆盖重传（无冲突臂）——与 cargo D-3 终态同构（Artifactory 包型间同构事实）。
- **103.4 门控**：nuget 槽（pro）既有——v2 新端点自动受三缝语义（零新织入点）。

验收标准（AC）：

- **AC1（规格票交付）**：nuget.md 落 docs/reverse/（双段 + 出处逐条 + 置信度标定）；tech-lead 拆票就绪确认。
- **AC2（v2 sweep）**：路由全集逐端点 curl 断言——Search 命中/未命中双臂、`$count` 数值一致、`Packages(id)` 条目元数据、`GetUpdates` 增量集、`$batch` 批查、Download nupkg 字节 sha256 对账、DELETE 200/403、PUT 上传链；M10「v2 其余 404」负向断言**反转**（归属本 FR 豁免票）。
- **AC3（真实客户端）**：dotnet 8 容器腿（v3 面回归维持）+ nuget.exe 活体腿（可得则：list/install 经 v2 源全绿）；不可得则 curl 等价 + BOARD 留痕（M11 conan 1.x 同款路径）。
- **AC4（重复臂）**：同 id+version 二次 push → 409（信封逐字）；d 权限主体重传 → 覆盖成功（sha256 新值断言）。
- **AC5（门控）**：nuget 既有三缝序列复跑（community 建仓 403 → pro 200 → 卸载 pull 200/push 403），v2 面含内。
- **AC6（回归）**：M10 T-287 v3 序列（push/restore/remote pull-through/virtual）零回归。

### 4.2 NuGet 对齐 bundle·下（用户裁决①，P0）

#### FR-104 NuGet v3 search 上游代理 + service index 动态解析 + virtual 合并（internal/adapter/nuget；T-304 §1.1-L3-remote/L4/L7 改回兑现）

**用户故事**：
- 作为 .NET 开发者，remote 仓搜索能看到上游**刚发布**的包版本（实时代理，不等缓存落地）。
- 作为私有源管理员，上游是自建/异构 NuGet 源（BaGet/GitLab/另一 BinFlow）时，BinFlow 从其 service index 自适应解析资源 URL——上游改版无需等 BinFlow 发版。
- 作为虚仓用户，virtual search 同时返回本地私有包与远端成员的实时结果。

行为规格：

- **104.1 service index 动态解析（L4）**：以**资源类型优先级阶梯**替代 M10 前缀常量（`v3-flatcontainer`/`v3/registration5-gz-semver2`）——RegistrationsBaseUrl/3.6.0→/3.4.0→…→Versioned 常量表 + 偏好序列（SearchQueryService/FlatContainer 同构阶梯），照 nuget.md 增量段（出处：FeedUtils）。
- **104.2 remote search 上游代理（L3-remote）**：remote 仓 search 从上游 service index 提取 SearchQueryService @id 实时代理（Artifactory downloadSearchResult 模式）；**local 半边维持存储事实**（T-304 判定维持项，不动）。
- **104.3 virtual search 合并（L7）**：local 成员存储事实 ∪ remote 成员上游实时搜索（collectAllSearchResultDataItems 模式）；去重/排序形态照规格票。
- **104.4 降级语义**：上游不可达 → 复用既有 remote 故障降级（本地缓存事实兜底 + 降级标记），零新 SSRF 面（复用 M3 Guard 五参数 + ADR-0025 决策 4 私网开关）。

验收标准（AC）：

- **AC1（动态解析）**：自指上游（BinFlow nuget 仓指 BinFlow）或 mock service index（自定义 @id 路径）→ flatcontainer/registration/search 三面 URL 全部由上游 service index 解析（curl 断言 + 无前缀常量拼接痕迹）；改上游资源 @id 路径 → BinFlow 零配置跟随。
- **AC2（remote 代理）**：上游发布新版本（不预下载）→ v3 SearchQueryService 经 BinFlow remote 仓**立即可搜**（`dotnet package search` / curl q 断言）；数据面二次命中本地缓存断言维持。
- **AC3（virtual 合并）**：虚仓 = local（私有包）+ remote（公共包）→ search 两域并见；remote 新发布版本可搜（M10「virtual search=local+已落地缓存」断言**反转**，归属本 FR 豁免票）。
- **AC4（降级）**：停上游 → search 回缓存事实/降级形态（零 5xx）；恢复后自动回代理。
- **AC5（回归）**：M10 v3 local/remote 序列零回归。

### 4.3 制品操作族（主轴增量选题，copy/move P0 / 归档族 P1）

#### FR-105 copy/move + `archive!/` + 目录 zip + exploded 解包（internal/storage + httpapi + 各 adapter 索引联动；前置 repo-operations.md）

**用户故事**：
- 作为仓库管理员，我在仓间搬制品：`POST /api/copy`（或 `/api/move`）先 dryRun 预演，确认后执行——属性、校验和、协议仓索引随行；flat 需求由 copy 主参数族承载（`/api/flat/*` 不实现 → 404——repo-operations.md §1.6 裁定，v1.1 措辞回写）。
- 作为用户，我直接读归档内成员（`<file>.zip!/inner/path`）、整目录一键 zip 下载、上传归档自动解包部署。

行为规格：

- **105.1 前置 mini 规格票**：repo-operations.md——`POST /api/copy|move/{srcRepo}/{srcPath}?to=/{dstRepo}/{dstPath}`（树级、dryRun、flat〔dry+failFast〕）、`<name>.<ext>!/inner/path` 按需解包成员读取（strictArchiveDotSlash 严格模式）、目录/整仓 zip（folderDownloadConfig 默认关 + 1024MB/5000 文件/10 并发/匿名单独开关 + `GET /api/archive/download` + entry 抽取 + 计流量）、exploded archive 解包上传（explodedArchiveExtensions=zip,tar,tar.gz,tgz）——逐条附 Artifactory 出处（主矩阵 §C copy/move 行、归档族三行锚点在案）。
- **105.2 copy/move 核心（P0）**：树级复制/移动 + dryRun 预演（零副作用）+ flat 折叠进 copy 主参数族（`/api/flat/*` 不实现 → 404——repo-operations.md §1.6 裁定；dry/failFast 为 copy 既有参数，v1.1 措辞回写）+ 目标仓写权限门 + 属性（node_props）/校验和随行 + **派生索引联动**：源/目标为协议仓（deb/rpm/conan/helm/npm/nuget…）时触发对应索引重算链（消费各 adapter 既有 reindex 内核）；「系统内路径豁免」面（FR-97.1 DB-2 建立的豁免名录）收编为显式系统路径集。
- **105.3 归档族（P1）**：`archive!/` 流式成员读取（不解包落盘）；目录 zip（配置默认关 + 四限参 + 计流量审计）；exploded 上传——**替换 M10「X-Explode-Archive 显式 400 拒绝」为接受+解包**（白名单扩展名闭集外维持 400）。
- **105.4 门控**：暂行基座能力不新增 addon 槽（Artifactory 侧无 license 门标记在案；规格票复核发现门控则上 BOARD——Q4）。
- **105.5 回收站联动**：move 底座为 FR-106 复用（删除 = move to `auto-trashcan`）。

验收标准（AC）：

- **AC1（copy/move 全链）**：maven/npm 源仓→目标仓 `curl -X POST -u $ADMIN "$BASE/api/copy/<src>/<path>" --data-urlencode "to=/<dst>/<path>"` → 200 + 源/目标 sha256 对账 + `?properties` 随行 + copy 源保留 / move 源消失。
- **AC2（dryRun/flat 折叠；v1.1 措辞对齐 as-built——T-356 L12 实证）**：dryRun → 冲突/规模报告形态 + 源/目标零变化（GET 对照）；`/api/flat/{copy,move}` → 404（Artifactory 默认部署同姿）+ copy 主参数族 to/dry/failFast 断言 + failFast 停走臂。
- **AC3（协议仓索引联动）**：deb 仓 copy `.deb` → 目标 Packages/by-hash 重算；conan 仓 move recipe → 目标 index.json 修订链一致；trash restore 回原仓 → 索引重算（copy 与 restore 两臂，npm install 复验）。
- **AC4（archive!/）**：PUT zip 后 `curl $BASE/binflow/<repo>/<file>.zip!/inner/path.txt` → 成员字节一致；strictArchiveDotSlash 开启后违规形态照规格。
- **AC5（目录 zip）**：开 folderDownload → `GET /api/archive/download` → 解包逐文件 sha256 对账；默认关断言；超限（>5000 文件）拒绝形态。
- **AC6（exploded）**：PUT 带 `X-Explode-Archive` 的 zip → 展开多文件 + sha256 对账；非白名单扩展 400 维持（断言反转登记：M10 400→白名单内接受）。
- **AC7（规模与回归）**：万节点树 copy 零 5xx + 抽样对账；M1~M11 P0 抽样（存储/属性/门控）零回归。

### 4.4 Trash can 回收站（Q7 兑现，T-330 转正）

#### FR-106 删除入站 + 打标 + 保留期 + restore/empty/clean + 控制台最小面（storage 统一删除 seam + cron + web；mini 规格随票）

**用户故事**：作为管理员，误删制品 14 天内可恢复——删除人/时间/原路径留痕，恢复后逐位一致；「删除即永久」的数据安全事故源消除（主矩阵缺口 3 原文）。

行为规格：

- **106.1 mini 规格随票（T-330 既定模式）**：内置 `auto-trashcan` local 仓；删除拷入并打标（`trash.time` / `trash.deletedBy` / `trash.originalRepository` / `trash.originalPath`）；保留期默认 **14 天**；REST 族 `POST /api/trash/empty`、restore（to + transaction-size）、clean；目录级 trash 属性批量清理（主矩阵 §B/G + config.xml trashcanConfig 锚点在案）。
- **106.2 实现面**：删除路径收口（generic + 各协议仓删除经统一 seam）→ move 到回收站（复用 FR-105 move 底座）+ 打标；保留期清理 cron（复用 T-324 cleanup 三腿架构模式：session 扫掠/policy 删除/GC 双门协调）；恢复 = 反向 move + 属性复原 + 协议仓索引重算。
- **106.3 控制台最小面**：回收站浏览/恢复/清空（MUI；FR-111 组件纪律）；readonly_admin 只读。
- **106.4 档位（Q3 待裁，暂行）**：新 addon 槽 `trashcan`（kind 暂定 feature-gov）——community locked / pro+ unlocked；与 Artifactory 分级对照随 mini 规格取证上 BOARD 终裁。
- **106.5 与 Cleanup-Retention 分界**：本 FR 仅回收站；策略引擎 M13+（§2.2 留痕防混淆）。

验收标准（AC）：

- **AC1（删除入站）**：DELETE 制品 → 原路径 404 + 回收站可见 + 四元组打标断言（`?properties`）。
- **AC2（恢复 roundtrip）**：restore → 原路径 200 + sha256 一致 + 属性复原 + （协议仓）索引重算断言。
- **AC3（保留期）**：短保留期夹具 → 过期自动清理 + 审计/日志；期内不清理。
- **AC4（empty/clean）**：`POST /api/trash/empty` → 清空 + 审计行；权限门（非授权主体 403）。
- **AC5（门控）**：按 Q3 终裁断言（community/pro 形态）。
- **AC6（回归）**：M1~M11 删除族断言（GC/grace/quota/cleanup 引擎 T-324 三腿）零回归——删除语义变化的全部断言翻转 100% 归属本 FR 豁免票。

### 4.5 dual-write S3 停机 fail-open（用户裁决③）

#### FR-107 本地优先写 + 异步 S3 重试队列 + 排空对账（dev-go-storage；dep ADR-0040；M6 FR-50 文面维持）

**用户故事**：作为运维，S3 检修/抖动窗口内 BinFlow 不断服——上传 200（本地落盘 + 入队）、下载 200（存量 fallback）；S3 恢复后自动补齐对账。M6 PRD FR-50「S3 写失败不阻塞请求」的承诺从文面落成实现。

行为规格：

- **107.1 前置 ADR-0040（architect）**：fail-open 语义（停机窗 PUT/GET 形态——T-327 D-A 证据链：现状 GET 存量 500〔fallback 仅 ErrBlobNotFound〕+ PUT 500 且 disk 零落盘〔BeginSession S3 臂失败连带 abort disk 臂〕两处修复目标）；重试队列形态（持久化载体/指数退避/上限与死信处置/凭据纪律）；排空与对账断言（全量或抽样策略）；与 M6 三模式状态机（bypass/dual-write/completed）交互及迁移完成后切纯 S3 的边界。
- **107.2 停机窗行为**：PUT → **disk 优先落盘 + 入队**（200）；GET 存量 → S3 失败时 disk fallback 探测（不再 500）。
- **107.3 异步重试队列**：持久化（重启幸存）+ 退避重试 + 恢复后排空 + 排空完成对账（disk↔S3 sha256）。
- **107.4 可观测**：队列深度/排空进度指标 + `storage.replay.*` 审计 + 启动日志一行（队列水位）。
- **107.5 M6 PRD FR-50 文面零修改**（裁决③原文——实现债收口，非范围变更）。

验收标准（AC）：

- **AC1（停机窗）**：MinIO stop → PUT 3 制品全 200（disk 落盘 + 队列深度 +3）→ GET 新旧制品全 200 → MinIO start → 排空 → mc 侧逐对象 sha256 对账零缺。
- **AC2（持久化）**：停机窗内重启 BinFlow → 队列幸存 → 恢复后排空对账（对账报告留票）。**（v1.1 加注——T-356 观察⑨：as-built boot 探针为 fail-closed，S3 全停窗内整进程重启会因 bucket 探针拒启〔T-338 按「重启排在恢复后」验证、ADR-0040 未修订 boot 探针〕；本 AC 的「停机窗内重启」指队列盘上幸存语义——通过姿势 = 重启时探针可过或排在 S3 恢复后，水位行与排空在恢复后兑现〔T-356 §1 D-3 实证〕。加注不改行为、ADR-0040 零修改。）**
- **AC3（迁移语义回归）**：M6 H12~H15 迁移序列复跑绿（fail-open 不破坏迁移状态机；completed 边界断言）。
- **AC4（可观测）**：queue depth gauge + 排空审计断言。
- **AC5（回归）**：binstore.yaml 三链 roundtrip（M11 L08/L09 口径）零回归。

### 4.6 空载 RSS 瘦身（收口裁定⑤）

#### FR-108 懒加载 embed 瘦身本体（dev-go-core；P0——资源门红→绿）

**用户故事**：作为运维，空载内存回到 PRODUCT.md ≤100MB 基线——M11 收口登记的 138.7MB 债清偿（T-326 裁定「正主 = make footprint EXPECT 门」，该门自 M11 起红）。

行为规格：

- **108.1 瘦身路径**：懒加载 embed（收口裁定点名路径——重资源〔嵌入门面/大查找表/模板族〕延迟到首次使用初始化）；**归因清单先行**（vmmap/RSS 顶源定位，前后对照入票报告）。
- **108.2 双门（T-326 裁定口径维持）**：`make footprint` EXPECT 门 **红→绿**（正主）；`make check-size` 六平台聚合 ≤100MiB 维持（94.13MB 基线不回归）。
- **108.3 冷启动 <2s 维持**（懒加载不得以启动时延换内存——回归断言）；首访功能零损（触及才加载的路径全绿）。

验收标准（AC）：

- **AC1（footprint 转绿）**：`make footprint` EXIT 0（≤100MB）+ 归因清单（顶源前后对照）入票报告。
- **AC2（check-size 维持）**：六平台聚合 ≤100MiB。
- **AC3（冷启动）**：三连启动 P95 <2s。
- **AC4（功能零损）**：懒加载触及路径的全量 e2e 抽样绿。

### 4.7 HelmOCI 分发（Q2 承接）

#### FR-109 HelmOCI（HL-3 既定方案）+ oci:// 透传 + chartsBaseUrl（dev-registry-adapter；P1 + P2）

**用户故事**：作为平台工程师，`helm push oci://$BASE/<repo>/...` 与经典仓同体验；虚仓聚合边界清晰（Helm 与 HelmOCI 不混仓）。

行为规格：

- **109.1 HelmOCI 主面**：`package_type=helmoci` 新 handler Register 复用 docker adapter 端点（ForRepoType 机制现成）+ helm config/layer/prov 三 media type 入 manifest 映射；虚仓建仓校验「Helm 与 HelmOCI 不混仓」。
- **109.2 oci:// 透传深化（T-313 D-5 随票）**：虚仓/代理面对 chart 依赖引用 `oci://` 形态的透传规则照规格票（helm.md 增量锚点随票）。
- **109.3 chartsBaseUrl 分体基址（T-313 D-2，P2）**：remote 仓 charts_base_url 独立于仓基址的 URL 改写支持。
- **109.4 `_external` 落盘缓存（T-313 D-3）**：**architect 评估条件票**（Q6——涉及 internal/remote + repo config 扩展；评估结论定 M12 内落地或滚 M13）。

验收标准（AC）：

- **AC1（HelmOCI 全链）**：`helm push chart-0.1.0.tgz oci://$BASE/helmoci-repo` → `helm pull oci://$BASE/helmoci-repo/chart --version 0.1.0` → `helm install` 全绿；manifest 三 media type 断言；`.prov` 经 OCI 面腿（`--verify`）。
- **AC2（混仓校验）**：虚仓同时含 helm + helmoci 成员 → 建仓/改仓 400（文案照规格）。
- **AC3（docker 零回归）**：/v2 面全量回归绿（ForRepoType 复用不改 docker 面）。
- **AC4（门控转正）**：helmoci 槽（M11 占位）三缝断言（community 403 → pro 200 → 降级 pull 200/push 403）。
- **AC5（P2/条件腿）**：chartsBaseUrl 改写断言；D-3 评估结论 BOARD 留痕（未触发非 DoD 缺口）。

### 4.8 包型收尾小票包（T-329 D-F + 遗留登记）

#### FR-110 conan（D-F + forceConanAuthentication + 活体互证条件腿）+ cargo（R-3/R-4 + DELETE 收敛）（P1/P2）

**用户故事**：作为 conan 用户，v1 管理面对 conan-2 上传形态（`_/_` 坐标）的删除操作得到正确的 200；作为仓管理员，`forceConanAuthentication` 开关真实生效。

行为规格：

- **110.1 D-F 状态码修正（T-329 §7 登记）**：v1 `packages/delete` 对 `_/_` 坐标（conan 2.x 无 user/channel 上传形态）删树成功回 **200**（现 404「Path not found」——dir 解析与删除结果码分离，v1.go servePackagesDeleteIDs）；conan.md 规格行随票回写；conan 1.x 流量不受影响（回归断言）。
- **110.2 forceConanAuthentication（T-308 遗留）**：仓配置字段落地——接受/回显/生效（匿名面收紧为强制认证；默认 false 行为已备）。
- **110.3 conan Artifactory 真实上游活体互证（T-312 遗留）**：**条件腿**（Q5——dep: 用户环境/Artifactory 实例可得性）；不可得维持 mock + 自指上游两腿留痕（非 DoD 缺口）。
- **110.4 cargo R-3/R-4（T-316 登记；v1.1 勘误——T-355A §2.3 修正案 / T-356 D-356-3）**：remote search 上游死时**双姿态照 cargo.md §8.1 R-3 登记**——默认（hardFail 关）→ **404 unfound** 信封（assumed offline 摘要；FR-20 全仓统一姿态优先）/ hardFail 开 → **409 + errors 信封**（Artifactory cargo 面 409 在 remote search 上游异常臂〔T-304 §3 表 Exception 臂 / CG-2 类 8〕，publish 面无 409——T-355A §2.1 查证）；`.cargo/**` DELETE 收敛（低危随票）。

验收标准（AC）：

- **AC1（D-F）**：conan 2.x 上传 `_/_` 形态包 → v1 packages/delete → **200** + 树删（curl 断言；404 复现对照脚本入票）。
- **AC2（forceConanAuthentication）**：PUT 开启 → 匿名面 401/引导登录形态照规格；GET 回显；关闭往返。
- **AC3（cargo 双姿态；v1.1）**：死上游 search → 默认 404 unfound / hardFail 409 + errors 信封（curl 双臂断言；实现与测试 T-355A §2.2 在案，T-356 L24 实测绿）。
- **AC4（条件腿）**：Q5 结论执行或留痕。

### 4.9 MUI 批次三（T-300 候选清单兑现）

#### FR-111 五页面 + 共享组件六件套 + combobox 统一化（web/；P1；交互逻辑零变化）

**用户故事**：作为 Artifactory 迁移用户，控制台交互不受迁移影响（M8 规范既定）；作为维护者，组件层完成存量收敛——批三后 web/ 无旧组件栈残留页面。

行为规格：

- **111.1 范围（T-300 §批次三候选清单）**：RepoDetailPage / Dashboard / Profile / Placeholder / NotFound 五页面 + 共享组件六件套（ConfirmDialog / CopyButton / DeployDialog / EmptyState / ErrorCard / SetMeUpDialog）+ 最近词下拉 combobox 统一化 + `rowBtnSx` 归一（T-300 §3）+ 批一边界残余（T-299 登记项：session 菜单/侧栏/badge）。
- **111.2 交互零变化条款（M11 94.3 同构）**：仅组件层换 MUI；console-ux 册锚**零改动**；服务端契约 diff=0；发现 Artifactory 交互出入上 BOARD 先改册再迁移。
- **111.3 四闸门**：console-ux 锚零改动 + anchor-audit ledger PASS + 全量 Playwright 绿 + assert-tokens 零硬编码；axe 双主题 serious=0；SPA gzip 相对 T-291 基线累计增量 ≤25% 维持（NFR-P56）。
- **111.4 de-flake 联动**：N01 straddle 与负载 flake 家族按 T-327 §7 协议处置（CI 专用 runner or 静默窗——基建腿归 FR-113.6，MUI 票只消费结论）。

验收标准（AC）：

- **AC1（四闸门）**：批三四闸门全绿 + axe 双主题 0 + gzip 累计预算维持。
- **AC2（契约零变化）**：服务端契约 git diff=0 + M8 零学习成本剧本抽样复跑绿。
- **AC3（收敛完成）**：批三完成后 web/ 旧组件栈残留页清单清零（共享层收敛清单留痕）。

### 4.10 架构/规格回写批（转交项集中消化）

#### FR-112 architecture §15.4/§23 + cargo.md §8 + conan.md 升置信 + D-G/D-H 校验（architect + reverse-engineer；docs 票，P1）

**用户故事**：作为团队，M11 转交的文档债集中清偿——架构册与逆向规格与 as-built 一致，后续拆票不再读到旧 wire。

行为规格：

- **112.1 architecture.md §15.4/§23（T-332 转交——architect）**：MPU REST 新 wire（ADR-0039：六端点 POST+QueryParam / complete?sha1=202 异步任务模型 / GET /config 能力探测）回写；§15.4.1 remote 字段落 canonical JSON as-built（T-317 差异 1——**在 FR-113.1 落地后以终态回写**，两票时序协调）。
- **112.2 cargo.md §8 virtual 行 as-built 回刷（T-318 遗留——reverse-engineer）**：首见去重/写路由/yank 双持有者 as-built 行。
- **112.3 conan.md D1/D5/D7/D8 升置信（T-312 差异——reverse-engineer）**：复核取证或标定置信（D-F 修正后的规格行联动随 FR-110.1 回写）。
- **112.4 M11 收口文档回刷落位校验**：D-G/D-H（docs/user/integrations/cargo.md 三处 / remote-virtual.md L56 / auth-config.md 边界表 / api-reference SAML 三端点）在档断言 + `make docs` SUCCESS 零断链。
- **112.5 前置规格票计入**：nuget.md（FR-103）与 repo-operations.md（FR-105）属本批交付物，不重复立项。

验收标准（AC）：

- **AC1（三处回写）**：architecture §15.4/§23 + cargo.md §8 + conan.md 升置信 diff 落盘 + 票报告留痕。
- **AC2（make docs）**：SUCCESS 零断链。
- **AC3（D-G/D-H）**：四处旧措辞 grep 零残留。

### 4.11 配置与治理域遗留小票打包

#### FR-113 六项遗留 + 待裁登记（repo config / auth / storage / 测试基建；P1/P2）

**用户故事**：作为管理员，M11 登记的配置面遗留一次清完——回显键拼写、表单覆盖、token 权限域、认证尾巴、启动诊断各就各位。

行为规格：

- **113.1 T-290-2 兑现（M11 PRD §5.6.1 登记——改回未承载）**：canonical/回显键统一 `socketTimeoutMillis`（`socketTimeoutMs` 降输入别名——internal/repo/config.go 别名对已备）。
- **113.2 byHash 值域枚举校验归 repo.Service（T-327R 登记）+ web 仓表单 deb/rpm 策略键跟进（T-327R——REST 已通〔T-327R/D-E〕、表单缺）**。
- **113.3 checksum-deploy token 窄域化（T-332 登记）**：MPU complete 后签发 token 的权限域收窄到目标会话（防横向使用）——安全向增强，Artifactory 无 wire 对照（C 层级）。
- **113.4 auth 尾巴（T-305 遗留）**：audit 词表两词补录 + `userDnPattern` 消费缺位（按 auth-integration.md v2 规格：DN 直写模式生效或明确拒绝）。
- **113.5 env-only 不完整链键组拒启（T-325 登记，dev-go-storage；v1.1 对齐 AC5 终验口径——T-349 设计化，D-356-4）**：S3 凭据 env 组不完整 → 启动期即拒、错误指名缺键——**生效条件 = binstore.yaml 缺席或自身可解析**；binstore.yaml 在场时其链语义优先（文件在且 schema 错 → binstore 解析错先出）。
- **113.6 测试基建**：e2e 负载 flake 族 **CI 专用 runner**（或静默窗协议——T-327 §7 协议 + T-329 观察④重申；形态 K46 定案）。
- **113.7 待裁登记（不阻塞）**：SAML POST key FE 入口（T-307R——regenerate 已覆盖主径，开口子与否待裁）；crates.io 直连双主机 R-2 维持（文档说明）；keypair T-319 D-1~D-8 / SAML T-331 D-1~D-5 维持在案（用户可推翻）。

验收标准（AC）：

- **AC1（socketTimeoutMillis）**：PUT `socketTimeoutMillis=N` → GET 回显同拼写；PUT 旧拼写 `socketTimeoutMs` → 接受且回显新拼写（别名）；文档同步。
- **AC2（byHash + 表单）**：PUT `byHash=<非法值>` → 400 枚举错误（值域照规格）；Playwright：deb/rpm 仓编辑器设策略键（byHash/calculateYumMetadata 等）→ 保存 → 重开回显。
- **AC3（token 窄域化）**：MPU 会话 token 用于其他路径 → 403；原会话续传维持 200。
- **AC4（auth 尾巴）**：audit picker 两词可见；userDnPattern 行为按规格断言（DN 直写登录腿）。
- **AC5（拒启序）**：S3 env 组缺 ACCESS_KEY 且无 binstore.yaml → 启动即拒 + 错误指名键；binstore.yaml 存在时其链语义优先（env 组缺键不先于 binstore 拒启——终验文面修正对齐 T-349 设计，D-356-4）。
- **AC6（CI runner/de-flake）**：de-flake 协议落地（runner 就位或静默窗脚本）+ 负载 flake 家族三连零复发或隔离归因留痕。

---

## 5. 兼容性矩阵（M12——NuGet 端点对齐 + 生命周期域端点 + 行为债留痕）

### 5.1 层级定义（沿用 M2~M11 分级口径）

| 层级 | 定义 |
|---|
| **A 兼容** | 端点路径/方法/语义对齐 Artifactory 或公开规范（高频子集承诺范围）；基座前缀差异（`/binflow` vs `/artifactory`）沿 E-26 口径；加宽回显 additive |
| **C 自有（/api/v1 或内部语义）** | 无 Artifactory 对应或 BinFlow 自有设计；行为模式对齐但载体自定 |
| **D 有意不兼容 / 不做** | 显式裁决不做（clean-room / 依赖不可得实证 / 安全向），矩阵留痕防再议 |
| **待裁** | 存在与 Artifactory 的可观测差异，须用户终裁——终裁后归 A/D 并回写 |

### 5.2 档位 × addon 解锁矩阵（M12 增量 2 行，其余沿 M11 §5.2〔15 槽〕不变）

| addon 槽位（M12 增量行） | kind | community | pro | enterprise | M12 状态 |
|---|---|---|---|---|---|
| trashcan | feature-gov（暂行） | locked | unlocked | unlocked | M12 交付（FR-106；档位与 kind 随 Q3 终裁） |
| helmoci（M11 占位行更新） | package-eco | locked | unlocked | unlocked | M12 交付（FR-109——Q2 承接转正） |

- 三态叠加规则、`addons.disabled` 熔断、license 覆盖表语义全部沿 M10 §5.2 不变；trashcan 槽自动获得门控三缝行为（FR-85 机制复用）。
- nuget 槽（M10 试点，pro）承载 v2/v3 深化，不新增槽。
- **copy/move/归档族暂行不门控**（Q4——规格票复核 Artifactory license 门后终裁；folderDownload 维持 Artifactory 默认关）。

### 5.3 契约矩阵（15 条，LC-31~LC-45 接续 M11 编号）

| # | 端点/契约面 | Artifactory 对应 / 公开规范 | 层级 | 优先级 | 置信度 | 验收 |
|---|---|---|---|---|---|---|
| LC-31 | NuGet v2 OData 路由全集（Search()/FindPackagesById()/Packages()/GetUpdates()/$batch/Download/DELETE/PUT×2 + OData 参数面） | Artifactory v2 路由全集（T-304 §1.1-L2 出处：`NuGetSubResource`）+ OData 官方规范 | A | P0 | 复核后（nuget.md） | L02 |
| LC-32 | NuGet publish 重复臂：409 CONFLICT + canDelete 覆盖 | `NuGetLocalRepoHandler`（T-304 §7 新发现；与 cargo D-3 同构） | A | P0 | 高 | L03 |
| LC-33 | NuGet v3 service index 动态解析（资源类型优先级阶梯） | `FeedUtils` 阶梯常量表 + 偏好序列（T-304 §1.1-L4） | A | P0 | 复核后 | L06 |
| LC-34 | NuGet v3 remote/virtual search 上游代理合并（SearchQueryService @id 提取 + local∪remote） | `downloadSearchResult` / `collectAllSearchResultDataItems`（T-304 §1.1-L3'/L7；local 半边维持存储事实） | A | P0 | 高 | L07/L08 |
| LC-35 | `POST /api/copy\|move/{srcRepo}/{srcPath}`（树级 + dryRun + flat 折叠进主参数族〔`/api/flat/*` → 404〕+ 属性/校验和/索引随行） | Artifactory copy/move REST（主矩阵 §C；repo-operations.md） | A | P0 | 复核后 | L11/L12 |
| LC-36 | 归档内路径 `archive!/`（按需解包成员读取 + strictArchiveDotSlash） | inv-3 §3.1（主矩阵归档族行） | A | P1 | 复核后 | L14 |
| LC-37 | 目录/整仓 zip 下载（folderDownloadConfig 默认关 + 四限参 + `GET /api/archive/download` + entry 抽取 + 计流量） | inv-1 F ArchiveResource + inv-2 §1.A/inv-3 §3.3 | A | P1 | 复核后 | L14 |
| LC-38 | exploded archive 解包上传（X-Explode-Archive + explodedArchiveExtensions 白名单） | inv-2 §5 system.properties——**M10「显式 400 拒绝」反转** | A | P1 | 高 | L14 |
| LC-39 | Trash can REST 族（auto-trashcan 内置仓 + trash 四元组打标 + 保留期 14 天 + empty/restore/clean） | inv-1 B/G + inv-2 §1.A + config.xml trashcanConfig（主矩阵缺口 3 行） | A | P1 | 高 | L15~L17 |
| LC-40 | dual-write S3 停机 fail-open（本地优先写 + 异步重试队列 + 排空对账） | **无 Artifactory 对照**（dual-write 为 BinFlow 自有迁移引擎——T-289-E 结构性不可对照；M6 FR-50 文面行为模式） | C | P1 | 高（ADR-0040） | L18/L19 |
| LC-41 | HelmOCI 分发（package_type=helmoci 复用 docker 面 + 三 media type + oci:// 透传 + 混仓校验） | Artifactory HelmOCI（HL-3；helm.md） | A | P1 | 高 | L21/L22 |
| LC-42 | conan v1 packages/delete `_/_` 坐标 200（D-F）+ forceConanAuthentication 字段 | conan.md §3.2 规格 200（缺陷修正）；artifactory.xsd 真字段 | A | P1/P2 | 高 | L23 |
| LC-43 | cargo remote 死上游 search 双姿态（默认 404 unfound / hardFail 409+errors 信封——R-3 登记；v1.1）+ `.cargo/**` DELETE 收敛 | `CargoRemoteRepoHandler.search` Exception 臂（T-304 §3 表）+ cargo.md §8.1 R-3/R-4 | A | P2 | 高 | L24 |
| LC-44 | `socketTimeoutMillis` canonical 回显（T-290-2 兑现）+ byHash 值域枚举 + web 策略键表单 | `HttpRepoDescriptor` 字段拼写（xsd 逐字）；debian.md/rpm.md 值域 | A | P2 | 高 | L28/L29 |
| LC-45 | debian Packages.bz2 压缩档 | **D（有意不兼容）**——dsnet 依赖不可得实证（T-314）；plain+gz+xz/lzma 在场，apt 功能面零损；用户可推翻（Q7） | D | — | 高 | L34（负向/留痕） |

> 计数：**15 条 = A 13（LC-31~39/41~44）+ C 1（LC-40）+ D 1（LC-45）+ 待裁 0**。既有契约面（五基础包型、go/nuget/cargo、conan/deb/rpm/helm、配置域、keypair/cleanup/MPU 新 wire）M12 对 M11 as-built **零行为变化**（§5.4），三处断言反转均经 PRD 回写。checksum-deploy token 窄域化（FR-113.3）为自有安全增强，不入矩阵（无对照面）。

### 5.4 回归基线（M12 断言反转三处——均经 PRD 回写；其余零回归）

| 既有断言 | M12 期望 |
|---|---|
| M1~M11 全部 P0 序列（双形态：无 license 默认 + pro） | 零回归（制品操作族/Trash can 只增不改既有协议面；删除语义变化面 100% 归属 FR-106 豁免票） |
| M10「v2 其余端点 404」（NuGet v2 收窄） | **反转**：FR-103 落地后路由全集可达（M10 L 序列相应负向断言翻转，PRD 回写） |
| M10「virtual search = local + 已落地缓存」 | **反转**：FR-104 落地后 remote 成员上游实时搜索合并 |
| M10「X-Explode-Archive 显式 400 拒绝」（矩阵 §Generic 行登记） | **反转**：白名单扩展名内接受 + 解包（FR-105.3；闭集外 400 维持） |
| `make footprint` RED（D-8R——M11 收口裁定留痕） | **转绿**：FR-108 落地（≤100MB） |
| deb/rpm 策略键 REST 传输（T-327R/D-E 补丁）+ helm enforce 两键（D-E fix-forward） | 维持（FR-113.2 只加表单面与枚举校验，REST 零变化） |
| binstore.yaml 三链 / M6 迁移 H 序列 | 零回归（FR-107 只改停机窗行为，迁移状态机不动） |
| console-ux 册锚 / anchor ledger | FE 迁移零改动（FR-111 熔断线延续） |
| M11 全部 L 序列承证面（五包型×三仓型真客户端矩阵） | 零回归（FR-110 只改登记缺陷位） |

### 5.5 M12 核心验收命令（L 序列骨架，QA 直接引用）

```bash
BASE=http://127.0.0.1:8080; ADMIN=admin:password
# ========== FR-103 NuGet v2 ==========
# L01 规格票走查：nuget.md 双段（as-built + 增量出处）+ tl 就绪确认；v2 路由全集/阶梯常量表出处逐条
# L02 v2 sweep：Search()（命中/未命中）/Search()/$count/FindPackagesById()±$count/Packages(id)(/Id)/
#    Packages()/$count/GetUpdates()±$count/$batch/Download/{id}/{ver}/{file}.nupkg（sha256 对账）/DELETE/PUT×2
#    ——M10「v2 其余 404」断言反转（归属 FR-103）
# L03 重复臂：同 id+version 二次 push → 409 CONFLICT 逐字；d 权限主体重传 → 覆盖（sha256 新值）
# L04 真实客户端与门控：nuget.exe 活体腿（可得则 list/install 经 v2 源；否则 curl 等价+留痕）；
#    nuget 槽三缝复跑（community 403→pro 200→卸载 pull 200/push 403，v2 面含内）
# L05 v3 回归：M10 T-287 序列（push/restore/remote pull-through/virtual）零回归

# ========== FR-104 NuGet v3 search/动态解析 ==========
# L06 动态解析：自指/mock service index（自定义 @id 路径）→ flatcontainer/registrations/search 三面 URL
#    由上游 index 解析（改 @id → 零配置跟随；前缀常量断言退役）
# L07 remote 代理：上游发布新版本（不预下载）→ SearchQueryService 经 BinFlow remote 立即可搜；
#    数据面二次命中缓存断言维持
# L08 virtual 合并：虚仓 local+remote → search 两域并见；remote 新版本可搜（M10 断言反转，归属 FR-104）
# L09 降级：停上游 → search 回缓存事实/降级形态零 5xx；恢复自动回代理

# ========== FR-105 制品操作族 ==========
# L10 规格走查：repo-operations.md（copy/move/archive!/zip/explode 出处逐条）+ tl 就绪确认
# L11 copy/move：curl -X POST -u $ADMIN "$BASE/api/copy/<src>/<path>" --data-urlencode "to=/<dst>/<path>"
#    → 200；源/目标 sha256 对账；?properties 随行；move 源消失；万节点树 copy 零 5xx
# L12 dryRun/flat：dryRun 报告 + 源/目标零变化（GET 对照）；/api/flat/* → 404（折叠口径）+ failFast 停走臂
# L13 索引联动：deb 仓 copy .deb → 目标 Packages/by-hash 重算；conan move recipe → 目标 index.json 一致
# L14 归档族：GET <file>.zip!/inner/path.txt 字节一致（strictArchiveDotSlash 臂）；开 folderDownload →
#    GET /api/archive/download 解包 sha256 对账（默认关/超限拒绝臂）；PUT X-Explode-Archive → 展开
#    （M10 400 断言反转，归属 FR-105；非白名单扩展 400 维持）

# ========== FR-106 Trash can ==========
# L15 删除入站：DELETE → 原路径 404 + 回收站可见 + trash.time/deletedBy/originalRepository/originalPath 打标
# L16 恢复：restore → 原路径 200 + sha256 一致 + 属性复原 + 协议仓索引重算
# L17 保留期与治理：短保留期 → 过期自动清理+审计；POST /api/trash/empty + 权限门；Q3 终裁门控形态

# ========== FR-107 dual-write fail-open ==========
# L18 停机窗：MinIO stop → PUT×3 全 200（disk 落盘+队列+3）→ GET 新旧全 200 → MinIO start → 排空 →
#    mc 逐对象 sha256 对账零缺（T-327 D-A 两处 500 消除）
# L19 持久化与回归：停机窗内重启 → 队列幸存 → 排空对账；M6 H12~H15 迁移序列复跑；binstore 三链 roundtrip

# ========== FR-108 RSS 瘦身 ==========
# L20 双门：make footprint EXIT 0（≤100MB，红→绿）+ 顶源归因清单；make check-size ≤100MiB 维持；
#    冷启动三连 P95 <2s；懒加载触及路径 e2e 抽样绿

# ========== FR-109 HelmOCI ==========
# L21 全链：helm push oci://$BASE/helmoci-repo → helm pull → helm install；manifest 三 media type；--verify 腿
# L22 边界：混仓 400；/v2 面全量回归零变化；helmoci 槽三缝转正；chartsBaseUrl P2 腿；D-3 评估留痕

# ========== FR-110 包型收尾 ==========
# L23 conan：_/_ 坐标 v1 packages/delete → 200+树删（404 复现对照）；forceConanAuthentication 开→匿名 401/回显；
#    conan 1.x 回归不受影响
# L24 cargo：死上游 search 双姿态（默认 404 unfound / hardFail 409+errors 信封）；.cargo/** DELETE 收敛断言

# ========== FR-111 MUI 批三 ==========
# L25 四闸门：锚零改动/ledger PASS/全量 playwright/assert-tokens + axe 双主题 0 + gzip 累计 ≤25%
# L26 收敛：服务端契约 git diff=0；旧组件栈残留页清单清零；M8 剧本抽样复跑

# ========== FR-112 回写批 ==========
# L27 三处回写 diff 落盘（architecture §15.4/§23、cargo.md §8、conan.md 升置信）+ make docs SUCCESS +
#    D-G/D-H 四处旧措辞 grep 零残留

# ========== FR-113 遗留小票 ==========
# L28 socketTimeoutMillis：PUT 新拼写回显一致；PUT 旧拼写 → 接受+回显新拼写；文档同步
# L29 byHash/表单：byHash 非法值 400 枚举；Playwright deb/rpm 策略键表单往返回显
# L30 token 窄域化：MPU 会话 token 他路径 403；原会话续传 200
# L31 auth/拒启：audit 两词可见；userDnPattern 规格行为；S3 env 缺键启动即拒（binstore.yaml 在场时其链语义优先）
# L32 CI runner/de-flake：协议落地 + 负载 flake 家族三连零复发或隔离归因留痕

# ========== 回归与收口 ==========
# L33 全量回归：M1~M11 全 P0 双形态复跑 + 契约变更面（git diff m11-done..HEAD -- internal/ cmd/）100% 归属
#    M12 豁免票 + 三处断言反转 PRD 回写核实
# L34 NFR 与负向留痕：100 并发 NuGet/操作族零 5xx；footprint/check-size/冷启动/SPA gzip；
#    bz2 负向留痕（LC-45）+ 条件腿（symbol server Q2/活体互证 Q5/D-3 Q6）留痕
# L35 文档：tech-writer 五类增量（NuGet v2/操作族/回收站/HelmOCI/FAQ）客户端命令实测可复跑
```

### 5.6 待校准项（nuget.md / repo-operations.md / ADR-0040 落地后回写）

| # | 项 | v1.0 暂行值 | 校准来源 |
|---|---|---|---|
| K38 | NuGet v2 OData 查询参数支持面（$filter/$orderby/$top/$skip/$inlinecount 子集）与 $batch 语义 | 全集照规格票取证 | nuget.md |
| K39 | service index 资源类型阶梯常量表（版本偏好序列逐字——RegistrationsBaseUrl 族/SearchQueryService/FlatContainer） | T-304 §1.1-L4 描述暂行 | nuget.md（FeedUtils 出处） |
| K40 | copy/move wire 细节（to 参数/树级冲突语义/dryRun 报告形态/flat+failFast） | 主矩阵 §C 行暂行 | repo-operations.md |
| K41 | archive!/strictArchiveDotSlash 语义 + folderDownload 四限参（1024MB/5000 文件/10 并发/匿名单独开关） | 主矩阵归档族行暂行 | repo-operations.md |
| K42 | Trash can REST 族形态（empty/restore〔to+transaction-size〕/clean；trash 属性集；保留期配置位） | 主矩阵缺口 3 行暂行 | mini 规格随票（T-330 模式） |
| K43 | fail-open 队列形态（持久化载体/退避/上限死信/排空对账策略/completed 边界） | 本 PRD §4.5 行为面暂行 | ADR-0040 |
| K44 | trashcan 槽位归属与 kind（community/pro/enterprise） | pro+ 解锁、kind=feature-gov | Q3 → 终裁（视需要 ADR-0041） |
| K45 | byHash 值域枚举集 + web 表单字段集（deb/rpm 策略键全列） | debian.md/rpm.md 值域 | FR-113 票内定案 |
| K46 | CI 专用 runner vs 静默窗协议（de-flake 家族处置形态） | runner 优先、静默窗兜底 | T-327 §7 协议 + FR-113.6 票内定案 |

---

## 6. 非功能需求（NFR）

### 6.1 与已有 ADR / 规范的冲突/补充标注

| ADR/规范 | 冲突/补充点 | 本 PRD 立场 | 所需动作 |
|---|---|---|---|
| PRODUCT.md「明确不做」 | HA/Xray 本体是否随对齐指令进 M12 | 不进（指令对齐行为不解锁本体——Q1；PM 建议 M13 专程，须 PRODUCT.md 修订） | 用户决策（Q1） |
| M6 PRD FR-50 文面 | 「S3 写失败不阻塞+重试队列」文面 vs D-A 实现 fail-closed | M12 补实现兑现（裁决③）；**文面零修改** | ADR-0040 |
| ADR-0039（MPU 新 wire）vs architecture.md §15.4/§23 旧述 | 架构册滞后 | FR-112.1 回写（architect） | 无（docs 票） |
| M10 PRD §4.3 X-Explode-Archive 400 拒绝 + v2 收窄 + virtual search 缓存 | 三处 as-built 断言 | M12 三处反转（FR-103/104/105），PRD 回写 | PM 回写 + QA 序列更新 |
| M11 PRD §5.6.1 T-290-2 登记 | 改回未承载 | FR-113.1 兑现 | 无 |
| ADR-0032（license 门控） | trashcan 新槽 | Q3 终裁（暂行 pro+） | 视需要 ADR-0041 |
| ADR-0001（clean-room） | 行为对齐与版权红线张力 | 红线保留：行为逐项对齐 ≠ 逐行翻译；公开规范覆盖处以规范为准 | §1.4 条款 4 延续 |

### 6.2 性能（M12 增量）

| NFR | 指标与验收方式 | 优先级 |
|---|---|---|
| NFR-P53 NuGet 面并发 | 100 并发 dotnet restore（经 v3 虚仓）+ v2 sweep 并发零 5xx；remote search 代理 P95 对齐既有 remote 口径 | P0 |
| NFR-P54 制品操作吞吐 | 万节点树 copy/move 零 5xx + 抽样 sha256 对账；dryRun 报告 P95 < 5s（万节点） | P0 |
| NFR-P55 资源门 | 空载 RSS ≤100MB（`make footprint` 门转绿——正主）；check-size 六平台聚合 ≤100MiB 维持；冷启动 <2s 三连 | P0 |
| NFR-P56 MUI 批三体积 | SPA gzip 相对 T-291 基线累计增量 ≤25% 维持（批一 +3.57%/批二 +3.12% 后仍有充足余量）；axe 双主题 serious=0 | P1 |
| NFR-P57 fail-open 停机窗 | S3 停机窗 PUT/GET 零 5xx；恢复后排空最终一致（对账报告）；队列深度有界（上限 + 死信告警） | P1 |

### 6.3 安全底线（M12 增量）

| NFR | 要求 | 验收 |
|---|---|---|
| NFR-S61 Trash can 数据面 | 回收站制品不可匿名读（门控 + 权限门）；restore/empty 审计（actor/目标/规模）；empty 需管理权限 | L15~L17 |
| NFR-S62 制品操作族越权面 | copy/move 目标仓写权限门（无 w 权限 403）；dryRun 零副作用；目录 zip 匿名默认关（folderDownload 匿名单独开关维持 Artifactory 默认）；archive!/ 读权限随制品 | L11~L14 |
| NFR-S63 fail-open 队列凭据 | S3 凭据不落队列明文（env 引用 + AES-GCM 链维持）；队列重放不可跨会话伪造（条目绑定 blob 坐标）；checksum-deploy token 窄域化（FR-113.3）防横向使用 | L18/L19（token 腿 L30） |
| NFR-S64 上游代理 SSRF | NuGet search/service index 代理复用 M3 Guard（DNS rebinding pinning 五参数）+ ADR-0025 决策 4 私网开关——零新 SSRF 面 | L06/L09 |

### 6.4 可观测性（M12 增量）

- 新审计事件：`repo.copy` / `repo.move`（actor/源/目标/dryRun 标志/规模）、`trash.restore` / `trash.empty`（actor/规模）、`storage.replay.drain`（排空完成对账摘要）。
- /metrics：`binflow_storage_replay_queue_depth`（gauge）+ 排空计数器；NuGet 上游 search 代理计数族（沿用 remote family 口径：命中代理/降级/缓存）；`binflow_addon_gate_requests_total{addon,decision}` trashcan 槽标签自动生效。
- 启动日志：fail-open 队列水位一行（重启幸存条目数）；瘦身归因清单入票报告（非运行时面）。
- 既有指标族与口径冻结维持。

---

## 7. 开放问题（Q1~Q7，均带暂行；需用户/conductor 决策，PM 不代拍）

| # | 问题 | 影响面 | 暂行口径（v1.0） |
|---|---|---|---|
| Q1 | **HA 本体排期**：Q1 终裁「M12+ 单列」——本程 or M13 专程 | 范围体量（HA 是架构级大件）；PRODUCT.md 修订 | **M12 不进；PM 建议 M13 专程单列**（须先修订 PRODUCT.md 解禁 Non-goal + ADR 群〔心跳/推举/传播〕；M12 已满载，插队须等量置换）。Xray 集成面本体同族处置 |
| Q2 | **NuGet symbol server 余量条件票触发**（T-293 终裁：立项随票补 as-built 规格） | NuGet bundle 域增量面（.pdb/GUID 路径） | 全部 P0/P1 收官且余量足 → 触发（mini 规格随票）；未触发 M13+，BOARD 留痕非 DoD 缺口 |
| Q3 | **trashcan 槽位档位归属**（community/pro/enterprise + kind） | 门控矩阵第 16 槽；Artifactory 分级对照 | 暂行 pro+ 解锁、kind=feature-gov；随 mini 规格取证 Artifactory 侧 license 门后上 BOARD 终裁（视需要 ADR-0041） |
| Q4 | **制品操作族 license 门复核**：copy/move/归档族在 Artifactory 侧是否有 license 门（folderDownload 计流量面尤须查证） | FR-105 是否入槽 | 暂行基座不门控；repo-operations.md 取证发现门控则上 BOARD（进则补槽 + 门控三缝） |
| Q5 | **conan Artifactory 真实上游活体互证**（T-312 遗留） | FR-110.3 条件腿 | dep: 用户环境/Artifactory 实例可得——可得则活体互证票，不可得维持 mock+自指两腿留痕（非 DoD 缺口） |
| Q6 | **helm `_external` 落盘缓存**（T-313 D-3——architect 评估单列候选） | FR-109 域增量（internal/remote + repo config 扩展） | architect 评估结论定 M12 内落地（条件票）或滚 M13；评估留痕 BOARD |
| Q7 | **deb Packages.bz2 维持**（dsnet 依赖不可得实证 T-314） | debian 索引压缩集完备度 | 维持不做（LC-45 D 留痕；gz/xz 在场 apt 零损）；用户可推翻（提供实现路径则翻转） |

---

## 8. M12 验收剧本（QA 总纲）

1. **回归基线（硬门槛先行）**：M1~M11 全部 P0 序列双形态（无 license + pro）复跑全绿；契约变更面（`git diff m11-done..HEAD -- internal/ cmd/`）100% 归属 M12 豁免票。
2. **NuGet 域**：L01（规格票走查）→ L02（v2 路由全集 sweep）→ L03（重复臂）→ L04（真实客户端 + 门控）→ L05（v3 回归）→ L06~L09（动态解析 → remote 代理 → virtual 合并 → 降级）。
3. **制品操作域**：L10（规格走查）→ L11~L13（copy/move → dryRun/flat → 索引联动）→ L14（归档族三面）。
4. **Trash can**：L15~L17（入站打标 → 恢复 → 保留期/empty/门控）。
5. **存储与资源**：L18/L19（fail-open 停机窗 + 持久化排空）→ L20（footprint 转绿双门 + 冷启动）。
6. **HelmOCI 与包型收尾**：L21/L22（OCI 全链 + 边界/门控转正）→ L23/L24（conan D-F + forceConanAuthentication / cargo 409）。
7. **前端与回写**：L25/L26（MUI 批三四闸门 + 收敛清零）→ L27（三处回写 + make docs + D-G/D-H grep）。
8. **遗留小票**：L28~L32（socketTimeoutMillis → byHash/表单 → token 窄域 → auth/拒启 → CI runner）。
9. **NFR 与收口**：L33（全量回归 + 归属审计 + 三处断言反转回写核实）→ L34（NFR + 负向/条件腿留痕）→ L35（文档实测复跑）。
10. **文档**：tech-writer 增量——NuGet v2 接入指南（老客户端 + OData 面）、制品操作族指南（copy/move/归档下载/解包上传）、回收站管理指南、HelmOCI 接入（与经典仓辨析）、api-reference 增量（新端点 + 三处反转）、FAQ 增补（回收站保留期/exploded 上传/fail-open 行为）。

---

## 9. M12 DoD

1. §4 全部 P0 AC（FR-103/104/105 copy-move 段/108）经 qa 验证全绿；P1（FR-105 归档族/106/107/109/110/111/112/113）全绿；条件票（NuGet symbol server Q2 / conan 活体互证 Q5 / D-3 Q6 / chartsBaseUrl P2 段）按余量条款——未触发不构成 DoD 缺口，须 BOARD 留痕；
2. §8 剧本全绿；§1.2 量化门槛表逐行达标（v2 覆盖 / search 时效 / 动态解析 / 操作族 / 回收站 / fail-open / 资源门转绿 / HelmOCI / 包型收尾 / MUI / 回写 / 遗留小票——全部有实测数字归档）；
3. 回归硬门槛：M1~M11 全部 P0 序列双形态复跑全绿；M11 as-built 零行为变化（豁免票归属外）；契约变更面 100% 归属 M12 豁免票；**三处断言反转**（nuget v2 404→全集 / virtual search 缓存→代理 / X-Explode 400→接受）+ footprint 红→绿 经 PRD 回写核实；
4. 前置产物齐备：nuget.md（双段 + 出处逐条）+ repo-operations.md + ADR-0040 Accepted（含 K40~K43 校准）；Q3/Q4 终裁归位（trashcan 槽 / 操作族门控）；
5. tech-writer 五类增量文档交付（§8-10）；新增能力的客户端命令全部实测可复跑；
6. NFR-P53~P57 达标归档；`make test`（race）/`make lint` 0 issues / gofmt 空维持；默认并发全量 e2e 绿（de-flake 协议下）；
7. §2.2 Non-goals 与主轴重排对账完成：滚入 M13+ 项在 ROADMAP 登记（「M12 未纳入项」于收口时建段）；19:05 出处义务在全部 M12 规格票可审计；
8. 主会话 git tag `m12-done`（对外发布任何制品先经用户确认；PR 化合并沿既定 gitflow 程序）。
