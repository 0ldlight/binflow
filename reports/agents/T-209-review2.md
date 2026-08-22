# T-209 返修轮复核日志（code-reviewer，review2）

- 日期：2026-08-23
- 视角：correctness（含上轮三 blocker 逐项复核 + 新增改动扫查）
- 结论：**REQUEST_CHANGES**（三 blocker 均确认修复；但返修 commit 在 OpenEngine 失败路径新引入一处数据破坏行为，1 blocking）

## 一、上轮三 blocker 逐项判定

### Blocker ① PurgeTransientFromSnapshot 未清 upload_sessions → 已修复

- `internal/metadata/snapshot.go:198-220`：改为对 `{web_sessions, upload_sessions}` 固定白名单循环，逐表独立 `sqlite_master` 探测（pre-010 快照 no-op 不失败导出），DELETE 为字面量语句（gosec G202 无虞），错误均 wrap 表名。
- 「sqlite/postgres 两形态」核实：备份快照**只有 sqlite 一种形态**——`cmd/binflow-server/backup.go:150-152` 导出要求 snapshot face（"SQLite only in M4"），`backup.go:330` 导入强制 `metadata.driver: sqlite`。postgres 实例 M4/M6 无法导出，`migrations/postgres/010_upload_sessions.sql` 仅为方言对齐文档（migrator 只 embed sqlite，README 明示）。故本修复覆盖了唯一存在的快照形态。
- 测试钉死：`snapshot_test.go` fixture 经**真 store**（`md.UploadSessions().Create`）种入 upload_sessions 行；`TestPurgeTransientFromSnapshotRemovesRuntimeSessionTables` 断言净化后两表 COUNT=0 且 remote_cache 保留。真实端到端，非空断言。

### Blocker ② ResumeSession 持 e.mu 调 old.cleanup()（锁序反转 + 误删共享态）→ 已修复

- `internal/storage/engine.go:276-297`：持 `e.mu` 仅做注册表替换并取出旧引用，**先 `e.mu.Unlock()` 再 `old.detach()`**；`session.go:262-280` `detach()` 只取 `old.mu`（关 fd + 置 done），不删目录、不删 DB 行——新会话共享的 `uploads/<id>/` 与行得以保全。
- 全包锁嵌套审计：唯一嵌套对是 `s.mu → e.mu`（finishLocked → forgetSession）；`Close` 持 e.mu 排空后**先释放再** cleanup（s.mu）；`isLiveSession`（e.mu.RLock）仅在 sweep（OpenEngine 时，无 s.mu）调用；`BeginSession`/`ResumeSession` 的 e.mu 段内无 s.mu。**不存在任何持 e.mu 获取 s.mu 的路径**，反转彻底消除。
- 并发推演：同 id 并发 resume 为 last-writer-wins，被顶替的旧句柄后续 Append/Commit 确定性返回 "session already finalized"（done 置位），无数据破坏；resume-vs-abort 竞态 fail-closed（行被删 → SetState 报 ErrUploadSessionNotFound → 毒化；目录被删 → open 报错）。未发现死锁或损坏路径。

### Blocker ③ TestV2BlobSessionSweepResidue 断言已删除的磁盘 sessions/ → 已修复（一处弱化，non-blocking）

- `docker_blob_test.go:759-820` 重写：先 `metadata.Open` 再注入 `Sessions` 缝；POST+PATCH 弃置后断言 `ListExpired` 恰 1 行——**真实证明 httpapi 全栈把会话落进了 upload_sessions**（旧测试从未证明任何持久化），这正是上轮要求的核心。
- 弱化点（non-blocking N2）：「restart sweep」臂部分空洞——弃置会话仍在 `engine.sessions` 注册表中，`st.Close()` 的 cleanup 已先行删除行+目录，故重开后的 0 行/空目录断言即使 sweep 为 no-op 也会通过。sweep 语义本身已在 storage 包被真实钉死（`TestStartupSweepSessions`/`TestSweepBoundaryExactTTL`/`TestCrashWindowsSimulated`/`TestSweepKeepsLiveSessions`/`TestSweepFallsBackToMtime`，行+目录双断言、受控 expires_at、边界 `<=`），覆盖存在、位置在别包。

## 二、其余改动扫查

- **迁移 010 两方言**：列集逐字一致（id TEXT PK / state TEXT NOT NULL DEFAULT '' / created_at / expires_at TEXT NOT NULL + idx_upload_sessions_expiry）；`LatestSchemaVersion` 从 embed 文件推导，ceiling 自动升到 10；老库回绕 fixture 补 DROP。缝：两文件均缺行尾换行（与兄弟文件不一致，纯外观）。
- **substores_upload.go**：哨兵 wrap（Get/SetState/Delete miss → ErrUploadSessionNotFound）、ListExpired 确定性排序、rows.Err 检查、defer Close。干净。
- **cmd 接线**：三处 `openStorageEngine` 全部落 `Sessions: md.UploadSessions()`（disk / dual-write 磁盘腿 / gc）；gc 改为先开 metadata 再开 engine（依赖序正确）；backup import 把 restored md 前移到 blob 恢复前并复用记审计（单 store、defer Close，无泄漏）。
- **T-208 (f21fd74) / T-209 (e7b581e) 切分**：共享文件仅 `internal/metadata/api.go`，hunk 完全不相交（T-208 +5 行 UserStore.SetEnabled；T-209 +40 行 UploadSession 族）；substores_auth.go / store.go / security.go / docker_blob_test.go 各归一主，无交叠遗漏。两 commit 相邻提交无冲突，构建全绿佐证。
- **clean-room 抽查**：全部设计溯源自 BinFlow 自有 ADR-0006/0025 与架构 §4.1，注释/错误 wrap 风格与仓内一致，无反编译翻译嫌疑。通过。

## 三、新发现 blocking

### B1（blocking）`internal/storage/engine.go:116-118` sweep 失败即 RemoveAll 整个 uploads/ 目录

```go
if err := e.sweepSessions(time.Now()); err != nil {
    _ = os.RemoveAll(filepath.Join(abs, uploadsDirName))
    return nil, fmt.Errorf("storage: open %s: %w", abs, err)
}
```

- 此行为**本 commit 新引入**（旧代码仅返回错误），且无任何测试钉住。
- 危害：sweep 任意失败（如 `ListExpired` 撞 SQLITE_BUSY——T-54 已实证高负载下 busy 可烧穿 5s timeout）时，删掉 uploads/ 下**全部**目录，包括未过期行所保护的、sweep 契约上必须保留的可续传目录。`runGC`/`runExport` 现在把**活实例的** md 注入引擎（本 commit 接线），对运行中服务器跑 gc/export 时一次瞬时 DB busy 即可摧毁全部在途上传的暂存数据（POSIX 下 fd 存活但 Commit 的 rename 路径已消失 → 在途上传集体失败）。
- 唯一良性场景是首次打开刚建的空目录（RemoveAll 为 no-op），即该行在所有子场景中只增加破坏、不增加一致性。
- 与相邻注释自相矛盾：`sweepSessions` 注释称 "a failing sweep must not brick startup; the next Open retries"，而 OpenEngine 实际硬失败。
- **建议改法**：删除该 RemoveAll 行（首次打开失败遗留一个空 uploads/ 无害）；若坚持清理，仅在本调用新建了该目录时移除。顺手把 sweepSessions 注释与实际行为对齐。

## 四、non-blocking 清单

1. `internal/storage/api.go:82-86` ResumeSession godoc「Missing rows or data files yield ErrSessionNotFound」不准确：data **文件**缺失但目录在时 O_CREATE 会新建空文件、返回 offset 0 会话（有意行为）；data **目录**缺失时返回的是裸 open 错误而非哨兵。建议改写措辞。
2. TestV2BlobSessionSweepResidue restart 臂部分空洞（见 Blocker ③）；建议修正「crash」注释或使该臂非空洞（Close 后重新种入过期行+目录再断言 sweep）。
3. `Append` 在 body 读完 EOF 与 SetState 之间 ctx 恰好取消会把完好的会话毒化（SetState 用请求 ctx）。窗口极窄、fail-closed，可接受；可考虑 `context.WithoutCancel`。
4. 迁移 010 两 .sql 缺行尾换行，与兄弟文件不一致（外观）。
5. 过期注释：`internal/adapter/docker/uploads.go:29`（"state.json persists"）、`internal/storage/s3.go:67`（"the disk engine's sessionsDirName"——磁盘引擎常量已更名 uploadsDirName；s3.go 属本包可顺手一词修正，uploads.go 属 adapter 范围外）。
6. **范围外发现（交 conductor）**：`ResumeSession` 能力目前仅 engine 级，无任何 adapter/httpapi/repo 调用方——docker PATCH 重启后仍会在 adapter 自有注册表 404，客户端从零重传。AC1 措辞在 engine 级已满足，协议层接线建议另立小票或明示延后。
7. runExport 对活实例注入 Sessions 后，export 顺带回收活实例过期会话（语义无害）且孤儿目录扫描对在途 BeginSession 有微秒级窗口——记录即可。

## 五、验证命令与输出（本机实跑，2026-08-23）

```
$ go build ./... && go vet ./...
BUILD+VET OK

$ go test -race -count=1 -run 'TestV2BlobSessionSweepResidue' ./internal/httpapi/
ok  github.com/lzwzzy/binflow/internal/httpapi	2.226s

$ go test -race -count=1 -run 'Snapshot|Purge' ./internal/metadata/
ok  github.com/lzwzzy/binflow/internal/metadata	3.168s

$ go test -race -count=1 -run 'Resume|Sweep|SessionState|CrashWindows' ./internal/storage/
ok  github.com/lzwzzy/binflow/internal/storage	1.843s

$ ~/go/bin/golangci-lint run ./internal/storage/... ./internal/metadata/... ./cmd/binflow-server/
0 issues.（exit 0）

$ go test -race -count=1 ./internal/metadata/ ./internal/storage/
ok  github.com/lzwzzy/binflow/internal/metadata	69.543s
ok  github.com/lzwzzy/binflow/internal/storage	98.999s

$ go test -race -count=1 ./cmd/binflow-server/
ok  github.com/lzwzzy/binflow/cmd/binflow-server	29.927s
```

（测试名按实际锚测试调整：metadata 用 `Snapshot|Purge`，storage 用 `Resume|Sweep|SessionState|CrashWindows`，覆盖三 blocker 对应面。）

## 六、结论

三个返修项全部真实修复（非表面补丁），构建/vet/定向 race/三包 lint/全量 race 全绿。但 `engine.go:117` 的 sweep 失败 RemoveAll 是返修 commit 新引入的错误路径数据破坏行为，须移除或收窄后放行。修复面极小（删一行 + 注释对齐），预计一轮即收。
