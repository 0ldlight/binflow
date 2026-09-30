# R14 载荷 Review A（correctness 面）

Ticket:        R14 载荷 PR 评审（range d9ea6344..HEAD，9 提交；代码票 T-617/T-608/T-607，文档票 T-604/T-609/T-610/T-611/T-615 + aa9d107c 台账批）
Role:          code-reviewer (reviewer-a, correctness)
Area:          internal/remote（redact）+ internal/repo + internal/adapter/{generic,maven}（下载头集）+ internal/httpapi（repo config REST 错误族）
Input:         conductor 派发单（三代码票重点 + 文档票锚点抽样）；通读 architecture 分层约束、known-divergence 台账、T-607/T-608/T-617 报告全文、/tmp/t607、/tmp/t608、/tmp/t617 证据
Changes:       逐行审三代码票全部产品 diff；上下游追读：requestMediaType/writeError/repoBatchDeleteReport/writeRepoSvcError/parseRemoteConfig/configJSON、evalConditional/etagMatch/digestsOf/parseRFC3339/mimeByPath、clientFor/upstreamPropsURL/JoinURL/targetURL/CheckURL 四调用方、web 前端 rawRequest CT 行为；文档票抽验 file:line 锚点 8 处 + 台账计数全量
Files:         internal/httpapi/repositories.go（415 门/charset 臂/404 族/DELETE 报告体——逻辑核毕，无缺陷）；internal/repo/config.go（闭域枚举+指针 merge——无缺陷）；internal/repo/downloadheaders.go（CD 双形+416 裸集——无缺陷，见 N1）；internal/adapter/generic/handler.go + maven/handler.go（verb-conditional echo+416 单源——无缺陷）；internal/remote 4 文件（7 站点 redact——闭环核毕，见 N2/N3）；14 个既有测试文件（抽 4 全量+余 10 扫描：全部为 CT 请求管道改动，断言零改）
Tests:         go test -count=1：remote ok 57.8s / repo ok 116.3s / generic ok 27.7s / maven ok 50.8s / httpapi ok 223.5s（exit 0）；-race 定向：remote(Userinfo/Redact/AuthSemantics) ok 7.5s、repo+generic+maven(Download/Sidecar/SetDisposition/WriteRange) ok 6.5-11.1s
Commands:      cd worktree && go build ./... && go vet ./internal/{remote,repo,adapter/generic,adapter/maven,httpapi}/ && gofmt -l internal/（空）；GOLANGCI_LINT_CACHE=/tmp/gcl-r14-revA golangci-lint run ./internal/...（0 issues）；上述 go test 五包 + -race 三组；/tmp/hdrinj_test.go 实证 Go server 对含 CRLF 头值的中和（见 Security）；python3 归一化比对 /tmp/t607 四 JSON 30 腿、/tmp/t608 A-r2 40 腿逐头、known-divergence.yaml safe_load 计数
Outputs:       本报告 reports/agents/R14-pr-review-a.md
Compatibility: 台账三 resolved 条目（put-ct-strictness / artifact-get-response-headers / remote-domain-policy-enum-gate 含 409→400 偏差记录）与 /tmp 证据、实现三方一致；trailing-space side-finding 已按流程上新条目（BUG open）；A 活体头形（CD 双参/filename*-only/416 裸集/304 保留对/sidecar verb 模型）与 /tmp/t608/probe-a-r2.json 逐头吻合
Security:      T-617 闭环复走：站点 #12（fetcher.go:1492-1521 sync-props WARN err.Error()）裁定复核成立——props 请求走 Request{URL:派生 URL}（upstreamPropsURL 只取 Scheme/Host/Path，无 userinfo），transport 错误文本不含凭据；主拉取面 summary 于 :1140 统一 redact；client.go:153「构造性不可达」核实（internal/repo/config.go:402 url.Parse 门在先）。SetDownloadDisposition 裸文件名内插经实证不构成头注入（Go net/http 对 CRLF 头值中和，raw wire 单头折叠）；全 range 2780 新增行凭据/IP 扫描 0 命中，测试凭据均为占位符（ulogin-t617/FAKECRED-T617）
Performance:   CT 门=常数比较；redactUserinfo=故障/探测/拒绝路径单次正则；writeChecksumEcho HEAD 增 digestsOf（同请求内 md.Get 一次，与主面 handleGet 同模式）；无热路径新分配面
Risks:         ①T-607 三未探角（rclass-less+参数化 CT、不可解析 CT 串、裸分号）按 fail-closed 落地且已披露——接受；②redactUserinfo 正则对用户名含裸 @ 的 URL 只删到第一个 @（残留 er:pass@host 类）——R13 既有正则、本 range 未改，见 N3；③T-608 文件名含 DQUOTE/反斜杠时 quoted-string 形态不闭合（A 未探、verbatim 姿态），见 N1
Blockers:      无
Next:          ①建议微票：下载文件名含 `"`/`\` 的 A 活体探腿（gl-get-quote 之类）后再决定是否转义（无证据先转义=主动分叉）；②建议随 T-617 转派的 httpapi 微票一并收紧 client.go:376 target-parse wrap 的 redact（或 JoinURL 路径段转义）；③N3 正则加固可与②同票；④bundle_wire_test 内联重复 repoConfigWritePath 判别（风格）

## 评审报告 R14-pr（形态: reviewer-a）
结论: APPROVE

### 必须修改（blocking）
- 无。三张代码票的正确性面（415 门边界、PUT 双 rclass 内插、POST 两变体 404、DELETE 报告体、闭域枚举、CD 双形/416 裸集/304 保留、redact 七站点含第二层 parse 错误臂）逐一核毕，与 /tmp 活体证据三方一致；测试全绿含定向 -race；台账计数精确复现（149=86+63，BUG 10o/UNKNOWN 41o/INTENTIONAL 10o/UNSUPPORTED 2o，dup-id 0）。

### 建议改进（non-blocking）
- N1 internal/repo/downloadheaders.go:46-48：ASCII 臂 quoted filename 未对 `"`/`\`/CTL 转义（`filename="a"b.bin"` 形态不闭合）。实证无头注入（Go 中和 CRLF），且 verbatim-A 姿态下不宜擅改——建议先补 A 探腿定行为（Next①）。
- N2 internal/remote/client.go:376：`remote %s: target url: %w` 包装错误仍内嵌全 URL；JoinURL(client.go:286) 不转义路径段，artifact path 含破坏 url.Parse 的字节时可达。票面已披露为「构造性不可达」，其中 :153 臂经 config.go:402 核实成立，:376 臂判定偏乐观（路径是运行时输入非「已 parse URL 往返」）；但下游主渲染面（retrievalFaultMessage/:1140 summary）均再 redact，实际暴露≈仅日志残面。建议随转派微票加固。
- N3 internal/remote/fetcher.go:1195 `reUserInfoInURL = //[^/@?#\s]*@`：Go url.Parse 按 LastIndex 取 userinfo，用户名含裸 `@` 时（`http://us@er:pass@host`）正则只删 `//us@`，残留 `er:pass@host`。R13 既有正则、本 range 仅改注释；极端配置角，建议后续正则收紧（贪心至最后一个 @）。
- N4 internal/httpapi/bundle_wire_test.go：内联重写 repoConfigWritePath 判别逻辑（CutPrefix "repositories/" 变体）——复用 harness 助手即可（风格）。
- N5 repoConfigCTParameterized 对退化拼写 `application/json;`（无参数）按参数化臂走 400 拒绝族——票面已披露 fail-closed，接受，仅记录。

### 抽样核验记录（文档票锚点）
- parseVirtualConfig@internal/repo/config.go:744 ✓；OriginalChecksums(node, serverSha256)@internal/repo/api.go:899 ✓；clientChecksumSeam@internal/adapter/maven/put.go:789 ✓；promote.go:386 virtualDefaultDeployment 调用/404 副本仍在（ADR-0054 工程票未动码，一致）✓；maven/walk.go:163/267 overlayClient 形 ✓；agent-graph.yaml internal/build 回填（owns/writes 对称）✓；maven-virtual.yaml +174/-0、remote-fetch.yaml +96/-0、repo-semantics.md +14/-0 纯追加 ✓。

### 断点快照
已完成：全部评审与取证。未完成：无。
