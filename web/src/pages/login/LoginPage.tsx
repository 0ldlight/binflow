// 登录页（console-ux §4.1 / FR-23 W09——P2 新栈重写）。
//
// B1 契约漂移修正（frontend-rewrite-audit §7）：SSO 三态探测的 302 hack
// （fetch redirect:'manual'）退役——直接消费 GET /api/v1/auth/methods
// （{password, oidc, ldap} 三布尔，匿名可达，T-179 起就位）。LDAP 位
// 顺带到手（展示语义：目录用户与本地用户共用同一表单，零分支——位仅
// 作非交互注记）。
//
// 语义承接（audit §2.1 login 行）：
// - 独立布局（无壳）；原生 form Enter 隐式提交；行内 401 固定文案（不泄
//   存在性）；提交 loading；成功跳 return（开放跳转防护）；已登录访问
//   /login 直接放行。
// - SSO 点击时二次复核（methods 再读一次——页面可能已停留多时）；复核
//   仍是 on 才顶层导航 GET /api/v1/oidc/login。
// - 锚族原样（冻结锚）：login-page / brand-login-lockup / login-username
//   / login-password / login-submit / login-error / login-sso / sso-error
//   / login-docs + .login-divider 类钩。
import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useAuth } from '@/app/AuthContext'
import { BrandLockup } from '@/components/BrandLogo'
import { ApiError, apiJSON, errText } from '@/lib/api'
import { tr } from '@/i18n'

const t = tr('console')

/** SSO 浏览器入口（后端契约：GET → 302 IdP；disabled → 404 E-26） */
const OIDC_LOGIN_URL = '/binflow/api/v1/oidc/login'

/** auth/methods 能力面（audit §2.15）：{password, oidc, ldap} 三布尔 */
interface AuthMethods {
  password: boolean
  oidc: boolean
  ldap: boolean
}

async function fetchAuthMethods(signal: AbortSignal): Promise<AuthMethods | null> {
  try {
    return await apiJSON<AuthMethods>('/v1/auth/methods', { signal, silent401: true })
  } catch {
    return null // 能力面不可达：按最保守形态（仅密码表单）收敛
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
  // SSO 三态：probing（能力面在途，不渲染按钮避免闪现）→ on / off
  const [sso, setSso] = useState<'probing' | 'on' | 'off'>('probing')
  const [ldap, setLdap] = useState(false)
  const [ssoBusy, setSsoBusy] = useState(false)
  const [ssoError, setSsoError] = useState('')

  const params = new URLSearchParams(location.search)
  const rawReturn = params.get('return') ?? '/'
  // 只接受应用内路径，防开放跳转
  const safeReturn = rawReturn.startsWith('/') && !rawReturn.startsWith('//') ? rawReturn : '/'

  useEffect(() => {
    const ac = new AbortController()
    fetchAuthMethods(ac.signal).then((methods) => {
      if (ac.signal.aborted || !methods) {
        if (!ac.signal.aborted) setSso('off')
        return
      }
      setSso(methods.oidc ? 'on' : 'off')
      setLdap(methods.ldap === true)
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
        setError(t('用户名或密码错误'))
      } else {
        setError(t('登录失败：{v1}', { v1: errText(err) }))
      }
      setSubmitting(false)
    }
  }

  const onSSO = async () => {
    if (sso !== 'on' || ssoBusy) return
    setSsoBusy(true)
    setSsoError('')
    // 点击时复核一次：登录页可能已停留多时，实例或已重启/改配置。
    const methods = await fetchAuthMethods(new AbortController().signal)
    if (methods?.oidc) {
      // 顶层导航：后端 302 → IdP，往返后回到 /binflow/ui/（带 session）。
      // 页面即将卸载，保持 busy 态防止双击重复发起。
      window.location.assign(OIDC_LOGIN_URL)
      return
    }
    setSsoBusy(false)
    if (methods && !methods.oidc) {
      // 端点已消失：SSO 被关闭，收敛回密码表单
      setSso('off')
      setSsoError(t('SSO 登录未启用，请使用用户名密码登录'))
    } else {
      setSsoError(t('SSO 登录暂不可用（服务异常），请稍后重试或使用用户名密码登录'))
    }
  }

  const canSubmit = username.trim() !== '' && password !== '' && !submitting

  return (
    <div
      data-testid="login-page"
      className="flex min-h-screen flex-col items-center justify-center gap-5 bg-background px-3 py-5 text-foreground"
    >
      {/* 品牌区（FR-126）：横版 lockup（path 化 wordmark，零字体依赖） */}
      <div className="text-center">
        <h1 className="inline-block leading-none">
          <BrandLockup height={48} testid="brand-login-lockup" />
        </h1>
        <p className="mt-2 text-dense text-muted-foreground">{t('制品仓库控制台')}</p>
      </div>
      {/* 表单本体保持原生 <form>（Enter 隐式提交链路零变化） */}
      <form
        className="card w-[min(380px,100%)] rounded-md border border-border bg-surface-1 p-5 shadow-flat"
        onSubmit={(e) => void onSubmit(e)}
      >
        <div className="flex flex-col gap-3">
          <div className="field flex flex-col gap-1.5">
            <Label htmlFor="login-username-input">{t('用户名')}</Label>
            <Input
              id="login-username-input"
              name="username"
              autoComplete="username"
              autoFocus
              className="font-mono"
              spellCheck={false}
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              data-testid="login-username"
            />
          </div>
          <div className="field flex flex-col gap-1.5">
            <Label htmlFor="login-password-input">{t('密码')}</Label>
            <Input
              id="login-password-input"
              name="password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              data-testid="login-password"
            />
          </div>
          {error && (
            <div data-testid="login-error" role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-dense text-destructive">
              {error}
            </div>
          )}
          <Button type="submit" className="w-full" data-testid="login-submit" disabled={!canSubmit}>
            {submitting ? t('登录中…') : t('登录')}
          </Button>
          {sso === 'on' && (
            <>
              <div className="login-divider flex items-center gap-3 text-aux text-muted-foreground" aria-hidden="true">
                <span className="h-px flex-1 bg-border" />
                {t('或')}
                <span className="h-px flex-1 bg-border" />
              </div>
              <Button type="button" variant="outline" className="w-full" data-testid="login-sso" disabled={ssoBusy} onClick={() => void onSSO()}>
                {ssoBusy ? t('正在跳转…') : t('使用 SSO 登录')}
              </Button>
            </>
          )}
          {ssoError && (
            <div data-testid="sso-error" role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-dense text-destructive">
              {ssoError}
            </div>
          )}
        </div>
      </form>
      {/* 常驻说明 + 文档链接（console-m8 §6.1 [6]；链接带下划线——弱化色
          说明文字中的链接需非色彩信号区分 axe link-in-text-block） */}
      <p className="text-center text-dense text-muted-foreground">
        {t('管理面需认证。CI 与脚本请使用 API Token。')}
        {ldap && <span className="ml-1.5" title={t('实例启用 LDAP——目录用户与本地用户共用同一表单')}>LDAP</span>}
        <a
          href="/binflow/docs/api-reference"
          target="_blank"
          rel="noopener noreferrer"
          data-testid="login-docs"
          className="ml-2 underline underline-offset-2"
        >
          {t('查看文档')}
        </a>
      </p>
    </div>
  )
}
