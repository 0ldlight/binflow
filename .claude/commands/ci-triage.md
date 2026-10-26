---
description: main 红灯分诊——CI 失败归因、flake/回归分类、立票与重跑一条链
---

对 main CI 失败执行分诊（$ARGUMENTS = run id 或 `latest`，省略=latest）：

1. **取证**：`gh run view <id> --log-failed` 拉失败腿——分类 build / lint / test；test 记录测试名、文件行号、失败断言。
2. **本地复现（干净基线）**：`git archive` 导出失败 commit 独立目录构建复跑（杜绝在途态混入——B-NB1 纪律）；在途 worktree 不得当证据。
3. **flake 启发式**：测试名含 Concurrent/race/stampede/timeout 类、本地单跑过而 CI 败、历史同测试闪过 → 竞态嫌疑，`go test -race -count=200` 压测定谳（BIN-113 先例）；压测复现 = 读码定根因再修，不无脑 retry。
4. **分类处置**：
   - **回归**（代码缺陷）：定位引入 merge（`git log`/bisect），立 Linear 票（BIN-xxx）+ conductor 直改或派票；红灯急救链走 PR 先例（#197/#198 cherry-pick → 保鲜）。
   - **flake**：`gh run rerun <id> --failed` + 立加固票（seam 确定性化/hook 驱动），并在迭代报告披露。
   - **环境/上游漂移**（依赖 advisory、镜像缺失）：如 BIN-100 npm audit 先例——修复+如实归因。
5. **产出**：分诊报告入 `reports/agents/`（归因链/证据命令/处置）；迭代报告 Watch items 登记 main 复绿确认项。
