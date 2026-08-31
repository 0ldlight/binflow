import { useEffect, useRef, useState } from 'react'
import type { ComponentPropsWithoutRef, KeyboardEvent as ReactKeyboardEvent } from 'react'
import { Link, Navigate, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'

import AppBar from '@mui/material/AppBar'
import Box from '@mui/material/Box'
import Breadcrumbs from '@mui/material/Breadcrumbs'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import CircularProgress from '@mui/material/CircularProgress'
import Container from '@mui/material/Container'
import Divider from '@mui/material/Divider'
import Drawer from '@mui/material/Drawer'
import IconButton from '@mui/material/IconButton'
import InputBase from '@mui/material/InputBase'
import MuiLink from '@mui/material/Link'
import List from '@mui/material/List'
import ListItemButton from '@mui/material/ListItemButton'
import Menu from '@mui/material/Menu'
import MenuItem from '@mui/material/MenuItem'
import Paper from '@mui/material/Paper'
import Toolbar from '@mui/material/Toolbar'
import Typography from '@mui/material/Typography'
import type { Theme } from '@mui/material/styles'

import { useAuth } from '../app/AuthContext'
import { useTheme } from '../app/ThemeContext'
import { useToast } from '../app/ToastContext'
import { useConfirm } from './ConfirmDialog'
import SetMeUpDialog from './SetMeUpDialog'
import { abandonStepUp, useStepUp } from '../lib/stepUpGrant'
import type { PendingMint } from '../lib/stepUpGrant'
import { useVersion } from '../lib/useVersion'
import { errText, isReadOnlyAdmin } from '../lib/api'

// 双模式壳（console-m8 §1/§2，T-235——Artifactory 对齐 IA 重排；T-344 批 A
// MUI 原生壳重构，mui-native-visual §4.1）：
//
//   应用模式（/dashboard /artifacts /search /profile）
//     侧栏「应用」分组：仪表盘、制品（跨仓树，T-236）。
//   管理模式（/admin/** 五分组：仓库/用户与权限/治理/监控/常规）
//     URL 进 /admin/** 即渲染管理侧栏（admin / readonly_admin）；
//     非 admin 直链保持应用侧栏（L1 预收敛），页面自身 403 收敛（L2）。
//
// 骨架 = Drawer(permanent) + AppBar(sticky, elevation 0) + Toolbar(dense
// 48px) + Container(main, maxWidth 1440) + Breadcrumbs；侧栏 List +
// ListItemButton（NavLink 承载，DOM 仍 <a class="nav-item active">）。
// spec 类钩子保名去规（§3.8）：.app-nav / .nav-item / .nav-group-label /
// .nav-mode-switch / .search-entry 等类名留 DOM 作 inert 钩子，皮肤规则
// 全部退役（base.css 壳族删除），视觉由 Drawer PaperProps sx（--bf-sidebar
// 系 token——侧栏身份例外）与 MUI 主题承载。
//
// 模式切换 = 侧栏底部常驻项（应用模式显「管理」，管理模式显「返回应用」；
// readonly_admin 可见——M7 语义保留清单 §7.3，含会话徽章「只读」）。
// 顶栏（§2.1）：面包屑/标题 · 搜索制品（T-265 起真输入框——Enter 提交跳
// /search?q=、Esc 清空、最近词下拉沿搜索页 recentSearches，FR-82-AC9）·
// 帮助 · 主题 · 用户菜单（Quick 动作 = §2.3；仅全量 admin 渲染写入口，
// readonly_admin 不见快速建仓）。
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

/** 管理模式侧栏（console-m8 §1.3 全图：五分组；M10 T-288 License & Add-ons、
 * M11 T-307 认证配置、M12 T-352 回收站、M13 T-366 Webhooks 增补后 = 16 条目；
 * 分组标题是标签不是折叠项——沿 console-ux §3.1 纪律） */
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
      // M11 T-307：认证配置（FR-92——LDAP/OAuth/SAML 三协议；readonly_admin
      // 只读可见，普通 user 不入管理面）
      { label: '认证配置', to: '/admin/security/auth/ldap' },
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
      // M12 T-352：回收站（FR-106——浏览/恢复/清空；trashcan 槽门控态呈现）
      { label: '回收站', to: '/admin/governance/trash' },
      // M13 T-366：Webhook 订阅（FR-115.5——订阅 CRUD/test + 投递排障记录；
      // readonly_admin 只读可见，读写入口页内按角色收敛）
      { label: 'Webhooks', to: '/admin/governance/webhooks' },
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
    auth: '认证配置',
  }
  if (pathname.startsWith('/admin/security/')) {
    const rest = pathname.slice('/admin/security/'.length).split('/')
    const label = sec[rest[0]] ?? ''
    const crumbs: Crumb[] = [
      { label: '用户与权限', to: '/admin/security/users' },
      { label, to: `/admin/security/${rest[0]}` },
    ]
    // 认证配置三协议段显名（ldap/oauth/saml → Tab 名；T-307）
    const proto: Record<string, string> = { ldap: 'LDAP', oauth: 'OAuth (OIDC)', saml: 'SAML SSO' }
    if (rest[0] === 'auth' && proto[rest[1]]) crumbs.push({ label: proto[rest[1]] })
    else if (rest[1] && rest[1] !== 'new') crumbs.push({ label: safeDecode(rest[1]) })
    else if (rest[1] === 'new') crumbs.push({ label: '新建' })
    return crumbs
  }
  const gov: Record<string, string> = {
    audit: '审计日志',
    gc: '维护（GC）',
    quotas: '配额',
    replication: '复制',
    backup: '备份 / 恢复',
    trash: '回收站',
    webhooks: 'Webhooks',
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

  // 用户菜单（MUI Menu 原生）：Esc 关闭 / 点击外部（backdrop）关闭 / ↑↓
  // 循环由 Menu+MenuList 承载；焦点语义按 MUI 惯例——开菜单即聚焦首项、
  // 关闭回焦锚钮。T-344 批 A 摘 paper sx 复刻块：菜单壳交 MUI 默认
  // （elevation paper / 主题密度档）。

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
        <CircularProgress size={18} aria-label="会话验证中" sx={{ mr: 'var(--bf-sp-2)' }} />
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

  // 侧栏条目共通形态：ListItemButton 承载 NavLink（DOM = <a class="nav-item
  // active">——NavLink 字符串形态自动追加 active 类，isActive 函数形态的
  // 等价面）；active 视觉经 '&.active' 消费 --bf-sidebar 系 token（侧栏
  // 身份例外）+ 主题 primary 指示条（console-m8 §2.1）。函数形态 = sx 的
  // 主题回调（属性级 palette 消费的正确通道）。
  const navItemSx = (t: Theme) => ({
    color: 'var(--bf-sidebar-text)',
    '&:hover': { backgroundColor: 'var(--bf-sidebar-2)' },
    '&.active': {
      backgroundColor: 'var(--bf-sidebar-3)',
      boxShadow: `inset 2px 0 0 ${t.palette.primary.main}`,
    },
  })

  return (
    <Box sx={{ display: 'flex', minHeight: '100vh' }}>
      {/* 侧栏身份保留（深底 + --bf-sidebar 系 token），实现换 MUI Drawer
          （mui-native-visual §4.1）；nav 标签语义与 app-nav 锚不动 */}
      <Drawer
        variant="permanent"
        sx={{ width: 224, flexShrink: 0 }}
        slotProps={{
          paper: {
            sx: {
              width: 224,
              boxSizing: 'border-box',
              bgcolor: 'var(--bf-sidebar)',
              borderRight: '1px solid var(--bf-sidebar-border)',
              color: 'var(--bf-sidebar-text)',
              overflowY: 'auto',
            },
          },
        }}
      >
        <nav className="app-nav" aria-label="主导航" data-testid="app-nav" style={{ display: 'flex', flexDirection: 'column', minHeight: '100%' }}>
          <Box className="app-nav-brand" sx={{ display: 'flex', alignItems: 'baseline', gap: 'var(--bf-sp-2)', padding: 'var(--bf-sp-4)', borderBottom: '1px solid var(--bf-sidebar-border)' }}>
            <Typography className="name" variant="subtitle1" sx={{ fontWeight: 600, color: 'var(--bf-sidebar-text)' }}>
              BinFlow <span aria-hidden="true">◆</span>
            </Typography>
          </Box>
          <List className="app-nav-items" component="div" disablePadding sx={{ flex: 1, padding: 'var(--bf-sp-2) var(--bf-sp-1)' }}>
            {groups.map((group, gi) => (
              <div key={group.title}>
                {/* 分组标签：spec 类钩子保名（shell ×6 计数断言）；overline 观感 */}
                <Typography className={`nav-group-label${gi === 0 ? ' first' : ''}`} variant="overline" component="div" sx={{ padding: 'var(--bf-sp-3) var(--bf-sp-3) var(--bf-sp-1)', color: 'var(--bf-sidebar-text-2)', lineHeight: 1.6, '&.first': { paddingTop: 'var(--bf-sp-1)' } }}>
                  {group.title}
                </Typography>
                {group.entries.map((entry) => (
                  <ListItemButton
                    key={entry.to}
                    component={NavLink}
                    to={entry.to}
                    end={entry.end}
                    className="nav-item"
                    sx={navItemSx}
                  >
                    {entry.label}
                  </ListItemButton>
                ))}
              </div>
            ))}
          </List>
          <Box className="app-nav-footer" sx={{ display: 'flex', flexDirection: 'column', borderTop: '1px solid var(--bf-sidebar-border)', padding: 'var(--bf-sp-2)' }}>
            {/* 模式切换（§1.1）：侧栏底部常驻项；admin / readonly_admin 可见。
                button 元素语义保留（aria-current + Enter 激活链路零变化） */}
            {canSeeAdmin && (
              <ListItemButton
                component="button"
                type="button"
                className="nav-item nav-mode-switch"
                data-testid="nav-mode-switch"
                aria-current={mode === 'admin' ? 'true' : undefined}
                title={mode === 'admin' ? '返回应用模式' : '进入管理模式（/admin）'}
                onClick={() => navigate(mode === 'admin' ? APP_HOME : ADMIN_HOME)}
                sx={(t) => ({ ...navItemSx(t), marginBottom: 'var(--bf-sp-2)' })}
              >
                <span aria-hidden="true">{mode === 'admin' ? '↩' : '⚙'}</span>
                {mode === 'admin' ? '返回应用' : '管理'}
              </ListItemButton>
            )}
            {/* 许可行（§1.1）：版本来自 /api/system/version（nav-version 锚不变） */}
            <Typography className="app-nav-license" variant="caption" sx={{ padding: 'var(--bf-sp-1) var(--bf-sp-2)', color: 'var(--bf-sidebar-text-2)' }}>
              BinFlow <span data-testid="nav-version" lang="en">{version ? `v${version.version}` : '—'}</span> · 单二进制制品仓库
            </Typography>
          </Box>
        </nav>
      </Drawer>
      <Box sx={{ flexGrow: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
        {/* 顶栏背景必须不透明（background.default = 旧 .app-topbar 的 --bf-bg
            同值）：axe color-contrast 在祖先链全透明时会做视觉重叠合成——
            建仓向导「进页即弹」的 modal scrim（--bf-scrim 45%）叠进合成底，
            顶栏文字双主题对比度假性炸裂（T-344B 回归修复）。zIndex 恢复
            §7.2 层次（70 < dropdown 80 < modal 90）：MUI AppBar 默认 1100
            会浮在 scrim 上，破坏模态全屏遮罩语义 */}
        <AppBar
          position="sticky"
          elevation={0}
          color="transparent"
          className="app-topbar"
          sx={{ backgroundColor: 'background.default', borderBottom: 1, borderColor: 'divider', zIndex: 'var(--bf-z-nav-sticky)' }}
        >
          <Toolbar variant="dense" disableGutters sx={{ gap: 'var(--bf-sp-4)', minHeight: 48, padding: '0 var(--bf-sp-5)' }}>
            {/* 面包屑/页面标题（§2.1）：管理模式带分组层级（§1.3）。
                Breadcrumbs 根即 nav 容器（topbar-breadcrumb 锚不动） */}
            {crumbs ? (
              <Breadcrumbs
                data-testid="topbar-breadcrumb"
                className="topbar-breadcrumb"
                aria-label="位置"
                separator={<span aria-hidden="true">/</span>}
                sx={{ minWidth: 0, '& .MuiBreadcrumbs-li': { whiteSpace: 'nowrap' } }}
              >
                {crumbs.map((c, i) =>
                  c.to && i < crumbs.length - 1 && canSeeAdmin ? (
                    <MuiLink key={`${c.label}-${i}`} component={Link} to={c.to} underline="hover" color="inherit">
                      {c.label}
                    </MuiLink>
                  ) : (
                    <Typography
                      key={`${c.label}-${i}`}
                      component="span"
                      sx={{ fontWeight: i === crumbs.length - 1 ? 600 : 400, color: i === crumbs.length - 1 ? 'text.primary' : 'text.secondary' }}
                    >
                      {c.label}
                    </Typography>
                  ),
                )}
              </Breadcrumbs>
            ) : (
              <Typography component="span" variant="h6" noWrap sx={{ fontSize: 'var(--bf-fs-h3)' }}>
                {appTitle(location.pathname)}
              </Typography>
            )}
            <Box sx={{ flexGrow: 1 }} />
            {/* 顶栏搜索（§2.1 / FR-82-AC9）：真输入框 + 最近词下拉（MUI 文档
                站搜索形态：Paper elevation 0 + InputBase；search-entry 类名保
                （kbd/媒体查询布局规则）；键盘链路（Esc 两段/↑↓/Enter、⌘K
                focus+select）零变化。输入框不加 aria-expanded /
                aria-autocomplete：role=searchbox 不容这两个属性（axe
                aria-allowed-attr），完整 combobox 模式随搜索页归后续票统一 */}
            <Paper
              component="div"
              role="search"
              elevation={0}
              className="search-entry"
              sx={{
                position: 'relative',
                display: 'flex',
                alignItems: 'center',
                gap: 'var(--bf-sp-2)',
                minWidth: 220,
                maxWidth: 360,
                padding: '0 var(--bf-sp-3)',
                border: '1px solid',
                borderColor: 'divider',
                color: 'text.secondary',
                '&:hover': { borderColor: 'text.disabled' },
                '&:focus-within': { borderColor: 'primary.main' },
              }}
            >
              <span aria-hidden="true">⌕</span>
              <InputBase
                inputRef={topbarSearchRef}
                type="search"
                placeholder="搜索制品…"
                slotProps={{
                  // data-testid 落 input 本体（topbar-search 锚）；MUI v7 的
                  // slot 类型不容 data-* 属性，按 input 元素 props 断言放行
                  input: {
                    'data-testid': 'topbar-search',
                    'aria-label': '搜索制品',
                    autoComplete: 'off',
                  } as ComponentPropsWithoutRef<'input'>,
                }}
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
                sx={{ flex: 1, minWidth: 0 }}
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
            </Paper>
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
            {/* 主题切换：title 原生 tooltip、aria-label、字形与
                ThemeContext 翻转链路零变化；皮肤交 MUI 默认 */}
            <IconButton
              aria-label={theme === 'dark' ? '切换亮色主题' : '切换暗色主题'}
              title={theme === 'dark' ? '切换亮色主题' : '切换暗色主题'}
              data-testid="topbar-theme-toggle"
              onClick={() => void toggle()}
            >
              {theme === 'dark' ? '◐' : '◑'}
            </IconButton>
            {/* 用户菜单（§2.3）：Quick 动作仅全量 admin（readonly_admin 不见
                快速建仓等写入口——L4 预收敛，服务端 403 兜底）。Esc/backdrop
                关闭、↑↓ 循环、关闭回焦锚钮均为 Menu 原生；锚
                （session-toggle / quick-* / menu-edit-profile / logout-button）
                全部保持。Chip 根 span（button 内禁嵌 interactive） */}
            <div>
              <Button
                color="inherit"
                ref={sessionToggleRef}
                aria-haspopup="menu"
                aria-expanded={menuOpen}
                data-testid="session-toggle"
                onClick={() => setMenuOpen((v) => !v)}
                sx={{ minWidth: 0, padding: '0 var(--bf-sp-2)' }}
              >
                <span aria-hidden="true">▣</span>
                <span data-testid="session-user" style={{ fontWeight: 600 }}>
                  {session?.username ?? ''}
                </span>
                {admin && !readOnlyAdmin && (
                  <Chip size="small" color="default" label="admin" component="span" />
                )}
                {readOnlyAdmin && (
                  <Chip
                    size="small"
                    color="default"
                    label="只读"
                    component="span"
                    data-testid="session-readonly-badge"
                    title="readonly_admin：管理面只读（服务端 403 兜底）"
                  />
                )}
                <span aria-hidden="true">▾</span>
              </Button>
              <Menu
                open={menuOpen}
                onClose={() => setMenuOpen(false)}
                anchorEl={sessionToggleRef.current}
                anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
                transformOrigin={{ vertical: 'top', horizontal: 'right' }}
              >
                {admin && !readOnlyAdmin && (
                  <>
                    <div className="menu-label" role="presentation">
                      快速建仓
                    </div>
                    <MenuItem
                      data-testid="quick-set-me-up"
                      onClick={() => {
                        setMenuOpen(false)
                        setSmuOpen(true)
                      }}
                    >
                      Set Me Up
                    </MenuItem>
                    <MenuItem component={Link} to="/admin/repositories/new?rclass=local" onClick={() => setMenuOpen(false)}>
                      新建 Local 仓库
                    </MenuItem>
                    <MenuItem
                      component={Link}
                      data-testid="quick-new-repo-remote"
                      to="/admin/repositories/new?rclass=remote"
                      onClick={() => setMenuOpen(false)}
                    >
                      新建 Remote 仓库
                    </MenuItem>
                    <MenuItem component={Link} to="/admin/repositories/new?rclass=virtual" onClick={() => setMenuOpen(false)}>
                      新建 Virtual 仓库
                    </MenuItem>
                    <div className="menu-label" role="presentation">
                      新建
                    </div>
                    <MenuItem component={Link} data-testid="quick-new-user" to="/admin/security/users" onClick={() => setMenuOpen(false)}>
                      新建用户
                    </MenuItem>
                    <MenuItem component={Link} to="/admin/security/groups" onClick={() => setMenuOpen(false)}>
                      新建组
                    </MenuItem>
                    <MenuItem
                      component={Link}
                      data-testid="quick-new-perm"
                      to="/admin/security/permissions/new"
                      onClick={() => setMenuOpen(false)}
                    >
                      新建权限
                    </MenuItem>
                    <Divider component="div" role="presentation" sx={{ my: 'var(--bf-sp-1)' }} />
                  </>
                )}
                <MenuItem component={Link} data-testid="menu-edit-profile" to="/profile" onClick={() => setMenuOpen(false)}>
                  编辑档案
                </MenuItem>
                <MenuItem onClick={() => void toggle()}>{theme === 'dark' ? '切换亮色主题' : '切换暗色主题'}</MenuItem>
                <MenuItem data-testid="logout-button" color="error" onClick={() => void doLogout()}>
                  登出
                </MenuItem>
              </Menu>
            </div>
          </Toolbar>
        </AppBar>
        <Container component="main" maxWidth={false} sx={{ maxWidth: 1440, px: 'var(--bf-sp-5)', py: 'var(--bf-sp-5)' }}>
          <Outlet />
        </Container>
      </Box>
      {/* 全局 Set Me Up 入口承载（quick-set-me-up 接线，T-244）+ OIDC
          step-up 回跳续铸承载（T-260）——T-382 起壳为右抽屉（fixed 定位
          不随壳布局）；关闭回焦菜单钮（续铸态关闭 = 放弃 grant + pending） */}
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
    </Box>
  )
}
