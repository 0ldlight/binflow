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
    o["responses"] = responses or {"default": {"description": "See error response formats."}}
    if security is not None:
        o["security"] = security
    if servers:
        o["servers"] = servers
    paths.setdefault(path, OrderedDict())[method] = o


# ---- reusable responses -------------------------------------------------

def err_json(code, desc, example=None):
    return r(desc, schema=S("ErrorsEnvelope"), example=example)


ERR_401 = err_json(401, "Unauthenticated or invalid credentials (challenges with `WWW-Authenticate: Basic realm=\"BinFlow\"`; errors[] envelope `invalid credentials`)",
                   example={"errors": [{"status": 401, "message": "invalid credentials"}]})
ERR_403 = err_json(403, "Permission denied", example={"errors": [{"status": 403, "message": "permission denied"}]})


def err_text(code, desc, text=None):
    return r(code, desc, schema={"type": "string"}, example=text, ctype="text/plain")


ANON = []          # security: [] — no auth (or anonymous-enabled instance)
ANY_AUTH = None    # inherit global basicAuth | bearerToken


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
        desc="Primary error format (management API and artifact domains): `{\"errors\":[{\"status\":<code>,\"message\":\"…\"}]}`",
        required=["errors"], additional=False)
    sc["OAuthError"] = obj(
        {"error": {"type": "string"}, "error_description": {"type": "string"}},
        desc="OAuth 2.0-style error body used by the token endpoint and the docker domain",
        required=["error", "error_description"], additional=False)
    sc["FileInfo"] = obj(
        {"uri": {"type": "string"}, "downloadUri": {"type": "string"}},
        desc="FileInfo field family (full-field superset including uri / downloadUri)")
    sc["FolderInfo"] = obj(
        {"uri": {"type": "string"}, "repo": {"type": "string"}, "children": arr({"type": "object"})},
        desc="FolderInfo field family (children is an array of node summaries)")
    sc["StatsInfo"] = obj(
        {"uri": {"type": "string"},
         "downloadCount": {"type": "integer"},
         "lastDownloaded": {"type": "string", "description": "Last download time; empty when never downloaded"},
         "lastDownloadedBy": {"type": "string", "description": "Returned only to admin / readonly_admin; omitted on lower tiers (never fabricated)"},
         "remoteDownloadCount": {"type": "integer"}},
        desc="Download statistics (the `?stats` probe itself is not counted)")
    sc["SearchResults"] = obj(
        {"results": arr(S("FileInfo"), desc="Filtered by the caller's permissions; no matches = 200 + `results:[]`, capped at 1,000 rows (over cap sets `X-Binflow-Search-Truncated: true`)")},
        desc="Search results envelope", required=["results"], additional=False)
    sc["AqlResponse"] = obj(
        {"results": arr(obj({}, desc="Per-row fields are determined by the query's include()")),
         "range": obj({"start_pos": {"type": "integer"}, "end_pos": {"type": "integer"},
                       "total": {"type": "integer"}, "limit": {"type": "integer"}},
                      required=["start_pos", "end_pos", "total", "limit"], additional=False)},
        desc="AQL query response (results capped at 1,000 rows; query text capped at 6,000 characters)",
        required=["results", "range"], additional=False)
    sc["UsageSearchRow"] = obj(
        {"uri": {"type": "string"}, "downloadCount": {"type": "integer"},
         "lastDownloaded": {"type": "string"}, "remoteDownloadCount": {"type": "integer"},
         "remoteLastDownloaded": {"type": "string"}},
        desc="Unused-artifact search row (five fields)", additional=False)
    sc["DateRangeResults"] = obj(
        {"results": arr(obj({"uri": {"type": "string"},
                             "created": {"type": "string",
                                         "description": "ISO8601 with milliseconds; rows whose created falls inside the requested range return created, rows matched only via lastModified return the modification time"}},
                        desc="Date slim row", additional=False))},
        desc="creation/dates search results (empty set = 404; this shape is not returned)", additional=False)
    sc["RepoTestInput"] = obj(
        {"url": {"type": "string", "description": "Draft URL (overrides the stored value for this probe only)"},
         "username": {"type": "string"},
         "password": {"type": "string", "description": "write-only; url/username changed without a password → anonymous probe"}},
        desc="Draft-override body for probing a remote repository's upstream (optional; empty body = probe using the stored configuration)")
    sc["ProbeOutcome"] = obj(
        {"ok": {"type": "boolean"}, "status_code": {"type": "integer"},
         "message": {"type": "string"}},
        desc="Connectivity probe verdict — ok:true returns 200 / ok:false returns 400 (same shape)", additional=False)
    sc["QRLSettings"] = obj(
        {"rlSettings": arr(obj({"rlType": {"type": "string", "enum": ["DEFAULT", "LOW_PRIORITY"]},
                                "permitsPerTimeFrame": {"type": "integer"},
                                "timeFrameMillis": {"type": "integer"},
                                "timeQuota": {"type": "integer"}},
                               desc="Rate-limit buckets (closed set of two types; all three values must be positive)", additional=False))},
        desc="Query rate limiter configuration (GET response shape)", additional=False)
    sc["QRLConfigInput"] = obj(
        {"mode": {"type": "string", "enum": ["disabled", "enabled", "simulation"],
                  "description": "BinFlow tri-state carrier — while disabled, requests without mode keep the 400 arm"},
         "rlSettings": arr(obj({"rlType": {"type": "string", "enum": ["DEFAULT", "LOW_PRIORITY"]},
                                "permitsPerTimeFrame": {"type": "integer"},
                                "timeFrameMillis": {"type": "integer"},
                                "timeQuota": {"type": "integer"}},
                               desc="Rate-limit buckets (closed set of two types; all three values must be positive)", additional=False))},
        desc="Query rate limiter merge-write body (both halves optional — merge semantics)")
    sc["UserSummary"] = obj(
        {"name": {"type": "string"}, "uri": {"type": "string"}, "realm": {"type": "string"},
         "source": {"type": "string"}, "email": {"type": "string"},
         "adminRole": {"type": "string", "description": "admin / readonly_admin / user"},
         "enabled": {"type": "boolean"}, "groups": arr({"type": "string"}, desc="Always rendered; empty group list is [] not null")},
        desc="User entry (same shape in list and single GET; no password fields)")
    sc["UserInput"] = obj(
        {"name": {"type": "string"}, "email": {"type": "string"}, "password": {"type": "string"},
         "admin": {"type": "boolean"}, "adminRole": {"type": "string"}, "groups": arr({"type": "string"}),
         "enabled": {"type": "boolean", "description": "Pointer semantics: explicit false disables login; absent leaves unchanged"}},
        desc="Create/update user body (adminRole writable by admins only)")
    sc["GroupDetail"] = obj(
        {"name": {"type": "string"}, "description": {"type": "string"},
         "userNames": arr({"type": "string"}, desc="Member-name array attached only with `?includeUsers=true` (empty group is [] never null)")},
        desc="Group detail (three fields without params; userNames attached when requested)")
    sc["TokenResponse"] = obj(
        {"access_token": {"type": "string", "description": "64-char hex (256-bit)"},
         "token_id": {"type": "string"}, "expires_in": {"type": "integer", "description": "Default 2592000 seconds (30 days)"},
         "scope": {"type": "string", "description": "`api:*`"}},
        desc="Token mint response", required=["access_token"], additional=False)
    sc["PermissionTarget"] = obj(
        {"name": {"type": "string"},
         "repos": arr({"type": "string"}),
         "principals": obj({"users": obj({}, desc="username → array of actions"),
                            "groups": obj({}, desc="group name → array of actions")},
                           desc="principals always echoes the canonical action names: read, deploy-cache, annotate, delete, manage (the write alias is accepted but never echoed)")},
        desc="Permission Target")
    sc["PermissionTargetInput"] = obj(
        {"name": {"type": "string"}, "repos": arr({"type": "string"}),
         "principals": obj({"users": obj({}, desc="username → array of actions"),
                            "groups": obj({}, desc="group name → array of actions")})},
        desc="Create body. The action set is a closed five-value set `read / deploy-cache / annotate / delete / manage`; `write` is still accepted as a compatibility alias for `deploy-cache` (does not imply annotate); manage holders can edit targets within their coverage set")
    sc["RepoConfig"] = obj(
        {"key": {"type": "string"}, "rclass": {"type": "string", "enum": ["local", "remote", "virtual"]},
         "packageType": {"type": "string"},
         "url": {"type": "string", "description": "Remote repository upstream URL (bare origin, without the /binflow suffix)"},
         "description": {"type": "string"},
         "username": {"type": "string"}, "password": {"type": "string"},
         "quotaBytes": {"type": "integer", "description": "Quota (0 = unlimited)"},
         "defaultDeploymentRepo": {"type": "string"},
         "unusedArtifactsCleanupPeriodHours": {"type": "integer"},
         "chartsBaseUrl": {"type": "string", "description": "Remote repos with packageType=helm only; absolute http(s) URL; carried on other package types → 400 naming this field"},
         "enableTokenAuthentication": {"type": "boolean", "description": "Remote repositories; when true the pull side sends Authorization: Bearer <password> to the upstream"},
         "contentSynchronisation": obj({"enabled": {"type": "boolean"},
                                        "propertiesEnabled": {"type": "boolean"},
                                        "statisticsEnabled": {"type": "boolean"},
                                        "sourceOrigin": {"type": "boolean"}})},
        desc="Repository configuration (reads and writes echo the canonical shape). The rclass × packageType combination gates are fully retired; the only remaining gate on repository creation is the license tier (advanced package types on lower tiers get 400 `package type not available on this instance: ...`)")
    sc["UsageRow"] = obj(
        {"repo": {"type": "string"}, "usedBytes": {"type": "integer"}, "quotaBytes": {"type": "integer"},
         "nodeCount": {"type": "integer", "description": "Only with `?include=counts`; counts file nodes only"},
         "updatedAt": {"type": "string", "description": "Only with `?include=counts`; time of the last repository configuration change (not the latest artifact time)"}},
        desc="Quota usage row", required=["repo", "usedBytes", "quotaBytes"], additional=False)
    sc["AuditResponse"] = obj(
        {"events": arr(obj({"id": {"type": "integer"}, "time": {"type": "string"},
                            "actor": {"type": "string"}, "action": {"type": "string"},
                            "repo": {"type": "string"}, "path": {"type": "string"},
                            "detail": {"type": "object", "description": "Optional additional context (e.g. {used, quota} for quota.exceeded)"}},
                           desc="Results in reverse chronological order (newest first)")),
         "nextCursor": {"type": "string", "description": "Cursor pagination (opaque; pass back to continue)"}},
        desc="Audit log response", required=["events"])
    sc["GCRequest"] = obj(
        {"apply": {"type": "boolean", "description": "false=dry-run (report only, delete nothing); true=actually execute"},
         "graceHours": {"type": "number", "description": "Grace period in hours (configured default 24); 0=no grace; negative or >876000 → 400"}},
        desc="GC trigger body")
    sc["GCResponse"] = obj(
        {"candidateCount": {"type": "integer"}, "candidateBytes": {"type": "integer"},
         "deletedCount": {"type": "integer", "description": "Always 0 on dry-run"}},
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
        desc="unused-cleanup report (dry-run by default)")
    sc["SystemSettings"] = obj(
        {"folder_download": obj({"enabled": {"type": "boolean"},
                                 "enabled_for_anonymous": {"type": "boolean"},
                                 "max_download_size_mb": {"type": "integer"},
                                 "max_files": {"type": "integer"},
                                 "max_concurrent_requests": {"type": "integer"},
                                 "enabled_empty_directories": {"type": "boolean"}}),
         "trashcan": obj({"retention_days": {"type": "integer"}})},
        desc="Resolved runtime knobs (YAML+env+defaults merged); knob-scoped — returns behavioral knobs only, never secrets/DSNs/paths; read-only (other verbs 404); the knobs themselves take effect on restart",
        additional=False)
    sc["MaintenanceSlot"] = obj(
        {"key": {"type": "string", "enum": ["gc", "cleanup-unused-cache", "cleanup-virtual"]},
         "cronExp": {"type": "string"}, "enabled": {"type": "boolean"},
         "nextRun": {"type": "string"}, "lastRun": {"type": "string"},
         "lastStatus": {"type": "string"}, "lastError": {"type": "string"}},
        desc="Maintenance cron slot status (no row = unscheduled, rendered default shape)")
    sc["MaintenanceProjection"] = obj(
        {"slots": arr(S("MaintenanceSlot"))}, required=["slots"], additional=False)
    sc["MaintenancePutSlot"] = obj(
        {"cronExp": {"type": "string", "description": "Empty string = delete this slot's schedule row (the only way to clear); non-empty is parse-validated (including a reachable future fire time)"},
         "enabled": {"type": "boolean", "description": "Defaults to true (a freshly entered schedule is meant to be active)"}},
        desc="Maintenance single-slot write arm")
    sc["MaintenancePutBody"] = obj(
        {"gc": S("MaintenancePutSlot"), "cleanup-unused-cache": S("MaintenancePutSlot"),
         "cleanup-virtual": S("MaintenancePutSlot")},
        desc="Each slot optional, absent means unchanged; an all-empty body → 400 `maintenance body carries no slot arm (gc, cleanup-unused-cache, cleanup-virtual)`; all arms are validated before anything is persisted (a bad expression does not half-apply)",
        additional=False)
    sc["BackupConfig"] = obj(
        {"backupKey": {"type": "string"}, "enabled": {"type": "boolean"},
         "exportPath": {"type": "string", "description": "Server-side absolute path (directory where backup artifacts are written)"},
         "cronExp": {"type": "string"}, "nextScheduleBackup": {"type": "string", "description": "Empty = unscheduled"},
         "lastRun": {"type": "string"}, "lastStatus": {"type": "string"}, "lastError": {"type": "string"},
         "createdAt": {"type": "string"}, "updatedAt": {"type": "string"}},
        desc="Backup configuration read shape (payload row + cron projection merged)")
    sc["BackupBody"] = obj(
        {"backupKey": {"type": "string", "description": "1..64 chars of letters/digits/./_/-, starting alphanumeric; in the {key} path form the path wins"},
         "enabled": {"type": "boolean", "description": "Defaults to true"},
         "cronExp": {"type": "string", "description": "Empty string = keep the payload row but do not schedule (schedule row deleted)"},
         "nextBackupTime": {"type": "string", "description": "RFC3339 and must be in the future; default computed from the expression"},
         "exportPath": {"type": "string", "description": "Required; absolute path without .. segments"}},
        desc="Backup configuration write body")
    sc["BackupsList"] = obj({"backups": arr(S("BackupConfig"))},
                            required=["backups"], additional=False)
    sc["ScheduleStatus"] = obj(
        {"domain": {"type": "string", "enum": ["maintenance", "backup", "replication"]},
         "key": {"type": "string"}, "cronExp": {"type": "string"}, "enabled": {"type": "boolean"},
         "nextRun": {"type": "string"}, "lastRun": {"type": "string"},
         "lastStatus": {"type": "string"}, "lastError": {"type": "string"}},
        desc="Cron ledger row projection (enabled = row enabled and expression non-empty)")
    sc["SchedulesList"] = obj({"schedules": arr(S("ScheduleStatus"))},
                              required=["schedules"], additional=False)
    sc["GlobalBlockState"] = obj(
        {"blockPullReplications": {"type": "boolean"}, "blockPushReplications": {"type": "boolean"}},
        desc="Global block state (canonical key shape)", additional=False)
    sc["ReplicationConfig"] = obj(
        {"id": {"type": "integer", "description": "Numeric id (immutable key; PUT enable/disable addresses by it)"},
         "name": {"type": "string", "description": "DELETE removes by name (both addressing forms coexist)"},
         "enabled": {"type": "boolean"},
         "updated_at": {"type": "string"}},
        desc="Replication configuration row (GET projection same shape; credential fields never echoed; whole-row round-trip fields tolerated on input)")
    sc["ReplicationRunResponse"] = obj(
        {"info": {"type": "string"}, "id": {"type": "integer"}, "name": {"type": "string"},
         "scheduled": {"type": "integer", "description": "Tasks seeded by this call (empty source repo = 0, a no-op run)"},
         "capped": {"type": "boolean", "description": "Truncated by max_items_per_push (call again to fetch the next batch)"}},
        additional=False)
    sc["ReplicationTestResponse"] = obj(
        {"ok": {"type": "boolean"}, "status_code": {"type": "integer"}, "message": {"type": "string"}},
        desc="Connectivity probe verdict (failures use the same shape with 400)", additional=False)
    sc["KeyPairSummary"] = obj(
        {"pairName": {"type": "string"}, "pairType": {"type": "string", "description": "GPG"},
         "alias": {"type": "string"}, "publicKey": {"type": "string"},
         "algorithm": {"type": "string"}, "createdAt": {"type": "string"},
         "updatedAt": {"type": "string"}, "updatedBy": {"type": "string"},
         "repositories": arr({"type": "string"})},
        desc="Key pair summary (private keys and passphrases never leave the store — no export endpoint)")
    sc["KeyPairInput"] = obj(
        {"pairName": {"type": "string"}, "pairType": {"type": "string"},
         "alias": {"type": "string"}, "privateKey": {"type": "string"},
         "publicKey": {"type": "string"}, "passphrase": {"type": "string"}},
        desc="Import/update body")
    sc["KeyPairGenerateInput"] = obj(
        {"pairName": {"type": "string"}, "alias": {"type": "string"}, "passphrase": {"type": "string"},
         "keyBits": {"type": "integer"}, "uidName": {"type": "string"},
         "uidComment": {"type": "string"}, "uidEmail": {"type": "string"}},
        desc="Server-side generation input (duplicate name 409)")
    sc["TestReport"] = obj(
        {"ok": {"type": "boolean"}, "phase": {"type": "string"},
         "category": {"type": "string"}, "message": {"type": "string"}},
        desc="Auth config test response; ok:false yields HTTP 400", additional=False)
    sc["AddonSlot"] = obj(
        {"id": {"type": "string"}, "kind": {"type": "string"}, "minTier": {"type": "string"},
         "enabled": {"type": "boolean"}, "reason": {"type": "string"},
         "displayName": {"type": "string"}, "description": {"type": "string"}},
        desc="Add-on slot (no write surface — other verbs 404)")
    sc["UploadSessionToken"] = obj({"token": {"type": "string"}},
                                   desc="Session capability credential", additional=False)
    sc["UploadConfig"] = obj({"supported": {"type": "boolean"}},
                             desc="Capability probe: true on the S3 stack / false on filestore (the probe endpoint never returns 501)", additional=False)
    sc["UploadUrl"] = obj({"url": {"type": "string"}},
                          desc="Part upload URL — the query string carries the ?token= capability, so the PUT may omit Authorization", additional=False)
    sc["UploadStatus"] = obj(
        {"status": {"type": "string", "enum": ["PARTS", "PROCESSING", "FINISHED", "NON_RETRYABLE_ERROR"]},
         "error": {"type": "string"}, "progress": {"type": "integer"},
         "checksumToken": {"type": "string", "description": "Returned when FINISHED; use it for a zero-transfer X-Checksum-Deploy PUT that lands the node"}},
        additional=False)
    sc["CopyMoveResult"] = obj(
        {"messages": arr(obj({"level": {"type": "string"}, "message": {"type": "string"},
                              "status": {"type": "integer", "description": "Overall status = the code of the last error message (409 fallback when none)"}}))},
        desc="Tree-level copy/move response (Content-Type is the vendor form application/vnd.org.jfrog.artifactory.storage.CopyOrMoveResult+json)")
    sc["TrashSummary"] = obj(
        {"removed": {"type": "integer"}, "files": {"type": "integer"},
         "folders": {"type": "integer"}, "bytes": {"type": "integer"}},
        additional=False)
    sc["SubscriptionRequest"] = obj(
        {"key": {"type": "string", "description": "^[A-Za-z][A-Za-z0-9_-]+$ ≤500"},
         "project_key": {"type": "string"}, "description": {"type": "string"},
         "enabled": {"type": "boolean", "description": "Defaults to false"},
         "event_filter": obj({"domain": {"type": "string"}, "event_types": arr({"type": "string"}),
                              "criteria": obj({}, desc="strict — unknown keys 400")}),
         "handlers": arr(obj({"url": {"type": "string"}, "type": {"type": "string"},
                              "use_secret_for_signing": {"type": "boolean"},
                              "secrets": obj({}, desc="secrets always masked as ********")}),
                         desc="Exactly 1; webhook or custom-webhook"),
         "debug": {"type": "boolean"}},
        desc="Single subscription shape (shared by create/update/test)")
    sc["SubscriptionView"] = obj(
        {"key": {"type": "string"}, "project_key": {"type": "string"}, "description": {"type": "string"},
         "enabled": {"type": "boolean"}, "event_filter": obj({}), "handlers": arr(obj({})),
         "debug": {"type": "boolean"}},
        desc="Subscription echo (secrets always masked as ********)")
    sc["TestOutcome"] = obj(
        {"ok": {"type": "boolean"},
         "attempt": obj({"status_code": {"type": "integer"}, "elapsed_millis": {"type": "integer"},
                         "error": {"type": "string"}})},
        desc="Test-send outcome — failures also return 200, inspect the body", additional=False)
    sc["SessionResponse"] = obj(
        {"username": {"type": "string"}, "admin": {"type": "boolean"},
         "adminRole": {"type": "string"}, "source": {"type": "string"}},
        desc="Login/whoami echo")
    return sc
