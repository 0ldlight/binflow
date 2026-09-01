---
title: AQL 搜索指南（Artifactory Query Language 子集）
sidebar_position: 61
---

# AQL 搜索指南（Artifactory Query Language 子集）

> 适用版本：M15（语言内核 T-409/T-411、引擎 T-413、REST 端点 T-415）。行为逐项核对 `internal/search`（parser/fields/engine）与 `internal/httpapi/search_aql.go`；本文 curl 命令于 HEAD 构建的双 scratch 实例实测（2026-09-02），输出摘录原样。
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
| `items` | **支持**（唯一入口） | — |
| `properties` / `statistics` | 400 | 属性走 items 的 `@key` 条件；统计未存储 |
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

**已登记但不支持**（引用即 400，message 点名字段与原因——宁可拒绝也不伪空集）：

| 字段 | 400 message（实测） |
|---|---|
| `modified_by` | `AQL field not supported yet: modified_by (no storage source in BinFlow yet)` |
| `original_sha1` / `original_md5` | `... BinFlow does not store separate original checksums` |
| `stat.downloads` 等 stat 族 | `... (statistics data is not stored yet)`（M16 计划） |
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

## 响应 envelope 与截断

- 流式形态：前导 `\n{\n"results" : [ `，行间 `},{`，空集 `"results" : [  ]`（两空格）；日期回显 ISO8601 毫秒 UTC。`?compact=true` 只压行体与 range，外层包裹不变。
- `range` 对象：`start_pos`（= offset 回显）、`end_pos` / `total`（**本页行数**——流式约定，total 不是全量计数，翻页判断用「本页满窗或 notification 在场」）、`limit`（仅声明时）、`notification`（截断通告）。
- **行数上限 1,000**：超限返回前 1,000 行，同时置响应头 `X-Binflow-Search-Truncated: true` 与 `range.notification = "AQL query reached the search hard limit, results are trimmed."`（文案与 Artifactory 逐字一致）。用 `.offset()` 续翻可达全量。
  - 注意：**用户自己的 `.limit(n)` 小于原始命中数时同样置截断标记**（诚实上界——「窗口没吃完全集」就告诉你还有余量），此时 notification 语义是「仍有未取行」，不区分是用户 limit 还是硬上限。
- 查询文本上限 **6,000 字符**（与 Artifactory 官方默认一致）：超限 400 `AQL query is too long; please reduce the query length to less than 6000 chars`。

## 权限与脱敏

- **行级过滤**：结果按调用者 read 权限过滤——不限 repo 的查询也只回可读行（admin 全见）。
- **身份脱敏**：非 admin 调用者看到的 `created_by` 等身份字段值为字面量 `unknown`（admin 调用回显真名）。
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
| `stat.downloads` 统计字段 | 400（统计未存储，M16 计划） | 下载统计改用 usage 端点/指标（M16 前） |
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
