# Sprint 841 迭代报告 — CI 双跑模式定型（每次合并一绿一红）；指纹修复后 deploy 持续成功

**日期**: 2026-08-28 14:26
**上轮**: Sprint 840（分歧整合）

## CI 定性

- **PR #14（您的指纹修改）→ build #15 全绿**（第二次成功部署，指纹修复生效）；#16 同签名红（重复 run 的 publickey 拒绝）。
- **双跑模式定型**：#13/#14、#15/#16 两轮均「一绿一红」——每次合并触发两条 pipeline（推定 GitHub 侧双 webhook：GitHub App + 传统 webhook 各一）。绿跑为权威（部署成功+烟测过）；红跑仅噪音但会持续告警。
- **建议用户一次性排查**：GitHub 仓库 Settings → Webhooks——若见两条 CircleCI 注册（一条 GitHub App、一条 webhook service），删传统 webhook 留 App 即可绝双跑。

## 状态

M11 DONE。在途 ×0。HEAD[develop]=`b4f2030`；main=PR#14 态（UAT 二度部署成功）。M12 待指令。
