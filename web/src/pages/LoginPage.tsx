import { useState } from 'react'
import type { FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'

import { useAuth } from '../app/AuthContext'
import { ApiError, errText } from '../lib/api'

// 登录页（console-ux §4.1 / FR-23 W09）：独立布局（无侧导航壳）；
// 用户名 + 口令；行内 401 错误（文案不泄露存在性——服务端本来就是统一
// 401「invalid credentials」，前端再收敛为固定中文文案）；提交 loading；
// 成功跳 return；已登录访问 /login 直接放行进应用。

export default function LoginPage() {
  const { status, login } = useAuth()
  const location = useLocation()
  const navigate = useNavigate()

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const params = new URLSearchParams(location.search)
  const rawReturn = params.get('return') ?? '/'
  // 只接受应用内路径，防开放跳转
  const safeReturn = rawReturn.startsWith('/') && !rawReturn.startsWith('//') ? rawReturn : '/'

  if (status === 'authenticated') {
    return <Navigate to={safeReturn} replace />
  }

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!username || !password || submitting) return
    setSubmitting(true)
    setError('')
    try {
      await login(username, password)
      navigate(safeReturn, { replace: true })
    } catch (err) {
      // 401 统一呈现（不区分用户不存在/口令错误）；其他错误如实给 message
      if (err instanceof ApiError && err.status === 401) {
        setError('用户名或密码错误')
      } else {
        setError(`登录失败：${errText(err)}`)
      }
      setSubmitting(false)
    }
  }

  const canSubmit = username.trim() !== '' && password !== '' && !submitting

  return (
    <div className="login-page" data-testid="login-page">
      <div className="login-brand">
        <h1>
          BinFlow <span aria-hidden="true">◆</span>
        </h1>
        <p>制品仓库控制台</p>
      </div>
      <form className="login-card" onSubmit={(e) => void onSubmit(e)}>
        <div className="field">
          <label htmlFor="login-username">用户名</label>
          <input
            id="login-username"
            className="mono"
            data-testid="login-username"
            name="username"
            autoComplete="username"
            autoFocus
            spellCheck={false}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </div>
        <div className="field">
          <label htmlFor="login-password">密码</label>
          <input
            id="login-password"
            data-testid="login-password"
            name="password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>
        {error && (
          <p className="field-error" data-testid="login-error" role="alert">
            {error}
          </p>
        )}
        <div className="actions">
          <button type="submit" className="btn primary" data-testid="login-submit" disabled={!canSubmit}>
            {submitting ? '登录中…' : '登录'}
          </button>
        </div>
      </form>
      <p className="login-note">管理面需认证。CI 与脚本请使用 API Token。</p>
    </div>
  )
}
