# Domain: 通用制品域（内容面）+ storage 面 + 制品操作/trash。
# Source: docs/user/api-reference.md 「E: 通用制品域」「ME: Maven 域」「SR: 仓库管理域附注」
# 「M12 增补速览 · 制品操作域 / trash 域」；路由核对 internal/httpapi/router.go。

from helpers import (op, tag, q, pp, h, r, rh, S, arr, body, obj,
                     ERR_401, ERR_403, ANON)

tag("artifacts", "Artifacts — content-path upload/download/delete plus the storage metadata face (container paths without the /api prefix + the /api/storage family)")
tag("artifact-operations", "Artifact operations — copy/move, archive download and extraction, trash can (pro slots repo-operations / trashcan)")


def build():
    # ---- 内容面：/binflow/{repoKey}/{path} ----
    def common(verb):
        return (
            "\n\nCanonical path: `%s /binflow/{repoKey}/{path}` (content path; all protocol clients go through it). "
            "Read access follows the instance's anonymous switch (anonymous-enabled instances allow unauthenticated reads; otherwise 401). "
            "Package-type access surfaces share the same path family — Maven (`/{GAV path}` deploy/resolve/checksums), "
            "npm tarballs (`/{name}/-/{name}-{v}.tgz` direct fetch), Go Modules (`/{module}/@v/...`) — see each client guide." % verb)

    op("/{repoKey}/{path}", "get", "artifactDownload", "artifacts",
       "Download a file",
       "Supports Range/If-None-Match/ETag; `.sha1|.md5|.sha256` suffixes return the bare hex; "
       "archive members are directly readable: `/{repo}/{archive}!/{entry}` (split at the first `!/`, nested recursively)." + common("GET"),
       params=[pp("repoKey", "Repository key"), pp("path", "Artifact path (multiple segments)")],
       responses={
           "200": r("File stream", headers=rh("X-Checksum-Sha1", "Server-measured checksum (sent only when available)")
                   | rh("X-Checksum-Sha256", "Server-measured checksum (sent only when available)")
                   | rh("X-Checksum-Md5", "Server-measured checksum (sent only when available)")
                   | rh("ETag", "<sha1> without surrounding quotes; conditional requests via If-None-Match")
                   | rh("Last-Modified", "RFC1123 format")
                   | rh("Accept-Ranges", "bytes")
                   | rh("X-Artifactory-Filename", "URL-encoded file name")),
           "404": r("Artifact not found", schema=S("ErrorsEnvelope"),
                example={"errors": [{"status": 404, "message": "Unable to find the requested resource 'generic-local/missing.jar'."}]}),
           "401": ERR_401,
       }, security=ANON)

    op("/{repoKey}/{path}", "head", "artifactHead", "artifacts",
       "File metadata",
       "Response headers identical to GET, no body." + common("HEAD"),
       params=[pp("repoKey", "Repository key"), pp("path", "Artifact path (multiple segments)")],
       responses={"200": r("No body; response headers identical to GET")},
       security=ANON)

    op("/{repoKey}/{path}", "put", "artifactUpload", "artifacts",
       "Upload a file",
       "The body is the content; checksum headers are supported. **Matrix parameters** "
       "(trailing `;k=v` pairs are stripped off as deploy properties) — an unpaired `;` stays literal in the file name. "
       "`PUT /binflow/{repoKey}/{path}/` (trailing slash) creates a directory; "
       "`PUT /binflow/{repoKey}/{path}.sha1|.md5|.sha256` uploads a checksum sidecar file. "
       "Explode deploy: send `X-Explode-Archive: true` (or `X-Explode-Archive-Atomic: true`) — "
       "whitelist zip/tar/tar.gz/tgz; success is 201 with an empty body plus the `X-Binflow-Exploded-Files: <n>` count header; "
       "the archive itself is not stored (`w` on the target parent directory). "
       "A tarball-path PUT in the npm domain is 405 (the npm domain accepts packument PUTs only)." + common("PUT"),
       params=[pp("repoKey", "Repository key"), pp("path", "Artifact path (multiple segments)"),
               h("X-Checksum-Sha1", "Client-declared checksum"),
               h("X-Checksum-Sha256", "Client-declared checksum"),
               h("X-Checksum-Md5", "Client-declared checksum"),
               h("X-Checksum", "Checksum without a type marker (auto-detected by length)"),
               h("X-Checksum-Deploy", "Checksum-only deploy (no body); value true"),
               h("Expect", "100-continue: dedup fast path — checks blob existence first"),
               h("X-Explode-Archive", "Explode deploy switch (true)"),
               h("X-Explode-Archive-Atomic", "Atomic explode (true)")],
       req_body=body("File content", schema={"type": "string", "format": "binary"}),
       responses={
           "201": r("Uploaded", headers=rh("Location", "URL of the new resource")
                    | rh("X-Binflow-Exploded-Files", "Count of files stored by the explode deploy")),
           "409": r("Checksum mismatch", schema=S("ErrorsEnvelope"),
                    example={"errors": [{"status": 409, "message": "Checksum error for 'maven-local/com/example/demo/1.0.0/demo-1.0.0.jar': received 'abc123' but actual is 'def456'."}]}),
           "413": r("Quota exceeded", schema=S("ErrorsEnvelope"),
                    example={"errors": [{"status": 413, "message": "Repository 'tiny' quota exceeded: used 800 of 934 bytes; the write to 'b.bin' needs 800 more bytes."}]}),
           "401": ERR_401,
       })

    op("/{repoKey}/{path}", "delete", "artifactDelete", "artifacts",
       "Delete a file or a directory tree",
       "Directory trees are deleted recursively." + common("DELETE"),
       params=[pp("repoKey", "Repository key"), pp("path", "Artifact path (multiple segments)")],
       responses={"200": r("Deleted"), "401": ERR_401, "403": ERR_403})

    # ---- /api/storage 族 ----
    op("/api/storage/{repoKey}/{path}", "get", "storageItemInfo", "artifacts",
       "Get FileInfo / FolderInfo JSON",
       "Canonical path: `GET /binflow/api/storage/{repoKey}/{path}`. Query arms:\n"
       "- `?properties=K1,K2*` returns properties (key filter + trailing `*` wildcard; no matches = 200 `{\"properties\":{}}` — "
       "a BinFlow compatibility ruling, not a 404; nonexistent node = 404);\n"
       "- `?stats` returns download statistics (counts visible on all tiers — the item-info read gate; "
       "`lastDownloadedBy` is returned only to admin / readonly_admin, omitted on lower tiers, never fabricated; "
       "the probe itself is not counted);\n"
       "- `?lastModified` returns the directory's latest modification time;\n"
       "- `?permissions` returns the effective-permissions view (admin only, local repositories only).\n"
       "Trash can browsing rides the same face: `GET /api/storage/auto-trashcan[...][?properties|?list]` "
       "(the five-tuple assertion face = `?properties`).",
       params=[pp("repoKey", "Repository key"), pp("path", "Node path"),
               q("properties", "Key filter (comma-separated; trailing `*` wildcard)"),
               q("stats", "Present → return download statistics", schema={"type": "boolean"}),
               q("lastModified", "Present → return the directory's latest modification time", schema={"type": "boolean"}),
               q("permissions", "Present → return the effective-permissions view (admin only, local repositories only)", schema={"type": "boolean"})],
       responses={"200": r("FileInfo / FolderInfo (?arms return the corresponding shape; ?stats returns StatsInfo)",
                           schema={"anyOf": [S("FolderInfo"), S("FileInfo"), S("StatsInfo")]}),
                  "404": r("Node not found")},
       security=ANON)

    int32_err = ("If any of the seven integer parameters is **present** but fails to parse (not an integer or out of int32 range) "
                 "→ 400 `For input string: \"<v>\"`; empty/blank values are treated as absent (same as not sent)")
    op("/api/storage/{repoKey}", "get", "storageList", "artifacts",
       "Streaming file listing (authenticated users only)",
       "Canonical path: `GET /binflow/api/storage/{repoKey}?list` (a path prefix also works: `/api/storage/{repoKey}/{path}?list`). "
       "The full seven-parameter family: `deep`/`depth`/`listFolders`/`includeRootPath` control the enumeration shape, "
       "`mdTimestamps`/`statsTimestamps`/`includePropertiesMd5` attach extra per-entry metadata. Boolean arms are taken as "
       "value == 1 (other integers do not enable); `depth` only bounds the recursion of `deep=1` and does not trigger "
       "recursion itself. The response Content-Type is the vendor form "
       "`application/vnd.org.jfrog.artifactory.storage.FileList+json` — "
       "`files[]` mixes files and folders in one alphabetical order, `uri` carries a leading slash and is relative to the "
       "queried directory; folder rows (`listFolders=1`) have no trailing slash on `uri`, `size` -1, `folder` true. "
       + int32_err + ". Anonymous → 403 (answered by the handler itself, not a 401 challenge).",
       params=[pp("repoKey", "Repository key"),
               q("list", "Present → streaming listing (anonymous → 403 — answered by the handler itself, not a 401 challenge)", schema={"type": "boolean"}),
               q("deep", "1 = recursively list subdirectory contents (default 0 flat; other integer values do not enable — boolean arms are taken as ==1)",
                 schema={"type": "integer", "default": 0}, example=1),
               q("depth", "Recursion depth bound for deep=1 (default 0 unlimited; 1 = direct children only); modifier only — does not trigger recursion itself",
                 schema={"type": "integer", "default": 0}, example=1),
               q("listFolders", "1 = folder rows merged into files[] (size -1, folder true, same alphabetical order as file rows)",
                 schema={"type": "integer", "default": 0}, example=1),
               q("includeRootPath", "1 = the queried directory itself leads files[] as a `/` row (size -1)",
                 schema={"type": "integer", "default": 0}, example=1),
               q("mdTimestamps", "1 = entries carrying properties (files and folders) get mdTimestamps.properties (time of the last property change)",
                 schema={"type": "integer", "default": 0}, example=1),
               q("statsTimestamps", "1 = file entries with download history get mdTimestamps.artifactory.stats (last download time; never-downloaded files and folder rows are skipped)",
                 schema={"type": "integer", "default": 0}, example=1),
               q("includePropertiesMd5", "1 = entries carrying properties get propertiesMd5 (md5 over the canonical serialization of the property set)",
                 schema={"type": "integer", "default": 0}, example=1)],
       responses={"200": r("File listing (FileList shape: uri / created / files[]; entry fields uri·size·lastModified·folder·sha1·sha2 "
                          "+ optional mdTimestamps{}·propertiesMd5)",
                          schema=obj({"uri": {"type": "string"},
                                      "created": {"type": "string"},
                                      "files": arr(obj({"uri": {"type": "string"},
                                                        "size": {"type": "integer"},
                                                        "lastModified": {"type": "string"},
                                                        "folder": {"type": "boolean"},
                                                        "sha1": {"type": "string"},
                                                        "sha2": {"type": "string"},
                                                        "mdTimestamps": obj({"properties": {"type": "string"},
                                                                             "artifactory.stats": {"type": "string"}},
                                                                            desc="Keys attached by mdTimestamps=1 / statsTimestamps=1 respectively (only when the underlying fact exists)"),
                                                        "propertiesMd5": {"type": "string"}},
                                                       desc="Listing entry (alphabetical interleave; folder rows have size -1 and no digest)"))},
                                      desc="FileList response (streaming)"),
                          ctype="application/vnd.org.jfrog.artifactory.storage.FileList+json"),
                  "400": r(int32_err, schema=S("ErrorsEnvelope"),
                           example={"errors": [{"status": 400, "message": "For input string: \"abc\""}]}),
                  "403": ERR_403})

    op("/api/storage/{repoKey}/{path}", "put", "storagePropertiesPut", "artifacts",
       "Write properties (merge semantics)",
       "Canonical path: `PUT /binflow/api/storage/{repoKey}/{path}?properties=k=v1,v2[&recursive=1]`. "
       "**Merge semantics**: the value set of a same-named key is replaced wholesale, differently-named keys are kept; "
       "the node must exist (404).",
       params=[pp("repoKey", "Repository key"), pp("path", "Node path"),
               q("properties", "k=v1,v2 form (multiple keys comma-separated)", required=True),
               q("recursive", "folder + recursive=1 applies recursively", schema={"type": "boolean"})],
       responses={"200": r("Written"), "404": r("Node not found"), "401": ERR_401})

    op("/api/storage/{repoKey}/{path}", "delete", "storagePropertiesDelete", "artifacts",
       "Delete properties (idempotent)",
       "Canonical path: `DELETE /binflow/api/storage/{repoKey}/{path}?properties=k1,k2[&recursive=1]`. "
       "Nonexistent keys are 204; `properties=*` deletes everything; folder + `recursive=1` applies recursively.",
       params=[pp("repoKey", "Repository key"), pp("path", "Node path"),
               q("properties", "k1,k2 or * (delete all)", required=True),
               q("recursive", "Apply recursively on folders", schema={"type": "boolean"})],
       responses={"204": r("Deleted (idempotent)"), "401": ERR_401})

    # ---- copy / move / archive / trash ----
    for verb, word in (("copy", "copy"), ("move", "move")):
        op("/api/%s/{srcRepo}/{srcPath}" % verb, "post", "artifact%s" % verb.capitalize(),
           "artifact-operations",
           "Tree-level %s (zero-copy)" % word,
           "Canonical path: `POST /binflow/api/%s/{srcRepo}[/{srcPath}]?to=/{targetRepo}[/{targetPath}]` "
           "(srcPath may be omitted = the whole repository). A %s = copy + source deletion + directory pruning "
           "(move additionally requires source `delete`). Requires authentication + a per-file pipeline "
           "(source read / target write) + license; `dry=1` for a dry run; "
           "responds 200 + `messages[]`, Content-Type is the vendor form "
           "`application/vnd.org.jfrog.artifactory.storage.CopyOrMoveResult+json`; "
           "the status = the code of the last error message (409 fallback when none). Community instances answer the "
           "whole family 403 + `X-Binflow-License-Required: repo-operations`; `/api/flat/copy|move` is not implemented (404)."
           % (verb, "Move" if verb == "move" else "Copy"),
           params=[pp("srcRepo", "Source repository key"), pp("srcPath", "Source path (may be omitted = the whole repository)"),
                   q("to", "Target /{targetRepo}[/{targetPath}]", required=True),
                   q("dry", "1 = dry run", schema={"type": "string"})],
           responses={"200": r("CopyOrMoveResult (messages[])", schema=S("CopyMoveResult"),
                               ctype="application/vnd.org.jfrog.artifactory.storage.CopyOrMoveResult+json"),
                      "401": ERR_401, "403": ERR_403})

    op("/api/archive/download/{repoKey}/{path}", "get", "archiveDownload",
       "artifact-operations", "Streaming archive download of a directory or whole repository",
       "Canonical path: `GET /binflow/api/archive/download/{repo}[/{path}]?archiveType=zip|tar|tar.gz|tgz` "
       "(path may be omitted = the whole repository). Nothing is written to disk; read permission "
       "(anonymous 401 precedes parameter parsing); `includeChecksumFiles=true` includes checksum sidecar entries. "
       "**Off by default** (`folder_download.enabled=false`; six configurable fields, effective on restart — "
       "see the artifact operations guide).",
       params=[pp("repoKey", "Repository key"), pp("path", "Subtree path (may be omitted = the whole repository)"),
               q("archiveType", "zip | tar | tar.gz | tgz", required=True,
                 schema={"type": "string", "enum": ["zip", "tar", "tar.gz", "tgz"]}),
               q("includeChecksumFiles", "true includes checksum sidecar entries", schema={"type": "boolean"})],
       responses={"200": r("Archive stream"),
                  "401": ERR_401, "403": ERR_403})

    op("/api/trash/restore/{path}", "post", "trashRestore", "artifact-operations",
       "Restore a trash can entry",
       "Canonical path: `POST /binflow/api/trash/restore/{path}?to=&transaction-size=`. "
       "`to` overrides > five-tuple inference > first path segment; `trash.*` markers are stripped, original properties "
       "are kept; the response is isomorphic to copy/move `messages[]`. Gate = system:write (full admins only) + the "
       "trashcan pro slot (interim).",
       params=[pp("path", "Path inside the trash can"),
               q("to", "Restore target (overrides five-tuple inference)"),
               q("transaction-size", "Transaction batch size", schema={"type": "integer"})],
       responses={"200": r("CopyOrMoveResult (messages[], isomorphic)", schema=S("CopyMoveResult")),
                  "401": ERR_401, "403": ERR_403})

    op("/api/trash/empty", "post", "trashEmpty", "artifact-operations",
       "Empty the whole trash can",
       "Canonical path: `POST /binflow/api/trash/empty`. Returns a JSON summary `{\"removed\",\"files\",\"folders\",\"bytes\"}`.",
       responses={"200": r("Summary", schema=S("TrashSummary")),
                  "401": ERR_401, "403": ERR_403})

    op("/api/trash/clean/{path}", "delete", "trashClean", "artifact-operations",
       "Permanently purge a single entry (subtree)",
       "Canonical path: `DELETE /binflow/api/trash/clean/{path}`. Same summary shape as empty.",
       params=[pp("path", "Path inside the trash can")],
       responses={"200": r("Summary", schema=S("TrashSummary")),
                  "401": ERR_401, "403": ERR_403})
