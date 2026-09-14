# 运行时分析 · 认证与身份面（证据指针文档——正文在既有产物）

> 截图基线 ：8082（7.161.20）。路径前缀 `docs/reverse/frontend/parity-capture/`（下称 `pc/`）。

## 1. 截图与 DOM

| 资产 | 内容 |
|---|---|
| `pc/screenshots/screens/login.png` + `pc/dom-snapshots/login-form.html` | 登录页形态 |
| `pc/screenshots/states/login-error.png` / `login-error-after.png` / `login-form-unexpected.png` | 登录错误态（错一次内联错误） |
| `pc/screenshots/screens/oauth.png` + `oauth-new-provider.png` | OAuth SSO 配置列表 + 新建 provider |
| `saml.png` / `ldap.png` / `http-sso.png` / `crowd.png` / `scim.png`（同目录） | SAML/LDAP/HTTP SSO/Crowd/SCIM 配置页 |
| `vault-integration.png` / `vaults.png` / `reverse-proxy.png` / `certificates.png` | Vault 集成/实例/反向代理/证书 |
| `security-general-access.png` / `security-general-af.png` | General Security 两页签（Access/Artifactory 侧） |
| `access-tokens.png` + `pc/screenshots/dialogs/token-generate-form.png` | Access Tokens 页 + Generate Token 表单（未提交） |
| `global-roles.png` | 全局角色（Pro 面） |

## 2. 行为规格（正文）

- `docs/reverse/auth-model.md`——用户/组/权限模型、token 行为。
- `docs/reverse/auth-integration.md` §1.4/§2.3/§3.2/§6——LDAP/OAuth/SAML 三协议配置页 UI 端点表与表单形态（7.161 反编译；SCIM/Vault/HTTP SSO/Crowd 无规格——UI 截图是唯一证据）。
- `docs/reverse/rbac-model.md`——实例级/Projects 两层授权、角色闭集、effective admin。
- `docs/reverse/frontend/screens.yaml`——login 屏四态判据（error 停留本页；admin 首登 → onboarding）。
- 登录会话/step-up：`docs/reverse/gap-endpoints.md`（users 回显字段级）。

## 3. 已知运行时事实（引用既有结论）

- 管理侧栏 Authentication 组六子项（LDAP/SAML SSO/OAuth SSO/HTTP SSO/Crowd·JIRA/SCIM）——t459-probe s4-sidebar-groups 实据。
- Profile 页签仅 Password（无主题控件）——walkthrough-probe2.json。

## 4. 缺口声明

SSO/MFA 登录重定向表单未采集（实例 internal auth）；Vault/Crowd/SCIM 仅截图、无行为规格（UNKNOWN 池见 `docs/compatibility/unknown.yaml` SEC/CFG 域）。
