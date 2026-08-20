# T-96 评审报告（视角: correctness）

- reviewer: code-reviewer（双 reviewer 票之一，另一位看架构）
- 对象: commit `75c6d95`（current tree 已含 T-95+T-96）
- 结论: **REQUEST_CHANGES**（blocking 2，non-blocking 7）
- 复验命令与结果：`go build ./...` ✅；`go test -count=1 ./internal/storage/ ./internal/metadata/` ok（20.5s/13.5s）；`go test -count=1 ./cmd/binflow-server/` ok（9.9s）；`go test -count=1 -race -run "TestDataLock|TestManifest|TestCopyBlobs|TestHashFile" ./internal/storage/` ok。另以 /tmp 独立探针程序实证了 B2a 的路径行为（见下）。

## 重点审查区逐项核验（先说对的）

以下为本票核心正确性主张的核验结果，均读过实现+测试，部分补跑：

1. **备份顺序硬规则（ADR-0015 决策 3）— 通过**。`runExport`（cmd/binflow-server/backup.go:142 快照 → :168 blobs 拷贝 → :176-209 manifest）顺序正确；引用集取快照自身（`SnapshotChecksums(snapPath)`，nodes ∪ docker_refs，与 GC mark 同形）；窗口语义成立：引擎侧 blob 物理删除仅 GC 一个入口（engine.go:367-391 注释+实现确认「runtime artifact deletion removes metadata references」），GC 被锁排除后，窗口内只可能多拷（surplus）、不可能悬空；导出侧对引用缺失 fail-fast（backup.go:189-192）。ledger 孤儿随拷不引用有单测（TestExportOnlineArtifacts）。
2. **mtime 保真（ADR-0006 勘误②）— 通过**。`copyFilePreserving`（internal/storage/backup.go:226-259）：OpenFile 带 `info.Mode().Perm()` → io.Copy → Sync → Close → Chtimes(dst, mtime, mtime)。Chtimes 在 close/sync 之后、且之后无任何会重置 mtime 的 chmod（0700/0600 chmod 都只落在目录与 db 上，不落 blob）。无临时文件 rename 支路（直接写目标+fsync），不存在 rename 重置 mtime 的路径。测试：W32 源/恢复 mtime 相等（cmd 级 per-blob 断言 backup_test.go:262-268、478-480）。实践上 blob 由引擎 0600 落盘，umask 不会剥位（0600 与任意 umask 相交仍是 0600）。
3. **锁原语 — 主体通过，残留一处生命周期破绽（B1）**。flock 按 open file description，同进程两个 fd 真互斥，有测试（TestTestDataLockMutualExclusion，storage/backup_test.go:231-266）+ 真机 python flock 双向证据；`LOCK_EX|LOCK_NB` 失败路径无 fd 泄漏（datalock.go:64-71：读 holder → Close → 返回 wrap ErrDataLockHeld）；stale 记录文案以 "last holder" 呈现、不误导（且 EWOULDBLOCK 下当前确有持有者，记录就是现任写的）；Release 幂等、nil 安全。
4. **import 安全矩阵 — 主体通过**。非空目标先于读备份拒绝（backup.go:291-293）；写阶段任何失败 `clearDirContents` 清回空（:326-332，损坏矩阵 8 行断言 assertDirEffectivelyEmpty）；spot-vs-full 边界由 TestSpotVersusFullVerification（101 blob，第 101 个损坏 spot 放行 full 拒）固化；未来 schema（SnapshotSchemaVersion > LatestSchemaVersion，动态取嵌入集顶）与未来 formatVersion（Validate）均拒；metadata.file 裸文件名 + blob 64-hex 校验双防穿越（windows 下 `\` 分隔符同样被 Base() 判非裸名拒绝）。
5. **VACUUM INTO 并发一致性 — 通过**。WAL 源上 VACUUM INTO 产出事务一致快照；并发写下一致性有测试（TestVacuumIntoSnapshotIsConsistentWhileSourceServes）；产物为 rollback-journal 单文件、无 -wal/-shm 伴件（TestVacuumIntoProducesPlainArtifact，且断言 journal_mode=delete）；已存在目标拒重写（SQLite 自身拒 + prepareBackupDir 非空拒 + 测试断言）。
6. **web_sessions purge 时点 — 通过，非 blocker**。`PurgeTransientFromSnapshot(ctx, snapPath)` 作用在**输出目录里的快照文件**（backup.go:153），源库不动；purge 在 hash 之前（manifest 的 metadata.sha256 是 purge 后的字节），rw 打开不带 WAL pragma、不留伴件（测试断言 -journal 亦无）。
7. **enc:v1 恢复链 — 通过（基线复用）**。import 路径只 `metadata.Open`，不实例化 repo/remote，不解密——无钥 fail-fast 留在 serve 启动面（M3 T-66 基线，W30b 真机证据在自测日志），实现点正确（fail-fast 先于任何 remote 解密/ repo.New）。

## 必须修改（blocking）

### B1 — import 不参与数据锁，且失败清理会 unlink 活锁文件（锁生命周期破绽）

- 位置：`cmd/binflow-server/backup.go:291-293`（import 全程不 `AcquireDataLock`）、`:509-520`（`clearDirContents` 无差别 `os.RemoveAll`，包含 `.maintenance.lock`）、对照 `internal/storage/datalock.go:31-35`（自身注释明言「removing it would race the next acquirer — unlink-while-waiting is the classic flock lifecycle bug」）。
- 失败场景：目标 data dir 只含残留 `.maintenance.lock`（`requireEmptyDataDir` 明确豁免它，说明该形态是被预期的——gc 在新目录上跑过一次即如此）。此时另一管理员/定时任务对该目录跑 `gc --apply`（gc 在空目录上会照常取锁并 `OpenEngine`+`metadata.Open` 建出 binflow.db），import 的空目录检查放行 → 两者并发：a) import 写阶段失败时 `clearDirContents` 把**正在被持有**的锁文件 unlink，下一个获取者在新建的 inode 上加锁，互斥对第三方静默失效——这正是 datalock.go 注释点名要避免的经典 bug，而本票自己的清理路径在做这件事；b) gc 与 import 的写阶段交叠（gc 的 O_TRUNC binflow.db 与 import 的 copyFile 互踩、gc mark 读到半恢复库），行为未定义。
- 修复建议：`runImport` 对目标目录取 `storage.AcquireDataLock(cfg.Storage.DataDir, "import")`，至少覆盖写阶段（整程持锁最简，且 import 本就是 data 目录级维护操作，与原语语义一致；`requireEmptyDataDir`/`assertDirEffectivelyEmpty` 已豁免锁文件，天然兼容）；同时 `clearDirContents` 跳过 `maintenanceLockName` 而非删除（与 datalock.go 的生命周期注释对齐）。两处均为小改动。

### B2 — import 对「非 sqlite driver / dsn 指向 data 目录之外」无守卫：越界写、覆盖既有库、半恢复残留逃逸清理

- 位置：`cmd/binflow-server/backup.go:335-346`（`dbDst := sqlitePath(cfg)` → MkdirAll → `copyFile` O_TRUNC）、`:326-332`+`:509-520`（失败清理只清 `cfg.Storage.DataDir`）、`cmd/binflow-server/main.go:455-460`（`sqlitePath` 对非空 DSN 原样返回，不区分 driver）。
- 失败场景 a（postgres 配置）：config 允许 `driver: postgres` + URL 型 dsn（internal/config/validate.go:105-134 放行）。此时 `sqlitePath(cfg)` 返回 URL 字符串，`MkdirAll(filepath.Dir(url))` + `copyFile` 会把 SQLite 快照写成形如 `./postgres:/user@host:5431/binflow` 的**垃圾路径文件**（已用独立探针程序实证：`filepath.Dir` 折叠 `//` 后在 CWD 下建出 `postgres:` 目录树并成功写入文件），直到最后 `metadata.Open` 才因 errPostgresDisabled 失败退出——exit code 非 0，但垃圾残留位于 data 目录之外，`clearDirContents` 清不到，且全程白拷一遍 blobs 再清空。
- 失败场景 b（sqlite + 显式 dsn 在 data 目录外）：`metadata.dsn: /srv/binflow/other.db`、data dir 为空时，`requireEmptyDataDir` 放行，`copyFile` 以 O_TRUNC **静默覆盖既有 db 文件**——违背 ADR-0015 决策 4「仅空实例恢复」（守卫只覆盖 data 目录，不覆盖显式 dsn 的落点）；同理写阶段中途失败时，已写到 data 目录外的 dbDst 不会被清理，形成 data 目录外的半恢复残留（下次同配置 serve 打开即悬空引用）。
- 修复建议：import 开头 fail-fast：`cfg.Metadata.Driver` 非 sqlite → 拒（M4 恢复是文件级操作，SQLite only，与 export 的类型断言守卫对齐）；`dbDst` 不在 data 目录下时要求其**不存在**方可继续；失败清理路径把 dbDst 一并纳入（或干脆拒绝 data 目录外的 dsn，最小面）。

## 建议改进（non-blocking）

1. `backup.go:98-121`：`checkDirDisjoint` 在 `prepareBackupDir` **之后**才跑——`--output <data>/bk` 的拒绝会在 data 目录里留下刚创建的空目录（cleanup 只删产物不删目录本体）。把 disjoint 检查挪到创建之前。
2. `internal/storage/backup_test.go:191`：mtime 断言是 per-blob 的双值析取（`Equal(stamp) || Equal(stamp+1m)`），两个 blob 的 mtime 互换也能通过；应各自断言自己的期望值（cmd 级 backup_test.go:262-268 的 src-vs-dst 对账是对的）。
3. `backup.go:398-427`：spot 抽样的「前 100」依赖 manifest 顺序，import 侧不重排——被重排过但结构合法的 manifest 会平移样本。`LoadManifest` 后防御性 `SortManifestBlobs` 一下更稳。
4. `ManifestBlob.MTime` 目前纯文档性，import 从不校验恢复后的 mtime 与 manifest 记录一致；`--verify full` 下顺带 stat 对账可拦「备份内 mtime 被动过」的 grace 时钟破坏（ADR-0006 勘误②的完整性面）。
5. windows 面：`lockHolder` 对被其他句柄独占区间加锁的字节做 `ReadAt` 会被 LockFileEx 拒（ERROR_LOCK_VIOLATION）→ holder 降级 "unknown"。仅诊断降级，与「windows 运行时未验」遗留一并交 M5。
6. `backup.go:113-131`：export 对不存在的源（config 打错/空 data dir）会 `AcquireDataLock` 建目录 + `metadata.Open` 建出种子库，产出一个「成功」的空备份（exit 0）。建议 `sqlitePath(cfg)` 不存在时拒（防呆）。
7. `internal/storage/backup.go:239-259`：`copyFilePreserving` fsync 了文件但没 syncDir 目标 shard 目录（引擎自身写路径按 ADR-0006 三件套含 syncDir）；恢复树在掉电场景缺目录项持久性。与引擎协议对齐更一致。

## 范围外发现（交 conductor）

- 整库 `make lint` 在 internal/adapter/docker 测试文件 typecheck（T-95 给 repo.Service 加 Usage 后 docker fake 未跟上）——工作日志已申报，非本票文件，建议随当前工作树的 docker 修改一并处理。

## 结论

主线（默认配置、规范操作序列）正确且测试扎实：顺序硬规则、mtime 保真、快照一致性、损坏矩阵、锁互斥证据都成立。两个 blocker 都在边缘配置/并发操作员序列上：B1 是锁生命周期自洽性（代码自身注释点名的反模式被清理路径实现），B2 是恢复写面的越界守卫缺失。均为小改动，修完即可转 APPROVE。
