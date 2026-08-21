# 复制（Replication）行为规格（M6）

> 逆向基线：artifactory-pro 7.161.16（`reverse-src/artifactory/`）。
> 置信度标注：`高` = 代码 + JFrog 官方文档双证；`中` = 仅代码可见；`低` = 推断待动态验证。

## 1. REST 端点总表

### 1.1 仓库级复制

| 方法 | 路径 | 角色 | 语义 | 成功响应 | 错误 | 置信度 |
|---|---|---|---|---|---|---|
| GET | `/replication/{path: .+}` | admin/user | 获取仓库路径的复制状态 | 200 + `ReplicationStatus` JSON | 404 | 中 |
| POST | `/replication/execute/{path: .+}` | 需 `repo:replication:x` 权限 | 触发复制（带 JSON 请求体） | 200 | 401/404/500 | 高 |
| POST | `/replication/execute/{path: .+}` | 需 `repo:replication:x` 权限 | 触发复制（无 JSON，仅 strategy 参数） | 200 | 404 | 高 |

**`/replication/execute` 请求体**（JSON）：`List<ReplicationRequest>` 对象列表。

**`/replication/execute` 查询参数**：
- `strategy`（string）：复制策略。无 JSON 体时，系统查找目标仓库路径匹配的已启用 `LocalReplicationDescriptor`，并设置 push 策略。

### 1.2 分布式制品传输（已弃用/流式）

| 方法 | 路径 | 角色 | 语义 | 置信度 |
|---|---|---|---|---|
| POST | `/replication/replicate/file/{tx_path: .+}` | admin | **已弃用**：分发制品（非流式） | 中 |
| POST | `/replication/replicate/file/streaming/{tx_path: .+}` | admin | 流式分发制品（带 `X-Jfrpl-Txid` / `X-Auth-Token-Delegate` / `Authorization` 头） | 中 |

**流式分发请求头**：
- `X-Jfrpl-Txid`：文件事务 ID
- `X-Auth-Token-Delegate`：委托令牌（必填，缺失返回 400 `"Missing X-Auth-Token-Delegate header"`）
- `Authorization`：认证头
- `include_properties`（query bool）：是否包含制品属性

**流式分发请求体**：`FileSpec` JSON（包含 `sourcePath`、`targetPath`、`internalTmpPath` 等）

**流式分发校验**：`delegateToken` 不为空、`transactionPath` 不为空、`targetPath` 不为空，否则返回 400 BadRequest。

### 1.3 全局复制控制

| 方法 | 路径 | 角色 | 语义 | 成功响应 | 置信度 |
|---|---|---|---|---|---|
| GET | `/system/replications` | admin | 获取全局复制配置 | 200 + `GlobalReplicationsConfigDescriptor` JSON | 中 |
| POST | `/system/replications/block` | admin | 阻止 push 或 pull 复制 | 200 | 中 |
| POST | `/system/replications/unblock` | admin | 解除阻止 push 或 pull 复制 | 200 | 中 |

**block/unblock 查询参数**：
- `push`（string）：控制 push 复制
- `pull`（string）：控制 pull 复制

---

## 2. 复制配置模型

### 2.1 基类：`ReplicationBaseDescriptor`

| 字段 | 类型 | 默认值 | 说明 | 置信度 |
|---|---|---|---|---|
| `enabled` | boolean | false | 是否启用复制 | 高 |
| `cronExp` | string | null | Cron 表达式，周期性触发复制 | 高 |
| `syncDeletes` | boolean | false | 是否同步删除操作到目标 | 高 |
| `syncProperties` | boolean | true | 是否同步制品属性 | 高 |
| `repoKey` | string | — | 源仓库键名（必填） | 高 |
| `replicationKey` | string | — | 复制配置唯一标识（必填，DiffKey） | 高 |
| `enableEventReplication` | boolean | false | 是否启用基于事件的复制（实时同步） | 高 |
| `checkBinaryExistenceInFilestore` | boolean | false | 同步前是否检查目标侧是否已存在该二进制 | 高 |
| `includePathPrefixPattern` | string | "" | 仅同步匹配此路径前缀的制品 | 高 |
| `excludePathPrefixPattern` | string | "" | 排除匹配此路径前缀的制品 | 高 |

**事件复制开关**：`isEventBasedReplicationEnabled()` = `enabled && enableEventReplication`，两个条件同时为 true 才启用。

### 2.2 Push 复制：`LocalReplicationDescriptor extends ReplicationBaseDescriptor`

| 字段 | 类型 | 默认值 | 说明 | 置信度 |
|---|---|---|---|---|
| `url` | string | — | 目标远程 Artifactory 实例 URL | 高 |
| `proxy` | ProxyDescriptor | null | 代理配置引用（通过 `@XmlIDREF` 引用已有代理） | 高 |
| `disableProxy` | boolean | false | 是否禁用代理 | 高 |
| `socketTimeoutMillis` | int | 15000 | 连接超时（毫秒） | 高 |
| `username` | string | — | 目标实例用户名 | 高 |
| `password` | string | — | 目标实例密码 | 高 |
| `syncStatistics` | boolean | false | 是否同步制品下载统计信息 | 高 |

**行为**：从本实例推送制品到远程 Artifactory 实例。这是 "push" 模型。

### 2.3 Pull 复制：`RemoteReplicationDescriptor extends ReplicationBaseDescriptor`

无额外字段（仅继承基类所有字段）。

**行为**：从远程 Artifactory 实例拉取制品到本实例。这是 "pull" 模型。

### 2.4 全局复制控制：`GlobalReplicationsConfigDescriptor`

| 字段 | 类型 | 说明 | 置信度 |
|---|---|---|---|
| `blockPushReplications` | boolean | 全局阻止所有 push 复制 | 中 |
| `blockPullReplications` | boolean | 全局阻止所有 pull 复制 | 中 |

---

## 3. Push 复制设置（`PushReplicationSettingsBuilder`）

push 复制执行时使用的参数对象：

| 字段 | 说明 | 置信度 |
|---|---|---|
| `repoPath` | 源制品路径 | 中 |
| `url` | 目标实例 URL | 中 |
| `proxyDescriptor` | 代理配置 | 中 |
| `socketTimeoutMillis` | 连接超时 | 中 |
| `username` | 目标用户名 | 中 |
| `password` | 目标密码 | 中 |
| `deleteExisting` | 是否删除目标侧不存在的制品 | 中 |
| `includeProperties` | 是否同步属性 | 中 |
| `includeStatistics` | 是否同步统计信息 | 中 |
| `checkBinaryExistenceInFilestore` | 是否检查二进制已存在 | 中 |
| `includePathPrefixPattern` | 路径前缀过滤（包含） | 中 |
| `excludePathPrefixPattern` | 路径前缀过滤（排除） | 中 |
| `replicationKey` | 复制配置标识 | 中 |

---

## 4. 事件驱动复制

### 4.1 事件类型

| 方法 | 触发事件 | 置信度 |
|---|---|---|
| `offerLocalReplicationDeploymentEvent(RepoPath, isFile)` | 制品部署/上传 | 中 |
| `offerLocalReplicationMkDirEvent(RepoPath, isFile)` | 目录创建 | 中 |
| `offerLocalReplicationDeleteEvent(RepoPath, isFile)` | 制品删除 | 中 |
| `offerLocalReplicationPropertiesChangeEvent(RepoPath, isFile)` | 属性变更 | 中 |

### 4.2 事件复制属性（ReplicationAddon）

| 属性 | 前缀 | 说明 | 置信度 |
|---|---|---|---|
| `artifactory.replication.*.started` | 复制开始时间戳 | 中 |  |
| `artifactory.replication.*.finished` | 复制完成时间戳 | 中 |  |
| `artifactory.replication.*.stats.progress` | 统计同步进度 | 中 |  |
| `artifactory.replication.*.event.progress` | 事件同步进度 | 中 |  |
| `artifactory.replication.*.sig` | 复制签名 | 中 |  |
| `artifactory.replication.*.result` | 复制结果 | 中 |  |

### 4.3 事件复制配置参数（ConstantValues）

| 属性 | 默认值 | 说明 | 置信度 |
|---|---|---|---|
| `replication.event.queue.size` | 50000 | 事件队列大小 | 中 |
| `replication.properties.max.length` | 100000 | 属性同步最大长度 | 中 |
| `replication.statistics.max.length` | 5000 | 统计同步最大长度 | 中 |
| `replication.full.useEventLog` | false | 是否使用事件日志进行全量复制 | 中 |
| `replication.full.eventlog.commit.durationMillis` | 5000 | 事件日志提交时长 | 中 |
| `replication.full.eventlog.forceSuccessfulFullTree` | false | 强制全树成功标记 | 中 |
| `replication.errorQueue.maxErrors` | 100 | 错误队列最大错误数 | 中 |
| `replication.errorQueue.enabled` | false | 是否启用错误队列 | 中 |
| `replication.errorQueue.retryCount` | 10 | 错误队列重试次数 | 中 |
| `replications.eventbased.workers` | 8 | 事件复制工作线程数 | 中 |
| `replication.eventbased.maxQueueItems` | 500 | 事件复制队列最大项数 | 中 |
| `replication.eventbased.maxPullReplicationsPerRepo` | 30 | 每仓库最大 pull 复制数 | 中 |
| `replication.eventbased.maxPullReplicationInboundEventsPerRepo` | 10000 | 每仓库入站事件上限 | 中 |
| `replication.eventbased.pullDispatcher.timeMillis` | 1511 | pull 事件分发间隔 | 中 |
| `replication.eventBased.connection.maxDelay` | 1800000 (30min) | 事件复制重连最大延迟 | 中 |
| `replication.events.fetch.batch.limit` | 1000 | 事件拉取批次大小 | 中 |
| `replication.events.research.cache.eviction.time.minutes` | 30 | 事件缓存过期时间 | 中 |
| `replication.max.lock.lease.time.minutes` | 720 (12h) | 复制锁最大租约时间 | 中 |
| `replication.consumer.queueSize` | 1 | 复制消费者队列大小 | 中 |
| `replication.checksumDeploy.minSizeKb` | 10 | 校验和部署最小尺寸（KB） | 中 |
| `replication.full.sync.log.progress.interval` | 1000 | 全量同步日志进度间隔 | 中 |

---

## 5. 复制策略（ReplicationStrategy）

代码中 `setPushStrategy` 接受字符串策略名，具体值未在反编译代码中直接可见（外部库）。Artifactory 官方文档提到以下策略（置信度：低）：

- `push`：推送变更到目标
- `push-pull`：双向同步

---

## 6. 复制状态（ReplicationStatus）

`ReplicationAddon.getReplicationStatus(RepoPath)` 返回 `ReplicationStatus` 对象，由 `RestAddon` 实现。具体字段未在反编译代码中完整可见，但返回的 JSON 使用 `application/vnd.org.jfrog.artifactory.replication.ReplicationStatus+json` 媒体类型。

---

## 7. 联邦（Federation）概念

联邦是 JFrog 企业版功能，代码中对应 `FederatedRepoService` 接口（`o.a.a.federation`），与复制是不同的系统。联邦提供：

- 镜像状态（mirror states）管理
- 联邦队列管理
- 联邦迁移（mirroring, demote, evacuate, migrate）
- 二进制提供者 V2（federation binary provider）
- RTFS 客户端（remote transfer file system）
- 认证令牌（federation authentication tokens）
- 网格拓扑（grid topology）

**联邦与复制的关系**：联邦用于跨站点创建读写镜像，形成活动-活动集群；复制是单向或双向的异步同步。M6 阶段 BinFlow 以复制（push/pull）为主，联邦为高级功能，规划阶段不深入。

（置信度：中，仅接口可见，实现未在反编译代码中）

---

## 8. 与官方规范的差异/补充

| 项目 | 说明 | 置信度 |
|---|---|---|
| 权限 | 复制端点需要 `admin` 或 `user` 角色，`/execute` 额外需要 `repo:replication:x` 权限范围 | 高 |
| 事件驱动复制 | 官方文档描述事件复制为 "near real-time"，但反编译代码中事件队列和worker配置可见，默认 `enableEventReplication=false` | 中 |
| 全局阻止 | `blockPushPull` 和 `unblockPushPull` 接受 `push` / `pull` 查询参数，可独立控制两类复制 | 中 |
| 流式分发 | 非复制内容，而是 JFrog Distribution 集成，用于跨实例分发制品 | 中 |
| 复制校验 | `validateTargetIsDifferentInstance` 保证目标不是自身，`validateTargetLicense` 校验目标许可 | 中 |

---

## 待验证清单（低置信度）

1. `ReplicationRequest` 具体字段结构（仅在外部 RestAddon 库中定义）
2. `ReplicationStatus` 完整响应体字段
3. 复制策略字符串的完整枚举值（`push`、`push-pull` 等）
4. Pull 复制（RemoteReplication）的完整执行流程（无额外字段，但行为模型与 push 不同）
5. 联邦迁移命令的具体 REST API 端点
6. 复制冲突解决策略（当源和目标同时修改同一制品时）
7. 多线程复制中的并发控制机制