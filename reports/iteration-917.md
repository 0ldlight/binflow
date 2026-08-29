# Sprint 917 迭代报告 — T-347 收口（architecture 回写，PR #40）；锁争用根因定位；M12 21/25

**日期**: 2026-08-29 19:15
**上轮**: Sprint 916（18:47——报告延迟随本票合入）

## T-347 → done（PR #40，develop=`f760d73`）

§15.4 as-built 整节重写（MPU Artifactory 形/操作域三节/§15.4.1 终态）+ §23 可观测整节（replay 实名指标族/e2e 权威信号）；make docs 4.20MB 零断链；事实全对照 HEAD 代码。遗留：ADR 文字层两处陈旧（DECISIONS 维护窗）+ Q3 翻转点就绪。

## 锁争用根因定位（回溯 916 轮误判）

并非 VS Code——**持久 shell CWD 漂移到 web/**（跨 Bash 调用保持）导致部分命令在子目录执行 + 在途 agent 高频 `git status` 闸门短暂持锁的叠加。修正：全命令显式 `cd` 前缀 + rm-commit 竞速环（首轮即成）。上轮「VS Code 持锁」判断撤回。

## 状态

M12：**21/25**。在途 ×1（T-352+353 web 聚票）。HEAD[develop]=`f760d73`。剩：T-351 中期回归/T-355 release/T-356 终验（+T-352/353 在途）。
