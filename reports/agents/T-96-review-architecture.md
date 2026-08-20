# T-96 架构评审（双 reviewer 之一：架构视角）

- 评审对象: commit `75c6d95`（已核对 HEAD 工作树与该 commit 在本票文件上零差异）
- 输入: reports/agents/T-96.md 申报段、reports/agents/T-88.md T-96 节 AC、ADR-0005/0006/0012/0013/0015、architecture §3/§4.1/§4.4/§7.6/§11.19、代码（datalock* / backup.go / snapshot.go / cmd backup.go / main.go / engine.go）
- 结论: **APPROVE**（零 blocker；8 项 non-blocking，其中 §7.6 勘误建议采纳并附草案）

---

## 一、六个重点审查区逐项裁决

### 1. 锁原语契约（R9 申报）——成立，适合 T-94 消费

**签名与语义**。`storage.AcquireDataLock(dataDir, op) (*DataLock, error)` + `Release()`（幂等、nil 安全）+ 哨兵 `ErrDataLockHeld`（`internal/storage/datalock.go:50/29/105`）：

- **锁粒度选 data 目录而非 db 路径是正确裁定**：GC 与 export 共享的可变资源是 `blobs/` 树；db 一致性由 `VACUUM INTO` 自身保证（在线安全），不该由锁承担。显式 dsn 下 db 在别处时锁仍然成立，这正好是需要的性质。
- **跨进程 + 跨线程同真**：flock 按 open file description（`datalock_unix.go:16`）、LockFileEx 按文件句柄（`datalock_windows.go:37`），serve 进程内 REST gc 与 CLI export 进程同目录真实争用成立；已有双向单测 + python flock 互操作真机验证。这是 T-94 面的硬前提，已满足。
- **fail-fast 不排队**有明确文档理由（排队的 export 会无限期扣押输出目录）——与 ADR-0015 勘误③「409 / 退出码非 0」姿态一致。
- **锁文件常驻不删**（避免 unlink-while-waiting 经典竞态）——flock 生命周期处理正确。
- **未来维护模式复用**：当前仅互斥档（exclusive-only）。未来若出现「维护模式（只读档）」需求，可加 `AcquireDataLockShared` 而不动既有调用方——缝在，不为想象买单，符合本里程碑口径。

**锁文件位置 `<data>/.maintenance.lock` 的三个暴露面全部核实无虞**：

| 消费面 | 结论 | 依据 |
|---|---|---|
| GC 扫描 | 不可见 | `internal/storage/gc.go:63/75`——sweep 只扫 `<root>/blobs/` 下的 2-hex 分片目录，data 目录根的外来文件本就不在扫描面 |
| export 拷贝 | 不会带走 | `internal/storage/backup.go:184-217` CopyBlobsTree 只拷 `blobs/<2hex>/<64hex>` 形态；且 `checkDirDisjoint`（cmd/backup.go:465）双向拒绝嵌套，锁文件不可能经路径重叠泄入产物 |
| import 空目录检查 | 显式豁免 | cmd/backup.go:492-506 唯一豁免 `.maintenance.lock`（锁残留是运行痕迹非状态），其余任一 entries 即拒 |

放置在 data 目录内的理由（所有 ADR-0004 部署形态下「能看到 blobs 的进程都能看到锁」，无带外协调通道）在 datalock.go:11-15 注释成立，对 PVC/compose volume/systemd 三形态皆真。

**op 参数封闭性**：开放字符串、仅作诊断（写入持有者记录 `pid=<n> op=<op>`）。可接受，但值域事实上收敛为 {gc, export}（T-94 将再加 REST gc），而 T-94 需要按 holder op 渲染「export in progress」文案——建议钉住词表（见 N2）。

### 2. kernel32 直调 vs x/sys——取舍成立

- ADR-0005 依赖门：白名单为 sqlite/yaml/x/crypto 三项，x/sys 现为 indirect（经 modernc.org/libc 传递）。为 40 行代码把 x/sys 提 direct 与「默认 stdlib、引入须 architect 记录」的准入原则不成比例。三候选实际对比过：提 x/sys（净增 direct 依赖）/ O_EXCL 锁文件+删除（stale 持有者 + unlink 竞态，代码注释明确点名否决）/ kernel32 直调（76 行，Win32 稳定 API）。选第三者成立。
- 代码质量：winbase.h 常量值注明出处、`ERROR_LOCK_VIOLATION(33)` 正确映射哨兵、`errno==0 → EINVAL` 防御、Overlapped 零偏移 + 1 字节区间是标准形态；per-handle 语义给了跨线程互斥（与 POSIX per-process 记录锁不同，注释表述正确）。
- 交叉编译矩阵：`GOOS=windows` build+vet、linux/amd64、linux/arm64 全过（日志自测）；darwin 运行时真机验证。运行时未验已诚实入遗留②（M5），风险受控（最坏情形 = Windows 上互斥失效，W25b 出洞——列入 M5 QA 矩阵即可）。
- 一处已知分叉需点名（见 N3）：Windows 下独占锁拒绝其他句柄**读**被锁字节，争用方的 holder 诊断读取会失败降级为 "unknown"（unix 无此问题，flock 是劝告锁不拦 I/O）。仅诊断面，不阻塞。

### 3. 分层——干净，无循环依赖

- **cmd 无 SQL**：backup.go 全部经 `metadata.Open` / 快照检查函数 / `storage` 助手 / `audit`；与 main.go 既有「cmd has no SQL handle by design」教义一致。gc mark 在 cmd 走 Store 面（main.go:612-640）、快照检查在 metadata SQL 直查——各自守住了所在层的规则，快照直查的理由（制品不是活库，不能经 Open——会跑迁移+种子）成立且已注释。
- **storage 无 metadata**：backup.go 纯 FS/JSON；**metadata 无 storage**：snapshot.go 纯 SQL。双向零 import。
- **engine.blobPath 委托 `storage.BlobPath`**（engine.go:114-116）：同包内委托，**结构上不可能引入循环依赖**；且 ADR-0006 的布局兼容承诺（blob 路径形态）从此只有唯一定义点，engine/export/import 三消费方同源——这是去重的正确方向。
- `metadataSnapshotter` 接口定义在消费侧（cmd/backup.go:61）是 Go 惯用法，Store 接口（§3.2）不被快照能力污染，未来 pg store 可另行满足。快照检查函数为包级函数（操作文件而非 store）同样不该上 Store 接口。
- Manifest 类型放 storage 可接受：manifest 描述 blobs + 一个不透明 db 文件，无元数据知识；为 M4 单建 `internal/backup` 包属过度分层。

### 4. ADR-0015 对齐——机制内核三条全中；表面形态按 PRD 走需勘误回写

| 决策 | 核对结果 |
|---|---|
| 决策 3（顺序） | **机制内核完整**：锁 → 快照先 → purge → 拷贝 blobs → manifest；引用集 = 快照自身 nodes ∪ docker_refs（非磁盘现状）；窗口内多余 blob 允许且文档化为普通 GC 候选；导出侧引用缺失 fail-fast（反向悬空引用被堵在源头）。✔ |
| 决策 4（import 仅空实例 / 迁移链） | 空目录检查先于读备份（目标永不被污染）；schema 天花板（快照 > 本 build 嵌入集顶 → 拒；旧快照恢复首开自动升迁，`LatestSchemaVersion` 动态取值不硬编码——T-95 并行落 005 无感的处理正确）；写序 db 先 blobs 后；无半恢复。✔ 「幂等」释义为清空目标后重复导入等价（ADR 原文「db 覆盖 + tar 覆盖」是 tar 形态时代措辞，与 PRD 空 data dir 口径有内在张力——勘误一并收口） |
| 决策 5（窗口语义 / 入口形态） | import 仅 CLI 不走 REST ✔（GE-09 404 已验）；export 的「admin REST 异步触发」M4 不做（PRD GE-09 裁定 `/api/export/**` 404）——**PRD 后出且为 W 序列验收锚，按 T-88 R1 先例（PRD 命名面 + ADR 机制内核）处理正确，但 architecture §7.6 与 ADR-0015 决策 3/5 的产物形态表述已过时，需勘误（见 N6 + 第三节草案）** |
| `--tar` P2 | flag 已定义 + 显式 fail-fast「not implemented in M4」——诚实的 P2 姿态，接口面先发布，行为面文档化。口径与 PRD FR-32-AC7 一致 |

### 5. export.run 审计写源实例——语义正确

快照在 purge+hash 后即封存，`export.run` 落在**活库**（cmd/backup.go:228）；恢复侧不含该事件，`import.run` 落**恢复库**（:374）。这是对的：审计是**每实例自身的历史**，备份是时点影像——若把 export.run 写进快照，等于在每一次未来恢复中伪造一条「该实例上从未发生的导出」历史；事件归属「事件发生在谁身上」的对称性（export 发生在源 → 源；import 发生在目标 → 目标）成立。写入活库在在线 export 契约下是常规写（锁只排 GC/export，不排业务写），与 VACUUM INTO 互不干扰（不同文件）。actor=admin 对齐 T-94 对 gc.run 的 CLI 口径（CLI 无可命名 principal，已知限制有据）。

### 6. T-94 消费面预留——够用，建议两处加固

- `errors.Is(err, storage.ErrDataLockHeld)` → 409 映射：哨兵已就位。✔
- 409 文案素材：争用错误串已含锁路径 + `last holder: pid=<n> op=<op>`，足以派生 ADR-0015 勘误③要求的「export in progress」文案与持有者详情。✔
- 加固点见 N1（holder 无编程面读取入口，跨包契约目前只活在注释里）与 N2（op 词表未钉）——建议随 T-94 派单注记解决，不构成本票返工。

---

## 二、发现清单（blocker：无；non-blocking 8 项）

**Blocker：无。**

### Non-blocking

- **N1｜锁持有者信息缺编程面入口**｜`internal/storage/datalock.go:84`（`lockHolder` unexported 且吃 `*os.File`）｜T-94 的 area 是 `internal/httpapi/system`，摸不到 storage 内部；若靠解析错误字符串取 holder 则脆（文案即契约）。锁文件首行格式 `pid=<n> op=<op>` 已是跨包事实契约但只存在于 doc 注释。｜建议：T-94 派单注明二选一——(i) 409 detail 直接引用 acquire 错误 message（最小改）；(ii) T-94 area 增 5 行导出助手 `storage.LockHolder(dataDir string) string`（可测、结构化，推荐）。
- **N2｜op 词表未钉**｜`internal/storage/datalock.go:41/50`｜T-94 需按 holder op 渲染「export in progress」，字符串漂移（"gc" vs "rest-gc" 之类）会静默破坏文案分支。｜建议：doc 注释钉死已知值 {gc, export} 或补 `LockOpGC/LockOpExport` 常量（可随 N1(ii) 同改）。
- **N3｜Windows 持有者诊断降级**｜`internal/storage/datalock_windows.go:38` + `datalock.go:84`｜独占字节锁在 Windows 拒绝其他句柄读该字节 → 争用方读 holder 记录失败，降级 "unknown"（unix 正常显示）。仅诊断面。｜建议：并入遗留②（M5 Windows QA 矩阵）；T-94 的 409 文案不对 Windows 承诺持有者详情。
- **N4｜export 对错误/空 data dir 会「制造成实例」**｜`cmd/binflow-server/backup.go:113-131` + `internal/metadata/store.go:61-82`｜`AcquireDataLock` MkdirAll + `metadata.Open` 对不存在的库执行迁移+种子 → `-c` 配错路径的 export 以退出码 0 产出一个 manifest 自洽的「空备份」，且在错误路径留下 binflow.db。名义只读的操作不应凭空制造实例状态。｜建议：export 在 Open 前 `os.Stat(sqlitePath)` 缺失即拒（「nothing to export」）；P2，可并入 T-94 或文档票。
- **N5｜引用集双实现的漂移风险**｜`internal/metadata/snapshot.go:90-93`（SQL 直查）vs `cmd/binflow-server/main.go:612-640`（Store 面遍历）｜两处各自有分层理由（快照是制品不能走 Store；cmd 无 SQL），但都编码了「nodes ∪ docker_refs」——未来引用事实扩张（新引用表）需两处同步，否则 manifest 边界与 GC mark 集分叉（manifest 引用 GC 会删的 blob，或反之）。｜建议：技术债台账记一条 + 补一个等价性钉子测试（同一 seed 库上 `liveChecksumSet` == `SnapshotChecksums`，cmd 侧可测）；可随 T-94 QA 或后续小票。
- **N6｜architecture §7.6 过时四处 + ADR-0015 形态措辞**｜`docs/design/architecture.md:842-845`｜①「tar blobs/ / 产物 = db + blobs.tar + manifest」vs 实现目录形态（PRD FR-32 W28：`metadata.db` + `blobs/` + `manifest.json`，`--tar` P2）；②「CLI export `--out`」vs 实际 `--output`；③「admin REST 异步触发（产物落 data_dir/exports/）」vs GE-09 无 REST 面（404）；④ import「非空 409」为 REST 语态（CLI 实为退出码非 0）、「启动 GC dry-run 报差异」未实现（PRD 无此 AC，多余 blob 由常规 GC 收敛）。ADR-0015 决策 3 的「blobs.tar」与决策 5 的「admin REST 可选触发」同源过时。**采纳日志遗留⑥，勘误草案见第三节。**
- **N7｜§11.19 标题自相矛盾**｜`docs/design/architecture.md` §11 第 19 条｜标题「备份不含 remote_cache/web_sessions」与正文「db（含 remote_cache 表）」矛盾；实现按正文语义（remote_cache 随 db、web_sessions purge）正确。｜建议：勘误票顺手续写标题为「备份不含 web_sessions（remote_cache 随 db 走）」。
- **N8｜未来 pg store 的 import 路径无 driver 快速失败**｜`cmd/binflow-server/backup.go:335-363`｜export 侧经接口断言优雅拒绝（「SQLite only in M4」）；import 侧按配置 driver 走，pg store 落地后会在 Open 处以连接错误形态失败而非清晰的「import 仅支持 sqlite」。M4 postgres 未交付，无实害。｜建议：pg 票的 backlog 注记，现在不动。

### 值得保留的优点（评审确认，不要求改动）

- `BlobPath` 收敛为布局唯一定义点（engine/backup/import 同源）。
- manifest `Validate` 不触文件系统、import 侧再叠字节级校验——结构校验与存在性校验分层清晰；`metadata.file` 裸文件名防穿越在 Validate 内。
- 快照检查绕过 `metadata.Open`（防对制品跑迁移+种子）、purge 不带 WAL pragma（不污染制品 journal mode、不留伴文件）——两个「制品不是活库」的细节都想对了。
- `--verify` 的 spot 样本 = sha 排序后前 100，确定性可复现；spot/full 的 P0/P1 分界有固化测试。
- 失败清理对称完备：export 清产物、import 清目标、均无半成品。

---

## 三、§7.6 勘误草案（供 conductor 转建筑票；机制内核不变，形态以 M4 PRD 为准）

> **勘误（2026-08-20，T-96 架构 review，依 M4 PRD FR-32/GE-09 与 T-88 R1 先例「PRD 命名面 + ADR 机制内核」；顺序硬规则、空实例、mtime、互斥锁内核均不变）**：
>
> §7.6 export 条目修订为：
>
> - **export（在线，仅 CLI）**：① 取 data 目录维护锁（`storage.AcquireDataLock`，锁文件 `<data>/.maintenance.lock` 0600，flock/LockFileEx——GC↔export 双向互斥，ADR-0015 勘误③；REST gc 面与 CLI 同原语，见 T-94）→ ② `VACUUM INTO` SQLite 一致性快照，落 `<out>/metadata.db`（快照内 purge `web_sessions` 后再计 manifest 哈希，§11.19）→ ③ 拷贝 `blobs/<2hex>/` **目录树**（保 mtime+权限位，ADR-0006 勘误②）→ ④ `manifest.json`（引用集 = **快照自身** nodes ∪ docker_refs；窗口内多余 blob 允许，import 后为普通 GC 候选；导出侧引用缺失 = fail-fast）。**顺序不可换**：先 DB 后 blobs，多余 blob 无害（恢复后常规 GC 收敛）；反向产生悬空引用。产物 = `<out>/metadata.db` + `<out>/blobs/` + `<out>/manifest.json`（**目录形态**；`--tar` 单文件产物 P2 债务，flag 已定义、显式报未实现）。入口：CLI `binflow-server export -c <cfg> --output <dir>`；**无 REST 面**（`/api/export/**` 404——GE-09；ADR-0015 决策 5「admin REST 异步触发」M4 不做）。
> - import 条目同步修订：「非空 409」→「非空 → CLI 退出码非 0（import 无 REST 面）」；补「先验证后写盘（manifest 结构 + metadata.sha256 实测 + size 全验 + sha256 spot 前 100 / `--verify full` 全量 + schema 版本天花板）」与「任何失败清回空」；「启动 GC dry-run 报差异」降格为运维建议（文档面），不在 CLI 内强制执行。
>
> 联动：ADR-0015 决策 3 的产物表述（`binflow.db` + `blobs.tar`）与决策 5 的「admin REST 可选触发」按上述同口径加一条勘误行（产物形态 = `metadata.db` + `blobs/` 目录 + `manifest.json`；REST export M4 不做；决策 4「幂等」释义 = 清空目标后重复导入等价）；§11.19 标题改「备份不含 web_sessions（remote_cache 随 db 走）」；§3.1/§3.2 补一行新公共面（`storage.AcquireDataLock/DataLock/ErrDataLockHeld`、manifest 族、metadata 快照检查函数）。

## 四、技术债台账建议新增（随勘误票）

1. **引用集双实现**（N5）：cmd `liveChecksumSet`（Store 面）与 `metadata.SnapshotChecksums`（SQL 直查）同形不同路；引用事实扩张须两处同步；建议等价性钉子测试。
2. **Windows 锁运行时未验**（承接日志遗留②/N3）：GOOS=windows 编译/vet 过、无真机；M5 部署矩阵 QA 补 W25b 双向断言；Windows 下争用方持有者诊断降级 "unknown" 为已知行为。
3. **export 对空 data dir 制造空备份**（N4，若不做 fail-fast 则记债）：错配 `-c` 时产出自洽的空 manifest。

---

## 五、结论

**APPROVE**。锁原语契约（R9）按申报定案：T-94 派单注记应写明「**消费 `storage.AcquireDataLock(dataDir, "gc")`，勿重写原语**；409 = `errors.Is(err, ErrDataLockHeld)`，文案素材在错误串/锁文件首行（`pid=<n> op=<op>`）」并附 N1/N2 的加固选项。分层、ADR-0015 机制内核、export.run 审计归属均通过。§7.6/ADR-0015 形态勘误建议采纳（草案见第三节）。
