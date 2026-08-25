# inv-3 协议与包型全量清单（Artifactory 7.x 反编译盘点，分区 3）

- 材料：`reverse-src/artifactory/src/batch2-protocol`（全部）+ `batch1-core` 协议相关 + `batch3-addons` 包型 addon。
- 本文件是**功能盘点清单**，不是逐协议实现规格；已详述的协议见 `docker-registry.md` / `maven-npm-pypi.md`。
- 置信度：高 = 反编译代码 + 公开规范/文档双重印证；中 = 仅代码可见；低 = 推断或证据集合缺失。
- BinFlow 覆盖判定基准：`internal/adapter/{generic,docker,maven,npm,pypi}` 五包型 + local/remote/virtual + pull-through。

## 1. 包型总数与证据锚点

- 枚举证据：`org.artifactory.common.repo.PackageType`（batch1-core）共 **57 个枚举值**。
- 行为证据锚点：
  - `com/jfrog/ph/<type>/**`（batch2-protocol，"package handler" 框架：resource/repository/indexer/command/spring 每包型一组）——34 个目录。
  - `org/jfrog/repomd/<type>/**`（仓库元数据计算：模型+REST）——20 个目录。
  - `org/artifactory/addon/<type>/**`（batch3-addons 部分包型 + batch2-protocol 的 docker/oci/nuget/nugetv3/conan/helmoci）。
  - 独立 HttpRepo：`PypiHttpRepo`（batch1-core）、`DockerHttpRepo`、`NuGetHttpRepo`、`SwiftHttpRepo`、`TerraformHttpRepo`、`NimModelHttpRepo`、`AnsibleHttpRepo`。
  - 每包型可配字段：`PackageTypeConfigFields`（78 个配置键，含默认值）。
  - 默认 layout：`batch1-core/META-INF/default/artifactory.config.xml` `<repoLayouts>`（25 条内置 layout）。
- 分类计数：客户端可交互协议型 ≈ 45；Maven 家族 layout 型 3（gradle/ivy/sbt 无独立 HTTP 面）；内部/系统型 4（BUILDINFO/RELEASEBUNDLES/PIPEINFO/SUPPORT）；AI/编辑器生态型 13。

## 2. 包型逐项清单

格式：**包型 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖**。

### 2.1 JVM / 构建系

| 包型 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| MAVEN | maven-2-default layout（descriptor 单列 .pom）；suppressPomConsistencyChecks、fetchJarsEagerly/fetchSourcesEagerly、rejectInvalidJars、pomRepositoryReferencesCleanupPolicy（默认 discard_active_reference）、forceMavenAuthentication | artifactory.config.xml + PackageTypeConfigFields | 高 | **已有** |
| GRADLE | 无独立 HTTP 面，复用 maven/ivy layout 的 PUT/GET 布署；枚举存在仅为 UI/语义区分 | PackageType 枚举，无 ph/addon 目录 | 中 | 部分（走 maven 通道） |
| IVY | ivy-default layout（[org]/[module]/[baseRev]/[type]s/…，14 位时间戳 integration 版本） | artifactory.config.xml | 高 | 缺失（layout 级） |
| SBT | sbt-default + sbt-ivy-default layout（scala_/sbt_ 路径段） | artifactory.config.xml | 高 | 缺失（layout 级） |

### 2.2 通用 / 容器 / OCI

| 包型 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| GENERIC | simple-default layout（[orgPath]/[module]/[module]-[baseRev].[ext]）；矩阵参数属性化部署 | artifactory.config.xml | 高 | **已有** |
| DOCKER | Registry v2 全套（blobs/uploads PATCH 断点、manifest schema1/2、_catalog、tag 列表）；**v1 API 常量仍在**（X-Docker-Token/X-Docker-Endpoints/X-Docker-Checksum-Payload、images/{imageId}/json ancestry）；blockPushingSchema1 默认 true；maxUniqueTags/dockerTagRetention；resolveDockerTagsByTimestamp；cachingLocalForeignLayers（foreign layer 缓存）；dockerApiVersion 默认 V2；DSSE evidence 处理签名（addon/docker/evidence/DsseEvidenceHandler） | repomd/docker/v2/rest/** + PackageTypeConfigFields | 高 | **已有**（v2 面；v1 残留、tag retention、DSSE 未见） |
| OCI | 独立 OCI 包型（ORAS 类工件，非 docker 语义）：addon/oci/rest/OciResource + OciV2AuthenticationFilter | batch2-protocol addon/oci | 高 | 缺失 |
| HELM | 经典 chart 仓：index.yaml 维护（增删 chart 自动重算）、chartsBaseUrl；_external/_transitive 依赖代理（含 .prov 签名文件模式） | ph/helm/resource/HelmResource.java | 高 | 缺失 |
| HELMOCI | Helm over OCI（chart 以 OCI artifact 推拉），addon 壳复用 OCI 栈 | addon/helmoci/HelmOciAddonImpl.java | 中 | 缺失 |

### 2.3 语言生态管理器

| 包型 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| NPM | npm-default layout（[orgPath]/-/[module]-[baseRev].tgz）；registry 协议（-/all、dist-tags、auth/token 命令端点、scope 包） | ph/npm/**（NpmResource/NpmAuthCommandResource/NpmTokenCommandResource） | 高 | **已有** |
| PYPI | simple 索引 + 上传表单（PypiFormRequest）；**请求路径层接受 `list/<repo>/` 与 `simple/<repo>/` 前缀并剥离**；repositorySuffix 默认 "simple"；pyPIRegistryUrl 默认 pypi.org | ph/pypi + PypiHttpRepo(batch1-core) + ArtifactoryRequestBase.calculateRepoPath | 高 | **已有** |
| NUGET | v2 OData（Search()/FindPackagesById()/Packages()/GetUpdates()/$count，内嵌 v2 service/metadata document）；v3（v3-flatcontainer、SearchQueryService、v3-index/repository-signatures 签名索引）；symbol server（{symbolFile}.pdb/{GUID}/{symbolFileRepeat}.pdb，默认指向 symbols.nuget.org）；feedContextPath 默认 api/v2；enableNormalizedVersion；forceNugetAuthentication | ph/nuget（NugetResource + v2/ v3/）+ batch2-protocol 根目录 v2/ v3/ 静态文档 + addon/nuget{,v3} | 高 | 缺失 |
| CONAN | v1（v1/ping、users/authenticate、users/check_credentials、v1/conans、secret/key）+ **v2 全修订链**（v2/conans/…/revisions/{rRev}/packages/{packageId}/revisions/{pRev}/files）；forceConanAuthentication；内置 ConanV2 迁移 job（v1→v2 元数据重算） | ph/conan + addon/conan/**（interceptor/indexer/migration） | 高 | 缺失 |
| CARGO | api/v1/crates（publish(new)、yank/unyank、download、search）；**内部 sparse 索引**（index/config.json、.cargo 目录、原始 config.original.json 保留）；public_key 端点返回索引签名公钥；cargoAnonymousAccess；cargoInternalIndex（git 索引兼容） | ph/cargo + repomd/cargo/utils/CargoConstants.java | 高 | 缺失 |
| GO | GOPROXY 协议（@v/list、@v/{ver}.info/.mod/.zip、@latest）；**sumdb 代理**（sumdb/sum.golang.org/lookup|tile|supported）；虚拟仓 external dependencies 重定向（GoExternalDependenciesHelper，模式列表 + 独立远端仓） | ph/go/resource/GoResource.java | 高 | 缺失 |
| GEMS | rubygems 协议（gem push 为 multipart POST api/v1/gems；quick/Marshal 索引）；带 ruby 辅助脚本 gems_helper.rb 与 staticgems 引导集 | batch3-addons addon/gems/** + ruby/gems_helper.rb + staticgems | 高 | 缺失 |
| COMPOSER | packagist 元数据（packages.json、p/{ns}/{pkg}.json、p2/、registration{,-semver2}/ 逐版本元数据）；enableComposerV1Indexing；composerRegistryUrl 默认 packagist.org | ph/composer + repomd/composer | 高 | 缺失 |
| SWIFT | /api/package 面（{scope}/{name}/{ver}.zip/.json/Package.swift、@{scope}{slash}{name} 转义路径、_external 依赖代理）；swift-default layout [scope]/[name]/[name]-[version].zip | ph/swift + SwiftHttpRepo(batch3) + repomd/swift | 高 | 缺失 |
| PUB | /api/packages/{name}、versions、tarballs/；dist-tags（-/package/{name:.+}/dist-tags/{tag}）；PubSpec 索引合并（merger/PubPackageMetadataIndexer） | ph/pub + repomd/pub | 高 | 缺失 |
| CONDA | conda 通道（repodata.json / .bz2 / .zst / current_repodata 四形态正则识别）；**curated repodata 过滤**（按允许 build 列表重写 current_repodata.json）；通道认证 | ph/conda + repomd 无（自有 curation） | 高 | 缺失 |
| CRAN | CRAN 源布局（src/contrib）+ simple 索引 + 版本下载；CranSpecification 解析 | ph/cran | 中 | 缺失 |
| COCOAPODS | CDN specs 模式（index/{pkg} 哈希分片、Specs/…podspec.json、registration CDN 端点）；/pod/git、/pod/external、/pod/pkg 代理；cocoaPodsSpecsRepoUrl 默认 GitHub Specs、cocoaPodsCdnUrl 默认 cdn.cocoapods.org | ph/cocoapods + PackageTypeConfigFields | 高 | 缺失 |
| BOWER | bower-default layout；registry 元数据（packages.json、packages/{path}）；bowerRegistryUrl 默认 registry.bower.io | repomd/bower（16 文件）+ PackageTypeConfigFields | 高 | 缺失 |
| HEX | mix registry 协议（packages/{name}、names、api/packages/versions/new）；**内置 hex 公钥默认值**（hexPublicKey，repo.hex.pm 签名验证） | ph/hex + PackageTypeConfigFields（含完整 PEM 默认值） | 高 | 缺失 |
| LUAROCKS | luarocks 服务器协议（api/1/{key}/upload、upload_rock/{versionId}、check_rockspec、status） | ph/luarocks | 中 | 缺失 |
| NIMMODEL | Nim 包模型（NimModelHttpRepo + 专用 API 面） | addon/nimmodel + ph/nimmodel | 中 | 缺失 |
| BAZELMODULES | Bazel Central Registry 风格服务（modules/{repoKey}__{namespace} 元数据） | ph/bazelmodules | 中 | 缺失 |
| NIX | 二进制缓存协议（nix-cache-info、{hash}.narinfo、nar/{hash}.nar.{xz,zst}）；nix-default layout 以 narinfoHash 为路径 | ph/nix + artifactory.config.xml nix-default | 高 | 缺失 |
| VCS | GitHub/GitLab 原始码代理：vcs-dists/direct-dists/source-dists 三类 tarball 端点、refs/{org}/{repo}、downloadTag/downloadBranch、**api/v1/roles/downloads + getToken 下载令牌**、external+transitive 通配代理；vcsGitProvider 默认 GITHUB；gitLabResolveSubgroups | ph/vcs + repomd 无 + PackageTypeConfigFields | 高 | 缺失 |
| GITLFS | LFS batch API（objects/batch）+ 单对象 GET/PUT + verify + **锁协议全套**（locks 增删查、locks/verify、{id}/unlock、actions/unlock|lock|force-unlock） | ph/gitlfs/resource/GitLfsResource.java | 高 | 缺失 |

### 2.4 OS 包管理

| 包型 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| DEBIAN | apt 布局 dists/{dist}/{comp}/…；索引重算（Packages 及压缩形态）；**By-Hash 支持（ALL/SHA256/NONE 三档枚举）**；debianTrivialLayout；optionalIndexCompressionFormats 默认 ["bz2"]；ddebSupported；debianDefaultArchitectures 默认 i386,amd64 | repomd/debian（38 文件，含 util/ByHashSupport.java） | 高 | 缺失 |
| YUM (RPM) | repomd 元数据计算（calculateYumMetadata、yumRootDepth、yumGroupFilenames、enableFileListsIndexing filelists）；/rpm reindex 端点 | ph/rpm（RpmResource reindex）+ repomd/rpm + PackageTypeConfigFields | 高 | 缺失 |
| ALPINE | APKINDEX.tar.gz 索引 + **RSA 签名索引**（.SIGN.RSA. 与 .rsa.pub 公钥文件模式、APKINDEX.unsigned.tar.gz 中间产物） | ph/alpine | 高 | 缺失 |
| OPKG | Packages 索引型轻量协议 | ph/opkg + repomd/opkg | 中 | 缺失 |

### 2.5 基础设施 / 配置管理

| 包型 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| TERRAFORM | Registry 协议：服务发现（terraform.json well-known）、v1/modules（search、{ns}/{name}/{provider}/versions、download）、v1/providers（含 transitive/external 下载代理）；terraformType 区分 MODULE/PROVIDER 双 layout；terraformRegistryUrl 默认 registry.terraform.io、providers 默认 releases.hashicorp.com | ph/terraform/registry + repomd/terraform（TerraformWellKnown/TerraformLoginV1）+ TerraformHttpRepo(batch3) | 高 | 缺失 |
| TERRAFORMBACKEND | HTTP state 后端协议：v2/state-versions（+current）、workspaces 列表/详情、current-state-version、{stateId}/state-download；**state 锁**（actions/lock|unlock|force-unlock + locks/verify）；auth/v3/keys/get-caller-info（JWT 身份） | ph/terraform/backend | 高 | 缺失 |
| ANSIBLE | galaxy v3 API（api/v3/collections 全套 + artifacts、api/v3/imports、published index docs-blob）；v1 cookbooks 兼容面；ansible-default layout collections/{ns}/{name}/{ver}/… | ph/ansible + AnsibleHttpRepo(batch1-core) + artifactory.config.xml | 高 | 缺失 |
| PUPPET | forge 协议 v3（v3/releases、v3/modules）+ v1；puppet-default layout [orgPath]/[module]/[orgPath]-[module]-[baseRev].tar.gz | repomd/puppet（34 文件）+ @Path("v3/releases") | 高 | 缺失 |
| CHEF | supermarket API（api/v1/cookbooks 及版本下载、api/v1/search、universe 端点） | repomd/chef（24 文件）+ @Path("api/v1/cookbooks") | 高 | 缺失 |
| VAGRANT | atlas/box API（{boxName} 查询与 box 文件下载） | ph/vagrant | 中 | 缺失 |
| P2 | Eclipse p2 composite 仓（p2/{ns}/{pkg}.json、artifacts/content 元数据、$metadata/$batch OData 端点）；forceP2Authentication；p2Urls/p2OriginalUrl | batch2-protocol ODataBatchProvider + @Path("p2/…") + PackageTypeConfigFields | 中 | 缺失 |

### 2.6 AI / ML / 编辑器生态（7.x 后期增量）

| 包型 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| HUGGINGFACEML | HF 协议克隆：api/models/{name}/preupload|commit|branch（可断点提交）、{org}/{model}/resolve/{rev}/{file}、model-manifest、**xet CAS 子协议**（xet-read/write-token、xorbs、chunks/shards） | ph/huggingface（authentication/index/layout 全） | 高 | 缺失 |
| MACHINELEARNING | 通用 ML 仓（models/datasets 双实体、api/models/datasets preupload/commit/branch/complete_multipart、tree/paths-info）；isMlRepoLayout 配置 | ph/machinelearning（MLResource/MLSubResource） | 中 | 缺失 |
| FROGML | JFrog 自家 ML 工件类型；**未见独立 ph 目录**，推断复用 machinelearning 栈 | 仅 PackageType 枚举 | 低 | 缺失 |
| VSCODE / VSCODEEXTENSIONS | VS Code marketplace 协议（_apis/public/gallery、extensionquery、vscode 变体 external patterns 默认 **/*vsassets.io/**） | @Path("_apis/public/gallery…") + PackageTypeConfigFields | 中 | 缺失 |
| JETBRAINS / JETBRAINSPLUGINS | JetBrains 插件市场协议（files/{pluginId}/meta.json、brokenPlugins.json、api/products/intellij、pluginManager、compatibleUpdates 搜索） | ph/jetbrains | 中 | 缺失 |
| AIEDITOREXTENSIONS | AI 编辑器扩展市场（_apis/public/gallery/vscode 变体） | ph/aieditorextensions | 中 | 缺失 |
| SKILLS | agent skills API（api/v1/skills/{slug}/versions/file） | ph/skills | 中 | 缺失 |
| AGENTPLUGINS / AGENTPACKAGES | agent 插件/包分发（agent-plugins-default / agent-packages-default layout，文件型 API） | ph/agentplugins + ph/agentpackages + artifactory.config.xml | 中 | 缺失 |
| JEM | 内部新类型，未见独立处理器目录 | 仅 PackageType 枚举 | 低 | 缺失 |

### 2.7 内部 / 系统型（非对外协议）

| 包型 | 行为要点 | 证据 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|---|
| BUILDINFO | build-info 仓（CI 构建元数据存储，供 BI API 读写） | 仅枚举 + batch1-core build 相关服务 | 中 | 缺失（整条 BI 链路） |
| RELEASEBUNDLES | Distribution 发布包内部类型 | 仅枚举 | 低 | 缺失（Distribution 域） |
| PIPEINFO | Pipelines 集成内部类型 | 仅枚举 | 低 | 缺失 |
| SUPPORT | 支持包（support bundle）内部存储类型 | 仅枚举 + addon/support | 中 | 缺失（BinFlow 有备份，无 support bundle 类型） |

## 3. 包型之外的协议传输面（横切）

### 3.1 请求路径归一化中枢（ArtifactoryRequestBase.calculateRepoPath，batch1-core）

| 能力 | 行为要点 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|
| 矩阵参数 | 路径（含 repoKey 段）中 `;k=v;k2=v2` 整体剥离并转为部署属性（properties），随 PUT 存储为 item 属性；属性键校验失败返回 400 | 高 | **缺失**（layout.go 明示 M1 语义无矩阵参数） |
| PyPI 前缀 | 请求路径以 `list/` 或 `simple/` 开头时跳过该前缀再取 repoKey（pip 兼容） | 高 | 部分（pypi adapter 自有处理，非中枢） |
| 归档内路径 | `<name>.<archiveExt>!/inner/path` 形式触发按需解包成员读取；strictArchiveDotSlash 开关控制 `./` 分割严格模式；归档扩展名集合来自 mimeTypes 配置 | 高 | 缺失 |
| dot-segment 归一化 | `/./` 与结尾 `/.` 折叠；矩阵参数段外的路径先归一化再拼回 | 高 | 部分 |
| `+` 解码 | 路径 URL 解码前把 `+` 换成 `%2B` 再还原（防 query 语义污染路径） | 高 | 未知（待对查） |
| repoKey 别名 | substituteRepoKeys 映射表在路径层做 repoKey 替换 | 中 | 缺失 |

### 3.2 上传/部署特性（UploadServiceUtils + mpu，batch1-core）

| 能力 | 行为要点 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|
| Checksum 部署 | `X-Checksum-Deploy: true` 时零 body 部署：按 X-Checksum-Sha1（或 Sha256）在库内寻找已有二进制并建立引用；filestore 流可用时附加 sha256 属性；找不到则失败 | 高 | 部分（maven+generic 已有，错误语义为 maven 特例文案） |
| Filestore 存在性检查 | `X-Check-Binary-Existence-In-Filestore` 布尔头：仅查 filestore 是否已有该 checksum（不建 repo 条目） | 高 | 缺失 |
| 客户端断点续传（MPU） | MultipartUploadService：generateToken(repoKey,path,partSizeMB) → 分片 URL（urlPart(n)）→ status()/abort()/complete(sha1)；完成响应头 `X-Checksum-Deploy-Token`；scope 常量 internal:mpu:x | 高 | 部分（storage.Session 面向 S3/docker blob；无客户端 REST 面） |
| 属性部署校验 | 矩阵参数属性键经 PropertyNameValidator，非法键 400（RepoRejectException） | 高 | 缺失（无矩阵参数） |

### 3.3 下载/缓存特性

| 能力 | 行为要点 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|
| 条件请求 | ETag/If-None-Match（含 W/ 弱标签剥离与引号剥离）；If-Modified-Since 与 Last-Modified 并存时取 max 为 modificationTime | 高 | 部分（ETag=sha1 未加引号已有；弱标签语义待查） |
| Range | Range 头存在即 isRange，byte range 切片响应 | 高 | 已有（generic 206） |
| 目录下载 | folderDownloadConfig：整目录打 zip，默认关；限额 1024MB / 5000 文件 / 10 并发 / 匿名默认关 / 空目录默认关 | 高（config.xml 默认值） | 缺失 |
| 下载重定向 | 远端仓可选 downloadRedirect（>阈值重定向源站 URL）与 CDN 重定向（CdnRedirectRepoConfig） | 中 | 缺失 |
| 递归环防护 | `X-Artifactory-Originated`/`Origin-Artifactory` 头识别来自另一 Artifactory 的请求；多 origin 且含本机 hostId 判定递归 | 中 | 缺失 |

### 3.4 认证形态（协议层）

| 能力 | 行为要点 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|
| Docker bearer challenge | 401 携带 `WWW-Authenticate: Bearer realm="<url>",service="<svc>"(,scope="repository:<repo>:pull[,push]")` + `Docker-Distribution-Api-Version: registry/2.0`；token URL 模板 `{repoKey}/{v1|v2}/token`（按仓版本） | 高 | 部分（BinFlow 平铺 /v2/token；scope 语义已有） |
| 按型强制认证开关 | forceMavenAuthentication / forceConanAuthentication / forceNugetAuthentication / forceP2Authentication / cargoAnonymousAccess（反向匿名放行）/ hex 公钥校验 | 高 | 缺失（BinFlow 匿名策略全局，非按包型） |
| 外部依赖重定向 | externalDependenciesEnabled + externalDependenciesPatterns + externalDependenciesRemoteRepoRef：虚拟仓对匹配模式的请求重定向到指定远端仓（Go/VCS/npm/bower/helm/swift/p2/vscode 各自实现） | 高 | 缺失（BinFlow pull-through 不支持 pattern 分流） |

### 3.5 事件面

| 能力 | 行为要点 | 置信度 | BinFlow 覆盖 |
|---|---|---|---|
| Webhook | **未能定位**：反编译三 batch 无 org/artifactory/addon/webhook 模块（该 addon jar 未包含在集合内）；公开文档定义了 artifact/docker/property/release-bundle/retention 等事件族 | 低（代码侧）/文档侧存在 | 缺失 |
| Watch（邮件通知） | addon/watch：watch 标记 + events.properties 邮件模板（HTML 汇总 watched item 事件） | 中 | 缺失 |
| jfevent | 内部事件总线消费面：artifact(ArtifactEvent/ArtifactPropertyEvent)、docker(DockerEvent)、retention、release.bundle 事件模型 | 中 | 缺失 |
| X-Artifactory-* 头族 | 29 个内部头（Created/Modified-By/Created-By/Trace-Id/HA-Originated*/Project-Key/Package-Url/Override-Base-Url/Pass-Through/If-None-Match-MD5 等），用于复制、HA、追踪、云直传 | 高（grep 全集） | 极少部分（无对应头族） |

## 4. 内置 layout 全表（artifactory.config.xml 快照）

maven-2-default、ivy-default、maven-1-default、nuget-default、npm-default、bower-default、vcs-default（refs<tags|branches> 段）、sbt-default、simple-default、cargo-default、composer-default、conan-default（含 package_id 段）、puppet-default、go-default（@v 段）、build-default、terraform-module-default、terraform-provider-default（os/arch 段）、swift-default、ansible-default、sbt-ivy-default、nix-default、skills-default、agent-plugins-default、agent-packages-default、luarocks-default —— 共 25 条。token 语义（[orgPath]/[module]/[baseRev]/[folderItegRev]/[fileItegRev]/[classifier]/[ext] + 自定义正则段）与 BinFlow adapter/layout.go 的五包型声明等价但表达力更宽（含条件段与自定义捕获组）。

## 5. BinFlow 缺口汇总（按优先级）

1. **横切断层（影响所有客户端）**：矩阵参数属性部署、归档内路径（archive!/）、目录 zip 下载、客户端断点续传 REST 面、X-Check-Binary-Existence-In-Filestore。
2. **高使用率包型缺口**：NuGet（含 v2+v3+symbol server）、Conan（v1+v2）、Cargo、Go（含 sumdb 代理）、Debian（含 By-Hash）、RPM/YUM、Helm（经典+OCI）、Terraform（Registry+Backend state 锁）、GitLFS、Gems。
3. **中使用率包型缺口**：VCS、Composer、Conda（curated repodata）、Alpine（RSA 签名索引）、Swift、Pub、CocoaPods、Bower、Hex、OCI、P2、Chef/Puppet/Ansible/Vagrant/Opkg/CRAN/LuaRocks。
4. **生态增量缺口（可整体缓议）**：AI/ML 群（HuggingFaceML/MachineLearning/FrogML/Skills/AgentPlugins/AgentPackages/VSCode/JetBrains/AIEditorExtensions/JEM/NimModel）、Distribution/BuildInfo 链（RELEASEBUNDLES/BUILDINFO/PIPEINFO/SUPPORT）。
5. **认证/分流缺口**：按包型强制认证开关、external dependencies 模式分流、per-repo docker token 路径形态。
6. **事件缺口**：webhook addon 未在集合内（需补材料或以官方文档立项）、watch 邮件、jfevent。

## 6. 待验证清单（低置信度）

- FROGML / JEM 的实际处理器位置（枚举存在但无 ph 目录）。
- Webhook addon 行为（集合缺失，需补 reverse-src 或以官方文档为唯一来源）。
- GRADLE/IVY/SBT 是否有隐藏的专属端点（当前判断：纯 layout 型）。
- Cargo internal git index（cargoGitIndexEnabled）在当前版本是否仍暴露 git 协议面。
- `+` 号解码语义在 BinFlow 的现状（未对查）。
