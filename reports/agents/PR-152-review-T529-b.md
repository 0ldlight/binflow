# PR-152 · T-529 评审报告（Reviewer B / architecture 形态）

Ticket:        T-529 [P0] `<K>-cache` 投影派生注册表（virtual 四桶解析 remote 侧底座）
Role:          code-reviewer (reviewer-b)
Area:          internal/remote（dev-go-storage 域）；改动为两个新文件，无存量文件触碰
Input:         conductor 派发（形态 reviewer-b，重点=设计符合度/分层/兼容语义/测试架构面/下游对接）；通读 docs/design/virtual-four-bucket.md 全文（§0-§9）、docs/reverse/remote-cache-projection.md 全文、reports/agents/T-529.md；追读 internal/repo/config.go（remoteConfig tags/defaultRepoLayoutRef/parseRemoteConfig）、internal/repo/virtual.go:100-165（memberPriorityResolution 探针姿势）、internal/repo/blackedout.go（blackout 读取面）、internal/repo/service.go:2340-2740（remote_configs 列写入面）、internal/httpapi/repo_config_render.go:180-225（v1 默认表）、internal/metadata/api.go（Repo/ErrRepoNotFound）、internal/remote/fetcher.go:114-175（repoPolicy 双 literal 先例）+ Engine 结构、internal/remote/probe.go/fetcher.go 的 `"remote"` 字面量姿势
Changes:       projection.go（175 行）+ projection_test.go（283 行）全读；上游追读到 canonical config 的生产侧（repo.Service 写面）与消费侧（blackedOut()/memberPriorityResolution 读面）两端；下游对接面追读到 internal/repo/api.go:832-861（RemoteFetcher seam 先例）
Files:         internal/remote/projection.go — 设计 §4 契约逐条符合（对照表见下），包边界干净，判定通过；internal/remote/projection_test.go — 架构级不变量覆盖充分（I12 reload/派生一致/gate 缝/非法 key/并发），3 处非阻断补充建议；reports/agents/T-529.md — 证据链完整，复跑全部对上（含 ⑥ 的 blackedOut 单源走读声明）
Tests:         复跑全绿（-count=1 非缓存）：build/vet/gofmt/golangci-lint 四门 0 告警；`go test -race -run TestCacheProjection` 6 测试 17 子用例全 PASS；全包 `go test -count=1 ./internal/remote/` ok 66.129s
Commands:      `go list -deps ./internal/remote | grep -c binflow/internal/repo`（=0，无反向依赖）；`go build ./internal/remote/... && go vet ./internal/remote/... && gofmt -l internal/remote`（BUILD_VET_FMT_OK）；`golangci-lint run ./internal/remote/...`（0 issues.）；`go test -race -run 'TestCacheProjection' -v ./internal/remote/`（全 PASS）；`go test -count=1 ./internal/remote/`（ok 66.129s）；clean-room 扫描 `grep -nE "LocalCacheRepo|DbCacheRepo|RepositoryServiceImpl|RemoteCacheRepoTypeConfig|Artifactory" projection*.go`（仅 2 处注释对公开 artifactory.xsd 默认值的锚引用，无反编译结构名）
Outputs:       reports/agents/PR-152-review-T529-b.md（本文件）
Compatibility: 与 docs/reverse/remote-cache-projection.md 逐条：§1.1 无持久实体（实现唯一 store 调用=Get，无写面）✓；§1.1 命名恒 `<K>-cache` 常量拼接（CacheSuffix 单源，无配置形态）✓；§1.1 投影条件 storeArtifactsLocally（gate 缝保留，注释登记不可达原因）✓；§1.1 description 后缀/时间戳镜像——不落字段（设计 §4 struct 无此位、无消费面，⑭登记，F1 缝位注释 projection.go:46-47 已挂）✓；§1.2 继承字段表逐项镜像（八字段全）✓；§1.2 checksumPolicyType 恒 client-checksums、snapshotVersionBehavior 恒 unique——**按设计落注释不落字段**（projection.go:37-44 明说 speculative seat = guessing past the spec，clean-room 防扩面正确姿势）✓；§1.3 后缀建仓防护常量消费方在 internal/repo（T-530，不在本票）✓。known-divergence.yaml 无投影面条目（docker/remote-cache-layout 等属 remote-cache-v2 语义面，不涉派生注册表）
Security:      纯只读派生（Repos().Get 单调用），remoteKey 直传 store 层参数化查询，无 SQL/路径拼接面；日志仅 repo key + store 错误文本，不含凭据（config blob 不落日志）✓
Performance:   每调用一次 store Get + 一次小结构 json.Unmarshal，无锁无跨调用状态，与设计 §2.3「每请求重算、无解析缓存」口径一致；热 path 影响可忽略（B 形态记录，A 形态主责）
Risks:         ① 双 literal 源（defaultProjectionLayoutRef / projectionConfig tags ↔ repo.remoteConfig）——import 环所迫，repoPolicy+pkgTypeHelm 先例同姿势且同步注释在位，漂移风险低；② store-fault 降级（ok=false + WARN）不可测（需 metadata 故障注入）；③ ok=false 三因重载（未知 key/非 remote 型/gate off/Store 故障）——对接语义见 Next 钉法
Blockers:      无（全部取证命令可跑、结果与实现日志一致）

## 评审报告 T-529（形态: reviewer-b）

结论: **APPROVE**

### 设计 §4 契约逐条对照（architecture 主证）

| §4 条款 | 实现锚 | 判定 |
|---|---|---|
| CacheProjection 八字段（名/型/序） | projection.go:48-66 | 逐字段一致 |
| 接口签名 `CacheProjection(ctx, remoteKey) (CacheProjection, bool)` | projection.go:75-77 | 逐字一致；ctx 显式传递 ✓ |
| 派生源 = 已加载 remote 行 | projection.go:123（`e.md.Repos().Get`，与 fetcher `loadRepo`:1267 同读取面） | ✓ |
| config reload 重建 | 现读现派（每次调用新读） | 语义严格强于「reload 时重建」（reload by construction），与 §2.3/FR-15-AC6 口径一致；⑬已登记为口径说明非偏离，**判级：不构成偏差** |
| never a stored entity | 全函数唯一 store 调用是 Get；无 repositories 行、无任何写面 | ✓ |
| `-cache` 常量本包单源导出 | projection.go:25 `const CacheSuffix = "-cache"` | ✓ |
| gate storeArtifactsLocally=false → 无投影（当前恒 true 缝位） | projection.go:151-156 分支 + :102-106 缝位注释 | ✓（测试盖到 false 臂，虽今日不可达） |
| 固定值（checksum=client / snapshot=unique）落注释 | projection.go:37-44 | ✓ 落注释不落字段，防猜测扩面（ADR-0001 口径正确） |
| 明确不做（列表/存储面） | projection.go:11-18 注释 + 实现无相应代码 | ✓ |

### 分层与依赖方向

- `go list -deps ./internal/remote` 对 `internal/repo` 计数 = **0**：projection.go 仅 import context/encoding/json/errors + internal/metadata，包边界干净，无反向依赖（repo 是消费方、已单向 import remote，见 repo/api.go:832-861 RemoteFetcher 先例）。✓
- `"remote"` 类型字面量（projection.go:135）与包内既有姿势一致（fetcher.go:376/1278、probe.go:95）；metadata 包不导出类型常量（常量在 repo/api.go:139-141，import 即环），字面量是既有约定，非新漂移。

### 兼容语义口径（恒定值三查）

- handleReleases/handleSnapshots 缺省 true：锚 v1 render 默认表（repo_config_render.go:187-188 `{"handleReleases", true}`/`{"handleSnapshots", true}`，实读取证）+ remote canonical 结构不持久化此二键（repo/config.go remoteConfig 无此 tag）——当前行恒读 true = 镜像有效值，读取面已就位待 seat 落地。✓
- blackout 单源声明走读成立：repo.Service 建/改 remote_configs 不写 blocked_out 列（service.go:2346-2350/2729-2740 实读，RemoteConfig 字面量无 BlockedOut 位），repo 侧既有读面 `blackedOut()` 同样读 config blob——投影取 JSON 键与消费面同源。✓
- repoLayoutRef 缺省 `maven-2-default` 与 repo.defaultRepoLayoutRef（config.go:59）同值，xsd 公开默认，双 literal + 同步注释。✓

### 测试覆盖架构面（§8 映射）

I12（每请求重算）= TestCacheProjectionReloadSemantics（改配置下一调用可见 + 删行即灭，两臂都有）✓；派生内部一致/并发 = ConcurrentDerivation（8×500 读 race 1×200 写，-race PASS）✓；gate 缝三态 ✓；未知/local/virtual/空 key 四负例 ✓；table-driven ✓；文件按行为命名（票号在文件头）✓。

### 建议改进（non-blocking）

- projection_test.go:270-276 — 并发测试注释宣称断言 "priority rides the row's mark verbatim" 但实际只断言 Key。建议补一行 `proj.PriorityResolution` 属 {canonical:false, flipped:true} 二值之一的断言，使注释与断言对齐（或删注释半句）。
- projection.go:122-134 — store-fault 降级路径（WARN + ok=false）无测试覆盖。需 metadata 故障注入，成本高；至少建议 T-530 落地后由四桶序装配层补一条「投影缺席时本体步仍服务」的集成腿（D2 降级可观察化）。
- projection_test.go — 可补一条并发「读期间删行」竞争腿（现并发测试只有改配置写者）；`ok=false` 即合法结果，断言不 panic 即可。低价值，可不做。
- projection.go:89 — defaultProjectionLayoutRef 双 literal 源的最廉价闭环：T-530 的 `-cache` 建仓防护测试若顺带断言 `remote.CacheSuffix == "-cache"` 字面拼写（projection_test.go:139 已钉），即把两个跨包 literal 的漂移窗口纳入两侧测试网。归 T-530 面登记即可。

### 范围外（不评审，仅标注）

internal/adapter/maven/virtual_metadata.go（T-531 在途）、T-530 的 internal/repo 消费面（validate.go 防护/virtual.go 四段序）——本票仅保证导出面就绪。

### 给 conductor 的 Next

1. **T-530 合并前建议钉两点契约细节**（写进 T-530 的票面即可，无需返工 T-529）：① `ok=false` 语义三因重载（未知 key / 非 remote 型 / gate off / store fault）——四段装配的**成员资格判定必须来自展开序列的行读取**（§2.2），投影调用只做字段装饰（priority/handle* 单源），不得用 ok=false 反推成员缺席；② store-fault 时 cache 步缺席 + 本体步仍在（D2 降级为两桶成员语义，T-529 风险②），建议 T-530 的 walk 测试面盖一条该降级腿。
2. 双审判定：本票改动域 = internal/remote（remote cache 域，落入双审强制域清单的 remote cache），Reviewer A 实例并行评审中——两份独立报告齐后收口。
3. 本评审无范围外新发现（blackout 读取面单源、render 默认表与投影默认的一致性均为顺带验证通过项，无需建票）。
