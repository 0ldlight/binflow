// 治理域 API（T-102；契约源 internal/httpapi/audit.go + system_gc.go + usage.go，
// usage 的消费面在 lib/repos.ts——此处只收 T-102 新页面组直接吃的两端）：
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
