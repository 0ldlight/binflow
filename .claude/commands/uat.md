---
description: UAT 状态（健康 + 版本 + 最近十腿矩阵/差分结论）
---

生成 UAT 环境状态报告，只读不写：

1. `curl -u admin:<凭据> <UAT>/binflow/api/system/version` → 版本戳（uat.<sha>，与 main HEAD 对照新鲜度）。
2. healthz + /binflow/docs/ 200 烟测。
3. 最近一次 CircleCI `protocol_leg ×10` 各腿结论（gh api commit status）。
4. 最近差分结论：`reports/compatibility/` 最新文件的 Summary（一致率/新差异）。
5. UAT 数据面漂移信号（nightly 漂移面最近一轮结论，如有）。

UAT 端点与凭据从 CI 环境口径（uat.binflow.org / 52.79.109.153:8080，admin）。输出简洁 markdown。$ARGUMENTS
