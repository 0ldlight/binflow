// 统一数据请求层（console-ux §5.1：错误呈现收敛为「状态码 + message 提取」，
// 不把 E-01 errors[] / 用户管理纯文本 / OAuth 三种格式暴露给组件）。
//
// - 全部走同源 /binflow/api/**（ADR-0014 决策 3：前端消费通用管理面，
//   无 console 专属树；cookie binflow_session 是等价凭据）。
// - 写请求浏览器自动带同源 Origin（CSRF 主防线，ADR-0014 勘误④）；
//   X-BinFlow-Console 头是前端自身的第二层习惯，服务端记录不强制。
// - 401 监听：会话中（已认证态）任何请求突然 401 = 会话已死于
//   created_at+TTL（T-110 塌缩句：绝对上限与活跃度无关）或被吊销——
//   AuthContext 挂监听做 toast + 重登跳转；探活/登录这类预期 401 的
//   调用用 silent401 豁免。

export class ApiError extends Error {
  readonly status: number
  /** 服务端原始响应体（排障折叠区展示用） */
  readonly raw: string

  constructor(status: number, message: string, raw = '') {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.raw = raw
  }
}

const API_ROOT = '/binflow/api'

type UnauthorizedListener = () => void
let unauthorizedListener: UnauthorizedListener | null = null

/** AuthContext 启动时挂载；传 null 卸载 */
export function setUnauthorizedListener(fn: UnauthorizedListener | null): void {
  unauthorizedListener = fn
}

export interface RequestOptions {
  method?: string
  /** JSON 序列化的请求体（Content-Type 自动置 application/json） */
  body?: unknown
  signal?: AbortSignal
  /** 预期可能 401 的调用（whoami 探活、登录提交）不触发全局会话过期处理 */
  silent401?: boolean
}

/** 从三种错误体格式（errors[] / 纯文本 / OAuth 形）统一提取 message */
async function toApiError(res: Response): Promise<ApiError> {
  const text = await res.text().catch(() => '')
  let message = ''
  if (text) {
    try {
      const parsed = JSON.parse(text) as { errors?: { message?: string }[] }
      const first = parsed?.errors?.[0]?.message
      if (typeof first === 'string' && first) message = first
    } catch {
      // 非 JSON：用户管理层是纯文本，直接用
    }
  }
  if (!message) message = text.trim()
  if (!message) message = `HTTP ${res.status}`
  return new ApiError(res.status, message, text)
}

async function rawRequest(path: string, opts: RequestOptions = {}): Promise<Response> {
  const headers: Record<string, string> = {
    Accept: 'application/json, text/plain, */*',
    'X-BinFlow-Console': '1',
  }
  const init: RequestInit = {
    method: opts.method ?? 'GET',
    headers,
    credentials: 'same-origin',
    signal: opts.signal,
  }
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(opts.body)
  }
  const res = await fetch(`${API_ROOT}${path}`, init)
  if (!res.ok) {
    if (res.status === 401 && !opts.silent401) unauthorizedListener?.()
    throw await toApiError(res)
  }
  return res
}

/** JSON 端点请求；非 2xx 抛 ApiError */
export async function apiJSON<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const res = await rawRequest(path, opts)
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

/** 纯文本端点请求（如 /api/security/password 的 200 文案）；非 2xx 抛 ApiError */
export async function apiText(path: string, opts: RequestOptions = {}): Promise<string> {
  const res = await rawRequest(path, opts)
  return (await res.text()).trim()
}

/** 暴露原始 fetch（后续票的下载/上传等非 JSON 面复用同一信封） */
export { rawRequest }

/** 错误转用户可读文案（组件层 catch 后统一入口） */
export function errText(err: unknown): string {
  if (err instanceof ApiError) return err.message
  if (err instanceof Error) return err.message
  return String(err)
}

// ---- 会话 API（CE-03~05） ----

export interface Whoami {
  username: string
  admin: boolean
  /** M7 RBAC 闭集角色回显（wire snake 值，与 users.role 列同拼——ADR-0026
   *  决策 6）。旧二进制无此字段：归一化回退 admin 布尔镜像。 */
  adminRole?: string
}

// ---- RBAC 角色闭集（M7 FR-64/FR-66；唯一事实源 internal/auth/rbac.go） ----

/** 闭集角色 wire 值（snake 三值；新增角色 = 架构变更，永远不是数据变更） */
export type AdminRole = 'user' | 'readonly_admin' | 'admin'
export const ADMIN_ROLES: readonly AdminRole[] = ['user', 'readonly_admin', 'admin']

/** 回显归一：闭集外（旧二进制 / 残缺回显）回退 admin 布尔镜像，与服务端
 *  EffectiveRole / roleFromStored 的 fail-safe 同口径——UI 呈现永不放大权限。 */
export function normalizeAdminRole(role: string | null | undefined, admin: boolean): AdminRole {
  if (role === 'admin' || role === 'readonly_admin' || role === 'user') return role
  return admin ? 'admin' : 'user'
}

/** 管理面写能力（UI 呈现位）：仅全量 admin。readonly_admin 是只读呈现态，
 *  普通 user 不入管理面。服务端是唯一守门——这里只驱动禁用/隐藏，不放大。 */
export function canAdminWrite(session: Whoami | null): boolean {
  return !!session && normalizeAdminRole(session.adminRole, session.admin) === 'admin'
}

/** readonly_admin 只读态（管理页可见 + 编辑动作禁用的 UI 判定位） */
export function isReadOnlyAdmin(session: Whoami | null): boolean {
  return !!session && normalizeAdminRole(session.adminRole, session.admin) === 'readonly_admin'
}

export function postSession(username: string, password: string): Promise<Whoami> {
  return apiJSON<Whoami>('/v1/session', { method: 'POST', body: { username, password }, silent401: true })
}

export function getWhoami(): Promise<Whoami> {
  return apiJSON<Whoami>('/v1/session', { silent401: true })
}

export function deleteSession(): Promise<void> {
  return apiJSON<void>('/v1/session', { method: 'DELETE', silent401: true })
}

// ---- 版本 / 健康 / 统计 / 仓库 / 审计（仪表盘与设置页消费） ----

export interface VersionInfo {
  version: string
  revision: string
  product: string
}

export interface SubsystemStatus {
  status: string
  detail?: string
}

export interface HealthInfo {
  status: string
  storage: SubsystemStatus
  metadata: SubsystemStatus
  registry: SubsystemStatus
}

export interface StorageStats {
  blobs: number
  logical_bytes: number
  physical_bytes: number
}

export interface RepoListItem {
  key: string
  description: string
  type: string
  packageType: string
  url: string
  /** remote/virtual 行回带的规范化配置（local 行 M1 裸形态；T-98 加字段，
   *  T-99 起列表消费 url/成员/priorityResolution——契约面实存） */
  configuration?: Record<string, unknown>
}

export interface AuditEvent {
  id: number
  time: string
  actor: string
  action: string
  repo: string
  path: string
  detail?: unknown
}

export interface AuditPage {
  events: AuditEvent[]
  nextCursor: string
}

export function getVersion(): Promise<VersionInfo> {
  return apiJSON<VersionInfo>('/system/version', { silent401: true })
}

export function getHealth(): Promise<HealthInfo> {
  return apiJSON<HealthInfo>('/v1/health')
}

export function getStorageStats(): Promise<StorageStats> {
  return apiJSON<StorageStats>('/v1/storage/stats')
}

export function getRepositories(): Promise<RepoListItem[]> {
  return apiJSON<RepoListItem[]>('/repositories')
}

export function getRecentAudit(limit = 8): Promise<AuditPage> {
  return apiJSON<AuditPage>(`/v1/audit?limit=${limit}`)
}
