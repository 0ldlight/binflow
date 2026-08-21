# 迭代报告 252 — Sprint 252（批 8 全闭环 + 批 9 派发轮）

- 日期：2026-08-21 08:40
- 里程碑：M4
- conductor：主会话

## 本轮动作摘要

1. **T-105 → done（PASS 279/279）**——M4 QA 三段（T-103/T-104/T-105）全部收口，**DoD §9 第 1/2 条终判素材齐**：
   - M1~M3 全序列回归 distinct 279 PASS / 0 FAIL（首试 79 FAIL 全为 QA 侧构造，修正账归档）
   - 性能门槛全达：冷启动 120~151ms · SPA 首屏 393~477ms · P95 搜索 72ms/审计 16ms · 1GB PUT 194MB/s（RSS 52KB）· 三组并发零 5xx
   - 客户端矩阵扩至 podman/crane/oras（Helm OCI 逐位一致）/skopeo
   - **非预期 5xx=0 · panic=0**；O-MEM1（argon2id 瞬态、m1-done 同形非回归）→ T-107 文档引导
   - 报告提交 `c4a9db3`
2. **批 9 三线派发**（M4 最终批）：T-106（部署烟测——二进制+compose 形态，M4 新面 console 反代/session 路径/GC·备份 CLI）+ T-107（M4 用户文档——控制台指南/管理增补/FAQ，收编回写遗留移交）+ T-121（PRD v1.4，T-105 E1~E5）。

## 看板快照（本轮结束时）

- todo: 0（原始票单 T-89~T-107 全部派完/闭环）· doing: 3（T-106/T-107/T-121）· done: 135 · blocked: 0

## 阻塞与风险

- 无。批 9 收口后即 M4 DoD 五条核查。

## 下轮计划

1. 收批 9 三线 → 核验提交 → done。
2. **M4 DoD 五条核查**（§9 第 1/2 条素材已齐 + 批 9 补全）→ 向用户报告并请示 tag m4-done。
