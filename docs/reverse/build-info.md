# Build-info 域 行为规格（M17 FR-152 前置锚，T-488）

> **定位**：Build-info 域 REST 表面 + 数据模型 + 权限面 + promotion 状态机 + retention 形态 + Docker promote 语义 + webhook/AQL 联动清单 + OSS 档可用性档位核验（ADR-0033 槽联动、ADR-0045 软缝对拍输入）。
>
> **取证状态（如实登记，2026-09-06）**：Q10 活体基线双损坏（pro 7.161 router 不拉起 ×2 轮；t226 7.84.10 access→PG 连接拒绝 ×3 轮含依赖顺序修正——修复尝试留痕见 `reports/agents/T-488.md` §0）。**本轮零活体**；置信度按双书面源（产品内嵌一手 OpenAPI〔reverse-src `rest/resource/build/openapi.yaml`，996 行，产品自维护〕+ JFrog 官方 REST 参考页〔docs.jfrog.com/integrations/reference/*，2026-09-06 实取〕）或（一手 OpenAPI/产品 schema + 反编译行为）评「高」；单一反编译源评「中」；推测评「低」。既有 t226 活体证据（T-407 会话 2026-09-01）凡覆盖本域条目者直接引用。**零静默升格**。
>
> **效力序**（ADR-0045 条款）：用户裁决（BOARD）> 本规格（含 as-built 段）> ADR-0045 > PRD 暂行值。

## 0. 与 ADR-0045 的软缝对拍（十项逐答，Accepted 期复核输入）

| # | ADR 软缝项 | 本规格定案（详节） | 与 ADR 骨架差异 |
|---|---|---|---|
| ① | 表族列集与 wire JSON 字段集 | §3 字段集表（builds 9+id / modules 2+id / artifacts 5 / dependencies 6 / promotions 6 / properties 3） | 列名字面以本表为准（ADR 骨架仅钉主键/外键） |
| ② | build_repo 缺省值与指定参数形态 | 缺省 **`artifactory-build-info`**（产品 schema DDL DEFAULT + webhook §3.4 示例 + `getPreferredBuildRepo` 回落三证，高）；指定参数 = `buildRepo`（append/dependency）/ `project`（上传/查询单/批量删/promote/rename/retention）——§1 表 | ADR 暂行值**证实** |
| ③ | 上传/append 端点方法与路径 | 上传 = **`PUT /api/build`**（name/number 在 body，不在路径）；append = **`POST /api/build/append/{name}/{number}`**（数组 body）——§1 表，双源高 | **ADR 骨架 `PUT /api/build/{name}/{number}` 与官方不符**（勘误回填项：上传路径不带 name/number；append 是 POST 非 PUT） |
| ④ | append 合并细则 | module 按 `id` 合并进父 build；body = BuildModule **数组**（一手 OpenAPI + 官方参考页双源，高）；「引用子 build」形态 = module id = `<子build名>/<子build号>`（官方 CLI 页，高）——§2.2 | 合并键 = module id（ADR 骨架方向证实） |
| ⑤ | promotion 状态机字面 | status = **自由字符串**（无闭集枚举，产品模型不约束）；promotions = **append-only 历史**，现势状态 = 按时间戳取最新一条；字段六元组 status/timestamp/comment/repository/ciUser/user——§2.4 | 「状态值集」答案 = 无闭集（ADR 待答项关闭） |
| ⑥ | retention 参数形态 | body = `{deleteBuildArtifacts, count, minimumBuildDate, buildNumbersNotToBeDiscarded[]}`；query `async`（缺省 true）+ 系统属性 `build.retention.always.async`（缺省 false）；「设定保留不立即删」（官方明文）——§2.5 | ADR「count/days/buildNumbers/minimum retained days」中 days 实为 **minimumBuildDate**（ISO 时间戳，非天数）——字面修正 |
| ⑦ | 「最新」端点形态 | **无专用 latest 端点**；`GET /api/build` 每名回 `lastStarted`、`GET /api/build/{name}` 回 numbers+started，最新 = 按 started 倒序取首（排序语义：中——回显顺序未文档化）——§2.1 | ADR「排序取首 vs 专用端点」定案 = 排序取首（无专用端点） |
| ⑧ | 错误文案逐字样本 | 逐字可得：`/api/search/buildArtifacts` 三条 400 + 404 文案、docker promote 全族文案（§4）；**上传/promotion/retention 的逐字错误文案不可得**（REST 实现类未在反编译集合内）——§5 待验证 | 部分覆盖，如实登记 |
| ⑨ | delete 端点参数族 | `DELETE /api/build/{name}?buildNumbers=<csv>&artifacts=0|1&deleteAll=0|1` + `POST /api/build/delete`（body `{project, buildName, buildNumbers[], deleteArtifacts, deleteAll}`，6.13+，支持特殊字符）——§1 表 | ADR 骨架 `?builds=&dateRange=` 字面**有误**（实际 buildNumbers/artifacts/deleteAll，无 dateRange）——勘误回填项 |
| ⑩ | 档位核验结论 | 证据指向 Artifactory 侧行为 = **JCR/Pro 档门控**（官方描述三处 Pro/JCR + t226 旧会话 build 搜索族 400 Pro 门）；Builds 页 nav 在 OSS 在场（console-ui 高）——§6。**不翻转 ADR-0045 点 11 的 community 地板**（BinFlow 无许可门先例 = 超集实现不违 parity），但「OSS 发行集合含 build CRUD 全族」的推断依据需修正为「openapi.yaml 随 OSS 代码库发行 ≠ 端点在 OSS 档可用」——勘误注记 | 勘误注记（机制轴零翻动） |

---

## 1. REST 端点族（置信度：高——产品内嵌一手 OpenAPI + 官方参考页双源逐字，除单独标注者）

Base：`/artifactory/api`。认证：Basic / Bearer JWT 双收。产品媒体类型族（`BuildRestConstants`，补充官方规范）：`application/vnd.org.jfrog.build.Builds+json`（列表）、`Build+json`（单 build）、`BuildsByName+json`、`BuildsDiff+json`、`BuildPatternArtifactsRequest/Result+json`、`BuildArtifactsRequest+json`、`PromotionRequest/Result+json`（端点实际同时收发裸 `application/json`）。

| 方法 | 路径 | 参数 | 成功响应 | 错误响应 | 语义要点 | 置信度 |
|---|---|---|---|---|---|---|
| GET | `/build` | `?projectKey=` | 200 `{uri, builds:[{uri, lastStarted}]}`（每 build **名**一行，uri = `/<buildName>` 相对形态） | 401/403 | 全部 build 名清单；`lastStarted` = 该名最新一次 run 的 started | 高 |
| PUT | `/build` | `?project=` | 200（空体） | 400 畸形 body / 401 / 403 | **全量上传**：body = build info JSON（name/number 在 body）；覆盖既有 name+number 需 **delete 权**（官方权限注记）；modules 须带正确 sha1/md5 才与制品关联 | 高 |
| GET | `/build/{buildName}` | `?projectKey=` | 200 `{uri, buildsNumbers:[{uri, started}]}`（uri = `/<number>`） | 401/403/404 | 某 build 名下的全部 run 号清单 | 高 |
| DELETE | `/build/{buildName}` | `?buildNumbers=<csv>&artifacts=0\|1&deleteAll=0\|1` | 200 text/plain：`The following builds has been deleted successfully: 'name#51', 'name#52'.`（官方例文，含历史语法瑕疵「has」） | 401/403/404 | `deleteAll=1` 全删；`artifacts=1` 连制品删；**Requires Artifactory Pro**（官方） | 高 |
| GET | `/build/{buildName}/{buildNumber}` | `?started=&diff=&project=` | 200 `{uri, buildInfo:{…}}` | 401/403/404 | 单 build 详情；`started`（`yyyy-MM-dd'T'HH:mm:ss.SSSZ`）用于同名同号多 run 消歧；`diff` = 旧号对比（Builds Diff）；**Requires JFrog Container Registry or Artifactory Pro**（官方）。实际回显 buildInfo 含 `statuses`（promotion 历史）数组——OpenAPI schema 未列此字段（schema 缺项，中） | 高（statuses 回显：中） |
| POST | `/build/append/{buildName}/{buildNumber}` | `?started=&buildRepo=&project=` | **204** 空体 | 400 / 401 / 403 / **404 `Build-Info not found`** | **段合并**：body = BuildModule **数组**；模块按 id 并入父 build，不覆盖既有模块；权限 = Deploy ∧ Delete（官方） | 高 |
| POST | `/build/promote/{buildName}/{buildNumber}` | `?project=` | 200 `{messages:[{level, message}]}`，level ∈ error/warning/info | 401/403/404 | promotion，body 见 §2.4 | 高 |
| POST | `/build/delete` | —（body 承载） | 200 text/plain（同 DELETE 族文案） | 400/401/403 | 批删（6.13+）：body `{project, buildName, buildNumbers[], deleteArtifacts, deleteAll}`；**支持 build 号含特殊字符**（官方明示——这是该端点独立存在的理由） | 高 |
| POST | `/build/rename/{buildName}` | `?to=<new>`（必填）`&project=` | 200 text/plain：`Build renaming of 'x' to 'y' was successfully started.` | 400/401/403/404 | **Requires Artifactory Pro**；异步语义（文案「was successfully started」） | 高 |
| POST | `/build/retention/{buildName}` | `?async=`（缺省 true） | 200 | 400/401/403/404 | 设保留参数，body 见 §2.5；**不立即删**（官方明文） | 高 |
| POST | `/docker/{repoKey}/v2/promote` | —（body 承载） | 200 text/plain `Promotion ended successfully` / 部分 206 | 400/401/403/404 | Docker 镜像晋升，语义见 §2.6；另有 legacy `POST /docker/{repoKey}/v1/promote` 与 `DELETE /docker/{repoKey}/v2/delete` 同族 | 高 |

**M17 面内/面外切分**（对齐 ADR-0045 点 6）：M17 七族 = 上传/append/查询单/列表族/批删/promote/retention；**面外登记** = rename、diff（查询参数）、docker promote 独立端点、projectKey 过滤族（BinFlow 无 projects 域，参数面不承接）。

## 2. 语义流程

### 2.1 查询族（单·列表·最新）

- 列表两跳：`GET /build`（名清单+lastStarted）→ `GET /build/{name}`（号清单+started）→ `GET /build/{name}/{number}`（详情）。URI 回显为**相对路径形态**（`/<buildName>`、`/<number>`），非绝对 URL（官方例证）。
- 「最新」语义：无专用端点；`lastStarted`/`buildsNumbers` 的 started 时间戳即排序依据。builds 唯一性 = (name, number, started, repo) 四元（§3 builds 表 UNIQUE 索引）——**同名同号不同 started 是不同 run**，`started` 查询参数即消歧键。
- `diff` 参数（GET 单详情）触发 Builds Diff 输出（对比形态官方未展开——低，待活体）。

### 2.2 PUT 上传与 append 段合并

- **PUT 全量上传**：body = 完整 build info JSON（§3 字段集）。重复上传同名同号 = 覆盖（需 delete 权；官方权限注记）。modules 的 artifacts 携 sha1/md5 方可与仓内制品关联（官方明示「correct SHA1 and MD5 to be properly linked」）；无 checksum 的 artifact 行只入记录不建关联。
- **append 段合并**（POST，数组 body）：
  1. 父 build (name, number[, started, buildRepo, project]) 必须已存在——不存在 404 `Build-Info not found`（逐字）。
  2. 数组逐模块并入：**module 按 `id` 合并**（同 id = 同模块追加 artifacts/dependencies；ADR-0045 软缝④的合并键）；不传则新增模块。
  3. 引用形态（官方 CLI `jf rt ba` 语义）：module id = `<被引用 build 名>/<被引用 build 号>`——「聚合 build」通过引用模块收纳子 build，被引用 build 须**已发布**（CLI 前置校验）。
  4. 成功 204 无 body。
- 覆盖 vs 合并语义分界：PUT = 整 build 替换；append = 模块级增量合并——两出口并存（官方设计）。

### 2.3 buildinfo 仓直传语义（PUT 进 build-info 仓的文件面；置信度：中——仅反编译，补充官方规范）

PUT 进 build-info 类型仓（含缺省 `artifactory-build-info`）的 `.json` 文件会被内部转发到 build 上传链路（`/api/build/buildUploadRedirect/<原路径>` 内部 forward）：

- 非 `.json` 扩展名的 PUT → **409** 拒绝（文案形如 `The '<repo>' repository rejected the deployment of '<path>'. Only valid Build Info .json files are supported.`）。
- JSON 可解析但路径与 build 坐标推导路径不符 → **静默忽略**（不入 build 索引，文件照存）。
- 重复 PUT 同一 build JSON（覆盖文件）→ 先删 DB 既有记录再重建（override 语义）；系统属性可强制按 DB 存在性 override。
- 删除 buildinfo 仓里的 build JSON 文件 → 级联删除对应 build DB 记录（解析失败时按路径坐标回退删除）。
- buildinfo 仓内 **copy/move 全拒**（400 `Copy and Move operations are not allowed within the Build Info repository`）；从 buildinfo 仓 copy/move 出去也拒；copy/move 进 buildinfo 仓仅当目标路径 == build 坐标推导路径（否则 400 bad-path 文案）。
- BinFlow 对位注记：ADR-0045 载体轴 A（build 记录不进 storage/nodes）——本节为 Artifactory 形态记录，BinFlow 不承接文件面直传（REST 面 `PUT /api/build` 为唯一入口）。

### 2.4 Promotion 状态机

- 请求 body（高，双源）：`status`（新状态，可省 = 状态-only 促销可省 targetRepo）、`comment`、`ciUser`、`timestamp`（ISO8601 `yyyy-MM-dd'T'HH:mm:ss.SSSZ`）、`dryRun`（缺省 false，全链校验零落变更）、`sourceRepo`（缺省自动解析）、`targetRepo`（5.7+ 可为 virtual）、`copy`（缺省 false = **move**）、`artifacts`（缺省 true）、`dependencies`（缺省 false）、`scopes[]`（dependencies=true 时生效，additive OR）、`properties`（挂在被促销制品上，与 targetRepo 无关）、`failFast`（缺省 true）。官方 schema 将 ciUser/timestamp/copy/artifacts/dependencies/failFast 标 required（schema 生成器痕迹，实际行为按缺省值容忍——低置信，待活体）。
- **状态机**：
  - status = **自由字符串**，产品不约束值集（无枚举、无迁移表）——UI 惯例值（如 staged/rolled-up/released）是约定不是协议。
  - 每次 promote **追加**一条 promotion 记录（六元组：status/timestamp/comment/repository/ciUser/user）——append-only 历史，不可改。
  - 现势状态 = **按 timestamp 取最新一条**（产品模型明示 max-by-timestamp）。
  - `started` 字段不随 promotion/replication 变化（官方明示 immutable）。
- 成功响应 `{messages:[{level: error|warning|info, message}]}`——部分失败（failFast=false）时 warning/error 行与 200 并存。
- 权限（官方）：promote 需 build 的 Deploy 权；BinFlow 门 = ADR-0045 点 5（w(targetRepo) ∧ r(buildRepo)）。
- 缺制品语义（ADR-0045 待答项）：官方未文档化逐字行为；BinFlow 定案 = failFast 时 4xx 拒、否则跳过（ADR-0045 点 5，C 层自有细则）。

### 2.5 Retention 形态

- body：`deleteBuildArtifacts`（bool，删 build 时连制品删）、`count`（int，最多保留 N 个 build）、`minimumBuildDate`（**ISO8601 时间戳**，早于此的 build 可删——非天数）、`buildNumbersNotToBeDiscarded[]`（豁免号清单）。
- `async` query（缺省 **true**）+ 系统属性 `build.retention.always.async`（缺省 false，置 true 则 sync 请求也转异步）。相关系统属性族（反编译，补充官方规范）：`build.retention.enabled`（true）/`build.retention.workers`（10）/`build.forced.delete.artifacts`（true）/`build.block.duplicate.entries`（false）/`build.search.maxBatchSize`（5000）/`build.info.manifest.file.size.limit.bytes`（700MB）/`build.ui.skip.delete.permission.check`（false）。
- **设定保留 ≠ 立即删除**（官方明文）——retention 参数挂在 build 名上，后续删除由保留窗驱动。
- 权限 = build 的 delete 权（官方）。

### 2.6 Docker promote 语义（`POST /api/docker/{repoKey}/v2/promote`；置信度：高——一手 OpenAPI 端点面 + 反编译行为）

body：`targetRepo`*、`dockerRepository`*、`targetDockerRepository`（缺省同 dockerRepository）、`tag`（缺省 = 整仓晋升）、`targetTag`（缺省同 tag）、`copy`（缺省 false = **move**）。

- 校验链（逐字文案，反编译）：
  1. body 缺 dockerRepository → 400 `You must provide dockerRepository name`；缺 targetRepo → 400 `You must provide targetRepo`。
  2. 源/目标仓必须 docker 支持（v2）→ 否则 400 `Unsupported V2 repository request for '<repo>'`。
  3. 同仓无 tag（非 retag 场景）→ 400 `Skipping promote since destination and source are the same`；同仓 + tag（retag）放行。
  4. `targetTag`/`targetDockerRepository` 存在而 `tag` 空 → 400 `When 'targetTag' or 'targetDockerRepository' exists, 'tag' cannot be empty`；re-tag 仅 v2 →（v1 端点）400 `Promotion with re-tag is only supported on Docker V2 repositories`。
  5. 源 manifest 必须存在（`manifest.json` 不在则探 `list.manifest.json`）→ 缺 404。
- 执行语义：逐 manifest 拷贝目录树 + **属性随迁**（源属性写到目标 + 父路径补属性）；`copy=false` 时拷贝后删源（源尚有他引用则保留——manifest 复用判定）；执行前**清理目标既有同名文件**（重复 promote = 覆盖）；全程持 `docker_promotion` 分布式锁（超时 400 `Failed to acquire lock for promotion of '<path>' from '<repo>' to '<repo>'.`）。
- 权限链：目标 w（403 `No permission to write to the target repository '<repo>'. Promotion aborted`）∧ 源 r（403 `No permission to read from the source repository…`）∧ 目标 annotate（403 `User doesn't have permissions to annotate/write in '<path>'. Promotion aborted`）。
- 响应：成功 200 text/plain `Promotion ended successfully`；**源删除部分失败 206** `Promotion executed successfully; however, there were not enough permissions to delete all artifacts under '<repo>/<image>/<tag>'.`；找不到镜像 404（`Unable to find '<path>'. Promotion aborted` / `Unable to find docker image '<path>', request aborted`）。
- 输入消毒：全部参数过非法字符剥离。
- **M17 处置**：独立端点不进 M17（ADR-0045 点 6）——镜像晋升经 build promote 的 targetRepo 直达 docker 仓（T-509 腿）；本节为远期翻案锚。

## 3. 数据模型字段集（表族）

### 3.1 wire JSON 字段集（build info；置信度：高——一手 OpenAPI schema + 官方参考页）

顶层：`version` / `name` / `number` / `type`（MAVEN|GRADLE|ANT|IVY|GENERIC）/ `buildAgent{name,version}` / `agent{name,version}` / `started`（`yyyy-MM-dd'T'HH:mm:ss.SSSZ`）/ `artifactoryPluginVersion` / `durationMillis` / `artifactoryPrincipal` / `url` / `vcs[]{revision,message,branch,url}` / `licenseControl{runChecks,includePublishedArtifacts,autoDiscover,scopesList,licenseViolationsRecipientsList}` / `buildRetention{deleteBuildArtifacts,count,minimumBuildDate,buildNumbersNotToBeDiscarded[]}` / `modules[]` / `issues{tracker{name,version},aggregateBuildIssues,aggregationBuildStatus,affectedIssues[]{key,url,summary,aggregated}}` / `properties`（环境变量/属性 map）。实际回显另含 `statuses[]`（promotion 历史六元组，§2.4；schema 缺项——中）。

module：`properties` / `id` / `type` / `artifacts[]` / `dependencies[]`。
artifact：`type` / `sha1` / `sha256` / `md5` / `name` / `path` / `originalDeploymentRepo`。
dependency：`type` / `sha1` / `sha256` / `md5` / `id` / `scopes[]` / `requestedBy[][]`。

### 3.2 存储表族（产品 DDL 逐字，`postgresql.sql`；置信度：高——产品 schema 文件；补充官方规范）

```
builds(           build_id PK, build_name, build_number, build_date(epoch-ms), ci_url,
                  created, created_by, modified, modified_by,
                  repo DEFAULT 'artifactory-build-info', immutable )
                  UNIQUE(build_name, build_number, build_date, repo)        ← run 唯一键四元
build_promotions( (build_id, created) PK, created_by, status, repo, promotion_comment, ci_user )
build_props(      prop_id PK, build_id FK, prop_key, prop_value )
build_modules(    module_id PK, build_id FK, module_name_id, type )
build_artifacts(  artifact_id PK, module_id FK, artifact_name, artifact_type, sha1, md5 )   ← sha1/md5 各有索引（关联查询面）
build_dependencies( dependency_id PK, module_id FK, dependency_name_id, dependency_scopes, dependency_type, sha1, md5 )
module_props(     prop_id PK, module_id FK, prop_key, prop_value )
build_release_bundles( build_rb_id PK, build_id FK ON DELETE CASCADE, bundle_repository, bundle_name, bundle_version )
```

要点：① build↔bundle 有专门关联表（§联动）；② module/dependency 名入 `*_name_id` 列（名字规范化存储）；③ AQL builds 域字段 = 本表族的直投影（aql.md §15 字段集与此一一对应）；④ buildinfo 仓里的 build JSON 文件布局 = `<buildName>/<buildNumber>/…json`（**低**——路径推导在外部库 `generateBuildJsonRepoPath`，未反编译；唯一键四元与 webhook `build_repo` 示例旁证）。

## 4. 老搜索两入口 wire 锚（详表归 aql.md §15.4）

- `POST /api/search/buildArtifacts`（RolesAllowed user/admin；body `{buildName*, buildNumber XOR buildStatus, …}`）：缺名 400 `Cannot search without build name.`；号/状态双缺 400 `Cannot search without build number or build status.`；双给 400 `Cannot search with both build number and build status parameters, please omit build number if your are looking for latest build by status or omit build status to search for specific build version.`（**逐字含「your」拼写**——产品原文）；命中 200 `{"results":[{"downloadUri":…}]}`（**downloadUri 键**，非 uri）；空 404 `Could not find any build artifacts for build '<name>' [number '<n>'|status '<s>']`。（文案：反编译逐字，中；端点面+400 Pro 门：官方 + t226 活体，高）
- `GET /api/search/dependency?sha1=&sha256=&buildRepo=&project=`（user/admin）：按 checksum 反查「哪些 build 依赖了它」；命中 200 `{"results":[{"uri":…}]}`（uri 键，指向 build API 形态）；非法参数 400 BadRequestException。（同上双置信度分层）

## 5. 与公开规范的差异/补充（「此条补充官方规范」）

- 官方参考页未载、仅反编译/产品 schema 可见：buildinfo 仓直传/copy-move 拒绝族（§2.3）、build 系系统属性族（§2.5）、docker promote 错误文案与 206 部分成功（§2.6）、buildArtifacts/dependency 搜索逐字文案（§4）、表族 DDL（§3.2）、buildinfo 仓在 ANY LOCAL/REMOTE/DISTRIBUTION/Anything 预置目标回退链中被**排除**（§7）。
- 官方与产品的分歧登记：官方参考页将 promotion body 六字段标 required（§2.4 低置信注记）；append 的 body 历史上曾为单对象形态，现行官方 + 一手 OpenAPI 均为数组（以数组为准）。

## 6. OSS 档可用性档位核验（ADR-0033 槽联动 / ADR-0045 点 11 勘误输入）

| 证据 | 内容 | 置信度 |
|---|---|---|
| console-ui.md §1.1（t226 OSS 7.84.10 活体 2026-09-01） | Builds **nav 项在 OSS 在场**（Application → Packages/Builds/Artifacts）；权限编辑器有 `Edit Builds` 与 User Permissions 的 Builds tab | 高 |
| aql.md §8.2/§2.1（t226 同会话） | `POST /api/search/buildArtifacts`、`GET /api/search/dependency`、AQL build 系五入口在 OSS **全部 400 Pro 门**（`This REST API is available only in Artifactory Pro…`） | 高 |
| 一手 OpenAPI 描述字段 | GET build info「Requires JFrog Container Registry or Artifactory Pro」；DELETE builds / rename / retention「Requires Artifactory Pro」 | 高 |
| 本轮活体 | **不可得**（双基线损坏，§取证状态）——GET/PUT `/api/build` 本体在 OSS 档的通/门行为**未直接实证** | — |

**结论**：Artifactory 侧行为证据整体指向 **build 数据面 = JCR/Pro 商业档**（nav 在场 + 数据端点 Pro 门 + 官方描述三处）；`openapi.yaml` 随 OSS 代码库发行 ≠ 端点在 OSS 档可用。BinFlow 侧档位归属（community 地板、无槽）是**自有设计**（无许可门 = 超集实现，aql.md §8.2 档位口径先例），维持 ADR-0045 点 11 不翻案；「OSS 发行集合含 build CRUD 全族」的推断表述建议勘误为「代码库在场、运行时门控」。Builds 页内容行为（空态/license 提示形态）未实证——待验证清单 #1。

## 7. 权限面两出口（交 ADR-0045——已裁 A；此处为 Artifactory 侧真值记录）

- **出口 B（Artifactory 实态——独立 build 权限面）**（置信度：高——产品模型 + t226 UI 双证）：
  - permission target 模型含**可选 `builds` 段**（`PermissionTargetModel.build`，RepoPermissionTarget 形态 = include/exclude Ant 模式对 **build 名**生效）；UI 权限编辑器 `Edit Builds` 两步弹窗（console-ui.md §3.8 t226 活体）。
  - 系统预置 build 权限目标常量 `artifactory-system-default-build-permission`（其授予语义不可考——中）。
  - 6.6+ 语义（官方）：read/deploy/delete permission **for the build**（区别于仓库权限动词）。
  - buildinfo 仓（文件面）的 ACL 走仓库权限，但**被排除在 ANY LOCAL/ANY REMOTE/ANY DISTRIBUTION/Anything 预置回退链之外**（授权序：精确仓 ACL → buildinfo/release-bundle 仓**短路拒绝** → ANY LOCAL → ANY REMOTE → ANY DISTRIBUTION → ANY）；buildinfo 仓路径匹配前先 URL 解码。
- **出口 A（BinFlow 已裁，ADR-0045 点 4）**：仓库级 allow() 同源，path 位传 build_name，五动词闭集——Artifactory 的「按 build 名模式授权」语义零新面可得。
- 对拍注记：两出口表达力等价面 = build 名模式授权；Artifactory 用独立段承载、BinFlow 用 path 位承载——语义覆盖不损失（ADR-0045 已论证）；本节不构成翻案输入。

## 8. webhook·AQL 联动清单

- **webhook 三事件投影**（webhook.md §3.4 已冻结，envelope/data 字段零发明）：`uploaded` / `deleted` / `promoted`；data = `build_name, build_number, build_started`（`1970-01-01T00:00:00.000+0000` 形态）, `build_repo`（示例 `artifactory-build-info`）。发射点对位（ADR-0045 点 7）：上传/append 成功→uploaded；删除/retention 成功→deleted；promote 成功→promoted。criteria 面 build scope（anyBuild/selectedBuilds/include-excludePatterns）= webhook.md §2 域表 build 行。
- **AQL 三入口字段集**（aql.md §15 冻结）：`builds` / `modules` / `dependencies` 字段集与 §3.2 表族一一对应；`artifacts(build)` / `build.promotions` / `build.properties` / `module.properties` 入口 M17 面外维持 400（翻转点 = T-511 票 + M18+ 翻转注记，aql.md §15.3）。
- **两老搜索端点**：§4（wire 锚随 aql.md §15.4 双挂）。

## 9. 待验证清单（低置信/未实证——零静默升格）

1. Builds 页在 OSS 档的**页面内容行为**（空态/升级提示/加载即 403）——nav 在场已证，内容未实证（本轮活体双损坏）。
2. promotion body 六「required」字段的**运行时强制度**（官方 schema required vs 产品缺省容忍）。
3. buildinfo 仓 build JSON 文件路径布局字面（`<name>/<number>/…json` 的第三段命名——`generateBuildJsonRepoPath` 在外部库未反编译）。
4. `GET /build/{name}` 回显 buildsNumbers 的**排序保证**（是否 started 倒序——「最新=取首」依赖此）。
5. builds diff（`?diff=` 参数）的响应形态（官方未展开）。
6. 上传/promotion/retention 的**逐字错误文案**（REST 实现类不在反编译集合内；§1 表错误码位是官方 schema 级别）。
7. `statuses[]` 在 GET 单详情回显中的完整字段形态（OpenAPI schema 缺项，产品模型旁证）。

## 10. 取证锚点（2026-09-06 会话）

- 一手 OpenAPI：reverse-src `src/batch1-core/rest/resource/build/openapi.yaml`（996 行，产品内嵌自维护规格 + agents.md 维护说明）——**注意此文件位于只读 reverse-src，行为摘录进本规格、文件不引用进实现**。
- 官方参考页（docs.jfrog.com/integrations/reference/，2026-09-06 实取）：`appendbuild.md` / `promotebuild.md` / `controlbuildretention.md` / `uploadbuild.md` / `getallbuilds.md` / `getbuildruns.md` / `getbuildinfo.md` / `deletebuilds.md` / `deletebuildsmultiple.md` / `renamebuild.md`。
- 官方指南页：`docs/artifactory/docs/build-integration.md`（CLI append 语义/引用模块形态）、`docs/aql-entities-fields-reference.md`（字段表）、`docs/aql-examples.md`（跨域路径例）。
- 反编译：`org/artifactory/build/*`（ReleaseStatus/PromotionConfig/BuildId/DetailedBuildRunImpl）、`rest/resource/build/BuildRestConstants.java`、`repo/interceptor/BuildInfoInterceptor.java`、`rest/resource/search/types/{BuildArtifactsSearchResource,DependencySearchResource}.java`、`batch2-protocol addon/docker/rest/DockerResourceBase.java + rest/v2/promotion/DockerV2Promoter.java`、`security/{PermissionTarget,permissions/PermissionTargetModel,AuthorizationServiceBase,LegacyAuthorizationServiceImpl}.java`、`common/ConstantValues.java`（build.* 键族）、`postgresql/postgresql.sql`（builds 表族 DDL）。
- 既有活体证据引用：t226 OSS 7.84.10（T-407 会话 2026-09-01）经 aql.md §2.1/§8.2 与 console-ui.md §1.1/§3.8 转引。
- 本轮活体：**零**（pro 7.161 与 t226 双损坏，修复尝试与终态见 reports/agents/T-488.md §0——容器均复原为 stopped 原状）。
