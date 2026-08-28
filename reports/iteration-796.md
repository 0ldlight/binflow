# Sprint 796 迭代报告 — CI 触发机制定性（仅 PR 合并可触发）；T-325 在途

**日期**: 2026-08-28 01:48
**上轮**: Sprint 795（01:45）

## CI 触发机制定性

- `8305e67` 直推 main **25 分钟无 build**；仅有的 #3/#5 都是用户 GitHub PR 合并触发（why=github_app）。
- API 手动触发不可用：v2 POST 因 slug 解析 404/500（同 GET 的老问题），v1.1 POST 500。
- **结论**：本项目当前只有 PR 合并能触发 CI。用户侧二选一：① GitHub 开 develop→main PR 并合并（既带最新代码又触发——推荐，含 M26 修复+T-328 文档）；② Project Settings → Triggers 开启 main 的 push 触发（此后我的直推也能触发）。

## T-325（部署矩阵）在途

13 文件在写（deploy/charts/.circleci 域）。

## 状态

M11：24/32。在途 ×1。HEAD[develop]=`cf396bf`；main=`8305e67`（待 PR 或触发器启用后才有下一跑）。
