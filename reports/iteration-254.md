# 迭代报告 254 — Sprint 254

- 日期：2026-08-21 09:02（loop job 067cb679 触发）
- 里程碑：M4
- conductor：主会话

## 本轮动作摘要

1. 阶段 0 复位：T-106（部署烟测，M4 最后一票）在途——compose 形态烟测中**发现真 P1 产品缺陷**：
   - **D-106-1 [P1]**：`metadata.SnapshotChecksums` 的 `WHERE sha256 != ''` 不过滤 folder 节点的 64 零占位 sha——任何含显式目录（mkdir/C16）的实例 **export 全败**（fail-closed 拒绝导出）。T-96/T-103 未踩到因测试目录全隐式（上传推断）。
   - 根因定位：`internal/metadata/snapshot.go:84` SQL 一行修复（排除 64 零占位或按 path 尾斜杠排 folder 行）。
   - QA 正在因果证明（删 folder 节点 → export 转 PASS）——报告出来后立票派修（M4 内必修：FR-32 对 folder 实例破坏）。
2. T-107 移交微票 T-122/T-123 已收口（前轮）。

## 看板快照（本轮结束时）

- todo: 0 · doing: 1（T-106 收尾+缺陷定性）· done: 139 · blocked: 0

## 阻塞与风险

- **D-106-1（P1）待修**——M4 DoD 前必须闭环（备份面）。修复面窄：snapshot.go SQL + folder 实例 export 回归测试。

## 下轮计划

1. 收 T-106 报告 → D-106-1 立票派修（dev-go-core）→ 因果证明入票。
2. 修复复验（folder 实例 export/import 往返）→ T-106 done。
3. **M4 DoD 五条核查** → 向用户请示 tag m4-done。
