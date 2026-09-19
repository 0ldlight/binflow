// BinFlow navigation model aligned to Artifactory's interaction model, not a
// literal clone of its licensed/enterprise information architecture.
//
// Platform mode keeps the four in-scope Artifactory application entries in
// captured order. Administration mode keeps Artifactory's section order, but
// exposes every implemented BinFlow destination directly. Reference-only
// Pro/Projects faces stay in nav-parity.yaml instead of becoming dead UI.
import type { LucideIcon } from 'lucide-react'
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

/** Platform mode: Artifactory first; BinFlow-only supplements stay terminal. */
export const APP_NAV_GROUPS: NavGroup[] = [
  {
    id: 'artifactory',
    label: 'Artifactory',
    items: [
      item('packages', 'Packages', '/packages', Package, 'all'),
      item('builds', 'Builds', '/builds', Activity, 'all'),
      item('artifacts', 'Artifacts', '/artifacts', FolderTree, 'all'),
      item('release-lifecycle', 'Release Lifecycle', '/artifactory/release-lifecycle', BadgeCheck, 'all'),
    ],
  },
  {
    id: 'binflow-platform',
    label: 'BinFlow',
    items: [item('dashboard', 'Dashboard', '/dashboard', Gauge, 'all', { end: true })],
  },
]

/** Administration sections follow Artifactory order while remaining direct-use. */
export const ADMIN_NAV_GROUPS: NavGroup[] = [
  {
    id: 'repositories',
    label: 'Repositories',
    items: [item('repositories', 'Repositories', '/admin/repositories/local', Boxes, 'admin-sight')],
  },
  {
    id: 'user-management',
    label: 'User Management',
    items: [
      item('users', 'Users', '/admin/security/users', Users, 'admin-sight'),
      item('groups', 'Groups', '/admin/security/groups', Users, 'admin-sight'),
      item('permissions', 'Permissions', '/admin/security/permissions', Lock, 'admin-sight'),
      item('access-tokens', 'Access Tokens', '/admin/security/tokens', KeyRound, 'admin-sight'),
    ],
  },
  {
    id: 'authentication',
    label: 'Authentication',
    items: [item('ldap', 'LDAP', '/admin/security/auth/ldap', ShieldCheck, 'admin-sight')],
  },
  {
    id: 'security',
    label: 'Security',
    items: [item('signing-keys', 'Signing Keys', '/admin/security/keypair', KeyRound, 'admin-sight')],
  },
  {
    id: 'general-management',
    label: 'General Management',
    items: [item('general-settings', 'Settings', '/admin/monitoring/settings', Gauge, 'admin-sight')],
  },
  {
    id: 'monitoring',
    label: 'Monitoring',
    items: [
      item('service-status', 'Service Status', '/admin/monitoring/status', Activity, 'admin-sight'),
      item('storage', 'Storage', '/admin/monitoring/storage', HardDrive, 'admin-sight'),
      item('system-logs', 'System Logs', '/admin/monitoring/logs', ScrollText, 'admin-sight'),
      item('system-info', 'System Info', '/admin/monitoring/system-info', Gauge, 'admin-sight'),
    ],
  },
  {
    id: 'artifactory-settings',
    label: 'Artifactory Settings',
    items: [
      item('maintenance', 'Maintenance', '/admin/monitoring/gc', HardDrive, 'admin-sight'),
      item('backups', 'Backups', '/admin/monitoring/backup', HardDrive, 'admin-sight'),
    ],
  },
]

/** Terminal, clearly identified BinFlow-only capabilities. */
export const BINFLOW_NAV_GROUPS: NavGroup[] = [
  {
    id: 'binflow-extensions',
    label: 'BinFlow Extensions',
    items: [
      item('quotas', 'Quotas', '/admin/governance/quotas', Gauge, 'admin-sight'),
      item('replication', 'Replication', '/admin/governance/replication', ClipboardCopy, 'admin-sight'),
      item('trash', 'Trash', '/admin/governance/trash', HardDrive, 'admin-sight'),
      item('audit-log', 'Audit Log', '/admin/governance/audit', ScrollText, 'admin-sight'),
      item('webhooks', 'Webhooks', '/admin/general/webhooks', Webhook, 'admin-sight'),
      item('license-addons', 'License & Add-ons', '/admin/general/license', BadgeCheck, 'admin-sight'),
    ],
  },
]

/** Backward-compatible complete model for the command palette and older consumers. */
export const NAV_GROUPS: NavGroup[] = [...APP_NAV_GROUPS, ...ADMIN_NAV_GROUPS, ...BINFLOW_NAV_GROUPS]

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
