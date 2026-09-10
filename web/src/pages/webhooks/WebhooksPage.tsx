// Webhook 订阅管理页（M13 T-366——P3 新栈重写 + outbox 死信面解锁）。
// 两 Tab：
// - 订阅：GET /event/api/v1/subscriptions（读门 system:read——readonly 可
//   见；读面不过 license 门）。行内：启停开关（PUT 全量体）、详情抽屉、
//   试发、编辑对话框、删除（danger 确认——级联投递行）。
// - 死信（Outbox——T-496 API 的 FE 解锁）：GET /api/v1/webhooks/outbox
//   （管理面根；filter: subscription/status/event_type + keyset 分页），
//   dead 行 replay（POST …/{id}/replay——reset pending 即刻重投；写面过
//   webhook 槽门）。
// 交互形态照 console-artifactory-parity：新建/编辑 = Dialog，详情 + 记录 =
// 右侧 Drawer。
// 锚族原样：wh-page/wh-refresh/wh-create/wh-readonly-note/wh-locked-note/
// wh-test-last/wh-empty(-create)?/wh-table/wh-row-<key>/wh-toggle-<key>/
// wh-open-<key>/wh-test-<key>/wh-edit-<key>/wh-delete-<key>/wh-count。
// 新锚（日志登记诉求）：wh-tab-subs/wh-tab-outbox/outbox-panel/
// outbox-filter-*/outbox-table/outbox-row-<i>/outbox-replay-<id>/
// outbox-pager/outbox-empty。
import { useCallback, useEffect, useState } from 'react'

import { useAuth } from '@/app/AuthContext'
import { Button } from '@/components/ui/button'
import { AlertBox } from '@/components/layout/bits'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { useConfirm } from '@/app/providers'
import { toast } from '@/lib/toast'
import { ApiError, canAdminWrite, errText, isReadOnlyAdmin } from '@/lib/api'
import { getAddons } from '@/lib/addons'
import type { AddonRow } from '@/lib/addons'
import {
  deleteSubscription,
  isWired,
  listSubscriptions,
  testSubscription,
  updateSubscription,
} from '@/lib/webhooks'
import type { SubscriptionRequest, TestOutcome, WebhookSubscription } from '@/lib/webhooks'
import SubscriptionDialog from './SubscriptionDialog'
import SubscriptionDrawer from './SubscriptionDrawer'
import OutboxPanel from './OutboxPanel'
import { tr } from '@/i18n'

const tt = tr('webhooks')

/** webhook 功能槽 id（slots.go 第 19 槽） */
const WEBHOOK_SLOT_ID = 'webhook'

/** 回显视图 → 全量 PUT 体（启停开关复用；secret 省略 = 保持库存密文） */
function putBodyOf(sub: WebhookSubscription, enabled: boolean): SubscriptionRequest {
  const h = sub.handlers[0]
  return {
    key: sub.key,
    project_key: sub.project_key,
    description: sub.description,
    enabled,
    event_filter: {
      domain: sub.event_filter.domain,
      event_types: [...sub.event_filter.event_types],
      criteria: sub.event_filter.criteria ?? {},
    },
    handlers: [
      {
        handler_type: 'webhook',
        url: h?.url ?? '',
        use_secret_for_signing: h?.use_secret_for_signing ?? false,
        custom_http_headers: h?.custom_http_headers ?? [],
      },
    ],
    debug: sub.debug,
  }
}

type ListPhase =
  | { kind: 'loading' }
  | { kind: 'ok'; subs: WebhookSubscription[] }
  | { kind: 'forbidden' }
  | { kind: 'error'; message: string }

export default function WebhooksPage() {
  const { session } = useAuth()
  const readOnly = isReadOnlyAdmin(session)
  const adminWrite = canAdminWrite(session)
  const confirm = useConfirm()

  const [tab, setTab] = useState<'subs' | 'outbox'>('subs')
  const [phase, setPhase] = useState<ListPhase>({ kind: 'loading' })
  const [slot, setSlot] = useState<AddonRow | null | 'unavailable'>('unavailable')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<WebhookSubscription | null>(null)
  const [drawerSub, setDrawerSub] = useState<WebhookSubscription | null>(null)
  const [busyKey, setBusyKey] = useState('')
  /** 最近一次行内试发的结果（就地呈现——排障面向工程师） */
  const [lastTest, setLastTest] = useState<{ key: string; outcome: TestOutcome } | null>(null)

  const reload = useCallback(async () => {
    setPhase((prev) => (prev.kind === 'ok' ? prev : { kind: 'loading' }))
    try {
      const subs = await listSubscriptions()
      setPhase({ kind: 'ok', subs })
    } catch (err) {
      const status = err instanceof ApiError ? err.status : 0
      if (status === 403) {
        setPhase({ kind: 'forbidden' })
        return
      }
      setPhase({ kind: 'error', message: errText(err) })
    }
  }, [])

  useEffect(() => {
    void reload()
    // 槽行仅驱动提示文案（服务端 403 终裁——失败按未门控呈现）
    getAddons()
      .then((rows) => setSlot(rows.find((r) => r.id === WEBHOOK_SLOT_ID) ?? null))
      .catch(() => setSlot(null))
  }, [reload])

  const subs = phase.kind === 'ok' ? phase.subs : []
  const slotRow = slot === 'unavailable' ? null : slot
  const slotLocked = slotRow !== null && !slotRow.enabled

  /** 行内试发：吃库存订阅的全量体（官方 test 语义 = 完整订阅体非 key 引用） */
  const doTest = async (sub: WebhookSubscription) => {
    setBusyKey(sub.key)
    try {
      const body = putBodyOf(sub, sub.enabled)
      const outcome = await testSubscription(body)
      setLastTest({ key: sub.key, outcome })
      if (outcome.ok) toast.success(tt('试发成功（{v1}）：{v2}', { v1: sub.key, v2: outcome.message }))
      else toast.error(tt('试发失败（{v1}）：{v2}', { v1: sub.key, v2: outcome.message }))
    } catch (err) {
      const msg =
        err instanceof ApiError && err.status === 403
          ? tt('试发被拒（403）：{v1}——webhook 为 pro+ 档特性', { v1: err.message })
          : errText(err)
      toast.error(msg)
    } finally {
      setBusyKey('')
    }
  }

  /** 启停开关：PUT 全量体（key/project_key 不可改；secret 省略 = 保持） */
  const doToggle = async (sub: WebhookSubscription, enabled: boolean) => {
    setBusyKey(sub.key)
    try {
      await updateSubscription(sub.key, putBodyOf(sub, enabled))
      toast.success(tt('订阅 {v1} 已{v2}', { v1: sub.key, v2: enabled ? tt('启用') : tt('停用') }))
      await reload()
    } catch (err) {
      const msg =
        err instanceof ApiError && err.status === 403
          ? tt('写入被拒（403）：{v1}——webhook 为 pro+ 档特性', { v1: err.message })
          : errText(err)
      toast.error(msg)
    } finally {
      setBusyKey('')
    }
  }

  const doDelete = async (sub: WebhookSubscription) => {
    const ok = await confirm.confirm({
      title: tt('删除订阅 {v1}', { v1: sub.key }),
      description: tt('删除后该订阅的全部投递记录一并级联清除；接收器不会再收到任何事件。此操作不可撤销。'),
      confirmLabel: tt('删除'),
      danger: true,
    })
    if (!ok) return
    setBusyKey(sub.key)
    try {
      await deleteSubscription(sub.key)
      toast.success(tt('订阅 {v1} 已删除', { v1: sub.key }))
      if (drawerSub?.key === sub.key) setDrawerSub(null)
      await reload()
    } catch (err) {
      toast.error(tt('删除失败：{v1}', { v1: errText(err) }))
    } finally {
      setBusyKey('')
    }
  }

  return (
    <div data-testid="wh-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">Webhooks</h2>
        <span className="text-aux text-2">{tt('统一事件订阅（/binflow/event/api/v1——13 域 66 事件型；outbox 投递，失败重试固定 10s×4）')}</span>
      </div>

      {/* Tab 条（订阅 / 死信 outbox——P3 解锁面） */}
      <div className="mb-3 flex gap-1 border-b border-border">
        <button
          type="button"
          data-testid="wh-tab-subs"
          aria-current={tab === 'subs' ? 'page' : undefined}
          onClick={() => setTab('subs')}
          className={`-mb-px rounded-t-sm border-b-2 px-3 py-1.5 text-dense ${tab === 'subs' ? 'border-primary font-medium' : 'border-transparent text-muted-foreground hover:text-foreground'}`}
        >
          {tt('订阅')}
        </button>
        <button
          type="button"
          data-testid="wh-tab-outbox"
          aria-current={tab === 'outbox' ? 'page' : undefined}
          onClick={() => setTab('outbox')}
          className={`-mb-px rounded-t-sm border-b-2 px-3 py-1.5 text-dense ${tab === 'outbox' ? 'border-primary font-medium' : 'border-transparent text-muted-foreground hover:text-foreground'}`}
        >
          {tt('投递（Outbox / 死信）')}
        </button>
      </div>

      {tab === 'outbox' ? (
        <OutboxPanel readOnly={readOnly} />
      ) : (
        <>
          <div className="mb-2 flex items-center gap-2">
            <span className="flex-1" />
            <Button variant="outline" size="sm" onClick={() => void reload()} data-testid="wh-refresh">
              {tt('刷新')}
            </Button>
            {adminWrite && (
              <Button
                size="sm"
                onClick={() => {
                  setEditing(null)
                  setDialogOpen(true)
                }}
                data-testid="wh-create"
              >
                {tt('新建订阅')}
              </Button>
            )}
          </div>

          {readOnly && (
            <AlertBox severity="info" testid="wh-readonly-note">
              {tt('只读管理员：订阅面只读呈现（写动作禁用；服务端以 403 兜底）。')}
            </AlertBox>
          )}
          {slotLocked && (
            <AlertBox severity="warning" testid="wh-locked-note">
              {tt('webhook 功能槽未解锁（')}{slotRow?.minTier ?? 'pro'}{tt('+ 档特性）——读面可见，写操作（新建/编辑/删除/试发）将被服务端以 403 拒绝。')}
            </AlertBox>
          )}
          {lastTest && (
            <AlertBox severity={lastTest.outcome.ok ? 'success' : 'error'} testid="wh-test-last">
              {tt('试发')} {lastTest.key}{tt('：')}<span lang="en">{lastTest.outcome.message ?? tt('（无回执）')}</span>{tt('（')}
              <span lang="en">HTTP {lastTest.outcome.attempt?.status_code ?? '—'}</span>
              {lastTest.outcome.attempt?.status_code === 0 ? tt('（无响应）') : ''}{tt('，耗时')}{' '}
              <span className="font-mono" lang="en">{lastTest.outcome.attempt?.elapsed_millis ?? '—'}ms</span>{tt('）')}
            </AlertBox>
          )}

          {phase.kind === 'loading' && <StateSkeleton lines={6} />}
          {phase.kind === 'forbidden' && (
            <EmptyState
              message={tt('无权限查看 Webhook 订阅')}
              hint={tt('订阅面为管理员视图（GET /event/api/v1/subscriptions 仅 admin / readonly_admin）。')}
            />
          )}
          {phase.kind === 'error' && (
            <ErrorCard error={new ApiError(0, phase.message)} onRetry={() => void reload()} />
          )}
          {phase.kind === 'ok' && subs.length === 0 && (
            <EmptyState
              illustration
              message={tt('暂无 Webhook 订阅')}
              hint={tt('订阅一个事件域与接收器 URL，制品部署/删除等事件会以签名 JSON 信封 POST 到接收器（试发不入箱）。')}
              testid="wh-empty"
              action={
                adminWrite ? (
                  <Button
                    size="sm"
                    onClick={() => {
                      setEditing(null)
                      setDialogOpen(true)
                    }}
                    data-testid="wh-empty-create"
                  >
                    {tt('新建第一个订阅')}
                  </Button>
                ) : undefined
              }
            />
          )}
          {phase.kind === 'ok' && subs.length > 0 && (
            <div className="overflow-x-auto rounded-md border border-border">
              <table className="w-full text-dense" data-testid="wh-table">
                <thead>
                  <tr className="border-b border-border text-left text-aux text-muted-foreground">
                    <th scope="col" className="px-3 py-2 font-medium">{tt('启用')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">key</th>
                    <th scope="col" className="px-3 py-2 font-medium">{tt('事件域 / 类型')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{tt('接收器 URL')}</th>
                    <th scope="col" className="px-3 py-2 text-right font-medium">{tt('操作')}</th>
                  </tr>
                </thead>
                <tbody>
                  {subs.map((sub) => (
                    <tr key={sub.key} data-testid={`wh-row-${sub.key}`} className="border-b border-border/60 hover:bg-accent">
                      <td className="px-3 py-1.5">
                        <input
                          type="checkbox"
                          role="switch"
                          checked={sub.enabled}
                          disabled={readOnly || busyKey === sub.key}
                          onChange={(e) => void doToggle(sub, e.target.checked)}
                          aria-label={tt('启用订阅 {v1}', { v1: sub.key })}
                          data-testid={`wh-toggle-${sub.key}`}
                        />
                      </td>
                      <td className="px-3 py-1.5 font-mono" lang="en">{sub.key}</td>
                      <td className="px-3 py-1.5">
                        <span className="flex max-w-[320px] flex-wrap items-center gap-1">
                          <span className="badge neutral" lang="en">{sub.event_filter.domain}</span>
                          {sub.event_filter.event_types.slice(0, 3).map((t) => (
                            <span
                              key={t}
                              className={`badge ${isWired(sub.event_filter.domain, t) ? 'success' : 'neutral'}`}
                              lang="en"
                            >
                              {t}
                            </span>
                          ))}
                          {sub.event_filter.event_types.length > 3 && (
                            <span className="text-aux text-muted-foreground">+{sub.event_filter.event_types.length - 3}</span>
                          )}
                        </span>
                      </td>
                      <td className="max-w-[280px] break-all px-3 py-1.5 font-mono" lang="en">
                        {sub.handlers[0]?.url ?? '—'}{' '}
                        {sub.handlers[0]?.url && <CopyButton value={sub.handlers[0].url} label={tt('接收器 URL {v1}', { v1: sub.key })} />}
                      </td>
                      <td className="whitespace-nowrap px-3 py-1.5 text-right">
                        <button
                          type="button"
                          className="grid size-7 place-items-center rounded-sm hover:bg-accent disabled:opacity-40"
                          title={tt('订阅详情 + 最近投递记录')}
                          aria-label={tt('详情 {v1}', { v1: sub.key })}
                          onClick={() => setDrawerSub(sub)}
                          data-testid={`wh-open-${sub.key}`}
                        >
                          ☰
                        </button>
                        <button
                          type="button"
                          className="grid size-7 place-items-center rounded-sm hover:bg-accent disabled:opacity-40"
                          title={tt('试发（test——同步单发，不入箱）')}
                          aria-label={tt('试发 {v1}', { v1: sub.key })}
                          disabled={readOnly || busyKey === sub.key}
                          onClick={() => void doTest(sub)}
                          data-testid={`wh-test-${sub.key}`}
                        >
                          ➤
                        </button>
                        <button
                          type="button"
                          className="grid size-7 place-items-center rounded-sm hover:bg-accent disabled:opacity-40"
                          title={tt('编辑')}
                          aria-label={tt('编辑 {v1}', { v1: sub.key })}
                          disabled={readOnly}
                          onClick={() => {
                            setEditing(sub)
                            setDialogOpen(true)
                          }}
                          data-testid={`wh-edit-${sub.key}`}
                        >
                          ✎
                        </button>
                        <button
                          type="button"
                          className="grid size-7 place-items-center rounded-sm text-destructive hover:bg-accent disabled:opacity-40"
                          title={tt('删除（级联投递行）')}
                          aria-label={tt('删除 {v1}', { v1: sub.key })}
                          disabled={readOnly}
                          onClick={() => void doDelete(sub)}
                          data-testid={`wh-delete-${sub.key}`}
                        >
                          ✕
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {phase.kind === 'ok' && (
            <p className="mt-1 text-aux text-muted-foreground" data-testid="wh-count">
              {subs.length} {tt('个订阅')}
            </p>
          )}
        </>
      )}

      <SubscriptionDialog
        open={dialogOpen}
        editing={editing}
        readOnly={readOnly}
        onClose={() => setDialogOpen(false)}
        onSaved={() => {
          setDialogOpen(false)
          void reload()
        }}
      />
      {drawerSub && (
        <SubscriptionDrawer
          subscription={phase.kind === 'ok' ? (phase.subs.find((s) => s.key === drawerSub.key) ?? drawerSub) : drawerSub}
          onClose={() => setDrawerSub(null)}
        />
      )}
    </div>
  )
}
