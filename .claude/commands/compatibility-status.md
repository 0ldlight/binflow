---
description: 兼容面状态快照（matrix 四态总账 + Coverage + 四问）
---

生成兼容工程状态报告，只读不写：

1. 读 `docs/compatibility/matrix.yaml`（若不存在，读 `docs/reverse/rest-compat-matrix.md` 并注明「matrix.yaml 迁移未落」），统计四态（✅/◐/❌/⛔ + 超集）与置信度分布。
2. 用 `tools/difftest/score.sh`（若已就位）计算 Overall / P0 / P1 / P2 Score；脚本未就位则手算折算值并注明。
3. 读 `docs/compatibility/known-divergence.yaml`，按四分类（BUG/INTENTIONAL/UNSUPPORTED/UNKNOWN）统计开放差异。
4. 对照最新 `reports/iteration-*.md` 的四问，输出本轮对照：surface / matched / divergence / unknown + new regressions / fixed divergences / new divergences。
5. 检查 Artifactory 参照实例健康（可达性探针）；不可达则标注「差分降级 golden-only 模式」。

输出为简洁 markdown 快照。$ARGUMENTS
