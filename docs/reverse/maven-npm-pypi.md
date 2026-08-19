# Maven 2 / npm / PyPI 协议行为规格（M3：补官方规范空白处）

> 逆向基线：artifactory-pro 7.161.16。npm/PyPI 协议面 = `c.j.ph.npm.*` / `c.j.ph.pypi.*`（batch2-protocol，pro）；Maven = OSS `backend/core` 的 `o.a.a.maven.*` + `o.a.a.repo.interceptor.Maven*` + `o.a.a.repo.snapshot.*` + base/config RepoLayout 模型 + `config-templates/artifactory.config.xml`（一手模板）。
> 官方规范（以此为准，本文不重复协议本身的定义）：
> - Maven：Apache "Repository Metadata"（maven.apache.org/repositories/metadata.html，下称 **[MVN-MD]**）；Maven 2/3 仓库布局为事实标准（无正式 RFC），以 [MVN-MD] + Maven 客户端行为为准。
> - npm：npm registry HTTP API（github.com/npm/registry docs：RESOURCES.md / responses/package-metadata.md，下称 **[NPM-API]**）。
> - PyPI：PEP 503（Simple Repository API）、PEP 592（yanked）、PEP 691（JSON simple）、PyPI Upload API（docs.pypi.org/api/upload/，下称 **[PYPI-UP]**）。
> 置信度：`高` = 反编译/OSS 源码 + 官方文档双证；`中` = 仅代码可见；`低` = 推断待动态验证。

## 0. 三协议共同的框架行为（pro ph 框架）

| 行为 | 规格 | 置信度 |
|---|---|---|
| 挂载前缀 | Maven 走存储命名空间 `/{repoKey}/{path}`（无 /api）；npm = `/api/npm/{repoKey}/...`；PyPI = `/api/pypi/{repoKey}/...`（pypi.org 域名改写场景也映射到 `/api/pypi/`） | 高 |
| 错误信封 | 协议处理器抛出的 PackageException 族统一映射为 `{"errors":[{"status":<N>,"message":"<public reason>"}]}`，Content-Type `application/json`；额外响应头（如 npm notice）随异常携带 | 高 |
| 状态码族 | 304（NotModified）/400/401/403/404/409/500/501 与异常类型一一对应（`o.a.a.rest.common.exception.mapper.PackageExceptionMapper`） | 高 |
| 域根 GET | `GET /api/npm/{repo}` 与 `GET /api/pypi/{repo}` 返回 200 空 body（连通性探测用） | 高 |
| 下载计数 | 包下载成功后发 analytics 事件（npm 按包名、pypi 按文件名推导包名）；对客户端不可见 | 中 |

---

## 1. Maven 2

### 1.1 端点表

制品传输不走 /api（与 Generic 完全共用存储命名空间，路径解析见 repo-semantics.md §1）：

| 方法 | 路径 | 语义 | 成功 | 错误 | 置信度 |
|---|---|---|---|---|---|
| GET/HEAD | `/{repoKey}/{orgPath}/{module}/{version}/{file}` | 下载制品/pom/metadata/checksum | 200 | 404（见 M1 rest-api.md） | 高 |
| PUT | 同上 | 部署（jar/pom/`maven-metadata.xml`/`.sha1`/`.md5`） | 201 | 403/404/409（§1.4/§1.5） | 高 |
| DELETE | 同上 | 删除（删除后触发受影响目录 metadata 重算） | 204 | 404/403 | 高 |
| POST | `/api/maven/calculateMetadata/{repoKey}/{path}?noneRecursive=bool` | 手动重算 maven-metadata.xml（异步） | 200 空 | 404 `Could not find path '<path>'` / 404 `Unable to find local repository '<key>'.` / 400 `Repository '<key>' is not a maven repository.` | 高 |
| POST | `/api/maven/generatePom/{repoKey}/{path}` | 为无 pom 制品生成占位 pom（同步） | 200 | 同上分支 | 高 |
| POST | `/api/maven?reposToIndex=<k>&force=<n>` | 触发 Maven 索引器（admin 角色） | 200 | 403 | 中（pro 索引 addon） |

### 1.2 layout 解析（RepoLayout 模型）

RepoLayout 六字段（OSS base/config JAXB 模型）：`name`、`artifactPathPattern`、`descriptorPathPattern`、`distinctiveDescriptorPathPattern`、`folderIntegrationRevisionRegExp`、`fileIntegrationRevisionRegExp`。maven-2-default 的值（**一手 config 模板**，非反编译）：

| 字段 | 值 |
|---|---|
| artifactPathPattern | `[orgPath]/[module]/[baseRev](-[folderItegRev])/[module]-[baseRev](-[fileItegRev])(-[classifier]).[ext]` |
| descriptorPathPattern | 同上但固定 `.pom` 后缀（distinctive=true） |
| folderIntegrationRevisionRegExp | `SNAPSHOT` |
| fileIntegrationRevisionRegExp | `SNAPSHOT\|(?:(?:[0-9]{8}.[0-9]{6})-(?:[0-9]+))` |

| 规则 | 规格 | 置信度 |
|---|---|---|
| token 语义 | `[orgPath]`=groupId 点分隔展开、`[module]`=artifactId、`[baseRev]`=去 SNAPSHOT 的版本、`[folderItegRev]`=目录级集成修订（SNAPSHOT）、`[fileItegRev]`=文件级集成修订（SNAPSHOT 或 `yyyyMMdd.HHmmss-N`）、`[classifier]`、`[ext]` | 高（[MVN-MD] + 模板双证） |
| unique snapshot 文件名 | `{module}-{baseRev}-{yyyyMMdd.HHmmss}-{N}[-{classifier}].{ext}`，即 fileItegRev 命中时间戳-buildNumber 分支；目录仍是 `{baseRev}-SNAPSHOT` | 高 |
| 判定顺序 | 先剥 checksum 后缀与 metadata 前缀（`maven-metadata.xml` 或 `metadata-maven-metadata.xml`，后者为 plugin group metadata 命名），再匹配 artifact/descriptor 模板 | 高 |

### 1.3 snapshot 上传改写（local repo 字段 `snapshotVersionBehavior`）

PUT 一个 `-SNAPSHOT` 文件名时按 repo 配置改写落盘路径（`UploadServiceUtils#adjustMavenSnapshotPath` → snapshot adapter）：

| 值 | 行为 | 置信度 |
|---|---|---|
| `unique`（Maven 2 默认语义） | `foo-1.0-SNAPSHOT.jar` → `foo-1.0-<ts>-<N>.jar`。`<ts>` 优先取请求携带的 `build.timestamp` property（矩阵参数），否则服务端当前时间格式化；`<N>` = 该目录 `maven-metadata.xml` 里 `<snapshot><buildNumber>` +1；同 buildNumber 的 classifier 文件与 pom 复用同一 N（同一趟部署）；checksum 伴随文件（`.sha1`）改写为指向同 buildNumber 的主文件 | 高 |
| `non-unique` | 保持 `-SNAPSHOT` 文件名原样落盘 | 高 |
| `deployer` | 客户端传什么文件名就用什么（已是 unique 名则按 unique 处理） | 高 |
| 已是 unique 文件名 | 任何 behavior 下都不再改写 | 高 |

### 1.4 maven-metadata.xml 的服务端计算（对 [MVN-MD] 的关键补充）

[MVN-MD] 只定义文件结构与消费规则；**服务端何时生成/合并是 Artifactory 的实现行为**，全部来自代码（中-高置信）：

**触发时机**（deploy 拦截器链 `MavenMetadataCalculationInterceptor`）：

| 事件 | 计算 | 同步/异步 | 置信度 |
|---|---|---|---|
| 上传 unique snapshot 文件 / non-unique pom | **父目录**（version 目录）metadata，同步（阻塞 deploy 响应；后续 snapshot 编号依赖它） | 同步 | 高 |
| 上传其它文件（release jar 等） | 父目录 metadata | 异步 | 高 |
| 上传 pom | **祖父目录**（group/module 目录，版本清单）非递归 | 异步（可配同步） | 高 |
| move/copy/delete | 源与目标两侧受影响目录树 | 异步 | 高 |

**版本组目录（`{orgPath}/{module}/maven-metadata.xml`）内容规则**：

- `<versions>` = 子版本目录中含 `*.pom` 的目录集合（AQL：`path LIKE '<dir>/*' AND name = '*.pom' AND depth = dir+2`），同版本多行取 created 最新一条去重。
- 排序用 Maven 版本比较器（默认 `VersionNameMavenMetadataVersionComparator`，可经系统属性替换 FQN）。
- `<latest>` = 排序后最后一个（SNAPSHOT 也算）；`<release>` = 最后一个非 SNAPSHOT 版本；`<lastUpdated>` = 计算时刻（`yyyyMMddHHmmss`，UTC）。
- 子目录无 pom → **删除**已存在的 `maven-metadata.xml` 及其 `.sha512` 伴随文件；例外：现存内容是 snapshot 型 metadata 而路径非 snapshot 目录时不删（兼容 Maven 2 客户端手工部署 bug，RTFACT-6242）。
- 置信度：高（计算器逐分支可读 + [MVN-MD] 结构印证）。

**SNAPSHOT 版本目录（`.../{baseRev}-SNAPSHOT/maven-metadata.xml`）内容规则**：

- `<groupId>/<artifactId>` 取自同目录任一 pom 解析；`<version>` = `{baseRev}-SNAPSHOT`；`<lastUpdated>` = 计算时刻。
- `<snapshot><buildNumber>/<timestamp>`：repo behavior=non-unique（或 deployer 且无 unique 文件）→ buildNumber 固定 1、无 timestamp；unique → 取「最新 unique snapshot pom」的 buildNumber/timestamp（默认按 buildNumber 数值比较，可插拔比较器）。
- `<snapshotVersions>`（每 extension×classifier 一条，Maven 3 客户端需要）：默认开启（`mvnMetadataVersion3Enabled`），每 (ext, classifier) 取最新一条，`<updated>` = 时间戳去点号。
- 置信度：高。

**handleReleases / handleSnapshots 拒绝**：向 `handleReleases=false` 的 repo PUT release（或反之）→ **409**（SnapshotPolicyException 显式 409；M1 规格中的推断在此修正为确定值）。下载侧相应开关只影响可服务性。高。

### 1.5 checksum 伴随文件

| 行为 | 规格 | 置信度 |
|---|---|---|
| 客户端 PUT `{file}.sha1/.md5` | 走通用校验和文件登记（rest-api.md §1.5：>1024B 拒 409、client-checksums 策略不匹配 409） | 高 |
| GET `{file}.sha1/.md5`（local） | 服务端自算值下发（不是存储旁车文件的原样透传——旁车只用于登记 original） | 高 |
| GET checksum（**remote** repo） | **绝不回源**：remote 链路对 checksum 后缀请求直接 404 `"Checksums are not downloadable."`，checksum 只能来自缓存或服务端计算。**此条为 Artifactory 对 [MVN-MD] 场景的私有补充** | 高 |
| `maven-metadata.xml.sha512` | metadata 删除时的伴随清理包含 sha512；提示 metadata checksum 至少有 sha1/sha512 两种伴随 | 中 |

### 1.6 virtual 仓库中的 maven-metadata.xml 合并（见 repo-semantics.md §8.3 摘要）

按成员仓解析顺序逐仓取同名 metadata，合并 `<versions>`（去重后按 Maven 版本序重排）、重算 `<latest>/<release>`、`<snapshot>` 取 buildNumber 更大者；v3 开关下再并 `<snapshotVersions>`（绕开 Maven 官方 merge 不含 snapshotVersions 的缺陷 MNG-5180）。合并结果**不缓存、每次请求现算**；任一仓被 block 则整体透传 block。高。

### 1.7 与官方规范的差异/补充汇总

1. 服务端主动计算并维护三级 metadata（[MVN-MD] 未要求服务端行为）——补充。
2. `maven-metadata.xml.sha512` 伴随（官方仅惯例 `.sha1/.md5`）——补充，中。
3. unique snapshot 的 buildNumber 连续性由服务端 metadata 保证（客户端 timestamp 可经 `build.timestamp` property 注入）——补充。
4. remote 仓不代理 checksum 文件——补充，高。
5. RTFACT-6242 保护性不删除——补充。

---

## 2. npm

### 2.1 端点表（前缀 `/api/npm/{repoKey}`）

| 方法 | 路径 | 参数 | 成功 | 错误 | 置信度 |
|---|---|---|---|---|---|
| GET | `/` | — | 200 空 | — | 高 |
| GET/HEAD | `/{name}`（含 `@{scope}/{name}`，斜杠可为 `%2f`） | — | 200 包文档 JSON | 404；304（If-None-Match=metadata sha1） | 高 |
| GET | `/{name}/{version}` | — | 200 单版本 JSON | 404 | 高 |
| GET | `/-/ping` | — | 200 `{}` | — | 高 |
| PUT | `/{name}` | body=发布文档 | 201 | §2.3 | 高 |
| GET | `/{filename}.tgz` / `.tar.gz`（HEAD 同） | — | 200 tarball 流 | 404 | 高 |
| GET | `@{scope}/{name}/-/@{scope}/{name}/{file}.tgz` | — | 200（scoped 全路径形态） | 404 | 高 |
| GET | `/download/@{scope}/{name}/{version}/{checksum}` | — | 200（GitHub registry 兼容下载） | 404 | 中 |
| GET | `/_external/{file}` | — | 200（外部依赖缓存） | 404 | 中 |
| GET | `/_relative/{path}` | — | 200（smart remote 同 host 相对路径下载；有 SSRF 校验） | 404 | 中 |
| GET | `/.npm/{file}.json` | — | 200（直读版本元数据旁车） | 404 | 中 |
| GET | `/-/package/{name}/dist-tags` | — | 200 `{"<tag>":"<ver>",...}` | 404 | 高 |
| PUT | `/-/package/{name}/dist-tags/{tag}` | body=JSON string 版本号 | 201 `{"ok":"created new tag"}` | 404 元数据缺失/版本不存在；403 无 write/annotate | 高 |
| DELETE | `/-/package/{name}/dist-tags/{tag}` | — | 200 空 | 404 `npm package not found with name:<n>, and tag:<t>` | 高 |
| PUT | `/{name}/-rev/{rev}` | — | **200 `{"ok":"updated package"}` 恒定假成功（unpublish 流程的 rev 占位，服务端不做事）** | — | 高 |
| DELETE | `/{name}/-/{filename}/-rev/{rev}` | — | 200/删除版本 | 404 | 高 |
| DELETE | `/{name}/-rev/{rev}` | — | 200/删整包 | 404 | 高 |
| GET | `/-/v1/search?text=&size=&from=&quality=&popularity=` | text 必填 | 200 搜索结果 | 400 `Bad request, text can't be blank` | 高 |
| GET | `/-/all`、`/-/all/since?startkey=` | — | 200 旧版全量搜索 JSON | — | 中 |
| POST | `/-/npm/v1/security/audits{,/quick}`、`/advisories/bulk` | 审计请求 | 200 报告 | — | 中 |
| GET | `/-/npm/v1/keys`、`/-/npm/v1/attestations/{name}@{ver}` | — | 200 | 404 | 中 |
| GET | `/-/whoami` | — | 200 `{"username":...}` | 401 | 高 |
| PUT | `/-/user/{user_id}`、POST `/-/v1/login`、GET `/getToken?uuid=`、DELETE `/-/user/token/{token}`、GET `/auth/{scope}` | 登录/SSO 流 | 200/201 | 401 | 中 |
| POST | `/{path}/reindex` | manage 权限 | 200 | 403 `Forbidden` | 中 |

### 2.2 存储布局与元数据 JSON

| 项 | 值 | 置信度 |
|---|---|---|
| tarball 路径 | `{name}/-/{name}-{version}.tgz`；scoped：`@{scope}/{name}/-/@{scope}/{name}-{version}.tgz` | 高 |
| 包级元数据 | `.npm/{name}/package.json`（服务端维护的聚合文档） | 高 |
| 版本级元数据 | `.npm/{name}/{name}-{version}.json` | 高 |
| 制品属性 | `npm.name`、`npm.version`（tarball 上）、`npm.disttag`（0..n 个 tag 值）、`sha512`（integrity 原串） | 高 |
| 密钥/证明 | `.jfrog/keys.json`、`.jfrog/attestations/{name}/{version}.json`（M3 可不做） | 中 |
| 包文档结构 | 遵循 [NPM-API]：`name/dist-tags/versions/time/...`；**瘦身变体**：客户端 `Accept: application/vnd.npm.install-v1+json`（npm ci 场景）→ SLIM 文档（去掉 README 等重字段），Content-Type 原样回 `vnd.npm.install-v1+json` | 高（[NPM-API] + 代码双证） |

### 2.3 PUT publish 流程（校验顺序即错误顺序）

1. body JSON 解析失败 → 400。
2. `_attachments` 非空但 `versions` 空 → 400 `Missing versions in npm package...`。
3. 取第一个 attachment + 第一个 version；无写权限（对 tarball 路径）→ 403 `Cannot deploy to '<tarballPath>'`。
4. tarball 路径已存在 → **403** `Cannot modify pre-existing version '<v>', aborting upload for: '<name>'`（npm 官方语义是 403/409 族，Artifactory 定 403）。
5. name/version 非法（semver 与 leading zeros 规则）→ 400 `Invalid Version: '<v>'...`。
6. `_attachments` 空但 `versions[-].deprecated` 存在 → deprecate 流程，**201** `{"ok":"updated package"}`。
7. `_attachments` 空且非 deprecate → 400 `Missing attachments with tarball data...`。
8. integrity（`sha512-<base64>`）与 tarball 实测不一致且 repo 校验开关开 → 400 `Conflict between integrity from metadata and tarball`（默认信任记录 sha512 为属性，不强制拒）。
9. `dist.shasum`（sha1 hex）传给存储层做 client checksum；存储层 409 → 转译为 400 `Conflict between sha1 from metadata and tarball`。
10. 成功 → 201；dist-tags 写入 `npm.disttag` 属性（仅第一个 tag；`latest` 默认跳过，按日期策略时才记）。

置信度：高（`NpmPublishCommand` 逐分支）。

### 2.4 metadata GET 行为

- 包文档读自 `.npm/{name}/package.json`；响应头 `X-Checksum-Sha1` 与 `ETag` = 该文件 sha1；`If-None-Match` 命中 → 304（带同键头）。高。
- `dist.tarball` 一律重写为本服务地址：`{base}/api/npm/{repoKey}/{tarballPath}`。tarballPath 解析顺序：文档内 tarball 相对路径 → 默认布局路径 → 按 `npm.name`+`npm.version` 属性搜索兜底（version 带 `v` 前缀重试一次）。高。
- 包不存在 → 404（信封 §0）。高。
- HEAD：返回 lastModified + `X-Checksum-Sha1`/`ETag`（virtual 仓不带 origin 头）。高。

### 2.5 remote 仓的 tarball URL 重写（代理场景，对 [NPM-API] 的补充）

上游 metadata 里 `dist.tarball` 指向外域时，按优先级改写为本地可服务 URL：

1. tarball URL 以 remote repo URL 为前缀 → 剥成相对路径。
2. URL 含 `/api/npm/`（上游是另一台 Artifactory，smart remote 场景）→ 取 `/api/npm/{repoKey}/` 之后的相对路径。
3. 与 remote URL 同 host 不同 path → `_relative/{path}`（下载时再回源该 host）。
4. 完全外域（如 GitHub tarball）→ 按 npm 默认布局或 GitHub 相对布局重建。
5. 最终 URL = `{localBase}/{相对路径}`；「替代下载」模式下追加 `?dl={原始URL}`。

置信度：高（`NpmRemoteBaseCommand#replaceTarballUrl` 四分支完整）。remote 下载响应另带 `X-Artifactory-Origin-Remote-Path` 头。中-高。

### 2.6 virtual 仓的 metadata 合并

- 按解析顺序遍历成员仓；local 成员无 `.npm/{name}/package.json` 跳过；每仓取包文档后并入：**首个仓为基底，后续仓版本 `putIfAbsent`（先到先得，同版本不覆盖）**；`time`/`dist-tags` 并集，通用字段（description 等）取先到者；被排除模式过滤后最新版本与 `latest` 标签重算。高。
- 合并结果缓存进 virtual cache 仓：路径 `.npm/{name}/package-{hash}-{STRATEGY}.json`（STRATEGY=FULL/SLIM/RT/FULL_STREAM），TTL = virtual 仓 `virtualRetrievalCachePeriodSecs`，**< 600s 视为禁用**（只警告不缓存）。高。
- 并发：同一 cache path 走 work queue 单飞 + 120s 冲突锁；锁超时/失败回退旧合并实现。高。
- tarball 下载（virtual）：按搜索顺序首命中即服务（§8 repo-semantics）。中。

### 2.7 与官方规范的差异/补充汇总

1. `PUT /{name}/-rev/{rev}` 恒 200 假成功——npm 客户端 unpublish 前置步骤，官方无此端点定义。补充，高。
2. scoped 包路径允许 `%2f` 编码斜杠（官方客户端行为，代码显式路由）。补充，高。
3. `X-Checksum-Sha1`/`ETag` 用**包文档**（而非 tarball）的 sha1——官方未规定元数据 ETag。补充，高。
4. `?dl=`、`_relative/`、`_external/` 是 Artifactory 私有下载形态。补充，高。
5. 已存在版本的 republish 拒绝用 403（npmjs 官方错误族未明确定义码）。补充，高。
6. SLIM 文档 `vnd.npm.install-v1+json`（npm ci 优化）+ 私有 `vnd.rt.install-v1+json`。补充，高。

---

## 3. PyPI

### 3.1 端点表（前缀 `/api/pypi/{repoKey}`）

| 方法 | 路径 | 成功 | 错误 | 置信度 |
|---|---|---|---|---|
| GET | `/` | 200 空 | — | 高 |
| GET | `/simple/`（或 `/{user}/{scope}/simple/`） | 200 仓级索引（全包名清单） | 403 `{"error":"Forbidden","reason":"User has insufficient permissions."}` | 高 |
| GET | `/simple/{name}/` | 200 包级 simple index | 404；304 | 高 |
| GET | `/simple/{name}`（无尾斜杠） | **302** → 补尾斜杠 URL（尊重 `X-JFrog-Override-Base-Url` / `X-Artifactory-Override-Base-Url` 头；skipUrlTranslation 参数保留） | — | 高 |
| GET | `/simple/{name}/{version}` | 恒 **404**（保留端点，无实现语义） | — | 高 |
| GET | `/packages/{path}`（scoped：`/{user}/{scope}/packages/{path}`） | 200 文件流 | 404 | 高 |
| GET | `/pypi/{name}/json`、`/pypi/{name}/{version}/json` | 200 legacy JSON API（[PYPI-UP] 生态的 warehouse 兼容，非 PEP 691） | 404 | 高 |
| POST | `/`（multipart/form-data） | 200（§3.3） | 400 | 高 |
| POST | `/`、`/simple`、`/{user}/{scope}/...`（text/xml） | 200 XML-RPC 搜索响应 | 无结果时 XML fault | 中 |
| GET | `/{任意路径}`（含两段形态 `{path}/{name}`） | 200（复制/镜像场景的裸下载兼容） | 404 | 中 |

注意 scoped 形态 `{user}/{scope}/...` 是 devpi 风格私有扩展。中-高。

### 3.2 simple index HTML 形态（对 PEP 503/592 的实现细节）

- 文档头固定：`<!DOCTYPE html>\n<html><head><title>Simple Index</title><meta name="api-version" value="2" /></head><body>\n`（PEP 629 的 api-version=2 声明；仓级与包级同头）。高。
- 锚点模板：`<a href="{link}"{attrs} rel="{internal|external}">{filename}</a>`；attrs 依序 `data-requires-python`、`data-yanked`、`data-dist-info-metadata`、`data-core-metadata`（后两者默认关闭，系统属性开启；PEP 658 语义）。**`rel="internal|external"` 是 Artifactory 私有属性，PEP 503 无此属性**。高。
- `link` = `../../{repo 内路径}#sha256={sha256}`（无 sha256 时 `#md5={md5}`）——相对回退两级到 `/api/pypi/{repoKey}/packages/...`。高。
- 条目按文件名排序输出；值经 HTML 转义。高。
- `Accept-Encoding: gzip`（含 `*` 通配、q>0）→ 响应 `Content-Encoding: gzip` + `Vary: Accept-Encoding`（响应体预压缩缓存）。高。
- **ETag**：对全部条目字段做 31-折迭哈希取十进制整数字符串（空索引 `"0"`），非内容寻址、非校验和；`If-None-Match` 精确串比较命中 → 304 无 body。PEP 503 只要求支持 ETag/304，不规定算法。高。
- JSON simple index（PEP 691 形态）：`pypi.simple.json.format.enabled` **默认 false**，默认 HTML；开启后按 Accept 协商（`application/vnd.pypi.simple.v1+json`）。中。

### 3.3 upload POST 流程（[PYPI-UP] 对照）

multipart 表单字段（Artifactory 识别集）：

| 字段 | 处理 | 置信度 |
|---|---|---|
| `:action` | 必须为 `file_upload`；否则 400 `unknown action '<action>'`（submit/submit_form/browse 等枚举存在但都走 400 分支） | 高 |
| `content` | 文件体；文件名取该 part 的 Content-Disposition filename | 高 |
| `md5_digest` | 作为客户端 md5 checksum 传给存储层（可缺失——twine ≥6.2 不再发送，服务端自算并接受） | 高 |
| `filetype` / `protocol_version` / `comment` | 收下不参与路径 | 高 |
| 其余全部字段（name/version/summary/requires_python/yanked/...） | 打包为元数据 → 制品属性 | 高 |

流程：存储路径 = `{name}/{version}/{filename}`（**name 用元数据里的原始名，不 normalize**）；上传成功（200/201）→ 响应统一 **200**；随后写属性 `pypi.name`、`pypi.normalized.name`、`pypi.version`、`pypi.summary`、`pypi.requires.python`、`pypi.yanked`、`pypi.metadata.file.hash` 并异步更新 `.pypi/` 索引。高。

### 3.4 哈希算法选择（对 [PYPI-UP]/PEP 503 的补充）

| 场景 | 算法 | 置信度 |
|---|---|---|
| 上传校验 | 客户端只可能提供 md5（`md5_digest`）；sha256/sha1 一律服务端计算 | 高 |
| simple index 链接 fragment | sha256 优先（`#sha256=`），制品无 sha256 才 `#md5=` | 高 |
| legacy JSON API hashes 字段 | `{"sha256": "<hex>"}`（剥掉 `sha256=` 前缀）或 `{"md5": "..."}` | 高 |
| BinFlow 校准 | 必须容忍 `md5_digest` 缺失（新 twine）；pip 校验依赖 `#sha256=`，因此**索引输出必须优先 sha256** | 高 |

### 3.5 存储布局与命名规范化

| 项 | 值 | 置信度 |
|---|---|---|
| 制品 | `{name}/{version}/{filename}`（原始名） | 高 |
| local 索引 | `.pypi/{norm}/{norm}.html`、`.pypi/{norm}/{norm}.json`、`.pypi/simple.html`、`.pypi/simple.json`、`.pypi/{norm}/{version}.json`、`.pypi/{norm}/latest.json`、`.pypi/{norm}_upload_times.json` | 高 |
| remote 缓存索引 | `.pypi/{norm}.html`、`.pypi/{norm}.json`、`.pypi/simple.json`（平铺，与 local 不同） | 高 |
| `normalizePackageName`（PEP 503） | lower + `[-_.]+`→`-`，用于索引路径与 simple/{name} 查找 | 高 |
| `normalizeDistributionName`（PEP 427 wheel 惯例） | `[-_]+`→`_`（允许 `.`），用于文件名侧 | 高 |
| 强制开关 | `pypi.enforce.layout` / `pypi.enforce.naming.normalization` 默认 false（只算不强校验） | 中 |

### 3.6 virtual 仓索引合并

按顺序收集各成员的包索引后合并条目；**格式回退**：请求 JSON 而某成员只出 HTML（或反之）→ 整个合并切换到 fallback 格式（HTML）重新输出；任一成员失败不阻塞其它。remote 成员的「包不存在」有独立负缓存（`pypi.package.local.index.miss.cache`：默认开、TTL 28800s=8h、容量 50000），避免上游抖动反复打远端。高（system property 常量 + controller 可见）。

### 3.7 与官方规范的差异/补充汇总

1. `rel="internal|external"` 属性——PEP 503 无定义。补充，高。
2. ETag 为折迭哈希整数串——算法私有，客户端只当不透明串即可。补充，高。
3. `/pypi/{name}/json` 是 warehouse legacy JSON API 而非 PEP 691 simple JSON；两者并存时靠 Accept/开关区分。补充，高。
4. `{user}/{scope}/` 前缀（devpi 风格 scoped 索引/下载/上传）。补充，中。
5. `skipUrlTranslation` 查询参数（poetry UA 自动追加）——控制 remote 索引是否改写链接为本地。补充，高。
6. 上传成功响应统一 200（warehouse 返回 200，一致）；但 Artifactory 把存储层 201 也归一为 200。补充，高。

---

## 4. 对 BinFlow M3 的校准建议

1. **错误信封与状态码族先行**：三协议共用 `{"errors":[{status,message}]}` 信封 + 304/400/401/403/404/409 映射，建议落在 `internal/httpapi` 公共错误层，adapter 只抛领域错误。
2. **Maven 最小闭环** = layout 正则（maven-2-default 六字段）+ snapshot 改写（先只做 `non-unique` 与 `deployer`，`unique` 的 buildNumber 连续性依赖 metadata 计算可后置）+ 两级 metadata 计算（版本组目录 + SNAPSHOT 目录）+ 409 snapshot policy。metadata 计算器建议与 OSS 同构：deploy 后异步任务、AQL-free（BinFlow 可直接查 nodes）。
3. **npm 关键校验点**：已存在版本 republish 拒绝（403）、integrity sha512 校验、SLIM Accept 协商、`-rev` PUT 假成功 200、ETag=包文档 sha1。virtual 合并先做「每次现算」版（Artifactory 的缓存合并是性能优化，语义等价），TTL 语义（<600s 禁用）可不实现。
4. **PyPI 关键校验点**：`:action` 严格 `file_upload`、md5_digest 可缺失、simple HTML 的 api-version=2 头与 `data-requires-python/data-yanked`、链接 `#sha256=` 优先、无尾斜杠 302。ETag 用任意稳定内容哈希即可（客户端不透明）。
5. **协议无关的共性**：三协议的「包索引」本质都是**服务端维护的隐藏目录文件**（`.npm/`、`.pypi/`）+ **制品属性驱动**（npm.name/npm.disttag、pypi.normalized.name...）。BinFlow 建议直接以 metadata 表属性为中心重建索引，无需逐字复刻隐藏文件布局——但隐藏目录**必须**在 REST 层不可见或显式跳过（Artifactory 用 `.npm/.pypi` 前缀 + 浏览过滤）。
6. **低置信度项**全部集中在上表标注「中」的端点（GitHub 兼容下载、`_external`、XML-RPC 搜索、reindex、SSO 流），M3 均可裁剪不实现——真实客户端（mvn/npm/pip/twine）不触达。
7. 动态验证清单见 §5。

## 5. 待验证清单

1. `GET /api/pypi/{repo}/simple/{name}/{version}` 是否在旧版客户端有实际语义（现恒 404）。
2. npm `PUT /{name}/-rev/{rev}` 是否存在隐式副作用（代码只回 200，未见写操作；需动态抓包确认 npm unpublish 全流程可用）。
3. npm scoped `%2f` 与 `%2F` 两种编码在 BinFlow 反代/路由层的等价性（Jersey 正则显式两者都收）。
4. PyPI JSON simple（PEP 691）默认关闭的确认基于系统属性默认值，未在 UI 配置层验证是否有仓库级开关。
5. Maven `unique` snapshot 上传时 classifier/pom 复用 buildNumber 的条件分支较多（有 pom 校验特例），建议 BinFlow 实现后用 mvn deploy 对拍。
