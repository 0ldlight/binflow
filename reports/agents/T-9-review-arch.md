# 评审报告 T-9（视角: 架构一致性 / 测试覆盖）

- reviewer: code-reviewer (arch)
- 日期: 2026-08-17
- 对象: `internal/storage/`（api/engine/session/gc/singleflight/digest + 3 个测试文件，2285 行）
- 依据: docs/design/architecture.md §2/§3.1/§4、DECISIONS.md ADR-0006、BOARD.md T-9 AC（只读）、reports/agents/T-9.md
- 结论: **REQUEST_CHANGES**（blocker 1 条；其余为 major/minor/nit 与 architect 回写项，无包边界/clean-room 违例）

## 0. 取证命令（本机实际执行）

```
$ go list -deps ./internal/storage | grep binflow        → 仅 github.com/lzwzzy/binflow/internal/storage（§2 红线 PASS）
$ go list -f '{{join .Imports "\n"}}' ./internal/storage → 全部 stdlib（context/crypto/encoding/errors/fmt/hash/io/os/path/sort/strings/sync/time）
$ go vet ./internal/storage/ && gofmt -l internal/storage/ → 通过 / 空
$ go test -count=1 -short ./internal/storage/              → ok 7.966s
$ go test -count=1 -race -run 'TestGC|TestClose|TestSweep|TestStartup' ./internal/storage/ → ok 2.875s
$ go test -list '.*' ./internal/storage/ | grep -c '^Test'  → 25（与日志一致）
```

## 1. 契约对齐（§3.1 逐条比对）

| 架构 §3.1 | 实现 (api.go) | 判定 |
|---|---|---|
| `BlobRef{Sha256,Sha1,Md5,Size}` | 一致 | PASS |
| `Session{ID,Append,Commit,Abort}` | 一致；nil-receiver 安全按 §3.1 实现 | PASS |
| `BeginSession/ResumeSession/Open/Stat/Delete` | 签名一致；ResumeSession M1 恒 `ErrSessionNotFound` | PASS |
| `GC(ctx, func(ctx,sha256)(bool,error), grace, apply)` | **偏离①**：`GC(ctx, func()(map[string]struct{},error), grace, apply)` | 见 §1.1，偏离合理 |
| Engine 无 `Close()` | **偏离②**：补入 `Close() error` | 见 §1.2，偏离合理 |
| sentinel 三个 | 另补 `ErrBlobCorrupt`、`ErrEngineClosed` | 加法式扩展，合理，但偏离记录漏列（nit-1） |

附：§3.1 接口注释写 `ErrNoSuchSession`，错误约定行写 `ErrSessionNotFound`——架构文档自相矛盾；实现取错误约定行的名字，是正确解法（回写项 H）。

### 1.1 偏离①评估：GC 回调集合形 — **合理，建议回写架构**

- 动机成立：逐条形 = N 次 DB 往返（blob 数即查询数）；集合形一次查询。票据/BOARD 与架构文档不一致，实现者按票据走并在 `api.go:60-63` godoc 报备，流程正确。
- 技术上更优：磁盘驱动 sweep（storage 扫盘 + 调用方给 keep-set）比「DB 反连接给候选 + 逐条复查」**更彻底**——能发现 blobs 表缺行/DB 回退到旧备份产生的孤儿；且 grace 窗口使集合快照与逐条复查的 TOCTOU 差异在工程上不成立（两者都不原子）。
- 代价（需记入回写）：① 全量引用集驻内存（1M nodes ≈ 100MB 量级），M6+ 规模需流式变体；② **涟漪效应实现者未报备**：§3.2 `NodeStore.FilterUnreferenced` 的 godoc 明说「供 GC mark 阶段」——集合形下 GC mark 变为 `SELECT DISTINCT sha256 FROM nodes`，FilterUnreferenced 对 GC 失去用途（§4.4「mark（FilterUnreferenced 反连接）」整句作废），需 architect 一并改。

**回写建议措辞**（§3.1 Engine.GC 处替换）：

> `GC(ctx context.Context, referenced func() (map[string]struct{}, error), grace time.Duration, apply bool) (removed []string, err error)`
> mark：`referenced` 一次返回全部被 nodes 引用的 sha256 集合（调用方 `SELECT DISTINCT sha256 FROM nodes`）；sweep：storage 扫描 `blobs/` 磁盘，未在集合中且文件 mtime 早于 `now-grace` 者为候选。dry-run（apply=false）只返回候选清单；apply=true 删除并返回实际删除清单。注：引用集常驻内存，M6+ 大库需评估流式接口变体。

同步修改：§3.2 `FilterUnreferenced` 注释改为「一致性检查/对账用（GC 已改用引用集合回调，T-9）」；§4.4 mark 段同步改写。

### 1.2 偏离②评估：Engine.Close() — **合理，建议回写架构**

§7.4 要求优雅停机「关 metadata → 退出」，metadata.Store 有 `Close()` 而 Engine 没有，属架构遗漏；生命周期对称是接口完备性的硬需求。实现（Close 排空活会话、幂等、`ErrEngineClosed` sentinel）符合预期。

**回写建议措辞**（§3.1 Engine 接口末尾追加）：

> `Close() error` — 关闭引擎：排空并丢弃在途会话目录（数据由下次启动清扫兜底）、拒绝后续 BeginSession（`ErrEngineClosed`）。幂等。
> 错误约定补：`var ErrBlobCorrupt`（Stat 完整性校验失败）、`var ErrEngineClosed`（Close 后的变更操作）。

## 2. 包边界与概念泄漏

- 零内部 import 已验证（§0 取证）；`doc.go:18-20` 明文声明解耦且与事实相符。
- 类型面只有 blob/checksum/session 概念：`BlobRef`/`Session`/`Engine`/sentinel 全部是存储词汇；GC 回调 `map[string]struct{}` 不携带 node/repo 形态。godoc 中提及 "nodes" 仅出现在注释说明调用方语义（`api.go:84`），属契约意图描述，可接受。
- **无违例**。

## 3. 磁盘布局契约（ADR-0006）

| ADR-0006 条目 | 实现 | 判定 |
|---|---|---|
| `blobs/<sha256[0:2]>/<sha256>` | `engine.go:110` blobPath，64 位小写 hex 校验（兼防穿越） | PASS |
| `sessions/<uuid>/{data,state.json}` | 常量 `data`/`state.json`，BeginSession 建两文件 | PASS |
| 落盘顺序 write→fsync(data)→rename→fsync(dir) | `session.go:113-144` 逐字对应，不可换序已遵守 | PASS |
| 并发 per-checksum singleflight、幂等去重 | `singleflight.go` + `session.go:127-145` | PASS |
| state.json = `{"id","created_at","received","sha256":null}`（§4.1） | 实现为 `{"version","id","created_at","received"}`：**多了 `version`、少了 `"sha256":null`，且 `received` 恒为 0** | **major-2**（见下） |
| GC：`created_at < now - grace`（§4.4，指 blobs 表列） | 实现用**文件 mtime**（`gc.go:91`） | 语义替换，合理但**未报备**（回写项 G） |
| 启动清扫 `created_at` 超 ttl 删除 | `engine.go:257-284`，mtime 回退，活会话保护 | PASS |

判定说明：
- state.json 属会话瞬态（ADR-0006 的兼容承诺是 `blobs/`+`sessions/` 目录命名；备份面 = blobs/ + SQLite），**不是**兼容性事故；但「文档说 A 代码做 B」必须消除——见 major-2。
- grace 基准从 blobs 表列换成磁盘 mtime 是 storage 自包含的必然选择（不能读 DB），且方向安全（mtime 被推新只会多保留）；但**备份/恢复工具若不保留 mtime 会重置宽限期时钟**，必须回写 ADR 供 M4 备份票与 ops 文档遵守。

## 4. blocking / major / minor / nit

### blocker（1）

1. **`api.go:26-27` `ErrEngineClosed` 契约注释与行为不符（文档说 A、代码做 B）**
   - 注释宣称 "returned by **any mutating operation** after Close"，但 `engine.go` 中只有 `BeginSession` 调 `checkOpen()`（engine.go:151）；`Delete`（engine.go:364）与 `GC`（gc.go:36）关闭后照常工作。下游 T-12（repo.Service，dep:T-9）正按 api.go 并行开发，若依赖「Close 后 Delete 必失败」的约定即踩坑。
   - 改法（二选一，推荐前者）：
     a. `Delete`/`GC` 入口加 `checkOpen()` 并 wrap `ErrEngineClosed`（约 8 行）；
     b. 把注释收窄为 "returned by BeginSession after Close"。
   - 无论选哪个，补一条表驱动测试钉死 Close 后 `BeginSession/Delete/GC/Open/Stat` 各自的行为（当前 `TestCloseRefusesNewSessionsAndCleansUp` 只测了 BeginSession 与双重 Close）。

### major（2）

2. **`session.go:47-52` Append 写错误路径：部分写导致「文件 vs 摘要」永久偏移，会话仍可复用，且零测试覆盖**
   - `io.MultiWriter(s.file, digests)` 在 file 部分写返回 `(n>0, err)` 时，digesters 未收到那 n 字节，而文件已落盘；会话不 finalize，调用方重试 Append 后文件内容 = 摘要输入 + 未计入前缀。此后 Commit 会把「内容与其名不符」的文件 rename 入库（ENOSPC 场景可达），未来同内容真实上传去重命中该坏 blob。
   - 建议：Append 出错即 `failLocked()`（会话作废，符合 ADR「绝不暴露半写」精神），并补注入失败 writer 的测试。
   - *注：此项本质偏正确性，按分工交叉确认——若正确性 reviewer 已立案，本条合并计。*
3. **state.json 与 §4.1 形状不符 + `received` 恒为 0 的 M2 陷阱**
   - §4.1 写 `{"id","created_at","received","sha256":null}`；实现写 `{"version","id","created_at","received"}`（多 `version`、删 `"sha256":null`）。
   - `received` 仅在 BeginSession 落盘一次（engine.go:162-163），Append 后从不更新——磁盘上恒为 0。M1 无读者（ResumeSession 恒失败、清扫只读 CreatedAt）故无实际影响，但 M2 chunked 续传实现者会拿到一个「字段在、值恒错」的文件。
   - 建议（本票内，小改）：在 `sessionState` 注释明写「M1: received 仅创建时写入恒 0，M2 chunked 需按 Append 维护」；字段形状差异走回写项 F。

### minor（4）

4. GC grace / 清扫 TTL 的**精确边界**未被测试钉死：`TestGCDryRunAndApply` 用 2×grace（远超）与 fresh（远内），`TestStartupSweepSessions` 用 23h/25h（非 24h 整点）。`age == grace` 保留（`<=`）与 `age == grace+ε` 删除的语义靠实现细节维持，重构改成 `<` 不会有测试报警。各补一行边界 case。
5. `api.go:83-87` GC godoc 未写 `grace <= 0 → DefaultGCGrace`（行为在 gc.go:40-42 且被 `TestGCZeroGraceDefaultsAndNilCallback` 钉死，但契约面上调用方看不到）。补一句。附带设计提示：grace=0 对 ops 是合法意图（立即回收），静默升级为 24h 是单向无害 footgun，M1 可接受，回写时明确。
6. Close 后 `Open`/`Stat` 的行为无测试（与 blocker-1 的补测可合并）。
7. `TestSessionStateOnDisk`（日志称「state.json 契约」）只断言 `id` 存在、`sha256` 不存在，未钉 `version`/`created_at`/`received` 键——布局契约测试应把 JSON 键集钉全。

### nit（2）

8. 偏离记录（T-9.md §四）漏列 `ErrBlobCorrupt`/`ErrEngineClosed` 两个 sentinel 增项；已并入回写清单 C。
9. 测试 fixture 权限（0o755/0o644）宽于产品（0o700/0o600）；模拟崩溃残态无碍，仅提示一致性。

## 5. 测试覆盖矩阵（AC 全分支）

AC 分支：去重 ✓（TestCommitIdempotentDedup）/ 摘要不匹配（含三种 algo、错长、非 hex）✓ / Abort 零残留+终结后操作 ✓ / 并发收敛 2/10/32 ✓ / 崩溃窗口三断点 ✓ / 启动清扫（TTL 档/回退/活会话）✓ / GC dry-run vs apply、在途引用、异构残渣、nil 回调 ✓ / 512MB 流式 RSS + 外部工具交叉 ✓ / Open-Seek ✓ / 多次 Append ✓ / ResumeSession ✓ / 损坏检测 ✓ / Close ✓。

缺口（除上列 4/6/7 外）：Append 中途失败的语义分支（→ major-2）；Commit 前置 ctx 取消分支；Abort(ctx 已取消) 的「无条件清理」语义——均无测试。

## 6. clean-room 抽查

无逐行对应嫌疑：代码为地道 Go（接口/错误 wrap/表驱动），无 Java 结构痕迹；逆向规格（docs/reverse/storage-layout.md）记载 Artifactory 以 sha1 为 PK、sha256 不参与路径，而实现按 ADR-0006 用 sha256 寻址——正是逆向规格自己建议的「行为等价、磁盘不兼容」路线，证明实现走的是规格→ADR→代码链路而非抄代码。**PASS**。

## 7. 转交 architect 的回写清单（10 条）

| # | 位置 | 措辞/动作 |
|---|---|---|
| A | §3.1 GC 签名 | 按 §1.1 措辞替换（集合形回调 + 内存代价注记） |
| B | §3.1 Engine | 补 `Close() error`（§1.2 措辞） |
| C | §3.1 错误约定 | 补 `ErrBlobCorrupt`、`ErrEngineClosed` |
| D | §3.1 Open 注释 | 补「返回的 BlobRef 只填 Sha256+Size；sha1/md5 以 metadata blobs 表为准（无 sidecar，ADR-0006）」——api.go:71-74 已如此实现，架构未写 |
| E | §3.1 Stat 注释 | 「轻量探测」与实现（全量读内容重算三摘要并验证自洽，O(size)）不符；改写为完整性校验语义，或注明大 blob 代价；是否另设轻量变体由 architect 定 |
| F | §4.1 state.json | 形状改为 `{"version":1,"id","created_at","received"}`；注明摘要不落盘、M1 received 恒 0（M2 维护） |
| G | ADR-0006 决策5 / §4.4 | grace 基准明确为 **blob 文件 mtime**（非 blobs.created_at）；加注「备份/恢复必须保留 mtime（tar / rsync -a 默认保留），否则宽限期时钟重置」 |
| H | §3.1 行内注释 | `ErrNoSuchSession` → `ErrSessionNotFound`（与错误约定行统一） |
| I | §3.2 + §4.4 | FilterUnreferenced 的「供 GC mark」定位作废（集合形下 GC mark = DISTINCT nodes.sha256），改为对账用途或标注保留原因 |
| J | §4.4 / config 文档 | 明确 session_ttl/grace 的零值=默认语义（T-9.md §四第 4 条已报备） |

## 8. 范围外发现（交 conductor）

1. `.golangci.yml` 全局排除 gosec G301/G304/G306/G401/G115/G204（T-7 所有，实现者已自报）：G304（路径穿越）/G204（命令注入）全局熄灭影响全仓安全告警面，建议收窄为 per-path；同时熄灭了 T-8/T-10 的 3 处 nolint。需 T-7 owner 与 conductor 决策。
2. 清扫仅启动时执行（ADR 字面）：长驻进程中被遗弃会话目录要等重启才清，可考虑后续票加周期 ticker（`sweepSessions` 已就绪，接线即可）。
3. GC 引用集全量驻内存：M1 无碍；blob 到千万级时需流式变体（并入回写项 A 注记）。

## 9. 结论

- 包边界、clean-room、落盘顺序、目录命名：全部 PASS。
- 两处报备偏离（GC 集合形、Close）均**合理**，建议按 §1.1/§1.2 措辞回写架构而非改代码。
- REQUEST_CHANGES 仅由 blocker-1（`ErrEngineClosed` 注释/行为二选一对齐 + 补测）触发；major-2 建议与正确性 review 合并裁决。修复 blocker 后无需再次架构评审。
