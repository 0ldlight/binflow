// 用户列表（console-m8 §6.9——P3 新栈重写：轻量 table + 列选器 + 客户端
// 页窗；语义承接 audit §2.7 行为契约）：
// - 列集：Name │ Email │ Groups（计数 | 明细，"1 | readers" 形态）│ Role
//   （三值 badge）│ Status（E2 enabled 真值）│ Last Login（T-454 投影，
//   缺席 = 从未登录「—（尚未登录）」）│ 操作（admin）；
// - 数据源 = 单请求 GET /security/users（E2 加宽，无 N+1）；
// - 删除（E4）：行内 Delete（admin）——自删/内置 admin 预禁用（服务端 400
//   终裁兜底）；强确认 = 输入用户名（widgets.useUserDelete）；
// - 403 收敛（§3.6）：L2 无权限卡；L4 创建/删除仅 admin 渲染；
//   readonly_admin 读面全通 + users-readonly-note。
// 锚族原样：users-page/users-create/users-columns(-menu|-item-*|-reset)/
// users-table/user-row-<name>/user-status-<name>/user-delete-<name>/
// users-sort-*/users-count/users-readonly-note。
import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { useAuth } from '@/app/AuthContext'
import { Button } from '@/components/ui/button'
import { CopyButton } from '@/components/layout/copy-button'
import { Badge, StatusLabel } from '@/components/layout/bits'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { Pager, useClientPager } from '@/components/layout/pager'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { SortTh, applySort, useTableSort, Th } from '@/components/layout/table'
import { canAdminWrite, isReadOnlyAdmin, normalizeAdminRole } from '@/lib/api'
import type { AdminRole } from '@/lib/api'
import { useColumnPrefs } from '@/lib/columnPrefs'
import type { ColumnDef } from '@/lib/columnPrefs'
import { onTableRowKeys } from '@/lib/keys'
import { useAsync } from '@/lib/useAsync'
import './security.css'
import { useUserDelete } from './widgets'
import { listUsers } from './api'
import type { UserListItem } from './api'
import { tr } from '@/i18n'

const t = tr('security')

type UserSortKey = 'name' | 'email' | 'groups' | 'role' | 'status' | 'lastLogin'

/** 列选器列集 = 既有全部列闭集（T-414；label 与表头一致；anchor = 菜单项锚）。
 *  操作列仅 admin 在场——非 admin 视图该列与菜单项同步剔除。 */
const COLUMNS: ColumnDef[] = [
  { id: 'name', label: t('用户名'), anchor: 'users-columns-item-name' },
  { id: 'email', label: 'Email', anchor: 'users-columns-item-email' },
  { id: 'groups', label: t('组'), anchor: 'users-columns-item-groups' },
  { id: 'role', label: t('角色'), anchor: 'users-columns-item-role' },
  { id: 'status', label: 'Status', anchor: 'users-columns-item-status' },
  { id: 'lastLogin', label: t('最近登录'), anchor: 'users-columns-item-lastlogin' },
  { id: 'actions', label: t('操作'), anchor: 'users-columns-item-actions' },
]
const COLUMN_IDS = COLUMNS.map((c) => c.id)
const COLS_KEY = 'binflow-console-cols-users'

function RoleLabel({ role }: { role: AdminRole }) {
  // 语义 badge（admin = warning outlined 形态的语义对位——对比度登记 T-344D）
  if (role === 'admin') return <Badge variant="warning" mono lang="en" className="badge-warning-outlined">admin</Badge>
  if (role === 'readonly_admin') return <Badge mono lang="en">readonly_admin</Badge>
  return <Badge mono lang="en">user</Badge>
}

function roleBadge(item: UserListItem) {
  // E2 列表项无 admin 布尔（W40 禁）——adminRole 恒渲染，闭集外回退
  // false（fail-safe 不放大，normalizeAdminRole 同口径）
  return <RoleLabel role={normalizeAdminRole(item.adminRole, false)} />
}

export default function UsersPage() {
  const { session } = useAuth()
  const navigate = useNavigate()
  const admin = canAdminWrite(session)
  const readOnly = isReadOnlyAdmin(session)
  // 单请求（E2 加宽列表）——行模型 = 列表项本体，无逐用户详情扇出
  const state = useAsync(listUsers, [])
  const pageColumns = useMemo(() => (admin ? COLUMNS : COLUMNS.filter((c) => c.id !== 'actions')), [admin])
  const pageIds = useMemo(() => (admin ? COLUMN_IDS : COLUMN_IDS.filter((id) => id !== 'actions')), [admin])
  const cols = useColumnPrefs(pageIds, COLS_KEY)
  const [colsOpen, setColsOpen] = useState(false)
  const { sort, toggle } = useTableSort<UserSortKey>({ key: 'name', dir: 'asc' })
  const { openDialog: deleteUser } = useUserDelete({ onDeleted: () => state.reload() })

  const rows = applySort(state.data ?? [], sort, (r) => {
    switch (sort.key) {
      case 'email':
        return r.email
      case 'groups':
        return r.groups.length
      case 'role':
        return normalizeAdminRole(r.adminRole, false)
      case 'status':
        return r.enabled ? 1 : 0
      case 'lastLogin':
        // RFC3339 UTC 字典序 = 时间序（T-454 投影恒 UTC）；缺席 = ''
        return r.lastLoggedIn ?? ''
      default:
        return r.name
    }
  })
  // 客户端页窗（数据形态 × 排序变化即回落第 1 页；同形刷新不丢页位）
  const pageEpoch = `${state.status}|${rows.length}|${sort.key ?? ''}|${sort.dir}`
  const pager = useClientPager(rows.length, pageEpoch)
  const pageRows = pager.slice(rows)

  return (
    <div data-testid="users-page" className="flex flex-col gap-3">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{t('用户')}</h2>
        {admin && (
          <Button size="sm" className="ml-auto" onClick={() => navigate('/admin/security/users/new')} data-testid="users-create">
            {t('＋ 新建用户')}
          </Button>
        )}
      </div>

      {readOnly && (
        <p className="admin-note" data-testid="users-readonly-note">
          {t('ⓘ 只读管理员（readonly_admin）视角：用户与角色只读；创建/编辑是管理面写操作（服务端 403 兜底）。')}
        </p>
      )}

      {/* 列选器（filter-bar 尾组——本页无工具栏搜索面，用户数 = 实例账号规模） */}
      <div className="filter-bar">
        <span className="filter-tail-actions filter-tail-end ml-auto">
      {/* 列选器（T-387 L1 / T-414——内联形态：锚字面量对 anchor-audit 可见，
          P2 RepositoriesPage 同款） */}
      <Popover open={colsOpen} onOpenChange={setColsOpen}>
        <PopoverTrigger asChild>
          <Button
            variant="outline"
            size="sm"
            aria-haspopup="menu"
            aria-expanded={colsOpen}
            data-testid="users-columns"
            title={t('自定义显示列（偏好保存在本浏览器）')}
          >
            <span aria-hidden="true">▤</span> {t('列')} {cols.visibleCount}/{pageColumns.length}
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-56 p-1" align="end" role="menu" data-testid="users-columns-menu">
          {pageColumns.map((c) => {
            const visible = cols.isVisible(c.id)
            const last = visible && cols.visibleCount === 1
            return (
              <button
                key={c.id}
                type="button"
                role="menuitemcheckbox"
                aria-checked={visible}
                aria-disabled={last || undefined}
                title={last ? t('至少保留一列') : undefined}
                data-testid={c.anchor}
                className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-dense hover:bg-accent aria-disabled:opacity-50"
                onClick={() => {
                  if (!last) cols.toggle(c.id)
                }}
              >
                <span aria-hidden="true" className="col-check">{visible ? '☑' : '☐'}</span>
                {c.label}
              </button>
            )
          })}
          <div role="separator" className="my-1 border-t border-border" />
          <button
            type="button"
role="menuitem"
            aria-disabled={cols.visibleCount === pageColumns.length || undefined}
            title={cols.visibleCount === pageColumns.length ? t('全部列已在场') : t('显示全部列')}
            data-testid="users-columns-reset"
            className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-dense hover:bg-accent aria-disabled:opacity-50"
            onClick={() => cols.reset()}
          >
            {t('全选列')}
          </button>
        </PopoverContent>
      </Popover>
        </span>
      </div>

      {state.status === 'loading' && <StateSkeleton lines={6} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'forbidden' && state.error && (
        <EmptyState
          message={t('无权限访问用户管理')}
          hint={t('用户与组管理是管理员功能（管理面需 admin）。制品访问请使用搜索或仓库直链。')}
        />
      )}
      {state.status === 'ok' &&
        (rows.length === 0 ? (
          admin ? (
            <EmptyState illustration message={t('还没有用户')} hint={t('点击「新建用户」建立第一个账号；CI 与脚本建议使用 API Token。')} />
          ) : (
            <EmptyState illustration message={t('还没有用户')} />
          )
        ) : (
          <>
            <table className="w-full text-dense" data-testid="users-table">
              <thead>
                <tr className="border-b border-border text-left text-aux text-muted-foreground">
                  {cols.isVisible('name') && <SortTh label={t('用户名')} sortKey="name" sort={sort} onToggle={toggle} testid="users-sort-name" />}
                  {cols.isVisible('email') && <SortTh label="Email" sortKey="email" sort={sort} onToggle={toggle} />}
                  {cols.isVisible('groups') && <SortTh label={t('组')} sortKey="groups" sort={sort} onToggle={toggle} />}
                  {cols.isVisible('role') && <SortTh label={t('角色')} sortKey="role" sort={sort} onToggle={toggle} />}
                  {cols.isVisible('status') && <SortTh label="Status" sortKey="status" sort={sort} onToggle={toggle} testid="users-sort-status" />}
                  {cols.isVisible('lastLogin') && <SortTh label={t('最近登录')} sortKey="lastLogin" sort={sort} onToggle={toggle} testid="users-sort-lastlogin" />}
                  {admin && cols.isVisible('actions') && <Th label={t('操作')} />}
                </tr>
              </thead>
              <tbody>
                {pageRows.map((r) => {
                  // 自删/内置 admin：UI 预禁用（服务端 400 终裁；title 述因）
                  const self = session?.username === r.name
                  const builtin = r.name === 'admin'
                  const deleteBlocked = self
                    ? t('不能删除当前登录用户（服务端 400 护栏）')
                    : builtin
                      ? t('不能删除内置 admin 用户（服务端 400 护栏）')
                      : undefined
                  return (
                    <tr
                      key={r.name}
                      data-testid={`user-row-${r.name}`}
                      className="cursor-pointer border-b border-border/60 hover:bg-accent"
                      tabIndex={0}
                      onClick={() => navigate(`/admin/security/users/${encodeURIComponent(r.name)}`)}
                      onKeyDown={(e) => onTableRowKeys(e, () => navigate(`/admin/security/users/${encodeURIComponent(r.name)}`))}
                    >
                      {cols.isVisible('name') && (
                        <td className="px-3 py-1.5">
                          <Link className="row-link font-mono text-primary hover:underline" to={`/admin/security/users/${encodeURIComponent(r.name)}`} lang="en" onClick={(e) => e.stopPropagation()}>
                            {r.name}
                          </Link>{' '}
                          <span onClick={(e) => e.stopPropagation()}>
                            <CopyButton value={r.name} label={t('用户名 {v1}', { v1: r.name })} />
                          </span>
                        </td>
                      )}
                      {cols.isVisible('email') && (
                        <td className="px-3 py-1.5">
                          <span className="text-2">{r.email}</span>
                        </td>
                      )}
                      {cols.isVisible('groups') && (
                        <td className="max-w-[360px] break-words px-3 py-1.5">
                          {r.groups.length === 0 ? (
                            <span className="text-muted-foreground">—</span>
                          ) : (
                            <span title={r.groups.join(', ')}>
                              <Badge>{r.groups.length}</Badge>{' '}
                              <span className="sec-chips">
                                {r.groups.map((g) => (
                                  <Badge key={g} mono lang="en">{g}</Badge>
                                ))}
                              </span>
                            </span>
                          )}
                        </td>
                      )}
                      {cols.isVisible('role') && <td className="px-3 py-1.5">{roleBadge(r)}</td>}
                      {cols.isVisible('status') && (
                        <td className="px-3 py-1.5">
                          <span onClick={(e) => e.stopPropagation()}>
                            <StatusLabel enabled={r.enabled} name={r.name} />
                          </span>
                        </td>
                      )}
                      {cols.isVisible('lastLogin') && (
                        <td className="px-3 py-1.5 font-mono" title={r.lastLoggedIn || undefined}>
                          {r.lastLoggedIn ? (
                            r.lastLoggedIn.replace('T', ' ').slice(0, 19)
                          ) : (
                            // T-454 omitempty：整键缺席 = 从未登录——如实呈现
                            <span className="text-muted-foreground">{t('—（尚未登录）')}</span>
                          )}
                        </td>
                      )}
                      {admin && cols.isVisible('actions') && (
                        <td className="whitespace-nowrap px-3 py-1.5" onClick={(e) => e.stopPropagation()}>
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-7 border-destructive/50 text-destructive hover:bg-destructive/10"
                            disabled={deleteBlocked !== undefined}
                            title={deleteBlocked}
                            onClick={() => void deleteUser(r.name)}
                            data-testid={`user-delete-${r.name}`}
                          >
                            {t('删除')}
                          </Button>
                        </td>
                      )}
                    </tr>
                  )
                })}
              </tbody>
            </table>
            <div className="table-foot" data-testid="users-count">
              <Pager
                page={pager.page}
                pageCount={pager.pageCount}
                onPageChange={pager.setPage}
                from={pager.from}
                to={pager.to}
                total={rows.length}
                pageSize={pager.size}
                onPageSizeChange={pager.setSize}
              />
            </div>
          </>
        ))}
    </div>
  )
}
