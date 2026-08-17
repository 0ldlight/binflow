# 评审报告 T-10（视角: correctness — 并发/错误处理/资源泄漏）

结论: **APPROVE**
日期: 2026-08-17 · reviewer: code-reviewer（正确性）
代码基线: 工作区未提交版本（internal/metadata/ 14 文件 + go.mod/go.sum）

## 验证命令（全部实际执行）

```
go test -race -count=1 ./internal/metadata/...        → ok 13.275s（33 测试）
go vet ./internal/metadata/...                        → 通过
gofmt -l internal/metadata/                           → 空
```

另在 /tmp/t10review（仓库完整副本，不污染仓库）追加 8 个取证探针，`-race` 全部通过：

| 探针 | 检验假设 | 结果 |
|---|---|---|
| A 双 Open 句柄并发写 + 并发 PutTarget 事务 | ADR-0007 busy_timeout 能否吸收跨进程/跨句柄写冲突 | 120/120 成功，0 SQLITE_BUSY |
| B1 FilterUnreferenced 边流边**部分**删（grace 截断形态） | 同 created_at 游标会否重复投递 | 40 行零重复（假设否定） |
| B2 fn 回调中途插入引用 | 缓冲页 TOCTOU | **证实**（见 minor-1） |
| B3 created_at 混合格式/空串 | 游标比较丢行 | 15 行零丢失（假设否定） |
| C 畸形 hash m/t 超 uint32 范围 | uint32 截断 fail-open/panic | 全部 false，无 panic |
| D ctx 取消的错误形态 | errors.Is(context.Canceled) 可判别 | 可判别，且 wrap 带操作上下文 |
| E 迁移第 3 句失败 | 半套 schema / 记账泄漏 | 完整回滚，0 表 0 行（原子性成立） |
| F/F2 多连接池下的 per-conn PRAGMA | FK 是否只对单连接生效 | **证实**（见 major-1 依据） |

---

## blocker（0 条）

无。

## major（0 条）

（major-1 见下「重要判定」，为对 conductor 决策的输入而非对当前代码的缺陷。）

## minor（4 条）

### minor-1 · substores.go:247-289 — FilterUnreferenced 缓冲页存在 TOCTOU，建议文档化或收紧
**问题**：`QueryContext` 整页拉取后才调用 `fn`。若 `fn` 执行期间其它路径（上传 Put node）为页内某个 sha256 建立了引用，该 sha256 本轮仍会被投递。探针 B2 证实：25 行全部在流中途被引用后，25 行仍全部投递。
**影响**：这是「流式反连接」的固有竞态（OFFSET 也一样），不是本实现引入的回归；真正的安全网是 T-13 GC 在物理删除前必须二次确认引用（`nodes` 反查 + grace）。当前包内 `TestFilterUnreferencedWhileDeleting` 不覆盖此形态。
**建议**：在 `FilterUnreferenced` godoc（api.go:170-176）明示「回调返回的 sha256 是页快照，删除前调用方必须复查引用」；T-13 review 时核对 GC 是否做二次确认。二选一，不改代码也行，但契约要说清楚。

### minor-2 · store.go:157-159 — seedAdmin 的 RowsAffected 分支是死代码
```go
if n, err := res.RowsAffected(); err == nil && n > 0 {
    _ = n // seeded; nothing else to report in M1 (logging belongs to the caller)
}
```
整块无副作用，返回值全被丢弃。**建议**：删掉整个 if，或改为 `if _, err := res.RowsAffected(); err != nil { return fmt.Errorf(...) }` 把 RowsAffected 的错误路径（理论上可触达）纳入错误链——现在的写法把它静默吞了。

### minor-3 · substores_auth.go:147-158 — tokenStore.Touch 丢上下文
`wrapExec("tokens touch", "", err)` 与 `"tokens delete"` 的 key 传空串，错误里没有 token id；同函数下方 sentinel 分支却拼了 `%d`。另外 Touch 对「值未变化」（lastUsedAt 相同）在 SQLite 下 RowsAffected 可能为 0，会误报 ErrTokenNotFound——认证热路径上 Touch 同一秒两次（时间戳秒级精度，`Now()` 无小数位）是现实场景。**建议**：key 传 `strconv.FormatInt(id, 10)`；Touch 的 n==0 分支改为容忍（或改用 `changes()` 不可移植，直接去掉 n==0 判定即可，UPDATE 未命中与未变化都无害）。

### minor-4 · concurrency_test.go — 并发测试未覆盖「事务中途失败」与「迁移并发」
现有并发负载是纯 happy-path upsert。建议（不阻塞）补两类：
1. 并发写中混入约束冲突（同 (repo,path) 不同 sha 的 FK 违例），确认失败者拿到的错误可判别且不污染连接状态；
2. 两个 goroutine 同时 Open 同一文件的迁移（进程内首启竞态）。当前 `migrate` 靠 `schema_migrations` 版本判断，双 Open 双迁移并发跑 DDL 时无互斥保护——跨进程靠 busy_timeout 串行化事务本身，但「读 current → 决定应用」在两个连接间不是原子的。实测场景（探针 A 双 Open）第二个 Open 因版本已应用而跳过，未触发；但两进程**同时首启**（compose 里 sidecar 与主进程竞跑）窗口存在。低成本修法：`applyMigration` 的 INSERT 改 `INSERT INTO schema_migrations ... ` 前先 `SELECT ... FOR UPDATE` 不可移植——SQLite 下把「读版本+应用」整体放进一个 `BEGIN IMMEDIATE` 事务即可，或文档声明「首启迁移非并发安全，由部署层保证单进程先完成迁移」。

## nit（4 条）

1. **store.go:124** `NumCPUConcurrency()` 现在无任何调用点（实现者自称是"语义锚点"，但锚点无人引用即为死代码）。若 conductor 裁定维持 MaxOpenConns=1，建议删除该函数并在 store.go:73-75 注释中直接写明「PRAGMA 是 per-connection 的，见 major-1」。
2. **internal_test.go:334** `_ = sql.ErrNoRows // keep database/sql import meaningful` —— 该文件本就 import database/sql（line 5 用了 `*sql.DB`），此行为遗留噪音，可直接删。
3. **substores_auth.go:240-248** GetTarget 里那个立即执行的匿名函数包装 QueryRow 让人费解（为了闭包捕获三个变量？直接顺序写更清楚）。风格问题。
4. **api.go:24-27** `ErrDuplicate` 声明后零使用。M1 各 Create 直接透传驱动约束错误（wrap 过），调用方判 409 需要字符串匹配驱动错误。T-11/T-12 接手时若确需类型化信号再实现，目前留着无害但接口位是空的。

## 重要判定（供 conductor，非缺陷）

### MaxOpenConns=1 vs AC 字面 NumCPU —— **支持实现者的偏离，反对"一行改回"**

T-10.md 遗留 1 提出请 conductor 定夺。取证结论（探针 F/F2）：

- `foreign_keys` 是 **per-connection** PRAGMA，modernc 驱动默认 **0**（探针 F2：4 连接全 0）。
- `setupSQLite` 经 `db.ExecContext` 只 PRAGMA 了**池当时给出的那一个连接**。
- 探针 F：若把池放大到 NumCPU(4)，setupSQLite 后新取的连接……实测仍全是 1/5000 —— 这是因为探针 F 里 PRAGMA 在 4 个 conn 都建立后才执行且池把语句路由到了后续复用的同一连接；**但 F2 证明基线是 0**，即多连接池下「哪条连接被 PRAGMA 过」完全取决于执行时序，不 PRAGMA 的连接 FK 静默失效（DELETE repo 不级联 nodes → 孤儿 node 指向已删 blob，数据完整性破坏，且无任何报错）。

因此：
- **当前代码（MaxOpenConns=1）下 PRAGMA 策略是正确的**——唯一连接被 PRAGMA，永远复用。
- **若按 AC 字面改回 NumCPU，现状 setupSQLite 会变成潜伏 bug**。T-10.md "一行可改" 的说法不成立：改 NumCPU 必须同时把 PRAGMA 挂到 `db.Conn` 级（每次取连接执行）或 DSN 参数（modernc 支持 `_pragma=foreign_keys(1)` query 参数）。
- 顺带：ADR-0007 写的 `MaxOpenConns = NumCPU`（DECISIONS.md:96）在「PRAGMA 只跑一次」的实现策略下与 FK 强制自相矛盾。建议 conductor 指示 architect 出 ADR 勘误（要么改 ADR 为 1，要么规定 per-conn PRAGMA 机制），而不是让本票改代码。

### 其余核对结论（简述）

- **迁移原子性**：探针 E 证实第 3 句失败时前两句完全回滚、无版本记账，`defer tx.Rollback()` 模式正确。✓
- **删仓级联 / PutTarget 整体替换 / DeleteTarget 双删**：均在单事务或 FK CASCADE 内。✓
- **rows.Close**：所有 Query 路径 defer Close，FilterUnreferenced 三条提前 return 路径（scan 错/rows.Err/fn 错前）均显式 Close——fn 错误路径在 batch 已收集、rows 已 Close 之后才回调，无泄漏。✓
- **sql.ErrNoRows**：全部 8 个 Get 类方法转为包 sentinel，无裸泄漏。✓
- **argon2id**：常量时间比较 ✓；畸形 hash（含 m/t 超 uint32 截断场景）fail-closed 无 panic ✓；Token 明文仅 sha256 入库、无日志路径 ✓。
- **keyset 分页**：B1/B3 假设均被否定——同 created_at 部分删除不产生重复投递，混合时间戳不丢行；实现者声称的 50 行零跳行测试有效。✓
- **clean-room**：代码与 reverse-src 无逐行对应嫌疑（查询/错误风格均为 Go database/sql 惯用法，DDL 与架构 §6 文本一致而非与反编译产物一致）。✓
- **范围外发现**：`go build ./...` 失败于 internal/storage/engine.go:263 与 internal/config（T-8/T-9 在途）——已由实现者申报，非本票问题，不影响本 area 内全绿。

## 结论

正确性维度无 blocker/major 缺陷。4 个 minor 均不阻塞合入（minor-1 需在 T-13 review 时闭环）。MaxOpenConns 争议建议按上文判定处理（保持 =1，勘误 ADR）。
