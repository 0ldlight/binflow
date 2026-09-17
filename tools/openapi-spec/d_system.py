# Domain: 系统端点 + license/addons + 维护面（maintenance/backups/schedules）+
# 查询限流配置（query_rate_limiter）+ uploads（MPU）+ replication + session。
# Source: docs/user/api-reference.md（系统端点/M10/M11/M13/M14/M15 增补速览）+
# internal/httpapi/system_{maintenance,backups,schedules,qrl}.go
# （维护面三族 + 查询限流三动词——契约页尚未覆盖）。

from helpers import (op, tag, q, pp, r, S, arr, body, obj,
                     ERR_401, ERR_403, ANON)

tag("system", "System — ping/version/license/addons/health/audit/GC/cleanup/settings")
tag("maintenance", "Maintenance — the three cron consumers (maintenance/backups/schedules ledgers)")
tag("uploads", "Multipart uploads (MPU) — data endpoints require a pure S3 backend; filestore/dual-write instances return 501 plain text, not 404")
tag("replication", "Push replication — configuration CRUD, Replicate Now, connectivity probes and the global block")
tag("session", "Console session (/api/v1/session)")


def build():
    # ---- 基础系统 ----
    op("/api/system/ping", "get", "systemPing", "system", "Liveness probe (no authentication)",
       "Canonical path: `GET /binflow/api/system/ping`.",
       responses={"200": r("OK", schema={"type": "string"}, example="OK", ctype="text/plain")},
       security=ANON)

    op("/api/system/version", "get", "systemVersion", "system", "Version info (no authentication)",
       "Canonical path: `GET /binflow/api/system/version` — returns the product name and build id only.",
       responses={"200": r("Version info", schema=obj({"version": {"type": "string"},
                                                   "revision": {"type": "string"}}))},
       security=ANON)

    op("/api/system/license", "get", "licenseGet", "system", "License status",
       "Canonical path: `GET /binflow/api/system/license` (CapSystemRead — admin/readonly_admin). "
       "The body never contains the certificate text/signature.",
       responses={"200": r("License status", schema=obj({}, desc="Fields in the License and Add-ons guide"))})

    op("/api/system/license", "post", "licenseInstall", "system", "Install a license",
       "Canonical path: `POST /binflow/api/system/license` (body = the certificate text; success **201**; "
       "a failed signature check **400** with wire code `LICENSE_EXPIRED`/`LICENSE_INVALID`, the current certificate "
       "untouched; CapSystemWrite).",
       req_body=body("License certificate text (text/plain)", schema={"type": "string"},
                     ctype="text/plain", required=True),
       responses={"201": r("Installed"), "400": r("Signature check rejected (wire codes in the description)")})

    op("/api/system/license", "delete", "licenseDelete", "system", "Uninstall the license",
       "Canonical path: `DELETE /binflow/api/system/license` (idempotent **200 plain text** "
       "`License removed successfully.`; CapSystemWrite; a downgrade does not hold data hostage).",
       responses={"200": r("License removed successfully. (plain text)", schema={"type": "string"},
                           example="License removed successfully.", ctype="text/plain")})

    op("/api/v1/addons", "get", "addonsList", "system", "Add-on slot list, evaluated live",
       "Canonical path: `GET /binflow/api/v1/addons` (bare array; CapSystemRead; **no write surface** — other verbs 404).",
       responses={"200": r("Slot list", schema=arr(S("AddonSlot"))),
                  "403": ERR_403})

    op("/api/v1/health", "get", "healthGet", "system", "Health dashboard",
       "Canonical path: `GET /binflow/api/v1/health` (admin / readonly_admin). "
       "Deployment probes should use the unauthenticated `/healthz` and `/readyz`.",
       responses={"200": r("Instance health details", schema=obj({}, desc="Health dashboard field family"))})

    op("/api/v1/storage/stats", "get", "storageStatsGet", "system", "Instance-wide storage statistics",
       "Canonical path: `GET /binflow/api/v1/storage/stats` (admin / readonly_admin) — instance-wide blob/byte "
       "statistics.",
       responses={"200": r("Storage statistics", schema=obj({}, desc="Instance-wide blob/byte statistics field family"))})

    op("/api/v1/storage/usage", "get", "storageUsageBatch", "system", "Batch quota usage",
       "Canonical path: `GET /binflow/api/v1/storage/usage?repos=&include=counts` (admin / readonly_admin; "
       "a regular user gets the subset of repositories they can `read` **or** `manage`; anonymous 401). **bare array** "
       "(no envelope, no pagination; an empty visible set is `200 []` never null). Naming an unknown repository and "
       "naming a repository without permission **silently omit the same way** (no existence signal). "
       "`?include=counts` attaches `nodeCount`/`updatedAt` (nodeCount counts file nodes only; updatedAt = time of the "
       "repository configuration change); unknown values are rejected explicitly: 400 "
       "`{\"errors\":[{\"status\":400,\"message\":\"include must be \\\"counts\\\" (unknown include value: \\\"bogus\\\")\"}]}`.",
       params=[q("repos", "Named repository set (unknown names silently omitted)"),
               q("include", "counts", schema={"type": "string", "enum": ["counts"]})],
       responses={"200": r("Usage rows (bare array)", schema=arr(S("UsageRow")),
                           example=[{"repo": "g-local", "usedBytes": 10, "quotaBytes": 0}]),
                  "400": r("Unknown include value", schema=S("ErrorsEnvelope"),
                           example={"errors": [{"status": 400,
                                                "message": "include must be \"counts\" (unknown include value: \"bogus\")"}]}),
                  "401": ERR_401})

    op("/api/v1/storage/usage/{repo}", "get", "storageUsageOne", "system", "Single-repository quota usage",
       "Canonical path: `GET /binflow/api/v1/storage/usage/{repo}` (admin / readonly_admin / anyone granted `read` "
       "**or** `manage` on the repository). Row shape `{\"repo\",\"usedBytes\",\"quotaBytes\"}` (field-for-field "
       "isomorphic with the batch endpoint).",
       params=[pp("repo", "Repository key")],
       responses={"200": r("Usage row", schema=S("UsageRow")),
                  "403": ERR_403})

    op("/api/v1/audit", "get", "auditQuery", "system", "Audit log query",
       "Canonical path: `GET /binflow/api/v1/audit` (admin / readonly_admin; some faces are admin-only). "
       "Results in **reverse chronological order** (newest first); `limit` defaults to 100, caps at 1000 (over → 400).",
       params=[q("repo", "Filter by repository (exact match)"),
               q("actor", "Filter by actor (exact match)"),
               q("action", "Filter by action (exact match; see the audit vocabulary)"),
               q("since", "Start time (inclusive, RFC3339)"),
               q("until", "End time (exclusive, RFC3339)"),
               q("limit", "Default 100, cap 1000 (over → 400)", schema={"type": "integer"}),
               q("cursor", "Cursor pagination (opaque; pass nextCursor back)")],
       responses={"200": r("Audit events", schema=S("AuditResponse"),
                           example={"events": [{"id": 42, "time": "2026-08-21T12:34:56.789Z",
                                                "actor": "admin", "action": "deploy",
                                                "repo": "generic-local", "path": "a/b/w.bin",
                                                "detail": None}],
                                    "nextCursor": "43"}),
                  "400": r("limit over the cap"),
                  "403": ERR_403})

    op("/api/v1/system/gc", "post", "systemGC", "system", "Trigger GC",
       "Canonical path: `POST /binflow/api/v1/system/gc` (admin only; readonly_admin 403 — runs synchronously, "
       "dry-run/apply; body below). There is no GET route — query past runs through the audit `gc.run` events.",
       req_body=body("GC parameters", schema=S("GCRequest"),
                     example={"apply": False, "graceHours": 24}),
       responses={"200": r("GC result", schema=S("GCResponse"),
                           example={"candidateCount": 5, "candidateBytes": 204800, "deletedCount": 0}),
                  "403": ERR_403})

    op("/api/v1/system/cleanup", "post", "cleanupRun", "system", "Manual unused-cleanup trigger",
       "Canonical path: `POST /binflow/api/v1/system/cleanup` (admin only). "
       "Body `{\"apply\":bool,\"repo\":string?}` — **dry-run by default**; runs synchronously and returns a "
       "CleanupReport. The engine has three legs (one maintenance lock, mutually exclusive with gc/export/import); "
       "when `audit.enabled=false` the policy leg refuses to run (without download traces there is no honest "
       "\"unused\" — better not to delete), the session/gc legs still run.",
       req_body=body("Trigger parameters", schema=obj({"apply": {"type": "boolean", "description": "false=dry-run, the default"},
                                             "repo": {"type": "string", "description": "Limit to a single repository (optional)"}})),
       responses={"200": r("CleanupReport", schema=S("CleanupReport")),
                  "403": ERR_403})

    op("/api/v1/system/cleanup", "get", "cleanupStatus", "system", "unused-cleanup status face",
       "Canonical path: `GET /binflow/api/v1/system/cleanup` (system:read). "
       "Cron cadence, cumulative counters, the last report, and per-remote-repository policy rows.",
       responses={"200": r("Status face", schema=obj({}, desc="Cron cadence/cumulative counters/last report/policy rows"))})

    op("/api/v1/system/settings", "get", "systemSettingsGet", "system", "Runtime knobs echo",
       "Canonical path: `GET /binflow/api/v1/system/settings` (system:read — admin / readonly_admin). "
       "Echoes the **resolved** runtime knobs (YAML+env+defaults merged). Knob-scoped by design — behavioral knobs "
       "only, never secrets/DSNs/paths; read-only (other verbs 404); the knobs themselves take effect on restart.",
       responses={"200": r("Knobs echo", schema=S("SystemSettings"),
                           example={"folder_download": {"enabled": False, "enabled_for_anonymous": False,
                                                        "max_download_size_mb": 1024, "max_files": 5000,
                                                        "max_concurrent_requests": 10,
                                                        "enabled_empty_directories": False},
                                    "trashcan": {"retention_days": 14}})})

    # ---- 查询限流配置（query_rate_limiter）----
    op("/api/v1/system/query_rate_limiter/config", "get", "qrlConfigGet", "system",
       "Read the query rate limiter configuration",
       "Canonical path: `GET /binflow/api/v1/system/query_rate_limiter/config` (system:read — "
       "admin / readonly_admin). Returns `{\"rlSettings\":[…]}` — two buckets `rlType: DEFAULT | LOW_PRIORITY`, "
       "each a triple `{permitsPerTimeFrame, timeFrameMillis, timeQuota}`; never-written instances return the default "
       "buckets (a constant snapshot of the query gate). While the limiter is **disabled, all three verbs return 400 "
       "plain text** `Query rate limiter is disabled` (except a POST carrying an explicit mode — see POST). The "
       "configuration lives for the process lifetime: a restart returns to factory defaults (no DB persistence). "
       "Rate limiting only delays, never rejects — the 429/408 gates are orthogonal to this configuration.",
       responses={"200": r("Effective configuration", schema=S("QRLSettings"),
                           example={"rlSettings": [
                               {"rlType": "DEFAULT", "permitsPerTimeFrame": 4,
                                "timeFrameMillis": 10000, "timeQuota": 1000},
                               {"rlType": "LOW_PRIORITY", "permitsPerTimeFrame": 4,
                                "timeFrameMillis": 10000, "timeQuota": 1000}]}),
                  "400": r("Limiter disabled", schema={"type": "string"},
                           example="Query rate limiter is disabled", ctype="text/plain"),
                  "403": ERR_403})

    op("/api/v1/system/query_rate_limiter/config", "post", "qrlConfigPost", "system",
       "Merge-write the query rate limiter configuration",
       "Canonical path: `POST /binflow/api/v1/system/query_rate_limiter/config` (system:write — admin only). "
       "Merge write: fields not carried by the request keep their current values. The body may include `\"mode\"` "
       "(the BinFlow tri-state carrier: `disabled | enabled | simulation` — unknown names 400 envelope "
       "`unknown mode \"…\" (expected one of disabled, enabled, simulation)`) and `rlSettings[]` "
       "(rlType closed set of two values, the triple must be all positive — violations 400 envelope "
       "`invalid query rate limiter setting`). While disabled, requests without mode keep the 400 plain-text arm; "
       "success is **200 plain text verbatim** `Query rate limiter configuration was updated successfully`.",
       req_body=body("Merge write (both halves optional)", schema=S("QRLConfigInput"),
                     example={"mode": "enabled",
                              "rlSettings": [{"rlType": "DEFAULT", "permitsPerTimeFrame": 8,
                                              "timeFrameMillis": 10000, "timeQuota": 1000}]}),
       responses={"200": r("Updated (plain text)", schema={"type": "string"},
                           example="Query rate limiter configuration was updated successfully",
                           ctype="text/plain"),
                  "400": r("Validation failed / disabled arm", schema={"type": "string"},
                           example="Query rate limiter is disabled", ctype="text/plain"),
                  "403": ERR_403})

    op("/api/v1/system/query_rate_limiter/config", "delete", "qrlConfigDelete", "system",
       "Reset the query rate limiter configuration to factory defaults",
       "Canonical path: `DELETE /binflow/api/v1/system/query_rate_limiter/config` (system:write — admin only). "
       "Restores the factory state (disabled + default buckets); a limiter that is already disabled still gets the "
       "disabled 400 arm (the \"feature off\" check is uniform). Success is **200 plain text verbatim** "
       "`Query rate limiter configuration was deleted successfully`.",
       responses={"200": r("Reset to factory defaults (plain text)", schema={"type": "string"},
                           example="Query rate limiter configuration was deleted successfully",
                           ctype="text/plain"),
                  "400": r("Limiter disabled", schema={"type": "string"},
                           example="Query rate limiter is disabled", ctype="text/plain"),
                  "403": ERR_403})

    # ---- 维护面（maintenance / backups / schedules）----
    op("/api/v1/system/maintenance", "get", "maintenanceGet", "maintenance",
       "Read the three maintenance cron slots",
       "Canonical path: `GET /binflow/api/v1/system/maintenance` (system:read). "
       "The cron + next-run + last-run projection of the three slots `gc` / `cleanup-unused-cache` / "
       "`cleanup-virtual` (no row = unscheduled, rendered default shape). The manual face (Run Now) remains "
       "`POST /api/v1/system/gc` and `/cleanup`.",
       responses={"200": r("Slot projection", schema=S("MaintenanceProjection"))})

    op("/api/v1/system/maintenance", "put", "maintenancePut", "maintenance",
       "Write maintenance crons",
       "Canonical path: `PUT /binflow/api/v1/system/maintenance` (system:write — admin only). "
       "Each slot optional (absent means unchanged); an empty-string `cronExp` = delete that slot's schedule row "
       "(the only way to clear); `enabled` defaults to true. All arms are validated before anything is persisted "
       "(a bad expression does not half-apply). Invalid expressions 400 "
       "`Invalid cronExp <expr> for <slot>: <reason>`.",
       req_body=body("Slot write arms (at least one)", schema=S("MaintenancePutBody"),
                     example={"gc": {"cronExp": "0 3 * * *", "enabled": True}}),
       responses={"200": r("Post-write projection", schema=S("MaintenanceProjection")),
                  "400": r("No slot arm / bad expression (message in the description)"),
                  "403": ERR_403})

    op("/api/v1/system/backups", "get", "backupsList", "maintenance", "List backup configurations",
       "Canonical path: `GET /binflow/api/v1/system/backups` (system:read). "
       "Each payload row merged with its cron projection, in key order.",
       responses={"200": r("Backup configuration list", schema=S("BackupsList"))})

    op("/api/v1/system/backups", "put", "backupsUpsert", "maintenance", "Upsert a backup configuration (body-key form)",
       "Canonical path: `PUT /binflow/api/v1/system/backups` (system:write — admin only). "
       "The single official PUT form: the key comes from the body's `backupKey`. `exportPath` is required "
       "(server-side absolute path, no `..` segments); an empty-string `cronExp` = keep the payload row but do not "
       "schedule. Scheduled backups only — `/api/export/**` stays 404 and restore is CLI-only.",
       req_body=body("Backup configuration", schema=S("BackupBody"),
                     example={"backupKey": "nightly", "enabled": True, "cronExp": "0 2 * * *",
                              "exportPath": "/var/backups/binflow"}),
       responses={"200": r("Backup configuration read shape", schema=S("BackupConfig")),
                  "400": r("key/path/cron validation rejection (messages verbatim)"),
                  "403": ERR_403})

    op("/api/v1/system/backups/{key}", "get", "backupGet", "maintenance", "Get one backup configuration",
       "Canonical path: `GET /binflow/api/v1/system/backups/{key}` (system:read). Unknown name 404 "
       "`backup not found: <key>`.",
       params=[pp("key", "Backup key")],
       responses={"200": r("Backup configuration read shape", schema=S("BackupConfig")),
                  "404": r("backup not found: <key>")})

    op("/api/v1/system/backups/{key}", "put", "backupUpsertByKey", "maintenance",
       "Upsert a backup configuration (by-key alias form)",
       "Canonical path: `PUT /binflow/api/v1/system/backups/{key}` (system:write). When a path key and a body key "
       "coexist, **the path wins**.",
       params=[pp("key", "Backup key (path addressing)")],
       req_body=body("Backup configuration", schema=S("BackupBody")),
       responses={"200": r("Backup configuration read shape", schema=S("BackupConfig")),
                  "400": r("Validation rejection"), "403": ERR_403})

    op("/api/v1/system/backups/{key}", "delete", "backupDelete", "maintenance",
       "Delete a backup configuration",
       "Canonical path: `DELETE /binflow/api/v1/system/backups/{key}` (system:write). "
       "Deletes the payload row and the schedule row together (**204**).",
       params=[pp("key", "Backup key")],
       responses={"204": r("Deleted (no body)"),
                  "404": r("backup not found: <key>"), "403": ERR_403})

    op("/api/v1/system/schedules", "get", "schedulesList", "maintenance",
       "Read-only cron ledger projection",
       "Canonical path: `GET /binflow/api/v1/system/schedules?domain=` (system:read). "
       "Every domain, or one closed-set domain (maintenance/backup/replication); unknown domain values 400 "
       "`domain must be one of maintenance, backup, replication (or omitted for every domain)`. "
       "Writes exist only on the three configuration faces.",
       params=[q("domain", "maintenance | backup | replication",
                 schema={"type": "string", "enum": ["maintenance", "backup", "replication"]})],
       responses={"200": r("Ledger projection", schema=S("SchedulesList")),
                  "400": r("Unknown domain value (message in the description)")})

    # ---- uploads（MPU）----
    op("/api/v1/uploads/create", "post", "uploadsCreate", "uploads", "Open a session",
       "Canonical path: `POST /binflow/api/v1/uploads/create?repoKey=&repoPath=&partSizeMB=`. "
       "Query params, not a JSON body; requires authentication + the admin/user role + `w` on the target repository; "
       "virtual repositories fall back to defaultDeploymentRepo; any package type. **200 `{\"token\": ...}`** — the "
       "session capability credential. Sessions survive restarts (upload_sessions rows).",
       params=[q("repoKey", "Target repository", required=True),
               q("repoPath", "Target path", required=True),
               q("partSizeMB", "Part size", schema={"type": "integer"})],
       responses={"200": r("Session token", schema=S("UploadSessionToken")),
                  "501": r("Not a pure S3 backend (filestore/dual-write instances, plain text 501)",
                           schema={"type": "string"}, ctype="text/plain")})

    op("/api/v1/uploads/config", "get", "uploadsConfig", "uploads", "Capability probe",
       "Canonical path: `GET /binflow/api/v1/uploads/config`. **200 `{\"supported\": bool}`** "
       "(true on the S3 stack / false on filestore — the probe endpoint never returns 501); gated by the "
       "jfrog-cli-go UA version (below 2.62.2 returns false).",
       responses={"200": r("Capability probe", schema=S("UploadConfig"))})

    op("/api/v1/uploads/urlPart", "post", "uploadsUrlPart", "uploads", "Get the part-n upload URL",
       "Canonical path: `POST /binflow/api/v1/uploads/urlPart?partNumber=N` (Bearer session token). "
       "**200 `{\"url\": ...}`** — the URL's query string carries the `?token=` capability, so the PUT may omit "
       "Authorization.",
       params=[q("partNumber", "Part number", required=True, schema={"type": "integer"})],
       responses={"200": r("Part URL", schema=S("UploadUrl"))})

    op("/api/v1/uploads/status", "post", "uploadsStatus", "uploads", "Async job progress",
       "Canonical path: `POST /binflow/api/v1/uploads/status` (Bearer). "
       "status ∈ PARTS/PROCESSING/**FINISHED**(progress 100 + checksumToken)/NON_RETRYABLE_ERROR.",
       responses={"200": r("Job progress", schema=S("UploadStatus"))})

    op("/api/v1/uploads/complete", "post", "uploadsComplete", "uploads", "Submit for assembly",
       "Canonical path: `POST /binflow/api/v1/uploads/complete?sha1=` (Bearer; **sha1**, 40 hex, required) → "
       "**202 accepted**, the job is asynchronous; a mismatch surfaces through status as NON_RETRYABLE_ERROR. "
       "Once finished, use the checksumToken returned by status for a zero-transfer `X-Checksum-Deploy` PUT that "
       "lands the node (the client lands the node; the server only assembles + registers the blob).",
       params=[q("sha1", "40 hex", required=True)],
       responses={"202": r("Accepted (poll status)")})

    op("/api/v1/uploads/abort", "post", "uploadsAbort", "uploads", "Abort a session",
       "Canonical path: `POST /binflow/api/v1/uploads/abort` (Bearer) → 204.",
       responses={"204": r("Aborted (no body)")})

    op("/api/v1/uploads/part/{id}/{partNumber}", "put", "uploadsPart", "uploads", "Upload a part",
       "Canonical path: `PUT /binflow/api/v1/uploads/part/{id}/{n}?token=` (the urlPart target). "
       "**200** in S3 PutObject shape; parts may arrive out of order — a bounded reorder buffer holds them; the "
       "server relays into S3 multipart.",
       params=[pp("id", "Session id"), pp("partNumber", "Part number", ),
               q("token", "Session capability token (the query-string capability returned by urlPart)", required=True)],
       req_body=body("Part bytes", schema={"type": "string", "format": "binary"}),
       responses={"200": r("Received")})

    # ---- replication ----
    op("/api/v1/replications", "get", "replicationList", "replication", "List replication configurations",
       "Canonical path: `GET /binflow/api/v1/replications` (system:read — readable by readonly_admin). "
       "bare array; credential fields are never echoed.",
       responses={"200": r("Configuration list", schema=arr(S("ReplicationConfig")))})

    op("/api/v1/replications", "post", "replicationCreate", "replication", "Create a replication configuration",
       "Canonical path: `POST /binflow/api/v1/replications` (system:write — admin only). "
       "**201** echoes the configuration row; `enabled` defaults to true; duplicate names 409, unknown source "
       "repositories 400 naming the key. **target_url takes the target instance's bare origin** "
       "(e.g. `http://target.example:8080`), **without** the `/binflow` suffix (including it produces "
       "`/binflow/binflow/…`).",
       req_body=body("Replication configuration", schema=obj(
           {"name": {"type": "string"}, "target_url": {"type": "string"},
            "target_repo": {"type": "string"}, "target_username": {"type": "string"},
            "target_password": {"type": "string", "description": "Sealed after write, never echoed"},
            "enabled": {"type": "boolean", "description": "Defaults to true"},
            "cronExp": {"type": "string", "description": "Scheduled full sync (empty = event-driven only)"},
            "max_items_per_push": {"type": "integer"}},
           desc="Full fields and engine behavior in the governance guide · replication"),
           required=True),
       responses={"201": r("Created (echoes the configuration row)", schema=S("ReplicationConfig")),
                  "400": r("Unknown source repository (names the key)"), "409": r("Duplicate name")})

    op("/api/v1/replications/{key}", "put", "replicationUpdateEnabled", "replication",
       "Enable/disable a replication configuration",
       "Canonical path: `PUT /binflow/api/v1/replications/{id}` (system:write — admin only). "
       "`{id}` = the **numeric id** at the head of the list row (the immutable key; DELETE goes by name — both "
       "addressing forms coexist). The body `{\"enabled\":true|false}` is required, **other fields are parsed but "
       "ignored** (a whole-row round-trip is not rejected); **200** echoes the updated configuration row (same shape "
       "as the GET projection, `updated_at` refreshed, sealed credentials kept as-is). After disabling, new artifacts "
       "stop enqueueing and in-flight tasks run to their own conclusion; after re-enabling, the backlog drains on the "
       "next sweep — no restart needed. Errors: anonymous 401 / non-admin 403 / unknown id 404 "
       "`replication config not found: <id>` / non-numeric id 400 / missing enabled 400.",
       params=[pp("key", "Numeric id (immutable key)")],
       req_body=body("Enable/disable", schema=obj({"enabled": {"type": "boolean"}}, additional=False),
                     example={"enabled": False}, required=True),
       responses={"200": r("Updated configuration row", schema=S("ReplicationConfig")),
                  "400": r("Non-numeric id / missing enabled"), "404": r("replication config not found: <id>")})

    op("/api/v1/replications/{key}/run", "post", "replicationRun", "replication",
       "Replicate Now — trigger a full sync",
       "Canonical path: `POST /binflow/api/v1/replications/{id}/run` (system:write — admin only). "
       "Seeds one reconciliation run from the stored configuration; **returns upon scheduling, does not wait for "
       "replication**. Repeated triggers **are not deduplicated** (200 seeds again; the target side converges "
       "idempotently by sha256); a disabled configuration → **409** (run `PUT enabled=true` first); push blocked → "
       "**409** (anchored message `Push replication is blocked, skipping replication` plus unlocking pointers); "
       "unknown id 404.",
       params=[pp("key", "Numeric id")],
       responses={"200": r("Scheduling accepted", schema=S("ReplicationRunResponse"),
                           example={"info": "The replication tasks was successfully scheduled to run",
                                    "id": 1, "name": "push-b", "scheduled": 5, "capped": False}),
                  "409": r("Disabled configuration / push blocked (message in the description)"),
                  "404": r("Unknown id")})

    op("/api/v1/replications/{key}/test", "post", "replicationTest", "replication",
       "Probe a stored configuration's target connectivity",
       "Canonical path: `POST /binflow/api/v1/replications/{id}/test` (system:write — admin only). "
       "Probes `GET {target_url}/binflow/api/storage/{target_repo}` (with the stored sealed credentials); "
       "the optional body `{target_url/target_repo/target_username/target_password}` overrides field by field "
       "(a changed URL/username without a password → probe anonymously, the old secret is never sent). "
       "**ok verdict body**: pass 200; failure **same shape with 400** (`ok:false` + the target status code/reason "
       "inline). Zero side effects, **ignores the block state**.",
       params=[pp("key", "Numeric id")],
       req_body=body("Field-by-field override (optional)", schema=obj(
           {"target_url": {"type": "string"}, "target_repo": {"type": "string"},
            "target_username": {"type": "string"}, "target_password": {"type": "string"}})),
       responses={"200": r("Probe passed", schema=S("ReplicationTestResponse"),
                           example={"ok": True, "status_code": 200,
                                    "message": "Push replication target url 'http://127.0.0.1:18502/binflow/api/storage/mirror-b' tested successfully"}),
                  "400": r("Probe failed (same shape, ok:false)", schema=S("ReplicationTestResponse"),
                           example={"ok": False, "status_code": 401,
                                    "message": "Connection failed: Target replication URL returned error 401: {…invalid credentials…}"})})

    op("/api/v1/replications/test", "post", "replicationTestDraft", "replication",
       "Draft face without an id — test a candidate before saving",
       "Canonical path: `POST /binflow/api/v1/replications/test` (system:write — admin only). "
       "The body is required (`{target_url,target_repo,target_username?,target_password?}`). "
       "A target on the same instance → `ok:false` (`Cannot replicate to the same instance: …`); "
       "a target ending in `-cache` → the official message `Replication to remote cache repositories are not "
       "allowed.`",
       req_body=body("Draft probe target", schema=obj(
           {"target_url": {"type": "string"}, "target_repo": {"type": "string"},
            "target_username": {"type": "string"}, "target_password": {"type": "string"}},
           required=["target_url", "target_repo"]), required=True),
       responses={"200": r("Verdict body", schema=S("ReplicationTestResponse")),
                  "400": r("Same shape with 400 (see description)", schema=S("ReplicationTestResponse"))})

    op("/api/v1/replications/{key}", "delete", "replicationDelete", "replication",
       "Delete a replication configuration by name",
       "Canonical path: `DELETE /binflow/api/v1/replications/{name}` (system:write — admin only; **by name**). "
       "The task ledger is cascaded clean; **204** no body; deleting again 404.",
       params=[pp("key", "Configuration name (DELETE by name — coexists with PUT's numeric id addressing)")],
       responses={"204": r("Deleted (no body)"), "404": r("Deleting again 404")})

    op("/api/v1/replication/status", "get", "replicationStatus", "replication",
       "Replication dashboard payload",
       "Canonical path: `GET /binflow/api/v1/replication/status` (system:read — readable by readonly_admin). "
       "`targets[]` (a per-configuration task-count row) + `events[]` (recent tasks merged across configurations; "
       "`?limit=` 1..500, default 50).",
       params=[q("limit", "1..500, default 50", schema={"type": "integer"})],
       responses={"200": r("Dashboard payload", schema=obj({"targets": arr(obj({}, desc="Per-configuration task-count row")),
                                                   "events": arr(obj({}, desc="Recent tasks merged"))}))})

    op("/api/v1/system/replications", "get", "replicationGlobalBlockGet", "replication",
       "Global block state",
       "Canonical path: `GET /binflow/api/v1/system/replications` (system:read). "
       "Canonical key shape `{\"blockPullReplications\":bool,\"blockPushReplications\":bool}`.",
       responses={"200": r("Block state", schema=S("GlobalBlockState"),
                           example={"blockPullReplications": False, "blockPushReplications": True})})

    for verb, word in (("block", "block"), ("unblock", "unblock")):
        op("/api/v1/system/replications/%s" % verb, "post",
           "replicationGlobal%s" % verb.capitalize(), "replication",
           "Emergency brake — %s" % word,
           "Canonical path: `POST /binflow/api/v1/system/replications/%s?push=&pull=` (system:write — admin only). "
           "The `push`/`pull` query parameters pick directions (omitted = the direction's action; **any string other "
           "than `\"true\"` = no-op for this call**); the response is **text/plain** with the official message "
           "(`Successfully blocked all replications, no replication will be triggered.` / the single-direction "
           "variants / both no-op `No action taken.`). Idempotent, persisted on write (survives restarts); "
           "**does not gate the configuration face** (CRUD/enable-disable/list work as usual while blocked). "
           "The block covers: the event track (new artifacts enqueue zero) + the claim track (in-flight sends stop, "
           "retry counters kept) + manual triggers (run 409) + pull-side origin fetches (miss 404 naming the block)."
           % verb,
           params=[q("push", "Direction selection (omitted = the action; any string other than true = no-op for this call)",
                     schema={"type": "string", "enum": ["true"]}),
                   q("pull", "Direction selection (omitted = the action; any string other than true = no-op for this call)",
                     schema={"type": "string", "enum": ["true"]})],
           responses={"200": r("Official message (text/plain)", schema={"type": "string"},
                               example="Successfully unblocked all replications.", ctype="text/plain")})

    # ---- session ----
    op("/api/v1/session", "post", "sessionCreate", "session", "Log in",
       "Canonical path: `POST /binflow/api/v1/session` (JSON or form; no authentication). "
       "The login entry is exempt from invalid-cookie rejection (immune to cookie tossing). "
       "The response sets `Set-Cookie: binflow_session=<id>; HttpOnly; Path=/binflow; SameSite=Lax`. "
       "Session TTL defaults to 24 hours and **activity does not extend it** (sliding renewal is swallowed by the "
       "absolute TTL cap). CSRF: non-GET/HEAD writes authenticated by the session cookie that carry a cross-origin "
       "`Origin` header → **403** (Basic/token are naturally immune).",
       req_body=body("Credentials", schema=obj({"username": {"type": "string"},
                                         "password": {"type": "string"}},
                                        required=["username", "password"], additional=False),
                     example={"username": "admin", "password": "<password>"}, required=True),
       responses={"200": r("Login echo (including adminRole and source)", schema=S("SessionResponse"),
                           example={"username": "admin", "admin": True}),
                  "401": ERR_401},
       security=ANON)

    op("/api/v1/session", "get", "sessionWhoami", "session", "Whoami (current session info)",
       "Canonical path: `GET /binflow/api/v1/session`.",
       responses={"200": r("Current session info", schema=S("SessionResponse")),
                  "401": ERR_401})

    op("/api/v1/session", "delete", "sessionDelete", "session", "Log out (revoke the session)",
       "Canonical path: `DELETE /binflow/api/v1/session` (revoked server-side; replaying the same cookie after "
       "expiry → 401).",
       responses={"204": r("Logged out (no body)"), "401": ERR_401})
