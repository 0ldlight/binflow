# Sprint 1386 迭代报告 — 矩阵⑥=9/10（nuget 第五层=产品侧：spool 同族病）→ T-476 立派 + ci 30m + T-475 已派

**日期**: 2026-09-04 22:5x
**上轮**: Sprint 1385（具名源层）

## 一、矩阵⑥裁定（run 33885553421 @89e8e58）

**9/10**：九腿稳绿；nuget 第五层——push 抵达服务器得 **500**，本地铁证复刻：

```
PUT …/api/nuget/v3/uat-matrix-nuget-local/flatcontainer
spool upload: open /tmp/binflow-nuget-865178418.nupkg: read-only file system
```

**= helm 同族病**（T-474 遗留登记的预言成真：cargo/deb/rpm/nuget 四面 `os.CreateTemp("")` 潜伏）。洋葱前四层皆脚本侧，此层产品侧——**T-476 立派即派**（dev-registry-adapter：普查+四面迁移 `adapter.StageFile`+对照针）。

## 二、GH ci 终局（peer 取证，run 33881407081 @d38186c）

- **audit node 24 ✅ 实证生效**（400 消失）
- **ci 红 = Test 20m 墙钟**（慢 runner：repo 单包 772s 累计爆表；警报落点 3s 测试=纯容量非 hang）→ **30m 已落**（`b1228be`，SSH 443 路线——本机 22 端口被代理拦，HTTPS 拒 workflow 文件）
- **e2e 红 = 老家族**（t443/repositories:58/m9-usage-fanout…retries 未吸收）→ **T-475 已派**（dev-frontend：六 spec 并集，TZ=UTC/viewport 1280×720 本地复现二分，spec 侧确定性优先）
- release-dryrun ✅

## 三、nuget 洋葱全录（五层收口在望）

① SDK 10 吞错 → ② 包版本 Int32 → ③ 装配版本 UInt16 → ④ 具名源 cwd 解析 → **⑤ 产品 spool /tmp（T-476）**。

## 四、在途与状态

- lane：T-475（FE e2e 确定性）+ T-476（四协议 spool 迁移）
- M16: **24/35**
- 复测收官链：T-476 收编 → 终局合 main（矩阵⑦+ci 30m+node24 一次验）
