import { useEffect, useState } from 'react'
import type { ComponentPropsWithoutRef } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import FormControlLabel from '@mui/material/FormControlLabel'
import Select from '@mui/material/Select'
import TextField from '@mui/material/TextField'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ADMIN_ROLES, ApiError, canAdminWrite, errText, isReadOnlyAdmin, normalizeAdminRole } from '../../lib/api'
import type { AdminRole } from '../../lib/api'
import { useAsync } from '../../lib/useAsync'
import './security.css'
import { TransferBox } from './TransferBox'
import { PermSummaryTable, StatusLabel, useUserDelete } from './widgets'
import { getUser, grantsOfUser, listGroups, listPermissionTargets, updateUser } from './api'
import type { UserDetail, UserUpdateBody } from './api'

// 用户编辑器（console-m8 §6.9 编辑态，T-237 重排；T-257 数据源换 E3/E4）：
// 分区形态 = 用户设置 / 选项（状态）/ 口令 / 相关组（双列穿梭）/ 用户权限
// 矩阵（只读汇总）/ 账户信息（含危险区）。
//
// 角色（M7 FR-66 / §7.1）：三值下拉，仅走 adminRole 通道（与 admin 布尔
// 混发不一致 → 服务端 400）；readonly_admin 视角禁用 + 说明行。
//
// enabled 翻转（§7.5 / T-224 非缺陷②）：表单全量提交形态——保存体总是
// 携带 enabled（POST 部分更新臂的指针语义：携带即写入）。控件**回显驱动**
// （E3 enabled 恒渲染，T-251）——T-237 期的 knownEnabled 本地回显 hack
// （「本页写过的值即已知值」+ 默认启用假设）及其漂移注记随本票退役。
//
// 删除用户（E4，T-257）：账户信息卡内危险区（admin）——自删/内置 admin
// 预禁用，其余护栏（last-admin 400 / 404 已删）服务端原文如实呈现；成功
// 后回列表。§6.9 线框的「Actions ▾」菜单不建（单动作不设菜单壳）。
//
// readonly_admin 进入本页 = 只读呈现：全部编辑面禁用（服务端 403 兜底，
// UI 无绕过——服务端是唯一守门）。

const ROLE_LABEL: Record<AdminRole, string> = {
  user: 'user —— 内容面按 permission target 授权',
  readonly_admin: 'readonly_admin —— 管理面全量只读',
  admin: 'admin —— 管理面全权',
}

interface EditState {
  email: string
  role: AdminRole
  enabled: boolean
  groups: string[]
  password: string
  password2: string
}

/** 编辑初值：E3 回显驱动（enabled 恒渲染）——DB 行事实直入表单 */
function editFromDetail(d: UserDetail): EditState {
  return {
    email: d.email,
    role: normalizeAdminRole(d.adminRole, d.admin),
    enabled: d.enabled,
    groups: [...d.groups],
    password: '',
    password2: '',
  }
}

export default function UserDetailPage() {
  const { name = '' } = useParams<{ name: string }>()
  const { session } = useAuth()
  const readOnly = isReadOnlyAdmin(session)
  const admin = canAdminWrite(session)
  const toast = useToast()
  const navigate = useNavigate()
  const detail = useAsync(() => getUser(name), [name])
  const groups = useAsync(listGroups, [])
  const targets = useAsync(listPermissionTargets, [])
  const [f, setF] = useState<EditState | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [serverError, setServerError] = useState<ApiError | null>(null)
  // 删除成功（或 404 已被他人删）→ 回列表（详情页的对象已不存在）
  const { openDialog: deleteUser } = useUserDelete({
    onDeleted: () => navigate('/admin/security/users'),
  })

  useEffect(() => {
    if (detail.status === 'ok' && detail.data) setF(editFromDetail(detail.data))
  }, [detail.status, detail.data])

  if (detail.status === 'loading') {
    return (
      <div data-testid="user-detail-page">
        <Skeleton lines={8} />
      </div>
    )
  }
  if (detail.status === 'error' && detail.error) {
    return (
      <div data-testid="user-detail-page">
        {detail.error.status === 404 ? (
          <EmptyState
            message={`用户 ${name} 不存在`}
            action={
              <Button variant="outlined" size="small" component={Link} to="/admin/security/users">
                ← 返回用户列表
              </Button>
            }
          />
        ) : (
          <ErrorCard error={detail.error} onRetry={detail.reload} />
        )}
      </div>
    )
  }
  if (detail.status === 'forbidden' && detail.error) {
    return (
      <div data-testid="user-detail-page">
        <EmptyState message="无权限访问用户管理" hint={detail.error.message} />
      </div>
    )
  }

  const d = detail.data
  const baseRole = d ? normalizeAdminRole(d.adminRole, d.admin) : null
  // 自删/内置 admin：UI 预禁用（与列表行同口径；服务端 400 终裁）
  const self = session?.username === name
  const builtin = name === 'admin'
  const deleteBlocked = self
    ? '不能删除当前登录用户（服务端 400 护栏）'
    : builtin
      ? '不能删除内置 admin 用户（服务端 400 护栏）'
      : undefined
  const groupsEqual = (a: readonly string[], b: readonly string[]) =>
    JSON.stringify([...a].sort()) === JSON.stringify([...b].sort())
  const dirty =
    !!f &&
    !!d &&
    (f.email.trim() !== d.email ||
      f.role !== baseRole ||
      f.enabled !== d.enabled ||
      !groupsEqual(f.groups, d.groups) ||
      f.password !== '')
  const passMismatch = !!f && f.password !== '' && f.password !== f.password2

  const submit = async () => {
    if (!f || !d) return
    setServerError(null)
    setSubmitting(true)
    try {
      const body: UserUpdateBody = { name: d.name }
      if (f.email.trim() !== d.email) body.email = f.email.trim()
      // 角色只走 adminRole 通道（与 admin 布尔混发不一致 → 服务端 400）
      if (f.role !== baseRole) body.adminRole = f.role
      // enabled：表单全量提交形态（§7.5）——总是携带，指针语义即写入
      body.enabled = f.enabled
      if (!groupsEqual(f.groups, d.groups)) body.groups = f.groups
      if (f.password !== '') body.password = f.password
      await updateUser(d.name, body)
      toast.success(`用户 ${d.name} 已更新`)
      setF((p) => (p ? { ...p, password: '', password2: '' } : p))
      detail.reload()
    } catch (err) {
      setServerError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setSubmitting(false)
    }
  }

  const permRows = d && targets.status === 'ok' ? grantsOfUser(targets.data ?? [], d.name, d.groups) : null

  return (
    <div data-testid="user-detail-page">
      <div className="page-header">
        <h2>
          编辑用户 · <span className="mono" lang="en">{name}</span>
        </h2>
        <Button variant="outlined" size="small" component={Link} to="/admin/security/users">
          ← 返回列表
        </Button>
      </div>

      <section className="card inline-form" data-testid="user-form" aria-label="编辑用户">
        <h3>用户设置</h3>
        {f && (
          <>
            {readOnly && (
              <p className="admin-note" data-testid="user-form-readonly-note">
                ⓘ 只读管理员（readonly_admin）视角：用户编辑是管理面写操作，本页为只读呈现
                （服务端 403 兜底，UI 不代持判定）。
              </p>
            )}
            <div className="form-section">
              <div className="field">
                <label htmlFor="ud-name">用户名（不可变）</label>
                <div>
                  <span className="mono" lang="en">
                    {name}
                  </span>{' '}
                  <CopyButton value={name} label={`用户名 ${name}`} />
                </div>
              </div>
              <div className="field">
                <label htmlFor="ud-email">Email</label>
                <TextField
                  id="ud-email"
                  size="small"
                  type="email"
                  value={f.email}
                  disabled={readOnly}
                  onChange={(e) => setF((p) => (p ? { ...p, email: e.target.value } : p))}
                  sx={{ width: 320 }}
                  slotProps={{ htmlInput: { 'data-testid': 'user-form-email' } }}
                />
                {f.email.trim() === '' && <p className="field-error">email 不能为空（服务端 400）</p>}
              </div>
              <div className="field" style={{ maxWidth: 480 }}>
                <label htmlFor="ud-role">角色（三值闭集——wire 值即选项值）</label>
                <TextField
                  id="ud-role"
                  select
                  size="small"
                  value={f.role}
                  disabled={readOnly}
                  onChange={(e) => setF((p) => (p ? { ...p, role: e.target.value as AdminRole } : p))}
                  sx={{ width: 420 }}
                  slotProps={{
                    select: {
                      native: true,
                      inputProps: { 'data-testid': 'user-form-role' } as ComponentPropsWithoutRef<'select'>,
                    } as ComponentPropsWithoutRef<typeof Select>,
                  }}
                >
                  {ADMIN_ROLES.map((r) => (
                    <option key={r} value={r} data-testid={`user-form-role-${r}`}>
                      {ROLE_LABEL[r]}
                    </option>
                  ))}
                </TextField>
                <p className="field-hint">
                  角色变更即时生效并落 <span className="mono" lang="en">user.role.change</span> 审计；只读管理员对
                  permission target 短路（组合无效而非非法）。
                </p>
              </div>
            </div>
            <div className="form-section">
              <h4>选项</h4>
              <FormControlLabel
                className="check-row"
                control={
                  <Checkbox
                    size="small"
                    checked={f.enabled}
                    disabled={readOnly}
                    onChange={(e) => setF((p) => (p ? { ...p, enabled: e.target.checked } : p))}
                    slotProps={{ input: { 'data-testid': 'user-form-enabled' } as ComponentPropsWithoutRef<'input'> }}
                  />
                }
                label="启用（取消勾选 = 禁用账号——登录与写面全部拒绝）"
              />
              <p className="field-hint">勾选态 = 服务端 enabled 回显（E3，DB 行事实）；保存总是携带该位写入。</p>
            </div>
            <div className="form-section">
              <h4>口令</h4>
              <div className="field">
                <label htmlFor="ud-pass">重置口令（可选——留空不改动；无需旧口令）</label>
                <TextField
                  id="ud-pass"
                  size="small"
                  type="password"
                  autoComplete="new-password"
                  placeholder="（不改动）"
                  value={f.password}
                  disabled={readOnly}
                  onChange={(e) => setF((p) => (p ? { ...p, password: e.target.value } : p))}
                  sx={{ width: 320 }}
                  slotProps={{ htmlInput: { 'data-testid': 'user-form-password' } }}
                />
              </div>
              <div className="field">
                <label htmlFor="ud-pass2">确认口令</label>
                <TextField
                  id="ud-pass2"
                  size="small"
                  type="password"
                  autoComplete="new-password"
                  placeholder="（再输入一次）"
                  value={f.password2}
                  disabled={readOnly}
                  onChange={(e) => setF((p) => (p ? { ...p, password2: e.target.value } : p))}
                  error={passMismatch}
                  sx={{ width: 320 }}
                  slotProps={{ htmlInput: { 'data-testid': 'user-form-password2' } }}
                />
                {passMismatch && (
                  <p className="field-error" role="alert">
                    两次输入的口令不一致
                  </p>
                )}
              </div>
            </div>
            <div className="form-section">
              <h4>相关组</h4>
              <p className="field-hint">勾选即加入（右列）；保存后即时生效——移出组即失去该组授权，无需重登。</p>
              {groups.status === 'loading' && <Skeleton lines={2} />}
              {groups.status === 'ok' && (
                <div data-testid="user-form-groups">
                  <TransferBox
                    items={(groups.data ?? []).map((g) => ({ name: g.name, note: g.description || undefined }))}
                    selected={f.groups}
                    disabled={readOnly}
                    onToggle={(g, next) =>
                      setF((p) =>
                        p ? { ...p, groups: next ? [...p.groups, g] : p.groups.filter((x) => x !== g) } : p,
                      )
                    }
                    availableLabel="可选组"
                    selectedLabel="已选组"
                    itemTestid={(g) => `user-form-group-${g}`}
                  />
                </div>
              )}
              {groups.status !== 'loading' && groups.status !== 'ok' && (
                <p className="field-hint">组列表不可用（{groups.error?.message}）。</p>
              )}
            </div>
            {serverError && (
              <Alert severity="error" data-testid="user-form-error">
                <div className="headline">保存失败（HTTP {serverError.status || '网络'}）</div>
                <div className="raw" lang="en">
                  {serverError.message}
                </div>
              </Alert>
            )}
            <div className="form-actions">
              <Button variant="outlined" size="small" component={Link} to="/admin/security/users">
                取消
              </Button>
              <Button
                variant="outlined"
                size="small"
               
                disabled={!dirty || submitting}
                onClick={() => d && setF(editFromDetail(d))}
              >
                重置
              </Button>
              <Button
                variant="contained"
                size="small"
                disabled={!dirty || f.email.trim() === '' || passMismatch || submitting || readOnly}
                title={readOnly ? '只读管理员：用户编辑是管理面写操作（服务端 403）' : undefined}
                onClick={() => void submit()}
                data-testid="user-form-submit"
              >
                {submitting ? '保存中…' : '保存'}
              </Button>
            </div>
          </>
        )}
      </section>

      <section className="card" data-testid="user-perms">
        <h3>用户权限矩阵</h3>
        <p className="field-hint">只读汇总（来源 = 各 permission target 的直接行与经组行）——变更入口在权限编辑器。</p>
        {targets.status === 'loading' && <Skeleton lines={3} />}
        {targets.status === 'ok' && (
          <PermSummaryTable
            rows={permRows ?? []}
            rowTestidPrefix="user-perm"
            emptyHint="该用户未获得任何 permission target 授权（直接与经组均为空）。"
          />
        )}
        {targets.status !== 'loading' && targets.status !== 'ok' && (
          <p className="field-hint">权限汇总不可用（{targets.error?.message}）。</p>
        )}
      </section>

      <section className="card" data-testid="user-facts">
        <h3>账户信息</h3>
        <div className="kv">
          <span className="k">realm</span>
          <span className="mono" lang="en">
            {d?.realm ?? '—'}
          </span>
        </div>
        <div className="kv">
          <span className="k">角色</span>
          <span className="mono" lang="en" data-testid="user-facts-role">
            {baseRole ?? '—'}
          </span>
        </div>
        <div className="kv">
          <span className="k">Status</span>
          <span>{d ? <StatusLabel enabled={d.enabled} /> : '—'}</span>
        </div>
        <div className="kv">
          <span className="k">最近登录</span>
          <span>{d?.lastLoggedIn ? <span className="mono" lang="en">{d.lastLoggedIn}</span> : '—（尚未登录）'}</span>
        </div>
        <div className="kv">
          <span className="k">API URI</span>
          <span className="mono wrap" lang="en" style={{ overflowWrap: 'anywhere' }}>
            {d ? `/binflow/api/security/users/${d.name}` : '—'}
          </span>
        </div>
        {admin && (
          <div className="sec-danger-zone" data-testid="user-danger-zone">
            <div className="dz-head">危险区</div>
            <p className="dz-note">
              删除不可恢复（组员/授权/token/会话同事务级联；审计保留）。人员离场的可逆路径是
              <b>禁用</b>（选项区）——删除仅用于账号彻底清退。
            </p>
            {deleteBlocked ? (
              <Button
                variant="outlined"
                color="error"
                size="small"
               
                disabled
                title={deleteBlocked}
                data-testid="user-delete"
              >
                删除用户
              </Button>
            ) : (
              <Button
                variant="outlined"
                color="error"
                size="small"
               
                onClick={() => void deleteUser(name)}
                data-testid="user-delete"
              >
                删除用户
              </Button>
            )}
          </div>
        )}
      </section>
    </div>
  )
}
