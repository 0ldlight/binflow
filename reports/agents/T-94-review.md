# T-94 评审报告（视角: correctness + concurrency）

- ticket: T-94 [P0] GC 管理化 + data 目录互斥锁（FR-30，GE-03）
- 评审对象: commit `86f879b` 的 T-94 部分（`internal/httpapi/system_gc.go`、`t94_gc_test.go`、`router.go` gc 分支、`server.go` Deps.GC、`harness_test.go`、`internal/storage/datalock.go` 增补 + `datalock_ops_test.go`、`cmd/binflow-server/main.go` 接线）。同 commit 的 auth/metadata/groups 改动属 T-97，不在本轮。
- 日期: 2026-08-21
- 结论: **APPROVE**（0 blocking / 6 non-blocking；另有 2 条范围外发现需 conductor 处置，见文末）

## 取证（实际执行）

- `go build ./...` 通过；`go vet ./internal/httpapi/... ./internal/storage/... ./cmd/...` 零输出
- `go test -count=1 -run 'TestT94|TestDataLockHolderHelpers' ./internal/httpapi/ ./internal/storage/` → ok（4.1s / 0.3s）
- `go test -race -count=1 -run 'TestT94' ./internal/httpapi/` → ok（10.5s，无数据竞争）
- 通读上下游：`storage/gc.go`（kernel 未改）、`storage/datalock.go`、`cmd main.go runGC`（对照面）、`repo/service.go PutManifest`（manifest 落 node 的证明）、`metadata/substores_docker.go ListImages`（limit 0 = 无限）、`config/validate.go`（GCGrace 必须 > 0）、`httpapi/server.go Shutdown`（排水不取消请求 ctx）
- clean-room 抽查：新文件为原生 Go、按本项目 ADR/PRD 措辞实现，与 `reverse-src/`（Java 反编译物）无逐行对应嫌疑。通过。

## 逐项核对（按派发重点）

1. **dry-run/apply 语义** ✅ `apply` 缺省 false；空 body/EOF 容忍为 dry-run；非法 JSON/类型错/graceHours 越界（<0 或 >876000）→ 400 E-01（测试矩阵含 876001 溢出边界）。数值核验：876000h ≈ 3.15e18 ns < int64 上限 9.22e18，400 边界确在溢出之前，防「storage.GC 静默改义」的动机成立。两步确认形态（先 dry-run 后显式 apply:true）与 ADR-0015 勘误①一致。
2. **graceHours 三态** ✅ 缺省 → `config.Storage.GCGrace`（config 校验强制 > 0，故缺省路径不存在被 kernel 改义为 24h 的可能）；显式 0 → `time.Nanosecond`（§3.1「无宽限须亚秒时长」的教科书执行）；CLI `--grace-hours 0` = 用 config（flag 哨兵）。`TestT94GCGraceDefault` 三面钉死（config 调 1ns 后缺省可见孤儿 / 默认实例新鲜孤儿不可见 / 空 body 默认 dry-run）。
3. **mark 集正确性** ✅ `liveChecksumSet` = nodes（`ListByPrefix` 全前缀）∪ docker_refs（ListImages `limit=0 → limitOrDefault → -1` 无截断，逐 manifest 展开 refs）。manifest 本体覆盖疑点已排除：`repo.PutManifest` 在 `<image>/manifests/<hex>` 落 node 行（service.go:1051 起），manifest 摘要由 nodes 半边保护——与 §6 DDL 注释「docker_manifests 行随 nodes 级联语义」一致。docker 半边有专测。两遍扫描语义（candidateCount=预扫 / deletedCount=实删）与安静实例相等不变式有 W25 断言（含 stats 降量 == deletedCount、blobs ledger 行数对账、keeper 幸存、再 dry-run 归零）。
4. **409 映射** ✅ 不 parse 错误串，经 `LockHolder` + `HolderOp` 编程面；op 词表为 storage 常量（gc/export/import），cmd 两处字面量已换常量；import op 落 generic「another maintenance operation」分支（REST gc ↔ import 的 PRD 无字面文案要求，err 原句仍带 `op=import`，可接受）。Windows/不可读记录降级有 truncate 空记录专测。body 同时含 op 词 + `ErrDataLockHeld` 原句 + holder pid/op（err.Error() 自带）。
5. **锁消费契约** ✅ 消费 `storage.AcquireDataLock`，机制零重写；`defer func(){ _ = lock.Release() }()` panic 安全；fail-fast 不排队。同进程跨 fd 互斥由测试实证（harness server 与测试持有的锁在同一进程内争用 → 409，unix flock 按 open-file-description 排他的正确运用）；跨进程双向真机证据见工作日志 W25b。
6. **审计** ✅ 消费 T-93 `audit.ActionGCRun` 无重复定义；dry-run 也落审计且 detail 以 `apply:false` 区分——治理面上「看过报告」与「执行过删除」都该留痕，语义正确；`graceHours` null=缺省 / 0=显式的区分有断言；RemoteAddr 由 audit.Append 合入 Detail（非 handler 责任，链路成立）。
7. **Deps.GC 装配** ✅ 消费侧接口恰为 `storage.Engine.GC` 方法签名（无新 storage 面）；nil → 503（`TestT94GCGateWithoutEngine` 走真正未接 engine 的 `rebuildWithDataDir`，证明力足）；403 先于 503 的优先级正确。
8. **无死锁面**：锁序单一（data lock → SQLite 写），serve 写路径不取 data lock，无反转可能。锁内做审计与 ledger 清理，释放于响应写完后。

## 必须修改（blocking）

无。

## 建议改进（non-blocking）

- **N1 apply 腿绑定 r.Context()，连接断开即半途而废**（system_gc.go:172）。客户端断连/代理超时/停机排水超时会在 sweep 中途取消 ctx：已删 blob 留永久幻影 blobs ledger 行（sweep 只看磁盘，不会再访「有行无文件」，stats 计数漂移无自愈路径），且部分生效的删除性运行不落 gc.run。失败路径下 handler 也无从补救——storage.GC 出错时返回的是 Candidates（已见）而非 Deleted（已删），ledger 清理环拿不到真删除清单。建议：apply 腿改用 `context.WithoutCancel(r.Context())`（Go 1.21+，一行，kernel 零改动，符合「storage.GC 复用零改动」约束），使单条 HTTP 连接的寿命不约束 sweep；失败审计可另行加戏。与 CLI Ctrl-C 同病、AC 未规定失败路径行为，故 non-blocking——但 Q6 同步执行下长请求是常态，建议随最近的 GC 相关票顺手收口。
- **N2 mark 集实现双份漂移风险**（system_gc.go:306 vs cmd/binflow-server/main.go:639）。同名 `liveChecksumSet` 同形复制，靠双侧注释互指维持同步；任一侧改动（如未来 docker 表结构演进）会让 REST 与 CLI 的安全语义悄然分叉。建议后续票上提到单一消费点（或 metadata 层 DISTINCT 助手，需 architect 意见）。今日双侧均有 docker 半边测试，风险受控。
- **N3 409「最后持有者」语义**（system_gc.go:258）：外来持有者（不写记录的 python flock）时 op 词沿用上一记录。工作日志遗留 3 已如实登记；如需打磨可在 pid 不活（`kill(pid,0)` 探测）时降级 generic 文案，P2。
- **N4 body 解码容忍尾部垃圾**（system_gc.go:101）：`{"apply":true} garbage` 只解第一个 JSON 值即放行。与 `repositories.go` 同款 laxness，全库一致，改严格需全局面统一，不单独动本端点。
- **N5 并发双 POST 未有端到端测试**：同进程互斥已由「测试持锁 → handler 409」实证（同一 flock 机制），但两个真正同时在飞的 REST gc 请求（其一 409）没有直接用例。价值边际，登记备查。
- **N6 graceHours:0 的运维语义应进用户文档**：显式 0 = 摘掉 mtime 宽限 = 失去对在途上传的保护窗。blob-first + `nodes.sha256 REFERENCES blobs` FK（001_init.sql:46）保证竞态后果是「上传失败可重试」而非悬空引用，无损数据，但 busy 实例上 `apply:true + graceHours:0` 会偶发 5xx 上传失败。建议 tech-writer 在 user/API 文档写明（W24 剧本是静默实例配方）。

## 范围外发现（交 conductor）

1. **FR-30-AC5 的 CLI 腿未闭合**：AC① 要求 `gc.run`「REST 与 CLI 两路径都落」，`cmd` 的 runGC 至今无审计写入（T-94 工作日志遗留 1 已申报，`cliAuditActor` 常量与 BestEffort 模式现成）。属 cmd area（T-96 已 closed）。**不阻塞本票评审**（REST 半边完整且经测试），但 FR-30-AC5 在补上 CLI 腿之前不能宣告满足——请按日志建议并入 T-96 修复波或开补丁票。
2. **cmd 一行接线（`GC: stack.st`）+ 两处字面量换常量的 area 越界**：工作日志「接线」节已显式申报（T-91 Console 装配同款先例）。行为面看是纯装配无逻辑引入，建议主会话追认归档。

## 结论

正确性与并发两视角均未发现阻塞缺陷：dry-run/apply/grace 三态、mark 集完备性（含 docker_refs 与 manifest-node 双覆盖）、锁互斥的真实性（同进程 + 跨进程双向）、审计与降级装配全部成立且有高质量测试钉死；`-race`/`vet`/`build` 复验通过。同意合入，按上述 N1/N2 安排后续收口、范围外两项请 conductor 处置。
