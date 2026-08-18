# 迭代报告 116 — Sprint 116

- 日期：2026-08-19 00:40（T-39 review 裁决轮 + 用户决策「文档服务使用 Docusaurus」处理）
- 里程碑：M2
- conductor：主会话

## 用户新决策（追加）

**文档中心（docs/user/，M5 交付）采用 Docusaurus**——已派 T-50（architect）：ADR-0011 选型（候选对比 + 与 M4 控制台同栈 React 的理由）；两个架构接点裁决（交付形态：独立静态站 vs go:embed 自带 /docs 路由——倾向后者符合单二进制差异化，待 architect 权衡；源文件与站点配置分离——writer 继续只写 Markdown）。影响面：T-46 docker 文档将成为 Docusaurus 首批页面；M5 需加脚手架票。

## 原动作摘要（T-39 裁决）

1. **T-39 正确性 review：REQUEST_CHANGES（1 blocker）**→ 修复单已派：
   - B1「假失败真发布」：重复 descriptor digest 撞 docker_refs PK → 500 但 manifest 已生效（GET by-tag 200 逐位一致）——违反「任何失败先于状态变更」不变量；修法两层（adapter 去重主修 + store INSERT OR IGNORE 纵深）+ fake PK 语义对齐（逃逸根因）+ 4 行重复态测试。
   - 正面面极广：非规范化 JSON roundtrip 逐位一致、20 路并发 PUT 同 tag 终态一致、PUT+DELETE+GET 30/30 无漂移、校验链①-⑤全对。
   - 顺手：并发 PUT 转正式回归（钉住性质而非 SQLite 偶然）；schemaVersion 文案。
2. **R3 终审意见在案**（架构 APPROVE 附）：透传+结构判读为最终口径——T-39 修复后随 T-40 派单同批落 architect 回写小票。
3. 在途：T-39 修复。

## 看板快照（本轮结束时）

- todo: 4 · doing: T-39（修复）· review: 0 · done: 49 · blocked: 0

## 阻塞与风险

- T-40 等 T-39 修复终裁（同包串行，最后一环）。

## 下轮计划

1. 收 T-39 修复 → 针对性复审（4 重复态测试 + INSERT OR IGNORE 纵深）→ done → **T-40 派发 + R3 回写小票双发**。
2. T-40 → T-43/T-44 QA → T-45 烟测 → M2 DoD。
