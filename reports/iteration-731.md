# Sprint 731 迭代报告 — T-304 关账（`ac15110`）；两项待用户裁决上板

**日期**: 2026-08-26 22:52
**上轮**: Sprint 730

## T-304 关账（回头看裁决票）

- **30 项复核**：维持 10（附出处）/ 改回-既有票 5 / 改回-新票候选 4 / 已裁 9 / **待用户裁决 2**
- **CG-2 十一类失败形态锚定**（T-316 硬前置交付）：权限/重复类 = 401/403 + errors 信封；IOException 族 = 200 + 错误串入 `warnings.other`（模型无顶层 errors 键——与 20:55 终值吻合）；无 409 臂；帧前缀畸形 500 穿透
- Q8 六项归位 + 两翻转路由核对 ✓；PM 转交 PRD v1.2 勘误三处待执行
- gitflow 四航：feature/T-304-lookback → develop `ac15110`

## 待用户裁决（T-304 正确上交，不阻塞当前流）

1. **NuGet 对齐 bundle**：v2 全面 / remote search 上游代理 / service index 动态解析 / virtual 合并（M11 无承载票，批准则立新票）
2. **MPU 面形状**：中继 vs presigned 直传（翻需新 ADR）+ create 范围 + Artifactory 实测形态束（全 POST+QueryParam / complete?sha1→202 / status 异步任务模型）

→ 将在 T-316/T-323 派发前（B8 前）正式上会。

## 状态

M11：5/32。在途 ×1（T-305 认证 BE）。HEAD[develop]=`ac15110`。
