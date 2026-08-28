# M12-PRD 工作日志 — product-manager

- 日期：2026-08-28 ｜ 角色：product-manager ｜ 产物：`docs/prd/milestone-12.md`（PRD v1.0 草案，待 conductor 审）+ ROADMAP M12 立项段/主轴重排
- 输入通读：ROADMAP「M11 未纳入项」段全文、BOARD M11 票据节（含 2026-08-28 07:5x 三项裁决 + 收口裁定⑤留痕）、M11 PRD v1.2.2（体例基线）、票报告 T-304/T-300/T-327/T-329/T-332/T-318/T-312/T-316/T-313/T-325/T-319/T-331、M6/M10 PRD（FR-50 文面 / §4.3 T-293 终裁口径 / §5.2 档位矩阵）、主矩阵（artifactory-full-feature-matrix.md 缺口 3/5 及归档族/copy/move/Trash can/Webhook 行）、DECISIONS.md（ADR 水位 = 0039，M12 起 ADR-0040）

## 一、范围决策摘要

**必须纳入（用户已裁，四项全承载为 FR）**：
1. NuGet 对齐 bundle（裁决①）→ **FR-103 + FR-104**（P0 双条）：v2 路由全集（T-304 §1.1-L2 出处列逐字引用）+ publish 重复臂（409+canDelete 覆盖——T-304 §7 新发现，随 bundle 带入）+ v3 remote/virtual search 上游代理（L3-remote/L7）+ service index 动态解析（L4——FeedUtils 阶梯）。前置 nuget.md 规格票按 M10 §4.3 T-293 终裁「立项随票补 as-built 规格」口径设计为 **as-built + 增量出处双段**（不写纯 retro）。
2. D-A dual-write S3 停机 fail-open（裁决③）→ **FR-107**（P1）：本地优先写 + 异步重试队列 + 排空对账；M6 FR-50 文面零修改（实现债收口）；前置 ADR-0040；T-327 D-A 两处 500（GET 存量 fallback / PUT disk 连带 abort）即修复目标与 AC 原文。
3. D-F conan v1 delete 状态码 → **FR-110.1**（P2 小票，并入包型收尾包）。
4. D-8R 空载 RSS 瘦身 → **FR-108**（P0——`make footprint` 门红→绿；T-326 双门裁定口径维持：footprint 正主 + check-size 副门）。

**建议纳入的裁量（PM 定）**：
- **Trash can 进 M12**（FR-106，P1，T-330 转正）：与 Cleanup-Retention 策略引擎**分票分界**——M12 仅回收站语义（内置仓/打标/保留期/restore），策略引擎滚 M13+（PRD §2.2 显式防混淆行）。档位归属 Q3 待裁（暂行 trashcan 槽 pro+）。
- **主轴增量选题 = 制品操作族**（FR-105）：copy/move P0 + `archive!/`/zip/explode P1。选题理由：主矩阵缺口 5「日常运维高频」；与 Trash can 同生命周期域（删除=move to 回收站，FR-105.5 显式联动复用底座）；直接消费 M11 属性系统/复制硬化基建；含一处既有断言反转（M10 X-Explode 400 拒绝→白名单内接受）可顺带清债。前置 repo-operations.md mini 规格。
- **HelmOCI 进 M12**（FR-109，P1）：Q2 条件票未派滚入 + T-313 D-5 透传随票 + D-2 P2 + D-3 architect 评估条件票（Q6）。
- **MUI 批三进 M12**（FR-111，P1）：T-300 候选清单全量（五页面 + 六件套 + combobox 统一化 + rowBtnSx 归一）。
- **回写批集中消化**（FR-112，P1 docs 票）：architecture §15.4/§23（T-332 转交，注明与 FR-113.1 的时序协调——remote 字段 as-built 以 `socketTimeoutMillis` 终态回写）+ cargo.md §8（T-318）+ conan.md D1/D5/D7/D8 升置信（T-312）+ D-G/D-H 落位校验。
- **票级遗留按域聚类打包**（FR-113，P1/P2）：T-290-2 / byHash 枚举 + web 表单（T-327R）/ token 窄域化（T-332）/ auth 尾巴（T-305）/ 拒启序（T-325）/ CI 专用 runner（T-327 §7 + T-329 观察④）六项成 AC；维持项（keypair D-1~D-8、SAML D-1~D-5、R-2 双主机、mc tag 等）登记不入 FR。

**HA 本体处置**：Q1 终裁「M12+ 单列」——PM 暂行建议 **M13 专程单列**（PRD Q1 + ROADMAP 主轴行留痕），理由：PRODUCT.md Non-goal 未解禁（须用户修订）+ 架构级大件需 ADR 群 + M12 已满载。用户可推翻插队（等量置换条款写入 DoD#1）。Xray 集成面本体同族处置。

## 二、FR 清单概览（FR-103~FR-113，11 条；估 22~28 票含 QA/文档/部署/条件票）

| FR | 域 | 优先级 | 一句话 | 前置 |
|---|---|---|---|---|
| FR-103 | NuGet v2 | P0 | v2 路由全集 + OData 参数面 + 409/覆盖重复臂 | nuget.md |
| FR-104 | NuGet v3 | P0 | service index 阶梯动态解析 + remote/virtual search 上游代理合并 | nuget.md |
| FR-105 | 制品操作族 | P0/P1 | copy/move（dryRun/flat/属性与索引随行）+ `archive!/`/目录 zip/exploded | repo-operations.md |
| FR-106 | Trash can | P1 | auto-trashcan 内置仓 + 四元组打标 + 保留期 + restore/empty/clean | mini 规格随票；dep FR-105 |
| FR-107 | 存储行为债 | P1 | dual-write 停机 fail-open：本地优先写 + 异步队列 + 排空对账 | ADR-0040 |
| FR-108 | 资源债 | P0 | 懒加载 embed，footprint 门红→绿（138.7MB→≤100MB） | — |
| FR-109 | HelmOCI | P1 | helmoci 包型（docker 面复用）+ oci:// 透传 + chartsBaseUrl P2 | Q6 评估 |
| FR-110 | 包型收尾 | P1/P2 | conan D-F/forceConanAuthentication/活体互证条件腿 + cargo 409/DELETE 收敛 | Q5 条件 |
| FR-111 | 前端 | P1 | MUI 批三（五页面 + 六件套 + combobox）——交互零变化四闸门 | — |
| FR-112 | 文档回写 | P1 | architecture/cargo/conan 三处 + D-G/D-H 校验 | 与 113.1 时序协调 |
| FR-113 | 遗留小票 | P1/P2 | 六项配置/治理域遗留 + 待裁登记 | — |

契约矩阵 LC-31~LC-45（15 条 = A 13 / C 1〔LC-40 fail-open——自有迁移引擎无对照〕/ D 1〔LC-45 deb bz2——依赖不可得实证〕）；档位矩阵增量 2 行（trashcan 新槽 Q3 / helmoci 转正）；L01~L35；NFR-P53~P57 + NFR-S61~S64；K38~K46 待校准。**三处断言反转**入 DoD#3：nuget v2 404→路由全集、virtual search 缓存→上游代理、X-Explode 400→接受（+footprint 红→绿）。FR/LC/NFR/K 编号均接续 M11 水位（FR-102→FR-103；LC-30→LC-31；NFR-P52→P53、S60→S61；K37→K38）。

## 三、待裁项（PRD §7 Q1~Q7，均带暂行不阻塞）

1. **Q1 HA 排期**：暂行 M13 专程（须 PRODUCT.md 修订解禁）；Xray 本体同族。
2. **Q2 NuGet symbol server**：余量条件票（mini as-built 规格随票——T-293 终裁口径）。
3. **Q3 trashcan 槽档位**：暂行 pro+ / kind=feature-gov；随 mini 规格取证 Artifactory license 门后终裁（视需要 ADR-0041）。
4. **Q4 制品操作族 license 门复核**：暂行基座不门控；repo-operations.md 取证发现门控则上 BOARD。
5. **Q5 conan Artifactory 真实上游活体互证**：dep 用户环境可得性；不可得留痕非 DoD 缺口。
6. **Q6 helm `_external` 落盘缓存**：architect 评估定 M12 条件票或 M13。
7. **Q7 deb bz2 维持**：依赖不可得实证维持 D 留痕；用户可推翻。
另：FR-113.7 登记微待裁（SAML POST key FE 入口）；PRD §2.2 对 R-2 / keypair・SAML 差异族 / statisticsEnabled・sourceOrigin / 属性复制协议面等维持项留痕「用户可推翻」。

## 四、滚程处置（M13+ 去向全部留痕于 PRD §2.2 + ROADMAP 主轴重排行）

- **M13 专程候选**：HA 本体（+Xray 集成面，须 PRODUCT.md 修订）——PM 建议，Q1 终裁。
- **M13+ 常规滚程**：AQL + 13 老搜索（搜索基建专程）/ Cleanup-Retention 策略引擎（与 FR-106 分界行）/ Webhook 事件总线（36 事件）/ Build-info 域 / Go 深化（sumdb + external——PM 排期留痕：非淘汰，容量让位）/ license 公钥覆盖（安全面专 ADR）/ 制品 license 识别 + 冷存储分层 / Terraform・GitLFS（规格随 M13 规划）/ deb snapshot 族（缓议维持）/ statisticsEnabled・sourceOrigin（stats 面立项时）/ 属性复制协议面（归各适配器深化票）。
- **维持在案（用户可推翻）**：deb bz2（Q7）/ crates.io 双主机 R-2 / keypair T-319 D-1~D-8 / SAML T-331 D-1~D-5 / mc tag 版本锚定。
- **收编清偿**：T-290-2（M11 PRD §5.6.1 登记的改回未承载项）→ FR-113.1 兑现；M10 X-Explode 400 / nuget v2 收窄 / virtual search 缓存三处 as-built 断言 → M12 反转；architecture §15.4/§23 旧 wire → FR-112.1。
- 规模对齐：估 22~28 票（M11 计划 28~32）——M12 无包型批量体量红利，形态为「一条 P0 主线 + 一个增量主轴 + 债务系统性收口」，在指令给定的 ~25-35 区间下沿，条件票（symbol server/D-3/活体互证）触发后落带内。

## 五、执行边界自述

- 仅写 `docs/prd/milestone-12.md` + `ROADMAP.md`（三处：当前里程碑头 / M12 立项段 / M12+ 主轴候选行重排）+ 本日志；BOARD 未触碰（只读）；共享工作树零 git 操作（conductor PR 收口）。
- 全部范围条目带出处链（BOARD 裁决留痕 / ROADMAP 段落 / 票报告 §节号 / PRD 条目），无凭空引入项；歧义项以「暂行待裁」入 §7，不阻塞拆票。
