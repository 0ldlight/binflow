import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { ComponentPropsWithoutRef } from 'react'

import Button from '@mui/material/Button'
import CircularProgress from '@mui/material/CircularProgress'
import Divider from '@mui/material/Divider'
import IconButton from '@mui/material/IconButton'
import Menu from '@mui/material/Menu'
import MenuItem from '@mui/material/MenuItem'
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
import { Pager, PAGER_SIZE_DEFAULT } from '../../components/Pager'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, getRepositories } from '../../lib/api'
import type { AuditEvent } from '../../lib/api'
import { useColumnPrefs } from '../../lib/columnPrefs'
import type { ColumnDef } from '../../lib/columnPrefs'
import { formatAuditTime } from '../../lib/format'
import { monoInputSx } from '../../lib/muiAtoms'
import { AUDIT_ACTIONS, EMPTY_AUDIT_FILTERS, getAuditEventsPage, localInputToRFC3339 } from '../../lib/governance.ts'
import type { AuditFilters } from '../../lib/governance.ts'
import { useAsync } from '../../lib/useAsync'
import { tr } from '../../i18n'

const tt = tr('governance')

// 审计日志（console-ux §4.10 / §5.3；T-102 AC①）：
// - 过滤器：repo/actor（**精确匹配**——REST 契约是 `repo_key = ?`，输入框
//   配 datalist 建议 + 文案明示）/ action（词表选择器，GE-02）/ 时间窗
//   （since 含 until 不含，datetime-local 折算 UTC）/ path（**已加载集**
//   客户端子串——REST 无 path 参数，§6.3 兜底，显式提示边界）。
// - 分页：T-451（E2 翻案 / LC-98）页码控件——keyset 游标页窗映射：页码 N
//   = 游标链推进 N-1 跳（后端维持 keyset，呈现对齐语义自有 C 注）；末页
//   未知（游标耗尽才知），页数 = 前沿 + 1，页码逐页揭示、全链禁置不隐藏。
// - 表格：时间 mono / 动作原样 mono（不翻译 enum，排障要比对）/ 对象
//   repo/path mono + 拷贝 / 来源 detail.remote_addr / detail JSON 折叠。
// - CSV 导出 P2 债务：不渲染入口（ux R3）。
// - 非 admin：GET /api/v1/audit 403 → 单张无权限卡（§3.6.3 L2）。
// - T-387（FR-125.2 L1，console-artifactory-parity §5 L1）：工具栏补列选器
//   （Menu + menuitemcheckbox，列集 = 既有六列；偏好 localStorage per-page）
//   与刷新 IconButton（静态列表手动重取——回首页游标，过滤保持）。

/** T-387 列选器列集：label 与表头一致；anchor = 菜单项锚（anchor-audit
 *  的 anchor: 属性形态）。 */
const COLUMNS: ColumnDef[] = [
  { id: 'time', label: tt('时间'), anchor: 'audit-columns-item-time' },
  { id: 'actor', label: tt('操作者'), anchor: 'audit-columns-item-actor' },
  { id: 'action', label: tt('动作'), anchor: 'audit-columns-item-action' },
  { id: 'target', label: tt('对象'), anchor: 'audit-columns-item-target' },
  { id: 'source', label: tt('来源'), anchor: 'audit-columns-item-source' },
  { id: 'detail', label: tt('详情'), anchor: 'audit-columns-item-detail' },
]
const COLUMN_IDS = COLUMNS.map((c) => c.id)
const COLS_KEY = 'binflow-console-cols-audit'

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
 * 服务端分页容器（T-451 窗口化改版——E2 翻案：keyset 游标 → 页码页窗）：
 * 页码 N 的窗口 = 游标链第 N-1 跳的 limit 行；向前翻 = 前沿逐跳推进（每跳
 * 一次 keyset 取数，索引便宜——深翻页 offset 扫描成本规避的 C 注兑现），
 * 向后翻 = 已缓存游标直接取窗。过滤/页大小/刷新变化 = 游标链重建（页回
 * 1）；晚到旧响应按 alive 闭包旗标丢弃（useAsync 同款语义）。T-387（L1
 * 刷新）：refresh 手动重拉（过滤保持、游标链丢弃回第 1 页）。
 */
function useAuditPages(filters: AuditFilters) {
  const [tick, setTick] = useState(0)
  const [pageSize, setPageSize] = useState(PAGER_SIZE_DEFAULT)
  // nav.key !== 当前过滤键 = 派生回落第 1 页（无 effect 回零，ResultsTable
  // epoch 同款口径）
  const [nav, setNav] = useState<{ key: string; page: number } | null>(null)
  const [events, setEvents] = useState<AuditEvent[]>([])
  const [more, setMore] = useState(false)
  const [phase, setPhase] = useState<'loading' | 'ok' | 'error' | 'forbidden'>('loading')
  const [error, setError] = useState<ApiError | null>(null)
  const key = JSON.stringify(filters)
  const page = nav !== null && nav.key === key ? nav.page : 1

  // 游标链：cursors[k-1] = 取第 k 页的游标（cursors[0] = '' 首页）。链与
  // （过滤 × 页大小 × tick）绑定——任一变化即重建（旧链在新窗口宽度下
  // 指向错位窗口）。ref 承载：写入不触发重取（effect 依赖保持窄面）。
  const chainRef = useRef<{ scope: string; cursors: string[] }>({ scope: '', cursors: [''] })
  const scope = `${key} ${pageSize} ${tick}`

  useEffect(() => {
    if (chainRef.current.scope !== scope) chainRef.current = { scope, cursors: [''] }
    let alive = true
    setPhase('loading')
    setError(null)
    getAuditEventsPage(JSON.parse(key) as AuditFilters, chainRef.current.cursors[page - 1] ?? '', pageSize)
      .then((p) => {
        if (!alive) return
        setEvents(p.events)
        setMore(p.nextCursor !== '')
        if (p.nextCursor !== '') {
          // 前沿推进：本页带下一游标 → 第 page+1 页已确证存在（页码序列
          // 由 more 派生逐页揭示）
          const chain = chainRef.current
          if (chain.cursors[page] === undefined) {
            chainRef.current = { scope: chain.scope, cursors: [...chain.cursors.slice(0, page), p.nextCursor] }
          }
        }
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
  }, [key, page, pageSize, tick, scope])

  const goTo = useCallback((p: number) => setNav({ key, page: p }), [key])
  const changeSize = useCallback((n: number) => {
    setPageSize(n)
    setNav(null)
  }, [])
  // T-387（L1 刷新）：手动重拉（过滤保持；游标链丢弃回第 1 页）
  const refresh = useCallback(() => {
    setTick((t) => t + 1)
    setNav(null)
  }, [])

  return { events, more, page, pageSize, phase, error, goTo, changeSize, refresh }
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
  const { events, more, page, pageSize, phase, error, goTo, changeSize, refresh } = useAuditPages(committed)

  // T-387（L1）：列显隐偏好（per-page localStorage）+ 列选菜单锚
  const cols = useColumnPrefs(COLUMN_IDS, COLS_KEY)
  const [colsAnchor, setColsAnchor] = useState<HTMLElement | null>(null)
  const colsOpen = Boolean(colsAnchor)

  // path 过滤：只作用于已加载集（§6.3——绝不假装过滤了全量）
  const pathQ = path.trim().toLowerCase()
  const rows = pathQ ? events.filter((e) => e.path.toLowerCase().includes(pathQ)) : events

  const hasServerFilter =
    committed.repo !== '' || committed.actor !== '' || committed.action !== '' || committed.since !== '' || committed.until !== ''
  const hasFilter = hasServerFilter || pathQ !== ''
  const timeHint =
    committed.since || committed.until
      ? tt('窗口（UTC）：{v1} ≤ time < {v2}', { v1: committed.since || '…', v2: committed.until || '…' })
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
        <h2>{tt('审计')}</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>{tt('只增不改（append-only）；动作值原样呈现')}        </span>
      </div>

      <div className="filter-bar">
        <TextField
          size="small"
          placeholder={tt('仓库 key（精确）')}
          value={repo}
          onChange={(e) => setRepo(e.target.value)}
          sx={{ ...monoInputSx, width: 180 }}
          slotProps={{
            htmlInput: {
              list: 'audit-repo-options',
              'aria-label': tt('按仓库过滤（精确匹配）'),
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
          placeholder={tt('操作者（精确）')}
          value={actor}
          onChange={(e) => setActor(e.target.value)}
          sx={{ width: 160 }}
          slotProps={{
            htmlInput: {
              'aria-label': tt('按操作者过滤（精确匹配）'),
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
          sx={{ width: 180 }}
          slotProps={{
            select: {
              native: true,
              inputProps: {
                'aria-label': tt('按动作过滤'),
                'data-testid': 'audit-filter-action',
              } as ComponentPropsWithoutRef<'select'>,
            } as ComponentPropsWithoutRef<typeof Select>,
          }}
        >
          <option value="">{tt('动作：全部')}</option>
          {AUDIT_ACTIONS.map((a) => (
            <option key={a} value={a} lang="en">
              {a}
            </option>
          ))}
        </TextField>
        <label className="time-field">
          <span>{tt('起（含）')}</span>
          <TextField
            size="small"
            type="datetime-local"
            value={sinceLocal}
            onChange={(e) => setSinceLocal(e.target.value)}
            sx={{ width: 200 }}
            slotProps={{ htmlInput: { 'aria-label': tt('起始时间（含）'), 'data-testid': 'audit-filter-since' } }}
          />
        </label>
        <label className="time-field">
          <span>{tt('止（不含）')}</span>
          <TextField
            size="small"
            type="datetime-local"
            value={untilLocal}
            onChange={(e) => setUntilLocal(e.target.value)}
            sx={{ width: 200 }}
            slotProps={{ htmlInput: { 'aria-label': tt('截止时间（不含）'), 'data-testid': 'audit-filter-until' } }}
          />
        </label>
        <TextField
          type="search"
          size="small"
          placeholder={tt('对象路径包含（仅已加载）')}
          value={path}
          onChange={(e) => setPath(e.target.value)}
          sx={{ ...monoInputSx, width: 200 }}
          slotProps={{
            htmlInput: {
              'aria-label': tt('按对象路径过滤（仅已加载条目）'),
              'data-testid': 'audit-filter-path',
              lang: 'en',
              className: 'mono',
            },
          }}
        />
        {hasFilter && (
          <Button variant="outlined" size="small" onClick={clearFilters}>{tt('清除过滤')}          </Button>
        )}
        <span className="count" data-testid="audit-count">{tt('本页')} {events.length} {tt('条')}{pathQ ? tt('（路径过滤命中 {v1}——仅作用于本页窗口）', { v1: rows.length }) : ''}
        </span>
        {/* T-387（L1）：工具栏尾 = 列选器 + 刷新（parity L1「列选择器 + 刷新
            按钮」；计数已在栏尾 audit-count）。形态与仓库页同款——两页共写
            的持久层在 lib/columnPrefs（菜单壳 30 行级，页内直挂换全字面量锚，
            不做共享组件 + 动态前缀）。 */}
        <span className="filter-tail-actions">
          <Button
            variant="outlined"
            size="small"
            aria-haspopup="menu"
            aria-expanded={colsOpen}
            data-testid="audit-columns"
            title={tt('自定义显示列（偏好保存在本浏览器）')}
            onClick={(e) => setColsAnchor(e.currentTarget)}
          >
            <span aria-hidden="true">▤</span> {tt('列')} {cols.visibleCount}/{COLUMNS.length}
          </Button>
          <Menu
            open={colsOpen}
            onClose={() => setColsAnchor(null)}
            anchorEl={colsAnchor}
            anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
            transformOrigin={{ vertical: 'top', horizontal: 'right' }}
            data-testid="audit-columns-menu"
          >
            {COLUMNS.map((c) => {
              const visible = cols.isVisible(c.id)
              const last = visible && cols.visibleCount === 1
              return (
                <MenuItem
                  key={c.id}
                  role="menuitemcheckbox"
                  aria-checked={visible}
                  aria-disabled={last || undefined}
                  title={last ? tt('至少保留一列') : undefined}
                  data-testid={c.anchor}
                  onClick={() => {
                    if (!last) cols.toggle(c.id)
                  }}
                >
                  <span aria-hidden="true" className="col-check">
                    {visible ? '☑' : '☐'}
                  </span>
                  {c.label}
                </MenuItem>
              )
            })}
            <Divider component="li" />
            <MenuItem
              aria-disabled={cols.visibleCount === COLUMNS.length || undefined}
              title={cols.visibleCount === COLUMNS.length ? tt('全部列已在场') : tt('显示全部列')}
              data-testid="audit-columns-reset"
              onClick={() => cols.reset()}
            >{tt('全选列')}            </MenuItem>
          </Menu>
          <IconButton
            size="small"
            aria-label={tt('刷新审计列表')}
            data-testid="audit-refresh"
            disabled={phase === 'loading'}
            onClick={refresh}
            title={tt('重新拉取审计事件（保持当前过滤，回第 1 页）')}
          >
            {phase === 'loading' ? (
              <CircularProgress size={16} aria-hidden="true" />
            ) : (
              <span aria-hidden="true">↻</span>
            )}
          </IconButton>
        </span>
      </div>
      {(timeInvalid || timeHint) && (
        <p className="field-hint" style={{ margin: '0 0 var(--bf-sp-2)' }}>
          {timeInvalid
            ? tt('时间格式无法解析——修正后才会发起查询')
            : timeHint}
        </p>
      )}

      {phase === 'loading' && <Skeleton lines={8} />}
      {phase === 'forbidden' && error && (
        <EmptyState
          message={tt('无权限查看审计日志')}
          hint={tt('审计查询面为管理员视图（GET /api/v1/audit 仅 admin）。')}
        />
      )}
      {phase === 'error' && error && <ErrorCard error={error} />}
      {phase === 'ok' &&
        (events.length === 0 ? (
          hasFilter ? (
            <EmptyState
              illustration
              message={tt('当前过滤条件下无匹配事件')}
              hint={tt('仓库 / 操作者为精确匹配；时间窗为闭开区间（起含、止不含）。')}
              action={
                <Button variant="outlined" size="small" onClick={clearFilters}>{tt('清除过滤')}                </Button>
              }
              testid="audit-empty-filtered"
            />
          ) : (
            <EmptyState
              illustration
              message={tt('暂无审计事件')}
              hint={tt('登录、建仓、上传等操作会记录在这里')}
            />
          )
        ) : (
          <>
            <Table data-testid="audit-table">
              <TableHead>
                <TableRow>
                  {cols.isVisible('time') && <TableCell component="th" scope="col">{tt('时间')}</TableCell>}
                  {cols.isVisible('actor') && <TableCell component="th" scope="col">{tt('操作者')}</TableCell>}
                  {cols.isVisible('action') && <TableCell component="th" scope="col">{tt('动作')}</TableCell>}
                  {cols.isVisible('target') && <TableCell component="th" scope="col">{tt('对象')}</TableCell>}
                  {cols.isVisible('source') && <TableCell component="th" scope="col">{tt('来源')}</TableCell>}
                  {cols.isVisible('detail') && <TableCell component="th" scope="col">{tt('详情')}</TableCell>}
                </TableRow>
              </TableHead>
              <TableBody>
                {rows.map((ev, i) => {
                  const target = ev.repo ? `${ev.repo}/${ev.path}` : ev.path
                  const json = detailJSON(ev.detail)
                  const remote = detailRemoteAddr(ev.detail)
                  return (
                    <TableRow key={ev.id} data-testid={`audit-row-${i}`} hover>
                      {cols.isVisible('time') && (
                        <TableCell className="mono audit-time" title={ev.time}>
                          {formatAuditTime(ev.time)}
                        </TableCell>
                      )}
                      {cols.isVisible('actor') && <TableCell>{ev.actor}</TableCell>}
                      {cols.isVisible('action') && (
                        <TableCell className="mono" lang="en">
                          {ev.action}
                        </TableCell>
                      )}
                      {cols.isVisible('target') && (
                        <TableCell className="mono" sx={{ maxWidth: 360, whiteSpace: 'normal', wordBreak: 'break-all' }} lang="en">
                          {target || '—'} {target && <CopyButton value={target} label={tt('审计对象 {target}', { target: target })} />}
                        </TableCell>
                      )}
                      {cols.isVisible('source') && (
                        <TableCell className="mono" lang="en">
                          {remote || '—'}
                        </TableCell>
                      )}
                      {cols.isVisible('detail') && (
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
                      )}
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
            <div className="pager-row" data-testid="audit-pager">
              <Pager
                page={page}
                pageCount={more ? page + 1 : page}
                onPageChange={goTo}
                from={events.length === 0 ? 0 : (page - 1) * pageSize + 1}
                to={(page - 1) * pageSize + events.length}
                total={null}
                lastUnknown
                pageSize={pageSize}
                onPageSizeChange={changeSize}
              />
            </div>
          </>
        ))}
    </div>
  )
}
