import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'

import { deleteSession, getWhoami, postSession, setUnauthorizedListener } from '../lib/api'
import type { Whoami } from '../lib/api'
import { useToast } from './ToastContext'
import { tr } from '../i18n'

const t = tr('console')

// 会话状态（FR-23 / ADR-0014）：
//
// - 挂载时 GET /api/v1/session 探活（whoami），401 = 未登录；
// - login/logout 走 POST/DELETE /api/v1/session（服务端吊销）；
// - 全局 401 监听：已认证态下任何请求突然 401 = 会话已死（TTL 到期
//   或吊销——T-110 塌缩句：会话必死于 created_at+TTL，与活跃度无关），
//   toast「登录已过期」+ 带 return 重登（console-ux §5.1）。
//   刻意不做任何「保活」交互：绝对上限下保活是误导。

export type AuthStatus = 'checking' | 'anonymous' | 'authenticated'

interface AuthApi {
  status: AuthStatus
  session: Whoami | null
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthApi>({
  status: 'checking',
  session: null,
  login: async () => {},
  logout: async () => {},
})

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>('checking')
  const [session, setSession] = useState<Whoami | null>(null)
  const toast = useToast()
  const navigate = useNavigate()
  const location = useLocation()

  // 监听闭包需要最新值，但监听只注册一次
  const statusRef = useRef(status)
  statusRef.current = status
  const pathnameRef = useRef(location.pathname + location.search)
  pathnameRef.current = location.pathname + location.search

  useEffect(() => {
    let alive = true
    getWhoami()
      .then((who) => {
        if (!alive) return
        setSession(who)
        setStatus('authenticated')
      })
      .catch(() => {
        if (!alive) return
        setSession(null)
        setStatus('anonymous')
      })
    return () => {
      alive = false
    }
  }, [])

  useEffect(() => {
    setUnauthorizedListener(() => {
      if (statusRef.current !== 'authenticated') return
      // 同步置哨兵（review B1）：React 渲染提交是异步调度，同一轮并发
      // 到达的多个 401（如仪表盘四卡同挂载）在重渲染前都会读到旧值，
      // 否则「登录已过期」常驻 toast 会重复弹 N 条。
      statusRef.current = 'anonymous'
      setSession(null)
      setStatus('anonymous')
      toast.error(t('登录已过期，请重新登录'))
      const target = pathnameRef.current
      const safe = target.startsWith('/') && !target.startsWith('//') ? target : '/'
      navigate(`/login?return=${encodeURIComponent(safe)}`, { replace: true })
    })
    return () => setUnauthorizedListener(null)
  }, [navigate, toast])

  const login = useCallback(async (username: string, password: string) => {
    const who = await postSession(username, password)
    setSession(who)
    setStatus('authenticated')
  }, [])

  const logout = useCallback(async () => {
    await deleteSession()
    setSession(null)
    setStatus('anonymous')
  }, [])

  return (
    <AuthContext.Provider value={{ status, session, login, logout }}>{children}</AuthContext.Provider>
  )
}

export function useAuth(): AuthApi {
  return useContext(AuthContext)
}
