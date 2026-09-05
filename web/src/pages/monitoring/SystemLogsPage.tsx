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
import { ApiError } from '../../lib/api'
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
// BinFlow 载体定案（零新端点——票内裁决，契约漂移登记见工作日志）：
// - **日志源 = 审计跟踪**（GET /api/v1/audit，system:read）——BinFlow 控制
//   台可达的唯一「系统日志」面。服务进程日志（slog 文件）无 REST 端点
//   （internal/httpapi 全量无日志尾随面）——不伪造源选择器（7.161 的
//   Service/Node/LogFile 三选择器如实降为单源说明行 logs-source）；真要
//   服务进程日志须 PM FR 增补 + 后端新端点族，已登记建议票。
// - **尾随刷新**：7s 倒计时自动重取（新到顶——audit API newest-first，
//   与审计页同序）+ Pause/继续 + Refresh now（7.161 三件套逐一对位；
//   倒计时文案对位 "Refreshing Logs in N seconds"）。
// - **过滤**：文本子串过滤当前窗口（客户端——与审计页 path 过滤同款
//   「仅已加载集」边界，hint 明示）；服务端精过滤走审计页（repo/actor/
//   action/时间窗）。
// - **下载**：当前窗口导出 .log（Blob 客户端落盘——BinFlow 无 Support
//   Zone，7.161 的「去 Support Zone 下载」如实换形；导出的是已取窗口，
//   非全量文件）。
// - 四态：loading 骨架 / 空（实例无审计事件）/ error 错误卡 + 重试 /
//   403 → L2 无权限卡。readonly_admin 读面全通。
//
// 下载按钮的 URL.createObjectURL 在点击时创建、下载后即 revoke（无泄漏）。

/** 尾随间隔（7.161 活体 = 7s） */
const TAIL_SECONDS = 7

/** 拉取窗口档（audit limit 1..1000；100 = 审计页缺省同源） */
const LIMITS = [100, 200, 500, 1000] as const

/** 一条审计事件 → 日志行（mono 单行；detail 折进行尾 JSON 摘要） */
function logLine(e: AuditEvent): string {
  const target = e.repo ? `${e.repo}/${e.path}` : e.path
  const detail =
    e.detail && typeof e.detail === 'object' && Object.keys(e.detail as object).length > 0
      ? ' ' + JSON.stringify(e.detail)
      : ''
  return `${e.time} ${e.action} actor=${e.actor} ${target || '-'}${detail}`
}

export default function SystemLogsPage() {
  const [limit, setLimit] = useState<number>(100)
  const [filter, setFilter] = useState('')
  const [paused, setPaused] = useState(false)
  const [countdown, setCountdown] = useState(TAIL_SECONDS)

  const [events, setEvents] = useState<AuditEvent[]>([])
  const [phase, setPhase] = useState<'loading' | 'ok' | 'error' | 'forbidden'>('loading')
  const [error, setError] = useState<ApiError | null>(null)
  const [fetchedAt, setFetchedAt] = useState<Date | null>(null)
  const fetchSeq = useRef(0)

  const fetchNow = useCallback(async () => {
    const seq = ++fetchSeq.current
    setPhase('loading')
    try {
      const page = await getAuditEventsPage(
        { repo: '', actor: '', action: '', since: '', until: '' },
        '',
        limit,
      )
      if (seq !== fetchSeq.current) return // 晚到响应丢弃（limit 已变）
      setEvents(page.events)
      setError(null)
      setPhase('ok')
      setFetchedAt(new Date())
    } catch (err) {
      if (seq !== fetchSeq.current) return
      const apiErr = err instanceof ApiError ? err : new ApiError(0, String(err))
      setError(apiErr)
      setPhase(apiErr.status === 403 ? 'forbidden' : 'error')
      // 403（普通 user 深链）：尾随自动停——注定 403 的轮询没有意义；
      // 手动「立即刷新」仍可重试（服务端是终裁）
      if (apiErr.status === 403) setPaused(true)
    }
  }, [limit])

  // 首取 + limit 变化重取
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

  // 客户端子串过滤（仅已加载窗口——hint 明示边界）
  const q = filter.trim().toLowerCase()
  const lines = useMemo(() => events.map(logLine), [events])
  const shown = q ? lines.filter((l) => l.toLowerCase().includes(q)) : lines

  const doDownload = () => {
    const text = `${shown.join('\n')}\n`
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain;charset=utf-8' }))
    const a = document.createElement('a')
    a.href = url
    a.download = `binflow-system-log-${new Date().toISOString().replace(/[:.]/g, '-')}.log`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div data-testid="logs-page">
      <div className="page-header">
        <h2>{tt('系统日志')}</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>{tt('系统日志查看器（尾随刷新 / 过滤 / 下载）')}        </span>
      </div>

      {/* 源说明行（7.161 三选择器的单源如实降形——不伪造选择器） */}
      <p className="field-hint" data-testid="logs-source" style={{ margin: '0 0 var(--bf-sp-2)' }}>{tt('日志源：')}<span className="mono" lang="en">{tt('审计跟踪（GET /api/v1/audit，append-only，最新在前）')}</span>{tt('——服务进程日志（slog 文件）暂无 REST 端点，未列入可选源（契约缺口已登记）。服务端精过滤 （仓库 / 操作者 / 动作 / 时间窗）在审计日志页。')}      </p>

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
          placeholder={tt('过滤日志行（仅当前窗口）')}
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          sx={{ ...monoInputSx, width: 240 }}
          slotProps={{
            htmlInput: {
              'aria-label': tt('过滤日志行（仅当前窗口）'),
              'data-testid': 'logs-filter',
              className: 'mono',
            },
          }}
        />
        <Button variant="outlined" size="small" onClick={doDownload} data-testid="logs-download">{tt('下载当前窗口')}        </Button>
        <span className="count" data-testid="logs-updated-at">{tt('视图更新于：')}          <span className="mono" lang="en">
            {fetchedAt ? fetchedAt.toISOString().replace('T', ' ').replace(/\.\d+Z$/, ' UTC') : '—'}
          </span>
        </span>
      </div>

      {phase === 'loading' && <Skeleton lines={8} />}
      {phase === 'forbidden' && error && (
        <EmptyState
          message={tt('无权限查看系统日志')}
          hint={tt('日志源（审计查询面）为管理员视图（GET /api/v1/audit 仅 admin / readonly_admin）。')}
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
              {shown.length} {tt('行')}{q ? tt('（窗口 {v1} 行，过滤命中 {v2}）', { v1: lines.length, v2: shown.length }) : tt('（最近 {v1} 行）', { v1: lines.length })}
            </span>
            <span>
              {shown.length > 0 && (
                <CopyButton value={shown.join('\n')} label={tt('当前窗口日志')} />
              )}
            </span>
          </header>
          {lines.length === 0 ? (
            <div style={{ padding: 'var(--bf-sp-3)' }}>
              <EmptyState
                illustration
                message={tt('暂无日志行')}
                hint={tt('实例的登录、建仓、上传等操作会记录在审计跟踪里——发生操作后回到本页或等待自动刷新。')}
              />
            </div>
          ) : shown.length === 0 ? (
            <div style={{ padding: 'var(--bf-sp-3)' }}>
              <EmptyState
                illustration
                message={tt('当前窗口内无匹配行')}
                hint={tt('「{v1}」未命中最近 {v2} 行——过滤只作用于已加载窗口；更大范围的精过滤走审计日志页。', { v1: filter.trim(), v2: lines.length })}
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
