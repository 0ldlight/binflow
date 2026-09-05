import { useCallback, useEffect, useState } from 'react'

import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import Divider from '@mui/material/Divider'
import Drawer from '@mui/material/Drawer'
import IconButton from '@mui/material/IconButton'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import Typography from '@mui/material/Typography'

import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, errText } from '../../lib/api'
import { getTroubleshooting, isWired } from '../../lib/webhooks'
import type { TroubleshootingRecord, WebhookSubscription } from '../../lib/webhooks'
import { tr } from '../../i18n'

const tt = tr('webhooks')

// 订阅详情 / 最近投递记录抽屉（M13 T-366；形态 = console-artifactory-parity
// 抽屉族通用规格：右侧滑入、宽 480 档、右上 X + Esc/遮罩关闭、内部滚动）。
//
// 「最近投递记录」= GET /event/api/v1/troubleshooting?subscription=<key>
// （webhook.md §7 排障环）：**失败必录；成功仅 debug:true 订阅入记录**——
// 空列表 ≠ 无投递，空态文案如实说明。每行呈现状态（response.status 或
// 发送失败）/ 耗时（elapsed_millis）/ 重试计数（retries_attempted），
// payload 快照可展开（mono + 一键拷贝，§7.3）。

const DRAWER_WIDTH = 480

function formatMillis(ts: number): string {
  const t = new Date(ts)
  if (Number.isNaN(t.getTime())) return String(ts)
  const pad = (v: number) => String(v).padStart(2, '0')
  return `${t.getFullYear()}-${pad(t.getMonth() + 1)}-${pad(t.getDate())} ${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}`
}

/** 一条记录的可呈现状态 */
function recordStatus(rec: TroubleshootingRecord): { label: string; color: 'success' | 'error' | 'warning' } {
  if (rec.errors.length > 0 && rec.response.status === 0) {
    return { label: tt('发送失败'), color: 'error' }
  }
  const s = rec.response.status
  if (s >= 200 && s < 300) return { label: tt('{s} 已送达', { s: s }), color: 'success' }
  return { label: `${s}`, color: 'error' }
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
    <Drawer
      anchor="right"
      open
      onClose={onClose}
      slotProps={{
        paper: {
          // modal Drawer 的 paper 承载 role="dialog"——需要可及名（axe
          // aria-dialog-name）；标题 = 订阅 key
          'aria-label': tt('订阅详情 {v1}', { v1: sub.key }),
          sx: { width: `min(${DRAWER_WIDTH}px, 100vw - 32px)`, display: 'flex', flexDirection: 'column' },
        },
      }}
      data-testid="wh-drawer"
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2, borderBottom: 1, borderColor: 'divider' }}>
        <Typography variant="subtitle1" sx={{ fontWeight: 600, wordBreak: 'break-all' }} lang="en">
          {sub.key}
        </Typography>
        <Box sx={{ flexGrow: 1 }} />
        <Chip size="small" variant="outlined" color={sub.enabled ? 'success' : 'default'} label={sub.enabled ? tt('启用') : tt('停用')} />
        {sub.debug && <Chip size="small" variant="outlined" color="info" label="debug" />}
        <IconButton aria-label={tt('关闭')} onClick={onClose} data-testid="wh-drawer-close">
          ✕
        </IconButton>
      </Box>

      <Box sx={{ flex: 1, overflowY: 'auto', p: 2, display: 'grid', gap: 2, alignContent: 'start' }}>
        {sub.description && <Typography color="text.secondary">{sub.description}</Typography>}

        <Box sx={{ display: 'grid', gap: 0.5 }}>
          <Typography variant="caption" color="text.secondary">{tt('事件域')}          </Typography>
          <div lang="en" className="mono">
            {sub.event_filter.domain}
          </div>
          <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5 }}>{tt('事件型（wired = 有触发源）')}          </Typography>
          <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5 }}>
            {sub.event_filter.event_types.map((t) => (
              <Chip
                key={t}
                size="small"
                variant="outlined"
                color={isWired(sub.event_filter.domain, t) ? 'success' : 'default'}
                label={t}
                lang="en"
              />
            ))}
          </Box>
        </Box>

        <Box sx={{ display: 'grid', gap: 0.5 }}>
          <Typography variant="caption" color="text.secondary">{tt('过滤条件（criteria）')}          </Typography>
          <pre className="mono" data-testid="wh-drawer-criteria" style={{ margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-all', fontSize: 12 }}>
            {JSON.stringify(criteria, null, 2)}
          </pre>
        </Box>

        <Box sx={{ display: 'grid', gap: 0.5 }}>
          <Typography variant="caption" color="text.secondary">{tt('投递目标（handler）')}          </Typography>
          <div className="mono" lang="en" style={{ wordBreak: 'break-all' }}>
            {handler?.url ?? '—'} {handler?.url && <CopyButton value={handler.url} label={tt('接收器 URL')} />}
          </div>
          <Typography variant="body2" color="text.secondary">{tt('secret：')}{handler?.secret ? tt('已设置（write-only，回显为掩码）') : tt('未设置')}
            {handler?.secret && handler.use_secret_for_signing ? tt('；签名态（HMAC-SHA256 → X-JFrog-Event-Auth）') : ''}
          </Typography>
        </Box>

        <Divider />

        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Typography variant="subtitle2">{tt('最近投递记录（排障环）')}</Typography>
          <Box sx={{ flexGrow: 1 }} />
          <Button size="small" onClick={() => void load(sub.key)} data-testid="wh-records-refresh">{tt('刷新')}          </Button>
        </Box>

        {phase.kind === 'loading' && <Skeleton lines={4} />}
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
            <Table size="small" data-testid="wh-records-table">
              <TableHead>
                <TableRow>
                  <TableCell component="th" scope="col">{tt('时间')}</TableCell>
                  <TableCell component="th" scope="col">{tt('状态')}</TableCell>
                  <TableCell component="th" scope="col">{tt('事件')}</TableCell>
                  <TableCell component="th" scope="col" align="right">{tt('耗时')}</TableCell>
                  <TableCell component="th" scope="col" align="right">{tt('重试')}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {phase.records.map((rec, i) => {
                  const st = recordStatus(rec)
                  return (
                    <TableRow
                      key={`${rec.timestamp}-${i}`}
                      data-testid={`wh-record-${i}`}
                      hover
                      sx={{ cursor: 'pointer' }}
                      onClick={() => setExpanded(expanded === i ? null : i)}
                    >
                      <TableCell className="mono" sx={{ whiteSpace: 'nowrap' }} title={String(rec.timestamp)}>
                        {formatMillis(rec.timestamp)}
                      </TableCell>
                      <TableCell>
                        <Chip size="small" variant="outlined" color={st.color} label={st.label} />
                      </TableCell>
                      <TableCell className="mono" lang="en">
                        {rec.event.event_type}
                      </TableCell>
                      <TableCell className="mono" align="right" lang="en">
                        {rec.elapsed_millis}ms
                      </TableCell>
                      <TableCell className="mono" align="right" lang="en">
                        {rec.request.retries_attempted}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
            {expanded !== null && phase.records[expanded] && (
              <Box data-testid={`wh-record-payload-${expanded}`} sx={{ border: 1, borderColor: 'divider', p: 1 }}>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.5 }}>
                  <Typography variant="caption" color="text.secondary">{tt('投递载荷快照（点击行收起）')}{phase.records[expanded].errors.length > 0 && tt('；错误：')}
                  </Typography>
                  {phase.records[expanded].errors.length > 0 && (
                    <Typography variant="caption" color="error" className="mono" style={{ wordBreak: 'break-all' }}>
                      {phase.records[expanded].errors.join('; ')}
                    </Typography>
                  )}
                  <Box sx={{ flexGrow: 1 }} />
                  <CopyButton value={phase.records[expanded].request.payload} label={tt('投递载荷 JSON')} />
                </Box>
                <pre
                  className="mono"
                  aria-label={tt('投递载荷 JSON')}
                  style={{ margin: 0, maxHeight: 240, overflow: 'auto', fontSize: 12, whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}
                >
                  {phase.records[expanded].request.payload}
                </pre>
                {phase.records[expanded].response.body && (
                  <Typography variant="caption" color="text.secondary" component="div" sx={{ mt: 0.5, wordBreak: 'break-all' }}>{tt('接收器应答体：')}<span className="mono">{phase.records[expanded].response.body.slice(0, 512)}</span>
                  </Typography>
                )}
              </Box>
            )}
          </>
        )}
        <Box sx={{ flexGrow: 1 }} />
        <Typography variant="caption" color="text.secondary">{tt('投递语义（官方锚点）：失败或 ≥500 按固定 10s 重试、首试计入共 5 次；4xx/3xx 不重试一步终态；重试耗尽行标 dead。')}        </Typography>
      </Box>
    </Drawer>
  )
}
