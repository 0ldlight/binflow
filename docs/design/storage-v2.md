# 存储 v2 设计——binary provider 链差距裁定与 KEEP/REFACTOR/REWRITE 终案（Phase 1 架构族 / L001-4）

- 状态: Accepted（裁定性内容已由 **ADR-0049** 正式化收编——2026-09-11，LOOP 004 L004-4；本文裁定总表/终案建议以 ADR 为准，§4-1 的 progress sink 措辞由 ADR-0049 注记 A〔Pruner optional facet〕取代、正文不回改；§6 建议稿位维持历史记录）
- 日期: 2026-09-11
- 作者: architect（L001-4）
- 输入证据:
  - 逆向规格: `docs/reverse/storage/binary-provider-chain.md`（LOOP 000 / L000-A，U-STG-01 → partial；证据基 B=运行时 7.161.20 活体 / A=反编译 7.161.24 全量 + 7.161.20 partial / C=镜像内 jar 模板 4.372.5；条目 A1-A6/D1-D4/G1-G8/C1-C6/H1-H3 各带置信度，本文引用即「§节-条目（证据基，置信度）」）
  - gap 总账: `docs/ai-engineering/artifactory-binflow-gap.yaml` storage-filestore 条目（migration_verdict 候选倾向「KEEP〔本层读写链〕+ REFACTOR〔provider 链抽象按 U-STG-01 取证后扩展〕」——本文给出终案）+ db-persistence 条目（KEEP，本文不重开）
  - as-built（只读）: `internal/storage/`（api.go 接口契约、engine.go/session.go、gc.go/hold.go、s3.go、backup.go）、`internal/repo/trash.go`（回收站）、`internal/httpapi/router.go`（GC 管理面）、`internal/config/api.go`（trashcan/binstore 段）
  - 既有裁定: ADR-0006（布局即兼容承诺）、ADR-0007（元数据双栈）、ADR-0015（GC 在线触发）、ADR-0018/0019（S3/Backend 缝）、ADR-0031（GC 并发安全）、ADR-0036（binstore.yaml 显式链）、ADR-0039（MPU 面翻转）、ADR-0040（dual-write fail-open）
- 设计原则（57 节总令 §41，conductor 转述）: dependency inversion / plugin point / service boundary——对齐**等价能力**；Artifactory 的 24 模板 provider 链是其 Java 类层次的表达，BinFlow 已有 Go-native 等价物的面**不复刻形态**，未有能力按部署矩阵与产品目标裁剪。

---

## 1. 裁定总表（Artifactory 行为 → BinFlow as-built → 终案）

裁定词：**KEEP**（已有等价能力，不动）/ **REFACTOR**（改造对齐）/ **INTENTIONAL**（不迁移、登记有据的有意差异）/ **UNSUPPORTED**（不进目标，留缝或不留）/ **UNKNOWN**（证据不足不裁，挂账）。

| # | Artifactory 行为（证据） | BinFlow as-built | 终案 | 理由 |
|---|---|---|---|---|
| 1 | 上传流式暂存 `_pre/dbRecord*.bin` → 成功 rename 至寻址路径（§4.1-A1/A2，B 活体，高） | `uploads/<uuid>/data` 追加写 → fsync → rename → fsync(dir)（ADR-0006 决策 3，Session 契约） | **KEEP + INTENTIONAL（形态）** | 原子性语义等价（同分区 rename、崩溃无半写暴露）；`_pre` 命名/路径不迁移——`uploads/` 是 BinFlow 布局即兼容承诺（ADR-0006 后果条款），`dbRecord*.bin` 是 filestore 实现产物且命名成分生成源未取证（§8-5，低） |
| 2 | 传输中断秒级回收 _pre、无节点无 tmp 残留（§4.1-A3，B，高） | Append 失败 poison + Abort + 启动 sweep（Session 契约 / ADR-0028） | **KEEP** | 行为等价（差分可证：中断后 GET 404、无残留） |
| 3 | 崩溃残留周期清理（§4.1-A6：`PreTempDirCleanupRunnable` 类锚在、cron 值 UNKNOWN，低） | sweep = 启动 + SessionSweeper 维护钟（cron 台账 gc 槽族） | **KEEP** | 能力等价；Artifactory 周期值 UNKNOWN，无从对齐亦不猜测（ADR-0001） |
| 4 | checksum 不符 **409** + 三算法对照错误体（§4.1-A4，B，高；修正旧规格 400） | client-checksums 409 已 ✅（matrix D01） | **KEEP** | 已兼容 |
| 5 | checksum-deploy 缺请求头 400 明示缺哪个（§4.1-A5，B，高） | 已 ✅（matrix D01 checksum deploy 面） | **KEEP** | 已兼容 |
| 6 | blob 寻址 `filestore/<sha1[:2]>/<sha1>`、sha256 只入库（§4.2-D4，B，高） | `blobs/<sha256[:2]>/<sha256>`，sha1/md5 附属入 `blobs` 表（ADR-0006） | **KEEP + INTENTIONAL（sha256 寻址）** | 协议面兼容由附属摘要承载（下载校验文件/X-Checksum 头族已 ✅）；物理布局是各自版本承诺——跨产品磁盘级互换不在目标（迁移工具走 `bf migrate`，ADR-0024） |
| 7 | 去重：N 路径一 blob、零字节 checksum-deploy、删引用不删 blob（§4.2-D1/D2/D3，B，高） | checksum 寻址去重同语义（§4.2 as-built，幂等 Commit） | **KEEP** | 已兼容（数据完整性 P0，§2） |
| 8 | 删除 → 回收站捕获 + 属性五元组 + blob 免于 GC（§4.3-G1/G2，B，高） | `auto-trashcan` + trash.* 五属性 + LiveChecksumSet 引用阻断（`internal/repo/trash.go`，M12） | **KEEP** | 已对齐；「回收站保留期=物理保留期」语义同构（trash 节点在 GC mark 集） |
| 9 | prune/gc 端点：`POST /api/system/storage/prune/start` **202 异步** + `prune/status` progress/report JSON（totalBinariesProcessed/Cleaned/BytesCleaned/lastHandledDirectory 逐目录）；gc 先 prune 扫描再 TRASH_AND_BINARIES（§4.3-G3/G4，B，高——**官方未记载，补充官方规范级新契约**） | `POST /api/v1/system/gc`（同步锁护、dry-run 默认）+ cron 六槽；无 prune/status 报告面 | **REFACTOR（P2）** | 管理面（非客户端面）行为差异：同步 vs 202 异步、无 progress 报告结构。响应 JSON 是新契约，需规格票冻结字面量（compatibility-engineer 域）后实现票对齐；优先级低（管理员面，无真实客户端锚——验收载体 = REST 对拍 curl） |
| 10 | GC 事件驱动：删除事件入 `node_events`（分区表）→ GCProvider 批次供给 MinorGcCollector（batch=trashcanMaxSearchResults、workers=3）（§4.3-G7，A，中） | mark-sweep 全量扫描 + hold-set + 删除前 Live 复核 + grace（blob mtime 基准）（ADR-0031，`internal/storage/gc.go/hold.go`） | **KEEP + INTENTIONAL（非事件驱动）** | 事件管道是 Artifactory 规模化实现形态；BinFlow 单实例 mark-sweep 的正确性论证更简单可证（hold + Live 双门，W-2 闭合）。事件管道不进目标；批次/worker 参数族随之不进 |
| 11 | **回收资格窗 UNKNOWN**：零引用 blob（含回收站节点全删）数分钟内手动 prune/gc 均不回收（§4.3-G5，B 现象高/原因 UNKNOWN；§8-1 挂账） | 资格 = 引用集判定 + grace 窗（显式、可证、零误删验收） | **KEEP + INTENTIONAL（不复刻 UNKNOWN 行为）** | 不可能也不应该复刻一条原因未知的延迟行为——Artifactory 该现象方向是缺陷向（回收不及时），BinFlow grace 是显式契约。§8-1 取证票若揭示机制，仅作记录不翻案 |
| 12 | GC 策略枚举（FULL/EVENTS_GC/TRASH_AND_BINARIES/…）+ cluster singleton 调度 + 任务联动（暂停 SHA256 迁移/回收站清理）（§4.3-G6，A，中） | 单实例 cron 台账 gc 槽（§25 调度域） | **UNSUPPORTED（HA 面随 #17 裁）** | 单实例形态无 singleton 语义需求 |
| 13 | 云链模板 `cache-fs(eventual(retry(s3-storage-v3)))`：本地缓存确认 + eventual 异步持久化 + retry（§3.2/§4.4-C1，C 模板高/时序中） | `[filestore, s3] + migration.mode=dual-write`（ADR-0036 显式链）+ 同步双写 + 故障窗 fail-open 磁盘重试队列（ADR-0040，已验收含 D-A 差分） | **KEEP + INTENTIONAL（不逐层复刻）** | 等价能力已立：本地快路径确认 + 云最终持久 + 故障窗不丢（fail-open 队列 = eventual+retry 的 Go-native 合并形态）。三层 provider 包装是 Java 类链表达，复刻即 class-to-class 翻译（§41）。cache-fs 本地读缓存层不做：filestore 形态即缓存；S3 形态的本地腿已由 dual-write 磁盘腿覆盖 |
| 14 | `-direct` 变体（无缓存直写云，§3.2） | `[s3]` 单员链 | **KEEP** | 等价 |
| 15 | GCS / Azure 模板族（§3.2，C，中） | 无（binstore.yaml 保留名 `azure`/`gs` 出现即拒启、文案点名保留位——ADR-0036 决策 2） | **UNSUPPORTED（留缝）** | Backend 接口缝（ADR-0019）+ 保留名诚实拒绝已埋；是否进目标 = 产品裁定（用户），进则新 Backend 实现 + binstore.yaml 开词表，引擎零改 |
| 16 | 归档（-archive/restore 族 §4.4-C4，低）/ sharding/double-shards/redundant-shards（§3.2，中） | 无 | **UNSUPPORTED** | 企业版冷存储/分片形态；无部署矩阵承载（ADR-0004），进目标需用户裁定 |
| 17 | HA 链 cluster-*（`sharding-cluster{local,remote}` + crossNetworkStrategy + r2/lenient 1，§3.3/§4.5-H1~H3，中） | 无（servelock 单实例，ADR-0002/0004） | **UNSUPPORTED（待产品裁定）** | gap 总账 NEW-BUILD 候选「ha」；wire 协议 UNKNOWN（H1）。HA 是否进目标是 Phase 1 产品裁定项，非本设计可裁 |
| 18 | 云重定向：≥200KB 且支持时 302 预签名 URL（`enableSignedUrlRedirect`，§4.4-C2，C+docs，中；URL 有效期 UNKNOWN） | 无（S3 读全代理） | **UNKNOWN（挂账不实现）** | 运行时未取证（需 MinIO 实例抓包，§8-4）；盲实现 = 猜测兼容行为（ADR-0001 红线）。取证后再裁 |
| 19 | S3 流式写入后存在性校验/自动建桶开关（§4.4-C3，C，中） | Commit 幂等去重 exists 检查（s3.go）；无自动建桶 | **as-built 核对项（P3）** | 静态单源（中置信）不足以裁定行为面；S3 差分票（MinIO 腿）顺带核对 |
| 20 | MPU 会话（`storage_multipart_uploads` 表 + 参数族，§4.4-C5，schema 高/会话中） | ADR-0039 已整体翻转对齐（六端点 + complete?sha1= 202 + 异步任务） | **KEEP** | 已裁定面不重开 |
| 21 | `full-db`/`full-db-direct`（blob 入 DB，§3.1，高/中） | 无（元数据 SQLite/Postgres 双栈 + blob 文件/对象存储，ADR-0007/0006） | **UNSUPPORTED** | blob-in-DB 形态无目标需求；BinFlow 三层模型（元数据/blob 存储/节点）等价心智已立 |
| 22 | `empty` provider 零字节优化（§3.4 类清单，B，中） | docker 空层 digest 协议面合成（emptyLayerDigestHex，C17 SAME） | **KEEP（协议面）/ 不做（存储面特化）** | 空 blob 在 checksum 寻址下就是普通 blob，存储面特化是内部优化、客户端不可观测——不做（§41：不为不可观测的实现细节买单） |
| 23 | 配置面 binarystore.xml（模板/加密属性/警告注释，§4.6，高） | binstore.yaml（ADR-0036：显式链 + 三分支并存 + fail-fast + secret 拒收） | **KEEP** | 已对齐行为（独立文件/链式多方案/provider 语义，clean-room 只对齐行为、格式自有）；**模板速记层维持不进**——ADR-0036 模板条款；U-STG-01 的 24 模板矩阵反而强化原裁定：模板词汇（20+ provider 类型 + 组合规则 + 默认参数展开）远超 BinFlow 链形闭集（filestore/s3/双员迁移链），速记层在当前链集零收益 |
| 24 | `tmp/artifactory-uploads` 角色（§5 修正：直传大文件不经它，何种形态走它 UNKNOWN） | `uploads/` 即全部瞬态（会话数据文件） | **KEEP** | 对照面本身 UNKNOWN，无对齐义务 |
| 25 | tempFolder 配额族常量（`tempFolderMaxSizeBytes` 等，§4.4-C6，B 常量名中/默认值未取） | 无临时目录配额 | **技术债登记（P3，默认不做）** | 运维防御面；超配额 4xx 形态 UNKNOWN（§8-7）。登记备查，不进 LOOP 001 |

## 2. 数据完整性 P0 面（必须兼容清单——全部 KEEP，as-built 锚）

不可妥协面（任何存储改造不得回退；每项给 as-built 契约锚）：

1. **原子落盘**：write → fsync(data) → rename → fsync(dir)，崩溃只留 session 残渣或完整 blob（ADR-0006 决策 3；`storage.Session.Commit` 契约注释（internal/storage/api.go））。
2. **中断回滚**：失败 Append 毒化会话、Abort 清除、启动 sweep 收残渣（`ErrSessionPoisoned` 契约 + ADR-0028 停机语义）。
3. **digest 强校验**：`Commit(expect)` 不符即弃、绝不以错配摘要落盘（`ErrChecksumMismatch`）；docker remote 代取 digest 验证（landFetchedManifest 502 臂）。
4. **去重不变量**：blob 全局唯一、nodes 多行引用、运行时 DELETE 只删引用（ADR-0006 决策 5；物理删除唯一入口 GC）。
5. **GC 零误删**：hold-set 排除在途 + 删除前 Live 单点复核（ADR-0031，`GCMarker` 契约）；dry-run 默认姿态。
6. **回收站引用阻断**：trash 节点在 LiveChecksumSet 内，GC/cleanup 双引擎均不可收（`internal/repo/trash.go` 尾注，测试钉死）。
7. **备份保 mtime**：grace 基准 = blob 文件 mtime（S3 = LastModified），备份/恢复工具必须保留（ADR-0006 勘误①硬约束）。
8. **checksum-deploy / 409 / X-Checksum 头族**：matrix D01 ✅ 面，回归锚。

## 3. 终案建议（对 gap 总账候选倾向的细化）

gap 总账 storage-filestore 候选倾向「KEEP（本层读写链）+ REFACTOR（provider 链抽象按 U-STG-01 取证后扩展）」。U-STG-01 已 partial 取证（24 模板矩阵 + file-system 链全行为面），终案：

- **KEEP（主体）**：核心读写链、回收站、GC 引擎行为（mark-sweep + hold + Live + grace）、S3/dual-write 链、binstore.yaml 配置面、MPU 面（ADR-0039 已裁）。
- **REFACTOR（仅两小面，均低优先）**：
  1. prune/gc 管理面对齐（202 异步 + status 报告 JSON，表 #9）——P2，规格票先行冻结响应字面量；
  2. per-package-type 检索窗默认（docker/helmoci 21600s）——P1，属 remote 缓存域，见 `docs/design/remote-cache-v2.md` §5.1（跨文档引用，存储域仅涉及 TTL 列消费）。
- **INTENTIONAL 登记（需 ADR authority，§6 草案）**：sha256 寻址（#6）、uploads/ 暂存形态（#1）、GC 非事件驱动 + 不复刻 UNKNOWN 资格窗（#10/#11）、云链不逐层复刻（#13）、存储面空 blob 不特化（#22）。
- **UNSUPPORTED（留缝或待产品裁定）**：GCS/Azure（Backend 缝 + 保留名已埋，#15）、归档/sharding（#16）、HA cluster 链（#17，gap 总账 NEW-BUILD 候选 ha——**用户裁定项**）、full-db（#21）、GC 策略枚举/cluster 调度（#12）。
- **UNKNOWN 挂账（取证后再裁，不猜测）**：云重定向 200KB/预签名（#18，MinIO 抓包）、S3 exists-check/auto-bucket（#19）、GC 资格窗机制（#11 记录性）、_pre 清理周期（#3 对齐已等价，周期值存档）。
- **REWRITE：无**。存储域不存在需要推倒重写的子系统——BinFlow 的 Go-native 等价物已覆盖 Artifactory 7.161 默认链（file-system）与企业 S3 链的全部可观测行为面；剩余差距是**产品范围**问题（云厂商广度/HA/归档），不是实现质量问题。

## 4. 接口级改造清单（不写实现）

| # | 改造 | 触点 | 前置 |
|---|---|---|---|
| 1 | prune/gc 管理面：`POST /api/system/storage/prune/start`（202）+ `prune/status`（progress + report{totalBinariesProcessed/totalBinariesCleaned/totalBytesCleaned/lastHandledDirectory}）；与既有 `/api/v1/system/gc` 的关系（对齐路径族 vs 兼容层挂载）随规格票定 | internal/httpapi + internal/storage（扫描进度回调缝：现有 GCSweep 无进度面，需加可选 progress sink——不动 Engine 接口本体，走 optional facet 先例） | 规格票冻结响应字面量（G3 是「补充官方规范」级新契约） |
| 2 | 包型检索窗默认（21600s） | internal/remote + internal/repo（remote-cache-v2 §5.1 已列，此处不重复） | remote-cache-v2 ADR |
| 3 | （无其他）storage.Engine / Backend / Session / GCMarker 接口**零改动**——本裁定即「不动」 | — | — |

明确**不做**的改造（防实现票越界）：provider 链运行时重排/模板展开器（#13/#23）、事件驱动 GC 管道（#10）、cache-fs 层（#13）、blob sidecar 属性文件（ADR-0006 已裁不跟进）、`_pre` 命名迁移（#1）。

## 5. 与既有 ADR 对账（冲突/无冲突）

| ADR | 结论 |
|---|---|
| ADR-0006（布局即兼容承诺、mark-sweep、备份保 mtime） | 无冲突——本终案是其延伸；sha256/uploads 形态差异由其后果条款承载，INTENTIONAL 登记（§6）补 authority |
| ADR-0007（元数据双栈） | 无冲突；full-db 链 UNSUPPORTED 与其互补（blob 不入 DB） |
| ADR-0015（GC 在线触发） | **小张力**：其同步触发面 vs Artifactory 202 异步（#9）——不翻 ADR-0015（同步触发保留），REFACTOR 以**新增** prune/status 异步面呈现，兼容层路径随规格票；若规格票要求 `/api/v1/system/gc` 本身翻 202，则需 Errata（待裁项，§6 草案留缝） |
| ADR-0018/0019（S3/Backend 缝） | 无冲突——GCS/Azure 留缝即沿其扩展点；`Open` 返回 io.ReadCloser 的降级契约维持 |
| ADR-0031（GC 并发安全） | 无冲突——表 #10 的 INTENTIONAL 即以其为 as-built 锚 |
| ADR-0036（binstore.yaml） | 无冲突——模板条款维持有效且被 U-STG-01 证据强化（#23） |
| ADR-0039（MPU 翻转） | 无冲突——已裁面不重开（#20） |
| ADR-0040（dual-write fail-open） | 无冲突——#13 以其为 eventual/retry 的等价能力锚 |
| ADR-0002/0004（单体/部署矩阵） | HA UNSUPPORTED 待裁即部署矩阵问题（#17），上交用户 |

## 6. ADR 草案位（建议稿——正式 ADR 归 DECISIONS.md 流程）

> **ADR-00YY: 存储链兼容终案——核心链 KEEP、GC 非事件驱动与形态差异 INTENTIONAL、云链裁剪**
>
> - 状态: Proposed（本建议稿；编号由 conductor/DECISIONS.md 流程分配）
> - 背景: U-STG-01 partial 取证（binary-provider-chain.md，B/A/C 三证据基）落地后，gap 总账 storage-filestore 的候选倾向（KEEP+REFACTOR）需终案；known-divergence INTENTIONAL 登记需 ADR authority。
> - 候选方案: ① GC 形态 A 事件驱动管道对齐 / B mark-sweep + hold + Live 维持（§1 #10）；② 云链 A 逐层复刻 cache-fs/eventual/retry / B dual-write+fail-open 等价能力维持（#13）；③ 寻址 A 迁 sha1 对齐 / B sha256 维持（#6）；④ provider 广度 A 全模板族 / B S3 已立 + GCS/Azure/HA 留缝待产品裁定（#15-#17）。
> - 决策: **B/B/B/B**——存储域终案 KEEP 主体 + 两小面 REFACTOR（prune/gc 管理面 P2、包型检索窗 P1〔remote-cache-v2 域〕）+ 五项 INTENTIONAL（sha256 寻址、uploads/ 暂存形态、GC 非事件驱动、不复刻 UNKNOWN 资格窗、云链不逐层复刻）+ UNSUPPORTED 清单（GCS/Azure 留缝、归档/sharding、HA〔待用户裁定〕、full-db）+ UNKNOWN 挂账（云重定向等取证后再裁）；REWRITE 无。storage.Engine/Backend/Session/GCMarker 接口零改动。
> - 理由: §1 逐行证据链（B 活体优先、UNKNOWN 不猜测）；§41 等价能力原则——24 模板矩阵是 Java 类层次表达，BinFlow 已有等价物的面不复刻形态；数据完整性 P0 面全 KEEP（§2）。
> - 后果: known-divergence INTENTIONAL 条目可引本 ADR 为 authority；prune/gc 规格票（compatibility-engineer）与实现票（P2）入池；HA/GCS/Azure 进产品裁定清单（conductor 转用户）；架构文档 §11 技术债台账建议增补三行（云重定向 UNKNOWN、tempFolder 配额 P3、S3 exists-check 核对项）——回写归 conductor 派票。**验证载体**：存储票验收锚维持 corruption/concurrency/recovery 三门（看板硬门）+ 双系统 REST 对拍（prune/gc 面以 curl 对拍 :8082）。

## 7. 遗留与移交

1. 表 #9 prune/gc 面的**响应字面量**未经运行时差分复核（G3 JSON 结构已活体取证，但 BinFlow 侧实现前的契约冻结归规格票）——移交 compatibility-engineer。
2. 表 #18 云重定向、#19 S3 exists-check：MinIO 差分票候选（binary-provider-chain §8-4 同源挂账）。
3. HA（#17）：gap 总账 NEW-BUILD 候选 ha 的产品裁定——上交 conductor 转用户；裁定进目标则另立架构票（wire 协议取证 H1 前置）。
4. 架构文档回写建议（本票不写 architecture.md）：§4 存储引擎节追加「v2 裁定引用本设计」一行 + §11 技术债三行（§6 后果段）——归 conductor 派票。
5. db-persistence 域（gap 总账 KEEP）不受本终案影响；多方言目标维持「Phase 1 产品裁定（Derby/PostgreSQL 两档倾向）」原判。
