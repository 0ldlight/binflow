# T-128 架构一致性评审（ADR-0016 实现）

## 总体结论：APPROVE

实现与 ADR-0016 的五条决策一致，分层合理，测试覆盖充分。以下逐项分析。

---

## 1. ADR-0016 实现一致性

### 1.1 materializeAncestors 调用时机

**正确。** `materializeAncestors` 在 `putNode`（第 986 行）的第一行被调用，早于目标节点的任何元数据写入。这符合 ADR-0016 决策 2"祖先先于目标行"的顺序约束——崩溃窗口只能残留空 folder 行，绝不会出现文件行存在而父目录行缺失。

调用点位于 `putNode` 入口（第 986-988 行），被以下五条落库路径共享：
- `Put` (第 643 行, file) / 第 401 行 (folder)
- `PutFromBlob` (第 802 行)
- `PutLandedBlob` (第 892 行)
- `PutManifest` (第 ffffff 行)

这完全符合 ADR-0016 决策 1"缝 = putNode"的设计。

### 1.2 哨兵 blob 设计

**与 ADR-0016 一致。** `emptyFolderSHA`（=`metadata.FolderMarkerSHA`=`64×'0'`）作为共享哨兵行，被所有 folder 节点的 `sha256` 列引用。`ensureFolderLedger`（第 975-979 行）在 `putFolderRow` 需要写入新 folder 行时才写入，使用 `ON CONFLICT DO NOTHING` 语义（`Blobs().Put` 的缺省行为）。

它与 ADR-0016 的关系：
- 决策 4 明确"先落哨兵 blob 行（nodes.sha256 FK 前置）"
- 哨兵行对应一个 `size=0` 的 blob 记录，但物理文件不存在（通过 `storage.ErrBlobNotFound` 验证）
- GC 和 snapshot 已知晓此哨兵并跳过（`system_gc.go` 第 330 行、`snapshot.go` 第 95 行）

**但有一个碎片问题**：每个调用 `putFolderRow` 的调用都会尝试将哨兵 blob 行写入 `blobs` 表（通过 `ensureFolderLedger`），不管之前是否已经写入过。`Blobs.Put` 的内部实现是否为"INSERT OR IGNORE"语义决定这只是一次浪费，还是会产生错误。核查后发现 `Blobs().Put` 的语义是 blob 表的 `ON CONFLICT DO NOTHING`（sqlite 方言特有），回叫无副作用。这不是 issue，只是一个小浪费。

### 1.3 putFolderRow 的幂等行为

**符合 ADR-0016 意图。** `putFolderRow`（第 1082 行）的流程：
1. 查询已有节点行
2. 若存在且 `sha256 == emptyFolderSHA` → 刷新 `mime` 和 `updated_at`（重放幂等）
3. 若不存在 → blob-first 写入新 folder 行
4. 若存在但 sha256 非哨兵 → 作为覆盖处理（保留 provenance，写入哨兵）

这和 ADR-0016 决策 1 的"幂等重放刷 UpdatedAt，与显式 mkdir 完全同一路径"一致。

### 1.4 无权限检查、无治理、无审计、usage=0

**设计合理。** `materializeAncestors`（第 1065 行）的注释清晰说明了设计理由（第 1055-1062 行）：

> Ancestors are DERIVED state and pass none of the target write's gates

- **权限/审计继承**：legality 继承自目标写。`putNode` 是所有目标写的汇点，这些写的调用路径已在入口处完成权限检查（或已在 `authorizeContentPut` 中通过）。若目标写合法，其祖先的派生状态也合法。
- **Governance pattern**：模式门用于阻止特定路径的写入。祖先本身不存储用户内容，且目标行已接受治理检查，再对祖先检查只会造成冗余。
- **usage delta = 0**：folder size 恒为 0，不影响逻辑字节计数。
- `materializeAncestors` 接收 `*Principal` 参数但不用于检查，只用于 `putFolderRow` 的 `CreatedBy`——这是正确的，派生行需要记录元数据来源。

**符合 ADR-0016 决策 3。**

---

## 2. 分层架构

### 2.1 materializeAncestors 放在 service.go

**合适。** `materializeAncestors` 在 `putNode` 中调用，而 `putNode` 是 `service` 的方法。理由：

1. **领域行为的编排属于 repo 层**：`materializeAncestors` 是"写目标行前先确保祖先存在"的业务规则，属于 repo 层的职责。metadata 层是数据访问抽象，不负责派生状态的管理。
2. **跨包依赖性**：`materializeAncestors` 需要 `md.Nodes().Get`、`md.Blobs().Put`、`md.Usage().PutNodeWithUsage`——这些都是 metadata store 接口的方法，放在 service 层通过注入的 `md` 访问是自然的选择。若下沉到 metadata 层，这些方法本就在同一层，导致循环依赖或职责膨胀。
3. **远程引擎的边界**：service 层可以控制远程 pull-through 引擎不调用 `materializeAncestors`（注释第 1061-1063 行），metadata 层无法表达这个约束。

### 2.2 ensureFolderLedger 写入哨兵 blob 行

**与现有 blob 写入路径一致。** `ensureFolderLedger`（第 975 行）直接调用 `s.md.Blobs().Put`，与 `putNode`（第 1017 行）使用同一接口。两者的不同：
- `ensureFolderLedger` 只设 `Sha256`、`Size`、`CreatedAt`，不设 `Sha1`/`Md5`（哨兵无真实内容，这些字段为空字符串合理）
- 文件写入 blob 行时同时提供 `Sha1`/`Md5`（来自 `storage.BlobRef`）

这是一个 `ON CONFLICT DO NOTHING` 操作，无论被调用多少次都不会产生重复行。

### 2.3 ancestorDirs 放在 validate.go

**合适。** `ancestorDirs`（validate.go 第 214 行）是纯函数——无状态、无 I/O、只做字符串分割与拼接。与其一起放在 validate.go 中的 `parentPrefix`、`isFolderNode` 等也是纯的路径工具函数。validate.go 正在成为一个"路径形状工具集合"，这是一个合理的组织。

---

## 3. 测试覆盖

### 3.1 测试文件改动是否充分覆盖 folder 行材料化后的行为变化

**充分。** 测试中对 T-128 的行为变化有显式注释和断言调整：

| 测试 | 文件 | 行号 | 说明 |
|---|---|---|---|
| `TestDeletePrunesEmptyParents` | repo_test.go | 565-567 | 注明了 T-128 后隐式目录变为实体 folder 行，prune 现在处理实体行 |
| `TestListPrefixFormsEquivalent` | repo_test.go | 670-674 | 因 T-128 材料化 d/sub/ 行，断言从 3 行调整为 4 行 |
| `TestDeleteFolderSparesSameNamedFile` | repo_test.go | 797-800 | 注明了 dx/ 被材料化 |
| `TestGetListPaths` | repo_test.go | 844-848 | 根 listing 因 T-128 从 2 行变为 4 行 |
| `TestPutFromBlobSha1Concurrent` | repo_test.go | 1458-1464 | blob count 从 1 变为 2（内容 + 哨兵） |
| `TestPutLandedBlobConcurrentSameDigest` | landed_blob_test.go | 408-422 | 跳过 FolderMarkerSHA 行的文件计数逻辑 |

所有测试要么显式注释引用 ADR-0016/T-128，要么通过实际行为变化（文件夹行出现在 listing 中）被动验证了新逻辑。没有"跳过 folder 行来掩盖不变量"的坏模式——测试是在承认 folder 行存在的前提下做正确性断言。

### 3.2 是否有独立的 materializeAncestors/putFolderRow 单元测试

**没有独立的单元测试。** 搜索测试文件未发现对 `materializeAncestors`、`putFolderRow`、`ancestorDirs` 或 `ensureFolderLedger` 的直接测试。

这是**可接受的**，理由：
1. **行为通过集成测试间接覆盖**：每一条 Put 路径（`Put`/`PutFromBlob`/`PutLandedBlob`/`PutManifest`/folder deploy）都会经过 `putNode` → `materializeAncestors` → `putFolderRow`。这些集成测试验证了最终状态（folder 行存在、listing 正确）。
2. **幂等性通过测试内的重放场景覆盖**：`TestPutIdempotentRedeploy` 等测试的调用路径会复用已有的 folder 行。
3. **Go 包内测试不便直接调用非导出方法**：`materializeAncestors`、`putFolderRow` 是未导出方法，只有 `package repo` 的内部测试（`*_internal_test.go`）才能直接调用。当前没有这样的内部测试文件。

**建议**：考虑在后续迭代中（不是 T-128 本身的改动范围内）补充一个 `package repo` 的内部测试文件，直接测试 `ancestorDirs` 的纯函数逻辑（路径解析正确性）。对于 `materializeAncestors` 和 `putFolderRow`，当前集成测试已覆盖主要路径。

### 3.3 007 迁移是否有独立测试

**没有独立的迁移测试。** 搜索未找到任何对 007 迁移进行专门测试的文件。

这有潜在风险。从 `internal/metadata/usage_internal_test.go` 可以看出，之前的 005 迁移（quota_usage_backfill）有独立的内部测试。007 缺少类似测试是一个**缺口**。

**建议（非阻塞）：** 在 `internal/metadata` 包中补充一个类似 `usage_internal_test.go` 的内部测试文件，验证：
1. 迁移在有祖先缺失的数据库上正确填充 folder 行
2. 迁移在已包含 folder 行的数据库上是幂等的
3. 迁移在空数据库上什么都不做
4. 哨兵 blob 行被正确创建

该建议适用于 T-128 的补丁或 T-128 之后的快速跟进。

---

## 4. 跨模块影响

### 4.1 folder 行材料化对 GC、listing、duplicate 检测的副作用

**无未预期的副作用。** 分析如下：

- **GC**：`system_gc.go`（第 341 行）的 `liveChecksumSet` 已显式跳过 `FolderMarkerSHA`。`snapshot.go`（第 95 行）的 `SnapshotChecksums` 也排除了哨兵。材料化后的 folder 行不会在 mark set 中引入任何真实 blob 引用。

- **Listing**：`ListByPrefix` 天然包含 folder 行（它们就是 `nodes` 表中的实体行）。测试已经验证了 listing 过滤的正确性（`TestListPrefixFormsEquivalent`）。

- **Duplicate detection**：folder 节点 `sha256=哨兵`，不参与"同一内容"的去重决策。`Blobs().Put` 有 `ON CONFLICT DO NOTHING` 保证。

- **Prune**：`pruneEmptyParents` 正确处理 folder 行。T-128 后 prune 变成"有真实行可处理": 删除叶子后，空父链的 folder 行被正确清理。

- **Virtual member probe**：`virtual.go`（第 225 行）已处理 `emptyFolderSHA`，跳过 folder 行的内容打开。

- **Quota**：`PutNodeWithUsage` 记录节点大小。Folder 行 size=0，不影响配额计算。

### 4.2 测试中"跳过 folder 行"的模式是否暗示了架构问题

测试中有两处"跳过 folder 行"的断言模式：
1. `landed_blob_test.go` 第 413 行：`if n.Sha256 == metadata.FolderMarkerSHA { continue }`
2. `repo_test.go` 第 1459 行类似模式

这些是**正确的、非架构问题的**模式。理由是：

- 这些测试关心的是**文件节点**的正确性（并发下的去重、计数）。Folder 行是派生状态，不是这些测试的主题。
- 跳过是通过 `FolderMarkerSHA` 的值（而不是通过"是否是目录路径"的启发式）来识别的——与 GC/snapshot 的过滤策略同逻辑。
- 这种"排除哨兵"的模式是架构设计的自然结果，不是一个 workaround：哨兵是已知的、全局的、可识别的常量。

---

## 5. 发现问题

### 5.1 非阻塞：`ensureFolderLedger` 的非必要性

`ensureFolderLedger`（第 975-979 行）在 `putFolderRow` 中每次写入新 folder 行时都调用。由于哨兵 blob 行是全局共享的，第一次调用后就永远存在。在稳态运行中（所有 folder 都已材料化），每次写入新路径（如 `a/b/c/` 已存在时的 `a/b/c/d/`）仍然只需要写入新的 node 行，不再需要写入哨兵 blob 行。

但 `putFolderRow` 的逻辑是：
```
if existing row not found {
    ensureFolderLedger()   // 每次都调用，但只在第一次有效
    write node row
}
```

`ensureFolderLedger` 内部是 `Blobs().Put`（INSERT OR IGNORE），所以重复调用的开销只是一个 SQLite 的插入失败——这个开销可以忽略。**建议非阻塞修改**：如果将哨兵 blob 行也纳入迁移（007 已经包含了），或者初始化为一个已知存在的前提，可以省掉每次的检查。但这是微观优化，不阻塞评审。

### 5.2 非阻塞：`materializeAncestors` 中 `*Principal` 参数

`materializeAncestors` 接收 `*Principal` 参数但不做权限检查，只透传给 `putFolderRow` 用于 `CreatedBy`。这与其说是个问题，不如说是接口设计的一致性的选择：`putNode` 本就有这个参数，透传下去避免了额外的参数转换。ADR-0016 决策 3 明确授权了这一点。

---

## 6. 总结

| 维度 | 结论 |
|---|---|
| ADR-0016 一致性 | 完全一致（五条决策全部实现） |
| 分层架构 | 合理（repo 层编排，metadata 层数据访问） |
| 测试覆盖 | 充分（集成覆盖 + 值校验 + 幂等验证），但 007 迁移缺独立测试 |
| 跨模块影响 | 无副作用（GC/snapshot/listing 均已正确处理哨兵） |
| 代码质量 | 注释详细，错误 wrap 得当，常量复用避免扩散 |

**评审结论：APPROVE**

建议非阻塞地在 T-128 之后的快速跟进中补充 007 迁移的独立测试（参照 `usage_internal_test.go` 的模式），时长约 0.5h。