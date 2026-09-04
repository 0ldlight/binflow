# Domain: 安全域——users / groups / token / permissions / keypair / 认证配置。
# Source: docs/user/api-reference.md 「SE: 安全域」「M9 增补速览」「M11 增补速览」；
# 路由核对 internal/httpapi/router.go（security/ 与 v1/admin/security/ 与 v2 keyPairs 面）。

from helpers import (op, tag, q, pp, r, S, arr, body, obj, ERR_401, ERR_403)

tag("users", "用户管理（/api/security/users）——错误族为纯文本响应体")
tag("groups", "组管理（/api/security/groups）")
tag("tokens", "Access Token（/api/security/token）——OAuth 2.0 错误规范")
tag("permissions", "Permission Target（/api/v1/permissions）")
tag("keypairs", "GPG keypair 族（/api/security/keypair* + BinFlow 原生生成/仓关联面）")
tag("auth-config", "认证配置面（/api/v1/admin/security/{ldap,oauth,saml}）")


def build():
    # ---- users ----
    op("/api/security/users", "get", "userList", "users", "用户列表",
       "官方拼写：`GET /binflow/api/security/users`（admin / readonly_admin）。"
       "条目 `{name,uri,realm,source,email,adminRole,enabled,groups}`——`enabled`/`groups` 恒渲染"
       "（空组 `[]` 非 null），一次请求含全部列表所需字段。",
       responses={"200": r("用户列表", schema=arr(S("UserSummary"))),
                  "401": ERR_401, "403": ERR_403})

    op("/api/security/users", "post", "userCreatePost", "users", "创建用户（集合路由，create-only）",
       "官方拼写：`POST /binflow/api/security/users`（**BinFlow 自有集合路由**——Artifactory 官方无此面；"
       "create-or-replace 走 `PUT /api/security/users/{name}`）。create-only：body 必含 `name`"
       "（缺失或保留名 `_system_` → 400 纯文本 `Unable to create user.`）；同名已存在 → **409 纯文本** "
       "`The user already exists: <name>`（无路径可键替换——部分更新在 `POST /api/security/users/{name}`）。"
       "混合大小写用户名 400 纯文本拒绝（不静默改名）；缺 email/password 的 400 文案与 PUT 同族"
       "（`Please provide a valid user email.` / `Please provide a valid user password.`）。成功 **201 无 body**。",
       req_body=body("用户字段（name 在此必填）", schema=S("UserInput"),
                     example={"name": "jane", "email": "jane@example.com", "password": "s3cr3t!"}),
       responses={"201": r("已创建（无 body）"),
                  "400": r("参数错误（纯文本，文案见描述）", schema={"type": "string"}, ctype="text/plain"),
                  "401": ERR_401, "403": ERR_403,
                  "409": r("同名用户已存在", schema={"type": "string"},
                           example="The user already exists: jane", ctype="text/plain")})

    op("/api/security/users/{name}", "get", "userGet", "users", "用户详情",
       "官方拼写：`GET /binflow/api/security/users/{name}`。无口令字段；`adminRole` 与 `enabled` 恒回显。",
       params=[pp("name", "用户名")],
       responses={"200": r("用户详情", schema=S("UserSummary")),
                  "404": r("404 `User not found`（文本体）", schema={"type": "string"},
                           example="User not found", ctype="text/plain")})

    op("/api/security/users/{name}", "put", "userPut", "users", "创建或替换用户",
       "官方拼写：`PUT /binflow/api/security/users/{name}`（create-or-replace，两态 201）。"
       "body 可含 `adminRole`，仅 admin 可写。缺 email：400 纯文本 `Please provide a valid user email.`；"
       "引用不存在的组：400 纯文本 `Unable to find group by name 'devs'. Please make sure the group exists before adding users to it.`；"
       "`admin` 与 `adminRole` 布尔矛盾：400 纯文本 `conflicting 'admin' and 'adminRole' fields: admin=false is incompatible with adminRole=\"admin\" (admin=true is equivalent to adminRole=admin)`。",
       params=[pp("name", "用户名")],
       req_body=body("用户字段", schema=S("UserInput")),
       responses={"201": r("已创建/已替换"),
                  "400": r("参数错误（纯文本，文案逐字见描述）", schema={"type": "string"}, ctype="text/plain"),
                  "401": ERR_401, "403": ERR_403})

    op("/api/security/users/{name}", "post", "userPost", "users", "部分更新用户",
       "官方拼写：`POST /binflow/api/security/users/{name}`。"
       "可更新 email/password/admin/groups/adminRole/enabled——`enabled` 为指针语义，显式 `false` 禁用登录"
       "（禁用后该用户登录/既有会话 401），`{\"enabled\":true}` 复启；显式传值才生效，缺省不动。",
       params=[pp("name", "用户名")],
       req_body=body("部分更新字段", schema=S("UserInput")),
       responses={"200": r("已更新"),
                  "400": r("参数错误（纯文本）", schema={"type": "string"}, ctype="text/plain"),
                  "401": ERR_401, "403": ERR_403})

    op("/api/security/users/{name}", "delete", "userDelete", "users", "删除用户",
       "官方拼写：`DELETE /binflow/api/security/users/{name}`（admin only）。"
       "成功 **200 纯文本** `The user: '<name>' has been removed successfully.`，四道护栏全 400 纯文本，检查序固定：\n"
       "1. 目标不存在 → **404** `User not found`（文本体，与 GET 单用户同形）；\n"
       "2. 内置 admin → 400 `Cannot delete the built-in admin user.`；\n"
       "3. 最后一个 admin → 400 `Cannot delete user '<name>'. There must be at least one user configured with admin privileges.`；\n"
       "4. 自删 → 400 `Cannot delete the current authenticated user.`。\n"
       "级联（同事务）：剥全部 permission target 授权行 → 删用户行 → FK 级联清组员关系、吊销全部 token 与 web session"
       "（已持有的 Bearer 即刻 401）；审计历史保留。**重复删除 = 确定性 404（有意非幂等）**——调用方应把第二次 404 "
       "理解为「对象已被删」，不要重试。审计落 `user.delete`；护栏拒绝不落审计。",
       params=[pp("name", "用户名")],
       responses={"200": r("已删除", schema={"type": "string"},
                           example="The user: 'jane' has been removed successfully.", ctype="text/plain"),
                  "400": r("护栏拒绝（纯文本）", schema={"type": "string"}, ctype="text/plain"),
                  "404": r("目标不存在", schema={"type": "string"},
                           example="User not found", ctype="text/plain")})

    op("/api/security/password", "put", "changePasswordOwn", "users", "当前用户改密",
       "官方拼写：`PUT /binflow/api/security/password`。",
       req_body=body("旧/新口令", schema=obj({"old": {"type": "string"}, "new": {"type": "string"},
                                              "password": {"type": "string"}, "username": {"type": "string"}})),
       responses={"200": r("已修改"), "401": ERR_401})

    op("/api/security/users/authorization/changePassword", "post", "changePasswordAlias",
       "users", "别名改密端点",
       "官方拼写：`POST /binflow/api/security/users/authorization/changePassword`。",
       responses={"200": r("已修改"), "401": ERR_401})

    # ---- groups ----
    op("/api/security/groups", "get", "groupList", "groups", "组列表",
       "官方拼写：`GET /binflow/api/security/groups`（admin / readonly_admin）。列表端点不加宽（无成员汇总）。",
       responses={"200": r("组列表", schema=arr(S("GroupDetail"))),
                  "401": ERR_401, "403": ERR_403})

    op("/api/security/groups/{name}", "get", "groupGet", "groups", "组详情",
       "官方拼写：`GET /binflow/api/security/groups/{name}?includeUsers=true`。"
       "带参（字面 `true`，大小写敏感）附 `userNames: []string`（空组 `[]` 恒非 null）；"
       "其余拼法（`false`/`junk`/`TRUE`）回无参三字段形态 200，不发明 400。未知组带参 → 404 `Group not found`（与无参同文案）。",
       params=[pp("name", "组名"),
               q("includeUsers", "字面 true 才开", schema={"type": "string", "enum": ["true"]})],
       responses={"200": r("组详情", schema=S("GroupDetail")),
                  "404": r("组不存在（纯文本）", schema={"type": "string"},
                           example="Unable to find group by name 'nonexistent-group'.", ctype="text/plain")})

    op("/api/security/groups/{name}", "put", "groupPut", "groups", "创建或更新组",
       "官方拼写：`PUT /binflow/api/security/groups/{name}`（创建 201 / 更新 200）。"
       "名字须匹配 `[a-z][a-z0-9._-]*`——大写开头 400 纯文本 "
       "`Unable to create group: name must match [a-z][a-z0-9._-]* but it starts with uppercase 'X'.`",
       params=[pp("name", "组名")],
       req_body=body("组字段", schema=obj({"description": {"type": "string"}})),
       responses={"200": r("已更新"), "201": r("已创建"),
                  "400": r("参数错误（纯文本）", schema={"type": "string"}, ctype="text/plain")})

    op("/api/security/groups/{name}", "post", "groupPost", "groups", "改组描述",
       "官方拼写：`POST /binflow/api/security/groups/{name}`。",
       params=[pp("name", "组名")],
       req_body=body("描述", schema=obj({"description": {"type": "string"}})),
       responses={"200": r("已更新")})

    op("/api/security/groups/{name}", "delete", "groupDelete", "groups", "删组",
       "官方拼写：`DELETE /binflow/api/security/groups/{name}`。被 permission target 引用 → 409 纯文本 "
       "`Cannot delete group 'devs': it is referenced by permission target(s): devs-rw, jane-rd. Remove the group from those targets first.`",
       params=[pp("name", "组名")],
       responses={"200": r("已删除"),
                  "409": r("被权限引用（纯文本）", schema={"type": "string"}, ctype="text/plain")})

    # ---- token ----
    op("/api/security/token", "post", "tokenCreate", "tokens", "签发 Access Token",
       "官方拼写：`POST /binflow/api/security/token`。admin 为任意用户签发；非 admin 限本人。"
       "body 可选 `step_up_password` / `step_up_grant`（仅 `auth.token_step_up` 开启时的非 admin session 臂要求，"
       "见 step-up 指南）。错误按 OAuth 2.0 规范：400 `{\"error\":\"invalid_request\",\"error_description\":\"missing grant_type parameter\"}`；"
       "401 `{\"error\":\"invalid_client\",\"error_description\":\"authentication failed\"}`；"
       "step-up 两形态 401 `{\"error\":\"step_up_required\",\"error_description\":\"step-up authentication required to mint a token\"}` / "
       "`{\"error\":\"step_up_invalid\",\"error_description\":\"step-up credential rejected, expired, or already used\"}`。"
       "docker token 流也走同表——管理面吊销对 docker token 即时生效。",
       req_body=body("application/x-www-form-urlencoded：grant_type=client_credentials&username=ci-bot（可选 expires_in/scope/step_up_password/step_up_grant）",
                     schema=obj({"grant_type": {"type": "string"}, "username": {"type": "string"},
                                 "expires_in": {"type": "integer"}, "scope": {"type": "string"},
                                 "step_up_password": {"type": "string"}, "step_up_grant": {"type": "string"}},
                                desc="表单字段"),
                     example="grant_type=client_credentials&username=ci-bot",
                     ctype="application/x-www-form-urlencoded", required=True),
       responses={"200": r("签发成功", schema=S("TokenResponse"),
                           example={"access_token": "<64hex>", "token_id": "<id>",
                                    "expires_in": 2592000, "scope": "api:*"}),
                  "400": r("invalid_request", schema=S("OAuthError"),
                           example={"error": "invalid_request", "error_description": "missing grant_type parameter"}),
                  "401": r("invalid_client / step_up_*", schema=S("OAuthError"))})

    op("/api/security/token/revoke", "post", "tokenRevoke", "tokens", "吊销 Token",
       "官方拼写：`POST /binflow/api/security/token/revoke`（admin only）。未知 token：403 "
       "`{\"error\":\"access_denied\",\"error_description\":\"token not found\"}`。",
       req_body=body("token_id=<上面的 token_id>", schema=obj({"token_id": {"type": "string"}}),
                     example="token_id=42", ctype="application/x-www-form-urlencoded", required=True),
       responses={"200": r("已吊销"),
                  "403": r("token not found", schema=S("OAuthError"),
                           example={"error": "access_denied", "error_description": "token not found"})})

    # ---- permissions ----
    op("/api/v1/permissions", "post", "permissionCreate", "permissions",
       "创建 Permission Target（create-or-replace）",
       "官方拼写：`POST /binflow/api/v1/permissions`。动作集**五值闭集** `read / deploy-cache / annotate / delete / manage`"
       "——`write` 仍被接受为 `deploy-cache` 的兼容别名（**不附带 annotate**），GET 回显恒正名单形；"
       "`annotate` 单独控制属性写门；manage 持有者可编辑覆盖集内的 target。写门 = CapSecurityWrite ∨ 覆盖臂"
       "（body 相关，handler 内裁决）。",
       req_body=body("Permission Target", schema=S("PermissionTargetInput"),
                     example={"name": "devs-rw", "repos": ["dev-local"],
                              "principals": {"groups": {"devs": ["read", "deploy-cache", "annotate", "delete", "manage"]}}}),
       responses={"200": r("已创建/已替换（回显正名单形）", schema=S("PermissionTarget")),
                  "400": ERR_401, "401": ERR_401})

    op("/api/v1/permissions", "get", "permissionList", "permissions", "列出 Permission Targets",
       "官方拼写：`GET /binflow/api/v1/permissions[?filter=manage]`（admin / readonly_admin）。"
       "principals 回显动作**正名单单形**：`read, deploy-cache, annotate, delete, manage`——`write` 别名收词不回显。"
       "`?filter=manage`：manage 持有者可达的覆盖集内 target 子集（admin/readonly_admin 带参与无参响应逐字节一致；"
       "部分覆盖的 target 隐藏；覆盖集为空 403）。`?filter=`（空值）= 无 ask；未知值 → 400 errors[] 信封 "
       "（`filter must be \"manage\" (unknown filter value: \"bogus\")`）。",
       params=[q("filter", "manage", schema={"type": "string", "enum": ["manage"]})],
       responses={"200": r("Permission Target 列表", schema=arr(S("PermissionTarget"))),
                  "400": r("未知 filter 值", schema=S("ErrorsEnvelope")),
                  "403": ERR_403})

    op("/api/v1/permissions/{name}", "delete", "permissionDelete", "permissions",
       "删除 Permission Target",
       "官方拼写：`DELETE /binflow/api/v1/permissions/{name}`（**204** 无 body）。"
       "被删 target 的 repo 集取自存量行，manage 覆盖越界 → 403。",
       params=[pp("name", "target 名")],
       responses={"204": r("已删除（无 body）"), "403": ERR_403})

    # ---- keypair 族 ----
    op("/api/security/keypair", "post", "keypairImport", "keypairs", "导入钥对",
       "官方拼写：`POST /binflow/api/security/keypair`（create-or-replace；201 回 KeyPairSummary）。"
       "`X-GPG-PASSPHRASE` 头不收（口令随钥行密封）。",
       req_body=body("KeyPairInput", schema=S("KeyPairInput")),
       responses={"201": r("KeyPairSummary", schema=S("KeyPairSummary"))})

    op("/api/security/keypair", "put", "keypairUpdate", "keypairs", "更新钥对",
       "官方拼写：`PUT /binflow/api/security/keypair`（不存在 → 404；轮换面）。",
       req_body=body("KeyPairInput", schema=S("KeyPairInput")),
       responses={"200": r("已更新"), "404": r("不存在")})

    op("/api/security/keypair", "get", "keypairList", "keypairs", "钥对列表",
       "官方拼写：`GET /binflow/api/security/keypair`（bare array）。",
       responses={"200": r("KeyPairSummary 列表", schema=arr(S("KeyPairSummary")))})

    op("/api/security/keypair/{pairName}", "get", "keypairGet", "keypairs", "单查钥对",
       "官方拼写：`GET /binflow/api/security/keypair/{pairName}`（未知名 404）。",
       params=[pp("pairName", "钥对名")],
       responses={"200": r("KeyPairSummary", schema=S("KeyPairSummary")), "404": r("未知名")})

    op("/api/security/keypair/{pairName}", "delete", "keypairDelete", "keypairs", "删除钥对",
       "官方拼写：`DELETE /binflow/api/security/keypair/{pairName}`。200 纯文本 `OK`；"
       "被仓引用 → 400 点名引用仓清单。",
       params=[pp("pairName", "钥对名")],
       responses={"200": r("OK（纯文本）", schema={"type": "string"}, example="OK", ctype="text/plain"),
                  "400": r("被仓引用——点名引用仓清单（纯文本）", schema={"type": "string"},
                           ctype="text/plain")})

    op("/api/security/keypair/verify", "post", "keypairVerify", "keypairs", "校验钥对",
       "官方拼写：`POST /binflow/api/security/keypair/verify`。200 纯文本 `Key was verified.`；"
       "body 全量材料或（BinFlow 扩展）仅 `{\"pairName\":…}` 校验存量密封钥。",
       req_body=body("全量材料或仅 pairName", schema=obj({"pairName": {"type": "string"}})),
       responses={"200": r("Key was verified.（纯文本）", schema={"type": "string"},
                           example="Key was verified.", ctype="text/plain")})

    op("/api/security/keypair/public/repositories/{repoKey}", "get", "keypairPublicByRepo",
       "keypairs", "该仓关联 keypair 的 armored 公钥",
       "官方拼写：`GET /binflow/api/security/keypair/public/repositories/{repoKey}`（text/plain）。",
       params=[pp("repoKey", "仓库 key")],
       responses={"200": r("armored 公钥（text/plain）", schema={"type": "string"}, ctype="text/plain")})

    op("/api/v1/admin/security/keypair/generate", "post", "keypairGenerate", "keypairs",
       "服务端生成钥对（BinFlow 原生面）",
       "官方拼写：`POST /binflow/api/v1/admin/security/keypair/generate`（201 回 summary；重名 409）——"
       "Artifactory 官方 REST 无 keygen，此端点为自有管理面。",
       req_body=body("生成入参", schema=S("KeyPairGenerateInput"), required=True),
       responses={"201": r("KeyPairSummary", schema=S("KeyPairSummary")), "409": r("重名")})

    op("/api/v2/repositories/{repoKey}/keyPairs", "post", "keypairAssociate", "keypairs",
       "仓关联钥对",
       "官方拼写：`POST /binflow/api/v2/repositories/{repoKey}/keyPairs`（text/plain body = 钥名）。"
       "仅 local `debian`/`rpm` 仓接受 `keyPairName`，其余包型按名 400。",
       params=[pp("repoKey", "仓库 key")],
       req_body=body("钥名（text/plain）", schema={"type": "string"}, ctype="text/plain", required=True),
       responses={"200": r("已关联")})

    op("/api/v2/repositories/{repoKey}/keyPairs/{keyName}", "delete", "keypairDisassociate",
       "keypairs", "解除仓钥对关联",
       "官方拼写：`DELETE /binflow/api/v2/repositories/{repoKey}/keyPairs/{keyName}`。",
       params=[pp("repoKey", "仓库 key"), pp("keyName", "钥名")],
       responses={"200": r("已解除")})

    # ---- 认证配置面 ----
    for seg, label in (("ldap", "LDAP"), ("oauth", "OIDC"), ("saml/config", "SAML")):
        base = "/api/v1/admin/security/" + seg
        oid = seg.replace("/", "").replace("ldap", "Ldap").replace("oauth", "Oidc").replace("samlconfig", "Saml")
        op(base, "get", "authConfig%sGet" % oid, "auth-config", "%s 段读取" % label,
           "官方拼写：`GET /binflow/api/v1/admin/security/%s`（CapSecurityRead）。%s 段%s"
           % (seg, label, "（未设置回默认形）" if seg == "ldap" else "（未设置 GET 回 `{}`）" if "saml" in seg else "（snake_case wire）"),
           responses={"200": r("%s 配置段（secret 哨兵 20 星）" % label,
                               schema=obj({}, desc="字段表见认证配置指南"))})
        op(base, "put", "authConfig%sPut" % oid, "auth-config", "%s 段整段替换" % label,
           "官方拼写：`PUT /binflow/api/v1/admin/security/%s`（CapSecurityWrite；**保存即生效**，无需重启）。"
           "secret 哨兵语义（write-only）：GET 对已设置 secret 恒回 20 星 `********************`；"
           "PUT 键缺席 = 保持、`\"\"` = 清除、新明文 = 替换；**回传哨兵 → 400** "
           "`refusing the masked placeholder — leave the field empty to keep the stored secret, or re-enter the value`。"
           "secret 落库前 enc:v1 密封（实例主密钥 `BINFLOW_REMOTE_CREDENTIALS_KEY`）；无主密钥时 secret 写拒绝。" % seg,
           req_body=body("整段配置", schema=obj({}, desc="字段表见认证配置指南")),
           responses={"200": r("已保存（回显段）")})
        op(base + "/test", "post", "authConfig%sTest" % oid, "auth-config", "%s 测试连接" % label,
           "官方拼写：`POST /binflow/api/v1/admin/security/%s/test`（CapSecurityWrite——探测开外连）。"
           "test 响应 `{\"ok\":bool,\"phase\":\"…\",\"category\":\"…\",\"message\":\"…\"}`，`ok:false` 时 HTTP 400"
           "（如 `{\"ok\":false,\"phase\":\"dial\",\"category\":\"unreachable\",\"message\":\"could not connect to the target (dial failed or timed out)\"}`）。"
           "审计：`auth.config.update`（detail 只含变更键名，值不落）/ `auth.config.test`。" % seg,
           responses={"200": r("TestReport", schema=S("TestReport")),
                      "400": r("ok:false 同形 400", schema=S("TestReport"))})

    op("/api/v1/admin/security/saml/config/key/public", "get", "samlKeyPublic", "auth-config",
       "当前 SP 加密证书 PEM",
       "官方拼写：`GET /binflow/api/v1/admin/security/saml/config/key/public`（CapSecurityRead；text/plain）。"
       "未生成 404 `saml sp encryption certificate has not been generated`。",
       responses={"200": r("证书 PEM（text/plain）", schema={"type": "string"}, ctype="text/plain"),
                  "404": r("saml sp encryption certificate has not been generated",
                           schema={"type": "string"}, ctype="text/plain")})

    op("/api/v1/admin/security/saml/config/key/public/regenerate", "put", "samlKeyRegenerate",
       "auth-config", "轮换 SP 钥对",
       "官方拼写：`PUT /binflow/api/v1/admin/security/saml/config/key/public/regenerate`（CapSecurityWrite）。"
       "force 一对一替换、旧证书即刻失效；响应体 = 新证书 PEM。审计 `auth.config.samlkey.regenerate`（零密材落日志）。",
       responses={"200": r("新证书 PEM（text/plain）", schema={"type": "string"}, ctype="text/plain")})

    op("/api/v1/admin/security/saml/key", "post", "samlKeyGenerate", "auth-config",
       "生成/替换 SP 钥对（BinFlow 原生面）",
       "官方拼写：`POST /binflow/api/v1/admin/security/saml/key`（CapSecurityWrite）。"
       "与 regenerate 同机、审计动作分立：`auth.config.samlkey.{generate,regenerate}`。",
       responses={"200": r("新证书 PEM（text/plain）", schema={"type": "string"}, ctype="text/plain")})
