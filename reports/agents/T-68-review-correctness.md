# 评审报告 T-68（视角: correctness — 触发时机与并发合并）

结论: APPROVE
日期: 2026-08-20 · reviewer: code-reviewer · 输入: T-68.md、T-83.md、calc.go/put.go/handler.go/api.go/layout.go/version.go、docs/reverse/maven-npm-pypi.md §1.4/§1.5、repo-semantics §3、PRD milestone-3 FR-17 v1.2、BOARD.md 只读

## 取证（实际执行）

- `CGO_ENABLED=1 go test -race -count=1 ./internal/adapter/maven/` → ok 30.9s；`./internal/repo/` → ok 48.1s；`go vet`（两包）零告警；`gofmt -l` 零文件。
- 并发压力复跑：`-race -count=5 -run 'TestCalcConcurrentMerges|TestCalcInterleavedRecalcsStaleWriteGuard|TestCalcSyncTriggerTiming|TestCalcDeleteCascade'` → ok 52.0s。
- **真服务器并发探针**（`/tmp/t68-review`，127.0.0.1:18089 全新数据目录）：4 worker 并发 —— 双 release 版本（pom+旁车+metadata PUT ×8 轮）、unique snapshot 递增 buildNumber ×12、同 GAV「deploy→delete」churn ×10 轮交错。终态：module metadata versions=[1.2.0-SNAPSHOT,1.3.0,1.4.0] latest=1.4.0（churn 的 9.9.x 全部收敛清除）；snapshot 文档 buildNumber=12、jar/pom 双条目 updated 去点号；9.9.x 残留 404；server log **0 条 5xx、0 条 ERROR**。探针中 16 个 409 全部是探针自己故意发错 artifact 旁车 sha1 的正确拒绝（client-checksums），metadata PUT 全 201。
- **AC6 现场复核**：并发收敛后 `maven-metadata.xml.sha1/.md5` GET == 对 GET XML 现算（cd188b2b…/f207f873… 双双相等）。
- clean-room 抽查：对照 `reverse-src/.../maven/MavenMetadataCalculator.java` 与 `MavenMetadataCalculationInterceptor.java`。**通过**——机制不同构（ItemTree/AQL vs 前缀 list+过滤；坐标取 sample pom 的 RepoPath 解析 vs 目录派生——注意 Artifactory 的 fromRepoPath 也是从**路径**而非 pom 内容取坐标，两者行为等价）；无逐行对应；分歧均落在 docs/reverse/PRD 已定案条文或代码内标注的裁决上。

## 逐项核查结论

1. **触发时机四类** ✓。`afterArtifactDeploy`（calc.go:196）`l.Timestamped || (pom && l.Snapshot)` 与拦截器 `shouldCalculateMavenMetadata`（isUniqueSnapshot || isPom&&isSnapshot）逐条对应；pom 祖父目录异步非递归（`trigger{org,module}` 无 version）；delete 两侧异步（version+module）；sidecar 删除无触发（事实未变，正确）。接线以落盘 `node.RepoKey`（put.go:148/150）——virtual 写落 member、metadata 归 member，正确；remote DELETE 经 class 门跳过（handler.go:273，TestCalcRemoteRepoUntouched 实证）。异步失败观察面 = slog ERROR（不上浮内容面，AC5 零 5xx 的实现），无重试、靠下次触发自愈——M3 可接受（见 non-blocking 2）。
2. **合并语义（AC5）** ✓。论证成立：trigger 在 Put 返回后触发（事实已落盘），每次重算在 `exec` 锁**内** list+write（recalcSync:157-158），故按锁序最后一次执行的重算，其 listing 必然晚于所有先其触发的 deploy 落盘——stale 覆盖 fresh 在锁序下不可达。TestCalcConcurrentMerges（-race ×5）与真服务器 4-worker 探针均收敛。锁粒度为全局单锁（非 per-repo/directory）：每次重算 = 一次前缀查 + 一次小写，M3 deploy 频率下无争用担忧，注释已述取舍；跨进程边界见 non-blocking 6。
3. **版本组目录生成** ✓。versions=含直系 pom 子目录集去重（spec 的「同版本多行取 created 最新」在 set 语义下自然满足）；`isPomFile`（calc.go:545）先剥 checksum 后缀再看 `.pom`，方向正确（`x.pom.sha1` 不计，矩阵已钉）；排序/比较器与 T-67 `VersionComparator` 同一实现；latest=排序末位（含 SNAPSHOT）、release=末位非 SNAPSHOT、全 SNAPSHOT 省略、lastUpdated 锁内取 UTC 时刻。前缀串扰安全：`likePrefix`（substores.go:186）segment-aware（"a" 不匹配 "ab"），且 `Cut`+`Contains(file,"/")` 双重过滤。
4. **SNAPSHOT 目录生成** ✓。buildNumber/timestamp 取最新 unique pom（数值比较+timestamp 决胜，对应 BuildNumberSnapshotComparator 语义）；无 unique pom 固定 1 无 timestamp；unique 无 pom 取全部 unique 文件最大 N（spec 沉默裁决，保编号单调，合理）；snapshotVersions 每 ext×classifier 取最新（createdAt 决胜、buildNumber/timestamp 平局）；`<updated>` 去点号。
5. **删除联动** ✓。无 pom → 删文档+`.sha1/.md5/.sha256` 旁车（sha512 P2 有注记）；RTFACT-6242 守卫（非 snapshot 目录的 snapshot 型内容保留，探针 parse `<snapshot>` 块或 snapshotVersions 任一）；探针读失败 fail-safe 不删（calc.go:576-587）。
6. **metadata 旁车 PUT 不再 409**（put.go:206-214）✓ 与规格 §3 一致且必要：v1.1 下任意 deploy 都会重写 metadata 字节，mvn 随后 PUT 的旁车（对它自己的 XML 算的）与服务端实测必然不一致——不豁免则 mvn deploy 100% 死于 409；>1024B 守卫与 target-must-exist 不变；落实测值使存储旁车恒等于现算。真客户端五次 deploy 全 201 + 我方探针佐证。
7. **SPI PutWithOptions**：语义核心逐条 ✓——只跳 delete 半检查（authorizeContentPut:582 的 `!skipOverwrite`）、写门不豁免（:587）、同 sha 幂等重传豁免保留（:579）、`Put` = 零值薄封装（:301）、PutFromBlob/PutLandedBlob 传 false 不变。**godoc 调用方范围与 T-83 措辞存在偏差**→ non-blocking 1（行为按 PRD 定案正确，需 architect 对 T-83 文字做勘误或对 godoc 补一句）。
8. **写经触发 principal 的 write 授权** ✓ 合理。服务端计算是触发写操作的伴生产物，可写性由触发 write 权限担保（T-83 安全边界原文）；极端 ACL 下降级 ERROR/WARN 不 5xx、下次有权限触发自愈；「服务端权威写免授权缝」未擅自扩面、已留 SPI 决策——正确处理。注：removeMetadata 的 Delete 同理受 trigger principal 的 delete 权限约束（write-only 用户 PUT pomless 目录后清理会降级、bogus 文档暂存至下次有权触发）——同一已披露降级面的自然延伸，不 5xx，可接受。

## 必须修改（blocking）

无。

## 建议改进（non-blocking）

1. **T-83 godoc 条款勘误（architect 动作）**：T-83 §4 写「仅服务端自有写入…godoc 明示 client puts must not set」，而 api.go:322-330 的 godoc 明列「client re-PUTs of checksum sidecars / maven-metadata.xml」为消费者。实现方向是对的——PRD FR-16「maven-metadata.xml 与旁车 checksum 文件永不触发覆盖检查（客户端重发是常态）」+ repo-semantics §3「可自由重写」都是**客户端可见**的路径族属性，按 T-83 字面实现会让 write-no-delete 用户每次 deploy 的 metadata 变更字节重发 403，T-67 遗留②就无法关闭（TestMetadataOverwriteFreedom 正是钉此行为）。建议：T-83 勘误一句（豁免按**路径族**生效、由服务端 maven 面决定、任何 HTTP 面不可选），或 godoc 补「the option is server-decided per path family — never client-selectable」。
2. **asyncRecalcTimeout 预算在取锁前起算**（calc.go:182）：30s ctx 在 goroutine 启动时创建，排队等 `exec` 锁的时间计入预算——极端触发风暴下队尾重算可能 deadline 超时（ERROR + 该次重算丢失，下次触发自愈）。建议取锁后再建 ctx，或对 pending trigger 按目录去重合并（Artifactory 用 work-item 队列 + 去重）。观察面仅 slog，无 metric/重试——M4 telemetry 候选。
3. **dotted group 段歧义（现场证实）**：`PUT /mvn-probe/com.acme/dotty/1.0.0/dotty-1.0.0.pom` 201，但该目录的 metadata **任何位置都不生成**（`com.acme/dotty/maven-metadata.xml` 与 `com/acme/dotty/maven-metadata.xml` 均 404）——Layout 把单段 `com.acme` 与两段 `com/acme` 折叠为同一 OrgPath，trigger.dirPath 的点→斜线展开落到另一棵树（list 为空 → no-op，无跨树污染）。真实 mvn 客户端不产生此形态；建议 Parse 拒绝 org 段含 `.`（maven-2-default 的 groupId 路径段不含点）作为加固，或注释记录。
4. **TestCalcInterleavedRecalcsStaleWriteGuard 自弱化**（calc_test.go:705）：末尾的 settle recalc 使断言对任何实现都通过（同义反复）；不变量实际由 TestCalcConcurrentMerges 有效钉住。建议去掉 settle recalc 或断言中间态。
5. **注释与实现不符**（calc.go:174）："The trigger principal is captured by value" —— `p *repo.Principal` 是指针逃逸出请求。principal 实际不可变、无竞态，改注释或复制值即可。
6. **跨进程边界一行 godoc**：exec 锁的排序论证是进程内的；多实例部署会失效（嵌入式 SQLite + ADR-0007 本就是单进程拓扑）。建议 calc.go 注释补一句「single-process scope (M1 topology)」防未来误读。
7. **路由 virtual 的 metadata 寻址无测试**：`node.RepoKey` 寻址（put.go:148）是特意为 routed virtual 写落 member 设计的，但矩阵无「经 routed virtual deploy → metadata 落 member」的钉子（maven-virtual 桩未配路由）。一行测试防回归。

## 三处 spec 沉默裁决（复核意见）

1. **release 版本目录不生成 version 级文档：同意**。规格 §1.4 只定义版本组与 SNAPSHOT 目录的内容规则；reverse 侧同样只在 snapshot 分支生成（`createSnapshotsMetadata` 仅 `isSnapshot(nodePath)`），release 发现走 module 级与 Maven Central 惯例一致；release 目录重算作为清理通道（RTFACT 守卫保护）自洽，TestCalcReleaseVersionDirRuling 三态钉死。
2. **客户端 PUT metadata 取同步重算：同意**。v1.1「权威内容以服务端计算为准」+ M15 型「PUT 后即读」要求 201 返回时内容已权威；成本有界（一次前缀 list + 一次小写）；异步会让等效合并对调用方可观察地抖动、伤 `-U` 语义。与旁车 409 豁免（mvn 后续旁车对重算后字节算值）配套自洽。
3. **groupId/artifactId 目录派生：同意，附边界提示**。maven-2-default 下目录即坐标、上传时 layout 已对文件名-目录一致性校验；且 Artifactory 的坐标同样取自 pom 的 **RepoPath 解析**（非 pom 内容）——两者行为等价，非简化。唯一漏洞是 non-blocking 3 的 dotted 段形态，建议加守卫收口。

## 范围外发现（交 conductor）

- **maven 路径的目录 DELETE 不可达**：`Parse` 对目录形路径（如 `com/acme/demo-app/1.0.0/`）按 artifact 模板解析失败 → 400，递归目录删除（repo-semantics §4）在 maven 面无法触达。属 T-67 既有面、非本票 diff；建议小票跟进（目录形路径 → 跳过 artifact 模板、走 svc.Delete 的 folder 分支 + afterDelete 两侧树）。
- Artifactory 版本组文档携带 `<version>=latest` 顶层元素、BinFlow 省略——与 docs/reverse §1.4 内容规则清单（未列该项）一致，属规格文档层已定案差异，报 architect 知悉即可。
- afterDelete 对非 pom artifact 也触发 module 重算（reverse 只在 pom 删除时）——多一次幂等重算，无害，已知悉。
