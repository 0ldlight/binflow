import { Link, useNavigate } from 'react-router-dom'

import { useAuth } from '../../app/AuthContext'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { canAdminWrite, isReadOnlyAdmin } from '../../lib/api'
import { onTableRowKeys } from '../../lib/keys'
import { useAsync } from '../../lib/useAsync'
import './security.css'
import { listPermissionTargets } from './api'
import type { PermissionTarget } from './api'
import { SortTh, applySort, useTableSort } from './widgets'

// 权限 target 列表（T-241 重排，console-m8 §6.11 / reverse §3.8）：
// 基准表格形态——「New Permission」入口（页头主按钮，L4 仅 admin 渲染）+
// 列头排序 + 底部计数行；列集 = Permission Name │ 仓库数 │ patterns 数 │
// 用户数 │ 组数（对齐 §6.11 线框；仓库/pattern 计数带 title 悬浮明细）。
// manage 徽章（任务项 1）：target 的任一主体行（用户或组）携带 manage 即
// 徽章——与组页 group-manage-badge 同构（数据源 = 列表回显，零新端点）。
// GET 回显无时间戳字段，更新时间列不呈现（登记漂移，T-101 起沿用）。
//
// readonly_admin（M7 FR-66）：列表可见（security:read），创建入口退场 +
// 只读注记；普通 user 直链 → 403 驱动 L2（覆盖集边界说明见 hint）。

/** 任一主体行携带 manage 即徽章（manage = 仓库配置派生权，ADR-0026） */
function holdsManage(t: PermissionTarget): boolean {
  return (
    Object.values(t.principals.users).some((a) => a.includes('manage')) ||
    Object.values(t.principals.groups).some((a) => a.includes('manage'))
  )
}

type SortKey = 'name' | 'repos' | 'patterns' | 'users' | 'groups'

export default function PermissionsPage() {
  const { session } = useAuth()
  const admin = canAdminWrite(session)
  const readOnly = isReadOnlyAdmin(session)
  const navigate = useNavigate()
  const state = useAsync(listPermissionTargets, [])
  const { sort, toggle } = useTableSort<SortKey>({ key: 'name', dir: 'asc' })

  const rows = state.data ?? []
  const sorted = applySort(rows, sort, (t) => {
    switch (sort.key) {
      case 'repos':
        return t.repos.length
      case 'patterns':
        return t.includePatterns.length + t.excludePatterns.length
      case 'users':
        return Object.keys(t.principals.users).length
      case 'groups':
        return Object.keys(t.principals.groups).length
      default:
        return t.name
    }
  })

  return (
    <div data-testid="perms-page">
      <div className="page-header">
        <h2>权限</h2>
        {admin && (
          <Link className="btn primary" to="/admin/security/permissions/new" data-testid="perms-create">
            ＋ 新建权限
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
      {state.status === 'forbidden' && (
        <EmptyState
          message="无权限访问权限管理"
          hint="permission target 列表是 security:read 管理面读端点（admin / readonly_admin）。若你持有 manage（仓库配置派生权），仍可经 API 在 manage 覆盖集内维护 target（覆盖集外服务端 403）。"
        />
      )}
      {state.status === 'ok' &&
        (sorted.length === 0 ? (
          admin ? (
            <EmptyState
              message="还没有 permission target"
              hint="target = 仓库 × 路径 pattern × 主体（用户/组）× 动作（read/write/delete/manage）；授权并集、即时生效。"
              action={
                <Link className="btn primary" to="/admin/security/permissions/new">
                  创建第一个 target
                </Link>
              }
            />
          ) : (
            <EmptyState message="还没有 permission target" />
          )
        ) : (
          <>
            <table className="table" data-testid="perms-table">
              <thead>
                <tr>
                  <SortTh label="权限名" sortKey="name" sort={sort} onToggle={toggle} testid="perms-sort-name" />
                  <SortTh label="仓库数" sortKey="repos" sort={sort} onToggle={toggle} testid="perms-sort-repos" />
                  <SortTh label="patterns" sortKey="patterns" sort={sort} onToggle={toggle} testid="perms-sort-patterns" />
                  <SortTh label="用户数" sortKey="users" sort={sort} onToggle={toggle} testid="perms-sort-users" />
                  <SortTh label="组数" sortKey="groups" sort={sort} onToggle={toggle} testid="perms-sort-groups" />
                </tr>
              </thead>
              <tbody>
                {sorted.map((t) => (
                  <tr
                    key={t.name}
                    data-testid={`perm-row-${t.name}`}
                    style={{ cursor: 'pointer' }}
                    tabIndex={0}
                    onClick={() => navigate(`/admin/security/permissions/${encodeURIComponent(t.name)}`)}
                    onKeyDown={(e) =>
                      onTableRowKeys(e, () =>
                        navigate(`/admin/security/permissions/${encodeURIComponent(t.name)}`),
                      )
                    }
                  >
                    <td>
                      <span className="cell-inline">
                        <Link
                          className="row-link mono"
                          to={`/admin/security/permissions/${encodeURIComponent(t.name)}`}
                          onClick={(e) => e.stopPropagation()}
                          lang="en"
                        >
                          {t.name}
                        </Link>
                        {holdsManage(t) && (
                          <span
                            className="badge neutral mono"
                            lang="en"
                            title="该 target 的某主体行携带 manage（仓库配置派生权；不隐含读写删）"
                            data-testid={`perm-manage-badge-${t.name}`}
                          >
                            manage
                          </span>
                        )}
                        <CopyButton value={t.name} label={`target ${t.name}`} />
                      </span>
                    </td>
                    <td>
                      <span className="text-2" title={t.repos.join(', ')}>
                        {t.repos.length}
                      </span>
                    </td>
                    <td>
                      <span
                        className="mono"
                        title={`include: ${t.includePatterns.join(', ') || '（空 = 全部）'}\nexclude: ${t.excludePatterns.join(', ') || '（无）'}`}
                      >
                        +{t.includePatterns.length} / −{t.excludePatterns.length}
                      </span>
                    </td>
                    <td>
                      <span className="text-2">{Object.keys(t.principals.users).length}</span>
                    </td>
                    <td>
                      <span className="text-2">{Object.keys(t.principals.groups).length}</span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            <p className="table-foot" data-testid="perms-count">
              权限 target 总数： {sorted.length}
            </p>
          </>
        ))}
    </div>
  )
}
