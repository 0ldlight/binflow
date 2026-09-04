# Domain: 协议接入面——docker registry（根级 /v2 平面）+ npm + PyPI。
# Source: docs/user/api-reference.md「DE: Docker 域」「NE: npm 域」「PE: PyPI 域」；
# 路由核对 internal/httpapi/router.go（/v2 根级例外 + api 协议挂载重写）+
# internal/adapter/npm/route.go（NE 表的 `-/` 前缀拼写系文档漂移——实测文法无该段，
# 登记于工作日志；spec 采路由验证文法并在描述标注）。

from helpers import (op, tag, q, pp, r, S, arr, body, obj,
                     ERR_401, ERR_403, ANON)

tag("docker", "Docker Registry（根级 /v2 平面——独立路由，不走 /binflow 前缀；"
              "401 挑战为 Bearer realm=/v2/token，随实例匿名开关不变）")
tag("npm", "npm 域（/api/npm/{repoKey}——packument/发布/登录/探活）")
tag("pypi", "PyPI 域（/api/pypi/{repoKey}——PEP 503/629 simple 索引与 twine 上传）")

V2 = [{"url": "/", "description": "docker registry 根级平面（ADR-0010 根级例外——不在 /binflow 前缀下）"}]

V2_ERR_401 = r("401 挑战（registry spec body + Bearer challenge）",
               schema=obj({"errors": arr(obj({"code": {"type": "string"},
                                              "message": {"type": "string"},
                                              "detail": {"type": ["object", "null"]}}))}),
               example={"errors": [{"code": "UNAUTHORIZED", "message": "authentication required", "detail": None}]})

V2_ERR_403 = r("权限不足",
               schema=obj({"errors": arr(obj({"code": {"type": "string"},
                                              "message": {"type": "string"},
                                              "detail": {"type": ["object", "null"]}}))}),
               example={"errors": [{"code": "DENIED", "message": "requested access to the resource is denied", "detail": None}]})


def build():
    # ---- docker /v2 ----
    op("/v2/", "get", "dockerBase", "docker", "API 版本检查",
       "官方拼写：`GET /v2/`（401 挑战，不随匿名开关变化）。",
       responses={"200": r("OK（已认证）", schema=obj({}, additional=True)),
                  "401": V2_ERR_401},
       security=ANON, servers=V2)

    op("/v2/_catalog", "get", "dockerCatalog", "docker", "仓库目录",
       "官方拼写：`GET /v2/_catalog`。",
       params=[q("n", "分页条数", schema={"type": "integer"})],
       responses={"200": r("仓库目录", schema=obj({"repositories": arr({"type": "string"})})),
                  "401": V2_ERR_401},
       servers=V2)

    op("/v2/{name}/tags/list", "get", "dockerTagsList", "docker", "标签列表",
       "官方拼写：`GET /v2/{name}/tags/list`（空标签集返回 `\"tags\":null`）。",
       params=[pp("name", "镜像名（可含斜杠）")],
       responses={"200": r("标签列表", schema=obj({"name": {"type": "string"},
                                                   "tags": arr({"type": "string"}, desc="空集为 null")}))},
       servers=V2)

    op("/v2/{name}/manifests/{ref}", "get", "dockerManifestGet", "docker", "取 manifest",
       "官方拼写：`GET /v2/{name}/manifests/{ref}`（tag 或 digest）。",
       params=[pp("name", "镜像名"), pp("ref", "tag 或 digest")],
       responses={"200": r("manifest", schema=obj({}, additional=True))},
       servers=V2)

    op("/v2/{name}/manifests/{ref}", "put", "dockerManifestPut", "docker", "上传 manifest",
       "官方拼写：`PUT /v2/{name}/manifests/{ref}`（Content-Type 透传不白名单）。",
       params=[pp("name", "镜像名"), pp("ref", "tag")],
       req_body=body("manifest", schema=obj({}, additional=True)),
       responses={"201": r("已上传")},
       servers=V2)

    op("/v2/{name}/manifests/{ref}", "delete", "dockerManifestDelete", "docker",
       "删除 manifest",
       "官方拼写：`DELETE /v2/{name}/manifests/{digest}`（by-digest 仅——本路径模板与 GET/PUT 共用 `{ref}`，"
       "DELETE 臂的 ref 只接受 digest 形）。",
       params=[pp("name", "镜像名"), pp("ref", "digest（sha256:...）——DELETE 仅认 digest")],
       responses={"202": r("已接受删除")},
       servers=V2)

    op("/v2/{name}/blobs/uploads/", "post", "dockerBlobUploadStart", "docker",
       "启动 blob 上传会话",
       "官方拼写：`POST /v2/{name}/blobs/uploads/`。",
       params=[pp("name", "镜像名")],
       responses={"202": r("会话已建（Location 头为续传地址）")},
       servers=V2)

    op("/v2/{name}/blobs/uploads/{uuid}", "get", "dockerBlobUploadStatus", "docker",
       "上传状态查询",
       "官方拼写：`GET /v2/{name}/blobs/uploads/{uuid}`。**204 + `Range: 0-<offset-1>`** 权威断点"
       "（跨重启存活，续传见 Docker 接入指南）。",
       params=[pp("name", "镜像名"), pp("uuid", "会话 id")],
       responses={"204": r("权威断点（Range 头）")},
       servers=V2)

    op("/v2/{name}/blobs/uploads/{uuid}", "patch", "dockerBlobUploadPatch", "docker",
       "上传 blob 分片",
       "官方拼写：`PATCH /v2/{name}/blobs/uploads/{uuid}`。"
       "`Content-Range` 起点错位 → 416 空 body + 权威 `Range`。",
       params=[pp("name", "镜像名"), pp("uuid", "会话 id")],
       req_body=body("分片字节", schema={"type": "string", "format": "binary"}),
       responses={"202": r("已接收"),
                  "416": r("Content-Range 起点错位（空 body + 权威 Range 头）")},
       servers=V2)

    op("/v2/{name}/blobs/uploads/{uuid}", "put", "dockerBlobUploadComplete", "docker",
       "完成 blob 上传",
       "官方拼写：`PUT /v2/{name}/blobs/uploads/{uuid}?digest=sha256:...`。",
       params=[pp("name", "镜像名"), pp("uuid", "会话 id"),
               q("digest", "sha256:...", required=True)],
       responses={"201": r("已落库")},
       servers=V2)

    op("/v2/{name}/blobs/{digest}", "get", "dockerBlobGet", "docker", "下载 blob",
       "官方拼写：`GET /v2/{name}/blobs/{digest}`。",
       params=[pp("name", "镜像名"), pp("digest", "sha256:...")],
       responses={"200": r("blob 流")},
       servers=V2)

    op("/v2/{name}/blobs/{digest}", "head", "dockerBlobHead", "docker", "blob 存在检测",
       "官方拼写：`HEAD /v2/{name}/blobs/{digest}`。",
       params=[pp("name", "镜像名"), pp("digest", "sha256:...")],
       responses={"200": r("存在（无 body）")},
       servers=V2)

    op("/v2/{name}/blobs/{digest}", "delete", "dockerBlobDelete", "docker",
       "删除 blob（不做）",
       "官方拼写：`DELETE /v2/{name}/blobs/{digest}` → **405 UNSUPPORTED**——blob 删除仅 GC。",
       params=[pp("name", "镜像名"), pp("digest", "sha256:...")],
       responses={"405": r("UNSUPPORTED——blob 删除仅 GC",
                    schema=obj({"errors": arr(obj({"code": {"type": "string"},
                                                   "message": {"type": "string"},
                                                   "detail": {"type": ["object", "null"]}}))}),
                    example={"errors": [{"code": "UNSUPPORTED",
                                         "message": "blob deletion is not supported; blobs are reclaimed by GC only",
                                         "detail": None}]})},
       servers=V2)

    op("/v2/token", "get", "dockerTokenGet", "docker", "docker 认证 token 端点（GET 形）",
       "官方拼写：`GET /v2/token`（distribution token 协议；GET/POST 两形同服务）。"
       "管理面吊销对 docker token 即时生效。",
       params=[q("service", None), q("scope", None),
               q("account", "用户名（Basic 凭据对）"), q("client_id", None),
               q("offline_token", None, schema={"type": "boolean"})],
       responses={"200": r("distribution token", schema=obj({"token": {"type": "string"},
                                                             "access_token": {"type": "string"},
                                                             "expires_in": {"type": "integer"},
                                                             "issued_at": {"type": "string"}})),
                  "401": V2_ERR_401},
       servers=V2)
    op("/v2/token", "post", "dockerTokenPost", "docker", "docker 认证 token 端点（POST 形）",
       "官方拼写：`POST /v2/token`（GET/POST 两形同服务——distribution token 协议）。",
       responses={"200": r("distribution token", schema=obj({"token": {"type": "string"},
                                                             "access_token": {"type": "string"},
                                                             "expires_in": {"type": "integer"},
                                                             "issued_at": {"type": "string"}})),
                  "401": V2_ERR_401},
       servers=V2)

    op("/v2/{name}/referrers/", "get", "dockerReferrers", "docker",
       "OCI referrers API（不做）",
       "官方拼写：`GET /v2/{name}/referrers/` → **404**——OCI referrers API 不做。",
       params=[pp("name", "镜像名")],
       responses={"404": r("有意不做（404）")},
       servers=V2)

    # ---- npm ----
    op("/api/npm/{repoKey}/{pkg}", "get", "npmPackumentGet", "npm", "packument（包元数据）",
       "路由核对文法：`GET /binflow/api/npm/{repoKey}/{pkg}`（api 挂载重写到内容面；"
       "pkg 为 `pkg` 或 `@scope%2Fpkg` 两拼写等价）。契约页 NE 表的 `-/` 前缀拼写与适配器文法不符——"
       "已登记漂移，本 spec 采路由验证文法。",
       params=[pp("repoKey", "npm 仓 key"), pp("pkg", "包名（scope 形可 %2f 转义）")],
       responses={"200": r("packument", schema=obj({}, additional=True)),
                  "404": r("包不存在")},
       security=ANON)

    op("/api/npm/{repoKey}/{pkg}", "put", "npmPublish", "npm", "发布包",
       "路由核对文法：`PUT /binflow/api/npm/{repoKey}/{pkg}`（十步 packument 链）。",
       params=[pp("repoKey", "npm 仓 key"), pp("pkg", "包名")],
       req_body=body("packument（含 _attachments tarball base64）", schema=obj({}, additional=True)),
       responses={"201": r("已发布"), "409": r("版本冲突")})

    op("/api/npm/{repoKey}/{pkg}/-rev/{rev}", "delete", "npmUnpublish", "npm",
       "unpublish（整包）",
       "路由核对文法：`DELETE /binflow/api/npm/{repoKey}/{pkg}/-rev/{rev}`（rev 为占位、opaque）。"
       "契约页的 `?rev=` 查询参数形态与适配器文法不符——已登记漂移。",
       params=[pp("repoKey", "npm 仓 key"), pp("pkg", "包名"), pp("rev", "占位 rev")],
       responses={"200": r("已移除"), "404": r("包不存在")})

    op("/api/npm/{repoKey}/{pkg}/-/{filename}/-rev/{rev}", "delete", "npmUnpublishVersion",
       "npm", "unpublish（移除指定版本）",
       "路由核对文法：`DELETE /binflow/api/npm/{repoKey}/{pkg}/-/{file}.tgz/-rev/{rev}`"
       "（scoped tarball 文件名自带 scope 段）。",
       params=[pp("repoKey", "npm 仓 key"), pp("pkg", "包名"),
               pp("filename", "tarball 文件名（<name>-<version>.tgz 形）"),
               pp("rev", "占位 rev")],
       responses={"200": r("已移除"), "404": r("不存在")})

    op("/api/npm/{repoKey}/-/ping", "get", "npmPing", "npm", "连通性探测",
       "官方拼写：`GET /binflow/api/npm/{repoKey}/-/ping`（免认证，`200 {}`）。",
       params=[pp("repoKey", "npm 仓 key")],
       responses={"200": r("空 JSON 对象", schema=obj({}, additional=False))},
       security=ANON)

    op("/api/npm/{repoKey}/-/whoami", "get", "npmWhoami", "npm", "当前用户",
       "官方拼写：`GET /binflow/api/npm/{repoKey}/-/whoami`（需认证；无该仓读权限的账号 403——读面 ACL 语义）。",
       params=[pp("repoKey", "npm 仓 key")],
       responses={"200": r("当前用户", schema=obj({"username": {"type": "string"}})),
                  "403": ERR_403})

    op("/api/npm/{repoKey}/-/user/org.couchdb.user:{name}", "put", "npmLogin", "npm",
       "npm legacy login（couch 用户文档族）",
       "官方拼写：`PUT /binflow/api/npm/{repoKey}/-/user/org.couchdb.user:{name}`。"
       "npm `login --auth-type=legacy` 的落点：凭据在 body（`name`/`password`），该路径族对 npm 仓"
       "**豁免写认证门**，由登录端点验证 body 凭据后铸 token（**201 幂等再铸**，不报 409）；"
       "错口令 401 + Basic challenge。仅 `packageType=npm` 仓适用（generic 仓同路径仍 401——类型钉死防匿名写入）。",
       params=[pp("repoKey", "npm 仓 key"), pp("name", "用户名（couch id 段）")],
       req_body=body("couch 用户文档（name/password）",
                     schema=obj({"name": {"type": "string"}, "password": {"type": "string"}},
                                required=["name", "password"])),
       responses={"201": r("已铸 token", schema=obj({"ok": {"type": "string"},
                                                     "token": {"type": "string"},
                                                     "rev": {"type": "string"}})),
                  "401": ERR_401},
       security=ANON)

    op("/api/npm/{repoKey}/-/user/org.couchdb.user:{name}/-rev/{rev}", "put", "npmLoginRetry",
       "npm", "npm legacy login（E409 重试拼写）",
       "官方拼写：`PUT /binflow/api/npm/{repoKey}/-/user/org.couchdb.user:{name}/-rev/{rev}`"
       "（npm 带 revision 重发），同臂服务。",
       params=[pp("repoKey", "npm 仓 key"), pp("name", "用户名"), pp("rev", "占位 rev")],
       req_body=body("couch 用户文档（name/password）",
                     schema=obj({"name": {"type": "string"}, "password": {"type": "string"}})),
       responses={"201": r("已铸 token")},
       security=ANON)

    op("/api/npm/{repoKey}/-/v1/login", "post", "npmWebLogin", "npm",
       "web 登录端点（不提供）",
       "官方拼写：`POST /binflow/api/npm/{repoKey}/-/v1/login` → **401**。"
       "npm 客户端（npm ≥ 9 默认 web 形态）收到 401 后自动回落 couch 链，行为可用。",
       params=[pp("repoKey", "npm 仓 key")],
       responses={"401": ERR_401},
       security=ANON)

    # ---- pypi ----
    op("/api/pypi/{repoKey}/simple/", "get", "pypiSimpleIndex", "pypi",
       "包列表（PEP 503/629）",
       "官方拼写：`GET /binflow/api/pypi/{repoKey}/simple/`。",
       params=[pp("repoKey", "pypi 仓 key")],
       responses={"200": r("simple 索引（HTML）", schema={"type": "string"}, ctype="text/html")},
       security=ANON)

    op("/api/pypi/{repoKey}/simple/{pkg}/", "get", "pypiSimplePkg", "pypi",
       "单包索引页（HTML + JSON，Accept 驱动）",
       "官方拼写：`GET /binflow/api/pypi/{repoKey}/simple/{pkg}/`。",
       params=[pp("repoKey", "pypi 仓 key"), pp("pkg", "包名（规范化后）")],
       responses={"200": r("单包索引（text/html 或 application/vnd.pypi.simple.v1+json）")},
       security=ANON)

    op("/api/pypi/{repoKey}/", "post", "pypiUpload", "pypi", "上传（twine）",
       "官方拼写：`POST /binflow/api/pypi/{repoKey}/`（multipart `:action=file_upload`）。",
       params=[pp("repoKey", "pypi 仓 key")],
       req_body=body("multipart 表单（:action=file_upload + 文件件）",
                     schema={"type": "object", "properties": {
                         ":action": {"type": "string", "const": "file_upload"},
                         "content": {"type": "string", "format": "binary"}}},
                     ctype="multipart/form-data", required=True),
       responses={"200": r("已上传"), "400": r("表单/文件非法")})

    op("/api/pypi/{repoKey}/packages/{name}/{version}/{filename}", "get", "pypiDownload",
       "pypi", "下载构件（twine 回显 URL）",
       "官方拼写：`GET /binflow/api/pypi/{repoKey}/packages/{name}/{ver}/{file}`。",
       params=[pp("repoKey", "pypi 仓 key"), pp("name", "项目名"),
               pp("version", "版本"), pp("filename", "文件名")],
       responses={"200": r("构件流")},
       security=ANON)
