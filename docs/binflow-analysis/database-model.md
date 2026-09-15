# BinFlow 数据模型地图（证据指针文档——正文在既有产物）

> 指针层：元数据 schema 的入口与表族分组。DDL 正文在 `internal/metadata/migrations/`（sqlite 25 文件 / postgres 15 文件）与 `docs/design/architecture.md` §6。采集日 2026-09-14。

## 1. 存储双态

- SQLite（默认，ADR-0007 并发策略：单写多读 + busy 重试）与 PostgreSQL 双方言迁移并行维护——`internal/metadata/migrations/{sqlite,postgres}/`。
- 迁移机制：`internal/metadata/migrate.go` + `cmd/bf-migrate`（Artifactory 导入侧，ADR-0024）。

## 2. 表族分组（41 张表，按域归组——逐列 DDL 见 migrations）

| 域 | 表 |
|---|---|
| 制品/存储 | nodes、node_props、blobs、remote_cache、upload_sessions、repo_usage |
| 仓库 | repositories、virtual_members、remote_configs |
| 安全 | users、groups、user_groups、tokens、web_sessions、permission_targets、permission_principals、auth_configs |
| docker | docker_manifests、docker_tags、docker_refs |
| build-info（M17） | builds、build_modules、build_artifacts、build_dependencies、build_promotions、build_properties |
| release bundle（M17） | bundles、bundle_items |
| 治理 | backups、schedules、replications、replication_tasks、replication_globals |
| 密钥/许可 | gpg_keypairs、licenses |
| 集成 | webhook_subscriptions、webhook_subscription_events、webhook_deliveries |
| 观测 | audit_events |

## 3. Schema 规范正文

- `docs/design/architecture.md` §6「元数据 Schema（SQLite DDL）」——表级完整规范（838 行起）。
- 关键不变量的 ADR：目录实体化（0016——putNode 材料化祖先 folder 行）、GC 并发安全（0031——在途持有集 + 删除前引用复核）、cron 台账（0044）、nodes 统计四列与三分计数口径（K69，详规 architecture §25）。

## 4. 与 Artifactory 数据面的关系

BinFlow 不复刻 Artifactory 的 derby/内部表结构（clean-room：行为对齐而非结构对齐）。行为侧对账入口：`docs/reverse/storage-layout.md`（filestore 目录推导）、`docs/reverse/storage/`（binary provider 链/prune-gc）、`docs/reverse/build-info.md` §3（参照表族 DDL 仅作行为参照）。

## 5. 缺口声明（真无证据的面）

无。DDL 双方言齐全；性能/并发行为有 race_on/off 测试族（internal/metadata/*_test.go 60+ 文件）承载。
