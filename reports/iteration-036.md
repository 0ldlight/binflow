# 迭代报告 036 — Sprint 036

- 日期：2026-08-18 01:15（T-12 review + T-11 修复双通知触发的处理轮）
- 里程碑：M1 内核基座
- conductor：主会话

## 本轮动作摘要

1. **T-12 review 裁决：REQUEST_CHANGES**（2 blocker 同根因——尾斜杠前缀进 likePrefix 变 "d//%"：B1 pruneEmptyParents 存活检查失效致误删目录行（数据丢失级）、B2 List("d") vs List("d/") 双形态不一致违反契约）。**事务边界被证实过硬**（blob-first/FK 兜底/回滚注入/24 并发探针全过）。修复单已派原 agent。
   - 范围外登记：`%2e` 解码责任在 httpapi——记入 T-14 派单要点（先 decode 再进 Service，否则 dot-segment 防御可被 URL 编码绕过）。
2. **T-11 修复复审通过 → done**，提交 `c681dda`：
   - B-1 哨兵导出别名（TestSentinelAliases 钉死 errors.Is 双拼法）；
   - B-2 folder 语义（matchStart 仅 folder；over-grant 探针三行全转 false）；
   - B-3 fail-closed 三测试 + 真实触发日志卫生断言；
   - 顺手 4 major（Redact camelCase、ChangePassword 顺序、Touch 节流、空 path 语义）。
   - 29 测试/94 子用例，race 23s 绿，lint 0。
   - **新契约待 architect 回写 §3.4**（Can 尾斜杠=folder 约定）——与 T-12 修复同批合并成一张小票。
3. go.sum 首次入库（随 T-11 提交，补上 reviewer 指出的 untracked 缺口）。

## 看板快照（本轮结束时）

- todo: 8（T-13~T-20）
- doing: T-12（修复）
- review / qa:（空）
- done: 17（+T-11）
- blocked:（空）

## 证据与测试结果

- T-11 针对性复审 6 测试全 PASS（见上方输出）。
- T-12-review.md 落盘（探针两形态复现 + 事务边界正面取证）。

## 阻塞与风险

- T-13 等 T-12 修复（最后一环）。T-12 修复 + architect 回写小票（§3.4 Can 契约）同批后即可双发。

## 下轮计划

1. 收 T-12 修复 → 针对性复审（prune 回归两形态 + List 一致性）→ done → **立即派 T-13 + architect 回写小票（B-2 Can 契约 + gosec 豁免收窄复核）**。
2. T-14 派单要点袋已备：%2e 先 decode、匿名/管理面分层、revoke 哨兵已导出。
