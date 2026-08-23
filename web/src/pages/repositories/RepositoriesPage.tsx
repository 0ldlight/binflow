import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { useAuth } from '../../app/AuthContext'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import type { RepoListItem } from '../../lib/api'
import { isReadOnlyAdmin } from '../../lib/api'
import { cfgStr, cfgStrList, getRepositoriesFiltered, getRepoUsage } from '../../lib/repos'
import { PACKAGE_TYPES, RCLASSES } from '../../lib/repos'
import { formatBytes } from '../../lib/format'
import { useAsync } from '../../lib/useAsync'

// 仓库列表（console-ux §4.3）：key（mono 主链接 + 拷贝）/ 类型 / 包类型 /
// 上游或成员 / 已用。四态（§5.3）：骨架 8 行、两种空态（从未有数据→建仓
// CTA；过滤后为空→清除过滤）、错误卡 + 重试。
//
// 契约缺口（见工作日志）：列表项不带 node 计数与更新时间（repoListItem
// 无此字段）→ 线框「制品/缓存」「更新时间」两列不呈现；remote assumed-
// offline 标记无状态端点（ux R9）→ 不伪造。已用列走 usage 端点逐仓拉取
//（量小；virtual 无自身内容恒 —，403/错误降级 — 不显示 0）。

const TYPE_LABEL: Record<string, string> = { local: 'Local', remote: 'Remote', virtual: 'Virtual' }
const PKG_LABEL: Record<string, string> = {
  generic: 'Generic',
  docker: 'Docker',
  maven: 'Maven',
  npm: 'npm',
  pypi: 'PyPI',
}

function truncate(s: string, max = 36): string {
  return s.length > max ? `${s.slice(0, max - 1)}…` : s
}

/** 行内已用列：独立请求独立到达（§5.2 卡片级思路的行级版） */
function UsageCell({ repoKey, rclass }: { repoKey: string; rclass: string }) {
  const state = useAsync(() => getRepoUsage(repoKey), [repoKey])
  if (rclass === 'virtual') return <span className="text-muted">—</span>
  if (state.status === 'loading') {
    return <span className="cell-pending" role="progressbar" aria-label="用量加载中" />
  }
  if (state.status !== 'ok' || !state.data) {
    return (
      <span className="text-muted" title={state.error?.message ?? '用量不可用'}>
        —
      </span>
    )
  }
  return <span className="mono">{formatBytes(state.data.usedBytes)}</span>
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
        <summary>
          {members.length} 成员
        </summary>
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

export default function RepositoriesPage() {
  const { session } = useAuth()
  const admin = session?.admin ?? false
  const readOnly = isReadOnlyAdmin(session)
  const navigate = useNavigate()

  const [typeFilter, setTypeFilter] = useState('')
  const [pkgFilter, setPkgFilter] = useState('')
  const [keyQuery, setKeyQuery] = useState('')

  // 类型/包类型走服务端过滤（E-04 ?type=&packageType=，契约形态）；
  // key 搜索是已加载集上的前端子串（仓库量级小，无需服务端面）
  const state = useAsync(() => getRepositoriesFiltered(typeFilter, pkgFilter), [typeFilter, pkgFilter])

  const q = keyQuery.trim().toLowerCase()
  const rows = (state.data ?? []).filter((r) => (q ? r.key.toLowerCase().includes(q) : true))
  const filtered = typeFilter !== '' || pkgFilter !== '' || q !== ''

  return (
    <div data-testid="repos-page">
      <div className="page-header">
        <h2>仓库</h2>
        {admin && (
          <Link className="btn primary" to="/repositories/new" data-testid="repos-create">
            ＋ 创建仓库
          </Link>
        )}
      </div>

      {readOnly && (
        <p className="admin-note" data-testid="repos-readonly-note">
          ⓘ 只读管理员（readonly_admin）视角：仓库配置与制品只读；创建/删除仓库与写操作是管理面写
          （repo:write，服务端 403 兜底）。
        </p>
      )}

      <div className="filter-bar">
        <input
          type="search"
          placeholder="搜索 key…"
          aria-label="搜索仓库 key"
          value={keyQuery}
          onChange={(e) => setKeyQuery(e.target.value)}
          data-testid="repos-filter-key"
        />
        <select
          aria-label="按类型过滤"
          value={typeFilter}
          onChange={(e) => setTypeFilter(e.target.value)}
          data-testid="repos-filter-type"
        >
          <option value="">类型：全部</option>
          {RCLASSES.map((t) => (
            <option key={t} value={t}>
              {TYPE_LABEL[t]}
            </option>
          ))}
        </select>
        <select
          aria-label="按包类型过滤"
          value={pkgFilter}
          onChange={(e) => setPkgFilter(e.target.value)}
          data-testid="repos-filter-package"
        >
          <option value="">包类型：全部</option>
          {PACKAGE_TYPES.map((t) => (
            <option key={t} value={t}>
              {PKG_LABEL[t]}
            </option>
          ))}
        </select>
        <span className="count" data-testid="repos-count">
          共 {rows.length} 个仓库
        </span>
      </div>

      {state.status === 'loading' && <Skeleton lines={8} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'forbidden' && state.error && (
        <EmptyState message="无权限查看仓库列表" hint={state.error.message} />
      )}
      {state.status === 'ok' &&
        (rows.length === 0 ? (
          filtered ? (
            <EmptyState
              message={`无匹配的仓库（${keyQuery ? `「${keyQuery}」` : ''}${typeFilter ? ` ${TYPE_LABEL[typeFilter]}` : ''}${pkgFilter ? ` ${PKG_LABEL[pkgFilter]}` : ''}）`}
              action={
                <button
                  type="button"
                  className="btn"
                  onClick={() => {
                    setTypeFilter('')
                    setPkgFilter('')
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
              message="还没有仓库"
              action={
                <Link className="btn primary" to="/repositories/new">
                  创建第一个仓库
                </Link>
              }
              hint="建议从 local 仓开始（generic 适配任意文件；协议仓按客户端接入文档选型）"
              testid="repos-empty"
            />
          ) : (
            <EmptyState message="还没有仓库" hint="仓库由管理员创建" testid="repos-empty" />
          )
        ) : (
          <table className="table" data-testid="repos-table">
            <thead>
              <tr>
                <th scope="col">key</th>
                <th scope="col">类型</th>
                <th scope="col">包类型</th>
                <th scope="col">上游 / 成员</th>
                <th scope="col">已用</th>
                <th scope="col">描述</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((repo) => (
                <tr
                  key={repo.key}
                  data-testid={`repos-row-${repo.key}`}
                  style={{ cursor: 'pointer' }}
                  onClick={() => navigate(`/repositories/${repo.key}`)}
                >
                  <td>
                    <Link
                      className="row-link mono"
                      to={`/repositories/${repo.key}`}
                      onClick={(e) => e.stopPropagation()}
                      lang="en"
                    >
                      {repo.key}
                    </Link>{' '}
                    {/* review B1：拷贝按钮包隔离层（页面级，不动共享 CopyButton——
                        T-101/T-102 并行在途），点击/键盘触发都不再触发行导航 */}
                    <span onClick={(e) => e.stopPropagation()}>
                      <CopyButton value={repo.key} label={`仓库 key ${repo.key}`} />
                    </span>
                  </td>
                  <td>
                    <span className="badge neutral">{TYPE_LABEL[repo.type] ?? repo.type}</span>
                  </td>
                  <td>
                    <span className="badge neutral">{PKG_LABEL[repo.packageType] ?? repo.packageType}</span>
                  </td>
                  <td>
                    <UpstreamCell repo={repo} />
                  </td>
                  <td>
                    <UsageCell repoKey={repo.key} rclass={repo.type} />
                  </td>
                  <td className="wrap" style={{ maxWidth: 260, color: 'var(--bf-text-2)' }}>
                    {repo.description || '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ))}
    </div>
  )
}
