# Sprint 1385 迭代报告 — nuget 洋葱第四层：具名源解析（push 上下文错位）→ ⑥在跑

**日期**: 2026-09-04 22:4x
**上轮**: Sprint 1384（装配版本层）

## 矩阵⑤裁定（run 33884989165 @9b4fd0e）

**9/10**：九腿全绿稳定；nuget 第四层——`dotnet nuget push --source binflow` 报 **"The specified source 'binflow' is invalid"**：具名源只沿 cwd 的 nuget.config 链向上解析，而 push 从 WORKDIR 跑、config 在 proj/ 内。

## ⑥修（`89e8e58`）

nupkg 拷入 proj/ 并在 proj/ 上下文 push（具名源+凭据块同链解析）。run 33885553421 在跑。

## 洋葱累计四层

① SDK 10 吞错（MSB4181）→ ② 包版本 Int32 → ③ 装配版本 UInt16 → ④ 具名源 cwd 解析。每层皆真实客户端工具链行为差——本地 community 档 SKIP 掩盖的「首跑坑」本质。

## 状态

M16: **24/35**；⑥裁定挂下轮。
