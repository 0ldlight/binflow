---
description: P0/P1/P2 兼容缺口清单 + 优先序建议（Gap-Driven Planning 输入）
---

生成兼容缺口优先级报告，只读不写：

1. 读 `docs/compatibility/matrix.yaml`（或迁移源 `docs/reverse/rest-compat-matrix.md`）中 ❌ 缺位与 ◐ 部分兼容行，按域（D01~D14）归类。
2. 交叉读 `docs/compatibility/known-divergence.yaml`（BUG 类优先）与 BOARD 在途票（已在做的剔除）。
3. 对每个缺口按 **Priority Score = 业务影响 + 兼容影响 + 客户端影响 + 回归风险 + 架构依赖** 打分（1-5 各维，附一句话理由）。
4. 按 SPRINT-LOOP 硬约束排序：P0 兼容缺口/P0 安全/P0 数据完整性 → P1 兼容/P1 客户端失败/P1 存储正确性 → P2 增强。
5. 输出 Top 10 建议票面（T-号候补、角色、area、一句话 AC），供 tech-lead 拆票引用。

$ARGUMENTS
