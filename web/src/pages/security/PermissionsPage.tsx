import { Link, useNavigate } from 'react-router-dom'

import { useAuth } from '../../app/AuthContext'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { isReadOnlyAdmin } from '../../lib/api'
import { useAsync } from '../../lib/useAsync'
import './security.css'
import { listPermissionTargets } from './api'

// 权限 target 列表（console-ux §4.9 列表行：name / 仓库数 / patterns 数 /
// 主体数 / 更新时间——GET /v1/permissions 回显无时间戳字段，该列不呈现，
// 登记漂移）。行点击进编辑器；建 target 入口在右上（L4：仅 admin 渲染）。
// readonly_admin（M7 FR-66）：列表可见（security:read），建 target 隐藏 + 只读注记。

export default function PermissionsPage() {
  const { session } = useAuth()
  const admin = session?.admin ?? false
  const readOnly = isReadOnlyAdmin(session)
  const navigate = useNavigate()
  const state = useAsync(listPermissionTargets, [])

  const rows = state.data ?? []
  const count = (t: (typeof rows)[number], key: 'users' | 'groups') => Object.keys(t.principals[key]).length

  return (
    <div data-testid="perms-page">
      <div className="page-header">
        <h2>权限</h2>
        {admin && (
          <Link className="btn primary" to="/security/permissions/new" data-testid="perms-create">
            ＋ 创建 target
          </Link>
        )}
      </div>

      {readOnly && (
        <p className="admin-note" data-testid="perms-readonly-note">
          ⓘ 只读管理员（readonly_admin）视角：授权矩阵只读；创建/编辑 target 是管理面写操作（服务端 403 兜底）。
        </p>
      )}

      {state.status === 'loading' && <Skeleton lines={5} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'forbidden' && state.error && (
        <EmptyState
          message="无权限访问权限管理"
          hint="permission target 是管理员配置（管理面需 admin）。"
        />
      )}
      {state.status === 'ok' &&
        (rows.length === 0 ? (
          admin ? (
            <EmptyState
              message="还没有 permission target"
              hint="target = 仓库 × 路径 pattern × 主体（用户/组）× 动作（read/write/delete/manage）；授权并集、即时生效。"
              action={
                <Link className="btn primary" to="/security/permissions/new">
                  创建第一个 target
                </Link>
              }
            />
          ) : (
            <EmptyState message="还没有 permission target" />
          )
        ) : (
          <table className="table" data-testid="perms-table">
            <thead>
              <tr>
                <th scope="col">name</th>
                <th scope="col">适用仓库</th>
                <th scope="col">patterns</th>
                <th scope="col">主体（用户 / 组）</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((t) => (
                <tr
                  key={t.name}
                  data-testid={`perm-row-${t.name}`}
                  style={{ cursor: 'pointer' }}
                  onClick={() => navigate(`/security/permissions/${encodeURIComponent(t.name)}`)}
                >
                  <td>
                    <Link
                      className="row-link mono"
                      to={`/security/permissions/${encodeURIComponent(t.name)}`}
                      onClick={(e) => e.stopPropagation()}
                      lang="en"
                    >
                      {t.name}
                    </Link>{' '}
                    <CopyButton value={t.name} label={`target ${t.name}`} />
                  </td>
                  <td className="wrap" style={{ maxWidth: 280 }}>
                    <span className="sec-chips">
                      {t.repos.map((r) => (
                        <span key={r} className="badge neutral mono" lang="en">
                          {r}
                        </span>
                      ))}
                    </span>
                  </td>
                  <td>
                    <span className="mono">
                      +{t.includePatterns.length} / −{t.excludePatterns.length}
                    </span>
                  </td>
                  <td>
                    <span className="mono">
                      {count(t, 'users')} / {count(t, 'groups')}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ))}
    </div>
  )
}
