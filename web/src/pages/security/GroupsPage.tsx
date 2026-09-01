import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import TextField from '@mui/material/TextField'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { useConfirm } from '../../components/ConfirmDialog'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, canAdminWrite, errText, isReadOnlyAdmin } from '../../lib/api'
import { useAsync } from '../../lib/useAsync'
import './security.css'
import { TransferBox } from './TransferBox'
import { PermSummaryTable, SortTh, applySort, useTableSort } from './widgets'
import {
  deleteGroup,
  getGroupWithUsers,
  grantsOfGroup,
  listGroups,
  listPermissionTargets,
  listUsers,
  parseReferencedTargets,
  putGroup,
  updateUser,
  validateGroupName,
} from './api'
import type { GroupListItem, PermissionTarget } from './api'

// 组管理（console-m8 §6.10，T-237 重排；T-257 数据源换 E2/E5 单源）：
// 列表（Name〔描述副行〕│ 权限数〔+ manage 徽章〕│ 成员数）+ 同形态分区
// 编辑器（组设置 / 成员穿梭 / 编辑态组权限矩阵）。
//
// adminPrivileges 徽章：BinFlow 组模型无 Artifactory 的 adminPrivileges
// 布尔（rbac-model §5 有意不跟进——组不承载角色语义）；徽章呈现的是
// 「组在至少一个 target 上持有 manage」——manage（仓库配置派生权，
// ADR-0026）是其最小诚实同构，数据源 = 权限 target 列表（零新端点）。
//
// 成员单源（T-257，ADR-0030 K19/§14.1 E5）：成员汇总的事实源 = user_groups
// 行，仅经两个服务端视图物化——**E2** users 列表内嵌 groups[]（本页成员
// 数列/穿梭候选全集一次请求推导；T-237 期的「listUsers + 逐用户 getUser」
// N+1〔20 用户 = 21 请求〕退役，契约漂移②随 E5 落地销账）与 **E5**
// `GET groups/{name}?includeUsers=true`（编辑器打开瞬间的按需单组读，
// userNames 种入穿梭已选列——比页面级 E2 快照新鲜）。两视图同源，e2e
// 交叉断言。groups 列表端点不加宽（K19：membersCount 与 groups[] 双物化
// 面必漂移）。成员变更仍经各用户的部分更新臂落盘（POST groups 全量替换
// 该用户组集——组侧写端点无票承载，gap-endpoints §3.2）。
//
// W33c 核心：删除被 permission target 引用的组 → 409，message 列 target
// 名。UI 呈现：行内冲突面板（服务端原文 mono + 解析出的 target 名链接到
// 权限编辑器，解除引用后重删即成——「不撞墙」）。

/** 成员扫描快照（E2 单源）：users 列表项内嵌 groups[] 的双索引投影 */
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

interface EditorSeed {
  mode: 'create' | 'edit'
  name: string
  description: string
}

function GroupEditor({
  seed,
  snapshot,
  targets,
  onDone,
  onCancel,
}: {
  seed: EditorSeed
  snapshot: MembershipSnapshot | null
  targets: PermissionTarget[] | null
  onDone: () => void
  onCancel: () => void
}) {
  const toast = useToast()
  const editMode = seed.mode === 'edit'
  const [name, setName] = useState(seed.name)
  const [description, setDescription] = useState(seed.description)
  // 编辑态选区 = E5 按需单组读（userNames）；null = 取数中/不可用。
  // 创建态无选区初值（空串组）。
  const e5 = useAsync(
    () => (editMode ? getGroupWithUsers(seed.name) : Promise.resolve(null)),
    [editMode, seed.name],
  )
  const e5Members = e5.status === 'ok' && e5.data ? e5.data.userNames : null
  const [members, setMembers] = useState<string[] | null>(editMode ? null : [])
  useEffect(() => {
    if (e5Members) setMembers(e5Members)
  }, [e5Members])
  const [submitting, setSubmitting] = useState(false)
  const [serverError, setServerError] = useState<ApiError | null>(null)

  const initialMembers = e5Members ?? []
  const membersReady = !editMode || e5Members !== null
  const nameErr = editMode ? null : validateGroupName(name.trim())
  const memberAdded = (members ?? []).filter((m) => !initialMembers.includes(m))
  const memberRemoved = initialMembers.filter((m) => !(members ?? []).includes(m))
  const canSubmit = (editMode || (name.trim() !== '' && nameErr === null)) && membersReady
  const dirty = editMode
    ? (description !== seed.description || memberAdded.length > 0 || memberRemoved.length > 0) && membersReady
    : true

  /** 成员落盘：逐用户替换组集（add → 追加本组；remove → 去掉本组）。
   *  幂等护栏：E5 视图与 E2 快照之间带外并发下，已入组用户跳过追加
   *  （避免重复组名）；移除臂的 filter 天然幂等。 */
  const applyMembership = async (group: string) => {
    if (!snapshot) return
    const failures: string[] = []
    for (const m of memberAdded) {
      const current = snapshot.userGroups[m]
      if (!current) {
        failures.push(m)
        continue
      }
      if (current.includes(group)) continue // 视图竞窗外已在组——收敛而非重复追加
      try {
        await updateUser(m, { groups: [...current, group] })
      } catch {
        failures.push(m)
      }
    }
    for (const m of memberRemoved) {
      const current = snapshot.userGroups[m]
      if (!current) {
        failures.push(m)
        continue
      }
      try {
        await updateUser(m, { groups: current.filter((g) => g !== group) })
      } catch {
        failures.push(m)
      }
    }
    return failures
  }

  const submit = async () => {
    setServerError(null)
    setSubmitting(true)
    try {
      await putGroup(name.trim(), description.trim())
      let failures: string[] = []
      if (memberAdded.length > 0 || memberRemoved.length > 0) {
        failures = (await applyMembership(name.trim())) ?? []
      }
      if (failures.length > 0) {
        // 部分成员变更未落盘：组本体已保存（PUT 先成），失败名单行内呈现
        setServerError(
          new ApiError(
            0,
            `部分成员变更未落盘（${failures.join(', ')}）——组本体已保存；请重试或到用户编辑器逐个处理。`,
          ),
        )
        return
      }
      if (editMode) {
        toast.success(
          memberAdded.length === 0 && memberRemoved.length === 0
            ? `组 ${name.trim()} 的描述已更新`
            : `组 ${name.trim()} 已更新（成员 +${memberAdded.length} −${memberRemoved.length}）`,
        )
      } else {
        toast.success(
          memberAdded.length > 0 ? `组 ${name.trim()} 已创建（成员 +${memberAdded.length}）` : `组 ${name.trim()} 已创建`,
        )
      }
      onDone()
    } catch (err) {
      setServerError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setSubmitting(false)
    }
  }

  const userItems = snapshot
    ? Object.keys(snapshot.userGroups)
        .sort()
        .map((u) => ({ name: u, note: snapshot.userGroups[u].length > 0 ? snapshot.userGroups[u].join(', ') : undefined }))
    : []
  const grants = editMode && targets ? grantsOfGroup(targets, seed.name) : []

  return (
    <section className="card inline-form" data-testid="group-form" aria-label={editMode ? '编辑组' : '新建组'}>
      <h3>{editMode ? `编辑组 · ${seed.name}` : '新建组'}</h3>
      {/* T-384 节锚（v1.20 批）：组面两节——M3 组表单结构 parity 断言载体
          （编辑态第三节日权限矩阵沿用 group-perm-matrix 既有锚，不设节锚） */}
      <div className="form-section" data-testid="group-form-section-settings">
        <h4>组设置</h4>
        <div className="field">
          <label htmlFor="gf-name">组名{editMode ? '（不可变）' : ' *'}</label>
          <TextField
            id="gf-name"
            size="small"
            value={editMode ? seed.name : name}
            disabled={editMode}
            onChange={(e) => setName(e.target.value)}
            placeholder="qa-team"
            error={!!nameErr}
            sx={{ width: 320 }}
            slotProps={{ htmlInput: { className: 'mono-input', 'data-testid': 'group-form-name', lang: 'en' } }}
          />
          {nameErr ? (
            <p className="field-error" role="alert">
              {nameErr}
            </p>
          ) : (
            <p className="field-hint">[a-z][a-z0-9._-]*，≤64；保留字 anonymous / _system_ 拒绝。</p>
          )}
        </div>
        <div className="field">
          <label htmlFor="gf-desc">描述</label>
          <TextField
            id="gf-desc"
            size="small"
            multiline
            minRows={2}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="用途、负责人…"
            sx={{ width: 480 }}
            slotProps={{ htmlInput: { 'data-testid': 'group-form-description' } }}
          />
        </div>
      </div>
      <div className="form-section" data-testid="group-form-section-members">
        <h4>成员</h4>
        <p className="field-hint">勾选即加入（右列）；保存后即时生效——移出即失去该组授权，无需重登。</p>
        {!snapshot ? (
          <p className="field-hint">成员数据不可用（用户列表加载失败）——可先保存组，稍后维护成员。</p>
        ) : !membersReady ? (
          <Skeleton lines={2} />
        ) : (
          <div data-testid="group-form-members">
            <TransferBox
              items={userItems}
              selected={members ?? []}
              onToggle={(u, next) => setMembers((p) => (next ? [...(p ?? []), u] : (p ?? []).filter((x) => x !== u)))}
              availableLabel="可选用户"
              selectedLabel="已选成员"
              itemTestid={(u) => `group-form-member-${u}`}
            />
          </div>
        )}
        {e5.status === 'error' && e5.error && (
          <p className="field-error" role="alert">
            组成员读取失败（{e5.error.message}）——组可能已被删除或更名。
          </p>
        )}
      </div>
      {editMode && (
        <div className="form-section">
          <h4>组权限矩阵</h4>
          <p className="field-hint">只读汇总（来源 = 引用本组的 permission target）——变更入口在权限编辑器。</p>
          {targets ? (
            <PermSummaryTable
              rows={grants}
              rowTestidPrefix="group-perm"
              emptyHint="该组未被任何 permission target 引用——组的授权经 target 生效。"
            />
          ) : (
            <p className="field-hint">权限汇总不可用。</p>
          )}
        </div>
      )}
      {serverError && (
        <Alert severity="error">
          <div className="headline">{editMode ? '保存失败' : '创建失败'}</div>
          <div className="raw">{serverError.message}</div>
        </Alert>
      )}
      <div className="form-actions">
        {/* 页脚三联（T-384 复役 v1.9 退役锚）：Cancel 最左 / Reset / Save 右 */}
        <Button variant="outlined" size="small" onClick={onCancel} data-testid="group-form-cancel">
          取消
        </Button>
        <Button
          variant="outlined"
          size="small"
          disabled={!dirty || submitting}
          data-testid="group-form-reset"
          onClick={() => {
            setName(seed.name)
            setDescription(seed.description)
            setMembers(initialMembers)
          }}
        >
          重置
        </Button>
        <Button
          variant="contained"
          size="small"
          disabled={!canSubmit || !dirty || submitting}
          onClick={() => void submit()}
          data-testid="group-form-submit"
        >
          {submitting ? '保存中…' : editMode ? '保存' : '创建组'}
        </Button>
      </div>
    </section>
  )
}

export default function GroupsPage() {
  const { session } = useAuth()
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

  const [form, setForm] = useState<EditorSeed | null>(null)
  const [conflict, setConflict] = useState<Conflict | null>(null)

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

  const doDelete = async (name: string, description: string) => {
    const memberCount = membership ? (membership.groupMembers[name] ?? []).length : null
    const ok = await confirm({
      title: `删除组 ${name}`,
      body: (
        <div>
          <p>确定要移除该组吗？此操作不可撤销。</p>
          {memberCount !== null && memberCount > 0 && <p>将解除 {memberCount} 个成员的关联。</p>}
          <p className="text-muted" style={{ fontSize: 12 }}>
            {description && (
              <>
                描述：{description}。
                <br />
              </>
            )}
            若该组被 permission target 引用，服务端会拒绝（409）并列出引用的 target 名。
          </p>
        </div>
      ),
      confirmLabel: '删除',
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
      toast.error(`删除失败：${errText(err)}`)
    }
  }

  return (
    <div data-testid="groups-page">
      <div className="page-header">
        <h2>组</h2>
        {admin && (
          <Button
            variant="contained"
            size="small"
            onClick={() => setForm({ mode: 'create', name: '', description: '' })}
            data-testid="groups-create"
          >
            ＋ 新建组
          </Button>
        )}
      </div>

      {readOnly && (
        <p className="admin-note" data-testid="groups-readonly-note">
          ⓘ 只读管理员（readonly_admin）视角：组只读；创建/编辑/删除是管理面写操作（服务端 403 兜底）。
        </p>
      )}

      {form && admin && (
        <GroupEditor
          seed={form}
          snapshot={membership}
          targets={targets}
          onDone={() => {
            setForm(null)
            state.reload()
          }}
          onCancel={() => setForm(null)}
        />
      )}

      {conflict && (
        <Alert
          severity="warning"
          data-testid="group-delete-reason"
          sx={{ mb: 2, '& .MuiAlert-message': { width: '100%' } }}
        >
          <div>无法删除组 <span className="mono" lang="en">{conflict.group}</span>——它正被 permission target 引用</div>
          <div className="mono" lang="en" style={{ fontSize: 'var(--bf-fs-aux)', overflowWrap: 'anywhere' }}>
            {conflict.message}
          </div>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center', marginTop: 4 }}>
            {conflict.targets.length > 0 && <span className="text-2">解除引用（编辑后移除该组主体）：</span>}
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
            <Button variant="outlined" size="small" onClick={() => setConflict(null)}>
              稍后再试
            </Button>
          </div>
        </Alert>
      )}

      {state.status === 'loading' && <Skeleton lines={5} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'forbidden' && state.error && (
        <EmptyState
          message="无权限访问组管理"
          hint="用户与组管理是管理员功能（管理面需 admin）。"
        />
      )}
      {state.status === 'ok' &&
        (sorted.length === 0 ? (
          admin ? (
            <EmptyState
              illustration
              message="还没有组"
              hint="组的授权经 permission target 生效（组行 × read/write/delete/manage 并集）。"
            />
          ) : (
            <EmptyState illustration message="还没有组" />
          )
        ) : (
          <>
            <Table data-testid="groups-table">
              <TableHead>
                <TableRow>
                  <SortTh label="组名" sortKey="name" sort={sort} onToggle={toggle} testid="groups-sort-name" />
                  <SortTh label="权限数" sortKey="perms" sort={sort} onToggle={toggle} />
                  <SortTh label="成员数" sortKey="members" sort={sort} onToggle={toggle} />
                  {admin && <TableCell component="th" scope="col">操作</TableCell>}
                </TableRow>
              </TableHead>
              <TableBody>
                {sorted.map((r) => (
                  <TableRow key={r.group.name} data-testid={`group-row-${r.group.name}`} hover>
                    <TableCell>
                      <div className="cell-stack">
                        <span>
                          <span className="mono" lang="en">
                            {r.group.name}
                          </span>{' '}
                          <CopyButton value={r.group.name} label={`组名 ${r.group.name}`} />
                        </span>
                        {r.group.description && (
                          <span className="text-muted" style={{ fontSize: 11 }}>
                            {r.group.description}
                          </span>
                        )}
                      </div>
                    </TableCell>
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
                              title="组在至少一个 permission target 上持有 manage（仓库配置派生权）——BinFlow 无 Artifactory 组级 adminPrivileges 字段（有意不跟进，rbac-model §5）"
                            />
                          )}
                        </span>
                      )}
                    </TableCell>
                    <TableCell>
                      {r.memberCount === null ? (
                        <span className="text-muted">—</span>
                      ) : (
                        <span className="text-2" title={(membership?.groupMembers[r.group.name] ?? []).join(', ') || undefined} data-testid={`group-members-${r.group.name}`}>
                          {r.memberCount}
                        </span>
                      )}
                    </TableCell>
                    {admin && (
                      <TableCell>
                        <span style={{ display: 'inline-flex', gap: 8 }}>
                          <Button
                            variant="outlined"
                            size="small"
                            onClick={() => setForm({ mode: 'edit', name: r.group.name, description: r.group.description })}
                            data-testid={`group-edit-${r.group.name}`}
                          >
                            编辑
                          </Button>
                          <Button
                            variant="outlined"
                            color="error"
                            size="small"
                            onClick={() => void doDelete(r.group.name, r.group.description)}
                            data-testid={`group-delete-${r.group.name}`}
                          >
                            删除
                          </Button>
                        </span>
                      </TableCell>
                    )}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <p className="table-foot" data-testid="groups-count">
              组总数： {sorted.length}
            </p>
          </>
        ))}
    </div>
  )
}
