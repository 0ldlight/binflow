// 用户编辑器（console-m8 §6.9 编辑态——P3 新栈重写）：分区形态 = 用户
// 设置 / 选项（状态）/ 口令 / 相关组（双列穿梭）/ 用户权限矩阵（只读
// 汇总）/ 账户信息（含危险区）。
// - 角色三值下拉仅走 adminRole 通道（与 admin 布尔混发不一致 → 服务端 400）；
// - enabled 翻转 = 表单全量提交形态（保存体总是携带 enabled，指针语义即
//   写入）；回显驱动（E3 enabled 恒渲染）；
// - 删除（E4）：危险区（admin）——自删/内置 admin 预禁用；成功（或 404
//   已被删）回列表；
// - readonly_admin 进入本页 = 只读呈现（服务端 403 兜底）。
// 锚族原样：user-detail-page/user-form(-email|-role(-<r>)?|-enabled|
// -password(2)?|-groups|-group-<name>|-submit|-error|-readonly-note)/
// user-perms/user-perm-matrix/user-facts(-role)/user-danger-zone/user-delete。
import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { useAuth } from '@/app/AuthContext'
import { Button, ButtonAsChild } from '@/components/ui/button'
import { AlertBox, CheckRow, StatusLabel } from '@/components/layout/bits'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { TextInput, NativeSelect } from '@/components/layout/fields'
import { toast } from '@/lib/toast'
import { ADMIN_ROLES, ApiError, canAdminWrite, errText, isReadOnlyAdmin, normalizeAdminRole } from '@/lib/api'
import type { AdminRole } from '@/lib/api'
import { useAsync } from '@/lib/useAsync'
import './security.css'
import { TransferBox } from '@/components/layout/transfer-box'
import { PermSummaryTable, useUserDelete } from './widgets'
import { getUser, grantsOfUser, listGroups, listPermissionTargets, updateUser } from './api'
import type { UserDetail, UserUpdateBody } from './api'
import { tr } from '@/i18n'

const t = tr('security')

const ROLE_LABEL: Record<AdminRole, string> = {
  user: t('user —— 内容面按 permission target 授权'),
  readonly_admin: t('readonly_admin —— 管理面全量只读'),
  admin: t('admin —— 管理面全权'),
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
        <StateSkeleton lines={8} />
      </div>
    )
  }
  if (detail.status === 'error' && detail.error) {
    return (
      <div data-testid="user-detail-page">
        {detail.error.status === 404 ? (
          <EmptyState
            message={t('用户 {name} 不存在', { name: name })}
            action={
              <ButtonAsChild variant="outline" size="sm">
                <Link to="/admin/security/users">{t('← 返回用户列表')}</Link>
              </ButtonAsChild>
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
        <EmptyState message={t('无权限访问用户管理')} hint={detail.error.message} />
      </div>
    )
  }

  const d = detail.data
  const baseRole = d ? normalizeAdminRole(d.adminRole, d.admin) : null
  // 自删/内置 admin：UI 预禁用（与列表行同口径；服务端 400 终裁）
  const self = session?.username === name
  const builtin = name === 'admin'
  const deleteBlocked = self
    ? t('不能删除当前登录用户（服务端 400 护栏）')
    : builtin
      ? t('不能删除内置 admin 用户（服务端 400 护栏）')
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
      toast.success(t('用户 {v1} 已更新', { v1: d.name }))
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
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">
          {t('编辑用户 ·')} <span className="font-mono" lang="en">{name}</span>
        </h2>
        <ButtonAsChild variant="outline" size="sm" className="ml-auto">
          <Link to="/admin/security/users">{t('← 返回列表')}</Link>
        </ButtonAsChild>
      </div>

      <section className="card inline-form" data-testid="user-form" aria-label={t('编辑用户')}>
        <h3>{t('用户设置')}</h3>
        {f && (
          <>
            {readOnly && (
              <p className="admin-note" data-testid="user-form-readonly-note">
                {t('ⓘ 只读管理员（readonly_admin）视角：用户编辑是管理面写操作，本页为只读呈现 （服务端 403 兜底，UI 不代持判定）。')}
              </p>
            )}
            <div className="form-section">
              <div className="field">
                <label>{t('用户名（不可变）')}</label>
                <div>
                  <span className="font-mono" lang="en">{name}</span>{' '}
                  <CopyButton value={name} label={t('用户名 {name}', { name: name })} />
                </div>
              </div>
              <div className="field">
                <label htmlFor="ud-email">Email</label>
                <TextInput
                  id="ud-email"
                  type="email"
                  value={f.email}
                  disabled={readOnly}
                  onChange={(e) => setF((p) => (p ? { ...p, email: e.target.value } : p))}
                  data-testid="user-form-email"
                />
                {f.email.trim() === '' && <p className="field-error">{t('email 不能为空（服务端 400）')}</p>}
              </div>
              <div className="field max-w-[480px]">
                <label htmlFor="ud-role">{t('角色（三值闭集——wire 值即选项值）')}</label>
                <NativeSelect
                  id="ud-role"
                  className="max-w-[420px]"
                  value={f.role}
                  disabled={readOnly}
                  onChange={(e) => setF((p) => (p ? { ...p, role: e.target.value as AdminRole } : p))}
                  options={ADMIN_ROLES.map((r) => ({ value: r, label: ROLE_LABEL[r] }))}
                  data-testid="user-form-role"
                />
                <p className="field-hint">
                  {t('角色变更即时生效并落')} <span className="font-mono" lang="en">user.role.change</span> {t('审计；只读管理员对 permission target 短路（组合无效而非非法）。')}
                </p>
              </div>
            </div>
            <div className="form-section">
              <h4>{t('选项')}</h4>
              <CheckRow
                checked={f.enabled}
                disabled={readOnly}
                onChange={(next) => setF((p) => (p ? { ...p, enabled: next } : p))}
                label={t('启用（取消勾选 = 禁用账号——登录与写面全部拒绝）')}
                testid="user-form-enabled"
              />
              <p className="field-hint">{t('勾选态 = 服务端 enabled 回显（E3，DB 行事实）；保存总是携带该位写入。')}</p>
            </div>
            <div className="form-section">
              <h4>{t('口令')}</h4>
              <div className="field">
                <label htmlFor="ud-pass">{t('重置口令（可选——留空不改动；无需旧口令）')}</label>
                <TextInput
                  id="ud-pass"
                  type="password"
                  autoComplete="new-password"
                  placeholder={t('（不改动）')}
                  value={f.password}
                  disabled={readOnly}
                  onChange={(e) => setF((p) => (p ? { ...p, password: e.target.value } : p))}
                  data-testid="user-form-password"
                />
              </div>
              <div className="field">
                <label htmlFor="ud-pass2">{t('确认口令')}</label>
                <TextInput
                  id="ud-pass2"
                  type="password"
                  autoComplete="new-password"
                  placeholder={t('（再输入一次）')}
                  value={f.password2}
                  disabled={readOnly}
                  onChange={(e) => setF((p) => (p ? { ...p, password2: e.target.value } : p))}
                  aria-invalid={passMismatch || undefined}
                  data-testid="user-form-password2"
                />
                {passMismatch && <p className="field-error" role="alert">{t('两次输入的口令不一致')}</p>}
              </div>
            </div>
            <div className="form-section">
              <h4>{t('相关组')}</h4>
              <p className="field-hint">{t('勾选即加入（右列）；保存后即时生效——移出组即失去该组授权，无需重登。')}</p>
              {groups.status === 'loading' && <StateSkeleton lines={2} />}
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
                    availableLabel={t('可选组')}
                    selectedLabel={t('已选组')}
                    itemTestid={(g) => `user-form-group-${g}`}
                  />
                </div>
              )}
              {groups.status !== 'loading' && groups.status !== 'ok' && (
                <p className="field-hint">{t('组列表不可用（')}{groups.error?.message}{t('）。')}</p>
              )}
            </div>
            {serverError && (
              <AlertBox severity="error" testid="user-form-error">
                <div className="font-medium">{t('保存失败（HTTP')} {serverError.status || t('网络')}{t('）')}</div>
                <div className="mt-1 break-all font-mono text-aux opacity-90" lang="en">{serverError.message}</div>
              </AlertBox>
            )}
            <div className="form-actions">
              <Button variant="outline" size="sm" onClick={() => navigate('/admin/security/users')}>{t('取消')}</Button>
              <Button
                variant="outline"
                size="sm"
                disabled={!dirty || submitting}
                onClick={() => d && setF(editFromDetail(d))}
              >
                {t('重置')}
              </Button>
              <Button
                size="sm"
                disabled={!dirty || f.email.trim() === '' || passMismatch || submitting || readOnly}
                title={readOnly ? t('只读管理员：用户编辑是管理面写操作（服务端 403）') : undefined}
                onClick={() => void submit()}
                data-testid="user-form-submit"
              >
                {submitting ? t('保存中…') : t('保存')}
              </Button>
            </div>
          </>
        )}
      </section>

      <section className="card" data-testid="user-perms">
        <h3>{t('用户权限矩阵')}</h3>
        <p className="field-hint">{t('只读汇总（来源 = 各 permission target 的直接行与经组行）——变更入口在权限编辑器。')}</p>
        {targets.status === 'loading' && <StateSkeleton lines={3} />}
        {targets.status === 'ok' && (
          <PermSummaryTable
            rows={permRows ?? []}
            rowTestidPrefix="user-perm"
            emptyHint={t('该用户未获得任何 permission target 授权（直接与经组均为空）。')}
          />
        )}
        {targets.status !== 'loading' && targets.status !== 'ok' && (
          <p className="field-hint">{t('权限汇总不可用（')}{targets.error?.message}{t('）。')}</p>
        )}
      </section>

      <section className="card" data-testid="user-facts">
        <h3>{t('账户信息')}</h3>
        <div className="kv">
          <span className="k">realm</span>
          <span className="font-mono" lang="en">{d?.realm ?? '—'}</span>
        </div>
        <div className="kv">
          <span className="k">{t('角色')}</span>
          <span className="font-mono" lang="en" data-testid="user-facts-role">{baseRole ?? '—'}</span>
        </div>
        <div className="kv">
          <span className="k">Status</span>
          <span>{d ? <StatusLabel enabled={d.enabled} /> : '—'}</span>
        </div>
        <div className="kv">
          <span className="k">{t('最近登录')}</span>
          <span>{d?.lastLoggedIn ? <span className="font-mono" lang="en">{d.lastLoggedIn}</span> : t('—（尚未登录）')}</span>
        </div>
        <div className="kv">
          <span className="k">API URI</span>
          <span className="font-mono break-all" lang="en" style={{ overflowWrap: 'anywhere' }}>
            {d ? `/binflow/api/security/users/${d.name}` : '—'}
          </span>
        </div>
        {admin && (
          <div className="mt-4 rounded-md border border-destructive/50 px-4 py-3" data-testid="user-danger-zone">
            <div className="mb-0.5 text-dense font-semibold text-destructive">{t('危险区')}</div>
            <p className="mb-2 max-w-[72ch] text-dense text-2">
              {t('删除不可恢复（组员/授权/token/会话同事务级联；审计保留）。人员离场的可逆路径是')}<b>{t('禁用')}</b>{t('（选项区）——删除仅用于账号彻底清退。')}
            </p>
            <Button
              variant="outline"
              size="sm"
              className="border-destructive/50 text-destructive hover:bg-destructive/10"
              disabled={deleteBlocked !== undefined}
              title={deleteBlocked}
              onClick={() => void deleteUser(name)}
              data-testid="user-delete"
            >
              {t('删除用户')}
            </Button>
          </div>
        )}
      </section>
    </div>
  )
}
