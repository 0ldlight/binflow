# Virtual 仓解析顺序行为规格

> 源版本：7.161.24 反编译（部分 7.161.20）；模块内嵌 pom 多标 7.161.14（发行包版本以 README 为准 7.161.24）。
> 本文是 `repo-semantics.md` §8（virtual 语义）的**深化增量**：四桶序的精确装配、过滤时机、Maven/npm 特有链路、聚合与统计口径。写路由（PUT/405）与 `defaultDeploymentRepo` 见彼处 §8.2，不重复。
> 置信度：`高` = 反编译代码可逐分支复读；`中` = 仅代码单源、调用链未全展开；`低` = 推断，待动态验证。
> 证据锚统一写法：`模块名 相对路径`（相对 `/Users/lzw/workspace/artifactory-decompiled/backend/`）。

## 1. 成员解析基线（成员序可配性与去重）

| 规则 | 行为规格 | 置信度 | 证据锚 |
|---|---|---|---|
| 成员序来源 | virtual 配置的成员数组（`repositories` 字段）**按声明顺序**就是基准序；成员顺序完全由客户端配置决定（无自动排序） | 高 | `artifactory-core org/artifactory/repo/service/RepositoryConfigServiceImpl.java` getRepoConfigsByKeysInSpecificOrder（按 repositoryRefs 声明序重排） |
| 重复成员 | 当客户端在同一 virtual 的成员表里重复声明同一仓库，服务端按 key 去重（首次出现位置生效，第二次忽略）——**重复声明不会造成重复搜索或重复计入**；嵌套展开时跨层重复同样去重 | 高 | 同上（LinkedHashSet 构造）+ `artifactory-core org/artifactory/repo/virtual/VirtualRepo.java` addIfNewByKey + VirtualConfigResolverHelper.resolve 的 visitedKeys |
| 不存在的成员 | 当成员表引用不存在的 key，该引用被静默丢弃（树重建时记 warn 日志，不影响其余成员） | 高 | RepositoryConfigServiceImpl（configByKey.get(ref)==null → skip）+ VirtualRepo.aggregatedRepoByRef warn |
| 嵌套 virtual 展开 | 成员中的 virtual 递归展开：直接成员先登记、后递归其子成员（展开序 = 声明序 DFS）；展开结果**按类型分桶**收集：全部 local/federated 成员（含嵌套带来的，按遇到序）一桶、全部 remote 成员一桶——即**返回的配置序列里 locals 恒排在 remotes 之前**，无论声明顺序如何穿插 | 高 | `artifactory-core org/artifactory/repo/virtual/VirtualRepositoryConfigResolverImpl.java` getRepoConfigs + VirtualConfigResolverHelper.resolve（localAndFederatedRepoConfigs / remoteRepoConfigs / virtualRepoConfigs 三集合） |
| 嵌套层级上限 | 展开深度参数默认无限（Integer.MAX_VALUE）；环引用由 visitedKeys 剪断（A→B→A 时回到 A 直接返回） | 高 | 同上（visitedKeys.contains 提前 return） |
| 混合成员 | local/federated/remote/virtual 混排均支持；一个 remote 成员在解析序里贡献**两个实体**——其 cache 仓进 local 桶、remote 本体进 remote 桶（见 §2） | 高 | `artifactory-core org/artifactory/repo/virtual/VirtualRepositoriesResolver.java` assembleSearchRepositoriesList |

## 2. 四桶解析序（精确装配）

当客户端对 virtual 路径发起下载类解析，服务端把 §1 的展开序列装配为**四段拼接**的搜索序：

```
[优先 local/federated + 优先 remote 的 cache 仓]（成员声明序）
→ [优先 remote 本体]（成员声明序）
→ [非优先 local/federated + 非优先 remote 的 cache 仓]
→ [非优先 remote 本体]
```

| 规则 | 行为规格 | 置信度 |
|---|---|---|
| 桶划分 | 「优先」= 成员自身 `priorityResolution=true`；cache 仓跟随其 remote 的 priorityResolution（投影继承，见 remote-cache-projection.md §1.2） | 高 |
| cache 与 remote 不相邻 | 由于 locals 恒前于 remotes（§1 展开），同一优先级内**所有 cache 仓排在所有 remote 本体之前**——remote R 声明在 local L 之前时，序仍为 L（及各 cache）→ R | 高（两段代码合成推演：分桶收集序 + 四段拼接均可逐行复读） |
| 远端抑制 | 当请求来自另一台 Artifactory（header `X-Artifactory-Originated` 或 `Origin-Artifactory` 存在）且 virtual `artifactoryRequestsCanRetrieveRemoteArtifacts=false` → remote 本体**不入序**（但其 cache 仓仍在 local 桶内参与） | 高 |
| EDGE 边缘节点 | 带 EDGE_MIXED entitlement 的实例上 cache 仓不入序（remote 直接流式服务） | 高 |

证据锚：`artifactory-core org/artifactory/repo/virtual/VirtualRepositoriesResolver.java`（assembleSearchRepositoriesList 四桶 + addRemoteRepo/addLocalCacheRepo）、`artifactory-core org/artifactory/ph/PackageRepoServiceImpl.java`（isFromAnotherArtifactory 的 header 判定）。

## 3. 请求级解析流程（下载/HEAD）

当客户端 GET/HEAD `<virtual>/<path>`，按序（`artifactory-core org/artifactory/repo/virtual/ArtifactoryVirtualDownloadStrategy.java`）：

1. **virtual 自身缓存先查**：先查 `<virtualKey>-cache` 投影仓（npm/PyPI 聚合产物落这里，见 §6）；命中且未过期 → 直接返回（Maven 索引类文件恒走此捷径）。
2. **成员搜索**：按 §2 四段序逐仓 getInfo。开始前有协议拦截器机会（Maven metadata 合并即在此挂入，见 §5）。
3. **路径翻译**：每个成员查询前按「virtual 布局 → 成员布局」做 artifact 路径翻译；翻译路径未命中时**回退用原路径重查一次**。
4. **普通制品（processStandard）**：
   - 跳过不处理 release 的成员（路径可解析出模块信息且成员 `handleReleases=false`；checksum 旁车文件不受此限）；
   - 首个**精确匹配**（exact match）命中即返回；
   - 非精确命中（如目录级/翻译降级命中）暂存为「最近候选」，继续向后搜索，最终无精确命中才回退它；
   - 已有最近候选后，后续成员除非开启属性同步（synchronizeProperties）才继续查询 remote 系成员（默认跳过以省上游流量）。
5. **`[RELEASE]` 记号（processLatestRelease）**：路径含 `[RELEASE]` 时跨成员收集候选、按 Maven 版本比较器取最新；一旦某优先成员产出，非优先成员全部跳过；精确匹配候选优先于非精确、更新的版本覆盖更旧。
6. **快照路径（processSnapshot）**：路径为 Maven 快照或含 `[INTEGRATION]` 时：
   - 跳过 `handleSnapshots=false` 的成员；
   - **跳过所有 cache 仓**（快照由 remote 本体自行处理其缓存）；
   - 唯一快照（timestamped）精确命中即停止；
   - 非唯一快照按 lastModified 取最新（后续成员更晚修改才替换），精确匹配可越位替换非精确；
   - 找到过的最优候选在返回时其 responseRepoPath 指回实际持有的成员仓。
7. **成员 403 透传**：某成员返回 403（无权限/被拒）时记为 forbidden 候选；若最终无任何成员命中，最终 404 响应携带该成员的 403 详情（而不是笼统 not found）。
8. **本地 vs virtual 缓存裁决**：成员搜索命中且来源是**非 remote**（local/federated 成员或 cache 仓）时，若第 1 步的 virtual 缓存副本 lastModified ≥ 成员副本 → 返回 virtual 缓存副本（并把下载统计记到成员路径上）；否则返回成员结果。命中成员是 remote 本体时永远以成员结果为准（remote 内部自己裁决其 cache）。
9. **成员全 miss 且 virtual 缓存有存量**：当成员搜索结论是 404（真 not found）而 virtual 缓存还有该条目 → 服务端**删除 virtual 缓存条目**并返回 404。

置信度：高（整链逐分支可读；此条流程为代码补充官方规范——官方文档只描述 priorityResolution「优先」二字，不描述精确匹配/候选/403 透传/缓存裁决）。

## 4. includes/excludes 过滤时机与优先级

过滤分**三层**，作用点不同（全部为 `artifactory-core` 证据）：

| 层 | 时机与效果 | 置信度 | 证据锚 |
|---|---|---|---|
| ① virtual 自身 patterns | virtual 配置自己的 includesPattern/excludesPattern（默认 `**/*`/空）。解析成员序列**之前**先对请求 path 求值：不匹配 → 整个 virtual 拒绝（下载 404 / 上传 409，见 repo-semantics.md §9 勘误），任何成员都不参与。目录浏览的每个子项同样过此过滤（子项被滤 = 树里不显示） | 高 | VirtualConfigResolverHelper.resolve 首行 filter.accepts(top) + `artifactory-core org/artifactory/repo/service/RepositoryBrowsingServiceImpl.java` virtualRepoAccepts |
| ② 嵌套 virtual patterns | 展开嵌套 virtual 时，子 virtual 的 patterns 先按「父布局→子布局」**翻译后的 path** 求值；不匹配 → 该子 virtual 的**整棵子树被剪掉**（其下所有成员不参与） | 高 | VirtualRepositoryConfigResolverImpl.resolve（accepts(virtualRepo) 不通过即 return）+ `artifactory-core org/artifactory/repo/virtual/VirtualResolverRequestFilter.java` accepts/comparePaths/translateRepoPath |
| ③ 成员仓自身 patterns | 各成员 local/remote 自己的 includes/excludes 在**该成员被查询时**生效（remote 成员即 repo-semantics.md §7.2 第 1 步）；对 virtual 来说是成员内部行为，不改变成员序 | 高 | repo-semantics.md §7.2 / RealRepoBase.getInfo（已证） |

补充细节（`artifactory-core org/artifactory/repo/RepoBase.java` accepts）：

- 匹配对象做预处理：`<path>:properties` 按父路径匹配、checksum 旁车（`.sha1` 等）按去扩展名的主文件匹配；仓库根路径与系统路径（`.index` 等 NamingUtils.isSystem）与内部元数据路径**恒放行**（不被 patterns 拦）。
- Ant 风格通配（`**`/`*`）；仓库开启大小写不敏感模式时 path 先 lowercase 再匹配。
- **优先级总结**：① 拦整个 virtual → ② 剪子树 → ③ 成员内拒绝（该成员对此 path 视为 miss，顺序向后）。三层独立生效，不存在跨层合并/覆盖。

## 5. Maven 特有

### 5.1 maven-metadata.xml 合并（GET 拦截）

当客户端 GET virtual 下的 `maven-metadata.xml`（任意层级），服务端不落盘合并、每次现算（`artifactory-core org/artifactory/repo/virtual/interceptor/MavenMetadataInterceptor.java` + `.../interceptor/MergeableMavenMetadata.java`）：

| 规则 | 行为规格 | 置信度 |
|---|---|---|
| 触发与遍历 | 按 §2 四段序逐成员拉取同名文件：**跳过所有 cache 仓**（remote 本体自己带缓存语义）；快照级 metadata（`<artifactId>/<ver>-SNAPSHOT/maven-metadata.xml`）跳过 `handleSnapshots=false` 成员；优先成员已产出后非优先成员跳过 | 高 |
| 阻断透传 | 任一成员返回 blocked（curation 拦截）→ 直接原样返回该 blocked 响应，不再合并 | 高 |
| 合并算法 | 首个产出成员的 metadata 为基底；后续成员并入：versions 取并集后按 Maven 版本比较器重排、`latest` 重算为排序末位、`release` 重算为末位非 SNAPSHOT、快照 `snapshot`（buildNumber/timestamp）取二者较大；`snapshotVersions`（v3 记号）仅当系统开关开启 + 快照 metadata + 客户端声明支持 M3 快照记号时按「同 classifier+extension 取较新」合并，否则首个基底的 snapshotVersions 被剥除 | 高 |
| lastModified | 合并结果的 lastModified 取各成员最大值（影响响应 Last-Modified 头） | 高 |
| 全 miss | 无任何成员产出 → 404 `Maven metadata not found for '<path>'.`（成员有 403 时携带其详情） | 高 |
| 不缓存 | 合并结果不写 `<virtual>-cache`（区别于 npm，见 §6）；每请求重算 | 高 |
| 索引例外 | Maven 索引文件（`.index`）不走合并，恒返回 virtual 自身缓存副本（若缓存里没有则按普通文件解析） | 高 | MavenMetadataInterceptor.shouldReturnCachedResource |

### 5.2 pom 引用清洗（下载侧变换）

当客户端经 virtual 下载 `.pom` 文件（Maven/Gradle 系包型），服务端按 virtual 的 `pomRepositoryReferencesCleanupPolicy` 现场改写 pom（`artifactory-core org/artifactory/repo/virtual/interceptor/PomInterceptor.java` + `.../interceptor/transformer/PomTransformer.java`）：

| 策略值 | 行为规格 | 置信度 |
|---|---|---|
| `nothing` | 原样返回，不做任何改写 | 高 |
| `discard_active_reference`（**默认**） | 移除 pom 根级 `<repositories>` 与 `<pluginRepositories>`；`<profiles>` 内仅清洗「activation.activeByDefault=true」的 profile 的同名节点（非默认激活 profile 的仓库引用保留） | 高 |
| `discard_any_reference` | 根级 + **所有** profile 的 repositories/pluginRepositories 全部移除 | 高 |

- 未改写命中的 pom 走内存缓存（容量 10 万、12h 访问过期）跳过重复解析；改写后内容变了才输出新文档（否则返回原文）。
- XML 解析失败的 pom 原样返回（不阻断下载）。
- 改写只发生在**下载/服务侧**（virtual 对外发 pom 时）；上传侧不做此清洗——经 virtual 部署的 pom 落 defaultDeploymentRepo 时只做 pom 坐标与目标路径一致性校验（不一致按 suppressPomConsistencyChecks 决定拒绝与否，属 Maven 仓通用校验，与本策略无关）。
- 当客户端**变更**该策略值，服务端检测到 policy 变化后清理该 virtual 自身存储的全部内容（含按旧策略生成的缓存态 pom），后续请求按新策略重新派生——**此条补充官方规范**（官方文档只列字段值，不给改写节点清单与变更联动）。

### 5.3 快照与 release 记号解析

`[RELEASE]` / `[INTEGRATION]` 记号与唯一/非唯一快照的解析规则见 §3 第 5/6 条（同一代码路径，无 Maven 独立分支）；`handleReleases/handleSnapshots` 开关的跳过时机亦同。置信度高。

### 5.4 pom 级模块解析依赖

virtual 对 path 的模块信息解析（判定 release/snapshot、GAVC 拆分）基于 virtual 自身 `repoLayout`；对每个成员的路径翻译见 §3 第 3 条。（置信度：高）

## 6. npm 特有（与聚合缓存）

| 规则 | 行为规格 | 置信度 | 证据锚 |
|---|---|---|---|
| 包文档合并 | npm virtual 的包文档（registry 元数据 JSON）按成员序合并：首成员为基底、后续成员版本 putIfAbsent、dist-tags/time 取并集——**逐字段级合并算法见 `maven-npm-pypi.md` §2.6（已有规格，本文不重复）**；本文补充：合并发生在协议包处理器层，成员序列取 §2 四段序再做**缓存仓去重**（只要有序列含任一 remote 本体，就把所有 cache 仓从序列滤掉——remote 本体自身代表其缓存，避免同仓双查） | 高（去重分支本仓可读；逐字段合并算法沿用既有规格） | `artifactory-core org/artifactory/ph/PackageRepoServiceImpl.java` getSubRepositories（filterCacheRepositoriesDuplication） |
| 聚合缓存仓 | 合并结果写 `<virtualKey>-cache` 投影仓（`.npm/` 路径下）；TTL=`virtualRetrievalCachePeriodSecs`（virtual 配置 `virtualCacheConfig`，默认 **600s**）；§3 第 1/8/9 条的缓存裁决/删除规则即作用于它 | 高 | `artifactory-core org/artifactory/repo/virtual/VirtualRepo.java` getVirtualCacheTimeoutMillis + `artifactory-config org/artifactory/model/.../VirtualCacheConfig.java`（默认 600） |
| 缓存清理 | npm virtual 缓存有专属清理器：非审计条目 TTL 常量 `npm.virtual.cache.non.audit.item.ttl.seconds`（默认 864000s=10 天）、审计（`.security`）条目 TTL `npm.virtual.cache.item.ttl.seconds`（默认 600s），批量删除（batch 1000）；触发时机与 virtual cache 通用清理（`repo.virtualCacheCleanup.*`，默认 maxAgeHours=168）并存 | 高 | `artifactory-core org/artifactory/repo/cleanup/NpmVirtualCacheCleaner.java` + `artifactory-config org/artifactory/common/ConstantValues.java` 418-421/619-621 行 |
| 成员失效联动 | 当 local 成员发生部署/删除，含它的各 virtual 的聚合缓存按路径 zap（标记过期）或 evict（删除）——remote 成员回源变化由其自身 404→§3 第 9 条联动 | 高 | PackageRepoServiceImpl zapVirtualMetadataCache / undeployVirtualMetadataCache |
| npm 包文档合并的**成员内**细节（版本冲突取谁、unpublished 处理） | 未在本仓定位到逐字段实现（npm 协议处理器在闭源 addon 段）——**UNKNOWN**，以 `maven-npm-pypi.md` §2.6 既有口径为准，不在此扩写 | 低（转既有规格） | — |

## 7. 聚合行为（list / search / 下载归属 / 统计口径）

### 7.1 目录浏览合并

当客户端浏览 virtual 目录（UI 树 / REST file list），服务端把**全部嵌套展开后的 virtual 成员**（注意：以 virtual 链为单位，见下）各自的可浏览子项合并（`artifactory-core org/artifactory/repo/service/RepositoryBrowsingServiceImpl.java` getVirtualRepoBrowsableChildrenData）：

- 候选子项 = 各成员 local/federated 内容 + remote 成员内容（remote 侧含其缓存行与开档远端行，见 remote-browsing.md）；
- 按 relativePath 去重合并：**local 成员的 created/lastModified/size 覆盖远端条目的显示值**（同路径既有 local 又有 remote 时，展示 local 的时间与大小）；一个子项是否标记 remote = 所有来源都为 remote 才是；
- 每个子项携带**全部实际持有它的成员仓 key 清单**（UI 据此显示多仓来源）；同一子项经多条 virtual 链可达时记录各链路由；
- 合并前每个子项过 §4 ① 层过滤（virtual patterns）；
- Maven 系包型的 virtual 树**隐藏索引文件**（路径含 `.index` 或 MavenNaming 索引判定）。

置信度：高（合并分支逐行可读；**此条补充官方规范**——官方不描述 local 覆盖 remote 展示值与多来源清单）。

### 7.2 REST 存储面（GET /api/storage/...）

对 virtual 路径取 ItemInfo/FolderInfo/FileInfo 时按「**全部 local/federated 成员（展开序）→ 全部 cache 仓**」的序列取**第一个实际持有该 path 的成员**返回（不合并多成员的同名文件，目录才有 §7.1 合并）。（置信度：高；证据锚：`artifactory-core org/artifactory/repo/service/RepositoryServiceImpl.java` applyFunctionToLocalFederatedWithCaching——getResolvedLocalFederatedAndCachedRepos = locals → caches 拼接）

### 7.3 搜索域映射

- UI 语法搜索在 virtual 上执行时，搜索域展开为「各嵌套 virtual 链的 local 成员 key + 各 remote 成员的 `<remote>-cache` key」（remote 制品以缓存仓形态入库，故以 cache key 检索）；对每个 cache key 的读权限按 remote key 判定。
- AQL/老搜索对 virtual 的语义见 `aql.md` §virtual 域（既有规格）。

置信度：高。证据锚：`artifactory-rest-ui org/artifactory/ui/rest/service/artifacts/search/syntax/SyntaxSearchService.java` + `artifactory-core org/artifactory/repo/NestedRealRepositoriesWithRoutes.java`。

### 7.4 下载统计口径

| 场景 | 统计落点 | 置信度 |
|---|---|---|
| virtual → local 成员命中 | 下载计数记在**成员仓的实际路径**（responseRepoPath=成员 key），同时仓级统计记录原始请求 repo（virtual key）作为 origin | 高 |
| virtual → remote 成员命中（触发回源） | 响应路径被重写为 **`<remote>-cache` 仓路径**，下载计数落在 cache 仓路径上（制品在缓存中的真实存放地） | 高 |
| HEAD-only 请求 / `skipUpdateStats=true` 参数 / Xray 系用户 / 内部请求标记 skip / 全局 downloadStatsEnabled=false | 不计任何统计 | 高 |
| virtual 自身 | virtual 是非 real 仓，**永不做统计主体**（只在上面两类成员路径上计数） | 高 |

证据锚：`artifactory-core org/artifactory/storage/service/StatsServiceImpl.java` updateDownloadStatsIfNeeded（跳过条件与 repoStats origin）+ ArtifactoryVirtualDownloadStrategy:104（成员路径计数）+ RemoteRepoBase.getResourceStreamHandle（responseRepoPath 重写 cache key）。**此条补充官方规范**（官方不写统计归属仓）。

### 7.5 删除路径的归属（勘误 repo-semantics.md §8.2）

当客户端 `DELETE /{virtualKey}/{path}`：删除只作用于 **virtual 自身存储（即 `<virtual>-cache` 投影仓里的聚合缓存条目）**，**不会**逐成员删除 local 成员中的实体制品；virtual 存储中无此条目 → 404（ITEM_NOT_FOUND）。`repo-semantics.md` §8.2 中「逐成员查找实际持有者删除」一行与本代码证据**冲突**（该行本就标中置信度待补）——按本规格修正为准，并已上报冲突（见工作日志 Risks）。| 高 | RepositoryServiceImpl.undeployInternal/undeployMultiTransactionInternal（storingRepositoryByKey(virtual)→VirtualRepo.undeploy→仅 dbStorageMixin 自有存储）+ VirtualRepo.undeploy。

## 8. 与既有规格的关系

- 四桶序的概览版、写路由（PUT 405/defaultDeploymentRepo）、`artifactoryRequestsCanRetrieveRemoteArtifacts` 概念首见于 `repo-semantics.md` §8.1/§8.2——本文为其精确化与勘误（§7.5）。
- cache 投影仓自身的创建/可见性/清理见 `remote-cache-projection.md`（virtual `<key>-cache` 同源机制）。
- npm/PyPI/Maven 协议级细节入口：`maven-npm-pypi.md`（§0/§2/§3）、搜索域 `aql.md`、远端浏览 `remote-browsing.md`。

## 9. 待验证清单（低置信度，供转差分/活体验证）

1. §1「locals 恒前于 remotes」在**极端穿插声明**下的最终序（locals[L1,R1,L2] → 序为 L1,L2,(R1-cache),R1 还是 L1,L2,R1…）——两段代码合成推演为前者，建议活体用三个成员仓放同名不同内容制品验证首命中。
2. §5.2 上传侧 pom 清洗的确切触发点与 policy 变更重算范围（「virtual 内容清理重建」分支已见调用于配置变更路径，重建粒度未走读）。
3. §7.3 的搜索域映射对 AQL（非 UI 语法搜索）是否同样自动把 remote 翻译为 cache key（aql.md 记为按 virtual 域展开，两侧口径需对拍）。
4. `discard_any_reference` 官方字段名与枚举内部名（`discard_any_reference` vs UI 文档的 `discard_any_reference`）一致性与合法值集合的 wire 校验错误形态。
5. 本会话参照实例（192.168.120.38:8082）凭据不可用，全部条目为反编译单源——高风险条目（§7.5 勘误、§3 第 9 条缓存删除联动）建议列入下一轮差分腿。
