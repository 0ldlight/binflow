import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'

import { useAuth } from '../app/AuthContext'
import { ApiError, errText } from '../lib/api'

// 登录页（console-ux §4.1 / FR-23 W09；T-158 增 SSO）：独立布局（无侧
// 导航壳）；用户名 + 口令；行内 401 错误（文案不泄露存在性——服务端本来
// 就是统一 401「invalid credentials」，前端再收敛为固定中文文案）；提交
// loading；成功跳 return；已登录访问 /login 直接放行进应用。
//
// SSO（T-158 / FR-54 OD-01）：oidc.enabled=true 的实例上渲染「使用 SSO
// 登录」，点击后顶层导航 GET /binflow/api/v1/oidc/login，由后端 302 到
// IdP（Authorization Code + PKCE，事务全在后端 cookie/重定向里，前端不
// 经手任何令牌）。disabled 实例该端点整体 404（FR-54-AC6/H29），按钮
// 相应隐藏——实例的 SSO 姿态不可探测，前端也不暴露。
//
// LDAP（AC②）：目录用户与本地用户共用下面同一张表单，后端先本地后
// LDAP 回退（T-156/T-157）；前端零分支、零提示差异。

/** SSO 浏览器入口（后端契约：GET → 302 IdP；disabled → 404 E-26） */
const OIDC_LOGIN_URL = '/binflow/api/v1/oidc/login'

/** 探测结论：redirect = 已启用（端点 302）；absent = 未启用（404）；unknown = 无法断言 */
type OIDCAvailability = 'redirect' | 'absent' | 'unknown'

// 探测 SSO 是否启用：GET 登录路由并要求 redirect:'manual'，302 会被
// fetch 截为 opaqueredirect（type='opaqueredirect'、status=0）——浏览器
// 不离开页面、也绝不触达 IdP，我们只读取「是否发生重定向」这一事实。
// 当前没有公开的「认证方式清单」端点（如 GET /api/v1/auth/methods），
// 这是对 T-157 实际行为最稳的探测；后端补出显式端点后应迁移过去。
// 副作用说明：对启用实例的探测会让后端铸造一对 state+PKCE 并落下
// 一次性事务 cookie（10 分钟过期，真实点击时会被新事务覆盖，无害）。
// 不走 lib/api 的 rawRequest：数据层面向 JSON 面（非 2xx 一律抛
// ApiError、fetch 默认跟随重定向），此处需要的是导航语义的原始响应。
async function probeOIDCLogin(signal: AbortSignal): Promise<OIDCAvailability> {
  try {
    const res = await fetch(OIDC_LOGIN_URL, {
      method: 'GET',
      redirect: 'manual',
      credentials: 'same-origin',
      headers: { Accept: 'application/json, text/plain, */*' },
      signal,
    })
    if (res.type === 'opaqueredirect' || res.status === 0) return 'redirect'
    if (res.status === 404) return 'absent'
    return 'unknown' // 5xx 等：端点在但没在放行——按不可用收敛
  } catch {
    return 'unknown' // 网络失败：同样无法断言启用
  }
}

export default function LoginPage() {
  const { status, login } = useAuth()
  const location = useLocation()
  const navigate = useNavigate()

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  // SSO 三态：probing（探测中，不渲染按钮避免闪现）→ on / off
  const [sso, setSso] = useState<'probing' | 'on' | 'off'>('probing')
  const [ssoBusy, setSsoBusy] = useState(false)
  const [ssoError, setSsoError] = useState('')

  const params = new URLSearchParams(location.search)
  const rawReturn = params.get('return') ?? '/'
  // 只接受应用内路径，防开放跳转
  const safeReturn = rawReturn.startsWith('/') && !rawReturn.startsWith('//') ? rawReturn : '/'

  useEffect(() => {
    // 挂载即探测一次：oidc disabled 的实例（默认形态）按钮永不出现。
    const ac = new AbortController()
    probeOIDCLogin(ac.signal).then((avail) => {
      if (ac.signal.aborted) return
      setSso(avail === 'redirect' ? 'on' : 'off')
    })
    return () => ac.abort()
  }, [])

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
      // 401 统一呈现（不区分本地/LDAP/用户不存在/口令错误）；其他错误如实给 message
      if (err instanceof ApiError && err.status === 401) {
        setError('用户名或密码错误')
      } else {
        setError(`登录失败：${errText(err)}`)
      }
      setSubmitting(false)
    }
  }

  const onSSO = async () => {
    if (sso !== 'on' || ssoBusy) return
    setSsoBusy(true)
    setSsoError('')
    // 点击时复核一次：登录页可能已停留多时，实例或已重启/改配置。
    // 仍是 302 才放行顶层导航，避免把用户丢到裸 404/5xx JSON 页上。
    const avail = await probeOIDCLogin(new AbortController().signal)
    if (avail === 'redirect') {
      // 顶层导航：后端 302 → IdP，往返后回到 /binflow/ui/（带 session）。
      // 页面即将卸载，保持 busy 态防止双击重复发起。
      window.location.assign(OIDC_LOGIN_URL)
      return
    }
    setSsoBusy(false)
    if (avail === 'absent') {
      // 端点已消失：SSO 被关闭，收敛回密码表单
      setSso('off')
      setSsoError('SSO 登录未启用，请使用用户名密码登录')
    } else {
      setSsoError('SSO 登录暂不可用（服务异常），请稍后重试或使用用户名密码登录')
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
        {sso === 'on' && (
          <>
            <div className="login-divider" aria-hidden="true">
              或
            </div>
            <button
              type="button"
              className="btn"
              data-testid="login-sso"
              disabled={ssoBusy}
              onClick={() => void onSSO()}
            >
              {ssoBusy ? '正在跳转…' : '使用 SSO 登录'}
            </button>
          </>
        )}
        {ssoError && (
          <p className="field-error login-sso-error" data-testid="sso-error" role="alert">
            {ssoError}
          </p>
        )}
      </form>
      {/* 常驻说明 + 文档链接（console-m8 §6.1 [6]）。链接带下划线：弱化色
          说明文字中的链接需非色彩信号区分（axe link-in-text-block） */}
      <p className="login-note">
        管理面需认证。CI 与脚本请使用 API Token。
        <a
          href="/binflow/docs/api-reference"
          target="_blank"
          rel="noopener noreferrer"
          data-testid="login-docs"
          style={{ marginLeft: 8, textDecoration: 'underline', textUnderlineOffset: 2 }}
        >
          查看文档
        </a>
      </p>
    </div>
  )
}
