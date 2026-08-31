# Sprint 965 迭代报告 — T-366 收口（`a00f029`）+ 死键修复（`7afa947`）；T-367 存活续跑

**日期**: 2026-08-30 21:34
**上轮**: Sprint 964（复活轮）

## T-366 → done（develop=`a00f029` + `7afa947`，双远端）——M13 9/23

- **FE 面**：治理分组第七页（Dialog 新建/编辑 + Drawer 投递记录——**对齐规格前两块落地实践**）+ 锚册 v1.16（47 锚）+ ledger PASS + axe 双主题 0 + 352KB 预算维持 + readonly 臂零写反断言 + 契约 diff=0。
- **消费者腿 8/8 全绿**（真 pro 实例 + 脚本接收器）：七字段信封 + HMAC 线上字节逐字验签 + 固定间隔重试（≥9s×2）+ 5 次耗尽死信 + 30s 挂死预算 + SSRF 默认拒回环 + Dialog→Drawer 集成。
- **自擒 conductor 死键**：`webhook.allow_private_target` 装载层未接线（T-362 三处漏一）——**conductor 已修**：load.go raw 段 + 默认 false + 单下划线 env 别名 + webhook_test.go（双形态/显式 false/env 覆写/strict schema），config 全包 39s 绿 + lint 0（`7afa947`）。
- 遗留登记：outbox 行级 REST 面与 Replay REST 候裁（与 T-364 §5-3 并案）；Jenkins dogfood 腿候旋钮修复后平移。

## T-367 状态修正

上轮判「疑似击落」**误报**——transcript mtime 不可靠（T-366 同款：transcript 停 20:20 但 21:0x 仍在交付）。`internal/httpapi/repositories.go` 21:10 有写入（charts_base_url REST 回显面），agent 存活推进中。

## 状态

M13：**9/23**。在途 ×1（T-367）。HEAD[develop]=`7afa947`。
