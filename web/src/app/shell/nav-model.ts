// BinFlow navigation model aligned to 同类控制台's interaction model, not a
// literal clone of its licensed/enterprise information architecture.
//
// Platform mode keeps the four in-scope 同类控制台 application entries in
// captured order. Administration mode keeps 同类控制台's section order, but
// exposes every implemented BinFlow destination directly. Reference-only
// Pro/Projects faces stay in nav-parity.yaml instead of becoming dead UI.
import type { LucideIcon } from 'lucide-react'

import { tr } from '@/i18n'
import {
  Activity,
  BadgeCheck,
  Boxes,
  ClipboardCopy,
  FolderTree,
  Gauge,
  HardDrive,
  KeyRound,
  Lock,
  Package,
  ScrollText,
  ShieldCheck,
  Users,
  Webhook,
} from 'lucide-react'

export type NavVisibility = 'all' | 'admin-sight'

const t = tr('console')

export interface NavItem {
  id: string
  to: string
  icon: LucideIcon
  visibility: NavVisibility
  label: string
  end?: boolean
  children?: NavItem[]
}

export interface NavGroup {
  id: string
  label: string
  items: NavItem[]
}

const item = (
  id: string,
  label: string,
  to: string,
  icon: LucideIcon,
  visibility: NavVisibility,
  extra: Partial<NavItem> = {},
): NavItem => ({ id, label, to, icon, visibility, ...extra })

/** Platform mode: 同类控制台 first; BinFlow-only supplements stay terminal. */
export const APP_NAV_GROUPS: NavGroup[] = [
  {
    id: 'product',
    label: 'BinFlow',
    items: [
      item('packages', '软件包', '/packages', Package, 'all'), // i18n-allow
      item('builds', '构建', '/builds', Activity, 'all'), // i18n-allow
      item('artifacts', '制品', '/artifacts', FolderTree, 'all'), // i18n-allow
      item('release-lifecycle', '发布生命周期', '/release-lifecycle', BadgeCheck, 'all'), // i18n-allow
    ],
  },
  {
    id: 'binflow-platform',
    label: 'BinFlow',
    items: [item('dashboard', '仪表盘', '/dashboard', Gauge, 'all', { end: true })], // i18n-allow
  },
]

/** Administration sections follow 同类控制台 order while remaining direct-use. */
export const ADMIN_NAV_GROUPS: NavGroup[] = [
  {
    id: 'repositories',
    label: '仓库', // i18n-allow
    items: [item('repositories', '仓库', '/admin/repositories', Boxes, 'admin-sight')], // i18n-allow
  },
  {
    id: 'user-management',
    label: '用户管理', // i18n-allow
    items: [
      item('users', '用户', '/admin/security/users', Users, 'admin-sight'), // i18n-allow
      item('groups', '组', '/admin/security/groups', Users, 'admin-sight'), // i18n-allow
      item('permissions', '权限', '/admin/security/permissions', Lock, 'admin-sight'), // i18n-allow
      item('access-tokens', '访问令牌', '/admin/security/tokens', KeyRound, 'admin-sight'), // i18n-allow
    ],
  },
  {
    id: 'authentication',
    label: '认证', // i18n-allow
    items: [item('ldap', 'LDAP', '/admin/security/auth/ldap', ShieldCheck, 'admin-sight')], // i18n-allow
  },
  {
    id: 'security',
    label: '安全', // i18n-allow
    items: [item('signing-keys', '签名密钥', '/admin/security/keypair', KeyRound, 'admin-sight')], // i18n-allow
  },
  {
    id: 'general-management',
    label: '通用管理', // i18n-allow
    items: [item('general-settings', '设置', '/admin/monitoring/settings', Gauge, 'admin-sight')], // i18n-allow
  },
  {
    id: 'monitoring',
    label: '监控', // i18n-allow
    items: [
      item('service-status', '服务状态', '/admin/monitoring/status', Activity, 'admin-sight'), // i18n-allow
      item('storage', '存储', '/admin/monitoring/storage', HardDrive, 'admin-sight'), // i18n-allow
      item('system-logs', '系统日志', '/admin/monitoring/logs', ScrollText, 'admin-sight'), // i18n-allow
      item('system-info', '系统信息', '/admin/monitoring/system-info', Gauge, 'admin-sight'), // i18n-allow
    ],
  },
  {
    id: 'repository-settings',
    label: '仓库设置', // i18n-allow
    items: [
      item('maintenance', '维护', '/admin/monitoring/gc', HardDrive, 'admin-sight'), // i18n-allow
      item('backups', '备份', '/admin/monitoring/backup', HardDrive, 'admin-sight'), // i18n-allow
    ],
  },
]

/** Terminal, clearly identified BinFlow-only capabilities. */
export const BINFLOW_NAV_GROUPS: NavGroup[] = [
  {
    id: 'binflow-extensions',
    label: 'BinFlow 扩展', // i18n-allow
    items: [
      item('quotas', '配额', '/admin/governance/quotas', Gauge, 'admin-sight'), // i18n-allow
      item('replication', '复制', '/admin/governance/replication', ClipboardCopy, 'admin-sight'), // i18n-allow
      item('trash', '回收站', '/admin/governance/trash', HardDrive, 'admin-sight'), // i18n-allow
      item('audit-log', '审计日志', '/admin/governance/audit', ScrollText, 'admin-sight'), // i18n-allow
      item('webhooks', 'Webhooks', '/admin/general/webhooks', Webhook, 'admin-sight'), // i18n-allow
      item('license-addons', '许可与扩展', '/admin/general/license', BadgeCheck, 'admin-sight'), // i18n-allow
    ],
  },
]

/** Backward-compatible complete model for the command palette and older consumers. */
export const NAV_GROUPS: NavGroup[] = [...APP_NAV_GROUPS, ...ADMIN_NAV_GROUPS, ...BINFLOW_NAV_GROUPS]

// Navigation data carries Chinese canonical labels so the default locale has no
// extra catalog load. This function is called during render (after initI18n),
// which lets the English catalog translate the data without losing cmdk filtering.
export function localizeNavGroups(groups: NavGroup[]): NavGroup[] {
  const labels: Record<string, string> = {
    软件包: t('软件包'),
    构建: t('构建'),
    制品: t('制品'),
    发布生命周期: t('发布生命周期'),
    仪表盘: t('仪表盘'),
    仓库: t('仓库'),
    用户: t('用户'),
    组: t('组'),
    权限: t('权限'),
    访问令牌: t('访问令牌'),
    LDAP: t('LDAP'),
    签名密钥: t('签名密钥'),
    设置: t('设置'),
    服务状态: t('服务状态'),
    存储: t('存储'),
    系统日志: t('系统日志'),
    系统信息: t('系统信息'),
    仓库设置: t('仓库设置'),
    维护: t('维护'),
    备份: t('备份'),
    'BinFlow 扩展': t('BinFlow 扩展'), // i18n-allow
    配额: t('配额'),
    复制: t('复制'),
    回收站: t('回收站'),
    审计日志: t('审计日志'),
    Webhooks: t('Webhooks'),
    许可与扩展: t('许可与扩展'),
    用户管理: t('用户管理'),
    认证: t('认证'),
    安全: t('安全'),
    通用管理: t('通用管理'),
    监控: t('监控'),
  }
  return groups.map((group) => ({
    ...group,
    label: labels[group.label] ?? group.label,
    items: group.items.map((entry) => ({ ...entry, label: labels[entry.label] ?? entry.label })),
  }))
}

export function flattenNavItems(items: NavItem[]): NavItem[] {
  return items.flatMap((entry) => [entry, ...flattenNavItems(entry.children ?? [])])
}

export function filterNavGroups(groups: NavGroup[], query: string): NavGroup[] {
  const q = query.trim().toLowerCase()
  if (!q) return groups
  return groups
    .map((group) => ({
    ...group,
      items: group.items.filter((entry) => entry.label.toLowerCase().includes(q)),
    }))
    .filter((group) => group.items.length > 0)
}
