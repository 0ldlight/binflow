# Docker Registry v2 行为规格（M2：补官方规范空白处）

> 逆向基线：artifactory-pro 7.161.16，`o.j.repomd.docker.v2.rest.*`（batch2-protocol）+ `o.a.a.addon.docker.*`。
> 官方规范（以此为准，本文不重复）：Docker Registry HTTP API V2（distribution.github.io/distribution/spec/api/，下称 **[DIST-API]**）与 OCI distribution-spec（github.com/opencontainers/distribution-spec，下称 **[OCI]**）。
> 本文只写 Artifactory 对规范的**补充/偏离行为**——M1 rest-api.md 风格延续。置信度：`高`=反编译+官方文档双证；`中`=仅代码；`低`=推断待动态验证。

## 0. URL 布局与路由（Artifactory 特有）

官方 [DIST-API] 假定注册表根为 `/v2/`；Artifactory 把 Docker API 挂在**仓库维度**下（`o.a.a.util.DockerInternalRewrite#getInternalRewrite`）：

| 访问方式 | 外部 URL | 内部转发 |
|---|---|---|
| 路径前缀法（reverse proxy method=repositoryPathPrefix，默认） | `https://host/<repoKey>/v2/...` | `/api/docker/<repoKey>/v2/...` |
| 子域法（method=subdomain） | `https://<repoKey>.host/v2/...` | `/api/docker/<repoKey>/v2/...`（子域首段=repoKey；IPv4 地址 Host 不做子域解析） |
| 直接 API 前缀 | `https://host/artifactory/api/docker/<repoKey>/v2/...` | 原样 |

**置信度：高**（代码逐分支可见；官方 JFrog Docker Registry 文档亦描述 path/subdomain 两种方法）。BinFlow M2 至少实现「路径前缀法 + 子域法」的 Host/转发语义，`/v2/` 兜底 ping 也要能应答（无 repoKey 时转发到 `/api/docker/v2/`）。

每个 v2 端点响应都会带 `Docker-Distribution-Api-Version: registry/2.0` 头（[DIST-API] 的 SHOULD，Artifactory 全端点强制）。**高**

## 1. 错误体形态（对 [DIST-API] §Errors 的补充）

官方定义 `{"errors":[{code,message,detail}]}`；Artifactory 的**手写 JSON 序列化**（`o.j.repomd.docker.v2.rest.errors.DockerV2Errors`）有以下偏离/细节：

| # | 行为 | 置信度 |
|---|---|---|
| 1 | 错误体一律单元素 errors 数组（不聚合多个缺失 blob——与官方「每个未知 blob 一条 BLOB_UNKNOWN」不同，Artifactory 的 MANIFEST 校验失败合并为一条 MANIFEST_INVALID，detail.description 携带原因） | 高 |
| 2 | `detail` 为对象时用 `"detail":{...}`、无内容时用 `"detail":null`（官方允许任意 JSON）；detail 的 key 因错误而异：`blobSum`（BLOB_UNKNOWN）、`description`（BLOB_UPLOAD_INVALID/MANIFEST_INVALID）、`manifest`（MANIFEST_UNKNOWN/UNAUTHORIZED-manifest）、`name`（NAME_UNKNOWN/NO_TAGS_FOUND）、`paths`（INTERNAL_ERROR 删manifest失败） | 高 |
| 3 | **非官方错误码**：`SIZE_INVALID`（400，"provided length did not match content length"）、`NO_TAGS_FOUND`（404，"No tags for image found in registry."）、`NOT FOUND`（注意：字面量含空格，用于 remote repo 凭据错误 404）、`INTERNAL_ERROR`（500 删 manifest 失败）。官方码表无这三项 | 高 |
| 4 | MANIFEST_INVALID 的 description 会做 `"`→`\"`、换行→`\n` 转义（手写 JSON 转义） | 高 |
| 5 | **私有响应头**：manifest 类错误（MANIFEST_INVALID/MANIFEST_UNKNOWN/UNAUTHORIZED-manifest）附 `Artifactory-Manifest-Handler-Error: true`，若 manifest 标识含 `/` 再附 `Artifactory-Manifest-Repo-Path`。客户端可忽略 | 中 |
| 6 | 错误响应显式带 `Content-Length`（= body 字节长度）与 `Content-Type: application/json` | 高 |

## 2. blob upload 会话（POST → PATCH → PUT）

### 2.1 会话标识与 Location 语义（`DockerV2LocalRepoHandler#startBlobUpload`）

| 行为 | 规格 | 置信度 |
|---|---|---|
| UUID 形态 | `UUID.randomUUID() + ".patch"` —— 即 `<uuid>.patch`，**不是纯 UUID**。Docker-Upload-UUID 头与 Location 路径都用这个带后缀形态。官方 [DIST-API] 说 uuid 匹配 `[a-zA-Z0-9-_.=]+`，`.` 合法，docker 客户端可接受 | 高 |
| 会话存储 | 会话即 `<image>/_uploads/<uuid>.patch` 这个**仓库内文件**；PATCH 追加=读旧内容+新流串接后重上传（`SequenceInputStream`），没有独立会话表 | 高 |
| Location 构造 | `DockerLocationUriHelper#getDockerURI`：优先 `X-Forwarded-Host`（多值取第一个；含端口则带端口），否则 `Host` 头（host[:port]）；scheme 取 `X-Forwarded-Proto`，**缺省 https**；路径 `v2/<image>/blobs/uploads/<uuid>`。最后经 `DockerInternalRewrite#rewriteBack` 按反代方法改写（子域法会把 host 部分改回 `<repoKey>.<domain>`） | 高 |
| 202 响应头 | `Location` + `Docker-Upload-UUID` + `Content-Length: 0` + `Docker-Distribution-Api-Version`（**无 Range 头**——官方 POST 202 示例含 `Range: bytes=0-<offset>`，Artifactory 省略；docker 客户端容忍） | 高 |
| POST 单体一步完成 | POST 带 `?digest=` 且 body 非空时：先正常 start（202），若成功立即内部调 PUT 完成逻辑（`DockerV2Resource#startBlobUploadOrCompleteUpload`），客户端一次请求得 201 | 高 |
| **GET 上传状态端点缺失** | [DIST-API] 定义 `GET /v2/<name>/blobs/uploads/<uuid>`（204+Range）；Artifactory 用 `GET .../uploads/<uuid>.patch` 这一**非标准路径**替代（返回 204 + Range `0-<len-1>`，见 `DockerBlobUploadHandler#getPatchedBlob`）。标准 GET 未见实现——**docker 客户端不用此端点，影响面小** | 中（路由表无标准 GET；`.patch` GET 存在） |
| DELETE 取消上传 | [DIST-API] 定义 DELETE uploads；`DockerV2Resource` 路由表未见 DELETE 处理器。取消依赖超时清理（§2.4） | 中 |

### 2.2 monolithic vs chunked 的切换判定（`DockerV2LocalRepoHandler#putHasStream` + `DockerBlobUploadHandler#patchBlobUpload`）

PUT 结束会话时，服务端要判定「PUT 带 body（单体）」还是「body 为空、收尾已 PATCH 的数据」：

1. 默认策略 `docker.push.by.blob.existence.check`（旧名）：看 `<image>/_uploads/<uuid>` 是否已存在——**不存在 → PUT 带 body（单体直传）**；存在 → 收尾。
2. 兜底策略（旧客户端兼容）：User-Agent 匹配 `^(?:docker/1\.(3|4|5|6|7(?!.[0-9]-dev))|Go ).*$` → 视为 PUT 带 body（老 docker 总在 PUT 里重发全量）。
3. PATCH 侧：`artifact==null && contentRange==null` → **全量流式上传**（stream upload，官方 [DIST-API] "Stream upload"）；有 Content-Range → 分块：首块（artifact 不存在）要求 range 起点必须 0，后续块起点必须等于已存字节数，否则 416（无 body）。
4. Content-Range 解析：按 `-` split 成 2 段，格式非法 → 416。

**置信度：高**（两个类分支完整可见）。BinFlow 建议：判定规则实现 1+3 即可（规则 2 是 2015 年老 docker 的 workaround）。

### 2.3 digest 校验与落盘

| 行为 | 规格 | 置信度 |
|---|---|---|
| PUT 收尾校验 | 服务端实测 sha256 与 `?digest=` 不符 → 删掉临时文件 + `400 BLOB_UPLOAD_INVALID`（description: `Uploading of temp blob '<path>' failed - checksum 'sha256:<x>' does not match the expected digest '<y>`，注意文案**缺右引号**，为 Artifactory 原样 bug，BinFlow 不必复刻） | 高 |
| 校验开关 | `shouldVerifyBlobChecksumBeforeCopy()` 常量控制（binarystore 层已校验时关闭） | 中 |
| 成功响应 | `201 Created` + `Location: <host>/v2/<image>/blobs/<digest>` + `Docker-Content-Digest`（+ PUT 收尾时 `Content-Length: 0`）；校验和属性按算法写到节点属性（`setAttribute(digest.getAlg(), hex)`） | 高 |
| blob 存储路径 | `<image>/_uploads/<alg>__<hex>`（digest.filename() = `alg__hex` 双下划线拼接）；manifest 引用后由 syncer 复制到 `<image>/<tag>/<alg>__<hex>` | 高 |
| 空层 blob | digest == `sha256:a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4`（32 字节 gzip 空 tar）→ GET/HEAD 直接合成响应，不查库 | 高 |

### 2.4 上传会话超时与清理（`DockerV2UploadsCleaner`）

| 行为 | 规格 | 置信度 |
|---|---|---|
| 过期阈值 | `docker.cleanup.maxAgeMillis` 默认 **86400000（24h）**——`_uploads` 下 modified 早于该阈值的文件被删 | 高（ConstantValues 默认值） |
| 清理周期 | `docker.cleanup.uploadsTmpFolderJobSecs` 默认 86400s（每日）；另 manifest PUT 成功后**同步**清一次 `<image>/_uploads`（`DockerWorkContext#cleanup`） | 高 |
| 实现 | AQL 查 `_uploads` 路径下过期项逐个 undeploy（跳过回收站） | 高 |

## 3. manifest PUT 校验链（`DockerManifestPutHandler#uploadManifest`）

按代码执行顺序：

| # | 校验 | 失败响应 | 置信度 |
|---|---|---|---|
| 1 | Content-Type → ManifestType；schema1 且 repo `blockPushingSchema1=true`（Docker repo + API V2 时默认 true） | **403**（非官方码表路径）body 文案 `Pushing Docker images with manifest v2 schema 1 to this repository is blocked. For more information visit https://www.jfrog.com/confluence/display/RTF/Advanced+Topics#AdvancedTopics-DockerManifestV2Schema1Deprecation` | 高 |
| 2 | 无写权限（manifest 路径） | 403 `No permission to write manifest` | 高 |
| 3 | 覆盖检查：tag 已存在（含另一种 manifest 文件名）且无 DELETE 权限，且 repo 开启 strict tag overwrite | 403 `No permission to overwrite manifest` | 高 |
| 4 | 解析 manifest（按 Content-Type 选 schema1/2/OCI 反序列化） | 解析异常 → 400 MANIFEST_INVALID（description=异常消息） | 高 |
| 5 | **mediaType 判定**：`ManifestType#from(MediaType)` 用请求 Content-Type 精确匹配 6 种媒体类型（`manifest.v1+json` / `v1+prettyjws` / `v2+json` / `list.v2+json` / `oci.image.manifest.v1+json` / `oci.image.index.v1+json`）；**不匹配任何一种时兜底按 Schema1Signed 处理**（然后被 #1 的 schema1 block 拦截——所以错误 Content-Type 的表象常是 403 blocked） | 高 |
| 6 | tag retention 策略（`dockerTagRetention` 默认 1）：旧 tag 不同 digest 时把旧 manifest 移到 `<image>/sha256__<digest>/`（retention>1 保留 N 版；retention=1 且 completeOverride 开启时走 temp 目录待覆盖） | 失败仅日志，不影响响应 | 中 |
| 7 | **blob 存在性**（`DockerManifestSyncer#sync`）：config blob + 每个 layer 依次确保 `<image>/<tag>/<file>` 存在——顺序：已在目标 → 用 `_uploads` 临时 → 全局 blob 搜索（跨 Docker repo 找可读副本，跨 repo mount 语义）→ 空层合成。foreign layer（`urls` 字段非空且 mediaType 为非本地层）**跳过** | 找不到 → `DockerManifestSyncException` → 400 MANIFEST_INVALID `Failed to copy blob <digest> to <path>`（**不是官方的 BLOB_UNKNOWN**——BinFlow 建议按官方回 BLOB_UNKNOWN 更兼容） | 高 |
| 8 | subject（OCI 1.1 referrers）解析：manifest JSON `subject.digest` 存入元数据 | 解析失败仅日志 | 高 |
| 9 | 落盘 `<image>/<tag>/manifest.json`（单 manifest）或 `list.manifest.json`（index/list）；附带属性：`docker.manifest.type`、`docker.label.*`（OCI labels，key 特殊字符归一为 `-`）、`docker.referrers.*`、sha256 等 | 上传失败 → 400 MANIFEST_INVALID | 高 |
| 10 | 成功 → `201` + `Location` + `Docker-Content-Digest`；**OCI subject 且 repo 开 referrers API → 追加 `OCI-Subject: <subject digest>` 头**（[OCI] 规定，BinFlow 必须实现） | — | 高 |

## 4. manifest GET/HEAD 的 Accept 协商（`DockerManifestGetHandler#chooseManifestType`）

官方 [DIST-API] "Pulling An Image Manifest"：客户端应带 Accept；Artifactory 的协商算法（对官方的补充细化）：

1. Accept 集合 = 所有 Accept 头映射为 ManifestType。
2. 若接受任一 list 类（list.v2 / oci index）且 `<image>/<tag>/list.manifest.json` 存在 → 返回 index/list（其 `docker.manifest.type` 属性必须在 Accept 内）。
3. 否则查 `manifest.json`：
   - 其 `docker.manifest.type` 在 Accept 内 → 用之；
   - **User-Agent 覆盖开关**：`oci.accepted.user.agents.list` 配置的 UA 前缀（或 `*`）→ 无视 Accept 直接返回存储类型（老客户端不带 OCI Accept 的兼容手段）；
   - 存储是 OCI 而 Accept 全是 OCI 之外的 docker 类型 → 404 manifest unknown；
4. 兜底顺序 Schema2 → Schema1Signed → Schema1；仍无 → 默认 Schema1Signed。
5. **schema2→schema1 转换**：存储为 schema2、Accept 不含 schema2（老 docker 客户端）→ 服务端把 manifest 转换为 schema1 signed 再返回（`DockerSchemaProvider#convertSchema2To1`）；转换失败则原样返回 schema2。
6. 响应头：`Content-Type` 强制设为所返回 manifest 的媒体类型 + `Docker-Content-Digest`。

**置信度：高**（分支完整）。**BinFlow 校准**：M2 不做 schema2→1 转换（2026 年无老客户端），但 OCI Accept 协商（#3 前半）与 `Docker-Content-Digest` 必须实现。

## 5. token 端点（docker login 流）

### 5.1 挑战（401）

`DockerV2AuthenticationFilter#sendAuthChallenge`：

- `WWW-Authenticate: Bearer realm="<servletContextUrl>/api/docker/<repoKey>/v2/token",service="<host>"`
  - servletContextUrl 依次取：可信的 `X-JFrog-Override-Base-Url` → `X-Artifactory-Override-Base-Url` → `X-Forwarded-Proto`+`X-Forwarded-Host`[+`X-Forwarded-Port`] → 请求原生 URL。service = URL `//` 与首个 `/` 之间（即 host）。
- scope 按端点推导（`DockerV2AuthUtil#getDockerV2ResourceEndpointInfo` 正则匹配路径）：manifests/blobs/tags → `repository:<image>:pull`（GET）或 `pull,push`（PUT/POST）；catalog → `registry:catalog:*`；DELETE → `repository:<image>:delete`。
- body：`{"errors":[{"code":"UNAUTHORIZED","message":"authentication required","detail":null}]}` + `Docker-Distribution-Api-Version` 头。
- 额外参数：请求带 `rerouteRepo` 时 token realm URL 追加 `?rerouteRepo=<v>`。

**置信度：高**（代码+官方 token spec 的 realm/service/scope 语义双证）。

### 5.2 token 签发（`DockerResourceBase#getToken`，`GET /api/docker/<repoKey>/{v1|v2}/token`）

| 行为 | 规格 | 置信度 |
|---|---|---|
| 参数 | `account`（缺省 `anonymous`）、`service`、`scope`（客户端原样传，Artifactory **不按 scope 逐项校验**，权限在资源端点上判）、`offline_token`（接受但走同一 provider，未见专门离线 token 分支——标低）、`client_id` | 中 |
| 认证 | 无 Authorization 且匿名访问未开启 → 401 `Authentication is required`；Basic 凭据经标准认证链 | 高 |
| 成功响应 | 200 JSON `{"token": "...", "expires_in": 3600, "issued_at": "..."}`（`AuthenticationModel`；issued_at 在 docker token 路径被置 null）；token 为内部签发的访问 token（可作 Bearer 用于后续请求） | 高（字段）/ 中（expires_in=3600 默认值） |
| 失败 | provider 错误透传（如 401 invalid credentials，OauthDockerErrorModel 包装，body 含 status/internalErrorMsg） | 中 |
| 兼容 | 另有 `GET /api/docker/<repoKey>/{v1|v2}/auth`（v1 遗留，返回 base64 凭据 JSON）——BinFlow 不实现 | 低 |

**BinFlow 校准**：实现标准 token 端点（realm 指向自身），scope 校验可宽松（资源端点已鉴权），`expires_in` 与内部 token TTL 对齐。

## 6. catalog 与 tags/list 分页（对 [DIST-API] Pagination 的补充）

| 行为 | 规格 | 置信度 |
|---|---|---|
| 数据源 | **缓存文件**而非实时查库：catalog = repo 根 `.jfrog/repository_v2.catalog`（旧 `repository.catalog`）；tags = `.jfrog/<image>/tags_v2.json`（旧 `tags.json`）。不存在时**首次请求同步构建**（syncCatalogIndex/syncTagsIndex）再读 | 高 |
| 权限过滤 | tags：逐 tag 检查 `<image>/<tag>/manifest.json` 或 `list.manifest.json` 可读，过滤后分页；catalog：`shouldFilterCatalogByUserPermission` 开启时同构过滤 | 高 |
| 分页算法 | TreeSet 字典序，`n` 取上限，`last` 之后（exclusive）取 n 条（`DockerCatalogTagsSlicer#sliceCatalog`）——与官方 `last` 语义一致 | 高 |
| Link 头 | 有更多元素时：tags `</v2/[<repoKey>/]<image>/tags/list?last=<last>&n=<n>>; rel="next"`；catalog `</v2/[<repoKey>/]_catalog?last=<last>&n=<n>>; rel="next"`（repoPath 请求模式下 URL 前缀 repoKey） | 高 |
| n 缺省 | `maxEntries==0` → 不切页返回全量（官方默认 100，**Artifactory 是全量**——大 repo 行为差异点，BinFlow 建议按官方 100 兜底） | 高 |
| n 非法 | 官方 `PAGINATION_NUMBER_INVALID` 400；Artifactory 未见该错误码（int 解析失败交给 JAX-RS → 404/400 框架错误） | 中 |
| tags 空 | 镜像存在但无 tag → 404 `NO_TAGS_FOUND`（官方建议 200 空数组；**Artifactory 偏离**——BinFlow 按官方 200 空数组实现更安全） | 高 |
| catalog 空 repo | 403（无根读权限）或 200 空 repositories | 高 |

## 7. HEAD 优化路径

| 行为 | 规格 | 置信度 |
|---|---|---|
| HEAD blob | 命中 → 200，头：`Docker-Content-Digest`、`Content-Length`、`X-Checksum-Md5/Sh1/Sha256`、`ETag`（=sha1）、`Content-Type: application/octet-stream`。**blob 搜索范围默认全 repo**（`dockerEnforceSearchBlobUnderImage` 关时 `findReadableBlob` 全局搜；开时限定 image 内） | 高 |
| **push 时的隐式 copy** | `dockerCopyBlobToUploadsEnabled` 开启时（默认 false），HEAD 命中外部路径的 blob 会**系统身份复制**到 `<image>/_uploads/`，加速后续 manifest PUT 的 blob 汇聚 | 高（默认值 ConstantValues `docker.copy.blobs.to.uploads.folder=false`） |
| HEAD manifest | 有专门优化路径（`isHeadManifestEnabled`）：直接读 artifact 元数据构响应（Content-Length + Docker-Content-Digest + Content-Type），**不流 manifest 内容**；关闭时退化为 GET。schema2→1 转换需求存在时仍会读内容转换 | 中（开关默认值未定位，推断默认开） |

## 8. 删除语义（对 [DIST-API] DELETE 的补充）

| 行为 | 规格 | 置信度 |
|---|---|---|
| DELETE manifest by digest | 搜出该 digest 的**全部** manifest 副本（跨 tag），逐一删 `<image>/<tag>` 目录；部分失败 → 500 `INTERNAL_ERROR`（detail.paths=失败清单）；成功 → 202。无 DELETE 权限 → 403 UNAUTHORIZED（detail.manifest=hex） | 高 |
| DELETE manifest by tag | **官方 spec 禁止 by-tag 删除**；Artifactory 实际支持：解析失败 digest 时按 tag 删 `<image>/<tag>/manifest.json` 所在目录（202）。BinFlow 建议按官方仅支持 digest | 高 |
| DELETE blob | 未见 DockerV2Resource 路由（官方 DELETE /v2/<name>/blobs/<digest>）——Artifactory 不支持单 blob 删除（依赖 GC） | 中 |

## 9. Referrers API（OCI 1.1，`OciReferrersHandler`）

| 行为 | 规格 | 置信度 |
|---|---|---|
| 端点 | `GET /v2/<image>/referrers/<digest>`（官方 [OCI] 路径） | 高 |
| 开关 | repo 未启用 referrers → 404 空 body | 高 |
| 数据源 | 索引文件 `<repo>/.jfrog/<image>/v1/referrers.json`；不存在则**首次请求同步构建** | 高 |
| 过滤 | `?artifactType=` 过滤；应用了过滤时响应头 `OCI-Filters-Applied: artifactType`（官方规定） | 高 |
| 响应体 | image index 形态（manifests 数组），`Content-Type: application/vnd.oci.image.index.v1+json`；subject 不存在 → 空 manifests 列表（200） | 高 |

## 10. 与官方规范差异汇总（BinFlow M2 的校准建议）

**建议按官方实现（不复刻 Artifactory 偏离）**：
1. manifest 校验失败缺 layer 时回官方 `BLOB_UNKNOWN`（400 + detail.digest），而非 Artifactory 的 MANIFEST_INVALID 文案。
2. tags 为空回 200 `{"name":..., "tags":[]}`，不用 404 NO_TAGS_FOUND。
3. manifest 删除仅支持 by-digest（405/400 拒绝 by-tag）。
4. 分页 `n` 缺省取 100，非法回 PAGINATION_NUMBER_INVALID。
5. 实现 GET 上传状态标准端点（204 + Range + Docker-Upload-UUID），`.patch` 变体可不做。
6. UUID 用纯 UUID（可含自定义后缀但无必要）；docker 客户端均容忍。
7. `Docker-Distribution-Api-Version: registry/2.0` 全端点带上（Artifactory 同）。

**建议照抄 Artifactory（实测兼容性关键）**：
1. 401 挑战的 realm 指向自身 token 端点，service=host，scope 端点推导规则（§5.1）。
2. blob 上传会话 = 仓库内 `_uploads` 文件；PATCH 追加语义（顺序错 416 空 body）；PUT 收尾 digest 校验失败删临时文件。
3. `_uploads` 24h 过期清理 + manifest PUT 成功后同步清理（防半推垃圾）。
4. 空 blob digest `sha256:a3ed95ca...` 直接合成 32 字节响应。
5. manifest 存储 `<image>/<tag>/manifest.json|list.manifest.json`、blob `<alg>__<hex>` 文件名、layer 从 `_uploads` 汇聚到 `<image>/<tag>/`（跨 repo blob 复用）。
6. HEAD manifest 走元数据快路径；HEAD blob 附全套 X-Checksum 头。
7. POST `?digest=` 单体一步完成（201 直返）。
8. `OCI-Subject` 响应头（referrers 场景）与 referrers 索引懒构建。

**待动态验证（低置信度）**：
- `offline_token` 参数是否有独立行为（走读未见分支）。
- HEAD manifest 优化开关（isHeadManifestEnabled）默认值。
- DockerV2RemoteRepoHandler（代理模式）的 v2 行为差异——M3 remote 逆向时补。
- token `expires_in` 实际值是否恒 3600。
