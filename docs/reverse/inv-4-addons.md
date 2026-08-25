# inv-4-addons — Addon / 企业功能全量盘点（分区 4）

- 材料：`reverse-src/artifactory/src/{batch1-core,batch3-addons}`（只读，ADR-0001 clean-room）。
- 方法：包/类名级清点 + REST `@Path` 扫描 + 关键服务类行为抽读；有公开文档的（JFrog REST API 文档）标注双证。
- 本文件是**功能清单**，不重复既有规格细节：复制细节见 `replication.md`，S3 布局见 `s3-storage-layout.md`，RBAC/Projects 域见 `rbac-model.md`。
- 置信度：高 = 代码 + 公开文档双证；中 = 仅代码；低 = 推断待验证。
- 「依赖外部」列：该功能运行时依赖 Artifactory 之外的 JFrog 产品/服务（Xray、Distribution、Access、Mission Control、Workers 平台等），单机重实现无法获得等价行为，只能实现 Artifactory 侧的集成面。

---

## A. HA 高可用集群

| # | 功能 | Artifactory 行为要点 | 证据（类/包/配置） | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|---|
| A1 | 集群成员注册表 | 所有节点在 DB 表 `artifactory_servers` 注册：server_id、context_url、membership_port、server_state、server_role、last_heartbeat、版本/revision、running_mode、license_hash；成员发现走 DB 轮询而非内存网格（7.x 去 Hazelcast 化） | `org/artifactory/storage/db/servers/dao/ArtifactoryServersDao.java`（INSERT/SELECT/UPDATE 语句）；`ArtifactoryServersCommonServiceImpl` | 高 | 缺失（单体部署，无集群模式） |
| A2 | 节点心跳 | 定时更新 `last_heartbeat`；默认间隔 5s、30s 视为 stale、stale 成员 180min 后清理；成员列表查询有 10s 缓存 | `ConstantValues`：`ha.heartbeat.intervalSecs=5`、`staleSecs=30`、`stale.server.cleanup.periodMinutes=180`；`ArtifactoryHeartbeatServiceImpl` | 高 | 缺失 |
| A3 | 主节点角色推举 | 配置 `primary=true` 的节点启动时若已存在其它活跃主节点则降级 MEMBER 并报错；角色枚举 `TASK_AFFINITY / MEMBER`（定时任务只在 TASK_AFFINITY 节点跑，除非 propagation 到 MEMBER） | `addon/ha/manager/PrimaryBasedHaRoleManagerServiceImpl`；`ArtifactoryServerRole` | 高 | 缺失 |
| A4 | 节点状态机 | server_state：running/starting/stopping/converting；活跃成员 = 有心跳 且 state ∈ {running, starting, stopping, converting}；graceful shutdown 期间任务不再派发到该节点 | `ArtifactoryServersCommonServiceImpl.getActiveMembers/isConvertingMembers` | 中 | 缺失 |
| A5 | HA 事件传播 | 管理面变更（配置 reload、ACL、license、watches、repo config）通过 HTTP 逐节点传播到其它活跃成员；请求用 Access 颁发的 scope=`ha-propagate` 的 token 认证，非该 scope 拒收；跨版本节点跳过传播；支持 propagateUntilCriteriaMet（某节点返回指定 HTTP 状态即终止） | `addon/ha/propagate/HaPropagationServiceImpl`（HA_PROPAGATE_SCOPE、版本比对、terminating config）；`ReloadConfigListener/ReloadAclListener/ReloadLicensesListener` | 高 | 缺失 |
| A6 | 可传播 job 上下文 | 定时任务（备份、GC、push/remote replication、Maven indexer、cleanup/retention/archive、sha256 迁移、virtual cache 清理等）在 HA 下声明为 PropagatableJobContext，仅主节点触发后按策略传播/去重 | `addon/ha/context/*JobContext.java`（12 种） | 中 | 缺失 |
| A7 | 分布式工作队列 | 复制/Xray 索引/清理等用 DB 持久化 work queue（WorkItem 入队、冲突守卫 optimistic/DB-based、HA 下多节点消费） | `org/artifactory/workqueue/*`（WorkQueue、AsyncWorkQueueService、WorkQueueConflictsGuardProvider）；`addon/ha/DbConflictGuardProvider` | 中 | 缺失 |
| A8 | 数据库分布式锁 | 7.x 用原生 DB 锁（global/double-check 锁表）替代 JVM 锁；`NativeDbLocksActivationService` 仅 PostgreSQL 启用（Derby 单机不启用） | `org/artifactory/lock/{ClusterNativeDbLocksMechanismStatus,service/NativeDbLocksActivationService}`；`storage/db/locks/service/HaInitLockFactory` | 中 | 缺失 |
| A9 | HA 管理 REST | `POST /api/ha-admin/clusterDump`（集群状态转储，`ha` 角色可用） | `addon/ha/rest/HaAdminResource`（@Path("ha-admin")） | 中 | 缺失 |
| A10 | 混合许可检测 | 集群内 license_hash 随心跳上报；混合许可类型（如 HA+非 HA）集群被检出并限制 addon | `LicenseAddonsManagerImpl.isMixedLicenseTypeInCluster`；`artifactory_servers.license_hash` 列 | 中 | 缺失 |

## B. Xray 集成【依赖外部产品：Xray】

| # | 功能 | Artifactory 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|---|
| B1 | Xray 连接配置 | central config 配 Xray base URL + 凭据；启动时探测 `/api/v1/system/version` 校验最低版本；心跳判断 isXrayAlive；配置移除时清空索引事件队列 | `addon/xray/XrayServiceImpl`（HttpGet xrayBaseUrl+"/api/v1/system/version"、reload() 清队列）；`XrayClientConfigHelper` | 高 | 缺失（无 Xray 对接） |
| B2 | 索引事件队列 | 被索引仓库的制品变更写入 DB 持久队列 `hqc_xray_indexes`（Heavy Query Cache 机制），后台 job 消费批量推给 Xray；可 clearAllIndexTasks | `XrayService` 上的 `@HeavyQueryCache(hqcType="hqc_xray_indexes")`；`addon/xray/indexer/XrayIndexEventJob`；`XrayResource` DELETE clearAllIndexTasks | 中 | 缺失 |
| B3 | 索引仓库管理 REST | `/api/xray/index_repos`（GET 列表/PUT 更新）、`/api/xray/index`、`/api/xray/scanBuild`、`/api/xray/license`、`/api/xray/{repoKey}/indexStats`、`/api/xray/repos`、`/api/xray/nonIndexRepos`、`/api/xray/buildrepo/names` | `batch1-core org/artifactory/rest/resource/xray/XrayResource.java`（@Path("xray") 全套） | 高 | 缺失 |
| B4 | 索引一致性对账 | 仓库配置变更 diff 后向 Xray 报告 indexed/unIndexed 仓库集合（reportReposIndexDiff → updateIndexedRepositories） | `XrayServiceImpl.reportReposIndexDiff`；`RepositoriesDiffReportImpl` | 中 | 缺失 |
| B5 | 拦截未扫描/违规制品下载 | 远程下载经 `XrayRemoteDownloadInterceptor`；按 `xray.allowBlockedDownload` / allowWhenUnavailable 决定放行或阻断；Curation 仓库走 SecurityDecisionMapper | `addon/xray/interceptor/XrayRemoteDownloadInterceptor`；`rest/common/model/xray/XrayAllowBlockedDownloadModel`；`xray/mapper/SecurityDecisionMapper` | 中 | 缺失 |
| B6 | Xray 属性清理 | 周期性批量删除 nodes 上的 xray_* 属性（bulkDeleteXrayProperties 按 node_props 批删） | `XrayServiceImpl.bulkDeleteXrayProperties`；`XrayPropsCleanupJob` | 中 | 缺失 |
| B7 | Release Bundle Xray 联动 | bundle 创建/晋升后调度 Xray 扫描 job（BundleXrayScheduler、BundlePromotionXrayScheduler） | `addon/bh/scheduler/Bundle*XrayJob` | 中 | 缺失 |

## C. Distribution / Release Bundles【依赖外部产品：Distribution 服务（Edge/中心模型）】

| # | 功能 | Artifactory 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|---|
| C1 | Release Bundle v2 存储 | 7.x 用本地 `release-bundle@<site>` 仓库存 bundle 记录/内容；bundle 版本化（name+version）；目标侧收 bundle 有专用客户端 | `addon/release/bundle/service/ReleaseBundleAddonServiceImpl`；`BundleHandlerReceivedBundlesClientImpl`；`ReleaseBundleStorageInterceptor` | 中 | 缺失 |
| C2 | Bundle 事务 REST | `POST /api/release/bundle`（创建）；`POST /api/release/bundle/transaction/open`、`close/{tx}`、`async/close/{tx}?sync_wait_time_secs=`、`async/close/status/{tx}`；`GET /api/release/bundles`、`/bundles/{name}`、`/bundles/{name}/{version}`（源 checksum）；`PUT /api/release/store`（目标侧落盘） | `batch1-core org/artifactory/rest/resource/release/ReleaseBundleResource.java`（@Path("release")） | 高 | 缺失 |
| C3 | Air-Gap 离线分发 | `POST /api/release/import`（及 `/internal/import`）导入离线 bundle；`POST /api/release/export/{name}/{version}` 导出 | `release/AirGapReleaseBundleResource.java`、`AirGapReleaseBundleV2Resource`（AirGapExportInformationCollectionException） | 中 | 缺失 |
| C4 | 目标侧 bundle 保留 | 目标 Artifactory 对收到的 bundle 做保留策略（TargetBundleRetentionResource） | `rest/resource/release/TargetBundleRetentionResource.java` | 中 | 缺失 |
| C5 | legacy 直接分发 | 旧版 `/api/distribute`（按 FileSpec+checksums 流式分发制品到目标，带 fileTransactionId 续传语义、delegateToken）仍保留实现 | `addon/distribution/DistributionAddonImpl.distributeArtifact(Streaming)`、`strategy/DirectDeployDistributionStrategyImpl`、`ChecksumDeployUtil` | 中 | 缺失（反编译内未见对应 @Path 挂载，端点路径以公开文档为准——低） |
| C6 | Distribution 规则 | 按 path/layout token/property token 组成的 DistributionRule 解析坐标（供 MC/Distribution 用） | `addon/distribution/rule/{DistributionRuleTokenFactory,DefaultDistributionRules}` | 中 | 缺失 |
| C7 | bundle 联邦事务 | bundle 相关联邦操作用事务资源包裹（BundleFederationTransactionResource） | `addon/bh/transaction/*` | 低 | 缺失 |
| C8 | bundle V1→V2 适配 | 老 V1 bundle 在 V2 上的记录/内容/删除适配层 | `ReleaseBundleTargetV1ToV2*Adapter`（6 个） | 中 | 缺失 |

## D. Build Info（CI 集成）

| # | 功能 | Artifactory 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|---|
| D1 | build-info 数据模型 | BuildRun（name/number/started）、Module（artifact/dependency 列表带 sha1/md5）、promotion 历史、ReleaseStatus；`PUT /api/build` 全量/增量保存，`POST /api/build/append/{name}/{number}` 增量合并 | `batch1-core org/artifactory/build/*`（BuildRunImpl、Module、promotion、staging 子包）；`rest/resource/build/openapi.yaml`（/build PUT、/build/append） | 高 | 缺失（无 build-info 域） |
| D2 | 查询/删除 | `GET /api/build`（全部，支持 projectKey 过滤）、`GET /api/build/{name}`、`GET /api/build/{name}/{number}`、`POST /api/build/delete`（按 name+numbers/dateRange） | 同上 openapi.yaml；`search/build` 包 | 高 | 缺失 |
| D3 | Build 晋升 | `POST /api/build/promote/{buildName}/{buildNumber}`：目标仓库、status、ciUser、properties、dependencies、failsOnMissingArtifacts 等 | openapi.yaml /build/promote；`build/promotion` 包 | 高 | 缺失 |
| D4 | Docker promote | `POST /api/docker/{repoKey}/v2/promote`（镜像跨仓库晋升，targetRepo/tag/sourceTag/copy 覆盖） | openapi.yaml 同文件 | 高 | 缺失（BinFlow Docker 无 promote 端点） |
| D5 | Build 重命名/保留 | `POST /api/build/rename/{buildName}`；`POST /api/build/retention/{buildName}`（按数量/天数/buildNumbers/minimum retained days） | openapi.yaml；`addon/cleanup/{job,service}/builds`、`BuildCleanupJobContext` | 高 | 缺失 |
| D6 | Build 权限域 | build 信息可作为 project 资源挂到 projectKey；UI 有 xray 阻断展示 | openapi.yaml projectKey 参数；`ui/.../tabs/xray/IsArtifactBlockedService` | 中 | 缺失 |

## E. Projects / Teams【依赖外部组件：Access（内嵌服务）】

| # | 功能 | Artifactory 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|---|
| E1 | Projects CRUD 委派 | 项目/成员/角色/环境全部由 Access 经 gRPC（GrpcProjectClient：ProjectEntity、EnvironmentEntity、ProjectResourceEntity、RoleActions）管理；Artifactory 只做校验与同步 | `org/artifactory/projects/ProjectsServiceImpl`（accessClient/project gRPC） | 高 | 部分（BinFlow 有 Projects 域两层授权语义〔rbac-model.md〕，但无独立项目资源/环境/团队管理） |
| E2 | 仓库-项目同步 | 定时 job 将仓库与 Access 项目归属对账（SyncRepositoriesWithAccessJob）；projectKey 写入仓库 key 或属性 | `projects/job/SyncRepositoriesWithAccessJob`；`repo/interceptor/projects` | 中 | 部分（无同步 job 概念，直接本地归属） |
| E3 | 项目存储配额 | 项目级 soft/hard 配额（StorageQuotaEntity）；超配额发通知邮件 job（ProjectStorageQuotaNotificationJob + storageQuotaNotificationEmail.properties 模板） | `projects/ProjectStorageQuotaNotificationJob`；`org.artifactory.projects` 资源目录 | 中 | 部分（BinFlow 有实例/仓库级配额，无按项目配额与邮件通知） |
| E4 | projectKey 语法校验 | 项目 key 命名/长度校验（ProjectAdminValidation） | `projects/ProjectAdminValidation.java` | 中 | 部分 |

## F. 复制体系（push / pull / smart remote / 多目标）

| # | 功能 | Artifactory 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|---|
| F1 | Push 复制 V1 | 定时（cron）或手动把本地仓库内容推到远端 Artifactory；full/incremental 两模式；支持路径过滤、属性复制、删除同步；事件驱动增量（event 包） | `addon/replication/core/push/{PushReplicatorV1,full,incremental,event}`；`PushReplicationJobContext` | 高 | 部分（BinFlow 有 push 单向复制，无多目标/事件驱动/删除同步细节——见 replication.md 对照） |
| F2 | Pull（remote）复制 | 远程仓库定时回源预取（RemoteReplicator），可同步属性与统计 | `addon/replication/core/remote/{RemoteReplicator,RemoteReplicationProducer}`；`RemoteReplicationJobContext` | 高 | 部分（BinFlow pull-through 缓存即近似形态；无定时全量镜像 job） |
| F3 | 复制 V2（流式） | 新复制引擎：traversal 产 filelist → streamer 逐文件流式传输，控制器管理断点；`/api/replication/replicate/file/streaming/{tx}` | `addon/replication/v2/replication/{controller,filelist,streamer,traversal}`；`ReplicationResource` replicate/file/streaming | 中 | 缺失 |
| F4 | 复制配置 REST | `GET/POST/DELETE /api/replications/{repoKey}`、`/api/replications/multiple{repoKey}`（多目标）、`POST /api/replications/enable|disable`（全局开关）、`GET /api/replication/{path}`（状态）、`POST /api/replication/execute/{path}`（手动触发，带 strategy） | `rest/resource/replication/{ReplicationResource,ReplicationsResource}` | 高 | 部分（BinFlow 有 push 复制配置；无 multiple/全局 enable-disable/execute 端点） |
| F5 | Smart Remote Repo | 远程仓库指向另一 Artifactory 的高级形态：`enableTokenAuthentication`（复制品拉取走源端代理 token）、`contentSynchronisation`（statistics/properties 同步联动）、missRetrievalCachePeriod、unusedArtifactsCleanupPeriodHours、remoteRepoChecksumPolicyType（generate-if-absent 默认）、hardFail、metadataRetrievalTimeoutSecs=60、socketTimeoutMillis=15000 | `artifactory.xsd`（854/1032/1058/1087/1199/1251 行字段及默认值）；`TypeSpecificConfigModel.setEnableTokenAuthentication` | 高 | 部分（BinFlow remote 有基本回源缓存；无 token 认证代理/内容同步统计联动/未用制品清理策略字段） |

## G. Federated 仓库

| # | 功能 | Artifactory 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|---|
| G1 | 联邦成员与传播 | federated rclass：多站点同 key 仓库互为 peer；上传/删除/属性变更产生 FederatedMirror* 事件按 peer 传播；成员协商（FederatedNegotiateEvent）；全量同步控制器（FullSync）；请求隧道代理（RequestTunneling*，新旧两套） | `addon/federated/`（134 文件：FederatedRepoImpl、FederatedMirrorFullSyncController、MirrorResource）；`rest/resource/federation/*` | 中 | 缺失 |
| G2 | 联邦 REST/内部端点 | `/api/federation/...`（binary tasks、status、replace url）；GridTopologyChange 事件 | `rest/resource/federation/{FederatedBinaryTasksResource,FederatedStatusResource(V2),FederatedReplaceUrlResource}` | 中 | 缺失 |
| G3 | Lead artifact 推举 | 联邦内同一 sha256 多副本时按策略推举 lead（LeadingArtifactExtractor + LeadArtifactDetectorAddonImpl） | `addon/federated/LeadingArtifactExtractor`；`addon/leadartifactdetector` | 低 | 缺失 |

## H. 用户插件（Groovy）

| # | 功能 | Artifactory 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|---|
| H1 | Groovy 插件加载 | 扫描插件目录（`$ARTIFACTORY_HOME/etc/plugins/*.groovy`，scripts 目录约定）动态编译装载；插件可声明 cron/job；校验器拒绝不合规插件 | `addon/plugin/{GroovyRunnerImpl,GroovyShellWrapper,PluginValidator,PluginsAddonImpl}` | 高 | 缺失（无用户脚本扩展机制） |
| H2 | 插件 REST | `GET /api/plugins`（列表）、`GET /api/plugins/status`、`GET /api/plugins/{pluginType}`、`POST|PUT|GET|DELETE /api/plugins/execute/{executionName}?params=k=v;k=v&async=int` | `rest/resource/plugin/PluginsResource.java`（@Path("plugins")） | 高 | 缺失 |
| H3 | 插件能力面 | execution（REST 触发）、download/upload 请求改写、user realm（自定义认证 PluggableAuthenticationProvider）、storage 事件钩子、build 操作、searches/repositories/security 服务代理、AQL 结果暴露 | `addon/plugin/{execution,download,upload,realms,storage,services,aql,security}` 子包 | 高 | 缺失 |
| H4 | staging 策略 | Maven promotion 策略可由插件提供（`/api/plugins/build/staging/{strategyName}`） | `PluginsResource` build/staging 路径 | 中 | 缺失 |

## I. Webhooks / 统一事件 / Workers【部分依赖外部：Access（webhook 配置）、Workers 平台】

| # | 功能 | Artifactory 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|---|
| I1 | 事件类型闭集 | 36 种统一事件：before/after × {Create,Delete,Download(含 Request/Error/Remote),Move,Copy,Property*,Upload,BuildInfoSave,Repo*,Statistics/File/Directory/DeleteReplication} + alt*（altAllResponses/altRemoteContent/altRemotePath）；CloudEvents 风格 envelope（specVersion/id/type/source/time/subject/dataContentType/headers） | `org/artifactory/unifiedevent/constants/SupportedUnifiedEvents`（36 枚举）；`model/EventEnvelope` | 高 | 部分（BinFlow 有审计日志；无出站事件信封/订阅） |
| I2 | Webhook 管理 | 订阅/过滤/secret 管理在 **Access** 侧（Artifactory 无 webhook CRUD 资源，仅 `internal:webhook` 权限常量与 UI 引用）；Artifactory 产生事件由 Access 分发到用户 webhook | `org/jfrog/access/common/PermissionConstants.INTERNAL_PERMISSION_WEBHOOK`；rest 树无 webhook resource；rtui JS 引用 | 中 | 缺失（无 webhook 出站） |
| I3 | 统一事件总线出站 | 事件可注册 consumer（Worker/WorkerClient/Xray）与 outbound（HTTP POST JSON / client 回调）；repo CUD 事件走 jfbus 异步分发 | `unifiedevent/dispatchers/outbound/{HttpOutbound,ClientOutbound}`；`SupportedConsumers/SupportedOutbound`；`RepoCudJfbusEventDispatcher` | 中 | 缺失 |
| I4 | 事件注册 REST | `/api/v1/unifiedevent/registration`（POST/GET/DELETE，按 event+consumer） | `rest/resource/unifiedevent/RegistrationResource.java` | 中 | 缺失 |
| I5 | Workers（serverless 扩展） | TS worker 以 events-schema.json 声明（name/service/meta/sample/payloadDocumentation/supportProjects/isAsync/filterType/mandatoryFilter/executionRequestType）；worker-events 目录制出 40+ 事件 TS 类型；REST `/api/workers`（WorkersResource）暴露 worker 状态 | `batch3-addons/worker-events/{schema/events-schema.json,events/*,shared/repoCudTypes.ts}`；`rest/resource/workers/WorkersResource.java` | 中 | 缺失 |

## J. 许可与订阅（产品许可 + 制品 license）

| # | 功能 | Artifactory 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|---|
| J1 | 产品许可 REST | `POST /api/system/licenses`（安装）、`GET`（列表）、`POST .../activate`、`POST .../licenseChanged`（HA 传播回执）、`DELETE`；角色 `admin,ha`；许可变更传播到集群并触发 addon 重载 | `rest/resource/system/ArtifactoryLicensesResource.java`（@Path activate/licenseChanged、@RolesAllowed({"admin","ha"})）；`LicenseAddonsManagerImpl.propagateLicenseChanges` | 高 | 缺失（BinFlow 无许可体系，开源等价物不需要） |
| J2 | 订阅类型模型 | SubscriptionType：none/free/free_xray/pro/pro_team/pro_xray/pro_team_auth_providers/enterprise_team/enterprise_xray_team/enterprise_plus；addon 用注解（EdgeAddon/EnterprisePlusAddon/EdgeBlockedAddon/SubscriptionTypeBlockedAddon）按订阅门控启用 | `org/jfrog/common/platform/subscription/SubscriptionType`；`addon/LicenseAddonsManagerImpl`（注解分派逻辑） | 高 | 缺失（不适用） |
| J3 | 制品 license 识别 | 内置 91 条 OSS license 模式（name/longName/regexp/url，licences.xml）；按包类型策略定位（Maven pom/Npm/Nuget/Ivy）；`/api/licenses` 按仓库+状态（approved/unapproved/unknown/notFound/neutral）过滤；支持导入导出与人工增改 | `licences.xml`（根目录，91 license）；`addon/license/{service,strategy/*LicenseLocatorStrategy}`；`LicensesAddonImpl.findLicensesInRepos` | 中 | 缺失 |
| J4 | callhome / jfconnect | 匿名使用统计上报（CallHomeRequest）与 JFrog Connect 服务（JFConnectServiceImpl） | `api/callhome/CallHomeRequest`；`addon/jfconnect/JFconnectServiceImpl` | 低 | 缺失（不适用） |

## K. 数据库支持矩阵

| # | 功能 | Artifactory 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|---|
| K1 | 六方言 | DbType 枚举：DERBY/MYSQL/ORACLE/MSSQL/POSTGRESQL/MARIADB；每库有 DDL 模板（templates/{derby,mysql,oracle,mssql,postgresql}/*.tpl：add_column/alter_column_size/rename_* 等）与 schema change log（scripts/*_schema_change_log.sql） | `org/jfrog/storage/DbType.java`；`templates/`、`scripts/` 目录 | 高 | 部分（BinFlow 主打 PostgreSQL；多 DB 方言层未做） |
| K2 | Derby 内嵌默认 | 无外部 DB 时用 Derby（默认分发形态）；AQL 等功能在 Derby 下降级 | `storage/db/DerbyUtils`；多处 `DbType.DERBY` 特判 | 高 | 缺失（单一 PG，无内嵌库形态） |
| K3 | Outbox 模式 | 事务 outbox 表按 6 方言建表（db/outbox-schema-*.sql），用于事件/消息可靠投递 | `batch3-addons/db/outbox-schema-{derby,mariadb,mssql,mysql,oracle,postgresql}.sql` | 高 | 缺失（BinFlow 审计/事件无 outbox） |
| K4 | 连接池与 DB 增强 | Tomcat JDBC 池为主（pool sweeper 强制）、可选 Hikari；AWS RDS IAM 认证数据源；DB 层指标包装（MetricsDataSource 全链包装）；查询监控（DbQueryMonitor，PG 专属语法） | `org/jfrog/storage/{TomcatJDBCDataSource,HikariJDBCDataSource,RDSIAMDataSource,metric/*}`；`storage/db/monitor/DbQueryMonitorImpl` | 中 | 部分（BinFlow 有 PG + 池；无 RDS IAM/查询监控） |
| K5 | 模式迁移 | Liquibase 风格 versioned converter 链（v1..vN，可 dbSpecific 按库跳过）；bootstrap 时 DbInitializationManager 驱动 | `storage/db/version/ArtifactoryDBVersion`（如 v26_node_props_index PG-only）；`storage/db/init/DbInitializationManager` | 高 | 部分（BinFlow 用自研迁移；无多库条件迁移） |

## L. 二进制存储后端（云/块存储）

| # | 功能 | Artifactory 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|---|
| L1 | binary provider 链 | `binarystore.xml` 声明 provider chain（默认模板 `file-system`）；chain 可组合 cache/checksum/fs/s3 等层（具体 provider 实现在外部 storage-client 库，本批未见源码） | `batch1-core/META-INF/default/binarystore.xml`（`<chain template="file-system"/>`）；`org/jfrog/storage/binstore.*` import 引用 | 中 | 部分（BinFlow 已有 filestore+S3；配置面为静态链式模板） |
| L2 | 云后端直连重定向 | 当所配云 provider 支持 redirect（如 S3 预签名）时下载可 302 直连对象存储；`isCloudProviderSupportingRedirectionConfigured()` 门控；云信息缓存 | `sh/service/{StorageBinaryServiceImpl,ArtifactoryBinaryServiceImpl}`；`getCloudProviderInfo`（StorageClient） | 中 | 部分（BinFlow S3 直传/重定向见 s3-storage-layout.md；无 provider 信息探测缓存） |
| L3 | 分片上传（MPU）REST | `/api/v1/uploads/create|config|urlPart|complete|status|abort`：大文件对 S3 的分片上传暴露为 Artifactory REST（可断点续传、按 node 查状态） | `rest/resource/mpu/MultipartUploadResource.java`（@Path("v1/uploads")，6 端点） | 高 | 部分（BinFlow S3 后端内部 MPU；未作为独立 REST 面） |
| L4 | 冷存储（Archive） | 制品冷归档到廉价层：archive 策略（cron 触发/dryRun）、run 管理（forceStopAll/summaryReports/downloadReport）、`/api/archive/v2/consumption`（用量）、restore（warm/cold 两路径，PackageRestoreColdRunResource）；恢复状态 job | `addon/archive/`（50 文件：ArchivePackagePolicyResource、ArchivePolicyRunResource、restore/cold）；`BinaryRestorationStatusJob` | 中 | 缺失 |
| L5 | GC/存储任务 HA 化 | GC、备份、sha256 迁移等以 HaJobContext 声明，集群单点执行 | `addon/ha/context/BinaryStoreGarbageCollectorJobContext、Sha256MigrationJobContext` | 中 | 部分（BinFlow GC 单机，无集群单点语义——单机下等价） |

## M. 安全企业面

| # | 功能 | Artifactory 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|---|
| M1 | Crowd SSO | Atlassian Crowd 目录认证（CrowdHttpAuthenticator、ArtifactoryCrowdClient） | `addon/sso/{ArtifactoryCrowdClientImpl,CrowdHttpAuthenticator}` | 中 | 缺失（BinFlow 有 OIDC/LDAP，无 Crowd） |
| M2 | OAuth SSO | OAuth 提供商登录（provider handler、state 机、sso login REST）；7.x 实际主要经 Access 的 SSO 设置 | `addon/sso/oauth/{OAuthHandlerImpl,OauthProviderHandlerImpl,ProviderStates}` | 中 | 部分（BinFlow OIDC 已有；无通用 OAuth provider 框架） |
| M3 | 签名密钥对 | `GET/POST /api/security/keypair`、`GET /api/security/keypair/{keyPair}`、`/public`、`/public/repositories/{repoKey}`、DELETE、`POST .../verify`；密钥对用于 Debian/Alpine RPM/Yarn 等索引签名与 trusted-keys 验签 | `addon/keys/rest/{KeyPairsResource,TrustedKeysResource}`（@Path("security/keypair")、@Path("security/keys/trusted")） | 高 | 缺失（无索引签名体系） |
| M4 | Trusted keys | 远端索引签名验证用可信公钥管理（`/api/security/keys/trusted`，按 kid 删/列） | `keys/rest/TrustedKeysResource` | 中 | 缺失 |
| M5 | Webstart/JavaWS | JNlp 动态改写 + jar 签名拦截（JarSigner、KeyStore） | `addon/webstart/*`（含 keystore） | 中 | 缺失（已淘汰技术，可不实现） |
| M6 | 服务信任/配对 | Service Trust：跨服务配对 token（pairing token、usecase 列表，`/api/v1/service_trust/pairings`）；用于 MC/Distribution 与 RT 互信 | `rest/resource/servicetrust/ServiceTrustResource.java` | 中 | 缺失 |
| M7 | 多租户基础设施 | multitenantinfra 库：k8s 感知、分布式锁、限流、sysconf——供 JMC/云部署形态 | `com/jfrog/commons/multitenantinfra/{k8s,locks,ratelimit,registry}` | 低 | 缺失（不适用） |

## N. 运维/可观测企业面

| # | 功能 | Artifactory 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|---|
| N1 | Support Bundle | `POST /api/system/support/bundle`（创建）、`/{bundleId}`（生成/查询）、`/currentnode`；收集器体系（系统/配置/日志/前端/JFrog melt/微服务/grid/apptrust）；`GET/POST/DELETE /api/support/bundles[/{archive}]`（列出/下载/删除，HA 指定 node） | `rest/resource/system/SupportBundleResource`；`rest/resource/support/SupportResource`；`addon/support/collectors/*`（7 收集器） | 高 | 缺失（BinFlow 有备份，无诊断包） |
| N2 | Observability 日志服务 | 集中日志/用量上报（ObservabilityServiceImpl、usage 统计 NewUsageFile/UsageFilesStats、LogType 枚举） | `addon/observability/*` | 中 | 部分（BinFlow 有 Prometheus 指标 + 结构化日志；无用量文件上报） |
| N3 | 请求流量记录/回放 | traffic 包：请求录制与过滤（debug 用） | `rest/resource/traffic/` 目录 | 低 | 缺失 |
| N4 | 反向代理配置 | reverseproxies REST：生成 nginx 配置等 | `rest/resource/reverseproxies/` | 中 | 缺失 |
| N5 | 邮件通知 | 邮件服务器配置 + 发送 REST（EmailResource）、配额/watch 通知模板 | `rest/resource/email/EmailResource`；`addon/email`；storageQuotaNotificationEmail.properties | 中 | 缺失（BinFlow 无邮件子系统） |
| N6 | Onboarding 向导 | 首次登录仓库创建向导（校验器+缓存） | `addon/onboarding/{validator,cache}` | 低 | 缺失 |

## O. 治理类 addon（清理/保留/观察/证据/策展）

| # | 功能 | Artifactory 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|---|
| O1 | Cleanup Policies | 声明式清理策略（策略 CRUD + cron 运行 + dryRun + run 视图/强制停止/报告下载），v2 面 `/api/cleanup/...`；旧的 Artifact Cleanup job 仍在（Addon 早期形态） | `addon/cleanup/`（112 文件：cleanup/{dto,job,processor,rest,schedule,service,strategy,task}）；`rest/resource/cleanup/CleanupPolicyViewResource`（@Path("cleanup/policies/view")） | 中 | 部分（BinFlow 有 GC/快照清理；无策略化 UI/REST 面——具体差距见功能对照） |
| O2 | Retention Policies | 制品保留策略（按策略删旧版本，AQL 驱动 core+common 双层 processor/task/job/interceptor/rest；UI 面 `/api/retention` + ArtifactRetentionUIResource） | `addon/retention/`（235 文件）；`rest/resource/retention/ArtifactRetentionUI*Resource` | 中 | 部分（同上，BinFlow 清理偏 GC 而非策略 DSL） |
| O3 | Watch（制品观察） | 用户对路径/repo 订阅，制品变更邮件通知；watch 权限校验（WatcherPermissionValidator）+ 存储/安全拦截器；HA 下 watch 变更传播 | `addon/watch/`（8 文件 + api/） | 中 | 缺失 |
| O4 | Evidence（制品证据） | 为制品/构建附加证据（合规签名链），evidence 服务 + 拦截器 + 事件 + 客户端 | `addon/evidence/`（38 文件） | 中 | 缺失 |
| O5 | Curation【依赖外部：Curation 服务】 | OSS 包策展（远程包放行/阻断决策走 Xray-Curation；CurationNotEntitled 无订阅时 403 映射；CurationRepoIndexMapper） | `addon/curation/`（12 文件 + exception mapper） | 中 | 缺失 |

## P. 其他 addon（清点存档）

| # | 功能 | 要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|---|
| P1 | VCS 元数据 | GitHub/GitLab refs/tags/branches/commit 元数据 API（`/api/vcs/{repoKey}/{userOrg}/{repo}/refs|tags|branches|tag/{tag}|commit/{sha}`），供 release bundle 按 VCS 坐标收集 | `addon/vcs/rest/VcsResource` | 中 | 缺失 |
| P2 | Git / Git-LFS | 内嵌 Git servlet（git-lfs 协议端点），Cargo 复用 git 后端（CargoConfig）；receive-pack 弃用过滤 | `addon/git/`（servlet/filter/util） | 中 | 缺失 |
| P3 | Filtered Resources | 文本模板占位符替换（TokenFilter 风格：@@token@@）+ 安全限制 | `addon/filtered/adapters/*` | 中 | 缺失 |
| P4 | Package re-route | 包管理器请求按配置改路由（registry 级 reroute + 认证过滤器 + 指标） | `addon/packagereroute/` | 低 | 缺失 |
| P5 | Nim model / Pub / Opkg 等包类型 | nimmodel（内部模型服务用）、pub（Dart）、opkg 及其余 20+ 包类型 addon 归分区 2/3 盘点 | `addon/{nimmodel,pub,opkg,...}` | 中 | 缺失（BinFlow 五包型；扩展包类型属协议分区） |
| P6 | analytics / metricslogger | 匿名指标采集与 addon 级指标日志 | `addon/{analytics,metricslogger}` | 低 | 不适用 |
| P7 | devopsagent / mfe / localgenerated | DevOps agent 错误目录、微前端版本注册（`/api/v1/mfe/{name}/version`）、本地生成资源端点 | `rest/resource/{devopsagent,mfe,localgenerated}` | 低 | 不适用 |

---

## 与公开规范/文档的关系

- Release Bundle / Distribution、Xray、Projects、Webhooks 均以 JFrog 官方 REST API 文档为行为基准（端点/参数双证）；反编译补充的空白：HA 传播 token scope、Xray 索引队列表名、订阅注解门控清单、outbox 六方言 DDL、MPU REST 端点集。
- 「此条补充官方规范」：A5（ha-propagate scope）、B2（hqc_xray_indexes）、J3（licences.xml 91 条内置模式）、K3（outbox schema）、L3（MPU REST）。

## 待验证清单（低置信度）

1. C5：legacy `/api/distribute` 端点挂载点未在反编译中定位到 @Path（实现类在，路由未见）。
2. G3：federated lead artifact 推举策略细节（按何序 tie-break）未读。
3. I2：webhook 管理 REST 是否完全在 Access（还是存在 Artifactory 侧代理资源）——仅有权限常量与 UI 引用佐证。
4. J4：callhome/jfconnect 上报内容与频率。
5. L1：binary provider 具体 chain 组合（S3/GCS/Azure 模板）在外部 storage-client 库，本批源码未见，需以 `binarystore.xml` 公开文档为准。
6. N3：traffic 录制回放的具体端点面。
7. P4：packagereroute 的触发语义与配置模型。
8. M7：multitenantinfra 是否在自托管发行版激活。

## 汇总

- 企业功能条目：**91**（A 集群 10 / B Xray 7 / C 分发 8 / D 构建信息 6 / E 项目 4 / F 复制 5 / G 联邦 3 / H 插件 4 / I 事件 5 / J 许可 4 / K 数据库 5 / L 存储 5 / M 安全 7 / N 运维 6 / O 治理 5 / P 其他 7）。
- 依赖外部产品的项（单机重实现只能做集成面，无法复刻对端行为）：**Xray（B1~B7）、Distribution 服务（C1~C8）、Access（E1~E4、I2、J1 部分）、Curation（O5）、Workers 平台（I5）、JFrog Connect/Mission Control（J4、M6、M7）**。
- BinFlow 覆盖粗判：已有≈0 项完全对齐；「部分」≈15（F1/F2/F4/F5、E1~E4、K1/K4/K5、L1~L3/L5、M2、N2、O1/O2）；其余缺失。最接近可直接平移的是 F5 smart remote 字段面与 L3 MPU REST（BinFlow 内部机制已在，缺 REST/配置暴露）。
