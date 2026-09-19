// Artifactory-strict navigation model (7.161 E4 evidence).
// Visual style remains BinFlow monochrome; this model owns order, labels,
// hierarchy, mode semantics, and honest capability gaps.
// Evidence matrix: docs/reverse/frontend/nav-parity.yaml
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
  /** Stable BinFlow route. Reference-only gaps deliberately omit a destination. */
  to?: string
  icon: LucideIcon
  visibility: NavVisibility
  label: string
  end?: boolean
  children?: NavItem[]
  /** A disabled reference entry stays visible for IA parity but cannot fake a capability. */
  disabled?: boolean
}

export interface NavGroup {
  id: string
  label: string
  items: NavItem[]
}

const item = (
  id: string,
  label: string,
  to: string | undefined,
  icon: LucideIcon,
  visibility: NavVisibility,
  extra: Partial<NavItem> = {},
): NavItem => ({ id, label, to, icon, visibility, ...extra })

const gap = (id: string, label: string, icon: LucideIcon): NavItem =>
  item(id, label, undefined, icon, 'admin-sight', { disabled: true })

/** Platform mode / Artifactory application submenu. Exact sibling order is normative. */
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
]

/** Administration mode top-level order is the 7.161 E4 sidebar body order. */
export const ADMIN_NAV_GROUPS: NavGroup[] = [
  {
    id: 'administration',
    label: 'Administration',
    items: [
      gap('all-projects-overview', 'All Projects Overview', Gauge),
      gap('stages-lifecycle', 'Stages & Lifecycle', BadgeCheck),
      item(
        'repositories',
        'Repositories',
        '/admin/repositories/local',
        Boxes,
        'admin-sight',
        {
          children: [item('repositories-all', 'Repositories', '/admin/repositories/local', Boxes, 'admin-sight')],
        },
      ),
      item('user-management', 'User Management', '/admin/security/users', Users, 'admin-sight', {
        children: [
          item('users', 'Users', '/admin/security/users', Users, 'admin-sight'),
          item('groups', 'Groups', '/admin/security/groups', Users, 'admin-sight'),
          item('permissions', 'Permissions', '/admin/security/permissions', Lock, 'admin-sight'),
          gap('global-roles', 'Global Roles', Users),
          item('access-tokens', 'Access Tokens', '/admin/security/tokens', KeyRound, 'admin-sight'),
        ],
      }),
      gap('proxies', 'Proxies', Boxes),
      item('authentication', 'Authentication', '/admin/security/auth/ldap', ShieldCheck, 'admin-sight', {
        children: [
          item('ldap', 'LDAP', '/admin/security/auth/ldap', ShieldCheck, 'admin-sight'),
          gap('oauth-sso', 'OAuth SSO', KeyRound),
          gap('saml-sso', 'SAML SSO', KeyRound),
          gap('http-sso', 'HTTP SSO', KeyRound),
          gap('crowd-jira', 'Crowd / JIRA', Users),
          gap('scim', 'SCIM', Users),
        ],
      }),
      item('security', 'Security', '/admin/security/keypair', Lock, 'admin-sight', {
        children: [
          gap('security-general', 'General', ShieldCheck),
          gap('keys-management', 'Keys Management', KeyRound),
          item('signing-keys', 'Signing Keys', '/admin/security/keypair', KeyRound, 'admin-sight'),
          gap('trusted-keys', 'Trusted Keys', KeyRound),
          gap('certificates', 'Certificates', ShieldCheck),
          gap('vault', 'Vault', Lock),
        ],
      }),
      item('general-management', 'General Management', '/admin/monitoring/settings', Gauge, 'admin-sight', {
        children: [
          item('general-settings', 'Settings', '/admin/monitoring/settings', Gauge, 'admin-sight'),
          gap('mail-server', 'Mail Server', ScrollText),
        ],
      }),
      item('monitoring', 'Monitoring', '/admin/monitoring/storage', Activity, 'admin-sight', {
        children: [
          item('service-status', 'Service Status', '/admin/monitoring/status', Activity, 'admin-sight'),
          item('storage', 'Storage', '/admin/monitoring/storage', HardDrive, 'admin-sight'),
          item('system-logs', 'System Logs', '/admin/monitoring/logs', ScrollText, 'admin-sight'),
          gap('log-analytics', 'Log Analytics', ScrollText),
          gap('artifactory-logs', 'Artifactory Logs', ScrollText),
          gap('federation-status', 'Federation Status', Activity),
        ],
      }),
      gap('topology', 'Topology', Boxes),
      gap('support-zone', 'Support Zone', ShieldCheck),
      item('artifactory-settings', 'Artifactory Settings', '/admin/monitoring/settings', Boxes, 'admin-sight', {
        children: [
          item('artifactory-general-settings', 'General Settings', '/admin/monitoring/settings', Gauge, 'admin-sight'),
          gap('artifactory-security', 'Artifactory Security', Lock),
          gap('packages-settings', 'Packages Settings', Package),
          gap('http-settings', 'HTTP Settings', Boxes),
          gap('repository-imp-exp', 'Repository Imp/Exp', ClipboardCopy),
          gap('system-imp-exp', 'System Imp/Exp', ClipboardCopy),
          item('artifactory-repositories', 'Repositories', '/admin/repositories/local', Boxes, 'admin-sight'),
          gap('layouts', 'Layouts', FolderTree),
          gap('migration-tool', 'Migration Tool', Activity),
          gap('property-sets', 'Property Sets', ScrollText),
          gap('maven-indexer', 'Maven Indexer', Package),
          gap('config-descriptor', 'Config Descriptor', ScrollText),
          gap('security-descriptor', 'Security Descriptor', ScrollText),
          item('maintenance', 'Maintenance', '/admin/monitoring/gc', HardDrive, 'admin-sight'),
          item('backups', 'Backups', '/admin/monitoring/backup', HardDrive, 'admin-sight'),
          gap('retention-policies', 'Retention Policies', ScrollText),
          gap('user-plugins', 'User Plugins', Webhook),
        ],
      }),
    ],
  },
]

/** Terminal group: preserve BinFlow-only capabilities without perturbing reference order. */
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
  const matches = (entry: NavItem): boolean =>
    entry.label.toLowerCase().includes(q) || (entry.children ?? []).some(matches)
  const retain = (entry: NavItem): NavItem => {
    if (entry.label.toLowerCase().includes(q) || (entry.children ?? []).length === 0) return entry
    return { ...entry, children: (entry.children ?? []).filter(matches) }
  }
  return groups
    .map((group) => ({ ...group, items: group.items.filter(matches).map(retain) }))
    .filter((group) => group.items.length > 0)
}
