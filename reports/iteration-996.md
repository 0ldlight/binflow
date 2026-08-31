# Sprint 996 迭代报告 — T-361 收口（`b3dfcf5`，马拉松 3.5h）——**实现票全清**；T-374 派发

**日期**: 2026-08-31 04:4x
**上轮**: Sprint 995（等待轮）

## T-361 → done（develop=`b3dfcf5`，双远端）——M13 17/23

de-flake 全票：双档重定标（no-race 5s 生产数不动/race 45s）+ pollWindow 3x + **escape #3 settle 补齐（Round B 自擒第三族）**。**AC1**：隔离快照 Round C/D 背靠背双 exit=0（35 包×2 零 FAIL）。**AC2**：GHA 计费封锁下 CI 配方逐字本地复跑 **R6/R7/R8 三轮 222 passed exit=0** + .circleci e2e job 落盘 + Jenkins 对账（VM main 滞后已由 conductor sprint 974 同步）。**AC3**：escape 台账 20 命中全注释。conductor 复验 build/BigTree/lint 0。

## 里程碑态：实现票全清

17/23——T-358~T-371+T-378 全部 done。剩余四票全为收口波：T-373（QA 中期回归，在途）→ T-374（docs 尾巴，本轮派发）→ T-376（release+UAT 首跑）→ T-377（终验）→ m13-done。

## 派发

- **T-374**（tech-writer）：console-m8 侧栏清单 15 页对齐 + T-372 推翻落痕核查 + npm.md 尾斜杠注记（实测后落笔）。

## 状态

M13：**17/23**。在途 ×2（T-373/T-374）。HEAD[develop]=`b3dfcf5`。
