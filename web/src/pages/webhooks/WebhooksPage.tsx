import { useCallback, useEffect, useState } from 'react'
import type { ComponentPropsWithoutRef } from 'react'

import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import IconButton from '@mui/material/IconButton'
import Paper from '@mui/material/Paper'
import Switch from '@mui/material/Switch'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { useConfirm } from '../../components/ConfirmDialog'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, canAdminWrite, errText, isReadOnlyAdmin } from '../../lib/api'
import { getAddons } from '../../lib/addons'
import type { AddonRow } from '../../lib/addons'
import {
  deleteSubscription,
  isWired,
  listSubscriptions,
  testSubscription,
  updateSubscription,
} from '../../lib/webhooks'
import type { SubscriptionRequest, TestOutcome, WebhookSubscription } from '../../lib/webhooks'
import SubscriptionDialog from './SubscriptionDialog'
import SubscriptionDrawer from './SubscriptionDrawer'

// Webhook 订阅管理页（M13 T-366，FR-115.5——治理分组「Webhooks」）。最小面：
//
// - 列表：GET /event/api/v1/subscriptions（读门 = system:read——
//   readonly_admin 可见；**读面不过 license 门**〔D1：锁定实例上已配订阅
//   仍可见〕）。行内：启停开关（PUT 全量体——key/project_key 不可改、
//   secret 省略 = 保持）、详情抽屉、试发（POST …/test 吃完整订阅体）、
//   编辑对话框、删除（danger 确认——订阅删除级联投递行）。
// - 门控呈现：写动词过 webhook 槽（pro+）——community 实例试写收
//   403 + X-Binflow-License-Required: webhook（服务端终裁；FE 只以
//   /api/v1/addons 槽行驱动提示文案，不复制门控）。
// - 四态：骨架 / 空态（从未有过订阅）/ 403 无权限卡 / 错误卡 + 重试。
// - 交互形态照 console-artifactory-parity：新建/编辑 = Dialog（M3/M4），
//   详情 + 最近投递记录 = 右侧 Drawer（抽屉族通用规格）。

type ListPhase =
  | { kind: 'loading' }
  | { kind: 'ok'; subs: WebhookSubscription[] }
  /** 403：非 admin/readonly_admin——页面主数据面无权限（L2 无权限卡） */
  | { kind: 'forbidden' }
  | { kind: 'error'; message: string }

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

export default function WebhooksPage() {
  const { session } = useAuth()
  const readOnly = isReadOnlyAdmin(session)
  const adminWrite = canAdminWrite(session)
  const toast = useToast()
  const confirm = useConfirm()

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
      if (outcome.ok) toast.success(`试发成功（${sub.key}）：${outcome.message}`)
      else toast.error(`试发失败（${sub.key}）：${outcome.message}`)
    } catch (err) {
      const msg =
        err instanceof ApiError && err.status === 403
          ? `试发被拒（403）：${err.message}——webhook 为 pro+ 档特性`
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
      toast.success(`订阅 ${sub.key} 已${enabled ? '启用' : '停用'}`)
      await reload()
    } catch (err) {
      const msg =
        err instanceof ApiError && err.status === 403
          ? `写入被拒（403）：${err.message}——webhook 为 pro+ 档特性`
          : errText(err)
      toast.error(msg)
    } finally {
      setBusyKey('')
    }
  }

  const doDelete = async (sub: WebhookSubscription) => {
    const ok = await confirm({
      title: `删除订阅 ${sub.key}`,
      body: '删除后该订阅的全部投递记录一并级联清除；接收器不会再收到任何事件。此操作不可撤销。',
      confirmLabel: '删除',
      danger: true,
    })
    if (!ok) return
    setBusyKey(sub.key)
    try {
      await deleteSubscription(sub.key)
      toast.success(`订阅 ${sub.key} 已删除`)
      if (drawerSub?.key === sub.key) setDrawerSub(null)
      await reload()
    } catch (err) {
      toast.error(`删除失败：${errText(err)}`)
    } finally {
      setBusyKey('')
    }
  }

  return (
    <div data-testid="wh-page">
      <div className="page-header">
        <h2>Webhooks</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>
          统一事件订阅（/binflow/event/api/v1——13 域 66 事件型；outbox 投递，失败重试固定 10s×4）
        </span>
      </div>
      <Box sx={{ display: 'flex', gap: 1, mb: 1, alignItems: 'center' }}>
        <Box sx={{ flexGrow: 1 }} />
        <Button
          variant="outlined"
          onClick={() => void reload()}
          data-testid="wh-refresh"
          sx={{ minWidth: 0 }}
        >
          刷新
        </Button>
        {adminWrite && (
          <Button
            variant="contained"
            onClick={() => {
              setEditing(null)
              setDialogOpen(true)
            }}
            data-testid="wh-create"
          >
            新建订阅
          </Button>
        )}
      </Box>

      {readOnly && (
        <Alert severity="info" data-testid="wh-readonly-note" sx={{ mb: 1 }}>
          只读管理员：订阅面只读呈现（写动作禁用；服务端以 403 兜底）。
        </Alert>
      )}
      {slotLocked && (
        <Alert severity="warning" data-testid="wh-locked-note" sx={{ mb: 1 }}>
          webhook 功能槽未解锁（{slotRow?.minTier ?? 'pro'}+ 档特性）——读面可见，写操作（新建/编辑/删除/试发）将被服务端以 403 拒绝。
        </Alert>
      )}
      {lastTest && (
        <Alert
          severity={lastTest.outcome.ok ? 'success' : 'error'}
          data-testid="wh-test-last"
          sx={{ mb: 1 }}
        >
          试发 {lastTest.key}：<span lang="en">{lastTest.outcome.message ?? '（无回执）'}</span>（
          <span lang="en">HTTP {lastTest.outcome.attempt?.status_code ?? '—'}</span>
          {lastTest.outcome.attempt?.status_code === 0 ? '（无响应）' : ''}，耗时{' '}
          <span className="mono" lang="en">{lastTest.outcome.attempt?.elapsed_millis ?? '—'}ms</span>）
        </Alert>
      )}

      {phase.kind === 'loading' && <Skeleton lines={6} />}
      {phase.kind === 'forbidden' && (
        <EmptyState
          message="无权限查看 Webhook 订阅"
          hint="订阅面为管理员视图（GET /event/api/v1/subscriptions 仅 admin / readonly_admin）。"
        />
      )}
      {phase.kind === 'error' && (
        <ErrorCard error={new ApiError(0, phase.message)} onRetry={() => void reload()} />
      )}
      {phase.kind === 'ok' && subs.length === 0 && (
        <EmptyState
          message="暂无 Webhook 订阅"
          hint="订阅一个事件域与接收器 URL，制品部署/删除等事件会以签名 JSON 信封 POST 到接收器（试发不入箱）。"
          testid="wh-empty"
          action={
            adminWrite ? (
              <Button
                variant="contained"
                onClick={() => {
                  setEditing(null)
                  setDialogOpen(true)
                }}
                data-testid="wh-empty-create"
              >
                新建第一个订阅
              </Button>
            ) : undefined
          }
        />
      )}
      {phase.kind === 'ok' && subs.length > 0 && (
        <Paper variant="outlined">
          <Table size="small" data-testid="wh-table">
            <TableHead>
              <TableRow>
                <TableCell component="th" scope="col">启用</TableCell>
                <TableCell component="th" scope="col">key</TableCell>
                <TableCell component="th" scope="col">事件域 / 类型</TableCell>
                <TableCell component="th" scope="col">接收器 URL</TableCell>
                <TableCell component="th" scope="col" align="right">操作</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {subs.map((sub) => (
                <TableRow key={sub.key} data-testid={`wh-row-${sub.key}`} hover>
                  <TableCell>
                    <Switch
                      checked={sub.enabled}
                      disabled={readOnly || busyKey === sub.key}
                      onChange={(e) => void doToggle(sub, e.target.checked)}
                      size="small"
                      slotProps={
                        {
                          input: {
                            'aria-label': `启用订阅 ${sub.key}`,
                            'data-testid': `wh-toggle-${sub.key}`,
                          },
                        } as { input: ComponentPropsWithoutRef<'input'> }
                      }
                    />
                  </TableCell>
                  <TableCell className="mono" lang="en">
                    {sub.key}
                  </TableCell>
                  <TableCell>
                    <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5, alignItems: 'center', maxWidth: 320 }}>
                      <Chip size="small" variant="outlined" label={sub.event_filter.domain} lang="en" />
                      {sub.event_filter.event_types.slice(0, 3).map((t) => (
                        <Chip
                          key={t}
                          size="small"
                          variant="outlined"
                          color={isWired(sub.event_filter.domain, t) ? 'success' : 'default'}
                          label={t}
                          lang="en"
                        />
                      ))}
                      {sub.event_filter.event_types.length > 3 && (
                        <Typography variant="caption" color="text.secondary">
                          +{sub.event_filter.event_types.length - 3}
                        </Typography>
                      )}
                    </Box>
                  </TableCell>
                  <TableCell className="mono" lang="en" sx={{ maxWidth: 280, whiteSpace: 'normal', wordBreak: 'break-all' }}>
                    {sub.handlers[0]?.url ?? '—'}{' '}
                    {sub.handlers[0]?.url && <CopyButton value={sub.handlers[0].url} label={`接收器 URL ${sub.key}`} />}
                  </TableCell>
                  <TableCell align="right" sx={{ whiteSpace: 'nowrap' }}>
                    <Tooltip title="订阅详情 + 最近投递记录">
                      <IconButton
                        aria-label={`详情 ${sub.key}`}
                        onClick={() => setDrawerSub(sub)}
                        data-testid={`wh-open-${sub.key}`}
                        size="small"
                      >
                        ☰
                      </IconButton>
                    </Tooltip>
                    <Tooltip title="试发（test——同步单发，不入箱）">
                      <IconButton
                        aria-label={`试发 ${sub.key}`}
                        disabled={readOnly || busyKey === sub.key}
                        onClick={() => void doTest(sub)}
                        data-testid={`wh-test-${sub.key}`}
                        size="small"
                      >
                        ➤
                      </IconButton>
                    </Tooltip>
                    <Tooltip title="编辑">
                      <IconButton
                        aria-label={`编辑 ${sub.key}`}
                        disabled={readOnly}
                        onClick={() => {
                          setEditing(sub)
                          setDialogOpen(true)
                        }}
                        data-testid={`wh-edit-${sub.key}`}
                        size="small"
                      >
                        ✎
                      </IconButton>
                    </Tooltip>
                    <Tooltip title="删除（级联投递行）">
                      <IconButton
                        aria-label={`删除 ${sub.key}`}
                        disabled={readOnly}
                        onClick={() => void doDelete(sub)}
                        data-testid={`wh-delete-${sub.key}`}
                        size="small"
                        color="error"
                      >
                        ✕
                      </IconButton>
                    </Tooltip>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Paper>
      )}
      {phase.kind === 'ok' && (
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }} data-testid="wh-count">
          {subs.length} 个订阅
        </Typography>
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
