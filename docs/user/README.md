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
	  - [升级与版本说明](install/upgrade.md) — 升级策略、迁移链 001~007
- **客户端接入**（每协议一篇）
  - [Docker / OCI 镜像](docker-registry.md)（login/push/pull、oras/Helm 承载、podman/crane/skopeo、差异清单）— M2
  - [Maven](integrations/maven.md)（settings.xml + deploy/resolve、snapshot/-U、checksum 策略、mirror 收口）— M3
  - [npm](integrations/npm.md)（.npmrc + _auth、publish/install、dist-tag/unpublish、上游边界）— M3
  - [PyPI](integrations/pypi.md)（pip.conf + twine、hash 对账、PEP 691）— M3
  - [Generic / 任意文件](integrations/generic.md)（curl roundtrip）
  - CI 集成：GitHub Actions / GitLab CI / Jenkins 用作依赖源与镜像源
- **Web 控制台** — M4
  - [控制台使用指南](console.md)（登录与会话/TTL 语义、仓库/树/搜索/安全/治理五组页面、角色可见性、浏览器兼容）
- **管理指南**（`admin/`）
  - [remote / virtual 仓库管理](admin/remote-virtual.md)（建仓字段表、缓存/负缓存/assumed-offline、强刷、SSRF 放行指引、凭据密钥部署、M3 不兼容清单与报错码汇总）— M3
  - [用户组与权限管理](admin/groups-permissions.md)（三步授权流、并集与即时生效、组 CRUD 与 409 保护、`?permissions` 视图、组无 admin 位）— M4
  - [治理：审计、GC 与配额](admin/governance.md)（审计查询与词表、GC dry-run→apply 与互斥 409、quotaBytes 413 语义、includes/excludes 409/404 双值码）— M4
  - [备份与恢复手册](admin/backup-restore.md)（export/import CLI、产物 0700 保管告警、`--verify spot/full`、无钥 fail-fast 恢复链、停机强一致可选）— M4
  - API Token · 监控（随里程碑补齐）
- **API 参考**（`api/`）：Artifactory 兼容子集 + `/api/v1`
- [FAQ 与故障排查](faq.md)（401/403/404/409/413 信封解读、高 QPS 用 Token、M4 不兼容清单、Artifactory 迁移对照表）

## 从 Artifactory 迁移

概念一一对应：local/remote/virtual 仓库、repo key、node、checksum、部署/解析权限——术语不变。
对照表见 [faq.md](faq.md)。
