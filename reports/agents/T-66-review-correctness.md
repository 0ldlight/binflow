# T-66 评审报告（视角: correctness — 缓存状态机/并发/事务）

结论: **REQUEST_CHANGES**

- 评审对象：commit 05acd91（internal/remote 三文件 + internal/repo 分流 + 两处申报跨 area）
- 口径：PRD v1.2 FR-20-AC1~13 / FR-15-AC9、ADR-0012 + T-79 勘误一/二、repo-semantics §7.1~§7.6、architecture §4.5/§5.4
- 取证命令（均实际执行）：
  - `go vet ./internal/remote/... ./internal/repo/...` → 0 告警
  - `go test -race -count=1 ./internal/remote/ ./internal/repo/` → ok 38.5s / ok 41.8s
  - `go test -count=1 ./internal/adapter/generic/ ./internal/config/` → ok（跨 area 两缝回归）
  - `gofmt -l` 四目录 → 0 文件
  - 两个一次性并发探针（写入 `internal/remote/zz_review_probe_test.go` 运行后已删除，工作树无残留）：
    - PROBE1：16 并发请求同一**上游 404** 路径 → **上游接触 16 次（契约 1 次）**
    - PROBE2：metadata 类 winner 400ms + MetadataWait 80ms + 1 等待者 → **上游接触 2 次（契约 1 次）**，等待者未回退旧副本

## 必须修改（blocking）

### B1. singleflight 等待者无视获胜者写入的负缓存行 —— 并发 miss 打穿上游（实测 16→16）

- 位置：`internal/remote/fetcher.go:354-359`（Fetch 的重试循环只重跑 `attempt`＝步 4/5，**步 3 负缓存检查永远不重跑**）+ `fetcher.go:398-403`（`pass >= 1` 分支直接 `contactUpstream`，且**不持有 flight**）。
- 场景（探针实测）：16 个并发请求同一缺失路径。G1 获胜 → 上游 404 → 写负缓存 → 释放；15 个等待者醒后重入 `attempt(pass=1)`：无 node 行可 HIT、负缓存行不可见 → G2 又赢一个新 flight（上游第 2 次）→ 其余 14 个在 `pass>=1` 分支**无单飞保护地各自回源** → 上游共 16 次。
- 违反：
  - repo-semantics §7.3（高置信度）「同 path 全局单飞……获胜者完成后从缓存服务」；
  - FR-20-AC4 / M43 的并发形态——「负缓存期内 404 零上游流量」只在串行请求下成立；
  - 实现者日志「16 并发 miss：上游恰好 1 次」的声明只对 200 形态验证过（`TestFetchConcurrentMissSingleFlight` 上游是 200），负缓存/401 形态未被覆盖。
- 附带：401/403 分支不写负缓存也不开 offline，同样形态下等待者全部重放（凭据被拒的上游被并发打 N 次）。
- 建议改法：把步 3 的负缓存检查搬进重试循环（`attempt` 顶部，或 `waitFlight` 返回后、`pass>=1` 自行接触前）：fresh negative 行在场 → 直接按 unfound 返回，零上游。单点改动，测试补一条「16 并发 × 上游 404 → 上游恰 1 次」。

### B2. `pass>=1` 等待者跳过二次确认直接自行回源；metadata 60s 等待上限语义未实现（实测 2→2 次接触）

- 位置：`internal/remote/fetcher.go:392-405`；相关注释 `fetcher.go:126-129`、`934-936` 与实际行为不符。
- 两个可观察后果（同一根因）：
  1. **重复拉取**：等待者在第二个等待窗口内因 winner `done` 被唤醒后，不再复查「winner 是否已落盘」——即便新鲜副本已在缓存，也自行再回源一次（PROBE2 实测 2 次接触）。200-慢上游形态下，凡等待超时过一次的 metadata 请求都会产生一次冗余上游拉取。
  2. **metadataRetrievalTimeoutSecs 语义偏离**：§7.1/§7.3（高置信度）规定等待超时后「**回发旧缓存副本**」；实现是「再等一个 60s → 自行回源」，最坏 120s + 自身 15s 超时，且从不回退 stale。日志声明「超时回退 stale/unfound——等待者绝不 5xx」中前半句不成立（hardFail + 无副本时等待者会拿到 502，后半句也仅在 200 形态成立）。
- 建议改法：`waitFlight` 返回后统一 `return nil, true, nil` 重入 `attempt`（完整重查 HIT/负缓存/offline）；仅当重查仍 miss 且无在飞 flight 时才允许自行接触（此时也应先 `acquireFlight` 而非裸接触）；metadata 超时改走 stale/unfound 降级。循环上限用「自行接触至多一次」的显式标志而非 pass 计数，保证不活锁。

## 建议改进（non-blocking）

1. `fetcher.go:620-623`：非主动 304 的 REVALIDATED 分支用**空 ETag/Last-Modified** 重写验证器行——把已存验证器抹掉了。P1 条件再验证（If-None-Match）落地时无从发起。建议 `contentEntry` 保留旧 entry 的验证器（该分支本来就读了旧 entry 取 kind）。
2. `fetcher.go:471-481`：流式路径把 `land` 的**本地**失败（PutCache/Nodes.Put/Blobs.Put/Open）也交给 `mapTransportFault`——开 offline 窗 + stale 降级，本地磁盘故障被归因为上游并静默 300s；而缓冲路径同类失败直接 500。建议至少把 Commit 之后的元数据写失败与传输失败区分开（前者 500、不开窗）。
3. `fetcher.go:344/374`：步 3/步 4 吞掉 `GetCache` 的 store 错误（`err == nil &&` 短路）——DB 抖动静默变「无缓存」回源。建议记 WARN。
4. `fetcher.go:347`：负缓存命中的日志 `cache_result=STALE` 与 X-BinFlow-Cache 头语义混淆（负缓存命中并不回发该头，stats 计数走的是 negatives）。建议独立 token（如 `negative`）。
5. 尾斜杠文件夹形态路径（`d/`）会照常回源并把上游目录 listing 落成 node；httpapi `storageNode` 的「无斜杠→补斜杠」二次探测（storage.go:129-134）在 remote 上会**双触发上游**（`d` 与 `d/` 各一次，各写一条负缓存）。建议 `Fetch` 对尾斜杠直接 unfound（与 local Get 的 ErrIsFolder 语义对齐）。
6. （范围外，转 conductor）`internal/httpapi/storage.go:387` `writeStorageError` 无 `*repo.StatusError` 分支：REST `/api/storage` 上 remote 仓的 **400（SSRF 拒绝）/502（hardFail）FetchError 渲染为 500**（unfound 族因 wrap ErrNodeNotFound 正确 404）；REST 无 PUT 部署路由故 405 不受影响。与 generic 缝对称的 ~8 行分支即可修复；httpapi 属 T-71 在途 area，建议并入 T-71 或单独小票。

## 逐项核对结论（无 blocking 项的检查面）

- **落盘协议/事务**：`land` 的 Begin→Append→Commit(BlobRef{})→blobs 行→node 行→cache 行与 M1 `commitBlob`/`putNode` 顺序一致；Append/Commit 失败均 `Abort(context.Background())`（不依赖已取消 ctx）；node 落 remote 仓自身命名空间、无影子仓（§6.4-③/§4.5）；Commit 后失败只留孤儿 blob（GC 域）。通过。
- **hintedReader/ExtraHeaders 缝**：generic `handleGet` 在任何 WriteHeader 前注入（handler.go:276-283），GET/HEAD 共用路径均覆盖。通过。
- **凭据面**：AES-256-GCM 三态 LoadKey（未设=nil 合法 / 坏值=必炸）、`enc:v1:` 一次性迁移幂等（二次启动走解密校验臂）、nil-cipher 三态自洽（`Decrypt("")` 匿名容忍）；明文零日志/零回显（测试 + 真机 DB grep 双证）；redirect 跨 host 摘除 Authorization + Location userinfo 清除（T-65 面）未被本票破坏。通过。
- **分流接线**：Get 顺序 validate→loadRepoRow→**读门**→remote 分派（未授权主体零上游包，测试钉死）；local 路径逐位不变；Put 族 405+`Allow: GET`+wrap ErrRepoTypeNotSupported（T-64 旧断言兼容）；Delete 仅本地缓存、204/幂等 404、审计 `remoteCache:true`；DeleteRepo 级联 + `Forget`。通过。
- **FetchResult.HasCopy（R10/T-71）**：成功路径（HIT/MISS/STALE/REVALIDATED）恒 `HasCopy=true`，真 miss 一律 `*FetchError{Unfound:true}`，无「空成功」形态。充分。
- **故障矩阵其余分支**：hardFail 只改无副本码值（502）、5xx/传输开 offline 300s 且期内外行为正确（时钟注入）、401/403→404+摘要且不写负缓存不标 offline、SSRF 拒绝 400 不开窗、64MB 缓冲型 502、blocked_out 文案、sidecar 逐字文案——均与 PRD/勘误一致且有测试。
- **clean-room**：reverse-src 本地不存在；代码为行为规格驱动的原生 Go，无反编译标识符/结构搬运痕迹。未见嫌疑。

## 决策点裁决（5/5）

1. **负缓存与过期副本并存的次请求 404 —— 确认**。§7.2 步骤序字面（步 3 先于步 4）+ 测试钉板成立。已知副作用：负缓存每个 1800s 窗口到期时恰有一个请求拿到过期副本（回源 404→又写负缓存→回发 stale），属字面流程的自然结果，接受。
2. **panic 表达启动 fail-fast —— 确认**。R6 冻结签名 + §5.4「Get 内分流」⇒ 引擎必须装配在 `repo.New` 内；对齐 `adapter.Register` 装配期 panic 先例；真机 exit 2 + stderr 指名 env。备选构造器变体会留下无 remote 的分叉调用方，更差。
3. **无钥建仓丢弃 password + WARN —— 有保留地确认**。「未保护字节永不落盘」红线保持；但建仓 200 + fetch 静默匿名对 admin 是脚枪。建议 PM/conductor 在 M4 前定夺是否改 400（改动点 `sealPassword` 一处）；本票不改判。
4. **上游 original checksum 只登记不拒 —— 接受现状，要求 architect 回写 §4.5**。PRD v1.2「四值策略 M4」优先于架构草案成立；但注意：① §7.5 高置信度默认 `generate-if-absent` 是**拒收**，M4 落地时默认策略必须实现拒收，本票的 WARN 分支即 M4 的挂接点；② 当前 WARN 未把 original 值入库，M4 的 original 登记无历史数据（可接受，M4 票补）。**顺带**：§4.5「无副本 → 502」与 T-79 勘误一/PRD 定案（默认 404、仅 hardFail 502）矛盾，请 architect 一并勘误。
5. **其它 4xx → unfound 不开 offline —— 确认**。§7.6 该行为中置信度、PRD 基线只列 5xx/超时；「确定应答非故障」的读法技术正确，单分支可翻。

## 跨 area 申报裁决（2 准许 + 1 处未申报缺口）

1. **generic handler（StatusError 优先渲染 + ExtraHeaders 结构化探测，~30 行）—— 准许**。类无关、零 import、结构性探测使 §5.4「adapter 对三型无感知」保持机械化；T-71 的 405 文案与 Resolved-From 复用价值真实；`remote_render_test.go` 端到端钉板。保留。
2. **config env 白名单放行 `BINFLOW_REMOTE_CREDENTIALS_KEY`（+13 行）—— 准许**。无它则 ADR-0012 指定 env 在真二进制上无法设置（严格拒绝未知变量，实测 exit 1）；与 BINFLOW_ADMIN_PASSWORD 同款例外，注释 + doc.go 条目齐。保留。
3. **未申报：httpapi `writeStorageError` 缺 StatusError 分支**（见 non-blocking 6 / 范围外）。实现者以「线上可观察契约无法从两包送达 HTTP 面」论证了两处申报，同一论证在本处同样成立却只做了一半——建议 conductor 裁定归 T-71 或回 T-66 补。

## 结论重述

六步状态机主干、事务顺序、凭据链、分流接线、两处跨 area 申报均成立；但 §7.3 单飞 + §7.1 metadata 等待上限这两条**高置信度并发契约未实现到位**（均有实测证据），且实现者日志对此两处的声明与代码行为不符。修复面集中在 `attempt` 的等待者路径（预计 <40 行 + 2 条测试）。**REQUEST_CHANGES**，其余照单放行。
