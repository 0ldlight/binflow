---
title: LDAP 目录认证配置
sidebar_position: 52
---

# LDAP 目录认证配置

> 适用版本：M6（ADR-0020；`auth.ldap` 段，含 `start_tls`/`skip_tls_verify`/`group_base_dn`，T-186 落地）。
> 本文配置键与行为逐项核对 `internal/config/load.go`、`internal/auth/ldap.go`；ldaps/StartTLS 腿以真 OpenLDAP（osixia 镜像）验收为据（`reports/agents/T-174.md` H30~H35 + T-186 修复后语义）。

启用后，控制台登录表单同时接受**本地用户**与**目录用户**——表单零差异，服务端**先本地后 LDAP** 回退。LDAP 只挂登录端点（`POST /api/v1/session`），没有 Bearer 臂：目录用户的 API 凭据走登录后签发的 Token。

## 前置条件

- M6 构建的 `binflow-server`。
- 可达的 LDAP 目录（OpenLDAP / AD 等），明确搜索基与用户条目布局。

## 配置

`binflow.yaml`：

```yaml
auth:
  ldap:
    enabled: true
    url: ldaps://ldap.example.com:636        # 或 ldap://host:389 + start_tls: true
    base_dn: dc=example,dc=org
    bind_dn: cn=admin,dc=example,dc=org      # 服务账号；空 = 直连用户 DN 绑定（uid=<name>,<base_dn>）
    user_filter: "(uid=%s)"                  # %s = 登录用户名；AD 常用 "(sAMAccountName=%s)"
    user_id_attr: uid                        # 映射到 BinFlow 用户名的属性
    group_filter: "(&(objectClass=groupOfNames)(member=uid=%s,ou=people,dc=example,dc=org))"
    group_base_dn: ou=groups,dc=example,dc=org   # 缺省回落 base_dn
    group_name_attr: cn                      # AD 常用 sAMAccountName
    admin_group: cn=binflow-admins,ou=groups,dc=example,dc=org   # DN；空 = 不映射
    pool_size: 5
    # start_tls: true                        # ldap:// 上升级；与 ldaps:// 互斥（见下）
    # skip_tls_verify: false                 # 仅评估可用
```

bind password 走环境变量，**写进 YAML 会拒启**：

```bash
export BINFLOW_AUTH_LDAP_BIND_PASSWORD=<bind-password>
binflow-server serve -c binflow.yaml
```

| 键 | 必填（enabled 时） | 缺省 | 说明 |
|---|---|---|---|
| `auth.ldap.enabled` | — | `false` | 关闭时登录只走本地口令 |
| `auth.ldap.url` | 是 | — | 必须 `ldap://` 或 `ldaps://` 带 host（裸主机名会被校验拒绝） |
| `auth.ldap.base_dn` | 是 | — | 用户搜索基 DN |
| `auth.ldap.bind_dn` | 否 | 空 | 服务账号 DN；空 = 直接以 `uid=<name>,<base_dn>` 绑定 |
| `auth.ldap.user_filter` | 否 | `(uid=%s)` | 用户搜索模板，首个 `%s` 换成用户名 |
| `auth.ldap.user_id_attr` | 否 | `uid` | 用户名来源属性 |
| `auth.ldap.group_filter` | 否 | 空 | 空 = 不做组搜索（组同步停用） |
| `auth.ldap.group_base_dn` | 否 | `base_dn` | 组搜索基；组独立子树的目录设它免扫用户树 |
| `auth.ldap.group_name_attr` | 否 | `cn` | 组名属性 |
| `auth.ldap.admin_group` | 否 | 空 | 组 **DN**；成员映射为管理员 |
| `auth.ldap.pool_size` | 否 | `5` | 空闲连接池大小（必须为正） |
| `auth.ldap.start_tls` | 否 | `false` | `ldap://` 连接升级 StartTLS |
| `auth.ldap.skip_tls_verify` | 否 | `false` | 跳过证书校验（仅评估，启用打 WARN） |

除 `BINFLOW_AUTH_LDAP_BIND_PASSWORD` 外，`auth.ldap` 其余键**只能写在 YAML**（env 覆盖面不含这些路径）。

## TLS 姿势（三条路线，一表定案）

| 场景 | 配置 | 行为 |
|---|---|---|
| 隐式 TLS | `url: ldaps://host:636` | 拨号即 TLS；`start_tls` 被忽略并打 WARN（连接已加密，双升级无意义） |
| 明文端口升级 | `url: ldap://host:389` + `start_tls: true` | 每条连接进池前先升级，服务 bind 绝不明文出行；升级失败（服务端拒绝/证书不信）**连接拆除、登录失败，绝不静默回落明文** |
| 纯明文 | `url: ldap://host:389`（无 start_tls） | 明文——仅限可信内网 |

证书信任：**没有 CA 证书配置键**——校验用运行环境系统信任库。Linux 容器内把 CA 放进 `/usr/local/share/ca-certificates/` 后 `update-ca-certificates`；注意 macOS 上 Go 程序不读 `SSL_CERT_FILE`（走 keychain），CA 调试腿建议在 Linux 容器里跑。自签评估可用 `skip_tls_verify: true`（启动打 WARN：仅限评估，生产必须 false）。

## 验证

```bash
# 1. 启动日志（start_tls 姿势直接印在这行，不用翻配置）
#    ... "ldap authentication active" url=ldaps://ldap.example.com:636 base_dn=dc=example,dc=org start_tls=false

# 2. 能力清单
curl -s $BASE/binflow/api/v1/auth/methods
# {"password":true,"oidc":false,"ldap":true}

# 3. 目录用户登录（与本地用户同一表单/同一端点）
curl -s -X POST $BASE/binflow/api/v1/session \
  -H 'Content-Type: application/json' -d '{"username":"jdoe","password":"<目录口令>"}'
# {"username":"jdoe","admin":false,"source":"ldap","groups":[...]}

# 4. 管理员组映射
#    badmin（cn=binflow-admins 成员）登录 → "admin":true
```

行为要点（全部真目录验收）：

- **先本地后 LDAP**：本地口令命中即返回；本地失败且配置了 LDAP 才做 bind。本地用户错口令不会被目录补救，目录用户错口令同样 401。
- **首登自动建号**：bind 成功而本地无行时按目录 claim 建行（`provider=ldap`、无本地口令、enabled）。
- **目录停机不影响存量会话**：新登录快速 401（几十毫秒量级，不 hang 不 5xx）；已登录会话与管理面照常——会话验证不碰目录。
- **组同步**：`group_filter` 搜到的组按替换语义写 `user_groups`（与 OIDC 一致：IdP 组需目标侧已物化；管理员组每次登录刷新）。
- **目录用户改密**：控制台改密返回 400——口令归目录管，本地无哈希可对。

## 故障排查（真 OpenLDAP 踩坑实录）

| 症状 | 根因 | 处置 |
|---|---|---|
| 登录恒 401，`ldapsearch` 正常 | osixia/openldap 默认 ACL `to * by self read by * none`——连组都搜不到 | 改 ACL 为 `to * by self read by users read`（对齐 AD「认证用户可读组」姿态） |
| ldaps 一连即 EOF，openssl s_client 正常 | osixia 默认 `olcTLSVerifyClient: demand` 要求客户端证书，Go TLS 客户端直接断（openssl/ldapwhoami 不受影响，易误判） | `olcTLSVerifyClient: never` 后 ldaps 即通 |
| `ldapsearch` 通而 BinFlow 401 | 过滤器/属性拼写不匹配（如 AD 用 `sAMAccountName` 而非 `uid`） | 用与 `user_filter` 完全相同的过滤器手工 `ldapsearch` 验证 |
| 组授权不生效 | 目标侧未建同名组（目录组不自动物化） | 先建组并授权，让用户重新登录 |
| 启动报 `auth.ldap.url: scheme must be ldap or ldaps` | URL 拼错 | 补 scheme 与端口 |
| 目录停机与错口令不可区分 | 两者同为 401（对外统一，不泄露存在性） | 查服务端审计：认证失败事件带 method/reason 字段，目录不可达记 provider_error |

## Helm Chart

```yaml
config:
  ldap:
    enabled: true
    url: ldaps://ldap.example.com:636
    baseDn: dc=example,dc=org
    bindDn: cn=admin,dc=example,dc=org
    userFilter: "(uid=%s)"
    groupFilter: "(&(objectClass=groupOfNames)(member=uid=%s,ou=people,dc=example,dc=org))"
    groupBaseDn: ou=groups,dc=example,dc=org   # 空 = 回落 baseDn（T-194 起渲染）
    groupNameAttr: cn
    adminGroup: cn=binflow-admins,ou=groups,dc=example,dc=org
    startTls: false
    skipTlsVerify: false          # 仅评估可用（T-194 起渲染；true 启动打 WARN）
    existingSecret: binflow-ldap   # Secret 需含 BINFLOW_AUTH_LDAP_BIND_PASSWORD 键
```

```bash
kubectl create secret generic binflow-ldap \
  --from-literal=BINFLOW_AUTH_LDAP_BIND_PASSWORD=<password>
```

> Chart 的 `config.ldap` 与 `auth.ldap` 全键一一对应（T-194 补齐 `groupBaseDn`/`skipTlsVerify`，逐键核对 `internal/config/load.go`）；`skip_tls_verify` 在 LDAP 启用时显式渲染，姿态在 ConfigMap 里可直接审计。

## 下一步

- OIDC 单点登录（Bearer 臂 + SSO 按钮）：[OIDC 配置](oidc-config.md)
- 组与权限模型：[用户组与权限管理](../admin/groups-permissions.md)
