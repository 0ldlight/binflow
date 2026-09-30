# R11 双审报告 A（correctness）— 载荷七提交 885b22e5..e16034bb

- 形态：reviewer-a（correctness：并发/失败处理/正确性）
- 评审对象：a37e559b (BIN-64/T-582, docs) / a2b4b359 (BIN-67/T-585) / a3f053df (BIN-65/T-583) / b53c576a (BIN-68/T-586, docs) / 26f7f781 (BIN-63/T-581) / 46ffbe70 (docs 勘误) / e16034bb (BIN-66/T-584)
- 双审并行确认：reviewer-b 实例已在跑（reports/agents/R11-pr-review-b.md 未跟踪在案），本报告未读对方产出，结论独立。
- 取证自跑：build/vet/gofmt/golangci-lint 全绿；repo/generic/maven/mimetable/nuget/httpapi 六包 `go test -count=1` 全 ok；新测试族 `-race -count=2` 全 PASS；mime 表字节恒等独立复现（5×SHA-256 同值 + diff 空）。

---

## 评审报告 R11 载荷（形态: reviewer-a）

**结论: APPROVE** — 0 blocking / 6 NB / 7 范围外

工作目录 `/Users/lzw/dev-center/.claude/worktrees/clever-grothendieck-a3fa4f`（tip=e16034bb，下述路径省略此前缀）。

### 逐项结论（派发 7 项）

1. **T-583 virtual plane 写路由 — 通过**。`internal/adapter/generic/handler.go:209-232` clientChecksumPutPlane：LOCAL 原样；VIRTUAL 经 virtualDeploymentTarget 解析部署目标，非 local 目标（漂移）或无路由 → ok=false 回落原链——remote 面 RE-05 读-only 拒绝与未路由 virtual 405 面零扰动（`TestChecksumPutVirtualNoRouteKeeps405` 钉 405 + "No local repository was configured…" 逐字），R9 写动词红线（写动词永不触发 remote pull-through 探测）保持：class.Get 只查仓行，不触 svc.Get。putClientChecksum 全动作（svc.Get 存在探、SetClientChecksums、404 文案 repo 段、201 Location）面向成员 key，与 L040 arm1b A 实证（404 repo 段=成员 key `difftest-l040-maven`、Location→成员源）逐字同形；409 write-through 落成员节点 client 列由测试直查 `Nodes().Get("generic-local")` 钉住。serveVirtualClientChecksum：未设值/无源 → return false 落原链，C2 按需姿态保持（未设 GET 落原链 404 Failed-to-find，测试钉）；存值回显经 virtual 读平面解析成员节点 client 列（测试实证聚合面携带 overlay 列）。writeCreated node.RepoKey 渲染：LOCAL 两 key 恒等零变化（guard `node != nil && node.RepoKey != ""`），virtual 寻址渲染成员 key 与 L040 arm1b 种子腿 A 面（envelope repo=成员 key）一致；Put/PutFromBlob 两调用点同收。
2. **T-583 大小写折叠 — 通过**。`handler.go:188-197` checksumPutSource：末段 `strings.ToLower` 后 HasSuffix 三键，源引用 `relPath[:len(relPath)-len(sfx)]` 保客户端原拼写（含大写源名，测试 "LONE.TXT.SHA1"→"LONE.TXT" 钉）；排除面在任意大小写下仍排除——`.SHA512/.ASC/.SHA1.BAK/.Md5.old` 全部 201 普通部署 + GET 回字节（`TestChecksumPutSuffixCaseExclusionsStillExcluded`），`.sha1.bak` 类复合尾不误伤（lower 末段不含三键终缀）。零节点落地断言（List=0）防误建 sidecar。
3. **T-585 枚举门 — 通过**。`internal/repo/config.go:753-759,815-844`：闭集 {client-checksums, server-generated-checksums}，缺席/空串/null 合法（不设策略）；probe 字段 `*string`——非字符串 JSON 值走 decode 失败 400（家族姿势，T-585 Risks④ 如实登记 A 面未探）。单门在 validateLocalConfig，service.go CreateRepo/UpdateRepo 双面共用路径天然覆盖（byHash 同款先例）；keep-current 更新不复验存量 blob（手改坏值行上 description-only 更新成功，测试钉）。StatusError 链逐环验证：`Error()`=Message 逐字（api.go:789）、`Unwrap()`→ErrInvalidRepoConfig（api.go:793）、httpapi `writeRepoSvcError` `errors.Is(ErrInvalidRepoConfig)`→400 `err.Error()`（repositories.go:884）——wire 上无 BinFlow 组装前缀。**negative test 硬门达成**：repo 侧 9 例 table（含 none/generate-if-absent/Client-Checksums/bogus 三态断言：逐字文案+errors.Is+StatusError.Code）+ 不落仓断言（同 key 复建 200）；httpapi 侧 errors[] envelope 逐字 + 改仓面同文案。live 19 腿双轮 maven+generic 两包型 A=B（报告在案）。
4. **T-584 SET 无条件化 — 通过**。maven `put.go:456`：SET 门收窄为 `origLocal && TargetKind==KindArtifact`（去 client-policy 条件）——注册与校验解耦，srvgen 下错值 .md5 201 + ClientMd5=错值 + `repo.OriginalChecksums` md5=错值（测试三重钉，含 000…0 案形）；client 策略 409 对照腿逐字复跑（`refuse := mismatch && client && 非 metadata` 未动，put.go:439）。generic `handler.go:365`：比对臂包进 `!checksumPolicySrvgen`，SET 仍无条件先行——T-583 三臂路由序零扰动；srvgen GET 面服务计算值（registered-wrong md5 与 never-registered sha1 同形），ledger 缺口降级两态 404 不造假值。checksumPolicySrvgen 容错姿态（absent/unparseable/unknown→client 默认）与 ParseRepoConfig/mimetable 家族一致，方向 fail-closed（从严）。miss 臂族 404 统一后旧文案零残留（putSidecar `.sha512` 死分支已删；"Could not locate artifact" 仅存于 DELETE/miss 面 handler.go:481、generic handler.go:824 既有族，非 sidecar 面）。
5. **T-584 .sha512 退出 sidecar 族 — 通过**。layout.go checksumSuffixes={sha256,sha1,md5} 后：GAV 拼写自然解析为 KindArtifact（layout_test 钉五段 Layout，ext=sha512）→ putFile 普通链（pom 门天然跳过、srvgen declared-drop 照常、计算器照常）；非 GAV 拼写经 ServeHTTP 前置 terminalSha512File（小写精确，A 面未探不猜——Risks① 如实登记）四动词全通（201 Location 自指/GET 回字节/DELETE 204/删后 404，测试全动词环）。既有回归：maven 包全套 ok（含 t5/t6 walk 腿、direct overlay、TestSidecarStates 改钉 .sha512=合法实体节点）；TestSha512PutNonLocalPlaneRefusal 钉 remote/virtual 405 不回归（类拒绝经 svc 面不绕过）。注意两个 NB：非 GAV 臂 srvgen 不 drop declared（NB-1）、非 GAV DELETE 空 GAV 触发器（NB-2）——均不构成错误行为，见 NB。
6. **T-581 mime 收敛 — 通过**。字节恒等**独立复核**（非采信报告）：四份 pre-hoist（885b22e5 的 generic/maven/nuget/httpapi）+ hoisted mimetable 五个 map 字面量空白归一后 SHA-256 全=`e830737a49b0eb29…01ad01d227`，generic pre-hoist vs hoisted diff 为空——与 T-581 报告声称的 ff08527d…（未归一原文 hash）口径不同但结论互证。四 re-export 逐文件读过：一行委托 + 面注释，零逻辑残留（httpapi OCI carve-out/ociMediaTreePrefixes 原样、maven .sha512 L032 注记保留）；全仓 grep `extensionMimes` 仅存 mimetable 包内（编译级防第五副本成立）；snapshot golden 69 条 reflect.DeepEqual + 规则腿 15 例为单源守卫。消费方存量 mime 测试零改动全存活（六包全绿含 nuget/httpapi）。
7. **横切 — 通过**。错误链：新路径全部走 writeError/writeServiceError 族（StatusError 逐字渲染优先），无裸 err 直出新面；ctx 显式传递全程（一处 NB-5 修辞不一致）；并发：零新增共享可变状态（三个包级 map 皆只读，-race 全绿）、recalcAsync wg 模式未动；失败路径无假成功（seam==nil 显式 500、SET 失败 honest 500、读探失败 return false 落原链由 svc 拒绝）。go build/gofmt/vet/golangci-lint 四门 0。

### 取证命令（均实际执行）

- `go build ./...` → exit 0；`gofmt -l internal/` → 空；`go vet ./internal/repo/... ./internal/adapter/{generic,maven,mimetable,nuget}/... ./internal/httpapi/...` → exit 0；`golangci-lint run`（同六包）→ `0 issues.`
- `go test -count=1 ./internal/repo/ ./internal/adapter/generic/ ./internal/adapter/maven/ ./internal/adapter/mimetable/` → 4×ok（103.1s/17.3s/40.9s/1.2s）
- `go test -count=1 ./internal/adapter/nuget/ ./internal/httpapi/` → 2×ok（19.9s/226.1s）
- `go test -race -count=2 ./internal/adapter/mimetable/ -run 'TestTableSnapshot|TestByPathTableRule'` → ok 1.7s
- `go test -race -count=2 ./internal/repo/ -run 'TestChecksumPolicyTypeEnum'` → ok 19.9s
- `go test -race -count=2 ./internal/adapter/generic/ -run 'TestChecksumPutSuffixCase|TestChecksumPutVirtual|TestChecksumGetUnset|TestChecksumPutSrvgen|TestChecksumPut'` → ok 20.0s
- `go test -race -count=2 ./internal/adapter/maven/ -run 'TestChecksumPutSrvgen|TestSha512PutNonLocal|TestChecksumPutSourceMissing|TestChecksumPutRoutingBeforeLayout|TestSidecarStates|TestParseLayoutMatrix'` → ok 11.8s
- `go test -count=1 ./internal/httpapi/ -run 'TestChecksumPolicyRest'` → ok 0.97s
- mime 恒等复核：`git show 885b22e5:<四mime.go> | sed -n '/^var extensionMimes = map…/,/^}/p' | sed 's/\s//g' | shasum -a 256` ×4 + hoisted 同法 → 五 hash 同值 `e830737a…d227`；`diff /tmp/pre-generic.map /tmp/hoisted.map` → 空
- 活体双轮差分（T-583 27 腿、T-584 35 门×2、T-585 19 腿×2）：评审环境无 A 实例凭据不可复跑，报告 raw 锚（/tmp/t58*、/tmp/l039）在案；本地可复跑面（四门+全量测试+新族 race）与各报告 Commands 一致，证据采信（R10 同款姿势）。

### 必须修改（blocking）

无。

### 建议改进（non-blocking）

1. `internal/adapter/maven/put.go:515-532`（putSha512ChecksumFile）— **srvgen declared-drop 缺席**：putFile 在 server-generated-checksums 下丢弃客户端声明头（put.go:301-303 `declared = storage.BlobRef{}`，repo-semantics §5），本臂原样传入 declared → 同一 .sha512 文件面在 GAV/非 GAV 两拼写下的 declared 处理不一致（GAV 臂 drop、非 GAV 臂注册进 client 列并进 envelope checksums）。A 面该角落（非 GAV .sha512 + 声明头 + srvgen）未探（T-584 报告未覆盖此腿）。建议：解析 row/ParseRepoConfig 镜像 putFile 的 drop（一行 + 一次 class.Get），或先补 A 探针再裁——勿留两臂内部分歧。
2. `internal/adapter/maven/handler.go:97-99` + `calc.go:264-271` — **非 GAV .sha512 DELETE 空 GAV 触发器**：合成 `Layout{Kind:KindArtifact, File:file}` 的 OrgPath/Module/VersionDir 全空 → afterDelete 触发两次 recalcModule(dirPath="")：全仓 ListByPrefix（prefix=""，逐路径 TrimPrefix(prefix+"/") 恒 no-op、深度路径全被跳过 → versions 恒 ∅）→ removeMetadata 打在不可达的 `/maven-metadata.xml` 路径（maven 面无法创建该节点，ErrNodeNotFound 容忍 or 路径校验拒绝→recalcSync 记 error log）。无数据风险（async、wg 有界、删除不可达路径），但是每次此类 DELETE 一次全仓扫描 + 可能的 error-log 噪声。建议：afterDelete 对空 OrgPath&&Module 直接收敛（或合成 Layout 时跳过 calc）。
3. `internal/adapter/maven/handler.go:246,298` / `walk.go:246` / `virtual_metadata.go:100` — KindSidecar 不再产出 Algo=sha512 后，四处 `algo == "sha512"` 守卫成死码（防御纵深可留），但 writeSidecarDigest 的注释仍以现在时描述「EVERY sidecar path 404s it (R7 blocking fix)」——该面已由 layout 前置路由收编，注释口径应刷新，防后来者按旧地图找路。
4. `internal/adapter/generic/handler.go:240-255`（virtualDeploymentTarget）— 与 `internal/adapter/maven/put.go:163-178`（routeTargetOf）逐字重复（同 alias 三元组 + first-wins）。「adapter 包不共享非导出代码」约定成立，但 mimetable 票刚证明了 internal/adapter 下共享包可行；npm/cargo/conan 各自还有第三、四份同型探针。建议 conductor 排一次小收敛票（挂 adapter 公共层，同 T-581 形态），非本票义务。
5. `internal/adapter/generic/handler.go:143` — `clientChecksumPutPlane(r.Context(), repoKey)` 用 r.Context() 而 handlePut 全程持 ctx 形参（本函数内两者同源，纯修辞不一致；顺带 putClientChecksum 又收 ctx）。
6. T-585 证据缺口注记 — live 19 腿未含**大小写变体**枚举值腿（"Client-Checksums" 仅 B 侧单测钉拒收；A 侧 Java enum 惯例大小写敏感，低风险）；建议差分腿集增补该腿入 L 系常驻（T-585 Next③ 已提 create-none/get-unset，可一并）。

### 范围外发现（交 conductor，不塞本票）

1. **L040 N1 BUG 未随本载荷修复**：maven 未路由 virtual 面 sidecar PUT 绕 405 渲染 201/404/409 且零落地（putSidecar 的 svc.Get 读探先于部署路由拒绝；T-583 只修了 generic 面，maven 拦截臂未过路由门）。T-586 Next① 已建议「N1 与 C1 同票收」——请确认后继票落地，本载荷不含该修复不构成回归（pre-existing，探针新坐实）。
2. **L040 arm1b maven 已路由 virtual 半面**：404 repo 段指虚拟 key（A=成员 key）、409 后无写穿注册（origLocal 门未在虚拟寻址放行）——C1 的 maven 平面实例，open，随 N1 后继票。
3. **L040 N3/C2 模型修订**：virtual GET 按需计算仅 sha256（A 面），B 恒 404——UNKNOWN 待裁，裁定输入已齐。
4. **L040 N2/N5**：remote 拒绝族分面（generic PUT 405-vs-404 可复用 deployNoLocalRepoMessage）与 generic sidecar 守卫 1024B 含边界 BUG——待立案。
5. **T-584 Next②**：httpapi fileInfoOf originalChecksums 键集（A=client∪{sha256} vs B=client∪全 triple）——建议新票，先裁 A 形是否规范态。
6. **T-585 Risks②**：A 对无 Content-Type 的建仓 PUT 答 415、B 接受——repo config 面 CT 严格性差异，转 compatibility-engineer 评估立案。
7. **工作区卫生**：`.playwright-mcp/`（未跟踪目录）仍在 worktree——收编 R11 PR 时勿 `git add -A` 带入（T-584/T-581 报告均注记为他 agent 产物）。

### 对账

- **报告 vs 代码**：T-581 字节恒等声称独立复现成立（见逐项 6）；T-583/T-584/T-585 Commands 声称的本地可复跑面（四门、包全量、新族 -race -count=2）全部复跑一致，无虚假证据；live 双轮凭据面按 R10 姿势采信（raw 锚在案）。
- **46ffbe70 勘误 vs 本载荷**：勘误判定 R10 五提交缺席 develop（PR #185 第二父=docs-only）→ 载荷随 R11 矫正 PR 携带。本载荷树（885b22e5..e16034bb）含 R10+R11 全量，合并即完成对账处方；L040 白名单 B2 条件性 collapse 结论随合并生效。
- **ADR-0053/Errata（T-582, docs-only）**：六点通则与 as-built（api.go 六段+编译钉+resolver 惯用法）核对一致，纯架构票无行为面；architecture.md §5.4 第三层段与 ADR 同步在案。
- **AC/清单覆盖**：派发 7 项逐项过（见逐项结论），正确性缺陷零、关键测试覆盖（枚举 negative test 硬门、大小写排除负测、virtual 全腿、srvgen 对钉、mime golden）齐备。

**状态: done**
