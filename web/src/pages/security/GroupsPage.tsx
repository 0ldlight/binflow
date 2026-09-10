// 组列表（console-m8 §6.10——P3 新栈重写）：Name〔描述副行〕│ 权限数〔+
// manage 徽章〕│ 成员数 │ 操作（admin）。
// - 三请求链式取数：组列表 + 成员扫描（E2 单请求双索引）+ 权限 target
//   列表；后两者行级降级（null）不阻塞组列表；
// - W33c 核心：删除被 permission target 引用的组 → 409 冲突面板（服务端
//   原文 mono + 解析 target 名链接直达权限编辑器解除引用）；
// - 成员单源（ADR-0030 K19）：E2 users.groups 投影。
// 锚族原样：groups-page/groups-create/groups-columns(-menu|-item-*|-reset)/
// groups-table/group-row-<name>/group-manage-badge-<name>/group-members-<name>
// /group-edit-<name>/group-delete-<name>/group-delete-reason/groups-count/
// groups-readonly-note/groups-sort-name。
import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { useAuth } from '@/app/AuthContext'
import { Button, ButtonAsChild } from '@/components/ui/button'
import { Badge, AlertBox } from '@/components/layout/bits'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { Pager, useClientPager } from '@/components/layout/pager'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { SortTh, applySort, useTableSort, Th } from '@/components/layout/table'
import { useConfirm } from '@/app/providers'
import { toast } from '@/lib/toast'
import { ApiError, canAdminWrite, errText, isReadOnlyAdmin } from '@/lib/api'
import { useColumnPrefs } from '@/lib/columnPrefs'
import type { ColumnDef } from '@/lib/columnPrefs'
import { useAsync } from '@/lib/useAsync'
import './security.css'
import {
  deleteGroup,
  grantsOfGroup,
  listGroups,
  listPermissionTargets,
  listUsers,
  parseReferencedTargets,
} from './api'
import type { GroupListItem } from './api'
import { tr } from '@/i18n'

const tt = tr('security')

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
  const confirm = useConfirm()
  // 一次链式取数（三请求，与用户数无关）；成员扫描/targets 行级降级
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

  const pageColumns = useMemo(() => (admin ? COLUMNS : COLUMNS.filter((c) => c.id !== 'actions')), [admin])
  const pageIds = useMemo(() => (admin ? COLUMN_IDS : COLUMN_IDS.filter((id) => id !== 'actions')), [admin])
  const cols = useColumnPrefs(pageIds, COLS_KEY)
  const [colsOpen, setColsOpen] = useState(false)

  const groups = state.data?.groups ?? []
  const membership = state.data?.membership ?? null
  const targets = state.data?.targets ?? null
  const rows: GroupRowModel[] = groups.map((g) => {
    const grants = targets ? grantsOfGroup(targets, g.name) : []
    return { group: g, grants, memberCount: membership ? (membership.groupMembers[g.name] ?? []).length : null }
  })
  const sorted = applySort(rows, sort, (r) => {
    switch (sort.key) {
      case 'perms':
        return r.grants.length
      case 'members':
        return r.memberCount ?? null
      default:
        return r.group.name
    }
  })
  const pageEpoch = `${state.status}|${sorted.length}|${sort.key ?? ''}|${sort.dir}`
  const pager = useClientPager(sorted.length, pageEpoch)
  const pageRows = pager.slice(sorted)

  const doDelete = async (name: string, description: string) => {
    const memberCount = membership ? (membership.groupMembers[name] ?? []).length : null
    const ok = await confirm.confirm({
      title: tt('删除组 {name}', { name: name }),
      description: (
        <div>
          <p>{tt('确定要移除该组吗？此操作不可撤销。')}</p>
          {memberCount !== null && memberCount > 0 && <p>{tt('将解除')} {memberCount} {tt('个成员的关联。')}</p>}
          <p className="text-muted-foreground text-aux">
            {description ? (
              <>
                {tt('描述：')}{description}{tt('。')}<br />
              </>
            ) : null}
            {tt('若该组被 permission target 引用，服务端会拒绝（409）并列出引用的 target 名。')}
          </p>
        </div>
      ),
      confirmLabel: tt('删除'),
      danger: true,
    })
    if (!ok) return
    try {
      const text = await deleteGroup(name)
      toast.success(text) // 服务端纯文本文案
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
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{tt('组')}</h2>
        {admin && (
          <Button size="sm" className="ml-auto" onClick={() => navigate('/admin/security/groups/new')} data-testid="groups-create">
            {tt('＋ 新建组')}
          </Button>
        )}
      </div>

      {readOnly && (
        <p className="admin-note" data-testid="groups-readonly-note">
          {tt('ⓘ 只读管理员（readonly_admin）视角：组只读；创建/编辑/删除是管理面写操作（服务端 403 兜底）。')}
        </p>
      )}

      {conflict && (
        <AlertBox severity="warning" testid="group-delete-reason">
          <div>{tt('无法删除组')} <span className="font-mono" lang="en">{conflict.group}</span>{tt('——它正被 permission target 引用')}</div>
          <div className="mt-1 font-mono text-aux [overflow-wrap:anywhere]" lang="en">{conflict.message}</div>
          <div className="mt-1.5 flex flex-wrap items-center gap-2">
            {conflict.targets.length > 0 && <span className="text-2">{tt('解除引用（编辑后移除该组主体）：')}</span>}
            {conflict.targets.map((tgt) => (
              <ButtonAsChild key={tgt} variant="outline" size="sm" className="h-7">
                <Link to={`/admin/security/permissions/${encodeURIComponent(tgt)}`}>
                  <span className="font-mono" lang="en">{tgt}</span>
                </Link>
              </ButtonAsChild>
            ))}
            <Button variant="outline" size="sm" className="h-7" onClick={() => setConflict(null)}>{tt('稍后再试')}</Button>
          </div>
        </AlertBox>
      )}

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
            data-testid="groups-columns"
            title={tt('自定义显示列（偏好保存在本浏览器）')}
          >
            <span aria-hidden="true">▤</span> {tt('列')} {cols.visibleCount}/{pageColumns.length}
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-56 p-1" align="end" role="menu" data-testid="groups-columns-menu">
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
                title={last ? tt('至少保留一列') : undefined}
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
            title={cols.visibleCount === pageColumns.length ? tt('全部列已在场') : tt('显示全部列')}
            data-testid="groups-columns-reset"
            className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-dense hover:bg-accent aria-disabled:opacity-50"
            onClick={() => cols.reset()}
          >
            {tt('全选列')}
          </button>
        </PopoverContent>
      </Popover>
        </span>
      </div>

      {state.status === 'loading' && <StateSkeleton lines={5} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'forbidden' && state.error && (
        <EmptyState message={tt('无权限访问组管理')} hint={tt('用户与组管理是管理员功能（管理面需 admin）。')} />
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
            <table className="w-full text-dense" data-testid="groups-table">
              <thead>
                <tr className="border-b border-border text-left text-aux text-muted-foreground">
                  {cols.isVisible('name') && <SortTh label={tt('组名')} sortKey="name" sort={sort} onToggle={toggle} testid="groups-sort-name" />}
                  {cols.isVisible('perms') && <SortTh label={tt('权限数')} sortKey="perms" sort={sort} onToggle={toggle} />}
                  {cols.isVisible('members') && <SortTh label={tt('成员数')} sortKey="members" sort={sort} onToggle={toggle} />}
                  {admin && cols.isVisible('actions') && <Th label={tt('操作')} />}
                </tr>
              </thead>
              <tbody>
                {pageRows.map((r) => (
                  <tr key={r.group.name} data-testid={`group-row-${r.group.name}`} className="border-b border-border/60 hover:bg-accent">
                    {cols.isVisible('name') && (
                      <td className="px-3 py-1.5">
                        <div className="cell-stack">
                          <span>
                            <span className="font-mono" lang="en">{r.group.name}</span>{' '}
                            <CopyButton value={r.group.name} label={tt('组名 {v1}', { v1: r.group.name })} />
                          </span>
                          {r.group.description && (
                            <span className="text-[11px] text-muted-foreground">{r.group.description}</span>
                          )}
                        </div>
                      </td>
                    )}
                    {cols.isVisible('perms') && (
                      <td className="px-3 py-1.5">
                        {targets === null ? (
                          <span className="text-muted-foreground">—</span>
                        ) : (
                          <span className="cell-inline">
                            <span className="text-2">{r.grants.length}</span>
                            {r.grants.some((g) => g.actions.includes('manage')) && (
                              <Badge
                                mono
                                lang="en"
                                testid={`group-manage-badge-${r.group.name}`}
                                title={tt('组在至少一个 permission target 上持有 manage（仓库配置派生权）——BinFlow 无 Artifactory 组级 adminPrivileges 字段（有意不跟进，rbac-model §5）')}
                              >
                                manage
                              </Badge>
                            )}
                          </span>
                        )}
                      </td>
                    )}
                    {cols.isVisible('members') && (
                      <td className="px-3 py-1.5">
                        {r.memberCount === null ? (
                          <span className="text-muted-foreground">—</span>
                        ) : (
                          <span
                            className="text-2"
                            title={(membership?.groupMembers[r.group.name] ?? []).join(', ') || undefined}
                            data-testid={`group-members-${r.group.name}`}
                          >
                            {r.memberCount}
                          </span>
                        )}
                      </td>
                    )}
                    {admin && cols.isVisible('actions') && (
                      <td className="whitespace-nowrap px-3 py-1.5">
                        <span className="inline-flex gap-2">
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-7"
                            onClick={() => navigate(`/admin/security/groups/${encodeURIComponent(r.group.name)}/edit`)}
                            data-testid={`group-edit-${r.group.name}`}
                          >
                            {tt('编辑')}
                          </Button>
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-7 border-destructive/50 text-destructive hover:bg-destructive/10"
                            onClick={() => void doDelete(r.group.name, r.group.description)}
                            data-testid={`group-delete-${r.group.name}`}
                          >
                            {tt('删除')}
                          </Button>
                        </span>
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
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
