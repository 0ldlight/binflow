# S3 / 对象存储配置行为规格（M6）

> 逆向基线：artifactory-pro 7.161.16（`reverse-src/artifactory/`）。**注意**：S3 二进制提供者实现代码（`org.jfrog.storage.binstore` 及相关类）不在 `reverse-src/` 中，位于 JFrog 通用库中。本规格基于 `binarystore.xml` 模板、`ConstantValues` 配置属性、`BinaryStoreConfigService` 接口行为推断。
> 置信度标注：`高` = 代码 + 文档双证；`中` = 仅代码/配置属性可见；`低` = 配置属性推断，待验证。

## 1. binarystore.xml 链式架构

### 1.1 配置结构

`binarystore.xml` 使用 **链（chain）** 架构：

- 默认模板：`<chain template="file-system"/>` 对应本地文件存储
- 可扩展为多层链：`<chain>` 内包含多个 `<provider>` 元素，按顺序路由
- 每个 provider 有 `id`（唯一标识）和 `type`（提供者类型）
- 支持的 provider 类型（推断自 `binaryStoreConstValues` 和社区）：`file-system`、`cached-fs`、`S3`、`google-cloud-storage`、`azure-blob-storage`、`sharding`、`double-cache-fs`、`retry`、`state-aware`、`jfs`、`jfrog-binary-service`（置信度：中）

### 1.2 配置管理

- 文件位置：`$JFROG_HOME/var/etc/artifactory/binarystore.xml`
- 通过 `ArtifactoryHome.getBinaryStoreXmlFile()` 读取
- 由 `BinaryStoreConfigService` 接口管理（Spring bean，`StorageServiceConfig` 中注入）
- `BinaryStoreConfigServiceImpl` 实现类提供平台集成功能（HA 支持、联邦客户端、HTTP 客户端创建等）

### 1.3 二进制提供者配置加密

- `BinaryProviderConfigEncrypter` / `BinaryProviderConfigDecrypter`：加密/解密 binarystore.xml 中的敏感配置（如云存储访问密钥）
- 使用 `JFrogMasterKeyEncrypter` 作为主密钥（从 `ArtifactoryHome.getJFrogMasterKeyEncrypter()` 获取）
- 加密/解密实现：`BinaryProviderConfigEncrypterImpl`

---

## 2. S3 相关配置属性（ConstantValues）

### 2.1 S3 备份配置

| 属性名 | 默认值 | 说明 | 置信度 |
|---|---|---|---|
| `backup.s3.bucket` | — | S3 备份 Bucket 名称 | 中 |
| `backup.s3.folder` | — | S3 备份 Bucket 中的文件夹路径 | 中 |
| `backup.s3.accountId` | — | S3 备份 AWS 账户 ID | 中 |
| `backup.s3.accountSecretKey` | — | S3 备份 AWS 账户密钥 | 中 |

### 2.2 S3 通用配置

| 属性名 | 默认值 | 说明 | 置信度 |
|---|---|---|---|
| `s3.existsCheckAfterAddingStream` | true | 上传流后检查 S3 对象是否存在 | 中 |
| `s3.autoBucketCreation.skip` | false | 是否跳过自动创建 Bucket（true = 不自动创建） | 中 |

### 2.3 Cloud Binary Provider 通用配置

| 属性名 | 默认值 | 说明 | 置信度 |
|---|---|---|---|
| `cloud.binary.provider.redirect.threshold.in.bytes` | 204800 (200KB) | 二进制下载时 HTTP 重定向到云存储的阈值（小于此大小的二进制不重定向，直接代理） | 中 |
| `cloud.binary.store.range.support.enabled` | false | 是否启用云存储的 Range 请求支持 | 中 |
| `cloud.binary.store.force.skip.cache.for.range` | false | 是否对 Range 请求强制跳过缓存层 | 中 |

### 2.4 二进制提供者通用配置

| 属性名 | 默认值 | 说明 | 置信度 |
|---|---|---|---|
| `binary.provider.artifacts.existence.cache.expirySecs` | 60 | 二进制存在性缓存过期时间（秒） | 中 |
| `binary.provider.artifacts.existence.cache.size` | 20000 | 二进制存在性缓存最大条目数 | 中 |
| `binary.provider.prune.chunk.size` | 500 | 二进制清理分块大小 | 中 |
| `binary.provider.prune.checkpoint.numberOfFiles` | 100000 | 二进制清理检查点文件数 | 中 |
| `binary.store.error.notification.intervalSecs` | 30 | 二进制存储错误通知间隔 | 中 |
| `binary.store.error.notification.staleSecs` | 30 | 二进制存储错误通知过期时间 | 中 |
| `binary.store.full.binary.provider.chain.ping` | true | 是否对完整二进制提供者链进行健康探测 | 中 |

---

## 3. Multipart Upload（MPU）配置

### 3.1 MPU 总开关

| 属性名 | 默认值 | 说明 | 置信度 |
|---|---|---|---|
| `multipart.upload.enabled` | false | **默认禁用**。是否启用 multipart 上传（大文件分块上传到 S3） | 中 |

### 3.2 MPU 参数配置

| 属性名 | 默认值 | 说明 | 置信度 |
|---|---|---|---|
| `multipart.upload.token.expiry.secs` | 172800 (2天) | Multipart 上传令牌过期时间 | 中 |
| `multipart.checksum.deploy.token.expiry.secs` | 300 (5分钟) | 校验和部署令牌过期时间 | 中 |
| `multipart.jfrog.cli.min.required.version` | `"2.62.2"` | 支持 MPU 的最小 JFrog CLI 版本 | 中 |
| `multipart.upload.threads` | 20 | MPU 并行上传线程数 | 中 |
| `multipart.upload.cleanup.cron` | null | MPU 清理定时任务 cron 表达式 | 中 |
| `multipart.upload.cleanup.period.days` | 7 | MPU 清理周期（天），超期未完成的分块上传会被清理 | 中 |
| `multipart.upload.heartbeat.interval.secs` | 60 | MPU 心跳间隔（秒），客户端定期发送心跳维持上传会话 | 中 |
| `multipart.upload.idle.timeout.secs` | 180 (3分钟) | MPU 空闲超时（秒），超过此时间无心跳则会话失效 | 中 |

### 3.3 MPU 行为语义

1. **默认禁用**：`mpuEnabled=false`，客户端需通过配置显式开启
2. **客户端交互**：需 JFrog CLI >= 2.62.2 版本支持
3. **会话管理**：
   - 上传令牌（token）有效期 2 天
   - 校验和部署令牌有效期 5 分钟
   - 心跳每 60 秒一次，180 秒无心跳则会话超时
4. **清理策略**：
   - 通过 cron 表达式 `mpuCleanupCron` 调度（默认 null，需显式配置）
   - 清理周期 7 天（`mpuCleanupPeriodDays`），超过 7 天未完成的分块上传被清理
5. **并行度**：最多 20 个线程并发上传

---

## 4. 存储二进制服务（StorageBinaryServiceImpl）

`BinaryStoreConfigService` 的实现类 `BinaryStoreConfigServiceImpl` 提供以下平台集成：

| 功能 | 方法 | 说明 | 置信度 |
|---|---|---|---|
| 获取 GCP 凭证 | `getBinaryStoreGcpCredentialsFile()` | Google Cloud Storage 凭证文件路径 | 中 |
| 权利检查 | `isEntitledForRshProvider()` | 是否可使用 RSH 二进制提供者（远程存储） | 中 |
| 权利检查 | `isEntitledForCustomEndpoint()` | 是否可使用自定义端点（S3 Compatible） | 中 |
| CDN 权利 | `isEntitledCdn()` | 是否可使用 CDN 加速 | 中 |
| HTTP 客户端 | `createClosableHttpClient(Map)` | 为云存储提供可配置的 HTTP 客户端（连接数、超时、代理） | 中 |
| 联邦客户端 | `getFederationClient()` | 创建联邦二进制传输客户端 | 中 |
| 联邦 V2 | `shouldUseFederationV2()` | 是否使用联邦二进制提供者 V2 | 中 |
| 二进制同步 | `binariesSync(List<BinaryElement>)` | 发布二进制同步事件（BinariesSyncEvent） | 中 |
| 制品保留 | `getArtifactRetentionBinaryHeaders(repoKey)` | 获取制品保留（cold storage）的二进制请求头 | 中 |
| Prune 操作 | `deletePruneStopMarker()` / `updatePruneReport()` | 管理二进制清理（prune）状态 | 中 |

---

## 5. BinFlow 差异与设计建议

### 5.1 与 Artifactory 的架构差异

| 项目 | Artifactory | BinFlow M6 建议 | 说明 |
|---|---|---|---|
| 配置格式 | `binarystore.xml`（XML） | YAML（与 `config-formats.md` 一致） | Artifactory 用 XML + JAXB 绑定；BinFlow 已采用 YAML |
| 链式架构 | 多层 provider 链（缓存→S3） | 直接配置存储后端 URL | M6 阶段可简化为一个存储后端枚举 |
| 凭证加密 | `JFrogMasterKeyEncrypter` | 环境变量/密钥文件 | 简化安全模型 |
| 权利检查 | `EntitlementType` 检查 | 无需（开源产品） | Artifactory 按企业版特性收费 |

### 5.2 S3 存储后端必需配置项

| 配置项 | 必需 | 说明 |
|---|---|---|
| `endpoint` | 是 | S3 兼容端点（AWS S3 或 MinIO 等） |
| `bucket` | 是 | Bucket 名称 |
| `region` | 是 | AWS 区域 |
| `accessKey` | 是 | 访问密钥 |
| `secretKey` | 是 | 秘密密钥 |
| `prefix` (folder) | 否 | 存储路径前缀 |
| `pathStyleAccess` | 否 | 是否使用路径样式（非虚拟主机） |
| `uploadPartSize` | 否 | 分块上传每块大小 |
| `maxConnections` | 否 | HTTP 连接池大小 |

---

## 6. 与官方规范的差异/补充

| 项目 | 说明 | 置信度 |
|---|---|---|
| S3 实现代码不可见 | S3 二进制提供者实现在 JFrog 通用库中，不在 `reverse-src/` 内。本规格基于配置属性推断 | 中 |
| binarystore.xml 模板 | 官方文档仅提供 `file-system` 模板，S3/云存储的完整 XML 配置需参考 JFrog 官方文档或社区 | 中 |
| MPU 默认禁用 | `mpuEnabled=false`——与 AWS S3 的默认行为不同，Artifactory 的 MPU 是其自定义的客户端交互协议，非直接使用 S3 原生 multipart upload API | 低 |
| 重定向阈值 | 200KB 阈值表示小于此大小的制品直接代理，大于此大小的重定向到云存储 URL，用于优化传输 | 中 |

---

## 待验证清单（低置信度）

1. S3 binary provider 的完整 XML 配置结构（`id`、`type`、`endpoint`、`bucketName`、`path`、`identity`、`credential` 等）
2. MPU 协议的具体握手流程（JFrog CLI 客户端与 Artifactory 之间的交互）
3. 云存储重定向机制（签名的预签名 URL 生成方式、有效期）
4. `cached-fs` 模板的行为（本地缓存 + 远程 S3 的两层架构）
5. `sharding` 模板的行为（多个后端负载均衡）
6. 二进制提供者链中多层 provider 的路由规则（error fallback、健康检查）
7. GCS 和 Azure Blob 的配置端点与行为（仅可推断与 S3 类似）