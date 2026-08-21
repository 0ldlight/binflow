# 指标与监控行为规格（M6）

> 逆向基线：artifactory-pro 7.161.16（`reverse-src/artifactory/`）。
> 置信度标注：`高` = 代码 + 文档双证；`中` = 仅代码可见；`低` = 推断待动态验证。

## 1. 内部指标框架

### 1.1 核心开关（ConstantValues）

| 属性名 | 默认值 | 说明 | 置信度 |
|---|---|---|---|
| `metrics.enabled` | true | 全局指标开关 | 中 |
| `storage.per.repo.metrics.enabled` | false | 是否按仓库维度收集存储指标 | 中 |
| `metrics.caches.enabled` | true | 是否收集缓存指标 | 中 |
| `metrics.caches.ignore.names` | `""` | 要忽略的缓存名称列表（逗号分隔） | 中 |
| `metrics.caches.ignore.classes` | `""` | 要忽略的缓存类列表 | 中 |
| `metrics.caches.ignore.packages` | `""` | 要忽略的缓存包列表 | 中 |
| `temp.folder.metrics.enabled` | true | 是否收集临时文件夹指标 | 中 |
| `collect.metrics.timeout` | 120000 (2min) | 指标收集超时时间（毫秒） | 中 |
| `consumption.usage.metrics.enabled` | false | 是否收集消费使用指标（制品下载量等） | 中 |
| `consumption.usage.metrics.collector.interval.sec` | 900 (15min) | 消费指标收集间隔 | 中 |
| `consumption.usage.metrics.packages.count.interval.min` | 360 (6h) | 制品包计数收集间隔 | 中 |
| `federated.metrics.enabled` | false | 是否收集联邦指标 | 中 |
| `federated.metrics.monitoring.interval.time.sec` | 30 | 联邦指标监控间隔 | 中 |
| `federated.metrics.sync.threshold.time.ms` | 20000 | 联邦指标同步阈值（毫秒） | 中 |
| `federated.metrics.exclude.disabled` | true | 联邦指标排除已禁用成员 | 中 |
| `federated.metrics.enable.count.disabled.members` | true | 联邦指标计数已禁用成员 | 中 |
| `federated.metrics.collector.job.sec` | 30 | 联邦指标收集 job 间隔（秒） | 中 |
| `federated.metrics.collector.job.init.delay.sec` | 060 | 联邦指标收集 job 初始延迟（秒） | 中 |
| `federated.metrics.number.of.events.lag.threshold` | 100 | 联邦指标事件滞后阈值 | 中 |
| `pg.schema.metrics.enabled` | true | PostgreSQL schema 指标 | 中 |
| `pg.schema.metrics.tracked.indexes` | 参见反编译 | 追踪的索引列表 | 中 |
| `pg.schema.metrics.job.interval.hours` | 24 | schema 指标收集间隔 | 中 |
| `cold.metric.collection.cron` | `"0 0 0/6 1/1 * ? *"` | Cold storage 指标收集 cron | 中 |
| `oci.manifest.tracking.metrics.enabled` | true | OCI manifest 追踪指标 | 中 |
| `query.rate.limiter.metrics.interval.secs` | 060 | 查询限流指标间隔 | 中 |
| `httpconnections.metrics.max.total.repositories` | 10 | HTTP 连接指标追踪的最大仓库数 | 中 |
| `router.metrics.maintenance.port.enabled` | false | 路由器指标维护端口 | 中 |

### 1.2 可观测性日志服务（`ObservabilityServiceImpl`）

此服务用于 **logs 收集**（非 Prometheus 指标），支持三种日志类型：

#### LogType 枚举（`o.a.a.addon.observability.LogType`）

| 类型 | 说明 | 置信度 |
|---|---|---|
| `USAGE` | 使用量日志 | 中 |
| `LOG_SHIPPING` | 日志传送 | 中 |
| `BILLING` | 计费日志 | 中 |
| `METRIC` | 指标日志（需要 `FeatureAdoptionEnabled` 才支持） | 中 |

#### 日志仓库映射（`repoPath()` 方法）

| 日志类型 | 仓库路径 | 说明 | 置信度 |
|---|---|---|---|
| USAGE | 基于状态的子目录（`Done/` or `Failed/`） | `status` 字段决定子目录 | 中 |
| LOG_SHIPPING | `jfrog-logs/<relativePath>` | 固定仓库 | 中 |
| BILLING | `jfrog-billing-logs/<relativePath>` | 固定仓库 | 中 |

##### USAGE 日志路径推导规则：

1. 如果表单参数 `status` 存在且值为 `SUCCESS` → 路径：`Done/<repoRelativePath>`
2. 如果表单参数 `status` 存在且值为 `FAILURE` → 路径：`Failed/<repoRelativePath>`
3. 如果 `status` 不存在或不合法 → 初始路径（`UsageRepoFolderUtils.createInitialFolderPath`）

##### USAGE 日志校验规则：

- 验证 `type` 字段（必须为合法 `LogType`）
- 验证 `consolidated` 字段（必须为 `"true"` 或 `"false"`）
- 验证 `status` 字段（必须为 `SUCCESS` 或 `FAILURE`）

#### 日志上传请求字段（FormDataMultiPart）

**UsageInfo**：

| 字段 | 说明 | 置信度 |
|---|---|---|
| `productName` | 产品名称 | 中 |
| `type` | 日志类型 | 中 |
| `logName` | 日志文件名 | 中 |
| `serviceId` | 服务标识 | 中 |
| `nodeId` | 节点标识 | 中 |
| `consolidated` | 是否为合并数据 | 中 |
| `logFile` | 日志文件（InputStream） | 中 |

**LogShippingInfo**：

| 字段 | 说明 | 置信度 |
|---|---|---|
| `productName` | 产品名称 | 中 |
| `logName` | 日志文件名 | 中 |
| `serviceId` | 服务标识 | 中 |
| `nodeId` | 节点标识 | 中 |
| `logFile` | 日志文件（InputStream） | 中 |

**BillingInfo**：

| 字段 | 说明 | 置信度 |
|---|---|---|
| `productName` | 产品名称 | 中 |
| `type` | 计费类型（需合法 LogType） | 中 |
| `logName` | 日志文件名 | 中 |
| `feature` | 功能标识 | 中 |
| `logFile` | 日志文件（InputStream） | 中 |

---

## 2. Prometheus / Micrometer 集成（未找到）

在反编译代码中 **未发现** Prometheus 端点（`/metrics` 或 `/actuator/prometheus`）或 Micrometer 依赖。Artifactory 在此版本中的指标通过以下方式收集：

- **内部指标系统**（`metrics.enabled` 开关控制的内嵌指标）
- **存储客户端指标**（`StorageClientMetricsProvider`）
- **可观测性日志**（`ObservabilityService`，通过日志上传到 JFrog 平台）
- **缓存指标**（`cacheMetricsEnabled`）
- **联邦指标**（`federatedMetricsEnabled`）

**结论**：Artifactory 此版本没有公开的 Prometheus 指标端点。OpenMetrics/Prometheus 集成需要在 BinFlow 中自行实现。

---

## 3. 与官方规范的差异/补充

| 项目 | 说明 | 置信度 |
|---|---|---|
| 无 Prometheus 端点 | 在反编译代码中未找到 `/metrics` 路由或 Prometheus 导出器 | 中 |
| 指标为内嵌系统 | Artifactory 的指标主要由内部框架管理，导出到 JFrog 平台的日志服务，而非开放标准 | 中 |
| 可观测性日志 API | 有完整的日志上传 REST API（通过 `ObservabilityService`），用于向 JFrog 平台上报使用/计费/指标日志 | 中 |
| 冷存储指标 | 有独立的冷存储指标收集 cron（每6小时） | 中 |
| 数据库 schema 指标 | Artifactory 追踪 PostgreSQL 索引使用情况（`pg.schema.metrics`） | 中 |

---

## 待验证清单（低置信度）

1. 内部指标的具体输出形式（JMX? 日志文件? 数据库?）
2. `StorageClientMetricsProvider` 的精确度量项
3. 可观测性日志服务的完整 REST 端点路径
4. 缓存指标的具体类型（命中率、大小、过期数等）
5. 指标的 Grafana 仪表盘模板（Artiatory 是否提供、格式如何）
6. 消费使用指标（`consumptionUsageMetricsEnabled`）的计算方式（按制品、按仓库、按时段）