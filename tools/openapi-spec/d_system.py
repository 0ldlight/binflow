# Domain: 系统端点 + license/addons + 维护面（maintenance/backups/schedules）+
# 查询限流配置（query_rate_limiter）+ uploads（MPU）+ replication + session。
# Source: docs/user/api-reference.md（系统端点/M10/M11/M13/M14/M15 增补速览）+
# internal/httpapi/system_{maintenance,backups,schedules,qrl}.go
# （维护面三族 + 查询限流三动词——契约页尚未覆盖）。

from helpers import (op, tag, q, pp, r, S, arr, body, obj,
                     ERR_401, ERR_403, ANON)

tag("system", "系统端点——ping/version/license/addons/health/审计/GC/cleanup/settings")
tag("maintenance", "维护面——cron 三消费面（maintenance/backups/schedules 台账）")
tag("uploads", "分片上传（MPU）——数据端点仅纯 S3 后端；filestore/双写实例回 501 纯文本非 404")
tag("replication", "push 复制——配置 CRUD、Replicate Now、连通探测与全局封锁")
tag("session", "控制台会话（/api/v1/session）")


def build():
    # ---- 基础系统 ----
    op("/api/system/ping", "get", "systemPing", "system", "存活探测（免认证）",
       "官方拼写：`GET /binflow/api/system/ping`。",
       responses={"200": r("OK", schema={"type": "string"}, example="OK", ctype="text/plain")},
       security=ANON)

    op("/api/system/version", "get", "systemVersion", "system", "版本信息（免认证）",
       "官方拼写：`GET /binflow/api/system/version`——只回产品名与 build id。",
       responses={"200": r("版本信息", schema=obj({"version": {"type": "string"},
                                                   "revision": {"type": "string"}}))},
       security=ANON)

    op("/api/system/license", "get", "licenseGet", "system", "license 状态查询",
       "官方拼写：`GET /binflow/api/system/license`（CapSystemRead——admin/readonly_admin）。"
       "body 永不含文档原文/签名。",
       responses={"200": r("license 状态", schema=obj({}, desc="字段见 License 与 Add-ons 管理指南"))})

    op("/api/system/license", "post", "licenseInstall", "system", "安装 license",
       "官方拼写：`POST /binflow/api/system/license`（body = 文档原文；成功 **201**；"
       "验签拒 **400**，wire 码 `LICENSE_EXPIRED`/`LICENSE_INVALID`，现证不动；CapSystemWrite）。",
       req_body=body("license 文档原文（text/plain）", schema={"type": "string"},
                     ctype="text/plain", required=True),
       responses={"201": r("已安装"), "400": r("验签拒绝（wire 码见描述）")})

    op("/api/system/license", "delete", "licenseDelete", "system", "卸载 license",
       "官方拼写：`DELETE /binflow/api/system/license`（幂等 **200 纯文本** `License removed successfully.`；"
       "CapSystemWrite；降级不劫持数据）。",
       responses={"200": r("License removed successfully.（纯文本）", schema={"type": "string"},
                           example="License removed successfully.", ctype="text/plain")})

    op("/api/v1/addons", "get", "addonsList", "system", "addon 槽位清单实时求值",
       "官方拼写：`GET /binflow/api/v1/addons`（bare array；CapSystemRead；**无写面**——其余动词 404）。",
       responses={"200": r("槽位清单", schema=arr(S("AddonSlot"))),
                  "403": ERR_403})

    op("/api/v1/health", "get", "healthGet", "system", "健康面板",
       "官方拼写：`GET /binflow/api/v1/health`（admin / readonly_admin）。"
       "部署探针请用免认证的 `/healthz` 与 `/readyz`。",
       responses={"200": r("实例健康详情", schema=obj({}, desc="健康面板字段族"))})

    op("/api/v1/storage/stats", "get", "storageStatsGet", "system", "全实例存储统计",
       "官方拼写：`GET /binflow/api/v1/storage/stats`（admin / readonly_admin）——全实例 blob/字节统计。",
       responses={"200": r("存储统计", schema=obj({}, desc="全实例 blob/字节统计字段族"))})

    op("/api/v1/storage/usage", "get", "storageUsageBatch", "system", "批量配额用量",
       "官方拼写：`GET /binflow/api/v1/storage/usage?repos=&include=counts`（admin / readonly_admin；"
       "普通 user = 对该仓有 `read` **或** `manage` 的子集；匿名 401）。**bare array**（无信封、无分页；"
       "空可见集 `200 []` 恒非 null）。点名未知名的仓与点名无权限的仓**同形静默缺失**（无存在性信号）。"
       "`?include=counts` 附 `nodeCount`/`updatedAt`（nodeCount 只计文件 node；updatedAt = 仓库配置变更时刻）；"
       "未知值显式拒绝：400 "
       "`{\"errors\":[{\"status\":400,\"message\":\"include must be \\\"counts\\\" (unknown include value: \\\"bogus\\\")\"}]}`。",
       params=[q("repos", "点名仓集（未知名静默缺失）"),
               q("include", "counts", schema={"type": "string", "enum": ["counts"]})],
       responses={"200": r("用量行（bare array）", schema=arr(S("UsageRow")),
                           example=[{"repo": "g-local", "usedBytes": 10, "quotaBytes": 0}]),
                  "400": r("未知 include 值", schema=S("ErrorsEnvelope"),
                           example={"errors": [{"status": 400,
                                                "message": "include must be \"counts\" (unknown include value: \"bogus\")"}]}),
                  "401": ERR_401})

    op("/api/v1/storage/usage/{repo}", "get", "storageUsageOne", "system", "单仓配额用量",
       "官方拼写：`GET /binflow/api/v1/storage/usage/{repo}`（admin / readonly_admin / 对该仓有 `read` "
       "**或** `manage` 授权者）。行形 `{\"repo\",\"usedBytes\",\"quotaBytes\"}`（与批量端点逐字段同构）。",
       params=[pp("repo", "仓库 key")],
       responses={"200": r("用量行", schema=S("UsageRow")),
                  "403": ERR_403})

    op("/api/v1/audit", "get", "auditQuery", "system", "审计日志查询",
       "官方拼写：`GET /binflow/api/v1/audit`（admin / readonly_admin；admin only 见契约页门注）。"
       "结果按**时间倒序**（最新在前）；`limit` 默认 100、上限 1000（超限 400）。",
       params=[q("repo", "按仓库等值过滤"),
               q("actor", "按操作者等值过滤"),
               q("action", "按动作等值过滤（见审计词表）"),
               q("since", "起始时间（闭，RFC3339）"),
               q("until", "结束时间（开，RFC3339）"),
               q("limit", "默认 100，上限 1000（超限 400）", schema={"type": "integer"}),
               q("cursor", "游标分页（不透明，nextCursor 回传）")],
       responses={"200": r("审计事件", schema=S("AuditResponse"),
                           example={"events": [{"id": 42, "time": "2026-08-21T12:34:56.789Z",
                                                "actor": "admin", "action": "deploy",
                                                "repo": "generic-local", "path": "a/b/w.bin",
                                                "detail": None}],
                                    "nextCursor": "43"}),
                  "400": r("limit 超限"),
                  "403": ERR_403})

    op("/api/v1/system/gc", "post", "systemGC", "system", "触发 GC",
       "官方拼写：`POST /binflow/api/v1/system/gc`（admin only；readonly_admin 403——同步执行，"
       "dry-run/apply；body 见下）。GET 无路由——上次运行经审计 `gc.run` 事件查询。",
       req_body=body("GC 参数", schema=S("GCRequest"),
                     example={"apply": False, "graceHours": 24}),
       responses={"200": r("GC 结果", schema=S("GCResponse"),
                           example={"candidateCount": 5, "candidateBytes": 204800, "deletedCount": 0}),
                  "403": ERR_403})

    op("/api/v1/system/cleanup", "post", "cleanupRun", "system", "unused-cleanup 手动触发",
       "官方拼写：`POST /binflow/api/v1/system/cleanup`（admin only）。"
       "body `{\"apply\":bool,\"repo\":string?}`——**dry-run 默认**；同步执行回 CleanupReport。"
       "引擎三腿（单把维护锁，与 gc/export/import 互斥）；`audit.enabled=false` 时 policy 腿拒绝运行"
       "（无下载痕迹就没有诚实的「未用」，宁可不删），session/gc 腿照跑。",
       req_body=body("触发参数", schema=obj({"apply": {"type": "boolean", "description": "false=dry-run 默认"},
                                             "repo": {"type": "string", "description": "限定单仓（可选）"}})),
       responses={"200": r("CleanupReport", schema=S("CleanupReport")),
                  "403": ERR_403})

    op("/api/v1/system/cleanup", "get", "cleanupStatus", "system", "unused-cleanup 状态面",
       "官方拼写：`GET /binflow/api/v1/system/cleanup`（system:read）。"
       "cron 节奏、累计计数、上次报告、各 remote 仓策略行。",
       responses={"200": r("状态面", schema=obj({}, desc="cron 节奏/累计计数/上次报告/策略行"))})

    op("/api/v1/system/settings", "get", "systemSettingsGet", "system", "运行旋钮回显",
       "官方拼写：`GET /binflow/api/v1/system/settings`（system:read——admin / readonly_admin）。"
       "回显**已解析**的运行旋钮（YAML+env+defaults 合流值）。knob-scoped 裁量——只回行为旋钮，"
       "永不携带 secret/DSN/路径；只读（其余动词 404）；旋钮本身重启生效。",
       responses={"200": r("旋钮回显", schema=S("SystemSettings"),
                           example={"folder_download": {"enabled": False, "enabled_for_anonymous": False,
                                                        "max_download_size_mb": 1024, "max_files": 5000,
                                                        "max_concurrent_requests": 10,
                                                        "enabled_empty_directories": False},
                                    "trashcan": {"retention_days": 14}})})

    # ---- 查询限流配置（query_rate_limiter）----
    op("/api/v1/system/query_rate_limiter/config", "get", "qrlConfigGet", "system",
       "查询限流配置读取",
       "官方拼写：`GET /binflow/api/v1/system/query_rate_limiter/config`（system:read——"
       "admin / readonly_admin）。回 `{\"rlSettings\":[…]}`——两桶 `rlType: DEFAULT | LOW_PRIORITY`，"
       "每桶三元组 `{permitsPerTimeFrame, timeFrameMillis, timeQuota}`；未写过回默认桶"
       "（K63 查询门的常量快照）。限流器 **disabled 时三动词一律 400 纯文本** "
       "`Query rate limiter is disabled`（携带显式 mode 的 POST 除外——见 POST）。"
       "配置为进程生命周期态：重启回出厂（无 DB 持久化）。限流只延迟不拒绝——"
       "429/408 门行为与本配置正交。",
       responses={"200": r("生效配置", schema=S("QRLSettings"),
                           example={"rlSettings": [
                               {"rlType": "DEFAULT", "permitsPerTimeFrame": 4,
                                "timeFrameMillis": 10000, "timeQuota": 1000},
                               {"rlType": "LOW_PRIORITY", "permitsPerTimeFrame": 4,
                                "timeFrameMillis": 10000, "timeQuota": 1000}]}),
                  "400": r("限流器 disabled", schema={"type": "string"},
                           example="Query rate limiter is disabled", ctype="text/plain"),
                  "403": ERR_403})

    op("/api/v1/system/query_rate_limiter/config", "post", "qrlConfigPost", "system",
       "查询限流配置合并写",
       "官方拼写：`POST /binflow/api/v1/system/query_rate_limiter/config`（system:write——仅 admin）。"
       "merge 写：请求未携带的字段保持原值。body 可含 `\"mode\"`（BinFlow 三态载体："
       "`disabled | enabled | simulation`——未知名 400 envelope `unknown mode \"…\" "
       "(expected one of disabled, enabled, simulation)`）与 `rlSettings[]`"
       "（rlType 闭集两值、三元组须全正——违例 400 envelope `invalid query rate limiter setting`）。"
       "disabled 态下不带 mode 的请求维持 400 纯文本臂；成功 **200 纯文本逐字** "
       "`Query rate limiter configuration was updated successfully`。",
       req_body=body("合并写（两半皆可选）", schema=S("QRLConfigInput"),
                     example={"mode": "enabled",
                              "rlSettings": [{"rlType": "DEFAULT", "permitsPerTimeFrame": 8,
                                              "timeFrameMillis": 10000, "timeQuota": 1000}]}),
       responses={"200": r("已更新（纯文本）", schema={"type": "string"},
                           example="Query rate limiter configuration was updated successfully",
                           ctype="text/plain"),
                  "400": r("校验失败 / disabled 臂", schema={"type": "string"},
                           example="Query rate limiter is disabled", ctype="text/plain"),
                  "403": ERR_403})

    op("/api/v1/system/query_rate_limiter/config", "delete", "qrlConfigDelete", "system",
       "查询限流配置恢复出厂",
       "官方拼写：`DELETE /binflow/api/v1/system/query_rate_limiter/config`（system:write——仅 admin）。"
       "恢复出厂态（disabled + 默认桶）；对已 disabled 的限流器同样回 disabled 400 臂"
       "（「特性关闭」判定一致）。成功 **200 纯文本逐字** "
       "`Query rate limiter configuration was deleted successfully`。",
       responses={"200": r("已恢复出厂（纯文本）", schema={"type": "string"},
                           example="Query rate limiter configuration was deleted successfully",
                           ctype="text/plain"),
                  "400": r("限流器 disabled", schema={"type": "string"},
                           example="Query rate limiter is disabled", ctype="text/plain"),
                  "403": ERR_403})

    # ---- 维护面（maintenance / backups / schedules）----
    op("/api/v1/system/maintenance", "get", "maintenanceGet", "maintenance",
       "维护面 cron 三槽位读取",
       "官方拼写：`GET /binflow/api/v1/system/maintenance`（system:read）。"
       "三槽位 `gc` / `cleanup-unused-cache` / `cleanup-virtual` 的 cron + next-run + last-run 投影"
       "（无行 = 未排程，渲染默认形）。手动面（Run Now）仍是 `POST /api/v1/system/gc` 与 `/cleanup`。",
       responses={"200": r("槽位投影", schema=S("MaintenanceProjection"))})

    op("/api/v1/system/maintenance", "put", "maintenancePut", "maintenance",
       "维护面 cron 写入",
       "官方拼写：`PUT /binflow/api/v1/system/maintenance`（system:write——仅 admin）。"
       "每槽位可选（缺席不动）；`cronExp` 空串 = 删除排程行（唯一清除途径）；`enabled` 缺省 true。"
       "所有臂先校验后落库（坏表达式不半落）。无效表达式 400 "
       "`Invalid cronExp <expr> for <slot>: <reason>`。",
       req_body=body("槽位写臂（至少一臂）", schema=S("MaintenancePutBody"),
                     example={"gc": {"cronExp": "0 3 * * *", "enabled": True}}),
       responses={"200": r("写后投影", schema=S("MaintenanceProjection")),
                  "400": r("无槽位臂 / 坏表达式（文案见描述）"),
                  "403": ERR_403})

    op("/api/v1/system/backups", "get", "backupsList", "maintenance", "备份配置列表",
       "官方拼写：`GET /binflow/api/v1/system/backups`（system:read）。"
       "每 payload 行 + cron 投影合流，key 序。",
       responses={"200": r("备份配置列表", schema=S("BackupsList"))})

    op("/api/v1/system/backups", "put", "backupsUpsert", "maintenance", "备份配置 upsert（body-key 形）",
       "官方拼写：`PUT /binflow/api/v1/system/backups`（system:write——仅 admin）。"
       "官方单 PUT 形态：key 取 body 的 `backupKey`。`exportPath` 必填（服务端绝对路径，"
       "不含 `..` 段）；`cronExp` 空串 = 保留 payload 行但不排程。"
       "仅配置定时备份——`/api/export/**` 保持 404、恢复仅 CLI。",
       req_body=body("备份配置", schema=S("BackupBody"),
                     example={"backupKey": "nightly", "enabled": True, "cronExp": "0 2 * * *",
                              "exportPath": "/var/backups/binflow"}),
       responses={"200": r("备份配置读形", schema=S("BackupConfig")),
                  "400": r("key/路径/cron 校验拒绝（文案逐字）"),
                  "403": ERR_403})

    op("/api/v1/system/backups/{key}", "get", "backupGet", "maintenance", "单条备份配置",
       "官方拼写：`GET /binflow/api/v1/system/backups/{key}`（system:read）。未知名 404 `backup not found: <key>`。",
       params=[pp("key", "备份 key")],
       responses={"200": r("备份配置读形", schema=S("BackupConfig")),
                  "404": r("backup not found: <key>")})

    op("/api/v1/system/backups/{key}", "put", "backupUpsertByKey", "maintenance",
       "备份配置 upsert（by-key 别名形）",
       "官方拼写：`PUT /binflow/api/v1/system/backups/{key}`（system:write）。路径 key 与 body key 并存时**路径为准**。",
       params=[pp("key", "备份 key（路径寻址）")],
       req_body=body("备份配置", schema=S("BackupBody")),
       responses={"200": r("备份配置读形", schema=S("BackupConfig")),
                  "400": r("校验拒绝"), "403": ERR_403})

    op("/api/v1/system/backups/{key}", "delete", "backupDelete", "maintenance",
       "删除备份配置",
       "官方拼写：`DELETE /binflow/api/v1/system/backups/{key}`（system:write）。"
       "payload 行与排程行一起删（**204**）。",
       params=[pp("key", "备份 key")],
       responses={"204": r("已删除（无 body）"),
                  "404": r("backup not found: <key>"), "403": ERR_403})

    op("/api/v1/system/schedules", "get", "schedulesList", "maintenance",
       "cron 台账只读投影",
       "官方拼写：`GET /binflow/api/v1/system/schedules?domain=`（system:read）。"
       "全部域或一个闭集域（maintenance/backup/replication）；未知域值 400 "
       "`domain must be one of maintenance, backup, replication (or omitted for every domain)`。"
       "写只存在于三个配置面。",
       params=[q("domain", "maintenance | backup | replication",
                 schema={"type": "string", "enum": ["maintenance", "backup", "replication"]})],
       responses={"200": r("台账投影", schema=S("SchedulesList")),
                  "400": r("未知 domain 值（文案见描述）")})

    # ---- uploads（MPU）----
    op("/api/v1/uploads/create", "post", "uploadsCreate", "uploads", "开会话",
       "官方拼写：`POST /binflow/api/v1/uploads/create?repoKey=&repoPath=&partSizeMB=`。"
       "QueryParam 非 JSON 体；认证 + admin/user 角色 + 目标仓 `w`；virtual 仓回落 defaultDeploymentRepo；"
       "不限包型。**200 `{\"token\": ...}`**——会话能力凭据。会话跨重启存活（upload_sessions 行）。",
       params=[q("repoKey", "目标仓", required=True),
               q("repoPath", "目标路径", required=True),
               q("partSizeMB", "分片大小", schema={"type": "integer"})],
       responses={"200": r("会话 token", schema=S("UploadSessionToken")),
                  "501": r("非纯 S3 后端（filestore/双写实例纯文本 501）",
                           schema={"type": "string"}, ctype="text/plain")})

    op("/api/v1/uploads/config", "get", "uploadsConfig", "uploads", "能力探测",
       "官方拼写：`GET /binflow/api/v1/uploads/config`。**200 `{\"supported\": bool}`**"
       "（S3 栈 true / filestore false——探测端点不回 501）；带 jfrog-cli-go UA 版本门（低于 2.62.2 回 false）。",
       responses={"200": r("能力探测", schema=S("UploadConfig"))})

    op("/api/v1/uploads/urlPart", "post", "uploadsUrlPart", "uploads", "取第 n 片上传 URL",
       "官方拼写：`POST /binflow/api/v1/uploads/urlPart?partNumber=N`（Bearer 会话 token）。"
       "**200 `{\"url\": ...}`**——URL 查询串自带 `?token=` 能力，PUT 可免 Authorization。",
       params=[q("partNumber", "分片序号", required=True, schema={"type": "integer"})],
       responses={"200": r("分片 URL", schema=S("UploadUrl"))})

    op("/api/v1/uploads/status", "post", "uploadsStatus", "uploads", "异步任务进度",
       "官方拼写：`POST /binflow/api/v1/uploads/status`（Bearer）。"
       "status ∈ PARTS/PROCESSING/**FINISHED**(progress 100 + checksumToken)/NON_RETRYABLE_ERROR。",
       responses={"200": r("任务进度", schema=S("UploadStatus"))})

    op("/api/v1/uploads/complete", "post", "uploadsComplete", "uploads", "提交组装",
       "官方拼写：`POST /binflow/api/v1/uploads/complete?sha1=`（Bearer；**sha1** 40 hex 必填）→ "
       "**202 受理**，任务异步；错配经 status 的 NON_RETRYABLE_ERROR 呈现。"
       "完成后凭 status 回的 checksumToken 做零传输 `X-Checksum-Deploy` PUT 落节点"
       "（节点由客户端落，服务端只组装+登记 blob）。",
       params=[q("sha1", "40 hex", required=True)],
       responses={"202": r("已受理（轮询 status）")})

    op("/api/v1/uploads/abort", "post", "uploadsAbort", "uploads", "弃置会话",
       "官方拼写：`POST /binflow/api/v1/uploads/abort`（Bearer）→ 204。",
       responses={"204": r("已弃置（无 body）")})

    op("/api/v1/uploads/part/{id}/{partNumber}", "put", "uploadsPart", "uploads", "传片",
       "官方拼写：`PUT /binflow/api/v1/uploads/part/{id}/{n}?token=`（urlPart 目标）。"
       "**200** S3 PutObject 形；可乱序到达——有界重排暂存；服务端中继进 S3 multipart。",
       params=[pp("id", "会话 id"), pp("partNumber", "分片序号", ),
               q("token", "会话能力 token（urlPart 回带的查询串能力）", required=True)],
       req_body=body("分片字节", schema={"type": "string", "format": "binary"}),
       responses={"200": r("已接收")})

    # ---- replication ----
    op("/api/v1/replications", "get", "replicationList", "replication", "复制配置列表",
       "官方拼写：`GET /binflow/api/v1/replications`（system:read——readonly_admin 可读）。"
       "bare array；凭据字段永不回显。",
       responses={"200": r("配置列表", schema=arr(S("ReplicationConfig")))})

    op("/api/v1/replications", "post", "replicationCreate", "replication", "建复制配置",
       "官方拼写：`POST /binflow/api/v1/replications`（system:write——仅 admin）。"
       "**201** 回显配置行；`enabled` 缺省 true；重名 409、未知源仓 400 点名 key。"
       "**target_url 填目标实例裸 origin**（如 `http://target.example:8080`），**不带** `/binflow` 后缀"
       "（带了会拼出 `/binflow/binflow/…`）。",
       req_body=body("复制配置", schema=obj(
           {"name": {"type": "string"}, "target_url": {"type": "string"},
            "target_repo": {"type": "string"}, "target_username": {"type": "string"},
            "target_password": {"type": "string", "description": "写后密封，永不回显"},
            "enabled": {"type": "boolean", "description": "缺省 true"},
            "cronExp": {"type": "string", "description": "定时全量同步（空 = 仅事件驱动）"},
            "max_items_per_push": {"type": "integer"}},
           desc="完整字段与引擎行为见治理指南 · 复制"),
           required=True),
       responses={"201": r("已创建（回显配置行）", schema=S("ReplicationConfig")),
                  "400": r("未知源仓（点名 key）"), "409": r("重名")})

    op("/api/v1/replications/{key}", "put", "replicationUpdateEnabled", "replication",
       "启停一条复制配置",
       "官方拼写：`PUT /binflow/api/v1/replications/{id}`（system:write——仅 admin）。"
       "`{id}` = 列表行首的**数值 id**（不可变键；DELETE 按 name——两种寻址并存）。"
       "body `{\"enabled\":true|false}` 必填，**其余字段解析但忽略**（整行 round-trip 不被拒）；"
       "**200** 回显更新后配置行（GET 投影同形，`updated_at` 刷新，sealed 凭据原样保留）。"
       "停用后新制品即不入队、在途任务跑完自身结论；恢复后积压由下一趟 sweep 排空，无需重启。"
       "错误：匿名 401 / 非 admin 403 / 未知 id 404 `replication config not found: <id>` / "
       "非数字 id 400 / 缺 enabled 400。",
       params=[pp("key", "数值 id（不可变键）")],
       req_body=body("启停", schema=obj({"enabled": {"type": "boolean"}}, additional=False),
                     example={"enabled": False}, required=True),
       responses={"200": r("更新后配置行", schema=S("ReplicationConfig")),
                  "400": r("非数字 id / 缺 enabled"), "404": r("replication config not found: <id>")})

    op("/api/v1/replications/{key}/run", "post", "replicationRun", "replication",
       "Replicate Now——全量同步触发",
       "官方拼写：`POST /binflow/api/v1/replications/{id}/run`（system:write——仅 admin）。"
       "按已存配置种一趟对账任务；**排程即返回，不等复制**。重复触发**不去重**（200 再种，目标侧 sha256 幂等收敛）；"
       "停用配置 → **409**（先 `PUT enabled=true`）；push 被封 → **409**（锚文 "
       "`Push replication is blocked, skipping replication` + 解锁指路）；未知 id 404。",
       params=[pp("key", "数值 id")],
       responses={"200": r("排程受理", schema=S("ReplicationRunResponse"),
                           example={"info": "The replication tasks was successfully scheduled to run",
                                    "id": 1, "name": "push-b", "scheduled": 5, "capped": False}),
                  "409": r("停用配置 / push 被封锁（文案见描述）"),
                  "404": r("未知 id")})

    op("/api/v1/replications/{key}/test", "post", "replicationTest", "replication",
       "探测已存配置的目标连通",
       "官方拼写：`POST /binflow/api/v1/replications/{id}/test`（system:write——仅 admin）。"
       "探测 `GET {target_url}/binflow/api/storage/{target_repo}`（携已存密封凭据）；"
       "可选 body `{target_url/target_repo/target_username/target_password}` 逐字段覆盖"
       "（改了 URL/用户名没给密码 → 按匿名探测，旧密文不外发）。**ok 判定体**：通过 200；"
       "失败**同形 400**（`ok:false` + 目标状态码/原因内联）。零副作用、**不看封锁态**。",
       params=[pp("key", "数值 id")],
       req_body=body("逐字段覆盖（可选）", schema=obj(
           {"target_url": {"type": "string"}, "target_repo": {"type": "string"},
            "target_username": {"type": "string"}, "target_password": {"type": "string"}})),
       responses={"200": r("探测通过", schema=S("ReplicationTestResponse"),
                           example={"ok": True, "status_code": 200,
                                    "message": "Push replication target url 'http://127.0.0.1:18502/binflow/api/storage/mirror-b' tested successfully"}),
                  "400": r("探测失败（同形 ok:false）", schema=S("ReplicationTestResponse"),
                           example={"ok": False, "status_code": 401,
                                    "message": "Connection failed: Target replication URL returned error 401: {…invalid credentials…}"})})

    op("/api/v1/replications/test", "post", "replicationTestDraft", "replication",
       "无 id 草稿面——未存候选先测后存",
       "官方拼写：`POST /binflow/api/v1/replications/test`（system:write——仅 admin）。"
       "body 必填（`{target_url,target_repo,target_username?,target_password?}`）。"
       "自实例目标 → `ok:false`（`Cannot replicate to the same instance: …`）；"
       "`-cache` 结尾目标 → 官方文案 `Replication to remote cache repositories are not allowed.`",
       req_body=body("草稿探测目标", schema=obj(
           {"target_url": {"type": "string"}, "target_repo": {"type": "string"},
            "target_username": {"type": "string"}, "target_password": {"type": "string"}},
           required=["target_url", "target_repo"]), required=True),
       responses={"200": r("判定体", schema=S("ReplicationTestResponse")),
                  "400": r("同形 400（见描述）", schema=S("ReplicationTestResponse"))})

    op("/api/v1/replications/{key}", "delete", "replicationDelete", "replication",
       "按名删除复制配置",
       "官方拼写：`DELETE /binflow/api/v1/replications/{name}`（system:write——仅 admin；**按名**）。"
       "任务台账级联清空；**204** 无体；再删 404。",
       params=[pp("key", "配置 name（DELETE 按名——与 PUT 的数值 id 寻址并存）")],
       responses={"204": r("已删除（无 body）"), "404": r("再删 404")})

    op("/api/v1/replication/status", "get", "replicationStatus", "replication",
       "复制面板载荷",
       "官方拼写：`GET /binflow/api/v1/replication/status`（system:read——readonly_admin 可读）。"
       "`targets[]`（每配置任务计数行）+ `events[]`（跨配置最近任务合并，`?limit=` 1..500 缺省 50）。",
       params=[q("limit", "1..500，缺省 50", schema={"type": "integer"})],
       responses={"200": r("面板载荷", schema=obj({"targets": arr(obj({}, desc="每配置任务计数行")),
                                                   "events": arr(obj({}, desc="最近任务合并"))}))})

    op("/api/v1/system/replications", "get", "replicationGlobalBlockGet", "replication",
       "全局封锁态",
       "官方拼写：`GET /binflow/api/v1/system/replications`（system:read）。"
       "官方键形 `{\"blockPullReplications\":bool,\"blockPushReplications\":bool}`。",
       responses={"200": r("封锁态", schema=S("GlobalBlockState"),
                           example={"blockPullReplications": False, "blockPushReplications": True})})

    for verb, word in (("block", "封锁"), ("unblock", "解除封锁")):
        op("/api/v1/system/replications/%s" % verb, "post",
           "replicationGlobal%s" % verb.capitalize(), "replication",
           "应急刹车——%s" % word,
           "官方拼写：`POST /binflow/api/v1/system/replications/%s?push=&pull=`（system:write——仅 admin）。"
           "query `push`/`pull` 选方向（缺省 = 该方向动作；**非 `\"true\"` 串 = 本次不动**）；"
           "响应 **text/plain** 官方文案（`Successfully blocked all replications, no replication will be triggered.` / "
           "仅单方向变体 / 双不动 `No action taken.`）。幂等、写入即持久（重启保持）；"
           "**不拦配置面**（CRUD/启停/列表封锁期照常）。封锁生效面：事件轨（新制品零入队）+ 认领轨（在途停发，"
           "重试计数保留）+ 手动触发（run 409）+ 拉侧回源（miss 404 点名封锁）。" % verb,
           params=[q("push", "方向选择（缺省 = 动作；非 true 串 = 本次不动）",
                     schema={"type": "string", "enum": ["true"]}),
                   q("pull", "方向选择（缺省 = 动作；非 true 串 = 本次不动）",
                     schema={"type": "string", "enum": ["true"]})],
           responses={"200": r("官方文案（text/plain）", schema={"type": "string"},
                               example="Successfully unblocked all replications.", ctype="text/plain")})

    # ---- session ----
    op("/api/v1/session", "post", "sessionCreate", "session", "登录",
       "官方拼写：`POST /binflow/api/v1/session`（JSON 或 form；免认证）。"
       "登录入口对失效 cookie 豁免（cookie-tossing 免疫）。"
       "响应 `Set-Cookie: binflow_session=<id>; HttpOnly; Path=/binflow; SameSite=Lax`。"
       "会话 TTL 默认 24 小时，**活跃不能续期**（滑动续期被绝对 TTL 封顶吞没）。"
       "CSRF：会话 cookie 认证的非 GET/HEAD 写请求，携带非同源 `Origin` 头 → **403**（Basic/Token 天然免疫）。",
       req_body=body("凭据", schema=obj({"username": {"type": "string"},
                                         "password": {"type": "string"}},
                                        required=["username", "password"], additional=False),
                     example={"username": "admin", "password": "<口令>"}, required=True),
       responses={"200": r("登录回显（含 adminRole 与 source）", schema=S("SessionResponse"),
                           example={"username": "admin", "admin": True}),
                  "401": ERR_401},
       security=ANON)

    op("/api/v1/session", "get", "sessionWhoami", "session", "Whoami（当前会话信息）",
       "官方拼写：`GET /binflow/api/v1/session`。",
       responses={"200": r("当前会话信息", schema=S("SessionResponse")),
                  "401": ERR_401})

    op("/api/v1/session", "delete", "sessionDelete", "session", "登出（吊销会话）",
       "官方拼写：`DELETE /binflow/api/v1/session`（服务端吊销；会话过期后同一 cookie 重放 → 401）。",
       responses={"204": r("已登出（无 body）"), "401": ERR_401})
