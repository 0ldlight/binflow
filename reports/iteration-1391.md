# Sprint 1391 迭代报告 — 【里程碑】十协议矩阵 GH 面 10/10 全绿（intake ⑩ 内容首满格）+ intake ⑱ 落地（CircleCI 拆十并行 job）+ 新执行机受阻

**日期**: 2026-09-05 03:0x~03:2x
**上轮**: Sprint 1390（终局合 main）

## 一、【里程碑】矩阵 10/10（main f98bb6b9，双 dispatch 确认）

**nuget ✅（T-476 五层洋葱收口实证）** + conan/helm/docker/maven/gradle/npm/pypi/generic/go 全绿——**intake ⑩ 的十协议推送/拉取矩阵目标内容侧首次满格**。CircleCI protocol_matrix 同 commit 红且无腿归因（单 job 串行的可观测性缺陷）→ 由 intake ⑱ 落地解决（下述）。

## 二、intake ⑱ 落地（`b578bdca`）

「circleci 将不同协议的测试改为不同的 job 并行执行」→ **protocol_matrix 单 job 拆参数化 protocol_leg ×10**：每腿只装自家工具链 / 腿级失败归因上 commit status / uat + nightly 双 workflow 并行。`circleci config process` 过。CircleCI 面定谳走新结构首跑（T-475 收口后合 main 触发）。

## 三、新执行机 192.68.1.250（用户免密）——受阻待修

TCP 22 通但 **kex 期被远端断**（三次复现）= 对端 sshd 侧问题（候选：MaxStartups/fail2ban/hosts.deny/sshd 半死）。**已请用户查**；通后承载净窗验证（race 全量/重型 e2e——共租噪音痛点正解）。

## 四、T-474~T-476 战果链归档

helm/nuget/cargo/deb/rpm 五协议 spool 家族病全灭（同卷 staging + 507 有界披露）；CI 侧五层洋葱（SDK 吞错→Int32→UInt16→具名源→产品 spool）全剥。**T-477 候立**（repo/archive.go X-Explode-Archive 同族）。

## 五、在途与状态

- lane：T-475（e2e 确定性，验证段）+ CircleCI e2e（f98bb6b9 pending）
- M16: **24/35**
