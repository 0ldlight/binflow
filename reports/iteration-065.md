# 迭代报告 065 — Sprint 065

- 日期：2026-08-18 08:50（T-28 修复回报触发的收尾轮）
- 里程碑：M1 内核基座
- conductor：主会话

## 本轮动作摘要

1. **T-28（D2/D3 修复）收尾**：核验通过 → 合入 `1a11cd2`：
   - routeAuth 分级表补 admin-only 位：GET repositories 列表/单仓、v1-stats、v1-health 四读面 + token 签发；
   - 顺手修掉 revoke 403 错误体漏成 errors[] 的不一致（routeAuth 新增 oauth 位，token 族统一 OAuth 形）；
   - 不误伤验证：ping/version 匿名 200、探针 200（compose 健康检查不受影响）；
   - 旧「非 admin 列仓 200」断言按 FR-5-AC8 更正为 403（测试与规格对齐）；自跑场景 6 全套 22/22 PASS。
2. **T-18 回归轮派发**（仅场景 6：D2/D3 矩阵 + 不误伤面 + C20-C27 链 + 日志检查）。
3. v1/health admin-only 按 C28a 口径定案（备注：产品若要放开只需去一个标志位）。

## 看板快照（本轮结束时）

- todo: 1（T-19）· doing: 0 · qa: T-18（回归中）· done: 29/31 · blocked: 0

## 证据与测试结果

- T-28 复现输出见上方（两矩阵测试 PASS、race 15.5s 绿、lint 0）。

## 阻塞与风险

- 无。回归绿后 T-18 done → T-19 终验。

## 下轮计划

1. 收 T-18 回归 → done → 派 T-19（存储完整性/性能/持久化终验 + README 复跑 + DoD 结论）。
2. T-19 done → M1 DoD 五条核查 → 请用户确认 tag m1-done。
