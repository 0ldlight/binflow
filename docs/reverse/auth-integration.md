# 认证集成行为规格（M6 — LDAP / OAuth / SAML 配置）

> 逆向基线：artifactory-pro 7.161.16（`reverse-src/artifactory/`）。
> 置信度标注：`高` = 代码 + JFrog 官方文档双证；`中` = 仅代码可见；`低` = 推断待动态验证。

## 1. LDAP 集成

### 1.1 配置模型：`LdapSetting`

命名空间：`http://artifactory.jfrog.org/xsd/3.5.11`，XML 类型名 `LdapSettingType`。

| 字段 | 类型 | 默认值 | 说明 | 置信度 |
|---|---|---|---|---|
| `key` | string | — | LDAP 配置唯一标识（@XmlID + DiffKey，必填） | 高 |
| `enabled` | boolean | true | 是否启用 | 高 |
| `ldapUrl` | string | — | LDAP 服务器 URL（如 `ldap://ldap.example.com:389`），协议部分自动转小写 | 高 |
| `userDnPattern` | string | — | 用户 DN 模式（如 `uid={0},ou=users,dc=example,dc=com`），非搜索模式时使用 | 高 |
| `search` | SearchPattern | null | 搜索模式（用于替代 `userDnPattern` 的精细搜索配置） | 高 |
| `autoCreateUser` | boolean | true | LDAP 用户首次成功认证后是否自动在 Artifactory 中创建用户 | 高 |
| `emailAttribute` | string | `"mail"` | LDAP 用户对象的 email 属性名 | 高 |
| `ldapPoisoningProtection` | Boolean | true | 是否启用 LDAP 投毒防护（对搜索输入过滤特殊字符） | 高 |
| `allowUserToAccessProfile` | boolean | false | 是否允许用户修改个人资料 | 高 |
| `pagingSupportEnabled` | boolean | true | 是否启用 LDAP 分页支持 | 高 |

**URL 协议变换**：设置 `ldapUrl` 时，协议部分自动转为小写（如 `LDAP://` → `ldap://`），通过 `transformUrlProtocol()` 实现。（置信度：高）

**identicalConfiguration 比较**：比较所有字段的一致性，用于判断配置是否变更（`LdapSetting.identicalConfiguration()`）。（置信度：中）

### 1.2 搜索模式：`SearchPattern`

| 字段 | 类型 | 默认值 | 说明 | 置信度 |
|---|---|---|---|---|
| `searchFilter` | string | — | LDAP 搜索过滤器（如 `(&(objectClass=inetOrgPerson)(uid={0}))`） | 高 |
| `searchBase` | string | — | 搜索基准 DN（如 `ou=users,dc=example,dc=com`） | 高 |
| `searchSubTree` | boolean | true | 是否递归搜索子树 | 高 |
| `managerDn` | string | — | 用于执行搜索的 LDAP 管理员 DN | 高 |
| `managerPassword` | string | — | 管理员密码 | 高 |

**认证流程（两种模式）**：

1. **DN 模式**（`userDnPattern` 非空）：用模式替换 `{0}` 为用户输入的用户名，构建完整 DN，直接绑定认证。
2. **搜索模式**（`search` 非空）：先用 `managerDn`/`managerPassword` 绑定 LDAP 服务器，按 `searchFilter` + `searchBase` 查找用户，找到后使用用户 DN 验证密码。

### 1.3 LDAP 组设置：`LdapGroupSetting`

| 字段 | 类型 | 默认值 | 说明 | 置信度 |
|---|---|---|---|---|
| `name` | string | — | LDAP 组设置名称（唯一标识，DiffKey） | 高 |
| `groupBaseDn` | string | `""` | 组搜索基准 DN | 高 |
| `groupNameAttribute` | string | `"cn"` | 组名的 LDAP 属性名 | 高 |
| `groupMemberAttribute` | string | — | 组成员属性的 LDAP 属性名（如 `member`、`uniqueMember`） | 高 |
| `subTree` | boolean | false | 是否递归搜索组子树 | 高 |
| `forceAttributeSearch` | boolean | false | 是否强制属性搜索 | 高 |
| `filter` | string | — | 组搜索过滤器 | 高 |
| `descriptionAttribute` | string | — | 组描述的 LDAP 属性名 | 高 |
| `enabledLdap` | string | `""` | 关联的 LDAP 配置 key。仅当非空时该组设置被启用 | 高 |
| `strategy` | enum | `STATIC` | 组填充策略 | 高 |

**组填充策略**（`LdapGroupPopulatorStrategies`）：

| 枚举值 | 含义 | 说明 | 置信度 |
|---|---|---|---|
| `HIERARCHICAL` | DN 层级 | 通过 DN 层级推导组成员关系 | 高 |
| `STATIC` | 组包含成员 | 组对象的 `groupMemberAttribute` 直接列出成员 DN（默认策略） | 高 |
| `STATIC` ← 注意反编译值 | "Group contains members" | 组定义中显式列出成员 | 高 |
| `DYNAMIC` | 成员包含组 | 用户对象中通过属性反向推导所属组 | 高 |

**启用判断**：`isEnabled()` 返回 `StringUtils.isNotBlank(enabledLdap)`，即仅当 `enabledLdap` 字段非空时该组设置才生效。

### 1.4 LDAP 用户搜索辅助（`LdapUserSearchesHelper`）

- 根据 `LdapSetting` 创建对应的 LDAP 搜索对象
- 支持 LDAP 投毒防护：当 `ldapPoisoningProtection=true` 时，对搜索输入进行过滤，防止 LDAP 注入攻击（置信度：中）

---

## 2. OAuth / OIDC

### 2.1 实现状态

`OAuthHandlerDefaultImpl` 是 **空桩（stub）实现**，所有方法返回 null 或空集合：

| 方法 | 返回值 | 说明 | 置信度 |
|---|---|---|---|
| `handleLoginResponse(request, response, isPlatformMode)` | null | OAuth 登录回调处理 | 高 |
| `getActiveProviders(request, response, isPlatformMode)` | 空列表 | 获取已激活的 OAuth 提供者 | 高 |
| `handleLogin(method, name, path, request, ssoLoginModel)` | null | OAuth 登录入口 | 高 |
| `getNpmLoginHandler()` | `Optional.empty()` | npm 登录处理器 | 高 |
| `getCreateToken(providerName, basicAuth)` | null | 创建 OAuth Token | 高 |

**结论**：此构建版本的 OAuth/OIDC 为 **skeleton 实现**，实际功能由外部库或 addon 提供（`OAuthHandler` 接口从 `o.a.a.addon.oauth` 引入）。OAuth 集成需要 addon 模式，不在免费/开源版中。（置信度：中）

### 2.2 OAuth 接口声明（`OAuthHandler`）

接口方法签名（来自反编译代码可见的外部引用）：

- `handleLoginResponse(HttpServletRequest, HttpServletResponse, boolean isPlatformMode)` → `URI`
- `getActiveProviders(HttpServletRequest, HttpServletResponse, boolean isPlatformMode)` → `List<OAuthLoginUrl>`
- `handleLogin(String method, String name, String path, HttpServletRequest, SsoLoginModel)` → `Object`
- `getNpmLoginHandler()` → `Optional<String>`
- `getCreateToken(String providerName, String basicAuth)` → `OauthModel`

---

## 3. SAML / SSO

SAML 相关代码未在 `reverse-src/` 中直接找到。JMSP/SSO 支持在 Artifactory 中属于企业版插件（addon）功能。反编译代码中不可见。

---

## 4. 用户自动创建流程

1. 用户通过 LDAP 首次认证成功
2. 如果 `LdapSetting.autoCreateUser = true`（默认），Artifactory 自动在内部创建用户
3. 用户属性映射：
   - 用户名 → LDAP 认证返回的用户名
   - 邮箱 → `LdapSetting.emailAttribute` 指定的 LDAP 属性（默认 `mail`）
4. LDAP 组设置确定用户所属组（按 `strategy` 策略决定组成员关系）
5. 已存在的用户跳过创建步骤

（置信度：中）

---

## 5. 与官方规范的差异/补充

| 项目 | 说明 | 置信度 |
|---|---|---|
| LDAP 投毒防护 | 默认启用（`ldapPoisoningProtection=true`），影响搜索过滤器的构建，具体过滤规则在 `LdapUserSearchesHelper` 中 | 中 |
| 密码加密 | LDAP 密码在配置中存储，需要在序列化/反序列化时加密。`BinaryProviderConfigEncrypterImpl` 表明 JFrog 有主密钥加密机制 | 低 |
| 组设置的 `enabledLdap` 字段 | 与 LdapSetting 的 `enabled` 不同——组设置通过引用关联到具体的 LDAP 配置，需 `enabledLdap` 非空才生效 | 高 |
| OAuth stub | OAuthHandler 是注册的 `@Service` bean，但所有方法返回 null/空——实际 OAuth 功能在 addon 插件中 | 高 |

---

## 待验证清单（低置信度）

1. SAML SSO 配置模型与端点（不在反编译代码中）
2. LDAP 密码的存储加密机制（`BinaryProviderConfigEncrypterImpl` 提示涉及主密钥加密）
3. 用户自动创建时的组分配精确行为（策略 HIERARCHICAL 具体推导规则）
4. LDAP 投毒防护的精确过滤规则（`LdapUserSearchesHelper` 中的字符转义逻辑）
5. OAuth addon 实现的实际行为与端点（需外部 JAR 或平台版）
6. 配置生效时机：修改 LDAP 设置后是否需要重启以生效