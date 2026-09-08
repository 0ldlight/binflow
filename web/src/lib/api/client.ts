// 统一数据请求层（frontend-rewrite-architecture §5：三入口一信封）。
//
// 逻辑平移自现役 lib/api.ts（旧文件不动——P2+ 消费面逐域迁移）：
// - 错误呈现收敛为「状态码 + message 提取」——toApiError 把三种错误体
//   （E-01 errors[] JSON / 用户管理层纯文本 / OAuth error 形）折成单一
//   ApiError，组件层永不感知格式差异；
// - 写请求浏览器自动带同源 Origin（CSRF 主防线）；X-BinFlow-Console 头
//   是前端自身的第二层习惯（服务端记录不强制，ADR-0014 勘误④）；
// - 401 监听：已认证态任何请求 401 = 会话死（TTL 塌缩/吊销）→ 监听方
//   toast + 重登；探活/登录等预期 401 用 silent401 豁免。
// - body 三形态：body(JSON) / formBody(urlencoded，OAuth 族) /
//   rawBody(text/plain，license 装载、AQL 查询体)。
import { API_ROOT, EVENT_ROOT } from './roots'

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

type UnauthorizedListener = () => void
let unauthorizedListener: UnauthorizedListener | null = null

/** AuthProvider 启动时挂载；传 null 卸载（QueryCache onError 同接此口） */
export function setUnauthorizedListener(fn: UnauthorizedListener | null): void {
  unauthorizedListener = fn
}

export interface RequestOptions {
  method?: string
  /** JSON 序列化的请求体（Content-Type 自动置 application/json） */
  body?: unknown
  /** form-urlencoded 表单体（OAuth 族端点官方形态；E-18 revoke 只吃 form） */
  formBody?: Record<string, string>
  /** 原样字节体（license 装载 / AQL text/plain 查询体共享同一信封） */
  rawBody?: string
  signal?: AbortSignal
  /** 预期可能 401 的调用（whoami 探活、登录提交）不触发全局会话过期处理 */
  silent401?: boolean
}

/** 从三种错误体格式（errors[] / 纯文本 / OAuth 形）统一提取 message
 * （折衷器——逻辑与旧 toApiError 逐行平移：errors[0].message 优先，回退
 * 原文本；OAuth 形的语义解包归调用方经 ApiError.raw 处理，如 step-up 腿） */
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

/** 单一信封：root + path → fetch；非 2xx 抛 ApiError（401 走全局监听） */
async function rawRequest(root: string, path: string, opts: RequestOptions = {}): Promise<Response> {
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
  } else if (opts.formBody !== undefined) {
    headers['Content-Type'] = 'application/x-www-form-urlencoded'
    init.body = new URLSearchParams(opts.formBody).toString()
  } else if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(opts.body)
  }
  const res = await fetch(`${root}${path}`, init)
  if (!res.ok) {
    if (res.status === 401 && !opts.silent401) unauthorizedListener?.()
    throw await toApiError(res)
  }
  return res
}

/** 管理面 JSON 端点（默认入口）；非 2xx 抛 ApiError */
export async function apiJSON<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const res = await rawRequest(API_ROOT, path, opts)
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

/** 管理面纯文本端点（如 /api/security/password 的 200 文案） */
export async function apiText(path: string, opts: RequestOptions = {}): Promise<string> {
  const res = await rawRequest(API_ROOT, path, opts)
  return (await res.text()).trim()
}

/** 事件面 JSON 端点（webhook 订阅族——独立根，同一信封） */
export async function eventJSON<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const res = await rawRequest(EVENT_ROOT, path, opts)
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

/** 暴露原始信封（上传 XHR 进度 / 下载流式 sha256 tee 的 P2 接续位） */
export { rawRequest }

/** 错误转用户可读文案（组件层 catch 后统一入口） */
export function errText(err: unknown): string {
  if (err instanceof ApiError) return err.message
  if (err instanceof Error) return err.message
  return String(err)
}
