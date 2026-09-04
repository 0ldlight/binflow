---
title: AQL 搜索指南（Artifactory Query Language 子集）
sidebar_position: 61
---

# AQL 搜索指南（Artifactory Query Language 子集）

> 适用版本：M15（语言内核 T-409/T-411、引擎 T-413、REST 端点 T-415）。行为逐项核对 `internal/search`（parser/fields/engine）与 `internal/httpapi/search_aql.go`；本文 curl 命令于 HEAD 构建的双 scratch 实例实测（2026-09-02），输出摘录原样；statistics 域与 usage 端点两节于当前 HEAD 构建的 scratch 实例（127.0.0.1:18095，admin 凭据）实测（2026-09-04），输出原样摘录。
> BinFlow 实现 AQL 的 **items 域只读查询子集**——子集边界与 Artifactory 的差异逐条见文末[迁移对照表](#从-artifactory-aql-迁移对照表)；老搜索端点（gavc/prop/pattern）见 [API 参考 · SR 搜索域](api-reference.md#sr-搜索域)。

AQL 是 Artifactory 的制品查询语言：一段查询文本描述「查什么、输出哪些字段、怎么排序翻页」，服务端返回流式 JSON。BinFlow 以 `items.find(...)` 为唯一入口，覆盖日常的「按仓库/路径/属性/checksum/时间窗找制品」场景。

## 前置条件

- 一个运行中的 BinFlow 实例（任何部署形态均可；本文示例 `BASE=http://127.0.0.1:18501`）。
- **AQL 不允许匿名**：闭环实例（`anonymous_access: false`）匿名调用 → 401；开匿名实例匿名调用 → 403。用 Basic 或 Token 凭据（见 [API 参考 · 认证](api-reference.md#三种认证方式)）。

```bash
export BASE=http://127.0.0.1:18501
export AU='admin:<你的管理员口令>'
```

## 请求形态

| 项 | 值 |
|---|---|
| 方法/路径 | `POST /binflow/api/search/aql`（**POST 专用**——GET 404） |
| 请求体 | `text/plain`，body = AQL 查询文本（**非 JSON 包裹**，不校验 Content-Type） |
| 查询参数 | `?compact=true`（行体与 range 压成单行）；body 为空时回退读 `?query=<urlencoded AQL>` |
| 成功 | 200 `application/json`，`{results, range}` |
| 错误 | 400 / 401 / 403 / 408 / 429 / 500，`{"errors":[...]}` 信封 |

五个可复制场景（实测输出摘录）：

```bash
# 1) 单仓全列——默认输出 repo/path/name/type/size/created/created_by/modified/updated
curl -su $AU -X POST $BASE/binflow/api/search/aql \
  --data-binary 'items.find({"repo":"maven-local"})'
# 200
# {
# "results" : [ {
#   "repo" : "maven-local",
#   "path" : "com/acme/demo/1.0.0",
#   "name" : "demo-1.0.0-sources.jar",
#   "type" : "file",
#   "size" : 6,
#   "created" : "2026-09-01T20:06:19.000Z",
#   "created_by" : "admin",
#   "modified" : "2026-09-01T20:06:19.000Z",
#   "updated" : "2026-09-01T20:06:19.000Z"
# }, ... ],
# "range" : { "start_pos" : 0, "end_pos" : 5, "total" : 5 }
# }

# 2) 投影 + 排序 + 分页（include→sort→offset→limit 链序固定，见下文）
curl -su $AU -X POST $BASE/binflow/api/search/aql \
  --data-binary 'items.find({"repo":"maven-local","type":"file"})
                 .include("repo","path","name","size","modified")
                 .sort({"$desc":["path"]}).offset(1).limit(2)'

# 3) 属性条件（@key 短形；属性写入见「属性系统」）
curl -su $AU -X POST $BASE/binflow/api/search/aql \
  --data-binary 'items.find({"@stage":"prod"}).include("repo","path","name","@stage","@team")'
# 行内属性聚合为嵌套成员：
#   "properties" : [ { "key" : "stage", "value" : "prod" },
#                    { "key" : "team",  "value" : "core" } ]

# 4) 通配与相对时间
curl -su $AU -X POST $BASE/binflow/api/search/aql \
  --data-binary 'items.find({"name":{"$match":"demo-1.0.0*"}}).include("repo","name")'
curl -su $AU -X POST $BASE/binflow/api/search/aql \
  --data-binary 'items.find({"repo":"team-local","created":{"$last":"1 day"}})
                 .include("name","created")'

# 5) 紧凑输出与 query 参数回退
curl -su $AU -X POST "$BASE/binflow/api/search/aql?compact=true" \
  --data-binary 'items.find({"repo":"team-local"}).include("repo","name","size")'
# {"results" : [ {"repo":"team-local","name":"build-draft.bin","size":6},... ],
#  "range" : {"start_pos":0,"end_pos":2,"total":2}}（外层包裹形态不变）
curl -su $AU -X POST "$BASE/binflow/api/search/aql?query=items.find(%7B%22repo%22%3A%22team-local%22%7D)" --data-binary ''
```

## 语言子集

### 域：只有 items

查询入口域只有一个：`items`。其余 Artifactory 域一律 **400 点名拒绝**（envelope 里的 message 直接说该域为什么不行）：

```bash
curl -su $AU -X POST $BASE/binflow/api/search/aql --data-binary 'builds.find({})'
# 400 {"errors":[{"status":400,
#   "message":"AQL domain not supported: builds (BinFlow AQL supports: items; build-info domains are not implemented)"}]}

curl -su $AU -X POST $BASE/binflow/api/search/aql --data-binary 'properties.find({})'
# 400 ... "AQL domain not supported: properties (BinFlow AQL supports: items;
#        query properties through items.find with {\"@key\": value} criteria)"
```

| 域（Artifactory） | BinFlow | 拒绝文案提示 |
|---|---|---|
| `items` | **支持**（唯一入口；`stat.*` 统计字段嵌于 items 查询，见[statistics 域字段](#statistics-域下载统计字段)） | — |
| `properties` | 400 | 属性走 items 的 `@key` 条件 |
| `statistics` | 400（入口域） | 统计走 items 的 `{"stat.<field>": value}` 条件（实测文案逐字给出该提示） |
| `builds` / `modules` / `dependencies` / `artifacts` / `build.properties` / `build.promotions` | 400 | build-info 域未实现 |
| `releases` / `release_artifacts` | 400 | release-bundle 域未实现 |
| `item.infos` | 400 | 内部域不暴露 |

### 字段

items 域条件与输出字段（`include` 可用即条件可用）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `repo` / `path` / `name` | string | 仓 key / 父目录 / 条目名（`$match` 通配可用） |
| `type` | enum | `file` / `folder` / `any`；**查询不含 type 条件时默认只搜 file**（folder 须显式 `{"type":"folder"}`） |
| `created` / `modified` / `updated` | date | ISO8601 或相对时间（`modified` 与 `updated` 同源于 nodes.updated_at——BinFlow 无独立文件系统时间戳，两字段同值） |
| `created_by` | string | 上传者（非 admin 调用者脱敏为 `unknown`，见[权限](#权限与脱敏)） |
| `size` / `depth` | long / int | 字节 / 路径深度 |
| `sha256` / `actual_sha1` / `actual_md5` | string | checksum（BinFlow 单值存储，无 original/actual 分离） |
| `virtual_repos` | — | **仅 include**：该制品被哪些 virtual 仓包含（数组输出；不能作条件或排序键） |
| `@<key>` / `property.key` / `property.value` | string | 属性域字段，嵌于 items 查询（`{"@stage":"prod"}` 等价 `{"property.key":"stage","property.value":"prod"}` 的短形；`{"@*":"v"}` 任意键、`{"@k":"*"}` 键存在性） |
| `stat.downloads` / `stat.downloaded` / `stat.downloaded_by` | int / date / string | **统计域字段**，嵌于 items 查询——条件 / include / sort 全能力（见[下文专节](#statistics-域下载统计字段)） |

**已登记但不支持**（引用即 400，message 点名字段与原因——宁可拒绝也不伪空集）：

| 字段 | 400 message（实测） |
|---|---|
| `modified_by` | `AQL field not supported yet: modified_by (no storage source in BinFlow yet)` |
| `original_sha1` / `original_md5` | `... BinFlow does not store separate original checksums` |
| `stat.id` / `stat.remote_id` | `... (internal field, not exposed)`（实测） |
| `id` / `repo_path_checksum` | 内部字段不暴露 |

### 操作符

| 类别 | 操作符 | 备注 |
|---|---|---|
| 比较 | `$eq` `$ne` `$gt` `$gte` `$lt` `$lte` | string/date/int/long 通用（`$gt` 数字比较实测可用） |
| 通配 | `$match` `$nmatch` | 仅 string；`*` 任意串、`?` 单字符——**只在这两个操作符内生效，其它位置按字面** |
| 逻辑 | `$and` `$or`，逗号分隔 = 隐式 $and | 任意深度嵌套 |
| 单属性 | `$msp` | 要求**同一个属性实例**满足全部条件——`{"$msp":[{"property.key":"stage"},{"property.value":"dev"}]}` 命中 key=stage 且 value=dev 的那个属性；两个不同 `@key` 条件放进 `$msp` 永远空集（语义正确，不是 bug） |
| 相对时间 | `$last` `$before`（仅 date 字段） | 见下——**数值与单位之间必须有空格** |

相对时间语法：`"<count> <unit>"`，单位词表 `ms/millis/millisecond(s)`、`s/second(s)`、`minute(s)`、`d/day(s)`、`w/week(s)`、`mo/month(s)`、`y/year(s)`。**count 与 unit 之间至少一个空格**（`"1 day"`、`"1 d"` 可用；`"1d"` → 400 `Invalid relative date format for: 1d`）——与 Artifactory 官方示例的粘连短后缀不同，迁移脚本注意改写（对照表见文末）。

**不收录**（400 点名拒绝）：`$not`（AQL 语言本身无此操作符，用 `$ne`/`$nmatch`）、`$contains`（非 AQL 操作符，用 `$match`）、`$eqic`/`$eqvic`/`$matchic`（反编译才有的忽略大小写变体，官方未文档化）。

```bash
curl -su $AU -X POST $BASE/binflow/api/search/aql --data-binary 'items.find({"$not":{"repo":"team-local"}})'
# 400 ... "AQL operator not supported: $not ($not is not part of the AQL language; use $ne or $nmatch)"
```

### 尾缀方法链

| 尾缀 | 语义 |
|---|---|
| `.include(<fields>)` | 投影字段集，`"*"` 全字段；首次列出某域字段即覆盖该域默认输出集 |
| `.sort({"$asc\|"$desc":["<field>",...]})` | 排序字段**必须在输出字段集内**（否则 400 `Only the result fields are allowed to use in the sort section.`）；字段不可重复 |
| `.offset(<n>)` | 跳过 n 行（range.start_pos 回显） |
| `.limit(<n>)` | 上限 n 行（仅显式声明时 range 才有 `limit` 键） |

**链序固定**：`include → sort → offset → limit`。乱序（如 `.limit(1).sort(...)`）→ 400 语法错，文案逐字对齐 Artifactory：

```bash
curl -su $AU -X POST $BASE/binflow/api/search/aql \
  --data-binary 'items.find({"repo":"team-local"}).limit(1).sort({"$asc":["name"]})'
# 400 ... "Failed to parse query: items.find({\"repo\":\"team-local\"}).limit(1).sort({\"$asc\":[\"name\"]}),
#         it looks like there is syntax error near the following sub-query: sort({\"$asc\":[\"name\"]})"
```

不收录的动词（引用即 400 点名）：`.distinct()`、`.transitive`（Smart Remote 追源）、`.delete()`/`.update()`/`.dryRun`（**AQL 写动作不做，BinFlow AQL 只读**）。

## virtual 仓语义

virtual key 是**合法查询值**，不是查询实体：`items.find({"repo":"maven-virtual"})` 在编译期展开为成员仓的 OR 组。结果行 `repo` 字段 = **实际存储仓 key**（成员 local），并且**隐式附加 `virtual_repos` 输出字段**列出包含该制品的 virtual 仓：

```bash
curl -su $AU -X POST $BASE/binflow/api/search/aql \
  --data-binary 'items.find({"repo":"maven-virtual"}).include("repo","path","name")'
# 200 —— 行来自两个成员仓，virtual_repos 隐式附加：
# { "repo" : "maven-local",  "path" : "com/acme/demo/1.0.0", "name" : "demo-1.0.0.jar",
#   "virtual_repos" : [ "maven-virtual" ] },
# { "repo" : "maven2-local", "path" : "com/other/lib/2.0",   "name" : "lib-2.0.jar",
#   "virtual_repos" : [ "maven-virtual" ] }, ...
```

不存在的 repo key（含拼错的 virtual 名）→ **200 空集**（无存在性校验）。

## statistics 域（下载统计字段）

`stat.*` 字段族嵌于 `items.find` 使用（`statistics.find(...)` 入口域仍是 400——文案会指路 `{"stat.<field>": value}`）。三个实数据字段，**条件 / include / sort 全能力**：

| 字段 | 类型 | 数据源 |
|---|---|---|
| `stat.downloads` | int | 累计下载计数（整数比较） |
| `stat.downloaded` | date | 最后下载时间（date 归一比较） |
| `stat.downloaded_by` | string | 最后下载者（**非 admin 调用者脱敏为 `unknown`**，与 `created_by` 同规则） |

**恒零 stub 五字段**：`stat.remote_downloaded` / `stat.remote_downloads` / `stat.remote_downloaded_by` / `stat.remote_origin` / `stat.remote_path`——smart remote 回拉维度，BinFlow 无该拓扑，**可查可投影但恒 null/0**（不可 sort——常量无序）。注意与 [`?stats` 面](api-reference.md) 的 `remoteDownloadCount` 同名不同义：后者是 BinFlow 自有的 remote 服务点指标，两者并存。

**null 字面量**：statistics 域字段接受 `null` 值条件，**仅 `$eq` / `$ne`**（顺序比较符配 null 无注册语义，拒绝并点名）——`{"stat.downloaded":{"$eq":null}}` 即「从未下载」。

```bash
# 投影：include 点名 stat 字段 → 行内渲染嵌套 "stats" 块（恰好被点名的字段、echo 序）
curl -su $AU -X POST $BASE/binflow/api/search/aql \
  --data-binary 'items.find({"repo":"generic-local","type":"file"})
                 .include("repo","path","name","stat.downloads","stat.downloaded","stat.downloaded_by")'
# 200 —— 从未下载的行 downloaded/downloaded_by 渲染 null（实测原样）：
# {
# "results" : [ {
#   "repo" : "generic-local",
#   "path" : "acme",
#   "name" : "app.bin",
#   "stats" : [ {
#     "downloads" : 3,
#     "downloaded" : "2026-09-04T09:51:36.000Z",
#     "downloaded_by" : "admin"
#   } ]
# },{
#   "repo" : "generic-local",
#   "path" : "acme",
#   "name" : "legacy-installer.bin",
#   "stats" : [ {
#     "downloads" : 0,
#     "downloaded" : null,
#     "downloaded_by" : null
#   } ]
# } ],
# "range" : { "start_pos" : 0, "end_pos" : 2, "total" : 2 }
# }

# 条件 + 排序（下载数降序）
curl -su $AU -X POST $BASE/binflow/api/search/aql \
  --data-binary 'items.find({"repo":"generic-local","stat.downloads":{"$gte":3}}).include("name","stat.downloads")'
curl -su $AU -X POST $BASE/binflow/api/search/aql \
  --data-binary 'items.find({"repo":"generic-local"}).include("name","stat.downloads")
                 .sort({"$desc":["stat.downloads"]})'

# null 字面量：从未下载的制品（实测原样）
curl -su $AU -X POST $BASE/binflow/api/search/aql \
  --data-binary 'items.find({"repo":"generic-local","stat.downloaded":{"$eq":null}}).include("name","stat.downloaded")'
# {
# "results" : [ {
#   "name" : "legacy-installer.bin",
#   "stats" : [ {
#     "downloaded" : null
#   } ]
# } ],
# "range" : { "start_pos" : 0, "end_pos" : 1, "total" : 1 }
# }
```

**计数口径**（与 `?stats` 面单源——同一 nodes 计数列，无第二通道）：内容面 GET 递增；存储 API 的节点读取（控制台打开详情、`?properties` 读写）同样递增；`?stats` 探针自身不计。把 `downloads` 读作「被访问次数」更安全——人工浏览也会增计数。

## usage 端点：`GET /api/search/usage`

「找闲置制品」的专用面——statistics 域单源的 REST 便捷端点（内部走同一引擎模板，同 ACL / 同并发门 / 同 1,000 行截断）。典型用途：清理策略与保留策略的数据面。

| 项 | 值 |
|---|---|
| 方法/路径 | `GET /binflow/api/search/usage`（**GET 专用**——POST 404） |
| `notUsedSince` | **必填**，epoch **毫秒**——最后下载严格早于它（从未下载 = 恒命中） |
| `createdBefore` | 可选 epoch 毫秒；**缺省回退 notUsedSince 的值**——即默认只统计「切点之前就已存在」的制品（新上传未下载的制品不算闲置，见下例） |
| `repos` | 可选 CSV 仓库收窄 |
| 命中语义 | `(downloaded < T OR 从未下载) AND created < createdBefore`，**严格小于** |
| 行形态 | 恰五字段 `{uri, downloadCount, lastDownloaded, remoteDownloadCount, remoteLastDownloaded}`；`uri` = storage API 路径；结果按 `lastDownloaded` 升序 |
| 空集 / 缺参 | **404 `No results found.`**（缺参同空集 404——兼容怪癖，非 400） |
| 非数字 / 负 epoch | 400 `Usage search requires a non-negative 'notUsedSince' epoch-milliseconds value.` |
| 匿名 | 401 challenge（AQL 同款） |
| 截断 | 超 1,000 行置 `X-Binflow-Search-Truncated: true` |

```bash
# 「90 天未下载」：切点 = 当前时间减 90 天，转 epoch 毫秒
CUTOFF=$(python3 -c "import time; print(int((time.time()-90*86400)*1000))")
curl -su $AU "$BASE/binflow/api/search/usage?notUsedSince=$CUTOFF"
# 200 —— 只回 legacy-installer.bin（120 天前上传、从未下载）。
# 当天上传的 app.bin 不命中：它既有下载记录，且 created 晚于切点——
# createdBefore 缺省 = notUsedSince，当天上传的制品即便零下载也不判闲置：
# {
#   "results" : [ {
#     "uri" : "http://127.0.0.1:18095/binflow/api/storage/generic-local/acme/legacy-installer.bin",
#     "downloadCount" : 0,
#     "lastDownloaded" : "1970-01-01T00:00:00.000Z",
#     "remoteDownloadCount" : 0,
#     "remoteLastDownloaded" : "1970-01-01T00:00:00.000Z"
#   } ]
# }

# createdBefore 显式放宽：把「早于切点创建」改为「早于现在」——新上传但零下载的制品也计入
NOW=$(python3 -c "import time; print(int(time.time()*1000))")
curl -su $AU "$BASE/binflow/api/search/usage?notUsedSince=$CUTOFF&createdBefore=$NOW"

# 未命中 / 缺参 → 404（不是 200 空数组）
curl -su $AU "$BASE/binflow/api/search/usage" -w '%{http_code}\n' -o /dev/null    # 404
```

三个如实呈现的怪癖：

- **never 行的 `lastDownloaded` 是 epoch-0 字面**（`1970-01-01T00:00:00.000Z`）而非 null——`remoteLastDownloaded` 对所有行恒为该值（无 smart remote 拓扑，恒零维度不造数据）。
- **usage 面的 `remoteDownloadCount` 恒 0**，即使 `?stats` 面显示非零（同名不同义，见上节）。
- **缺参 404**——自动化脚本请把 404 同时当「无闲置」与「参数没带上」两种情况排查。

## 响应 envelope 与截断

- 流式形态：前导 `\n{\n"results" : [ `，行间 `},{`，空集 `"results" : [  ]`（两空格）；日期回显 ISO8601 毫秒 UTC。`?compact=true` 只压行体与 range，外层包裹不变。
- `range` 对象：`start_pos`（= offset 回显）、`end_pos` / `total`（**本页行数**——流式约定，total 不是全量计数，翻页判断用「本页满窗或 notification 在场」）、`limit`（仅声明时）、`notification`（截断通告）。
- **行数上限 1,000**：超限返回前 1,000 行，同时置响应头 `X-Binflow-Search-Truncated: true` 与 `range.notification = "AQL query reached the search hard limit, results are trimmed."`（文案与 Artifactory 逐字一致）。用 `.offset()` 续翻可达全量。
  - 注意：**用户自己的 `.limit(n)` 小于原始命中数时同样置截断标记**（诚实上界——「窗口没吃完全集」就告诉你还有余量），此时 notification 语义是「仍有未取行」，不区分是用户 limit 还是硬上限。
- 查询文本上限 **6,000 字符**（与 Artifactory 官方默认一致）：超限 400 `AQL query is too long; please reduce the query length to less than 6000 chars`。

## 权限与脱敏

- **行级过滤**：结果按调用者 read 权限过滤——不限 repo 的查询也只回可读行（admin 全见）。
- **身份脱敏**：非 admin 调用者看到的 `created_by` / `stat.downloaded_by` 等身份字段值为字面量 `unknown`（admin 调用回显真名）。
- **匿名两臂**：闭环实例匿名 → 401 `Authentication is required`（带 Basic challenge）；开匿名实例匿名 → 403 `Only non-anonymous users are allowed to access AQL queries`。

## 资源门与限流

| 面 | 行为 |
|---|---|
| 并发 | 同实例并发执行上限 4：超限 **429** + `Retry-After: 1`（body `too many requests`） |
| 执行超时 | **10s**：超时 **408**（非 503/504），message 带引擎超时原因 |
| 慢查询 | >5s 服务端 WARN 单行日志（查询摘要 + 耗时） |
| 指标 | `binflow_search_queries_total{plane="aql"}`、`binflow_search_query_duration_seconds`、`binflow_search_rejections_total{reason=…}`（reason = `concurrency` / `timeout`；见 [Prometheus 指标参考](metrics/prometheus-reference.md)） |

## 从 Artifactory AQL 迁移对照表

术语与语法主体不变（`items.find()`、操作符、尾缀链、envelope 形态），差异集中在**域子集**与少量行为面：

| Artifactory 行为 | BinFlow 现状（M15） | 迁移动作 |
|---|---|---|
| `builds.find()` / `modules` / `dependencies` / `releases` 等 build 系域 | 400 点名拒绝（build-info 未实现） | build 系查询改走外部 CI 记录；或等 build-info 域立项 |
| `stat.downloads` 统计字段 | **支持**（条件/include/sort；`stat.id` 仍 400） | 无——`stat.*` 族查询可直接迁入；`statistics.find(...)` 入口域写法须改为 items 嵌字段 |
| `stat.remote_downloads` 等 smart remote 统计 | 可查可投影但**恒 0/null**（无 smart remote 拓扑，不造数据；不可 sort） | 脚本里依赖 remote 维度的分支删掉 |
| usage 端点 `GET /api/search/usage` | 支持（`notUsedSince`/`createdBefore`/`repos`；空集 404 `No results found.`） | Artifactory 脚本可直接迁移；注意 never 行 lastDownloaded 为 epoch-0 字面 |
| `items.find(...).include("modified_by")` | 400（无存储源） | 脚本删掉该字段 |
| `original_sha1` / `original_md5` 双值 checksum | 400（单值存储） | 改用 `sha256` / `actual_sha1` / `actual_md5` |
| `items.delete()` / `properties.update()` AQL 写动作 | 400（AQL 只读） | 删除走 `DELETE /binflow/{repo}/{path}`；属性走 `?properties` 三动词（[属性系统](properties.md)） |
| `.transitive` Smart Remote 追源 | 400（未实现） | remote 仓缓存面直接查（`{"repo":"<remote>"}` 查已缓存行） |
| `.sort()` 在 OSS 档被许可门挡 | **无许可门，sort 可用**（对齐官方全集） | 从 OSS 迁来的查询含 sort 反而能跑了 |
| 相对时间 `"1d"` 粘连短后缀 | 400——**count 与 unit 必须空格分隔**（`"1 d"`） | 迁移脚本正则改写：`"(\d+)(d|w|mo|y|s)"` → `"$1 $2"` |
| self-managed 无默认行数上限 | 硬上限 **1,000** + 截断标记 + offset 翻页 | 大结果集查询加 `.limit()` 分页循环 |
| 匿名可查（实例开匿名时部分面） | **AQL 永不允许匿名**（401/403） | 脚本配凭据或 Token |
| 查询长度 6,000 字符 | 同为 6,000 | 无 |
| envelope / range / 错误信封 / E1 语法错文案 | 逐字一致（t407 活体证据复刻） | 无 |

## 验证

```bash
# 端到端三连：建数据 → 查 → 断言行数
curl -su $AU -X PUT $BASE/binflow/team-local/qa/probe.bin --data-binary 'probe' -o /dev/null -w '%{http_code}\n'   # 201
curl -su $AU -X POST $BASE/binflow/api/search/aql --data-binary 'items.find({"repo":"team-local"})' | grep -c '"name"'   # ≥1
curl -su $AU -X POST $BASE/binflow/api/search/aql --data-binary 'items.find({"repo":"no-such"})' | grep '"total" : 0'     # 空集 200
```

## 下一步

- 老搜索端点（gavc / prop / pattern / artifact / checksum）：[API 参考 · SR 搜索域](api-reference.md#sr-搜索域)；控制台搜索页的 AQL 模式见[控制台指南](console.md#搜索与仪表盘)
- 属性的写入与其它读取入口：[属性系统用法](properties.md)
- 从 Artifactory 整体迁移：[bf-migrate 指南](guides/migrate-artifactory.md)与 [FAQ 迁移对照表](faq.md#从-artifactory-迁移对照表)
