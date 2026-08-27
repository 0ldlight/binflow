import { useEffect, useMemo, useRef, useState } from 'react'
import type { ComponentPropsWithoutRef } from 'react'

import Button from '@mui/material/Button'
import Select from '@mui/material/Select'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import TextField from '@mui/material/TextField'

import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, getRepositories } from '../../lib/api'
import type { AuditEvent } from '../../lib/api'
import { formatAuditTime } from '../../lib/format'
import { denseInputSx, monoInputSx, rowBtnSx } from '../../lib/muiAtoms'
import {
  AUDIT_ACTIONS,
  AUDIT_PAGE_SIZE,
  EMPTY_AUDIT_FILTERS,
  getAuditEventsPage,
  localInputToRFC3339,
} from '../../lib/governance.ts'
import type { AuditFilters } from '../../lib/governance.ts'
import { useAsync } from '../../lib/useAsync'

// 审计日志（console-ux §4.10 / §5.3；T-102 AC①）：
// - 过滤器：repo/actor（**精确匹配**——REST 契约是 `repo_key = ?`，输入框
//   配 datalist 建议 + 文案明示）/ action（词表选择器，GE-02）/ 时间窗
//   （since 含 until 不含，datetime-local 折算 UTC）/ path（**已加载集**
//   客户端子串——REST 无 path 参数，§6.3 兜底，显式提示边界）。
// - 分页：keyset cursor「加载更多」增量追加（§6.1 不做页码跳转）。
// - 表格：时间 mono / 动作原样 mono（不翻译 enum，排障要比对）/ 对象
//   repo/path mono + 拷贝 / 来源 detail.remote_addr / detail JSON 折叠。
// - CSV 导出 P2 债务：不渲染入口（ux R3）。
// - 非 admin：GET /api/v1/audit 403 → 单张无权限卡（§3.6.3 L2）。

/** detail 收窄：remote_addr 是 Logger 合并进 Detail 的来源地址（M1 契约） */
function detailObject(d: unknown): Record<string, unknown> {
  return d && typeof d === 'object' && !Array.isArray(d) ? (d as Record<string, unknown>) : {}
}

function detailRemoteAddr(d: unknown): string {
  const v = detailObject(d).remote_addr
  return typeof v === 'string' ? v : ''
}

function detailJSON(d: unknown): string {
  const obj = detailObject(d)
  if (Object.keys(obj).length === 0) return ''
  try {
    return JSON.stringify(obj, null, 2)
  } catch {
    return ''
  }
}

/**
 * 服务端分页容器：首页随 filters 变化重拉（防抖由调用方做），「加载更多」
 * 携 cursor 增量追加。晚到的旧响应按运行闭包旗标丢弃（useAsync 同款语义）。
 */
function useAuditPages(filters: AuditFilters) {
  const [events, setEvents] = useState<AuditEvent[]>([])
  const [nextCursor, setNextCursor] = useState('')
  const [phase, setPhase] = useState<'loading' | 'ok' | 'error' | 'forbidden'>('loading')
  const [error, setError] = useState<ApiError | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const [moreError, setMoreError] = useState<ApiError | null>(null)
  const key = JSON.stringify(filters)
  // 供 loadMore 读取最新游标与过滤（避免依赖数组抖动）
  const stateRef = useRef({ key, nextCursor })
  stateRef.current = { key, nextCursor }

  useEffect(() => {
    let alive = true
    setPhase('loading')
    setError(null)
    setMoreError(null)
    setEvents([])
    setNextCursor('')
    getAuditEventsPage(filters, '', AUDIT_PAGE_SIZE)
      .then((p) => {
        if (!alive) return
        setEvents(p.events)
        setNextCursor(p.nextCursor)
        setPhase('ok')
      })
      .catch((err: unknown) => {
        if (!alive) return
        const apiErr = err instanceof ApiError ? err : new ApiError(0, String(err))
        setError(apiErr)
        setPhase(apiErr.status === 403 ? 'forbidden' : 'error')
      })
    return () => {
      alive = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- key 即 filters 的全量刻画
  }, [key])

  const loadMore = async (): Promise<void> => {
    const { key: k, nextCursor: cursor } = stateRef.current
    if (cursor === '' || loadingMore) return
    setLoadingMore(true)
    setMoreError(null)
    try {
      const p = await getAuditEventsPage(JSON.parse(k) as AuditFilters, cursor, AUDIT_PAGE_SIZE)
      // 晚到丢弃守卫（review B1）：请求在飞时过滤变更，首页 effect 已清空
      // events/nextCursor——旧过滤的第 2 页不得追加进新列表，更不得用旧
      // 游标覆写 nextCursor（否则后续加载更多在新过滤下延续旧游标链）。
      // 过滤键比对与 useAsync 的 alive 闭包旗标同语义；finally 仍复位
      // loadingMore，不卡按钮。
      if (stateRef.current.key !== k) return
      setEvents((prev) => [...prev, ...p.events])
      setNextCursor(p.nextCursor)
    } catch (err) {
      setMoreError(err instanceof ApiError ? err : new ApiError(0, String(err)))
    } finally {
      setLoadingMore(false)
    }
  }

  return { events, nextCursor, phase, error, loadingMore, moreError, loadMore }
}

export default function AuditPage() {
  // 原始输入态
  const [repo, setRepo] = useState('')
  const [actor, setActor] = useState('')
  const [action, setAction] = useState('')
  const [path, setPath] = useState('')
  const [sinceLocal, setSinceLocal] = useState('')
  const [untilLocal, setUntilLocal] = useState('')

  // repo 建议列表（admin 面；失败不阻塞审计页——只是没有补全）
  const repos = useAsync(getRepositories, [])
  const repoOptions = useMemo(
    () => (repos.data ?? []).map((r) => r.key).sort(),
    [repos.data],
  )

  const sinceRFC = localInputToRFC3339(sinceLocal)
  const untilRFC = localInputToRFC3339(untilLocal)
  const timeInvalid = sinceRFC === null || untilRFC === null

  // 服务端过滤面（repo/actor/action/时间窗），350ms 防抖提交
  const [committed, setCommitted] = useState<AuditFilters>(EMPTY_AUDIT_FILTERS)
  useEffect(() => {
    const t = window.setTimeout(() => {
      setCommitted({
        repo: repo.trim(),
        actor: actor.trim(),
        action,
        since: sinceRFC ?? '',
        until: untilRFC ?? '',
      })
    }, 350)
    return () => window.clearTimeout(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 防抖输入面，RFC 值由前两者派生
  }, [repo, actor, action, sinceLocal, untilLocal])

  // 时间值由 datetime-local 产出，浏览器侧不可产出非法串；timeInvalid
  // 只作行内提示（防手动改 DOM 等异常路径），不参与查询门控
  const { events, nextCursor, phase, error, loadingMore, moreError, loadMore } = useAuditPages(committed)

  // path 过滤：只作用于已加载集（§6.3——绝不假装过滤了全量）
  const pathQ = path.trim().toLowerCase()
  const rows = pathQ ? events.filter((e) => e.path.toLowerCase().includes(pathQ)) : events

  const hasServerFilter =
    committed.repo !== '' || committed.actor !== '' || committed.action !== '' || committed.since !== '' || committed.until !== ''
  const hasFilter = hasServerFilter || pathQ !== ''
  const timeHint =
    committed.since || committed.until
      ? `窗口（UTC）：${committed.since || '…'} ≤ time < ${committed.until || '…'}`
      : ''

  const clearFilters = () => {
    setRepo('')
    setActor('')
    setAction('')
    setPath('')
    setSinceLocal('')
    setUntilLocal('')
  }

  return (
    <div data-testid="audit-page">
      <div className="page-header">
        <h2>审计</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>
          只增不改（append-only）；动作值原样呈现
        </span>
      </div>

      <div className="filter-bar">
        <TextField
          size="small"
          placeholder="仓库 key（精确）"
          value={repo}
          onChange={(e) => setRepo(e.target.value)}
          sx={{ ...denseInputSx, ...monoInputSx, width: 180 }}
          slotProps={{
            htmlInput: {
              list: 'audit-repo-options',
              'aria-label': '按仓库过滤（精确匹配）',
              'data-testid': 'audit-filter-repo',
              lang: 'en',
              className: 'mono',
            },
          }}
        />
        <datalist id="audit-repo-options">
          {repoOptions.map((k) => (
            <option key={k} value={k} />
          ))}
        </datalist>
        <TextField
          size="small"
          placeholder="操作者（精确）"
          value={actor}
          onChange={(e) => setActor(e.target.value)}
          sx={{ ...denseInputSx, width: 160 }}
          slotProps={{
            htmlInput: {
              'aria-label': '按操作者过滤（精确匹配）',
              'data-testid': 'audit-filter-actor',
              lang: 'en',
            },
          }}
        />
        <TextField
          select
          size="small"
          value={action}
          onChange={(e) => setAction(e.target.value)}
          sx={{ ...denseInputSx, width: 180 }}
          slotProps={{
            select: {
              native: true,
              inputProps: {
                'aria-label': '按动作过滤',
                'data-testid': 'audit-filter-action',
              } as ComponentPropsWithoutRef<'select'>,
            } as ComponentPropsWithoutRef<typeof Select>,
          }}
        >
          <option value="">动作：全部</option>
          {AUDIT_ACTIONS.map((a) => (
            <option key={a} value={a} lang="en">
              {a}
            </option>
          ))}
        </TextField>
        <label className="time-field">
          <span>起（含）</span>
          <TextField
            size="small"
            type="datetime-local"
            value={sinceLocal}
            onChange={(e) => setSinceLocal(e.target.value)}
            sx={{ ...denseInputSx, width: 200 }}
            slotProps={{ htmlInput: { 'aria-label': '起始时间（含）', 'data-testid': 'audit-filter-since' } }}
          />
        </label>
        <label className="time-field">
          <span>止（不含）</span>
          <TextField
            size="small"
            type="datetime-local"
            value={untilLocal}
            onChange={(e) => setUntilLocal(e.target.value)}
            sx={{ ...denseInputSx, width: 200 }}
            slotProps={{ htmlInput: { 'aria-label': '截止时间（不含）', 'data-testid': 'audit-filter-until' } }}
          />
        </label>
        <TextField
          type="search"
          size="small"
          placeholder="对象路径包含（仅已加载）"
          value={path}
          onChange={(e) => setPath(e.target.value)}
          sx={{ ...denseInputSx, ...monoInputSx, width: 200 }}
          slotProps={{
            htmlInput: {
              'aria-label': '按对象路径过滤（仅已加载条目）',
              'data-testid': 'audit-filter-path',
              lang: 'en',
              className: 'mono',
            },
          }}
        />
        {hasFilter && (
          <Button variant="outlined" size="small" sx={rowBtnSx} onClick={clearFilters}>
            清除过滤
          </Button>
        )}
        <span className="count" data-testid="audit-count">
          已加载 {rows.length} 条{pathQ ? '（路径过滤仅作用于已加载集）' : ''}
        </span>
      </div>
      {(timeInvalid || timeHint) && (
        <p className="field-hint" style={{ margin: '0 0 var(--bf-sp-2)' }}>
          {timeInvalid
            ? '时间格式无法解析——修正后才会发起查询'
            : timeHint}
        </p>
      )}

      {phase === 'loading' && <Skeleton lines={8} />}
      {phase === 'forbidden' && error && (
        <EmptyState
          message="无权限查看审计日志"
          hint="审计查询面为管理员视图（GET /api/v1/audit 仅 admin）。"
        />
      )}
      {phase === 'error' && error && <ErrorCard error={error} />}
      {phase === 'ok' &&
        (events.length === 0 ? (
          hasFilter ? (
            <EmptyState
              message="当前过滤条件下无匹配事件"
              hint="仓库 / 操作者为精确匹配；时间窗为闭开区间（起含、止不含）。"
              action={
                <Button variant="outlined" size="small" sx={rowBtnSx} onClick={clearFilters}>
                  清除过滤
                </Button>
              }
              testid="audit-empty-filtered"
            />
          ) : (
            <EmptyState
              message="暂无审计事件"
              hint="登录、建仓、上传等操作会记录在这里"
            />
          )
        ) : (
          <>
            <Table className="table" data-testid="audit-table">
              <TableHead>
                <TableRow>
                  <TableCell component="th" scope="col">时间</TableCell>
                  <TableCell component="th" scope="col">操作者</TableCell>
                  <TableCell component="th" scope="col">动作</TableCell>
                  <TableCell component="th" scope="col">对象</TableCell>
                  <TableCell component="th" scope="col">来源</TableCell>
                  <TableCell component="th" scope="col">详情</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {rows.map((ev, i) => {
                  const target = ev.repo ? `${ev.repo}/${ev.path}` : ev.path
                  const json = detailJSON(ev.detail)
                  const remote = detailRemoteAddr(ev.detail)
                  return (
                    <TableRow key={ev.id} data-testid={`audit-row-${i}`}>
                      <TableCell className="mono audit-time" title={ev.time}>
                        {formatAuditTime(ev.time)}
                      </TableCell>
                      <TableCell>{ev.actor}</TableCell>
                      <TableCell className="mono" lang="en">
                        {ev.action}
                      </TableCell>
                      <TableCell className="mono wrap" sx={{ maxWidth: 360 }} lang="en">
                        {target || '—'} {target && <CopyButton value={target} label={`审计对象 ${target}`} />}
                      </TableCell>
                      <TableCell className="mono" lang="en">
                        {remote || '—'}
                      </TableCell>
                      <TableCell>
                        {json ? (
                          <details className="detail-pop">
                            <summary>detail</summary>
                            <pre lang="en">{json}</pre>
                          </details>
                        ) : (
                          <span className="text-muted">—</span>
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
            <div className="more-row">
              {nextCursor !== '' ? (
                <Button
                  variant="outlined"
                  size="small"
                  sx={rowBtnSx}
                  disabled={loadingMore}
                  onClick={() => void loadMore()}
                  data-testid="audit-more"
                >
                  {loadingMore ? '加载中…' : `加载更多（已加载 ${events.length} 条）`}
                </Button>
              ) : (
                <span className="text-muted" style={{ fontSize: 'var(--bf-fs-aux)' }}>
                  共 {events.length} 条（已到末页）
                </span>
              )}
              {moreError && (
                <span className="field-error" role="alert">
                  加载更多失败：{moreError.message}
                </span>
              )}
            </div>
          </>
        ))}
    </div>
  )
}
