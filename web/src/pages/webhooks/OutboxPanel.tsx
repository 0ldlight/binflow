// Webhook outbox 死信面（M17 T-496 FR-159.2 / LC-109——P3 FE 解锁）：
// GET /api/v1/webhooks/outbox（管理面根，非事件面）——filter（subscription /
// status 闭集 pending|delivering|delivered|dead / event_type）+ keyset 分页
// （nextCursor 链；audit 信封规则：'' = 尾页）。dead 行 replay（POST
// …/{id}/replay：reset pending、attempts 清零即刻重投；404 无行 / 409 非死
// 态如实呈现）。读面越过 addon 门（锁定实例可见——操作员先看死信再买槽）；
// replay 写面过 webhook 槽门（community 403 服务端终裁）。
// 锚族（新锚——日志登记诉求）：outbox-panel/outbox-filter-subscription/
// outbox-filter-status/outbox-filter-event-type/outbox-refresh/outbox-table/
// outbox-row-<i>/outbox-replay-<id>/outbox-pager/outbox-empty/
// outbox-replay-note。
import { useCallback, useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Badge } from '@/components/layout/bits'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { Pager } from '@/components/layout/pager'
import { TextInput, NativeSelect } from '@/components/layout/fields'
import { useConfirm } from '@/app/providers'
import { toast } from '@/lib/toast'
import { ApiError, errText } from '@/lib/api'
import { formatCount } from '@/lib/format'
import { getOutboxPage, replayOutboxDelivery } from '@/lib/webhooks'
import type { OutboxDelivery } from '@/lib/webhooks'
import { tr } from '@/i18n'

const tt = tr('webhooks')

const STATUS_OPTIONS = ['pending', 'delivering', 'delivered', 'dead']
const PAGE_SIZE = 20

/** 状态 → badge 语义色（值原样呈现不翻译——排障要比对 API） */
function statusBadge(s: string): 'success' | 'warning' | 'danger' | 'neutral' {
  if (s === 'delivered') return 'success'
  if (s === 'dead') return 'danger'
  if (s === 'pending' || s === 'delivering') return 'warning'
  return 'neutral'
}

function fmtTime(v: string | null): string {
  return v ? v.replace('T', ' ').replace(/(\.\d+)?Z$/, ' UTC') : '—'
}

/** keyset 游标链（页码 N = 链上第 N-1 跳——audit 页同款页窗映射） */
function useOutboxPages(filter: { subscription: string; status: string; eventType: string }) {
  const [tick, setTick] = useState(0)
  const [page, setPage] = useState(1)
  const [cursors, setCursors] = useState<string[]>([''])
  const [rows, setRows] = useState<OutboxDelivery[]>([])
  const [more, setMore] = useState(false)
  const [phase, setPhase] = useState<'loading' | 'ok' | 'error' | 'forbidden'>('loading')
  const [error, setError] = useState<ApiError | null>(null)

  const filterKey = JSON.stringify(filter)
  const scope = `${filterKey} ${tick}`
  const activePage = cursors.length > 0 ? page : 1

  const goTo = useCallback((p: number) => setPage(p), [])
  const refresh = useCallback(() => {
    setTick((t) => t + 1)
    setPage(1)
    setCursors([''])
  }, [])

  useEffect(() => {
    let alive = true
    setPhase((prev) => (rows.length > 0 ? prev : 'loading'))
    getOutboxPage({
      subscription: filter.subscription || undefined,
      status: filter.status || undefined,
      eventType: filter.eventType || undefined,
      limit: PAGE_SIZE,
      cursor: cursors[page - 1] ?? '',
    })
      .then((p) => {
        if (!alive) return
        setRows(p.deliveries ?? [])
        setMore(p.nextCursor !== '')
        if (p.nextCursor !== '') {
          setCursors((c) => (c.length > page ? c : [...c.slice(0, page), p.nextCursor]))
        }
        setError(null)
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
    // eslint-disable-next-line react-hooks/exhaustive-deps -- cursors/page/过滤面共同刻画请求；rows 只作首帧骨架判定
  }, [scope, page, cursors])

  return { rows, more, page: activePage, pageSize: PAGE_SIZE, phase, error, goTo, refresh }
}

export default function OutboxPanel({ readOnly }: { readOnly: boolean }) {
  const confirm = useConfirm()
  const [subscription, setSubscription] = useState('')
  const [status, setStatus] = useState('')
  const [eventType, setEventType] = useState('')
  const [busyId, setBusyId] = useState('')

  // 过滤面提交（350ms 防抖）
  const [committed, setCommitted] = useState({ subscription: '', status: '', eventType: '' })
  useEffect(() => {
    const t = window.setTimeout(() => {
      setCommitted({ subscription: subscription.trim(), status, eventType: eventType.trim() })
    }, 350)
    return () => window.clearTimeout(t)
  }, [subscription, status, eventType])

  const outbox = useOutboxPages(committed)

  const doReplay = async (row: OutboxDelivery) => {
    const ok = await confirm.confirm({
      title: tt('重放死信 {v1}', { v1: row.id }),
      description: tt('将把该投递重置为 pending（attempts 清零，即刻重新投递）——存档信封原样重发：订阅此后的编辑不影响本次载荷（快照即终稿）。仅 dead 行可重放。'),
      confirmLabel: tt('重放'),
      danger: true,
    })
    if (!ok) return
    setBusyId(row.id)
    try {
      const updated = await replayOutboxDelivery(row.id)
      toast.success(tt('已重放 {v1}（订阅 {v2}，状态 {v3}）', { v1: row.id, v2: updated.subscription_key, v3: updated.status }))
      outbox.refresh()
    } catch (err) {
      // 404 无行 / 409 非死态 / 403 槽门——服务端原文如实呈现
      toast.error(tt('重放失败：{v1}', { v1: errText(err) }))
    } finally {
      setBusyId('')
    }
  }

  const hasFilter = committed.subscription !== '' || committed.status !== '' || committed.eventType !== ''

  return (
    <section data-testid="outbox-panel">
      <div className="filter-bar">
        <TextInput
          mono
          lang="en"
          placeholder={tt('订阅 key（精确）')}
          value={subscription}
          onChange={(e) => setSubscription(e.target.value)}
          className="w-[180px]"
          aria-label={tt('按订阅 key 过滤')}
          data-testid="outbox-filter-subscription"
        />
        <NativeSelect
          value={status}
          onChange={(e) => setStatus(e.target.value)}
          className="w-[150px]"
          aria-label={tt('按状态过滤')}
          options={[
            { value: '', label: tt('状态：全部') },
            ...STATUS_OPTIONS.map((s) => ({ value: s, label: s })),
          ]}
          data-testid="outbox-filter-status"
        />
        <TextInput
          mono
          lang="en"
          placeholder={tt('事件型（精确）')}
          value={eventType}
          onChange={(e) => setEventType(e.target.value)}
          className="w-[180px]"
          aria-label={tt('按事件型过滤')}
          data-testid="outbox-filter-event-type"
        />
        {hasFilter && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              setSubscription('')
              setStatus('')
              setEventType('')
            }}
          >
            {tt('清除过滤')}
          </Button>
        )}
        <span className="filter-tail-actions filter-tail-end ml-auto">
          <Button variant="outline" size="sm" onClick={outbox.refresh} data-testid="outbox-refresh">
            {tt('刷新')}
          </Button>
        </span>
      </div>

      {outbox.phase === 'loading' && <StateSkeleton lines={6} />}
      {outbox.phase === 'forbidden' && outbox.error && (
        <EmptyState
          message={tt('无权限查看投递 outbox')}
          hint={tt('GET /api/v1/webhooks/outbox 为管理员视图（system:read——admin / readonly_admin）。')}
        />
      )}
      {outbox.phase === 'error' && outbox.error && (
        <ErrorCard error={outbox.error} onRetry={outbox.refresh} />
      )}
      {outbox.phase === 'ok' &&
        (outbox.rows.length === 0 ? (
          <EmptyState
            illustration
            message={hasFilter ? tt('当前过滤条件下无投递行') : tt('outbox 暂无投递行')}
            hint={
              hasFilter
                ? tt('三个过滤位均为精确匹配（subscription / status / event_type）。')
                : tt('事件投递在 outbox 中排队——失败重试耗尽的行标 dead（本页的重放入口即为此而设）。')
            }
            testid="outbox-empty"
          />
        ) : (
          <>
            <div className="overflow-x-auto">
              <table className="w-full text-dense" data-testid="outbox-table">
                <thead>
                  <tr className="border-b border-border text-left text-aux text-muted-foreground">
                    <th scope="col" className="px-3 py-2 font-medium">{tt('投递 ID')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{tt('订阅')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{tt('事件型')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{tt('状态')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{tt('尝试')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{tt('最近错误 / 状态码')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{tt('时间')}</th>
                    <th scope="col" className="px-3 py-2 text-right font-medium">{tt('操作')}</th>
                  </tr>
                </thead>
                <tbody>
                  {outbox.rows.map((row, i) => (
                    <tr key={row.id} data-testid={`outbox-row-${i}`} className="border-b border-border/60 hover:bg-accent">
                      <td className="px-3 py-1.5 font-mono" lang="en">
                        {row.id} <CopyButton value={row.id} label={tt('投递 ID {v1}', { v1: row.id })} />
                      </td>
                      <td className="px-3 py-1.5 font-mono" lang="en">{row.subscription_key}</td>
                      <td className="px-3 py-1.5 font-mono" lang="en">{row.event_type}</td>
                      <td className="px-3 py-1.5">
                        <Badge variant={statusBadge(row.status)} mono lang="en">{row.status}</Badge>
                      </td>
                      <td className="px-3 py-1.5 font-mono" lang="en">{formatCount(row.attempts)}</td>
                      <td className="max-w-[280px] break-all px-3 py-1.5 font-mono text-aux" title={row.last_error || undefined}>
                        {row.last_error ? row.last_error : row.last_status_code !== null ? `HTTP ${row.last_status_code}` : <span className="text-muted-foreground">—</span>}
                      </td>
                      <td className="whitespace-nowrap px-3 py-1.5 font-mono text-aux" title={`${row.created_at}${row.delivered_at ? ` → ${row.delivered_at}` : ''}`}>
                        {fmtTime(row.created_at)}
                      </td>
                      <td className="whitespace-nowrap px-3 py-1.5 text-right">
                        {row.status === 'dead' && (
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-7"
                            disabled={readOnly || busyId === row.id}
                            onClick={() => void doReplay(row)}
                            data-testid={`outbox-replay-${row.id}`}
                            title={readOnly ? tt('只读管理员：重放是 system:write（服务端 403 兜底）') : tt('重置为 pending 并即刻重投（存档信封原样）')}
                          >
                            {busyId === row.id ? tt('重放中…') : tt('重放')}
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div data-testid="outbox-pager">
              <Pager
                page={outbox.page}
                pageCount={outbox.more ? outbox.page + 1 : outbox.page}
                onPageChange={outbox.goTo}
                from={outbox.rows.length === 0 ? 0 : (outbox.page - 1) * outbox.pageSize + 1}
                to={(outbox.page - 1) * outbox.pageSize + outbox.rows.length}
                total={null}
                lastUnknown
                pageSize={outbox.pageSize}
              />
            </div>
            <p className="field-hint" data-testid="outbox-replay-note">
              {tt('重放仅对 dead 行开放（pending/delivering 已在途、delivered 已终态——重放会造成双投递，服务端以 409 拒绝）；重放投递记审计（webhook.delivery.replay）。读面越过 webhook 槽门：锁定实例可见（先看死信再买槽）。')}
            </p>
          </>
        ))}
    </section>
  )
}
