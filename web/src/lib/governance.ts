// 治理域 API（T-102；契约源 internal/httpapi/audit.go + system_gc.go + usage.go，
// usage 的消费面在 lib/repos.ts——T-102 两端 + T-462 cron 三消费面
//〔维护三槽 / cleanup Run Now / 备份定时 CRUD〕随消费页入本文件）：
//
// - 审计查询 GET /api/v1/audit：repo/actor/action 均为**精确匹配**
//   （metadata auditStore `repo_key = ?` / `actor = ?` / `action = ?`），
//   since 含 / until 不含（闭开区间），limit 1..1000，nextCursor 为 keyset
//   游标（"" = 末页）。**没有 path 过滤参数**——对象路径过滤按 console-ux
//   §6.3 兜底为已加载集客户端子串（契约漂移见工作日志）。
// - GC POST /api/v1/system/gc：{"apply":bool,"graceHours":int?}；缺省 body
//   即 dry-run；graceHours 缺省 = 实例配置 storage.gc_grace_hours，显式 0 =
//   无宽限窗口。409 = data 目录维护锁互斥（message 含 holder 诊断）。

import { apiJSON } from './api'
import type { AuditPage } from './api'

/** 服务端过滤面（path 过滤不在此列——它不进 REST 查询） */
export interface AuditFilters {
  repo: string
  actor: string
  action: string
  /** RFC3339 UTC；空 = 不设界 */
  since: string
  /** RFC3339 UTC（排他界）；空 = 不设界 */
  until: string
}

export const EMPTY_AUDIT_FILTERS: AuditFilters = { repo: '', actor: '', action: '', since: '', until: '' }

/** 审计分页大小（console-ux §6.1：与 docker API n 缺省一致） */
export const AUDIT_PAGE_SIZE = 100

/** 审计动作词表（internal/audit Actions() 的前端镜像，GE-02：选择器与断言源；
 * T-353 对 T-346（FR-113.4）后的全量 picker 逐枚对照同步——54 枚）。
 * 过滤未知动作只是匹配零行（前向兼容），不报错。 */
export const AUDIT_ACTIONS: readonly string[] = [
  'deploy',
  'delete',
  'download',
  'login.success',
  'auth.failed', // M6 PRD FR-56 拼写：认证失败动作（T-187 更名后的全局单一词汇）
  'repo.create',
  'repo.update',
  'repo.delete',
  'token.issue',
  'token.revoke',
  'password.change',
  'group.create',
  'group.update',
  'group.delete',
  'group.member',
  'user.role.change', // M7 PRD FR-64：角色指派/变更审计（T-212 recordRoleChange 落点）
  'user.delete', // M9 E4（T-251）：DELETE /api/security/users/{name} 成功删除审计
  'permission.create',
  'permission.update',
  'permission.delete',
  'gc.run',
  'export.run',
  'import.run',
  'quota.exceeded',
  'cleanup.run', // M12 T-324（FR-102.2）：unused-cleanup 引擎运行（手动/定时同词）
  // ---- T-346（FR-113.4）：M6~M12 累积词表入 picker，29 枚逐枚对照 ----
  'props.write', // M4 属性面（T-286）
  'props.delete',
  'replication.push', // M6 T-180 复制配置/事件面
  'replication.push.failed',
  'replication.config.create',
  'replication.config.delete',
  'keypair.create', // M11 T-319 签名密钥对
  'keypair.update',
  'keypair.generate',
  'keypair.delete',
  'keypair.verify',
  'keypair.associate',
  'auth.config.update', // M11 T-305 认证配置（SAML SP 密钥动词 = T-331）
  'auth.config.test',
  'auth.config.samlkey.generate',
  'auth.config.samlkey.regenerate',
  'license.install', // M10 license 面 + T-283 addon 门
  'license.delete',
  'license.invalid',
  'license.addon.denied',
  'artifact.copy', // M12 T-339/T-343 制品操作族
  'artifact.move',
  'artifact.explode',
  'trash.restore', // M12 T-345 回收站
  'trash.empty',
  'trash.clean',
  'trash.retention',
  'storage.replay.window', // M12 T-338 dual-write fail-open
  'storage.replay.drained',
  // ---- M16 T-446/T-450（FR-150.2 / ADR-0044 决策 10）：cron 调度器
  // 三域 set/run/fail 三连词——set 由配置面发（本票消费面），
  // run/fail 由引擎发；叠加在载体自身审计词（gc.run/export.run 等）之上
  'maintenance.schedule.set',
  'maintenance.schedule.run',
  'maintenance.schedule.fail',
  'backup.schedule.set',
  'backup.schedule.run',
  'backup.schedule.fail',
  'replication.schedule.set',
  'replication.schedule.run',
  'replication.schedule.fail',
]

/** 组装 /v1/audit 查询串（空值不进参数） */
export function auditQueryString(f: AuditFilters, cursor = '', limit = AUDIT_PAGE_SIZE): string {
  const p = new URLSearchParams()
  if (f.repo.trim()) p.set('repo', f.repo.trim())
  if (f.actor.trim()) p.set('actor', f.actor.trim())
  if (f.action.trim()) p.set('action', f.action.trim())
  if (f.since) p.set('since', f.since)
  if (f.until) p.set('until', f.until)
  if (cursor) p.set('cursor', cursor)
  p.set('limit', String(limit))
  return p.toString()
}

/** 拉一页审计事件（首页 cursor 留空） */
export function getAuditEventsPage(f: AuditFilters, cursor = '', limit = AUDIT_PAGE_SIZE): Promise<AuditPage> {
  return apiJSON<AuditPage>(`/v1/audit?${auditQueryString(f, cursor, limit)}`)
}

/**
 * datetime-local 输入值 → 后端口径的 RFC3339 UTC（秒精度）。
 * 浏览器把 datetime-local 值按本地时区解析，此处转成 UTC 后送出，
 * 与 audit.NormalizeTimestamp 的「offset 折算到 UTC」语义一致。
 * 返回：'' = 未设界；null = 输入非法（调用方行内报错，不发请求）。
 */
export function localInputToRFC3339(v: string): string | null {
  if (v === '') return ''
  const t = new Date(v)
  if (Number.isNaN(t.getTime())) return null
  return t.toISOString().replace(/\.\d{3}Z$/, 'Z')
}

// ---- GC（GE-03 / T-94） ----

/** gcResponse：dry-run 时 deletedCount 恒 0；apply 时 candidateCount 是
 * 预扫所见、deletedCount 是实际回收（静默实例上相等；写入方竞态下的
 * 分歧是诚实结果而非错误）。**不含候选清单**——REST 面只回聚合计数。 */
export interface GCRunResult {
  candidateCount: number
  candidateBytes: number
  deletedCount: number
}

/** maxGCHours 的前端同值（100 年；越界服务端 400） */
export const GC_MAX_GRACE_HOURS = 100 * 365 * 24

/**
 * 触发一次 GC。graceHours：null = 缺省（实例配置）；数字 = 显式窗口
 * （0 = 无宽限窗口，只回收已确认孤儿）。
 */
export function runGC(apply: boolean, graceHours: number | null): Promise<GCRunResult> {
  const body: Record<string, unknown> = { apply }
  if (graceHours !== null) body.graceHours = graceHours
  return apiJSON<GCRunResult>('/v1/system/gc', { method: 'POST', body })
}

// ---- 维护面 cron 三槽（M16 T-462 / FR-145.7，契约 = internal/httpapi
// system_maintenance.go——T-450 落的 GET/PUT /api/v1/system/maintenance） ----
//
// 三槽闭集（021 台账 domain='maintenance' 的行键）：gc / cleanup-unused-cache
// / cleanup-virtual。BinFlow 的诚实映射：两 cleanup 槽 fire 时同走
// CleanupEngine.RunOnce 全量 pass（ADR-0044 决策 8①——BinFlow 无
// virtual-only 载体，C 层差异留痕）；GC 槽 fire 走 RunGC（apply 载体，
// grace 兜底不缩短）。手动面（dry-run/apply/POST /system/cleanup）并存维持
// ——「Run Now」就是它们。

/** 维护面槽键闭集（wire key 原样进 PUT 臂名） */
export type MaintenanceSlotKey = 'gc' | 'cleanup-unused-cache' | 'cleanup-virtual'

export const MAINTENANCE_SLOTS: readonly MaintenanceSlotKey[] = [
  'gc',
  'cleanup-unused-cache',
  'cleanup-virtual',
]

/** GET/PUT 投影的行（maintenanceSlotStatus——camelCase；nextRun/lastRun 为
 *  RFC3339 串，'' = 未排/未跑；无台账行 = 全空缺省态「未调度」） */
export interface MaintenanceSlot {
  key: MaintenanceSlotKey
  cronExp: string
  enabled: boolean
  nextRun: string
  lastRun: string
  lastStatus: string
  lastError: string
}

export interface MaintenanceView {
  slots: MaintenanceSlot[]
}

/** PUT 单槽臂（cronExp 空 = 删行〔无行 = 不调度单态〕；enabled 缺省 true
 *  ——FE 面不暴露停用位：cron 在场即调度，语义与 7.161 表单一致） */
export interface MaintenanceArm {
  cronExp: string
  enabled?: boolean
}

/** 读三槽调度态（system:read——readonly_admin 可读） */
export function getMaintenance(): Promise<MaintenanceView> {
  return apiJSON<MaintenanceView>('/v1/system/maintenance')
}

/** 写一或多槽（system:write——服务端全臂先验后写，坏表达式 400 零残留） */
export function putMaintenance(
  arms: Partial<Record<MaintenanceSlotKey, MaintenanceArm>>,
): Promise<MaintenanceView> {
  return apiJSON<MaintenanceView>('/v1/system/maintenance', { method: 'PUT', body: arms })
}

// ---- Cleanup 全量 pass 手动面（T-324 REST + T-462 消费） ----

/** POST /api/v1/system/cleanup {apply:true} 的报告（repo.CleanupReport
 *  子集——面只呈现聚合行）。ok=false 时 error 是引擎点名原因。 */
export interface CleanupRunReport {
  trigger: string
  apply: boolean
  startedAt: string
  finishedAt: string
  gracePending: number
  gcDeleted: number
  sessionsSwept: number
  objectsCleaned: number
  bytesReclaimed: number
  ok: boolean
  error?: string
}

/** Cleanup「Run Now」：三腿全量 pass（apply 载体，Trigger=manual——与调度
 *  fire 同载体；报告聚合回呈现面） */
export function runCleanupNow(): Promise<CleanupRunReport> {
  return apiJSON<CleanupRunReport>('/v1/system/cleanup', { method: 'POST', body: { apply: true } })
}

// ---- 备份定时 CRUD（M16 T-462 / FR-145.7，契约 = internal/httpapi
// system_backups.go——022 payload 台账 + 021 cron 半，五面实测验形） ----
//
// wire 字段 = ADR-0044 软缝⑦三名（backupKey/cronExp/nextBackupTime）+
// exportPath（服务器绝对路径，禁 ..）。cronExp 空 = payload 行保留但未
// 调度（台账行删——单态）；nextBackupTime 可写位须未来时刻（过去 400）。
// Artifactory 描述符字段无载体者（仓子集/incremental/retention 轮转/zip/
// 邮件告警）刻意缺席——不伪造。ADR-0015 勘误②边界维持：import CLI-only。

/** 列表/读回显（backupResponse——nextScheduleBackup 为台账 next_run，
 *  '' = 未排/停用） */
export interface BackupConfig {
  backupKey: string
  enabled: boolean
  exportPath: string
  cronExp: string
  nextScheduleBackup: string
  lastRun: string
  lastStatus: string
  lastError: string
  createdAt: string
  updatedAt: string
}

export interface BackupsView {
  backups: BackupConfig[]
}

/** PUT upsert 体（两种 PUT 形共享——FE 走 body-key 官方形） */
export interface BackupUpsertBody {
  backupKey: string
  enabled: boolean
  cronExp: string
  /** RFC3339；'' = 由表达式推算 */
  nextBackupTime?: string
  exportPath: string
}

/** 列表（system:read） */
export function listBackups(): Promise<BackupsView> {
  return apiJSON<BackupsView>('/v1/system/backups')
}

/** upsert（system:write；200 回保存后的完整回显） */
export function putBackup(body: BackupUpsertBody): Promise<BackupConfig> {
  return apiJSON<BackupConfig>('/v1/system/backups', { method: 'PUT', body })
}

/** 删除（payload + 台账行联动删；204） */
export function deleteBackup(key: string): Promise<void> {
  return apiJSON<void>(`/v1/system/backups/${encodeURIComponent(key)}`, { method: 'DELETE' })
}

/**
 * backup key 合法性的前端镜像（validBackupKey）：1..64 字符的
 * [A-Za-z0-9._-]、首字符字母数字（与复制配置名同规则——key 是 {key} 路由
 * 的单段 + 台账行键）。null = 合法，否则为行内错误文案。
 */
export function validateBackupKey(key: string): string | null {
  if (key === '') return 'Backup Key 未填'
  if (key.length > 64) return 'Backup Key 最长 64 字符'
  if (!/^[A-Za-z0-9]/.test(key)) return 'Backup Key 须以字母或数字开头'
  if (!/^[A-Za-z0-9._-]+$/.test(key)) return 'Backup Key 仅允许字母/数字/./_/-（无空格与路径段）'
  return null
}

/**
 * exportPath 形态的前端镜像（validExportPathShape——廉价形态门）：绝对
 * 路径 + 无 .. 段。与 data 目录的边界判定在 fire 时由服务端执行
 * （本门只拒绝永不可能合法的形态）。
 */
export function validateExportPath(path: string): string | null {
  if (path === '') return '服务器路径未填'
  if (!path.startsWith('/')) return '须为服务器绝对路径（以 / 开头）'
  if (path.includes('..')) return '路径不得包含 .. 段'
  return null
}
