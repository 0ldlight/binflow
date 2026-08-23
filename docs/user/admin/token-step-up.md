---
title: Token 铸造二次认证（step-up）
sidebar_position: 45
---

# Token 铸造二次认证（step-up）

> 适用版本：M7（PRD milestone-7 v1.1 FR-68、ADR-0027 Accepted）。
> 本文全部命令在本机 scratch 实例（2026-08-23，`make build` 产物；Keycloak 26.2.5 + OpenLDAP 容器实腿）上复跑：本地/LDAP/OIDC 三腿三态、豁免臂（Basic/Bearer/admin session/docker token）、grant 单次消费/过期/重启丢失、TTL 越界拒启动（开关双态）、env 双拼写、审计维度均按预期。

## 这解决什么问题

web session cookie 本身就是完整的 bearer 证明。若浏览器 session 被盗（XSS / 离机未登出），攻击者可立即用该 cookie 铸一枚**最长 365 天**的 API Token——把小时级的劫持窗口（session TTL 24h 封顶）升级为季度级持久化立足点，且 IdP 侧停用用户只杀会话、不影响已铸 Token。

step-up 在 `POST /api/security/token` 上加一道**第二因子门**：session cookie 可用 ≠ 可铸 Token，铸造还需重验口令（本地/LDAP）或 IdP 新鲜重认证（OIDC）。**默认关闭**——不开开关，一切行为与 M6 逐字相同。

## 开关键（两把）

| 键 | 类型 | 默认 | 校验 |
|---|---|---|---|
| `auth.token_step_up` | bool | **false** | 不配置 = 全部行为不变（含既有脚本） |
| `auth.token_step_up_grant_ttl_seconds` | int | 300 | 域 **[60, 3600] 闭区间**，越界**无条件拒启动**（开关 off 也拒——严格 schema） |

```yaml
auth:
  token_step_up: true
  token_step_up_grant_ttl_seconds: 300   # OIDC mint grant 生存期，秒
```

环境变量双拼写（与全局 `BINFLOW_` 前缀规则一致，单下划线与 `__` 两形等价）：

```bash
BINFLOW_AUTH_TOKEN_STEP_UP=true                # 或 BINFLOW_AUTH__TOKEN_STEP_UP
BINFLOW_AUTH_TOKEN_STEP_UP_GRANT_TTL_SECONDS=300  # 或 BINFLOW_AUTH__TOKEN_STEP_UP_GRANT_TTL_SECONDS
```

越界实测（进程退出码 1，**不是静默回退默认值**）：

```text
token_step_up_grant_ttl_seconds: 5     → config: auth.token_step_up_grant_ttl_seconds must be within [60, 3600] seconds, got 5s
token_step_up: false + ttl: 3601       → config: auth.token_step_up_grant_ttl_seconds must be within [60, 3600] seconds, got 1h0m1s
```

## 作用域：谁触发、谁豁免

触发条件 = **web session cookie 认证** × **非 admin 角色** × **开关开启**，三者同时成立。判定按 `users.provider` 选腿：

| 认证臂 | 触发？ | 说明 |
|---|---|---|
| web session（本地用户） | **是** | body 携 `step_up_password`，本地 argon2 校验（与登录同一参数） |
| web session（LDAP 用户） | **是** | body 携 `step_up_password`，LDAP bind 重验（与登录同源） |
| web session（OIDC 用户） | **是** | body 携 `step_up_grant`（见下文 re-auth 流） |
| web session（**admin** 角色） | 否 | admin session 免——首因子已是全权 |
| Basic 认证 | 否 | CI/脚本主路径：**请求本身就携带口令**，首因子新鲜 |
| Bearer / API Token | 否 | 已持有长效凭据，再铸不提权 |
| docker `/v2/token` | 否 | 本就 Basic 支撑（docker login 流零影响） |
| 匿名 | 否 | 路由 401 挑战在 step-up 之前（`{"error":"invalid_request","error_description":"authentication required"}`） |

**CI 安全说明**：流水线用 Basic 或既有 Token 铸 Token 的自助面（M6 Q11 开放）**完全不受影响**——step-up 只作用于浏览器 session 臂。把控制台 cookie 混进 CI 的反模式（见 [FAQ](../faq.md)）不受本特性保护。

两点边界（重要）：

- **readonly_admin 不豁免**——豁免的是 admin 角色而非「管理员ish」：readonly_admin 的 session 自铸（限本人）同样要过 step-up（实测：无凭据 401 `step_up_required`，带口令 200）。
- 门位在主体/TTL 护栏**之前**：未过 step-up 时，请求不会泄露「为他人铸 403 / TTL 超帽 401」等护栏状态。step-up 通过后护栏照常生效——例如非 admin 请求 `expires_in=31622401`（>365d 帽）仍 401 `invalid_request`（文案见下文报错对照）。

## 本地与 LDAP 腿：`step_up_password`

铸造请求 body 增可选字段 `step_up_password`（form 与 JSON 双形态）。以本地用户 `dev1` 为例（`$BASE` 为实例地址，下同）：

```bash
export BASE=http://localhost:8080

# 1) 登录取 session cookie
curl -s -c cj-dev1.txt -X POST $BASE/binflow/api/v1/session \
  -H 'Content-Type: application/json' \
  -d '{"username":"dev1","password":"<dev1口令>"}' -o /dev/null -w '%{http_code}\n'   # 200

# 2) 无凭据铸 → 401 step_up_required
curl -s -b cj-dev1.txt -X POST $BASE/binflow/api/security/token \
  -d 'grant_type=client_credentials' -w '\nHTTP %{http_code}\n'
# {"error":"step_up_required","error_description":"step-up authentication required to mint a token"}
# HTTP 401

# 3) 错口令 → 401 step_up_invalid
curl -s -b cj-dev1.txt -X POST $BASE/binflow/api/security/token \
  -d 'grant_type=client_credentials&step_up_password=<错口令>' -w '\nHTTP %{http_code}\n'
# {"error":"step_up_invalid","error_description":"step-up credential rejected, expired, or already used"}
# HTTP 401

# 4) 对口令 → 200
curl -s -b cj-dev1.txt -X POST $BASE/binflow/api/security/token \
  -d 'grant_type=client_credentials&step_up_password=<dev1口令>'
# {"access_token":"<64hex>","token_type":"Bearer","expires_in":2592000,"scope":"api:*"}

# JSON 形态等价：
curl -s -b cj-dev1.txt -X POST $BASE/binflow/api/security/token \
  -H 'Content-Type: application/json' \
  -d '{"grant_type":"client_credentials","step_up_password":"<dev1口令>"}'
```

LDAP 腿同形：用户 `users.provider=ldap` 时，同一字段 `step_up_password` 走 **LDAP bind 重验**（对目录验原口令，与登录同一连接器）。目录停机等目录侧失败与口令错误同形态 401 `step_up_invalid`——对外不区分原因（与登录面同姿态），细节在服务端日志。

## OIDC 腿：`prompt=login` 重认证换 mint grant

OIDC 用户没有本地口令可重验——第二因子是**IdP 新鲜重认证**。流程四步：

```text
浏览器 session（已登录）
  │ 1. GET /api/v1/oidc/login?purpose=step_up
  │    → 302 到 IdP authorize URL，强制 prompt=login（OIDC Core 标准参数：
  │      IdP 必须重新问凭据，不吃 SSO 会话）——未知 purpose 值 400
  │ 2. 用户在 IdP 重新提交口令
  │ 3. callback：purpose=step_up 不建新 session；重认证主体必须与
  │    当前 session 同人（不同人 401）→ 签发 mint grant
  │    → 302 /binflow/ui/#step_up_grant=<64hex>（URL fragment 承载）
  ▼ 4. 携 grant 铸造：POST /api/security/token
       body: grant_type=client_credentials&step_up_grant=<grant>（同一 session cookie）
```

grant 契约：

| 属性 | 值 |
|---|---|
| 形态 | opaque 256-bit 随机串（64 位 hex），服务端只存 sha256 |
| 绑定 | `{username, session_id}`——换 session / 换人无效 |
| 消费 | **单次**（消费即删；复用、错绑定、过期一律烧掉，不给第二次机会） |
| TTL | `auth.token_step_up_grant_ttl_seconds`（默认 300s） |
| 台账 | 进程内存——**重启丢失**，用户重走一次 re-auth（实测：重启后同 grant → 401 `step_up_invalid`） |
| 回传 | redirect **fragment**（`#step_up_grant=...`）——fragment 不进服务端与反向代理日志 |

curl 全链（脚本形态；Keycloak 实测）：

```bash
A=$BASE/binflow; JAR=cj-sso.txt   # JAR 已持有 OIDC 登录 session

# ① login init 携 purpose=step_up → authorize URL（应含 prompt=login）
LOC=$(curl -s -b $JAR -c $JAR -o /dev/null -w '%{redirect_url}' \
  "$A/api/v1/oidc/login?purpose=step_up")
echo "$LOC" | grep -q 'prompt=login' && echo OK   # OK

# ② IdP 登录表单 → 提交凭据（prompt=login 强制出现表单，即使 IdP 有 SSO 会话）
ACTION=$(curl -s -b $JAR -c $JAR "$LOC" \
  | grep -o 'action="[^"]*"' | head -1 | sed 's/action="//;s/"$//' | sed 's/&amp;/\&/g')
CB=$(curl -s -b $JAR -c $JAR -o /dev/null -w '%{redirect_url}' -X POST "$ACTION" \
  --data-urlencode "username=<ssouser>" --data-urlencode "password=<ssopass>" \
  --data-urlencode "credentialId=")

# ③ callback → 302，Location 的 fragment 即 grant；不建新 session（无新 Set-Cookie）
LOCATION=$(curl -s -b $JAR -c $JAR -o /dev/null -D - "$CB" \
  | grep -i '^location:' | head -1 | tr -d '\r' | sed 's/^[Ll]ocation: //')
echo "$LOCATION"
# /binflow/ui/#step_up_grant=fab28b17a753f234aeb40adb5d68c6854ad429d30006c9d6b3cc7aa24e2cfcad
G=${LOCATION##*#step_up_grant=}   # 64 位 hex

# ④ TTL 窗口内携 grant 铸造 → 200；复用同 grant → 401 step_up_invalid
curl -s -b $JAR -X POST $A/api/security/token \
  -d "grant_type=client_credentials&step_up_grant=$G" | jq '{expires_in,scope}'
curl -s -b $JAR -X POST $A/api/security/token \
  -d "grant_type=client_credentials&step_up_grant=$G" -w '\nHTTP %{http_code}\n'
# {"error":"step_up_invalid","error_description":"step-up credential rejected, expired, or already used"}
# HTTP 401
```

两处**产品行为**（ADR 未规定面，按实现如实记录）：

1. **grant 走 URL fragment** 而非 query——`#step_up_grant=...` 不会出现在服务端/代理的访问日志里。
2. **错腿凭据按「所欠凭据缺失」判**：OIDC 用户交 `step_up_password`（或本地用户交 `step_up_grant`）→ 401 `step_up_required` 而非 `step_up_invalid`——不借此泄露「该用户的腿是什么」的探查信息。

## SSO 用户 CLI 铸 Token 的路径

OIDC 用户**没有本地口令**，口令腿对其恒不可用。三条现实路径：

1. **脚本驱动 re-auth 流**（上文 curl 全链的形态）：cookie jar 持有 OIDC session，脚本完成 IdP 表单 POST 拿 grant 后立即铸造——全程可自动化，实测可用。
2. **浏览器完成 re-auth，取 fragment 里的 grant**：callback 落到 `/binflow/ui/#step_up_grant=<grant>`，从地址栏复制 grant 后**在同一浏览器会话**内完成铸造。注意控制台铸造页（读取 hash 自动续铸、二次密码框）尚未落地（Access Tokens 页现为占位）——落地前以本形态过渡。
3. **admin 代铸**：admin 可指名替目标用户签发（`grant_type=client_credentials&username=<目标>`，admin 臂免 step-up）——「给 SSO 同事发一枚 Token」的最短路径。

## 401 `step_up_invalid` 的全部场景

一个错误码，四种成因（对调用方同形，成因看服务端日志）：

| 成因 | 说明 |
|---|---|
| 口令失验 / grant 不存在 | 本地 argon2 不过、LDAP bind 不过、grant 拼错 |
| **grant 已被消费** | 单次性——同一 grant 第二次铸造即烧（含第一次失败的请求） |
| grant 过期 | 超出 `token_step_up_grant_ttl_seconds` 窗口（实测 TTL=60 + 70s 后 → 401） |
| 服务重启 | grant 台账在进程内存，重启即空——重走 re-auth |

## 审计

step-up 路径铸造的 `token.issue` 事件 detail 增两个维度（**仅 step-up 路径出现，豁免臂不写**）：

```bash
curl -su admin:<口令> "$BASE/binflow/api/v1/audit?action=token.issue&limit=6" \
  | jq -c '.events[] | {actor, detail:(if (.detail|type)=="string" then (.detail|fromjson) else .detail end)}'
# {"actor":"ssouser","detail":{...,"step_up":true,"step_up_method":"oidc_reauth","source":"oidc"}}
# {"actor":"jdoe",  "detail":{...,"step_up":true,"step_up_method":"password","source":"ldap"}}
# {"actor":"dev1",  "detail":{...,"step_up":true,"step_up_method":"password","source":"local"}}
# {"actor":"admin", "detail":{...,"source":"local"}}          ← 豁免臂：无 step_up 字段
```

`step_up_method` 闭集两值：`password`（本地与 LDAP 同为口令腿）/ `oidc_reauth`。

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 401 `step_up_required` | 所欠凭据缺失（本地/LDAP 缺 `step_up_password`；OIDC 缺 `step_up_grant`）；**或交了错腿的凭据** | 按腿补字段；OIDC 用户先走 re-auth 流 |
| 401 `step_up_invalid` | 口令失验 / grant 过期 / **grant 复用** / 服务重启 | 见上文四成因表；grant 一次性，重走获取流 |
| 401 `invalid_request` + `The user: '...' can only create user token with expires in larger than 0 and smaller than 31536000 seconds ...` | **不是 step-up 报错**——step-up 已过，TTL 护栏拒了 `expires_in`（>365d 帽） | 调低 `expires_in`（非 admin 上限 365d） |
| 403 `invalid_request` + `administrator privileges required` | 非 admin 为**他人**铸（step-up 之后才判） | 仅限本人；为他人铸走 admin |
| 启动失败 `config: auth.token_step_up_grant_ttl_seconds must be within [60, 3600] seconds` | TTL 越界（**开关 off 也拒**） | 取值落回 [60, 3600] |
| CI 里 Basic 铸 Token 突然 401 `step_up_*` | 不可能由 step-up 引起（Basic 臂豁免）——查口令本身 | 核对凭据；step-up 只作用于 session 臂 |

## 下一步

- Token 的日常使用与高 QPS 建议：[FAQ](../faq.md)
- 角色模型与 readonly_admin 边界：[RBAC 角色与仓库级管理员](rbac-roles.md)
- OIDC / LDAP 配置全解：[OIDC 配置](../guides/oidc-config.md) · [LDAP 配置](../guides/ldap-config.md)
