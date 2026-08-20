# 评审报告 T-90（视角: correctness）

结论: **APPROVE**

- reviewer: code-reviewer（单 reviewer，正确性为主，沿 T-34/T-62 迁移票范式）
- 日期: 2026-08-20
- 对象: commit 595e090（feat: T-90 metadata 004_console_governance）+ 实现日志 reports/agents/T-90.md
- 评审树: main @ f42e157（含并行票在制面，与本票无关的差异已剔除）

## 逐项核对

### 1. DDL 对照（AC ①）— 通过

`internal/metadata/migrations/sqlite/004_console_governance.sql` 与 architecture.md §6「004_console_governance.sql」定稿块**逐列一致**：

- `users.email TEXT NOT NULL DEFAULT ''`（FR-27-AC8）、`idx_audit_actor(actor,time)` / `idx_audit_action(action,time)`（GE-01/NFR-P17）——两处 PRD R2 列级增量齐备；
- 四表列集/类型/默认值/PK/FK 全对齐：`groups`（id ROWID alias、name UNIQUE、description 默认 ''）、`user_groups`（**T-108 勘误名**，非派单摘要的 `group_members`；PK (group_id,username)、双 FK ON DELETE CASCADE）、`web_sessions`（id_hash TEXT PK、last_used_at/revoked_at 默认 ''、FK→users CASCADE）、`repo_usage`（repo_key PK、FK→repositories CASCADE）；`idx_web_sessions_user` 在位；
- 差异仅注释（契约允许），文件体零 BEGIN/COMMIT，语句在共同子集内（无 AUTOINCREMENT/RETURNING）。

钉死度：`TestConsoleGovernanceSchemaShapeMatchesArchitecture` 用 pragma 断言列集（`tableColumns`）、PK 顺序、users.email default `''`、groups.name UNIQUE（origin='u'）、三索引存在、FK 计数（2/1/1）。CASCADE 语义未走 pragma 而是走行为断言（TestGroupDeleteCascadesMembers / TestWebSessionSweepCandidates 的用户删级联）——行为断言更强，可接受。

### 2. 迁移安全（AC ①）— 通过

- 迁移器零改动，`loadMigrations` 吃新文件、`schema_migrations` 账本跳过已应用版本——幂等范式沿用 T-10/T-34/T-62；`TestConsoleGovernanceMigrationIdempotent` 双开验证。
- 老库升级真实性：`TestConsoleGovernanceUpgradeFromHistoricalShapes` 用 `buildLegacyDatabase` 真实回放 001-only / 001+002 / 001+002+003 三形态（带存量 repositories/nodes/blobs、docker_manifests、remote_configs 行 + 旧口令 admin），断言升级到 latestMigrationVersion()、admin 口令不被覆盖、**legacy 用户 email 回填 ''**、004 三面（groups/SetUserGroups/web session/audit Query）立即可用。`ALTER TABLE ADD COLUMN ... NOT NULL DEFAULT ''` 为 SQLite 常量默认值合法形态，存量行零改写。
- `TestDockerUpgradeFromM1Database` 回卷语句表按 FK 依赖序补齐 004 拆除（repo_usage → web_sessions → user_groups → groups 先子后父，email DROP COLUMN）。

### 3. 子接口正确性（AC ②）— 通过

- **SetUserGroups 原子替换**：单事务 DELETE-then-INSERT；未知组在事务外预解析为带组名的 ErrGroupNotFound，失败替换不半应用（测试显式断言 [qa] 不变）；重名去重、空表清空、FK 拒绝幽灵用户。
- **Touch 并发**：单条 UPDATE 语句原子；`TestWebSessionConcurrentTouch`（16 goroutine，-race）终值 ∈ 写入集。另实测 SQLite `changes()` 对相同值 UPDATE 仍计 1（`UPDATE t SET v='x' ... ; SELECT changes()` → 1），Revoke/UpdateEmail 幂等重放不会误报 NotFound。
- **ListSweepable 边界**：`expires_at <= now`（含等于）OR `revoked_at != ''`，limit<=0 全量、live 行排除、删除用户 FK 级联——测试覆盖。
- **audit Query keyset 倒序稳定性（重点复核）**：排序 `(time DESC, id DESC)` + 谓词 `(time < ct OR (time = ct AND id < cid))` 在 rowid 唯一的前提下构成全序上的严格「游标之后」集，数学上无跳无重。**独立探针**（sqlite3 3.43.2，/tmp，300 行仅 6 个时间戳 ≈ 50 行同秒带，页大小 1/7/13/50）：四种页大小均 seen=300、duplicates=0——同秒多条翻页正确。仓库内 TestAuditQueryCursorWalk（7 行 3 时间带、页 2）+ 10k 全量走查（seen==10000）双重确认。
- **EXPLAIN 断言真实性**：11 过滤形态在 10k 行上全 PASS；我另在独立库上复核三个代表形态的计划——actor+窗口+cursor → `SEARCH ... USING COVERING INDEX idx_audit_actor`（OR 游标谓词被收进索引区间约束）、裸 cursor → `SEARCH idx_audit_time (time<?)`、action → `SEARCH idx_audit_action`——与实现者「复合索引倒扫免 SORT」的论证一致；断言拒绝裸 `SCAN audit_events`。

### 4. 越界 1 行 — 合理，知悉即可

`internal/adapter/docker/token_test.go`：fakeUsers 补 `UpdateEmail` no-op 桩（注释 + 3 行方法，commit 计 4 行）。UserStore 接口扩方法的机械适配，不补则 docker 包测试编译红；token 流不触 email，桩无行为。`go test -race ./internal/adapter/docker/` 全绿。判定：保留，无需回退（回退方案反而砍掉 T-97 SE-05/06 的落盘缝）。

### 5. 接口面 — 干净

- `GroupStore`/`WebSessionStore` 方法最小集，语义全部落在 doc comment；组名/保留字校验、409 引用检查、TTL 滑动封顶判定正确留给 T-97/T-93（存储层不含策略，边界正确）。
- 三个哨兵错误均以 `%w` 包装、测试用 `errors.Is` 验证；ErrInvalidCursor 供 T-93 映射 400。
- `AuditStore.List` 保留且 SQL 逐字未变（`ORDER BY id DESC`），仅抽公共 `scanAuditEvents`——internal/audit 消费者 -race 绿，零回归。
- `repo_usage` 仅建表、零方法——计量归 T-95，边界守住；README 已给 T-95 的 upsert 同事务建议。
- postgres README 版本史补 004 行，占位状态如实。

### 必须修改（blocking）

无。

### 建议改进（non-blocking）

1. `substores_console.go:313` parseAuditCursor 不校验时间部分形状：`"garbage|5"` 这类游标被静默接受（time < 'garbage' 多半空结果，客户端误判为数据尽头）。建议加 `time.Parse(time.RFC3339, timePart)`，或在 T-93 把 cursor 包成不透明 base64 时一并收紧。
2. `console_governance_internal_test.go:303` EXPLAIN 测试手工复写了 Query 的 SQL 文本而非截获 store 实际构造的语句——日后 Query 改 SQL 形态时计划断言会与实现漂移。建议抽出 SQL 构造函数（builder）供产码与测试共用。
3. `console_governance_internal_test.go:70` 索引存在性检查未断言复合索引的**列序**（pragma_index_info）：若误写成 `(time,actor)` 仍能通过存在性检查，且 `SCAN ... USING INDEX` 形态也放行。建议补三索引列序断言。
4. `substores_console.go:100` SetUserGroups 的组名→id 解析在事务外：解析后、插入前组被并发删除会以裸 FK 错误（非 ErrGroupNotFound）冒出——事务回滚保证不半应用，安全；仅错误类型在罕见竞态下退化。
5. `substores_console.go:169` GroupsOfUser/List 空集返回 nil 而非 `[]`（包内一致约定），但 api.go doc 写 "returns an empty slice"——T-97 序列化 JSON 时注意 null vs [] 归一。
6. 仓库内现有两个 ErrInvalidCursor（repo.ErrInvalidCursor / metadata.ErrInvalidCursor，语义域不同）：T-93 在 GE-01 端点只应把 metadata 的映射 400，留意别混用。

### 范围外发现（交 conductor）

- commit 595e090 把 T-92 在制的 `search.go`/`search_test.go` 一并带进了 T-90 的 feat 提交（T-90.md 遗留 4 已预警合流）；提交粒度问题，无代码缺陷。
- RFC3339 文本比较的时间序依赖全链统一秒级精度（`Now()`）；若上游（T-93 since/until 解析）引入亚秒精度文本会出现字典序≠时间序。ADR-0007 既有约定，建议 T-93 解析后统一 `time.RFC3339` 重排。

## 取证命令（实际执行）

```
go vet ./internal/metadata/                        # 干净
gofmt -l internal/metadata/ internal/adapter/docker/token_test.go   # 空输出
go build ./...                                     # rc=0
go test -race ./internal/metadata/ -count=1        # ok 19.469s（77 顶层测试，-count 全量）
go test -race ./internal/audit/ ./internal/auth/ ./internal/adapter/docker/ -count=1  # 全 ok
go test -race ./internal/metadata/ -run 'TestConsoleGovernance|TestAuditQuery' -v     # 含 11 计划子测全 PASS
sqlite3 /tmp 独立探针: 300 行 6 时间带, 页 1/7/13/50 → seen=300 dup=0; EXPLAIN 三形态均 SEARCH USING INDEX
sqlite3 :memory: UPDATE 同值 changes()=1            # 幂等 UPDATE 不误报 RowsAffected=0
grep reverse-src user_groups|web_sessions|repo_usage  # 零命中（clean-room 抽查通过）
golangci-lint 本机未装，采信实现日志 make lint 0 issues（vet/gofmt 已独立复核）
```
