---
title: API Reference
sidebar_position: 70
---

# API Reference

> This page applies to **BinFlow v1.0.0**. BinFlow-native endpoints carry the `/api/v1` prefix; all other management endpoints follow the Artifactory-compatible REST conventions.

The BinFlow API spans four surfaces:

1. **Artifact content paths** (no `/api` prefix): `/binflow/<repoKey>/<path>` — upload, download and delete artifacts; every protocol client goes through here.
2. **Management API** (`/binflow/api/*`): repository CRUD, users/groups/permissions, audit, GC, tokens, search, system info.
3. **Docker Registry root-level plane** (`/v2/*`) — separately routed, not under the `/binflow` prefix; see the [Docker registry guide](docker-registry.md).
4. **Webhook event surface** (`/binflow/event/api/v1/*`) — subscription CRUD, test sends, troubleshooting; see the [Webhooks guide](admin/webhooks.md).

An interactive, machine-readable version of this reference is published as an OpenAPI spec on the documentation site.

---

## Authentication

BinFlow accepts three kinds of credentials, each suited to a different scenario.

### Basic authentication

Use with curl, CI scripts and client credential files (Maven `settings.xml`, `.pypirc`, npm `_auth`). At the REST API layer credentials must always be present and correct in the request — there is no challenge-then-retry dance; a failed authentication is an immediate 401:

```bash
# The -u shorthand (recommended)
curl -su admin:<password> $BASE/binflow/api/system/ping

# The equivalent explicit Authorization header
curl -s -H "Authorization: Basic $(printf 'admin:<password>' | base64 -w0)" \
  $BASE/binflow/api/system/ping
```

Every `/binflow/api/*` route accepts Basic authentication. Passwords are hashed with **argon2id** (memory-hard); for high-throughput scenarios use an access token instead.

### Bearer tokens

Best for high QPS, CI/CD pipelines and headless clients. Token validation skips the argon2 hash entirely, so it is far cheaper than Basic:

```bash
# Mint a token (admins may mint for any user; non-admins mint for themselves)
TOKEN=$(curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/token \
  -d 'grant_type=client_credentials&username=ci-bot' | jq -r '.access_token')
# 200: {"access_token":"<64hex>","token_id":"<id>","expires_in":2592000,"scope":"api:*"}

# Use the token
curl -s -H "Authorization: Bearer $TOKEN" $BASE/binflow/api/v1/storage/stats

# Revoke a token
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/token/revoke \
  -d "token_id=<the token_id from above>"
```

| Property | Value |
|---|---|
| Token length | 64 hex characters (256-bit) |
| Default TTL | 2592000 seconds (30 days); adjustable via `auth__token_default_ttl_hours` |
| Minting | Admins mint for any user; non-admins for themselves only. The body accepts optional `step_up_password` / `step_up_grant` (required on the non-admin session arm only when `auth.token_step_up` is enabled — see the [step-up guide](admin/token-step-up.md)) |
| Revocation | Admin only; all tokens live in one table |
| Audit | Issuance is not audited; `token.revoke` is |
| Docker token flow | Uses the same table — a revocation on the management API takes effect on docker tokens immediately |

### Console session cookie

Used by the web console. The server-issued `binflow_session` cookie (HttpOnly; Path=/binflow; SameSite=Lax) rides along with same-origin requests automatically:

```bash
# Log in (JSON) → Set-Cookie: binflow_session=<id>; HttpOnly; Path=/binflow; SameSite=Lax
curl -s -c jar.txt -X POST $BASE/binflow/api/v1/session \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<password>"}'
# 200 {"username":"admin","admin":true}

# Whoami (while the session is valid)
curl -s -b jar.txt $BASE/binflow/api/v1/session
# 200 {"username":"admin","admin":true}

# Log out (the session is revoked server-side)
curl -s -b jar.txt -X DELETE $BASE/binflow/api/v1/session -o /dev/null -w '%{http_code}'
# 204

# Replaying the same cookie after expiry → 401
```

The session TTL defaults to 24 hours and **activity does not extend it** (sliding renewal is capped by the absolute TTL). The login entry is exempt from invalid-cookie rejection, which makes it immune to cookie tossing. See the [console guide](console.md).

> **CSRF protection**: non-GET/HEAD writes authenticated by the session cookie that carry a cross-origin `Origin` header get **403**. Basic and token authentication are naturally immune.

---

## Error formats

### errors[] envelope (primary format)

The primary error format of the artifact domain and the management API:

```json
// 404 — artifact not found
HTTP/1.1 404 Not Found
Content-Type: application/json

{"errors":[{"status":404,"message":"Unable to find the requested resource 'generic-local/missing.jar'."}]}

// 413 — quota exceeded
HTTP/1.1 413 Request Entity Too Large
Content-Type: application/json

{"errors":[{"status":413,"message":"Repository 'tiny' quota exceeded: used 800 of 934 bytes; the write to 'b.bin' needs 800 more bytes."}]}

// 409 — checksum mismatch
HTTP/1.1 409 Conflict
Content-Type: application/json

{"errors":[{"status":409,"message":"Checksum error for 'maven-local/com/example/demo/1.0.0/demo-1.0.0.jar': received 'abc123' but actual is 'def456'."}]}

// 400 — invalid parameter
HTTP/1.1 400 Bad Request
Content-Type: application/json

{"errors":[{"status":400,"message":"Repository key must be at least 2 characters: 'x'"}]}

// 403 — no permission
HTTP/1.1 403 Forbidden
Content-Type: application/json

{"errors":[{"status":403,"message":"permission denied"}]}

// 401 — unauthenticated or invalid credentials (artifact domain)
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Basic realm="BinFlow"
Content-Type: application/json

{"errors":[{"status":401,"message":"invalid credentials"}]}
```

### Plain-text errors (user and group management)

Some errors in the user, group and token endpoints respond with plain-text bodies:

```bash
# 404 — group not found
HTTP/1.1 404 Not Found
Content-Type: text/plain; charset=utf-8

Unable to find group by name 'nonexistent-group'.

# 400 — invalid group name
HTTP/1.1 400 Bad Request
Content-Type: text/plain; charset=utf-8

Unable to create group: name must match [a-z][a-z0-9._-]* but it starts with uppercase 'X'.

# 400 — creating a user without an email
HTTP/1.1 400 Bad Request
Content-Type: text/plain; charset=utf-8

Please provide a valid user email.

# 400 — user references a nonexistent group
HTTP/1.1 400 Bad Request
Content-Type: text/plain; charset=utf-8

Unable to find group by name 'devs'. Please make sure the group exists before adding users to it.

# 400 — admin/adminRole boolean contradiction (only admins may write role fields)
HTTP/1.1 400 Bad Request
Content-Type: text/plain; charset=utf-8

conflicting 'admin' and 'adminRole' fields: admin=false is incompatible with adminRole="admin" (admin=true is equivalent to adminRole=admin)

# 409 — deleting a group referenced by permissions
HTTP/1.1 409 Conflict
Content-Type: text/plain; charset=utf-8

Cannot delete group 'devs': it is referenced by permission target(s): devs-rw, jane-rd. Remove the group from those targets first.

# DELETE /api/security/users/{name} belongs to the plain-text family as well:
# success 200 / guardrail 400 / missing target 404 — every message verbatim in the users table below
```

### OAuth-style errors (token endpoints and the docker domain)

The token mint/revoke endpoints and `/v2/token` follow the OAuth 2.0 error conventions:

```bash
# Token minting: management error
HTTP/1.1 400 Bad Request
Content-Type: application/json

{"error":"invalid_request","error_description":"missing grant_type parameter"}

# Token revocation: unknown token
HTTP/1.1 403 Forbidden
Content-Type: application/json

{"error":"access_denied","error_description":"token not found"}

# Token minting: authentication failed
HTTP/1.1 401 Unauthorized
Content-Type: application/json
WWW-Authenticate: Basic realm="BinFlow"

{"error":"invalid_client","error_description":"authentication failed"}

# Step-up authentication, two forms (the non-admin session arm when auth.token_step_up is enabled)
# 401 — the second credential is missing (local/LDAP without step_up_password; OIDC without step_up_grant)
{"error":"step_up_required","error_description":"step-up authentication required to mint a token"}

# 401 — the second credential was rejected / grant expired / grant reused (single-use, deleted on consumption)
{"error":"step_up_invalid","error_description":"step-up credential rejected, expired, or already used"}

# Docker /v2 challenge
HTTP/1.1 401 Unauthorized
Content-Type: application/json
Docker-Distribution-Api-Version: registry/2.0
WWW-Authenticate: Bearer realm="http://localhost:8080/v2/token",service="binflow"

{"errors":[{"code":"UNAUTHORIZED","message":"authentication required","detail":null}]}

# Docker /v2 insufficient permission
HTTP/1.1 403 Forbidden
Content-Type: application/json
Docker-Distribution-Api-Version: registry/2.0

{"errors":[{"code":"DENIED","message":"requested access to the resource is denied","detail":null}]}
```

---

## Request and response headers

### Common request headers

| Header | Where | Purpose |
|---|---|---|
| `Authorization: Basic <base64>` | All management + content paths | Basic authentication |
| `Authorization: Bearer <token>` | All management + content paths + docker | Token authentication |
| `X-Checksum-Sha1` / `X-Checksum-Sha256` / `X-Checksum-Md5` | PUT upload | Client-declared checksum |
| `X-Checksum` | PUT upload | Checksum without a type marker (auto-detected by length) |
| `X-Checksum-Deploy: true` | PUT upload | Checksum-only deploy (no body) |
| `Expect: 100-continue` | PUT upload | Dedup fast path — checks blob existence first |
| `Content-Type` | manifest PUT | Passed through, not whitelisted (docker domain) |
| `X-Explode-Archive` / `X-Explode-Archive-Atomic` | PUT upload | Explode deploy (`true`): store the archive's members, not the archive |

### Common response headers

| Header | Where | Purpose |
|---|---|---|
| `X-Checksum-Sha1` / `X-Checksum-Sha256` / `X-Checksum-Md5` | GET download | Server-measured checksum (sent only when available) |
| `ETag: <sha1>` | GET download | Without surrounding quotes; conditional requests via `If-None-Match` |
| `Last-Modified` | GET download | RFC1123 format |
| `Accept-Ranges: bytes` | GET download | Range request support |
| `X-Artifactory-Filename` | GET download | URL-encoded file name |
| `Location` | PUT upload success | URL of the new resource |
| `X-Binflow-Exploded-Files: <n>` | Explode deploy | Count of files stored |
| `Cache-Control: no-store` | Repository lists and other sensitive data | Caching forbidden |

---

## Endpoint reference

Endpoints are grouped by functional domain. Permission notes use the instance roles: **admin**, **readonly_admin**, and regular users with per-repository grants.

### Artifacts and storage

Content paths — every protocol client (Maven, Go, Cargo, npm tarballs, …) deploys and resolves through this same family.

| Method | Path | Semantics |
|---|---|---|
| PUT | `/binflow/{repoKey}/{path}` | Upload a file (body is the content); checksum headers supported. **Matrix parameters** — trailing `;k=v` pairs are stripped off as deploy properties; an unpaired `;` stays literal in the file name. Explode deploy via `X-Explode-Archive: true` (whitelist zip/tar/tar.gz/tgz; success 201 empty body + `X-Binflow-Exploded-Files` count; the archive itself is not stored) |
| PUT | `/binflow/{repoKey}/{path}/` | Create a directory (trailing slash) |
| PUT | `/binflow/{repoKey}/{path}.sha1\|.md5\|.sha256` | Upload a checksum sidecar file |
| GET | `/binflow/{repoKey}/{path}` | Download a file (Range / If-None-Match / ETag). `.sha1\|.md5\|.sha256` suffixes return the bare hex; archive members are directly readable as `/{repo}/{archive}!/{entry}` (split at the first `!/`, nested recursively) |
| HEAD | `/binflow/{repoKey}/{path}` | File metadata — response headers identical to GET, no body |
| DELETE | `/binflow/{repoKey}/{path}` | Delete a file or a directory tree (recursive) |
| PUT | `/binflow/{repoKey}/{GAV path}` | Deploy a Maven artifact (strict layout validation); resolve via GET, checksums via the `.sha1/.md5/.sha256` suffixes |
| GET | `/binflow/api/storage/{repoKey}/{path}` | FileInfo / FolderInfo JSON. Query arms: `?properties=K1,K2*` (key filter + trailing `*` wildcard; no matches = 200 `{"properties":{}}` — a BinFlow ruling, not Artifactory's 404; a nonexistent node is 404), `?lastModified` (the directory's latest modification time), `?permissions` (effective-permissions view; admin only, local repositories only) |
| GET | `/binflow/api/storage/{repoKey}/{path}?stats` | Download statistics `{uri, downloadCount, lastDownloaded, lastDownloadedBy, remoteDownloadCount}`. Counts are visible on all tiers (the item-info read gate); `lastDownloadedBy` is returned only to admin / readonly_admin (omitted on lower tiers, never fabricated); the stats probe itself is not counted |
| PUT | `/binflow/api/storage/{repoKey}/{path}?properties=k=v1,v2[&recursive=1]` | Write properties — **merge semantics**: the value set of a same-named key is replaced wholesale, differently-named keys are kept; the node must exist (404) |
| DELETE | `/binflow/api/storage/{repoKey}/{path}?properties=k1,k2[&recursive=1]` | Delete properties (idempotent; nonexistent keys 204; `properties=*` deletes everything; folder + `recursive=1` applies recursively) |
| GET | `/binflow/api/storage/{repoKey}?list` | Streaming file listing (authenticated users only). Seven-parameter family: `deep` / `depth` / `listFolders` / `includeRootPath` / `mdTimestamps` / `statsTimestamps` / `includePropertiesMd5`; a parameter present but not an integer → 400 `For input string: "<v>"`. Content-Type is the vendor form `application/vnd.org.jfrog.artifactory.storage.FileList+json` |

Property grammar and merge examples: [properties guide](properties.md).

### Docker Registry (root-level /v2 plane)

Separately routed — not under the `/binflow` prefix. Client configuration and resumable uploads: [Docker registry guide](docker-registry.md).

| Method | Path | Semantics |
|---|---|---|
| GET | `/v2/` | API version check (401 challenge, unchanged by the instance's anonymous switch) |
| GET | `/v2/_catalog` | Repository catalog (`?n=` page size) |
| GET | `/v2/{name}/tags/list` | Tag list (an empty tag set returns `"tags":null`) |
| GET | `/v2/{name}/manifests/{ref}` | Get a manifest (tag or digest) |
| PUT | `/v2/{name}/manifests/{ref}` | Upload a manifest (Content-Type passed through, not whitelisted) |
| DELETE | `/v2/{name}/manifests/{digest}` | Delete a manifest (by digest only) |
| POST | `/v2/{name}/blobs/uploads/` | Start a blob upload session |
| GET | `/v2/{name}/blobs/uploads/{uuid}` | Upload status query — **204 + `Range: 0-<offset-1>`** is the authoritative offset (survives restarts) |
| PATCH | `/v2/{name}/blobs/uploads/{uuid}` | Upload a blob chunk (a mismatched `Content-Range` start → 416 empty body + the authoritative `Range`) |
| PUT | `/v2/{name}/blobs/uploads/{uuid}` | Complete a blob upload (`?digest=sha256:...`) |
| GET | `/v2/{name}/blobs/{digest}` | Download a blob |
| HEAD | `/v2/{name}/blobs/{digest}` | Blob existence check |
| DELETE | `/v2/{name}/blobs/{digest}` | **405 UNSUPPORTED** — blob deletion goes through GC only |
| GET/POST | `/v2/token` | Docker auth token endpoint (distribution token protocol) |
| GET | `/v2/{name}/referrers/` | **404** — the OCI referrers API is not supported |

### npm

The `/api/npm` mount rewrites onto the content face — `/binflow/api/npm/<repo>/<rest>` and `/binflow/<repo>/<rest>` address one node namespace.

| Method | Path | Semantics |
|---|---|---|
| GET | `/binflow/api/npm/{repoKey}/{pkg}` | Packument (package metadata); the scoped form `@scope%2Fpkg` is equivalent |
| PUT | `/binflow/api/npm/{repoKey}/{pkg}` | Publish a package (ten-step packument chain) |
| DELETE | `/binflow/api/npm/{repoKey}/{pkg}/-rev/{rev}` | Unpublish the whole package (`rev` is an opaque placeholder) |
| DELETE | `/binflow/api/npm/{repoKey}/{pkg}/-/{filename}/-rev/{rev}` | Unpublish a specific version (`filename` = `<name>-<version>.tgz`; scoped names carry the scope segment) |
| GET | `/binflow/{repoKey}/{name}/-/{name}-{v}.tgz` | Download a tarball (direct content path) |
| PUT | `/binflow/{repoKey}/{name}/-/{name}-{v}.tgz` | **405** — the npm domain accepts packument PUTs only |
| PUT | `/binflow/api/npm/{repoKey}/-/user/org.couchdb.user:{name}` | npm legacy login (couch user document family) — where `npm login --auth-type=legacy` lands. Credentials ride in the body (`name`/`password`); this path family is **exempt from the write-authentication gate** on npm repositories: the endpoint validates the body credentials and mints a token (**201 re-mints idempotently**, no 409); a wrong password gets 401 + Basic challenge. Applies to `packageType=npm` repositories only (the same path on a generic repository still 401s — the type is pinned to prevent anonymous writes) |
| PUT | `/binflow/api/npm/{repoKey}/-/user/org.couchdb.user:{name}/-rev/{rev}` | Login retry spelling (npm resends with a revision attached); served by the same arm |
| POST | `/binflow/api/npm/{repoKey}/-/v1/login` | **401** — the web login endpoint is not provided; npm (≥ 9 defaults to the web flow) automatically falls back to the couch chain, so the flow works |
| GET | `/binflow/api/npm/{repoKey}/-/whoami` | Current user (requires authentication; accounts without read permission on the repository get 403 — read-face ACL semantics) |
| GET | `/binflow/api/npm/{repoKey}/-/ping` | Connectivity probe (no authentication, `200 {}`) |

### PyPI

| Method | Path | Semantics |
|---|---|---|
| GET | `/binflow/api/pypi/{repoKey}/simple/` | Package list (PEP 503/629) |
| GET | `/binflow/api/pypi/{repoKey}/simple/{pkg}/` | Per-package index page (HTML + JSON, Accept-driven) |
| POST | `/binflow/api/pypi/{repoKey}/` | Upload (multipart `:action=file_upload`) |
| GET | `/binflow/api/pypi/{repoKey}/packages/{name}/{ver}/{file}` | Download a distribution (the URL twine echoes back) |

### Repositories

| Method | Path | Semantics |
|---|---|---|
| GET | `/binflow/api/repositories?type=&packageType=` | Repository list (admin / readonly_admin); both filters optional |
| GET | `/binflow/api/repositories/{key}` | Single repository configuration (manage holders can also read repositories in their coverage set) |
| PUT | `/binflow/api/repositories/{key}` | **Create only** — an existing key always gets 400 (`error when validating repository name: <key> : Repository key already exists`) with **zero side effects**; the only update spelling is POST. Admin only |
| POST | `/binflow/api/repositories/{key}` | Update, **merge semantics**: omitted field = keep the stored value; `null` / empty string = clear (array empty values kept, an object `{}` resets the whole family); explicit value = overwrite (`0` included). Unknown key 404. Carries `quotaBytes` quota writes; admin or the repository's manage holder |
| POST | `/binflow/api/repositories/{key}/test` | Remote repository upstream connectivity probe. Optional body `{url,username,password}` overrides the stored configuration for this probe only (zero writes; a changed URL/username without a password probes anonymously — the stored secret is never sent); an empty body probes with the stored configuration. Verdict 200/400 `{ok,status_code,message}`; only `rclass=remote` repositories have an upstream to test |
| DELETE | `/binflow/api/repositories/{key}` | Delete a repository (optional `?deleteContent=true`; admin only, not delegated) |

Notes on repository creation:

- `rclass=remote + packageType=docker` (community tier — no extra license slot) and `rclass=virtual + packageType=docker` (the aggregated read face serves the union of member repositories) are both open — **the rclass × packageType combination gates are fully retired**. The remaining gates are the **license tier** (advanced package types on lower tiers get 400 `package type not available on this instance: ...`) and the **remote browsing option**: `listRemoteFolderItems: true` on a remote repository body is accepted for helm/debian/rpm only; other package types get a 400 naming the supported set. See the [remote and virtual repositories guide](admin/remote-virtual.md).
- Remote (smart) repository fields: `enableTokenAuthentication` (`true` sends `Authorization: Bearer <password>` upstream on pulls; no password = stays anonymous) and the `contentSynchronisation` family (`enabled`, `propertiesEnabled` — best-effort property attachment from the upstream, failures only WARN; `statisticsEnabled` / `sourceOrigin` are accepted and echoed but currently carry no behavior). `chartsBaseUrl` (helm remotes only) sets the base address for content-type origin fetches (tgz/.prov/`_external` fold paths) — an absolute http(s) URL, `""` clears it; metadata (index.yaml) always goes to the repository URL; carrying the field on other package types gets a 400 naming it.

### Users and groups

This family's errors are plain-text response bodies.

| Method | Path | Semantics |
|---|---|---|
| GET | `/binflow/api/security/users` | User list (admin / readonly_admin). Entries `{name,uri,realm,source,email,adminRole,enabled,groups}` — `enabled`/`groups` always rendered (empty groups `[]` not null); one request carries every field the listing needs |
| POST | `/binflow/api/security/users` | Create a user (collection route, create-only; a BinFlow-native face). Body must include `name`; an existing name → **409** `The user already exists: <name>`; mixed-case usernames rejected 400. Success **201 no body** |
| GET | `/binflow/api/security/users/{name}` | User details — no password fields; `adminRole` and `enabled` always echoed. Unknown name → 404 `User not found` (text body) |
| PUT | `/binflow/api/security/users/{name}` | Create or replace a user (create-or-replace; both states 201). Body may include `adminRole` (admin-writable only) |
| POST | `/binflow/api/security/users/{name}` | Partial update — email/password/admin/groups/adminRole/enabled; `enabled` uses pointer semantics: an explicit `false` disables login (that user's logins and existing sessions get 401), `true` re-enables; absent leaves things unchanged |
| DELETE | `/binflow/api/security/users/{name}` | Delete a user (admin only). Success **200 plain text** `The user: '<name>' has been removed successfully.`; guardrails below, in a fixed check order |
| PUT | `/binflow/api/security/password` | Change the current user's password |
| POST | `/binflow/api/security/users/authorization/changePassword` | Alias password-change endpoint |
| GET | `/binflow/api/security/groups` | Group list (admin / readonly_admin); the list endpoint carries no member roll-up |
| GET | `/binflow/api/security/groups/{name}?includeUsers=true` | Group details; the parameter (literal `true`, case-sensitive) attaches `userNames: []` (empty group `[]` never null); other spellings (`false`/`junk`/`TRUE`) return the plain three-field shape 200 — no invented 400. Unknown group → 404 `Group not found` |
| PUT | `/binflow/api/security/groups/{name}` | Create or update a group (create 201 / update 200) |
| POST | `/binflow/api/security/groups/{name}` | Update the group description |
| DELETE | `/binflow/api/security/groups/{name}` | Delete a group — referenced by a permission target → 409 (plain text, names the targets) |

User deletion guardrails (all 400 plain text except the first):

| # | Guardrail | Response (verbatim) |
|---|---|---|
| 1 | Target missing | **404** `User not found` (text body, same shape as the single-user GET) |
| 2 | Built-in admin | 400 `Cannot delete the built-in admin user.` |
| 3 | Last admin | 400 `Cannot delete user '<name>'. There must be at least one user configured with admin privileges.` |
| 4 | Self-deletion | 400 `Cannot delete the current authenticated user.` |

Deletion cascades in one transaction: permission-target grant rows are stripped, the user row deleted, group memberships FK-cascaded, and **every token and web session revoked** (held Bearers turn 401 immediately); audit history is kept. **Repeated deletion is a deterministic 404 (intentionally not idempotent)** — read the second 404 as "already deleted" and do not retry. Successful deletions are audited as `user.delete`; guardrail rejections are not audited.

### Permissions

| Method | Path | Semantics |
|---|---|---|
| POST | `/binflow/api/v1/permissions` | Create a permission target (create-or-replace). The action set is a **closed five-value set** `read / deploy-cache / annotate / delete / manage` — `write` is still accepted as a compatibility alias for `deploy-cache` (**it does not imply annotate**), and GET echoes always use the canonical names. `annotate` alone gates property writes; manage holders can edit targets within their coverage set. See the [groups and permissions guide](admin/groups-permissions.md) |
| GET | `/binflow/api/v1/permissions` | List permission targets (admin / readonly_admin). principals echoes use the canonical single form. `?filter=manage` returns the subset of targets within a manage holder's reach (admins get byte-identical responses with and without the parameter; partially covered targets are hidden; an empty coverage set 403s); `?filter=` (empty) = no ask; unknown values 400 |
| DELETE | `/binflow/api/v1/permissions/{name}` | Delete a permission target (**204**, no body); the repo set comes from the stored row — going outside the manage coverage → 403 |

Repository-level administrators reach the permission editor through `?filter=manage` — see the [RBAC guide](admin/rbac-roles.md).

### Access tokens

| Method | Path | Semantics |
|---|---|---|
| POST | `/binflow/api/security/token` | Mint an access token — admins mint for any user, non-admins for themselves. Errors follow the OAuth 2.0 conventions (examples above) |
| POST | `/binflow/api/security/token/revoke` | Revoke a token (admin only). Unknown token → 403 `{"error":"access_denied","error_description":"token not found"}` |

### Search

Results are always filtered by the caller's permissions. Language subset, error message family and the Artifactory migration table: [AQL search guide](aql.md).

| Method | Path | Parameters | Semantics |
|---|---|---|---|
| GET | `/binflow/api/search/artifact` | `name=` (required, case-insensitive substring), `repos=a,b` | Search by name substring (SQL LIKE) |
| GET | `/binflow/api/search/checksum` | `sha1=` / `md5=` / `sha256=` (at least one), `repos=a,b` | Exact checksum search |
| POST | `/binflow/api/search/aql` | body = AQL text (`text/plain`); `?compact=true`; `?query=` empty-body fallback | **AQL query** — the items domain subset plus the `stat.*` statistics field family. Limits: results 1,000 rows (over cap sets `X-Binflow-Search-Truncated: true`, continue with `.offset()`); query text 6,000 characters; concurrency 4 → 429 + `Retry-After: 1`; execution 10s → 408. Not available to anonymous callers (closed instances 401 / anonymous-enabled instances 403); unsupported domains and fields are rejected 400 by name. Virtual keys are legal — member repositories expand at compile time |
| GET | `/binflow/api/search/usage` | `notUsedSince=` (required, epoch milliseconds), `createdBefore=` (defaults back to notUsedSince), `repos=a,b` | **Unused artifact search** — the data face for "not downloaded in N days" cleanup strategies. Rows carry `{uri, downloadCount, lastDownloaded, remoteDownloadCount, remoteLastDownloaded}`; both the empty set and missing parameters return **404 `No results found.`** |
| GET | `/binflow/api/search/creation` | `from=` (required, epoch milliseconds), `to=` (defaults to now), `repos=a,b` | Search by creation time. Row shape `{uri, created}` — rows matched only via lastModified return the modification time; empty set 404 `No results found.`; anonymous → 401 |
| GET | `/binflow/api/search/dates` | `from=`, `to=`, `dateFields=` (CSV over the closed set `created, lastModified, lastDownloaded, remote_last_downloaded`), `repos=a,b` | Search by date field (default `{created, lastModified}`); unknown field names 400 verbatim; empty set 404 `No results found.` |
| GET | `/binflow/api/search/gavc` | `g=/a=/v=/c=` (at least one), `repos=a,b` | Maven coordinate search — matches Maven layout path forms (fully literal, case-sensitive); does not filter by repository package type or layout descriptors (`repos=` narrows) |
| GET | `/binflow/api/search/prop` | `props=k[=v]` or any `?k=v` parameter (`repos` reserved) | Property search — a key without a value = key existence; key-value pairs follow the property grammar (invalid keys 400). `prop` is the official singular spelling; the plural `props` is 404 |
| GET | `/binflow/api/search/pattern` | `pattern=<repo-glob>:<path-glob>` | Path pattern search — `*`/`?` cross segments (SQL semantics, the same engine as AQL `$match`); the repo half may wildcard across repositories; querying a virtual key returns an empty set (member expansion happens only on the AQL face) |
| GET | `/binflow/api/search/props\|users\|artifactory\|badge` | — | **404** — intentionally not provided |

The `gavc`/`prop`/`pattern` endpoints share the family envelope `{"results":[FileInfo…]}` with `artifact`/`checksum`; misses are always **200 + `results:[]`** (not 404), capped at 1,000 rows with the same `X-Binflow-Search-Truncated` truncation header.

```bash
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/search/aql \
  --data-binary 'items.find({"repo":"maven-local"}).include("repo","path","name").sort({"$desc":["name"]}).limit(2)'
# 200
# {
# "results" : [ { "repo" : "maven-local", "path" : "com/acme/demo", "name" : "maven-metadata.xml" }, ... ],
# "range" : { "start_pos" : 0, "end_pos" : 2, "total" : 2, "limit" : 2 }
# }

# Property search: documented form, or any parameter as a key
curl -su admin:$ADMIN_PW "$BASE/binflow/api/search/prop?props=stage=prod"
curl -su admin:$ADMIN_PW "$BASE/binflow/api/search/prop?stage=prod&repos=team-local"

# Path patterns cross segments and may wildcard repositories
curl -su admin:$ADMIN_PW "$BASE/binflow/api/search/pattern?pattern=maven-local:com/acme/**/*.jar"
curl -su admin:$ADMIN_PW "$BASE/binflow/api/search/pattern?pattern=*-local:**/build-*.bin"
curl -su admin:$ADMIN_PW "$BASE/binflow/api/search/pattern?pattern=nocolon"
# 400 ... "Pattern search requires a '<repo-pattern>:<path-pattern>' value."
```

### Artifact operations

The whole family sits behind the `repo-operations` pro slot — community instances answer 403 + `X-Binflow-License-Required: repo-operations`. Behavior detail and verbatim errors: [artifact operations guide](admin/artifact-operations.md).

| Method | Path | Semantics |
|---|---|---|
| POST | `/binflow/api/copy/{srcRepo}[/{srcPath}]?to=/{targetRepo}[/{targetPath}]` | Tree-level copy (zero-copy). `dry=1` for a dry run; responds 200 + `messages[]` (vendor Content-Type `application/vnd.org.jfrog.artifactory.storage.CopyOrMoveResult+json`); the status = the code of the last error message (409 fallback). Per-file pipeline: source read / target write |
| POST | `/binflow/api/move/{srcRepo}[/{srcPath}]?to=…` | Tree-level move — copy + source deletion + directory pruning (additionally requires source `delete`) |
| GET | `/binflow/api/archive/download/{repo}[/{path}]?archiveType=zip\|tar\|tar.gz\|tgz` | Streaming archive download of a directory or whole repository (nothing written to disk); `includeChecksumFiles=true` includes checksum sidecar entries. **Off by default** — `folder_download.enabled` and five more knobs, effective on restart |
| GET | `/binflow/{repo}/{archive}!/{entry}` | Read an archive member directly (first `!/` splits, nested archives recurse, `.sha1/.md5/.sha256` suffixes return the bare hex); non-GET 405 |
| PUT | `/binflow/{repo}/{path}` + `X-Explode-Archive: true` | Explode deploy — whitelist zip/tar/tar.gz/tgz; success **201 empty body** + `X-Binflow-Exploded-Files: <n>`; requires `w` on the target parent directory |

`/api/flat/copy|move` is not implemented (404).

### Trash can

Gate: system:write (full admins only) + the `trashcan` pro slot (interim). Capture and retention semantics: [trash can guide](admin/trash-can.md).

| Method | Path | Semantics |
|---|---|---|
| POST | `/binflow/api/trash/restore/{path}?to=&transaction-size=` | Restore — `to` overrides > five-tuple inference > first path segment; `trash.*` markers stripped, original properties kept; the response is isomorphic to copy/move `messages[]` |
| POST | `/binflow/api/trash/empty` | Empty the whole can; JSON summary `{"removed","files","folders","bytes"}` |
| DELETE | `/binflow/api/trash/clean/{path}` | Permanently purge a single entry (subtree); same summary shape |

Browsing is not a fourth route — it rides the regular `GET /api/storage/auto-trashcan[...][?properties|?list]` face.

### Key pairs

GPG key pairs for repository metadata signing. Private keys and passphrases never leave the store (no export endpoint). Consumption: debian `InRelease`/`Release.gpg`, rpm `repomd.xml.asc`/`.key` — see the [Debian](integrations/debian.md) and [RPM](integrations/rpm.md) guides.

| Method | Path | Semantics |
|---|---|---|
| POST | `/binflow/api/security/keypair` | Import (create-or-replace; 201 echoes a KeyPairSummary). The `X-GPG-PASSPHRASE` header is not accepted — the passphrase is sealed with the key row |
| PUT | `/binflow/api/security/keypair` | Update (not found → 404; the rotation face) |
| GET | `/binflow/api/security/keypair` | List (bare array) |
| GET | `/binflow/api/security/keypair/{pairName}` | Get one (unknown name 404) |
| DELETE | `/binflow/api/security/keypair/{pairName}` | Delete; 200 plain text `OK`; referenced by repositories → 400 naming the referencing repositories |
| POST | `/binflow/api/security/keypair/verify` | 200 plain text `Key was verified.`; the body is the full material, or (BinFlow extension) just `{"pairName":…}` to verify the stored sealed key |
| GET | `/binflow/api/security/keypair/public/repositories/{repoKey}` | The repository's associated key pair, armored public key (text/plain) |
| POST | `/binflow/api/v1/admin/security/keypair/generate` | **Generate server-side** (201 echoes a summary; duplicate name 409) — official Artifactory REST has no keygen; this is a BinFlow-native management face |
| POST / DELETE | `/binflow/api/v2/repositories/{repoKey}/keyPairs[/{keyName}]` | Associate (text/plain body = the key pair name) / disassociate — only local `debian`/`rpm` repositories accept `keyPairName`; other package types get 400 by name |

Import body = `{pairName, pairType ("GPG"), alias, privateKey, publicKey, passphrase}`; generation body = `{pairName, alias, passphrase, keyBits, uidName, uidComment, uidEmail}`. Summaries carry `{pairName, pairType, alias, publicKey}` plus additive fields (`algorithm`/`createdAt`/`updatedAt`/`updatedBy`/`repositories`).

### Authentication configuration

Three sections (`ldap` / `oauth` / `saml/config`), each with GET (read) / PUT (replace wholesale — **takes effect on save**, no restart) / POST `…/test` (connectivity probe), plus the SAML SP certificate endpoints. Field tables and the console face: [authentication configuration guide](admin/auth-config.md).

| Method | Path | Semantics |
|---|---|---|
| GET/PUT/POST `…/test` | `/binflow/api/v1/admin/security/ldap` | LDAP section (unset GET returns the default shape) |
| GET/PUT/POST `…/test` | `/binflow/api/v1/admin/security/oauth` | OIDC section (snake_case wire) |
| GET/PUT/POST `…/test` | `/binflow/api/v1/admin/security/saml/config` | SAML section (unset GET returns `{}`) |
| GET | `/binflow/api/v1/admin/security/saml/config/key/public` | **Current SP encryption certificate PEM** (text/plain); not yet generated → 404 `saml sp encryption certificate has not been generated` |
| PUT | `/binflow/api/v1/admin/security/saml/config/key/public/regenerate` | Rotate the SP key pair (forced one-for-one replacement, the old certificate invalidated immediately); the response body is the new certificate PEM |
| POST | `/binflow/api/v1/admin/security/saml/key` | Generate/replace the SP key pair (BinFlow-native face; same machinery as regenerate, a distinct audit action) |

- **Secret sentinel semantics (write-only)**: GET always returns 20 asterisks `********************` for a set secret; on PUT an absent key = keep, `""` = clear, new plaintext = replace; **sending the sentinel back → 400** `refusing the masked placeholder — leave the field empty to keep the stored secret, or re-enter the value`.
- Secrets are sealed with enc:v1 before storage (instance master key `BINFLOW_REMOTE_CREDENTIALS_KEY`); without a master key, secret writes are refused.
- Test responses: `{"ok":bool,"phase":"…","category":"…","message":"…"}`; `ok:false` yields HTTP 400 (e.g. `{"ok":false,"phase":"dial","category":"unreachable","message":"could not connect to the target (dial failed or timed out)"}`).
- Audit: `auth.config.update` (detail carries changed key names only, values are never stored) / `auth.config.test` / `auth.config.samlkey.{generate,regenerate}` (no key material is logged).

### System

| Method | Path | Semantics |
|---|---|---|
| GET | `/binflow/api/system/ping` | Liveness probe (no authentication) |
| GET | `/binflow/api/system/version` | Version info (no authentication) |
| GET | `/binflow/api/v1/health` | Health dashboard (admin / readonly_admin); deployment probes should use the unauthenticated `/healthz` / `/readyz` |
| GET | `/binflow/api/v1/storage/stats` | Instance-wide storage statistics (admin / readonly_admin) |
| GET | `/binflow/api/v1/storage/usage/{repo}` | Single-repository quota usage — admin / readonly_admin / anyone granted `read` **or** `manage` on the repository; row shape `{"repo","usedBytes","quotaBytes"}` |
| GET | `/binflow/api/v1/storage/usage` | **Batch quota usage** — **bare array** (no envelope, no pagination; an empty visible set is `200 []` never null); a regular user gets the subset of repositories they can `read` or `manage`; anonymous 401. Naming an unknown repository and naming one without permission silently omit the same way. `?repos=` picks a named set; `?include=counts` attaches `nodeCount`/`updatedAt` (nodeCount counts file nodes only; updatedAt = time of the repository configuration change); unknown values 400 |
| GET | `/binflow/api/v1/audit` | Audit log query (admin / readonly_admin) — parameters below |
| POST | `/binflow/api/v1/system/gc` | Trigger GC (admin only; readonly_admin 403 including dry-run). No GET route — query past runs through the audit `gc.run` events. Body below |
| GET | `/binflow/api/v1/system/settings` | Echoes the **resolved** runtime knobs (YAML + env + defaults merged). Knob-scoped by design — behavioral knobs only, never secrets/DSNs/paths; read-only; the knobs themselves take effect on restart |
| GET/POST/DELETE | `/binflow/api/v1/system/query_rate_limiter/config` | Query rate limiter configuration: read / merge-write / reset to factory defaults. While the limiter is **disabled, all three verbs return 400 plain text** `Query rate limiter is disabled` (except a POST carrying an explicit mode). The configuration lives for the process lifetime — a restart returns to factory defaults. Rate limiting only delays, never rejects |

```bash
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/storage/usage
# [{"repo":"g-local","usedBytes":10,"quotaBytes":0}, ...]

curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/storage/usage?repos=g-local,no-such"   # unknown names silently omitted
# [{"repo":"g-local","usedBytes":10,"quotaBytes":0}]

curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/storage/usage?include=counts"
# rows gain {"nodeCount":1,"updatedAt":"2026-08-24T20:37:26Z"}

curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/system/settings
# {"folder_download":{"enabled":false,"enabled_for_anonymous":false,
#   "max_download_size_mb":1024,"max_files":5000,"max_concurrent_requests":10,
#   "enabled_empty_directories":false},
#  "trashcan":{"retention_days":14}}
```

Audit query parameters (`GET /api/v1/audit`):

| Parameter | Type | Semantics |
|---|---|---|
| `repo` | string | Filter by repository (exact match) |
| `actor` | string | Filter by actor (exact match) |
| `action` | string | Filter by action (exact match; see the audit vocabulary) |
| `since` | RFC3339 | Start time (inclusive) |
| `until` | RFC3339 | End time (exclusive) |
| `limit` | int | Default 100, cap 1000 (over → 400) |
| `cursor` | string | Cursor pagination (opaque; pass `nextCursor` back) |

```json
{
  "events": [
    {
      "id": 42,
      "time": "2026-08-21T12:34:56.789Z",
      "actor": "admin",
      "action": "deploy",
      "repo": "generic-local",
      "path": "a/b/w.bin",
      "detail": null
    }
  ],
  "nextCursor": "43"
}
```

Results are in **reverse chronological order** (newest first); `detail` is an optional context object (e.g. `{used, quota}` for `quota.exceeded`). The action vocabulary and console face: [governance guide](admin/governance.md).

GC trigger body (`POST /api/v1/system/gc`):

| Field | Type | Default | Semantics |
|---|---|---|---|
| `apply` | bool | false | false = dry-run (report only); true = execute |
| `graceHours` | number | configured (default 24) | Grace window; 0 = no grace; negative or >876000 → 400 |

```json
// dry-run
{"candidateCount":5,"candidateBytes":204800,"deletedCount":0}

// apply
{"candidateCount":5,"candidateBytes":204800,"deletedCount":3}
```

### License and add-ons

The license path is **singular**; the plural `/api/system/licenses` is 404 (the HA multi-license semantics are not adopted). Slots, tiers and the community gate behavior: [License and add-ons guide](admin/license.md).

| Method | Path | Semantics |
|---|---|---|
| GET | `/binflow/api/system/license` | License status (admin / readonly_admin; the body never contains the certificate text/signature) |
| POST | `/binflow/api/system/license` | Install (body = the certificate text; success **201**; a failed signature check **400** with wire code `LICENSE_EXPIRED`/`LICENSE_INVALID`, the current certificate untouched) |
| DELETE | `/binflow/api/system/license` | Uninstall (idempotent **200 plain text** `License removed successfully.`; a downgrade does not hold data hostage) |
| GET | `/binflow/api/v1/addons` | Add-on slot list, evaluated live — bare array of `id`/`kind`/`minTier`/`enabled`/`reason`/`displayName`/`description`; **no write surface** (other verbs 404) |

### Cleanup

The unused-cleanup engine reclaims remote cache artifacts with no download activity inside the policy window. The policy source is the remote repository's `unusedArtifactsCleanupPeriodHours` (hours; 0 = off); an hourly cron applies rounds.

| Method | Path | Semantics |
|---|---|---|
| POST | `/binflow/api/v1/system/cleanup` | Manual trigger (admin only). Body `{"apply":bool,"repo":string?}` — **dry-run by default**; runs synchronously and returns a CleanupReport |
| GET | `/binflow/api/v1/system/cleanup` | Status face (system:read) — cron cadence, cumulative counters, the last report, per-remote-repository policy rows |

Engine notes:

- Three legs under one maintenance lock (mutually exclusive with gc/export/import): expired upload-session sweep → policy deletion (remote cache file nodes with no download event inside the window; the in-use oracle is audit download trails plus downloads through virtual repositories that count the member) → GCSweep.
- When `audit.enabled=false` the policy leg refuses to run — without download traces there is no honest "unused", so nothing is deleted; the session/gc legs still run.
- Report fields: `trigger/apply/repos[{repo,periodHours,cutoff,keptByUse,candidates,deleted,bytes}]/gracePending/gcDeleted/sessionsSwept/objectsCleaned/bytesReclaimed/ok`; audited as `cleanup.run`; metrics `binflow_cleanup_objects` / `binflow_cleanup_bytes` (gauges).

### Maintenance and scheduled backups

The cron scheduling domains — expression subset, validation family and the audit vocabulary: [scheduled tasks guide](admin/cron-scheduling.md).

| Method | Path | Semantics |
|---|---|---|
| GET/PUT | `/binflow/api/v1/system/maintenance` | The three maintenance cron slots `gc` / `cleanup-unused-cache` / `cleanup-virtual`: GET projects all three (no row = unscheduled, rendered default shape); PUT writes slot by slot — each slot optional (absent = unchanged), an empty-string `cronExp` = delete the schedule row (the only way to clear), `enabled` defaults to true. All arms are validated before anything is persisted; a bad expression 400 `Invalid cronExp <expr> for <slot>: <reason>`. The manual face stays `POST /api/v1/system/gc` and `/cleanup`. Reads system:read; writes admin only |
| PUT/GET/DELETE | `/binflow/api/v1/system/backups` (+ `/{key}`) | **Scheduled backup CRUD** — the official PUT form carries `backupKey` in the body (when a path key and a body key coexist, the path wins); `exportPath` is required (server-side absolute path, no `..` segments); `nextBackupTime` is the writable first-run moment (a past value 400s); an empty-string `cronExp` is legal = keep the row, do not schedule; DELETE removes the payload row and the schedule row together (**204**). Scheduled backups only — restore stays CLI-only |
| GET | `/binflow/api/v1/system/schedules` | Read-only projection of the three scheduling domains (`maintenance` / `backup` / `replication`); `?domain=` filters over the closed set, unknown values 400 `domain must be one of maintenance, backup, replication (or omitted for every domain)`. Writes exist only on the three configuration faces |

### Multipart uploads

The Artifactory-shaped MPU family for very large files (used by JFrog CLI). **Data endpoints require a pure S3 backend** — filestore/dual-write instances return **501 plain text**, not 404.

| Method | Path | Semantics |
|---|---|---|
| POST | `/binflow/api/v1/uploads/create?repoKey=&repoPath=&partSizeMB=` | Open a session (query parameters, not a JSON body; authentication + the admin/user role + `w` on the target repository; virtual repositories fall back to `defaultDeploymentRepo`; any package type). **200 `{"token": …}`** — the session capability credential. Sessions survive restarts |
| GET | `/binflow/api/v1/uploads/config` | Capability probe — **200 `{"supported": bool}`** (true on the S3 stack / false on filestore; the probe never returns 501); gated by the jfrog-cli-go UA version (below 2.62.2 returns false) |
| POST | `/binflow/api/v1/uploads/urlPart?partNumber=N` | The part-N upload URL (Bearer session token); the URL's query string carries the `?token=` capability, so the PUT may omit Authorization |
| PUT | `/binflow/api/v1/uploads/part/{id}/{n}?token=` | Upload a part (the urlPart target); **200** in S3 PutObject shape; parts may arrive out of order (a bounded reorder buffer holds them); the server relays into S3 multipart |
| POST | `/binflow/api/v1/uploads/status` | Async job progress (Bearer): `{status, error, progress, checksumToken}`; status ∈ PARTS / PROCESSING / **FINISHED** (progress 100 + checksumToken) / NON_RETRYABLE_ERROR |
| POST | `/binflow/api/v1/uploads/complete?sha1=` | Submit for assembly (Bearer; **sha1**, 40 hex, required) → **202 accepted**, the job is asynchronous; a mismatch surfaces through status as NON_RETRYABLE_ERROR |
| POST | `/binflow/api/v1/uploads/abort` | Abort a session (Bearer) → 204 |

Flow: `create` returns a token → `urlPart` + PUT parts (concurrent, out of order is fine) → `complete?sha1=` 202 → poll `status` to **FINISHED** and take the `checksumToken` → use it for a zero-transfer `X-Checksum-Deploy` PUT that lands the node. The client lands the node; the server only assembles and registers the blob.

### Replication

Push replication — configuration CRUD (writes: admin only), Replicate Now, connectivity probes and the global block. Engine behavior and field validation: [governance guide](admin/governance.md).

| Method | Path | Semantics |
|---|---|---|
| GET | `/binflow/api/v1/replications` | Configuration list (bare array, readable by readonly_admin); credential fields are never echoed |
| POST | `/binflow/api/v1/replications` | Create — **201** echoes the configuration row; `enabled` defaults to true; `cronExp` schedules full syncs (empty = event-driven only); duplicate names 409; unknown source repositories 400 naming the key |
| PUT | `/binflow/api/v1/replications/{id}` | Enable/disable / reschedule. `{id}` = the **numeric id** heading the list row (the immutable key; DELETE goes by name — both addressing forms coexist). Body: `enabled` **or** `cron_exp`, at least one (empty body 400 `enabled or cron_exp is required; no other field is editable on this face`); other fields are parsed but ignored. `cron_exp` empty string = clear the schedule (pure event track); `enabled:false` = park (schedule kept, `next_schedule_sync` cleared, event track paused). **200** echoes the updated row (`updated_at` refreshed, sealed credentials kept as-is). After disabling, new artifacts stop enqueueing and in-flight tasks run to their conclusion; on re-enable the backlog drains on the next sweep — no restart needed. Errors: anonymous 401 / non-admin 403 / unknown id 404 `replication config not found: <id>` / non-numeric id 400 |
| DELETE | `/binflow/api/v1/replications/{name}` | Delete by name (the task ledger cascades clean); **204**; deleting again 404 |
| GET | `/binflow/api/v1/replication/status` | Dashboard payload: `targets[]` (per-configuration task counts) + `events[]` (recent tasks merged across configurations; `?limit=` 1..500, default 50) |
| POST | `/binflow/api/v1/replications/{id}/run` | **Replicate Now** — seeds one reconciliation run from the stored configuration; returns upon scheduling, does not wait for replication. 200 `{"info":"The replication tasks was successfully scheduled to run","id","name","scheduled","capped"}` — `scheduled` = tasks seeded this round (an empty source repository = 0), `capped` = truncated by `max_items_per_push` (run again for the next slice). Repeated triggers are not deduplicated (the target side converges idempotently by sha256); a disabled configuration → **409** (run `PUT enabled=true` first); push blocked → **409** (anchored `Push replication is blocked, skipping replication` + unlock pointers); unknown id 404 |
| POST | `/binflow/api/v1/replications/{id}/test` | Probe a **stored** configuration's target connectivity (`GET {target_url}/binflow/api/storage/{target_repo}`, sealed credentials attached); optional body `{target_url/target_repo/target_username/target_password}` overrides field by field (a changed URL/username without a password probes anonymously — the old secret is never sent). Verdict body: pass 200 `{"ok":true,"status_code":200,"message":"Push replication target url '<url>' tested successfully"}`; failure is the **same shape with 400** (`ok:false` + the target status/reason inline). Zero side effects; ignores the block state |
| POST | `/binflow/api/v1/replications/test` | **Draft face without an id** — test a candidate before saving. Body required (`{target_url,target_repo,target_username?,target_password?}`); a target on the same instance → `ok:false` (`Cannot replicate to the same instance: …`); a target ending in `-cache` → `Replication to remote cache repositories are not allowed.` |
| GET | `/binflow/api/v1/system/replications` | Global block state, official key shape `{"blockPullReplications":bool,"blockPushReplications":bool}` |
| POST | `/binflow/api/v1/system/replications/block` / `unblock` | **Emergency brake**: `push`/`pull` query parameters pick directions (omitted = the direction's action; **any string other than `"true"` = no-op for this call**); responses are **text/plain** with the official messages (`Successfully blocked all replications, no replication will be triggered.` / single-direction variants / both no-op `No action taken.`). Idempotent, persisted on write (survives restarts); **does not gate the configuration face** — CRUD, enable/disable and listing work as usual while blocked |

> **`target_url` shape**: the engine pushes to `{target_url}/binflow/{target_repo}/{path}` — set `target_url` to the target instance's **bare origin** (e.g. `http://target.example:8080`), **without** the `/binflow` suffix (including it produces `/binflow/binflow/…`).

The block covers the event track (new artifacts enqueue zero), the claim track (in-flight sends stop, retry counters kept), manual triggers (run 409) and the pull side (`blockPull` on: remote repositories touch no upstream — fresh cache entries still HIT, stale copies downgrade to STALE, misses 404 naming the block; lifting the block restores immediately). The same switches live in `binflow.yaml` (`replication.block_push` / `replication.block_pull`, default false) and the governance console.

```bash
# Replicate Now: 5 artifacts scheduled; the target converges path by path (sha256 idempotent)
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/replications/1/run
# 200 {"info":"The replication tasks was successfully scheduled to run","id":1,"name":"push-b",
#      "scheduled":5,"capped":false}

# Park a configuration — no restart needed to resume
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/v1/replications/1 \
  -H 'Content-Type: application/json' -d '{"enabled":false}'
# 200 {"id":1,"name":"push-prod",…,"enabled":false,…,"updated_at":"<refreshed>"}

# Global block roundtrip
curl -su admin:$ADMIN_PW -X POST "$BASE/binflow/api/v1/system/replications/block?push=true"
# Successfully blocked all replications, no replication will be triggered.
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/replications/1/run
# 409 ... "Push replication is blocked, skipping replication (config push-b; POST /api/v1/system/replications/unblock to resume)"
curl -su admin:$ADMIN_PW -X POST "$BASE/binflow/api/v1/system/replications/unblock?push=true"
# Successfully unblocked all replications.
```

Audit actions: `replication.run` (the run control face), `replication.config.test`, `replication.block.update` — kept separate from the engine execution layer's `replication.push`.

### Webhooks

The subscription family lives at `/binflow/event/api/v1/**` — note it is **not** under `/binflow/api`. The read face is open to readonly_admin; write verbs sit behind the `webhook` pro slot (community instances get 403 + `X-Binflow-License-Required: webhook`). Full semantics: [Webhooks guide](admin/webhooks.md).

| Method | Path | Semantics |
|---|---|---|
| GET | `/binflow/event/api/v1/subscriptions` | Subscription list (bare array) |
| POST | `/binflow/event/api/v1/subscriptions` | Create — **201** echoes a SubscriptionView (`secret` always masked `********`) |
| GET | `/binflow/event/api/v1/subscriptions/{key}` | Get one; a miss is **404 `Subscription not found`** |
| PUT | `/binflow/event/api/v1/subscriptions/{key}` | Full update — **204 no body**; the key cannot be changed |
| DELETE | `/binflow/event/api/v1/subscriptions/{key}` | Delete (delivery rows cascade); **204**; deleting again 404 |
| POST | `/binflow/event/api/v1/subscriptions/test` | **Test-send a draft** (consumes a full subscription body, not a key reference); a synchronous single send that does not enter the queue; 200 TestOutcome (`ok`/`attempt{status_code,elapsed_millis,error}`) — **failures also return 200, inspect the body** |
| GET | `/binflow/event/api/v1/troubleshooting` | Troubleshooting record ring (query: `subscription`/`target`/`start`/`end`/`count`); failures are always recorded, successes too when `debug:true`; an in-process ring of 10,000 entries pruned every 30s — history is lost on restart |

Request body (one shape for create/update/test): `key` (`^[A-Za-z][A-Za-z0-9_-]+$`, ≤500) / `project_key` / `description` / `enabled` (**default false**) / `event_filter{domain,event_types[],criteria}` (strict — unknown keys 400) / `handlers[]` (**exactly one**; type `webhook` or `custom-webhook`) / `debug`.

Delivery: HMAC-SHA256 hex in `X-JFrog-Event-Auth` (with `use_secret_for_signing=false` the secret is sent as plaintext); retries **5 including the first attempt / fixed 10s spacing / 30s per-attempt timeout / only on send failure or ≥500** (4xx is a one-step terminal state); dead letters are audited as `webhook.dead_letter` plus the `binflow_webhook_*` metric family. SSRF protection: targets on loopback/private networks are rejected by default; `webhook.allow_private_target` (default false, effective on restart) permits them.

### Reindex

Index rebuild endpoints for the signed/indexed package types — see each client guide ([Conan](integrations/conan.md), [Helm](integrations/helm-charts.md), [RPM](integrations/rpm.md), [Debian](integrations/debian.md)).

| Endpoint | Semantics |
|---|---|
| `POST /binflow/api/conan/reindex[?repoKey=]` · `POST /binflow/api/conan/{repoKey}/reindex` | conan recipe index rebuild (local repositories only; synchronous) |
| `POST /binflow/api/helm/{repoKey}/reindex` · `…/reindex/{path}` | helm `index.yaml` recomputation (whole repository async / partial synchronous) |
| `POST /binflow/api/deb/reindex/{repoKey}?async=0\|1` | debian index recomputation (virtual/remote classes 400) |
| `POST /binflow/api/yum/{repoKey}?path=&async=0\|1` | rpm repodata recomputation; on virtual repositories 200/202 triggers an aggregate re-merge (`path` gets `/repodata` appended automatically); a synchronous request against an auto-async repository gets 409 |

### Console session

| Method | Path | Semantics |
|---|---|---|
| POST | `/binflow/api/v1/session` | Log in (JSON or form; no authentication) — the response sets the session cookie and echoes `adminRole` and `source` |
| GET | `/binflow/api/v1/session` | Whoami (current session info) |
| DELETE | `/binflow/api/v1/session` | Log out (the session is revoked server-side) |

---

## Intentionally unrouted paths

These paths are intentionally unrouted (404) — the supported alternatives are named:

| Path | Status | Use instead |
|---|---|---|
| `/binflow/api/v2/**` | 404 | The Artifactory v2 permissions API is not implemented — use `/api/v1/permissions`. The one exception: the repository key-pair association face `/api/v2/repositories/{key}/keyPairs` |
| `/binflow/api/export/**`, `/binflow/api/import/**` | 404 | Backup/restore is CLI-only |
| `/binflow/api/system/storage/prune/**` | 404 | Space reclamation goes through GC |
| `/binflow/v2/**` | 404 | Docker endpoints do not live under the `/binflow` prefix — use the root-level `/v2` plane |
| `/binflow/api/system/licenses` (plural) | 404 | The HA multi-license semantics are not adopted — use the singular `/api/system/license` |
| `/binflow/api/search/props\|users\|artifactory\|badge` | 404 | `prop` is the official singular spelling; the plural `props` is 404 |
| `/binflow/api/flat/copy\|move` | 404 | Use `/api/copy` / `/api/move` |

---

## Next steps

- Client integration guides: [Docker](docker-registry.md) · [Maven](integrations/maven.md) · [npm](integrations/npm.md) · [PyPI](integrations/pypi.md) · [Go](integrations/golang.md) · [NuGet](integrations/nuget.md) · [Cargo](integrations/cargo.md) · [Conan](integrations/conan.md) · [Helm](integrations/helm-charts.md) · [RPM](integrations/rpm.md) · [Debian](integrations/debian.md)
- Search: [AQL search guide](aql.md) (language subset / error family / Artifactory migration mapping) · [properties](properties.md)
- Administration: [governance](admin/governance.md) · [groups and permissions](admin/groups-permissions.md) · [RBAC roles and repository-level admins](admin/rbac-roles.md) · [token step-up](admin/token-step-up.md) · [backup and restore](admin/backup-restore.md) · [license and add-ons](admin/license.md) · [authentication configuration](admin/auth-config.md) · [storage configuration](admin/storage-config.md) · [webhooks](admin/webhooks.md)
- FAQ and troubleshooting: [FAQ](faq.md)
