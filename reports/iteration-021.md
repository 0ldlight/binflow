# 迭代报告 021 — Sprint 021

- 日期：2026-08-17 23:22（loop 触发 + T-8 review 落盘处理）
- 里程碑：M1 内核基座
- conductor：主会话

## 本轮动作摘要

1. 阶段 0 复位：T-8 review 已落盘（REQUEST_CHANGES）；T-10 修复磁盘可见（DSN PRAGMA + NumCPU 池成形）；T-9 双 reviewer 在途。
2. **T-8 review 裁决：REQUEST_CHANGES** → 回炉。reviewer 探针实证 4 项（报告 10.7KB，全部带复现）：
   - B1 多文档 YAML：doc2+ 静默丢弃，秘密扫描/strict 全部绕过（`---` 后放 admin_password 无报错）；
   - B2 `file::memory:` 击穿内存库防线（且下游 metadata 放行 file: 前缀，端到端贯通——重启数据蒸发）；
   - B3 `BINFLOW_ADMIN_PASSWORD` 小写变体静默失效 → seedAdmin 回落弱口令 password（「自认为加固的部署带弱口令启动」）；
   - M1 postgres DSN 校验错误原文回显口令（违反实现者自己划的红线）。
   - 修复单已派原 agent（含 reviewer 建议修法与补测清单）。
   - 裁决：m1 BINFLOW_DATA_DIR **保留**（docker -e 场景高频），doc.go 例外清单补第四个名字，architect §8 回写挂账。
3. B2 暴露的跨票事实：metadata store.go 放行 file: 前缀放大了 config 防线缺口——T-10 修复轮顺车自查该面（已在修复单附带上下文）。

## 看板快照（本轮结束时）

- todo: 10（T-11~T-20）
- doing: T-8（修复）、T-10（修复）
- review: T-9（双 reviewer）
- qa / blocked:（空）
- done: T-1~T-7, T-21~T-24

## 证据与测试结果

- T-8-review.md 落盘（探针全部实测，未污染仓库）。
- 三个基础包全部经历 review 打回：双 reviewer 制度连续两轮抓出单视角漏掉的真缺陷（T-10 数据丢失路径、T-8 弱口令路径）。流程成本换来的是 M1 内核的可靠性。

## 阻塞与风险

- 4 agent 槽满（2 修复 + 2 reviewer）；T-11 等 T-8+T-10 双双出修复。
- T-8 B2 与 T-10 的 file: 放行是同一防线两层——两边修复必须一致（config 白名单 + metadata 侧不放大），复审时交叉核对。

## 下轮计划

1. 收 T-8/T-10 修复 → 各自针对性复审（只验 blocker 项）→ done + commit。
2. 收 T-9 双 review → 裁决。
3. 三基础包 done 后：派 T-11（auth/audit）+ architect 勘误票（ADR-0007 DSN PRAGMA 机制、BINFLOW_DATA_DIR、T-9 两处契约偏离回写）。
