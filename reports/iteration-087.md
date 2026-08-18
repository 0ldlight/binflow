# 迭代报告 087 — Sprint 087

- 日期：2026-08-18 13:40（T-35 review 回报触发的裁决轮）
- 里程碑：M2
- conductor：主会话

## 本轮动作摘要

1. **T-35 review 裁决：REQUEST_CHANGES（3 blocker 探针实证）**→ 修复单已派：
   - B1：DeleteRepo 拆库顺序——DeleteRepoRefs 在仓行删除前跑，并发窗口写入的无 FK refs 行永久存活（GC 永久 pin blob + 幽灵引用继承）；reviewer 已验证修复路径（移到删除后）。
   - B2：同 digest 幂等重推改写 created_by/media_type/size（违反幂等零变更语义）+ TagRepointed 三方矛盾定约。
   - B3：哨兵错位（ErrInvalidImage 被复用两处，T-38/39/40 定约点）。
   - 正面确认：级联同事务、blob-first、200 轮并发不变量、clean-room 合规。
2. 在途：T-33 修复、T-35 修复（2 槽）。
3. M2 节奏注：两修复轮并行，完成后批次 2 补齐（T-37/T-41）。

## 看板快照（本轮结束时）

- todo: 8 · doing: T-33（修复）、T-35（修复）· review: 0 · done: 41 · blocked: 0

## 阻塞与风险

- 无。review 探针方法学（/tmp 实测非推断）持续兑现价值。

## 下轮计划

1. 收双修复 → 针对性复审 → done → **派 T-37 + T-41（批次 2 补齐）**。
2. T-37 派单袋：PRD v1.1 口径（realm/双入口/D04c）、R7 文件边界（仅 adapter/docker/）。
