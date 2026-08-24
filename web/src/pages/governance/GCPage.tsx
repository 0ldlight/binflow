import { useState } from 'react'
import type { ReactNode } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { useConfirm } from '../../components/ConfirmDialog'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, canAdminWrite, errText, getStorageStats, isReadOnlyAdmin } from '../../lib/api'
import { dedupRatio, formatBytes, formatCount } from '../../lib/format'
import { GC_MAX_GRACE_HOURS, runGC } from '../../lib/governance'
import type { GCRunResult } from '../../lib/governance'
import { useAsync } from '../../lib/useAsync'
import MigrationPanel from './MigrationPanel'

// 维护（GC）（console-m8 §6.14 归位 /admin/governance/gc；T-102 语义原样）：
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
  if (hours === null) return '实例配置缺省（storage.gc_grace_hours）'
  if (hours === 0) return '无宽限窗口（0——只回收已确认孤儿）'
  return `${hours} 小时`
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
        <p>
          将<b>立即删除</b>垃圾回收候选 blob——制品不可变，删除<b>没有撤销</b>。
        </p>
        <div className="kv">
          <span className="k">候选（最近试运行）</span>
          <span className="mono">
            {formatCount(dryRun.candidateCount)} 项 / {formatBytes(dryRun.candidateBytes)}
          </span>
        </div>
        <div className="kv">
          <span className="k">grace 窗口</span>
          <span>{graceLabel(grace)}</span>
        </div>
        <div className="field" style={{ maxWidth: 'none', marginBottom: 0, marginTop: 12 }}>
          <label htmlFor="gc-confirm">
            输入 <b className="mono" lang="en">YES</b> 以确认：
          </label>
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
      title: '执行垃圾回收（apply）',
      body,
      danger: true,
      confirmLabel: '执行 GC',
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
        `GC 完成：回收 ${formatCount(r.deletedCount)} 项，释放 ${formatBytes(r.candidateBytes)}`,
        {
          label: '查看审计（gc.run）',
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
        <h2>维护</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>
          垃圾回收（GC）与存储迁移（console-m8 §6.14 分块骨架）
        </span>
      </div>

      <section className="card section" data-testid="gc-stats">
        <h3>存储概况</h3>
        {stats.status === 'loading' && <Skeleton lines={4} />}
        {stats.status === 'error' && stats.error && <ErrorCard error={stats.error} onRetry={stats.reload} />}
        {stats.status === 'forbidden' && stats.error && (
          <EmptyState
            message="无权限查看存储概况"
            hint="存储统计与 GC 均为管理员视图（GET /api/v1/storage/stats / POST /api/v1/system/gc 仅 admin）。"
          />
        )}
        {stats.status === 'ok' && stats.data && (
          <>
            <div className="kv">
              <span className="k">blob</span>
              <span className="mono">{formatCount(stats.data.blobs)}</span>
            </div>
            <div className="kv">
              <span className="k">逻辑容量</span>
              <span className="mono">{formatBytes(stats.data.logical_bytes)}</span>
            </div>
            <div className="kv">
              <span className="k">物理占用</span>
              <span className="mono">{formatBytes(stats.data.physical_bytes)}</span>
            </div>
            <div className="kv">
              <span className="k">去重率</span>
              <span className="mono">
                {(dedupRatio(stats.data.logical_bytes, stats.data.physical_bytes) * 100).toFixed(0)}%
              </span>
            </div>
            <p className="field-hint" style={{ marginBottom: 0 }}>
              上次 GC 运行记录经审计查询（<Link to="/admin/governance/audit">审计日志</Link> 过滤 <span className="mono" lang="en">gc.run</span>）；
              GC 状态端点为 P2 债务（ux R4）。
            </p>
          </>
        )}
      </section>

      {/* 存储迁移面板（T-160）：只读进度 + 5s 轮询；自身收敛 403 隐藏 /
          501 未配置降级，与非 admin 的 stats 无权限卡互不干扰 */}
      <MigrationPanel />

      {(adminWrite || readOnly) && (
        <div className="danger-zone" data-testid="gc-danger-zone">
          <h3>危险区：垃圾回收</h3>
          <p>
            回收未被任何节点引用且超过 grace 窗口的 blob（grace 基准 = blob mtime）。
            先试运行看候选，再输入确认执行。与 export / 其它 gc 互斥（运行中被拒 409）。
          </p>
          {readOnly && (
            <p className="admin-note" data-testid="gc-readonly-note">
              只读管理员（readonly_admin）：GC 全部路由（含 dry-run）均为管理面写操作
              （system:write），入口已禁用——直接提交会被服务端 403 拒绝。
            </p>
          )}

          <details className="grace-details">
            <summary>高级：graceHours（{grace === 'invalid' ? '输入非法' : graceLabel(grace)}）</summary>
            <div className="field" style={{ marginTop: 8 }}>
              <label htmlFor="gc-grace">
                graceHours（小时，0 = 无宽限窗口；留空 = 实例配置缺省）
              </label>
              <input
                id="gc-grace"
                className="mono"
                inputMode="numeric"
                autoComplete="off"
                value={graceInput}
                onChange={(e) => setGraceInput(e.target.value)}
                disabled={readOnly}
                data-testid="gc-grace-hours"
                lang="en"
              />
              {grace === 'invalid' && (
                <span className="field-error">需为 0~{GC_MAX_GRACE_HOURS} 的整数</span>
              )}
            </div>
          </details>

          <div className="gc-actions">
            <button
              type="button"
              className="btn"
              disabled={grace === 'invalid' || running !== '' || readOnly}
              onClick={() => void doDryRun()}
              data-testid="gc-dryrun"
              title={readOnly ? '只读管理员：GC 试运行是 system:write（服务端 403 兜底）' : undefined}
            >
              {running === 'dry' ? '试运行中…' : '试运行（dry-run）'}
            </button>
            <button
              type="button"
              className="btn danger"
              disabled={!canApply || readOnly}
              onClick={() => void doApply()}
              data-testid="gc-apply"
              title={readOnly ? '只读管理员：GC 执行是 system:write（服务端 403 兜底）' : dryStale ? '参数已变更，请重新试运行' : canApply ? '' : '先完成一次当前参数下的试运行'}
            >
              {running === 'apply' ? '执行中…' : '执行 GC（apply）'}
            </button>
            {dryStale && (
              <span className="field-error" role="alert">
                试运行结果基于已变更的 grace 参数——请重新试运行
              </span>
            )}
          </div>

          {runError && (
            <div className="gc-error" role="alert">
              <div className="headline">
                <span aria-hidden="true">✗</span>
                {runError.status === 409
                  ? '维护操作互斥——请求被拒'
                  : `GC 请求失败（HTTP ${runError.status}）`}
              </div>
              {runError.status === 409 && (
                <div className="text-2">
                  data 目录锁正被其它维护操作持有（export / gc）。等待其完成后再试；
                  队列化会绑架连接，服务端按 PRD 语义直接拒绝。
                </div>
              )}
              <pre lang="en">{runError.raw || runError.message}</pre>
            </div>
          )}

          {latest && (
            <div className="gc-result" data-testid="gc-result">
              {latest.candidateCount === 0 ? (
                <div className="gc-empty-ok" data-testid="gc-empty-ok">
                  <span className="status-dot ok" aria-hidden="true" /> 没有可回收的 blob（在当前 grace 窗口下）
                </div>
              ) : applied ? (
                <>
                  <h4>执行结果（apply）</h4>
                  <div className="kv">
                    <span className="k">实际回收</span>
                    <span className="mono">{formatCount(applied.deletedCount)} 项</span>
                  </div>
                  <div className="kv">
                    <span className="k">释放字节</span>
                    <span className="mono">{formatBytes(applied.candidateBytes)}</span>
                  </div>
                  <div className="kv">
                    <span className="k">试运行预计</span>
                    <span className="mono">{formatCount(applied.candidateCount)} 项</span>
                  </div>
                  {applied.deletedCount !== applied.candidateCount && (
                    <p className="field-hint">
                      实际与预计不等：试运行与执行之间有写入方竞态（诚实分歧，非错误）。
                    </p>
                  )}
                </>
              ) : (
                <>
                  <h4>试运行结果（未删除任何数据）</h4>
                  <div className="kv">
                    <span className="k">候选 blob</span>
                    <span className="mono">{formatCount(latest.candidateCount)} 项</span>
                  </div>
                  <div className="kv">
                    <span className="k">预计回收</span>
                    <span className="mono">{formatBytes(latest.candidateBytes)}</span>
                  </div>
                  <p className="field-hint" style={{ marginBottom: 0 }}>
                    grace 窗口内（mtime 距今不足 {graceLabel(grace === 'invalid' ? null : grace)}）的孤儿不回收。
                    候选逐项清单不在 REST 回执里（仅聚合计数）——逐项核对走 CLI{' '}
                    <span className="mono" lang="en">binflow-server gc</span>。
                  </p>
                </>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
