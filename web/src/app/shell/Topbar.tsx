// 顶栏（新壳——architecture §4：面包屑/标题 · 全局搜索（驻留查询+最近词）
// · 帮助 · 主题 · 用户菜单（Quick 动作仅全量 admin））。
//
// 语义平移自旧 AppShell 顶栏（audit §3 全局能力清单 #9/11/12/13/15/23）：
// - topbar-search：Enter → /search?q=（/search 上 replace，沿当前 scope）；
//   驻留回显（URL q 是事实源 + draft 草稿层）；最近词下拉聚焦恒渲染
//   （空历史占位「暂无最近搜索」）；两段 Esc；↑↓ 循环。
// - admin-filter：/admin 域的管理资源过滤框（客户端子串过滤侧栏条目，
//   Esc 清词）——管理态顶栏单框形态（制品搜索让位，7.161 同形）。
// - 用户菜单 Quick 动作：仅全量 admin（readonly_admin 不见写入口）；
//   Set Me Up 全局入口 + step-up 回跳续铸（useStepUp）。
// - 锚族全保：topbar-breadcrumb / topbar-search(-recent-* ) / admin-filter
//   / topbar-help / help-* / topbar-theme-toggle / session-* / quick-* /
//   menu-edit-profile / logout-button / about-*。
import { useEffect, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useAuth } from '@/app/AuthContext'
import { useTheme } from '@/app/providers'
import { useConfirm } from '@/app/providers'
import { BrandMark } from '@/components/BrandLogo'
import { errText, isReadOnlyAdmin } from '@/lib/api'
import { useVersion } from '@/lib/useVersion'
import { toast } from '@/lib/toast'
import { tr } from '@/i18n'

import type { Crumb } from './breadcrumbs'

const t = tr('console')

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
    // localStorage 不可用：最近搜索退化为会话内
  }
  return next
}

export function Topbar({
  crumbs,
  title,
  adminMode,
  onAdminFilter,
  onOpenSetMeUp,
  aboutOpen,
  onAboutOpenChange,
}: {
  /** 管理域面包屑（null = 应用域，顶栏显页面标题） */
  crumbs: Crumb[] | null
  title: string
  adminMode: boolean
  onAdminFilter: (term: string) => void
  /** 用户菜单 Set Me Up 入口（壳层承载对话框挂载） */
  onOpenSetMeUp: (focusRef: HTMLElement | null) => void
  /** About 弹窗开合（壳层持有——nav-about/help-about 双入口同源） */
  aboutOpen: boolean
  onAboutOpenChange: (open: boolean) => void
}) {
  const { session, logout } = useAuth()
  const { theme, toggle } = useTheme()
  const { confirm } = useConfirm()
  const navigate = useNavigate()
  const location = useLocation()
  const version = useVersion()

  const admin = session?.admin ?? false
  const readOnlyAdmin = isReadOnlyAdmin(session)
  const sessionToggleRef = useRef<HTMLButtonElement>(null)

  // ---- 全局搜索（驻留查询 + 最近词） ----
  const topbarSearchRef = useRef<HTMLInputElement>(null)
  const urlQ = location.pathname === '/search' ? (new URLSearchParams(location.search).get('q') ?? '').trim() : ''
  const [draft, setDraft] = useState<{ key: string; term: string }>({ key: '', term: '' })
  const searchTerm = draft.key === location.key ? draft.term : urlQ
  const [recent, setRecent] = useState<string[]>(() => loadRecentSearches())
  const [recentOpen, setRecentOpen] = useState(false)
  const [recentActive, setRecentActive] = useState(-1)
  const recentList = recent.filter((q) => q.toLowerCase().includes(searchTerm.trim().toLowerCase()))

  const submitTopbarSearch = (raw: string) => {
    const term = raw.trim()
    setRecentOpen(false)
    setRecentActive(-1)
    if (term === '') {
      setDraft({ key: location.key, term: '' })
      navigate('/search')
      return
    }
    setRecent(commitRecentSearch(term))
    setDraft({ key: location.key, term })
    const onSearch = location.pathname === '/search'
    const scope = onSearch ? new URLSearchParams(location.search).get('scope') : null
    const scopeSeg = scope === 'builds' ? '&scope=builds' : ''
    navigate(`/search?q=${encodeURIComponent(term)}${scopeSeg}`, { replace: onSearch })
  }

  const onTopbarSearchKeyDown = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown' && recentList.length > 0) {
      e.preventDefault()
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
      e.preventDefault()
      if (recentOpen) {
        setRecentOpen(false)
        setRecentActive(-1)
      } else {
        setDraft({ key: location.key, term: '' })
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

  // ---- 管理资源过滤框 ----
  const [adminFilter, setAdminFilter] = useState('')
  const adminFilterRef = useRef<HTMLInputElement>(null)
  useEffect(() => {
    onAdminFilter(adminFilter)
  }, [adminFilter, onAdminFilter])

  // ---- 全局快捷键（⌘K / /）——指向当前模式的框 ----
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (document.querySelector('[role="dialog"]')) return
      const el = e.target as HTMLElement | null
      const inField = !!el?.closest('input, textarea, select, [contenteditable="true"]')
      const target = adminMode ? adminFilterRef : topbarSearchRef
      if ((e.key === 'k' || e.key === 'K') && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        target.current?.focus()
        target.current?.select()
      } else if (e.key === '/' && !inField) {
        e.preventDefault()
        target.current?.focus()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [adminMode])

  const doLogout = async () => {
    const ok = await confirm({
      title: t('登出'),
      description: t('将结束当前会话并返回登录页；会话在服务端吊销，浏览器回退无法恢复。'),
      confirmLabel: t('登出'),
    })
    if (!ok) return
    try {
      await logout()
      toast.success(t('已登出'))
    } catch (err) {
      toast.error(t('登出失败：{v1}', { v1: errText(err) }))
    }
  }

  // ---- OIDC step-up 回跳续铸由壳层承载（AppShell 持 SetMeUp 挂载态） ----

  return (
    <header className="app-topbar sticky top-0 z-[var(--bf-z-nav-sticky,70)] flex h-12 items-center gap-4 border-b border-border bg-background px-5">
      {crumbs ? (
        <nav className="topbar-breadcrumb flex min-w-0 items-center gap-1 text-dense" data-testid="topbar-breadcrumb" aria-label={t('位置')}>
          {crumbs.map((c, i) =>
            c.to && i < crumbs.length - 1 ? (
              <Link key={`${c.label}-${i}`} to={c.to} className="truncate text-muted-foreground hover:text-foreground hover:underline">
                {c.label}
              </Link>
            ) : (
              <span key={`${c.label}-${i}`} className={i === crumbs.length - 1 ? 'truncate font-semibold' : 'truncate text-muted-foreground'}>
                {c.label}
              </span>
            ),
          ).reduce<React.ReactNode[]>((acc, el, i) => (i === 0 ? [el] : [...acc, <span key={`sep-${i}`} aria-hidden="true" className="text-muted-foreground">/</span>, el]), [])}
        </nav>
      ) : (
        <h1 className="truncate text-[17px] font-semibold">{title}</h1>
      )}
      <div className="flex-1" />

      {adminMode ? (
        <div className="search-entry relative flex min-w-[220px] max-w-[360px] items-center gap-2 rounded-md border border-input px-3 text-muted-foreground focus-within:border-ring hover:border-muted-foreground/50">
          <span aria-hidden="true">⌕</span>
          <input
            ref={adminFilterRef}
            type="search"
            placeholder="Search Admin Resources…"
            data-testid="admin-filter"
            aria-label={t('搜索管理资源（过滤管理侧栏条目）')}
            autoComplete="off"
            className="min-w-0 flex-1 bg-transparent text-dense outline-none"
            value={adminFilter}
            onChange={(e) => setAdminFilter(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape') {
                e.preventDefault()
                setAdminFilter('')
              }
            }}
          />
          <kbd aria-hidden="true" className="text-aux">⌘K</kbd>
        </div>
      ) : (
        <div className="search-entry relative flex min-w-[220px] max-w-[360px] items-center gap-2 rounded-md border border-input px-3 text-muted-foreground focus-within:border-ring hover:border-muted-foreground/50">
          <span aria-hidden="true">⌕</span>
          <input
            ref={topbarSearchRef}
            type="search"
            placeholder={t('搜索制品…')}
            data-testid="topbar-search"
            aria-label={t('搜索制品')}
            autoComplete="off"
            className="min-w-0 flex-1 bg-transparent text-dense outline-none"
            value={searchTerm}
            onChange={(e) => {
              setDraft({ key: location.key, term: e.target.value })
              setRecentOpen(true)
            }}
            onFocus={() => setRecentOpen(true)}
            onBlur={() => {
              setRecentOpen(false)
              setRecentActive(-1)
            }}
            onKeyDown={onTopbarSearchKeyDown}
          />
          <kbd aria-hidden="true" className="text-aux">⌘K</kbd>
          {recentOpen && (
            <div className="topbar-search-recent absolute right-0 top-[calc(100%+4px)] z-50 w-full min-w-[280px] rounded-md border border-border bg-popover p-2 text-popover-foreground shadow-flat" data-testid="topbar-search-recent">
              <div className="search-recent-head mb-1 flex items-center justify-between text-aux">
                <span>{t('最近搜索')}</span>
                {recent.length > 0 && (
                  <button
                    type="button"
                    className="copy-btn text-primary hover:underline"
                    data-testid="topbar-search-recent-clear"
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => {
                      try {
                        window.localStorage.removeItem(RECENT_SEARCH_KEY)
                      } catch {
                        // 仅清会话内副本
                      }
                      setRecent([])
                      setRecentOpen(false)
                      setRecentActive(-1)
                    }}
                  >
                    {t('清除历史')}
                  </button>
                )}
              </div>
              {recentList.length > 0 ? (
                <ul className="search-recent-list" aria-label={t('最近搜索')}>
                  {recentList.map((q, i) => (
                    <li key={q}>
                      <button
                        type="button"
                        className={`w-full rounded-sm px-2 py-1 text-left font-mono text-dense hover:bg-accent ${i === recentActive ? 'active bg-accent' : ''}`}
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
              ) : (
                <p className="search-recent-empty px-2 py-1 text-aux text-muted-foreground" data-testid="topbar-search-recent-empty">
                  {recent.length === 0 ? t('暂无最近搜索') : t('「{v1}」无匹配历史', { v1: searchTerm.trim() })}
                </p>
              )}
            </div>
          )}
        </div>
      )}

      {/* 帮助下拉（Documentation / Online Training 占位 / Release Notes / About） */}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="sm" className="topbar-help" data-testid="topbar-help" title={t('帮助')}>
            <span aria-hidden="true">?</span> {t('帮助')}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" data-testid="topbar-help-menu">
          <DropdownMenuItem asChild>
            <a href="/binflow/docs/" target="_blank" rel="noopener noreferrer" data-testid="help-docs">
              Documentation
            </a>
          </DropdownMenuItem>
          <DropdownMenuItem disabled data-testid="help-training">
            {t('Online Training（暂无对应服务）')}
          </DropdownMenuItem>
          <DropdownMenuItem asChild>
            <a href="/binflow/docs/install/upgrade" target="_blank" rel="noopener noreferrer" data-testid="help-release-notes">
              Release Notes
            </a>
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem data-testid="help-about" onSelect={() => onAboutOpenChange(true)}>
            About
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      {/* 主题切换 */}
      <Button
        variant="ghost"
        size="icon"
        aria-label={theme === 'dark' ? t('切换亮色主题') : t('切换暗色主题')}
        title={theme === 'dark' ? t('切换亮色主题') : t('切换暗色主题')}
        data-testid="topbar-theme-toggle"
        onClick={toggle}
      >
        <span aria-hidden="true">{theme === 'dark' ? '◐' : '◑'}</span>
      </Button>

      {/* 用户菜单（Quick 动作仅全量 admin） */}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            ref={sessionToggleRef}
            type="button"
            className="flex items-center gap-1.5 rounded-sm px-2 py-1 text-dense hover:bg-accent"
            data-testid="session-toggle"
          >
            <span aria-hidden="true">▣</span>
            <span data-testid="session-user" className="font-semibold">
              {session?.username ?? ''}
            </span>
            {admin && !readOnlyAdmin && (
              <span className="rounded-sm bg-secondary px-1.5 py-px text-[11px]">admin</span>
            )}
            {readOnlyAdmin && (
              <span
                className="rounded-sm bg-secondary px-1.5 py-px text-[11px]"
                data-testid="session-readonly-badge"
                title={t('readonly_admin：管理面只读（服务端 403 兜底）')}
              >
                {t('只读')}
              </span>
            )}
            <span aria-hidden="true">▾</span>
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="min-w-[220px]">
          {admin && !readOnlyAdmin && (
            <>
              <div className="menu-label px-2 py-1 text-aux text-muted-foreground" role="presentation">
                {t('快速建仓')}
              </div>
              <DropdownMenuItem data-testid="quick-set-me-up" onSelect={() => onOpenSetMeUp(sessionToggleRef.current)}>
                Set Me Up
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link to="/admin/repositories/new?rclass=local">{t('新建 Local 仓库')}</Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link to="/admin/repositories/new?rclass=remote" data-testid="quick-new-repo-remote">
                  {t('新建 Remote 仓库')}
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link to="/admin/repositories/new?rclass=virtual">{t('新建 Virtual 仓库')}</Link>
              </DropdownMenuItem>
              <div className="menu-label px-2 py-1 text-aux text-muted-foreground" role="presentation">
                {t('新建')}
              </div>
              <DropdownMenuItem asChild>
                <Link to="/admin/security/users" data-testid="quick-new-user">
                  {t('新建用户')}
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link to="/admin/security/groups">{t('新建组')}</Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link to="/admin/security/permissions/new" data-testid="quick-new-perm">
                  {t('新建权限')}
                </Link>
              </DropdownMenuItem>
              <DropdownMenuSeparator />
            </>
          )}
          <DropdownMenuItem asChild>
            <Link to="/profile" data-testid="menu-edit-profile">
              {t('编辑档案')}
            </Link>
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => toggle()}>
            {theme === 'dark' ? t('切换亮色主题') : t('切换暗色主题')}
          </DropdownMenuItem>
          <DropdownMenuItem
            className="text-destructive focus:text-destructive"
            data-testid="logout-button"
            onSelect={() => void doLogout()}
          >
            {t('登出')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      {/* About 版本弹窗（消费 /api/system/version——useVersion 同源） */}
      <Dialog open={aboutOpen} onOpenChange={onAboutOpenChange}>
        <DialogContent className="max-w-sm" data-testid="about-dialog">
          <DialogHeader>
            <DialogTitle>About</DialogTitle>
          </DialogHeader>
          <div className="flex items-center gap-2">
            <BrandMark size={32} testid="about-brand-mark" />
            <div>
              <p className="text-[15px] font-semibold">BinFlow</p>
              <p className="text-aux text-muted-foreground">{t('单二进制云原生制品仓库')}</p>
            </div>
          </div>
          <div className="flex flex-col gap-1 text-dense">
            <p>
              {t('版本：')}
              <span className="font-mono" lang="en" data-testid="about-version">
                {version ? `v${version.version}` : '—'}
              </span>
            </p>
            <p>
              {t('构建：')}
              <span className="font-mono" lang="en" data-testid="about-revision">
                {version ? version.revision : '—'}
              </span>
            </p>
            <p>
              {t('产品标识：')}
              <span className="font-mono" lang="en" data-testid="about-product">
                {version ? version.product : '—'}
              </span>
            </p>
          </div>
          <p className="text-aux text-muted-foreground">
            {t('版本与构建信息来自 GET /api/system/version（开放端点）；License 详情见管理面 「Administration → License & Add-ons」。')}
          </p>
          <div className="flex justify-end">
            <Button data-testid="about-close" onClick={() => onAboutOpenChange(false)}>
              {t('关闭')}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </header>
  )
}
