# Artifactory OSS 工程结构参考规格

> T-49 产出。与 docs/reverse/ 既有五份**行为规格**不同，本文是**工程结构规格**：回答「Artifactory 官方 OSS 仓库如何划分模块、模块间如何依赖、扩展点在哪」，供 BinFlow 的包结构演进与 M3 拆票参考。
>
> 材料：`/Users/lzw/Downloads/jfrog-artifactory-7.161.16`（官方 OSS 源码，7.161.16，agent 只读）。
> 交叉源：`reverse-src/artifactory/src/`（pro 反编译，batch1-core / batch2-protocol / batch3-addons）。
> clean-room 边界（ADR-0001 延伸至 OSS 源码）：仅引用模块名、类名、包路径与装配关系；不含任何代码翻译。

**参考强度标注**（对应行为规格的置信度体系）：
- `[OSS]` = OSS 源码直接可见
- `[pro]` = 仅 pro 反编译佐证
- `[双源]` = OSS + pro 反编译相互印证

---

## 1. 材料口径

| 项 | 数值 |
|---|---|
| pom.xml 总数 | 51（含根聚合 pom） |
| 纯聚合/parent pom | 14（根 + base / backend / bridge / build-handler / storage / storage/binaries / storage/binaries/bridges / support / web / event-api-parent / index-event-api / stats-event-api） |
| 实体模块（产出 jar/war） | 37（36 jar + 1 war：`web/war`） |
| 主代码 Java 文件 | 约 7.3k（另有测试与生成代码，合计约 9.2k，与登记一致） |

README 登记的「48 模块」为粗粒度口径（不含根与部分聚合器）；本文以 pom 逐个数为准。

实体模块体积（main Java 文件数，量级感受用）：

| 模块 | 文件数 | 模块 | 文件数 |
|---|---|---|---|
| backend/core | 1444 | web/rest | 336 |
| base/common | 838 | storage/common | 184 |
| base/config | 622 | web/rest-ui | 1179 |
| storage/db | 644 | web/application | 46 |
| base/api | 555 | base/capi | 111 |
| 其余（<100） | — | base/papi | 71 |

---

## 2. 模块地图（按领域归类 → BinFlow 映射）

### 2.1 base 骨架层（7 实体模块）`[OSS]`

| Maven 模块 (artifactId) | 职责（一句话） | BinFlow 对应 |
|---|---|---|
| `base/utils` (artifactory-utils) | 零依赖工具箱（6 个文件，最底层） | `internal/` 各包零散 util（无独立包） |
| `base/config` (artifactory-config) | 全部配置的 JAXB XML 描述符模型（`org.artifactory.descriptor.*`：repo/security/backup/index/cleanup/replication…，622 文件，最大配置域） | `internal/config`（结构对应度最高的一块） |
| `base/papi` (artifactory-papi) | Public API：对外暴露的最小稳定接口（71 文件） | 各包 `api.go` 的「唯一公开入口」思想 |
| `base/capi` (artifactory-capi) | Common/SPI API：内部模块间服务契约，含 `org.artifactory.sapi.*`（storage/fs VfsItem 抽象、security、search） | 接口层的第二粒度（BinFlow 现为单粒度 api.go） |
| `base/api` (artifactory-api) | 服务接口大本营：`org.artifactory.api.{repo,storage,security,search,config,…}` + AQL 客户端模型（555 文件） | `internal/repo` + `internal/adapter` 的 api.go 等价物 |
| `base/common` (artifactory-common) | **核心公共实现**：`org.artifactory.addon.*`（61 个 addon 接口子包）、repo 配置实现、converter、logging、crypto（838 文件） | `internal/` 公共语义层（addon 接口 → BinFlow adapter SPI） |
| `base/base-rest` (artifactory-base-rest) | REST 基元：ArtifactoryRequest/Response 抽象、公共异常类型（9 文件，极薄） | `internal/httpapi` 的错误信封/请求抽象 |

### 2.2 backend 业务核心（2 实体模块）`[OSS]`

| Maven 模块 | 职责 | BinFlow 对应 |
|---|---|---|
| `backend/core` (artifactory-core) | **业务实现聚合根**：repo（local/remote/virtual 实现）、engine（上传/下载服务）、security、storage 编排、maven 索引、metadata provider、`org.artifactory.ph.*` 包处理 acl 骨架（1444 文件） | `internal/repo`（服务层核心）+ `internal/adapter`（部分） |
| `backend/traffic` (artifactory-traffic) | 请求流量日志（TrafficService/Entry/Logger，25 文件） | `internal/audit`（部分语义） |

### 2.3 storage 存储域（8 实体模块）`[OSS]`

| Maven 模块 | 职责 | BinFlow 对应 |
|---|---|---|
| `storage/common` (artifactory-storage-common) | 存储公共：`storage.fs`（VfsItem/VfsItemProvider/StorageTx 会话树）、fs/lock（锁 + AOP）、fs/service（FileService/PropertiesService/StatsService）、quartz 任务历史（184 文件） | `internal/storage`（会话/锁语义高度对应） |
| `storage/db` (artifactory-storage-db) | SQL 层：dao / entity / itest 实体、binstore、aql、conversion（644 文件） | `internal/metadata`（Store + 迁移器） |
| `storage/update` (artifactory-update) | 历史版本 DB/XML 配置 → 当前版本转换器（28 文件） | `internal/metadata/migrations`（升级语义） |
| `storage/config` (artifactory-storage-config) | 存储配置占位（仅 pom，无源码——装配依赖管理用） | —（无对应，纯 Maven 产物） |
| `storage/binaries/bridges/a2b-bridge` | Artifactory→BinaryStore 桥：实现 binary-store 服务 api（6 文件，`org.artifactory.sh.acl/service`） | storage Backend 接缝（ADR 留的 S3 缝 [M6+]） |
| `storage/binaries/bridges/b2a-bridge` | BinaryStore→Artifactory 反向桥：让存储处理器找到 artifactory 运行时 bean（2 文件） | 同上（反向回调） |
| `storage/binaries/bridges/itest-bridge` | 集成测试访问存储内部（BinaryProviderManager） | 测试专用接缝（BinFlow 用 `_test` 内 fakes） |
| `storage/binaries/gc` (storage-handler-gc) | 依赖 artifactory 代码的 GC 实现（Minor/Events/GC 任务，22 文件） | `internal/storage/gc.go` + `cmd ... gc` |

### 2.4 web / REST 表面（5 实体模块）`[OSS]`

| Maven 模块 | 职责 | BinFlow 对应 |
|---|---|---|
| `web/rest-common` (artifactory-rest-common) | REST 公共：exception mapper、model 基类、`rest.common.service.*`（admin/artifact/security/trash 服务基座）、aop、validator（147 文件） | `internal/httpapi` middleware/错误映射 |
| `web/rest` (artifactory-rest) | **/api REST 资源层**：`org.artifactory.rest.resource.<domain>.<Xxx>Resource`（121 个 Resource 类，域子包见 §4.3）+ JerseyApplication 装配（336 文件） | `internal/adapter/<proto>` handler + `internal/httpapi` 路由表 |
| `web/rest-ui` (artifactory-rest-ui) | **/ui REST**（控制台后端 API）：resource + model + service 三层，按 admin/artifacts/distribution/home/… 分域（1179 文件） | `internal/httpapi` 控制台 API [M4] |
| `web/application` (artifactory-web-application) | servlet 过滤器链（AccessFilter/RepoFilter/RequestFilter 等 14 个 filter）、WebAddonsImpl、会话/重定向（46 文件） | `internal/httpapi` middleware 链 |
| `web/war` (artifactory-web-war) | 打包：web.xml（filter 链顺序 + Jersey servlet 映射 /api /mc /ui） | `cmd/binflow-server` 装配 |

### 2.5 外围域（build/event/support/lifecycle/devenv，15 实体模块）`[OSS]`

| Maven 模块 | 职责 | BinFlow 对应 |
|---|---|---|
| `build-handler/build-handler-acl` | Build Info 的防腐层：`com.jfrog.build.acl.bridge.{BuildBridgeFromArtifactory, BuildBridgeToArtifactory}` 双向桥接口 + 模型（40 文件） | 跨域模型隔离参考（M6+ build info 若做） |
| `build-handler/build-handler-service` | Build Info 存储服务实现：dao/mapper/xml/tx（161 文件） | —（M6+） |
| `bridge/build-handler-bridge` | Spring bean 桥：artifactory bean ↔ build-handler bean 互见（21 文件） | — |
| `event-api-parent` + `index-event-api/{classes,schema}` + `stats-event-api/{classes,schema}` | **事件 API 家族**：OpenAPI YAML → proto → Java+Go 双语言绑定（每域独立 go.mod，`jfrog.com/jfrog-artifactory-index-event`）；包索引事件与 stats 事件（RTDEV-84772/META-2762） | 事件总线契约 [M6+]；**proto+Go 双绑定模式值得借鉴** |
| `support/core` | 支持包（诊断 bundle）收集：collectors/bundle/manifest（25 文件） | —（M5 可选诊断包） |
| `lifecycle` | 启动生命周期：webapp/storage 两阶段 init、postinit 转换器（9 文件，极薄编排） | `cmd/binflow-server` 启动序列 |
| `devenv` | 开发环境辅助（测试资源：dev/etc 配置样例） | `deploy/` 开发环境 |
| `coverage` / `bins` | 覆盖率聚合（全内部依赖）与打包辅助 | Makefile/CI |

### 2.6 双向对照速查（BinFlow → OSS）

| BinFlow 包 | OSS 对应模块（强→弱） |
|---|---|
| `internal/config` | base/config（descriptor 模型） |
| `internal/storage` | storage/common（fs 会话/锁）+ storage/binaries/*（blob 桥）+ storage/binaries/gc |
| `internal/metadata` | storage/db（dao/迁移）+ storage/update |
| `internal/repo` | backend/core 的 repo/ engine/ service/ + base/api 的 api.repo |
| `internal/adapter` | base/common 的 addon.* 接口 + pro 的 com.jfrog.ph.*（实现侧） |
| `internal/auth` | backend/core 的 security/ + base/api 的 api.security |
| `internal/audit` | backend/traffic + core 的统计 |
| `internal/httpapi` | web/application（filter 链）+ web/rest-common（错误映射）+ web/war（装配） |
| `internal/console` [M4] | web/rest-ui |
| `cmd/binflow-server` | web/war + lifecycle + core/spring 装配 |

---

## 3. 分层思想提炼

### 3.1 依赖方向：严格单向、五层收口 `[OSS]`

从 51 个 pom 的内部依赖提取的依赖图（→ 表示「依赖」）：

```
L0  utils（零依赖）
L1  papi → utils, build-handler-acl        capi → papi
    base-rest → utils                      config → capi, papi, utils
L2  api → base-rest, capi, config, build-handler-acl
    common → api, base-rest, config, utils, index-event-java
    （注意：common 依赖 api；api 不依赖 common——接口与公共实现分属两层）
L3  storage-common → capi, common, config, a2b-bridge
    storage-db → config, storage-common, build-handler-acl, a2b-bridge
    traffic → api, common, config, storage-common
L4  core → api, capi, common, config, lifecycle, storage-common,
           storage-db, support-core, traffic, update, build-handler-acl,
           index/stats-event-java, b2b-bridge, gc
L5  rest-common → common, config, core, storage-common
    rest → config, core, rest-common, build-handler-acl
    rest-ui → api, capi, common, config, core, rest-common, storage-common, support-core
    web-application → core, rest, rest-ui, traffic, bridge(build-handler)
    war → web-application
```

要点：
1. **接口层（L1–L2 的 api/capi/papi/base-rest）不依赖任何实现模块**；实现模块（core/storage-*）反向依赖接口。与 BinFlow 边界规则 2（`httpapi → adapter → repo → {storage, metadata}` 单向）同构。
2. **core 是唯一的全知聚合根**：它依赖除 web 层外的一切实现，web 层只经它触达业务。BinFlow 的 `repo` 扮演同角色。
3. **两粒度接口**：papi（对外最小稳定面）与 capi（模块间内部 SPI，含 `sapi` storage 抽象）分层，pro addon 只被允许挂进 capi/api 这一侧。BinFlow 目前单粒度 `api.go`，M3 包增多后可考虑同样拆分。
4. **双向桥以成对模块解环**：a2b/b2b 两个方向各一个桥模块（`storage/binaries/bridges/`），把「存储引擎 ⇄ 业务核心」的环切成两个单向依赖；build-handler 的 From/To 两个 bridge 接口同理。BinFlow 若遇 storage↔metadata 环，可参考此「成对桥」模式而非全局单例。

### 3.2 SPI 接缝：addon 如何挂载 `[双源]`

四条独立接缝，pro 闭源代码全部经由它们进入：

1. **addon 接口族**（`base/common` 的 `org.artifactory.addon.<type>`，61 个子包：docker/npm/pypi 不在其中——见下）。OSS 侧由单个聚合类 `CoreAddonsImpl`（backend/core）**一次性 implements 60+ 个 addon 接口**，未购买许可证时全部返回「功能未启用」语义；pro 侧用真实现 bean 覆盖（同 bean 名替换）。`[OSS]`（CoreAddonsImpl implements 清单直接可见）+ `[pro]`（batch3-addons 可见覆盖侧）
2. **REST 包扫描**（`web/rest` 的 JerseyApplication）：JAX-RS 资源按包名扫描注册，扫描列表同时包含 OSS 包（`org.artifactory`、`org.jfrog.repomd`）**和 pro 专有包**（`com.jfrog.ph`、`com.jfrog.bh`、`com.jfrog.build`、`com.jfrog.distribution.handler`、`com.jfrog.cs`）——pro 的 REST 资源类无需改 OSS 装配代码即可挂上 `/api`。`[双源]`（OSS 可见扫描列表；pro batch2 的 `com/jfrog/ph/` 下确有资源类）
3. **许可证管理器分层**（`base/common`）：`AddonsManagerBase` 抽象基类 + `OssAddonsManager`（OSS 实现）与 pro 的 LicenseManagerImpl 成对；addon 启用状态由许可证驱动。`[OSS]`
4. **事件/回调接缝**：`repo/interceptor/`（backend/core，31 个拦截器：Maven 元数据计算、NuGet 计算、Trash、build info 等在 deploy/delete/move 生命周期挂钩）+ `VirtualResolverFilter`（virtual 解析顺序过滤器）。`[OSS]`

**关键结论（M3 拆票直接相关）**：`org.artifactory.addon.*` 61 个子包里**没有** npm/pypi/cargo 等包类型的独立 addon 接口——这些协议在 7.x 已不走「addon 接口」老路，而是走 `com.jfrog.ph.<type>`（package handler，pro）+ OSS 侧的 `org.artifactory.ph.*` acl 骨架（见 §4.4）。

### 3.3 REST 资源层组织 `[OSS]`

- **一个 Resource 类 = 一组同前缀端点**：`web/rest` 的 `org.artifactory.rest.resource.<domain>/<Xxx>Resource`，121 个 Resource 类、约 40 个域子包。前缀最大的是 `system/`（64 文件，含证书/配置/导入导出/license），其次 `search/`（16）、`token/`、`repositories/`、`security/`。
- **三层分离**：resource（薄壳，参数绑定与响应码）→ `base/api` 的 `*Service` 接口 → `backend/core` 实现。rest-ui 同样 resource/model/service 三层但独立成模块。
- **装配**：web.xml 只声明 filter 链（14 个 filter 类、13 个注册名，顺序：ArtifactoryFilter → Mfe → Tracing → Session → Csrf → Request → Traffic → Probes → HttpHeadersAudit → GalleryPathTokenWrapping → Access → LoadHealer → Repo）和一个 Jersey servlet（映射 `/api/* /mc/* /ui/*`）；Spring 全注解装配（backend/core 409 个文件用 @Component/@Service），主代码无 spring xml。`[OSS]`
- **与 BinFlow handler 模式的对照**：OSS Resource 类 ≈ BinFlow `internal/adapter/<proto>` 的 handler 结构体；OSS 的域子包（`rest.resource.system` 等）≈ BinFlow 按协议分包。差异：OSS 是「按业务域分」+「JAX-RS 注解路由」，BinFlow 是「按协议分」+「显式路由表」——BinFlow 模式更利于 M3 的 per-protocol 独立拆票。

### 3.4 配置装配方式 `[OSS]`

1. **XML 描述符模型集中制**：全部配置对象（repo/backup/security/index/cleanup/replication/…）是 `base/config` 的 JAXB 注解类（`org.artifactory.descriptor.*`），带 `@XmlType propOrder`、`@XmlID`、diff 注解（`@DiffKey`/`@GenerateDiffFunction` 支持配置比对）。RepoLayout 即此模式：字段 = name/artifactPathPattern/descriptorPathPattern/distinctiveDescriptorPathPattern/两个 integrationRevision 正则。
2. **分层默认值**：`base/common/src/main/resources/templates/`（artifactory.config.template.yml、defaultRepository.json、apache/nginx 反代生成模板 ftl）+ 测试资源里的 `META-INF/binarystore-default.xml`。
3. **版本迁移器**：`base/common` 的 `converter/local/version/v1..v2` + `lifecycle/converter/postinit/v100..v119`（按版本号链式 post-init 转换）+ `storage/update`（2.0.0+ 的 DB/XML 迁移）。BinFlow 的 `migrations/001_init.sql` 单向前进模式更简单，但「postinit 链」对 M3 引入 remote/virtual 配置默认值有参考价值。

---

## 4. M3 高价值区导航

### 4.1 npm / PyPI / Maven 包处理：OSS 还是 pro？`[双源]`

**协议 HTTP 端点与包格式实现 → 全在 pro 反编译**：

| 内容 | 位置（pro 反编译） | 文件量 |
|---|---|---|
| 包协议处理器（npm/pypi/go/helm/conan/…34 种包类型） | `reverse-src/artifactory/src/batch2-protocol/com/jfrog/ph/` | 2184 个 Java |
| addon 桥接（bh：bridge/config/factory/gateway/interceptor/listener/mapper/repo/scheduler/transaction） | `reverse-src/artifactory/src/batch3-addons/org/artifactory/addon/bh/` | — |
| OSS 侧 ph acl 骨架（被 pro 实现的接口与桩） | OSS `backend/core` 的 `org.artifactory.ph.*`（约 50 类） | `[OSS]` |

**OSS 里能找到的骨架（M3 拆票时先读这些）**：

| 主题 | OSS 位置 | 参考价值 |
|---|---|---|
| 布局解析 | `base/config` 的 `descriptor/repo/RepoLayout` + `RepoLayoutBuilder` + `util/RepoLayoutUtils`；`web/rest-ui` 的 `RepoLayoutsResource` | Maven/Gradle/Ivy 布局正则模型与 API 面（配置+CRUD）`[OSS]` |
| Maven 元数据 | `backend/core` 的 `maven/`（MavenMetadataCalculator、MavenMetadataServiceImpl、index/、versioning/、snapshot/）+ `base/api` 的 `api/maven/*` | maven-metadata.xml 计算、索引器骨架 `[OSS]` |
| **包类型元数据注册表** | `backend/core` 的 `metadata/service/provider/`：50 个 `*MetadataProvider`（Maven/Npm/Pypi/Go/Docker/GAVC/…），抽象基类 AbstractMetadataProvider + MetadataResolver 接口 | **M3 最有价值的 OSS 骨架**：每包类型一个 provider、统一注册，BinFlow adapter 的 per-protocol 元数据抽取可同构 `[OSS]` |
| PyPI 仓库骨架 | `backend/core` 的 `repo/PypiHttpRepo(+Factory)`、`AnsibleHttpRepo(+Factory)` | 包类型专属 HttpRepo 子类的挂载方式 `[OSS]` |
| ph acl 接口面 | `backend/core` 的 `org.artifactory.ph/`（PackageArtifactService、PackageRepoService、PackageVirtualResolutionService、PackageUploadService 等接口/Impl 桩 + `ph/repo/{interceptor,provider}`） | pro 实现所覆盖的接口清单——**读接口签名即可知包处理服务边界** `[双源]` |

### 4.2 remote / virtual 仓库语义的 OSS 侧线索 `[OSS]`

OSS `backend/core` 的 `org.artifactory.repo/` 包含完整的仓库类层次（这部分 OSS 未闭源）：

- **类层次**：`Repo` 接口 → `RealRepo`/`StoringRepo`；`LocalRepo`、`RemoteRepoBase`(abstract) → `HttpRepo` → `PypiHttpRepo`/`AnsibleHttpRepo` 等 per-type 子类 + 配对的 `*Factory`；`VirtualRepo extends RepoBase implements StoringRepo`。
- **virtual 解析**：`repo/virtual/` 下 `VirtualRepositoriesResolver`、`VirtualRepositoryConfigResolver`、`VirtualResolverRequestFilter implements VirtualResolverFilter`（解析顺序过滤接缝）、`VirtualDownloadStrategy`/`ArtifactoryVirtualDownloadStrategy`、`PathTranslator`；`repo/NestedRealRepositoriesWithRoutes`（嵌套真实仓与路由）。
- **remote 行为**：`repo/cache/`（远程缓存）、`RemoteResourceTrafficContextHolder`、`EagerResourcesDownloader`、`UrlVerifierRedirectStrategy`（SSRF 相关的重定向策略）、`ConnectionStatesRtfsResponse`（不可达状态，OSS `base/common` 的 `repo/` 下）。
- **配置模型**：`base/common` 的 `repo/{Local,Http,Virtual,Federated}RepositoryConfigurationImpl` + `RepositoryConfigurationBase`（repoLayoutRef 字段在此层）+ `Public{Local,Remote,Virtual}RepoConfig`（对外投影）。
- **拦截器/校验器**：`repo/interceptor/`（31 个）+ `repo/validator/` + `repo/service/flexible/`（validators/listeners/interfaces——M3 的 remote pull-through 校验链参考）。

已有行为规格 `repo-semantics.md`（M1 版）可在此基础上补 M3 细节。

### 4.3 REST 域清单（web/rest 资源子包 → 端点归属）`[OSS]`

按文件数排序的主要域：system(64) search(16) devopsagent(12) token(10) mfe(10) cleanup(9) artifact(9) security(7) repositories(7) federation(7) retention(5) mpu(5) stats replication release upload …。M3 相关：`artifact/legacy`、`search/fields`、`repositories`。行为级端点表见既有 `rest-api.md`，不重复。

### 4.4 事件 API 的 proto+Go 双绑定 `[OSS]`

`index-event-api`/`stats-event-api` 模式：OpenAPI YAML（`schema/src/main/resources/public/openapi/`）→ proto → Java + **Go 绑定（每域独立 go.mod）**，为 jfbus 事件总线输出。若 BinFlow M6+ 做事件/复制，这种「schema 单源、双语言生成」可直接照搬思想。

---

## 5. 待验证清单（低强度项）

| 条目 | 疑点 | 验证方式 |
|---|---|---|
| `storage/config` 为空壳 | 仅 pom 无源码，推测是依赖管理占位 | 跑一次 OSS 构建看产物（成本高，可搁置） |
| CoreAddonsImpl 被 pro 覆盖的机制 | 推测为同 bean 名替换，pro 侧装配未完整反编译 | pro batch3 搜 bean 定义类名对照 |
| event-api Go 绑定产物完整性 | go.mod 存在但生成代码不在仓库（构建期生成） | 无需验证，仅结构参考 |
| `web/rest` 的 `devopsagent`/`mfe` 域 | 7.16 新增域，行为规格 rest-api.md 未覆盖 | M4 控制台 API 设计时再补 |

---

## 6. 对 BinFlow 的结构启示（基于以上证据）

1. **per-protocol 子包结构与 OSS 包处理划分同构**（证据 §4.1）：OSS/pro 把包类型实现切成「OSS acl 接口面（`org.artifactory.ph.*`）+ pro 协议实现（`com.jfrog.ph.<type>`）+ 统一元数据注册表（54 个 MetadataProvider）」。BinFlow 的 `internal/adapter/{generic,docker,maven,npm,pypi}` 三件套应对应为：handler（HTTP 面）+ layout/metadata provider（包解析）+ 与 `internal/repo` 的服务调用——M3 每协议一张票正好按此切。
2. **接口两粒度 + 实现单向依赖**（证据 §3.1）：OSS 用 papi/capi/api 三层把「对外稳定面」与「模块间 SPI」分开，且接口模块零实现依赖。BinFlow M3 若 remote/virtual 落地，建议在 `internal/repo/api.go` 内显式分「公开用例面」与「adapter 可实现的 SPI 面」两段（或两个文件），避免 adapter 反向摸 repo 内部——这正是 OSS 防止 pro addon 摸内部所用的手段。
3. **成对桥解依赖环**（证据 §3.1 要点 4）：storage↔业务、artifactory↔build-handler 的双向需要均以「两个单向桥模块」而非互相 import 解决。BinFlow 出现 `storage` 需要 repo 层信息（如 GC 需要知道 remote 缓存语义）时，用回调接口注入，不 import。
4. **拦截器链作为协议无关扩展点**（证据 §3.2 接缝 4）：deploy/delete/move 生命周期上的 31 个 interceptor 承载了 Maven 元数据计算、trash、属性传播等横切逻辑。BinFlow M3 的 maven-metadata 生成、npm metadata 更新若不想塞进 adapter，可在 `internal/repo` 定义 deploy/delete 钩子接口，adapter 注册实现——与 OSS 的 Interceptors 聚合（`implements Iterable, ReloadableBean`）同构。
5. **配置对象集中描述符 + 版本化迁移链**（证据 §3.4）：全部仓库/安全/备份配置在单一描述符模块以带 diff 注解的模型表达，且配 postinit 版本链转换器。BinFlow M3 引入 remote/virtual 配置时，`internal/config` 的 Config 结构与 `metadata` 迁移应同步加版本字段（若尚未有），避免 M4 再做一次性大迁移。
6. **（可选）事件 schema 单源双语言**（证据 §4.4）：M6+ 事件/复制需求出现时，优先 OpenAPI/proto 单源生成，而非手写两端模型。

---

## 变更记录

| 日期 | 变更 | 作者 |
|---|---|---|
| 2026-08-18 | 初版（T-49）：模块地图 / 分层 / M3 导航 / 结构启示 | reverse-engineer |
