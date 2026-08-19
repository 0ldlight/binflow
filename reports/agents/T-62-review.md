# 评审报告 T-62（视角: correctness）

- reviewer: code-reviewer（单 reviewer，正确性为主）
- 日期: 2026-08-19
- 对象: commit 54ed293（internal/metadata 003_remote_virtual.sql + substores_remote_virtual.go + api.go 扩展 + 测试）；工作树中 adapter/httpapi/repo 的改动属并行票 T-63 WIP，不在本票范围
- 结论: **APPROVE**（0 blocking / 5 non-blocking）

## 逐项核验

### 1. DDL 对照（架构 §6 定稿块）— 通过

- remote_cache 列集/PK(repo_key,path)/etag|last_modified|kind 默认值/FK→repositories ON DELETE CASCADE/idx_remote_cache_expiry 与架构 §6 逐项一致；pragma 形状测试（`TestRemoteVirtualSchemaShapeMatchesArchitecture`）钉死列集、PK 顺序、三个默认值字面量、两索引存在性、FK on_delete=CASCADE。
- remote_configs 扩列默认值 86400/600/0 钉死；`unreachable_mask` 消失、`blocked_out` 继承 001 默认 0（RENAME 保默认值，语义正确）；001 旧列（cache_ttl_seconds 等）保留且测试断言不被 003 丢失。
- `idx_blobs_sha1` 为架构 §6 003 块之外的增量，但派单 AC ② 明文授权（T-61「顺手 P2 缝：idx_blobs_sha1（T-73 sha1 秒传查询用）」），且 T-73 节注明「T-62 留缝」——归属清晰，非越界。
- RENAME 语义：`ALTER TABLE ... RENAME COLUMN`（SQLite ≥3.25；modernc.org/sqlite v1.56.0 捆绑 3.5x，满足）；remote_configs 上无引用旧列名的索引/视图，无重写残留风险。

### 2. 迁移安全 — 通过

- 事务性：migrate.go 对每个迁移体整体包一事务（applyMigration），003 的 4 条 ALTER + 2 条 CREATE 在 SQLite 中 DDL 可事务化——要么全落要么全无；文件体零 BEGIN/COMMIT，`TestMigrationsHaveNoTransactionStatements` 按文件表驱动守卫自动覆盖 003（核实测试遍历 loadMigrations 全集）。
- 幂等：ledger 守卫（version<=current 跳过）+ `TestRemoteVirtualMigrationIdempotent` 双开断言版本稳定不重跑。
- 老库升级真实性：`buildLegacyDatabase` 以原生连接手工 apply 001（或 001+002）体 + 写 ledger + 插真实存量行（M1: repositories/blobs/nodes + admin 口令；M2: docker_manifests），再走正式 Open 升级，断言存量数据/admin 口令不动、v=latest、新面立即可用——非 rewind 伪造，路径真实。`TestDockerUpgradeFromM1Database` 的 rewind 补齐 003 面（drop remote_cache/idx_blobs_sha1、RENAME 回 unreachable_mask、DROP 三列、清 ledger>1）顺序正确（先 drop 表再还列名，RENAME 回去保证 003 重放可再 RENAME）。
- 版本断言泛化（`latestMigrationVersion()`）合理：版本化迁移器下硬编码 2/3 必然过期；`v < 3 || v != latest` 双断言保留「003 已应用」的语义。

### 3. 子接口正确性 — 通过

- PutCache 同 key 双写窗口：单语句 `INSERT ... ON CONFLICT(repo_key,path) DO UPDATE`，SQLite 单写者串行 + WAL + busy_timeout 15s；本评审查额外注入 /tmp 探针（8 worker × 50 iter 同 key 并发 upsert+get，-race）：零错误、零 busy 逃逸、终态单行完整（last-writer-wins etag）。包内 `TestRemoteVirtualConcurrentWorkload`（-race）断言 40 行精确落库，通过。
- ListExpiredCache：`expires_at <= now`（边界含 now，测试钉死）+ `ORDER BY expires_at, repo_key, path` 确定序 + `limitOrDefault`（<=0 → -1 全量，与 docker 子存储同构）；吃 idx_remote_cache_expiry。
- SetMembers 原子替换：单事务 DELETE+按序 INSERT，position=0..n-1 稠密；重复成员撞 PK 回滚、前值完好（测试断言）；FK（成员/虚拟仓双侧）拒绝；删成员仓/虚拟仓/整仓三向级联均有测试。本评审探针（4 worker 并发 SetMembers 同一虚拟仓 + 每次 ListMembers 一致性校验，-race）：全程只观测到完整候选集，无撕裂读。
- 哨兵：ErrRemoteConfigNotFound/ErrRemoteCacheNotFound 全部经 `fmt.Errorf(...%w)` 包装，测试用 errors.Is 断言通过；FK 错误不被误包装成哨兵（有专断言）。
- Store 接口扩展兼容性：全仓 `metadata.Store` 消费者均为类型引用或 embed（如 audit_test 的 `failingStore struct{ metadata.Store }`），`go build ./...` 通过，无第三实现破坏。

### 4. 两桶序注释（T-79 勘误后口径）— 通过

- 003 迁移文件注释与 api.go（VirtualMember doc、VirtualStore doc）均落「两桶序——priorityResolution 标记成员优先桶在前、桶内声明序；local 不再绝对优先」，与 DECISIONS.md ADR-0013 联动记录①逐点一致；priorityResolution 存 repositories.config JSON 的归属已写明。T-71 消费面无歧义（见 non-blocking #4 的一处措辞补强建议）。
- position 采用全局稠密 0..n-1 而非按桶编号：按 (bucket, position) 稳定划分后桶内相对序==声明序，语义自洽。
- ADR-0012 勘误②④的 TTL 口径（DDL 86400/600 仅兜底、产品默认 7200/1800 归 T-64 建仓落值）已在迁移注释固化。

### 5. 遗留处置 — 合理

- T-73：索引已留、查询面不加（票内补）——边界干净。
- T-64：产品默认落值 + priorityResolution JSON 标记的口径已交代。
- T-66：password 列语义注释已固化；「无存量可加密」声明经核验属实（git grep 54ed293^ 全仓无任何 INSERT/UPDATE remote_configs 的生产代码）。遗留顺序风险见 non-blocking #1。
- 架构 §6 注释块旧口径（600↔1800 映射、「同类内次序」措辞）实现者已自曝为文档债，建议 architect 修订时同步——不阻塞代码。

## clean-room 抽查

无嫌疑。003 DDL 逐字对齐本项目架构 §6 定稿块（第一方文档），子存储实现沿用本包 001/002 既有范式（wrapExec/哨兵/limitOrDefault 同构）；reverse-src 为 Java 反编译参考，与本产出无逐行对应面。

## 取证命令与输出

- `go build ./...` → 通过；`go vet ./internal/metadata/` → 通过。
- `golangci-lint run ./internal/metadata/...` → `0 issues.`
- `go test -race -count=1 ./internal/metadata/` → `ok ... 28.342s`。
- 评审探针（internal/metadata 包内临时文件，跑后已删）：`TestReviewProbeSameKeyUpsertContention`（400 次同 key 并发 upsert，-race）PASS，终态单行 etag 完整；`TestReviewProbeConcurrentSetMembersSameRepo`（4×40 并发原子替换+一致性读，-race）PASS，无撕裂。
- `git show 54ed293 --stat` → 10 文件，无 go.mod/go.sum（零新依赖属实）。

## 必须修改（blocking）

无。

## 建议改进（non-blocking）

1. **T-64/T-66 顺序风险（交 conductor）**：ADR-0012 决策 4 要求 003 迁移内加密存量明文 + 有行无钥启动 fail-fast；本票以「零存量」（已核验）合理免做迁移内加密，fail-fast 归 T-66。但 T-64（批 2）先于 T-66（批 3）获得可用的 `CreateConfig`——若 T-64 在 T-66 落地前放行带 password 的建仓面，会写入 T-66 之外的无保护明文。建议 conductor 在 T-64 派单注明「T-66 落地前 Password 恒空串/不接受凭据入参」，或把 fail-fast 前移。
2. **pragma 钉死度可再紧一格**：`remote_virtual_internal_test.go` 未钉 remote_cache 非默认列（repo_key/path/fetched_at/expires_at）的 NOT NULL，也未钉 `idx_remote_cache_expiry` 的索引列（仅按名存在性）。未来误删 NOT NULL 或改索引列不会红。建议 tableColumns 同路补 `pragma_table_info.notnull` 断言 + `pragma_index_info` 断言索引列。
3. **过期序对时间戳格式纪律的依赖**：`ListExpiredCache` 的字典序==时间序仅对无小数秒的 RFC3339 成立（`01:00:00.5Z` 字典序小于 `01:00:00Z` 但时间更晚）。不变量已写在 `RemoteCacheEntry.ExpiresAt` 注释，但 store 不校验/不归一化 Kind 与时间戳形状；T-65/T-71 消费侧需统一用 `metadata.Now()`——建议在 T-65 派单注一句格式纪律。
4. **一处措辞补强**：`VirtualMember` 注释「position records declaration order within each bucket」与 `SetMembers` 实际写全局稠密 0..n-1 并存时，语义靠「稳定划分」隐式成立；建议在 VirtualStore 注释补一句「positions are globally dense across both buckets; buckets are a stable partition on (bucket, position)」，消除 T-71 实现者按桶重编号的误读空间。
5. **架构 §6 003 注释块同步债**（实现者已自曝）：仍为勘误前口径（600↔1800 字段映射、「同类内次序」）。归 architect 下次修订；两边暂差以迁移文件为准。

## 范围外发现

- 工作树 `internal/httpapi`/`internal/adapter`/`internal/repo` 的未提交改动为 T-63 WIP（本票零触碰）；T-62 全量回归时 httpapi 测试编译红系 T-63 占位 import，非本票问题。
