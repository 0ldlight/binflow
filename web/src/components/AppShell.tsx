import { useEffect, useRef, useState } from 'react'
import { Link, Navigate, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'

import { useAuth } from '../app/AuthContext'
import { useTheme } from '../app/ThemeContext'
import { useToast } from '../app/ToastContext'
import { useConfirm } from './ConfirmDialog'
import { useVersion } from '../lib/useVersion'
import { errText } from '../lib/api'

// 框架壳（console-ux §3.1/§3.5）：左侧固定导航（224px）+ 顶栏（48px）
// + 内容区。已启用入口：仪表盘、设置（T-98）、仓库（T-99）、搜索
// （T-100）、安全组（T-101）、治理组（T-102）；仅 Access Tokens（ux R6
// P2）保持占位禁用态——title 说明票号。
// admin/非 admin 收敛：whoami 的 admin 位为主信号（CE-04），
// 「API 403 即隐藏」为兜底（仪表盘卡片层）。

interface NavGroup {
  title?: string
  adminOnly?: boolean
  entries: { label: string; to?: string; ticket?: string }[]
}

const NAV: NavGroup[] = [
  {
    entries: [
      { label: '仪表盘', to: '/' },
      { label: '仓库', to: '/repositories' },
      { label: '搜索', to: '/search' },
    ],
  },
  {
    title: '安全',
    adminOnly: true,
    entries: [
      { label: '用户', to: '/security/users' },
      { label: '组', to: '/security/groups' },
      { label: '权限', to: '/security/permissions' },
      { label: 'Access Tokens', to: '/security/tokens', ticket: 'P2' },
    ],
  },
  {
    title: '治理',
    adminOnly: true,
    entries: [
      // 审计日志条目为 console-ux v1.1 补列（§3.1：治理组含审计——
      // GET /api/v1/audit 是 admin 门，归治理组）
      { label: '审计日志', to: '/audit' },
      { label: '存储 & GC', to: '/governance/gc' },
      { label: '备份 / 恢复', to: '/governance/backup' },
      { label: '配额', to: '/governance/quotas' },
    ],
  },
  {
    entries: [{ label: '设置', to: '/settings' }],
  },
]

/** 顶栏页面标题（§3.5 顶栏左侧） */
function pageTitle(pathname: string): string {
  if (pathname === '/') return '仪表盘'
  if (pathname.startsWith('/settings')) return '设置'
  if (pathname.startsWith('/repositories')) return '仓库'
  if (pathname.startsWith('/search')) return '搜索'
  if (pathname.startsWith('/security')) return '安全'
  if (pathname.startsWith('/governance')) return '治理'
  if (pathname.startsWith('/audit')) return '审计'
  return 'BinFlow'
}

export default function AppShell() {
  const { status, session, logout } = useAuth()
  const { theme, toggle } = useTheme()
  const toast = useToast()
  const confirm = useConfirm()
  const navigate = useNavigate()
  const location = useLocation()
  const version = useVersion()

  const [menuOpen, setMenuOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)

  // 全局搜索快捷键（§3.5）：⌘K / Ctrl+K / 「/」（输入框内不劫持）。
  // modal（危险确认框等）打开时让位（review N2）：确认框开着时背景页
  // 被换走，会让确认结果落在无关页面上——T-99 删仓/T-102 GC apply 的
  // 前置。
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (document.querySelector('[role="dialog"], .modal-backdrop')) return
      const el = e.target as HTMLElement | null
      const inField = !!el?.closest('input, textarea, select, [contenteditable="true"]')
      if ((e.key === 'k' || e.key === 'K') && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        navigate('/search')
      } else if (e.key === '/' && !inField) {
        e.preventDefault()
        navigate('/search')
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [navigate])

  // 点击菜单外关闭
  useEffect(() => {
    if (!menuOpen) return
    const onDown = (e: MouseEvent) => {
      if (!menuRef.current?.contains(e.target as Node)) setMenuOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [menuOpen])

  const doLogout = async () => {
    setMenuOpen(false)
    const ok = await confirm({
      title: '登出',
      body: '将结束当前会话并返回登录页；会话在服务端吊销，浏览器回退无法恢复。',
      confirmLabel: '登出',
    })
    if (!ok) return
    try {
      await logout()
      toast.success('已登出')
    } catch (err) {
      toast.error(`登出失败：${errText(err)}`)
    }
  }

  if (status === 'checking') {
    return (
      <div className="boot-screen" data-testid="app-boot">
        正在验证会话…
      </div>
    )
  }

  if (status === 'anonymous') {
    const target = location.pathname + location.search
    const safe = target.startsWith('/') && !target.startsWith('//') ? target : '/'
    return <Navigate to={`/login?return=${encodeURIComponent(safe)}`} replace />
  }

  const admin = session?.admin ?? false

  return (
    <div className="app">
      <nav className="app-nav" aria-label="主导航" data-testid="app-nav">
        <div className="app-nav-brand">
          <span className="name">
            BinFlow <span aria-hidden="true">◆</span>
          </span>
          <span className="ver" data-testid="nav-version" lang="en">
            {version ? `v${version.version}` : '—'}
          </span>
        </div>
        <div className="app-nav-items">
          {NAV.map((group, gi) => {
            if (group.adminOnly && !admin) return null
            return (
              <div key={group.title ?? `g${gi}`}>
                {group.title && (
                  <div className={`nav-group-label${gi === 0 ? ' first' : ''}`}>{group.title}</div>
                )}
                {group.entries.map((entry) =>
                  entry.to && !entry.ticket ? (
                    <NavLink
                      key={entry.to}
                      to={entry.to}
                      end={entry.to === '/'}
                      className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}
                    >
                      {entry.label}
                    </NavLink>
                  ) : (
                    <span
                      key={entry.to}
                      className="nav-item disabled"
                      aria-disabled="true"
                      title={`「${entry.label}」页在后续批次交付（${entry.ticket}）`}
                    >
                      {entry.label}
                      <span className="badge-soon">即将</span>
                    </span>
                  ),
                )}
              </div>
            )
          })}
        </div>
        <div className="app-nav-footer">
          <div className="session-box" ref={menuRef}>
            <button
              type="button"
              className="session-toggle"
              aria-haspopup="menu"
              aria-expanded={menuOpen}
              data-testid="session-toggle"
              onClick={() => setMenuOpen((v) => !v)}
            >
              <span aria-hidden="true">▣</span>
              <span className="who" data-testid="session-user">
                {session?.username ?? ''}
              </span>
              {admin && <span className="badge neutral">admin</span>}
              <span aria-hidden="true">▾</span>
            </button>
            {menuOpen && (
              <div className="session-menu" role="menu">
                <Link role="menuitem" to="/settings" onClick={() => setMenuOpen(false)}>
                  修改口令
                </Link>
                <button type="button" role="menuitem" onClick={() => void toggle()}>
                  {theme === 'dark' ? '切换亮色主题' : '切换暗色主题'}
                </button>
                <button type="button" role="menuitem" className="danger" data-testid="logout-button" onClick={() => void doLogout()}>
                  登出
                </button>
              </div>
            )}
          </div>
        </div>
      </nav>
      <div className="app-main">
        <header className="app-topbar">
          <span className="page-title">{pageTitle(location.pathname)}</span>
          <span className="spacer" />
          <button
            type="button"
            className="search-entry"
            data-testid="topbar-search"
            onClick={() => navigate('/search')}
          >
            <span aria-hidden="true">⌕</span> 搜索
            <span className="spacer" />
            <kbd>⌘K</kbd>
          </button>
          <a
            className="topbar-help"
            href="/binflow/docs/"
            target="_blank"
            rel="noopener noreferrer"
            title="帮助文档（新标签页打开）"
            data-testid="topbar-help"
          >
            <span aria-hidden="true">?</span> 帮助
          </a>
          <button
            type="button"
            className="icon-btn"
            aria-label={theme === 'dark' ? '切换亮色主题' : '切换暗色主题'}
            title={theme === 'dark' ? '切换亮色主题' : '切换暗色主题'}
            data-testid="topbar-theme-toggle"
            onClick={() => void toggle()}
          >
            {theme === 'dark' ? '◐' : '◑'}
          </button>
        </header>
        <main className="app-content">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
