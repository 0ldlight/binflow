# PR-152 · T-530 Reviewer B（architecture 形态）评审报告

```
Ticket:        T-530 [P0] virtual 四桶解析序实现 + <K>-cache 直访缝（F1）+ virtual DELETE 404 语义修复（D-2）
Role:          code-reviewer (reviewer-b)
Area:          internal/repo + internal/httpapi（架构/兼容语义/测试覆盖面视角；internal/remote 投影注册表为 T-529 已审物，作耦合对象核验）
Input:         conductor 派发（重点 1-6 项）；docs/design/virtual-four-bucket.md（§4/§5/§6/§8/§9 全文）；docs/reverse/virtual-resolution.md §1/§2/§3.4/§3.6/§5.1/§7.2/§7.5；docs/reverse/remote-cache-projection.md §1.1-1.4/§2.1/§2.2；reports/agents/T-530.md；reports/compatibility/L028-maven-v2-batch1-diff.md；docs/compatibility/known-divergence.yaml（-cache 相关条目核查）；internal/remote/projection.go（T-529 耦合对象）
Changes:       internal/repo 七文件 diff 全读（virtual.go 977 行全文走读、service.go/validate.go/api.go/operations.go/dockervirtual.go/remoteexternal.go diff）；internal/httpapi/router.go diff + dispatchContent/authorize/splitFirstSegment/api-storage 路由链走读；四新测试文件全文 + virtual_test.go 翻新 diff 全读 + t80/t324/cleanup/remote_virtual 翻新 diff 抽查；上游追读：Get/ResolveMeta/listRowsChecked 门禁序、middleware.go authorize、storage.go metaNodeResolver
Files:         internal/repo/virtual.go——四段序/展开/F1 服务面：与规格 §1/§2 逐行符合（见 Tests/结论区）；internal/repo/service.go——Get:397/ResolveMeta:590/listRowsChecked:1734 三处投影分支均在 repoKey 门禁之前、委托父 key 求值，§2.2 ACL 映射正确；Delete:1431→deleteVirtualOwnStorage:1572 §7.5 直译成立；validate.go:69 建仓防护三类 rclass 全拒+update 先于存在性（§1.3/§2.1 中置信行同向）；api.go 契约注释翻新如实；operations.go/dockervirtual.go plainSteps 折叠=行寻址面同命名空间规则，正确；httpapi/router.go:2289 GET/HEAD 拦截位（enforce 前）成立且为唯一可行位
Tests:         复跑（只读）：repo 定向 8 组全绿（8.845s）；adapter 九包（避 maven/T-531 在改）：npm/pypi/helm/cargo/rpm 五包 FAIL——全部为 T-530 语义翻转导致的存量钉子过期（证据见结论区 blocking 1），nuget/goproxy/deb/conan/docker 五包 ok（docker 193s 全绿）
Commands:      go test ./internal/repo/ -run 'TestFourBucket|TestVirtualCacheFacet|TestVirtualSnapshotPath|TestVirtualDelete|TestCacheProjection|TestVirtualResolutionMatrix|TestVirtualTrueMiss|TestVirtualFaults|TestVirtualExploratory' -count=1 → ok 8.845s
               go test ./internal/adapter/{npm,nuget,pypi,helm,cargo,goproxy,deb,conan,rpm}/...（分包）→ ok×5 / FAIL×5（npm TestVirtualRenderSeams×2、pypi TestVirtualRenderSeams×1、helm TestVirtualWriteRouting×1、cargo TestVirtualRemoteMemberChain+TestVirtualWriteRouting×2、rpm TestVirtualWriteRouting×1）
               go test ./internal/adapter/docker/ -count=1 → ok 193.418s
               go build ./... → 通过；gofmt -l internal/repo internal/httpapi → 空
               grep -rn '"-cache"|CacheSuffix|CacheProjection|ProjectionRegistry' internal/repo internal/httpapi（非测试）→ 唯一拼接源=virtual.go:890 remote.CacheSuffix 别名，无第二处字面量
Outputs:       reports/agents/PR-152-review-T530-b.md（本文件）
Compatibility: 规格符合度总判：四段序=§2 逐段符合（含 §1 分桶收集前提下的段内三遍扫描=§2「cache 与 remote 不相邻」推演）；快照跳过=§3.6 walk 层统一；D-2=§7.5 高置信直译（404+成员存活+own-storage drift 行删除）；F1 GET/HEAD=§2.1 高置信行直译（字节精确/零上游/ACL 父 key/列表不含投影）；PUT 落标准 404=与参照状态码同、措辞族异——登记属实但仅在工作日志（T-530.md Compatibility 行），known-divergence.yaml 无条目（建议补，non-blocking 1）；GET /api/repositories/<K>-cache 派生直查未实现=与登记一致（留缝+Next ③ 小票），确认无越权实现；§7.2 locals→caches 序（F2）确认未动：ResolveMeta→listVirtual 仍走四段 plain 序而非 §7.2 序，缝保持
Security:      F1 面 ACL：内容面拦截在 authorize 中间件 a.Can 之前（router.go:2289），实际判定在 getCacheProjection:926 以父 key 求值（§2.2），401/403/父授权三态有测试；/api/storage 面 routeAuth{} 无内容 action（router.go:1204 等），判定同样落 service 委托父 key——中间件对投影 key 无 a.Can 求值路径，无越权面；cache 步零上游=不可探测驱动的拉取；-cache 建改仓 400 挡实体化冒名；D-2 gate 在 virtual key ActionDelete（403 测试固化）；无凭证入日志
Performance:   B 形态记录：每请求 O(V+E) 重算维持（FR-15-AC6，I12 有锚）；plainSteps 线性过滤与 cache 步常量阶合成为新增全部成本，同阶；cache 步命中免回源对上游净减少；无新共享状态/goroutine（A 形态 -race 未跑的判断成立）
Risks:         ① O1 段内 real-local 先于 cache（I4）为推演实现，ADR-0051 Errata 预登记在案（设计稿层面），差分腿重点臂（Next ②）——合理；② timestamped snapshot 路径（无 -SNAPSHOT 段）不触发 cache 跳过（isSnapshotResolutionPath virtual.go:342 启发式），与非快照路径下 *-SNAPSHOT.bin 误跳同族，见 non-blocking 3；③ gate-day（storeArtifactsLocally=false 可存储日）walk 侧 cache 步不感知 gate（结构性合成），见 non-blocking 6
Blockers:      无（取证全部可跑；adapter 五包失败为被评审改动的确定归因，非环境障碍）
Next:          ①【上报 conductor】D-2/Facet 缝变更的 adapter 消费面收口必须建票或并入 T-531 扩围（五包钉子翻新 + deb/conan 两面 adapter 层 405 拦截移除），PR-152 合并前须落地——见 blocking 1；② PUT 措辞族漂移与 §2.1 派生直查缺口建议交 compatibility-engineer 评估 known-divergence 落账（差分腿后）；③ 建议 architect 在 virtual-four-bucket.md 补 Errata 注记：§5.1「cache 步 priority/handle* 从投影注册表读」已被 conductor 钉 a 的结构性处理取代（walk 不消费注册表，priority 单源自成员行 config 同字段），设计文档现与实现读起来不一致；④ 差分腿 O1/O2/O3 重点臂 + timestamped snapshot 观察臂
```

## 评审报告 T-530（形态: reviewer-b）
结论: REQUEST_CHANGES

### 必须修改（blocking）

- **[跨 area 集成缺口] D-2 与四段序缝变更的 adapter 消费面未收口——树对 ./internal/adapter 红五包 + deb/conan 两协议面语义分叉**。T-530 把 `Service.Delete` 的 virtual 分支从 405（`refuseVirtualDelete`，本票删除）翻转为 404（`deleteVirtualOwnStorage`，service.go:1431/1572），并把 virtual 重复下载的服务源从 remote 成员 FR-20 链改为 cache facet（零上游、无 `X-BinFlow-Cache` 头）。这是跨包可观测契约变更，消费面在 dev-go-adapter 域，T-530 的 area 管不到，但收口无人认领：
  - **五包存量钉子过期（树红）**，实测归因（命令见 Commands）：
    - `internal/adapter/npm` TestVirtualRenderSeams：`virtual unpublish = 404, want 405`（virtual_render_test.go:150）+ `repeat tarball X-BinFlow-Cache = "", want HIT`（:120）——前者钉 D-2 旧 405，后者钉两桶时代「重复下载经 FR-20 得 HIT 头」的可观测面；
    - `internal/adapter/pypi` 同文件同族（virtual_render_test.go:101 repeat 头）；
    - `internal/adapter/helm`（virtual_test.go:186）、`internal/adapter/cargo`（virtual_test.go:283）、`internal/adapter/rpm`（virtual_test.go:397）各一例 `virtual DELETE = 404, want 405`；
    - 全部是被 D-2/Facet 新语义取代的过期断言，非运行时缺陷；但 PR-152 以树为合并单元，红树不可过闸。
  - **deb/conan 两协议面仍活在旧契约下**：`internal/adapter/deb/virtual.go:188/199/210` 三处与 `internal/adapter/conan/virtual.go:170` 在 **adapter 层**自答 405（`refuseVirtualDelete`，deb:746 注释自认「the repo package's own refusal, restated」），DELETE 根本不落 service 层——repo 层拒答已删、adapter 层复述仍在，deb/conan 的 wire 面 virtual DELETE 依旧 405，与 §7.5（404+成员存活）及本票 L028 D-2 闭环声称形成**按协议分叉**；且这两包测试仍绿，无任何红灯提示 conductor。日志 Compatibility 行「wire 面已由 httpapi 测试固化（D-2 404…）」只覆盖 generic 路径，对 deb/conan 不成立。
  - **T-530 日志门不完备**：Commands 只跑 repo+httpapi 两包（声称如实、可复现，非虚假证据），但改动爆炸半径含 15 个 adapter 消费文件（设计 §1 as-built 表自列），五包红面与 deb/conan 分叉未在日志 Risks/Next 出现。
  - → 建议改法（最小）：conductor 建 adapter 翻新票或扩 T-531 范围——(a) 五包钉子按 D-2 新语义与 cache-facet 可观测面翻新（参照 T-530 已翻新的 `internal/repo/virtual_test.go` 手法：mark 保持原断言意图）；(b) 删除 deb:746/conan:182 的 adapter 层 `refuseVirtualDelete` 及其四处调用，让 DELETE 落 service 层统一语义；(c) T-530 日志补记 adapter 红面与分叉。PR 内收口后再转 QA。

### 建议改进（non-blocking）

1. **PUT /<K>-cache 措辞族漂移的登记载体**：登记属实（T-530.md Compatibility 行：BinFlow "Failed to find the repository '<K>-cache'…" vs 参照 "Could not find a local repository named <K>-cache to deploy to."，状态码一致、deploy 404 家族、低影响），但仅在工作日志，known-divergence.yaml 无对应条目。建议差分腿覆盖后由 compatibility-engineer 落账（现无 differential authority，不强制先行）。
2. **`GET /api/repositories/<K>-cache` 派生配置直查（§2.1 中置信）**：确认未实现（标准 404）且与登记一致（日志 Compatibility + Next ③）——缝位纪律合规，维持小票跟进即可。
3. **isSnapshotResolutionPath 启发式（virtual.go:342）**：段后缀 `-SNAPSHOT` + `[INTEGRATION]` 子串判定，(a) timestamped snapshot（`1.0-20260101.123456-1`，无 -SNAPSHOT 段）不触发 cache 跳过——参照按模块信息判定快照路径时大概率含 timestamped 形态；(b) 非快照目录 `foo-SNAPSHOT.bin` 会误跳 cache。建议列入 O 家族差分观察臂（Next ④），Errata 触发条件同 O1 处理。
4. **设计 §3.5 的 handleReleases cache 步跳过未实现**：walk 层无 handle* 消费（handle* 消费移 T-531 的 ClassReader 缝，api.go:815 注释已如实翻新为多消费者）。当前 BinFlow remote canonical 配置不持久化 handleReleases/handleSnapshots（remote/projection.go:98-101 自证：恒 true），不可达状态，不构成缺陷；建议随设计 Errata（blocking 附带项 ③）一并注记该偏差与可达日处理点。
5. **F1 直访 wire 覆盖仅 generic 包型**（httpapi/cache_projection_test.go 全部 generic 父）：maven/npm 父包型经 `-cache` key 的 GET（含 metadata 路径落各 adapter 拦截面）未测。建议 adapter 收口票顺带补 maven 父一例（参照 Artifactory 差分臂同源）。
6. **gate-day 提示**：`storeArtifactsLocally=false` 落地日，F1 面有拒绝点+注释（virtual.go:919-924），但 **walk 的 cache 步是结构性合成、不查 gate**——gate 票须同时处理 virtualMemberOrder 侧，建议在该缝注释处补一行指针（现注释只写了 F1 面）。

### 逐项判定记录（重点 1-6）

1. **§6 耦合点表**：`repo` 未消费 `ProjectionRegistry`（grep 证实零引用）——与设计 §5.1 字面（「cache 步 priority/handle* 从投影注册表读」）偏差，但 conductor 契约钉 a 已按「结构性满足」认可（T-530.md Risks ③ 如实记载），且语义等价成立：priority 单源=成员行 config 同字段同拼写（virtual.go:232 memberPriorityResolution 与 projection.go:108 projectionConfig 解析同一 `priorityResolution`），展开序列行天然是成员资格来源。`repo` 已持有 `*remote.Engine` 满足接口的断言成立但未被利用（引擎实现在 remote/projection.go:80 `var _ ProjectionRegistry = (*Engine)(nil)`，消费方仅 validate 常量与 F1 面 key 映射）。`-cache` 单源：grep 证实 repo/httpapi 非测试代码无第二处字面量（virtual.go:890 唯一别名）；validate.go:70 报错文案中的 `'-cache' suffix` 是消息文本非拼接逻辑。投影面 CacheProjectionTarget/getCacheProjection 与 §4 描述符八字段映射：消费面只用 Key 派生（剥后缀）与 gate 位注释，未消费八字段本体——与「walk 不消费注册表」同一裁定族，一致。
2. **分层边界**：repo→remote 仅取导出常量/既有 Engine/FetchError（预存依赖方向）；httpapi→repo 取导出 CacheProjectionTarget；无反向摸 storage 内部（probeLocalMember 走 md.Nodes/st 既有面）；router.go 只做分发（拦截→父 PackageType adapter 派发，无业务逻辑）；T-530 写入面严格限于 repo+httpapi+reports，未越界改 adapter/storage/remote（git status 佐证：adapter/maven 改动属 T-531、remote/projection.go 属 T-529）。
3. **规格符合度**：四段序=§2 逐段比对符合（段内 real-local→cache 序=O1 推演实现+ADR-0051 Errata 预登记；§1 分桶收集 locals 恒前 → 段内三遍扫描的合成即规格「cache 与 remote 不相邻」行）；cache 投影插入位置=段1/段3 正确；快照路径跳过全部 cache 步=§3.6 walk 层统一协议无关（virtual.go:295 skipCaches / :303 continue）；§7.5 DELETE 勘误=高置信直译（deleteVirtualOwnStorage：own 命名空间空→ErrNodeNotFound→adapter 404 ITEM_NOT_FOUND 形态、drift 行/文件夹子树删、成员永不动、gate 403 保持；wire 测试 virtual_delete_test.go:46 逐字钉 "Could not locate artifact. Path: 'virt/…'"）；F1 GET/HEAD 姿态=§2.1 高置信行直译（probeLocalMember 父命名空间、miss 404 零上游、markDownload audit=投影 key/count=父 remote=§2.1 stats 归属）；PUT=登记属实（见 non-blocking 1）。
4. **缝位纪律（§9）**：F2（§7.2 locals→caches 序——ResolveMeta/listVirtual 仍四段 plain 序，未实现 §7.2 序）、F3（search 映射零改动）、F4（stats 路径不重写 `<remote>-cache`，markDownload countRepo=remote key 维持）、F5（zap 零改动）、F6（patterns 三层过滤零改动）、F7（抑制 gate 仅注释留位 virtual.go:174-178，无 header 判定）、F8（深水语义未越权实现）——全部留缝未动，合规；`GET /api/repositories/<K>-cache` 直查未实现与登记一致（non-blocking 2）。
5. **测试覆盖架构面（I1-I12）**：I1=Matrix "I1 local-class first"+virtual_test 翻新例；I2=Matrix "I2"；I3=Matrix "I3"；I4=Matrix "I4"；I5=NestedExpansion 双向去重；I6=CycleCut 双入口；I7=vanished member（unknown-class+级联两腿）；I8=NestedExpansion+ServesContent 端到端；I9=ZeroUpstream（含对照直读面 +1 上游）；I10=SnapshotPathSkips；I12=RecomputedPerRequest——**11/12 有锚且复跑绿**；I11（metadata 合并不含 cache 步）归 T-531（进行中，范围外）。负缓存/陈旧副本/TTL 覆盖：O2（过期窗口零上游无条件服务）、O3（新鲜负缓存行+standing copy 并存，virtual 仍服务零上游）均有专测且用时钟推进，覆盖等级：行为票足够，差分腿待跑（Next ②）。存量翻新等价性：virtual_test.go 翻新手法正确（mark 加持保持原断言意图——TrueMiss/Faults/Exploratory 三例的 priority mark 注释如实；Matrix 两例期望翻转=新语义所致；WriteUnrouted405 拆出 DELETE；DeleteNeverPropagates 退役留指路注释）；t324/cleanup 纯 `-cache`→`-cleanup` 夹具改名；t80 删 nested 拒绝行+补接受测。翻新合规。
6. **两钉子核验**：(a) **成立**——walk 成员资格完全来自展开序列行（expandVirtualMembers 读 virtual_members ledger + Repos().Get 行；cache 步由 remotes 桶行直接合成 virtual.go:194-199，全程无注册表调用），投影 ok=false 三因不影响成员资格；结构性满足声明与代码一致。(b) **成立**——可测等价面确证：TestVirtualTrueMissFallsThrough "upstream 404 without a copy" 断言 hits==1（"the cache step adds none"）且落 local（cache 步 miss 后本体步跑+成员继续）；TestVirtualSnapshotPathSkipsCacheFacets 断言 cache 步全跳时 body 步 STALE+1 上游照常跑——「cache 步缺席/miss 不移除本体步」两面均有断言，日志声称 ④ 属实。

### 范围外发现（交 conductor）

- T-531 回炉中 `internal/adapter/maven/virtual_metadata.go` 与新测试 `virtual_metadata_cache_skip_test.go` 在工作树（另一 agent area，未评审，不在本结论内）。
- blocking 1 本体（adapter 五包红 + deb/conan 分叉）属 dev-go-adapter area 的收口工作，但因是 T-530 契约变更的直接后果且阻断 PR 合并，按闸门职责记为 blocking 上报。
