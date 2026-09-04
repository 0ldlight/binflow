# Shared helpers for the OpenAPI spec builder.
#
# Contract sources (authority order):
#   1. docs/user/api-reference.md  — endpoints/params/error wording/examples
#      verbatim; iteration markers (milestone/ticket ids) stripped.
#   2. internal/httpapi/router.go   — route inventory cross-check.
#   3. internal/httpapi/system_{maintenance,backups,schedules}.go — the
#      maintenance/backup/schedule families (ahead of the contract page).

from collections import OrderedDict

paths = OrderedDict()
TAGS = []


def tag(name, desc):
    TAGS.append({"name": name, "description": desc})


def S(ref):
    return {"$ref": "#/components/schemas/%s" % ref}


def arr(items, desc=None):
    s = {"type": "array", "items": items}
    if desc:
        s["description"] = desc
    return s


def q(name, desc=None, required=False, schema=None, example=None):
    p = {"name": name, "in": "query", "required": required,
         "description": desc if desc is not None else name, "schema": schema or {"type": "string"}}
    if example is not None:
        p["example"] = example
    return p


def pp(name, desc, example=None):
    p = {"name": name, "in": "path", "required": True,
         "description": desc, "schema": {"type": "string"}}
    if example is not None:
        p["example"] = example
    return p


def h(name, desc, typ="string", required=False, example=None):
    p = {"name": name, "in": "header", "required": required,
         "description": desc, "schema": {"type": typ}}
    if example is not None:
        p["example"] = example
    return p


def r(desc, schema=None, example=None, ctype="application/json", headers=None):
    out = {"description": desc}
    if schema is not None or example is not None:
        body = OrderedDict()
        if schema is not None:
            body["schema"] = schema
        if example is not None:
            body["example"] = example
        out["content"] = {ctype: body}
    if headers:
        out["headers"] = headers
    return out


def rh(name, desc, typ="string"):
    return {name: {"description": desc, "schema": {"type": typ}}}


def body(desc, schema=None, example=None, ctype="application/json", required=False):
    b = {"description": desc}
    if schema is not None or example is not None:
        c = {}
        if schema is not None:
            c["schema"] = schema
        if example is not None:
            c["example"] = example
        b["content"] = {ctype: c}
    if required:
        b["required"] = True
    return b


def op(path, method, oid, tags, summary, desc, params=None, responses=None,
       req_body=None, security=None, servers=None):
    o = OrderedDict()
    o["tags"] = tags if isinstance(tags, list) else [tags]
    o["summary"] = summary
    o["description"] = desc
    o["operationId"] = oid
    if params:
        o["parameters"] = params
    if req_body is not None:
        o["requestBody"] = req_body
    o["responses"] = responses or {"default": {"description": "见错误响应格式"}}
    if security is not None:
        o["security"] = security
    if servers:
        o["servers"] = servers
    paths.setdefault(path, OrderedDict())[method] = o


# ---- reusable responses -------------------------------------------------

def err_json(code, desc, example=None):
    return r(desc, schema=S("ErrorsEnvelope"), example=example)


ERR_401 = err_json(401, "未认证或凭据无效（`WWW-Authenticate: Basic realm=\"BinFlow\"`；errors[] 信封 `invalid credentials`）",
                   example={"errors": [{"status": 401, "message": "invalid credentials"}]})
ERR_403 = err_json(403, "无权限", example={"errors": [{"status": 403, "message": "permission denied"}]})


def err_text(code, desc, text=None):
    return r(code, desc, schema={"type": "string"}, example=text, ctype="text/plain")


ANON = []          # security: [] — 免认证（或随匿名开关）
ANY_AUTH = None    # 继承全局 basicAuth | bearerToken


# ---- component schemas --------------------------------------------------

def obj(props, desc=None, required=None, additional=True):
    s = {"type": "object", "properties": props}
    if desc:
        s["description"] = desc
    if required:
        s["required"] = required
    s["additionalProperties"] = additional
    return s


def build_schemas():
    sc = OrderedDict()
    sc["ErrorsEnvelope"] = obj(
        {"errors": arr(obj({"status": {"type": "integer"}, "message": {"type": "string"}},
                           additional=False))},
        desc="主要错误格式（管理面与制品域）：`{\"errors\":[{\"status\":<code>,\"message\":\"…\"}]}`",
        required=["errors"], additional=False)
    sc["OAuthError"] = obj(
        {"error": {"type": "string"}, "error_description": {"type": "string"}},
        desc="Token 端点与 docker 域的 OAuth 2.0 风格错误体",
        required=["error", "error_description"], additional=False)
    sc["FileInfo"] = obj(
        {"uri": {"type": "string"}, "downloadUri": {"type": "string"}},
        desc="FileInfo 字段族（含 uri / downloadUri 的全字段超集）")
    sc["FolderInfo"] = obj(
        {"uri": {"type": "string"}, "repo": {"type": "string"}, "children": arr({"type": "object"})},
        desc="FolderInfo 字段族（children 为节点摘要数组）")
    sc["StatsInfo"] = obj(
        {"uri": {"type": "string"},
         "downloadCount": {"type": "integer"},
         "lastDownloaded": {"type": "string", "description": "最近下载时刻；从未下载为空"},
         "lastDownloadedBy": {"type": "string", "description": "仅 admin / readonly_admin 回带；低档位 omit（从不伪造）"},
         "remoteDownloadCount": {"type": "integer"}},
        desc="下载统计（`?stats` 探针自身不计入计数）")
    sc["SearchResults"] = obj(
        {"results": arr(S("FileInfo"), desc="按调用者权限过滤；未命中 = 200 + `results:[]`，上限 1,000 行（超限置 `X-Binflow-Search-Truncated: true`）")},
        desc="搜索结果信封", required=["results"], additional=False)
    sc["AqlResponse"] = obj(
        {"results": arr(obj({}, desc="行内字段由查询的 include() 决定")),
         "range": obj({"start_pos": {"type": "integer"}, "end_pos": {"type": "integer"},
                       "total": {"type": "integer"}, "limit": {"type": "integer"}},
                      required=["start_pos", "end_pos", "total", "limit"], additional=False)},
        desc="AQL 查询响应（结果上限 1,000 行；查询文本上限 6,000 字符）",
        required=["results", "range"], additional=False)
    sc["UsageSearchRow"] = obj(
        {"uri": {"type": "string"}, "downloadCount": {"type": "integer"},
         "lastDownloaded": {"type": "string"}, "remoteDownloadCount": {"type": "integer"},
         "remoteLastDownloaded": {"type": "string"}},
        desc="闲置制品检索行（五字段）", additional=False)
    sc["DateRangeResults"] = obj(
        {"results": arr(obj({"uri": {"type": "string"},
                             "created": {"type": "string",
                                         "description": "ISO8601 毫秒；created 落在请求区间内回 created，"
                                                        "仅经 lastModified 命中的行回修改时刻"}},
                        desc="日期瘦行", additional=False))},
        desc="creation/dates 检索结果（空集 = 404，不回此形）", additional=False)
    sc["RepoTestInput"] = obj(
        {"url": {"type": "string", "description": "草稿 URL（仅本次探测覆盖存量）"},
         "username": {"type": "string"},
         "password": {"type": "string", "description": "write-only；改了 url/username 没给密码 → 匿名探测"}},
        desc="remote 仓上游探测的草稿覆盖 body（可选；空 body = 按存量配置探测）")
    sc["ProbeOutcome"] = obj(
        {"ok": {"type": "boolean"}, "status_code": {"type": "integer"},
         "message": {"type": "string"}},
        desc="连通探测判定体——ok:true 200 / ok:false 400（同形）", additional=False)
    sc["QRLSettings"] = obj(
        {"rlSettings": arr(obj({"rlType": {"type": "string", "enum": ["DEFAULT", "LOW_PRIORITY"]},
                                "permitsPerTimeFrame": {"type": "integer"},
                                "timeFrameMillis": {"type": "integer"},
                                "timeQuota": {"type": "integer"}},
                               desc="限流桶（闭集两型，三元组全正）", additional=False))},
        desc="查询限流配置（GET 回显形）", additional=False)
    sc["QRLConfigInput"] = obj(
        {"mode": {"type": "string", "enum": ["disabled", "enabled", "simulation"],
                  "description": "BinFlow 三态载体——disabled 态下不带 mode 的请求维持 400 臂"},
         "rlSettings": arr(obj({"rlType": {"type": "string", "enum": ["DEFAULT", "LOW_PRIORITY"]},
                                "permitsPerTimeFrame": {"type": "integer"},
                                "timeFrameMillis": {"type": "integer"},
                                "timeQuota": {"type": "integer"}},
                               desc="限流桶（闭集两型，三元组全正）", additional=False))},
        desc="查询限流配置合并写 body（两半皆可选——merge 语义）")
    sc["UserSummary"] = obj(
        {"name": {"type": "string"}, "uri": {"type": "string"}, "realm": {"type": "string"},
         "source": {"type": "string"}, "email": {"type": "string"},
         "adminRole": {"type": "string", "description": "admin / readonly_admin / user"},
         "enabled": {"type": "boolean"}, "groups": arr({"type": "string"}, desc="恒渲染，空组为 [] 非 null")},
        desc="用户条目（列表与单查同构；无口令字段）")
    sc["UserInput"] = obj(
        {"name": {"type": "string"}, "email": {"type": "string"}, "password": {"type": "string"},
         "admin": {"type": "boolean"}, "adminRole": {"type": "string"}, "groups": arr({"type": "string"}),
         "enabled": {"type": "boolean", "description": "指针语义：显式 false 禁用登录；缺省不动"}},
        desc="创建/更新用户 body（adminRole 仅 admin 可写）")
    sc["GroupDetail"] = obj(
        {"name": {"type": "string"}, "description": {"type": "string"},
         "userNames": arr({"type": "string"}, desc="仅 `?includeUsers=true` 时附带的成员名数组（空组为 [] 恒非 null）")},
        desc="组详情（无参形态三字段；带参附 userNames）")
    sc["TokenResponse"] = obj(
        {"access_token": {"type": "string", "description": "64 位 hex（256-bit）"},
         "token_id": {"type": "string"}, "expires_in": {"type": "integer", "description": "默认 2592000 秒（30 天）"},
         "scope": {"type": "string", "description": "`api:*`"}},
        desc="Token 签发响应", required=["access_token"], additional=False)
    sc["PermissionTarget"] = obj(
        {"name": {"type": "string"},
         "repos": arr({"type": "string"}),
         "principals": obj({"users": obj({}, desc="用户名 → 动作数组"),
                            "groups": obj({}, desc="组名 → 动作数组")},
                           desc="principals 回显动作正名单单形：read, deploy-cache, annotate, delete, manage（write 别名收词不回显）")},
        desc="Permission Target")
    sc["PermissionTargetInput"] = obj(
        {"name": {"type": "string"}, "repos": arr({"type": "string"}),
         "principals": obj({"users": obj({}, desc="用户名 → 动作数组"),
                            "groups": obj({}, desc="组名 → 动作数组")})},
        desc="创建 body。动作集五值闭集 `read / deploy-cache / annotate / delete / manage`；`write` 仍被接受为 `deploy-cache` 的兼容别名（不附带 annotate）；manage 持有者可编辑覆盖集内的 target")
    sc["RepoConfig"] = obj(
        {"key": {"type": "string"}, "rclass": {"type": "string", "enum": ["local", "remote", "virtual"]},
         "packageType": {"type": "string"},
         "url": {"type": "string", "description": "remote 仓上游地址（裸 origin，不带 /binflow 后缀）"},
         "description": {"type": "string"},
         "username": {"type": "string"}, "password": {"type": "string"},
         "quotaBytes": {"type": "integer", "description": "配额（0 = 不限）"},
         "defaultDeploymentRepo": {"type": "string"},
         "unusedArtifactsCleanupPeriodHours": {"type": "integer"},
         "chartsBaseUrl": {"type": "string", "description": "仅 packageType=helm 的 remote；绝对 http(s) URL；其它包型携带 → 400 点名字段"},
         "enableTokenAuthentication": {"type": "boolean", "description": "remote 仓；true 时拉取侧对上游发 Authorization: Bearer <password>"},
         "contentSynchronisation": obj({"enabled": {"type": "boolean"},
                                        "propertiesEnabled": {"type": "boolean"},
                                        "statisticsEnabled": {"type": "boolean"},
                                        "sourceOrigin": {"type": "boolean"}})},
        desc="仓库配置（读写同构回显 canonical 形）。rclass × packageType 组合门已全量退役，建仓面唯一剩余门是 license 档位（进阶包型在低档位 400 `package type not available on this instance: ...`）")
    sc["UsageRow"] = obj(
        {"repo": {"type": "string"}, "usedBytes": {"type": "integer"}, "quotaBytes": {"type": "integer"},
         "nodeCount": {"type": "integer", "description": "仅 `?include=counts`；只计文件 node"},
         "updatedAt": {"type": "string", "description": "仅 `?include=counts`；仓库配置变更时刻（非「最新制品时间」）"}},
        desc="配额用量行", required=["repo", "usedBytes", "quotaBytes"], additional=False)
    sc["AuditResponse"] = obj(
        {"events": arr(obj({"id": {"type": "integer"}, "time": {"type": "string"},
                            "actor": {"type": "string"}, "action": {"type": "string"},
                            "repo": {"type": "string"}, "path": {"type": "string"},
                            "detail": {"type": "object", "description": "可选附加上下文（如 quota.exceeded 的 {used, quota}）"}},
                           desc="结果按时间倒序（最新在前）")),
         "nextCursor": {"type": "string", "description": "游标分页（不透明，回传继续翻页）"}},
        desc="审计日志响应", required=["events"])
    sc["GCRequest"] = obj(
        {"apply": {"type": "boolean", "description": "false=dry-run（只报告不删除）；true=实际执行"},
         "graceHours": {"type": "number", "description": "宽限期（配置值默认 24）；0=无宽限；负值或 >876000 → 400"}},
        desc="GC 触发 body")
    sc["GCResponse"] = obj(
        {"candidateCount": {"type": "integer"}, "candidateBytes": {"type": "integer"},
         "deletedCount": {"type": "integer", "description": "dry-run 恒 0"}},
        required=["candidateCount", "candidateBytes", "deletedCount"], additional=False)
    sc["CleanupReport"] = obj(
        {"trigger": {"type": "string"}, "apply": {"type": "boolean"},
         "repos": arr(obj({"repo": {"type": "string"}, "periodHours": {"type": "integer"},
                           "cutoff": {"type": "string"}, "keptByUse": {"type": "integer"},
                           "candidates": {"type": "integer"}, "deleted": {"type": "integer"},
                           "bytes": {"type": "integer"}})),
         "gracePending": {"type": "integer"}, "gcDeleted": {"type": "integer"},
         "sessionsSwept": {"type": "integer"}, "objectsCleaned": {"type": "integer"},
         "bytesReclaimed": {"type": "integer"}, "ok": {"type": "boolean"}},
        desc="unused-cleanup 报告（dry-run 默认）")
    sc["SystemSettings"] = obj(
        {"folder_download": obj({"enabled": {"type": "boolean"},
                                 "enabled_for_anonymous": {"type": "boolean"},
                                 "max_download_size_mb": {"type": "integer"},
                                 "max_files": {"type": "integer"},
                                 "max_concurrent_requests": {"type": "integer"},
                                 "enabled_empty_directories": {"type": "boolean"}}),
         "trashcan": obj({"retention_days": {"type": "integer"}})},
        desc="已解析的运行旋钮（YAML+env+defaults 合流值）；knob-scoped——只回行为旋钮，永不携带 secret/DSN/路径；只读（其余动词 404）；旋钮本身重启生效",
        additional=False)
    sc["MaintenanceSlot"] = obj(
        {"key": {"type": "string", "enum": ["gc", "cleanup-unused-cache", "cleanup-virtual"]},
         "cronExp": {"type": "string"}, "enabled": {"type": "boolean"},
         "nextRun": {"type": "string"}, "lastRun": {"type": "string"},
         "lastStatus": {"type": "string"}, "lastError": {"type": "string"}},
        desc="维护面 cron 槽位状态（无行 = 未排程，渲染默认形）")
    sc["MaintenanceProjection"] = obj(
        {"slots": arr(S("MaintenanceSlot"))}, required=["slots"], additional=False)
    sc["MaintenancePutSlot"] = obj(
        {"cronExp": {"type": "string", "description": "空串 = 删除该槽位的排程行（唯一清除途径）；非空经解析校验（含可达的未来触发点）"},
         "enabled": {"type": "boolean", "description": "缺省 true（刚输入的排程意为生效）"}},
        desc="维护面单槽位写臂")
    sc["MaintenancePutBody"] = obj(
        {"gc": S("MaintenancePutSlot"), "cleanup-unused-cache": S("MaintenancePutSlot"),
         "cleanup-virtual": S("MaintenancePutSlot")},
        desc="每槽位可选，缺席即不动；全空 body → 400 `maintenance body carries no slot arm (gc, cleanup-unused-cache, cleanup-virtual)`；所有臂先校验后落库（坏表达式不半落）",
        additional=False)
    sc["BackupConfig"] = obj(
        {"backupKey": {"type": "string"}, "enabled": {"type": "boolean"},
         "exportPath": {"type": "string", "description": "服务端绝对路径（备份产物落盘目录）"},
         "cronExp": {"type": "string"}, "nextScheduleBackup": {"type": "string", "description": "空 = 未排程"},
         "lastRun": {"type": "string"}, "lastStatus": {"type": "string"}, "lastError": {"type": "string"},
         "createdAt": {"type": "string"}, "updatedAt": {"type": "string"}},
        desc="备份配置读形（payload 行 + cron 投影合流）")
    sc["BackupBody"] = obj(
        {"backupKey": {"type": "string", "description": "1..64 位字母/数字/./_/-，字母数字开头；路径 {key} 形态时以路径为准"},
         "enabled": {"type": "boolean", "description": "缺省 true"},
         "cronExp": {"type": "string", "description": "空串 = 保留 payload 行但不排程（排程行删除）"},
         "nextBackupTime": {"type": "string", "description": "RFC3339 且须在未来；缺省由表达式计算"},
         "exportPath": {"type": "string", "description": "必填；绝对路径且不含 .. 段"}},
        desc="备份配置写 body")
    sc["BackupsList"] = obj({"backups": arr(S("BackupConfig"))},
                            required=["backups"], additional=False)
    sc["ScheduleStatus"] = obj(
        {"domain": {"type": "string", "enum": ["maintenance", "backup", "replication"]},
         "key": {"type": "string"}, "cronExp": {"type": "string"}, "enabled": {"type": "boolean"},
         "nextRun": {"type": "string"}, "lastRun": {"type": "string"},
         "lastStatus": {"type": "string"}, "lastError": {"type": "string"}},
        desc="cron 台账行投影（enabled = 行启用且表达式非空）")
    sc["SchedulesList"] = obj({"schedules": arr(S("ScheduleStatus"))},
                              required=["schedules"], additional=False)
    sc["GlobalBlockState"] = obj(
        {"blockPullReplications": {"type": "boolean"}, "blockPushReplications": {"type": "boolean"}},
        desc="全局封锁态（官方键形）", additional=False)
    sc["ReplicationConfig"] = obj(
        {"id": {"type": "integer", "description": "数值 id（不可变键；PUT 启停按它）"},
         "name": {"type": "string", "description": "DELETE 按名删除（两种寻址并存）"},
         "enabled": {"type": "boolean"},
         "updated_at": {"type": "string"}},
        desc="复制配置行（GET 投影同形；凭据字段永不回显；整行 round-trip 字段容忍接收）")
    sc["ReplicationRunResponse"] = obj(
        {"info": {"type": "string"}, "id": {"type": "integer"}, "name": {"type": "string"},
         "scheduled": {"type": "integer", "description": "本次种入任务数（空源仓 = 0 空跑）"},
         "capped": {"type": "boolean", "description": "受 max_items_per_push 截断（再点取下一段）"}},
        additional=False)
    sc["ReplicationTestResponse"] = obj(
        {"ok": {"type": "boolean"}, "status_code": {"type": "integer"}, "message": {"type": "string"}},
        desc="连通探测判定体（失败同形 400）", additional=False)
    sc["KeyPairSummary"] = obj(
        {"pairName": {"type": "string"}, "pairType": {"type": "string", "description": "GPG"},
         "alias": {"type": "string"}, "publicKey": {"type": "string"},
         "algorithm": {"type": "string"}, "createdAt": {"type": "string"},
         "updatedAt": {"type": "string"}, "updatedBy": {"type": "string"},
         "repositories": arr({"type": "string"})},
        desc="钥对摘要（私钥与口令永不出库——无导出端点）")
    sc["KeyPairInput"] = obj(
        {"pairName": {"type": "string"}, "pairType": {"type": "string"},
         "alias": {"type": "string"}, "privateKey": {"type": "string"},
         "publicKey": {"type": "string"}, "passphrase": {"type": "string"}},
        desc="导入/更新 body")
    sc["KeyPairGenerateInput"] = obj(
        {"pairName": {"type": "string"}, "alias": {"type": "string"}, "passphrase": {"type": "string"},
         "keyBits": {"type": "integer"}, "uidName": {"type": "string"},
         "uidComment": {"type": "string"}, "uidEmail": {"type": "string"}},
        desc="服务端生成入参（重名 409）")
    sc["TestReport"] = obj(
        {"ok": {"type": "boolean"}, "phase": {"type": "string"},
         "category": {"type": "string"}, "message": {"type": "string"}},
        desc="认证配置 test 响应；ok:false 时 HTTP 400", additional=False)
    sc["AddonSlot"] = obj(
        {"id": {"type": "string"}, "kind": {"type": "string"}, "minTier": {"type": "string"},
         "enabled": {"type": "boolean"}, "reason": {"type": "string"},
         "displayName": {"type": "string"}, "description": {"type": "string"}},
        desc="addon 槽位（无写面——其余动词 404）")
    sc["UploadSessionToken"] = obj({"token": {"type": "string"}},
                                   desc="会话能力凭据", additional=False)
    sc["UploadConfig"] = obj({"supported": {"type": "boolean"}},
                             desc="能力探测：S3 栈 true / filestore false（探测端点不回 501）", additional=False)
    sc["UploadUrl"] = obj({"url": {"type": "string"}},
                          desc="分片上传 URL——查询串自带 ?token= 能力，PUT 可免 Authorization", additional=False)
    sc["UploadStatus"] = obj(
        {"status": {"type": "string", "enum": ["PARTS", "PROCESSING", "FINISHED", "NON_RETRYABLE_ERROR"]},
         "error": {"type": "string"}, "progress": {"type": "integer"},
         "checksumToken": {"type": "string", "description": "FINISHED 时回带；凭它做零传输 X-Checksum-Deploy PUT 落节点"}},
        additional=False)
    sc["CopyMoveResult"] = obj(
        {"messages": arr(obj({"level": {"type": "string"}, "message": {"type": "string"},
                              "status": {"type": "integer", "description": "整体状态 = 最后一条 error 的码（无码 409 兜底）"}}))},
        desc="树级复制/搬移响应（Content-Type 为 vendor 形 application/vnd.org.jfrog.artifactory.storage.CopyOrMoveResult+json）")
    sc["TrashSummary"] = obj(
        {"removed": {"type": "integer"}, "files": {"type": "integer"},
         "folders": {"type": "integer"}, "bytes": {"type": "integer"}},
        additional=False)
    sc["SubscriptionRequest"] = obj(
        {"key": {"type": "string", "description": "^[A-Za-z][A-Za-z0-9_-]+$ ≤500"},
         "project_key": {"type": "string"}, "description": {"type": "string"},
         "enabled": {"type": "boolean", "description": "默认 false"},
         "event_filter": obj({"domain": {"type": "string"}, "event_types": arr({"type": "string"}),
                              "criteria": obj({}, desc="strict——未知键 400")}),
         "handlers": arr(obj({"url": {"type": "string"}, "type": {"type": "string"},
                              "use_secret_for_signing": {"type": "boolean"},
                              "secrets": obj({}, desc="secret 恒掩码 ********")}),
                         desc="恰 1 个；webhook 或 custom-webhook 两型"),
         "debug": {"type": "boolean"}},
        desc="订阅一形（创建/更新/试发共用）")
    sc["SubscriptionView"] = obj(
        {"key": {"type": "string"}, "project_key": {"type": "string"}, "description": {"type": "string"},
         "enabled": {"type": "boolean"}, "event_filter": obj({}), "handlers": arr(obj({})),
         "debug": {"type": "boolean"}},
        desc="订阅回显（secret 恒掩码 ********）")
    sc["TestOutcome"] = obj(
        {"ok": {"type": "boolean"},
         "attempt": obj({"status_code": {"type": "integer"}, "elapsed_millis": {"type": "integer"},
                         "error": {"type": "string"}})},
        desc="试发结果——失败也是 200，看 body", additional=False)
    sc["SessionResponse"] = obj(
        {"username": {"type": "string"}, "admin": {"type": "boolean"},
         "adminRole": {"type": "string"}, "source": {"type": "string"}},
        desc="登录/whoami 回显")
    return sc
