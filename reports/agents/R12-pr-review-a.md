# R12 双审报告 A（correctness）— 载荷九提交 5729c092^..262d3a12

- 形态：reviewer-a（correctness：并发/失败处理/边界正确性/测试覆盖/证据链抽查/越界检查）
- 评审对象：5729c092 (BIN-70/T-588) / 92c588b9 (BIN-74/T-592, docs) / 2a75624b (BIN-73/T-591, docs) / abc922fc (BIN-69/T-587) / 2a4c28c7 + 3ea65e1e (BIN-72/T-590) / d14f34aa + ea552b76 (BIN-71/T-589) / 262d3a12 (R12 台账批, docs)
- 双审并行确认：reviewer-b 实例并行在跑，本报告未读对方产出，结论独立。
- 取证自跑：build/vet/gofmt 全绿；maven/generic/repo/httpapi 四核心包全量 + 八 adapter 消费包全量 `go test -count=1` 全 ok；新测试族与 adapter 基座 `-race` PASS；台账算术与 T-591 树恒等声明脚本复算通过。活体探针未复跑（A 实例凭据 env-only，派发令明示不碰活体）——报告 raw 锚与命令输出内部自洽，本地可复算面全部采信并抽验。

---

## 评审报告 R12 载荷（形态: reviewer-a）

**结论: APPROVE** — 0 blocking / 6 NB / 3 范围外

工作目录 `/Users/lzw/dev-center/.claude/worktrees/clever-grothendieck-a3fa4f`（tip=262d3a12，下述路径省略此前缀）。

### 逐项结论（四代码提交 + 三文档面）

1. **T-588/BIN-70 generic 1024B 守卫 — 通过**。`internal/adapter/generic/handler.go:313-344`：头部臂 `r.ContentLength > 1024` 先于存在性探测拒 409（miss-guard 角与 A 同序，T-588 活体新钉）；读臂 `io.LimitReader(1025)` 有界读消灭无上限 `io.ReadAll`（内存面净改善）；边界含端点正确：1024 放行入比对（409 Checksum error + 写穿保留）、1025 头部/读臂双拒、`hiddenReader` 钉 chunked（ContentLength=-1）走读臂。逐字文案 `Suspicious checksum file, content length of %d bytes is bigger than allowed.` 与 maven 面（put.go:672-675）及 L040 N5 实证同串。零副作用断言扎实：拒腿族后 GET 仍回显 1024 腿写穿值（非任何 a×1025+ 体）；平面限定腿钉 64KB 普通部署 201 不受扰。失败路径：读错 400 带 wrap 上下文、seam==nil 诚实 500、SET 失败走 writeServiceError——无假成功。拒后不排空 body 由 Go http server 收割/关流，与 A 1MB 断连同形（活体腿实证）。
2. **T-587/BIN-69 maven 拦截臂过部署路由门 — 通过**。`internal/adapter/maven/put.go:117-135`：gate 置于 facts 解析之后、putSidecar 读探针之前——`row.Type != TypeLocal` 即 405+`Allow: GET`（与 repo 服务侧 refuseVirtualWrite 的 `Allow: GET` 头值逐字节核对一致，virtual.go:70）；unrouted virtual 渲染 C5 逐字（unroutedVirtualWriteMessage 单源提取，与 writeServiceError 兜底臂同拼写）、remote/漂移路由渲染 RE-05（row.RepoKey=成员 key，语义正确）。路由穿透 factsKey 化四处全核对：miss 404 repo 段（put.go:453）、svc.Get 探针（put.go:440）、SET 注册（put.go:498）、201 Location（put.go:534）均寻址 factsKey；快照伴随名调整（put.go:417）同用 factsKey。SET 门 `origLocal &&` 删除正确（gate 后恒真）。handleGet overlay 扩 virtual（handler.go:273-276）经 svc.Get virtual 读平面解析成员节点回显注册值，unset 保计算兜底——测试直查成员 `Nodes().Get("maven-local")` 钉 ClientMd5 写穿 + virtual GET 回显。sha512 分支删除的不可达论证独立复核：layout.go:126 唯一 Algo 生产者，stripChecksumSuffix 仅迭代 checksumSuffixes={.sha256,.sha1,.md5}（layout.go:35,269-274），writeSidecarDigest 两 caller（walk.go:221 `l.Algo`、serveSidecarOfPath）algo 皆溯源自 Parse——论证成立；可观测契约由 TestPlainSnapshotWalkSha512SidecarGate 续钉（404 非 500，双平面）。次序模型（remote 拒绝→部署路由门→sidecar 拦截→普通部署）在本提交后成立；X-Checksum-Deploy 分支对 sidecar 路径 400 前置拒绝（put.go:124-127），不触路由面，范围外姿态维持（见 NB-5）。
3. **T-590/BIN-72 virtualDeploymentTarget 单源收敛 — 通过**。`internal/adapter/deploytarget.go`：三别名固定序、first-non-empty、unmarshal 失败=""、verbatim 不 trim——与 repo 包 unexported seam（virtual.go:908-924）逐字同构复核。8 消费方全数薄化核对（cargo/conan/deb/generic/helm/maven/npm/nuget grep 实证全走 `adapter.VirtualDeploymentTarget`），helm 保留 Get 包装（失败 "" 语义不变），conan (string,bool) 签名包装保持；仓内 adapter 层无残留探针体（`DefaultDeploymentRepoRef` grep 仅剩 repo seam 本尊与范围外两处，见范围外发现）。21 例 golden 钉容错语义（别名×大小写×缺失×非法×verbatim），本地 PASS。hunk 分离独立复核：2a4c28c7 的 maven/put.go 与 generic/handler.go diff 仅含探针收敛 hunk、d14f34aa 仅含键集 hunk——T-590 Blockers 声称的共享树 hunk 级分离属实。
4. **T-589/BIN-71 originalChecksums 键集单源化 — 通过**。`internal/repo/api.go:895-908`：新语义（sha256 恒在=注册值否则 serverSha256；md5/sha1 仅注册出现、逐字回显含全零占位；空串=键缺席；nil node=(serverSha256,"","")）与台账 A 模型逐条对上，8 腿表测试钉死。调用面 6 处全迁移核对；sidecar GET 单成员读（maven put.go:581、generic handler.go:279 传空 serverSha256）语义不变。**「envelope 读 landed node 等价旧 declared 过滤」的底层不变式独立核实**：repo/service.go:1186-1221 部署链对 Client 三列整体替换（同内容重部署亦刷新、无声明即清空）——node 列==本次部署已验证声明集，重部署清注册测试腿（FileInfo 回 {sha256}）与此互证。nuget 坍缩输出恒等论证核实：既有 TestBarePutCreatedEnvelope 本就钉「sha256 always」raw 规则，零改动通过=行为不变证明成立。死 helper 4 个（declaredSetOf/declaredSet/uploadContext/originalChecksumsOf）grep 全仓零引用，真死。srvgen 臂 wire 变化（maven srvgen 201 envelope oc 从 {} 变 {sha256:计算值}）为台账裁定面（空集面关闭），活体 GATE 12/12×2 在案。
5. **T-591/BIN-73 L041 探针批（docs）— 证据链自洽**。可本地复算项抽验通过：`git diff --stat 30e96418 1b1e3bfb`=空（B 面=收敛树账实声明复现）；L041 报告六臂表与台账 5 新条目（remote-domain enum gate / ondemand matrix / artifact-get headers / srvgen declared-drop / metadata visibility facet）逐条锚对上；溯源门（detached pristine worktree 重跑 drift=0）处置规范。
6. **T-592/BIN-74 联合裁定稿（docs）— 自洽**。C2/C5/C7 三建议与台账 262d3a12 落笔逐条对应（C2 对齐+A 权威修订「本地面零触碰→两面同缝」、C5 三分面+次序模型固化、C7 四分面）；作用域互斥钉界（generic virtual=计算回显 vs maven virtual=404 反向模型，禁共享层）在稿与 ledger 双处一致；maven-npm-pypi.md §1.5 勘误上报链在案。
7. **262d3a12 R12 台账批 — 算术复算通过**。脚本重数（行锚 `- id: ` 分割，排除 673 行注释伪条目）：**132 条 = 74 resolved + 58 open；open BUG=12**（R11 9 + 新裁 BUG 6〔C2/C5/C7 三翻 BUG、xsha-echo 翻 BUG、ondemand-matrix/srvgen 两新 BUG〕− 3 翻面 resolved〔N1/N5/oc-key-model〕）——与提交信息「Z: 127→132=74+58, open BUG 9→12」逐数吻合。三条 resolved 的 evidence 锚均指向本载荷 agent 报告，无空引。
8. **横切 — 通过**。错误链：新路径全走 writeError/writeServiceError 族，无裸 err 新面；ctx 显式传递全程；零新增共享可变状态（-race PASS）；失败路径无假成功（守卫拒/路由拒/SRVGEN seam nil/SET 失败各自诚实渲染）。越界检查：四代码提交文件清单与票面 area 全对齐（T-590/T-589 票据级豁免在案），diff 中无 area 外顺手改动；BOARD/known-divergence 未被代码票触碰（翻面草稿留 reports，conductor 执行）。

### 取证命令（均实际执行）

- `go build ./...` → exit 0；`go vet ./internal/adapter/... ./internal/repo/... ./internal/httpapi/...` → exit 0；`gofmt -l internal/` → 空
- `go test -count=1 ./internal/adapter/maven/ ./internal/adapter/generic/ ./internal/repo/ ./internal/httpapi/` → 4×ok（39.5s / 18.0s / 106.9s / 218.0s）
- `go test -count=1 ./internal/adapter/nuget/ ./helm/ ./cargo/ ./conan/ ./deb/ ./npm/` → 6×ok（79.2s-108.0s，八消费方收敛全量回归）
- `go test -count=1 ./internal/adapter/ -run TestVirtualDeploymentTargetGolden` → ok（21 例 golden）
- `go test -count=1 ./internal/adapter/generic/ -run 'TestChecksumPutBodySize|TestUploadResponseShapes'`、`./internal/adapter/maven/ -run 'TestChecksumPutUnrouted|TestChecksumPutRouted|TestPlainSnapshotWalkSha512'` → 2×ok
- `go test -count=1 ./internal/repo/ -run 'TestOriginalChecksumsKeyset|TestSetClientChecksums'`、`./internal/httpapi/ -run 'TestFileInfoOriginalChecksumsKeyset|TestT92'` → 2×ok
- `go test -count=1 ./internal/adapter/nuget/ -run 'TestBarePutCreatedEnvelope|TestV3PushCreatedBody|TestBarePutLocationContextPrefix' -v` → 全 PASS（created_location 零改动通过=坍缩行为不变证明）
- `go test -race -count=1 ./internal/adapter/` + maven/generic 新测试族 -race → 全 ok
- 台账算术：python3 行锚分割重数（132/74+58/open BUG 12，含翻面链条 9+6−3=12 复算）
- 树恒等：`git diff --stat 30e96418 1b1e3bfb` → 空（T-591 声明复现）
- 活体双轮（T-587 32/32、T-588 11 腿×4、T-589 GATE 12/12×2、T-591 108 腿×4）：评审不碰活体（派发令+A 凭据 env-only），raw 锚（/tmp/t58*/l041）与报告 Commands/Outputs 内部自洽，本地可复算面全部抽验一致，证据采信（R10/R11 同款姿势）。

### 必须修改（blocking）

无。

### 建议改进（non-blocking）

1. `internal/adapter/generic/handler.go:341-343`（+ maven `put.go:408-410` 同型存量）— **chunked 超限腿文案 N 值截断**：读臂拒文报 `len(raw)`（上限 1025），chunked 体远大于 1025 时文案称「content length of 1025 bytes」与真实长度不符（头部臂报真实 N）。已钉腿（szb-1025 恰界）不受影响；大 chunked 角未对 A 探过。建议：后续 L 批补一腿大 chunked 探针定 A 形，或读臂文案维持 1025 并在注释记此截断口径。
2. `internal/adapter/generic/handler.go:291-300` vs `internal/adapter/maven/put.go:524-525,672-675` — **逐字文案+限值常量双包三副本**（maven/generic/测试各一）：wire 逐字串散在两协议包，漂移即分歧面。T-588 报告已注记「2 行拷贝不跨包 import」的 deliberate 选择，但 deploytarget.go 先例证明 internal/adapter 基座可承载共享件——建议 conductor 排小收敛票（maxSidecarBytes+suspiciousSidecarMessage 上提基座），非本票义务。
3. `internal/adapter/maven/handler.go:246` / `walk.go:246` / `virtual_metadata.go:100-105` — **sha512 死码清理不彻底**（R11 NB-3 四处，T-587 只删 writeSidecarDigest 一处）：剩两处 `!= "sha512"` 死条件 + virtual_metadata 死 404 分支；且 handler.go:243 注释「sha512 stays on serveSidecar's 404」双重过期（404 现出自传输面 miss，serveSidecar 已无 sha512 特判）。建议下次触达这些文件时一并清并刷注释。
4. `internal/adapter/maven/put.go:95-105 vs 117-135` — **policy 门先于 sidecar 路由门的未钉角**：raw-seeded unrouted virtual 带 `handleReleases:false` 时，release sidecar PUT 会先答 409（policy）而非 A 形 405。与 plain PUT 面同序（票面 parity 目标含序），A 面此角未探。建议后续探针补「否决策略 virtual + sidecar PUT」一腿定序。
5. `internal/adapter/maven/put.go:124-127`（putChecksumDeploy）— X-Checksum-Deploy 分支先于 sidecar 路由门 return：sidecar 路径答 400 artifact-only（不触路由），artifact 路径走 svc.PutFromBlob 服务级拒绝。与 A 的次序关系未钉（C5/C7 后继票面已固化次序模型，建议把 checksum-deploy 臂纳入模型表述）。
6. T-589 Risks② 自曝腿 — **checksum-deploy envelope md5 回显改为注册值逐字**（声明错值+秒传命中组合）：与 A 键集模型一致化但该腿未活体探。建议列入下轮 GATE 腿集（wrong-declared-md5 + checksum-deploy 命中）。

### 范围外发现（交 conductor，不塞本票）

1. **internal/build/promote.go:402-416 virtualDefaultDeployment** — 三别名全同型第 9 份锁步拷贝（T-590 收敛范围为 adapter 八包，此处在 internal/build，未收敛；可能是分层方向考量）。建议并入 T-590 Next① 的 seam 统一票一并裁。
2. **internal/httpapi/uploads.go:708-725 mpuVirtualDefault** — **二别名变体**（缺 deploymentRepository 第三别名）：仅带 `deploymentRepository` 的 raw-seeded virtual 在 adapter 写面路由、mpu 上传面不路由——存量语义分叉，非本区间引入。建议台账查重或立小票统一。
3. **工作区卫生**：`.playwright-mcp/`（未跟踪目录）仍在 worktree——组装 PR #188 时勿 `git add -A` 带入（R11 已注记，延续提醒）。
