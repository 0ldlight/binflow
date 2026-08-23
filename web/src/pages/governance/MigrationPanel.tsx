import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { useConfirm } from '../../components/ConfirmDialog'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, apiJSON, errText, isReadOnlyAdmin } from '../../lib/api'
import { formatAuditTime, formatCount } from '../../lib/format'

// 存储迁移面板（T-160 进度呈现 + T-177 启动入口）：
// 磁盘 filestore → S3 后台迁移的进度与触发。
// GET /api/v1/storage/migration（T-164，admin 门），每 5s 轮询（AC②）。
//
// 字段以 internal/storage.MigrationStatusView 实存契约为准：running /
// done / total / migrated / skipped / failed / error / started_at /
// finished_at。票面 AC 的拟名 total_blobs / in_progress / completed 不在
// 契约里（对应 total / running / done）——漂移记入工作日志。
//
// 四态收敛（console-ux §5.3）：
// - loading：骨架（仅首帧；轮询刷新不清空已到达的数据）
// - 403：整面板隐藏（§3.6.3 L3——GC 页在治理组导航本就 admin-only，
//   这里兜底非 admin 直链场景，不渲染无权限卡；启动按钮同理只在 admin
//   可达的数据态渲染）
// - 501：实例未配置 S3 迁移 → 降级为一句提示（不渲染进度条，也不渲染
//   启动按钮——AC①）
// - 其它错误：无数据 → ErrorCard + 重试；有旧数据 → 保留进度条，
//   行内标「上次刷新失败」——观察进行中的迁移时单次掉线不清屏
//
// 启动（T-177）：POST /api/v1/storage/migration/start，202 + 状态体
// （幂等——已在运行中重复调用返回当前状态）、启动被拒 409（契约见
// architecture.md §7.1）。危险面：ConfirmDialog 二次确认（输入 YES，
// §3.5 前置缝——沿 GC apply 同款）+ 影响说明；202 状态体即刻并入呈现
// （不等下一轮轮询）。迁移本身在服务端后台执行，双写装配期间中断可
// 恢复（重启后重新盘点、只补缺，internal/storage/migration.go 语义）。

/** GET /api/v1/storage/migration 响应体（internal/storage.MigrationStatusView） */
export interface MigrationStatus {
  running: boolean
  done: boolean
  total: number
  migrated: number
  skipped: number
  failed: number
  error?: string
  started_at?: string
  finished_at?: string
}

/** 轮询周期：AC② 每 5s 自动刷新 */
export const MIGRATION_POLL_MS = 5000

type Phase =
  | { kind: 'loading' }
  | { kind: 'ok'; data: MigrationStatus }
  /** 403：非 admin——面板整体隐藏 */
  | { kind: 'hidden' }
  /** 501：实例未配置 S3 迁移 */
  | { kind: 'unconfigured' }
  /** 其余错误；stale = 最后一次成功数据（有则保留呈现） */
  | { kind: 'error'; message: string; stale: MigrationStatus | null }

function fetchMigration(): Promise<MigrationStatus> {
  return apiJSON<MigrationStatus>('/v1/storage/migration')
}

/** 迁移状态轮询：挂载即取一次，此后每 POLL_MS 刷一次；tick 驱动手动重试；
 * merge 把 POST /start 的 202 状态体即刻并入（清掉 stale 行内提示） */
function useMigrationStatus(): {
  phase: Phase
  retry: () => void
  merge: (data: MigrationStatus) => void
} {
  const [phase, setPhase] = useState<Phase>({ kind: 'loading' })
  const [tick, setTick] = useState(0)

  useEffect(() => {
    let alive = true
    const poll = () => {
      fetchMigration()
        .then((data) => {
          if (alive) setPhase({ kind: 'ok', data })
        })
        .catch((err: unknown) => {
          if (!alive) return
          const status = err instanceof ApiError ? err.status : 0
          if (status === 403) {
            setPhase({ kind: 'hidden' })
            return
          }
          if (status === 501) {
            setPhase({ kind: 'unconfigured' })
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
    const timer = setInterval(() => void poll(), MIGRATION_POLL_MS)
    return () => {
      alive = false
      clearInterval(timer)
    }
  }, [tick])

  return {
    phase,
    retry: () => setTick((t) => t + 1),
    merge: (data) => setPhase({ kind: 'ok', data }),
  }
}

function statusLabel(d: MigrationStatus): { label: string; dot: string } {
  if (d.running) return { label: '迁移中', dot: 'warn' }
  if (d.done) return d.failed > 0 ? { label: '已完成（有失败）', dot: 'err' } : { label: '已完成', dot: 'ok' }
  return { label: '未开始', dot: 'warn' }
}

function MigrationBody({ data: d, staleError }: { data: MigrationStatus; staleError?: string }) {
  // 进度口径（T-177 AC④，修 T-176 遗留③）：total = 启动盘点时的待迁移数
  // （本地盘有、S3 还没有的 blob），skipped 是盘点时 S3 已有的——不在
  // total 基数内，计入分子会提前触顶。分子 = migrated + failed（待迁移集
  // 的每个 blob 终态非成功即失败），完成时自然到达 100%。
  const settled = d.migrated + d.failed
  const pct = d.total > 0 ? Math.min(100, (settled / d.total) * 100) : d.done ? 100 : 0
  const st = statusLabel(d)
  const failedDone = d.done && d.failed > 0

  return (
    <>
      <div className="kv">
        <span className="k">状态</span>
        <span data-testid="migration-status">
          <span className={`status-dot ${st.dot}`} aria-hidden="true" />
          {st.label}
        </span>
      </div>
      <div className="kv">
        <span className="k">进度</span>
        <div className="water-line" style={{ flex: 1 }}>
          <div
            className={`water-bar${failedDone ? ' full' : ''}`}
            role="progressbar"
            aria-label="迁移进度"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={Math.round(pct)}
            data-testid="migration-bar"
          >
            {/* 宽度取整：0.3*100 这类浮点尾差不能漏进内联样式 */}
            <div className="fill" data-testid="migration-bar-fill" style={{ width: `${Math.round(pct)}%` }} />
          </div>
          <span className="mono" data-testid="migration-pct" lang="en">
            {pct.toFixed(1)}%
          </span>
        </div>
      </div>
      <div className="kv">
        <span className="k">待迁移 blob（盘点时）</span>
        <span className="mono" data-testid="migration-total" lang="en">
          {formatCount(d.total)}
        </span>
      </div>
      <div className="kv">
        <span className="k">已迁移</span>
        <span className="mono" data-testid="migration-migrated" lang="en">
          {formatCount(d.migrated)}
        </span>
      </div>
      <div className="kv">
        <span className="k">已跳过（S3 已存在）</span>
        <span className="mono" data-testid="migration-skipped" lang="en">
          {formatCount(d.skipped)}
        </span>
      </div>
      <div className="kv">
        <span className="k">失败</span>
        <span
          className="mono"
          data-testid="migration-failed"
          lang="en"
          style={d.failed > 0 ? { color: 'var(--bf-danger)' } : undefined}
        >
          {formatCount(d.failed)}
        </span>
      </div>
      {d.error && (
        <div className="kv">
          <span className="k">错误</span>
          <span
            className="mono"
            data-testid="migration-error"
            lang="en"
            style={{ color: 'var(--bf-danger)', textAlign: 'right', wordBreak: 'break-all' }}
          >
            {d.error}
          </span>
        </div>
      )}
      <div className="kv">
        <span className="k">开始时间</span>
        <span className="mono" data-testid="migration-started" lang="en">
          {d.started_at ? formatAuditTime(d.started_at) : '—'}
        </span>
      </div>
      <div className="kv">
        <span className="k">结束时间</span>
        <span className="mono" data-testid="migration-finished" lang="en">
          {d.finished_at ? formatAuditTime(d.finished_at) : '—'}
        </span>
      </div>
      {staleError && (
        <p className="field-error" data-testid="migration-stale" role="alert">
          上次刷新失败：{staleError}（每 {MIGRATION_POLL_MS / 1000} 秒自动重试，以上为最后一次成功数据）
        </p>
      )}
      <p className="field-hint" style={{ marginBottom: 0 }}>
        每 {MIGRATION_POLL_MS / 1000} 秒自动刷新（GET /api/v1/storage/migration）；进度 =（已迁移 + 失败）/
        待迁移数——已跳过是盘点时 S3 已有的 blob，不在待迁移基数内（幂等重跑不重复拷贝）。
      </p>
    </>
  )
}

export default function MigrationPanel() {
  const { phase, retry, merge } = useMigrationStatus()
  const confirm = useConfirm()
  const toast = useToast()
  // readonly_admin（T-218 治理域 UI 债收口，M7 §7.3）：迁移启动是
  // system:write——按钮禁用 + 只读注记；GET 状态面（system:read）照常
  const { session } = useAuth()
  const readOnly = isReadOnlyAdmin(session)

  const [starting, setStarting] = useState(false)
  const [startError, setStartError] = useState<{ status: number; message: string } | null>(null)

  if (phase.kind === 'hidden') return null

  const okData = phase.kind === 'ok' ? phase.data : phase.kind === 'error' ? phase.stale : null
  // running 态不渲染启动按钮（POST 幂等返回当前状态，重复启动无意义——
  // 状态行已在呈现「迁移中」）；未开始 / 已完成均可（重跑只补新缺）
  const canStart = okData !== null && !okData.running

  const doStart = async (): Promise<void> => {
    if (!canStart || starting || readOnly) return
    // confirmDisabled 是事件驱动重求值的闭包缝（T-99）：输入值收在 holder
    const holder = { typed: '' }
    const body: ReactNode = (
      <>
        <p>
          将触发<b>后台迁移</b>：先盘点（本地盘有、S3 还没有的 blob 构成待迁移集），再逐个流式拷贝到
          S3；S3 已有的直接跳过，不重复拷贝（幂等，可安全重跑）。
        </p>
        <ul style={{ margin: '8px 0', paddingLeft: 20 }}>
          <li>
            <b>双写期间写入放大</b>：实例处于双写装配，迁移期间每个上传同时落本地盘与
            S3——写入流量与 S3 请求约为两份。
          </li>
          <li>
            <b>中断可恢复</b>：迁移可安全中断或重启后端——重启后重新盘点、只补缺失部分，已拷贝的不会重拷。
          </li>
          <li>
            <b>完成前不要关闭后端</b>或改动 storage.migration 配置；状态到达「已完成」后由运维置
            completed 切换为 S3 单写。
          </li>
          <li>
            <b>后台执行</b>：启动请求立即返回（202），不阻塞——进度在本面板每 5 秒自动刷新。
          </li>
        </ul>
        <div className="field" style={{ maxWidth: 'none', marginBottom: 0, marginTop: 12 }}>
          <label htmlFor="migration-confirm">
            输入 <b className="mono" lang="en">YES</b> 以确认：
          </label>
          <input
            id="migration-confirm"
            className="confirm-input"
            autoComplete="off"
            onChange={(e) => {
              holder.typed = e.target.value
            }}
            data-testid="migration-confirm-text"
            lang="en"
          />
        </div>
      </>
    )
    const ok = await confirm({
      title: '启动存储迁移（本地 → S3）',
      body,
      danger: true,
      confirmLabel: '启动迁移',
      confirmDisabled: () => holder.typed.trim().toUpperCase() !== 'YES',
    })
    if (!ok) return
    setStarting(true)
    setStartError(null)
    try {
      // 202 + 状态体（已在运行中时幂等返回当前状态，同为 202）
      const data = await apiJSON<MigrationStatus>('/v1/storage/migration/start', { method: 'POST' })
      merge(data)
      toast.success('存储迁移已启动——后台执行，进度每 5 秒自动刷新')
    } catch (err) {
      setStartError({ status: err instanceof ApiError ? err.status : 0, message: errText(err) })
    } finally {
      setStarting(false)
    }
  }

  return (
    <section className="card section" data-testid="migration-panel">
      <h3>存储迁移（本地 → S3）</h3>
      {phase.kind === 'loading' && <Skeleton lines={4} />}
      {phase.kind === 'unconfigured' && (
        <p className="field-hint" data-testid="migration-unconfigured" style={{ marginBottom: 0 }}>
          本实例未配置 S3 迁移（storage.migration 未启用，端点返回 501）——当前仅使用本地
          filestore；启用后本面板自动呈现迁移进度。
        </p>
      )}
      {phase.kind === 'error' && phase.stale === null && (
        <ErrorCard error={new ApiError(0, phase.message)} onRetry={retry} />
      )}
      {okData && <MigrationBody data={okData} staleError={phase.kind === 'error' ? phase.message : undefined} />}
      {canStart && (
        <div className="gc-actions">
          <button
            type="button"
            className="btn danger"
            disabled={starting || readOnly}
            onClick={() => void doStart()}
            data-testid="migration-start"
            title={readOnly ? '只读管理员：迁移启动是 system:write（服务端 403 兜底）' : undefined}
          >
            {starting ? '启动中…' : '启动迁移'}
          </button>
          {readOnly ? (
            <span className="text-2" data-testid="migration-readonly-note" style={{ fontSize: 'var(--bf-fs-aux)' }}>
              只读管理员：启动迁移为管理面写操作（system:write），入口已禁用——服务端 403 兜底。
            </span>
          ) : (
            <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>
              危险操作——需二次确认（输入 YES）
            </span>
          )}
        </div>
      )}
      {startError && (
        <div className="gc-error" role="alert" data-testid="migration-start-error">
          <div className="headline">
            <span aria-hidden="true">✗</span>
            {startError.status === 409
              ? '迁移启动被拒（HTTP 409）'
              : `迁移启动失败（HTTP ${startError.status}）`}
          </div>
          {startError.status === 409 && (
            <div className="text-2">
              服务端拒绝了本次启动——实例可能已不在双写装配状态；检查 storage.migration
              配置后重试，或直接查看上方状态。
            </div>
          )}
          <pre lang="en">{startError.message}</pre>
        </div>
      )}
    </section>
  )
}
