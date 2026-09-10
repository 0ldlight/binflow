# remote 缓存 v2 设计——marker 门控语义的 Go-native 化（Phase 1 架构族 / L001-4）

- 状态: Proposed（ADR 建议稿见 §7，正式裁定走 DECISIONS.md 流程——本文不写 DECISIONS.md）
- 日期: 2026-09-11
- 作者: architect（L001-4）
- 输入证据:
  - 差分/证据: `reports/compatibility/L000-docker-remote-evidence.md`（E1=反编译走读 7.161.20 partial / E4=运行时实测 :8082 Artifactory-pro 7.161.20 rev 86120900 / E5=双系统差分）、`reports/compatibility/L000-docker-remote-diff.md`（BinFlow 腿 rev 59f33ab5，复验段更新）
  - 差异台账: `docs/compatibility/known-divergence.yaml`（docker/remote-cache-layout〔C05〕、docker/remote-manifest-headers〔C06〕、docker/remote-blob-marker-semantics〔C10〕均 classification: BUG，review_gate 指「LOOP 001 设计票」——即本文）
  - 协议规格: `docs/reverse/protocols/registry.yaml` docker/helmoci 条目（remote 面待补段 + smart remote/federated 关联边界）
  - as-built（只读）: `internal/remote/`（engine/fetcher/cachestate）、`internal/repo/dockerremote.go`（RemoteV2Plane）、`internal/adapter/docker/remote.go`（remoteSessions/blob 代取臂）、`internal/metadata/migrations/sqlite/002_docker.sql`（docker_refs）
- 设计原则（57 节总令 §41，conductor 转述）: dependency inversion / plugin point / service boundary——对齐**等价能力**，不做 class-to-class 翻译；Artifactory 的 `.marker` 文件是其 filestore 树的实现投影，BinFlow 以元数据事实源复刻其**可观测语义**（未拉 manifest 的 digest 不代取、无往返本地快 404）。

---

## 1. 差距基线：两种代取语义

| 维度 | Artifactory（E4/E1，7.161.20） | BinFlow（E5 复验，rev 59f33ab5） |
|---|---|---|
| blob 未命中判定 | **marker 门控**：manifest 下载时 `createManifestMarkers` 为 config/layers 预写 `.marker`；blob GET 顺序 = 空层合成 → 缓存 → `downloadBlobFromMarker`（AQL 查 `<repoKey>-cache` 下 `path matches <image>*` 且 `name == <digest>.marker`）→ 无 marker **立即 BLOB_UNKNOWN**；blob 首取成功后 marker 被真实内容替换（E4-3，E1 `DockerV2RemoteGetBlobHandler#getBlob`） | **digest 盲代理 + 负缓存**：`serveRemoteBlob` 缓存 probe miss → 直接上游代取（`fetchBlobStream` 按 digest）→ 404 则 `CacheRemoteMiss` 负缓存（diff C10） |
| 随机 digest（从未见于 manifest） | 40ms 内本地 404，零上游往返（E4-2） | 上游 404 一次（往返可见）→ 此后本地负缓存 404（C10b） |
| 链内 digest（未缓存但 manifest 已声明） | 上游代取成功、落缓存（E4-1） | 上游代取成功、落缓存（复验 a——语义面SAME） |
| 客户端可见差异 | 首次随机 digest 404 的时延与上游流量；扫描/投毒面（盲代理会把任意 digest 打到上游） | |

结论：差异是**门控语义**级的（known-divergence C10「语义级分歧，牵动缓存架构」），不是布局级的。门控查询键是 digest，与 C05 的缓存树布局（tag 目录/`sha256__` 命名）正交。

## 2. 决策点 D1：门控状态存哪

### 候选方案

- **A) marker 文件逐行翻译**：在 remote 仓缓存命名空间落 `<image>/<digest>.marker` 形态的节点/文件，复刻 Artifactory 文件形态（预写 → 首取替换 → 清理）。
- **B) 复用 `docker_refs` 引用账本 + node 存在性**（推荐）：门控 oracle = 两条既有事实的组合查询——「(repo, image) 的任何 manifest 链是否声明过该 digest」（`docker_refs`）与「该 digest 是否已取回」（node 行存在）。
- **C) 新表 `remote_chain_markers`**：专用门控状态表，manifest 落地时写入、blob 落地后失效。

### 对比

| 维度 | A 文件翻译 | B refs 复用 | C 新表 |
|---|---|---|---|
| 新 schema/写入路径 | marker 生命周期全套新代码 + 污染 node 树（UI/REST 可见垃圾行） | **零**（`RecordRemoteManifest` 已在写 refs，`remoteManifestRefs` 已提取 descriptor digests——`internal/adapter/docker/remote.go` `remoteManifestRefs`） | 一张表 + 双写或迁移 |
| 写入时机与 Artifactory 同构性 | 同构（逐字预写） | **天然同构**：Artifactory marker 预写时机 = manifest 下载完成；BinFlow refs 写入时机 = manifest 落地（`RecordRemoteManifest`，E4-3 机制等价） | 同构但冗余 |
| 替换/删除时机 | 首取后删 marker（`replaceRepoMarkers`/`removeMarkerAsSystem`） | **无需删除**：门控查询顺序 node → refs，node 存在即短路，refs 行自然退位（还保留「被逐出后可重取」的记忆，语义更优） | 需失效逻辑 |
| GC 影响 | marker 节点入引用集（复杂化） | **零**：docker_refs 已在 GC mark 集（002_docker.sql 注释、GC 引用事实 = nodes ∪ docker_refs） | 需并入 mark 集 |
| clean-room 边界 | class-to-class 翻译（filestore 投影复刻）——违反 §41 | 能力等价（门控语义），形态自由 | 能力等价 |

### 决策（建议）

**B**。`docker_refs(repo_key, image, manifest_digest, blob_digest)`（002_docker.sql，PK 含 image）正是 marker 语义的结构化等价物：Artifactory 的 marker 说「该 digest 被本 image 的某个 manifest 链声明过」；refs 行说同一件事，且作用域 (repo_key, image) 与 Artifactory AQL `path matches <image>*` 的 image 前缀作用域等价（见 §2.1 边缘差异）。

### 2.1 B 案的语义边界（可观测差异登记）

1. **refs 记录是 best-effort**（`landFetchedManifest`：record 失败 WARN 后继续服务）。门控接上后，record 失败 ⇒ 后续 blob GET 本地 404 ⇒ 真实 pull 卡在 blob 段。这与 Artifactory 的 marker 写失败耦合**等价**（其 marker 写失败同样使代取不可用），但 BinFlow 应把该 WARN 升级为可观测（log 字段对齐既有 `cache_result` 行）——实现票注意项，不改 best-effort 契约。
2. **image 作用域精确匹配 vs 前缀匹配**：Artifactory AQL `path matches <image>*` 是前缀匹配（镜像名互为前缀时存在跨链误放行瑕疵）；BinFlow `(repo_key, image)` 精确匹配更严。差异仅出现在「镜像 A 名是镜像 B 的前缀且 digest 只在 B 链内」的构造性场景，方向保守（拒绝代取）。登记 INTENTIONAL 候选（随 ADR 一并定谳，§7）。
3. **索引**：现索引 `idx_docker_refs_blob(blob_digest)` 单列。门控查询谓词 `(repo_key, image, blob_digest)` 走单列索引回行过滤足够（同 digest 跨 image 引用稀少）；实现票以 EXPLAIN QUERY PLAN 实测，必要时补 `(repo_key, image, blob_digest)` 复合索引（双方言迁移）。
4. **级联链**：OCI index → 子 manifest → blob 的级联在既有流程中天然成立（子 manifest GET 走 `serveRemoteManifest` by-digest → 落地 → 记录其 refs）。referrers/subject 面不在本期（registry.yaml docker 条目 `docker.referrers.*` 属性面待逆向）。

## 3. 决策点 D2：门控接口位置与跨协议复用面

### 接口位置

门控 oracle 需要 metadata 查询，**不是**路径纯函数 ⇒ 不进 `adapter.MetadataProvider`（纯函数 SPI，`internal/adapter/metadata.go` 契约）。放 repo 层，作为 `RemoteV2Plane` 体系的 optional facet（capability discovery by type assertion——先例：`MultipartUploads`/`SessionSweeper`/`V2VirtualPlane`）：

```go
// internal/repo（建议稿；正式签名归实现票，经 go build 验证）
// DigestChainGate is the marker-gate oracle of remote-cache-v2: may a blob
// digest be fetched upstream for this image? Artifactory's
// downloadBlobFromMarker semantics (E4-3, E1) answered from the docker_refs
// ledger instead of marker files.
type DigestChainGate interface {
	// BlobInChain reports whether hex was named by any manifest chain
	// recorded for (repoKey, image). Read-gated like the plane's own faces.
	BlobInChain(ctx context.Context, p *Principal, repoKey, image, hex string) (bool, error)
}
```

adapter 消费点（`serveRemoteBlob`，`internal/adapter/docker/remote.go`）：probe miss 且无 standing 副本 ⇒ 查 gate ⇒ `false` → 本地 `BLOB_UNKNOWN`（走既有 `blobUnfound` 臂，自动继承 C13 detail 键修复）且**不写负缓存行**（门控应答是确定性的，无需 TTL 记忆）；`true` → 既有上游代取臂不变（404 时负缓存保留，记忆「链内声明但上游没有」的异常）。

### 跨协议复用面

- **docker + helmoci 先行**：helmoci 复用 docker v2 全套 REST（registry.yaml helmoci 条目「remote: 同 docker remote 代理」），同一 adapter 栈 ⇒ 门控天然覆盖，实现票确认 helmoci remote 臂与 virtual 臂（`remoteManifestSink` 的 V2VirtualPlane 落仓目标）都过同一 gate 查询（virtual walk 按**成员** repoKey+image 查询，与 ADR-0013「virtual miss 透传探索」无冲突——门控只拒绝从未见于链的 digest，不产生缓存副作用）。
- **maven/npm/pypi/generic/conan/deb/rpm/…不引入**：这些协议是**路径寻址**（客户端请求的路径就是元数据命名的路径），不存在「凭 digest 探测未命名资源」的协议面；盲代理问题（把任意 digest 打上游）是 registry 协议 digest 寻址特有的。泛化 = 为想象买单（§41 service boundary：门控是 digest-addressed 协议闭集 {docker, helmoci} 的面，不是 remote 缓存层的普适机制）。
- **blob streaming 直代面**（Artifactory `docker.remote.blob.streaming.enabled` 默认 false，E4-3）：BinFlow 不做流式直代（对齐默认姿态），UNSUPPORTED 登记。

## 4. 决策点 D3：负缓存裁剪

证据：Artifactory 侧 `missedRetrievalCachePeriodSecs=1800` 配置在场，但 docker manifest 404 面重复请求仍每次 6.6s 上游往返——**运行时未见负缓存生效**（E6-3/E3-4，E4）；BinFlow 侧负缓存生效（重复 miss 40-70ms 本地，diff C14 DIVERGENT）。

### 候选方案

- A) **对齐删除**：docker 面去掉负缓存，复刻「配置在场但不生效」的运行时状态。
- B) **保留 + INTENTIONAL 登记**（推荐）：行为保留，known-divergence C14 转 INTENTIONAL_DIFFERENCE，authority = ADR（§7 草案含此条）。
- C) 分面裁剪：blob 面保留、manifest 面删除。

### 决策（建议）

**B**。理由：① 客户端可见语义同为 404，唯一差异是时延与上游流量——负缓存是 `missedRetrievalCachePeriodSecs` 配置**承诺**的行为（配置键在两侧同名同默认），Artifactory 是实现未兑现；② 负缓存保护上游免受重复 miss 风暴（恰是 docker hub 限流场景，E6-4 同域）；③ 门控落地后 blob 面负缓存自然退居异常记忆（仅「链内声明但上游 404」的罕见路径），manifest 面负缓存独立成立，无需 C 的分面复杂度。

**前提义务**：① INTENTIONAL 必须 ADR authority（known-divergence 规则）——随 §7 草案定谳；② Artifactory 侧跨 missedTTL 窗（>1800s）复测一次（差分票附腿）排除窗口内观察误差——若复测翻转（有负缓存），C14 按对齐收、本条自动作废。

## 5. 决策点 D4：检索窗默认与 304 语义

### 5.1 docker remote 检索窗默认 21600s

Artifactory：docker remote 检索缓存默认 **21600s（6h）**（`RepoConfigDefaultValues.DEFAULT_DOCKER_REMOTE_RETRIEVAL_CACHE_PERIOD`，E3-4 E1），generic 为 7200s。BinFlow：`remote_configs.content_ttl_seconds` 建仓写 7200（显式值，`internal/repo/service.go` 产品默认），无包型差异。

**决策（建议）**：per-package-type 默认——docker/helmoci remote 的内容类 TTL 默认 21600s，其余包型维持 7200s；仅在「未显式设置」时生效。存量行已是显式 7200，**不回改**（7200 是合法显式值；回改需要「显式 vs 默认」区分列，不值得）。新建 docker/helmoci remote 建仓默认取包型默认。

**落点**：单点解析 + 双消费同步——`loadRemoteV2Repo`（repo facts）与 engine `loadRepo`（generic Fetch）两处消费同一解析函数（先例：`effectiveV2SocketTimeoutMs` 的「one order, two consumers」注释，`internal/repo/dockerremote.go` `effectiveV2SocketTimeoutMs`）。

**冲突登记**：ADR-0012 勘误二 ④ 写死「`retrievalCachePeriodSecs` 默认 7200」不分包型 ⇒ 新 ADR（§7）显式引用修订该句（ADR-0012 本体不动，按 Errata 协议在新 ADR 留痕）。

### 5.2 客户端 If-None-Match：不消费，恒 200

两侧已一致（Artifactory E3-2 E4：带/不带引号、GET/HEAD 一律 200 全量；BinFlow E5 复验 C08 SAME）。**钉死为回归锚**，随契约票冻结进 `docs/compatibility/contracts/`（compatibility-engineer 域）；实现票禁止「顺手优化」成 304。

### 5.3 上游条件 GET 与 304 透传

Artifactory：用自己的 ETag 对上游条件 GET，**上游回 304 才向客户端回 304**（`returnResponseGettingManifest`，E3-3 E1「待动态复证」；客户端 INM 与该判定无关）。BinFlow：不发现条件头（fetcher.go 注释「HEAD+validators negotiation is P1」；docker `fetchManifest` 无 INM；`mapUpstreamStatus` 只有 unsolicited-304 臂）。

**决策（建议）**：实现上游条件再验证——ADR-0012 决策 1 早已定「TTL + 条件再验证」框架（validators 已存 `remote_cache.etag/last_modified` 列），docker 面是补接线不是新面：tag 路径 manifest 再验证臂带 `If-None-Match: <存档 ETag>`，上游 304 → 刷新时钟 + **客户端 304**（附 `Docker-Distribution-Api-Version: registry/2.0` 头，E3-3）；200 → 全量替换（同 digest 幂等覆盖）。blob 面 digest 不可变，维持「checksum 命中永不再验」（ADR-0012 决策 1，无冲突——检索窗只作用于 tag→digest 解析与未命中探索，不作用于已命中的 digest 内容）。generic engine 的条件再验证腿（maven/npm metadata 面）与 docker 面同构，建议分票：docker 面先行（真实客户端可证），generic 腿随后。

## 6. 与既有 ADR / 台账对账

| # | 关联 | 结论 |
|---|---|---|
| 1 | ADR-0012 决策 1（TTL+条件再验证；checksum 命中永不再验） | 无冲突；§5.3 是其 docker 面接线。digest 寻址内容不可变 ⇒ 命中不验维持；检索窗只管解析与探索 |
| 2 | ADR-0012 勘误二 ④（默认 7200） | **冲突**：per-package-type 默认需修订该句 → 新 ADR 显式引用（§5.1） |
| 3 | ADR-0012 勘误一（负缓存 + expired-but-serving） | 无冲突；C14 INTENTIONAL 只裁「Artifactory 侧未生效」这一差分方向 |
| 4 | ADR-0013（virtual 解析、miss 不落盘） | 无冲突；gate 是纯读 oracle，virtual 臂按成员 (repoKey, image) 查询，不产生缓存副作用（§3） |
| 5 | ADR-0006/0002（元数据事实源、模块边界） | B 案即其执行：门控状态进元数据账本，不落影子文件 |
| 6 | known-divergence C10（BUG） | 本设计供实现票修复后转 FIXED；authority 链 = 本设计 + ADR |
| 7 | known-divergence C05（BUG：tag 目录/marker/library 归一） | **拆解**：(a) `library/` 归一是协议行为（`DockerUtil.adjustDockerRepo`，E2-6/E6-1——错误 detail 泄漏归一化结果）→ 随错误形态票修；(b) 缓存树布局（tag 目录/`list.manifest.json`/`sha256__` 命名 vs BinFlow digest 寻址）是 filestore 投影——**单独立裁**（倾向 INTENTIONAL：digest 寻址是 BinFlow 存储模型〔ADR-0006/0019〕，tag 目录是 Artifactory 实现产物投影；本设计门控键=digest 与布局正交，不预支结论） |
| 8 | known-divergence C06（manifest 头集：Etag/Last-Modified/Origin-Remote-Path） | 邻接但独立——头集补齐归错误形态/头集票；§5.3 的 304 臂依赖 ETag 服务化，实现票注意时序（先头集、后 304，或同票） |
| 9 | smart remote / federated（registry.yaml docker 条目 remote 段待补；evidence §9 UNKNOWN） | **域外**：smart remote（event-based 复制、`/api/npm/` 剥离）属 replication 域（ADR-0021）与 repo 模型重构（gap 总账 repo-domain REFACTOR federated），不进缓存层设计；缓存 v2 只管 pull-through 代理语义 |

## 7. ADR 草案位（建议稿——正式 ADR 归 DECISIONS.md 流程）

> **ADR-00XX: remote 缓存 v2——digest 链门控（marker 语义 Go-native 化）、负缓存 INTENTIONAL、docker 检索窗 21600s、上游 304 透传**
>
> - 状态: Proposed（本建议稿；编号由 conductor/DECISIONS.md 流程分配）
> - 背景: LOOP 000 差分 C10/C14 证据（E1/E4/E5，7.161.20 vs rev 59f33ab5）——BinFlow digest 盲代理+负缓存 vs Artifactory marker 门控；known-divergence C10 BUG 待设计输入。
> - 候选方案: 门控状态 A 文件翻译 / B docker_refs 复用 / C 新表（§2）；负缓存 A 删除对齐 / B 保留 INTENTIONAL / C 分面（§4）；检索窗 A 全局 7200 / B 包型默认（§5.1）。
> - 决策: **B/B/B** 四条——① 门控 oracle = `(repo_key, image)` 作用域的 docker_refs 查询 + node 存在性短路（`repo.DigestChainGate` optional facet），无 marker 文件、零新 schema；无链成员 digest 本地 BLOB_UNKNOWN 零上游往返、零负缓存行；② 负缓存保留，C14 转 INTENTIONAL（authority=本 ADR），附 Artifactory 跨 missedTTL 复测义务；③ docker/helmoci remote 内容 TTL 默认 21600s（未显式设置时），显式值优先、存量不回改；修订 ADR-0012 勘误二④的「默认 7200」为包型默认；④ tag manifest 再验证臂发上游条件 GET（存档 ETag），上游 304 → 客户端 304；客户端 INM 恒不消费（契约冻结）。
> - 理由: refs 账本与 marker 语义写入时机天然同构且零新状态（§2 对比表）；负缓存是配置承诺行为且客户端可见语义不变（§4）；21600s 是 Artifactory 包型默认实测值（E3-4）；条件再验证是 ADR-0012 既有框架的 docker 面补线（§5.3）。
> - 后果: `internal/repo` 增 `DigestChainGate` facet + docker_refs 门控查询（索引按 EXPLAIN 实测补）；`internal/adapter/docker` blob 代取臂接 gate（helmoci/virtual 臂同步）；TTL 解析单点化双消费；known-divergence C10→FIXED、C14→INTENTIONAL、C08/C17 契约化。**验证载体（真实客户端）**：dind docker pull 重放（链内代取/随机 digest 快 404/二次 pull HIT）、curl INM 恒 200、可控上游 registry:3 改 tag 观察窗过期与新 digest、上游 304 → 客户端 304（差分票 C10/C07/C08/C14 重放协议，evidence §10）。

## 8. 实现票拆分建议（接口级，不写实现）

| 票 | 内容 | 优先级 | area（agent-graph owns） |
|---|---|---|---|
| A | `DigestChainGate` facet + docker_refs 门控查询（含 EXPLAIN/索引评估）+ `serveRemoteBlob` 接线（direct/virtual/helmoci 三臂）+ 门控 404 不写负缓存 | P0（C10 修复本体） | internal/repo + internal/adapter/docker |
| B | 包型检索窗默认 21600s：单点解析函数 + 建仓默认 + 双消费同步 | P1 | internal/remote + internal/repo |
| C | 上游条件 GET + 304 透传（docker manifest 再验证臂；C06 头集票先行或同票） | P1 | internal/adapter/docker |
| D | Invalidate/清理联动：全仓 Invalidate 连带删 refs（metadata 需 `DeleteRefsByImage`/前缀面）；unused-cleanup 不删 refs（保留重取记忆）的语义注释 | P2 | internal/metadata + internal/repo |
| E | 契约票：C08（INM 恒 200）、C10（门控语义）、C17（空 blob 合成金样）冻结进 contracts/；C14 复测附腿（Artifactory 跨 missedTTL 窗） | P1 | docs/compatibility（compatibility-engineer） |

## 9. 验证锚（差分重放即验收）

1. **C10 门控语义**：拉 manifest 后、blob 未缓存时——链内 digest 上游代取 200（上游日志有往返）；随机合法 digest **40ms 级本地 404、上游日志零往返**（E4-2 形态）。
2. **C07 缓存快路径**：rmi 后重拉 HIT（上游计数冻结）。
3. **C08 INM**：带/不带引号、GET/HEAD 恒 200 全量（除 §5.3 上游 304 透传臂外——透传臂触发条件是上游 304，与客户端 INM 无关，两断言并存不矛盾）。
4. **C14 负缓存**：重复 miss 本地快 404（INTENTIONAL 生效面）。
5. **检索窗**：可控上游改 tag → 窗内 pull 仍旧 digest、窗过期后新 digest；上游 304 → 客户端 304。
6. 真实客户端：dind docker CLI 27.x（既有差分骨架，evidence §0.1 复用命令骨架）。
