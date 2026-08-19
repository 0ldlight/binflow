# 仓库语义行为规格 — Local（M1）+ Remote/Virtual（M3）

> 逆向基线：`o.a.a.repo.service.RepositoryServiceImpl`、`o.a.a.repo.db.DbStoringRepoMixin`、`o.a.a.repo.RealRepoBase`、`o.a.a.engine.UploadServiceImpl/DownloadServiceImpl`、`o.a.a.io.checksum.policy.LocalRepoChecksumPolicy`。
> 官方参照：JFrog "Repository Configuration"（local repo 字段表）。
> 置信度：`高` 双证 / `中` 仅代码 / `低` 推断。

## 1. 路径解析（repoKey + path 的推导规则）

来源：`ArtifactoryRequestBase#calculateRepoPath`、`RestUtils#calcRepoPathFromRequestPath`。

| 规则 | 规格 | 置信度 |
|---|---|---|
| 切分 | 请求路径第一段（首个 `/` 前）= repoKey，其余 = path | 高 |
| 尾斜杠 | `repo/a/b/` → path `a/b` 且标记 isFolder=true（PUT 建目录、GET 目录列表的依据） | 高 |
| matrix 参数 | `repo/a/b.txt;k=v` → path `a/b.txt`，`;k=v` 收进属性集；repoKey 段也可带（`repo;k=v/...`）并同样剥离 | 高 |
| 特殊前缀 | 首段为 `list`（NuGet v2 装饰）或 `simple`（Docker/Debian 装饰）时跳过后再取 repoKey。M1 Generic 不涉及 | 中 |
| dot-segment | `/./`、`/.` 归一化移除；`..` 处理未在走读范围内（BinFlow 建议 Go 侧 `path.Clean` 并拒绝逃逸出 repo 根） | 中（`#removeDotSegments` 只见 `.` 处理） |
| URL decode | path 与 repoKey 均 UTF-8 decode；`+` 保护为字面加号（先 `%2B` 转义再 decode） | 高 |
| 老元数据后缀 | `<path>:properties` / `:statistics` 剥离并把请求标记为元数据操作 | 高 |
| zip 内路径 | `<file.jar>!<inner/path>`（`!` 分隔）拆出 zipResourcePath，M1 不实现 | 中 |

## 2. 请求校验顺序（PUT 上传链，`assertValidDeployPathAndPermissions`）

1. repo 存在性 / 是否 local（virtual 未配 local deployment → 405+`Allow: GET`）。
2. `assertValidPath`：
   - `blackedOut=true` → 拒绝（BlackedOutException，经默认 RepoRejectException 状态 **404**）。**中**（文案确定，最终状态码经 `sendError(var5.getErrorCode())`，`RejectedArtifactException` 未覆写 getErrorCode → 基类默认 404；SnapshotPolicyException 显式 409，推断 BlackedOut 走 404）
   - Maven 系 release/snapshot 开关（Generic 忽略）。
   - includesPattern / excludesPattern 匹配（默认 `**/*` / 空 → 全放行）。**高**
3. 权限：canDeploy（文件）/ canAnnotate（元数据）→ 403；匿名 → 401 挑战。
4. 覆盖检查（`assertOverwrite=true` 路径，见 §3）。
5. 存储配额 → 413。

## 3. 覆盖 / 禁止覆盖语义

来源：`DbStoringRepoMixin#shouldProtectPathDeletion/assertOverride/isFileOverwrite`。

| 场景 | 行为 | 置信度 |
|---|---|---|
| 路径不存在 | 直接新建 | 高 |
| 路径已存在，客户端带 checksum 且与服务端既有 checksum **相同** | 视为幂等重传：**不触发覆盖权限检查**（`differentChecksums` 均 false → isFileOverwrite=false），无 deploy 权限也可完成——除非 `enforcePermissionCheckOnIdenticalChecksumDeploy=true`（默认 false） | 高（代码逐条件可见） |
| 路径已存在且 checksum 不同（或未带 checksum） | 需要对旧节点有 **DELETE 权限**才能覆盖；否则 403（`Not enough permissions to delete/overwrite all artifacts under '<path>' (user: '<u>' needs DELETE permission).`） | 高 |
| 旁车 checksum 文件（`.sha1` 等）与 maven-metadata.xml | 永不触发覆盖检查（可自由重写） | 高 |
| 覆盖时的保留 | `sendOverwritesToTrashcan=true`（默认）→ 旧版本先拷入回收站再覆盖 | 高 |
| 覆盖后的元数据 | `created/createdBy` 保留首次值，`modified/modifiedBy/updated` 更新；旧 checksum 引用关系清除（`clearOldFileData`） | 高 |

**BinFlow 实现要点**：M1 无完整 ACL 时，至少实现「同 checksum 幂等重传免覆盖检查 + 不同 checksum 视为覆盖」的骨架，权限位接 admin/token 即可。

## 4. 删除语义

来源：`RepositoryServiceImpl#undeploy/undeployInternal`、`webdav.methods.DeleteMethod`、`ArtifactDeleteFailStatus`。

| 场景 | 行为 | 置信度 |
|---|---|---|
| 成功（文件或目录树） | **204** 无 body；目录删除后自动 prune 空父目录（多事务模式下） | 高 |
| 路径不存在 | 404 `Could not locate artifact. Path: '<repoPath>'` | 高 |
| repo 不存在 | 404 `Could not find storing repository by key: '<key>'` | 高 |
| 无 DELETE 权限 | 403 `Not enough permissions to delete/overwrite ...`（hideUnauthorizedResources=true 时伪装 404 `Could not locate artifact...`） | 高 |
| 删除仓库根（`DELETE /repo`） | 非 project-admin → 403 `No permissions to delete content of repository. This requires admin privileges...` | 高 |
| 内部系统目录 | 404（伪装不存在） | 中 |
| 回收站联动 | 删除前复制到 `auto-trashcan`（见 storage-layout.md §5；trashcan.enabled=true 时） | 高 |
| blob | 从不直接删；交 GC | 高 |
| `atomic` 参数 | `?atomic=true` 单事务（全成或全败）；默认多事务（部分成功可能残留） | 中 |

## 5. checksum 策略（local 仓库专属）

来源：`LocalRepoChecksumPolicyType`（枚举）、`LocalRepoChecksumPolicy#verify`。

repo 配置字段 `checksumPolicyType`，两个合法值：

| 值 | 上传时 | 读取时（ETag/X-Checksum 头、REST checksums 字段） | 置信度 |
|---|---|---|---|
| `client-checksums`（默认） | 客户端带的 checksum 与服务端实测**必须一致**，否则 `409`（`Checksum policy 'LocalRepoChecksumPolicy: CLIENT' rejected the artifact '<repoPath>'. Checksums info: ...`）；未带则服务端计算后接受 | 优先返回客户端声明值（original），缺失回退 actual | 高 |
| `server-generated-checksums` | 客户端值不匹配也接受，以服务端实测为准 | 返回服务端实测值 | 高 |

- 校验只对「客户端显式提供了 checksum」的类型执行；没提供的类型跳过。
- 三种算法（sha1/sha256/md5）逐一独立校验。
- `.sha1` 旁车文件登记的不匹配：`client-checksums` → 409；`server-generated-checksums` → 静默接受（详见 rest-api.md §1.5）。
- 下载时 `binaries` 表 actual 值即响应头 `X-Checksum-*` 与 ETag 的来源。
- **制品不可变性**：同一 repo path 的已存文件只能被「覆盖」或「删除」，没有 in-place 修改；blob 层内容寻址天然不可变。**高**

## 6. 其它 local 配置字段对 M1 的行为影响

| 字段 | 默认 | M1 行为 | 置信度 |
|---|---|---|---|
| `includesPattern` / `excludesPattern` | `**/*` / 空 | Ant 风格通配；命中排除或不含于包含 → 上传 404 拒绝（`Rejected by include/exclude patterns` 类文案）。BinFlow 建议实现 `**`、`*` 两级 | 高（默认值）/ 中（拒绝状态码：走 assertValidPath → RepoRejectException 默认 404） |
| `blackedOut` | false | true 时拒上传（404，见 §2）；下载侧对 blacked-out repo 的文件列表请求返回 404 | 中 |
| `handleReleases` / `handleSnapshots` | true/true | 仅 Maven 系语义；Generic 忽略 | 高 |
| `archiveBrowsingEnabled` | false | 影响 Content-Disposition 注入（见 rest-api.md §1.4）；M1 可忽略 | 中 |
| `propertySets` | 空 | 属性校验集；M1 不实现 | — |
| `notes` / `description` | 空 | 纯展示 | 高 |

## 7. remote 仓库语义（M3）

> 基线：OSS `o.a.a.repo.RemoteRepoBase` / `HttpRepo`（backend/core，未闭源）+ base/config `RemoteRepositoryRepoTypeConfig`（默认值一手）+ pro `c.j.ph.*.command.remote.*`（协议层缓存补充）。JFrog 官方 "Remote Repository Configuration" 文档字段表可交叉印证，行为细节多为代码补充。

### 7.1 配置字段与默认值

| 字段 | 默认 | 行为要点 | 置信度 |
|---|---|---|---|
| `url` | — | 上游基地址；路径拼接 `{url}/{path}` | 高 |
| `username` / `password` | 空 | Basic 凭据（header 形式透传）；`enableTokenAuthentication` 切 token 头 | 高 |
| `proxyRef` / `disableProxy` | 空/false | 出口代理 | 高 |
| `socketTimeoutMillis` | 15000 | 上游 IO 超时 | 高 |
| `storeArtifactsLocally` | true | false = 纯流式代理不落盘（checksum 改为回源取或 Generic 的 sha256 旁站文件） | 高 |
| `retrievalCachePeriodSecs` | 7200（2h） | 已缓存**制品**的新鲜期；过期后才回源校验 | 高 |
| `missedRetrievalCachePeriodSecs` | 1800（30min） | 上游 404 的负缓存 TTL | 高 |
| `assumedOfflinePeriodSecs` | 300（5min） | 上游连接故障后标记 assumed-offline 的静默期（期内不回源，直接走缓存/404） | 高 |
| `metadataRetrievalTimeoutSecs` | 60 | 并发刷新 metadata（如 maven-metadata.xml）时等锁上限，超时回发旧缓存副本 | 高 |
| `hardFail` | false | true = 上游错误向上抛（500）而非 404 | 高 |
| `offline` | false | 显式离线：只服务缓存 | 高 |
| `checksumPolicyType` | `generate-if-absent` | §7.5 四值 | 高 |
| `shareConfiguration` | false | smart remote：从上游 Artifactory 继承 repo 配置 | 高 |
| `listRemoteFolderItems` | false | 目录浏览时是否合并远端条目 | 高 |
| `blockMismatchingMimeTypes` | true | 上游 Content-Type 与本仓 mime 期望不符 → 拒收 | 高 |
| `bypassHeadRequests` | false | true = 探测直接 GET（上游 HEAD 不准时） | 高 |
| `allowAnyHostAuth` / `enableCookieManagement` / `propagateQueryParams` / `queryParams` / `customHttpHeaders` / `clientTlsCertificate` / `localAddress` | — | 传输层选项 | 高 |
| `blackedOut` | false | 同 local：拒绝服务 | 高 |
| `handleReleases` / `handleSnapshots` / `includesPattern` / `excludesPattern` / `priorityResolution` | true/true/`**/*`/—/false | 同 local 语义（virtual 桶序见 §8.1） | 高 |
| `unusedArtifactsCleanupPeriodHours` | 0（关） | 缓存清扫任务周期 | 高 |

（`maxUniqueSnapshots`、`remoteRepoLayoutRef`、`synchronizeProperties`、`contentSynchronisation` 为进阶项，M3 不依赖。）

### 7.2 pull-through 读取流程（GET/HEAD 统一）

对 `{repoKey}/{path}` 的未命中请求，按序（`RemoteRepoBase#getInfo` → `internalGetInfo`）：

1. 前置拒绝：blacked-out / 不处理该 path（handle*、include/exclude）→ 404/409（同 §2）。
2. **checksum 后缀请求（`.sha1`/`.md5`/...）一律不回源**：直接 404 `"Checksums are not downloadable."`——校验和只来自缓存条目或本地计算。
3. 查**负缓存**（miss cache）：期内已知 miss → 直接 404，不打上游。
4. 查**本地缓存仓**（`{remoteKey}-cache`，见 §8.4）：命中且未过期 → 直接服务（不发上游请求）。
5. 缓存过期或缺失且 repo 在线 → 回源探测（默认 HEAD；`bypassHeadRequests` 时 GET）：
   - 上游 200：进入下载与保存（§7.3）。
   - 上游 404：写负缓存；**若本地有过期副本则仍回发过期副本**（"expired but serving"）。
   - 上游连接错误：repo 标记 assumed-offline（`assumedOfflinePeriodSecs` 静默）；`hardFail=true` → 抛错（500）；否则有缓存用缓存、无缓存 404（message 带 offline 状态）。
6. 显式 `offline=true`：只走缓存，无缓存 404。

置信度：高（主干逐分支可读；官方文档只描述字段不描述流程——**本流程为代码补充**）。

### 7.3 下载与保存（单飞与并发）

- 触发条件：缓存缺失/过期，或 `forceExpiryCheck`（matrix 参数）、或远程较新。
- **同 path 全局单飞**：按缓存 path 加锁；等待者超时后：metadata 类（可过期条目）→ **回发旧缓存副本**；制品类 → 并发中获胜者完成后从缓存服务。
- 拿到锁后**二次确认**缓存状态（可能在等锁期间被其它线程刷新）。
- 保存前执行与 local 上传相同的 `assertValidDeployPathAndPermissions`（缓存仓也是 local 仓语义）。
- 上游响应头 `X-Checksum-Sha1` / `X-Checksum-Md5` / `X-Checksum-Sha256` 被读为「original checksum」，与本地实测比对（§7.5）；无头的类型跳过。
- 缓存副本的时间戳比上游 Last-Modified 新 → 视为同内容，仅撤销过期标记（不重下）。

置信度：高。

### 7.4 缓存失效与清理

| 机制 | 行为 | 置信度 |
|---|---|---|
| 制品新鲜期 | `retrievalCachePeriodSecs`（默认 2h）内不回源；过期后下次请求触发 HEAD 校验 + 条件下载 | 高 |
| metadata 类文件 | 内部 metadata（`maven-metadata.xml` 等）按更短周期对待；maven 场景 `MavenMetadataChecker` 判定可过期性 | 高 |
| 负缓存 | miss 记录 TTL `missedRetrievalCachePeriodSecs`（30min）；`RemoveFromCaches`/Zapping 端点可清 | 高 |
| 手动失效 | REST `POST /api/repos/{repo}/...` 清缓存族（Zapping remote cache，见 rest-api.md 增补） | 中 |
| 协议级负缓存 | PyPI 包索引 miss 独立内存缓存 8h/5 万条（maven-npm-pypi.md §3.6）——pro 补充 | 高 |
| assumed-offline | 故障静默 5min，期内全部请求绕过上游 | 高 |

### 7.5 remote checksum 策略（`checksumPolicyType` 四值）

| 值 | 上传/缓存写入时（original=上游头值，actual=本地实测） | 读取时暴露 | 置信度 |
|---|---|---|---|
| `generate-if-absent`（默认） | original 缺失 → 接受；original 存在且 ≠ actual → 拒收 | actual | 高 |
| `fail` | original 必须存在且 = actual，否则拒收 | actual | 高 |
| `ignore-and-generate` | 不校验，一律接受 | actual | 高 |
| `pass-thru` | 不校验 | original（原样透传上游声明） | 高 |

### 7.6 上游故障矩阵

| 故障 | 行为 | 置信度 |
|---|---|---|
| 连接超时/重置 | putOffline() → assumed-offline 5min；有缓存（含过期）→ 服务缓存；无 → 404 | 高 |
| `hardFail=true` 时同上 | 抛异常 → 500（对客户端表现为错误而非 404） | 高 |
| 上游 404 | 负缓存 30min；过期缓存副本仍在 → 回发旧副本 | 高 |
| 上游 4xx/5xx 其它 | 视同连接错误处理（unfound + offline 标记路径分支） | 中 |
| 凭据失败（401/403 上游） | 资源 unfound（404 透传给客户端，Docker 场景有专用文案）；remote 状态打点 | 中 |

---

## 8. virtual 仓库语义（M3）

> 基线：OSS `o.a.a.repo.virtual.VirtualRepo`、`VirtualRepositoriesResolver`、`VirtualResolverRequestFilter`、`o.a.a.repo.virtual.interceptor.MavenMetadataInterceptor`、`o.a.a.engine.UploadServiceImpl`（pro 反编译）+ pro `c.j.ph.*.command.virtual.*`。

### 8.1 解析顺序（搜索顺序）

成员列表展开（含嵌套 virtual 递归展开、按被嵌套 virtual 的 include/exclude 过滤）后**四桶拼接**：

1. `priorityResolution=true` 的 local（含 federated）与 remote 的 **cache 仓**——按成员声明顺序；
2. `priorityResolution=true` 的 **remote** 仓本体；
3. 非优先的 local/cache 仓；
4. 非优先的 remote 仓本体。

要点：
- **cache 仓与所属 remote 一起入序**（cache 先于 remote 本体，同优先级内），remote 本体内部再走 §7.2 流程。
- 下载类解析「首命中即停」；`priorityResolution` 的意义是让标了优先的成员整体前置。
- 请求来自另一台 Artifactory（smart remote 拉取，header 可识别）且 `artifactoryRequestsCanRetrieveRemoteArtifacts=false` → 远端仓不参与（只搜 local 成员）。
- 嵌套 virtual 的 include/exclude 不匹配该 path 时，其整个子树被剪掉。

置信度：高（四桶装配代码完整）。

### 8.2 写路由（PUT/DELETE 到 virtual）

| 场景 | 行为 | 置信度 |
|---|---|---|
| virtual 配置了 `defaultDeploymentRepoRef` | PUT 路由到该 local 仓，后续与直接部署该仓一致（响应 Location 等用目标仓） | 高 |
| virtual 未配 local deployment repo | **405** + `Allow: GET` 头，body `No local repository was configured as local deployment repository for the (<key>) virtual repository.` | 高 |
| repoKey 不是 local/virtual（如 remote） | 404 `Could not find a local repository named <key> to deploy to.` | 高 |
| DELETE virtual 下路径 | 逐成员查找实际持有者删除（M3 细节待逆向增补） | 中 |

### 8.3 per-protocol 的 virtual 聚合差异（关键：并非统一机制）

| 协议 | 元数据聚合 | 缓存 | 置信度 |
|---|---|---|---|
| Maven/Gradle/Ivy | `maven-metadata.xml` GET 拦截：按 §8.1 顺序逐仓（跳过 cache 仓本体；snapshot metadata 跳过 `handleSnapshots=false` 成员）拉取同名文件，**内存合并**：versions 去重重排、latest/release 重算、snapshot 取大、v3 并 snapshotVersions；**一旦某优先成员已产出即停止非优先成员**（foundByPriority 短路） | **不缓存**，每次现算 | 高 |
| npm | 包文档合并：首成员为基底、后续版本 putIfAbsent、dist-tags/time 并集（maven-npm-pypi.md §2.6） | 合并结果存 virtual cache（`<key>-cache` 仓 `.npm/` 下），TTL `virtualRetrievalCachePeriodSecs`（默认 600s，**<600 视为禁用**） | 高 |
| PyPI | simple 索引逐仓收集后条目合并；格式不一致整体回退 HTML | 索引翻译缓存（≥30s 检索周期）+ 包级 miss 负缓存 8h | 高 |
| Generic/Docker | 无聚合元数据；纯下载首命中 | — | 高 |

### 8.4 存储布局：virtual 与 remote 的隐藏仓

| 规则 | 规格 | 置信度 |
|---|---|---|
| remote 缓存仓 repoKey | `<remoteKey>-cache`（`RepositoryServiceImpl` 直接拼接；配置服务逆向解析 `-cache` 后缀） | 高（M1 待验证 #4 关闭） |
| virtual 缓存仓 repoKey | 同规则 `<virtualKey>-cache`，存 npm/PyPI 聚合缓存 | 高 |
| 可见性 | cache 仓不出现在普通浏览/搜索（内部仓标记），REST 直接指定其 key 可访问 | 中 |
| 清理 | cache 仓的制品删除/GC 与 local 相同；remote 缓存清理任务只清未引用条目 | 中 |

### 8.5 浏览与搜索聚合

virtual 目录 GET = 各成员（含 `listRemoteFolderItems=true` 的 remote）条目按 §8.1 顺序合并去重；搜索同理限定成员集合。M3 可后置（BinFlow 控制台 M4 才需要）。中。

## 9. 与官方文档的差异 / 补充

- 「同 checksum 幂等重传不触发覆盖权限检查」：官方文档未记载（只有代码可见），对 CI 重试场景行为关键 → **本条为反编译补充官方规范**。
- BlackedOutException 最终 HTTP 状态（404 vs 409）：官方文档无记载；本规格标注中置信度待动态验证。
- 回收站属性名（`trash.*` 五项）：官方 UI 有展示但字段名未见文档，属代码补充。

M3 增补（remote/virtual）：

- **remote pull-through 全流程**（§7.2 的 6 步、负缓存/过期回发/assumed-offline 静默）：官方文档只给字段表不给流程语义 → 全部为代码补充官方规范。
- **remote 不代理 checksum 文件**（`Checksums are not downloadable.`）：官方未记载，直接影响 Maven/npm 客户端兼容 → 代码补充。
- **virtual 四桶搜索序**（§8.1）：官方只说 priorityResolution「优先」，四桶精确语义（cache 与 remote 同进退、同优先级内声明序）为代码补充。
- **per-protocol 聚合非统一机制**（§8.3）：官方文档从不区分——Maven 现算不缓存 vs npm 缓存合并 vs PyPI 收集合并，是行为差异最大的一处，代码补充。
- **M1 §6 勘误**：includes/excludes 拒绝码为 **下载 404 / 上传 409**（`IncludeExcludeException` 显式双值，`RealRepoBase` 可见），M1 记的统一 404 不准确。
- **Maven snapshot policy 拒绝码 409**（M1 标注「推断」）：`SnapshotPolicyException#getErrorCode` 显式 409，确定。

## 待验证清单

1. blacked-out repo 上传的确切 HTTP 状态（404 推断自 RepoRejectException 默认值，未动态复现）。
2. ~~includesPattern 不匹配时的确切状态码~~ 已收敛（T-59）：下载 404 / 上传 409（`IncludeExcludeException` 构造参数双值显式可见）；逐字文案仍未见，待动态捕获。
3. `DELETE /{repoKey}`（仓库根，非空仓库）在 admin 下的行为分支（全删 or 需二次确认——REST 层 `DELETE /api/repositories/{key}` 才是删仓库；此处仅删内容，需与 repo 删除区分）。
4. ~~remote-cache 仓在 DB 中的 repoKey 命名（`<key>-cache`）~~ 已验证（T-59）：`RepositoryServiceImpl`/`RepoConfigRootFolderKeyExtractor` 直接拼 `key + "-cache"`，virtual cache 同规则。

M3 新增待验证：

5. virtual DELETE 逐成员删除的确切遍历与部分失败语义（§8.2，标中，未走读完整实现）。
6. remote 上游 5xx（非 404）的分支细节——走读确认与连接错误同路，但未逐分支复现（§7.6 标中）。
7. cache 仓经 REST 直接访问（`GET /{key}-cache/...`）是否受权限/可见性约束（§8.4 标中）。
