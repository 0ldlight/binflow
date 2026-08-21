import { useEffect, useState } from 'react'

import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, apiJSON, errText } from '../../lib/api'
import { formatAuditTime, formatCount } from '../../lib/format'

// 复制面板（T-159）：push 复制状态 + 事件列表（治理组「复制」页）。
// GET /api/v1/replication/status（admin），每 10s 轮询（AC②）。
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
  if (!t.enabled) return { label: '已停用', dot: 'warn' }
  if (t.failed > 0) return { label: `异常（${formatCount(t.failed)} 失败）`, dot: 'err' }
  if (t.in_progress > 0) return { label: '复制中', dot: 'warn' }
  if (t.pending > 0) return { label: `排队（${formatCount(t.pending)}）`, dot: 'warn' }
  return { label: '正常', dot: 'ok' }
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

function ReplicationBody({ data, staleError }: { data: ReplicationStatus; staleError?: string }) {
  // replication_id → 源仓库（事件行只带 id，制品路径补全用）
  const repoOf = new Map(data.targets.map((t) => [t.id, t.source_repo]))

  return (
    <>
      <section className="card section" data-testid="repl-targets">
        <h3>复制目标</h3>
        {data.targets.length === 0 ? (
          <EmptyState
            message="未配置复制目标"
            hint="复制为单向 push（源仓库 → 目标实例仓库）；目标配置经 REST /api/v1/replications 或实例配置创建（M6 桥接后可用）。"
            testid="repl-empty-targets"
          />
        ) : (
          <table className="table" data-testid="repl-targets-table">
            <thead>
              <tr>
                <th scope="col">状态</th>
                <th scope="col">目标</th>
                <th scope="col">URL</th>
                <th scope="col">仓库（源 → 目标）</th>
                <th scope="col" lang="en">
                  pending
                </th>
                <th scope="col">进行中</th>
                <th scope="col">失败</th>
                <th scope="col">累计成功</th>
                <th scope="col">上次成功</th>
              </tr>
            </thead>
            <tbody>
              {data.targets.map((t, i) => {
                const st = targetState(t)
                return (
                  <tr key={t.id} data-testid={`repl-target-${i}`}>
                    <td>
                      <span className={`status-dot ${st.dot}`} aria-hidden="true" /> {st.label}
                    </td>
                    <td lang="en">{t.name}</td>
                    <td className="mono wrap" style={{ maxWidth: 240 }} lang="en">
                      {t.target_url} <CopyButton value={t.target_url} label={`目标 URL ${t.name}`} />
                    </td>
                    <td className="mono" lang="en">
                      {t.source_repo} → {t.target_repo}
                    </td>
                    <td className="mono" lang="en">
                      {formatCount(t.pending)}
                    </td>
                    <td className="mono" lang="en">
                      {formatCount(t.in_progress)}
                    </td>
                    <td
                      className="mono"
                      lang="en"
                      style={t.failed > 0 ? { color: 'var(--bf-danger)' } : undefined}
                    >
                      {formatCount(t.failed)}
                    </td>
                    <td className="mono" lang="en">
                      {formatCount(t.succeeded)}
                    </td>
                    <td className="mono" title={t.last_success_at || undefined}>
                      {t.last_success_at ? formatAuditTime(t.last_success_at) : '—'}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
        {staleError && (
          <p className="field-error" data-testid="repl-stale" role="alert">
            上次刷新失败：{staleError}（每 {REPLICATION_POLL_MS / 1000} 秒自动重试，以上为最后一次成功数据）
          </p>
        )}
        <p className="field-hint" style={{ marginBottom: 0 }}>
          每 {REPLICATION_POLL_MS / 1000} 秒自动刷新（GET /api/v1/replication/status）；复制为单向
          push——源上传后异步推送，失败按 1s→16s 指数退避重试（最多 6 次后终态）。
        </p>
      </section>

      <section className="card section" data-testid="repl-events">
        <h3>最近事件</h3>
        {data.events.length === 0 ? (
          <EmptyState
            message="暂无复制事件"
            hint="源仓库有新上传且存在启用的复制目标时，推送事件会出现在这里（时间倒序）。"
            testid="repl-empty-events"
          />
        ) : (
          <table className="table" data-testid="repl-events-table">
            <thead>
              <tr>
                <th scope="col">时间</th>
                <th scope="col">状态</th>
                <th scope="col">制品</th>
                <th scope="col">sha256</th>
                <th scope="col">尝试</th>
                <th scope="col">错误</th>
              </tr>
            </thead>
            <tbody>
              {data.events.map((ev, i) => {
                const repo = repoOf.get(ev.replication_id)
                const artifact = repo ? `${repo}/${ev.node_path}` : ev.node_path
                return (
                  <tr key={ev.id} data-testid={`repl-event-${i}`}>
                    <td className="mono audit-time" title={ev.created_at}>
                      {formatAuditTime(ev.created_at)}
                    </td>
                    <td>
                      <span className={`badge ${TASK_BADGE[ev.status] ?? 'neutral'}`} lang="en">
                        {ev.status}
                      </span>
                    </td>
                    <td className="mono wrap" style={{ maxWidth: 320 }} lang="en">
                      {artifact} <CopyButton value={artifact} label={`制品路径 ${artifact}`} />
                    </td>
                    <td className="mono" lang="en" title={ev.blob_sha256}>
                      {shortSha(ev.blob_sha256)}{' '}
                      <CopyButton value={ev.blob_sha256} label={`sha256 ${ev.blob_sha256}`} />
                    </td>
                    <td className="mono" lang="en">
                      {formatCount(ev.attempts)}
                    </td>
                    <td className="mono wrap" style={{ maxWidth: 320 }}>
                      {ev.last_error || <span className="text-muted">—</span>}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
        <p className="field-hint" style={{ marginBottom: 0 }}>
          事件为最近的推送尝试（时间倒序，全目标合并）；排队 / 进行中为未决任务，失败行保留最近一次错误原因。
        </p>
      </section>
    </>
  )
}

export default function ReplicationPage() {
  const { phase, retry } = useReplicationStatus()

  const okData = phase.kind === 'ok' ? phase.data : phase.kind === 'error' ? phase.stale : null

  return (
    <div data-testid="repl-page">
      <div className="page-header">
        <h2>复制</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>
          单向 push：源仓库 → 目标实例（ADR-0021）
        </span>
      </div>

      {phase.kind === 'loading' && <Skeleton lines={8} />}
      {phase.kind === 'forbidden' && (
        <EmptyState
          message="无权限查看复制状态"
          hint="复制面板为管理员视图（GET /api/v1/replication/status 仅 admin）。"
        />
      )}
      {phase.kind === 'unavailable' && (
        <section className="card section" data-testid="repl-unavailable">
          <h3>复制</h3>
          <p className="field-hint" style={{ marginBottom: 0 }}>
            {phase.reason === 501
              ? '本实例未启用复制（replication 配置段缺失，端点返回 501）；在实例配置中启用后本面板自动呈现目标与事件。'
              : '本实例的复制状态端点不可用（HTTP 404——服务端尚未桥接 GET /api/v1/replication/status）；桥接后本面板自动呈现目标与事件。'}
          </p>
        </section>
      )}
      {phase.kind === 'error' && phase.stale === null && (
        <ErrorCard error={new ApiError(0, phase.message)} onRetry={retry} />
      )}
      {okData && <ReplicationBody data={okData} staleError={phase.kind === 'error' ? phase.message : undefined} />}
    </div>
  )
}
