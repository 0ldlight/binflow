# Domain: 通用制品域（内容面）+ storage 面 + 制品操作/trash。
# Source: docs/user/api-reference.md 「E: 通用制品域」「ME: Maven 域」「SR: 仓库管理域附注」
# 「M12 增补速览 · 制品操作域 / trash 域」；路由核对 internal/httpapi/router.go。

from helpers import (op, tag, q, pp, h, r, rh, S, arr, body, obj,
                     ERR_401, ERR_403, ANON)

tag("artifacts", "通用制品域——内容路径上传/下载/删除与 storage 元数据面（无 /api 前缀的容器路径 + /api/storage 族）")
tag("artifact-operations", "制品操作族——copy/move、归档下载与解包、trash can（pro 槽 repo-operations / trashcan）")


def build():
    # ---- 内容面：/binflow/{repoKey}/{path} ----
    def common(verb):
        return (
            "\n\n官方拼写：`%s /binflow/{repoKey}/{path}`（内容路径，各协议客户端同走这里）。"
            "读门随实例匿名开关（开匿名实例免认证读；闭实例 401）。Maven 域（`/{GAV路径}` 部署/解析/校验和）、"
            "npm tarball（`/{name}/-/{name}-{v}.tgz` 直取）、Go Modules（`/{module}/@v/...`）等包型接入面同路径族，"
            "详见各接入指南。" % verb)

    op("/{repoKey}/{path}", "get", "artifactDownload", "artifacts",
       "下载文件",
       "支持 Range/If-None-Match/ETag；`.sha1|.md5|.sha256` 后缀回裸 hex；"
       "归档内成员直读：`/{repo}/{archive}!/{entry}`（首个 `!/` 切分、嵌套递归）。" + common("GET"),
       params=[pp("repoKey", "仓库 key"), pp("path", "制品路径（多段）")],
       responses={
           "200": r("文件流", headers=rh("X-Checksum-Sha1", "服务端实测校验和（有值才发）")
                   | rh("X-Checksum-Sha256", "服务端实测校验和（有值才发）")
                   | rh("X-Checksum-Md5", "服务端实测校验和（有值才发）")
                   | rh("ETag", "<sha1>，不包围引号；条件请求 If-None-Match")
                   | rh("Last-Modified", "RFC1123 格式")
                   | rh("Accept-Ranges", "bytes")
                   | rh("X-Artifactory-Filename", "URL-encoded 文件名")),
           "404": r("制品不存在", schema=S("ErrorsEnvelope"),
                example={"errors": [{"status": 404, "message": "Unable to find the requested resource 'generic-local/missing.jar'."}]}),
           "401": ERR_401,
       }, security=ANON)

    op("/{repoKey}/{path}", "head", "artifactHead", "artifacts",
       "文件元信息",
       "响应头同 GET 无 body。" + common("HEAD"),
       params=[pp("repoKey", "仓库 key"), pp("path", "制品路径（多段）")],
       responses={"200": r("无 body；响应头同 GET")},
       security=ANON)

    op("/{repoKey}/{path}", "put", "artifactUpload", "artifacts",
       "上传文件",
       "body 为内容；checksum 头支持。**matrix 参数**（`;k=v` 尾随成对序列剥离为部署属性）"
       "——非成对 `;` 维持文件名字面。`PUT /binflow/{repoKey}/{path}/`（尾斜杠）= 创建目录；"
       "`PUT /binflow/{repoKey}/{path}.sha1|.md5|.sha256` = 上传校验和旁车文件。"
       "解包部署：携带 `X-Explode-Archive[: true]`（或 `X-Explode-Archive-Atomic: true`）——"
       "白名单 zip/tar/tar.gz/tgz；成功 201 空体 + `X-Binflow-Exploded-Files: <n>` 计数头；归档原件不落库（目标父目录 `w`）。"
       "npm 域的 tarball 路径 PUT 为 405（npm 域仅认 packument PUT）。" + common("PUT"),
       params=[pp("repoKey", "仓库 key"), pp("path", "制品路径（多段）"),
               h("X-Checksum-Sha1", "客户端声明校验和"),
               h("X-Checksum-Sha256", "客户端声明校验和"),
               h("X-Checksum-Md5", "客户端声明校验和"),
               h("X-Checksum", "无类型标记的校验和（按长度自动识别）"),
               h("X-Checksum-Deploy", "checksum-only 部署（不传 body）；值 true"),
               h("Expect", "100-continue：去重加速——先查 blob 是否存在"),
               h("X-Explode-Archive", "解包部署开关（true）"),
               h("X-Explode-Archive-Atomic", "原子解包（true）")],
       req_body=body("文件内容", schema={"type": "string", "format": "binary"}),
       responses={
           "201": r("上传成功", headers=rh("Location", "新资源 URL")
                    | rh("X-Binflow-Exploded-Files", "解包部署落库文件计数")),
           "409": r("checksum 不匹配", schema=S("ErrorsEnvelope"),
                    example={"errors": [{"status": 409, "message": "Checksum error for 'maven-local/com/example/demo/1.0.0/demo-1.0.0.jar': received 'abc123' but actual is 'def456'."}]}),
           "413": r("配额超限", schema=S("ErrorsEnvelope"),
                    example={"errors": [{"status": 413, "message": "Repository 'tiny' quota exceeded: used 800 of 934 bytes; the write to 'b.bin' needs 800 more bytes."}]}),
           "401": ERR_401,
       })

    op("/{repoKey}/{path}", "delete", "artifactDelete", "artifacts",
       "删除文件或目录树",
       "目录树递归删除。" + common("DELETE"),
       params=[pp("repoKey", "仓库 key"), pp("path", "制品路径（多段）")],
       responses={"200": r("已删除"), "401": ERR_401, "403": ERR_403})

    # ---- /api/storage 族 ----
    op("/api/storage/{repoKey}/{path}", "get", "storageItemInfo", "artifacts",
       "取 FileInfo / FolderInfo JSON",
       "官方拼写：`GET /binflow/api/storage/{repoKey}/{path}`。查询臂：\n"
       "- `?properties=K1,K2*` 取属性（key 过滤 + 尾 `*` 通配；无命中 = 200 `{\"properties\":{}}`——BinFlow 自有裁定，"
       "非 Artifactory 的 404；node 不存在 = 404）；\n"
       "- `?stats` 取下载统计（计数对全档可见——item-info 读门；`lastDownloadedBy` 仅 admin / readonly_admin 回带，"
       "低档位 omitempty，从不伪造；探针自身不计入计数）；\n"
       "- `?lastModified` 取目录最新修改时间；\n"
       "- `?permissions` 取有效权限视图（admin only，仅 local 仓）。\n"
       "trash can 浏览骑同一面：`GET /api/storage/auto-trashcan[...][?properties|?list]`（五元组断言面 = `?properties`）。",
       params=[pp("repoKey", "仓库 key"), pp("path", "节点路径"),
               q("properties", "key 过滤（逗号分隔；尾 `*` 通配）"),
               q("stats", "存在即取下载统计", schema={"type": "boolean"}),
               q("lastModified", "存在即取目录最新修改时间", schema={"type": "boolean"}),
               q("permissions", "存在即取有效权限视图（admin only，仅 local 仓）", schema={"type": "boolean"})],
       responses={"200": r("FileInfo / FolderInfo（?arms 回对应形态；?stats 回 StatsInfo）",
                           schema={"anyOf": [S("FolderInfo"), S("FileInfo"), S("StatsInfo")]}),
                  "404": r("node 不存在")},
       security=ANON)

    op("/api/storage/{repoKey}", "get", "storageList", "artifacts",
       "流式文件清单（仅认证用户）",
       "官方拼写：`GET /binflow/api/storage/{repoKey}?list`（带路径前缀亦可：`/api/storage/{repoKey}/{path}?list`）。",
       params=[pp("repoKey", "仓库 key"),
               q("list", "存在即流式清单（匿名 403——handler 自答，非 401 挑战）", schema={"type": "boolean"})],
       responses={"200": r("文件清单（流式）"),
                  "403": ERR_403})

    op("/api/storage/{repoKey}/{path}", "put", "storagePropertiesPut", "artifacts",
       "写属性（merge 语义）",
       "官方拼写：`PUT /binflow/api/storage/{repoKey}/{path}?properties=k=v1,v2[&recursive=1]`。"
       "**merge 语义**：同名键值集整体替换、异名键保留；node 须存在（404）。",
       params=[pp("repoKey", "仓库 key"), pp("path", "节点路径"),
               q("properties", "k=v1,v2 形（多键逗号分隔）", required=True),
               q("recursive", "folder + recursive=1 递归", schema={"type": "boolean"})],
       responses={"200": r("已写入"), "404": r("node 不存在"), "401": ERR_401})

    op("/api/storage/{repoKey}/{path}", "delete", "storagePropertiesDelete", "artifacts",
       "删属性（幂等）",
       "官方拼写：`DELETE /binflow/api/storage/{repoKey}/{path}?properties=k1,k2[&recursive=1]`。"
       "不存在的键 204；`properties=*` 全删；folder + `recursive=1` 递归。",
       params=[pp("repoKey", "仓库 key"), pp("path", "节点路径"),
               q("properties", "k1,k2 或 *（全删）", required=True),
               q("recursive", "folder 递归", schema={"type": "boolean"})],
       responses={"204": r("已删除（幂等）"), "401": ERR_401})

    # ---- copy / move / archive / trash ----
    for verb, word in (("copy", "复制"), ("move", "搬移")):
        op("/api/%s/{srcRepo}/{srcPath}" % verb, "post", "artifact%s" % verb.capitalize(),
           "artifact-operations",
           "树级%s（零拷贝）" % word,
           "官方拼写：`POST /binflow/api/%s/{srcRepo}[/{srcPath}]?to=/{targetRepo}[/{targetPath}]`"
           "（srcPath 可省略 = 整仓）。%s = copy + 源删除 + 目录剪除（move 另需源 `delete`）。"
           "认证 + 逐文件管线（源 read/目标 write）+ license；`dry=1` 干跑；"
           "响应 200 + `messages[]`，Content-Type 为 vendor 形"
           " `application/vnd.org.jfrog.artifactory.storage.CopyOrMoveResult+json`；"
           "状态 = 最后一条 error 的码（无码 409 兜底）。community 实例整族答 403 +"
           " `X-Binflow-License-Required: repo-operations`；`/api/flat/copy|move` 不实现（404）。"
           % (verb, "树级搬移" if verb == "move" else "树级复制"),
           params=[pp("srcRepo", "源仓库 key"), pp("srcPath", "源路径（可省略 = 整仓）"),
                   q("to", "/{targetRepo}[/{targetPath}] 目标", required=True),
                   q("dry", "1 = 干跑", schema={"type": "string"})],
           responses={"200": r("CopyOrMoveResult（messages[]）", schema=S("CopyMoveResult"),
                               ctype="application/vnd.org.jfrog.artifactory.storage.CopyOrMoveResult+json"),
                      "401": ERR_401, "403": ERR_403})

    op("/api/archive/download/{repoKey}/{path}", "get", "archiveDownload",
       "artifact-operations", "目录/整仓流式打包下载",
       "官方拼写：`GET /binflow/api/archive/download/{repo}[/{path}]?archiveType=zip|tar|tar.gz|tgz`"
       "（path 可省略 = 整仓）。不落盘；读权限（匿名 401 先于参数解析）；"
       "`includeChecksumFiles=true` 附 checksum 伴随条目。**默认关**（`folder_download.enabled=false`，"
       "六字段可配、重启生效——见制品操作族指南）。",
       params=[pp("repoKey", "仓库 key"), pp("path", "子树路径（可省略 = 整仓）"),
               q("archiveType", "zip | tar | tar.gz | tgz", required=True,
                 schema={"type": "string", "enum": ["zip", "tar", "tar.gz", "tgz"]}),
               q("includeChecksumFiles", "true 附 checksum 伴随条目", schema={"type": "boolean"})],
       responses={"200": r("归档流"),
                  "401": ERR_401, "403": ERR_403})

    op("/api/trash/restore/{path}", "post", "trashRestore", "artifact-operations",
       "恢复回收站条目",
       "官方拼写：`POST /binflow/api/trash/restore/{path}?to=&transaction-size=`。"
       "`to` 覆盖 > 五元组 > 路径首段；剥 `trash.*` 标记、原属性保留；"
       "响应 = copy/move 的 `messages[]` 同构。门 = system:write（仅全量 admin）+ pro 槽 trashcan（暂行）。",
       params=[pp("path", "回收站内路径"),
               q("to", "恢复目标（覆盖五元组推断）"),
               q("transaction-size", "事务批量", schema={"type": "integer"})],
       responses={"200": r("CopyOrMoveResult（messages[] 同构）", schema=S("CopyMoveResult")),
                  "401": ERR_401, "403": ERR_403})

    op("/api/trash/empty", "post", "trashEmpty", "artifact-operations",
       "清空整个回收站",
       "官方拼写：`POST /binflow/api/trash/empty`。回 JSON 摘要 `{\"removed\",\"files\",\"folders\",\"bytes\"}`。",
       responses={"200": r("摘要", schema=S("TrashSummary")),
                  "401": ERR_401, "403": ERR_403})

    op("/api/trash/clean/{path}", "delete", "trashClean", "artifact-operations",
       "单条（子树）永久清除",
       "官方拼写：`DELETE /binflow/api/trash/clean/{path}`。摘要同 empty。",
       params=[pp("path", "回收站内路径")],
       responses={"200": r("摘要", schema=S("TrashSummary")),
                  "401": ERR_401, "403": ERR_403})
