# Sprint 612 迭代报告 — T-279 关账（license 核心落地），T-282 派发（通知轮）

**日期**: 2026-08-25 13:45
**上轮**: Sprint 611；其间 T-279 完成 → conductor 复验（build/vet/race/lint 0 + **不变量闸门维持 0 deviations**）→ 提交 `90929f9` → **T-282 派发**。

## T-279 亮点

- **内嵌公钥 bootstrap 对私钥即毁**——stock 二进制恒 community 地板（不变量 1 的密钥学保证）；首发签发时 T-281 keygen 换权威对
- D7 原子性：单事务替换——失败安装绝不扰动在位状态
- NFR-S53 fail-safe：DB 行被外部篡改 → 重启 readyz 200 + WARN + community
- redact 实证：serve 日志零 licensee/文档碎片
- 四处 PRD↔ADR 分歧按 T-293 登记口径取 ADR

## 阶段 0

在途 ×1：**T-282**（addon 注册表——五核心 retro-fit community 地板 + 槽位 ≥10 + GET /api/v1/addons）。T-281（签发 CLI）待宽度空位穿插。HEAD=`02573d3`。M10：**3/21 done**。

## 下轮计划

T-282 收口 → T-281（deps 已绿）∥ T-280（NuGet 规格）→ B3 门控织入。
