---
title: S3 对象存储后端与在线迁移
sidebar_position: 53
---

# S3 对象存储后端与在线迁移

> 适用版本：M6（`storage.backend=s3` + `storage.s3` 段，T-151/T-152/T-178；本地→S3 在线迁移 T-164/T-160/T-177）。
> 配置键与校验逐项核对 `internal/config/load.go`/`validate.go`；`/healthz` 探测语义核对 `internal/httpapi/system.go`；compose `--profile s3` 全链（建桶→healthy→roundtrip）由 T-170 烟测背书。

BinFlow 的 blob 引擎可在**本地磁盘**（默认）与 **S3 兼容对象存储**（AWS S3、MinIO 等）之间选择；元数据仍在本机（SQLite/postgres DSN），切后端**不迁移元数据**。已运行的磁盘实例可走**在线双写迁移**无痛切到 S3。

## 配置

`binflow.yaml`：

```yaml
storage:
  backend: s3
  data_dir: /var/lib/binflow        # 迁移期与元数据仍用它；纯 S3 模式下只放 SQLite
  s3:
    bucket: binflow                 # 桶必须已存在——BinFlow 不建桶
    region: us-east-1
    endpoint: https://s3.amazonaws.com   # MinIO 用 http://minio:9000 + use_path_style: true
    access_key_id: AKIA...
    use_path_style: false           # MinIO/自建 true；AWS 虚拟主机风格 false
    # bucket_prefix: binflow-prod   # 可选：所有对象加统一前缀（多实例共桶）
    # upload_part_size: 5242880     # 分片大小，缺省 5 MiB
    # upload_concurrency: 4         # 并发分片数，缺省 4
```

secret key 走环境变量，**写进 YAML 会拒启**：

```bash
export BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY=<secret-access-key>
binflow-server serve -c binflow.yaml
```

| 键 | 必填（backend=s3 时） | 缺省 | 说明 |
|---|---|---|---|
| `storage.backend` | — | `disk` | `disk` \| `s3` |
| `storage.s3.bucket` | 是 | — | 桶名；不存在则启动拒绝（BucketExists 预检） |
| `storage.s3.region` | 是 | — | AWS region 或 MinIO 兼容拼写 |
| `storage.s3.endpoint` | 是 | — | 带 scheme；`http://` 走明文、`https://` 走 TLS、无 scheme 按 TLS |
| `storage.s3.access_key_id` | 是 | — | 访问键 ID |
| `storage.s3.secret_access_key` | 是 | — | **仅 env**：`BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY` |
| `storage.s3.use_path_style` | 否 | `false` | MinIO 必须 true |
| `storage.s3.bucket_prefix` | 否 | 空 | 统一对象前缀 |
| `storage.s3.upload_part_size` | 否 | `5242880`（5 MiB） | 正整数 |
| `storage.s3.upload_concurrency` | 否 | `4` | 正整数 |

env 覆盖（双下划线拼写映射 `storage.s3.*`）：

```bash
BINFLOW_STORAGE__BACKEND=s3
BINFLOW_STORAGE__S3__BUCKET=binflow
BINFLOW_STORAGE__S3__REGION=us-east-1
BINFLOW_STORAGE__S3__ENDPOINT=http://minio:9000
BINFLOW_STORAGE__S3__ACCESS_KEY_ID=binflow
BINFLOW_STORAGE__S3__USE_PATH_STYLE=true
BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY=<secret>   # 唯一单下划线特例
```

## 验证

启动日志出现 `s3 storage backend active bucket=... endpoint=...`。健康面语义：

- `GET /healthz`、`GET /readyz` 的 storage 子系统对 S3 做 **HeadBucket → PutObject（哨兵）→ DeleteObject** 三连——桶存在、可写、可删全过才算 ok。
- endpoint 的 scheme 决定 TLS：`http://minio:9000` 明文、`https://...` TLS、不带 scheme 默认 TLS。scheme 与拨号姿势不一致 historically 会让 `/readyz` 永久 503（T-178 已修），如今按上表自动对齐。

```bash
curl -s $BASE/binflow/api/v1/health | python3 -m json.tool
# "storage": {"status":"ok"} — 三连探测通过
```

## docker-compose（--profile s3）

自带 MinIO + 一次性建桶任务，BinFlow 等 MinIO healthy 且建桶成功后才起：

```bash
cd <repo-root>
cp deploy/compose/.env.example deploy/compose/.env
# .env 里设置：
#   MINIO_ROOT_PASSWORD=<minio-root-password>          # 必填
#   BINFLOW_STORAGE__BACKEND=s3
#   BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY=<同 MINIO_ROOT_PASSWORD>
#   BINFLOW_ADMIN_PASSWORD=<...>
docker compose -f deploy/compose/docker-compose.yml --profile s3 up -d --build
```

默认桶名 `binflow`（`BINFLOW_STORAGE__S3__BUCKET` 可改，建桶任务自动跟随）。MinIO 控制台发布在 `127.0.0.1:9001`（仅回环）。对象在 `minio-data` 卷——`down` 保留，`down -v` 才销毁。

## Helm Chart

```yaml
config:
  s3:
    enabled: true
    bucket: binflow
    region: us-east-1
    endpoint: http://minio.minio.svc.cluster.local:9000
    accessKeyId: binflow
    usePathStyle: true
    existingSecret: binflow-s3   # Secret 需含 BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY 键
```

```bash
kubectl create secret generic binflow-s3 \
  --from-literal=BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY=<key>
```

## 本地 → S3 在线迁移

三段式：**双写 → 后台搬 → 收口单写**。全程服务在线。

```yaml
storage:
  backend: s3
  data_dir: /var/lib/binflow       # 磁盘侧原地保留
  s3: { ... }
  migration:
    enabled: true                  # 双写模式：写双份，读 S3 优先回落磁盘，删双删，GC 双侧
    completed: false               # 迁移收尾后手工改 true
    concurrency: 5                 # 后台搬运协程数（缺省 5）
```

启动即入双写（日志 `storage migration dual-write mode`）。随后在**控制台 → 治理 → 垃圾回收页的「存储迁移」面板**点「启动迁移」（危险面，二次确认），或走 API：

```bash
# 启动（admin；幂等——运行中再调返回当前进度，不报错）
curl -su admin:$PW -X POST $BASE/binflow/api/v1/storage/migration/start
# {"running":true,"total":927,"migrated":0,...}

# 进度（5s 轮询同款）
curl -su admin:$PW $BASE/binflow/api/v1/storage/migration
# {"running":false,"done":true,"total":927,"migrated":925,"skipped":2,"failed":0,
#  "started_at":"...","finished_at":"..."}
```

字段语义：`total` 是**本轮待搬数**（磁盘有、S3 没有），不是全量 blob 数；已在 S3 的记 `skipped`；重启后重扫，已搬过的不再计入——断点天然安全，进度条按 `(migrated+failed)/total` 收敛。

收尾三步：

1. `done:true` 且 `failed:0` 后，把 `storage.migration.completed: true` 写进 `binflow.yaml`（**手工改配置**，没有端点代改），重启。
2. 日志出现 `storage migration completed; s3 is the source of truth`——此后纯 S3 单写。
3. 磁盘侧 `blobs/` 退役前先做一次备份（见[备份手册](../admin/backup-restore.md)），确认无误再清。

约束与坑：

- `migration.enabled: true` 必须**同时** `backend: s3`——磁盘后端下开迁移直接拒启（双写目标就是 S3）。
- 迁移期是双写：磁盘空间只增不减，S3 侧失败会回滚该次上传（不留半截对象）。
- `failed` 非 0 时进度面板会显示，失败项在**重跑**（再次 start）时重扫重试。
- `/metrics` 的 `binflow_storage_blob_bytes{engine="s3"}` 是**逻辑字节**（配额口径的仓用量合计），与 `engine="disk"` 的物理目录大小语义不同。

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 启动报 `storage.s3.bucket is required...` 系列 | backend=s3 但必填缺失 | 按表补齐；secret 看 env |
| 启动报 `s3 ... bucket does not exist` | 桶未预建 | 先建桶（compose 的 minio-init / `mc mb` / 控制台） |
| `/readyz` 503 + `s3 PutObject ...` | 键无写权限 / endpoint 拼错 | 核对 access key 权限与 scheme |
| 启动报 `key "storage.s3.secret_access_key" looks like a secret` | secret 写进了 YAML | 移到 env |
| 启动报 `storage.migration.enabled requires storage.backend=s3` | 迁移开着却是磁盘后端 | 补 `backend: s3` |
| MinIO 明文 endpoint 报 scheme 冲突 | 旧二进制 Secure 写死（T-178 前） | 升级到修复后的构建 |

## 下一步

- 备份与恢复（export/import 与迁移期的关系）：[备份手册](../admin/backup-restore.md)
- 治理面板其余功能：[治理指南](../admin/governance.md)
- 指标（engine 标签口径）：[Prometheus 指标参考](../metrics/prometheus-reference.md)
