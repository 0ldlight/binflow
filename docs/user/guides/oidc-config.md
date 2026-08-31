---
title: OIDC 单点登录配置
sidebar_position: 51
---

# OIDC 单点登录配置

> 适用版本：M6 起（ADR-0020；`auth.oidc` 段 + `/api/v1/oidc/*` 端点）；**M9 增补**：`purpose=step_up` 重认证与 token step-up 联合部署（armed 形态）注记（T-260）。
> 本文配置键与行为逐项核对 `internal/config/load.go`、`internal/httpapi/oidc.go`；登录全链行为以真 Keycloak 26.2.5 容器验收为据（`reports/agents/T-174.md` H24~H29，T-185 修复组同步后行为见下文「用户与组映射」）。armed 形态样例在自起 mock IdP 栈（2026-08-25 HEAD 构建）实测启动并走通 step-up 全链。

启用后，控制台出现「使用 SSO 登录」按钮，浏览器跳转 IdP 完成登录；ID Token 也可直接作 Bearer 凭据调用管理 API。未启用（默认）的实例上，两条 `/oidc` 路由一律 404，SSO 姿态不可探测。

## 前置条件

- M6 构建的 `binflow-server`（旧构建的严格配置解码器遇到 `auth.oidc` 段会拒启）。
- 一个可达的 OIDC Provider（Keycloak、Okta、Casdoor 等任意实现发现端点的 IdP）。
- 在 IdP 侧注册一个 OAuth2 client（confidential 或 public 均可，见下文）。

## 配置

`binflow.yaml`：

```yaml
auth:
  oidc:
    enabled: true
    issuer_url: https://accounts.example.com          # 发现基准：<issuer>/.well-known/openid-configuration
    client_id: binflow-console
    redirect_url: https://binflow.example.com/binflow/api/v1/oidc/callback
    # scopes: [openid, profile, email]                # 缺省即这三个
    # user_claim: preferred_username                   # 缺省
    # group_claim: groups                              # 缺省
    # admin_group: binflow-admins                      # 空 = 不做组→管理员映射
```

client secret 走环境变量，**写进 YAML 会拒启**（指向修复动作的报错）：

```bash
export BINFLOW_AUTH_OIDC_CLIENT_SECRET=<client-secret>
binflow-server serve -c binflow.yaml
```

| 键 | 必填（enabled 时） | 缺省 | 说明 |
|---|---|---|---|
| `auth.oidc.enabled` | — | `false` | 关闭时两条 `/oidc` 路由 404 |
| `auth.oidc.issuer_url` | 是 | — | 必须是绝对 http(s) URL；启动时做发现，不可达直接拒启 |
| `auth.oidc.client_id` | 是 | — | IdP 侧注册的 client ID |
| `auth.oidc.redirect_url` | 是 | — | 回调地址，固定形如 `<对外地址>/binflow/api/v1/oidc/callback` |
| `auth.oidc.scopes` | 否 | `[openid, profile, email]` | 请求的 scope 列表 |
| `auth.oidc.user_claim` | 否 | `preferred_username` | 用户名取自哪个 ID Token claim |
| `auth.oidc.group_claim` | 否 | `groups` | 组成员取自哪个 claim |
| `auth.oidc.admin_group` | 否 | 空 | 该组成员映射为 BinFlow 管理员 |

除 `BINFLOW_AUTH_OIDC_CLIENT_SECRET`（client secret）与 `BINFLOW_AUTH_OIDC_READONLY_GROUP`（M7 角色映射键，双拼写 `BINFLOW_AUTH__OIDC__READONLY_GROUP` 等价）外，`auth.oidc` 的其余键**只能写在 YAML**——配置加载器的 env 覆盖面不含这些路径：`BINFLOW_AUTH__OIDC__ENABLED`、`BINFLOW_AUTH__OIDC__ISSUER_URL` 等拼法一律拒启（`unknown BINFLOW_ environment variable(s)`，fail-fast），**不能用环境变量把 OIDC「armed」起来**（见下文 armed 形态）。

`client_secret` 非必填：PKCE-only 的 public client 合法（校验层放行，由 Provider 构造面最终裁定）。

## 验证

```bash
# 1. 启动日志确认（发现失败则看不到这行，转而看到 wiring auth.oidc 报错）
#    ... "oidc authentication active" issuer=https://accounts.example.com

# 2. 能力清单：oidc 位翻 true
curl -s $BASE/binflow/api/v1/auth/methods
# {"password":true,"oidc":true,"ldap":false}

# 3. 浏览器打开控制台登录页，应出现「使用 SSO 登录」
#    （前端以 redirect:manual 探测 GET /binflow/api/v1/oidc/login：302=启用，404=隐藏）

# 4. 手动走一遍首跳
curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' $BASE/binflow/api/v1/oidc/login
# 302 https://accounts.example.com/...?code_challenge=...&code_challenge_method=S256&state=...
```

登录流（全部由服务端编排，前端不经手令牌）：`GET /binflow/api/v1/oidc/login` 302 到 IdP（Authorization Code + PKCE S256 + 256 位 state，事务存 10 分钟 HttpOnly cookie）→ IdP 认证后回调 `GET /binflow/api/v1/oidc/callback` → 服务端换码、验签（JWKS/issuer/audience/expiry）→ 签发 `binflow_session` → 302 `/binflow/ui/`。

同一入口携 `purpose=step_up`（M7 服务端 / M9 控制台消费）即为 **step-up 重认证流**：302 到 IdP 时强制 `prompt=login`（OIDC Core 标准参数——IdP 必须重新问凭据，不吃 SSO 会话）；callback 不建新 session（重认证主体必须与当前 session 同人），改签发一次性的 mint grant 经 URL fragment 回传（`/binflow/ui/#step_up_grant=<64hex>`，fragment 不进服务端与代理日志）。未知 `purpose` 值 → 400。全链语义见 [Token 铸造二次认证（step-up）](../admin/token-step-up.md)。

## 与 token step-up 联合部署（armed 形态）

「SSO 用户铸 Token 也要过二次认证」需要两把开关同时打开：`auth.token_step_up: true` + 本文的 `auth.oidc.enabled: true`。**注意配置面**：`auth.token_step_up` 可走环境变量（`BINFLOW_AUTH_TOKEN_STEP_UP=true`），但 `auth.oidc` 的子键（enabled/issuer_url/client_id/redirect_url 等）**只能写进 YAML 配置文件**——armed 实例必须用 `-c` 携带配置文件启动，不能只靠 env 拼起：

```yaml
# armed.yaml（实测样例，本地演练值）
server:
  listen: 127.0.0.1:18098
storage:
  data_dir: /path/to/data-armed
auth:
  token_step_up: true            # 本键 env 可达；oidc 子键不行——整段进 YAML
  oidc:
    enabled: true
    issuer_url: http://127.0.0.1:18099
    client_id: binflow-console
    redirect_url: http://127.0.0.1:18098/binflow/api/v1/oidc/callback
    readonly_group: bf-readonly   # 该组成员映射 readonly_admin（非 admin 会话——恰为 step-up 作用域）
```

```bash
BINFLOW_ADMIN_PASSWORD=<口令> BINFLOW_AUTH_OIDC_CLIENT_SECRET=<client-secret> \
  BINFLOW_REMOTE_CREDENTIALS_KEY=<base64 的 32 字节密钥> \
  binflow-server serve -c armed.yaml
```

> `BINFLOW_REMOTE_CREDENTIALS_KEY` 是 armed 起法的**必配项**（M11 认证配置面起）：`auth.oidc` 落库的 `client_secret` 走凭据密封，无主密钥启动即拒——`… seeding section oidc: field "client_secret": … no master key: BINFLOW_REMOTE_CREDENTIALS_KEY is not set (required to store secrets)`（T-382 契约漂移修正，2026-08-31 复跑实证：补钥后启动 200、`GET /api/v1/auth/methods` 回 `"oidc":true`）。密钥生成见 [remote/virtual 管理指南](../admin/remote-virtual.md#上游凭据与-binflow_remote_credentials_key)。

启动后自检：`GET /api/v1/auth/methods` 回 `"oidc":true`；用 SSO 账号登录控制台 → Set Me Up → 生成令牌，应走「重新认证并继续」引导（不出口令框）。本地演练可用仓库自带的零依赖 mock IdP（`node web/scripts/mock-idp.mjs <端口> <client-secret>`，固定身份 `ssouser`）代替真实 IdP 走通全链。

## 用户与组映射

- **首登自动建号**：本地无对应用户行时按 claim 建行（`provider=oidc`、无本地口令、enabled）。注意同名冲突边界：IdP 用户名与既有**本地**用户同名时，建行会撞唯一约束、该次登录失败（500 `oidc login failed`）——用户名空间三臂共享，规划命名时避开。
- **组同步是替换不是并集**：每次认证成功，`user_groups` 被重写为 claim 里的组集合——IdP 侧移除组，下次登录即失效（权限即时收回）。
- **IdP 组不自动物化**：只有目标侧**已存在同名组**才写入成员；未知组名跳过（Debug 日志）。要让某个 IdP 组生效，先在 BinFlow 建组、挂权限，成员随登录到位。
- **管理员映射**：`admin_group` 命中的用户 `is_admin=true`；Provider 是权威——每次登录刷新，IdP 侧降权下次登录即收回。
- **whoami 自检**：`GET /binflow/api/v1/session` 返回 `{"username","admin","source":"oidc","groups":[...]}`。

OIDC 用户可为自己签发 API Token（`POST /api/security/token`，非管理员受 `auth.token_nonadmin_max_ttl` 上限约束，默认 365 天），用于脚本与 CI。

## Keycloak 参考配置（验收实测）

| 项 | 值 |
|---|---|
| realm | 任意（issuer 即 `https://<host>/realms/<realm>`） |
| client | confidential；standard flow；PKCE S256 |
| redirect URI | `https://binflow.example.com/binflow/api/v1/oidc/callback`（与 `redirect_url` 逐字一致） |
| 组 claim | Client scopes → Groups Membership mapper，Token Claim Name 填 `groups`（加进 ID Token） |
| 管理员组 | 建 `binflow-admins` 组，`admin_group` 填组名 |

## Helm Chart

```yaml
config:
  oidc:
    enabled: true
    issuerUrl: https://accounts.example.com
    clientId: binflow-console
    redirectUrl: https://binflow.example.com/binflow/api/v1/oidc/callback
    adminGroup: binflow-admins
    existingSecret: binflow-oidc   # Secret 需含 BINFLOW_AUTH_OIDC_CLIENT_SECRET 键
```

```bash
kubectl create secret generic binflow-oidc \
  --from-literal=BINFLOW_AUTH_OIDC_CLIENT_SECRET=<secret>
```

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 启动报 `wiring auth.oidc: ... discovery` | issuer 不可达/证书不信/URL 拼错 | 修 `issuer_url`；启动即做发现是 fail-fast 设计 |
| 启动报 `auth.oidc.issuer_url is required...` | enabled 但三必填缺项 | 补齐 issuer_url / client_id / redirect_url |
| 启动报 `key "auth.oidc.client_secret" looks like a secret` | secret 写进了 YAML | 移到 `BINFLOW_AUTH_OIDC_CLIENT_SECRET` |
| 回调 400 `oidc state mismatch` | 事务 cookie 与 state 不匹配（伪造/跨流重放/旧标签页） | 从控制台重新发起登录 |
| 回调 400 `oidc login transaction missing or expired` | 事务 cookie 超过 10 分钟或被清 | 重新登录 |
| 登录后 401 `invalid credentials` | ID Token 验签失败（issuer/audience/expiry）或用户被禁用 | 核对 `client_id` 与 IdP 时钟；查用户 enabled 位 |
| SSO 后组授权仓库 403 | 组未在目标侧物化 | 先建同名组并授权，重新登录触发同步 |

## 下一步

- 目录认证（同一登录表单、先本地后目录）：[LDAP 配置](ldap-config.md)
- 组与权限模型：[用户组与权限管理](../admin/groups-permissions.md)
- Token 生命周期与吊销：[API 参考](../api-reference.md)
