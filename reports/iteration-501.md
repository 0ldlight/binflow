# Sprint 501 迭代报告 — 第六次熔断：M9 规划半损即恢复（合并轮，09:38~11:05 共 10 次堆积触发）

**日期**: 2026-08-24 11:06
**上轮**: Sprint 500

## 事件与恢复

- **09:38** M9 规划 workflow 三路（PM/architect/tech-lead）被 429 击落（第六次；本窗终验+fix-forward+规划启动的总耗）。**reverse 抢在熔断前完成**：gap-endpoints.md 落盘（14 端点/5 流程，高 21/中 13/低 7）。
- **11:05** 窗过即 **resume**（同 runId）：reverse 走缓存，PM/architect/tech-lead 三路重跑。

## reverse 关键发现（将改变 M9 设计）

1. **Artifactory wire 无 enabled 布尔**——status 枚举（invited/enabled/disabled/locked）；T-208 的 Enabled *bool 回显形态需 ADR 裁（对齐 status vs 自有扩展）。
2. DELETE users 级联全链（剥 ACE→删本体→Access 级联→幂等 404）。
3. 组成员：?includeUsers=true + PUT 全量替换/POST 增量的不对称写；N+1 的对齐答案 = 一次返回全员各带 groups。
4. 权限列表**无过滤面**（v2 对非 admin 开放 name+uri）——BinFlow 过滤列表确认为自有优化。
5. 仓库用量：列表无用量字段，走 storageinfo 式独立端点（缓存快照/冷 503/POST calculate）——~170 扇出的对齐形态。
6. 七条缺位建议活体验证（VM 上 Artifactory 栈可复用）。

## 下轮计划

规划落地 → 审四件套 → 录板 → 波 1（宽 ≤2）。自动 push 生效中。
