# 评审报告 T-38（视角: correctness）

结论: **REQUEST_CHANGES**

- 评审人: code-reviewer（correctness）
- 日期: 2026-08-18
- 范围: `internal/adapter/docker/{uploads.go,blob.go,range.go,digest.go,api.go,handler.go,errors.go}` + `internal/httpapi/docker_blob_test.go` + 装配点
- 取证命令（均实际执行）:
  - `go vet ./internal/adapter/docker/` 零输出；`gofmt -l` 空
  - `go test -race -count=1 ./internal/adapter/docker/` ok（4.5s）
  - `go test -race -count=1 ./internal/httpapi/ -run 'TestV2Blob…'` ok
  - **临时探针**（`zz_review_probe_test.go`，跑完已删，工作树恢复 clean）: 并发 PATCH 同会话 / PUT-PATCH 竞态 / 毒化标志竞态 / mount 源形态矩阵 / 空 PATCH / chunked PUT / HEAD+Range —— 其中**并发同 UUID 探针触发 race detector 实证告警**（见 B1），PUT-PATCH 竞态 20 轮、mount 降级、chunked finalize(CL=-1)、HEAD 206 头形均符合预期。

## 必须修改（blocking）

### B1. `liveUpload` 字段无锁并发读写 —— 实证 data race
- **位置**: `internal/adapter/docker/uploads.go:232 / 253 / 265 / 270 / 193`
- **问题**: `sessionRegistry.mu` 只护 map 本身，不护条目。同一 upload UUID 的并发请求下，`up.received` 与 `up.poisoned` 被无同步读写。探针（6 个并发 PATCH 同会话）触发 race detector 告警：`uploads.go:265`（写 `up.received = received`）对 `uploads.go:232`（`alignContentRange` 读 received）与 `:270`（`rangeHeader` 读）竞争；GET 偏移查询（`:193`）同样裸读。语义后果不止告警：两个并发 PATCH 都按**过期**的 received 通过锚点校验后各自 Append，`up.received` 赋值（非累加）在乱序完成时可**回退**，响应的 `Range` 权威值可错。并发同 UUID 不是纯理论——两个 CI 节点共用凭据重试同一 push、或恶意客户端，都直达此处；票据评审重点第 1 条（"并发同 UUID 操作的串行化"）当前不成立。
- **建议**: 给 `liveUpload` 加 `mu sync.Mutex`，在 `patchUpload` / `finalizeUpload` / GET 偏移 / DELETE 入口处对**整个会话操作**加锁（串行化同会话的所有动词）；`alignContentRange` 判定与 `Append`+`received` 赋值必须在同一临界区内。补一条并发同 UUID 的表驱动测试（对齐错乱断言 416、顺序收敛断言 Range 单调）。

### B2. 会话注册表 + 存储会话 fd 无限泄漏（无任何时限/驱逐）
- **位置**: `internal/adapter/docker/uploads.go:30-56`（`liveUpload`/`sessionRegistry`）；`internal/storage/engine.go:265-292`
- **问题**: 条目只经 finalize / DELETE / 毒化终结 / 畸形 digest 离场。客户端 POST 会话后消失（**中断的 push 是常态**）→ 条目永驻，且每个条目持有一个 storage 会话 = 一个**打开的 data fd + 磁盘目录**。storage 的 TTL sweep（`sweepSessions`）只在 `OpenEngine`（启动）跑一次，且**显式跳过 engine 内活会话**（`isLiveSession`），运行期永不清它们。`liveUpload.started` 字段已埋但**无任何读取者**——超时驱逐显然计划了没做完。认证后的 pusher 可以无上限地耗 fd/内存（POST N 次 = N 个 fd）。
- **建议**: 用 `started`/last-activity 实现 idle TTL 驱逐（goroutine 定期扫注册表：过期 → `sess.Abort(context.Background())` + `remove`；storage 会话 Abort 幂等且清理目录），或把注册表上限 + LRU 驱逐作为硬底。TTL 建议对齐 storage `DefaultSessionTTL`（24h）或更短（docker 客户端 push 中断后不会回用旧会话）。补一条"过期会话被驱逐且 sessions/ 目录清空"的测试。

### B3. `blobPresent` 漏 Close —— 真实栈上每次成功 mount 泄漏一个 blob fd
- **位置**: `internal/adapter/docker/blob.go:319-322`
- **问题**: `_, _, err := h.svc.Get(ctx, p, repoKey, path)` —— 真实 `repo.Service.Get` 成功时返回 `st.Open` 的 `io.ReadSeekCloser`（`*os.File`），三个返回值全部被丢弃，**永不 Close**。每一次成功的 cross-repo mount 泄一个 fd。单测的 fake 返回内存 `bytes.Reader` 掩盖了此问题（fake 无 fd），`TestV2BlobCrossRepoMount`（真栈）也不断言 fd。长期运行的 registry 上高频 mount 会耗尽 fd。
- **建议**: `rc, _, err := h.svc.Get(...); if err == nil { _ = rc.Close() }; return err == nil`（Close 失败不影响在场判定）。可顺手在真栈集成测试里断言 `runtime.NumGoroutine`/fd 计数不随 mount 增长（可选）。

### B4. Commit 成功但挂账（`svc.Put`）失败仍回 201 —— "成功"的 push 会静默蒸发
- **位置**: `internal/adapter/docker/uploads.go:455-461`（`writeBlobCreated`）
- **问题**: 裁定③把挂账失败降为 ERROR 日志 + 仍 201。后果链：客户端收到 201 → manifest 引用该 blob → `GET blob` 因无 node 而 404 / T-39 校验链 400 `MANIFEST_BLOB_UNKNOWN` → 24h GC grace 后物理 blob 被回收。客户端视角：一次被确认成功的 push **静默丢失**，且无任何可重试信号。日志条目 6 自己也把这个窗口留给 review 裁定——correctness 视角裁定为缺陷：进程内可观察的 Put 失败不是架构允许的"crash 窗口"（那是 GC 的领地），是应当如实渲染的失败。重试是安全的：`Commit` 幂等去重 + `Put` 的幂等重传规则已覆盖（重推同内容 → singleflight 命中 → putNode 落账）。
- **建议**: `writeBlobCreated` 里 `registerBlobNode` 失败（非 denied）时回 500 `UNKNOWN`（body 指示 retry-safe），blob 留作 GC 候选；denied 分支维持现状（403）。改动点即日志所述单处。

## 建议改进（non-blocking）

1. **毒化 Append 的早退分支不写任何响应**（uploads.go:254-261）: `r.Context().Err() != nil` 时裸 return，Go 栈会对仍连着的客户端隐式写 **200 空体**（如 shutdown 取消 ctx 而连接未断）。建议显式回 416+权威 offset 或 499/500，勿依赖隐式状态码。
2. **`serveUploadStart` 注释与实现不符**（uploads.go:143-145）: 注释称 initiating POST 的非空 body "drained and ignored"，实际未 drain——handler 直接返回，未读 body 会导致该连接被服务端关闭。行为无害但注释说谎；要么 `io.Copy(io.Discard, …)` 限量 drain，要么改注释。
3. **会话不绑定创建时的 `<repoKey>/<image>`**: PATCH/PUT/GET/DELETE 用任意 name 前缀 + 同 UUID 都能操作同一会话（route gate 按"当前 URL"的 name 鉴权，非会话归属）。当前无越权（目标 image 需有 push 才能终结），但 Location 回显会用错 name。建议会话记住 ref，不匹配即 404 `BLOB_UPLOAD_UNKNOWN`（也挡掉 B1 的跨 name 竞态面）。
4. **PUT 终结不校验 Content-Range**: 带 body 的 PUT 若附了错位 Content-Range 直接忽略。官方语义 PUT 不要求该头，容忍可接受；记录备查，T-43 conformance 若暴露问题再收紧。
5. **`registerBlobNode` 双流大 blob**: 每次终结把已落盘 blob 完整重读并重算三摘要（svc.Put 内部 session）。10GB 层的 push 时间近似翻倍。正确性无虞（幂等），性能项留给后续票（元数据直达 API 或 Stat 复用）。
6. **`/v2/<name>/blobs/uploadsX`（无斜杠黏连）落进 `blobs/` 分支 → 400 `DIGEST_INVALID`**，路由形态错应为 404。纯观感。
7. **`TestSingleflightExecutesOnce` flake**（storage concurrency_test.go）: 维持日志裁定转 dev-go-core，非本票 area。
8. **PRD §8 D06/D13 示例 URL 单段 name 勘误**（日志遗留 4）: 同意转 conductor/PM，T-43 复跑前修正。

## 走查确认（无问题项）

- **416 后会话存活可恢复**: 实现与测试一致（`TestContentRangeMismatchMatrix` 5 行 + 恢复锚点断言）；416 空 body + 权威 `Range: 0-<received-1>` 形状正确。
- **PUT 带末段 body 计入流**: `ContentLength != 0`（含 -1 chunked，探针实证 201）先 Append 再 Commit；`Append` 返回值被丢弃但会话随即移除、无观察面，正确。
- **D12 失配 400 `DIGEST_INVALID` + 零残留**: finalize / 单请求双式均 Abort 或依赖 Commit 的 failLocked 清目录；测试断言无幽灵 blob。
- **毒化映射**: 毒化 PATCH → 416+权威 offset；毒化终结 → 400 `BLOB_UPLOAD_INVALID` + 消费；`ErrSessionPoisoned` 链路（storage 契约）对接正确（除上述第 1 条的渲染细节）。
- **mount 权限时序**: 源 read（NFR-S12，`canMountFrom`）→ 源在场探测 → `PutFromBlob`（内部再查 destination 权限对）→ 降级 202 永不报错（探针 4 形态矩阵全过：单段 from / 空 image / 嵌套 image / 不存在 repo）。台账 vs 物理：PutFromBlob 双查（storage.Open + blobs ledger），孤儿物理 blob 拒绝——正确不信任客户端。
- **空层合成**: 常量自洽（测试逐位钉 sha256/sha1/md5）；GET/HEAD 头族齐全；在 repo 门与 authorizeRoute 之后（不越权）；不落盘 ⇒ stats/mount 面无影响、T-39 豁免常量已在包内就位。
- **Range 复述**: 与 generic/conditional.go 逐条对齐（206/416 形态、多区间与非 bytes 单位忽略走 200、clamp、倒置、空后缀）；唯一差异是 `parseUint64` 的溢出上界（1<<62 附近 vs ParseInt），语义等价。
- **重启 404**: 注册表为进程态，空表即 404；磁盘残渣走 storage 启动 sweep（TTL 内保留是 M1 既有行为）；`TestV2BlobSessionDiesWithProcess` 覆盖。
- **交接面**: `blobNodePath` / `emptyLayerDigestHex` 包内私有、无导出污染；handler.go 分派缝对 T-39/T-40 干净（blobs* 之外落基座 404）。
- **clean-room 抽查**: 对照 `reverse-src/.../DockerV2LocalRepoHandler.java` / `DockerBlobUploadHandler.java`——BinFlow 为有状态 sessionRegistry + storage Session 架构，与 Artifactory 的 `_uploads/<uuid>.patch` 仓库内文件 + SequenceInputStream 拼接完全异构；且多处**有意背离** Artifactory（纯 UUID、补 Range 头、DIGEST_INVALID、标准 GET 端点）并以官方 spec 为准。无逐行对应嫌疑。

## 结论

状态机主链（POST→PATCH→PUT 的 received 对齐、416 恢复、digest 失配清场、毒化对接、mount 降级、空层、Range）质量高、测试扎实；但 **B1（实证 data race）+ B2（fd/注册表无限泄漏）+ B3（mount fd 泄漏）+ B4（假 201）** 四项均为正确性/资源缺陷，须修复后重审。修复面全部收敛在 `uploads.go`/`blob.go` 单包内，不触架构裁定。
