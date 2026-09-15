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

---

# §9 复验段（L023-2G，2026-09-15 晚——L023-2F 返工后差分重跑）

- **B 被测**：dev.**c6f6c71a**（L023-2F 含 8 BUG 修复重部署；A 参照 7.161.15 不变）
- **重放门**：`tools/difftest/l0232d/` 原样全量重跑（45 REST 单元 + 2 shape + 1 搬迁聚合 + jf 四真腿）；pass2/pass3 判定集逐 case 一致（复跑门过；pass1 中 22c 为 harness 聚合串匹配 bug 的假阴性，已修——wire 实证双端均搬迁成功）
- **结果**：**SAME 34 / DIVERGENT 14**（首轮 24/24）

## 9.1 D-item 翻态对账（首轮 → 复验）

| D-item | 首轮 | 复验 | 证据 |
|---|---|---|---|
| D1 promote E12 abort 响应形 | DIVERGENT | **SAME** | 18 单元：双端 400 + **errors[] 信封逐字同**（含 status:400 内嵌） |
| D2 成功空 messages | DIVERGENT | **SAME** | 19/20 无自造汇总行；22a 双端 `{"messages":[]}` |
| D3 制品收集双通道 | DIVERGENT | **SAME** | 22a 双端 200；22c 搬迁双绿（双端 dev→404/rel→200 内容匹配）；**jf promote 真腿 exit=0 双端 + 搬迁 + promoted 豁免幸存集 `['/9','/7']` 逐位一致** |
| D4 artifact echo 丢字段 | DIVERGENT | **主体 SAME + 新残差 R3** | `path`/`originalDeploymentRepo`/空串三 checksum 全部回显（22b/02）；残差见 9.2-R3 |
| D5 键省略 vs 空值 | DIVERGENT | **SAME** | 09 slim 省键；16b comment 空省键；26c buildRetention 默认三字段（`buildNumbersNotToBeDiscarded:[]`/`deleteBuildArtifacts:false`）回显 |
| D6 详情 404 空格口径 | DIVERGENT | **SAME** | 10a/10c 尾空格、10d 逗号前空格逐字同 |
| D7 malformed started 文案 | DIVERGENT | **3/4 臂 SAME + 1 边缘臂差** | 见 9.3 观察一 |
| D8 media-type label 族 | DIVERGENT | **维持（预期内）** | 11/13a/13c/23a/23c/24a/24b 七单元仍仅 Content-Type 差 |
| D9 自定义 buildRepo projects 门 | DIVERGENT | **维持（预期内）** | 28 单元不变（A=400 信封/B=204） |
| D10 批删 deleteAll repo 内插 | DIVERGENT | **SAME** | B 侧探针：`All builds 'l023d-gate' under 'l023d-void-build-info' …`——回显**解析仓**（源码 L78 口径；双端活体仍不可达，A 侧 projects 门结构性阻挡） |
| D11 AQL property 面 | UNSUPPORTED | **维持（2F 范围外）** | jf build-publish 后置步仍 400 `Unknown AQL field: property` → exit=1（PUT 已落库）；T-511 翻转点在案 |

## 9.2 复验残余清单（14 DIVERGENT 单元 = 3 族）

- **R1=D8 label 族（7 单元，候裁维持）**：A 对 text body 标 application/json / B 标 text/plain——待 compatibility-engineer 裁 INTENTIONAL 与否。
- **R2=D9 projects 门（1 单元，候裁维持）**：自定义 buildRepo 写门——同上候裁。
- **R3=新残差：空集合物化（6 单元：02/15b/16b/19b/22b/26c）**：2F 修复 D4 时带出——module 无 dependencies 时 B 回显 `"dependencies": []`、build 无 modules 时回显 `"modules": []`；A 两处均**省略键**。与 D5（键省略 vs 空值）同族但方向相反（多发而非少发）。**分类建议 BUG-minor**（echo 保真；jf client-blind）。

## 9.3 记录级观察（coordinator 点名的两处近似，如实呈现）

- **观察一（D7 rest 计算）**：2F 的 `is malformed at` 尾段计算在 3/4 探针臂与 A 逐字同（`2026-13-45T99:99:99.999+0000`→`" 0000"`、`+000` 短时区→`" 000"`、`+`成空格→`" 0000"`）；**边缘臂差**：值在位 0 即不可解析（`started=xyz`）时 A 回 `Invalid format: "xyz"`（**无** malformed-at 子句——Java 解析器在零前缀失败时不产生尾段），B 恒发 `Invalid format: "xyz" is malformed at "xyz"`。wire：`l023d-wire/malformed-started-variants.log`。分类 BUG-minor 残余（单臂文案差，400/信封/前缀均同）。
- **观察二（D3 属性通道匹配规则）**：2F 的 build 属性（`build.name`/`build.number`/`build.timestamp` 三键）收集通道经 jf 真腿验证成立——jf `upload --build-name/--build-number` 打标 → `build-promote` 收集/搬迁/豁免全链双端一致（幸存集逐位同）。本轮未构造**属性键部分缺失/多仓同 checksum 冲突**等对抗臂——匹配规则在这些对抗面上的行为未差分，登记为后续探针（低风险：jf 主链路已绿）。

## 9.4 D07 翻态建议终版（落账归 conductor/compatibility-engineer）

| 行 | 首轮建议 | **终版建议** | 依据 |
|---|---|---|---|
| D07-R01 GET /api/build | 翻 ✅ | **翻 ✅（VERIFIED）** | 03/08/27 全 SAME（两轮稳定） |
| D07-R02 PUT /api/build | 不翻 | **翻 ✅（VERIFIED）**——R3 echo 残差挂 D07-R05（回显面），PUT 响应面（204+头/hidden/覆盖/同号多 run）全 SAME；buildRepo 门归 D9 行外登记 | 01/04/05 SAME + 26（尾部触发）SAME |
| D07-R03 GET /{name} | 翻 ✅ | **翻 ✅（VERIFIED）** | 06/07/10c SAME |
| D07-R04 DELETE /{name} | 不翻 | **候裁态（label 待裁）**——body 语义/文案逐字绿 | 11/13a-d 仅 D8 label |
| D07-R05 GET /{name}/{number} | 不翻 | **不翻（R3 空集合物化 + D7 边缘臂）** | 02/26c 等 6 单元 R3 |
| D07-R06 append | 不翻 | **不翻（R3——15b）**；语义面（E4/E5）绿 | 14/15a SAME |
| D07-R07 promote | 不翻 | **不翻（D7 边缘臂挂在 R05 文案族；本行 REST/jf 全绿但 statuses 回显 16b/19b/22b 载 R3）** | 16a/17/18/19/20/21/22a/22c + jf promote 全 SAME |
| D07-R08 POST /build/delete | 不翻 | **候裁态（label 待裁）**——blank-name/E6/deleteAll 文案 + D10 全绿 | 23a/b/c 仅 D8 label |
| D07-R09 rename | 面外 | 面外维持 | 未测 |
| D07-R10 retention | 不翻 | **候裁态（label 待裁）**——count 门/豁免/删序/尾部触发全绿 | 24-26b 仅 D8 label |
| D07-R11 docker promote | 面外 | 面外维持 | 未测 |

## 9.5 复验环境处置

资产删净复核：A `GET /api/build` → 404 空态；B 名单零 `l023d-` 残留（t512app-* 票外存量未触碰）；双端 `l023d-*` 四仓删净（`?deleteContent=true`）；B void 命名空间 404；jf config l023da 已移除；/tmp 日志清除。

---

# §10 终验段（L023-2I，2026-09-15 深夜——2H 微返工后两臂定谳）

- **B 被测**：dev.**5077f50b**（L023-2H 微返工重部署；A 参照 7.161.15 不变）
- **重放**：全量重放门两轮（pass1==pass2 判定集一致，SAME 35 / DIVERGENT 13）+ D7 六臂定向三角探针（`l023d-wire/d7-triangulation.log`）+ R3 全路径 JSON diff（去 12 行截断遮蔽）
- **13 残余单元构成**：D8 label 族 7（11/13a/13c/23a/23c/24a/24b，候裁维持）+ D9 门 1（28，候裁维持）+ R3 残留 5（15b/16b/19b/22b/26c）

## 10.1 D4-R3 臂定谳：**仍 DIVERGENT——但收敛为单面**

- 2H 已修两子面：`modules: []`（26c 复验轮差）与 `dependencies: []`（02/15b 等前轮差）——02 本轮全 SAME。
- **唯一残留面（5 单元）**：build PUT 未带 properties 时，B 详情回显顶层 `"properties": {}`，A **省略键**（全路径 diff 证：除 host/statuses 服务端时钟外唯一差异 = `.buildInfo.properties: A=<absent> B={}`）。带 properties 的 build（02）回显正常。
- **2J 修法**：无 properties 时键省略（与 2H 对 modules/dependencies 的修法同理——空集合不物化）。

## 10.2 D7 臂定谳：**仍 DIVERGENT——GET 面 2H 修对，PUT 面与值域臂残留**

六臂探针矩阵（A/B 逐字）：

| 臂 | A | B | 判定 |
|---|---|---|---|
| p1 GET `?started=xyz`（**差分原臂**） | `Invalid format: "xyz"` | `Invalid format: "xyz"` | **SAME（2H 修对）** |
| p3 GET 存在号 `?started=xyz` | 同上 | 同上 | SAME |
| p4b GET `?started=xyz2026` | `Invalid format: "xyz2026"` | 同 | SAME |
| 10b（首轮）GET `+`成空格 | `… is malformed at " 0000"` | 同 | SAME（§2.1 已录） |
| **p4a GET 值域越界** `%2B0000` 正确编码 | `Cannot parse "2026-13-45T99:99:99.999+0000": Value 13 for monthOfYear must be in the range [1,12]` | `Invalid format: "2026-13-45T99:99:99.999+0000" is malformed at ""` | **DIVERGENT（新臂）** |
| **p2 PUT body started=xyz**（**conductor 探针臂**） | `Invalid format: "xyz"` | `build started "xyz" must be an ISO8601 timestamp (yyyy-MM-dd'T'HH:mm:ss.SSSZ): build: invalid build info` | **DIVERGENT** |
| **p5 PUT body 值域越界** | `Cannot parse "…+0000": Value 13 for monthOfYear must be in the range [1,12]` | `build started "…" must be an ISO8601 timestamp …` | **DIVERGENT** |
| p6 GET 号单 `?started=`（对照） | 参数被忽略，200 | 同 | SAME |

**定谳回答（conductor 两问）**：① **差分原臂（GET 查询面）不是 conductor 见到的形态**——原臂在 5077f50b 已逐字对齐（p1/p3/p4b/10b 全 SAME）；conductor 引用的 `build started "xyz" must be an ISO8601…` 是 **PUT 上传面（body 内 started 校验）**——该面从未进过差分 case 集（首轮至今未测），B 仍持首轮自有文案，A 与其 GET 面同族（joda-time 原始异常文案直上 wire）。② GET 面的位 0 臂确已对齐；**D7 整体仍 DIVERGENT**，残留三臂 = PUT 两臂（p2/p5）+ GET 值域越界臂（p4a）。

**参照机制（供 2J）**：A 的该族文案 = joda-time `IllegalArgumentException.getMessage()` 原样入 errors[] 信封——格式错=`Invalid format: "<text>"`（有已解析前缀时追加 ` is malformed at "<未解析尾段>"`）；**值域错=换族** `Cannot parse "<text>": Value <n> for <field> must be in the range [<min>,<max>]`（field 名如 monthOfYear/hourOfDay 等 joda 字面）。

**2J 精确文案**（三臂）：
1. PUT body `started=xyz`（位 0 不可解析）→ `Invalid format: "xyz"`（无 malformed-at 子句）。
2. PUT body `started=2026-13-45T99:99:99.999+0000` → `Cannot parse "2026-13-45T99:99:99.999+0000": Value 13 for monthOfYear must be in the range [1,12]`。
3. GET `?started=2026-13-45T99:99:99.999%2B0000` → 同第 2 条（B 现回 `is malformed at ""` 应改为 Cannot parse 族）。

## 10.3 D07 终版翻态（沿 §9.4，本段确认）

R01/R02/R03 翻 ✅ 维持；R04/R08/R10 候裁态维持（仅 D8）；R05 仍不翻（R3 properties:{} + D7 残臂）；R06 不翻（15b 载 R3）；R07 不翻（16b/19b/22b 载 R3——promote 响应面本身绿）；R09/R11 面外维持。

## 10.4 终验环境处置

资产删净复核：A `GET /api/build` 404 空态；B 零 `l023d-` 残留；双端 `l023d-dev/rel-local` 删净（含 l023d-malf 探针名）；void 命名空间已清；/tmp 日志清除。

---

## §10.5 末验收口段（L023-2K，2026-09-15 夜末——2J 后四臂终确）

- **B 被测**：dev.**1afec0af**（L023-2J 微票后重部署；A 参照不变）
- **重放**：全量重放门两轮一致（pass1==pass2：**SAME 40 / DIVERGENT 8**）+ D7 三臂定向探针（`l023d-wire/d7-final.log`）+ R3 键集全量比对
- **四臂终确（预期全 SAME——实得全 SAME）**：
  1. **D4-R3 properties 省键臂 → SAME**：R3 五单元（15b/16b/19b/22b/26c）全翻绿；26c 键集 A/B 逐字同（`properties` 键双端省略；modules/dependencies 子面维持 §10.1 修复态）。
  2. **D7 PUT started 位 0（xyz）→ SAME**：双端 `Invalid format: "xyz"`。
  3. **D7 PUT started 值域 → SAME**：双端 `Cannot parse "2026-13-45T99:99:99.999+0000": Value 13 for monthOfYear must be in the range [1,12]`（joda 族逐字节同）。
  4. **D7 GET started 值域 → SAME**：同上逐字。
- **残余全景（8 单元 = 仅两候裁族）**：D8 media-type label 族 7（11/13a/13c/23a/23c/24a/24b）+ D9 自定义 buildRepo projects 门 1（28）——**零 BUG 类残差**；D11（AQL property→jf publish 腿）面外维持（T-511）。
- **D07 翻绿终版（收口）**：**R05/R06/R07 翻 ✅（VERIFIED）**——R05 的 R3+D7 残臂、R06 的 15b、R07 的 statuses 回显残差全部清零；连同 §9.4 的 R01/R02/R03，**D07 共 6 行翻 ✅**；R04/R08/R10 候裁态（纯 D8 label 挂账，裁 INTENTIONAL 即可整族翻绿）；R09/R11 面外维持。
- **环境处置**：资产删净复核（A 404 空态 / B 零 l023d / 双端仓净 / void 已清 / jf config 无残留）。
