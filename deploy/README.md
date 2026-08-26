# deploy/ — 多元部署矩阵

release-engineer 维护（基线见 DECISIONS.md ADR-0004）。目标：GA 时交付以下全部部署方式，每种都有 tech-writer 对应的安装指南 + 部署烟测。

| # | 方式 | 产物位置 | 说明 |
|---|---|---|---|
| 1 | 单二进制 | goreleaser → dist/ | linux/darwin/windows × amd64/arm64 + 校验和 |
| 2 | Docker 镜像 | 根 Dockerfile | multi-arch，distroless / alpine 双变体 |
| 3 | docker-compose | deploy/compose/ | 含卷持久化、健康检查、Postgres 可选；`--profile nginx` TLS 反代；`--profile s3` 内置 MinIO（桶自动初始化，BinFlow 等 MinIO ready 后起服） |
| 4 | Helm Chart | charts/binflow/ | values：镜像/PVC/ingress/资源/探针/HPA；`config.s3`/`config.oidc`/`config.ldap`/`config.addons`（M10 断路器）/`config.replication`/`config.storage`/`config.auth` 配置段，托管键显式渲染进 ConfigMap（T-183 教训）；密钥一律 existingSecret 引用，ADR-0009 |
| 5 | 原生 K8s 清单 | deploy/k8s/ | Deployment/PVC/Service/Ingress/Secret 模板 |
| 6 | systemd | contrib/systemd/ | unit 文件 + install.sh（建用户/目录/自启；配置 `/etc/binflow/binflow.yaml`，env 覆盖走 `/etc/binflow/env`，可承载 `BINFLOW_ADDONS__DISABLED`） |
| 7 | 离线安装包 | deploy/offline/ | 镜像 tar + Chart + 脚本 + 校验和（air-gapped） |

约定：

- 目录布局细节由 architect 在 `docs/design/architecture.md` 定稿后落地。
- M2 起每个里程碑包含对本阶段已有部署方式的烟测票。
  - M2（2026-08-19）：`deploy/dev`（compose + 镜像）烟测全过（FR-14-AC1~AC4 + O2），报告 `reports/agents/T-45-smoke.md`；M2 docker 接入口径与前置反代直通示例见 `deploy/dev/README.md`。
  - M4（2026-08-21）：`deploy/dev` 烟测全过（FR-33-AC5 console 链 + session/GC/备份 + 反代；镜像 Dockerfile 增 node console 构建阶段），报告 `reports/agents/T-106-qa.md`；产品侧缺陷 D-106-1（mkdir 目录节点致 export 拒绝）转主会话。
  - M6（2026-08-22，T-170）：compose `--profile s3`（MinIO + 桶初始化 + depends_on 就绪门控）烟测全过（/healthz storage=ok + 制品 roundtrip sha256 一致）；Chart `config.s3/oidc/ldap` 段经 `helm lint`/`helm template` + 真实 config.Load 回灌验证。OIDC/LDAP 示例待 T-157 接线合入后方可启用（当前启用会被严格解码器 fail-fast，属预期）。
  - M10（2026-08-26，T-295）：M10 配置面（`addons.disabled`）+ M6 后新增键（`replication.allow_private_target`、`storage.gc_hold_ttl_seconds`、`auth.token_step_up`(+grant TTL)、oidc/ldap `readonly_group`）进部署矩阵。Chart 托管键全部显式渲染（T-183 教训）；`helm lint` 0 告警；渲染产物起服烟测全过（license 三端点 community 地板 + `addons.disabled` 未知槽位/空格/空项容错 WARN 不炸、core id docker 被禁用 honored）。`BINFLOW_M10_LICENSE_DIR` 是矩阵测试变量而非 config 键，设到 server 上会被严格 env 扫描拒绝启动——三处部署面均加了警示。
- 对外推送镜像 / Chart / 发布二进制 = 对外发布，必须先经用户确认。
