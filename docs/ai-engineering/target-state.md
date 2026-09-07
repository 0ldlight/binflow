# BinFlow AI Engineering Organization — 目标态与迁移计划（Target State & Migration）

> 重组总令 §二/§六~§十四/§十八~§二十七/§三十 落地设计。现状见 current-state.md；体系细节见 compatibility-engineering.md；依赖图见 agent-graph.yaml。
> 组织形态：**AI Software Factory + Compatibility Engineering Organization**（自「AI Scrum Team」升级，保留有效角色，迁移而非推翻）。

## 1. GAPS（现状→总令要求的差距总表）

| # | 总令要求 | 现状 | Gap |
|---|---|---|---|
| G1 | compatibility-engineer 体系（契约/probe/matrix/divergence/金样/评分） | rest-compat-matrix 活账 + 三套分散登记册，零可执行契约 | 全套建制（compatibility-engineering.md） |
| G2 | differential-qa 双实例差分 L0~L12 | 全仓零 differential；唯一 BASE2 样本 | 体系 + 参照实例修复（前置） |
| G3 | Loop 目标=减少 Compatibility Gap + 四问 + Coverage 计量 | Loop 目标=完成 ticket；无计量 | SPRINT-LOOP 重写 + score 脚本 |
| G4 | Ticket 八态生命周期 + 分类 DoD 硬门 | 五态（review 无区）+ DoD 无分类硬门 | 生命周期升级 + 在途票豁免条款 |
| G5 | 15+4 agent（compatibility/differential-qa/performance/observability） | 15 agent（无四新域） | 4 新定义 + 15 定义二代化 |
| G6 | Agent Contract 十二要素（分层：identity/mission/scope/authority/deliverable/evidence/handoff/escalation） | 四段结构 + generic 规则混写 | 逐个重写（§27） |
| G7 | 显式 agent-graph + domain ownership | 隐式（conductor 人脑 + SPLIT 表） | agent-graph.yaml（已成稿） |
| G8 | 双审制度化（A correctness / B architecture·compat·coverage） | 按票裁量双实例 | SPRINT-LOOP 制度化六域强制 |
| G9 | CircleCI 全质量闸门链（含 differential 段） | 双 CI 三段 + 十腿（无差分段） | CI 增量（差分 job 挂 deploy_uat 后） |
| G10 | UAT = Compatibility Laboratory（双实例） | UAT 单实例；参照双容器损坏 | 参照修复 + difftest 接入 |
| G11 | 8 命令面（compatibility-status/gap/diff-test/uat/release-check/architecture-review/security-review） | sprint/team-status 两命令 | 6 新命令 |
| G12 | 停止条件=Gap 归零口径 | ROADMAP 全 done | 完成标准重定义（不推翻里程碑历史） |

## 2. PROPOSED TARGET — 组织（A~S 能力域 → 角色×实例）

| 能力域 | 承载角色（定义文件） | 实例形态 |
|---|---|---|
| A Product/Program | product-manager（+conductor 的 Program 面） | 单实例 |
| B Architecture | architect / tech-lead | 各单实例 |
| C Reverse Engineering | reverse-engineer | 按域多实例 |
| **D Compatibility Engineering** | **compatibility-engineer（新）** | 按域多实例 |
| E Backend Core | dev-go-core | 按包多实例（repo/metadata/auth/httpapi） |
| F Storage/Blob | dev-go-storage | 单实例 |
| G Remote/Cache | dev-go-storage（owns internal/remote）+ dev-registry-adapter（协议回源面） | 双角色分面 |
| H Package Protocol | dev-registry-adapter（**领域实例制**：dev-package-maven/npm/pypi/docker/… 同一定义派生，派发时按协议具名） | 每协议一实例 |
| I Security/Identity | dev-go-core（auth 面）+ security-auditor（评审） | 实现与审计分立 |
| J Distributed Systems | architect（J 域 ADR 前置）+ 后续按需领域实例 | 现阶段以 ADR 承载 |
| K Frontend | dev-frontend | 页面组多实例 |
| L QA/Differential | qa-engineer + **differential-qa-engineer（新）** | 功能 AC 与差分双轨 |
| M Performance | **performance-engineer（新）** | 单实例 |
| N Observability | **observability-engineer（新）** | 单实例 |
| O/P/Q DevOps/CI/Release | devops-engineer / release-engineer | 各单实例 |
| R Documentation | tech-writer | 单实例 |
| S Security Review | security-auditor | 周期 + 里程碑节点 |

**模型策略**（§二十）：全部 `model: inherit`（当前 Claude Code + GLM 环境不支持 agent 级模型选择的伪配置，不制造）。TEAM.md 保留「高推理任务优先大上下文/高能力模型」的**人读建议表**，不作机器配置。

## 3. PROPOSED TARGET — Loop（AI Software Factory Loop，18 阶段）

```
0 Reset          复位（读 BOARD/matrix/known-divergence/最新报告/PRODUCT）
1 Observe        观察（在途 agent/CI/UAT/参照实例健康）
2 Measure        计量 Gap（四问 + Coverage + P0/P1/P2 清单，score.sh 机读）
3 Discover       发现（逆向规格缺口/差分新差异/回归/性能/安全信号 → DISCOVERY 态票）
4 Specify        规格化（reverse-engineer 行为规格 → compatibility-engineer 可执行契约）
5 Design         设计（architect ADR / ux 规范）
6 Plan           计划（tech-lead 按优先序公式拆票 + 波次）
7 Dispatch       派发（宽度 ≤4、area 排他、agent-graph 依赖校验）
8 Implement      实现（dev-* 领域实例）
9 Review         评审（双审制度化：六关键域 A/B 双 reviewer 强制）
10 Unit/Int QA   功能 QA（qa-engineer，AC 逐条 + 真实客户端）
11 Diff QA       差分 QA（differential-qa-engineer；协议域票无差分不得 DONE）
12 Perf/Sec      性能/安全（performance-engineer 基线比对；security-auditor 周期）
13 Deploy UAT    部署（CircleCI 既有链：build→deploy→protocol×10→[新增]difftest）
14 UAT Verify    UAT 验证（health/smoke/critical-compat/regression）
15 Update Matrix 矩阵更新（状态机翻态 + 行级 changelog + Score 重算）
16 Persist       落盘（BOARD/iteration 报告/commit/tag）
17 Report        战报（四问对照上轮：new regressions/fixed divergences/new divergences）
18 Select Next   选下一最高价值 Gap → 回 0
```
等待轮纪律保留；配额窗击落-复活协议保留；双 conductor 划界协议保留（memory 在案）。

## 4. PROPOSED TARGET — Ticket 生命周期与 DoD

**八态**：`DISCOVERY → SPECIFIED → READY → IMPLEMENTING → REVIEW → QA → DIFFERENTIAL → UAT → DONE`（映射：现五态=READY 以前合并+后段合并；BOARD 行内状态字段改用八态缩写，尾部日志制保留）。
**分类硬门**：协议兼容票无差分测试 ≠ DONE；Storage 票无 corruption/concurrency/recovery 验证 ≠ DONE；Security 票无 negative test ≠ DONE；部署票无 UAT smoke ≠ DONE。
**DoD 12 条**（code/tests-pass/lint/review/contract/differential/security/perf/observability/UAT/report/证据齐）——「代码写完=done」禁止。
**在途票豁免**：改造落地时在途票（W8 T-512/T-496 及其后续波次）按**旧口径**收编至 m17-done 或该票自然终点；新口径自 M18 波次起生效（与并行 conductor 的约定条款）。

## 5. PROPOSED TARGET — 命令面

保留 `/sprint`（执行新 Loop 18 阶段）、`/loop`、`/team-status`；新增：
`/compatibility-status`（matrix+score+四问快照）｜`/compatibility-gap`（P0/P1/P2 Gap 清单+优先序建议）｜`/diff-test`（触发差分批次：域参数→differential-qa-engineer）｜`/uat`（UAT 健康+版本+最近十腿/差分结论）｜`/release-check`（DoD 12 条核对+发布红线检查清单）｜`/architecture-review`（ADR 一致性巡检：agent-graph owns 违规/软缝勘误欠账/Errata 开放项）｜`/security-review`（security-auditor 旁路触发）。

## 6. PROPOSED TARGET — CI 与 UAT

- CircleCI `uat` workflow 增量：`deploy_uat` 后挂 `difftest` job（tools/difftest 对 UAT×参照；参照未修复前以金样单边模式跑，job 内显式标注 mode=golden-only）
- nightly 增挂金样回归 + Score 重算产出
- UAT 完成标准（§十九）落 ROADMAP 尾部「完成定义」段（不重写历史里程碑）

## 7. MIGRATION PLAN（四步，均不破坏在途）

| 步 | 内容 | 风险控制 |
|---|---|---|
| M1 组织文件 | 4 新 agent 定义 + 15 定义二代化（Agent Contract 十二要素）+ TEAM.md 重写为 Organization Charter + agent-graph.yaml 入库 | 逐文件迁移；保留原文有效条款；并行 conductor 期间其域不动 |
| M2 流程文件 | SPRINT-LOOP.md 重写（18 阶段+八态+DoD+Gap 计量）+ CLAUDE.md 更新（文件地图增 docs/compatibility、tools/difftest、bench）+ BOARD 头部修正（当前里程碑字段+八态说明，仅必要字段） | 在途票豁免条款显式；旧五态与新八态映射表附录 |
| M3 体系落地票 | 参照实例修复（Q10 转正）→ matrix.yaml 迁移 → known-divergence 首批收编 → difftest 骨架 → 金样首批采集 → score.sh —— 以正常 ticket 走新 Loop（Dogfood） | 每票可独立回滚；不动产品码 |
| M4 命令与 CI | 7 命令落 .claude/commands/ + CircleCI difftest job | actionlint/config process 门 |
| 验收 | git diff --stat 全审 + 全量 lint/test（sidecar 或净窗）+ smoke + §32 十项清单战报 | --no-verify 仅限治理文件（chunk 钩子 npm 面已修） |

## 8. 不变项（红线与有效资产全保留）

- BOARD 单写者（conductor/Loop Engineer）——§二十二
- clean-room 铁律（ADR-0001）与 reverse-src/ 只读
- 危险操作问用户（删除/外发/写密钥/对外发布）
- 双 CI 三段既有链 + 十协议矩阵（只增不拆）
- M1~M17 里程碑历史与 ROADMAP 不重写；完成标准新增不替代
- 禁止无意义 spawn（§二十三五禁例入 SPRINT-LOOP）
