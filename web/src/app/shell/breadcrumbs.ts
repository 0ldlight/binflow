// 面包屑模型（管理域层级表达——§1.3）：逻辑平移自旧 AppShell.adminCrumbs。
// 应用域无层级（顶栏显页面标题），见 appTitle。
import { tr } from '@/i18n'

const t = tr('console')

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
  if (pathname.startsWith('/admin/repositories')) {
    const rest = pathname
      .slice('/admin/repositories'.length)
      .split('/')
      .filter((s) => s !== '')
    const crumbs: Crumb[] = [{ label: t('仓库'), to: '/admin/repositories/local' }]
    if (rest.length === 0 || ['local', 'remote', 'virtual'].includes(rest[0])) return crumbs
    if (rest[0] === 'new') return [...crumbs, { label: t('新建仓库') }]
    crumbs.push({ label: safeDecode(rest[0]) })
    if (rest[1] === 'edit') crumbs.push({ label: t('编辑') })
    return crumbs
  }
  const sec: Record<string, string> = {
    users: t('用户'),
    groups: t('组'),
    permissions: t('权限'),
    tokens: 'Access Tokens',
    auth: t('认证配置'),
  }
  if (pathname.startsWith('/admin/security/')) {
    const rest = pathname.slice('/admin/security/'.length).split('/')
    const label = sec[rest[0]] ?? ''
    const crumbs: Crumb[] = [
      { label: t('安全'), to: '/admin/security/users' },
      { label, to: `/admin/security/${rest[0]}` },
    ]
    const proto: Record<string, string> = { ldap: 'LDAP', oauth: 'OAuth (OIDC)', saml: 'SAML SSO' }
    if (rest[0] === 'auth' && proto[rest[1]]) crumbs.push({ label: proto[rest[1]] })
    else if (rest[1] && rest[1] !== 'new') crumbs.push({ label: safeDecode(rest[1]) })
    else if (rest[1] === 'new') crumbs.push({ label: t('新建') })
    return crumbs
  }
  const gov: Record<string, string> = {
    audit: t('审计日志'),
    quotas: t('配额'),
    replication: t('复制'),
    trash: t('回收站'),
  }
  if (pathname.startsWith('/admin/governance/')) {
    const seg = pathname.slice('/admin/governance/'.length)
    return [{ label: t('管理'), to: '/admin/monitoring/storage' }, { label: gov[seg] ?? seg }]
  }
  if (pathname.startsWith('/admin/monitoring/')) {
    const mon: Record<string, string> = {
      storage: t('存储'),
      status: t('服务状态'),
      logs: t('系统日志'),
      'system-info': t('系统信息'),
      gc: t('维护（GC）'),
      backup: t('备份 / 恢复'),
    }
    const seg = pathname.slice('/admin/monitoring/'.length).split('/')[0]
    return [{ label: t('管理'), to: '/admin/monitoring/storage' }, { label: mon[seg] ?? seg }]
  }
  if (pathname.startsWith('/admin/general/')) {
    const gen: Record<string, string> = {
      webhooks: 'Webhooks',
      license: 'License & Add-ons',
    }
    const seg = pathname.slice('/admin/general/'.length).split('/')[0]
    return [{ label: t('管理'), to: '/admin/monitoring/storage' }, { label: gen[seg] ?? seg }]
  }
  return [{ label: t('管理') }]
}

/** 应用域页面标题（无层级，直接页面名） */
export function appTitle(pathname: string): string {
  if (pathname.startsWith('/artifacts')) return t('制品')
  if (pathname.startsWith('/dashboard')) return t('仪表盘')
  if (pathname.startsWith('/search')) return t('搜索制品')
  if (pathname.startsWith('/profile')) return t('编辑档案')
  if (pathname.startsWith('/builds')) return 'Builds'
  if (pathname.startsWith('/bundles')) return 'Release Bundles'
  return 'BinFlow'
}
