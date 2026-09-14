# BinFlow API 表面地图（证据指针文档——正文在既有产物）

> 指针层：BinFlow 对外 API 面 + 与 Artifactory 表面的对账入口。采集日 2026-09-14。

## 1. BinFlow 自有 API 面（as-built）

- **OpenAPI 唯一事实源**：`fern/openapi/binflow.json`（OpenAPI 3.1，**112 paths / 158 operations**——生成器 `tools/openapi-spec/`，文档站 `fern/`）。
- **路由前缀**：管理/协议面统一挂 `/binflow/api/...`（ADR-0008）；新协议包型挂 `/binflow/api/<proto>/`（ADR-0034）；docker `/v2` 根级例外（ADR-0010）。
- **前端消费面**：≈156 管理面动词字面量全表在 `docs/design/frontend-rewrite-audit.md` §API 契约路由表。

## 2. Artifactory 参照表面（对账基线）

| 资产 | 规模 | 内容 |
|---|---|---|
| `docs/reverse/api-inventory.yaml` | 357 resource 类 ≈ 1,866 方法级操作（聚合行 = resource × 操作类别） | REST/UI/协议表面清单，认证词汇表逐行标注 |
| `docs/reverse/rest-api.md` | 核心面行为细节（端点表/错误体/示例） | M1 起核心 REST 规格 |
| `docs/reverse/rest-compat-matrix.md` | 195 冻结行（历史快照，行态不再更新） | 官方 REST reference 653 条目 × BinFlow 对账 |
| `docs/reverse/frontend/api-map.yaml`（114 行） | `/ui/api/v1` SSR/UI 面端点族 | 前端网关面（与 REST 面两套 API） |

## 3. 对账主账（四态）

`docs/compatibility/matrix.yaml`——**200 行**，四态 ✅74 / ◐17 / ❌78 / ⛔19 / 超集12（summary 实计），D01~D14 域分布、契约引用 10 行、差分回填 16 行。优先级分布 P0 58 / P1 62 / P2 80。

## 4. 可执行契约（87 条目，7 文件）

`docs/compatibility/contracts/`：conan 16 / docker-remote 25 / goproxy 4 / helmoci 5 / npm 17 / pypi 10 / storage-admin 10（十七字段契约格式；evidence 挂 probes/）。

## 5. 协议面细则

`docs/compatibility/protocol/`（每协议可执行面）+ `docs/reverse/protocols/registry.yaml`（57 包型注册表）+ 各包型规格（docker-registry / maven-npm-pypi / npm / goproxy / conan / debian / rpm / helm / nuget / cargo / aql …）。

## 6. 缺口声明（真无证据的面）

无。API 面有 OpenAPI（BinFlow 侧）+ 官方 653 条目对账 + 200 行主账三层覆盖；唯 UI 内部 SSR 面（D13）为 ⛔ not_applicable（参照前端网关不属 BinFlow 对齐义务）。
