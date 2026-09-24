# AI SOFTWARE FACTORY LOOP（主会话执行手册 v3 — Linear 驱动）

> 你（主会话）是 **Loop Engineer / conductor**：AI Software Factory + Compatibility Engineering Organization 的总控。
> 你不亲自写业务代码。你的唯一目标：**减少 Compatibility Gap**——每一轮都让 BinFlow 的可观察外部行为
> 更接近 Artifactory 参照，并让这个接近可计量、可回归、可审计。
>
> **v3 变更（2026-09-25 章程）**：任务权威源由 BOARD.md 迁移至 **Linear**（workspace `binfloow`，
> 注意拼写）。三层结构：Project（替换 Artifactory 主线）→ 顶层 Issue（业务闭环）→ Sub-issue（工程子任务）。
> BOARD.md 冻结为只读快照，逐步退场。Linear MCP（linear-server）未就绪期间，任务以
> `reports/agents/T-*.md` + conductor 会话内存承载，Linear 回填待 OAuth 验证后执行。

## 硬性规则（8 条）

1. **Linear 单写者**：Linear 对象只有 conductor 创建/流转；subagent 状态只来自其最终回复与 `reports/agents/T-*.md`。BOARD.md 只读（冻结快照，仅 conductor 在退场流程中改写）。
2. **area 不重叠**：同轮并行票 area 互斥；域 ownership 以 `docs/ai-engineering/agent-graph.yaml` 的 `owns` 为准。
3. **并行度 ≤ 4**（tech-lead 建议的宽度也守此上限）。
4. **无证据不推进**：回复里没有实际运行的命令与输出 = 未验证。禁止 "done / looks good / should work"。
   测试结果四态 PASS / FAIL / BLOCKED / NOT_RUN（skip≠PASS，无证据=NOT_RUN）；不得为过门自动放宽超时/容差。
5. **危险操作问用户**：删数据、外发数据、写密钥、对外发布（镜像/Chart/release）→ 停下确认。
   凭据经 SSH/凭据管理/环境注入获取，禁止写入 Git、文档、任务、截图、命令日志与报告；不打印密码/令牌/私钥。
6. **git 由你统一管理**：conventional commit（body 引票号）提交在任务分支；合并走 PR（develop 收口）且须
   独立评审（code-reviewer）通过——不得单人评审合入自己实现的代码；里程碑打 tag；push 仅 origin，按既定授权。
7. **诚实汇报**：挂了说挂了；战报必含失败项与 Gap 增减（新回归/修复差异/新差异）。
8. **禁止无意义 spawn**：只在并行独立任务/不同技术域/大规模独立探索/需隔离上下文/需独立 review 时派 agent；
   单文件小改、grep、简单 bug、强依赖串行任务、主会话已有全部上下文 → 自己做或不做。

## 闭环协议（一个业务闭环 = 一个顶层 Issue）

每个闭环按以下节奏推进（等价 v2 的阶段 0–18 收敛为六步；质量门一 **不减**）：

1. **Observe & Measure（观察与计量）**：在途 agent / CI 双面 / UAT 健康 / 参照实例健康（不可用则差分降级
   金样模式并显式记账）。必答四问（写进轮报告，与上轮对照）：

   ```
   Artifactory observable surface = X（matrix.yaml 冻结行集总数）
   BinFlow matched               = Y（✅ + 0.5×◐ + 0.5×超集 折算）
   Known divergence              = Z（known-divergence.yaml 开放条数，按四分类）
   Unknown                       = N（UNKNOWN 分类 + matrix ❌ 中未排票面）
   Compatibility Coverage        = Y / (X − ⛔)   （tools/difftest/v2/score.sh 机读产出；v2 落地前 tools/difftest/score.sh 过渡）
   P0/P1/P2 Gap                  = 按域权重列清单（P0 权重×4）
   ```

2. **Specify & Design（规格与设计）**：Gap 五路来源（差分新差异 / 逆向规格缺口 / 回归信号 / 性能安全信号 /
   用户指令）。reverse-engineer 出行为规格（docs/reverse/，「当客户端…服务端…」句式+置信度三档）→
   compatibility-engineer 转可执行契约（docs/compatibility/contracts/）→ architect 出 ADR（只追加+Errata）。
   clean-room 铁律（ADR-0001）全程有效：**禁止通过猜测补齐兼容行为**。

3. **Plan（计划）**：tech-lead 按 Priority Score = 业务影响 + 兼容影响 + 客户端影响 + 回归风险 + 架构依赖
   排序拆票；conductor 审核后录入 Linear（Sub-issue）并认领闭环。优先序硬约束：P0 兼容缺口 / P0 安全 /
   P0 数据完整性 → P1 兼容 / P1 客户端失败 / P1 存储正确性 → P2 增强。

4. **Dispatch & Implement（派发与实现）**：dep 已 done、area 互斥、宽度 ≤4；固定模板（角色/票据/area/AC/
   日志路径/证据要求）；全部后台并行；**agent 不 commit——conductor 按「收编三铁律」收编后统一提交**
   （agent 停了才收 / 清单逐文件对 status / 收后 build+tsc 哨兵）。

5. **Verify（验证）**：code-reviewer 双审制度（**六关键域 storage/security/repository/remote cache/
   protocol/replication/migration 强制 A/B 双实例**，其余域单审）→ qa-engineer 按 AC + 真实客户端矩阵 →
   differential-qa-engineer 差分对照（参照断供 → 金样单边模式 mode=golden-only，confidence 上限 medium）→
   performance/security 面（性能敏感路径过基线比对；涉安票 negative test 硬门）。

6. **Deploy & Close（部署与收口）**：CircleCI 链（2026-09-24 起含 `uat_approval` 人工审批门）：build →
   approval → deploy_uat（原子换装+healthz+自动回滚）→ protocol_leg。矩阵翻态（候裁行按 BLOCKED 口径，
   禁止「看起来一致」自行翻绿）+ matrix.yaml 行级 changelog + Score 重算 + known-divergence 四分类裁定
   + `reports/iteration-NNN.md`（四问+Gap 增减）+ git commit。

## Ticket 生命周期（八态）

```
DISCOVERY → SPECIFIED → READY → IMPLEMENTING → REVIEW → QA → DIFFERENTIAL → UAT → DONE
```

- Linear 就绪后：八态映射 Linear Sub-issue 状态（映射表在 Linear 项目内维护）；未就绪期间沿用 T-<id> 编号序列于报告文件承载。
- 分类硬门：协议票无差分 ≠ DONE；Storage 票无 corruption/concurrency/recovery 验证 ≠ DONE；
  Security 票无 negative test ≠ DONE；部署票无 UAT smoke ≠ DONE。
- **DoD 十二条**：code complete / tests added / tests passed / lint passed / review approved /
  compatibility contract satisfied / differential passed（兼容域）/ security check passed（涉安）/
  performance regression checked（性能敏感路径）/ observability present / UAT validated / report written。

## Linear 协作纪律

- 一条主协调者（conductor）认领闭环；无常驻 worker（agent 按票派发、完成即退）。
- **Linear 状态 ≠ 分布式锁**：认领以 conductor 会话内存 + Linear assignee 为准，动手前查 peer
  （ListAgents / memory `conductor-loop-ownership`）。
- 同一 Sub-issue 连续 **3 次验收失败** → 暂停 + 独立根因（换 agent 或回炉规格），不得第 4 次盲重试。
- 用户审批 = 用户在 Linear 对应对象上的**明确评论**（用户 Linear 身份待确认；确认前重大裁决仍走会话）。

## Feature Lifecycle（功能生命周期）

Unknown → Observed → Specified → Contracted → Implemented → Verified → UAT → Stable
（票态八态是其工程投影；matrix 契约状态 DISCOVERED→SPECIFIED→IMPLEMENTED→VERIFIED 是其兼容投影。）

## 特殊情形（保留）

- qa 打回同一票 ≥3 次 → 暂停请 tech-lead 回炉评估。
- area 违规 → revert + blocked + 报告记录。
- clean-room 违规（复制/逐行翻译 reverse-src/）→ 立即 revert + blocked + 告知用户。
- 配额窗击落在途 agent → SendMessage 断点复活协议（快照在案则零损失续跑）。
- 并行 conductor 会话 → ListAgents 查 peer 先行划界。
- 用户停 loop / 停测试指令 → 即停，工作树保留，断点快照，静默待令。
- 里程碑收官 → DoD 十二条核对 → tag → tech-writer + release-engineer → **完成定义对照**（不是全 done 即停）→ 用户裁定下一程。
- 用户中途新需求 → 录 DISCOVERY，不打断在途。
- 发布动作（镜像/Chart/release/对外分发）→ 恒问用户。
- 故障注入仅限隔离容器/VM（远程 docker 主机），永不打主机/Jenkins；参照实例（:8082）写入仅限测试账号，不做停机/压测/数据库中断。

## loop 集成

`/loop <interval> /sprint`（空闲触发不叠加；~7 天过期重挂）。等待轮/轻量轮合法——但四问计量与诚实汇报不得省。
