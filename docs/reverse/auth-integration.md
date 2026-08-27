# 认证集成行为规格 v2（LDAP / OAuth / SAML 配置 — FR-92 唯一行为基准）

> 逆向基线：artifactory-pro 7.161.16（`reverse-src/artifactory/`，batch1-core + batch3-addons）。
> 置信度标注：`高` = 反编译代码 + JFrog 官方文档双证；`中` = 仅代码可见；`低` = 推断/混淆，待动态验证。
> **v2 重写说明（T-302）**：v1 写于 M6，当时只看了 batch1-core，导致两个错误结论：①「OAuthHandler 为空桩」——实际空桩 `OAuthHandlerDefaultImpl` 只是未授权时的 fallback，真实实现 `OAuthHandlerImpl` 在 batch3-addons 中完整存在；②「SAML 代码缺失」——SAML 处理器、UI REST 服务、Access 模型全部在库中（batch1 + batch3）。本版逐条给出行为出处，两区置信度升级，见 §3.5 / §4.4。
> 官方文档锚点：LDAP `docs.jfrog.com/administration/docs/ldap`；SAML `docs.jfrog.com/administration/docs/saml-sso`；OAuth `docs.jfrog.com/administration/docs/oauth-sso`。

---

## 0. 总体架构事实（先读——决定 BE 票 T-305 的存储与生效设计）

| # | 行为事实 | 出处 | 置信度 |
|---|---|---|---|
| A1 | 7.x 起 LDAP/SAML/OAuth/Crowd/HttpSso 认证设置**不存放在 artifactory.config.xml**，而是存放在平台 Access 服务的配置存储中。Artifactory 侧 UI 服务统一通过 AccessService 客户端读写（`AccessServiceImpl.upsertLdapSetting` → Access auth-settings REST client；`upsertSamlSetting`、`getAllOAuthSettings` 同理）。历史迁移由版本转换器完成（`MoveLdapSettingToConfigServiceConverter` v326、`MoveSamlSettingToAccessConverter` v334、`MigrateSamlEncryptionToAccessConverter` v336） | 代码：`org/artifactory/security/access/AccessServiceImpl`、`org/artifactory/version/converter/v326|v334|v336` | 高 |
| A2 | 配置描述符（artifactory.config.xml 语义）本体存 DB（configs 服务，名 `artifactory.config.xml`），带 revision 乐观并发（`updateConfigIfLastModificationMatch`） | 代码：`CentralConfigServiceImpl.saveDescriptorInternal`/`loadConfigFromStorage` | 高 |
| A3 | UI 保存认证设置 → Access upsert → Artifactory 侧**订阅回调**触发本地认证器重建，**无需重启**（详见 §6） | 代码：`AccessServiceImpl.subscribeToAuthSettingChanges`（Realm: LDAP/SAML/CROWD/HTTPSSO 分发）、`ArtifactoryLdapAuthenticator.onLdapSettingChange` | 高 |
| A4 | 用户名密码认证的主链顺序固定：**ldap → crowd → access(用户密码) → githubEnterprise → db(内部) → rememberMe → accessInternal**（Spring `authenticationManager` 的 provider 列表即优先级） | 代码：`META-INF/spring/security.xml` bean `authenticationManager` 构造列表 | 高（代码）；外部优先于内部库的行为与官方 LDAP 文档 Entra ID 段描述一致（"JPD first attempts to authenticate… internal database"），双证 |
| A5 | 官方文档说明：启用 LDAP/SAML/OAuth **默认禁用内部密码认证**（"Enabling LDAP disables internal password authentication by default"，链接指向 Disable Basic Authentication Method）；关闭 basic auth 的守卫见 §7 补充 | 官方文档三页 Note；代码佐证 `UpdateSecurityConfigService`（basicAuthEnabled=false 且无外部 provider 时仅告警不落 Access 配置） | 高 |
| A6 | SAML/OAuth REST 面挂**许可门控**：`SamlResource` 标注 `@Entitlement(EntitlementType.SAML)`；官方文档注明 SAML 需 Pro X/Enterprise X/Enterprise+、OAuth 需 Enterprise X/Enterprise+ | 代码注解 + 官方文档 Subscription Information | 高 |

**对 BinFlow 的映射含义（非行为，供 ADR-0035 / T-305 参考）**：Artifactory 把「认证配置存储」「认证执行」「UI 配置面」拆在三处（Access / 本地 provider 链 / UI REST）。BinFlow 单体内等价物 = 一张可热更新的认证配置存储 + 保存后触发认证器重建的内部事件 + 上述 UI REST 面。

---

## 1. LDAP 集成

### 1.1 配置模型：LdapSetting（设置条目）

存储侧模型 XML 类型 `LdapSettingType`（命名空间 `http://artifactory.jfrog.org/xsd/3.5.11`）。字段序即下表序（`propOrder`，FE 表单默认序可参照）。

| # | 字段 | 类型 | 默认值 | 行为说明 | 出处 | 置信度 |
|---|---|---|---|---|---|---|
| 1 | `key` | string | — | 设置唯一标识；必须为合法 XML 名（`Verifier.checkXMLName`），create 时查重 | 代码 `LdapSetting`（@XmlID/@DiffKey）+ 官方「Settings Name: The unique ID」 | 高 |
| 2 | `enabled` | boolean | true | 启用开关；仅 enabled 条目参与认证器构建（`getEnabledLdapSettings`） | 代码 + 官方「Enabled: When set, these settings are enabled」 | 高 |
| 3 | `ldapUrl` | string | — | LDAP URL（含 base DN）；写入时协议段自动转小写（`LDAP://`→`ldap://`，setter `transformUrlProtocol`）；URL 中 `://` 后的路径作为搜索 base | 代码 `LdapSetting.setLdapUrl` + 官方 URL 格式说明 | 高 |
| 4 | `userDnPattern` | string | — | 直接绑定模式 DN 模板，`{0}` 运行时替换为用户名；AD/Entra 建议留空 | 代码 + 官方「User DN Pattern」（Entra 段建议留空，双证） | 高 |
| 5 | `search` | SearchPattern | null | 搜索模式子对象，见 §1.2 | 代码 + 官方 Search Filter/Base/SubTree/Manager DN/Password 五字段 | 高 |
| 6 | `autoCreateUser` | boolean | true | 首次登录成功后是否在系统内持久化创建用户（false → 仅 transient 会话用户） | 代码 + 官方「Auto Create System Users」 | 高 |
| 7 | `emailAttribute` | string | `"mail"` | 自动创建/更新用户时的 email 来源属性；每次登录若与库存值不同则更新 | 代码 `LdapUtils.createSimpleUser` + 官方「Email Attribute」 | 高 |
| 8 | `ldapPoisoningProtection` | Boolean | true | LDAP 投毒防护（搜索输入过滤）；UI 文案名「Secure LDAP Search」 | 代码字段 + 官方「Secure LDAP Search: Protects against LDAP poisoning」 | 高（存在与默认值）；过滤具体规则=中（见 §7） |
| 9 | `allowUserToAccessProfile` | boolean | false | 自动创建用户是否可访问个人资料页（生成 API key 等） | 代码 + 官方「Allow Created Users Access To Profile Page」 | 高 |
| 10 | `pagingSupportEnabled` | boolean | true | LDAP 分页（PagedResultsControl）支持；UI 文案名「Used Page Results」 | 代码 + 官方（「requires that the LDAP Server supports a PagedResultsControl configuration」） | 高 |

### 1.2 搜索模式：SearchPattern（`search` 子对象）

| # | 字段 | 类型 | 默认值 | 行为说明 | 出处 | 置信度 |
|---|---|---|---|---|---|---|
| 1 | `searchFilter` | string | — | RFC 2254 过滤器，`{0}`=用户名（如 `uid={0}`；AD 用 `sAMAccountName={0}`） | 代码 `SearchPattern` + 官方 Search Filter | 高 |
| 2 | `searchBase` | string | — | 相对 base DN 的搜索上下文；官方支持 `|` 分隔多 base | 代码 + 官方（多 base 分隔符为官方补充） | 高 |
| 3 | `searchSubTree` | boolean | true | 递归子树搜索 | 代码 + 官方「True by default」 | 高 |
| 4 | `managerDn` | string | — | 执行搜索绑定的管理员 DN；留空则匿名只读绑定（`setAnonymousReadOnly(true)`） | 代码 `ArtifactoryLdapAuthenticator.createSecurityContextWithBaseFilter` + 官方 Manager DN | 高 |
| 5 | `managerPassword` | string | — | 搜索绑定密码；加密存储（见 §1.6） | 代码 + 官方 Manager Password | 高 |

### 1.3 LDAP 组设置：LdapGroupSetting

XML 类型 `LdapGroupSettingType`，10 字段。

| # | 字段 | 类型 | 默认值 | 行为说明 | 出处 | 置信度 |
|---|---|---|---|---|---|---|
| 1 | `name` | string | — | 组设置唯一标识（@DiffKey） | 代码 + 官方 REST 建组 payload `name` | 高 |
| 2 | `groupBaseDn` | string | `""` | 组搜索 base DN | 代码 + 官方组设置截图字段 | 高 |
| 3 | `groupNameAttribute` | string | `"cn"` | 组名属性 | 代码 + 官方（Entra 嵌套组示例 `cn`） | 高 |
| 4 | `groupMemberAttribute` | string | — | 成员属性（STATIC: `member`/`uniqueMember`；DYNAMIC: `memberOf`；嵌套 AD: `member:1.2.840.113556.1.4.1941:`、`msds-memberOfTransitive`） | 代码 + 官方两种策略示例 | 高 |
| 5 | `subTree` | boolean | false | 组搜索是否递归子树（注意：与 SearchPattern.searchSubTree 默认值相反） | 代码 | 高 |
| 6 | `filter` | string | — | 组对象过滤器（如 `(objectClass=group)`） | 代码 + 官方示例 | 高 |
| 7 | `descriptionAttribute` | string | — | 组描述属性 | 代码 | 高 |
| 8 | `enabledLdap` | string | `""` | 关联的 LDAP 设置 key；**非空才启用该组设置**（`isEnabled()` = isNotBlank(enabledLdap)） | 代码 + 官方「Make sure that LDAP group settings is enabled…in order for your settings to become effective」 | 高 |
| 9 | `strategy` | enum | `STATIC` | HIERARCHICAL / STATIC / DYNAMIC（见下表） | 代码 `LdapGroupPopulatorStrategies` + 官方三种策略定义 | 高 |
| 10 | `forceAttributeSearch` | boolean | false | 强制属性动态搜索（官方：7.57.2 后经 REST `force_attribute_search:true` 设置） | 代码 + 官方 | 高 |

**组映射策略语义**（代码枚举 + 官方双证，高）：

| 策略 | 语义 |
|---|---|
| `STATIC`（默认） | 组对象持有成员 DN（groupOfNames/groupOfUniqueNames 的 member/uniqueMember） |
| `DYNAMIC` | 用户对象持有组 DN/组名（自定义属性或 memberOf；OpenLDAP 需 7.37.17+） |
| `HIERARCHICAL` | 由用户 DN 的 ou 层级推导组（`uid=user1,ou=developers,ou=uk,…` → 组 uk、developers） |

（v1 此处有重复枚举行，系反编译枚举注释误读，已修正。）

**LDAP 组 UI 端点**（`LdapGroupResource`，base `/ui/api/v1/admin/security/ldapgroups`）：GET 列表/单条、PUT 更新、POST 创建、DELETE、`refresh[/{name}]` 触发组同步刷新、`import` 导入组、`strategy` 查询映射策略。官方补充：组通过 REST 直接建时 realm=`ldap` 且组名必须全小写、`realmAttributes` 携带 `ldapGroupName/groupsStrategy/groupDn`。置信度：高。

### 1.4 LDAP 设置 UI 端点表（FE 票 T-307 直接消费）

Base path：`/ui/api/v1/admin/security/ldap`，全部 `@RolesAllowed("admin")`。

| 方法 | 路径 | 请求体 | 成功 | 失败 | 出处 | 置信度 |
|---|---|---|---|---|---|---|
| GET | `/ldap` | — | 设置列表（**view 模式**：每条仅 `key`+`ldapUrl`，其余字段被 `@IgnoreSpecialFields` 剔除） | — | `LdapSettingResource`+`GetLdapSettingsService` | 高 |
| GET | `/ldap/{id}` | — | 单条完整字段（edit 模式；`managerPassword` 以加密形态返回） | — | 同上 | 高 |
| POST | `/ldap` | LdapSettingModel | info「Successfully created LDAP settings '{key}'」 | error：key 非 XML 名 / key 已存在（"Ldap with key {k} already exists"） | `CreateLdapSettingsService`+`LdapSettingsValidator.validateCreate` | 高 |
| PUT | `/ldap/{id}` | LdapSettingModel | info「Successfully updated LDAP settings '{key}'」 | error：路径 key 与体 key 不一致（"Key in path and object are different"）/ 密码占位符未改（"LDAP password needs to be re-entered to allow changes in LDAP settings"） | `UpdateLdapSettingsService`+`validateUpdate` | 高 |
| DELETE | `/ldap/{id}` | — | info「LDAP {id} successfully deleted」 | — | `DeleteLdapSettingsService` | 高 |
| POST | `/ldap/test/{id}` | LdapSettingModel + `testUsername`/`testPassword` | info「Successfully connected and authenticated the test user」 | error=status 汇总消息 + 逐条 warn/info（详见 §1.5） | `TestLdapSettingsService` | 高 |
| POST | `/ldap/reorder` | `["key3","key1",…]`（key 列表） | 200 静默（无消息） | 空列表 → 直接忽略不报错 | `ReorderLdapSettingsService` | 高 |

**排序语义**：列表顺序 = 认证尝试顺序（§1.5）；reorder 以 key 列表重排 Access 侧存储顺序。

### 1.5 认证流程语义（BE 行为基准）

1. 登录进入 provider 链（§0 A4 顺序），ldap provider 先行。
2. 对每个 enabled 的 LDAP 设置**按存储顺序**逐一尝试绑定认证（LinkedHashMap 插入序 = `getEnabledLdapSettings` 返回序）：第一个成功者胜出并记录所用 setting key。
3. 单个设置认证抛异常时**记住最后一个异常并继续尝试下一个设置**；全部失败：有异常 → 重抛该异常；无异常（如全部返回 null）→ `AuthenticationServiceException("LDAP service misconfigured")`。
4. 无任何 LDAP 设置或 LDAP 未启用 → `"No LDAP service configured"`，provider 链继续走下家（crowd → access → db）。
5. 两种绑定模式：`userDnPattern` 非空 → 直接 DN 绑定；`search` 非空 → manager（或匿名）先按 filter+base 搜用户 DN 再绑定验密。**两者同时配置时仍可认证**，但失败时打 warn 提示互斥（"you have configured direct user binding and manager-based search, which are usually mutually exclusive. For AD leave the User DN Pattern field empty."）。
6. 连接参数（可经 system properties 覆盖，默认值代码与官方文档双证，高）：connect timeout 10000ms（`artifactory.security.ldap.connect.timeoutMillis`）、read timeout 15000ms（`…socket.timeoutMillis`）、referral 策略 `follow`（`…referralStrategy`）、连接池空闲超时可选（`…pool.timeoutMillis`）。
7. REST（非 UI）认证有外部认证缓存，默认 300s（`artifactory.security.authentication.cache.idleTimeSecs`，官方文档）；仅 REST 生效，UI 不缓存。
8. 认证失败清理（中）：非 BadCredentials/Communication 类失败且 `ldapCleanGroupOnFail` 开启、且用户已不存在于任何 LDAP 设置时，移除该用户 realm=ldap 的组关联（`LdapUtils.removeUserLdapRelatedGroups`）。

出处：`ArtifactoryLdapAuthenticator.authenticate/createBindAuthenticators`、`ArtifactoryLdapAuthenticationProvider.authenticate`。整体置信度：高（1–7）；8=中（开关默认值未在代码中直接读到）。

### 1.6 测试连接交互形态（FE 票 T-307）

- 入口：设置编辑表单内「Test LDAP Connection」动作 → `POST /ldap/test/{id}`，体为**当前表单值 + testUsername + testPassword**（服务端用的是提交的表单值，不落库）。
- `testUsername`/`testPassword` 任一缺失 → error「Please enter test username and password to test the LDAP settings」。
- 服务端先把提交的 `managerPassword` 解密（`CryptoHelper.decryptIfNeeded`）再执行真实连接+绑定。
- 成功：单条 info「Successfully connected and authenticated the test user」。
- 失败：error = status 汇总消息，并附逐条 warn / info 条目（BasicStatusHolder 的 WARNING/INFO entries）——FE 应展示为消息列表。

出处：`TestLdapSettingsService.execute/testLdapConnection` + 官方「Test LDAP Connection: Run a LDAP test to validate your settings are correct」。置信度：高。

**密码掩码语义（跨协议通用，FE 必须实现）**：编辑读取时 `managerPassword` 返回加密串；前端以 20 个星号 `********************` 占位展示；保存时若原样提交该占位符，PUT 被 400 拒绝并提示重输（`LdapSettingsValidator.validatePassword` 精确比对占位符常量）。置信度：高。

### 1.7 LDAP 用户自动创建（与 §5 通用流程合并描述）

登录成功 → `findOrCreateExternalAuthUser(username, !autoCreateUser, allowUserToAccessProfile)`：用户名小写化查找；不存在则创建（NameValidator 校验，非法名抛 InvalidNameException）；`passwordDisabled=true`；默认新用户组；`autoCreateUser=false` → transient（不落库，仅本次会话，权限=默认组）；email 取 `emailAttribute` 属性且每次登录比对更新；realm 标记 `ldap`；组关联经 LdapGroupAddon 按策略填充。出处：`SecurityServiceImpl.findOrCreateExternalUser/autoCreateUser`、`LdapUtils.createSimpleUser`、官方「Make sure users log in」Note。置信度：高。

---

## 2. OAuth / OIDC SSO

### 2.1 一般设置模型（OAuthUIModel，GET/POST `/ui/api/v1/admin/security/oauth` 的体）

| # | 字段 | 类型 | 默认 | 行为说明 | 出处 | 置信度 |
|---|---|---|---|---|---|---|
| 1 | `enabled` | boolean | false | 「Enable OAuth」总开关 | 代码 `OAuthUIModel`+descriptor `OAuthSettings.enableIntegration`（默认 false）+ 官方 General OAuth Setting | 高 |
| 2 | `persistUsers` | boolean | false | 「Auto Create System Users」；保存时**摊平写到全部 provider**（见 §2.3 流程 2） | 代码 `UpdateOrCreateOAuthSettings` + 官方 | 高 |
| 3 | `allowUserToAccessProfile` | boolean | false | 「Allow Created Users Access To Profile Page」；同样摊平到全部 provider | 代码 + 官方 | 高 |
| 4 | `defaultNpm` | string | — | 「Default Provider」：npm 等客户端登录所用 provider 名；仅 GitHub Enterprise 类型可任默认（官方） | 代码 + 官方（"Currently, only a GitHub Enterprise OAuth provider may be defined as the Default Provider"） | 高 |
| 5 | `providers` | list | [] | provider 明细列表（§2.2） | 代码 `GetOAuthSettings.createUIModel` | 高 |
| 6 | `availableTypes` | list | 计算值 | 可用 provider 类型元数据（displayName/type/mandatoryFields/fieldsValues/fieldHolders），GET 时由枚举计算，**POST 时忽略** | 代码 `OAuthUIProvidersTypeEnum.getProviderInfo` | 高 |

### 2.2 Provider 条目模型（OAuthProviderUIModel / 存储侧 OAuthProviderSettings）

UI 12 字段；存储侧另有 `central`（平台中心下发 provider 标记，UI 不下发，保存固定 false）。

| # | 字段 | 类型 | 默认 | 行为说明 | 出处 | 置信度 |
|---|---|---|---|---|---|---|
| 1 | `name` | string | — | provider 逻辑名，JPD 内唯一；必填 | 代码（@DiffKey）+ 官方「Provider Name…must be unique」 | 高 |
| 2 | `enabled` | boolean | false | 启用后出现在登录页 | 代码 + 官方 | 高 |
| 3 | `providerType` | string | — | `github` / `google` / `cloudfoundry` / `openId`（小写集合校验） | 代码枚举 + 官方（GitHub/Git Enterprise/Google/OpenID/Cloud Foundry） | 高 |
| 4 | `id` | string | — | Client ID | 代码 + 官方 | 高 |
| 5 | `secret` | string | — | Client Secret；保存时 `CryptoHelper.encryptIfNeeded` 后入库 | 代码 `AddOAuthProviderSettings` | 高（加密入库）；GET 回显形态=中（见 §2.3 note） |
| 6 | `apiUrl` | string | — | 拉取用户资料的 API URL | 代码 + 官方 API URL 列 | 高 |
| 7 | `authUrl` | string | — | 授权跳转 URL；官方支持带 query 参数 | 代码 + 官方（Use Query Params） | 高 |
| 8 | `tokenUrl` | string | — | 换 token URL | 代码 + 官方 | 高 |
| 9 | `basicUrl` | string | — | GitHub(GHE) 基础 URL（docker/npm login 用） | 代码 + 官方 Basic URL | 高 |
| 10 | `domain` | string | — | Google=域过滤；GitHub=Organization（github 类型必填，见 §2.3） | 代码 + 官方 Domain/说明（留空视为安全漏洞） | 高 |
| 11 | `pkce` | boolean | false | PKCE 流程；开启后 Secret 字段禁用（官方） | 代码 + 官方「PKCE Enabled…The Secret field is disabled」 | 高 |
| 12 | `useDefaultProxy` | boolean | false | 用系统默认代理；SaaS(Aol) 环境拒绝 | 代码（"Proxy is not available in Saas"）+ 官方 | 高 |

**类型默认值矩阵**（GET `availableTypes` 下发，FE 用于表单预填/占位；代码 `OAuthUIProvidersTypeEnum` 与官方 provider 字段表双证，高）：

| type | 必填字段 | 官方域名默认值 | 内网占位 |
|---|---|---|---|
| `github` | apiUrl, authUrl, tokenUrl, basicUrl | `https://api.github.com/user`、`https://github.com/login/oauth/authorize`、`https://github.com/login/oauth/access_token`、`https://github.com/` | `<base_url>/api/v3/user`、`<base_url>/login/oauth/authorize`、`<base_url>/login/oauth/access_token`、— |
| `google` | apiUrl, authUrl, tokenUrl, domain | `https://www.googleapis.com/oauth2/v1/userinfo`、`https://accounts.google.com/o/oauth2/auth`、`https://www.googleapis.com/oauth2/v3/token` | — |
| `cloudfoundry` | apiUrl, authUrl, tokenUrl | — | `<base_url>/userinfo`、`<base_url>/oauth/authorize`、`<base_url>/oauth/token` |
| `openId` | apiUrl, authUrl, tokenUrl | — | — |

### 2.3 OAuth UI 端点表与保存语义

Base：`/ui/api/v1/admin/security/oauth`，类级 `@RolesAllowed({"admin","user"})`，但配置操作均再标 `admin`。

| 方法 | 路径 | 语义 | 成功/失败消息 | 出处 | 置信度 |
|---|---|---|---|---|---|
| GET | `/oauth` | 读一般设置+providers+availableTypes | `persistUsers`/`allowUserToAccessProfile` 取**第一个 provider** 的存储值回显 | `GetOAuthSettings` | 高 |
| POST | `/oauth` | 保存一般设置 | 「Successfully updated OAuth settings」/「Error occurred while updating OAuth settings, please review the log」 | `UpdateOrCreateOAuthSettings` | 高 |
| GET | `/oauth/central` | 平台中心 OAuth 设置 | — | `GetCentralOAuthSettings` | 中 |
| GET | `/oauth/user/tokens` | 当前用户 token 列表（user+admin） | — | `GetOAuthTokensForUser` | 高 |
| PUT | `/oauth/provider` | **新增** provider | 见下方校验链 | `AddOAuthProviderSettings` | 高 |
| POST | `/oauth/provider` | **更新** provider | 同上风格 | `UpdateOAuthProviderSettings` | 高 |
| DELETE | `/oauth/provider/{name}` | 删除 provider | — | `DeleteOAuthProviderSettings` | 高 |
| DELETE | `/oauth/user/tokens/{userName}/{providerName}` | 删用户绑定 token | — | `DeleteOAuthUserToken` | 高 |

**POST /oauth（保存一般设置）语义**（高）：
1. 仅当 enable 或 defaultNpm 与现状不同才 upsert OAuthGeneralSettings（避免无谓写）。
2. `persistUsers`/`allowUserToAccessProfile` 是**全局摊平字段**：对所有现存 provider 逐个检查不一致并批量 upsert（存储侧每 provider 都带 autoUserCreation/allowUserToAccessProfile）。
3. 异常一律折叠为「Error occurred while updating OAuth settings, please review the log」。

**PUT /oauth/provider（新增）校验链（顺序即行为，全部 error 级消息原文，高）**：
1. 重名（与现存任一 provider 同名）→「Couldn't add provider, already exists.」
2. SaaS + useDefaultProxy →「Proxy is not available in Saas」
3. name 空 →「Missing provider name」
4. providerType 空 →「Missing provider Type」
5. providerType 不在 {github,google,cloudfoundry,openid} →「Invalid provider Type」
6. github 且 domain(Organization) 空 →「Organization cannot be empty for github provider settings」
7. 通过后：secret 加密入库；新 provider 继承现存第一个 provider 的 autoUserCreation/allowUserToAccessProfile；成功「Successfully added OAuth provider {name}」。

note（中）：GET 回显的 `secret` 为 Access 存储值原样转发（`toModel` 未见解密调用）——回显是否为掩码取决于 Access 侧行为，未在反编译内闭环，FE 先按「可能为密文/掩码」处理，待动态验证。

### 2.4 OAuth 登录面行为（摘要，供登录页对齐）

- 登录回调端点常量：平台 UI `/ui/api/v1/auth/oauth2/loginResponse`、Artifactory API `/api/oauth2/loginResponse`（`OAuthHandlerImpl` 常量；官方 OAuth 设置回调 URL 双证，高）。
- 启用后登录页展示各 enabled provider 按钮（官方 OAuth SSO Usage）；点击跳 provider authUrl。
- 状态校验：state cookie 校验失败/回调带 error 参数 → 断言异常（`validateOAuthState`/`assertError`）。
- GitHub Enterprise 支持 docker/npm 登录（`getNpmLoginHandler`、basicUrl），与官方 provider 能力矩阵一致（高）。

### 2.5 实现状态（v1 低置信区之一——已升级）

**结论（高）**：OAuth 是**双层实现**：
- batch1-core 的 `OAuthHandlerDefaultImpl` 是无授权/无 addon 时的 fallback 空桩（方法返回 null/空）——v1 只看到这个，误判为「整体是 stub」。
- batch3-addons 的 `OAuthHandlerImpl`（`o.a.a.addon.sso.oauth`）是完整实现：provider 列表组装（私有 provider + 平台中心 provider 合流；Access OAuth realm 开启时私有 provider 让位）、登录 URL 生成、state 管理、token 交换、用户资料拉取、npm 登录处理、`getCreateToken`（basic auth 换 OAuth 身份 token）。
- 官方文档锚定 OAuth 为 Enterprise X/Enterprise+ 许可功能，与 addon 分层一致。

---

## 3. SAML / SSO

### 3.1 配置模型（UI wire 模型 `Saml`，13 字段；GET/PUT `/ui/api/v1/admin/security/saml/config` 的体）

| # | 字段 | 类型 | 默认 | UI 文案（官方） | 行为说明 | 出处 | 置信度 |
|---|---|---|---|---|---|---|---|
| 1 | `enableIntegration` | boolean | false | Enable SAML Integration | 总开关 | 代码 + 官方 | 高 |
| 2 | `loginUrl` | string | — | SAML Login URL | IdP SSO 端点 | 代码 + 官方 | 高 |
| 3 | `logoutUrl` | string | — | SAML Logout URL | 可填 `{baseUrl}` 动态回跳（官方多节点建议） | 代码 + 官方 | 高 |
| 4 | `serviceProviderName` | string | — | SAML Service Provider Name | entityID，须配 Custom Base URL | 代码 + 官方（7.98.7 起 Base URL 缺失会 500，官方 Breaking Change） | 高 |
| 5 | `certificate` | string | — | SAML Certificate | IdP X.509 公钥证书 | 代码 + 官方 | 高 |
| 6 | `useEncryptedAssertion` | boolean | false | Use Encrypted Assertion | 开启时服务端生成/复用 SP 加密密钥对（§3.3 流程 2） | 代码 `UpdateSamlService` + 官方 | 高 |
| 7 | `syncGroups` | boolean | false | Auto Associate Groups | 按组属性同步组；7.116.0 起持久化且只增不删（官方） | 代码 + 官方 | 高 |
| 8 | `groupAttribute` | string | — | Group Attribute | 组属性名；**大小写敏感**匹配既有组（官方） | 代码 + 官方 | 高 |
| 9 | `emailAttribute` | string | — | Email Attribute | 自动建用户/内部用户存在时写 email；官方：触发 email OTP 验证 | 代码 + 官方 | 高 |
| 10 | `noAutoUserCreation` | boolean | **true** | Auto Create Artifactory Users（wire 名为否定式，默认 true = 默认不自动建） | true → 用户 transient；false → 落库持久化。命名陷阱见 §3.4 | 代码（builder 默认）+ 官方 | 高 |
| 11 | `allowUserToAccessProfile` | boolean | false | Allow Created Users Access To Profile Page | 资料页/API key 访问 | 代码 + 官方 | 高 |
| 12 | `autoRedirect` | boolean | false | Auto Redirect Login Link to SAML Login | 会话过期自动跳 SAML（手动登出不触发，官方） | 代码 + 官方 | 高 |
| 13 | `verifyAudienceRestriction` | boolean | **true** | （隐含校验） | 受众校验开关，默认开 | 代码（builder 默认）+ 官方 Verify audience restriction | 高 |

存储侧（Access `SamlSetting`，15 字段）额外含：`name`（多配置名，官方新版多 SAML 配置用，本版 Artifactory UI 未暴露）与 `nameIdAttribute`。Artifactory 侧单配置 upsert（`accessService.upsertSamlSetting`）。置信度：高（代码）；多配置 UI 为官方 Cloud 灰度能力（官方 7.83.1 Note），本版 UI 面未含 → 中。

### 3.2 SAML UI 端点表

| 方法 | 路径 | 语义 | 成功/失败 | 出处 | 置信度 |
|---|---|---|---|---|---|
| GET | `/ui/api/v1/admin/security/saml/config` | 读配置；**无配置时返回空对象 `{}`**（EmptyModel）——FE 需容忍空体 | — | `GetSamlService` | 高 |
| PUT | `/ui/api/v1/admin/security/saml/config` | 保存（全量） | info「Successfully updated SAML SSO settings」/ error「Error occurred while updating SAML settings, please review the log」 | `UpdateSamlService` | 高 |
| GET | `…/saml/config/key/public`（text/plain） | 下载 SP 加密公钥证书 | — | `GetSAMLPublicCertificateForEncryptionService` | 高 |
| PUT | `…/saml/config/key/public/regenerate`（text/plain） | 重新生成加密公钥（影响所有启用加密断言的配置，官方） | — | `RegenerateSAMLPublicCertificateForEncryptionService` | 高 |
| GET | `/ui/api/v1/auth/saml/loginRequest` | 生成 AuthnRequest 跳 IdP | — | `SamlLoginLogoutResource`+`GetSamlLoginRequestService` | 高 |
| POST | `/ui/api/v1/auth/saml/loginResponse` | ACS；恒 307 重定向到平台统一 loginResponse 端点 | — | `SamlLoginLogoutResource`（`SAML_LOGIN_ENDPOINT` 常量） | 高 |
| POST | `/ui/api/v1/auth/saml/loginResponsePlatform` | 平台 ACS 实际处理 | — | 同上 | 高 |
| GET | `/ui/api/v1/auth/saml/logoutRequest`（text/plain） | 生成 LogoutRequest | — | `GetSamlLogoutRequestService` | 高 |
| GET | `/ui/api/v1/auth/issaml` | 登录页探测 SAML 是否启用（决定 SSO 入口展示） | — | `AuthResource`+`IsSamlAuthentication` | 高 |

`/saml/config*` 端点挂 `@RolesAllowed("admin")` + `@Entitlement(EntitlementType.SAML)`（许可门控，§0 A6）。

### 3.3 保存流程语义（PUT saml/config）

1. enableIntegration=true → 先 `createCertificate(certificate)`（解析/落 IdP 证书，失败进统一 error）。
2. useEncryptedAssertion=true → 额外 `createStoreAndGetKeyPair(false)` 生成/装载 SP 加密密钥对。
3. UI 模型映射为 Access 模型后 upsert；任何异常统一折叠为「Error occurred while updating SAML settings, please review the log」（原始异常只进服务端日志）。
4. GET 回程：Access 模型 → UI 模型 1:1 映射（无配置 → `{}`）。

出处：`UpdateSamlService.updateSamlSetting`。置信度：高。

### 3.4 登录流程语义（摘要）

POST loginResponse → 禁用则抛「Saml integration is disabled」→ InResponseTo 与本地（HA 下跨节点缓存）请求 ID 比对（常量 `samlInResponseToValidationEnable` 控制）→ `verifyAudienceRestriction(baseUrl)`（开关默认开）→ 验签（assertion 签名必须，官方：signed assertion mandatory、signed logout 不支持）→ `createOrUpdateUserIfNeeded`：`findOrCreateExternalAuthUser(username, !noAutoUserCreation值, true)`（注意 §3.1 #10 的否定式命名——Access 存储字段名叫 `autoUserCreation` 但承载的是 UI `noAutoUserCreation` 原值，mapper 双向均不取反；行为上等价于「no-create 标志」，命名陷阱勿照抄进 Go 字段名）→ disabledUiAccess 用户抛「UI Access is Disabled For This User」→ 更新 realm=saml、email、lastLogin、组同步（syncGroups 时）→ 建会话。绑定：HTTP-Redirect 发请求、HTTP-POST 收响应（官方 Profiles and Bindings）。置信度：高。

### 3.5 实现状态（v1 低置信区之二——已升级）

**结论（高）**：SAML 代码**完整在库**，v1「SAML 代码缺失」结论作废：
- batch3-addons `o.a.a.addon.sso.saml`：`SamlHandlerImpl`（登录/登出/校验/用户落库）、`SamlResolver(Samactory)`、`SamlUrlManager`、`TimesVerifyer`。
- batch1-core：UI REST 层全套（§3.2）、`SamlSsoAddon` 接口、`SamlRedirectionHandler`、Access 客户端模型 `SamlSetting/SamlGeneralSetting/SamlEncryptedAssertion`、`SamlGatewayResource`。
- 许可：Enterprise X/+（SaaS）、Pro X/Enterprise X/+（自管）——官方 Subscription Information + `@Entitlement` 注解双证。
- 仍属 addon 层（license-gated），OSS 构建里等同禁用——这与 v1 直觉一致，但「代码缺失」不成立。

---

## 4. 用户自动创建（跨协议统一语义）

所有外部 realm（ldap/saml/oauth/crowd/httpsso）共用同一创建入口（高）：

1. 查找（用户名小写）→ 未找到 → `autoCreateUser`（按协议各自开关）决定 transient vs 持久化。
2. 创建内容：username 小写、`passwordDisabled=true`（无内部密码）、`updatableProfile`=allowUserToAccessProfile、默认新用户组。
3. 用户名合法性校验失败 → InvalidNameException（对外表现为认证失败）。
4. 已存在用户：每次登录刷新 email（LDAP emailAttribute / SAML emailAttribute）与 lastLogin；组按各协议策略刷新。
5. transient 用户（no-auto-create）：仅当次会话有效，权限=默认组；要精细授权需手工建同名内部用户（官方 SAML Auto Create 说明）。

各协议开关载体：LDAP=`LdapSetting.autoCreateUser`（默认 true）；SAML=`noAutoUserCreation`（默认 true=不建）；OAuth=`persistUsers`（摊平到 provider 的 autoUserCreation）；Crowd/HttpSso=`noAutoUserCreation`（migration handler 可见）。

出处：`SecurityServiceImpl.findOrCreateExternalUser/autoCreateUser`、各 handler 调用点（`LdapUtils`、`SamlHandlerImpl`、`ArtifactoryHttpSsoAuthenticationFilter`、`ArtifactoryCrowdClientImpl`）+ 官方三文档。置信度：高。

---

## 5. 优先级与生效语义（ADR-0035 关键行为事实）

### 5.1 认证优先级（高）

- 用户名密码链：**ldap → crowd → access → githubEnterprise → db → rememberMe → accessInternal**（§0 A4）。外部 realm 先于内部 DB；官方 Entra ID 段落佐证「先外部后内部库」。
- LDAP 内部多设置：存储顺序即尝试顺序，首成功胜；失败重抛最后异常或「LDAP service misconfigured」（§1.5）。
- 单一 realm 独占模式：当 realm 进入 Access「enabled realms」（禁内部密码模式）时，本地 provider 让位给 Access 委托认证（`supports()` 检查 `isRealmAuthenticationEnabled`）。官方：「Enabling LDAP/SAML/OAuth disables internal password authentication by default」。此条解读=中（机制代码可见，「让位后由 Access 全权处理」的细节在 Access 服务端，反编译内不可见）。
- 登录页 SSO 入口：`GET /auth/issaml` 探测；SAML autoRedirect 只在会话过期时生效。

### 5.2 配置保存 → 生效（ADR-0035 核心，高置信）

**结论：Artifactory 7.161 的认证配置变更为「保存即生效」，无需重启。** 三条链路：

1. **中央描述符链**（security general 等仍走 descriptor 的项）：`saveEditedDescriptorAndReload` → 校验 → 持久化（DB，revision 乐观锁，冲突自动重试）→ `reloadConfiguration` → 应用上下文对**所有 @Reloadable bean 做差异路由重载**：计算 old/new descriptor 的 diff 路径，仅 `listenOn` 命中变更键的 bean 被调 `reload(oldDescriptor, configDiff)`（如 `ArtifactoryLdapAuthenticator` 监听 `security`+`proxies`）。重载过程同步执行，期间上下文短暂置 not-ready。
2. **Access 配置链**（LDAP/SAML/OAuth/Crowd/HttpSso 设置本体）：UI 保存 → Access upsert → Artifactory 经**变更订阅**（`subscribeToAuthSettingChanges`，Realm 分发）收到回调 → 重新初始化本地认证器（如 `onLdapSettingChange` → 重建 BindAuthenticator 映射）。HA 下多节点同理订阅。
3. **兜底**：认证器映射为空且 LDAP 启用时，认证路径上懒初始化（`authenticate()` 内 `init()`）。

边界（中）：重启不需要，但**已建立会话/已发 token 不因配置变更失效**；认证缓存（REST 300s）内的旧结果在缓存过期前仍可用；「not-ready」窗口内请求可能短暂失败重试。历史行为（≤6.x 直接改 artifactory.config.xml 需重启）不再适用——7.x 配置存 DB 且只应经 UI/REST 改。

出处：`CentralConfigServiceImpl.saveDescriptor/saveDescriptorInternal/reloadConfiguration/callReload`、`ArtifactoryApplicationContext.reload`、`ArtifactoryLdapAuthenticator.reload/onLdapSettingChange`、`AccessServiceImpl.subscribeToAuthSettingChanges`。主链=高；边界项=中（token 失效与缓存行为未逐行核）。

---

## 6. Admin > Security 三协议配置页形态（FE 票 T-307 消费）

**证据等级声明**：reverse-src 的 rtui 前端包是微前端壳，未含 admin security 页面组件（检索 `ldapSetting/userDnPattern/saml/config` 零命中）——**字段序/分组以下列可佐证材料合成**：① REST DTO `propOrder`/字段声明序（wire 序）；② 官方文档字段表序（UI 呈现序）；③ 服务端校验链序（交互序）。分组以官方页面结构为准。缺动态截图佐证的项标中。

| 页面 | 分组与字段序（官方文档表序 ↔ wire 序） | 测试连接/特殊交互 | 置信度 |
|---|---|---|---|
| LDAP 设置 | 表单序：Enabled → Settings Name(key) → LDAP URL → Auto Create System Users → Allow Created Users Access To Profile Page → Used Page Results(paging) → User DN Pattern → Email Attribute → [Search 子组] Search Filter → Search Base → Secure LDAP Search(poisoning) → Search Sub Tree → Manager DN → Manager Password。列表页（GET 列表 view 模式）仅显 key+ldapUrl，支持多行**拖拽排序**（POST reorder） | 「Test LDAP Connection」按钮 → 弹测试账号输入（testUsername/testPassword 随表单提交）→ 成功/失败消息列表（§1.6）；Manager Password 掩码 20 星、原样提交被拒 | 高（字段）· 中（拖拽控件形态——端点在、控件样式未佐证） |
| LDAP 组设置 | 组设置独立子面板：Group Base DN → Group Name Attribute → Group Member Attribute → Sub Tree → Filter → Description Attribute → Strategy(STATIC/DYNAMIC/HIERARCHICAL 单选) → Enable 关联 LDAP 设置(enabledLdap)；「Synchronize LDAP Groups」refresh→列表→import | Refresh 拉组列表 → 勾选 Import；REST 建组须小写 | 高（字段）· 中（交互细节） |
| OAuth SSO | General 组：Enable OAuth(enabled) → Auto Create System Users(persistUsers) → Default Provider(defaultNpm，下拉列 github 类型 provider) → Allow Created Users Access To Profile Page。Providers 组：列表 + New 弹窗（Provider Type 先选 → 按 availableTypes 预填/占位 URL；PKCE 开时 Secret 禁用；github 必填 Organization） | 无 test 连接动作（反编译与官方文档均无 OAuth 测试连接）；secret 掩码语义待动态验证 | 高（字段）· 中（掩码交互） |
| SAML SSO | 单表单：Enable SAML Integration → Using Encrypted Assertion（附「下载公钥证书」与「刷新证书」动作 = GET/PUT key/public）→ Service Provider Name → SAML Login URL → SAML Logout URL → SAML Certificate → Auto Associate Groups(syncGroups) → Group Attribute → Email Attribute → Auto Create Artifactory Users(=noAutoUserCreation wire 反义) → Allow Created Users Access To Profile Page → Auto Redirect Login Link to SAML Login。无配置时 GET 返回 `{}` → 表单全默认 | 证书下载/再生成为独立动作按钮；页面顶部有总开关与单配置开关的区别（本版 Artifactory UI=单配置） | 高（字段）· 中（新多配置 UI，仅官方 Cloud 文档佐证，本版未含） |

---

## 7. 与官方规范的差异/补充（「此条补充官方规范」）

| 项 | 行为 | 置信度 |
|---|---|---|
| LDAP 密码占位符协议 | 官方文档未写：编辑回显加密串 + FE 20 星占位 + PUT 原样提交占位符被服务端拒绝（错误消息原文见 §1.4） | 高 |
| OAuth 一般设置摊平语义 | 官方未写 persistUsers/allowUserToAccessProfile 存储上摊平到每个 provider、GET 从第一个 provider 回显 | 高 |
| OAuth 新增校验链与错误消息原文 | 官方未列 6 步校验顺序与消息（§2.3） | 高 |
| SAML 保存的证书前置处理 | 官方未写：enable 时先解析证书、useEncryptedAssertion 时先生成密钥对，失败统一 error 文案 | 高 |
| SAML `noAutoUserCreation` ↔ Access `autoUserCreation` 不取反映射 | 官方未写（UI 文案是正向 Auto Create，wire 是否定式且默认 true）——Go 实现建议单侧命名（如 `autoCreateUser`）并在 DTO 边界转换 | 高 |
| SAML GET 空态 `{}` | 官方未写 | 高 |
| reorder 空列表静默忽略 | 官方未写 | 高 |
| LDAP 投毒防护的具体过滤规则 | 未定位实现体（过滤逻辑在 spring-security-ldap 模板/LdapUserSearchesHelper 调用链内，本批未含完整实现）——仅存在性与默认开=高，规则=低待验证 | 存在性 高 / 规则 低 |
| REST 认证缓存 300s | 官方已写（补充：仅 REST、Docker 需 v2 token）——此处仅引用 | 高 |

---

## 8. 待验证清单（动态验证项）

1. **LDAP 投毒防护字符过滤规则**（`ldapPoisoningProtection` 的具体转义/黑名单）——低，需动态或补齐 spring-security-ldap 反编译。
2. **OAuth GET 回显 secret 形态**（明文/密文/掩码）——中，curl 实测 GET `/ui/api/v1/admin/security/oauth`。
3. **`ldapCleanGroupOnFail` 默认值**（失败清组行为开关）——中。
4. **realm 独占模式下 Access 委托认证的完整行为**（enabled realms 配置后本地 provider 让位的端到端路径在 Access 服务端）——中。
5. **SAML 多配置（name 字段）何时在自管 UI 放开**——中（官方 Cloud 灰度 7.83.1+，本版代码未暴露 UI 面）。
6. **配置重载 not-ready 窗口的用户可见影响**（并发请求失败重试形态）——低。
7. token/会话在认证配置变更后的失效语义（§5.2 边界项）——中。

---

## 附：v1 → v2 结论变更对照

| v1 结论 | v2 结论 | 依据 |
|---|---|---|
| OAuthHandler 为空桩、功能在外部 addon | 空桩仅为 fallback；batch3 `OAuthHandlerImpl` 完整实装 | §2.5 |
| SAML 代码缺失 | UI REST + handler + Access 模型齐全（batch1+batch3），许可门控 | §3.5 |
| LdapGroupPopulatorStrategies 有重复 STATIC | 枚举三值 HIERARCHICAL/STATIC/DYNAMIC，v1 重复行为注释误读 | §1.3 |
| 配置生效时机待验证（低） | 保存即生效：descriptor diff-reload 链 + Access 订阅链，双链高置信 | §5.2 |
| 密码加密「低置信」 | managerPassword/oauth secret 入库前 `CryptoHelper.encryptIfNeeded`，编辑回显加密串，高置信 | §1.6/§2.2 |
