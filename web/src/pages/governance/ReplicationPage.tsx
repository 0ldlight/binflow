import { useEffect, useState } from 'react'
import type { ComponentPropsWithoutRef } from 'react'

import Chip from '@mui/material/Chip'
import FormControlLabel from '@mui/material/FormControlLabel'
import Switch from '@mui/material/Switch'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'

import { useToast } from '../../app/ToastContext'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, apiJSON, canAdminWrite, errText, isReadOnlyAdmin } from '../../lib/api'
import { formatAuditTime, formatCount } from '../../lib/format'
import { useAuth } from '../../app/AuthContext'
import {
  getReplicationGlobalBlock,
  listReplicationConfigs,
  setReplicationBlock,
} from '../../lib/replications'
import { useAsync } from '../../lib/useAsync'
import { tr } from '../../i18n'

const tt = tr('governance')

// 复制面板（T-159）：push 复制状态 + 事件列表（治理组「复制」页）。
// GET /api/v1/replication/status（admin），每 10s 轮询（AC②）。T-462 增
// 「调度」列：GET /api/v1/replications 的 cron_exp/next_schedule_sync
// 投影按 id join（status 面不携 cron——读配置面；读失败降 '—'）。
//
// 契约假设（端点属 T-162 遗留的桥接票，尚不存在——形状按
// internal/replication/model.go 的真实 Go 类型推定，桥接票以本文件为准对齐）：
// - targets[] = ListConfigs 行（凭据字段不下发）与 Store.Status(id) 的
//   ConfigStatus 逐行拍平：ConfigStatus.ReplicationID 与配置 id 相等，
//   不重复透出；Succeeded 沿 Go 字段名（非 "success"）。
// - events[] = ReplicationTask（ListTasks，newest-first，跨配置合并取最近
//   N 条）；status 为 009 闭集 pending|in_progress|success|failed|skipped；
//   时间戳 RFC3339 UTC 文本，'' = 从未/未完成（沿 ADR-0007）。
// - 序列化沿 storage.MigrationStatusView 惯例：snake_case、int64 计数、
//   时间字段 *_at。
//
// 四态收敛（console-ux §5.1/§3.6.3）：
// - loading：骨架（仅首帧；轮询刷新不清空已到达的数据）
// - 403：页面主数据面整体无权限 → 单张无权限卡（L2，复用缺省
//   empty-state 锚；治理组导航本就 admin-only，这里兜底非 admin 直链）
// - 404 / 501：端点未桥接（当前二进制 404）或实例未启用复制 → 降级为
//   一句提示（不渲染表格）；空 targets[] = 已配置面正常但无目标 → 空态
// - 其它错误：无数据 → ErrorCard + 重试；有旧数据 → 保留表格，行内标
//   「上次刷新失败」——单次掉线不清屏
// 控制台只读呈现；复制配置 CRUD / trigger 归 REST 面与 CLI（M6 桥接票）。

/** 单个复制目标的聚合行（配置行 + ConfigStatus 拍平） */
export interface ReplicationTargetStatus {
  /** replications.id（= ConfigStatus.ReplicationID，不重复透出） */
  id: number
  /** 唯一人类可读名 */
  name: string
  /** 源仓库 key（本实例） */
  source_repo: string
  /** 目标实例 base URL */
  target_url: string
  /** 目标实例上的仓库 key */
  target_repo: string
  enabled: boolean
  // ---- ConfigStatus（Store.Status 的派生计数）----
  pending: number
  in_progress: number
  succeeded: number
  failed: number
  skipped: number
  /** MAX(completed_at) over 成功任务；'' = 从未成功 */
  last_success_at: string
  /** 配置行 updated_at（RFC3339；'' 兜底） */
  updated_at: string
}

/** 事件行 = ReplicationTask（newest-first，跨配置合并） */
export interface ReplicationEvent {
  id: number
  replication_id: number
  blob_sha256: string
  node_path: string
  /** 009 闭集：pending|in_progress|success|failed|skipped */
  status: string
  attempts: number
  /** '' until a failure records the reason */
  last_error: string
  created_at: string
  /** 终态时刻；pending/in_progress 为 '' */
  completed_at: string
}

/** GET /api/v1/replication/status 响应体 */
export interface ReplicationStatus {
  targets: ReplicationTargetStatus[]
  events: ReplicationEvent[]
}

/** 轮询周期：AC② 每 10s 自动刷新 */
export const REPLICATION_POLL_MS = 10_000

type Phase =
  | { kind: 'loading' }
  | { kind: 'ok'; data: ReplicationStatus }
  /** 403：非 admin——页面主数据面无权限（§3.6.3 L2） */
  | { kind: 'forbidden' }
  /** 404（端点未桥接）/ 501（实例未启用复制）——降级提示 */
  | { kind: 'unavailable'; reason: number }
  /** 其余错误；stale = 最后一次成功数据（有则保留呈现） */
  | { kind: 'error'; message: string; stale: ReplicationStatus | null }

/** 复制状态轮询：挂载即取一次，此后每 POLL_MS 刷一次；tick 驱动手动重试 */
function useReplicationStatus(): { phase: Phase; retry: () => void } {
  const [phase, setPhase] = useState<Phase>({ kind: 'loading' })
  const [tick, setTick] = useState(0)

  useEffect(() => {
    let alive = true
    const poll = () => {
      apiJSON<ReplicationStatus>('/v1/replication/status')
        .then((data) => {
          if (alive) setPhase({ kind: 'ok', data })
        })
        .catch((err: unknown) => {
          if (!alive) return
          const status = err instanceof ApiError ? err.status : 0
          if (status === 403) {
            setPhase({ kind: 'forbidden' })
            return
          }
          if (status === 404 || status === 501) {
            setPhase({ kind: 'unavailable', reason: status })
            return
          }
          setPhase((prev) => ({
            kind: 'error',
            message: errText(err),
            stale: prev.kind === 'ok' ? prev.data : null,
          }))
        })
    }
    void poll()
    const timer = setInterval(() => void poll(), REPLICATION_POLL_MS)
    return () => {
      alive = false
      clearInterval(timer)
    }
  }, [tick])

  return { phase, retry: () => setTick((t) => t + 1) }
}

/** 目标行运行态：停用 > 失败 > 进行中 > 排队 > 正常（一次只呈现最要紧的） */
function targetState(t: ReplicationTargetStatus): { label: string; dot: string } {
  if (!t.enabled) return { label: tt('已停用'), dot: 'warn' }
  if (t.failed > 0) return { label: tt('异常（{v1} 失败）', { v1: formatCount(t.failed) }), dot: 'err' }
  if (t.in_progress > 0) return { label: tt('复制中'), dot: 'warn' }
  if (t.pending > 0) return { label: tt('排队（{v1}）', { v1: formatCount(t.pending) }), dot: 'warn' }
  return { label: tt('正常'), dot: 'ok' }
}

/** badge 类名 → MUI Chip color（neutral = default filled；语义色走
 *  outlined——filled 对比度见 T-344D 差异登记） */
const TASK_COLOR: Record<string, 'success' | 'warning' | 'error' | 'default'> = {
  success: 'success',
  warning: 'warning',
  danger: 'error',
  neutral: 'default',
}

/** 任务状态 → badge 色（值原样呈现不翻译——排障要比对 API，§4.10 同口径） */
const TASK_BADGE: Record<string, string> = {
  pending: 'warning',
  in_progress: 'warning',
  success: 'success',
  failed: 'danger',
  skipped: 'neutral',
}

/** digest 展示可截断，拷贝复制完整值（§7.3） */
function shortSha(s: string): string {
  return s.length > 14 ? `${s.slice(0, 7)}…${s.slice(-4)}` : s
}

/** 全局封锁双开关卡（T-422，FR-138.3 / parity §6A R8）：blockPush/
 *  blockPull 应急刹车——形态照 R8（General Settings 字段族成员 + tooltip
 *  "regardless of configuration"）；两方向独立翻转，GET/POST
 *  /api/v1/system/replications 族（规格 §9.1-B/§9.2-B）。封锁只拦复制执行，
 *  不拦本面板与配置面（t226 UI-API 不受门）。 */
function GlobalBlockCard() {
  const { session } = useAuth()
  const toast = useToast()
  const readOnly = isReadOnlyAdmin(session)
  const adminWrite = canAdminWrite(session)
  const [state, setState] = useState<'loading' | 'ok' | 'error'>('loading')
  const [flags, setFlags] = useState({ push: false, pull: false })
  const [busy, setBusy] = useState<'' | 'push' | 'pull'>('')

  const load = () => {
    getReplicationGlobalBlock()
      .then((b) => {
        setFlags({ push: b.blockPushReplications, pull: b.blockPullReplications })
        setState('ok')
      })
      .catch(() => setState('error'))
  }
  useEffect(load, [])

  const flip = async (dir: 'push' | 'pull', next: boolean) => {
    setBusy(dir)
    try {
      // 只动一个方向（§9.2-B-1 选择器）：另一方向显式 false = 本次不动。
      const msg = await setReplicationBlock(next, dir === 'push', dir === 'pull')
      toast.success(msg)
      load()
    } catch (err) {
      toast.error(tt('封锁开关翻转失败：{v1}', { v1: errText(err) }))
      load() // 行内不乐观更新——失败后回读服务端真值
    } finally {
      setBusy('')
    }
  }

  if (state === 'loading') return <Skeleton lines={2} />
  return (
    <section className="card section" data-testid="repl-global-block">
      <h3>{tt('全局封锁（应急刹车）')}</h3>
      {state === 'error' ? (
        <p className="field-hint" style={{ marginBottom: 0 }}>{tt('全局封锁状态读取失败（GET /api/v1/system/replications）。')}        </p>
      ) : (
        <>
          <FormControlLabel
            disabled={!adminWrite || busy !== ''}
            control={
              <Switch
                size="small"
                checked={flags.push}
                onChange={(e) => void flip('push', e.target.checked)}
                slotProps={
                  {
                    input: { 'data-testid': 'repl-block-push' } as ComponentPropsWithoutRef<'input'>,
                  } as { input: ComponentPropsWithoutRef<'input'> }
                }
              />
            }
            label={tt('封锁 push 复制（blockPushReplications）')}
          />
          <FormControlLabel
            disabled={!adminWrite || busy !== ''}
            control={
              <Switch
                size="small"
                checked={flags.pull}
                onChange={(e) => void flip('pull', e.target.checked)}
                slotProps={
                  {
                    input: { 'data-testid': 'repl-block-pull' } as ComponentPropsWithoutRef<'input'>,
                  } as { input: ComponentPropsWithoutRef<'input'> }
                }
              />
            }
            label={tt('封锁 pull 复制（blockPullReplications）——remote 回源/智能拉取零上游流量')}
          />
          <p className="field-hint" style={{ marginBottom: 0 }}>{tt('设定后，无论各复制配置如何，push/pull 复制都不会触发（R8 tooltip 语义）； 已缓存制品照常服务（pull 侧仅停上游接触）。封锁不影响本面板与复制配置的读写。')}            {readOnly ? tt('（当前会话为只读管理员——开关只读呈现）') : ''}
          </p>
        </>
      )}
    </section>
  )
}

function ReplicationBody({
  data,
  staleError,
  cronOf,
}: {
  data: ReplicationStatus
  staleError?: string
  /** id → 配置行的 cron 投影（GET /v1/replications 回显；null = 该行无
   *  配置回读（列表失败/行已删）——调度列如实降 '—'） */
  cronOf: Map<number, { cron_exp: string; next_schedule_sync: string; enabled: boolean }> | null
}) {
  // replication_id → 源仓库（事件行只带 id，制品路径补全用）
  const repoOf = new Map(data.targets.map((t) => [t.id, t.source_repo]))

  return (
    <>
      <section className="card section" data-testid="repl-targets">
        <h3>{tt('复制目标')}</h3>
        {data.targets.length === 0 ? (
          <EmptyState
            message={tt('未配置复制目标')}
            hint={tt('复制为单向 push（源仓库 → 目标实例仓库）；目标配置经 REST /api/v1/replications 或实例配置创建（M6 桥接后可用）。')}
            testid="repl-empty-targets"
          />
        ) : (
          <Table data-testid="repl-targets-table">
            <TableHead>
              <TableRow>
                <TableCell component="th" scope="col">{tt('状态')}</TableCell>
                <TableCell component="th" scope="col">{tt('目标')}</TableCell>
                <TableCell component="th" scope="col">URL</TableCell>
                <TableCell component="th" scope="col">{tt('仓库（源 → 目标）')}</TableCell>
                <TableCell component="th" scope="col">{tt('调度')}</TableCell>
                <TableCell component="th" scope="col" lang="en">
                  pending
                </TableCell>
                <TableCell component="th" scope="col">{tt('进行中')}</TableCell>
                <TableCell component="th" scope="col">{tt('失败')}</TableCell>
                <TableCell component="th" scope="col">{tt('累计成功')}</TableCell>
                <TableCell component="th" scope="col">{tt('上次成功')}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {data.targets.map((t, i) => {
                const st = targetState(t)
                const cron = cronOf?.get(t.id) ?? null
                return (
                  <TableRow key={t.id} data-testid={`repl-target-${i}`} hover>
                    <TableCell>
                      <span className={`status-dot ${st.dot}`} aria-hidden="true" /> {st.label}
                    </TableCell>
                    <TableCell lang="en">{t.name}</TableCell>
                    <TableCell className="mono" sx={{ maxWidth: 240, whiteSpace: 'normal', wordBreak: 'break-all' }} lang="en">
                      {t.target_url} <CopyButton value={t.target_url} label={tt('目标 URL {v1}', { v1: t.name })} />
                    </TableCell>
                    <TableCell className="mono" lang="en">
                      {t.source_repo} → {t.target_repo}
                    </TableCell>
                    <TableCell data-testid={`repl-sched-${i}`}>
                      {cron === null ? (
                        <span className="text-muted">—</span>
                      ) : cron.cron_exp ? (
                        <>
                          <span className="mono" lang="en">{cron.cron_exp}</span>
                          <br />
                          <span className="text-2" title={cron.next_schedule_sync}>
                            {cron.enabled && cron.next_schedule_sync
                              ? tt('下次 {v1}', { v1: cron.next_schedule_sync.replace('T', ' ').replace(/(\.\d+)?Z$/, ' UTC') })
                              : cron.enabled
                                ? tt('未排')
                                : tt('已停用')}
                          </span>
                        </>
                      ) : (
                        <span className="text-muted">{tt('事件驱动')}</span>
                      )}
                    </TableCell>
                    <TableCell className="mono" lang="en">
                      {formatCount(t.pending)}
                    </TableCell>
                    <TableCell className="mono" lang="en">
                      {formatCount(t.in_progress)}
                    </TableCell>
                    <TableCell
                      className="mono"
                      lang="en"
                      sx={t.failed > 0 ? { color: 'error.main' } : undefined}
                    >
                      {formatCount(t.failed)}
                    </TableCell>
                    <TableCell className="mono" lang="en">
                      {formatCount(t.succeeded)}
                    </TableCell>
                    <TableCell className="mono" title={t.last_success_at || undefined}>
                      {t.last_success_at ? formatAuditTime(t.last_success_at) : '—'}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
        {staleError && (
          <p className="field-error" data-testid="repl-stale" role="alert">{tt('上次刷新失败：')}{staleError}{tt('（每')} {REPLICATION_POLL_MS / 1000} {tt('秒自动重试，以上为最后一次成功数据）')}          </p>
        )}
        <p className="field-hint" style={{ marginBottom: 0 }}>{tt('每')} {REPLICATION_POLL_MS / 1000} {tt('秒自动刷新（GET /api/v1/replication/status）；复制为单向 push——源上传后异步推送，失败按 1s→16s 指数退避重试（最多 6 次后终态）。「调度」列自 配置面（GET /api/v1/replications）join：cron 到点触发全量对账，事件轨照常承载增量（同制品 不双推）；表达式在仓库编辑页 Replications 节配置。')}        </p>
      </section>

      <section className="card section" data-testid="repl-events">
        <h3>{tt('最近事件')}</h3>
        {data.events.length === 0 ? (
          <EmptyState
            message={tt('暂无复制事件')}
            hint={tt('源仓库有新上传且存在启用的复制目标时，推送事件会出现在这里（时间倒序）。')}
            testid="repl-empty-events"
          />
        ) : (
          <Table data-testid="repl-events-table">
            <TableHead>
              <TableRow>
                <TableCell component="th" scope="col">{tt('时间')}</TableCell>
                <TableCell component="th" scope="col">{tt('状态')}</TableCell>
                <TableCell component="th" scope="col">{tt('制品')}</TableCell>
                <TableCell component="th" scope="col">sha256</TableCell>
                <TableCell component="th" scope="col">{tt('尝试')}</TableCell>
                <TableCell component="th" scope="col">{tt('错误')}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {data.events.map((ev, i) => {
                const repo = repoOf.get(ev.replication_id)
                const artifact = repo ? `${repo}/${ev.node_path}` : ev.node_path
                return (
                  <TableRow key={ev.id} data-testid={`repl-event-${i}`} hover>
                    <TableCell className="mono audit-time" title={ev.created_at}>
                      {formatAuditTime(ev.created_at)}
                    </TableCell>
                    <TableCell>
                      <Chip
                        size="small"
                        variant="outlined"
                        color={TASK_COLOR[TASK_BADGE[ev.status] ?? 'neutral'] ?? 'default'}
                        className={`badge ${TASK_BADGE[ev.status] ?? 'neutral'}`}
                        label={ev.status}
                        lang="en"
                      />
                    </TableCell>
                    <TableCell className="mono" sx={{ maxWidth: 320, whiteSpace: 'normal', wordBreak: 'break-all' }} lang="en">
                      {artifact} <CopyButton value={artifact} label={tt('制品路径 {artifact}', { artifact: artifact })} />
                    </TableCell>
                    <TableCell className="mono" lang="en" title={ev.blob_sha256}>
                      {shortSha(ev.blob_sha256)}{' '}
                      <CopyButton value={ev.blob_sha256} label={`sha256 ${ev.blob_sha256}`} />
                    </TableCell>
                    <TableCell className="mono" lang="en">
                      {formatCount(ev.attempts)}
                    </TableCell>
                    <TableCell className="mono" sx={{ maxWidth: 320, whiteSpace: 'normal', wordBreak: 'break-all' }}>
                      {ev.last_error || <span className="text-muted">—</span>}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
        <p className="field-hint" style={{ marginBottom: 0 }}>{tt('事件为最近的推送尝试（时间倒序，全目标合并）；排队 / 进行中为未决任务，失败行保留最近一次错误原因。')}        </p>
      </section>
    </>
  )
}

export default function ReplicationPage() {
  const { phase, retry } = useReplicationStatus()
  // 配置面（GET /v1/replications）：targets 的 cron/next-sync 投影 join 源
  //（T-462——status 面不携 cron 字段，读配置面回显）。读失败 = 调度列降
  // '—'（状态面主数据不受牵连——cron 是投影不是状态）。
  const configs = useAsync(listReplicationConfigs, [])
  const cronOf =
    configs.status === 'ok' && configs.data
      ? new Map(
          configs.data.map((c) => [
            c.id,
            { cron_exp: c.cron_exp, next_schedule_sync: c.next_schedule_sync, enabled: c.enabled },
          ]),
        )
      : null

  const okData = phase.kind === 'ok' ? phase.data : phase.kind === 'error' ? phase.stale : null

  return (
    <div data-testid="repl-page">
      <div className="page-header">
        <h2>{tt('复制')}</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>{tt('单向 push：源仓库 → 目标实例（ADR-0021）——事件轨 + 可选定时全量双轨')}        </span>
      </div>

      {phase.kind === 'loading' && <Skeleton lines={8} />}
      {phase.kind === 'forbidden' && (
        <EmptyState
          message={tt('无权限查看复制状态')}
          hint={tt('复制面板为管理员视图（GET /api/v1/replication/status 仅 admin）。')}
        />
      )}
      {phase.kind === 'unavailable' && (
        <section className="card section" data-testid="repl-unavailable">
          <h3>{tt('复制')}</h3>
          <p className="field-hint" style={{ marginBottom: 0 }}>
            {phase.reason === 501
              ? tt('本实例未启用复制（replication 配置段缺失，端点返回 501）；在实例配置中启用后本面板自动呈现目标与事件。')
              : tt('本实例的复制状态端点不可用（HTTP 404——服务端尚未桥接 GET /api/v1/replication/status）；桥接后本面板自动呈现目标与事件。')}
          </p>
        </section>
      )}
      {phase.kind === 'error' && phase.stale === null && (
        <ErrorCard error={new ApiError(0, phase.message)} onRetry={retry} />
      )}
      {/* T-422：全局封锁双开关——独立于状态面的只读/降级态（封锁面自身
          可用即呈现；与状态面 403/404/501 分开收敛） */}
      {phase.kind !== 'forbidden' && <GlobalBlockCard />}
      {okData && (
        <ReplicationBody
          data={okData}
          staleError={phase.kind === 'error' ? phase.message : undefined}
          cronOf={cronOf}
        />
      )}
    </div>
  )
}
