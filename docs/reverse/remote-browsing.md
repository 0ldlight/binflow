# remote 仓远端浏览 行为规格（M16 FR-147 前置锚，T-435）

> **成稿来源**：T-425 评估票 §1/§2 直接成稿（conductor 编排确认口径——M16-SPLIT §1.2 T-435 AC2），素材 = JFrog 官方《Remote Repositories》文档（2026-09-02 实取，71KB markdown 全量比对）+ docs/reverse/repo-semantics.md §7.1/§7.2/§7.6/§8.5 + T-425 §1.4 t226 探针实录。实现票消费面 = T-442（三型回源枚举 + remote Test）/ T-448（可选档接线 + §8.5 口径扩面 + 降级）/ T-461（FE 树消费）。
>
> **取证档位注意**：远端浏览的**建仓配置面在 t226（OSS 7.84.10）被 Pro 许可门挡**（T-425 §1.4：REST 建 remote/local 仓一律 400 "This REST API is available only in Artifactory Pro"）——「开档后的 UI/树形态」**无活体对拍**，本规格该部分为**官方文档单源**（置信度中，附注留痕；补拍腿登记见 §7）。BinFlow 无许可门，按官方文档全集口径实现即可。

## 1. `listRemoteFolderItems` 可选档语义（置信度：高——官方文档 + repo-semantics §7.1 双源）

| 项 | 值 |
|---|---|
| 字段名 | `listRemoteFolderItems`（UI 名「List Remote Folder Items」/「List Remote Artifacts」——官方两种拼写并用） |
| 默认值 | **false**（BinFlow T-406 as-built「remote 浏览 = 仅缓存行」即此默认档同形态——**不欠默认 parity，欠可选档**） |
| off 行为 | 目录浏览（simple 与 list 两模式）只呈现本地缓存行；不接触上游 |
| on 行为 | 目录浏览时**合并远端（未缓存）条目**进树/列表 |
| 缓存语义 | 官方字段描述逐字要点：「远端内容**按 Metadata Retrieval Cache Period 的值缓存**」——远端列表属 metadata TTL 缓存族（BinFlow 对应面 = remote metadata TTL 600s 默认 + metadata 缓存行，语义可直接映射） |
| 附加声明 | 官方：「此设置是依赖远端目录内容信息的动态解析所必需」——即某些客户端语义（如按目录探测版本）要求开档 |

## 2. 官方支持面与机制声明（置信度：高——官方文档直读，2026-09-02 全量比对 21 小节）

**该设置只在五个包型小节出现**（官方《Remote Repositories》页「Additional Remote Repository Settings for Specific Package Types」节）：

| 官方开放该设置的包型 | BinFlow 13 型对位 |
|---|---|
| **Debian / Generic / Maven(Gradle/Ivy/sbt) / Opkg / RPM** | deb / generic / maven 三型在册；**Opkg 不在 BinFlow 包型集**（缺位登记，不伪造）；其余（helm/docker/npm/pypi/nuget/go/cargo/conan/helmoci）官方**未**开放该设置 |

- 官方专节《Browse Remote Repositories》机制声明（引文要点）：远端浏览**取决于上游资源是否支持浏览**——正例 **Maven Central 支持**（Apache 式目录索引服务器），反例 **Docker Hub 不支持**（v2 API 无可用 catalog 根树）。配图强调图中包**未缓存**——浏览面直接呈现上游未缓存条目。
- 机制推断（置信度中——官方未写算法，仅正反例）：开档枚举 = **抓取/解析上游目录索引/元数据文档**，不是通用协议能力。由此推论：官方开放的 5 型全部是「上游 = 纯 HTTP 文件树」族；协议型上游（registry API 族）无根级枚举，官方干脆不开设置。
- **BinFlow 做官方未开放型（helm/deb/rpm 中 helm 一型）= 超越官方设置面的 L2 自有增强**（deb/rpm 官方有开）——PRD FR-147 / M15 Q4 出口 C（批 1 = helm + deb + rpm）已裁，此定性差异如实登记：helm 腿为 L2 增强、deb/rpm 腿为可选档对齐。

## 3. 13 包型上游枚举能力矩阵（T-425 §2 原表；L27/K66 主体）

列说明：**枚举形态** = 上游能给出什么清单、在哪个粒度；**BinFlow 缝** = 现有可复用代码面（M15 时点锚，文件级锚点以 T-425 走读为准）；**估级** = 若做远端浏览的增量实现量（S/M/L，「不可行」= 协议无根级枚举）。置信度 = 该行主要判断的规格强度。

| # | 包型 | 上游廉价枚举 API | 枚举形态 | 压力/配额风险 | BinFlow 缝复用度 | 估级 | 置信度 |
|---|---|---|---|---|---|---|---|
| 1 | **helm**（classic） | **有（最强）**：index.yaml 一文档 = 全仓 chart+版本树 | helm.sh Chart Repository Guide（公开规范）；helm.md 锚 | index.yaml 大仓数 MB；一次拉取按 metadata TTL 缓存后零重复成本 | **最厚**：remote index.yaml 已回源（helm.md：local=存储/remote=回源/virtual=聚合）；chartsBaseUrl 分离基座（T-367）；本地 index 解析引擎既有（T-309）→ 合成树 = 解析器复用 + 渲染层 | **S** | 高（官方协议 + 本仓规格双源） |
| 2 | **deb** | 有：`dists/<suite>/…/Packages(.gz)` 一次拉取 = 该 suite 全包清单 | DebianRepository/Format（公开规范）；debian.md §布局/By-Hash 三档 | Packages.gz 大仓数十 MB；metadata TTL 缓存一次合成全树；suite/component/arch 层级固定 | dists/ 索引族已被 provider 归类 metadata（deb/layout）；本地 Packages 引擎既有（T-310）；拉取走引擎单路径 GET | **M** | 高 |
| 3 | **rpm** | 有：`repodata/repomd.xml` → primary.xml(.gz) = 全包清单 | repomd 社区规范；rpm.md §布局（primary/filelists/other） | primary.xml.gz EPEL 级 ~10–30MB；同 deb 形态 | repomd 族已归类 EXPIRABLE metadata（rpm provider，T-315）；本地 repodata 引擎既有（T-311） | **M** | 高 |
| 4 | maven | 半有：①GA 目录 `maven-metadata.xml` = 版本清单（规范内）②目录层 = 上游 HTML 目录索引（无标准，Maven Central 恰好服务） | maven-npm-pypi.md §1.4/§1.6；官方 Browse 节正例 = Maven Central | metadata 轻；HTML 抓取每目录一 GET 且上游换皮肤即碎 | maven-metadata.xml 已归类 metadata + VersionComparator 既有；HTML 索引解析器**缺** | ①版本层 S–M ②目录层 M 且脆弱 | ①高 ②中（机制官方未写算法） |
| 5 | generic | 无协议标准；仅当上游恰为目录索引服务器（Apache autoindex/另一 Artifactory/S3 网站索引）可 HTML 抓取 | 官方 Generic 小节**有**该设置（§2）；机制 = 上游目录索引 | 每目录一 GET；HTML 形态千人千面 | 引擎单路径 GET/凭据/SSRF guard 全既有；缺 HTML 目录解析器；folder face 已有（T-406 putFolderRow） | **M（可靠性低）** | 上游能力 = 协议事实（高）；抓取机制（中） |
| 6 | docker | 半有：per-image `GET /v2/<name>/tags/list`（分页 n/last+Link）；根级 `_catalog` 公共 registry 常禁用/限流 | OCI Distribution 公开规范；docker-registry.md §6（含 Link 头形态） | tags 上千 = 多次分页 GET；**Docker Hub 拉取配额/429 真实消耗**；_catalog 不可依赖（官方反例） | adapter 自驱上游会话（RemoteUpstream + guarded client）；tags/list 服务面既有（catalog.go 读本地 docker_manifests）→ 增量 = 上游 tags/list 代理并入 | **M**（tags 层；根树不可靠） | 高 |
| 7 | helmoci | 同 docker（per-chart tags/list；根级同限） | docker-registry.md §6 + helm.md §8（v2 栈共享） | 同 docker | 与 docker 同缝同实现（共享 v2 栈先例） | **M**（随 docker 腿近零边际） | 高 |
| 8 | npm | 仅 per-已知包名：packument = 该包全版本清单；**仓级全包枚举无**（`/-/all` 已退役；`/-/v1/search` 为评分搜索非全量） | maven-npm-pypi.md §2；npm registry 公开行为 | packument 大包数 MB；根/命名空间树**不可行** | npm remote 走服务层引擎（remote 对 adapter 不可见——npm/api.go 注释）；packument 已归类 metadata | **不可行**（根树）；已知包版本树 S | 高 |
| 9 | pypi | 双层：`/simple/` 根 = 全项目清单（PEP 503 HTML/PEP 691 JSON，重）；`/simple/<name>/` = 包级版本清单（轻） | PEP 503/691；maven-npm-pypi.md §3 | 根 simple 数 MB/数十万锚点（首拉重、解析重）；包级轻 | 包级 simple 已代理（T-406b 落穿先例）；根 simple 需 HTML/JSON 解析合成树 | 包级 **S–M**；根级 **M** | 高 |
| 10 | nuget | 无 v3 枚举端点；SearchQueryService `q=''`+take 分页可全量游走；flatcontainer `<id>/index.json` = 已知 id 版本清单 | nuget.md §8/§9；NuGet v3 公开规范 | 全量游走 = 数十页（nuget.org ~40 万包级）；search 语义 ≠ 目录树 | SearchQueryService 直连代理已有（经 guarded egress client——现「never lands as artifact」，浏览若要缓存需改落袋策略）；flatcontainer id/index 已缓存 | **M**（search 驱动合成树） | 高 |
| 11 | conan | 仅 per-ref：`<ref>/revisions` = 修订链清单；**无全局包枚举** | conan.md §2（端点族表；GitLab v2 公开规范锚） | per-ref 轻；根树不可行 | revisions 端点已 UpstreamPath 映射（T-312）；两枚 marker 已是 listing/search 基座 | **不可行**（根树）；per-ref S | 高 |
| 12 | go（goproxy） | 仅 per-已知模块：`<module>/@v/list` = 版本清单；**模块发现端点协议中不存在**（模块路径不透明） | goproxy.md §4.4（GOPROXY 公开规范） | per-module 轻 | `.versionList` = 上游 list 透传缓存既有（provider 两枚 marker + UpstreamPath facet） | **不可行**（根树）；叶级 S | 高 |
| 13 | cargo | 无枚举：sparse index 仅 config.json 入口 + per-crate 索引行（需已知名）；Web API `api/v1/crates?q=` = 搜索（非全量） | Cargo Book Registry Index/Web API；cargo.md §2/§3 | 搜索分页轻；无全量面 | search 响应已按查询键缓存（dirSearchCache）；config.json 已缓存 verbatim | **不可行**（根树）；search 驱动 M | 高 |

横向结论：
- **「一文档 = 全树」族**（枚举形态天然适配树浏览）：helm（S）/ deb（M）/ rpm（M）——三型 BinFlow 解析器全部现成。
- **「per-实体枚举」族**：docker/helmoci（tags 层）、maven（metadata 版本层）、pypi（包级）、npm/conan/goproxy/cargo（根树无 API）。
- **「无标准」族**：generic 与 maven 目录层（仅 HTML 抓取，脆弱）。
- 与 §2 官方支持面的关键错位：Artifactory 开的 5 型 = 「上游是文件树」族；helm 是 Artifactory 未开设置但枚举能力最强的一型（BinFlow 批 1 = 超集姿态，定性 L2 增强）。

## 4. 上游故障降级语义（置信度：高——repo-semantics §7.2/§7.6 行为基座 + 官方 metadata TTL 口径）

开档后的远端枚举层与既有 remote 行为基座的关系（T-442/T-448 消费）：

1. **枚举失败不整树塌**：上游不可达/超时时，远端层进入**错误态**（该层呈现错误信息），**已缓存行维持可用**——降级 = 回到默认档的可见面 + 远端层错误标注，而非整棵树 500/空树。
2. **assumed-offline 静默期沿用**（repo-semantics §7.2）：上游连接故障后 repo 标记 assumed-offline（默认 300s 静默期），期内**不重试回源**——远端枚举在静默期内直接呈现错误态，不打上游（上游压力防护与既有 pull-through 同一套）。
3. **缓存行优先、远端行 display-only**：缓存行与远端派生行并存时**缓存行优先**；远端派生行**零落库**（display-only 合成行，T-412 模式沿用）——去重规则：同路径缓存行存在则不重复出远端行。
4. **索引文档防护**：大索引（deb Packages.gz 数十 MB 级）需**尺寸上限 + metadata TTL 缓存**（官方语义即「按 Metadata Retrieval Cache Period 缓存」）——一次拉取窗内合成树零重复成本。
5. **点击未缓存远端行** → 触发回源拉取（正常 pull-through 链）+ 下载计数埋点联动（BinFlow 侧 T-438 单源——Artifactory 侧该行为同构：拉取即计 stats）。
6. **SSRF 无新面**：枚举目标 URL 仍由 repo 配置（url 字段）决定，既有 guarded egress 面覆盖。

## 5. virtual 仓口径扩面（§8.5 联动；置信度：中——repo-semantics §8.5 原文 + T-412 as-built 张力登记）

- repo-semantics §8.5 原口径：virtual 目录 GET = 各成员（**含 `listRemoteFolderItems=true` 的 remote 成员**）条目按 §8.1 顺序合并去重。
- **既登记张力**：BinFlow T-412 `listVirtual` as-built 注释「remote 成员仅缓存行」= 默认档（全 remote 均未开档）下的正确行为；**可选档 on 后口径扩面**——开档 remote 成员的**远端派生行**也进 virtual 树（display-only 同 §4-3）。T-448 的 §8.5 回写即此扩面的落笔。
- 合并序沿用 §8.1 四桶序不变；远端行同样受 include/exclude patterns 过滤（§7.1 同族）。
- 权限面：远端行可见性 = 该 remote 仓 allow() 同源（越权仓零远端行泄漏——T-448/T-461 AC 探针位）。

## 6. BinFlow 取舍基线（M15 Q4 出口 C 已裁进 M16 FR-147；本节为登记非新裁）

- **批 1（M16 实现）**：helm classic + deb + rpm（C2 序——协议廉价度 × 缝厚度；官方 5 型中的 deb/rpm 为可选档对齐，helm 为 L2 增强如实标注）。
- **批 2（条件票）**：docker/helmoci tags 层（drill-down 定位，需向用户明示 catalog 根不可达）+ maven metadata 版本层。
- **明确不做**：generic 与 maven 的 HTML 目录抓取（脆弱负债）；npm/pypi 根 /goproxy/cargo/conan 根树（无 API——做即假树）。
- **字段当下姿态**：BinFlow canonical config 现不接受 `listRemoteFolderItems`（未知字段容忍丢弃族）——T-448 接线后配置面开此开关（默认 false，off 行为 diff=0）。

## 7. 置信度分布与待验证清单

置信度分布：高（官方 + 本仓规格/反编译双源）= §1/§2 支持面与默认值/§3 矩阵主行/§4 降级基座；中（单源或推断）= §2 机制推断、§5 virtual 扩面、§3 maven 目录层行；低 = 无。

| # | 项 | 现值依据 | 验证途径 |
|---|---|---|---|
| R-a | 开档后 UI 树/列表的**呈现形态**（远端行视觉标识、层级展开行为、分页） | 官方文档单源（配图文字描述） | t226 恢复 + UI 建仓腿（T-425 §1.4 遗留建议：实现票带 Pro 实例或 UI 建仓对照腿）；或 BinFlow e2e 自证形态自有 |
| R-b | 开档对「simple vs list 两种浏览模式」的差异呈现 | 官方字段描述提及 simple/list 两模式（无细节） | 同上 |
| R-c | virtual 树含远端行的**去重与排序细节**（§8.1 序下远端行的精确插位） | repo-semantics §8.5（中）+ 推断 | BinFlow T-448/T-461 e2e 断言面定案（Artifactory 活体需 virtual+开档 remote 仓，t226 不可建） |
| R-d | deb/rpm 索引文档的尺寸上限现值 | BinFlow 自定防护（Artifactory 未见公开阈值） | 实现票压测腿定案（C 层参数） |

## 8. 取证锚点

- 官方：《Remote Repositories》`https://docs.jfrog.com/artifactory/docs/remote-repositories`（2026-09-02 实取 71KB——「List Remote Artifacts」逐小节出现矩阵 21 小节全量比对 + 《Browse Remote Repositories》专节引文）；GC/维护 cron 相关另见 cron-scheduling.md。
- 本仓：repo-semantics.md §7.1（字段表）/§7.2（pull-through 六步）/§7.6（上游故障矩阵）/§8.5（浏览聚合）；helm.md / debian.md / rpm.md / maven-npm-pypi.md / docker-registry.md / nuget.md / conan.md / goproxy.md / cargo.md / npm.md 各协议锚（§3 矩阵列出处）。
- t226 探针（T-425 §1.4，2026-09-02）：REST 建 remote/local 仓双臂 400 Pro 门实录、探针前后仓库清单差集 = 零残留（INC-1 只读纪律）；开档形态活体对拍**结构性不可执行**（Pro 门），留痕在案。
