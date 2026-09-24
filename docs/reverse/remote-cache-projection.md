# Remote 仓 `<repoKey>-cache` 影子仓库投影行为规格

> 源版本：7.161.24 反编译（部分 7.161.20）；模块内嵌 pom 多标 7.161.14（发行包版本以 README 为准 7.161.24）。
> 本文是 `repo-semantics.md` §7（remote 语义）与 §8.4（隐藏仓布局）的**深化增量**，不重复其内容；pull-through 主流程、checksum 四值策略、上游故障矩阵见彼处。
> 置信度：`高` = 反编译代码可逐分支复读（+官方文档印证）；`中` = 仅代码单源、调用链未全展开；`低` = 推断，待动态验证。
> 证据锚统一写法：`模块名 相对路径`（相对 `/Users/lzw/workspace/artifactory-decompiled/backend/`）。

## 1. 投影创建与生命周期

### 1.1 cache 仓不是持久实体，是投影

| 规则 | 行为规格 | 置信度 | 证据锚 |
|---|---|---|---|
| 无独立配置记录 | 当客户端创建 remote 仓库时，服务端**不会**在配置存储中创建任何 `-cache` 仓库记录；`-cache` 仓由 remote 配置在内存仓库缓存重建时**派生**（每次配置重载重新投影） | 高 | `artifactory-core org/artifactory/repo/service/RepositoryServiceImpl.java`（rebuildRemoteAndCacheRepositories：遍历 remote 配置，`remoteRepo.getLocalCacheRepo()` 装入 localCacheRepositoriesMap） |
| 投影条件 | 仅当 remote 配置 `storeArtifactsLocally=true`（默认 true）时投影出 cache 仓；`false` 时该 remote **没有** `<key>-cache` 仓（改回 true 后下次重载重新投影，旧缓存数据仍在存储中） | 高 | 同上 + `artifactory-core org/artifactory/repo/RemoteRepoBase.java` init()：`if (isStoreArtifactsLocally()) { new DbCacheRepo(...) }` |
| 命名规则 | 当 remote 仓 key 为 `<K>`，投影仓 key 恒为 `<K>-cache`（常量拼接，无任何可配置项）；virtual 聚合缓存仓同规则 `<virtualKey>-cache` | 高 | `artifactory-config org/artifactory/model/LocalCacheRepoConfig.java`（`key = remoteKey + "-cache"`，PATH_SUFFIX="-cache"） |
| 描述字段 | 投影仓 description = remote 的 description 追加 `" (local file cache)"`（已含该后缀则不重复追加） | 高 | 同上 + `artifactory-core org/artifactory/repo/db/DbCacheRepo.java` createCacheDescriptorFromRemote |
| 时间戳 | 投影仓的 creation/modification 完全镜像 remote 仓值，不可独立修改 | 高 | LocalCacheRepoConfig.getCreation/getModification/setXxx 空实现 |
| 仓库分类 | 投影仓对外呈现为 LOCAL 类仓库（rclass 语义），但带 cache 标记（对配置面 `isManaged()` 判定排除 `-cache` 后缀 key：**配置服务拒绝直接管理 cache key**） | 高 | LocalCacheRepoConfig.getRepoType()=LOCAL + `artifactory-core org/artifactory/repo/service/RepositoryConfigServiceImpl.java` isManaged() |

### 1.2 从 remote 配置继承与固定的字段

投影仓的仓库级配置由 remote 配置派生（当客户端改 remote 配置，投影仓行为随之改变，重载后生效）：

| 字段 | 投影值 | 置信度 |
|---|---|---|
| archiveBrowsingEnabled / blackedOut / propertySetRefs / priorityResolution / maxUniqueSnapshots / handleReleases / handleSnapshots / downloadRedirect / cdnRedirect | 逐项镜像 remote 同名字段 | 高 |
| checksumPolicyType | **固定 `client-checksums`**（与 remote 自身的四值策略无关；缓存写入按「客户端声明=上游响应头，必须与实测一致否则拒收」执行） | 高 |
| snapshotVersionBehavior | **固定 `unique`**（缓存中的 Maven 快照按唯一快照路径存储，不做 deployer/nonnull 变换） | 高 |
| repoLayout | 镜像 remote 的布局 | 高 |
| packageType | 镜像 remote 的包型 | 高 |

证据锚：`artifactory-config org/artifactory/repo/remote/RemoteCacheRepoTypeConfig.java`（逐方法覆写）+ `artifactory-config org/artifactory/model/LocalCacheRepoConfig.java`。

### 1.3 创建防护：`-cache` 后缀保留

- 当客户端尝试创建任何 key 以 `-cache` 结尾的仓库（local/remote/virtual/federated 均适用）→ **400**，body `Unable to create a <type> repository '<key>' with '-cache' suffix`。 | 高 | RepositoryConfigServiceImpl upsertRepoConfigInternal（isInsert 分支）与 badRequestValidation 双处。

### 1.4 删除与重命名

| 场景 | 行为规格 | 置信度 |
|---|---|---|
| 删除 remote 仓 | 服务端先临时把该 remote 置 blackedOut=true 且（若原为 false）storeArtifactsLocally=true 并静默更新配置（保证投影存在），再删除其存储内容（即 cache 仓全部数据，含回收站路径处理与 local 同），最后删除 remote 配置并从所有 permission target 中同时移除 `<key>` 与 `<key>-cache` 两个 key | 高 | RepositoryServiceImpl prepareRepoForRemoval / deleteRepositoryContent / deleteOrphanRepo / deleteRepoFromAllRelatedRepoAcls |
| 独立删除 cache 仓 | 当客户端 `DELETE /api/repositories/<K>-cache` → **404**，body `'<K>-cache', repository config does not exist`（默认 v2 删除流按配置存储判定，cache key 无记录；`remove.repository.flow.v2.enabled` 默认 true） | 高 | RemoveRepositoryServiceImpl.getRepoConfig（getRepoConfigByKeyNoValidationChecks）+ `artifactory-api org/artifactory/api/common/RepoRemovalStatus.java`（REPO_NOT_FOUND=404 文案）+ `artifactory-config org/artifactory/common/ConstantValues.java` 1624 行默认值 |
| 重命名 | 不存在 rename 仓库操作；cache 仓 key 由 remote key 派生，改 key = 删旧建新（旧 `<K>-cache` 内容随 remote 删除流程清理，新 key 重新投影空 cache） | 中（未见 rename 代码路径，由「无 rename 端点 + 派生 key」推定） |

## 2. 访问与可见性

### 2.1 REST 面

| 操作 | 行为规格 | 置信度 |
|---|---|---|
| `GET /api/repositories`（列表） | 返回集合**不含**任何 `-cache` 仓（列表来自配置存储的 LOCAL/FEDERATED/REMOTE/VIRTUAL 四类，cache 仓无配置记录） | 高 | `artifactory-rest org/artifactory/rest/resource/repositories/RepositoriesResource.java` getReposList |
| `GET /api/repositories/<K>-cache`（直查） | 服务端走「非受管 key」回退分支：按内存仓库缓存取投影对象并返回其**派生配置**（可见 key、description 带 "(local file cache)"、rclass=local 系字段），不 404 | 中（getRepoConfigByKey 回退分支可读；未活体复验响应 JSON 形态） | RepositoryServiceImpl getRepoConfigByKey（isManaged 为 false → repositoryByKey 回退） |
| `PUT/POST /api/repositories/<K>-cache`（建/改） | 创建 → §1.3 的 400（`-cache` 后缀防护）；更新语义上无此配置记录，按创建走 400 | 中 | 同 §1.3（isInsert 判定基于配置存储无记录 → 视为 insert） |
| `GET /<K>-cache/<path>`（直接下载/浏览） | **可用**：请求按通用仓库查找命中投影仓，直接服务缓存内容；命中文件时下载统计与 curation 检查挂到父 remote（`<K>`）上执行 | 高 | RepositoryServiceImpl RepositoriesCacheBuilder.repositoryByKey（localCacheRepositoriesMap 命中序第 2 位）+ `artifactory-core org/artifactory/engine/DownloadServiceImpl.java`（isCache 分支：strip `-cache` 找 remoteRepo 做 curation） |
| `PUT /<K>-cache/<path>`（直传 deploy） | **拒绝**：外部部署请求的目标仓解析只查 local/federated 表，cache 仓不在其中 → **404** `Could not find a local repository named <K>-cache to deploy to.`（与直接 deploy 到 remote 仓 key 同错误形态）。例外：**内部复制/校验和部署请求**（smart remote 复制回源、系统目录上传）允许以 cache key 为目标写入 | 高 | `artifactory-core org/artifactory/util/UploadServiceUtils.java` getTargetRepository（localOrFederatedRepositoryByKey vs localFederatedOrCachedRepositoryByKey 双路）+ `artifactory-core org/artifactory/engine/UploadServiceImpl.java` sendInvalidTargetRepositoryError（404 文案） |
| copy/move 目标为 cache 仓 | 当客户端把 copy/move 目标设为 `<K>-cache` → 拒绝（错误文案 `Target repository <K>-cache is a cache repository. <Copy/Move> to cache repositories is not allowed.`） | 高 | `artifactory-core org/artifactory/repo/service/mover/RepoPathMover.java` 107 行 |
| `GET /api/storage/<K>-cache/...`（REST 存储面） | 权限按 §2.2 的 ACL 映射求值后可直查（授权服务显式处理 `-cache` key） | 中 | `artifactory-core org/artifactory/security/AuthorizationServiceBase.java` getRepoPathAcls（localFederatedOrCachedRepoDescriptorByKey + 去 `-cache` 后缀求值） |

### 2.2 权限映射

- 当客户端以 `<K>-cache` 仓路径发起任意授权判定，服务端把 repoKey 的 `-cache` 后缀剥掉后按 remote 仓 `<K>` 的 permission target 求值——即**对 remote 的 ACL 自动覆盖其 cache 仓**，无需单独授权条目。| 高 | AuthorizationServiceBase.getRepoPathAcls。
- 反向防护：当客户端新建 ACL 时其 repo 集合含 `-cache` key，服务端把集合中的 cache key 自动转换为对应 remote key 再保存。| 高 | AuthorizationServiceBase.convertNewAclCachedRepoKeysToRemote + RepositoryServiceImpl.convertCachedRepoKeysToRemote。
- 删仓时 permission target 内的 `<K>` 与 `<K>-cache` 两个 key 一并移除（见 §1.4）。| 高

### 2.3 控制台 UI 面

| 面 | 行为规格 | 置信度 |
|---|---|---|
| 树根分组 | UI 树根把缓存仓作为独立分组类型 `cached`（与 local/federated/remote/virtual 并列的第五组）展示，节点数据取投影描述符集合，**分组过滤按去 `-cache` 后的 remote key 的项目归属与权限**判定 | 高 | `artifactory-rest-ui org/artifactory/ui/rest/model/artifacts/browse/treebrowser/nodes/fetch/RootFetchByRepositoryType.java`（RepositoryType.CACHED 组 + getCachedRepoDescriptorsByProject）+ RepositoryServiceImpl getCachedRepoDescriptorsByProject（removeDashCache 后过滤） |
| remote 节点展开 | UI 树中 remote 仓节点声明有子节点（hasChild=true）；展开 remote 节点浏览的就是其 `-cache` 仓内容，子项显示时把 repoKey 的 `-cache` 后缀剥掉映射回 remote key（下载 URL、tab 数据都指回 remote） | 高 | `artifactory-rest-ui .../nodes/repo/VirtualRemoteRepositoryNode.java`（hasChild=true）+ `.../nodes/JunctionNode.java` createRemoteFolderNode（removeEnd "-cache"） |
| Set Me Up / 依赖片段 | 生成 distributionManagement 等片段时同样剥 `-cache` 后缀取 remote key | 高 | `artifactory-rest-ui .../tabs/general/distributionmngt/DistributionManagement.java` |
| 搜索（UI 语法搜索） | 在 virtual/remote 上搜索时，远端制品的搜索域映射为 `<remote>-cache` key 集合（结果行显示为 "cached" 类型）；对 cache key 的可读权限沿用 §2.2 映射 | 高 | `artifactory-rest-ui org/artifactory/ui/rest/service/artifacts/search/syntax/SyntaxSearchService.java`（remote → `k + "-cache"` 映射两处 + addRemoteRepoWithCacheIfAllowed） |

## 3. 缓存写入路径（哪些请求产物落 cache）

以下补充 repo-semantics.md §7.2/§7.3 的 pull-through 主流程，聚焦投影仓视角：

| 规则 | 行为规格 | 置信度 |
|---|---|---|
| 写入触发 | 当客户端 GET 一个 remote 路径且缓存缺失/过期/强制过期（matrix `forceExpiryCheck` 或请求参数 `artifactory.forceDownloadIfNewer=true`），回源成功后服务端把字节流**先写入 `<K>-cache` 再流式回发客户端**；响应的 responseRepoPath 重写为 cache 仓路径（影响统计口径，见 virtual-resolution.md §7） | 高 | RemoteRepoBase.getResourceStreamHandle（isCacheArtifact → downloadAndSave；setResponseRepoPath(cacheKey/path)） |
| 不落盘模式 | `storeArtifactsLocally=false` 时完全流式代理不写投影仓（且无投影仓，见 §1.1）；curation 直通（pass-through）模式下也不写 | 高 | RemoteRepoBase.isCacheArtifact / isPassThroughEnabled |
| 写入校验 | 保存前对 cache 仓路径执行与 local 部署相同的路径校验链（include/exclude、blacked-out、快照策略、配额 413）；checksum 按 §1.2 固定 client 策略校验（上游 `X-Checksum-*` 头为「客户端声明」） | 高 | RemoteRepoBase.downloadAndSave（assertValidDeployPathAndPermissions 携 contentLength 与 forceExpiryCheck） |
| metadata 类文件 | `maven-metadata.xml`、npm 包文档、PyPI 索引等「内部元数据」同样落 cache 仓，但被标记为 expirable（按包型注册的过期检查器判定）；expirable 条目的并发刷新等待上限取 remote 的 `metadataRetrievalTimeoutSecs`（默认 60s），等锁超时回发旧缓存副本 | 高 | RemoteRepoBase.downloadAndSave（isExpirable && isExpired → metadataRetrievalTimeoutSecs 锁分支）+ `artifactory-core org/artifactory/repo/cache/expirable/CacheExpiryImpl.java`（按包型 checker 注册表） |
| 单飞与二次确认 | 同 path 全局按 cache path 加锁单飞；拿到锁后重读缓存状态（期间可能已被其它线程刷新→直接用新缓存） | 高 | RemoteRepoBase.downloadAndSave（lockAcquired → 重查 getInfo） |
| 回源失败回退 | 下载中 IO 错误且本地有过期副本 → 把过期副本 unexpire 后回发（不再回源） | 高 | RemoteRepoBase.getResourceStreamHandle catch 分支（unexpireAndRetrieveIfExists） |
| 回收站 | cache 仓内容的删除（zap 不删、cleanup 删）不进 auto-trashcan 的覆盖保护语义与 local 一致（删除走同一 undeploy 链） | 中 | DbCacheRepo.undeploy 委托 mixin.undeploy；trash 细节未逐行走读 |

## 4. 缓存读取与 TTL（missedRetrievalCachePeriod 等语义）

| 机制 | 行为规格 | 置信度 |
|---|---|---|
| 新鲜期判定 | cache 条目年龄 = now − lastUpdated（从未更新过 = -1 视为过期）；**仅当 remote 未显式 offline 且条目 expirable**（或按包型判 expirable）时才与 `retrievalCachePeriodSecs`（默认 7200s）比较，超过即标记过期（下次请求触发回源校验） | 高 | DbCacheRepo.isExpired + `artifactory-core org/artifactory/resource/FileResource.java` getCacheAge |
| offline 冻结 | remote `offline=true` 期间 cache 条目**永不过期**（isExpired 直接 false），只服务缓存 | 高 | DbCacheRepo.isExpired 首条件 |
| Maven Central 索引特例 | 当 remote URL 含 Maven Central 域且路径为 Maven 索引（`.index`）文件，其新鲜期上限改用全局常量 `mvnCentralIndexerMaxQueryIntervalSecs`（防高频拉索引） | 中（常量默认值未摘录，行为分支可读） | DbCacheRepo.getRetrievalCachePeriodMillis + ConstantValues.mvnCentralIndexerMaxQueryIntervalSecs |
| 负缓存（miss cache） | 上游 404 的 path 记入 remote 仓的**内存**负缓存（不落盘、不进投影仓），TTL=`missedRetrievalCachePeriodSecs`（默认 1800s）；期内同 path 请求直接 404 不打上游。已有过期本地副本时不写负缓存（继续可回发过期副本）。被 include/exclude 等规则拒绝的资源、远端目录枚举 IO 失败的目录路径也写入负缓存 | 高 | RemoteRepoBase getRemoteResource（`!foundExpiredInCache` 才 put）/ addRejectedResourceToMissedCache / addRemoteListingEntryToMissedCache / buildCache |
| 头信息缓存 | 对上游做过的 HEAD/GET 探测结果与远端目录列表走第二个内存缓存（TTL 同 retrievalCachePeriodSecs），仅服务于远端浏览，非制品缓存 | 高 | RemoteRepoBase.initCaches / getRemoteResourceCache |
| unexpire | 条目被确认为仍新鲜（上游 Last-Modified 不新于本地、或同 checksum）时撤销过期标记（只更新 lastUpdated，不重写字节） | 高 | DbCacheRepo.unexpire + RepositoryServiceImpl.unexpireIfExists |
| 重载清空 | remote 配置变更触发仓库缓存重建时，内存负缓存与远端列表缓存清空（制品缓存不受影响） | 高 | RemoteRepoBase.clearCaches |

## 5. 清理与配额

| 机制 | 行为规格 | 置信度 |
|---|---|---|
| 未使用清理任务 | 当 remote 配置 `unusedArtifactsCleanupPeriodHours > 0`（默认 0=关闭），清理任务周期性删除该 cache 仓中「最近 N 小时内无下载」的条目（判定基准=条目 lastDownloaded 统计早于 now−N；批量删除、受任务时长与条数上限约束）；`0` 跳过。配置了远端复制的仓默认跳过（可配置不跳过）。本地生成类条目（isLocalGenerated 判定，如翻译索引）不清理 | 高 | `artifactory-core org/artifactory/repo/cleanup/ArtifactCleanupServiceImpl.java` clean/performCleanOnRepo/doClean（周期取自 remote 配置、isCache 守卫、复制跳过） |
| 仅清 cache | 清理任务硬守卫：目标仓必须是 cache 仓（`Cannot cleanup non-cache repository` 告警跳过） | 高 | 同上 performCleanOnRepo |
| Zap（过期标记，非删除） | `POST /api/zap/<K>-cache/<path>`（admin/user 角色）→ 服务端把该子树的 lastUpdated 批量改为 now−retrievalCachePeriod（即全部标记为已过期，下次请求强制回源校验），同时从负缓存与远端列表缓存中移除该 path（子树）；仅 expirable 文件被 zap，文件夹一律 zap；返回 200 `Completed zapping cache in path <repoPath>`。对非 cache 仓调用 zap → 按节点类型拒绝（`Got a zap request on a non-local-cache node` 日志路径） | 高 | `artifactory-rest org/artifactory/rest/resource/artifact/ZapArtifactsResource.java` + `artifactory-core org/artifactory/repo/service/ZapServiceImpl.java` + RepositoryServiceImpl zap 分支 |
| 逐成员 zap 触发 | 当删除/修改某 local 成员路径时，含它的 virtual 聚合缓存可被逐路径 zap/evict（协议处理器触发；见 virtual-resolution.md §6） | 高 | `artifactory-core org/artifactory/ph/PackageRepoServiceImpl.java` zapVirtualMetadataCache/undeployVirtualMetadataCache |
| 存储配额 | 缓存写入前的部署校验链包含配额检查（超限 413），配额按投影仓所属项目口径计算——cache 仓用量计入 remote 所属 project | 中（配额链在 assertValidDeployPathAndPermissions 内可见，projects 配额分摊细节未走读） | RemoteRepoBase.downloadAndSave + RepositoryServiceImpl.assertStorageQuota |
| GC | cache 仓的 blob 去重/回收与 local 完全同一套（内容寻址） | 高 | 继承 DbLocalRepo 全套 |

## 6. 与既有规格的关系

- 本文件深化 `repo-semantics.md` §8.4 的三行表；§7.2/§7.3/§7.4/§7.5 的 pull-through 流程、checksum 策略、上游故障矩阵不在本文重复。
- virtual 仓视角下 cache 成员如何进入解析序，见 `virtual-resolution.md` §2。
- 远端目录浏览（`listRemoteFolderItems`）与远端列表内存缓存见 `remote-browsing.md`。
- npm/PyPI 聚合缓存的 virtual `<key>-cache` 仓语义见 `virtual-resolution.md` §6（与本文件所述 remote cache 投影规则同源：同一派生机制，§1 规则对 virtual 同样成立）。

## 7. 待验证清单（低置信度，供转差分/活体验证）

1. `GET /api/repositories/<K>-cache` 直查的完整响应 JSON（rclass 字段值、是否回显 remote 专属字段）——§2.1 标中，本会话参照实例凭据不可用，未能活体复验（ping 200 但认证 401）。
2. `unusedArtifactsCleanupPeriodHours` 清理判定用的确切统计字段（lastDownloaded vs 访问时间）与删除是否进回收站——§5 doClean 查询条件未逐字段摘录。
3. `mvnCentralIndexerMaxQueryIntervalSecs` 常量默认值与 Central 域名匹配模式（`ConstantValues.mvnCentralHostPattern` 值）。
4. v1 删除流（`remove.repository.flow.v2.enabled=false`）下 `DELETE /api/repositories/<K>-cache` 的行为差异（走 deprecated 流可能返回成功但仅清缓存内容）——默认关闭，仅列档。
5. 直传 `PUT /<K>-cache` 的内部请求白名单边界（ReplicationChecksumDeployRequest/InternalSystemFolderUploadRequest 之外还有哪些内部请求类型可写 cache）。
