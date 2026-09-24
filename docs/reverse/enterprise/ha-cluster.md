# Artifactory HA / 集群行为规格（ha-cluster）

> 源版本：7.161.24 反编译（部分 7.161.20）——**实测注**：反编译目录 `/Users/lzw/workspace/artifactory-decompiled` 内 Artifactory 自有模块的 `META-INF/maven/**/pom.properties` 一律戳 `7.161.14`（抽查 artifactory-core / common / config / storage-db / addon-ha / addon-federated / addon-mirror 均 7.161.14；access/metadata/router 等客户端库为独立版本号，属正常）。下文以「本反编译源」指代，行为归属按模块内代码描述。
> 置信度：`高` = 反编译 + 官方文档双证；`中` = 仅反编译可见；`低` = 推断/部分可见，待动态验证。
> 证据锚格式：`backend/<模块>/<路径>`（相对反编译根）。clean-room：只写可观察行为，不译代码。

---

## 0. 总模型（一句话）

当 `ha-node.properties` 中 `enabled=true` 且装有 HA 级 license 时，节点把自身登记进**共享数据库**的 `artifactory_servers` 表；集群没有专用成员发现协议——成员发现 = 各节点周期性写心跳 + 读全表；跨节点控制面消息 = 节点间 **HTTP POST**（经本机 Router，带 `X-JFrog-Route-To` 路由头与 Access 签发的 `ha-propagate` scope JWT）。置信度：高（表结构 + 传播实现双源可见，官方 HA 文档描述「共享数据库 + 负载均衡」拓扑一致）。

## 1. 节点类型与加入流程

### 1.1 ha-node.properties 生效面

文件位于 `$ARTIFACTORY_HOME/etc/ha-node.properties`（常量 `ARTIFACTORY_HA_NODE_PROPERTIES_FILE`，见 `backend/artifactory-config/org/artifactory/common/home/ArtifactoryHome.java:130`）。可识别键与效果（`backend/artifactory-config/org/artifactory/common/ha/HaNodeProperties.java`）：

| 键 | 语义 | 可观察效果 | 置信度 |
|---|---|---|---|
| `node.id` | 节点 ID（默认 `Artifactory`，仅 SysConfig 未给时） | 集群内唯一键；`artifactory_servers.server_id`；REST/日志中节点名 | 高 |
| `node.ip` | 对外 IP | 组成 context URL `<ip>:<routerExternalPort>/artifactory` 写入 servers 表 | 中 |
| `enabled`（haEnabled） | HA 开关 | `isHaConfigured()` 为 true；license 文件取 `artifactory.cluster.license`（否则 `artifactory.lic`）；HA addon 激活 | 高 |
| `primary` | 旧「主节点」标记（可选） | 见 §1.4：设了它则回退 primary-based 角色管理 | 高 |
| `membership.port` | 旧 Hazelcast 成员端口 | **本版仅持久化**：注册 servers 行时写 `0`（`INITIAL_CLUSTER_PORT=0`，`backend/artifactory-core/org/artifactory/state/ArtifactoryStateManagerUtil.java`）；无对监听行为可见 | 中 |
| `cross.zone.order` | 跨 zone 顺序 | 仅装载入属性表；具体消费点本反编译源未见（见 §8 UNKNOWN-2） | 低 |
| `artifactory.ha.data.dir` / `artifactory.ha.backup.dir` | 共享数据/备份目录重定向 | 仅存取属性；实际使用点在未反编译的存储实现（§5） | 低 |

**当** ha-node.properties 不存在或 `enabled!=true`，**则** 节点按非集群模式跑：`getServerId()` 返回常量 `Artifactory`（展示名），HA addon 强制禁用（与既有 `docs/reverse/enterprise/runtime-behavior.md` E4 实测「非 HA 部署 addons 列表无 ha」一致）。置信度：高。

### 1.2 节点登记与状态机

当节点启动，**则**向共享库 `artifactory_servers` 表 upsert 一行（列：`server_id, start_time, context_url, membership_port, server_state, server_role, last_heartbeat, artifactory_version, artifactory_revision, artifactory_release, artifactory_running_mode, license_hash`；`backend/artifactory-storage-db/org/artifactory/storage/db/servers/dao/ArtifactoryServersDao.java:40-118`）。置信度：高。

状态机（`backend/artifactory-common/org/artifactory/state/ArtifactoryServerState.java`；迁移写点 `backend/artifactory-core/org/artifactory/state/ArtifactoryStateManagerImpl.java`）：

```
启动 → STARTING ──context ready──→ RUNNING
       │ CONVERTING（DB schema 转换中）→ RUNNING
RUNNING → STOPPING（销毁前）→ STOPPED
任意 → OFFLINE（显式置离线）；展示层对心跳过期者显示 UNAVAILABLE
```

**当** 前状态非 STARTING/CONVERTING，**则** 不允许晋升 RUNNING（仅记 debug，不改状态）。置信度：中。

context URL 规则（`ArtifactoryStateManagerUtil.createContextUrl`）：Router 启用（默认 `router.enabled=true`）→ `<node.ip>:<router 外部端口>/artifactory`；Router 关闭 → `http://127.0.0.1:<tomcat 检测端口>/artifactory`。**当** 邻节点重启后 IP/端口变化，**则** 心跳任务以新 context URL 覆盖旧行。置信度：中。

### 1.3 加入流程（时序行为）

1. **当** 进程启动且 `enabled=true`，**则** 装载 ha-node.properties，日志输出 `Artifactory is running in clustered mode.`（否则 `non-clustered mode`）。
2. **当** 存储上下文初始化需要集群互斥（如 DB 转换），**则** 取 HA 初始化锁：非 HA → 空锁；Derby → 强制 POLLING 型；否则按 `ha.init.lock.default.type`（默认 `polling`）建 DB 锁（`backend/artifactory-storage-db/org/artifactory/storage/db/locks/service/HaInitLockFactory.java`）。置信度：中。
3. **当** DB 行不存在（新节点加入/行被清理），**则** 心跳任务在执行时以 RUNNING 重建自身行（`recreateServerEntryIfNeeded`）。即：**加入 = 写行**，无显式握手/审批。置信度：中。
4. **当** license 未激活或需重取，**则** 心跳任务内自动内部激活：先 `verifyAllArtifactoryServers(true)`，若发现 **重复 license** 直接跳过激活；HA 兼容 license 类型集合 = `Enterprise / Trial / Enterprise Plus / Enterprise Plus Trial / Edge / Edge Trial`（大小写不敏感；`backend/artifactory-core/org/artifactory/storage/db/servers/service/ArtifactoryHeartbeatServiceImpl.java:221-228`）。**当** 运行模式为 OSS/Conan/JCR/Partner license，**则** 不自动激活。置信度：高（HA license 类型与官方文档「HA 支持 Enterprise/Trial」表述一致，Edge 细节为源码补充）。
5. **当** 节点角色管理器初始化，**则** 计算 `server_role`（见 §1.4）并回写 servers 表。

### 1.4 节点角色（现代 Task Affinity vs 遗留 primary）

角色枚举：`TASK_AFFINITY`（可承载调度任务）与 `MEMBER`（普通成员）（`backend/artifactory-common/org/artifactory/storage/db/servers/model/ArtifactoryServerRole.java`；`TASK_AFFINITY` 节点在 `getAffinityTasksForNode()` 报告可承接任意 JobName）。

选择逻辑（`backend/artifactory-addon-ha/org/artifactory/addon/ha/manager/*`）：

- **当** `ha.task.affinity.enabled=true`（默认）**且** `primary` 键未设置，**则** 用 Task-Affinity 管理器：节点按 `artifactory.node.taskAffinity` SysConfig 取值——`none` → MEMBER，否则 TASK_AFFINITY。任意多节点可为 TASK_AFFINITY。
- **当** `primary` 键被设置（不论值），**则** 回退 Primary-based 管理器：`primary=true` 的节点申请成为唯一 TASK_AFFINITY；**当** 集群内已有其他活跃 TASK_AFFINITY 节点，**则** 本节点降为 MEMBER 并打 error 日志（`Could not set ... already exists`）。`primary=false` → MEMBER。
- **当** `taskAffinity` 与 `primary` 同时设置，**则** 警告二者互斥并**仍走 primary-based**。
- **当** HA 未启用，**则** 角色管理为 no-op（日志 `HA is not enabled on this server.`）。

置信度：高（双实现完整可见；与官方文档 7.x「Task Affinity 取代 primary/secondary（Elasticsearch 支持终止后）」一致）。

行为效果：**当** 某调度任务（cron/repeating job）声明 `allowedAffinity=true`，**则** 只有 TASK_AFFINITY 角色且心跳活跃的节点会承接（`getTaskAffinityActiveMembers` 过滤 isTaskAffinity+isRunning+hasHeartbeat，`backend/artifactory-lifecycle/org/artifactory/lifecycle/storage/db/servers/service/ArtifactoryServersCommonServiceImpl.java:99`）；`allowedAffinity=false` 的 job 各节点都可跑。心跳任务自身即 `allowedAffinity=false`。置信度：中。

### 1.5 license 文件面

**当** HA 配置，**则** license 读 `etc/artifactory.cluster.license`（多 license 拼装文件）而非 `artifactory.lic`；每节点把自身 license key hash 写进心跳（`license_hash` 列）。**当** 某 servers 行 hash 等于空串的 SHA1（`da39a3ee...7090`），**则** UI/状态接口判定该节点「未装 license」。license 变更通过 §3.3 的 `licensesChange` 主题广播刷新缓存。置信度：高（文件名常量 + hash 判空 + UI 侧双源；官方 HA 安装文档要求集群 license 文件一致）。

## 2. 集群通信

### 2.1 心跳（DB 轮询模型）

- **当** 节点 RUNNING 且 context ready，**则** 心跳 job 每 `ha.heartbeat.intervalSecs`（默认 **5s**）执行：更新 `last_heartbeat`（epoch ms）与 license hash → 重读全表 → `publishServersUpdatedEvent` → 通知二进制存储集群拓扑变化 → 清理残行。置信度：高。
- **活跃成员判据**（谓词，`backend/artifactory-common/org/artifactory/storage/db/servers/service/ArtifactoryServersCommonService.java:25-32`）：
  - `hasHeartbeat`：`now - last_heartbeat <= ha.heartbeat.staleSecs`（默认 **30s**）；
  - 「其他活跃成员」= 其他 server_id ∧ state=RUNNING ∧ hasHeartbeat。
- **残行清理**：非 CONVERTING 行心跳过期 ≥ `ha.heartbeat.stale.server.cleanup.periodMinutes`（默认 **180 分钟**）删除；CONVERTING 行过期 ≥ 2 小时删除（`qualifiesForDeletion`）。删除时 info 日志含 serverId 与最后心跳时间。置信度：高。
- **成员表缓存**：servers 全表查询结果缓存 `ha.heartbeat.members.cache.duration.secs`（默认 10s）；部分路径强制绕缓存（`getOtherRunningHaMembersWithOutCache`）。置信度：中。
- **当** 心跳写库失败，**则** 记 error 但节点不改状态（无降级动作）——DB 不可达期间本节点将逐渐被同伴视为 stale。置信度：中。

### 2.2 控制面消息分发（HTTP 传播）

实现：`backend/artifactory-addon-ha/org/artifactory/addon/ha/propagate/HaPropagationServiceImpl.java`。

- **目的地解析**：Router 启用（默认）→ 发往**本机** Artifactory URL + 目标 URI，依赖 `X-JFrog-Route-To: <目标serverId>` 头经 Router 转发；Router 关闭 → 直连目标节点 contextUrl + URI。置信度：中。
- **前置校验**：
  - 发送方必须 state ∈ {RUNNING, STARTING}，且自身行已绑定，否则拒绝传播；
  - 目标必须 RUNNING（inactive → 跳过并标记 `isServerInactive`）；
  - **版本闸门**：目标 `artifactoryVersion` 与本节点不同则跳过传播，warn 日志明示「propagation to instances running a different version is restricted」。置信度：高（滚动升级约束与官方文档「升级期间节点版本必须一致」一致）。
- **HTTP 参数**：连接超时 `ha.propagation.http.connectionTimeoutMs`（5s）、socket 超时 `15s`、单路并发 `ha.propagation.http.maxTotalConnections`（150）、调用等待 `ha.propagation.CallTimeoutSecs`（30s）、Gzip 关闭、请求重试 1 次。置信度：中。
- **业务重试**：按事件配置（典型 config/descriptor 变更 3 次 × 300ms；配置文件变更 5 次 × 400ms）。失败响应仅 error 日志 + 失败列表返回调用方，**不重排队列**（除 repo config 有专门补偿，见 §3.2）。置信度：中。
- **并发模型**：普通事件走 cached 线程池；复制事件走独立复制线程池。置信度：低（池实现细节未展开）。

### 2.3 节点间认证

- **当** HA 配置，**则** 启动时向 Access 服务登记可接受 scope 模式 `ha-propagate` 并创建通信 token：JWT、scope=`ha-propagate`、有效期 **30 天**、不可刷新、绑定本节点 ID（`createCommunicationToken`）。置信度：中。
- **当** 节点发送传播请求，**则** 携带头：
  - `X-Artifactory-HA-Security-Token: <JWT>`（无业务 auth 时）；
  - `X-JFrog-Route-To: <目标节点>`；
  - `X-Artifactory-HA-Originated-ServerId: <发送方>`；
  - `X-Artifactory-HA-Event: <事件名>`；
  - `X-Artifactory-HA-Originated-Username: <触发用户>`（无业务 auth 时）。
  置信度：高（发送端与接收端过滤器双源）。
- **当** 接收端（`backend/artifactory-addon-ha/org/artifactory/addon/ha/rest/HaRestAuthenticationFilter.java`）收到带上述 token 的请求，**则** 校验：JWT 签名/有效期通过 ∧ 签发方 serviceId == 本集群 serviceId ∧（默认 `ha.propagation.validate.token.scope=true`）scope 含 `ha-propagate`；任一不过 → **401** `HA propagation authentication failed`（`ha.rest.authentication.fail.closed` 默认 true=失败即拒；置 false 时为遗留 fail-open：校验失败仅记日志仍放行）。置信度：高。
- **认证后身份**：普通传播事件以系统身份（内部 system token）执行；**当** 事件为 `uiDeploy`（URI 以 `ui/artifact/deploy` 结尾且 event 头=uiDeploy），**则** 以 `X-Artifactory-HA-Originated-Username` 指名的真实用户身份执行（委托认证）；用户不存在 → 401。置信度：中。
- 传播消息处理端点 `POST /api/system/propagation/{topic}` 要求角色 `admin` 或 `ha`（`backend/artifactory-rest/org/artifactory/rest/resource/system/PropagationResource.java`）；对**来自自身** 的消息（publishingMemberId==本节点）返回 400 防环。置信度：中。

### 2.4 主题级消息（HaMessage 通道）

主题枚举（`backend/artifactory-common/org/artifactory/addon/ha/message/HaMessageTopic.java`）：`calculateDebian`、`calculateOpkg`、`putOffline`、`configChange`、`aclChange`、`licensesChange`、`nuPkgChange`、`watchesChange`。

- **当** 需广播主题消息，**则** 仅当 HA 启用且存在其他活跃成员且本节点 RUNNING 时，POST `/api/system/propagation/<topic>`（JSON HaMessage 体，含 publishingMemberId）。未知主题 → 接收端 400 `Unable to identify the event type`。置信度：中。
- **当** 收到 `configChange`，**则** 以系统身份触发全量配置重载；`aclChange` → 推高 ACL 缓存 DB 版本；`licensesChange` → 重载 license 缓存；`nuPkgChange`/`watchesChange` → 对应通知处理。置信度：中。

### 2.5 拓扑变化通知

**当** 心跳任务每次执行且 HA 配置，**则** 计算当前「活跃且有 HA 兼容 license」的节点集合并调用集群拓扑监听器链（二进制存储为主要消费者，通知其可用的活跃节点集合）；失败仅 error 日志不影响心跳。置信度：中。

## 3. 配置同步

### 3.1 同步面清单（事件 → 接收端动作）

发送端统一为 HaAddonImpl 的 `propagate*` 方法（`backend/artifactory-addon-ha/org/artifactory/addon/ha/HaAddonImpl.java`），接收端为 `@Path("ha")` 资源（`backend/artifactory-addon-ha/org/artifactory/rest/resource/ha/RestHaResource.java`，角色 `ha`）：

| 触发 | 目标 URI（POST） | 接收端动作 | 重试 | 置信度 |
|---|---|---|---|---|
| central config 描述符保存 | `/api/ha/descriptor/change/` | 重载 central config 描述符 | 3×300ms | 高 |
| etc 下配置文件被 watch 到变更 | `/api/ha/config/change/{change_type}/{name}` | `remoteConfigChanged(name, eventType)`（重读该文件） | 5×400ms | 高 |
| 仓库配置变更 | `/api/ha/repoconfig/change`（JSON 模型） | 应用 repo config 变更 | 3×300ms + 补偿 | 中 |
| policy(cron) 变更 | `/api/ha/policy/change/` | 应用 policy 变更 | 3×300ms | 中 |
| system properties 变更 | `/api/ha/systemconfig/change/` | 重读 system properties | 3×300ms | 中 |
| 调试日志级别变更 | `/api/ha/debug/loggers` | 应用 logger 级别 | 无 | 中 |
| 用户组缓存失效 | `/api/ha/security/groups/change[?groupName=]` | 失效组缓存（单组/全体） | 3×300ms | 中 |
| DB properties（加解密轮换） | `/api/ha/syncDbProperties/{encrypt_decrypt}` | 同步 db.properties | — | 中 |
| master 加密 key 变更 | `/api/ha/syncMasterKey` | 同步加密 key | — | 中 |
| 透明仓库缓存变更 | `/api/ha/transparent-repo/cache/change[?repoKey=]` | 失效/重载 | 3×300ms | 中 |
| QRL/RRL 重算 | `/api/ha/query_rate_limiter/recalculate`、`/api/ha/request_rate_limiter/recalculate` | 重算限流 | 无 | 中 |
| 任务传播 | `/api/ha/propagateTask`（JSON job 上下文） | 在目标节点启动同型任务 | — | 中 |
| curation 设置缓存失效 | `/api/ha/curation/settings` | 失效设置缓存 | 无 | 低 |
| 存储 summary 缓存 | `/api/ha/syncStorageSummary` | 更新缓存 | — | 低 |

（另有 federation 向 HA 兄弟节点传播队列控制的 `/api/federation/queue/*` 等端点，清单见 federation.md §2.5 末。）

### 3.2 配置文件 watch 的语义

**当** `etc/` 下被 watch 的配置文件发生 create/modify/delete，**则** 配置管理器先本地生效，再向其他活跃成员传播文件名与事件类型；**任一**成员传播非 200，**则** `notifyConfigChanged` 返回 false（调用方可感知部分失败）。置信度：中。

### 3.3 热生效边界（可证面）

- 热生效（接收端立即重载，无需重启）：central config 描述符、etc 配置文件、repo 配置、logger 级别、组缓存、ACL 缓存、license 缓存、限流参数、透明仓库缓存。置信度：高（接收端实现即重载动作）。
- **不**热生效（无传播通道，属节点本地文件）：`ha-node.properties` 本身（改动需重启——装载只发生在启动）、`binarystore.xml`（本反编译源中未见其变更传播端点；官方文档称 filestore 配置变更需滚动重启）、`master.key`（有专门 sync 端点但信任面见官方）。置信度：中（「未见端点」为单源否定证据，标中）。

### 3.4 repo config 传播的补偿机制

**当** repo config 传播部分失败，**则** 事件模型进入内存持有器（`RepoConfigPropagationEventHolder`），由周期任务 `retryPropagateRepoConfigsUpdates` 重试直至成功后清除（成功判定：对应节点 200）。**当** 节点重启，**则** 内存持有器丢失——存在短暂失同步窗口（源码未见持久化该持有器）。置信度：中。

## 4. binarystore HA 语义

**重要前提（证据边界）**：二进制提供者链（`binarystore.xml` 的 `cluster-default`/`eventual-consistency`/`sharding` 等提供者实现，包 `org.artifactory.sh.acl.*` 与 jfrog-commons-config binaries）**不在本反编译源内**（全库检索 `BinaryProvider` 实现类零命中；`org.artifactory.sh.acl.ArtifactoryBinaryService` 仅以外部引用出现）。以下为**间接可见面**，链内部行为列入 §8 UNKNOWN-1。

可见事实：

1. **当** 心跳任务算出活跃 licensed 节点集变化，**则** 通过集群拓扑监听器通知二进制存储（§2.5）——即 filestore 层感知节点增减的最小可见通路。置信度：中。
2. **当** HA 启用，**则** 文件/文件夹项的「vault」（防并发写锁）从本地 map 换成 **DB 锁提供者**实现（`backend/artifactory-addon-ha/org/artifactory/addon/ha/provider/FsItemsVaultHaMapImpl.java`：每个 RepoPath 经 LockProvider 取集群锁）。置信度：中。
3. **当** HA 启用，**则** UI 会话仓库换成 JDBC 版（spring-session，表 `UI_SESSION_V2`；本地版为内存 map）——登录会话跨节点共享。置信度：高（双实现类对比 + 表名常量）。
4. **当** 二进制提供者产生 critical error，**则** 周期 job（`binaryStoreErrorNotificationsIntervalSecs`）取出并管理错误集，邮件通知平台管理员邮箱列表，标题 `Critical binary provider error`。置信度：中。
5. **当** 配置 sharding（遗留），**则** `ShardingBalancerJob` 周期调用二进制服务 `tryToOptimize` 再平衡（clusterSingleton job）。置信度：低（遗留特性，现代部署默认 filestore）。
6. 垃圾回收：`BinaryStoreGarbageCollectorJob` 与 trash 清理 GC job 均**非** clusterSingleton（各节点自行对自己的 filestore 视图跑），但 GC 会在运行时暂停 sha256 迁移与 trash 清理（`commandsToStop` 声明）。置信度：中。
7. 主键/ID 生成：**当** HA 启用，**则** ID 生成器切换为按号段批量取（IndexRange，用尽再取）以避免集群冲突（`backend/artifactory-addon-ha/org/artifactory/addon/ha/generator/HaIdGenerator.java`）。置信度：中。
8. 事件表（DB 携带的存储事件）：节点把 create/delete/props 等事件写入共享库 `node_events`（列含 `event_id, timestamp, event_type, path, hints[, repo]`）与临时表 `node_events_tmp`，供 GC 游标、增量索引、复制与联邦队列共享消费（游标表 `node_event_cursor(operator_id, type, event_marker)`；`backend/artifactory-storage-db/org/artifactory/storage/db/event/dao/EventsDao.java`）。**当** 游标落后的消费型 job（如 GC）追平，**则** 事件可按时间窗删除（`DELETE_RANGE_SQL`，保护在 `node_events_errors` 中的事件）。置信度：中。
9. NFS/S3 等外部 filestore 一致性、checksum DB 模式、lazy vs eager 复制语义：**本反编译源不可证**（UNKNOWN-1）。

## 5. 故障面

### 5.1 成员失联的表现

- **当** 某节点心跳停滞超过 30s（staleSecs），**则** 所有同伴把它排除出「活跃成员」：不再向它传播任何事件、不再计入任务亲和候选；UI/System Status 列出该节点并标 `heartbeat_stale=true`、状态显示 `Down`。置信度：高。
- **当** 停滞持续到 180 分钟（CONVERTING 节点 2 小时），**则** 心跳任务自动删除其 servers 行（日志 `Deleting residual Artifactory Server entry`）。置信度：高。
- System Status 展示映射（`backend/artifactory-common/org/artifactory/servers/ServersServiceImpl.java`）：RUNNING+有 license → `Online`；RUNNING 无 license / STARTING / STOPPING / CONVERTING → `Partially Online`（无 license 追加 `- No license installed`）；其余/心跳过期 → `Down`。节点模型字段：`service_name/service_id/node_id/url/version/last_heartbeat/heartbeat_stale/start_time/status/status_details`。置信度：高。
- **当** 管理员在 UI 手工移除节点（`DELETE ui/highAvailability/{id}`），**则** 仅当该节点**当前无心跳**才允许删除；有心跳 → 拒绝（`Deleting a server with heartbeat is not allowed`），UI 显示 `Unable to remove '<id>'`。置信度：高。

### 5.2 优雅停机

**当** 节点正常关机，**则** 先把状态置 `STOPPING`（beforeDestroy），随后等待所有 `GracefulShutdownAware` 组件报告可停：等待线程先 sleep 5s，然后每 1s 轮询 busy 组件数，全部空闲再 sleep 5s 后放行；总超时 `graceful.shutdown.max.request.duration.millis`（默认 **60s**），超时返回 TIMEOUT 后继续停机；最后状态置 `STOPPED`（`backend/artifactory-core/org/artifactory/shutdown/GracefulShutdownServiceImpl.java`）。**当** 其他节点观察 STOPPING/STOPPED 节点，**则** 因状态≠RUNNING 而停止向其传播。置信度：中。

### 5.3 锁与单飞任务

- **当** job 声明 `clusterSingleton=true`，**则** 以 DB 锁保证集群单实例（锁键含 keyAttributes）；**当** 管理员手工启动同类任务而锁已持有，**则** REST 返回 **409**（`The task lock-key ... is already locked`）。示例：repo config 持久化 job（clusterSingleton）、心跳（node-local singleton）。
- **当** 在非属主节点 stopTask/pauseTask/resumeTask，**则** 转经 HA 传播路由到属主节点执行（stopHaTask 等）。置信度：中。
- 孤儿锁治理：DB 锁服务提供按 category/排除 server 集合的孤儿锁释放接口；`ha.orphan.cluster.singleton.lock.period.minutes=30`、`ha.orphan.shift.events.lock.*`、`ha.repo.creation/deletion.lock.*`（创建锁过期 5min/孤儿 2min；删除锁过期 5h/孤儿 1min/等待超时 0s=无限）等常量可见于 `backend/artifactory-config/org/artifactory/common/ConstantValues.java:517-525`。置信度：中。
- **当** 遗留 primary 模式下 primary 失联，**则** 按 §1.4 规则由仍在的 taskAffinity/primary 声明节点接续；**当** 两个节点都自认 primary=true，**则** 后到者被降级 MEMBER（DB 行唯一角色列，无双主并发窗口的可见机制=最后写入者胜出）。置信度：中。

### 5.4 脑裂

本反编译源中**未发现** quorum/多数派机制：成员资格只依赖共享 DB（心跳表）+ DB 锁。**当** 共享 DB 可达而节点间 HTTP 不可达，**则** 各节点仍视彼此为活跃成员（心跳在 DB），但事件传播失败仅记日志、任务单飞锁仍由 DB 保证——不会出现双写属主（单飞面）；**当** DB 本身分裂，**则** 无任何可见的仲裁逻辑（各分片各自为政）。此为「源码可见为准」结论：**无专用脑裂处置，正确性完全押在共享 DB 的可用性与串行化上**。置信度：中（否定性结论基于对 locks/servers/heartbeat 模块的通读，未见仲裁代码）。

### 5.5 HA 相关 REST/管理面

| 方法 | 路径 | 角色 | 语义 | 置信度 |
|---|---|---|---|---|
| GET | `/api/ha-admin/clusterDump` | admin | 输出 ha-node.properties 全量（文本）；HA 未启用 → 400 `HA is not enabled on this server` | 高 |
| GET | ui `highAvailability` | admin | 集群成员列表（HaModel） | 高 |
| DELETE | ui `highAvailability/{id}` | admin | 移除无心跳节点 | 高 |
| POST | `/api/system/propagation/{topic}` | admin/ha | 主题消息入口（§2.4） | 高 |
| POST | `/api/ha/**`（表 §3.1） | ha | 传播事件入口 | 高 |

## 6. 关键常量表（源码默认值）

| 常量 | 默认 | 效果 | 置信度 |
|---|---|---|---|
| `ha.heartbeat.intervalSecs` | 5 | 心跳写库间隔 | 高 |
| `ha.heartbeat.staleSecs` | 30 | 成员活跃判据 | 高 |
| `ha.heartbeat.stale.server.cleanup.periodMinutes` | 180 | 残行清理阈值 | 高 |
| `ha.heartbeat.members.cache.duration.secs` | 10 | 成员表缓存 | 中 |
| `ha.propagation.http.connectionTimeoutMs/socketTimeoutMs` | 5000/15000 | 传播 HTTP 超时 | 中 |
| `ha.propagation.CallTimeoutSecs` | 30 | 单次调用等待 | 中 |
| `ha.propagation.validate.token.scope` | true | 强制校验 ha-propagate scope | 中 |
| `ha.rest.authentication.fail.closed` | true | 传播认证失败即拒 | 高 |
| `ha.task.affinity.enabled` | true | Task-Affinity 角色管理 | 高 |
| `ha.init.lock.default.type` | polling | 初始化锁类型 | 中 |
| `graceful.shutdown.max.request.duration.millis` | 60000 | 优雅停机等待上限 | 中 |
| `ha.orphan.cluster.singleton.lock.period.minutes` | 30 | 孤儿单飞锁回收 | 中 |

## 7. 与官方规范的差异/补充（「此条补充官方规范」）

1. 官方 HA 文档只说「共享数据库 + 负载均衡器」；本规格补充：成员发现的**唯一**机制是 `artifactory_servers` 表心跳轮询，`membership.port` 已退化为持久化占位（写 0）。（补充）
2. 官方未写传播协议细节；本规格补充节点间 HTTP 头、JWT scope、版本闸门、重试参数。（补充）
3. 官方称 7.x 用 Task Affinity；本规格补充 legacy `primary` 键仍受支持且与 taskAffinity 互斥冲突时以 primary 优先。（补充）
4. 官方「节点失联」只描述 UI 现象；本规格补充 30s/180min 两级阈值与自动删行行为。（补充）
5. 心跳 job 会在每次 tick 对活跃+licensed 节点集通知二进制存储（拓扑监听器）——官方未见记载。（补充）

## 8. UNKNOWN / 待验证清单

| # | 条目 | 现状 | 建议验证 |
|---|---|---|---|
| U-1 | binarystore.xml 提供者链 HA 行为（cluster-default→eventual-consistency 链、checksum DB、lazy/eager、NFS/S3 下各节点 filestore 一致性、full gc 与 _trash 目录面） | 二进制提供者实现不在反编译源（`org.artifactory.sh.acl.*` 缺失） | 活体 HA 双节点部署 + S3/NFS 观察 `data/filestore` 布局与 `binarystore.xml` 生效面；或补齐该 jar 反编译 |
| U-2 | `cross.zone.order` 实际消费点 | 仅装载属性，未见消费代码 | grep 其他缺失 jar / 活体配置观察 |
| U-3 | `haMembersIntroduction.*`（30s/30s）常量的现役消费者 | 常量在但引用点未命中（疑似遗留 Hazelcast 时代属性） | 活体日志观察新节点加入时是否有 introduction 行为 |
| U-4 | 脑裂场景（DB 分裂）实测表现 | 未见仲裁代码（§5.4） | 实验环境模拟；预期无仲裁 |
| U-5 | `ha.replication.max.pool.size`（>4 核 32 否则 16）等复制池参数在 HA 下的实际并发 | 仅常量可见 | 压测观察 |
| U-6 | UI_SESSION_V2 表会话冲突面（节点时钟漂移下 spring-session 行为） | 实现委托 spring-session-jdbc | 活体多节点登录验证 |
| U-7 | `syncDbProperties`/`syncMasterKey` 的信任模型（是否要求 token scope） | 端点可见但权限注解仅 `ha` 角色 | 活体抓包 |

## 9. 对 ha-engineer 的最小实现面提示（非规范，供 docs/design/ha-federation.md 起草）

若 BinFlow 做兼容目标，最小可观察面优先级：心跳表语义（5s/30s/180min）> 传播头与 401 行为 > `/api/ha/**` 端点集 > System Status 展示映射 > 角色模型。U-1（filestore 链）是最大不可证缺口，建议单列设计决策而非逆向填充。
