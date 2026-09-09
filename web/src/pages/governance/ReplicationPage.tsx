// 复制面板（T-159——P3 新栈重写：TanStack Query refetchInterval 复刻 10s
// 轮询 + stale 保留语义）：
// - GET /api/v1/replication/status 每 10s 轮询（Query 的轮询失败不清空
//   已到达数据——isError + data 并存即「上次刷新失败」行内提示）；
// - 「调度」列：GET /api/v1/replications 的 cron_exp/next_schedule_sync
//   投影按 id join（读失败降 '—'）；
// - 全局封锁双开关卡（T-422）：blockPush/blockPull 应急刹车——失败回读
//   不乐观更新；
// - 404/501 降级提示；403 → L2 无权限卡。
// 锚族原样：repl-page/repl-global-block/repl-block-push/repl-block-pull/
// repl-targets(-table)?/repl-target-<i>/repl-sched-<i>/repl-empty-targets/
// repl-events(-table)?/repl-event-<i>/repl-stale/repl-unavailable。
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'

import { useAuth } from '@/app/AuthContext'
import { Badge, CheckRow } from '@/components/layout/bits'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { toast } from '@/lib/toast'
import { ApiError, apiJSON, errText, canAdminWrite, isReadOnlyAdmin } from '@/lib/api'
import { formatAuditTime, formatCount } from '@/lib/format'
import { POLL_INTERVALS } from '@/lib/query'
import {
  getReplicationGlobalBlock,
  listReplicationConfigs,
  setReplicationBlock,
} from '@/lib/replications'
import { useAsync } from '@/lib/useAsync'
import { tr } from '@/i18n'

const tt = tr('governance')

/** 单个复制目标的聚合行（配置行 + ConfigStatus 拍平） */
export interface ReplicationTargetStatus {
  id: number
  name: string
  source_repo: string
  target_url: string
  target_repo: string
  enabled: boolean
  pending: number
  in_progress: number
  succeeded: number
  failed: number
  skipped: number
  /** MAX(completed_at) over 成功任务；'' = 从未成功 */
  last_success_at: string
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
  last_error: string
  created_at: string
  completed_at: string
}

export interface ReplicationStatus {
  targets: ReplicationTargetStatus[]
  events: ReplicationEvent[]
}

/** 轮询周期（Query refetchInterval——复刻 10s） */
export const REPLICATION_POLL_MS = POLL_INTERVALS.replication

/** 目标行运行态：停用 > 失败 > 进行中 > 排队 > 正常 */
function targetState(t: ReplicationTargetStatus): { label: string; dot: string } {
  if (!t.enabled) return { label: tt('已停用'), dot: 'warn' }
  if (t.failed > 0) return { label: tt('异常（{v1} 失败）', { v1: formatCount(t.failed) }), dot: 'err' }
  if (t.in_progress > 0) return { label: tt('复制中'), dot: 'warn' }
  if (t.pending > 0) return { label: tt('排队（{v1}）', { v1: formatCount(t.pending) }), dot: 'warn' }
  return { label: tt('正常'), dot: 'ok' }
}

/** 任务状态 → badge 色（值原样呈现不翻译——排障要比对 API） */
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

/** 全局封锁双开关卡（T-422，FR-138.3 / parity §6A R8）：两方向独立翻转；
 *  封锁只拦复制执行，不拦本面板与配置面。 */
function GlobalBlockCard() {
  const { session } = useAuth()
  const readOnly = isReadOnlyAdmin(session)
  const adminWrite = canAdminWrite(session)
  const [busy, setBusy] = useState<'' | 'push' | 'pull'>('')
  const flags = useQuery({
    queryKey: ['replication', 'block-flags'],
    queryFn: getReplicationGlobalBlock,
  })

  const flip = async (dir: 'push' | 'pull', next: boolean) => {
    setBusy(dir)
    try {
      // 只动一个方向（§9.2-B-1 选择器）：另一方向显式 false = 本次不动
      const msg = await setReplicationBlock(next, dir === 'push', dir === 'pull')
      toast.success(msg)
      void flags.refetch()
    } catch (err) {
      toast.error(tt('封锁开关翻转失败：{v1}', { v1: errText(err) }))
      void flags.refetch() // 行内不乐观更新——失败后回读服务端真值
    } finally {
      setBusy('')
    }
  }

  if (flags.isLoading && !flags.data) return <StateSkeleton lines={2} />
  return (
    <section className="card section" data-testid="repl-global-block">
      <h3>{tt('全局封锁（应急刹车）')}</h3>
      {flags.isError && !flags.data ? (
        <p className="field-hint mb-0">{tt('全局封锁状态读取失败（GET /api/v1/system/replications）。')}</p>
      ) : flags.data ? (
        <>
          <CheckRow
            disabled={!adminWrite || busy !== ''}
            checked={flags.data.blockPushReplications}
            onChange={(next) => void flip('push', next)}
            label={tt('封锁 push 复制（blockPushReplications）')}
            testid="repl-block-push"
          />
          <CheckRow
            disabled={!adminWrite || busy !== ''}
            checked={flags.data.blockPullReplications}
            onChange={(next) => void flip('pull', next)}
            label={tt('封锁 pull 复制（blockPullReplications）——remote 回源/智能拉取零上游流量')}
            testid="repl-block-pull"
          />
          <p className="field-hint mb-0">
            {tt('设定后，无论各复制配置如何，push/pull 复制都不会触发（R8 tooltip 语义）； 已缓存制品照常服务（pull 侧仅停上游接触）。封锁不影响本面板与复制配置的读写。')}
            {readOnly ? tt('（当前会话为只读管理员——开关只读呈现）') : ''}
          </p>
        </>
      ) : null}
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
  /** id → 配置行的 cron 投影；null = 无配置回读——调度列如实降 '—' */
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
          <div className="overflow-x-auto">
            <table className="w-full text-dense" data-testid="repl-targets-table">
              <thead>
                <tr className="border-b border-border text-left text-aux text-muted-foreground">
                  <th scope="col" className="px-3 py-2 font-medium">{tt('状态')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{tt('目标')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">URL</th>
                  <th scope="col" className="px-3 py-2 font-medium">{tt('仓库（源 → 目标）')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{tt('调度')}</th>
                  <th scope="col" className="px-3 py-2 font-medium" lang="en">pending</th>
                  <th scope="col" className="px-3 py-2 font-medium">{tt('进行中')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{tt('失败')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{tt('累计成功')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{tt('上次成功')}</th>
                </tr>
              </thead>
              <tbody>
                {data.targets.map((t, i) => {
                  const st = targetState(t)
                  const cron = cronOf?.get(t.id) ?? null
                  return (
                    <tr key={t.id} data-testid={`repl-target-${i}`} className="border-b border-border/60 hover:bg-accent">
                      <td className="px-3 py-1.5">
                        <span className={`status-dot ${st.dot}`} aria-hidden="true" /> {st.label}
                      </td>
                      <td className="px-3 py-1.5" lang="en">{t.name}</td>
                      <td className="max-w-[240px] break-all px-3 py-1.5 font-mono" lang="en">
                        {t.target_url} <CopyButton value={t.target_url} label={tt('目标 URL {v1}', { v1: t.name })} />
                      </td>
                      <td className="px-3 py-1.5 font-mono" lang="en">
                        {t.source_repo} → {t.target_repo}
                      </td>
                      <td className="px-3 py-1.5" data-testid={`repl-sched-${i}`}>
                        {cron === null ? (
                          <span className="text-muted-foreground">—</span>
                        ) : cron.cron_exp ? (
                          <>
                            <span className="font-mono" lang="en">{cron.cron_exp}</span>
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
                          <span className="text-muted-foreground">{tt('事件驱动')}</span>
                        )}
                      </td>
                      <td className="px-3 py-1.5 font-mono" lang="en">{formatCount(t.pending)}</td>
                      <td className="px-3 py-1.5 font-mono" lang="en">{formatCount(t.in_progress)}</td>
                      <td className={`px-3 py-1.5 font-mono${t.failed > 0 ? ' text-destructive' : ''}`} lang="en">
                        {formatCount(t.failed)}
                      </td>
                      <td className="px-3 py-1.5 font-mono" lang="en">{formatCount(t.succeeded)}</td>
                      <td className="px-3 py-1.5 font-mono" title={t.last_success_at || undefined}>
                        {t.last_success_at ? formatAuditTime(t.last_success_at) : '—'}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
        {staleError && (
          <p className="field-error" data-testid="repl-stale" role="alert">
            {tt('上次刷新失败：')}{staleError}{tt('（每')} {REPLICATION_POLL_MS / 1000} {tt('秒自动重试，以上为最后一次成功数据）')}
          </p>
        )}
        <p className="field-hint mb-0">
          {tt('每')} {REPLICATION_POLL_MS / 1000} {tt('秒自动刷新（GET /api/v1/replication/status）；复制为单向 push——源上传后异步推送，失败按 1s→16s 指数退避重试（最多 6 次后终态）。「调度」列自 配置面（GET /api/v1/replications）join：cron 到点触发全量对账，事件轨照常承载增量（同制品 不双推）；表达式在仓库编辑页 Replications 节配置。')}
        </p>
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
          <div className="overflow-x-auto">
            <table className="w-full text-dense" data-testid="repl-events-table">
              <thead>
                <tr className="border-b border-border text-left text-aux text-muted-foreground">
                  <th scope="col" className="px-3 py-2 font-medium">{tt('时间')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{tt('状态')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{tt('制品')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">sha256</th>
                  <th scope="col" className="px-3 py-2 font-medium">{tt('尝试')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{tt('错误')}</th>
                </tr>
              </thead>
              <tbody>
                {data.events.map((ev, i) => {
                  const repo = repoOf.get(ev.replication_id)
                  const artifact = repo ? `${repo}/${ev.node_path}` : ev.node_path
                  return (
                    <tr key={ev.id} data-testid={`repl-event-${i}`} className="border-b border-border/60 hover:bg-accent">
                      <td className="px-3 py-1.5 font-mono audit-time" title={ev.created_at}>
                        {formatAuditTime(ev.created_at)}
                      </td>
                      <td className="px-3 py-1.5">
                        <Badge variant={(TASK_BADGE[ev.status] ?? 'neutral') as 'success' | 'warning' | 'danger' | 'neutral'} mono lang="en">
                          {ev.status}
                        </Badge>
                      </td>
                      <td className="max-w-[320px] break-all px-3 py-1.5 font-mono" lang="en">
                        {artifact} <CopyButton value={artifact} label={tt('制品路径 {artifact}', { artifact: artifact })} />
                      </td>
                      <td className="px-3 py-1.5 font-mono" lang="en" title={ev.blob_sha256}>
                        {shortSha(ev.blob_sha256)}{' '}
                        <CopyButton value={ev.blob_sha256} label={`sha256 ${ev.blob_sha256}`} />
                      </td>
                      <td className="px-3 py-1.5 font-mono" lang="en">{formatCount(ev.attempts)}</td>
                      <td className="max-w-[320px] break-all px-3 py-1.5 font-mono">
                        {ev.last_error || <span className="text-muted-foreground">—</span>}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
        <p className="field-hint mb-0">{tt('事件为最近的推送尝试（时间倒序，全目标合并）；排队 / 进行中为未决任务，失败行保留最近一次错误原因。')}</p>
      </section>
    </>
  )
}

export default function ReplicationPage() {
  // 状态面：TanStack Query 10s 轮询（失败保留旧数据 = stale 语义）
  const status = useQuery({
    queryKey: ['replication', 'status'],
    queryFn: () => apiJSON<ReplicationStatus>('/v1/replication/status'),
    refetchInterval: REPLICATION_POLL_MS,
  })
  const retry = () => void status.refetch()
  // 配置面：targets 的 cron/next-sync 投影 join 源（读失败 = 调度列降 '—'）
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

  const data = status.data ?? null
  const error = status.error
  const errorStatus = error instanceof ApiError ? error.status : 0
  const loading = status.isPending
  const forbidden = errorStatus === 403
  const unavailable = !forbidden && (errorStatus === 404 || errorStatus === 501)

  return (
    <div data-testid="repl-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{tt('复制')}</h2>
        <span className="text-aux text-2">{tt('单向 push：源仓库 → 目标实例（ADR-0021）——事件轨 + 可选定时全量双轨')}</span>
      </div>

      {loading && <StateSkeleton lines={8} />}
      {forbidden && (
        <EmptyState
          message={tt('无权限查看复制状态')}
          hint={tt('复制面板为管理员视图（GET /api/v1/replication/status 仅 admin）。')}
        />
      )}
      {unavailable && (
        <section className="card section" data-testid="repl-unavailable">
          <h3>{tt('复制')}</h3>
          <p className="field-hint mb-0">
            {errorStatus === 501
              ? tt('本实例未启用复制（replication 配置段缺失，端点返回 501）；在实例配置中启用后本面板自动呈现目标与事件。')
              : tt('本实例的复制状态端点不可用（HTTP 404——服务端尚未桥接 GET /api/v1/replication/status）；桥接后本面板自动呈现目标与事件。')}
          </p>
        </section>
      )}
      {status.isError && !forbidden && !unavailable && !data && (
        <ErrorCard error={new ApiError(0, errText(error))} onRetry={retry} />
      )}
      {/* 全局封锁双开关——独立于状态面的只读/降级态 */}
      {!forbidden && <GlobalBlockCard />}
      {data && !unavailable && (
        <ReplicationBody
          data={data}
          staleError={status.isError ? errText(error) : undefined}
          cronOf={cronOf}
        />
      )}
    </div>
  )
}
