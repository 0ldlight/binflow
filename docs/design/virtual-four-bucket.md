# virtual 仓四桶解析序设计（两桶 → 四桶）

> T-520（P0）设计稿。ADR 面 = ADR-0051（本仓 DECISIONS.md，附带 ADR-0013 联动记录① 勘误与 ADR-0012 引用面注记）。
> 权威行为规格 = `docs/reverse/virtual-resolution.md`（T-516 产出，7.161.24 反编译单源，置信度随行标注）；投影派生语义 = `docs/reverse/remote-cache-projection.md` §1。
> 本文只依行为规格与官方协议文档（clean-room，ADR-0001），不含任何反编译结构引用。

## 0. 范围

**在范围内**：

1. 解析序装配算法：成员声明序展开（嵌套 virtual DFS + 环剪断 + key 去重 + 缺失静默丢弃）→ 按类型分桶 → priorityResolution 四段拼接（§1/§2）。
2. cache facet 读取语义：零上游 standing-copy 命中（§2 的「cache 仓进 local 桶」在 BinFlow 的等价实现）。
3. 嵌套 virtual 解禁（配置校验放宽 + 运行时展开）。
4. `<K>-cache` 投影派生注册表（internal/remote 域，继承字段单源）。
5. `VirtualMemberOrder` seam 扩展（adapter 消费面向后兼容）。

**不在范围内**（后续缝，逐项登记在 §9）：`GET /<K>-cache/<path>` 直访、§7.2 REST 存储面 locals→caches 序、§7.3 搜索域 cache key 映射、§7.4 统计路径重写、zap、UI 树 cached 分组、§3 深水语义（非精确候选回退/403 透传/`[RELEASE]` 跨成员收集）、§4 patterns 三层过滤、远端抑制 header（`artifactoryRequestsCanRetrieveRemoteArtifacts` 配置面）。

## 1. 现状（as-built，2026-09-25）

| 现状 | 位置 | 与四桶的差距 |
|---|---|---|
| 两桶序：优先桶（`priorityResolution=true` 成员，声明序）→ 其余桶 | `internal/repo/virtual.go:100-145`（`virtualMemberOrder`） | 桶内 local/remote 混排；无 cache/本体分步 |
| remote 成员探测 = FR-20 全链一步（cache 命中/回源/负缓存合并） | `internal/repo/virtual.go:424-462`（`probeRemoteMember`） | cache 命中与回源命中不可区分，无法表达 §3.6/§5.1 的「跳过 cache 仓」 |
| 缓存行落 remote 仓自身命名空间 | `internal/remote/fetcher.go:77`（注释）、`:938`（`Nodes().Put`） | 无 `<K>-cache` 实体；本设计维持不动（逻辑投影） |
| 嵌套 virtual 配置期拒绝 | `internal/repo/service.go:2407-2438`（`validateVirtualMembers`）、`virtual.go:134-139`（运行时 drift 分支） | §1 要求嵌套展开（DFS + visitedKeys） |
| seam：`VirtualMember{Key,Type,Priority}` | `internal/repo/api.go:506-512`；消费方 ~15 个 adapter 文件（maven/npm/pypi/cargo/goproxy/deb/nuget×5/conan/rpm/helm） + `operations.go:521`、`dockervirtual.go:62/595`、`remoteexternal.go:66` | 需增 facet 维度（向后兼容加字段） |
| `storeArtifactsLocally` 仅 wire 默认值（恒 true），engine 无不落盘分支 | `internal/httpapi/repo_config_render.go:219/330` | 投影 gate 位留缝，当前恒投影 |

## 2. 数据结构与装配算法

### 2.1 类型（Go 形态；已独立编译验证，见工作日志 Commands）

```go
// Facet distinguishes the two resolution entities a remote member
// contributes (spec virtual-resolution.md section 1, "mixed members").
type Facet uint8

const (
	FacetPlain Facet = iota // local members, and the remote itself
	FacetCache              // the remote's <key>-cache projection
)

// ResolutionStep is one entry of the four-bucket order.
type ResolutionStep struct {
	Key      string // member repo key (cache facets carry the REMOTE key)
	Type     string // TypeLocal | TypeRemote
	Facet    Facet  // cache facets: TypeRemote + FacetCache
	Priority bool   // priorityResolution; cache facets inherit the remote's
}
```

### 2.2 展开算法（§1）

输入 = `metadata.Virtual().ListMembers` 的声明序 key 列表 + 逐 key 行解析（`Repos().Get`）：

1. `visitedKeys` 以**根 virtual key 预置**（自环在配置层已被 `validateVirtualMembers` 的 self 检查拒绝，此处兜底）。
2. 依声明序逐 key：已见 → 跳过（**key 去重，首现位次生效**——§1「重复成员」行）；行不存在 → warn 日志 + **静默丢弃**（§1「不存在的成员」行）；行存在 → 登记 key，virtual 行则**先登记后递归其子成员**（直接成员先于递归子树 = 声明序 DFS，§1「嵌套 virtual 展开」行），local/remote 行进展开序列。
3. 环引用由 `visitedKeys.contains` 提前返回剪断（§1「嵌套层级上限」行：深度默认无限，环是唯一终止条件）。
4. 展开结果**按类型分桶收集**：全部 local 类（local 成员，含嵌套带来的，按遇到序）与全部 remote 成员两个子序列——**locals 恒排在 remotes 之前**，无论声明序如何穿插（§1）。

### 2.3 四段拼接（§2）

```
段1 [优先 local + 优先 remote 的 cache 投影]（real-local 先，cache 投影后，各按声明/遇到序）
段2 [优先 remote 本体]（声明序）
段3 [非优先 local + 非优先 remote 的 cache 投影]（同段1 规则）
段4 [非优先 remote 本体]（声明序）
```

- 每个优先级类内三遍扫描展开序列：real-local → cache 投影 → remote 本体。**段内 real-local 先于 cache 投影**是 §9.1 的两段代码合成推演序（规格自身列为开放验证项，差分推翻时仅动段内两类次序，机制轴零翻动——见 §8 开放项 O1）。
- cache 投影的 `Priority` = **投影继承**：跟随其 remote 的 `priorityResolution`（§2「桶划分」行 + remote-cache-projection.md §1.2），单源自 §4 的投影注册表读取。
- 远端抑制 gate：`artifactoryRequestsCanRetrieveRemoteArtifacts=false` 且请求带对端 Artifactory 标识 header → remote 本体不入序、cache 投影保留（§2「远端抑制」行）。BinFlow 该配置面未实现——算法保留 gate 位，配置面落地前恒不抑制（§9 缝 F7）。
- 复杂度：每请求 O(V+E)（V=涉及 virtual 数、E=成员边数）重算，**无解析缓存**——与现状 FR-15-AC6（成员/priority 变更下一请求可见）契约一致，不引入新状态。

### 2.4 段序对两桶的可观测差异（动机摘要）

两桶已表达 priority 越位（优先桶整体在前）。四段序新增三点：

| # | 新语义 | 规格依据 |
|---|---|---|
| D1 | 同优先级内 local 类（含 cache 投影）恒先于 remote 本体——两桶桶内为声明序混排 | §1 分桶收集 + §2「cache 与 remote 不相邻」 |
| D2 | cache 副本命中与 remote 本体回源命中成为**两个独立解析步**——过期 standing copy 在 cache 步被服务、上游零接触；Maven 快照路径与 metadata 合并可精确「跳过 cache 仓」 | §2 四段结构 + §3.6/§5.1 |
| D3 | 嵌套 virtual 解禁（DFS 展开） | §1 |

BIN-M1（Maven 试点）依赖 D2：`maven-metadata.xml` 合并按四段序逐成员且**跳过全部 cache 仓**（§5.1）、快照解析跳过 cache 仓（§3.6）——两桶的合并步无法区分「该 remote 的缓存副本」与「该 remote 的上游文档」，同一 remote 会被双读且跳过规则无处挂。

## 3. cache facet 读取语义

cache facet 是解析层对「`<K>-cache` 投影仓参与 local 桶」的等价实现——**节点存储零迁移**（缓存行仍在 remote key 命名空间，ADR-0012 落盘协议不动）：

1. **命中判定**：remote key 命名空间下该 path 存在文件行且 blob 可开 → 命中；否则 miss。等价于对 remote key 复用 `probeLocalMember`（`virtual.go:242`）。
2. **零上游**：cache 步不触发 FR-20 链的回源/再验证/负缓存读写——探索性 miss 无残留（ADR-0013）在 cache 步天然成立（它不写任何东西）。
3. **过期副本亦服务**：cache 步不检查检索窗。依据 = §2 结构（cache 投影是 local 桶参与者，local 桶参与者无再验证机械）与 §3.8（virtual 缓存裁决以 lastModified 比较、无新鲜度语言）；§2「远端抑制」行（remote 本体被禁后 cache 投影仍参与）同向佐证。**此条是本设计最高风险的语义断言**，预登记差分项 O2（§8）。
4. **负缓存行不影响 cache 步**：负缓存是 remote 本体的回源机械。并存态（新鲜负缓存 + 过期 standing copy）经 virtual 的表现 = cache 步服务 standing copy，与直读 remote 面（负缓存期内 404）不同——差分项 O3（§8）。
5. **跳过规则**（§3.4/§3.6）：路径可解析出 release 模块信息且投影继承 `handleReleases=false` → 跳过该 cache 步；Maven 快照路径/`[INTEGRATION]` → **跳过全部 cache 步**（walk 层统一实施，协议无关同一代码路径）。
6. remote 本体 facet（段2/段4）= 既有 `probeRemoteMember` FR-20 全链原样（cache miss 后到达时才回源；stale 服务、403 透传、硬失败传播全部维持）。

## 4. 投影注册表（dev-go-storage 域）

`internal/remote` 新增导出面（名称实现票定，形态如下）：

```go
// CacheProjection is the derived <key>-cache repository descriptor of one
// remote repository (remote-cache-projection.md section 1): a resolution-
// layer view, NOT a persisted repository row and NOT a storage namespace.
type CacheProjection struct {
	Key                string // remoteKey + "-cache"
	PackageType        string // mirrors the remote
	RepoLayout         string // mirrors the remote
	PriorityResolution bool   // inherited (section 1.2)
	HandleReleases     bool   // inherited
	HandleSnapshots    bool   // inherited
	ArchiveBrowsing    bool   // inherited
	BlackedOut         bool   // inherited
}

// ProjectionRegistry derives cache projections off the loaded remote rows
// (rebuilt on config reload — the projection is never a stored entity).
type ProjectionRegistry interface {
	CacheProjection(ctx context.Context, remoteKey string) (CacheProjection, bool)
}
```

- 派生规则与固定值（checksum 策略恒 client 校验、快照行为恒 unique）按 remote-cache-projection.md §1.2 表逐项落注释；`-cache` 后缀拼接常量在此包单源导出。
- gate：`storeArtifactsLocally=false` → 无投影（当前 BinFlow 恒落盘，gate 位留缝不生效）。
- 消费方：`internal/repo` 的序装配（继承 priority 与 handle* 单源）；`-cache` 后缀建仓防护（§1.3：建/改任何 `-cache` 后缀 key → 400）落 `internal/repo/validate.go`（跨 owns 协作点，见 §5）。
- **明确不做**：投影不进 `repositories` 表、不进 `GET /api/repositories` 列表、不改节点/bloom/配额任何存储面。

## 5. 实现拆分（按 agent-graph owns）

### 5.1 dev-go-core（owns internal/repo、internal/metadata）

| 文件 | 改动 |
|---|---|
| `internal/repo/virtual.go:100-145` | `virtualMemberOrder` 重写为 §2 展开 + 四段拼接两函数；cache 步的 priority/handle* 从投影注册表读 |
| `internal/repo/virtual.go:194-237` | `getVirtual` walk 增 facet 分支：cache 步 = 对 remote key 复用 `probeLocalMember`；本体步 = 既有 `probeRemoteMember`；快照路径过滤 cache 步（§3.6） |
| `internal/repo/virtual.go:134-139` | drift 分支（成员漂移为 virtual）从「warn 跳过」改为「递归展开」 |
| `internal/repo/api.go:506-512` | `VirtualMember` 增 `Facet` 字段 + `VirtualMemberOrder` 契约注释翻新（两桶→四段）；`Priority` 语义不变 |
| `internal/repo/service.go:2407-2438` | `validateVirtualMembers` 解禁嵌套 virtual 成员（保留 self-list/重复/缺失/trash/defaultDeploymentRepo-local 全部既有检查；**不做**配置级环全图检测——运行时 `visitedKeys` 剪断即终态，配置级检测在成员可动态变更下不可靠） |
| `internal/repo/validate.go` | `-cache` 后缀 key 建仓防护（消费 storage 域导出的常量） |
| `internal/repo/operations.go:521`、`dockervirtual.go:62/595`、`remoteexternal.go:66` | 消费序跟进（`resolveVirtualSource` 的 file/folder 首现规则对 facet 不敏感——cache 步与本体步查同一命名空间） |
| `internal/repo/virtual_test.go`、`virtual_aggregate_test.go` | 两桶断言翻新为 §8 不变量（表驱动） |

`internal/metadata`：**零 schema 变化**——`virtual_members.position` 即声明序，嵌套成员就是 virtual key 的普通成员行。

### 5.2 dev-go-storage（owns internal/storage、internal/remote）

| 文件 | 改动 |
|---|---|
| `internal/remote/`（新文件，如 `projection.go`） | §4 注册表 + 描述符派生 + `-cache` 常量单源 |
| `internal/remote/fetcher.go` | **零改动**（落盘/再验证/负缓存协议不动） |

### 5.3 dev-go-adapter（owns internal/adapter；消费面，随实现票或后续票）

| 消费方 | 必要动作 | 依据 |
|---|---|---|
| `internal/adapter/maven/virtual_metadata.go` | 遍历序消费 `Facet`：跳过 `FacetCache` 步（remote 本体自带缓存语义）；快照级 metadata 另跳 `handleSnapshots=false` | §5.1 |
| `internal/adapter/npm/virtual_packument.go` | 缓存仓去重：序列含任一 remote 本体 → 滤掉全部 cache 投影（防同仓双查） | §6 |
| pypi/cargo/goproxy/deb/nuget×5/conan/rpm/helm 各 `virtual*.go` | 逐个核对：聚合遍历遇 `FacetCache` 步的处理（多数应跳过——协议聚合的成员语义以 remote 本体为单元） | §6 同源规则 |

**兼容承诺**：`VirtualMember` 加字段是结构体字段追加，既有 `Key/Type/Priority` 读取方零破坏；未处理 `Facet` 的 adapter 行为 = cache 步按普通 remote 成员走 `ReadVirtualMember`（FR-20 全链）——即退回两桶时代的成员语义，无错误放大。

## 6. Maven 语义耦合点

| 耦合点 | 归属 | 说明 |
|---|---|---|
| §3.6 快照路径跳过全部 cache 仓 | repo walk 层（dev-go-core） | 协议无关、同一代码路径；`handleSnapshots=false` 成员跳过同样作用（继承值经投影） |
| §5.1 metadata 合并跳过全部 cache 仓 | maven adapter | foundByPriority 短路语义不变，只是遍历序换四段 + cache 过滤 |
| §5.2 pom 清洗 | 无耦合 | 下载侧变换，与解析序无关（`pomRepositoryReferencesCleanupPolicy` 维持） |
| §5.4 路径翻译 | 不在本票 | virtual 布局→成员布局翻译缝维持现状；嵌套 + 混布局组合的翻译正确性随嵌套解禁列为差分关注项（O4） |

## 7. 兼容风险与迁移

**不设 feature flag**（对齐 ADR-0050 决策 6「无双轨期」先例：BinFlow 无外部存量承诺，双轨 = 自造差异）。单窗口切换，差分腿覆盖验证。

风险清单：

| # | 风险 | 缓解 |
|---|---|---|
| R1 | 过期副本经 virtual 从「触发再验证」变「cache 步直接服务」（D2）——与参照的实测差 | O2 差分腿优先覆盖；Errata 触发条件预登记（ADR-0051） |
| R2 | 15 个 adapter 消费方面未及时处理 `Facet` | 兼容承诺（§5.3）：未处理 = 退回成员级语义，无放大；maven/npm 两个必要动作随票 |
| R3 | browse 聚合（`listVirtual`/`getVirtualFolder`）同路径归属成员变化 | cache 步与本体步读同一命名空间，union 集合不变；仅 display 归属次序变（差分关注项 O5） |
| R4 | 嵌套解禁后环/深链导致解析放大 | visitedKeys 剪断（环）；深度上限留缝不设（对齐 §1「默认无限」）；每请求重算无缓存放大 |
| R5 | 负缓存并存态（O3）与直读面行为分叉 | 差分腿登记；如参照实测不同走 Errata |
| R6 | §4 patterns 三层过滤未实现（virtual 自身 patterns 面现状缺失） | 不在本票扩面；嵌套子树的 patterns 剪枝（§4 ②）依赖该面，登记为后续缝 F6——嵌套解禁先不实现 patterns 剪枝（默认 `**/*` 恒过，与现状一致） |

## 8. 验收口径（可测试不变量）

| # | 不变量 | 锚 |
|---|---|---|
| I1 | 同优先级内，local 类实体（real-local + cache 投影）恒先于 remote 本体 | §1/§2 |
| I2 | cache 投影恒排在其本体之前（不同段） | §2「cache 与 remote 不相邻」 |
| I3 | 跨段严格序：P-local → P-remote → NP-local → NP-remote | §2 四段拼接 |
| I4 | 段内 real-local 先于 cache 投影（两类各按声明/遇到序） | §9.1 推演（开放项 O1） |
| I5 | 重复声明（同层/嵌套跨层）首现位次生效，展开序列 key 唯一 | §1「重复成员」 |
| I6 | 环引用剪断（A→B→A 终止；根自环配置层拒绝） | §1「嵌套层级上限」 |
| I7 | 缺失成员静默丢弃 + warn，其余成员不受影响 | §1「不存在的成员」 |
| I8 | 嵌套展开序 = 声明序 DFS（直接成员先登记、后递归子成员） | §1「嵌套 virtual 展开」 |
| I9 | cache 步零上游：纯 cache 命中路径成员上游计数零增量 | §2/§3（探针断言） |
| I10 | Maven 快照路径 walk 不触 cache 步 | §3.6 |
| I11 | metadata 合并遍历不含 cache 步（foundByPriority 语义维持） | §5.1 |
| I12 | 每请求重算：成员/priority 变更下一请求立即可见 | FR-15-AC6 回归 |

真实客户端验证载体：mvn CLI（快照解析 + metadata 合并）与 curl 双发差分腿，重点臂 = O1（穿插声明序）/ O2（cache 过期副本）/ O3（负缓存并存）。

**开放差分项**（Errata 触发条件，ADR-0051 软缝节同款登记）：

- O1：段内 real-local 与 cache 投影的相对次序（§9.1 推演 vs 声明位次穿插）。
- O2：cache 步对过期 standing copy 的无条件服务（§3 结构读法）。
- O3：新鲜负缓存 + 过期 standing copy 并存时经 virtual 的表现。
- O4：嵌套 + 混布局的路径翻译正确性（§3.3/§5.4 面）。
- O5：browse 聚合 display 归属次序（§7.1 的 local 覆盖 remote 展示值未在本票实现，随 browse 面后续票）。

## 9. 后续缝（不实现，留接口）

| # | 缝 | 依据 |
|---|---|---|
| F1 | `GET /<K>-cache/<path>` 直访 + 投影 key 可寻址 | remote-cache-projection.md §2.1 |
| F2 | REST 存储面 locals→caches 取序 | §7.2 |
| F3 | 搜索域 remote→cache key 映射 | §7.3 |
| F4 | 统计路径重写 `<remote>-cache`（含 virtual 永不做统计主体） | §7.4 |
| F5 | zap / 聚合缓存 `<virtual>-cache` 投影 | remote-cache-projection.md §5、§6 |
| F6 | virtual patterns 三层过滤（§4 ①②③） | §4 |
| F7 | 远端抑制 header + `artifactoryRequestsCanRetrieveRemoteArtifacts` 配置面（算法 gate 位已留） | §2「远端抑制」 |
| F8 | §3 深水语义（非精确候选回退、403 透传、`[RELEASE]` 跨成员、virtual 缓存裁决/删除联动） | §3.4-3.9 |
