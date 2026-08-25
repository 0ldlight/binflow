# 全量功能盘点 · 分区 1：核心服务面（batch1-core）

> 2026-08-25 · reverse-engineer · 材料：`reverse-src/artifactory/src/batch1-core`（org.artifactory 主树，含 rest/ 与 ui/rest/ 全部 resource 类）。
> 性质：**功能目录**（广度优先），每条 = 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖。深度规格见既有各分册（rest-api / storage-layout / auth-model 等），本文不重复展开。
> 置信度：高 = 代码 + 公开文档双证；中 = 仅代码；低 = 混淆/推断，待动态验证。
> BinFlow 覆盖基线：五包型 + 三 rclass + pull-through + OIDC/LDAP + RBAC + manage 派生 + push 复制 + GC/配额/审计/备份 + step-up token + 双模式控制台 + S3 + Prometheus + bf CLI/迁移工具（BOARD.md M1~M9）。

## A. 安全与信任

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| Access 服务化认证 | 7.x 认证主干委托 Access：用户名密码/token 都走 AccessClient；本地仅存缓存用户；scope 令牌模型（applied-permissions / member-of-groups / repo-path / system-admin） | `o.a.security.access.AccessRestClient(Impl)`、`AccessTokenAuthenticationProvider`、`AccessUserPassAuthenticationProvider`、`AppliedPermissionsScopeToken` 等 | 高 | 部分：自建认证（OIDC/LDAP+本地），无 Access 中枢属设计差异 |
| Token 服务 | `POST /api/security/token`（grant_type=client_credentials/refresh_token/password 三种，代码枚举闭集）、`GET` 列表（admin）、`POST revoke`（form 参数 token/token_id/token_type_hint）。刷新语义见 auth-model.md | `rest/resource/token/TokenResource.java`、`token/GrantType.java` | 高 | 已有（含 step-up 扩展，超出参考） |
| API Key | `GET/PUT(再生)/POST(建)/DELETE(吊销) /api/security/apiKey{id?}`；UI 侧取回需密码复认 | `rest/resource/userprofile/ApiKeyResource.java`、`ui/.../apikey/{UserApiKeyResource,PasswordProtectedResource}` | 高 | 缺失：无 API key（7.x 已标记废弃，可不做） |
| 密码过期策略+邮件通知 | 策略开启且 notifyByEmail 时定时扫描临期用户逐个发信；凭据看护 job 巡检凭据变化 | `security/jobs/PasswordExpireNotificationJob.java`、`CredentialsWatchJob.java` | 中 | 缺失：无密码过期策略 |
| 主加密/解密 | 首次启动对描述符整体加密；可手动 encrypt/decrypt；远程仓库密码单独加解密并静默回写；binarystore.xml 由 BinaryProviderConfigEncrypter 加密 | `security/ArtifactoryEncryptionServiceImpl.java`（监听 NewDbInstallation） | 高 | 部分：有密钥管理，无「配置文件整体加解密」操作面 |
| 敏感字段脱敏体系 | `@DynamicSensitive` 注解 + 字段处理器在 diff/序列化时掩码；CryptoServiceImpl 统一加解密 | `o.a.sensitive.*`（SensitiveFieldCollector、DynamicSensitiveFieldHandler 等） | 中 | 缺失：配置回显无系统性脱敏框架 |
| Service Trust / Token Exchange | JFrog 服务间配对：按 usecase 签发/吊销 pairing token，列出已配对 baseUrl；支持 OAuth token exchange（RFC 8693 风格 grant） | `rest/resource/servicetrust/ServiceTrustResource.java`（/v1/service_trust/pairing|pairings|revoke）、`system/TokenExchangeRequest.java`、`servicetrust/cold|federation` | 中 | 缺失：多服务信任域（单体可不建） |
| 签名 URL | URL 签名密钥随机生成、加密后落 configs 存储（`artifactory.security.url.signing.key`）；对下载 URL 签发/校验 | `signed/url/SignedUrlServiceImpl.java`、`signature/SignedUrlService.java` | 中 | 缺失：无签名下载 URL（S3 presigned 可部分替代） |
| SSH 认证（服务器侧） | 上传/替换 ssh 公私钥（`PUT /api/ssh/key/public|private`、GET 公钥）；UI 有 ssh-server 设置页 | `rest/resource/ssh/SshResource.java`、`ui/.../sshserver/SshServerResource` | 中 | 缺失 |
| 密钥对/受信密钥 | 仓库级 RSA keypair（SSH 认证用）+ gpg 受信密钥（校验远端签名元数据）管理 | `keys/KeyPairsUtil.java`、`keys/TrustedKeysServiceImpl.java`、`ui/.../keys/{KeyPairsUIResource,TrustedKeysUIResource}` | 中 | 缺失 |
| 签名密钥（包元数据签名） | gpg 签名密钥管理：debian/rpm 仓库元数据签名（KeyStore） | `ui/.../signingkeys/{SigningKeysResource,KeyStoreResource}` | 中 | 缺失 |
| 证书信任库 | 远程仓库 SSL 证书导入/列出/删除（`/api/system/certificates` 族） | `rest/resource/system/CertificatesResource.java`、`CertificateInfoRestModel` | 高 | 缺失：remote TLS 自定义 CA 不可配 |
| 代理认证（Delegate login） | `POST /api/security/auth/login` 返回用户认证详情（供网关/代理二次校验） | `rest/resource/security/DelegateLoginResource.java`、`AuthDelegationHandler` | 中 | 缺失 |
| 反向代理配置+代码段 | webServer 配置 CRUD（`/api/system/configuration/webServer`）+ 按类型（nginx/docker 等）生成 snippet | `reverseproxies/{ReverseProxiesResource,ReverseProxiesSnippetResource}` | 高 | 缺失：部署文档手工给配置，无生成器 |
| SSO 家族 | SAML（含 SamlGateway）、Crowd SSO、HTTP header SSO、OAuth/GitHubEnterprise 认证 provider | `security/authentication/GitHubEnterpriseAuthenticationProvider.java`、`ui/.../{saml,crowdsso,httpsso,oauth}` | 中 | 部分：有 OIDC/LDAP（auth-integration.md 覆盖 LDAP/OAuth 面）；SAML/Crowd/GHE 缺 |
| 匿名访问 | 匿名 token + 匿名权限面（auth-model.md 已载） | `security/AnonymousAuthenticationToken.java` | 高 | 已有 |
| 审计日志 | 安全/仓库操作审计（BinFlow 已有审计面；参考行为见既有分册） | `security/AccessLogger.java`（approved/rejected 双流） | 高 | 已有 |

## B. 存储与二进制

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| 文件存储 VFS | 目录树 + 事务会话 + workspace 暂存 + stats/checksum 等子元数据（storage-layout.md 已载） | `storage/fs/*`（VfsItem 族、StorageTx） | 高 | 已有 |
| 全 DB 形态 | 元数据全在 DB（nodes/props/stats…）；多库支持：derby/postgresql/mysql/oracle/mssql（根目录同名 SQL 资源）；版本化 conversion v213/v227/v228 | `storage/db/*`、根目录 `{derby,postgresql,mysql,oracle,mssql}/`、`db/conversion/version/*` | 高 | 已有（单 DB 形态，多库矩阵窄） |
| DAO 目录（数据面清单） | aql、bundle、devopsagent、event、federated、federation、filelist、fs、keys、locks、policy、postgresql、properties、remote stats、repo、search、security、servers、statistics、storage cache、support database、tasks、traffic、xray ——即 DB 表族目录 | `storage/db/*/dao` | 中 | 部分：核心表有；policy/event/traffic/locks 等无 |
| GC（标记+清理） | cron 驱动（GcConfigDescriptor.cronExp）；`POST /api/system/storage/gc`（HA 需全节点确认）；prune 由 FilestorePrunerJob→binaryStore.prune(PruneRequestModel)（waitForNodes/countGcCandidates 参数化）；`GET prune/status` 可查进度 | `storage/jobs/FilestorePrunerJob.java`、`rest/resource/system/StorageResource.java`、`descriptor/gc/GcConfigDescriptor.java` | 高 | 已有（graceHours 语义自定，见 BOARD） |
| Trash can | 内置 local 仓 `auto-trashcan`；删除时拷入并打标（trash.time/deletedBy/originalRepository/originalPath/restoredTime）；保留期校验；`POST /api/trash/empty`、`POST /api/trash/restore/{path}?to&transaction-size`、`DELETE /api/trash/clean/{path}`；目录级 trash 属性批量清理 job | `repo/trash/{TrashService,TrashGCHelper}`、`rest/resource/trash/{TrashcanResource,RemoveDirsTrashcanTimeResource}`、`descriptor/trashcan/TrashcanConfigDescriptor` | 高 | **缺失**：删除即永久，无回收站 |
| Multipart Upload（直传云） | `POST/GET v1/uploads/{create,config,urlPart,complete,status,abort}`：客户端直传 S3 的 MPU 会话编排（预签名 part URL、完成/中止/状态） | `rest/resource/mpu/*`、`o.a.mpu` | 高 | 缺失：S3 后端走服务端中转（无直传） |
| 存储汇总 | filestore/binaries/repo 三层 summary + 15min 缓存（gap-endpoints.md §5 已载） | `storage/{StorageSummary*,FileStoreSummary}`、`storagesummary/StorageSummaryInfoResource` | 高 | 部分：有 storageinfo 等价面 |
| 磁盘配额 | 阈值告警 + 邮件通知；项目级配额通知 job | `descriptor/quota/QuotaConfigDescriptor.java`、`projects/ProjectStorageQuotaNotificationJob.java` | 高 | 已有（实例级）；项目级 N/A |
| 校验和体系 | sha1+sha256 双校验；sha1-only 老数据按需补算/迁移任务；`POST /api/checksum/sha256`（补算指定路径） | `sha2/Sha256MigrationTaskHelper(Impl).java`、`rest/resource/ChecksumResource.java` | 中 | 部分：双校验已有；按需补算端点无 |
| 存储后台 job 族 | 用量同步、repo/project summary 计算、stats 落盘 flush、sharding 均衡、禁用 URL 巡检、remote offline 守护、仓库配置持久化、metric 更新 | `storage/jobs/*`（SyncStorageUsageJob、CalculateReposStorageSummaryJob、ShardingBalancerJob、ProhibitedUrlsJob 等） | 中 | 部分：有 GC/sweep，其余无 |
| In-transit 加密拦截 | 文件存储/复制流量的传输层加密拦截器 | `intransit/interceptor/IntransitInterceptor.java` | 低 | 未知对位 |
| 导出 DS / 压缩 | `POST /api/system/storage/exportds`（调试导出数据存储）、`POST /api/system/storage/compress`（内部数据压缩） | `rest/resource/system/StorageResource.java` | 中 | 缺失 |
| 双写/影子请求对比 | 将请求镜像发往 shadow 实例，比对 status/headers/location 并产出指标（迁移验证工具，`/api/shadow`） | `shadow/{ShadowRequestService,ShadowMetricRegistry}` | 低 | 缺失（可选不做） |
| 重查询缓存（HQC） | heavy query cache（fullsync 侧）配置与类型 | `hqc/{HeavyQueryCacheAppConfig,enums.HeavyQueryCacheType}` | 低 | 无对位 |

## C. 仓库模型与仓库运维

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| rclass 四态 | local / remote / virtual / **federated**；另有内部 release-bundles 仓型；trashcan 亦为内部 local 仓 | `repo/{LocalRepo,HttpRepo,remote/…,federated}`、`descriptor/repo/{FederatedRepoDescriptor,ReleaseBundlesRepositoryConfiguration}` | 高 | 部分：前三已有，federated 缺（replication.md 已载概念） |
| 包型枚举 | **57 个 PackageType**（docker…machinelearning/ansible/hex/nimmodel/jem/vscodeextensions/jetbrains/bazelmodules/nix/luarocks/skills/agentplugins 等） | `common/repo/PackageType.java` | 高 | 部分：5 包型（对标 OSS 核心集） |
| 自定义仓库布局 | 模块路径模式（token 正则）；UI REST `admin/repolayouts` CRUD + `testArtPath`（路径试解析）+ `resolveRegex` | `ui/.../layouts/RepoLayoutsResource.java`、`o.a.layout` | 高 | 缺失：布局内置不可自定义 |
| 远程仓库守护 | offline guard job + 触发端点（可手动标不可用/恢复）；带独立 openapi 描述 | `storage/jobs/RemoteRepoOfflineGuardJob.java`、`admin/RemoteRepoOfflineTriggersResource.java`、根 `remote-repo-offline-guard-api.openapi.yaml` | 中 | 缺失：无远端熔断标记面 |
| 远程统计 | `GET/POST v1/stats/remotes`：远端响应统计（成功率/延迟） | `rest/resource/stats/RemoteRepoStatsResource.java`、`storage/db/remote/stats` | 中 | 缺失 |
| CDN/下载重定向 | 仓库级 CDN redirect 与 download redirect（直接 302 到云/CDN） | `repo/{CdnRedirectRepoConfig,DownloadRedirectRepoConfig}.java` | 中 | 部分：S3 presigned 重定向已有，CDN 层无 |
| 外部依赖重写 | virtual 仓库 external dependencies 重写规则（构建工具拉外部依赖时改道） | `repo/ExternalDependenciesConfig.java`、`descriptor/repo/ExternalDependenciesConfig` | 中 | 缺失 |
| 仓库批量操作 | `v2/repositories/batch` PUT/GET/POST/DELETE（一次多仓创建/校验/更新/删除，含批量删除报告） | `repositories/RepositoriesBatchResource.java`、`api/common/RepoBulkRemovalReport.java` | 中 | 缺失 |
| Yum 元数据计算 | `POST /api/yum/{repoKey}?async&path`：异步计算 yum 元数据 | `repositories/YumResource.java` | 高 | N/A（无 yum 包型） |
| Maven 索引器 | 索引 cron 配置 + 手动 calc/purge | `ui/.../indexer/MavenIndexerResource.java` | 高 | 缺失：无 Maven 索引（可后补） |
| Zap（清缓存元数据） | `POST /api/zap/{path}`：仅删数据存储记录，保留二进制（回收前的负空间释放） | `artifact/ZapArtifactsResource.java` | 中 | 缺失 |
| Copy/Move | `POST /api/copy|move/{path}`（树级，dryRun）；另有 flat 模式（flat/copy|move，dry+failFast 参数） | `artifact/{CopyResource,MoveResource,FlatCopyMoveResource}` | 高 | 部分：push 复制是仓库级；无路径级 copy/move |
| 路径修复 | `POST /api/repairPaths/createOrphanItems/{path}`（补缺失父目录，dry）+ `repairPaths/{path}`（冲突路径修复，dry） | `repositories/RepairRepositoryResource.java` | 中 | 缺失 |
| 虚拟仓库维护 | `POST maintenance/cleanVirtualRepo`（清虚拟仓聚合缓存）；cleanUnusedCache（清远端未用缓存） | `ui/.../maintenance/MaintenanceResource.java` | 中 | 缺失：pull-through 缓存无清理策略端点 |
| 元数据服务端重索引 | `POST metadata_server/reindex` + `stats/recreate`（新元数据服务的重建操作） | `metadata/MetadataServerReindexResource.java` | 中 | N/A（架构差异） |
| 本地生成路径过滤 | `localgenerated/filter/paths`：federation 用的「本节点生成路径」过滤 | `localgenerated/LocalGeneratedResource.java` | 低 | N/A |
| 内部上传 | `POST /api/internal/upload/{repo}/{path}`：服务内部直推流（旁路常规上传校验链） | `upload/InternalUploadResource.java` | 中 | 缺失（可选） |
| 仓库配置持久化 job | 变更仓库配置定期持久化守卫 | `storage/jobs/RepositoryConfigPersistingJob.java` | 中 | 已有（自有配置面） |
| 仓库配置 v2 资源 | v2 仓库配置 CRUD（含 maven/gradle 等 packagetype 子路径） | `repositories/RepositoriesResourceV2.java`、`ui/.../repositories/RepoConfigResource` | 中 | 部分：自有 CRUD 面 |

## D. 配置与集群

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| 中心描述符+变更拦截器 | 单一 mutable descriptor；变更经拦截器链分发（security/layout/reverse proxy/trashcan 各有 ConfigurationChangesInterceptor） | `config/{CentralConfigDescriptorChange,ConfigurationChangesInterceptors*}`、`layout/*ConfigurationChangesInterceptor` | 高 | 部分：自有配置面，无统一变更分发 |
| 配置导出/导入 | GET/PUT 全量配置 XML（config-formats.md 已载）；ConfigResource 另暴露 platform baseUrl/federatedUrl/saas/mail 只读查询 | `system/ConfigResource.java` | 高 | 已有（自有格式） |
| HA 集群 | ha-node.properties（node.id/ip、membership.port、primary、cross.zone.order、ha.data.dir、ha.backup.dir）；集群锁/节点锁查询端点（locks/local、locks/type）+ 原生 DB 锁机制状态 | `common/ha/HaNodeProperties.java`、`system/locks/*`、`lock/{ClusterNativeDbLocksMechanismStatus,…}` | 高（概念）/中（细节） | 缺失：单节点设计 |
| 配置传播 | `POST /api/system/propagation/{topic}`：HA 内配置事件消息（HaMessage） | `system/PropagationResource.java` | 中 | 缺失（单节点 N/A） |
| Pre-stop 钩子 | `GET v1/system/preStop`：容器终止前优雅停机状态 | `system/PreStopHookResource.java` | 低 | 部分：有 graceful shutdown 自测 |
| 运行时系统属性 | `PUT /api/system/properties/update`、`DELETE .../delete`：运行时改/删 JVM 系统属性 | `system/SystemPropertyResource.java` | 中 | 缺失 |
| 代理服务器管理 | 命名代理 CRUD（remote 仓库引用） | `ui/.../proxies/ProxyResource.java`、`descriptor/repo/ProxyDescriptor` | 中 | 缺失：remote 无代理配置 |
| 邮件服务器 | SMTP 配置（descriptor）+ `POST /api/send_mail` 测试/发信 | `descriptor/mail/MailServerDescriptor.java`、`email/EmailResource.java` | 高 | 部分：有通知邮件；管理端点未确认 |
| 通用配置 | logo 上传、基础 URL、数据目录展示；平台级 general config + 系统公告（PlatformSystemMessageConfig） | `ui/.../generalconfiguration/GeneralConfigurationResource.java`、`platform/PlatformGeneralConfig.java` | 中 | 部分：控制台有设置页子集 |
| 内部 KV 配置存储 | configsService：命名配置 KV（url signing key、checksumReplication 等），`GET/PUT config/storage/checksumReplication` | `config/ConfigsServiceResource.java`、`signed/url` 调用点 | 中 | 缺失（可并入自有 config） |
| Sumo Logic 集成 | sumologic 配置 + OAuth 回调 + token 刷新（日志外发） | `ui/.../sumologic/SumoLogicResource.java` | 低 | 缺失（可不做） |
| Cron 工具端点 | UI 计算下次触发时间 | `utils/cron/CronTimeResource.java` | 中 | 缺失（小工具端点） |
| UI 校验/预定义值/自动完成 | UiValidationsResource、PreDefineValuesResource、AutoComplete（仓库名等） | `ui/rest/resource/utils/*` | 中 | 部分：控制台自带有部分校验 |

## E. 搜索与查询

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| AQL | `POST /api/search/aql?compact`；领域：item/statistics/property/build/module/dependency/promotion/releasebundle(+file)/sensitive；SQL builder+optimizer+result decorator；并发上限抛 AqlTooManyRequestsException（接 QRL） | `rest/resource/aql/AqlResource.java`、`storage/db/aql/**`、`aql/AqlTooManyRequestsException.java` | 高 | **缺失**：无 AQL（最大查询面差距） |
| 老搜索族（13 类） | artifact / checksum / gavc / property / pattern（异步） / usageSince / badChecksum / createdInRange / anyDateInRange / dependency / buildArtifacts / artifactLatestVersion / artifactVersions | `rest/resource/search/types/*` | 高 | 部分：仅 checksum（M1 面） |
| UI 搜索增强 | 搜索结果 stash（保存结果集供分页/继续）、syntax search、字段名助手 | `ui/.../search/{StashSearchResultsResource,SyntaxSearchResource}`、`search/fields/FieldNameHelper` | 中 | 缺失 |
| 搜索结果可见性 | visible aql items 辅助（按权限过滤搜索结果） | `search/VisibleAqlItemsSearchHelper.java` | 中 | 部分：搜索按权限过滤原则已有 |

## F. 元数据 / 统计 / UI 数据面

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| 属性集（property sets） | 预定义属性集 CRUD（闭集值/校验），仓库引用；`deletePropertySet` | `ui/.../propertysets/PropertySetsResource.java`、`descriptor/property` | 高 | 缺失：properties 自由键值，无预定义集 |
| 制品属性 CRUD | /api/storage/{repo}/{path}/properties（rest-api.md 已载）+ UI tabs 面 | `ui/.../tabs/{ArtifactsPropertiesResource,DeletePropertiesResource}` | 高 | 已有 |
| 制品关注（watch） | `artifactwatches` GET/POST remove/status：关注路径，部署/删除时邮件通知；watcher 信息持久化在制品元数据 | `tabs/watches/WatchersResource.java`、`fs/WatchersInfo`、`md/WatchersXmlProvider` | 高 | **缺失** |
| 目录/仓库归档下载 | `GET /api/archive/download/{repoKey}[/{path}]`：目录或整仓打 zip 下载（计流量）；`POST /api/archive/buildArtifacts`：按 build 拉归档 | `archive/ArchiveResource.java`（内部记 TrafficEntry） | 高 | **缺失** |
| 依赖声明生成 | 按 repo+包型生成 dependency 声明片段（gradle/maven/ivy/sbt 等），UI tab | `tabs/generalinfo/DependencyDeclarationResource.java` | 中 | 缺失（控制台展示增强） |
| 包视图 tabs | alpine/nuget/swift 专用视图 + ViewSource | `tabs/views/{ViewAlpineResource,ViewNugetResource,ViewSwiftResource,ViewsResource}` | 低 | N/A（包型缺） |
| 路径有效权限 | 浏览树 tab：当前用户对该路径的有效权限 | `tabs/EffectivePermissionResource.java` | 中 | 缺失：无 per-path 权限探测端点 |
| 制品统计 | 下载计数/下载者/最后下载；UI statistics 资源 | `artifact/ArtifactStatisticsResource.java`、`storage/db/statistics` | 中 | 部分：审计有记录，制品级统计面未确认 |
| 主制品探测 | 模块主制品识别（包视图用） | `leadartifact/{LeadArtifactFacade,detector}` | 低 | 缺失 |
| 树浏览器 V2 | 树浏览分页版（console-ui.md 已载 UI 面） | `tree/{TreeBrowserResource,TreeBrowserV2Resource}` | 中 | 部分：控制台已有树浏览 |
| Packages/Versions 新 API | 原生 package/version 浏览资源（新 UI 数据面） | `ui/.../artifacts/{packages/PackageNativeResource,versions/VersionNativeResource,repositories/RepoNativeResource}` | 中 | 部分：控制台自有列表页 |
| 通用信息 tab | GeneralArtifact/Checksums/FilteredResource/licenses 子资源 | `tabs/generalinfo/*`、`tabs/checksums/ChecksumsResource` | 中 | 部分 |

## G. 生命周期治理（cleanup / trash / GC / backup）

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| 清理策略（新框架） | DB 策略实体+运行摘要：package/build/bundle 三类 cleanup policy 存储、view run、NodeCleaned 记录、恢复运行摘要；UI 搜索条件 DTO | `storage/db/policy/**`、`cleanup/CleanupPackagePolicyUIResource.java` + dto 族 | 中 | **缺失**：无策略化清理（仅 GC） |
| 保留策略（retention） | 包保留策略（search criteria + master token + operation type）+ 归档策略 UI + RestoreRunSummary | `storage/db/policy/retention/**`、`descriptor/retention/*`、`retention/ArtifactRetentionUI*Resource` | 中 | **缺失** |
| 旧版 artifact cleanup | cron 配置 + 清理选择器（unused deps / one per version 等） | `descriptor/cleanup/CleanupConfigDescriptor.java`、`repo/ArtifactCleanupJobSelector` | 中 | 缺失 |
| Trash 生命周期 | 见 B 节 trash can 行（保留期/恢复/清空） | 同上 | 高 | 缺失 |
| GC/维护操作 | maintenance：garbageCollection / cleanUnusedCache / cleanVirtualRepo / compress | `ui/.../maintenance/MaintenanceResource.java` | 中 | 部分：仅 GC |
| 备份 | 备份集配置（目录/排除项）+ BackupJob 定时执行 + 大小预估 + 系统备份暂停回调；HA 备份目录独立属性 | `backup/{BackupServiceImpl,BackupJob,BackupSizeCalculator,SystemBackupPauseCallback}` | 高 | 已有（自有备份面） |
| 导入/导出 | 系统/仓库级 + 流式状态（import-export-api.md 已载） | `system/{ImportResource,ExportResource,ImportExportStreamStatusHolder}` | 高 | 已有 |
| 事件日志治理 | events/log 清理端点 + tmp event 分区截断 job + shift events | `event/{EventsLogCleanUpService,TmpEventLogTruncateJob}`、`system/EventsLogResource.java` | 中 | 缺失：事件日志无治理端点 |
| 上传会话清理 | （BinFlow upload_sessions 自有 sweep；参考物在 batch2/3 协议面） | — | — | 已有 |
| 任务/作业管理 | 后台任务模型 + `jobs/replications` 列表/单查（复制作业视角）+ TasksResource | `task/{TasksResource,BackgroundTask(s)}`、`jobs/JobsResource.java` | 中 | 部分：有 job 状态面（自测口径），无统一任务 API |

## H. 运维 / 可观测 / 平台服务

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| 限流（请求级） | 请求速率限制 config GET/POST/DELETE（含 v2 资源）；策略+指标 provider | `system/{RateLimiterResource,RequestRateLimiterResourceV2}`、`throttling/rrl/**` | 中 | 缺失：无限流 |
| 查询限流（QRL） | `v1/system/query_rate_limiter`：AQL 并发/速率限制，enabled/disabled/**simulation** 三态 + 指标 job | `system/QueryRateLimiterResource.java`、`throttling/qrl/**` | 中 | 缺失（无 AQL 故暂 N/A） |
| 搜索限流 | 搜索专用限流服务 | `throttling/search/SearchRateLimiterServiceImpl` | 中 | 缺失 |
| Load Healer | 负载自愈：反应式模式、活动集配置、节点排空（disable drain）；`POST/GET system/loadhealer` | `throttling/lh/**`、`system/LoadHealerResource.java` | 低 | 缺失（单节点 N/A） |
| 流量统计 | `traffic/filter/node`、`traffic/stream/filter`、`traffic/summary`：按节点入/出流量记账（DB stats+summary 两表族） | `traffic/TrafficResource.java`、`storage/db/traffic/**` | 中 | 缺失：Prometheus 有瞬时指标，无持久流量账本 |
| Support Bundle | `POST /api/system/support/bundle[+currentnode]`、GET 状态/下载、DELETE；v1 `/api/support/bundles`（可指定 node）；可选参数化内容 | `system/SupportBundleResource.java`、`support/SupportResource.java`、`descriptor/supportbundles` | 高 | **缺失** |
| Live Logs | `system/logs/config|data`：日志配置列举+日志内容流式拉取（UI 实时日志） | `system/LiveLogsResource.java`、`ui/.../systemlogs/*` | 中 | 缺失：日志要登录机器看 |
| Debug 日志级别 | `GET/POST/DELETE system/debug/loggers/{className}`：运行时调整指定类日志级别 | `debug/DebugResource.java` | 中 | 缺失 |
| DevOps Agent 观测 | 出站请求日志（RequestLogsResource：requestlog 列表/相关 trace）+ 错误目录（ErrorCatalog：错误码条目+事件入库可查） | `devopsagent/{requestlog,ErrorCatalogResource}`、`storage/db/devopsagent/**` | 低 | 缺失（现代等价=结构化日志+trace，可另案） |
| Workers 事件流 | `GET /v1/workers/events`：worker 服务事件（SSE 流） | `workers/WorkersResource.java` | 低 | N/A（无 worker 平台） |
| Probes | 深度健康探针服务（含 tomcat 级） | `probes/{ProbesService,tomcat}` | 低 | 部分：/ping 级健康检查 |
| Ping / 版本 | `GET /api/system/ping`→OK；`system/version`（+product/service 子资源：版本/revision/feature 列表）；老 `/api/versions` | `system/{PingResource,VersionResource}`、`versions/VersionsResources` | 高 | 已有 |
| System Info | 系统信息（JVM/DB/存储汇总快照）UI 资源 | `ui/.../systeminfo/SystemInfoResource.java`、`system/{SystemInfo,ServerInfo}` | 中 | 部分：有 metrics，无汇总页数据源 |
| 使用遥测 | `POST /api/system/usage`：匿名使用数据上报开关触点 | `system/UsageResource.java`、`o.a.usage` | 中 | 缺失（可不做/默认关） |
| 服务信息/注册 | `system/service[info]` + `system/service/registry` GET/POST：平台服务注册表（router 注册数据） | `system/ServiceInfoResource.java`、`access/ArtifactoryRouterRegistrationData` | 中 | N/A（单体） |
| 平台配置 | 平台 general config / 系统公告 / baseUrl 族读取 | `platform/*`、`system/ConfigResource` 相关端点 | 中 | 部分 |
| 守护触发端点 | 入站请求守护 + 手动触发资源（带 openapi 契约） | `admin/InboundRequestGuardTriggersResource.java`、根 `inbound-request-guard-api.openapi.yaml` | 低 | 缺失 |
| 文件系统浏览器 | `browsefilesystem`：admin 浏览服务器文件系统（导入/导出路径选择器） | `ui/.../filesystem/FileSystemBrowserResource.java` | 中 | 缺失（CLI 有等价力） |
| Onboarding/QuickStart | `onboarding/initStatus|reposStates|createDefaultRepos|createQuickRepos`；quickstart start/status/supported（向导式建默认仓） | `onboarding/{ArtifactoryOnboardingResource,ArtifactoryQuickStartResource}` | 中 | 缺失：首次启动无向导 |
| SetMeUp | 按 repo/packageType 生成连接说明（依赖片段+凭据） | `artifacts/setmeup/SetMeUpResource.java` | 中 | 缺失（文档手工覆盖） |
| UI 部署 | 控制台上传制品（explode 等选项） | `ui/.../deploy/DeployArtifactResource.java` | 中 | 已有（双模式控制台） |

## I. 集成面（跨产品）

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| Xray 集成 | `xray/index_repos|index|scanBuild|clearAllIndexTasks|license|{repo}/indexStats` + 每仓 xray 配置 UI | `xray/XrayResource.java`、`ui/.../xray/XrayRepoResource` | 中 | 缺失（无扫描产品，留 webhooks/事件出口即可） |
| Release Bundle（distribution 侧核心） | release-bundle 仓型 + v1/v2 资源 + **Air-Gap** 捆绑（离线分发）+ 目标保留 | `release/*Resource`、`bundle/`、`db/bundle/**` | 中 | 缺失（产品范围外，记录在案） |
| 复制 | push/pull/多源/全局（replication.md 已载） | `replication/*`、`system/GlobalReplicationResource` | 高 | 部分：仅单向 push |
| Federation | 多站点镜像仓：状态（mirrorsLag/unavailableMirrors/按仓状态）、replace url、二进制任务、grid topology 变更、心跳/全量同步 | `federation/*`、`repo/federated/**`、`storage/db/federated/**` | 中 | 缺失 |
| 联邦/网格 token 信任 | unidirectional federation token spec + cold token spec（配对信任） | `servicetrust/{federation,cold}/*TokenSpecService` | 低 | 缺失 |
| Mission Control / Pipelines | 旧 MC 集成 + pipelines 资源（头部信息） | `missioncontrol/*`、`ci/PipelinesResource.java` | 低 | N/A |
| 统一事件总线 | UnifiedEvent 服务 + consumer 注册端点 + jfbus/UemV2 外发 publisher（outbox 分区生命周期） | `unifiedevent/*`、`jfbus/**`、`unifiedevent/dto/ConsumerRegistrationRequest` | 低 | 缺失：无事件外发总线（webhooks 由 batch3 盘点） |
| MFE 微前端 | 内/外部 MFE 文件服务 + 版本刷新 job（新 UI 架构） | `mfe/**` | 低 | N/A |
| JCR EULA/订阅 | 容器注册版 EULA 门 + 订阅资源 | `jcr/{JcrEulaResource,JcrSubscriptionResource}`、`eula/EulaServiceImpl` | 中 | N/A |
| 许可/授权/订阅 | license 读取 + 多许可管理 + license usage 上报 + entitlement 门控（EntitlementService/Type）+ subscription | `license/LicenseResource.java`、`system/{ArtifactoryLicense(s)Resource}`、`entitlement/*`、`subscription/*`、`ui/.../license/*` | 高 | N/A（开源对标，无许可面） |

## J. 汇总

- **条目总数**：约 120（A17 / B14 / C20 / D14 / E4 / F12 / G10 / H21 / I10 + 交叉引用若干）。
- **置信度分布**：高 ≈ 34，中 ≈ 66，低 ≈ 20（低项集中在 JFrog 内部新功能：load healer、devops agent、workers、jfbus、MFE、HQC、intransit、shadow、guards）。
- **BinFlow 覆盖粗分**：已有/等价 ≈ 30；部分 ≈ 30；缺失 ≈ 60（其中产品范围外/N-A ≈ 12）。

## 待验证清单（低置信度，动态验证后回填）

1. Load Healer 的「活动集」具体语义与排空行为（throttling/lh 仅见结构）。
2. DevOps Agent request log 的采样与保留策略；ErrorCatalog 写入方是谁。
3. HQC（heavy query cache）与 fullsync 的关系；启用条件。
4. IntransitInterceptor 拦截的具体流量面（filestore 通道还是复制通道）。
5. Shadow 请求服务的启用开关与指标出口。
6. inbound-request-guard 的触发条件（openapi 在根目录，未读实现）。
7. jfbus/UemV2 outbox 分区参数与背压行为。
8. localgenerated 过滤器的调用方（推测 federation 全量同步）。
