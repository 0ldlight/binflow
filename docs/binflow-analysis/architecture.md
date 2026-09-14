# BinFlow 架构地图（证据指针文档——正文在既有产物）

> 本文件是新宪章（UI/UX 现代化阶段）Discovery 归档的**指针层**：只做既有证据的地图与链接，不复制正文、不新增取证。
> 派生自 22 轮兼容程序既有产物（docs/design/、DECISIONS.md、docs/reverse/domain-map.yaml），采集日 2026-09-14。

## 1. 一句话现状

Go 模块化单体 + 单二进制（ADR-0002），自托管 SQLite/Postgres 双态元数据（ADR-0003/0007），控制台为 React SPA（ADR-0014，fe-rewrite 后 shadcn/tailwind 栈），概念模型与 Artifactory 一一对齐（ADR-0003 执行表见 architecture §10）。

## 2. 架构正文所在（不在此重述）

| 面 | 唯一事实源 | 覆盖 |
|---|---|---|
| 总体架构规范 | `docs/design/architecture.md`（§1 总览 / §2 包结构 / §3 模块接口契约 / §4 存储引擎 / §5 适配器 SPI / §6 元数据 Schema / §7 HTTP 层 / §8 配置模型 / §9 部署约定） | M1 定稿 + M2~M17 增量逐节标注（§13 控制台 / §14 M9 服务端 / §15 license·addon / §23 可观测 / §24 AQL / §25 调度 / §26 build·bundle·insights） |
| 决策记录 | `DECISIONS.md`（ADR-0000~0050，51 条，只追加+Errata） | clean-room（0001）/模块化单体（0002）/存储布局即契约（0006）/RBAC 闭集（0026）/license 门控（0032）/addon 注册表（0033）/Webhook 总线（0041）/AQL 引擎（0043）/cron 域（0044）/存储链终案（0049）等 |
| 能力域对照 | `docs/reverse/domain-map.yaml`（Artifactory 26 能力域 → 模块映射，E1）+ `docs/compatibility/matrix.yaml` by_domain（D01~D14） | BinFlow 按能力域（非按 Artifactory 包结构）对齐的映射依据 |

## 3. 分层与包面速览（指针级）

- **入口**：`cmd/`（binflow-server 主二进制、bf CLI〔ADR-0023〕、bf-migrate〔ADR-0024〕）。
- **业务包**：`internal/` 24 包（repo / storage / metadata / auth / httpapi / console / search / remote / replication / build / bundle / scheduler / webhook / license / addons / audit / keypair / metrics / migrate / adapter / config / client / docs）——逐包职责见 `docs/design/architecture.md` §2。
- **前端**：`web/src/` React SPA——见本目录 `frontend-map.md`。
- **横切缝**：存储 Backend 缝（ADR-0018/0019）、适配器 SPI（§5）、addon 注册表（ADR-0033）、Webhook 单一 Emit 缝（ADR-0041）。

## 4. 关键架构不变量（出处可点的短清单）

1. `/binflow` 统一路由前缀 + docker `/v2` 根级例外（ADR-0008/0010）。
2. 目录实体化不变量：putNode 材料化祖先 folder 行（ADR-0016）。
3. 布局即兼容契约：blob 磁盘布局不可漂移（ADR-0006）。
4. 概念对齐：rclass 三态 / permission target / layout 等术语与 Artifactory 一一对应（ADR-0003 + architecture §10 表）。
5. license 三档闭集 + 数据面单点门控（ADR-0032）；新协议包型管理面统一挂 `/binflow/api/<proto>/`（ADR-0034）。

## 5. 与参照的能力面差距入口

架构层面的差距不在此维护——四态主账在 `docs/compatibility/matrix.yaml`（200 行），UI 面差距在新宪章矩阵 `docs/ui-parity-matrix.md`，已知差异台账在 `docs/compatibility/known-divergence.yaml`（51 条）。

## 6. 缺口声明（真无证据的面）

无。架构面证据充分（architecture.md 3,000+ 行规范 + 51 条 ADR 均在库）。
