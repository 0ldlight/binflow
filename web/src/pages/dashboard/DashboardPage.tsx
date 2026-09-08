// 仪表盘（console-ux §6.2——P2 新栈重写：五卡 → 指标面板形态）。
//
// 语义承接（audit §2.1 dashboard 行）：
// - 各面板独立骨架独立到达（health 慢不挡 storage stats）；
// - 四态矩阵：loading=骨架、empty=空实例 CTA、error=该卡错误卡其余照常、
//   403=整卡隐藏（§3.6「API 403 即隐藏」——非 admin 面板自然收敛）；
// - 审计行点击/Enter 深链 /artifacts/<repo>/<路径段>（K67-3 规范形）；
// - 建仓 CTA（仅 admin，L4 预收敛）；readonly/非 admin 收敛横幅；
// - Remote 状态不建（无端点，ux R9——不伪造）。
// - 锚族原样：dashboard / dashboard-health-card / dashboard-storage-card
//   / dashboard-repos-card / dashboard-audit-card / dashboard-instance-card
//   / dashboard-audit-row-* / dashboard-audit-all / dashboard-repos-create
//   / dashboard-readonly-note。
import { Link, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'

import { useAuth } from '@/app/AuthContext'
import { ButtonAsChild } from '@/components/ui/button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { ApiError, canAdminWrite, getHealth, getRecentAudit, getRepositories, getStorageStats, isReadOnlyAdmin } from '@/lib/api'
import type { AuditEvent, HealthInfo, StorageStats } from '@/lib/api'
import { qk } from '@/lib/query'
import { dedupRatio, formatAuditTime, formatBytes, formatCount } from '@/lib/format'
import { useVersion } from '@/lib/useVersion'
import { tr } from '@/i18n'

const t = tr('console')

/** 审计事件 → 制品树深链（K67-3 路径段规范形） */
function auditTarget(ev: AuditEvent): string | null {
  if (!ev.repo || ev.repo === '') return null
  const segs = ev.path.replace(/^\//, '').split('/').filter((s) => s !== '')
  return `/artifacts/${[ev.repo, ...segs].map((s) => encodeURIComponent(s)).join('/')}`
}

export default function DashboardPage() {
  const { session } = useAuth()
  const navigate = useNavigate()
  const admin = session?.admin ?? false
  const readOnly = isReadOnlyAdmin(session)
  const version = useVersion()

  const health = useQuery({ queryKey: qk.health(), queryFn: () => getHealth(), retry: 1 })
  const stats = useQuery({ queryKey: qk.storageStats(), queryFn: () => getStorageStats(), retry: 1 })
  const repos = useQuery({ queryKey: qk.repositories(), queryFn: () => getRepositories(), retry: 1 })
  const audit = useQuery({ queryKey: ['audit', 'recent'], queryFn: () => getRecentAudit(8), retry: 1 })

  const healthForbidden = health.error instanceof ApiError && health.error.status === 403
  const statsForbidden = stats.error instanceof ApiError && stats.error.status === 403
  const reposForbidden = repos.error instanceof ApiError && repos.error.status === 403
  const auditForbidden = audit.error instanceof ApiError && audit.error.status === 403

  const h = health.data as HealthInfo | undefined
  const s = stats.data as StorageStats | undefined
  const repoList = repos.data ?? []
  const events = (audit.data as { events: AuditEvent[] } | undefined)?.events ?? []
  const canCreate = canAdminWrite(session)

  const cardState = (q: { isPending: boolean; error: unknown }, forbidden: boolean, reload: () => void) =>
    forbidden
      ? ({ status: 'forbidden' } as const)
      : q.isPending
        ? ({ status: 'loading' } as const)
        : q.error
          ? ({ status: 'error', error: q.error as ApiError, reload } as const)
          : ({ status: 'ok' } as const)

  const wrap = (testid: string, title: string, state: ReturnType<typeof cardState>, children: React.ReactNode) => {
    if (state.status === 'forbidden') return null
    return (
      <section className="card rounded-md border border-border bg-surface-1 p-4" data-testid={testid}>
        <h3 className="mb-3 text-[13px] font-semibold">{title}</h3>
        {state.status === 'loading' && <StateSkeleton lines={4} />}
        {state.status === 'error' && <ErrorCard error={state.error} onRetry={state.reload} />}
        {state.status === 'ok' && children}
      </section>
    )
  }

  const kv = (k: string, v: React.ReactNode, mono = true) => (
    <div className="kv mb-1 flex gap-2 text-dense">
      <span className="k w-28 shrink-0 text-muted-foreground">{k}</span>
      <span className={mono ? 'min-w-0 break-all font-mono' : 'min-w-0 break-all'}>{v}</span>
    </div>
  )

  const overallOk = h?.status === 'ok'
  const anyDown = h ? h.status !== 'ok' : false

  return (
    <div data-testid="dashboard" className="flex flex-col gap-3">
      <div className="page-header">
        <h2 className="text-lg font-semibold">{t('仪表盘')}</h2>
      </div>
      {!admin && !readOnly && (
        <div className="card section max-w-2xl rounded-md border border-border bg-surface-1 p-4 text-dense">
          {t('以')} <b>{session?.username}</b>{t('（非 admin）身份登录：健康、存储、仓库与审计面板为管理员视图， 已按「无权限即隐藏」收敛；可用操作见右上角用户菜单「编辑档案」（修改口令）与全局搜索。')}
        </div>
      )}
      {readOnly && (
        <div className="card section max-w-2xl rounded-md border border-border bg-surface-1 p-4 text-dense" data-testid="dashboard-readonly-note">
          {t('以')} <b>{session?.username}</b>{t('（readonly_admin）身份登录：管理面全量只读——健康、存储、仓库与审计 面板可见；配置变更（建仓 / 用户与权限 / GC / 复制）需 admin，提交会被服务端 403 拒绝。')}
        </div>
      )}
      <div className="card-grid grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4">
        {/* 实例卡：版本端点开放，非 admin 也能看到 */}
        <section className="card rounded-md border border-border bg-surface-1 p-4" data-testid="dashboard-instance-card">
          <h3 className="mb-3 text-[13px] font-semibold">{t('实例')}</h3>
          {kv(t('产品'), version?.product ?? '—')}
          {kv(t('版本'), version ? `v${version.version}` : '—')}
          {kv(t('当前用户'), (
            <>
              {session?.username}
              {session?.admin ? t('（admin）') : isReadOnlyAdmin(session) ? t('（readonly_admin）') : ''}
            </>
          ), false)}
        </section>

        {/* 健康卡 */}
        {!healthForbidden && (
          <section
            className={`card rounded-md border bg-surface-1 p-4 ${anyDown ? (overallOk ? 'border-warning' : 'border-destructive') : 'border-border'}`}
            data-testid="dashboard-health-card"
          >
            <h3 className="mb-3 text-[13px] font-semibold">{t('健康')}</h3>
            {health.isPending && <StateSkeleton lines={4} />}
            {health.error && !healthForbidden && <ErrorCard error={health.error as ApiError} onRetry={() => void health.refetch()} />}
            {h && (
              <>
                <div className="stat-row mb-2 flex items-center gap-2 text-dense">
                  <span className={`status-dot inline-block size-2 rounded-full ${overallOk ? 'bg-success' : 'bg-destructive'}`} aria-hidden="true" />
                  <span className="v font-mono">{overallOk ? 'ok' : 'error'}</span>
                </div>
                {(['storage', 'metadata', 'registry'] as const).map((name) => {
                  const st = h[name]
                  const ok = st.status === 'ok'
                  return (
                    <div className="kv mb-1 flex gap-2 text-dense" key={name}>
                      <span className="k flex w-28 shrink-0 items-center gap-1.5 text-muted-foreground">
                        <span className={`status-dot inline-block size-1.5 rounded-full ${ok ? 'bg-success' : 'bg-destructive'}`} aria-hidden="true" />
                        {name}
                      </span>
                      <span className={`${ok ? 'text-muted-foreground' : ''} font-mono`} title={st.detail ?? ''}>
                        {ok ? 'ok' : (st.detail ?? st.status)}
                      </span>
                    </div>
                  )
                })}
              </>
            )}
          </section>
        )}

        {/* 存储卡 */}
        {wrap('dashboard-storage-card', t('存储'), cardState(stats, statsForbidden, () => void stats.refetch()), s && (
          <>
            {kv('blob', formatCount(s.blobs))}
            {kv(t('逻辑容量'), formatBytes(s.logical_bytes))}
            {kv(t('物理占用'), formatBytes(s.physical_bytes))}
            {kv(t('去重率'), `${(dedupRatio(s.logical_bytes, s.physical_bytes) * 100).toFixed(0)}%`)}
          </>
        ))}

        {/* 仓库卡 */}
        {wrap('dashboard-repos-card', t('仓库'), cardState(repos, reposForbidden, () => void repos.refetch()), (
          <>
            {repoList.length === 0 ? (
              <EmptyState
                message={t('还没有仓库')}
                action={
                  canCreate ? (
                    <ButtonAsChild size="sm">
                      <Link to="/admin/repositories/new">{t('创建第一个仓库')}</Link>
                    </ButtonAsChild>
                  ) : undefined
                }
                hint={t('建议从 local + generic 起步（任意文件）；协议仓选型见 docs/user 接入文档')}
              />
            ) : (
              <>
                {kv('Local', repoList.filter((r) => r.type === 'local').length)}
                {kv('Remote', repoList.filter((r) => r.type === 'remote').length)}
                {kv('Virtual', repoList.filter((r) => r.type === 'virtual').length)}
                {repoList.some((r) => r.type === 'remote') && (
                  <div className="water-mark mt-1 text-aux text-muted-foreground">
                    {repoList.filter((r) => r.type === 'remote').length} {t('个 remote 仓，上游状态')} <span className="font-mono">—</span>{t('（统计端点未开放，ux R9 兜底）')}
                  </div>
                )}
                {canCreate && (
                  <p className="mt-2">
                    <Link to="/admin/repositories/new" data-testid="dashboard-repos-create" className="text-primary hover:underline">
                      {t('建仓 →')}
                    </Link>
                  </p>
                )}
              </>
            )}
          </>
        ))}
      </div>

      {/* 审计卡 */}
      {!auditForbidden && (
        <section className="card rounded-md border border-border bg-surface-1 p-4" data-testid="dashboard-audit-card">
          <div className="mb-3 flex items-baseline justify-between">
            <h3 className="text-[13px] font-semibold">{t('最近审计（最新 8 条）')}</h3>
            {audit.isSuccess && (
              <Link to="/admin/governance/audit" className="text-[12px] leading-5 text-primary hover:underline" data-testid="dashboard-audit-all">
                {t('查看全部 →')}
              </Link>
            )}
          </div>
          {audit.isPending && <StateSkeleton lines={6} />}
          {audit.error && !auditForbidden && <ErrorCard error={audit.error as ApiError} onRetry={() => void audit.refetch()} />}
          {audit.isSuccess &&
            (events.length === 0 ? (
              <EmptyState message={t('暂无审计事件')} hint={t('登录、建仓、上传等操作会记录在这里')} />
            ) : (
              <table className="w-full text-dense">
                <thead>
                  <tr className="border-b border-border text-left text-aux text-muted-foreground">
                    <th scope="col" className="py-1.5 pr-3 font-medium">{t('时间')}</th>
                    <th scope="col" className="py-1.5 pr-3 font-medium">{t('操作者')}</th>
                    <th scope="col" className="py-1.5 pr-3 font-medium">{t('动作')}</th>
                    <th scope="col" className="py-1.5 font-medium">{t('对象')}</th>
                  </tr>
                </thead>
                <tbody>
                  {events.map((ev, i) => {
                    const target = auditTarget(ev)
                    return (
                      <tr
                        key={ev.id}
                        className={`border-b border-border/60 ${target ? 'cursor-pointer hover:bg-accent' : ''}`}
                        data-testid={`dashboard-audit-row-${i}`}
                        tabIndex={target ? 0 : undefined}
                        onClick={target ? () => navigate(target) : undefined}
                        onKeyDown={
                          target
                            ? (e) => {
                                if (e.key === 'Enter') navigate(target)
                              }
                            : undefined
                        }
                      >
                        <td className="whitespace-nowrap py-1.5 pr-3 font-mono">{formatAuditTime(ev.time)}</td>
                        <td className="py-1.5 pr-3">{ev.actor}</td>
                        <td className="whitespace-nowrap py-1.5 pr-3 font-mono" lang="en">{ev.action}</td>
                        <td className="break-all py-1.5 font-mono" lang="en">{ev.repo ? `${ev.repo}/${ev.path}` : ev.path}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            ))}
        </section>
      )}
    </div>
  )
}
