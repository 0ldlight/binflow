# 认证与授权模型行为规格（用户 / 密码 / Token / 权限概览）

> 逆向基线：artifactory-pro 7.161.16（见 `reverse-src/artifactory/README.md`）。
> 置信度标注：`高` = 反编译代码 + JFrog 官方文档双证；`中` = 仅反编译代码可见；`低` = 推断待验证。
> 官方参考：JFrog "Deprecated JFrog APIs" 页（Get Users / Create or Replace User / Update User / Delete User / Change Password / Create Token / Refresh Token / Get Tokens / Revoke Token 条目，docs.jfrog.com/integrations/docs/deprecated-jfrog-apis）。
> 本规格为 PRD §5.5 校准项 ⑤（E-17/E-18）与 E-16/E-19 的校准来源。

## 0. 总则

| 条目 | 行为 | 置信度 |
|---|---|---|
| 架构前提 | 7.x 中用户/组/权限的**事实存储在 Access 服务**（独立部署的内嵌服务），Artifactory 自身只有内存模型 `UserInfo`/`GroupInfo` + 缓存，用户写操作经内部 REST 转发给 Access（`o.a.a.storage.db.security.service.access.AccessUserGroupStoreService#patchUser`）。BinFlow 单体实现可等效为「自有用户表」，对外行为不变。 | 高 |
| 错误响应体 | 用户/权限管理 API 的错误多为**纯文本 body**（`text/plain`，如 `Response.status(BAD_REQUEST).entity("...")`），与制品 API 的 `{"errors":[...]}` JSON 格式**不同**；Token API 用 OAuth 风格 `{"error":"...","error_description":"..."}`。三种格式并存。 | 高 |
| 路径参数解码 | `/api/security/*` 的实体名路径参数在 `security.api.plus.insteadof.space=true`（**默认 true**）时做 URL-decode，即 `+` 被解释为空格；要传字面 `+` 需先关掉该开关。 | 高（`o.a.a.rest.resource.security.SecurityResource#decodeEntityKey` + 官方文档同名属性记载） |
| 认证入口（管理 API） | 全部要求认证（匿名仅有匿名权限集）；失败 401 + `WWW-Authenticate: Basic realm="Artifactory Realm"`。 | 高 |

---

## 1. 用户模型

### 1.1 用户数据结构（内存模型 `o.a.a.model.security.UserImpl`，对外可观察字段）

| 字段 | 形态/语义 | 置信度 |
|---|---|---|
| `username` | 唯一；创建/更新时统一小写化（`o.a.a.security.SecurityServiceImpl#updateUser` 先 `toLowerCase` 再落库）；保留名 `_system_` 禁止创建 | 高 |
| `password` / `salt` | 旧形态为「加盐口令」二元组（`SaltedPassword{password, salt}`）；7.x 实际哈希与校验由 Access 服务完成，Artifactory 侧创建用户时把**明文口令**经内部 REST 送 Access（`AccessUserGroupStoreService#changePassword`/`createUser`）。旧本地默认盐为常量 `CAFEBABEEBABEFAC`（`ConstantValues.defaultSaltValue`，仅遗留兼容）。**任何 API 响应都不回显口令或哈希。** | 高（不回显）；中（遗留盐值） |
| `email` | 创建时必填（blank → 400，见 §1.2） | 高 |
| `admin` | 布尔；「有效 admin」= 直接 admin 或所属组带 admin 权限（`UserConfigurationImpl#isEffectiveAdmin`）。admin 用户强制 `profileUpdatable=true` 且 `disableUIAccess=false`（违反 → 400） | 高 |
| 禁用/锁定状态 | 状态机 `invited / enabled / disabled / locked`（`ArtifactoryUserStatus`），另有 `enabled`、`accountNonExpired`、`accountNonLocked`、`credentialsExpired`、`passwordDisabled`（禁用内部口令=只允许 token/外部 realm 登录）、`locked`（登录锁定策略触发）。 | 中 |
| `realm` | `internal` / `ldap` / `access` 等；GET users 列表按用户返回 realm | 高 |
| `groups` | 组名集合；创建用户时自动并入「新用户默认组」（auto-join 组） | 高 |
| 其它 | `lastLoginTimeMillis`（GET 单用户时格式化为 ISO8601 `lastLoggedIn`）、`updatableProfile`、`userProperties`（含 `blockUiView` 实现 `disableUIAccess`）、MFA 状态 | 中 |

### 1.2 用户 CRUD 端点表（`o.a.a.rest.resource.security.SecurityResource` + `o.a.a.addon.security.RestSecurityRequestHandler`）

注意：**真实 Artifactory 没有集合级 `POST /api/security/users`**；创建/替换走 `PUT /api/security/users/{userName}`。

| 方法 | 路径 | 权限 | 成功 | 主要错误 | 置信度 |
|---|---|---|---|---|---|
| GET | `/api/security/users` | admin | 200 JSON **数组**，元素 `{"name","uri","realm"}`（uri 为该用户详情的绝对链接） | 非admin 403 | 高 |
| GET | `/api/security/users/{userName}` | admin | 200 JSON：`name/email/admin/groups[]/lastLoggedIn/realm/profileUpdatable/internalPasswordDisabled/disableUIAccess/...`（见下） | 404（无 body） | 高 |
| PUT | `/api/security/users/{userName}` | admin（非 admin 默认 403；开关 `security.allow.only.admin.create.entity=false` 时放行 resource-manager） | 201 **无 body** | 见下方校验链 | 高 |
| POST | `/api/security/users/{userName}` | admin | 200 无 body（部分更新语义） | 404 / 409 / 400 | 高 |
| DELETE | `/api/security/users/{userName}` | admin | 200 text `The user: '<name>' has been removed successfully.` | 404；Access 拒绝 → 403（透传消息） | 高 |

GET 单用户响应字段集（`SecurityModelPopulator#getUserConfiguration`）：`name`、`email`、`admin`（直接或组 admin 均为 true）、`groups`、`lastLoggedIn`（ISO8601，>0 才有）、`realm`、`profileUpdatable`、`internalPasswordDisabled`、`disableUIAccess`（来自用户属性 `blockUiView`）、`policyViewer/policyManager/watchManager/reportsManager`、`mfaStatus`、`status`。**不含任何口令字段。高**

### 1.3 创建/替换用户（PUT）校验链（按代码顺序）

1. 用户名为保留名 `_system_` → 400 `Unable to create user.`
2. body 内 `name` 非空且 ≠ 路径名 → 409 `The username that was provided in the request path does not match the username in the provided user configuration object.`
3. 用户名格式校验 / email XSS 校验失败 → 400（校验器消息）
4. `email` 空 → 400 `Please provide a valid user email.`
5. `password` 空且（未禁用内部口令 或 admin）→ 400 `Please provide a valid user password.`
6. `admin=true` 且 `profileUpdatable=false` → 400 `Admin users cannot have a non-updatable profile.`
7. `admin=true` 且 `disableUIAccess=true` → 400 `Admin users cannot have a disabled UI view property.`
8. `groups` 中有不存在的组 → 400 `Unable to find group by name '<g>'.`
9. anonymous 用户被加入 admin 组 → 400 `Can't add anonymous user to an admin group.`
10. 非 admin 调用者尝试创建/替换 admin 用户 → 403
11. 全部通过：已存在 → 更新（update），不存在 → 创建；返回 **201 无 body**

以上整链置信度：高（代码 + 官方文档 Create or Replace User 条目双证；具体文案仅代码可见，文案本身标中）。

### 1.4 更新用户（POST）差异点

- body `name` ≠ 路径名 → 409（同上文案）
- 「有效 admin」且 `profileUpdatable=false` / `disableUIAccess=true` → 400（同上文案）
- 用户属于 admin 组但请求试图去掉其直接 admin → 400 `The user is associated with one or more groups with admin privileges. To disable the admin privileges, remove the user from all relevant groups.`
- 用户不存在 → 404；未提供的字段保持原值（部分更新）

置信度：高。

---

## 2. 密码策略与改密

### 2.1 端点

**7.x 基线没有 `PUT /api/security/password`**（该路径属 4.x–6.x 旧版，本基线代码中已不存在）。现行改密端点：

| 方法 | 路径 | 权限 | 请求体 | 成功 | 错误 | 置信度 |
|---|---|---|---|---|---|---|
| POST | `/api/security/users/authorization/changePassword` | 认证用户（admin 可改任意用户，非匿名用户改自己） | JSON `{userName, oldPassword, newPassword1, newPassword2}` | 200 text `Password has been successfully changed` | 400 text，见下 | 高（`o.a.a.rest.resource.security.SecurityUserResource#changePassword` + 官方文档 Change Password 条目） |

### 2.2 校验规则（`o.a.a.security.SecurityServiceImpl#changePassword` / `#isOldPasswordValid`）

按触发顺序，命中即返回 400 + 纯文本消息：

| 条件 | 消息 | 置信度 |
|---|---|---|
| 用户不存在 / 被锁 / 登录被延迟 / 旧口令错误之外的运行时异常 | `Incorrect username/password or the user might be locked` | 高 |
| 旧口令验证失败 | `Incorrect username/password` | 高 |
| `newPassword1` ≠ `newPassword2` | `New passwords do not match` | 高 |
| 新口令 == 旧口令 | `New password has to be different from the old one` | 高 |
| `newPassword1` 为 null/空 | `New passwords cannot be empty` | 高 |
| 目标用户 `passwordDisabled`（禁用内部口令） | `The specified user is not permitted to reset his password.` | 中（异常文案在 `#changePasswordWithoutValidation` 路径上） |

- **旧口令错误码是 400 而非 401**（已在认证上下文内，属请求校验错误）。
- **复杂度策略默认关闭**：仅当 Access 侧 password policy 启用时才校验复杂度（`o.a.a.util.SecurityUtils#validatePasswordPolicy`），且跳过默认 `admin/password` 与 anonymous。M1 无需实现复杂度。
- 改密成功后旧口令立即失效（认证缓存被清除 `#invalidateAuthCacheEntries`）。
- 相关端点（M1 不需要，列出备查）：`GET /api/security/encryptedPassword`（加密未启用 → 409 `Server doesn't support encrypted passwords`）；`POST .../expirePassword/{userName}` 系列（未启用过期策略 → 400 `Password expiration is not enforced, please enable it first`）。置信度中。

---

## 3. Token 生命周期（PRD E-17/E-18 校准来源，重点）

REST 入口类：`o.a.a.rest.resource.token.TokenResource`（@Path `security/token`；该资源在 7.117.x 起标记 @Deprecated，由 Access 的新 API 取代，但**路径仍由它服务且行为如下**）。服务层：`o.a.a.security.access.AccessServiceImpl`。

### 3.1 创建 / 刷新：`POST /api/security/token`

- **Content-Type：`application/x-www-form-urlencoded`（表单，不是 JSON）**。Produces `application/json`，成功 200。置信度：高（代码 + 官方文档）。
- 权限：admin 全量；非 admin 已认证用户可为**自己**发 token；匿名（未认证）拒绝（401/403，见 3.4）。`grant_type` 缺省 = `client_credentials`。
- 7.x 内部优先代理给 Access 生成（`AccessServiceImpl#createTokenAsProxy`，开关默认关），失败回落本地生成；对外字段一致。

请求表单字段：

| 字段 | 必填 | 语义 | 置信度 |
|---|---|---|---|
| `grant_type` | 否（默认 `client_credentials`） | `client_credentials`（创建）/ `refresh_token`（刷新）；其它值 → 400 `unsupported_grant_type`（`GrantTypeNotSupportedException`，消息 `Grant type is not supported: <v>`） | 高 |
| `username` | 创建时必填 | token 主体；非 admin 只能填自己；不存在则建 transient 用户（需配组 scope）。缺省 → 400 `invalid_request`，`username is required`（`TokenUtils#requireUsername`）。**与 `access_token` 互斥**（刷新场景） | 高 |
| `scope` | 否 | 空格分隔 scope 列表；`api:*` 恒隐含授予。malformed → 400 `invalid_scope` `scope is malformed: <s>`；`internal:` 前缀 → 400 `invalid_scope` `invalid scope: internal`；空 scope/仅 `api:*` 对已存在用户自动补 `applied-permissions/groups:<g1,g2>` | 高 |
| `expires_in` | 否 | 秒数；**0 = 永不过期**；负数 → 400 `invalid_request` `Invalid expires_in value: <n>`；缺省取系统默认。非 admin 上限 365 天（`access.token.non.admin.max.expires.in`，超限 → 401/`invalid_request` 文案 `The user: '<u>' can only create user token with expires in larger than 0 and smaller than <max> seconds (requested: <n>)`） | 高 |
| `refreshable` | 否（默认 false） | true 时响应附 `refresh_token`。**refreshable token 必须带有限 expires_in>0，否则自定义 audience 组合 → 400 `invalid_request` `Only refreshable tokens with expiry can have custom audience`**（`TokenUtils#assertSupportedTokenSpecForCreate`） | 高 |
| `audience` | 否 | 空格分隔的服务 ID；缺省= 本实例 service id；`*`/`*@*` 归一化为 `jfrt@*` | 高 |
| `refresh_token` + `access_token` | 刷新时必填 | grant_type=refresh_token 时两者都必须在，缺一 → 401 `invalid_grant`（`access token is required.` / `refresh token is required.`） | 高 |

响应体字段（`TokenResource#toTokenResponseModel` → `TokenResponseModel`，官方文档 Create Token 条目同构）：

| 字段 | 是否必返 | 语义 | 置信度 |
|---|---|---|---|
| `access_token` | **必返** | JWT 形态的 token 值 | 高 |
| `token_type` | **必返** | 固定 `Bearer` | 高 |
| `expires_in` | 条件返回 | 有效秒数；**永不过期（expires_in=0）的 token 该字段为 null/缺省**（`CreatedTokenInfo#getExpiresIn` @Nullable） | 高（可空性）；中（null 时是否序列化为缺省还是显式 null，取决于全局 Jackson 配置） |
| `scope` | **必返** | 生效 scope 空格串 | 高 |
| `refresh_token` | 仅 `refreshable=true` 时返回 | 刷新凭据 | 高 |
| `token_id` | **不返回** | 创建响应中**没有** `token_id`；token_id 仅出现在 `GET /api/security/token` 列表（见 3.3） | 高（`TokenResponseModel` 字段集 + 官方文档响应示例双证） |

**这条是 PRD 校准项 ⑤ 的关键结论：真实 Artifactory 的创建响应字段集 = `access_token / expires_in / scope / token_type / refresh_token(条件)`，不含 `token_id`。**

错误响应统一格式：`{"error":"<code>","error_description":"<msg>"}`，`Content-Type: application/json`。错误码表（`o.a.a.security.token.TokenResponseErrorCode`）：

| error | HTTP | 触发 |
|---|---|---|
| `invalid_request` | 400 | 参数缺失/非法（username、expires_in<0、token/token_id 互斥等） |
| `invalid_scope` | 400 | scope malformed / internal: 前缀 |
| `unsupported_grant_type` | 400 | grant_type 未知 |
| `invalid_grant` | 401 | 刷新时 token 对无效/过期不可刷新/缺失 |
| `invalid_client` | 401 | （代码枚举存在，本资源未直接使用，保留） |
| `unauthorized_client` | 400 | （同上保留） |
| `invalid_repo_path` | 404 | path: scope 指向不存在路径或目录（`Cannot create a token with provided scope - path doesn't exist : <p>`） |

置信度：高。

### 3.2 刷新语义（grant_type=refresh_token）

- 「简单刷新」：请求参数**恰好**为 `grant_type + refresh_token + access_token`（可加 `expires_in`）时不要求调用者已认证（匿名放行，`AnonymousRefreshTokenRequestInterceptor` 仅对 `POST /api/security/token` 且参数集完全匹配时生效）；多带任何其它参数（如 username）则必须 admin。
- 刷新允许对**已过期**token 进行（签名验证失败原因为 EXPIRED 时容忍；其它验签失败 → 401 `Cannot refresh token: access token signature is invalid (...)`）。
- 跨服务 token（issuer ≠ 本服务）→ 400 `invalid_request`（`TokenIssuedByOtherServiceException` 消息含双方 service id）。
- 新 `expires_in` 必须 >0 且 ≤ 原 token 的有效期，否则 → 401 `To refresh a token, set the 'expires_in' value to <orig> or lower (requested: <n>)`。
- 成功响应同 3.1（返回新 access_token；refreshable=false 可顺带去掉可刷新性）。

置信度：高（`AccessServiceImpl#refreshToken` + 官方文档 Refresh Token 条目）。

### 3.3 列表：`GET /api/security/token`（admin only）

200 JSON：`{"tokens":[{token_id, issuer, subject, issued_at, expiry, refreshable}, ...]}`；`issued_at`/`expiry` 为 **epoch 秒**；`expiry` 可为 null（永不过期）；内部 token 被过滤。置信度：高（`TokenInfoModel` + 官方文档 Get Tokens 条目）。

### 3.4 吊销：`POST /api/security/token/revoke`（admin only）

请求为 `application/x-www-form-urlencoded`（@Consumes 同时声明 `text/plain`），字段：

| 字段 | 语义 | 置信度 |
|---|---|---|
| `token` | 要吊销的 token 值 | 高 |
| `token_id` | 要吊销的 token ID（来自 3.3 列表） | 高 |
| `token_type_hint` | 可选提示，取值必须是 `access_token` 或 `refresh_token`，否则 400 `invalid_request` `Token type is not supported: <v>` | 高 |

行为（`TokenResource#revokeToken` / `AccessServiceImpl#revokeToken(ById)`）：

1. `token` 与 `token_id` **同时**出现 → 400 `invalid_request` `token and token_id are mutually exclusive`
2. 两者都缺 → 400 `invalid_request` `token or token_id are required`
3. 按 `token` 吊销时先验签并校验 issuer 是本服务：跨服务 token → 400 `invalid_request`（消息含双方 service id）
4. 吊销成功 → **200 `text/plain`，body 为 `Token revoked`**
5. **token 不存在/已吊销 → 仍 200，body 变为 `Token not found`（幂等，不报错）**
6. Access 拒绝（403）→ 401/`invalid_request`（`Revoke token operation rejected`）
7. 吊销后：用该 token 的任何请求验证失败 → **401**（验签环节报告 token 已被吊销）

置信度：高（代码 + 官方文档 Revoke Token 条目，文档明确「200 OK (Also returned if the token was already revoked or non-existent)」）。

### 3.5 过期语义

- `expires_in` 秒数自签发起算；**0 = 永不过期**（此时创建响应 `expires_in` 为空）。高。
- 过期 token 验证失败原因标签为 `EXPIRED`；常规请求直接 401；刷新流程对其豁免（见 3.2）。高。
- 默认有效期：代码 `access.token.expiresIn.default` = **365 天（31536000 秒）**；官方文档 Create Token 条目写「default: 3600」。两者不一致——7.161 代码值 365 天（中，仅代码；文档值疑为旧版残留，待动态验证）。非 admin 上限 `access.token.non.admin.max.expires.in` = 365 天（中）。

### 3.6 Token 的使用路径（验证行为）

Token 不是独立头协议的「API key」，而是**多入口进入同一验证器**（`o.a.a.security.access.AccessTokenAuthenticationProvider#authenticate`：验签 → 取 subject → 按需建 transient principal → 组/admin 权限从 scope 还原）：

| 入口 | 行为 | 置信度 |
|---|---|---|
| **Basic 口令位**（`Authorization: Basic base64(<user>:<token>)`） | 口令字段先尝试按普通口令认证，失败后若值可解析为 access token 则走 token 验证（`o.a.a.security.PasswordDecryptingManager#authenticate`）；Basic 的用户名需与 token subject 用户名**忽略大小写一致**，否则 401 `Token principal mismatch.`（`AccessTokenAuthenticationProvider#verifyMatchingPrincipal`） | 高 |
| `Authorization: Bearer <token>` | props 认证链按 `basictoken` 处理，同样回落到 token 验证 | 高（代码；Bearer 大小写不敏感见 `RequestUtils#getBearerAuthenticationValue`） |
| `X-JFrog-Art-Api: <token>`（旧头 `X-Api-Key` 同义） | 先按 API key 属性查找，未命中且值是 access token → 走 token 验证。即该头可同时承载 API key 与 access token | 高（`o.a.a.authentication.AuthenticationFilterUtils#getApiKeyTokenKeyValue` + `PasswordDecryptingManager` 回落分支） |
| 查询参数 `?token=<jwt>` | 仅当 query string 以 `token=` 开头时生效（`ArtifactoryAccessTokenAuthenticationFilter`）；失败 401 | 中 |

验证成功后的权限还原：token 的 `scope` 决定身份语义——`applied-permissions/groups` 显式列组、`applied-permissions/groups:*`（identity token，等价主体本人动态权限）、`jfrt@<id>:admin` 服务 admin；identity token 且主体是 admin 时 token 也是 admin。普通非 identity token 只按 scope 里列出的组取权限。置信度：中（`AccessTokenAuthenticationProvider#populateGroups` / `#setAdminPrivileges`，逻辑仅代码可见）。

被吊销/验签失败的 token：所有入口统一 401。

---

## 4. 权限模型概览（简述，M4 前再细化）

- 授权单元 permission target = `{name, repositories[], includesPattern, excludesPattern, principals{users{<name>:[actions]}, groups{<name>:[actions]}}}`；动作集：`read / write(=deploy) / annotate / delete / manage / distribute / managedXrayMeta`（`o.a.a.security.AceInfo`）。BinFlow M1 用 `read|write|delete` 子集即可。高。
- 路径匹配为 **Ant 风格通配**（`o.a.a.util.PathMatcher`，Spring AntPathMatcher，token 去空白）：`**` 与 `**/*` 视为全匹配；目录前缀用 matchStart（目录命中即覆盖其下尚未上传路径的授权判断）；**excludes 优先于 includes**；includes 为空 = 全部包含。高。
- 端点：旧版 `/api/security/permissions/{name}`（PUT/POST 已标废弃，默认仍可用，`security.deprecated.permissions.api.disabled=false`）；7.x 主用 `/api/v2/security/permissions/*`（GET/HEAD 所有用户，POST/DELETE admin；PUT 亦标废弃）。virtual 仓库不允许出现在 permission target（`Virtual repository is not supported in permissions target: <key>` → 400）。repositories 空/缺失 → 400（`Permission target request missing repositories.` / `must contain at least one repository`）。引用不存在的 repo → 400。principal 引用不存在的用户/组 → 400。高/中混合（详见 `RestSecurityRequestHandler#createOrReplacePermissionTarget`）。
- admin 用户隐式拥有全部权限，不经 permission target 判定。高。

---

## 5. 对 BinFlow M1 的校准建议（PRD §5.5 校准项 ⑤ 收口）

依据：本规格 §3（全部条目置信度高，除非另注）。

### 5.1 E-17 创建 Token 响应字段（PRD 暂定 `access_token/token_id/expires_in`）

**需要修订。** 真实 Artifactory 响应字段集为：

```
access_token   必返
token_type     必返（固定 "Bearer"）
expires_in     条件返（永不过期时缺省/null）
scope          必返
refresh_token  仅 refreshable=true 时返
```

- **`token_id` 不在创建响应中**（真实响应无此字段；token_id 只在 admin 的 `GET /api/security/token` 列表出现）。建议：BinFlow 可继续返回 `token_id` 作为**超集扩展字段**（对真实客户端无害，且方便 revoke），但 FR-5-AC4 的验收不应以「真实 Artifactory 兼容」为由强制它；PRD 表述建议改为「返回 `access_token`（非空）、`token_type`、`expires_in`、`scope`；BinFlow 扩展附 `token_id`」。
- **必须补 `token_type: "Bearer"` 与 `scope`** 两个字段：真实客户端（JFrog CLI、`docker login` 的 token 交换、curl 脚本）依赖它们；缺了会破坏 M2 Docker 与 CLI 兼容。
- `expires_in` 语义校准：秒数；**0 = 永不过期且响应该字段缺省**。M1 若固定有效期，返回实际秒数即可。
- 请求格式校准：真实端点吃 **form-urlencoded**（`-d "username=..."`），PRD C21a 用 JSON。建议 BinFlow 同时接受两种（form 为主、JSON 作兼容扩展），否则 M2 `docker login`（form 提交）会挂。

### 5.2 E-18 Revoke 请求形态（PRD 暂定 JSON `{"token_id"}|{"token"}`）

- 真实形态：`POST /api/security/token/revoke`，**form-urlencoded**，参数 `token` 或 `token_id` **互斥**（同传 → 400 `token and token_id are mutually exclusive`；都缺 → 400 `token or token_id are required`）；`token_type_hint` 可选（值域 `access_token|refresh_token`）。admin only。
- 响应：成功 **200 纯文本 `Token revoked`**；目标不存在/已吊销**仍 200**，body `Token not found`（幂等——PRD 的「2xx」判断正确，但注意 body 文案不同）；吊销后用该 token → 401。
- 建议：BinFlow 接受 form 与 JSON 两种请求体、同时支持 `token`/`token_id`；保留幂等 200；错误走 `{"error":"invalid_request","error_description":...}` 400。

### 5.3 E-16 改密（PRD 暂定 `PUT /api/security/password`）

- 真实 7.x 无该路径；现行 `POST /api/security/users/authorization/changePassword`，JSON `{userName, oldPassword, newPassword1, newPassword2}`，成功 200 纯文本，**旧口令错误 → 400（非 401）**，消息见 §2.2。
- 建议：BinFlow 保留自有 `PUT /api/security/password`（PRD 已定案为自有语义），同时**补挂**真实路径 `POST /api/security/users/authorization/changePassword`（别名路由）以照顾存量脚本；两者错误码统一 400 纯文本。

### 5.4 E-19 用户创建（PRD 暂定 `POST /api/security/users`）

- 真实形态为 **`PUT /api/security/users/{userName}`**（路径带用户名，201 无 body），无集合级 POST。校验链与文案见 §1.3（email/password blank → 400）。
- 建议：BinFlow 保留 PRD 自有的 `POST /api/security/users`，同时补 `PUT /api/security/users/{name}` 兼容路由；响应 201 无 body；GET 列表元素补 `uri`/`realm` 字段，GET 单用户永不回显口令。

### 5.5 其它采纳点

- Token 当 Basic 口令用：校验用户名与 token 主体一致（忽略大小写）再放行；`X-JFrog-Art-Api` 头建议 M1 即支持（等价 Bearer）。
- 非 admin 发 token 仅能为自己发；非 admin 有效期上限可先不实现（365 天上限）。
- 错误体三种格式并存（制品 JSON errors / 用户管理纯文本 / token OAuth error）——建议 BinFlow 照此分层，别把 token 错误塞进 `{"errors":[...]}`。

## 待验证清单（低置信度，动态验证后回填）

| # | 条目 | 现状 |
|---|---|---|
| 1 | token 默认有效期：代码 365 天 vs 官方文档 3600 秒 | 冲突待动态验证（§3.5，标中） |
| 2 | `expires_in=null`（永不过期）时响应是省略字段还是显式 `null` | Jackson 全局配置未定位（§3.1） |
| 3 | 匿名调 `POST /api/security/token`（client_credentials）的拒绝码是 401 还是 403（RolesAllowed 与代码内检查的先后） | §3.1，标低-中 |
| 4 | `invalid_client`/`unauthorized_client` 错误码在本资源的实际触发场景 | 枚举存在但未见直接抛点（§3.1） |
| 5 | 7.161 中 `/api/security/token` 的 @Deprecated 是否影响响应（如加告警头） | 仅见注解，未见行为差异 |
