# 存储布局行为规格（M1：checksum 寻址 filestore）

> 逆向基线：artifactory-pro 7.161.16。官方参照：JFrog "Checksum-Based Storage" / "Checksum-Based Storage Implementation" / "File System Binary Provider"（docs.jfrog.com/installation/docs/*）。
> 置信度：`高` 双证 / `中` 仅代码 / `低` 推断。

## 1. 三层模型（概览）

| 层 | Artifactory 实现 | BinFlow 对应 | 置信度 |
|---|---|---|---|
| 元数据层 | 关系库表 `nodes` / `binaries` / `node_props` / `stats`（Derby/PG/MySQL…，DDL 在 `storage-db` jar 的 `<dbtype>/<dbtype>.sql`） | SQLite（默认）/ Postgres | 高 |
| blob 层 | `$JFROG_HOME/var/data/artifactory/filestore/`（binarystore.xml 默认 `file-system` 模板） | `data/blobs/`（命名可自定，见 config-formats.md） | 高 |
| 临时层 | `var/data/artifactory/tmp/work`、`tmp/artifactory-uploads` + filestore 内 `_pre` 暂存 | `data/tmp/` | 中 |

## 2. filestore 目录推导与 blob 命名

| 规则 | 规格 | 置信度 |
|---|---|---|
| blob 文件名 | **sha1 十六进制小写全量**（40 字符），无扩展名 | 高（官方 docs「Name File By Checksum」+ `binaries` 表主键 `sha1 CHAR(40)`） |
| 分片目录 | sha1 **前 2 字符**为一级子目录：`filestore/ab/abcdef...`、`filestore/d4/d4a3b2c1...`（共 256 个桶） | 高（官方 docs 示例；DDL `nodes.sha1_actual → binaries.sha1` 外键证明一对一） |
| sha256 角色 | 仅存于 DB（`binaries.sha256 CHAR(64)` + 索引），**不参与物理路径**；5.5+ 上传时一并计算 | 高 |
| md5 角色 | DB 字段 + 唯一索引（`binaries_md5_idx`），不参与路径 | 高 |
| 去重 | 相同 sha1 只落一份文件；多个 repo path 经 `nodes.sha1_actual` 外键引用同一 `binaries` 行。上传已存在 checksum 的内容（含 Expect: 100-continue / checksum-deploy 路径）直接复用 | 高 |
| BinFlow 决策点 | 建议 BinFlow 以 sha256 为 blob 名（`blobs/ab/<sha256>`），sha1 仅作兼容字段记录——与 Artifactory 磁盘布局不兼容但行为等价；若需磁盘级迁移兼容则沿用 sha1 命名。规格要求二选一后在 config-formats.md 固化 | 设计建议（非逆向结论） |

## 3. 元数据表结构（对 BinFlow SQLite schema 的行为要求）

以下为 Artifactory DDL（`reverse-src/artifactory/src/batch1-core/derby/derby.sql` 等）中与 M1 相关的最小集。BinFlow 无需同构，但**必须提供等价能力**：

| 表 | 关键列 | 行为要求 | 置信度 |
|---|---|---|---|
| `binaries` | `sha1`(PK), `md5`(uniq idx), `sha256`(idx), `bin_length` | 每个 blob 一行；引用计数由 `nodes` 隐式提供（删除节点不删 blob，GC 负责） | 高 |
| `nodes` | `node_id`(PK), `node_type`(0=folder/1=file), `repo`, `node_path`(父目录路径，根为空), `node_name`, `depth`, `created/created_by/modified/modified_by/updated`, `bin_length`, `sha1_actual`(FK→binaries), `sha1_original`, `md5_actual`, `md5_original`, `sha256` | `(repo, node_path, node_name)` **唯一索引** = 同目录不可重名；`node_type` 区分文件/目录；`sha1_actual` 服务端实测值、`*_original` 客户端声明值（REST `checksums` vs `originalChecksums` 的来源） | 高 |
| `node_props` | `prop_id`, `node_id`(FK), `prop_key`, `prop_value` | 一个节点多行 KV；`(node_id, prop_key, prop_value)` 索引支撑属性搜索 | 高 |
| `stats` | `node_id`(PK), `download_count`, `last_downloaded`, `last_downloaded_by` | 下载计数；REST `?stats` 数据源 | 高 |
| `unique_ids` | `index_type`, `current_id` | 全局 ID 发号器（node_id/prop_id 等自增来源） | 高 |

路径语义：`node_path` 存父目录（`repo/path/to` 的父为 `path/to`？否——`node_path` 是**该项所在目录**的完整相对路径，文件名在 `node_name`；根目录节点 `node_path=''`、`node_name` 为空或仓库名约定）。**中**（DDL 无注释，由唯一索引与查询语义反推；动态验证手段：上传 `a/b/c.txt` 后查 `node_path='a/b' AND node_name='c.txt'`）

## 4. 上传落盘流程（临时区行为）

```
客户端 body
  → tmp/artifactory-uploads/<随机名>（流式落盘，边算 checksum）   [置信度: 中]
  → 校验通过后 move/rename 到 filestore/<sha1[:2]>/<sha1>（同分区原子 rename）
  → DB 事务写 binaries（不存在则插）+ nodes + node_props
```

| 行为 | 规格 | 置信度 |
|---|---|---|
| 暂存目录 `filestore/_pre` | 上传未完成/未算出 checksum 的文件暂存处；完成后移入正式位置。残留 `_pre` 内容 = 中断上传的垃圾，可被清理任务回收 | 高（JFrog KB "All About the _pre, artifactory-uploads, and work Folders" + 官方 eventual/cached provider 文档提及 `_pre`） |
| `_temp` 目录 | GC/压缩等内部操作的临时区 | 中（官方文档仅在特定 binary provider 链中提及；默认 file-system 模板下的使用点未在反编译代码中直接定位——`org.jfrog.storage.binstore` 三方库未随 JAR 反编译） |
| `tmp/work`、`tmp/artifactory-uploads` | 启动时创建；`cleanTempDirs` 在**启动时删除修改时间 > 24h 的所有子目录**（保留 work/、artifactory-uploads/、fullSync 三个常驻目录本身），并清空目录树 | 高（`o.a.a.common.home.ArtifactoryHome#create/cleanTempDirs`） |
| 崩溃一致性 | rename 原子性保证 filestore 中不会出现半写 blob；DB 事务保证 node 与 binaries 一致。孤儿（blob 有、node 无）由 GC 收敛 | 高（官方 docs「Deleting Files」：删除只删 DB 记录，物理文件后台 GC） |
| 上传会话 | Artifactory 单体上传无显式会话对象；BinFlow 的「上传会话」对应此暂存文件生命周期（创建→写满→checksum→rename 或超时清理） | 设计映射 |

## 5. 删除与垃圾回收

| 行为 | 规格 | 置信度 |
|---|---|---|
| 删除制品 | 只删 `nodes`（及级联 props/stats），**不删 blob**；返回 204 | 高 |
| 回收站 | 默认开启（`trashcanConfig.enabled=true`，保留 14 天）：删除前把节点复制到内置仓库 `auto-trashcan`，属性打上 `trash.deletedBy` / `trash.originalRepository` / `trash.originalRepositoryType` / `trash.originalPath` / `trash.time`（epoch ms）。**非 admin 删除仓库根目录被拒（403）**；BinFlow M1 可将回收站降级为可选开关 | 高（`o.a.a.repo.service.trash.TrashServiceImpl` + config 模板默认值） |
| GC | 后台任务比对 `binaries` 与 `nodes` 引用，删无引用 blob；`POST /api/system/storage/prune/start` 手动触发（202 异步） | 高（语义）/ 中（触发时机 cron：`gcConfig.cronExp` 默认 `0 0 /4 * * ?` 即每 4 小时——cron 语法为 Quartz，注意 `/4` 语义为「从 0 分起每 4 分钟」属于 Quartz 对分钟域 step 的解释，实际官方模板意图为每 4 小时于 0 分执行需写作 `0 0 0/4 * * ?`；此处以官方模板原文为准存疑） |
| binarystore 内部 `_pre` 清理 | 每日任务清理陈旧 `dbRecord*.bin`（cached-fs provider 文档）；默认 file-system 模板下未见独立清理 cron | 低 |

## 6. 目录结构总表（Artifactory 7.x，$JFROG_HOME/var 下）

| 路径 | 用途 | 置信度 |
|---|---|---|
| `etc/artifactory.config.xml` | 全局配置（repo 定义已迁出至 DB） | 高 |
| `etc/binarystore.xml` | blob 存储链配置 | 高 |
| `etc/security/` | 密钥（artifactory.key、access.creds 等） | 高 |
| `data/artifactory/filestore/` | blob 主存储（`_pre`/`_temp` 在其内） | 高 |
| `data/artifactory/derby/`（默认内嵌 DB） | 元数据库 | 高 |
| `data/artifactory/tmp/work`、`tmp/artifactory-uploads` | 临时工作区 / 上传暂存 | 高 |
| `data/artifactory/backup/` | 系统备份输出 | 中 |
| `data/artifactory/.cache` | 元数据缓存 | 中 |
| `log/` | 日志 | 高 |
| `data/artifactory/git/`、`import/` | git-lfs / 导入暂存 | 低（M1 无关） |

来源：`o.a.a.common.home.ArtifactoryHome#create`（逐目录 createSubDir + 可写校验，任一失败启动即失败并报 `Could not initialize artifactory directory structure`）。

## 7. 与官方文档的差异 / 补充

- 官方文档完整描述 sha1 前缀两字符分片；反编译补充了 **DB 侧 `binaries.md5` 唯一索引**、`node_path/node_name` 唯一约束等 schema 细节（官方从不公开 DDL）。
- 官方未写 `_pre`/`_temp` 的确切清理周期（cached provider 提「daily cleanup job」）；默认模板下未见明确 cron，标注低置信度。
- 启动时 temp 目录「>24h 即删」的行为仅代码可见（官方文档未提）。

## 待验证清单

1. `node_path` 是否含根节点特例（空串 vs 仓库 key）——需对运行实例查库确认。
2. 默认 file-system 链下 `_pre` 的清理触发点（怀疑在未反编译的 `org.jfrog.storage.binstore` 三方库内）。
3. `gcConfig.cronExp=0 0 /4 * * ?` 的真实调度语义（Quartz 分钟域 step）——建议 BinFlow 直接用标准 cron 每 4 小时，不复制该表达式。
