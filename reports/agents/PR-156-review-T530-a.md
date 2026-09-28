# PR-152 Reviewer A — T-530 virtual 四桶解析序 + F1 直访 + D-2 DELETE（correctness 形态）

```
Ticket:        T-530 [P0] virtual 四桶解析序实现 + <K>-cache 直访缝（F1）+ virtual DELETE 404 语义修复（D-2）
Role:          code-reviewer (reviewer-a / correctness)
Area:          internal/repo + internal/httpapi（router.go）；internal/remote/projection.go 仅作上游单源常量读取
Input:         conductor 派发（T-530、correctness 重点清单 7 项）；通读 docs/design/virtual-four-bucket.md 全文、
               docs/reverse/virtual-resolution.md §1-§7.5、docs/reverse/remote-cache-projection.md 全文、reports/agents/T-530.md、
               T-529 已审毕的 internal/remote/projection.go（常量单源）、docs/compatibility 台账未在本形态展开
Changes:       评审了 internal/repo{virtual.go,service.go,validate.go,api.go,operations.go,dockervirtual.go,remoteexternal.go}
               与 internal/httpapi/router.go 的全部未提交 diff（git diff 共 ~880 行），上下游追读：
               dispatchContent/enforce 链（router.go:2245-2366）、Get/ResolveMeta/listRowsChecked/Delete 全函数
               （service.go:387-450/582-638/1410-1470/1721-1750）、probeLocalMember/isFolderNode（virtual.go:351-385/validate.go:170）、
               listRows/remoteBrowseRows（browse.go:205-221/104-130）、pypi/nuget/maven/docker/cargo/goprogo 各 adapter 的
               svc.Get 调用点（F1 可达性）、internal/remote/projection.go（CacheSuffix 常量）
Files:         virtual.go — 四段序+投影面实现正确（逐条见结论区）；service.go — Delete/Get/ResolveMeta/List/Create/Update 分支正确；
               validate.go refuseCacheProjectionKey — 双挂接正确、有一个裸 "-cache" 低危边缘（N1）；api.go/dockervirtual.go/
               operations.go/remoteexternal.go — plainSteps 消费面与注释翻新正确；router.go — GET/HEAD 投影拦截正确（enforce
               前置为刻意设计，ACL 由 service 层父 key 求值+测试固化）
Tests:         见 Commands——全部独立复跑通过；无 skip（grep t.Skip 仅命中测试名 "Skips"）；race 定向跑通过
Commands:      cd /Users/lzw/dev-center/.claude/worktrees/clever-grothendieck-a3fa4f
               1. go build ./...                                            → exit 0（含并行 T-531 的 adapter/maven，编译面无冲突）
               2. go vet ./internal/repo/... ./internal/httpapi/...          → exit 0（无输出）
               3. gofmt -l internal/repo internal/httpapi                    → 空
               4. ~/go/bin/golangci-lint run ./internal/repo/... ./internal/httpapi/... → 0 issues.
               5. go test ./internal/repo/... -count=1                       → ok 149.023s
               6. go test ./internal/httpapi/... -count=1                    → ok 467.995s
               7. go test ./internal/repo/ -race -run 'TestFourBucket|TestVirtualCacheFacet|TestVirtualSnapshotPath|TestVirtualDelete|TestCacheProjection|TestVirtualResolutionMatrix|TestVirtualTrueMiss|TestVirtualMemberFaults|TestVirtualExploratory' -count=1 → ok 114.828s
               8. grep -rn "CacheProjection|ProjectionRegistry|remote.CacheSuffix" internal/repo/（非测试）→ 仅 virtual.go:890 常量别名（钉子 a 结构性证据）
               9. grep -rn "Repos().Create|Repos().Upsert" internal/ cmd/（非测试）→ service.go:2424（CreateRepo 内，已防护）+ trash.go:358（固定系统 key，非绕过）
Outputs:       reports/agents/PR-156-review-T530-a.md（本文件）
Compatibility: 与 docs/design/virtual-four-bucket.md §2/§3/§8 逐条核对：四段装配、展开算法、cache 语义 1/2/3/4/6 条全部
               落实；第 5 条（跳过规则）的 §3.6 半边落实、§3.4 handle* 半边未实现（N4，与存量成员级 handle* 缺口同源）；
               §7.5 DELETE 勘误（高置信）按规格实现；remote-cache-projection §1.3 建改仓 400 / §2.1 直访 GET / §2.2 ACL
               父 key 映射均实现并测试固化；PUT 措辞族漂移已在实现日志登记（低影响）。I1-I10/I12 有测试锚；I11
               （metadata 合并不含 cache 步）归 T-531 adapter 面（并行票，非本票缺陷，见 Next）
Security:      -cache 直访面：ACL 判定落在 service 层父 remote key（§2.2 映射），匿名 401/未授权 403/父授权放行三态
               有测试（TestCacheProjectionGateOnParent + wire 匿名腿）；-cache 建改仓 400 挡住投影冒名实体化；miss 不
               回源=不可被探测驱动的拉取（探测面净收紧）；D-2 删除 gate 在 virtual key ActionDelete（403 测试固化）；
               router 拦截绕过 enforce 是刻意的（投影 key 无 permission target），认证缺失场景由 service 层 p==nil → 401
               兜住并有 wire 测试。无注入/穿越新增面（路径全部经 validateNodePath 前置）
Performance:   每请求 O(V+E) 重算无缓存（FR-15-AC6 维持）；四段装配三遍扫描 = 常数倍于两桶；plainSteps 一次线性过滤；
               VirtualMemberOrder 删除了旧 memberIsPriority 的每成员二次 row 读（净减少 N 次 Get）；cache 步命中免回源
               = 上游流量净减少；无新 goroutine/锁/共享状态
Risks:         ① 段内 real-local 先于 cache 投影（I4/O1）是推演序，差分腿重点臂（ADR-0051 已预登记 Errata 条件）；
               ② N1/N2/N3 三个低危边缘（见 non-blocking）；③ 预存量 -cache 后缀实体行会被投影分支遮蔽（Get/GET 优先
               投影语义）——BinFlow 无外部存量承诺（ADR-0050 单窗口切换），测试夹具已改名规避，登记即可
Blockers:      无（全部取证命令可跑、全部通过）
Next:          ① 建议登记小票：N1 裸 "-cache" key 建仓放行（对齐 §1.3 全后缀拒绝）+ N2 cacheProjectionFolder 的 folder
               判据对齐 emptyFolderSHA；② 建议把 N3（api/storage/-cache 在 listRemoteFolderItems 开启时折入上游枚举行）
               登记为 F1 残余缝或随 F2/F4 面一起收；③ N4（walk 层 handle* 跳过规则半边）建议随 F8 深水语义票登记；
               ④ 差分腿 O1/O2/O3 重点臂复跑（mvn CLI + curl 双发）；⑤ I11 落点在 T-531（adapter Facet 消费），PR-152
               合并前确认 T-531 评审覆盖之；⑥ 本票为关键域（repository/remote cache/protocol）——双审 Reviewer B
               （architecture 形态）应并行出具 PR-156-review-T530-b.md
```

## 评审报告 T-530（形态: reviewer-a）
结论: **APPROVE**

### 取证摘要（correctness 七重点逐条）

1. **独立实跑**：四门 + 两包全量 + race 定向全部通过（见 Commands；repo 149s / httpapi 468s，比实现日志的 68s/181s 慢系机器负载，结果一致）。实现日志声称的失败面→修复对应关系与我所见 diff 一致，无虚假证据。无 skip。
2. **四段序正确性**（设计 §2.2/§2.3 逐条）：
   - 声明序 DFS ✓ — expandVirtualMembers 先登记后递归（virtual.go，I8 测试：子树落在声明槽位）
   - visitedKeys 环切 + root 预种子 ✓ — `visited := map[string]bool{virtualKey: true}`；I6 双入口测试
   - key 去重首现 ✓ — visited 检查；I5 跨层双向测试
   - 缺失成员静默 drop+warn ✓ — ErrRepoNotFound → warn+continue；I7 测试（FK 级联腿 + unknown-class 腿）
   - locals 恒先于 remotes ✓ — locals/remotes 双桶收集，装配序 locals→caches→bodies per priority class
   - 四段严格序 ✓ — [P-loc, P-cache][P-body][NP-loc, NP-cache][NP-body]（virtualMemberOrder 三遍扫描）；I1-I4 matrix 表驱动
   - 段内 real-local 先 cache 投影 ✓ — I4（O1 推演序，ADR-0051 Errata 预登记）
   - 无解析缓存 ✓ — 每请求重算，I12 测试（mark 与成员重指向下一调用可见）
3. **并发/失败**：store 故障（ListMembers/Repos().Get 非 NotFound）wrap 传播；probeLocalMember blob-open 失败 surfaced 不静默；context 全程显式传递；无新增 goroutine/共享状态（race 定向绿）。**钉子 a 结构性满足**：internal/repo 对 internal/remote 仅引用 `CacheSuffix` 常量（grep 证实），cache 步由成员 ledger 行在 virtualMemberOrder 内直接合成，成员资格天然来自展开序列行。**钉子 b 结构性满足**：walk 零投影注册表依赖，cache 步存在性是结构性的；可测等价面（cache miss 后本体步仍跑 / cache 步被跳过时本体步照常跑）分别由 TestVirtualTrueMissFallsThrough 与 TestVirtualSnapshotPathSkipsCacheFacets 固化。负缓存行不影响 cache facet ✓（O3 测试：负缓存窗内 direct miss 而 virtual 仍零上游服务 standing copy）。-cache 键 400 防护无绕过面 ✓（CreateRepo/UpdateRepo 双挂；全仓 Repos().Create 仅此两处+trash 固定 key；update 先于存在性 Get，PUT 不存在 -cache 得 400 非 404，有测试）。
4. **D-2 语义** ✓：DELETE 经 virtual → ErrNodeNotFound（adapter 渲染 404 "Could not locate artifact. Path: 'virt/...'"，wire 测试固化）+ 成员存活（service+wire 双测试）+ 成员直删 204 + virtual 再解析 404 全链；deleteVirtualOwnStorage 只扫 virtual key 自身命名空间（folder 行带子树、slash-append 兜底、ListByPrefix 兄弟拼写过滤正确）；gate 403 先于查找（测试固化）；嵌套 virtual DELETE 同语义（row.Type==TypeVirtual 单一分支）。不做 trash 捕获/pruneEmptyParents 与「own storage 仅为漂移行」的定位自洽。
5. **F1 直访** ✓：GET /<K>-cache/<path> 字节精确、零上游（repo+wire 双测试，计数 upstream）；miss 404 零上游（miss 不回源=不可探测驱动拉取）；TTL/新鲜期不参与（无窗口检查代码路径；walk 面的 O2 过期窗口测试覆盖同一 probeLocalMember）；PUT → 标准 404 "Failed to find the repository"（措辞族漂移已登记）；匿名 401 / 未授权 403 / 父授权放行（ACL 父 key 求值，§2.2）；/api/repositories 列表不含投影；local 父/幽灵父落回标准 404。router 拦截仅 GET/HEAD、仅父 row 存在且为 remote 时生效——分类正确的降级链。
6. **测试质量**：I1-I10/I12 均有实锚（I11 归 T-531 并行票，日志如实未声称）；O2/O3 已固化。翻新抽查 9 处（>5 要求）：matrix 两例期望翻转（I1/I4 新规格语义，方向正确非放宽）、StaleHit 改 snapshot 路径+双 remote（保留全部原断言并新增 fresher 成员零上游计数——净增强）、TrueMiss/Faults×2/Exploratory 三处加 priority mark（保持原断言语义所必需的序调整）、WriteUnrouted405 拆出 DELETE 新 404 断言、DeleteNeverPropagates 退役（有指针指向新文件，D-2 语义翻案）、t80/remote_virtual 删 nested 拒绝行**并新增接受子测**、cleanup/t324 纯夹具改名（-cache→-cleanup，断言集不变）。**无一处弱化**。
7. 钉子 a 见第 3 条——结构性核验通过。

### 必须修改（blocking）

无。

### 建议改进（non-blocking）

- **N1**（validate.go:69 refuseCacheProjectionKey）：裸 key `"-cache"`（parent 剥后为空）不落 CacheProjectionTarget → 建仓放行。规格 remote-cache-projection §1.3 拒绝**一切** `-cache` 结尾 key。无投影歧义、无冒名面（"-cache" 作为普通实体仓名语义完整），低危；建议 guard 改用 `strings.HasSuffix(key, cacheProjectionSuffix)` 判定，一行 diff。
- **N2**（virtual.go cacheProjectionFolder 首分支）：`Nodes().Get` 命中任意行即返回 ErrIsFolder——含**斜杠结尾的文件行**（pypi simple 页 / nuget v2 资源的缓存形态；remote Get 分支 T-406b 为此专门用 `n.Sha256 == emptyFolderSHA` 判据）。当前 wire 不可达（会传斜杠路径进 svc.Get 的 adapter 臂都先自行行分类，-cache key 在彼处 404），属潜伏坑；建议判据对齐 emptyFolderSHA，非 folder 行走 probeLocalMember 出字节。
- **N3**（service.go ResolveMeta:590 / listRowsChecked:1734 委托）：api/storage/-cache 面委托父 remote 的 ResolveMeta/List，在父 remote 开 `listRemoteFolderItems`（T-448 可选浏览）时 `listRows→remoteBrowseRows→BrowseRemote` 会触上游枚举并把**未缓存的上游行**折进 -cache 列表——与投影面零上游姿态相悖（参照 cache 仓是 local 类，REST 面只展示已存行）。flag 门控、流量低；建议投影分支改走本地语义读取或登记 F1 残余缝。
- **N4**（设计 §3 第 5 条半边）：`handleReleases=false`（release 可解析路径）跳过 cache 步、快照路径 `handleSnapshots=false` 成员跳过——walk 层未实现（全 internal/repo 无 handle* 消费，grep 证实）。与存量成员级 handle* 跳过缺口同源（两桶时代即无），需要 walk 层模块信息解析（GAVC/布局）方可实现，默认配置（handle*=true）无观测差异，§8 不变量不含。建议随 F8 深水语义票登记，避免口径悬空。
- **N5**（service.go deleteVirtualOwnStorage slash-append 臂）：漂移行仅有子文件、无 folder 行的目录拼写删不掉（兜底臂要求 `path+"/"` folder 行存在才 dropSubtree；local 面用 DeleteByPrefix 不要求 folder 行）。own storage 仅有手种漂移行的极端边缘；建议兜底臂改为「folder 行命中或子行存在即 dropSubtree」。
- **N6**（测试小补，非必需）：F1 直访面的 TTL 过期后仍服务（wire/repo 面显式 advance clock）未单独断言——O2 测的是 walk 面；两处共用 probeLocalMember 且无窗口检查，结构保证成立。
