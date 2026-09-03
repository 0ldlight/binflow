import { Link, useNavigate } from 'react-router-dom'
import type { ReactNode } from 'react'

import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Paper from '@mui/material/Paper'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import Typography from '@mui/material/Typography'

import { useAuth } from '../app/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { ErrorCard } from '../components/ErrorCard'
import { Skeleton } from '../components/Skeleton'
import { canAdminWrite, getHealth, getRecentAudit, getRepositories, getStorageStats, isReadOnlyAdmin } from '../lib/api'
import type { HealthInfo, RepoListItem, StorageStats, AuditPage, AuditEvent } from '../lib/api'
import { dedupRatio, formatAuditTime, formatBytes, formatCount } from '../lib/format'
import { useAsync } from '../lib/useAsync'
import { useVersion } from '../lib/useVersion'

// 仪表盘（console-m8 §6.2——应用模式数据面；reverse 未深走 Dashboard，
// 无对齐负担，保留 BinFlow 五卡形态）：健康 / 存储 stats / 仓库计数 /
// 最近审计 8 条。快捷入口（§6.2 线框）：仓库卡「建仓 →」（仅全量 admin，
// L4 写入口预收敛）与审计卡「查看全部 →」。审计行点击进对象（深链
// /artifacts/<repo>/<路径段>——T-236 深链自动展开消费；T-449 起发射
// 路径段规范形，?focus= 退役）。
// 各卡片独立骨架独立到达（health 慢不挡 storage stats）。四态矩阵
// （§3.2）：loading=卡片骨架、empty=空实例 CTA、error=该卡错误卡其余
// 照常、403=整卡隐藏（§3.6「API 403 即隐藏」——非 admin 面板自然收敛
// 为实例卡 + 说明）。
// §6.2 [4]「Remote 状态」不建：remote 上游健康/统计端点未开放（ux R9，
// 仓库卡内如实注记）——无端点支撑的形态不伪造。
// T-344 批 D：残面换装——section.card → Paper elevation 1（类名留 DOM：
// auxiliary.spec 的 `.card.section` 钩子 + 45 落点渐进迁移；base.css
// .card 族 :not(.MuiPaper-root) shim 排除）、h3 → Typography subtitle2、
// 审计表 → MUI Table(small)（行锚在 TableRow、mono/wrap 语义经 sx 重落）、
// 建仓 CTA 的 .btn → Button component={Link}。degraded/down = Paper sx
// 语义色边（§3.2 card 行目标态）。

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
    <Paper component="section" className="card" elevation={1} data-testid={testid}>
      <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>
        {title}
      </Typography>
      {state.status === 'loading' && <Skeleton lines={4} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'ok' && children}
    </Paper>
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
    <Paper
      component="section"
      className={`card${anyDown ? (overallOk ? ' degraded' : ' down') : ''}`}
      elevation={1}
      data-testid="dashboard-health-card"
      sx={anyDown ? { border: '1px solid', borderColor: overallOk ? 'warning.main' : 'error.main' } : undefined}
    >
      <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>
        健康
      </Typography>
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
    </Paper>
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
  const { session } = useAuth()
  const state = useAsync<RepoListItem[]>(getRepositories, [])
  const repos = state.data ?? []
  const byType = (t: string) => repos.filter((r) => r.type === t).length
  const remotes = repos.filter((r) => r.type === 'remote')
  const canCreate = canAdminWrite(session)
  return (
    <Card title="仓库" testid="dashboard-repos-card" state={state}>
      {repos.length === 0 ? (
        <EmptyState
          message="还没有仓库"
          action={
            canCreate ? (
              <Button component={Link} variant="contained" to="/admin/repositories/new">
                创建第一个仓库
              </Button>
            ) : undefined
          }
          hint="建议从 local + generic 起步（任意文件）；协议仓选型见 docs/user 接入文档"
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
          {/* 快捷入口（§6.2 线框 [3]）：建仓直达——仅全量 admin（L4 写入口
              预收敛；readonly_admin 不渲染，服务端 403 兜底） */}
          {canCreate && (
            <p style={{ margin: 'var(--bf-sp-3) 0 0' }}>
              <Link to="/admin/repositories/new" data-testid="dashboard-repos-create">
                建仓 →
              </Link>
            </p>
          )}
        </>
      )}
    </Card>
  )
}

/** 审计事件 → 制品树深链（T-449 发射端翻新 / K67-3 路径段规范形：文件
 *  与目录两态都是路径末段——目录会成为父层的一个子节点被选中；?focus=
 *  退役，旧深链由 ArtifactsBrowser 一次性折入维持兼容）。无 repo 的事件
 *  （用户/权限面）无树目标，行不可点。 */
function auditTarget(ev: AuditEvent): string | null {
  if (!ev.repo || ev.repo === '') return null
  const segs = ev.path.replace(/^\//, '').split('/').filter((s) => s !== '')
  return `/artifacts/${[ev.repo, ...segs].map((s) => encodeURIComponent(s)).join('/')}`
}

function AuditCard() {
  const navigate = useNavigate()
  const state = useAsync<AuditPage>(() => getRecentAudit(8), [])
  const events = state.data?.events ?? []
  if (state.status === 'forbidden') return null
  return (
    <Paper component="section" className="card" elevation={1} data-testid="dashboard-audit-card">
      <Box sx={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', mb: 1.5 }}>
        <Typography variant="subtitle2" component="h3">
          最近审计（最新 8 条）
        </Typography>
        {state.status === 'ok' && (
          <Link
            to="/admin/governance/audit"
            style={{ fontSize: 12, lineHeight: '20px' }}
            data-testid="dashboard-audit-all"
          >
            查看全部 →
          </Link>
        )}
      </Box>
      {state.status === 'loading' && <Skeleton lines={6} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'ok' &&
        (events.length === 0 ? (
          <EmptyState message="暂无审计事件" hint="登录、建仓、上传等操作会记录在这里" />
        ) : (
          <Table>
            <TableHead>
              <TableRow>
                <TableCell component="th" scope="col">时间</TableCell>
                <TableCell component="th" scope="col">操作者</TableCell>
                <TableCell component="th" scope="col">动作</TableCell>
                <TableCell component="th" scope="col">对象</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {events.map((ev, i) => {
                const target = auditTarget(ev)
                return (
                  <TableRow
                    key={ev.id}
                    hover
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
                    <TableCell className="mono" sx={{ whiteSpace: 'nowrap' }}>
                      {formatAuditTime(ev.time)}
                    </TableCell>
                    <TableCell>{ev.actor}</TableCell>
                    <TableCell className="mono" lang="en" sx={{ whiteSpace: 'nowrap' }}>
                      {ev.action}
                    </TableCell>
                    <TableCell className="mono" lang="en" sx={{ whiteSpace: 'normal', wordBreak: 'break-all' }}>
                      {ev.repo ? `${ev.repo}/${ev.path}` : ev.path}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        ))}
    </Paper>
  )
}

/** 实例卡：版本端点开放（routeAuth{}），非 admin 也能看到——面板不至于空 */
function InstanceCard() {
  const version = useVersion()
  const { session } = useAuth()
  return (
    <Paper component="section" className="card" elevation={1} data-testid="dashboard-instance-card">
      <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>
        实例
      </Typography>
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
          {session?.admin ? '（admin）' : isReadOnlyAdmin(session) ? '（readonly_admin）' : ''}
        </span>
      </div>
    </Paper>
  )
}

export default function DashboardPage() {
  const { session } = useAuth()
  const admin = session?.admin ?? false
  const readOnly = isReadOnlyAdmin(session)

  return (
    <div data-testid="dashboard">
      <div className="page-header">
        <h2>仪表盘</h2>
      </div>
      {/* 收敛说明（§3.6.4 姿态不变；M7 readonly 横幅语义融入新形态）。
          .card.section 类名留 DOM——auxiliary.spec 的类组合钩子。 */}
      {!admin && !readOnly && (
        <Paper component="div" className="card section" elevation={1} sx={{ maxWidth: 640 }}>
          以 <b>{session?.username}</b>（非 admin）身份登录：健康、存储、仓库与审计面板为管理员视图，
          已按「无权限即隐藏」收敛；可用操作见右上角用户菜单「编辑档案」（修改口令）与全局搜索。
        </Paper>
      )}
      {readOnly && (
        <Paper component="div" className="card section" elevation={1} sx={{ maxWidth: 640 }} data-testid="dashboard-readonly-note">
          以 <b>{session?.username}</b>（readonly_admin）身份登录：管理面全量只读——健康、存储、仓库与审计
          面板可见；配置变更（建仓 / 用户与权限 / GC / 复制）需 admin，提交会被服务端 403 拒绝。
        </Paper>
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
