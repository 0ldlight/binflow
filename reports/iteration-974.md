# Sprint 974 迭代报告 — 等待轮 + conductor 处置 VM 镜像 main 滞后（T-361 §5.4 发现）

**日期**: 2026-08-31 01:5x
**上轮**: Sprint 973（等待轮）

## T-361 中期报告要点（状态 doing，三处待填——承证轮在跑）

- 机制定案：**双档重定标非 skip**（race 档 45s/5s 生产数不动 + deb pollWindow 3x）——论证四条在案；escape 台账 14 命中/7 文件（FR-121.3 可审计）。
- **发现①（conductor 已处置）**：VM 镜像 main 停在 95a8f9ab（M11 时代）——nightly 九连绿认证的是旧树。本轮 `git push vm origin/main:main` 已同步至 9263439（GitHub main，PR #44）——下次 nightly 即认证当前树。
- **发现②（用户动作）**：GHA 计费封锁——2026-08-28 10:15 起全部 main run 启动即红（"recent account payments have failed or spending limit…"，12+ 连续 run 取证在案）。修复前 GHA 三 job 不跑；e2e 基线以 CircleCI 镜像 job + 本地三连复跑双腿落。
- .circleci e2e job 已新增（main-only 过滤 + build 工件复用——随下次 main 合并首跑）。

## 在途 ×2

- **T-361**：AC1 双轮承证 + e2e 三连在跑（报告待填段）。
- **T-368**：第二轮验证在跑。

## 状态

M13：**11/23**。在途 ×2。HEAD[develop]=`40868f8`。
