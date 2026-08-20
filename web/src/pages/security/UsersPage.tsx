import { useState } from 'react'
import { Link } from 'react-router-dom'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, errText } from '../../lib/api'
import { useAsync } from '../../lib/useAsync'
import './security.css'
import { createUser, getUser, listGroups, listUsers, validateUserName } from './api'
import type { UserListItem } from './api'

// 用户列表（console-ux §3.2 /security/users；§3.6 安全全组 admin 面）。
// 列表端点是简形态 {name,uri,realm}（SE-05 双端点分工）；admin 位与组员
// 在详情端点——按 T-99 已用列先例（usage 逐仓拉取）行级独立请求独立到达，
// 失败降级 —（不伪造）。
//
// 403 收敛（§3.6.3）：L2——列表整体 403 呈现无权限卡（直链可达，导航组
// 已由 whoami 预收敛隐藏）；L4——创建按钮仅 admin 渲染。

function UserRow({ user }: { user: UserListItem }) {
  const detail = useAsync(() => getUser(user.name), [user.name])
  return (
    <tr data-testid={`user-row-${user.name}`}>
      <td>
        <Link className="row-link mono" to={`/security/users/${user.name}`} lang="en">
          {user.name}
        </Link>{' '}
        <CopyButton value={user.name} label={`用户名 ${user.name}`} />
      </td>
      <td>
        {detail.status === 'loading' && <span className="cell-pending" role="progressbar" aria-label="用户信息加载中" />}
        {detail.status === 'ok' && detail.data?.admin && <span className="badge warning">admin</span>}
        {detail.status === 'ok' && detail.data && !detail.data.admin && <span className="text-muted">—</span>}
        {detail.status !== 'loading' && detail.status !== 'ok' && (
          <span className="text-muted" title={detail.error?.message ?? '详情不可用'}>
            —
          </span>
        )}
      </td>
      <td className="wrap" style={{ maxWidth: 360 }}>
        {detail.status === 'loading' && <span className="cell-pending" role="progressbar" aria-label="组员加载中" />}
        {detail.status === 'ok' && detail.data &&
          (detail.data.groups.length === 0 ? (
            <span className="text-muted">—</span>
          ) : (
            <span className="sec-chips">
              {detail.data.groups.map((g) => (
                <span key={g} className="badge neutral mono" lang="en">
                  {g}
                </span>
              ))}
            </span>
          ))}
        {detail.status !== 'loading' && detail.status !== 'ok' && <span className="text-muted">—</span>}
      </td>
      <td className="text-2">{user.realm}</td>
    </tr>
  )
}

interface CreateState {
  name: string
  email: string
  password: string
  admin: boolean
  groups: string[]
}

const CREATE_INITIAL: CreateState = { name: '', email: '', password: '', admin: false, groups: [] }

function CreateUserForm({ onDone }: { onDone: () => void }) {
  const toast = useToast()
  const groups = useAsync(listGroups, [])
  const [f, setF] = useState<CreateState>(CREATE_INITIAL)
  const [submitting, setSubmitting] = useState(false)
  const [serverError, setServerError] = useState<ApiError | null>(null)

  const nameErr = validateUserName(f.name.trim())
  const canSubmit = f.name.trim() !== '' && nameErr === null && f.email.trim() !== '' && f.password !== '' && !submitting

  const submit = async () => {
    setServerError(null)
    setSubmitting(true)
    try {
      await createUser(f.name.trim(), {
        name: f.name.trim(),
        email: f.email.trim(),
        password: f.password,
        admin: f.admin,
        groups: f.groups,
      })
      toast.success(`用户 ${f.name.trim()} 已创建`)
      setF(CREATE_INITIAL)
      onDone()
    } catch (err) {
      // 服务端 400 文案原样行内（email/口令缺失、混合大小写、未知组）
      setServerError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <section className="card inline-form" data-testid="user-form" aria-label="创建用户">
      <h3>创建用户</h3>
      <div className="field">
        <label htmlFor="uf-name">用户名</label>
        <input
          id="uf-name"
          className="mono-input"
          value={f.name}
          onChange={(e) => setF((p) => ({ ...p, name: e.target.value }))}
          placeholder="bob"
          aria-invalid={!!nameErr}
          data-testid="user-form-name"
          lang="en"
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
        <label htmlFor="uf-email">Email（必填）</label>
        <input
          id="uf-email"
          type="email"
          value={f.email}
          onChange={(e) => setF((p) => ({ ...p, email: e.target.value }))}
          placeholder="bob@example.com"
          data-testid="user-form-email"
        />
      </div>
      <div className="field">
        <label htmlFor="uf-pass">初始口令（必填）</label>
        <input
          id="uf-pass"
          type="password"
          autoComplete="new-password"
          value={f.password}
          onChange={(e) => setF((p) => ({ ...p, password: e.target.value }))}
          data-testid="user-form-password"
        />
      </div>
      <label className="check-row">
        <input
          type="checkbox"
          checked={f.admin}
          onChange={(e) => setF((p) => ({ ...p, admin: e.target.checked }))}
          data-testid="user-form-admin"
        />
        admin（管理面全权；默认关闭）
      </label>
      <div className="field" style={{ maxWidth: 560 }}>
        <label>组成员（可选，多选；即时生效——移出组即失去该组授权）</label>
        {groups.status === 'loading' && <Skeleton lines={2} />}
        {groups.status === 'ok' && (
          <div className="sec-pick" data-testid="user-form-groups">
            {(groups.data ?? []).length === 0 && (
              <span className="empty">还没有组——先到「组」页创建（三步流第一步）。</span>
            )}
            {(groups.data ?? []).map((g) => (
              <label key={g.name}>
                <input
                  type="checkbox"
                  checked={f.groups.includes(g.name)}
                  onChange={(e) =>
                    setF((p) => ({
                      ...p,
                      groups: e.target.checked ? [...p.groups, g.name] : p.groups.filter((x) => x !== g.name),
                    }))
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
          <p className="field-hint">组列表不可用（{groups.error?.message}）；可先建用户，稍后在详情页入组。</p>
        )}
      </div>
      {serverError && (
        <div className="form-error" data-testid="user-form-error" role="alert">
          <div className="headline">创建失败（HTTP {serverError.status || '网络'}）</div>
          <div className="raw" lang="en">
            {serverError.message}
          </div>
        </div>
      )}
      <div className="form-actions">
        <button type="button" className="btn primary" disabled={!canSubmit} onClick={() => void submit()} data-testid="user-form-submit">
          {submitting ? '创建中…' : '创建用户'}
        </button>
      </div>
    </section>
  )
}

export default function UsersPage() {
  const { session } = useAuth()
  const admin = session?.admin ?? false
  const state = useAsync(listUsers, [])
  const [creating, setCreating] = useState(false)

  return (
    <div data-testid="users-page">
      <div className="page-header">
        <h2>用户</h2>
        {admin && (
          <button type="button" className="btn primary" onClick={() => setCreating((v) => !v)} data-testid="users-create">
            {creating ? '收起表单' : '＋ 创建用户'}
          </button>
        )}
      </div>

      {creating && <CreateUserForm onDone={state.reload} />}

      {state.status === 'loading' && <Skeleton lines={6} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'forbidden' && state.error && (
        <EmptyState
          message="无权限访问用户管理"
          hint="用户与组管理是管理员功能（管理面需 admin）。制品访问请使用搜索或仓库直链。"
        />
      )}
      {state.status === 'ok' &&
        ((state.data ?? []).length === 0 ? (
          admin ? (
            <EmptyState message="还没有用户" hint="点击「创建用户」建立第一个账号；CI 与脚本建议使用 API Token。" />
          ) : (
            <EmptyState message="还没有用户" />
          )
        ) : (
          <table className="table" data-testid="users-table">
            <thead>
              <tr>
                <th scope="col">用户名</th>
                <th scope="col">admin</th>
                <th scope="col">组</th>
                <th scope="col">realm</th>
              </tr>
            </thead>
            <tbody>
              {(state.data ?? []).map((u) => (
                <UserRow key={u.name} user={u} />
              ))}
            </tbody>
          </table>
        ))}
    </div>
  )
}
