---
title: 存储配置（binstore.yaml）
sidebar_position: 49
---

# 存储配置（binstore.yaml）

> 适用版本：M11（T-306 交付；设计依据 ADR-0036，行为基准 `docs/reverse/config-formats.md` §1 复核版）。本文的启动 INFO/WARN 与四类拒启报错均在 HEAD 构建二进制上真实 boot 实测（2026-08-28）；`[s3]` / `[filestore, s3]` 双写链的 roundtrip 证据取自 T-306 的 MinIO 容器栈实测（`reports/agents/T-306.md` §自测）。
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

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 拒启 `looks like a secret` | 文件里写了 `secret_access_key` | 移到 `BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY` |
| 拒启 `would assemble differently` | binstore.yaml 与内嵌链键分歧 | 对齐两文件或删内嵌链键 |
| 拒启 `reserved slot` | `cache-fs`/`azure`/`gs` | 从链中移除（本 build 未实现） |
| 每启 WARN `compatibility window` | 分支①：还在用内嵌链键 | 搬迁到 binstore.yaml 收口 |
| env 覆盖不生效 | binstore.yaml 生效时链相关 env 被忽略 | 改 binstore.yaml（secret env 除外） |

## 下一步

- S3 参数族与在线迁移操作流：[S3 对象存储后端与在线迁移](../guides/s3-config.md)
- 备份恢复（三链同源零变化）：[备份与恢复手册](backup-restore.md)
