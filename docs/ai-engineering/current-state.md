# BinFlow AI Engineering Organization — 现状审计（Current State）

> 产出依据：重组总令（2026-09-08，32 节）§一/§31 第一阶段「先审计后改动」。三路并行审计（A 治理与角色 / B 技术资产与 CI·UAT / C 流程现实与债务）实测文件内容，非文件名推断。
> 审计基线：develop@a1ac8438（2026-09-08 01:54），里程碑 M17 进行中 16/35（W8 双票在途）。

## 1. 当前 Agent Team（15 角色，.claude/agents/ 实读）

主会话 = conductor（研发总监/Scrum Master），唯一 BOARD 写者与 git 提交者。15 个 agent 定义文件，全部为「输入/职责/工作准则/输出契约」四段结构，工具面按角色裁剪：

| 组 | 角色 | 模型建议（TEAM.md） | 一句话职责 |
|---|---|---|---|
| 产品 | product-manager | opus | PRD（用户故事+可验证 AC）、ROADMAP、需求终裁建议 |
| 产品 | ux-designer | sonnet | 控制台信息架构/线框/四态/design token（无 Bash/Web 工具） |
| 架构 | architect | opus | ADR（只追加）、存储/元数据/适配器 SPI、部署架构 |
| 架构 | tech-lead | opus | PRD→工程票分解、并行分区、攻坚（qa 打回 ≥3 次接手） |
| 逆向 | reverse-engineer | opus | reverse-src/ 只读反编译 → clean-room 行为规格（docs/reverse/） |
| 实现 | dev-go-core | sonnet | internal/repo、metadata、auth、httpapi |
| 实现 | dev-go-storage | sonnet | internal/storage（checksum 寻址/去重/原子落盘/GC/备份） |
| 实现 | dev-registry-adapter | sonnet | internal/adapter/ 13 协议子包，真实客户端验收 |
| 实现 | dev-frontend | sonnet | web/ React 控制台（go:embed、四态、契约漂移上报） |
| 工程 | devops-engineer | sonnet | 工具链/Makefile/lint/CI/compose/kind |
| 发布 | release-engineer | sonnet | goreleaser/镜像/compose/Helm/K8s/systemd/离线包（只构建不发布） |
| 质量 | qa-engineer | sonnet | AC→测试计划、真实客户端矩阵、Playwright+axe、独立验证不采信自测 |
| 质量 | code-reviewer | opus | 只读评审 APPROVE/REQUEST_CHANGES，correctness+consistency 双视角 |
| 安全 | security-auditor | opus | 威胁模型审计（越权/穿越/SSRF/供应链/密钥/容器），只发现不改码 |
| 文档 | tech-writer | sonnet | docs/user 帮助中心 + fern 镜像（命令实跑验证） |

**多实例机制**（TEAM.md）：类型=定义文件，实例=派发。支持并行实例（同型多派）、视角实例（双 code-reviewer）、领域实例（复制定义文件改名）。

## 2. 每个 Agent 的职责细节

见 `.claude/agents/<name>.md` 各自正文（审计 A 已全文核对）。共性：四段结构、输出契约含「reports/agents/T-<id>.md 工作日志 + 最终回复=状态+证据+日志路径」。个性差异显著（如 reverse-engineer 的置信度三档标注、qa 的「不采信自测」铁律、code-reviewer 的只读取证可跑 go test）。

## 3. Agent 间关系（现行，实为隐式——无显式依赖图文件）

```
用户 → PRODUCT.md ← product-manager（维护）
product-manager → docs/prd/milestone-N.md → tech-lead → BOARD 拆票（M<N>-SPLIT.md）
architect ⇄ reverse-engineer（并行于阶段 1：ADR ⇄ 行为规格，软缝协议ADR-0041 效力序=用户裁决>规格票>ADR>PRD）
tech-lead 拆票 → conductor 派发 → dev-*（实现，area 排他）→ code-reviewer（review 态，关键模块×2 视角）→ qa-engineer（qa 态）→ conductor 收编+commit
security-auditor：每 10 轮或里程碑节点旁路插入
release-engineer：里程碑收口；tech-writer：文档票/收尾
```
**无 agent-graph.yaml**；关系承载于 SPRINT-LOOP.md 流程文字 + 各 agent prompt 的「输入（conductor 派发时给出）」段。

## 4. 当前 Sprint Loop（.claude/team/SPRINT-LOOP.md，110 行）

`/sprint` = 阶段 0–5 一轮；`/loop 20m /sprint` 常驻。硬性规则 7 条：BOARD 单写者 / area 不重叠 / **并行度 ≤4** / 无证据不推进 / 危险操作问用户 / git 由 conductor 统一管理 / 诚实汇报。

| 阶段 | 内容 |
|---|---|
| 0 复位 | 读 BOARD/最新迭代报告/PRODUCT（空壳即停）/ROADMAP + 检查上轮 agent 通知 |
| 1 补给 | todo 空时：PM PRD → architect+reverse-engineer 并行 → tech-lead 拆票（脚手架最前、协议票 dep 规格票、宽度 ≤4）→ conductor 录入 |
| 2 收尾 | doing 在途不干预（失败转 blocked）；review→code-reviewer（关键模块双视角）；qa→qa-engineer（AC 逐条+真实客户端矩阵）→ done+conductor commit；缺陷→D-票 |
| 3 派发 | dep 已 done、area 排他、P0>P1>P2、≤4；固定 prompt 模板；全后台并行 |
| 4 落盘 | BOARD 更新 + reports/iteration-NNN.md（动作/快照/证据含失败/阻塞/下轮）+ git commit |
| 5 汇报 | 精简战报 |

特殊轮型：qa 打回 ≥3 次→tech-lead 回炉；area 违规→revert+blocked；里程碑完成→DoD→tag→tech-writer+release→请用户定下一程；用户新需求→录 todo 不打断在途；每 10 轮安全审计；clean-room 违规→立即 revert+告知用户。

**实际运转补充**（迭代报告 1533+ 轮实证）：配额窗击落-复活循环（SendMessage 断点续）；双 conductor 会话划界（主循环/只读简报+接管预案，memory `conductor-loop-ownership` 在案）；拆票日志 M<N>-SPLIT.md 承载波次表（W0~W17）。

## 5. 当前 Ticket 生命周期

**纸面**：`todo → doing → review → qa → done`，异常 `blocked`。票格式 `T-<id> [P0|P1|P2] 标题 role: area: dep:` + AC 1-3 条可验证标准。缺陷票 `D-T<票>-<n>`。
**实际**：BOARD 头部五区（todo/doing/qa/done/blocked）中 doing/qa 区长期「（空）」——真实状态以里程碑分节+票行内标注+尾部时序日志承载（矛盾点 #1，见 §12）。

## 6. 当前 Board 规则

- 单写者=conductor（subagent 只读，状态经 reports/agents/T-*.md 回流）
- 结构：头部五区 + 里程碑分节 + 尾部时序战况日志（现 2035 行）
- 已知漂移：头部「当前里程碑」字段滞留 M6+（实际 M17）——矛盾点 #2

## 7. 当前 Review / QA 机制

- code-reviewer（opus）：correctness（并发/错误/goroutine 泄漏）+ consistency（分层/覆盖）双视角，关键模块双实例并行；结论 APPROVE/REQUEST_CHANGES + blocking 文件:行号
- qa-engineer：独立验证（不采信开发自测）、真实客户端矩阵、Playwright UI（含 axe 双主题、trace 调试、scratch 实例自起）、存储完整性、部署烟测复验
- 实际执行：大量票走 conductor 直审/亲验直收；关键票完整双审（T-215/T-217 双 REQUEST_CHANGES 独立收敛等案例在案）
- 无系统性「双 reviewer A/B 分工」制度——按票裁量

## 8. 当前 Reverse Engineering 机制

- clean-room 铁律（ADR-0001）：reverse-src/ 只读；产出=docs/reverse/ 行为规格（38 份，含全量功能矩阵 inv-1~4、REST 兼容矩阵 rest-compat-matrix.md、console-ui 活体走查）
- 规格三档置信度标注；软缝协议（ADR-0041 起）：ADR 定机制不定字面量，字面以规格票冻结为准
- 参照基线：本地 Artifactory :8082（7.161.20，addons 全开）活体实证文化（t*-probe/ 取证目录惯例）
- **无 compatibility-engineer 角色**：规格→可执行契约的转化、四态差异裁定（Bug/Intentional/Unsupported/Unknown）、差异台账均不存在

## 9. 当前 CI/CD

**双面 CI**（同内容两面板）：
- **CircleCI**（主部署链）：`uat` workflow（main push）= build（parallelism 2 + tests split + junit/insights + 版本戳 uat.<sha>）→ deploy_uat（caddy TLS 幂等层 + systemd 换装 + 备份×5 + healthz 有界探针 + **失败自动回滚**）→ **protocol_leg ×10 并行矩阵**（intake ⑱ 落地：每腿只装自家工具链，腿级归因上 commit status）；build → e2e（Playwright 真栈+种子）。`nightly`（03:17 UTC）= race_full（全树 -race ×4 分片）+ 十腿打常驻 UAT（漂移面）。缓存五面（go-mod/go-build/npm×2/pw-browsers）；DLC；无 orbs（刻意）
- **GitHub Actions**：ci.yml（main push：console lint/typecheck/audit→make test 30m -race→GC 并发压力专属步（ADR-0031 永久耦合）→零 CGo build）+ e2e + release-dryrun（goreleaser 六平台干跑）；protocol-matrix.yml（手动 dispatch，push:main 注释候账单）
- 脚本单源：ci/protocol-matrix.sh（1583 行）——十腿=真实客户端全链路（推送+拉取+内容断言）+ T-479 remote/virtual 段（真实公网上游 MISS→HIT/Resolved-From 头断言）
- 另有 Jenkins/VM 测试链（develop → 172.16.58.129）
- chunk sidecar（CircleCI 供给的远程 Linux 验证环境）已接入：node22 snapshot 在案

## 10. 当前 UAT 机制

- 双链 gitflow：develop→Jenkins/VM；**main→CircleCI/UAT**（52.79.109.153，systemd `binflow-uat`，:8080，数据 /opt/binflow-uat；域名 uat.binflow.org + caddy TLS 幂等层）
- 部署：原子换装 + healthz 有界探针 + 失败自动回滚 + 烟测（healthz/版本/docs 200）
- 版本标记：uat.<sha7>（ldflags 注入）
- 部署后验证 = protocol_leg 十腿打新装实例；nightly 打常驻实例（漂移面）
- **UAT=单一 BinFlow 实例**——无 Artifactory 参照实例并行运行（「Compatibility Laboratory」形态不存在）

## 11. 当前 Compatibility Testing 能力

- **载体**：docs/reverse/rest-compat-matrix.md（T-503，2026-09-06）——BinFlow 现状 REST 面（112 路径/158 ops/20 域）× Artifactory 全量 REST 面（官方三索引 653 条目、反编译测绘 ≈1866 方法级操作）的**逐端点四态对账活账**（✅/◐/❌/⛔ + 行级 changelog）
- **无**：docs/compatibility/ 目录体系、Compatibility Contract（可执行契约）、probe 资产库、Known Divergence 台账、Golden Behavior Dataset、Compatibility Score、双实例差分测试框架
- 最接近的既有能力：protocol-matrix 的 remote/virtual 段（单实例对真实上游拉穿对照）；e2e 中 BASE2 门控的双实例腿（t422，唯一样本）；artifactory-parity 锚册（FE 侧）
- 差异四态裁定（Bug/Intentional/Unsupported/Unknown）无建制——散见于票面「契约漂移登记」

## 12. 当前主要缺陷（A/B/C 三审计合账）

**流程/治理面**：
1. 状态流纸面 vs 看板实际脱节（review 态无区；doing/qa 区长期「空」但 W8 双票实际在途——只能从最新迭代报告看到）
2. BOARD 头部滞留（「当前里程碑 M6+」+ 头部三节 M7~M9 时代陈旧内容）
3. product-manager 定义未随 2026-09-06 范围翻案更新（仍写「不做 HA/LDAP/SAML」）
4. 回写欠账积压在 conductor（A1 残留 4 锚、B-3.18 措辞、T-513 勘误、ADR-0044 勘误行、TEST_TIMEOUT 重校准——均「顺腿/候收口」无人接）
5. 并行 conductor 双写险（已划界；本轮 BOARD 追加被并行会话提交裹走即例证）

**产品面（开放项）**：
6. `/api/system/version` 返 'dev' —— jf CLI 直接报错（未排票）
7. T-509：DELETE 批删族未路由、retention 不持久化、manifest 源属性不随迁 divergence
8. T-508/T-511 等 4+ 项中/低置信裁定「候活体翻案」排队
9. npmjs packument 上游不兼容（协议矩阵以合成上游绕行，M4 积压）

**质量面**：
10. 双活体参照实例（7.161 pro + t226 OSS）自 09-05/06 双双损坏未修复（Q10 悬置）——rest-compat-matrix 取证基线退化为纯书面三源
11. 全量 race 门被单机共租反复突破（httpapi solo 1610s > 20m 窗；TEST_TIMEOUT 重校准欠账）

## 13. 当前架构债务（A/B/C 合账）

1. BOARD.md 2035 行/520KB + reports/ 1530+564 条目单仓承载（检索/写入成本持续增长）
2. 里程碑分节制使头部五区形同虚设（结构债）
3. agent 定义重复 generic 规则（无「通用规范/专业契约」分层）
4. 无显式 agent 依赖图/域 ownership 表（area 排他靠 conductor 人脑 + SPLIT 波次表）
5. **三套差异登记册并存未合并**（rest-compat-matrix 195 行 / console-parity v1.14 / gap-endpoints+full-feature-matrix 旧账）——更新协议全手工
6. **differential 全仓零命中、对拍零 e2e 化**——兼容验证 = 书面对账 + 单票内 curl 断言
7. 里程碑推进依赖单机共租（load 125 / 快照树复验 / lint 锁候自愈等事件反复）
8. web/i18n 仅 en 一档（M16 双语成果状态以 BOARD 在案记录为准）
9. 等待轮占 42%（1530 轮中 642 轮）——派发宽度 ≤4 + 全宽 2 下两票深在途的结构性空转

## 14. 与「完整复刻 Artifactory」目标的差距

**数量面**（rest-compat-matrix v1 冻结行集）：195 冻结行四态 = ✅已兼容 57 / ◐部分 27 / ❌缺位 83 / ⛔不适用 17 / 超集 11；BinFlow 158 ops 全落位（对位 119/别名 19/纯超集 20）。对位 Artifactory 测绘 ≈1866 方法级操作（官方三索引 653 条目）——**核心口径覆盖约 1/3，全量口径更低**。
**结构面**：
1. 无兼容工程建制（可执行契约/probe 资产/差分执行/金样集/评分——总令 §三~§七直指的全套缺位）
2. UAT 非双实例实验室（无 same-request→双发→normalize→diff 能力）
3. 差异裁定无 Bug/Intentional/Unsupported/Unknown 四分类（矩阵四态是覆盖分类，非成因分类）
4. Loop 目标=「完成 ticket」非「减少 Compatibility Gap」（无 Coverage/Gap 计量与 P0/P1/P2 Gap 排序）
5. 活体参照断供（双容器损坏）使「逼近可观察行为」失去对照源——差分体系的地基缺口
6. 领域纵深：Federation/HA/分布式零基础；Build-info 域 W7 刚闭环

---
*Phase 1 审计三部曲（A 治理/B 技术/C 流程）已全体并入，本文件定稿。GAPS/PROPOSED TARGET/MIGRATION 见 target-state.md。*
