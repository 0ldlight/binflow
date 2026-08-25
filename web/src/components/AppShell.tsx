import { useEffect, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent } from 'react'
import { Link, NavLink, Navigate, Outlet, useLocation, useNavigate } from 'react-router-dom'

import { useAuth } from '../app/AuthContext'
import { useTheme } from '../app/ThemeContext'
import { useToast } from '../app/ToastContext'
import { useConfirm } from './ConfirmDialog'
import SetMeUpDialog from './SetMeUpDialog'
import { abandonStepUp, useStepUp } from '../lib/stepUpGrant'
import type { PendingMint } from '../lib/stepUpGrant'
import { useVersion } from '../lib/useVersion'
import { errText, isReadOnlyAdmin } from '../lib/api'

// 双模式壳（console-m8 §1/§2，T-235——Artifactory 对齐 IA 重排）：
//
//   应用模式（/dashboard /artifacts /search /profile）
//     侧栏「应用」分组：仪表盘、制品（跨仓树，T-236）。
//   管理模式（/admin/** 五分组：仓库/用户与权限/治理/监控/常规）
//     URL 进 /admin/** 即渲染管理侧栏（admin / readonly_admin）；
//     非 admin 直链保持应用侧栏（L1 预收敛），页面自身 403 收敛（L2）。
//
// 模式切换 = 侧栏底部常驻项（应用模式显「管理」，管理模式显「返回应用」；
// readonly_admin 可见——M7 语义保留清单 §7.3，含会话徽章「只读」）。
// 顶栏（§2.1）：面包屑/标题 · 搜索制品（T-265 起真输入框——Enter 提交跳
// /search?q=、Esc 清空、最近词下拉沿搜索页 recentSearches，FR-82-AC9）·
// 帮助 · 主题 · 用户菜单（Quick
// 动作 = §2.3；仅全量 admin 渲染写入口，readonly_admin 不见快速建仓）。
// 「不建」清单零影子入口（ADR-0029 决策 5）：无 Proxies/包索引/独立
// 产品入口；单实例无范围下拉（§1.2 单值不渲染口径）。
// testid 242 锚不随路由改名（console-ux §10/§10.5——W 资产保全）。

interface NavEntry {
  label: string
  to: string
  /** 仅精确匹配算 active（无子路由的叶子；默认前缀匹配覆盖子路径） */
  end?: boolean
}

interface NavGroup {
  title: string
  entries: NavEntry[]
}

/** 应用模式侧栏（console-m8 §1.3 全图：应用分组 2 条目） */
const APP_NAV: NavGroup[] = [
  {
    title: '应用',
    entries: [
      { label: '仪表盘', to: '/dashboard', end: true },
      { label: '制品', to: '/artifacts' },
    ],
  },
]

/** 管理模式侧栏（console-m8 §1.3 全图：五分组 12 条目 + M10 T-288 的
 * 「常规」分组 License & Add-ons = 13 条目；分组标题是标签不是折叠项
 * ——沿 console-ux §3.1 纪律） */
const ADMIN_NAV: NavGroup[] = [
  {
    title: '仓库',
    entries: [{ label: '仓库', to: '/admin/repositories' }],
  },
  {
    title: '用户与权限',
    entries: [
      { label: '用户', to: '/admin/security/users' },
      { label: '组', to: '/admin/security/groups' },
      { label: '权限', to: '/admin/security/permissions' },
      { label: 'Access Tokens', to: '/admin/security/tokens' },
    ],
  },
  {
    title: '治理',
    entries: [
      { label: '审计日志', to: '/admin/governance/audit' },
      { label: '维护（GC）', to: '/admin/governance/gc' },
      { label: '配额', to: '/admin/governance/quotas' },
      { label: '复制', to: '/admin/governance/replication' },
      { label: '备份 / 恢复', to: '/admin/governance/backup' },
    ],
  },
  {
    title: '监控',
    entries: [{ label: '存储', to: '/admin/monitoring/storage' }],
  },
  {
    title: '常规',
    entries: [
      { label: '系统信息', to: '/admin/general/settings' },
      // M10 T-288：License & Add-ons（FR-86-AC5——readonly_admin 只读可见，
      // 写入口页内按角色收敛；普通 user 不入管理面）
      { label: 'License & Add-ons', to: '/admin/general/license' },
    ],
  },
]

/** 应用模式落地与管理模式首页（模式切换的目标路由） */
export const APP_HOME = '/artifacts'
export const ADMIN_HOME = '/admin/repositories/local'

interface Crumb {
  label: string
  to?: string
}

function safeDecode(seg: string): string {
  try {
    return decodeURIComponent(seg)
  } catch {
    return seg
  }
}

/** 管理模式面包屑（§1.3：管理页层级表达，如「仓库 / maven-remote」） */
function adminCrumbs(pathname: string): Crumb[] {
  if (pathname.startsWith('/admin/repositories')) {
    const rest = pathname
      .slice('/admin/repositories'.length)
      .split('/')
      .filter((s) => s !== '')
    const crumbs: Crumb[] = [{ label: '仓库', to: '/admin/repositories/local' }]
    if (rest.length === 0 || ['local', 'remote', 'virtual'].includes(rest[0])) return crumbs
    if (rest[0] === 'new') return [...crumbs, { label: '新建仓库' }]
    crumbs.push({ label: safeDecode(rest[0]) })
    if (rest[1] === 'edit') crumbs.push({ label: '编辑' })
    return crumbs
  }
  const sec: Record<string, string> = {
    users: '用户',
    groups: '组',
    permissions: '权限',
    tokens: 'Access Tokens',
  }
  if (pathname.startsWith('/admin/security/')) {
    const rest = pathname.slice('/admin/security/'.length).split('/')
    const label = sec[rest[0]] ?? ''
    const crumbs: Crumb[] = [
      { label: '用户与权限', to: '/admin/security/users' },
      { label, to: `/admin/security/${rest[0]}` },
    ]
    if (rest[1] && rest[1] !== 'new') crumbs.push({ label: safeDecode(rest[1]) })
    if (rest[1] === 'new') crumbs.push({ label: '新建' })
    return crumbs
  }
  const gov: Record<string, string> = {
    audit: '审计日志',
    gc: '维护（GC）',
    quotas: '配额',
    replication: '复制',
    backup: '备份 / 恢复',
  }
  if (pathname.startsWith('/admin/governance/')) {
    const seg = pathname.slice('/admin/governance/'.length)
    return [{ label: '治理', to: '/admin/governance/audit' }, { label: gov[seg] ?? seg }]
  }
  if (pathname.startsWith('/admin/monitoring/')) {
    return [{ label: '监控', to: '/admin/monitoring/storage' }, { label: '存储' }]
  }
  if (pathname.startsWith('/admin/general/')) {
    // M10 T-288：常规分组两页（系统信息 / License & Add-ons）
    const seg = pathname.slice('/admin/general/'.length).split('/')[0]
    const label = seg === 'license' ? 'License & Add-ons' : '系统信息'
    return [{ label: '常规', to: '/admin/general/settings' }, { label }]
  }
  if (pathname === '/admin') return [{ label: '管理' }]
  return [{ label: '管理' }]
}

/** 应用模式顶栏标题（无层级，直接页面名） */
function appTitle(pathname: string): string {
  if (pathname.startsWith('/artifacts')) return '制品'
  if (pathname.startsWith('/dashboard')) return '仪表盘'
  if (pathname.startsWith('/search')) return '搜索制品'
  if (pathname.startsWith('/profile')) return '编辑档案'
  return 'BinFlow'
}

// ---- 顶栏 recentSearches（FR-82-AC9 / T-265） ------------------------------
// 与 pages/search/SearchPage 共用同一 localStorage 键与语义（reverse §4.5：
// 最近 8 条、去重置顶、隐私模式降级会话内）——顶栏提交即入列，搜索页聚焦
// 即见。pages/search 不在本票 area，两处实现暂各自持有（键与上限必须保持
// 一致）；收敛为共享 lib 归后续共享层票（T-266 邻域）。

const RECENT_SEARCH_KEY = 'binflow-console-recent-searches'
const RECENT_SEARCH_MAX = 8

function loadRecentSearches(): string[] {
  try {
    const raw: unknown = JSON.parse(window.localStorage.getItem(RECENT_SEARCH_KEY) ?? '[]')
    if (!Array.isArray(raw)) return []
    return raw.filter((v): v is string => typeof v === 'string' && v.trim() !== '').slice(0, RECENT_SEARCH_MAX)
  } catch {
    return []
  }
}

function commitRecentSearch(term: string): string[] {
  const next = [term, ...loadRecentSearches().filter((v) => v !== term)].slice(0, RECENT_SEARCH_MAX)
  try {
    window.localStorage.setItem(RECENT_SEARCH_KEY, JSON.stringify(next))
  } catch {
    // localStorage 不可用：最近搜索退化为会话内（与搜索页同款降级）
  }
  return next
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
  const sessionToggleRef = useRef<HTMLButtonElement>(null)

  // 顶栏搜索（console-m8 §2.1 / FR-82-AC9，T-265 升真输入框）：
  //   Enter → /search?q=<词>（顶栏提交即写入 recentSearches——与搜索页
  //   同键联动）；空词 Enter = 纯入口跳 /search（沿 T-235 前身「点进搜索
  //   页」的通道，/search 自身的 autoFocus 回显行为维持不变）。
  //   Esc 两段：先收最近词下拉，再清空 + 失焦。
  //   最近词下拉沿 SearchPage 既有 recentSearches 语义：按当前词子串过滤、
  //   ↑↓ 循环 + Enter 应用、一键清除历史。
  const topbarSearchRef = useRef<HTMLInputElement>(null)
  const [searchTerm, setSearchTerm] = useState('')
  const [recent, setRecent] = useState<string[]>(() => loadRecentSearches())
  const [recentOpen, setRecentOpen] = useState(false)
  const [recentActive, setRecentActive] = useState(-1)
  // 按当前词子串过滤（空词 = 全量历史；无匹配即不渲染下拉）
  const recentList = recent.filter((q) => q.toLowerCase().includes(searchTerm.trim().toLowerCase()))

  const submitTopbarSearch = (raw: string) => {
    const term = raw.trim()
    setRecentOpen(false)
    setRecentActive(-1)
    if (term === '') {
      navigate('/search')
      return
    }
    setRecent(commitRecentSearch(term))
    setSearchTerm(term)
    navigate(`/search?q=${encodeURIComponent(term)}`)
  }

  const onTopbarSearchKeyDown = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown' && recentList.length > 0) {
      e.preventDefault() // 光标移动让位给历史导航
      setRecentOpen(true)
      setRecentActive((i) => Math.min(i + 1, recentList.length - 1))
      return
    }
    if (e.key === 'ArrowUp' && recentOpen && recentList.length > 0) {
      e.preventDefault()
      setRecentActive((i) => Math.max(i - 1, 0))
      return
    }
    if (e.key === 'Escape') {
      // 两段 Esc：可见下拉在（recentList 非空）先收它；否则清空 + 失焦。
      // 判据用「实际呈现的下拉」而非 recentOpen——子串无匹配时下拉本就
      // 不渲染，此时 Esc 直接走清空（不可见的状态不该吞掉一次按键）。
      if (recentOpen && recentList.length > 0) {
        setRecentOpen(false)
        setRecentActive(-1)
      } else {
        setSearchTerm('')
        topbarSearchRef.current?.blur()
      }
      return
    }
    if (e.key === 'Enter') {
      e.preventDefault()
      if (recentOpen && recentActive >= 0 && recentList[recentActive]) submitTopbarSearch(recentList[recentActive])
      else submitTopbarSearch(searchTerm)
    }
  }

  const clearTopbarRecent = () => {
    try {
      window.localStorage.removeItem(RECENT_SEARCH_KEY)
    } catch {
      // localStorage 不可用：仅清会话内副本
    }
    setRecent([])
    setRecentOpen(false)
    setRecentActive(-1)
  }

  // Set Me Up 全局入口（T-244 收口 quick-set-me-up 占位接线——T-242 遗留）：
  // 用户菜单 → 快速建仓 → Set Me Up 直接开全局对话框（无仓库上下文 →
  // 步骤 0 包类型网格）；关闭回焦菜单钮（焦点回到开启者，§8）。
  const [smuOpen, setSmuOpen] = useState(false)

  // OIDC step-up 回跳续铸（T-260 / architecture §14.3-2）：main.tsx 挂载期
  // 已消费 #step_up_grant= fragment（提取即抹除）——grant 到手即在此重开
  // Set Me Up 于「续铸」态（恢复 pending 上下文、自动携 grant 重发 mint）。
  // resumeOpen 独立于 grant 生命周期：mint 结算（成功/invalid）后 grant 清
  // 空但视图保留（令牌面板/内联错误仍须可见）；关闭 = 放弃（清 grant +
  // pending——绝不以旧 grant 重试）。会话死于 IdP 往返的边角不重开（登录
  // 守卫先行）。
  const stepUp = useStepUp()
  const [resumeOpen, setResumeOpen] = useState(false)
  const resumeCtxRef = useRef<PendingMint | null>(null)
  useEffect(() => {
    if (!stepUp.grant || !stepUp.pending) return
    resumeCtxRef.current = stepUp.pending
    setResumeOpen(true)
  }, [stepUp.grant, stepUp.pending])

  const admin = session?.admin ?? false
  const readOnlyAdmin = isReadOnlyAdmin(session)
  // readonly_admin ⊆ admin 可见面（§7.6）：管理侧栏与「管理」入口同门
  const canSeeAdmin = admin || readOnlyAdmin
  const inAdminArea = location.pathname.startsWith('/admin')
  const mode: 'app' | 'admin' = inAdminArea && canSeeAdmin ? 'admin' : 'app'

  // 全局搜索快捷键（console-m8 §3.4）：⌘K / Ctrl+K / 「/」（输入框内
  // 不劫持）；modal（危险确认框等）打开时让位（review N2）。T-265 起顶栏
  // 是真输入框——快捷键聚焦它（⌘K 附带全选，便于直接覆写），Enter 即提交。
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (document.querySelector('[role="dialog"], .modal-backdrop')) return
      const el = e.target as HTMLElement | null
      const inField = !!el?.closest('input, textarea, select, [contenteditable="true"]')
      if ((e.key === 'k' || e.key === 'K') && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        topbarSearchRef.current?.focus()
        topbarSearchRef.current?.select()
      } else if (e.key === '/' && !inField) {
        e.preventDefault()
        topbarSearchRef.current?.focus()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  // 用户菜单：点击外部关闭 + Esc 关闭（§3.4 键盘清单）
  useEffect(() => {
    if (!menuOpen) return
    const onDown = (e: MouseEvent) => {
      if (!menuRef.current?.contains(e.target as Node)) setMenuOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMenuOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
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
      <div className="boot-screen">
        正在验证会话…
      </div>
    )
  }

  if (status === 'anonymous') {
    const target = location.pathname + location.search
    const safe = target.startsWith('/') && !target.startsWith('//') ? target : '/'
    return <Navigate to={`/login?return=${encodeURIComponent(safe)}`} replace />
  }

  const groups = mode === 'admin' ? ADMIN_NAV : APP_NAV
  const crumbs = mode === 'admin' || inAdminArea ? adminCrumbs(location.pathname) : null

  return (
    <div className="app">
      <nav className="app-nav" aria-label="主导航" data-testid="app-nav">
        <div className="app-nav-brand">
          <span className="name">
            BinFlow <span aria-hidden="true">◆</span>
          </span>
        </div>
        <div className="app-nav-items">
          {groups.map((group, gi) => (
            <div key={group.title}>
              <div className={`nav-group-label${gi === 0 ? ' first' : ''}`}>{group.title}</div>
              {group.entries.map((entry) => (
                <NavLink
                  key={entry.to}
                  to={entry.to}
                  end={entry.end}
                  className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}
                >
                  {entry.label}
                </NavLink>
              ))}
            </div>
          ))}
        </div>
        <div className="app-nav-footer">
          {/* 模式切换（§1.1）：侧栏底部常驻项；admin / readonly_admin 可见 */}
          {canSeeAdmin && (
            <button
              type="button"
              className="nav-item nav-mode-switch"
              data-testid="nav-mode-switch"
              aria-current={mode === 'admin' ? 'true' : undefined}
              title={mode === 'admin' ? '返回应用模式' : '进入管理模式（/admin）'}
              onClick={() => navigate(mode === 'admin' ? APP_HOME : ADMIN_HOME)}
            >
              <span aria-hidden="true">{mode === 'admin' ? '↩' : '⚙'}</span>
              {mode === 'admin' ? '返回应用' : '管理'}
            </button>
          )}
          {/* 许可行（§1.1）：版本来自 /api/system/version（nav-version 锚不变） */}
          <div className="app-nav-license">
            BinFlow <span data-testid="nav-version" lang="en">{version ? `v${version.version}` : '—'}</span> · 单二进制制品仓库
          </div>
        </div>
      </nav>
      <div className="app-main">
        <header className="app-topbar">
          {/* 面包屑/页面标题（§2.1）：管理模式带分组层级（§1.3） */}
          {crumbs ? (
            <nav className="topbar-breadcrumb" data-testid="topbar-breadcrumb" aria-label="位置">
              {crumbs.map((c, i) => (
                <span key={`${c.label}-${i}`} className="crumb">
                  {i > 0 && (
                    <span className="crumb-sep" aria-hidden="true">
                      /
                    </span>
                  )}
                  {c.to && i < crumbs.length - 1 && canSeeAdmin ? (
                    <Link to={c.to}>{c.label}</Link>
                  ) : (
                    <span className={i === crumbs.length - 1 ? 'crumb-current' : undefined}>{c.label}</span>
                  )}
                </span>
              ))}
            </nav>
          ) : (
            <span className="page-title">{appTitle(location.pathname)}</span>
          )}
          <span className="spacer" />
          {/* 顶栏搜索（§2.1 / FR-82-AC9）：真输入框 + 最近词下拉（样式与
              搜索页 .search-recent 同形，但自持于 base.css——页面 css 随
              懒加载分片，顶栏不可依赖）。输入框不加 aria-expanded /
              aria-autocomplete：role=searchbox 不容这两个属性（axe
              aria-allowed-attr），完整 combobox 模式（aria-controls +
              listbox/option）随搜索页一并归后续票统一 */}
          <div className="search-entry" role="search">
            <span aria-hidden="true">⌕</span>
            <input
              ref={topbarSearchRef}
              type="search"
              placeholder="搜索制品…"
              aria-label="搜索制品"
              data-testid="topbar-search"
              autoComplete="off"
              value={searchTerm}
              onChange={(e) => {
                setSearchTerm(e.target.value)
                setRecentOpen(true)
              }}
              onFocus={() => setRecentOpen(true)}
              onBlur={() => {
                setRecentOpen(false)
                setRecentActive(-1)
              }}
              onKeyDown={onTopbarSearchKeyDown}
            />
            <kbd aria-hidden="true">⌘K</kbd>
            {recentOpen && recentList.length > 0 && (
              <div className="topbar-search-recent" data-testid="topbar-search-recent">
                <div className="search-recent-head">
                  <span>最近搜索</span>
                  <button
                    type="button"
                    className="copy-btn"
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={clearTopbarRecent}
                  >
                    清除历史
                  </button>
                </div>
                <ul className="search-recent-list" aria-label="最近搜索">
                  {recentList.map((q, i) => (
                    <li key={q}>
                      <button
                        type="button"
                        className={i === recentActive ? 'active' : ''}
                        data-testid={`topbar-search-recent-item-${i}`}
                        lang="en"
                        onMouseDown={(e) => e.preventDefault()}
                        onClick={() => submitTopbarSearch(q)}
                      >
                        {q}
                      </button>
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
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
          {/* 用户菜单（§2.3）：Quick 动作仅全量 admin（readonly_admin 不见
              快速建仓等写入口——L4 预收敛，服务端 403 兜底） */}
          <div className="session-box topbar-session" ref={menuRef}>
            <button
              type="button"
              ref={sessionToggleRef}
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
              {admin && !readOnlyAdmin && <span className="badge neutral">admin</span>}
              {readOnlyAdmin && (
                <span
                  className="badge neutral"
                  data-testid="session-readonly-badge"
                  title="readonly_admin：管理面只读（服务端 403 兜底）"
                >
                  只读
                </span>
              )}
              <span aria-hidden="true">▾</span>
            </button>
            {menuOpen && (
              <div
                className="session-menu"
                role="menu"
                onKeyDown={(e) => {
                  // §3.4 键盘清单：菜单 ↑/↓ 循环移动菜单项（role=menu 语义）
                  if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
                  e.preventDefault()
                  const items = Array.from(
                    menuRef.current?.querySelectorAll<HTMLElement>('[role="menuitem"]:not([disabled])') ?? [],
                  )
                  if (items.length === 0) return
                  const i = items.indexOf(document.activeElement as HTMLElement)
                  const next =
                    e.key === 'ArrowDown' ? items[(i + 1) % items.length] : items[(i - 1 + items.length) % items.length]
                  next?.focus()
                }}
              >
                {admin && !readOnlyAdmin && (
                  <>
                    <div className="menu-label" role="presentation">
                      快速建仓
                    </div>
                    <button
                      type="button"
                      role="menuitem"
                      data-testid="quick-set-me-up"
                      onClick={() => {
                        setMenuOpen(false)
                        setSmuOpen(true)
                      }}
                    >
                      Set Me Up
                    </button>
                    <Link
                      role="menuitem"
                      to="/admin/repositories/new?rclass=local"
                      onClick={() => setMenuOpen(false)}
                    >
                      新建 Local 仓库
                    </Link>
                    <Link
                      role="menuitem"
                      data-testid="quick-new-repo-remote"
                      to="/admin/repositories/new?rclass=remote"
                      onClick={() => setMenuOpen(false)}
                    >
                      新建 Remote 仓库
                    </Link>
                    <Link
                      role="menuitem"
                      to="/admin/repositories/new?rclass=virtual"
                      onClick={() => setMenuOpen(false)}
                    >
                      新建 Virtual 仓库
                    </Link>
                    <div className="menu-label" role="presentation">
                      新建
                    </div>
                    <Link role="menuitem" data-testid="quick-new-user" to="/admin/security/users" onClick={() => setMenuOpen(false)}>
                      新建用户
                    </Link>
                    <Link role="menuitem" to="/admin/security/groups" onClick={() => setMenuOpen(false)}>
                      新建组
                    </Link>
                    <Link
                      role="menuitem"
                      data-testid="quick-new-perm"
                      to="/admin/security/permissions/new"
                      onClick={() => setMenuOpen(false)}
                    >
                      新建权限
                    </Link>
                    <div className="menu-sep" role="presentation" />
                  </>
                )}
                <Link role="menuitem" data-testid="menu-edit-profile" to="/profile" onClick={() => setMenuOpen(false)}>
                  编辑档案
                </Link>
                <button type="button" role="menuitem" onClick={() => void toggle()}>
                  {theme === 'dark' ? '切换亮色主题' : '切换暗色主题'}
                </button>
                <button
                  type="button"
                  role="menuitem"
                  className="danger"
                  data-testid="logout-button"
                  onClick={() => void doLogout()}
                >
                  登出
                </button>
              </div>
            )}
          </div>
        </header>
        <main className="app-content">
          <Outlet />
        </main>
      </div>
      {/* 全局 Set Me Up 入口承载（quick-set-me-up 接线，T-244）+ OIDC
          step-up 回跳续铸承载（T-260）——modal 层 fixed 定位不随壳布局；
          关闭回焦菜单钮（续铸态关闭 = 放弃 grant + pending） */}
      {(smuOpen || (resumeOpen && status === 'authenticated')) && (
        <SetMeUpDialog
          preselectedRepo={resumeOpen ? resumeCtxRef.current?.repo : undefined}
          resume={resumeOpen ? resumeCtxRef.current : null}
          onClose={() => {
            if (resumeOpen) {
              abandonStepUp()
              setResumeOpen(false)
            }
            setSmuOpen(false)
            sessionToggleRef.current?.focus()
          }}
        />
      )}
    </div>
  )
}
