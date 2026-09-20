// BinFlow console breadcrumb and application-title model. Routes stay stable
// while labels render through the console i18n domain; decoded repository and
// user identifiers intentionally retain their original casing.
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
  const root: Crumb = { label: t('所有项目'), to: '/dashboard' }

  if (pathname.startsWith('/admin/repositories')) {
    const rest = pathname.slice('/admin/repositories'.length).split('/').filter(Boolean)
    const crumbs: Crumb[] = [root, { label: t('仓库'), to: '/admin/repositories/local' }]
    if (rest.length === 0 || ['local', 'remote', 'virtual'].includes(rest[0] ?? '')) return crumbs
    if (rest[0] === 'new') return [...crumbs, { label: t('新建仓库') }]
    crumbs.push({ label: safeDecode(rest[0]) })
    if (rest[1] === 'edit') crumbs.push({ label: t('编辑') })
    return crumbs
  }

  if (pathname.startsWith('/admin/security/')) {
    const rest = pathname.slice('/admin/security/'.length).split('/')
    const section: Record<string, { parent: string; label: string }> = {
      users: { parent: t('用户管理'), label: t('用户') },
      groups: { parent: t('用户管理'), label: t('组') },
      permissions: { parent: t('用户管理'), label: t('权限') },
      tokens: { parent: t('用户管理'), label: t('访问令牌') },
      keypair: { parent: t('安全'), label: t('签名密钥') },
      auth: { parent: t('认证'), label: t('认证') },
    }
    const meta = section[rest[0] ?? ''] ?? { parent: t('安全'), label: safeDecode(rest[0] ?? '') }
    const crumbs: Crumb[] = [
      root,
      { label: meta.parent, to: `/admin/security/${rest[0] === 'keypair' ? 'keypair' : rest[0] === 'auth' ? 'auth/ldap' : 'users'}` },
      { label: meta.label, to: `/admin/security/${rest[0]}` },
    ]
    const proto: Record<string, string> = { ldap: 'LDAP', oauth: 'OAuth SSO', saml: 'SAML SSO' }
    if (rest[0] === 'auth' && proto[rest[1] ?? '']) crumbs.push({ label: proto[rest[1] ?? ''] })
    else if (rest[1] === 'new') crumbs.push({ label: t('新建') })
    else if (rest[1]) crumbs.push({ label: safeDecode(rest[1]) })
    return crumbs
  }

  if (pathname.startsWith('/admin/governance/')) {
    const map: Record<string, string> = {
      audit: t('审计日志'),
      quotas: t('配额'),
      replication: t('复制'),
      trash: t('回收站'),
    }
    const seg = pathname.slice('/admin/governance/'.length).split('/')[0]
    return [root, { label: t('BinFlow 扩展') }, { label: map[seg] ?? safeDecode(seg) }]
  }

  if (pathname.startsWith('/admin/monitoring/')) {
    const map: Record<string, string> = {
      storage: t('存储'),
      status: t('服务状态'),
      logs: t('系统日志'),
      'system-info': t('系统信息'),
      settings: t('设置'),
      gc: t('维护'),
      backup: t('备份'),
    }
    const seg = pathname.slice('/admin/monitoring/'.length).split('/')[0]
    const label = map[seg] ?? safeDecode(seg)
    const parent = seg === 'gc' || seg === 'backup' ? t('仓库设置') : t('监控')
    return [root, { label: parent, to: seg === 'gc' || seg === 'backup' ? '/admin/monitoring/settings' : '/admin/monitoring/storage' }, { label }]
  }

  if (pathname.startsWith('/admin/general/')) {
    const map: Record<string, string> = { webhooks: t('Webhooks'), license: t('许可与扩展') }
    const seg = pathname.slice('/admin/general/'.length).split('/')[0]
    return [root, { label: t('BinFlow 扩展') }, { label: map[seg] ?? safeDecode(seg) }]
  }

  return [root, { label: t('管理') }]
}

export function appTitle(pathname: string): string {
  if (pathname.startsWith('/packages')) return t('软件包')
  if (pathname.startsWith('/artifacts')) return t('制品')
  if (pathname.startsWith('/dashboard')) return t('所有项目概览')
  if (pathname.startsWith('/search')) return t('搜索制品')
  if (pathname.startsWith('/profile')) return t('用户档案')
  if (pathname.startsWith('/builds')) return t('构建')
  if (pathname.startsWith('/bundles') || pathname.startsWith('/release-')) return t('发布生命周期')
  return 'BinFlow'
}
