import { useCallback, useEffect, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'

import { useAuth } from '../../app/AuthContext'
import { CopyButton } from '../../components/CopyButton'
import DeployDialog from '../../components/DeployDialog'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import SetMeUpDialog from '../../components/SetMeUpDialog'
import { Skeleton } from '../../components/Skeleton'
import { ApiError } from '../../lib/api'
import type { RepoListItem } from '../../lib/api'
import { canAdminWrite, isReadOnlyAdmin } from '../../lib/api'
import { cfgStr, cfgStrList, getRepositoriesFiltered, getUsageBatch } from '../../lib/repos'
import type { RepoUsageRow, RClass } from '../../lib/repos'
import { formatBytes, formatCount } from '../../lib/format'
import { onTableRowKeys, onTablistKeys } from '../../lib/keys'
import { useAsync } from '../../lib/useAsync'

import './repositories.css'
import { useRepoDelete } from './RepoDeleteConfirm'

// 仓库管理列表（console-m8 §6.6 / reverse §3.4，T-240 重排）：
//
// - 三 Tab = 子路由（/admin/repositories/{local|remote|virtual}），Tab +
//   「N 个仓库」计数 + 右上「添加仓库」+ 行 hover 删除图标 + 列头排序 +
//   底部计数行——Artifactory 仓库列表的操作骨架。
// - 列集：key（mono 链接 + 拷贝）/ 类型 / 包类型 / 上游或成员 / 已用 /
//   描述。契约缺口（沿 T-99 登记）：列表项不带节点计数与更新时间 →
//   「制品/缓存」「更新时间」两列不呈现；remote assumed-offline 无状态
//   端点（ux R9）→ 不伪造。已用列 = usage 批量端点**单请求注水**（T-258，
//   E1 GET /api/v1/storage/usage?include=counts——N 仓 N 请求的扇出退役
//   为整页 1 趟；E1 counts 的 nodeCount 以行 tooltip 呈现，不破坏既有列集；
//   updatedAt 是配置时刻，不当「最新制品时间」用）；virtual 无自身内容
//   恒 —；批量缺行/失败降级 — 不显示 0。
// - 门（router.go 实测）：GET /api/repositories = CapRepoRead——admin 与
//   readonly_admin 全量（T-236 定案），普通 user 403 → L2 无权限卡。
//   写入口（添加/删除）仅全量 admin（L4 预收敛，服务端 403 兜底）。
// - 排序：key / 包类型前端列头排序（asc → desc → none 循环，§4.7）；
//   已用列数据行级异步到达，不参与排序。

const TYPE_LABEL: Record<string, string> = { local: 'Local', remote: 'Remote', virtual: 'Virtual' }
const PKG_LABEL: Record<string, string> = {
  generic: 'Generic',
  docker: 'Docker',
  maven: 'Maven',
  npm: 'npm',
  pypi: 'PyPI',
}

const TABS: { id: RClass; label: string }[] = [
  { id: 'local', label: 'Local' },
  { id: 'remote', label: 'Remote' },
  { id: 'virtual', label: 'Virtual' },
]

/** 当前 Tab 自子路由段推导（本组件只挂在三条静态 Tab 路由上） */
function tabFromPath(pathname: string): RClass {
  const seg = pathname.split('/').filter(Boolean).pop() ?? ''
  return seg === 'remote' || seg === 'virtual' ? (seg as RClass) : 'local'
}

type SortDir = 'asc' | 'desc'

function truncate(s: string, max = 36): string {
  return s.length > max ? `${s.slice(0, max - 1)}…` : s
}

/** usage 批量索引：repo key → 行（T-258 单请求注水的内存消费面） */
type UsageIndex = Map<string, RepoUsageRow>

interface UsageState {
  status: 'loading' | 'ok' | 'error'
  index: UsageIndex | null
  error: ApiError | null
  /** 失败态行内重试：重跑一次批量请求 */
  reload: () => void
}

/**
 * 已用列批量注水（T-258，E1）：整页恰一次 GET /api/v1/storage/usage?
 * include=counts——排序/筛选是纯前端态，永不触发再请求（~171 请求 → ≤3
 * 的 usage 腿）。仅列表 ok 后发起：列表 403（普通 user 的 L2 无权限卡）/
 * 失败时无行可注水，不发孤儿请求；列表重载（删除/上传收尾）走
 * loading→ok 变迁，用量随之一并刷新（不另做跨挂载缓存——上传后陈旧值
 * 比多一趟请求更糟）。
 */
function useUsageBatch(enabled: boolean): UsageState {
  const [tick, setTick] = useState(0)
  const [state, setState] = useState<Omit<UsageState, 'reload'>>({
    status: 'loading',
    index: null,
    error: null,
  })

  useEffect(() => {
    if (!enabled) return
    // 闭包 alive 旗标（useAsync 同款）：卸载/禁用后晚到响应丢弃
    let alive = true
    setState({ status: 'loading', index: null, error: null })
    getUsageBatch(true)
      .then((rows) => {
        if (alive)
          setState({ status: 'ok', index: new Map(rows.map((r) => [r.repo, r])), error: null })
      })
      .catch((err: unknown) => {
        if (!alive) return
        setState({
          status: 'error',
          index: null,
          error: err instanceof ApiError ? err : new ApiError(0, String(err)),
        })
      })
    return () => {
      alive = false
    }
  }, [enabled, tick])

  const reload = useCallback(() => setTick((t) => t + 1), [])
  return { ...state, reload }
}

/**
 * 行内已用列（T-258）：状态由页面级单请求承载，行只查索引。四态：
 * ok = mono 值 + 文件数 tooltip（E1 counts，替代 T-99 登记的「制品/缓存」
 * 列缺口的信息面——tooltip 承载，不破坏既有列集）；加载 = 暂 `—`；
 * 失败 = 灰显 `—` + tooltip 原因、点击重试（键盘可达）；virtual 恒 `—`
 * （无自身内容）；批量缺行 = `—`（不伪造 0）。
 */
function UsageCell({ repoKey, rclass, usage }: { repoKey: string; rclass: string; usage: UsageState }) {
  const testid = `repos-usage-${repoKey}`
  if (rclass === 'virtual') {
    return (
      <span className="text-muted" data-testid={testid}>
        —
      </span>
    )
  }
  if (usage.status === 'loading') {
    return (
      <span className="text-muted" data-testid={testid}>
        —
      </span>
    )
  }
  if (usage.status === 'error') {
    return (
      <button
        type="button"
        className="usage-failed"
        data-testid={testid}
        title={`${usage.error?.message ?? '用量不可用'}（点击重试）`}
        aria-label={`仓库 ${repoKey} 用量加载失败，点击重试`}
        onClick={(e) => {
          // 行点击是导航——重试不得冒泡（CopyButton 隔离层同款）
          e.stopPropagation()
          usage.reload()
        }}
      >
        —
      </button>
    )
  }
  const row = usage.index?.get(repoKey)
  if (!row) {
    return (
      <span className="text-muted" data-testid={testid}>
        —
      </span>
    )
  }
  return (
    <span
      className="mono"
      data-testid={testid}
      title={row.nodeCount !== undefined ? `${formatCount(row.nodeCount)} 个文件` : undefined}
    >
      {formatBytes(row.usedBytes)}
    </span>
  )
}

function UpstreamCell({ repo }: { repo: RepoListItem }) {
  if (repo.type === 'remote') {
    const url = cfgStr(repo.configuration, 'url')
    if (!url) return <span className="text-muted">—</span>
    return (
      <span className="mono" title={url} lang="en">
        {truncate(url)}
      </span>
    )
  }
  if (repo.type === 'virtual') {
    const members = cfgStrList(repo.configuration, 'repositories')
    if (members.length === 0) return <span className="text-muted">—</span>
    // review B1：details 点击不得冒泡到 tr 的行导航——否则浮层刚开即被换页
    return (
      <details className="member-pop" onClick={(e) => e.stopPropagation()}>
        <summary>{members.length} 成员</summary>
        <div className="pop">
          <ol className="mono">
            {members.map((m) => (
              <li key={m} lang="en">
                {m}
              </li>
            ))}
          </ol>
          <div className="text-muted" style={{ fontSize: 11, marginTop: 4 }}>
            按声明序（优先解析成员在前由其自身配置标记）
          </div>
        </div>
      </details>
    )
  }
  return <span className="text-muted">—</span>
}

/** 列头排序（§4.7 循环 none → asc → desc → none；T-237 表格基准同款） */
function SortTh({
  label,
  active,
  dir,
  onToggle,
  testid,
}: {
  label: string
  active: boolean
  dir: SortDir
  onToggle: () => void
  testid?: string
}) {
  return (
    // 点击承载在 th 上（热区 = 整格；内部 button 的 click 冒泡到 th，
    // 键盘 Enter/Space 仍经 button 触发——同一冒泡路径，不双发）
    <th
      scope="col"
      aria-sort={active ? (dir === 'asc' ? 'ascending' : 'descending') : 'none'}
      data-testid={testid}
      onClick={onToggle}
    >
      <button type="button" className="th-sort">
        {label}
        <span className="th-sort-arrow" aria-hidden="true">
          {active ? (dir === 'asc' ? ' ▲' : ' ▼') : ''}
        </span>
      </button>
    </th>
  )
}

export default function RepositoriesPage() {
  const { session } = useAuth()
  const admin = canAdminWrite(session)
  const readOnly = isReadOnlyAdmin(session)
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const tab = tabFromPath(pathname)

  const [keyQuery, setKeyQuery] = useState('')
  // Tab = 服务端 ?type= 过滤（E-04 契约形态）；key 是已加载集上的前端子串
  const state = useAsync(() => getRepositoriesFiltered(tab, ''), [tab])
  const reload = state.reload
  // 已用列注水（T-258）：仅列表 ok 后发一次批量；排序/筛选（纯前端态）零触发
  const usage = useUsageBatch(state.status === 'ok')

  const requestDelete = useRepoDelete({ onDeleted: reload })

  // 对话框族（T-242）：行内 Set Me Up / Deploy 入口（dialog state 就地）
  const [smuKey, setSmuKey] = useState<string | null>(null)
  const [deployKey, setDeployKey] = useState<string | null>(null)

  const [sortKey, setSortKey] = useState<'key' | 'package' | null>(null)
  const [sortDir, setSortDir] = useState<SortDir>('asc')
  const toggleSort = (k: 'key' | 'package') => {
    if (sortKey !== k) {
      setSortKey(k)
      setSortDir('asc')
    } else if (sortDir === 'asc') {
      setSortDir('desc')
    } else {
      setSortKey(null)
      setSortDir('asc')
    }
  }

  const q = keyQuery.trim().toLowerCase()
  const rows = (state.data ?? []).filter((r) => (q ? r.key.toLowerCase().includes(q) : true))
  const sorted = [...rows].sort((a, b) => {
    if (!sortKey) return 0
    const va = sortKey === 'key' ? a.key : a.packageType
    const vb = sortKey === 'key' ? b.key : b.packageType
    if (va === vb) return 0
    return ((va < vb ? -1 : 1) * (sortDir === 'asc' ? 1 : -1)) as number
  })

  return (
    <div data-testid="repos-page">
      <div className="page-header">
        <h2>仓库</h2>
        <div className="repos-head-actions">
          <span className="repos-head-count" data-testid="repos-count">
            {state.status === 'ok' ? `${rows.length} 个仓库` : '…'}
          </span>
          {admin && (
            <Link className="btn primary" to="/admin/repositories/new" data-testid="repos-create">
              ＋ 添加仓库
            </Link>
          )}
        </div>
      </div>

      {readOnly && (
        <p className="page-note" data-testid="repos-readonly-note">
          ⓘ 只读管理员（readonly_admin）视角：仓库清单与配置只读；创建/删除仓库与浏览器部署（Deploy）等写操作已禁用——服务端一律 403 兜底。
        </p>
      )}

      <nav
        className="repos-tabs"
        aria-label="仓库类型"
        onKeyDown={(e) => onTablistKeys(e, TABS.map((t) => t.id), tab, (id) => navigate(`/admin/repositories/${id}`))}
      >
        {TABS.map((t) => (
          <Link
            key={t.id}
            to={`/admin/repositories/${t.id}`}
            className={`repos-tab${tab === t.id ? ' active' : ''}`}
            aria-current={tab === t.id ? 'page' : undefined}
            data-testid={`repos-tab-${t.id}`}
          >
            {t.label}
          </Link>
        ))}
      </nav>

      <div className="filter-bar">
        <input
          type="search"
          placeholder={`搜索 ${TYPE_LABEL[tab]} 仓 key…`}
          aria-label="搜索仓库 key"
          value={keyQuery}
          onChange={(e) => setKeyQuery(e.target.value)}
          data-testid="repos-filter-key"
        />
      </div>

      {state.status === 'loading' && <Skeleton lines={8} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'forbidden' && state.error && (
        <EmptyState
          message="无权限查看仓库列表"
          hint="仓库清单是管理面读端点（admin 与只读管理员可见）。制品访问请使用制品浏览、搜索或仓库直链。"
        />
      )}
      {state.status === 'ok' &&
        (rows.length === 0 ? (
          q !== '' ? (
            <EmptyState
              message={`无匹配的仓库（「${keyQuery}」）`}
              action={
                <button
                  type="button"
                  className="btn"
                  onClick={() => {
                    setKeyQuery('')
                  }}
                >
                  清除过滤
                </button>
              }
              testid="repos-empty-filtered"
            />
          ) : admin ? (
            <EmptyState
              message={`还没有 ${TYPE_LABEL[tab]} 仓库`}
              action={
                <Link className="btn primary" to={`/admin/repositories/new${tab !== 'local' ? `?rclass=${tab}` : ''}`}>
                  创建第一个{TYPE_LABEL[tab]}仓库
                </Link>
              }
              hint={
                tab === 'local'
                  ? '建议从 generic 起步（适配任意文件；协议仓按客户端接入文档选型）'
                  : tab === 'remote'
                    ? 'Remote 仓代理上游（如 repo1.maven.org），制品按需缓存'
                    : 'Virtual 仓聚合多个 local/remote 成员，统一团队出口'
              }
            />
          ) : (
            <EmptyState message={`还没有 ${TYPE_LABEL[tab]} 仓库`} hint="仓库由管理员创建" />
          )
        ) : (
          <>
            <table className="table" data-testid="repos-table">
              <thead>
                <tr>
                  <SortTh label="Repository Key" active={sortKey === 'key'} dir={sortDir} onToggle={() => toggleSort('key')} testid="repos-sort-key" />
                  <SortTh label="包类型" active={sortKey === 'package'} dir={sortDir} onToggle={() => toggleSort('package')} />
                  <th scope="col">类型</th>
                  <th scope="col">上游 / 成员</th>
                  <th scope="col">已用</th>
                  <th scope="col">描述</th>
                  <th scope="col" className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>
                    操作
                  </th>
                </tr>
              </thead>
              <tbody>
                {sorted.map((repo) => (
                  <tr
                    key={repo.key}
                    style={{ cursor: 'pointer' }}
                    tabIndex={0}
                    onClick={() => navigate(`/admin/repositories/${repo.key}`)}
                    onKeyDown={(e) => onTableRowKeys(e, () => navigate(`/admin/repositories/${repo.key}`))}
                  >
                    <td>
                      <Link
                        className="row-link mono"
                        to={`/admin/repositories/${repo.key}`}
                        onClick={(e) => e.stopPropagation()}
                        lang="en"
                      >
                        {repo.key}
                      </Link>{' '}
                      {/* review B1：拷贝按钮包隔离层（点击/键盘触发都不触发行导航） */}
                      <span onClick={(e) => e.stopPropagation()}>
                        <CopyButton value={repo.key} label={`仓库 key ${repo.key}`} />
                      </span>
                    </td>
                    <td>
                      <span className="badge neutral">{PKG_LABEL[repo.packageType] ?? repo.packageType}</span>
                    </td>
                    <td>
                      <span className="badge neutral">{TYPE_LABEL[repo.type] ?? repo.type}</span>
                    </td>
                    <td>
                      <UpstreamCell repo={repo} />
                    </td>
                    <td>
                      <UsageCell repoKey={repo.key} rclass={repo.type} usage={usage} />
                    </td>
                    <td className="wrap" style={{ maxWidth: 260, color: 'var(--bf-text-2)' }}>
                      {repo.description || '—'}
                    </td>
                    <td onClick={(e) => e.stopPropagation()}>
                        <button
                          type="button"
                          className="btn"
                          title={`Set Me Up：${repo.key} 的客户端接入向导`}
                          onClick={() => setSmuKey(repo.key)}
                        >
                          Set Me Up
                        </button>{' '}
                        {repo.type === 'local' && (repo.packageType === 'generic' || repo.packageType === 'maven') && (
                          <button
                            type="button"
                            className="btn"
                            disabled={readOnly}
                            title={
                              readOnly
                                ? '只读管理员不可写（服务端 403 兜底）'
                                : `部署到 ${repo.key}（浏览器上传）`
                            }
                            onClick={() => setDeployKey(repo.key)}
                          >
                            部署
                          </button>
                        )}{' '}
                        {admin && (
                          <button
                            type="button"
                            className="row-del"
                            aria-label={`删除仓库 ${repo.key}`}
                            title={`删除仓库 ${repo.key}`}
                            onClick={(e) => {
                              e.stopPropagation()
                              requestDelete({ key: repo.key, rclass: repo.type, packageType: repo.packageType })
                            }}
                          >
                            删除
                          </button>
                        )}
                      </td>
                  </tr>
                ))}
              </tbody>
            </table>
            <p className="table-foot" data-testid="repos-pager">
              显示 {sorted.length === 0 ? 0 : 1} – {sorted.length} / 共 {sorted.length} 项
              {q !== '' && `（按「${keyQuery}」过滤）`}
            </p>
          </>
        ))}

      {smuKey && <SetMeUpDialog preselectedRepo={smuKey} onClose={() => setSmuKey(null)} />}
      {deployKey && (
        <DeployDialog preselectedRepo={deployKey} onClose={() => setDeployKey(null)} onUploaded={reload} />
      )}
    </div>
  )
}
