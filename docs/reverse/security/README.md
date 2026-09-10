# security/ — 安全域规格目录（Phase 0 任务 #7）

> 目录化索引：安全域三份既有规格的入口 + 覆盖缺口/UNKNOWN 清单（宪章 §19 negative 场景覆盖度对账）。
> **不重写既有内容**；行为规格本体在 `docs/reverse/` 根下三文件。证据源版本见各规格头注（主要为
> 反编译 7.161.16/7.161.24 + 官方 Deprecated APIs 页交叉）。

## 1. 既有规格索引（三份）

| 文件 | 覆盖面 | 章节地图 | 版本 |
|---|---|---|---|
| `../auth-model.md` | 用户模型与 CRUD 校验链（§1）、密码策略与改密（§2）、**Token 生命周期**（§3：创建/刷新/列表/吊销/过期/使用路径/二次认证边界）、权限模型概览（§4）、PRD 校准建议（§5） | 错误响应三格式并存（text/plain vs JSON errors vs OAuth error）、用户状态机（invited/enabled/disabled/locked） | 7.161.16 反编译 + 官方 |
| `../rbac-model.md` | 实例级授权（§1：admin 判定语义、组 CRUD、scoped manager 边界）、**Projects 域角色层**（§2：角色 gRPC 服务、预定义角色闭集、project admin 判定、角色 REST）、UI vs API 差异（§3） | 「read-only admin / 仓库级 admin」问题定论 | 7.161 反编译 + 官方 |
| `../auth-integration.md` | LDAP（§1：配置模型/搜索模式/组设置/UI 端点/认证流程/测试连接/自动创建）、OAuth-OIDC（§2：一般设置/Provider 模型/UI 端点/登录面）、SAML（§3：13 字段 wire/端点/保存与登录流程）、用户自动创建统一语义（§4）、**优先级与生效语义**（§5：ADR-0035 关键事实）、Admin>Security 三协议页形态（§6） | v2 已纠正 v1 的「OAuth 空桩/SAML 缺失」误判 | 7.161.16 反编译 |

相关联（不在本目录但属安全面）：`../rest-api.md`（认证入口/401+WWW-Authenticate）、`../gap-endpoints.md`（users DELETE 级联守卫）、`../inv-1-core.md` 安全信任分区、frontend/api-map.yaml 的 SSR 开放路由白名单。

## 2. 宪章 §19 覆盖度对账（token / OIDC / SAML negative 场景）

### 2.1 已覆盖（有规格出处）

| 场景族 | 出处 |
|---|---|
| Token 创建参数级拒绝（scope malformed / internal 前缀 / expires_in 负数与非 admin 365d 上限 / refreshable+无期组合 / username 缺失 / 双 token 互斥） | auth-model §3.1 错误码表 |
| Token 刷新缺参（401 invalid_grant 两文案）、吊销 admin-only 403 | auth-model §3.2/§3.4 |
| 改密旧口令错误（400 非 401 + 两档文案）、锁定用户文案、加密口令未启用 409、密码过期未启用 400 | auth-model §2.2 |
| 用户 CRUD 校验链 10 步（403/400/404 全枚举）、非 admin 建 admin 403 | auth-model §1.3/§1.4 |
| LDAP 多设置失败链（末异常重抛 / misconfigured 文案）、认证失败清组开关 | auth-integration §1.5 |
| OAuth state cookie 校验失败/回调 error 参数断言、providerType 非法 | auth-integration §2.4/§2.3 |
| SAML 证书解析失败统一 error、用户名非法 → InvalidNameException | auth-integration §3.3/§4 |
| 配置变更不失效已发会话/token + 认证缓存 300s 窗口 | auth-integration §5.2 |

### 2.2 缺口清单（UNKNOWN——每条：问题 / 为何未知 / 需要什么证据）

1. **OIDC 登录失败 wire 级 negative 矩阵**：token exchange 失败/issuer 不匹配/JWKS 拉取失败时登录页可见形态与响应码——规格只有「断言异常」语义级描述；需 :8082 配置真实 IdP 的失败腿抓包（或反编译 OAuthHandlerImpl 错误分支细读）。
2. **SAML 断言签名无效/时钟偏移/NameID 不匹配**：用户可见错误形态与 HTTP 码未记录；需 SAML IdP 模拟器（如 testshib）打 :8082 或反编译 SamlLogin 错误路径。
3. **登录锁定策略阈值与解锁**：用户状态机有 `locked`，但触发条件（次数/窗口）、锁定时长、admin 解锁端点行为未规格化；需反编译锁定计数器 + 活体暴力触发（对参照实例属写操作——需授权）。
4. **MFA 全域**：enroll/verify/resetMfaStatus/config 五端点已在 frontend/api-map.yaml E1 实证（frontend-server LoginRoutes），行为规格为零；需反编译 Access MFA 控制器 + 活体走查。
5. **过期/被吊销 token 使用时的 401 body 逐字**：auth-model §3.5/§3.6 有语义（含 audience 不符族），wire 逐字形态未录；需 :8082 只读探针（携过期 token GET 任意 API）。
6. **SCIM 供给**（7.161 access MFE 有 scim 页）：用户/组供给协议行为零规格；需反编译 access SCIM 资源 + 官方 SCIM 文档交叉。
7. **Vault 集成**（vault_integration/vaults 页）：零规格；需同上。
8. **HTTP SSO / Crowd 认证腿**：端点存在（/auth/http-sso-login、/ui/crowd PUT），认证行为规格零覆盖；auth-integration v3 候选。
9. **API Key 生命周期**（userApiKey 五端点 E1 实证）：生成/再生成/吊销/过期语义与 token 的差异未规格化；需反编译 ArtifactoryApiKeyController。
10. **匿名/越权矩阵**：非 admin 对各安全端点的 403 面散在三份规格，无集中 negative 矩阵；建议 compatibility-engineer 契约化时汇总（非本目录产出）。
11. **SSR 面认证模型**：frontend-server session/router-token 与 basic auth 的精确边界（E4 已证 `/ui/api/v1/ui/auth/current` basic → anonymous）未体系化；需 frontend-server Authentication 中间件细读（A 源在 reverse-src/artifactory → frontend/frontend-server/src/Middlewares）。

## 3. 阅读序

1. auth-model.md §0 总则 → §3 Token（BinFlow 最先消费）
2. auth-integration.md §0 架构事实 → §5 优先级与生效（ADR-0035 输入）→ 按协议节
3. rbac-model.md §0 结论速览 → §2 Projects 域
4. negative 缺口（本文件 §2.2）→ 转 unknown 队列（任务 #8）

## 4. 一致性自检

- 三份规格间无冲突（auth-integration v2 已显式裁决 v1 两处错误结论，附对照表）。
- 本 README 只新增缺口条目，不改三份本体；§2.2-11 涉及 frontend 域证据已交叉引用 frontend/api-map.yaml，无重复规格。
