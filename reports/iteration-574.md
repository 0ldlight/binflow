# Sprint 574 迭代报告 — 🏁 M9 完结（closure）

**日期**: 2026-08-25 08:00
**上轮**: Sprint 573；其间 T-272 复验 **PASS**（DoD 八条全绿）→ 收官序列全执行。

## M9 DoD 终态（八条全绿）

首验 FAIL（诚实判 P0）→ 修复窗（T-274 锚回填 / T-275 gosec+census）→ 复验 PASS：e2e **176/0 默认并发**（26 红腿逐点名复绿 + 8 dnr 全恢复）/ lint 0 / ledger 六形态诚实 PASS / 契约审计 100% 归属 / F 池对账（T-273 本里程碑修复收口 #9）。

## 收官序列执行

- ROADMAP：M9 段勾账完结 + **M10 候选池**补建（延后 3 + Q5/E7 + 票级遗留 17）
- 瘦身手册头注翻转（已执行态）
- BOARD：M9 完结节 + 26 票终态
- **`m9-done` tag 已打并推双远端**（origin + vm；常态授权）——**九枚里程碑 tag 齐（m1~m9）**
- **用户实例 18080 刷新到 M9**（新二进制 + 新端点实测：usage 批量端点 live、登录 200、新控制台）

## M9 交付总览（26 票）

- **A 组六端点**：users 域加宽/enabled 回显/DELETE 三护栏级联/groups includeUsers/usage 批量（~171→1）/permissions filter=manage（ManageCoverage seam）
- **GC 根治**：hold set + 删除前复核（T-232 竞态在原伤口条件下验证）；`--workers=1` 退役（三轮并发绿）
- **OIDC step-up 控制台腿** + npm 追加语义 + redirect 移除 + 锚册单一权威（六形态 + 局限史条款）
- **运营**：git 历史瘦身（clone 70→8MB，三远端净）+ 多架构镜像（BinFlow 首次自托管 manifest list）
- **修复窗**：锚误杀 19 族回填（audit 正则盲区根因修）+ last-admin census 折入事务

## 九里程碑链

M1 内核基座 → M2 Docker Registry → M3 多生态代理 → M4 控制台治理 → M5 GA 发布矩阵 → M6 企业就绪 → M7 RBAC/续传/step-up → M8 控制台对齐 Artifactory → **M9 服务端缺口收口**。全部 DoD 达成、tag 齐、双远端同步。

## 待用户（均非阻塞）

1. M10 方向（候选池已录 ROADMAP）或新方向或收工停 /loop
2. Jenkins 常驻（场景五 job + 三级自建 + dogfood）在 VM 持续可用
