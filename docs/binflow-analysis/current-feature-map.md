# BinFlow 当前功能地图（证据指针文档——正文在既有产物）

> 指针层：BinFlow 已有/缺什么功能——四态主账 + 契约的视图入口。采集日 2026-09-14。

## 1. 四态总览（matrix.yaml summary 实计，2026-09-14 时点）

**200 行**：✅ compatible 74 ｜ ◐ partial 17 ｜ ❌ absent 78 ｜ ⛔ not_applicable 19 ｜ 超集 superset 12。
优先级：P0 58 / P1 62 / P2 80。置信度：高 134 / 中 66 / 低 0。

## 2. 按域（D01~D14）现状一览

| 域 | 行数 | 态势（compatible/partial/absent/超集/⛔） |
|---|---|---|
| D01 制品与存储 | 43 | 22/5/10/2/4 |
| D02 仓库配置 | 13 | 5/0/6/1/1 |
| D03 搜索 | 19 | 9/0/8/0/2 |
| D04 安全 | 35 | 19/3/12/1/0 |
| D05 复制 | 11 | 0/5/4/1/1 |
| D06 系统与运维 | 30 | 4/3/11/7/5 |
| D07 Build-info | 11 | 0/0/11（M17 已建最小面——主账行态滞后，见 §4 注） |
| D08 Release Bundle | 7 | 0/0/6/0/1（同上） |
| D09 Webhook | 9 | 7/0/1/0/1 |
| D10 用户插件 | 1 | 0/0/1 |
| D11 生命周期治理 | 2 | 0/0/1/0/1 |
| D12 协议面 | 17 | 8/1/7/0/1 |
| D13 UI 内部面 | 1 | ⛔1 |
| D14 外部产品面 | 1 | ⛔1 |

> 注：主账行态以 matrix.yaml 为唯一事实源（本表为只读投影，行态更新不在此发生）。

## 3. 可执行契约（功能面的行为级账）

`docs/compatibility/contracts/` 七文件 87 条目：npm 17（K60 认证族 + tarball 面）、docker-remote 25（含 404 负缓存 INTENTIONAL）、conan 16、pypi 10、storage-admin 10（E5-E10 新行族）、helmoci 5、goproxy 4。契约状态机 DISCOVERED→SPECIFIED→IMPLEMENTED→VERIFIED（±DIVERGENT/BLOCKED/INTENTIONAL）。

## 4. 已知差异与未知项

- 已知差异 51 条：`docs/compatibility/known-divergence.yaml`（BUG 26 / UNKNOWN 17 / INTENTIONAL 7 / UNSUPPORTED 2）。
- UNKNOWN 队列 110 条：`docs/compatibility/unknown.yaml`（STG11/SEC12/FE7/LOG15/DIST8/ENT28/PROTO10/API8/CFG5/JOB6；P0 3 / P1 50 / P2 57）。
- 详见本目录 `technical-debt.md`。

## 5. UI 功能面

UI parity 不在 matrix.yaml 维护（D13 为 ⛔）——新宪章矩阵 `docs/ui-parity-matrix.md`（六列，页面族粒度）；锚册 `docs/design/console-artifactory-parity.md` §7 页面×模式差距矩阵 + §12 B47 四态预归属表（48 行：翻正 36〔v1.6~v1.14 修订已将其中约 30 行翻「已落」〕、豁免·复核维持 3、stay-out 确认 2、候裁挂起 4、对账去重 3）。

## 6. 迭代史（22 轮闭环）

`reports/iterations/iteration-000.md` ~ `iteration-022.md`（23 篇，含 Compatibility 四问）——每轮功能增量与差分证据的时间线账。

## 7. 缺口声明（真无证据的面）

无。功能面四态、契约、差异、迭代四线齐备。
