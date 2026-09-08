---
description: 查看团队当前状态（看板八态 + 在途 agent + 里程碑进度 + 兼容四问）
---

生成当前团队状态报告，只读不写：

1. 读 `BOARD.md`，按八态（DISCOVERY/SPECIFIED/READY/IMPLEMENTING/REVIEW/QA/DIFFERENTIAL/UAT/done，含 blocked）汇总票据（编号、标题、角色）——尾部日志与里程碑分节为实态源。
2. 读最新一份 `reports/iteration-*.md`，摘出阻塞、风险与 Compatibility 四问（surface/matched/divergence/unknown + Coverage）。
3. 读 `ROADMAP.md`，估算当前里程碑完成度（done / 总票数）。
4. 列出当前在途的后台 agent（如有）与 CI 双面最近结论（CircleCI/GH，一眼即可）。

输出为简洁的 markdown 状态卡。$ARGUMENTS
