---
description: 运行一轮完整的 AI 团队迭代（收尾→补给→派发→落盘→战报）
---

严格按照 `.claude/team/SPRINT-LOOP.md`（v3，六步闭环）执行一轮迭代。

要求：
- 阶段 0 先复位：读最新迭代报告与在途任务（Linear；未就绪期间读 `reports/agents/T-*.md`），检查在途 agent。
- 该并行的地方并行派发（一条消息多个 Agent 调用，后台运行）。
- 结束时只落盘 reports/iteration-NNN.md 并输出战报（BOARD.md 已冻结只读，不再写入；新票以 `reports/agents/T-<id>.md` 承载待 Linear 回填）。
- 若 PRODUCT.md 还是空壳/模板，停下来请用户填写，不要空转。

$ARGUMENTS
