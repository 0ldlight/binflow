# T-213 复审报告（code-reviewer，视角: correctness + consistency）

- 结论：**APPROVE**
- 日期：2026-08-23
- 范围：internal/storage（api.go / engine.go / session.go / migration.go / s3.go + 新测试 resume_offset_test.go，`git diff` 全量 + 上下游调用点核查）
- blocking：0；non-blocking：5（记录性，不要求本票修改）

## 1. 契约正确性（§3.1 [M7] / §5.3.1 契约 1/3/5）

- **Offset() 语义**：disk `uploadSession.Offset()`（session.go:41-48）持 `s.mu` 返回 `state.Received`。活会话：Append 在 `s.mu` 下 `state.Received += written`，与文件长度一致（唯一 fd 顺序写，无 O_APPEND 竞态）；恢复会话：`ResumeSession` 构造 `sessionState{Received: info.Size()}`（engine.go:308），即重算文件长度、非行内簿记。严格等价「恢复后=数据文件重算长度 / 在途=已收字节」。判定：**通过**。
- **godoc 措辞**：api.go Session.Offset 注释逐点覆盖 §3.1 的「累计已收字节数 / disk=文件长度 / 恢复后=重算长度 / 权威 offset 源、取代 adapter 内存镜像 / 与 Append/Commit 互斥读」；ResumeSession godoc 与 §3.1 修订版契约（行缺失或已过期未清扫 → ErrSessionNotFound fail-closed；数据文件缺失但目录在 → offset 0 重建）一致；ErrSessionNotFound 哨兵 doc、S3 注释（契约 5 hard-404 + 测试钉死）均对齐。判定：**通过**。
- **migrationSession 委托**（migration.go:660-667）：disk 是 primary（ID 由 disk 命名、`MigrationEngine.ResumeSession` 经 `activeEngine` 委托 disk），且 dual-write Append 在成功路径强制两侧 cumulative offset 相等（不等即双双 Abort 报错），委托 disk.Offset() 语义正确。判定：**通过**。
- **S3 最小实现**（s3.go:577-588）：`received` 仅在 Append 内 `s.mu` 下递增（= 已提交 parts + 挂起 partBuf），Offset 持同一 `s.mu` 读取；S3 恒无恢复路径（ResumeSession hard 404），故 Offset 只描述在途会话——append 链路语义正确。判定：**通过**。

## 2. 过期 fail-closed 边界一致性

- **边界对齐**：sweep `ListExpired` = SQL `WHERE expires_at <= ?`（substores_upload.go:70，now 为 `UTC().Format(RFC3339)`）；`sessionRowExpired` = `time.Parse(RFC3339)` 后 `!now.Before(t)` 即 `t <= now`。生产格式行（写路径 `BeginSession` 恒写 `expiresAt.UTC().Format(time.RFC3339)`，秒精度定宽 UTC "Z" 串；schema 010 `expires_at TEXT NOT NULL` RFC3339 UTC）下字符串序 == 时间序，`<=` 两侧同界：now 含亚秒时 `Truncate(now) >= t ⇔ now >= t`（t 为整秒），两者永不分歧。测试钉死 `exactly now` 边界（frozen clock）。判定：**通过**。
- **空/畸形 expires_at fail-closed extra**：正常写路径不可能产生（BeginSession 恒写、SetState 不触 expires_at、schema NOT NULL）；仅手工损坏行触达。空串 `""` 在 ListExpired 字符串比较下 `<= anything` 为真 → sweep 会回收，resume 判过期——两侧一致。畸形串 sweep 视为「永不回收」而 resume 判死（见 non-blocking ②）。判定：**通过**（姿态正确）。
- **拒绝路径零副作用**：过期检查位于 `ss.Get` 之后、`os.OpenFile(O_CREATE)` 之前（engine.go:282-288），此前仅有 ctx/checkOpen/空 id/nil store 只读判断；不删行、不建目录/文件。测试逐例断言行存活 + 数据文件字节原样。判定：**通过**。

## 3. 并发与锁

- Offset 只取 `s.mu`，无嵌套锁（不存在 s.mu→e.mu 反序路径：finishLocked/forgetSession 先释 s.mu 再取 e.mu 的既有序不受影响）；Append/Commit 全程持 `s.mu`，Offset 为互斥读（§3.1「与 Append/Commit 互斥读」的忠实实现），无撕裂读。
- 恢复期重哈希发生在会话对象构造并注册之前，调用方拿到句柄时 Offset 已是重算值；同 id 再恢复的旧句柄被 `detach()`（后续 Append 报 already finalized），见 non-blocking ③。
- race 臂真实：`TestSessionOffset/Offset is safe to read while Append runs` 一侧 64 轮 Append、一侧循环读 Offset 断言单调有界，`-race` 绿。判定：**通过**。

## 4. S3 零改动声明核实

s3.go diff 逐行仅两处：`ResumeSession` 注释扩写（函数体一行未动，仍 `ErrSessionNotFound` wrap）+ `s3Session.Offset()` 新增（接口编译必需，§3.1 已冻结该方法签名，非单方改接口）。可执行行为零变化；`TestS3ResumeSessionNotSupported` 未被动、实测 PASS。声明成立。判定：**通过**。

## 5. 接口扩散

`go build ./... && go vet ./...` 全绿（vet 连测试文件一起编译）；全仓 `storage.Session` 实现者仅 storage 包内三个（disk/s3/migration）；生产代码尚无 `.Offset()` 调用方（仅 migration.go 内部委托）——adapter 接线确属 T-216（BOARD 波 2，§5.3.1 resolve 伪代码归 adapter area）。判定：**通过**。

## 6. 实跑命令与输出（复核人本机）

```
$ go build ./... && go vet ./...
ALL-BUILD-VET-OK
$ go test -race -count=1 -run 'Offset|Resume|Sweep|Expired' ./internal/storage/...
ok  github.com/lzwzzy/binflow/internal/storage  1.903s
$ go test -race -count=1 -run 'TestS3ResumeSessionNotSupported' ./internal/storage/... -v
--- PASS: TestS3ResumeSessionNotSupported (0.00s)
$ ~/go/bin/golangci-lint run ./internal/storage/...
0 issues.（exit=0）
$ gofmt -l internal/storage/
（空）
```

## 7. 独立红绿复核（已还原）

将 engine.go:286 临时改为 `if false && sessionRowExpired(...)`（标记 RED-GREEN-TEMP）后：

```
--- FAIL: TestResumeSessionExpiredFailClosed (0.05s)
    --- FAIL: .../expired_row_fails_closed:_strictly_past
    --- FAIL: .../expired_row_fails_closed:_exactly_now_(<=_boundary)
    --- FAIL: .../expired_row_fails_closed:_empty_expires_at
    --- FAIL: .../expired_row_fails_closed:_malformed_expires_at
```

四个过期子例恰好全红（unexpired/no-row/no-data-file 子例仍绿，证明失败确由过期检查驱动、非连带破坏）。还原后 `grep RED-GREEN-TEMP` 0 命中、`git diff --stat` 与复审前一致、目标测试复跑绿。**无未还原临时代码**。

## 8. clean-room 抽查

改动为惯用 Go + 项目自有 ADR/§节引用，语义来源是 architecture.md §3.1/§5.3.1（architect 定稿契约）与 OCI distribution 行为规格（docs/reverse/docker-registry.md），与 reverse-src/（Artifactory Java 反编译）无逐行/结构对应嫌疑。**无 clean-room 违规**。

## non-blocking（记录，不阻塞本票）

1. **理论边界分歧（不可达路径）**：非生产格式 expires_at（亚秒精度或非 UTC 偏移，仅手工改库可产生）下 SQLite 字符串比较与 `time.Parse` 时间比较可能分歧（如 `...:00.5Z` 字符串小于截断到秒的 now 串、时间上却晚于 now）。写路径不变量保证不可达；如未来改写路径格式需同步核对此处。
2. **畸形 expires_at 行 sweep 永不回收**：如 `not-a-timestamp` 字符串序大于任何数字头 now 串，ListExpired 永不选中 → 常驻残留（resume 已 fail-closed 拒绝）。手工损坏才可达，属 sweep 侧既有性质，非本票引入。→ 范围外交 conductor 知悉即可。
3. **detached/已完成句柄的 Offset**：同 id 被再恢复后旧句柄 Offset 返回 detach 前值；Commit 后 Offset 返回最终字节数（未清零）。均无 mutator 可用（Append 报 already finalized），T-216 的单飞 funnel 下不会外露。
4. **毒化会话 Offset 可落后于文件长度**（部分写推进文件、Received 未更新即毒化）：godoc「=文件长度」的等价关系仅对健康活会话成立；毒化会话按契约必须 Abort，无消费方。可在未来票把措辞收窄为「live session」。
5. **Offset 在长 Append 期间阻塞**（互斥读的代价）：REST GET 状态腿若在 PATCH 进行中调用会等待 Append 完成。§5.3.1 resolve 仅在 lookup-miss 路径调 Offset，无实际停顿面；若 T-216 需要非阻塞状态读再议（需原子计数器），当前实现符合冻结契约「互斥读」。

## 范围外发现

- 工作树另有 T-211（scripts/m7-*.sh、Makefile）与 T-214（docs/design/architecture.md、DECISIONS.md）未提交改动，与本票无关，area 不重叠，未纳入本复审。
- OpenEngine 启动 sweep 用 `time.Now()` 而非 `opts.Now`（engine.go:120，既有代码）：测试注入时钟不影响启动 sweep。本票测试以「sweep 后再播种」绕开，无影响；如未来想让 sweep 可测时钟，属小重构票。
