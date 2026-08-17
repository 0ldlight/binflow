---
description: 查看团队当前状态（看板 + 在途 agent + 里程碑进度）
---

生成当前团队状态报告，只读不写：

1. 读 `BOARD.md`，按 todo/doing/review/qa/done/blocked 汇总票据（编号、标题、角色）。
2. 读最新一份 `reports/iteration-*.md`，摘出阻塞与风险。
3. 读 `ROADMAP.md`，估算当前里程碑完成度（done / 总票数）。
4. 列出当前在途的后台 agent（如有）。

输出为简洁的 markdown 状态卡。$ARGUMENTS
