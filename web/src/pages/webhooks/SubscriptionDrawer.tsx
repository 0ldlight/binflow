// 订阅详情 / 最近投递记录抽屉（M13 T-366——P3 新栈重写：shadcn Sheet 右滑
// 480 档；形态 = console-artifactory-parity 抽屉族通用规格）。
// 「最近投递记录」= GET /event/api/v1/troubleshooting?subscription=<key>
// （webhook.md §7 排障环）：失败必录；成功仅 debug:true 订阅入记录——空
// 列表 ≠ 无投递，空态文案如实说明。行点击展开 payload 快照（mono + 拷贝）。
// 锚族原样：wh-drawer/wh-drawer-close/wh-drawer-criteria/wh-records-refresh/
// wh-records-table/wh-record-<i>/wh-record-payload-<i>/wh-records-empty。
import { useCallback, useEffect, useState } from 'react'

import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/layout/bits'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { ApiError, errText } from '@/lib/api'
import { getTroubleshooting, isWired } from '@/lib/webhooks'
import type { TroubleshootingRecord, WebhookSubscription } from '@/lib/webhooks'
import { tr } from '@/i18n'

const tt = tr('webhooks')

function formatMillis(ts: number): string {
  const t = new Date(ts)
  if (Number.isNaN(t.getTime())) return String(ts)
  const pad = (v: number) => String(v).padStart(2, '0')
  return `${t.getFullYear()}-${pad(t.getMonth() + 1)}-${pad(t.getDate())} ${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}`
}

/** 一条记录的可呈现状态 */
function recordStatus(rec: TroubleshootingRecord): { label: string; color: 'success' | 'danger' | 'warning' } {
  if (rec.errors.length > 0 && rec.response.status === 0) {
    return { label: tt('发送失败'), color: 'danger' }
  }
  const s = rec.response.status
  if (s >= 200 && s < 300) return { label: tt('{s} 已送达', { s: s }), color: 'success' }
  return { label: `${s}`, color: 'danger' }
}

type RecordsPhase =
  | { kind: 'loading' }
  | { kind: 'ok'; records: TroubleshootingRecord[] }
  | { kind: 'error'; message: string }

export default function SubscriptionDrawer({
  subscription,
  onClose,
}: {
  /** 订阅详情（列表行打开；key 驱动记录查询） */
  subscription: WebhookSubscription | null
  onClose: () => void
}) {
  const [phase, setPhase] = useState<RecordsPhase>({ kind: 'loading' })
  const [expanded, setExpanded] = useState<number | null>(null)

  const load = useCallback(async (key: string) => {
    setPhase({ kind: 'loading' })
    try {
      const records = await getTroubleshooting(key)
      setPhase({ kind: 'ok', records })
    } catch (err) {
      setPhase({ kind: 'error', message: errText(err) })
    }
  }, [])

  useEffect(() => {
    if (subscription) {
      setExpanded(null)
      void load(subscription.key)
    }
  }, [subscription, load])

  if (!subscription) return null
  const sub = subscription
  const handler = sub.handlers[0]
  const criteria = sub.event_filter.criteria ?? {}

  return (
    <Sheet open onOpenChange={(open) => { if (!open) onClose() }}>
      <SheetContent
        side="right"
        className="flex w-full max-w-[480px] flex-col gap-4 overflow-y-auto p-0"
        data-testid="wh-drawer"
      >
        <SheetHeader className="border-b border-border px-4 py-3">
          <SheetTitle className="flex items-center gap-2">
            <span className="break-all font-mono text-base" lang="en">{sub.key}</span>
            <span className="flex-1" />
            <Badge variant={sub.enabled ? 'success' : 'neutral'}>{sub.enabled ? tt('启用') : tt('停用')}</Badge>
            {sub.debug && <Badge mono lang="en">debug</Badge>}
            <button
              type="button"
              aria-label={tt('关闭')}
              onClick={onClose}
              data-testid="wh-drawer-close"
              className="grid size-7 place-items-center rounded-sm text-muted-foreground hover:bg-accent hover:text-foreground"
            >
              ✕
            </button>
          </SheetTitle>
        </SheetHeader>

        <div className="flex flex-1 flex-col gap-4 px-4 pb-4">
          {sub.description && <p className="text-dense text-2">{sub.description}</p>}

          <div className="flex flex-col gap-1">
            <span className="text-aux text-muted-foreground">{tt('事件域')}</span>
            <div lang="en" className="font-mono">{sub.event_filter.domain}</div>
            <span className="mt-1 text-aux text-muted-foreground">{tt('事件型（wired = 有触发源）')}</span>
            <div className="flex flex-wrap gap-1">
              {sub.event_filter.event_types.map((t) => (
                <span
                  key={t}
                  className={`badge ${isWired(sub.event_filter.domain, t) ? 'success' : 'neutral'}`}
                  lang="en"
                >
                  {t}
                </span>
              ))}
            </div>
          </div>

          <div className="flex flex-col gap-1">
            <span className="text-aux text-muted-foreground">{tt('过滤条件（criteria）')}</span>
            <pre className="m-0 break-all whitespace-pre-wrap font-mono text-xs" data-testid="wh-drawer-criteria">
              {JSON.stringify(criteria, null, 2)}
            </pre>
          </div>

          <div className="flex flex-col gap-1">
            <span className="text-aux text-muted-foreground">{tt('投递目标（handler）')}</span>
            <div className="break-all font-mono" lang="en">
              {handler?.url ?? '—'} {handler?.url && <CopyButton value={handler.url} label={tt('接收器 URL')} />}
            </div>
            <p className="text-dense text-2">
              {tt('secret：')}{handler?.secret ? tt('已设置（write-only，回显为掩码）') : tt('未设置')}
              {handler?.secret && handler.use_secret_for_signing ? tt('；签名态（HMAC-SHA256 → X-JFrog-Event-Auth）') : ''}
            </p>
          </div>

          <div className="border-t border-border" />

          <div className="flex items-center gap-2">
            <h4 className="text-dense font-semibold">{tt('最近投递记录（排障环）')}</h4>
            <span className="flex-1" />
            <Button variant="outline" size="sm" className="h-7" onClick={() => void load(sub.key)} data-testid="wh-records-refresh">{tt('刷新')}</Button>
          </div>

          {phase.kind === 'loading' && <StateSkeleton lines={4} />}
          {phase.kind === 'error' && (
            <ErrorCard error={new ApiError(0, phase.message)} onRetry={() => void load(sub.key)} />
          )}
          {phase.kind === 'ok' && phase.records.length === 0 && (
            <EmptyState
              message={tt('暂无投递记录')}
              hint={tt('排障环只记录失败投递；开启 debug 的订阅成功也记录。空列表不等于没有投递发生。')}
              testid="wh-records-empty"
            />
          )}
          {phase.kind === 'ok' && phase.records.length > 0 && (
            <>
              <table className="w-full text-dense" data-testid="wh-records-table">
                <thead>
                  <tr className="border-b border-border text-left text-aux text-muted-foreground">
                    <th scope="col" className="px-2 py-1.5 font-medium">{tt('时间')}</th>
                    <th scope="col" className="px-2 py-1.5 font-medium">{tt('状态')}</th>
                    <th scope="col" className="px-2 py-1.5 font-medium">{tt('事件')}</th>
                    <th scope="col" className="px-2 py-1.5 text-right font-medium">{tt('耗时')}</th>
                    <th scope="col" className="px-2 py-1.5 text-right font-medium">{tt('重试')}</th>
                  </tr>
                </thead>
                <tbody>
                  {phase.records.map((rec, i) => {
                    const st = recordStatus(rec)
                    return (
                      <tr
                        key={`${rec.timestamp}-${i}`}
                        data-testid={`wh-record-${i}`}
                        className="cursor-pointer border-b border-border/60 hover:bg-accent"
                        onClick={() => setExpanded(expanded === i ? null : i)}
                      >
                        <td className="whitespace-nowrap px-2 py-1.5 font-mono text-aux" title={String(rec.timestamp)}>
                          {formatMillis(rec.timestamp)}
                        </td>
                        <td className="px-2 py-1.5">
                          <Badge variant={st.color}>{st.label}</Badge>
                        </td>
                        <td className="px-2 py-1.5 font-mono" lang="en">{rec.event.event_type}</td>
                        <td className="px-2 py-1.5 text-right font-mono" lang="en">{rec.elapsed_millis}ms</td>
                        <td className="px-2 py-1.5 text-right font-mono" lang="en">{rec.request.retries_attempted}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
              {expanded !== null && phase.records[expanded] && (
                <div className="rounded-md border border-border p-2" data-testid={`wh-record-payload-${expanded}`}>
                  <div className="mb-1 flex items-center gap-2">
                    <span className="text-aux text-muted-foreground">
                      {tt('投递载荷快照（点击行收起）')}{phase.records[expanded].errors.length > 0 ? tt('；错误：') : ''}
                    </span>
                    {phase.records[expanded].errors.length > 0 && (
                      <span className="break-all font-mono text-aux text-destructive">
                        {phase.records[expanded].errors.join('; ')}
                      </span>
                    )}
                    <span className="flex-1" />
                    <CopyButton value={phase.records[expanded].request.payload} label={tt('投递载荷 JSON')} />
                  </div>
                  <pre
                    className="m-0 max-h-[240px] overflow-auto whitespace-pre-wrap break-all font-mono text-xs"
                    aria-label={tt('投递载荷 JSON')}
                  >
                    {phase.records[expanded].request.payload}
                  </pre>
                  {phase.records[expanded].response.body && (
                    <p className="mt-1 break-all text-aux text-muted-foreground">
                      {tt('接收器应答体：')}<span className="font-mono">{phase.records[expanded].response.body.slice(0, 512)}</span>
                    </p>
                  )}
                </div>
              )}
            </>
          )}
          <p className="mt-auto text-aux text-muted-foreground">
            {tt('投递语义（官方锚点）：失败或 ≥500 按固定 10s 重试、首试计入共 5 次；4xx/3xx 不重试一步终态；重试耗尽行标 dead。')}
          </p>
        </div>
      </SheetContent>
    </Sheet>
  )
}
