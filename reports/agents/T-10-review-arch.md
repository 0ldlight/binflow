# 评审报告 T-10（视角: 架构一致性 / 测试覆盖）

结论: **REQUEST_CHANGES**

- 日期: 2026-08-17；reviewer: code-reviewer (arch/consistency lens)
- 评审对象: `internal/metadata/`（15 文件）+ `go.mod`/`go.sum`
- 取证命令（全部实际执行）:
  - `go test -race -count=1 ./internal/metadata/...` → `ok 14.139s`（33 测试全绿）
  - `go vet ./internal/metadata/...`、`gofmt -l internal/metadata/` → 通过/空
  - `CGO_ENABLED=0 go build ./internal/metadata/...` → exit 0
  - `go list -f '{{join .Imports "\n"}}' ./internal/metadata` → 无任何 `lzwzzy/binflow/internal/*` import
  - 针对性探针测试（临时加入、跑完已删）：`ListByPrefix("lib")`/`DeleteByPrefix("LIB")` 大小写行为实测

---

## 一、必须修改（blocking）

### B1. SQLite `LIKE` 大小写不敏感 → ListByPrefix/DeleteByPrefix 跨大小写误匹配（正确性缺陷，有实测证据）

- 位置: `internal/metadata/substores.go:158-178`（ListByPrefix）、`substores.go:143-156`（DeleteByPrefix）、`substores.go:186-193`（likePrefix）
- 问题: SQLite 的 `LIKE` 对 ASCII **大小写不敏感**。`path = ?` 精确臂是二进制比较（大小写敏感），但 `path LIKE 'prefix/%'` 臂不是。实测（探针，库内三行 `lib/a.jar`、`LIB/b.jar`、`lib-x/c.jar`）:
  - `ListByPrefix(ctx, "m", "lib")` 返回 **2 行，含 `LIB/b.jar`**（应只返回 `lib/a.jar`）；
  - `DeleteByPrefix(ctx, "m", "LIB")` **删除了 2 行——把 `lib/a.jar` 一并删掉了**（应只删 `LIB/b.jar`）。
- 影响: 制品路径大小写敏感（Maven groupId、Generic 任意路径均可含大写）。目录前缀删除是 T-12 的目录递归删基础；跨大小写误删是**静默数据丢失**路径。测试未捕获是因为全部 fixture 用小写路径。
- 建议改法（任一，推荐 a）:
  - a) `store.go setupSQLite` 增加 PRAGMA `case_sensitive_like=ON`（连接级，与现有 foreign_keys/busy_timeout 同点位；注意若未来走 DSN 参数需同步挪入 DSN）。一行修复，两处查询同时修正。
  - b) 改用区间比较 `path >= ? AND path < ?`（上界为 `prefix || '/'` 与 `prefix || '0'` 之间的构造）——可移植性最好但实现更绕。
  - 注意: `PrincipalsFor`（substores_auth.go:317-326）也用 LIKE，但 repo key 字符集 `[a-z0-9-]` 全小写且 service 层校验，不受此影响，可不动。
- 同时补测试: 大小写混合路径的 List/DeleteByPrefix 边界用例；以及前缀含 `%`/`_`/`\` 的转义用例（likePrefix 的 replacer 目前零测试覆盖，见 M5）。

### B2. MaxOpenConns=1 偏离 ADR-0007 字面决策，且实现者"语义等价"论据不成立；附带暴露 per-connection PRAGMA 陷阱

- 位置: `internal/metadata/store.go:73-77`（`db.SetMaxOpenConns(1)`）、`store.go:105-120`（setupSQLite 经 `db.ExecContext` 打 PRAGMA）
- 判断（conductor 点名要求）: **不是语义等价，应改回 NumCPU 或走 ADR 修订流程**。理由:
  1. ADR-0007 候选方案 A 就是"单连接全串行（正确但读吞吐受限）"，**被明确弃选**；决策文写着 `MaxOpenConns = NumCPU`，理由恰是"WAL 下 modernc 驱动支持多读并发"。实现取 1 = 退回被弃选的方案 A。
  2. T-10.md 的论据"WAL 的读并发在单连接上由驱动管线提供"**事实上不成立**: modernc.org/sqlite 对单条 `*sql.DB` 连接不做查询管线/交错执行，MaxOpenConns=1 下所有读（含 `/readyz` 的 Ping）在任一慢查询（大 ListByPrefix）后面排队。PRD 的 1000 并发拉取验收（ADR-0007 背景）与 §7.4 优雅停机期间的探针可用性都受影响。
  3. **附带的隐性陷阱**: `journal_mode` 是库级，但 `foreign_keys=ON` 与 `busy_timeout=5000` 是**每连接**设置。现在 `db.ExecContext` 打 PRAGMA 只配置了池中第一条连接——当前因 MaxOpenConns=1 恰好安全；谁按 ADR 把池改大到 NumCPU，FK 约束就会在新连接上**静默失效**（TestOpenPRAGMAsApplied 只测单连接，拦不住）。`TestFKBlocksOrphanNode` 也会继续在它那条连接上通过。
- 建议改法（两缺陷一次解决）:
  - PRAGMA 全部挪进 DSN（modernc 支持 `file:...?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)` 形态，**每连接**生效）；
  - `db.SetMaxOpenConns(runtime.NumCPU())`（`NumCPUConcurrency()` 直接用起来，删掉目前"导出但零调用、godoc 与实际行为不符"的状态，见 M6）；
  - 测试补一条: 并发打开多连接后任一连接 `PRAGMA foreign_keys` 均为 1（可在 DSN 落地后用两条并发生效的连接验证）。
- 备选（不推荐）: 若 conductor/architect 判断 M1 负载下单连接足够，则必须**修订 ADR-0007**（明示降级为方案 A + 用 1000 并发验收数据佐证），并在 T-10.md 更正"驱动管线"错误论据。不能维持"代码 1 连接、ADR 写 NumCPU"的矛盾状态——review 依据会失效。

---

## 二、建议改进（major，依赖票开工前需裁决）

### M1. Store 接口无法满足架构 §3.3/§4.4「node + blob link 一个 SQL 事务」（影响 T-12）

- 位置: `internal/metadata/api.go:121-219`
- 架构 §3.3 Put 的事务边界: "metadata 写 node+blob link（**一个 SQL 事务**）"；§3.2 里有一行语焉不详的注释 `// Txn 只读透传：Repo.PutNode 必须与 blob link 在同一事务`——接口体里却没有 Txn 方法（文档自身残缺）。
- 现状: `BlobStore.Put`（DO NOTHING 幂等）与 `NodeStore.Put` 是两条独立 autocommit。崩溃窗口分析: 先 blob 后 node，中间崩溃 → 只剩无引用 blob 行（GC 可收，不损数据）；FK 又强制了这个顺序。**语义安全，但字面违反 §3.3**。
- 建议: 裁决二选一并落文档/代码——
  - a) architect 修订 §3.2/§3.3: 明示"blob link 与 node 两语句、blob-first 顺序、FK 兜底"为 M1 事务边界（合理，改动最小，建议采纳）；残缺的 Txn 注释行一并清理；
  - b) T-10 补一个组合方法（如 `NodeStore.PutWithBlob(ctx, n *Node, b *Blob) error` 单事务）。
  - 不能拖到 T-12 开工后——接口变更必须走 architect（§3 前言），且 T-12 的 area 不能改 metadata。

### M2. AuditStore.List 缺时间过滤，§7.1 路由 `GET /binflow/api/v1/audit?repo=&actor=&since=` 的 `since` 无法实现（影响 T-15）

- 位置: `internal/metadata/api.go:214-219`（List 只有 repoKey/actor/limit）、`substores_auth.go:365-402`
- T-15（area: internal/httpapi 兼容 handlers）无法越区改 metadata；等它开工再发现就要开新票。建议 T-10 顺手扩成 `List(ctx, repoKey, actor, since string, limit int)`（since 为 RFC3339 前缀闭开区间，`time >= ?`，`""` 表示不过滤），或者 architect 明示 M1 砍掉 since 参数并同步 §7.1。
- 顺带（可并入同一次改动）: `ORDER BY id DESC` 用不上 `idx_audit_repo(repo_key, time)`；时间过滤加上后建议 `ORDER BY time DESC, id DESC` 以吃索引（M1 数据量下非必须，nit 级）。

---

## 三、建议改进（minor / nit）

- **M5** `substores.go:186-193` likePrefix 的 `%`/`_`/`\` 转义逻辑零测试: 前缀含下划线极常见（`my_lib/`），应有表驱动用例钉住"通配符字面匹配"（B1 修复时一并补）。
- **M6** `api.go:24-27` ErrDuplicate 已导出但没有任何 store 返回它（实现者已在遗留 #5 自认）; `store.go:122-124` NumCPUConcurrency 导出零调用、godoc 声称"连接预算"与实际 `SetMaxOpenConns(1)` 不符。B2 落地后后者自然消解；ErrDuplicate 建议要么在 substores 映射驱动 UNIQUE 冲突（`errors.Is(err, sqlite.ErrConstraintUnique)` → wrap ErrDuplicate），要么删除，避免接口污染。
- **M7** `store.go:149-159` seedAdmin: `if n, err := res.RowsAffected(); err == nil && n > 0 { _ = n }` 是死分支（体里什么都不做）；且 admin 已存在时仍先算一次 argon2（约 50ms 无用功）。建议把"是否新种"返回给上层（WARN 与 ADR-0009 的"检测到缺省值"提示归 cmd，但"是否首次种子"事实在本层）。
- **M8** `substores_auth.go:31-35` GetByPasswordHash 对非唯一列 `LIMIT 1`: 两个用户同 hash 时选行不确定（随机盐下实际不可碰撞，理论瑕疵）。建议注释标注或加 `ORDER BY username` 固定行为。
- **M9** `migrations/postgres/README.md` 建议补一条约定: 迁移文件体内**禁止自带 BEGIN/COMMIT**（applyMigration 已包事务，嵌套即错）。
- **M10** 文档侧建议（交 architect，不改代码）:
  - 架构 §6 前言"布尔用 INTEGER 0/1"与 DDL 里 `remote_configs.unreachable_mask BOOLEAN` 自相矛盾（SQLite 里 BOOLEAN 只是 NUMERIC 亲和，Postgres 里 BOOLEAN 又不吃 0/1 字面量——README 已意识到，建议 §6 统一成 INTEGER，Postgres 方言文件自行映射）;
  - 架构 §6 DDL 块内含 `CREATE TABLE schema_migrations`，实现挪到迁移器创建（鸡生蛋问题，挪得对）——建议 §6 加一行注记与代码对齐;
  - §3.2 残缺的 `Txn 只读透传` 注释行（见 M1）。

## 四、核对通过项（无问题）

1. **包边界（§2）**: metadata 仅 import stdlib + `modernc.org/sqlite` + `golang.org/x/crypto/argon2`，零 `internal/*` 横向依赖（`go list` 取证）；`metadata_test.go` 等外部测试也只 import metadata 自身。
2. **api.go vs §3.2**: 六访问器签名与 §3.2 逐一致；新增 `Permissions()`（§3.2 注释明示 PermissionStore 在列）、`Ping`（§7.1 readyz 需要）、`DeleteByPrefix`（§3.2 自身声明"定稿以本文件 + 代码 api.go 为准"，扩展合规）；FilterUnreferenced 落在 BlobStore 并带分页回调，符合 §3.2 "反连接分页流式产出"的意图，签名偏离有注释。
3. **DDL vs §6 逐表 diff**: 9 表 + 全部索引/外键/UNIQUE 级联逐项一致；permission_targets/permission_principals 为 T-22 回写版（含三点差异注释，T-22 要求满足）；repo key `{1,62}` 注释在位（001_init.sql:14）；迁移 SQL 无 AUTOINCREMENT/RETURNING/`strftime` 等 SQLite 专有特性（ADR-0007 红线，达标）；`schema_migrations` 挪出迁移文件有注释说明（见 M10）。
4. **依赖白名单（ADR-0005）**: go.mod 直接依赖仅 `modernc.org/sqlite v1.56.0` + `golang.org/x/crypto v0.55.0`（均在白名单；`go.yaml.in/yaml/v3` 系 T-8 所加，亦在白名单）；间接依赖 9 个全部是 modernc 传递链；go.sum 54 行可控。
5. **测试覆盖 vs AC**: 33 测试全绿（-race）；AC ③ 列举的幂等迁移/upsert/ListByPrefix 边界/Token 摘要文件扫描/级联删（repo→nodes、user→tokens）/admin 两路径/postgres 拒绝逐一有对应测试且断言到位；keyset 分页 + 边删边流的回归钉子（gc_pagination_test.go）是超出 AC 的正确性投资，值得肯定。
6. **clean-room**: 纯 Go 惯用 SQL 代码，无 reverse-src 逐行对应嫌疑；权限两表形态溯源到 PRD/T-22 回写，非反编译产物。
7. **可维护性**: doc.go 文件地图准确；时间戳 Now() 单一来源 RFC3339 UTC；substores 双文件拆分合理。

## 五、范围外发现（交 conductor）

- 全模块 `go build ./...` 失败于 `internal/storage/engine.go:263` 语法错误与 `internal/config` 未定义符号——T-9/T-8 在途代码，与本票无关（实现者已在遗留 #2 说明）。
- `golangci-lint` 在本 review 环境不可用（command not found），lint 结论采信实现者日志；B1/M5 相关测试补齐后建议 conductor 侧复跑一次。

## 六、结论

REQUEST_CHANGES。B1 是带实测证据的正确性缺陷（跨大小写误删），必须修；B2 需要 conductor 当场裁决（推荐: DSN PRAGMA + NumCPU，一并消掉 per-connection FK 陷阱）。M1/M2 建议在 T-12/T-15 开工前完成裁决，改动都很小但窗口在此。其余按 minor 清单顺手处理即可，整体实现质量高——keyset 分页与边界匹配的设计意识明显好于平均水平。
