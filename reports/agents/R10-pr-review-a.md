# R10 双审报告 A（correctness）— 载荷四提交 d9bae6b4..fc6fc0f5

- 形态：reviewer-a（correctness：并发/失败处理/正确性）；评审对象 `3254c887` (BIN-60/T-578) / `b248be9d` (BIN-61/T-579) / `4fa01dda` (ADR-0052) / `fc6fc0f5` (L039/T-580, docs only)
- 取证自跑：build/vet/gofmt 全绿；三 adapter 包 `go test -count=1` 3×ok；`-race -count=2` 新测试族全 PASS（首跑 internal/repo 一次未捕获 FAIL，复跑+单包连跑 5 次全 ok——非稳定复现，归入注记）
- 以下为评审全文（conductor 代落盘，内联原文）

---

评审完成。所有取证性测试套件均已通过；我已经核对了每一处派发的正确性项，并对照了代码。

## 评审报告 — R10 载荷四提交（d9bae6b4..fc6fc0f5），形态: reviewer-a (correctness)

**结论: APPROVE** — 0 blocking / 6 NB / 3 范围外

工作目录 `/Users/lzw/dev-center/.claude/worktrees/clever-grothendieck-a3fa4f`（下述路径省略此前缀）。

### 逐项结论（派发 8 项）

1. **SET-先行-后渲染次序 — 通过**。generic `internal/adapter/generic/handler.go`（putClientChecksum）：读探→`io.ReadAll`→`seam.SetClientChecksums`→比对→201/409，SET 失败走 `writeServiceError`（default=500、ErrForbidden=403、ErrUnauthorized=401+WWW-Authenticate，handler.go:630-666），无假 201/409。maven `internal/adapter/maven/put.go:443-494`：measured/refuse 先算，SET 在 refuse 渲染与 201 之前，409 臂不跳过 SET；seam==nil 时如实 500。与 ADR-0052 决策 3③ 逐字一致。
2. **单语句条件列更新 — 通过**。`internal/metadata/substores.go:288-308`：单条 `UPDATE nodes SET client_x = CASE WHEN ?<>'' THEN ? ELSE client_x END … WHERE repo_key=? AND path=? AND sha256 <> FolderMarkerSHA`，无 read-modify-write 窗；folder 行在 SQL 边排除（与既有 Stats/CountDownload 同款 FolderMarkerSHA 约定，store.go:261）；0 行→wrap `ErrNodeNotFound`；只碰三列（不触 updated_at/usage）。并发 SET = 行级串行 per-algo last-writer-wins。service 层 `internal/repo/service.go:1266-1294` 顺序：validateNodePath→requireAuthenticated→Get→isFolder→w-gate→空 ref no-op→store。
3. **权限门 — 通过**。w 门在服务缝内（`s.allow(ActionWrite)`；deny→ErrForbidden、匿名→ErrUnauthorized），principal 从 ServeHTTP 的 `adapter.PrincipalFrom(ctx)` 直传，无丢失环节。`TestSetClientChecksumsGates` 钉匿名/非写者拒绝且拒绝不落列。adapter 只读探查路径上 PUT 终缀腿最终都过缝内 w 门（读探失败的 401/403 形态 `return false` 落原链，由 svc.Put 再拒，一致）。
4. **LOCAL 门保持 — 通过**。generic PUT/GET 两面均 `class.Get→Type == TypeLocal` 门（handler.go:139、:476-485）；`internal/adapter/generic/remote_render_test.go:103-119` 三终缀 405+Allow+上游 hits 冻结负测未动、随套件全绿（复跑 PASS）。maven 侧 SET 追加 `origLocal` 门（不削弱，只收窄）；GET overlay 门 `rowType==Local && KindArtifact`。全模型翻正未触碰 R9 修复臂。
5. **GET/HEAD 回显面 — 通过**。`serveClientChecksum`：存值 200 CT=`application/x-checksum`+CL=len(value)，HEAD 同头无体；未存/源缺同走族内 404 `File not found.; Path: '<repo>:<src>'` 指剥终缀源；`defer rc.Close()`。maven `serveSidecarOfPath` overlay 仅直连面（walk 腿传 false，t5/t6 计算值契约不回归）；`writeSidecarBody` 共用渲染（ETag=body 值、条件 304、HEAD 无体），未存落回 `writeSidecarDigest` 计算值（L014-2 姿态）。
6. **nuget created.go — 通过**。envelope：字段序/Jackson 形/uri==Location 逐字节由 `TestBarePutCreatedEnvelope` 钉；originalChecksumsLines 已接 `repo.OriginalChecksums` + 本面成员过滤（sha256 恒在、sha1/md5 仅声明时），201 路径 node.Client* == 已验声明集（putNode 两腿均写 Client 列，service.go:1194、:1220——注释断言属实）。409：只消费声明头+tee 实测，storage 错误链零引用，负测四断言（session/commit upload/storage:/Checksum error for）钉零泄露；mismatch 在 storage Commit 后才可能触发，tee 此时必完整（实测三联正确）。`svc.Put` 成功恒返回非 nil node（putNode 契约），itemCreatedBody 的 node 解引用无可达 panic。X-Checksum-Sha256 条件 `node.Sha256 != ""` 维持 T-575。
7. **失败处理 — 通过**。404 臂 drain（`io.Copy(io.Discard)`）；201/409/SET 臂 body 已 ReadAll；maven 1024B 守卫维持；ctx 全程显式传递；store 错误 wrap 带上下文（`nodes set-client-checksums %s/%s: %w`）；`jsonStr` 全函数化无 panic 面；`measured==""`（账本缺口）按既有降级姿态放行不误 409（与 maven digestOf ok=false 同构）。
8. **测试覆盖 — 通过**。写穿回显（generic 列+GET 错值、maven 同、httpapi oc 三态）、覆写（re-PUT 对值）、双文案（409 received/actual + GET-unset 逐字）、del3 收敛（t574i 节点数=2 钉无 sidecar 节点）、nuget 409 无内部泄露负测、per-algo 独立与兄弟列保持——全部钉住。漏测面见 NB-4/NB-5。

### 取证命令（均实际执行）

- `go build ./...` OK；`go vet`（六包）OK；`gofmt -l`（六目录）空
- `go test -count=1 ./internal/adapter/generic/... ./internal/adapter/maven/... ./internal/adapter/nuget/...` → 3×ok（15.3s/39.3s/31.8s）
- `go test -count=1 ./internal/repo/... ./internal/httpapi/... ./internal/metadata/...` → 首跑 **internal/repo FAIL（测试名未捕获）**；同命令复跑+单包连跑 5 次全 ok（含 `-count=2 -race` 新测试族全 PASS）——归入注记，非稳定复现
- `go test -race -count=2 ./internal/repo/ -run 'TestSetClientChecksums|TestOriginalChecksums'` 全 PASS

活体双轮差分（T-578 28 腿、T-579 七腿）凭据面无法复跑，报告 raw 锚在案、四门命令本地复跑结果与 T-578/T-579 Claims 一致，证据采信。

### 必须修改（blocking）

无。

### 建议改进（non-blocking，转候选池）

1. `internal/adapter/generic/handler.go:243`（putClientChecksum `io.ReadAll(r.Body)`）— 注册体无大小上限（maven 同族有 1024B 守卫）。持写凭据者可无界耗内存。报告已如实登记（T-578 Performance/Risks⑤）；建议 probe A 面超限行为后补守卫，勿猜限值。
2. `internal/adapter/nuget/created.go:167`（writeBareCreated）— `node != nil` 只护了头，`itemCreatedBody` 无条件解引用 node。当前 svc.Put 契约下不可达，但防御不对称：要么都护、要么删掉头部的 nil 判断（一处守卫统一）。
3. `internal/adapter/nuget/created.go` + 三处 mime.go — mime 表第四副本落地，与同一批次的 generic/maven 注释「do not grow a fourth copy」（generic/mime.go:24-26）自相矛盾。表体已核对一致（diff 仅注释/命名）；建议下票 hoist（B 形态收编项，T-579 Risks 已登记）。
4. `internal/repo/client_checksum_test.go:124` — `strings.Contains(err.Error(), "not found")` 弱断言，应 `errors.Is(err, repo.ErrNodeNotFound)`（同文件 Gates 两处同病：只断言 err 非 nil，未断言 sentinel）。
5. generic/maven PUT 面 SET 失败→writeServiceError 路径无 adapter 级测试（denied writer × 源在场 checksum PUT）；缝级已钉，adapter 级映射臂未钉，低风险。
6. `writeServiceError` default 臂 `err.Error()` 直出（两 adapter 既有形态）——store 层错误文本（表名/路径）可达 wire。既有横切面，非本票引入，建议横切票统一收敛。

### 范围外发现（交 conductor，不塞本票）

1. **maven virtual/remote 面 sidecar PUT 无 405**：`put.go` putSidecar 对非 local factsKey 不走 svc.Put，`svc.Get` 读探后直接渲染 404/409/201（virtual 未路由时可能渲染「注册成功 201」而实际零落地）。L014-2/T-574 起既有，T-578 未加剧（SET 已按 origLocal 收窄），remote 面读探仍是写动词触回源的同族窗（generic 已在 R9 修，maven GAV 臂未修）。L039 Arm 1/7 只探了 generic 面；建议 probe 票补 maven virtual/remote sidecar PUT 腿。
2. **GET 未设值双文案**（L039 P8）：A 源在场未设=「Checksum not found for <src>」，B 本实现两态同用 `File not found.; Path:`。已由 L039 登记修订既有条目，修复票随台账走。
3. **srvgen 策略写面**（L039 Arm 6）：A 在 server-generated 下仍把 client 宣称值记入 oc，B 的 SET 门限 CLIENT 不注册——已登记 oc family 候选，随 BIN-60 后继票。

### 对账

- ADR-0052 六点与实现逐条对上（签名/单语句/次序/w 门在缝内/仓型判定留 adapter/渲染单源+边界条款）；upload-context ItemCreated（generic iteminfo.go:80、maven put.go:747）按边界条款保留 declared-only，非违例——单源声明成立。
- L039（fc6fc0f5）：B 面用 origin/develop 干净树隔离在途改动，方法论正确；白名单 9/9 零回归、R9 修复面复绿、C1-C7 候选分类待裁，纯文档无代码面，通过。
- 报告 vs 代码：T-579 Notes 称 rewire「待收编」，b248be9d 实际已含 rewire（commit message 如实）——报告为收编前快照，无证据失实。

**状态: done**
