# R13 载荷评审报告 — Reviewer A（correctness）

Ticket:        R13 载荷（origin/develop..HEAD，14 提交：T-593..T-603, T-606；BIN-75..85）
Role:          code-reviewer (reviewer-a, correctness：并发/失败处理/边界/错误路径)
Area:          internal/adapter/maven + generic + cargo、internal/remote、internal/httpapi、internal/repo
Input:         conductor 派发（评审重点 7 项）；`git diff origin/develop..HEAD` 全量代码面 + 上下游现行代码追读（evalConditional/rangecond.go、digestTriple/handler.go:684、clientChecksumSeam、markOffline/offlineWindow/fetcher.go:1510-1531、parseRemoteConfig/config.go:353-400、各 adapter writeServiceError 的 StatusError 优先序、cargo remote.go delete 面）
Changes:       put.go(+292) handler.go(+134) walk.go(+45) virtual_metadata.go(+16) generic handler.go(+160) fetcher.go(+127) cachestate.go(-28) router.go(+35) service.go(+21) + 14 个测试文件；docs/reports 面未读（B 审管辖）
Files:         逐文件见下方发现区；全部代码文件过目，重点函数通读至现行全文
Tests:         见「独立验证」；全部绿
Commands:      见「独立验证」
Outputs:       本报告
Compatibility: A 视角不适用（B 审管辖）；仅记录 2 条跨面兼容观察（NB-4/NB-5）
Security:      1 条 blocking：retrievalFaultMessage 把可含 userinfo 的上游 URL 逐字渲染进匿名可读的 404 body
Performance:   writeSidecarBody HEAD 臂新增一次 digestTriple（ledger 读，非计算）；writePlaneSrvgen/archiveSrvgenDeclared 各一次额外 class/store 读——写路径一次性开销，可接受
Risks:         见 NB 区
Blockers:      无（取证环境完整）
Next:          B1 建议转 security-auditor 复核；NB-7（metadata 路由 base 名大小写折叠超出 live 证据）建议补 live 腿或收紧匹配

## 评审报告 R13（形态: reviewer-a）
结论: APPROVE（2026-09-30 复审翻案：B1 已由 d413ff10 修复并复验，见文末复审记录；blocking=0）

### 逐项核对（conductor 派发的 7 个重点）

1. **archiveSrvgenDeclared 落地尾**（put.go:749-777）：SET 失败/重读失败 → writeServiceError over landed node，节点保持已落地态——两种中间态自洽（未注册→GET md5/sha1 答 404 Checksum-not-found；已注册→echo），无 panic 路径（seam nil 检查在先，rc.Close 在 err 检查之后）。与 putSidecar seam 失败姿态一致，documented。并发同路径 re-PUT 的 commit→SET 窗口为 last-writer-wins，与普通覆写链同级（NB-2）。writePlaneSrvgen 探针 miss 读作 client 默认：并发改仓下最坏一次 409（可重试自愈），无数据损坏（NB-1）。
2. **writeSidecarBody 动词条件模型**（handler.go:366-386）：GET etag 恒 ""，evalConditional（rangecond.go:134-146）对空 etag 在 etagMatch:150 直接 false → INM 永不 304，正确；IMS 仍按 lastMod 评估（零时间跳过）。HEAD triple 逐键 `!= ""` 门控，零值不渲染；sha1 缺失时无 ETag、INM 退化为 IMS。条件求值在 WriteHeader 之前、body 抑制（HEAD return）在 200 之后——次序正确。
3. **metadata 专用路由**（handler.go:83-93 → put.go:230-275）：drain 仅在 200 臂执行；401/403/405 早退不 drain 由 net/http 关连接兜底（与全 codebase writeError 姿态一致），无 fd/goroutine 泄漏。explode 400 → GetRepo → 路由门（405）→ 401 → 403 → drain+200，与 doc comment 钉死的 live 次序一致。发现 1 条 doc-code 偏差（NB-7：base 名也被大小写折叠）。
4. **上游故障两形**（fetcher.go:1084-1197）：markOffline/offlineWindow 互斥保护（1510-1531）；并发首障各自渲染 retrieval 形（各自开窗，幂等 set），窗内请求在 attempt 短路得 offline 形——翻转无 lost-update。5xx 默认臂 fault=nil 保持 offline 族（documented 保守）。NFR-S13 筛选链在 Step-2 删除后反而必经（原来被 a-priori 404 挡在筛选之前），负面腿 pin 在案。但 retrieval 形引入全 URL 逐字外化 → **B1**。
5. **generic sha256 按需计算**（handler.go:399-530）：local/virtual 两面均单 node 快照决策，无 TOCTOU（srvgen 探针只选面，不参与值）。virtual 成员解析失败：ErrNodeNotFound/ErrIsFolder → "Could not find resource; Path: '<virt>:<src>'" 冒号形指向 source，正确；其它错误 fall-through 普通链。
6. **deployEngineRemote 谓词**（router.go:2437-2441）：恰为 PackageMaven||PackageGeneric、仅 PUT、仅 TypeRemote——无误伤；其余包型保持 service 405，与测试钉死面一致。
7. **deleteRemoteCache StatusError**（service.go:1625-1629）：NewStatusError(404, msg, nil, wrap(ErrNodeNotFound))——全部 adapter 的 writeServiceError 均 errors.As(StatusError) 优先 → 状态码不丢、verbatim 渲染；Unwrap 保 sentinel 链（calc.go:626 类调用方不受影响，其只打 local 面）。

### 必须修改（blocking）

- ~~**B1（安全，凭据泄露面）**~~ **[已修复 d413ff10，复审通过]** `internal/remote/fetcher.go:1181-1185`（retrievalFaultMessage）+ `:795-799`（outboundFor 以 `JoinURL(cfg.URL, upPath)` 为 upURL）：首障 retrieval 形把上游 URL 逐字插进 404 body，而 `internal/repo/config.go:387-399`（parseRemoteConfig）对 url 只校验 scheme/host，`u.String()` **保留 userinfo**——管理员配置 `https://user:pass@host/base` 完全合法入库。旧文案（hostOf/upstreamHost，fetcher.go:867-872/1607-1615）只取 `u.Host`，本载荷首次把完整 URL 外化到**内容面**（匿名可读）。失败场景：remote 配置带 URL 内嵌凭据 + 上游首障 → 任意匿名读者 GET `/<remote>/x.sha1` 收到含 `user:pass@` 的 404 body。建议改法（render 侧一行根因）：retrievalFaultMessage 入口 `if u, err := url.Parse(upURL); err == nil { u.User = nil; upURL = u.String() }`；可选加固：parseRemoteConfig 存储/校验时拒绝或剥离 userinfo（管理面 config echo 亦受益）。

### 建议改进（non-blocking）

- **NB-1** `put.go:714-730` writePlaneSrvgen 探针任何 miss 读作 client 默认：并发删仓/改策窗口内 srvgen 面的 .sha512 PUT 可能走 client 臂得 409（应为 201+archive）。可用性级竞态、可重试自愈、无落盘损坏；documented best-effort。接受，记录在案。
- **NB-2** `put.go:749-777` archiveSrvgenDeclared 的 SET/重读失败 500-over-landed-node 与并发 re-PUT 的 commit→SET 窗口：中间态自洽、与 quota-refusal residue 姿态一致；无 torn row（单行 SET）。接受。
- **NB-3** `put.go:221-236` metadataChecksumRouteKey 对**整个文件名** ToLower 后比对 base——doc comment 与 live 证据只覆盖**后缀**折叠（wp1-case-SHA1/Sha1），base 名 `Maven-Metadata.xml.sha1` 这类大小写变体也被吞成 200 no-op，超出证据的兼容猜测（clean-room 纪律：禁猜测补齐）。建议补一条 live 腿或收紧为 base 精确匹配（`file[:len(file)-len(sfx)] == metadataFileName` 原串比对）。
- **NB-4** `fetcher.go:1100-1105` mapTransportFault 对 external hop（extHost != ""，FetchAbsolute/charts-base）也传 upstreamFault：该面 no-copy 404 从"外方 host 不可达"族换成 retrieval 形并逐字引用第三方 target URL——ruling 的 live 腿只覆盖配置上游臂，外方面未探测。低风险，建议 B 审/compat 侧登记或补腿。
- **NB-5** `internal/repo/service.go:1625` deleteRemoteCache 的 StatusError 经各 adapter StatusError-first 渲染，**同时**改写了 conan/deb/nuget/helm/rpm 的 remote DELETE-miss 文案（不止 commit 所列 generic/cargo/conan/deb）；全树测试绿说明无旧钉冲突，但跨面爆炸半径超出 T-596 文案域声明——交 B 审核对 wording-domain 主张是否覆盖这些面。

### 独立验证（本机复跑，全部实际执行）

```
go build ./...                                   → exit 0
go vet ./...                                     → exit 0（零告警）
gofmt -l internal/                               → 空
go test -race -count=1 ./internal/adapter/maven/ -run 'TestSrvgen|TestSidecarHeader|TestSrvgenDeclared|TestMetadataChecksumRoute'   → ok 8.890s
go test -race -count=1 ./internal/remote/        → ok 103.561s
go test -count=1 ./internal/adapter/generic/ ./internal/repo/ ./internal/adapter/cargo/   → ok 19.0s / 84.5s / 21.6s
go test -count=1 ./internal/adapter/maven/       → ok 30.655s
go test -count=1 ./internal/httpapi/ -run 'TestDeployRefusal|TestRemote'                  → ok 3.996s
```

新测试抽查为真实行为断言（srvgen_declared_archive 三臂、sidecar_header_verb_model 条件腿、remote_sidecar_backsource 首障/窗内两形 + NFR-S13 负面腿、metadata_checksum_route 排序/miss/source 引用），非 mock 自证。

结论: APPROVE（blocking=0；原 B1 经 d413ff10 修复后复审通过）

---

## 复审记录（B1 修复验证，commit d413ff10）

**修复面比原发现更宽（采信）**：原始 transport error 实际喂三个匿名可读面——(a) retrieval 404 的 URL 插值（原发现）；(b) 同 body 的 cause 文本（Go http client 错误以 `Get "http://user:***@host/..."` 引用完整 URL，stdlib 只掩密码、**用户名明文**）；(c) downgrade summary → 过期副本 STALE 服务的 X-Binflow-Upstream-Error 头（cachestate.go:198）与 hardFail 502 body（fetcher.go:1147-1151）。

**机制核对**：单一 regex `//[^/@?#/s]*@` → `//`（fetcher.go redactUserinfo），落在两个 render choke point——retrievalFaultMessage 整 body 包裹（:1189-1192）+ downgrade 入口（:1139-1142，先于 logResult/serveStale/hardFail 三处消费）。regex 同时覆盖裸 `user:pass@` 与 Go 掩码 `user:***@` 形；无 userinfo 的 URL 不受影响（`@` 必须出现在首个 `/` 之前即 userinfo 位）。
**choke point 完备性复核**：upURL 仅流入 mapTransportFault → retrievalFaultMessage（已包裹）；summary 的全部四个生产者（transport/5xx/offline-window/pullBlocked）经 downgrade 入口统一 redact；pullBlocked 臂 hardFail 502 为固定文案不掺 err/URL（:572-577）；extHost no-copy 404 只用 u.Host。未发现第四个旁路面。
**测试核对**：TestRetrievalFaultRedactsUserinfo 双面钉死（no-copy retrieval 404 + 过期副本 STALE UpstreamError），断言 redacted URL 在场、hunter2/ci: 缺席——同时覆盖 URL 插值与 cause 文本两条泄露径。测试有牙（实现者自述首版 url.Parse-only 修法被该测试打回）。

**独立复跑（本机）**：
- go build ./... && go vet ./internal/remote/ && gofmt -l internal/remote/ → 全绿
- go test -count=1 -v ./internal/remote/ -run TestRetrievalFaultRedactsUserinfo → PASS (1.33s)
- go test -count=1 ./internal/remote/ → ok 30.958s
- go test -race -count=1 ./internal/remote/ -run 'TestRetrievalFault|TestFetchTransportRefusal|TestRemoteSidecar|TestOffline' → ok 5.308s

**范围外发现（交 conductor，非本载荷引入）**：fetcher.go:1484 upstreamPropsURL 以 cfg.URL 全量构造 source-origin 属性落在节点上（properties 面可见，需认证读取）——凭据 URL 入库时同样外化，属 R13 之前的既有面，建议另开小票对齐 redact 策略（或 parse 侧统一拒绝 userinfo，一劳永逸关闭全部面）。

复审结论: APPROVE（blocking=0；NB-1..5 维持原判不改码）