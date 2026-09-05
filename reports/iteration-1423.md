# Sprint 1423 迭代报告 — T-478 收编（443/ACME 落地，用户前置两项）+ 双 lane 在途

**日期**: 2026-09-06 01:0x
**上轮**: Sprint 1422（等待轮③）

## T-478 → done（`3bb71d4e`，5 文件 +337/−19）

Caddy 反代终结（ACME 全 daemon 内）+ deploy_uat proxy 步骤 + 双面基地址默认 `https://uat.binflow.org`（8080 回退 env）。门全绿（caddy validate×2 + 行为级 + cc process 0）。`protocol-matrix.sh` 注释性变更随 T-479 收编（同文件在途防误扫）。

**⚠️ 用户前置两项**（实切换前）：
1. **DNS A 记录** `uat.binflow.org` → `52.79.109.153`（权威 NS 在 businessidentity.llc；DoH 实测现 NXDOMAIN）
2. **AWS 安全组**放行 80+443（80 = HTTP-01 + 308 跳转）

## 在途

- T-464（en 填充）+ T-479（矩阵 remote/virtual）——双 lane 心跳活跃。

## 状态

M16: **29/35**。
