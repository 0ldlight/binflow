# R11 载荷 PR 评审报告 — Reviewer B（architecture 形态）

- 范围：`git log 885b22e5..e16034bb`（7 提交：a37e559b T-582 / a2b4b359 T-585 / a3f053df T-583 / b53c576a T-586 / 26f7f781 T-581 / 46ffbe70 勘误 docs / e16034bb T-584）
- 形态：reviewer-b（架构分层/包边界/契约一致性/测试覆盖）；与 reviewer-a 并行独立出具，未互看
- 分支：claude/r6-payload @ e16034bb（只读取证，未做任何写改动）

## 结论：APPROVE（blocking 0 / non-blocking 6）

七提交全部通过架构形态核查：依赖方向零违例、ADR 追加式纪律保持、area 波次隔离成立、未裁定兼容面（N1/C2/C5/C7）零触碰、known-divergence.yaml 零触碰、新测试全部行为命名且负测覆盖到位。T-581 字节平价与 iteration-1549 勘误的 git 事实经本评审独立复验属实。

---

## 逐项核查（按派发清单七项）

### 1. 依赖方向（architecture.md §5.1 / §5.4）— 通过

- `internal/adapter/mimetable` 直接 import 仅 `path`+`strings`（`go list -f '{{join .Imports "\n"}}'` 实测）；`go list -deps` 传递闭包内 lzwzzy 包仅其自身——stdlib-only 分层守恒，与 godoc 声明一致，环不可能。
- 四消费方向：generic/maven/nuget（协议包 → adapter 公共子包，与既有「协议包 → internal/adapter 基座」同向）；httpapi → adapter/mimetable 与既有 httpapi → internal/adapter 非测试导入（archive.go:41 / server.go:12 / router.go:26）同向。**无新反向边**。
- 位置合规：共享包落 adapter 公共层而非 repo SPI 面——T-582 Next ② 预告的「留在 adapter 公共层则不涉 ADR-0053 通则」路径，T-581 按此落地。
- 消费方薄化后符号面零变化（mimeByPath/mimeForPath 保名），调用点零改动；maven `.sha512` L032 注记与 httpapi OCI carve-out（ociMediaTreePrefixes + mimeForNode）原样保留。

### 2. ADR-0053 符合度（a37e559b）— 通过

- 文档 ↔ as-built 一致性逐项实测：api.go 六段行号 865/956/990/1014/1051/1136 全中；Service 大接口 290→657、方法数 awk 实数 **30**（与 ADR 声明一致）；编译钉 `var _ ClientChecksumWriter = (*service)(nil)` 在 873。
- 三层结构（§5.4 新段）与 ADR-0053 决策 1/3/5/6 浓缩一致：判据、命名后缀（Plane/Writer/Gate/Reader）、resolver 单点+nil 显式 500、fake 适配指引齐备；Errata-1 措辞准确（决策 1 字面载体勘误，六点语义契约逐字不变，authority 归属清楚）。
- 追加式纪律：DECISIONS.md **+30/-0**、architecture.md **+2/-0**，零删除零改写既有正文。
- 新缝遵循度现场核：T-583/T-584 的 clientChecksumSeam（generic handler.go:282 / maven put.go:546）= 每 Handler 一处 resolver、nil 显式 500、无散落断言——ADR-0053 决策 5 的活样例。

### 3. area 纪律（agent-graph.yaml owns）— 通过（一处记录在案的票据级豁免）

逐提交文件归属对账（`git show --stat` 七连）：
- T-583（dev-registry-adapter/generic）仅 internal/adapter/generic；T-584（同 role，maven+generic 双域票面明示）maven+generic；T-585（dev-go-core）repo+httpapi（owns 行 66 覆盖）；T-582（architect）DECISIONS+architecture；T-586 仅 reports/；T-581 跨 adapter 三包+httpapi/mime.go——**票据级豁免在 T-581 报告 Role 行记录在案**（「票面明确豁免」条款），可接受。
- 波次隔离成立：T-583（a3f053df）先提交、T-584（e16034bb）后落，e16034bb 的 handler.go diff 以 T-583 landed 形态（plane 模型在位）为基——顺序提交佐证「T-583 已提交后才派 T-584」。T-581 与 T-583 并行但文件不相交（mime.go vs handler.go）；共享 worktree 瞬态断编在 T-585 Blockers 如实记录（自愈，未触碰他域文件）。

### 4. 兼容面边界 — 通过

- 未裁定项零触碰（逐面核码）：maven virtual sidecar 平面（L040 N1 BUG）未修——putSidecar 的 origLocal 门原样，L040 Arm 1b 如实记录仍分歧；C2 按需计算未动——serveVirtualClientChecksum 显式回落（「C2 family's open faces」注释在位）；C5 metadata 专用路由未动——metadata 目标 tolerance no-op 保持，L040 记录三端三形现状；C7/N2 remote 拒绝族未动（TestSha512PutNonLocalPlaneRefusal 钉 405 不回归）。
- known-divergence.yaml 本轮载荷零触碰（diff stat 无该文件）；六条翻面均为报告内「草稿」交 conductor——台账翻面归 conductor 的纪律维持。
- L040 新发现（N1/N5 BUG、N3/N4 修订、N6/N7 注记）全部以「分类建议待裁」形态登记，探针报告未越权修码。

### 5. T-584 策略语义架构 — 通过

- ADR-0052 seam 守恒：SetClientChecksums（repo 缝）仍不读不判定 checksumPolicyType；策略门留在 adapter 现位（maven ParseRepoConfig / generic checksumPolicySrvgen 均为 adapter 侧配置读）。SET 无条件化 = ADR-0052 决策 6.4 显式预登记的 probe 票兑现（「policy ∈ {none, server-generated} 下 sidecar 值错 A 行为 NOT_RUN 归 probe 票」），refuse 门保持策略域、SET-先于渲染次序（决策 3③）不动。
- srvgen GET 面：maven overlay 门收紧为 local+artifact+非 srvgen（handler.go）；generic srvgen 计算值回显带 ledger-gap 诚实 404 降级——两域同模型。
- `.sha512` 退出 checksumSuffixes 的 layout 影响面收口完整：KindSidecar 家族收窄至三键（与 generic terminalChecksumSuffixes 对齐）、Kind/Algo godoc 同步、layout_test 五段断言更新、GAV 拼写自然流入 putFile（pom 门天然跳过）、un-GAV-able 拼写走 putSha512ChecksumFile 专臂（合成 `Layout{Kind:KindArtifact, File:file}` 与 Parse 自身 File=basename 语义一致，artifact 读/删经 relPath 寻址不受影响——已核 File 全部消费点）。

### 6. 测试覆盖与命名 — 通过

- 命名纪律：七个新测试文件全部行为命名（checksum_put / checksum_suffix_case / checksum_virtual_plane / checksum_srvgen_policy / checksum_policy_enum / checksum_policy_rest / mimetable_test），票号仅进文件头注释——合规。
- 覆盖面：virtual plane 全腿+无路由 405 对照（T-583）；大小写折叠两族（入族+排除面 .SHA512/.ASC/.SHA1.BAK 任意大小写仍排除）；srvgen 注册+计算值 GET+client 策略对照解耦门两侧对钉（T-584）；枚举 negative 9 例 table（err verbatim+errors.Is+StatusError.Code 三断言）+零副作用断言+改仓面+REST echo 往返（T-585，security 分类硬门 negative test 达成）；mimetable snapshot golden 69 条+规则腿（T-581）。
- 活体差分证据链与本地测试互补：T-583 27 腿×2 / T-584 GATE 35/35×2 / T-585 19 腿×2 / T-586 111+2 腿双面×2——活体腿本评审无法复跑（需 A 实例与双实例 B），但 raw 资产路径、环境处置（teardown/residual=0/graceful stop）与 NOT_RUN 清单在报告内自洽；本地全量+race 已由本评审复跑（见取证）。
- T-584 Risks ⑤ 如实承认 generic srvgen FileInfo oc 键集未逐键比对并已在 Next ② 开票建议——诚实缺口登记，非静默。

### 7. docs 质量 — 通过

- iteration-1549 勘误（46ffbe70）git 事实**三项独立复验全属实**：① d420bbc7 第二父=d9bae6b4（`git log --format='%h %p' -1`）；② R10 载荷五提交 `merge-base --is-ancestor` 五连 NOT in develop；③ d420bbc7 树内 `git grep SetClientChecksums` 零命中（HEAD 五文件在案）。勘误措辞与 git 图一致，风险处置（载荷随 R11 修正 PR 补载）路径明确。
- L040：B1/B2 双面方法论正确（票面 SHA 与载荷账实不符→双跑钉形而非弃跑）；账实不符立此存照置顶+三连证据；A 侧 1MB 断连形态如实记录；NOT_RUN/BLOCKED 清单不粉饰。
- 树身份一致性：T-583 白名单复验（自家 landed 树）与 L040 B2=885b22e5 上 #9a 仅半 collapse 不矛盾——不同树、不同时点，报告各自标明。

---

## 取证命令（本评审实际执行）

```
go build ./...                                                  → 0
gofmt -l internal/                                              → 空
go vet ./internal/adapter/{mimetable,generic,maven,nuget}/ ./internal/httpapi/ ./internal/repo/   → 0
go test -count=1 ./internal/adapter/mimetable/ ./internal/httpapi/ ./internal/adapter/nuget/     → ok (230.0s httpapi)
go test -count=1 ./internal/adapter/generic/ ./internal/adapter/maven/ ./internal/repo/          → ok
go test -race -count=1 -run 'TestChecksumPutSrvgen|TestSha512PutNonLocal|TestChecksumPutVirtualPlane|TestChecksumPutSuffixCase|TestChecksumPolicyType|TestChecksumPolicyRest' ./internal/adapter/{maven,generic}/ ./internal/repo/ ./internal/httpapi/   → 4 包全 ok
go list -f '{{join .Imports "\n"}}' ./internal/adapter/mimetable/  → path, strings（仅 stdlib）
# T-581 字节平价独立复验：885b22e5 四份旧表 grep 提取 vs 新表
  → 四份各 69 条，与新 mimetable 表 sort 后 diff 四连 IDENTICAL
# ADR-0053 引用核：api.go 段行号 grep + Service 方法数 awk 计数 → 865/956/990/1014/1051/1136、30 方法全中
# 勘误核：d420bbc7 %p、五提交 is-ancestor、树内 grep → 三项属实
# 逐提交 stat 七连（票务归属对账）
```

---

## 建议改进（non-blocking，6 条）

1. **virtualDeploymentTarget 第 7 份锁步拷贝**（internal/adapter/generic/handler.go:240，与 cargo/maven/deb/nuget/conan/npm 六份既有 tolerant 三别名探针同构）：T-583 按「adapter 包不共享非导出代码」先例自认合理，但本仓上周刚为同型病灶付 T-581（mime 四副本收敛）；六+一份三字段 probe 是收敛候选（落 internal/adapter 基座，全部协议包已 import 该包，无新边）。建议 conductor 立收敛池票（同 T-581 由 R10 双审 handoff 触发的路径）。
2. **ADR-0052 决策 6.3 括注已被 T-584 活体证据超越**：「generic 无 policy 配置面，A 活体 409 已钉」——T-584 证得 generic 域在 server-generated-checksums 下与 maven 同模型（GATE 35/35），generic adapter 现消费 checksumPolicyType（checksumPolicySrvgen）。机制轴（缝不读 policy、门在 adapter）无恙，纯事实注记过时；按本仓 ADR 纪律（Errata-1 先例）建议 conductor 在台账翻面提交时补一行 Errata-2 或注记，保 ADR 证据链与 as-built 一致。
3. **writeSidecarDigest 的 `algo == "sha512"` 分支成不可达防御码**（internal/adapter/maven/handler.go:298 起）：checksumSuffixes 剔除 .sha512 后 Parse 不可能产出 Algo=sha512，两调用链（handler.go:283 / walk.go:162,221）均经 Parse。建议下次触 maven sidecar 票时清理或注释改标 defensive。
4. **ADR-0052 Errata-1 引用行号漂移**：put.go:516 / handler.go:190 写时点准确（已在 a37e559b 复验），e16034bb 后移至 546/282——按惯例 as-built 引用不改；如需可点性可在 Errata 加漂移注记（极轻，可不做）。
5. **T-584 Risks ⑤ generic srvgen FileInfo oc 键集未逐键比对**——其 Next ② 已建议 httpapi 域新票（originalChecksums 键集 A=client∪{sha256} 模型），维持跟进勿失联。
6. **remote/virtual 仓配置的 checksumPolicyType 枚举门未探**：T-585 门只骑 validateLocalConfig（byHash 同款先例，注释自认）；参照对 remote/virtual 面是否同拒未取证——「不猜」姿势正确，建议登记 probe 候选（一句行,归 compatibility-engineer）。

## 范围外发现（交 conductor）

- L040 两条 BUG 形修复票输入待立：N1（maven 未路由 virtual sidecar PUT 绕 405 假成功，C1 的 maven 平面扩展）、N5（generic sidecar 1024B 守卫缺失，限值与文案已实证钉死）。
- T-584 新发现漂移：FileInfo originalChecksums 键集（httpapi 域，五腿 live 证据 /tmp/t584-difftest/{a,b}-r2.json）。
- 台账翻面草稿六条（T-583×3 / T-584×2 / T-585×1）待 conductor 收口；known-divergence.yaml 本轮零触碰正确。
- **R11 PR 合并操作风险**：R10 载荷落地缺口根源=建 PR 时分支头未推（PR #185 事故在案）。R11 修正 PR 建前须推净分支+核 headRefOid=e16034bb，合并后复验 develop 树内含 SetClientChecksums（关闭 L040 立此存照的条件句）。

## Handoff

```
结论: APPROVE
形态: reviewer-b (architecture)
blocking: 0
non-blocking: 6（收敛池票建议 / ADR-0052 6.3 括注超越注记 / 死防御分支 / 行号漂移注记 / oc 键集跟进 / remote·virtual 枚举 probe 候选）
范围外: 4（N1+N5 修复票输入、oc 键集 httpapi 新票、台账翻面六条草稿、R11 PR 推净+headRefOid 复验操作项）
报告: reports/agents/R11-pr-review-b.md
状态: done
```
