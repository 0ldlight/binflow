import { useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { useConfirm } from '../../components/ConfirmDialog'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, canAdminWrite, errText, getStorageStats, isReadOnlyAdmin } from '../../lib/api'
import { dedupRatio, formatBytes, formatCount } from '../../lib/format'
import {
  GC_MAX_GRACE_HOURS,
  MAINTENANCE_SLOTS,
  getMaintenance,
  putMaintenance,
  runCleanupNow,
  runGC,
} from '../../lib/governance'
import type { GCRunResult, MaintenanceSlot, MaintenanceSlotKey } from '../../lib/governance'
import { useAsync } from '../../lib/useAsync'
import MigrationPanel from './MigrationPanel'
import { tr } from '../../i18n'

const tt = tr('governance')

// 维护（GC）（console-m8 §6.14 归位 /admin/governance/gc；T-102 语义原样；
// T-459 迁址 /admin/monitoring/gc——服务节点组挂靠）：
// - 定时维护卡（T-462 / FR-145.7）：三 cron 槽（gc / cleanup 两族）+
//   Cleanup Run Now——GET/PUT /api/v1/system/maintenance +
//   POST /api/v1/system/cleanup（细节见 MaintenanceCronCard 头注）。
// - 概况卡与仪表盘同源（GET /api/v1/storage/stats，system:read——
//   readonly_admin 读面全通）。
// - dry-run 是默认姿态（ADR-0015 勘误①）：POST {} 即试运行；apply 必须
//   在「看过一次当前参数下的 dry-run」之后才可用——grace 变更后视为
//   过期，须重新试运行（UI 门控，服务端不强制）。
// - apply 二次确认（P5 危险区）：输入 YES（§4.11「输入仓库实例名或
//   YES 确认」的后一分支——产品无实例名概念，取字面量），走 T-99 沉淀
//   的 confirmDisabled 缝。
// - 409 互斥：data 目录维护锁被 export / 另一次 gc 持有——服务端
//   message 含 holder 诊断（pid/op），红面板原样呈现 + mono。
// - 无候选 = 绿色空态（好消息，§5.3）。
// - 上次运行历史：GET 状态端点是 P2 债务（R4）——经审计页 gc.run 查询。
// - 非 admin：stats 403 → 单张无权限卡（§3.6.3 L2）；写入口不渲染（L4）。
// - readonly_admin（M7 §7.3 / T-238 收口）：GC 全路由（含 dry-run）是
//   system:write——试运行与执行按钮禁用 + 只读注记；服务端 403 兜底。

/** grace 输入解析：'' → null（实例缺省）；纯数字 → number；其余非法 */
function parseGrace(v: string): number | null | 'invalid' {
  const t = v.trim()
  if (t === '') return null
  if (!/^\d+$/.test(t)) return 'invalid'
  const n = Number(t)
  if (n > GC_MAX_GRACE_HOURS) return 'invalid'
  return n
}

function graceLabel(hours: number | null): string {
  if (hours === null) return tt('实例配置缺省（storage.gc_grace_hours）')
  if (hours === 0) return tt('无宽限窗口（0——只回收已确认孤儿）')
  return tt('{hours} 小时', { hours: hours })
}

/** RFC3339 UTC 串 → 人类可读（秒精度——next-run 排障要精确到秒；
 *  与 ServiceStatusPage 同形，页面级小函数不抽公共层） */
function fmtUTC(v: string): string {
  return v ? v.replace('T', ' ').replace(/(\.\d+)?Z$/, ' UTC') : '—'
}

/** 三槽中文语境标签（wire key 原样进 PUT——标签只作呈现） */
const SLOT_LABEL: Record<MaintenanceSlotKey, string> = {
  gc: tt('垃圾回收（Garbage Collection）'),
  'cleanup-unused-cache': tt('清理未使用缓存（Cleanup Unused Cached Artifacts）'),
  'cleanup-virtual': tt('清理虚拟仓（Cleanup Virtual Repositories）'),
}

/** 定时维护卡（T-462 / FR-145.7——维护面 cron 三槽消费）：7.161 维护页
 *  三区块（GC / Cleanup 两族——各带 Cron Expression + Next Run Time +
 *  Run Now）的 BinFlow 承载，GET/PUT /api/v1/system/maintenance。
 *
 * - 每槽一行：表达式输入（Quartz 六/七域）+ 保存 / 清除 + 下次 / 上次
 *  运行。空表达式保存 = 拒（用「清除」取消调度——单态：无行 = 不调度）。
 * - 「Run Now」= 既有手动面（ADR-0044 决策 7①「并存维持」）：GC 槽的
 *  手动执行就是本页危险区的 dry-run/apply（锚点滚动，不另设入口）；
 *  两 cleanup 槽 = POST /api/v1/system/cleanup {apply:true}（T-324 手动
 *  面，Trigger=manual 与调度 fire 同载体）。BinFlow 两 cleanup 槽 fire
 *  时同走 CleanupEngine 全量 pass（无 virtual-only 载体——ADR-0044 决策
 *  8① 的 C 层差异，行内如实注记）。
 * - Quota 百分比 / Compress 内部库 / Prune 无 BinFlow 后端载体——缺位
 *  不伪造（gc-cron-gap 一句注记，7.161 §3.9 区块 2/5 的对应面）。
 * - 门：GET system:read（readonly_admin 可读）；PUT/Run Now system:write
 *  ——readonly_admin 输入与按钮禁用 + 注记，服务端 403 兜底。
 * - 400 族（Invalid cronExp …）按行内错误呈现（服务端点名原因原样）。 */
function MaintenanceCronCard({
  readOnly,
  adminWrite,
  onGotoDangerZone,
}: {
  readOnly: boolean
  adminWrite: boolean
  onGotoDangerZone: () => void
}) {
  const toast = useToast()
  const confirm = useConfirm()
  const view = useAsync(getMaintenance, [])
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [savingKey, setSavingKey] = useState<MaintenanceSlotKey | ''>('')
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({})

  const slotOf = (key: string): MaintenanceSlot | undefined =>
    view.data?.slots.find((s) => s.key === key)

  const draftFor = (s: MaintenanceSlot): string => drafts[s.key] ?? s.cronExp

  const setDraft = (key: string, v: string) => setDrafts((d) => ({ ...d, [key]: v }))

  const saveSlot = async (key: MaintenanceSlotKey): Promise<void> => {
    const expr = (drafts[key] ?? slotOf(key)?.cronExp ?? '').trim()
    if (expr === '') return
    setSavingKey(key)
    setRowErrors((e) => ({ ...e, [key]: '' }))
    try {
      await putMaintenance({ [key]: { cronExp: expr } })
      toast.success(tt('已保存定时任务（{v1}）', { v1: SLOT_LABEL[key] }))
      setDrafts((d) => {
        const next = { ...d }
        delete next[key]
        return next
      })
      view.reload()
    } catch (err) {
      // 400 = Invalid cronExp …（服务端点名原因）；403/503/5xx 原样行内
      setRowErrors((e) => ({ ...e, [key]: errText(err) }))
    } finally {
      setSavingKey('')
    }
  }

  const clearSlot = async (key: MaintenanceSlotKey): Promise<void> => {
    setSavingKey(key)
    setRowErrors((e) => ({ ...e, [key]: '' }))
    try {
      await putMaintenance({ [key]: { cronExp: '' } })
      toast.success(tt('已清除定时任务（{v1}）——不再调度', { v1: SLOT_LABEL[key] }))
      setDrafts((d) => {
        const next = { ...d }
        delete next[key]
        return next
      })
      view.reload()
    } catch (err) {
      setRowErrors((e) => ({ ...e, [key]: errText(err) }))
    } finally {
      setSavingKey('')
    }
  }

  const runCleanupSlot = async (key: MaintenanceSlotKey): Promise<void> => {
    const ok = await confirm({
      title: tt('立即清理（{v1}）', { v1: SLOT_LABEL[key] }),
      body: (
        <>
          <p>{tt('对全实例执行一次清理全量 pass（')}<span className="mono" lang="en">POST /api/v1/system/cleanup</span>{tt('，')}            <span className="mono" lang="en">apply=true</span>{tt('）：按各 remote 仓的未使用策略回收过期缓存、 清扫过期上传会话、并以引擎 grace 窗口执行 GC 腿。与 GC / export 共用 data 目录维护锁， 运行中被其它维护操作拒绝（409）。')}          </p>
          <p className="field-hint" style={{ marginBottom: 0 }}>{tt('两族清理槽在 BinFlow 同走一个全量 pass（无 virtual-only 载体——本按钮与另一槽等价； 差异登记见 parity 册）。')}          </p>
        </>
      ),
      danger: true,
      confirmLabel: tt('立即清理'),
    })
    if (!ok) return
    setSavingKey(key)
    setRowErrors((e) => ({ ...e, [key]: '' }))
    try {
      const rep = await runCleanupNow()
      if (rep.ok) {
        toast.success(
          tt('清理完成：回收 {v1} 项 / {v2}', { v1: formatCount(rep.objectsCleaned), v2: formatBytes(rep.bytesReclaimed) }) +
            tt('（grace 内暂缓 {v1} 项）', { v1: formatCount(rep.gracePending) }),
        )
      } else {
        toast.error(tt('清理未完成：{v1}', { v1: rep.error || tt('引擎未报告原因') }))
      }
      // 手动面不写台账行（lastRun 仍属调度 fire）——只刷新表达式列
      view.reload()
    } catch (err) {
      setRowErrors((e) => ({ ...e, [key]: errText(err) }))
    } finally {
      setSavingKey('')
    }
  }

  return (
    <section className="card section" data-testid="gc-cron">
      <Typography variant="subtitle2" component="h3" sx={{ mb: 0.5 }}>{tt('定时维护（cron）')}      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>{tt('三类维护作业的定时表达式（Quartz 六/七域，如')} <span className="mono" lang="en">0 0 /4 * * ?</span>{tt('）。 到点由服务端调度器执行全量 pass；手动执行与定时并存（下方危险区 / 各行「立即清理」）。')}      </Typography>

      {view.status === 'loading' && <Skeleton lines={4} />}
      {view.status === 'error' && view.error && <ErrorCard error={view.error} onRetry={view.reload} />}
      {/* 403 不设独立降级注记：本卡仅对 admin/readonly_admin 渲染（两者
          GET system:read 均通）——普通 user 的页面级 L2 收敛由上方 stats
          无权限卡承载，非 admin 不入本面（§3.6.3） */}

      {view.status === 'ok' && view.data && (
        <Table size="small" data-testid="gc-cron-table">
          <TableHead>
            <TableRow>
              <TableCell component="th" scope="col">{tt('作业')}</TableCell>
              <TableCell component="th" scope="col">{tt('cron 表达式')}</TableCell>
              <TableCell component="th" scope="col">{tt('下次运行')}</TableCell>
              <TableCell component="th" scope="col">{tt('上次运行 / 结果')}</TableCell>
              <TableCell component="th" scope="col" align="right">{tt('操作')}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {MAINTENANCE_SLOTS.map((key) => {
              const slot = slotOf(key)
              if (!slot) return null
              const draft = draftFor(slot)
              const scheduled = slot.cronExp !== ''
              const dirty = draft !== slot.cronExp
              const err = rowErrors[key]
              return (
                <TableRow key={key} data-testid={`gc-cron-row-${key}`} hover>
                  <TableCell>{SLOT_LABEL[key]}</TableCell>
                  <TableCell>
                    <TextField
                      size="small"
                      value={draft}
                      disabled={!adminWrite || savingKey !== ''}
                      onChange={(e) => setDraft(key, e.target.value)}
                      placeholder="0 0 /4 * * ?"
                      sx={{ width: 190 }}
                      slotProps={{ htmlInput: { className: 'mono', 'data-testid': `gc-cron-input-${key}`, lang: 'en' } }}
                    />
                    {err && (
                      <p className="field-error" role="alert" style={{ maxWidth: 340, whiteSpace: 'normal' }}>
                        {err}
                      </p>
                    )}
                    {!scheduled && !dirty && <span className="text-muted">{tt('未调度')}</span>}
                  </TableCell>
                  <TableCell>
                    {scheduled ? (
                      slot.enabled ? (
                        <span className="mono" lang="en" title={slot.nextRun} data-testid={`gc-cron-next-${key}`}>
                          {fmtUTC(slot.nextRun)}
                        </span>
                      ) : (
                        <Chip size="small" className="badge neutral" label={tt('已停用')} />
                      )
                    ) : (
                      <span className="text-muted">—</span>
                    )}
                  </TableCell>
                  <TableCell>
                    {slot.lastRun ? (
                      <span
                        className="mono"
                        lang="en"
                        title={slot.lastError || undefined}
                        data-testid={`gc-cron-last-${key}`}
                      >
                        {fmtUTC(slot.lastRun)}
                        {slot.lastStatus ? tt('（{v1}{v2}）', { v1: slot.lastStatus, v2: slot.lastStatus !== 'ok' && slot.lastError ? tt('：{v1}', { v1: slot.lastError }) : '' }) : ''}
                      </span>
                    ) : (
                      <span className="text-muted">{tt('未运行')}</span>
                    )}
                  </TableCell>
                  <TableCell align="right" sx={{ whiteSpace: 'nowrap' }}>
                    {key === 'gc' ? (
                      <Button
                        variant="outlined"
                        size="small"
                        onClick={onGotoDangerZone}
                        data-testid="gc-cron-run-gc"
                        title={tt('GC 手动执行走本页危险区的 dry-run / apply（并存维持）')}
                      >{tt('手动执行 ↓')}                      </Button>
                    ) : (
                      <Button
                        variant="outlined"
                        size="small"
                        disabled={!adminWrite || savingKey !== ''}
                        onClick={() => void runCleanupSlot(key)}
                        data-testid={`gc-cron-run-${key}`}
                        title={readOnly ? tt('只读管理员：手动清理是 system:write（服务端 403 兜底）') : undefined}
                      >{tt('立即清理')}                      </Button>
                    )}{' '}
                    <Button
                      variant="outlined"
                      size="small"
                      disabled={!adminWrite || savingKey !== '' || draft.trim() === '' || !dirty}
                      onClick={() => void saveSlot(key)}
                      data-testid={`gc-cron-save-${key}`}
                      title={readOnly ? tt('只读管理员：定时配置是 system:write（服务端 403 兜底）') : undefined}
                    >{tt('保存')}                    </Button>{' '}
                    <Button
                      variant="text"
                      color="inherit"
                      size="small"
                      disabled={!adminWrite || savingKey !== '' || !scheduled}
                      onClick={() => void clearSlot(key)}
                      data-testid={`gc-cron-clear-${key}`}
                      title={tt('清空表达式 = 取消调度（单态：无行 = 不调度）')}
                    >{tt('清除')}                    </Button>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      )}

      {readOnly && (
        <p className="admin-note" data-testid="gc-cron-readonly-note">{tt('只读管理员（readonly_admin）：定时维护配置与手动清理均为 system:write，入口已禁用—— 直接提交会被服务端 403 拒绝。')}        </p>
      )}
      <p className="field-hint" data-testid="gc-cron-gap" style={{ marginBottom: 0 }}>{tt('7.161 维护页的 Quota 百分比、Compress 内部库、Prune 未引用数据三项在 BinFlow 无后端载体—— 如实缺位不呈现（ quota 阈值走存储配额页）。全量调度台账见')}{' '}
        <Link to="/admin/monitoring/status">{tt('服务状态')}</Link> {tt('页（只读投影）。')}      </p>
    </section>
  )
}

export default function GCPage() {
  const { session } = useAuth()
  // 写面判定（M7 §7.3）：admin 布尔是角色镜像，readonly_admin 为 false——
  // 只读态单列；普通 user 不入本面（stats 403 → L2 收敛在前）
  const readOnly = isReadOnlyAdmin(session)
  const adminWrite = canAdminWrite(session)
  const toast = useToast()
  const confirm = useConfirm()
  const navigate = useNavigate()

  const stats = useAsync(getStorageStats, [])
  // 危险区锚点：定时卡「手动执行 ↓」的滚动目标（GC 的 Run Now 就是危险区）
  const dangerRef = useRef<HTMLDivElement | null>(null)

  const [graceInput, setGraceInput] = useState('')
  const grace = parseGrace(graceInput)

  const [running, setRunning] = useState<'' | 'dry' | 'apply'>('')
  const [dryRun, setDryRun] = useState<GCRunResult | null>(null)
  /** dry-run 时的 grace 参数——与当前不一致即「结果已过期」 */
  const [dryGrace, setDryGrace] = useState<number | null | undefined>(undefined)
  const [applied, setApplied] = useState<GCRunResult | null>(null)
  const [runError, setRunError] = useState<ApiError | null>(null)

  const dryStale = dryRun !== null && dryGrace !== undefined && dryGrace !== grace
  const canApply = adminWrite && dryRun !== null && !dryStale && grace !== 'invalid' && running === ''

  const doDryRun = async (): Promise<void> => {
    if (grace === 'invalid' || running !== '') return
    setRunning('dry')
    setRunError(null)
    setApplied(null)
    try {
      const r = await runGC(false, grace)
      setDryRun(r)
      setDryGrace(grace)
    } catch (err) {
      setRunError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setRunning('')
    }
  }

  const doApply = async (): Promise<void> => {
    if (!canApply || !dryRun) return
    const holder = { typed: '' }
    const body: ReactNode = (
      <>
        <p>{tt('将')}<b>{tt('立即删除')}</b>{tt('垃圾回收候选 blob——制品不可变，删除')}<b>{tt('没有撤销')}</b>{tt('。')}        </p>
        <div className="kv">
          <span className="k">{tt('候选（最近试运行）')}</span>
          <span className="mono">
            {formatCount(dryRun.candidateCount)} {tt('项 /')} {formatBytes(dryRun.candidateBytes)}
          </span>
        </div>
        <div className="kv">
          <span className="k">{tt('grace 窗口')}</span>
          <span>{graceLabel(grace)}</span>
        </div>
        <div className="field" style={{ maxWidth: 'none', marginBottom: 0, marginTop: 12 }}>
          <label htmlFor="gc-confirm">{tt('输入')} <b className="mono" lang="en">YES</b> {tt('以确认：')}          </label>
          <input
            id="gc-confirm"
            className="confirm-input"
            autoComplete="off"
            onChange={(e) => {
              holder.typed = e.target.value
            }}
            data-testid="gc-confirm-text"
            lang="en"
          />
        </div>
      </>
    )
    const ok = await confirm({
      title: tt('执行垃圾回收（apply）'),
      body,
      danger: true,
      confirmLabel: tt('执行 GC'),
      confirmDisabled: () => holder.typed.trim().toUpperCase() !== 'YES',
    })
    if (!ok) return
    setRunning('apply')
    setRunError(null)
    try {
      // canApply 门控已排除 'invalid'（TS 经别名条件收窄）
      const r = await runGC(true, grace)
      setApplied(r)
      stats.reload()
      toast.success(
        tt('GC 完成：回收 {v1} 项，释放 {v2}', { v1: formatCount(r.deletedCount), v2: formatBytes(r.candidateBytes) }),
        {
          label: tt('查看审计（gc.run）'),
          onClick: () => navigate('/admin/governance/audit'),
        },
      )
    } catch (err) {
      setRunError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setRunning('')
    }
  }

  const latest = applied ?? dryRun

  return (
    <div data-testid="gc-page">
      <div className="page-header">
        <h2>{tt('维护')}</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>{tt('垃圾回收（GC）定时与手动维护、存储迁移（FR-145.7 / console-m8 §6.14）')}        </span>
      </div>

      <section className="card section" data-testid="gc-stats">
        <h3>{tt('存储概况')}</h3>
        {stats.status === 'loading' && <Skeleton lines={4} />}
        {stats.status === 'error' && stats.error && <ErrorCard error={stats.error} onRetry={stats.reload} />}
        {stats.status === 'forbidden' && stats.error && (
          <EmptyState
            message={tt('无权限查看存储概况')}
            hint={tt('存储统计与 GC 均为管理员视图（GET /api/v1/storage/stats / POST /api/v1/system/gc 仅 admin）。')}
          />
        )}
        {stats.status === 'ok' && stats.data && (
          <>
            <div className="kv">
              <span className="k">blob</span>
              <span className="mono">{formatCount(stats.data.blobs)}</span>
            </div>
            <div className="kv">
              <span className="k">{tt('逻辑容量')}</span>
              <span className="mono">{formatBytes(stats.data.logical_bytes)}</span>
            </div>
            <div className="kv">
              <span className="k">{tt('物理占用')}</span>
              <span className="mono">{formatBytes(stats.data.physical_bytes)}</span>
            </div>
            <div className="kv">
              <span className="k">{tt('去重率')}</span>
              <span className="mono">
                {(dedupRatio(stats.data.logical_bytes, stats.data.physical_bytes) * 100).toFixed(0)}%
              </span>
            </div>
            <p className="field-hint" style={{ marginBottom: 0 }}>{tt('上次 GC 运行记录经审计查询（')}<Link to="/admin/governance/audit">{tt('审计日志')}</Link> {tt('过滤')} <span className="mono" lang="en">gc.run</span>{tt('）； GC 状态端点为 P2 债务（ux R4）。')}            </p>
          </>
        )}
      </section>

      {/* 定时维护卡（T-462 / FR-145.7）：三 cron 槽 + 手动面并存；与 stats
          同门收敛（adminWrite || readOnly 才渲染——普通 user 直链只见无权限卡） */}
      {(adminWrite || readOnly) && (
        <MaintenanceCronCard
          readOnly={readOnly}
          adminWrite={adminWrite}
          onGotoDangerZone={() =>
            dangerRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
          }
        />
      )}

      {/* 存储迁移面板（T-160）：只读进度 + 5s 轮询；自身收敛 403 隐藏 /
          501 未配置降级，与非 admin 的 stats 无权限卡互不干扰 */}
      <MigrationPanel />

      {(adminWrite || readOnly) && (
        <div className="danger-zone" ref={dangerRef} data-testid="gc-danger-zone">
          <h3>{tt('危险区：垃圾回收')}</h3>
          <p>{tt('回收未被任何节点引用且超过 grace 窗口的 blob（grace 基准 = blob mtime）。 先试运行看候选，再输入确认执行。与 export / 其它 gc 互斥（运行中被拒 409）。')}          </p>
          {readOnly && (
            <p className="admin-note" data-testid="gc-readonly-note">{tt('只读管理员（readonly_admin）：GC 全部路由（含 dry-run）均为管理面写操作 （system:write），入口已禁用——直接提交会被服务端 403 拒绝。')}            </p>
          )}

          <details className="grace-details">
            <summary>{tt('高级：graceHours（')}{grace === 'invalid' ? tt('输入非法') : graceLabel(grace)}{tt('）')}</summary>
            <div className="field" style={{ marginTop: 8 }}>
              <label htmlFor="gc-grace">{tt('graceHours（小时，0 = 无宽限窗口；留空 = 实例配置缺省）')}              </label>
              <TextField
                id="gc-grace"
                size="small"
                inputMode="numeric"
                autoComplete="off"
                value={graceInput}
                onChange={(e) => setGraceInput(e.target.value)}
                disabled={readOnly}
                sx={{ width: 200 }}
                slotProps={{ htmlInput: { 'data-testid': 'gc-grace-hours', lang: 'en', className: 'mono' } }}
              />
              {grace === 'invalid' && (
                <span className="field-error">{tt('需为 0~')}{GC_MAX_GRACE_HOURS} {tt('的整数')}</span>
              )}
            </div>
          </details>

          <div className="gc-actions">
            <Button
              variant="outlined"
              size="small"
             
              disabled={grace === 'invalid' || running !== '' || readOnly}
              onClick={() => void doDryRun()}
              data-testid="gc-dryrun"
              title={readOnly ? tt('只读管理员：GC 试运行是 system:write（服务端 403 兜底）') : undefined}
            >
              {running === 'dry' ? tt('试运行中…') : tt('试运行（dry-run）')}
            </Button>
            <Button
              variant="outlined"
              color="error"
              size="small"
             
              disabled={!canApply || readOnly}
              onClick={() => void doApply()}
              data-testid="gc-apply"
              title={readOnly ? tt('只读管理员：GC 执行是 system:write（服务端 403 兜底）') : dryStale ? tt('参数已变更，请重新试运行') : canApply ? '' : tt('先完成一次当前参数下的试运行')}
            >
              {running === 'apply' ? tt('执行中…') : tt('执行 GC（apply）')}
            </Button>
            {dryStale && (
              <span className="field-error" role="alert">{tt('试运行结果基于已变更的 grace 参数——请重新试运行')}              </span>
            )}
          </div>

          {runError && (
            <Alert severity="error" icon={false} className="gc-error">
              <div className="headline">
                <span aria-hidden="true">✗</span>
                {runError.status === 409
                  ? tt('维护操作互斥——请求被拒')
                  : tt('GC 请求失败（HTTP {v1}）', { v1: runError.status })}
              </div>
              {runError.status === 409 && (
                <div className="text-2">{tt('data 目录锁正被其它维护操作持有（export / gc）。等待其完成后再试； 队列化会绑架连接，服务端按 PRD 语义直接拒绝。')}                </div>
              )}
              <pre lang="en">{runError.raw || runError.message}</pre>
            </Alert>
          )}

          {latest && (
            <div className="gc-result" data-testid="gc-result">
              {latest.candidateCount === 0 ? (
                <div className="gc-empty-ok" data-testid="gc-empty-ok">
                  <span className="status-dot ok" aria-hidden="true" /> {tt('没有可回收的 blob（在当前 grace 窗口下）')}                </div>
              ) : applied ? (
                <>
                  <h4>{tt('执行结果（apply）')}</h4>
                  <div className="kv">
                    <span className="k">{tt('实际回收')}</span>
                    <span className="mono">{formatCount(applied.deletedCount)} {tt('项')}</span>
                  </div>
                  <div className="kv">
                    <span className="k">{tt('释放字节')}</span>
                    <span className="mono">{formatBytes(applied.candidateBytes)}</span>
                  </div>
                  <div className="kv">
                    <span className="k">{tt('试运行预计')}</span>
                    <span className="mono">{formatCount(applied.candidateCount)} {tt('项')}</span>
                  </div>
                  {applied.deletedCount !== applied.candidateCount && (
                    <p className="field-hint">{tt('实际与预计不等：试运行与执行之间有写入方竞态（诚实分歧，非错误）。')}                    </p>
                  )}
                </>
              ) : (
                <>
                  <h4>{tt('试运行结果（未删除任何数据）')}</h4>
                  <div className="kv">
                    <span className="k">{tt('候选 blob')}</span>
                    <span className="mono">{formatCount(latest.candidateCount)} {tt('项')}</span>
                  </div>
                  <div className="kv">
                    <span className="k">{tt('预计回收')}</span>
                    <span className="mono">{formatBytes(latest.candidateBytes)}</span>
                  </div>
                  <p className="field-hint" style={{ marginBottom: 0 }}>{tt('grace 窗口内（mtime 距今不足')} {graceLabel(grace === 'invalid' ? null : grace)}{tt('）的孤儿不回收。 候选逐项清单不在 REST 回执里（仅聚合计数）——逐项核对走 CLI')}{' '}
                    <span className="mono" lang="en">binflow-server gc</span>{tt('。')}                  </p>
                </>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
