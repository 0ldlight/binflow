import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ADMIN_ROLES, ApiError, errText, isReadOnlyAdmin, normalizeAdminRole } from '../../lib/api'
import type { AdminRole } from '../../lib/api'
import { useAsync } from '../../lib/useAsync'
import './security.css'
import { TransferBox } from './TransferBox'
import { PermSummaryTable } from './widgets'
import { getUser, grantsOfUser, listGroups, listPermissionTargets, updateUser } from './api'
import type { UserDetail, UserUpdateBody } from './api'

// 用户编辑器（console-m8 §6.9 编辑态，T-237 重排）：分区形态 = 用户设置 /
// 选项（状态）/ 口令 / 相关组（双列穿梭）/ 用户权限矩阵（只读汇总）。
//
// 角色（M7 FR-66 / §7.1）：三值下拉，仅走 adminRole 通道（与 admin 布尔
// 混发不一致 → 服务端 400）；readonly_admin 视角禁用 + 说明行。
//
// enabled 翻转（§7.5 / T-224 非缺陷②）：表单全量提交形态——保存体总是
// 携带 enabled（POST 部分更新臂的指针语义：携带即写入）。契约漂移①：
// GET 无 enabled 回显，控件按「默认启用」呈现（建号即启用 + 极少禁用），
// 以勾选态为准写入，漂移已登记（后端补 echo 后本控件改回显驱动）。
//
// 删除用户：后端无 DELETE /security/users/{name}（SE 域仅 GET/PUT/POST，
// 契约冻结）——§6.9 Actions 菜单不建，不伪造入口。
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

/** 编辑初值：enabled 无回显（漂移①）——按 knownEnabled（本页写过的值，
 *  未写过 = 默认启用假设）；保存时显式落盘 */
function editFromDetail(d: UserDetail, enabled: boolean): EditState {
  return {
    email: d.email,
    role: normalizeAdminRole(d.adminRole, d.admin),
    enabled,
    groups: [...d.groups],
    password: '',
    password2: '',
  }
}

export default function UserDetailPage() {
  const { name = '' } = useParams<{ name: string }>()
  const { session } = useAuth()
  const readOnly = isReadOnlyAdmin(session)
  const toast = useToast()
  const detail = useAsync(() => getUser(name), [name])
  const groups = useAsync(listGroups, [])
  const targets = useAsync(listPermissionTargets, [])
  const [f, setF] = useState<EditState | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [serverError, setServerError] = useState<ApiError | null>(null)
  // enabled 的本地回显：GET 无该字段（漂移①）——本页写过的值即已知值，
  // 未写过按「建号即启用」假设。带外变更（CLI/迁移）不可见，已登记漂移。
  const [knownEnabled, setKnownEnabled] = useState(true)

  useEffect(() => {
    if (detail.status === 'ok' && detail.data) setF(editFromDetail(detail.data, knownEnabled))
  }, [detail.status, detail.data, knownEnabled])

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
              <Link className="btn" to="/admin/security/users">
                ← 返回用户列表
              </Link>
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
  const groupsEqual = (a: readonly string[], b: readonly string[]) =>
    JSON.stringify([...a].sort()) === JSON.stringify([...b].sort())
  const dirty =
    !!f &&
    !!d &&
    (f.email.trim() !== d.email ||
      f.role !== baseRole ||
      f.enabled !== knownEnabled ||
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
      if (body.enabled !== undefined) setKnownEnabled(body.enabled) // 本地回显（漂移①的 UI 侧补）
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
        <Link className="btn" to="/admin/security/users">
          ← 返回列表
        </Link>
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
                <input
                  id="ud-email"
                  type="email"
                  value={f.email}
                  disabled={readOnly}
                  onChange={(e) => setF((p) => (p ? { ...p, email: e.target.value } : p))}
                  data-testid="user-form-email"
                />
                {f.email.trim() === '' && <p className="field-error">email 不能为空（服务端 400）</p>}
              </div>
              <div className="field" style={{ maxWidth: 480 }}>
                <label htmlFor="ud-role">角色（三值闭集——wire 值即选项值）</label>
                <select
                  id="ud-role"
                  value={f.role}
                  disabled={readOnly}
                  onChange={(e) => setF((p) => (p ? { ...p, role: e.target.value as AdminRole } : p))}
                  data-testid="user-form-role"
                >
                  {ADMIN_ROLES.map((r) => (
                    <option key={r} value={r} data-testid={`user-form-role-${r}`}>
                      {ROLE_LABEL[r]}
                    </option>
                  ))}
                </select>
                <p className="field-hint">
                  角色变更即时生效并落 <span className="mono" lang="en">user.role.change</span> 审计；只读管理员对
                  permission target 短路（组合无效而非非法）。
                </p>
              </div>
            </div>
            <div className="form-section">
              <h4>选项</h4>
              <label className="check-row">
                <input
                  type="checkbox"
                  checked={f.enabled}
                  disabled={readOnly}
                  onChange={(e) => setF((p) => (p ? { ...p, enabled: e.target.checked } : p))}
                  data-testid="user-form-enabled"
                />
                启用（取消勾选 = 禁用账号——登录与写面全部拒绝）
              </label>
              <p className="field-hint">
                服务端暂无 enabled 回显（契约漂移已登记）：本控件按默认启用呈现，保存时以勾选状态写入。
              </p>
            </div>
            <div className="form-section">
              <h4>口令</h4>
              <div className="field">
                <label htmlFor="ud-pass">重置口令（可选——留空不改动；无需旧口令）</label>
                <input
                  id="ud-pass"
                  type="password"
                  autoComplete="new-password"
                  placeholder="（不改动）"
                  value={f.password}
                  disabled={readOnly}
                  onChange={(e) => setF((p) => (p ? { ...p, password: e.target.value } : p))}
                  data-testid="user-form-password"
                />
              </div>
              <div className="field">
                <label htmlFor="ud-pass2">确认口令</label>
                <input
                  id="ud-pass2"
                  type="password"
                  autoComplete="new-password"
                  placeholder="（再输入一次）"
                  value={f.password2}
                  disabled={readOnly}
                  onChange={(e) => setF((p) => (p ? { ...p, password2: e.target.value } : p))}
                  aria-invalid={passMismatch}
                  data-testid="user-form-password2"
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
              <div className="form-error" data-testid="user-form-error" role="alert">
                <div className="headline">保存失败（HTTP {serverError.status || '网络'}）</div>
                <div className="raw" lang="en">
                  {serverError.message}
                </div>
              </div>
            )}
            <div className="form-actions">
              <Link className="btn" to="/admin/security/users" data-testid="user-form-cancel">
                取消
              </Link>
              <button
                type="button"
                className="btn"
                disabled={!dirty || submitting}
                onClick={() => d && setF(editFromDetail(d, knownEnabled))}
                data-testid="user-form-reset"
              >
                重置
              </button>
              <button
                type="button"
                className="btn primary"
                disabled={!dirty || f.email.trim() === '' || passMismatch || submitting || readOnly}
                title={readOnly ? '只读管理员：用户编辑是管理面写操作（服务端 403）' : undefined}
                onClick={() => void submit()}
                data-testid="user-form-submit"
              >
                {submitting ? '保存中…' : '保存'}
              </button>
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
          <span className="k">最近登录</span>
          <span>{d?.lastLoggedIn ? <span className="mono" lang="en">{d.lastLoggedIn}</span> : '—（尚未登录）'}</span>
        </div>
        <div className="kv">
          <span className="k">API URI</span>
          <span className="mono wrap" lang="en" style={{ overflowWrap: 'anywhere' }}>
            {d ? `/binflow/api/security/users/${d.name}` : '—'}
          </span>
        </div>
        <p className="admin-note">
          ⓘ 删除用户当前无 API 面（SE 域未定义 DELETE 端点，契约冻结）——如需移除访问，先清空其组员并撤回
          permission target 中的授权，或禁用账号（选项区）。
        </p>
      </section>
    </div>
  )
}
