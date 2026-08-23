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
import { getUser, listGroups, updateUser } from './api'
import type { UserDetail } from './api'

// 用户详情/编辑（console-ux §3.2 /security/users/:name）：email/角色/组员
// 维护 + 口令重置入口（AC③）。写面走 POST /{name} 部分更新——指针语义下
// 仅携带改动字段（groups:[] 是显式清空，不是「未提及」）。
//
// 角色（M7 FR-66）：三值下拉（wire snake：user/readonly_admin/admin），只走
// adminRole 通道、不与 admin 布尔混发（同时发且不一致 → 服务端 400）。
// readonly_admin 会话进入本页 = 只读呈现：编辑控件禁用（服务端 403 兜底，
// UI 无绕过——服务端是唯一守门）。
//
// 契约缺口（登记漂移）：后端无「删除用户」端点（SE 域仅 GET/PUT/POST）；
// 禁用位（internalPasswordDisabled/disableUIAccess）为回显字段且恒 false。
// UI 不伪造入口。

/** 下拉显示名（wire 值严格 snake——选项 value 即 wire，显示名仅呈现层） */
const ROLE_LABEL: Record<AdminRole, string> = {
  user: '普通用户（user）——内容面按 permission target 授权',
  readonly_admin: '只读管理员（readonly_admin）——管理面全量只读',
  admin: '管理员（admin）——管理面全权',
}

interface EditState {
  email: string
  role: AdminRole
  groups: string[]
  password: string
}

function editFromDetail(d: UserDetail): EditState {
  return { email: d.email, role: normalizeAdminRole(d.adminRole, d.admin), groups: [...d.groups], password: '' }
}

export default function UserDetailPage() {
  const { name = '' } = useParams<{ name: string }>()
  const { session } = useAuth()
  const readOnly = isReadOnlyAdmin(session)
  const toast = useToast()
  const detail = useAsync(() => getUser(name), [name])
  const groups = useAsync(listGroups, [])
  const [f, setF] = useState<EditState | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [serverError, setServerError] = useState<ApiError | null>(null)

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
              <Link className="btn" to="/security/users">
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
  const dirty =
    !!f &&
    !!d &&
    (f.email.trim() !== d.email ||
      f.role !== baseRole ||
      JSON.stringify([...f.groups].sort()) !== JSON.stringify([...d.groups].sort()) ||
      f.password !== '')

  const submit = async () => {
    if (!f || !d) return
    setServerError(null)
    setSubmitting(true)
    try {
      const body: Record<string, unknown> = { name: d.name }
      if (f.email.trim() !== d.email) body.email = f.email.trim()
      // 角色只走 adminRole 通道（与 admin 布尔混发不一致 → 服务端 400）
      if (f.role !== baseRole) body.adminRole = f.role
      if (JSON.stringify([...f.groups].sort()) !== JSON.stringify([...d.groups].sort())) body.groups = f.groups
      if (f.password !== '') body.password = f.password
      await updateUser(d.name, body)
      toast.success(`用户 ${d.name} 已更新`)
      setF((p) => (p ? { ...p, password: '' } : p))
      detail.reload()
    } catch (err) {
      setServerError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div data-testid="user-detail-page">
      <div className="page-header">
        <h2>
          用户 · <span className="mono" lang="en">{name}</span>
        </h2>
        <Link className="btn" to="/security/users">
          ← 返回列表
        </Link>
      </div>

      <section className="card inline-form" data-testid="user-form" aria-label="编辑用户">
        <h3>编辑</h3>
        <div className="field">
          <label>用户名（不可变）</label>
          <div>
            <span className="mono" lang="en">
              {name}
            </span>{' '}
            <CopyButton value={name} label={`用户名 ${name}`} />
          </div>
        </div>
        {f && (
          <>
            {readOnly && (
              <p className="admin-note" data-testid="user-form-readonly-note">
                ⓘ 只读管理员（readonly_admin）视角：用户编辑是管理面写操作，本页为只读呈现
                （服务端 403 兜底，UI 不代持判定）。
              </p>
            )}
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
              <label htmlFor="ud-role">角色（闭集三值——wire 值即选项值）</label>
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
            <div className="field" style={{ maxWidth: 560 }}>
              <label>组成员（保存后即时生效——移出组即失去该组授权，无需重登）</label>
              {groups.status === 'loading' && <Skeleton lines={2} />}
              {groups.status === 'ok' && (
                <div className="sec-pick" data-testid="user-form-groups">
                  {(groups.data ?? []).length === 0 && <span className="empty">实例还没有组。</span>}
                  {(groups.data ?? []).map((g) => (
                    <label key={g.name}>
                      <input
                        type="checkbox"
                        checked={f.groups.includes(g.name)}
                        disabled={readOnly}
                        onChange={(e) =>
                          setF((p) =>
                            p
                              ? {
                                  ...p,
                                  groups: e.target.checked
                                    ? [...p.groups, g.name]
                                    : p.groups.filter((x) => x !== g.name),
                                }
                              : p,
                          )
                        }
                        data-testid={`user-form-group-${g.name}`}
                      />
                      <span className="mono" lang="en">
                        {g.name}
                      </span>
                      {g.description && <span className="text-muted" style={{ fontSize: 11 }}>{g.description}</span>}
                    </label>
                  ))}
                </div>
              )}
              {groups.status !== 'loading' && groups.status !== 'ok' && (
                <p className="field-hint">组列表不可用（{groups.error?.message}）。</p>
              )}
            </div>
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
            {serverError && (
              <div className="form-error" data-testid="user-form-error" role="alert">
                <div className="headline">保存失败（HTTP {serverError.status || '网络'}）</div>
                <div className="raw" lang="en">
                  {serverError.message}
                </div>
              </div>
            )}
            <div className="form-actions">
              <button
                type="button"
                className="btn primary"
                disabled={!dirty || f.email.trim() === '' || submitting || readOnly}
                title={readOnly ? '只读管理员：用户编辑是管理面写操作（服务端 403）' : undefined}
                onClick={() => void submit()}
                data-testid="user-form-submit"
              >
                {submitting ? '保存中…' : '保存变更'}
              </button>
            </div>
          </>
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
          ⓘ 禁用与删除用户当前无 API 面（SE 域未定义写端点）——如需移除访问，先清空其组员并撤回
          permission target 中的授权。
        </p>
      </section>
    </div>
  )
}
