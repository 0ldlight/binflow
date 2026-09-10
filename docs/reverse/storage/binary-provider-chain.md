# BinaryProvider 存储链行为规格 — U-STG-01 取证（LOOP 000 / L000-A）

> 证据源与版本（宪章效力序 B>A>C）：
> - **B 运行时（7.161.20，最高效力）**：本地参照实例 :8082（docker 容器 artifactory）。含 E4 因果实验与元数据库只读探查。原始输出见 `evidence/`。
> - **A 反编译（7.161.24 全量树 + 7.161.20 partial 树）**：`reverse-src/artifactory/backend/**`、`reverse-src/artifactory-7.161.20-partial/src/**`（只读）。
> - **C 安装包（7.161.16）**：本机未定位到独立安装包归档；以 B 镜像内随附工件（binarystore.xml 出厂模板 + binary-store-*-4.372.5.jar 内模板资源）替代，版本差异在条目内注明。
> - **官方文档**：docs.jfrog.com（S3/cloud storage 配置页，见 §6）。
> 置信度：`高` = B 活体 + 模板/文档双源；`中` = 单静态源（jar 模板/类清单/常量）；`低` = 推断。

## 1. 核心结论（对 domain-map 缺口的直接回应）

1. **BinaryProvider 实现类确实不在 A 反编译树**（`find reverse-src/artifactory -path "*jfrog/storage/binstore*"` 零命中，已验证）。但它**随运行镜像可达**：实现在 `binary-store-{api,core,filestore,client,rest}-4.372.5.jar`（tomcat WEB-INF/lib），模板与 provider 类型词表以资源/类清单形式可完整提取——本规格的链组合矩阵即来自该权威静态源（B 效力）。
2. 旧规格所称实现库「org.jfrog.storage.binstore 三方库不可达」需要修正：**不可达的只是反编译源码**；其**配置模板（binarystore-default.xml ×3）、provider 类词表（288 类）、常量（BinaryStoreConstValues）** 均已从镜像内 jar 提取（evidence/ 目录）。
3. 默认存储链出厂即 `<chain template="file-system"/>`（活体 binarystore.xml 与出厂模板逐字节一致），filestore 行为（sha1 寻址、_pre 原子暂存、去重、GC）已全部活体取证。

## 2. 实现库定位（证据锚，非行为断言）

| 组件 | 位置（B 镜像内） | 角色 |
|---|---|---|
| binary-store-api-4.372.5.jar | WEB-INF/lib | 接口 + `BinaryStoreConstValues` 库级常量 |
| binary-store-core-4.372.5.jar | WEB-INF/lib | 核心链模板（file-system/cache-fs/full-db）+ FileBinaryProvider/CacheFS/Sharding/GC/Prune 类 |
| binary-store-filestore-4.372.5.jar | WEB-INF/lib | 企业版云链模板（S3/GCS/Azure/sharding）+ 云 provider 类 |
| binary-store-client-4.372.5.jar | WEB-INF/lib | HA 集群链模板（cluster-*）+ remote provider 类 |
| binary-store-rest-4.372.5.jar | WEB-INF/lib | 节点间二进制传输 REST 面 |
| storage-commons-1.163.0.jar | WEB-INF/lib | 仅 REST 模型（MPU/CloudInfo 等），**不是** provider 实现（修正旧规格） |
| binarystore.xml | $JFROG_HOME/var/etc/artifactory/binarystore.xml | 用户配置入口（ArtifactoryHome.BINARY_STORE_FILE_NAME） |

行为锚（A 树 grep 定位）：加载点 `o.a.a.common.home.ArtifactoryHome#getBinaryStoreXmlFile`；平台集成 `o.a.a.sh.config.BinaryStoreConfigServiceImpl`（partial 树，A 全量树缺此包）；GC 任务 `o.a.a.sh.cleanup.job.BinaryStoreGarbageCollectorJob` / `BinaryStoreTrashCleanupGCJob`（双树均有）。

## 3. 链组合矩阵（模板 → provider 链 → 证据等级）

provider 链记法：`A(B(C))` = A 包装 C 经 B；`{…}` = 子提供者集合；`r2` = redundancy=2。全部条目源出 jar 内 `META-INF/binarystore-default.xml`（B 静态提取，evidence/ 三文件）。

### 3.1 核心模板（binary-store-core）

| template | 链 | 证据等级 |
|---|---|---|
| `file-system` | `file-system` | 高（jar + 活体实况 + 出厂默认） |
| `cache-fs` | `cache-fs(file-system)`；cache-fs 默认 `fileStoreDir=cache` | 高（jar + 官方 docs） |
| `full-db` | `cache-fs(blob)` | 高（jar + 活体 binary_blobs 表存在） |
| `full-db-direct` | `blob` | 中（仅 jar） |

### 3.2 企业版云/sharding 模板（binary-store-filestore，注释原文 "Default chains for Enterprise version"）

| template | 链 | 证据等级 |
|---|---|---|
| `s3-storage-v3` | `cache-fs(eventual(retry(s3-storage-v3)))` | 高（jar + docs.jfrog.com S3 页） |
| `s3-storage-v3-direct` | `cache-fs(s3-storage-v3)` | 高（jar + docs） |
| `s3-storage-v3-archive` | `s3-storage-v3-archive(s3-storage-v3)`（冷归档包装） | 中 |
| `s3-sharding` | `cache-fs(sharding r2{state-aware-s3, state-aware-s3})` | 中 |
| `google-storage-v2` | `cache-fs(eventual(retry(google-storage-v2)))` | 中 |
| `google-storage-v2-direct` | `cache-fs(google-storage-v2)` | 中 |
| `google-archive-storage` | `google-archive-storage(google-storage-v2)` | 中 |
| `azure-blob-storage` | `cache-fs(eventual(retry(azure-blob-storage)))` | 中 |
| `azure-blob-storage-direct` | `cache-fs(azure-blob-storage)` | 中 |
| `azure-blob-storage-v2` | `cache-fs(eventual(retry(azure-blob-storage-v2)))` | 中 |
| `azure-blob-storage-v2-direct` | `cache-fs(azure-blob-storage-v2)` | 中 |
| `azure-blob-storage-archive` | `azure-blob-archive-storage(azure-blob-storage-v2)` | 中 |
| `double-shards` | `cache-fs(sharding r1{state-aware(shard-fs-1), state-aware(shard-fs-2)})` | 中 |
| `redundant-shards` | `cache-fs(sharding r2{state-aware(shard-state-aware-1), state-aware(shard-state-aware-2)})` | 中 |
| `s3-readonly-testing` | `cache-fs(s3-readonly-testing(s3-storage-v3(testing-remote)))` | 中（测试模板） |

### 3.3 HA 集群模板（binary-store-client，config version="2"）

| template | 链 | 证据等级 |
|---|---|---|
| `cluster-file-system` | `cache-fs(sharding-cluster{state-aware(zone=local), remote(zone=remote)})` | 中（jar；docs 提及 cluster 模式） |
| `cluster-s3-storage-v3` | `cache-fs(sharding-cluster{eventual-cluster(zone=local)(retry(s3-storage-v3)), remote(zone=remote)})` | 高（jar + docs.jfrog.com HA/S3 示例） |
| `cluster-google-storage-v2` | 同构，末端 `google-storage-v2` | 中 |
| `cluster-azure-blob-storage` | 同构，末端 `azure-blob-storage` | 中 |
| `cluster-azure-blob-storage-v2` | 同构，末端 `azure-blob-storage-v2` | 中 |

集群模板共享的 sharding-cluster 出厂参数（jar 资源原文）：`readBehavior=crossNetworkStrategy`、`writeBehavior=crossNetworkStrategy`、`redundancy=2`、`lenientLimit=1`、`zones=local,remote`。**此条补充官方规范**（docs 不列全这些默认值）。

### 3.4 provider 类型词表（binarystore.xml `type=` 属性全集）

模板中出现 20 种：`file-system, cache-fs, blob, eventual, retry, sharding, state-aware, state-aware-s3, eventual-cluster, sharding-cluster, remote, s3-storage-v3, s3-storage-v3-archive, google-storage-v2, google-archive-storage, azure-blob-storage, azure-blob-storage-v2, azure-blob-archive-storage, s3-readonly-testing, testing-remote`。
类清单额外可见（不在默认模板、供自定义链用）：federated v1/v2（联邦仓库）、empty（零字节优化）、archive 工具族（B 类清单，evidence/binary-store-class-inventory.txt）。
写入策略类族（sharding 路由用）：`freeSpace / freeSpacePercentage / roundRobin / zone / crossNetwork`；读取策略族：`crossNetwork / roundRobin / zone`（B 类清单，中）。

## 4. 行为规格（「当…则…」句式）

### 4.1 上传原子性与 _pre 暂存（默认 file-system 链，B 活体，高）

| # | 行为 | 置信度 |
|---|---|---|
| A1 | 当客户端 PUT 制品 body 时，服务端将字节流式写入 `filestore/_pre/dbRecord<19位数字>-<32位hex无连字符>-<repoKey>.bin`（数字段疑似 nanoTime，hex 段疑似随机 UUID——命名成分的生成源未取证，标低）；大文件直连 PUT **不经** `tmp/artifactory-uploads`（本实验 30MB 流式路径全程只观测到 _pre） | 高（飞行中快照实证） |
| A2 | 当上传正常完成（201）时，_pre 暂存文件立即消失，同内容出现在 `filestore/<sha1[:2]>/<sha1 全量>`（同分区 rename 语义），字节数与内容 sha1 精确对应 | 高 |
| A3 | 当客户端在传输中掐断连接（连接层中断）时，服务端在数秒内（本实验 <8s）删除 _pre 暂存文件，不创建节点（GET 返回 404）、不落 blob、无 tmp 残留 | 高 |
| A4 | 当客户端声明的 X-Checksum-Sha1 与实测不符时，服务端返回 **409 Conflict**（非 400），错误体含 `Checksum policy 'LocalRepoChecksumPolicy:CLIENT' rejected the artifact` 与完整 original/actual 三算法 checksum 对照；无任何磁盘/DB 残留 | 高（**修正** storage-layout.md「checksum mismatch 400」；官方 REST 文档未写明码，此条补充） |
| A5 | 当 checksum-deploy 请求只带 query/matrix 参数（?sha1=）而不带 `X-Checksum-Sha1`/`X-Checksum-Sha256` 请求头时，返回 400，错误体明示缺哪个头 | 高（补充官方规范：docs 未写必须用请求头） |
| A6 | 当服务进程在写入中途崩溃时，_pre 残留文件的清理触发点与周期：**UNKNOWN**（类清单锚 `PreTempDirCleanupRunnable` 存在；KB 文献称每日清理 dbRecord*.bin——未活体验证周期） | 低 |

### 4.2 去重与 checksum 寻址（高，B 活体 + DB）

| # | 行为 | 置信度 |
|---|---|---|
| D1 | 当客户端上传与既有制品同 sha1 的内容（无论全量重传还是 checksum-deploy 零字节）时，磁盘 blob 数不变——一个物理文件服务 N 个仓库路径，多路径经 `nodes.sha1_actual` 外键共享同一 `binaries` 行；**无硬链接/副本语义** | 高 |
| D2 | 当客户端以正确请求头执行 checksum-deploy（sha1+sha256 均已存在）时，服务端返回 201 与完整 checksums/originalChecksums JSON，不传输任何 body 字节 | 高 |
| D3 | 当删除某个引用制品时，若仍有其他节点（含回收站节点）引用同 blob，checksum search 仍可命中剩余路径，blob 不动 | 高 |
| D4 | blob 物理布局（活体核对 5→9 个 blob 全样本）：`filestore/<sha1[:2]>/<sha1>`，文件名 40 hex 小写、无扩展名；sha256 只入库不入路径——证实 storage-layout.md §2 | 高 |

### 4.3 删除、回收站与 GC 语义（B 活体 + partial 树反编译）

| # | 行为 | 置信度 |
|---|---|---|
| G1 | 当客户端 DELETE 制品时返回 204，节点复制进 `auto-trashcan/<repoKey>/...`，携带 `trash.deletedBy/originalRepository/originalRepositoryType/originalPath/time(epoch ms)` 属性族；原 blob 不删 | 高（证实旧规格） |
| G2 | 当回收站节点存在时，其 `sha1_actual` 引用使 blob 免于 GC——prune 报告 cleaned=0（活体：3 个回收站节点全部手动删除前 prune 零回收） | 高（**新证据**：回收站保留期=物理保留期） |
| G3 | 当手动触发 `POST /api/system/storage/prune/start` 时返回 202 异步；任务遍历 **全部 256 个分片目录**（progress "256 of 256"，日志逐目录输出）；`prune/status` 暴露 timing/progress/report{totalBinariesProcessed,totalBinariesCleaned,totalBytesCleaned}/lastHandledDirectory{per-dir binariesProcessed/binariesCleaned/bytesCleaned} ——此响应结构官方文档未记载，**补充官方规范** | 高 |
| G4 | 当手动触发 `POST /api/system/storage/gc` 时返回 200；服务端先执行与 prune 相同的 FilestorePruner 全目录扫描，再执行 GC 策略 `TRASH_AND_BINARIES`（MinorGcCollector，日志原文）；二者可独立触发 | 高 |
| G5 | 当制品的所有节点引用（含回收站）已被删除后**数分钟内**手动执行 prune 或 gc 时，零引用的 binaries 行与磁盘 blob **均不被回收**（活体两轮实证：processed 3 / cleaned 0）——存在未知的回收资格延迟/事件管道门槛 | 高（现象）/ **原因 UNKNOWN** |
| G6 | GC 策略枚举（partial 树 `GarbageCollectorStrategy`）：`FULL / EVENTS_GC / TRASH_AND_BINARIES / TRASH / TRASH_BINARIES_MARKED_FOR_GC`；调度任务 `BinaryStoreGarbageCollectorJob`（cluster singleton，运行时暂停 SHA256 迁移任务与回收站清理任务），FULL 完成后触发 sharding 均衡器；`BinaryStoreTrashCleanupGCJob` 逐节点清理「标记为 GC 的回收站制品」 | 中（反编译单源；策略名与活体日志互证 → 中高） |
| G7 | 当 GC 以事件驱动运转时，删除事件落入 `node_events` / `node_events_tmp`（分区表）/ `node_events_errors`，候选批次经 GCProvider 供给 MinorGcCollector（batch 尺寸=trashcanMaxSearchResults，工作线程 gcNumberOfWorkersThreads=3，MSSQL 降为 1）——**事件如何转为回收资格（延迟/批处理窗口）未取证** | 中 |
| G8 | GC 相关可调参数（ConstantValues，出厂值）：`gc.intervalSecs=86400`、`gc.trashBinariesCleanup.enabled=false`、`gc.trashBinariesCleanup.intervalSecs=900`、`gc.trashBinariesCleanup.delaySecs=-1`、`gc.eventsCleanup.enabled=false`、`gc.eventsCleanup.delayMillis=20min`、`gc.numberOfWorkersThreads=3`、`gc.readersMaxTimeSecs=10800`、`gc.useIndex=false`、`gc.binaries.joinWithNodes=true`、`gcSkipFullGcBetweenMinorIterations`（A 树 ConstantValues 全表 grep，evidence 转录于规格工作日志）——**修正** storage-layout §5「gcConfig.cronExp=0 0 /4 * * ?」：现行引擎按 `gc.intervalSecs` 间隔驱动，Quartz cron 存疑挂账可以此条收口 | 高（常量双树一致）/中（调度绑定） |

### 4.4 云 provider（S3/GCS/Azure）行为面（静态为主，B 模板+常量；运行时未配云后端）

| # | 行为 | 置信度 |
|---|---|---|
| C1 | 当配置任一云模板（非 -direct 变体）时，链=本地 cache-fs 缓存 + eventual 异步持久化 + retry 重试 + 云终端：上传先落本地缓存并尽快确认，持久化到云由 eventual 层异步补齐（`EventuallyPersistedAddFileTask/DeleteFileTask` 任务族）；-direct 变体去掉 eventual/retry，直写云 | 中（模板结构高置信；异步时序细节中） |
| C2 | 当二进制大小 ≥ `cloud.binary.provider.redirect.threshold.in.bytes`（默认 204800=200KB）且云 provider 支持时，下载可重定向至云（预签名 URL；`enableSignedUrlRedirect` 见官方 docs）；小于阈值直接代理 | 中（常量+docs；URL 有效期 UNKNOWN——需 MinIO 实例抓包） |
| C3 | 当配置 S3 且 `s3.existsCheckAfterAddingStream=true`（默认）时，流式写入后追加存在性校验；`s3.autoBucketCreation.skip=false`（默认）允许自动建桶 | 中（证实 s3-storage-layout §2.2 并上移为库级常量） |
| C4 | 归档模板（-archive）当制品被置归档态后恢复（restore）期间的重复恢复请求：`RestoreAlreadyInProgressException`（类清单锚）——具体 HTTP 呈现 UNKNOWN | 低 |
| C5 | MPU：默认 `multipart.upload.enabled=false`；会话登记于 `storage_multipart_uploads` 表（upload_id PK/created_date/temp_path/status/modified_date/data json，活体 schema 已取证，表空）；参数族与既有 s3-storage-layout §3 一致（A 树 ConstantValues 双树核对无出入） | 高（schema）/中（会话机) |
| C6 | 新增库级常量（BinaryStoreConstValues，B jar 常量池字符串，旧规格未收录）：`cachefsSyncIdleTimeoutSecs` / `cachefsSyncMaxThreads`（缓存回源同步线程池）、`fileSystemMaxUploadSizeBytes`（文件系统链单上传上限）、`tempFolderMaxSizeBytes` / `tempFolderMetricsEnabled` / `tempFolderQuotaSkipCache`（临时目录配额族）、`dbOperationsBulkSize` / `totalSize*`（DB 批量统计）、`federatedTaskHandlerThreads` | 中（常量名提取；默认值未从常量池读出） |

### 4.5 HA 一致性面（静态，未组集群活体）

| # | 行为 | 置信度 |
|---|---|---|
| H1 | 当配置 cluster-* 模板时，本节点写 local zone（state-aware 或 eventual-cluster 包云终端），跨节点读写走 `remote` 动态 provider（zone=remote），节点间传输由 binary-store-rest 的传输端点承载（类锚 `BinaryStoreServlet`）——wire 协议（路径/鉴权头/端口）**UNKNOWN** | 中（模板） |
| H2 | sharding-cluster 出厂读写行为 `crossNetworkStrategy` + redundancy=2 + lenientLimit=1（见 §3.3）——超出 lenientLimit 的写失败是否整体拒绝 **UNKNOWN** | 中 |
| H3 | 当某分片/成员故障时链的降级与恢复（reactivate 收集器类锚存在）、Federated v1/v2 的任务持久化队列语义——**UNKNOWN** | 低 |

### 4.6 配置面（端点/文件）

| 操作 | 面 | 行为 | 置信度 |
|---|---|---|---|
| 读 binarystore.xml | `var/etc/artifactory/binarystore.xml` | 存在即生效；`<chain template="X"/>` 引用 §3 模板；文件头带「修改此文件可能丢失制品」警告注释。加密属性经主密钥加解密（BinaryProviderConfigEncrypter/Decrypter，s3-storage-layout §1.3 证实） | 高 |
| system.yaml | `shared.database`、`artifactory.node.haDataDir/haBackupDir`（HA NFS 数据/备份目录）、`jfrogColdStorage`（冷实例开关） | storage 段在 7.161 仍**不在** system.yaml（full-template 中无活跃 storage 键）——binarystore.xml 仍是唯一存储链入口 | 高（活体+模板） |

## 5. 与既有规格对账

| 既有条目 | 本票结论 |
|---|---|
| storage-layout.md §2（sha1 寻址/分片/去重） | **证实**（活体 9 blob 全样本 + DB 引用计数） |
| storage-layout.md §4（tmp/artifactory-uploads 先行暂存） | **修正**：直接 PUT 大文件观测为流式直写 `_pre/dbRecord*.bin`，未经 tmp/artifactory-uploads；tmp 路径的角色（哪些上传形态走它）降为 UNKNOWN |
| storage-layout.md §5「binarystore 内 _pre 清理 低置信」 | **升级**：默认 file-system 链即使用 _pre（非 cached-fs 专属）；中断上传服务端秒级清理（活体）；周期清理任务类锚 `PreTempDirCleanupRunnable`，cron 值仍 UNKNOWN |
| storage-layout.md「checksum mismatch 400」 | **修正为 409**（活体，错误体含 ChecksumsInfo 明细） |
| storage-layout.md §5「gcConfig.cronExp=0 0 /4」存疑 | **收口建议**：现行引擎见 `gc.intervalSecs=86400` 常量族；Quartz 表达式挂账转入 G8 |
| s3-storage-layout.md §1.1 provider 类型表 | **修正**：`cached-fs`→`cache-fs`、`S3`→`s3-storage-v3`；`jfs`/`jfrog-binary-service`/`double-cache-fs` 在 7.161 binary-store 4.372.5 不存在（历史名称）；全词表以本规格 §3.4 为准 |
| s3-storage-layout.md「实现不可达」 | **修正**：见 §1——jar 内模板/常量/类清单可达并已归档 evidence/ |
| s3-storage-layout.md §2/§3（云常量/MPU 参数） | **证实**（A/B 双树 ConstantValues 无出入），并补充 §4.4-C6 新常量族 |
| storage/README §2.2-5（_pre 清理触发点） | **部分解决**：中断即清（秒级）已证；陈旧残留的周期清理任务未证 |
| storage/README §2.2-9（quota 运行时） | **推进**：tempFolder 配额族常量已录（C6）；超配额 4xx 形态仍 UNKNOWN |

## 6. 官方规范交叉印证

- [Configure Artifactory S3 storage（docs.jfrog.com）](https://docs.jfrog.com/installation/docs/configure-artifactory-to-use-s3-storage)：`s3-storage-v3` / `s3-storage-v3-direct` 模板名与 jar 资源一致。
- [Direct cloud storage（docs.jfrog.com）](https://docs.jfrog.com/installation/docs/step-1-configure-the-artifactory-filestore-for-direct-cloud-storage)：`enableSignedUrlRedirect`（云重定向开关）与 C2 对应。
- [Advanced storage options（docs.jfrog.com）](https://docs.jfrog.com/installation/docs/advanced-storage-options)：`testConnection`、区域/桶配置示例。
- jar 模板独有、官方未文档化：full-db/full-db-direct、double-shards/redundant-shards、s3-readonly-testing、cluster-google/azure 变体、sharding-cluster 默认参数族——均标「此条补充官方规范」。

## 7. U-STG-01 判定建议：**PARTIAL**

已解决（可收口）：
1. 存储链组合全矩阵（24 模板 × 链结构，B 级静态权威源）；
2. file-system 链全部可观察行为：上传原子性（_pre 命名/中断回滚/成功 rename）、去重（三路径一 blob，零字节 checksum-deploy）、sha1 寻址布局、删除→回收站→引用阻断；
3. prune/gc 端点行为与响应结构（含官方未记载的 status 报告 JSON）；
4. 实现库正确定位（binary-store-* 4.372.5），旧「不可达」表述修正。

残余（转新 UNKNOWN，见 §8）：GC 回收资格延迟原因、云链运行时（redirect/预签名/evict）、HA wire 协议、崩溃残留清理周期。

## 8. 待验证清单（低置信度，供转差分/活体/专项票）

1. **GC 资格窗口**（G5/G7）：零引用 blob 何时刻可回收？需 24h+ 观测窗（gc.intervalSecs 出厂 86400s——疑手动 gc 也受同一门槛约束）或反编译 binary-store-core 的 GCProvider 批次源（jar bytecode）。
2. `_pre` 周期清理的调度值（A6）：需等待实例静置或查 qrtz 触发器行（qrtz_cron_triggers 按 SCHED_NAME 关联重查）。
3. tmp/artifactory-uploads 的使用形态（何种上传走 tmp 而非直写 _pre）。
4. 云链运行时：MinIO 后端 + 200KB 阈值上下文件下载，抓 302/预签名 URL 与有效期；cache-fs LRU 淘汰时点。
5. dbRecord*.bin 文件名成分生成源（nanoTime/UUID 假说）。
6. HA：双节点 compose 起集群，抓 BinaryStoreServlet 节点间协议（路径/鉴权/端口）与 crossNetworkStrategy 故障降级。
7. 超配额（tempFolderMaxSizeBytes / 文件系统满）上传的 4xx 码与文案。
8. `full-db` 链 blob 读写路径与 binary_blobs.data 行为（本实例 file-system 模板，表空不可观察）。
