// 存储迁移面板（T-160 进度呈现 + T-177 启动入口——P3 新栈重写：
// TanStack Query refetchInterval 复刻 5s 轮询 + stale 保留）：
// 磁盘 filestore → S3 后台迁移的进度与触发。
// GET /api/v1/storage/migration（T-164，admin 门），每 5s 轮询（AC②）。
// 四态收敛：loading 骨架（仅首帧）/ 403 整面板隐藏（L3）/ 501 未配置 S3
// 迁移降级一句提示 / 其它错误（无数据 ErrorCard；有旧数据保留 + 行内标）。
// 启动（T-177）：POST /api/v1/storage/migration/start，202 + 状态体（幂等）、
// 409 被拒（契约见 architecture.md §7.1）。危险面：prompt 输入 YES。
// 锚族原样：migration-panel/migration-status/migration-bar(-fill)/
// migration-pct/migration-total/migration-migrated/migration-skipped/
// migration-failed/migration-error/migration-started/migration-finished/
// migration-stale/migration-unconfigured/migration-start/
// migration-start-error/migration-confirm-text。
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { useAuth } from '@/app/AuthContext'
import { Button } from '@/components/ui/button'
import { AlertBox } from '@/components/layout/bits'
import { ErrorCard, StateSkeleton } from '@/components/layout/states'
import { useConfirm } from '@/app/providers'
import { toast } from '@/lib/toast'
import { ApiError, apiJSON, errText, isReadOnlyAdmin } from '@/lib/api'
import { POLL_INTERVALS } from '@/lib/query'
import { formatAuditTime, formatCount } from '@/lib/format'
import { tr } from '@/i18n'

const tt = tr('governance')

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

/** 轮询周期（Query refetchInterval——复刻 5s） */
export const MIGRATION_POLL_MS = POLL_INTERVALS.migration

function statusLabel(d: MigrationStatus): { label: string; dot: string } {
  if (d.running) return { label: tt('迁移中'), dot: 'warn' }
  if (d.done) return d.failed > 0 ? { label: tt('已完成（有失败）'), dot: 'err' } : { label: tt('已完成'), dot: 'ok' }
  return { label: tt('未开始'), dot: 'warn' }
}

function MigrationBody({ data: d, staleError }: { data: MigrationStatus; staleError?: string }) {
  // 进度口径（T-177 AC④）：total = 启动盘点时的待迁移数，skipped 不在基数
  // 内；分子 = migrated + failed，完成时自然到达 100%。
  const settled = d.migrated + d.failed
  const pct = d.total > 0 ? Math.min(100, (settled / d.total) * 100) : d.done ? 100 : 0
  const st = statusLabel(d)
  const failedDone = d.done && d.failed > 0

  return (
    <>
      <div className="kv">
        <span className="k">{tt('状态')}</span>
        <span data-testid="migration-status">
          <span className={`status-dot ${st.dot}`} aria-hidden="true" />
          {st.label}
        </span>
      </div>
      <div className="kv">
        <span className="k">{tt('进度')}</span>
        <div className="water-line flex-1">
          <div
            className={`water-bar${failedDone ? ' full' : ''}`}
            role="progressbar"
            aria-label={tt('迁移进度')}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={Math.round(pct)}
            data-testid="migration-bar"
          >
            {/* 宽度取整：0.3*100 这类浮点尾差不能漏进内联样式 */}
            <div className="fill" data-testid="migration-bar-fill" style={{ width: `${Math.round(pct)}%` }} />
          </div>
          <span className="font-mono" data-testid="migration-pct" lang="en">
            {pct.toFixed(1)}%
          </span>
        </div>
      </div>
      <div className="kv">
        <span className="k">{tt('待迁移 blob（盘点时）')}</span>
        <span className="font-mono" data-testid="migration-total" lang="en">{formatCount(d.total)}</span>
      </div>
      <div className="kv">
        <span className="k">{tt('已迁移')}</span>
        <span className="font-mono" data-testid="migration-migrated" lang="en">{formatCount(d.migrated)}</span>
      </div>
      <div className="kv">
        <span className="k">{tt('已跳过（S3 已存在）')}</span>
        <span className="font-mono" data-testid="migration-skipped" lang="en">{formatCount(d.skipped)}</span>
      </div>
      <div className="kv">
        <span className="k">{tt('失败')}</span>
        <span className={`font-mono${d.failed > 0 ? ' text-destructive' : ''}`} data-testid="migration-failed" lang="en">
          {formatCount(d.failed)}
        </span>
      </div>
      {d.error && (
        <div className="kv">
          <span className="k">{tt('错误')}</span>
          <span className="break-all text-right font-mono text-destructive" data-testid="migration-error" lang="en">
            {d.error}
          </span>
        </div>
      )}
      <div className="kv">
        <span className="k">{tt('开始时间')}</span>
        <span className="font-mono" data-testid="migration-started" lang="en">
          {d.started_at ? formatAuditTime(d.started_at) : '—'}
        </span>
      </div>
      <div className="kv">
        <span className="k">{tt('结束时间')}</span>
        <span className="font-mono" data-testid="migration-finished" lang="en">
          {d.finished_at ? formatAuditTime(d.finished_at) : '—'}
        </span>
      </div>
      {staleError && (
        <p className="field-error" data-testid="migration-stale" role="alert">
          {tt('上次刷新失败：')}{staleError}{tt('（每')} {MIGRATION_POLL_MS / 1000} {tt('秒自动重试，以上为最后一次成功数据）')}
        </p>
      )}
      <p className="field-hint mb-0">
        {tt('每')} {MIGRATION_POLL_MS / 1000} {tt('秒自动刷新（GET /api/v1/storage/migration）；进度 =（已迁移 + 失败）/ 待迁移数——已跳过是盘点时 S3 已有的 blob，不在待迁移基数内（幂等重跑不重复拷贝）。')}
      </p>
    </>
  )
}

export default function MigrationPanel() {
  const confirm = useConfirm()
  const queryClient = useQueryClient()
  // readonly_admin：迁移启动是 system:write——按钮禁用 + 只读注记
  const { session } = useAuth()
  const readOnly = isReadOnlyAdmin(session)

  const [starting, setStarting] = useState(false)
  const [startError, setStartError] = useState<{ status: number; message: string } | null>(null)

  const status = useQuery({
    queryKey: ['migration', 'status'],
    queryFn: () => apiJSON<MigrationStatus>('/v1/storage/migration'),
    refetchInterval: MIGRATION_POLL_MS,
  })
  const retry = () => void status.refetch()
  // merge：POST /start 的 202 状态体即刻并入（清掉 stale 行内提示）
  const merge = (data: MigrationStatus) => queryClient.setQueryData(['migration', 'status'], data)

  const error = status.error
  const errorStatus = error instanceof ApiError ? error.status : 0
  // 403：非 admin——面板整体隐藏（L3）
  if (errorStatus === 403) return null

  const data = status.data ?? null
  // 501：实例未配置 S3 迁移
  const unconfigured = errorStatus === 501
  // running 态不渲染启动按钮（幂等返回当前状态，重复启动无意义）
  const canStart = data !== null && !data.running && !unconfigured

  const doStart = async (): Promise<void> => {
    if (!canStart || starting || readOnly) return
    const typed = await confirm.prompt({
      title: tt('启动存储迁移（本地 → S3）'),
      description: (
        <>
          <p>{tt('将触发')}<b>{tt('后台迁移')}</b>{tt('：先盘点（本地盘有、S3 还没有的 blob 构成待迁移集），再逐个流式拷贝到 S3；S3 已有的直接跳过，不重复拷贝（幂等，可安全重跑）。')}</p>
          <ul className="my-2 list-disc pl-5">
            <li><b>{tt('双写期间写入放大')}</b>{tt('：实例处于双写装配，迁移期间每个上传同时落本地盘与 S3——写入流量与 S3 请求约为两份。')}</li>
            <li><b>{tt('中断可恢复')}</b>{tt('：迁移可安全中断或重启后端——重启后重新盘点、只补缺失部分，已拷贝的不会重拷。')}</li>
            <li><b>{tt('完成前不要关闭后端')}</b>{tt('或改动 storage.migration 配置；状态到达「已完成」后由运维置 completed 切换为 S3 单写。')}</li>
            <li><b>{tt('后台执行')}</b>{tt('：启动请求立即返回（202），不阻塞——进度在本面板每 5 秒自动刷新。')}</li>
          </ul>
          <p className="text-aux text-muted-foreground">{tt('输入')} <b className="font-mono" lang="en">YES</b> {tt('以确认。')}</p>
        </>
      ),
      placeholder: 'YES',
      mono: true,
      danger: true,
      confirmLabel: tt('启动迁移'),
      cancelLabel: tt('取消'),
      anchor: 'migration-confirm-text',
      validate: (v) => (v.trim().toUpperCase() === 'YES' ? null : tt('需输入 YES')),
    })
    if (typed === null) return
    setStarting(true)
    setStartError(null)
    try {
      // 202 + 状态体（已在运行中时幂等返回当前状态，同为 202）
      const data = await apiJSON<MigrationStatus>('/v1/storage/migration/start', { method: 'POST' })
      merge(data)
      toast.success(tt('存储迁移已启动——后台执行，进度每 5 秒自动刷新'))
    } catch (err) {
      setStartError({ status: err instanceof ApiError ? err.status : 0, message: errText(err) })
    } finally {
      setStarting(false)
    }
  }

  return (
    <section className="card section" data-testid="migration-panel">
      <h3>{tt('存储迁移（本地 → S3）')}</h3>
      {status.isPending && <StateSkeleton lines={4} />}
      {unconfigured && (
        <p className="field-hint" data-testid="migration-unconfigured" style={{ marginBottom: 0 }}>
          {tt('本实例未配置 S3 迁移（storage.migration 未启用，端点返回 501）——当前仅使用本地 filestore；启用后本面板自动呈现迁移进度。')}
        </p>
      )}
      {status.isError && !unconfigured && !data && (
        <ErrorCard error={new ApiError(0, errText(error))} onRetry={retry} />
      )}
      {data && !unconfigured && <MigrationBody data={data} staleError={status.isError ? errText(error) : undefined} />}
      {canStart && (
        <div className="gc-actions">
          <Button
            variant="outline"
            size="sm"
            className="border-destructive/50 text-destructive hover:bg-destructive/10"
            disabled={starting || readOnly}
            onClick={() => void doStart()}
            data-testid="migration-start"
            title={readOnly ? tt('只读管理员：迁移启动是 system:write（服务端 403 兜底）') : undefined}
          >
            {starting ? tt('启动中…') : tt('启动迁移')}
          </Button>
          {readOnly ? (
            <span className="text-aux text-2">{tt('只读管理员：启动迁移为管理面写操作（system:write），入口已禁用——服务端 403 兜底。')}</span>
          ) : (
            <span className="text-aux text-2">{tt('危险操作——需二次确认（输入 YES）')}</span>
          )}
        </div>
      )}
      {startError && (
        <AlertBox severity="error" className="gc-error" testid="migration-start-error">
          <div className="font-medium">
            <span aria-hidden="true">✗</span>
            {startError.status === 409
              ? tt('迁移启动被拒（HTTP 409）')
              : tt('迁移启动失败（HTTP {v1}）', { v1: startError.status })}
          </div>
          {startError.status === 409 && (
            <div className="text-2">{tt('服务端拒绝了本次启动——实例可能已不在双写装配状态；检查 storage.migration 配置后重试，或直接查看上方状态。')}</div>
          )}
          <pre lang="en" className="font-mono text-aux">{startError.message}</pre>
        </AlertBox>
      )}
    </section>
  )
}
