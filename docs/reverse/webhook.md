# Webhook 统一事件总线 行为规格（M13 FR-114/115 前置，T-358）

> **取证基准（PRD M13 §1 特例条款）**：反编译集合无 webhook addon（`reverse-src/` 内 find `*webhook*` 为空；inv-4 §待验证 L365 既定）。本规格以 **docs.jfrog.com 官方文档为唯一行为基准**，实时取证于 **2026-08-30**（各页 updatedAt 见锚点）；反编译锚点（inv-4 §I/§K）仅用于补官方文档空白，逐条标注「此条补充官方规范」。
>
> 置信度分级（本文件口径）：**高** = 官方文档逐字锚定（含 OpenAPI schema）；**中** = 官方文档措辞歧义或仅有间接佐证；**低** = 官方文档自身瑕疵/推断，待动态验证。

## 0. 关键校正：36 事件 → 13 域 66 事件型

PRD 与主矩阵沿用的「36 事件」口径源自 inv-4 I1（反编译 `SupportedUnifiedEvents` 36 枚举，**内部统一事件总线**层，含 before/after 变体与 alt* 变体）。**用户可订阅的 webhook 事件面是另一层**：官方文档当前登记 **13 个域（domain）、66 个事件型（event_type）**（页 updatedAt 2026-07-22，含 2026 年新增的 xray_scan_status 与 app_trust 域）。两层关系：

- 内部总线（36 枚举）→ 出站分发时**投影**为用户面 `{domain, event_type}`；before/after 等内部变体不暴露给订阅者。
- 内部注册 REST `/api/v1/unifiedevent/registration`（inv-4 I4）是 **Worker/Xray 等内部 consumer 的注册面**，不是用户 webhook 订阅面；用户面是 `/event/api/v1/subscriptions*`（§1）。BinFlow 只需实现后者；前者语义归 ADR-0041 内部架构裁量。（置信度：高——两层均有官方文档/反编译双证）

**BinFlow 触发源覆盖界（K48/Q6 口径）**：66 事件型全表登记；**9 个有本体触发源**（artifact 域 5 + artifact_property 域 2 + docker 域 pushed/deleted），**57 个注册休眠**（可订阅、校验通过、永不触发、文档如实标注，Q6 暂行）。

## 1. 订阅面 REST 端点表（置信度：高，OpenAPI 3.1 逐字）

基座前缀：官方挂载在 Event 服务命名空间 **`/event/api/v1/`**（server `https://{jfrog_url}`）。BinFlow 映射沿 E-26 前缀口径（实现票定案，规格不锁）。

| # | 方法 | 路径 | 请求体 | 成功 | 错误 | 鉴权 |
|---|---|---|---|---|---|---|
| 1 | GET | `/event/api/v1/subscriptions` | — | 200：`WebhookSubscription[]` | 401 Bad Credentials / 403 Permission Denied | r |
| 2 | POST | `/event/api/v1/subscriptions` | `WebhookSubscriptionCreate` | **201**：`WebhookSubscription` | 400 Bad Request / 401 / 403 | w |
| 3 | GET | `/event/api/v1/subscriptions/{key}` | — | 200：`WebhookSubscription` | 401 / 403 / **404 Not Found - Subscription not found** | r |
| 4 | PUT | `/event/api/v1/subscriptions/{key}` | `WebhookSubscriptionUpdate` | **204**（无响应体） | 400 / 401 / 403 / 404 | w |
| 5 | DELETE | `/event/api/v1/subscriptions/{key}` | — | **204** | 401 / 403 / 404 | d |
| 6 | POST | `/event/api/v1/subscriptions/test` | `WebhookSubscriptionCreate`（**完整订阅体，非 key 引用**——对未落盘的草稿配置直接试发） | 200 Test successful | 400 / 401 / 403 | x |
| 7 | GET | `/event/api/v1/troubleshooting` | query：`subscription`/`target`（实时流过滤）；`start`/`end`/`count`（历史拉取，`end`/`count` 不可脱离 `start` 单独用；`start=0` 取 Redis 最早） | 200：排障记录对象（§7） | 401 / 403 | r |

鉴权（官方三层，逐字）：

1. **Administrator Roles**：admin / Project Admin / Platform Admin。**Project Admin 只能对 `artifact`、`artifact_property`、`docker`、`build` 四个域建订阅**（官方明文）。
2. **Manage Webhooks Role**（Artifactory **7.137.0** 起）：具该角色的普通用户。
3. **Scoped Token**（**7.135.0** 起）：scope 形如 `system:webhooks@<project_key>/<JFrog_domain>:<action>`，action ∈ `r|w|d|x`（test 用 `x`）。

认证载体：**Access Token 作 Bearer**（`Authorization: Bearer <token>`）。OpenAPI info 原文：「Unless otherwise specified, all APIs require Platform Admin privileges.」——非管理员缺 Manage Webhooks 角色时全端点 403。

版本备注：非 admin 用户可用是近期开放（administration 域 webhooks 页迁移注记：「Webhooks are no longer limited to Platform Admins」）；`destination` 域要求 Artifactory 7.15.1+；RBv2 域项目级订阅 7.125.3+；xray_scan_status 要求 Xray 3.150.0+。

## 2. 订阅 wire schema（置信度：高，OpenAPI 逐字）

### 2.1 WebhookSubscriptionCreate（POST 请求体）

| 字段 | 类型 | 必填 | 约束/语义（逐字或近逐字） |
|---|---|---|---|
| `key` | string | ✓ | `maxLength: 500`，`pattern: ^[A-Za-z][A-Za-z0-9_\\-]+$`（字母开头，仅字母数字下划线连字符） |
| `project_key` | string | 条件 | 「Required when all chosen event types support project scope.」 |
| `description` | string | — | 自由文本 |
| `enabled` | boolean | ✓ | **default: false**（建后默认禁用） |
| `event_filter` | EventFilter | ✓ | 见 2.2 |
| `handlers` | array | ✓ | **minItems: 1, maxItems: 1**——单订阅恰好一个 handler |
| `debug` | boolean | — | default: false；true = 无论成败都记录排障数据（§7） |

### 2.2 EventFilter / criteria

`EventFilter` 必填 `domain` + `event_types`（`minItems: 1`）；`criteria` 为域相关松散对象（`additionalProperties: true`），官方 schema 列出的全部维度：

`anyLocal` / `anyRemote` / `anyFederated` / `anyBuild` / `anyReleaseBundle`（bool）、`repoKeys` / `selectedBuilds` / `includePatterns` / `excludePatterns` / `registeredReleaseBundlesNames` / `selectedEnvironments` / `applicationKeys` / `stages`（string[]）、`selectedReleaseBundles`（`map<string, string[]>`——bundle 名 → pattern 列表）。

各域实际取用（官方事件页「Criteria by domain」节）：

| 域 | criteria 取用 | 语义 |
|---|---|---|
| artifact / artifact_property / docker | anyLocal、anyRemote、repoKeys、includePatterns、excludePatterns | 选中 any local/remote 时**对未来新建仓库同样生效**（官方注记）；include/exclude 为 Ant 风格通配 pattern |
| build | anyBuild、selectedBuilds、include/excludePatterns | 全局 scope 匹配系统内全部同名 build；项目 scope 仅项目内（7.122.2+） |
| release_bundle | registeredReleaseBundlesNames + patterns | |
| release_bundle_v2 | anyReleaseBundleV2 形态 / selectedReleaseBundles + 项目 scope | 仅平台 admin 可建项目级 RBv2 订阅（官方明文） |
| release_bundle_v2_promotion | selectedEnvironments | 必须选择目标环境（「You must choose the environments」） |
| distribution / destination | bundle 名/pattern + Edge 节点 | |
| curation | 无 criteria | 实例内全部 curation 事件 |
| user | 无 criteria | 每次账户锁定都触发 |
| xray_scan_status | 终态集合 | done/failed/partial/not_supported 多选 |
| app_trust | projectKey（精确匹配）、applicationKeys、stages（in-list） | **设了 `stages` 则 stage 无关的 application_* 事件不投递**（官方警示，要收需另建无 stages 的订阅） |

### 2.3 Handler 两型（oneOf，判别字段 `handler_type`）

**预定义型 `"webhook"`**：`url`（uri，必填）、`proxy`（self-hosted 代理 key，**不是 URL**；云版省略）、`secret`（write-only）、`use_secret_for_signing`（bool，**default false**）、`custom_http_headers`（`{name,value}[]`）。

**自定义型 `"custom-webhook"`**：`url`、`proxy`、`method`（`POST|PUT|GET|PATCH|DELETE`，default POST）、`payload`（**含 Go `{{.path}}` 占位符的 JSON 模板字符串**，可引用 `{{ .userContext.id }}` 等）、`http_headers`、`secrets`（命名 secret，name pattern `^[a-zA-Z_][a-zA-Z0-9_]*$`；模板内以 `{{.secrets.<name>}}` 注入）。

### 2.4 回显与更新语义

- **回显（WebhookSubscription）**：`key/project_key/description/enabled/event_filter/handlers/debug`。handler 回显中 `secret` 为「Encrypted platform ciphertext when present; **copy unchanged on update unless rotating**」；custom 型 `secrets` **只回名字不回值**（「values are never returned」）。
- **更新（WebhookSubscriptionUpdate）**：**key 不可改**（不在 schema；仅路径标识）；`project_key`「Must match stored value exactly; cannot be changed.」；必填 `enabled/event_filter/handlers`。`NamedSecretUpdate.value` 三态：「**Omit to preserve; set to add/rotate; empty string wipes.**」custom secrets PUT 为**全量列表**：「omit a name to remove that secret.」

## 3. 事件类型全表（13 域 66 型；置信度：高——官方事件页逐字，updatedAt 2026-07-22）

覆盖界图例：**本体** = BinFlow 有触发源（M13 织入）；**休眠** = 注册休眠（Q6 暂行——可订阅、无触发源）；括号内为休眠原因。

### 3.1 artifact（5）

| event_type | 触发点 | data 字段 | 覆盖界 |
|---|---|---|---|
| `deployed` | 制品部署到仓库 | repo_key, path, name, sha256, size | **本体**（PUT 部署链） |
| `deleted` | 制品从仓库删除 | 同上 | **本体**（统一删除 seam） |
| `moved` | 制品 move 到另一仓库（**按源仓库匹配 criteria**） | + source_repo_path, target_repo_path | **本体**（M12 T-339 `/api/move`） |
| `copied` | 制品 copy（**按源仓库匹配**） | + source_repo_path, target_repo_path | **本体**（M12 T-339 `/api/copy`） |
| `cached` | remote 仓缓存未命中拉取落缓存（官方例：`npm install busybox`；pull replication 也可触发） | 同 deployed | **本体**（remote 回源落盘链） |

### 3.2 artifact_property（2）

| event_type | 触发点 | data 字段 | 覆盖界 |
|---|---|---|---|
| `added` | 属性加到制品/文件夹/仓库本体 | deployed 字段 + property_key, property_values[] | **本体**（properties PUT） |
| `deleted` | 属性删除 | 同上 | **本体**（properties DELETE） |

### 3.3 docker（3）

| event_type | 触发点 | data 字段 | 覆盖界 |
|---|---|---|---|
| `pushed` | Docker/OCI 新 tag push | repo_key, path, name, sha256, size, image_name, tag, platforms[{architecture,os}], image_type(oci/docker) | **本体**（/v2 push 链，dind 腿） |
| `deleted` | tag 删除 | 同上 | **本体**（registry 按 digest/tag 删除） |
| `promoted` | tag promote（**按源仓库匹配**） | 同上 + target_repo, target_tag | **休眠**（docker promotion REST `/api/docker/{repo}/v2/promote` 属 Build-info 邻域，PRD 裁 M14+；不伪造触发） |

### 3.4 build（3，休眠——Build-info 域 M14+）

`uploaded` / `deleted` / `promoted`：data = build_name, build_number, build_started（`1970-01-01T00:00:00.000+0000` 格式）, build_repo（示例值 `artifactory-build-info`）。

### 3.5 release_bundle（3，休眠——RBv1 域不建）

`created` / `signed` / `deleted`：data = release_bundle_name, release_bundle_version, release_bundle_size。

### 3.6 release_bundle_v2（3，休眠）

`release_bundle_v2_started` / `release_bundle_v2_failed` / `release_bundle_v2_completed`：data = repository_key, release_bundle_name, release_bundle_version, created。

### 3.7 release_bundle_v2_promotion（3，休眠）

`release_bundle_v2_promotion_started` / `_failed` / `_completed`：上表 + environment, created_millis。

### 3.8 distribution（7，休眠——Distribution 本体外部产品）

`distribute_started` / `distribute_completed` / `distribute_aborted` / `distribute_failed` / `delete_started` / `delete_completed` / `delete_failed`：data = release_bundle_name, release_bundle_version, release_bundle_size, edge_node_info_list[{edge_node_name, edge_node_address}], status_message, transaction_id。
**命名注意（文档瑕疵如实登记）**：节标题写 `deletion_started/completed/failed`，载荷 `event_type` 实为 `delete_*`——以载荷为准（置信度：标题低/载荷高）。

### 3.9 destination（4，休眠——Edge 节点本体不建；官方标注需 Artifactory 7.15.1+）

`received` / `delete_started` / `delete_completed` / `delete_failed`：data = release_bundle_name, release_bundle_version, status_message。

### 3.10 curation（4，休眠——Curation 本体不建）

事件型（官方以标题而非统一 snake_case 名登记，**载荷无 `{domain,event_type,data}` 包裹、字段平铺**——登记为文档形态，置信度中）：`Package was blocked by Curation`（package_type/package_name/package_version/package_url/reason/curated_*/origin_*/public_repo_*/policies[]/event_id）、`Curation Waiver Request Created`、`Curation Waiver Request Updated`（waiver_request 嵌套 + decision/decided_policies/pending_policies）、`Curation Policy Changed`（curation_event_type: "Policy Updated" + policy_before/policy_after）。

### 3.11 user（1，休眠——BinFlow 无失败登录锁定本体）

`locked`：达到失败登录次数阈值后账户锁定。data = admin, disable_ui_access, email, groups[], internal_password_disabled, last_logged_in, profile_updatable, realm, status("locked"), username。（若 M14+ 引入锁定策略，此型升本体。）

### 3.12 xray_scan_status（4，休眠——Xray 本体 PRODUCT Non-goal）

`done` / `failed` / `partial` / `not_supported`：event_type 镜像终态 `overall.status`；data = resource{type,repo,path,name…}、overall{status,updated_at,error?}、details{sca,violations,exposures,contextual_analysis 各 {status,updated_at,error?,categories?}}、occurred_at（RFC3339）。官方注意点：同一资源终态后仍可能多发（扫描支柱迟更新），「Treat payloads as notifications and deduplicate in your consumer」。

### 3.13 app_trust（24，休眠——AppTrust 外部产品）

- 门评估 6：`entry_gate_evaluation_started` / `entry_gate_evaluation_validation_passed` / `entry_gate_evaluation_validation_failed` / `exit_gate_evaluation_started` / `exit_gate_evaluation_validation_passed` / `exit_gate_evaluation_validation_failed`（data: application_key, application_version, stage, evaluation_id；passed/failed 另有 decision, explanation）。
- application 9：`application_creation_{started,completed,failed}` / `application_update_{…}` / `application_deletion_{…}`（data: application_key, project_key, display_name, description, criticality, maturity_level, labels[{key,value}], owners[{name,type}], timestamp 毫秒）。
- version 9：`version_creation_{started,completed,failed}` / `version_promotion_{…}` / `release_{started,completed,failed}`（data: application_version, application_key；promotion/release 另有 stage）。

## 4. 出站 envelope 与 userContext（置信度：高，官方示例逐字）

预定义型投递 HTTP POST，body 为单 JSON 对象：

```json
{
  "domain": "artifact",
  "event_type": "deployed",
  "data": { /* 域相关，见 §3 */ },
  "subscription_key": "<订阅 key>",
  "jpd_origin": "https://<your_origin>",
  "source": "jfrog/<your_source>",
  "userContext": { "id": "johndoe", "isToken": false, "realm": "internal" }
}
```

- `userContext`（官方字段表逐字）：`id`——触发用户名；token 认证时为 token subject（例 `jffe@<platform-id>/users/admin`）。`isToken`——token 触发为 true。`realm`——认证域（`internal`/`ldap`）。custom 型模板可用 `{{ .userContext.id }}` 点语法引用。
- `source` 实形态（排障页真实样例，补充文档示例）：`jfrog/jfrt@01h9mspg5atsp402qqxm071zjv`（`jfrog/<service>@<node-id>`）；事件 `id` 为 ULID（如 `01HA4MPK6XVKTD0XMS9SVM1GER`，仅出现在排障记录的 event 对象，**不在投递 body 内**）。
- **字段集偏差（低置信，待动态验证）**：排障页捕获的真实 request.payload 仅 5 字段（无 `jpd_origin`/`userContext`），文档示例为 7 字段。BinFlow 取文档全集（7 字段）；验收断言以 webhook.md 本条为准。
- **artifact 域载荷无事件时间戳字段**（域内 data 无 time；仅 build 有 build_started、xray 有 occurred_at、app_trust 有 timestamp）——接收端如需时间以到达时刻为准。**不要发明字段**。

## 5. 投递语义（置信度：高——system.yaml 官方模板逐字，默认值即官方默认）

投递由 **Event 微服务异步分发**（官方 Webhooks 页「How does it work?」）。参数挂 `event:` 段（Artifactory System YAML）：

### 5.1 限流（`event.rateLimit`）

| 键 | 默认 | 官方语义 |
|---|---|---|
| `frequency` | `1000.0` | 每秒可发往 webhook 目标的**平均**事件数 |
| `burstSize` | `10000` | 可瞬时突发的最大事件数；到阈值后按 frequency 匀速 |
| `maxConcurrentHandlers` | `50000` | 并发上限；**超限的新事件被拒绝**（rejected） |

### 5.2 预定义型投递（`event.webhooks`）与自定义型（`event.customWebhooks`）

| 键 | 默认 | 官方语义 |
|---|---|---|
| `timeoutMillis` | `30000` | 单请求时限；**含建连、重定向、读响应体**；计时在请求返回后仍继续直至 body 读完；0 = 无超时 |
| `maxIdleConnections` | `100` | 全主机 keep-alive 空闲连接上限（0 无限） |
| `maxIdleConnectionsPerHost` | `100` | 每主机空闲连接上限（0 = 系统默认 2） |
| `tlsInsecure` | `false` | true = 跳过证书链/主机名校验（官方自注仅供测试） |
| `retryCount` | `5` | 「Number of retry that will be done when event service is unable to send the request or when it receives an error (**>= 500**) from the server. **The first try count as one**」→ 首次计入，共 5 次尝试 = 初次 + 4 重试；**4xx 不重试**（重试条件仅发送失败或 ≥500）（首试计数的解释置信度中，数值高） |
| `retryWaitMillis` | `10000` | 两次重试间等待——**固定间隔，非指数退避**（官方无 backoff 曲线） |
| `sizeLimits.headersMaxCount` | `50` | 自定义头数量上限 |
| `sizeLimits.headersMaxSizeBytes` | `5120` | 头总字节上限 |
| `sizeLimits.secretsMaxCount` / `secretsMaxSizeBytes`（仅 custom） | `50` / `5120` | 命名 secret 数量/字节上限 |
| `sizeLimits.payloadMaxSizeBytes`（仅 custom） | `51200` | 模板 payload 上限（50KB） |

### 5.3 重试后终态（死信）

官方文档**未定义** retryCount 耗尽后的持久死信队列；可观测出口是排障面（§7：失败即记录，`debug:true` 成功也记录）+ event-metrics.log。「死信可查」的 BinFlow 形态归 ADR-0041（K50），本规格钉住官方可观察行为：**重试仅针对发送失败/HTTP≥500；间隔固定 10s；次数 5；之后事件弃投且仅在排障记录留痕**。（置信度：高）

### 5.4 SSRF 防护（`event.security.blacklist`）

| 键 | 默认 | 官方语义 |
|---|---|---|
| `security.blacklist.enabled` | `true` | 「When true **private networks (loopback, RFC1918, RFC3927, and IPV6 unique local addresses)** will not be allowed as webhook targets (used to prevent probing the network using SSRF)」 |

**文档矛盾登记**：Predefined Webhooks 页写「change the URL strict policy configuration in the `system.yaml` file: `urlStrictPolicy: true`」——`urlStrictPolicy` 键**不存在**于官方 system.yaml 模板（全文检索无命中），且「设 true 以放行私网」与默认禁止的语义矛盾。以 system.yaml 的 `event.security.blacklist.enabled` 为准（高）；`urlStrictPolicy` 判为文档陈旧残留（低，待验证）。BinFlow 旋钮按 system.yaml 键对齐（K50/ADR-0041）。

### 5.5 代理

预定义页「Use Proxy」下拉取自已配置代理服务器列表，**仅 self-hosted 适用**；REST `handlers[].proxy` 值为**代理 key 非 URL**，云版省略。

### 5.6 投递顺序与语义保证

官方文档**未承诺**顺序性与 exactly-once。可推定 at-least-once（重试可致重复；xray 域官方明文要求消费端去重）。BinFlow 不应假设顺序。（置信度：中——由重试机制推定，无逐字承诺）

## 6. 签名与 secret（X-JFrog-Event-Auth / HMAC-SHA256）

- 头名（官方逐字）：**`X-JFrog-Event-Auth`** HTTP header。
- 验签命令（官方「Verify Webhook Payload」逐字）：

  ```
  echo -n '<actual payload>' | openssl sha256 -hmac "<secret>"
  ```

  「For a valid payload, the result must be the same on both sides.」→ 签名 = **HMAC-SHA256 hex 摘要**（openssl sha256 输出形态）。
- 双态语义（官方措辞拼合）：`use_secret_for_signing`（default false）——false 时「the secret is passed through the `X-JFrog-Event-Auth` HTTP header」（**明文 secret 直传**）；true 时 secret 用于签载荷（secret 本体不随事件传）。**签名态下摘要的承载头官方未逐字写明**——按文档仅有的认证头推断仍为 `X-JFrog-Event-Auth`（置信度：中，待动态验证：建订阅 → 抓请求头比对）。
- 命名 secret（custom 型）：值加密存储；模板 `{{.secrets.<name>}}` 注入 headers/payload；回显只出名不出值（§2.4）。
- BinFlow 对齐（K49）：算法 HMAC-SHA256、验签命令逐字照抄官方（openssl 兼容形态）；头名 `X-JFrog-Event-Auth`；`use_secret_for_signing` 双态照录；secret 存储走 BinFlow AES-GCM 链（PRD 115.3）。

## 7. 排障面（死信可查的官方形态；置信度：高）

- 存储（system.yaml `event.troubleshooting`）：`enabled: false` 默认；`storageType: log | redis`。log 落 `JFROG_HOME/var/log/event-troubleshooting.log`（只记失败；可配轮转 compress/keepLastDecompressed/maxSizeMb 25/maxAgeDays 365/maxFiles 10）。redis 需同网 Redis **7.2**（`shared.cache.url: redis://…`），**streamMaxLen 10000 / cleanupIntervalMillis 30000**（超量每 30s 清老）。UI Troubleshooting 页与 REST 仅 Redis 形态可用。
- 记录 schema（REST #7 响应与日志同构）：`timestamp`（处理开始，UNIX ms）、`elapsed_millis`、`errors[]`、`request{method,url,headers,payload,retries_attempted}`、`response{status,headers,body}`、`event{id(ULID),subscription_key,domain,event_type,data,source}`。`retries_attempted` 为重试可观察点。
- 订阅级 `debug:true` = 成功也记录（默认只记失败）。

## 8. Q4 档位取证与终裁建议（置信度：高）

官方 Feature Comparison Matrix（updatedAt 2026-05-29）「Security and Authentication」表逐字：

> | [Webhooks](/integrations/docs/webhooks), vulnerability scanning, open source license checks | ❌ Not included | ✅ Included | ✅ Included | ✅ Included |

列序：**Non-commercial（JCR/OSS/CE C/C++）| Pro X | Enterprise X | Enterprise+**。

**结论**：webhook 属**商业订阅特性**——非商业（社区等价物）不含，**Pro X 起包含**。当前官方商业梯子最低档即 Pro X（矩阵无单独 Pro 列）。

**终裁建议**：维持 PRD 暂行 **pro+ 解锁、community 锁定、kind=feature-int**——与官方档位边界一致，无需翻转 unlocked。kind（feature-int vs 别的）是 BinFlow ADR-0032/33 内部分类裁量，官方文档只钉「社区不含」这一边界。三缝语义照 FR-85 机制：community 建订阅 403 + `X-Binflow-License-Required: webhook` → pro 200 → 卸载降级。

## 9. 反编译锚点补白（「此条补充官方规范」，置信度按 inv-4 原判）

- **I1（高）**：内部统一事件闭集 36 枚举 + CloudEvents 风格 envelope（specVersion/id/type/source/time/subject/dataContentType/headers）。→ BinFlow 内部事件信封（ADR-0041）可采 CloudEvents 风格；**对用户出站 wire 仍按 §4 平铺 JSON**，两层解耦。
- **I2（中）**：webhook 管理面历史上在 Access 侧（Artifactory rest 树无 webhook resource；`internal:webhook` 权限常量）——与现行官方 `/event/api/v1/subscriptions` 挂 Event 服务不矛盾（平台演进：Access → Event 服务）。legacy 路径 `/access/api/v1/system/webhooks*`（Terraform provider 等生态仍在用）**中置信**，BinFlow 不实现，规格仅登记。
- **I3（中）**：内部 outbound 分发器（HTTP POST JSON / client 回调）与 jfbus 异步——outbox/队列形态归 ADR-0041。
- **I4（中）**：内部 consumer 注册 REST `/api/v1/unifiedevent/registration`（POST/GET/DELETE）——内部面，非用户订阅面（§0）。
- **K3（高）**：事务 outbox 六方言 DDL（derby/mariadb/mssql/mysql/oracle/postgresql）——BinFlow outbox 持久化（K50）的可靠性模式佐证；BinFlow 仅 PG/SQLite 两方言即可。

## 10. 官方文档已知瑕疵登记（实现时以何种读法为准）

1. docker 域三个示例载荷字段**值**粘连（如 `"path": "sample_dirsample-image/1.0.0/sample.txtmanifest.json"`、`"size": 0120`）——两个文档变体合并残留。**字段名与结构为准，值忽略**。
2. distribution 域 `deletion_*` 标题 vs `delete_*` 载荷（§3.8）——**以载荷 event_type 为准**。
3. curation 载荷无统一 envelope 包裹（§3.10）——按文档登记；BinFlow 休眠不触发，不影响 wire。
4. RBv2 示例 `created` 值尾带转义引号、`created_millis` 值带前导冒号（`":1665405448941"`）——文档排版残留，语义为 ISO8601 / 毫秒 epoch。
5. `urlStrictPolicy` 陈旧键名（§5.4）——以 `event.security.blacklist.enabled` 为准。

## 11. 消费面参考（T-247 dogfood 栈 / T-362 要点）

- **T-247 栈在案可用**（reports/agents/T-247.md）：VM 172.16.58.129，BinFlow systemd :8080 + Jenkins :9090，五仓（maven/npm/docker local+remote+virtual）与 ci-bot 凭据就位。webhook 验收腿可直接以 Jenkins 为真实消费者：订阅 `artifact/deployed`（repoKeys=maven-local）→ 触发 t247-mvn-publish 类 job → Jenkins 接收器（如 generic webhook trigger 或简易 HTTP listener）断言 envelope。JFrog 生态先例：官方 Webhooks 页示例即「build promotion → HTTP POST → 触发 Jenkins CI build」。
- **T-362（消费 e2e）要点**：① 接收器需断言 §4 七字段（含 userContext 三子字段——用 ci-bot token 触发则 isToken=true、id 为 token subject 形态）；② HMAC 腿用 §6 openssl 命令逐字验签；③ 故障注入（500/超时）断言 §5.2 重试节奏（10s 间隔 ×4）与 `retries_attempted` 计数（§7）；④ 4xx 不重试是官方暗含行为，值得负面腿钉住；⑤ 私网目标默认被拒（§5.4）——接收器若在同 VM 私网，需按 ADR-0041 的开关语义放行后才能跑通（这条同时就是 SSRF 腿的验收）。
- 真实客户端准绳：验收不许只测 happy path——至少覆盖 401/403/400（key 正则违例）/404（update/delete 不存在 key）四错误臂（§1 表）。

## 12. 待验证清单（低置信汇总）

| # | 项 | 验证途径 |
|---|---|---|
| V1 | `use_secret_for_signing=true` 时签名摘要承载头是否仍为 `X-JFrog-Event-Auth`（及编码形态 hex/base64） | 活体 Artifactory（t226 容器可恢复）建订阅抓包 |
| V2 | artifact 域真实投递 body 是否含 `jpd_origin`/`userContext`（排障样例 5 字段 vs 文档 7 字段） | 同上 |
| V3 | `retryCount`「首试计入」解释（5=总次数 or 5=重试数） | 活体故障注入数 retries_attempted 上限 |
| V4 | 4xx 是否确不重试（官方仅写 ≥500 重试） | 活体注入 404 接收器 |
| V5 | `urlStrictPolicy` 是否为有效遗留键（判陈旧残留） | 活体 system.yaml 试设 |
| V6 | legacy `/access/api/v1/system/webhooks*` 与现行 `/event/api/v1/subscriptions` 并存关系 | 活体双路径探测（仅登记用，BinFlow 不实现） |

## 13. 取证锚点（全部 2026-08-30 实取）

- 事件目录（13 域 66 型 + event_filter + userContext）：`https://docs.jfrog.com/integrations/docs/webhook-event-types`（updatedAt 2026-07-22）
- 预定义 webhook 创建/签名/SSRF/代理：`https://docs.jfrog.com/integrations/docs/predefined-webhooks`（updatedAt 2026-05-28）
- Webhooks 总览（Event 服务异步分发/两类 webhook）：`https://docs.jfrog.com/integrations/docs/webhooks`
- REST OpenAPI 七端点：`https://docs.jfrog.com/integrations/reference/{getwebhooksubscriptions,createwebhooksubscription,getwebhooksubscription,updatewebhooksubscription,deletewebhooksubscription,testwebhooksubscription,streamwebhooktroubleshooting}.md`（updatedAt 2026-07-01）
- 排障面：`https://docs.jfrog.com/integrations/docs/webhooks-troubleshooting`（updatedAt 2026-07-14）
- 投递参数/SSRF 键：`https://docs.jfrog.com/installation/docs/artifactory-system-yaml`（`event:` 段）
- Q4 档位：`https://docs.jfrog.com/installation/docs/feature-comparison-matrix-for-self-mangaged-jpds`（updatedAt 2026-05-29）
- 内部锚点：docs/reverse/inv-4-addons.md §I（I1~I5）/§K3；docs/reverse/artifactory-full-feature-matrix.md L186/L365
