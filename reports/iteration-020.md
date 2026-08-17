# 迭代报告 020 — Sprint 020

- 日期：2026-08-17 23:55（T-10 架构 review 回报触发的裁决轮）
- 里程碑：M1 内核基座
- conductor：主会话

## 本轮动作摘要

1. **T-10 双 review 合并裁决：REQUEST_CHANGES**（正确性 APPROVE + 架构 REQUEST_CHANGES）：
   - **B1（blocker，数据丢失路径）**：ListByPrefix/DeleteByPrefix 的 LIKE 大小写不敏感——reviewer 探针实证 `DeleteByPrefix("LIB")` 静默删掉 `lib/a.jar`。修法：PRAGMA case_sensitive_like=ON + 大小写/通配符转义测试。
   - **B2（blocker）**：MaxOpenConns=1 + PRAGMA per-connection 耦合。两位 reviewer 一致；采纳「池开 NumCPU + PRAGMA 挪 DSN」方案（同车消除 FK 静默失效与未来放宽池的隐患；WAL 为库级可留 db.Exec）。
   - → T-10 回 doing 修复（原 agent 续跑保留上下文）；修复后仅针对性复审两 blocker。
   - 6 minor 不阻塞（含 M1 Store 缺事务组合方法、AuditStore 缺 since 过滤——记 T-11/T-12 派单参考）。
2. T-9 双 reviewer、T-8 单 reviewer 继续在途。
3. ADR-0007 勘误需求升级：B2 修复方案本身就改变机制（DSN PRAGMA），architect 勘误时一并写入；挂 T-10 修复验证后。

## 看板快照（本轮结束时）

- todo: 10（T-11~T-20）
- doing: T-10（修复轮）
- review: T-9（双 reviewer）、T-8（单 reviewer）
- qa:（空）
- done: T-1~T-7, T-21~T-24
- blocked:（空）

## 证据与测试结果

- T-10-review-arch.md 落盘（B1 探针实证 + B2 机制分析 + 6 minor + DDL 逐表核对通过 + 包边界/依赖白名单/clean-room 全过）。
- 双 reviewer 机制兑现价值：正确性 APPROVE 而架构抓出静默数据丢失——单 reviewer 会漏。

## 阻塞与风险

- T-11 仍等 T-10 出修复+复审；auth 的 TokenStore 恰好依赖 metadata 修复面（tokenStore.Touch minor 也在同文件），顺车处理窗口。
- 修复引入 case_sensitive_like 后需回归 LikePrefix 相关全部测试（LIKE 转义假设）。

## 下轮计划

1. 收 T-10 修复 → 针对性复审（两 blocker 逐条验证）→ 通过则 T-10 done + commit + 立即派 T-11 与 architect 勘误票（ADR-0007 + BINFLOW_DATA_DIR 等 T-8 遗留）。
2. 收 T-9 双 review、T-8 review。
3. minor 项按票分配：TOCTOU 契约 → T-13 派单附注；AuditStore since → T-12 参考；双进程首启竞态 → 技术债台账（M4）。
