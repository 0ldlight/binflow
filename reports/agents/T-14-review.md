# T-14 评审报告 — httpapi 核心（视角: correctness + security，单 reviewer）

- 评审人: code-reviewer
- 日期: 2026-08-18
- 结论: **REQUEST_CHANGES**（blocker 3 / major 2 / minor 8）
- 取证命令（均实际执行）:
  - `go vet ./internal/httpapi/... ./internal/console/...` → 无输出；`gofmt -l` → 无输出
  - `go test -race -count=1 ./internal/httpapi/... ./internal/console/...` → ok（5.0s / 1.7s）
  - `go test -cover ./internal/httpapi/ ./internal/console/` → 82.9% / 85.7%（与日志声称 83% 一致）
  - 临时探针（模块内测试文件，跑完已删，工作树已恢复 clean）：裸 TCP 重放 19 条混合形态请求行 + 坏认证头 5 形态 + `%2F` 编码斜杠的 ACL 判定探针（授权 granting/exclude 两态 × 匿名/已认证 × GET/PUT/DELETE）

---

## 必须修改（blocking）

### B1. `/readyz` 缺 storage 可写检查 — 违反 architecture §7.1 契约
- 位置: `internal/httpapi/router.go:46-58`（`probeHandler`）
- 问题: §7.1 明文 `/readyz GET 就绪（metadata ping + storage 可写）`。实现只 `Metadata.Ping`，storage 可写探测（`probeStorage`，system.go:104）只用在 `/binflow/api/v1/health`。数据目录只读的实例（卷只读、磁盘满、权限错）会报 ready，K8s 继续把流量（含上传）路由到一个写不进盘的节点。
- 建议改法: ready 分支加 `if st := s.probeStorage(); st.Status != "ok" { writeError(w, 503, "storage not ready: "+st.Detail) }`；补一条「data_dir 只读 → /readyz 503」的测试（harness mutate 把 DataDir 指向一个 0555 目录即可）。

### B2. `dispatchAPI` switch 出现两条完全相同的 case — 死代码掩盖了一条丢失的路由
- 位置: `internal/httpapi/router.go:110-113`
```go
case rest == "system/ping" && r.Method == http.MethodGet:
    s.enforce(w, r, routeAuth{}, handlePing)
case rest == "system/ping" && r.Method == http.MethodGet:
    s.enforce(w, r, routeAuth{}, handlePing)
```
- 问题: Go 静默接受重复且永不可达的 case。第二个分支显然是某条本应不同的路由（最可能：`system/version` 的 HEAD 变体或 `v1/health` 的笔误）被复制后忘了改条件。无论原意是什么，现在的代码在撒谎。`go vet`/golangci-lint 均不报。
- 建议改法: 删除重复分支；如果原意是补某条路由（如 `system/ping/` 尾斜杠），补上并加测试。

### B3. `statusRecorder.WriteHeader` 把「先 200 写出、后置错误码」记成 200 — 访问日志状态失真
- 位置: `internal/httpapi/middleware.go:56-61`
```go
func (s *statusRecorder) WriteHeader(code int) {
    if s.status == http.StatusOK && code != http.StatusOK {
        s.status = code
    }
    ...
}
```
- 问题: `status` 初值即 200，条件 `s.status == http.StatusOK` 在「handler 已隐式 200 写过 body、随后再显式 WriteHeader(4xx/5xx)」时为假（`s.status` 仍是初始 200，但 `Write` 已经发生）——判据把「从未写过」和「已按 200 写过」混为一谈。凡 handler 先 `Write` 一段再报错的路径（流式下载、adapter 的 partial write），访问日志记 200，NFR-S3 的观测面说谎；且函数注释「WriteHeader is recorded once (later calls are no-ops)」与实际行为不符。
- 建议改法: 加 `wrote bool` 字段，`WriteHeader` 首次调用无条件记录 `code`；`Write` 隐式置 `wrote`（不覆盖已记录的 status）。与 M1（见下）共用同一标志位。

## 建议尽快修（major，非本轮阻断）

### M1. recover 在「响应已写一半」时把 500 信封注入响应流 — 污染下载制品
- 位置: `internal/httpapi/middleware.go:159`（`writeError` 在 recover 内无条件调用）
- 实证: recover 位于 accessLog 内层，panic 发生在 body 已流出（如 generic 大文件 `io.Copy` 中途）时，`writeError` 的 JSON 会**追加**进已发出的 200 字节流——客户端拿到「200 + 制品字节 + 错误信封字节」的静默损坏制品。`http.ErrAbortHandler` 重抛只覆盖了显式请求静默断连的场景。
- 建议改法: recover 里先查 statusRecorder 的 wrote 标志（B3 引入）；已写出则只记日志、不再写任何响应字节（连接交给 net/http 截断），未写出才写 500 信封。补一条「mid-stream panic 不追加字节」的测试（console 换成先 Write 后 panic 的 handler）。

### M2. 授权与路由按 **escaped** 段判定，adapter 按 **decoded** 寻址 — 两层判的不是同一条路径
- 位置: `internal/httpapi/middleware.go:302-310`（`splitFirstSegment` 用 `EscapedPath`）、`router.go:157`
- 实证（编码斜杠探针，已跑）:
  - `GET /binflow/generic-local/priv%2Fsecret.bin`（ACL exclude `priv/**`）: router 层按字符串 `priv%2Fsecret.bin` 匹配 exclude 不中→ 放行到 service；**repo.Service 用解码后的 `priv/secret.bin` 再查 ACL → 403 兜住**。反向（escaped 命中、decoded 不命中）→ router 层 403 文案与 service 层不同但结论一致。
  - **无越权**——M1 的纵深防御（service 层 `allow()` 用解码路径重查）目前兜住了所有探针组合（GET/PUT/DELETE × 匿名/已认证 × `%2F`/`%2f`/混合点段）。
  - 但有两个非越权后果: ① 外层 ACL 判的字符串与存储寻址的字符串不同，纯靠第二层兜底，任一未来 adapter（M2 docker/M3 maven）若不复用 repo.Service 的 allow 就直接裸奔；② 功能缺陷——`/binflow/generic%2Dlocal/x.bin`（连字符被编码）escaped 首段 `generic%2Dlocal` 查不到 repo 行 → 404 repo not found；Artifactory 的 RepoFilter 按解码路径匹配 repo key，这类客户端在 BinFlow 永远路由失败。`/binflow/%61pi/...` 同理。
- 建议改法: `splitFirstSegment` 对结果做一次 `url.PathUnescape`（或直接以 `r.URL.Path` 为准取首段），保证授权 keying、repo 行查询与 adapter 寻址三者同源；补「%2D/%61 编码的合法 repo key 可路由」「%2F 编码路径的 ACL 判定与明文一致」两条回归测试。

## 建议改进（minor / nit）

1. `system.go:131` v1/storage/stats 只要「已认证」不要 admin。§7.1 只写「全部需认证」，字面合规；但全局 blob 数/物理字节是运维面数据，建议 T-15 挂权限面时收成 admin（或至少在 T-15 票里显式决策）。
2. `router.go:108-127` 已知路径 + 错误方法（如 `POST /binflow/api/system/ping`）落到 default → 404 "not implemented"，REST 语义应为 405 + Allow。E-26 不受影响，但和内容路径的 405 姿态（adapter 有）不一致。
3. 探针端点接受任意方法（`POST /healthz` → 200 OK）。K8s 只发 GET，无实害；建议限定 GET/HEAD。
4. `context.go:33` requestID 的 `rand.Read` 失败回退 `"unavailable"` → 大量请求共享同一 id，且和「不泄露请求量」的设计意图相悖。回退建议改 `time.Now().UnixNano()` 十六进制。
5. CORS：带 Origin 的 OPTIONS 对**任意**路径（含不存在的）答 204。这是 CORS 惯例，可接受；建议在 middleware 注释里写明「preflight 不进 dispatch，故不受 E-26 约束」，防后人误当缺陷。
6. 覆盖盲区（83% 的缺口正好都在错误路径上）: /readyz 503 分支（metadata down）、无 adapter 挂载的 package type 分支（router.go:163-171，需注入假 RepoLookup 才能触达）、mid-stream panic、`%2`-编码 repo key、PUT 的 bytes_in>0 断言、点段请求的 accessLog path 记录（注释声称「dot-segment probes stay visible to operators」但无断言）。
7. FR-5-AC12 的第三格「匿名 GET /binflow/api/repositories → 401」当前实测 404（T-15 未挂端点，先落 E-26②）。无数据泄露，属可接受的中间态；提醒 conductor：该 AC 的最终验证依赖 T-15 挂上 `required:true` 后复测。
8. `server.go:148` Shutdown 超时返回 wrapped error → Run 返回非 nil → §7.4「SIGTERM 退出码 0」在「排空超时」场景可能落空。T-16 装配时需要决定：超时是否仍 exit 0（或 1）。请在 T-16 票注明。

## 重点核查项结论（对照评审委托单）

1. **路由安全**: 手写分发树实测通过——19 条混合形态探针（`/healthz/../binflow/api/...`、`..%2f`、`//healthz`、`/./healthz`、`/%68ealthz`、点段/编码点段内容路径、`%2D`/`%61` 编码段）全部**非 3xx**，无 mux 归一泄漏；mux 只在 EscapedPath 精确等于 `/healthz`/`/readyz` 时才被调用，无法被路径混淆命中。dot/编码 dot 到达 adapter 后由 Layout 400 拦截（信封形）。唯一残留 = M2 的 escaped/decoded 错位（无越权，有功能缺陷）。`v2` 预留 404 与 E-26② 一致。reserved 判定层次（repo 层 `api`/`v2` + adapter 层 `IsReservedSegment`）双层成立。
2. **认证分层**: 与 FR-5-AC12/AC13 矩阵一致（测试以 AC 命名，匿名 GET/HEAD 200、PUT/DELETE 401+challenge、管理面 401、坏凭据 401 不降级匿名、已认证无权 403 vs 匿名 401 边界正确——实测 `Digest abc` 这类未知 scheme 视为匿名而非坏凭据，随后被 required 门拦成 401，语义自洽）。坏 Basic 头（`!!!`、`Basic %%%%`、截断 base64）→ 401 + `Basic realm="BinFlow Realm"`。version 匿名开放的自裁量已固化在 `system_test.go`（匿名用例）+ 代码注释，留了 T-15 收紧缝，合格。
3. **中间件链**: 顺序与 §7.2 逐项一致（requestID→accessLog→recover→CORS→authn→authz(按路由)→handler；authz 挂在 terminal 前等价成立，测试 pin 了可观测不变量）。accessLog 字段齐、绝不记 Authorization（测试断言 base64 值与明文对都不出现——hook 捕获断言真实）。bytes_in 按 Content-Length 声明值（遗留③：chunked=0，注释已写明，**可接受**；若 QA 要实测值再包 body reader）。requestID 唯一性有测试（32 次无重复），传播（响应头 + 日志字段）已断言。recover 缺 M1 所述的「已写一半」防护。
4. **优雅停机**: Run(ctx 取消)=Shutdown(GracefulTimeout) 语义正确（等待在途，超时强断），幂等有测试；storage→metadata 关停顺序留在 cmd 单点（§7.4 归位正确）；双信号归 T-16。见 minor 8 的退出码提醒。
5. **E-26 矩阵**: 五类 404 文案与信封全过（根路径前缀提示、api 未实现、v2 预留、未知 repo spec 文案、无 adapter E-26 形）；`/artifactory/**` 提示 `/binflow` 有断言；实测无 500/空 200。
6. **遗留①（RepoLookup 缝）裁决意见**: **可接受，无需回滚**。证据: ① 内容路径上 `authorize()` 包在 repo 行查询**外层**（router.go:176 `enforce` 包 `inner`），`anonymous_access=false` 时匿名/无权者在查询前就被 401/403 拦下——能借缝探测私有 repo 存在性的只剩「本就通过读门禁」的主体，对他们的存在性信息不构成增量；② 默认匿名开时，能读即能知其存在，存在性被读能力包含；③ 只读 PackageType 一列，config 不过缝。给 architect 的建议措辞: 「路由层 repo 存在性解析位于授权门之后、仅取 packageType，不构成越权信道；建议在 repo.Service 增设 `PackageTypeOf(ctx, key)`（匿名可用、只回包类型）作为正式缝，把 metadata 直读收回服务层——列为 T-15/M2 的非阻断重构项，并在 ADR-0009 补一句『路由解析先于 adapter、后于授权门』」。
7. **测试质量**: 真栈 harness（sqlite+storage+repo+auth+generic 全真件）稳定，`-race` 多轮全绿；裸 TCP 探针手法正确（Go client 不发未归一请求行）。盲区见 minor 6。

## clean-room 抽查

httpapi/console 无逐行对应嫌疑: Go 实现与反编译 Java 无结构对应；realm 文案为改写的 `BinFlow Realm`（reverse-src 为 `Artifactory Realm`）；404 文案取自 docs/reverse/rest-api.md 的行为规格（§1.4 高置信度），非代码翻译。通过。

## 范围外发现（交 conductor）

- PRD §5.2 C28a/QA 剧本 `curl -sf $BASE/binflow/api/v1/health` 未带凭据，而架构 §7.1 定案该端点需认证——QA 剧本需补 `-u`，否则 C28a 会误报失败。
- T-16 装配清单提醒（日志遗留②已覆盖）+ 上述退出码决策（minor 8）。
