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
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
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
import Stack from '@mui/material/Stack'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import Toolbar from '@mui/material/Toolbar'
import Typography from '@mui/material/Typography'
import type { Theme } from '@mui/material/styles'

import { useAuth } from '../app/AuthContext'
import { useTheme } from '../app/ThemeContext'
import { useToast } from '../app/ToastContext'
import { BrandMark } from './BrandLogo'
import { useConfirm } from './ConfirmDialog'
import { NavIcon } from './NavIcons'
import type { NavIconName } from './NavIcons'
import SetMeUpDialog from './SetMeUpDialog'
import { abandonStepUp, useStepUp } from '../lib/stepUpGrant'
import type { PendingMint } from '../lib/stepUpGrant'
import { useVersion } from '../lib/useVersion'
import { errText, isReadOnlyAdmin } from '../lib/api'
import { tr, getLocale, setLocale } from '../i18n'
import type { Locale } from '../i18n'

const tt = tr('console')

// 语言名走母语名（endonym）：zh 态显示「中文」、en 态仍显示「中文」——
// 语言名不随 locale 翻译（切换器上看到目标语言的自称是通行做法，对位
// Artifactory Profile 语言下拉的 native label）。故豁免硬编码闸（行尾）。
const LOCALE_LABEL_ZH = '中文' // i18n-allow

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
// T-388 N2/V5：一级条目接 16px mono 图标（NavIcons.tsx——Material 通用
// 符号内联 SVG，currentColor 随文字色；分组标签与底部模式切换项不配——
// V5 档位仅一级条目，模式切换项维持既有字形）。
//
// 模式切换 = 侧栏底部常驻项（应用模式显「管理」，管理模式显「返回应用」；
// readonly_admin 可见——M7 语义保留清单 §7.3，含会话徽章「只读」）。
// 顶栏（§2.1）：面包屑/标题 · 搜索制品（T-265 起真输入框；T-449 起 =
// 搜索页驻留查询面——Enter 提交 /search?q=、/search 上 replace；Esc 清空、
// 最近词下拉聚焦恒渲染含空历史占位「暂无最近搜索」，FR-82-AC9 / parity
// B-2.13·B-3.16）· 帮助 · 主题 · 用户菜单（Quick 动作 = §2.3；仅全量
// admin 渲染写入口，readonly_admin 不见快速建仓）。
// 「不建」清单零影子入口（ADR-0029 决策 5）：无 Proxies/包索引/独立
// 产品入口；单实例无范围下拉（§1.2 单值不渲染口径）。
// testid 242 锚不随路由改名（console-ux §10/§10.5——W 资产保全）。

interface NavEntry {
  label: string
  to: string
  /** 一级条目图标（T-388 N2/V5 档位：mono currentColor，NavIcons.tsx 闭集） */
  icon: NavIconName
  /** 仅精确匹配算 active（无子路由的叶子；默认前缀匹配覆盖子路径） */
  end?: boolean
}

interface NavGroup {
  title: string
  entries: NavEntry[]
}

/** 应用模式侧栏（console-m8 §1.3 全图：应用分组 2 条目；T-514 起第三
 * 条目 = Release Bundles——Artifactory 应用域 /ui/repobundles 的 BinFlow
 * 对位〔console-ui §1.1 应用域族；官方归属 Artifactory 应用域，OSS 7.84
 * 册无 Distribution 不列——BinFlow M17 域已落地故入册〕；专域名词对位
 * Access Tokens / Webhooks 先例不译） */
const APP_NAV: NavGroup[] = [
  {
    title: tt('应用'),
    entries: [
      { label: tt('仪表盘'), to: '/dashboard', icon: 'dashboard', end: true },
      { label: tt('制品'), to: '/artifacts', icon: 'account_tree' },
      // T-514（FR-153.3）：bundle 列表/详情只读查询面（读门 = 系统读权限
      // ∨ Any Distribution 通道——普通用户 200 空集可见空态，服务端零泄漏）
      { label: 'Release Bundles', to: '/bundles', icon: 'bundle' },
    ],
  },
]

/** 管理模式侧栏（console-m8 §1.3 全图：五分组；**T-459（FR-145.6b /
 * parity B-2.18）7.161 形态重排**：Webhooks 归常规组 / 维护·备份归
 * 服务节点组〔监控〕/ 系统信息自常规组归位服务节点组 + 监控组扩三页
 * （存储/服务状态/系统日志）——18 条目；分组标题是标签不是折叠项——
 * 沿 console-ux §3.1 纪律。认证组子项形态（7.161 Authentication 组的
 * LDAP/SAML SSO/OAuth SSO/HTTP SSO/Crowd·JIRA/SCIM 六子项——探针
 * reports/agents/t459-probe/）中 HTTP SSO/Crowd·JIRA/SCIM 三域在 BinFlow
 * 不存在（FR-92 闭集 = LDAP/OAuth/SAML）——单页三页签维持，缺位登记
 * 不伪造）。
 * T-388 N2/V5：一级条目逐条接 16px mono 图标（Material 通用符号，随文字色）；
 * 分组标签不配（V5 实测档位：仅一级条目、子项/父级标签裸文本） */
const ADMIN_NAV: NavGroup[] = [
  {
    title: tt('仓库'),
    entries: [{ label: tt('仓库'), to: '/admin/repositories', icon: 'inventory_2' }],
  },
  {
    title: tt('用户与权限'),
    entries: [
      { label: tt('用户'), to: '/admin/security/users', icon: 'person' },
      { label: tt('组'), to: '/admin/security/groups', icon: 'group' },
      { label: tt('权限'), to: '/admin/security/permissions', icon: 'lock' },
      { label: 'Access Tokens', to: '/admin/security/tokens', icon: 'vpn_key' },
      // M11 T-307：认证配置（FR-92——LDAP/OAuth/SAML 三协议；readonly_admin
      // 只读可见，普通 user 不入管理面）
      { label: tt('认证配置'), to: '/admin/security/auth/ldap', icon: 'shield' },
    ],
  },
  {
    title: tt('治理'),
    entries: [
      { label: tt('审计日志'), to: '/admin/governance/audit', icon: 'history' },
      { label: tt('配额'), to: '/admin/governance/quotas', icon: 'pie_chart' },
      { label: tt('复制'), to: '/admin/governance/replication', icon: 'sync' },
      // M12 T-352：回收站（FR-106——浏览/恢复/清空；trashcan 槽门控态呈现）
      { label: tt('回收站'), to: '/admin/governance/trash', icon: 'delete' },
    ],
  },
  {
    title: tt('监控'),
    entries: [
      { label: tt('存储'), to: '/admin/monitoring/storage', icon: 'storage' },
      // T-459（FR-145.5）：监控组三页 + 归位两页——服务状态（health/schedules
      // 只读运行面）/ 系统日志（审计跟踪尾随查看器）/ 系统信息（自常规组
      // 归位，路由 /admin/monitoring/system-info）/ 维护（GC）与备份恢复
      // （自治理组迁入——服务级页挂服务节点组，T-462 的 cron 消费面同场）
      { label: tt('服务状态'), to: '/admin/monitoring/status', icon: 'pulse' },
      { label: tt('系统日志'), to: '/admin/monitoring/logs', icon: 'article' },
      { label: tt('系统信息'), to: '/admin/monitoring/system-info', icon: 'info' },
      { label: tt('维护（GC）'), to: '/admin/monitoring/gc', icon: 'delete_sweep' },
      { label: tt('备份 / 恢复'), to: '/admin/monitoring/backup', icon: 'backup' },
    ],
  },
  {
    title: tt('常规'),
    entries: [
      // M13 T-366：Webhook 订阅（FR-115.5）；T-459 归常规组（B-2.18——
      // 7.161 管理导航不再单列 Webhooks 条目，BinFlow 保留页面归常规组）
      { label: 'Webhooks', to: '/admin/general/webhooks', icon: 'bolt' },
      // M10 T-288：License & Add-ons（FR-86-AC5——readonly_admin 只读可见，
      // 写入口页内按角色收敛；普通 user 不入管理面）
      { label: 'License & Add-ons', to: '/admin/general/license', icon: 'card_membership' },
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
    const crumbs: Crumb[] = [{ label: tt('仓库'), to: '/admin/repositories/local' }]
    if (rest.length === 0 || ['local', 'remote', 'virtual'].includes(rest[0])) return crumbs
    if (rest[0] === 'new') return [...crumbs, { label: tt('新建仓库') }]
    crumbs.push({ label: safeDecode(rest[0]) })
    if (rest[1] === 'edit') crumbs.push({ label: tt('编辑') })
    return crumbs
  }
  const sec: Record<string, string> = {
    users: tt('用户'),
    groups: tt('组'),
    permissions: tt('权限'),
    tokens: 'Access Tokens',
    auth: tt('认证配置'),
  }
  if (pathname.startsWith('/admin/security/')) {
    const rest = pathname.slice('/admin/security/'.length).split('/')
    const label = sec[rest[0]] ?? ''
    const crumbs: Crumb[] = [
      { label: tt('用户与权限'), to: '/admin/security/users' },
      { label, to: `/admin/security/${rest[0]}` },
    ]
    // 认证配置三协议段显名（ldap/oauth/saml → Tab 名；T-307）
    const proto: Record<string, string> = { ldap: 'LDAP', oauth: 'OAuth (OIDC)', saml: 'SAML SSO' }
    if (rest[0] === 'auth' && proto[rest[1]]) crumbs.push({ label: proto[rest[1]] })
    else if (rest[1] && rest[1] !== 'new') crumbs.push({ label: safeDecode(rest[1]) })
    else if (rest[1] === 'new') crumbs.push({ label: tt('新建') })
    return crumbs
  }
  const gov: Record<string, string> = {
    audit: tt('审计日志'),
    quotas: tt('配额'),
    replication: tt('复制'),
    trash: tt('回收站'),
  }
  if (pathname.startsWith('/admin/governance/')) {
    const seg = pathname.slice('/admin/governance/'.length)
    return [{ label: tt('治理'), to: '/admin/governance/audit' }, { label: gov[seg] ?? seg }]
  }
  if (pathname.startsWith('/admin/monitoring/')) {
    // T-459：监控组扩为六页（存储/服务状态/系统日志/系统信息 + 归位的
    // 维护与备份）——段名 → 条目名镜像 ADMIN_NAV
    const mon: Record<string, string> = {
      storage: tt('存储'),
      status: tt('服务状态'),
      logs: tt('系统日志'),
      'system-info': tt('系统信息'),
      gc: tt('维护（GC）'),
      backup: tt('备份 / 恢复'),
    }
    const seg = pathname.slice('/admin/monitoring/'.length).split('/')[0]
    return [{ label: tt('监控'), to: '/admin/monitoring/storage' }, { label: mon[seg] ?? seg }]
  }
  if (pathname.startsWith('/admin/general/')) {
    // T-459：常规分组两页（Webhooks / License & Add-ons——系统信息已归
    // 监控组；旧 settings 深链经路由表 replace 折入新址）
    const gen: Record<string, string> = {
      webhooks: 'Webhooks',
      license: 'License & Add-ons',
    }
    const seg = pathname.slice('/admin/general/'.length).split('/')[0]
    return [{ label: tt('常规'), to: '/admin/general/webhooks' }, { label: gen[seg] ?? seg }]
  }
  if (pathname === '/admin') return [{ label: tt('管理') }]
  return [{ label: tt('管理') }]
}

/** 应用模式顶栏标题（无层级，直接页面名） */
function appTitle(pathname: string): string {
  if (pathname.startsWith('/artifacts')) return tt('制品')
  if (pathname.startsWith('/dashboard')) return tt('仪表盘')
  if (pathname.startsWith('/search')) return tt('搜索制品')
  if (pathname.startsWith('/profile')) return tt('编辑档案')
  if (pathname.startsWith('/bundles')) return 'Release Bundles'
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

  // 顶栏搜索（console-m8 §2.1 / FR-82-AC9，T-265 升真输入框；T-449 起
  //   = 搜索页的驻留查询面，parity B-2.13 翻正）：
  //   Enter → /search?q=<词>（顶栏提交即写入 recentSearches）；空词
  //   Enter = 纯入口跳 /search（沿 T-235 前身通道）。已在 /search 时
  //   replace（连续细化查询不逐词进栈——接替原 SearchPage 的
  //   replaceState 写回环）。
  //   驻留回显：URL 的 q 即查询事实源——/search 上同步输入框显值（导航
  //   触发；编辑未提交不回写，输入框是草稿层）。
  //   Esc 两段：先收最近词下拉，再清空 + 失焦。
  //   最近词下拉（T-449 / B-3.16 翻正）：聚焦即渲染——空历史给
  //   「暂无最近搜索」占位（对位 Artifactory 恒渲染 "No recent searches
  //   yet"）；有历史按当前词子串过滤、↑↓ 循环 + Enter 应用、一键清除。
  const topbarSearchRef = useRef<HTMLInputElement>(null)
  // 驻留回显（派生态零 effect）：URL 的 q 是查询事实源；草稿 = 用户在
  // 「当前 location」上敲的未提交词（location.key 变即让位 URL 回显——
  // 导航后输入框显示已提交查询，编辑中不回跳）
  const urlQ = location.pathname === '/search' ? (new URLSearchParams(location.search).get('q') ?? '').trim() : ''
  const [draft, setDraft] = useState<{ key: string; term: string }>({ key: '', term: '' })
  const searchTerm = draft.key === location.key ? draft.term : urlQ
  const [recent, setRecent] = useState<string[]>(() => loadRecentSearches())
  const [recentOpen, setRecentOpen] = useState(false)
  const [recentActive, setRecentActive] = useState(-1)
  // 按当前词子串过滤（空词 = 全量历史；无匹配 = 占位提示）
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
    navigate(`/search?q=${encodeURIComponent(term)}`, { replace: onSearch })
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
      // 两段 Esc：下拉开（含空历史占位态——T-449 起聚焦恒渲染）先收它；
      // 否则清空 + 失焦。preventDefault 阻断 input[type=search] 的原生
      // Esc 清空（清空触发 input 事件 → onChange 重开下拉——收下拉动作用
      // 会被原生副作用抵消；清空由第二段的显式 setDraft 承载）。
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

  // ---- 侧栏 Search Admin Resources 过滤框（T-459 / FR-145.6b——parity
  // B-2.18）--------------------------------------------------------------
  // 7.161.20 活体形态（探针 reports/agents/t459-probe/ s3-*）：管理模式
  // **顶栏**出现占位 "Search Admin Resources" 的 340px 过滤框（A1-5：位置
  // = 顶栏非侧栏——B-2.18 原始审计词「侧栏过滤」按活体勘误落顶栏；本实例
  // 活体上输入/Enter 均无可观测过滤效果，过滤语义按 7.84 经典行为〔过滤
  // 管理侧栏〕承载——差异留痕 parity 册 B-2.18 行）。客户端子串匹配条目
  // 文案（含英文术语——case-insensitive）；整组空则分组标签一并隐藏；
  // 全空给 admin-filter-empty 注记。管理模式下顶栏制品搜索位让位（7.161
  // 同为管理态单框），⌘K / 「/」聚焦随模式指向当前框。
  const [adminFilter, setAdminFilter] = useState('')
  const adminFilterRef = useRef<HTMLInputElement>(null)
  const adminMode = mode === 'admin'
  const adminQ = adminFilter.trim().toLowerCase()
  const adminGroups: NavGroup[] = adminQ
    ? ADMIN_NAV.map((g) => ({
        ...g,
        entries: g.entries.filter((e) => e.label.toLowerCase().includes(adminQ)),
      })).filter((g) => g.entries.length > 0)
    : ADMIN_NAV

  // 全局搜索快捷键（console-m8 §3.4）：⌘K / Ctrl+K / 「/」（输入框内
  // 不劫持）；modal（危险确认框等）打开时让位（review N2）。T-265 起顶栏
  // 是真输入框——快捷键聚焦它（⌘K 附带全选，便于直接覆写），Enter 即提交。
  // T-459：管理模式顶栏是管理资源过滤框——快捷键随模式指向当前框
  // （adminMode 变化时重挂监听，避免闭包陈旧）。
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (document.querySelector('[role="dialog"], .modal-backdrop')) return
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

  // 用户菜单（MUI Menu 原生）：Esc 关闭 / 点击外部（backdrop）关闭 / ↑↓
  // 循环由 Menu+MenuList 承载；焦点语义按 MUI 惯例——开菜单即聚焦首项、
  // 关闭回焦锚钮。T-344 批 A 摘 paper sx 复刻块：菜单壳交 MUI 默认
  // （elevation paper / 主题密度档）。

  // 帮助下拉 + About 版本弹窗（T-457 / FR-145.6a——parity B-2.17 翻正：
  // 纯链接 → ? 下拉）。四项 = Documentation（/binflow/docs/——console-ux
  // §3.5 定案链接形态）/ Online Training（无对应服务——按 7.161 活体形态
  // 处置为禁用占位 + 行内如实注记，登记不伪造）/ Release Notes（实链
  // docs/user/install/upgrade「升级与版本说明」）/ About（版本弹窗——
  // 消费 /api/system/version，与侧栏脚注 nav-about 同一入口）。7.161.20
  // 活体平台菜单 = JFrog Documentation / JFrog Academy / Navigation Tour
  // 三项（探针 reports/agents/t457-probe/——无 About/Release Notes 项；
  // 本四项集 = PRD FR-145.6a 定案，差异留痕 parity 册 B-2.17）。
  const [helpOpen, setHelpOpen] = useState(false)
  const helpToggleRef = useRef<HTMLButtonElement>(null)
  const [aboutOpen, setAboutOpen] = useState(false)
  const openAbout = () => {
    setHelpOpen(false)
    setAboutOpen(true)
  }

  const doLogout = async () => {
    setMenuOpen(false)
    const ok = await confirm({
      title: tt('登出'),
      body: tt('将结束当前会话并返回登录页；会话在服务端吊销，浏览器回退无法恢复。'),
      confirmLabel: tt('登出'),
    })
    if (!ok) return
    try {
      await logout()
      toast.success(tt('已登出'))
    } catch (err) {
      toast.error(tt('登出失败：{v1}', { v1: errText(err) }))
    }
  }

  if (status === 'checking') {
    return (
      <div className="boot-screen">
        <CircularProgress size={18} aria-label={tt('会话验证中')} sx={{ mr: 'var(--bf-sp-2)' }} />{tt('正在验证会话…')}      </div>
    )
  }

  if (status === 'anonymous') {
    const target = location.pathname + location.search
    const safe = target.startsWith('/') && !target.startsWith('//') ? target : '/'
    return <Navigate to={`/login?return=${encodeURIComponent(safe)}`} replace />
  }

  const groups = mode === 'admin' ? adminGroups : APP_NAV
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
        <nav className="app-nav" aria-label={tt('主导航')} data-testid="app-nav" style={{ display: 'flex', flexDirection: 'column', minHeight: '100%' }}>
          {/* 品牌位（FR-126 / T-389）：mark 24px + 产品名。侧栏两主题恒为
              深底 → 固定 mark-dark 变体（BrandMark 单点引用）；app-nav-brand
              结构与 .name 类钩不动，占位字形退役为 mark（grep 面零品牌残留）。 */}
          <Box className="app-nav-brand" sx={{ display: 'flex', alignItems: 'center', gap: 'var(--bf-sp-2)', padding: 'var(--bf-sp-4)', borderBottom: '1px solid var(--bf-sidebar-border)' }}>
            <BrandMark size={24} testid="brand-sidebar-mark" />
            <Typography className="name" variant="subtitle1" sx={{ fontWeight: 600, color: 'var(--bf-sidebar-text)' }}>
              BinFlow
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
                    {/* T-388 N2：一级条目 16px mono 图标（currentColor 随文字色，
                        active/hover 态零额外控色；aria-hidden 装饰——文字承载语义） */}
                    <NavIcon name={entry.icon} />
                    {entry.label}
                  </ListItemButton>
                ))}
              </div>
            ))}
            {/* T-459：管理资源过滤无匹配注记（整侧栏条目全被滤掉时——
                如实反馈而非空白侧栏；Esc 清词走输入框） */}
            {adminMode && adminQ && adminGroups.length === 0 && (
              <Typography
                variant="caption"
                data-testid="admin-filter-empty"
                sx={{ display: 'block', padding: 'var(--bf-sp-2) var(--bf-sp-3)', color: 'var(--bf-sidebar-text-2)' }}
              >{tt('「')}{adminFilter.trim()}{tt('」无匹配管理资源')}              </Typography>
            )}
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
                title={mode === 'admin' ? tt('返回应用模式') : tt('进入管理模式（/admin）')}
                onClick={() => navigate(mode === 'admin' ? APP_HOME : ADMIN_HOME)}
                sx={(t) => ({ ...navItemSx(t), marginBottom: 'var(--bf-sp-2)' })}
              >
                <span aria-hidden="true">{mode === 'admin' ? '↩' : '⚙'}</span>
                {mode === 'admin' ? tt('返回应用') : tt('管理')}
              </ListItemButton>
            )}
            {/* T-464（FR-149.1/.2）：语言切换器——位置票内定案 = 侧栏脚。
                册面候选 = 侧栏脚或 Profile（PRD FR-149.1「位置归 ux 定」，
                锚册无 ux 终裁；7.161 活体参照不可用——容器损坏）。取侧栏脚：
                ① 与模式切换/版本行同属「全局 + 常驻 + 个人偏好」控件层；
                ② 全站生效一步可达（Profile 需一次导航，BinFlow Profile 页
                  无编辑态表单可挂）；③ 两侧栏（应用/管理）同脚注常驻。
                切换 = setLocale（持久化 + 整页 reload，T-463 定案 3——
                模块级 t() 求值点按新 locale 重新求值）；当前 locale =
                选中态 + aria-pressed（ToggleButton 原生）。 */}
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 'var(--bf-sp-2)', marginBottom: 'var(--bf-sp-2)' }}>
              <Typography variant="caption" sx={{ color: 'var(--bf-sidebar-text-2)' }}>{tt('语言')}</Typography>
              <ToggleButtonGroup
                size="small"
                exclusive
                data-testid="nav-locale"
                aria-label={tt('语言')}
                value={getLocale()}
                onChange={(_e, v) => {
                  if (v) setLocale(v as Locale)
                }}
                sx={{
                  marginLeft: 'auto',
                  '& .MuiToggleButton-root': {
                    padding: '1px 10px',
                    fontSize: 'var(--bf-fs-aux)',
                    lineHeight: 1.6,
                    color: 'var(--bf-sidebar-text)',
                    borderColor: 'var(--bf-sidebar-border)',
                    '&.Mui-selected': { color: 'var(--bf-sidebar-active, inherit)' },
                  },
                }}
              >
                <ToggleButton value="zh" data-testid="nav-locale-zh">{LOCALE_LABEL_ZH}</ToggleButton>
                <ToggleButton value="en" data-testid="nav-locale-en">English</ToggleButton>
              </ToggleButtonGroup>
            </Box>
            {/* 许可行（§1.1）：版本来自 /api/system/version（nav-version 锚不变）。
                T-457：脚注升格为 About 版本弹窗入口（B-2.17——vdev 行可点，
                开 About 弹窗；文案与版本呈现零变化） */}
            <ListItemButton
              component="button"
              type="button"
              className="app-nav-license"
              data-testid="nav-about"
              title={tt('关于 BinFlow（版本 / 构建信息）')}
              onClick={() => setAboutOpen(true)}
              sx={(t) => ({
                ...navItemSx(t),
                padding: 'var(--bf-sp-1) var(--bf-sp-2)',
                justifyContent: 'flex-start',
                minHeight: 0,
                marginTop: 'auto',
                '& .MuiTypography-root': { color: 'inherit' },
              })}
            >
              <Typography variant="caption" sx={{ textAlign: 'left' }}>
                BinFlow <span data-testid="nav-version" lang="en">{version ? `v${version.version}` : '—'}</span> {tt('· 单二进制制品仓库')}              </Typography>
            </ListItemButton>
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
                aria-label={tt('位置')}
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
            {adminMode ? (
              /* 管理资源过滤框（T-459 / B-2.18——7.161 管理态顶栏单框形态；
                  过滤语义 = 管理侧栏条目客户端子串；Esc 清词。制品搜索在
                  管理模式让位（7.161 同为单框），回应用模式即恢复） */
              <Paper
                component="div"
                role="search"
                elevation={0}
                className="search-entry"
                sx={{
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
                  inputRef={adminFilterRef}
                  type="search"
                  placeholder="Search Admin Resources…"
                  slotProps={{
                    input: {
                      'data-testid': 'admin-filter',
                      'aria-label': tt('搜索管理资源（过滤管理侧栏条目）'),
                      autoComplete: 'off',
                    } as ComponentPropsWithoutRef<'input'>,
                  }}
                  value={adminFilter}
                  onChange={(e) => setAdminFilter(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Escape') {
                      // 与制品搜索同款两段 Esc：先清词（preventDefault 阻断
                      // input[type=search] 原生清空的 onChange 重入）
                      e.preventDefault()
                      setAdminFilter('')
                    }
                  }}
                  sx={{ flex: 1, minWidth: 0 }}
                />
                <kbd aria-hidden="true">⌘K</kbd>
              </Paper>
            ) : (
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
                placeholder={tt('搜索制品…')}
                slotProps={{
                  // data-testid 落 input 本体（topbar-search 锚）；MUI v7 的
                  // slot 类型不容 data-* 属性，按 input 元素 props 断言放行
                  input: {
                    'data-testid': 'topbar-search',
                    'aria-label': tt('搜索制品'),
                    autoComplete: 'off',
                  } as ComponentPropsWithoutRef<'input'>,
                }}
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
                sx={{ flex: 1, minWidth: 0 }}
              />
              <kbd aria-hidden="true">⌘K</kbd>
              {recentOpen && (
                <div className="topbar-search-recent" data-testid="topbar-search-recent">
                  <div className="search-recent-head">
                    <span>{tt('最近搜索')}</span>
                    {recent.length > 0 && (
                      <button
                        type="button"
                        className="copy-btn"
                        data-testid="topbar-search-recent-clear"
                        onMouseDown={(e) => e.preventDefault()}
                        onClick={clearTopbarRecent}
                      >{tt('清除历史')}                      </button>
                    )}
                  </div>
                  {recentList.length > 0 ? (
                    <ul className="search-recent-list" aria-label={tt('最近搜索')}>
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
                  ) : (
                    // 空历史占位恒渲染（T-449 / B-3.16——对位 Artifactory
                    // "No recent searches yet"；含子串无匹配态）
                    <p className="search-recent-empty" data-testid="topbar-search-recent-empty">
                      {recent.length === 0 ? tt('暂无最近搜索') : tt('「{v1}」无匹配历史', { v1: searchTerm.trim() })}
                    </p>
                  )}
                </div>
              )}
            </Paper>
            )}
            {/* 帮助下拉（T-457 / B-2.17 翻正）：topbar-help 锚语义翻新零改名
                （链接 → 下拉触发钮——aria-haspopup/expanded，四项见上方注释） */}
            <div>
              <Button
                className="topbar-help"
                color="inherit"
                ref={helpToggleRef}
                aria-haspopup="menu"
                aria-expanded={helpOpen}
                title={tt('帮助')}
                data-testid="topbar-help"
                onClick={() => setHelpOpen((v) => !v)}
                sx={{ minWidth: 0, padding: '0 var(--bf-sp-2)' }}
              >
                <span aria-hidden="true">?</span> {tt('帮助')}              </Button>
              <Menu
                open={helpOpen}
                onClose={() => setHelpOpen(false)}
                anchorEl={helpToggleRef.current}
                anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
                transformOrigin={{ vertical: 'top', horizontal: 'right' }}
              >
                <MenuItem
                  component="a"
                  href="/binflow/docs/"
                  target="_blank"
                  rel="noopener noreferrer"
                  data-testid="help-docs"
                  onClick={() => setHelpOpen(false)}
                >
                  Documentation
                </MenuItem>
                {/* 无对应服务：7.161 活体此项是外链（JFrog Academy）——BinFlow
                    无培训站点，外链无处可指；禁用 + 行内注记 = 诚实占位 */}
                <MenuItem disabled data-testid="help-training">{tt('Online Training（暂无对应服务）')}                </MenuItem>
                <MenuItem
                  component="a"
                  href="/binflow/docs/install/upgrade"
                  target="_blank"
                  rel="noopener noreferrer"
                  data-testid="help-release-notes"
                  onClick={() => setHelpOpen(false)}
                >
                  Release Notes
                </MenuItem>
                <Divider component="li" role="presentation" sx={{ my: 'var(--bf-sp-1)' }} />
                <MenuItem data-testid="help-about" onClick={openAbout}>
                  About
                </MenuItem>
              </Menu>
            </div>
            {/* 主题切换：title 原生 tooltip、aria-label、字形与
                ThemeContext 翻转链路零变化；皮肤交 MUI 默认 */}
            <IconButton
              aria-label={theme === 'dark' ? tt('切换亮色主题') : tt('切换暗色主题')}
              title={theme === 'dark' ? tt('切换亮色主题') : tt('切换暗色主题')}
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
                    label={tt('只读')}
                    component="span"
                    data-testid="session-readonly-badge"
                    title={tt('readonly_admin：管理面只读（服务端 403 兜底）')}
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
                    <div className="menu-label" role="presentation">{tt('快速建仓')}                    </div>
                    <MenuItem
                      data-testid="quick-set-me-up"
                      onClick={() => {
                        setMenuOpen(false)
                        setSmuOpen(true)
                      }}
                    >
                      Set Me Up
                    </MenuItem>
                    <MenuItem component={Link} to="/admin/repositories/new?rclass=local" onClick={() => setMenuOpen(false)}>{tt('新建 Local 仓库')}                    </MenuItem>
                    <MenuItem
                      component={Link}
                      data-testid="quick-new-repo-remote"
                      to="/admin/repositories/new?rclass=remote"
                      onClick={() => setMenuOpen(false)}
                    >{tt('新建 Remote 仓库')}                    </MenuItem>
                    <MenuItem component={Link} to="/admin/repositories/new?rclass=virtual" onClick={() => setMenuOpen(false)}>{tt('新建 Virtual 仓库')}                    </MenuItem>
                    <div className="menu-label" role="presentation">{tt('新建')}                    </div>
                    <MenuItem component={Link} data-testid="quick-new-user" to="/admin/security/users" onClick={() => setMenuOpen(false)}>{tt('新建用户')}                    </MenuItem>
                    <MenuItem component={Link} to="/admin/security/groups" onClick={() => setMenuOpen(false)}>{tt('新建组')}                    </MenuItem>
                    <MenuItem
                      component={Link}
                      data-testid="quick-new-perm"
                      to="/admin/security/permissions/new"
                      onClick={() => setMenuOpen(false)}
                    >{tt('新建权限')}                    </MenuItem>
                    <Divider component="div" role="presentation" sx={{ my: 'var(--bf-sp-1)' }} />
                  </>
                )}
                <MenuItem component={Link} data-testid="menu-edit-profile" to="/profile" onClick={() => setMenuOpen(false)}>{tt('编辑档案')}                </MenuItem>
                <MenuItem onClick={() => void toggle()}>{theme === 'dark' ? tt('切换亮色主题') : tt('切换暗色主题')}</MenuItem>
                <MenuItem data-testid="logout-button" color="error" onClick={() => void doLogout()}>{tt('登出')}                </MenuItem>
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
      {/* About 版本弹窗（T-457 / FR-145.6a——B-2.17）：消费 /api/system/version
          （useVersion 模块级缓存同源）；版本/构建信息如实呈现，失败 = —
          （Q4 不伪装）。入口两处：? 帮助下拉 help-about + 侧栏脚注 nav-about */}
      <Dialog
        open={aboutOpen}
        onClose={() => setAboutOpen(false)}
        maxWidth="xs"
        fullWidth
        data-testid="about-dialog"
        aria-labelledby="about-dialog-title"
      >
        <DialogTitle id="about-dialog-title">About</DialogTitle>
        <DialogContent dividers sx={{ display: 'grid', gap: 'var(--bf-sp-2)', pt: 1 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 'var(--bf-sp-2)' }}>
            <BrandMark size={32} testid="about-brand-mark" />
            <Box>
              <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
                BinFlow
              </Typography>
              <Typography variant="caption" color="text.secondary">{tt('单二进制云原生制品仓库')}              </Typography>
            </Box>
          </Box>
          <Divider />
          <Stack spacing={0.5}>
            <Typography variant="body2">{tt('版本：')}              <span className="mono" lang="en" data-testid="about-version">
                {version ? `v${version.version}` : '—'}
              </span>
            </Typography>
            <Typography variant="body2">{tt('构建：')}              <span className="mono" lang="en" data-testid="about-revision">
                {version ? version.revision : '—'}
              </span>
            </Typography>
            <Typography variant="body2">{tt('产品标识：')}              <span className="mono" lang="en" data-testid="about-product">
                {version ? version.product : '—'}
              </span>
            </Typography>
          </Stack>
          <Typography variant="caption" color="text.secondary">{tt('版本与构建信息来自 GET /api/system/version（开放端点）；License 详情见管理面 「Administration → License &amp; Add-ons」。')}          </Typography>
        </DialogContent>
        <DialogActions>
          <Box sx={{ flexGrow: 1 }} />
          <Button variant="contained" data-testid="about-close" onClick={() => setAboutOpen(false)}>{tt('关闭')}          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  )
}
