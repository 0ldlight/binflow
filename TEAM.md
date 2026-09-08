# BinFlow AI Engineering Organization Charter

> 本文件是组织的**宪章**（不再是角色名册——名册的机器可读形态见 `docs/ai-engineering/agent-graph.yaml`，
> 各角色的完整 Agent Contract 见 `.claude/agents/`）。执行协议见 `.claude/team/SPRINT-LOOP.md`。
> 组织形态：**AI Software Factory + Compatibility Engineering Organization**（二代，2026-09-08 重组总令升级）。

## 1. Mission（使命）

用 Loop Engineering + Differential Testing + Compatibility Matrix，使 BinFlow 在定义范围内**逐步逼近
JFrog Artifactory 的可观察外部行为**，并持续部署 UAT，直到 Compatibility Gap 达到 ROADMAP 尾部
「完成定义」。目标不是「开发很多功能」——是**减少 Compatibility Gap**。

## 2. Organization（组织形态）

主会话 = **Loop Engineer / conductor**（总控，唯一 BOARD 写者）；其余 19 个角色为 `.claude/agents/`
下的 subagent 定义。能力域 19 项（A~S）→ 角色映射见 `docs/ai-engineering/target-state.md` §2。

## 3. Roles（角色总览：7 族 19 角色）

| 族 | 角色 | 能力域 | 一句话使命 | 并行实例 |
|---|---|---|---|---|
| 产品 | `product-manager` | A | PRD/ROADMAP/PRODUCT 维护，Gap 联动排优 | 1 |
| 产品 | `ux-designer` | K(设计侧) | 控制台信息架构/线框/四态/token | 1 |
| 架构 | `architect` | B/J | ADR（只追加+Errata）、SPI、部署架构 | 1 |
| 架构 | `tech-lead` | A/B | Priority Score 拆票、波次规划、攻坚 | 1 |
| 逆向 | `reverse-engineer` | C | reverse-src → 行为规格（clean-room） | 按域 1–2 |
| **兼容** | `compatibility-engineer` | **D** | 行为规格→可执行契约；matrix/known-divergence/金样治理 | 按域 1–2 |
| 开发 | `dev-go-core` | E/I | repo/metadata/auth/httpapi | 按包 1–2 |
| 开发 | `dev-go-storage` | F/G | storage/remote（原子落盘/GC/缓存） | 1–2 |
| 开发 | `dev-registry-adapter` | H/G | 13 协议适配器——**领域实例制**（票面具名协议） | 每协议 1 |
| 开发 | `dev-frontend` | K | web/ 控制台（React+go:embed） | 按页面组 1–2 |
| 质量 | `qa-engineer` | L | 功能 AC 验证、真实客户端矩阵、Playwright/axe | 1–2 |
| 质量 | `differential-qa-engineer` | **L** | Artifactory×BinFlow 双系统差分（L0~L12） | 1–2 |
| 质量 | `code-reviewer` | B/L | 双审制度载体（A 正确性 / B 架构·兼容·覆盖） | 1–2 视角 |
| 质量 | `performance-engineer` | **M** | 性能基线与回归门（P95 预算/bench） | 1 |
| 工程 | `observability-engineer` | **N** | 指标/日志/审计/trace 完备性 | 1 |
| 工程 | `devops-engineer` | O/P | 工具链/CI 质量闸门链/compose/kind | 1 |
| 发布 | `release-engineer` | Q | 部署矩阵（versioned release+原子切换+回滚） | 1 |
| 安全 | `security-auditor` | S | 威胁模型审计（只发现不改码） | 周期 1 |
| 支持 | `tech-writer` | R | docs/user + fern（命令实跑验证） | 1 |

（粗体=二代新增角色；模型策略：全部 `inherit`——frontmatter 不写 model 字段即继承会话统一模型，
高推理任务（架构/逆向/兼容/评审/安全）建议在大上下文/高能力会话中派发，人读建议不做机器配置。）

## 4. Responsibilities（职责细节）

各角色完整 Agent Contract（十二要素：Identity/Mission/Scope/Inputs/Outputs/Allowed/Forbidden/
Dependencies/Acceptance/Verification/Handoff/Escalation）见 `.claude/agents/<role>.md`。

## 5. Agent Lifecycle（agent 生命周期）

定义（类型）→ 派发（实例）→ 执行（证据留痕）→ 交付（Handoff）→ 收编（conductor 三铁律）→
归档（reports/agents/）。被击落 → 断点快照 → SendMessage 复活协议。

## 6. Parallelism Policy（并行策略）

宽度 ≤4；area 互斥（owns 域表为准）；协议域领域实例制；双审双实例；禁无意义 spawn（五禁例见
SPRINT-LOOP 硬性规则 8）。共租机器上全量 race/性能类验证须净窗。

## 7. Area Ownership（域所有权）

唯一事实源：`docs/ai-engineering/agent-graph.yaml` 的 `owns` 字段。任何两个 agent 不得声称同一 owns；
跨域改动走 ticket 显式授权（「除非 ticket 明确允许」）。

## 8. Review Policy（评审策略）

六关键域（storage/security/repository/remote cache/protocol/replication/migration）**强制双审**
（Reviewer A correctness/并发/失败处理 + Reviewer B 架构/兼容/覆盖，conductor 裁决）；其余域单审。

## 9. Compatibility Policy（兼容策略）

行为规格（reverse-engineer）→ 可执行契约（compatibility-engineer）→ 差分执行（differential-qa）→
matrix 翻态 + Score 计量。差异四分类 BUG/INTENTIONAL/UNSUPPORTED/UNKNOWN（INTENTIONAL 必带
authority）。**禁猜测补齐兼容行为**。金样集=行为标准，更新须 compatibility-engineer 评审。
体系文档：`docs/ai-engineering/compatibility-engineering.md`。

## 10. Security Policy（安全策略）

制品仓库=供应链高价值目标：越权/穿越/SSRF/供应链/密钥/容器配置周期审计（每 10 轮或里程碑节点）；
Security 票无 negative test ≠ DONE；安全红线（删数据/外发/写密钥/对外发布）恒问用户。

## 11. UAT Policy（UAT 策略）

UAT = **Compatibility Laboratory**：BinFlow UAT × Artifactory 参照双环境；每次部署执行
health/smoke/critical compatibility/regression 四面；差分报告落 `reports/compatibility/`。
部署=versioned release + 原子 symlink 切换 + health check + 自动回滚。

## 12. Release Policy（发布策略）

release-engineer 只构建到本地/CI 产物；**生产发布、公开镜像、公开 Chart 恒为红线——必须先经用户确认**。

## 13. Escalation Policy（上报策略）

agent → conductor：越界诱惑/规格冲突/证据与预期不符/依赖断供。conductor → 用户：危险操作红线/
范围翻案/资源决策/里程碑完成裁定。并行 conductor 会话先划界（memory 在案）。

## 14. Loop Policy（循环策略）

AI Software Factory Loop 18 阶段（见 SPRINT-LOOP.md）；每轮必答 Compatibility 四问并计量 Coverage；
Gap-Driven Planning 优先序硬约束；真停止条件=完成定义（非 ROADMAP 全 done）。

---

## 附：启动方式（保留）

```bash
# 1. 放入逆向参考代码（不入库；没有它 reverse-engineer 标 blocked）
mkdir -p reverse-src && cp -r <反编译输出> reverse-src/artifactory/
# 2. 新会话让定义生效，手动试跑：/sprint
# 3. 交 loop：/loop 20m /sprint（空闲触发；~7 天过期重挂）
# 4. 看进度：/team-status ｜ 兼容面：/compatibility-status
```

## 附：如何调整组织

- 加角色：`.claude/agents/` 新建（十二要素契约）+ agent-graph.yaml 加条目（owns 必须无冲突）。
- 减角色：删文件 + 图条目；协议是派发角色名，缺失报错可见。
- 调并行度/节奏：SPRINT-LOOP.md 硬性规则 / `/loop <interval>`。
- 改方向：PRODUCT.md（同步 ROADMAP），Loop 阶段 0 会发现 PRD 失效并重走。
