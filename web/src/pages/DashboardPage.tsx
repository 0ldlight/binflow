import { Link } from 'react-router-dom'
import type { ReactNode } from 'react'

import { useAuth } from '../app/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { ErrorCard } from '../components/ErrorCard'
import { Skeleton } from '../components/Skeleton'
import { getHealth, getRecentAudit, getRepositories, getStorageStats } from '../lib/api'
import type { HealthInfo, RepoListItem, StorageStats, AuditPage } from '../lib/api'
import { dedupRatio, formatAuditTime, formatBytes, formatCount } from '../lib/format'
import { useAsync } from '../lib/useAsync'
import { useVersion } from '../lib/useVersion'

// 仪表盘（console-ux §4.2 / §5.2）：只读聚合——健康 / 存储 stats /
// 仓库计数 / 最近审计 8 条。各卡片独立骨架独立到达（health 慢不挡
// storage stats）。四态矩阵（§5.3）：loading=卡片骨架、empty=空实例
// CTA、error=该卡错误卡其余照常、403=整卡隐藏（§3.3「API 403 即隐藏」
// ——本批管理面数据端点均 admin-only，非 admin 面板自然收敛为实例卡）。

function Card({
  title,
  testid,
  state,
  children,
}: {
  title: string
  testid: string
  state: { status: 'loading' | 'ok' | 'error' | 'forbidden'; error: Parameters<typeof ErrorCard>[0]['error'] | null; reload: () => void }
  children: ReactNode
}) {
  if (state.status === 'forbidden') return null
  return (
    <section className="card" data-testid={testid}>
      <h3>{title}</h3>
      {state.status === 'loading' && <Skeleton lines={4} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'ok' && children}
    </section>
  )
}

function SubsystemRow({ name, st }: { name: string; st: { status: string; detail?: string } }) {
  const ok = st.status === 'ok'
  return (
    <div className="kv">
      <span className="k">
        <span className={`status-dot ${ok ? 'ok' : 'err'}`} aria-hidden="true" />
        {name}
      </span>
      <span className={ok ? 'text-2' : ''} title={st.detail ?? ''}>
        {ok ? 'ok' : (st.detail ?? st.status)}
      </span>
    </div>
  )
}

function HealthCard() {
  const state = useAsync<HealthInfo>(getHealth, [])
  const overallOk = state.data?.status === 'ok'
  const anyDown = state.data ? state.data.status !== 'ok' : false
  if (state.status === 'forbidden') return null
  return (
    <section
      className={`card${anyDown ? (overallOk ? ' degraded' : ' down') : ''}`}
      data-testid="dashboard-health-card"
    >
      <h3>健康</h3>
      {state.status === 'loading' && <Skeleton lines={4} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'ok' && state.data && (
        <>
          <div className="stat-row">
            <span className={`status-dot ${overallOk ? 'ok' : 'err'}`} aria-hidden="true" />
            <span className="v">{overallOk ? 'ok' : 'error'}</span>
          </div>
          <SubsystemRow name="storage" st={state.data.storage} />
          <SubsystemRow name="metadata" st={state.data.metadata} />
          <SubsystemRow name="registry" st={state.data.registry} />
        </>
      )}
    </section>
  )
}

function StorageCard() {
  const state = useAsync<StorageStats>(getStorageStats, [])
  const d = state.data
  const ratio = d ? dedupRatio(d.logical_bytes, d.physical_bytes) : 0
  return (
    <Card title="存储" testid="dashboard-storage-card" state={state}>
      {d && (
        <>
          <div className="kv">
            <span className="k">blob</span>
            <span className="mono">{formatCount(d.blobs)}</span>
          </div>
          <div className="kv">
            <span className="k">逻辑容量</span>
            <span className="mono">{formatBytes(d.logical_bytes)}</span>
          </div>
          <div className="kv">
            <span className="k">物理占用</span>
            <span className="mono">{formatBytes(d.physical_bytes)}</span>
          </div>
          <div className="kv">
            <span className="k">去重率</span>
            <span className="mono">{(ratio * 100).toFixed(0)}%</span>
          </div>
        </>
      )}
    </Card>
  )
}

function ReposCard() {
  const state = useAsync<RepoListItem[]>(getRepositories, [])
  const repos = state.data ?? []
  const byType = (t: string) => repos.filter((r) => r.type === t).length
  const remotes = repos.filter((r) => r.type === 'remote')
  return (
    <Card title="仓库" testid="dashboard-repos-card" state={state}>
      {repos.length === 0 ? (
        <EmptyState
          message="还没有仓库"
          action={
            <Link className="btn primary" to="/repositories/new">
              创建第一个仓库
            </Link>
          }
          hint="仓库管理页将在下一批交付（T-99），届时可用表单建仓"
          testid="repos-empty"
        />
      ) : (
        <>
          <div className="kv">
            <span className="k">Local</span>
            <span className="mono">{byType('local')}</span>
          </div>
          <div className="kv">
            <span className="k">Remote</span>
            <span className="mono">{byType('remote')}</span>
          </div>
          <div className="kv">
            <span className="k">Virtual</span>
            <span className="mono">{byType('virtual')}</span>
          </div>
          {remotes.length > 0 && (
            <div className="water-mark">
              {remotes.length} 个 remote 仓，上游状态 <span className="mono">—</span>（统计端点未开放，ux R9 兜底）
            </div>
          )}
        </>
      )}
    </Card>
  )
}

function AuditCard() {
  const state = useAsync<AuditPage>(() => getRecentAudit(8), [])
  const events = state.data?.events ?? []
  if (state.status === 'forbidden') return null
  return (
    <section className="card" data-testid="dashboard-audit-card">
      <h3>
        最近审计（最新 8 条）{state.status === 'ok' && <Link to="/audit" style={{ float: 'right', fontSize: 12 }}>查看全部 →</Link>}
      </h3>
      {state.status === 'loading' && <Skeleton lines={6} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'ok' &&
        (events.length === 0 ? (
          <EmptyState message="暂无审计事件" hint="登录、建仓、上传等操作会记录在这里" />
        ) : (
          <table className="table" data-testid="dashboard-audit-table">
            <thead>
              <tr>
                <th scope="col">时间</th>
                <th scope="col">操作者</th>
                <th scope="col">动作</th>
                <th scope="col">对象</th>
              </tr>
            </thead>
            <tbody>
              {events.map((ev) => (
                <tr key={ev.id}>
                  <td className="mono">{formatAuditTime(ev.time)}</td>
                  <td>{ev.actor}</td>
                  <td className="mono" lang="en">
                    {ev.action}
                  </td>
                  <td className="mono wrap" lang="en">
                    {ev.repo ? `${ev.repo}/${ev.path}` : ev.path}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ))}
    </section>
  )
}

/** 实例卡：版本端点开放（routeAuth{}），非 admin 也能看到——面板不至于空 */
function InstanceCard() {
  const version = useVersion()
  const { session } = useAuth()
  return (
    <section className="card" data-testid="dashboard-instance-card">
      <h3>实例</h3>
      <div className="kv">
        <span className="k">产品</span>
        <span className="mono" lang="en">
          {version?.product ?? '—'}
        </span>
      </div>
      <div className="kv">
        <span className="k">版本</span>
        <span className="mono" lang="en">
          {version ? `v${version.version}` : '—'}
        </span>
      </div>
      <div className="kv">
        <span className="k">当前用户</span>
        <span>
          {session?.username}
          {session?.admin ? '（admin）' : ''}
        </span>
      </div>
    </section>
  )
}

export default function DashboardPage() {
  const { session } = useAuth()
  const admin = session?.admin ?? false

  return (
    <div data-testid="dashboard">
      <div className="page-header">
        <h2>仪表盘</h2>
      </div>
      {!admin && (
        <div className="card section" style={{ maxWidth: 640 }}>
          以 <b>{session?.username}</b>（非 admin）身份登录：健康、存储、仓库与审计面板为管理员视图，
          已按「无权限即隐藏」收敛；你的可用操作见左下角「修改口令」。
        </div>
      )}
      <div className="card-grid">
        <InstanceCard />
        <HealthCard />
        <StorageCard />
        <ReposCard />
      </div>
      <AuditCard />
    </div>
  )
}
