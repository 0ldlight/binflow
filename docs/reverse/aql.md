# AQL 与搜索域 行为规格（M15 FR-132/133/134 前置锚，T-407；**M16 增量段 §14**——statistics/usage + QRL + dates + UI 搜索族，T-435）

> **取证基准（PRD M15 §1.4 条款 1 特例，webhook.md 先例）**：AQL 有 JFrog 官方文档全覆盖 → **官方文档为唯一行为基准**；inv-1 §E / inv-2 §1.C 反编译锚点仅补官方文档空白（逐条标注「此条补充官方规范」）；**t226 活体核验腿**（OSS 7.84.10，2026-09-01 会话）作置信度校验——AQL 系 AddonType `oss` 档，无 entitlement 锁。证据：`reports/agents/t407-evidence/`（36 份响应体逐字节实录）。
>
> 置信度分级（本文件口径）：**高** = 官方文档 + 活体/反编译至少双源一致；**中** = 单源（官方或反编译）；**低** = 源间冲突或未能复现，待验证。**活体档位注意**：t226 是 OSS 档——凡「官方文档描述的能力」在 OSS 档被许可门降级的行为，本规格单列「OSS 档实测」并给出逐字文案（BinFlow 无许可门，实现面按官方全集口径，OSS 门行为仅作档位差异登记，不进 BinFlow 契约）。

## 0. 关键校正与口径归一（本规格的五个定案）

1. **`$not` 不存在于 AQL**（置信度：高——官方文档操作符面无 + 活体 400 双证）。PRD FR-133.1「$and·$or·$not 任意复合」前提**校准为 $and·$or 任意复合**：官方文档 criteria 页只登记 `$and`/`$or`（逗号分隔为隐式 $and）与 `$msp`；t226 活体 `{"$not":{...}}` → 400 parse error（`v03-not-operator.txt`）。BinFlow 不实现 `$not`（实现即超 parity，无对齐义务）。
2. **`.sort()` 在 OSS 档被许可门挡**（置信度：高——活体逐字 + 反编译门双证；官方文档全集含 sort）。t226 活体：`.sort({"$asc":[...]})` → 400，body 逐字 `Sorting is not supported by AQL in the open source version`（`v1b-sort-gated.txt`，application/json 但 body 为纯文本——wire 怪癖如实登记）。**BinFlow 无许可门：sort 按 A 层实现**（对齐官方全集语义，非 OSS 降级行为）。
3. **老搜索端点计数 = 14**（K65 勘误定案，置信度：高——反编译 SearchResource 铁证）。`SearchResource`（`@Path("search")`）注册**恰好 14 个**子资源：`artifact / gavc / prop / usage / creation / dates / pattern / license / checksum / badChecksum / dependency / versions / latestVersion / buildArtifacts`。inv-2 §1.C「14 种」正确；inv-1 §E「13 类」漏 `license`；主矩阵 L338「13 个」沿 inv-1 错值——两处已回写。官方 REST reference 另文档化 2 枚**SearchResource 外**挂载的搜索端点（`archive` 条目搜索、`latestVersionByProperties`），全量口径 = 官方 reference 16 枚非 AQL 搜索端点 + AQL（§8.3）。
4. **老搜索空集语义按端点族分化**（置信度：高——活体双臂实证）。`artifact/gavc/prop` 未命中 → **200 + `"results" : [ ]`**（`v8e/v8f/v8j`）；`usage/creation/dates` 未命中 → **404 + `{"errors":[{"status":404,"message":"No results found."}]}`**（`v8l/v8n/v8o`）。PRD FR-134「未命中空集 200」按端点族校准：**M15 三端点（gavc/prop/pattern）全部属 200-空数组族**（pattern 在 OSS 被 Pro 门挡，其空集形态按官方文档族归 200——以核验为准标注，见 §8.2）。
5. **K64 校准：`/api/search/artifact` name 匹配 = 大小写不敏感子串**（置信度：高——官方措辞 + 活体 + 反编译三源）。官方 OpenAPI：`name` = "Part of file name to search for"（无通配文档）；活体 `name=T228-LIB` 命中 `t228-lib-*`（`v8a`）；反编译 `ui.search.artifacts.caseInsensitive` **默认 true** + contains 比较器 + 特殊字符以 `^` 转义按字面处理。**结论：BinFlow as-built「LIKE 子串」维持（无断言反转）；两个可校准点交 T-417**——①大小写不敏感（BinFlow 现值待核，若区分大小写则对齐改大小写不敏感）②`*` 通配不做（官方/反编译均按字面处理，维持）。

## 1. REST 端点 wire（LC-68 锚；置信度：高——官方 OpenAPI 逐字 + 活体）

| 项 | 值 | 出处 |
|---|---|---|
| 方法/路径 | `POST /artifactory/api/search/aql`（BinFlow 映射 `POST /api/search/aql`） | 官方 OpenAPI `operationId: searchAql`；Since 3.5.0 |
| 请求体 | `text/plain`，body = AQL 查询文本（**非 JSON 包裹**） | OpenAPI requestBody + 活体 curl |
| 查询参数 | `compact`（boolean；`?compact=true`）——见 §3.3 | OpenAPI 无此参数（**反编译补白**，inv-1 §E）；活体实证 |
| 查询参数（回退） | body 为空时回退读 `?query=<urlencoded AQL>` 请求参数 | 反编译 getQuery 回退 + 活体实证（`v4c-query-param-fallback.txt`，200） |
| 请求头（流式） | `stream: true` → 游标分批（批 50,000 行），降服务端内存 | 官方 Execution 页 |
| 鉴权 | Basic / `X-JFrog-Art-Api` / Bearer token；**匿名不可用** | OpenAPI securitySchemes + 活体 |
| 成功 | 200 `application/json`，流式输出 `{results, range}`（§3） | OpenAPI + 活体 |
| 错误族 | 400 / 401 / 403 / 408 / 429 / 500（§4 逐字表） | 官方错误码表 + 活体 |

匿名两臂（置信度：高）：实例未开匿名 → **401** `{"errors":[{"status":401,"message":"Authentication is required"}]}`（活体 `v4b`）；实例开匿名但调用者匿名 → AqlResource 抛 **403**，文案逐字 `Only non-anonymous users are allowed to access AQL queries` + 尾 `\n`（反编译；活体不可复现第一臂——t226 未开匿名，**此臂以反编译为准**）。

## 2. AQL 语言规格（LC-69 锚）

### 2.1 域与查询入口（132.4② 子集边界输入）

查询模式：`<domain_query>.<action>(<criteria>).<尾缀链>`。

| 来源 | 入口域清单 |
|---|---|
| 官方文档（aql-syntax，updatedAt 2026-03-22） | `items` / `builds` / `build.promotions` / `releases` / `release_artifacts`（5 个文档化入口） |
| 反编译 RootElement（此条补充官方规范） | parser 实际接受 **13 个**入口：`items / properties / item.infos / statistics / artifacts(build) / dependencies / modules / module.properties / build.properties / build.promotions / builds / releases / release_artifacts`（官方未文档化的 8 个为内部/低曝光入口；`sensitive` 域在 inv-1 §E 所列九域中实为字段级敏感数据域而非查询入口——inv-1 §E 的九域清单按域模型（AqlDomainEnum signature）计，与入口域两口径，已注明） |

**BinFlow 子集边界表（132.4② 定案，Q1 暂行口径支撑）**：

| 域 | BinFlow 归属 | 依据 |
|---|---|---|
| `items`（item 域） | **M15 实现** | nodes 表基座全备 |
| `properties`（property 域，`@key` 嵌套于 items） | **M15 实现** | node_props + `idx_node_props_name`（M10 预留索引兑现） |
| `statistics`（`stat.*` 字段族） | **M16**（dep per-node 下载计数基建） | **注意**：stats 字段在 t226 OSS 档活体**可用**（`v16`，嵌套 `stats[]` 输出）——「OSS 无 stats」不成立，BinFlow 排 M16 的理由是自有基建缺失而非 parity 档位 |
| `builds / modules / dependencies / artifacts / promotions`（build 系五入口） | **远期 dep Build-info 域立项** | 入口在 OSS 档活体即 400 parse error（`v14`——语法层面即不存在，非运行时门） |
| `releases / release_artifacts` | 远期 dep Release Bundle 域 | 同上 |
| `item.infos`（内部树浏览用） | 不实现（内部域） | 反编译低曝光入口，官方无文档 |

未支持域的**错误形态**（Q3 校准，置信度：高）：Artifactory 对未支持/未知入口域与未知字段一律走**通用 parse error 400**（`Failed to parse query: ...`，见 §4.1）——**没有专门的 domain-not-supported 文案**（活体 `v14`/`v15` 双证）。BinFlow 的「400 诚实拒绝（envelope 含 domain/field 名）」为 **C 层自有增强文案**：码位（400）与 Artifactory 一致，文案更明确不违反 parity（Artifactory 文案本身不携带 domain 信息）；LC-69 维持 A（子集注记）。

### 2.2 item 域字段全集（M15 实现面；置信度：高——官方字段表 + 反编译枚举 + 活体 include("*") 三源）

官方字段表（aql-entities-fields-reference）17 字段 + 反编译 `AqlPhysicalFieldEnum` 补 2（标注◆）：

| 字段 | 类型 | 语义 | BinFlow 取数（基座映射详见 §10） |
|---|---|---|---|
| `repo` | String | 存储仓 key（**virtual 查询时结果行为实际成员仓 key**，§7） | nodes.repo_key 直取 |
| `path` | String | 父目录路径（folder 不带尾 `/*`；item 带——官方原文如是有排版残留，语义 = 父目录） | nodes.path 去末段派生 |
| `name` | String | 条目名 | nodes.path 末段派生 |
| `type` | Enum | `file` / `folder` / `any`；**查询不含 type 条件时默认只搜 file**（活体实证：全仓查询只出 file 行，`v09`） | path 尾 `/` 判 folder 派生 |
| `created` | Date | 创建时间 | nodes.created_at |
| `modified` | Date | 文件系统时间戳（最后修改） | nodes.updated_at（映射归 ADR-0043） |
| `updated` | Date | 最后上传时间 | nodes.updated_at |
| `created_by` | String | 创建者（**非 admin 脱敏**，§6） | nodes.created_by |
| `modified_by` | String | 最后修改者（同上脱敏） | 无独立列——ADR-0043 裁（暂映 created_by 或扩展列） |
| `size` | Long | 磁盘大小 | nodes.size |
| `depth` | Int | 路径深度（根 folder 起） | path 段数派生 |
| `original_md5` / `actual_md5` | String | 上传时/当前 MD5 | blobs.md5（BinFlow 无双值——映射归 ADR-0043，暂行同值） |
| `original_sha1` / `actual_sha1` | String | 上传时/当前 SHA1 | blobs.sha1 |
| `sha256` | String | SHA256（官方注明 5.5+） | nodes.sha256 |
| `virtual_repos` | String | 包含此 item 的 virtual 仓清单（**逻辑字段**；virtual 查询时隐式输出，§7） | 运行时由 repo 聚合解析派生 |
| ◆`id` | long | node 内部 id（默认结果不含） | — |
| ◆`repo_path_checksum` | String | repo+path 校验和（内部） | — |

statistics 域字段（M16 参考，活体可用）：`downloaded / downloads / downloaded_by / remote_downloaded / remote_downloads / remote_downloaded_by / remote_origin / remote_path`（+◆`id`/`remote_id`）。property 域：`key` / `value`（+◆`id`/`item_id`）。

### 2.3 property 域匹配（M15 第二域；置信度：高——官方 + 语法活体）

- 嵌套形态：`{"@<key>": {"$eq": "<value>"}}`，短形态 `{"@<key>": "<value>"}`；跨域全路径如 `artifact.module.build.@os`（build 系，远期）。
- 通配 catch-all：`{"@*": "<value>"}` = 任意键该值；`{"@<key>": "*"}` = 该键任意值（= 键存在性）。
- 展开形态（与 @ 形态等价的完整写法）：`{"property.key": {"$eq": "<key>"}}` / `{"property.value": ...}`（活体两种形态均 200 语法通过，`v13`——实例零属性数据，**匹配数据腿以核验为准**，BinFlow 实现后以 M10 属性夹具对拍）。
- **$msp（match single property）**：官方文档化操作符——普通 find 里多属性条件是「任一属性满足任一条件即命中」；`{"$msp": [ {条件}, {条件} ]}` 要求**同一个属性实例**满足全部条件。活体语法通过（`v12`）。M15 取舍归 T-409/ADR-0043（建议 M15 收录——property 域核心语义，成本一行组内判定）。

### 2.4 操作符集（K62 定案输入）

| 类别 | 操作符 | 置信度 |
|---|---|---|
| 比较符（官方文档全集 8 个） | `$eq` `$ne` `$gt` `$gte` `$lt` `$lte`（string/date/int/long）+ `$match` `$nmatch`（仅 string，支持 `*`/`?` 通配） | 高（官方表 + 活体） |
| 逻辑复合 | `$and` `$or`（逗号分隔 = 隐式 $and）；任意深度嵌套 | 高（官方 + 活体） |
| 单属性匹配 | `$msp` | 高（官方 + 语法活体） |
| 相对时间 | `$last`（→ `>` now−period）/ `$before`（→ `<` now−period） | 高（官方 + 活体 `v11`） |
| 相对时间周期后缀（官方表） | `milliseconds(mills,ms)`（**官方表拼写 "mills"，反编译实现为 "millis"+ms——以实现为准登记**）、`seconds(s)`、`minutes`、`days(d)`、`weeks(w)`、`months(mo)`、`years(y)` | 高（数值）/ 中（拼写差异如实登记） |
| 反编译补白（官方未文档化，此条补充官方规范） | `$eqic` / `$eqvic` / `$matchic`（忽略大小写三变体）；反编译另有 `mi`（minutes 短后缀） | 中（仅反编译；活体未逐个验证——**以核验为准**） |
| **不支持** | `$not`（§0-1）；`$contains`（PRD 草案曾提——官方无此操作符） | 高 |

非法相对日期 → AqlException `"Invalid relative date format for: <value>"`（反编译）。

### 2.5 尾缀方法链（`.include().sort().offset().limit()` + `.distinct()`）

| 尾缀 | 语义 | 置信度 |
|---|---|---|
| `.include(<fields>)` | 投影字段集；支持 `"*"`（全字段，活体 `v05`）；跨域字段（`"stat.downloads"`、`"property.key"`、`"@<key>"`）；include 未列出的域字段从输出剔除；**首次出现实体域字段时覆盖该域默认字段集**（反编译 handleIncludeExtraField） | 高 |
| `.sort({"$asc\|"$desc": ["<field>", ...]})` | 排序；字段须为输出字段（否则 `"Only the result fields are allowed to use in the sort section."`）；字段重复 → `"Duplicate fields, all the fields in the sort section should be unique."`；**OSS 档被许可门挡**（§0-2） | 高（文案反编译）/ 门行为活体 |
| `.offset(<n>)` | 跳过 n 行（分页）；range.start_pos 回显 | 高（活体 `v07`） |
| `.limit(<n>)` | 上限 n 行；range.limit 回显；未指定时**无服务端默认上限**（self-managed，§5） | 高 |
| `.distinct(<bool>)` | 布尔**不带引号**（`.distinct(true)`；带引号 `"true"` → parse error，活体 `v10a`）；默认 true | 高 |
| `.transitive`（remote/virtual 专用） | §7 | 高 |
| `.delete(...)` / `.update(...)` / `.dryRun` | **破坏性动词**（官方文档化：`items.delete()` / `properties.update().keys().newValue()`）；**BinFlow M15 不实现**（查询域只读；实现面立项另裁）——t226 核验全程零触碰 | 高 |

**链序敏感（置信度：高——活体双臂）**：尾缀须按 `include → transitive → sort → offset → limit → distinct` 顺序；`.limit(1).sort(...)` → parse error（`v1c`，错误位置指向 sort 段）。与官方文档示例的书写顺序一致（官方未明文声明顺序约束——此条为活体+反编译补充）。

**语法通则**（官方，高）：字段名与字符串值双引号；数字可带/不带引号；比较符/逻辑符 `$` 前缀；字段名**大小写敏感**；`null` 字面量（`{"stat.downloads":{"$eq":null}}` = 零下载——官方明示 stat 域零值须用 null 非 0）；日期 W3C/ISO8601（部分精度可用，`YYYY` / `YYYY-MM` / …）；通配 `*`（任意串）`?`（单字符）**仅 $match/$nmatch 内生效，他处按字面**。

## 3. 响应 envelope（置信度：高——反编译流器逐字 + 活体逐字节）

### 3.1 wire 形态（逐字，活体 `v01-basic-chain.txt`）

```
\n{\n"results" : [ {…},{…} ],\n"range" : {…}\n}\n
```

- 前缀逐字 `\n{\n"results" : [ `（反编译 QUERY_PREFIX 常量，活体逐字节一致）；行间分隔 `},{`；空集时 `"results" : [  ]`（两个空格，活体 `v6`）。
- 默认（pretty）输出：每行多行缩进 JSON；**`?compact=true`：外层包裹不变、行体与 range 压成单行**（活体 `v02-compact.txt`——注意 compact 压的是行序列化，不是整体一个 JSON 行）。
- 日期回显格式：ISO8601 带毫秒 UTC（活体 `"created" : "2026-08-23T08:19:04.618Z"`）。

### 3.2 range 对象（官方 OpenAPI AqlRange + 反编译 Range 一致；高）

| 字段 | 语义 | 备注 |
|---|---|---|
| `start_pos` | 本页起始（= offset 回显） | 活体 offset(1) → 1 |
| `end_pos` | 本页返回行数（相对语义 = 末行序） | — |
| `total` | **流式实现下与 end_pos 相同**（本页行数；非全量计数——官方示例与活体一致） | 客户端勿当全量 total 用 |
| `limit` | 查询 limit 回显；**未指定时省略该键** | 活体 |
| `notification` | 命中硬上限时的通告文案（§5） | 官方示例含 |

### 3.3 行形态

默认 item 域输出字段（无 include 时）：`repo, path, name, type, size, created, created_by, modified, modified_by, updated`（活体）。include 覆盖之。跨域字段嵌套对象输出（stats 域 → `"stats" : [ {…} ]` 数组，活体 `v16`）。

## 4. 错误面（逐字文案表；LC-68/69 错误族锚）

错误 envelope 统一 `{"errors":[{"status":<code>,"message":"<text>"}]}`（官方 OpenAPI ErrorResponse + 活体一致）——与 BinFlow E-01 同构。

| # | 触发 | 码 | message 逐字 | 置信度 |
|---|---|---|---|---|
| E1 | 语法错/未知字段/未知域/非法操作符/链序错 | 400 | `Failed to parse query: <原查询>, it looks like there is syntax error near the following sub-query: <残段>` | 高（反编译格式串 + 活体三臂 `v1c/v14/v15`） |
| E2 | 空 body 且无 query 参数（7.84.10 活体） | 400 | `{"errors":[{"status":400,"message":"Bad Request"}]}`（通用 Bad Request——空 body 未达 AQL 资源层） | 高（活体 `v4a`） |
| E3 | 空 body 且无 query 参数（新版代码路径） | 400 | `Couldn't find the query neither in the request URL and the attached file` | 中（反编译；与 E2 版本漂移——**以核验为准**，t226 现值 E2） |
| E4 | 查询超长（>6,000 字符） | 400 | `AQL query is too long; please reduce the query length to less than 6000 chars` | 中（官方文档声明 6000 默认 + 反编译；**活体 12k 查询 200 未复现**（`v4d`）——版本行为差异，BinFlow 取官方 6000 口径） |
| E5 | 匿名（实例未开匿名） | 401 | `Authentication is required` | 高（活体 `v4b`） |
| E6 | 匿名（实例开匿名） | 403 | `Only non-anonymous users are allowed to access AQL queries\n`（尾换行如则） | 中（反编译；活体不可复现第一臂） |
| E7 | 查询执行超时 | **408** | cause 消息透传（SQLTimeoutException 或含 "cancel" 的 cause） | 高（官方错误码表 408 + 反编译 AqlExceptionMapper；**注意：408 非 504/503**） |
| E8 | QRL 并发超限 | **429** | `too many requests`（小写逐字） | 中（反编译 ErrorResponse；官方错误码表列 429；**活体 8 路并发未触发**（语料过小）——429 body 以反编译为准） |
| E9 | `.sort()` @ OSS 档 | 400 | `Sorting is not supported by AQL in the open source version`（body 纯文本，Content-Type 仍 application/json） | 高（活体逐字；BinFlow 无门不实现此文案） |
| E10 | 流式读失败 | 400 | `An error has occurred while streaming the AQL response `（尾空格）+ cause 拼接 | 中（反编译） |

其余校验文案（反编译 AqlQueryValidator，置信度：中——未在活体逐条复现，**以核验为准**）：

- 非 admin include 缺三字段：`For permissions reasons AQL items domain demands the following fields: repo, path and name.`（官方 Execution 页以日志形态佐证："AQL minimal field expectation error: repo, path and name"）
- `When including virtual_repos field, the following fields are also required: repo, path and name.`
- transitive 族：`Only the items domain can be used with the transitive property` / `When including the transitive property, offset is not supported by AQL` / `When including the transitive property, the repo property has to be specified with the $eq operator` / `When including the transitive property, only a single repo can be specified` / `Only items and properties subdomains can be used in the include section with the transitive property`
- `Invalid relative date format for: <value>`

## 5. 资源治理与限流（K63 校准表；LC-73 C 层定案输入）

| 维度 | Artifactory 官方/反编译现值 | BinFlow 暂行（K63） | 口径 |
|---|---|---|---|
| 结果上限 | **self-managed 无默认上限**（SaaS 7.100.2+ 固定 500,000 = `aql.search.query.max.limit`；self-managed 7.90.6+ 可配）；`.limit()` 是唯一行数约束 | 上限 1,000 行 + 截断标记 + offset/limit 分页可达全量 | **冲突 → C 层留痕不硬仿**（Q2：单机 SQLite 防护必要性；官方 500k 量级对 BinFlow 无意义） |
| 上限命中通告 | range.notification = `AQL query reached the search hard limit, results are trimmed.`（+可选 `. Please address configurable limit.`） | 沿用同文案（A 层——官方 OpenAPI 示例逐字在册） | A |
| 查询长度 | 6,000 字符默认（`aql.search.query.size.limit`）→ 400 | 沿用 6,000 | A |
| 并发上限 | `artifactory.aql.queries.limit` 默认 **3**；7.84 起生效（7.84.x enabled 默认 true；**7.84.16+ 默认 false**——反编译版本默认 false）；豁免 UA：`pipeline, xray, artifactory, distribution, federation` | 并发 4 → 429 + Retry-After | 并发数 C 层裁量；**429 码位与 body `too many requests` 对齐（A）**；Retry-After 头官方未文档化（C 层自有） |
| 并发等待 | pending timeout 10,000ms（等待期后 429） | BinFlow 直接 429（不排队） | C 层简化 |
| 执行超时 | REST 900s（15min）/ UI 120s / streamed 7200s / override 未设 → **408** | 10s → 超时形态 | **码位对齐 408（A）**；时长 C 层（10s 单机合理） |
| 流式批大小 | 50,000 行（`aql.streamed.fetch.size`） | 结果集流式装饰（T-413） | 形态对齐，参数自有 |
| QRL REST | `v1/system/query_rate_limiter`（三态 enabled/disabled/simulation + 指标 job；inv-1 §E 高置信） | M16+（M15 不建） | M16 |

## 6. 权限语义（LC-72 锚；置信度：高）

- **非 admin 查询强制携带 `repo`/`path`/`name` 三字段**（include 或默认字段集）——权限行级过滤的依据；items 域外，builds 域要求 `name/number/repo`。官方 Execution 页 + 反编译双证。
- **行级过滤在结果流侧**：SQL 执行后逐行 canRead 复核（admin 直过；非 admin 按可读仓过滤）——「查询不限 repo 也只回可读行」的机制基础（反编译；BinFlow allow() 织入点走 SQL 谓词，等价语义，实现归 ADR-0043/T-413）。
- **用户身份脱敏**：非 admin 调用者的 `created_by/modified_by/downloaded_by` 等字段值替换为字面量 `unknown`（反编译 ObfuscationUtils，开关 `security.personal.info.non.admin.exposure` 默认关=脱敏；**活体为 admin 不可复现，以核验为准**——BinFlow 是否对齐脱敏交 ADR-0043/PM，暂无 PRD 断言）。
- scoped token 域扩展（`artifact:*/**:r` → items；`build:artifactory-build-info/**:r` → builds）——BinFlow token 无 scope 体系，登记不实现（远期 dep）。
- 老搜索面（§8）鉴权各端点「privileged user (can be anonymous)」或「non-anonymous」不一——artifact/gavc/prop 可匿名（实例开匿名时）；BinFlow 沿 T-92 既有门（401/403）。

## 7. virtual / remote 仓在 AQL 中的语义（FR-133.5 定案；置信度：高——官方 + 反编译双源互证）

1. **virtual key 是合法查询值，非查询实体**：`items.find({"repo":"my-virtual"})` 被服务端**展开为成员仓（local + federated + remote 的 cache 仓）的 OR 组**；`$eq` 时原条件被替换，`$match`/范围比较时原条件保留并与成员条件 OR（负向 `$ne/$nmatch` 为 AND 排除组）（反编译 VirtualRepoCriteriaDecorator 逐分支）。
2. **结果行 `repo` 字段 = 实际存储仓 key**（成员 local/…-cache），不是 virtual key（官方 transitive 示例输出 `"repo": "docker-remote-repo-cache"` 逐字佐证）。
3. **`virtual_repos` 逻辑输出字段**：查询目标是 virtual 时**隐式加入输出**（官方明文）；平时须显式 include。
4. **不存在的 repo key（含拼写的 virtual 名）→ 200 空集**（活体 `v6`——无存在性校验、无错误）。
5. **`.transitive`（Smart Remote 追源）**：规则官方明文——仅 items 域、单仓 + `$eq`、禁 sort/offset、include 仅 items+properties、追源上限 5 个 remote 仓（`aql.transitive.remote.repos.limit` 默认 5）。**BinFlow M15 不实现 transitive**（dep smart remote 深链语义；远期行）。
6. **AQL 结果遵循仓的 include/exclude patterns**（官方明文；反编译 `aql.query.apply.includeExcludePatterns` 默认 true）——BinFlow remote 仓若有 patterns 需在查询侧同过滤（T-411/T-412 对齐点）。

**133.5 暂行口径定案**：「查询对象 = 实际存储行（local + remote 缓存行），virtual 不作查询实体」**成立**，且补充——virtual key 须作为**合法查询值透明展开**（M15 实现面：T-411 编译器将 repo 条件值经 repo.Service 解析 virtual 成员后织入谓词）。

> 活体核验边界如实登记：t226 实例当前 8 仓全 local（无 virtual/remote 仓），且 INC-1 只读纪律禁止建仓——**virtual 语义活体对拍不可执行**，本节以官方文档 + 反编译双源定案（双源一致，置信度高）；待有 virtual 仓的实例（或 BinFlow 自身 e2e 对拍 T-415/T-412）补活体腿。

## 8. 老搜索端点族（FR-134/K64/K65 锚）

### 8.1 计数勘误（K65 定案）

见 §0-3。勘误回写：inv-1 §E「老搜索族（13 类）」→ 14（补 license）；主矩阵 L283/L338 计数与就绪度同步回写。官方 REST reference 全量（SearchResource 外 2 枚补挂）：`archive`（Archive Entries Search，官方标注 deprecated）、`latestVersionByProperties`（Latest Version Search Based on Properties，反编译在 RestAddon 接口有方法面）——两枚登记入端点全景远期行（PRD §5.7 未列，PM 重审项，BinFlow 不实现）。

### 8.2 OSS 7.84.10 活体可用性矩阵（t226 实测，2026-09-01）

| 端点 | OSS 活体 | 空/错语义 | 备注 |
|---|---|---|---|
| `GET /api/search/artifact?name=` | **200 可用** | 未命中 200 `results:[]` | name = 大小写不敏感子串（§0-5）；envelope **uri-only**：`{"results":[{"uri":"…api/storage/<repo>/<path>"}]}`（uri 指向实例 base URL 的 storage API） |
| `GET /api/search/gavc?g=&a=&v=&c=&repos=` | **200 可用** | 未命中 200 `results:[]` | 坐标至少一项（官方）；repos 限定 |
| `GET /api/search/prop?props=&repos=` | **200 可用** | 未命中 200 `results:[]` | 参数名官方勘正：**任意查询参数即属性键**（`GET /api/search/prop?build.name=x&repos=…`），`props=` 形态亦接受（活体 200）；键无值 = `*` |
| `GET /api/search/pattern?pattern=` | **400 Pro 门** | `This REST API is available only in Artifactory Pro (see: jfrog.com/artifactory/features). If you are already running Artifactory Pro please make sure your server is activated with a valid license key.\n`（尾换行） | 官方注明 Product: Pro；`**` 模式不支持（官方） |
| `GET /api/search/checksum?md5=&sha1=&sha256=` | 400 Pro 门 | 同上文案 | BinFlow T-92 已实现（超 OSS 档、对齐官方 Pro 文档——既有面不动） |
| `GET /api/search/usage?notUsedSince=<epoch-ms>` | **200 可用**（epoch 毫秒形态） | 未命中 **404 `No results found.`** | 行 = `{uri, downloadCount, lastDownloaded, remoteDownloadCount,…}` |
| `GET /api/search/creation?from=&to=` | **200 可用**（epoch 毫秒；to 缺省 now()） | 未命中 **404 `No results found.`** | 行 = `{uri, created}` |
| `GET /api/search/dates?from=&to=&dateFields=` | 200 可用（同族） | 未命中 **404 `No results found.`** | dateFields 决定回显日期字段 |
| `GET /api/search/license` | 400 Pro 门 | — | 远期 dep license 域 |
| `GET /api/search/badChecksum` | 400 Pro 门 | — | 响应上限 10,000（官方） |
| `GET /api/search/dependency` | 400 Pro 门（GET） | — | — |
| `GET /api/search/versions` / `latestVersion` | 400 Pro 门 | — | `remote=1` 触发远端搜索（官方） |
| `POST /api/search/buildArtifacts` | 400 Pro 门（**POST**——GET 405） | — | body JSON `{buildName, buildNumber,…}` |
| （外挂）`GET /api/search/archive` | 未活体（官方 deprecated） | — | 远期登记 |
| （外挂）latestVersionByProperties | 未活体 | — | 远期登记 |

**档位口径（重要）**：`pattern/checksum/license/badChecksum/dependency/versions/latestVersion/buildArtifacts` 在 OSS 档被 Pro 门挡 ≠ BinFlow 不做——BinFlow 无许可门，按**官方文档全集**（描述的是 Pro 行为）对齐实现。M15 三端点（gavc/prop/pattern）中 pattern 属此类：**对齐源 = 官方 pattern 文档语义，非 OSS 400 行为**（LC-70 维持 A，注记在案）。

### 8.3 老搜索共通 wire（T-417 输入；置信度：高）

- envelope `{"results":[…]}`，行为 **uri 为主的瘦行**（artifact/gavc/prop 仅 `uri`；creation 加 `created`、usage 加下载统计字段）——**BinFlow T-92 as-built 的 E-09 FileInfo 全字段行为超集**（含 uri/downloadUri），FR-134.4 已定 E-09 复用——差异留痕不追改（超集方向，客户端兼容）。
- **老搜索结果默认上限**：`artifactory.search.userQueryLimit` 默认 **1,000**（对 internal/anonymous 用户；`limitAnonymousUsersOnly` 默认 true——官方 Searches API OpenAPI 头注）——K63 老搜索侧 1,000 上限的官方依据。
- 参数缺失/非法 → 400 errors[]（官方 BadRequest 例）；`repos` CSV 限定通用于全族。
- K64 两个落笔臂（§0-5）：大小写不敏感对齐 + `*` 字面维持——断言更新（如有）归 T-417 票内留痕，**无断言反转**（子串语义本体维持）。

## 9. 与 PRD/规格的效力关系（132 各项归位）

| PRD 条目 | 本规格定案 |
|---|---|
| 132.1 官方锚点 | §1~§3、§8 |
| 132.2 inv 补白 | §11（逐条「此条补充官方规范」） |
| 132.3 t226 活体 | §0（五定案）+ 逐节「活体」标注 + 证据目录；不可核验项「以核验为准」零静默升格 |
| 132.4① 14-vs-13 | §0-3 / §8.1（=14，回写 inv-1 + 主矩阵） |
| 132.4② 子集边界 | §2.1 表（M15 item+property / M16 stats / 远期 build 系 + releases） |
| 132.4③ K64 | §0-5 / §8.3（维持 LIKE 子串 + 大小写不敏感校准点交 T-417；`*` 维持字面） |
| 132.4④ 基座映射 | §10 |
| K62（子集边界+文案） | §2 + §4（E1 文案 = BinFlow 400 文案的对齐底本；domain-not-supported 为 C 层增强文案，码位 400 一致） |
| K63（资源门） | §5 校准表（408/429/6000/截断文案 A 对齐；上限值/并发数/超时时长 C 层） |
| K65（老搜索清单 + dates 顺车） | §8.2——`creation/dates` 形态 = epoch 毫秒直查 created_at，**判 trivial 可顺车**（单列区间查询 + 404 空集语义 + 行加 created 字段）；`usage` dep stats 基建维持 M16；顺车与否归 T-417 票内余量条款 |

## 10. 基座映射表（132.4④；交 ADR-0043 消费——差异留痕待对齐）

BinFlow 基座（architecture §15.3 + migrations 001/013）：`nodes(repo_key, path, sha256, size, mime, created_by, created_at, updated_at)` PK(repo_key,path)；`blobs(sha256, sha1, md5, size, created_at)`；`node_props(repo_key, path, name, value)` PK 四列 + `idx_node_props_name(name, value)`。

| AQL 面 | 基座取数 | 派生/缺口（ADR-0043 裁） |
|---|---|---|
| `repo` | nodes.repo_key | virtual 展开在编译期经 repo.Service 解析（§7-1） |
| `path` / `name` | nodes.path 拆分：末段 = name，余段 = path | **无独立列——SQL 内派生**（substr/instr 或应用层投影；索引影响 ADR 评） |
| `type` | path 尾 `/` 判 folder，余 file | 默认 file 语义 = 查询侧补 `type=file` 谓词（§2.2） |
| `depth` | path 段数 | 派生 |
| `size` | nodes.size | 直取 |
| `created` | nodes.created_at | 直取（时区/格式投影） |
| `modified` / `updated` | nodes.updated_at | **单列双义**——BinFlow 无 modified 独立源；暂行同值映射（差异留痕） |
| `created_by` | nodes.created_by | 直取；`modified_by` 无列——暂行同值或列扩展（ADR） |
| `sha256` | nodes.sha256 | 直取 |
| `original_sha1/actual_sha1`、`original_md5/actual_md5` | blobs.sha1 / blobs.md5（join nodes.sha256→blobs） | **无双值语义**（BinFlow 不存 original/actual 分离）——四字段同源两值（差异留痕；M15 子集建议只收 `actual_*` 族官方默认输出不含、sha1/md5 经 include 可取） |
| `@key` / property 域 | node_props join（nodes.repo_key+path = node_props.repo_key+path） | `idx_node_props_name(name,value)` 兑现点（M10）；多值 = 多行（组内聚合投影） |
| `virtual_repos` | 运行时 repo 聚合派生 | M15 含（§7-2 隐式输出语义） |

## 11. 反编译补白清单（「此条补充官方规范」；inv-1 §E / inv-2 §1.C）

- inv-1 §E AQL 行（高）：`POST /api/search/aql?compact` 端点形态、九域清单、`AqlTooManyRequestsException`→QRL 关联、SQL builder/optimizer/result decorator 架构——**全部与官方文档/活体一致**，其中 compact 参数、query 参数回退、429 body `too many requests`、错误文案族、`range` 尾段、QUERY_PREFIX 逐字、行级 canRead 过滤、`unknown` 脱敏、`$eqic/$eqvic/$matchic` 变体、13 入口域全集、`.delete/.update` 动词与链序敏感为**官方文档未覆盖**的补白（本规格 §1/§2/§3/§4/§6 各节已融入，置信度按节标注）。
- inv-2 §1.C 14 端点枚举（高）：SearchResource 子资源清单与活体逐端点状态（§8.2）互证；UI 搜索族（artifactsearch/stashResults/packagesSearch/syntax-search）与 QRL REST 归 M16（§5）。
- inv-1 §E「九域」口径勘正：按 AqlDomainEnum 域模型 signature 计（item/stat/property/build/module/dependency/promotion/release(+file)——`sensitive` 在反编译中无对应查询入口域，为敏感数据域概念；查询入口域 = 13 个，RootElement 铁证）。

## 12. 待验证清单（低置信/未复现汇总——零静默升格）

| # | 项 | 现值依据 | 验证途径 |
|---|---|---|---|
| V-a | 429 body `too many requests` 与 Retry-After 头有无 | 反编译（body）/官方（无头文档）；活体 8 路未触发（语料小） | 大语料实例并发注入；或 BinFlow 实现后以 K63 门自证（C 层参数） |
| V-b | 查询长度 6,000 上限在 7.84.10 的实际生效值 | 官方文档+反编译声明 6,000；**活体 12k 查询 200** | 更新版活体或大查询逐段二分；BinFlow 按官方 6,000 实现（T-415 断言） |
| V-c | E3/E6 文案（空查询资源层分支、开匿名实例的 403 臂） | 反编译；活体实例形态不可达 | 开匿名的活体实例；BinFlow 按 E1/E2/E5/E6 全集实现（T-415 断言逐字） |
| V-d | property 域匹配的数据腿（@key/@*/$msp/property.key） | 语法活体 200；实例零属性数据 | M10 属性夹具（BinFlow 侧）+ 有属性数据的活体实例 |
| V-e | 非 admin `unknown` 脱敏现值 | 反编译（默认脱敏）；活体仅 admin | 活体非 admin 用户凭据（carol/dave 口令不在案——不爆破）；BinFlow 侧 ADR-0043 定 |
| V-f | `$eqic/$eqvic/$matchic` 与 `mi` 后缀可用性 | 反编译枚举 | 活体逐一探针（BinFlow M15 子集可不收——官方未文档化） |
| V-g | virtual 仓 AQL 活体对拍（repo 展开行/virtual_repos 隐式输出） | 官方+反编译双源一致（高）；t226 无 virtual 仓不可拍 | BinFlow T-412/T-415 e2e 对拍腿（virtual 夹具在案） |
| V-h | pattern 端点空集形态（OSS Pro 门挡——200 空数组族推断） | 官方族归类推断 | BinFlow 实现后定案（FR-134 AC3 已含跨仓臂） |

## 13. 取证锚点（官方文档 2026-09-01 实取 + t226 会话）

- AQL 语法/域/方法链/动作：`https://docs.jfrog.com/artifactory/docs/aql-syntax`（updatedAt 2026-03-22）
- 操作符/通配/日期/相对时间/$msp：`https://docs.jfrog.com/artifactory/docs/aql-search-criteria`
- include/sort/offset/limit/distinct 输出修饰：`https://docs.jfrog.com/artifactory/docs/aql-query-output`
- 域字段全集：`https://docs.jfrog.com/artifactory/docs/aql-entities-fields-reference`
- 执行/鉴权/错误码/流式/权限：`https://docs.jfrog.com/artifactory/docs/aql-query-execution`
- 结果上限/QRL/超时/transitive 配置：`https://docs.jfrog.com/artifactory/docs/aql-performance`
- remote/virtual 仓与 .transitive：`https://docs.jfrog.com/artifactory/docs/aql-repository-queries`
- AQL REST OpenAPI（含 AqlRange/ErrorResponse/搜索上限头注）：`https://docs.jfrog.com/artifactory/reference/searchaql`
- 老搜索端点族 OpenAPI（artifact/gavc/prop/pattern/checksum/usage/dates/creation/license/versions/latestVersion/latestVersionByProperties/badChecksum/dependency/buildArtifacts/archive 各页）：`https://docs.jfrog.com/artifactory/reference/llms.txt` 索引逐页（searchartifact/searchgavc/searchproperty/searchpattern 等，节录于 §8）
- 活体：t226-artifactory OSS 7.84.10 rev 78410900（VM 172.16.58.129，API 8181 `/artifactory/api`；容器恢复沿 T-381 §0——抵达即 Up 保留态，用毕维持 Up；证据 `reports/agents/t407-evidence/` 36 文件）
- 反编译：`reverse-src/artifactory/src/batch1-core/org/artifactory/rest/resource/aql/AqlResource.java`、`rest/resource/search/SearchResource.java`、`aql/model/*`（AqlDomainEnum/AqlComparatorEnum/AqlPhysicalFieldEnum/AqlRelativeDateComparatorEnum/AqlSortTypeEnum/AqlOperatorEnum）、`aql/result/AqlRestResult.java + AqlJsonLocalStreamer.java`、`storage/db/aql/service/AqlServiceImpl.java + AqlQueryValidator.java + decorator/VirtualRepoCriteriaDecorator.java`、`rest/common/exception/mapper/AqlExceptionMapper.java`、`common/ConstantValues.java`（aql* 键）、`search/ArtifactSearcher.java`、`utils/ObfuscationUtils.java`、`rest/resource/system/QueryRateLimiterResource.java`

---

# M16 增量段（FR-148 前置锚，T-435，2026-09-03）

> **定位**：statistics/usage 域字段集（T-440 定案面）+ `/api/search/usage` wire + QRL 全量锚（K72）+ dates/creation 承接（K65）+ UI 搜索族四端点（inv-2 §1.C）。主文档 §0~§13 为 M15 冻结面，本段不改动其条文；凡本段与 §8.2 表述有出入处，在 §14.6「对既有条目的校正」集中登记。
>
> **取证状态（如实登记）**：本段活体腿降级——t226（VM 172.16.58.129）2026-09-03 会话不可达（本机路由经 TUN 代理，SSH kex 即断、API 8181/8182 无响应；`docker start` 恢复链〔T-381 §0〕无法执行）。降级路径按票据 R1 内置条款：官方文档 + 反编译双/单源推进 + 逐条附注，**零静默升格**。既有活体证据（T-407 会话 2026-09-01，同实例同版本）凡覆盖本段条目者直接引用并标注出处文件；本段新增锚（QRL REST 现值、UI 搜索族、crontime、dates 错误臂）未获活体复现的，置信度按单源降档并进 §14.7 待验证清单。

## 14.1 statistics 域字段集（FR-148.1 / T-440 定案面；§2.1 表 statistics 行的展开）

字段全集（官方 aql-entities-fields-reference 字段表 + 反编译 AqlPhysicalFieldEnum statistics 十项逐一对齐，双源一致；活体输出形态 v16）：

| 字段 | 类型 | 域默认输出 | 语义 | 置信度 |
|---|---|---|---|---|
| `downloaded` | Date | **是** | 该 item 最后一次被下载的时间（本实例视角） | 高（官方 + 反编译 + 活体 v16） |
| `downloads` | Int | **是** | 该 item 累计下载次数 | 高（三源同上） |
| `downloaded_by` | String | **是** | 最后下载者用户名；非 admin 调用者按 §6 脱敏规则替换 `unknown` | 高（官方 + 反编译；脱敏现值沿 V-e 待验证） |
| `remote_downloads` | Int | 否（须 include） | **从代理本 local 仓的 smart remote 仓**累计回拉次数 | 高（官方 + 反编译） |
| `remote_downloaded` | Date | 否 | 同上维度的最后一次回拉时间 | 高 |
| `remote_downloaded_by` | String | 否 | 同上维度最后回拉者 | 高 |
| `remote_origin` | String | 否 | smart remote 链上远端 Artifactory 实例地址 | 高 |
| `remote_path` | String | 否 | smart remote 链上的完整路径 | 高 |
| `id` / `remote_id` | Long | 否（内部） | stats 行内部 id，默认结果不含 | 高（反编译；官方无文档——补充官方规范） |

- **域默认输出集** = `{downloaded, downloads, downloaded_by}`（反编译 defaultResultField 旗标三分：仅前三者为域默认；remote_* 全族与 id 非默认）。items 域 include 首次出现 `stat.*` 字段时按 §2.5 规则覆盖该默认集（活体 v16：include 只点 `stat.downloads`/`stat.downloaded` → 嵌套行只含两字段）。
- **输出形态**：items 查询带统计域字段 → 行内嵌套数组 `"stats" : [ {…} ]`（活体 v16 逐字）。
- **零值语义**：官方明示统计域零值用 `null` 查询（`{"stat.downloads":{"$eq":null}}` = 零下载；§2.5 已录）。从未被下载的 item：`downloaded/downloads/downloaded_by` 查询面按 null 处理。
- **remote_* 语义澄清（对 BinFlow 映射关键）**：官方措辞逐字为「downloads **from a smart remote repository proxying the local repository**」——remote_* 族统计的是**下游 smart remote 代理回拉本 local 制品**（上游视角），**不是**本仓 remote 回源下载。BinFlow 无 smart remote 拓扑 → remote_* 全族恒 null/0（C 层零值 stub 登记即可，勿造数据）。**K69 三分口径（直连/经 virtual/remote 缓存命中）全部属于 `downloads` 族的本实例视角计数，与 remote_* 是两个正交维度**——T-438 埋点落列时勿把「remote 缓存命中」灌进 remote_downloads（那是 Artifactory 的 smart remote 语义）。
- **usage 端点的内部形态（补充官方规范，反编译）**：`/api/search/usage` 的命中集等价于一条固定模板的 items 统计域查询（行为等价，不贴实现）：`(downloaded < T 或 downloaded=null) 且 (remote_downloaded < T 或 =null) 且 created < T'`——Artifactory 自身即「usage REST = statistics 域单源」的一鱼两吃先例（T-440 断言对位）。

## 14.2 `GET /api/search/usage` wire（FR-148.1；§8.2 usage 行的展开）

| 项 | 值 | 出处 |
|---|---|---|
| 方法/路径 | `GET /api/search/usage` | 官方 OpenAPI（searchusage，Since 2.2.4） |
| 参数 `notUsedSince` | **必填**，Java epoch 毫秒（int64）——上次下载早于该时刻（含从未下载）的制品入选 | 官方 + 反编译 @QueryParam + 活体 v8m |
| 参数 `createdBefore` | 可选，epoch 毫秒；**缺省时回退用 notUsedSince 值**（官方明示 "if omitted, only artifacts created before notUsedSince"；反编译同） | 官方 + 反编译双源 |
| 参数 `repos` | 可选 CSV，限定 local/缓存仓 | 官方 + 反编译 |
| 鉴权 | privileged non-anonymous（官方）；匿名 → 401（AuthorizationRestException） | 官方 + 反编译 |
| 命中语义 | `(downloaded < notUsedSince OR downloaded IS NULL) AND (remote_downloaded < notUsedSince OR NULL) AND created < createdBefore(缺省=notUsedSince)`；严格小于 | 反编译（高）；官方未写布尔式（补充官方规范） |
| 排序 | `lastDownloaded` 升序，次键 `remoteLastDownloaded` 升序 | 反编译（中——活体未逐行验证排序） |
| 行形态 | `{uri, downloadCount, lastDownloaded, remoteDownloadCount, remoteLastDownloaded}` 五字段；`uri` = 实例 base URL 的 storage API 路径；两日期 = ISO8601 毫秒 Z；`remoteLastDownloaded` 无远端下载时 = `"1970-01-01T00:00:00.000Z"`（epoch-0 格式化，非 null） | **活体 v8m 逐字节**（高） |
| 空/未命中 | **404** `{"errors":[{"status":404,"message":"No results found."}]}` | 活体 v8n + 反编译（高） |
| 缺 notUsedSince 参数 | **404 同上文案**（不走 400——参数缺失与空集同文案的反直觉怪癖） | 反编译（中；官方文档只写 400） |
| Content-Type | `application/json` 或 `application/vnd.org.jfrog.artifactory.search.ArtifactUsageResult+json` | 官方 + 反编译 |
| 结果上限 | 老搜索共通 `search.userQueryLimit` 1000 族（§8.3） | 官方头注 |

**官方文档差异登记（零静默升格）**：官方 OpenAPI 的行 schema 只列 `uri`/`lastDownloaded` 两字段——**活体 + 反编译双源为五字段行**（v8m 逐字节）。本规格按五字段定案，标注「此条补充官方规范」。

**命名口径警示（交 conductor/T-440）**：PRD FR-148.1 与 T-440 AC1 文本写 `usageSince=`，**Artifactory wire 实参名为 `notUsedSince`**（官方 + 反编译 + 活体三源）。BinFlow 对外参数面按 `notUsedSince` 对齐；`usageSince` 不应成为对外参数名（实现票断言书写时注意）。

## 14.3 dates/creation 双端点（K65 承接；§8.2 两行的展开）

共通骨架（两端点同一父类行为）：

| 项 | 值 | 出处/置信度 |
|---|---|---|
| 鉴权 | privileged non-anonymous；匿名 401 | 官方 + 反编译（高） |
| `from` | **必填** epoch 毫秒；缺失 → **400 `'from' parameter cannot be empty!`**（单引号逐字） | 反编译（高——文案逐字；官方未载） |
| `to` | 可选 epoch 毫秒；**区间语义 = `from` 严格大于、`to` 含等号**（`> from AND <= to`） | 反编译（高）；官方未写开闭区间 |
| `to` 缺省 | **官方文档：use now()；反编译：无上界（不拼上界谓词）**——两源冲突，见 §14.7 V-j（差异仅对 created 在未来的 item 可观察；BinFlow 建议按官方 now() 口径实现并留痕） | 冲突如实登记 |
| 排除项 | `maven-metadata.xml` 恒排除（两端口径） | 反编译（中；官方未载——补充官方规范） |
| 未命中 | **404 `No results found.`**（§0-4 族） | 活体 v8l/v8o + 反编译（高） |
| 结果上限 | 专键 `limit.search.results.in.dates.range` 默认 **-1**（关）；未启用时回落 `search.userQueryLimit` 1000（anonymous/非 admin 档，`search.limitAnonymousUsersOnly` 默认 true） | 反编译 + 官方（高） |

**`GET /api/search/creation?from=&to=&repos=`**：

- 固定按**两个日期字段**匹配：`created` **或** `lastModified` 落入区间即命中（OR 语义；反编译——官方文档未提 lastModified 也参与，补充官方规范）。
- 行 = `{uri, created}` 瘦行（uri 同 usage 形态；created = ISO8601 字符串）。**回显日期怪癖（反编译，中）**：命中行回显值优先取 `created`；若 created 不落在请求区间（因 lastModified 命中的行），回显改用 `lastModified` 值放进 `created` 字段——即 `created` 字段可能实际是 lastModified 的时间。
- Content-Type：`application/json` 或 `application/vnd.org.jfrog.artifactory.search.ArtifactCreationResult+json`。

**`GET /api/search/dates?from=&to=&repos=&dateFields=`**：

- `dateFields` 可选 CSV，**合法值四枚**：`created` / `lastModified` / `lastDownloaded` / `remote_last_downloaded`（注意末项 snake_case）；缺省行为未在反编译明确（默认字段集以核验为准——§14.7 V-k）。
- 未知字段名 → **400** `Date field name '<name>' unknown!, possible values are: [...]`（"unknown!, possible" 拼写逐字；枚举回显为四值）。
- 多字段 = OR 组（任一字段落区间即命中）。
- Content-Type：`application/json` 或 `application/vnd.org.jfrog.artifactory.search.ArtifactResult+json`。

## 14.4 QRL 全量锚（K72 归位；§5 表 QRL 行的展开）

**REST wire**（反编译 QueryRateLimiterResource + RateLimiterResource——官方 REST reference 未文档化此端点，整节为补充官方规范；置信度：中〔单源反编译〕，活体现值待验证 §14.7 V-l）：

| 方法/路径 | 行为 | 成功响应 | 错误响应 |
|---|---|---|---|
| `GET /api/v1/system/query_rate_limiter/config` | 读当前限流配置（DB 有则回 DB 值，无则回默认值） | 200 `application/json`：`{"rlSettings":[{"rlType":"DEFAULT","permitsPerTimeFrame":N,"timeFrameMillis":N,"timeQuota":N},{"rlType":"LOW_PRIORITY",…}]}` | 功能关闭 → 400 纯文本 `Query rate limiter is disabled` |
| `POST /api/v1/system/query_rate_limiter/config`（body = 上述 JSON） | 合并写（缺省字段回落既有/默认值；只认 DEFAULT/LOW_PRIORITY 两型） | 200 纯文本 `Query rate limiter configuration was updated successfully` | 同上 400 形态（异常 message 透传） |
| `DELETE /api/v1/system/query_rate_limiter/config` | 删除自定义配置（回默认） | 200 纯文本 `Query rate limiter configuration was deleted successfully` | 同上 |

- **权限**：`admin` 专属（RolesAllowed admin + dashboard 校验；非 admin 403）。注意路径**不带** `/artifactory/api` 之外的 search 前缀——属 v1 system 面（T-452 挂 v1 面）。
- **三态语义**（K72 核心条目；反编译 + 官方 system.properties 文档可互证默认值）：

| 态 | 触发 | 行为 | 置信度 |
|---|---|---|---|
| disabled | `artifactory.query.rate.limiter.enabled=false`（**出厂默认**） | 整个限流器旁路；REST 三操作一律 400 `Query rate limiter is disabled` | 高（反编译默认值 + 官方 properties 文档） |
| enabled | enabled=true 且 simulation=false | 超限查询**阻塞等待**（许可桶 + 时间配额双桶）；DB 查询面生效（非仅 AQL 面） | 高（反编译） |
| simulation | `artifactory.query.rate.limiter.simulation.mode.enabled=true` | 超限**不阻塞不拒绝**，只记录「本应被限流」的时长指标——观测模式 | 高（反编译；官方 properties 文档列该键） |

- **配置模型**：两型 `DEFAULT`（常规查询）与 `LOW_PRIORITY`（低优先请求面，按请求路径正则归类）+ 内部 `SYSTEM`（限流器自身的 DB 操作，恒旁路不可配）。**HA 语义：permits 按 running server 数均分**（BinFlow 单节点 = 不除）。
- **出厂默认值**（system.properties 键，反编译 ConstantValues）：`query.rate.limiter.default.permitsPerTimeFrame=2147483647`（即不限）、`.timeFrameMillis=1000`、`.timeQuota=2147483647`；low.priority 同族同值。
- **指标 job**：重复任务每 `query.rate.limiter.metrics.interval.secs`（默认 **60s**）采样一次；仅 enabled 态调度。指标面：Prometheus provider `jfrt_qrl`（CONTINUOUS，刷新 60s，disabled 时输出空）；限流发生时 INFO 日志文案族逐字 `Artifactory database queries have reached the set limit ({} per {} ms). Throttling has been applied ({}% of the time) to protect system health.`（low priority 变体同构）。指标字段族：totalQueries / totalPermits / slowedDownMillis(%) / slowedDownByTimeMillis(%) / chargedQueryTime（每采样窗归零）。
- **与 AQL 429 的边界（勿混淆，承 §5）**：AQL REST 的 429 `too many requests` 来自 **AQL 并发上限**（`artifactory.aql.queries.limit`，默认 3）——那是查询并发闸，不是 QRL；QRL 管的是底层 **DB 查询速率**（限流形态是**延迟放行**而非 429 拒绝，simulation 态更只记不断）。两机制正交。
- **BinFlow 映射注记（交 T-452/K72，非本规格裁定）**：K63 定案门（1000/4/10s → 429/408 形态）维持不动；QRL 面按本锚建 admin REST 三操作 + 三态 + 指标采样；默认值是否沿用 Artifactory 出厂（=不限流）vs 映射 K63 值，属 T-452 票内定案，本锚只钉 Artifactory 行为。

## 14.5 UI 搜索族四端点（inv-2 §1.C；FR-148.3 / T-452 输入）

四资源全部挂在 Artifactory **UI REST 树**（`/ui/api/…` 面——供自带前端消费；与 `/api` 公共 REST 面不同源）。**挂载前缀精确拼法未获活体确认（§14.7 V-m）**：反编译资源级注解为下表「注册子路径」，四资源无 `v1/…` 前缀注解（对照 auth-integration.md 已实录的 `v1/admin/…` 族），推测全路径 = `/artifactory/ui/api/<注册子路径>`；置信度中，活体对拍后钉死。角色面：四资源均 `@RolesAllowed({admin, user})`。

| 端点 | 操作清单（注册子路径下） | 行为要点 | 置信度 |
|---|---|---|---|
| `artifactsearch` | `POST quick` / `POST gavc` / `POST pkg{type}` / `GET pkg{type}`（选项集）/ `POST pkg/tonative`（UI 检索条件 → AQL 转换）/ `POST checksum` / `POST trash` / `POST deleteArtifact` | 控制台搜索页数据面：quick 快搜、pkg 族按包型检索（请求体 = 检索条件模型数组）；`deleteArtifact` 为**删除动词**（BinFlow 只读面不承载则缺位登记） | 高（反编译操作面）/ 中（wire 模型字段） |
| `stashResults` | `POST`（存结果集）/ `GET`（取）/ `DELETE`（清）/ `POST subtract` / `POST intersect` / `POST add` / `POST export` / `POST copy` / `POST move` / `POST discard` —— 共 10 操作 | 搜索结果暂存与集合运算（交/差/并）+ 批量 copy/move/discard；**总开关 `artifactory.ui.search.stashResults.endpoint.enabled` 出厂 false** → 关闭时全操作 404 `Stash search results endpoint is disabled`（OSS 档默认即关） | 高（反编译含开关默认值与文案） |
| `packagesSearch` | `POST leadFile` / `POST artifacts`（batch3-addons） | 按包型+repo+包名+版本取主制品/制品清单；包不存在 404 空体 | 高（反编译） |
| `syntax-search` | `POST` | AQL 语法搜索（树浏览新搜索页数据面）；开关 `artifactory.treebrowser.newpagewithsearch.enabled` → 关闭 400 `Syntax Search is disabled`；语法错 400 ErrorResponse envelope | 高（反编译） |

- **BinFlow 取向注记（非裁定）**：此族是 UI 内部面；T-452 AC「四端点 curl wire 断言」以本表操作面 + 上表路径为锚。Smart Searches 保存面 pro 档不做的 PRD 口径与本表 `stashResults` 出厂 false 相互印证（Artifactory OSS 档该族默认关闭）。
- `searchResults`（`GET`，同目录第五资源）：暂存结果视图数据面，inv-2 未列——登记为外延项，T-452 不承载则缺位登记。

## 14.6 对既有条目的校正（本段 vs §8.2；零静默升格）

1. **§8.2 usage 行「行 = {uri, downloadCount, lastDownloaded, remoteDownloadCount,…}」**：经 v8m 全量核对定案为**恰五字段**（…+ `remoteLastDownloaded`，无省略号余项）——§14.2。
2. **§8.2 creation 行「to 缺省 now()」**：此说法源自官方文档；反编译为「to 缺省 = 无上界」。冲突登记 V-j，差异仅对 created 在未来的 item 可观察。§8.2 原文维持（官方口径），BinFlow 实现票按 V-j 裁定。
3. **§8.2 dates 行「dateFields 决定回显日期字段」**：定案精确化——`dateFields` 是**匹配条件字段集**（OR 组），非回显投影；合法值四枚含 `remote_last_downloaded`（§14.3）。
4. **§5 表 QRL 行「三态 enabled/disabled/simulation + 指标 job」**：展开定案于 §14.4——三态触发键、指标 job 60s、REST 三操作、HA 均分语义。

## 14.7 增量段待验证清单（低置信/未复现汇总——零静默升格）

| # | 项 | 现值依据 | 验证途径 |
|---|---|---|---|
| V-i | usage 端点排序键（lastDownloaded 升序 + remoteLastDownloaded 次键） | 反编译单源 | t226 恢复后多制品多时戳语料对拍（语料仓已有下载记录） |
| V-j | creation/dates 的 `to` 缺省语义（官方 now() vs 反编译无上界） | 双源冲突 | 需 created 在未来的 item（只读纪律下不可造——可候 BinFlow e2e 自证，或以官方 now() 口径实现后登记） |
| V-k | dates 端点 `dateFields` 缺省默认集 | 反编译未见显式默认 | t226 恢复后 `GET /api/search/dates?from=…`（无 dateFields）观测回显行为 |
| V-l | QRL REST 三操作在 7.84.10 的现值（disabled 400 文案 / GET 默认体） | 反编译单源（官方无 REST 文档） | t226 恢复后 admin GET `$BASE/api/v1/system/query_rate_limiter/config`（只读） |
| V-m | UI 搜索族挂载前缀精确拼法（`/ui/api/artifactsearch/quick` 推测值）与 stash 开关现值 | 反编译注解 + auth-integration.md 先例类推（中） | t226 恢复后 GET `$BASE/ui/api/artifactsearch/pkg`（只读选项端点）+ stashResults 404 臂 |
| V-n | statistics 域 `downloaded_by` 非 admin 脱敏现值 | 反编译 ObfuscationUtils（默认脱敏）；沿用 V-e | 同 V-e |
| V-o | creation 端点 created-回显-fallback（lastModified 命中行回显 lastModified 值） | 反编译单源（怪癖级） | t226 恢复后构造 matched-by-modified 行观测（只读可测：对既有语料以窄 from 区间探测） |

## 14.8 增量段取证锚点（2026-09-03 会话）

- 官方：aql-entities-fields-reference（statistics 字段表）、reference/searchusage、reference/searchcreation（from/to 参数与 now() 声明）、Quartz CronTrigger tutorial 与 maintenance 文档（cron 锚另落 cron-scheduling.md）；QRL REST 官方 reference **无页**（llms.txt 索引核对）——§14.4 整节反编译单源。
- 反编译：`aql/model/AqlPhysicalFieldEnum.java`（statistics 十字段 + defaultResultField 旗标）、`rest/resource/search/types/{UsageSinceResource,CreatedInRangeResource,AnyDateInRangeResource,GenericSearchResource}.java`、`api/rest/search/{result/LastDownloadRestResult,common/RestDateFieldName}.java`、`search/{SearchServiceImpl,stats/LastDownloadedItemsSearcherUtils}.java`、`rest/resource/system/{QueryRateLimiterResource,RateLimiterResource}.java`、`throttling/qrl/**`（type/{Enabled,Simulation,Disabled}QueryRateLimiter + QueryRateLimiterFactory + service/QueryRateLimiterServiceImpl + metrics/{QueryRateLimiterMetricsJob,QueryRateLimiterMetricProvider}）、`throtlling/{common/model/RateLimiterConfig,qrl/model/QueryRateLimiterMetrics,qrl/enums/QueryRateLimiterType}`、`common/ConstantValues.java`（qrl* / search* / limit.search.results.in.dates.range 键）、`ui/rest/resource/artifacts/search/{ArtifactSearch,StashSearchResults,SyntaxSearch,SearchResults}Resource.java`、`addon/search/packages/rest/PackagesSearchResource.java`
- 既有活体证据（T-407 会话 2026-09-01 引用）：`reports/agents/t407-evidence/v16-stats-fields.txt`（stats 嵌套输出）、`v8m-usage-epoch-hit.txt`（五字段行逐字节）、`v8n-usage-empty-404.txt`、`v8k-creation-epoch-hit.txt`、`v8l-creation-empty-404.txt`、`v8o-dates-empty-404.txt`
- t226 状态（2026-09-03）：不可达（TUN 路由断），零接触零残留（未建立任何会话）——保留态无从扰动；恢复链待环境修复后按 T-381 §0 执行。
