import { useState } from 'react'
import type { FormEvent } from 'react'

import { useAuth } from '../app/AuthContext'
import { useToast } from '../app/ToastContext'
import { apiText, errText, getHealth } from '../lib/api'
import type { HealthInfo } from '../lib/api'
import { useAsync } from '../lib/useAsync'
import { useVersion } from '../lib/useVersion'

// 设置页（console-ux §3.2 /settings）：实例信息（版本开放端点 + 健康行
// ——v1.1 §3.6.3 N1 收口：403 驱动而非 whoami admin 位硬编码，后端放宽
// 门时自动跟随，锚 settings-health）+ 修改口令（PUT /api/security/password
// ——错误体走用户管理纯文本层，统一由 api 层解析成 message 行内呈现）。
// 匿名读开关状态：后端无查询端点（M4 缺口），不伪造数据，见工作日志。

function InstanceSection() {
  const version = useVersion()
  const { session } = useAuth()
  const health = useAsync<HealthInfo>(getHealth, [])
  const admin = session?.admin ?? false

  return (
    <section className="card section" data-testid="settings-instance">
      <h3>实例信息</h3>
      <div className="kv">
        <span className="k">产品</span>
        <span className="mono" lang="en">
          {version?.product ?? '—'}
        </span>
      </div>
      <div className="kv">
        <span className="k">版本</span>
        <span className="mono" lang="en">
          {version ? `v${version.version}` : '—'}
        </span>
      </div>
      <div className="kv">
        <span className="k">修订</span>
        <span className="mono" lang="en">
          {version?.revision || '—'}
        </span>
      </div>
      <div className="kv">
        <span className="k">当前用户</span>
        <span>
          {session?.username}
          {admin ? '（admin）' : ''}
        </span>
      </div>
      {health.status !== 'forbidden' && (
        <div className="kv" data-testid="settings-health">
          <span className="k">健康</span>
          {health.status === 'loading' && <span className="text-2">检查中…</span>}
          {health.status === 'error' && health.error && <span style={{ color: 'var(--bf-danger)' }}>{health.error.message}</span>}
          {health.status === 'ok' && health.data && (
            <span>
              <span className={`status-dot ${health.data.status === 'ok' ? 'ok' : 'err'}`} aria-hidden="true" />
              {health.data.status}
            </span>
          )}
        </div>
      )}
    </section>
  )
}

function PasswordSection() {
  const toast = useToast()
  const [oldPw, setOldPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [confirmPw, setConfirmPw] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const canSubmit = oldPw !== '' && newPw !== '' && confirmPw !== '' && !saving

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    if (newPw !== confirmPw) {
      setError('两次输入的新口令不一致')
      return
    }
    setSaving(true)
    try {
      await apiText('/security/password', {
        method: 'PUT',
        body: { oldPassword: oldPw, newPassword: newPw },
      })
      toast.success('口令修改成功')
      setOldPw('')
      setNewPw('')
      setConfirmPw('')
    } catch (err) {
      // 服务端纯文本层文案（如 Incorrect username/password / New password
      // has to be different from the old one）原样行内呈现
      setError(errText(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <section className="card section" data-testid="settings-password">
      <h3>修改口令</h3>
      <form onSubmit={(e) => void onSubmit(e)}>
        <div className="field">
          <label htmlFor="pw-old">当前口令</label>
          <input
            id="pw-old"
            data-testid="password-old"
            type="password"
            autoComplete="current-password"
            value={oldPw}
            onChange={(e) => setOldPw(e.target.value)}
          />
        </div>
        <div className="field">
          <label htmlFor="pw-new">新口令</label>
          <input
            id="pw-new"
            data-testid="password-new"
            type="password"
            autoComplete="new-password"
            value={newPw}
            onChange={(e) => setNewPw(e.target.value)}
          />
        </div>
        <div className="field">
          <label htmlFor="pw-confirm">确认新口令</label>
          <input
            id="pw-confirm"
            data-testid="password-confirm"
            type="password"
            autoComplete="new-password"
            value={confirmPw}
            onChange={(e) => setConfirmPw(e.target.value)}
          />
        </div>
        {error && (
          <p className="field-error" data-testid="password-error" role="alert">
            {error}
          </p>
        )}
        <button type="submit" className="btn primary" data-testid="password-submit" disabled={!canSubmit}>
          {saving ? '保存中…' : '修改口令'}
        </button>
      </form>
    </section>
  )
}

export default function SettingsPage() {
  return (
    <div data-testid="settings">
      <div className="page-header">
        <h2>设置</h2>
      </div>
      <InstanceSection />
      <PasswordSection />
    </div>
  )
}
