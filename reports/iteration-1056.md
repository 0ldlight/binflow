# Sprint 1056 迭代报告 — 用户 license 请求兑现（企业版 365 天）；T-384 复活；10 轮叠发吸收

**日期**: 2026-08-31 16:0x（限额 16:04 重置后首轮；14:20~16:04 配额窗内 10 轮 cron 叠发被吸收）
**上轮**: Sprint 1055（K56 收口）

## 用户请求：企业版 license 已生成并活体验证 ✓

- **签发**：`bin/bf license issue --licensee binflow --tier enterprise --days 365` → `/tmp/binflow-enterprise.lic`（439B 两段式 ed25519 文档；license id `8aaf3822-7a4d-4677-b704-3087211ec61c`；kid bf-lic-2026）。
- **验签**：`bf license inspect` — verification OK + **stock binary 内嵌公钥 accepted**。
- **活体**：scratch 实例 REST 安装（POST /api/system/license）→ 回显 tier=enterprise/licenseee=binflow/expires 2027-08-31/daysToExpiry 364 → **/api/v1/addons 19/19 槽 enabled**（tier-wide 全开）。清理完毕。
- **用法**：装到任意实例（REST 注入或放 BINFLOW_HOME）；有效期至 2027-08-31。

## 配额窗损失处置

- **T-384**：击落于 a11y-sweep 前（m9 串行 6/6 已过）→ SendMessage 复活续跑。

## 状态

M14：**7/22**（+K56）。在途 ×1（T-384 复活）。HEAD[develop]=`79c6db1`。
