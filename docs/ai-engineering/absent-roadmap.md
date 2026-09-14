# Absent 面攻坚路线图（❌78 行主战计划，LOOP 022 / L022-2，2026-09-14）

- 基线：matrix.yaml 200 行中 absent 78（P1 35 / P2 43）。扩张期已收官（六域 87 契约条目），本图是下一阶段主战输入。
- 三轴口径：**证据厚薄**（规格/差分底座在册度）/ **实现缺口**（既有底座可复用度）/ **客户端影响**（真实消费方面）。
- 排序原则：P1 密度 × 客户端影响 × 底座现成度；候裁/候超集族单列（不占攻坚批次）。

## 分域总表（每域一行：行数 / 前五能力 / 建议批次）

| D 域 | 行数（P1/P2） | 前五能力（按价值序） | 三轴速评 | 建议批次 |
|---|---|---|---|---|
| **D07 build-info** | 11（11/0） | build CRUD 五件 / append / promote / 批删 / retention | 证据厚（build-info.md 规格在册+指针行链 T-511/FR-152）；实现零底座（新域）；**客户端影响最高**（mvn/gradle CI 真实消费面） | **批次 1（主战线）**：3-4 票拆（CRUD→append/promote→批删/retention）；docker promote 随 M17 |
| **D03 search** | 8（4/4） | versions / latestVersion / dependency / buildArtifacts / docker-manifests | 证据厚（官方端点页+AQL 底座现成——gavc 族六端点已 ✅ 同构）；实现缺口小（查询层增量）；影响中（Maven 元数据消费方） | **批次 2a（快赢）**：versions+latestVersion 一票（P1 对）；build 指针两行随 D07 落地解锁；docker/P2 长尾后段 |
| **D01 storage 长尾** | 10（2/8） | Update Properties（R08）/ archive entry 抽取（R16）/ explode-archive / zap / ?mark | 证据中（R08=R06/R07 同族增量近邻已 ✅；R16 规格在 gap-endpoints）；缺口小；影响中（CI 脚本面） | **批次 2b（快赢）**：R08+R16 一票；P2 长尾（zap/explode/mark/tasks）挑 2-3 后段 |
| **D02 repo 管理面** | 6（4/2） | configurations / v2 配置读 / v2 batch 族（207 混合态）/ existence / repo_layouts | 证据厚（官方 v2 文档+update-merge 域刚清偿——方言底座新）；缺口中（batch 207 状态机）；影响中（terraform/CI provision 面） | **批次 3**：configurations+v2 读一票；batch 族单独（207 混合态）；Federation/layouts 候裁 |
| **D08 release-bundle** | 6（6/0） | 装配清单 / 事务三段式 / store 承接 / 查询族 / config | 证据中（release-bundle.md+ADR-0046 最小面已铺）；**依赖 D07**（build 数据源）；影响中高（发布链） | **批次 4（D07 后）**：最小面三票（v1 装配+查询→事务→store）；v2 signing M17 面外维持 |
| **D06 system 面** | 11（3/8） | System Info 汇总 / serverTime / config.xml 往返 / 导入导出 / Support Bundle | 证据中（rest-api.md 在册）；缺口分层（R04/R05/R14 轻聚合 vs config.xml 脱敏深水）；影响低-中（运维/监控面） | **批次 5a**：轻面一票（R04/R05/R14）；config.xml 往返单独深水票；导入导出候超集裁定（bf-migrate 对位） |
| **D04 安全长尾** | 12（0/12） | v2 permissions / 密码过期族 / 锁定族 / encryptedPassword / token 列表 | 证据厚（rbac-model/auth-model 在册+users 族底座现成）；缺口小（同 handler 族）；影响低（管理面）——**候裁族三支**（API Key〔R-8 族同判〕/SCIM/Crowd/Vault〔enterprise〕） | **批次 5b（微票群）**：过期+锁定+encryptedPassword+token 列表一票收 4 行；v2 permissions 单独；候裁三支入 pending-rulings 下轮 |
| **D05 replication 长尾** | 4（0/4） | multipush / 批量启停 / remote 清单 / checksum replication 配置 | 证据中（replication.md+D05-R01-R03 partial 底座现成）；缺口小；影响低（多目标推送场景） | **批次 6a**：一票收 4 行 |
| **D12 协议长尾** | 7（3/4） | PyPI JSON 元数据 / 重索引缺位 13 包型 / 40+ 包型栈 / VCS / migrations | 分层：R07=**pypi 契约域顺手票**（差分腿现成）；R10 随包型扩张；R15 归 PRODUCT 范围裁 | **批次 6b**：R07 随 pypi 域下轮；其余随包型排程/候裁 |
| D09 / D10 / D11 | 3（2/1） | outbox 行级重放 / 用户插件 / Lifecycles 治理 | **不占攻坚批次**：D09=C 层自有增强目标（候超集行翻位）；D10=R-9 已裁 DEPRECATE（候 ⛔）；D11=M18+ 专程（Q1 裁定在册） | 候裁/候超集通道 |

## 批次总览（建议执行序）

1. **批次 1**：D07 build-info（11 P1 主战线，3-4 票）
2. **批次 2（快赢并行）**：2a D03 P1 对 + 2b D01 R08/R16
3. **批次 3**：D02 P1 群（configurations/v2 读→batch 族）
4. **批次 4**：D08 release-bundle（D07 后置依赖）
5. **批次 5**：5a D06 轻面 / 5b D04 安全长尾微票群
6. **批次 6**：6a D05 长尾 / 6b D12 协议长尾（R07 顺手先行）
7. **候裁/候超集通道**（随呈批批次走）：D04 三支（API Key/SCIM/Crowd+Vault）、D06 导入导出、D09 超集翻位、D10 ⛔、D11/D12-R15 排程裁

- 预估：批次 1-4 清 P1 35 行的全部主量（D07 11+D08 6+D03/D01/D02 P1 12+D06 P1 3）；批次 5-6 清高值 P2；候裁族约 8-10 行走裁定通道不写码。
- 纪律提示：每批次沿用「规格核对→实现→契约化→差分→翻绿」全环；新域（D07/D08）开工前 reverse-engineer 规格置信复核先行。
