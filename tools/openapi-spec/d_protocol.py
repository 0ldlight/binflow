# Domain: 协议接入面——docker registry（根级 /v2 平面）+ npm + PyPI。
# Source: docs/user/api-reference.md「DE: Docker 域」「NE: npm 域」「PE: PyPI 域」；
# 路由核对 internal/httpapi/router.go（/v2 根级例外 + api 协议挂载重写）+
# internal/adapter/npm/route.go（NE 表的 `-/` 前缀拼写系文档漂移——实测文法无该段，
# 登记于工作日志；spec 采路由验证文法并在描述标注）。

from helpers import (op, tag, q, pp, r, S, arr, body, obj,
                     ERR_401, ERR_403, ANON)

tag("docker", "Docker Registry (root-level /v2 plane — separately routed, not under the /binflow prefix; "
              "the 401 challenge is Bearer realm=/v2/token, unchanged by the instance's anonymous switch)")
tag("npm", "npm domain (/api/npm/{repoKey} — packument/publish/login/ping)")
tag("pypi", "PyPI domain (/api/pypi/{repoKey} — PEP 503/629 simple index and twine uploads)")

V2 = [{"url": "/", "description": "Docker registry root-level plane (not under the /binflow prefix)"}]

V2_ERR_401 = r("401 challenge (registry spec body + Bearer challenge)",
               schema=obj({"errors": arr(obj({"code": {"type": "string"},
                                              "message": {"type": "string"},
                                              "detail": {"type": ["object", "null"]}}))}),
               example={"errors": [{"code": "UNAUTHORIZED", "message": "authentication required", "detail": None}]})

V2_ERR_403 = r("Access denied",
               schema=obj({"errors": arr(obj({"code": {"type": "string"},
                                              "message": {"type": "string"},
                                              "detail": {"type": ["object", "null"]}}))}),
               example={"errors": [{"code": "DENIED", "message": "requested access to the resource is denied", "detail": None}]})


def build():
    # ---- docker /v2 ----
    op("/v2/", "get", "dockerBase", "docker", "API version check",
       "Canonical path: `GET /v2/` (401 challenge, does not change with the anonymous switch).",
       responses={"200": r("OK (authenticated)", schema=obj({}, additional=True)),
                  "401": V2_ERR_401},
       security=ANON, servers=V2)

    op("/v2/_catalog", "get", "dockerCatalog", "docker", "Repository catalog",
       "Canonical path: `GET /v2/_catalog`.",
       params=[q("n", "Page size", schema={"type": "integer"})],
       responses={"200": r("Repository catalog", schema=obj({"repositories": arr({"type": "string"})})),
                  "401": V2_ERR_401},
       servers=V2)

    op("/v2/{name}/tags/list", "get", "dockerTagsList", "docker", "Tag list",
       "Canonical path: `GET /v2/{name}/tags/list` (an empty tag set returns `\"tags\":null`).",
       params=[pp("name", "Image name (may contain slashes)")],
       responses={"200": r("Tag list", schema=obj({"name": {"type": "string"},
                                                   "tags": arr({"type": "string"}, desc="null when empty")}))},
       servers=V2)

    op("/v2/{name}/manifests/{ref}", "get", "dockerManifestGet", "docker", "Get a manifest",
       "Canonical path: `GET /v2/{name}/manifests/{ref}` (tag or digest).",
       params=[pp("name", "Image name"), pp("ref", "Tag or digest")],
       responses={"200": r("manifest", schema=obj({}, additional=True))},
       servers=V2)

    op("/v2/{name}/manifests/{ref}", "put", "dockerManifestPut", "docker", "Upload a manifest",
       "Canonical path: `PUT /v2/{name}/manifests/{ref}` (Content-Type is passed through, not whitelisted).",
       params=[pp("name", "Image name"), pp("ref", "Tag")],
       req_body=body("manifest", schema=obj({}, additional=True)),
       responses={"201": r("Uploaded")},
       servers=V2)

    op("/v2/{name}/manifests/{ref}", "delete", "dockerManifestDelete", "docker",
       "Delete a manifest",
       "Canonical path: `DELETE /v2/{name}/manifests/{digest}` (by-digest only — this path template shares `{ref}` "
       "with GET/PUT; the DELETE arm's ref accepts digest form only).",
       params=[pp("name", "Image name"), pp("ref", "Digest (sha256:...) — DELETE accepts digests only")],
       responses={"202": r("Deletion accepted")},
       servers=V2)

    op("/v2/{name}/blobs/uploads/", "post", "dockerBlobUploadStart", "docker",
       "Start a blob upload session",
       "Canonical path: `POST /v2/{name}/blobs/uploads/`.",
       params=[pp("name", "Image name")],
       responses={"202": r("Session created (the Location header is the resume URL)")},
       servers=V2)

    op("/v2/{name}/blobs/uploads/{uuid}", "get", "dockerBlobUploadStatus", "docker",
       "Upload status query",
       "Canonical path: `GET /v2/{name}/blobs/uploads/{uuid}`. **204 + `Range: 0-<offset-1>`** is the authoritative "
       "offset (survives restarts; see the Docker guide for resuming).",
       params=[pp("name", "Image name"), pp("uuid", "Session id")],
       responses={"204": r("Authoritative offset (Range header)")},
       servers=V2)

    op("/v2/{name}/blobs/uploads/{uuid}", "patch", "dockerBlobUploadPatch", "docker",
       "Upload a blob chunk",
       "Canonical path: `PATCH /v2/{name}/blobs/uploads/{uuid}`. "
       "A mismatched `Content-Range` start → 416 with an empty body + the authoritative `Range`.",
       params=[pp("name", "Image name"), pp("uuid", "Session id")],
       req_body=body("Chunk bytes", schema={"type": "string", "format": "binary"}),
       responses={"202": r("Received"),
                  "416": r("Content-Range start mismatch (empty body + authoritative Range header)")},
       servers=V2)

    op("/v2/{name}/blobs/uploads/{uuid}", "put", "dockerBlobUploadComplete", "docker",
       "Complete a blob upload",
       "Canonical path: `PUT /v2/{name}/blobs/uploads/{uuid}?digest=sha256:...`.",
       params=[pp("name", "Image name"), pp("uuid", "Session id"),
               q("digest", "sha256:...", required=True)],
       responses={"201": r("Stored")},
       servers=V2)

    op("/v2/{name}/blobs/{digest}", "get", "dockerBlobGet", "docker", "Download a blob",
       "Canonical path: `GET /v2/{name}/blobs/{digest}`.",
       params=[pp("name", "Image name"), pp("digest", "sha256:...")],
       responses={"200": r("Blob stream")},
       servers=V2)

    op("/v2/{name}/blobs/{digest}", "head", "dockerBlobHead", "docker", "Blob existence check",
       "Canonical path: `HEAD /v2/{name}/blobs/{digest}`.",
       params=[pp("name", "Image name"), pp("digest", "sha256:...")],
       responses={"200": r("Exists (no body)")},
       servers=V2)

    op("/v2/{name}/blobs/{digest}", "delete", "dockerBlobDelete", "docker",
       "Delete a blob (not supported)",
       "Canonical path: `DELETE /v2/{name}/blobs/{digest}` → **405 UNSUPPORTED** — blob deletion goes through GC only.",
       params=[pp("name", "Image name"), pp("digest", "sha256:...")],
       responses={"405": r("UNSUPPORTED — blob deletion goes through GC only",
                    schema=obj({"errors": arr(obj({"code": {"type": "string"},
                                                   "message": {"type": "string"},
                                                   "detail": {"type": ["object", "null"]}}))}),
                    example={"errors": [{"code": "UNSUPPORTED",
                                         "message": "blob deletion is not supported; blobs are reclaimed by GC only",
                                         "detail": None}]})},
       servers=V2)

    op("/v2/token", "get", "dockerTokenGet", "docker", "Docker auth token endpoint (GET form)",
       "Canonical path: `GET /v2/token` (distribution token protocol; the GET/POST forms hit the same service). "
       "Revocations on the management API take effect on docker tokens immediately.",
       params=[q("service", None), q("scope", None),
               q("account", "Username (Basic credential pair)"), q("client_id", None),
               q("offline_token", None, schema={"type": "boolean"})],
       responses={"200": r("distribution token", schema=obj({"token": {"type": "string"},
                                                             "access_token": {"type": "string"},
                                                             "expires_in": {"type": "integer"},
                                                             "issued_at": {"type": "string"}})),
                  "401": V2_ERR_401},
       security=ANON, servers=V2)
    op("/v2/token", "post", "dockerTokenPost", "docker", "Docker auth token endpoint (POST form)",
       "Canonical path: `POST /v2/token` (GET/POST forms hit the same service — distribution token protocol).",
       responses={"200": r("distribution token", schema=obj({"token": {"type": "string"},
                                                             "access_token": {"type": "string"},
                                                             "expires_in": {"type": "integer"},
                                                             "issued_at": {"type": "string"}})),
                  "401": V2_ERR_401},
       servers=V2)

    op("/v2/{name}/referrers/", "get", "dockerReferrers", "docker",
       "OCI referrers API (not supported)",
       "Canonical path: `GET /v2/{name}/referrers/` → **404** — the OCI referrers API is not supported.",
       params=[pp("name", "Image name")],
       responses={"404": r("Intentionally not supported (404)")},
       servers=V2)

    # ---- npm ----
    op("/api/npm/{repoKey}/{pkg}", "get", "npmPackumentGet", "npm", "Packument (package metadata)",
       "Canonical path: `GET /binflow/api/npm/{repoKey}/{pkg}` (the api mount rewrites onto the content face; "
       "the two spellings `pkg` and `@scope%2Fpkg` are equivalent).",
       params=[pp("repoKey", "npm repository key"), pp("pkg", "Package name (the scoped form may be %2f-escaped)")],
       responses={"200": r("packument", schema=obj({}, additional=True)),
                  "404": r("Package not found")},
       security=ANON)

    op("/api/npm/{repoKey}/{pkg}", "put", "npmPublish", "npm", "Publish a package",
       "Canonical path: `PUT /binflow/api/npm/{repoKey}/{pkg}` (the ten-step packument chain).",
       params=[pp("repoKey", "npm repository key"), pp("pkg", "Package name")],
       req_body=body("Packument (with _attachments tarball base64)", schema=obj({}, additional=True)),
       responses={"201": r("Published"), "409": r("Version conflict")})

    op("/api/npm/{repoKey}/{pkg}/-rev/{rev}", "delete", "npmUnpublish", "npm",
       "Unpublish (whole package)",
       "Canonical path: `DELETE /binflow/api/npm/{repoKey}/{pkg}/-rev/{rev}` (rev is a placeholder, opaque).",
       params=[pp("repoKey", "npm repository key"), pp("pkg", "Package name"), pp("rev", "Placeholder rev")],
       responses={"200": r("Removed"), "404": r("Package not found")})

    op("/api/npm/{repoKey}/{pkg}/-/{filename}/-rev/{rev}", "delete", "npmUnpublishVersion",
       "npm", "Unpublish (remove a specific version)",
       "Canonical path: `DELETE /binflow/api/npm/{repoKey}/{pkg}/-/{file}.tgz/-rev/{rev}` "
       "(scoped tarball file names carry the scope segment).",
       params=[pp("repoKey", "npm repository key"), pp("pkg", "Package name"),
               pp("filename", "Tarball file name (<name>-<version>.tgz form)"),
               pp("rev", "Placeholder rev")],
       responses={"200": r("Removed"), "404": r("Not found")})

    op("/api/npm/{repoKey}/-/ping", "get", "npmPing", "npm", "Connectivity ping",
       "Canonical path: `GET /binflow/api/npm/{repoKey}/-/ping` (no authentication, `200 {}`).",
       params=[pp("repoKey", "npm repository key")],
       responses={"200": r("Empty JSON object", schema=obj({}, additional=False))},
       security=ANON)

    op("/api/npm/{repoKey}/-/whoami", "get", "npmWhoami", "npm", "Current user",
       "Canonical path: `GET /binflow/api/npm/{repoKey}/-/whoami` (requires authentication; accounts without read "
       "permission on the repository get 403 — read-face ACL semantics).",
       params=[pp("repoKey", "npm repository key")],
       responses={"200": r("Current user", schema=obj({"username": {"type": "string"}})),
                  "403": ERR_403})

    op("/api/npm/{repoKey}/-/user/org.couchdb.user:{name}", "put", "npmLogin", "npm",
       "npm legacy login (couch user document family)",
       "Canonical path: `PUT /binflow/api/npm/{repoKey}/-/user/org.couchdb.user:{name}`. "
       "This is where npm `login --auth-type=legacy` lands: credentials ride in the body (`name`/`password`); "
       "this path family is **exempt from the write-authentication gate** on npm repositories — the login endpoint "
       "validates the body credentials and mints a token (**201 re-mints idempotently**, no 409); "
       "a wrong password gets 401 + Basic challenge. Applies to `packageType=npm` repositories only "
       "(the same path on a generic repository still 401s — the type is pinned to prevent anonymous writes).",
       params=[pp("repoKey", "npm repository key"), pp("name", "Username (the couch id segment)")],
       req_body=body("Couch user document (name/password)",
                     schema=obj({"name": {"type": "string"}, "password": {"type": "string"}},
                                required=["name", "password"])),
       responses={"201": r("Token minted", schema=obj({"ok": {"type": "string"},
                                                       "token": {"type": "string"},
                                                       "rev": {"type": "string"}})),
                  "401": ERR_401},
       security=ANON)

    op("/api/npm/{repoKey}/-/user/org.couchdb.user:{name}/-rev/{rev}", "put", "npmLoginRetry",
       "npm", "npm legacy login (E409 retry spelling)",
       "Canonical path: `PUT /binflow/api/npm/{repoKey}/-/user/org.couchdb.user:{name}/-rev/{rev}` "
       "(npm resends with a revision attached); served by the same arm.",
       params=[pp("repoKey", "npm repository key"), pp("name", "Username"), pp("rev", "Placeholder rev")],
       req_body=body("Couch user document (name/password)",
                     schema=obj({"name": {"type": "string"}, "password": {"type": "string"}})),
       responses={"201": r("Token minted")},
       security=ANON)

    op("/api/npm/{repoKey}/-/v1/login", "post", "npmWebLogin", "npm",
       "Web login endpoint (not provided)",
       "Canonical path: `POST /binflow/api/npm/{repoKey}/-/v1/login` → **401**. "
       "The npm client (npm ≥ 9 defaults to the web flow) automatically falls back to the couch chain on 401, "
       "so the flow still works.",
       params=[pp("repoKey", "npm repository key")],
       responses={"401": ERR_401},
       security=ANON)

    # ---- pypi ----
    op("/api/pypi/{repoKey}/simple/", "get", "pypiSimpleIndex", "pypi",
       "Package list (PEP 503/629)",
       "Canonical path: `GET /binflow/api/pypi/{repoKey}/simple/`.",
       params=[pp("repoKey", "pypi repository key")],
       responses={"200": r("Simple index (HTML)", schema={"type": "string"}, ctype="text/html")},
       security=ANON)

    op("/api/pypi/{repoKey}/simple/{pkg}/", "get", "pypiSimplePkg", "pypi",
       "Per-package index page (HTML + JSON, Accept-driven)",
       "Canonical path: `GET /binflow/api/pypi/{repoKey}/simple/{pkg}/`.",
       params=[pp("repoKey", "pypi repository key"), pp("pkg", "Package name (normalized)")],
       responses={"200": r("Per-package index (text/html or application/vnd.pypi.simple.v1+json)")},
       security=ANON)

    op("/api/pypi/{repoKey}/", "post", "pypiUpload", "pypi", "Upload (twine)",
       "Canonical path: `POST /binflow/api/pypi/{repoKey}/` (multipart `:action=file_upload`).",
       params=[pp("repoKey", "pypi repository key")],
       req_body=body("Multipart form (:action=file_upload + the file)",
                     schema={"type": "object", "properties": {
                         ":action": {"type": "string", "const": "file_upload"},
                         "content": {"type": "string", "format": "binary"}}},
                     ctype="multipart/form-data", required=True),
       responses={"200": r("Uploaded"), "400": r("Invalid form/file")})

    op("/api/pypi/{repoKey}/packages/{name}/{version}/{filename}", "get", "pypiDownload",
       "pypi", "Download a distribution (the URL twine echoes back)",
       "Canonical path: `GET /binflow/api/pypi/{repoKey}/packages/{name}/{ver}/{file}`.",
       params=[pp("repoKey", "pypi repository key"), pp("name", "Project name"),
               pp("version", "Version"), pp("filename", "File name")],
       responses={"200": r("Distribution stream")},
       security=ANON)
