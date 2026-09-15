# 运行时分析 · 搜索面（证据指针文档——正文在既有产物）

> 截图基线 ：8082（7.161.20）。路径前缀 `pc/` = `docs/reverse/frontend/parity-capture/`。

## 1. 截图

| 资产 | 内容 |
|---|---|
| `pc/screenshots/screens/artifact-search-results.png` | 搜索结果页（AG Grid 列集 + 计数） |
| `pc/screenshots/dialogs/quick-search-overlay.png` / `quick-search-focused.png` | 顶栏快搜 overlay（815px 形态，输入聚焦 + 结果） |
| `pc/screenshots/states/search-results-after-enter.png` | Enter 提交后结果态 |

## 2. 行为规格（正文）

- `docs/reverse/aql.md`——AQL 语言子集/envelope 与错误文案逐字/资源治理 K63/virtual 仓语义/老搜索 14 端点族 OSS 可用性矩阵；§14 UI 搜索族四端点 wire（`/api/search/usage` 等）；§15 builds 入口三族。
- `docs/compatibility/matrix.yaml` D03（搜索 19 行：✅9/❌8/⛔2）。
- `docs/reverse/frontend/screens.yaml`——artifact-search-results 屏四态（含计数不一致怪癖 E4）。
- 快搜范围页签（Artifacts/Builds…）——parity §9A-S7 stay-out 登记（Builds dep M17）。

## 3. 已知运行时事实

- 快搜 overlay 实测含最近搜索与结果两层（quick-search 采集注记）。
- 搜索结果计数 header/footer 不一致系参照实测缺陷——BinFlow stay-out（§9A-S3：自有恒一致更优）。

## 4. 缺口声明

AQL 编辑器 UI（7.161 Query Language 面板）无专项截图——行为以 `docs/reverse/aql.md` 规格为准（反编译 + t226 活体核验双源）；saved searches / 搜索管理页无证据。
