# 配置格式行为规格（M1：artifactory.config.xml / binarystore.xml → BinFlow YAML）

> 逆向基线：`reverse-src/artifactory/config-templates/`（一手模板：artifactory.config.xml / binarystore.xml / mimetypes.xml / artifactory.system.properties / logback.xml）+ `o.a.a.descriptor.*` / `o.a.a.common.ConstantValues`。
> 官方参照：JFrog "Artifactory Configuration XSD / Descriptors"、"System YAML Configuration"。
> 置信度：`高` 双证 / `中` 仅代码/模板 / `低` 推断。

## 1. binarystore.xml（blob 存储链）

默认模板全文（template `file-system`）：

```xml
<config version="1">
    <chain template="file-system"/>
</config>
```

| 行为 | 规格 | 置信度 |
|---|---|---|
| 文件位置 | `$JFROG_HOME/var/etc/artifactory/binarystore.xml`（`ArtifactoryHome#getBinaryStoreXmlFile`） | 高 |
| chain 模板 | `file-system`（默认本地盘）；其它模板（cached-fs / S3 / GCS / Azure / sharding…）通过 `<chain><provider id=... type=.../></chain>` 组合。**BinFlow M1 只需本地盘一种** | 高 |
| 可配置点（file-system 模板展开后） | `<provider id="file-system" type="file-system"><baseDataDir>...</baseDataDir><fileStoreDir>...</fileStoreDir><tempDir>...</tempDir></provider>`——filestore 目录、临时目录可改 | 高（官方 File System Binary Provider 文档） |
| 危险性 | 官方在文件头警告：改此文件可能丢数据 | — |

### BinFlow YAML 映射（建议）

```yaml
storage:
  blobstore:
    kind: filesystem        # 对应 chain template；M1 仅 filesystem
    path: data/blobs        # 对应 fileStoreDir（Artifactory 默认 var/data/artifactory/filestore）
    tmp: data/tmp           # 对应 tempDir + var/data/artifactory/tmp 职责合并
    shard_depth: 2          # checksum 前缀分片字符数（Artifactory 固定 2，BinFlow 可配）
    address_by: sha256      # sha1（Artifactory 兼容）/ sha256（BinFlow 推荐）
  gc:
    interval: 4h            # 对应 gcConfig.cronExp 的意图
    prune_on_start: false
```

**置信度：映射表本身为设计建议；各默认值来源见 storage-layout.md。**

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
