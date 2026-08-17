# deploy/ — 多元部署矩阵

release-engineer 维护（基线见 DECISIONS.md ADR-0004）。目标：GA 时交付以下全部部署方式，每种都有 tech-writer 对应的安装指南 + 部署烟测。

| # | 方式 | 产物位置 | 说明 |
|---|---|---|---|
| 1 | 单二进制 | goreleaser → dist/ | linux/darwin/windows × amd64/arm64 + 校验和 |
| 2 | Docker 镜像 | 根 Dockerfile | multi-arch，distroless / alpine 双变体 |
| 3 | docker-compose | deploy/compose/ | 含卷持久化、健康检查、Postgres 可选 |
| 4 | Helm Chart | charts/binflow/ | values：镜像/PVC/ingress/资源/探针/HPA |
| 5 | 原生 K8s 清单 | deploy/k8s/ | Deployment/PVC/Service/Ingress/Secret 模板 |
| 6 | systemd | deploy/systemd/ | unit 文件 + install.sh（建用户/目录/自启） |
| 7 | 离线安装包 | deploy/offline/ | 镜像 tar + Chart + 脚本 + 校验和（air-gapped） |

约定：

- 目录布局细节由 architect 在 `docs/design/architecture.md` 定稿后落地。
- M2 起每个里程碑包含对本阶段已有部署方式的烟测票。
- 对外推送镜像 / Chart / 发布二进制 = 对外发布，必须先经用户确认。
