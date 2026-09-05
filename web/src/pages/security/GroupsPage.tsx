import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import Divider from '@mui/material/Divider'
import Menu from '@mui/material/Menu'
import MenuItem from '@mui/material/MenuItem'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { useConfirm } from '../../components/ConfirmDialog'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Pager, useClientPager } from '../../components/Pager'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, canAdminWrite, errText, isReadOnlyAdmin } from '../../lib/api'
import { useColumnPrefs } from '../../lib/columnPrefs'
import type { ColumnDef } from '../../lib/columnPrefs'
import { useAsync } from '../../lib/useAsync'
import './security.css'
import { SortTh, applySort, useTableSort } from './widgets'
import {
  deleteGroup,
  grantsOfGroup,
  listGroups,
  listPermissionTargets,
  listUsers,
  parseReferencedTargets,
} from './api'
import type { GroupListItem } from './api'
import { tr } from '../../i18n'

const tt = tr('security')

// 组列表（console-m8 §6.10，T-237 重排；T-257 数据源换 E2/E5 单源）：
// Name〔描述副行〕│ 权限数〔+ manage 徽章〕│ 成员数。
//
// T-453（FR-145.1，断言反转④——Q5 出口①路由化）：创建/编辑迁整页路由
// 表单（/admin/security/groups/new、/:name/edit——7.161.20 同构），列表
// 内联展开卡（创建 + 编辑同卡）退役——「＋ 新建组」/行内「编辑」= 导航
// 入口（groups-create / group-edit-<name> 锚保持）。编辑器本体迁
// GroupFormPage.tsx（E5 选区 + E2 候选 + 权限矩阵随迁）。
//
// adminPrivileges 徽章：BinFlow 组模型无 Artifactory 的 adminPrivileges
// 布尔（rbac-model §5 有意不跟进——组不承载角色语义）；徽章呈现的是
// 「组在至少一个 target 上持有 manage」——manage（仓库配置派生权，
// ADR-0026）是其最小诚实同构，数据源 = 权限 target 列表（零新端点）。
//
// 成员单源（T-257，ADR-0030 K19/§14.1 E5）：成员汇总的事实源 = user_groups
// 行，仅经两个服务端视图物化——**E2** users 列表内嵌 groups[]（本页成员
// 数列一次请求推导）与 **E5** `GET groups/{name}?includeUsers=true`
// （GroupFormPage 编辑态的按需单组读）。两视图同源，e2e 交叉断言。groups
// 列表端点不加宽（K19：membersCount 与 groups[] 双物化面必漂移）。
//
// W33c 核心：删除被 permission target 引用的组 → 409，message 列 target
// 名。UI 呈现：行内冲突面板（服务端原文 mono + 解析出的 target 名链接到
// 权限编辑器，解除引用后重删即成——「不撞墙」）。

/** 成员扫描快照（E2 单源）：users 列表项内嵌 groups[] 的双索引投影 */
/** T-414（FR-135.2，T-387 spec 形态复用）：列选器列集 = **既有全部列**闭集
 * （「无端点列不伪造」——组模型无 adminPrivileges 徽章列外的虚构列）；label
 * 与表头一致；anchor = 菜单项锚（anchor-audit 的 anchor: 属性形态）。操作列
 * 仅 admin 在场——非 admin 视图列与菜单项同步剔除（UsersPage 同款）。 */
const COLUMNS: ColumnDef[] = [
  { id: 'name', label: tt('组名'), anchor: 'groups-columns-item-name' },
  { id: 'perms', label: tt('权限数'), anchor: 'groups-columns-item-perms' },
  { id: 'members', label: tt('成员数'), anchor: 'groups-columns-item-members' },
  { id: 'actions', label: tt('操作'), anchor: 'groups-columns-item-actions' },
]
const COLUMN_IDS = COLUMNS.map((c) => c.id)
const COLS_KEY = 'binflow-console-cols-groups'

interface MembershipSnapshot {
  /** user → groups */
  userGroups: Record<string, string[]>
  /** group → 成员用户名（有序） */
  groupMembers: Record<string, string[]>
}

/** E2 列表 → 双索引（成员数列 + 穿梭候选全集 + 成员落盘的当前组集） */
function snapshotFromUsers(users: readonly { name: string; groups: string[] }[]): MembershipSnapshot {
  const userGroups: Record<string, string[]> = {}
  const groupMembers: Record<string, string[]> = {}
  for (const u of users) {
    userGroups[u.name] = u.groups
    for (const g of u.groups) {
      ;(groupMembers[g] ??= []).push(u.name)
    }
  }
  return { userGroups, groupMembers }
}

/** 单请求成员扫描：GET /security/users（E2 加宽列表）——与用户数无关 */
async function scanMembership(): Promise<MembershipSnapshot> {
  return snapshotFromUsers(await listUsers())
}

interface GroupRowModel {
  group: GroupListItem
  /** 该组被引用的 target 行（权限数 + manage 徽章 + 矩阵数据源） */
  grants: ReturnType<typeof grantsOfGroup>
  memberCount: number | null
}

interface Conflict {
  group: string
  message: string
  targets: string[]
}

export default function GroupsPage() {
  const { session } = useAuth()
  const navigate = useNavigate()
  const admin = canAdminWrite(session)
  const readOnly = isReadOnlyAdmin(session)
  const toast = useToast()
  const confirm = useConfirm()
  // 一次链式取数（三请求，与用户数无关）：组列表 + 成员扫描（E2 单请求）
  // + 权限 target 列表；成员扫描/targets 行级降级（null）不阻塞组列表
  const state = useAsync(async () => {
    const [groups, membership, targets] = await Promise.all([
      listGroups(),
      scanMembership().catch(() => null),
      listPermissionTargets().catch(() => null),
    ])
    return { groups, membership, targets }
  }, [])
  const { sort, toggle } = useTableSort<'name' | 'perms' | 'members'>({ key: 'name', dir: 'asc' })

  const [conflict, setConflict] = useState<Conflict | null>(null)

  // T-414（FR-135.2）：列显隐偏好（T-387 共享层；非 admin 视图无操作列——
  // 列集/菜单项/偏好 id 同步剔除，UsersPage 同款口径）
  const pageColumns = useMemo(() => (admin ? COLUMNS : COLUMNS.filter((c) => c.id !== 'actions')), [admin])
  const pageIds = useMemo(() => (admin ? COLUMN_IDS : COLUMN_IDS.filter((id) => id !== 'actions')), [admin])
  const cols = useColumnPrefs(pageIds, COLS_KEY)
  const [colsAnchor, setColsAnchor] = useState<HTMLElement | null>(null)
  const colsOpen = Boolean(colsAnchor)

  const groups = state.data?.groups ?? []
  const membership = state.data?.membership ?? null
  const targets = state.data?.targets ?? null
  const rows: GroupRowModel[] = groups.map((g) => {
    const grants = targets ? grantsOfGroup(targets, g.name) : []
    return { group: g, grants, memberCount: membership ? (membership.groupMembers[g.name] ?? []).length : null }
  })
  const sorted = applySort(rows, sort as { key: string | null; dir: 'asc' | 'desc' }, (r) => {
    switch (sort.key) {
      case 'perms':
        return r.grants.length
      case 'members':
        return r.memberCount ?? null
      default:
        return r.group.name
    }
  })
  // T-451（E2 翻案）：客户端页窗（数据形态 × 排序变化即回落第 1 页；
  // 字符串键口径：同形刷新不丢页位）
  const pageEpoch = `${state.status}|${sorted.length}|${sort.key ?? ''}|${sort.dir}`
  const pager = useClientPager(sorted.length, pageEpoch)
  const pageRows = pager.slice(sorted)

  const doDelete = async (name: string, description: string) => {
    const memberCount = membership ? (membership.groupMembers[name] ?? []).length : null
    const ok = await confirm({
      title: tt('删除组 {name}', { name: name }),
      body: (
        <div>
          <p>{tt('确定要移除该组吗？此操作不可撤销。')}</p>
          {memberCount !== null && memberCount > 0 && <p>{tt('将解除')} {memberCount} {tt('个成员的关联。')}</p>}
          <p className="text-muted" style={{ fontSize: 12 }}>
            {description && (
              <>{tt('描述：')}{description}{tt('。')}                <br />
              </>
            )}{tt('若该组被 permission target 引用，服务端会拒绝（409）并列出引用的 target 名。')}          </p>
        </div>
      ),
      confirmLabel: tt('删除'),
      danger: true,
    })
    if (!ok) return
    try {
      const text = await deleteGroup(name)
      toast.success(text) // 服务端纯文本文案（"The group: 'x' has been removed successfully."）
      setConflict(null)
      state.reload()
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        // W33c：被引用保护——原文 + 解析 target 名（链接直达编辑器解除）
        setConflict({ group: name, message: err.message, targets: parseReferencedTargets(err.message) })
        return
      }
      toast.error(tt('删除失败：{v1}', { v1: errText(err) }))
    }
  }

  return (
    <div data-testid="groups-page">
      <div className="page-header">
        <h2>{tt('组')}</h2>
        {admin && (
          <Button
            variant="contained"
            size="small"
            onClick={() => navigate('/admin/security/groups/new')}
            data-testid="groups-create"
          >{tt('＋ 新建组')}          </Button>
        )}
      </div>

      {readOnly && (
        <p className="admin-note" data-testid="groups-readonly-note">{tt('ⓘ 只读管理员（readonly_admin）视角：组只读；创建/编辑/删除是管理面写操作（服务端 403 兜底）。')}        </p>
      )}

      {conflict && (
        <Alert
          severity="warning"
          data-testid="group-delete-reason"
          sx={{ mb: 2, '& .MuiAlert-message': { width: '100%' } }}
        >
          <div>{tt('无法删除组')} <span className="mono" lang="en">{conflict.group}</span>{tt('——它正被 permission target 引用')}</div>
          <div className="mono" lang="en" style={{ fontSize: 'var(--bf-fs-aux)', overflowWrap: 'anywhere' }}>
            {conflict.message}
          </div>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center', marginTop: 4 }}>
            {conflict.targets.length > 0 && <span className="text-2">{tt('解除引用（编辑后移除该组主体）：')}</span>}
            {conflict.targets.map((t) => (
              <Button
                key={t}
                variant="outlined"
                size="small"
                component={Link}
                to={`/admin/security/permissions/${encodeURIComponent(t)}`}
              >
                <span className="mono" lang="en">
                  {t}
                </span>
              </Button>
            ))}
            <Button variant="outlined" size="small" onClick={() => setConflict(null)}>{tt('稍后再试')}            </Button>
          </div>
        </Alert>
      )}

      {/* T-414（FR-135.2）：工具栏尾 = 列选器（T-387 L1 形态复用；计数行
          groups-count 在表尾，尾组自推右端；本页无工具栏搜索面——filter-bar
          单独承载列选尾组，UsersPage 同款） */}
      <div className="filter-bar">
        <span className="filter-tail-actions filter-tail-end">
          <Button
            variant="outlined"
            size="small"
            aria-haspopup="menu"
            aria-expanded={colsOpen}
            data-testid="groups-columns"
            title={tt('自定义显示列（偏好保存在本浏览器）')}
            onClick={(e) => setColsAnchor(e.currentTarget)}
          >
            <span aria-hidden="true">▤</span> {tt('列')} {cols.visibleCount}/{pageColumns.length}
          </Button>
          <Menu
            open={colsOpen}
            onClose={() => setColsAnchor(null)}
            anchorEl={colsAnchor}
            anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
            transformOrigin={{ vertical: 'top', horizontal: 'right' }}
            data-testid="groups-columns-menu"
          >
            {pageColumns.map((c) => {
              const visible = cols.isVisible(c.id)
              // 至少一列在场：仅剩一列可见时该项不可再弃
              const last = visible && cols.visibleCount === 1
              return (
                <MenuItem
                  key={c.id}
                  role="menuitemcheckbox"
                  aria-checked={visible}
                  aria-disabled={last || undefined}
                  title={last ? tt('至少保留一列') : undefined}
                  data-testid={c.anchor}
                  onClick={() => {
                    if (!last) cols.toggle(c.id)
                  }}
                >
                  <span aria-hidden="true" className="col-check">
                    {visible ? '☑' : '☐'}
                  </span>
                  {c.label}
                </MenuItem>
              )
            })}
            <Divider component="li" />
            <MenuItem
              aria-disabled={cols.visibleCount === pageColumns.length || undefined}
              title={cols.visibleCount === pageColumns.length ? tt('全部列已在场') : tt('显示全部列')}
              data-testid="groups-columns-reset"
              onClick={() => cols.reset()}
            >{tt('全选列')}            </MenuItem>
          </Menu>
        </span>
      </div>

      {state.status === 'loading' && <Skeleton lines={5} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'forbidden' && state.error && (
        <EmptyState
          message={tt('无权限访问组管理')}
          hint={tt('用户与组管理是管理员功能（管理面需 admin）。')}
        />
      )}
      {state.status === 'ok' &&
        (sorted.length === 0 ? (
          admin ? (
            <EmptyState
              illustration
              message={tt('还没有组')}
              hint={tt('组的授权经 permission target 生效（组行 × read/write/delete/manage 并集）。')}
            />
          ) : (
            <EmptyState illustration message={tt('还没有组')} />
          )
        ) : (
          <>
            <Table data-testid="groups-table">
              <TableHead>
                <TableRow>
                  {cols.isVisible('name') && (
                    <SortTh label={tt('组名')} sortKey="name" sort={sort} onToggle={toggle} testid="groups-sort-name" />
                  )}
                  {cols.isVisible('perms') && <SortTh label={tt('权限数')} sortKey="perms" sort={sort} onToggle={toggle} />}
                  {cols.isVisible('members') && (
                    <SortTh label={tt('成员数')} sortKey="members" sort={sort} onToggle={toggle} />
                  )}
                  {admin && cols.isVisible('actions') && <TableCell component="th" scope="col">{tt('操作')}</TableCell>}
                </TableRow>
              </TableHead>
              <TableBody>
                {pageRows.map((r) => (
                  <TableRow key={r.group.name} data-testid={`group-row-${r.group.name}`} hover>
                    {cols.isVisible('name') && (
                      <TableCell>
                        <div className="cell-stack">
                          <span>
                            <span className="mono" lang="en">
                              {r.group.name}
                            </span>{' '}
                            <CopyButton value={r.group.name} label={tt('组名 {v1}', { v1: r.group.name })} />
                          </span>
                          {r.group.description && (
                            <span className="text-muted" style={{ fontSize: 11 }}>
                              {r.group.description}
                            </span>
                          )}
                        </div>
                      </TableCell>
                    )}
                    {cols.isVisible('perms') && (
                      <TableCell>
                        {targets === null ? (
                          <span className="text-muted">—</span>
                        ) : (
                          <span className="cell-inline">
                            <span className="text-2">
                              {r.grants.length}
                            </span>
                            {r.grants.some((g) => g.actions.includes('manage')) && (
                              <Chip
                                size="small"
                                className="badge neutral mono"
                                label="manage"
                                sx={{ fontFamily: 'var(--bf-mono)' }}
                                lang="en"
                                data-testid={`group-manage-badge-${r.group.name}`}
                                title={tt('组在至少一个 permission target 上持有 manage（仓库配置派生权）——BinFlow 无 Artifactory 组级 adminPrivileges 字段（有意不跟进，rbac-model §5）')}
                              />
                            )}
                          </span>
                        )}
                      </TableCell>
                    )}
                    {cols.isVisible('members') && (
                      <TableCell>
                        {r.memberCount === null ? (
                          <span className="text-muted">—</span>
                        ) : (
                          <span className="text-2" title={(membership?.groupMembers[r.group.name] ?? []).join(', ') || undefined} data-testid={`group-members-${r.group.name}`}>
                            {r.memberCount}
                          </span>
                        )}
                      </TableCell>
                    )}
                    {admin && cols.isVisible('actions') && (
                      <TableCell>
                        <span style={{ display: 'inline-flex', gap: 8 }}>
                          <Button
                            variant="outlined"
                            size="small"
                            onClick={() =>
                              navigate(`/admin/security/groups/${encodeURIComponent(r.group.name)}/edit`)
                            }
                            data-testid={`group-edit-${r.group.name}`}
                          >{tt('编辑')}                          </Button>
                          <Button
                            variant="outlined"
                            color="error"
                            size="small"
                            onClick={() => void doDelete(r.group.name, r.group.description)}
                            data-testid={`group-delete-${r.group.name}`}
                          >{tt('删除')}                          </Button>
                        </span>
                      </TableCell>
                    )}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <div className="table-foot" data-testid="groups-count">
              <Pager
                page={pager.page}
                pageCount={pager.pageCount}
                onPageChange={pager.setPage}
                from={pager.from}
                to={pager.to}
                total={sorted.length}
                pageSize={pager.size}
                onPageSizeChange={pager.setSize}
              />
            </div>
          </>
        ))}
    </div>
  )
}
