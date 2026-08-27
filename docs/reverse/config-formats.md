# 配置格式行为规格（M1：artifactory.config.xml / binarystore.xml → BinFlow YAML）

> 逆向基线：`reverse-src/artifactory/config-templates/`（一手模板：artifactory.config.xml / binarystore.xml / mimetypes.xml / artifactory.system.properties / logback.xml）+ `o.a.a.descriptor.*` / `o.a.a.common.ConstantValues`。
> 官方参照：JFrog "Artifactory Configuration XSD / Descriptors"、"System YAML Configuration"。
> 置信度：`高` 双证 / `中` 仅代码/模板 / `低` 推断。

## 1. binarystore.xml（blob 存储链）——T-303 复核版（2026-08-26）

> **取证边界（先读）**：provider 链装配器 / 模板展开器本体（`org.jfrog.storage.binstore.manager.BinaryProviderManager` 及其 config 解析类）位于 JFrog 通用库 jar，**不在反编译范围**——reverse-src 仅有接口与模型类的引用证据（`sh/service/StorageBinaryServiceImpl.java` import `...binstore.manager.BinaryProviderManager`；`sh/config/BinaryStoreConfigServiceImpl.java` import `...binstore.ifc.model.*`）。因此本节条目为「外围代码直证」或「官方文档 + 运行时旁证」双证；展开器内部行为（非法组合报错形态等）如实标 `未能定位`，不编造。
> 出处缩写：〔代码〕= 反编译类#方法（artifactory-pro 7.161.16）；〔官方〕= JFrog Filestore Configuration 文档（docs.jfrog.com/installation/docs/*）；〔模板〕= reverse-src 一手 `config-templates/binarystore.xml`。

### 1.1 文件位置、发现顺序与缺省行为

| # | 行为 | 出处 | 置信度 |
|---|---|---|---|
| L1 | 位置 = 固定单一路径 `<etcDir>/binarystore.xml`（7.x 解析为 `$JFROG_HOME/var/etc/artifactory/binarystore.xml`）；**无搜索路径、无备选文件名、无 classpath 回退**（与 mimetypes.xml 的内置回退不同） | 〔代码〕`common/home/ArtifactoryHome#getBinaryStoreXmlFile`（= etcDir/binarystore.xml）+ `#create()`（etcDir = sysLayout.getServiceEtc()；SysLayout 类未反编译，具体路径段由〔官方〕双证） | 高 |
| L2 | 文件为**强制配置**：进入 HA 共享配置清单，元数据 mandatory=true、encrypted=true | 〔代码〕`common/config/adapter/ArtifactoryConfigurationAdapter#initSharedConfigs`：`SharedConfigMetadata(getBinaryStoreXmlFile(), "/META-INF/default/binarystore.xml", true, true, false)` | 高 |
| L3 | 缺省播种：etc 目录缺此文件时由内置资源 `/META-INF/default/binarystore.xml` 物化到 `etc/binarystore.xml`；默认内容 = `<config version="1"><chain template="file-system"/></config>`（与一手模板逐字一致）。播种触发条件（仅缺失时 vs 每次启动）的执行器未反编译 | 〔代码〕`#initDefaultConfigs`（MetaInfFile：资源→目标映射）+〔模板〕 | 高（种子源与默认内容）/ 中（触发时机） |
| L4 | 凭据落盘加密：启动时用实例主密钥对 binarystore.xml **就地加密**（云 provider 的 identity/credential 等敏感字段 → 加密串），后续启动解密读入 | 〔代码〕`security/ArtifactoryEncryptionServiceImpl#encryptBinaryStoreConfig`（onContextCreated 调用）+ `sh/service/StorageServiceConfig`（`BinaryProviderConfigEncrypterImpl` bean，密钥 = JFrogMasterKeyEncrypter） | 高 |
| L5 | 读取时机 = 仅启动装配；未见热更新/reload 路径（改文件需重启生效） | 〔官方〕Filestore 各页「restart」措辞；〔代码〕未见 reload 监听 | 中 |
| L6 | 支持包（system info zip）收集此文件 | 〔代码〕`support/core/collectors/configfiles/ArtifactoryConfigFileCollector` | 高 |

### 1.2 模板体系（ADR-0036 分歧门裁决依据）

模板 = **固定注册表的命名链速记**：`<chain template="X"/>` 展开为完整嵌套 provider 链；参数经 `<chain>` 兄弟位置的顶层 `<provider id="Y">` 块按 **id 匹配**注入展开链同名 provider（模板与覆盖块共存——〔官方〕file-system 覆盖例、cache-fs NFS 双覆盖例、S3 参数覆盖例三证）。

| 模板名 | 展开链（外→内 = 最靠近 Artifactory → 最远存储） | 出处 | 置信度 |
|---|---|---|---|
| `file-system` | `file-system` | 〔官方〕file-system template 页 +〔模板〕默认文件 | 高 |
| `cache-fs` | `cache-fs( file-system )` | 〔官方〕cached-filesystem-binary-provider 页 | 高 |
| `full-db` / `full-db-direct` | 二进制入数据库（blob 型）；展开细节本票未取全文 | 〔代码〕运行时模板名引用（`storage/FileStoreStorageSummary#isFullDb` 比较 `"full-db"`）+〔官方〕TOC 专页存在 | 名录 高 / 展开 中 |
| `s3-storage-v3-direct` | `cache-fs( s3-storage-v3 )` | 〔官方〕S3 Binary Storage Templates 页 | 高 |
| `s3-storage-v3` | `cache-fs( eventual( retry( s3-storage-v3 ) ) )` | 同上 | 高 |
| `cluster-s3-storage-v3` | `cache-fs( sharding-cluster( eventual-cluster( retry( s3-storage-v3 ) ), dynamic remote ) )`，附带默认参数块（redundancy=2 / lenientLimit=1 / zones=local,remote 等） | 同上 | 高 |
| `s3-sharding` | `cache-fs( sharding( state-aware-s3 × N ) )` | 同上 | 高 |

模板名在运行时的地位（本票关键发现，双证）：

- 模板名**展开后仍保留**为 `BinaryProvidersInfo.template` 字段，且被产品代码直接消费（非仅解析期糖）：
  - 〔代码〕`ui/rest/service/admin/advanced/systeminfo/GetSystemInfoService#updateStorageInfo`：`storageInfo.put("Storage Type", getBinaryProviderInfo().template)`——模板名**原样出现在系统信息输出**。
  - 〔代码〕`storage/FileStoreStorageSummary`：构造器保存 `binariesStorageTemplate = template`；`cacheSize` 仅当 template == `cache-fs` 或 `full-db` 才计算上报（否则 -1），值取树上 type=="cache-fs" 节点的 `maxCacheSize`。
  - 〔代码〕`storage/StorageSummaryImpl#updateFileStoreSummary`：存储摘要的 storageType = 模板名；storageDirectory = binaries 目录清单（无 FS 层时输出 "Filesystem storage is not used"）。
- **无通用 `dual` 模板 / dual provider**：反编译全源对 `dual` 作为 provider 或模板名 **0 命中**（仅 SQL `FROM DUAL`、`individually` 等无关子串）；当前〔官方〕Filestore 文档 TOC 亦无 dual 页。现行 filestore→S3 迁移 = ①手动（停机拷贝 filestore → 换 S3 模板 → 重启）或 ②自动（在 `var/data/artifactory/eventual/` 放 `_add` → filestore、`__pre` → filestore/_pre 符号链接，启动后 eventual 排水：逐文件上传校验成功即删 NFS 源；官方警告会删除原 filestore）。〔官方〕migrate-your-filestore-to-s3 页 +〔代码〕反证。→ 高
- 显式链（`<chain>` 不带 template、直接嵌 provider）时 `template` 字段取值（null / 链首 type / 合成名）：模型类未反编译，**未能定位**（待动态验证）。

**分歧门结论（ADR-0036 模板条款）**：模板速记在 Artifactory 实际行为中的地位 = ①固定展开注册表（非用户可组合原语）＋②自描述标签（系统信息 / 存储摘要可见）＋③一个摘要上报分支条件（cacheSize）。**未定位到「模板 vs 等价显式链」在数据存取路径上的行为分叉证据** → **分歧门：关**——M11 canonical 显式链不损失行为等价性。遗留两个登记项（非阻塞，转 T-306 设计决策）：a) BinFlow 等价自描述字段（如链形态 INFO 行 / 未来 storage info 端点）的取值语义；b) cacheSize 类「按模板名条件上报」是否以链结构判定（BinFlow 无模板层，应按链上是否存在缓存 provider 判定）。

### 1.3 provider 链组合规则

| # | 规则 | 出处 | 置信度 |
|---|---|---|---|
| C1 | 链为嵌套声明：`<chain>` 直接子 provider = 最靠近 Artifactory 的层（读写路径入口），内层 = 下一跳存储 | 〔官方〕全部模板展开结构 | 高 |
| C2 | `cache-fs` 前置（最外层）：官方明示「cache-fs 是最靠近 Artifactory 的 filestore 层」；filestore 已本地时无收益；HA 每节点各自的 cache-fs | 〔官方〕cache-fs 页 | 高 |
| C3 | `eventual`（异步排水）与 `retry` 位于 cache-fs 与云 provider 之间：`cache-fs( eventual( retry( cloud ) ) )` 为 S3 eventual 固定序 | 〔官方〕s3-storage-v3 展开 | 高 |
| C4 | 「dual 主从 / 双主」组合原语不存在（见 §1.2）；双写迁移态由 eventual + `_add` 符号链接机制表达，非链内 provider | 〔官方〕+〔代码〕反证 | 高 |
| C5 | sharding 家族：`sharding` 下挂 `sub-provider`（静态 shard，如 state-aware-s3）；sharding-cluster 另有 `dynamic-provider type="remote"`（按集群节点动态建 remote 连接） | 〔官方〕cluster-s3-storage-v3 / s3-sharding 模板 | 高 |
| C6 | 顶层覆盖块与链内 provider 按 id 匹配，多覆盖块并存（NFS 例同时覆盖 cache-fs 与 file-system 两个 id） | 〔官方〕cache-fs NFS 例 | 高 |
| C7 | **非法组合 / 未知 provider type / 坏 XML 的报错形态与错误码**：展开器未反编译、官方文档不载 → **未能定位**（见文末待验证清单 binarystore-1） | — | 低 |

### 1.4 file-system provider 可配置点（模板展开后）

| 参数 | 默认 | 语义 | 出处 | 置信度 |
|---|---|---|---|---|
| `baseDataDir` | `$JFROG_HOME/var/data/artifactory`（官方页写旧式 `$JFROG_HOME/artifactory/var/data/artifactory`，7.x 实际为前者） | 数据根目录 | 〔官方〕 | 高 |
| `fileStoreDir` | `filestore`（相对 baseDataDir；`/` 开头视为绝对路径） | filestore 根 | 〔官方〕 | 高 |
| `tempDir` | `temp`；7.98.2 起相对 **fileStoreDir**（此前相对 baseDataDir，且绝对路径强制落到 `$BASEDATADIR/filestore/tmp`） | 临时目录 | 〔官方〕版本注记 | 高 |
| `config/@version` | 一手默认模板 `1`；官方示例见 `v1`/`2`/`5` | 未文档化（疑为 provider 配置 schema 版本） | 〔模板〕/〔官方〕示例 | 低 |
| cache-fs 层参数（组合链时） | `maxCacheSize`=5000000000（5GB，不含 `_pre`）、`cacheProviderDir`=`cache`、`multiReadEnabled`=true、`validateChecksum`=true；7.71.1+ 另有 `maxFileSizeLimit`/`skipDuringUpload`；pre 清理三参默认关闭/86400s | LRU 读缓存层 | 〔官方〕cache-fs 参数表 | 高 |

### 1.5 可观测面（链形态的外部印证）

| 面 | 行为 | 出处 | 置信度 |
|---|---|---|---|
| storage info 端点 | 返回**展开后** provider 树 JSON（rootTreeElement，节点 data 含 type/id/参数）；SaaS（AOL）非 dashboard 用户 405 "Artifactory SaaS does not support this feature." | 〔代码〕`rest/resource/system/StorageResource#getStorageInfo`；UI 同树：`ui/.../advanced/storage`（`binary/providers/info`，SaaS 拒绝走 error 文案） | 高 |
| 系统信息 Storage Type | = 模板名原样输出 | 〔代码〕GetSystemInfoService#updateStorageInfo | 高 |
| 存储摘要 storageType | = 模板名；storageDirectory = binaries 目录清单 | 〔代码〕StorageSummaryImpl#updateFileStoreSummary | 高（值）/ 中（对外 JSON 字段名） |
| 指标 | `provider` = 根 provider 的 type；根为 sharding 时附加 mounts/redundancy | 〔代码〕`metrics/providers/features/StorageFeature#addStorageSummaryFeature` | 中 |
| 模板 vs 等价显式链差异 | 该面未定位到分叉（树按展开结构输出）；仅 §1.2 所列 template 字段消费点待验证 | — | 低（见文末待验证清单 binarystore-2） |

### BinFlow 映射

机制已由 **ADR-0036（binstore.yaml）** 终裁（独立文件 + 有序显式链 + migration 三分支 + 三分支并存裁决）；本节为该 ADR 的行为锚点，分歧门结论见 §1.2（关，两项登记转 T-306）。本文件早期版本的 M1 YAML 建议块（blobstore.kind/shard_depth 等）**废止**，以 ADR-0036 与 storage-layout.md 为准。
## 2. artifactory.config.xml（全局配置）

模板关键节点与默认值（一手模板 `config-templates/artifactory.config.xml`，xsd 命名空间 `http://artifactory.jfrog.org/xsd/3.5.4`）：

| XML 节点 | 默认值 | 语义 | BinFlow YAML 映射 | 置信度 |
|---|---|---|---|---|
| `security/hideUnauthorizedResources` | `false` | true 时无权限资源按 404 响应而非 403 | `security.hide_unauthorized: false` | 高 |
| `backups/backup[]` | backup-daily（cron `0 0 2 ? * MON-FRI`，retention 0=增量永久）+ backup-weekly（disabled） | 定时备份 | `backup.jobs: []`（M1 可留空） | 高 |
| `indexer/cronExp` | `0 23 5 * * ?` | Maven 索引 | 不实现（M3+） | 高 |
| `repoLayouts/repoLayout[]` | 25+ 套命名布局（maven-2-default / npm-default / simple-default…） | 路径模板（`[orgPath]/[module]/[baseRev]...`） | `repo_layouts:` 只内置 simple-default（M1 Generic）；其余 M3 按协议补 | 高 |
| `gcConfig/cronExp` | `0 0 /4 * * ?` | GC 周期 | `storage.gc.interval: 4h` | 中（cron 表达式语义存疑，见 storage-layout.md 待验证#3） |
| `cleanupConfig/cronExp` | `0 12 5 * * ?`（每日 5:12） | 空目录/过期清理 | `storage.cleanup.interval: 24h` | 高 |
| `virtualCacheCleanupConfig/cronExp` | `0 12 0 * * ?` | virtual 缓存清理 | M3 | — |
| `folderDownloadConfig` | enabled=false, maxDownloadSizeMb=1024, maxFiles=5000, maxConcurrentRequests=10 | 目录打包下载 | M4 | — |
| `trashcanConfig` | enabled=true, allowPermDeletes=false, retentionPeriodDays=14 | 回收站 | `trashcan: {enabled: true, retention_days: 14}` | 高 |
| `reverseProxies` | direct 模板 | 反代/子域 Docker 路由 | 不实现 | — |

仓库定义：7.x 中**不在** config.xml 里，存于 DB（`configs` 表 / `artifactory.repository.config.json` 导入导出文件，见 `ArtifactoryHome#ARTIFACTORY_REPO_CONFIG_FILE`）。BinFlow 的 repo 配置建议同样存元数据库，YAML 只放启动期初始仓库（bootstrap）。**高**

## 3. mimetypes.xml

| 行为 | 规格 | 置信度 |
|---|---|---|
| 位置 | `etc/mimetypes.xml`，缺失回退内置 | 高 |
| M1 相关条目 | `application/x-checksum` → 扩展名 `sha1, sha256, md5`（**判定 `.sha1` 旁车文件的关键**）；其余按扩展名 → Content-Type | 高 |
| BinFlow 映射 | Go 内置 `mime.TypeByExtension` + 追加 `sha1/sha256/md5 → application/x-checksum`；暴露 `mime_extra` 配置项供覆盖 | 设计建议 |

## 4. artifactory.system.properties（JVM 属性覆盖）

模板默认仅 5 行（derby 调优 + repo 全局禁用开关）。关键点：所有 `artifactory.*` 键即 `ConstantValues` 枚举（`o.a.a.common.ConstantValues`，数千项）。M1 相关摘录：

| 属性 | 默认 | 语义 | BinFlow 对应 | 置信度 |
|---|---|---|---|---|
| `upload.failOnChecksumValidationError` | false | 上传 checksum 校验失败是否硬失败 | `storage.upload.fail_on_checksum_error: false` | 中 |
| `file.system.max.upload.size.bytes` | 10737418240（10GiB） | 单文件上限 | `storage.max_upload_bytes` | 中 |
| `send.overwrites.to.trashcan` | true | 覆盖上传时旧版本进回收站 | `trashcan.capture_overwrites: true` | 中 |
| `multipart.upload.*` | enabled=false | MPU；M2 Docker 大层可用 | M2 | — |
| `binary.store.full.binary.provider.chain.ping` | true | 存储链健康探测 | 忽略 | — |

## 5. 密钥与凭据文件（etc/security/）

| 文件 | 用途 | BinFlow | 置信度 |
|---|---|---|---|
| `artifactory.key` | 加密主密钥 | `security.master_key`（首次启动自动生成 32B hex） | 高 |
| `access.creds`（etc/security/access/keys/） | Access admin token | BinFlow 无独立 Access，token 自管 | — |

## 6. 环境变量优先级（12-factor 要求）

Artifactory 7.x 的真实顺序：`system.yaml`（Helm/docker 场景）→ `artifactory.system.properties` → JVM `-D` → 内置默认（`ConstantValues`）。BinFlow 按产品约束：**YAML → 环境变量（`BINFLOW_<SECTION>_<KEY>`）→ 默认**。置信度：Artifactory 顺序 `中`（组合逻辑分散在 `ArtifactorySystemProperties`/system yaml loader，未完整走读）；BinFlow 顺序为产品决策。

## 7. 与官方文档的差异 / 补充

- 官方 XSD 文档列出全部节点但**不给默认值**；本表默认值来自一手 config 模板（`config-templates/`），比文档更可信。
- `ConstantValues` 的数千个 `artifactory.*` 微调项从未被官方完整文档化；本规格只挑了 M1 行为相关的 3 项，其余不映射（BinFlow 不承诺兼容该层）。
- repo 定义在 7.x 存 DB 而非 config.xml，官方文档表述分散（Configuration Descriptors 页），此处给出代码级确认（`ARTIFACTORY_REPO_CONFIG_FILE` 常量 + `configs` 表）。

## 待验证清单

1. `repoLayouts` 中 `simple-default`（`[orgPath]/[module]/[module]-[baseRev].[ext]`）是否真的对 Generic 上传路径零约束——Generic 仓库实际不强制 layout（仅用于 UI/集成展示），需动态验证上传任意路径是否成功（推断会成功，Generic 的 includesPattern 默认 `**/*`）。
2. `system.yaml` 与 `artifactory.system.properties` 在同键时的确切优先级（未完整走读 loader）。
3. binarystore-1（T-303）：非法 provider 组合 / 未知 type / 坏 XML 的启动报错形态与错误码——展开器（`org.jfrog.storage.binstore.manager.*`）不在反编译范围，官方文档不载；需对真实实例做动态验证（坏 type / cache-fs 不前置 / 缺 chain 等）。
4. binarystore-2（T-303）：显式链（`<chain>` 无 template 属性）时 `BinaryProvidersInfo.template` 的取值（null / 链首 type / 合成名）——决定 §1.2 所列「模板名消费点」（Storage Type、cacheSize 条件上报）在显式链下的表现；动态验证法：同形状链分别用模板与显式写法启动，比对系统信息 Storage Type 与存储摘要。
