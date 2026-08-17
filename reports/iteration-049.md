# 迭代报告 049 — Sprint 049

- 日期：2026-08-18 04:45（T-14 完成通知触发的收尾轮）
- 里程碑：M1 内核基座
- conductor：主会话

## 本轮动作摘要

1. **T-14（httpapi 核心）编码收尾**：conductor 复现通过 → 提交 `f6416a8`，转 review（单 reviewer：认证分层/路由安全混合形态探针/中间件顺序/停机语义）。
   - 关键设计：产品路径**绕开 ServeMux** 走 EscapedPath 手写分发树（根治 mux cleanPath 对 `..` 的 3xx 归一化陷阱——上上轮探针的结论落地）；探针精确匹配仍用 mux。
   - logFields 修掉「principal 只能向内传导致日志记不到 user」的真实缺陷。
   - curl 冒烟：ping/version/匿名 401/admin 200/E-26 五路径/点段非 3xx/SIGTERM exit 0。
2. 遗留三项待 reviewer/architect：RepoLookup 缝（匿名读需先解析 repo key，metadata.Get 绕过已认证要求——安全面待评估）；T-16 装配要点；bytes_in 声明值语义。
3. T-15（兼容 REST）与 T-14 同包串行——等 review 出结果即派。

## 看板快照（本轮结束时）

- todo: 5（T-15~T-19）
- doing:（空）
- review: T-14
- qa / blocked:（空）
- done: 23/27

## 证据与测试结果

- T-14 复现输出见上方（race 4.7s/1.3s 绿、lint 0、zero-cgo ok、cleanPath 注释落点）。

## 阻塞与风险

- T-15 被 T-14 review 阻塞（同包）。reviewer ~30-50 分钟预期。
- 额度：窗口（02:26 起）已 2h20m，T-14 是 273 调用大票——T-15 派发可能撞上限，撞了由 loop 下窗口接力（已验证模式）。

## 下轮计划

1. 收 T-14 review → 裁决 → done/修复 → **派 T-15**（兼容 REST，v1.3 口径袋已备：form token/双路由改密/PUT 建用户/错误体三分层/revoke 哨兵）。
2. RepoLookup 缝安全面意见 → 并入 T-15 派单或 architect 小票。
