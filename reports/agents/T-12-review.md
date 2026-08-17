# 评审报告 T-12（视角: correctness — 事务边界/并发/错误处理）

结论: **REQUEST_CHANGES**

- 评审对象：`internal/repo/`（api.go / service.go / validate.go / repo_test.go / fakes_test.go）
- 依据：架构 §3.2/§3.3（T-25 回写版）、docs/reverse/repo-semantics.md §3/§4、PRD FR-3-AC4/AC5、reports/agents/T-12.md
- 验证命令（实际执行）：
  - `go test -race -count=1 ./internal/repo/` → ok 16.6s（复现日志声明）
  - `go vet ./internal/repo/`、`go vet ./internal/...` → 通过
  - `golangci-lint run ./internal/repo/...` → 0 issues
  - 独立并发探针（临时测试文件，跑完已删除，未触碰交付代码）：并发同名 Put（24 写者）、并发递归删 vs 写、prune 共享父目录、List 尾斜杠等价性

---

## 必须修改（blocking）

### B1（blocker）pruneEmptyParents 的「有存活子节点立即停」检查是盲的——会删掉仍有存活子节点的目录行

- 位置：`internal/repo/service.go:419-446`（检查点在 `:423` 的 `ListByPrefix` 调用）
- 机理：`parentPrefix`（validate.go:133-139）返回**带尾斜杠**前缀（如 `"d/"`），而 metadata 的 `likePrefix`（`internal/metadata/substores.go:186-193`，只读依赖）构造子树模式为 `prefix + "/%"` → `"d//%"`。路径中双斜杠被 `validateNodePath` 禁止，因此子树臂**永远匹配不到任何子节点**；查询只剩 exact 臂命中目录行自身（`path = 'd/'`），循环里 `c.Path == folder` 又把它跳过 → 目录被判定为空 → 删除，**无论还有多少存活子节点**，并且沿父链一路误删到根。
- 佐证（实现者自己的对照）：目录删除主路径 `service.go:381` 调 `DeleteByPrefix` 时**专门做了** `strings.TrimSuffix(path, "/")`，而 prune 的List 查询没做——同一文件内一处剥斜杠一处不剥，证明是疏漏而非设计。
- 运行时证据（独立探针，非实现者测试）：
  - `d/`（显式目录行）+ `d/one.bin`（删）+ `d/two.bin`（存活）→ 删 one.bin 后 **`d/` 行消失而 `d/two.bin` 仍在**；
  - 删子目录 `a/b/`（内有 a/b/c.bin，兄弟 `a/keep.bin` 存活）→ **共享父目录 `a/` 行被误删**。
- 后果：`Get("d/")` 从 ErrIsFolder 变 404、`List` 不再显示目录、用户 mkdir 的目录因无关同级文件的删除而消失；直接违反 T-12.md 自述 AC（「有存活子节点立即停」）与 repo-semantics §4 的 prune 行。
- 为什么现有测试没抓住：`TestDeletePrunesEmptyParents`（repo_test.go:522-550）只断言 prune 的**正向**（目录应消失），其注释自承「folder rows were never explicitly created」——「显式目录行 + 存活子节点」的组合从未被构造。
- 建议改法：`pruneEmptyParents` 查询前归一化，`ListByPrefix(ctx, repoKey, strings.TrimSuffix(folder, "/"))`（保留 `c.Path == folder` 跳过自身；exact 臂若命中同名**文件** `"d"` 视为存活子节点停下，是安全方向）。**并补回归测试**：显式目录行 + 删一个子节点 + 断言目录行与存活子节点俱在（叶子删与子树删两个形态）。

### B2（major，blocking）List 的 prefix 两种形态语义分裂，且工作日志的等价性声明是错的

- 位置：`internal/repo/service.go:460-480`；契约文本 `internal/repo/api.go:147-149`（"List returns every node under prefix"）
- 机理：同 B1 的 `likePrefix` 交互。`List("d/")` 的子树臂是 `"d//%"` → 只返回目录行自身；`List("d")` 返回目录行 + 全部子节点。探针实测：`List("d")=2 行，List("d/")=1 行`——**不等价**。
- 违反点：按 api.go 契约，`d/x.bin` 明明在 prefix `"d/"` 之下却不在返回集里。T-12.md 遗留 #2（`reports/agents/T-12.md:130`）声称「"a" 与 "a/" 等价……adapter 层如需严格目录语义请自行归一」——该声明**与事实相反**，会直接误导 adapter 票：adapter 目录浏览若按直觉传 `"dir/"`，将拿到空目录（只剩 marker 行）。
- 建议改法：Service.List 入口对 prefix 做 `strings.TrimSuffix(prefix, "/")` 归一化，使两形态真正等价（与 B1 的修法同源）；同步勘误 T-12.md 遗留 #2，并在测试中固化 `"d"` 与 `"d/"` 同结果。

## 建议改进（non-blocking）

### minor

- **M1 覆盖权限判定的 TOCTOU**（service.go:177-194 读旧 node 判权 → :276 putNode 内**重读** existing 后直接覆盖）：判权与写入之间节点可被并发改变。因 ACL 按 (repo, path) 判定而路径不变，主要暴露面是「t0 不存在（只查 WRITE）→ 写入时已存在」的窗口，无 DELETE 权限者可覆盖他人刚上传的内容。单实例 + SQLite 串行化下窗口极窄，M1 可接受；建议在 putNode 重读后复核，或 M2 引入条件 upsert（`WHERE sha256 = 旧值`）时一并解决。
- **M2 目录删的旁车文件误伤**（service.go:381 + likePrefix exact 臂）：`Delete("d/")` 的前缀归一为 `"d"`，exact 臂 `path = 'd'` 会连带删除与目录**同名**的文件 `d`（两者可合法共存）。建议前缀改为 `TrimSuffix(path,"/") + "/"` 的子树-only 形态或过滤返回集合。
- **M3 folder deploy 先耗尽 body 再判权**（service.go:214-219）：无写权限的主体可让服务端无条件读取并丢弃任意长 body。建议先判 `allow(ActionWrite)` 再 `countReader`；顺带处理 `countReader` 的读错误目前被 `_` 吞掉（读到 0 字节即出错会被当作空 body 放行）。
- **M4 目录递归删只对目录路径本身做一次 DELETE 判定**（service.go:373）：repo-semantics §4 文案（"needs DELETE permission … all artifacts under '<path>'"）隐含对子树逐项判定；M1 前缀式 ACL 下通常等价，但「仅授权 d/ 未授权 d/sub/」的用户可删 d/sub/。建议在报告/ADR 标注此简化，或逐子节点复核。

### nit

- 零权限幂等重传会刷新 node 的 `Mime`（service.go:283-288，测试 repo_test.go:382 传了 "text/plain"）：无 deploy 权限者可改写节点 mime。属「modified 系」的解读范围，建议明确是否保留原值。
- `CreateRepo` 的 Get→Create 判重有竞态（service.go:502-512）：并发同名建仓时第二个失败走 SQLite 主键约束 wrap，**不携带 ErrRepoExists sentinel**，httpapi 无法映射 409。建议对 `metadata.ErrDuplicate`/约束错误做兜底映射。
- `UpdateRepo` 无法区分「未提供 description」与「清空 description」（service.go:584 `current.Description = r.Description`）：只想改 config 的调用会意外清掉描述。config 有 `!= ""` 守卫而 description 没有，建议对齐。
- `DeleteRepo` 用 `ListByPrefix("")` 全表载入判空（service.go:612）：实现者已在遗留 #3 自报，M1 无碍；接口变更（CountByPrefix）走 architect。

## 已验证为正确的关键点（取证记录）

1. **Put 事务边界（最高优先项）**：blob-first 两语句顺序与 §3.2/§3.3 一致；`TestPutMetadataFailureRollsBackNode` 的 hookStore 注入点在委托给真 Store **之前**（fakes_test.go:236-244 check 先于调用），确能命中失败路径；「Commit 成功但 blobs.put 失败」窗口 = 盘上无引用 blob，符合 ADR-0006 GC 宽限出口；FK（migrations/sqlite/001_init.sql:46 `sha256 REFERENCES blobs(sha256)`）使反向顺序数据库层不可能。
2. **并发**：独立探针 24 并发同名 Put（内容 4 种）→ 最终 node 的 blobs 行与物理 blob 均在、Get 回读 sha 一致；并发递归删 vs 并发写 → 无悬挂 node（FK + WAL 串行化兜底）。`-race` 全绿。
3. **幂等重传免权限的测试构造真实**：`policyAuthz` 对无授权 alice 确实返回 false（fakes_test.go:151-166 空规则 → 默认拒绝），成功只能来自免检分支；同测试的「未带 checksum → 403 含 DELETE」反证 fake 没有放水。commitBlob 对 expect.Sha256 重校验，改内容不改声明的欺骗路径会被 ErrChecksumMismatch 拦截。
4. **写序 journal 断言有效**：`TestPutBlobFirstOrder` 在首个 nodes.put 处核对 journal 无 blobs.put 即报错 + 事后下标复核，乱序会被捕获；`TestPutPhysicalBlobBeforeMetadata` 在 blobs.put 时 Stat 物理文件，反向同样被捕获。时钟注入（固定时刻 + Advance）断言稳定。
5. **校验矩阵**：key `[a-z][a-z0-9-]{1,62}` 与 PRD FR-3-AC4 一致；`api`/`v2` 精确保留、`api-local` 放行（ADR-0008）；type/packageType 不可变；`ErrRepoNotEmpty` message 含 `deleteContent`（FR-3-AC5）。dot-segment/`//`/反斜杠/首斜杠/512 上限齐备；`%2e` 解码归 httpapi 层（见范围外）。
6. **sentinel/类型面**：全部错误经 `%w` wrap，errors.Is 可判（测试逐一断言）；`Principal = auth.Principal`、`AuditEvent = audit.Event` 真别名，Authorizer/AuditLogger 与 T-11 落地类型结构化吻合。

## 范围外发现（交 conductor）

- 评审窗口内 `internal/auth` 一度编译失败（token.go:39 `errTokenMissing` 未定义、errors.go `errors` 未使用）——T-11 agent 并发编辑所致，数分钟后自行恢复，`go build ./...` 终态通过。建议 T-11 review 确认终态。
- `%2e` 等百分号编码变体的解码责任在 httpapi（internal/httpapi 目前仅 doc.go，未实现）。本层防御到位，但 T-14/15 落地时必须「先 decode 再进 Service」，否则 `validateNodePath` 的 dot-segment 检查可被 `%2e%2e` 绕过——请在该票 AC 中显式固化。
- marker sha256（64 个 '0'）与真实内容碰撞：实际不可构造（需 sha256 原像命中常量；空 body 的真实摘要为 e3b0…，Commit 校验兜底）。无风险，仅记录。

## 修复验收标准（B1/B2）

1. 回归测试：显式目录行 + 存活子节点，删同级单文件 / 删兄弟子目录两种形态，目录行必须保留；
2. `List(repo, "d")` 与 `List(repo, "d/")` 返回一致（含子节点与目录行）；
3. 既有 24 测试全绿 + `-race`；T-12.md 遗留 #2 勘误。
