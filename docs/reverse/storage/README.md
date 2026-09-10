# storage/ — 存储域规格目录（Phase 0 任务 #7）

> 目录化索引：两份既有存储规格的入口 + 宪章 §16 缺口对账（multipart / GC / corruption / recovery 等）。
> **不重写既有内容**；行为规格本体在 `docs/reverse/` 根下两文件。注意两份规格共同的证据限制：
> S3 二进制提供者实现（`org.jfrog.storage.binstore`）在 JFrog 通用库内、不在反编译范围——云存储行为多为
> 配置属性推断（中/低置信）。

## 1. 既有规格索引（两份）

| 文件 | 覆盖面 | 关键锚 | 版本 |
|---|---|---|---|
| `../storage-layout.md` | 三层模型（§1：元数据/bin/blob）、**filestore 目录推导与 blob 命名**（§2：sha1 全量命名+前 2 字符分片+sha256 不参与路径）、元数据表结构（§3：binaries/nodes/node_props/stats/unique_ids DDL 级）、上传落盘流程（§4：tmp 暂存→_pre→rename→DB 事务）、**删除与 GC**（§5：回收站 14 天属性族、prune 端点、GC cron）、目录结构总表（§6） | 崩溃一致性（rename 原子+DB 事务）、启动清 temp >24h | 7.161.16 反编译 + 官方 docs |
| `../s3-storage-layout.md` | binarystore.xml 链式架构（§1：provider 类型族/配置加密）、S3 与云存储配置属性（§2：重定向阈值 200KB 等 ConstantValues 全表）、**MPU 配置与会话语义**（§3：默认禁用、token 2 天/5 分钟、心跳 60s/超时 180s、清理 cron+7 天）、StorageBinaryServiceImpl 平台集成面（§4）、BinFlow 配置映射建议（§5） | MPU 是 Artifactory 自定义客户端协议而非 S3 原生 MPU API（低置信注记） | 7.161.16 配置属性推断 |

相关联：`../config-formats.md`（binarystore.xml→BinFlow 配置映射）、`../import-export-api.md`（系统级导出/恢复面）、`../repo-operations.md`（上传/删除操作语义）、inv-1-core 存储分区、运行时日志 taxonomy 中 `artifactory-binarystore` / `artifactory-cleanup-audit` / `artifactory-path-checksum-migration` 三个 logger（E4 样本，reports/artifactory-full-audit.md 附录 A）。

## 2. 宪章 §16 覆盖度对账

### 2.1 已覆盖

| 轴 | 覆盖内容 | 出处 |
|---|---|---|
| GC（部分） | prune 语义（比对 binaries/nodes 引用、202 异步、手动端点）、回收站属性族与 403 守卫、GC cron 出厂值 | storage-layout §5；cron-scheduling §2 |
| multipart（配置面） | MPU 开关/参数/会话/清理全表（配置属性级，中置信） | s3 §3 |
| 崩溃一致性（上传腿） | tmp→_pre→rename→DB 事务链；孤儿 blob 由 GC 收敛 | storage-layout §4 |
| 去重与校验 | sha1 寻址、上传复用、checksum mismatch 400（REST 面） | storage-layout §2；rest-api §1 |

### 2.2 缺口清单（UNKNOWN——每条：问题 / 为何未知 / 需要什么证据）

1. **MPU wire 协议**：JFrog CLI 与 Artifactory 的分块握手逐端点（init/part/complete/abort）、checksum-deploy token 用法——s3 §3 只有配置与会话语义，协议本体未提取；需 JFrog CLI 源码/抓包 + 反编译 MPU REST handler（若在 `org.jfrog.storage.binstore` 则不可达，转 CLI 侧取证）。
2. **GC 运行时行为**：GC 运行中的锁/并发上传互斥、GC 报告格式与落盘、prune checkpoint 语义（`binary.provider.prune.checkpoint.numberOfFiles` 属性存在但行为未录）、quota 触发后 GC 是否加压——需反编译 GC/prune 任务类 + :8082 触发观测（写操作腿需授权）。
3. **corruption（静置损坏）**：bit-rot/静默损坏的检测机制（是否存在周期校验）、`artifactory-path-checksum-migration` logger 对应的 Sha256MigrationJob 行为（反编译已定位 `artifactory-core/.../storage/jobs/migration/sha256/Sha256MigrationJob.java`——迁移时机/断点续跑/范围未规格化）、missing binary 时的下载/列表降级行为（`MissingBinaryException` 引用散见 UploadServiceImpl/RemoteRepoBase 等，未汇总）——需专项反编译票。
4. **recovery（系统级）**：备份→恢复全流程行为（import-export-api.md 覆盖 REST 面，恢复语义/中断续恢/版本兼容未录）、`PersistentQueueErrorService`（artifactory-core/queue/error——持久队列错误恢复语义零规格）、federation 断网恢复腿——需专项票。
5. **`_pre` 清理触发点**：storage-layout 待验证#2 原挂账（疑在未反编译的 storage.binstore 库）；可转移为 :8082 观测（造中断上传后看清理周期）。
6. **`gcConfig.cronExp=0 0 /4 * * ?` 调度语义**：storage-layout 与 cron-scheduling 交叉挂账（C-d）；BinFlow 引擎对拍腿可关。
7. **cached-fs / sharding / 双缓存链行为**：s3 待验证#4/5/6——provider 链路由/降级/健康检查零规格；实现库不可达，需官方文档 + 运行时（MinIO 后端实例）观测。
8. **云存储重定向**：预签名 URL 生成/有效期（s3 待验证#3）；需运行时抓包（配置 MinIO + 大文件下载）。
9. **配额（quota）运行时**：超配额时上传拒绝形态（4xx 码/文案）未规格化；需 :8082 配低配额触发（写腿）。
10. **HA 下的存储一致**：主备 filestore 同步（EFS/NFS 场景）与 binarystore 锁——企业域（任务 #5 交叉），此处登记不展开。

## 3. 阅读序

1. storage-layout.md §1→§2（blob 寻址——一切存储票的地基）→ §4/§5（上传/GC）
2. s3-storage-layout.md §1→§3（对象存储与 MPU）
3. 缺口（§2.2）→ unknown 队列（任务 #8）；corruption/recovery 两条建议立专项逆向票（A 源已定位类路径）。

## 4. 一致性自检

- 两份规格无冲突；`0 0 /4` cron 存疑点两文件均显式挂账（storage-layout 待验证#3 ↔ cron-scheduling C-d），口径一致。
- 本 README 新增的 §2.2-3/§2.2-4 类路径锚（Sha256MigrationJob、PersistentQueueErrorService）来自本轮 grep 定位，属 evidence 指针而非行为断言，未越 clean-room（无结构翻译）。
