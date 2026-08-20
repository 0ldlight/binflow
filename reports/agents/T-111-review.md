# 评审报告 T-111（视角: correctness + 协议一致性）

结论: **REQUEST_CHANGES**（1 blocking；verbatim 渲染臂本身实现正确、与四 adapter 先例一致、测试证明力充分）

- 被评审: commit `8ed20f7`（工作树 docker 包与该 commit 一致；工作树其余改动为 T-97/T-94 等并行票 WIP，未纳入本次评审）
- 输入: reports/agents/T-111.md、T-95.md 遗留①、generic(T-66)/maven·npm·pypi(T-82) verbatim 先例、docs/reverse/docker-registry.md §1、docs/prd/milestone-4.md FR-31-AC3/AC5·NFR-S23

## 逐项审查结论（对应 conductor 五个重点）

### 1. specCodeOfVerbatim 映射 — **通过**

- **413/409 → DENIED**：官方码表中确无 quota 专属码；DENIED 的官方语义（"the access controller denied access for the operation on a resource"）是「对资源上操作的策略拒绝」的最贴官方拟合；与 adapter 内部一致性成立（DENIED 是本 adapter 全部拒绝类的信封码，errors.go:26-29 的既有注释同源）。
- **TOOMANYREQUESTS 弃用论证成立**：distribution 规范把 TOOMANYREQUESTS 绑定 429 限流，配 413 是 status/code 错配，弃用正确。
- **不造自造码**正确：docs/reverse/docker-registry.md §1 行 3 确证 Artifactory 自由发散（SIZE_INVALID/NO_TAGS_FOUND），但那是「无官方码可用」时的发散，不是先例；有官方码可用时不扩面是对的。SIZE_INVALID（Content-Length 不符）语义更远，不考虑正确。
- 405→UNSUPPORTED + Allow 透传正确；verbatim 契约（status+message 逐字）不受 code 映射影响，测试断言到 message 级。

### 2. verbatim-first 臂位置与类型断言形态 — **通过**

- 逐一枚举了两个挂点能到达的 `*repo.StatusError` 全集（repo 包全部构造点）：pattern 409（governance.go:152/162）、quota 413（governance.go:294）、remote/virtual 写 405（service.go:172、virtual.go:55/77）。**鉴权类拒绝（ErrUnauthorized/ErrForbidden）在 repo 层均为 plain wrapped sentinel，从不以 StatusError 形态出现**——`errors.As` 臂不可能截走 challenge/403 臂，dev 的前置安全论证经代码核实成立。
- 真正 500 类错误（store 故障、putNode 失败等）均非 StatusError，不会被臂截走；`writeBlobCreated` 的 busy 臂在前（busy 非 StatusError，顺序无冲突）。
- 臂序与四 adapter 先例完全一致（generic handler.go:389、maven handler.go:302、npm errors.go:73、pypi handler.go:214 均 verbatim-first）。顺带行为改进：/v2 面 virtual/remote 写的 405 从旧 default 500 变为 405+Allow 逐字（无既有测试钉旧 500，无回归）。

### 3. tryMount 降级 202 — **论证半成立，AC 冲突未收口 → B1（blocking）**

正确性一半成立、AC 一半不成立，详见 B1。

### 4. Header 透传 — **通过**

`writeVerbatimStatusError` 先 `Add` StatusError 自带 Header、再由 `writeSpecError` `Set` Content-Type 与 API-version——若 StatusError 自带这两者会被规范化覆盖（正确的优先级），Allow 等其它头原样随行；注入层测试断言了 405 携带 Allow。

### 5. 测试证明力 — **通过**

注入层（信封形状/code 映射/Allow 携带）+ 真栈层（真 repo.Service + 治理配置）双层结构成立；413/409/405/未映射四路齐（未映射在 TestSpecCodeOfVerbatim 表级覆盖 500/502/507，可接受）；W26c 断言形态强：零残留覆盖 tags/list（v1 only）、refused manifest 按 tag **和** digest 双 404、refused blob GET 404，pattern 面还断到 NAME_UNKNOWN（从未落 manifest）这一层；`-race` 下全绿（本次复跑 6.5s ok）。

## 必须修改（blocking）

- **B1** `internal/adapter/docker/uploads.go:543-561`（tryMount 的 PutFromBlob 失败支路）— 治理拒绝（quota 413 / pattern 409）在 mount 面保持 202 降级，与 P0 AC 的字面断言直接冲突，且该面零测试钉死。
  - 证据链：PutFromBlob 在配额门处返回 `*StatusError(413)`（service.go:542-547，任何元数据写之前，故降级无残留——正确性半边成立）；tryMount 里 `isDenied(err)==false` → WARN → 202。而 PRD FR-31-AC5（milestone-4.md:310，P0）写「**docker mount 到 quota 仓超限 → 413**」、NFR-S23（:668）写「docker mount…全部过配额检查（**负断言全 413**）」。W26 变体的 QA 断言若按字面 curl mount POST 期 413，将拿到 202。「finalize 面诚实拒绝」的辩护覆盖了零残留与端到端最终 413，但覆盖不了 mount 调用自身的 wire 断言。
  - 同函数内部不对称：目的仓的**权限型**拒绝（403）以「real denial」为由立即渲染、不降级；治理拒绝对同一目的仓同样是**终态**拒绝（回退上传在 registration 处必然吃到同一个 413/409，除非并发释放配额的边缘态），降级只会让客户端把整层 blob 白传一遍再得到同一判决——大层在慢链路上是用户可见的浪费。
  - **建议改法（方案一，推荐）**：在 tryMount 的 `isDenied` 臂之前加 `errors.As(err, &se)` 臂，`writeVerbatimStatusError(w, se)` + WARN + `return true`（已写响应）。与同函数 403 臂的「real denial」逻辑对齐，mount 面即出 413/409 verbatim，AC 字面即过；现有三个降级测试（missing source / unreadable source / malformed digest）均为机械性失败、不受影响（复核过 blob_test.go:1107-1140、review_fixes_test.go:548/565，无一钉治理拒绝降级）；补一条真栈 mount-under-quota 用例（newGovernanceHarness 已具备全部建材）。
  - **方案二（若 conductor/PM 裁决 mount 面由 finalize 面代理）**：走 PRD 勘误（FR-31-AC5/NFR-S23 注明 mount 的 413 在 finalize 面断言），同时**必须**补一条钉死「治理拒绝 mount → 202 → 回退上传 registration → 413」全链的测试——该面目前无任何测试。两条路必须走一条，不能维持「注释写明 + 无测试 + 与 AC 字面相抵」的现状。

## 建议改进（non-blocking）

- `internal/adapter/docker/errors.go:142-143` — `specCodeOfVerbatim` 的 401→UNAUTHORIZED 臂是死路防御：当前 repo 层无任何 StatusError 构造带 401/403，但若未来出现，verbatim 臂会渲染一个**无 WWW-Authenticate challenge 的 401**，docker 客户端 token 流程会直接失败。建议要么在该臂改走 `h.challenge`（需要把函数变 method），要么在注释里写明「StatusError 永不携带鉴权态」的服务层约束（目前由构造点约定保证，无编译期强制）。
- `governance_render_test.go` — 「未映射→UNKNOWN」仅在映射函数表级覆盖；如想闭环，可在注入层加一个 502 StatusError 经 writeVerbatimStatusError 的端到端子例。锦上添花，非必需。
- `token_test.go` UpdateProfile 桩 — 编译修复属实（当前树 build 通过，签名与 metadata 工作树一致），dev 已自曝与并行 profile 票的撞车风险；维持其遗留登记即可。
- 读面 FetchError 型 StatusError（writeBlobReadError/writeManifestReadError 的 default 500）— 非「治理只写面」范畴，dev 已登记遗留①建议随 remote-docker 代理票收口，同意该归置。

## 取证记录（本次评审实跑）

- `go build ./internal/adapter/docker/` / `go vet` / `gofmt -l` → 通过 / 干净 / 空
- `go test -count=1 ./internal/adapter/docker/` → ok 30.3s（全量）
- `go test -race -count=1 -run 'TestVerbatim|TestGovernance|TestSpecCodeOfVerbatim' -v` → 全 PASS（7 用例含子例）
- clean-room 抽查：verbatim 臂、映射函数、测试均为原创形态（项目注释风格、自拟断言），决策引官方规范与 docs/reverse 行为规格而非反编译代码；无逐行对应嫌疑。

## 范围外发现（交 conductor）

- 无新增。T-95 遗留②（docker manifest 双 node 计数）在 T-111 测试中被显式当作 base 计算的一部分（governance_render_test.go:278-281 注释指明），处理方式诚实，但该计口径本身的收口仍在 T-95 遗留登记里。
