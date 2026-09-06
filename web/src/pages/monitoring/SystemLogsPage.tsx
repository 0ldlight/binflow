import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { ComponentPropsWithoutRef } from 'react'

import Button from '@mui/material/Button'
import Paper from '@mui/material/Paper'
import Select from '@mui/material/Select'
import TextField from '@mui/material/TextField'

import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, apiJSON } from '../../lib/api'
import type { AuditEvent } from '../../lib/api'
import { monoInputSx } from '../../lib/muiAtoms'
import { getAuditEventsPage } from '../../lib/governance.ts'
import { tr } from '../../i18n'

const tt = tr('monitoring')

// 系统日志查看器（T-459 / FR-145.5——监控组三页之三；7.161.20 活体形态 =
// /ui/admin/monitoring/system_logs「System Logs Viewer」：服务/节点/日志
// 文件三选择器 + 自动刷新倒计时（7s）+ Pause / Refresh now + File Last
// Modified / View Last Updated + 日志尾随窗；探针 reports/agents/t459-probe/）。
//
// 数据源（T-494 / FR-157 勘误——T-459「审计承载」缺位解除）：
// - **主源 = 服务进程日志真身**（GET /api/v1/system/logs，T-493 落地——
//   internal/console LogRing 4096 行环形 + slog 扇出；门 = CapSystemRead，
//   与 audit 读同档）：limit 尾随窗口（1..1000，缺省 200）+ filter 服务端
//   子串（≤256 字符，对最近窗口匹配——「视图所见即下载所载」）+ download
//   附件臂（binflow-service.log，text/plain attachment）。行序 = 环内
//   最早 → 最新；held/capacity/truncated 是环形缓冲的诚实遥测（truncated
//   = 更早日志已淘汰）。
// - **降级路径保留**（T-459 as-built 形态延续）：端点 404（旧二进制未挂
//   该面）→ 会话内粘性回落审计跟踪源（GET /api/v1/audit，append-only，
//   newest-first——本页初版形态），源说明行/空态文案/客户端窗口过滤/
//   Blob 导出随源切回；降级注记明示。403 不降级（两源同门——无权限卡）。
// - 7.161 的 Service/Node/LogFile 三选择器仍不伪造（单实例单日志流，
//   选择器无第二源——如实缺位维持）。
// - 尾随刷新（7s 倒计时 + Pause/继续 + Refresh now）与四态（loading 骨架
//   / 空 / error 错误卡 + 重试 / 403 → 无权限卡）两源同构。
//
// 下载按钮：主源 = 服务端附件臂（<a> 导航落盘——同源 cookie 随行）；降级
// 源 = 窗口 Blob 导出（URL.createObjectURL 点击时创建、下载后即 revoke）。

/** 尾随间隔（7.161 活体 = 7s） */
const TAIL_SECONDS = 7

/** 拉取窗口档（process 面 limit 1..1000；audit 面 limit 1..1000 同界） */
const LIMITS = [100, 200, 500, 1000] as const

/** 过滤词去抖（process 源的 ?filter 是服务端匹配——每词一请求，敲字即发
 *  会打爆；400ms 去抖收口。audit 源是客户端窄化，无需去抖） */
const FILTER_DEBOUNCE_MS = 400

/** 进程日志端点绝对路径（下载附件臂的 <a> href——apiJSON 的 API_ROOT 是
 *  模块私有，页面侧同款字面量；同源相对路径，cookie 随行） */
const LOGS_ENDPOINT = '/binflow/api/v1/system/logs'

/** GET /api/v1/system/logs 的响应体（T-493 wire 契约） */
interface SystemLogsBody {
  /** 尾随窗口行（已过 filter），最早 → 最新；永不为 null */
  lines: string[]
  count: number
  /** 环内现存总行数（窗口外的更早行仍在环里） */
  held: number
  capacity: number
  /** 环形已翻转 = 更早日志被淘汰（诚实缺位注记的遥测位） */
  truncated: boolean
  generatedAt: string
}

function getSystemLogs(limit: number, filter: string, signal?: AbortSignal): Promise<SystemLogsBody> {
  const params = new URLSearchParams()
  params.set('limit', String(limit))
  if (filter) params.set('filter', filter)
  // apiJSON 相对路径（API_ROOT=/binflow/api 前缀）；LOGS_ENDPOINT 只作
  // 下载附件臂 <a> href 的绝对形态
  return apiJSON<SystemLogsBody>(`/v1/system/logs?${params.toString()}`, { signal })
}

/** 一条审计事件 → 日志行（降级源的呈现；mono 单行；detail 折进行尾 JSON 摘要） */
function logLine(e: AuditEvent): string {
  const target = e.repo ? `${e.repo}/${e.path}` : e.path
  const detail =
    e.detail && typeof e.detail === 'object' && Object.keys(e.detail as object).length > 0
      ? ' ' + JSON.stringify(e.detail)
      : ''
  return `${e.time} ${e.action} actor=${e.actor} ${target || '-'}${detail}`
}

/** 降级源的窗口取数（T-459 as-built 形态——getAuditEventsPage newest-first） */
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
  /** process 源的环形遥测（truncated 注记 + 窗口计数文案） */
  const [ring, setRing] = useState<{ held: number; truncated: boolean } | null>(null)
  const fetchSeq = useRef(0)

  // process 源的过滤去抖：敲词 400ms 后收口成一次重取
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
        // 端点不可用（旧二进制未挂该面）→ 粘性降级审计源（T-459 形态延续）。
        // 403 不降级：两源同门（CapSystemRead），无权限卡照常呈现。
        if (apiErr.status === 404) {
          setSource('audit')
          return // source 翻转驱动 fetchNow 重建 → 审计取数接管
        }
        setError(apiErr)
        setPhase(apiErr.status === 403 ? 'forbidden' : 'error')
        // 403（普通 user 深链）：尾随自动停——注定 403 的轮询没有意义；
        // 手动「立即刷新」仍可重试（服务端是终裁）
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

  // 尾随：未暂停时 1s 计拍递减；归零由独立 effect 触发重取（updater 保持
  // 纯函数——StrictMode 双调用不下重复请求）。后台页签的 interval 被浏览器
  // 节流为 ≥1s，对本面无害。
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

  // 过滤呈现：process 源 = 服务端已过滤（shown 即 lines）；audit 源 =
  // 客户端子串窄化（仅已加载窗口——T-459 as-built 边界）
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

  /** 附件臂 URL：与当前视图同参数（limit + filter）——视图所见即下载所载 */
  const downloadHref = `${LOGS_ENDPOINT}?limit=${limit}${serverFilter ? `&filter=${encodeURIComponent(serverFilter)}` : ''}&download=1`

  const filterPlaceholder =
    source === 'process' ? tt('过滤日志行（服务端子串）') : tt('过滤日志行（仅当前窗口）')

  return (
    <div data-testid="logs-page">
      <div className="page-header">
        <h2>{tt('系统日志')}</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>{tt('系统日志查看器（尾随刷新 / 过滤 / 下载）')}        </span>
      </div>

      {/* 源说明行（7.161 三选择器的单源如实降形——不伪造选择器）。主源 =
          进程日志真身（T-494 切换）；降级 = 审计跟踪（T-459 as-built 文案
          原样延续 + 降级注记）。 */}
      {source === 'process' ? (
        <p className="field-hint" data-testid="logs-source" style={{ margin: '0 0 var(--bf-sp-2)' }}>{tt('日志源：')}<span className="mono" lang="en">{tt('服务进程日志（GET /api/v1/system/logs，slog 环形尾随，最早 → 最新）')}</span>{tt('——超出环形容量的更早日志已淘汰；下载为服务端附件（binflow-service.log）。')}          {ring?.truncated && (
            <span className="badge warning" data-testid="logs-truncated" style={{ marginLeft: 'var(--bf-sp-2)' }}>{tt('环形已满，更早日志已被淘汰')}</span>
          )}
        </p>
      ) : (
        <p className="field-hint" data-testid="logs-source" style={{ margin: '0 0 var(--bf-sp-2)' }}>
          <span data-testid="logs-degraded">{tt('进程日志端点不可用（HTTP 404），已回落审计跟踪数据源（T-459 形态延续）。')}</span>{tt('日志源：')}<span className="mono" lang="en">{tt('审计跟踪（GET /api/v1/audit，append-only，最新在前）')}</span>{tt('——服务进程日志（slog 文件）暂无 REST 端点，未列入可选源（契约缺口已登记）。服务端精过滤 （仓库 / 操作者 / 动作 / 时间窗）在审计日志页。')}      </p>
      )}

      <div className="filter-bar" role="toolbar" aria-label={tt('系统日志工具栏')}>
        <Button
          variant="outlined"
          size="small"
          onClick={() => {
            setPaused((p) => !p)
            setCountdown(TAIL_SECONDS)
          }}
          data-testid="logs-pause"
          aria-pressed={paused}
        >
          {paused ? tt('继续') : tt('暂停')}
        </Button>
        <Button variant="outlined" size="small" onClick={doRefreshNow} data-testid="logs-refresh">{tt('立即刷新')}        </Button>
        <span className="text-2" data-testid="logs-countdown" style={{ fontSize: 'var(--bf-fs-aux)' }}>
          {paused ? tt('已暂停尾随') : tt('{countdown} 秒后自动刷新', { countdown: countdown })}
        </span>
        <TextField
          select
          size="small"
          value={limit}
          onChange={(e) => setLimit(Number(e.target.value))}
          sx={{ width: 140 }}
          label={tt('窗口行数')}
          slotProps={{
            select: {
              native: true,
              inputProps: {
                'aria-label': tt('日志窗口行数'),
                'data-testid': 'logs-limit',
              } as ComponentPropsWithoutRef<'select'>,
            } as ComponentPropsWithoutRef<typeof Select>,
          }}
        >
          {LIMITS.map((n) => (
            <option key={n} value={n}>{tt('最近')} {n} {tt('行')}            </option>
          ))}
        </TextField>
        <TextField
          type="search"
          size="small"
          placeholder={filterPlaceholder}
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          sx={{ ...monoInputSx, width: 240 }}
          slotProps={{
            htmlInput: {
              'aria-label': filterPlaceholder,
              'data-testid': 'logs-filter',
              className: 'mono',
            },
          }}
        />
        {source === 'process' ? (
          // 附件臂（T-493 download=1）：同源导航落盘，Content-Disposition
          // 定名 binflow-service.log——与视图同参数，所见即所载
          <Button variant="outlined" size="small" component="a" href={downloadHref} data-testid="logs-download">{tt('下载日志文件')}          </Button>
        ) : (
          <Button variant="outlined" size="small" onClick={doDownloadWindow} data-testid="logs-download">{tt('下载当前窗口')}          </Button>
        )}
        <span className="count" data-testid="logs-updated-at">{tt('视图更新于：')}          <span className="mono" lang="en">
            {fetchedAt ? fetchedAt.toISOString().replace('T', ' ').replace(/\.\d+Z$/, ' UTC') : '—'}
          </span>
        </span>
      </div>

      {phase === 'loading' && <Skeleton lines={8} />}
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
        <Paper
          className="cmd-block"
          elevation={1}
          data-testid="logs-pane"
          sx={{ '& pre': { maxHeight: 480, overflow: 'auto', whiteSpace: 'pre', margin: 0 } }}
        >
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
          {lines.length === 0 && !q ? (
            <div style={{ padding: 'var(--bf-sp-3)' }}>
              <EmptyState
                illustration
                message={tt('暂无日志行')}
                hint={
                  source === 'process'
                    ? tt('实例运行中的进程日志（访问记录、调度与存储事件）进入尾随窗口——发生操作后回到本页或等待自动刷新。')
                    : tt('实例的登录、建仓、上传等操作会记录在审计跟踪里——发生操作后回到本页或等待自动刷新。')
                }
              />
            </div>
          ) : lines.length === 0 && q ? (
            // process 源的空集 = 服务端过滤无命中（空环走上面的 !q 分支；
            // audit 源的 shown 空集 = 客户端窄化无命中——两源同卡不同 hint）
            <div style={{ padding: 'var(--bf-sp-3)' }}>
              <EmptyState
                illustration
                message={tt('当前窗口内无匹配行')}
                hint={
                  source === 'process'
                    ? tt('「{v1}」未命中当前尾随窗口——过滤在服务端对最近窗口做子串匹配；更长历史可调大窗口行数。', { v1: filter.trim() })
                    : tt('「{v1}」未命中最近 {v2} 行——过滤只作用于已加载窗口；更大范围的精过滤走审计日志页。', { v1: filter.trim(), v2: lines.length })
                }
                action={
                  <Button variant="outlined" size="small" onClick={() => setFilter('')}>{tt('清除过滤')}                  </Button>
                }
                testid="logs-filter-empty"
              />
            </div>
          ) : shown.length === 0 ? (
            <div style={{ padding: 'var(--bf-sp-3)' }}>
              <EmptyState
                illustration
                message={tt('当前窗口内无匹配行')}
                hint={
                  source === 'process'
                    ? tt('「{v1}」未命中当前尾随窗口——过滤在服务端对最近窗口做子串匹配；更长历史可调大窗口行数。', { v1: filter.trim() })
                    : tt('「{v1}」未命中最近 {v2} 行——过滤只作用于已加载窗口；更大范围的精过滤走审计日志页。', { v1: filter.trim(), v2: lines.length })
                }
                action={
                  <Button variant="outlined" size="small" onClick={() => setFilter('')}>{tt('清除过滤')}                  </Button>
                }
                testid="logs-filter-empty"
              />
            </div>
          ) : (
            <pre lang="en" tabIndex={0} data-testid="logs-lines">
              {shown.map((l, i) => (
                <span
                  className="log-line"
                  key={i}
                  data-testid={`logs-line-${i}`}
                  style={{ display: 'block' }}
                >
                  {l}
                  {'\n'}
                </span>
              ))}
            </pre>
          )}
        </Paper>
      )}
    </div>
  )
}
