import { Link, useNavigate } from 'react-router-dom'

import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'

import { useAuth } from '../../app/AuthContext'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { canAdminWrite, isReadOnlyAdmin, normalizeAdminRole } from '../../lib/api'
import { badgeChipSx } from '../../lib/muiAtoms'
import { onTableRowKeys } from '../../lib/keys'
import { useAsync } from '../../lib/useAsync'
import './security.css'
import { listPermissionTargets, listPermissionTargetsManaged } from './api'
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
// 只读注记。
//
// 取数路径按 principal 身份分流（T-259，E6）：admin/readonly 走全量
// （现状零改）；普通 user（潜在 m-holder，判定形态沿 security/repositories
// 域既有——role === 'user' 且取数通过即覆盖集内）走 `?filter=manage`：
// 200 = m-holder（覆盖集内 target 子集，L2 边界卡退役）；403（与无 filter
// 同形）= 无 manage / 覆盖集空 → L2 无权限卡保留；200 空数组 = 覆盖集非空
// 但无完全落入的 target → 友好空态。

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
  // 普通 user = 潜在 m-holder（AppShell 认证守卫保证 session 已就绪）：
  // 是否真持有 manage 由 filter=manage 的 200/403 事实判定，UI 不预判
  const mHolder = normalizeAdminRole(session?.adminRole, session?.admin ?? false) === 'user'
  const navigate = useNavigate()
  const state = useAsync(
    () => (mHolder ? listPermissionTargetsManaged() : listPermissionTargets()),
    [mHolder],
  )
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
          <Button
            variant="contained"
            size="small"
            component={Link}
            to="/admin/security/permissions/new"
            data-testid="perms-create"
          >
            ＋ 新建权限
          </Button>
        )}
      </div>

      {readOnly && (
        <p className="admin-note" data-testid="perms-readonly-note">
          ⓘ 只读管理员（readonly_admin）视角：授权矩阵只读；创建/编辑 target 是管理面写操作（服务端 403 兜底）。
        </p>
      )}

      {state.status === 'loading' && <Skeleton lines={5} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'forbidden' &&
        (mHolder ? (
          // 覆盖集空的「m-holder」（wire：filter=manage 403，与无 filter 同形
          // ——零新增可区分面）——L2 收敛保留（表格零渲染），文案为友好空态
          <EmptyState
            message="无管理范围内的权限目标"
            hint="manage 覆盖集由携带 manage 的 permission target 授予——当前会话覆盖集为空（服务端 403），无可在控制台维护的 target。若你刚获授权，请刷新本页。"
          />
        ) : (
          <EmptyState
            message="无权限访问权限管理"
            hint="permission target 列表是 security:read 管理面读端点（admin / readonly_admin）。"
          />
        ))}
      {mHolder && state.status === 'ok' && sorted.length > 0 && (
        <p className="admin-note" data-testid="perms-manage-note">
          ⓘ 当前会话以 manage 持有者身份查看：仅列出引用仓库全部落在 manage 覆盖集内的 target（部分覆盖的由服务端隐藏）；编辑/删除由服务端按覆盖集终裁（越界 403）。
        </p>
      )}
      {state.status === 'ok' &&
        (sorted.length === 0 ? (
          admin ? (
            <EmptyState
              message="还没有 permission target"
              hint="target = 仓库 × 路径 pattern × 主体（用户/组）× 动作（read/write/delete/manage）；授权并集、即时生效。"
              action={
                <Button variant="contained" size="small" component={Link} to="/admin/security/permissions/new">
                  创建第一个 target
                </Button>
              }
            />
          ) : mHolder ? (
            // 200 空数组分支：构造上不可达（覆盖集非空 ⟹ 授 manage 的 target
            // 自身已全落入覆盖集），纯防御呈现——与 403 空集同文案族
            <EmptyState
              message="无管理范围内的权限目标"
              hint="manage 持有者可管理的 target 需满足：其引用的全部仓库都落在你的 manage 覆盖集内（覆盖集由携带 manage 的 permission target 授予）。若你刚获授 manage，请刷新本页。"
            />
          ) : (
            <EmptyState message="还没有 permission target" />
          )
        ) : (
          <>
            <Table className="table" data-testid="perms-table">
              <TableHead>
                <TableRow>
                  <SortTh label="权限名" sortKey="name" sort={sort} onToggle={toggle} testid="perms-sort-name" />
                  <SortTh label="仓库数" sortKey="repos" sort={sort} onToggle={toggle} />
                  <SortTh label="patterns" sortKey="patterns" sort={sort} onToggle={toggle} />
                  <SortTh label="用户数" sortKey="users" sort={sort} onToggle={toggle} />
                  <SortTh label="组数" sortKey="groups" sort={sort} onToggle={toggle} />
                </TableRow>
              </TableHead>
              <TableBody>
                {sorted.map((t) => (
                  <TableRow
                    key={t.name}
                    data-testid={`perm-row-${t.name}`}
                    sx={{ cursor: 'pointer' }}
                    tabIndex={0}
                    onClick={() => navigate(`/admin/security/permissions/${encodeURIComponent(t.name)}`)}
                    onKeyDown={(e) =>
                      onTableRowKeys(e, () =>
                        navigate(`/admin/security/permissions/${encodeURIComponent(t.name)}`),
                      )
                    }
                  >
                    <TableCell>
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
                          <Chip
                            size="small"
                            className="badge neutral mono"
                            label="manage"
                            sx={badgeChipSx}
                            lang="en"
                            data-testid={`perm-manage-badge-${t.name}`}
                            title="该 target 的某主体行携带 manage（仓库配置派生权；不隐含读写删）"
                          />
                        )}
                        <CopyButton value={t.name} label={`target ${t.name}`} />
                      </span>
                    </TableCell>
                    <TableCell>
                      <span className="text-2" title={t.repos.join(', ')}>
                        {t.repos.length}
                      </span>
                    </TableCell>
                    <TableCell>
                      <span
                        className="mono"
                        title={`include: ${t.includePatterns.join(', ') || '（空 = 全部）'}\nexclude: ${t.excludePatterns.join(', ') || '（无）'}`}
                      >
                        +{t.includePatterns.length} / −{t.excludePatterns.length}
                      </span>
                    </TableCell>
                    <TableCell>
                      <span className="text-2">{Object.keys(t.principals.users).length}</span>
                    </TableCell>
                    <TableCell>
                      <span className="text-2">{Object.keys(t.principals.groups).length}</span>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <p className="table-foot" data-testid="perms-count">
              {mHolder ? '管理范围内的权限 target：' : '权限 target 总数：'} {sorted.length}
            </p>
          </>
        ))}
    </div>
  )
}
