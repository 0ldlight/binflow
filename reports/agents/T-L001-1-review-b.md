# T-L001-1 · review-b（architecture/compat 面）

```
Ticket:        L001-1 · docker remote DIVERGENT 族清偿 · P1
Role:          code-reviewer (reviewer-b)
Area:          internal/adapter/docker（remote 代理 v2 面）+ 扩散测试（helmoci/httpapi 测试）
Input:         conductor 派发（reviewer-b 形态）；通读 git diff（docker/helmoci 两目录全量）、L000-docker-remote-diff.md 复验两段（L000-F/L001-1）、L000-docker-remote-evidence.md E1/E5/E6/E7/§8、docs/compatibility/contracts/docker-remote.yaml 全 21 条目、DECISIONS.md ADR-0047/0048、reports/agents/T-L001-1.md、上下游（handler.go 路由序/authorizeRoute/scope.go/virtual.go walk/catalog.go 根级面/token.go/helmoci handler 装配）
Changes:       diff 11 文件（api/catalog/errors/handler/remote/token/virtual + 4 测试文件）+ 2 新测试文件；上下游追读至 serveNameRoute 路由序、authorizeRoute/splitScopeToken、writeRemoteServeError、serveCatalog 根级面、helmoci New(plane *docker.Handler) 装配
Files:         errors.go ✓（statusForm 模型单点渲染器，与 writeSpecError 并置）；token.go ✓（三臂 E1-4/E1-5 原样，参数 400 保留 OAuth——注释锚定充分）；handler.go ✓（bearerChallenge 合一消除双源；repoCatalogKey 拦截位次正确）；remote.go △（fetchList/翻页/两聚合面锚定扎实；但 manifestUnfound/blobUnfound 形态变更经共享函数传导 virtual 面——见 blocking）；virtual.go △（传导点，未声明未测）；catalog.go ✓（repoCatalogKey 精确匹配，不可穿越）；api.go ✓（注释勘误）；测试 6 文件 ✓（逐字段断言 E6 体、live 聚合可变上游+命中计数、翻页合并、降级、路由门）
Tests:         go build ./... ✓；go vet 两包 ✓；golangci-lint 两包 0 issues ✓；go test docker/helmoci/httpapi -count=1 全 ok（20.7s/17.2s/243.8s）✓；-race 定向 RemoteTagsList|RepoCatalog|RemoteUnfound|RemoteWriteRefusal|RenderAuthFailure|TokenWrongCredentials ok ✓；实现日志声称的命令复跑相符（无虚假证据迹象）
Commands:      git diff internal/adapter/docker/ internal/adapter/helmoci/；go build ./... && go vet ./internal/adapter/docker/... ./internal/adapter/helmoci/...；~/go/bin/golangci-lint run 同两包；go test ./internal/adapter/docker/ ./internal/adapter/helmoci/ ./internal/httpapi/ -count=1；go test -race ./internal/adapter/docker/ -run 'RemoteTagsList|RepoCatalog|RemoteUnfound|RemoteWriteRefusal|RenderAuthFailure|TokenWrongCredentials' -count=1；grep 取证 ServiceID/negative|marker|DigestChain（diff 零命中）
Outputs:       reports/agents/T-L001-1-review-b.md（本文件）
Compatibility: 7 项修复逐案锚定核对通过：C02→E1-5（含 CLI unknown: 指纹实测）、E1-4 匿名关闭臂、C12→E6-1、C13→E6-2、C11→E5-1..3（未观察组合保留 405）、C15→E7-1（上限 3=E7-2/§8 键默认表）、C16→E7-2、C01→derived_rules.service_equals_request_host；与契约条目 path_literal/keys_absent 逐字一致。四处自决角落（tags:null、未观察 405、参数 400 OAuth、2-space pretty）均在代码注释+diff 报告双声明，可接受。缺一项声明：unfound 形态经共享函数传导 virtual 面+经共享 Handler 传导 helmoci 面（见 blocking/范围外）
Security:      repoCatalogKey 精确段匹配不可穿越（key 非 / 单段，repos.Get 键查非文件路径）；fetchList 翻页逐跳走 remote.Client（SSRF 复筛未绕过）；catalog 面 NAME_UNKNOWN 不可区分 posture 保持；serveRepoCatalog 匿名关闭先挑战后 404（比旧 404 直出更收敛，与平面 doctrine 一致）
Performance:   （B 形态侧记）tags/_catalog 每调用实时上游往返=E7-1 同姿势；列表体 buffered 4MB 上限+翻页 cap 3 有界；无锁/分配面劣化
Risks:         virtual 面 unfound 传导（blocking 主题）；serveRepoCatalog 匿名关闭 401-先于-404 与未授权 403-先于-404 两处次序 delta（与 serveNameRoute 既有序一致，未观察，建议补一行注释声明）；contract 文件 7 条 binflow_state 仍标 DIVERGENT 待 L001-2 刷新
Blockers:      无取证障碍（全量测试可跑）
Next:          ① 实现者补 virtual 传导声明+测试后收（见 blocking）② contract/known-divergence 状态刷新归 L001-2/conductor ③ PRD v1.2/C3（FR-11 token 错误体二分）被实测证据推翻，需 product-manager 走勘误 ④ router.go:291 兜底 service 常量（实现者已报，生产不可达）随手票 ⑤ D21 已正确转 httpapi 域（本 diff 未越界，确认）
```

## 评审报告 T-L001-1（形态: reviewer-b）
结论: REQUEST_CHANGES

### 必须修改（blocking）

- `internal/adapter/docker/virtual.go:89` + `virtual.go:287` → 新 unfound 形态（静态 message + detail.manifest/blobSum）经共享函数无差别传导到 **virtual 面**的 wire 404 体（`virtual.go:182`/`:345` 实际写出），而契约域明确「不含 local/virtual 面」（docker-remote.yaml scope 行）、E6 证据仅采自 remote 面且 L001-1 复验也未触 virtual；该传导**既未列入报告 Risks 自决清单，也无任何测试钉住新形态**（virtual_test.go 未改、不断言 unfound 体）。兼容票纪律=未观察行为必须声明；改法二选一（推荐 a）：
  (a) 在 T-L001-1.md Risks 与 diff 报告「残留与边界」补一行「unfound 形态经共享渲染函数传导 virtual/helmoci 面（依据 §8 E1 走读：E6 渲染位于共享 v2 REST 层 org.artifactory.addon.docker.rest.v2，非 remote 专有；属推断非观察）」，并加一个 virtual 面最小测试钉住传导后的体（仿 remote_unfound_shapes_test.go 三臂之 tag 臂即可）；
  (b) 或将 virtual 面拆回旧形态待 virtual 面差分证据（不推荐——旧形态同为无证据自创，共享渲染在架构上是对的）。

### 建议改进（non-blocking）

- `internal/adapter/docker/remote.go:727-747`（serveRepoCatalog）→ 两处未声明的次序 delta：匿名关闭实例上 `/v2/<repo>/_catalog` 先 401 挑战后 404（旧为 parse 404 直出）；已认证无读权限主体先 403 后（非 remote 行的）404。两者均与 serveNameRoute 既有 doctrine 一致（challenge 先于 repo 门/authorizeRoute 先于 class 分支），无安全劣化，但属未观察角落——建议代码注释一行或 Risks 补记。
- `internal/adapter/docker/remote.go:732-747` → serveRepoCatalog 复制了 serveNameRoute 的 repo 门块（repos.Get + 500/404/authorizeRoute 约 15 行）；当前一处复制可接受，第二个 repo 域专用路由出现时应参数化收编。
- `docs/compatibility/contracts/docker-remote.yaml` → 7 条修复案的 binflow_state 仍标 DIVERGENT/reverify_after L001-1，与 diff 报告 L001-1 复验段 SAME（灭）不一致——契约刷新归 L001-2/conductor 流程，勿留双源矛盾。
- token.go 注释宣告「superseding the PRD v1.2/C3 ruling」→ 代码注释不能退役 PRD 裁定；PRD（FR-11 错误体二分/T-55）需 product-manager 勘误，转 conductor。
- `internal/adapter/docker/token.go` formCredentials 坏凭据臂不带 WWW-Authenticate 头（renderTokenAuthFailure 臂带）——契约条目 headers 只要求 Content-Type，合法；仅记此差异供后续 token 端点头集差分时对照。

### 逐面确认（无问题的面）

- **契约锚定**：7/7 修复逐字锚定 E1-4/E1-5/E5-1..3/E6-1/E6-2/E7-1/E7-2/§8 键默认表与契约断言词汇；三处字面（"Bad Credentials"/"Authentication is required"/三禁推文案）与证据逐字节一致；tags name=上游回显名（非本地归一）符合 E7-1；除 virtual 传导（blocking）外无超出观察面的自创行为，四处自决角落双声明齐备。
- **架构一致性**：错误形态单点收敛（writeStatusFormError 与 writeSpecError 并置 errors.go，4 调用点零散落构造）；bearerChallenge 合一后 renderTokenAuthFailure 双源消除（grep 全仓仅 handler.go 一处构造 + api.go 常量回退）；fetchList 复用 attempt/dance 既有机件，internal/remote 保持纯传输（URL+guard），Link/scope 协议语义留 adapter——与 manifest 臂分层一致，聚合职责未越层，无需下沉 remote 层。
- **回归风险**：local 面禁推（refuseNonLocalWrite 独立函数）与 local manifest/blob 404（不走 manifestUnfound/blobUnfound）零触碰；根级 /v2/_catalog（serveCatalog）与虚拟面 tags（serveTagsList 缓存行）未动（复验段已声明）；非 remote 行的 `_catalog` 经 parseV2Name notAName 错误原样复刻旧 404 体；E2-6 library/ 归一化确认**未半实现**（全 diff 无归一化代码；tags 靠上游回显、detail.manifest=ref.image 无前缀，已在 Risks 声明留后续票）；C04 回归腿复验在案。三包全量测试+定向 race 通过。
- **ADR-0047/0048 边界**：diff 对 negative/missedTTL/marker/DigestChain 零命中——C10 marker 门控与 C14 负缓存语义未触碰；blobUnfound 仅改 body 形态，与 ADR-0048 保留的负缓存正交可组合；C10b 时延分歧（upstream_rtt:false）维持 DIVERGENT 未被顺手"修复"。边界干净。
