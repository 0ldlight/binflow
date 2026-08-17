# BinFlow 帮助文档中心

> tech-writer 维护。每篇头部标注适用版本。结构如下，未写篇目随里程碑补齐。

## 导航

- **快速开始**
  - [5 分钟上手（单二进制）](getting-started/binary.md) — M1
  - [5 分钟上手（Docker）](getting-started/docker.md) — M1
- **安装指南**（每种部署方式一篇，`install/`）
  - [单二进制安装](install/binary.md) — linux/darwin/windows × amd64/arm64
  - [Docker 运行](install/docker.md) — 含 distroless/alpine 变体说明
  - [docker-compose 部署](install/compose.md)
  - [Helm Chart（Kubernetes）](install/helm.md)
  - [原生 K8s 清单](install/k8s.md)
  - [systemd 服务（裸机）](install/systemd.md)
  - [离线安装（air-gapped）](install/offline.md)
- **客户端接入**（`integrations/`，每协议一篇）
  - [Docker / OCI 镜像](integrations/docker.md)（含 podman/crane/skopeo/oras）
  - [Maven](integrations/maven.md)（settings.xml 配置 + deploy/resolve）
  - [npm](integrations/npm.md)（.npmrc + publish/install）
  - [PyPI](integrations/pypi.md)（pip index-url + twine）
  - [Generic / 任意文件](integrations/generic.md)（curl roundtrip）
  - CI 集成：GitHub Actions / GitLab CI / Jenkins 用作依赖源与镜像源
- **管理指南**（`admin/`）：仓库配置（local/remote/virtual）· 用户组与权限 · API Token · 备份恢复 · GC 与配额 · 监控
- **API 参考**（`api/`）：Artifactory 兼容子集 + `/api/v1`
- [FAQ 与故障排查](faq.md)（含 Artifactory 迁移对照表）

## 从 Artifactory 迁移

概念一一对应：local/remote/virtual 仓库、repo key、node、checksum、部署/解析权限——术语不变。
对照表见 [faq.md](faq.md)。
