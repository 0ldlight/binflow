# Domain: 搜索域 + 仓库管理域 + reindex 管理族 + webhook 事件面。
# Source: docs/user/api-reference.md「SR: 搜索域」「SR: 仓库管理域」「四包型 reindex 管理族」
#「M13 增补速览 · webhook 域」；路由核对 internal/httpapi/router.go + webhooks.go。
# Ahead of the contract page（wire 事实取自 handler）：search_dates.go 的
# creation/dates 二端点、repositories_probe.go 的 remote 仓上游探测。

from helpers import (op, tag, q, pp, r, S, arr, body, obj,
                     ERR_401, ERR_403, ANON)

tag("search", "搜索域——artifact/checksum/AQL/usage/gavc/prop/pattern（结果按调用者权限过滤）")
tag("repositories", "仓库管理（/api/repositories）")
tag("reindex", "reindex 管理族——conan/helm/deb/yum 索引重建")
tag("webhooks", "webhook 订阅（/event/api/v1——注意不在 /binflow/api 下；整族 pro 槽 webhook）")


def build():
    # ---- 搜索 ----
    op("/api/search/artifact", "get", "searchArtifact", "search", "按名称子串搜索",
       "官方拼写：`GET /binflow/api/search/artifact?name=&repos=`。SQL LIKE（大小写不敏感子串），权限过滤。",
       params=[q("name", "必填，大小写不敏感子串", required=True),
               q("repos", "a,b 逗号分隔收窄")],
       responses={"200": r("结果信封", schema=S("SearchResults"))},
       security=ANON)

    op("/api/search/checksum", "get", "searchChecksum", "search", "按 checksum 精确搜索",
       "官方拼写：`GET /binflow/api/search/checksum?sha1=&repos=`（sha1/md5/sha256 至少一）。",
       params=[q("sha1", None), q("md5", None), q("sha256", None), q("repos", None)],
       responses={"200": r("结果信封", schema=S("SearchResults"))},
       security=ANON)

    op("/api/search/aql", "post", "searchAql", "search", "AQL 查询",
       "官方拼写：`POST /binflow/api/search/aql`。body = AQL 文本（`text/plain`；`?query=` 空体回退；"
       "`?compact=true`）。items 域子集 + `stat.*` 统计字段族（语言/错误/迁移对照见 AQL 搜索指南）。"
       "**匿名不可用**（闭环实例 401 / 开匿名实例 403）；未支持域与字段一律 400 点名；"
       "链序 `include→sort→offset→limit` 乱序 = 400 语法错。上限：结果 1,000 行（超限置 "
       "`X-Binflow-Search-Truncated: true` + range.notification，`.offset()` 续翻）；查询文本 6,000 字符；"
       "并发 4 → 429 + `Retry-After: 1`；执行 10s → 408。virtual key 合法（编译期展开成员仓）。",
       params=[q("compact", "true 压缩输出", schema={"type": "boolean"}),
               q("query", "查询文本的 query 参数回退形态")],
       req_body=body("AQL 查询文本（text/plain）", schema={"type": "string"},
                     example='items.find({"repo":"maven-local"}).include("repo","path","name").sort({"$desc":["name"]}).limit(2)',
                     ctype="text/plain", required=True),
       responses={"200": r("AQL 结果", schema=S("AqlResponse"),
                           example={"results": [{"repo": "maven-local", "path": "com/acme/demo",
                                                 "name": "maven-metadata.xml"}],
                                    "range": {"start_pos": 0, "end_pos": 2, "total": 2, "limit": 2}}),
                  "400": r("未支持域/字段/语法错（文案逐字）", schema=S("ErrorsEnvelope"),
                           example={"errors": [{"status": 400,
                                                "message": "AQL domain not supported: builds (BinFlow AQL supports: items; build-info domains are not implemented)"}]}),
                  "401": ERR_401, "403": ERR_403, "408": r("执行 10s 超时"),
                  "429": r("并发 4 上限（Retry-After: 1）")})

    op("/api/search/usage", "get", "searchUsage", "search", "闲置制品检索",
       "官方拼写：`GET /binflow/api/search/usage?notUsedSince=&createdBefore=&repos=`。"
       "「N 天未下载」清理策略数据面；行五字段 `{uri, downloadCount, lastDownloaded, remoteDownloadCount, remoteLastDownloaded}`；"
       "空集与缺参均 **404 `No results found.`**（语义见 AQL 搜索指南 · usage 端点）。",
       params=[q("notUsedSince", "必填 epoch 毫秒", required=True),
               q("createdBefore", "缺省回退 notUsedSince"),
               q("repos", "a,b")],
       responses={"200": r("闲置行", schema=arr(S("UsageSearchRow"))),
                  "404": r("No results found.", schema={"type": "string"},
                           example="No results found.", ctype="text/plain")})

    op("/api/search/creation", "get", "searchCreation", "search", "按创建时间检索",
       "官方拼写：`GET /binflow/api/search/creation?from=&to=&repos=`。`from` 必填 epoch 毫秒"
       "（非负；缺失 400 `'from' parameter cannot be empty!`——单引号逐字）；`to` 缺省 = now。"
       "行形 `{uri, created}`——`created` 回显带回退：created 落在请求区间内回 created，"
       "仅经 lastModified 命中的行回修改时刻。空集 **404 `No results found.`**（404-empty 族）；"
       "超限 1,000 行置 `X-Binflow-Search-Truncated: true`；匿名 401 `Authentication is required`。",
       params=[q("from", "必填 epoch 毫秒（非负）", required=True, example="1725148800000"),
               q("to", "epoch 毫秒，缺省 now"),
               q("repos", "a,b")],
       responses={"200": r("日期行", schema=S("DateRangeResults")),
                  "400": r("from 缺失/非数值", schema=S("ErrorsEnvelope"),
                           example={"errors": [{"status": 400,
                                                "message": "'from' parameter cannot be empty!"}]}),
                  "401": ERR_401,
                  "404": r("No results found.", schema={"type": "string"},
                           example="No results found.", ctype="text/plain")})

    op("/api/search/dates", "get", "searchDates", "search", "按日期字段检索",
       "官方拼写：`GET /binflow/api/search/dates?from=&to=&dateFields=&repos=`。"
       "`dateFields` 为 CSV，闭集四值（wire echo 顺序）`created, lastModified, lastDownloaded, "
       "remote_last_downloaded`，缺省 {created, lastModified}；未知名 400 逐字 "
       "`Date field name '<name>' unknown!, possible values are: [created, lastModified, "
       "lastDownloaded, remote_last_downloaded]`。行形与 creation 同（`{uri, created}` 瘦行）；"
       "空集 404 `No results found.`；匿名 401。",
       params=[q("from", "必填 epoch 毫秒（非负）", required=True, example="1725148800000"),
               q("to", "epoch 毫秒，缺省 now"),
               q("dateFields", "CSV 闭集四值（见描述）",
                 example="created,lastModified"),
               q("repos", "a,b")],
       responses={"200": r("日期行", schema=S("DateRangeResults")),
                  "400": r("from 缺失/未知 dateFields 名", schema=S("ErrorsEnvelope"),
                           example={"errors": [{"status": 400,
                                                "message": "Date field name 'created2' unknown!, possible values are: [created, lastModified, lastDownloaded, remote_last_downloaded]"}]}),
                  "401": ERR_401,
                  "404": r("No results found.", schema={"type": "string"},
                           example="No results found.", ctype="text/plain")})

    op("/api/search/gavc", "get", "searchGavc", "search", "Maven 坐标检索",
       "官方拼写：`GET /binflow/api/search/gavc?g=&a=&v=&c=&repos=`（g/a/v/c 至少一）。"
       "按 Maven 布局路径形态匹配（全字面、大小写敏感）；不按仓包型/layout 描述符过滤（`repos=` 收窄）。",
       params=[q("g", "groupId"), q("a", "artifactId"), q("v", "version"),
               q("c", "classifier"), q("repos", None)],
       responses={"200": r("结果信封（空集族——`results:[]` 非 404）", schema=S("SearchResults"))})

    op("/api/search/prop", "get", "searchProp", "search", "按属性检索",
       "官方拼写：`GET /binflow/api/search/prop?props=k[=v]`（或任意 `?k=v` 参数——`repos` 保留）。"
       "键无值 = 键存在性；键值走属性文法（见属性系统指南；非法键 400）。注意 `prop` 是官方单数拼写，复数 `props` 404。",
       params=[q("props", "k=v 或 k（键存在性）"), q("repos", "保留参数")],
       responses={"200": r("结果信封", schema=S("SearchResults")),
                  "400": r("非法属性键")})

    op("/api/search/pattern", "get", "searchPattern", "search", "按路径模式检索",
       "官方拼写：`GET /binflow/api/search/pattern?pattern=<repo-glob>:<path-glob>`。"
       "`*`/`?` 跨段（SQL 语义，与 AQL $match 同内核）；repo 半可通配跨仓。"
       "查 virtual key → 空集（成员展开只在 AQL 面）。",
       params=[q("pattern", "<repo-glob>:<path-glob>", required=True,
                 example="maven-local:com/acme/**/*.jar")],
       responses={"200": r("结果信封", schema=S("SearchResults")),
                  "400": r("缺冒号分隔", schema=S("ErrorsEnvelope"),
                           example={"errors": [{"status": 400,
                                                "message": "Pattern search requires a '<repo-pattern>:<path-pattern>' value."}]})})

    # ---- 仓库管理 ----
    op("/api/repositories", "get", "repoList", "repositories", "仓库列表",
       "官方拼写：`GET /binflow/api/repositories?type=&packageType=`（admin / readonly_admin）。",
       params=[q("type", "过滤：local | remote | virtual",
                 schema={"type": "string", "enum": ["local", "remote", "virtual"]}),
               q("packageType", "按包型过滤")],
       responses={"200": r("仓库配置列表", schema=arr(S("RepoConfig"))),
                  "403": ERR_403})

    op("/api/repositories/{key}", "get", "repoGet", "repositories", "单仓配置",
       "官方拼写：`GET /binflow/api/repositories/{key}`（manage 持有者对覆盖仓亦可读）。",
       params=[pp("key", "仓库 key")],
       responses={"200": r("仓库配置（canonical 回显）", schema=S("RepoConfig")),
                  "404": r("仓库不存在（`Failed to find the repository '<key>' specified in the request.`）")})

    op("/api/repositories/{key}", "put", "repoPut", "repositories", "建仓 / 替换既有仓",
       "官方拼写：`PUT /binflow/api/repositories/{key}`。建仓（创建）/ 替换既有仓"
       "（替换臂与配额字段对覆盖仓的 manage 持有者开放；**建仓臂仍 admin only**）。"
       "建仓形态：`rclass=remote + packageType=docker`（community 档——不新增 license 槽）；"
       "`rclass=virtual + packageType=docker` 亦已开闸（聚合读面按成员仓并集服务）——"
       "**rclass × packageType 组合门已全量退役**，建仓面唯一剩余门是 license 档位"
       "（进阶包型在低档位 400 `package type not available on this instance: ...`）。",
       params=[pp("key", "仓库 key")],
       req_body=body("仓库配置", schema=S("RepoConfig"),
                     example={"rclass": "local", "packageType": "generic",
                              "description": "demo"}),
       responses={"200": r("已替换"), "201": r("已创建"),
                  "400": r("参数非法", schema=S("ErrorsEnvelope"),
                           example={"errors": [{"status": 400,
                                                "message": "Repository key must be at least 2 characters: 'x'"}]})})

    op("/api/repositories/{key}", "post", "repoPost", "repositories", "改仓（更新配置）",
       "官方拼写：`POST /binflow/api/repositories/{key}`（含 quotaBytes 配额写；manage 持有者同上）。",
       params=[pp("key", "仓库 key")],
       req_body=body("仓库配置（增量字段）", schema=S("RepoConfig")),
       responses={"200": r("已更新")})

    op("/api/repositories/{key}", "delete", "repoDelete", "repositories", "删仓",
       "官方拼写：`DELETE /binflow/api/repositories/{key}?deleteContent=`（admin only，不下放）。",
       params=[pp("key", "仓库 key"),
               q("deleteContent", "true = 连内容一起删", schema={"type": "boolean"})],
       responses={"200": r("已删除"), "403": ERR_403})

    op("/api/repositories/{key}/test", "post", "repoTest", "repositories",
       "remote 仓上游连通探测",
       "官方拼写：`POST /binflow/api/repositories/{key}/test`（配置写臂同门——admin 或该仓 "
       "manage 持有者）。body **可选** `{url,username,password}`——非空字段仅本次探测覆盖存量配置，"
       "零写入；改了 URL/用户名没给密码 → 按匿名探测（存量密文不外发）；空 body = 按存量配置探测。"
       "仅 `rclass=remote` 仓有上游可测。判定体 200/400 同形 `{ok,status_code,message}`："
       "通过 200 `Remote repository '<key>' url '<url>' tested successfully`；失败 `ok:false` 400 内联原因。"
       "未知仓 404 `repository not found: <key>`（信封）；审计 `repository.remote.test`。",
       params=[pp("key", "仓库 key")],
       req_body=body("草稿覆盖（可选；空 body = 按存量配置探测）", schema=S("RepoTestInput")),
       responses={"200": r("探测通过", schema=S("ProbeOutcome"),
                           example={"ok": True, "status_code": 200,
                                    "message": "Remote repository 'maven-remote' url 'https://repo.example.com/maven' tested successfully"}),
                  "400": r("探测失败（ok:false 内联原因）/ 非 remote 仓 / body 非法", schema=S("ProbeOutcome"),
                           example={"ok": False, "status_code": 401,
                                    "message": "Connection failed: Remote repository URL returned error 401: {…}"}),
                  "401": ERR_401, "403": ERR_403,
                  "404": r("仓库不存在", schema=S("ErrorsEnvelope"),
                           example={"errors": [{"status": 404, "message": "repository not found: maven-remote"}]})})

    # ---- reindex 族 ----
    op("/api/conan/reindex", "post", "conanReindex", "reindex", "conan 修订索引重建（query 形）",
       "官方拼写：`POST /binflow/api/conan/reindex?repoKey=`（仅 local；同步；CanManageRepo）。",
       params=[q("repoKey", "仓 key（query/body 形）")],
       responses={"200": r("已重建")})

    op("/api/conan/{repoKey}/reindex", "post", "conanReindexByKey", "reindex",
       "conan 修订索引重建（路径形）",
       "官方拼写：`POST /binflow/api/conan/{repoKey}/reindex`（或 `…/{repoKey}/{sub}/reindex`）。",
       params=[pp("repoKey", "仓 key")],
       responses={"200": r("已重建")})

    op("/api/helm/{repoKey}/reindex", "post", "helmReindex", "reindex",
       "helm index.yaml 重算（全仓异步）",
       "官方拼写：`POST /binflow/api/helm/{repoKey}/reindex`（异步全仓）。",
       params=[pp("repoKey", "仓 key")],
       responses={"200": r("已受理（异步）")})

    op("/api/helm/{repoKey}/reindex/{path}", "post", "helmReindexPath", "reindex",
       "helm index.yaml 重算（部分同步）",
       "官方拼写：`POST /binflow/api/helm/{repoKey}/reindex/{path}`（同步部分）。",
       params=[pp("repoKey", "仓 key"), pp("path", "子树路径")],
       responses={"200": r("已重算")})

    op("/api/deb/reindex/{repoKey}", "post", "debReindex", "reindex",
       "debian 索引重算",
       "官方拼写：`POST /binflow/api/deb/reindex/{repoKey}?async=0|1`（virtual/remote 类 400）。",
       params=[pp("repoKey", "仓 key"),
               q("async", "0 同步 | 1 异步", schema={"type": "string", "enum": ["0", "1"]})],
       responses={"200": r("已重算"), "400": r("virtual/remote 类拒绝")})

    op("/api/yum/{repoKey}", "post", "yumReindex", "reindex",
       "rpm repodata 重算",
       "官方拼写：`POST /binflow/api/yum/{repoKey}?path=&async=0|1`。"
       "`path` 自动补 `/repodata`；auto-async 仓同步请求 409；**virtual 仓 200/202 触发聚合重合并**。",
       params=[pp("repoKey", "仓 key"),
               q("path", "子树（自动补 /repodata）"),
               q("async", "0 同步 | 1 异步", schema={"type": "string", "enum": ["0", "1"]})],
       responses={"200": r("已重算/聚合重合并受理"), "202": r("异步受理"),
                  "409": r("auto-async 仓的同步请求冲突")})

    # ---- webhook 事件面 ----
    sub = "/event/api/v1/subscriptions"
    op(sub, "get", "webhookList", "webhooks", "订阅列表",
       "官方拼写：`GET /binflow/event/api/v1/subscriptions`（system:read——readonly_admin 可见；bare array）。"
       "注意**不在** `/binflow/api` 下。读面不设 license 门。",
       responses={"200": r("订阅列表（bare array）", schema=arr(S("SubscriptionView")))})

    op(sub, "post", "webhookCreate", "webhooks", "创建订阅",
       "官方拼写：`POST /binflow/event/api/v1/subscriptions`（system:write + license——写动词 community 403 + "
       "`X-Binflow-License-Required: webhook`）。**201** 回显 SubscriptionView（`secret` 恒掩码 `********`）。"
       "投递：HMAC-SHA256 hex 于 `X-JFrog-Event-Auth`（`use_secret_for_signing=false` 时为 secret 明文直传）；"
       "重试 **5 次首试计入 / 固定 10s / 单次 30s 超时 / 仅发送失败或 ≥500**（4xx 一步终态）；"
       "死信落审计 `webhook.dead_letter`。SSRF：目标默认拒 loopback/私网；`webhook.allow_private_target`"
       "（默认 false，重启生效）放行。",
       req_body=body("订阅体", schema=S("SubscriptionRequest"), required=True),
       responses={"201": r("SubscriptionView", schema=S("SubscriptionView")),
                  "403": r("community 档 license 拒（`X-Binflow-License-Required: webhook`）")})

    op(sub + "/test", "post", "webhookTest", "webhooks", "试发草稿",
       "官方拼写：`POST /binflow/event/api/v1/subscriptions/test`（system:write + license）。"
       "**试发草稿**（吃完整订阅体，非 key 引用）；同步单发不入箱；200 TestOutcome"
       "（`ok`/`attempt{status_code,elapsed_millis,error}`——**失败也是 200，看 body**）。",
       req_body=body("完整订阅体（草稿）", schema=S("SubscriptionRequest"), required=True),
       responses={"200": r("TestOutcome", schema=S("TestOutcome"))})

    op(sub + "/{key}", "get", "webhookGet", "webhooks", "单查订阅",
       "官方拼写：`GET /binflow/event/api/v1/subscriptions/{key}`（system:read）。miss **404 `Subscription not found`**。",
       params=[pp("key", "订阅 key")],
       responses={"200": r("SubscriptionView", schema=S("SubscriptionView")),
                  "404": r("Subscription not found")})

    op(sub + "/{key}", "put", "webhookUpdate", "webhooks", "全量更新订阅",
       "官方拼写：`PUT /binflow/event/api/v1/subscriptions/{key}`（system:write + license）。"
       "**204 无体**；key 不可改。",
       params=[pp("key", "订阅 key")],
       req_body=body("订阅体（全量）", schema=S("SubscriptionRequest"), required=True),
       responses={"204": r("已更新（无体）")})

    op(sub + "/{key}", "delete", "webhookDelete", "webhooks", "删除订阅",
       "官方拼写：`DELETE /binflow/event/api/v1/subscriptions/{key}`（system:write + license）。"
       "级联删投递行；**204**；再删 404。",
       params=[pp("key", "订阅 key")],
       responses={"204": r("已删除（无体）"), "404": r("再删 404")})

    op("/event/api/v1/troubleshooting", "get", "webhookTroubleshooting", "webhooks",
       "排障记录环",
       "官方拼写：`GET /binflow/event/api/v1/troubleshooting`（system:read）。"
       "失败必录、`debug:true` 成功也录；进程内环 10000 条/30s 修剪，重启失史。",
       params=[q("subscription", "按订阅过滤"), q("target", "按目标过滤"),
               q("start", None), q("end", None),
               q("count", None, schema={"type": "integer"})],
       responses={"200": r("排障记录", schema=arr(obj({}, desc="投递排障记录行")))})
