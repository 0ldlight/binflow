# Domain: 安全域——users / groups / token / permissions / keypair / 认证配置。
# Source: docs/user/api-reference.md 「SE: 安全域」「M9 增补速览」「M11 增补速览」；
# 路由核对 internal/httpapi/router.go（security/ 与 v1/admin/security/ 与 v2 keyPairs 面）。

from helpers import (op, tag, q, pp, r, S, arr, body, obj, ERR_401, ERR_403)

tag("users", "User management (/api/security/users) — this family's errors are plain-text response bodies")
tag("groups", "Group management (/api/security/groups)")
tag("tokens", "Access tokens (/api/security/token) — OAuth 2.0 error conventions")
tag("permissions", "Permission targets (/api/v1/permissions)")
tag("keypairs", "GPG key pairs (/api/security/keypair* plus BinFlow-native generation and repository association)")
tag("auth-config", "Authentication configuration (/api/v1/admin/security/{ldap,oauth,saml})")


def build():
    # ---- users ----
    op("/api/security/users", "get", "userList", "users", "List users",
       "Canonical path: `GET /binflow/api/security/users` (admin / readonly_admin). "
       "Entries `{name,uri,realm,source,email,adminRole,enabled,groups}` — `enabled`/`groups` are always rendered "
       "(empty groups `[]` not null); a single request carries every field the listing needs.",
       responses={"200": r("User list", schema=arr(S("UserSummary"))),
                  "401": ERR_401, "403": ERR_403})

    op("/api/security/users", "post", "userCreatePost", "users", "Create a user (collection route, create-only)",
       "Canonical path: `POST /binflow/api/security/users` (**a BinFlow-native collection route** — official "
       "Artifactory has no such face; create-or-replace goes through `PUT /api/security/users/{name}`). Create-only: "
       "the body must include `name` (missing, or the reserved name `_system_` → 400 plain text `Unable to create "
       "user.`); an existing name → **409 plain text** `The user already exists: <name>` (no path to key-replace — "
       "partial updates live at `POST /api/security/users/{name}`). Mixed-case usernames are rejected 400 plain text "
       "(no silent renaming); missing email/password reuse the PUT-family 400 messages "
       "(`Please provide a valid user email.` / `Please provide a valid user password.`). Success is **201 with no "
       "body**.",
       req_body=body("User fields (name is required here)", schema=S("UserInput"),
                     example={"name": "jane", "email": "jane@example.com", "password": "s3cr3t!"}),
       responses={"201": r("Created (no body)"),
                  "400": r("Bad request (plain text, messages in the description)", schema={"type": "string"}, ctype="text/plain"),
                  "401": ERR_401, "403": ERR_403,
                  "409": r("User with the same name already exists", schema={"type": "string"},
                           example="The user already exists: jane", ctype="text/plain")})

    op("/api/security/users/{name}", "get", "userGet", "users", "User details",
       "Canonical path: `GET /binflow/api/security/users/{name}`. No password fields; `adminRole` and `enabled` are "
       "always echoed.",
       params=[pp("name", "Username")],
       responses={"200": r("User details", schema=S("UserSummary")),
                  "404": r("404 `User not found` (text body)", schema={"type": "string"},
                           example="User not found", ctype="text/plain")})

    op("/api/security/users/{name}", "put", "userPut", "users", "Create or replace a user",
       "Canonical path: `PUT /binflow/api/security/users/{name}` (create-or-replace; both states return 201). "
       "The body may include `adminRole`, writable by admins only. Missing email: 400 plain text "
       "`Please provide a valid user email.`; referencing a nonexistent group: 400 plain text "
       "`Unable to find group by name 'devs'. Please make sure the group exists before adding users to it.`; "
       "a boolean contradiction between `admin` and `adminRole`: 400 plain text "
       "`conflicting 'admin' and 'adminRole' fields: admin=false is incompatible with adminRole=\"admin\" "
       "(admin=true is equivalent to adminRole=admin)`.",
       params=[pp("name", "Username")],
       req_body=body("User fields", schema=S("UserInput")),
       responses={"201": r("Created/replaced"),
                  "400": r("Bad request (plain text, messages verbatim in the description)", schema={"type": "string"}, ctype="text/plain"),
                  "401": ERR_401, "403": ERR_403})

    op("/api/security/users/{name}", "post", "userPost", "users", "Partially update a user",
       "Canonical path: `POST /binflow/api/security/users/{name}`. "
       "Can update email/password/admin/groups/adminRole/enabled — `enabled` uses pointer semantics: an explicit "
       "`false` disables login (afterwards that user's logins and existing sessions get 401), `{\"enabled\":true}` "
       "re-enables; only explicit values take effect, absent leaves things unchanged.",
       params=[pp("name", "Username")],
       req_body=body("Partial update fields", schema=S("UserInput")),
       responses={"200": r("Updated"),
                  "400": r("Bad request (plain text)", schema={"type": "string"}, ctype="text/plain"),
                  "401": ERR_401, "403": ERR_403})

    op("/api/security/users/{name}", "delete", "userDelete", "users", "Delete a user",
       "Canonical path: `DELETE /binflow/api/security/users/{name}` (admin only). "
       "Success is **200 plain text** `The user: '<name>' has been removed successfully.`; the four guardrails are all "
       "400 plain text with a fixed check order:\n"
       "1. Target missing → **404** `User not found` (text body, same shape as the single-user GET);\n"
       "2. Built-in admin → 400 `Cannot delete the built-in admin user.`;\n"
       "3. Last admin → 400 `Cannot delete user '<name>'. There must be at least one user configured with admin "
       "privileges.`;\n"
       "4. Self-deletion → 400 `Cannot delete the current authenticated user.`.\n"
       "Cascades (same transaction): strips all permission-target grant rows → deletes the user row → FK-cascades group "
       "memberships, revokes every token and web session (held Bearers turn 401 immediately); audit history is kept. "
       "**Repeated deletion = deterministic 404 (intentionally not idempotent)** — callers should read the second 404 "
       "as \"already deleted\" and not retry. Audited as `user.delete`; guardrail rejections are not audited.",
       params=[pp("name", "Username")],
       responses={"200": r("Deleted", schema={"type": "string"},
                           example="The user: 'jane' has been removed successfully.", ctype="text/plain"),
                  "400": r("Guardrail rejection (plain text)", schema={"type": "string"}, ctype="text/plain"),
                  "404": r("Target not found", schema={"type": "string"},
                           example="User not found", ctype="text/plain")})

    op("/api/security/password", "put", "changePasswordOwn", "users", "Change the current user's password",
       "Canonical path: `PUT /binflow/api/security/password`.",
       req_body=body("Old/new password", schema=obj({"old": {"type": "string"}, "new": {"type": "string"},
                                              "password": {"type": "string"}, "username": {"type": "string"}})),
       responses={"200": r("Changed"), "401": ERR_401})

    op("/api/security/users/authorization/changePassword", "post", "changePasswordAlias",
       "users", "Alias password-change endpoint",
       "Canonical path: `POST /binflow/api/security/users/authorization/changePassword`.",
       responses={"200": r("Changed"), "401": ERR_401})

    # ---- groups ----
    op("/api/security/groups", "get", "groupList", "groups", "List groups",
       "Canonical path: `GET /binflow/api/security/groups` (admin / readonly_admin). The list endpoint does not widen "
       "(no member roll-up).",
       responses={"200": r("Group list", schema=arr(S("GroupDetail"))),
                  "401": ERR_401, "403": ERR_403})

    op("/api/security/groups/{name}", "get", "groupGet", "groups", "Group details",
       "Canonical path: `GET /binflow/api/security/groups/{name}?includeUsers=true`. "
       "The parameter (literal `true`, case-sensitive) attaches `userNames: []string` (an empty group is `[]` never "
       "null); other spellings (`false`/`junk`/`TRUE`) return the three-field no-parameter shape 200 — no invented "
       "400. An unknown group with the parameter → 404 `Group not found` (same message as without).",
       params=[pp("name", "Group name"),
               q("includeUsers", "Literal true to enable", schema={"type": "string", "enum": ["true"]})],
       responses={"200": r("Group details", schema=S("GroupDetail")),
                  "404": r("Group not found (plain text)", schema={"type": "string"},
                           example="Unable to find group by name 'nonexistent-group'.", ctype="text/plain")})

    op("/api/security/groups/{name}", "put", "groupPut", "groups", "Create or update a group",
       "Canonical path: `PUT /binflow/api/security/groups/{name}` (create 201 / update 200). "
       "The name must match `[a-z][a-z0-9._-]*` — an uppercase start gets 400 plain text "
       "`Unable to create group: name must match [a-z][a-z0-9._-]* but it starts with uppercase 'X'.`",
       params=[pp("name", "Group name")],
       req_body=body("Group fields", schema=obj({"description": {"type": "string"}})),
       responses={"200": r("Updated"), "201": r("Created"),
                  "400": r("Bad request (plain text)", schema={"type": "string"}, ctype="text/plain")})

    op("/api/security/groups/{name}", "post", "groupPost", "groups", "Update group description",
       "Canonical path: `POST /binflow/api/security/groups/{name}`.",
       params=[pp("name", "Group name")],
       req_body=body("Description", schema=obj({"description": {"type": "string"}})),
       responses={"200": r("Updated")})

    op("/api/security/groups/{name}", "delete", "groupDelete", "groups", "Delete a group",
       "Canonical path: `DELETE /binflow/api/security/groups/{name}`. Referenced by a permission target → 409 plain "
       "text `Cannot delete group 'devs': it is referenced by permission target(s): devs-rw, jane-rd. Remove the group "
       "from those targets first.`",
       params=[pp("name", "Group name")],
       responses={"200": r("Deleted"),
                  "409": r("Referenced by permissions (plain text)", schema={"type": "string"}, ctype="text/plain")})

    # ---- token ----
    op("/api/security/token", "post", "tokenCreate", "tokens", "Mint an access token",
       "Canonical path: `POST /binflow/api/security/token`. Admins mint for any user; non-admins for themselves only. "
       "The body accepts optional `step_up_password` / `step_up_grant` (required on the non-admin session arm only "
       "when `auth.token_step_up` is enabled — see the step-up guide). Errors follow the OAuth 2.0 conventions: 400 "
       "`{\"error\":\"invalid_request\",\"error_description\":\"missing grant_type parameter\"}`; 401 "
       "`{\"error\":\"invalid_client\",\"error_description\":\"authentication failed\"}`; step-up's two forms 401 "
       "`{\"error\":\"step_up_required\",\"error_description\":\"step-up authentication required to mint a token\"}` / "
       "`{\"error\":\"step_up_invalid\",\"error_description\":\"step-up credential rejected, expired, or already "
       "used\"}`. The docker token flow uses the same table — revocations on the management API take effect on docker "
       "tokens immediately.",
       req_body=body("application/x-www-form-urlencoded: grant_type=client_credentials&username=ci-bot "
                     "(optional expires_in/scope/step_up_password/step_up_grant)",
                     schema=obj({"grant_type": {"type": "string"}, "username": {"type": "string"},
                                 "expires_in": {"type": "integer"}, "scope": {"type": "string"},
                                 "step_up_password": {"type": "string"}, "step_up_grant": {"type": "string"}},
                                desc="Form fields"),
                     example="grant_type=client_credentials&username=ci-bot",
                     ctype="application/x-www-form-urlencoded", required=True),
       responses={"200": r("Minted", schema=S("TokenResponse"),
                           example={"access_token": "<64hex>", "token_id": "<id>",
                                    "expires_in": 2592000, "scope": "api:*"}),
                  "400": r("invalid_request", schema=S("OAuthError"),
                           example={"error": "invalid_request", "error_description": "missing grant_type parameter"}),
                  "401": r("invalid_client / step_up_*", schema=S("OAuthError"))})

    op("/api/security/token/revoke", "post", "tokenRevoke", "tokens", "Revoke a token",
       "Canonical path: `POST /binflow/api/security/token/revoke` (admin only). Unknown token: 403 "
       "`{\"error\":\"access_denied\",\"error_description\":\"token not found\"}`.",
       req_body=body("token_id=<the token_id from above>", schema=obj({"token_id": {"type": "string"}}),
                     example="token_id=42", ctype="application/x-www-form-urlencoded", required=True),
       responses={"200": r("Revoked"),
                  "403": r("token not found", schema=S("OAuthError"),
                           example={"error": "access_denied", "error_description": "token not found"})})

    # ---- permissions ----
    op("/api/v1/permissions", "post", "permissionCreate", "permissions",
       "Create a permission target (create-or-replace)",
       "Canonical path: `POST /binflow/api/v1/permissions`. The action set is a **closed five-value set** "
       "`read / deploy-cache / annotate / delete / manage` — `write` is still accepted as a compatibility alias for "
       "`deploy-cache` (**does not imply annotate**), and GET echoes always use the canonical names; `annotate` alone "
       "gates property writes; manage holders can edit targets within their coverage set. The write gate = "
       "CapSecurityWrite OR the coverage arm (body-dependent, adjudicated inside the handler).",
       req_body=body("Permission Target", schema=S("PermissionTargetInput"),
                     example={"name": "devs-rw", "repos": ["dev-local"],
                              "principals": {"groups": {"devs": ["read", "deploy-cache", "annotate", "delete", "manage"]}}}),
       responses={"200": r("Created/replaced (echoed in canonical form)", schema=S("PermissionTarget")),
                  "400": ERR_401, "401": ERR_401})

    op("/api/v1/permissions", "get", "permissionList", "permissions", "List permission targets",
       "Canonical path: `GET /binflow/api/v1/permissions[?filter=manage]` (admin / readonly_admin). "
       "principals echoes actions in the **canonical single form**: `read, deploy-cache, annotate, delete, manage` — "
       "the `write` alias is accepted but never echoed. `?filter=manage`: the subset of targets within the manage "
       "holder's reach (admin/readonly_admin get byte-identical responses with and without the parameter; "
       "partially-covered targets are hidden; an empty coverage set 403s). `?filter=` (empty value) = no ask at all; "
       "unknown values → 400 errors[] envelope (`filter must be \"manage\" (unknown filter value: \"bogus\")`).",
       params=[q("filter", "manage", schema={"type": "string", "enum": ["manage"]})],
       responses={"200": r("Permission target list", schema=arr(S("PermissionTarget"))),
                  "400": r("Unknown filter value", schema=S("ErrorsEnvelope")),
                  "403": ERR_403})

    op("/api/v1/permissions/{name}", "delete", "permissionDelete", "permissions",
       "Delete a permission target",
       "Canonical path: `DELETE /binflow/api/v1/permissions/{name}` (**204**, no body). "
       "The deleted target's repo set comes from the stored row; going outside the manage coverage → 403.",
       params=[pp("name", "Target name")],
       responses={"204": r("Deleted (no body)"), "403": ERR_403})

    # ---- keypair 族 ----
    op("/api/security/keypair", "post", "keypairImport", "keypairs", "Import a key pair",
       "Canonical path: `POST /binflow/api/security/keypair` (create-or-replace; 201 echoes a KeyPairSummary). "
       "The `X-GPG-PASSPHRASE` header is not accepted (the passphrase is sealed with the key row).",
       req_body=body("KeyPairInput", schema=S("KeyPairInput")),
       responses={"201": r("KeyPairSummary", schema=S("KeyPairSummary"))})

    op("/api/security/keypair", "put", "keypairUpdate", "keypairs", "Update a key pair",
       "Canonical path: `PUT /binflow/api/security/keypair` (not found → 404; the rotation face).",
       req_body=body("KeyPairInput", schema=S("KeyPairInput")),
       responses={"200": r("Updated"), "404": r("Not found")})

    op("/api/security/keypair", "get", "keypairList", "keypairs", "List key pairs",
       "Canonical path: `GET /binflow/api/security/keypair` (bare array).",
       responses={"200": r("KeyPairSummary list", schema=arr(S("KeyPairSummary")))})

    op("/api/security/keypair/{pairName}", "get", "keypairGet", "keypairs", "Get a key pair",
       "Canonical path: `GET /binflow/api/security/keypair/{pairName}` (unknown name 404).",
       params=[pp("pairName", "Key pair name")],
       responses={"200": r("KeyPairSummary", schema=S("KeyPairSummary")), "404": r("Unknown name")})

    op("/api/security/keypair/{pairName}", "delete", "keypairDelete", "keypairs", "Delete a key pair",
       "Canonical path: `DELETE /binflow/api/security/keypair/{pairName}`. 200 plain text `OK`; "
       "referenced by repositories → 400 naming the referencing repositories.",
       params=[pp("pairName", "Key pair name")],
       responses={"200": r("OK (plain text)", schema={"type": "string"}, example="OK", ctype="text/plain"),
                  "400": r("Referenced by repositories — names the referencing repositories (plain text)",
                           schema={"type": "string"},
                           ctype="text/plain")})

    op("/api/security/keypair/verify", "post", "keypairVerify", "keypairs", "Verify a key pair",
       "Canonical path: `POST /binflow/api/security/keypair/verify`. 200 plain text `Key was verified.`; "
       "the body is either the full material or (a BinFlow extension) just `{\"pairName\":…}` to verify the stored "
       "sealed key.",
       req_body=body("Full material, or pairName only", schema=obj({"pairName": {"type": "string"}})),
       responses={"200": r("Key was verified. (plain text)", schema={"type": "string"},
                           example="Key was verified.", ctype="text/plain")})

    op("/api/security/keypair/public/repositories/{repoKey}", "get", "keypairPublicByRepo",
       "keypairs", "Armored public key of the repository's associated key pair",
       "Canonical path: `GET /binflow/api/security/keypair/public/repositories/{repoKey}` (text/plain).",
       params=[pp("repoKey", "Repository key")],
       responses={"200": r("Armored public key (text/plain)", schema={"type": "string"}, ctype="text/plain")})

    op("/api/v1/admin/security/keypair/generate", "post", "keypairGenerate", "keypairs",
       "Generate a key pair server-side (BinFlow-native face)",
       "Canonical path: `POST /binflow/api/v1/admin/security/keypair/generate` (201 echoes a summary; duplicate name "
       "409) — official Artifactory REST has no keygen; this endpoint is a BinFlow-native management face.",
       req_body=body("Generation input", schema=S("KeyPairGenerateInput"), required=True),
       responses={"201": r("KeyPairSummary", schema=S("KeyPairSummary")), "409": r("Duplicate name")})

    op("/api/v2/repositories/{repoKey}/keyPairs", "post", "keypairAssociate", "keypairs",
       "Associate a key pair with a repository",
       "Canonical path: `POST /binflow/api/v2/repositories/{repoKey}/keyPairs` (text/plain body = the key pair name). "
       "Only local `debian`/`rpm` repositories accept `keyPairName`; other package types get 400 by name.",
       params=[pp("repoKey", "Repository key")],
       req_body=body("Key pair name (text/plain)", schema={"type": "string"}, ctype="text/plain", required=True),
       responses={"200": r("Associated")})

    op("/api/v2/repositories/{repoKey}/keyPairs/{keyName}", "delete", "keypairDisassociate",
       "keypairs", "Disassociate a key pair from a repository",
       "Canonical path: `DELETE /binflow/api/v2/repositories/{repoKey}/keyPairs/{keyName}`.",
       params=[pp("repoKey", "Repository key"), pp("keyName", "Key pair name")],
       responses={"200": r("Disassociated")})

    # ---- 认证配置面 ----
    for seg, label in (("ldap", "LDAP"), ("oauth", "OIDC"), ("saml/config", "SAML")):
        base = "/api/v1/admin/security/" + seg
        oid = seg.replace("/", "").replace("ldap", "Ldap").replace("oauth", "Oidc").replace("samlconfig", "Saml")
        op(base, "get", "authConfig%sGet" % oid, "auth-config", "Read the %s section" % label,
           "Canonical path: `GET /binflow/api/v1/admin/security/%s` (CapSecurityRead). The %s section%s"
           % (seg, label, " returns the default shape when unset" if seg == "ldap" else " returns `{}` on GET when unset" if "saml" in seg else " (snake_case wire)"),
           responses={"200": r("%s configuration section (secret sentinel, 20 asterisks)" % label,
                               schema=obj({}, desc="Field table in the authentication configuration guide"))})
        op(base, "put", "authConfig%sPut" % oid, "auth-config", "Replace the %s section wholesale" % label,
           "Canonical path: `PUT /binflow/api/v1/admin/security/%s` (CapSecurityWrite; **takes effect on save**, no "
           "restart needed). Secret sentinel semantics (write-only): GET always returns 20 asterisks "
           "`********************` for a set secret; on PUT an absent key = keep, `\"\"` = clear, new plaintext = "
           "replace; **sending the sentinel back → 400** `refusing the masked placeholder — leave the field empty to "
           "keep the stored secret, or re-enter the value`. Secrets are sealed with enc:v1 before storage (instance "
           "master key `BINFLOW_REMOTE_CREDENTIALS_KEY`); without a master key, secret writes are refused." % seg,
           req_body=body("Whole-section configuration", schema=obj({}, desc="Field table in the authentication configuration guide")),
           responses={"200": r("Saved (echoed section)")})
        op(base + "/test", "post", "authConfig%sTest" % oid, "auth-config", "Test the %s connection" % label,
           "Canonical path: `POST /binflow/api/v1/admin/security/%s/test` (CapSecurityWrite — the probe opens an "
           "outbound connection). The test response is "
           "`{\"ok\":bool,\"phase\":\"…\",\"category\":\"…\",\"message\":\"…\"}`; `ok:false` yields HTTP 400 "
           "(e.g. `{\"ok\":false,\"phase\":\"dial\",\"category\":\"unreachable\",\"message\":\"could not connect to "
           "the target (dial failed or timed out)\"}`). Audit: `auth.config.update` (detail carries changed key names "
           "only, values are not stored) / `auth.config.test`." % seg,
           responses={"200": r("TestReport", schema=S("TestReport")),
                      "400": r("ok:false, same shape with 400", schema=S("TestReport"))})

    op("/api/v1/admin/security/saml/config/key/public", "get", "samlKeyPublic", "auth-config",
       "Current SP encryption certificate PEM",
       "Canonical path: `GET /binflow/api/v1/admin/security/saml/config/key/public` (CapSecurityRead; text/plain). "
       "Not yet generated → 404 `saml sp encryption certificate has not been generated`.",
       responses={"200": r("Certificate PEM (text/plain)", schema={"type": "string"}, ctype="text/plain"),
                  "404": r("saml sp encryption certificate has not been generated",
                           schema={"type": "string"}, ctype="text/plain")})

    op("/api/v1/admin/security/saml/config/key/public/regenerate", "put", "samlKeyRegenerate",
       "auth-config", "Rotate the SP key pair",
       "Canonical path: `PUT /binflow/api/v1/admin/security/saml/config/key/public/regenerate` (CapSecurityWrite). "
       "A forced one-for-one replacement; the old certificate is invalidated immediately; the response body is the new "
       "certificate PEM. Audited as `auth.config.samlkey.regenerate` (no key material is logged).",
       responses={"200": r("New certificate PEM (text/plain)", schema={"type": "string"}, ctype="text/plain")})

    op("/api/v1/admin/security/saml/key", "post", "samlKeyGenerate", "auth-config",
       "Generate/replace the SP key pair (BinFlow-native face)",
       "Canonical path: `POST /binflow/api/v1/admin/security/saml/key` (CapSecurityWrite). "
       "Same machinery as regenerate, with a distinct audit action: `auth.config.samlkey.{generate,regenerate}`.",
       responses={"200": r("New certificate PEM (text/plain)", schema={"type": "string"}, ctype="text/plain")})
