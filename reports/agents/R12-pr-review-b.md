# R12 载荷 PR 评审报告 — Reviewer B（architecture 形态）

- 范围：`git log 5729c092^..262d3a12`（9 提交：5729c092 T-588 / 92c588b9 T-592 / 2a75624b T-591+L041 / abc922fc T-587 / 2a4c28c7+3ea65e1e T-590 / d14f34aa+ea552b76 T-589 / 262d3a12 台账批）
- 形态：reviewer-b（架构分层/包边界/单源收敛质量/裁定面一致性/探针方法学）；与 reviewer-a 并行独立出具，未互看
- 分支：claude/r6-payload @ 262d3a12（只读取证，除本报告外零写改动、零 git 写操作）

## 结论：APPROVE（blocking 0 / non-blocking 6）

九提交全部通过架构形态核查：依赖方向零违例（adapter 基座闭包仅 metadata+auth，八协议包零互依）、两台单源手术（T-590/T-589）行为等价性经本评审独立复验成立且守卫充分、ADR-0052 机制轴无损（策略门留 adapter、缝不读 policy）、T-592 建议稿→台账落账忠实度机械比对通过（C5/C7 逐字、C2 一处修正且修正依据核实）、台账 Z 对账独立复算全中、L041 溯源门（pristine detached worktree 重跑）方法学闭环。known-divergence.yaml 仅 conductor 台账批（262d3a12）触碰，纪律维持。

---

## 逐项核查（按派发清单五项）

### 1. 依赖方向与分层 — 通过

- **adapter 基座**：`go list -deps ./internal/adapter/` 传递闭包内 lzwzzy 包仅 `internal/metadata`+`internal/auth`——deploytarget.go 新增仅用 encoding/json，环不可能。
- **八协议包 → 基座**：cargo/conan/deb/generic/helm/maven/npm/nuget 各新增**一条** `internal/adapter` 导入边（包级既有边，T-590 Changes 自述「包级非新边」经查属实——八包此前均已 import 基座）；协议包互扫（11 包 × 排除基座/mimetable）**零协议间边**。
- **mimetable 侧边**（generic/maven/npm）为 T-581 既有先例，非本批引入。
- **repo 不懂 HTTP 面**：`repo.OriginalChecksums(node, serverSha256)` 纯函数签名（node+string 入、三元出），包零 HTTP 依赖；三 envelope 面（maven/generic/nuget）与 FileInfo 面全部经它渲染。
- **httpapi fileInfoOf 收口**：`repo.OriginalChecksums(node, sums.Sha256)`——httpapi→repo 与既有方向同向，无反向边。
- 单源落位合规：deploytarget 落 adapter 基座（非 repo SPI 面）——循 mimetable 先例的「共享代码留 adapter 层」，不触发 ADR-0053 判据（无新可选能力段、无新接口；导出单函数非接口污染）。

### 2. 单源收敛手术质量（T-590/T-589）— 通过

**T-590（virtualDeploymentTarget 8 拷贝收敛）**：
- **独立字节平价复验**：本评审从 abc922fc（pre-hoist 树）机械提取八份探针体（regex `var probe struct … return ""`）——**7 份逐字节一致**（sha256 前 12 位 8a9020c95ad5：cargo/conan/deb/generic/maven/npm/nuget），helm 份仅缩进 + `row.Config`（vs `config`）变量名差异、结构体字段与逻辑逐字同构——与 T-590 Changes 的诚实披露（「helm 仅 []byte(row.Config) 因自做 Get 内联，语义等价」）完全一致。generic 内第二处 probe 命中系 checksumPolicySrvgen（policy 探针，非部署路由探针），无混淆。
- **薄化保名**：八包本地名全保留（cargoDeploymentTarget/conanDeploymentTarget/debRouteTarget/virtualDeploymentTarget/routeTargetOf/virtualDeployTarget/virtualWriteTarget 包装），调用点零改动；deb/helm/maven/npm/nuget 清 encoding/json 死导入正确。
- **golden 守卫充分性**：deploytarget_test.go 21 例覆盖三别名×单独/优先序/空串跳过/键名大小写回退/未知键/非法 JSON/非对象/类型错/null/verbatim 不 trim/多余字段——容错契约全维度钉死；注释明言「消费方再长本地探针体=漂移即 FAIL」。
- 残留：repo 包自身 virtualRouteTarget seam 仍双轨（T-590 Next ① 已登记，导出化属 architect SPI 面，未越权处理正确）——NB-⑥ 跟进。

**T-589（originalChecksums 键集单源化）**：
- **使能器消灭验证**：`grep -rn "OriginalChecksums(" internal/` 全量——产品码 6 调用点 + 测试 2 处**全部** `(node, serverSha256)` 或 `(node, "")` 形态；旧 `(node, s256, s1, md5)` per-algo 兜底形态零残留。
- **nuget 坍缩等价独立核**：repo/service.go:1194（幂等重部署）与 :1220（新节点）证明共享 Put 引擎把**已验证** declared 集持久化进 node.Client 列 → 201 腿 node 列 == declared 集 → 新代码「按注册列渲染」与旧代码「按 declared 过滤」值恒等；nuget created_location_test 既有 A 键集表零改动通过（本评审复跑 nuget 包 ok）为行为不变佐证。
- **maven/generic envelope**：uploadContext/declaredSet/declaredSetOf/originalChecksumsOf 四 helper 净删；零声明腿渲染 {sha256:计算值}（generic_test 两新腿钉住）；srvgen 臂渲染 {sha256:计算值} 的残余=N4 裁定域（见 NB-④）。
- **签名收窄副作用核查**：checksum GET 单成员读（maven put.go:581 / generic handler.go:279）传空 server sha256——`out256` 变为空（旧代码返回传入 sha256），但两调用点均为 clientChecksumValueOf 投影、只消费 o1/o5 成员（switch algo 后 sha256 case 返回 o256=""）——**maven/generic 侧car GET 的 sha256 未设值本就不走该 helper**（走计算兜底链），无行为回退；repo 测试表第 8 腿（empty server sha256）明示此契约。

### 3. ADR 符合度 — 通过（一处文档级漂移记 NB-①）

- **ADR-0052 机制轴**：SetClientChecksums/OriginalChecksums 缝仍不读不判定 checksumPolicyType（repo 包零 policy 引用）；策略门留 adapter 现位（maven putSidecar 比对门 / maven handleGet srvgen overlay 门 / generic checksumPolicySrvgen）。守卫次序（>1024B → target-exists → policy 比对 → 注册）与决策 6.2 逐字一致，且 T-588 miss-guard-1025 活体腿证 A 亦守卫先行。
- **T-587 部署路由门次序**与 T-592 C5 面一钉死的次序模型（remote 拒绝→部署路由门→…→sidecar 拦截）一致：门在 svc.Get 读探针之前，写动词不再触发无路由 key 的读解析。
- **ADR-0053**：无新可选能力段；ClientChecksumWriter 编译钉（api.go:873）原样。
- **漂移**：ADR-0052 决策 4 边界子句「上传时 ItemCreated 的 declared-only 过滤语义维持 uploadContext 形——overlay 只治无上传上下文渲染面」+ 字面签名描述「node + 服务端 triple 入」已被 T-589（依 conductor BIN-66/BIN-71 裁定）超越——建议循 Errata 二先例补 Errata 三（NB-①）。

### 4. 裁定面一致性（92c588b9/2a75624b/262d3a12）— 通过（两处措辞级瑕疵记 NB-②④）

- **gate 行忠实度机械比对**（python 逐字）：C5、C7 两条 gate 行 T-592 草稿→台账**逐字采用**（verbatim=True）；C2 一处偏差=已知修正：「本地面零触碰」→「virtual+local 两面」+「sha1/md5 404 维持」，修正依据**核实成立**——L041 Arm 1 g1-get-sha256-unset（generic local 面 A=200 计算值，「延及 local 面」原文在案）+ virtual 面沿用 L040 c2-v-get-* 证据（L041 NOT_RUN 清单如实分工：本批只证 local，virtual 沿用前批）；台账 rationale 明文记录修正出处（「T-592 草稿『本地面零触碰』据此修正」）——非静默改写。
- **新条目草稿落账**：maven/metadata-fileinfo-visibility（T-592 面三草稿）→台账逐字段一致（surface/UNKNOWN/authority pending/review_gate/evidence，仅「Arm5」→「Arm 5」空格规整）。
- **Z 对账独立复算**（python yaml，本评审执行）：批前 127 条（resolved 71 / open BUG 9）→ 批后 **132 条 = resolved 74 + 未解决 58**、open BUG **12**——算术全链吻合：+5 新立（N1rest/ondemand-matrix/headers/N4/visibility）、+3 resolved（N1 写面、N5、oc）、UNKNOWN 45→44（-4 reclass BUG〔C2/C5/C7/xsha〕+3 新 UNKNOWN）、BUG 总数 71→77（+4 reclass +2 新立）。commit message 的「Z: 127→132=74+58，open BUG 9→12」**独立复算全中**。
- **防双计分界自洽**：oc-key-model（已 resolved，键集渲染模型）vs N4 srvgen-declared-header-drop（declared 值的注册处置+409 策略域角落）——两 entry 互写分界（N4「键集渲染已随 T-589 单源」/ oc entry「值面/注册面与本条键集面防双计分界」）；C2（generic 域双面 sha256）vs ondemand-matrix（maven 面+miss 指向，明写「generic 面半腿归 C2 票——防双计」）。分界清晰、领地互斥。
- **N1 翻面+读面剥离**：T-587 实测（sha256 unset 双端 200 三腿）修正原 surface 过宽读面子句→剥离入 ondemand-matrix——证据链（T-587 Outputs mvu/mv2/cl 腿）与台账修订文本一致。

### 5. L041 探针方法学 — 通过

- **溯源门闭环**：共享 worktree 有在途他票修改（本载荷 T-587 的 maven 文件）→ 另建 detached pristine worktree @30e96418 重构建 b-server-clean 双轮全量重跑——clean r1≡r2 drift=0 且原始 B vs clean B 108 腿归一后 drift=0（二进制差仅 vcs stamp）——**账实防污染手法正确且留痕**。
- **收敛树身份**：HEAD 30e96418 内容==develop 1b1e3bfb（diff --stat 空、9 提交全 merge 节点）——L040「B2 条件性 collapse」转正论证成立；白名单 #1-#9 九面全 collapse + 唯一残余（8a sha1 兜底键）正确归属 open BUG 条目而非新立。
- **归一口径**：ISO-TS/RFC-1123/UUID/epoch-ms/host:port + 请求级 x-request-id 丢弃——四轮 self-drift 0 为归一不当会暴露的间接证据。
- **residual/环境处置**：A 每轮 teardown+收尾独立计数 0、B SIGTERM graceful+lsof/pgrep 空、detached worktree remove 完毕、凭据零落盘（grep 命中逐处定位=字段名巧合+产品既有告警）——如实在案。
- **NOT_RUN 清单不粉饰**：真客户端腿缓期、generic HEAD 未设腿、charset-CT PUT 腿、goodenum 往返、C3/C6 未复验、C2 virtual 面沿用前批——六项明列。

---

## 取证命令（本评审实际执行）

```
go build ./...                                                                    → 0
gofmt -l internal/                                                                → 空
go vet ./internal/adapter/ …八协议包… ./internal/httpapi/ ./internal/repo/          → 0
go test -count=1 ./internal/adapter/ ./internal/repo/ ./internal/adapter/mimetable/ → ok（repo 70.5s）
go test -count=1 ./internal/adapter/{generic,nuget,cargo,conan,deb,helm,npm}/       → 全 ok（npm 126.7s 最长）
go test -count=1 ./internal/adapter/maven/                                         → ok 43.7s
go test -count=1 -run 'TestFileInfoOriginalChecksumsKeyset|TestSearchChecksumW15' ./internal/httpapi/ → ok
go test -race -count=1 -run 'TestVirtualDeploymentTargetGolden|TestOriginalChecksumsKeyset|TestChecksumPutBodySizeGuard|TestChecksumPutUnrouted|TestChecksumPutRouted' ./internal/adapter/ ./internal/repo/ ./internal/adapter/{generic,maven}/ → 4 包全 ok
go list -deps ./internal/adapter/ | grep lzwzzy                                    → metadata, auth 仅
go list -f Imports × 11 协议包（互边扫描）                                            → 零协议间边
grep -rn "OriginalChecksums(" internal/                                            → 8 调用点全 (node, serverSha256|"") 形态
python3 独立提取 abc922fc 八份探针体 + sha256 比对                                    → 7 份逐字节一致 + helm 缩进/变量名差
python3 yaml 台账复算（批前 262d3a12^ / 批后两态）                                    → 127→132=74+58、open BUG 9→12 全中
python3 T-592 gate 行草稿 vs 台账逐字比对                                            → C5/C7 verbatim、C2 一处已披露修正
grep -rn C5 拒绝文案 internal/                                                      → 4 处（repo/maven/cargo/pypi）
git show --stat ×4 docs 提交                                                        → docs-only 范围属实
```

活体差分腿（T-587 GATE 32/32×2、T-588 11 腿×4、T-589 GATE 12/12×2、L041 108 腿×4）本评审无法复跑（需 A 实例与双实例 B），但 raw 路径、residual 处置、NOT_RUN 清单在各报告内自洽，且本地回归全绿由本评审复跑覆盖。

---

## 建议改进（non-blocking，6 条）

1. **ADR-0052 决策 4 边界子句已被 T-589 超越，待 Errata 三**（docs/design 无涉，DECISIONS.md ADR-0052）：「上传时 ItemCreated 的 declared-only 过滤语义维持 uploadContext 形——overlay 只治无上传上下文渲染面」与字面签名「node + 服务端 triple 入」均与 as-built 漂移（uploadContext 已删、envelope 面并入键集单源、签名收窄为 (node, serverSha256)）。机制轴无恙、conductor BIN-66/BIN-71 裁定链在案——循 Errata 二先例，建议 conductor 下次台账批补 Errata 三，保 ADR 证据链与 as-built 一致。
2. **C2 gate 行「maven virtual GET=404」简写与同批新条目字面冲突**（known-divergence.yaml generic/checksum-get-virtual-ondemand review_gate）：同一提交落账的 maven/sidecar-get-ondemand-matrix 证 A maven 面 sha256=按需 200（mv2-get-sha256-unset A=B 200）、仅 sha1/md5 404——C2 行的简写按字面读与姊妹条目矛盾（T-592 原文本限定 .sha1，压缩成 gate 行时丢了限定）。指令语义（generic 票禁入 maven）两读皆安全，故不 blocking；建议下轮 docs 批修为「maven 面 sha1/md5 GET=404（sha256 按需归 ondemand-matrix）」。
3. **C5 拒绝文案 + 1024B 守卫常量的跨包锁步拷贝**：C5 文案现 4 处（repo/virtual.go:62 / maven/handler.go:531 / cargo/virtual.go:222 / pypi/upload.go:368）；maxSidecarBytes+suspiciousSidecarMessage 在 maven/generic 各一份（T-588 自述「包内 2 行拷贝，不跨协议子包 import」）。同型病灶刚由 T-581/T-590 两票证明代价——建议 conductor 立收敛池候选票（C5 文案可随 repo seam 导出一并解决，见 NB-⑥）。
4. **N4 条目 surface 的 B 侧描述系 T-589 前形态**（maven/srvgen-declared-header-drop）：「B=GAV 臂丢声明（oc=计算 triple）」——T-589 落地后该面渲染 oc={sha256:计算值}（本载荷自身改动）；review_gate 末句「键集渲染已随 T-589 单源」已纠偏，但 surface 字面是陈旧观测。R13 修复票实现者应以 gate 行模型为准；下轮台账批可顺手把 surface 的 B 形更新为当前树形态。
5. **T-589 Risk ② 未探角落**：checksum-deploy（秒传）envelope 的 md5 回显从 ledger 值改为注册值逐字（声明错 md5+秒传命中罕见组合），A 形未单独活体探——建议 R13 probe 批补该腿（与 ondemand-matrix 票的探腿合并跑，成本一腿）。
6. **repo virtualRouteTarget 双轨残留**（T-590 Next ① 已登记）：repo 包 unexported seam 与 adapter 单源仍是同一探针的两份实现——最后一副本。导出化/反向引用属 architect SPI 面（ADR-0053 判据），建议 conductor 移交 architect 定夺；落定后 deploytarget.go godoc 的「restates repo's own reader」注记同步收口。

## 范围外发现（交 conductor）

- **PR #188 合并操作项**：建 PR 前推净分支+核 headRefOid=262d3a12（#185 事故在案）；合并后复验 develop 树内含 VirtualDeploymentTarget 与 (node, serverSha256) 签名。
- **T-592 上报三项未执行**：PRD RE-05 域注记（→product-manager）、maven-npm-pypi.md §1.5 行 101 Erratum（→reverse-engineer）、B1/B2 账实对账——R13 四票票面须钉「含 R10 载荷之树」为基树。
- **maven virtual GET overlay × 成员 srvgen 策略组合未探**：T-587 overlay 扩 virtual 面时不查成员策略（mv2-get-md5 腿成员非 srvgen）——建议并入 R13 ondemand-matrix 票探腿（该票本须触 maven sidecar GET 矩阵）。
- **L041 两条 UNKNOWN 待 R13 联合票分诊**：rest/remote-domain-policy-enum-gate（goodenum 往返腿先行）、storage/artifact-get-response-headers（Content-Disposition 客户端可见性优先裁）。
- T-592 Next ④（PUT 面处理次序模型升 ADR）维持「随修复票规格固化、双票落地后再议」路径，勿失联。

## Handoff

```
结论: APPROVE
形态: reviewer-b (architecture)
blocking: 0
non-blocking: 6（ADR-0052 决策4 Errata 三 / C2 gate 行 maven 简写 / C5 文案+1024B 常量收敛池 / N4 surface 陈旧 B 形 / 秒传 md5 回显未探腿 / virtualRouteTarget 双轨移交 architect）
范围外: 5（PR#188 推净+headRefOid、T-592 上报三项未执行、virtual×srvgen overlay 探腿、L041 两 UNKNOWN 分诊、次序模型 ADR 后议）
报告: reports/agents/R12-pr-review-b.md
状态: done
```
