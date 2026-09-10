// Webhook 订阅域 API（M13 T-366，FR-115.5 FE 腿；契约源
// internal/httpapi/webhooks.go + internal/webhook/{model,service}.go，即
// docs/reverse/webhook.md §1/§2/§7——官方 Event 服务命名空间逐字）：
//
// - 挂载面是 **/binflow/event/api/v1/**（E-26 前缀下的官方段名，不在
//   /binflow/api 通用管理面下）——因此本族不走 lib/api.ts 的 API_ROOT，
//   自带同源 fetch 信封（ApiError 复用、错误体三形态提取同款）。
// - 七端点族：list(200 数组) / create(201 回显) / get / update(**204 无体**，
//   key 与 project_key 不可改) / delete(204) / test（**吃完整订阅体草稿**，
//   非 key 引用——同步单发不入箱不重试，200 恒定，判定在体的 ok/attempt）
//   / troubleshooting（排障记录环：失败必录、debug:true 成功也录）。
// - 权限：读 = system:read（readonly_admin 可见）；写 = system:write 且过
//   webhook 槽 license 门（community 403 + X-Binflow-License-Required: webhook
//   ——FE 不复制门控，服务端终裁；槽行仅用于提示文案）。
// - secret 哨兵语义（webhook.md §2.4 + ADR-0041 决策 5）：回显 `********`
//   = 已设置；PUT 三态——**省略字段 = 保持、明文 = 轮换、"" = 擦除**。
//   FE 的「留空保持不变」= 提交时从 payload 剔除 secret 键（哨兵绝不回传）。
// - 事件类型闭集 = 13 域 66 型（eventtypes.go 镜像，唯二事实源之间的静态
//   拷贝——服务端校验终裁，FE 表仅驱动下拉分组与 wired/dormant 标注）。

import { ApiError, apiJSON } from './api'
import { tr } from '../i18n'

const tt = tr('console')

/** 事件面根（E-26 前缀 + 官方段名逐字） */
const EVENT_ROOT = '/binflow/event/api/v1'

/** 预定义 handler 的 secret 脱敏哨兵（服务端回显形，ADR-0041 决策 5） */
export const WEBHOOK_SECRET_SENTINEL = '********'

/** 事件面同源请求：错误折叠为 ApiError（与 lib/api.ts 同口径，根不同） */
async function eventJSON<T>(path: string, opts: { method?: string; body?: unknown } = {}): Promise<T> {
  const init: RequestInit = {
    method: opts.method ?? 'GET',
    headers: { Accept: 'application/json, text/plain, */*', 'X-BinFlow-Console': '1' },
    credentials: 'same-origin',
  }
  if (opts.body !== undefined) {
    ;(init.headers as Record<string, string>)['Content-Type'] = 'application/json'
    init.body = JSON.stringify(opts.body)
  }
  const res = await fetch(`${EVENT_ROOT}${path}`, init)
  if (!res.ok) {
    const text = await res.text().catch(() => '')
    let message = ''
    if (text) {
      try {
        const parsed = JSON.parse(text) as { errors?: { message?: string }[]; message?: string }
        const first = parsed?.errors?.[0]?.message ?? parsed?.message
        if (typeof first === 'string' && first) message = first
      } catch {
        // 非 JSON：管理面多为纯文本
      }
    }
    if (!message) message = text.trim()
    if (!message) message = `HTTP ${res.status}`
    throw new ApiError(res.status, message, text)
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

// ---- wire 类型（model.go 逐字字段名） --------------------------------------

/** custom http 头对 */
export interface HeaderPair {
  name: string
  value: string
}

/** 回显 handler（handlers[0]；官方 minItems/maxItems 1——恰一个） */
export interface HandlerView {
  handler_type: 'webhook' | 'custom-webhook'
  url: string
  proxy: string
  /** 已设置时为哨兵 `********`；缺省 = 未设置 */
  secret?: string
  use_secret_for_signing: boolean
  custom_http_headers: HeaderPair[]
  method?: string
  payload?: string
  http_headers: HeaderPair[]
  secrets: { name: string }[]
}

/** 订阅回显（WebhookSubscription） */
export interface WebhookSubscription {
  key: string
  project_key: string
  description: string
  enabled: boolean
  event_filter: {
    domain: string
    event_types: string[]
    /** 松散对象（strict 校验后的规范化回显） */
    criteria: Record<string, unknown>
  }
  handlers: HandlerView[]
  debug: boolean
}

/** POST/PUT/test 请求体（三动词同形；test 吃完整草稿体） */
export interface SubscriptionRequest {
  key: string
  project_key: string
  description: string
  enabled: boolean
  event_filter: {
    domain: string
    event_types: string[]
    criteria: Record<string, unknown>
  }
  handlers: [
    {
      handler_type: 'webhook'
      url: string
      /** 三态：缺省 = 保持（update）；明文 = 轮换；"" = 擦除 */
      secret?: string
      use_secret_for_signing: boolean
      custom_http_headers: HeaderPair[]
    },
  ]
  debug: boolean
}

/** POST …/test 应答（同步单发判定；HTTP 恒 200，成败在体） */
export interface TestOutcome {
  message: string
  ok: boolean
  attempt: {
    status_code: number
    elapsed_millis: number
    error?: string
  }
}

/** 排障记录（webhook.md §7 schema；失败必录、debug:true 成功也录） */
export interface TroubleshootingRecord {
  /** UNIX 毫秒，attempt 开始时刻 */
  timestamp: number
  elapsed_millis: number
  errors: string[]
  request: {
    method: string
    url: string
    headers: Record<string, string[]>
    payload: string
    retries_attempted: number
  }
  response: {
    status: number
    headers: Record<string, string[]>
    body: string
  }
  event: {
    id: string
    subscription_key: string
    domain: string
    event_type: string
    data: Record<string, unknown>
    source: string
  }
}

// ---- 事件类型闭集（eventtypes.go 镜像：13 域 66 型，9 型 wired） ----------

export type EventSource = 'wired' | 'dormant'

export interface EventTypeEntry {
  name: string
  domain: string
  source: EventSource
}

/** 13 域注册序（artifact 族在前——BinFlow 有本体触发源的域） */
export const EVENT_DOMAINS = [
  'artifact',
  'artifact_property',
  'docker',
  'build',
  'release_bundle',
  'release_bundle_v2',
  'release_bundle_v2_promotion',
  'distribution',
  'destination',
  'curation',
  'user',
  'xray_scan_status',
  'app_trust',
] as const

/** 域展示名（UI 分组标签；en 原词保留，console-ux §1.2） */
export const DOMAIN_LABELS: Record<string, string> = {
  artifact: tt('artifact（制品部署/删除/移动/复制/缓存）'),
  artifact_property: tt('artifact_property（属性增删）'),
  docker: tt('docker（tag push/删除）'),
  build: tt('build（Build-info，M14+）'),
  release_bundle: tt('release_bundle（RBv1，不建）'),
  release_bundle_v2: 'release_bundle_v2',
  release_bundle_v2_promotion: 'release_bundle_v2_promotion',
  distribution: tt('distribution（Distribution 外部产品）'),
  destination: tt('destination（Edge 节点，不建）'),
  curation: tt('curation（Curation 外部产品）'),
  user: tt('user（账户锁定）'),
  xray_scan_status: tt('xray_scan_status（Xray，Non-goal）'),
  app_trust: tt('app_trust（AppTrust 外部产品）'),
}

/**
 * 66 型全表（eventtypes.go 的静态镜像；跨域同名型按 (domain,name) 对
 * 解析——表按域分组消费，不会歧义）。wired = BinFlow 有触发源；dormant
 * = 可订阅、校验通过、永不触发（如实标注，不伪造）。
 */
export const EVENT_TYPES: EventTypeEntry[] = [
  // artifact (5) — 全 wired
  { name: 'deployed', domain: 'artifact', source: 'wired' },
  { name: 'deleted', domain: 'artifact', source: 'wired' },
  { name: 'moved', domain: 'artifact', source: 'wired' },
  { name: 'copied', domain: 'artifact', source: 'wired' },
  { name: 'cached', domain: 'artifact', source: 'wired' },
  // artifact_property (2) — 全 wired
  { name: 'added', domain: 'artifact_property', source: 'wired' },
  { name: 'deleted', domain: 'artifact_property', source: 'wired' },
  // docker (3) — pushed/deleted wired；promoted 休眠（promotion REST M14+）
  { name: 'pushed', domain: 'docker', source: 'wired' },
  { name: 'deleted', domain: 'docker', source: 'wired' },
  { name: 'promoted', domain: 'docker', source: 'dormant' },
  // build (3) — 休眠（Build-info M14+）
  { name: 'uploaded', domain: 'build', source: 'dormant' },
  { name: 'deleted', domain: 'build', source: 'dormant' },
  { name: 'promoted', domain: 'build', source: 'dormant' },
  // release_bundle (3) — 休眠
  { name: 'created', domain: 'release_bundle', source: 'dormant' },
  { name: 'signed', domain: 'release_bundle', source: 'dormant' },
  { name: 'deleted', domain: 'release_bundle', source: 'dormant' },
  // release_bundle_v2 (3) — 休眠
  { name: 'release_bundle_v2_started', domain: 'release_bundle_v2', source: 'dormant' },
  { name: 'release_bundle_v2_failed', domain: 'release_bundle_v2', source: 'dormant' },
  { name: 'release_bundle_v2_completed', domain: 'release_bundle_v2', source: 'dormant' },
  // release_bundle_v2_promotion (3) — 休眠
  { name: 'release_bundle_v2_promotion_started', domain: 'release_bundle_v2_promotion', source: 'dormant' },
  { name: 'release_bundle_v2_promotion_failed', domain: 'release_bundle_v2_promotion', source: 'dormant' },
  { name: 'release_bundle_v2_promotion_completed', domain: 'release_bundle_v2_promotion', source: 'dormant' },
  // distribution (7) — 休眠；delete_* 为载荷拼写（webhook.md §10.2）
  { name: 'distribute_started', domain: 'distribution', source: 'dormant' },
  { name: 'distribute_completed', domain: 'distribution', source: 'dormant' },
  { name: 'distribute_aborted', domain: 'distribution', source: 'dormant' },
  { name: 'distribute_failed', domain: 'distribution', source: 'dormant' },
  { name: 'delete_started', domain: 'distribution', source: 'dormant' },
  { name: 'delete_completed', domain: 'distribution', source: 'dormant' },
  { name: 'delete_failed', domain: 'distribution', source: 'dormant' },
  // destination (4) — 休眠
  { name: 'received', domain: 'destination', source: 'dormant' },
  { name: 'delete_started', domain: 'destination', source: 'dormant' },
  { name: 'delete_completed', domain: 'destination', source: 'dormant' },
  { name: 'delete_failed', domain: 'destination', source: 'dormant' },
  // curation (4) — 休眠；官方以标题拼写登记（webhook.md §3.10，中置信）
  { name: 'Package was blocked by Curation', domain: 'curation', source: 'dormant' },
  { name: 'Curation Waiver Request Created', domain: 'curation', source: 'dormant' },
  { name: 'Curation Waiver Request Updated', domain: 'curation', source: 'dormant' },
  { name: 'Curation Policy Changed', domain: 'curation', source: 'dormant' },
  // user (1) — 休眠
  { name: 'locked', domain: 'user', source: 'dormant' },
  // xray_scan_status (4) — 休眠
  { name: 'done', domain: 'xray_scan_status', source: 'dormant' },
  { name: 'failed', domain: 'xray_scan_status', source: 'dormant' },
  { name: 'partial', domain: 'xray_scan_status', source: 'dormant' },
  { name: 'not_supported', domain: 'xray_scan_status', source: 'dormant' },
  // app_trust (24) — 休眠
  { name: 'entry_gate_evaluation_started', domain: 'app_trust', source: 'dormant' },
  { name: 'entry_gate_evaluation_validation_passed', domain: 'app_trust', source: 'dormant' },
  { name: 'entry_gate_evaluation_validation_failed', domain: 'app_trust', source: 'dormant' },
  { name: 'exit_gate_evaluation_started', domain: 'app_trust', source: 'dormant' },
  { name: 'exit_gate_evaluation_validation_passed', domain: 'app_trust', source: 'dormant' },
  { name: 'exit_gate_evaluation_validation_failed', domain: 'app_trust', source: 'dormant' },
  { name: 'application_creation_started', domain: 'app_trust', source: 'dormant' },
  { name: 'application_creation_completed', domain: 'app_trust', source: 'dormant' },
  { name: 'application_creation_failed', domain: 'app_trust', source: 'dormant' },
  { name: 'application_update_started', domain: 'app_trust', source: 'dormant' },
  { name: 'application_update_completed', domain: 'app_trust', source: 'dormant' },
  { name: 'application_update_failed', domain: 'app_trust', source: 'dormant' },
  { name: 'application_deletion_started', domain: 'app_trust', source: 'dormant' },
  { name: 'application_deletion_completed', domain: 'app_trust', source: 'dormant' },
  { name: 'application_deletion_failed', domain: 'app_trust', source: 'dormant' },
  { name: 'version_creation_started', domain: 'app_trust', source: 'dormant' },
  { name: 'version_creation_completed', domain: 'app_trust', source: 'dormant' },
  { name: 'version_creation_failed', domain: 'app_trust', source: 'dormant' },
  { name: 'version_promotion_started', domain: 'app_trust', source: 'dormant' },
  { name: 'version_promotion_completed', domain: 'app_trust', source: 'dormant' },
  { name: 'version_promotion_failed', domain: 'app_trust', source: 'dormant' },
  { name: 'release_started', domain: 'app_trust', source: 'dormant' },
  { name: 'release_completed', domain: 'app_trust', source: 'dormant' },
  { name: 'release_failed', domain: 'app_trust', source: 'dormant' },
]

/** 一域的事件型（注册序） */
export function typesOfDomain(domain: string): EventTypeEntry[] {
  return EVENT_TYPES.filter((t) => t.domain === domain)
}

/** (domain, name) 对是否有本体触发源（列表/详情的 wired 标注） */
export function isWired(domain: string, name: string): boolean {
  return EVENT_TYPES.some((t) => t.domain === domain && t.name === name && t.source === 'wired')
}

/** 表单托管 criteria 五键的域（webhook.md §2.2 criteria-by-domain：本体三域） */
export const CRITERIA_MANAGED_DOMAINS = ['artifact', 'artifact_property', 'docker']

// ---- criteria 表单模型（五键托管；其余域原样透传） ------------------------

/** criteria 的表单形态（artifact 族五键） */
export interface CriteriaForm {
  anyLocal: boolean
  anyRemote: boolean
  repoKeys: string
  includePatterns: string
  excludePatterns: string
}

export const EMPTY_CRITERIA: CriteriaForm = {
  anyLocal: false,
  anyRemote: false,
  repoKeys: '',
  includePatterns: '',
  excludePatterns: '',
}

const csvToList = (s: string): string[] =>
  s
    .split(',')
    .map((v) => v.trim())
    .filter((v) => v !== '')

const listToCsv = (xs: string[] | undefined): string => (xs ?? []).join(', ')

/** 回显 criteria → 表单形态（未知键保留在透传桶，提交时合并回去） */
export function criteriaToForm(raw: Record<string, unknown> | undefined): CriteriaForm {
  const obj = raw ?? {}
  const str = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [])
  return {
    anyLocal: obj.anyLocal === true,
    anyRemote: obj.anyRemote === true,
    repoKeys: listToCsv(str(obj.repoKeys)),
    includePatterns: listToCsv(str(obj.includePatterns)),
    excludePatterns: listToCsv(str(obj.excludePatterns)),
  }
}

/** 表单形态 → wire criteria（五键 + 透传桶里编辑前就有的外来键）。
 *  空选择是合法 wire（服务端只校验形状）——但「空选择不命中任何事件」
 *  （criteria.go 空选择不 admit），调用方在 UI 上提示。 */
export function criteriaFromForm(form: CriteriaForm, passthrough: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(passthrough)) {
    // 托管五键不透传（表单重建）；其余键原样保留（编辑非托管域时不丢配置）
    if (!(k in EMPTY_CRITERIA)) out[k] = v
  }
  if (form.anyLocal) out.anyLocal = true
  if (form.anyRemote) out.anyRemote = true
  const repoKeys = csvToList(form.repoKeys)
  if (repoKeys.length > 0) out.repoKeys = repoKeys
  const includePatterns = csvToList(form.includePatterns)
  if (includePatterns.length > 0) out.includePatterns = includePatterns
  const excludePatterns = csvToList(form.excludePatterns)
  if (excludePatterns.length > 0) out.excludePatterns = excludePatterns
  return out
}

/** criteria 是否一个范围也没选（服务端语义：永不命中——UI 警示） */
export function criteriaEmptyScope(form: CriteriaForm): boolean {
  return !form.anyLocal && !form.anyRemote && csvToList(form.repoKeys).length === 0
}

// ---- 端点封装 ---------------------------------------------------------------

export function listSubscriptions(): Promise<WebhookSubscription[]> {
  return eventJSON<WebhookSubscription[]>('/subscriptions')
}

export function createSubscription(body: SubscriptionRequest): Promise<WebhookSubscription> {
  return eventJSON<WebhookSubscription>('/subscriptions', { method: 'POST', body })
}

/** update 是 204 无体（webhook.md §1 行 4） */
export function updateSubscription(key: string, body: SubscriptionRequest): Promise<void> {
  return eventJSON<void>(`/subscriptions/${encodeURIComponent(key)}`, { method: 'PUT', body })
}

export function deleteSubscription(key: string): Promise<void> {
  return eventJSON<void>(`/subscriptions/${encodeURIComponent(key)}`, { method: 'DELETE' })
}

/** 试发（完整草稿体，非 key 引用——同步单发，HTTP 恒 200 判定在体） */
export function testSubscription(body: SubscriptionRequest): Promise<TestOutcome> {
  return eventJSON<TestOutcome>('/subscriptions/test', { method: 'POST', body })
}

/** 排障记录（按订阅过滤；失败必录、debug:true 成功也录——空≠无投递） */
export function getTroubleshooting(subscription: string, count = 20): Promise<TroubleshootingRecord[]> {
  return eventJSON<TroubleshootingRecord[]>(
    `/troubleshooting?subscription=${encodeURIComponent(subscription)}&count=${count}`,
  )
}

// ---- outbox 死信面（M17 T-496 FR-159.2 / LC-109——P3 FE 解锁：管理面根
// /binflow/api/v1/webhooks/outbox，非事件面根；读面越过 addon 门——锁定实例
// 可见（操作员先看死信再买槽），replay 写面过 webhook 槽门）。 ----

/** GET /api/v1/webhooks/outbox 行投影（internal/webhook DeliveryView） */
export interface OutboxDelivery {
  id: string
  subscription_id: string
  subscription_key: string
  event_type: string
  /** 闭集 pending | delivering | delivered | dead */
  status: string
  attempts: number
  next_attempt_at: string
  last_error: string
  last_status_code: number | null
  created_at: string
  delivered_at: string | null
}

export interface OutboxPage {
  deliveries: OutboxDelivery[]
  /** 恒在场；'' = 尾页（audit 信封规则） */
  nextCursor: string
}

export interface OutboxFilterInput {
  subscription?: string
  status?: string
  eventType?: string
  limit?: number
  cursor?: string
}

/** 死信行查询（filter + keyset 分页，newest-first；越界 limit/未知 status → 400） */
export function getOutboxPage(filter: OutboxFilterInput = {}): Promise<OutboxPage> {
  const params = new URLSearchParams()
  if (filter.subscription) params.set('subscription', filter.subscription)
  if (filter.status) params.set('status', filter.status)
  if (filter.eventType) params.set('event_type', filter.eventType)
  if (filter.limit) params.set('limit', String(filter.limit))
  if (filter.cursor) params.set('cursor', filter.cursor)
  const qs = params.toString()
  return apiJSON<OutboxPage>(`/v1/webhooks/outbox${qs ? `?${qs}` : ''}`)
}

/** 重放死信行（reset 为 pending，attempts 清零即刻投递——存档信封原样重发；
 *  404 无行 / 409 非死态） */
export function replayOutboxDelivery(id: string): Promise<OutboxDelivery> {
  return apiJSON<OutboxDelivery>(`/v1/webhooks/outbox/${encodeURIComponent(id)}/replay`, { method: 'POST' })
}
