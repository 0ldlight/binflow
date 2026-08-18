# 迭代报告 118 — Sprint 118

- 日期：2026-08-19 00:15（T-50 完成触发的收尾轮）
- 里程碑：M2
- conductor：主会话

## 本轮动作摘要

1. **T-50（ADR-0011 Docusaurus）收尾**：核验通过 → done，提交 `5e24bcc`。
   - 交付形态裁决：**go:embed 挂 /binflow/docs（统一前缀、匿名可读）**——离线/air-gapped 自带文档是差异化，独立托管用户自办不双轨；
   - 工作流：docs/user 纯 Markdown 源 + docs-site 聚合构建分离，writer 零 Docusaurus 认知负担；
   - 体积预算 5~15MB，M5 check-size 把关 + 超限 fallback；
   - 架构四处增量；M5 脚手架票预告（类 T-7 先行）。
2. 在途：T-39 修复（manifest 假发布 blocker）。

## 看板快照（本轮结束时）

- todo: 4 · doing: T-39（修复）· done: 50 · blocked: 0

## 阻塞与风险

- 无。

## 下轮计划

1. 收 T-39 修复 → 针对性复审 → done → **T-40 + R3 回写小票双发**。
2. T-40 → T-43/T-44 QA → T-45 烟测 → M2 DoD → tag 请用户确认。
