// 侧栏（新壳——architecture §4 四分组 IA）。类钩与锚纪律：
// - app-nav 锚 / .nav-item / .nav-group-label / .app-nav-brand /
//   .app-nav-footer 类钩原样保留（spec 类钩纪律 §3.8；e2e 以
//   `a.nav-item:text-is(…)` 导航——NavLink 字符串形态自动追加 active）。
// - 侧栏身份 = 恒深底（--bf-sidebar 系 token 的 Tailwind 桥接语义类）。
// - 脚注：语言切换器（nav-locale 族锚）+ About 版本行（nav-about /
//   nav-version——点击开 About 弹窗，Topbar 承载）。
// - admin-filter（顶栏管理资源过滤）的空匹配注记 admin-filter-empty
//   驻本栏（过滤的是侧栏条目——语义与旧壳一致）。
import { NavLink } from 'react-router-dom'

import { BrandMark } from '@/components/BrandLogo'
import { cn } from '@/lib/utils'
import { getLocale, setLocale, tr } from '@/i18n'
import type { Locale } from '@/i18n'

import type { NavGroup } from './nav-model'

const t = tr('console')

// 语言名走母语名（endonym）：切换器上看到目标语言的自称（旧壳同款豁免）
const LOCALE_LABEL_ZH = '中文' // i18n-allow

export function Sidebar({
  groups,
  version,
  onAbout,
  adminFilter = '',
}: {
  groups: NavGroup[]
  version: string | null
  onAbout: () => void
  /** 管理资源过滤词（顶栏 admin-filter 的空匹配注记用） */
  adminFilter?: string
}) {
  const locale = getLocale()
  return (
    <nav
      className="app-nav"
      aria-label={t('主导航')}
      data-testid="app-nav"
      style={{ display: 'flex', flexDirection: 'column', minHeight: '100%' }}
    >
      <div className="app-nav-brand border-b border-sidebar-border flex items-center gap-2 px-4 py-4">
        <BrandMark size={24} testid="brand-sidebar-mark" />
        <span className="name text-[15px] font-semibold text-sidebar-foreground">BinFlow</span>
      </div>
      <div className="app-nav-items flex-1 px-1 py-2">
        {groups.map((group, gi) => (
          <div key={group.id}>
            <div
              className={cn(
                'nav-group-label pt-3 pb-1 px-3 text-[11px] font-medium uppercase tracking-wider text-sidebar-muted-foreground',
                gi === 0 && 'first pt-1',
              )}
            >
              {group.label}
            </div>
            {group.items.map((entry) => (
              <NavLink
                key={entry.to}
                to={entry.to}
                end={entry.end}
                className="nav-item flex items-center gap-2 rounded-sm border-l-2 border-transparent px-3 py-1.5 text-dense text-sidebar-foreground hover:bg-sidebar-hover [&.active]:border-l-primary [&.active]:bg-sidebar-active"
                title={entry.label}
              >
                <entry.icon className="nav-icon size-4 shrink-0" aria-hidden="true" data-testid="nav-icon" data-icon={entry.id} />
                {entry.label}
              </NavLink>
            ))}
          </div>
        ))}
        {groups.length === 0 && adminFilter.trim() !== '' && (
          <p data-testid="admin-filter-empty" className="px-3 py-2 text-aux text-sidebar-muted-foreground">
            {t('「{v1}」无匹配管理资源', { v1: adminFilter.trim() })}
          </p>
        )}
      </div>
      <div className="app-nav-footer mt-auto flex flex-col gap-2 border-t border-sidebar-border p-2">
        <div className="flex items-center gap-2">
          <span className="text-aux text-sidebar-muted-foreground">{t('语言')}</span>
          <div
            role="group"
            aria-label={t('语言')}
            data-testid="nav-locale"
            className="ml-auto flex overflow-hidden rounded-sm border border-sidebar-border"
          >
            <button
              type="button"
              data-testid="nav-locale-zh"
              aria-pressed={locale === 'zh'}
              className={cn(
                'px-2.5 py-0.5 text-aux',
                locale === 'zh' ? 'bg-sidebar-active text-sidebar-foreground' : 'text-sidebar-foreground/80',
              )}
              onClick={() => setLocale('zh' as Locale)}
            >
              {LOCALE_LABEL_ZH}
            </button>
            <button
              type="button"
              data-testid="nav-locale-en"
              aria-pressed={locale === 'en'}
              className={cn(
                'px-2.5 py-0.5 text-aux',
                locale === 'en' ? 'bg-sidebar-active text-sidebar-foreground' : 'text-sidebar-foreground/80',
              )}
              onClick={() => setLocale('en' as Locale)}
            >
              English
            </button>
          </div>
        </div>
        <button
          type="button"
          className="app-nav-license nav-item flex items-center rounded-sm px-2 py-1 text-left text-aux text-sidebar-muted-foreground hover:bg-sidebar-hover"
          data-testid="nav-about"
          title={t('关于 BinFlow（版本 / 构建信息）')}
          onClick={onAbout}
        >
          <span>
            BinFlow <span data-testid="nav-version" lang="en">{version ? `v${version}` : '—'}</span> {t('· 单二进制制品仓库')}
          </span>
        </button>
      </div>
    </nav>
  )
}
