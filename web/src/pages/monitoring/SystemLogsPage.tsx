// 系统日志查看器（T-459 / T-494——P3 新栈重写；7.161 System Logs Viewer
// 对位：自动刷新倒计时 7s + Pause / Refresh now + 尾随窗）：
// - 主源 = 服务进程日志真身（GET /api/v1/system/logs，T-493：LogRing
//   4096 行环形 + slog 扇出）：limit 尾随窗口（1..1000 缺省 200）+ filter
//   服务端子串（400ms 去抖）+ download=1 附件臂；held/truncated 环形遥测。
// - 降级路径保留：端点 404（旧二进制未挂该面）→ 会话内粘性回落审计跟踪
//   源（GET /api/v1/audit newest-first）；403 不降级（两源同门）。
// - 7.161 的 Service/Node/LogFile 三选择器仍不伪造（单实例单日志流）。
// 锚族原样：logs-page/logs-source/logs-degraded/logs-truncated/logs-pause/
// logs-refresh/logs-countdown/logs-limit/logs-filter/logs-download/
// logs-updated-at/logs-pane/logs-lines/logs-line-<i>/logs-filter-empty。
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { ApiError, apiJSON } from '@/lib/api'
import type { AuditEvent } from '@/lib/api'
import { getAuditEventsPage } from '@/lib/governance'
import { tr } from '@/i18n'

const tt = tr('monitoring')

/** 尾随间隔（7.161 活体 = 7s） */
const TAIL_SECONDS = 7

/** 拉取窗口档 */
const LIMITS = [100, 200, 500, 1000] as const

/** 过滤词去抖（process 源 ?filter 服务端匹配——每词一请求） */
const FILTER_DEBOUNCE_MS = 400

/** 进程日志端点绝对路径（下载附件臂的 <a> href） */
const LOGS_ENDPOINT = '/binflow/api/v1/system/logs'

/** GET /api/v1/system/logs 的响应体（T-493 wire 契约） */
interface SystemLogsBody {
  /** 尾随窗口行（已过 filter），最早 → 最新；永不为 null */
  lines: string[]
  count: number
  held: number
  capacity: number
  truncated: boolean
  generatedAt: string
}

function getSystemLogs(limit: number, filter: string, signal?: AbortSignal): Promise<SystemLogsBody> {
  const params = new URLSearchParams()
  params.set('limit', String(limit))
  if (filter) params.set('filter', filter)
  return apiJSON<SystemLogsBody>(`/v1/system/logs?${params.toString()}`, { signal })
}

/** 一条审计事件 → 日志行（降级源的呈现） */
function logLine(e: AuditEvent): string {
  const target = e.repo ? `${e.repo}/${e.path}` : e.path
  const detail =
    e.detail && typeof e.detail === 'object' && Object.keys(e.detail as object).length > 0
      ? ' ' + JSON.stringify(e.detail)
      : ''
  return `${e.time} ${e.action} actor=${e.actor} ${target || '-'}${detail}`
}

/** 降级源的窗口取数（getAuditEventsPage newest-first） */
async function fetchAuditLines(limit: number): Promise<string[]> {
  const page = await getAuditEventsPage(
    { repo: '', actor: '', action: '', since: '', until: '' },
    '',
    limit,
  )
  return page.events.map(logLine)
}

export default function SystemLogsPage() {
  const [limit, setLimit] = useState<number>(100)
  const [filter, setFilter] = useState('')
  /** process 源的服务端过滤词（去抖后）；audit 源直接用 filter 客户端窄化 */
  const [serverFilter, setServerFilter] = useState('')
  const [paused, setPaused] = useState(false)
  const [countdown, setCountdown] = useState(TAIL_SECONDS)

  /** 当前数据源：process 主源 / audit 降级（404 后会话内粘性） */
  const [source, setSource] = useState<'process' | 'audit'>('process')
  const [lines, setLines] = useState<string[]>([])
  const [phase, setPhase] = useState<'loading' | 'ok' | 'error' | 'forbidden'>('loading')
  const [error, setError] = useState<ApiError | null>(null)
  const [fetchedAt, setFetchedAt] = useState<Date | null>(null)
  const [ring, setRing] = useState<{ held: number; truncated: boolean } | null>(null)
  const fetchSeq = useRef(0)

  // process 源的过滤去抖
  useEffect(() => {
    const t = window.setTimeout(() => setServerFilter(filter.trim()), FILTER_DEBOUNCE_MS)
    return () => window.clearTimeout(t)
  }, [filter])

  const fetchNow = useCallback(async () => {
    const seq = ++fetchSeq.current
    setPhase('loading')
    if (source === 'process') {
      try {
        const body = await getSystemLogs(limit, serverFilter)
        if (seq !== fetchSeq.current) return // 晚到响应丢弃（limit/filter 已变）
        setLines(body.lines ?? [])
        setRing({ held: body.held, truncated: body.truncated })
        setError(null)
        setPhase('ok')
        setFetchedAt(new Date())
      } catch (err) {
        if (seq !== fetchSeq.current) return
        const apiErr = err instanceof ApiError ? err : new ApiError(0, String(err))
        // 端点不可用（旧二进制未挂该面）→ 粘性降级审计源。403 不降级。
        if (apiErr.status === 404) {
          setSource('audit')
          return // source 翻转驱动 fetchNow 重建 → 审计取数接管
        }
        setError(apiErr)
        setPhase(apiErr.status === 403 ? 'forbidden' : 'error')
        // 403（普通 user 深链）：尾随自动停——注定 403 的轮询没有意义
        if (apiErr.status === 403) setPaused(true)
      }
      return
    }
    try {
      const auditLines = await fetchAuditLines(limit)
      if (seq !== fetchSeq.current) return
      setLines(auditLines)
      setRing(null)
      setError(null)
      setPhase('ok')
      setFetchedAt(new Date())
    } catch (err) {
      if (seq !== fetchSeq.current) return
      const apiErr = err instanceof ApiError ? err : new ApiError(0, String(err))
      setError(apiErr)
      setPhase(apiErr.status === 403 ? 'forbidden' : 'error')
      if (apiErr.status === 403) setPaused(true)
    }
  }, [limit, serverFilter, source])

  // 首取 + limit/过滤词/源变化重取
  useEffect(() => {
    void fetchNow()
  }, [fetchNow])

  // 尾随：未暂停时 1s 计拍递减；归零触发重取
  useEffect(() => {
    if (paused) return
    const t = window.setInterval(() => {
      setCountdown((c) => c - 1)
    }, 1000)
    return () => window.clearInterval(t)
  }, [paused])

  useEffect(() => {
    if (paused || countdown > 0) return
    setCountdown(TAIL_SECONDS)
    void fetchNow()
  }, [countdown, paused, fetchNow])

  const doRefreshNow = () => {
    setCountdown(TAIL_SECONDS)
    void fetchNow()
  }

  // 过滤呈现：process 源 = 服务端已过滤；audit 源 = 客户端子串窄化
  const q = filter.trim().toLowerCase()
  const shown = useMemo(
    () => (source === 'audit' && q ? lines.filter((l) => l.toLowerCase().includes(q)) : lines),
    [source, lines, q],
  )

  const doDownloadWindow = () => {
    const text = `${shown.join('\n')}\n`
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain;charset=utf-8' }))
    const a = document.createElement('a')
    a.href = url
    a.download = `binflow-system-log-${new Date().toISOString().replace(/[:.]/g, '-')}.log`
    a.click()
    URL.revokeObjectURL(url)
  }

  /** 附件臂 URL：与当前视图同参数——视图所见即下载所载 */
  const downloadHref = `${LOGS_ENDPOINT}?limit=${limit}${serverFilter ? `&filter=${encodeURIComponent(serverFilter)}` : ''}&download=1`

  const filterPlaceholder =
    source === 'process' ? tt('过滤日志行（服务端子串）') : tt('过滤日志行（仅当前窗口）')

  const emptyHint = (isProcess: boolean) =>
    isProcess
      ? tt('「{v1}」未命中当前尾随窗口——过滤在服务端对最近窗口做子串匹配；更长历史可调大窗口行数。', { v1: filter.trim() })
      : tt('「{v1}」未命中最近 {v2} 行——过滤只作用于已加载窗口；更大范围的精过滤走审计日志页。', { v1: filter.trim(), v2: lines.length })

  return (
    <div data-testid="logs-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{tt('系统日志')}</h2>
        <span className="text-aux text-2">{tt('系统日志查看器（尾随刷新 / 过滤 / 下载）')}</span>
      </div>

      {/* 源说明行（7.161 三选择器的单源如实降形——不伪造选择器） */}
      {source === 'process' ? (
        <p className="field-hint mb-2 mt-0" data-testid="logs-source">
          {tt('日志源：')}<span className="font-mono" lang="en">{tt('服务进程日志（GET /api/v1/system/logs，slog 环形尾随，最早 → 最新）')}</span>{tt('——超出环形容量的更早日志已淘汰；下载为服务端附件（binflow-service.log）。')}
          {ring?.truncated && (
            <span className="badge warning ml-2" data-testid="logs-truncated">{tt('环形已满，更早日志已被淘汰')}</span>
          )}
        </p>
      ) : (
        <p className="field-hint mb-2 mt-0" data-testid="logs-source">
          <span data-testid="logs-degraded">{tt('进程日志端点不可用（HTTP 404），已回落审计跟踪数据源（T-459 形态延续）。')}</span>
          {tt('日志源：')}<span className="font-mono" lang="en">{tt('审计跟踪（GET /api/v1/audit，append-only，最新在前）')}</span>
          {tt('——服务进程日志（slog 文件）暂无 REST 端点，未列入可选源（契约缺口已登记）。服务端精过滤 （仓库 / 操作者 / 动作 / 时间窗）在审计日志页。')}
        </p>
      )}

      <div className="filter-bar" role="toolbar" aria-label={tt('系统日志工具栏')}>
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            setPaused((p) => !p)
            setCountdown(TAIL_SECONDS)
          }}
          data-testid="logs-pause"
          aria-pressed={paused}
        >
          {paused ? tt('继续') : tt('暂停')}
        </Button>
        <Button variant="outline" size="sm" onClick={doRefreshNow} data-testid="logs-refresh">{tt('立即刷新')}</Button>
        <span className="text-aux text-2" data-testid="logs-countdown">
          {paused ? tt('已暂停尾随') : tt('{countdown} 秒后自动刷新', { countdown: countdown })}
        </span>
        <select
          value={limit}
          onChange={(e) => setLimit(Number(e.target.value))}
          className="h-8 rounded-sm border border-input bg-surface-3 px-2 text-dense"
          aria-label={tt('日志窗口行数')}
          data-testid="logs-limit"
        >
          {LIMITS.map((n) => (
            <option key={n} value={n}>{tt('最近')} {n} {tt('行')}</option>
          ))}
        </select>
        <Input
          type="search"
          placeholder={filterPlaceholder}
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="w-[240px] font-mono"
          aria-label={filterPlaceholder}
          data-testid="logs-filter"
        />
        {source === 'process' ? (
          // 附件臂（download=1）：同源导航落盘——与视图同参数，所见即所载
          <ButtonAsAnchor href={downloadHref} data-testid="logs-download">{tt('下载日志文件')}</ButtonAsAnchor>
        ) : (
          <Button variant="outline" size="sm" onClick={doDownloadWindow} data-testid="logs-download">{tt('下载当前窗口')}</Button>
        )}
        <span className="count text-aux text-2" data-testid="logs-updated-at">
          {tt('视图更新于：')}
          <span className="font-mono" lang="en">
            {fetchedAt ? fetchedAt.toISOString().replace('T', ' ').replace(/\.\d+Z$/, ' UTC') : '—'}
          </span>
        </span>
      </div>

      {phase === 'loading' && <StateSkeleton lines={8} />}
      {phase === 'forbidden' && error && (
        <EmptyState
          message={tt('无权限查看系统日志')}
          hint={
            source === 'process'
              ? tt('日志源（服务进程日志端点 GET /api/v1/system/logs）为管理员视图（仅 admin / readonly_admin）。')
              : tt('日志源（审计查询面）为管理员视图（GET /api/v1/audit 仅 admin / readonly_admin）。')
          }
        />
      )}
      {phase === 'error' && error && <ErrorCard error={error} onRetry={doRefreshNow} />}

      {phase === 'ok' && (
        <div className="cmd-block" data-testid="logs-pane">
          <header>
            <span>
              {shown.length} {tt('行')}
              {source === 'process'
                ? q
                  ? tt('（过滤命中 {v1}）', { v1: shown.length })
                  : ring
                    ? tt('（环形 {v1} 行，取最近 {v2}）', { v1: ring.held, v2: shown.length })
                    : ''
                : q
                  ? tt('（窗口 {v1} 行，过滤命中 {v2}）', { v1: lines.length, v2: shown.length })
                  : tt('（最近 {v1} 行）', { v1: lines.length })}
            </span>
            <span>
              {shown.length > 0 && (
                <CopyButton value={shown.join('\n')} label={tt('当前窗口日志')} />
              )}
            </span>
          </header>
          {shown.length === 0 ? (
            <div className="p-3">
              <EmptyState
                illustration
                message={lines.length === 0 && !q ? tt('暂无日志行') : tt('当前窗口内无匹配行')}
                hint={
                  lines.length === 0 && !q
                    ? source === 'process'
                      ? tt('实例运行中的进程日志（访问记录、调度与存储事件）进入尾随窗口——发生操作后回到本页或等待自动刷新。')
                      : tt('实例的登录、建仓、上传等操作会记录在审计跟踪里——发生操作后回到本页或等待自动刷新。')
                    : emptyHint(source === 'process')
                }
                action={
                  <Button variant="outline" size="sm" onClick={() => setFilter('')}>{tt('清除过滤')}</Button>
                }
                testid="logs-filter-empty"
              />
            </div>
          ) : (
            <pre lang="en" tabIndex={0} data-testid="logs-lines" className="max-h-[480px] overflow-auto whitespace-pre">
              {shown.map((l, i) => (
                <span className="log-line block" key={i} data-testid={`logs-line-${i}`}>
                  {l}
                  {'\n'}
                </span>
              ))}
            </pre>
          )}
        </div>
      )}
    </div>
  )
}

/** <a> 承载的按钮形态（附件臂下载——同源导航，cookie 随行）；锚属性
 *  经 ...rest 透传（data-testid 等落 <a> 本体） */
function ButtonAsAnchor({ href, children, ...rest }: React.ComponentProps<'a'> & { href: string }) {
  return (
    <a
      href={href}
      {...rest}
      className="inline-flex h-7 items-center rounded-sm border border-input bg-surface-1 px-2 text-dense font-medium hover:bg-surface-2"
    >
      {children}
    </a>
  )
}
