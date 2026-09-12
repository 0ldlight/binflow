# L000-B 证据期报告：Artifactory remote docker 代理 v2 行为测绘（U-PROTO-01）

- Ticket: L000-B / U-PROTO-01（docker remote 代理 v2 差分对拍，证据期）
- 角色: differential-qa-engineer
- 日期: 2026-09-10/11
- 证据等级: E1=反编译走读（B=7.161.20 partial 源），E4=运行时实测（B=7.161.20 实例 :8082），E5=双系统差分（见 L000-docker-remote-diff.md）
- 参照实例: http://localhost:8082（Artifactory-pro 7.161.20, revision 86120900, package_handler 5.675.40, addons 含 docker/oci/helmoci）
- 测试仓: `audit-probe-docker-remote`（rclass=remote, packageType=docker, url=https://registry-1.docker.io, 匿名上游凭据, enableTokenAuthentication=true；本报告发布后已删除）
- 凭据: 命令行内使用，本报告一律脱敏为 `admin/***`

> **ERRATA（2026-09-12，compatibility-engineer 注记；正文保留原貌不回改）**
> - **E3-2 已证伪（superseded by reports/compatibility/L004-304-ping-diff.md §1）**：「If-None-Match（带/不带引号、
>   GET/HEAD）一律 200 全量回，永不见 304」与活体不符——2026-09-11 双端矩阵实证：新窗口内副本对 quoted 匹配/
>   unquoted 匹配/IMS 三种拼写均回 **304 本地应答（上游零往返）**。E3-2 的存活子集仅为：非匹配 INM（quoted/unquoted）
>   →200、HEAD→200、过期窗重验证服务→200 全量。原观察最可能系校验值错配（docker.io 腿 tag/digest 两文件 sha1 不同，
>   §1.3 三机理排序在案）。消费方注意：凡锚 E3-2 的结论（L000-F C08「双端恒 200 SAME」——该复验只测了 BinFlow 腿）需按
>   L004-1 重估；契约处置见 docs/compatibility/contracts/docker-remote.yaml#docker/remote-manifest-conditional-get（翻 DIVERGENT）。
> - **E3-3 半勘误（同报告 §1.3/§3）**：「304 只在上游回源时透传（条件 GET→304）」两处修正：① 客户端 304 是本地判定
>   （M1 上游零往返直证）；② 参照过期窗重验证的活体形状是**上游 HEAD 探查**，反编译的 returnResponseGettingManifest
>   条件 GET→304 路径在活体流中未被走及。

## 0. 环境事实（复现前提）

| # | 事实 | 影响 |
|---|---|---|
| E0-1 | 本机 shell 有 `http_proxy=127.0.0.1:7897`；所有探测 curl 必须 `--noproxy '*'` | 否则 404/截获来自代理而非 Artifactory |
| E0-2 | Artifactory :8082 为纯 HTTP；对其发起 TLS 握手会收到 alert `tls: unrecognized name`（router 无证书，按 SNI 拒绝；E4 容器内外均复现） | 任何 HTTPS 形式的直连探测不可用 |
| E0-3 | 宿主 docker daemon（Docker Desktop 29.7.2, containerd snapshotter）对 127.0.0.1:8082 先试 HTTPS，遇 TLS alert 后**不回退 HTTP**，pull 直接失败 `failed to do request: Head "https://..."` | 真实客户端腿改经一次性 dind 容器（`docker:27-dind` 经典存储栈，`--insecure-registry=host.docker.internal:8082`），经典栈对 insecure registry 有 HTTP 回退，pull 成功；对 Artifactory 而言线级行为与常规 docker CLI 一致 |
| E0-4 | 访问方法为 **repo-path 形态**：registry host = `<host>:8082`，镜像名首段 = repoKey（`127.0.0.1:8082/audit-probe-docker-remote/hello-world` → 服务端 rewrite `/v2/<repoKey>/...` → `/api/docker/<repoKey>/v2/...`） | 直连 `<host>:8082/<repoKey>/v2/` 路径形式不被 router 服务（Go 路由 404 page not found，E4） |

### 0.1 复用命令骨架（BinFlow 侧重放同一序列）

```bash
# 仓创建（参照腿）
curl -sS --noproxy '*' -u admin:*** -X PUT -H "Content-Type: application/json" \
  http://localhost:8082/artifactory/api/repositories/audit-probe-docker-remote -d '{
  "rclass":"remote","packageType":"docker","url":"https://registry-1.docker.io",
  "enableTokenAuthentication":true,"missedRetrievalCachePeriodSecs":1800}'
# 真实客户端腿（dind 一次性容器）
docker run -d --privileged --name difftest-dind docker:27-dind \
  --insecure-registry=host.docker.internal:<PORT>
docker exec difftest-dind sh -c 'echo *** | docker login host.docker.internal:<PORT> -u admin --password-stdin'
docker exec difftest-dind docker pull host.docker.internal:<PORT>/<repo>/hello-world
```

## 1. token / auth 流（remote 模式）

| # | 观察 | 等级 |
|---|---|---|
| E1-1 | `GET /v2/`（repo-path 兜底 ping）匿名 → `401`，`WWW-Authenticate: Bearer realm="http://localhost:8082/v2/token",service="localhost:8082"`（无 scope），body `{"errors":[{"code":"UNAUTHORIZED","message":"authentication required","detail":null}]}`，`Content-Type: application/json;charset=ISO-8859-1`，`Docker-Distribution-Api-Version: registry/2.0` | E4 |
| E1-2 | **remote 仓的 /v2/ ping 与 /token 均由 Artifactory 自答**（与 local 同一套 DockerV2AuthenticationFilter/DockerResourceBase）；E1：`DockerV2RemoteRepoHandler#ping()` 直接 `200 {}`。不代理上游 auth | E1+E4 |
| E1-3 | `GET /v2/token?service=...&scope=repository:<repo>/library/hello-world:pull&account=admin`（带 Basic）→ `200`，body 键 = `{token, expires_in}`：**`expires_in=9000`、无 `issued_at` 键**（docker-registry.md §5.2 记 3600/issued_at 被置 null——运行时 B 版为 9000 且键缺失；token 前缀 `eyJ2ZXIiOiIy` 即 access token JWT 形态）。响应头无 Docker 特有头，带 `X-Jfrog-Version: Artifactory/7.161.20 86120900` | E4 |
| E1-4 | token 请求匿名（无 Basic）→ `401`，body **pretty-JSON `{"errors":[{"status":401,"message":"Authentication is required"}]}`**（`status` 字段、无 `code`——非 docker v2 错误形态，是 Artifactory 通用错误模型） | E4 |
| E1-5 | 坏凭据：ping 与 token 端点均回 `401` `{"errors":[{"status":401,"message":"Bad Credentials"}]}`（pretty/status 形态）；真实 docker login 报 `unknown: Bad Credentials`（docker CLI 对无 code 错误体显示 `unknown:` 前缀） | E4 |
| E1-6 | scope 不做逐项校验（E1：DockerResourceBase#getToken 原样收 scope，权限在资源端点判；docker-registry.md §5.2 既有结论，B 版未变） | E1 |
| E1-7 | Artifactory→上游的 auth dance（auth.docker.io 匿名 token）对客户端不可见；无上游凭据配置下 docker.io 代取成功（E5 差分中 BinFlow 侧需等价实现但不可观察于此处） | E4 推断 |

## 2. manifest 拉取与缓存落点

| # | 观察 | 等级 |
|---|---|---|
| E2-1 | 真实 `docker pull` 成功（hello-world:latest，index digest `sha256:5e2309035332...cc8f8`），首拉 17.0s（含上游代取+落缓存） | E4 |
| E2-2 | 缓存落点（API ListFolder 实测）：`library/hello-world/latest/list.manifest.json`（11062B，Accept 含 list 类型时 tag 路径存 `list.manifest.json`，E1：`createManifestPath` 按 Accept 是否含 list 类决定文件名）+ `library/hello-world/sha256__<platform-manifest-digest>/{manifest.json, sha256__<layer>, sha256__<config>}`。**docker.io 的 `library/` 命名空间保留在缓存路径** | E4+E1 |
| E2-3 | manifest GET（Bearer）200 头集：`Content-Type: application/vnd.oci.image.index.v1+json`、`Docker-Content-Digest`、`Docker-Distribution-Api-Version`、`Etag`（=sha1）、`X-Checksum-Md5/Sha1/Sha256`、`Last-Modified`（=拉取时刻，非上游 mtime）、`X-Artifactory-Filename: list.manifest.json`、**`X-Artifactory-Origin-Remote-Path: https://registry-1.docker.io/v2/library/hello-world/manifests/latest`**（上游源直证）、`X-Jfrog-Version`、`Accept-Ranges: bytes`、`Content-Disposition: attachment` | E4 |
| E2-4 | HEAD manifest by digest：同上头集，另多 `X-Artifactory-Docker-Registry: <repoKey>`（GET 未带；HEAD 走 `dockerHeadManifestEnabled=true` 快路径，E1 默认值 `docker.head.manifest.enabled=TRUE` 已实锤——docker-registry.md §7 待验证项闭合） | E4+E1 |
| E2-5 | blob Last-Modified = **上游真实 mtime**（`Mon, 23 Mar 2026 ...`），与 manifest 的"拉取时刻"不同 | E4 |
| E2-6 | 命名空间归一：请求 `nosuchimage-xyz/manifests/latest` 的 404 detail 中镜像名变为 `library/nosuchimage-xyz`。E1：`DockerUtil.adjustDockerRepo`——host 以 `docker.io` 结尾且镜像名不含 `/` 时加 `library/` 前缀（对请求与错误体均生效，**归一化结果泄漏进错误 detail**） | E4+E1 |

## 3. 缓存命中语义（二次拉取）

| # | 观察 | 等级 |
|---|---|---|
| E3-1 | 二次 pull（rmi 后重拉）：1.54s vs 首拉 17.0s——缓存快路径成立 | E4 |
| E3-2 | **客户端条件请求不被消费**：`If-None-Match`（带/不带引号、GET/HEAD）一律 `200` 全量回，永不见 304 | E4 |
| E3-3 | 304 只在**上游回源**时透传：E1：`returnResponseGettingManifest` 中 `manifestDownloadResponse.getStatus()==304 → notModifiedResponse()`（304 + `Docker-Distribution-Api-Version`）。即 Artifactory 用自己的 ETag 对上游条件 GET，上游 304 才向客户端回 304；客户端的 INM 头与该判定无关 | E1（E4 在 6h 检索窗内无法触发，标待动态复证） |
| E3-4 | 检索窗默认：**docker remote retrieval cache = 21600s（6h）**（`RepoConfigDefaultValues.DEFAULT_DOCKER_REMOTE_RETRIEVAL_CACHE_PERIOD`），generic 为 7200s；missed 负缓存名义默认 1800s，但 §4-4 实测 docker manifest 404 重复请求**仍每次上游往返**（无负缓存效果） | E1+E4 |
| E3-5 | blob Range 请求：`206 Partial Content` + `Content-Range: bytes 0-63/577` + 完整校验和头集（支持断点） | E4 |

## 4. blob 代理语义（marker 驱动，非盲代理）

| # | 观察 | 等级 |
|---|---|---|
| E4-1 | 未缓存但**已见于 manifest 链**的 blob（busybox config 459B / alpine layer 3.8MB）→ 上游代取成功（2.6s/3.8s 往返），落缓存，响应带全套校验和 + `X-Artifactory-Origin-Remote-Path` | E4 |
| E4-2 | 未缓存且**不见于任何 manifest**的 digest（64 位全 0、随机合法 digest）→ `404 BLOB_UNKNOWN` **40ms 内本地应答，无上游往返**（docker.io RTT 实测 1.7-6.6s，40ms 排除上游） | E4 |
| E4-3 | E1 机制：`DockerV2RemoteGetBlobHandler#getBlob` 顺序 = 空层合成 → `findBlob`（缓存 + blobs 路径缓存）→ `downloadBlobFromMarker`（AQL 查 `<repoKey>-cache` 下 `path matches <image>*` 且 `name == <digest.filename()>.marker`；**无 marker → 立即 BLOB_UNKNOWN**）→ 可选流式直代（`docker.remote.blob.streaming.enabled` 默认 **false**）。marker 由 manifest 下载时 `createManifestMarkers` 为其 config/layers 预写，blob 首取成功后 marker 被真实内容替换（`replaceRepoMarkers`/`removeMarkerAsSystem`） | E1（E4 时序证据吻合） |
| E4-4 | foreign layer（manifest `urls` 属性）：仅当 repo 配 externalDependencies patterns 且 URL 命中白名单才代取，否则 `403 "External URLs were not accepted by patterns whitelist"`（E1，未在运行时触发） | E1 |
| E4-5 | blob 落缓存前校验开关 `docker.verify.blob.checksum.before.caching` 默认 **false**；开启后 sha256 不符会删缓存重试 | E1 |
| E4-6 | 空层 digest `sha256:a3ed95ca...` GET/HEAD 直接合成响应（与 local 一致，E1 `DockerSchemaUtils.isEmptyBlob` 分支） | E1 |

## 5. 上传面（remote 默认禁推）

| # | 观察 | 等级 |
|---|---|---|
| E5-1 | `POST .../blobs/uploads/` → `400`，body pretty `{"errors":[{"status":400,"message":"Unable to upload blobs to a remote repository."}]}`（无 code/detail）；PATCH/PUT/`.patch` 变体同 | E4+E1（`badUploadBlobs()` 抛 `BadRequestException`） |
| E5-2 | `PUT .../manifests/<tag>` → `400` `"Unable to upload a manifest to a remote repository."`；真实 docker push 报 `unknown: Unable to upload a manifest to a remote repository.`（blob HEAD 阶段 `Layer already exists` 通过，卡在 manifest PUT） | E4+E1 |
| E5-3 | `DELETE .../manifests/<ref>` → `400` `"Unable to delete a manifest from a remote repository."`（E1，未运行时触发） | E1 |
| E5-4 | **错误形态指纹**：remote 的上传拒绝/认证错误走 Artifactory 通用错误模型（Jackson pretty、`status` 字段、无 `code`），而 remote 的 manifest/blob 404 走 docker v2 手写错误（compact、`code`+`detail`）——同一仓库两种错误体形态并存 | E4 |

## 6. 错误面

| # | 观察 | 等级 |
|---|---|---|
| E6-1 | 不存在 tag / 不存在镜像 → `404` compact `{"errors":[{"code":"MANIFEST_UNKNOWN","message":"The named manifest is not known to the registry.","detail":{"manifest":"library/<image>"}}]}`；**不分 NAME_UNKNOWN/MANIFEST_UNKNOWN**（镜像不存在也是 MANIFEST_UNKNOWN，detail 为镜像路径不含 tag）；无 `Artifactory-Manifest-Handler-Error` 头（local 路径专有） | E4 |
| E6-2 | 不存在 blob → `404` compact `{"errors":[{"code":"BLOB_UNKNOWN","message":"blob unknown to registry","detail":{"blobSum":"sha256:..."}}]}` | E4 |
| E6-3 | 重复拉不存在 tag：第二次仍 6.6s 上游往返 → **manifest 404 无负缓存**（missedRetrievalCachePeriodSecs=1800 配置在 docker manifest GET 面未见生效） | E4 |
| E6-4 | 上游 429 透传文案（E1）：`"Docker remote repository reached the request rate limit set by the Docker registry. Please contact Artifactory admin."`；Artifactory 自身限流：`"You reached the request rate limit set by the Artifactory registry. Please contact Artifactory admin."`（RateLimiterException 路径） | E1 |
| E6-5 | 上游 403 → forbidden 路径（`downloadForbidden` → handleForbiddenResponse）；schema1 拉取阻断（`blockPushingSchema1` 拉取侧同禁，E1 `forbiddenSchema1PullResponse`） | E1 |

## 7. tags/list 与 _catalog

| # | 观察 | 等级 |
|---|---|---|
| E7-1 | `GET .../tags/list`：200，**上游聚合**（3.2s 往返取 docker.io 全量 tag），body pretty `{"name":"library/hello-world","tags":[...10 tags...]}`（name 含 library/ 前缀；返回顺序字典序） | E4 |
| E7-2 | `GET .../_catalog`：200 `{"repositories":[]}`（约 3s——上游 docker.io 无 `_catalog` API，上游失败后仍回 200 空列表；E1：上游非 200 时循环中断，回退开关 `docker.catalogs.tags.fallback.fetch.remote.cache` 默认 **false**，最终仍以空 CatalogResponse 200 收尾；分页迭代上限 `remote.fetching.list.maximum.iteration=3`、硬上限 15） | E4+E1 |
| E7-3 | 上游 Link 分页聚合（catalog/tags）遵循 `Link: rel="next"` 翻页 | E1 |

## 8. E1 类走读对照表（B=7.161.20 partial，源 `org.artifactory.addon.docker.rest.v2`）

| 运行时观察 | 代码定位 |
|---|---|
| ping 自答 200 {} | `DockerV2RemoteRepoHandler#ping` |
| 禁推三件套 400 | 同类 `badUploadBlobs`/`uploadManifest`/`deleteManifest` → `BadRequestException`（→ pretty status 形态） |
| HEAD manifest 快路径开关 | `ConstantValues.dockerHeadManifestEnabled`（TRUE） |
| blob 本地快 404 | `DockerV2RemoteGetBlobHandler#downloadBlobFromMarker`（无 marker 即 BLOB_UNKNOWN）+ `dockerRemoteBlobStreamingEnabled`（FALSE） |
| marker 生命周期 | manifest 下载 `createManifestMarkers` → blob 首取 `replaceMarkerAsSystemIfNeeded`/`removeMarkerAsSystem` |
| library/ 前缀 | `DockerUtil#adjustDockerRepo`（host endsWith docker.io 且无 `/`） |
| list.manifest.json vs manifest.json | `DockerV2RemoteGetManifestHandler#createManifestPath`（按 Accept 含 list 类与否） |
| 304 透传 | 同类 `returnResponseGettingManifest`（上游 304 → `notModifiedResponse()`） |
| storeArtifactsLocally=false 不可用 | `validateRemoteRepo` → `UnsupportedOperationException("Docker prerequisite - artifacts must be stored locally in cache...")` |
| catalog 空 200 | `DockerV2RemoteGetCatalogHandler`（上游失败 + fallback=false） |
| 上游 429 文案 | `handleUnfoundResource`/`handleMaxRequestsResponse` |

关键默认值（`RepoConfigDefaultValues` / `ConstantValues`）：docker remote 检索缓存 21600s；generic 7200s；missed 1800s（docker manifest 面实测未生效）；blob streaming=false；HEAD manifest=true；blob 校验 before caching=false；catalog/tags 回退 cache=false；列表翻页上限 3（硬 15）。

## 9. smart remote 差异面

不可观察：无第二 Artifactory 实例，`registry.git...` 系列（smart remote 源仓库 URL）与 `externalToolsRepo`/`curated` 行为均 **UNKNOWN**（需双 Artifactory 拓扑才能取证，非本票范围）。

## 10. BinFlow 差分期重放协议（case 清单，详见 L000-docker-remote-diff.md）

1. C01 ping/token 自答形态（realm/service、expires_in、issued_at 有无）
2. C02 坏凭据（ping+token 双端点、pretty/status 形态）
3. C03 匿名 token（401 形态）
4. C04 首拉 hello-world（客户端退出码 + digest 一致性）
5. C05 缓存落点布局（tag 目录文件名、sha256__ 命名、library/ 前缀）
6. C06 manifest GET/HEAD 头集（Origin-Remote-Path、Docker-Content-Digest、Etag=sha1、X-Checksum-*）
7. C07 二次 pull（命中耗时比）
8. C08 If-None-Match（不消费、恒 200）
9. C09 blob Range 206
10. C10 marker 驱动语义（随机 digest 本地快 404 vs manifest 链内 digest 上游代取）
11. C11 push 三端点 400 拒绝（blobs uploads/PUT manifest/DELETE manifest 文案）
12. C12 404 错误体形态（MANIFEST_UNKNOWN detail=镜像路径、library/ 泄漏、无 NAME_UNKNOWN 区分）
13. C13 不存在 blob 404（BLOB_UNKNOWN blobSum）
14. C14 404 无负缓存（重复 miss 仍慢）
15. C15 tags/list 上游聚合（pretty、name 含 library/）
16. C16 _catalog（200 空列表）
17. C17 空 blob digest 合成（GET/HEAD）
18. C18 上游 429 透传文案（如可触发，否则 E1-only 对照）

normalize 规则（本域）：时间戳类头（Date/Last-Modified/Age）剔除；ETag/X-Checksum 保留下作语义对照；token 值与 instance id 类头（X-Artifactory-Id/Node-Id、X-Jfrog-Version）剔除；body 中 digest 值保留。
