# 迭代报告 019 — Sprint 019

- 日期：2026-08-17 23:45（T-10 正确性 review 回报触发的处理轮）
- 里程碑：M1 内核基座
- conductor：主会话

## 本轮动作摘要

1. **T-10 正确性 review 回报：APPROVE**（0 blocker / 0 major / 4 minor / 4 nit；/tmp 探针 8 项实证全过；工作区无污染）。
   - 关键裁决依据：**MaxOpenConns=1 维持**——探针 F2 证实 modernc 驱动 foreign_keys 为 per-connection 且基线 0，若按 AC 字面改 NumCPU，单次 PRAGMA 会让其余连接 FK 静默失效（删仓不级联、无报错）。实现者「一行可改」说法被否定。→ 待 T-10 架构 reviewer 回报后，由 conductor 让 architect 勘误 ADR-0007/架构文档（pool 大小与 PRAGMA 挂载机制的耦合说明），代码不改。
   - 4 minor：FilterUnreferenced 缓冲页 TOCTOU（需 godoc 明示契约 + T-13 GC 二次确认闭环）、seedAdmin 死代码、tokenStore.Touch 上下文丢失、双进程首启迁移竞态未测。均不阻塞，记入 T-10 review 备忘，随 T-11（auth 用到 TokenStore）或修复窗口处理。
2. **T-9 架构 reviewer 补派**（第 4 槽位空出即补）：重点裁决两处契约偏离（GC 回调集合形、Close() 入接口）+ ADR-0006 布局比对。
3. T-10 双 review 现集齐 1/2（等架构视角）；T-8 review 仍未回。

## 看板快照（本轮结束时）

- todo: 10（T-11~T-20）
- doing:（空）
- review: T-8（1 reviewer 在途）、T-9（2 reviewer 在途：correctness + arch）、T-10（arch reviewer 在途，correctness 已 APPROVE）
- qa:（空）
- done: T-1~T-7, T-21~T-24
- blocked:（空）

## 证据与测试结果

- T-10-review-correctness.md 已落盘（8 探针 + race 全过 + clean-room 抽查通过）。
- 4 agent 槽位满（4 个 reviewer 全部在途）。

## 阻塞与风险

- T-10 的 4 minor 中「TOCTOU 契约」需在 T-13（adapter，调 GC/FilterUnreferenced 上游）实现时显式消化——已记入 review 备忘，T-13 派单要附此提醒。
- 双进程首启迁移竞态（compose 双进程窗口）：M1 单进程部署无实害，记入技术债（M4 备份/恢复时处理）。

## 下轮计划

1. 收剩余 3 份 review（T-8 / T-9 ×2 / T-10 arch）：合并裁决 → done 票 commit；REQUEST_CHANGES → 修复票。
2. T-10 全 APPROVE 后：① 派 architect 小票勘误 ADR-0007（MaxOpenConns=1 + PRAGMA per-connection 机制说明）② 派 T-11（auth/audit）。
3. T-13 派单时附 TOCTOU 契约提醒。
