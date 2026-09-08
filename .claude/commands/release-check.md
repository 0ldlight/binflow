---
description: 发布前核对（DoD 十二条 + 完成定义 + 发布红线清单）
---

对指定票/里程碑（$ARGUMENTS；省略=当前里程碑）执行发布前核对，只读不写：

1. **DoD 十二条**逐条核对该范围所有票：code complete / tests added / tests passed / lint passed / review approved（六关键域双审）/ compatibility contract satisfied / differential passed（兼容域）/ security check（涉安 negative test）/ performance regression checked（性能敏感路径）/ observability present / UAT validated / report written（15 字段证据）。
2. **完成定义**对照（ROADMAP 尾部）：P0=0 / P1=0 / P2≤阈值 / Coverage≥目标 / 回归=0 / 关键安全=0 / 数据完整性 PASS / 性能基线 PASS / 升级回滚 PASS / UAT PASS。
3. **发布红线**清单确认：对外发布（镜像/Chart/二进制/release）**必须先经用户确认**——本命令只核对不执行。
4. 输出：逐条 ✅/❌/N/A 表 + 未满足项的补救票建议。

所有结论必须引用实际证据（报告路径/命令输出），禁止「看起来没问题」。
