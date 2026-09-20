import { Link, NavLink } from 'react-router-dom'

import { BrandLockup } from '@/components/BrandLogo'
import { Button, ButtonAsChild } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { getLocale, setLocale, tr } from '@/i18n'
import type { Locale } from '@/i18n'

import type { NavGroup, NavItem } from './nav-model'

const t = tr('console')
const LOCALE_LABEL_ZH = '中文' // i18n-allow

const navLinkClass =
  'nav-item flex h-8 items-center gap-2.5 rounded-sm border-l-2 border-transparent px-2.5 text-dense text-sidebar-foreground/80 transition-colors hover:bg-sidebar-hover hover:text-sidebar-foreground [&.active]:border-l-primary [&.active]:bg-sidebar-active [&.active]:font-medium [&.active]:text-sidebar-foreground'

function NavEntry({ entry }: { entry: NavItem }) {
  return (
    <NavLink
      to={entry.to}
      end={entry.end}
      className={navLinkClass}
      title={entry.label}
      data-testid={`nav-entry-${entry.id}`}
    >
      <entry.icon className="nav-icon size-4 shrink-0" aria-hidden="true" data-testid="nav-icon" data-icon={entry.id} />
      <span className="min-w-0 flex-1 truncate">{entry.label}</span>
    </NavLink>
  )
}

export function Sidebar({
  groups,
  mode,
  canSeeAdmin,
  version,
  onAbout,
  adminFilter = '',
}: {
  groups: NavGroup[]
  mode: 'platform' | 'administration'
  canSeeAdmin: boolean
  version: string | null
  onAbout: () => void
  adminFilter?: string
}) {
  const locale = getLocale()
  const platformActive = mode === 'platform'

  return (
    <nav
      className="app-nav"
      aria-label={t('主导航')}
      data-testid="app-nav"
      data-mode={mode}
      style={{ display: 'flex', flexDirection: 'column', minHeight: '100%' }}
    >
      <div className="app-nav-brand border-b border-sidebar-border flex h-topbar items-center gap-2 px-4">
        {/* Historical anchor: brand-sidebar-mark. The supplied modular kit
            puts the navigation wordmark in the horizontal lockup itself. */}
        <BrandLockup height={42} testid="brand-sidebar-mark" />
      </div>

      <div className="flex gap-1 border-b border-sidebar-border p-2" role="group" aria-label={t('平台模式')}>
        <ButtonAsChild
          variant={platformActive ? 'secondary' : 'ghost'}
          size="sm"
          className="flex-1"
          data-testid="nav-mode-platform"
          aria-current={platformActive ? 'page' : undefined}
        >
          <Link to="/packages">{t('平台')}</Link>
        </ButtonAsChild>
        {canSeeAdmin ? (
          <ButtonAsChild
            variant={!platformActive ? 'secondary' : 'ghost'}
            size="sm"
            className="flex-1"
            data-testid="nav-mode-administration"
            aria-current={!platformActive ? 'page' : undefined}
          >
            <Link to="/admin/repositories/local">{t('管理')}</Link>
          </ButtonAsChild>
        ) : (
          <Button
            type="button"
            disabled
            variant="ghost"
            size="sm"
            className="flex-1"
            data-testid="nav-mode-administration"
            aria-current={!platformActive ? 'page' : undefined}
          >
            {t('管理')}
          </Button>
        )}
      </div>

      <div className="app-nav-items flex-1 px-1 py-2">
        {groups.map((group, gi) => (
          <div key={group.id}>
            <div
              className={cn(
                'nav-group-label pt-4 pb-2 px-3 text-[11px] font-semibold uppercase tracking-wider text-sidebar-muted-foreground',
                gi === 0 && 'first pt-2',
              )}
            >
              {group.label}
            </div>
            {group.items.map((entry) => (
              <NavEntry key={entry.id} entry={entry} />
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
            <Button
              type="button"
              variant="ghost"
              data-testid="nav-locale-zh"
              aria-pressed={locale === 'zh'}
              className={cn(
                'px-2.5 py-0.5 text-aux',
                locale === 'zh' ? 'bg-sidebar-active text-sidebar-foreground' : '!text-sidebar-foreground',
              )}
              onClick={() => setLocale('zh' as Locale)}
            >
              {LOCALE_LABEL_ZH}
            </Button>
            <Button
              type="button"
              variant="ghost"
              data-testid="nav-locale-en"
              aria-pressed={locale === 'en'}
              className={cn(
                'px-2.5 py-0.5 text-aux',
                locale === 'en' ? 'bg-sidebar-active text-sidebar-foreground' : '!text-sidebar-foreground',
              )}
              onClick={() => setLocale('en' as Locale)}
            >
              English
            </Button>
          </div>
        </div>
        <Button
          type="button"
          variant="ghost"
          className="app-nav-license nav-item flex items-center rounded-sm px-2 py-1 text-left text-aux !text-sidebar-foreground hover:bg-sidebar-hover"
          data-testid="nav-about"
          title={t('关于 BinFlow（版本 / 构建信息）')}
          onClick={onAbout}
        >
          <span>
            BinFlow <span data-testid="nav-version" lang="en">{version ? `v${version}` : '—'}</span> {t('· 单二进制制品仓库')}
          </span>
        </Button>
      </div>
    </nav>
  )
}
