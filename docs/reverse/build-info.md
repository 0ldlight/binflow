# Build-info 域 行为规格（M17 FR-152 前置锚，T-488）

> **定位**：Build-info 域 REST 表面 + 数据模型 + 权限面 + promotion 状态机 + retention 形态 + Docker promote 语义 + webhook/AQL 联动清单 + OSS 档可用性档位核验（ADR-0033 槽联动、ADR-0045 软缝对拍输入）。
>
> **取证状态（如实登记，2026-09-06）**：Q10 活体基线双损坏（pro 7.161 router 不拉起 ×2 轮；t226 7.84.10 access→PG 连接拒绝 ×3 轮含依赖顺序修正——修复尝试留痕见 `reports/agents/T-488.md` §0）。**本轮零活体**；置信度按双书面源（产品内嵌一手 OpenAPI〔reverse-src `rest/resource/build/openapi.yaml`，996 行，产品自维护〕+ JFrog 官方 REST 参考页〔docs.jfrog.com/integrations/reference/*，2026-09-06 实取〕）或（一手 OpenAPI/产品 schema + 反编译行为）评「高」；单一反编译源评「中」；推测评「低」。既有 t226 活体证据（T-407 会话 2026-09-01）凡覆盖本域条目者直接引用。**零静默升格**。
>
> **L023-1 增补（2026-09-15）**：§11 起为本轮核对补强——**服务端实现源首次可达**（用户提供的 jfrog-artifactory 7.161.16 OSS 发行源码树内 `build-handler/` 模块 = build REST 的服务端 command/service/DAO/资源层全套，T-488 期误判「实现疑在未开源模块」，§10 末条勘误）+ **活体基线首次可用**（172.16.58.130:8082，pro 7.161.15，admin）。凡 §11 条目均以「源码（7.161.16）+ 活体（7.161.15）」双源印证评「高」；仅源码单源评「中」；与 §1–§8 旧稿冲突处以 §11 勘误表为准（冲突已显式登记，见 §11.1）。
>
> **L023-2D 回写（2026-09-15）**：差分收官报告（`reports/compatibility/L023-buildinfo-diff.md` §7）以**活体双端证据推翻 L023-1 的七处源码单读**——勘误 E14-E20 已并入 §11.1 与正文（promote 400 两面性/收集双通道/slim 键省略/count 门 label/404 空格字面/名清单升序/buildRepo 读写门分野），证据锚 `l023d-wire`。
>
> **效力序**（ADR-0045 条款）：用户裁决（BOARD）> 本规格（含 as-built 段）> ADR-0045 > PRD 暂行值。

## 0. 与 ADR-0045 的软缝对拍（十项逐答，Accepted 期复核输入）

| # | ADR 软缝项 | 本规格定案（详节） | 与 ADR 骨架差异 |
|---|---|---|---|
| ① | 表族列集与 wire JSON 字段集 | §3 字段集表（builds 9+id / modules 2+id / artifacts 5 / dependencies 6 / promotions 6 / properties 3） | 列名字面以本表为准（ADR 骨架仅钉主键/外键） |
| ② | build_repo 缺省值与指定参数形态 | 缺省 **`artifactory-build-info`**（产品 schema DDL DEFAULT + webhook §3.4 示例 + `getPreferredBuildRepo` 回落三证，高）；指定参数 = `buildRepo`（append/dependency）/ `project`（上传/查询单/批量删/promote/rename/retention）——§1 表 | ADR 暂行值**证实** |
| ③ | 上传/append 端点方法与路径 | 上传 = **`PUT /api/build`**（name/number 在 body，不在路径）；append = **`POST /api/build/append/{name}/{number}`**（数组 body）——§1 表，双源高 | **ADR 骨架 `PUT /api/build/{name}/{number}` 与官方不符**（勘误回填项：上传路径不带 name/number；append 是 POST 非 PUT） |
| ④ | append 合并细则 | ~~module 按 `id` 合并进父 build~~ **L023-1 勘误**：append = 模块列表**直接拼接**，同 id 模块产生**重复模块条目**（源码 + 活体双证，§11.4）——合并键论废；「引用子 build」= 客户端约定（module id 形如 `<子build名>/<子build号>`），服务端不识别 | **软缝④原答案被证伪**——ADR-0045 Accepted 期复核输入 |
| ⑤ | promotion 状态机字面 | status = **自由字符串**（无闭集枚举，产品模型不约束）；promotions = **append-only 历史**，现势状态 = 按时间戳取最新一条；字段六元组 status/timestamp/comment/repository/ciUser/user——§2.4 | 「状态值集」答案 = 无闭集（ADR 待答项关闭） |
| ⑥ | retention 参数形态 | body = `{deleteBuildArtifacts, count, minimumBuildDate, buildNumbersNotToBeDiscarded[]}`；query `async`（**缺省 false=同步**，E9）+ 系统属性 `build.retention.always.async`（缺省 false）；~~「设定保留不立即删」（官方明文）~~ **L023-1 勘误**：7.161 实际 = POST 即触发删除（缺省同步删完再回；async=true 后台），官方表述过时（§11.6） | ADR「count/days/buildNumbers/minimum retained days」中 days 实为 **minimumBuildDate**（ISO 时间戳，非天数）——字面修正；**即删性修正** |
| ⑦ | 「最新」端点形态 | **无专用 latest 端点**；`GET /api/build` 每名回 `lastStarted`、`GET /api/build/{name}` 回 numbers+started，最新 = 按 started 倒序取首（**L023-1 落定**：倒序已双源证实，原「中」→「高」）——§2.1 | ADR「排序取首 vs 专用端点」定案 = 排序取首（无专用端点） |
| ⑧ | 错误文案逐字样本 | 逐字可得：`/api/search/buildArtifacts` 三条 400 + 404 文案、docker promote 全族文案（§4）；~~上传/promotion/retention 的逐字错误文案不可得~~ **L023-1 解锁**：REST 实现层源码 + 活体全族文案逐字落定（§11） | 部分覆盖 → 全覆盖（T-488 期源码不可达的登记作废） |
| ⑨ | delete 端点参数族 | `DELETE /api/build/{name}?buildNumbers=<csv>&artifacts=0|1&deleteAll=0|1` + `POST /api/build/delete`（body `{buildRepo, project, buildName, buildNumbers[], deleteArtifacts, deleteAll}`——**L023-1 补 `buildRepo` 字段**，6.13+，支持特殊字符）——§1 表 | ADR 骨架 `?builds=&dateRange=` 字面**有误**（实际 buildNumbers/artifacts/deleteAll，无 dateRange）——勘误回填项 |
| ⑩ | 档位核验结论 | 证据指向 Artifactory 侧行为 = **JCR/Pro 档门控**（官方描述三处 Pro/JCR + t226 旧会话 build 搜索族 400 Pro 门）；Builds 页 nav 在 OSS 在场（console-ui 高）——§6。**不翻转 ADR-0045 点 11 的 community 地板**（BinFlow 无许可门先例 = 超集实现不违 parity），但「OSS 发行集合含 build CRUD 全族」的推断依据需修正为「openapi.yaml 随 OSS 代码库发行 ≠ 端点在 OSS 档可用」——勘误注记 | 勘误注记（机制轴零翻动） |

---

## 1. REST 端点族（置信度：高——产品内嵌一手 OpenAPI + 官方参考页双源逐字，除单独标注者）

Base：`/artifactory/api`。认证：Basic / Bearer JWT 双收。产品媒体类型族（`BuildRestConstants`，补充官方规范）：`application/vnd.org.jfrog.build.Builds+json`（列表）、`Build+json`（单 build）、`BuildsByName+json`、`BuildsDiff+json`、`BuildPatternArtifactsRequest/Result+json`、`BuildArtifactsRequest+json`、`PromotionRequest/Result+json`（端点实际同时收发裸 `application/json`）。

| 方法 | 路径 | 参数 | 成功响应 | 错误响应 | 语义要点 | 置信度 |
|---|---|---|---|---|---|---|
| GET | `/build` | `?buildRepo=&project=`（**L023-1：openapi 的 `projectKey` 字面有误，实现收 `project`**，§11.1-E8） | 200 `{uri, builds:[{uri, lastStarted}]}`（顶级 uri = **绝对 URL + `?buildRepo=artifactory-build-info` 查询串**，条目 uri = `/<buildName>` 相对形态；**零 build 时 404 `No builds were found`**——L023-1） | 401/403/404（空态） | 全部 build 名清单；`lastStarted` = 该名最新一次 run 的 started（**UTC 归一回显**，§11.3） | 高 |
| PUT | `/build` | `?buildRepo=&project=` | **204 空体 + `X-Checksum-Sha256` 响应头**（build JSON manifest 的 sha256——L023-1；openapi/官方「200」字面有误，§11.1-E1） | 400 畸形 body / 401 / 403 / **400 hidden 坐标** | **全量上传**：body = build info JSON（name/number 在 body）；重复上传同坐标（name+number+started 同）= **覆盖**（先删后建，活体 204）；**同号不同 started = 新 run 并存**（L023-1）；覆盖需 delete 权；modules 须带正确 sha1/md5 才与制品关联 | 高 |
| GET | `/build/{buildName}` | `?buildRepo=&project=` | 200 `{uri, buildsNumbers:[{uri, started}]}`（uri = `/<number>`；**started 严格倒序——最新在前**，L023-1 落定原待验证 #4） | 401/403/404 `No build was found for build name: <name>` | 某 build 名下的全部 run 号清单；同名同号多 run = 同号多行并列 | 高 |
| DELETE | `/build/{buildName}` | `?buildNumbers=<csv>&artifacts=0\|1&deleteAll=0\|1` | 200 text/plain（部分删除：`The following builds have been deleted successfully: 'name#51'.\nWarning - the following builds could not be removed: '99'.\n`——**have**（官方例文 has 过时）+ Warning 段 + 尾随换行；deleteAll：`All builds '<name>' under '<repo>' have been deleted successfully`（无尾句点）） | 400（无名/无号非 deleteAll）/ 401 / 403 / 404 `Unable to find build '<name>'`（名不存在）`/ Unable to find the given build numbers`（号全不存在） | `deleteAll=1` 全删；`artifacts=1` 连制品删；**Requires Artifactory Pro**（官方） | 高 |
| GET | `/build/{buildName}/{buildNumber}` | `?started=&diff=&buildRepo=&project=&slim=`（**`slim` 为 L023-1 新发现参数**：true 时 modules 置 `[]`、**`properties` 键整体省略**（非置 null——L023-2D E16）——jf CLI 消费面） | 200 `{uri, buildInfo:{…}}`（uri = 绝对 URL + buildRepo 查询串） | 401/403/404 `No build was found for build name: <n>, build number: <m> `（**无 started 子句时句尾带一空格**；有子句 = `…build number: <m> , build started: <ts>`——**逗号前带空格**，L023-2D E18 精确字面） | 单 build 详情；`started`（`yyyy-MM-dd'T'HH:mm:ss.SSSZ`）用于同名同号多 run 消歧（**回显原样时区，不归一**——与列表端点相反，§11.3）；`diff` = 旧号对比（Builds Diff；**方向约束**：新号必须 ≥ 对比号否则 400，§11.5）；`diff=null` 字面量 → 400 `Parameter 'diff' must contain a value, if specified`；回显 buildInfo 含 `statuses`（promotion 历史）数组；`durationMillis` 缺省回显 0 | 高 |
| POST | `/build/append/{buildName}/{buildNumber}` | `?started=&buildRepo=&project=` | **204** 空体 | 400 / 401 / 403 / **404 `The build <name>:<number> is not found`**（L023-1 逐字；openapi 的 `Build-Info not found` 字面有误，§11.1-E4） | **段拼接**：body = BuildModule **数组**；**模块列表直接拼接，同 id 不合并**（重复 id 产生重复模块条目——L023-1 双证，勘误 §2.2/软缝④）；权限 = Deploy ∧ Delete（官方） | 高 |
| POST | `/build/promote/{buildName}/{buildNumber}` | `?buildRepo=&project=` | 200 `{messages:[{level, message}]}`，**level ∈ `INFO`/`WARNING`/`ERROR`（大写——openapi 小写枚举有误，L023-1）**；**failFast 400 两面**（L023-2D E14）：异常路径（E12 abort 等）= 400 **errors[] 信封**；流程完成路径（timestamp 非法等）= 400 + messages body | 400（blank 坐标/failFast 失败）/ 401 / 403 / 404 `Cannot find a build by the name: <n>, number: <n>, repo: <r>` / **404 `Cannot find target repository by the key '<key>'`** | promotion，body 见 §2.4/§11.5；**目标 = 同号多 run 中 started 最新者**（无 started 参数） | 高 |
| POST | `/build/delete` | —（body 承载） | 200 text/plain（同 DELETE 族文案） | 400/401/403 | 批删（6.13+）：body `{buildRepo, project, buildName, buildNumbers[], deleteArtifacts, deleteAll}`（**6 字段，含 buildRepo——L023-1**）；**支持 build 号含特殊字符**（官方明示——这是该端点独立存在的理由）；语义与 DELETE 族同源（§11.7） | 高 |
| POST | `/build/rename/{buildName}` | `?to=<new>`（必填）`&buildRepo=&project=` | 200 text/plain：`Build renaming of 'x' to 'y' was successfully started`（**无尾句点——openapi 例文有句点，L023-1**） | 400/401/403/404 | **Requires Artifactory Pro**；异步语义（文案「was successfully started」）；**immutable build 改名 → 403 `The build <n>:<n> is immutable and cannot be renamed`**（L023-1） | 高 |
| POST | `/build/retention/{buildName}` | `?async=`（**缺省 false=同步——源码 JAX-RS 原语直读；openapi/官方「缺省 true」失真**，§11.1-E9）`&buildRepo=&project=` | **204** 空体 | 400 `Max count retention needs to be a positive number`（count=0/缺 body；**Content-Type 标 `application/json` 但 body 是该句纯文本——产品错标为参照真值，L023-2D E17**）/ 401 / 403 / 404 | **设定保留并立即执行删除**（缺省同步删完再回 204；`async=true` 后台跑——官方「不立即删」表述过时，L023-1 勘误 §11.6）；body 见 §2.5/§11.6 | 高 |
| POST | `/build/patternArtifacts` | —（body：BuildPatternArtifactsRequestWithRepo 数组） | 200 `[{repository, uri…}]`（模式制品解析） | 400/401/403 | **L023-1 新登记端点**（openapi 未载）：CI 依赖解析面（jf CLI `--build` 消费）；M17 面外登记（归 aql.md §15.4 同族的依赖解析族） | 中（仅源码，待活体） |
| POST | `/archive/buildArtifacts` | body（见 §2.7） | 200 二进制档（zip→`application/zip`、tar→`application/x-tar`、tar.gz/tgz→`application/x-gzip`） | 400/401/403 | build 制品打包归档（2.6.5+）；**Requires Artifactory Pro**（官方）；M17 面外登记 | 高 |
| POST | `/docker/{repoKey}/v2/promote` | —（body 承载） | 200 text/plain `Promotion ended successfully` / 部分 206 | 400/401/403/404 | Docker 镜像晋升，语义见 §2.6；另有 legacy `POST /docker/{repoKey}/v1/promote` 与 `DELETE /docker/{repoKey}/v2/delete` 同族 | 高 |

**M17 面内/面外切分**（对齐 ADR-0045 点 6）：M17 七族 = 上传/append/查询单/列表族/批删/promote/retention；**面外登记** = rename、diff（查询参数）、docker promote 独立端点、patternArtifacts（L023-1 新增面外登记）、projectKey 过滤族（BinFlow 无 projects 域，参数面不承接）。

## 2. 语义流程

### 2.1 查询族（单·列表·最新）

- 列表两跳：`GET /build`（名清单+lastStarted）→ `GET /build/{name}`（号清单+started）→ `GET /build/{name}/{number}`（详情）。URI 回显：**条目 = 相对形态**（`/<buildName>`、`/<number>`），**顶级 = 绝对 URL + `?buildRepo=` 查询串**（官方例证 + L023-1 活体）。
- 「最新」语义：查询/归档 body 的 `buildNumber` 支持 **`LATEST` 哨兵值**（官方 archive 端点明示；归档/搜索族通用语义——软缝⑦补强：最新语义有 body 级哨兵形态，GET 族则仍无专用端点；GET 单详情的 path number **不支持** LATEST——L023-1 源码确认）；`GET /build/{name}` 回显 **started 严格倒序（最新在前）**（L023-1 落定）；「最新 = 取首」成立。builds 唯一性 = (name, number, started, repo) 四元（§3 builds 表 UNIQUE 索引）——**同名同号不同 started 是不同 run**，`started` 查询参数即消歧键；**同号多 run 在号清单中同号多行并列**（L023-1 活体）。
- `diff` 参数（GET 单详情）触发 Builds Diff 输出（对比形态官方未展开——低，待活体）。

### 2.2 PUT 上传与 append 段拼接

- **PUT 全量上传**：body = 完整 build info JSON（§3 字段集）。重复上传同坐标 = 覆盖（先删后建；活体 204 复证；覆盖需 delete 权；官方权限注记）。modules 的 artifacts 携 sha1/md5 方可与仓内制品关联（官方明示「correct SHA1 and MD5 to be properly linked」）；无 checksum 的 artifact 行只入记录不建关联。制品↔build 的关联标记 = 属性三键 `build.name` / `build.number` / `build.timestamp`（官方 OSS 源 `BuildConstants` 常量——「modules must have build.name and build.number properties」官方注记的机制本体；高）。
- **append 段拼接**（POST，数组 body）：
  1. 父 build (name, number[, started, buildRepo, project]) 必须已存在——不存在 404 `The build <name>:<number> is not found`（逐字——L023-1；openapi 描述字段 `Build-Info not found` 非响应实现文案）。
  2. 数组逐模块**列表拼接**：新模块 append 到既有 modules 尾部后整体重存；**同 id 模块不合并、产生重复条目**（源码列表 concat + 活体 GET 证——**证伪旧稿「按 id 合并」推断**，「聚合 build 引用子 build」是客户端约定不是服务端语义）。
  3. 引用形态（官方 CLI `jf rt ba` 语义）：module id = `<被引用 build 名>/<被引用 build 号>`——「聚合 build」通过引用模块收纳子 build，被引用 build 须**已发布**（CLI 前置校验）。
  4. 成功 204 无 body。
- 覆盖 vs 合并语义分界：PUT = 整 build 替换；append = 模块列表级追加拼接——两出口并存（官方设计）。

### 2.3 buildinfo 仓直传语义（PUT 进 build-info 仓的文件面；置信度：中——仅反编译，补充官方规范）

PUT 进 build-info 类型仓（含缺省 `artifactory-build-info`）的 `.json` 文件会被内部转发到 build 上传链路（`/api/build/buildUploadRedirect/<原路径>` 内部 forward）：

- 非 `.json` 扩展名的 PUT → **409** 拒绝（文案形如 `The '<repo>' repository rejected the deployment of '<path>'. Only valid Build Info .json files are supported.`）。
- JSON 可解析但路径与 build 坐标推导路径不符 → **静默忽略**（不入 build 索引，文件照存）。
- 重复 PUT 同一 build JSON（覆盖文件）→ 先删 DB 既有记录再重建（override 语义）；系统属性可强制按 DB 存在性 override。
- 删除 buildinfo 仓里的 build JSON 文件 → 级联删除对应 build DB 记录（解析失败时按路径坐标回退删除）。
- buildinfo 仓内 **copy/move 全拒**（400 `Copy and Move operations are not allowed within the Build Info repository`）；从 buildinfo 仓 copy/move 出去也拒；copy/move 进 buildinfo 仓仅当目标路径 == build 坐标推导路径（否则 400 bad-path 文案）。
- buildinfo 仓族：缺省 `artifactory-build-info`；项目域 buildinfo 仓 = **`<projectKey>-build-info`** 后缀约定（官方 OSS 源 `BuildConstants.BUILD_INFO_REPO_KEY_SUFFIX`；高）；包型字面 `buildinfo`。
- BinFlow 对位注记：ADR-0045 载体轴 A（build 记录不进 storage/nodes）——本节为 Artifactory 形态记录，BinFlow 不承接文件面直传（REST 面 `PUT /api/build` 为唯一入口）。

### 2.4 Promotion 状态机

- 请求 body（高，双源）：`status`（新状态，可省 = 状态-only 促销可省 targetRepo）、`comment`、`ciUser`、`timestamp`（ISO8601 `yyyy-MM-dd'T'HH:mm:ss.SSSZ`）、`dryRun`（缺省 false，全链校验零落变更）、`sourceRepo`（缺省自动解析）、`targetRepo`（5.7+ 可为 virtual）、`copy`（缺省 false = **move**）、`artifacts`（缺省 true）、`dependencies`（缺省 false）、`scopes[]`（dependencies=true 时生效，additive OR）、`properties`（挂在被促销制品上，与 targetRepo 无关）、`failFast`（缺省 true）。官方 schema 将 ciUser/timestamp/copy/artifacts/dependencies/failFast 标 required（schema 生成器痕迹，实际行为按缺省值容忍——**L023-1 活体：仅 status 的 promote 200 通过，required 强制度证伪为宽容**）。
- **状态机**：
  - status = **自由字符串**，产品不约束值集（无枚举、无迁移表）——UI 惯例值（如 staged/rolled-up/released）是约定不是协议。
  - 每次 promote **追加**一条 promotion 记录（六元组：status/timestamp/comment/repository/ciUser/user）——append-only 历史，不可改。**实现机制 = 全量删除重建 build JSON**（L023-1 源码；evidence 启用时 immutable build 走 DB 直插旁路）。
  - 现势状态 = **按 timestamp 取最新一条**（产品模型明示 max-by-timestamp）。
  - `started` 字段不随 promotion/replication 变化（官方明示 immutable）。
- 成功响应 `{messages:[{level: INFO|WARNING|ERROR（大写）, message}]}`——**400 有两个面（L023-2D E14）**：异常中止路径（缺制品 failFast abort）= 400 `errors[]` 信封；流程完成路径（timestamp 非法、failFast 警告集齐）= 400 + messages body；failFast=false 时 warning/error 行与 200 并存（活体）。
- 权限（官方）：promote 需 build 的 Deploy 权；BinFlow 门 = ADR-0045 点 5（w(targetRepo) ∧ r(buildRepo)）。
- 缺制品语义（ADR-0045 待答项→已实证）：**failFast=true 且有 artifact 无法解析 → 400 `Unable to find artifacts of build '<name>' #<number> from <buildRepo> repo: aborting promotion.`**；failFast=false → warning `Unable to find the following artifacts of build '<name>' #<number>: <names>` 后继续（L023-1 活体+源码——原「C 层自有细则」升级为参照真值）。

### 2.5 Retention 形态

- body：`deleteBuildArtifacts`（bool，删 build 时连制品删）、`count`（int，**必须为正数；=0 或 body 缺失 → 400 `Max count retention needs to be a positive number`**——L023-1；缺省 -1 = 不启用 count 维度）、`minimumBuildDate`（**ISO8601 时间戳**，早于此的 build 可删——非天数）、`buildNumbersNotToBeDiscarded[]`（豁免号清单）。
- `async` query（**缺省 false = 同步执行**〔源码 JAX-RS 原语直读；活体「204 返回后删除已可见」与同步/异步两解兼容，同步为源码直读口径——E9〕；`async=true` = 后台异步，立即 204）+ 系统属性 `build.retention.always.async`（缺省 false，置 true 则 sync 请求也转异步）。相关系统属性族（反编译，补充官方规范）：`build.retention.enabled`（true）/`build.retention.workers`（10）/`build.forced.delete.artifacts`（true）/`build.block.duplicate.entries`（false）/`build.search.maxBatchSize`（5000）/`build.info.manifest.file.size.limit.bytes`（700MB）/`build.ui.skip.delete.permission.check`（false）。
- **删除时机（L023-1 勘误）**：官方「setting build retention does not immediately delete any builds」表述**过时**——7.161 实际行为 = POST /retention **立即调度删除**（async 后台执行，活体 4s 内生效）；build info JSON 携带 buildRetention 块上传时也在**当次发布尾部同步执行**（源码——发布即触发）。删序/豁免细则见 §11.6。
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

### 2.7 Build Artifacts 归档（`POST /api/archive/buildArtifacts`；M17 面外）

body = `BuildArtifactsRequest` 全字段（官方 schema，高）：`buildName`*、`buildNumber`*（支持 `LATEST` 哨兵）、`archiveType`*（tar/zip/tar.gz/tgz）、`buildStatus`（可选，按最新状态过滤）、`repos[]`（限定仓）、`mappings[]`（`input` 正则 + `output` 支持正则组 token 的路径重映射）。同一 schema 亦服务 `/api/search/buildArtifacts`（去 archiveType）——aql.md §15.4 的 body 字段集由此补全。BinFlow M17 面外（归档装配族远期行）。

## 3. 数据模型字段集（表族）

### 3.1 wire JSON 字段集（build info；置信度：高——一手 OpenAPI schema + 官方参考页）

顶层：`version` / `name` / `number` / `type`（MAVEN|GRADLE|ANT|IVY|GENERIC）/ `buildAgent{name,version}` / `agent{name,version}` / `started`（`yyyy-MM-dd'T'HH:mm:ss.SSSZ`）/ `artifactoryPluginVersion` / `durationMillis`（**缺省回显 0——L023-1 活体**）/ `artifactoryPrincipal`（**服务端以当前认证用户覆写——L023-1 活体**）/ `url` / `vcs[]{revision,message,branch,url}` / `licenseControl{runChecks,includePublishedArtifacts,autoDiscover,scopesList,licenseViolationsRecipientsList}` / `buildRetention{deleteBuildArtifacts,count,minimumBuildDate,buildNumbersNotToBeDiscarded[]}` / `modules[]` / `issues{tracker{name,version},aggregateBuildIssues,aggregationBuildStatus,affectedIssues[]{key,url,summary,aggregated}}` / `properties`（环境变量/属性 map）。实际回显另含 `statuses[]`（promotion 历史——**wire 键：status/comment/timestamp/timestampDate/user/repository/ciUser，nullable 字段整体省略，含 schema 未载的 `timestampDate`（epoch-ms）——L023-1 活体**）。

module：`properties` / `id` / `type` / `artifacts[]` / `dependencies[]`。

artifact：`type` / `sha1` / `sha256` / `md5` / `name` / `path` / `originalDeploymentRepo`（**`path`+`originalDeploymentRepo` = promote manifest 收集通道的消费键——L023-2D E15；echo 保真要求空串字段也回显**）。

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

要点：① build↔bundle 有专门关联表（§联动）；② module/dependency 名入 `*_name_id` 列（名字规范化存储）；③ AQL builds 域字段 = 本表族的直投影（aql.md §15 字段集与此一一对应）；④ buildinfo 仓里的 build JSON 文件布局 = **`<buildName>/<buildNumber>-<startedMillis>.json`**（官方 OSS 源 `BuildInfoUtils` 逐字：路径正则 `^(.*)\/(.*)-([\d]*)\.json$`，started 由 ISO8601 转 epoch-ms；buildName/buildNumber 各自做**路径元素级编码**——斜杠恒编码，保障单段路径元素；置信度：高——官方源码补齐本规格原「低」项）。

## 4. 老搜索两入口 wire 锚（详表归 aql.md §15.4）

- `POST /api/search/buildArtifacts`（RolesAllowed user/admin；body `{buildName*, buildNumber XOR buildStatus, …}`）：缺名 400 `Cannot search without build name.`；号/状态双缺 400 `Cannot search without build number or build status.`；双给 400 `Cannot search with both build number and build status parameters, please omit build number if your are looking for latest build by status or omit build status to search for specific build version.`（**逐字含「your」拼写**——产品原文）；命中 200 `{"results":[{"downloadUri":…}]}`（**downloadUri 键**，非 uri）；空 404 `Could not find any build artifacts for build '<name>' [number '<n>'|status '<s>']`。（文案：反编译逐字，中；端点面+400 Pro 门：官方 + t226 活体，高）
- `GET /api/search/dependency?sha1=&sha256=&buildRepo=&project=`（user/admin）：按 checksum 反查「哪些 build 依赖了它」；命中 200 `{"results":[{"uri":…}]}`（uri 键，指向 build API 形态）；非法参数 400 BadRequestException。（同上双置信度分层）

## 5. 与公开规范的差异/补充（「此条补充官方规范」）

- 官方参考页未载、仅反编译/产品 schema 可见：buildinfo 仓直传/copy-move 拒绝族（§2.3）、build 系系统属性族（§2.5）、docker promote 错误文案与 206 部分成功（§2.6）、buildArtifacts/dependency 搜索逐字文案（§4）、表族 DDL（§3.2）、buildinfo 仓在 ANY LOCAL/REMOTE/DISTRIBUTION/Anything 预置目标回退链中被**排除**（§7）、`slim` 查询参数与 `timestampDate` 回显字段（§11）。
- 官方与产品的分歧登记：官方参考页将 promotion body 六字段标 required（§2.4——**L023-1 活体证伪为宽容**）；append 的 body 历史上曾为单对象形态，现行官方 + 一手 OpenAPI 均为数组（以数组为准）；**官方 DELETE 例文「has been deleted successfully」与 7.161 实现「have been deleted successfully」不一致（以实现为准）**；**官方 retention「不立即删除」与 7.161 实现「POST 即删（async）」不一致（以实现为准）**；**官方/一手 openapi 的 GET /build 200-only 与实现「空态 404」不一致（以实现为准）**——L023-1 三条全活体复核。
- **一手 openapi.yaml 自身与实现的分歧（L023-1 全表见 §11.1-E）**：PUT 200→实为 204+checksum 头、level 小写→实为大写、`projectKey`→实收 `project`、append 404 描述文案≠实现文案、rename 例文句点、批删 body 缺 buildRepo 字段、retention async 缺省值源码原语 false 但活体表现异步（以活体为准，见 §11.1-E9）。

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
- **403 文案族（L023-1 源码补齐，补充官方规范）**：read 拒 `The user: '<u>' is not authorized to access build info. Read permission is needed.`；basic-read 拒 `…access build info. 'Global Basic Read' Flag turned on or read permission is needed.`；delete 拒 `…not authorized to delete build info. Delete permission is needed.`；upload 拒 `…not authorized to upload build info. Upload permission is needed.`（内部上传路径另有 `User is not authorized to access build info` 短文案）；manage 拒 `…not authorized to manage build info. Manage permission is needed.`；权限可视拒 `…not authorized to view build permissions.`。无 read 权的 build 在列表查询中被**静默过滤**（非报错）。
- 对拍注记：两出口表达力等价面 = build 名模式授权；Artifactory 用独立段承载、BinFlow 用 path 位承载——语义覆盖不损失（ADR-0045 已论证）；本节不构成翻案输入。

## 8. webhook·AQL 联动清单

- **webhook 三事件投影**（webhook.md §3.4 已冻结，envelope/data 字段零发明）：`uploaded` / `deleted` / `promoted`；data = `build_name, build_number, build_started`（`1970-01-01T00:00:00.000+0000` 形态）, `build_repo`（示例 `artifactory-build-info`）。发射点对位（ADR-0045 点 7）：上传/append 成功→uploaded；删除/retention 成功→deleted；promote 成功→promoted。
- **AQL 三入口字段集**（aql.md §15 冻结）：`builds` / `modules` / `dependencies` 字段集与 §3.2 表族一一对应；`artifacts(build)` / `build.promotions` / `build.properties` / `module.properties` 入口 M17 面外维持 400（翻转点 = T-511 票 + M18+ 翻转注记，aql.md §15.3）。
- **两老搜索端点**：§4（wire 锚随 aql.md §15.4 双挂）。

## 9. 待验证清单（低置信/未实证——零静默升格）

1. Builds 页在 OSS 档的**页面内容行为**（空态/升级提示/加载即 403）——nav 在场已证，内容未实证（本轮活体双损坏）。
2. ~~promotion body 六「required」字段的运行时强制度~~ **已解**（L023-1 活体：仅 status 的 promote 200 通过——required 标注证伪为宽容，§2.4）。
3. ~~buildinfo 仓 build JSON 文件路径布局字面~~ **已解**（成稿后官方 OSS 源可达：`<name>/<number>-<millis>.json`，§3.2 ④——低→高，零静默升格的反向闭环）。
4. ~~`GET /build/{name}` 回显 buildsNumbers 的排序保证~~ **已解**（L023-1 双源：started 严格倒序、最新在前；同号多 run 同号多行并列——§2.1）。
5. builds diff（`?diff=` 参数）的响应形态（官方未展开；L023-1 已补方向约束 400 与 `diff=null` 400——§1 表；**diff 响应 body 形态仍待活体**）。
6. ~~上传/promotion/retention 的逐字错误文案~~ **大半已解**（L023-1：REST 实现层 7.161.16 源码 + 活体逐字落定 §11；残余 = promote timestamp 非法路径的 body 形态、`build.patternArtifacts` 端点响应）。
7. ~~`statuses[]` 在 GET 单详情回显中的完整字段形态~~ **已解**（L023-1 活体：`{status, comment, timestamp, timestampDate, user[, repository][, ciUser]}`，nullable 省略——§3.1）。
8. **`_START_`/`_EXT_` 环境变量透传格式——定案：四源零命中，按「不存在此语义」处理**（L023-1 登记 + 同日回执复核）：反编译 7.161.24 全树 / OSS 7.161.16 源码树含 build-handler/ / jfrog build-info 官方库 README / **JFrog 现役官方文档 Build-Info Integration 页（docs.jfrog.com/artifactory/docs/build-integration——旧 URL jfrog.com/help/...the-build-info-json 已 301→404，内容并入此页）均无 `_START_`/`_EXT_` 字样**。官方口径的邻接真值（升格记录）：环境变量入 build info `properties` = **纯客户端采集语义**（CLI `jf rt bp --collect-env` + `--env-include`（缺省 `*`）+ `--env-exclude`（缺省 `*password*;*psw*;*secret*;*key*;*token*;*auth*`，大小写不敏感分号分隔模式；`jf rt bce` 独立采集命令已废弃保留兼容）；`jf rt ba`（append）同携 env-include/exclude；唯一变量替换约定 = 文件 spec 的 `${key}`（客户端展开）；**服务端对 properties 零展开零过滤，只存储**（官方页 server-side 行为清单：publish/promote/discard）。置信度：高（官方文档锚 + 双源码零命中反证）。
9. `POST /build/patternArtifacts` 的响应 body 形态（L023-1 新登记端点，仅源码面——中置信，待活体）。
10. retention `async` 缺省口径已按源码定案为**同步**（E9）；残余 = 7.161.15 与 7.161.16 之间该默认值是否存在版本漂移（低风险时序差异，契约可忽略——功能面零差异）。
11. ~~名清单（GET /build）序向~~ **已解**（L023-2D 双活体收口：**升序**（各名最新 run 日期最早在前）——§11.8 中→高）。

## 10. 取证锚点（2026-09-06 会话）

- 一手 OpenAPI：reverse-src `src/batch1-core/rest/resource/build/openapi.yaml`（996 行，产品内嵌自维护规格 + agents.md 维护说明）——**注意此文件位于只读 reverse-src，行为摘录进本规格、文件不引用进实现**。
- 官方参考页（docs.jfrog.com/integrations/reference/，2026-09-06 实取）：`appendbuild.md` / `promotebuild.md` / `controlbuildretention.md` / `uploadbuild.md` / `getallbuilds.md` / `getbuildruns.md` / `getbuildinfo.md` / `deletebuilds.md` / `deletebuildsmultiple.md` / `renamebuild.md`。
- 官方指南页：`docs/artifactory/docs/build-integration.md`（CLI append 语义/引用模块形态）、`docs/aql-entities-fields-reference.md`（字段表）、`docs/aql-examples.md`（跨域路径例）。
- 反编译：`org/artifactory/build/*`（ReleaseStatus/PromotionConfig/BuildId/DetailedBuildRunImpl）、`rest/resource/build/BuildRestConstants.java`、`repo/interceptor/BuildInfoInterceptor.java`、`rest/resource/search/types/{BuildArtifactsSearchResource,DependencySearchResource}.java`、`batch2-protocol addon/docker/rest/DockerResourceBase.java + rest/v2/promotion/DockerV2Promoter.java`、`security/{PermissionTarget,permissions/PermissionTargetModel,AuthorizationServiceBase,LegacyAuthorizationServiceImpl}.java`、`common/ConstantValues.java`（build.* 键族）、`postgresql/postgresql.sql`（builds 表族 DDL）。
- 既有活体证据引用：t226 OSS 7.84.10（T-407 会话 2026-09-01）经 aql.md §2.1/§8.2 与 console-ui.md §1.1/§3.8 转引。
- 官方 OSS 源码（用户提供的 jfrog-artifactory 7.161.16 发行源码树，按绝对路径只读——非 reverse-src）：`build-handler/build-handler-acl/src/main/java/com/jfrog/build/acl/util/{BuildInfoUtils,BuildConstants}.java`（JSON 布局/属性三键/`-build-info` 后缀/`release-bundles-v2` 缺省常量）+ `encode/BuildRepoPathEncoder.java`（路径元素编码）；`web/rest/src/main/resources/rest/resource/build/openapi.yaml` 与反编译副本 **diff 逐字节一致**（一手规格完整性核验）。~~build REST 实现类（BuildResource.java）在 OSS 树内亦未检索到~~ **L023-1 勘误**：REST 实现即 OSS 树内 `build-handler/build-handler-service` 模块（`resource/BuildPublicResource.java` = `@Path("build")` 公共面；T-488 只检索了 acl 子模块故误判——§11.2）。
- 本轮活体：**零**（pro 7.161 与 t226 双损坏，修复尝试与终态见 reports/agents/T-488.md §0——容器均复原为 stopped 原状）。

---

## 11. L023-1 反编译 + 活体核对（2026-09-15；D07 11 行规格核对票）

> **取证基础**：① 服务端实现源码首次可达——OSS 7.161.16 树 `build-handler/build-handler-service/src/main/java/com/jfrog/build/`（REST 资源层 + command + service + DAO 全套，T-488 期「实现疑在未开源模块」误判勘误）；② 活体基线 = http://172.16.58.130:8082（pro 7.161.15，addons 全开）——本域首次活体取证（T-488 期双损坏）。**置信度规约：源码（7.161.16）+ 活体（7.161.15）双源 = 高；仅源码 = 中；推断 = 低。** 版本差一档（7.161.16 源 vs 7.161.15 活体）在本域全部抽查点上零分歧。

### 11.1 勘误总表（与 §1–§8 旧稿及一手 openapi/官方文档的分歧——以本表为准）

| # | 旧稿/官方口径 | 7.161 实际行为（双源） | 影响 |
|---|---|---|---|
| E1 | PUT `/api/build` 成功 = 200（openapi/官方/旧稿 §1） | **204 No Content + `X-Checksum-Sha256` 响应头**（= build JSON manifest sha256） | §1 表已改；契约测试断言面 |
| E2 | `GET /api/build` 空态 = 200 空列表（openapi 仅 200/401/403） | **404 `No builds were found`**（JSON errors 信封） | §1 表已改 |
| E3 | promote messages level ∈ error/warning/info 小写（openapi） | **大写 `INFO`/`WARNING`/`ERROR`**（内部状态枚举 name 原样上 wire） | §1/§2.4 已改 |
| E4 | append 404 文案 `Build-Info not found`（openapi 描述） | **`The build <name>:<number> is not found`** | §1/§2.2 已改 |
| E5 | append 模块按 id 合并（旧稿 §2.2/软缝④） | **列表直接拼接，同 id 产生重复模块条目**（源码 concat + 活体 GET 双证） | **软缝④证伪**——ADR-0045 复核输入 |
| E6 | DELETE 成功文案 `The following builds has been deleted successfully…`（官方例文） | **`The following builds have been deleted successfully: 'n#1', 'n#2'.\n`**（have；尾随 `.\n`；部分失败再接 `Warning - the following builds could not be removed: '99'.\n`）；deleteAll 另文案 `All builds '<name>' under '<repo>' have been deleted successfully`（无尾点） | §1 已改 |
| E7 | retention「设定不立即删」（官方明文/旧稿 §2.5） | **POST 即触发删除**（async 后台；活体 4s 内生效）；build JSON 带 buildRetention 块上传时也在当次发布尾部同步执行 | **软缝⑥修正**——ADR-0045 复核输入 |
| E8 | GET 列表族 query 参数名 `projectKey`（openapi） | **实现收 `project`**（全端点统一；`buildRepo` 亦全端点可选） | §1 已改 |
| E9 | retention `async` 缺省 true（openapi/官方） | **缺省 false = 同步**（源码 JAX-RS 原语直读：无 @DefaultValue 的 primitive boolean 缺参即 false → 走同步分支，删完才回 204）；活体「204 后删除立即可见」兼容两解、与同步口径一致。**对外可观察差异面 = 删除完成与响应返回的时序**（功能面零差异） | §2.5/§1 已按「缺省同步」定案；待验证 #10 降级为低风险时序差异 |
| E10 | 批删 body 5 字段 `{project, buildName, buildNumbers[], deleteArtifacts, deleteAll}`（旧稿 §1） | **6 字段——另有 `buildRepo`**；与 DELETE 族共用同一删除 command（文案/错误分支全同） | §1 已改 |
| E11 | rename 成功文案带尾句点（openapi 例文） | **无尾句点**：`Build renaming of 'x' to 'y' was successfully started` | §1 已改 |
| E12 | promote 缺制品语义「官方未文档化，BinFlow C 层自定」（旧稿 §2.4） | **实证**：failFast=true → 400 `Unable to find artifacts of build '<name>' #<number> from <buildRepo> repo: aborting promotion.`；否则 warning 后继续 | ADR-0045 点 5 参照真值升级 |
| E13 | T-488「build REST 实现类不在反编译/OSS 集合内」（§10 末条） | **实现 = OSS 树 `build-handler/build-handler-service`（`BuildPublicResource` @Path("build")）** | §10 已勘误 |
| E14 | L023-1 §11.5-9「failFast 400 body 同形」 | **promote 400 两面**：异常中止路径（E12 abort）= 400 **errors[] 信封**（REST 异常映射器产物）；流程完成路径（timestamp 非法/failFast 警告集齐）= 400 + messages body | §1/§2.4/§11.5-9 已改（L023-2D 活体双端） |
| E15 | L023-1 §11.5-5 收集机制隐含「checksum 搜索」 | **双通道**：manifest（`originalDeploymentRepo`+`path` 全集直查）→ 否则 AQL build 属性三键反查；checksum 仅 verify；`originalDeploymentRepo`+`path` = wire 回显保真键（空串也回） | §11.5-5/§3.1 已改（源码 BuildArtifactService） |
| E16 | L023-1 §1 slim 行「properties 置 null」 | **`properties` 键整体省略**（模块 `[]` 保留） | §1 已改 |
| E17 | L023-1 §11.6-1「count 门 400 text/plain」 | **Content-Type 实标 `application/json`**（body 仍为纯文本句——产品错标为参照真值） | §1/§11.6-1 已改 |
| E18 | L023-1 §1 详情 404 文案无空格口径 | 精确字面：无 started 子句**句尾一空格**；有子句**逗号前一空格** | §1 已改 |
| E19 | 名清单序向未判（L023-1 §11.8 中置信） | **升序**（按各名最新 run 日期，最早活跃在前——双活体一致） | §11.8 升高、待验证收口 |
| E20 | L023-1 §11.3 buildRepo 门文案「三因」未定读/写路径 | **写路径 = projects 门**（project 须存在；三因共用 `does not exist` 文案）；**读路径无门**（不存在仓读 = 404 No builds were found） | §11.3 已改；BinFlow 承接面属裁定（D9） |

### 11.2 端点面核对（D07 11 行 × 覆盖度）

| matrix 行 | 能力 | L023-1 前覆盖度 | L023-1 后 | 关键增量 |
|---|---|---|---|---|
| D07-R01 | GET /api/build | 已规格（表级） | **深规格** | 空态 404、顶级 uri 绝对形态+buildRepo 串、名序 = 各名最新 run 日期序、lastStarted UTC 归一 |
| D07-R02 | PUT /api/build | 已规格（表级） | **深规格** | 204+sha256 头、坐标 hidden 400、覆盖=先删后建、同号多 run 并存、principal 覆写、部分 checksum 服务端补全、issue 聚合、buildRepo 后缀校验 |
| D07-R03 | GET /{name} | 已规格（表级） | **深规格** | started 倒序落定（高）、空 404、同号多行 |
| D07-R04 | DELETE /{name} | 已规格（表级） | **深规格** | 文案 have/Warning 段逐字、404 两分支文案、deleteAll 独立文案、多 run 同号每次至多删一个、warning→500/error→cancel |
| D07-R05 | GET /{name}/{number} | 已规格（表级） | **深规格** | statuses wire 形态（含 timestampDate）、slim 参数、diff 方向 400 与 diff=null 400、started 原样回显、durationMillis 回显 0 |
| D07-R06 | POST append | 部分（合并键推断） | **深规格+勘误** | **不按 id 合并**（E5）、404 文案（E4）、append 权限=delete∧upload 断言、204 |
| D07-R07 | POST promote | 已规格（表级） | **深规格** | failFast→400、level 大写、虚拟仓解析为 default deployment repo、targetRepo 404 文案、timestamp 非法→跳过状态更新 error、配额 413、状态记录=删除重建、user vs ciUser 语义 |
| D07-R08 | POST /delete | 部分（body 字段集） | **深规格** | body 6 字段（含 buildRepo）、与 DELETE 族同 command 全同语义、特殊字符号支持定位 |
| D07-R09 | POST rename | 已规格（面外登记） | 微补强 | 文案无尾点、immutable 403、async 语义维持 |
| D07-R10 | POST retention | 已规格（参数面） | **深规格+勘误** | count 正数门 400、即删性（E7）、删序=日期维度先行+count 维度后行、promoted/pinned 双豁免、无权限=warn 跳过非报错、retention 删除幂等容忍 |
| D07-R11 | docker promote | 已规格（§2.6，面外） | 维持 | 本轮未再核（独立域已在案） |

### 11.3 上传/查询族深规格（D07-R01/R02/R03/R05）

**号分配与 recycle（票面点名面）**：当客户端 PUT /api/build——服务端**零号分配**：number 完全由客户端 body 提供（字符串型，数字/非数字皆收）；服务端不递增、不复用、不校验单调性。run 身份 = (name, number, started-millis, repo) 四元——**同号不同 started = 新 run 并存**（活体：号清单同号两行）；**同坐标重复 PUT = 覆盖**（先删后建，204，需 delete 权）；删除某号后重传同号 = 新记录（无 recycle 概念）。数字/非数字号仅影响排序比较器选择（§11.8）。置信度：高（源码存在性检查 + 活体三探针）。

**服务端改写行为**（当客户端 PUT build，服务端在存储前做）：① `artifactoryPrincipal` ← 当前认证用户（活体：请求未带，回显 `admin`）；② 部分缺失 checksum 补全——artifact/dependency 三 checksum（sha1/sha256/md5）**有一个或两个**时，按已有 checksum 反查二进制库补齐其余（**全有或全无的行跳过**）；③ `issues.aggregateBuildIssues=true` 时——取该名最新 run（LATEST 语义），若其 promotion 历史无 `aggregationBuildStatus` 状态，则其 affectedIssues 以 `aggregated:true` 并入新 build 的 issues；④ 坐标校验——name/number 去前导空白后以 `.` 开头 → 400 `Build name must not start with '.'` / `Build number must not start with '.'`（活体证实 name 分支）。置信度：高（①④活体；②③源码）。

**buildRepo 解析**：显式 `buildRepo` > `project` 推导（`<key>-build-info`）> 缺省 `artifactory-build-info`；当客户端显式指定 buildRepo 时——**写路径（PUT/append）存在 projects 门**（L023-2D E20）：`<projectKey>-build-info` 推导自仓名，project 须真实存在；「仓不存在 / project 不存在 / 名缺 `-build-info` 后缀」**三因共用同一文案** 400 `Build info repository '<repo>' does not exist`（误导性文案 = 参照真值）；**读路径（GET 族/删除/retention）无此门**——不存在的 buildRepo 读 = 正常 404 `No builds were found`（双活体）。REST PUT 走内部文件上传通道（build JSON 落 `<name>/<number>-<startedMillis>.json`），响应 checksum 头 = 该 JSON 的 sha256。置信度：高（源码 + L023-2D 双活体）。BinFlow 对位注记：无 projects 域——该门的承接面（超集放行 vs 仿真门）属产品语义裁定，登记不立票（差分报告 D9）。

**查询族响应细节**（活体）：列表/号单顶级 `uri` = `http://<host>/artifactory/api/build[...][?buildRepo=artifactory-build-info]`；`lastStarted`/`started` 在**列表端点回显 UTC 归一**（`+0200` 入 → `+0000` 出），**详情端点回显原样时区**（`+0200` 入 → `+0200` 出）——两端点不一致是实现真值（详情直读 JSON 文件）。名清单排序 = 按各名最新 run 日期（SQL GROUP BY max(build_date)）；号单排序 = started 严格倒序。无 read 权的 build 静默过滤。置信度：高。

### 11.4 append 深规格（D07-R06）

当客户端 POST /api/build/append/{name}/{number}（body = module 数组）——
1. 父 run 解析：(name, number[, started, buildRepo, project])；缺 started 时 = 该号**最新 run**。不存在 → 404 `The build <name>:<number> is not found`。
2. 权限：先断言 build 的 **delete 权**，再断言 **upload 权**（任一失败 → 对应 §7 403 文案；官方文档「Requires Deploy and Delete permissions」与此互证）。
3. 合并：新模块数组**拼接**到既有 modules 尾部 → 整体重存（内部走与 PUT 同一覆盖通道）。**同 id 模块不合并、不覆盖、不去重**——同 id 重复发布 = 重复模块条目并存（活体：mod-a×2 各持自己的 artifacts）。模块内 artifacts/dependencies 也无去重。
4. 成功 204 无 body；build 的 `started`/`modified` 元数据不变（重存不改 started）。
置信度：高（源码 + 活体）。「聚合 build 引用子 build」= 客户端把子 build 坐标编码进 module id 的约定，服务端不识别不解析（§2.2-3 维持）。

### 11.5 promote 深规格（D07-R07）

当客户端 POST /api/build/promote/{name}/{number}（body = Promotion）——执行序：
1. 前置：blank name → 400 `Build name cannot be blank`；blank number → 400 `Build number cannot be blank`；cold 实例拒；build 上传权断言（403 §7 文案）。
2. **目标 run = 同号全部 run 中 started 最新者**（无 started 查询参数——多 run 同号时不可指定旧 run）。找不到 → 404 `Cannot find a build by the name: <name>, number: <number>, repo: <repo>`（活体）。
3. **虚拟仓解析**：targetRepo 为 virtual 时替换为其 default deployment local 仓再继续。
4. targetRepo blank → 不搬迁，记 INFO `Skipping build item relocation: no target repository selected.`（活体逐字）；非 blank → 仓存在性+类型校验：不存在或**非 local/federated/cache** → 404 `Cannot find target repository by the key '<key>'`（活体）。
5. 制品收集（**双通道，L023-2D E15**）：**通道一 manifest**——module artifact 携 `originalDeploymentRepo`+`path` 全集时直查该仓该路径文件（jf/CI 常规发布链即如此，无需属性关联）；**通道二 AQL build 属性**——manifest 键缺失时按 `build.name`/`build.number` 属性三键反查仓内制品集合；checksum **只用于 verify 不用于收集**。`sourceRepo` 给定时仅收该仓；两通道皆解析不到制品 → warning `Unable to find the following artifacts of build '<name>' #<number>: <names>`；**failFast=true 时此况直接 400（errors[] 信封，步 9-E14）** `Unable to find artifacts of build '<name>' #<number> from <buildRepo> repo: aborting promotion.`（活体逐字）。`dependencies=true` 时收依赖，`scopes[]` 给定时按交集过滤；依赖缺失同 warning 逻辑。
6. 搬迁：`copy=true` 走复制，false（缺省）走**移动**；搬迁失败 → error `Error occurred while copying/moving: <cause>`。
7. properties 标注：body `properties` 非空时对（目标仓或源仓的）收集项逐个加属性；无 annotate 权 → warning `User doesn't have permissions to annotate '<path>'` + failFast 时中止标注。
8. **状态更新**（promotion 记录落库）：
   - failFast=true 且至此有 error/warning → **跳过状态更新**，记 `Skipping promotion status update: item promotion was completed with errors and warnings.`
   - status blank → 跳过，记 `Skipping promotion status update: no status received.`
   - timestamp 给定且非法（ISO8601 解析失败）→ **跳过状态更新**，记 error `Skipping promotion status update: invalid\unparsable timestamp <ts>.`（注意字面含反斜杠）；timestamp 缺省 → 服务端当前时间。
   - dryRun=true → 全链零落库（校验到步 8 为止）。
   - 落库路径：默认 = **删除旧 build JSON + 携新 statuses 重建**（status 字段自由串、user=当前认证用户、repository=解析后 targetRepo（无则省略）、comment、ciUser 原样、timestamp；活体 wire：`{status, comment, timestamp, timestampDate, user}`，repository/ciUser 缺省时整体省略）；storage 配额不足 → 413 `Promotion status update for build <name>:<number>:<started> failed due to storage quota exceeded; build info size: <n>`。
9. 响应：200 `{messages:[{level: 大写, message}]}`（成功且零消息 = `{"messages":[]}` 空数组——**无汇总行**，L023-2D）。**400 的两个面（E14，L023-2D 勘误——L023-1「body 同形」系误读）**：步 5 的 failFast abort 是**异常抛出路径** → REST 异常映射器 → 400 `errors[]` 信封 `{errors:[{status:400, message:"Unable to find artifacts…aborting promotion."}]}`；**流程完成路径**（步 8 timestamp 非法、failFast 且 messages 集齐 ERROR/WARNING）= 400 + **messages body**（`{"messages":[{level:"ERROR",…}]}`）。promotion 记录 append-only（多次 promote 累积，现势=最新 timestamp）。
置信度：高（源码 + 活体抽验 2/4/5/8 wire）。

### 11.6 retention 深规格（D07-R10）

当客户端 POST /api/build/retention/{name}（body = BuildRetention）——
1. 入参门：body 缺失或 `count == 0` → **400 `Max count retention needs to be a positive number`**（文案纯文本，但 **Content-Type 标 `application/json`**——L023-2D E17 勘误：L023-1 的「text/plain」为想定非实测）。count 缺省 -1 = 不启用 count 维度；正数 = 启用。`minimumBuildDate` 缺省 null = 不启用日期维度。
2. `async` 缺省 **false = 同步**（删完再回 204；出错 → 400 类 `Errors have occurred while maintaining build retention. Please review the logs for further information.`，warning 同构文案）；`async=true` → 立即 204，删除后台执行；系统属性 `build.retention.always.async=true` 强制全异步。
3. `build.retention.enabled=false` → 零删除，记 warning（不报错）。
4. **删序（两遍执行，日期维度先行）**：
   - **第一遍（日期）**：`minimumBuildDate` 给定时——遍历该名全部 run，`started` 的本地日期**严格早于** minimumBuildDate 的 run 删除；
   - **第二遍（count）**：count ≥ 0 时——若 run 总数 ≤ count 零删除；否则候选（剔豁免）按 **started 倒序**排列，**跳过前 count 个（最新 N 保留），其余全删**。
   - 两遍独立执行（日期遍删过的号不再参与 count 遍剩余集）。
5. **豁免（两遍共用）**：① run 的 promotion 历史非空（releaseStatus 非空白 = 曾 promote 过）→ **永不被 retention 删**（活体：promoted 的 build 1 在 count=1 下幸存）；② number ∈ `buildNumbersNotToBeDiscarded[]` → 豁免。豁免项**不占 count 名额**（保留数可因此少于 count）。
6. 无 build 名 delete 权 → 零删除 + log warn（**非 403**——REST 面 403 仅在资源层断言，retention 执行层是宽容跳过）。
7. `deleteBuildArtifacts=true` → 连制品删；retention 删除通道对「build JSON 已被并发覆盖删除」幂等容忍（NotFoundException 视为成功）。
8. build JSON 上传携带 `buildRetention` 块时——**当次发布尾部同步执行本删除流程**（发布即触发，非仅 POST /retention）。
置信度：高（源码 + 活体 count=0/400、count=1 删 2 留 3、promoted 豁免三探针）。**官方「setting retention does not immediately delete」表述与实现不符（E7）**。

### 11.7 批删集合语义（D07-R04/R08）

当客户端 DELETE /api/build/{name}?buildNumbers=… 或 POST /api/build/delete（body）——两出口共用同一删除 command，语义全同：
1. name blank → 400 `Please state the name of the build to be removed`；**非 deleteAll 且 buildNumbers 空/缺 → 400 `Please provide at least one build number to delete`**。
2. `deleteAll=1` → 删该名全部 run；成功 200 `All builds '<name>' under '<repo>' have been deleted successfully`（活体逐字，无尾点）。
3. 号删除：按 (name, numbers, repo) 查 run 集——
   - 集空且名不存在 → 404 `Unable to find build '<name>'`；集空但名存在 → 404 `Unable to find the given build numbers`（活体）。
   - **部分命中**：命中的删，成功段文案 `The following builds have been deleted successfully: '<name>#<n>', ….\n`（have；分隔 `, `；段尾 `.\n`）+ 未命中段 `Warning - the following builds could not be removed: '<n>'.\n`——整体 **200**（活体逐字）。
   - **多 run 同号**：一次调用每个号**至多删一个 run**（号从待删清单移除后，同号其余 run 跳过；删除顺序 = run 集内最新 started 先删）——需多次调用清同号多 run（源码控制流读法，中）。
   - 全部命中 → 200 仅成功段；请求号全不存在 → 走 3-1 404 分支。
4. 删除执行 = （可选删制品）+ undeploy build JSON 文件（级联删 DB 行：artifacts→dependencies→modules→build 事务内）。
5. 删除过程 warning → 500（`Warnings have been produced while removing certain builds. Please review the system logs…` 拼接既有消息）；error → cancel 异常（携状态码）。
6. POST /delete 的 body `{buildRepo, project, buildName, buildNumbers[], deleteArtifacts=false, deleteAll=false}`——**支持号含特殊字符**（数组分隔不受 CSV 限制，官方明示）。
置信度：高（源码 + 活体部分命中/404/deleteAll/清理复验）。

### 11.8 排序与「最新」语义（支撑 §2.1）

- 号清单（GET /{name}）：**started 倒序**（最新在前）——活体 3 run 3→2→1 顺序复验；空集 404。
- 名清单（GET /build）：按各名**最新 run 日期升序**（SQL `GROUP BY name, repo … ORDER BY max(build_date)`——L023-2D 双活体判向收口：升序（最早活跃在前），中→高）。
- 比较器族：全部号数字 → 数值比较；**任一号非数字 → 全体按字典序**；同号 tie → started → name。「最新 run」解析（promote/append 缺 started 时）= 同号中 started 最大。
- 列表端点 started 回显 UTC 归一；详情端点原样（§11.3）。
置信度：高（倒序+比较器源码+活体；名清单序向=升序经 L023-2D 双活体收口）。

### 11.9 L023-1 取证锚点

- 服务端实现源（OSS 7.161.16 树，绝对路径只读，非 reverse-src）：`build-handler/build-handler-service/src/main/java/com/jfrog/build/` 下——`resource/BuildPublicResource.java`（REST 面：204+checksum 头/retention 400/slim 参数/patternArtifacts 端点）、`command/{BuildCreateCommand, BuildAddModulesCommand, BuildPromoteCommand, BuildRetainCommand, BuildDeleteCommand, BuildGetAllCommand, BuildGetByNameCommand, BuildGetInfoCommand}.java`、`service/{BuildCreationService, BuildPromotionService, BuildRetentionService, BuildDeletionService, BuildReadService, BuildDatabaseService, BuildPermissionService}.java`、`dao/BuildsDao.java`（排序 SQL）、`model/{BuildRunComparators, api/PromotionResult, api/BuildsDeletionModel}.java`、`util/BuildServiceUtils.java`（hidden 坐标/参数解码）。
- 活体（172.16.58.130:8082，pro 7.161.15，2026-09-15）：PUT×5（含重复/同号多 run/hidden 坐标）、GET 列表/号单/详情×8、append×1、promote×3（status-only/no-target 404/failFast 400）、retention×2（count=0 400/count=1）、DELETE 部分命中/deleteAll×3、404 族×3——探针后全量清理（deleteAll 复验空态 404），零残留。
- 源码-活体版本差（7.161.16 vs 7.161.15）：本轮全部抽查点零分歧。
- L023-2D 差分收官回写（2026-09-15）：E14-E20 的活体证据 = `reports/compatibility/l023d-wire/{a,b}/`（A=172.16.58.130:8082 pro 7.161.15 / B=BinFlow dev 双发 wire 全量；重点：a/18 errors[] 信封、a/21 messages 面、a/22a `{"messages":[]}`、a/08 名清单升序、a/09 slim 键省略、a/24a count 门 label）+ 差分报告 `reports/compatibility/L023-buildinfo-diff.md` §3/§7。
- `_START_`/`_EXT_` 检索记录（含同日回执复核增补）：`grep -rn "_START_\|_EXT_"` 于 reverse-src 7.161.24 全树（命中仅前端 locale 噪声）、OSS 7.161.16 树 build-handler（零命中）、docs/ 与 PRD（零命中）；WebSearch JFrog 文档（零命中）；WebFetch 现役官方页 docs.jfrog.com/artifactory/docs/build-integration（**逐字零命中**，env 采集=纯客户端语义——详见 §9 #8 升格定案）与 jfrog/build-info README（零命中，其 env vars 均为测试配置）。

### 11.10 实现票拆分建议（CRUD / append+promote / 批删+retention 三票的边界与依赖序）

- **票 A（build CRUD 五件，D07-R01/R02/R03/R05 + 模型底座）**：表族落库（§3.2 对位）+ PUT 上传链（204+`X-Checksum-Sha256`、坐标 hidden 400、覆盖=先删后建、同号多 run、principal 覆写、部分 checksum 补全）+ 查询三跳（倒序号单、空态 404 族、UTC 归一 vs 原样回显、slim、statuses 回显含 timestampDate）。**其余两票的地基**——先落。AC 锚：§11.3。
- **票 B（append + promote，D07-R06/R07；依赖票 A）**：append 列表拼接语义（不合并——与直觉相反，diff 测试必须断言重复条目）+ promote 全执行序（§11.5 九步：虚拟仓解析→targetRepo 校验→收集→failFast 400→搬迁→属性→状态记录删除重建→响应 level 大写→400 门）。AC 锚：§11.4/§11.5；promote 是 docker 晋升腿（T-509）的前置。
- **票 C（批删 + retention，D07-R04/R08/R10；依赖票 A，可与票 B 并行）**：批删两出口同源语义（部分命中 Warning 段、404 两分支、多 run 同号怪癖）+ retention 两遍删序/promoted+pinned 豁免/count 正数门/async 缺省同步/发布尾部触发。AC 锚：§11.6/§11.7；webhook deleted 事件发射点在本票（retention 删除同发 deleted）。
- 面外维持：rename（R09）、docker promote 独立端点（R11→§2.6）、patternArtifacts、diff 响应形态——不进三票，翻案锚在案。
