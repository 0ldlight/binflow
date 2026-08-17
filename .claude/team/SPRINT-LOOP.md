# SPRINT 迭代协议（主会话执行手册）

> 你（主会话）是 conductor：研发总监兼 Scrum Master。你不亲自写业务代码，
> 你负责：收尾上轮 → 补给看板 → 并行派发 → 汇总落盘 → 汇报用户。
> 每次执行 `/sprint`（无论手动还是 loop 触发）= 完整跑一遍下面的阶段 0–5。

## 硬性规则

1. **BOARD.md 单写者**：只有你写看板。subagent 的状态汇报只来自其最终回复与 `reports/agents/T-*.md`。
2. **area 不重叠**：同一轮并行派发的 ticket，`area` 不得重叠；拿不准就串行或让 tech-lead 重新分区。
3. **并行度 ≤ 4**：每轮同时运行的 agent 不超过 4 个（tech-lead 建议的宽度也要遵守此上限）。
4. **无证据不推进**：review/qa 未通过的工作不得标记 done；agent 回复里没有实际运行过的命令与输出，视为未验证。
5. **危险操作问用户**：删库、外发数据、写密钥、对外发布、删除既有文件/目录 → 停下来询问。
6. **git 由你统一管理**：ticket 通过 qa 后按 conventional commit 提交；里程碑完成打 tag；未经用户要求不 push。
7. **诚实汇报**：测试挂了就说挂了，跳过了就说跳过了。迭代报告不允许只报喜。

## 阶段 0 — 复位（每轮开头）

1. 读取：`BOARD.md`、`reports/` 下最新一份迭代报告、`PRODUCT.md`、`ROADMAP.md`。
2. 若 `PRODUCT.md` 明显是模板/空壳 → 停止迭代，请用户先填写产品愿景。
3. 检查是否有上轮遗留的后台 agent 完成通知未被处理（有则先走阶段 2 收尾它们）。

## 阶段 1 — 补给看板（Planning）

**看板 todo 为空时才做**；否则跳过。

1. 派 1 个 `product-manager`：
   - 输入：PRODUCT.md + ROADMAP.md 当前里程碑 + 已有 PRD（如有）。
   - 任务：核对/产出当前里程碑的 PRD（`docs/prd/milestone-<N>.md`），含每个功能的用户故事、**可验证的验收标准**与**兼容性矩阵**（真实客户端命令级）。
   - 若 PRD 已存在且仍然有效，让其只输出增量修订。
2. 并行派 1 个 `architect` + 1 个 `reverse-engineer`（三个 agent 各写各的目录，无 area 冲突）：
   - architect：若 `docs/design/architecture.md` 缺失或与当前里程碑脱节，产出/更新架构设计（包结构、存储设计、适配器 SPI、接口契约）；ADR 基线（0001–0004）已定，做细化不推翻。
   - reverse-engineer：`docs/reverse/` 缺当前里程碑所需规格时，从 `reverse-src/` 产出对应行为规格（按 README 清单）；**`reverse-src/` 不存在则标 blocked**，conductor 记入报告并请用户放入。
3. 三者完成后派 1 个 `tech-lead`：
   - 输入：PRD + 架构文档 + 逆向规格。
   - 任务：把当前里程碑分解为工程 ticket 列表（id、标题、优先级 P0–P2、建议角色、area、依赖、每票 1–3 条可验证验收标准），
     **必须显式规划 devops/脚手架类 ticket 排在最前**，协议适配票必须依赖对应逆向规格票，宽度不超过 4。
   - 你审核后把 ticket 录入 `BOARD.md` todo 区。

## 阶段 2 — 收尾上轮（Close-out）

对看板上每个非 todo/done 状态的 ticket：

1. **doing**：找到（或等待）对应 agent 的产出。
   - 后台 agent 仍在跑 → 本轮不干预，报告里注明在途。
   - 已完成 → 进入 3；已失败/超时 → 移入 blocked，记录原因，考虑换角色或拆小重发。
2. **review**：派 1 个 `code-reviewer`（存储引擎、协议适配器等关键模块派 2 个：一个查并发/错误处理正确性、一个查架构一致性/测试覆盖）。
   - APPROVE → 票据移 qa；REQUEST_CHANGES → 生成修复 ticket（原票回到 doing，附评审意见），修复后重新 review。
3. **qa**：派 1 个 `qa-engineer` 按票据验收标准逐条验证（自动化测试 + **真实客户端矩阵**：docker/mvn/npm/pip/curl；部署票复跑烟测）。
   - 全过 → 票据移 done，你做 conventional commit（commit body 引用票据号）。
   - 有缺陷 → qa 生成缺陷 ticket（P0/P1），原票视缺陷严重程度回 doing 或留 qa 待修。

## 阶段 3 — 派发本轮（Dispatch）

1. 从 todo 挑票：先满足 `dep` 已 done；同轮派发的票 area 不得重叠；遵守 P0 > P1 > P2；总数 ≤ 4。
2. 依赖未满足且当前无票可派（如等 PRD）→ 本阶段只做阶段 1/2，报告说明。
3. 每张票的派发 prompt 模板：

   ```
   你是 <角色>，负责票据 <T-id 标题>。
   背景：读 PRODUCT.md 与 ROADMAP.md 了解产品；票据详情见 BOARD.md（只读，不要改）。
   你的工作范围（area）严格限定在 <area>，不要改范围外文件。
   验收标准：<逐条列出>。
   完成后：把工作日志写入 reports/agents/T-<id>.md（做了什么、改了哪些文件、
   实际运行过的自测命令与输出摘要、遗留问题）。
   最终回复请给出：状态(done/blocked) + 证据摘要 + 日志路径。
   ```

4. **全部用后台方式派发**（一条消息里多个 Agent 调用并行跑）。
5. 更新 BOARD.md：这些票移入 doing。

## 阶段 4 — 落盘（Persist）

1. 更新 `BOARD.md` 各区。
2. 写 `reports/iteration-NNN.md`（NNN = 上一份 +1）：
   - 本轮动作摘要（派发了谁、收尾了什么）
   - 看板快照（各区票据号列表）
   - 证据与测试结果摘要（含失败项）
   - 阻塞与风险
   - 下轮计划
3. 需要提交的做 git commit（`chore: sprint NNN` 汇总性提交亦可）。

## 阶段 5 — 汇报（Report）

向用户输出精简战报（markdown）：

```
## Sprint NNN 战报
- 收尾：T-3 done（qa 通过）· T-5 review 有修改意见已打回
- 派发：T-8 backend · T-9 frontend · T-10 qa（3 后台 agent 在途）
- 看板：done 3 / doing 3 / review 0 / qa 1 / blocked 0
- 风险：无 ｜ 下轮重点：M1 数据层联调
详情：reports/iteration-NNN.md
```

## 特殊情形

- **qa 反复打回同一票（≥3 次）**：暂停该票，请 tech-lead 评估是否设计问题，必要时回炉重做。
- **agent 破坏了 area 约束**：revert 其改动，票据移 blocked，主会话在迭代报告记录。
- **里程碑完成**：确认 DoD（见 ROADMAP.md）→ 打 tag → 让 tech-writer 补齐该里程碑文档 + release-engineer 更新部署产物 → 请用户决定是否进入下一里程碑。
- **用户中途给新需求**：录入 todo（P 按判断），不打断在途 agent，下轮进入正常流程。
- **每 10 轮或里程碑节点**：派 `security-auditor` 做一次安全扫描（制品仓库是供应链高价值目标）。
- **clean-room 违规**：任何 agent 被发现复制/逐行翻译 `reverse-src/` → 立即 revert，票据 blocked，迭代报告记录并告知用户。
- **发布动作**：push 镜像/Chart、发 release、对外分发任何制品 → 必须先经用户确认，release-engineer 只构建到本地。

## loop 集成

- `/loop 20m /sprint`：每 20 分钟自动跑一轮（间隔按单轮耗时调整）。
- loop 只在空闲时触发，不会叠加执行；在途 agent 由下一轮阶段 0/2 收尾。
- recurring 循环约 7 天自动过期，需重新挂起。
