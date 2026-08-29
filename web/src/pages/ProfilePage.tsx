import { useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from 'react-router-dom'

import Button from '@mui/material/Button'
import Paper from '@mui/material/Paper'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'

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
// T-344 批 D：残面换装——.card → Paper；.field + 裸 input + 手写 label →
// TextField 浮标 label（锚 password-* 落 input 本体）；.btn → Button。
// Tab 序（旧口令 → 新口令 → 确认 → 提交，浮标 label 非可聚焦元素）与
// Enter 隐式提交（原生 form + type=submit）零变化。

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
    <Paper component="section" className="card section" elevation={1} data-testid="profile-password">
      <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>
        认证设置 · 修改口令
      </Typography>
      <form onSubmit={(e) => void onSubmit(e)}>
        <Stack sx={{ gap: 'var(--bf-sp-3)', maxWidth: 420 }}>
          <TextField
            label="当前口令"
            size="small"
            type="password"
            value={oldPw}
            onChange={(e) => setOldPw(e.target.value)}
            slotProps={{ htmlInput: { 'data-testid': 'password-old', autoComplete: 'current-password' } }}
          />
          <TextField
            label="新口令"
            size="small"
            type="password"
            value={newPw}
            onChange={(e) => setNewPw(e.target.value)}
            slotProps={{ htmlInput: { 'data-testid': 'password-new', autoComplete: 'new-password' } }}
          />
          <TextField
            label="确认新口令"
            size="small"
            type="password"
            value={confirmPw}
            onChange={(e) => setConfirmPw(e.target.value)}
            slotProps={{ htmlInput: { 'data-testid': 'password-confirm', autoComplete: 'new-password' } }}
          />
          {error && (
            <p className="field-error" data-testid="password-error" role="alert">
              {error}
            </p>
          )}
          <Button type="submit" variant="contained" data-testid="password-submit" disabled={!canSubmit}>
            {saving ? '保存中…' : '修改口令'}
          </Button>
        </Stack>
      </form>
    </Paper>
  )
}

export default function ProfilePage() {
  return (
    <div data-testid="profile-page">
      <div className="page-header">
        <h2>编辑档案</h2>
      </div>
      <PasswordSection />
      <Paper component="section" className="card section" elevation={1} data-testid="profile-token">
        <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>
          API Token
        </Typography>
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
      </Paper>
    </div>
  )
}
