# T-632 — recurrent-failure 熔断缺失（坏凭据反复尝试 A 403 / B 恒 401）

| 项 | 值 |
|---|---|
| 票据 | T-632（2026-10-28，L042 wave-1 立） |
| 类型/状态 | divergent（认证安全面）；**SPECIFIED**（先裁后修） |
| 派发建议 | dev-go-core（internal/auth）；security-auditor 复核口径 |
| 优先级建议 | P3（对齐项兼具安全增强性质；已实测可被差分工装触发） |
| 输入 | L042 run3：跨轮同假用户名坏 Basic → A 侧 /v2/token 403 |
| 证据 | reports/compatibility/L042-wave1-docker-remote-replay.md 新发现 F-3 |

## 行为差异（双端活体）

- 参照（7.161.26）：同一用户名反复坏 Basic 凭据 → **403** `This request is blocked due to recurrent request failures, please try again in 4 second(s)`（时间窗指数/固定未表征）。
- BinFlow：恒 401 `Bad Credentials`，无熔断。

## 裁定问题（先裁后修）

- 对齐参照：实现等价熔断（响应文案/窗口参数需活体采样后规格化）；
- 或登记 known-divergence：BinFlow 侧作为待补安全能力（挂 security-auditor 周期审计面）。

## 验收标准

1. 裁定后按所选方向落地；negative test（Security 票硬门）覆盖熔断臂。
2. 差分工装纪律固化：坏凭据臂假用户名按轮唯一（tools/difftest/v2/cases 已按此实现，防再踩）。
