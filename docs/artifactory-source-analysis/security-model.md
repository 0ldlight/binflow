# 源码分析 · 安全模型（证据指针文档——正文在既有产物）

> 指针层。行为规格正文导航。

## 1. 规格文件

| 文件 | 覆盖 |
|---|---|
| `docs/reverse/auth-model.md` | 用户/组/权限模型、匿名访问、token 行为（BinFlow 对位 ADR-0009/0026/0027） |
| `docs/reverse/rbac-model.md` | 实例级 + Projects 域两层授权、角色闭集、组 CRUD 与 effective admin |
| `docs/reverse/auth-integration.md` | LDAP 配置模型、OAuth stub 状态、SAML、用户自动创建（§1.4/§2.3/§3.2/§6 配置页 UI 端点表） |
| `docs/reverse/gap-endpoints.md` | 五缺口群：users 回显字段级 / DELETE 级联守卫 / 组成员暴露面 / permission 列表无过滤 / 仓库用量扇出 |
| `docs/reverse/security/README.md` | 安全面子域导航（信任/证书/密码策略等） |

## 2. 对账与契约

- `docs/compatibility/matrix.yaml` D04（安全 35 行：✅19/◐3/❌12/超集1）。
- 已知差异：security 读族 readonly-admin 门姿 UNKNOWN（known-divergence rest/security-read-family-readonly-admin-gate）；readers 默认组建模级 UNKNOWN 两连。
- 权限动词域：r/w/n/d/m 五动词 + wire 双层（正名单 deploy-cache / 别名 write）——ADR-0026/0044 K68 与 parity B-1.6 对账。

## 3. 认证词汇

api-inventory.yaml 认证词汇表（anon/user/read/deploy/annotate/delete/manage/admin/UNKNOWN）逐行标注——UNKNOWN 词条在 unknown.yaml SEC 域（12 条）。

## 4. 缺口声明

SCIM/Vault/Crowd/HTTP SSO 四协议无行为规格（仅运行时截图——runtime-analysis/auth.md）；Access 服务的独立 token/会话 wire 面（/access/*）清点级无逐端点规格。
