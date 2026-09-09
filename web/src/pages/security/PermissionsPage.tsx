// 权限 target 列表（T-241 重排——P3 新栈重写）：Permission Name │ 仓库数 │
// patterns ± │ 用户数 │ 组数（列头排序 + 底部计数行）。manage 徽章 = 任一
// 主体行携带 manage。GET 回显无时间戳字段——更新时间列不呈现（登记漂移）。
// - 取数按身份分流（E6）：admin/readonly 全量；普通 user 走 ?filter=manage
//   （200 = m-holder 子集；403 = 覆盖集空 L2；200 空 = 友好空态）。
// 锚族原样：perms-page/perms-create/perms-table/perm-row-<name>/
// perm-manage-badge-<name>/perms-sort-name/perms-count/perms-readonly-note/
// perms-manage-note。
import { Link, useNavigate } from 'react-router-dom'

import { useAuth } from '@/app/AuthContext'
import { ButtonAsChild } from '@/components/ui/button'
import { Badge } from '@/components/layout/bits'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { Pager, useClientPager } from '@/components/layout/pager'
import { SortTh, applySort, useTableSort } from '@/components/layout/table'
import { canAdminWrite, isReadOnlyAdmin, normalizeAdminRole } from '@/lib/api'
import { onTableRowKeys } from '@/lib/keys'
import { useAsync } from '@/lib/useAsync'
import './security.css'
import { listPermissionTargets, listPermissionTargetsManaged } from './api'
import type { PermissionTarget } from './api'
import { tr } from '@/i18n'

const tt = tr('security')

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
  // 普通 user = 潜在 m-holder：是否真持有 manage 由 filter=manage 的
  // 200/403 事实判定，UI 不预判
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
  const pageEpoch = `${state.status}|${sorted.length}|${sort.key ?? ''}|${sort.dir}`
  const pager = useClientPager(sorted.length, pageEpoch)
  const pageRows = pager.slice(sorted)

  return (
    <div data-testid="perms-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{tt('权限')}</h2>
        {admin && (
          <ButtonAsChild size="sm" className="ml-auto" data-testid="perms-create">
            <Link to="/admin/security/permissions/new">{tt('＋ 新建权限')}</Link>
          </ButtonAsChild>
        )}
      </div>

      {readOnly && (
        <p className="admin-note" data-testid="perms-readonly-note">
          {tt('ⓘ 只读管理员（readonly_admin）视角：授权矩阵只读；创建/编辑 target 是管理面写操作（服务端 403 兜底）。')}
        </p>
      )}

      {state.status === 'loading' && <StateSkeleton lines={5} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'forbidden' &&
        (mHolder ? (
          // 覆盖集空的「m-holder」（filter=manage 403，与无 filter 同形）
          <EmptyState
            message={tt('无管理范围内的权限目标')}
            hint={tt('manage 覆盖集由携带 manage 的 permission target 授予——当前会话覆盖集为空（服务端 403），无可在控制台维护的 target。若你刚获授权，请刷新本页。')}
          />
        ) : (
          <EmptyState
            message={tt('无权限访问权限管理')}
            hint={tt('permission target 列表是 security:read 管理面读端点（admin / readonly_admin）。')}
          />
        ))}
      {mHolder && state.status === 'ok' && sorted.length > 0 && (
        <p className="admin-note" data-testid="perms-manage-note">
          {tt('ⓘ 当前会话以 manage 持有者身份查看：仅列出引用仓库全部落在 manage 覆盖集内的 target（部分覆盖的由服务端隐藏）；编辑/删除由服务端按覆盖集终裁（越界 403）。')}
        </p>
      )}
      {state.status === 'ok' &&
        (sorted.length === 0 ? (
          admin ? (
            <EmptyState
              illustration
              message={tt('还没有 permission target')}
              hint={tt('target = 仓库 × 路径 pattern × 主体（用户/组）× 动作（read/write/delete/manage）；授权并集、即时生效。')}
              action={
                <ButtonAsChild size="sm">
                  <Link to="/admin/security/permissions/new">{tt('创建第一个 target')}</Link>
                </ButtonAsChild>
              }
            />
          ) : mHolder ? (
            <EmptyState
              message={tt('无管理范围内的权限目标')}
              hint={tt('manage 持有者可管理的 target 需满足：其引用的全部仓库都落在你的 manage 覆盖集内（覆盖集由携带 manage 的 permission target 授予）。若你刚获授 manage，请刷新本页。')}
            />
          ) : (
            <EmptyState illustration message={tt('还没有 permission target')} />
          )
        ) : (
          <>
            <table className="w-full text-dense" data-testid="perms-table">
              <thead>
                <tr className="border-b border-border text-left text-aux text-muted-foreground">
                  <SortTh label={tt('权限名')} sortKey="name" sort={sort} onToggle={toggle} testid="perms-sort-name" />
                  <SortTh label={tt('仓库数')} sortKey="repos" sort={sort} onToggle={toggle} />
                  <SortTh label="patterns" sortKey="patterns" sort={sort} onToggle={toggle} />
                  <SortTh label={tt('用户数')} sortKey="users" sort={sort} onToggle={toggle} />
                  <SortTh label={tt('组数')} sortKey="groups" sort={sort} onToggle={toggle} />
                </tr>
              </thead>
              <tbody>
                {pageRows.map((t) => (
                  <tr
                    key={t.name}
                    data-testid={`perm-row-${t.name}`}
                    className="cursor-pointer border-b border-border/60 hover:bg-accent"
                    tabIndex={0}
                    onClick={() => navigate(`/admin/security/permissions/${encodeURIComponent(t.name)}`)}
                    onKeyDown={(e) =>
                      onTableRowKeys(e, () => navigate(`/admin/security/permissions/${encodeURIComponent(t.name)}`))
                    }
                  >
                    <td className="px-3 py-1.5">
                      <span className="cell-inline">
                        <Link
                          className="row-link font-mono text-primary hover:underline"
                          to={`/admin/security/permissions/${encodeURIComponent(t.name)}`}
                          onClick={(e) => e.stopPropagation()}
                          lang="en"
                        >
                          {t.name}
                        </Link>
                        {holdsManage(t) && (
                          <Badge
                            mono
                            lang="en"
                            testid={`perm-manage-badge-${t.name}`}
                            title={tt('该 target 的某主体行携带 manage（仓库配置派生权；不隐含读写删）')}
                          >
                            manage
                          </Badge>
                        )}
                        <span onClick={(e) => e.stopPropagation()}>
                          <CopyButton value={t.name} label={`target ${t.name}`} />
                        </span>
                      </span>
                    </td>
                    <td className="px-3 py-1.5">
                      <span className="text-2" title={t.repos.join(', ')}>{t.repos.length}</span>
                    </td>
                    <td className="px-3 py-1.5">
                      <span
                        className="font-mono"
                        title={`include: ${t.includePatterns.join(', ') || tt('（空 = 全部）')}\nexclude: ${t.excludePatterns.join(', ') || tt('（无）')}`}
                      >
                        +{t.includePatterns.length} / −{t.excludePatterns.length}
                      </span>
                    </td>
                    <td className="px-3 py-1.5"><span className="text-2">{Object.keys(t.principals.users).length}</span></td>
                    <td className="px-3 py-1.5"><span className="text-2">{Object.keys(t.principals.groups).length}</span></td>
                  </tr>
                ))}
              </tbody>
            </table>
            <div className="table-foot" data-testid="perms-count">
              <Pager
                page={pager.page}
                pageCount={pager.pageCount}
                onPageChange={pager.setPage}
                from={pager.from}
                to={pager.to}
                total={sorted.length}
                note={mHolder ? tt('（管理范围内的权限 target）') : undefined}
                pageSize={pager.size}
                onPageSizeChange={pager.setSize}
              />
            </div>
          </>
        ))}
    </div>
  )
}
