// Artifactory 7.161 breadcrumb and application-title model.
// Labels follow the captured runtime shell, while routes remain BinFlow-stable
// in this compatibility batch (see docs/reverse/frontend/nav-parity.yaml).
export interface Crumb {
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

export function adminCrumbs(pathname: string): Crumb[] {
  const root: Crumb = { label: 'All Projects', to: '/dashboard' }

  if (pathname.startsWith('/admin/repositories')) {
    const rest = pathname.slice('/admin/repositories'.length).split('/').filter(Boolean)
    const crumbs: Crumb[] = [root, { label: 'Repositories', to: '/admin/repositories/local' }]
    if (rest.length === 0 || ['local', 'remote', 'virtual'].includes(rest[0] ?? '')) return crumbs
    if (rest[0] === 'new') return [...crumbs, { label: 'Create a Repository' }]
    crumbs.push({ label: safeDecode(rest[0]) })
    if (rest[1] === 'edit') crumbs.push({ label: 'Edit' })
    return crumbs
  }

  if (pathname.startsWith('/admin/security/')) {
    const rest = pathname.slice('/admin/security/'.length).split('/')
    const section: Record<string, { parent: string; label: string }> = {
      users: { parent: 'User Management', label: 'Users' },
      groups: { parent: 'User Management', label: 'Groups' },
      permissions: { parent: 'User Management', label: 'Permissions' },
      tokens: { parent: 'User Management', label: 'Access Tokens' },
      keypair: { parent: 'Security', label: 'Signing Keys' },
      auth: { parent: 'Authentication', label: 'Authentication' },
    }
    const meta = section[rest[0] ?? ''] ?? { parent: 'Security', label: safeDecode(rest[0] ?? '') }
    const crumbs: Crumb[] = [
      root,
      { label: meta.parent, to: `/admin/security/${rest[0] === 'keypair' ? 'keypair' : rest[0] === 'auth' ? 'auth/ldap' : 'users'}` },
      { label: meta.label, to: `/admin/security/${rest[0]}` },
    ]
    const proto: Record<string, string> = { ldap: 'LDAP', oauth: 'OAuth SSO', saml: 'SAML SSO' }
    if (rest[0] === 'auth' && proto[rest[1] ?? '']) crumbs.push({ label: proto[rest[1] ?? ''] })
    else if (rest[1] === 'new') crumbs.push({ label: 'New' })
    else if (rest[1]) crumbs.push({ label: safeDecode(rest[1]) })
    return crumbs
  }

  if (pathname.startsWith('/admin/governance/')) {
    const map: Record<string, string> = {
      audit: 'Audit Log',
      quotas: 'Quotas',
      replication: 'Replication',
      trash: 'Trash',
    }
    const seg = pathname.slice('/admin/governance/'.length).split('/')[0]
    return [root, { label: 'BinFlow Extensions' }, { label: map[seg] ?? safeDecode(seg) }]
  }

  if (pathname.startsWith('/admin/monitoring/')) {
    const map: Record<string, string> = {
      storage: 'Storage',
      status: 'Service Status',
      logs: 'System Logs',
      'system-info': 'System Info',
      settings: 'Settings',
      gc: 'Maintenance',
      backup: 'Backups',
    }
    const seg = pathname.slice('/admin/monitoring/'.length).split('/')[0]
    const label = map[seg] ?? safeDecode(seg)
    const parent = seg === 'gc' || seg === 'backup' ? 'Artifactory Settings' : 'Monitoring'
    return [root, { label: parent, to: seg === 'gc' || seg === 'backup' ? '/admin/monitoring/settings' : '/admin/monitoring/storage' }, { label }]
  }

  if (pathname.startsWith('/admin/general/')) {
    const map: Record<string, string> = { webhooks: 'Webhooks', license: 'License & Add-ons' }
    const seg = pathname.slice('/admin/general/'.length).split('/')[0]
    return [root, { label: 'BinFlow Extensions' }, { label: map[seg] ?? safeDecode(seg) }]
  }

  return [root, { label: 'Administration' }]
}

export function appTitle(pathname: string): string {
  if (pathname.startsWith('/packages')) return 'Packages'
  if (pathname.startsWith('/artifacts')) return 'Artifacts'
  if (pathname.startsWith('/dashboard')) return 'All Projects Overview'
  if (pathname.startsWith('/search')) return 'Search Artifacts'
  if (pathname.startsWith('/profile')) return 'User Profile'
  if (pathname.startsWith('/builds')) return 'Builds'
  if (pathname.startsWith('/bundles') || pathname.startsWith('/artifactory/release-')) return 'Release Lifecycle'
  return 'BinFlow'
}
