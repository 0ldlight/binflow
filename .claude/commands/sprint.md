---
description: 运行一轮完整的 AI 团队迭代（收尾→补给→派发→落盘→战报）
---

严格按照 `.claude/team/SPRINT-LOOP.md` 执行一轮迭代（阶段 0–5）。

要求：
- 阶段 0 先复位：读看板与最新迭代报告，检查在途 agent。
- 该并行的地方并行派发（一条消息多个 Agent 调用，后台运行）。
- 结束时落盘 BOARD.md 与 reports/iteration-NNN.md，并输出战报。
- 若 PRODUCT.md 还是空壳/模板，停下来请用户填写，不要空转。

$ARGUMENTS
