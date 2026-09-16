# REST API 行为规格（M1 子集）

> 逆向基线：artifactory-pro 7.161.16（见 `reverse-src/artifactory/README.md`）。
> 置信度标注：`高` = 反编译代码 + JFrog 官方文档双证；`中` = 仅反编译代码可见；`低` = 推断待验证。
> 官方参考：JFrog REST API 文档（docs.jfrog.com/artifactory/reference/*，如 deployartifact / deleteartifact / createrepository）。

## 0. 总则

| 条目 | 行为 | 置信度 |
|---|---|---|
| URL 布局 | `<context>/api/...` 为管理 API；`/<repoKey>/<path>`（无 `api` 前缀）为制品直传直取。Artifactory 默认 context 为 `/artifactory`（如 `/artifactory/api/system/ping`）。BinFlow 需同时暴露两种前缀。 | 高（`o.a.a.webapp.servlet.RepoFilter`、`RequestUtils#extractRepoKey`：路径首段命中已存在 repoKey 即路由到存储引擎，否则交给 REST/UI 链） |
| 错误响应体 | 所有 REST 层错误统一 JSON：`{"errors": [{"status": <int>, "message": "<string>"}]}`，pretty-print，`Content-Type: application/json`（`o.a.a.util.HttpUtils#sendErrorResponse`、`o.a.a.rest.ErrorResponse`）。制品直传直取路径（非 /api）同样用该格式。 | 高 |
| 日期格式 | 时间戳序列化为 ISO8601 带毫秒与时区，如 `2026-08-17T12:34:56.789+08:00`（`RestUtils#toIsoDateString` → Joda `ISODateTimeFormat.dateTime()`）。 | 高 |
| 自定义媒体类型 | JFrog 用 vendor 类型（如 `application/vnd.org.jfrog.artifactory.storage.FileInfo+json`），但一律同时接受 `application/json`；客户端 Accept 不兼容时返回 406。 | 高 |

---

## 1. 制品上传 / 下载 / 删除（无 `/api` 前缀）

### 1.1 端点表

| 方法 | 路径 | 语义 | 成功 | 主要错误 | 置信度 |
|---|---|---|---|---|---|
| PUT | `/{repoKey}/{path}` | 上传文件；body 为内容 | 201 + `Location` 头 + ItemCreated JSON | 见 1.2 | 高 |
| PUT | `/{repoKey}/{path}/`（尾斜杠） | 创建目录（MKDir） | 201 + ItemCreated（FolderInfo 形态） | 403 无 annotate 权限 | 高 |
| PUT | `/{repoKey}/{path}.sha1/.md5/.sha256` | 上传「校验和文件」→ 为目标文件登记客户端 checksum | 201（无 body，带 Location） | 409 不匹配 / 404 目标不存在 | 高 |
| GET | `/{repoKey}/{path}` | 下载文件 | 200 + 内容流 | 见 1.4 | 高 |
| HEAD | `/{repoKey}/{path}` | 元信息 | 200 无 body，同 GET 头 | 同 GET | 高 |
| GET | `/{repoKey}/{path}?properties` | 只取属性（JSON） | 200 JSON | 404 无属性 | 高 |
| GET | `/{repoKey}/{path}?propertiesXml` | 只取属性（XML） | 200 XML | 404 无属性 | 高 |
| DELETE | `/{repoKey}/{path}` | 删除文件/目录树 | 204 无 body | 404 / 403 | 高 |
| DELETE | `/{repoKey}/{path}?atomic=true` | 单事务删除 | 204 | 同上 | 中 |
| DELETE | `/{repoKey}/{path}:properties`（旧写法） | 删除目标条目的属性 | 204 / 404 | — | 中（`o.a.a.repo.webdav.methods.DeleteMethod`） |

### 1.2 上传语义流程（`o.a.a.engine.UploadServiceImpl#processUpload`）

1. repoKey 为空 → `404 "No target local repository specified in deploy request."`
2. 目标 repo 不存在：local/remote-cache 不存在 → `404 "Could not find a local repository named <key> to deploy to."`；目标是未配 local deployment repo 的 virtual → `405` + `Allow: GET` 头（**高**，官方文档亦记载）。
3. 路径以 `:properties` / `:statistics` 结尾（旧元数据写法）→ 上传主流程直接 `409 "Old metadata notation is not supported anymore"`。**置信度：高**
4. 权限：无 deploy 权限 → 403（body 文案 `User <u> is not permitted to deploy '<path>' into '<repoPath>'.`）；若请求是匿名 → 返回 401 + `WWW-Authenticate: Basic realm="Artifactory Realm"` 挑战。**高**
5. 配额超限 → 413（文案含 `Datastore disk usage is too high...`）。**中**
6. repo `blackedOut=true` → 拒绝（`BlackedOutException`，文案 `The repository '<key>' is blacked out and cannot serve artifact '<repoPath>'.`，经 RepoRejectException 默认状态 404）。**中**
7. 落盘成功 → 201，响应头 `Location: <contextUrl>/<repoKey>/<path>`、`X-Checksum-Sha256: <sha256>`（有值时），body 为 FileInfo JSON（Content-Type `application/vnd.org.jfrog.artifactory.storage.ItemCreated+json; charset=UTF-8`），字段：`uri`（JSON 序列化名，Java 字段名 slf）、`downloadUri`、`repo`、`path`（`/` 开头）、`created`、`createdBy`、`size`（**字符串**）、`mimeType`、`checksums{sha1,md5,sha256}`、`originalChecksums{...}`。**高**

### 1.3 上传请求头（`o.a.a.util.HttpUtils#getSha1Checksum` 等）

| 头 | 语义 | 置信度 |
|---|---|---|
| `X-Checksum-Sha1` / `X-Checksum-Sha256` / `X-Checksum-Md5` | 客户端声明校验和；服务端流式计算后比对 | 高 |
| `X-Checksum` | 未带类型时的通用头，按长度（40=sha1 / 64=sha256 / 32=md5）自动识别 | 高 |
| `X-Checksum-Deploy: true` | checksum-only 部署：不传 body，要求服务端 filestore 已有该内容。两个专用头都缺 → `400 "Checksum deploy failed. no checksum header 'X-Checksum-Sha1/X-Checksum-Sha256' was found."`；checksum 格式非法或 blob 不存在 → 404。成功仍回 201（响应标记 checksumDeployed，外部镜像链路用它区分状态；对外 HTTP 状态一致）。 | 高（代码）；官方文档对 `X-Checksum-Deploy: false` 行为亦有记载 |
| `X-Artifactory-Last-Modified` | epoch 毫秒，覆盖制品 lastModified（>0 才生效） | 中（`UploadServiceUtils#setItemLastModifiedInfoFromHeaders`） |
| `Expect: 100-continue` | 服务端先查 sha1/sha256 是否已有 blob，命中则不发 body 请求、复用已有内容（去重上传加速） | 中 |

上传属性（matrix 参数）：`PUT /repo/path;key=v1,v2;key2=v3`，`;` 后的 `k=v` 对作为属性随文件保存；key 后缀 `+`（如 `;lic+`）表示强制属性；路径与参数部分均 URL-decode（UTF-8），`+` 在路径里保留。**高**（`ArtifactoryRequestBase#calculateRepoPath`、`processMatrixParams`；官方文档 "Deploy Artifact" 亦记 matrix properties）。

### 1.4 下载响应头与条件请求（`o.a.a.request.RequestResponseHelper`、`o.a.a.webapp.servlet.HttpArtifactoryResponse`）

| 行为 | 规格 | 置信度 |
|---|---|---|
| 成功响应头 | `ETag: <sha1>`（无引号）、`Last-Modified`、`Accept-Ranges: bytes`、`X-Checksum-Sha1` / `X-Checksum-Sha256` / `X-Checksum-Md5`（有值才发）、`X-Artifactory-Filename`（URL-encoded 文件名）、`Content-Type`（按 mimetypes.xml 后缀映射，未知默认 `application/octet-stream`） | 高 |
| Range / If-Range | 支持单区间；命中 → 206 + `Content-Range`；区间非法 → 416 + `Content-Range: bytes */<total>` | 高 |
| If-None-Match / If-Modified-Since | 命中 → 304 无 body；ETag 比较容忍 `W/` 前缀与引号 | 高 |
| 文件名非 ASCII | 追加 `Content-Disposition: attachment; filename="<ascii>"; filename*=UTF-8''<enc>`（仅当 repo 关闭 archive browsing 或浏览器直开被禁时） | 中 |
| 404 | 文案 `Failed to find the repository '<key>' specified in the request.` / `Failed to find the requested resource '<repoPath>'.` | 高 |
| 403→401 | 拒绝码 403 且当前匿名 → 401 挑战 | 高 |
| 校验和文件请求 | `GET /repo/file.jar` 之外的 `.sha1/.md5/.sha256` 后缀请求 = 登记客户端校验和（同 PUT 语义，见 1.1 第 3 行） | 中 |

### 1.5 校验和文件（`.sha1` 等）上传细节（`UploadServiceImpl#validatePathAndUploadChecksum`）

- body 为纯文本 checksum；Content-Length > 1024 → `409 "Suspicious checksum file, content length of N bytes is bigger than allowed."`。
- 目标 `<file>.sha1` → 主文件 `<file>`；写入「客户端原始 checksum」。
- 与服务端实测不一致：repo 策略为 `client-checksums` → `409 "Checksum error for '<path>': received '<x>' but actual is '<y>'"`；策略为 `server-generated-checksums` → 仍 201（容忍）。
- 目标文件不存在 → 404；目标是目录 → 409。

---

## 2. 仓库 CRUD（`/api/repositories`）

路由类：`o.a.a.rest.resource.repositories.RepositoriesResource`（@Path `repositories`），实现逻辑在 `o.a.a.addon.rest.RestAddonImpl`。

| 方法 | 路径 | 语义 | 成功 | 错误 | 置信度 |
|---|---|---|---|---|---|
| GET | `/api/repositories` | 列出当前用户在 repo 根上有读权限的仓库 | 200，`Cache-Control: no-store` | — | 高 |
| GET | `/api/repositories?type=&packageType=&project=` | 过滤；type 取值 `local/remote/virtual/federated`；非法 type/packageType → **空数组**（不报错） | 200 | — | 高 |
| GET | `/api/repositories/{repoKey}` | 取单仓配置 | 200 repo config JSON | 不存在 → 404 纯文本 `The repository <key> was not found`（新版本默认；旧开关下 400 空 body） | 高 |
| GET | `/api/repositories/configurations?packageType=&repoType=` | 全量配置（admin only） | 200，按 type 分组、组内按 key 排序，`Cache-Control: no-store` | 403 信封 | 高（详见 §2.1.1） |
| PUT | `/api/repositories/{repoKey}` | 创建 | 200 纯文本 `Successfully created repository '<key>' \n` | 400 校验失败；body 内 `key` 与路径不一致 → 400（create）/ 409（update） | 高 |
| POST | `/api/repositories/{repoKey}` | 更新 | 200 纯文本 `Repository <key> update successfully.\n` | 404 不存在；409 key 冲突；400 其它 | 高 |
| DELETE | `/api/repositories/{repoKey}` | 删除 | 200 + 报告体 | 400 空 key（`Repo key must not be empty`）；404（`Repository <key> does not exist`）；403 删最后一个非系统 local repo（`Deleting the last local repository is not allowed`） | 高 |

列表条目字段（`o.a.a.repo.RepoDetails`）：`key`、`description`、`type`（LOCAL/REMOTE/VIRTUAL/FEDERATED，小写输出）、`url`（`<contextUrl>/<key>`）、`packageType`、`configuration`（仅共享配置的 remote 有值）。排序：type 升序再 key 升序。**高**

local repo 配置 JSON 关键字段（创建/读取共用，`o.a.a.repo.LocalRepositoryConfigurationImpl`）：

```
key, rclass("local"), packageType("generic"), description, notes,
includesPattern("**/*"), excludesPattern(""), repoLayoutRef,
blackedOut(false), handleReleases(true), handleSnapshots(true),
maxUniqueSnapshots(0), snapshotVersionBehavior("unique"),
checksumPolicyType("client-checksums"), archiveBrowsingEnabled(false),
propertySets([])
```
（`rclass` 在 REST JSON 中用大写枚举 `LOCAL` 的映射由 JAXB/Json 注解决定；对外文档惯例为 `"rclass":"local"`。）**高（字段与默认值）/ 中（个别包类型专属字段仅特定 packageType 序列化时出现）**

### 2.1 D02 P1 群：配置族五面（configurations / v2 配置读 / batch 族 / existence / 布局面）（L025-1，2026-09-16）

置信度总则：标「高」= 反编译 7.161.24 + 参照 7.161.15 活体双源（部分另加官方 v2 文档三源）；标「中」= 仅反编译或仅活体单源；标「低」= 推断待验证。
证据锚：活体 = 本票 curl 探针（scratch 仓/用户 `l025p-*` 用毕删净，见 reports/agents/L025-1.md Tests）；反编译定位命令见该报告 Commands；官方页 = docs.jfrog.com/artifactory/reference/{getallrepositoryconfigurations, getrepositoryconfigurationv2, createmultiplerepositories, updatemultiplerepositories, deleterepository}（2026-09-16 实取）。

#### 2.1.0 端点表

| # | 方法 | 路径 | 权限 | 成功 | 错误 | 置信度 |
|---|---|---|---|---|---|---|
| 1 | GET | `/api/repositories/configurations?packageType=&repoType=` | admin（7.61.3+） | 200 按类型分组对象 | 403 | 高 |
| 2 | GET | `/api/repositories/existence?projectKey=&type=` | 目标项目管理员 | 200 exists 对象 | 400 / 403 | 高 |
| 3 | GET | `/api/v2/repositories/{key}` | admin 全量；非 admin 部分字段 | 200 v2 schema | 404 | 高 |
| 4 | GET | `/api/v2/repositories/batch?names=a&names=b` | resource 级 admin/user | 200 map | 400 | 高 |
| 5 | PUT | `/api/v2/repositories/batch`（**批建**） | admin | 201 text/plain | 400 | 高 |
| 6 | POST | `/api/v2/repositories/batch`（**批改**） | admin | 200 json 字符串 | 400 / 403 / 404 | 高 |
| 7 | DELETE | `/api/v2/repositories/batch`（批删，body=键数组） | admin | 200 / 207 / 首失败码 | 400 | 高 |
| 8 | GET 等 | `/api/repo_layouts[/{name}]`（官方旧挂载） | — | — | 404（挂载已撤） | 高 |
| 9 | GET | `/api/admin/repolayouts[/{key}]`（实际挂载） | any-project-admin（读）/ admin（写） | 200 | 500（缺名） | 高（读臂）/ 中（写臂） |

**勘误（本票定案，matrix/rest-compat-matrix 行描述需随票修行）**：① batch 族动词映射 = **PUT 批建 / POST 批改 / DELETE 批删 / GET 批读**——与 v1 单仓动词同向（PUT=create、POST=update）；旧 inventory/rest-compat-matrix 行写作「POST 批建 / PUT 批改」是反的。② existence 查询参数实名 `projectKey`；`project` 被静默忽略。③ `/api/repo_layouts` 在 7.161.15 已 404（反编译全库无该路径串、官方新索引无页）；布局的真实挂载是 `/api/admin/repolayouts`（UI-rest 族，官方未文档化）。

#### 2.1.1 GET /api/repositories/configurations（全量配置，admin）

行为句式：

- 当客户端以 admin GET `/api/repositories/configurations` 时，服务端返回 200，体为按仓类型分组的对象：顶层键 = `LOCAL` / `REMOTE` / `VIRTUAL` / `FEDERATED` / `RELEASE_BUNDLE`（大写；仅返回实际存在该类型时有键），组内数组按 key 升序，`Cache-Control: no-store`，`Content-Type: application/vnd.org.jfrog.artifactory.repositories.RepositoryConfigurationsList+json`，pretty-print。**高**（活体 + 反编译 + 官方页三源）
- 每条配置字段 = 「公共字段集」17 键：`key, packageType, description, notes, includesPattern, excludesPattern, repoLayoutRef, signedUrlTtl, priorityResolution, projectKey(有项目才出现), environments, blackedOut, propertySets, archiveBrowsingEnabled, downloadRedirect, cdnRedirect, xrayIndex, xrayDataTtl, rclass`（rclass 小写；null 字段省略）——与 v2 读的字段集同源但含 `rclass` 不含 `type`。**高**（活体）
- 当客户端带 `packageType=generic,buildinfo`（逗号分隔）或 `repoType=local,remote` 时，服务端做 OR 过滤（两种过滤可叠加）；当过滤值不匹配任何仓时返回 200 空对象 `{}`（不报错）。**高**（活体：comma 双命中实测；官方页明示 comma 分隔）
- 当客户端以非 admin 调用时，服务端返回 403，体 = errors 信封 `{"errors":[{"status":403,"message":"Forbidden"}]}`。**高**（活体 + @RolesAllowed(admin) 反编译 + 官方页「Requires a user with admin permissions」）
- 此条补充官方规范：官方页未载 403 信封文案与空对象行为、未载顶层键大写形态。

#### 2.1.2 GET /api/repositories/existence（项目×类型探测，7.103+）

- 当客户端 GET `/api/repositories/existence?projectKey=default&type=local` 时，服务端返回 200 `{"exists":<bool>,"matchingRepoTypes":["LOCAL"],"projectKey":"default"}`（pretty-print；`matchingRepoTypes` 为请求类型的解析结果，无 type 参数时回显全部五类且顺序不稳定）。**高**（活体）
- 当客户端带 `type=local&type=remote`（重复参数）时按 OR 生效；带 `type=local,remote`（逗号）时返回 400 errors 信封 `"Invalid repository type: local,remote"`；带未知值同样 400 `"Invalid repository type: <v>"`。**高**（活体）
- 当客户端带 `project=<v>` 时该参数被**静默忽略**（响应 projectKey 恒为 default 或 projectKey 参数值）——参数实名 `projectKey`。**高**（活体双臂判别）
- 当客户端对目标项目非项目管理员（admin 恒通过，含不存在的项目名）时返回 403 errors 信封 `Forbidden`；此时 `exists:false` + `projectKey:<原名>` 回显属于 admin 视角（admin 对任意 projectKey 均放行）。**高**（活体 + 反编译 isProjectAdmin 门）
- 此条补充官方规范：官方无独立文档页；参数名、逗号拒绝、admin 对不存在项目的回显均为反编译+活体补充。

#### 2.1.3 GET /api/v2/repositories/{key}（v2 配置读）

- 当客户端 GET（Accept: application/json）已存在的仓时，服务端返回 200 pretty-print 配置，`Content-Type` 恒为按 rclass 的 vendor 类型（`application/vnd.org.jfrog.artifactory.repositories.<Rclass>RepositoryConfiguration+json`），`Cache-Control: no-store`。**高**
- v2 schema 与 v1（GET /api/repositories/{key}）的差异：**`rclass` 改名为 `type`**；剔除全部包类型专属字段。local/generic 实测 v2 = 16 键：`key,type,packageType,description,notes,includesPattern,excludesPattern,repoLayoutRef,signedUrlTtl,priorityResolution,environments,blackedOut,propertySets,archiveBrowsingEnabled,downloadRedirect,cdnRedirect,xrayIndex,xrayDataTtl`；remote 另含 url/username/password/disableProxy/hardFail/offline/storeArtifactsLocally/socketTimeoutMillis/localAddress/retrievalCachePeriodSecs/assumedOfflinePeriodSecs/missedRetrievalCachePeriodSecs/metadataRetrievalTimeoutSecs/unusedArtifactsCleanupPeriodHours/shareConfiguration/synchronizeProperties/listRemoteFolderItems/allowAnyHostAuth/enableCookieManagement/propagateQueryParams/blockMismatchingMimeTypes/bypassHeadRequests/disableUrlNormalization/contentSynchronisation{…}/sendContext/passThrough/curated/retrieveSha256FromServer/customHttpHeaders；virtual 仅 9 键：`key,type,packageType,description,notes,includesPattern,excludesPattern,signedUrlTtl,environments,repositories,hideUnauthorizedResources,artifactoryRequestsCanRetrieveRemoteArtifacts`。**高**（活体三 rclass 全采样）
- 当客户端读不存在的 key 时返回 404 errors 信封 `{"errors":[{"status":404,"message":"The repository <key> was not found"}]}`。**高**（活体；反编译裸 text/plain 被 errors 信封过滤器包裹——活体为准）
- 当客户端以非 admin（但有基本凭据）读时返回 200 **部分字段**（实测 remote 仅 5 键：`key,type,packageType,description,url`）——官方页「Non-admin users receive only partial configuration data」的落地面。**高**（活体 + 官方页双源）
- 类型协商怪癖：协商输入是请求的 **Content-Type 而非 Accept**——GET 时带 `Content-Type: <Remote vendor>` 对 local 仓返回 406 errors 信封 `Not Acceptable`，而带不匹配的 `Accept:` 头返回 200。**高**（活体双臂判别；此条补充官方规范——官方未载协商键为 Content-Type）

#### 2.1.4 GET /api/v2/repositories/batch（批读）

- 当客户端 GET `/api/v2/repositories/batch?names=a&names=b` 时，服务端返回 200 map：键 = 仓 key（按找到的），值 = **v1 全量 schema 配置**（含 `rclass`，字段集与 GET /api/repositories/{key} 逐键一致，null 省略），`Content-Type: application/json`，`Cache-Control: no-store`。**高**（活体逐键 diff：61/61 一致）
- 不存在的 names 被静默省略（不报错）；`names=a,b` 逗号串按**单个 key** 处理（通常整体落空 → `{}`）。**高**（活体）
- 当客户端不带 names（或空串）时返回 400 errors 信封 `"Repository keys are missing."`。**高**（活体）
- 当 names 数量超过 100（`repo.config.rest.create.items.limit` 默认）时返回 400 `"Repository item limit exceeded: {N}. Limit: {100}"`。**中**（仅反编译；活体未造 101 仓）
- 此条补充官方规范：批读端点官方无文档页。

#### 2.1.5 PUT /api/v2/repositories/batch（批建——全有或全无）

- 当客户端 PUT 一个合法配置数组（每项必含 `key`，body 为 JSON 数组）时，服务端按序创建并返回 **201**，`Content-Type: text/plain`，体 = 每仓一行 `Successfully created repository '<key>' `（行尾带空格+`\n`），行间以额外 `\n` 连接（实测两仓体 = `Successfully created repository 'a' \n\nSuccessfully created repository 'b' \n\n`）。**高**（活体逐字节）
- 批内任一配置校验失败 → **整单 400**，已建项回滚（实测：含已存在 key 的批次中，同批新 key 未被创建）。多错误以 `\n` 连接合并为单条 message（errors 信封内）。**高**（活体 + 官方页「the entire batch request will fail」双源）
- 逐字错误文案（errors 信封）：已存在 key → `error when validating repository name: <key> : Repository key already exists`；缺 key → `Repository key are missing in configuration`（原文如此，"key are"）。**高**（活体）
- 数组超过 100 项（`repo.config.rest.update`…create 限同值 100）→ 400 `Repository item limit exceeded: {N}. Limit: {100}`。**中**（仅反编译）
- 创建顺序内部规则：federated 项先于其余 rclass 批（反编译 sortByPriorities）。**低**（仅反编译且未活体判别顺序可观察性）
- 此条补充官方规范：201 体逐字节形态、回滚保证、逐字 400 文案官方均未载。

#### 2.1.6 POST /api/v2/repositories/batch（批改——**merge 方言**）

- 当客户端 POST 合法数组（每项必含 `key`）时，服务端对每仓做**合并更新**（省略字段保留存量、显式值覆盖——与单仓 POST /api/repositories/{key} 的 ADR-0050 update-merge 方言同语义；实测：只发 `xrayIndex:true` 的仓，其 description 原值保留）。**高**（活体 + 反编译 handleUpdateRepos bean 合并）
- 成功返回 200，`Content-Type: application/json`，体 = 裸字符串 `Repositories updated successfully.`（无引号、非信封）。**高**（活体）
- 当批内含不存在的 key 时返回 **404**，体 = **裸文本**（非信封）`No repositories found for the following keys: <a, b>`，CT application/json；整单无副作用。**高**（活体）
- 当客户端以 Content-Type 指定某 vendor 类型（如 Remote vendor）而批内仓不属该 rclass 时，同样落入 404 上述文案（既有仓按 vendor 类型过滤检索）。**中**（反编译 getExistingRepoConfigs；活体仅验证 application/json 通吃）
- 非授权仓 → 403 `User is not authorized to update the following repositories: <a, b>`；缺 key → 400 `Repository key are missing in configuration`；超 100 项 → 400 limit 文案同上。**中**（仅反编译；活体未造非 admin 批改臂）
- 此条补充官方规范：merge 语义与裸 404 文案官方均未载（官方仅说「entire batch fails」）。

#### 2.1.7 DELETE /api/v2/repositories/batch（批删——207 混合态状态机）

body = JSON 字符串数组 `["a","b"]`（重复键去重、保序）。状态机（置信度：高 = 反编译 + 活体臂双源；官方式 207 文档仅覆盖单删报告形状）：

1. **空数组/缺 body** → 400，体 = `{"statusMessage":"No repository keys were provided for deletion"}`（无 reports 键）。**高**（活体）
2. **预校验段（整单中止臂）**：任一键为 blank/trash/support-bundle、导入进行中、无删除权限、配置校验器否决 → 整单立即返回该单仓状态码，体 = 单报告 `{"statusMessage":"<该仓消息>"}`（无 reports 键、无逐仓展开）。实测非 admin 臂：403，`statusMessage = "Cannot delete repository: 'x', Reason: User: ('u') has insufficient permission to delete repositories: x"`。**高**（活体 + 反编译 validateAllRepositories 抛出路径）
3. **逐仓删除段**：不存在的键**不失败**，产出 success:true 报告，`statusMsg = "Cannot delete repository: '<key>', repository config does not exist"`；删除锁被占（并发删同仓）同样 success:true，`statusMsg = "Cannot delete repository: '<key>', repository deletion is already in progress"`（官方单删语义里的 202 在批内退化为 success 形态）。**高**（活体并发双删实测）
4. **聚合规则**：全部报告无 error/warning（**含全部 ghost 与 202 形态**）→ **200** `statusMessage="All repositories were removed successfully"`；真失败与成功并存 → **207** `"Some repositories failed to be removed"`；全部真失败 → 返回首个失败仓的状态码。**高**（200/ghost 聚合活体实测；207/全败 = 反编译 calcStatusCode + 官方 207 文档双源，活体触发待验证）
5. 成功响应体：`{"reports":[{"repoKey","statusMsg","deletedArtifactsCount","success":true}],"statusMessage":…}`；真失败报告额外含 `"deleteArtifactsFailureCount"` 与 `"errors":[{"status","message"}]`（上限 50 条示例，`artifact.delete.report.failure.examples.max`）且 `success:false`。`reports` 为 HashSet，**顺序不稳定**。**高**（成功形态活体；失败形态 = 反编译 + 官方单删 FailureReport 文档）
6. 成功文案：真实仓 `Repository '<key>' and all its content have been removed successfully.`（local/federated） / `Repository '<key>' has been removed successfully.`（virtual）。**高**（活体两 rclass）
7. 该端点受 `remove.repository.flow.v2.enabled`（默认 **true**）门控：关闭时返回 400 `Delete multiple repositories v2 is not supported. Please contact support`。**高**（反编译常量默认值 + 活体默认路径行为吻合）

**联动发现（上报 D02-R05 复核）**：同一开关令**单仓** DELETE /api/repositories/{key} 在 7.161.15 也走 v2 报告形态——实测成功 = 200 报告体（`{"repoKey","statusMsg","deletedArtifactsCount","success":true}`），不存在 = **404 + success:true 报告体**（`statusMsg="Cannot delete repository: 'x', repository config does not exist"`），而不再是旧纯文本消息。§2 表 DELETE 行的「404 `Repository <key> does not exist`」为废弃流文案，现行以本节为准。**高**（活体两臂）

#### 2.1.8 布局面（repo_layouts 定案）

- 当客户端 GET `/api/repo_layouts` 或 `/api/repo_layouts/{name}` 时，服务端返回 404 errors 信封（`Not Found` / `Not found`）——官方旧挂载在 7.161.15 已撤（反编译全库无该路径串；官方新索引无页）。**高**（活体 + 反编译双源）
- 实际挂载 = `/api/admin/repolayouts`（UI-rest 族，官方未文档化）：GET 列表 → 200 数组，每项 `{name, artifactPathPattern, layoutActions}`；实测内置 **25** 个布局（maven-2-default / simple-default / npm-default / …见测试记录）。**高**（活体）
- GET `/api/admin/repolayouts/{name}` → 200 完整布局对象：`{name, artifactPathPattern, distinctiveDescriptorPathPattern, descriptorPathPattern, folderIntegrationRevisionRegExp, fileIntegrationRevisionRegExp, repositoryAssociations:{localRepositories:[],remoteRepositories:[],virtualRepositories:[]}}`。**高**（活体）
- 布局名不存在 → **500** errors 信封 `"No value present"`（内部 Optional 直取未捕获——bug 兼容点）。**高**（活体）
- 写臂（POST 建于集合、PUT 改、DELETE `/api/admin/repolayouts/{name}`、POST `testArtPath`、POST `resolveRegex`）= admin 门（读臂 any-project-admin）。**中**（仅反编译；活体未动全局布局）
- matrix D02-R12 行 capability 措辞（`/api/repo_layouts[/{name}]` CRUD）需改写为上述现实——归 compatibility-engineer。

---

## 3. 制品信息 / 属性 / 统计（`/api/storage`）

路由类：`o.a.a.rest.resource.artifact.ArtifactResource`（@Path `storage/{path:.+}`）。

| 方法+查询参数 | 语义 | 成功响应 | 错误 | 置信度 |
|---|---|---|---|---|
| GET `/api/storage/{repoKey}/{path}` | 文件 → FileInfo；目录 → FolderInfo | 200 JSON（见下） | 404 `Unable to find item`；无读权限 → 403（或配置 hideUnauthorizedResources=true 时伪装 404） | 高 |
| GET `...?properties=K1,K2*` | 取属性（可过滤 key，`*` 通配） | 200 `{"uri": "...", "properties": {"k": ["v"]}}` | 无任何属性 → 404 `No properties could be found.` | 高 |
| GET `...?stats` | 下载统计 | 200 StatsInfo：`uri, downloadCount, lastDownloaded, lastDownloadedBy, remoteDownloadCount, remoteLastDownloaded, remoteLastDownloadedBy` | 404 | 高 |
| GET `...?lastModified` | 目录内最新修改项 | 200 `{"uri":..., "lastModified": "yyyy-MM-dd'T'HH:mm:ss.SSSZ"}` + `Last-Modified` 头 | 非 local/cached repo → 400 | 高 |
| GET `...?permissions` | 有效权限（manage 位持有者专属） | 200 `{"uri":..., "principals":{"users":{"<主体名>":["r","w",...]},"groups":{"<组名>":[...]}}}`——**key=主体名、value=权限字母集合**；字母全集 r/w/n/d/m（read/deploy/annotate/delete/manage；BinFlow 子集 r/w/d）；无任何权限的主体不出现 | 非 local/cached 仓 → 400 `This method can only be invoked on local/cached repositories.`；local 仓但 item 不存在 → 404 `Unable to find item '<repoPath>'.`；无 manage 权限 → 403 | 高 |
| GET `...?list[&deep&depth&listFolders&mdTimestamps&statsTimestamps&includeRootPath&includePropertiesMd5]` | 流式文件清单（**仅认证用户**；七参按整数解析） | 200 FileList JSON（`uri/created/files[{uri,size,lastModified,folder,sha1,sha2?,mdTimestamps?,propertiesMd5?}]`，条目 uri 前导斜杠；`Content-Type: application/vnd.org.jfrog.artifactory.storage.FileList+json`） | 匿名 → 403；七参值非整数或越 int32 界 → 400 envelope `For input string: "<v>"`；目标是文件 → 400 `Expected a folder but found a file, at: <repo>:<path>`；repo 不存在 → 404；仓库根 → 200 可列 | 高（逐项见下勘误块） |
| PUT `/api/storage/{repoKey}/{path}?properties=k=v,k2=v1;v2&recursive=&atomic=` | 设属性 | 204 无 body | 属性名为空 → 400 `Properties value cannot be empty.`；属性名非法字符 → 400 | 高 |
| DELETE `/api/storage/{repoKey}/{path}?properties=k1,k2&recursive=` | 删属性 | 204 | 未指定属性 → 400 `Unspecified properties to delete.` | 高 |
| POST `/api/storage/{repoKey}/{path}`（旧「Update Item Properties」形态） | —（**7.161.x 已移除**） | — | **405** errors-envelope `Method Not Allowed`（L024-1 活体逐字；反编译 ArtifactResource 无 @POST 互证） | 高 |
| PATCH `/api/metadata/{repoKey}/{path}?recursiveProperties=`（**现行增量改属性**，6.1.0+） | body `{"props":{...}}` 增量改属性（值=串/串数组/`null`=删键）；亦接受 `{"stats":{...}}` | 204 无 body | 见 §3.1 | 高 |
| DELETE `/api/metadata/{repoKey}/{path}?recursive=` | 删**全部**属性 | 204 | **失败臂无守卫**：item 无属性 → 204；item 不存在 / virtual 仓 → 仍 **204 静默 no-op**（L024-10 m10c/m10d 活体定谳，与 PATCH 族 400 文案不同——勿借文案） | 高 |

> **勘误（T-113）**：本表 `?permissions` 行原记「key 为 r/w/d/a 权限位，value 为主体名集合」——键值方向相反，且字母集笔误（annotate 的字母是 `n` 非 `a`，另有 manage=`m`）。正确形态以修正后行为准。依据：`RestAddonImpl#getItemPermissions` 构造 主体名 → 权限字母集合 的映射，逐主体调 `#appendPrincipalsAndPermissions`（**空集合跳过**——无任何权限的主体不出现）；字母集见 `ArtifactoryPermission` 枚举（r/w/n/d/m）；官方 REST 文档 Get Item Permissions 示例同形（`"users":{"bob":["r","w","n"]}`）——代码与官方文档双证。
>
> **非 local 仓 404-vs-400 疑点核实（T-113 同批）——结论 400**（置信度：中，两层调用链代码直读；动态复现可选留 T-103）：资源层前置校验先于 addon——`ArtifactResource#preparePermissionsResponse` 对非 local/cached 仓（virtual、remote 本体、不存在的仓）抛 400；`RestAddonImpl#getItemPermissions` 内 `ItemNotFoundRuntimeException("Unable to find local repository '<key>'.")` → 404 为**二道门**（判定与资源层同源，正常请求面不可达，仅并发删仓等分叉窗口可触）。正常可达的 404 是「local 仓 + 路径不存在」（`Unable to find item '<repoPath>'.`，显式 catch 转 `NotFoundException`，全局 `ItemNotFoundExceptionMapper` 亦 404）。校验次序：Accept 不兼容 406 → 非 local 仓 400 → 无 manage 权限 403 → item 不存在 404 → 200。BinFlow 现行「非 local → 400」与规格一致，无需改码（T-97 review NB6「疑 404」不成立）。

> **勘误（T-L010-3）——`?list` 行原记三处误**：①「根目录 → 400 `Cannot list files of root.`」（实际根可列）；②「`uri` 为相对查询目录的路径」（实际一律前导斜杠）；③ 未记七参整数校验与形态五项。依据：L008-3 双端活体差分 32 臂（参照 Artifactory Pro 7.161.20）+ 反编译（`ArtifactResource` 参数经 `getQueryParameterAsInt`：containsKey+isNotBlank 短路 → `Integer.parseInt`，`IllegalArgumentException` → 400）+ 官方 Get Storage Item Information 页（docs.jfrog.com/artifactory/reference/getstorageitem）三源交叉；BinFlow 侧 T-L009-2 已按此实现并 32 臂复验。**正确行为规格（当客户端…服务端返回…）**：
>
> 1. **触发与校验**：当客户端 GET 带 `?list`（值无关，`list=` 空值同）时服务端进入列表模式；`deep`/`depth`/`listFolders`/`mdTimestamps`/`statsTimestamps`/`includeRootPath`/`includePropertiesMd5` 七参一律按整数解析——值非整数字面（`abc`/`true`）或越 int32 界（>2147483647 / <-2147483648）时返回 400 errors-envelope `For input string: "<v>"`（Java parseInt 文案逐字），边界值 ±2^31 内正常处理。**高**（非整数臂 A09/A29/A32；越界臂 2026-09-12 参照活体抽验 `depth=2147483648`/`-2147483649` 双双 400 同文案）。
> 2. **空值缺席**：七参为空值或纯空白（`depth=`、`depth=%20`）时视为缺席，不进整数解析，返回正常列表。**高**（2026-09-12 参照活体抽验三臂全 200；反编译 isNotBlank 短路同证）。
> 3. **递归语义**：`deep=1` 是唯一递归触发（其他整数值 0/2 = 不递归）；`depth` 仅作修饰——无 `deep=1` 时任意 `depth` 值均无递归效果（`depth=99` 仍只列直接子文件），`deep=1&depth=N` 把递归钳到 N 层，`depth≤0`（含缺省 0）不限层。**高**（A02–A11/A31 实测 + 官方 "Optional depth to limit the results for deep listing" 印证修饰定位）。
> 4. **listFolders=1**：文件夹行进入 files[]——`uri` 无尾斜杠（`/d2`）、`size: -1`、`folder: true`、无 sha，与文件行按字母序混排；缺省/0 时任何深度都不出现文件夹行。**高**（A12–A14/A23 实测 + 官方 "Include folders in listing"）。
> 5. **includeRootPath=1**：被查询文件夹自身作为**首个条目**出现——`uri: "/"`、size -1、folder true。**高**（A18–A20/A23/A27 实测 + 官方记载）。
> 6. **mdTimestamps=1**：条目（文件与文件夹）增 `mdTimestamps: {"properties": <属性最后修改时刻>}`；无属性条目整键缺席。**高**（A15–A17/A23 实测 + 官方 "Include metadata timestamp values"）。
> 7. **statsTimestamps=1**：文件条目增 `mdTimestamps: {"artifactory.stats": max(lastDownloaded, remoteLastDownloaded)}`，从未下载则缺席。**高**（A30 实测；**此条补充官方规范**——官方页未记载该参）。
> 8. **includePropertiesMd5=1**：有属性条目增 `propertiesMd5: <md5>`；无属性条目整键缺席。**高**（A21–A23 实测；**此条补充官方规范**——官方页未记载该参）。
> 9. **形态五项**（作用于所有 200 响应）：条目 `uri` 一律**前导斜杠**（`/f1.txt`）；顶层 `uri` 无尾斜杠、仓库根为裸仓 key（`.../api/storage/<repoKey>`）；`created` = **请求时刻墙钟**（毫秒，逐请求推进，非文件夹创建时刻）；`Content-Type: application/vnd.org.jfrog.artifactory.storage.FileList+json`；全列（含 deep 全树）按路径字母序（`/d2/d3/d4/f5.txt` 先于 `/d2/d3/f4.txt`）。**高**（L008-3 §4 实测 + 官方 FileList 示例 uri 前导斜杠/size "-1"/vendor CT 同形）。
> 10. **根与错误体裁**：仓库根可列——`?list` 打在仓库根返回 200 直接子文件，deep/listFolders/includeRootPath 组合在根上同样成立；400 `Cannot list files of root.` 仅属**无仓库段**的请求（带仓路由下不可达）；目标是文件 → 400 envelope `Expected a folder but found a file, at: <repo>:<path>`（repo:path 冒号拼写，非斜杠）；匿名 → 403；repo 不存在 → 404。**高**（A24–A27 实测；匿名 403 = 反编译 `AuthorizationRestException` + 官方 "Requires a non-anonymous privileged user"）。
> 11. **校验次序**：参数整数校验先于目标解析——坏参与文件目标同报时 400 返回坏参文案（`For input string: …`）。**中**（反编译代码序：资源层参数解析先于 addon 目标解析；BinFlow 侧 T-L009-2 优先级臂测试同序，参照侧未活体取证）。

FileInfo JSON 字段（`o.a.a.api.rest.artifact.RestFileInfo` + `RestBaseStorageInfo`）：`uri`、`downloadUri`、`remoteUrl`（仅 remote-cache 有）、`repo`、`path`（`/` 前缀）、`created`、`createdBy`、`lastModified`、`modifiedBy`、`lastUpdated`、`size`（字符串）、`mimeType`、`checksums{sha1,md5,sha256}`、`originalChecksums{sha1,md5,sha256}`、`properties`（有则带）。FolderInfo：同基础字段 + `children:[{uri:"/<name>", folder:bool}]`（按名排序）。**高**

`recursive` 参数缺省规则：目标是目录 → 默认递归；文件 → 默认非递归；显式传 `0/1`（非布尔字符串）。**中**（`PropertiesAddonImpl#getRecursive`）

### 3.1 增量属性面（L024-1，2026-09-16；置信度：高——反编译 + 参照 7.161.15 活体双源）

**迁移事实**：官方 6.1.0 引入的「Update Item Properties」现行形态是 **`PATCH /api/metadata/{repoKey}/{path}`**（官方 reference 页 + 7.161.15/7.161.24 反编译资源类双证）；旧 `POST /api/storage/{repoKey}/{path}?recursive&atomic` 形态**在 7.161.x 已不存在**——活体 POST 打 /api/storage 逐字回 405 `{"errors":[{"status":405,"message":"Method Not Allowed"}]}`。主矩阵 D01-R08 行的路径描述需照此更新（上报 conductor）。

行为句式（当客户端…服务端返回…）：

1. **PATCH + `{"props":{"k":["v1","v2"]}}`**（新键）→ 204；随后 `GET …?properties` 回 `"k":["v1","v2"]`。**高**
2. **PATCH 已有键** → 覆盖语义（删旧键再原子写新值，等价 delete+set）→ 204。**高**
3. **PATCH `{"props":{"k":null}}`** → 删除该键 → 204；键不存在亦 204（幂等）。**高**
4. **PATCH `{}`（props/stats 双缺）** → 400 envelope `"props or stats fields required"`（逐字）。**高**
5. **PATCH 值为非串非数组 JSON**（如 `{"props":{"n":5}}`）→ 400 envelope `"Failed to set properties on <repo>:<path>: Failed to parse json object while performing patch properties request."`（逐字，含句号）。**高**
6. **PATCH 目标不存在** → 400 envelope `"Failed to set properties on <repo>:<path>: Item <repo>:<path> does not exist"`（注意：**400 非 404**；文案 `repo:path` 冒号拼写）。**高**
7. 数组元素须为字符串——非文本元素（数字等）被静默跳过（反编译，中）。
8. `recursiveProperties` 语义同 PUT 族 `recursive`（目录缺省递归/文件非递归/`0/1`）；**官方文档列的 `atomicProperties` 参数在 7.161.24 反编译资源层不读取（忽略）**——差异登记。**中**
9. 执行序 = 先删（null 与被改键）→ 再改 → 后增（任一步失败短路返回其错误码）。**高**（反编译；活体只验终点）
10. `stats` 腿（**L024-10 定谳，推翻 L024-1 的「低置信未活体」登记**）：PATCH `{"stats":{"downloadCount":1}}` → 204，且**真 JSON-merge 落库**（非 no-op）——`downloadCount` 直写；`lastDownloadedBy` 写 **`"import"` 标记**（非调用者用户名）；`lastDownloaded`/`remoteLastDownloaded` 维持 0。随后 `?stats` 回显**六字段全量**：`uri`（= **repo-root 下载形** `<base>/<contextUrl>/<repo>/<path>`，非 api/storage 形）、`downloadCount`、`lastDownloaded`（epoch-0 整数 0，非 null）、`lastDownloadedBy`、`remoteDownloadCount`、`remoteLastDownloaded`——零值字段**整体保留不省略**。**高**（l024e-wire m09/m09b 双单元活体）。
11. **三动词权限门**：PATCH/PUT/DELETE 均要求 annotate；无权限 → 403（PATCH 走 403 带文案 `Request for '<repoPath>' is forbidden for user: '<u>', You must have annotate permission on this path`；PUT/DELETE 403 裸）。**高**（反编译）
12. **virtual/remote 仓上的属性写**（活体逐字，2026-09-16）：PUT `/api/storage` 属性 → **404 envelope `"Not Found"`**（资源层裸 404 状态经全局 mapper 包 envelope——非空体）；PATCH `/api/metadata` → **400 envelope `"Failed to set properties on <repo>:<path>: Repository '<repo>' is not a local repository"`**（与「item 不存在」文案不同）。**高**
13. **怪癖**：PATCH 的 400 文案在资源层以纯文本构造，但对外包装进标准 errors envelope（`message` 内为纯文案）——活体定案（反编译 entity 与活体 envelope 双证）。**高**

> **L024-4 差分回填注记（2026-09-16）**：§3 表 `PUT /api/storage/{repoKey}/{path}?properties=` 行为 jf build-publish 主链的**剩余断链点**——jf「Setting properties…」步实发 `PUT /api/storage/<path>`（props 经 URL、bytes_in=0），BinFlow 未实现（404 `"…is not implemented in BinFlow"`）；参照侧审计 `PROPERTY_UPDATED` 实证。该行规格本体（204 / `Properties value cannot be empty.` / 非法字符 400）不变，证据锚补 `reports/compatibility/l024d-wire/{a,b}`（L024-search-aql-diff §2.6 / §3-L8）。同链余项 = `/api/system/version` 版本串（jf `strconv.Atoi` 尾错，version 债登记）。

> **L024-10 差分回填注记（2026-09-16，D01 尾差分）**：① `/api/metadata` 的**其它动词**（PUT/GET/POST）→ **405** errors-envelope `"Method Not Allowed"` + **`Allow: DELETE,OPTIONS,PATCH` 头**（m14a/b/c 三臂活体逐字）——与 POST /api/storage 405 同族；无仓段 POST → 404 `Not Found`（L010-2 面维持）。② DELETE 失败臂无守卫与 stats 腿真 merge 已分别回填上表与 §3.1-10。③ `?properties` GET 的 `Cache-Control` 头与 405 `Allow` 头为「A 带 B 缺」无语义面（normalize 提案 PN-hdr drop，L024-10 §0）。证据锚 `reports/compatibility/l024e-wire/{a,b}/`（L024-d01-tail-diff，31 单元）。

---

## 4. 搜索（M1 仅 checksum 搜索）

| 方法 | 路径 | 参数 | 成功 | 错误 | 置信度 |
|---|---|---|---|---|---|
| GET | `/api/search/checksum?sha1=&md5=&sha256=&repos=a,b` | 至少一个 checksum；repos 可选限定范围 | 200 `{"results":[FileInfo...]}` | checksum 非法 → 400；未认证 → 403 | 高 |

（`o.a.a.rest.resource.search.types.ChecksumSearchResource`；官方文档 searchChecksum 同口径。）

---

## 5. System 类端点

| 方法 | 路径 | 语义 | 成功 | 错误 | 置信度 |
|---|---|---|---|---|---|
| GET | `/api/system/ping` | 存活探测（**免认证**） | 200 纯文本 `OK` | HA lockdown → 503 `Server state is offline`；数据/日志目录不可写或 DB/内部服务异常 → 500（body 为异常消息） | 高 |
| GET | `/api/system/version` | 版本 | 200 JSON `{version, revision, servicesVersions?, addons?, license?, entitlements?}`（BinFlow 最少实现 `version`+`revision`） | 匿名可被策略禁止 → 403 | 高 |
| GET | `/api/system/serverTime` | 服务器时间 | 200 纯文本 epoch 毫秒 | — | 中 |
| GET | `/api/system/configuration` | 全局配置 XML（admin） | 200 `application/xml`（脱敏 proxy/mail 密码字段） | — | 中 |
| POST | `/api/system/configuration` | 提交新配置 XML（admin） | 200 纯文本 `Reload of new configuration succeeded` | XML 非法 → 400 | 中 |
| POST | `/api/system/storage/gc` | 手动 GC | 200 / 流式状态 | — | 低（M1 不要求） |
| POST | `/api/system/storage/prune/start` | 清理无引用 blob | 202 `Pruning Unreferenced Data task has been submitted`；权限不足 → 403 | — | 中 |
| GET | `/api/system/storage/prune/status` | prune 状态 | 200 / 412（无任务） | — | 中 |

---

## 6. 与官方文档的差异 / 补充（此节为反编译补充项）

- **PUT 上传成功响应**：官方文档只写 201；反编译补充 body 为 FileInfo 形态 JSON、`Location` 与 `X-Checksum-Sha256` 头（`SuccessfulDeploymentResponseHelper`）。
- **错误体格式**：官方文档多数端点未给出错误 body 结构；反编译确认统一 `{"errors":[{status,message}]}`。
- **virtual 仓库 PUT → 405 + `Allow: GET`**：官方文档未显式记录该头；代码可见（`UploadServiceImpl#sendInvalidTargetRepositoryError`）。
- **checksum 文件上传（`.sha1` 旁车文件）行为**：官方文档分散记载；反编译确认 1024 字节上限、409 文案、与 repo checksum 策略的联动。
- **`?list` 家族补充面**：官方 Get Storage Item Information 页未记载 `statsTimestamps`/`includePropertiesMd5` 两参、仓库根可列行为、参数整数校验（非整数/越 int32 界 → 400 `For input string: "<v>"`、空值/空白=缺席）、文件目标 400 文案（`Expected a folder but found a file, at: <repo>:<path>`）与 `Cannot list files of root.` 仅属无仓段请求——反编译 + 活体差分补充（T-L010-3 勘误块，L008-3 32 臂）。
- **GET `/api/repositories/{key}` 不存在的 400/404 双态**：官方文档写 404；代码显示由 `respondWith404ForNonExistentRepo` 开关控制（新版默认 404）。BinFlow 实现选 404。
- **D02 配置族五面（§2.1，L025-1）**：官方未载面——批读端点（GET batch）、existence 参数实名 projectKey 且拒绝逗号、批建 201 体逐字节与整单回滚、批改 merge 方言与裸 404 文案、批删 ghost/202 形态在批内记 success:true、`/api/repo_layouts` 挂载已撤（现实体 `/api/admin/repolayouts`）、布局缺名 500 `No value present`、v2 读以 Content-Type（非 Accept）做类型协商——均反编译+活体双源补充。官方已载面：configurations admin 门与 comma 过滤、v2 读非 admin 部分字段、批建/批改整单失败语义、单删 207 报告形状（deleterepository 页）。

## 待验证清单（低置信度项）

1. `DELETE ...?atomic=true` 与默认多事务路径在部分失败时的中间态可见性（需动态验证：删除目录树中途 kill 进程，观察残留）。
2. `POST /api/system/storage/gc` 响应流式格式（M1 不实现，留 M4）。
3. `checksumDeployed` 标记是否在任何对外可见头/状态码体现（代码内仅镜像链路消费，推测对外无差异）。
4. `?list` 坏参与文件目标同报时参照侧的校验次序（现按反编译代码序推导：参数解析先于目标解析；BinFlow 侧已按此测试——勘误块第 11 条，LOOP 010 合并差分可覆盖）。
5. `?list&includeRootPath=1` 打在仓库根时根条目 `/` 的 `lastModified` 字段形态（L008-3 A27 参照侧字段级未取证；BinFlow 现渲染 1970 零时刻，见 T-L009-2 Risks ①）。
6. FileList 文件行的 `sha2` 字段是否随行列出（旧规格记 `sha2?`；L008-3 32 臂证据面只到 sha1 逐字级，未单独取证 sha2 在列）。
7. 批删真 207 与全败聚活体触发（§2.1.7-4：需可复现的逐仓真失败——联邦 424 需配 Base URL、内容删失败需保护属性；现 207/全败聚合 = 反编译状态机 + 官方单删 207 文档双源，标高但缺活体臂）。
8. 批建/批改 >100 项 limit 400 文案（`Repository item limit exceeded: {N}. Limit: {100}`）——仅反编译，未造 101 仓实测。
9. 批建 federated-first 内部排序的可观察性（§2.1.5）。
10. POST 批改的 403 非授权文案与 vendor Content-Type 过滤 404（§2.1.6 两中置信臂）。
11. `/api/admin/repolayouts` 写臂三动词与 testArtPath/resolveRegex 的响应形态（§2.1.8，仅反编译）。
12. `deletedArtifactsCount` 计数语义（40 文件实测回填 80——疑似含文件夹/校验和伴生项，未定）。
