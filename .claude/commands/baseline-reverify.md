---
description: 基准复验队列——7.161.26 新基准下按 P0 优先管理 matrix 行复验波次
---

管理兼容基准 7.161.26 的复验队列（$ARGUMENTS = 波次大小或域过滤，省略=出队报表）：

1. **前置**：跑 `/preflight`——D3 凭据不在位则本命令只出报表不派复验（ BLOCKED 如实标注）。
2. **队列构成**：matrix 全部既有行均按旧基准 7.161.15 认证（`reference.row_certification_note`）——按优先级排序：P0 compatible（翻绿结论最可能被基准差异推翻的）> P0 partial/absent > P1 > P2；同优先级内按 `last_difftest` 最旧优先。
3. **波次派发**：每波 N 行（默认 10–15）交 `differential-qa-engineer` 走 diff-test 流程；复验结论落行级 note 标注 `re-verified@7.161.26` + 差分报告引用；发现分歧→按四分类立台账条目，**候裁态禁自行翻绿**。
4. **覆盖跟踪**：报表输出——已复验/待复验行数与占比（分 P0/P1/P2）、本波结论（SAME/DIVERGENT/无差分输入）、复验中发现的新分歧清单。
5. **联动**：全部 P0 复验完成 = 基准切换正式收口的必要条件之一；进度并入月度 `/ruling-pack` 与迭代报告四问口径说明。
