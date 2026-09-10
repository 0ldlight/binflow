// 侧栏 IA 模型（frontend-rewrite-architecture §4：四分组 + 权限可见性）
// ——页面路由全部保持现 URL（IA 重组=侧栏分组重组非路由重组）。
//
// 分组重排裁定（architecture §4）：应用⇄管理双模式概念并入四分组——
// Core / Operations / Security / Administration；去掉模式切换（分组即
// 模式，权限门控可见性替代 nav-mode-switch）。条目标签保持旧侧栏的
// 逐字文案（e2e 以 text-is 断言导航——m8/setmeup-deploy 等 spec 的
// `a.nav-item:text-is("制品")` 链路不因重排断链）。
//
// 锚纪律：.nav-item / .nav-group-label / .app-nav 类钩与 app-nav 锚
// 原样保留（§10.5 换栈零锚改名先例）；nav-mode-switch 锚随双模式概念
// 退役（登记 console-ux §10.6 退役表——P2 批）。
import type { LucideIcon } from 'lucide-react'
import {
  Activity,
  BadgeCheck,
  Boxes,
  ClipboardCopy,
  FileSearch,
  FolderTree,
  Gauge,
  HardDrive,
  KeyRound,
  Lock,
  ScrollText,
  ShieldCheck,
  Trash2,
  Users,
  Webhook,
} from 'lucide-react'

import { tr } from '@/i18n'

const t = tr('console')

/** 条目可见位（权限门控——服务端是唯一守门，这里只驱动呈现） */
export type NavVisibility = 'all' | 'admin-sight'

export interface NavItem {
  id: string
  /** 目标路由（保持现 URL 形态） */
  to: string
  icon: LucideIcon
  visibility: NavVisibility
  /** 导航条目标签（e2e text-is 契约——与旧侧栏逐字一致） */
  label: string
  /** 仅精确匹配算 active（无子路由的叶子；默认前缀匹配覆盖子路径） */
  end?: boolean
}

export interface NavGroup {
  id: string
  label: string
  items: NavItem[]
}

/** 四分组全条目（Core / Operations / Security / Administration——§4 IA） */
export const NAV_GROUPS: NavGroup[] = [
  {
    id: 'core',
    label: t('核心'),
    items: [
      { id: 'dashboard', to: '/dashboard', icon: Gauge, visibility: 'all', label: t('仪表盘'), end: true },
      { id: 'artifacts', to: '/artifacts', icon: FolderTree, visibility: 'all', label: t('制品') },
      { id: 'repositories', to: '/admin/repositories/local', icon: Boxes, visibility: 'admin-sight', label: t('仓库') },
      { id: 'search', to: '/search', icon: FileSearch, visibility: 'all', label: t('搜索'), end: true },
    ],
  },
  {
    id: 'operations',
    label: t('运营'),
    items: [
      { id: 'builds', to: '/builds', icon: Activity, visibility: 'all', label: 'Builds' },
      { id: 'bundles', to: '/bundles', icon: BadgeCheck, visibility: 'all', label: 'Release Bundles' },
      { id: 'replication', to: '/admin/governance/replication', icon: ClipboardCopy, visibility: 'admin-sight', label: t('复制') },
      { id: 'webhooks', to: '/admin/general/webhooks', icon: Webhook, visibility: 'admin-sight', label: 'Webhooks' },
      { id: 'trash', to: '/admin/governance/trash', icon: Trash2, visibility: 'admin-sight', label: t('回收站') },
    ],
  },
  {
    id: 'security',
    label: t('安全'),
    items: [
      { id: 'users', to: '/admin/security/users', icon: Users, visibility: 'admin-sight', label: t('用户') },
      { id: 'groups', to: '/admin/security/groups', icon: Users, visibility: 'admin-sight', label: t('组') },
      { id: 'permissions', to: '/admin/security/permissions', icon: Lock, visibility: 'admin-sight', label: t('权限') },
      { id: 'tokens', to: '/admin/security/tokens', icon: KeyRound, visibility: 'admin-sight', label: 'Access Tokens' },
      { id: 'keypair', to: '/admin/security/keypair', icon: KeyRound, visibility: 'admin-sight', label: t('签名密钥') },
      { id: 'auth', to: '/admin/security/auth/ldap', icon: ShieldCheck, visibility: 'admin-sight', label: t('认证配置') },
      { id: 'audit', to: '/admin/governance/audit', icon: ScrollText, visibility: 'admin-sight', label: t('审计日志') },
    ],
  },
  {
    id: 'administration',
    label: t('管理'),
    items: [
      { id: 'quotas', to: '/admin/governance/quotas', icon: Gauge, visibility: 'admin-sight', label: t('配额') },
      { id: 'storage', to: '/admin/monitoring/storage', icon: HardDrive, visibility: 'admin-sight', label: t('存储') },
      { id: 'status', to: '/admin/monitoring/status', icon: Activity, visibility: 'admin-sight', label: t('服务状态') },
      { id: 'logs', to: '/admin/monitoring/logs', icon: ScrollText, visibility: 'admin-sight', label: t('系统日志') },
      { id: 'gc', to: '/admin/monitoring/gc', icon: Trash2, visibility: 'admin-sight', label: t('维护（GC）') },
      { id: 'backup', to: '/admin/monitoring/backup', icon: HardDrive, visibility: 'admin-sight', label: t('备份 / 恢复') },
      { id: 'system-info', to: '/admin/monitoring/system-info', icon: Gauge, visibility: 'admin-sight', label: t('系统信息') },
      { id: 'settings', to: '/admin/monitoring/settings', icon: Gauge, visibility: 'admin-sight', label: t('设置'), end: true },
      { id: 'license', to: '/admin/general/license', icon: BadgeCheck, visibility: 'admin-sight', label: 'License & Add-ons' },
    ],
  },
]
