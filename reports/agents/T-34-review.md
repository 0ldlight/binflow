# T-34 评审报告（视角: correctness，单 reviewer）

- ticket: T-34 [P0] metadata：002_docker 迁移三表 + DockerStore 子接口
- 评审人: code-reviewer（correctness）
- 日期: 2026-08-18
- 评审对象: commit 6df3b96（internal/metadata 8 文件）+ 实现日志 reports/agents/T-34.md
- 结论: **APPROVE**（0 blocker / 0 major / 5 minor / 2 nit）

## 取证命令（实际执行）

```
$ go vet ./internal/metadata/...                          → 通过
$ go test -race -count=1 ./internal/metadata/...          → ok 6.558s
$ go test -count=1 ./...                                  → 11 包 ok；internal/adapter/docker 构建失败（T-33 在飞未跟踪文件，非本票产物）
$ /tmp 并发探针（模块内 -race 运行后已清理）:
  PROBE1-OK: same-digest cross-image refs survive DeleteManifest of the sibling
  PROBE1-OK: imageB tag survives
  PROBE2-DONE: putErrs=0（2000 轮 DeleteManifest+重建 vs 持续 PutRefs，无撕裂窗口）
```

## 逐项裁决

### 1. DDL 对照（重点 1）— 通过

002_docker.sql 与 architecture.md §6 定稿块（572-611 行）逐列逐约束比对：三表的列名/顺序/类型/NOT NULL/DEFAULT、复合主键顺序（manifests=(repo_key,image,digest)、tags=(repo_key,image,tag)、refs=(repo_key,image,manifest_digest,blob_digest)）、manifests/tags 对 repositories 的 ON DELETE CASCADE、refs 无 FK、三条索引（idx_docker_manifests_image / idx_docker_tags_image / idx_docker_refs_blob）全部一致。`TestDockerSchemaShapeMatchesArchitecture` 用 pragma_table_info/foreign_key_list 把该对照固化成测试，防止后续漂移。

**「两索引 vs 三索引」裁决**：AC ① 措辞（T-32.md 行 28「三表 + 两索引」）与架构 §6 定稿（实际三条 CREATE INDEX）不一致。AC 自身写明「严格按架构 §6 定稿 DDL」，定稿块是唯一契约，`idx_docker_refs_blob` 在定稿内有明确用途注记（删 blob 前查引用，对齐 idx_nodes_blob），且 `RefsByBlob` 正是它的消费者。**实现者按三条落地正确，AC「两索引」判定为笔误**，建议 conductor 顺手校正 T-32.md 存档措辞（架构文档无需动）。另注：从自家架构定稿块落 DDL 是规定流程，不构成 clean-room 问题（§6 是正向设计产物，非 reverse-src 翻译）。

### 2. 迁移安全（重点 2）— 通过

- **老库升级路径仿真的等价性**：`TestDockerUpgradeFromM1Database` 先按当前构建开库、写真实 M1 数据（repo/blob/node/admin 改密），再 DROP 三表+两索引、回滚 ledger version=2。得到的库形状（schema_migrations 只有 version 1、无 docker 表、M1 数据在场）与真实 M1 二进制写出的库逐项等价（001_init.sql 未变过）。断言覆盖：仅补 002、M1 数据原样、admin 口令不被种子覆盖、新表立即可写。等价性成立。
- **事务语句守卫覆盖面**：`TestMigrationsHaveNoTransactionStatements` 表驱动扫**全部**内嵌迁移（ToUpper 后子串匹配 BEGIN/COMMIT/ROLLBACK/START TRANSACTION），003+ 自动继承。方向 fail-closed（见 minor 3 的误报注记）。
- **生产库执行边界**：002 是纯 CREATE TABLE/INDEX、无数据回填，applyMigration 单事务包住 DDL+ledger 插入——失败整体回滚、ledger 无 version 2、下次 Open 干净重试；执行时长 O(1)（无全表扫描），WAL + busy_timeout(5000) 下不长锁。崩溃恢复由 SQLite 事务原子性兜底。安全。

### 3. 级联正确性（重点 3）— 通过（含探针实证）

- **误删他人 refs 不可能**：DeleteManifest 的两条级联 DELETE 均带完整 `(repo_key, image, digest/manifest_digest)` 谓词。同 repo+image 下 digest 即 manifest 身份（PK 三列一致），不存在「digest 相同但属于别的 manifest」的 refs 行；跨 image 同 digest 被 image 谓词隔离。探针 PROBE1 实证：imageA/imageB 共享同一 digest，删 imageA 后 imageB 的 refs（1 行）与 tag 均存活。
- **单事务**：DELETE manifests → DELETE tags → DELETE refs → Commit，defer Rollback 模式与 M1 一致；n==0 时事务内返回哨兵（回滚生效，级联不半执行）。
- **DeleteImage 并发 push 窗口**：tags → refs → manifests 同事务，幂等，返回 manifest 行数。SQLite 写事务串行化下，并发 push 要么先落（被清）要么后落（成为完整新状态），不存在半删镜像。测试覆盖幂等与跨 image 隔离；race 用例未含 DeleteImage（见 minor 5 建议）。
- **PutRefs vs DeleteManifest 竞态**：探针 PROBE2（2000 轮删除+重建 vs 连续 PutRefs）0 错误 0 残留。反向窗口（delete 先提交、PutRefs 后落 → 无 manifest 的孤儿 refs）由无 FK 契约覆盖且方向安全（孤儿 refs 只会让 GC 保守保活，不会误回收）——归 T-35 服务层删除时序，doc.go 已写明同类义务。

### 4. keyset 分页（重点 4）— 通过

`WHERE image > ? ORDER BY image LIMIT ?`：列未声明 COLLATE，SQLite TEXT 默认 BINARY（memcmp）——大小写（大写在前）、`/`（0x2F 先于字母数字）、`acme/team/app` 嵌套名全部确定性排序；WHERE 比较与 ORDER BY 用同一列排序规则，游标不会跳页/重页。测试覆盖 exclusive 游标（after "team1/app" → team1/web）、逐页走全量、大写在前、v10<v2 字节序陷阱、tag-only image 不入 catalog（事实源= docker_manifests 正确）。`LIMIT -1`（limitOrDefault）在 SQLite/Postgres 语义均为无限制，方言安全。

### 5. 接口面（重点 5）— 通过

- 导出面干净：仅值类型 DockerManifest/DockerTag/DockerRef、哨兵 ErrManifestNotFound/ErrTagNotFound（%w wrap，errors.Is 可判，测试断言）、接口 DockerStore、Store.Docker()；实现 dockerStore 未导出，`var _ DockerStore = (*dockerStore)(nil)` 编译期断言；无 *sql.DB/Tx 泄漏进签名。
- PutRefs 整体替换（DELETE+INSERT 同事务）：读侧 WAL 快照只见旧集或新集，探针实证无中间态。空集清空语义有测试。

### 6. 一致性契约（重点 6）— 通过

doc.go「docker_refs consistency contract (architecture 11.12)」专节完整：无 FK 的技术理由（复合主键部分列不可声明跨表 FK）、四条契约（DeleteManifest 同事务级联 / DeleteImage 三表同事务 / **拆库必须调 DeleteRepoRefs 否则 refs 成为永久 GC live 垃圾**（MUST 措辞，且 DeleteRepoRefs 接口注释二次写明调用方义务）/ GC mark = nodes ∪ docker_refs + 孤儿 tag 归服务层）。与架构 §11（第 754 行）逐点对应。

## 必须修改（blocking）

无。

## 建议改进（minor，均不阻塞合入）

1. **substores_docker.go:290-292 死代码**：DeleteImage 中 `if err != nil` 复检的是 BeginTx 已判过的外层 err（此处恒 nil），为重构残留，永不触发。建议删除这两行（纯清理）。
2. **PutRefs 静默丢弃 DockerRef 结构体键列**（substores_docker.go:195-198）：插入只取参数 repoKey/image/manifestDigest，refs 元素自身的 RepoKey/Image/ManifestDigest 字段被忽略且不校验。参数权威是合理设计，但接口注释未声明——T-35 调用方构造带键结构体时易误以为结构体字段生效。建议在 api.go PutRefs 注释补一句「the key fields of each DockerRef are ignored; the arguments are authoritative」，或加一行不一致即错的防御校验。
3. **硬编码版本 2 的两个测试在 003 落地时会假失败**：TestDockerMigrationIdempotent（want 2）与 TestDockerUpgradeFromM1Database（回滚仅 002、断言 v==2）未像 TestCurrentVersionFreshDatabase 那样从 loadMigrations 推导。属自愈型（失败即提醒演进），但与实现者自己定下的「跟随迁移数演进」口径不一致，003 票顺手统一。
4. **事务语句守卫的子串误报面**：`strings.Contains(upper, "COMMIT")` 会命中未来合法标识符（如 `committed_at` 列名）——fail-closed 方向可接受，建议 003 遇到时再收紧为语句级匹配，不必现在改。
5. **race 用例补 DeleteImage**：TestDockerConcurrentUpsertAndRead 已含 DeleteManifest 级联，建议 003/T-35 期间顺手把 DeleteImage 也纳入写者混合（探针已实证安全，纯测试加固）。

## nit

- postgres/README.md 可补一句：keyset 游标与 ORDER BY 必须落在同一列排序规则上（SQLite 为 BINARY；postgres 落地时避免 ICU 大小写不敏感 collation），防止方言实装时 catalog 分页跳页。
- `limitOrDefault` 为包级通用名，目前仅 docker store 使用；后续包内出现第二个同语义 helper 时考虑收敛命名。

## 范围外发现（交 conductor）

- `internal/adapter/docker/`（T-33 area，未跟踪在飞文件）当前**构建失败**：api.go:52 `undefined: newSessionRegistry`、name.go:44 `strings.Cut` 返回值数量错。非本票产物（本票 area 纪律严守，git status 中的 generic 改动为 T-36），但会阻塞全仓 `go test ./...` 与后续 qa。
- AC「两索引」笔误的 T-32.md 存档校正（见裁决 1）。

## AC 核对

| AC | 状态 |
|---|---|
| ① 002_docker.sql 三表+索引严格按 §6 定稿、无事务语句、幂等+老库升级单测 | 满足（三条索引为定稿正解，AC 笔误） |
| ② Docker() 子接口：CRUD+repo+image 查询+digest 级联，仅导出面，-race 覆盖 upsert/级联/keyset | 满足 |
| ③ M1 测试零回归、无 FK 契约进 doc.go | 满足（仅两处口径演进改动，无断言弱化） |
