---
title: 认证配置（LDAP / OIDC / SAML）
sidebar_position: 48
---

# 认证配置（LDAP / OIDC / SAML）

> 适用版本：M11（配置面 REST + 控制台页组随 T-305/T-307 交付；设计依据 ADR-0035，行为基准 `docs/reverse/auth-integration.md` v2）。本文的 PUT/哨兵/测试连接命令在 HEAD 构建 scratch 实例（127.0.0.1:8328，`BINFLOW_REMOTE_CREDENTIALS_KEY` 已设）上 curl 实测，2026-08-28。
> 与专题指南的分工：[OIDC 配置](../guides/oidc-config.md)/[LDAP 配置](../guides/ldap-config.md)讲**文件配置**（`binflow.yaml` 的 `auth.oidc`/`auth.ldap` 段、IdP 侧注册、登录全链）；本文讲**运行态配置面**——控制台与 REST 直接改实例上的生效配置，改完即生效。

BinFlow 的三种外部认证协议（LDAP 目录登录、OIDC 单点登录、SAML 集成）各占**一个配置段**，支持两条管理路径：

- **控制台**：管理模式 → 用户与权限 → **认证配置**（路径 `/admin/security/auth`，admin / readonly_admin 可见）——三个 Tab（LDAP / OAuth / SAML），表单化编辑 + 测试连接。
- **REST**：`/binflow/api/v1/admin/security/*` 九端点（下文）。

核心语义（三条，先记住再操作）：

| 语义 | 说明 |
|---|---|
| **保存即生效** | PUT 成功后的**下一个请求**就走新配置——OIDC 登录臂与 LDAP 登录臂逐请求取当前快照，不重启、不断流；`GET /api/v1/auth/methods` 的 `oidc`/`ldap` 位随 `enabled` 翻转（实测：PUT 禁用 LDAP → methods 立即回 `"ldap": false`） |
| **secret 只写不读** | 口令/客户端密钥落库前经实例主密钥 **enc:v1 密封**（AES-256-GCM）；GET 对已设置的 secret 恒回显 20 星哨兵 `********************`，明文永不回传 |
| **测试连接先行** | 每段一个 `POST …/test` 端点，保存前后都可探测（LDAP 守卫拨号 + manager bind + 试搜；OIDC discovery 拉取；SAML loginUrl 探测），5s 短超时 |

## 前置条件

- 运行中的 BinFlow 实例；管理员凭据（PUT/test 需全量 admin，readonly_admin 仅可 GET）。
- **要写 secret 就必须设主密钥**：实例需带 `BINFLOW_REMOTE_CREDENTIALS_KEY`（base64 的 32 字节）启动，否则含 secret 的 PUT 被拒——密封不可用时不假装存了。已有密封行而无主密钥的实例**启动即拒**（fail-fast）。

## 控制台路径

`/admin/security/auth` 索引页重定向到 `ldap` Tab；Tab 间 `←`/`→` 键盘切换。三 Tab 表单要点：

- **LDAP Tab**：`key` 锁定展示 `ldap`（单段模型——BinFlow 一协议一段，非 Artifactory 的多设置列表）；search 子组内联；`managerPassword` 为 secret 字段——已设置时表单留空 + placeholder「留空保持不变」。
- **OAuth Tab**：字段为 BinFlow OIDC 单段 wire（`issuer_url`/`client_id`/`client_secret`/`redirect_url`/`scopes`/claims/组映射）——issuer 发现式，非 Artifactory oauthSettings 多 provider 模型。
- **SAML Tab**：13 字段全量表单；**「Auto Create Users」复选框是正语义**（勾选 = 自动创建）——wire 字段 `noAutoUserCreation` 为反语义，提交时自动取反；从未保存过的 SAML 段 GET 回 `{}`，页内显示引导块，保存后消失。

readonly_admin 打开页面时控件全部 disabled；直接调 REST PUT 由服务端 403 终裁。

## REST 面（九端点）

| 方法 | 路径 | 门 | 说明 |
|---|---|---|---|
| GET | `/binflow/api/v1/admin/security/ldap` | CapSecurityRead | LDAP 段（未设置回默认形） |
| PUT | `/binflow/api/v1/admin/security/ldap` | CapSecurityWrite | 整段替换（段粒度全量 PUT） |
| POST | `/binflow/api/v1/admin/security/ldap/test` | CapSecurityWrite | 测试连接 |
| GET / PUT | `/binflow/api/v1/admin/security/oauth` | 同上 | OIDC 段（未设置回默认形） |
| POST | `/binflow/api/v1/admin/security/oauth/test` | CapSecurityWrite | 测试连接（discovery 探测） |
| GET / PUT | `/binflow/api/v1/admin/security/saml/config` | 同上 | SAML 段（未设置回 `{}`） |
| POST | `/binflow/api/v1/admin/security/saml/config/test` | CapSecurityWrite | 测试连接（loginUrl 探测） |

`test` 归写门的原因：它会向配置的目标发起**出站连接**（全部经实例 SSRF 守卫缝）。PUT 是**整段替换**——非 secret 字段要逐字段全量回传，漏发的字段会被归一成默认值（控制台字段册保证全集；手写 curl 请先 GET 改后 PUT 全量发）。

### 实测往返（LDAP 段）

```bash
export BASE=http://localhost:8328 ADMIN_PW=<管理员口令>

# 1) 未设置的段：GET 回默认形（SAML 段则是字面 {}）
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/admin/security/ldap
# {"key":"ldap","enabled":true,"ldapUrl":"","userDnPattern":"","search":{...,"managerDn":"","managerPassword":""},...,"poolSize":5}

# 2) 整段 PUT（ldapUrl 必须把 base DN 写进 URL path）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/v1/admin/security/ldap \
  -H 'Content-Type: application/json' -d '{
    "key":"ldap","enabled":true,"ldapUrl":"ldap://ldap.example.com:389/dc=example,dc=com",
    "userDnPattern":"uid={0},ou=people",
    "search":{"searchFilter":"(uid={0})","searchBase":"ou=people","searchSubTree":true,
              "managerDn":"cn=admin,dc=example,dc=com","managerPassword":"s3cret-pass"},
    "autoCreateUser":true,"emailAttribute":"mail","allowUserToAccessProfile":false,
    "pagingSupportEnabled":true,"ldapPoisoningProtection":true,
    "groupFilter":"(objectClass=groupOfNames)","groupBaseDn":"ou=groups",
    "groupNameAttribute":"cn","adminGroup":"binflow-admins","readOnlyGroup":"",
    "startTls":false,"skipTlsVerify":false,"poolSize":5}' -o /dev/null -w '%{http_code}\n'
# 200

# 3) GET 回显：secret 折叠为 20 星哨兵
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/admin/security/ldap \
  | jq -r '.search.managerPassword'
# ********************

# 4) 把 GET 到的文档原样 PUT 回去（含哨兵）→ 400，在用配置不动
# {"errors":[{"status":400,"message":"auth config: field \"managerPassword\": refusing
#   the masked placeholder — leave the field empty to keep the stored secret, or re-enter the value"}]}

# 5) 不带 managerPassword 键的 PUT → 200，存量 secret 保持（GET 仍 20 星）
```

## secret 哨兵语义（write-only 三态）

| PUT 里 secret 字段的值 | 效果 |
|---|---|
| **键缺席**（或控制台留空） | 保持存量（最常用——改其它字段时不用重输口令） |
| **新明文** | 替换（落库前 enc:v1 密封） |
| `""` 空串 | 清除 |
| **20 星哨兵**（`********************`） | **400 拒绝**——回显占位符不是合法输入，防「把掩码存成口令」；在用配置不受影响 |

## 字段速查

### LDAP 段（wire 键）

| 组 | 字段 | 说明 |
|---|---|---|
| 基本 | `key`（恒 `ldap`）/ `enabled` / `ldapUrl` | `ldapUrl` 格式 `ldap://host[:port]/<baseDN>`——base DN 写在 URL path（缺失 400，报错附示例）；`startTls`/`skipTlsVerify` 控制升级姿势 |
| 搜索 | `search.searchFilter`（RFC 2254，`{0}` 占位用户名）/ `search.searchBase` / `search.searchSubTree`（默认 true） | 用户查找与绑定由 search 段驱动 |
| 凭据 | `search.managerDn` / `search.managerPassword`（secret） | manager bind；空 `managerDn` = 匿名只读 bind |
| 账号 | `autoCreateUser`（默认 true）/ `emailAttribute`（默认 `mail`）/ `allowUserToAccessProfile` / `pagingSupportEnabled` / `ldapPoisoningProtection` / `userDnPattern` | `userDnPattern` 已持久化+回显+校验，**直绑消费未实现**（绑定走 search 段）——见「已知边界」 |
| 组映射（BinFlow 扩展） | `groupFilter` / `groupBaseDn` / `groupNameAttribute` / `adminGroup` / `readOnlyGroup` | 登录时解析组成员关系映射角色 |
| 连接池 | `poolSize`（默认 5） | 换配置后旧池 30s 宽限排空，在途请求不被打断 |

### OIDC 段（wire 键，snake_case）

`enabled` / `issuer_url` / `client_id` / `client_secret`（secret）/ `redirect_url` / `scopes`（数组，默认 `["openid","profile","email"]`）/ `user_claim`（默认 `preferred_username`）/ `group_claim`（默认 `groups`）/ `admin_group` / `readonly_group` / `auto_create_users`（默认 true）。

### SAML 段（13 字段）

`enableIntegration` / `serviceProviderName` / `loginUrl` / `logoutUrl` / `certificate` / `useEncryptedAssertion` / `syncGroups` / `groupAttribute` / `emailAttribute` / `noAutoUserCreation`（**反语义**）/ `allowUserToAccessProfile` / `autoRedirect` / `verifyAudienceRestriction`。

## 测试连接

```bash
# 空体 POST = 探测已保存配置
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/admin/security/ldap/test
# 目标不可达时（400 + TestReport）：
# {"ok":false,"phase":"dial","category":"unreachable",
#  "message":"could not connect to the target (dial failed or timed out)"}
```

- 响应为单条 TestReport（`{ok, phase, category, message}`）；`ok:false` 时 HTTP 400，`message` 呈现失败原因原文。
- **LDAP 附带完整用户 bind 探测**：body 带 `{"testUsername":"jane","testPassword":"…"}` 两半齐备才走用户 bind 腿（只有一半 → 400 拒绝）。
- 控制台的「测试连接」把**当前表单值**随体发送——可以先测后存；空体则测存量配置。

## 首启种子与双源

`binflow.yaml` 的 `auth.oidc` / `auth.ldap` 段仍在（见专题指南）——**首次启动**时若有文件段，会被规范化种子进 DB（此后的权威源是 DB 配置面）。两条纪律：

- 文件段里的 secret 从环境变量取（如 `BINFLOW_AUTH_OIDC_CLIENT_SECRET`），写进 YAML 照旧拒启。
- **带 secret 的文件段首启种子需要主密钥**：无 `BINFLOW_REMOTE_CREDENTIALS_KEY` 时种子写被拒 → 实例拒启（升级部署时注意，见[安装升级](../install/upgrade.md)）。
- DB 有配置段后，文件段不再覆盖（每次启动 DB 权威 + WARN 提示）。

## 审计

| action | 触发 | detail |
|---|---|---|
| `auth.config.update` | 段 PUT 成功 | actor / 段名 / **变更键名列表**——值恒不落 |
| `auth.config.test` | 测试连接 | 结果类别 |

## 已知边界（M11）

| 项 | 现状 |
|---|---|
| SAML 运行时登录臂 | 13 字段已持久化 + 回显 + 校验；**SP 断言消费不在 M11 交付面**（保存的配置尚不构成可登录的 SAML IdP 接入） |
| SAML 证书动作 | Artifactory 的公钥下载 / 再生成端点（`…/saml/config/key/*`）未落——`useEncryptedAssertion` 字段已渲染可存 |
| `userDnPattern` 直绑 | 未消费（绑定由 search 段驱动）；如需对齐 Artifactory 行为另开票 |
| LDAP 多设置列表 | 单段模型——无 Artifactory 的多 LDAP 设置列表 / 拖拽排序 / 独立 DELETE |

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| PUT 400 `refusing the masked placeholder` | 回传了 GET 回显的 20 星哨兵 | secret 键整段删掉再 PUT（= 保持），或填新值 |
| PUT 400 `the URL path is the search base DN and is required` | `ldapUrl` 没带 base DN | 改成 `ldap://host/dc=example,dc=com` 形态 |
| PUT secret 403/500（密封类报错） | 实例无主密钥 | 带 `BINFLOW_REMOTE_CREDENTIALS_KEY` 重启实例 |
| test 400 `dial` / `unreachable` | 目标不可达或被 SSRF 守卫拦截 | 核对网络；私网目标走私网部署或放行策略 |
| 改完配置要重启吗 | 不用 | 保存即生效；methods 位与登录臂逐请求取当前快照 |

## 下一步

- 文件配置全链：[OIDC 单点登录](../guides/oidc-config.md) · [LDAP 目录认证](../guides/ldap-config.md)
- API 端点总表：[API 参考](../api-reference.md)
