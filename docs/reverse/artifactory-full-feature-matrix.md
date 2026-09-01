# Artifactory 全量功能对照主矩阵（BinFlow M10+ 路线图骨干）

> 2026-08-25 · reverse-engineer 合成。来源：四份分区盘点（inv-1-core / inv-2-surface / inv-3-protocols / inv-4-addons）去重合并；反编译材料 `reverse-src/artifactory/src/{batch1-core,batch2-protocol,batch3-addons}`（只读，ADR-0001 clean-room）。
> 参考版本：**Artifactory 7.161.11**（revision 86111900，package.handler 5.675.22；`META-INF/artifactory.version.properties`）。
> 每行：**功能 | Artifactory 行为要点 | 证据（多分区并列）| 置信度 | BinFlow 覆盖终判**。覆盖终判对照 BOARD.md M1~M9、docs/prd/milestone-{1..9}.md 与实际代码面（internal/httpapi/router.go 路由清点、internal/adapter 五包型、internal/{auth,storage,repo,replication,migrate,metrics} 包存在性逐项核实）。
> 覆盖取值：**已有**（含等价实现）/ **部分**（核心在、面窄或有语义差）/ **缺失** / **不适用**（开源对标下设计上不做，N-A）。置信度：高=代码+公开文档双证；中=仅代码；低=推断待动态验证。

## 0. 总览统计

| 分组 | 条目数 | 已有 | 部分 | 缺失 | 不适用 |
|---|---|---|---|---|---|
| 一、包型与协议面 | 73 | 3 | 11 | 58 | 1 |
| 二、仓库模型 | 13 | 0 | 3 | 9 | 1 |
| 三、安全与认证 | 22 | 6 | 4 | 12 | 0 |
| 四、治理与运维（含存储后端） | 47 | 5 | 14 | 27 | 1 |
| 五、企业集成 | 15 | 0 | 1 | 11 | 3 |
| 六、复制与分发 | 10 | 0 | 4 | 6 | 0 |
| 七、REST 面 | 8 | 0 | 5 | 3 | 0 |
| 八、控制台特性 | 13 | 5 | 5 | 2 | 1 |
| 九、部署形态 | 5 | 1 | 1 | 2 | 1 |
| 十、其他 | 6 | 0 | 2 | 3 | 1 |
| **合计** | **213** | **20** | **50** | **133** | **10** |

- 合计 213 行（纯包型 55 行覆盖 57 个枚举值——alias 变体并行）；平台能力条目 158。
- 覆盖率：已有 9.4%（20/213）；已有+部分 32.9%（70/213）；扣除「不适用」10 项后 34.5%（70/203）；**剔除纯包型行后的平台能力面 158 行中已有+部分 40.5%（64/158）**。
- 置信度分布（按行）：高 ≈95 / 中 ≈90 / 低 ≈22（低项集中于 JFrog 内部新功能：Load Healer、devops agent、workers、jfbus、MFE、grid、shadow、HQC、intransit、packagereroute、FROGML/JEM）。
- 端点规模参照：Artifactory ≈1,866 个方法级 REST 操作（inv-2 §0）；BinFlow M9 后 `/binflow/api` 注册路由 ≈50 个操作 + 五包型内容面。

---

## 一、包型与协议面

### 1.1 BinFlow 已有五包型（对齐差距逐项列出）

| 功能 | Artifactory 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| Docker（Registry v2） | v2 全套（blob/manifest/uploads PATCH 断点、_catalog、tag 列表）+ v1 兼容残留（X-Docker-Token 等）+ 镜像 promote + maxUniqueTags/tagRetention + resolveDockerTagsByTimestamp + foreign layer 缓存 + DSSE 签名证据 | inv-2 §1.J DockerV2Resource(17)+DockerSubResource(26)；inv-3 §2.2 | 高 | 部分：v2 面已有（含跨重启续传 T-216、blob mount、401 challenge+scope）；v1 兼容、promote、tag retention、DSSE 缺 |
| Maven | maven-2-default layout、suppressPomConsistencyChecks、fetchJars/SourcesEagerly、rejectInvalidJars、pomRepositoryReferencesCleanupPolicy、forceMavenAuthentication、metadata 计算 + compare 端点 | inv-3 §2.1 + MavenResource(3) | 高 | 部分：布局/元数据计算/校验和部署已有；78 个包型配置键面窄、`compare` 缺 |
| npm | 完整 registry 语义 42 操作（dist-tags、scope 包、tarball、auth/token 命令端点、-/ping） | inv-2 §1.J ph/npm | 高 | 已有（含 M8 T-249 追加/deprecate 语义对齐、dist-tag 移动） |
| PyPI | PEP 503 simple 索引 + 上传表单 + `list/`、`simple/` 前缀剥离 | inv-2 §1.J ph/pypi(17)；inv-3 §2.3 | 高 | 已有 |
| Generic | simple-default layout + 矩阵参数属性化部署 + exploded archive 解包 | inv-3 §2.2；rest-api.md | 高 | 部分：PUT/GET/DELETE/checksum-deploy 已有；矩阵参数、explode（X-Explode-Archive 显式 400 拒绝）缺 |

### 1.2 其余 52 包型（枚举 57 − 已有 5；GRADLE 走 Maven 通道标部分）

| 包型 | 行为要点（协议核心） | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| GRADLE | 无独立 HTTP 面，复用 maven/ivy layout 部署 | inv-3 §2.1 | 中 | 部分（走 maven 通道可用） |
| IVY | ivy-default layout（14 位时间戳 integration 版本） | artifactory.config.xml | 高 | 缺失（layout 级） |
| SBT | sbt-default + sbt-ivy-default（scala_/sbt_ 路径段） | 同上 | 高 | 缺失（layout 级） |
| OCI | 独立 OCI 包型（ORAS 类工件）+ OciV2AuthenticationFilter | inv-3 §2.2 addon/oci | 高 | 缺失 |
| HELM | 经典 chart 仓：index.yaml 维护、_external/_transitive 依赖代理（含 .prov 签名） | inv-3 §2.2 ph/helm | 高 | 缺失 |
| HELMOCI | Helm over OCI（chart 以 OCI artifact 推拉） | addon/helmoci | 中 | 缺失 |
| NUGET | v2 OData（Search()/FindPackagesById()/GetUpdates()）+ v3（flatcontainer/SearchQueryService/签名索引）+ symbol server（.pdb/GUID 路径） | inv-3 §2.3 ph/nuget + 根 v2/v3 静态文档 | 高 | 缺失 |
| CONAN | v1（users/authenticate、conans）+ v2 全修订链（rRev/pRev）+ 迁移 job + forceConanAuthentication | inv-3 §2.3 ph/conan(37)+addon/conan | 高 | 缺失 |
| CARGO | api/v1/crates（publish/yank/download/search）+ 内部 sparse 索引 + public_key + cargoAnonymousAccess | inv-3 §2.3 ph/cargo | 高 | 缺失 |
| GO | GOPROXY 协议（@v/list、.info/.mod/.zip、@latest）+ sumdb 代理 + external 依赖重定向 | inv-3 §2.3 ph/go | 高 | 缺失 |
| GEMS | rubygems（gem push multipart、quick/Marshal 索引）+ 引导静态 gems | inv-3 §2.3 addon/gems | 高 | 缺失 |
| COMPOSER | packagist 元数据（packages.json、p/、p2/、registration）+ v1 索引开关 | inv-3 §2.3 ph/composer | 高 | 缺失 |
| SWIFT | /api/package 面（.zip/.json/Package.swift、@{scope} 转义）+ _external 代理 | inv-3 §2.3 ph/swift | 高 | 缺失 |
| PUB | /api/packages/{name}、tarballs、dist-tags + PubSpec 索引合并 | inv-3 §2.3 ph/pub | 高 | 缺失 |
| CONDA | repodata 四形态（json/bz2/zst/current）+ curated repodata 过滤 | inv-3 §2.3 ph/conda | 高 | 缺失 |
| CRAN | CRAN 源布局（src/contrib）+ simple 索引 | inv-3 §2.3 ph/cran | 中 | 缺失 |
| COCOAPODS | CDN specs 哈希分片 + /pod/{git,external,pkg} 代理 | inv-3 §2.3 ph/cocoapods | 高 | 缺失 |
| BOWER | bower registry 元数据 + bower-default layout | inv-3 §2.3 repomd/bower | 高 | 缺失 |
| HEX | mix registry（packages/names/versions/new）+ 内置 hex 公钥验签 | inv-3 §2.3 ph/hex | 高 | 缺失 |
| LUAROCKS | api/1/{key}/upload、check_rockspec、status | inv-3 §2.3 ph/luarocks | 中 | 缺失 |
| NIMMODEL | Nim 包模型专用 API 面 | inv-3 §2.3 addon/nimmodel | 中 | 缺失 |
| BAZELMODULES | Bazel Central Registry 风格（modules/{repoKey}__{ns}） | inv-3 §2.3 ph/bazelmodules | 中 | 缺失 |
| NIX | 二进制缓存协议（nix-cache-info、.narinfo、nar/*.nar.xz/zst） | inv-3 §2.3 ph/nix | 高 | 缺失 |
| VCS | GitHub/GitLab 原始码代理（refs/tags/branches、downloadTag/Branch、下载令牌）+ /api/vcs REST 面 | inv-2 §1.H VcsResource(21)；inv-4 P1 | 中 | 缺失 |
| GITLFS | LFS batch API + 单对象 + verify + 锁协议全套（locks/verify/unlock/force-unlock） | inv-3 §2.3 ph/gitlfs | 高 | 缺失 |
| DEBIAN | apt 布局 + 索引重算 + **By-Hash 三档**（ALL/SHA256/NONE）+ ddeb + optionalIndexCompressionFormats | inv-3 §2.4 repomd/debian(38 文件) | 高 | 缺失 |
| YUM(RPM) | repomd 元数据计算（fileLists、groupFilenames）+ /rpm reindex | inv-3 §2.4 ph/rpm + repomd/rpm | 高 | 缺失 |
| ALPINE | APKINDEX.tar.gz + RSA 签名索引（.SIGN.RSA. 模式） | inv-3 §2.4 ph/alpine | 高 | 缺失 |
| OPKG | Packages 索引型轻量协议 | inv-3 §2.4 ph/opkg | 中 | 缺失 |
| TERRAFORM | Registry 协议（well-known 服务发现、v1/modules search+versions+download、v1/providers） | inv-3 §2.5 ph/terraform/registry | 高 | 缺失 |
| TERRAFORMBACKEND | HTTP state 后端（state-versions、workspaces、**state 锁** lock/unlock/force-unlock、JWT 身份） | inv-3 §2.5 ph/terraform/backend | 高 | 缺失 |
| ANSIBLE | galaxy v3 API（collections/imports/published index docs-blob）+ v1 cookbooks 兼容 | inv-3 §2.5 ph/ansible | 高 | 缺失 |
| PUPPET | forge v3/v1（releases、modules） | inv-3 §2.5 repomd/puppet(34 文件) | 高 | 缺失 |
| CHEF | supermarket API（cookbooks、search、universe） | inv-3 §2.5 repomd/chef(24 文件) | 高 | 缺失 |
| VAGRANT | atlas/box API（boxName 查询与下载） | inv-3 §2.5 ph/vagrant | 中 | 缺失 |
| P2 | Eclipse p2 composite 仓 + $metadata/$batch OData + forceP2Authentication | inv-3 §2.5 ODataBatchProvider | 中 | 缺失 |
| HUGGINGFACEML | HF 协议克隆（preupload/commit/branch 可断点、resolve、model-manifest）+ **xet CAS 子协议** | inv-3 §2.6 ph/huggingface(42 操作) | 高 | 缺失 |
| MACHINELEARNING | 通用 ML 仓（models/datasets 双实体、tree/paths-info） | inv-3 §2.6 ph/machinelearning | 中 | 缺失 |
| FROGML | JFrog 自家 ML 工件类型，未见独立处理器目录 | 仅 PackageType 枚举 | 低 | 缺失 |
| VSCODEEXTENSIONS（含 VSCODE 变体） | VS Code marketplace 协议（_apis/public/gallery、extensionquery） | inv-3 §2.6 | 中 | 缺失 |
| JETBRAINSPLUGINS（含 JETBRAINS 变体） | JetBrains 插件市场（meta.json、compatibleUpdates、pluginManager） | inv-3 §2.6 ph/jetbrains | 中 | 缺失 |
| AIEDITOREXTENSIONS | AI 编辑器扩展市场（gallery vscode 变体） | inv-3 §2.6 ph/aieditorextensions | 中 | 缺失 |
| SKILLS | agent skills API（api/v1/skills/{slug}/versions/file） | inv-3 §2.6 ph/skills | 中 | 缺失 |
| AGENTPLUGINS | agent 插件分发（agent-plugins-default layout，文件型 API） | inv-3 §2.6 + config.xml | 中 | 缺失 |
| AGENTPACKAGES | agent 包分发（agent-packages-default layout） | inv-3 §2.6 + config.xml | 中 | 缺失 |
| JEM | 内部新类型，未见处理器目录 | 仅枚举 | 低 | 缺失 |
| BUILDINFO | build-info 内部仓型（CI 构建元数据存储） | inv-3 §2.7 | 中 | 缺失（整条 BI 链路，见五） |
| RELEASEBUNDLES | Distribution 发布包内部类型 | inv-3 §2.7 | 低 | 缺失（Distribution 域，见五） |
| PIPEINFO | Pipelines 集成内部类型 | inv-3 §2.7 | 低 | 缺失（不适用倾向） |
| SUPPORT | support bundle 内部存储类型 | inv-3 §2.7 + addon/support | 中 | 缺失（见四 Support Bundle 行） |

### 1.3 横切协议能力（所有包型共享）

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| 矩阵参数属性部署 | 路径（含 repoKey 段）`;k=v;k2=v2` 统一剥离转 properties 随 PUT 存储；非法键 400；`lic+` 后缀强制属性 | inv-3 §3.1 calculateRepoPath；rest-api.md（官方文档双证） | 高 | 缺失（layout.go 明示 M1 无此语义）——**横切基座，最高优先缺口** |
| 归档内路径 `archive!/` | `<name>.<ext>!/inner/path` 按需解包成员读取；strictArchiveDotSlash 严格模式 | inv-3 §3.1 | 高 | 缺失 |
| 目录/整仓 zip 下载 | folderDownloadConfig（默认关；1024MB/5000 文件/10 并发/匿名单独开关）+ `GET /api/archive/download`（计流量）+ buildArtifacts 按构建拉归档 | inv-1 F ArchiveResource；inv-2 §1.A；inv-3 §3.3 | 高 | 缺失 |
| 归档内抽取下载 | archive/download entry 参数：从归档内抽单个文件返回 | inv-2 §1.A | 高 | 缺失 |
| Checksum 部署三头变体 | X-Checksum-Deploy（零 body 引用已有二进制）+ X-Check-Binary-Existence-In-Filestore（仅查 filestore）+ X-Checksum-Deploy-Token（MPU 完成头） | inv-3 §3.2 UploadServiceUtils | 高 | 部分：第一种已有（maven+generic 两 adapter，npm/pypi/docker 无）；后两种缺 |
| 客户端断点续传 REST（MPU） | generateToken→分片 URL→status/abort/complete(sha1)，scope internal:mpu:x | inv-3 §3.2 MultipartUploadService | 高 | 部分（storage.Session/S3 MPU 机制在；无客户端 REST 面——快赢项） |
| 请求路径归一化中枢 | 单点实现：dot-segment 折叠、`+`→`%2B` 预解码、PyPI list/simple 前缀剥离、repoKey 别名替换（substituteRepoKeys） | inv-3 §3.1 | 高 | 部分（分散在各 adapter；dot-segment 有 400 防御；`+` 解码与别名缺） |
| 条件请求语义 | ETag/If-None-Match（弱标签 W/ 剥离）、If-Modified-Since 与 Last-Modified 取 max | inv-3 §3.3 | 高 | 部分（ETag=sha1 已有；弱标签与双头并存语义待对查） |
| Range 切片 | Range 头即 isRange，byte range 响应 | inv-3 §3.3 | 高 | 已有（generic 206） |
| 下载重定向/CDN | 远端仓 downloadRedirect（阈值 302 源站）+ CdnRedirectRepoConfig | inv-3 §3.3；inv-1 C | 中 | 部分（S3 presigned 重定向已有；CDN 层与阈值策略缺） |
| 递归环防护 | X-Artifactory-Originated/Origin-Artifactory 头识别对端 Artifactory；多 origin 含本机 hostId 判递归 | inv-3 §3.3 | 中 | 缺失（多级 pull-through 链式前需要） |
| 按包型强制认证开关 | forceMaven/Conan/Nuget/P2Authentication、cargoAnonymousAccess（反向放行）、hex 公钥校验 | inv-3 §3.4 PackageTypeConfigFields | 高 | 缺失（BinFlow 匿名策略全局，非按包型/按仓库） |
| external dependencies 模式分流 | 虚拟仓按 pattern 将外部依赖请求重定向到指定远端仓（Go/VCS/npm/bower/helm/swift/p2/vscode 各自实现） | inv-3 §3.4 ExternalDependenciesConfig | 高 | 缺失（pull-through 不支持 pattern 分流） |
| Docker bearer challenge 形态 | 401 带 realm/service/scope（pull/pull,push 细粒度）+ token URL 模板 `{repoKey}/{v1|v2}/token` | inv-3 §3.4 | 高 | 部分（平铺 /v2/token；scope 语义已有、per-repo token 路径形态缺） |
| X-Artifactory-* 内部头族 | 29 个内部头（Created/Created-By/Trace-Id/HA-Originated/Project-Key/Package-Url/Pass-Through 等），服务于复制/HA/追踪/云直传 | inv-3 §3.5（grep 全集） | 高 | 极少部分（无对应头族） |
| 特性版本协商 | smartrepo-features.xml 6 特性按对端版本/revision 开关（SYNC_PROPERTIES→REPLICATION_INCLUDE_EXCLUDE_PATTERNS） | inv-2 §2 | 高 | 缺失（多级 pull-through 需等价能力握手） |
| exploded archive 解包上传 | explodedArchiveExtensions=zip,tar,tar.gz,tgz；解包为多文件部署 | inv-2 §5 system.properties | 高 | 缺失（X-Explode-Archive 显式 400 拒绝） |
| 双轨包框架灰度 | org.jfrog.repomd（老 17 型）与 com.jfrog.ph（新 25+ 型）并行，`artifactory.shadowing.enabled` shadow 切换 | inv-2 §0/§6 | 高 | 不适用（架构差异；单轨实现） |

---

## 二、仓库模型

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| rclass 四态 | local/remote/virtual/**federated**；另有内部 release-bundles 仓型与 auto-trashcan 内置 local 仓 | inv-1 C；inv-2 §1.B | 高 | 部分（前三已有；federated 缺，见六） |
| 包型枚举 57 | 57 个 PackageType，每型 78 个配置键（PackageTypeConfigFields） | inv-1 C；inv-3 §1 | 高 | 部分（5/57；配置键面未建模） |
| 自定义仓库布局 | 模块路径模式（token 正则/条件段/自定义捕获组）+ UI CRUD + testArtPath 路径试解析 + resolveRegex；内置 25~26 条 layout | inv-1 C；inv-2 §1.B/§5；inv-3 §4 | 高 | 缺失（五包走固定布局，不可自定义） |
| 仓库配置 CRUD v1/v2 + batch | v2 schema 新版配置；`v2/repositories/batch` 一次多仓创建/替换/校验/删除（含批量删除报告） | inv-2 §1.B；inv-1 C | 高 | 部分（自有 CRUD + 配置校验已有；无 v2 schema 与 batch） |
| Copy/Move/flat | `POST /api/copy|move/{path}`（树级 dryRun）+ flat 扁平化布局复制（dry+failFast） | inv-1 C；inv-2 §1.A | 高 | 缺失（push 复制是仓库级；无路径级 copy/move） |
| Zap/repairPaths | zap：删数据存储记录保留二进制（催 GC）；repairPaths：补缺失父目录/冲突路径修复（均支持 dry） | inv-1 C；inv-2 §1.A | 中 | 缺失 |
| Maven/Yum 索引器 | Maven 索引 cron（默认每日 05:23）+ 手动 calc/purge；Yum 异步 reindex（async 参数） | inv-1 C；inv-2 §1.B/§5 | 高 | 缺失（Maven 索引可后补；Yum 随包型） |
| 虚拟仓/缓存维护 | cleanUnusedCache（清远端未用缓存）+ cleanVirtualRepo（清虚拟仓聚合缓存）+ virtualCacheCleanupConfig cron | inv-1 C；inv-2 §5 | 中 | 缺失（pull-through 缓存无清理策略端点） |
| 远程仓守护与统计 | offline guard job + 手动触发/恢复端点（自带 openapi 348 行）；`v1/stats/remotes` 远端成功率/延迟 | inv-1 C；inv-2 §1.H | 中 | 缺失（无远端熔断标记面） |
| 代理服务器管理 | 命名代理 CRUD（remote 仓库引用） | inv-1 D；inv-2 §1.H | 中 | 缺失（remote 无代理配置） |
| Versions API | `GET /api/versions/{repoKey}/{path}`（跨仓 `_any` 通配；SNAPSHOT/RELEASE 语义） | inv-2 §1.A | 高 | 缺失（Maven metadata 内有版本列表，独立 API 无） |
| 属性集（property sets） | 预定义属性集 CRUD（闭集值/校验），仓库引用 | inv-1 F | 高 | 缺失（连自由属性面都缺，见七 REST 属性行） |
| 新元数据服务 | metadata/{path} 自定义 metadata（区别 properties）+ metadata_server reindex + stats/recreate | inv-1 F/§C；inv-2 §1.A | 中 | 不适用（架构差异；如做属性系统可一并评估） |

---

## 三、安全与认证

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| 认证中枢（Access 服务化） | 用户名密码/token 全部委托 Access；scope 令牌模型（applied-permissions/member-of-groups/repo-path/system-admin）；gRPC+protobuf 通道（16 个 .proto） | inv-1 A；inv-2 §5 | 高 | 部分（自建 OIDC/LDAP+本地；无 Access 中枢属设计差异，单体合理） |
| Token 服务 | `POST /api/security/token` 三种 grant_type（client_credentials/refresh_token/password）、GET 列表（admin）、revoke（token/token_id/hint） | inv-1 A TokenResource；inv-2 §1.D | 高 | 部分（create+revoke 已有；无 GET 列表与 refresh_token 形态；**有 step-up 扩展超出参考**） |
| API Key | 生成/再生/吊销；UI 取回需密码复认；7.x 已标废弃 | inv-1 A；inv-2 §1.D | 高 | 缺失（可用 token 替代，倾向不做） |
| 密码策略族 | 过期策略（maxAge/邮件通知）+ 登录失败递增锁定（(n-3)*500ms 上限 5s）+ unlockUsers/lockedUsers 端点 + 加密密码回显 | inv-1 A；inv-2 §1.D/§5 | 高 | 缺失（仅改密端点 security/password） |
| 主加密/配置加解密 | 首次启动描述符整体加密；手动 encrypt/decrypt；远端密码单独加解密静默回写；binarystore.xml 加密 | inv-1 A；inv-2 §1.H system/encrypt | 高 | 缺失（无配置整体加解密操作面） |
| 敏感字段脱敏框架 | @DynamicSensitive 注解 + 字段处理器在 diff/序列化时掩码 | inv-1 A o.a.sensitive.* | 中 | 缺失（配置回显无系统性脱敏） |
| SAML SSO | SAML SP + SamlGateway 登录回调 | inv-1 A；inv-2 §1.D | 高 | 缺失 |
| Crowd SSO | Atlassian Crowd 目录认证 | inv-1 A；inv-4 M1 | 中 | 缺失 |
| OAuth2/GHE/HTTP-header SSO | OAuth2 授权码+PKCE、GitHubEnterprise provider、信任 SSO 头、openid 网关回调 | inv-1 A；inv-2 §1.D | 高 | 部分（OIDC 已有；通用 OAuth provider 框架/信任头缺） |
| OIDC | 提供者集成、自动建用户、组映射 | inv-1 A；M6 FR-54 | 高 | 已有（+step-up mint grant 超集） |
| LDAP | 多 LDAP server 配置 UI + 组映射（ldapgroups 7+7 操作）+ pool | inv-2 §1.D；auth-integration.md | 高 | 部分（单 server env 配置可用；无多 server 管理面与组映射） |
| 匿名访问 | 匿名 token + 匿名权限面 | inv-1 A | 高 | 已有（anonymous_access 实例级） |
| 用户/组/权限 CRUD | v1 15 操作 + v2 8 操作（分页过滤）+ 权限目标 | inv-2 §1.D | 高 | 已有（users/groups/permissions 全 CRUD；M9 补 enabled 回显与 DELETE users；v2 分页过滤面窄） |
| 授权面（per-user） | `security/users/authorization`：按用户列可访问仓库/权限目标 | inv-2 §1.D | 中 | 缺失（仅 changePassword 端点占用该路径前缀） |
| 委托登录 | `POST /api/security/auth/login` 供网关二次校验 | inv-1 A | 中 | 已有（POST /api/v1/session 等价） |
| SSH 认证（服务器侧） | ssh 公私钥上传/替换 + UI 设置页 | inv-1 A；inv-2 §1.D | 中 | 缺失 |
| 密钥对/trusted keys/签名密钥 | 仓库级 RSA keypair + gpg 受信密钥（校验远端签名元数据）+ debian/rpm 元数据签名 KeyStore；REST `security/keypair`、`security/keys/trusted` | inv-1 A；inv-2 §1.D；inv-4 M3/M4 | 高 | 缺失（无索引签名与验签体系） |
| 证书信任库 | 远程仓库 SSL 证书导入/列出/删除（/api/system/certificates） | inv-1 A；inv-2 §1.D | 高 | 缺失（remote TLS 自定义 CA 不可配） |
| 签名 URL | 独立签名密钥（configs 存储加密）对下载 URL 签发/校验 | inv-1 A SignedUrlServiceImpl | 中 | 缺失（S3 presigned 可部分替代） |
| Service Trust / Token Exchange | JFrog 服务间配对（pairing token 按 usecase 签发/吊销/列出）+ RFC 8693 风格 token exchange | inv-1 A；inv-2 §1.H；inv-4 M6 | 中 | 缺失（单体可不建；联邦/分发前置） |
| RBAC 模型对位 | admin/user 两角色 + 权限目标（repo+pattern+principal 三元组）+ hideUnauthorizedResources | auth-model.md；rbac-model.md | 高 | 已有（三值角色+六能力+manage 派生+m 动作，M7/M9 已收口；模型表达力不同但覆盖对位） |
| 审计日志 | 安全/仓库操作审计（approved/rejected 双流） | inv-1 A AccessLogger | 高 | 已有（append-only + GET 查询 + gc/role 等事件） |

---

## 四、治理与运维

### 4.1 生命周期治理

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| Trash can（回收站） | 内置 `auto-trashcan` 仓；删除拷入并打标（trash.time/deletedBy/originalRepository/originalPath）；保留期 14 天；`POST /api/trash/empty`、restore（to+transaction-size）、clean；目录级 trash 属性批量清理 | inv-1 B/G；inv-2 §1.A；config.xml trashcanConfig | 高 | **缺失**（删除即永久）——数据安全事故源，高优先 |
| Cleanup v2 策略引擎 | DB 策略实体（package/build/bundle 三类）+ 手动 run + dry-run view + run 摘要/报告下载/forceStop + NodeCleaned 记录 + `supportedPackages/full` | inv-1 G；inv-2 §1.F；inv-4 O1 | 高 | **缺失**（仅 GC 管孤儿块） |
| Cleanup v1（cron） | cleanupConfig 每日 05:12 + 清理选择器（unused deps/one per version） | inv-1 G；inv-2 §5 | 中 | 缺失 |
| Retention v2 | 包保留策略（search criteria+master token）+ 归档策略 UI（11 操作）+ RestoreRunSummary + 覆盖率/迁移工具面 | inv-1 G；inv-2 §1.F；inv-4 O2 | 中 | 缺失 |
| 冷存储 Archive | 归档到冷层（策略 cron/dryRun、runs、restore warm/cold 两路径、consumption 用量、恢复状态 job） | inv-2 §1.F；inv-4 L4 | 中 | 缺失 |
| GC/维护操作 | maintenance 面：garbageCollection / cleanUnusedCache / cleanVirtualRepo / compress；GC cron 每 4h + 六节流参数 + prune status 可查 | inv-1 B/G；inv-2 §1.H/§5 | 高 | 部分（GC 已有含 graceHours 语义与引用原子化 M9；compress/缓存清理缺） |
| 备份 | 备份集配置（默认 daily MON-FRI 02:00 增量 + weekly SAT 02:00 保留 336h）+ 大小预估 + HA 独立备份目录 | inv-1 G；inv-2 §5 | 高 | 已有（自有 pg_dump+filestore 备份面，M4；无多策略 cron 配置 UI） |
| 导入/导出 | 系统级/仓库级导入导出 REST + 流式状态 | inv-1 G；import-export-api.md | 高 | 部分（bf-migrate 入迁工具 + local→S3 在线迁移已有；无 Artifactory 式导出/导入 REST） |
| 上传会话清理 | 上传会话过期清扫 | inv-1 G；inv-2 §1.J | 高 | 已有（upload_sessions 表 + sweep，T-209） |
| 事件日志治理 | events/log 清理端点 + tmp event 分区截断 job | inv-1 G | 中 | 缺失 |
| 统一任务/作业 API | 后台任务模型 + jobs 列表/单查 + jobs/replications 视角 | inv-1 G/H | 中 | 部分（GC/迁移/复制各有状态面；无统一任务 API） |

### 4.2 可观测与运行保护

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| 指标暴露 | 内部指标框架 + Prometheus 集成 | metrics.md | 高 | 已有（/metrics 四类指标，M6 FR-61） |
| 流量记账 | 按节点入/出流量 DB 账本（stats+summary 两表族）+ traffic/filter/summary REST | inv-1 H | 中 | 缺失（Prometheus 瞬时值，无持久流量账本） |
| 限流体系 | 请求级 v2（策略+指标 provider）+ 查询级 QRL（AQL 并发，enabled/disabled/simulation 三态）+ 搜索级三级限流器 | inv-1 H；inv-2 §1.H | 高 | 缺失（无限流） |
| Load Healer | 过载自愈：10 活动（xrayBlock/qrlLowPriority/parallelUpload 等）阈值=tomcat maxThreads 50–90%，healthyTimeout 60s；节点排空 | inv-1 H；inv-2 §5 application.yaml | 高（阈值表）/低（活动语义） | 缺失（单节点形态下可做简化版） |
| 入站请求守卫 | OFF/SHADOW/ACTIVE 三模式按 header/User-Agent/IP-CIDR 拒绝（自带 openapi 174 行） | inv-1 H；inv-2 §1.H | 高 | 缺失 |
| Support Bundle | 一键诊断包（7 类收集器：系统/配置/日志/前端/melt/微服务/grid）+ 状态/下载/删除 + HA 指定 node；上限 5 个 | inv-1 H；inv-2 §1.H；inv-4 N1 | 高 | **缺失**（要登录机器收集） |
| Live Logs | 日志配置列举 + 内容流式拉取（UI 实时日志） | inv-1 H；inv-2 §1.H | 高 | 缺失 |
| 运行时 debug 日志级别 | GET/POST/DELETE system/debug/loggers/{className} 按类调级 | inv-1 H | 中 | 缺失 |
| DevOps Agent 观测 | 出站请求日志（列表/trace）+ 错误目录（错误码条目+事件入库） | inv-1 H | 低 | 缺失（现代等价=结构化日志+trace） |
| 系统状态面 | ping/version（+product/service feature 列表）/status/serverTime/usage | inv-1 H；inv-2 §1.H | 高 | 部分（ping/version/v1/health/healthz/readyz 已有；status/serverTime 缺） |
| 使用遥测 usage | 匿名使用数据上报开关 | inv-1 H | 中 | 缺失（可不做/默认关） |
| 深度探针 probes | 深度健康探针（含 tomcat 级） | inv-1 H | 低 | 部分（healthz/readyz 含存储可写探针） |
| Observability 日志外发 | log shipping / billing / usage 三类日志外发（v1/system/logs/*） | inv-2 §1.H；inv-4 N2 | 中 | 缺失 |
| SumoLogic 集成 | 配置 + OAuth 回调 + token 刷新 | inv-1 D；inv-2 §1.H | 中 | 缺失（可不做） |
| 反向代理配置生成 | webServer 配置 CRUD + nginx/apache/router 四模板片段生成 | inv-1 D；inv-2 §1.H/§5 templates/*.ftl | 高 | 缺失（部署文档手工给配置） |
| 邮件服务器/通知 | SMTP 配置 + `POST /api/send_mail` 测试发送 + 配额/watch/密码过期邮件模板 | inv-1 D；inv-2 §1.H；inv-4 N5 | 高 | 缺失（无邮件子系统） |
| 运行时系统属性管理 | PUT/DELETE /api/system/properties/update|delete 运行时改 JVM 属性 + ≈120 个 system.properties 行为开关 | inv-1 D；inv-2 §5 | 中 | 缺失 |
| Onboarding/QuickStart | 首启向导（initStatus/createDefaultRepos/quickstart 模板） | inv-1 H；inv-2 §1.H；inv-4 N6 | 中 | 缺失（无首启向导） |
| SetMeUp | 按 repo/packageType 生成连接说明（12 种 gradle/maven 模板 + 凭据） | inv-1 H；inv-2 §1.H | 高 | 部分（控制台 T-242 对话框已有五包型；无独立 REST 面） |
| 文件系统浏览器 | admin 浏览服务器文件系统（导入/导出路径选择） | inv-1 H | 中 | 缺失（CLI 有等价力） |
| UI 工具族 | cron 计算器、校验器、预定义值、自动完成、分页 repodata | inv-1 D/H | 中 | 部分（控制台自带部分校验） |
| Watch（制品关注） | 关注路径/repo，部署/删除邮件通知（HTML 模板）；watcher 信息持久化在制品元数据 | inv-1 F；inv-3 §3.5；inv-4 O3 | 高 | 缺失（且依赖邮件子系统） |
| 影子对比（shadow） | 请求镜像发 shadow 实例比对 status/headers/location 产指标（迁移验证） | inv-1 B | 低 | 缺失（可选不做） |
| Checksum 补算端点 | `POST /api/checksum/sha256` 按路径补算 + sha2 迁移任务（start/stop） | inv-1 B；inv-2 §1.H | 中 | 不适用（BinFlow 原生 sha256 双校验，无存量迁移问题） |

### 4.3 存储与后端

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| 文件存储 VFS | 目录树 + 事务会话 + workspace 暂存 + stats/checksum 子元数据 | inv-1 B；storage-layout.md | 高 | 已有（自有 filestore + sha256 命名） |
| 全 DB 元数据 + 多库矩阵 | 六方言（DERBY/MYSQL/ORACLE/MSSQL/POSTGRESQL/MARIADB，DDL 实际五套）+ Liquibase 风格 converter 链 + Derby 内嵌默认（30+ 处降级特判） | inv-1 B；inv-4 K1/K2/K5 | 高 | 部分（单 PG 策略，设计取舍；多库方言层不做） |
| 存储汇总 | filestore/binaries/repo 三层 summary + 15min 缓存 + binary providers 链展示 | inv-1 B；inv-2 §1.A | 高 | 部分（v1/storage/stats + per-repo usage 已有；三层 summary/缓存缺） |
| 磁盘配额 | 阈值告警 + 邮件通知；项目级 soft/hard 配额 + 通知 job | inv-1 B；inv-4 E3 | 高 | 部分（仓库级 quotaBytes + 实例统计已有；无邮件告警/项目级） |
| 校验和体系 | sha1+sha256 双校验；filestore 流可用时附加 sha256 属性 | inv-1 B | 高 | 已有（sha256 主 + sha1 兼容） |
| MPU REST | `/api/v1/uploads` 六端点（create/config/urlPart/complete/status/abort）浏览器直传 S3 | inv-1 B；inv-2 §1.H；inv-4 L3 | 高 | 部分（S3 MPU 内部机制已在；只缺 REST 面——**快赢**） |
| 存储后台 job 族 | 用量同步、repo/project summary 计算、stats 落盘、sharding 均衡、禁用 URL 巡检、remote offline 守护、配置持久化 | inv-1 B | 中 | 部分（GC/sweep/迁移 job 已有；其余无） |
| exportds/compress | 数据存储调试导出 + 内部数据压缩 | inv-1 B | 中 | 缺失 |
| In-transit 加密拦截 | 文件存储/复制流量传输层加密拦截器 | inv-1 B | 低 | 缺失（TLS 终结可覆盖大半） |
| 二进制 provider 链 | binarystore.xml chain 模板（默认 file-system；cache/checksum/fs/s3 可组合；具体实现在外部 storage-client 库） | inv-1 B；inv-2 §5；inv-4 L1 | 中 | 部分（filestore+S3 双后端已有；无链式模板配置） |
| 云直连重定向 | 云 provider 支持 redirect 时下载 302 直连对象存储 + 云信息缓存 | inv-4 L2 | 中 | 部分（S3 presigned 重定向已有） |
| Outbox 模式 | 事务 outbox 表（六方言 DDL）可靠事件投递 | inv-4 K3 | 高 | 缺失（事件/审计无 outbox） |

---

## 五、企业集成（标注【外部】= 依赖 Artifactory 之外的 JFrog 产品，单机只能做集成面）

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| Xray 集成【外部：Xray】 | 连接探测（/api/v1/system/version 最低版本）、DB 持久索引队列 hqc_xray_indexes、索引仓库 REST、下载拦截（allowBlockedDownload）、xray_* 属性批量清理、bundle 扫描调度 | inv-1 I；inv-4 B1~B7 | 高 | 缺失（无扫描产品；留事件出口即可对接替代品） |
| Release Bundle v2【外部：Distribution】 | 事务式创建（open/close/async status）、签名分发、`release-bundle@<site>` 本地仓、目标侧 store、V1→V2 适配 | inv-1 I；inv-2 §1.E；inv-4 C1/C2/C8 | 高 | 缺失（产品范围外，记录在案） |
| Air-Gap 离线分发【外部：Distribution】 | bundle 导出/导入（+internal/import） | inv-1 I；inv-4 C3 | 中 | 缺失 |
| legacy 直接分发 + Distribution 规则【外部】 | `/api/distribute` FileSpec+checksums 流式分发（fileTransactionId 续传）；path/layout/property token 规则解析 | inv-4 C5/C6 | 中（端点挂载未见 @Path，低） | 缺失 |
| 目标侧 bundle 保留【外部：Distribution】 | TargetBundleRetention + bundle 联邦事务 | inv-4 C4/C7 | 中 | 缺失 |
| Projects 域【外部：Access（gRPC）】 | 项目/成员/角色/环境委派 Access；仓库-项目对账 job；项目级配额；projectKey 校验 | inv-4 E1~E4；inv-2 §1.H | 高 | 部分（BinFlow 有 Projects 域两层授权语义 rbac-model.md；无项目资源/环境/团队管理与对账 job） |
| Webhook 管理【外部：Access】 | 订阅/过滤/secret 管理在 Access 侧（Artifactory 仅 internal:webhook 权限常量；rest 树无 webhook resource；反编译集合亦无该 addon） | inv-4 I2；inv-3 §3.5 | 中 | 缺失（无 webhook 出站） |
| 统一事件总线 | 36 种事件（before/after × 全生命周期 + alt*）CloudEvents 风格 envelope；consumer 注册 REST；outbound HTTP/Client；jfbus 异步分发 + outbox | inv-1 I；inv-2 §1.H；inv-4 I1/I3/I4 | 高 | 缺失（有审计日志；无出站事件信封/订阅）——**可整体平移，无外部依赖** |
| Curation【外部：Curation 服务】 | 远端包下载前策展拦截（SecurityDecisionMapper、无订阅 403 映射） | inv-2 §1.F；inv-4 O5 | 中 | 缺失 |
| Workers serverless 扩展【外部：Workers 平台】 | TS worker 声明（events-schema.json 40+ 事件类型）+ /api/workers 状态 + SSE 事件流 | inv-1 H；inv-4 I5 | 中 | 缺失 |
| Mission Control / Pipelines【外部】 | 旧 MC 集成 + pipelines 资源 | inv-1 I；inv-2 §1.H | 低 | 不适用 |
| 产品许可/订阅门控 | AddonType 80 项 + SubscriptionType 十档按注解门控 + 集群 license_hash 混合检测 + `artifactory.addons.disabled` | inv-1 I；inv-2 §3；inv-4 J1/J2 | 高 | 不适用（开源无许可面；AddonType 表=官方功能边界权威清单） |
| 制品 license 识别 | licences.xml 内置 91 条正则模式；Maven/npm/nuget/ivy 定位策略；按仓库+状态（approved/unapproved/unknown/notFound/neutral）过滤查询；导入导出与人工增改 | inv-4 J3 | 中 | 缺失（真实可平移，无外部依赖） |
| Evidence（制品证据） | 制品/构建附加合规签名链（服务+拦截器+事件+客户端） | inv-4 O4 | 中 | 缺失 |
| JCR EULA/订阅 + callhome/jfconnect | 容器注册版 EULA 门、匿名上报 | inv-1 I；inv-2 §1.H；inv-4 J4 | 中/低 | 不适用 |

---

## 六、复制与分发

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| Push 复制 V1 | 定时/手动推远端；full/incremental；路径过滤、属性复制、删除同步、事件驱动增量 | inv-4 F1；replication.md | 高 | 部分（单向 push 已有 FR-57~60 多协议覆盖；无删除同步/事件驱动增量/多目标） |
| Pull（remote）复制 | 远程仓定时回源预取；属性与统计同步 | inv-4 F2 | 高 | 部分（pull-through 缓存近似；无定时全量镜像 job） |
| 复制 V2 流式引擎 | traversal 产 filelist → streamer 逐文件流式 + 控制器断点；`/api/replication/replicate/file/streaming/{tx}` | inv-4 F3 | 中 | 缺失 |
| 复制配置 REST | per-repo CRUD + multiple（多目标）+ 全局 enable/disable + execute 手动触发（strategy）+ 状态查询 + channels/establishChannel | inv-1 I；inv-2 §1.E；inv-4 F4 | 高 | 部分（v1/replications GET/POST/DELETE + status 已有；无 multiple/全局开关/execute） |
| Smart Remote Repo 字段面 | enableTokenAuthentication、contentSynchronisation（statistics/properties 联动）、missRetrievalCachePeriod、unusedArtifactsCleanupPeriodHours、checksumPolicy（generate-if-absent）、metadataRetrievalTimeoutSecs=60、socketTimeout=15000 | inv-4 F5（artifactory.xsd 字段及默认值） | 高 | 部分（基本回源缓存已有；高级字段全缺——**低成本对齐项**） |
| 全局复制 | system/replications MULTIPUSH/拉取全局配置 | inv-1 I；inv-2 §1.E | 中 | 缺失 |
| mirror/fullsync | 远端镜像拉齐 + 文件级同步下载 | inv-2 §1.E | 中 | 缺失 |
| Federation 联邦仓 | federated rclass：成员 CRUD/状态（mirrorsLag/unavailableMirrors）、FederatedMirror 事件传播、心跳/全量同步、replaceUrl、binary tasks、grid topology | inv-1 C/I；inv-2 §1.B；inv-4 G1/G2 | 高（资源面）/中（语义） | 缺失（多站点镜像；HA 之后的大项） |
| 联邦/grid 低置信细节 | lead artifact 推举 tie-break、grid snapshot/provision、bundle 联邦事务、联邦 token spec | inv-4 G3；inv-2 §1.B/§6 | 低 | 缺失 |
| Build-info 域（CI 集成，无外部依赖） | BuildRun/Module 数据模型；PUT 全量+append 增量、查询/批删/rename/diff；promotion（copy/move+scope+properties+dryRun）；**Docker promote** `/api/docker/{repo}/v2/promote`；build retention；projectKey 挂域 | inv-2 §1.G（openapi 996 行）；inv-4 D1~D6 | 高 | 缺失（整个 build-info 域——CI 集成半壁） |

---

## 七、REST 面

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| 核心 REST 家族规模 | `/artifactory/api` 103 resource 类 / ≈475 方法级操作 | inv-2 §0 | 高 | 部分（BinFlow ≈50 操作；/binflow 前缀 + errors[] envelope 风格差异，E-26） |
| UI REST 家族 | `/ui/api/v1` 107 类 / ≈470 操作（19 个服务域工厂） | inv-2 §0/§4 | 高 | 部分（控制台走自有 /api/v1 面） |
| AQL | POST aql 查询语言；域：item/statistics/property/build/module/dependency/promotion/releasebundle/sensitive；SQL builder+optimizer；并发上限接 QRL | inv-1 E；inv-2 §1.C；aql.md | 高 | **M15 实现中**（T-407 规格就绪 → T-409~T-415/T-417 实现票在途——**就绪度翻转留痕**：原「缺失（最大查询面差距）」，aql.md 落盘 2026-09-01 起翻转为规格就绪；M15 子集 = item+property 域 + 官方操作符主列） |
| 老搜索族（**14 端点**——T-407 勘误定案：SearchResource 注册 14 子资源为铁证，inv-1 §E 原记 13 漏 license） | artifact/gavc/property/pattern（异步）/usageSince/badChecksum/createdInRange/dependency/buildArtifacts/latestVersion/versions/checksum/**license** 等（官方 reference 另有 archive、latestVersionByProperties 2 枚外挂——全量 16，远期登记） | inv-1 E；inv-2 §1.C；aql.md §8 | 高 | 部分（artifact+checksum 已有 SR-01/02；**M15 收编 gavc/prop/pattern**〔FR-134 断言反转①〕；其余按 E-26 显式 404 归档） |
| UI 搜索增强 | 结果暂存 stash（分页/续查）、packagesSearch 包索引搜索、syntax search、字段助手 | inv-1 E；inv-2 §1.C | 高 | 部分（控制台全局搜索+recentSearches 已有；暂存/包索引搜索缺） |
| 配置描述符原文往返 | GET/POST config.xml 原文 + 子树级替换（rpm/debian/ha）；configdescriptor/securitydescriptor | inv-1 D；inv-2 §1.H | 高 | 部分（自有 YAML/env 配置 + 导出导入；无描述符原文往返） |
| 运行时 KV 配置存储 | configsService 命名配置 KV（url signing key、checksumReplication 等） | inv-1 D | 中 | 缺失（可并入自有 config） |
| 制品属性 REST（读写） | GET `?properties=K1,K2*`、PUT `?properties=k=v`（recursive/atomic）、矩阵参数写、旧 `:properties` 409、UI tabs 面 | rest-api.md §3（官方文档双证）；storage.go E-09 | 高 | **缺失**（?properties/?propertiesXml/?stats/?lastModified 显式 404；无写面；仅 ?permissions 有效权限臂已有） |

---

## 八、控制台特性

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| 双模式壳/IA | 应用/管理模式分组 + 模式切换 + Quick 动作 | inv-2 §1.K；console-ui.md | 高 | 已有（M8 T-235，12 条目五分组） |
| 树浏览器 | treebrowser/V2（分页版）/nativeBrowser；懒展开、深链、过滤 | inv-1 F；inv-2 §1.K | 高 | 已有（T-236 跨仓树+深链+过滤+右键；V2 分页面无） |
| 详情 Tab 族 | generalinfo/checksums/properties tabs/licenses/views（alpine/nuget/swift）/ViewSource/archiveViewSource | inv-1 F；inv-2 §1.K | 高 | 部分（Tab+校验和徽标+docker tag 已有；属性 Tab 受属性系统缺失制、licenses/views 缺） |
| 制品操作菜单 | artifactactions 19 操作（复制/移动/删除/watch/删除旧版本/zap） | inv-2 §1.K | 高 | 部分（删除/下载/重命名路径已有；复制/移动/watch/删旧版本缺） |
| SetMeUp 对话框 | 包类型网格/协议 Tab/一次性 Token/凭据生成 | inv-2 §1.K | 高 | 已有（T-242，五包型 + step-up 融合） |
| Deploy 对话框 | UI 上传（拖拽、explode 选项、目标路径计算） | inv-1 H；inv-2 §1.K | 中 | 部分（拖拽+流式 sha256 已有；explode 400 拒绝） |
| Watch UI | 关注管理 + 通知状态 | inv-1 F；inv-2 §1.K | 中 | 缺失 |
| 首页 widget/仪表盘 | home/widget 5 操作 + 平台公告 | inv-2 §1.K | 高 | 部分（仪表盘快捷卡 T-239 已有；widget API/系统公告缺） |
| 全局搜索 | artifactsearch + packagesSearch + 暂存 | inv-2 §1.K | 高 | 已有（T-239 recentSearches+深链+固定类型） |
| packages/versions 原生浏览 | v1/native/{packages,versions,repos}（新 UI 数据面） | inv-1 F；inv-2 §1.K | 中 | 部分（自有列表页；无原生包/版本 API） |
| 登录屏/auth screen | auth/screen + SSO 入口 | inv-2 §1.K | 中 | 已有（M8 登录 404 对齐 + OIDC 入口） |
| 依赖声明生成 tab | 按 repo+包型生成 gradle/maven/ivy/sbt 片段 | inv-1 F | 中 | 缺失（SetMeUp 部分覆盖同一需求） |
| MFE 微前端架构 | 内/外部 MFE 文件服务 + 版本刷新 job（core/packages/buildinfo 三 MFE） | inv-1 I；inv-2 §0 | 高 | 不适用→缺失（单体 SPA；对标不需要） |

> 修正一处分区盘点偏差：inv-1 将「路径有效权限」标缺失——实际 BinFlow 已有 `GET /api/storage/{repo}/{path}?permissions`（SE-08 handleStoragePermissions），归入已有；本表将其并入详情 Tab 族与 REST 属性行说明。

## 九、部署形态

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| HA 集群 | DB 表 artifactory_servers 心跳注册（5s/30s stale/180min 清理）+ primary 推举（TASK_AFFINITY/MEMBER）+ HTTP 逐节点传播（scope=ha-propagate token）+ 原生 DB 锁（仅 PG 激活）+ 12 种可传播 job 上下文 + ha-admin/clusterDump | inv-1 D；inv-2 §1.H；inv-4 A1~A10 | 高 | 缺失（单节点设计；PG 心跳表路线可低成本起步——**7.x 已去 Hazelcast 化，无内存网格依赖**） |
| 单机内嵌 Derby 默认 | 无外部 DB 时 Derby（AQL 等降级） | inv-4 K2 | 高 | 不适用（PG 单库策略；可选 SQLite 形态另议） |
| 部署矩阵 | compose / k8s 清单 / Helm / systemd / 离线安装包 | inv-2 §5（间接）；BinFlow M5 | 高 | 已有（M5 FR-34~40 全套 + M9 多架构镜像） |
| 优雅停机/pre-stop | v1/system/preStop 钩子 + graceful shutdown 状态 | inv-1 D | 低 | 部分（graceful shutdown+drain 已有实测；无 preStop REST） |
| 灰度双跑/edition | shadow 双实例对比；editions（oss/pro/ha/jcr）分版 | inv-1 B；inv-2 §3 | 低/高 | 缺失/不适用（单体单版） |

## 十、其他

| 功能 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| Groovy 用户插件 | etc/plugins/*.groovy 动态装载；execution（REST 触发 params/async）、download/upload 改写、user realm 自定义认证、storage 事件钩子、staging 策略、热重载 | inv-2 §1.I；inv-4 H1~H4 | 高 | 缺失（无用户脚本扩展机制；Go 生态等价可议 plugin 进程/WebAssembly） |
| Filtered Resources | 文本模板 @@token@@ 占位符替换 + 安全限制 | inv-4 P3 | 中 | 缺失 |
| Package re-route | 包管理器请求按配置改路由（registry 级 reroute） | inv-4 P4 | 低 | 缺失 |
| JFrog CLI 对位 | JFrog CLI 直连 Artifactory 命令面 | （生态事实） | 高 | 部分（自有 bf CLI + 迁移工具；不兼容 jf 命令面） |
| 内嵌迁移作业 REST | com.jfrog.ph.migrator /v1/migrations 13 操作（source→Artifactory 作业面） | inv-2 §1.J | 中 | 部分（bf-migrate 为 CLI 侧；无 REST 作业面） |
| Addon 装配框架 | META-INF/addon.{xml,properties} = Spring bean 集 + license 档装配单元（oci/rpm 例证） | inv-2 §3/§4 | 高 | 不适用（开源单体；功能开关走自有 config） |

---

## 十大高价值缺口（按用户价值排序，M10+ 候选主轴）

1. **制品属性系统**（矩阵参数部署 + `?properties` 读写 + 属性集）：所有 Artifactory 客户端的横切基座，且是搜索/清理策略/复制属性同步的基石——BinFlow 当前 ?properties 显式 404。
2. **查询面**：AQL + **14** 个老搜索端点（企业日常操作入口；可先做 artifact/gavc/pattern/usageSince 子集 + 简化查询语言）。（T-407 勘误：原记「13 个」沿 inv-1 §E 漏 license——SearchResource 14 子资源为铁证；M15 = AQL item+property + gavc/prop/pattern 首批，详见 docs/reverse/aql.md）
3. **Trash can 回收站**（14 天保留/恢复/清空/目录属性）：删除即永久是数据安全事故源；语义独立、实现面窄。
4. **Cleanup/Retention 策略引擎**（包/构建/bundle 三类策略 + cron run + dry-run view + 报告）：BinFlow 仅 GC 管孤儿块，无内容级治理。
5. **制品操作族**：路径级 copy/move/flat、zap、目录 zip 下载（folderDownload）、归档内浏览/抽取（archive!/）——日常运维高频操作。
6. **Webhook/事件出站总线**（36 事件 CloudEvents envelope + HTTP outbound + 订阅管理）：CI/CD 与下游集成刚需；inv-4 判定可整体平移、无需模拟 Access 拆分。
7. **包型第一梯队 9 种**：NuGet（v2+v3+symbol）、Conan（v1+v2 修订链）、Cargo（sparse 索引+yank）、Go（含 sumdb 代理）、Debian（By-Hash）、RPM/YUM、Helm（经典+OCI）、Terraform（Registry+Backend state 锁）、GitLFS（含锁协议）。
8. **运维纵深**：Support Bundle（7 收集器）、Live Logs、运行时日志级别、请求/查询限流、流量记账 DB 账本、入站请求守卫。
9. **Build-info 域**：PUT/append/查询/promotion（含 `/api/docker/{repo}/v2/promote`）/retention——Artifactory CI 集成的半壁，BinFlow 已有 Jenkins 场景可接。
10. **快赢对齐包**：MPU REST 化（S3 机制已在）、smart remote 字段面（enableTokenAuthentication/contentSynchronisation/unusedArtifactsCleanupPeriodHours）、versions API、按包型强制认证开关、代理服务器管理。

## 依赖外部产品的项（单机重实现只能做集成面）

Xray（索引队列/下载拦截）、Distribution 服务（Release Bundle v2/Air-Gap/目标保留）、Access（Projects gRPC、Webhook 管理面、ha-propagate token）、Curation、Workers 平台、JFrog Connect/Mission Control/Pipelines。合计 15 条中 9 条标注【外部】；其余（统一事件总线、制品 license 识别、Evidence、Build-info）无外部依赖可直接平移。

## 与公开规范的关系

Docker Registry v2 / Maven 2 / npm / PyPI / Debian apt / Helm / Terraform Registry 等有官方规范的以规范为准；反编译补充项（矩阵参数 400 语义、trash 属性集、MPU REST 端点集、HA 传播 token scope、Xray 队列表名、outbox DDL、licences.xml 91 模式、Load Healer 阈值表）在各分区文档标注「此条补充官方规范」。

## 待验证清单（低置信度汇总，动态验证后回填）

1. Load Healer 活动集语义与排空行为；入站请求守卫触发条件（openapi 在根目录，未读实现）。
2. DevOps Agent request log 采样/保留；ErrorCatalog 写入方。
3. HQC（heavy query cache）与 fullsync 关系；启用条件。
4. IntransitInterceptor 拦截面；shadow 服务启用开关与指标出口。
5. jfbus/UemV2 outbox 分区参数与背压；localgenerated 过滤器调用方。
6. Grid snapshot/provision 与 Jem 语义；handler shadowing 切换条件；com.jfrog.ph 实例级挂载前缀。
7. FROGML/JEM 处理器位置（仅枚举）；Gradle/Ivy/SBT 是否有隐藏专属端点；Cargo git 索引存活。
8. Webhook addon（反编译集合缺失，需补材料或以官方文档为唯一来源）；legacy /api/distribute 的 @Path 挂载点。
9. 联邦 lead artifact 推举 tie-break；retentionTools 口径；virtualCacheCleanupConfig cron 与注释不符（00:12 vs 05:00）。
10. multitenantinfra 是否随自托管发行；binary provider 具体 chain 组合（以外部 storage-client 库/binarystore.xml 公开文档为准）；packagereroute 触发语义；BinFlow `+` 解码与 ETag 弱标签现状。
