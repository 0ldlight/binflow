# T-128 正确性评审报告

**评审人**: code-reviewer  
**评审范围**: 并发安全、错误处理、资源泄漏  
**评审日期**: 2026-08-21  
**结论**: **APPROVE**（无阻塞性问题；一个低风险观察项已记录）

---

## 1. `internal/repo/service.go` — materializeAncestors / putFolderRow / ensureFolderLedger

### 1.1 并发安全（TOCTOU 竞态分析）

**putFolderRow 的幂等探测与写入时序** (line 829-866):

```
existing, err := s.md.Nodes().Get(ctx, repoKey, folderPath)  // 探测
...
if err := s.ensureFolderLedger(ctx); err != nil { ... }       // 写 blobs
...
if err := s.md.Usage().PutNodeWithUsage(ctx, n, s.now()); ... // 写 node
```

两个并发写入者同时到达同一个 ancestor 目录：

1. **两者都发现 existing == nil**（探测竞态，这是不可序列化的 snapshot read）
2. **两者都调用 `ensureFolderLedger`**：`Blobs.Put` 使用 `ON CONFLICT (sha256) DO NOTHING`（substores.go:200-201），所以第二次写入是安全的 no-op。无竞态问题。
3. **两者都调用 `PutNodeWithUsage`**：`PutNodeWithUsage` 使用 `ON CONFLICT (repo_key, path) DO UPDATE SET ...`（substores_usage.go:34），这是一个原子 upsert。即使两个写入者同时到达，SQLite 的串行化写入保证其中一个先完成，另一个做 UPDATE。因为两者写的是完全相同的值（marker sha、size 0、folderMime），结果等价。无数据损坏风险。

**结论**: 不存在 TOCTOU 导致的写入异常。`ON CONFLICT DO NOTHING`（blobs）和 `ON CONFLICT DO UPDATE`（nodes）的组合在并发路径下是正确的。

**一个观察项（非阻塞）**: 如果未来 `putFolderRow` 的更新逻辑不再是幂等的（例如 increment a counter），当前架构将需要 `SELECT ... FOR UPDATE` 或一个 advisory lock。但 ADR-0016 的语义下，folder 行是纯幂等的，不需要。

### 1.2 错误处理

逐错误路径检查：

| 位置 | 错误 | 是否 wrap 带上下文 | 是否吞错误 |
|------|------|-------------------|------------|
| putFolderRow:830 | `Nodes().Get` 非 `ErrNodeNotFound` | 是: `"node %s/%s: %w"` | 否 |
| putFolderRow:836 | `PutNodeWithUsage` 失败（idempotent 分支） | 是: `"idempotent folder redeploy %s/%s: %w"` | 否 |
| putFolderRow:850 | `ensureFolderLedger` 失败 | 是: `"folder ledger row: %w"` | 否 |
| putFolderRow:862 | `PutNodeWithUsage` 失败（新写入分支） | 是: `"node %s/%s: %w"` | 否 |
| materializeAncestors:814 | 任何 `putFolderRow` 失败 | 是: `"materialize ancestor %s/%s: %w"` | 否 |
| ensureFolderLedger:725 | `Blobs().Put` 失败 | 否（直接返回 `s.md.Blobs().Put(...)` 的错误） | 否（但缺少 wrap） |

**ensureFolderLedger 缺少错误 wrap**: `ensureFolderLedger` 直接返回 `Blobs().Put` 的原始错误，不加任何上下文。虽然调用方 `putFolderRow` 会对它做 `"folder ledger row: %w"` wrap，但如果未来有其他调用方直接调用 `ensureFolderLedger`，错误信息将缺少上下文。这是一个**低风险的可维护性观察项**。

**建议**（非阻塞）:
```go
func (s *service) ensureFolderLedger(ctx context.Context) error {
    if err := s.md.Blobs().Put(ctx, &metadata.Blob{...}); err != nil {
        return fmt.Errorf("ensure folder ledger row: %w", err)
    }
    return nil
}
```

### 1.3 事务边界

`materializeAncestors` 的每个 ancestor 目录写入是**独立的原子操作**（通过 `PutNodeWithUsage` 的 ON CONFLICT upsert），不是在一个大事务中。这是**正确的设计**：

- **好处**：一个 ancestor 写入失败不会回滚已成功的 ancestor（每个 ancestor 是独立的幂等操作）
- **崩溃场景**：如果进程在写入第 3 个 ancestor 时崩溃，前 2 个 ancestor 的 folder 行仍然存在（这是安全的——它们是"孤儿"目录行，不会造成数据损坏，后续写入会做 idempotent refresh）
- **与 ADR-0016 一致**：`"A crash past this point can only leave benign empty folder rows behind"` (putNode 注释, line 733)

### 1.4 Context 传递

所有数据库操作都正确传递了 `ctx`：
- `s.md.Nodes().Get(ctx, ...)` 
- `s.md.Blobs().Put(ctx, ...)`
- `s.md.Usage().PutNodeWithUsage(ctx, ...)`

无 context 泄漏问题。

### 1.5 父目录写入不继承 target 的 mime 类型

`materializeAncestors` 使用 `folderMime = "application/octet-stream"` (line 818) 作为 ancestor 目录行的 mime，而不是 target 写入的 mime。这是正确的设计——folder 行没有内容类型，固定值比可变值更可预测。

---

## 2. `internal/repo/validate.go` — ancestorDirs 函数

### 2.1 边界情况测试

| 输入 | 预期输出 | 实际逻辑 |
|------|---------|---------|
| `""` (空路径) | _(validateNodePath 会先拒绝)_ | `strings.TrimSuffix("", "/")` = `""`, `strings.Split("", "/")` = `[""]`, `len(segs)` = 1, loop `i=1; i<1` 不执行 → 返回 nil |
| `"f"` (根级文件) | 无 ancestor | `segs = ["f"]`, `len=1`, loop 不执行 → 返回 nil |
| `"a/"` (根级目录) | 无 ancestor | `segs = ["a"]`, `len=1`, loop 不执行 → 返回 nil |
| `"a/b"` (单层嵌套) | `["a/"]` | `segs = ["a","b"]`, `len=2`, `i=1` → `join(["a"], "/") + "/"` = `"a/"` |
| `"a/b/c"` (深层嵌套) | `["a/", "a/b/"]` | `segs = ["a","b","c"]`, `len=3`, `i=1` → `"a/"`, `i=2` → `"a/b/"` |
| `"a/b/c/"` (目录 target) | `["a/", "a/b/"]` | `TrimSuffix` strip 掉 trailing slash, `segs = ["a","b","c"]`, 同上。c 本身作为 target 被排除，符合 ADR-0016 规范 |
| `"/"` | _(validateNodePath 会先拒绝)_ | `TrimSuffix` → `""`, `Split` → `[""]`, 返回 nil |

### 2.2 排序正确性

`ancestorDirs` 返回 `i=1,2,...,n-1` 的顺序，即最外层（`a/`）优先。`materializeAncestors` 按此顺序遍历写入。这确保了如果崩溃发生在中间，最外层的 folder 行已经存在，内层缺失。这是安全的方向——pruneEmptyParents 能正确处理。

### 2.3 潜在问题：路径中包含连续斜杠

`validateNodePath` 已经拒绝了 `"//"` 和 `"\\"`，所以 `ancestorDirs` 不会收到无效路径。但 `strings.Split` 对于这样的路径会产生空 segment。防御深度足够。

**结论**: `ancestorDirs` 在所有合法输入下行为正确。排序正确（最外层优先）。

---

## 3. `internal/metadata/migrations/sqlite/007_folder_rows_backfill.sql` — 迁移 SQL

### 3.1 幂等性分析

**Step 1 — 哨兵 blob 行**:
```sql
INSERT OR IGNORE INTO blobs (...) 
SELECT '0000...0000', '', '', 0, strftime(...)
WHERE EXISTS (SELECT 1 FROM nodes WHERE instr(path, '/') > 0 AND instr(path, '/') < length(path));
```

- `INSERT OR IGNORE`：如果 marker blob 行已存在，整行被跳过（silent no-op）
- `WHERE EXISTS`：如果数据库中没有需要 backfill 的节点（"无 ancestor" 或 "已全部 backfill"），SELECT 返回 0 行，INSERT 什么都不做
- `strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`：每次执行可能产生不同的 created_at。但由于 `INSERT OR IGNORE`，第一次执行写入后，后续执行被忽略，**不会覆盖** 已存在的 marker 行。
- **幂等**：是。重复执行不会修改已存在的 marker blob 行。

**Step 2 — 递归 CTE + INSERT OR IGNORE**:
```sql
INSERT OR IGNORE INTO nodes (...) SELECT ... FROM walk GROUP BY repo_key, dir;
```

- `INSERT OR IGNORE`：如果 folder 行已存在（无论是 backfill 还是 explicit mkdir 创建的），整个行被跳过
- **递归 CTE** 始终产生相同的行集合（它是纯函数：`created_at` 从源 node 行读取，源 node 行不变）
- `MAX(stamp)` 确保 `created_at` 取最深（最年轻）的 descendant 的 created_at
- **幂等**：是。重复执行 = 0 行被插入（所有行都已存在）

### 3.2 递归 CTE 终止分析

```sql
WITH RECURSIVE walk(repo_key, dir, rest, stamp) AS (
  -- 基础情况：nodes 表中 path 包含 '/' 的每一行
  SELECT repo_key, substr(path, 1, instr(path, '/')),
         substr(path, instr(path, '/') + 1), created_at
  FROM nodes WHERE instr(path, '/') > 0 AND instr(path, '/') < length(path)
  UNION ALL
  -- 递归步骤：每次消费 rest 中第一个 segment
  SELECT repo_key, dir || substr(rest, 1, instr(rest, '/')),
         substr(rest, instr(rest, '/') + 1), stamp
  FROM walk WHERE instr(rest, '/') > 0
)
```

1. **基础情况**：`instr(path, '/') > 0 AND instr(path, '/') < length(path)` 确保路径中至少有一个 '/' 且不在末尾。这排除了根级路径（如 `"f"` 或 `"f/"`），它们没有 ancestor。
2. **递归步骤**：`WHERE instr(rest, '/') > 0` 确保当 `rest` 中不再有 '/' 时停止。`rest` 的长度在每一步中严格递减。
3. **终止保证**：对于路径 `a/b/c/file.bin`（5 segments）：
   - 基础：`dir="a/"`, `rest="b/c/file.bin"`（4 segments）
   - 递归 1：`dir="a/b/"`, `rest="c/file.bin"`（3 segments）
   - 递归 2：`dir="a/b/c/"`, `rest="file.bin"`（1 segment, 无 '/'）
   - 递归 3：`WHERE instr(rest, '/') > 0` → `instr("file.bin", '/')` = 0 → 停止
4. **无限循环**：不可能。`rest` 在每一步中严格递减（每次消费至少一个 '/' 和一个 segment），且递归条件 `instr(rest, '/') > 0` 最终会变为 false。

### 3.3 created_at 取 MAX 的逻辑

```sql
SELECT ..., MAX(stamp), MAX(stamp) FROM walk GROUP BY repo_key, dir
```

对于 ancestor 目录 `a/`，如果存在两个 descendant：
- `a/b/file1.bin` (created_at = "2026-01-01T...")
- `a/c/file2.bin` (created_at = "2026-06-01T...")

`walk` CTE 会产生两行 `(repo_key, "a/", ...)` — 每个 descendant 一条路径。`GROUP BY repo_key, dir` 合并它们，`MAX(stamp)` 取最年轻的 (`"2026-06-01T..."`)。

这与 putFolderRow 的语义一致：`existing.UpdatedAt = s.now()` — folder 的 mtime 跟随其最新的 descendant。

**正确性确认**：`stamp` 在所有递归步骤中保持为原始 descendant 的 `created_at`（不递增），所以 `MAX(stamp)` 确实取的是 "最年轻的 descendant" 的 created_at。

### 3.4 一个观察项（非阻塞）

**seed 谓词中的 `substr` 边界**：基础情况使用 `substr(path, 1, instr(path, '/'))` 获取第一个 segment（包括 trailing slash）。对于路径 `"a/b"`，`instr(path, '/')` = 2，`substr(path, 1, 2)` = `"a/"`。对于路径 `"a/b/"`，`instr(path, '/')` = 2，`substr(path, 1, 2)` = `"a/"`。

**但是**：`WHERE instr(path, '/') > 0 AND instr(path, '/') < length(path)` 对于路径 `"a/"` 的 `instr(path, '/')` = 2 且 `length(path)` = 2，条件 `2 < 2` 为 false，所以 `"a/"` 被排除。正确。

对于路径 `"a"` 的 `instr(path, '/')` = 0，被排除。正确。

种子选择正确。

---

## 4. 测试适配评估

### 4.1 repo_test.go 改动

| 测试 | 适配方式 | 是否正确覆盖 |
|------|---------|-------------|
| `TestPruneKeepsFoldersWithLiveChildren` | 注释更新（line 568-569）："Since T-128 (ADR-0016) the implicit directories are materialized" | 测试本身不依赖 folder 行材料化，只是注释解释了为什么行为一致。**正确** |
| `TestListPrefixFormsEquivalent` | `len(a) != 4 \|\| len(b) != 4` (line 672) — 从 3 改为 4，因为 `d/sub/` 现在也被材料化了 | **正确** — 确实验证了 `d/sub/` 出现在列表中 |
| `TestListPrefixFormsEquivalent` | `len(c) != 2` (line 687) — 预期 `d/sub/` 和 `d/sub/two.bin` 两行 | **正确** — 验证了深度前缀也包含 folder 行 |
| `TestDeleteFolderRecursive` | `len(paths) != 3` (line 799) — 预期 `keeper.bin`, `dx/sibling.bin`, `dx/` 三行（`dx/` 被材料化了） | **正确** — 验证了 `dx/` 出现在 residue 中 |
| `TestGetListPaths` | `len(all) != 4` (line 847) — 预期 `a/`, `a/one.bin`, `b/`, `b/two.bin` 四行 | **正确** — 验证了 folder 行出现在根列表中 |
| `TestPutFromBlobSha1Concurrent` | `n != 2` (line 1462) — 预期 2 个 blob（content + folder marker） | **正确** — 验证了 folder marker 出现在 blob count 中 |

### 4.2 landed_blob_test.go 改动

| 测试 | 适配方式 | 是否正确覆盖 |
|------|---------|-------------|
| `TestPutLandedBlobConcurrentSameDigest` | `n.Sha256 == metadata.FolderMarkerSHA { continue }` (line 413-415) — 跳过 folder 行后计数 file 行 | **正确** — 验证了 docker 路径 `acme/app/blobs/...` 的 ancestor `acme/` 和 `acme/app/` 被材料化了 |

### 4.3 docker_blob_test.go 改动

| 测试 | 适配方式 | 是否正确覆盖 |
|------|---------|-------------|
| `docker blob mount test` (line 574-575) | `n.Sha256 == metadata.FolderMarkerSHA { continue }` — 跳过 folder 行 | **正确** — 验证了 ancestor 被材料化 |
| `docker concurrent push test` (line 701-702) | `n != 7` — 预期 6 content + 1 marker = 7 blobs | **正确** — 验证了 folder marker 存在 |

### 4.4 评估结论

所有测试适配都是**真正的覆盖**，不是"跳过"。每个测试都验证了依赖 T-128 新行为的方面：

- **存在性断言**：验证 folder 行出现在列表/前缀查询中
- **计数断言**：将 folder 行纳入预期计数
- **区分性断言**：通过 `FolderMarkerSHA` 区分 file 行和 folder 行

没有发现"仅仅为了通过而跳过"的适配。

---

## 5. 综合评审结论

### 无阻塞性问题

1. **并发安全**：putFolderRow 的探测-写入竞态在 SQLite 的 ON CONFLICT 语义下是安全的。两个并发写入者产生等价结果。
2. **错误处理**：所有错误路径都正确 wrap 并返回。无吞错误情况。
3. **事务边界**：逐 ancestor 独立写入的设计是正确的（ADT-0016 明确允许崩溃后留下 orphan folder 行）。
4. **Context 传递**：全部正确。
5. **迁移 SQL**：幂等性正确，递归 CTE 终止保证正确，`MAX(stamp)` 逻辑正确。
6. **测试覆盖**：所有测试适配都真正验证了新行为。

### 一个观察项（非阻塞，低优先级）

- `ensureFolderLedger` (service.go:724-728) 缺少错误 wrap 上下文。建议添加 `fmt.Errorf("ensure folder ledger row: %w", err)` 以保持与项目中其他方法一致的错误 wrap 风格。

---

## 最终判定

**APPROVE**

无阻塞性问题。一个低风险观察项已记录，可在后续 tidy-up 中处理。