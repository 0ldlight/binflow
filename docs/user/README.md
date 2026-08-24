# BinFlow 帮助文档中心

> tech-writer 维护。每篇头部标注适用版本。结构如下，未写篇目随里程碑补齐。

## 导航

- **快速开始**（两条最快路径，完整步骤见下方「安装指南」）
  - [5 分钟上手（单二进制）](install/binary.md) — 下载 → `serve` → `/readyz` 200
  - [5 分钟上手（Docker）](install/docker.md) — `docker run` → `/readyz` 200
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
  - [Docker / OCI 镜像](docker-registry.md)（login/push/pull、oras/Helm 承载、podman/crane/skopeo、大层上传跨重启续传、差异清单）— M2/M7
  - [Maven](integrations/maven.md)（settings.xml + deploy/resolve、snapshot/-U、checksum 策略、mirror 收口）— M3
  - [npm](integrations/npm.md)（.npmrc + _auth、publish/install、dist-tag/unpublish、上游边界、发布权限语义〔M8 起；M9 复制同口径〕）— M3
  - [PyPI](integrations/pypi.md)（pip.conf + twine、hash 对账、PEP 691）— M3
  - [Generic / 任意文件](integrations/generic.md)（curl roundtrip）
  - CI 集成：GitHub Actions / GitLab CI / Jenkins 用作依赖源与镜像源
- **Web 控制台** — M8（新信息架构；M9 增补 Set Me Up OIDC 臂与用户删除面）
  - [控制台使用指南](console.md)（双模式导航、跨仓制品树、Set Me Up 与 Deploy 对话框、管理域五分组、旧路径迁移对照、角色可见性、浏览器兼容）
  - [Artifactory → BinFlow 操作路径对照表](artifactory-path-map.md)（建仓/建用户/删用户/配权限/找制品/Set Me Up/GC/备份等逐任务路径对照；无对应面如实登记）
- **管理指南**（`admin/`）
  - [remote / virtual 仓库管理](admin/remote-virtual.md)（建仓字段表、缓存/负缓存/assumed-offline、强刷、SSRF 放行指引、凭据密钥部署、M3 不兼容清单与报错码汇总）— M3
  - [用户组与权限管理](admin/groups-permissions.md)（三步授权流、并集与即时生效、组 CRUD 与 409 保护、`?permissions` 视图、组无 admin 位）— M4
  - [治理：审计、GC 与配额](admin/governance.md)（审计查询与词表、GC dry-run→apply 与互斥 409、quotaBytes 413 语义、includes/excludes 409/404 双值码、用户删除闭环与 last-admin 风险〔M9〕）— M4
  - [备份与恢复手册](admin/backup-restore.md)（export/import CLI、产物 0700 保管告警、`--verify spot/full`、无钥 fail-fast 恢复链、停机强一致可选）— M4
  - [RBAC 角色与仓库级管理员](admin/rbac-roles.md)（角色三值模型与能力矩阵、adminRole wire、manage 派生与覆盖集、`?filter=manage` 可达性〔M9〕、user.role.change 审计、IdP readonly 组映射）— M7
  - [Token 铸造二次认证 step-up](admin/token-step-up.md)（`auth.token_step_up` 开关与 TTL 域、作用域与豁免臂、本地/LDAP 口令腿与 OIDC mint grant 腿〔M9 起控制台自动续铸〕、审计维度）— M7
  - [附录：条件腿真实环境验收](admin/real-env-appendix.md)（V27 真实 AWS S3 / V28 真实 Artifactory 证据归档模板 + MinIO/OSS 等价口径）— M7
- **专题指南**（`guides/`）— M6
  - [OIDC 单点登录配置](guides/oidc-config.md)（auth.oidc 段、PKCE 登录流、组/管理员映射、step-up 联合部署 armed 形态、Keycloak 实例）
  - [LDAP 目录认证配置](guides/ldap-config.md)（auth.ldap 段、先本地后目录回退、ldaps/StartTLS 姿势、OpenLDAP 排障）
  - [S3 对象存储后端与在线迁移](guides/s3-config.md)（storage.s3 段、健康探测、compose --profile s3、双写迁移三步收口）
  - [bf CLI 使用指南](guides/bf-cli.md)（四子命令、~/.bf/config.yaml 多 profile、密钥 env 引用制）
  - [从 Artifactory 迁移（bf-migrate）](guides/migrate-artifactory.md)（三阶段、--dry-run/--resume、口令与 token 不可导出策略）
  - [Prometheus 指标参考](metrics/prometheus-reference.md)（/metrics 端点、四类指标族、path 基数防护、PromQL 示例）
- **API 参考**（`api-reference.md`）：Artifactory 兼容子集 + `/api/v1`（M9 六端点速览：usage 批量 / users 加宽与 enabled / DELETE users / groups includeUsers / permissions filter=manage）
- [FAQ 与故障排查](faq.md)（401/403/404/409/413 信封解读、高 QPS 用 Token、M4 不兼容清单、Artifactory 迁移对照表、M9 增补两问）

## 从 Artifactory 迁移

概念一一对应：local/remote/virtual 仓库、repo key、node、checksum、部署/解析权限——术语不变。
对照表见 [faq.md](faq.md)。
