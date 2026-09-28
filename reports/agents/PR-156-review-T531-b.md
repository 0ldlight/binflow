# PR-152 Review — T-531（reviewer-b / architecture 形态）

```
Ticket:        T-531 [P0] Maven virtual 聚合的 Facet 消费（四桶序 cache 投影跳过 + 级别键控 handle* 跳过）
Role:          code-reviewer (reviewer-b)
Area:          internal/adapter/maven（聚合 metadata 遍历面）
Input:         派发输入（评审形态 reviewer-b + 六项重点）；通读：virtual_metadata.go 全文 + git diff（140+/14-）、virtual_metadata_cache_skip_test.go 全文、docs/design/virtual-four-bucket.md §0-§9 全文、docs/reverse/virtual-resolution.md §2-§5.2、docs/reverse/remote-cache-projection.md §1.2（经设计 §4 引用面）、internal/repo/virtual.go VirtualMember 现状与 T-530 在途 diff、internal/adapter/helm/virtual.go（T-367 先例）、internal/adapter/maven/config.go ParseRepoConfig、internal/adapter/maven/virtual_metadata_test.go（fixture 形态）、known-divergence.yaml maven 条目、contracts/ 目录清单
Changes:       diff 全量逐行（单文件 virtual_metadata.go + 新测试文件）；上下游追读：serveVirtualMetadata 两个入口（metadata/sidecar）→ collectVirtualMetadata → virtualMetadataSteps → VirtualMemberOrder/ReadVirtualMember/ClassReader.Get 三缝；mergeMetadataDocs/merge*Versioning 确认本票零改动
Files:         internal/adapter/maven/virtual_metadata.go — 符合（seam 镜像 + 纯函数过滤 + 遍历改造，foundByPriority 短路逐字未动）；internal/adapter/maven/virtual_metadata_cache_skip_test.go — 符合（行为命名 + table-driven，纯函数层 + 真实栈两层）；reports/agents/T-531.md — 证据链完整，漂移点与集成动作登记诚实
Tests:         复跑取证（本 worktree，含 T-530 并行在途状态）：go build ./... + go vet ./internal/adapter/maven/... + gofmt -l internal/adapter = OK；golangci-lint run ./internal/adapter/maven/... = 0 issues；T-531 新腿 -v 全 PASS（CacheFacetSkip 4 腿 / LevelPolicySkip 6 腿 / LevelOf / SnapshotPolicySkip / ModulePolicySkip）；存量等价性腿复跑全 PASS（ModuleMerge/PriorityShortCircuit/SnapshotMerge/ComputedPerRequest/SidecarAndConditional/MemberFaults）；全包回归当前 FAIL=TestVirtualRenderSeams 两断言（jar 重复取 X-BinFlow-Cache 未 HIT + virtual DELETE 404≠405）——归因并行 T-530 对 internal/repo/virtual.go 的在途重写（212+/97-，期间还出现过 refuseVirtualDelete 未定义的 mid-edit 编译断），非本票 diff 面（详见 Risks/范围外）
Commands:      go build ./...; go vet ./internal/adapter/maven/...; gofmt -l internal/adapter; golangci-lint run ./internal/adapter/maven/...; go test ./internal/adapter/maven/ -run 'TestMetadataWalk|TestMetadataLevelOf|TestVirtualMetadataSnapshotPolicySkip|TestVirtualMetadataModulePolicySkip' -count=1 -v; go test ./internal/adapter/maven/ -run 'TestMetadataWalk|TestMetadataLevelOf|TestVirtualMetadata' -count=1 -v; go test ./internal/adapter/maven/ -count=1（×3：mid-edit 断 → 全包 FAIL 定位 → 腿隔离）
Outputs:       reports/agents/PR-156-review-T531-b.md（本文件）
Compatibility: 契约面核查属实：contracts/ 无 maven 契约、docs/compatibility/protocol/ 无 maven virtual 四桶细则（目录 grep 空）——reverse §5.1 为唯一语义源，实现正确标注。逐行对照 §5.1「触发与遍历」行：①「跳过所有 cache 仓」= 规格原文（置信度高，代码注释正确归属）；②「快照级跳 handleSnapshots=false」= 规格原文同行；③ 模块级 handleReleases=false 跳过 = §3.4（下载路径规则）对称推演，规格 §5.1 未具名——票面已登记为漂移点 1 并留翻回路径，归属诚实。「一行可翻回」核实：产品码隔离在 filterMetadataSteps 单分支（virtual_metadata.go:198-200），规格锚留在函数注释（:185-188）——结构达标；但翻回需连带翻 2 处测试腿（TestMetadataWalkLevelPolicySkip 模块 2 腿 + TestVirtualMetadataModulePolicySkip 整测试），「一行」指产品码口径成立、测试面非零，属可接受精度
Security:      本形态不深审（A 形态职责）；侧面确认：handle* 经 ClassReader 匿名缝只读公开 canonical 字段（helm T-367 同款先例，helm/virtual.go:68-96）；无新路径拼接面（relPath 仅 splitDirFile 后缀分类，原门未动）；无凭据接触
Performance:   每请求步表构建新增 len(order) 次 ClassReader.Get 行读（行缓存小表）+ 一次 O(n) 过滤——metadata 族非高 QPS 面，可忽略且票面已声明；合并体零缓存规则维持（每请求现算，§5.1「不缓存」行未被触碰）
Risks:         ① seam 在途：repo.VirtualMember 仍无 Facet 字段（virtual.go:622-626 核实；T-530 并行会话已在 repo 内部引入 Facet/FacetCache 类型但导出面未扩）——facetOfStep 恒返 facetPlain 对当前两桶序是精确值非猜测（virtualMemberOrder 不产 cache 步），判断成立；② 集成处方「body 换 return memberFacet(m.Facet)」为裸转型：当前镜像 (0/1) 与 repo.Facet (0/1) 数值对齐（今日成立），未来 repo 扩第三个 facet 值时越界转型会静默落 plain（fail-open：参与合并）——建议集成时改显式 switch/映射并定义 default 语义（见 non-blocking 2）；③ metadataLevelOf（splitDirFile 于 relPath）与 mergeMetadataDocs（l.Module 后缀）同一启发式两处代码位点，票面「单源」为启发式口径非代码口径（见 non-blocking 3）
Blockers:      全包回归复跑受并行 T-530 会话干扰：期间一次 mid-edit 编译断（service.go:1413 refuseVirtualDelete 未定义）、其后 TestVirtualRenderSeams 两断言 FAIL——失败腿全部位于 jar 重复取缓存头与 virtual DELETE 路由，与本票 diff 面（metadata 族 GET 拦截）零交集，且本票全部 metadata 腿 + 存量等价腿在同时刻全绿、实现者全包跑亦曾 ok 25.202s——归因充分，不构成本票取证障碍；已按隔离证据采信等价性
Next:          ① 转交 conductor：共享 worktree 内 T-530 在途改动当前打断 TestVirtualRenderSeams（缓存 HIT/DELETE 405 两断言）——T-530 收编时必须复跑全包确认自愈，若未自愈即 T-530 自身回归；② 模块级 handleReleases 漂移点随差分票开立时登记 known-divergence.yaml（当前仅在票面日志）；③ 集成动作（T-530 落地后 facetOfStep 换身 + 四桶序上游零增量断言）照 T-531.md Blockers 执行——fixture 形态已在位（virtualFixture.hits *atomic.Int64，virtual_metadata_test.go:26/39-40）；④ 范围外发现：mergeSnapshotVersioning 对 snapshotVersions 无条件按 (extension×classifier) 取较新合并，而规格 §5.1 合并算法行写的是条件合并（系统开关+快照级+客户端 M3 记号声明，否则剥除首基底 snapshotVersions）——存量 T-72 行为本票未改，建议补差分对照臂；⑤ npm virtual_packument.go cache 去重未动——与设计 §5.3「随实现票或后续票」口径一致，无越界
```

## 评审报告 T-531（形态: reviewer-b）
结论: APPROVE

### 设计符合度（§5.3 maven 行 / §6 耦合点表第 2 行 / §8 I11）

- **facetCache 恒滤**：filterMetadataSteps（virtual_metadata.go:192-194）对 cache 步先于级别判断无条件 continue——「跳过 FacetCache 步（remote 本体自带缓存语义）」逐字对应设计 §5.3 maven 行；测试双级别恒滤腿（cache_skip_test.go:107-108）锚死。
- **快照级 handleSnapshots 跳过**：:195-197，§5.3 maven 行第二半句；真实栈腿 TestVirtualMetadataSnapshotPolicySkip 同时验证了「级别键控非成员级」（模块级清单不受快照级拒答影响，:174-181）——这是规格语义的正确细化。
- **顺序保持前提下的过滤**：filterMetadataSteps 顺序 append 不重排；foundByPriority 短路原样保留在 collectVirtualMetadata（:255-257，diff 证实逐字未动，仅遍历变量 m→s）——设计 §6 耦合点表第 2 行「foundByPriority 短路语义不变，只是遍历序换四段 + cache 过滤」完全达成。优先级记账与 cache 滤除的交互正确：被滤步不产出文档故不触发 sawPriorityDoc（纯函数腿 2 专测此点）。
- **npm 域未越界**：本票 diff 仅 virtual_metadata.go 单文件 + 新测试（git diff --stat 核实），internal/adapter/npm 零触碰。

### 架构分层与 seam 结构

- 依赖面干净：新增代码仅经三缝出仓——VirtualMemberOrder / ReadVirtualMember（repo 服务缝）+ ClassReader.Get（匿名配置缝），import 仅 stdlib + internal/repo；无绕过 repo 直摸 storage/remote。
- memberFacet 镜像 + facetOfStep 单点读：全文件 grep 证实 seam 假设的耦合只落在 facetOfStep 一处（:141-143，配 //nolint:revive 说明），metadataWalkStep/测试用镜像类型——「T-530 未落地」耦合确实被限制在单点。集成后镜像类型非死码（仍为 step 字段与测试的类型），无残留死码路径。
- handle* config 缝先例一致：helm memberContext（helm/virtual.go:76-96，T-367）同为「ClassReader 读成员行 canonical JSON 公开字段」；且失败方向同为宽容（helm 缺行保 URL fallback，maven 缺行保双 true 不丢成员贡献），先例形态与语义双一致。ParseRepoConfig（config.go:56-70）对垃圾 config 亦落双 true 默认——宽容语义全链一致。

### 测试覆盖架构面

- I11 直接锚：TestMetadataWalkCacheFacetSkip 在 seam 形态步表上锚「合并遍历不含 cache 步」四腿（含纯 cache 序防御性零读腿 + 两桶恒等腿），foundByPriority 语义由存量 TestVirtualMetadataPriorityShortCircuit 复跑绿锚定——seam 未接线状态下这是可达的最强锚。
- I9 探针承诺核实：新测试文件本身未含上游计数断言（预期内——两桶序无 cache 步可断），但 fixture 形态已在位（virtualFixture.hits，virtual_metadata_test.go:26/39-40）且 T-531.md Blockers 明确登记集成后补断言及形态——「留 fixture 或明确登记」两条件均满足。

### 建议改进（non-blocking）

1. **漂移点登记位置**（T-531.md Compatibility）：模块级 handleReleases 跳过目前只登记在票面日志，known-divergence.yaml 尚无条目——差分票开立时应同步落册（已知该文件归 compatibility-engineer 写，故为转交项而非本票缺陷）。
2. **集成处方改显式映射**（T-531.md Blockers / virtual_metadata.go:142 注释）：`return memberFacet(m.Facet)` 裸转型依赖两包枚举数值对齐（今日 0/1 对 0/1 成立）；repo 未来扩第三 facet 值时越界值会静默按 plain 参与合并（fail-open，且恰在 I9 探针要防的双读方向）。建议 T-530 集成时写显式 switch 并对未知值定义行为（倾向 skip：宁欠合并不双读上游）。
3. **分级启发式两处位点**（virtual_metadata.go:158-164 vs :303）：metadataLevelOf 用 relPath 的 splitDirFile 目录后缀，mergeMetadataDocs 用 Layout.Module 后缀——两谓词今日等价（'/' 不含于 "-SNAPSHOT"，全路径后缀 ⇔ 末段后缀；Layout.Module=末目录，layout.go:154），但「同一启发式单源」是启发式口径非代码口径；可用 serveVirtualMetadata 已握有的 l.Module 收敛为单点调用。
4. **宽容默认分支无测试腿**（virtual_metadata.go:229-236）：h.class.Get 失败→warn+双 true 的路径无任何腿覆盖（现有腿均为 Get 成功或 config 缺字段）；补一腿注入坏 ClassReader 即可。
5. **handle* 缝的长期方向（记录不阻断）**：设计 §4 CacheProjection 已把 HandleReleases/HandleSnapshots 类型化为投影字段——T-530 落地后若 VirtualMember/步表携带类型化 handle*（投影继承单源于 repo 层），各 adapter 可退役逐协议 ParseRepoConfig JSON 解析。与 T-367 先例无冲突（先例读的是 chartsBaseUrl 这类 maven/helm 各自的协议字段，handle* 是全体协议共用的路由数据，收敛价值更高）。
