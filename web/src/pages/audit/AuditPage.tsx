// 审计日志（console-ux §4.10 / §5.3——P3 新栈重写：keyset 页窗游标链语义
// 平移 + 原生轻量表格）：
// - 过滤器：repo/actor（**精确匹配**——REST 契约 `repo_key = ?`，输入框配
//   datalist 建议 + 文案明示）/ action（词表选择器 GE-02）/ 时间窗（since
//   含 until 不含，datetime-local 折算 UTC）/ path（**已加载集**客户端子串
//   ——REST 无 path 参数，§6.3 兜底，显式提示边界）；350ms 防抖提交。
// - 分页：keyset 游标 → 页码页窗映射（页码 N = 游标链推进 N-1 跳；末页
//   未知——游标耗尽才知，页数 = 前沿 + 1 逐页揭示）。
// - 表格：原生轻量表格（**AG Grid 裁定偏离**：keyset 页窗契约 = 每窗全量
//   100 行在 DOM + 窗内客户端 path 过滤 + 行数断言——行虚拟化会破坏这组
//   冻结 spec 契约（governance/t387 家族）；页窗 ≤1000 行无虚拟化必要。
//   AG Grid 迁移随 spec 重写票跟进，日志 Next 登记）；时间 mono / 动作
//   原样 mono（不翻译 enum，排障要比对）/ 对象 repo/path mono + 拷贝 /
//   来源 detail.remote_addr / detail JSON 折叠。
// - CSV 导出 P2 债务：不渲染入口（ux R3）。
// - 非 admin：GET /api/v1/audit 403 → 单张无权限卡（§3.6.3 L2）。
// 锚族原样：audit-page/audit-filter-{repo,actor,action,since,until,path}/
// audit-count/audit-columns(-menu|-item-*|-reset)/audit-refresh/
// audit-empty-filtered/audit-row-<i>/audit-pager。
import { useCallback, useEffect, useMemo, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { Pager, PAGER_SIZE_DEFAULT } from '@/components/layout/pager'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { ApiError, getRepositories } from '@/lib/api'
import type { AuditEvent } from '@/lib/api'
import { useColumnPrefs } from '@/lib/columnPrefs'
import type { ColumnDef } from '@/lib/columnPrefs'
import { formatAuditTime } from '@/lib/format'
import { AUDIT_ACTIONS, EMPTY_AUDIT_FILTERS, getAuditEventsPage, localInputToRFC3339 } from '@/lib/governance'
import type { AuditFilters } from '@/lib/governance'
import { useAsync } from '@/lib/useAsync'
import { tr } from '@/i18n'

const tt = tr('governance')

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
 * 服务端分页容器（keyset 游标 → 页码页窗映射——语义平移，见 audit §2.8）：
 * 页码 N 的窗口 = 游标链第 N-1 跳的 limit 行；向前翻 = 前沿逐跳推进（每跳
 * 一次 keyset 取数，索引便宜——深翻页 offset 扫描成本规避的 C 注兑现），
 * 向后翻 = 已缓存游标直接取窗。过滤/页大小/刷新变化 = 游标链重建（页回
 * 1）；晚到旧响应按 alive 闭包旗标丢弃。refresh 手动重拉（过滤保持、
 * 游标链丢弃回第 1 页）。
 */
function useAuditPages(filters: AuditFilters) {
  const [tick, setTick] = useState(0)
  const [pageSize, setPageSize] = useState(PAGER_SIZE_DEFAULT)
  const [nav, setNav] = useState<{ key: string; page: number } | null>(null)
  const [events, setEvents] = useState<AuditEvent[]>([])
  const [more, setMore] = useState(false)
  const [phase, setPhase] = useState<'loading' | 'ok' | 'error' | 'forbidden'>('loading')
  const [error, setError] = useState<ApiError | null>(null)
  const key = JSON.stringify(filters)
  const page = nav !== null && nav.key === key ? nav.page : 1

  // 游标链：cursors[k-1] = 取第 k 页的游标（cursors[0] = '' 首页）。链与
  // （过滤 × 页大小 × tick）绑定——任一变化即重建。
  const [chain, setChain] = useState<{ scope: string; cursors: string[] }>({ scope: '', cursors: [''] })
  const scope = `${key} ${pageSize} ${tick}`
  if (chain.scope !== scope) {
    setChain({ scope, cursors: [''] })
  }
  const cursor = chain.cursors[page - 1] ?? ''

  useEffect(() => {
    let alive = true
    setPhase('loading')
    setError(null)
    getAuditEventsPage(JSON.parse(key) as AuditFilters, cursor, pageSize)
      .then((p) => {
        if (!alive) return
        setEvents(p.events)
        setMore(p.nextCursor !== '')
        if (p.nextCursor !== '') {
          // 前沿推进：本页带下一游标 → 第 page+1 页已确证存在（页码序列
          // 由 more 派生逐页揭示）
          setChain((c) =>
            c.scope === scope && c.cursors[page] === undefined
              ? { scope: c.scope, cursors: [...c.cursors.slice(0, page), p.nextCursor] }
              : c,
          )
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
  }, [key, page, pageSize, tick, scope, cursor])

  const goTo = useCallback((p: number) => setNav({ key, page: p }), [key])
  const changeSize = useCallback((n: number) => {
    setPageSize(n)
    setNav(null)
  }, [])
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

  const { events, more, page, pageSize, phase, error, goTo, changeSize, refresh } = useAuditPages(committed)

  const cols = useColumnPrefs(COLUMN_IDS, COLS_KEY)
  const [colsOpen, setColsOpen] = useState(false)

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
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{tt('审计')}</h2>
        <span className="text-aux text-2">{tt('只增不改（append-only）；动作值原样呈现')}</span>
      </div>

      <div className="filter-bar">
        <Input
          placeholder={tt('仓库 key（精确）')}
          value={repo}
          onChange={(e) => setRepo(e.target.value)}
          className="w-[180px] font-mono"
          list="audit-repo-options"
          aria-label={tt('按仓库过滤（精确匹配）')}
          data-testid="audit-filter-repo"
        />
        <datalist id="audit-repo-options">
          {repoOptions.map((k) => (
            <option key={k} value={k} />
          ))}
        </datalist>
        <Input
          placeholder={tt('操作者（精确）')}
          value={actor}
          onChange={(e) => setActor(e.target.value)}
          className="w-[160px]"
          aria-label={tt('按操作者过滤（精确匹配）')}
          data-testid="audit-filter-actor"
        />
        <select
          value={action}
          onChange={(e) => setAction(e.target.value)}
          className="h-8 rounded-sm border border-input bg-surface-3 px-2 text-dense"
          aria-label={tt('按动作过滤')}
          data-testid="audit-filter-action"
        >
          <option value="">{tt('动作：全部')}</option>
          {AUDIT_ACTIONS.map((a) => (
            <option key={a} value={a} lang="en">{a}</option>
          ))}
        </select>
        <label className="time-field flex items-center gap-1.5 text-aux text-2">
          <span>{tt('起（含）')}</span>
          <input
            type="datetime-local"
            value={sinceLocal}
            onChange={(e) => setSinceLocal(e.target.value)}
            className="h-8 rounded-sm border border-input bg-surface-3 px-2 text-dense"
            aria-label={tt('起始时间（含）')}
            data-testid="audit-filter-since"
          />
        </label>
        <label className="time-field flex items-center gap-1.5 text-aux text-2">
          <span>{tt('止（不含）')}</span>
          <input
            type="datetime-local"
            value={untilLocal}
            onChange={(e) => setUntilLocal(e.target.value)}
            className="h-8 rounded-sm border border-input bg-surface-3 px-2 text-dense"
            aria-label={tt('截止时间（不含）')}
            data-testid="audit-filter-until"
          />
        </label>
        <Input
          type="search"
          placeholder={tt('对象路径包含（仅已加载）')}
          value={path}
          onChange={(e) => setPath(e.target.value)}
          className="w-[200px] font-mono"
          aria-label={tt('按对象路径过滤（仅已加载条目）')}
          data-testid="audit-filter-path"
        />
        {hasFilter && (
          <Button variant="outline" size="sm" onClick={clearFilters}>{tt('清除过滤')}</Button>
        )}
        <span className="count text-aux text-2" data-testid="audit-count">
          {tt('本页')} {events.length} {tt('条')}{pathQ ? tt('（路径过滤命中 {v1}——仅作用于本页窗口）', { v1: rows.length }) : ''}
        </span>
        <span className="filter-tail-actions ml-auto flex items-center gap-1.5">

          <Popover open={colsOpen} onOpenChange={setColsOpen}>
            <PopoverTrigger asChild>
              <Button
                variant="outline"
                size="sm"
                aria-haspopup="menu"
                aria-expanded={colsOpen}
                data-testid="audit-columns"
                title={tt('自定义显示列（偏好保存在本浏览器）')}
              >
                <span aria-hidden="true">▤</span> {tt('列')} {cols.visibleCount}/{COLUMNS.length}
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-56 p-1" align="end" role="menu" data-testid="audit-columns-menu">
              {COLUMNS.map((c) => {
                const visible = cols.isVisible(c.id)
                const last = visible && cols.visibleCount === 1
                return (
                  <button
                    key={c.id}
                    type="button"
                    role="menuitemcheckbox"
                    aria-checked={visible}
                    aria-disabled={last || undefined}
                    title={last ? tt('至少保留一列') : undefined}
                    data-testid={c.anchor}
                    className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-dense hover:bg-accent aria-disabled:opacity-50"
                    onClick={() => {
                      if (!last) cols.toggle(c.id)
                    }}
                  >
                    <span aria-hidden="true" className="col-check">{visible ? '☑' : '☐'}</span>
                    {c.label}
                  </button>
                )
              })}
              <div role="separator" className="my-1 border-t border-border" />
              <button
                type="button"
role="menuitem"
                aria-disabled={cols.visibleCount === COLUMNS.length || undefined}
                title={cols.visibleCount === COLUMNS.length ? tt('全部列已在场') : tt('显示全部列')}
                data-testid="audit-columns-reset"
                className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-dense hover:bg-accent aria-disabled:opacity-50"
                onClick={() => cols.reset()}
              >
                {tt('全选列')}
              </button>
            </PopoverContent>
          </Popover>
          <Button
            variant="outline"
            size="icon"
            className="size-8"
            aria-label={tt('刷新审计列表')}
            data-testid="audit-refresh"
            disabled={phase === 'loading'}
            onClick={refresh}
            title={tt('重新拉取审计事件（保持当前过滤，回第 1 页）')}
          >
            {phase === 'loading' ? (
              <span role="progressbar" aria-label={tt('重新拉取审计事件（保持当前过滤，回第 1 页）')} className="inline-block size-4 animate-spin rounded-full border-2 border-border border-t-primary" />
            ) : (
              <span aria-hidden="true">↻</span>
            )}
          </Button>
        </span>
      </div>
      {(timeInvalid || timeHint) && (
        <p className="field-hint mb-2 mt-0">
          {timeInvalid ? tt('时间格式无法解析——修正后才会发起查询') : timeHint}
        </p>
      )}

      {phase === 'loading' && <StateSkeleton lines={8} />}
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
                <Button variant="outline" size="sm" onClick={clearFilters}>{tt('清除过滤')}</Button>
              }
              testid="audit-empty-filtered"
            />
          ) : (
            <EmptyState illustration message={tt('暂无审计事件')} hint={tt('登录、建仓、上传等操作会记录在这里')} />
          )
        ) : (
          <>
            <table className="w-full text-dense" data-testid="audit-table">
              <thead>
                <tr className="border-b border-border text-left text-aux text-muted-foreground">
                  {cols.isVisible('time') && <th scope="col" className="px-3 py-2 font-medium">{tt('时间')}</th>}
                  {cols.isVisible('actor') && <th scope="col" className="px-3 py-2 font-medium">{tt('操作者')}</th>}
                  {cols.isVisible('action') && <th scope="col" className="px-3 py-2 font-medium">{tt('动作')}</th>}
                  {cols.isVisible('target') && <th scope="col" className="px-3 py-2 font-medium">{tt('对象')}</th>}
                  {cols.isVisible('source') && <th scope="col" className="px-3 py-2 font-medium">{tt('来源')}</th>}
                  {cols.isVisible('detail') && <th scope="col" className="px-3 py-2 font-medium">{tt('详情')}</th>}
                </tr>
              </thead>
              <tbody>
                {rows.map((ev, i) => {
                  const target = ev.repo ? `${ev.repo}/${ev.path}` : ev.path
                  const json = detailJSON(ev.detail)
                  const remote = detailRemoteAddr(ev.detail)
                  return (
                    <tr key={ev.id} data-testid={`audit-row-${i}`} className="border-b border-border/60 hover:bg-accent">
                      {cols.isVisible('time') && (
                        <td className="px-3 py-1.5 font-mono" title={ev.time}>{formatAuditTime(ev.time)}</td>
                      )}
                      {cols.isVisible('actor') && <td className="px-3 py-1.5">{ev.actor}</td>}
                      {cols.isVisible('action') && (
                        <td className="px-3 py-1.5 font-mono" lang="en">{ev.action}</td>
                      )}
                      {cols.isVisible('target') && (
                        <td className="max-w-[360px] break-all px-3 py-1.5 font-mono" lang="en">
                          {target || '—'} {target && <CopyButton value={target} label={tt('审计对象 {target}', { target: target })} />}
                        </td>
                      )}
                      {cols.isVisible('source') && (
                        <td className="px-3 py-1.5 font-mono" lang="en">{remote || '—'}</td>
                      )}
                      {cols.isVisible('detail') && (
                        <td className="px-3 py-1.5">
                          {json ? (
                            <details className="detail-pop">
                              <summary>detail</summary>
                              <pre lang="en">{json}</pre>
                            </details>
                          ) : (
                            <span className="text-muted-foreground">—</span>
                          )}
                        </td>
                      )}
                    </tr>
                  )
                })}
              </tbody>
            </table>
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
