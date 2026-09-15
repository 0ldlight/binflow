# L023-2D build-info 域全环差分报告（批次 1 收官环）

- **模式**：dual（A 参照全称可达，无降级）
- **A 参照**：http://172.16.58.130:8082（Artifactory pro 7.161.15，admin）
- **B 被测**：http://172.16.58.130:8083（BinFlow dev.77033399——含 L023-2A/B/C 全部对位）
- **工具**：`tools/difftest/l0232d/buildinfo_diff.py`（双发→normalize→diff，wire 证据 `reports/compatibility/l023d-wire/{a,b}/`）+ `jf-legs.sh`（jf 2.122.0 真腿）
- **规格基线**：docs/reverse/build-info.md（L023-1 勘误后）+ reports/agents/L023-2{A,B,C}.md 对位面
- **复跑门**：pass3/pass4 两轮判定集逐 case 一致（SAME 24 / DIVERGENT 24 判定单元，含 2 个 shape 单元）
- **L0~L12 层级**：本域覆盖 L1（REST build 族）为主面，L4（auth 门/403 面）与 L8（storage 搬迁副作用）随 promote 腿携带；L0 头面（media-type/校验头）随全部 case 携带；L5 客户端协议面=jf 真腿。L2/L6/L7/L9-L12 非本域面。

## 0. 摘要

| 口径 | 数 |
|---|---|
| case 判定单元 | 48（44 REST + 2 shape + 2 聚合副作用；另 jf 真腿 4 腿双端） |
| SAME | 24 |
| DIVERGENT | 24（去族后 **11 个 D-item**：BUG 8 / UNSUPPORTED 1 / UNKNOWN（INTENTIONAL 候选）2） |
| 提金候选 | §5 列出（A 侧绿面 wire 全量在 `l023d-wire/a/`） |

**一句话结论**：CRUD/批删/retention 的**语义面与逐字文案面已双绿**（E1/E2/E4/E6/E7/count 门/豁免/怪癖全对齐）；残差集中在四族——promote 响应形与制品收集（D1/D2/D3，**含票面点名的破坏性契约更新确认**）、echo 保真（D4/D5）、两处 404 文案细节（D6/D7）、media-type label 族（D8）——另有 projects 门（D9）与 AQL property 面（D11）两处结构性差异待裁。

## 1. normalize 口径（提案 fixtures/normalize.yaml#buildinfo，登记权在 compatibility-engineer）

| # | 规则 | 说明 |
|---|---|---|
| N1 | header drop | Date/Server/X-Powered-By/Set-Cookie/X-Request-Id/X-Artifactory-Id/X-Artifactory-Node-Id/Via/X-Jfrog-Version/Content-Length/Connection/Transfer-Encoding/X-Content-Type-Options |
| N2 | Content-Type json-family 等价（**PN1 提案**） | `application/json` ≡ `application/vnd.org.jfrog.[artifactory.]build.*+json`（A 回 vendor 型、B 回裸型——值差不参与判定，单列 D8 聚合项）；charset 后缀剥离 |
| N3 | X-Checksum-Sha256 → `<sha256-hex64>` 占位 + hex64 shape 断言 | 双端 manifest 字节面必然不同（principal 覆写+序列化器），语义=「对存储文档取 sha256」同构（L023-2A 登记漂移维持） |
| N4 | 绝对 URL 前缀 → `<BASE>` | 列表/号单/详情顶级 uri 的 host 段 |
| N5 | statuses[] 服务端时钟戳 → `<ts-sssz>`/`<epoch-ms>` | 仅 status+timestampDate 成对条目；客户端显式提供的 started/timestamp **字面比对** |
| N6 | 名清单断言域 = `l023d-` 前缀子集 | 双端实例携带第三方 build（B 有 t512app-* 存量 ×3，票外不碰） |

## 2. 逐 case 判定（family 分组；wire 证据 = `l023d-wire/{a,b}/<id>.hdr|.body`）

### 2.1 CRUD 族（PUT/查询三跳/DELETE）

| case | 面 | 判定 | 要点 |
|---|---|---|---|
| 01-crud-put-204-sha256hdr | PUT 全量上传 | **SAME** | 双端 204 空体 + `X-Checksum-Sha256` hex64（E1 对位维持）；01-sha-shape-a/b 双绿 |
| 02-crud-get-detail-echo | 详情回显 | DIVERGENT | started 原样时区（+0530 入 +0530 出）/principal=admin/durationMillis=0 **双绿**；差在 artifact echo（D4） |
| 03-crud-get-list-LIST | 名清单 | **SAME** | 顶级 uri 绝对 + `?buildRepo=artifactory-build-info`；条目 uri 相对；lastStarted **UTC 归一**（+0530 入 → +0000 出） |
| 04/05-crud-put-hidden-* | hidden 400 | **SAME** | `Build name must not start with '.'` / `Build number must not start with '.'` 逐字 |
| 06-crud-multi-run-LIST | 同号多 run | **SAME** | `/1`×2 并列；started 列表回显 UTC 归一口径双端一致 |
| 07-crud-number-desc-LIST | 号单倒序 | **SAME** | 严格倒序 1b(12:40)→3(10:00)→2(09:00)→1a(05:00z) 逐行同 |
| 08-crud-name-list-order-LIST | 名清单序向 | **SAME** | **升序**（按各名最新 run 日期，beta 08:30 → app 12:40）——**待验证 #5 收口：序向=升序，双活体一致** |
| 09-crud-slim | slim | DIVERGENT | modules `[]` 双绿；`properties` A **省略键** vs B `null`（D5；spec §1「置 null」勘误） |
| 10a/10c-crud-detail-404-* | 详情/号单 404 | DIVERGENT | 文案本体同；A 尾空格（D6） |
| 10b-crud-detail-404-malformed-started | malformed started | DIVERGENT | 双端均 400；文案异（D7） |
| 10d-crud-detail-404-started-clause | started 子句 | DIVERGENT | 子句 `, build started: <ts>` 双端都有；A 在逗号前多一空格（`99 , build started`）（D6） |
| 11-crud-delete-partial-e6 | E6 双段 | DIVERGENT（**body 逐字 SAME**） | `The following builds have been deleted successfully: 'l023d-app#3'.\nWarning - the following builds could not be removed: '99'.\n` 双端逐字节同；差仅 Content-Type label（D8） |
| 12a/12b-crud-delete-404-* | 404 两分支 | **SAME** | `Unable to find build 'l023d-nosuch'` / `Unable to find the given build numbers` 逐字 |
| 13a-d-crud-delete-same-number-* | 同号怪癖 | body **SAME** | 每调至多删一 run（删最新 started）、二次删清、空号单 404——全对齐（L023-2A 中置信实现**活体复验通过**）；13a/13c 差仅 label（D8） |

### 2.2 append 族

| case | 判定 | 要点 |
|---|---|---|
| 14-append-404 | **SAME** | `The build l023d-none:1 is not found` 逐字（E4 维持） |
| 15a-append-duplicate | **SAME** | 同 id 模块 append ×2 双端 204 |
| 15b-append-duplicate-echo | DIVERGENT | mod-a×3 各持己物（a.jar, a.pom, a.pom）**语义双绿**（E5 拼接不合并钉死）；差在 artifact echo（D4） |

### 2.3 promote 族

| case | 判定 | 要点 |
|---|---|---|
| 16a-promote-status-only | **SAME** | 200 `{"messages":[{"level":"INFO","message":"Skipping build item relocation: no target repository selected."}]}`——level 大写 + 逐字（E3 维持） |
| 16b-promote-status-only-statuses | DIVERGENT | statuses wire `{status,timestamp .SSSZ+0000,timestampDate epoch-ms,user=admin}`、repository/ciUser 省略——**双绿**；差 = comment 空时 A 省略键 vs B `""`（D5）+ artifact echo（D4） |
| 17-promote-targetrepo-404 | **SAME** | `Cannot find target repository by the key 'l023d-no-such'` 逐字 404 |
| **18-promote-failfast-400** | **DIVERGENT（D1，票面点名）** | **A = 400 `{"errors":[{status:400,message:"Unable to find artifacts of build 'l023d-app3' #1 from artifactory-build-info repo: aborting promotion."}]}` 信封；B = 400 messages[] body**——详见 §3-D1（立即上报项） |
| 19-promote-lenient-warning | DIVERGENT | WARNING `Unable to find the following artifacts of build 'l023d-app3' #1: a.jar` 逐字 + 200 + 状态落地双绿；B 多自造 INFO 汇总行（D2） |
| 20-promote-skipping-nostatus | DIVERGENT | INFO `Skipping promotion status update: no status received.` 逐字；B 多汇总行（D2） |
| 21-promote-invalid-timestamp | **SAME** | **400 + messages body**：`Skipping promotion status update: invalid\unparsable timestamp not-a-time.`（字面反斜杠）双端逐字同——**400 的 messages 面在 A 存在且 B 对齐**（D1 的双面论佐证） |
| 22a-promote-happy | **DIVERGENT（D3 功能性）** | **A = 200 `{"messages":[]}` + 制品真搬迁；B = 400 E12 abort**（manifest 通道缺失） |
| 22b-promote-happy-statuses | DIVERGENT | 显式 timestamp SSSZ 逐字回显/comment/ciUser/repository=l023d-rel-local——statuses 六元组**双绿**；差 = B 丢 artifact `path`/`originalDeploymentRepo`（D4） |
| 22c-promote-relocation-side-effect | **DIVERGENT** | A：dev-local 404 / rel-local 200 内容匹配（move 语义成立）；B：dev-local 200 / rel-local 404（未搬） |

### 2.4 批删 + retention 族

| case | 判定 | 要点 |
|---|---|---|
| 23a-batchdelete-body-e6 | DIVERGENT（**body 逐字 SAME**） | body 六字段 + 特殊字符号 `1.0.0-rc+1` E6 双段逐字同；差仅 label（D8） |
| 23b-batchdelete-blank-name | **SAME** | `Please state the name of the build to be removed` 逐字 |
| 23c-batchdelete-deleteall | DIVERGENT（body SAME） | `All builds 'l023d-bd' under 'artifactory-build-info' have been deleted successfully`（无尾点）逐字同；差仅 label（D8）；D10 见 §3 |
| 24a/24b-retention-count0/nobody | DIVERGENT（**body+status SAME**） | `Max count retention needs to be a positive number` 400 逐字双绿（body 缺失臂同句）；差仅 label（D8——**spec §11.6「text/plain」活体复伪：A 实标 application/json**） |
| 25a-retention-count1-promoted-exempt | **SAME** | 204 |
| 25b-retention-survivors-LIST | **SAME** | 幸存 `['/3'(count 槽), '/2'(promoted 豁免)]`，/1 删——两遍删序+豁免+不占名额全对齐 |
| 26-retention-publish-tail | **SAME** | PUT 携 `buildRetention:{count:1}` → 204 |
| 26b-retention-publish-tail-aftermath-LIST | **SAME** | 尾部窗口即删 4/5 → 余 `['/6']`（E7 即删性维持） |
| 26c-retention-aftermath-detail-echo | DIVERGENT | buildRetention 块 A 补默认三字段（`buildNumbersNotToBeDiscarded:[]`/`deleteBuildArtifacts:false`）vs B 只回 count（D5） |

### 2.5 自定义 buildRepo 门（projects 面）

| case | 判定 | 要点 |
|---|---|---|
| 27-buildrepo-custom-read-empty | **SAME** | 不存在 buildRepo 的读路径双端 404 `No builds were found`（读路径无仓校验） |
| 28-buildrepo-custom-put-gate | DIVERGENT（D9） | A = 400 errors[] `Build info repository 'l023d-void-build-info' does not exist`（**projects 门**：`<projectKey>-build-info` 须 project 存在，源码 BuildCreationService L92-97；报错文案对「仓不存在/project 不存在/缺后缀」三因共用）；B = 204 无门 |

## 3. D-item 详单（去族 11 项；分类建议——终裁权在 compatibility-engineer/conductor）

### D1【BUG·高·票面点名立即上报】promote E12 failFast abort 响应形：errors[] 信封 ≠ messages body
- **A 活体**：400 + `{"errors":[{"status":400,"message":"Unable to find artifacts of build '<n>' #<num> from <buildRepo> repo: aborting promotion."}]}`（l023d-wire/a/18-*.body、22a）
- **B 现值**：400 + messages[]（ERROR 行 + Skipping 行）（l023d-wire/b/18-*.body）
- **归因（源码 7.161.16）**：BuildPromotionService L206-210 `throw badRequestException(...)` → REST 异常映射器 → errors[] 信封。**promote 的 400 有两个面**：异常路径（E12 abort）= errors[] 信封；流程完成路径（如 21 invalid timestamp、failFast warning 集齐）= 400 + messages body（PromotionResult.HTTPStatus）。L023-1 §11.5-9「body 同形」为误读；L023-2B 按 messages 实现的该臂需改信封。**spec 勘误输入**。
- 证据锚：a/18、b/18、a/21（messages 面反证）

### D2【BUG·中】promote 成功/收尾 messages：A 零汇总行，B 自造 INFO 汇总行
- A 22a 成功 = `{"messages":[]}`（完全空）；B 追加 `Promotion of <n>#<num> to <repo> completed, 0 artifact(s) migrated (moved)`（19/20/22a）——L023-2B 登记「BinFlow 自有文案」现证无参照对应，应删。

### D3【BUG·高·功能性】promote 制品收集：A 双通道（manifest / AQL build 属性），B 双缺
- A 22a：module artifact 携 `originalDeploymentRepo`+`path` 全集 → **manifest 分支直查文件**（BuildArtifactService L101/L363-379），无需 build 属性关联 → 200 + 真搬迁（dev 404/rel 200）。
- B 22a：同请求 400 abort、未搬——B 的收集既不走 manifest 通道，也不走 build 属性（`build.name`/`build.number` 属性三键）通道（jf 真腿 promote 复证，见 §4-D11 联动）。
- **spec §11.5-5 勘误输入**：收集机制=「manifest（originalDeploymentRepo+path 全集直查）否则 AQL 按 build 属性」——非「checksum 搜索」；checksum 用于 verify。`originalDeploymentRepo` 是 wire 字段（§3.1 artifact 五字段之外的实际第六键——spec 字段集勘误）。

### D4【BUG·中·echo 保真】详情回显 artifact 字段：B 丢空串 checksum/path/originalDeploymentRepo
- A：`{"type","sha1","sha256","md5","name","path"[,"originalDeploymentRepo"]}` 全保留（空串也回）；B：空串字段全丢 + `path`/`originalDeploymentRepo` 恒丢（02/15b/16b/19b/22b）。
- 注：B 丢 `path`/`originalDeploymentRepo` 也是 D3 根因之一（存储模型未持这两个键）。

### D5【BUG·低·echo 保真】键省略 vs 空值三处
- slim：A 省略 `properties` 键，B 回 `null`（09）——**spec §1「properties 置 null」勘误为「键省略」**。
- statuses 条目 comment 空：A 省略键，B 回 `""`（16b）。
- buildRetention 块：A 补默认三字段（`buildNumbersNotToBeDiscarded:[]`/`deleteBuildArtifacts:false`），B 只回显显式给的字段（26c）。

### D6【BUG·低·文案】详情 404 message 空格口径
- A：无 started 子句时**尾空格**（`...build number: 99 `）；有子句时逗号前空格（`...99 , build started: <ts>`）。B：两处均无空格。（10a/10c/10d）

### D7【BUG·低·文案】malformed started 400 文案
- A：`Invalid format: "2026-09-15T09:00:00.000 0000" is malformed at " 0000"`；B：`build started "…" must be an ISO8601 timestamp (yyyy-MM-dd'T'HH:mm:ss.SSSZ): build: invalid build info`（自有文案）。（10b）

### D8【UNKNOWN·INTENTIONAL 候选】text 面 media-type label 族（含 vendor 型缺位）
- A 对 **text body**（DELETE 200 E6 / 批删 / retention 400 count 门）统一标 `application/json`（**错标**——body 是纯文本）；B 标 `text/plain`。**spec §11.6「400 text/plain」的 Content-Type 字面活体复伪**（body 文案本身逐字同）。
- 同族：A 对 json body 回 vendor 型（`BuildInfo+json`/`Builds+json`/`BuildsByName+json`/`PromotionResult+json`），B 恒 `application/json`（PN1 提案按 json-family 等价放行判定，差异本体归本项）。
- 裁定输入：jf CLI 对两者均 client-blind（真腿双通过）；B 的 text/plain 是「更正确」的标签——对齐 A（错标复刻）还是保留正确标签，属产品语义裁定。

### D9【UNKNOWN·INTENTIONAL 候选·projects 面】自定义 buildRepo 写门
- A：PUT `?buildRepo=<X>-build-info` 要求 project X 存在（源码 L92-97；无 project→400 `Build info repository '<X>' does not exist`——该文案对缺仓/缺 project/缺后缀三因共用，误导性文案为参照真值）；读路径无此门（27 SAME）。
- B：无门 204。BinFlow 无 projects 域（spec §1 M17 面外、ADR-0045 点 6）——候选 INTENTIONAL（超集承接），待裁。

### D10【BUG·低·B 单侧·源码级证据】批删 deleteAll 文案的 repo 内插
- 源码 BuildDeleteCommand L78：`format("All builds '%s' under '%s' have been deleted successfully", buildName, buildRepo)`——`<repo>` 位 = **解析后的 buildRepo**。
- B 实测（本会话清理腿）：POST /build/delete 携 `buildRepo=l023d-bi-build-info` → 文案回 `under 'artifactory-build-info'`（回显缺省仓而非解析仓）。
- 双端活体不可证：A 写路径被 projects 门挡（D9），无法在 A 侧自定义 buildRepo 内造 build。以源码为参照真值判 BUG。

### D11【UNSUPPORTED·已知面·client-impact 新证据】AQL `property` 字段缺失
- jf build-publish 真腿（§4）：jf 在 publish 后置步骤发 AQL `property` 查询 → B 400 `Unknown AQL field: property` → **exit=1**（PUT 本身已落库）。
- 该面 = aql.md §15「build.properties 入口 M17 面外维持 400（T-511 翻转点）」——分类 UNSUPPORTED；**但本腿证明它在 build-publish 主链路上被真实客户端踩中**（上传了 build-tagged 制品的 publish 必触发），建议 T-511 提权或拆 publish 专用最小面。

## 4. jf CLI 真腿（jf 2.122.0；日志 `l023d-wire/{a,b}/jf-legs.log`）

| 腿 | A（config l023da） | B（逐命令旗标） | 判定 |
|---|---|---|---|
| `jf rt upload --build-name/--build-number` | exit=0（build 属性标注落地） | exit=0 | **SAME** |
| `jf rt build-publish` | exit=0（回 buildInfoUiUrl） | **exit=1**：PUT 落库成功（号单可见、modules 在）后，AQL property 后置查询 400 ×3 + `/api/system/version='dev'` 解析尾错（既有债） | DIVERGENT（D11 + version 债） |
| `jf rt build-promote <app> 7 l023d-rel-local --status=smoked` | exit=0；制品真搬迁（dev 404/rel 200）；**run 7 获 promoted 豁免**（discard 后与最新 run 并存，见下行） | exit=1：400 E12 abort（messages 形=D1；收集失败=D3——jf 走 build 属性通道，B 缺） | DIVERGENT（D1+D3） |
| `jf rt build-discard --max-builds=1`（runs 7/8/9） | exit=0；幸存 `['/9','/7']`——count 槽=9 + **promoted 豁免=7**（jf 真腿侧证 §11.6-5） | exit=0；幸存 `['/9']`（7/8 已在——discard 本身行为同；豁免臂因 promote 未成未触达，REST 面 25a/25b 已双绿补位） | discard 语义 **SAME**（幸存差源于上游 promote 失败） |

jf 配置注记：B 无法走 `jf c add`（预检调 `/artifactory/api/security/encryptedPassword`——BinFlow 不仿真 /artifactory 根前缀，报 `no root mirror` 404），逐命令 `--url/--user/--password` 旗标可用；该前缀面是 BinFlow 既定决策（repo.md §1），登记不立票。

## 5. 提金候选（A 侧绿面 wire → golden，交 compatibility-engineer 评审）

`l023d-wire/a/` 全量即为活体金样候选；重点条目：
1. `01-crud-put-204-sha256hdr.hdr`（204 + X-Checksum-Sha256 头形）
2. `11-crud-delete-partial-e6.body`（E6 双段逐字）、`12a/12b`（404 两分支）、`13a-d`（同号怪癖序列）
3. `16a-promote-status-only.body`（INFO 大写逐字）、`21-promote-invalid-timestamp.body`（400+messages 面 + 字面反斜杠）
4. `18-promote-failfast-400.body`（**errors[] 信封形——D1 修订后的断言面**）、`22a-promote-happy.body`（`{"messages":[]}` 成功空体）
5. `24a-retention-count0.body`+`.hdr`（count 门 400 文案 + A 的 application/json label 真值）、`25b/26b`（豁免/尾部触发幸存集）
6. `08-crud-name-list-order-LIST.body`（名清单升序收口）

## 6. matrix D07 翻态建议（落账归 conductor/compatibility-engineer，本报告不写 matrix.yaml）

| 行 | 建议 | 依据 |
|---|---|---|
| D07-R01 GET /api/build | **翻 ✅（VERIFIED）** | 03/08 全 SAME；空态 404 双端逐字（27 + L023-2A 冒烟 S1） |
| D07-R02 PUT /api/build | **不翻（partial）** | 204+头/hidden/覆盖/同号多 run 绿；echo 族（D4/D5）与 buildRepo 门（D9）红 |
| D07-R03 GET /{name} | **翻 ✅（VERIFIED）** | 06/07/10c 全 SAME |
| D07-R04 DELETE /{name} | 不翻（仅差 D8 label） | E6/404/怪癖逐字绿；等 D8 裁定 |
| D07-R05 GET /{name}/{number} | 不翻 | D4/D5/D6/D7 |
| D07-R06 append | 不翻（仅差 D4 echo） | E4/E5 语义绿 |
| D07-R07 promote | **不翻（三 BUG：D1/D2/D3）** | 本域最重残差 |
| D07-R08 POST /build/delete | 不翻 | D8 label + D10 |
| D07-R09 rename | 维持面外（本轮未测） | — |
| D07-R10 retention | 不翻（仅差 D8 label） | count 门/豁免/删序/尾部触发全绿 |
| D07-R11 docker promote | 维持面外（本轮未测） | — |

## 7. spec（docs/reverse/build-info.md）勘误输入清单

1. §11.5-5/E12 + §2.4：**E12 failFast abort 响应形 = errors[] 信封**（异常路径）；400+messages 面仅属流程完成路径——L023-1「body 同形」勘误。
2. §11.5-5：制品收集机制 = **manifest（originalDeploymentRepo+path 全集）→ 否则 AQL build 属性**；`originalDeploymentRepo` 为 wire 字段集第六键（§3.1 artifact 字段集补）。
3. §1 slim 行：`properties` **键省略**（非「置 null」）。
4. §11.6-1：count 门 400 的 Content-Type = **application/json**（body 仍为该句纯文本）——「text/plain」字面复伪。
5. §1 GET 详情 404 文案：无 started 子句时**尾空格**、有子句时**逗号前空格**——精确字面补录。
6. §9 #5 关闭：名清单序向 = **升序**（双活体一致）。
7. §11.3 buildRepo 解析段补：自定义 buildRepo 写路径 = projects 门（`<projectKey>-build-info` 须 project 存在；错误文案三因共用「does not exist」）；读路径无门。

## 8. 环境处置与遗留

- **清场复核**：A `GET /api/build` → 404（空态）；B 名单零 `l023d-` 残留（t512app-* 票外存量未触碰）；双端 `l023d-*` 四仓全删（B dev-local 含 promote-abort 残留制品，`?deleteContent=true` 删净）；jf config l023da 已移除；/tmp 临时文件清除。
- **风险**：① 10b malformed-started 是 URL `+` 未编码的意外产物（双端同请求同触发，判定有效，但「正确 started 语义」由 10d 补位）；② B dev 实例 `started` 回显依赖客户端时区（jf 腿 +0800 入 +0800 出，与 02 的 +0530 口径一致，无 flake 面）；③ D10 无双端活体（projects 门结构性阻挡），源码级判 BUG 置信中高；④ docker/helm 等腿本票面外。
- **下步**：① D1/D2/D3 转修复票（promote 双面响应形 + 汇总行删除 + manifest/属性收集通道）；② D4-D7 echo/文案小票合并修；③ D8/D9 交 compatibility-engineer 裁定（INTENTIONAL 与否）；④ D11 提请 T-511 提权或拆票；⑤ 契约蓝本入册评审（contracts/buildinfo.yaml）。
