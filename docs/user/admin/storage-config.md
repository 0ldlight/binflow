---
title: 存储配置（binstore.yaml）
sidebar_position: 49
---

# 存储配置（binstore.yaml）

> 适用版本：M11（T-306 交付；设计依据 ADR-0036，行为基准 `docs/reverse/config-formats.md` §1 复核版）+ **M12 增补**（T-338：dual-write 停机窗 **fail-open**——ADR-0040；证据取 T-338 真 MinIO docker stop/start 全链实测）。本文的启动 INFO/WARN 与四类拒启报错均在 HEAD 构建二进制上真实 boot 实测（2026-08-28）；`[s3]` / `[filestore, s3]` 双写链的 roundtrip 证据取自 T-306 的 MinIO 容器栈实测（`reports/agents/T-306.md` §自测）。
> 与 [S3 指南](../guides/s3-config.md)的分工：S3 指南讲内嵌 `storage.s3` 段与在线迁移操作流（M6 交付、继续有效）；本文讲 M11 的**独立存储链配置文件**——新部署建议直接用 binstore.yaml。

`binstore.yaml` 把 blob 存储链从主配置里解耦出来（对标 Artifactory 的 `binarystore.xml`，YAML 载体为 BinFlow 自有拼写）：

- **位置**：与**实际生效的主配置文件同目录**（跟随 `-c` 解析序：`./binflow.yaml` → `$BINFLOW_HOME/binflow.yaml`；默认部署下即 `$BINFLOW_HOME/binstore.yaml`）。无新 flag、无新 env。
- **文件缺席 = 零行为变化**：存量实例升级后什么都不用做。

## schema

```yaml
# binstore.yaml —— 与 binflow.yaml 同目录
version: 1                  # 可选；出现必须为 1
chain:                      # 有序 provider 链，声明序即链序
  - type: filestore         # 本地盘 provider（M11 无参数）
  - type: s3                # 参数 = 内嵌 storage.s3 键族平移：
    bucket: my-bucket
    region: us-east-1
    endpoint: http://minio.local:9000
    access_key_id: minioadmin
    # use_path_style / upload_part_size / upload_concurrency / bucket_prefix 同内嵌拼写
    # secret_access_key 永远不进文件 —— 只认 env（见下）
migration:                  # 仅 [filestore, s3] 双员链合法且必填
  mode: dual-write          # 闭集 bypass | dual-write | completed
  concurrency: 5
```

合法链形恰三种（其余序、重复、未知 type 一律拒启）：

| 链 | 等价内嵌形态 | 语义 |
|---|---|---|
| `[filestore]` | `backend: disk` | 本地盘 |
| `[s3]` | `backend: s3` | 纯 S3（含迁移完成后的终态） |
| `[filestore, s3]` + mode | `storage.migration` 三模式 | `bypass`=仅 disk 生效（S3 参数前置声明）；`dual-write`=双写 + 读 S3 先磁盘兜底 + 后台拷贝；`completed`=S3 单写 |

`type` 之外的保留名 `cache-fs` / `azure` / `gs`：出现即**拒启**，文案点名「保留位未实现」——不静默忽略、不假装支持。

## 启动观测

链解析成功打一行 INFO（provider 序 + mode + 来源 + 凭据 redact），实例 8329 实测：

```
INFO msg=storage chain resolved providers=filestore mode="" source=/etc/binflow/binstore.yaml
```

内嵌段来源的等价链标注 `source=embedded`。

## 三分支并存裁决（binstore.yaml × 内嵌链键）

判定对象是**链键**（内嵌 `storage.backend` / `storage.s3` / `storage.migration`——即 binstore.yaml 能表达的全部内容；`data_dir`、`gc_*` 等恒以 binflow.yaml 为准，不参与）：

| 分支 | 形态 | 行为 |
|---|---|---|
| ① | 无 binstore.yaml + 内嵌链键显式设置 | 照常启动 + **每次启动 WARN**（兼容窗迁移提示，存量实例的默认态） |
| ② | binstore.yaml 在 + 内嵌链键缺席或全默认 | 文件生效，静默（干净形态） |
| ③ | binstore.yaml 在 + 内嵌链键**语义等价**（归一化后同链同参，非文本比较） | 文件生效 + WARN（迁移提示） |
| ③′ | binstore.yaml 在 + 内嵌链键**语义分歧**（会装配出不同的链） | **拒启**——错误指明两处来源文件与分歧键 |

注意：显式拼写默认值也算「显式」——`backend: disk`（明明是默认值）+ binstore 声明 `[s3]` → 分歧拒启，不是静默吞并。

分支① 的实测 WARN（内嵌 `storage.backend: s3`、无 binstore.yaml）：

```
WARN msg=config: the storage chain is configured through binflow.yaml's embedded
  storage section (storage.backend/storage.s3/storage.migration) with no binstore.yaml
  next to the main configuration file; the existing spelling stays in effect for this
  compatibility window (ADR-0036) — consider moving it to a binstore.yaml
```

## 凭据纪律与 env 优先级

- **明文 secret 恒拒启**：全文件递归扫描，`secret_access_key` 或 secret 形键出现 → 拒启 + 行号 + env 逃生口。S3 secret 只认 `BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY`，**恒注入生效链的 S3 provider**（即使其它链相关 env 被忽略）。
- **binstore.yaml 生效时**：链相关 env 覆盖键（`BINFLOW_STORAGE__BACKEND` 与 `storage.s3.*` / `storage.migration.*` 族）**忽略 + WARN**——部署管道遗留值不阻塞升级；secret env 例外（恒生效）。
- 文件权限宽于 0600 → 启动 WARN（非拒启；文件内本无 secret，0600 为部署卫生建议）：

```
WARN msg=config: binstore.yaml /etc/binflow/binstore.yaml: file permissions are wider
  than 0600 (0644); tighten to 0600 (hygiene — the file must carry no secrets by policy)
```

## fail-fast 四形态（实测报错原文）

以下任一形态直接拒启（exit 1），错误含**绝对路径 + 行号 + 违规键名**：

```console
# 1. YAML 语法错
binflow-server: config: binstore.yaml /etc/binflow/binstore.yaml: yaml: line 3: did not find expected key

# 2. 明文 secret（行号指向违规键）
binflow-server: config: binstore.yaml /etc/binflow/binstore.yaml: line 6: key
  "chain.secret_access_key" looks like a secret; secrets must not be written into
  binstore.yaml — use the environment variable BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY instead

# 3. 保留名 provider
binflow-server: config: binstore.yaml /etc/binflow/binstore.yaml: line 3: provider type
  "cache-fs" is a reserved slot this BinFlow build does not implement — refusing to
  start rather than pretending to support it; remove it from the chain
  (reserved slots: cache-fs, azure, gs)

# 4. 双源分歧
binflow-server: config: refusing to start: binstore.yaml /etc/binflow/binstore.yaml and
  the embedded storage section in /etc/binflow/binflow.yaml declare storage chains that
  would assemble differently (the S3 provider is live on one side only); align the two
  files or delete the legacy embedded storage.backend/storage.s3/storage.migration keys
  from /etc/binflow/binflow.yaml
```

未知键、重复键、类型错、非法链形、migration 块误配（如单员链带 migration、双员链缺 migration）同族拒启。

## 升级与迁移路径

| 场景 | 动作 |
|---|---|
| 存量内嵌段实例 | 什么都不做（分支①，每启 WARN 提示）；想收口 WARN → 建等价 binstore.yaml 并删内嵌链键 |
| 搬迁到 binstore.yaml | 写文件 → **删除** binflow.yaml 里的链键（留着且等价 = 每启 WARN；不等价 = 拒启） |
| 在线迁移磁盘 → S3 | 双写链 `[filestore, s3] + mode: dual-write`；`/api/v1/storage/migration/start|status` 操作流不变（API 面零变化，系统不改写 binstore.yaml——迁移完成态由运维声明改 `mode: completed`） |

三链 roundtrip 证据（T-306 MinIO 容器栈 + 真实二进制 + curl）：filestore PUT/GET sha256 对账 + 磁盘落点对账；s3 bucket-only 姿态（无本地 blob）；dual-write 双端同 sha256 + 启动日志 `storage migration dual-write mode`。

## dual-write 停机窗 fail-open（M12，ADR-0040）

`[filestore, s3] + mode: dual-write` 链在 **S3 停机/故障窗内的行为（M12 起生效）**：

| 面 | 停机窗内行为 |
|---|---|
| 写（PUT/MPU 分片/remote 落盘，单缝全覆盖） | **本地优先落盘成功**（disk-only 快路，不再拨 S3）+ 落盘内容入重放队列 `<data>/replay-queue/<sha256>.json`（per-sha 幂等——同内容重复 PUT 不增队列深度）。旧版行为（PUT 500 且磁盘零落盘）废止；**已落盘的 blob 永不因 S3 失败被回滚删除**（旧版 Commit 臂失败反删盘的条款废止） |
| 读（GET/HEAD/Stat，含 checksum-deploy） | **回退 disk 探测**：S3 任何错误（不止 not-found）都回退读磁盘——disk 恒为超集（迁移方向 disk→S3 且 disk 副本不自动删），读己之写保持；旧版行为（存量件 GET 500）废止 |
| 治理面（GC/Delete） | **诚实失败维持**——fail-open 只覆盖数据面 PUT/GET；管理面可重试，静默吞错反而掩盖 S3 故障 |
| 开窗判定 | 进程内断路器：S3 臂任一操作失败（连接拒绝/超时/5xx/权限错，二分不做错误分类学）即开窗；S3 正常应答的 miss（迁移滞后）不开窗 |
| 恢复 | 首个成功拷贝即关窗 → 立即排空队列（并发 = `migration.concurrency`，指数退避 1s×2 上限 5min）→ 排空后 **diskList × s3Set 存在性对账**（≤3 轮收敛兜底孤儿 blob）；恢复后稳态回到同步双写 |
| 重启幸存 | 队列是磁盘文件——窗口内置债 → 重启 → 启动水位 INFO 一行（`storage: migration dual-write replay queue watermark depth=N drain=scheduled`）→ S3 恢复后 1 秒级排空（实测） |

- **S3 侧滞后量 = 窗口时长 + 排空时长**：带外直读 S3 桶会看到旧集合——**S3 桶是引擎私有物**，带外读不在契约内。
- 无死信删除：条目只在「确认 S3 已有」或「源 blob 已消失」时移除（后者记 `source_gone`）；连续 16 轮全失败 → permanent 记账（WARN + 指标），条目保留等修复后重试。人锤 = `POST /api/v1/storage/migration/start` 幂等重扫。
- **`mode: completed` 且 replay-queue 非空 → 拒启**（错误指明两条出路：切回 dual-write 排空，或人工确认弃队列后删目录）——「声明迁移完成但数据没到」正是要防的数据可见性事故。
- `bypass` 模式零新代码路径（S3 不在链上，行为不变）；`migration status` 端点形状**逐字节不变**（六键 `{"running","done","total","migrated","skipped","failed"}`——队列状态走指标与日志）。
- 可观测（`/metrics`，dual-write 实例才有）：`binflow_replay_queue_depth` / `binflow_replay_window_open` / `binflow_replay_drained_total` / `binflow_replay_failed_retry_total` / `binflow_replay_failed_permanent_total` / `binflow_replay_source_gone_total` / `binflow_replay_read_fallback_total`；审计事件 `storage.replay.window{phase:open|close}` 与 `storage.replay.drained`。
- 实测锚（T-338，真 MinIO docker stop/start + 真二进制）：停机窗内 2MiB PUT = 201 且 disk 落盘 + 队列在案；GET 新件与窗口前存量件均 200；MinIO 恢复后 1 秒关窗排空（`drained=5 rounds=1`，含对账抓到的存量），mc 侧逐对象 sha256 零缺；迁移中上传 = 201，status `skipped=12`（排空先收敛、扫描幂等跳过）。

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 拒启 `looks like a secret` | 文件里写了 `secret_access_key` | 移到 `BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY` |
| 拒启 `would assemble differently` | binstore.yaml 与内嵌链键分歧 | 对齐两文件或删内嵌链键 |
| 拒启 `reserved slot` | `cache-fs`/`azure`/`gs` | 从链中移除（本 build 未实现） |
| 每启 WARN `compatibility window` | 分支①：还在用内嵌链键 | 搬迁到 binstore.yaml 收口 |
| env 覆盖不生效 | binstore.yaml 生效时链相关 env 被忽略 | 改 binstore.yaml（secret env 除外） |
| 拒启 `completed` + replay queue 非空 | 切了 `mode: completed` 但停机窗置的债未排 | 切回 dual-write 排空，或确认可弃后删 `<data>/replay-queue/` |
| dual-write 下 S3 故障期 PUT/GET 仍 200（日志有 `opening fail-open window`） | M12 fail-open 行为（本地优先 + 队列重放） | 无需处置；观察 `binflow_replay_*` 指标等 S3 恢复自动排空 |
| dual-write 下 S3 故障期 GC/Delete 失败 | 治理面诚实失败（fail-open 不覆盖治理） | 等 S3 恢复重试 |

## 下一步

- S3 参数族与在线迁移操作流：[S3 对象存储后端与在线迁移](../guides/s3-config.md)
- 备份恢复（三链同源零变化）：[备份与恢复手册](backup-restore.md)
