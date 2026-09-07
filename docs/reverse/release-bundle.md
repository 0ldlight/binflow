# Release Bundle 域 行为规格（M17 FR-153 前置锚，T-488）

> **定位**：Release Bundle（RBv1 为主、RBv2 留痕）端点族 + bundle 模型 + 冲突语义 + 状态机 + 深度边界选项面（Q2 材料——两出口并列）+ Any Distribution 预置语义 + 商业档位核验（ADR-0033 / ADR-0046 软缝对拍输入）。
>
> **取证状态（2026-09-06）**：与 build-info.md 同——**零活体**（双基线损坏，reports/agents/T-488.md §0）。置信度分层：「高」= 反编译 REST 资源类/产品 DDL + 官方文档双源；「中」= 单反编译源；「低」= 推测。Artifactory 侧 Release Bundle 本体依赖 **JFrog Distribution 服务**（独立部署单元）——Artifactory 内只驻「源 bundle 承接面」，本规格把两侧分开锚定。
>
> **效力序**（ADR-0046 条款）：用户裁决（BOARD）> 本规格 > ADR-0046 > PRD。

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
| GET | `/release/bundles` | `?type=source|target`（缺省 **TARGET**） | 200 `BundlesResponse`（名清单） | — | 全部 bundle 名 | 高 |
| GET | `/release/bundles/{name}` | `?type=` | 200 版本清单 | — | 某 bundle 的版本列表 | 高 |
| HEAD | `/release/bundles/{name}/{version}` | —（**恒 SOURCE**） | 200 + `X-Checksum-Sha256` 头（bundle JSON 的 sha256） | 404 无 body | 源 bundle 校验和探针 | 高 |
| GET | `/release/bundles/{name}/{version}` | `?format=jws&type=` | 200 bundle JSON（`application/json`）/ 签名 JWS（`format=jws` → `application/jose`） | 404 | 取 bundle 描述符（v2 可取签名形态） | 高 |
| GET | `/release/bundles/{name}/{version}/status` | `?type=` | 200 状态字符串 | — | bundle 状态（§3.2） | 高 |
| DELETE | `/release/bundles/{name}/{version}` | —（**恒 TARGET**） | 200 `{}` | — | 删目标侧 bundle 记录 | 高 |
| DELETE | `/release/bundles/source/{name}/{version}` | — | 200 `{}` | — | 删源 bundle 记录 | 高 |
| GET/PUT | `/release/bundles/config` | body `{incompleteCleanupPeriodHours}` | GET 200 / PUT **202** `Successfully updated release bundles config` | — | admin 门；未完结 bundle 清理周期 | 高 |
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
2. `BundlesResponse` / `BundleVersionsResponse` 的行级字段集（模型类在反编译集合内但字段读取未逐项核——中偏低）。
3. `CloseTransactionStatusResponse` 字段集（同上）。
4. `GET /release/bundles?type=source` 在 v2 bundle 存在时的回显混合形态。
5. 本轮活体全缺：RB 端点在 pro 7.161 的实弹验证（创建→冲突→状态流转→删除全链）待活体恢复后补（T-488 §0 留痕）。

## 9. 取证锚点（2026-09-06 会话）

- 反编译：`rest/resource/release/ReleaseBundleResource.java`（18 端点逐方法）、`api/rest/release/{ReleaseBundleRequest,SourceReleaseBundleRequest,ReleaseBundleResult,ReleaseBundlesConfigModel}.java`、`addon/release/bundle/ReleaseBundleAddonImpl.java`（storeBundle 冲突三态 + 缺省仓）、`addon/release/bundle/exception/ReleaseBundleAlreadyExistsException.java`、`bundle/BundleType.java`（SOURCE/TARGET 缺省 TARGET）、`rest/common/exception/mapper/AlreadyExistsExceptionMapper.java`（409 + ErrorResponse）、`security/{PermissionTarget,AuthorizationServiceBase,LegacyAuthorizationServiceImpl}.java`（ANY DISTRIBUTION + 排除链）、`util/bundles/BundleConfigurationUtil.java`（v2 仓后缀/证据文件）、`postgresql/postgresql.sql`（artifact_bundles/bundle_files DDL）、`rest/common/resource/release/bundle/ReleaseBundleResourceFilter.java`。
- 官方（docs.jfrog.com，2026-09-06 实取）：`docs/artifactory/docs/create-release-bundles-v1.md`（签名/规格/限制）、`docs/artifactory/docs/release-bundle-repositories.md`（缺省仓/Enterprise+）、`docs/artifactory/reference/llms.txt` + `docs/integrations/reference/llms.txt`（Distribution v1/v2 全族索引页名）。
- 官方 OSS 源码（用户提供发行树，绝对路径只读）：`build-handler/build-handler-acl/.../util/BuildConstants.java`——`DEFAULT_RBV2_REPO = "release-bundles-v2"`（v2 缺省仓名逐字）、`PACKAGE_TYPE_RB = "releasebundles"`（§2.2 v2 形态字面的官方常量印证）。
- 既有仓内锚：aql.md §2.1（releases/release_artifacts 入口 400 行）、webhook.md §3.5~3.7（九型 dormant）、console-ui.md §3.8（Any Distribution 预置行，t226 活体）、inv-4 distribution 族（档位对照）。
- 本轮活体：零（同 build-info.md 取证状态）。
