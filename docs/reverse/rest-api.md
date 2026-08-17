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
| GET | `/api/repositories/configurations?packageType=&repoType=` | 全量配置（admin only） | 200，按 type 分组、组内按 key 排序，`Cache-Control: no-store` | — | 高 |
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

---

## 3. 制品信息 / 属性 / 统计（`/api/storage`）

路由类：`o.a.a.rest.resource.artifact.ArtifactResource`（@Path `storage/{path:.+}`）。

| 方法+查询参数 | 语义 | 成功响应 | 错误 | 置信度 |
|---|---|---|---|---|
| GET `/api/storage/{repoKey}/{path}` | 文件 → FileInfo；目录 → FolderInfo | 200 JSON（见下） | 404 `Unable to find item`；无读权限 → 403（或配置 hideUnauthorizedResources=true 时伪装 404） | 高 |
| GET `...?properties=K1,K2*` | 取属性（可过滤 key，`*` 通配） | 200 `{"uri": "...", "properties": {"k": ["v"]}}` | 无任何属性 → 404 `No properties could be found.` | 高 |
| GET `...?stats` | 下载统计 | 200 StatsInfo：`uri, downloadCount, lastDownloaded, lastDownloadedBy, remoteDownloadCount, remoteLastDownloaded, remoteLastDownloadedBy` | 404 | 高 |
| GET `...?lastModified` | 目录内最新修改项 | 200 `{"uri":..., "lastModified": "yyyy-MM-dd'T'HH:mm:ss.SSSZ"}` + `Last-Modified` 头 | 非 local/cached repo → 400 | 高 |
| GET `...?permissions` | 有效权限 | 200 `{"uri":..., "principals":{"users":{...},"groups":{...}}}`（key 为 r/w/d/a 权限位，value 为主体名集合） | 非 local/cached → 400 | 高 |
| GET `...?list&deep=&depth=&listFolders=&mdTimestamps=&statsTimestamps=&includeRootPath=&includePropertiesMd5=` | 流式文件清单（**仅认证用户**） | 200 `{"uri":..., "created":..., "files":[{uri,size,lastModified,folder,sha1,sha2?,mdTimestamps?,propertiesMd5?}]}`，`uri` 为相对查询目录的路径 | 匿名 → 403；根目录 → 400 `Cannot list files of root.`；目标是文件 → 400；repo 不存在 → 404 | 高 |
| PUT `/api/storage/{repoKey}/{path}?properties=k=v,k2=v1;v2&recursive=&atomic=` | 设属性 | 204 无 body | 属性名为空 → 400 `Properties value cannot be empty.`；属性名非法字符 → 400 | 高 |
| DELETE `/api/storage/{repoKey}/{path}?properties=k1,k2&recursive=` | 删属性 | 204 | 未指定属性 → 400 `Unspecified properties to delete.` | 高 |
| POST `/api/storage/{repoKey}/{path}?recursive=&atomic=`（PATCH 语义 v2） | 增量改属性 | 204 | 同上 | 中 |

FileInfo JSON 字段（`o.a.a.api.rest.artifact.RestFileInfo` + `RestBaseStorageInfo`）：`uri`、`downloadUri`、`remoteUrl`（仅 remote-cache 有）、`repo`、`path`（`/` 前缀）、`created`、`createdBy`、`lastModified`、`modifiedBy`、`lastUpdated`、`size`（字符串）、`mimeType`、`checksums{sha1,md5,sha256}`、`originalChecksums{sha1,md5,sha256}`、`properties`（有则带）。FolderInfo：同基础字段 + `children:[{uri:"/<name>", folder:bool}]`（按名排序）。**高**

`recursive` 参数缺省规则：目标是目录 → 默认递归；文件 → 默认非递归；显式传 `0/1`（非布尔字符串）。**中**（`PropertiesAddonImpl#getRecursive`）

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
- **`?list` 根目录 400 文案**与**匿名 403**：官方文档未写，代码可见。
- **GET `/api/repositories/{key}` 不存在的 400/404 双态**：官方文档写 404；代码显示由 `respondWith404ForNonExistentRepo` 开关控制（新版默认 404）。BinFlow 实现选 404。

## 待验证清单（低置信度项）

1. `DELETE ...?atomic=true` 与默认多事务路径在部分失败时的中间态可见性（需动态验证：删除目录树中途 kill 进程，观察残留）。
2. `POST /api/system/storage/gc` 响应流式格式（M1 不实现，留 M4）。
3. `checksumDeployed` 标记是否在任何对外可见头/状态码体现（代码内仅镜像链路消费，推测对外无差异）。
