# 评审报告 T-38（视角: consistency / 架构一致性）

- reviewer: code-reviewer (arch)
- 日期: 2026-08-18
- 输入: reports/agents/T-38.md、commit 2cd6d68（12 文件，+3026/-24）、architecture §5.1/§5.3/§6（T-30 定稿版）、T-32 票据 T-38 节 AC、docs/reverse/docker-registry.md §1/§2/§10、T-33-review-arch（先例：RepoTypes 裁定/分层核）、T-37（authorizeRoute）
- 结论: **REQUEST_CHANGES**（blocking 1，non-blocking 6，范围外 2）

取证命令与结果：

- `go test -count=1 ./internal/adapter/docker/` → ok 3.435s；`go test -race -count=1 ./internal/adapter/docker/` → ok 5.745s；`go test -count=1 ./internal/httpapi/` → ok 19.043s
- `go vet ./internal/adapter/docker/ ./internal/httpapi/` + `gofmt -l` → 零输出
- `go list -deps ./internal/adapter/docker` → internal 面 = {adapter, auth, metadata, repo, storage, audit(传递)}
- `grep -rn "SIZE_INVALID|NO_TAGS_FOUND|INTERNAL_ERROR" internal/adapter/docker/` → 零命中（非官方码未引入）

---

## 一、§5.3 三裁定逐条对照（conductor 点名项 1）

| 裁定 | 结论 | 证据 |
|---|---|---|
| 1. offset 协议态在 adapter | **守住** | uploads.go:30-40 `liveUpload{sess, received}` 由 adapter 持账；`alignContentRange`（uploads.go:538-555）对 adapter 自记账本判锚点，从不信任客户端声明；storage.Session 全程不接触 Content-Range（Append 契约只收裸流）。裁定原文「received 由 state.json 持久化」+ adapter 镜像持有的双账结构如实落地：storage 侧 `state.Received`（session.go:70）持续落盘，adapter 侧 `up.received` 走 Append 返回值赋值（uploads.go:265，assign-never-add 正确） |
| 1b. M2 不启用 ResumeSession | **守住，实现与裁定一致** | 全仓 grep：docker 包零 `ResumeSession` 调用（唯一出现处是 storage 自身实现与测试）。`sessionRegistry` 纯进程内（uploads.go:42-47），重启即空表 → 旧会话 404 `BLOB_UPLOAD_UNKNOWN`（serveUploadSession 三处 lookup miss 均走 writeBlobUploadUnknown）；D14 双测试（SessionDiesWithProcess 同 dataDir 二次装配 + SessionSweepResidue 磁盘残渣 sweep）恰是裁定「跨进程续传 M3+、磁盘残渣由 TTL sweep 收」的可执行对应 |
| 2. digest 剥 sha256: 前缀统一性 | **三处统一，守住** | mount 参数（blob.go:377 tryMount→parseDigestParam）、终结 query（uploads.go:281）、读路径 digest（blob.go:79）全部经 `parseDigestParam`（blob.go:213-226）归一为裸 hex；输出侧统一经 `digestPrefixHex`（blob.go:262）重加 `sha256:`。sha512 等他算法 → 400 DIGEST_INVALID（TestDigestParamShapes 第 4 行 + curl 探针），与「M2 仅 sha256，无算法转换」一致。无一处手写切前缀 |

**mount → PutFromBlob（裁定 3）**：tryMount（uploads.go:376-422）→ `h.svc.PutFromBlob`，源读经 `canMountFrom`→`Authorizer.Can`（NFR-S12），任一不满足降级 202 永不报错，目的地权限形拒绝如实 403——与 §5.3 表「mount 失败按 spec 降级为普通上传会话」逐语义吻合。sha1/md5 取 ledger 行不信任客户端声明（`ledgerRow`）与 §3.3 PutFromBlob 契约注释④同源。

## 二、分层红线与 WithStorage 缝裁决（conductor 点名项 2）

**裁决：blob 上传直用 storage 是 §5.3 已裁定的例外，代码不越线；但 §5.1 文本与 §5.3 实现现状互相矛盾，属文档缺口（须回写，见 N1 措辞），不构成本票 blocking。**

依据：

1. **§5.3 映射表（T-30 定稿）明文写了 adapter→storage 的直连**：`POST /v2/<name>/blobs/uploads/ → storage.BeginSession → 202`、`PATCH → 流式 Append（Range 校验由 adapter 做，storage 不感知协议头）`、`PUT → Session.Commit(expect)`。三裁定第 1 条进一步把「adapter 持协议状态、绕不过 Session 的 Append/Commit」定为对接法。T-38 的 `WithStorage(store, ledger)` 正是这条裁定的落点——**代码与控制性裁定（§5.3）一致**。
2. **§5.1 的字面禁令（「禁止直接 import storage/metadata，唯一例外：经 repo.Service 扩方法」）从未被 T-30 同步勘误**。于是 §5.1 说“禁止”、§5.3 表说“BeginSession”——新读者按 §5.1 会误判本票违线。这是 architect 的文档同步债，不是实现者缺陷（实现者无法写 architecture.md，且注释 api.go:69-74 已如实声明 bend 的出处）。
3. **metadata 触点（BlobLedger）有直接先例**：generic.BlobLedger（T-13 review B1 终判：READ-only digest 查询、类型经 repo.Service 签名同源，放行）。docker.BlobLedger 同构同用途（sha1/md5 头族 + mount 响应），消费端接口注入，一致。
4. **越出裁定面的第二触点——`registerBlobNode` 的 store.Open 回读**（uploads.go:465-474）：finalize 后把已落盘 blob 经 `store.Open` 回读成流喂 `svc.Put`。§5.3 映射表的 PUT 行只写「blob 落盘 + blobs 台账行 + 挂账」，未授权 adapter 自行开 blob 读。这是实现者在 area 约束（禁改 repo 包）下的合理裁定——repo.Service 没有「从已提交 BlobRef 建台账行」的用例（PutFromBlob 拒孤儿、Put 只吃 body）——但它是绕过 §5.1 例外条款（「扩方法，不得绕过」）的债务，须登记（见 N2 措辞）。

**§5.1 回写措辞（N1，供 architect 落 architecture.md §5.1 依赖方向段）**：

> 「依赖方向」修正为：`adapter/*` → `repo.Service` + `auth`；**例外一（§5.3 裁定，docker blob upload）**：上传端点族（POST/PATCH/PUT/GET/DELETE `/v2/<name>/blobs/uploads*`）直持 `storage.Engine` 驱动 Session 生命周期（BeginSession/Append/Commit/Abort），协议态（received、UUID 配对）在 adapter，storage 不感知协议头；该例外仅限上传会话对接，blob 读路径仍经 `repo.Service.Get`。**例外二（先例 T-13 B1）**：digest 台账的 READ-only 查询经 consumer-side `BlobLedger` 接口注入。其余「需要流式细节时经 repo.Service 扩方法，不得绕过」维持。

## 三、状态头形态对照（点名项 3）

对照 docker-registry.md §2.1/§2.2/§2.3 + §10 建议栏 5/6/7 与官方 [DIST-API]：

| 项 | 实现 | 裁定 |
|---|---|---|
| POST 202 Location | 相对路径 `/v2/<name>/blobs/uploads/<uuid>`（uploadURL, blob.go:274-276） | R6 ✓；绝对 Location（Artifactory 按 Host/X-Forwarded 构造绝对 URL）不采纳 ✓ |
| Docker-Upload-UUID | `sess.ID()` = storage uuid v4 裸形态（engine.go:134-142），无 `.patch` 后缀 | 规格建议栏 6 ✓（Artifactory 的 `<uuid>.patch` 不采纳） |
| POST 202 头族 | `Range: 0-0` + `Content-Length: 0` + api-version | 官方形 ✓（Artifactory 省略 Range 不采纳，TestUploadStartResponseShape 精确钉住） |
| PATCH 202 | Location 不变 + `Range: 0-<received-1>` 累计 | ✓（Range 单位前缀从简，同 distribution 参考实现的裸 `0-N` 形） |
| 416 错位 | 权威 `Range: 0-<received-1>` + 空 body | §2.2#3 高置信度行 ✓，且会话存活可正确锚点续传（TestContentRangeMismatchMatrix 5 行 × 恢复断言） |
| GET 状态 | 204 + Range + UUID（真官方端点） | 建议栏 5 ✓（`.patch` 变体不做） |
| finalize 201 Location | blob URL（digest 重加前缀） | ✓ |
| 空 body 语义 | 上传域 Content-Length: 0 全带 | ✓ |

**结论：头形态全项合格，无一项采纳 Artifactory 偏离。**

## 四、错误码表对照（点名项 4）

对照 docker-registry.md §1（官方码表 + Artifactory 非官方码清单）：

| 码 | 使用点 | 官方性 |
|---|---|---|
| BLOB_UNKNOWN | blob 读 miss（blob.go:190）、digest 失配后幽灵探测 | 官方 ✓ |
| BLOB_UPLOAD_INVALID | 仅毒化 finalize 拒绝（uploads.go:297） | 官方 ✓，用途正确（会话损坏）；**digest 失配不复刻 Artifactory 的此码文案** ✓ |
| BLOB_UPLOAD_UNKNOWN | 会话 miss/重启后旧会话（三处） | 官方 ✓ |
| DIGEST_INVALID | 失配 + 文法错（双式 + 读路径） | 官方 ✓（T-32 裁定落实） |
| UNAVAILABLE | ErrEngineClosed → 503 | 官方 ✓ |
| UNSUPPORTED/DENIED/UNAUTHORIZED/NAME_UNKNOWN/UNKNOWN | T-33/37 既有，未动 | 官方 ✓ |

grep 证实 SIZE_INVALID / NO_TAGS_FOUND / "NOT FOUND" / INTERNAL_ERROR 零引入。**码表干净。**

## 五、测试反转与净面（点名项 5）

- **T-33 空 sessionRegistry 桩删除**：diff 恰为桩类型 + 构造器 5 行删除、实现移至 uploads.go、handler.go 尾注释指路——净面最小 ✓，无行为漂移。
- **handler.go**：仅注释改写 + blobs/uploads 分派两分支（handler.go:211-218），manifests/tags/catalog 仍落 T-39/T-40 基座 404 ✓（不动 token/scope/authorizeRoute——授权门在分派之前，T-39/T-40 继承不需重推导，与 T-37 交接一致）。
- **api.go**：WithStorage + BlobLedger + 两字段 + 注释；`New` 签名不动（旧装配点零改动）✓。
- **httpapi 集成 16 顶层**（实际 grep 计数；日志写 15，见 N6）：D06/D07/D10/D10b/D12/D13/D13d/D14（双臂）/空层/Range/404/AC7 去重/auth 门/并发——**AC 的 D 序列全覆盖，无缺项**。
- **harness 增量**：`newHarnessCfg` 恰一行 `.WithStorage(st, md.Blobs())` ✓ 最小；`newHarnessWithDataDir` 是重启测试的必要自有栈（复用 harness 结构但不共享 engine 生命周期），不算 harness 膨胀。
- **race**：包内 `-race` 绿；日志对全量 -race 的 storage flake（TestSingleflightExecutesOnce，M1 遗留）披露诚实，归属判定（转 dev-go-core）正确。

## 六、交接点（点名项 6）

- `blobNodePath(image, hex)`（blob.go:266）与 `emptyLayerDigestHex`（blob.go:41）均为**包内私有**——T-39/T-40 同包（adapter/docker manifest/catalog 子域），私有即共享，**无过早导出** ✓。T-38.md 遗留 1 的「已导出于包内」措辞准确。
- `parseDigestParam`/`digestPrefixHex`/`isDenied` 同为 T-39 manifest 校验链的直接原料，形态够用。
- 路由分派点（tail 非 blobs* 落 404）给 T-40 catalog 留的接缝清晰；`catalogPath` 占位防 `_` 前缀被 repo key 劫持的注释在案。
- 空层豁免的 manifest 侧短路（引用链查询时同样合成）已显式移交 T-39 且常量就位——交接完整。

## 七、必须修改（blocking）

### B1. `blobPresent` 丢弃 `repo.Get` 打开的读取器——fd 泄漏 + 探测语义越出 §5.3 映射

- 位置：`internal/adapter/docker/blob.go:319-322`
- 问题：`func (h *Handler) blobPresent(...) bool { _, _, err := h.svc.Get(ctx, p, repoKey, path); return err == nil }` —— `repo.Service.Get` 的成功路径返回已打开的 `io.ReadSeekCloser`（service.go:169 `s.st.Open`），这里弃置不 Close。每次 cross-repo mount 探测泄漏一个 fd（os.File finalizer 回收不确定，高频 mount 下可触 fd 上限）。单测的 fakeService 返回内存 reader 掩盖了该泄漏，集成测试也不断言 fd 面——所以两套测试全绿。
- 架构面同类问题：§5.3 映射表把 mount 的前置条件写为「目标 blob 已被 from-repo 引用」——即**引用在场性**（node 行查询）；实现却走 `svc.Get` 打开整个 blob 流做探测，重量越出映射意图（O(size) IO 换一个布尔）。
- 建议改法（两行）：`rc, _, err := h.svc.Get(...); if err != nil { return false }; rc.Close(); return true`（Close 错误可忽略，只读 fd）。若顺带把探测降为服务层在场性查询（如 `List`/未来 presence 用例）更好，但 Close 是本票必须；正确性 reviewer 若已记同项，以其为准合并。

## 八、建议改进（non-blocking）

- **N1**（=第二节 4 的措辞落点）：§5.1 依赖方向段补「例外一/例外二」勘误（原文见上），消除 §5.1 与 §5.3 的互相矛盾。归 architect 小票，不阻塞本票。
- **N2** `registerBlobNode` 的 store.Open 回读（uploads.go:465-474）：登记为架构债——「从已提交 BlobRef 建 blobs 台账行 + node」的用例缺口迫使 adapter 绕过「扩方法不绕过」条款，且每次 finalize 多一轮 O(size) 回读+三摘要重算（singleflight 命中免二份磁盘副本，但 CPU/IO 不免）。建议措辞入 §11 技术债：M3 前为 repo.Service 增 `PutLandedBlob(ctx, p, repoKey, path, ref, mime)`（创建台账行的 PutFromBlob 变体），docker finalize 与未来的 maven/npm chunked 上传共用；届时删除 adapter 的回读。10GB 层推送场景该回读是可感知延迟。
- **N3** `writeBlobCreated`（uploads.go:456-459）：注册失败仅 `!isDenied(err)` 才记日志——isDenied 分支完全静默。该窗口 blob 已持久、node 未建、仍回 201，客户端后续 GET 该 blob 会 404（实现者日志遗留 6 已自曝）。201-vs-5xx 的取舍可维持（blob-first 不变量 + GC grace 兜底成立），但**拒绝形失败至少要同级记日志**（一行改动）；是否改 5xx 交正确性 reviewer 裁定。
- **N4** `serveEmptyLayer`（blob.go:167-183）声明 `Accept-Ranges: bytes` 但忽略 Range 恒回 200 全量——32B 固定件无实际影响，且 RFC 允许忽略 Range；与 serveBlobBody 的 206 契约不完全对称，记录备查即可（T-39 引用链短路处同规则复述时一并照做即可）。
- **N5** `liveUpload.started`（uploads.go:33）赋值后零读取——要么删，要么在遗留会话的内存回收（M3 周期清扫）用上；当前是死字段。
- **N6** T-38.md「15 顶层」实为 16（docker_blob_test.go grep 计数）；日志计数勘误，不改代码。

## 九、clean-room 抽查

- 上传状态机与 reverse-src 的 `DockerBlobUploadHandler.java`（会话 = 仓库内 `_uploads/<uuid>.patch` 文件、PATCH 追加 = SequenceInputStream 串接重上传）**无结构对应**：BinFlow 是进程内注册表 + storage.Session 追加，且头形态/错误码/端点四处**刻意选官方形而非 Artifactory 偏离**（纯 UUID、Range 0-0、官方 GET 端点、DIGEST_INVALID）——非照抄的方向性证据。
- 空层 32B artifact 是生态公共常量（digest 公开文档化），bytes 以 hex 直写 + 测试逐位断言 sha256 自洽（blob_test.go:763-766）——数据非代码，合规。
- Content-Range 错位 416 空 body 是规格（§2.2#3 高置信度行）驱动的行为实现，非逐行翻译。**逐行对应嫌疑：无。**

## 十、范围外发现（交 conductor）

- §5.1 勘误（N1 措辞）与 §11 债务登记（N2 措辞）归 architect；建议随 T-39 派单前落，避免 T-39 实现者按 §5.1 字面误判先例。
- `repo.Service` 缺「已提交 blob 挂账」用例（N2 根因）是 repo 包的面，本票 area 不可及——若 conductor 认可 N2，需单独小票（dev-go-core）而非塞进 T-39。

## 十一、结论

§5.3 三裁定逐条守住、头形态全项官方化、错误码表零污染、测试反转净面最小、交接无过早抽象、clean-room 无嫌疑——架构一致性主干干净。blocking 仅 B1 一处（mount 探测的读取器泄漏，两行修复 + 顺带补一条断言），修完即 Approve。WithStorage 缝的裁决与回写措辞见第二节/N1：**代码不越线（§5.3 已裁定的例外），文档须同步**。
