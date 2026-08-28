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
  /** 原样字节体（M10 license 装载：POST /api/system/license 的 body 就是
   *  license 文档全文，不是 JSON——T-288 起的纯文本写面共享同一信封）。 */
  rawBody?: string
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
  if (opts.rawBody !== undefined) {
    headers['Content-Type'] = 'text/plain;charset=utf-8'
    init.body = opts.rawBody
  } else if (opts.body !== undefined) {
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
  /** 身份属主（local/ldap/oidc，FR-56-AC1/H36）——step-up 腿分流依据
   *  （ADR-0027 决策 3：oidc → mint grant，其余 → step_up_password）。
   *  旧二进制无此字段：回退口令腿（local/LDAP 形态）。 */
  source?: string
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

// ---- 制品属性（?properties 家族，M10 T-286 后端 / T-291 FE） ---------------

/** §15.3.3 表体：{"properties":{k:[v…],…}}（GET 空命中 = 200 空表） */
export interface NodePropertiesView {
  properties: Record<string, string[]>
}

/**
 * /api/storage/{repo}/{path} 的路径编码（与 pages/artifacts/lib.ts 的
 * storagePath 同形——那是 storage 元数据族的既有落点，本族按票面落在
 * 统一请求层，编码器就地复刻并互指）。
 */
function storagePropsPath(repoKey: string, path: string): string {
  const rel = path
    .split('/')
    .filter((s) => s !== '')
    .map((s) => encodeURIComponent(s))
    .join('/')
  return `/storage/${encodeURIComponent(repoKey)}${rel ? `/${rel}` : ''}`
}

/**
 * 写集 → 逗号文法 raw query 值（§15.3.3 一处钉死的文法）：RAW 逗号是
 * 段分隔符；含 = 的段开键，不含 = 的段续前键值集。键/值各自
 * encodeURIComponent——值内逗号变 %2C 后在服务端按内容解回（encode 语义）。
 * 例：{qa:['passed','v2'],owner:['team-a']} → qa=passed,v2,owner=team-a
 */
export function encodePropsQuery(props: Record<string, string[]>): string {
  const segs: string[] = []
  for (const [k, vs] of Object.entries(props)) {
    if (vs.length === 0) continue
    segs.push(`${encodeURIComponent(k)}=${encodeURIComponent(vs[0])}`)
    for (const v of vs.slice(1)) segs.push(encodeURIComponent(v))
  }
  return segs.join(',')
}

/** 读属性：keys 过滤（尾 * 通配）；缺省 = 全量键 */
export function getNodeProperties(
  repoKey: string,
  path: string,
  keys?: string[],
): Promise<Record<string, string[]>> {
  const q = keys && keys.length > 0 ? `?properties=${keys.map((k) => encodeURIComponent(k)).join(',')}` : '?properties'
  return apiJSON<NodePropertiesView>(`${storagePropsPath(repoKey, path)}${q}`).then(
    (v) => v.properties ?? {},
  )
}

/**
 * 写属性（PUT，合并语义 §11.40：同名键值集整体替换、异名键保留）。
 * 204 无体。节点须存在（404）；无该路径 w 权限 → 403。
 *
 * 契约漂移注记（T-291）：docs/user/api-reference.md 列有
 * `POST …?properties=k=v`（增量）一行，但 router 的 properties 臂只挂了
 * GET/PUT/DELETE——POST 落 E-26 404（T-286 日志「三动词」）。FE 的增量
 * 编辑全部走 PUT：merge 语义下单键写即「替换该键值集、保留他键」，与
 * Artifactory 逐属性 add/remove 的效果面一致，无需 POST。
 */
export function putNodeProperties(
  repoKey: string,
  path: string,
  props: Record<string, string[]>,
): Promise<void> {
  return apiJSON<void>(`${storagePropsPath(repoKey, path)}?properties=${encodePropsQuery(props)}`, {
    method: 'PUT',
  })
}

/** 删属性：选键列表；`'*'` = 全删（properties=*）。幂等 204 */
export function deleteNodeProperties(repoKey: string, path: string, keys: string[] | '*'): Promise<void> {
  const raw = keys === '*' ? '*' : keys.map((k) => encodeURIComponent(k)).join(',')
  return apiJSON<void>(`${storagePropsPath(repoKey, path)}?properties=${raw}`, {
    method: 'DELETE',
  })
}

// ---- 认证配置面（M11 T-307 / FR-92，契约 = internal/httpapi/authconfig.go） ----
//
// 九端点里的三 GET/PUT + 三 test：段闭集 ldap/oauth/saml（wire 段名），REST
// 家族挂在 /v1/admin/security/{ldap|oauth|saml/config}。GET 回显是脱敏哨兵
// 形态（已设置 secret = 20 星，未设置 = ""）；PUT 对 secret 是 write-only：
// 键缺省 = 保持库存值、"" = 清除、新明文 = 替换、**回传哨兵 = 400 拒绝**
// （用户 2026-08-27 裁定照 Artifactory）——FE 的「留空保持不变」= 提交时把
// 空的 secret 字段从 payload 整个剔除，绝不回传哨兵。

/** 三协议段（wire 段名；REST 路径映射 saml → saml/config） */
export type AuthSection = 'ldap' | 'oauth' | 'saml'

/** GET 对已设置 secret 的固定 20 星哨兵（auth-integration §1.6） */
export const AUTH_SECRET_SENTINEL = '********************'

/** LDAP 段 wire 模型（§1.1/§1.2 + BinFlow 运行时扩展；camelCase） */
export interface LdapAuthConfig {
  key: string
  enabled: boolean
  ldapUrl: string
  userDnPattern: string
  search: {
    searchFilter: string
    searchBase: string
    searchSubTree: boolean
    managerDn: string
    /** GET = 哨兵（已设置）或 ""（未设置）；PUT = 新明文（空=剔除） */
    managerPassword: string
  }
  autoCreateUser: boolean
  emailAttribute: string
  allowUserToAccessProfile: boolean
  pagingSupportEnabled: boolean
  ldapPoisoningProtection: boolean
  groupFilter: string
  groupBaseDn: string
  groupNameAttribute: string
  adminGroup: string
  readOnlyGroup: string
  startTls: boolean
  skipTlsVerify: boolean
  poolSize: number
}

/** OAuth 段 wire 模型（BinFlow C 级 OIDC issuer 发现式；snake_case） */
export interface OidcAuthConfig {
  enabled: boolean
  issuer_url: string
  client_id: string
  /** GET = 哨兵（已设置）或 ""（未设置）；PUT = 新明文（空=剔除） */
  client_secret: string
  redirect_url: string
  scopes: string[]
  user_claim: string
  group_claim: string
  admin_group: string
  readonly_group: string
  auto_create_users: boolean
}

/** SAML 段 wire 模型（§3.1 13 字段 verbatim；camelCase；无 secret） */
export interface SamlAuthConfig {
  enableIntegration: boolean
  loginUrl: string
  logoutUrl: string
  serviceProviderName: string
  certificate: string
  useEncryptedAssertion: boolean
  syncGroups: boolean
  groupAttribute: string
  emailAttribute: string
  /** 命名陷阱（§3.4）：wire 是否定式且默认 true（= 默认不自动建用户） */
  noAutoUserCreation: boolean
  allowUserToAccessProfile: boolean
  autoRedirect: boolean
  verifyAudienceRestriction: boolean
}

function authSectionPath(section: AuthSection): string {
  return section === 'saml' ? '/v1/admin/security/saml/config' : `/v1/admin/security/${section}`
}

/** 读一段（未存储段：LDAP/OAuth 回默认形、SAML 回锚定空对象 {} ——§3.2） */
export function getAuthSection<T>(section: AuthSection): Promise<T> {
  return apiJSON<T>(authSectionPath(section))
}

/** 保存一段（全量替换；成功回应脱敏回显——可直接用于重置表单） */
export function putAuthSection<T>(section: AuthSection, body: unknown): Promise<T> {
  return apiJSON<T>(authSectionPath(section), { method: 'PUT', body })
}

/** TestReport（internal/auth.TestReport；探测失败也是这个体，只是 HTTP 400） */
export interface AuthTestReport {
  ok: boolean
  phase?: string
  category?: string
  message?: string
}

/**
 * 测试连接（POST …/test）。body 缺省 = 探存量（空体）；候选 = 表单当前值
 * 随体提交（LDAP 另携 testUsername/testPassword 两探针参数，§1.6 信封）。
 *
 * 探测失败是 HTTP 400 + TestReport 体（不是 errors[] 信封）——通用层会把
 * 它折成 ApiError，这里从 raw 解回报告原文呈现；其余非 2xx（403/503/5xx）
 * 保持抛 ApiError。
 */
export async function testAuthSection(section: AuthSection, body?: unknown): Promise<AuthTestReport> {
  try {
    return await apiJSON<AuthTestReport>(`${authSectionPath(section)}/test`, {
      method: 'POST',
      ...(body === undefined ? {} : { body }),
    })
  } catch (err) {
    if (err instanceof ApiError && err.status === 400 && err.raw) {
      try {
        const parsed = JSON.parse(err.raw) as AuthTestReport
        if (typeof parsed?.ok === 'boolean') return parsed
      } catch {
        // 不是报告体（strict-schema 400 信封）——按通用错误抛
      }
    }
    throw err
  }
}

// ---- SAML SP 加密证书族（T-307R / T-331，契约 = internal/httpapi saml key 三路由） ----
//
// SP（服务提供方）自己的加密密钥对的公钥面：IdP 要拿这份证书才能回发加密
// 断言（auth-integration §3.2）。text/plain 双向；读 = CapSecurityRead
// （readonly_admin 可下载——公钥是公开材料），写 = CapSecurityWrite。
// 未生成时 GET 404（errors[] 信封，锚定空态）。

/** 公钥证书 PEM 下载（text/plain；未生成 → 404 ApiError） */
export function getSamlSpCertificate(): Promise<string> {
  return apiText('/v1/admin/security/saml/config/key/public')
}

/**
 * 重生成 SP 密钥对（force 一对一替换）：**旧证书即刻失效**（只有新证书被
 * 服务），回应体 = 新证书 PEM（T-331 D-5）——直接用来刷新展示，无需再 GET。
 */
export function regenerateSamlSpKey(): Promise<string> {
  return apiText('/v1/admin/security/saml/config/key/public/regenerate', { method: 'PUT' })
}
