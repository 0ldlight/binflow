---
title: Webhook 使用指南（出站事件通知）
sidebar_position: 52
---

# Webhook 使用指南（出站事件通知）

> 适用版本：M13（T-362/T-364/T-366；**pro 及以上档**——feature 槽 `webhook`，见 [License 与 Add-ons 管理](license.md)；行为基准 `docs/reverse/webhook.md`〔官方文档逐字锚〕+ ADR-0041）。
> 本文全部 curl 命令与响应在 HEAD 构建的 scratch 实例上实测（双实例 + 脚本接收器，2026-08-30；接收端 HMAC 验签示例代码亦经真实投递验证）。

## 用途

BinFlow 里发生的制品事件可以**主动 POST 到你的 HTTP 端点**——触发 CI、通知聊天机器人、同步内部台账，不必轮询。一条订阅 = 「哪类事件 × 什么范围 × 投到哪里（含签名密钥）」。

**事件域与触发覆盖（如实登记）**：事件型闭集 13 域 66 型（与 Artifactory 官方目录逐字对齐），其中 **9 型有真实触发源**（下表），其余 57 型**可订阅、校验通过、但当前永不触发**（对应本体——build-info、release bundle、Xray 扫描等——不在 BinFlow 产品范围）；控制台订阅表单里休眠型如实灰显标注。

**已织入触发的 9 型**：

| 域 | event_type | 触发点 | data 关键字段 |
|---|---|---|---|
| artifact | `deployed` | 制品部署（PUT 落地） | repo_key / path / name / sha256 / size |
| artifact | `deleted` | 制品删除 | 同上 |
| artifact | `moved` / `copied` | `/api/move`、`/api/copy` 逐文件（**按源仓库匹配**） | + source_repo_path / target_repo_path |
| artifact | `cached` | remote 仓回源落缓存（仅 MISS 臂） | 同 deployed |
| artifact_property | `added` / `deleted` | 属性 PUT/DELETE（逐键发） | + property_key / property_values[] |
| docker | `pushed` | 新 tag push（manifest 层；layer 不发） | + image_name / tag / image_type(oci\|docker) / platforms |
| docker | `deleted` | manifest 按 digest 删除 | 同上（tag 不发明字段） |

## 前置条件

- **pro 及以上 license**（槽 `webhook`）：community 实例的写动词答 **403 + `X-Binflow-License-Required: webhook`**；读面（列表/排障）不设门，readonly_admin 可见。卸载/过期 license 后事件**不入箱**（触发点静默跳过，PUT 照常 200）。
- 订阅携带 secret 时，实例必须配置主密钥 `BINFLOW_REMOTE_CREDENTIALS_KEY`（base64 的 32 字节；secret 以 enc:v1 密封落库）——无主密钥时带 secret 的写请求 400。
- 管理面门：**写 = admin**（system:write），**读 = admin / readonly_admin**（system:read）。
- 接收端可以是任何能收 POST 的 HTTP 服务；注意下文 **SSRF 边界**（私网/回环目标默认被拒）。

## 订阅模型（wire schema）

POST/PUT 体一形（`WebhookSubscriptionCreate/Update`）：

```json
{
  "key": "deployed-notify",
  "description": "可选说明",
  "enabled": true,
  "event_filter": {
    "domain": "artifact",
    "event_types": ["deployed"],
    "criteria": { "anyLocal": true, "includePatterns": ["**/*.jar"] }
  },
  "handlers": [{
    "handler_type": "webhook",
    "url": "http://receiver.example.com/hook",
    "secret": "<secret 明文，仅写请求出现>",
    "use_secret_for_signing": true
  }],
  "debug": false
}
```

| 字段 | 约束 |
|---|---|
| `key` | `^[A-Za-z][A-Za-z0-9_-]+$`，≤500 字符；**创建后不可改**（PUT 携带不同 key → 400） |
| `enabled` | **默认 false**——建完订阅不会触发任何投递，显式置 true 才生效 |
| `event_filter.domain` / `event_types` | 13 域 66 型闭集（跨域同名型按 (domain, event_type) 对解析）；未知值 400 |
| `event_filter.criteria` | **strict 解析**：未知键 400（不静默丢弃） |
| `handlers` | **恰好 1 个**（minItems=maxItems=1） |
| `debug` | true = 成功投递也记录排障数据（默认只记失败） |

**criteria（artifact / artifact_property / docker 三域取用）**：

| 维度 | 语义 |
|---|---|
| `anyLocal` / `anyRemote` | 全部 local / remote 仓（**对未来新建的仓同样生效**） |
| `repoKeys` | 点名仓清单（与 any* 取并集） |
| `includePatterns` / `excludePatterns` | Ant 风格通配（`**` / `*`），作用于仓内路径；**exclude 优先** |
| （组合规则） | **空选择不命中任何事件**——订阅必须声明范围；控制台表单对空范围就地警示 |

**handler 两型**（`handler_type` 判别）：

- `"webhook"`（预定义）：`url`（http/https，≤2048，拒绝 userinfo/fragment）、`secret`（write-only）、`use_secret_for_signing`（默认 false）、`custom_http_headers`（`[{name,value}]`，≤50 个、总 ≤5KB）。
- `"custom-webhook"`（自定义载荷）：加 `method`（默认 POST）、`payload`（Go 模板字符串，≤50KB，可用 `{{.path}}`、`{{.userContext.id}}`）、`http_headers`、`secrets`（命名 secret，`{{.secrets.<name>}}` 注入；回显只出名不出值；PUT 为**全量列表**——漏一个名字即删除它）。

**secret 三态**（PUT 更新时）：省略键 = 保持；新明文 = 轮换（对在途投递即时生效——投递时现读）；`""` = 擦除。回显恒为掩码 `********`，**回传掩码原串会被拒绝**（防把掩码当真值存回去）。

## 订阅 CRUD：七端点

基座：`/binflow/event/api/v1`（官方 Event 服务段名逐字；注意**不在** `/binflow/api` 下）。认证 Basic 或 Bearer token（同管理面）。

| 方法 | 路径 | 成功 | 说明 |
|---|---|---|---|
| GET | `/event/api/v1/subscriptions` | 200 bare array | 列表 |
| POST | `/event/api/v1/subscriptions` | **201** | 创建（回显 SubscriptionView） |
| GET | `/event/api/v1/subscriptions/{key}` | 200 / 404 `Subscription not found` | 单查 |
| PUT | `/event/api/v1/subscriptions/{key}` | **204 无体** | 全量更新 |
| DELETE | `/event/api/v1/subscriptions/{key}` | **204** | 删除（**级联删除其全部投递行**——排障环记录保留） |
| POST | `/event/api/v1/subscriptions/test` | 200 TestOutcome | **试发草稿**：吃完整订阅体（非 key 引用），同步单次直发、不入箱不重试 |
| GET | `/event/api/v1/troubleshooting` | 200 | 排障记录（见下文） |

```bash
export BASE=http://localhost:8080 ADMIN_PW=<管理员口令>

# 列表（空实例）
curl -su admin:$ADMIN_PW $BASE/binflow/event/api/v1/subscriptions
# []

# 创建（签名态订阅）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/event/api/v1/subscriptions \
  -H 'Content-Type: application/json' -d '{
  "key": "deployed-notify", "enabled": true,
  "event_filter": {"domain": "artifact", "event_types": ["deployed"],
    "criteria": {"anyLocal": true, "includePatterns": ["**/*.jar"]}},
  "handlers": [{"handler_type": "webhook", "url": "http://receiver:9000/hook",
    "secret": "s3cr3t-hmac-key", "use_secret_for_signing": true}]
}' -o /dev/null -w '%{http_code}\n'          # 201

# 单查（criteria 以规范化全维度回显；secret 恒为掩码）
curl -su admin:$ADMIN_PW $BASE/binflow/event/api/v1/subscriptions/deployed-notify

# 更新（PUT 全量体；secret 省略 = 保持）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/event/api/v1/subscriptions/deployed-notify \
  -H 'Content-Type: application/json' -d '{...}' -o /dev/null -w '%{http_code}\n'   # 204

# 删除
curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/event/api/v1/subscriptions/deployed-notify \
  -o /dev/null -w '%{http_code}\n'            # 204；再删同 key → 404 Subscription not found
```

**试发（test）——配置落地前先验证连通与签名**：

```bash
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/event/api/v1/subscriptions/test \
  -H 'Content-Type: application/json' -d '{
  "key": "draft-probe", "enabled": false,
  "event_filter": {"domain": "artifact", "event_types": ["deployed"],
    "criteria": {"repoKeys": ["generic-local"]}},
  "handlers": [{"handler_type": "webhook", "url": "http://receiver:9000/hook",
    "secret": "plain-secret"}]
}'
# 200（注意：试发失败也是 200，看 body 的 ok）
# {"message":"Test successful","ok":true,
#  "attempt":{"status_code":200,"elapsed_millis":1}}

# 接收端 404 时（4xx 是终态语义的预告）：
# {"message":"Test attempt failed: receiver answered 404","ok":false,
#  "attempt":{"status_code":404,"elapsed_millis":1,"error":"receiver answered 404"}}
```

控制台等价面：**治理 → Webhooks**（`/admin/governance/webhooks`）——列表/新建/编辑（Dialog）、详情与最近投递记录（Drawer）、行内启停与试发；readonly_admin 只读。

## 事件信封（接收端看到什么）

预定义型投递为单次 HTTP POST，body 是 JSON 对象（**七字段全集**）：

```json
{
  "domain": "artifact",
  "event_type": "deployed",
  "data": {"name": "app.jar", "path": "com/acme/app.jar", "repo_key": "generic-local",
           "sha256": "2772b96d…", "size": 10},
  "subscription_key": "deployed-notify",
  "jpd_origin": "http://127.0.0.1:18412",
  "source": "binflow/binflow@108958GA6DTS6JP7GADYTV99B2",
  "userContext": {"id": "admin", "isToken": false, "realm": "internal"}
}
```

- `jpd_origin` = 实例 `server.base_url`（未配置则从请求推导）；`source` 为 BinFlow 拼法 `binflow/binflow@<26 位 ULID>`（节点 id）。
- `userContext.id` 是触发者；token 触发时 `isToken: true`；`realm` 为认证域（`internal`/`ldap`）。
- **artifact 域载荷无时间戳字段**（官方同形）——接收端需要时间就以到达时刻为准，不要等不存在的字段。

## 签名与接收端义务

**两种 secret 用法**（`use_secret_for_signing`）：

| 模式 | `X-JFrog-Event-Auth` 头的值 | 适用 |
|---|---|---|
| `false`（默认） | **secret 明文直传**（官方默认臂） | 接收端在可信网络内直接比对 |
| `true` | **HMAC-SHA256(secret, body) 的 hex 摘要** | 公网接收端验签（secret 不上线） |

**手工验签**（openssl，官方「Verify Webhook Payload」同形）：

```bash
# 对收到的原始字节（不是重新序列化的 JSON）：
printf '%s' "$(cat payload.json)" | openssl dgst -sha256 -hmac "s3cr3t-hmac-key"
# SHA2-256(stdin)= 82248438b9b3054a764606c5ed812e21b6b75f38cc91b39434a98318ecd51ba6
#   ← 与请求头 X-JFrog-Event-Auth 逐字一致即验签通过（实测对账）
```

**接收端示例（Python 标准库，含验签 + 去重义务）**——本文档该片段经真实投递验证（输出 `VERIFIED artifact/deployed -> deployed-notify`）：

```python
import hashlib, hmac, http.server, json

SECRET = b"s3cr3t-hmac-key"

class Handler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        want = hmac.new(SECRET, body, hashlib.sha256).hexdigest()
        got = self.headers.get("X-JFrog-Event-Auth", "")
        if not hmac.compare_digest(got, want):
            self.send_response(401)          # 验签失败：拒绝，勿处理
            self.send_header("Content-Length", "0"); self.end_headers(); return
        env = json.loads(body)
        # ⚠ 幂等义务：投递是 at-least-once（重试可致重复），以
        #   (subscription_key, data.sha256, event_type) 之类的稳定键去重后再入业务。
        handle_event(env)                    # 你的业务逻辑
        self.send_response(200)              # 2xx = 送达；4xx 立即终态，≥500 触发重试
        self.send_header("Content-Length", "0"); self.end_headers()

    def log_message(self, *a): pass

http.server.HTTPServer(("0.0.0.0", 9000), Handler).serve_forever()
```

接收端三件事：**验签**（对原始字节）、**去重**（at-least-once，不保证顺序）、**快速返回 2xx**（30s 超时内完成；重活异步化，超时会被当发送失败重试）。

## 投递语义：重试、死信与可观测

**官方锚定值**（不可配置，引擎常量）：

| 参数 | 值 | 语义 |
|---|---|---|
| 尝试上限 | **5（首试计入）** | 共 5 次尝试 = 初次 + 4 重试 |
| 重试间隔 | **固定 10s**（无退避曲线） | 两次尝试间隔恒定 |
| 单次尝试超时 | **30s**（含建连/交换/读响应体） | 超时计为发送失败，占用一次尝试 |
| 重试条件 | **仅「发送失败」或「HTTP ≥ 500」** | **4xx 一步终态**（不重试）；3xx 不跟随、同判终态 |
| 限流 | 1000 次/s 均值 + 10000 突发 | 到阈值后匀速放行 |
| 并发上限 | 50000 | 超限的**新事件在入箱前被拒**（丢弃语义，指标可查） |

实测形态（脚本接收器注入故障）：`500,500 → 200` 的订阅第 3 次尝试送达，两个间隔各 ≥10s；恒 500 的订阅 5 次尝试后放弃——排障环留 5 条记录（`retries_attempted` 0..4），审计落 `webhook.dead_letter`（detail 含 attempts/error/status_code/subscription/url）。

**死信与重放（如实登记）**：重试耗尽的事件**不丢行**——outbox 行转 `dead` 状态可查；但**当前没有 REST 重放端点**（`Dispatcher.Replay` 机制在、未挂 REST/控制台面——登记为后续票）。恢复一条死信的现行办法：修正接收端后**重新触发源事件**（如同一路径重新 PUT；deployed 等事件天然可重放）。

**排障环**（`GET /event/api/v1/troubleshooting`）：

```bash
# 按订阅过滤；支持 target=/start=/end=/count=
curl -su admin:$ADMIN_PW "$BASE/binflow/event/api/v1/troubleshooting?subscription=dead-end"
```

- 记录含：`timestamp` / `elapsed_millis` / `errors[]` / `request{method,url,headers,payload,retries_attempted}` / `response{status,headers,body}` / `event{id,subscription_key,domain,event_type,data,source}`。
- **失败必录；`debug:true` 成功也录**。环容量 10000 条、每 30s 修剪——**进程内存储，重启失史**（要长期留存请在接收端落库）。
- 记录里的 `X-JFrog-Event-Auth` 头值已脱敏为 `********`（secret 不落记录）。

**Prometheus 指标**（`/metrics`，前缀 `binflow_webhook_`）：

| 指标 | 含义 |
|---|---|
| `deliveries_total` | 成功投递计数 |
| `retries_total` | 排程的重试计数 |
| `dead_letter_total` | 终态放弃计数 |
| `queue_depth`（gauge） | outbox 待投水位 |
| `enqueue_failures_total` | 入箱失败 + 并发上限拒绝（**事件丢失面**——告警盯它） |

**disable / delete 的边界语义**：`enabled=false` 后**新事件不入箱**，但已入箱的快照行仍会投出（停止投递的机制是删除）；删除订阅级联删光其投递行。

## SSRF 边界（目标地址防护）

订阅目标是 REST 动态写入的——任何能写订阅的人都能让实例向任意 URL 发 POST。因此**私网目标默认全拒**（loopback / RFC1918 / 链路本地 / IPv6 ULA）：

```bash
# 默认姿态（未开旋钮）——试发指向 127.0.0.1：
# {"message":"Test attempt failed: target rejected: upstream target 127.0.0.1:9000
#   rejected: loopback address 127.0.0.1","ok":false,
#  "attempt":{"status_code":0,"elapsed_millis":0,
#   "error":"target rejected: … rejected: loopback address 127.0.0.1"}}
```

接收端与 BinFlow 同机/同私网时，显式放行（**默认 false，与 replication 的默认 true 有意不对称**——那边的目标是运维静态配置的，这边是 REST 动态写入的）：

```yaml
# binflow.yaml（重启生效）
webhook:
  allow_private_target: true    # env: BINFLOW_WEBHOOK__ALLOW_PRIVATE_TARGET
```

放行后回环/私网目标可订阅（本文实测环境即此形态）；公网目标不受该旋钮影响、无需任何配置。

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 建订阅 403 + `X-Binflow-License-Required: webhook` | community 档 | 装 pro 及以上 license |
| 建订阅 400（key/域/事件型/criteria） | key 不合 pattern、未知事件型、criteria 带未知键 | 对照上文约束；criteria 只收声明的维度 |
| 带 secret 的写 400 | 实例未配 `BINFLOW_REMOTE_CREDENTIALS_KEY` | 配置主密钥后重启 |
| 事件不投递、也无报错 | `enabled` 忘了置 true（**默认 false**）；或 license 失效（事件静默不入箱）；或 criteria 空选择 | 核对订阅回显的 enabled/criteria；`GET /api/system/license` 查档位 |
| test 通、真实事件不到 | criteria 不匹配该路径（include/exclude）；或事件型属 57 休眠型 | 用 test 的合成事件先验证连通；对照 9 型触发表 |
| 试发 `target rejected: … loopback …` | SSRF 默认拒私网 | 公网目标，或 `webhook.allow_private_target: true` |
| 接收端总收到重复事件 | at-least-once 语义（重试/并发窗） | 接收端按稳定键去重（见接收端义务） |
| 事件收到但验签不过 | 验的是重新序列化的 JSON 而非原始字节；或 secret 轮换后新旧混用 | 对原始请求字节验签；轮换后短暂窗口内新旧 secret 都可能命中（投递时现读，即刻收敛） |
| 死信怎么查 | 排障环按订阅过滤 + 审计 `action=webhook.dead_letter` | 见上文；重放走重新触发源事件 |

## 下一步

- 事件触发的操作域细节（copy/move 的按源仓匹配、cached 的 MISS 臂）：[制品操作族](artifact-operations.md) · [remote / virtual 仓库管理](remote-virtual.md)
- 指标族与告警接线：[Prometheus 指标参考](../metrics/prometheus-reference.md)
- 档位与槽位语义：[License 与 Add-ons 管理](license.md)
