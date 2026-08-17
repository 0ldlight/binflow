# 迭代报告 027 — Sprint 027

- 日期：2026-08-18 00:45（T-25 完成通知触发的收尾轮）
- 里程碑：M1 内核基座
- conductor：主会话

## 本轮动作摘要

1. **T-25（架构回写）收尾**：核验通过 → done，提交 `0990ae0`。14 处回写（GC 集合形 + mtime 硬约束、sentinel 全集、state.json 契约与 version 规则、DSN PRAGMA 机制、case_sensitive_like、事务边界 a 案、DATA_DIR 例外）+ ADR-0007 勘误 2 条；grep 旧措辞零残留；每处先核对实现代码后落笔。**架构文档与三基础包实现现已完全对齐。**
2. 在途：T-9 二轮修复（singleflight panic 死锁）、T-11（auth/audit 编码）。
3. 队列：T-9 二轮修复复验后 → T-9 done → T-12 派发（repo.Service）。

## 看板快照（本轮结束时）

- todo: 8（T-12~T-20）
- doing: T-9（二轮修复）、T-11
- review / qa / blocked:（空）
- done: T-1~T-8, T-10, T-21~T-25（done 15）

## 证据与测试结果

- T-25 核验输出见上方（GC 签名/mtime/PRAGMA 勘误/命名统一四处抽查 + 旧措辞零残留）。
- 本轮无代码产出。

## 阻塞与风险

- 无新增。T-12 等 T-9 二轮修复（最后一环）。

## 下轮计划

1. 收 T-9 二轮修复 → conductor 复验 panic 注入测试 → T-9 done + commit → **立即派 T-12**（附 ErrEngineClosed/ErrSessionPoisoned 已钉死契约 + FilterUnreferenced 对账用途 + blob-first 事务边界 a 案）。
2. 收 T-11 → review（单 reviewer）。
3. T-12+T-13 波次按票单推进。
