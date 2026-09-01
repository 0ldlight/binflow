# Sprint 1114 迭代报告 — P0 用户主诉热修 T-406（制品浏览 remote/virtual 红卡）；T-399 收口——M14 22/22 实现面收官

**日期**: 2026-09-01 07:5x
**上轮**: Sprint 1113（T-399 复活重跑）

## 用户 P0：制品浏览无法展示制品 → 定位 + 修复（conductor 亲自）

**复现路径**（穷举三环境后锁定）：UAT 正常 → HEAD 新种子正常 → **用户真实数据副本（~/dev-center/data，266 仓）复现**——树列全类仓库，内容面对 remote/virtual 一律 400 → 点 t240r/t240v 即红卡「请求失败」。

**T-406 → done**（internal/repo + ArtifactsBrowser + 锚册 v1.26）：
- 服务端：`List` remote 列缓存行（永不回源）；`Get` remote folder 面 = marker 行直答 + 读侧材料化（存量缓存自愈）；virtual 拒绝维持（FR-21-AC8 P2）。
- Console：virtual 不发注定 400 调用 → 成员感知空态；RepoBranch virtual 静态化；remote 空态文案分支。
- 证据：go 全量绿 + lint 0 + 四门（anchor 0 断链）+ e2e 38✓ + 用户数据副本 live 四态实证。
- **用户本地生效**：拉 develop + `make console && make build` 重启即可（无数据迁移）。

## T-399 → done（release+UAT）

六腿烟测全绿 + Chart **1.3.0→1.4.0** 定案 + PVC keep kind 真集群实证 + UAT BEFORE 取证 + AFTER 清单候 T-400。**F1 首红**：六平台聚合 103.36MB > 100MB 门（建门 94.13 累积所致；单档/运行时门绿，WARN 放行）——归 T-400 收口裁。

## 状态

M14：**22/22 实现面 + 2 条件票（T-401 已裁对齐 409 翻转票、T-403 symbol 余量）**。剩 **T-400（终验 P0）** → m14-done（LC-66 座 + F1/D-396-2 记账 + README/docs 例查 + m14-done tag + 里程碑 PR + UAT AFTER）。HEAD[develop]=待推（T-406 变更集）。
