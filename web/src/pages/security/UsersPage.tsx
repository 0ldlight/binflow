import { useMemo, useState } from 'react'
import type { ComponentPropsWithoutRef } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import Chip from '@mui/material/Chip'
import Divider from '@mui/material/Divider'
import FormControlLabel from '@mui/material/FormControlLabel'
import Menu from '@mui/material/Menu'
import MenuItem from '@mui/material/MenuItem'
import Select from '@mui/material/Select'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import TextField from '@mui/material/TextField'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Pager, useClientPager } from '../../components/Pager'
import { Skeleton } from '../../components/Skeleton'
import { ADMIN_ROLES, ApiError, canAdminWrite, errText, isReadOnlyAdmin, normalizeAdminRole } from '../../lib/api'
import { useColumnPrefs } from '../../lib/columnPrefs'
import type { ColumnDef } from '../../lib/columnPrefs'
import type { AdminRole } from '../../lib/api'
import { onTableRowKeys } from '../../lib/keys'
import { useAsync } from '../../lib/useAsync'
import './security.css'
import { TransferBox } from './TransferBox'
import { SortTh, StatusLabel, applySort, useTableSort, useUserDelete } from './widgets'
import { createUser, listGroups, listUsers, validateUserName } from './api'
import type { UserListItem } from './api'

// 用户列表 + 新建（console-m8 §6.9，T-237 重排；T-257 数据源换 E2 加宽）。
//
// 列集：Name │ Email │ Groups（计数 | 明细，Artifactory "1 | readers" 形态）
// │ Role（三值 badge）│ **Status**（E2 enabled 真值——禁用徽章形态；T-237
// 期的「暂缺（无回显不伪造列）」随 E2 落地退役）│ 操作（admin）。Realm/
// Last Login/Admin 布尔列不建（§6.9[1]）。
//
// 数据源 = **单请求** GET /security/users（E2 列表项已含 email/adminRole/
// enabled/groups——T-251）。T-237 期的「listUsers + 逐用户 getUser」
// N+1 扇出退役（20 用户 = 21 请求 → 1 请求，FR-78/PRD N01）。
// 排序 = 前端列头排序（§4.7 asc/desc/none 循环），全量数据在端上无分页
// （用户数 = 实例账号规模）；底部计数行对齐「用户总数： N」。
//
// 删除（T-257，E4）：行内 Delete（admin）——自删/内置 admin 两态 UI 预禁用
// （服务端 400 终裁兜底）；其余护栏（last-admin 400、404 已删）由服务端
// 原文如实呈现。强确认 = 输入用户名（widgets.useUserDelete）。
//
// 403 收敛（§3.6）：L2——列表 403 呈现无权限卡；L4——创建/删除按钮仅
// admin 渲染。readonly_admin：读面全通 + users-readonly-note（M7 §7.3）。

type UserSortKey = 'name' | 'email' | 'groups' | 'role' | 'status'

/** T-414（FR-135.2，T-387 spec 形态复用）：列选器列集 = **既有全部列**闭集
 * （「无端点列不伪造」——§6.9[1] 明示不建 Realm/Last Login/Admin 布尔列）；
 * label 与表头一致；anchor = 菜单项锚（anchor-audit 的 anchor: 属性形态）。
 * 操作列仅 admin 在场（L4 写面预收敛）——非 admin 视图该列不存在，菜单项
 * 同步不呈现（不伪造空控制）。 */
const COLUMNS: ColumnDef[] = [
  { id: 'name', label: '用户名', anchor: 'users-columns-item-name' },
  { id: 'email', label: 'Email', anchor: 'users-columns-item-email' },
  { id: 'groups', label: '组', anchor: 'users-columns-item-groups' },
  { id: 'role', label: '角色', anchor: 'users-columns-item-role' },
  { id: 'status', label: 'Status', anchor: 'users-columns-item-status' },
  { id: 'actions', label: '操作', anchor: 'users-columns-item-actions' },
]
const COLUMN_IDS = COLUMNS.map((c) => c.id)
const COLS_KEY = 'binflow-console-cols-users'

function roleBadge(item: UserListItem) {
  // E2 列表项无 admin 布尔（W40 禁）——adminRole 恒渲染，闭集外回退
  // false（fail-safe 不放大，normalizeAdminRole 同口径）
  return <RoleLabel role={normalizeAdminRole(item.adminRole, false)} />
}

function RoleLabel({ role }: { role: AdminRole }) {
  // 共享语义 badge（T-266：T-237 自持 role-warning 回退）。T-344 批 C 起
  // Chip 原生皮肤：admin = outlined warning（tint 配方在 MUI 亮色主题实测
  // 3.7:1 不过 4.5 门——outlined 文字色两主题 5.2~7.2:1，见 T-344D 登记）；
  // 其余 = filled default；类名组合续挂（spec §3.8 toHaveClass 钩子）。
  if (role === 'admin')
    return <Chip size="small" variant="outlined" color="warning" className="badge warning" label="admin" lang="en" />
  if (role === 'readonly_admin')
    return <Chip size="small" className="badge neutral" label="readonly_admin" lang="en" />
  return <Chip size="small" className="badge neutral" label="user" lang="en" />
}

interface CreateState {
  name: string
  email: string
  password: string
  role: AdminRole
  enabled: boolean
  groups: string[]
}

const CREATE_INITIAL: CreateState = { name: '', email: '', password: '', role: 'user', enabled: true, groups: [] }

function CreateUserForm({ onDone, onCancel }: { onDone: () => void; onCancel: () => void }) {
  const toast = useToast()
  // 组清单随表单挂载取数（非页面级）：老流（W33d）是先开页再带外建组、
  // 后开表单——页面级缓存会拿到建组前的陈旧列表。
  const groups = useAsync(listGroups, [])
  const [f, setF] = useState<CreateState>(CREATE_INITIAL)
  const [touched, setTouched] = useState<{ name: boolean; email: boolean; password: boolean }>({
    name: false,
    email: false,
    password: false,
  })
  const [submitting, setSubmitting] = useState(false)
  const [serverError, setServerError] = useState<ApiError | null>(null)

  // blur 触发校验（§4.7：必填空给「请填写此字段」语义）；必填未满足时
  // 主按钮禁用（reverse §4.6 新建用户 Save 置灰形态）。用户名规则非空即校。
  const nameErr = touched.name || f.name.trim() !== '' ? validateUserName(f.name.trim()) : null
  const emailErr = touched.email && f.email.trim() === '' ? '请填写此字段' : null
  const passErr = touched.password && f.password === '' ? '请填写此字段' : null
  const canSubmit =
    f.name.trim() !== '' && validateUserName(f.name.trim()) === null && f.email.trim() !== '' && f.password !== '' && !submitting

  const submit = async () => {
    setServerError(null)
    setSubmitting(true)
    try {
      await createUser(f.name.trim(), {
        name: f.name.trim(),
        email: f.email.trim(),
        password: f.password,
        // 角色走一致对（admin 布尔 = adminRole===admin——服务端对校验要求，
        // resolveCreateRole 的对一致性，非「混发」：编辑臂才只走 adminRole）
        admin: f.role === 'admin',
        adminRole: f.role,
        enabled: f.enabled,
        groups: f.groups,
      })
      toast.success(`用户 ${f.name.trim()} 已创建`)
      onDone()
    } catch (err) {
      // 服务端 400 文案原样行内（email/口令缺失、混合大小写、未知组）
      setServerError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <section className="card inline-form" data-testid="user-form" aria-label="新建用户">
      <h3>新建用户</h3>
      {/* T-384 节锚（v1.20 批）：创建表单四节——M3 表单结构 parity 断言载体 */}
      <div className="form-section" data-testid="user-form-section-settings">
        <h4>用户设置</h4>
        <div className="field">
          <label htmlFor="uf-name">用户名 *</label>
          <TextField
            id="uf-name"
            size="small"
            value={f.name}
            onBlur={() => setTouched((p) => ({ ...p, name: true }))}
            onChange={(e) => setF((p) => ({ ...p, name: e.target.value }))}
            placeholder="bob"
            error={!!nameErr}
            sx={{ width: 320 }}
            slotProps={{ htmlInput: { className: 'mono-input', 'data-testid': 'user-form-name', lang: 'en' } }}
          />
          {nameErr ? (
            <p className="field-error" role="alert">
              {nameErr}
            </p>
          ) : (
            <p className="field-hint">全小写；服务端终裁（保留名拒绝）。</p>
          )}
        </div>
        <div className="field">
          <label htmlFor="uf-email">Email *</label>
          <TextField
            id="uf-email"
            size="small"
            type="email"
            value={f.email}
            onBlur={() => setTouched((p) => ({ ...p, email: true }))}
            onChange={(e) => setF((p) => ({ ...p, email: e.target.value }))}
            placeholder="bob@example.com"
            error={!!emailErr}
            sx={{ width: 320 }}
            slotProps={{ htmlInput: { 'data-testid': 'user-form-email' } }}
          />
          {emailErr && (
            <p className="field-error" role="alert">
              {emailErr}
            </p>
          )}
        </div>
        <div className="field" style={{ maxWidth: 480 }}>
          <label htmlFor="uf-role">角色（三值闭集，M7 FR-66）</label>
          <TextField
            id="uf-role"
            select
            size="small"
            value={f.role}
            onChange={(e) => setF((p) => ({ ...p, role: e.target.value as AdminRole }))}
            sx={{ width: 320 }}
            slotProps={{
              select: {
                native: true,
                inputProps: { 'data-testid': 'user-form-role' } as ComponentPropsWithoutRef<'select'>,
              } as ComponentPropsWithoutRef<typeof Select>,
            }}
          >
            {ADMIN_ROLES.map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </TextField>
          <p className="field-hint">user=按 permission target 授权；readonly_admin=管理面只读；admin=管理面全权。</p>
        </div>
      </div>
      <div className="form-section" data-testid="user-form-section-options">
        <h4>选项</h4>
        <FormControlLabel
          className="check-row"
          control={
            <Checkbox
              size="small"
              checked={f.enabled}
              onChange={(e) => setF((p) => ({ ...p, enabled: e.target.checked }))}
              slotProps={{ input: { 'data-testid': 'user-form-enabled' } as ComponentPropsWithoutRef<'input'> }}
            />
          }
          label="启用（取消勾选 = 禁用账号——禁用后登录与写面全部拒绝）"
        />
      </div>
      <div className="form-section" data-testid="user-form-section-password">
        <h4>口令</h4>
        <div className="field">
          <label htmlFor="uf-pass">初始口令 *</label>
          <TextField
            id="uf-pass"
            size="small"
            type="password"
            autoComplete="new-password"
            value={f.password}
            onBlur={() => setTouched((p) => ({ ...p, password: true }))}
            onChange={(e) => setF((p) => ({ ...p, password: e.target.value }))}
            error={!!passErr}
            sx={{ width: 320 }}
            slotProps={{ htmlInput: { 'data-testid': 'user-form-password' } }}
          />
          {passErr && (
            <p className="field-error" role="alert">
              {passErr}
            </p>
          )}
        </div>
      </div>
      <div className="form-section" data-testid="user-form-section-groups">
        <h4>相关组</h4>
        <p className="field-hint">勾选即加入（右列）；保存后即时生效——移出组即失去该组授权，无需重登。</p>
        {groups.status === 'loading' && <Skeleton lines={2} />}
        {groups.status === 'ok' && (
          <div data-testid="user-form-groups">
            <TransferBox
              items={(groups.data ?? []).map((g) => ({ name: g.name, note: g.description || undefined }))}
              selected={f.groups}
              onToggle={(name, next) =>
                setF((p) => ({ ...p, groups: next ? [...p.groups, name] : p.groups.filter((x) => x !== name) }))
              }
              availableLabel="可选组"
              selectedLabel="已选组"
              itemTestid={(name) => `user-form-group-${name}`}
            />
          </div>
        )}
        {groups.status === 'ok' && (groups.data ?? []).length === 0 && (
          <p className="field-hint">实例还没有组——先到「组」页创建。</p>
        )}
        {groups.status !== 'loading' && groups.status !== 'ok' && (
          <p className="field-hint">组列表不可用（{groups.error?.message}）；可先建用户，稍后在编辑页入组。</p>
        )}
      </div>
      {serverError && (
        <Alert severity="error" data-testid="user-form-error">
          <div className="headline">创建失败（HTTP {serverError.status || '网络'}）</div>
          <div className="raw" lang="en">
            {serverError.message}
          </div>
        </Alert>
      )}
      <div className="form-actions">
        {/* 页脚三联（T-384 复役 v1.9 退役锚）：Cancel 最左 / Reset / Save 右——
            parity V6 实测页脚形态的断言载体 */}
        <Button variant="outlined" size="small" onClick={onCancel} data-testid="user-form-cancel">
          取消
        </Button>
        <Button
          variant="outlined"
          size="small"
          onClick={() => setF(CREATE_INITIAL)}
          data-testid="user-form-reset"
        >
          重置
        </Button>
        <Button
          variant="contained"
          size="small"
          disabled={!canSubmit}
          onClick={() => void submit()}
          data-testid="user-form-submit"
        >
          {submitting ? '创建中…' : '创建用户'}
        </Button>
      </div>
    </section>
  )
}

export default function UsersPage() {
  const { session } = useAuth()
  const navigate = useNavigate()
  const admin = canAdminWrite(session)
  const readOnly = isReadOnlyAdmin(session)
  // 单请求（E2 加宽列表）——行模型 = 列表项本体，无逐用户详情扇出
  const state = useAsync(listUsers, [])
  const [creating, setCreating] = useState(false)
  // T-414（FR-135.2）：列显隐偏好（per-page localStorage，T-387 共享层）。
  // 列集随视角收窄：非 admin 无操作列（列不在场则菜单项与偏好 id 同步剔除
  // ——readHidden 按当页列集清洗，未列入不在场列的隐藏项自动失效）。
  const pageColumns = useMemo(() => (admin ? COLUMNS : COLUMNS.filter((c) => c.id !== 'actions')), [admin])
  const pageIds = useMemo(() => (admin ? COLUMN_IDS : COLUMN_IDS.filter((id) => id !== 'actions')), [admin])
  const cols = useColumnPrefs(pageIds, COLS_KEY)
  const [colsAnchor, setColsAnchor] = useState<HTMLElement | null>(null)
  const colsOpen = Boolean(colsAnchor)
  const { sort, toggle } = useTableSort<UserSortKey>({ key: 'name', dir: 'asc' })
  const { openDialog: deleteUser } = useUserDelete({ onDeleted: () => state.reload() })

  const rows = applySort(state.data ?? [], sort as { key: string | null; dir: 'asc' | 'desc' }, (r) => {
    switch (sort.key) {
      case 'email':
        return r.email
      case 'groups':
        return r.groups.length
      case 'role':
        return normalizeAdminRole(r.adminRole, false)
      case 'status':
        return r.enabled ? 1 : 0
      default:
        return r.name
    }
  })
  // T-451（E2 翻案）：客户端页窗（数据形态 × 排序变化即回落第 1 页；
  // 字符串键口径：同形刷新不丢页位）
  const pageEpoch = `${state.status}|${rows.length}|${sort.key ?? ''}|${sort.dir}`
  const pager = useClientPager(rows.length, pageEpoch)
  const pageRows = pager.slice(rows)

  return (
    <div data-testid="users-page">
      <div className="page-header">
        <h2>用户</h2>
        {admin && (
          <Button variant="contained" size="small" onClick={() => setCreating((v) => !v)} data-testid="users-create">
            {creating ? '收起表单' : '＋ 新建用户'}
          </Button>
        )}
      </div>

      {readOnly && (
        <p className="admin-note" data-testid="users-readonly-note">
          ⓘ 只读管理员（readonly_admin）视角：用户与角色只读；创建/编辑是管理面写操作（服务端 403 兜底）。
        </p>
      )}

      {creating && admin && (
        <CreateUserForm
          onDone={() => {
            setCreating(false)
            state.reload()
          }}
          onCancel={() => setCreating(false)}
        />
      )}

      {/* T-414（FR-135.2）：工具栏尾 = 列选器（T-387 L1 形态复用——MUI Menu +
          menuitemcheckbox 项，字形勾选态 aria-hidden 装饰、语义在
          aria-checked；计数行 users-count 在表尾，尾组自推右端）。本页无
          工具栏搜索面（用户数 = 实例账号规模，全量在端上），filter-bar 单独
          承载列选尾组。 */}
      <div className="filter-bar">
        <span className="filter-tail-actions filter-tail-end">
          <Button
            variant="outlined"
            size="small"
            aria-haspopup="menu"
            aria-expanded={colsOpen}
            data-testid="users-columns"
            title="自定义显示列（偏好保存在本浏览器）"
            onClick={(e) => setColsAnchor(e.currentTarget)}
          >
            <span aria-hidden="true">▤</span> 列 {cols.visibleCount}/{pageColumns.length}
          </Button>
          <Menu
            open={colsOpen}
            onClose={() => setColsAnchor(null)}
            anchorEl={colsAnchor}
            anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
            transformOrigin={{ vertical: 'top', horizontal: 'right' }}
            data-testid="users-columns-menu"
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
                  title={last ? '至少保留一列' : undefined}
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
              title={cols.visibleCount === pageColumns.length ? '全部列已在场' : '显示全部列'}
              data-testid="users-columns-reset"
              onClick={() => cols.reset()}
            >
              全选列
            </MenuItem>
          </Menu>
        </span>
      </div>

      {state.status === 'loading' && <Skeleton lines={6} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'forbidden' && state.error && (
        <EmptyState
          message="无权限访问用户管理"
          hint="用户与组管理是管理员功能（管理面需 admin）。制品访问请使用搜索或仓库直链。"
        />
      )}
      {state.status === 'ok' &&
        (rows.length === 0 ? (
          admin ? (
            <EmptyState illustration message="还没有用户" hint="点击「新建用户」建立第一个账号；CI 与脚本建议使用 API Token。" />
          ) : (
            <EmptyState illustration message="还没有用户" />
          )
        ) : (
          <>
            <Table data-testid="users-table">
              <TableHead>
                <TableRow>
                  {cols.isVisible('name') && (
                    <SortTh label="用户名" sortKey="name" sort={sort} onToggle={toggle} testid="users-sort-name" />
                  )}
                  {cols.isVisible('email') && <SortTh label="Email" sortKey="email" sort={sort} onToggle={toggle} />}
                  {cols.isVisible('groups') && <SortTh label="组" sortKey="groups" sort={sort} onToggle={toggle} />}
                  {cols.isVisible('role') && <SortTh label="角色" sortKey="role" sort={sort} onToggle={toggle} />}
                  {cols.isVisible('status') && (
                    <SortTh label="Status" sortKey="status" sort={sort} onToggle={toggle} testid="users-sort-status" />
                  )}
                  {admin && cols.isVisible('actions') && <TableCell component="th" scope="col">操作</TableCell>}
                </TableRow>
              </TableHead>
              <TableBody>
                {pageRows.map((r) => {
                  // 自删/内置 admin：UI 预禁用（服务端 400 终裁；title 述因）
                  const self = session?.username === r.name
                  const builtin = r.name === 'admin'
                  const deleteBlocked = self ? '不能删除当前登录用户（服务端 400 护栏）' : builtin ? '不能删除内置 admin 用户（服务端 400 护栏）' : undefined
                  return (
                    <TableRow
                      key={r.name}
                      data-testid={`user-row-${r.name}`}
                      hover
                      tabIndex={0}
                      onKeyDown={(e) =>
                        onTableRowKeys(e, () => navigate(`/admin/security/users/${encodeURIComponent(r.name)}`))
                      }
                    >
                      {cols.isVisible('name') && (
                        <TableCell>
                          <Link className="row-link mono" to={`/admin/security/users/${encodeURIComponent(r.name)}`} lang="en">
                            {r.name}
                          </Link>{' '}
                          <CopyButton value={r.name} label={`用户名 ${r.name}`} />
                        </TableCell>
                      )}
                      {cols.isVisible('email') && (
                        <TableCell>
                          <span className="text-2">{r.email}</span>
                        </TableCell>
                      )}
                      {cols.isVisible('groups') && (
                        <TableCell sx={{ maxWidth: 360, whiteSpace: 'normal', wordBreak: 'break-word' }}>
                          {r.groups.length === 0 ? (
                            <span className="text-muted">—</span>
                          ) : (
                            <span title={r.groups.join(', ')}>
                              <Chip size="small" className="badge neutral" label={r.groups.length} />{' '}
                              <span className="sec-chips">
                                {r.groups.map((g) => (
                                  <Chip
                                    key={g}
                                    size="small"
                                    className="badge neutral mono"
                                    label={g}
                                    sx={{ fontFamily: 'var(--bf-mono)' }}
                                    lang="en"
                                  />
                                ))}
                              </span>
                            </span>
                          )}
                        </TableCell>
                      )}
                      {cols.isVisible('role') && <TableCell>{roleBadge(r)}</TableCell>}
                      {cols.isVisible('status') && (
                        <TableCell>
                          <StatusLabel enabled={r.enabled} name={r.name} />
                        </TableCell>
                      )}
                      {admin && cols.isVisible('actions') && (
                        <TableCell>
                          <Button
                            variant="outlined"
                            color="error"
                            size="small"
                            disabled={deleteBlocked !== undefined}
                            title={deleteBlocked}
                            onClick={() => void deleteUser(r.name)}
                            data-testid={`user-delete-${r.name}`}
                          >
                            删除
                          </Button>
                        </TableCell>
                      )}
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
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
