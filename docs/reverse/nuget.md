# NuGet 包型（v2 OData 全路由 + remote/virtual search 上游代理 + service index 动态解析）行为规格

- 票据：T-334（M12 B0，FR-103 规格前置，用户裁决① NuGet 对齐 bundle 的活化落点）。谱系：T-287（M10 试点 as-built，L1~L7 rulings）→ T-304 §1.1 复核（出处锚定 + 四项改回判定）→ 本票活化；T-393（M14 B2）增量锚 §5.4（K59 v3/flat 直推面重复臂）。消费票：**T-337**（FR-103：v2 路由全集 + OData 参数面 + publish 重复臂）、**T-341**（FR-104：v3 search 上游代理 + service index 动态解析 + virtual 合并）。
- **公开规范锚点（官方优先）**：Microsoft Learn NuGet Server API（v3：service index / SearchQueryService / registrations / flat container / push）；v2 OData 的 Learn 页已撤，现存权威 = NuGet/Home 官方 wiki 两页——[SemVer2 support for nuget.org (server side)](https://github.com/NuGet/Home/wiki/SemVer2-support-for-nuget.org-(server-side))（v2 OData 端点全集 + 逐端点参数签名 + semVerLevel 语义）与 [Semver 2.0.0 Protocol](https://github.com/NuGet/Home/wiki/Semver-2.0.0-Protocol)；[JFrog NuGet Repositories 文档](https://docs.jfrog.com/artifactory/docs/nuget-repositories)（URL 形态）。反编译只补规范空白，逐条标注「此条补充官方规范」。
- **出处层级标记**：`T-304` = T-304 §1.1 行项锚定；`DE` = 本票直接反编译取证（NuGetSubResource / NuGet{Local,Remote,Virtual}RepoHandler / NuPkgSearchRequestHandler 族 / NuGetServiceImpl / NuGetV3VirtualAndRemoteCommon / FeedUtils / NuGetSearchParameters / artifactory.xsd）；`官方` = 上述公开文档。
- 置信度：`高` = 反编译 + 公开文档双证（或纯官方定义）；`中` = 仅反编译；`低` = 推断。低置信不作验收依赖。

---

## 1. 基础路径形态

| 系统 | v2 形态 | v3 形态 | 置信度 |
|---|---|---|---|
| BinFlow（实现口径，as-built） | `$BASE/binflow/api/nuget/v2/<repoKey>/...` | `$BASE/binflow/api/nuget/v3/<repoKey>/...`（index.json 入口） | —（PRD FR-88 / T-287 已实现） |
| Artifactory（参考对照） | `$BASE/artifactory/api/nuget/<repoKey>/...` | `$BASE/artifactory/api/nuget/v3/<repoKey>/index.json` | 高（JFrog 官方文档） |

v3 service index 合成的 base 取值 = `server.base_url`（空则按请求 scheme+host 推导，X-Forwarded-Proto 认可）——ADR-0034/TL-1 统一定案（npm packument 同款先例，cargo.md §3.1 同引）。BinFlow 沿用，本票不新增配置键。

## 2. v2 全路由集表（相对 v2 base = `$BASE/binflow/api/nuget/v2/<repoKey>/`）

Artifactory 在该 base 下实装**全部 18 个端点**（T-304 §1.1-L2 锚定「NuGetSubResource 路由全集」；本票 DE 逐端点复核）。BinFlow as-built 仅实装 `FindPackagesById()`/`$metadata`/service document/v2 push `{id}/{version}`/`/package/` 前缀——**其余端点 as-built 为 404，属端点存在性可观测差异（裁决① 改回面，T-337 承载）**。

| # | 方法 | 路径 | 语义 | 成功响应 | 错误响应 | 出处 | 置信度 |
|---|---|---|---|---|---|---|---|
| 1 | GET | `/`（base 根） | v2 service document（OData 服务文档，列出 Collections/Functions） | 200 `application/xml;charset=utf-8` + `DataServiceVersion` 头 | 401（forceAuth 且仓不允许匿名根 GET，见 §3） | T-304-L2 + DE + 官方（OData 服务文档惯例） | 高 |
| 2 | GET | `/$metadata` | EDMX 元数据文档（实体集 + FunctionImport） | 200 `application/xml` + `DataServiceVersion` 头 | 401（forceAuth）；403（无任何授权仓） | T-304-L2 + DE | 高 |
| 3 | GET | `/Search()`（括号可省略，`Search` 同义） | 包搜索（searchTerm/targetFramework/includePrerelease + OData 选项） | 200 `application/atom+xml` feed | 404（feed 构建为空）；401/403（§3 门） | 官方（wiki 签名）+ DE | 高 |
| 4 | GET | `/Search()/$count` | 搜索计数 | 200 `text/plain;charset=utf-8`（十进制数字） | 同上 | 官方 + DE | 高 |
| 5 | GET | `/FindPackagesById()?id='<id>'` | 按 id 取全部版本（包还原主路径） | 200 atom feed（未知 id = 空 feed，收集语义） | **id 参数缺失 → 404**（非 400）；401/403 | 官方 + DE（local/virtual 均判 404） | 高 |
| 6 | GET | `/FindPackagesById()/$count` | 按 id 计数 | 200 text/plain | 同 #5 | 官方 + DE | 高 |
| 7 | GET | `/Packages()`（空括号/无括号） | 全包 feed（无搜索词过滤） | 200 atom feed | 404；401/403 | 官方 + DE | 高 |
| 8 | GET | `/Packages(Id='<id>',Version='<ver>')` | 单包条目（OData 寻址，Id/Version 从路径段解析） | 200 atom entry | **未命中 → 404**；401/403 | 官方 + DE | 高 |
| 9 | GET | `/Packages(Id='<id>')/Id` | 单包 Id 投影（客户端自检用） | 200 `application/xml`（仅 id 值） | 401/403 | DE（官方 wiki 未列此投影；此条补充官方规范） | 高（DE + OData 投影惯例） |
| 10 | GET | `/Packages()/$count` | 全包计数 | 200 text/plain | 401/403 | 官方 + DE | 高 |
| 11 | GET | `/GetUpdates()/?packageIds=&versions=&includePrerelease=&includeAllVersions=&targetFrameworks=&versionConstraints=` | 更新检查（VS 包管理器用） | 200 atom feed | 404；401/403 | 官方（wiki 完整签名）+ DE | 高 |
| 12 | GET | `/GetUpdates()/$count` | 更新计数 | 200 text/plain | 同上 | 官方 + DE | 高 |
| 13 | POST | `/$batch` | OData $batch（multipart/mixed，每 part 一个子请求 URI）；子请求分发到 GetUpdates/Search/Packages/FindPackagesById（±/$count）七形态；**part URI 的查询串会覆盖 handler 的搜索参数** | **202 Accepted** + `multipart/mixed;boundary=batchresponse_<guid>` 体（逐 part `application/http` 段）+ `DataServiceVersion: 2.0` 头 | 401/403；**不支持的子请求形态 → 400 `Unsupported batch entry.`**（整个 batch 失败） | T-304-L2 + DE | 高（端点与分发 = DE + T-304；官方 OData $batch 通则） |
| 14 | GET | `/Download/{packageId}/{packageVersion}` | 协议化下载（路径解析见 §6） | 200 `application/octet-stream`（或透传存储层重定向）；成功/3xx 计入下载分析事件 | 404 `Unable to find NuPkg '<id>-<version>' in '<repo>'` | T-304-L2 + DE | 高 |
| 15 | GET | `/{filename}.nupkg`（任意深度路径，末段 .nupkg） | 裸文件下载（直通存储层 DownloadContext，允许重定向头=false 显式关闭） | 200 包流 | 404（存储层） | T-304-L2 + DE | 高 |
| 16 | DELETE | `/{path}`（前两段 = id/version） | **硬删版本**（物理删除包文件，无 listed/unlisted 翻转面——T-287-L1 维持判定） | 200 `Successfully removed '<path>'`（text/plain） | 404（未找到）；**403** `Unable to delete NuGet package '<path>'`（删除失败/无权限）；401（forceAuth） | T-304-L1 + DE | 高 |
| 17 | PUT | `/`（base 根） | publish（multipart/form-data，字段名 **`package`**；身份取自包内 nuspec，URL 无 id/version） | 201 `Successfully published NuPkg to: <path>` | 见 §5 重复臂矩阵；400（缺 `package` 字段：`Unable to find 'package' field in request form data.`） | T-304-L2 + DE（dotnet 8 实测直推此形态——T-287 §2.2） | 高 |
| 18 | PUT | `/{path}` | publish 带 path 前缀（部署路径 = `<path>/<id>.<version>.nupkg`） | 同 #17 | 同 #17 | T-304-L2 + DE | 高 |

路由细节注记（DE，中置信——路由正则的边界行为）：
- `Search{ignoreParens: (?:\(\))?}` —— 括号整体可选，`Search` 与 `Search()` 等价路由。
- `Packages{id: (\(.*\))}` —— id 段为任意括号串：`()` 空括号 → 全包 feed；`Packages(Id='x',Version='y')` → 单条目。
- DELETE 取 path 前两段作 id/version，其余段忽略。

## 3. 鉴权与门控语义（此条补充官方规范）

每个 v2 端点入口有两道门，顺序固定（DE，高——逐端点复核一致）：

1. **forceAuthentication 门**：仓/系统开启强制认证时，除两个例外（见下），一律 **401 + `WWW-Authenticate: Basic realm="Artifactory Realm"`**，先于任何业务逻辑。
2. **hasAnyAuthorizedRepo 门**（仅读端点）：当前 principal 对该仓无任何读授权 → **403**（同 Basic 头）。

例外与差异表（DE）：

| 端点 | forceAuth 门 | hasAnyAuthorizedRepo 门 |
|---|---|---|
| GET `/`（service document） | 仅当「仓不允许匿名根 GET」时才 401（`repoAllowsAnonymousRootGet`——feedContextPath 为空的仓允许匿名取根文档） | 不查 |
| GET `{filename}.nupkg`（#15） | **不查**（直接进存储层下载，授权由存储层 RBAC 裁决） | 不查 |
| GET `Download/{id}/{ver}`（#14） | 查 | 不查 |
| DELETE / PUT 族（#16~18） | 查 | 不查 |
| 其余读端点（#2~13） | 查 | 查（403） |

BinFlow 实现建议：保持两道门顺序（401 先于 403）；匿名根 GET 豁免可与 BinFlow 匿名读策略合并表达，不必引入 per-repo 新参数（对齐 D-6 先例姿态）。

## 4. v2 查询参数面（OData 参数模型）

参数集（DE `NuGetSearchParameters` 字段 × 官方 wiki 端点签名双证，高）：

| 参数 | 用途端点 | 官方签名出现处 | 备注 |
|---|---|---|---|
| `searchTerm` | Search()（±$count） | 官方 wiki | 取值去引号（`'x'` → `x`） |
| `targetFramework` / `targetFrameworks` | Search() / GetUpdates() | 官方 wiki | 单数属 Search，复数属 GetUpdates |
| `includePrerelease` | Search() / GetUpdates() | 官方 wiki | 布尔 |
| `packageIds` / `versions` | GetUpdates() | 官方 wiki | 管道分隔多值（`id1|id2`） |
| `includeAllVersions` | GetUpdates() | 官方 wiki | 布尔 |
| `versionConstraints` | GetUpdates() | 官方 wiki | |
| `id` | FindPackagesById() | 官方 wiki | 缺失 → 404（§2 #5） |
| `semVerLevel` | 全部 v2 OData 端点 | 官方 wiki | 语义见下 |
| `$filter` / `$orderby` / `$top` / `$skip` / `$inlinecount` / `$skiptoken` / `$select` / `$expand` | 全部（OData 通用选项） | 官方 OData + [Filter OData query requests wiki](https://github.com/NuGet/Home/wiki/Filter-OData-query-requests) | 参数名大小写不敏感（DE：入参折叠为 CaseInsensitiveMap） |

**semVerLevel 官方语义（高，官方 wiki 全文锚定）**：

- 默认（缺省/不可解析）= **过滤掉 SemVer 2.0.0 版本**；`>=2.0.0` 且主版本 `<3` = 包含；`>=3.0.0` 回落按 2.0.0 处理；不区分大小写。
- `IsLatestVersion` / `IsAbsoluteLatestVersion` 的取值随 semVerLevel 变化（`1.0.0-a` 与 `1.0.0-b.1` 并存时，`semVerLevel<2` 前者为 absolute latest，`=2.0.0` 后者是）。
- feed 的 `<d:Version>` 保留原始版本串；`NormalizedVersion` 与各链接不得含 build metadata。
- Artifactory 补充（DE，高——ConstantValues 默认值双证）：系统参数 `nuget.ignoreIsLatestVersionFilter` **默认 true**，即 `semVerLevel=2.0.0` 且 `$filter=IsLatestVersion` 的请求默认丢弃该 filter（全版本返回）。

## 5. publish / DELETE 语义（rclass 矩阵）

### 5.1 publish 重复臂（T-337 核心消费点——T-304 §7 新发现，DE 复核）

| rclass | 行为 | 精确 wire |
|---|---|---|
| local | ① 缺 `package` 字段 → **400**（文案见 §2 #17）；② 包已存在且当前用户**无 d 权限** → **409 CONFLICT**，体 `Package already exist: <deployPath>`；③ 包已存在且**有 d 权限** → **直接覆盖上传**（无冲突臂）；④ 上传成功（2xx）→ **201** `Successfully published NuPkg to: <path>`；⑤ 上传非 2xx → 透传上游状态码与体 | DE（`isPackageAlreadyExistAndCantBeDeletedByCurrentUser` = exists && !canDelete） |
| remote | publish 一律 **400** `This operation can only be performed on local repositories.` | DE |
| virtual | 路由到 defaultDeploymentRepo：未配置/不存在 → **400**（同上文案）；配置了 → 先做 virtual 路径 pattern 校验，**不合法 → 409 `Unacceptable path.`**；合法则等价 local publish（重复臂同 local） | DE |

部署路径推导：`deployPath = <可选 path 前缀/> <nuspec.id>.<nuspec.version>.nupkg`（id/version 取自包内 nuspec，**不**取 URL）。

**D-10 关闭留痕（2026-08-30 终裁=对齐 409，T-378 落地）**：臂② DE 谓词 `exists && !canDelete` **无字节比对分支**——BinFlow as-built 曾把实测 sha256 当声明摘要送入服务层，借道幂等重传短路使「同字节+仅 w 权限重传」答 201（T-356 L03 实测，LC-56 待裁行）。T-378 移除该声明摘要后重复臂按本表字面执行：② 同字节/异字节一律 409（体逐字同上）、③ d 权限覆盖 201、④ 新包 201；M12 L03 as-built 断言反转归属 T-378 豁免（v2full_test.go 四臂 table-driven 固化 + curl 真客户端腿）。

**与 cargo 的对照**（T-304 §1.4-D-3 已锚定）：nuget local 重复臂是 409，cargo 是 401/403+覆盖臂——包型间不一致是 Artifactory 自身事实，BinFlow 按 nuget 取 409 + d 权限覆盖臂即可，无需向 cargo 形态看齐。

### 5.2 DELETE

| rclass | 行为 |
|---|---|
| local | §2 #16（硬删版本文件；200/404/403） |
| remote | **400** `This operation can only be performed on local repositories.` |
| virtual | 路由 defaultDeploymentRepo（未配置/缺失 → 400），等价 local DELETE |

### 5.3 Download/{id}/{version}（v2 协议化下载）

- virtual：路径 pattern 校验失败 → 404 `Unable to find package <id>`；随后**先查 local/federated 成员、再查 remote 成员**（每档内按成员序），首个 2xx/30x 即返回；全部失败时若任一成员返回过 403 且系统开关 `nuGetVirtualEnableForbiddenResponse` 开 → 403（最后一个 403 的消息），否则 404（最后错误消息）。
- remote：未命中本地路径解析时用**替代下载 URL** 直连上游：`<repo.url>/<downloadContextPath>/<id>/<version>`（downloadContextPath 默认 `api/v2/package`，xsd 默认值，高）；非 smart 源场景下版本串会剥 SemVer2 metadata。
- local：见 §6 路径解析。

### 5.4 v3/flatcontainer 直推面重复臂增量锚（K59，T-393——D-10 终裁邻域面）

背景：BinFlow T-287 注册偏差——本实例 service index 把 `PackagePublish/2.0.0` @id 指到 `<v3base>/flatcontainer`（无尾斜杠），于是真实客户端（dotnet 8.x，T-287 live 验证）的 v3 协议 push 落在 **v3 flatcontainer 面**（两形态：direct `PUT <publish-base>`〔客户端真实形态，身份取自包内 nuspec〕与 addressed `PUT <v3base>/flatcontainer/<id>/<version>`〔PRD carrier + curl 面〕）。Artifactory **不存在**这一面——增量锚按「官方规范 → Artifactory 可观察行为 → BinFlow as-built 对照」三层取证。

| # | 断言 | 出处 | 置信度 |
|---|---|---|---|
| A1 | 官方 push 契约：`PUT {PackagePublish@id}`，body = **multipart/form-data 首段 = nupkg 原始字节**（后续段忽略，文件名/头忽略）；成功 201/202；包无效 400；**「A package with the provided ID and version already exists」→ 409**；官方明示「该 URL 与 legacy V2 push endpoint 同址、协议相同」 | 官方（Learn: Push and Delete, NuGet API，本票全文复核） | 高 |
| A2 | Artifactory service index 把 `PackagePublish/2.0.0` @id 宣告为 **v2 base**（非 flatcontainer）——feed 模板 `fullNugetRootFeed.json` 与上游代理转换表（`getOriginalToArtifactoryResourceConverters`）双处一致。故 Artifactory 上真实客户端 push 永远落 v2 publish 面 → 重复臂即 §5.1 的 409 | DE（§9.1 T-304 锚定 + 本票双点复核） | 高 |
| A3 | Artifactory v3 flatcontainer 家族（`v3-flatcontainer/{id}/index.json`、`v3-flatcontainer/{id}/{ver}/{file}.nupkg`、`v3-flatcontainer/{id}/{ver}/{id}.nuspec`）**全 GET-only**；v3 mount 下仅有的 PUT = `PUT symbols`（symbol 包，multipart）与继承自 v2 资源的 catch-all `PUT {path: .+}`（@Consumes multipart/form-data）——后者把 URL 路径当 deploy-path 前缀转 v2 publish（重复臂同 §5.1 = 409） | DE（`NugetV3Resource`/`NuGetV3SubResource` 路由全集复核，本票） | 高 |
| A4 | 对 flatcontainer 形态路径发**非 multipart** 内容型 PUT（如 `application/octet-stream`）→ 无 @Consumes 匹配 → JAX-RS 容器默认 **415**（服务端未显式编码该分支） | DE 推断（容器通则，无显式代码） | 中 |
| A5 | **K59 锚定值 = 409**：与官方 push 资源逐字（A1）一致，且与 D-10 终裁方向（v2 面 DE 谓词 exists && !canDelete → 409）同族语义——「包已存在且不可覆盖」在两个 push 面上不应出现不同状态码 | 官方 + D-10 邻域推定 | 高 |
| A6 | BinFlow as-built 现状：v3/flat 直推面「已存在 + 无 d」→ 服务层 overwrite 门（ErrForbidden）→ adapter 403（体含 `needs DELETE permission on the existing node`）——**与锚定值不一致**；「已存在 + 有 d」→ 覆盖 201；新包 → 201（无 adapter 注入的字节比对，D-10 后语义同构） | BinFlow 代码（flat.go servePush → repo service overwrite 门 → writeError 403 映射）+ T-378 §2 留痕 | 高（代码事实；live 未跑——stock 二进制 community 档不发 nuget 证） |

消费指引（FR-130.4 腿 **P2 性质**，本票只锚定不动行为）：
- **T-394 AC3**：as-built 对照结论登记——按 A6 现状 = **不一致**（403 ≠ 409）。
- **T-401**（条件票）：Q6 终裁=对齐时按 A5 翻转（403→409，臂语义照 §5.1 四臂：同字节/异字节一律 409、d 权限覆盖、新包 201）；终裁=有意差异则 T-401 不触发，本节降级为 D 层差异行（LC-66 归 D）。

## 6. 包存储路径解析（v2，此条补充官方规范）

`Download/{id}/{version}` 与 DELETE 的路径按三级链解析（DE，高）：

1. **进程内 v2 包缓存**（local 用本仓键；remote 用 `<repo>-cache` 键且受系统参数 `inMemoryNuGetRemoteCaches` 门控）。
2. **默认扁平路径**：`<id>.<version>.nupkg`（仓根一级，无 id 目录）——存在即命中。
3. **属性检索兜底**：AQL 按节点属性 `nuget.id` + `nuget.version` 精确匹配（remote 查 `<repo>-cache`；blacked-out 仓跳过检索）。publish 带 path 前缀的包靠这级找到。

推论（中）：带路径前缀上传的包仍可被 v2 协议端点寻址——**路径布局自由，寻址靠属性索引**，不依赖固定目录形状。BinFlow T-287 的 flatcontainer 布局（`<id>/<ver>/<file>` 三件套）与 v2 寻址正交，v2 实现层只需等价三级解析。

## 7. v2 remote/virtual search：上游代理语义（T-304-L3 remote 半边，T-337/T-341 消费）

### 7.1 remote（非 virtual）仓的 v2 搜索族

local 仓的 Search()/Packages()/GetUpdates()/FindPackagesById() 全部走**存储事实**（T-304-L3 local 半边维持）；remote 仓则（DE，高）：

1. **offline 且 storeArtifactsLocally** → 回落 `{repo}-cache` 本地缓存事实（等价 local 语义）。
2. **在线** → **实时代理上游 v2 OData**：
   - 上游 URL = `<repo.url>/<feedContextPath>/` + `<resourcePrefix>` + `?` + 全量查询参数（feedContextPath 默认 `api/v2`，xsd；resourcePrefix 即 `Search()`/`Packages()`/`GetUpdates()`/`FindPackagesById()` 及 `/$count` 变体）。
   - 上游返回的 Atom feed 解析后，**entry 内 URL 改写到本实例请求 base**（下载链接指回本仓 v2 Download 面）。
   - 上游异常/解析失败 → null → 客户端得 404（不 5xx）。
   - `$count` 族：上游返回纯数字体，解析失败/空体 → -1（计数族失败哨兵）。
3. **storeArtifactsLocally=false**（纯透传仓）仅告警，行为同在线代理。

### 7.2 virtual 的 v2 搜索合并（九步，DE `BaseRequestDelegate.responseFromVirtual`，高）

1. 成员解析：按优先级分组（PRIORITISED_REAL → NON_PRIORITISED_REAL），过滤到「当前 principal 有授权的 NuGet 成员」。
2. fan-out 取各成员 feed：local/federated 成员 = 存储事实（等价 local 语义）；remote 成员 = §7.1 上游代理（FindPackagesById/Packages 识别 pass-through 请求头）。
3. `$select` 在 fan-out 期间重置（成员端不投影，合并后再投）。
4. **GetUpdates() 无 `$orderby` 时自动注入 `$orderby=Version`**。
5. 合并去重：**优先级档内**逐成员并入；低优先档的条目若其**包 id** 已在高优先档出现则整包丢弃（**id 级去重，非版本级**——高优先档没有的版本不会从低优先档补）。
6. entry 内容链接统一改写为**本 virtual 仓**的 `Download/{id}/{version}`。
7. 排序：id 小写升序，同 id 内按 NuGet 版本比较器升序。
8. **IsLatestVersion 聚合**：两模式由系统参数 `nuget.virtual.feed.isLatest.legacy` 切换，**默认 true = legacy 模式**（ConstantValues 双证）：按 id 分组后，同 id 内后出现的非 prerelease 条目覆盖前者的 latest 标记；enhanced 模式（参数切 false）额外把 prerelease 条目一律逐出 latest。`inlinecount=allpages` 时总计数 = 各成员 count 之和；任一成员 feed 带 next 链接 → 保留 skip-token 语义。
9. 终滤：virtual 仓 include/exclude 路径 pattern 过滤条目，再按原 skip/top 出页。

## 8. v3 remote/virtual search：上游代理语义（T-304-L3'/L7，T-341 消费）

### 8.1 remote search 链（`downloadSearchResult`，DE，高）

1. **取上游 service index**：URL = 仓配置 `v3FeedUrl`（xsd 默认 `https://api.nuget.org/v3/index.json`）；经内部下载层取回并**缓存为仓内制品 `.nuGetV3/feed.json`**（复用 remote 仓缓存到期语义——上游 index 改版在下个缓存周期生效）。
2. **提取 SearchQueryService @id（层阶解析）**：依次找 `SearchQueryService` → `SearchQueryService/3.0.0-beta` → `SearchQueryService/3.0.0-rc`，首个命中者；全不命中 → 搜索失败（该成员贡献空）。
3. 组上游查询 URL：`q`/`skip`/`take`/`prerelease`/`semVerLevel`。
4. **直连上游 GET**（remote 仓 HTTP 客户端，结果不落盘为制品）。
5. **上游 400 → 以 take=100 重试一次**（兼容不支持大 take 的源）。
6. 其他非 200/空体 → null（成员贡献空，不 5xx）。
7. 结果 URL 全量改写到本实例（registration 系 URL → 本仓 v3 registration base；`semVerLevel` 非空时 base 带 `-semver2` 后缀）。

### 8.2 virtual search 合并（`collectAllSearchResultDataItems`，DE，高）

1. 成员解析：local 与 remote 在**同一优先档内并列**（PRIORITISED_{LOCAL,REMOTE} → NON_PRIORITISED_{LOCAL,REMOTE}），授权过滤。
2. 每档合并：**local 成员 = 元数据存储事实**（按 q 解析出的过滤词查元数据）；**remote 成员 = §8.1 上游代理**（每成员固定 take=1000 拉全量候选）。
3. 档内合并：按包 id 分组，**组代表 = 最高 semver 版本的条目**，`versions` = 各成员版本并集排序。
4. 档间合并：高优先档结果按 id 分组；低优先档**仅补高优先档没有的 id**（putIfAbsent 语义，与 v2 §7.2-5 同构）。
5. **分页在合并后内存执行**（skip/take），`totalHits` = 合并全量大小。
6. 输出的 registration base：`<v3base>/registration`（semVerLevel 端点 → `/registration-semver2`）。

### 8.3 与 BinFlow as-built 的可观测差异（裁决① 改回清单）

| 差异点 | Artifactory 行为 | BinFlow as-built（T-287-L3/L7） | 可观测面 |
|---|---|---|---|
| remote search 数据源 | 上游 SearchQueryService 实时代理（§8.1） | 已落地缓存事实 | remote 仓未落地的新版本能否被 `dotnet add package`（无版本号）搜到 |
| virtual search 贡献 | local 事实 + remote 成员上游实时（§8.2） | local 成员 + 已落地缓存 | 同上（virtual 面） |
| v2 同构面 | v2 搜索族同样代理上游（§7.1/7.2） | 404/缓存事实 | 老客户端（nuget.exe 2.x/VS 包管理器 v2 源） |

## 9. service index 动态解析（T-304-L4，T-341 消费）

### 9.1 资源类型 → 本实例内部形状映射（service index 对客户端的宣告）

remote/virtual 仓 v3 service index 里的资源 @id 指向本实例固定内部形状（DE，高）：

| 上游资源 @type | 本实例内部形状 |
|---|---|
| `RegistrationsBaseUrl`、`/3.0.0-beta`、`/3.0.0-rc`、`/3.4.0` | `<v3base>/registration/` |
| `RegistrationsBaseUrl/3.6.0`、`/Versioned` | `<v3base>/registration-semver2/` |
| `SearchQueryService`（+ `/3.0.0-beta`、`/3.0.0-rc` 别名） | `<v3base>/query` |
| `LegacyGallery{,/2.0.0}`、`PackagePublish/2.0.0` | v2 base |
| `PackageDisplayMetadataUriTemplate/3.0.0-rc` | `<v3base>/registration/{id-lower}/index.json` |
| `PackageVersionDisplayMetadataUriTemplate/3.0.0-rc` | `<v3base>/registration/{id-lower}/{version-lower}.json` |

### 9.2 反向解析（本实例形状 → 上游真实 @id）

客户端请求打到的内部形状按上表反查出**资源类型**，再到上游 service index 里取该类型的 @id 作为真实上游 URL（DE，高）。**版本化类型的缺省回落阶梯**（FeedUtils 替换表）：上游 index 缺 `RegistrationsBaseUrl/3.6.0|3.4.0|3.0.0-rc|3.0.0-beta|Versioned` 任一精确类型时，回落到 plain `RegistrationsBaseUrl`。

**关键行为（L4 裁决依据）**：上游资源定位是**类型驱动 + service index 实时查询**，不是「上游路径前缀常量」。上游改版（换 registration hive 拼写）、异构 v3 源（MyGet/Azure Artifacts/BaGet 等）自动适配；BinFlow as-built 的 `v3-flatcontainer`/`v3/registration5-gz-semver2` 前缀常量在「上游=nuget.org 族」时观测等价，上游异构/改版时失效。**nuget.org 观测补充**（T-287 现场探针）：其 registration 实际拼写是 `v3/registration5-gz-semver2/`（index 宣告为 `RegistrationsBaseUrl/3.6.0`）——前缀常量法对拼写漂移脆弱的另一实证。

### 9.3 `.nuGetV3/` 缓存布局（remote 仓内，此条补充官方规范）

上游 v3 资源经内部下载层（alternative URL + 禁重定向）取回并缓存为仓内制品（DE，高）：

| 缓存路径 | 内容 |
|---|---|
| `.nuGetV3/feed.json` | 上游 service index |
| `.nuGetV3/<registrationPath>/<id>/index.json` | registration index |
| `.nuGetV3/<registrationPath>/<id>/page/<lower>/<upper>.json` | registration 分页 |
| `.nuGetV3/<registrationPath>/<id>/<version>.json` | 单版本 registration |

（`<registrationPath>` 即上游宣告的 registration 路径段，如 `registration5-gz-semver2`。）

### 9.4 registration 改写规则

服务端返回 registration 时逐字段重建（DE，高）：

- `packageContent` → 改写到**本仓 v2 base** 的包下载 URL（版本小写化；非 smart 源剥 metadata）。
- `id`/`registration` 系 URL → 改写到本实例 v3 registration base。
- `catalogEntry` 业务字段（authors/description/deprecation/icon/license/listed/…）原样保留；`dependencyGroups` 内依赖的 registration URL 一并改写。

## 10. 与公开规范的差异/补充汇总

- **官方已定义、本规格直接引用**：v2 OData 端点全集与参数签名、semVerLevel 全语义、v3 service index/registrations/flatcontainer/push（Learn）、URL 形态（JFrog 文档）。
- **官方未定义、反编译补充**（均已标「此条补充官方规范」）：§3 两道鉴权门与例外表；§5 重复臂矩阵（409/d 权限覆盖/remote 400/virtual 409 Unacceptable path）；§6 三级路径解析；§7.2 virtual 九步合并（id 级去重/GetUpdates 注入 $orderby/IsLatest 聚合/pattern 终滤）；§8.1 上游 400→take=100 重试；§8.2 每成员 take=1000 与合并后分页；§9.1~9.4 内部形状映射、回落阶梯、`.nuGetV3/` 布局、registration 改写。
- **官方与实现的张力**：官方 v2 OData 允许 curated-feed 变体（`/api/v2/curated-feed/...`）与 v2 autocomplete（`package-ids`/`package-versions`）——Artifactory 反编译路由全集**未见**这些变体（DE 全量路由枚举），BinFlow 不做。

## 11. BinFlow as-built 对照与消费指引

| T-287 ruling | T-304 判定 | 归宿 |
|---|---|---|
| L1 DELETE 硬删/无 listed | 维持（无分歧） | 已对齐，无需动 |
| L2 v2 最小面（Search 等 404） | 改回（裁决①） | **T-337**：§2 全 18 端点 + §4 参数面 + §5 重复臂 |
| L3 local 半边存储事实 | 维持 | 已对齐 |
| L3 remote 半边缓存事实 | 改回（裁决①） | **T-341**（v3 面）/**T-337**（v2 面）：§7.1/§8.1 上游代理 |
| L4 上游前缀常量 | 改回（裁决①） | **T-341**：§9 动态解析（nuget.org 族默认观测等价，作为回归护栏） |
| L5 512MiB/4MiB 上限 | 维持（降级声明） | 残余差异附录（T-304 §6-1） |
| L6 版本归一化/小写折叠 | 维持（官方锚点） | 已对齐 |
| L7 virtual remote 成员贡献=缓存 | 改回（裁决①） | **T-341**：§8.2 合并 |
| （新）publish 重复臂 409+覆盖 | T-304 §7 新发现 | **T-337**：§5.1 |
| （新）同字节幂等臂 as-built 201（D-10） | 终裁=对齐 409（2026-08-30） | **T-378**：§5.1 四臂回归 + M12 L03 断言反转豁免（本表上方留痕） |
| （新）v3/flat 直推面重复臂 403 vs 409 | K59 锚定 = 409（§5.4，官方 + D-10 邻域） | **T-394**（as-built 对照登记）→ **T-401**（Q6=对齐时翻转） |

**T-337 实现要点**：① v2 路由按 §2 全集挂到既有 plane-aware rewrite（escaped 拼写保留）；② 鉴权门序 401→403（§3）；③ 重复臂照 §5.1（409 + d 权限覆盖 + virtual 409 `Unacceptable path.`）；④ FindPackagesById 缺 id=404 而非 400（易踩）；⑤ `$batch` 的 202 + `batchresponse_<guid>` boundary + part 查询串覆盖语义（§2 #13）；⑥ remote/virtual 的 v2 搜索代理按 §7（offline 回落、id 级去重、GetUpdates 注入 $orderby）。
**T-341 实现要点**：① SearchQueryService 层阶 + FeedUtils 回落阶梯（§8.1/§9.2）替换前缀常量；② `.nuGetV3/` 缓存布局（§9.3）；③ 上游 400→take=100 重试；④ virtual 合并（档内 local 事实+remote 代理、id 级 putIfAbsent、合并后分页、totalHits=全量）；⑤ registration 改写（§9.4，packageContent 指 v2 base）。

## 12. 待验证清单（live 不可证项留痕）

| # | 项 | 现置信度 | 验证途径 |
|---|---|---|---|
| 1 | `$batch` part 级查询参数覆盖的客户端可见效果（哪个真实客户端用 $batch） | 中（仅代码） | nuget.exe 2.x/VS 老版本抓包；无客户端用则降级为低优先实现 |
| 2 | v2 remote 代理对上游 feed URL 改写的精确字段集（entry 的哪些链接族被改写） | 中 | 对 nuget.org v2 源实测 Search() 比对改写字段 |
| 3 | ~~`nuGetVirtualEnableForbiddenResponse` 默认值~~ **已闭合（本票取证）**：`nuget.virtual.enable.forbidden.response` 默认 **false** → virtual download 全败默认 404（§5.3）。 | 高 | — |
| 4 | GetUpdates() 服务端过滤语义（packageIds/versions 版本比较是服务端做还是全量返回客户端筛） | 中 | 上游源实测 + 老客户端对照 |
| 5 | v3 remote 上游 take 上限行为（>1000 结果的源，virtual 是否丢尾） | 中 | 构造多版本上游源实测 |
| 6 | `DataServiceVersion` 响应头的精确值族（1.0 vs 2.0 在各端点的分布） | 中 | 抓包核对（本规格记 V2 为主，root/$metadata 为 OData 常量头） |
| 7 | A4：Artifactory 对 flatcontainer 路径非 multipart PUT 的实际状态码（415 vs 其他） | 中 | t226 活体/真实例 curl 直打（本票 stock 二进制无法起 nuget 面） |
| 8 | K59 翻转腿（T-401 触发时）的 live 四臂矩阵（v3/flat 面同字节/异字节×w/d 权限） | 中（代码事实已高） | T-401 票内 curl + dotnet 双客户端 |
