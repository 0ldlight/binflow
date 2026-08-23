import { useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from 'react-router-dom'

import { useToast } from '../app/ToastContext'
import { apiText, errText } from '../lib/api'

// 编辑档案（console-m8 §6.5——Artifactory /ui/user_profile 的 BinFlow
// 对齐面，T-239 自设置页拆分）：
//
// - 认证设置 = 修改口令（自 /settings 平移，authenticated 全员可用；
//   PUT /api/security/password 的错误体走用户管理纯文本层，统一由 api
//   层解析成 message 行内呈现——auth-shell W 腿的 password-* 锚随表单
//   整体迁址，锚名不变）。
// - API Token = 说明 + 文档链接 + 管理 Tokens 页入口（§6.5[2]：Identity
//   Tokens 表不建——R6 未落地，无影子入口；readonly_admin 自铸 200 但
//   管理面 Tokens 页 admin 门——按现役门呈现入口，目标页自身收敛）。
// - step-up 相关交互不建（票面：console 铸造页未落地——T-219/T-242 域，
//   如实不占位；step-up 语义见 docs/user/admin/token-step-up.md）。

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
    <section className="card section" data-testid="profile-password">
      <h3>认证设置 · 修改口令</h3>
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

export default function ProfilePage() {
  return (
    <div data-testid="profile-page">
      <div className="page-header">
        <h2>编辑档案</h2>
      </div>
      <PasswordSection />
      <section className="card section" data-testid="profile-token">
        <h3>API Token</h3>
        <p className="text-2">
          CI 与脚本请使用 API Token（管理面签发需管理员；实例开启 step-up 时非 admin
          自铸需二次口令——见文档）。
        </p>
        <p>
          <a href="/binflow/docs/api-reference" target="_blank" rel="noopener noreferrer" data-testid="profile-token-docs">
            查看文档
          </a>
          <span aria-hidden="true"> · </span>
          <Link to="/admin/security/tokens" data-testid="profile-token-goto">
            去 Tokens 页 →
          </Link>
        </p>
      </section>
    </div>
  )
}
