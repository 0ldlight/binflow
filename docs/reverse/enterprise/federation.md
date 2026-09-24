# Artifactory Federated 仓库行为规格（federation）

> 源版本：7.161.24 反编译（部分 7.161.20）——实测注与 ha-cluster.md 相同：反编译目录内 Artifactory 自有模块 pom 戳为 `7.161.14`（含 artifactory-addon-federated / addon-mirror / addon-grid / federation 相关 dto）。以「本反编译源」指代。
> 置信度：`高` = 反编译 + 官方文档双证；`中` = 仅反编译可见；`低` = 推断/部分可见。
> 证据锚：`backend/<模块>/<路径>`。与 `docs/reverse/replication.md` §7（联邦概念短注）互补，本文为深规格；不重复复制（replication）端点面。

---

## 0. 总模型

当仓库配置声明 `federation.members`（成员 URL 列表）且本节点是其中一员，**则** 该仓库成为 federated 仓库：制品与属性以**事件驱动、逐成员推送通知 + 拉取**的方式在成员间镜像（默认双向）；成员配置本身在 create/update/delete 时**立即推送到全体成员**；成员状态靠周期心跳与事件队列游标（共享库 `node_events` + `node_event_cursor`）维护。置信度：高（官方 federated 仓库文档描述「members 间自动双向同步、单仓库视图」与源码双证）。

## 1. 联邦成员模型

### 1.1 成员登记（描述符面）

- 成员字段（repo config 的 federation 节）：`url`（或 `urlOrTemplate` 模板）、`enabled`（默认 true）、`mode`（镜像模式）。空 mode 视为 `bidirectional`（`backend/artifactory-addon-federated/org/artifactory/addon/federated/core/FederatedRepoServiceImpl.java` getMirrorMemberInfos 与 `utils/FederatedMemberUtils`）。置信度：高。
- **本地成员判别**：成员 URL 去尾斜杠后按 base URL（central config `federatedRepoUrlBase`，缺省取服务器 URL）匹配本实例者即 local member；其余为 remote。**当** 配置里找不到 local member，**则** 配置同步直接放弃（warn `No local member found`）。置信度：中。
- 镜像模式枚举（`rtfs dto MirrorMode`）：`transmitter`（只发）/ `bidirectional`（默认，收发）/ `receiver`（只收）。成对视角取 `invert()`：transmitter↔receiver，bidirectional 自反。置信度：高。
- **当** 本地成员为 `receiver` 模式且 RTFS 服务未启用，**则** 保存配置被拒（`This mode is only supported in RTFS service`）；RTFS 启用下 receiver 本地成员的**任何配置变更**被拒 409（`Config change rejected ... local member is in receiver mode`）（`validator/FederatedReceiverModeValidator.java`）。置信度：中。

### 1.2 Grid 拓扑模型（多站点登记，新形态）

- 另有 Grid 服务（`backend/artifactory-addon-grid`、`platformfederation-client-*`、`topology-service`）：**当** 实例加入 Grid（多 JPD 联邦）且创建/变更 federated 仓库，**则** 先本地校验 repo key 唯一（活跃库或 transparent 库命中 → `Repo key ... is already taken`），再向 Grid 拓扑服务发 `POST /grid/api/v1/topology/{topologyKey}/repos/approve-creation`（scope `internal:grid/topology:x`，token 缓存 30min、access 有效期 2000s、连接/读超时 15s、单请求读超时 1min），响应给出**站点计划**（site plan：origin / active / transparentRemote）；active 远端写入成员列表，transparent 远端进入透明仓库存储。Grid 拒绝（400/403）→ 创建失败（`GridApprovalRejectedException`）。置信度：中。
- 非成员实例上 Grid 路径为 no-op（`isPartOfGrid()` 闸门）。**当** 不在 Grid 中，**则** 走传统路径：成员列表完全由本端 repo config 声明并推送。置信度：中。
- Grid 拓扑持久化于 DB（GridTopologyDao）并以 `gridTopologyId` 缓存成员列表；**当** 收到外部拓扑变更消息（`handleExternalGridTopologyChange`），**则** 失效缓存并按新拓扑重建成员视图。置信度：低（消息来源链路部分在未反编译的 topology-service）。
- **远端最低版本**：**当** 远端实例版本 < `7.139.0`，**则** 配置同步按「已验证实例缓存」机制拒绝/告警（`FederatedRepoConfigChangeInterceptor.MIN_REMOTE_FEDERATION_VERSION`）。置信度：中。

### 1.3 成员加入/创建校验链（可观察错误）

保存 federated 仓库配置时的校验器（`backend/artifactory-addon-federated/org/artifactory/addon/federated/validator/`），失败均 400（BadRequestException/ValidationException），冲突 409：

| 校验 | 行为（当…则…） | 置信度 |
|---|---|---|
| Ping | 当保存含远端成员的配置，则向远端发连通性探测；非 2xx/IO 失败 → 400 `Connectivity between the federated member and the source failed: <code> <body>` | 高 |
| 时间同步 | 当请求时间戳与本地 UTC 差 > 10s，则 400 `Time is not synced between federated members.` | 高 |
| license | 当本端未装联邦 license，则拒绝（validateFederationLicenseInstalled） | 中 |
| 远端仓库存在 | 当远端不存在对应 repo key，则拒绝（RemoteServerValidator / FederatedRepoConfigExistsValidator） | 中 |
| 仓库类型一致 | 当本地与远端 repo 包类型不同，则 400 `Repo <X> can not be added to <Y> as it does not have the same repo type` | 高 |
| build-info 专属 | 非默认 build-info 仓不能连默认 `artifactory-build-info`；反向亦拒；类型不符拒 | 中 |
| release-bundles 专属 | 项目存在性、包类型一致、**源/目标 repo key 必须相同** | 中 |
| 同主机禁令 | 当成员 URL 解析到与本机同一 host，则 400 `Cannot add member on the same host as the local server` | 高 |
| 重复成员 | 当成员列表含重复项，则 400 | 中 |
| 重复平台 URL | 当两个成员解析到同一平台 base URL，则 400 | 中 |
| Edge 收方禁令 | 当 Edge license 实例收到 `update/message`（配置同步），则 400 `Feature is not supported on edge server.` | 中 |

（UI 侧校验服务：`ValidateFederatedRepoMirrorsService`——无 mirrors → 400 `No mirrors were provided`；远端拒绝 → 400；权限不足 → 403。）

### 1.4 认证与令牌

- **当** 本端需向某远端发联邦请求，**则** 用 `AuthenticationTokenProvider.getToken(remoteUrl, scope)` 取**按远端缓存**的 bearer token；scopes 形如 `internal:federated-repo/mirror-update:x`、`mirror-event:x`、`mirror-sync:x`、`mirror-statuses:x`、`internal:grid/topology:x`。置信度：高（发送端与资源端 `@AllowedPermissionScope` 双源）。
- **master token 维护**：每小时 cron（`federated.master.tokens.refresh.job.cron`）刷新；**当** token 将在 7 天（grace）内过期，**则** 提前刷新；**当** token 已过期，**则** 无法用于自刷新，警告 `Authentication token has already expired ... Please start a new token binding flow`；刷新余量 15 分钟（`federated.auth.token.expiry.margin.sec`）。置信度：中。
- 跨实例认证建立失败的可观察文案：`Failed adding federated repository. Make sure that cross-instance authentication is established between this instance and the remote JFrog Platform Deployments.`（常量 COT_ERROR_REASON）。置信度：中。

## 2. 同步机制（事件驱动 pull-through）

### 2.1 事件产生与队列

- **当** 本地发生 created/deleted/props 类节点事件，**则** 事件先落共享库 `node_events` 表（含 `hints` 位集），联邦消费者按 `(repoKey, FEDERATED_REPLICATION, remoteUrlWithRepoKey)` 维度的持久队列消费；每队列一个 `node_event_cursor` 游标（`event_marker` 位置）。置信度：高（表结构 + 游标类型枚举双源）。
- **事件过滤（shouldActOnEvent，`FederatedRepoMirrorCallBack.java:1899`）**：
  - **当** 事件 hints 含 `FederatedReplication`（即该事件本身就是联邦复制产生的）或 `RemoveRepo` 或 `AsyncThread`，**则** 不再向成员传播（**防环**）；
  - **例外放行**：hints 含 `DeleteBuildArtifact` / `CopyMoveArtifact` / `ReplicationAllowed` 者仍传播（复制/构建删除产生的变更要继续联邦化）。置信度：高。
  - **当** 事件 repo 非 federated 或成员列表空，**则** 忽略；remote/virtual 仓库联邦默认关（`remote.repo.federation.enabled=false`、`virtual.repo.federation.enabled=false` 常量闸门）。置信度：中。
  - **当** 路径命中系统文件夹 `EVIDENCE`（`system.folder.contract.enabled`），**则** 该类事件按特殊路径处理（证据制品）。置信度：低。
- **属性同步**：**当** 事件批量（bulk 默认开、批量 500）携带非 delete 事件，**则** 发送前按路径集合取回属性一并下 发（`federated.bulk.properties.enabled/size`）；delete 事件不带属性。置信度：中。

### 2.2 线协议（成员间 REST，全 POST，Bearer token）

发送端 `backend/artifactory-addon-mirror/org/artifactory/addon/mirror/internal/MirrorServiceImpl.java`；接收端 `MirrorResource.java`（`@Path("mirror")`，挂 `/api/mirror`）：

| 端点 | 接收 scope | 语义 | 失败行为 | 置信度 |
|---|---|---|---|---|
| `POST {remoteUrl}/api/mirror/statuses/message` | `internal:federated-repo/mirror-statuses:x` | 成员状态心跳互换；响应带 localUrl 与本端版本 | 空 statuses → 400 `Message is empty`；处理异常 → 500 | 高 |
| `POST /api/mirror/update/message` | `mirror-update:x` | 配置变更（成员增删/启停/模式）下发；`RTFS-LOCAL` 头标记本 HA 内转发 | 本地成员禁用 → 409；Edge → 400 | 高 |
| `POST /api/mirror/event/message` | `mirror-event:x` | 单事件传播 | 见 §2.3 队列状态机 | 高 |
| `POST /api/mirror/events/message` | `mirror-event:x` | 批量事件传播（全同步复用） | 同上 | 高 |
| `POST /api/mirror/sync/message` | `mirror-sync:x`（x 或 r） | 全同步触发/协商 | — | 中 |

- **当** 接收端收到未知 mirror 的心跳（本端无该 repo），**则** 返回 `mirrorNotFound` 状态并记日志 `repo doesn't exist, notifying admin`。置信度：中。
- **当** 接收端队列状态为 `DISABLED`/`OUT_OF_SYNC` 时收到事件，**则** 抛 `MirrorInactiveException`（发送端据此感知对端离同步）。置信度：中。

### 2.3 队列状态机（每成员一个）

`node_event_cursor` 状态（`NodeEventCursorStatus`）与 UI 展示态（`MemberQueueState`）映射：

| 内部状态 | 含义 | UI 态 |
|---|---|---|
| HEALTHY | 正常追平 | healthy |
| PENDING_FS / FS_RUNNING / OUT_OF_SYNC | 待/正在全同步、失同步 | pending_fs |
| EXHAUSTED | 队列错误耗尽 | error |
| DISABLED / DISABLED_BY_SYSTEM | 人工禁用 / 系统禁用 | pending_fs / disabled_by_system |
| STOPPED | 停止 | stopped |

置信度：高（枚举 + 映射 switch 双源）。

- **当** 成员心跳过期（`federated.repo.heartbeat.stale.sec` 默认 **600s**）且队列处于运行态，**则** 会被标 stale 并按策略转入待恢复状态（getStaleRunningQueues）。置信度：中。
- **当** 管理员停用某成员队列（`queueStop`），**则** 游标状态置 STOPPED；队列不存在 → 400 `Queue for repo %s and remote member %s not found`。置信度：中。
- **恢复（recovery）**：手动触发或按状态自动——OUT_OF_SYNC/DISABLED/EXHAUSTED 各有恢复函数（重置游标/重灌队列），支持单 repo（`triggerManualRecoveryForRepository`）与全量（`triggerRecovery`）；RTFS 启用时恢复经 RTFS 通道（`triggerRecoveryViaRTFS`）。置信度：中。

### 2.4 全同步（Full Sync）

- 触发面：**(a)** 队列进入 PENDING_FS 后自动；**(b)** 管理员手动（可 `forceRun`——**当** FS_RUNNING 中强跑，**则** 先移除旧控制器重启）；**(c)** 心跳发现失同步。**当** async 全同步开关关闭（`federated.repo.full.sync.enabled` 默认 true）或远端成员禁用或状态≠PENDING_FS，**则** 拒跑并清理展示。置信度：高。
- 防重：**当** 某 (repo,remote) 的全同步已 RUNNING，**则** 跳过（内存 FullSyncStatus 表）。全同步锁 24h、僵尸锁 1h（`federated.repo.full.sync.lock.time.min/stale.lock.time.min`）。置信度：中。
- 扫描方式：AQL 驱动（默认 `federated.full.sync.use.aql=true`），页大小 100000、4 页/文件、400k 阈值分片、批 1000、文件清单任务并发上限 5、清单任务心跳过期 6min。置信度：中。
- **单向（transmitter→receiver）全同步语义**（`replication/UnidirectionalFullReplicationVisitor.java`，本地视角）：
  - 本地有远端无 → 传播 `create`（artificial replication）；
  - 双方都有但 sha256 不同且本地较新 → 传播 `update`；
  - 属性 propsMd5 不同且本地较新 → 传播 `props`；
  - 远端有本地无 → 向远端传播 `delete`（单向模式本地为权威）；
  - 类型不一致（file↔folder）→ 先 `delete` 后 `create` 覆盖。
  置信度：高。
- 双向（bidirectional）visitor 存在（对称合并语义，时间戳仲裁）；**当** 时间不可比/并发写冲突，**则** 具体仲裁在 `BidirectionalFullReplicationVisitor`（本票未逐条展开，列入待验证）。置信度：低。
- **lag 指标**：`FederatedRepoLag` MBean 暴露成员落后毫秒（阈值 `federated.status.mirrors.lag.threshold.milliseconds`，默认 null=不限；展示上限 1000 条；状态 HTTP 超时 15s）。置信度：中。

### 2.5 RTFS（federation service，gRPC 新通道）

- **当** 实例 RTFS 合格（license/规模）且启用（`rtfs.enabled`），**则** 事件处理走 FEDERATION_SERVICE 游标（operator `jfrog_rtfs_internal` 的 federation_queue）；RTFS 禁用时该队列必须不存在（健康检查 job 周期断言，违规打 `ALERT` error）。置信度：中。
- **当** RTFS gRPC 客户端断连，**则** `FederationHealthCheckJob` 自动重连并补注册队列；自动迁移 job（RtfsAutoMigrationJob）与权益修复 job（RtfsEntitlementRemediationJob）按条件注册。**当** 迁移状态（MIGRATION_STARTED 及之后）与队列存在性不符，**则** 告警。置信度：中。
- **当** RTFS 未启用却对其队列发起读，**则** 抛存储异常 `Federated service is not enabled, can't process requests to federation service queue`。置信度：中。
- HA 内传播（队列控制在 HA 兄弟节点间同步，实锚 `backend/artifactory-addon-ha/.../HaAddonImpl.java:564-622`）：
  - `PUT /api/federation/queue/deregister?remote_member=&source_key=&remote_member_key=` —— 注销队列；
  - `POST /api/federation/queue/reset?internal=true`（JSON QueueState 体）—— 队列重置/重启；
  - `PUT /api/federation/migration?status=<状态>&internal=true` —— RTFS 迁移状态变更；
  - `POST /api/federation/metadata/invalidate?type=&url=[&proxy=]` —— 远端包元数据缓存失效。
  上述传播失败均校验响应有效性并告警。置信度：高（URI 直接可见）。

### 2.6 同步什么 / 不同步什么

| 数据 | 是否联邦同步 | 证据 | 置信度 |
|---|---|---|---|
| 制品二进制 + checksum | 是（create/update 事件 + 全同步） | 事件类型 create/delete/props/update | 高 |
| 属性（properties） | 是（props 事件、bulk 500 批） | getProperties + propsMd5 比对 | 高 |
| 删除 | 是（delete 事件；单向全同步删远端孤儿） | visitor onRemote | 高 |
| 构建信息（build-info 仓） | 是（build-info 仓可联邦，规则见 §1.3） | FederatedBuildInfoValidator | 中 |
| release-bundles v2 仓 | 是（同 repo key 约束） | FederatedReleaseBundlesValidator | 中 |
| 下载统计（last downloaded） | **否**——查询时跨成员实时 AQL 聚合（5 线程池、逐 JPD 执行、失败抛 FederatedStatisticException） | FederatedStatisticsServiceImpl | 高 |
| 包元数据缓存（npm 等） | 否（失效通知 propagateMirrorMetadataInvalid，缓存上限 3h） | metadataMaxCacheTimeHours | 低 |
| 权限/用户/组 | 否（本地安全域；无联邦传播端点） | 未见任何传播 | 中 |
| 代理配置、告警、证书 | 否（无联邦面） | 未见 | 中 |

## 3. 与 replication / distribution 的边界

- **replication（推送/拉取复制）**：独立 addon（`artifactory-addon-replication`），按显式 replication 配置单向搬运；**当** push replication 的源是 federated 仓库，**则** 允许（`localOrFederatedRepoConfigByKey` 把 federated 仓当本地仓源读取）；**当** 复制动作在源仓产生事件且带 `ReplicationAllowed` hint，**则** 该事件**继续**向联邦成员传播（§2.1 放行例外）。即：replication 与 federation 可叠加，federation 不读取 replication 配置。置信度：中。
- **distribution（Release Bundle 分发）**：`artifactory-addon-distribution-handler` 与 federation 无直接调用（全模块 grep 零命中）；唯一交点是 release-bundles v2 **仓库本身**可以做成 federated（成员规则见 §1.3），分发动作（Bundles→Edge）走 distribution 自己的通道。置信度：中。
- **federation ≠ HA**：federation 成员是**独立实例**（各自 DB/filestore），靠 HTTP+token 点对点；HA 成员共享一切（见 ha-cluster.md）。**当** HA 集群内有 federated 仓库，**则** 队列/游标建在共享 DB 上，兄弟节点经 `/api/ha/**` 同步队列控制。置信度：中。
- 联邦仓在 HA 上的一致性由事件表共享保证（node_events 为共享表），UI 展示 lag 聚合自各节点队列状态。置信度：低。

## 4. 删除/重命名成员时的联邦面行为

### 4.1 删除 federated 仓库

- **当** 删除 federated 仓库且存在**被禁用的远端成员**，**则** 拒绝整个删除（`Can't perform changes when disabled member present. Found disabled member: <url>`，FederatedRemoteMembersUpdateException）。置信度：高。
- **当** 删除被允许，**则** 先本地删除，再向所有远端成员推送 DELETE 配置变更（update 消息），随后注销所有指向该 repo 的 FEDERATED_REPLICATION 队列游标（deregister）；注销失败仅 error 日志（`Failed to deregister mirror markers`），不回滚删除。置信度：中。
- **当** 远端成员删除失败（网络/认证），**则** 失败成员列表进异常信息（对 SYNC 动作抛出）；对 DELETE 动作默认**不**抛（仅告警）——远端可能残留孤儿 federated 仓，此后其心跳会收到 `mirrorNotFound`。置信度：中。

### 4.2 成员增删改（UPDATE 路径）

- **当** 更新 federated 仓库配置，**则** 通知集合 = **新成员 ∪ 旧成员**（去重按成员 URL）；每个远端成员并行收配置（线程池上限 `federated.max.config.threads`=5，总等待 1 分钟超时，超时 error `time out elapsed`）。置信度：高。
- **当** 某成员不在新列表（被移除），**则** 其在远端的镜像控制器经 `update(removedMembers)` 停活（deactivate）。置信度：中。
- **当** 远端成员的 enabled 翻转（启↔停）经 update 消息到达，**则** 远端走状态变更处理（handleStatusChangeByUpdate）而非重建配置；**当** 远端本地成员本身禁用，**则** 对 update 返回 **409**（`Remote mirror ... request for update reached a disabled repo`）。置信度：高。
- **当** update 携带 config version 小于远端已知版本，**则** 版本校验拒绝（validateVersion→MirrorResponseStatus）；心跳响应同样带 config version 供对账。置信度：低（状态名可见、阈值逻辑未展开）。

### 4.3 成员 URL 变更（重命名/换域名）

- **当** 管理员执行 base URL 替换（`replaceUrlAndQueues(old,new,replaceQueues)`），**则** 遍历全部 federated 仓库：命中旧 URL 的**非本地**成员改写 URL；RTFS 启用时把替换委托给 federation 客户端（`replaceUrlOnFederationService`），否则可选择同时迁移队列；随后批量 upsert repo 配置并同步更新 Grid 拓扑站点 URL（normalize+校验后 UPDATE）。**当** context 未 ready，**则** 拒绝执行。置信度：中。
- **当** URL 变更未做队列迁移，**则** 旧 URL 队列游标成为孤儿（deregister 需显式触发）。置信度：低。

### 4.4 配置不一致治理

- **当** 某成员配置同步失败，**则** 进入不一致仓库登记（`addInconsistentRepoMember(memberUrl, repoKey)`，成功后移除）；周期 job（registerRepoConfigInconsistencyJob）复核并告警。置信度：中。
- **当** 收到的远端配置与本端冲突（ConfigMismatchException），**则** 由 ConfigMismatchResolver 决议（策略未逐条展开——待验证）。置信度：低。
- UI 冲突面：NegotiationConflictsModel/NegotiationConflictMemberModel 展示协商冲突成员。置信度：低。

## 5. 关键常量表（源码默认值）

| 常量 | 默认 | 效果 | 置信度 |
|---|---|---|---|
| `federated.repo.heartbeat.stale.sec` | 600 | 成员心跳失活阈值 | 中 |
| `federated.repo.full.sync.enabled` | true | 异步全同步开关 | 高 |
| `federated.repo.full.sync.lock.time.min` / `stale.lock` | 1440 / 60 | 全同步锁 24h / 僵尸 1h | 中 |
| `federated.full.sync.use.aql` / `page.size` / `batch.size` / `paging.threshold` | true / 100000 / 1000 / 400000 | 全同步扫描参数 | 中 |
| `federated.bulk.properties.enabled` / `size` | true / 500 | 属性批量 | 中 |
| `federated.max.config.threads` | 5 | 配置同步并发 | 高 |
| `federated.master.tokens.refresh.job.cron` / `grace.days` | 每小时 / 7 | token 刷新 | 中 |
| `federated.auth.token.expiry.margin.sec` | 900 | 刷新余量 | 中 |
| `federated.status.mirrors.lag.threshold.milliseconds` / `limit` | null / 1000 | lag 告警 | 中 |
| `federated.metadata.max.cache.time.hours` | 3 | 远端元数据缓存 | 低 |
| `remote/virtual.repo.federation.enabled` | false / false | remote/virtual 仓联邦闸门 | 中 |
| `federated.repo.max.total.http.connections` | 50 | 成员 HTTP 并发 | 中 |

## 6. 与官方规范的差异/补充（「此条补充官方规范」）

1. 官方 federated 文档只列 UI 概念与 license 要求；本规格补充完整线协议端点（`/api/mirror/*` 五端点 + scope 名）、事件防环 hint 规则、队列状态机与游标存储（`node_events`/`node_event_cursor`）。（补充）
2. 官方未写全同步仲裁细节；本规格补充单向 visitor 的权威仲裁规则（本地新者胜、远端孤儿删除、类型冲突 delete+create）。（补充）
3. 官方「成员间时钟同步」要求一笔带过；本规格补充硬阈值：偏差 >10s 直接 400。（补充）
4. 官方未写删除防护；本规格补充「存在禁用成员时禁止删除/变更」（validateNoDisabledRemoteMemberBeforeDeletion 亦用于删除路径）。（补充）
5. 下载统计为查询时聚合而非同步数据——官方未明说。（补充）

## 7. UNKNOWN / 待验证清单

| # | 条目 | 现状 | 建议验证 |
|---|---|---|---|
| F-1 | bidirectional 全同步并发冲突仲裁（同路径两端同时改） | visitor 存在未逐条读 | 活体双成员并发 deploy 同路径 |
| F-2 | RTFS gRPC 线协议（federation service 通道消息格式） | rtfs-service 反编译仅 3 个 devenv 文件，协议在未反编译 jar | 补齐反编译或抓包 |
| F-3 | Grid topology-service 侧语义（approve-creation 状态机、site plan 何时给 transparent） | 客户端可见、服务端不可见 | 活体多 JPD grid 环境走查 |
| F-4 | MirrorResponseStatus 全枚举与版本不匹配时 UI 提示文案 | 状态名零散可见 | 抓包/前端 bundle |
| F-5 | ConfigMismatchResolver 策略细节 | 异常类型可见 | 定向读 + 活体 |
| F-6 | federated remote/virtual 仓行为（闸门默认关） | 常量与处理钩子可见，实际语义未展开 | 开闸活体验证 |
| F-7 | 事件 hints 位集的完整编码（EventHintType 序列化） | 枚举名可见、编码未对 | node_events 表 dump |
| F-8 | 「NegotiationConflict」UI 流程（FederatedNegotiateEvent 消费面） | 事件类存在 | UI 活体走查 |

## 8. 对 ha-engineer 的最小实现面提示（非规范）

优先级：成员模型+校验链（错误文案即契约）> 事件表+游标队列语义 > `/api/mirror/*` 五端点与 scope > 全同步 visitor 语义 > RTFS（可先按 UNKNOWN 挂起，F-2 未解前不建议按猜测实现）。
