# 全量功能盘点 · 分区 2：REST + features + 描述符表面（inv-2-surface）

> 来源：`reverse-src/artifactory/src/{batch1-core,batch2-protocol,batch3-addons}`（只读）+ `config-templates/`。
> 版本鉴定（`META-INF/artifactory.version.properties`）：**Artifactory 7.161.11**，revision 86111900，package.handler 5.675.22；控制台为 MFE 微前端（core.mfe 1.1196.3 / rtui 1.43.0 / packages.mfe 1.31.0 / buildinfomfe 2.1.0）。置信度：高（版本文件 + JFrog 官方文档双证）。
> 方法：注解驱动（JAX-RS `@Path`/`@GET`…）扫描 13,365 个 Java 文件中的 resource 类；Spring 描述符与配置模板逐文件阅读。**不复制代码，只记录行为。**

## 0. 表面测绘总览（置信度：高）

| 层 | 挂载前缀 | resource 类数 | 方法级操作数（@GET/@POST…） | 说明 |
|---|---|---|---|---|
| 核心 REST | `/artifactory/api/…` | 103 | ≈475 | `org.artifactory.rest.resource.*` |
| UI REST | `/artifactory/ui/api/v1/…` | 107 | ≈470 | `org.artifactory.ui.rest.resource.*`（服务工厂装配见 §4） |
| 协议子资源 | `/{repoKey}/…`（按包类型分派） | 22 | ≈207 | `org.jfrog.repomd.*`（老一代协议处理层） |
| 新包框架 | `/{repoKey}/api/…`、实例级 `/api/…` | 51 | ≈432 | `com.jfrog.ph.*`（新一代 packages 框架，package.handler 5.675.22） |
| Addon 资源 | `/artifactory/api/…` | 74 | ≈282 | `org.artifactory.addon.*` 各增值域 |
| **合计** | | **357** | **≈1,866** | 端点操作总量估计 **1,800±100** |

Jersey 装配证据：`org.artifactory.rest.servlet.JerseyApplication` 注册扫描包 `org.artifactory;org.jfrog.repomd;com.jfrog.ph;com.jfrog.bh;com.jfrog.build;com.jfrog.distribution.handler;com.jfrog.application.lifecycle;com.jfrog.cs`，启用 `RolesAllowedDynamicFeature`（`@RolesAllowed` 两值 `admin`/`user`），可整体禁用 OPTIONS 方法与 WADL；存在「package handler shadowing」开关（`artifactory.shadowing.enabled`，新旧两套包处理器并行、shadow 模式只记录不生效）。置信度：高。

---

## 1. REST 端点功能族目录（核心 `/api` + UI `/ui/api/v1`）

> 每族一行组：**功能族 | 根路径（证据类）| 行为要点 | 置信度 | BinFlow 覆盖**。BinFlow 现状取自任务简报（五包型 + 三 rclass + pull-through + OIDC/LDAP + RBAC + push 复制 + GC/配额/审计/备份 + step-up token + 双模式控制台 + S3 + Prometheus + bf CLI）。

### A. 制品与存储操作

| 功能族 | 根路径 / 证据 | 行为要点 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| 制品 CRUD/下载 | `storage/{path}`（ArtifactResource）| PUT 上传（checksum/property 参数）、GET 信息、DELETE；`download/{path}` 为 legacy 别名 | 高 | 已有 |
| 复制/移动 | `copy`、`move`、`flat`（Copy/Move/FlatCopyMoveResource）| src/target 参数、dryRun、flat 扁平化布局复制 | 高 | 部分（有 push 复制，无制品级 copy/move/flat API） |
| 回收站 | `trash`（TrashcanResource）| 列出/恢复/清空回收站；`deleteDirectoryTrashProperty` 设空目录进回收站时限 | 高 | 缺失（有 GC，无 trashcan 语义） |
| Zap/清缓存 | `zap/{path}`（ZapArtifactsResource）| 删除物理文件保留元数据（催 GC） | 中 | 缺失 |
| 存储信息 | `storageinfo`、`storagesummary`（StorageSummaryInfoResource、StorageSummaryResource）| 仓库级/汇总存储用量、文件数；UI 有 `binary/providers` 列 binary provider 链 | 高 | 部分（配额有，存储汇总 API 缺） |
| 归档内浏览/下载 | `archive`（ArchiveResource）| 列归档条目、`archive/download/{repoKey}/{path}` 支持从归档内抽单个文件返回（entry 参数） | 高 | 缺失 |
| 统计 | `storage/statistics/{path}`、`v1/stats`（ArtifactStatisticsResource、RemoteRepoStatsResource）| 制品下载计数；远端仓库命中/未命中统计（自带 openapi：remote-repo-stats-api.openapi.yaml 791 行） | 高 | 部分（审计有，下载统计缺） |
| 元数据（新） | `metadata/{path}`（ArtifactMetadataResource）| 制品/仓库级自定义 metadata（区别于 properties）；`metadata_server` 提供重建索引 | 中 | 缺失 |
| 锁内观 | `system/locks`（LocksResource：local、type/local、type）| 查询/清理分布式锁表 | 中 | 缺失（单机无此概念） |
| 版本号检索 | `versions/{repoKey}/{path}`（VersionsResources）| 跨仓库列出路径下所有版本，`_any` 通配仓库名；含 SNAPSHOT/RELEASE 语义 | 高 | 缺失（Maven metadata 内有 version 列表，独立 API 无） |
| 本地生成文件 | `localgenerated/filter`（LocalGeneratedResource）| 按 path 正则过滤列举本地生成文件（如 nuget/npm 索引） | 低 | 缺失 |

### B. 仓库配置管理

| 功能族 | 根路径 / 证据 | 行为要点 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| 仓库 CRUD | `repositories`（RepositoriesResource）| GET 列表（type 过滤）/PUT/POST/DELETE、`{repoKey}/configuration`、`configurations` 批量、`existence` 探测 | 高 | 已有 |
| 仓库 v2 | `v2/repositories`（RepositoriesResourceV2 + batch）| 新版 schema 仓库配置；`v2/repositories/batch` 批量创建/替换（4 操作） | 高 | 部分 |
| 联邦仓库 | `federation`（FederatedRepoResource 53 操作 + status/镜像任务）| federated rclass 多成员 CRUD、成员状态、binary 分发任务、`federation/replaceUrl`、`v2/federation/status` | 高 | 缺失 |
| Grid/仓库快照 | `v1/grid/repos`（GridReposResource）| sync、provision、`snapshot/create`、`snapshot/download/{fileName}` | 低（企业网格预配，语义待动态验证） | 缺失 |
| 布局管理 | `admin/repolayouts`（RepoLayoutsResource）| 自定义 repo layout CRUD（默认 26 种见 §5） | 高 | 缺失（无 layout 概念，五包走固定布局） |
| Yum 索引 | `{repoKey}/reindex` + `yum`（YumResourceSelector）| 异步计算 RPM 索引（async 参数） | 高 | 缺失（无 RPM 包型） |
| GPG 签名 | `gpg`（GpgResource 5 操作）| 公钥分发/私钥设/删 | 中 | 缺失 |

### C. 搜索

| 功能族 | 根路径 / 证据 | 行为要点 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| AQL | `search/aql`（AqlResource）| POST AQL 查询文本，返回结果集（Artifactory Query Language）——**oss 档可用**（活体 7.84.10 实证；`.sort()` 在 OSS 被许可门挡——降级行为详见 aql.md §0-2/§2.5） | 高 | **M15 实现中**（规格就绪 docs/reverse/aql.md，T-407 2026-09-01——就绪度翻转留痕；原「缺失」） |
| 14 种搜索（**T-407 计数定案**：SearchResource 注册恰 14 子资源——铁证；官方 reference 另文档化 archive、latestVersionByProperties 2 枚**外挂**搜索端点，全量 16 归 aql.md §8.1） | `search/{artifact,gavc,prop,usage,creation,dates,pattern,license,checksum,badChecksum,dependency,versions,latestVersion,buildArtifacts}`（SearchResource）| 制品名/坐标/属性/使用方/创建日期/通配/许可/校验和(含坏块)/依赖/版本/最新版本/构建产物 搜索（OSS 7.84.10 活体可用性矩阵与空集语义族分化见 aql.md §8.2——artifact/gavc/prop/usage/creation/dates 可用，pattern/checksum/license/badChecksum/dependency/versions/latestVersion/buildArtifacts 为 Pro 门 400） | 高 | 部分：artifact+checksum 已有（T-92）；**M15 收编 gavc/prop/pattern**（FR-134 断言反转①）；余者 E-26 404 维持 |
| UI 搜索 + 结果暂存 | `artifactsearch`、`stashResults`、`packagesSearch`、`syntax-search`（UI SearchResource 族 + PackagesSearchResource）| 分页/暂存搜索结果（10 操作）、包级搜索（npm/pypi 等 packages 索引） | 高 | 缺失 |

### D. 安全、用户、令牌

| 功能族 | 根路径 / 证据 | 行为要点 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| 用户/组/权限 CRUD | `security/{users,groups,permissions}`、`v2/security`（SecurityResource 15 操作、V2 8 操作）| 实体 CRUD + 权限目标；v2 带分页过滤 | 高 | 已有（对照 gap-endpoints.md 已知回显差异） |
| 授权面 | `security/users/authorization`（SecurityUserResource 6 操作）| 按用户列可访问仓库/权限目标 | 中 | 缺失 |
| 密码策略/锁定 | `security/{passwordSettings,userLockPolicy,unlockUsers,unlockAllUsers,lockedUsers,encryptedPassword}` | 密码过期策略、登录失败递增锁定、加密密码回显 | 高 | 部分（无 lock policy） |
| Token | `security/token`（TokenResource：create/revoke/refresh）|-scoped token 签发（access 集成）、`security/token/revoke` | 高 | 部分（有 step-up token，无通用 scoped token API） |
| API Key | `security/apiKey{id}`（ApiKeyResource 4 操作）| 生成/吊销 API key（兼容老客户端） | 高 | 缺失（设计上可用 token 替代） |
| SSO 家族 | `saml/config`、`saml`（login/logout）、`oauth`（UI 8 操作）+ `oauth2`（OAuthResource 10 操作）、`httpsso`、`crowd`、`system/gateway/saml`、`system/gateway/openid` | SAML SP / OAuth2 授权码+PKCE / OIDC 网关登录回调 / Crowd / 信任 SSO 头 | 高 | 部分（OIDC 有，SAML/OAuth2/Crowd 缺） |
| 密钥对 | `security/keypair`、`security/keys/trusted`、`security/keyPairs`、`security/trustedKeys`、`keystore`、`signingkeys` | GPG/RSA 签名密钥对 CRUD、受信公钥（pgp/tuf） | 高 | 缺失 |
| SSH 服务 | `ssh`、`sshserver`（SshResource、SshServerResource）| SSH 认证 key 列表、服务端 SSH 设置 | 中 | 缺失 |
| 证书 | `system/certificates`、`admin/security/certificates`（CertificatesResource、SslCertificatesResource）| 远端 TLS 证书信任库管理 | 中 | 缺失 |
| LDAP | `ldap`、`ldapgroups`（UI 7+7 操作）| 多 LDAP server 配置 + 组映射 CRUD | 高 | 部分（LDAP 有，组映射待对照 auth-integration.md） |
| 委托登录 | `security/auth`（DelegateLoginResource）| 透传凭据换取会话 | 中 | 已有（等价登录端点） |

### E. 复制 / 分发 / 联邦

| 功能族 | 根路径 / 证据 | 行为要点 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| Push/Pull 复制 | `replication/{repoKey}`、`replications`（15 操作：CRUD、multiple、enable/disable、`channels/establishChannel`、propagateEvents）| push 复制触发、per-repo 复制配置、事件通道（smart replication 握手，见 §3） | 高 | 部分（单向 push 有，双向/事件/通道缺） |
| 全局复制 | `system/replications`（GlobalReplicationResource）+ UI `global/replications/config` | 全局 MULTIPUSH/拉取复制配置 | 中 | 缺失 |
| 镜像/全量同步 | `mirror`、`fullsync`（MirrorResource、FullSyncResource）| 远端镜像拉齐、`fullsync/repo/{fileName}` 文件级同步 | 中 | 缺失 |
| Release Bundle（Distribution）| `release`（v1 19 操作：bundle/transaction open-close/store/status、fat_manifest_content、bundles CRUD+artifacts）、`v2/release`（3 操作）、UI `bundles` | 分布式发布包：事务式创建、签名分发、目标节点状态、AirGap 变体 | 高 | 缺失 |
| 文件清单任务 | `fileList`（FileListTaskResource）| 复制用文件清单生成/查询 | 低 | 缺失 |

### F. 生命周期治理（cleanup/retention/archive）

| 功能族 | 根路径 / 证据 | 行为要点 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| 清理 v1 | `cleanup`（CleanupPolicyResource）+ `cleanup-ui/packages/policies` | 老版清理策略 | 中 | 缺失（GC 只管孤儿块） |
| 清理 v2 策略引擎 | `cleanup/packages`（8 操作 + `policies/runs` 6 + `policies/view`）、`cleanup/builds`、`cleanup/bundles`、`retention/proxy/v1`、`supportedPackages/full` | 策略 CRUD + 手动 run + dry-run view + 按包类型枚举受支持清理对象 | 高 | 缺失 |
| 保留 v2 | `retention`（ArtifactRetentionResource 13 操作）、`artifactRetention/archive/policies`（11 操作） | 制品保留策略（区别于清理：保多删少）+ 归档联动 | 中 | 缺失 |
| 保留工具面 | `retentionTools/{repositories,coverage,policies}`、`retention/migration` | 覆盖率/失败/影响分析、v1→v2 迁移 | 低 | 缺失 |
| 冷存储归档 | `archive/v2/packages`（策略 8 操作 + runs 6 + view）、`archive/v2/{restore,cold,consumption}` | 归档到冷层、restore 取回、用量统计 | 中 | 缺失 |
| Curation | `curation`（CurationResource 8 操作：types/repositories/status/settings/invalidatePackageStatusCache/audit/interfaceSettings）| 远端包下载前拦截审计（curation 服务集成） | 中 | 缺失 |

### G. 构建信息（Build Info）

| 功能族 | 根路径 / 证据 | 行为要点 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| Build CRUD | `/api/build`（随产品发布 openapi.yaml，996 行：GET 全量/PUT 上传/DELETE 批删/POST `/build/delete` 多删/rename/append/retention/promote/diff）| buildinfo JSON 持久化、module↔artifact 按 sha1/md5 关联、promotion 支持 copy/move+scope+properties+dryRun+failFast、Docker 镜像提升 `docker/{repoKey}/v2/promote` | 高 | 缺失（整个 build-info 域） |

### H. 系统管理 / 运维

| 功能族 | 根路径 / 证据 | 行为要点 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| 系统状态 | `system/ping`、`system/version`、`system/status`、`system/usage`、`system/service`、`system/service_id`、`system/serverTime` | 探活/版本/HA 健康/使用上报 | 高 | 部分 |
| 配置描述符 | `system/configuration`（GET 原文/POST 更新，支持 rpm/debian/ha 子段部分更新）、`system/security`、`configdescriptor`、`securitydescriptor`、`system/property` | 整体 config.xml 原文读写 + 子树级替换 | 高 | 部分（有配置 API，无原文描述符往返） |
| 加解密 | `system/encrypt`、`system/decrypt`、`crypto{action}` | 全库敏感字段加密/解密（master key） | 高 | 缺失 |
| sha2 迁移 | `system/migration/sha2/{start,stop}`、`ha/migration/sha2/stop` | sha1→sha256 主校验迁移任务 | 中 | 缺失（BinFlow 原生 sha256） |
| 备份/导入导出 | `backup{id}`（5 操作）、`import`、`export`、UI `artifactimport/artifactexport` | cron 备份（默认 daily+weekly 两条见 §5）、系统级与仓库级导入导出 | 高 | 部分（备份有，导入导出对照 import-export-api.md） |
| Support Bundle | `system/support/bundle{s}`（7 操作，含 currentnode、archive 下载）| 一键收集诊断包 | 高 | 缺失 |
| 反向代理 | `system/configuration/webServer`、`system/configuration/reverseProxy`（snippet 生成）、UI `reverseProxies` | nginx/apache 配置片段生成（模板证据：templates/nginx.ftl 等） | 高 | 缺失 |
| 邮件 | `system/configuration/platform/mail`、UI `mail`、`send_mail` | SMTP 设置 + 测试/任意发送 | 中 | 缺失 |
| 代理服务器 | `system/configuration/platform/proxies`、UI `proxies` | 出口代理 CRUD | 中 | 缺失 |
| 许可证 | `system/license`、`system/licenses`、UI `licenses/manageLicenses/licenseexport/registerlicense`、`subscription` | 许可证安装/集群三节点哈希/卸载 | 高 | 不适用（开源实现无 license；但 edition 门控语义见 §3–4） |
| HA 管理 | `ha`（RestHaResource 30 操作：propagateTask、config/descriptor/repoconfig/policy/security change 广播、syncMasterKey、syncStorageSummary/Usage、rate limiter 重算、thread_dump、events shift）、UI `highAvailability`、`system/nodes` | 集群内配置广播与状态同步 | 高 | 缺失（单机） |
| 维护模式 | `maintenance`（6 操作）、`system/loadhealer`、`v1/system`（PreStopHook）| 维护窗口设置、Load Healer 开关、K8s pre-stop 钩子 | 中 | 缺失 |
| 任务/作业 | `tasks`、`jobs`、`/v1/workers`、`traffic`（3 操作） | 后台任务列表、job 触发、traffic 日志开关 | 中 | 部分（GC 任务有 API 面） |
| 日志 | `system/logs`（tail）、UI `systemlogs/v1/system/logs`、`events/log` | 远程日志流式查看 | 高 | 部分 |
| 索引器 | `indexer`（MavenIndexerResource 3 操作）| Maven 索引 cron/手动触发 | 高 | 缺失（无索引器） |
| 限流 | `v1/system/query_rate_limiter`、`v2/system/request_rate_limiter`（3 操作）、`system/inboundRequestGuardTriggers`、`system/remoteRepoOfflineTriggers` | 两级限流器（查询级/请求级）+ 入站请求守卫（OFF/SHADOW/ACTIVE 三模式，按 header/User-Agent/IP-CIDR 拒绝，自带 openapi 174 行）+ 远端仓库离线熔断触发器（openapi 348 行） | 高 | 缺失 |
| 观测性增值 | `v1/system/logs/{billing,generic,usage}`（billing 1/log shipping 1/usage 7 操作）| 日志外发（log shipping）、用量上报、计费事件 | 中 | 缺失 |
| SumoLogic | `sumologic`（7 操作）| 日志分析集成注册/重置 | 中 | 缺失 |
| 调试 | `system/debug`（4 操作）、`devopsagent/errors`、`devopsagent/requests` | 线程栈/日志级别调试、请求日志目录 | 中 | 缺失 |
| 校验 | `system/checkup`（2 操作）| 系统自检 | 低 | 缺失 |
| 事件订阅 | `/v1/unifiedevent/registration`（4 操作）、`events`（JfEventResource/JfEventUIResource）、`internal/events` | 统一事件消费者注册/注销（webhook 类）、内部事件总线 | 中 | 缺失 |
| 服务信任 | `/v1/service_trust/`（6 操作：pairings/pairing/exchange/revoke） | 服务间 mTLS 配对与信任交换（联邦前置） | 中 | 缺失 |
| Multipart Upload | `v1/uploads/{create,config,urlPart,complete,status,abort}` | 浏览器直传 S3 的 MPU 会话管理（对照 BinFlow upload_sessions 表，语义同族） | 高 | 已有（T-209 同域） |
| Projects | UI `projects`（REST 面在 Access 侧）+ `internal/storage/projects/{projectKey}` | 项目域存储隔离查询 | 中 | 缺失 |
| Onboarding | UI `onboarding`、`onboarding/quickstart` | 首次向导状态、quickstart 模板 | 中 | 部分（双模式控制台首启流可对照） |
| SetMeUp | UI `setMeUp`（20 操作） | 按仓库/包类型生成客户端配置片段（gradle/maven/npm/pip/docker…，模板证据 build.gradle.*.template 12 个） | 高 | 部分（bf CLI 提供部分等价能力） |
| JCR | UI `jcr/{eula,subscription,xray}` | Jackrabbit JCR 订阅与 EULA（jcr edition） | 低 | 不适用 |
| VCS 仓库 | `vcs`（VcsResourceSelector 21 操作：refs/tags/branches 下载、按 checksum 归档） | 把远端 git 当仓库浏览/下载 | 中 | 缺失 |
| CI 集成 | `pipelines`（PipelinesResource） | JFrog Pipelines 同步资源 | 低 | 不适用 |
| 配置服务 | `config`（ConfigsServiceResource：token 化 config 服务）、UI `generalConfig`（10 操作）、`basicConfig`、`metadataMigrationStatus` | 平台级设置（baseUrl/logoUrl/mail/proxies/security 的 `/api/system/configuration/platform/*` 面） | 高 | 部分 |

### I. 用户插件（User Plugins）

| 功能族 | 根路径 / 证据 | 行为要点 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| Groovy 插件 | `plugins`（PluginsResource 13 操作：status、`execute/{executionName}` GET+POST、`build/staging/{strategy}`、`build/promote/{name}/{build}/{number}`、`{scriptName}` GET 源码、reload、`download/{pluginName}`） | 服务器端 Groovy 脚本生命周期执行、staging 策略、构建级 promotion 钩子、热重载（`artifactory.plugin.scripts.refreshIntervalSecs`） | 高 | 缺失 |

### J. 包类型协议面（`/{repoKey}/…`，org.jfrog.repomd 老层 + com.jfrog.ph 新层）

| 包类型 | 证据 | 行为要点 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| Docker/OCI | DockerV2Resource（17 操作：blob/manifest 上传下载 HEAD、uploads 会话、_catalog、tag list）、DockerSubResource（26 操作：v1 兼容、promote、`listTag` 等）、OciResource、`docker/api/{repoKey}` UI 面 | Registry v2 全套 + v1 兼容 + 镜像提升 + catalog | 高 | 已有（v2）；v1 兼容与 promote API 缺 |
| Maven/Ivy/Gradle/SBT | MavenResource（3 操作：`compare`、`calculate/{path}` 元数据重算）；布局见 §5 | 元数据计算/比对触发 | 高 | 已有（元数据计算）；compare 缺 |
| npm | com.jfrog.ph NpmResource（42 操作：`/-/ping`、dist-tags、`/{name}` 版本 CRUD、scope `@{scope}/{name}`、tarball 下载、`-/@` 路径变体） | 完整 npm registry 语义（publish/unpublish/dist-tags/star?） | 高 | 已有 |
| PyPI | com.jfrog.ph PypiResource（17 操作：simple 索引、`packages/` 直下、upload） | PEP 503 simple + 上传 | 高 | 已有 |
| NuGet v2/v3 | NuGetSubResource（17）/NuGetV3SubResource（13）/ph NugetV2+V3Resource | OData v2 + SearchQueryService v3 双协议 | 高 | 缺失 |
| Conan v1/v2 | repomd conan 5 子资源（users/conans/updown/root）+ ConanV2Resource（19）+ ph ConanResource（37） | v1 `/api/conan/…` 与 v2 revisions 全套 | 高 | 缺失 |
| Terraform | ph registry（module/provider/namespace/network mirror 4+4+5）+ backend（`.well-known/terraform.json` 服务发现、organizations/workspaces/state-versions v2 API） | registry + remote state backend 双能力 | 高 | 缺失 |
| Go/Helm/Cargo/Composer/Conda/CRAN/GitLFS/Hex/Nix/LuaRocks/Swift/Pub/Bazel/Alpine/Debian/RPM/Yum/Opkg/Chef/Puppet/CocoaPods/Bower/Vagrant/Ansible/JetBrains/Skills/HuggingFace/ML/NimModel/AgentPlugins/AgentPackages/AIEditorExtensions | repomd 各 SubResource + ph 各 Resource（HuggingFace 42 操作、Npm 42、Vagrant、Nix 13 含 `.narinfo`…） | 每包型独立协议栈（28+ 种），新框架统一 `api/packages` 风格 + 包信息/版本/下载三段式 | 高 | 缺失（BinFlow 仅五包型） |
| 迁移器 | `com.jfrog.ph.migrator`：`/v1/migrations`（13 操作）、`/migration/metrics` | 内嵌包迁移作业（source→Artifactory） | 中 | 部分（bf 迁移工具为 CLI 侧，无 REST 作业面） |

### K. UI 专属面（`/ui/api/v1`，仅列族）

树浏览（`treebrowser`/`v2/treebrowser`/`nativeBrowser`）、Tab 数据（`artifactgeneral`/`checksums`/`artifactproperties`/`artifactpermissions`/`artifactwatches`/`dependencydeclaration`/`filteredResource`/`generalTabLicenses`/`views/{alpine,nuget,swift}` 18 操作/`archiveViewSource`）、制品动作（`artifactactions` 19 操作：复制/移动/删除/watch/删除旧版本）、部署（UI `artifact` 5 操作含 exploded archive）、包/版本原生浏览（`v1/native/packages` 8、`v1/native/versions` 10、`v1/native/repos`）、首页 widget（`home/widget` 5）、验证（`validations` 6）、工具（`crontime`、`predefinevalues`、`repopropertyset`、`autoCompleteRepodata`、`pagedRepodata`、`repodata`）、登录屏（`auth/screen`）。置信度：高。BinFlow 覆盖：部分（双模式控制台已有浏览/部署，widget/watch/views 族缺）。

---

## 2. features 目录：版本门控特性（smartrepo-features.xml）

`src/batch1-core/features/smartrepo-features.xml`——**按版本/revision 协商的特性开关**，用于 smart remote replication 双端能力判定（对端版本低于 availableFrom 则关闭该特性）：

| 特性 | 起始版本 / revision | 行为 | 置信度 |
|---|---|---|---|
| SYNC_PROPERTIES | 4.1.0 / 40011 | 复制时同步属性 | 高（+官方 replication 文档） |
| LIST_CONTENT | 4.1.0 / 40011 | 远端内容列举 | 高 |
| SYNC_STATISTICS | 4.2.0 / 40030 | 下载统计同步 | 高 |
| DETECT_ORIGIN_ABSENCE | 4.3.3 / 40071 | 检测 origin 标记缺失（防回环复制） | 中 |
| EVENT_BASED_PULL_REPLICATION | 5.4.0-m001 / 50050 | 事件驱动 pull 复制 | 高 |
| REPLICATION_INCLUDE_EXCLUDE_PATTERNS | 7.27.4 / 0 | 复制路径 include/exclude 过滤 | 高 |

BinFlow 覆盖：缺失——pull-through 拉取无任何特性协商层。这提示 BinFlow 若做多级链式 pull-through 需等价机制（能力握手，避免老对端行为未定义）。

## 3. Addon 注册表：license 门控功能全集（AddonType 枚举，80 项）

`org.artifactory.addon.AddonType`（batch1-core）是**功能 → license 档位**的唯一权威表。档位取值：`oss`（含在社区版）/ `pro` / `ent`（企业）/ 特殊（`aol`、`all`、`jfrog-cli`）。运行时可 `artifactory.addons.disabled=<csv>` 整体禁用某 addon（artifactory.system.properties 证据）。

分组摘录（完整 80 项见枚举，此处按档位归类）：

- **oss 档**：SSH、AQL、SBT、Ivy 插件、Maven/Gradle/Jenkins/Bamboo/TeamCity/MSBuild 插件、Bintray 集成、JFrog CLI、SumoLogic、Distribution（repo 类型）
- **pro 档（功能类）**：Build Integration、License Control、Advanced REST、LDAP Groups、Replication、Properties、Smart Searches、User Plugins、Repository Layouts、Filtered Resources、Watches、Jar Signing(WebStart)、OAuth、Smart Remote Repo、Observability
- **pro 档（包类型类，与 §1.J 对应）**：RPM/YUM、NuGet、Gems、npm、Bower、CocoaPods、Conan、Debian、Docker、Vagrant、VCS、GitLFS、PyPI、Puppet、Opkg、Composer、Chef、Helm、Cargo、Alpine、Go、CRAN、Conda、Pub、Swift、Terraform、OCI、Bazel Modules、Nix、LuaRocks、HuggingFaceML、MachineLearning、HelmOCI、Ansible、Hex、NimModel、Jem、AI-Editor Extensions、JetBrains Plugins、Skills、Agent Plugins、Agent Packages、P2
- **ent 档**：HA、Multipush Replication、S3 Object Store、GCS、Sharding
- **生态类**：Xray、Distribution（Bintray）、Tracker

版本附加：产品另有 editions（ascii-editions.txt）：`default / oss / pro / ha / jcr`。batch2/batch3 的 `META-INF/addon.{xml,properties}` 示范了 addon 装配单元：oci（display.ordinal 150）、rpm（750）——**addon = 一个 Spring bean 集 + properties 描述 + license 档**。置信度：高（枚举 + addon.properties 双证）。

BinFlow 覆盖：不适用（无商业 license），但该表即「Artifactory 认为可独立开关的功能全集」——是功能清单的边界定义。

## 4. Spring 描述符（bean 装配 = 服务表面）

`META-INF/spring/`（batch1-core）：

- `restContext.xml`：UI REST 的**服务工厂注册表**（17 个工厂）——SecurityServiceFactory、ConfigServiceFactory、ServicesServiceFactory、BrowseServiceFactory、AdvancedServiceFactory、UtilsServiceFactory、SearchServiceFactory、NativeSearchServiceFactory、DeployServiceFactory、ImportExportServiceFactory、ConfigServiceFactory、RepoServiceFactory、GeneralServiceFactory、OnboardingServiceFactory、HomePageServiceFactory、ValidationsServiceFactory、ReleaseBundleServiceFactory、TrustedKeysServiceFactory、KeyPairsServiceFactory。→ UI REST 的功能边界 = 这 19 个服务域。置信度：高。
- `applicationContext.xml`（90 行）、`security.xml`（114 行）、`scheduling.xml`（52 行）、`interceptors.xml`（38 行）：核心 bean/安全过滤器链/调度器/请求拦截器装配（内容与既有 auth-model.md / rbac-model.md 重叠，不重复）。
- `addons.xml`：**空**（OSS 构建无 addon bean）；企业 addon 由各 addon jar 的 `META-INF/addon.xml` 提供（batch2=oci、batch3=rpm 例证）。
- `spring.components`（各 batch 仅 2–4 行）：Spring 索引文件，表明组件扫描走编译期索引。

## 5. config-templates：默认配置揭示的行为开关（高置信度）

`artifactory.config.xml`（默认 config descriptor，291 行）：

- **security**：`hideUnauthorizedResources=false`、密码过期策略（disabled，maxAge 60 天，邮件通知）、`buildGlobalBasicRead{,ForAnonymous}=false`
- **backups**：预置两条——`backup-daily`（MON-FRI 02:00，retention 0=增量 current 目录）+ `backup-weekly`（SAT 02:00，保留 336h，默认 disabled）
- **indexer**：cron `0 23 5 * * ?`（每日 05:23 Maven 索引）
- **reverseProxies**：预置 `direct` 模板（subdomain/path 两种 docker 方法、http 8080/https 443）
- **repoLayouts**：内置 26 种布局——maven-2/ivy/maven-1/nuget/npm/bower/vcs/sbt/simple/cargo/composer/conan/puppet/go/build-default/terraform-module/terraform-provider/swift/ansible/sbt-ivy/nix（`binary-cache/[narinfoHash]/[narinfoHash].narinfo`）/skills/agent-plugins/agent-packages/luarocks。每布局 = artifactPathPattern + 可选 descriptorPathPattern + folder/fileIntegrationRevisionRegExp
- **gcConfig** cron `0 0 /4 * * ?`（每 4h）；**cleanupConfig** 每日 05:12；**virtualCacheCleanupConfig** 每日 00:12（注意模板注释与 cron 不一致：cron 为 `0 12 0`）
- **folderDownloadConfig**：默认 disabled；启用后 maxDownloadSizeMb=1024、maxFiles=5000、maxConcurrentRequests=10、匿名单独开关、空目录开关 → **整目录打包下载**功能
- **trashcanConfig**：enabled、retention 14 天、allowPermDeletes=false

`binarystore.xml` 模板：默认 `chain template="file-system"`。`mimetypes.xml`/`logback.xml`：92/611 行行为配置。`artifactory.system.properties`（300 行）：≈120 个可调系统开关，值得注意的行为参数——GC 六个节流参数、锁超时 120s、认证缓存 300s、登录失败递增锁定（`(n-3)*500ms`，上限 5s）、remember-me 14 天、搜索上限 500/用户查询上限 1000、`artifactory.repo.global.disabled=true`（禁用全局 repo 虚拟根）、Maven central 识别 pattern、NuGet 强制认证开关、`explodedArchiveExtensions=zip,tar,tar.gz,tgz`（**捆绑解包上传**）、远端 URL 白名单/严格策略（默认关）、导入并行度、support bundle 上限 5 个。

`application.yaml`（batch1-core 根）：**Load Healer** 活动阈值表——xrayBlockUnscanned、xrayDownloadBlocked、qrlLowPriorityRequests、curationCachedPackageStatus、parallelUploadPreRepoKey、parallelDownloadUploadPerPackageType、limitUser、limitModule、limitEndPoint、limitActivity；阈值以 `tomcat.connector.maxThreads` 的百分比（50%–90%）表达，healthyTimeout 60s。→ 过载自愈：达阈值自动降级对应活动。置信度：高。

随包 OpenAPI 资产：`rest/resource/build/openapi.yaml`（Build API 996 行）、`inbound-request-guard-api.openapi.yaml`、`remote-repo-offline-guard-api.openapi.yaml`、`remote-repo-stats-api.openapi.yaml` —— 产品自带规范文档，可直接作为 BinFlow 对照实现契约。

`templates/`：nginx.ftl / apache.ftl / nginx_router.ftl / apache_router.ftl（反向代理片段生成模板）、`defaultRepository.json`、`artifactory.config.template.yml`。`build.gradle.*.template` ×12（SetMeUp 客户端片段）。`.proto` ×16（access 集成：token/user/group/permission(v1/v2)/project(v1/v2)/secret/sync/system/lock/customization/authentication）→ **与 Access 服务走 gRPC/protobuf 通道**的证据。

---

## 6. 待验证清单（低置信度项）

1. Grid（`v1/grid/repos` snapshot/provision）与 Jem addon 的确切语义（仅见资源类，未见服务端流转）。
2. `localgenerated/filter`、`system/checkup`、`config`（ConfigsServiceResource token 化配置）行为细节。
3. ShadowResponseFilter/ShadowModelBinder 的「handler shadowing」切换条件（新包框架灰度发布机制，仅见开关名）。
4. com.jfrog.ph 各包型的实例级挂载前缀（`/api/packages` 之外是否还有全局路由）。
5. `retentionTools/*`（coverage/impact）计算口径。
6. virtualCacheCleanupConfig 模板 cron 与注释不符（05:00 vs 00:12）——以 cron 为准还是注释为准。
