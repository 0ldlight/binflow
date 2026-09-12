# E4 活体实验记录 — U-STG-01 BinaryProvider 存储链（:8082 / 7.161.20）

> 环境：docker 容器 artifactory（releases-docker.jfrog.io/jfrog/artifactory-pro:7.161.20），PostgreSQL 16.8 元数据库。
> 临时仓：`audit-probe-stg01`（local generic，实验后已删除）。
> 时间：2026-09-10 18:29–18:39 UTC（容器内时间）。凭据仅命令行使用，未落盘。
> 注意：实例为共享参照实例——实验窗口内有并行会话的 docker 拉取写入 audit-probe-docker-remote-cache（blob 数 5→9 的增量 4 个属于该会话，与本实验无关，已用 nodes 表验明）。

## E4-1 基线上传 → blob 落位

```
$ printf 'u-stg-01 binary provider forensics payload v1\n' > /tmp/e4-payload.txt
$ sha1sum /tmp/e4-payload.txt
1274dc4073fc5c5c139ec44ef15f9861f5838401   # sha256=0ca27786925eaec2c45437ef00d6f2946959a27143ac989ca84489e70375453f
$ curl -u admin:*** -X PUT --data-binary @/tmp/e4-payload.txt -D - -o /dev/null \
    http://localhost:8082/artifactory/audit-probe-stg01/e4/dup-a.txt
HTTP/1.1 201 Created
X-Checksum-Sha256: 0ca27786925eaec2c45437ef00d6f2946959a27143ac989ca84489e70375453f

$ docker exec artifactory find /var/opt/jfrog/artifactory/data/artifactory/filestore -type f
.../filestore/12/1274dc4073fc5c5c139ec44ef15f9861f5838401   ← 新增，内容 sha1 与文件名一致（cat|sha1sum 核对相等）
$ docker exec artifactory ls /var/opt/jfrog/artifactory/data/artifactory/filestore/_pre | wc -l
0
```

观察：blob = `filestore/<sha1前2字符>/<sha1 全量40字符>`，无扩展名；_pre 静置为空。

## E4-2 去重（三条路径共享一个 blob）

```
# (a) 同内容第二路径全量上传
$ curl -X PUT --data-binary @/tmp/e4-payload.txt .../audit-probe-stg01/e4/dup-b.txt
HTTP/1.1 201 Created
blob count: 5  ← 未新增

# (b) checksum-deploy（仅 query 参数 ?sha1=...&sha256=... → 400）
{"errors":[{"status":400,"message":"Checksum deploy failed. no checksum header 'X-Checksum-Sha1/X-Checksum-Sha256' was found."}]}
# (c) checksum-deploy（正确姿势：X-Checksum-Deploy + X-Checksum-Sha1/X-Checksum-Sha256 请求头）
HTTP/1.1 201（响应含 checksums/originalChecksums 全量 JSON，零字节传输）
blob count: 5  ← 仍未新增

$ curl .../api/search/checksum?sha1=1274dc...
→ dup-a.txt、dup-b.txt（两个 URI；dup-c 亦命中，共 3 节点）
```

观察：同一 sha1 三节点（全量/重传/checksum-deploy）= 1 个物理 blob，无硬链接副本（文件数不变）。DB 侧三行 nodes.sha1_actual 同指一行 binaries。

## E4-3 删除 → blob 保留 + 回收站副本

```
$ curl -X DELETE .../audit-probe-stg01/e4/dup-a.txt → 204
$ ls filestore/12/ → 1274dc...仍在（46 bytes）
$ checksum search → 2 hits（dup-b、dup-c 仍可检索）
$ GET .../api/storage/auto-trashcan/audit-probe-stg01 → 200（回收站副本节点存在）
```

DB 证据（psql 只读）：
```
nodes WHERE repo='auto-trashcan' AND node_type=1:
 auto-trashcan | audit-probe-stg01/e4 | dup-a.txt | sha1_actual=1274dc...
 auto-trashcan | audit-probe-stg01/e4 | dup-b.txt | 1274dc...
 auto-trashcan | audit-probe-stg01/e4 | dup-c.txt | 1274dc...
node_props: trash.deletedBy=admin / trash.originalRepository / trash.originalRepositoryType=generic / trash.originalPath / trash.time=<epoch ms>
```

## E4-4 GC/prune 回收链（关键观察：短窗口内不回收）

```
# 删光 dup-b/dup-c → blob 仍在，checksum search 0 hits，binaries 行仍在
# 删除回收站内三节点（DELETE /auto-trashcan/audit-probe-stg01/e4/dup-{a,b,c}.txt → 204×3）
# nodes 表确认：sha1_actual=1274... 的引用数 = 0（LEFT JOIN 计数）
# 引用计数快照：
  1274dc...→0 refs, 547b...→0, 7058...→0, ea57...→0, fef8...→1(auto-trashcan hello.txt)

$ curl -X POST .../api/system/storage/prune/start → 202 {"info":"Pruning Unreferenced Data task has been submitted"}
$ GET .../api/system/storage/prune/status
{"status":"finished","dryRun":false,"timing":{...durationMillis 4189...},
 "progress":"256 of 256",
 "report":{"totalBinariesProcessed":3,"totalBinariesCleaned":0,"totalBytesCleaned":0},
 "lastHandledDirectory":{"name":"ff","status":"finished","binariesProcessed":0,...}}

$ curl -X POST .../api/system/storage/gc → 200（body "."）
日志：FilestorePruner 逐目录 "No files were pruned from dir <00..ff>"（256 个分片目录遍历）
随后 MinorGcCollector: Starting GC strategy 'TRASH_AND_BINARIES' → 空报告结束
结果：binaries 仍 5 行、磁盘仍 5 文件——零引用 blob 在删除后数分钟窗口内不被 prune/GC 回收
```

## E4-5 上传原子性（_pre 暂存与中断回滚）

```
# 30MB 随机文件，curl --limit-rate 2M --max-time 6（6 秒后掐断）
$ 飞行中（上传开始 ~3s 时）：
filestore/_pre/dbRecord3887003055415958854-2d019ba930ccdcc64dc945a4e5c14c2d-audit-probe-stg01.bin  (6586368 bytes)
$ 中断后 ~8s：
_pre 为空；tmp 无文件；blob count 不变；GET big.bin → 404

# 成功路径对照（30MB，--limit-rate 4M）：
飞行中：_pre/dbRecord7949358882377734970-5cce92e2225148f7659c400f599d9a99-audit-probe-stg01.bin
完成后：_pre 为空；filestore/b5/b5f00bef66fb7624a7d97dbd5c8531b883c1076e（31457280 bytes = sha1(内容)）
PUT → 201
```

观察：直接 PUT 的 body 流式写入 `filestore/_pre/dbRecord<19位数字>-<32位hex>-<repoKey>.bin`（本例未经过 tmp/artifactory-uploads）；完成即消失并出现最终 sha1 blob；客户端掐断后服务端数秒内清除暂存文件，不留节点、不留 blob。

## E4-6 checksum 不匹配拒绝

```
$ curl -X PUT -H "X-Checksum-Sha1: 0000000000000000000000000000000000000000" --data-binary @payload .../e4/mismatch.txt
HTTP/1.1 409 Conflict
{"errors":[{"message":"Checksum policy 'LocalRepoChecksumPolicy:CLIENT' rejected the artifact 'audit-probe-stg01:e4/mismatch.txt'.
 Checksums info: ChecksumsInfo{checksums={SHA-1=ChecksumInfo{type=SHA-1, original='0000...', actual='1274dc...'}, ...}}"}]}
结果：无新 blob、无 _pre/tmp 残留、节点未创建
```

## 附：GC 调度与 DB 表快照

- qrtz 调度表存在 GC 任务行（job 名与 cron 未逐条展开——见规格 UNKNOWN）。
- `binaries` 表无时间列（sha1/md5/bin_length/sha256）→ 无行级宽限字段。
- `binary_blobs`（full-db 模板目标表）：sha1 PK + data bytea，本实例为空（file-system 模板不写）。
- `storage_multipart_uploads`（MPU 会话表）：upload_id PK / created_date / temp_path / status(varchar20) / modified_date / data json；本实例为空（MPU 默认禁用）。
- GC 管道相关表：node_events（29 行，event_type 1/3/4）、node_events_tmp（分区表 p0_5/p6_11/p12_17/p18_23）、node_events_errors、node_event_cursor、nodes_cleaned（空）、binaries_tasks（空）。

## 收尾状态

audit-probe-stg01 仓已删除（200），回收站 audit-probe-stg01 前缀已清（204）。实验 blob（1274dc…/b5f00b…）作为零引用 binaries 行留存磁盘，与实验前既存的 3 个零引用 docker 层 blob 同状态，等待实例自身 GC 周期回收。
