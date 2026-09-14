# 源码分析 · API 表面（证据指针文档——正文在既有产物）

> 指针层。正文：`docs/reverse/api-inventory.yaml`（聚合粒度端点清单）+ `docs/reverse/rest-api.md`（核心面行为）+ `docs/reverse/inv-2-surface.md`（357 resource 全量清点）。

## 1. 表面规模

- **357 resource 类 ≈ 1,866 方法级操作**（inv-2 §0 估计 1,800±100）。
- api-inventory.yaml 每行 = 一个 resource × 操作类别（代表 method+path 形态），认证词汇表逐行标注（anon/user/r/w/n/d/m/admin/UNKNOWN）。
- UI/SSR 面：`docs/reverse/frontend/api-map.yaml`——`/ui/api/v1` 端点族（frontend-server 50+ 子路由文件，按服务前缀分发 /ui /access /mds /event…）。

## 2. 对账资产链

| 层 | 文件 | 状态 |
|---|---|---|
| 官方条目基线 | JFrog REST reference 三索引 **653 条目** | rest-compat-matrix.md §0 |
| 全量对账快照 | `docs/reverse/rest-compat-matrix.md`（195 冻结行——历史快照） | 行态已迁 matrix.yaml |
| 四态主账 | `docs/compatibility/matrix.yaml`（200 行，唯一事实源） | 持续更新 |
| 可执行契约 | `docs/compatibility/contracts/`（87 条目七文件） | IMPLEMENTED/VERIFIED 在途 |

## 3. 核心面行为规格

`docs/reverse/rest-api.md`——端点表（路径/方法/参数/响应码/示例）+ 待验证清单 3 项（atomic 删除中间态/GC 响应流式格式/checksumDeployed 外显）。

## 4. 协议端点（对外绝对表面）

- `docs/reverse/protocols/registry.yaml`——57 包型注册表。
- 协议规格文件族：docker-registry / maven-npm-pypi / npm / goproxy / conan / debian / rpm / helm / nuget / cargo / aql。
- 横切协议能力 22 项（矩阵参数/checksum 部署三头/路径归一化中枢）——`docs/reverse/inv-3-protocols.md`。

## 5. 缺口声明

UI/SSR 面（api-map.yaml）为清点级（无逐端点 wire——inv-2 §1 口径）；api-inventory 的 UNKNOWN 认证词条已收割进 `docs/compatibility/unknown.yaml` API 域（8 条）。
