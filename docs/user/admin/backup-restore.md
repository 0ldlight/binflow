---
title: 备份与恢复手册
sidebar_position: 43
---

# 备份与恢复手册

> 适用版本：M4（export/import CLI；PRD milestone-4 v1.2 FR-32/GE-07~09、ADR-0015 及勘误二）。
> 本文命令在本机 scratch 实例（commit `7593d8e`）上完整往返复跑：在线 export exit 0、产物 0700、`--verify full` import exit 0、恢复实例制品/配额/审计全保真、mtime 逐 blob 全等、非空目标 fail-fast、GC 互斥 409（蓝本 T-103 W28~W32，报告见 `reports/agents/T-103-qa.md` §2.8）。

备份恢复只有 CLI（**有意不做 REST**：`/api/export/**`、`/api/import/**` 一律 404——import 是覆盖元数据的高危操作，走带外通道）。export **在线**执行（服务运行中），import **停机**执行且目标必须为空。

## export（在线备份）

```bash
# serve 运行中执行；-c 指向该实例的 binflow.yaml
binflow-server export -c binflow.yaml --output /backup/binflow-20260821
# export: mode=online output=/backup/binflow-20260821 blobs=30 bytes=423091501 ...
```

执行流程（顺序是硬规则，理解产物语义的关键）：

1. 获取 data 目录维护锁（与 GC/import 三方互斥，见[治理指南](governance.md#gc垃圾回收)）；
2. **先 SQLite 在线一致性快照**（VACUUM INTO，不阻塞服务写入）；
3. **后拷贝 `blobs/` 目录**（**保留 mtime**——GC 宽限期以 blob mtime 为基准，mtime 不保真会把整份备份当「新写入」重置宽限时钟）；
4. 写 `manifest.json`。

先 DB 后 blobs 的窗口含义：快照之后新上传的 blob 可能出现在产物里但不在 manifest 引用集中（**多余**文件无害，恢复后由常规 GC 收敛）；顺序反过来则可能出现 DB 引用产物里不存在的 blob（悬空引用，不可接受）——因此该顺序不可换。

产物形态（目录，非压缩包；`--tar` 单文件形态 M4 未实现，显式报 `not implemented in M4`）：

```
/backup/binflow-20260821/        ← 目录权限 0700
├── metadata.db                  ← 元数据快照（0600）
├── manifest.json                ← 0600
└── blobs/<2hex>/<sha256>...     ← mtime 保真的 blob 副本
```

`manifest.json`：`{formatVersion, createdAt, binflowVersion, blobCount, totalBytes, metadata:{file, sha256}, graceNote}`。

> **保管告警（NFR-S22）**：产物**未加密**且包含用户口令哈希与 remote 仓上游凭据密文（`enc:v1:...`）——manifest 与 DB 均无明文秘密，但拿到整份产物等于拿到全部账号体系。产物目录由 CLI 固定建为 **0700**（文件 0600）；转存 NAS/对象存储时请保持最小权限或先行加密，不要放进可公开列举的位置。

`export.run` 落**源实例**审计（注意：本次 export 的事件写入发生在快照之后，因此**不在本份备份内**——恢复出的实例看到的 `export.run` 是更早的历史）。

## import（停机恢复）

```bash
# 前置：目标实例停机 + data 目录为空（非空 → fail-fast 退出码 1）
rm -rf /var/lib/binflow/data && mkdir -p /var/lib/binflow/data
binflow-server import -c binflow.yaml --input /backup/binflow-20260821 --verify full
# import: input=/backup/... blobs=30 bytes=805 verify=full rehashed=30
```

| `--verify` | 校验强度 |
|---|---|
| `spot`（缺省） | manifest 结构与 blobCount/totalBytes 对账 + metadata.db 实测 sha256 + **全部 blob size 校验 + sha256 抽验前 100 个**（sha 序） |
| `full` | 上述 + **全量重哈希每一个 blob**（耗时与数据量成正比；首次恢复到新机器建议 full） |

行为要点（全部 QA 真机验证）：

- **仅空实例**：目标 data dir 非空 → 退出码 1，明示发现的文件（`import restores into an empty directory only; move the existing data away first`）。恢复的正确心智是**重建**，不是合并。
- **无半恢复**：任何校验失败（缺失被引用 blob、篡改 blobCount、metadata 哈希不符、快照 schema 版本高于当前二进制）→ 退出码非 0，目标目录清回空（可能残留 0 字节 `.maintenance.lock` 锁文件，无数据残留）。
- **跨版本**：快照 schema 版本 ≤ 当前二进制即可（恢复后首启自动跑迁移升级）；高于则拒绝。
- **全保真**：四协议制品 sha256 逐位一致；users/groups/permission targets/tokens/审计历史/quota 配置与用量全部随行；`import.run` 落恢复实例审计。
- **`web_sessions` 不入备份**：控制台会话是瞬态——恢复后所有用户**重新登录**（属预期，不是故障）。
- blob mtime 随备份保真（GC 宽限时钟不重置）。

### 恢复链上的 `BINFLOW_REMOTE_CREDENTIALS_KEY`

备份**包含 remote 仓上游凭据密文**（`enc:v1:...` 原样随行）。恢复实例若配置过带密码的 remote 仓，**必须**在启动环境提供同一主密钥：

```
BINFLOW_REMOTE_CREDENTIALS_KEY=<base64 的 32 字节>
```

无钥启动 → **fail-fast 退出码 2**，日志点名修复动作：

```
remote repository "<key>" stores encrypted credentials but no master key is configured:
set BINFLOW_REMOTE_CREDENTIALS_KEY (base64 of exactly 32 bytes) and restart
```

配钥重启后代理链即通（缓存随备份恢复，上游不会因恢复而重拉）。密钥的生成与部署细节见[remote/virtual 管理指南](remote-virtual.md#上游凭据与-binflow_remote_credentials_key)。

## 停机强一致（可选）

在线 export 的一致性由「快照先行 + 窗口内多余 blob 允许」保证，适合常规备份。若业务上有更严格诉求（例如升级前定格），可**停 serve 后执行 export**——窗口闭死，产物与停机时刻完全一致；代价是备份期间实例不可用。两种方式产物形态与 import 流程完全相同。

## 定时备份（实例内调度，免外部 crontab）

除本机 crontab 调 CLI 外，实例可直接配置**到点自动 export**——fire 走与 CLI export 完全相同的内核（维护锁、快照先行序、产物形态、`0700` 权限全部一致），每次触发在 `<exportPath>/<backupKey>-<时间戳>` 子目录产出一份完整产物：

```bash
# 创建定时备份（REST；或控制台 监控 → 备份/恢复 页的「定时备份」卡）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/v1/system/backups \
  -H 'Content-Type: application/json' \
  -d '{"backupKey":"nightly","cronExp":"0 0 2 ? * MON-FRI",
       "exportPath":"/backup/binflow","enabled":true}'
# 200 {"backupKey":"nightly","cronExp":"0 0 2 ? * MON-FRI",
#      "nextScheduleBackup":"2026-09-07T02:00:00Z",...}
```

要点：`exportPath` 必须是**服务器上的绝对路径**（相对路径/`..` → 400）；`nextBackupTime` 可选指定首跑时刻（过去时间 400）；**import 恢复仍是停机 CLI 操作**（本调度只管备份侧）；触发撞上维护锁（手动 GC/export 在跑）记一次失败、下轮再试——排程与 GC 错峰。完整字段表、表达式子集与审计词见 **[计划任务指南 · 定时备份](cron-scheduling.md#定时备份到点-export)**。

## 运维建议

- **周期备份**：两条路任选——**实例内定时备份**（见上节，免外部 crontab）或主机 crontab 低峰执行 export（锁与 GC 互斥，409/退出码非 0 时下轮重试即可）；产物按日期分目录保留多份。
- **恢复演练**：备份的价值取决于可恢复性——定期在空目录上 `import --verify full` 并抽查制品 sha256 与登录链。
- **与 GC 的排程**：export 与 GC 不要同刻触发（互斥会拒绝后到者）；若 cron 窗口重叠，把 GC 排在 export 之后（两域都可用[计划任务](cron-scheduling.md)配置时，直接在表达式上错峰——如备份 02:00、GC `0 0 /4 * * ?` 对齐 04:00 起）。
- 吞吐参考（QA 记录，不设门）：在线 export 约 380~830 MiB/s（400MB 级实例、本地盘）。

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| import 退出码 1 + `data dir ... is not empty` | 目标目录非空 | 清空/换新目录（重建语义） |
| import 退出码非 0 + 点名缺失 sha | 备份缺被引用 blob（拷贝中断/被篡改） | 用完整备份重试；来源不可信时 `--verify full` |
| 启动即退出码 2 + `set BINFLOW_REMOTE_CREDENTIALS_KEY` | 恢复链无主密钥 | 注入 env 重启（见上节） |
| export 退出码非 0 + `data directory is locked` | GC/import 正持锁 | 等待后重试 |
| `--tar` 报 `not implemented in M4` | 单文件产物为 P2 债务 | 用目录形态 |

## 灾难恢复（DR）流程

### 标准恢复（日常备份 → 恢复）

1. 准备新实例的 `binflow.yaml`（指向新 `data` 目录）
2. 停机新实例（如已启动）
3. 清空新 `data` 目录
4. 执行 import：`binflow-server import -c binflow.yaml --input /backup/${date} --verify full`
5. 启动服务：`binflow-server serve -c binflow.yaml`
6. 验证：登录、抽查制品 sha256、确认审计历史可见

### 恢复后验证清单

| 检查项 | 验证命令 | 预期 |
|---|---|---|
| 服务启动 | `curl $BASE/binflow/api/system/ping` | 200 OK |
| 版本信息 | `curl $BASE/binflow/api/system/version` | 返回 version + revision |
| 仓库列表 | `curl -su admin:$PWD $BASE/binflow/api/repositories` | 所有仓库恢复 |
| 制品抽样 | `curl -s -o /dev/null -w '%{http_code}' $BASE/binflow/<repo>/<path>` | 200 |
| 制品 sha256 | `curl -s $BASE/binflow/<repo>/<path> \| shasum -a 256` | 与备份前一致 |
| 用户登录 | `curl -su jane:$PWD $BASE/binflow/api/v1/session -X POST` | 200 |
| 权限生效 | 用非 admin 用户验证其读取范围 | 行为一致 |
| 审计历史 | `curl -su admin:$PWD "$BASE/binflow/api/v1/audit?limit=5"` | events[] 非空 |
| 配额状态 | `curl -su admin:$PWD $BASE/binflow/api/v1/storage/usage/<repo>` | usedBytes 一致 |

### 恢复前的排查

如果恢复实例启动失败：

1. **检查 `BINFLOW_REMOTE_CREDENTIALS_KEY`**：备份含 remote 凭据密文，无密钥 → 启动退出码 2。日志点名缺失密钥的仓库名。
2. **检查 schema 版本**：备份 DB 的 schema 版本必须 ≤ 当前二进制。版本过高 → import 拒绝（不会自动降级）。
3. **检查 data 目录为空**：import 仅接受空目录。已有数据的实例不能合并恢复。
4. **检查备份完整性**：`manifest.json` 的 `blobCount` 与 `blobs/` 目录实际文件数是否一致。

### 备份策略建议

| 频率 | 说明 |
|---|---|
| 每日 | 低峰时段执行 export（实例内[定时备份](#定时备份实例内调度免外部-crontab)或主机 cron；锁与 GC 互斥，失败下轮重试） |
| 每周 | 保留 7 天分日备份，`--verify full` 全量校验 |
| 每版 | 升级前停服 `export`（强一致快照），升级完成验证后再开服 |
| 异地 | 产物目录 `0700`，转存 NAS/对象存储时保持最小权限或先行加密 |

### 恢复演练

备份的价值取决于可恢复性。建议每季度执行一次完整演练：

```bash
# 1. 在空机器上创建演练目录
mkdir -p /recovery-test/data

# 2. 用最新备份恢复
binflow-server import -c recovery.yaml --input /backup/latest --verify full

# 3. 启动演练实例
binflow-server serve -c recovery.yaml &

# 4. 执行验证清单（见上表）
# 5. 停止演练实例
# 6. 清理演练目录
```

### 一致性窗口的含义

在线 export 的产物不是「某一时刻的精确快照」——先 SQLite 快照、后拷贝 blobs。快照之后新上传的 blob 可能出现在产物里但不在 manifest 引用集中（多余文件无害，恢复后由常规 GC 收敛）。这个顺序不可换（反过来会出现 DB 引用不存在 blob 的悬空引用）。

如需精确停机快照，在 export 前停服（`binflow-server serve` 进程停止），产物与停机时刻完全一致。代价是备份期间实例不可用。

## 下一步

- 定时备份的完整配置面与表达式子集：[计划任务（cron 调度）](cron-scheduling.md)
- 维护锁与 GC 的完整语义：[治理指南](governance.md#gc垃圾回收)
- remote 凭据加密与密钥部署：[remote/virtual 管理](remote-virtual.md)
- 恢复后重登/会话语义：[Web 控制台使用指南](../console.md#登录与会话)
