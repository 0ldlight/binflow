# 配置格式行为规格（T-515 全量版：$ARTIFACTORY_HOME/etc 配置文件 + 加载时序 + 导入导出语义）

> 源版本：7.161.24 全量反编译树 `/Users/lzw/workspace/artifactory-decompiled`（树内模块版本戳主要为 7.161.14，XSD 版本链已含 7.162.0 命名空间——树为多构建产物混装，行为以代码为准）；§1 binarystore 节继承自 T-303（证据出自 reverse-src 7.161.16 反编译）。
> 官方参照：JFrog「System YAML Configuration File / Using System YAMLs / Artifactory System YAML」「Artifactory Configuration Descriptors」「Filestore Configuration」。
> 置信度：`高` = 反编译直证（+官方文档双证时注明）；`中` = 单源（反编译推断或仅官方文档）；`低` = 旁证/未能定位，待动态验证。
> 证据锚缩写：〔码·模块〕= 反编译树内 `backend/<模块>/...` 路径；〔官〕= JFrog 官方文档。

## 目录

1. 取证边界（先读）
2. etc/ 配置文件总清单与启动加载时序
3. system.yaml（SysConfig 层）：键读取、优先级、环境变量
4. 数据库配置（旧 db.properties 在 7.x 的去向）
5. HA 节点配置（ha-node.properties 在 7.x 的去向）
6. artifactory.system.properties 与代码内默认（ConstantValues）
7. binarystore.xml（T-303 复核版，继承）
8. artifactory.config.xml 家族：bootstrap / import / latest 与启动决策流程
9. config.xml 的版本转换、校验与保存语义（REST 导入 = replace 还是 merge）
10. 仓库配置导入 artifactory.repository.config.import.json
11. onboarding YAML（artifactory.config.import.yml）
12. security 相关文件（etc/security/ 与 access.security.bootstrap.yml）
13. etc → DB 共享配置同步机制（文件监听、冲突解决、HA 广播）
14. 生效配置对外暴露面（GET/POST/PATCH /api/system/configuration 等）
15. 与官方文档的差异/补充
16. 待验证清单

## 1. 取证边界（先读）

| 边界 | 说明 |
|---|---|
| SysConfig 内核未反编译 | `com.jfrog.sysconf.SysConfig / SysLayout`（system.yaml 解析、环境变量 JF_* 映射、legacy 文件回退）位于未反编译 jar；可见的是**外围消费证据**（各处 `sysConfig.get("<dotted.key>")` 调用）。凡涉及其内部合并顺序/插值行为，本规格仅给官方文档口径并标 `中`/`低`，不编造 |
| BinaryProvider 展开器未反编译 | 见 §7 既有边界声明（T-303） |
| 反编译代码怪癖 | 个别方法（如 `ArtifactoryHome#getBootstrapConfigXml`）反编译丢失了提前 `return`，语义按「逐级回退」还原，与官方描述一致；此类点标 `高` 但注明「反编译控制流还原」 |

## 2. etc/ 配置文件总清单与启动加载时序

### 2.1 文件清单（按代码内出现的文件名常量归纳）

路径基准：7.x 统一布局 `$JFROG_HOME/var/etc/artifactory/`（etc 目录 = SysLayout 的 service etc；〔官〕File System Layout 双证）。

| 文件/目录 | 角色 | 缺省行为 | HA 共享 | 加密 | 证据 | 置信度 |
|---|---|---|---|---|---|---|
| `artifactory.config.xml` | 旧版全局配置（7.x 仍作为启动回退源之一，读后改名） | 存在则启动时改名为 `artifactory.config.bootstrap.xml` | 否（同步黑名单） | 否 | 〔码·artifactory-config〕`common/home/ArtifactoryHome`（`renameInitialConfigFileIfExists`）；黑名单见 §13 | 高 |
| `artifactory.config.bootstrap.xml` | 每次启动实际读取的「本机文件配置」源 | 缺失时回退 etc/artifactory.config.xml，再回退内置 `/META-INF/default/artifactory.config.xml` | 否 | 否 | 同上（`getBootstrapConfigXml`，反编译控制流还原为逐级回退） | 高 |
| `artifactory.config.import.xml` | 一次性导入源：启动时读取并**改名**为 bootstrap | 存在且非空白才生效；读后 `switchFiles` 改名（一次性消费） | 否 | 否 | 同上（`getImportConfigXml`） | 高 |
| `new_artifactory.config.bootstrap.xml` | 自动转换旧配置时的中转文件名 | 仅在 bootstrap 已存在且需写新版时短暂使用 | 否 | 否 | 同上（`getArtifactoryConfigNewBootstrapFile`）+ 〔码·artifactory-core〕`config/CentralConfigServiceImpl#saveNewBootstrapConfigFile` | 高 |
| `artifactory.config.latest.xml` | 每次成功保存后滚动快照（带保留份数） | 不存在则由 DB 配置回填一次 | 否 | 否 | 〔码·artifactory-core〕`CentralConfigServiceImpl#storeLatestConfigToFile`（滚动保留 `fileRollerMaxFilesToRetain` 份） | 高 |
| `artifactory.repository.config.import.json` | 仓库定义一次性导入源（读后改名 bootstrap） | 见 §10 | 否 | 否 | 〔码·artifactory-config〕`ArtifactoryHome#getArtifactoryRepoConfig*` | 高 |
| `artifactory.repository.config.bootstrap.json` | 上次导入的留存 | — | 否 | 否 | 同上 | 高 |
| `artifactory.repository.config.latest.json` | 仓库配置滚动快照 | — | **是** | 否 | 〔码·artifactory-config〕`common/config/adapter/ArtifactoryConfigurationAdapter#initSharedConfigs` | 高 |
| `artifactory.config.import.yml` | onboarding YAML（首次建仓/baseUrl/代理/许可证） | 见 §11 | 否 | 否 | 〔码·artifactory-config〕`ArtifactoryHome#getArtifactoryBootstrapYamlImportFile` | 高 |
| `binarystore.xml` | blob 存储链 | 缺省播种内置默认；HA 共享+加密 | **是**（mandatory） | **是** | 见 §7 | 高 |
| `mimetypes.xml` | MIME 映射 | 缺省播种内置 | **是**（mandatory） | 否 | 〔码·artifactory-config〕`ArtifactoryConfigurationAdapter#initDefaultConfigs/initSharedConfigs` | 高 |
| `artifactory.system.properties` | JVM 属性覆盖文件 | 缺省播种内置 | **是**（mandatory） | 否 | 同上 | 高 |
| `logback.xml` | 日志配置 | 缺省播种内置 | 否（仅默认播种清单） | 否 | 同上（`initDefaultConfigs` 含 logback；共享清单不含） | 高 |
| `ha-node.properties` | 旧版 HA 节点文件 | 7.x 由 system.yaml `artifactory.node.*` 取代；文件本体读取在未反编译 sysconf | 否 | 否 | §5 | 中 |
| `db.properties` | 旧版数据库连接文件 | 7.x 由 system.yaml `shared.database`/`artifactory.database` 取代；反编译树内**无任何读取代码** | — | — | §4（全树 grep 0 命中消费路径） | 高（对 7.x 主流程） |
| `storage.properties` | — | 反编译树内不存在该文件名（全树 grep 0 命中）；属更早期版本概念，7.161 无此文件 | — | — | §4 检索记录 | 高（负证） |
| `artifactory.lic` / `artifactory.cluster.lic` | 许可证 | HA 模式固定读 cluster.lic | **是** | 否 | 〔码·artifactory-config〕`ArtifactoryHome#getLicenseFile` | 高 |
| `gcp.credentials.json` | GCP filestore 凭据 | — | **是** | **是** | §13 共享清单 | 高 |
| `security/` 目录（含 `artifactory.key`、SAML 密钥对等） | 密钥与凭据 | 目录权限强制收紧；security/ 下文件同步时自动加密、落盘 600 | **是**（mandatory 文件夹） | **是** | §12、§13 | 高 |
| `plugins/`、`ui/`（logo） | 插件与品牌 | — | **是** | 否 | §13 | 高 |
| `../system.yaml`（`$JFROG_HOME/var/etc/system.yaml`） | 平台级配置 | 缺失时全部走内置默认 | — | — | 〔码·artifactory-support-core〕`ArtifactoryConfigFileCollector`（按 ProductEtc 拼路径） | 高 |

### 2.2 启动加载时序（当实例启动，则按以下顺序发生）

1. **Home 解析**：当 servlet 容器初始化，则以 init 参数 `product.home`（= JF_PRODUCT_HOME/JFROG_HOME）构造 Home：解析 SysConfig/SysLayout → 强制创建 var 目录树 → 创建 etc、etc/security（并立即收紧权限）、etc/security/access、etc/plugins、etc/ui、data、data/git、data/import、data/tmp/{work,work/fullSync,artifactory-uploads}、backup、log 目录；清理 tmp 下 1 天前的陈旧文件。任一目录不可写 → 启动失败（IllegalArgumentException「Directory '...' is not writable!」）。〔码·artifactory-config〕`ArtifactoryHome#<init>/create` | 高
2. **HA 节点属性**：当 Home 创建完成，则从 SysConfig 读 `artifactory.node.*` 装配 HA 节点属性（§5）；enabled=true 时日志「Artifactory is running in clustered mode.」。data/backup 目录支持节点级重定向（绝对路径生效；相对路径告警并忽略）。〔码〕`ArtifactoryHome#create/initHaNodeProperties`、`getHAAwareSubDir` | 高
3. **数据库属性**：当 Home 初始化 DB 属性，则按 §4 规则解析；失败抛 IllegalStateException「Failed to init db properties: ...」。**HA + Derby → 直接拒绝启动**：「Cannot use Derby as the database type in HA mode, please check the system.yaml file.」。〔码·artifactory-lifecycle〕`ArtifactoryHomeConfigListener#initArtifactoryHome`（顺序：先 DB 后 system properties）+ 〔码·artifactory-config〕`ArtifactoryConfigurationAdapter#blockIfHAWithDerby` | 高
4. **artifactory.system.properties**：当读到该文件，则按 §6 算法与 JVM -D 合并；若最终 `artifactory.product.version` 缺失 → 抛「The artifactory.product.version property is missing...」（由 app 目录 `artifactory.product.version.properties` 注入）。〔码〕`ArtifactoryHome#initArtifactorySystemProperties/assertArtifactoryProductProperties` | 高
5. **Spring profile 推导**：当 system properties 装配完成，则按 `package.handler.*` 与权限缓存类型推导并 `System.setProperty("spring.profiles.active", ...)`。〔码〕`ArtifactoryHome#setActiveSpringProfiles` | 高
6. **文件同步器启动**：当配置管理器初始化，则对 etc 目录注册 JDK 文件监听；物化四个默认文件（mimetypes/system.properties/binarystore/logback，仅缺失时从 `/META-INF/default/` 拷贝）；注册共享配置到 DB `configs` 表（§13）。〔码·jfrog-config〕`ConfigurationManagerImpl#startSync` | 高
7. **中央配置装配**：当 Artifactory 上下文启动，则按 §8 决策流程选出本次生效的 config XML → 版本转换（§9.1）→ onboarding YAML 合并（§11，如有）→ 旧 etc/artifactory.config.xml 改名 → 与 DB 旧描述符合并/替换保存。〔码·artifactory-core〕`CentralConfigServiceImpl#getCurrentConfig` | 高
8. **仓库引导**：当存在 `artifactory.repository.config.import.json`，则 §10 流程（全量替换仓库定义）。〔码·artifactory-core〕`repo/service/RepositoryConfigBootstrapServiceImpl#init` | 高
9. **许可证/Access 安全引导**：onboarding YAML 携带许可证时在上下文创建后补装；`access.security.bootstrap.yml` 在 Access 侧就绪事件时消费并删除（§12.2）。〔码〕`CentralConfigServiceImpl#onContextCreated`、〔码·access-server-core〕`AccessSecurityBootstrap` | 高

### 2.3 目录/文件命名规则摘要

- 当 HA 启用且 `artifactory.node.haDataDir` 为绝对路径，则 data 目录改用该路径；backup 同理（`artifactory.node.haDataBackup`）；其余子目录（etc/log 等）不可重定向。〔码〕`ArtifactoryHome#getHAAwareSubDir`（仅 data/backup 白名单，其余抛 IllegalArgumentException）| 高
- Derby 数据目录默认 `<dataDir>/derby`，可用 `artifactory.database.dbHome` 覆盖；Derby 日志固定写 `<logDir>/derby.log`。〔码〕`ArtifactoryHome#getDefaultDbHome/getDerbyLogFilePath` + §4 | 高

## 3. system.yaml（SysConfig 层）

### 3.1 可见读取口径（反编译直证）

全部平台级配置经同一个 SysConfig 门面读取，键为点分路径。已确认的消费键：

| 键 | 消费方 | 默认 | 证据 | 置信度 |
|---|---|---|---|---|
| `artifactory.database.*`（§4 全表） | DB 属性装配 | 见 §4 | 〔码·jfrog-db-infra〕`storage/DbProperties` | 高 |
| `artifactory.node.id/haEnabled/primary/ip/membershipPort/haDataDir/haDataBackup/crossZoneOrder` | HA 节点属性 | node.id 默认 `Artifactory`，其余无默认（不设即非 HA） | 〔码·artifactory-config〕`common/ha/HaNodeProperties#load` | 高 |
| `shared.system.technicalServerName` | 租户标识 | 无 | 〔码〕`ArtifactoryHome#<init>` | 高 |
| `artifactory.security.import.allowedPaths` / `...export...` / `...backup...` | 导入/导出/备份路径白名单（逗号分隔） | 空字符串=不限制 | 〔码·artifactory-api〕`config/PathValidatorUtil#resolveAllowedPaths` | 高 |
| `shared.database.*` | DB 属性（shared 段被展开进各服务前缀） | — | 〔官〕Using System YAMLs 层级 + 〔码〕键拼接 `artifactory.` + `database.` | 中（shared 展开逻辑在 sysconf 内核） |

### 3.2 优先级顺序

- **官方口径**〔官〕Using System YAMLs「Configuration values are applied according to the following hierarchy」：**环境变量 > system.yaml 服务段（artifactory.*） > system.yaml shared 段 > 应用内置默认**。此条补充官方规范的反编译印证：内置默认一层的存在与形态可由 §4/§6 的代码默认值直证（如 DB type 默认 derby、ConstantValues 枚举默认）。置信度：中（层级本身仅官方文档，sysconf 内核未反编译；默认值来源代码直证为高）。
- **JVM 层（与 system.yaml 无关的另一轴）**：对 `artifactory.*` 微调键，顺序为 **JVM -D（System properties）> etc/artifactory.system.properties 文件 > ConstantValues 代码内默认**——〔码·artifactory-config〕`common/property/ArtifactorySystemProperties#loadArtifactorySystemProperties`：先 `Properties.load(file)`，后叠加 `System.getProperties()` 中 `artifactory.` 前缀键（后写覆盖=JVM 胜）。高。
- 两轴交汇点（如同一逻辑项既在 system.yaml 又是 ConstantValues）按消费方各自实现，无全局统一顺序——**未能定位全局仲裁器**，待动态验证（见 §16-1）。

### 3.3 环境变量与 ${...} 插值

- 环境变量映射（`JF_SHARED_*`/`JF_ARTIFACTORY_*` → 点分键）在 sysconf 内核执行，反编译树内**无实现代码**；官方文档确认环境变量优先于 system.yaml（§3.2）。置信度中（官方单源）。
- `${VAR}` 占位符插值：反编译树内**全量检索未见**任何 etc 配置文件（system.properties/binarystore/config.xml）加载路径上的占位符解析；官方 system.yaml 文档亦无插值条款 → **判定 7.161 不支持配置文件内 ${...} 插值**（负证 + 文档缺失）。置信度：中（sysconf 内核不可见，保留动态验证余地）。
- 反例提示：代码中出现的 `${...}` 字面量仅用于错误消息模板与 Maven POM 语义（PomTargetPathValidator），与配置加载无关。〔码〕`metrics/...`、`artifactory-common/.../PomTargetPathValidator` 检索记录 | 高

## 4. 数据库配置（db.properties 在 7.x 的去向）

### 4.1 总则

当实例启动，则数据库连接参数**只**从 SysConfig 的 `artifactory.database.*` 键解析（含 system.yaml 与环境变量两条来源；`shared.database.*` 官方文档确认可共享注入）；**不存在**对 etc 下 `db.properties` 文件的任何读取代码（全树检索仅命中文档字符串与 Access 侧同名概念的 DbConfigFactory，其同样走 SysConfig）。〔码·jfrog-db-infra〕`storage/DbProperties#<init>`；检索记录见工作日志 | 高

### 4.2 键与默认值表（反编译直证）

| 键（`artifactory.database.` 前缀） | 默认 | 语义 | 置信度 |
|---|---|---|---|
| `type` | `derby` | DB 类型枚举解析 | 高 |
| `url` | `jdbc:derby:{db.home};create=true` | JDBC URL；`{db.home}` 占位符在 derby 时替换为 dbHome 实际值 | 高 |
| `driver` | `org.apache.derby.jdbc.EmbeddedDriver` | 驱动类 | 高 |
| `username` / `password` | 无（空） | 凭据；password 支持加密串（先尝试主密钥解密，失败再尝试实例密钥解密，原文返回=明文） | 高 |
| `dbHome` | `<dataDir>/derby` | derby 数据目录 | 高 |
| `poolType` | `hikari`（另有 `tomcat-jdbc`、`hikari-rds-iam`） | 连接池类型 | 高 |
| `maxOpenConnections` / `maxIdleConnections` / `minIdle` | 100 / 10 / 1 | 池参数（get* 回退到 ArtifactoryDbProperties 默认而非枚举默认） | 高 |
| `allowNonPostgresql` | `false`（另有短键 `AllowNonPostgresql` 优先） | 非 PG 告警开关 | 高 |
| `secretsManagerAlias` | 无 | 设置后启动期从 AWS Secrets Manager 拉 JSON 覆盖 username/password；拉取失败 → IllegalStateException「Unable to retrieve jdbc secret from SecretsManager」 | 高 |
| `metrics.enabled` / `metrics.maxQueriesToTrack` | `false` / `1000` | 查询指标 | 高 |
| `lockingdb.{type,url,driver,username,password}` | 无 | 独立锁库；**三选一设置则三者全必填**，缺一抛「Mandatory database parameter '<key>' doesn't exist」 | 高 |
| `driverProperties` | 无 | `k=v;k2=v2` 分号串，逐项 trim 后作为 JDBC 驱动属性 | 高 |

### 4.3 校验时序与失败形态

- 当任一必填项（type/url/driver）解析后为空白，则抛 `IllegalStateException("Mandatory database parameter '<key>' doesn't exist")`，包一层「Failed to init db properties: ...」——启动中止。〔码〕`DbProperties#assertMandatoryProperties` | 高
- 当值含首尾空白，则统一 trim 后使用。〔码〕`DbProperties#trimValues` | 高
- 当 HA 且 type=derby，则拒绝启动（§2.2-3）。| 高

## 5. HA 节点配置（ha-node.properties 在 7.x 的去向）

- 当 Home 初始化，则 HA 节点属性从 SysConfig 读 `artifactory.node.*`，**不读** etc/ha-node.properties（文件读取在 sysconf 内核的 legacy 回退中是否存在**未能定位**；官方文档口径为 system.yaml 取代该文件）。〔码·artifactory-config〕`HaNodeProperties#load`；〔官〕 | 键表：高 / 「7.x 是否仍兼容读取旧文件」：低（待验证 §16-2）
- 键表与语义：`node.id`（默认 `Artifactory`，对外即 serverId）、`node.haEnabled`（布尔；true=集群模式）、`node.primary`、`node.ip`、`node.membershipPort`（整数，空白视为未设）、`node.haDataDir`/`node.haDataBackup`（绝对路径重定向 data/backup）、`node.crossZoneOrder`。〔码〕`HaNodeProperties` | 高
- 当 `haEnabled` 未设置或 false，则 `isHaConfigured()`=false：许可证固定读 `artifactory.lic`（非 cluster.lic）、hostId 用 `artifactory.hostId` 属性或进程内 VMID、serverId 回退常量 `Artifactory`。〔码〕`ArtifactoryHome#isHaConfigured/getLicenseFile/getHostId/getServerId` | 高

## 6. artifactory.system.properties 与代码内默认

### 6.1 文件格式与加载算法（当文件存在，则按此执行）

1. 按 `java.util.Properties` 文本格式加载（`#`/`!` 注释、`=`/`:` 分隔、反斜杠续行、ISO-8859-1 读入）。〔码〕`ArtifactorySystemProperties#loadArtifactorySystemProperties` | 高
2. 叠加 JVM -D：凡 `System.getProperties()` 中 `artifactory.` 前缀键，覆盖文件同名键（**JVM 优先于文件**）。| 高
3. **非 `artifactory.` 前缀键提升为 JVM 系统属性**（`System.setProperty` 后从合并集移除）→ 该文件可用来设置任意第三方库 -D（如 derby 调优）。| 高
4. `artifactory.repo.key.subst.<旧键>=<新键>` 前缀键抽出为仓库键替换表并从属性集移除。| 高
5. 废弃键处理：命中废弃表则 WARN 并自动改名（见 6.2）；映射为 null（NullPropertyMapper）= 仅告警弃用。| 高
6. `artifactory.ui.chroot` 校验：非空且目录不存在 → ERROR「Selected chroot '...' does not exist. Ignoring property value!」并丢弃该键。| 高
7. 仅当键名与代码内枚举（约 1800 项）精确匹配才进入「微调属性」表；其余键保留为自由属性。版本常量（version/revision/buildNumber/timestamp 等 9 项）随后由运行版本强制注入（文件不可覆盖）。| 高
8. 服务/产品版本任一可得则注入；最终缺 `artifactory.product.version` → 启动失败（§2.2-4）。| 高

### 6.2 废弃键自动改名表（代码直证，全量）

| 旧键 | 行为 |
|---|---|
| `artifactory.authenticationCacheIdleTimeSecs` | 改名 `artifactory.authentication.cache.idleTimeSecs` |
| `artifactory.maven.suppressPomConsistencyChecks` | 弃用（仅告警） |
| `artifactory.metadataCacheIdleTimeSecs` | 弃用 |
| `artifactory.logs.refreshrate.secs` | 改名 `artifactory.logs.viewRefreshRateSecs` |
| `artifactory.spring.configPath` | 改名 `artifactory.spring.configDir` |
| `artifactory.lockTimeoutSecs` | 改名 `artifactory.locks.timeoutSecs` |
| `artifactory.xmlAdditionalMimeTypeExtensions` | 弃用 |
| `repo.cleanup.intervalHours` | 弃用 |
| `bintray.system.user` / `bintray.system.api.key` | 迁管理页（仅告警，值丢弃） |
| `artifactory.security.useBase64` | 弃用 |
| `artifactory.security.authentication.encryptedPassword.surroundChars` | 弃用 |

〔码〕`ArtifactorySystemProperties#DEPRECATED` | 高

### 6.3 代码内默认机制

- 当微调键未在文件/-D 出现，则取代码内枚举默认（默认值硬编码于枚举构造参数，字符串化存储）。例：`search.maxResults`=500、`locks.timeoutSecs`=120、`system.import.enabled`=true、`config.xml.validation.enabled`=true、`central.config.save.number.of.retries`=5/backoff.max.delay=8000ms/multiplier=2、`configuration.manager.retry.amount`=3/quiet.period=20s/blocking.time=60s。〔码·artifactory-config〕`common/ConstantValues`（~1884 行枚举） | 高
- 类型解析行为：布尔=Boolean.parseBoolean（非 "true" 一律 false，不报错）；长整=Long.parseLong(trim)（**非法数字抛 NumberFormatException 未捕获**——坏值会让调用点失败）；列表=逗号分割+trim+去空。〔码〕`ArtifactorySystemProperties#getBooleanProperty/getLongProperty/parseList` | 高
- 此文件**无热更新路径**（加载仅发生在 Home 初始化；改文件需重启——HA 下文件会被 §13 同步到其他节点，但内存值不变）。| 中（未见 reload 监听的负证）

## 7. binarystore.xml（T-303 复核版，继承）

> 本节全文继承 2026-08-26 T-303 复核稿（证据出自 reverse-src/artifactory 7.161.16 反编译 + 官方 Filestore 文档），要点重述，详表见 git 历史或 T-306：

- 位置固定 `etc/binarystore.xml`，无备选名；缺省由 `/META-INF/default/binarystore.xml` 播种（默认 `<config version="1"><chain template="file-system"/></config>`）；HA 共享（mandatory）且敏感字段落盘自动加密；仅启动装配，无热更新。高
- 模板=固定展开注册表（file-system / cache-fs / full-db(-direct) / s3-storage-v3(-direct) / cluster-s3-storage-v3 / s3-sharding）；模板名展开后保留并出现在系统信息 Storage Type；无 dual 模板/provider（全源 0 命中）。高
- provider 链嵌套组合规则、顶层按 id 匹配注入参数、cache-fs 参数默认（maxCacheSize=5GB 等）见原稿；非法组合报错形态仍未定位（→§16-6）。高/低（分项）
- 存储信息端点返回展开后 provider 树；SaaS 非 dashboard 用户 405。高

## 8. artifactory.config.xml 家族与启动决策流程

### 8.1 启动时「本次生效配置」的来源决策（当启动，则按优先级取第一个可用源）

1. **import 文件**：`etc/artifactory.config.import.xml` 存在且非空白 → 读取全文，随后立即改名为 `artifactory.config.bootstrap.xml`（一次性；下次重启不再生效）。〔码〕`ArtifactoryHome#getImportConfigXml` | 高
2. **DB 配置**：configs 表键 `artifactory.config.xml`（非空则用，且不触发「需要保存描述符」标记；首次还会把它回填成 `artifactory.config.latest.xml`）。回滚安装（ROLLBACK）且正在转换态时改读目标版本的历史配置。〔码·artifactory-core〕`CentralConfigServiceImpl#loadConfigFromStorage` | 高
3. **bootstrap 文件**：`etc/artifactory.config.bootstrap.xml`。〔码〕`getBootstrapConfigXml` | 高
4. **旧式文件**：`etc/artifactory.config.xml`（读取后整个启动流程末尾被改名为 bootstrap——老用户首次升级场景）。| 高
5. **内置默认**：classpath `/META-INF/default/artifactory.config.xml`（读不到才 IllegalStateException）。| 高

注意：import 文件**优先于 DB**——即放一个 import 文件即可覆盖 DB 中的现存全局配置（updateDescriptor=true 走强制保存）。〔码〕`getCurrentConfig` 分支（`isBootstrapInitOrUpgradeFlow` 置位逻辑） | 高

### 8.2 与 DB 旧配置的合并语义（启动路径）

- 当本次来源是 DB（无 import/bootstrap），则 old=new，仅当需要转换（版本升级/回滚）才重存。| 高
- 当本次来源是 import/bootstrap 文件，则新描述符覆盖保存（老描述符作为 diff 事件旧值传给变更处理器）。| 高

### 8.3 onboarding YAML 叠加

当 `etc/artifactory.config.import.yml` 存在，则在上述 XML 描述符之上再执行 §11 合并（仅首次空实例生效）。

## 9. config.xml 的版本转换、校验与保存语义

### 9.1 版本探测与转换链

- 版本判定 = 在 XML 文本中查找**带引号的** XSD 命名空间串（如 `"http://artifactory.jfrog.org/xsd/3.5.4"`）；当前（7.161.x 树）最新命名空间 = **`http://artifactory.jfrog.org/xsd/3.5.11`**（对应服务版本 7.162.0；树内 3.5.0–3.5.11 为 7.137–7.162 区间命名空间）。〔码·artifactory-config〕`version/ArtifactoryConfigVersion`（枚举尾部 + `getConfigVersion`） | 高
- 当配置不含最新命名空间串：先按字符串探测旧版本；探测不到任何已知版本 → RuntimeException「…auto discovery…did not find any valid version…Please fix this file manually!」；探测到最新版本但命名空间串不匹配（文件声称新版但串不对）→ RuntimeException「…up to date but does not have the right schema…」。| 高
- 当版本低于最新：按枚举序应用该版本起的所有转换器（首个恒为命名空间改写器）。**启动路径宽松转换；REST API 导入路径严格转换**（convertStrict——转换即校验失败直接报错）。| 高
- 转 API（POST /api/system/configuration 等）的导入一律执行转换（`isApiRequest=true`），即使已是最新版本。〔码〕`descriptor/reader/CentralConfigReader#readAndConvert` | 高

### 9.2 校验开关与失败形态

| 场景 | 行为 | 证据 | 置信度 |
|---|---|---|---|
| REST/导入读入 | XSD 校验由 `artifactory.config.xml.validation.enabled`（默认 **true**）控制 | 〔码〕`ConstantValues#configXmlValidationEnabled` + `CentralConfigServiceImpl#setConfigXml` | 高 |
| 描述符保存前整体校验 | 由 `artifactory.config.descriptor.xsd.validation.enabled`（默认 true）控制；违规 → ConfigurationException「Found config violations: <rootCause>」 | 〔码〕`validateDescriptor` | 高 |
| 保存含重复 proxy key | RuntimeException「Duplicate proxy key in configuration: <key>.」 | 〔码〕`checkUniqueProxies` | 高 |
| 新格式配置内嵌旧 repo 描述符（localRepositories/remoteRepositories 等节点存在） | IllegalArgumentException「Cannot import configuration: config descriptor contains old repositories configuration, please use the new repositories export format instead.」 | 〔码〕`validateRepoDescriptorsImport` | 高 |
| 系统全量导入时 config.xml 带旧 repo 节点且同目录存在 artifactory.repository.config.json | IllegalArgumentException「Cannot import configuration: 'artifactory.config.xml' contains old repositories configuration, please use the new repositories export format instead.」 | 〔码〕`validateRepositoriesImport` | 高 |
| 保存遇 ConfigurationException 包装 | REST 层转 400 BadRequest「Could not merge and save new descriptor [...]」 | 〔码〕`saveAndReloadContextWithRetry` catch 分支 | 高 |

### 9.3 replace 还是 merge（REST 语义，本票核心问题）

- **POST /api/system/configuration（全量 XML）**：当提交的描述符 revision ≤ 0、或等于当前 revision、或当前无配置、或 forceReplace=true（POST 与系统导入路径均为 force）→ **整描述符替换**（缺省节点回落到转换器补默认，非「保留 DB 旧值」）。当提交带旧 revision 且缓存中存有该基线 → **三方 diff 合并**（以提交时基线与提交体的 diff 应用到当前最新；基线不在缓存 → WARN「Configuration update wasn't against the latest and the original version couldn't be found」后**回退为替换**）。〔码〕`mergeDescriptors`（`forceReplace \|\| current==null \|\| new.revision<=0 \|\| 同 revision` → 替换） | 高
- **PATCH /api/system/configuration（YAML 增量）**：当提交 YAML 体，则先摘出平台段与 Access 段（交由对应服务），剩余映射展平为点分路径变更集；**值为 null 或 `~` 的键 = 删除该键**；列表按下标 `{0},{1}` 对位替换；最后走 diff 合并保存。响应体 `<N> changes to config merged successfully`（N=变更数-仓库删除失败数+平台段数+Access 段数）。IllegalArgumentException/IllegalState/敏感数据异常 → 400。〔码·artifactory-rest〕`system/ConfigResource#patchConfig` + 〔码·jfrog-annotation-processing〕`config/diff/MapToDiffConverter` | 高
- **PUT baseUrl**（text/plain）：校验 http/https → 保存描述符并重载；SaaS 上受 `artifactory.allow.aol.base.url.change`（默认 false）限制，非 dashboard SaaS 调用 → BadRequest「Changing Custom URL Base is not supported for SaaS instances」。| 高
- **重复键**：XML 层由 JAXB/XSD 校验裁定（重复元素 → 校验失败 400）；Properties 层（system.properties）同键后值覆盖前值（Properties 语义）。〔码〕JaxbHelper 调用点 + Properties.load | 高（机制）/ 中（XML 重复键具体报错文案未取证）

### 9.4 保存、并发与传播

- 当保存描述符，则 revision 单调 +1（旧 revision 存入近期基线缓存，供三方合并）；DB 更新走「最后修改时间匹配」乐观并发，不匹配 → 重试（默认 5 次，指数退避，最大 8s，倍数 2）。〔码〕`bumpRevision/saveDescriptorInternal/saveAndReloadContextWithRetry` | 高
- 保存成功后：异步滚动写 `artifactory.config.latest.xml`；非导入进行态时向 HA 集群广播配置重载；记录 Access 审计「configuration changed」。| 高
- **热生效 vs 重启**（按消费面）：
  - config.xml 描述符（general/邮件/代理/备份计划/repo 布局等）→ **保存即热重载**（变更事件分发给各 Reloadable 处理器；每个处理器声明 initAfter/listenOn，按 SYSTEM_STARTUP 与运行期两策略分发）。〔码〕`notifyReposOnDeleteConfigChange/configChangeEventHandlerManager` + `@Reloadable` 注解体系 | 高
  - binarystore.xml / DB 连接 / HA 节点属性 / artifactory.system.properties / system.yaml → **仅启动读取，改后需重启**（无 reload 路径；§6.3、§7 与 DbProperties 构造时序直证）。| 高
  - logback.xml → 经日志 REST 热加载（不在本票范围展开）。| 中

## 10. 仓库配置导入 artifactory.repository.config.import.json

- 当启动时该文件存在，则先做冲突预检：若同时存在 `artifactory.repository.config.import.json` 与（config.import.xml 或含仓库节点的 config.bootstrap.xml）→ **启动失败** IllegalStateException「Cannot have both 'artifactory.repository.config.import.json' and '<另一个文件>' which contains repositories in 'etc/artifactory' dir」。〔码·artifactory-core〕`util/RepoConfigBootstrapUtils#verifyConflictingRepoBootstrap` | 高
- 当导入执行：读取 JSON（聚合格式，含 local/remote/virtual 数组）→ **先清空 DB 中全部现存仓库配置（deleteAll，日志「N existing repo configs were cleaned up」）→ 再批量插入**——**replace 语义，非合并**；随后热重载仓库配置并在进入 RUNNING 态后以系统身份向 HA 集群传播。〔码〕`repo/service/RepositoryConfigBootstrapServiceImpl#init/deleteExistingRepoConfigs` | 高
- 文件读后改名为 `artifactory.repository.config.bootstrap.json`（一次性消费）。〔码〕`ArtifactoryHome#getImportRepoConfigJson` | 高
- JSON 解析失败：异常向上抛（启动中止）。〔码〕`JsonUtils...readValue` 直调 | 中

## 11. onboarding YAML（artifactory.config.import.yml）

- 当启动且该文件存在，则仅当 **(a) 非 SaaS（AOL）且 (b) 当前实例只有默认/空仓库集**时才生效；任一不满足 → ERROR 日志（「can't import file ... - Artifactory repositories have already been created」）并放弃导入。〔码·artifactory-core〕`repository/onboarding/OnboardingYamlBootstrapper#loadBootstrapSettingsFromYaml` | 高
- 可配字段（YAML 顶层两段）：`OnboardingConfiguration.repoTypes`（按包类型创建默认 local/remote/virtual 仓库组，模板来自内置 `/templates/defaultRepository.json`；OSS/Conan-CE/JCR 运行模式过滤可用类型；remote 无 url 时套默认上游 URL；Maven 按仓库名含 "snapshot" 分流快照/发布）；`GeneralConfiguration.baseUrl` / `proxies[]`（key/host/port/username/password/platformDefault/services）/ `eula.accepted`（仅 JCR 模式）/ `licenseKey`（单点）/ `licenseKeys`（HA）。〔码〕`OnboardingYamlBootstrapper#setupDefaultRepos/setupBaseUrl/setupProxy/setupJcrEula/setupLicense` | 高
- 当导入成功：把生效内容导出为 `etc/artifactory.config.<yyyyMMdd.HHmmss>.yml` 留档，随后**删除 import.yml 本体**；许可证延迟到上下文创建后安装激活。| 高
- 当 YAML 解析失败：ERROR「Unable to parse YAML file ...」并放弃（不中止启动）。| 高

## 12. security 相关文件

### 12.1 etc/security/ 文件族（代码内清单）

| 文件 | 语义 | 证据 | 置信度 |
|---|---|---|---|
| `artifactory.key` | 实例级加密密钥（wrapper 工厂支持多回退密钥：数量由 `artifactory.security.artifactory.key.num.of.fallback.keys` 控制、位置由 `artifactory.security.artifactory.key.location` 控制） | 〔码〕`ArtifactoryHome#getArtifactoryKey/getArtifactoryEncryptionWrapper` | 高 |
| `access/keys/access.creds` / `access/access.admin.token` | Access 客户端凭据 / 管理令牌；HA 共享且加密 | §13 共享清单 | 高 |
| `communication.key` / `communication.token` | 组件间通信密钥/令牌（**同步黑名单**，仅本机） | §13 黑名单 | 高 |
| `master.key`（var/etc/security/）、`join.key` | 平台主密钥（缺失且无法提供 → IllegalStateException「Could not load master key...」）/ 入群密钥；黑名单不同步 | 〔码〕`ArtifactoryHome#getMasterKey` | 高 |
| `artifactory.saml.encrypted.assertion.public/private` | SAML 加密断言密钥对 | 〔码〕`ArtifactoryHome#getArtifactorySAML*` | 高 |
| `trusted.keys.json`（mc/）、`service_id`、`cluster.id`、`storage.usage.json` | 信任键、服务标识、集群标识、存储占用 | 〔码〕`ArtifactoryHome` 常量区 | 高 |

security 目录在创建时立即收紧权限；被 §13 同步器接管的 security/ 下文件落盘强制 600。

### 12.2 access.security.bootstrap.yml（Access 侧一次性安全引导）

- 当 Access 就绪事件触发且 `var/etc/access/access.security.bootstrap.yml` 存在（多租户关闭时），则按 YAML 中的 `security:` 段**逐项 upsert** 进 Access 数据库：httpSsoSettings、ldapSettings（map，键=ldap key）、ldapGroupSettings、crowdSettings、samlSettings（键=name）、OAuth 通用+各 provider 配置——**upsert 语义：同名更新、其余保留**（与 §10 的 replace 不同）。〔码·access-server-core〕`bootstrap/AccessSecurityBootstrap#init/loadConfigurationFromFile` | 高
- 处理成功后**删除该文件**；解析失败 → ConfigurationException「Failed to load bootstrap security configuration」（启动中止）。| 高

### 12.3 config.xml 的 security 段与 Access 的关系

- 当保存 config 描述符，则密码过期策略、默认代理/代理清单等 security 相关子集被抽取推送到 Access 配置（键形如 `security.authentication.*`、`security.user-lock-policy.*`——enabled→attempts/max-login-delay-millis 等）。〔码·artifactory-config〕`config/AccessConfigMapUtils` + 〔码·artifactory-core〕`prepareToSendDataToAccess` | 高
- LDAP/SAML/OAuth/Crowd/httpSSO 的持久真源在 Access 侧（config.xml 内对应节点仅为兼容视图）——由 §12.2 bootstrap 键集与 AccessConfigKeys 常量族印证。〔码〕`config/AccessConfigKeys` | 中（完整映射表未逐一走读）

## 13. etc → DB 共享配置同步机制（文件监听与 HA 广播）

- 当服务启动完成 DB 通道就绪，则对 **etc 目录树注册文件监听**（静默期默认 20s、阻塞窗口默认 60s、冲突重试 3 次——`artifactory.configuration.manager.*` 微调键）。白名单=§2.1 标「HA 共享」的文件 + plugins/ ui/ security/ 三个目录下任意文件；黑名单（永不入库/广播）：`security/access`、`security/binstore`、`artifactory.config*`、`communication.key/token`、`artifactory.tmp.key`、`master.key`、`join.key`、签名密钥，及正则 `artifactory/config/**.import.*`、`artifactory/config/security.*.xml`。〔码·jfrog-config〕`ConfigurationManagerImpl` + 〔码·artifactory-config〕`ArtifactoryConfigurationAdapter` | 高
- 当白名单文件被修改：按「文件 mtime vs DB 时间戳」判胜负——DB 新 → DB 覆写文件；文件新 → 文件写 DB 并广播集群；相同 → no-op；protected 配置（DB 已有受保护记录）走专用写路径。远程节点收到广播后反向物化文件（写后校准 mtime 抵消时钟差）。〔码〕`ConfigWrapperImpl#modifyInternal/dbToFile/fileToDb` | 高
- 当需要强制删除共享文件：创建 `<文件名>.force.delete` 标记文件 → 同步器按 DELETE 处理真实文件并清 DB 记录。〔码〕`ConfigurationManagerImpl#fileChanged`（FORCE_DELETE 分支） | 高
- 当 mandatory 文件被远程删除：WARN「Mandatory file ... was removed remotely ... Skipping deletion」并跳过（重新物化）；本地删除 mandatory → 因有内置默认播种而重建。〔码〕`ConfigWrapperImpl#remoteRemove/ensureConfigurationFileExist` | 高
- security/ 下文件入库自动加密、物化后权限强制 600。〔码〕`ConfigurationManagerImpl#getPermissionsFor` | 高
- 本机制只保证**文件在集群一致**；内存生效仍按 §9.4 分面（binarystore 等仍需重启）。| 高

## 14. 生效配置对外暴露面

### 14.1 端点表（Artifactory REST，前缀 /artifactory/api）

| 方法+路径 | 权限 | 成功响应 | 关键行为 | 置信度 |
|---|---|---|---|---|
| GET `/api/system/configuration`（Accept: application/xml） | admin/ha（方法级另有 user 覆盖的子资源） | 当前生效描述符 XML | **非访问令牌认证时 proxy/mail 密码置 null 后返回**（即明文密码永不回显，除非 access-token 认证）；SaaS 非 dashboard 用户拒绝 | 高 |
| POST `/api/system/configuration`（application/xml） | admin | 200 text/plain「Reload of new configuration succeeded」 | 全量导入：转换（strict）+校验+保存热重载（§9.3）；SaaS 校验同 §9.3 PUT；解析失败 400 | 高 |
| PATCH `/api/system/configuration`（application/yaml） | admin | 200「<N> changes to config merged successfully」 | 增量合并导入（§9.3）；`~`=删键 | 高 |
| PUT `/api/system/configuration/baseUrl` | admin | 200「URL base has been successfully updated to "...".」 | §9.3 | 高 |
| GET `/api/system/configuration/platform/{baseUrl,federatedUrl,saas,mail,proxies,proxies/{serviceKey},security}` | user/admin（system:platformconfig:r） | JSON 子模型 | 各子面只读；密码同样按认证方式脱敏；saas 布尔；proxies/{serviceKey} 不存在 → 404「Proxy configuration for specified service '...' not found」 | 高 |
| GET `/ui/api/admin/system/configdescriptor` | admin | JSON `{configXml: "<当前描述符 XML 字符串>"}` | SaaS 拒绝（assertNotAol） | 高 |
| PUT `/ui/api/admin/system/configdescriptor` | admin | JSON「Central configuration successfully saved」 | 空 XML → 错误「Cannot save null or empty central configuration」；离线态 → 「Cannot save config descriptor during offline state」；保存走与 POST /api/system/configuration 同一 setConfigXml 路径 | 高 |
| POST `/api/import/system` | admin | 200（流式状态）/500 | 全系统导入（含 §9 的 config.xml 导入路径）；`artifactory.system.import.enabled`（默认 true）关闭 → 403「System Import is disabled.」；路径校验：拒绝 `$`/UNC/`//` 前缀、含 `/..`、Artifactory home 或其祖先、白名单外路径（默认空=不限）；hybridMigration 仅 SaaS（非 SaaS 403） | 高 |
| POST `/api/export/system`、GET `/api/export/system`（示例） | admin | 200 | 导出将当前描述符写 `<path>/artifactory.config.xml`；路径校验同上 | 高 |
| POST `/api/import/repositories?path=&repo=&metadata=&verbose=` | admin | 流式 | `artifactory.repository.import.enabled`（默认 true）控制；repo 空格=全仓库 | 高 |

### 14.2 导出文件的敏感信息形态

- 当导出 config.xml（全系统导出或 GET configuration），密码类字段以**加密串**出现在 XML（保存前经拦截器加密）——加密串仍随导出携带；支持包收集器对采集文本做的**字面脱敏串表**（password=/password:/`<password>`/`<refreshToken>`/`<secret>`/`<managerPassword>`/`<passphrase>`/`<gpgPassPhrase>`/`<keyStorePassword>`/`<identity>`/`<credential>`/bintray 前缀/`<privateKey>`/`<accountName>`/`<accountKey>`/`<proxyIdentity>`/`<proxyCredential>`/`<clientSecret>`/`<tenantId>`/`<sasToken>`/`<cloudFrontPrivateKey>`/`<clientId>`）仅用于 system info/支持包，不影响导出文件本体。〔码·artifactory-support-core〕`ArtifactoryConfigFileCollector#getScrubStrings` | 高
- 支持包收集的配置文件清单 = §2.1 的 config 家族 5 文件 + logback + mimetypes + binarystore + system.properties + `$JFROG_HOME/var/etc/system.yaml`。| 高

## 15. 与官方文档的差异/补充

- 官方 System YAML 文档给出 env > 服务段 > shared > 默认 的层级与示例键，但**不给代码内默认值表**；§4.2/§6.3 默认值全部来自反编译（此条补充官方规范）。
- 官方未说明 `artifactory.config.import.xml` 会被**改名消费**、优先级高于 DB、以及与 repository.config.import.json 的互斥校验（§8/§10）——此条补充官方规范。
- 官方 Config Descriptors 页未列「POST 全量=替换（除带旧 revision 的三方合并例外）、PATCH=真合并、`~`=删键」的精确语义（§9.3）——此条补充官方规范。
- 官方文档无 etc→DB 文件同步器（§13）描述；该机制解释了 HA 下直接改 binarystore.xml/mimetypes.xml 会被集群物化传播的现象——此条补充官方规范。
- GET /api/system/configuration 的密码脱敏与 access-token 认证例外（§14.1）官方 REST 文档未载——此条补充官方规范。
- storage.properties 在 7.161 反编译树零命中：该文件名属于历史版本（4.x/5.x 概念），7.x 无对应物；官方文档亦无此文件（负证，双方一致）。

## 16. 待验证清单（低置信度/未定位项汇总）

1. **全局优先级仲裁**：同一逻辑项同时出现在 system.yaml 与 artifactory.system.properties/-D 时的最终取向（sysconf 内核未反编译；需活体实验：同键双设 + 观察行为）。
2. **ha-node.properties legacy 回退**：7.161 是否仍读旧文件（代码只见 system.yaml 键；放置旧格式文件于 etc 观察是否生效）。
3. **system.yaml ${VAR} 插值**：判定为不支持（负证），保留动态验证（sysconf 内核不可见）。
4. **JF_ 环境变量映射规则细节**（下划线↔驼峰/点、列表注入形态）：官方文档口径 + sysconf 未反编译——Helm/Docker 环境实测。
5. **XML 重复元素的确切报错文案**（§9.3 末行，机制清楚、文案未取证）。
6. **binarystore 非法组合/未知 provider 的启动报错形态**（T-303 遗留，展开器未反编译）。
7. **config.xml security 段 ↔ Access 键的完整映射表**（§12.3 只取证了子集）。
8. **artifactory.repository.config.import.json 解析失败的启动中止形态**（§10 末行，异常类型直证、HTTP/日志表现未取证——该路径无 HTTP 面，需看启动日志）。
9. **db.properties 旧文件在 7.x 安装器层面的迁移**（Java 侧无读取代码；是否存在安装脚本迁移未验证）。

## 断点快照（本票无遗留；供后续票参考）

本票 T-515 已完成 §1–§16 全部落盘。低置信度项全部收敛进 §16。
