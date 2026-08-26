# Sprint 675 迭代报告 — T-294 评审 REQUEST_CHANGES（1 blocker），修复轮在途

**日期**: 2026-08-26 11:05
**上轮**: Sprint 674

## 评审结论

- **通过面**：auth 裸 token 臂（臂序互斥/垃圾值 401 不降匿名/Verify 无比较侧信道）；D-1~D-7 七偏差**独立实证全部成立**（评审员自建最小 registry + cargo 1.98 重测五形态——200+`"errors":[]` 确使 cargo exit 101）；baseline 无 cargo 行「零翻格」与 main.go 点状挂载声明均核实；clean-room 合规
- **blocker B1**：index.go 索引整文件重写丢更新竞争（List→组装→Put 无同步；评审探针实锤 8 并发 publish 后索引仅剩 2 行，~1/15 概率复现，违反 AC3 一行一版本不变量）——**正是「现有测试全顺序」的漏网形态**
- M1（tab 畸形值 401→静默匿名漂移）+ M2（e2e 注释与立论相反）顺手清；其余 7 条 non-blocking 维持登记

## 动作

修复轮已派（11:05）：B1 per-(repo,crate) 互斥 + **必补并发回归腿**（≥8 goroutine 断言行数==版本数）+ M1/M2；自测门槛含 -race、不变量、真实 cargo L-r3 重跑。

## 状态

M10：16/21。在途 ×1（T-294 修复轮）。R-1 规格修订（D-1 落册 cargo.md）归 conductor 直改或 T-296。

## 下轮计划

修复轮回收 → conductor 复验（并发腿 + 门 + HEAD-build）→ 提交 → **B8 全清** → B9（T-295 部署接线 ∥ T-296 文档五项）。
