# deploy/ — 多元部署矩阵

release-engineer 维护（基线见 DECISIONS.md ADR-0004）。目标：GA 时交付以下全部部署方式，每种都有 tech-writer 对应的安装指南 + 部署烟测。

| # | 方式 | 产物位置 | 说明 |
|---|---|---|---|
| 1 | 单二进制 | goreleaser → dist/ | linux/darwin/windows × amd64/arm64 + 校验和 |
| 2 | Docker 镜像 | 根 Dockerfile | multi-arch，distroless / alpine 双变体 |
| 3 | docker-compose | deploy/compose/ | 含卷持久化、健康检查、Postgres 可选；`--profile nginx` TLS 反代；`--profile s3` 内置 MinIO（桶自动初始化，BinFlow 等 MinIO ready 后起服） |
| 4 | Helm Chart | charts/binflow/ | values：镜像/PVC/ingress/资源/探针/HPA；`config.s3`/`config.oidc`/`config.ldap` 配置段（密钥一律 existingSecret 引用，ADR-0009） |
| 5 | 原生 K8s 清单 | deploy/k8s/ | Deployment/PVC/Service/Ingress/Secret 模板 |
| 6 | systemd | deploy/systemd/ | unit 文件 + install.sh（建用户/目录/自启） |
| 7 | 离线安装包 | deploy/offline/ | 镜像 tar + Chart + 脚本 + 校验和（air-gapped） |

约定：

- 目录布局细节由 architect 在 `docs/design/architecture.md` 定稿后落地。
- M2 起每个里程碑包含对本阶段已有部署方式的烟测票。
  - M2（2026-08-19）：`deploy/dev`（compose + 镜像）烟测全过（FR-14-AC1~AC4 + O2），报告 `reports/agents/T-45-smoke.md`；M2 docker 接入口径与前置反代直通示例见 `deploy/dev/README.md`。
  - M4（2026-08-21）：`deploy/dev` 烟测全过（FR-33-AC5 console 链 + session/GC/备份 + 反代；镜像 Dockerfile 增 node console 构建阶段），报告 `reports/agents/T-106-qa.md`；产品侧缺陷 D-106-1（mkdir 目录节点致 export 拒绝）转主会话。
  - M6（2026-08-22，T-170）：compose `--profile s3`（MinIO + 桶初始化 + depends_on 就绪门控）烟测全过（/healthz storage=ok + 制品 roundtrip sha256 一致）；Chart `config.s3/oidc/ldap` 段经 `helm lint`/`helm template` + 真实 config.Load 回灌验证。OIDC/LDAP 示例待 T-157 接线合入后方可启用（当前启用会被严格解码器 fail-fast，属预期）。
- 对外推送镜像 / Chart / 发布二进制 = 对外发布，必须先经用户确认。
