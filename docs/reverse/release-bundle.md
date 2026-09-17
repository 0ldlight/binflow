# Release Bundle 域 行为规格（M17 FR-153 前置锚，T-488）

> **定位**：Release Bundle（RBv1 为主、RBv2 留痕）端点族 + bundle 模型 + 冲突语义 + 状态机 + 深度边界选项面（Q2 材料——两出口并列）+ Any Distribution 预置语义 + 商业档位核验（ADR-0033 / ADR-0046 软缝对拍输入）。
>
> **取证状态（2026-09-06）**：与 build-info.md 同——**零活体**（双基线损坏，reports/agents/T-488.md §0）。置信度分层：「高」= 反编译 REST 资源类/产品 DDL + 官方文档双源；「中」= 单反编译源；「低」= 推测。Artifactory 侧 Release Bundle 本体依赖 **JFrog Distribution 服务**（独立部署单元）——Artifactory 内只驻「源 bundle 承接面」，本规格把两侧分开锚定。
>
> **效力序**（ADR-0046 条款）：用户裁决（BOARD）> 本规格 > ADR-0046 > PRD。
>
> **活体核验状态（2026-09-17，L026-1）**：参照实例已恢复（pro 7.161.15 全 addons）——§1 主表与 §3 冲突/校验分支经 60 发实弹探针复核（wire 证据 `reports/compatibility/l026a-wire/` p01–p60），勘误已内联标注；新增 §10 活体核验节（v1/v2 并存定案 + R06 权限矩阵）。零活体时代关闭。

## 0. 与 ADR-0046 的软缝对拍（八项逐答）

| # | ADR 软缝项 | 本规格定案（详节） | 与 ADR 骨架差异 |
|---|---|---|---|
| ① | bundle 模型字段集与 wire 命名 | §2 字段集（name/version/status/created/signature/type/storing_repo/keep/source_service_id + 清单行 repo_path/original_component_details） | ADR bundles 表骨架的 `description/state` 列为 BinFlow 自有裁减——Artifactory v1 字面无 description（release notes 属 Distribution 侧 UI 面） |
| ② | 端点族路径 | Artifactory 源侧 = `/api/release/*` 族（§1 表，18 端点）；创建的官方对位 = **`POST /api/release/bundle`**（AQL 装配）或 Distribution 侧 `POST /api/v1/distribution/release_bundle`（跨服务）；ADR 骨架示意 `PUT/GET /api/release_bundle/{name}/{version}` 与两侧官方字面**均不符**——勘误回填（BinFlow 最小面路径归 T-513 按 §1 对位） | 路径字面修正 |
| ③ | 重复版本冲突语义 | 三态（§3.1）：新 → **202**；同签名未完结 → **200** 续传；签名不符 / 已 COMPLETE → **409** `{"status":409,"message":"Bundle already exists"}`（信封形态 = 产品 REST 通用 ErrorResponse） | ADR「409 vs 400」定案 = 409 + 三态细分（202/200/409） |
| ④ | 状态机字面 | Artifactory 承接面 = **FAILED / INPROGRESS / COMPLETE / CLOSE_INPROGRESS**（终态 = FAILED、COMPLETE）；Distribution v1 UI 面 = Draft/Signed 两段 | ADR `state CHECK IN (…)` 闭集以本行为准（M17 最小面可用 COMPLETE/INPROGRESS 子集——C 层裁减留痕） |
| ⑤ | release_bundle 域 webhook 处置 | 维持 ADR 倾向：`created` 单型随创建端点 wired、signed/deleted 等维持 dormant（webhook.md §3.5~3.7 九型清单不动） | 证实 ADR 倾向 |
| ⑥ | 清单行来源形态 | 官方 v1 三源：AQL / 构建查询（build name+number）/ Include-Exclude 模式（query builder）；Artifactory 源侧端点 body 即 `{uuid, signature, aql}`——**AQL 是源侧一等公民**。M17 倾向显式清单 = C 层子集（BinFlow 自有裁减，不违官方——官方亦接受显式 artifact 清单形态） | 倾向证实，AQL 引用联动 T-511 注记 |
| ⑦ | 商业档位核验 | 官方明文 **Enterprise+**（`release-bundle-repositories` 页：「available to Enterprise+ customers using JFrog Distribution」）；RBv1 历史 Enterprise、RBv2 = Distribution/Release Lifecycle。ADR MinTier=pro 暂行 = BinFlow 自有裁减（最小面 < enterprise 真身），不翻案、登记「真身 enterprise」 | 勘误注记（机制轴零翻动） |
| ⑧ | Any Distribution 桶值拼写 | Artifactory 内部常量 **`ANY DISTRIBUTION`**（`PermissionTarget.ANY_DISTRIBUTION_REPO`）；UI 呈现 `Any Distribution`（console-ui.md §3.8 t226 活体，Edit Repositories 两步弹窗预置行，与 Any Local/Any Remote 同场） | 拼写字面已锚（BinFlow 桶值归 T-491） |

---

## 1. 端点族（Artifactory 源侧 `/api/release/*`；置信度：高——反编译 REST 资源类逐方法 + 官方 llms.txt 索引页对拍）

角色门：`@RolesAllowed` admin/user（`ha` 角色仅创建端点）；anonymous 调创建 → 403 裸响应。

| 方法 | 路径 | 参数/body | 成功响应 | 错误响应 | 语义 | 置信度 |
|---|---|---|---|---|---|---|
| POST | `/release/bundle` | body `{uuid, signature, aql}`；`?includeMetaData=&projectKey=` | 200 `{results:[{urn, sha256, properties, signature, pkg_type, pkg_name, pkg_version, size, xray_scan_info{blocked, blocked_reason}}]}` | 403（anonymous） | **AQL 装配**：执行 AQL 得制品清单（xray 扫描信息随 includeMetaData） | 高 |
| POST | `/release/bundle/transaction` | body = **JWS 签名字符串**（`application/jose`） | → 转发 open | — | v2 签名开启事务（包装端点） | 高 |
| POST | `/release/bundle/transaction/open` | body `ReleaseBundleOpenTransactionRequest`（JSON，内含 signedJwsBundle） | **201** `BundleTransactionResponse` | 400 `Failed validating release bundle signature` / 400 `Failed to parse JWS. <msg>` / 400 非法参数 | 开 bundle 存储事务（签名校验前置） | 高 |
| POST | `/release/bundle/transaction/close/{transaction_path}` | 路径 = 事务路径 | 200 `{}` | — | 同步关闭事务（落 bundle） | 高 |
| POST | `/release/bundle/transaction/async/close/{transaction_path}` | `?sync_wait_time_secs=` | 200 `CloseTransactionStatusResponse` | — | 异步关闭（等待窗可调） | 高 |
| GET | `/release/bundle/transaction/async/close/status/{transaction_path}` | — | 200 状态响应 | — | admin 门；查异步关闭进度 | 高 |
| PUT | `/release/store` | body `{signedJwsBundle, storingRepo, artifactMapping}`；`?projectKey=` | **202**（新）/ **200**（同签名续传）+ `{bundle_path}` | 409 冲突（§3.1）/ 400 签名失败 | **Distribution 推送源 bundle 的承接端点**（admin 门）；冲突语义见 §3.1 | 高 |
| OPTIONS | `/release/store` | — | 200 `Allow: OPTIONS,PUT` | — | CORS 预检 | 高 |
| GET | `/release/bundles` | `?type=source|target`（缺省 **TARGET**） | 200 `{"bundles": {}}`——**名 → 版本数组的 map**，空态 = 空对象（L026-1 p01/p02 实弹） | — | 全部 bundle 名 | 高 |
| GET | `/release/bundles/{name}` | `?type=` | 200 `{"versions": []}`——**名不存在也 200 空数组**（非 404，L026-1 p03） | — | 某 bundle 的版本列表 | 高 |
| HEAD | `/release/bundles/{name}/{version}` | —（**恒 SOURCE**） | 200 + `X-Checksum-Sha256` 头（bundle JSON 的 sha256） | 404 无 body | 源 bundle 校验和探针 | 高 |
| GET | `/release/bundles/{name}/{version}` | `?format=jws&type=` | 200 bundle JSON（`application/json`）/ 签名 JWS（`format=jws` → `application/jose`） | 404 | 取 bundle 描述符（v2 可取签名形态） | 高 |
| GET | `/release/bundles/{name}/{version}/status` | `?type=` | 200 状态字符串 | — | bundle 状态（§3.2） | 高 |
| DELETE | `/release/bundles/{name}/{version}` | —（**恒 TARGET**） | 200 `{}` | **404 `{"status":404,"message":"Bundle not found"}`**（记录不存在，L026-1 p09） | 删目标侧 bundle 记录 | 高 |
| DELETE | `/release/bundles/source/{name}/{version}` | — | 200 `{}` | **404 `…"message":"Release bundle not found"`**（消息族与 TARGET 臂不同，L026-1 p10） | 删源 bundle 记录 | 高 |
| GET/PUT | `/release/bundles/config` | body `{incompleteCleanupPeriodHours}` | GET 200 **缺省 720**（L026-1 p06）/ PUT **202** `Successfully updated release bundles config` | — | admin 门；未完结 bundle 清理周期 | 高 |
| GET | `/release/bundles/{name}/{version}/artifacts` | — | 200 `ReleaseBundleArtifactsModel` | — | bundle 制品清单（xray 扫描消费面） | 高 |
| GET | `/release/fat_manifest_content/{path}` | `?projectKey=` | 200 fat manifest 内容 | — | **admin 门**；v2 fat manifest 查看 | 高 |

**来源分层注记**：`/bundles` 查询族（GET 列表/版本/单查/status、DELETE 两行）= 反编译 + 官方 reference 页（getallbundles/getallbundleversions/getreleasebundleversion/deletereleasebundleversion 等）双源**高**；`transaction` 三段式 / `store` / `jose` / `config` / `artifacts` / `fat_manifest` 族 = **反编译单源「中」**（官方无页——Distribution 文档只覆盖服务侧对端行为）。§0 ②的路径字面以本表为准。

**Distribution 侧 v1/v2 族**（官方 llms.txt 索引全录，2026-09-06 实取——**独立服务** `/api/v1/distribution/*`，BinFlow M17 面外）：创建（`createreleasebundlev1version`）/ 版本族查询/更新/删除/签名（`signreleasebundlev1version`）/ 扫描/分发（`distributereleasebundlev1version`、动态分发、中止、边缘删除）/ 分发状态族（按 name/version/trackerId）/ air-gap 导出导入四件 / v2 分发五件 / 边缘节点/维护/令牌/权限/GPG 密钥族。约 60+ 操作——本表不展开，Q2 出口②启用时再锚（§4）。

## 2. bundle 模型（字段集）

### 2.1 Artifactory 侧记录模型（产品 DDL 逐字，`postgresql.sql`；置信度：高）

```
artifact_bundles( id PK, name(≤255), version(≤255), status, date_created, date_distributed,
                  signature NOT NULL, type DEFAULT 'TARGET', storing_repo, keep, source_service_id )
                  UNIQUE(name, version, type)          ← 冲突语义的根
bundle_files(     id PK, node_id FK→nodes, bundle_id FK→artifact_bundles, repo_path, original_component_details )
build_release_bundles( …, bundle_repository, bundle_name, bundle_version )   ← 与 builds 表族的关联（ON DELETE CASCADE）
```

- AQL `releases` 域字段 = 本表直投影：`id/name/version/status/created/signature/type/storing_repo/keep(Int)/source_service_id`；`release_artifacts` 域 = `id/item_id/bundle_id/path`（aql.md §15.5 注记，M17 维持 400）。
- 命名规则（官方，高）：name 首字符字母数字、后续 `字母数字-_:`、≤255；version ≤255（无 SemVer 强制）。
- v1 单 bundle 制品量建议 ≤3,000（官方 recommended，非硬限）；AQL 输入上限 6,000 字符（Distribution 2.44+，`aql-input-size-limit` 可调）。

### 2.2 语义要点

- **type 二分**：SOURCE（源实例上的发布记录）/ TARGET（边缘/目标实例上的承接记录）；查询族 `?type=` 缺省 TARGET；HEAD 校验和恒 SOURCE；DELETE 主端点恒 TARGET、`/source/` 恒 SOURCE。
- **storing_repo**：缺省 **`release-bundles`**（自动创建——产品实现 `createReleaseBundlesRepoIfNotExist` + 官方「automatically created and used by default」双证，高）；store 时若请求未指定即解析为缺省并可自动建仓；bundle 内容落 `<storingRepo>/<name>/<version>/<target path>`。
- **v2 形态**（Release Lifecycle）：仓库 key 后缀 `release-bundles-v2` 或 `application-versions`；bundle JSON 文件形态 `.json.evd` / `.json.draft`；签名 = JWS（`application/jose` 媒体类型、`ReleaseBundleOpenTransactionRequest`、事务三段式 open→store→close）。M17 面外（§4 出口②）。
- **签名必填性**：`artifact_bundles.signature NOT NULL`——记录级无签名不落；v1 的 GPG 签名在 Distribution 侧 finalize 时做（草稿可未签名——Draft/Signed 两段，官方）；v2 恒 JWS。

## 3. 冲突与状态语义

### 3.1 冲突三态（`PUT /release/store`；置信度：根=高——产品 DDL `UNIQUE(name, version, type)` 一手约束；行为分支=中——反编译单源，官方未覆盖）

1. 无既有 (name, version, SOURCE) → 新建 → **202** + 异步拷贝制品 → `{bundle_path}`。
2. 有既有且签名**相同**且状态 ≠ COMPLETE → **200** 续传/重放（再次触发异步制品拷贝）。
3. 有既有且签名**不同** → 409；或状态 = COMPLETE → 409。错误体 = 产品通用 ErrorResponse：`{"status":409,"message":"Bundle already exists"}`（原因枚举 UNMATCHING_SIGNATURES / ALREADY_COMPLETED 不出 wire——内部 Reason）。
- 前置断言：artifactMapping 的**源制品必须全部存在**、目标制品须被接受（失败 400 系）。

### 3.2 状态机（Artifactory 承接面；置信度：高——产品枚举逐字）

`FAILED / INPROGRESS / COMPLETE / CLOSE_INPROGRESS`；终态闭集 = {FAILED, COMPLETE}。Distribution v1 UI 面 = Draft（可编辑）→ Signed（签名后不可编辑）两段（官方）——两套字面分属两侧，不混用。BinFlow M17 最小面状态闭集（ADR-0046 点 1 CHECK）建议取 Artifactory 侧字面子集（COMPLETE/INPROGRESS 起步）——C 层裁减留痕。

## 4. 深度边界选项面（Q2 材料——两出口并列；ADR-0046 已裁 M17 = 出口①，出口②登记为终裁输入）

- **出口①（最小面 = M17 实现位）**：本地 bundle 记录域——创建（名+版本+清单）/ 单查 / 列表 + Any Distribution 预置通道；**不做** v2 signing（JWS/GPG 链）、不做 Distribution 服务对接（跨实例分发/边缘/TRacker 状态机）。对位锚 = §1 Artifactory 源侧查询族 + `POST /api/release/bundle` 的装配语义（AQL→清单）可降级为显式清单。风险面：无签名 → `signature` 字面以占位（如空串/内校验和）承载——wire 兼容、语义留痕（C 层）。
- **出口②（全量面）**：v2 signing（JWS 事务三段式 + GPG 密钥族 + `.json.evd` 证据链）+ Distribution 服务对接（`/api/v1/distribution/*` 全族：创建/签名/分发/边缘状态/air-gap）。证据规模：Distribution 侧 60+ 操作（§1 尾段）；Artifactory 侧配套 = v1→v2 适配层（`ReleaseBundleTargetV1ToV2*` 四 adapter，反编译在案）。跨单仓产品边界（类 Xray 集成面）——翻转条件 = 用户明示推翻 Q2 + Distribution 可行性评估票 + ADR-0046 增补（MinTier 升 enterprise）。
- **webhook 联动**：release_bundle 域九型维持 dormant（webhook.md §3.5~3.7）；出口①下 `created` 单型 wired 倾向（ADR-0046 软缝⑤）。

## 5. Any Distribution 预置语义

- **Artifactory 实态**：预置权限目标 `ANY DISTRIBUTION`（内部常量字面，`PermissionTarget.ANY_DISTRIBUTION_REPO`）——与 `ANY LOCAL` / `ANY REMOTE` / `ANY`（Anything）同族，作用于 **distribution 类型仓**（rclass `distribution`，`RepoType.Distribution` 独立枚举值）。授权回退链位序：精确仓 ACL → … → ANY LOCAL → ANY REMOTE → **ANY DISTRIBUTION** → ANY。
- UI 呈现：权限编辑两步弹窗的预置行 `Any Distribution`（console-ui.md §3.8，t226 OSS 7.84.10 活体——**该预置在 OSS 档 UI 在场**，高）。
- release-bundle 仓（`release-bundles` 缺省仓 + RELEASE_BUNDLE 类型仓 + v2 后缀仓）与 buildinfo 仓一样**被排除在 ANY 家族回退之外**（`isBuildInfoOrReleaseBundleRepo` 短路）——即 bundle 内容仓的授权必须显式仓 ACL，ANY DISTRIBUTION 管的是 distribution 类型仓。
- **BinFlow 对位（ADR-0046 点 3 已裁）**：Any Distribution = allow() 同源伪键通道（Can 的 repoKey 位承载通配桶值、path 位传 bundle_name）；桶值拼写归 T-491（§0 ⑧字面已锚）。

## 6. 商业档位核验（ADR-0033 联动 / ADR-0046 点 4 勘误输入）

- 官方明文：Release Bundle 仓库特性「available to **Enterprise+** customers using JFrog Distribution」（release-bundle-repositories 页，2026-09-06 实取，高）；RBv2/Release Lifecycle = Distribution 服务域（Enterprise 级跨实例编排）。
- 产品侧佐证：`/api/release/*` 资源类位于 addon（`artifactory-addon-release-bundle` JAR）——addon 装配即商业门控形态（ADR-0033 行为模式对位）；OSS 发行集合内**无** release bundle 数据面（与 build-info 的「代码在场运行时门」形态不同——RB 在 OSS 代码面即不在 core）。
- **结论**：Artifactory 真身 = **Enterprise+ / Distribution**；ADR-0046 MinTier=pro（暂行）是 BinFlow 最小面自有裁减（pro < enterprise 因 enterprise 级真身被出口①排除）——不翻案，§0 ⑦勘误注记「真身 enterprise」；若 Q2 翻出口②则 MinTier 升 enterprise（ADR-0046 点 4 既定路径）。

## 7. 与公开规范的差异/补充（「此条补充官方规范」）

- 官方 Distribution 文档未载、仅反编译可见：`/api/release/*` 源侧 18 端点全表（官方 llms.txt 索引仅录 Distribution 服务侧）；冲突三态 202/200/409 细分与 ErrorResponse 形态；事务三段式 open/store/close 与 `application/jose` 媒体类型；`type` 参数缺省 TARGET 与 HEAD 恒 SOURCE 的不对称；`release-bundles` 缺省仓自动创建；v2 仓库后缀与 `.json.evd/.json.draft` 文件形态；release-bundle 仓排除于 ANY 回退链。
- 官方与产品的分歧登记：官方 create 页未覆盖 name+version 冲突行为（§3.1 以产品实现为准）；官方状态词（Draft/Signed）与产品 DB 状态枚举（INPROGRESS/COMPLETE/…）分属两侧——文档不混用，本规格已分层标注。

## 8. 待验证清单（低置信/未实证——零静默升格）

1. Distribution 侧 `POST /api/v1/distribution/release_bundle` 对 name+version 冲突的**逐字响应**（官方未覆盖，产品侧不可见——Distribution.war 未反编译）。
2. ~~`BundlesResponse` / `BundleVersionsResponse` 的行级字段集~~ **已闭（L026-1）**：名面 = `{"bundles": {name: [versions…]}}` map、版本面 = `{"versions": []}`（p01–p03）。
3. `CloseTransactionStatusResponse` 字段集（仍单源中——活体未能开启真实事务，仅错误臂实弹）。
4. ~~`GET /release/bundles?type=source` 在 v2 bundle 存在时的回显混合形态~~ **部分闭**：空态两种 type 均为 `{"bundles": {}}`（p02）；**有 v2 bundle 时的回显混合形态仍未证**（需真实 v2 记录，待 Distribution 联调环境）。
5. ~~本轮活体全缺~~ **已闭（L026-1）**：错误臂/校验臂/权限臂/空态全族实弹（§10）；**happy-path 的 202/200/409 三态与状态流转仍未实弹**（需有效 JWS 签名链——Distribution 侧密钥不在本环境，p44–p48 只到签名前置臂）。
6. 【L026-1 新增】`GET /api/v2/release_bundle/received/{name}/{version}` 恒 400 名字校验消息（p20/p23——合法字母名也 400，疑该 GET 路由不存在而落进名字校验器；DELETE 同路径是真实路由 p60）。
7. 【L026-1 新增】v2 读面 404 泄露的两个仓名 `release-bundles-v2`（records/statuses，p16/p17/p25）与 `release-bundles-v2-jfds`（received-DELETE，p60）的分工语义——jfds 后缀（疑 JFrog Distribution Service 专属库）单源低。
8. 【L026-1 新增】反编译在案但活体不可达的分支：非 entitlement 实例的 400 `Bundle request is only available on Enterprise Plus licensed Artifactory instances.`（本实例 license 含 RELEASE_BUNDLES_READ 权益未触发，p27–p29 直接进 AQL 校验）；匿名 403 裸响应分支（实例关匿名，p56–p58 均为全局 401 门拦截）。

## 9. 取证锚点（2026-09-06 会话）

- 反编译：`rest/resource/release/ReleaseBundleResource.java`（18 端点逐方法）、`api/rest/release/{ReleaseBundleRequest,SourceReleaseBundleRequest,ReleaseBundleResult,ReleaseBundlesConfigModel}.java`、`addon/release/bundle/ReleaseBundleAddonImpl.java`（storeBundle 冲突三态 + 缺省仓）、`addon/release/bundle/exception/ReleaseBundleAlreadyExistsException.java`、`bundle/BundleType.java`（SOURCE/TARGET 缺省 TARGET）、`rest/common/exception/mapper/AlreadyExistsExceptionMapper.java`（409 + ErrorResponse）、`security/{PermissionTarget,AuthorizationServiceBase,LegacyAuthorizationServiceImpl}.java`（ANY DISTRIBUTION + 排除链）、`util/bundles/BundleConfigurationUtil.java`（v2 仓后缀/证据文件）、`postgresql/postgresql.sql`（artifact_bundles/bundle_files DDL）、`rest/common/resource/release/bundle/ReleaseBundleResourceFilter.java`。
- 官方（docs.jfrog.com，2026-09-06 实取）：`docs/artifactory/docs/create-release-bundles-v1.md`（签名/规格/限制）、`docs/artifactory/docs/release-bundle-repositories.md`（缺省仓/Enterprise+）、`docs/artifactory/reference/llms.txt` + `docs/integrations/reference/llms.txt`（Distribution v1/v2 全族索引页名）。
- 官方 OSS 源码（用户提供发行树，绝对路径只读）：`build-handler/build-handler-acl/.../util/BuildConstants.java`——`DEFAULT_RBV2_REPO = "release-bundles-v2"`（v2 缺省仓名逐字）、`PACKAGE_TYPE_RB = "releasebundles"`（§2.2 v2 形态字面的官方常量印证）。
- 既有仓内锚：aql.md §2.1（releases/release_artifacts 入口 400 行）、webhook.md §3.5~3.7（九型 dormant）、console-ui.md §3.8（Any Distribution 预置行，t226 活体）、inv-4 distribution 族（档位对照）。
- 本轮活体（L026-1，2026-09-17）：参照实例 pro 7.161.15（172.16.58.130:8082），60 发探针 wire 全录 `reports/compatibility/l026a-wire/p01–p60.txt`（凭据不入档）；§10 逐条锚 p 编号。临时资产（l026q-rbsrc 仓、l026r-d08-user 用户、探针副作用自动创建的 release-bundles 系统仓）已全部删净复核。
- 前会话（T-488，2026-09-06）：取证状态维持原 §9 全部锚点不变。

---

## 10. 活体核验（L026-1，2026-09-17，pro 7.161.15 实弹；wire = reports/compatibility/l026a-wire/ p01–p63）

### 10.1 v1/v2 两代面并存定案（R01/R02 勘误总纲；置信度：高——活体 + 反编译路由注册双源）

当客户端在 pro 7.161.15（license 含 release-bundle 权益）上探测 release-bundle 面，服务端**同时暴露两代面**：

| 面 | 路径前缀 | 形态 | 活体状态 |
|---|---|---|---|
| v1（RBv1 源/目标承接面） | `/api/release/*`（18 端点，§1 表） | JAX-RS 资源类，直接服务 | **全存活**——查询族/校验臂/权限臂逐发实弹 |
| v2（Release Lifecycle 读面） | `/api/v2/release_bundle/*`（**下划线单数**；连字符复数 `/api/v2/release-bundles` 404 "Not found"） | 路由注册改写自 lifecycle 服务命名空间（`/lifecycle/api/v2/release_bundle` → `/artifactory/api/v2/release_bundle`），资源类不在 Artifactory 反编译树（独立 lifecycle 服务内嵌） | 读面/错误面存活（names/received/records/statuses）；写面（创建/事务）需签名链未探 |

**矩阵勘误结论**：D08-R01~R05 按 v1 记录**不翻案**——v1 无迁移/下线迹象；v2 面为并存增量，R02 的「事务三段式」在 v2 的对应形态 = lifecycle 服务侧 `records` + `distribution_transactions` 族（本面只见读端点，写端点归 Distribution 服务侧 R07 邻域）。R03 的 202/200/409 三态仍以 v1 `PUT /api/release/store` 为准。

### 10.2 R01 装配端点 `POST /api/release/bundle`（置信度：高——活体 + 反编译逐字对拍）

当客户端 POST `{"aql": "<items.find…>"}`（Content-Type application/json）：

- **校验链逐字**（顺序固定）：
  1. license 无 RELEASE_BUNDLES_READ 权益 → 400 `Bundle request is only available on Enterprise Plus licensed Artifactory instances.`（本实例未触发——反编译单源，中）
  2. body 缺 `aql` 键或空串 → 400 `Request is invalid. Missing AQL query`（p27/p28）
  3. aql 不以 `items.find(` 开头 → 400 `Request is invalid. AQL query should find artifacts (items)`（p29）
  4. AQL 语法错 → 400 `Failed to parse query: <原 aql>, it looks like there is syntax error near the following sub-query: <token>`（p33，与 /api/search/aql 同族消息）
- **命中形态**（p30/p36）：200 `{"results": [{"urn": "<repo>/<path>", "sha256": "<hex>", "properties": {…}, "size": <n>, "pkg_type": "Generic"}]}`——行字段闭集实测五键；properties 值形态 = `{"k": ["v"]}` 数组值；无属性行 `properties: {}`（键**不省略**）。
- **空命中**（p32）：200 `{"results": []}`——results 键在场空数组。
- `?includeMetaData=true/false` 差异 = 按包类型元数据**路径**过滤（generic 仓无元数据路径两臂同形 p36/p37；反编译 MetaDataFilter 按仓类型+路径判非按属姓名——中）；本票未建 maven 仓差分，留待验证清单。
- 角色门：admin/user 可达校验臂（非 admin 用户 p51 得 400 校验消息非 403）。

### 10.3 R02 事务三段式错误臂（置信度：高——活体逐字；happy-path 未实弹需签名链）

当客户端以无效载荷打事务族：

| 请求 | 响应 | 锚 |
|---|---|---|
| POST `/bundle/transaction`（application/jose，body 非 JWS） | 400 `Failed to parse JWS. Invalid serialized unsecured/JWS/JWE object: Missing part delimiters` | p38 |
| POST `/bundle/transaction/open` body `{"signedJwsBundle":"garbage"}` | 400 同上消息（包装端点与 open 端点同形） | p39 |
| POST `/open` body 非 JSON | 400 裸 Jackson 消息 `Unrecognized token 'not': was expecting (JSON String, Number, Array, Object or token 'null', 'true' or 'false')…` | p40 |
| POST `/bundle/transaction/close/{tx}`（事务不存在） | 400 `Release bundle {tx} not found` | p41 |
| POST `/bundle/transaction/async/close/{tx}` | 400 同上 | p42 |
| GET `/bundle/transaction/async/close/status/{tx}` | 400 同上 | p43 |

（`{tx}` = 客户端所发事务路径原文回显。）

### 10.4 R03 `PUT /api/release/store` 承接臂（置信度：高——活体 + 反编译双源；**校验顺序勘误**）

当客户端 PUT store，服务端按序执行，**首个失败臂短路**：

1. 非匿名 + admin 门（非 admin → 403 §10.7）；
2. `projectKey` 查参指向不存在项目 → 404 `HTTP response status 404:Failed to execute add project resource with error Could not find project `<key>``（p49，Access 联查透传）；
3. body 无 `signedJwsBundle` 键 → **500** `{"status":500,"message":"jwsString is marked non-null but is null"}`（p45——空指针泄漏为 500，**非** 400）；
4. `storingRepo` 显式给且非 release-bundle 类型仓（如普通 local）→ 400 **双键信封** `{"reason":"INVALID_RB_REPO","errors":[{"status":400,"message":"Invalid release bundle repository"}]}`（p44——§3.1「400 签名失败」的前置臂，仓库类型校验先于签名校验）；
5. `storingRepo` 缺省 + 默认项目 → 解析为 `release-bundles` 并**自动创建系统仓**（p48 副作用实弹：仓 rclass=local、packageType=**releasebundles**、simple-default 布局；**GET /api/repositories 列表不显示它**，单查 GET 200——p48b wire 录全配置）；
6. JWS 解析失败 → **500** `Invalid serialized unsecured/JWS/JWE object: Missing part delimiters`（p48——**非** transaction/open 的 400 包装形态；同因异码，两端口不对称）；
7. 此后才是签名校验（400 `Failed validating release bundle signature`）→ artifactMapping 源存在断言 → 冲突三态 202/200/409（§3.1 维持，未实弹）。

- OPTIONS `/release/store` → 200 text/plain `OPTIONS, PUT` + `Allow: OPTIONS,PUT`（p11）。

### 10.5 R04 查询族空态与 404 消息族（置信度：高——活体逐字）

当客户端查询不存在的 bundle：

| 请求 | 响应 | 锚 |
|---|---|---|
| GET `/release/bundles`（type 缺省/`source`） | 200 `{"bundles": {}}` | p01/p02 |
| GET `/release/bundles/{name}` | 200 `{"versions": []}`（**名不存在也 200**） | p03 |
| GET `/release/bundles/{name}/{version}`（type 任意值同形） | 404 `Bundle not found` | p04/p59 |
| GET `/release/bundles/{name}/{version}/status` | 404 `<name>:<version> not found`（**冒号拼接形，与上一行不同族**） | p05 |
| HEAD `/release/bundles/{name}/{version}` | 404 **无 body**（恒 SOURCE 语义维持） | p07 |
| GET `/release/bundles/{name}/{version}?format=jws` | 404 `Bundle not found` | p12 |
| GET `/release/bundles/{name}/{version}/artifacts` | 404 `Bundle not found` | p08 |
| DELETE `/release/bundles/{name}/{version}`（TARGET） | 404 `Bundle not found` | p09 |
| DELETE `/release/bundles/source/{name}/{version}` | 404 `Release bundle not found`（**第三个消息族**） | p10 |

404 一律 errors 信封（`{"errors":[{"status":404,"message":…}]}`）除 HEAD 无 body。

### 10.6 R05 config / fat_manifest 与 v2 读面（置信度：高——活体逐字；v2 面资源类不在反编译树故形态面=活体单源高、语义面=中）

当客户端操作 config 与 v2 读面：

- GET `/release/bundles/config` → 200 `{"incompleteCleanupPeriodHours": 720}`（**出厂缺省 720 小时**，p06）。
- GET `/release/fat_manifest_content/<path>` 且 path 非 `list.manifest.json` 结尾 → 400 `Fat manifest content view is only allowed on list.manifest.json files. Got: [<path>]`（p47）。
- **v2 读面**（`/api/v2/release_bundle`，下划线）：
  | 请求 | 响应 | 锚 |
  |---|---|---|
  | GET `/names` | 200 `{"release_bundles": []}` | p14 |
  | GET `/received` | 200 `{"release_bundles": [], "total": 0}` | p15 |
  | GET `/records/{name}` | 200 `{"release_bundles": [], "total": 0, "limit": 1000, "offset": 0}`（**分页三键信封**） | p18 |
  | GET `/received/{name}` | 200 `{"versions": [], "total": 0}` | p19 |
  | GET `/received/{name}/{version}` | **400** `Bad request: [`Release Bundle name must begin with a {letter | _ | digit} and consist of {letters | _ | . | - | digits}`]`（合法名也 400——疑无 GET 路由落进名字校验器，待验证清单 #6） | p20/p23 |
  | GET `/records/{name}/{version}` | 404 `Path not found: release-bundles-v2/<name>/<version>/release-bundle.json.evd`（**泄露 v2 存储布局三段路径 + .evd 证据文件名**） | p17/p24 |
  | GET `/statuses/{name}/{version}` | 404 `Record not found, repository: release-bundles-v2, name: <name>, version: <version>` | p16/p25 |
  | DELETE `/received/{name}/{version}` | 404 `Record not found, repository: release-bundles-v2-jfds, …`（**另一仓名带 -jfds 后缀**） | p60 |
  | POST `/api/v2/audit` body `{}` | 400 `Bad request: [`'event_summary' must be non-null`, `'event_status' must be non-null`, `'release_bundle_version' must be non-null`, `'subject_reference' must be non-null`, `'release_bundle_name' must be non-null`, `'subject_type' must be non-null`, `'created_by' must be non-null`]`（**审计面七必填字段清单**） | p26 |
  | GET `/api/v2/audit` | 405（audit 仅 POST） | p22 |

### 10.7 R06 权限矩阵与 ANY DISTRIBUTION 桶（置信度：高——活体 + 反编译双源）

非 admin 用户（l026r-d08-user，无任何权限授予）实测：

| 端点 | 非 admin 响应 | 锚 |
|---|---|---|
| GET `/release/bundles` | 200（user 角色放行） | p50 |
| POST `/release/bundle` | 400 校验消息（**达校验臂非 403**——user 角色放行） | p51 |
| PUT `/release/store` | 403 `{"errors":[{"status":403,"message":"Forbidden"}]}`（admin 门） | p52 |
| GET `/release/bundles/config` | 403 同上 | p53 |
| GET `/bundle/transaction/async/close/status/*` | 403 同上（admin 门） | p54 |
| GET `/release/fat_manifest_content/*` | 403 同上（admin 门） | p55 |

- 匿名（实例关匿名访问）：全域 401 `{"errors":[{"status":401,"message":"Authentication is required"}]}`（p56–p58）；反编译的匿名 403 裸响应分支仅在开匿名的实例可达（中）。
- **ANY DISTRIBUTION 桶非 REST 实体**：GET `/api/security/permissions` 名单仅 `["Any Remote", "Anything"]`（p61/p62）；单查 `Any Distribution` / `Any Local` 均 404 `Not Found`；`Anything` → `repositories: ["ANY"]`、`Any Remote` → `["ANY REMOTE"]`。结论：`ANY DISTRIBUTION` 仅为授权回退链内部常量 + UI 预置行（§5 维持），**不存在对应 REST 权限目标资源**——BinFlow 对位不得把它落成可 REST 读取的 permission target。
- **distribution 类型仓不可经 REST 创建**：PUT `/api/repositories/{key}` rclass=distribution → 400 `Unsupported repository type 'distribution' or media type 'application/json'`（活体 p63）；反编译 REST 仓建面接受集 = local/remote/virtual/federated/**releaseBundles**（releaseBundles 需 Local 配置媒体类型）——distribution rclass 属 RepoType 枚举但不入 REST 创建面。
