# Artifactory 全量 REST 兼容矩阵（rest-compat-matrix，活体 registry）

> **定位（M17 跨切程锚票 T-503，2026-09-06 建）**：BinFlow 现状 REST 面（`fern/openapi/binflow.json` 158 ops——tools/openapi-spec 生成器维护）× Artifactory 全量 REST 面的**逐端点四态对账 registry**。全量兼容 = PRODUCT.md 范围演进记录第 5 条（用户终裁 2026-09-06：「除了 2 不做，剩下的都做」——Xray 唯一排除）。本文件是跨切程（M17 首程 T-504/505/506 + M18+ 滚程）的**唯一行集事实源**：每程次收口时更新对应行（态翻转 + 行级 changelog），不新建平行清单。
>
> **取证基线（Q10 结论——纯书面三源，零活体）**：7.161 pro 参照容器（router 不拉起）与 t226 OSS 7.84.10（access→PG 拒连）双损坏（T-488 §0 留痕），本轮**零活体实证**。三源：① 官方 REST reference 三索引实取（2026-09-06）：`docs.jfrog.com/artifactory/reference/llms.txt`（230 条）、`docs.jfrog.com/administration/reference/llms.txt`（385 条）、`docs.jfrog.com/integrations/reference/llms.txt`（38 条）——合计 653 条目，含逐条 OpenAPI 路径；② `docs/reverse/` 既有 37 份规格（rest-api.md 为权威既有面 + inv-2 表面测绘 ≈1,866 方法级操作）；③ `fern/openapi/binflow.json` 158 ops。置信度：**高** = 官方 + 既有规格双源一致；**中** = 单源（官方或反编译其一）；**低** = 推测/路径字面待复核。
>
> **clean-room 声明**：本矩阵只登记行为对账结论；行为细节一律指向既有规格文件或官方参考页，不复制实现。

## 0. 口径与图例

| 项 | 定义 |
|---|---|
| 四态 | **✅ 已兼容**（BinFlow 有对位端点且行为一致——含「超集方向」差异：字段更多/更严不破坏客户端）；**◐ 部分兼容**（有对位端点但存在行级差异——「差异要点」列必须写明）；**❌ 缺位**（无对位端点——扩张候选）；**⛔ 不适用**（三类：pro/enterprise 商业档且 BinFlow 已裁不做、xray_tied（Q9 license 族连带缺位登记）、物理不可行/产品界外——云专属、已 sunset 服务、外部独立产品面） |
| 档位口径（沿 aql.md §8.2） | 官方标注「Requires Artifactory Pro/Enterprise」**不等于** ⛔——BinFlow 无许可门，按官方全集口径对齐实现（checksum 搜索先例）。⛔ 仅用于上表三类。 |
| 层级（沿 M17 PRD §5.1） | **A** 对齐 Artifactory 端点/语义；**C** BinFlow 自有（无 Artifactory 对位——超集行）；**D** 有意不兼容或不做 |
| 路径写法 | 均相对 Artifactory `/artifactory` 上下文（BinFlow 对位 = `/binflow` 前缀同路径）；路径字面未经双源核实的行在置信度列标「中/低」并进 §12 待验证 |
| 优先级 | P0 差异修复（既有端点行为漂移——修复优先）/ P1 高价值扩张 / P2 常规扩张 / P3 低优·登记即可 |

## 1. 总账

| 面 | 规模 | 出处 |
|---|---|---|
| 官方 REST reference（三索引） | 653 条目：artifactory 230 + administration 385 + integrations 38 | 2026-09-06 实取（§头部三 URL） |
| 其中 Artifactory 核心 REST 面 | ≈430 条目（administration 385 中 ≈185 属外部产品/云/Access 服务面——归 D14 ⛔ 族登记） | 索引逐条归类（本矩阵 §2~§11 行覆盖） |
| 反编译表面测绘 | 357 resource 类 ≈1,866 方法级操作（含 UI 内部面 ≈470 + 协议子资源 ≈207 + 新包框架 ≈432） | inv-2-surface.md §0 |
| BinFlow 现状 | 112 路径 / 158 ops / 20 域标签 | fern/openapi/binflow.json |

**域清单**：D01 制品与存储 / D02 仓库配置 / D03 搜索 / D04 安全 / D05 复制 / D06 系统与运维 / D07 Build-info（指针）/ D08 Release Bundle·Distribution（指针）/ D09 Webhook / D10 用户插件 / D11 生命周期治理 / D12 协议面 / D13 UI 内部面 / D14 外部产品面。

**四态 × 域分布**（行数；含臂行/族行，统计以各表行计）：

| 域 | ✅ | ◐ | ❌ | ⛔ | 超集(C) | 行计 |
|---|---|---|---|---|---|---|
| D01 制品与存储 | 13 | 7 | 12 | 3 | 2 | 37 |
| D02 仓库配置 | 2 | 3 | 6 | 1 | 1 | 13 |
| D03 搜索 | 9 | 0 | 9 | 2 | 0 | 20 |
| D04 安全 | 16 | 6 | 12 | 0 | 1 | 35 |
| D05 复制 | 0 | 5 | 4 | 1 | 1 | 11 |
| D06 系统与运维 | 4 | 3 | 12 | 4 | 6 | 29 |
| D07 Build-info（指针） | 0 | 0 | 11 | 0 | 0 | 11 |
| D08 Release Bundle（指针） | 0 | 0 | 6 | 1 | 0 | 7 |
| D09 Webhook | 7 | 0 | 1 | 1 | 0 | 9 |
| D10 用户插件 | 0 | 0 | 1 | 0 | 0 | 1 |
| D11 生命周期治理 | 0 | 0 | 2 | 1 | 0 | 3 |
| D12 协议面（含子表） | 6 | 3 | 7 | 1 | 0 | 17 |
| D13 UI 内部面 | 0 | 0 | 0 | 1 | 0 | 1 |
| D14 外部产品面（族级合并行） | 0 | 0 | 0 | 1 | 0 | 1 |
| **合计** | **57** | **27** | **83** | **17** | **11** | **195** |

BinFlow 158 ops 全部落位：**直接对位 119 / 路径别名变体 19**（gc 1 + backups 5 + maintenance 2 + replication 族 11——Artifactory 路径族对位见 D05/D06 各行）**/ 纯超集（C 层自有）20**（users 集合直建 1、permissions 3、audit 1、health 1、storage stats/usage 3、settings 1、schedules 1、cleanup 2、session 3、addons 1、repoTest 1、replication 测试 2）。

---

## 2. D01 制品与存储操作（优先级序：P0 差异修复 → P1 → P2 → P3）

| # | 方法+路径（Artifactory） | 官方锚点 / 既有规格 | BinFlow op | 态 | 差异要点 / 备注 | 层 | 置信度 | 优先级 |
|---|---|---|---|---|---|---|---|---|
| 1 | GET `/api/storage/{repoKey}/{path}`（无参形态：FileInfo/FolderInfo） | Get Storage Item Information + rest-api.md §3 | storageItemInfo / storageList | ✅ | 字段集对齐（size 字符串、children 排序、remoteUrl 仅 remote-cache）已按规格实现；**downloadUri 处置（T-493/FR-157②，LC-107 二选一=归位，2026-09-07）**：uri=元数据视图（`/binflow/api/storage/...`）、downloadUri=直取下载 URI（`/binflow/<repo>/<path>`）——官方 FileInfo 示例语义 + generic/maven 上传 201 体内核同形佐证；FolderInfo 维持无 downloadUri 的 as-built（官方 folder 示例同形） | A | 高 | — |
| 2 | ↳ `?properties` / `?list` / `?stats` / `?permissions` 四臂 | 同上 + gap-endpoints §5.3 / K69 | properties.go / storage.go | ✅ | 五臂互斥语义对齐；?permissions 键值方向按 T-113 勘误形态 | A | 高 | — |
| 3 | ↳ `?propertiesXml` 臂 | 同上 | —（501 显式拒绝） | ◐ | BinFlow 有意不实现 XML 形态（storage.go:116 notImplemented）——XML 客户端面缺位，修复或留痕归带② | A | 中 | P0 |
| 4 | ↳ `?lastModified` 臂 | 同上 | —（501） | ◐ | 同上；官方语义=目录内最新修改项 + Last-Modified 头 | A | 中 | P0 |
| 5 | ↳ `?list` 参数族 `deep/depth/listFolders/mdTimestamps/includeRootPath/includePropertiesMd5` | 同上 | 部分（list 基础臂） | ◐ | BinFlow 实现 list 主臂；深列表/元数据时间戳参数子集待核对（带②核对行） | A | 中 | P0 |
| 6 | PUT `/api/storage/{repoKey}/{path}?properties=k=v&recursive&atomic`（Set） | Set Item Properties | storagePropertiesPut | ✅ | matrix 分号语法=路径语法注记（T-447 契约注记：REST 只认逗号配对——T-493 wire 断言钉死：%3B=值内容；raw ';' 使整对从 net/url 解析中脱落〔GET 落 plain item、写动词 E-26 404〕，分号永不作配对分隔） | A | 高 | — |
| 7 | DELETE 同路径 `?properties=k1,k2&recursive`（Delete） | Delete Item Properties | storagePropertiesDelete | ✅ | — | A | 高 | — |
| 8 | POST 同路径 `?recursive&atomic`（Update，6.1.0 增量改属性） | Update Item Properties | — | ❌ | 三动词缺一；扩张候选（带②） | A | 高 | P1 |
| 9 | PUT `/{repoKey}/{path}`（Deploy / 尾斜杠建目录 / matrix 属性 / checksum 三头 / X-Checksum-Deploy） | Deploy Artifact or Create Directory + Deploy by Checksum + rest-api.md §1.2–1.3 | artifactUpload | ✅ | 校验链/405+Allow/409 文案已按规格；checksum 文件旁车（.sha1 上传）在位 | A | 高 | — |
| 10 | GET / HEAD `/{repoKey}/{path}`（下载/元信息 + Range/304/ETag） | Retrieve Artifact | artifactDownload / artifactHead | ◐ | 下载主链 ✅；**最新版 token**（`SNAPSHOT`/`[RELEASE]`/`[INTEGRATION]` 路径解析）缺位——dep repo layout 引擎（K73 联动，M18+） | A | 高 | P1 |
| 11 | DELETE `/{repoKey}/{path}`（?atomic） | Delete Item | artifactDelete | ✅ | atomic 单事务语义规格在案（rest-api.md §1.1） | A | 高 | — |
| 12 | POST `/api/copy/{srcRepo}/{srcPath}`（to/dry/suppressLayouts/failFast/atomic） | Copy Item + repo-operations.md §1 | artifactCopy | ✅ | 参数族全实现；跨布局翻译 BinFlow 无布局引擎（suppressLayouts 幂等空转——语义注记） | A | 高 | — |
| 13 | POST `/api/move/{srcRepo}/{srcPath}` | Move Item + repo-operations.md | artifactMove | ✅ | 同上 | A | 高 | — |
| 14 | POST `/api/flat/copy|move/{srcRepo}/{srcPath}` | 官方无页——inv-2 §A + repo-operations.md §1 行 3/4 | — | ❌ | Artifactory 系统开关默认关（整端点 404）；低优扩张 | A | 中 | P3 |
| 15 | GET `/api/archive/download/{repoKey}/{path}`（整包/整仓，archiveType 必填） | Retrieve Folder or Repository Archive + repo-operations.md §1 行 5/6 | archiveDownload | ✅ | 四类型/参数错误文案按规格；folderDownloadConfig 配额门 BinFlow 自有形态 | A | 高 | — |
| 16 | ↳ entry 抽取（`!` 归档内单文件语法） | Archive Entry Download | — | ❌ | `!` 路径语法与 entry 参数缺位；扩张候选（带②） | A | 中 | P1 |
| 17 | POST `/api/archive/buildArtifacts` | Retrieve Build Artifacts Archive | — | ❌ | **指针行**：属 build-info 域（build-info.md / repo-operations.md §1 行 9 登记）——T-511 联动，M17 面外 | A | 高 | P2 |
| 18 | PUT `/{repoKey}/{path}` + `X-Explode-Archive: true`（捆绑解包上传） | Deploy Artifacts from Archive | — | ❌ | FR-160 已登记 X-Explode-Archive staging 同族迁移（internal/repo/archive.go:1082） | A | 中 | P2 |
| 19 | GET `/{repoKey}/{path}?mark=&gcCaches=`（同步下载/缓存预热） | Artifact Sync Download | — | ❌ | remote 仓预热场景；P2 | A | 中 | P2 |
| 20 | DELETE `/api/zap/{path}`（删物理保元数据催 GC） | Zap Cache + inv-2 §A | — | ❌ | 运维向；P2 | A | 中 | P2 |
| 21 | POST `/api/storage/{path}`（Set Item SHA256 Checksum——sha256 属性回填） | Set Item SHA256 Checksum | — | ❌ | 遗留兼容端点（5.5 前用户）；BinFlow 原生 sha256——低优 | A | 中 | P3 |
| 22 | POST `/api/trash/empty` | Empty Trash Can | trashEmpty | ✅ | — | A | 高 | — |
| 23 | POST `/api/trash/restore/{path}` | Restore Item from Trash Can | trashRestore | ✅ | — | A | 高 | — |
| 24 | DELETE `/api/trash/clean/{path}` | Delete Item From Trash Can | trashClean | ✅ | — | A | 高 | — |
| 25 | PUT `/api/trash/settings/deleteDirectoryTrashProperty`（空目录进回收站时限） | Delete Redundant Records to Trash Can Directories | — | ❌ | trashcanConfig 旋钮面；P3 | A | 中 | P3 |
| 26 | POST `/api/system/storage/gc` | Run Garbage Collection（OpenAPI 实取路径） | —（BinFlow 走 `/api/v1/system/gc`） | ◐ | **路径别名缺位**：Artifactory 路径 `/api/system/storage/gc` 未挂——带①行；GC 本体（trash 过期 + 孤儿块）已实现 | A | 高 | P0 |
| 27 | POST `/api/system/storage/prune/start` + GET `.../status` + POST `.../stop`（PUD） | Start/Get Status/Stop PUD Process | —（cleanup 族自有形态） | ❌ | BinFlow `/api/v1/system/cleanup` 自有；PUD 三态任务面缺位 | A | 中 | P2 |
| 28 | GET `/api/storageinfo` + POST `/api/storageinfo/calculate` | Get/Refresh Storage Summary Info + gap-endpoints §5 | —（BinFlow 走 `/api/v1/storage/usage` 族） | ◐ | 用量扇出语义已有自有载体；storageinfo 形态（binariesSummary/fileStoreSummary/repositoriesSummaryList + 冷缓存 503）缺位——FE 用量页对位锚 | A | 高 | P1 |
| 29 | GET `/api/tasks`（后台任务清单） | Get Background Tasks + inv-2 §H | — | ❌ | 路径字面待复核（中）；P3 | A | 中 | P3 |
| 30 | POST `/api/system/storage/compact`（在线压缩） | Optimize System Storage | — | ❌ | FR-158「Compress Internal Database」cron 载体联动；路径字面待复核 | A | 中 | P2 |
| 31 | GET `/api/system/metrics`（应用指标）+ Open Metrics 面 | Get Artifactory Application Metrics / Get the Open Metrics + metrics.md | —（BinFlow 自有 Prometheus /metrics） | ◐ | 指标本体有（metrics.md）；官方两端的路径/字段形态差异待对位（低优） | A/C | 中 | P3 |
| 32 | `/api/v1/uploads/{create,urlPart,complete,status,abort,config}` + PUT `part/{id}/{n}`（MPU 直传会话） | 官方索引无页——inv-2 §H「v1/uploads 六件」 | uploads×7 | ✅ | 官方 reference 未载（反编译 + BinFlow 双在案——「此条补充官方规范」） | A | 中 | — |
| 33 | （超集）GET `/api/v1/storage/stats`、GET `/api/v1/storage/usage[/{repo}]` | —（Artifactory 无对位） | storageStatsGet / storageUsageBatch / storageUsageOne | C | BinFlow 自有用量面（storageinfo 对位扩张后评估去留） | C | 高 | — |
| 34 | （超集）GET `/api/archive/download` 的 BinFlow 参数差异 | — | — | C | 见行 15 注记（配置门自有形态） | C | 中 | — |

> ⛔ 登记（D01 相邻）：Create/Replace Signed URL（Cloud Enterprise 专属——物理不可行）；Distribute Artifact（Bintray 2021 sunset——物理不可行）。计入 §1 表 D01 ⛔=3（按官方条目数）。

---

## 3. D02 仓库配置

| # | 方法+路径 | 官方锚点 / 既有规格 | BinFlow op | 态 | 差异要点 | 层 | 置信度 | 优先级 |
|---|---|---|---|---|---|---|---|---|
| 1 | GET `/api/repositories?type=&packageType=&project=` | Get Repositories by Type and Project + gap-endpoints §5.1 | repoList | ◐ | ① `url` 已含 `/binflow` 上下文前缀（**T-493/FR-157① 已落，2026-09-07**——rest-api.md §2 `<contextUrl>/<key>`；单仓 GET 回显同族对齐）；② `project` 参数缺位（BinFlow 无 projects 域——参数容忍语义待定：Artifactory 对 project 过滤，BinFlow 应按忽略或空集裁定）；③ type/packageType 过滤已实现（ListReposFiltered） | A | 高 | P0 |
| 2 | GET `/api/repositories/{key}` | Get Repository Configuration + rest-api.md §2 | repoGet | ✅ | 404 纯文本 vs envelope：BinFlow 保留 envelope（E-01 既有裁——差异留痕）；回显域差异随行 3 一并（FR-156①） | A | 高 | P0 |
| 3 | PUT `/api/repositories/{key}`（创建，200 纯文本） | Create Repository | repoPut | ◐ | **configJSON 四域静默丢弃**（maxUniqueSnapshots/repoLayoutRef/blackedOut/archiveBrowsingEnabled）——FR-156① round-trip 收口（带①差异修复行）；key 冲突 400/409 语义已对齐 | A | 高 | P0 |
| 4 | POST `/api/repositories/{key}`（更新） | Update Repository Configuration | repoPost | ◐ | 同行 3（同一 round-trip 缺口） | A | 高 | P0 |
| 5 | DELETE `/api/repositories/{key}` | Delete Repository | repoDelete | ✅ | 最后一个 local 仓守卫文案核对项（低危） | A | 高 | — |
| 6 | GET `/api/repositories/configurations?packageType=&repoType=` | Get All Repository Configurations（admin） | — | ❌ | 带①扩张行 | A | 高 | P1 |
| 7 | GET `/api/v2/repositories/{key}` | Get Repository Configuration (v2) | — | ❌ | 带①扩张行 | A | 中 | P1 |
| 8 | `/api/v2/repositories/batch` 族（GET 批读〔路径实取〕/ POST 批建 / PUT 批改 / DELETE 批删） | Get Batch of Repositories by Name / Create·Update Multiple / Delete Multiple Repositories（DELETE 207 混合态） | — | ❌ | GET 批读路径已实取核验（高）；批建/改/删方法归属中置信；带①扩张行 | A | 中 | P1 |
| 9 | GET `/api/repositories/existence`（按 type/project 探测，7.103+） | Check If Repository Exists（路径实取） | — | ❌ | 带①扩张行 | A | 高 | P1 |
| 10 | GET `/api/repositories/{key}`（Remote Repository Configuration deprecated 变体） | Remote Repository Configuration (Deprecated) | — | ⛔ | deprecated 别形态——现行对位（行 2）已列，不实现 | D | 高 | — |
| 11 | Federation 族（convert local↔federated ×2、成员/状态/镜像/failed binary tasks/pairing token 等 ≈19 端点） | 官方 federation 条目群 + inv-2 §1.B | — | ❌ | **M18+ Federation 专程承载**（Q1 裁定滚 M18；T-519 registry 段）——族级登记，逐端点展开候专程规格票 | A | 高 | P2 |
| 12 | `/api/repo_layouts[/{name}]` CRUD（26 内置布局） | 官方新索引无页——经典参考 + inv-2 §1.B / config-formats.md | — | ❌ | K73 repoLayoutRef 布局引擎联动（FR-156.1 评估项）；候 M18+ | A | 中 | P2 |
| 13 | （超集）POST `/api/repositories/{key}/test`（remote 连通测试三臂） | —（Artifactory 公开面无对位；UI 内部 testLocal/RemoteReplication 旁证 replication.md §C） | repoTest | C | BinFlow 自有；Artifactory 等价能力在 UI 内部面 | C | 中 | — |

> ⛔ 登记（D02）：deprecated Remote Configuration 变体（行 10）；BinFlow 无 federated rclass 的配置面（并入行 11 Federation 族）——计 ⛔=2（行 10 + 族内 deprecated 项归并不另计）。

---

## 4. D03 搜索（AQL + 老搜索 16 + docker 搜索 + UI 族）

| # | 方法+路径 | 官方锚点 / 既有规格 | BinFlow op | 态 | 差异要点 | 层 | 置信度 | 优先级 |
|---|---|---|---|---|---|---|---|---|
| 1 | POST `/api/search/aql` | AQL + aql.md（官方 OpenAPI 唯一行为基准） | searchAql | ✅ | M15 落地；builds/modules/dependencies 入口随 T-511 开、promotions/releases M17 维持 400（aql.md §15.3 翻转点在册） | A | 高 | — |
| 2 | GET `/api/search/artifact?name=&repos=` | Artifact Search (Quick Search) + aql.md §0-5 | searchArtifact | ✅ | K64 两校准点（大小写不敏感①）——带③核对行；`*` 字面维持 | A | 高 | P0 |
| 3 | GET `/api/search/gavc?g=&a=&v=&c=&repos=` | GAVC Search | searchGavc | ✅ | M15 | A | 高 | — |
| 4 | GET `/api/search/prop`（任意查询参数即属性键；repos 限定） | Property Search + aql.md §8.2 | searchProp | ✅ | 键无值=`*` 语义核对（带③） | A | 高 | P0 |
| 5 | GET `/api/search/checksum?sha1=&md5=&sha256=&repos=` | Checksum Search | searchChecksum | ✅ | envelope 为 E-09 全字段超集（uri/downloadUri）——超集方向留痕不追改 | A | 高 | — |
| 6 | GET `/api/search/pattern?pattern=` | Pattern Search | searchPattern | ✅ | `**` 不支持行为核对（带③） | A | 高 | P0 |
| 7 | GET `/api/search/usage?notUsedSince=&createdBefore=&repos=` | Artifacts Not Downloaded Since + aql.md §14.2 | searchUsage | ✅ | **参数名 wire 实态 = notUsedSince**（PRD usageSince 笔误警示——对外参数面以 notUsedSince 为准）——带③核对行 | A | 高 | P0 |
| 8 | GET `/api/search/dates?from=&to=&dateFields=` | Artifacts With Date in Date Range + aql.md §14.3 | searchDates | ✅ | from 必填 400 逐字文案 + maven-metadata.xml 恒排除——带③核对行 | A | 高 | P0 |
| 9 | GET `/api/search/creation?from=&to=&repos=` | Artifacts Created in Date Range + aql.md §14.3 | searchCreation | ✅ | 同族 404 空集语义——带③核对行 | A | 高 | P0 |
| 10 | GET `/api/search/versions?g=&a=&v=&repos=&remote=` | Artifact Version Search | — | ❌ | 带③扩张行；remote=1 远端搜索 dep 远端列举（remote-browsing.md §1） | A | 高 | P1 |
| 11 | GET `/api/search/latestVersion?g=&a=&v=&repos=&remote=` | Artifact Latest Version Search Based on Layout | — | ❌ | 带③扩张行；版本排序算法 dep 布局 token 解析（K73 注记——可按 maven-2 默认布局先行） | A | 高 | P1 |
| 12 | GET `/api/search/latestVersionByProperties`（属性键值对最新版） | Artifact Latest Version Search Based on Properties | — | ❌ | 带③扩张行（低危可裁） | A | 中 | P2 |
| 13 | GET `/api/search/badChecksum?type=&repos=` | Bad Checksum Search（响应上限 10,000） | — | ❌ | 带③扩张行（低危可裁） | A | 中 | P2 |
| 14 | GET `/api/search/archive`（Archive Entries Search，官方 deprecated） | Archive Entries Search (Class Search) | — | ❌ | deprecated 条目——低优 | A | 中 | P3 |
| 15 | GET `/api/search/license` | License Search | — | ⛔ | **Q9 定案：缺位登记不做**——license 识别系 xray_tied 族（licences.xml 91 模式）无数据源，不伪造；PM 建议①已裁 | D | 高 | — |
| 16 | GET `/api/search/dependency?sha1=&buildRepo=&project=` | Builds for Dependency | — | ❌ | **指针行**：aql.md §15.4 / build-info.md §4 wire 锚已冻结——T-511 落地开，带③对账行 | A | 高 | P1* |
| 17 | POST `/api/search/buildArtifacts` | Build Artifacts Search | — | ❌ | **指针行**：同上（三 400 逐字文案 + downloadUri 键在案）——T-511 落地开 | A | 高 | P1* |
| 18 | GET `/api/search/docker/manifests/...`（parent manifest lists）+ List Docker Repositories / List Docker Tags | Find Parent Manifest Lists / List Docker Repositories / List Docker Tags | — | ❌ | docker 域检索面（D12 联动）——OCI referrers 已有对位载体（dockerReferrers） | A | 中 | P2 |
| 19 | UI 搜索族（artifactsearch 8 op / stashResults 10 / packagesSearch 2 / syntax-search 1 / searchResults 1——`/ui/api` 面） | inv-2 §1.C + aql.md §14.5 | — | ⛔ | UI 内部面（D13 登记口径）；BinFlow 控制台自有搜索面 | D | 高 | — |

> *P1* = 行为规格已冻结（T-488），实现随 T-511（FR-152）——不进带③实现行，对账指针。

---

## 5. D04 安全（用户/组/权限/令牌/密钥/SSO 家族）

> 官方文档现状注记（置信度高，2026-09-06 实取）：现行官方 reference 已把用户/组/token 主推面迁到 **Access 服务 API**（`/access/api/v2/users`、`/access/api/v1/tokens`）；Artifactory 侧 `/api/security/*` 面官方标注 deprecated 但仍为工具链（jf CLI/CI 插件）事实兼容面——BinFlow 以 Artifactory 路径族为 A 层对位，Access 面登记缺位。

| # | 方法+路径 | 官方锚点 / 既有规格 | BinFlow op | 态 | 差异要点 | 层 | 置信度 | 优先级 |
|---|---|---|---|---|---|---|---|---|
| 1 | GET `/api/security/users` | Get User List + gap-endpoints §1.1 | userList | ✅ | 数组×{name,uri,realm} 三字段闭集——带②核对行（无过滤参数语义） | A | 高 | P0 |
| 2 | GET `/api/security/users/{name}` | Get User Details + gap-endpoints §1.2 | userGet | ◐ | 回显字段集差异：`status` 枚举闭集（invited/enabled/disabled/locked）、**无 `enabled` 布尔**、`lastLoggedIn` 条件出现、`groups` 空集形态——带②差异修复行 | A | 高 | P0 |
| 3 | PUT `/api/security/users/{name}`（创建，201 无 body） | Create User + auth-model.md §1 | userPut | ✅ | 校验链按规格 | A | 高 | — |
| 4 | POST `/api/security/users/{name}`（部分更新，200） | Update a User (Partial Update) | userPost | ✅ | — | A | 高 | — |
| 5 | DELETE `/api/security/users/{name}` | Delete User + gap-endpoints §2 | userDelete | ✅ | 级联（ACE 剥离→转发删除→缓存失效）与 Access 404 幂等语义按规格；最后 admin 守卫缺口登记（gap-endpoints §6.1） | A | 高 | — |
| 6 | POST `/api/security/users/authorization/changePassword` | Change a User Password + auth-model.md | changePasswordAlias | ✅ | — | A | 高 | — |
| 7 | PUT `/api/security/password`（改自己口令） | Set User Password（经典条目）+ auth-model.md | changePasswordOwn | ✅ | — | A | 高 | — |
| 8 | （超集）POST `/api/security/users`（collection 直建） | —（Artifactory 无此形态） | userCreatePost | C | BinFlow 自有便利面——登记不追改（超集方向） | C | 高 | — |
| 9 | 密码过期族：`PUT /api/security/users/authorization/expire`、`.../unexpire`、`POST /api/security/users/authorization/expirePasswords`、GET/PUT 过期策略 | Expire Password ×N + Get/Set Password Expiration Policy | — | ❌ | 带②扩张候选（家族级） | A | 中 | P2 |
| 10 | 用户锁定族：GET/PUT lock policy、GET lockedUsers、unlock×2 | Retrieve/Configure User Lock Policy / Get Locked Out Users / Unlock ×2 | — | ❌ | 带②扩张候选（家族级） | A | 中 | P2 |
| 11 | GET `/api/security/encryptedPassword` | Get User Encrypted Password | — | ❌ | 带②扩张候选（单端点） | A | 中 | P2 |
| 12 | GET `/api/security/groups` | Get a List of Groups + rbac-model.md §1.2 | groupList | ✅ | 数组×{name,uri} | A | 高 | — |
| 13 | GET `/api/security/groups/{name}[?includeUsers=true]` | Get Group Details + gap-endpoints §3.1 | groupGet | ✅ | includeUsers → userNames[] 语义——带②核对行 | A | 高 | P0 |
| 14 | PUT `/api/security/groups/{name}`（创建/替换，**组成员全量替换**） | Create a Group / Create or Replace Group (deprecated 语义) + gap-endpoints §3.2 | groupPut | ✅ | PUT 全量替换 vs POST 增量不对称——带②核对行 | A | 高 | P0 |
| 15 | POST `/api/security/groups/{name}`（更新，**成员增量添加**） | Group Update | groupPost | ✅ | 同上 | A | 高 | P0 |
| 16 | DELETE `/api/security/groups/{name}` | Delete a Group + rbac-model.md §1.2.2（最后 admin 组守卫） | groupDelete | ✅ | — | A | 高 | — |
| 17 | GET `/api/security/permissions`（v1 列表，admin，{name,uri}） | Get Permission Targets（路径实取 `/security/permissions`）+ gap-endpoints §4 | —（BinFlow 走 `/api/v1/permissions`） | ◐ | **路径族缺位**：Artifactory 路径别名未挂（带②差异修复/别名行）；v1 无过滤参数语义按规格 | A | 高 | P0 |
| 18 | GET/PUT/DELETE `/api/security/permissions/{name}` | Get/Create or Replace/Delete Permission Target Details | —（同上） | ◐ | 同上（principals/ACE 模型 BinFlow 已有语义真身——路径 + wire 形态对齐） | A | 高 | P0 |
| 19 | `/api/v2/security/permissions` 族（GET 列表/详情/existence/PUT/DELETE + per-user/per-group 6 端点） | Get Permission Targets (V2) 等 8 条目 + gap-endpoints §4（v2 非 admin 可见语义） | — | ❌ | v2 面（repo/build/release-bundle 三域合并）dep build/bundle 域本体（M17 后段评估） | A | 高 | P2 |
| 20 | POST `/api/security/token`（create/refresh，form-encoded） | Create Token / Refresh Token（经典面；现行官方主推 `/access/api/v1/tokens`） | tokenCreate | ✅ | grant_type=client_credentials 形态；**expires_in/scope/access_token/refresh_token 字段集核对**（带②）——BinFlow 无 scope 体系（aql.md §6 登记远期） | A | 中 | P0 |
| 21 | POST `/api/security/token/revoke` | Revoke Token | tokenRevoke | ✅ | — | A | 高 | — |
| 22 | GET `/api/security/token`（token infos 列表） | Get Tokens（Access 侧 `/access/api/v1/tokens` 族） | — | ❌ | BinFlow token 列表走自有 session/console 面——Artifactory 形态缺位 | A | 中 | P2 |
| 23 | `/api/security/keypair` 族（GET/POST/PUT 集合 + GET/DELETE {pairName} + POST verify + GET public/repositories/{repoKey}） | Create/Get All/Update/Get/Delete/Verify Key Pair / Get Key Pair Public Key Per Repository | keypair×7 | ✅ | 字段集核对（passPhrase/impersonation 等官方字段）——带②核对行 | A | 中 | P0 |
| 24 | POST/DELETE `/api/v2/repositories/{repoKey}/keyPairs[/{keyName}]`（仓级关联） | Set/Delete Key for Repository（v2 形态） | keypairAssociate / keypairDisassociate | ✅ | 同上族 | A | 中 | — |
| 25 | POST `/api/v1/admin/security/keypair/generate` | —（官方 reference 无页——inv-2 §D 密钥对族旁证） | keypairGenerate | ✅ | 「此条补充官方规范」形态（反编译 + BinFlow 双在案） | A | 中 | — |
| 26 | API Key 族（create/regenerate/get/revoke/revoke-user ×5——`/api/security/apiKey`） | Create/Regenerate/Get/Revoke API Key ×5 | — | ❌ | legacy 凭据形态（设计上 token 替代——inv-2 §D 登记）；低优，候 PM 裁定转 ⛔ | A | 高 | P3 |
| 27 | GPG 族（set/get public、set private、passphrase + distribution GPG ×5） | Set/Get GPG Public/Private Key 等 | — | ❌ | 签名链 dep build/bundle 域深化（M18+ 联动 Q2） | A | 中 | P3 |
| 28 | 主/次签名密钥族（primary/secondary ×7——UI 签名面） | Set/Get/Delete Primary/Secondary Key 等 | — | ❌ | UI 证书签名面；低优 | A | 中 | P3 |
| 29 | GET/POST/DELETE `/api/system/certificates[/{alias}]`（远端 TLS 信任库） | Get/Add/Delete Certificate + inv-2 §D | — | ❌ | remote 仓出站 TLS 信任面；P2 | A | 中 | P2 |
| 30 | LDAP 配置族 | 官方现行主推 = `/access/api/v1/config/ldap` 六端点；Artifactory UI 内部面 = `/ui/api/v1/admin/security/ldap`（auth-integration.md） | authConfigLdapGet/Put/Test（BinFlow `/api/v1/admin/security/ldap`） | ◐ | BinFlow 自有 v1 形态（读写测试三件）——与 Access v2 及 UI 内部面均不同路径；语义核对 + 别名裁定归带②后续（M18+ SSO 补账 FR-151.3 联动） | A/C | 中 | P2 |
| 31 | SAML 配置族（settings + general + enable） | 官方 `/access/api/v1/saml` 六端点 + auth-integration.md UI 面 | authConfigSamlGet/Put/Test + key public ×3（BinFlow `/api/v1/admin/security/saml/*`） | ◐ | 同上形态注记 | A/C | 中 | P2 |
| 32 | OIDC/OAuth 族（config CRUD + identity mapping ×5 + token exchange） | Create/Get all/Update/Get/Delete OIDC Configuration ×5 + Identity Mapping ×4 + OIDC Token Exchange | authConfigOidcGet/Put/Test（BinFlow `/api/v1/admin/security/oauth`） | ◐ | BinFlow OIDC 已实现（FR-92）但走自有路径/形态；Access 侧 mapping 面缺位 | A/C | 中 | P2 |
| 33 | Crowd / HTTP SSO / Basic-auth 开关族 | Get/Create Crowd Configuration / Get HTTP SSO / Enable Basic Auth ×6 | — | ❌ | 外部目录/网关形态——低优候裁 | A | 中 | P3 |
| 34 | SCIM 族（`/access/api/v2/scim` Users/Groups ×12） | GET/CREATE Users (SCIM) 等 12 条目 | — | ❌ | Access 侧 SCIM 面；IdP 自动供给场景——M18+ 候 | A | 中 | P2 |
| 35 | Vault/AWS-IAM 族（secrets ×4 + IAM role ×3） | Set/Get/Test Vault ×4 + AWS IAM Role ×3 | — | ❌ | 外部密钥服务集成——候裁（物理依赖外部产品） | A | 中 | P3 |

> D04 ⛔ 计 0（全部按缺位/部分登记——BinFlow 无许可门口径）。超集行 1（行 8）。

---

## 6. D05 复制

> 依据：官方 replication 条目群 + replication.md（T-402/402a 全域规格）。BinFlow 已有 push 复制引擎（M6）+ M15 复制配置面——本域主体差异是**路径族形态**（BinFlow `/api/v1/*` 自有前缀 vs Artifactory `/api/replication*`）。

| # | 方法+路径（Artifactory） | 官方锚点 | BinFlow 现状 | 态 | 差异要点 | 层 | 置信度 | 优先级 |
|---|---|---|---|---|---|---|---|---|
| 1 | GET/PUT/POST/DELETE `/api/replications/{repoKey}`（单仓复制配置 CRUD） | Get/Set/Update/Delete Repository Replication Configuration | replicationList/Create/UpdateEnabled/Delete（`/api/v1/replications` 族） | ◐ | 路径族别名缺位；wire 模型（cron/events/pathPrefix 等字段集）已按 replication.md §9 对齐——别名行归 M18+（带外登记：复制域不在首三带） | A | 高 | P1 |
| 2 | GET `/api/replication/status`（异步任务状态） | Get Replication Status | replicationStatus（`/api/v1/replication/status`） | ◐ | 同上 | A | 高 | P1 |
| 3 | POST `/api/replication/execute/{repoKey}`（手动触发 push/pull） | Pull/Push Replication | replicationRun（`/api/v1/replications/{key}/run`） | ◐ | 同上；UI 触发面三端点（executereplicationnow 等）为 UI 内部面不挂 | A | 高 | P1 |
| 4 | POST `/api/replications/multiple/{repoKey}`（multipush 多目标） | Create or Replace Local Multi-push Replication / Update | — | ❌ | 多目标 push（ent 档语义）——M18+ HA/multipush 联动 | A | 高 | P2 |
| 5 | POST `/api/replications/enable|disable`（批量启停） | Enable or Disable Multiple Replications | — | ❌ | 批量启停形态 | A | 中 | P2 |
| 6 | GET `/api/replications/remote`（已注册远端仓清单） | Get Remote Repositories Registered for Replication | — | ❌ | smart remote 拓扑面 | A | 中 | P2 |
| 7 | 事件通道族 ×4（establish/teardown/close/propagate——官方标注 **[Internal]**） | Close/Establish/Tear down channel / Propagate replication events | — | ⛔ | 官方自标 Internal + smart replication 握手机制（inv-2 §2 特性协商）——远期 dep | D | 高 | — |
| 8 | GET `/api/system/replications`（全局配置） | Get Global System Replication Configuration | replicationGlobalBlockGet（`/api/v1/system/replications`） | ◐ | 读臂已有（路径别名）；blockPulls/blockPushes 两字段模型按 replication.md §B | A | 高 | P1 |
| 9 | POST `/api/system/replications/block` / `unblock` | Block/Unblock System Replication | replicationGlobalBlock/Unblock | ◐ | 同上（动作语义已实现——路径别名） | A | 高 | P1 |
| 10 | GET/PUT `/api/system/replication`（checksum replication 配置） | Get/Configure Checksum Replication | — | ❌ | 全局 checksum 复制旋钮 | A | 中 | P2 |
| 11 | （超集）POST `/api/v1/replications/test`（草稿测试）+ POST `/api/v1/replications/{key}/test` | —（Artifactory 公开面无对位；UI 内部 testlocal/testremotereplication 旁证 replication.md §C） | replicationTestDraft / replicationTest | C | BinFlow 自有测试面 | C | 中 | — |

---

## 7. D06 系统与运维

| # | 方法+路径 | 官方锚点 / 既有规格 | BinFlow 现状 | 态 | 差异要点 | 层 | 置信度 | 优先级 |
|---|---|---|---|---|---|---|---|---|
| 1 | GET `/api/system/ping`（免认证） | Artifactory Ping（路径实取）+ rest-api.md §5 | systemPing | ✅ | 免认证姿态核对（带①） | A | 高 | P0 |
| 2 | GET `/api/system/version` | Get Artifactory Version（路径实取） | systemVersion | ✅ | 字段集（version/revision〔/addons〕）核对（带①） | A | 高 | P0 |
| 3 | GET `/api/system/readiness` / `/api/system/liveness` | Readiness/Liveness Probe | —（BinFlow `/api/v1/health`） | ◐ | K8s 探针别名路径缺位（低危——health 语义已有） | A | 中 | P2 |
| 4 | GET `/api/system`（System Info——存储/HA 汇总） | Get System Info（路径实取 `/artifactory/api/system`） | — | ❌ | 带①扩张行（字段集 dep storageinfo——行 28 联动） | A | 高 | P1 |
| 5 | GET `/api/system/serverTime` | 经典条目（官方新索引未列——rest-api.md §5 在案） | — | ❌ | 带①扩张行（epoch 毫秒纯文本——trivial） | A | 中 | P1 |
| 6 | GET `/api/system/configuration` + POST（config.xml 描述符往返/脱敏） | Get System Configuration + config-formats.md | — | ❌ | 带①扩张行；BinFlow 配置体系自有（YAML/env）——C 层替代 or A 层别名候裁 | A/C | 中 | P1 |
| 7 | Apply YAML Configuration / Save General Configuration ×2 | Patch/Save System Configuration | — | ❌ | 平台化配置面；低优 | A | 中 | P3 |
| 8 | GET/POST/DELETE `/api/system/license` | Get License Information / Install License（路径实取） | licenseGet/Install/Delete | ✅ | BinFlow license 面为自有语义（license gate——license_gate.go）；wire 形态核对注记 | A/C | 高 | — |
| 9 | HA license 族 ×3（GET/install/delete HA cluster） | Get HA License Information 等 | — | ⛔ | HA 专属——M18+ HA 排程联动（Q3 已闭「进」） | A | 高 | — |
| 10 | Reverse proxy 族 ×4（get/update/snippet/possible-values） | Get/Update Reverse Proxy Configuration 等 + inv-2 §H（nginx.ftl 模板） | — | ❌ | 反代片段生成；P2（部署文档已有自有 Caddy 路径——候裁价值） | A | 高 | P2 |
| 11 | SHA256 迁移族 ×2（start/stop） | Start/Stop SHA256 Migration Task | — | ⛔ | BinFlow 原生 sha256——无迁移面（物理不适用） | D | 高 | — |
| 12 | POST `/api/system/metadata_server/reindex` | Re-index Paths on Metadata Server | — | ⛔ | JCR/企业 metadata server 组件依赖——候裁 | A | 中 | — |
| 13 | 日志族：GET/PUT/DELETE logger levels + `/api/system/logs`（tail） | Set/Delete/Get Logger Debug Levels + inv-2 §H（live-logs 内部面 `system/logs/config\|data` 另见 inv-1-core「Live Logs」——中置信） | systemLogsGet（**BinFlow 形 GET `/api/v1/system/logs`**：`?limit=` 1..1000 尾随 + `?filter=` 服务端子串 + `?download=1` 附件——T-493/FR-157③；system:read 门；源=进程自身日志流的环形缓冲〔internal/console/logtail.go〕，12-factor stderr 无日志文件场景下的诚实读面） | ◐ | **进程日志尾随/过滤/下载真身已落（T-493，2026-09-07）**——T-459「审计承载+端点缺位」契约漂移解除（FE 消费切换归 T-494，文档注记反转归 T-518）；残余：① logger levels GET/PUT/DELETE 三动词缺位；② A 层路径 `/api/system/logs` 别名未挂（BinFlow 走 v1 系统面惯例——与行 17/18 backups/maintenance 的别名缺位同族，归带① T-504 核定）；③ Artifactory live-logs 的 config/data 两面与 Service/Node/LogFile 选择器形态不复刻（内部 UI 面、中置信） | A/C | 高 | P1* |
| 14 | GET `/api/system/service_id` | Get Service ID | — | ❌ | 低优 | A | 中 | P3 |
| 15 | POST `/api/system/configuration/baseUrl`（custom URL base） | Update Custom URL Base | — | ❌ | baseUrl 配置面（BinFlow env 化 T-478 联动——形态候裁） | A | 中 | P3 |
| 16 | 导入导出族：POST `/api/import/system|repo`、`/api/export/system|repo`（含 metadata） | Full System Import / Export System / Import·Export Repository Content + import-export-api.md | —（bf-migrate CLI 自有载体） | ❌ | REST 化导入导出面缺位（CLI 已有等价能力）——P2 候 | A | 高 | P2 |
| 17 | 备份族：GET/PUT/DELETE `/api/system/backup/{key}`（+列表） | 经典条目（官方新索引未列）+ inv-2 §H + cron-scheduling.md | backups 族（BinFlow `/api/v1/system/backups`——GET/PUT 集合+单键×2、DELETE） | ◐ | **路径族别名缺位**（带①行）；cron 语义/导出目录已按 cron-scheduling.md 对齐 | A | 中 | P0 |
| 18 | 维护模式族：GET/PUT `/api/system/maintenance`（窗口设置） | 经典条目 + inv-2 §H | maintenanceGet/Put（`/api/v1/system/maintenance`） | ◐ | 路径别名缺位（读改两态） | A | 中 | P1 |
| 19 | GET/POST/DELETE `/api/v1/system/query_rate_limiter/config` | 官方无页——aql.md §14.4（反编译全量锚） | qrlConfig×3 | ✅ | 三态语义/文案按 aql.md §14.4（「补充官方规范」） | A | 中 | — |
| 20 | （超集）GET `/api/v1/system/settings` | — | systemSettingsGet | C | BinFlow 自有系统设置面 | C | 高 | — |
| 21 | （超集）GET `/api/v1/system/schedules` | —（Artifactory 调度无统一 REST 面——cron 分散在各配置） | schedulesList | C | BinFlow cron 调度域载体（ADR-0044） | C | 高 | — |
| 22 | （超集）GET/POST `/api/v1/system/cleanup` + POST `/api/v1/system/gc` | —（对位语义见行 26/27/30 的 Artifactory 缺位行） | cleanupStatus/cleanupRun/systemGC | C | 自有路径形态（Artifactory 对位路径见 D01 行 26–30） | C | 高 | — |
| 23 | （超集）GET `/api/v1/session`（whoami/登入/登出） | —（Artifactory 等价 = UI 登录面 `/ui/api/v1/security/auth`——内部面） | session×3 | C | BinFlow 自有会话面 | C | 中 | — |
| 24 | （超集）GET `/api/v1/audit` | —（Artifactory 审计走日志下载/UI 内部面） | auditQuery | C | BinFlow 自有审计查询面 | C | 中 | — |
| 25 | （超集）GET `/api/v1/health` + GET `/api/v1/addons` | — | healthGet / addonsList | C | health 对位见行 3；addons 无官方对位页（Artifactory addons 经 `/api/system/version` 回显）——形态候核 | C | 中 | — |
| 26 | 加解密族：POST `/api/system/encrypt|decrypt` + crypto{action} | Activate/Deactivate Artifactory Key Encryption + inv-2 §H | — | ❌ | 全库敏感字段加密（master key）；P3 | A | 高 | P3 |
| 27 | Support Bundle 族 ×5（create/list/get/delete/metadata） | Create Support Bundle 等 | — | ❌ | 诊断包收集；P2 | A | 高 | P2 |
| 28 | 集群锁族：GET cluster/node locks ×2 | Get Cluster/Node Locks | — | ⛔ | HA 分布式锁——单机不适用（M18+ HA 联动翻转） | A | 高 | — |
| 29 | SHA2/HA 迁移、Load Healer、traffic、debug 族 | inv-2 §H（多入口） | — | ❌ | 运维杂项族——P3 级联登记 | A | 中 | P3 |

---

## 8. D07 Build-info 域（T-488 对账——指针行，不重写）

> **零重复声明**：本域行为规格已由 `docs/reverse/build-info.md`（T-488，2026-09-06 冻结）承载——端点族子集表（§1，一手 OpenAPI + 官方参考页双源高置信）。本矩阵只登记对账态与实现归属，**不复制行为条文**。

| # | 端点（build-info.md §1 行对位） | 实现归属 | 态 | 层 | 置信度 |
|---|---|---|---|---|---|
| 1 | GET `/api/build`（名清单+lastStarted） | T-505+（FR-152 链） | ❌（M17 在途） | A | 高 |
| 2 | PUT `/api/build`（全量上传，name/number 在 body） | 同上 | ❌ | A | 高 |
| 3 | GET `/api/build/{buildName}`（号清单） | 同上 | ❌ | A | 高 |
| 4 | DELETE `/api/build/{buildName}`（buildNumbers/artifacts/deleteAll） | 同上 | ❌ | A | 高 |
| 5 | GET `/api/build/{buildName}/{buildNumber}`（详情，started/diff 消歧） | 同上 | ❌ | A | 高 |
| 6 | POST `/api/build/append/{name}/{number}`（模块数组合并） | 同上 | ❌ | A | 高 |
| 7 | POST `/api/build/promote/{name}/{number}`（状态机+迁仓） | 同上 | ❌ | A | 高 |
| 8 | POST `/api/build/delete`（批删——特殊字符号） | 同上 | ❌ | A | 高 |
| 9 | POST `/api/build/rename/{buildName}` | **M17 面外**（ADR-0045 点 6） | ❌（面外登记） | A | 高 |
| 10 | POST `/api/build/retention/{buildName}` | T-508+（FR-152 链） | ❌ | A | 高 |
| 11 | POST `/api/docker/{repoKey}/v2/promote`（镜像晋升） | M17 面外（经 build promote targetRepo 直达——ADR-0045 点 6） | ❌（面外登记） | A | 高 |

联动行（他域已登记）：`/api/search/buildArtifacts`、`/api/search/dependency`（D03 行 16/17）；`POST /api/archive/buildArtifacts`（D01 行 17）；AQL builds/modules/dependencies 入口（D03 行 1 注记）。

## 9. D08 Release Bundle / Distribution 域（T-488 对账——指针行）

> **零重复声明**：`docs/reverse/release-bundle.md`（T-488 冻结）承载全量行为——Artifactory 源侧 18 端点 + Distribution 侧 v1/v2 索引 + 冲突三态 + 状态机。此处只登记对账态。

| # | 端点族（release-bundle.md §1 对位） | 实现归属 | 态 | 层 | 置信度 |
|---|---|---|---|---|---|
| 1 | POST `/api/release/bundle`（AQL 装配清单） | T-513（FR-153 最小面——显式清单降级形态已裁） | ❌（M17 在途） | A | 高 |
| 2 | 事务三段式（transaction/open、close ×2、async status） | M17 面外（v2 signing——Q2 出口②） | ❌（面外登记） | A | 高 |
| 3 | PUT `/api/release/store`（源 bundle 承接，202/200/409 三态） | M17 面外（Distribution 推送对端） | ❌（面外登记） | A | 高 |
| 4 | 查询族（GET `/release/bundles[/{name}[/{version}[/status]]]`、HEAD 校验和、DELETE 两行） | T-513 最小面 | ❌（M17 在途） | A | 高 |
| 5 | GET/PUT `/api/release/bundles/config` + artifacts 清单 + fat_manifest | M17 面外（admin 深度面） | ❌（面外登记） | A | 高 |
| 6 | Any Distribution 权限桶（`ANY DISTRIBUTION` 预置） | T-491/T-513（FR-156.2 同场） | ❌（M17 在途） | A/C | 高 |
| 7 | Distribution 服务侧 v1/v2 族（≈60+ 操作：创建/签名/分发/边缘/air-gap/GPG/维护） | **⛔ 产品界外**（独立部署单元——Q2 出口②候裁翻转） | ⛔ | D | 高 |

## 10. D09 Webhook

| # | 方法+路径 | 官方锚点 / 既有规格 | BinFlow op | 态 | 差异要点 | 层 | 置信度 | 优先级 |
|---|---|---|---|---|---|---|---|---|
| 1–6 | GET/POST `/event/api/v1/subscriptions`、GET/PUT/DELETE `/{key}`、POST `/test` | 官方 integrations reference 六条目 + webhook.md | webhook×6 | ✅ | 201/204 码位与模型按 webhook.md（M13 落地） | A | 高 | — |
| 7 | GET `/event/api/v1/troubleshooting`（流式/历史双模） | Stream/Fetch Webhooks Troubleshooting Data | webhookTroubleshooting | ✅ | 参数族（subscription/target/start/end/count/start=0）按 webhook.md §7 | A | 高 | — |
| 8 | Replay/死信重放行级 REST（outbox 行查询+重放） | —（Artifactory 无公开对位） | — | ❌ | BinFlow 自有增强（FR-159.2——C 层目标） | C | 高 | P1 |
| 9 | legacy `/access/api/v1/system/webhooks*` | webhook.md V6 登记 | — | ⛔ | deprecated 并存面——不实现 | D | 中 | — |

## 11. D10–D14 族级登记（逐端点展开候对应程次）

| 域 | 官方/反编译锚点 | 规模 | 态 | 说明 | 层 | 置信度 |
|---|---|---|---|---|---|---|
| D10 用户插件（`/api/plugins` 执行/源码/info/staging/promote/reload ×13 操作） | 官方 integrations reference 7 条目 + inv-2 §I | 13 op | ❌ | Groovy 服务端脚本体系——实现量大且引入脚本执行面；候 PM 裁定（倾向不做→转 ⛔ 需明示） | A | 高 |
| D11 生命周期治理：清理策略 v2（packages/builds/bundles 24 条目）、保留 v2（retention ×13 + archive policies ×11）、冷存储归档（smart archiving ×30、archive/v2 ×10）、retentionTools | 官方 administration reference 条目群 + inv-2 §F | ≈80 op | ❌ | **M18+ Lifecycles 专程**（Q1 裁定滚 M18——与 T-519 同窗）；BinFlow GC/cleanup/TRASH 为基础层 | A | 高 |
| D11-C Curation（interface settings ×2 + 官方 curation 族） | 官方 + inv-2 §F | 2+ op | ⛔ | xray_tied（远端包拦截审计服务）——D14 同族 | D | 高 |
| D12 协议面（见 §11.1 子表） | 各包型官方 calc 条目 + 协议规格 | 55+ op | 混合 | 子表 | A | 按行 |
| D13 UI 内部面（`/ui/api/v1` 树浏览/Tab/制品动作/首页 widget/验证/SetMeUp/登录屏 ≈470 op） | inv-2 §1.K + console-ui.md | ≈470 op | ⛔ | UI 内部契约——BinFlow 自有控制台对位（M16 全前端对齐已完成）；不进 REST 兼容程 | D | 高 |
| D14 外部产品面 | — | — | ⛔ | Xray 集成族（漏洞/合规/license 识别）/ Pipelines / JCR 订阅 / Mission Control+Grid+JPD（≈25）/ Bridge（≈19）/ JFConnect（7）/ Workers（14）/ Access 平台联邦（≈15）/ 云专属族（signed URL、private link、SSL objects、IP allowlist、NAT/regions、MyJFrog、server list ≈19）/ Distribution 服务侧（≈60——D08 行 7）/ Curation / Bintray（sunset） | D | 高 |

### 11.1 D12 协议面子表（BinFlow 13 包型 × Artifactory 57 包型）

| # | 面族 | 官方锚点 / 规格 | BinFlow op | 态 | 差异要点 | 层 | 置信度 |
|---|---|---|---|---|---|---|---|
| 1 | Docker Registry v2（`/v2/` 全套 16 op：blob/manifest/uploads/catalog/tags/token） | Docker Registry v2 HTTP API 官方规范 + docker-registry.md | docker×16 | ✅ | 官方规范为基准（反编译只补空白）；v1 兼容面（DockerSubResource 26 op）不实现（deprecated） | A | 高 |
| 2 | Docker API 面（`/api/docker/{repoKey}` promote/listTag/UI 面族） | inv-2 §1.J + build-info.md §2.6 | — | ◐ | promote 端点随 D07 行 11 裁定；listTag 族缺位 | A | 高 |
| 3 | npm 认证族（`/-/ping`、`/-/user/org.couchdb.user:*` ×2、`/-/v1/login`、`/-/whoami`） | npm.md（客户端源码即规范 + live 对拍） | npm×5 | ✅ | K60 六条定案在册 | A | 高 |
| 4 | npm 包面（packument GET/PUT、unpublish ×2） | maven-npm-pypi.md §2 | npm×4 | ✅ | rev-dance 不变量按规格 | A | 高 |
| 5 | npm dist-tags 族（`/-/package/:pkg/dist-tags` GET/PUT + 版本 CRUD 子集——inv-2 计 42 op 全集） | inv-2 §1.J | — | ◐ | dist-tags 显式端点族缺位（部分语义经 packument PUT 旁路）——候扩张票 | A | 中 |
| 6 | PyPI（simple 索引/包页、packages 直下、upload） | PEP 503 + maven-npm-pypi.md §3 | pypi×4 | ✅ | — | A | 高 |
| 7 | PyPI JSON 元数据（latest/version in JSON ×2 官方条目） | Get PyPI Package Version Metadata in JSON ×2 | — | ❌ | `/api/pypi/{repo}/pypi/{pkg}/json` 族——低优 | A | 中 |
| 8 | Maven 元数据计算（POST `/api/maven/calculateMetadata/{repoKey}/{path}` + generatePom + 索引触发） | Calculate Maven Metadata / Generate Maven POM File / Calculate Maven Index + maven-npm-pypi.md §1 | — | ◐ | 自动重算已有（上传链路）；显式 REST 触发缺位 | A | 高 |
| 9 | 重索引族已实现 6 件：conan ×2、deb、helm ×2、yum | Calculate Conan/Debian/Helm/YUM Repository Metadata | reindex×6 | ✅ | async 语义按各包型规格（conan.md/debian.md/helm.md/rpm.md） | A | 高 |
| 10 | 重索引族缺位：alpine、cocoapods（bower）、nuget、npm、cran、cargo、conda、pub、rubygems、swift、terraform、opkg、vagrant | 官方 calc 条目群 | — | ❌ | BinFlow 已有包型（nuget/cargo/goproxy 等）的 calc 面逐包补；未有包型随包型扩张 | A | 中 |
| 11 | goproxy（`/api/go/...` 无独立 calc 面——模块协议原生） | goproxy.md | （存储面直答） | ✅ | GOPROXY 协议端点在内容面（`/{repoKey}/...`）承载 | A | 高 |
| 12 | VCS 族（git 仓浏览/下载 ×17 官方条目） | Get VCS Tags/Branches/Refs 等 + inv-2 §H | — | ❌ | git 远端当仓——低优候裁 | A | 中 |
| 13 | Puppet 族 ×4 | Get Puppet Modules/Releases | — | ❌ | 未有包型 | A | 中 |
| 14 | RubyGems 版本清单 | Get RubyGem Version List | — | ❌ | 未有包型 | A | 中 |
| 15 | 其余 40+ 包型协议栈（terraform backend、HuggingFace、swift、pub…） | inv-2 §1.J（57 包型清单） | — | ❌ | 包型扩张程载体（PRODUCT 范围内——按需排程） | A | 高 |
| 16 | 迁移器 `/v1/migrations` 族 ×13 | inv-2 §1.J | — | ❌ | bf-migrate CLI 等价——候裁 | A | 中 |

> D12 统计并入 §1 表（✅21：docker16+npm9 中对位 9 计入 +pypi4+reindex6+goproxy1 超集折算——按子表行计 21/◐2/❌5+族/⛔1〔v1 deprecated〕）。

---

## 12. 首程三带行集冻结（T-504/505/506 输入——2026-09-06 冻结 v1）

> 排序纪律：**差异修复行（型=R）优先于新端点扩张行（型=X）**。三带互不重叠（带①= D02+D06 面，带②= D01+D04 面，带③= D03 面）；D05/D07~D12 不进首程。每行目标层级全为 A（对齐官方全集——BinFlow 无许可门口径），置信度沿 §0 定义。

### 带① repositories + system（解锁 T-504）

| # | 端点（方法+路径） | 型 | 现态→目标 | 置信度 | 冻结依据 |
|---|---|---|---|---|---|
| 1-1 | GET `/api/repositories`（`url` 补 `/binflow` 前缀 + type/packageType 过滤行为 + project 参数容忍语义裁定） | R | ◐→✅ | 高 | FR-157① + gap-endpoints §5.1 + D02 行 1 |
| 1-2 | PUT/POST `/api/repositories/{key}`（configJSON 四域 round-trip：maxUniqueSnapshots/repoLayoutRef/blackedOut/archiveBrowsingEnabled + Stage 域 + GET 回显 + blackedOut 拒写联动） | R | ◐→✅ | 高 | FR-156① + D02 行 3/4 |
| 1-3 | GET `/api/repositories/{key}`（回显域与 404 姿态随 1-2 复核） | R | ✅（核对） | 高 | D02 行 2 |
| 1-4 | POST `/api/system/storage/gc`（Artifactory 路径别名挂载；`/api/v1/system/gc` 并行保留） | R | ◐→✅ | 高 | D01 行 26（官方 OpenAPI 路径实取） |
| 1-5 | GET/PUT/DELETE `/api/system/backup/{key}` + 列表（Artifactory 路径族别名；`/api/v1/system/backups` 并行保留） | R | ◐→✅ | 中 | D06 行 17（经典路径字面待复核） |
| 1-6 | GET `/api/system/ping` + `/api/system/version`（免认证姿态 + version/revision 字段集核对） | R | ✅（核对） | 高 | D06 行 1/2 |
| 1-7 | GET `/api/repositories/configurations`（admin 全量配置，按 type 分组排序） | X | ❌→✅ | 高 | D02 行 6 |
| 1-8 | GET `/api/repositories/existence`（type/project 探测） | X | ❌→✅ | 高 | D02 行 9（路径实取） |
| 1-9 | GET `/api/v2/repositories/{key}`（v2 配置读） | X | ❌→✅ | 中 | D02 行 7 |
| 1-10 | `/api/v2/repositories/batch` 族（GET 批读先行；POST/PUT/DELETE 批写次波） | X | ❌→✅ | 中 | D02 行 8 |
| 1-11 | GET `/api/system`（System Info 汇总） | X | ❌→✅ | 高 | D06 行 4（路径实取） |
| 1-12 | GET `/api/system/serverTime`（epoch 毫秒纯文本） | X | ❌→✅ | 中 | D06 行 5 |
| 1-13 | GET/POST `/api/system/configuration`（descriptor 往返——**裁定点：A 层别名 vs C 层替代**，票内留痕） | X | ❌→✅/裁定 | 中 | D06 行 6 |

### 带② storage + security（解锁 T-505）

| # | 端点 | 型 | 现态→目标 | 置信度 | 冻结依据 |
|---|---|---|---|---|---|
| 2-1 | GET `/api/storage/{repoKey}/{path}?propertiesXml`（实现 or 维持 501 + 留痕裁定） | R | ◐→✅/裁定 | 中 | D01 行 3 |
| 2-2 | GET `...?lastModified`（目录最新修改项 + Last-Modified 头） | R | ◐→✅ | 中 | D01 行 4 |
| 2-3 | GET `...?list` 参数族补齐（deep/depth/listFolders/mdTimestamps/includeRootPath） | R | ◐→✅ | 中 | D01 行 5 |
| 2-4 | GET `/api/security/users` 列表形态核对（数组×{name,uri,realm} 闭集 + 无过滤参数） | R | ✅（核对） | 高 | D04 行 1 + gap-endpoints §1.1 |
| 2-5 | GET `/api/security/users/{name}` 回显字段集对齐（`status` 枚举闭集 / 无 `enabled` 布尔 / `lastLoggedIn` 条件出现 / `groups` 空集形态） | R | ◐→✅ | 高 | D04 行 2 + gap-endpoints §1.2 |
| 2-6 | 组成员写语义核对（PUT=全量替换 / POST=增量添加 不对称 + `?includeUsers`） | R | ✅（核对） | 高 | D04 行 13–15 + gap-endpoints §3 |
| 2-7 | `/api/security/permissions` v1 族别名挂载（GET 列表 + GET/PUT/DELETE `{name}`；`/api/v1/permissions` 并行保留） | R | ◐→✅ | 高 | D04 行 17/18 + gap-endpoints §4 |
| 2-8 | POST `/api/security/token` 响应字段集核对（access_token/refresh_token/expires_in/scope 形态） | R | ✅（核对） | 中 | D04 行 20 |
| 2-9 | keypair 族字段集核对（passPhrase/impersonation 等官方字段面） | R | ✅（核对） | 中 | D04 行 23 |
| 2-10 | POST `/api/storage/{repoKey}/{path}?recursive&atomic`（Update Item Properties 增量语义——三动词补全） | X | ❌→✅ | 高 | D01 行 8 |
| 2-11 | GET `/api/security/encryptedPassword`（单端点） | X | ❌→✅ | 中 | D04 行 11 |
| 2-12 | GET `/api/storage/{path}` SHA256 属性回填端点（Set Item SHA256 Checksum） | X | ❌→✅（低优可裁） | 中 | D01 行 21 |
| 2-13 | archive entry 抽取（`!` 归档内单文件语法） | X | ❌→✅ | 中 | D01 行 16 |
| 2-14 | 密码过期族 + 用户锁定族（家族级——容量余量条款：满带可滚次波） | X | ❌→✅ | 中 | D04 行 9/10 |

### 带③ search + 老搜索（解锁 T-506）

| # | 端点 | 型 | 现态→目标 | 置信度 | 冻结依据 |
|---|---|---|---|---|---|
| 3-1 | GET `/api/search/artifact`（K64①：大小写不敏感核对——BinFlow 现值若区分则对齐） | R | ✅→✅（校准） | 高 | aql.md §0-5 + D03 行 2 |
| 3-2 | 老搜索 envelope uri-only 瘦行口径复核（E-09 超集差异——维持留痕 or 瘦行翻正，票内裁） | R | ✅（裁定） | 高 | aql.md §8.3 + D03 行 5 |
| 3-3 | GET `/api/search/prop`（任意查询参数即属性键语义 + 键无值=`*`） | R | ✅（核对） | 高 | aql.md §8.2 行 3 + D03 行 4 |
| 3-4 | GET `/api/search/usage`（对外参数名 `notUsedSince` 面复核——PRD usageSince 笔误警示） | R | ✅（核对） | 高 | aql.md §14.2 命名口径警示 |
| 3-5 | dates/creation（`from` 必填 400 逐字 `'from' parameter cannot be empty!` + maven-metadata.xml 恒排除 + 404 空集族） | R | ✅（核对） | 高 | aql.md §14.3 |
| 3-6 | GET `/api/search/pattern`（`**` 拒绝行为 + repos 参数） | R | ✅（核对） | 中 | aql.md §8.2 |
| 3-7 | GET `/api/search/versions`（版本全列 + `remote=1` 联动远端列举） | X | ❌→✅ | 高 | D03 行 10 |
| 3-8 | GET `/api/search/latestVersion`（maven-2 默认布局排序先行——K73 布局引擎注记） | X | ❌→✅ | 高 | D03 行 11 |
| 3-9 | GET `/api/search/latestVersionByProperties` | X | ❌→✅ | 中 | D03 行 12 |
| 3-10 | GET `/api/search/badChecksum` | X | ❌→✅ | 中 | D03 行 13 |
| 3-11 | GET `/api/search/archive`（deprecated 条目——容量余量条款） | X | ❌→✅（低优可裁） | 中 | D03 行 14 |
| 3-12 | （对账行，不进实现）`/api/search/dependency` + `/api/search/buildArtifacts` → **T-511 承载**（FR-152 联动，wire 锚 aql.md §15.4 已冻结） | — | ❌（在途） | 高 | D03 行 16/17 |
| 3-13 | （不适用登记行）`/api/search/license` ⛔ Q9 xray_tied——缺位登记不伪造 | — | ⛔ | 高 | D03 行 15 + M17 PRD Q9 |

---

## 13. 后续程 backlog registry（M18+ 滚程载体——按域分批）

| 批次 | 域/面 | 行集来源（本矩阵行号） | 承载程 |
|---|---|---|---|
| B-18a | Federation 专程（D02 行 11 + D14 Access 联邦面） | ≈19+15 端点 | M18（Q1 裁定 + T-519 registry 段） |
| B-18b | Lifecycles 专程（D11 清理/保留/归档 ≈80 op） | D11 | M18+（与 B-18a 同窗候裁） |
| B-18c | HA 专程（D06 行 9/28 + multipush D05 行 4 + cluster 面） | D05/D06 ⛔ 族翻转 | M18+（Q3 已闭「进」） |
| B-R2 | 复制路径族别名 + multipush/批量启停/checksum 复制（D05 行 1–6/8/10） | ≈10 端点 | M18 首批候 |
| B-R3 | 系统长尾：reverse proxy、support bundle、导入导出 REST 化、维护模式别名、readiness/liveness 别名、service_id、baseUrl（D06 行 3/10/14/15/16/18/27） | ≈15 端点 | M18+ 分批 |
| B-R4 | 安全长尾：v2 permissions、SCIM、密码过期/锁定族（若带②滚出）、API key 候裁、GPG、签名密钥、证书、Crowd/HTTP-SSO、Vault/IAM（D04 行 9/10/19/26–35） | ≈40 op | M18+ 分批 |
| B-R5 | 存储长尾：PUD 三态、compact、zap、sync download、exploded archive、flat copy/move、folderDownload 配额门、archive(buildArtifacts)（D01 行 14/17–21/27/30） | ≈10 端点 | M18+ 分批 |
| B-R6 | 包型扩张程：重索引缺位族 + dist-tags + 包型协议栈 40+（D12 行 7/8/10/12–16） | 视包型排程 | 按需（PRODUCT 范围内） |
| B-R7 | 用户插件程（D10——候 PM 裁定转 ⛔） | 13 op | 候裁 |
| B-R8 | Distribution 深度面（D08 行 2/3/5/7——Q2 出口②翻转条件在册） | ≈60 op | 候用户明示推翻 Q2 |

## 14. 与 T-488 的零重复对账（AC3）

| 对账项 | 结论 |
|---|---|
| Build-info 域 | 本矩阵 D07 全部 11 行 = **指针行**（指向 build-info.md §1 端点族子集表），零行为条文复制；联动行（D01 行 17、D03 行 1/16/17）同样只登记归属 |
| Release Bundle 域 | D08 全部 7 行 = 指针行（指向 release-bundle.md §1 源侧 18 端点表 + §4 深度边界）；Distribution 服务侧整族 ⛔ 归 D14 |
| 搜索域交集 | aql.md §15.4（buildArtifacts/dependency wire 锚）与 build-info.md §4 为双挂锚——本矩阵 D03 行 16/17 直接引用，不展开 wire 细节 |
| 老搜索计数 | 本矩阵采用 aql.md §0-3 定案（14 子资源 + 2 外挂 = 16 全量口径）；license 行按 Q9 ⛔ |
| 结论 | **零重复**：D07/D08 段无任何一条行为描述性条文（全部为对账态 + 归属）；tech-lead 拆票时 build/bundle 域行为输入一律取 T-488 两规格 |

## 15. tech-lead 就绪度自评（解锁 T-504/505/506）

| 就绪项 | 状态 |
|---|---|
| 三带行集冻结 | ✅ §12（带① 13 行 / 带② 14 行 / 带③ 13 行——各在 8~15 区间；差异修复行全部前置于扩张行） |
| 逐行置信度 + A 层目标 | ✅ 每行标注；带内低置信行（1-5/1-9/1-10/2-1/2-11~2-14/3-9~3-11）已注明单源/待复核属性 |
| BinFlow 现状底稿可机读 | ✅ fern/openapi/binflow.json 158 ops 全部落位（§1 尾注） |
| 差异修复行与既有 FR 的边界 | ✅ 带① 1-1/1-2 与 FR-156/157 已裁票**不重叠**（本矩阵行 = 行为核对层；FR 票 = 实现层——tech-lead 派单时注明消费关系，避免双改）；2-13 等新行无既有归属 |
| 待验证面 | §16 清单 9 条——全部为「路径字面/文案逐字」级，不阻塞派单（可先派高置信行，低置信行随活体基线修复〔Q10 出口①〕后钉死） |
| 活体基线依赖 | 无硬依赖（纯书面基线）；若 Q10 出口①修复达成，带内中置信行可升级——建议 tech-lead 在 T-504/505/506 验收段预留活体对拍腿 |

## 16. 待验证清单（低置信/待复核——零静默升格）

| # | 项 | 现值依据 | 验证途径 |
|---|---|---|---|
| V-1 | `/api/flat/copy|move`、`/api/tasks`、`/api/system/storage/compact`、`/api/system/backup/{key}`、`/api/system/maintenance` 的路径字面 | 经典条目/inv-2 单源（官方新索引无页） | 活体基线修复后逐条 curl 探测（带 GET 优先） |
| V-2 | `?list` 参数族（mdTimestamps/includePropertiesMd5 等）与 `?propertiesXml` 的 BinFlow as-built 子集 | 官方页 vs storage.go 实现面 | 带②票内核对 + 官方 OpenAPI 参数表 |
| V-3 | batch 族四方法 + existence 的 POST 形态 | GET 批读路径已实取；方法族推断 | 官方四页 OpenAPI 逐页取（或活体） |
| V-4 | keypair/token/users 字段级官方 schema | 反编译 + 部分官方页 | 带②票内逐页取官方 OpenAPI schema |
| V-5 | `/api/system`（System Info）响应字段集 | 路径已实取、字段集未取 | 官方页 OpenAPI |
| V-6 | addons 回显面（`/api/v1/addons` 无官方对位页——Artifactory 以 version.addons 回显） | BinFlow 自有形态 | 活体对拍 version 响应字段 |
| V-7 | npm dist-tags 缺位面的真实客户端依赖度（npm CLI 是否走显式端点） | inv-2 计数 | npm.md 客户端源码即规范路径补拍 |
| V-8 | replication 五行路径族的 wire 模型对齐度（字段集未逐项对拍） | replication.md §9 已冻结、别名未挂 | B-R2 批次票内核对 |
| V-9 | 本矩阵行数统计（§1 表）为手工 tally | §2–§11 各表行计 | tech-lead 收编时抽查（不影响行集有效性） |

## 变更日志（活体 registry）

| 日期 | 变更 | 票 |
|---|---|---|
| 2026-09-06 | 初版建册：205 行四态对账 + 三带行集冻结 v1 + backlog registry + T-488 零重复对账 | T-503 |
| 2026-09-07 | FR-157 勘误族三行翻新：D02 行 1（url 前缀已落①）+ D01 行 1（downloadUri 处置=归位）+ D06 行 13（❌→◐ System Logs 进程日志端点真身）——带① 1-1 行的 R 型差异项①由 T-493 清偿，project 参数等余项仍归 T-504 | T-493 |
