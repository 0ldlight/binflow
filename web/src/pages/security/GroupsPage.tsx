import { useState } from 'react'
import { Link } from 'react-router-dom'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { useConfirm } from '../../components/ConfirmDialog'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, errText } from '../../lib/api'
import { useAsync } from '../../lib/useAsync'
import './security.css'
import { deleteGroup, listGroups, parseReferencedTargets, putGroup, validateGroupName } from './api'

// 组页（console-ux §3.2 /security/groups；SE-01~04）：CRUD + 成员维护入口
// （成员在用户详情页维护——SE-06 双端点分工，本页不代持）。
//
// W33c 核心：删除被 permission target 引用的组 → 409，message 列 target
// 名。UI 呈现：行内冲突面板（服务端原文 mono + 解析出的 target 名链接到
// 权限编辑器，解除引用后重删即成——「不撞墙」）。

interface Conflict {
  group: string
  message: string
  targets: string[]
}

function GroupForm({
  initialName,
  initialDescription,
  mode,
  onDone,
  onCancel,
}: {
  initialName: string
  initialDescription: string
  mode: 'create' | 'edit'
  onDone: () => void
  onCancel: () => void
}) {
  const toast = useToast()
  const [name, setName] = useState(initialName)
  const [description, setDescription] = useState(initialDescription)
  const [submitting, setSubmitting] = useState(false)
  const [serverError, setServerError] = useState<ApiError | null>(null)

  const nameErr = mode === 'create' ? validateGroupName(name.trim()) : null
  const canSubmit = mode === 'edit' || (name.trim() !== '' && nameErr === null)
  const changed = mode === 'create' || description !== initialDescription

  const submit = async () => {
    setServerError(null)
    setSubmitting(true)
    try {
      await putGroup(name.trim(), description.trim())
      toast.success(mode === 'create' ? `组 ${name.trim()} 已创建` : `组 ${name.trim()} 的描述已更新`)
      onDone()
    } catch (err) {
      setServerError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <section className="card inline-form" data-testid="group-form" aria-label={mode === 'create' ? '创建组' : '编辑组'}>
      <h3>{mode === 'create' ? '创建组' : `编辑组 · ${initialName}`}</h3>
      <div className="field">
        <label htmlFor="gf-name">组名{mode === 'edit' ? '（不可变）' : ''}</label>
        <input
          id="gf-name"
          className="mono-input"
          value={mode === 'create' ? name : initialName}
          disabled={mode === 'edit'}
          onChange={(e) => setName(e.target.value)}
          placeholder="qa-team"
          aria-invalid={!!nameErr}
          data-testid="group-form-name"
          lang="en"
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
        <textarea
          id="gf-desc"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="用途、负责人…"
          data-testid="group-form-description"
        />
      </div>
      {serverError && (
        <div className="form-error" data-testid="group-form-error" role="alert">
          <div className="headline">保存失败（HTTP {serverError.status || '网络'}）</div>
          <div className="raw" lang="en">
            {serverError.message}
          </div>
        </div>
      )}
      <div className="form-actions">
        <button type="button" className="btn" onClick={onCancel}>
          取消
        </button>
        <button
          type="button"
          className="btn primary"
          disabled={!canSubmit || !changed || submitting}
          onClick={() => void submit()}
          data-testid="group-form-submit"
        >
          {submitting ? '保存中…' : mode === 'create' ? '创建组' : '保存变更'}
        </button>
      </div>
    </section>
  )
}

export default function GroupsPage() {
  const { session } = useAuth()
  const admin = session?.admin ?? false
  const toast = useToast()
  const confirm = useConfirm()
  const state = useAsync(listGroups, [])

  const [form, setForm] = useState<{ mode: 'create' | 'edit'; name: string; description: string } | null>(null)
  const [conflict, setConflict] = useState<Conflict | null>(null)

  const doDelete = async (name: string, description: string) => {
    const ok = await confirm({
      title: `删除组 ${name}`,
      body: (
        <div>
          <p>
            将删除组 <span className="mono" lang="en">{name}</span>
            {description ? `（${description}）` : ''}。成员关系随之解除；组成员基于该组的授权即时失效。
          </p>
          <p className="text-muted" style={{ fontSize: 12 }}>
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
          <button
            type="button"
            className="btn primary"
            onClick={() => setForm({ mode: 'create', name: '', description: '' })}
            data-testid="groups-create"
          >
            ＋ 创建组
          </button>
        )}
      </div>

      {form && (
        <GroupForm
          mode={form.mode}
          initialName={form.name}
          initialDescription={form.description}
          onDone={() => {
            setForm(null)
            state.reload()
          }}
          onCancel={() => setForm(null)}
        />
      )}

      {conflict && (
        <div className="conflict-panel" data-testid="group-delete-reason" role="alert">
          <div className="headline">无法删除组 <span className="mono" lang="en">{conflict.group}</span>——它正被 permission target 引用</div>
          <div className="raw" lang="en">
            {conflict.message}
          </div>
          <div className="targets">
            {conflict.targets.length > 0 && <span className="text-2">解除引用（编辑后移除该组主体）：</span>}
            {conflict.targets.map((t) => (
              <Link key={t} className="btn" to={`/security/permissions/${encodeURIComponent(t)}`}>
                <span className="mono" lang="en">
                  {t}
                </span>
              </Link>
            ))}
            <button type="button" className="btn" onClick={() => setConflict(null)} data-testid="group-delete-dismiss">
              稍后再试
            </button>
          </div>
        </div>
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
        ((state.data ?? []).length === 0 ? (
          admin ? (
            <EmptyState
              message="还没有组"
              hint="组的授权经 permission target 生效（组行 × read/write/delete 并集）。"
            />
          ) : (
            <EmptyState message="还没有组" />
          )
        ) : (
          <table className="table" data-testid="groups-table">
            <thead>
              <tr>
                <th scope="col">组名</th>
                <th scope="col">描述</th>
                <th scope="col">操作</th>
              </tr>
            </thead>
            <tbody>
              {(state.data ?? []).map((g) => (
                <tr key={g.name} data-testid={`group-row-${g.name}`}>
                  <td>
                    <span className="mono" lang="en">
                      {g.name}
                    </span>{' '}
                    <CopyButton value={g.name} label={`组名 ${g.name}`} />
                  </td>
                  <td className="wrap" style={{ maxWidth: 420, color: 'var(--bf-text-2)' }}>
                    {g.description || '—'}
                  </td>
                  <td>
                    {admin && (
                      <span style={{ display: 'inline-flex', gap: 8 }}>
                        <button
                          type="button"
                          className="btn"
                          onClick={() => setForm({ mode: 'edit', name: g.name, description: g.description })}
                          data-testid={`group-edit-${g.name}`}
                        >
                          编辑
                        </button>
                        <button
                          type="button"
                          className="btn danger"
                          onClick={() => void doDelete(g.name, g.description)}
                          data-testid={`group-delete-${g.name}`}
                        >
                          删除
                        </button>
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ))}
    </div>
  )
}
