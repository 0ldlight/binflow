# 复制（Replication）行为规格（M6）

> 逆向基线：artifactory-pro 7.161.16（`reverse-src/artifactory/`）。
> 置信度标注：`高` = 代码 + JFrog 官方文档双证；`中` = 仅代码可见；`低` = 推断待动态验证。
> **M15 增量（T-418）**：§9 为手动触发 / 全局封锁 / Test 连通三面 wire 规格（包 B 前置）——官方 docs.jfrog.com REST 参考（OpenAPI 3.1）为主源，`reverse-src` Pro 实现与 T-402a 实测为补白源，体例沿 webhook.md（§5/待验证 #1/#3 两处旧低置信项已由 §9 修订，改动留痕）。

## 1. REST 端点总表

### 1.1 仓库级复制

| 方法 | 路径 | 角色 | 语义 | 成功响应 | 错误 | 置信度 |
|---|---|---|---|---|---|---|
| GET | `/replication/{path: .+}` | admin/user | 获取仓库路径的复制状态 | 200 + `ReplicationStatus` JSON | 404 | 中 |
| POST | `/replication/execute/{path: .+}` | 需 `repo:replication:x` 权限 | 触发复制（带 JSON 请求体） | 200 | 401/404/500 | 高 |
| POST | `/replication/execute/{path: .+}` | 需 `repo:replication:x` 权限 | 触发复制（无 JSON，仅 strategy 参数） | 200 | 404 | 高 |

**`/replication/execute` 请求体**（JSON）：`List<ReplicationRequest>` 对象列表（字段全表见 §9.1——官方 OpenAPI 已登记，M15 解除本文件待验证 #1）。

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

（本节 M6 判级维持；官方 REST 参考已逐字登记同族端点，升级版 wire/错误码/幂等语义见 §9.2——`/system/replications` 三端点官方 OpenAPI 在册，置信度按 §9.2 判 `高`。）

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

（`高` 证见 §9.2：官方 GET 响应例 `{blockPullReplications, blockPushReplications}` 与本字段集逐字对齐。）

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

> **M15 勘误（T-418）**：上段 M6 猜测**不成立**。Pro 版反编译（`PushStrategy` 枚举）给出闭集 **`AUTO` / `TREE` / `EVENT`**：解析大小写不敏感，未知值静默回落 `AUTO`，空串/null 返回 null；官方 OpenAPI 对 `strategy` 参数只写 "Replication strategy to use" 未枚举（枚举集为「补充官方规范」，置信度：高——代码闭集 + 官方参数在场双证）。手动立即触发的 push 内部强制 `TREE`（全树）。详见 §9.2-A。

---

## 6. 复制状态（ReplicationStatus）

`ReplicationAddon.getReplicationStatus(RepoPath)` 返回 `ReplicationStatus` 对象，由 `RestAddon` 实现。具体字段未在反编译代码中完整可见，但返回的 JSON 使用 `application/vnd.org.jfrog.artifactory.replication.ReplicationStatus+json` 媒体类型。

（官方 REST 参考在册对应端点 **Get Replication Status**：`GET /artifactory/api/replication/{repoPath}`——"Returns status based on replication properties annotated on the artifact. Supported by local, local-cached, and remote repositories. Requires Artifactory Pro."，响应 200/401/403/404，2026-09-01 实取；本条置信度 `高`，完整响应体仍待动态验证。）

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

## 9. M15 增量段：手动触发 / 全局封锁 / Test 连通——三面 wire 规格（T-418，解锁包 B）

> **取证基准（webhook.md 先例：官方文档唯一基准 + 置信度分级）**：以 **docs.jfrog.com Artifactory REST 参考（OpenAPI 3.1 逐字）为主源**，2026-09-01 实取（各页 updatedAt 2026-03-31，锚点清单见 §9.8）；`reverse-src/artifactory`（artifactory-pro 7.161.16，**Pro 版复制 addon 实现与 UI rest 树均在场**）为补白源；T-402a 活体/bundle 实测（t226 OSS 7.84.10）为第三源。判级：多源对齐 → `高`；仅单一源 → `中`；推断 → `低`。
>
> **背景（Q5 终裁）**：BinFlow 不引入 cron——本段仅登记 Artifactory 形态语义；cron 双轨为 T-402a R4 留档项，不是 BinFlow 实现项。

### 9.1 端点总表（三面 × 两面）

Artifactory 同一语义常有两个面：**官方 REST 面**（`/artifactory/api/...`，官方参考在册）与 **UI-API 面**（`/ui/api/v1/ui/...`，控制台自用，**不在官方 REST 参考内**——2026-09-01 以官方参考全索引核对：复制族 12 参考页无 test / executeall / executereplicationnow 面）。BinFlow 对外一律落官方 REST 面语义，UI-API 面仅作行为参照。

| 面 | 面别 | 方法 + 路径 | 参数 | 成功响应 | 错误 | 置信度 |
|---|---|---|---|---|---|---|
| A 手动触发 | 官方 REST | POST `/artifactory/api/replication/execute/{repoPath}` | path `repoPath`（必填，minLength 1）；query `strategy`（可选）；body `ReplicationRequest[]`（可选） | 200（无响应体定义） | 400/401/403 + `errors[]` envelope | 高（官方 OpenAPI + 反编译双证） |
| A 手动触发 | UI-API | POST `/ui/api/v1/ui/admin/repositories/executereplicationnow` | query `replicationUrl`；body=本地仓表单模型（含 `replications[]`） | 200 `{info:"The replication tasks was successfully scheduled to run"}`（键形态见 §9.7-V1） | 400/404，逐字见 §9.2-A | 中高（反编译路由+服务、T-402a bundle 调用双源；官方参考无此面） |
| A 手动触发（全量） | UI-API | POST `/ui/api/v1/ui/admin/repositories/executeall` | query `repoKey`；body 不消费（按已存配置跑全部 enabled） | 200 `{info\|warn:...}` | 404 | 中高（同上） |
| A 手动触发（pull） | UI-API | POST `/ui/api/v1/ui/admin/repositories/exeucteremotereplication`（**官方拼写原文如此，含 typo**；BinFlow 勿照抄） | repoKey 自请求解析 | 200 `{info:"Replication task was successfully scheduled to run in the background."}` | 400/403/404 | 中高（反编译路由+服务；bundle 词表） |
| B 全局封锁-读 | 官方 REST | GET `/artifactory/api/system/replications` | — | 200 `{"blockPullReplications":bool,"blockPushReplications":bool}` | 401/403（admin） | 高（官方 OpenAPI + 反编译 + T-402a UI-API 活体 200 三证） |
| B 全局封锁-封 | 官方 REST | POST `/artifactory/api/system/replications/block` | query `push`/`pull`（均 default `"true"`，接受 "true"/"false"） | 200 **text/plain**：`Successfully blocked all replications, no replication will be triggered.` | 401/403 | 高（官方 OpenAPI 例文 + 反编译同串双证） |
| B 全局封锁-解 | 官方 REST | POST `/artifactory/api/system/replications/unblock` | 同上 | 200 text/plain：`Successfully unblocked all replications`（官方例文；变体见 §9.2-B-3） | 401/403 | 高（同上） |
| B 全局封锁（UI 形态） | UI-API | GET / PUT `/ui/api/v1/ui/global/replications/config` | PUT body `{blockPullReplications,blockPushReplications}`（全量两字段，PUT 后整体覆写） | 200（GET 同形回显） | GET 非 any-project-admin 403；PUT 非 admin 403 | GET 高（T-402a 活体 200 实测）；PUT 中（仅反编译） |
| C Test 连通 | 官方 REST | **无对位端点**（参考全索引核对无 test 面）→ BinFlow 定案见 §9.3 | — | — | — | 高（以官方索引缺失为准） |
| C Test（push） | UI-API | POST `/ui/api/v1/ui/admin/repositories/testlocalreplication` | query `replicationUrl`；body=本地仓表单模型 | 200 `{info:"Push replication target url '<url>' tested successfully"}` | 400（逐字见 §9.2-C） | 中（反编译完整实现 + T-402a bundle；OSS 门后不可活体） |
| C Test（pull） | UI-API | POST `/ui/api/v1/ui/admin/repositories/testremotereplication` | 无 query（body=remote 仓表单模型） | 200 `{info:"Pull replication configuration tested successfully"}` | 400/404 | 中（同上） |

**通用错误 envelope（官方 REST 面，OpenAPI 逐字）**：`{"errors":[{"status":<int>,"message":"<string>"}]}`。401 文案 "Bad Credentials - Authentication failed. A valid token is required."；403 文案 "Permission Denied - ..."（block/unblock/global 三端点的 403 为 "The user does not have admin permissions."）。认证：Bearer 或 Basic 均可（OpenAPI securitySchemes 双列）。

**UI-API envelope（补白源，反编译）**：severity 键控消息对象（`info`/`warn`/`error` 单串 + `errors[]` 列表 + `url`），默认码 200；`error(...)` 未显式设码时自动落 **400**。确切 JSON 键集待 Pro 实例抓包（§9.7-V1/V2）。

**`ReplicationRequest` 字段表（官方 OpenAPI 逐字——解除 M6 待验证 #1）**：

| 字段 | 类型 | 默认 | 语义（官方描述要旨） |
|---|---|---|---|
| `url` | string | — | 目标实例上仓库的 URL；**仅 push 用** |
| `username` / `password` | string | — | 目标实例凭据；仅 push 用 |
| `proxy` | string | — | 目标侧代理名；仅 push 用 |
| `disableProxy` | boolean | — | true 时本复制不走代理 |
| `properties` | boolean | **true** | 同步制品属性 |
| `delete` | boolean | **false** | 同步远端删除（含属性元数据） |
| `includePathPrefixPattern` / `excludePathPrefixPattern` | string | — | 路径前缀过滤（含/排） |
| `checkBinaryExistenceInFilestore` | boolean | — | 分布式校验和存储；**需 Enterprise+ license** |

官方注：所有字段对 pull 均可选；`url/username/password` 仅 push 需要。

### 9.2 语义流程（逐面，幂等语义逐条）

**A. 手动触发（Execute Replication Now）**

1. **方向由 path 决定**（官方逐字）：`repoPath` 为本地仓 → push；为 remote 仓 cache → pull；multipush（一仓多目标）Enterprise only。
2. **body 缺省** → 触发该仓**全部既有**复制配置（官方逐字 "If no repositories are provided in the payload, Artifactory will trigger all existing replication configurations."）；body 在场 → 逐项按 `ReplicationRequest`（§9.1 表）执行。
3. **调度语义**（补白源）：push 走「立即全树复制任务」（内部强制 `TREE` 策略，失败/异常时回滚 forced 位）；pull 走「立即 remote 复制任务」。调度为异步——响应只表示「已成功排程」，不等复制完成。
4. **`strategy` query 解析**（补白源，补充官方规范）：`PushStrategy.fromString`——大小写不敏感，合法值 `AUTO`/`TREE`/`EVENT`，**未知值静默回落 `AUTO`**（不报错），空/null → null。官方 OpenAPI 未枚举该参数取值。
5. **封锁门**（补白源）：排程入口先查全局封锁——push 被封 → status error `"Push replication is blocked, skipping replication"`；pull 被封 → `"Pull replication is blocked, skipping replication"`。UI 面最终呈现为 400 `"Replication tasks scheduling finished with errors. Check Artifactory logs for more details."`；官方 REST 面的错误传播形态未单独验证（§9.7-V3）。
6. **目标校验**（补白源）：排程前 `validateTargetLicense`（目标实例 license 校验 + 多目标数上限语义——BinFlow 无 license 概念，语义位不适用）；Test 面（§9.2-C）另有 `validateTargetIsDifferentInstance`（禁自实例）与 `-cache` 目标拒绝。
7. **幂等语义**：触发**不合并、不去重**——重复调用产生重复排程（结果收敛但代价翻倍）；并发护栏为复制锁（max lease 720 分钟，§4.3）。**封锁与触发同门**：封锁期手动触发立即失败（同 5），不是排队等待。（触发不合并=反编译排程链可见；置信度：中）

UI 面错误文案逐字（反编译，供断言）：

| 条件 | 码 | 文案（英文原样） |
|---|---|---|
| body 模型无 repoKey | 400 | `Repository key is not configured.` |
| body `replications[]` 中无 url 匹配 `replicationUrl` | 400 | `Could not find replication.` |
| 仓不存在 | 404 | `Repository '<key>' doesn't exist, the 'run now' function only works for existing repositories` |
| 该仓该 url 无已存配置 | 404 | `Replication config for repository '<key>' and url '<url>' doesn't exist, the 'run now' button only works for exiting replication configurations - save the configuration and try again.`（`exiting` 为官方原文拼写） |
| 排程失败（含封锁门） | 400 | `Replication tasks scheduling finished with errors. Check Artifactory logs for more details.` |
| DNS 失败 | 400 | `Error scheduling push replication task: \nUnknown host: <host>` |

`executeall` 补充三态（200 warn，非错误）：全局封锁中 → `Blocking push replication , to unblock replication please update accordingly the replication section in the configuration`（空格位置官方原文如此）；无启用配置 → `No active push replications are configured forthis repo.`（缺空格，官方原文如此）；multipush 未授权 Enterprise → 仅第一个 enabled 目标运行 + warn。

**B. 全局封锁（blockPush/blockPull——应急刹车）**

1. **参数语义是「选方向」不是「设布尔」**（官方+反编译双证，高）：`push`/`pull` 均 default `"true"`；给 `"false"`（或任何非 `"true"` 串）= 该方向**本次不动**；**缺省（null）视为 true**（= 该方向被封/解封）。即 `block?push=false` 只封 pull 方向；`block`（无参）= 双向全封。
2. **写入即持久**：状态写入中央配置描述符（`blockPushReplications`/`blockPullReplications` 两布尔位）并 save+reload——跨重启保持。
3. **响应为 text/plain 单串**（官方 OpenAPI `content: text/plain`，非 JSON）。消息变体（反编译补白，官方仅例示默认全封/全解串）：
   - block：双向 → `Successfully blocked all replications, no replication will be triggered.`；仅封 pull（push=false）→ `Successfully blocked all pull replications, no pull replication will be triggered.`；仅封 push → `Successfully blocked all push replications, no push replication will be triggered.`；双 false → `No action taken.`（仍 200，**状态零变化**）。
   - unblock 同构：`Successfully unblocked all replications.` / `...all pull replications.` / `...all push replications.` / `No action taken.`
4. **生效面**（高）：a) 手动触发排程门（§9.2-A-5）；b) **事件复制开关计算**——push 事件复制启用判定 = `enabled && enableEventReplication && !blockPushReplications`，即封锁对 cron 轨与事件轨同时生效（与 T-402a R8 官方 tooltip "regardless of configuration" 双证）；c) `executeall` 面在排程前预检 blockPush 并直接 warn 返回。
5. **幂等语义**：**幂等**——同值重复调用 = 同状态再写一遍 + 同文案 200；无版本号/etag 冲突面。（反编译 `updateDescriptor` 无条件覆写；置信度：高）
6. **鉴权**：官方 REST 三端点 admin-only（403 "The user does not have admin permissions."）；UI-API GET 放宽到 any-project-admin（T-402a 活体：OSS 亦 200——该面不受复制 license 门约束），PUT 仍 admin。
7. **license 门**：官方三端点页均标 "Requires Artifactory Pro"；T-402a 活体实测 OSS 对 `/api/replications*` 一律 400 `"This REST API is available only in Artifactory Pro (see: jfrog.com/artifactory/features)…"`，**例外**是 UI-API `global/replications/config` GET 不受门（OSS 200）。BinFlow 无 license 概念，对齐目标即 Pro 形态。

**C. Test 连通（保存前探测，无落盘副作用）**

push 面流程（反编译完整实现，逐字文案）：

1. body 无仓库模型 → 400 `No repository configuration given to test replication with.`
2. `replicationUrl` query 空 → 400 `No url given to identify which replication target to test`
3. 在 body 模型的 `replications[]` 中找 `url == replicationUrl` 的配置；找不到 → 400 `No replication configuration exists for this repo  and url '<url>'`（双空格官方原文如此）
4. 目标 URL 以 **`-cache` 结尾** → 400 `Replication to remote cache repositories is not allowed.`（禁把 remote 缓存仓当复制目标）
5. **HTTP HEAD 探测目标 URL**（携该复制配置中的 username/password/代理/socketTimeoutMillis 构建 client；附 originated 头）：响应 200 或 **302** → 通过；其他状态 → 400 `Connection failed: Target replication URL returned error <status>: <reason>`
6. 目标 license 校验（`validateTargetLicense`——Pro 语义，BinFlow 不适用）
7. 全过 → 200 `Push replication target url '<url>' tested successfully`
8. DNS 失败 → 400 `Error testing push replication config: unknown host '<host>'`（解析消息为 `api` 时以配置 URL 代入）
9. **cron 不参与**：测试请求中 cronExp 为空的复制项在服务端内存里临时填 fake cron（Quartz 形态 `0 0 12 1/1 * ? *`）仅供构造描述符，**不落盘**——即「未保存的草稿配置可直接测」。
10. **幂等语义**：**天然幂等**——纯探测零状态写入，重复调用无副作用；**不看全局封锁态**（封锁拦执行不拦测试）。

pull 面差异（`testremotereplication`）：无 `replicationUrl` query；body 无复制配置 → **404** `No Replication configuration was sent to test.`；先测 remote 仓 URL 连通（同 HEAD 探测复用件），再 `validateTargetIsDifferentInstance`（禁自实例为目标）；成功 200 `Pull replication configuration tested successfully`。

### 9.3 Test 端点形态定案（AC2-1）

**定案：官方 REST 参考无 test 对位端点（索引核对），BinFlow 以 `/api/v1` 自有 C 层登记，不造 Artifactory 兼容路径。** 依据：a) 官方复制族 12 参考页（execute/block/global/status/config 族）无任何 test 面；b) Artifactory 的 test 动作只存在于 UI-API（控制台自用面）；c) BinFlow 既有先例——auth.config.test（SAML/LDAP test）即自有 C 层动作词，未造官方兼容路径。

规格钉死（路径命名 tech-lead 可裁，语义不可变）：

| 项 | 定案 |
|---|---|
| 建议 path | `POST /api/v1/replications/{id}/test`（id=数值 row id，沿 T-405 寻址口径） |
| 请求体 | 无（凭据/URL 取自已存配置）；可选 body `{url:...}` 覆盖探测地址（草稿测）留 T-420 裁 |
| 探测动作 | 对 `target_url` 发 **HTTP HEAD**（携 sealed 凭据解密后的凭据；socket timeout 沿配置），200/302 视为通 |
| 语义位对齐 | `-cache` 目标拒绝（§9.2-C-4）；自实例拒绝（pull 面语义，BinFlow 单向 push 下等价于「目标 URL 不得指向本实例 base URL」——实现票验证）；license 校验**不适用**（BinFlow 无 license） |
| 成功 | 200 `{"ok":true,"status_code":<int>,"message":"..."}`（或族惯例 envelope——T-420 定形） |
| 失败 | 连不通 400（含目标状态码与 reason）；未知 id 404（沿 T-405 措辞族）；凭据解密失败 500 不泄露明文 |
| 幂等 | 无副作用幂等；不产生审计之外的任何状态变化；**不看封锁态** |
| 审计 | `replication.config.test`（§9.4） |

### 9.4 审计词表补词清单（AC2-2，replication.config.* 族——供 T-420/T-422 与 audit owner 同场登记）

现状（internal/audit 词表 M6~M12 块 + emit site）：在册 `replication.push` / `replication.push.failed` / `replication.config.create` / `replication.config.delete`；**`replication.config.update` 已在 httpapi 以字面量 emit（T-405 落地）但未入册**——T-405 遗留 #2，本清单第 1 项即清偿。

| # | 词 | 动作面 | 优先级 | detail 建议载荷 | 备注 |
|---|---|---|---|---|---|
| 1 | `replication.config.update` | PUT enabled（已上线） | **P0（遗留清偿）** | `{"enabled","name"}`（T-405 已定形） | 只需登记常量入 Actions——emit site 与测试已钉 |
| 2 | `replication.config.test` | Test 连通（T-420） | P1 | `{"name","target_url","status_code"}` | 命名沿 `auth.config.test` 先例（控制面探测动词） |
| 3 | `replication.run` | 手动触发（T-422） | P1 | `{"name","target_url"}`（全量触发则 `{"scope":"all"}`） | 命名沿 `gc.run`/`export.run`/`import.run`/`cleanup.run` 的 run 族（手动运行控制面动作）；**与 `replication.push`（引擎执行层）分层不混用** |
| 4 | `replication.block.update` | 全局封锁翻位（T-422） | P2 | `{"blockPush","blockPull"}`（翻转后终态） | 备选形态 `replication.block`/`replication.unblock` 两词（动词面）——**交 tech-lead 裁一**；单词方案与 #1 的 `.update` 族形一致性更好 |

登记纪律提醒：词表是 picker/查询面的单一事实源；emit site 保留字面量（各自测试钉住），新增词走 internal/audit 常量块 + 逐词 provenance 注释（沿 T-346 体例）。

### 9.5 与官方规范的差异/补充（本段登记）

| # | 项 | 说明 | 置信度 |
|---|---|---|---|
| 1 | `strategy` 取值闭集（AUTO/TREE/EVENT + 未知回落 AUTO） | 官方 OpenAPI 只写 "Replication strategy to use" 未枚举——**此条补充官方规范**（反编译闭集） | 高 |
| 2 | block/unblock 消息四变体与 `No action taken.` | 官方仅例示默认全封/全解串——**此条补充官方规范** | 中高 |
| 3 | 封锁对事件轨同样生效（启用判定含 `!blockPushReplications`） | 官方页未展开生效面——**此条补充官方规范**（T-402a R8 tooltip "regardless of configuration" 旁证） | 高 |
| 4 | UI-API 面存在且行为完整（executereplicationnow/executeall/testlocal 等） | 官方 REST 参考不收录 UI-API 面——**此条补充官方规范**（路径/文案/错误码全量） | 中高 |
| 5 | 官方文档瑕疵：`exiting`（execute 链两处应为 existing）、`exeucteremotereplication` 路径 typo、executeall 两处文案空格异常 | 照录登记，BinFlow **不照抄**（T-402a 已警示） | 高（原文在场） |
| 6 | trigger 无「已排程去重」语义 | 官方描述 "Schedules immediate" 隐含一次性排程；重复触发产生重复排程为反编译可见——**补充官方规范** | 中 |
| 7 | Test 面 REST 无对位 | 以官方参考索引缺失为定案依据 | 高 |

### 9.6 实现就绪度（AC3——解锁 T-420/T-422）

| 面 | 就绪判据 | 状态 |
|---|---|---|
| 手动触发 | path/参数/body schema/错误 envelope/封锁门交互/幂等——全在册（§9.1-A、§9.2-A） | **就绪**；BinFlow 落点：`POST /api/v1/replications/{id}/run`（或 `{name}`，沿族寻址）+ `replication.run` 审计；引擎侧复用现有 push 队列（trigger 即「对该配置种一次全量对账」，不需要新执行器） |
| 全局封锁 | 三端点 wire/参数语义/幂等/生效面——全在册（§9.1-B、§9.2-B） | **就绪**；BinFlow 落点二选一交 tech-lead：a) 兼容对齐——照 §9.1-B 三端点形态；b) 自有简形——`GET/PUT /api/v1/replications/global-config`（PUT 全量两布尔；**形态偏离需明示裁决**）+ `replication.block.update` 审计 |
| Test 连通 | 探测动作/通过判据/错误族/幂等/审计词——全在册（§9.2-C、§9.3） | **就绪**（§9.3 表即 T-420 输入） |
| 阻断项 | 无——三面均无双源冲突或未决低置信阻断 | — |

### 9.7 待验证清单（本段新增低置信项）

| # | 项 | 验证途径 |
|---|---|---|
| V1 | UI-API trigger 面确切响应键集（`info`/`warn`/`error` 单键 or 复合）——反编译 FeedbackMsg 推定 | Pro 实例（t226 换 Pro license 或他源）抓包 |
| V2 | 同 V1，test 面 | 同上 |
| V3 | 官方 REST 面（/api/replication/execute）在封锁态的确切错误体（UI 面 400 文案已知） | Pro 实例封锁后 curl |
| V4 | `POST /api/replications/{action}`（enable/disable 族）body schema `ReplicationEnableDisableRequest` 字段——本票范围外，家族完整性留档 | 取 enableordisablemultiplereplications 页全文展开 |

### 9.8 取证锚点（全部 2026-09-01 实取；官方页面 URL 加 `.md` 后缀得 markdown+OpenAPI 版）

- 手动触发（官方）：`https://docs.jfrog.com/artifactory/reference/executepullpushreplication`（updatedAt 2026-03-31，OpenAPI 3.1 含 ReplicationRequest/ErrorResponse schema）
- 封锁三端点（官方）：`.../reference/getglobalsystemreplicationconfiguration`、`.../reference/blocksystemreplication`、`.../reference/unblocksystemreplication`（均 2026-03-31）
- 状态面（官方，旁证）：`.../reference/getreplicationstatus`；启停族（旁证）：`.../reference/enableordisablemultiplereplications`（`POST /api/replications/{action}`，action ∈ enable/disable）
- 参考索引核对途径：`https://docs.jfrog.com/sitemap.xml`（复制族 12 页全列）+ `https://docs.jfrog.com/artifactory/llms.txt`
- 反编译锚点：UI rest 树仓库资源（executereplicationnow / executeall / exeucteremotereplication / testlocal / testremote 五路由 + 服务实现，batch1-core）、系统复制资源（system/replications 三端点，batch1-core）、Pro 复制 addon（blockPushPull / unblockPushPull / 封锁门 / 策略枚举 / 立即排程链，batch3-addons）、UI 全局配置资源（global/replications/config GET/PUT + 服务）
- 实测锚点：T-402a（reports/agents/T-402a.md + t402-evidence/）——R2 license 门 400 文案、R5 列形态与 bundle 调用、R6 test 端点 bundle 证、R8 UI-API 活体 200

---

## 待验证清单（低置信度）

1. ~~`ReplicationRequest` 具体字段结构~~（**已解**：官方 OpenAPI 全字段表见 §9.1，置信度高——T-418）
2. `ReplicationStatus` 完整响应体字段（部分可见：NEVER_RUN 初始态、按 `artifactory.replication.*.result` 属性聚合、任一目标 ERROR → PARTIAL_FAILURE——完整字段待动态验证）
3. ~~复制策略字符串的完整枚举值~~（**已解并勘误**：`AUTO`/`TREE`/`EVENT`，未知回落 AUTO——§5 勘误注 + §9.2-A-4，置信度高——T-418）
4. Pull 复制（RemoteReplication）的完整执行流程（无额外字段，但行为模型与 push 不同）
5. 联邦迁移命令的具体 REST API 端点
6. 复制冲突解决策略（当源和目标同时修改同一制品时）
7. 多线程复制中的并发控制机制
8. （T-418 增）§9.7 V1~V4
