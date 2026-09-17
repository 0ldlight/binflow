# Domain: 搜索域 + 仓库管理域 + reindex 管理族 + webhook 事件面。
# Source: docs/user/api-reference.md「SR: 搜索域」「SR: 仓库管理域」「四包型 reindex 管理族」
#「M13 增补速览 · webhook 域」；路由核对 internal/httpapi/router.go + webhooks.go。
# Ahead of the contract page（wire 事实取自 handler）：search_dates.go 的
# creation/dates 二端点、repositories_probe.go 的 remote 仓上游探测。

from helpers import (op, tag, q, pp, r, S, arr, body, obj,
                     ERR_401, ERR_403, ANON)

tag("search", "Search — artifact/checksum/AQL/usage/gavc/prop/pattern (results filtered by the caller's permissions)")
tag("repositories", "Repository management (/api/repositories)")
tag("reindex", "Reindex family — conan/helm/deb/yum index rebuilds")
tag("webhooks", "Webhook subscriptions (/event/api/v1 — note it is not under /binflow/api; the whole family sits behind the webhook pro slot)")


def build():
    # ---- 搜索 ----
    op("/api/search/artifact", "get", "searchArtifact", "search", "Search by name substring",
       "Canonical path: `GET /binflow/api/search/artifact?name=&repos=`. SQL LIKE (case-insensitive substring), permission-filtered.",
       params=[q("name", "Required, case-insensitive substring", required=True),
               q("repos", "a,b comma-separated narrowing")],
       responses={"200": r("Results envelope", schema=S("SearchResults"))},
       security=ANON)

    op("/api/search/checksum", "get", "searchChecksum", "search", "Exact checksum search",
       "Canonical path: `GET /binflow/api/search/checksum?sha1=&repos=` (at least one of sha1/md5/sha256).",
       params=[q("sha1", None), q("md5", None), q("sha256", None), q("repos", None)],
       responses={"200": r("Results envelope", schema=S("SearchResults"))},
       security=ANON)

    op("/api/search/aql", "post", "searchAql", "search", "AQL query",
       "Canonical path: `POST /binflow/api/search/aql`. The body is AQL text (`text/plain`; `?query=` as the empty-body "
       "fallback; `?compact=true`). The items domain subset + the `stat.*` statistics field family "
       "(language/error/migration mapping in the AQL search guide). **Not available to anonymous users** "
       "(closed instances 401 / anonymous-enabled instances 403); unsupported domains and fields are rejected 400 by name; "
       "chain order `include→sort→offset→limit` out of order = 400 syntax error. Limits: results 1,000 rows "
       "(over cap sets `X-Binflow-Search-Truncated: true` + range.notification, continue paging with `.offset()`); "
       "query text 6,000 characters; concurrency 4 → 429 + `Retry-After: 1`; execution 10s → 408. "
       "Virtual keys are legal (member repositories expand at compile time).",
       params=[q("compact", "true for compact output", schema={"type": "boolean"}),
               q("query", "Query-parameter fallback form of the query text")],
       req_body=body("AQL query text (text/plain)", schema={"type": "string"},
                     example='items.find({"repo":"maven-local"}).include("repo","path","name").sort({"$desc":["name"]}).limit(2)',
                     ctype="text/plain", required=True),
       responses={"200": r("AQL results", schema=S("AqlResponse"),
                           example={"results": [{"repo": "maven-local", "path": "com/acme/demo",
                                                 "name": "maven-metadata.xml"}],
                                    "range": {"start_pos": 0, "end_pos": 2, "total": 2, "limit": 2}}),
                  "400": r("Unsupported domain/field/syntax error (message verbatim)", schema=S("ErrorsEnvelope"),
                           example={"errors": [{"status": 400,
                                                "message": "AQL domain not supported: builds (BinFlow AQL supports: items; build-info domains are not implemented)"}]}),
                  "401": ERR_401, "403": ERR_403, "408": r("Execution exceeded 10s"),
                  "429": r("Concurrency cap of 4 (Retry-After: 1)")})

    op("/api/search/usage", "get", "searchUsage", "search", "Unused artifact search",
       "Canonical path: `GET /binflow/api/search/usage?notUsedSince=&createdBefore=&repos=`. "
       "The data face for the \"not downloaded in N days\" cleanup strategy; rows carry five fields "
       "`{uri, downloadCount, lastDownloaded, remoteDownloadCount, remoteLastDownloaded}`; "
       "both the empty set and missing parameters return **404 `No results found.`** "
       "(semantics in the AQL search guide · usage endpoint).",
       params=[q("notUsedSince", "Required, epoch milliseconds", required=True),
               q("createdBefore", "Defaults back to notUsedSince"),
               q("repos", "a,b")],
       responses={"200": r("Unused rows", schema=arr(S("UsageSearchRow"))),
                  "404": r("No results found.", schema={"type": "string"},
                           example="No results found.", ctype="text/plain")})

    op("/api/search/creation", "get", "searchCreation", "search", "Search by creation time",
       "Canonical path: `GET /binflow/api/search/creation?from=&to=&repos=`. `from` is required, epoch milliseconds "
       "(non-negative; missing → 400 `'from' parameter cannot be empty!` — single quotes verbatim); `to` defaults to now. "
       "Row shape `{uri, created}` — `created` echoes with fallback: rows whose created falls inside the requested range "
       "return created, rows matched only via lastModified return the modification time. Empty set "
       "**404 `No results found.`** (the 404-empty family); over the 1,000-row cap sets "
       "`X-Binflow-Search-Truncated: true`; anonymous → 401 `Authentication is required`.",
       params=[q("from", "Required, epoch milliseconds (non-negative)", required=True, example="1725148800000"),
               q("to", "Epoch milliseconds, defaults to now"),
               q("repos", "a,b")],
       responses={"200": r("Date rows", schema=S("DateRangeResults")),
                  "400": r("from missing/non-numeric", schema=S("ErrorsEnvelope"),
                           example={"errors": [{"status": 400,
                                                "message": "'from' parameter cannot be empty!"}]}),
                  "401": ERR_401,
                  "404": r("No results found.", schema={"type": "string"},
                           example="No results found.", ctype="text/plain")})

    op("/api/search/dates", "get", "searchDates", "search", "Search by date field",
       "Canonical path: `GET /binflow/api/search/dates?from=&to=&dateFields=&repos=`. "
       "`dateFields` is a CSV over a closed set of four values (wire echo order) "
       "`created, lastModified, lastDownloaded, remote_last_downloaded`, defaulting to {created, lastModified}; "
       "unknown names get 400 verbatim `Date field name '<name>' unknown!, possible values are: [created, lastModified, "
       "lastDownloaded, remote_last_downloaded]`. Row shape is the same as creation (`{uri, created}` slim row); "
       "empty set 404 `No results found.`; anonymous 401.",
       params=[q("from", "Required, epoch milliseconds (non-negative)", required=True, example="1725148800000"),
               q("to", "Epoch milliseconds, defaults to now"),
               q("dateFields", "CSV over the closed four-value set (see description)",
                 example="created,lastModified"),
               q("repos", "a,b")],
       responses={"200": r("Date rows", schema=S("DateRangeResults")),
                  "400": r("from missing/unknown dateFields name", schema=S("ErrorsEnvelope"),
                           example={"errors": [{"status": 400,
                                                "message": "Date field name 'created2' unknown!, possible values are: [created, lastModified, lastDownloaded, remote_last_downloaded]"}]}),
                  "401": ERR_401,
                  "404": r("No results found.", schema={"type": "string"},
                           example="No results found.", ctype="text/plain")})

    op("/api/search/gavc", "get", "searchGavc", "search", "Maven coordinate search",
       "Canonical path: `GET /binflow/api/search/gavc?g=&a=&v=&c=&repos=` (at least one of g/a/v/c). "
       "Matches Maven layout path forms (fully literal, case-sensitive); does not filter by repository package "
       "type/layout descriptors (`repos=` narrows).",
       params=[q("g", "groupId"), q("a", "artifactId"), q("v", "version"),
               q("c", "classifier"), q("repos", None)],
       responses={"200": r("Results envelope (empty-set family — `results:[]` not 404)", schema=S("SearchResults"))})

    op("/api/search/prop", "get", "searchProp", "search", "Property search",
       "Canonical path: `GET /binflow/api/search/prop?props=k[=v]` (or any `?k=v` parameter — `repos` is reserved). "
       "A key without a value = key existence; key-value pairs follow the property grammar (see the properties guide; "
       "invalid keys 400). Note `prop` is the official singular spelling — the plural `props` is 404.",
       params=[q("props", "k=v or k (key existence)"), q("repos", "Reserved parameter")],
       responses={"200": r("Results envelope", schema=S("SearchResults")),
                  "400": r("Invalid property key")})

    op("/api/search/pattern", "get", "searchPattern", "search", "Path pattern search",
       "Canonical path: `GET /binflow/api/search/pattern?pattern=<repo-glob>:<path-glob>`. "
       "`*`/`?` cross segments (SQL semantics, same engine as AQL $match); the repo half may wildcard across "
       "repositories. Querying a virtual key → empty set (member expansion happens only on the AQL face).",
       params=[q("pattern", "<repo-glob>:<path-glob>", required=True,
                 example="maven-local:com/acme/**/*.jar")],
       responses={"200": r("Results envelope", schema=S("SearchResults")),
                  "400": r("Missing colon separator", schema=S("ErrorsEnvelope"),
                           example={"errors": [{"status": 400,
                                                "message": "Pattern search requires a '<repo-pattern>:<path-pattern>' value."}]})})

    # ---- 仓库管理 ----
    op("/api/repositories", "get", "repoList", "repositories", "List repositories",
       "Canonical path: `GET /binflow/api/repositories?type=&packageType=` (admin / readonly_admin).",
       params=[q("type", "Filter: local | remote | virtual",
                 schema={"type": "string", "enum": ["local", "remote", "virtual"]}),
               q("packageType", "Filter by package type")],
       responses={"200": r("Repository configuration list", schema=arr(S("RepoConfig"))),
                  "403": ERR_403})

    op("/api/repositories/{key}", "get", "repoGet", "repositories", "Get repository configuration",
       "Canonical path: `GET /binflow/api/repositories/{key}` (manage holders can also read repositories in their "
       "coverage set).",
       params=[pp("key", "Repository key")],
       responses={"200": r("Repository configuration (canonical echo)", schema=S("RepoConfig")),
                  "404": r("Repository not found (`Failed to find the repository '<key>' specified in the request.`)")})

    op("/api/repositories/{key}", "put", "repoPut", "repositories", "Create repository (create-only)",
       "Canonical path: `PUT /binflow/api/repositories/{key}`. **Create only** — an existing key always gets **400** "
       "(errors envelope verbatim `error when validating repository name: <key> : Repository key already exists`) with "
       "**zero side effects** (the stored configuration is untouched); a body key that differs from the path key is also "
       "400. The only update spelling is POST (merge semantics, next endpoint). Admin only (manage holders cannot create "
       "repositories). A body missing `rclass` hits the creation-path type validation first — 400 "
       "(the empty packageType slot is rejected — ahead of the key-exists check). "
       "Creation forms: `rclass=remote + packageType=docker` (community tier — no new license slot); "
       "`rclass=virtual + packageType=docker` is also open (the aggregated read face serves the union of member "
       "repositories) — **the rclass × packageType combination gates are fully retired**; the only remaining gate on "
       "creation is the license tier (advanced package types on lower tiers get 400 "
       "`package type not available on this instance: ...`).",
       params=[pp("key", "Repository key")],
       req_body=body("Repository configuration (full creation body)", schema=S("RepoConfig"),
                     example={"rclass": "local", "packageType": "generic",
                              "description": "demo"}),
       responses={"200": r("Created (plain text `Successfully created repository '<key>'`)", ctype="text/plain"),
                  "400": r("Existing key (create-only 400, zero side effects) / invalid parameters / body key differs from the path key",
                           schema=S("ErrorsEnvelope"),
                           example={"errors": [{"status": 400,
                                                "message": "error when validating repository name: libs-release : Repository key already exists"}]})})

    op("/api/repositories/{key}", "post", "repoPost", "repositories", "Update repository (merge)",
       "Canonical path: `POST /binflow/api/repositories/{key}`. **The only update spelling**, three-column merge "
       "semantics — fields **omitted** from the body = keep the stored value; **`null` / empty string** = clear that "
       "field (array empty values are kept, an object `{}` resets the whole family); **explicit values** = overwrite "
       "(`0` is also an explicit value, no default fallback). The description layer: no `description` key in the body = "
       "keep the stored value; explicit `null` (decoded as an empty string) or a value = overwrite. Omitted "
       "rclass / packageType keep the stored values. Unknown key 404. Includes quotaBytes quota writes; admin or the "
       "repository's manage holder (the quota write arm is open to coverage-set repositories).",
       params=[pp("key", "Repository key")],
       req_body=body("Repository configuration (delta fields — omitted means kept)", schema=S("RepoConfig"),
                     example={"description": "new text"}),
       responses={"200": r("Updated (plain text `Repository <key> update successfully.`)", ctype="text/plain"),
                  "400": r("Body is not valid JSON", schema=S("ErrorsEnvelope"),
                           example={"errors": [{"status": 400,
                                                "message": "request body is not valid repository configuration JSON: ..."}]}),
                  "404": r("Repository not found (`Repository does not exist: ...`)", schema=S("ErrorsEnvelope"),
                           example={"errors": [{"status": 404,
                                                "message": "Repository does not exist: repo \"libs-release\": repository not found"}]})})

    op("/api/repositories/{key}", "delete", "repoDelete", "repositories", "Delete repository",
       "Canonical path: `DELETE /binflow/api/repositories/{key}?deleteContent=` (admin only, not delegated).",
       params=[pp("key", "Repository key"),
               q("deleteContent", "true = delete the content as well", schema={"type": "boolean"})],
       responses={"200": r("Deleted"), "403": ERR_403})

    op("/api/repositories/{key}/test", "post", "repoTest", "repositories",
       "Remote repository upstream connectivity probe",
       "Canonical path: `POST /binflow/api/repositories/{key}/test` (same gate as the configuration write arm — "
       "admin or the repository's manage holder). The body is **optional** `{url,username,password}` — non-empty fields "
       "override the stored configuration for this probe only, with zero writes; a changed URL/username without a "
       "password → probe anonymously (the stored secret is never sent); an empty body = probe with the stored "
       "configuration. Only `rclass=remote` repositories have an upstream to test. The verdict body 200/400 is the same "
       "shape `{ok,status_code,message}`: pass 200 `Remote repository '<key>' url '<url>' tested successfully`; "
       "failure `ok:false` 400 with the reason inline. Unknown repository 404 `repository not found: <key>` (envelope); "
       "audited as `repository.remote.test`.",
       params=[pp("key", "Repository key")],
       req_body=body("Draft override (optional; empty body = probe with the stored configuration)", schema=S("RepoTestInput")),
       responses={"200": r("Probe passed", schema=S("ProbeOutcome"),
                           example={"ok": True, "status_code": 200,
                                    "message": "Remote repository 'maven-remote' url 'https://repo.example.com/maven' tested successfully"}),
                  "400": r("Probe failed (ok:false with reason inline) / not a remote repository / invalid body", schema=S("ProbeOutcome"),
                           example={"ok": False, "status_code": 401,
                                    "message": "Connection failed: Remote repository URL returned error 401: {…}"}),
                  "401": ERR_401, "403": ERR_403,
                  "404": r("Repository not found", schema=S("ErrorsEnvelope"),
                           example={"errors": [{"status": 404, "message": "repository not found: maven-remote"}]})})

    # ---- reindex 族 ----
    op("/api/conan/reindex", "post", "conanReindex", "reindex", "conan recipe index rebuild (query form)",
       "Canonical path: `POST /binflow/api/conan/reindex?repoKey=` (local repositories only; synchronous; CanManageRepo).",
       params=[q("repoKey", "Repository key (query/body form)")],
       responses={"200": r("Rebuilt")})

    op("/api/conan/{repoKey}/reindex", "post", "conanReindexByKey", "reindex",
       "conan recipe index rebuild (path form)",
       "Canonical path: `POST /binflow/api/conan/{repoKey}/reindex` (or `…/{repoKey}/{sub}/reindex`).",
       params=[pp("repoKey", "Repository key")],
       responses={"200": r("Rebuilt")})

    op("/api/helm/{repoKey}/reindex", "post", "helmReindex", "reindex",
       "helm index.yaml recomputation (whole repository, async)",
       "Canonical path: `POST /binflow/api/helm/{repoKey}/reindex` (whole repository, async).",
       params=[pp("repoKey", "Repository key")],
       responses={"200": r("Accepted (async)")})

    op("/api/helm/{repoKey}/reindex/{path}", "post", "helmReindexPath", "reindex",
       "helm index.yaml recomputation (partial, synchronous)",
       "Canonical path: `POST /binflow/api/helm/{repoKey}/reindex/{path}` (synchronous partial).",
       params=[pp("repoKey", "Repository key"), pp("path", "Subtree path")],
       responses={"200": r("Recomputed")})

    op("/api/deb/reindex/{repoKey}", "post", "debReindex", "reindex",
       "debian index recomputation",
       "Canonical path: `POST /binflow/api/deb/reindex/{repoKey}?async=0|1` (virtual/remote classes get 400).",
       params=[pp("repoKey", "Repository key"),
               q("async", "0 synchronous | 1 asynchronous", schema={"type": "string", "enum": ["0", "1"]})],
       responses={"200": r("Recomputed"), "400": r("virtual/remote classes rejected")})

    op("/api/yum/{repoKey}", "post", "yumReindex", "reindex",
       "rpm repodata recomputation",
       "Canonical path: `POST /binflow/api/yum/{repoKey}?path=&async=0|1`. "
       "`path` gets `/repodata` appended automatically; a synchronous request against an auto-async repository gets 409; "
       "**on virtual repositories 200/202 triggers an aggregate re-merge**.",
       params=[pp("repoKey", "Repository key"),
               q("path", "Subtree (/repodata appended automatically)"),
               q("async", "0 synchronous | 1 asynchronous", schema={"type": "string", "enum": ["0", "1"]})],
       responses={"200": r("Recomputed / aggregate re-merge accepted"), "202": r("Accepted (async)"),
                  "409": r("Synchronous request against an auto-async repository")})

    # ---- webhook 事件面 ----
    sub = "/event/api/v1/subscriptions"
    op(sub, "get", "webhookList", "webhooks", "Subscription list",
       "Canonical path: `GET /binflow/event/api/v1/subscriptions` (system:read — visible to readonly_admin; bare array). "
       "Note it lives **outside** `/binflow/api`. The read face has no license gate.",
       responses={"200": r("Subscription list (bare array)", schema=arr(S("SubscriptionView")))})

    op(sub, "post", "webhookCreate", "webhooks", "Create a subscription",
       "Canonical path: `POST /binflow/event/api/v1/subscriptions` (system:write + license — write verbs on community "
       "get 403 + `X-Binflow-License-Required: webhook`). **201** echoes a SubscriptionView (`secret` always masked "
       "`********`). Delivery: HMAC-SHA256 hex in `X-JFrog-Event-Auth` (with `use_secret_for_signing=false` the secret "
       "is sent as plaintext); retries **5 including the first attempt / fixed 10s spacing / 30s per-attempt timeout / "
       "only on send failure or ≥500** (4xx is a one-step terminal state); dead letters are audited as "
       "`webhook.dead_letter`. SSRF: targets on loopback/private networks are rejected by default; "
       "`webhook.allow_private_target` (default false, effective on restart) permits them.",
       req_body=body("Subscription body", schema=S("SubscriptionRequest"), required=True),
       responses={"201": r("SubscriptionView", schema=S("SubscriptionView")),
                  "403": r("Rejected by the community-tier license gate (`X-Binflow-License-Required: webhook`)")})

    op(sub + "/test", "post", "webhookTest", "webhooks", "Test-send a draft",
       "Canonical path: `POST /binflow/event/api/v1/subscriptions/test` (system:write + license). "
       "**Test-sends a draft** (consumes a full subscription body, not a key reference); a synchronous single send that "
       "does not enter the queue; 200 TestOutcome (`ok`/`attempt{status_code,elapsed_millis,error}` — **failures also "
       "return 200, inspect the body**).",
       req_body=body("Full subscription body (draft)", schema=S("SubscriptionRequest"), required=True),
       responses={"200": r("TestOutcome", schema=S("TestOutcome"))})

    op(sub + "/{key}", "get", "webhookGet", "webhooks", "Get a subscription",
       "Canonical path: `GET /binflow/event/api/v1/subscriptions/{key}` (system:read). A miss is "
       "**404 `Subscription not found`**.",
       params=[pp("key", "Subscription key")],
       responses={"200": r("SubscriptionView", schema=S("SubscriptionView")),
                  "404": r("Subscription not found")})

    op(sub + "/{key}", "put", "webhookUpdate", "webhooks", "Fully update a subscription",
       "Canonical path: `PUT /binflow/event/api/v1/subscriptions/{key}` (system:write + license). "
       "**204 with no body**; the key cannot be changed.",
       params=[pp("key", "Subscription key")],
       req_body=body("Subscription body (full)", schema=S("SubscriptionRequest"), required=True),
       responses={"204": r("Updated (no body)")})

    op(sub + "/{key}", "delete", "webhookDelete", "webhooks", "Delete a subscription",
       "Canonical path: `DELETE /binflow/event/api/v1/subscriptions/{key}` (system:write + license). "
       "Cascades to delivery rows; **204**; deleting again 404.",
       params=[pp("key", "Subscription key")],
       responses={"204": r("Deleted (no body)"), "404": r("Deleting again 404")})

    op("/event/api/v1/troubleshooting", "get", "webhookTroubleshooting", "webhooks",
       "Troubleshooting record ring",
       "Canonical path: `GET /binflow/event/api/v1/troubleshooting` (system:read). "
       "Failures are always recorded, successes too when `debug:true`; an in-process ring of 10,000 entries pruned "
       "every 30s, history is lost on restart.",
       params=[q("subscription", "Filter by subscription"), q("target", "Filter by target"),
               q("start", None), q("end", None),
               q("count", None, schema={"type": "integer"})],
       responses={"200": r("Troubleshooting records", schema=arr(obj({}, desc="Delivery troubleshooting record row")))})
