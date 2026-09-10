// 命令面板（⌘/Ctrl+K——frontend-rewrite-architecture §8 / FE-P4 A1）：
// shadcn Command（cmdk）承载的全局面板——导航（四分组全条目，权限门控
// 可见性与侧栏同源 nav-model）/ 动作（建仓三预选 · 上传 · 新建用户/组/
// 权限）/ 偏好（主题切换 · 语言切换）/ AI 入口（FE-P5 接线：开 AI drawer
// ——本地 mock 零端点，audit 盲区② 边界）。管理资源过滤语义并入：/admin 域侧栏条目全量进
// 导航组，子串过滤即 cmdk 自身的模糊匹配——Topbar 管理过滤框原样保留
// （既有键位与 e2e 面零回退）。
//
// 开闭态 = stores/command-palette-store（Zustand UI 五仓之一）；⌘K 全局
// 监听在本组件（Topbar 的旧 ⌘K→聚焦搜索让位——`/` 聚焦语义不动）。键盘
// 全集交 cmdk：↑↓ 选择 / Enter 激活 / Esc 关闭（Dialog onOpenChange）。
// 上下文注入（palette context.route/artifactPath）已接 store——P5 AI 面
// 的参数源，本相位消费面为导航默认值。
//
// 锚族（新）：palette-root / palette-input / palette-item-<id> / palette-ai。
import { useEffect } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import * as DialogPrimitive from '@radix-ui/react-dialog'

import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command'
import { useAuth } from '@/app/AuthContext'
import { isReadOnlyAdmin } from '@/lib/api'
import { deriveAiContext } from '@/lib/ai/context'
import { getLocale, setLocale } from '@/i18n'
import { useTheme } from '@/app/providers'
import { useAiStore } from '@/stores/ai-store'
import { useCommandPaletteStore } from '@/stores/command-palette-store'
import { tr } from '@/i18n'

import { NAV_GROUPS } from './nav-model'

const t = tr('console')

export function CommandPalette() {
  const open = useCommandPaletteStore((s) => s.open)
  const closePalette = useCommandPaletteStore((s) => s.closePalette)
  const openPalette = useCommandPaletteStore((s) => s.openPalette)
  const openAiDrawer = useAiStore((s) => s.openDrawer)
  const navigate = useNavigate()
  const location = useLocation()
  const { session } = useAuth()
  const { theme, toggle } = useTheme()

  const admin = session?.admin ?? false
  const canSeeAdmin = admin || isReadOnlyAdmin(session)
  const locale = getLocale()

  // ⌘/Ctrl+K 开合（Topbar 的 `/` 聚焦搜索不动——两者并存；对话框在场时
  // Topbar 侧已让位，palette 自身的 Esc 关闭由 cmdk/Dialog 承载）
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.key === 'k' || e.key === 'K') && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        if (useCommandPaletteStore.getState().open) closePalette()
        else openPalette()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [openPalette, closePalette])

  const go = (to: string) => {
    closePalette()
    navigate(to)
  }

  const visibleGroups = NAV_GROUPS.map((g) => ({
    ...g,
    items: g.items.filter((i) => i.visibility === 'all' || canSeeAdmin),
  })).filter((g) => g.items.length > 0)

  return (
    <DialogPrimitive.Root open={open} onOpenChange={(o) => { if (!o) closePalette() }}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay data-slot="dialog-overlay" className="fixed inset-0 z-[95] bg-scrim" />
        <DialogPrimitive.Content
          data-testid="palette-root"
          aria-labelledby="palette-title"
          className="fixed top-[12vh] left-1/2 z-[95] w-[min(560px,calc(100vw-32px))] -translate-x-1/2 overflow-hidden rounded-lg border border-border bg-popover text-popover-foreground shadow-modal"
        >
          <DialogPrimitive.Title id="palette-title" className="sr-only">
            {t('命令面板')}
          </DialogPrimitive.Title>
          <Command>
            <CommandInput data-testid="palette-input" placeholder={t('搜索命令与页面…（导航 / 动作 / 偏好）')} />
            <CommandList>
              <CommandEmpty>{t('没有匹配的命令')}</CommandEmpty>

              {/* 动作组（写口仅全量 admin——与用户菜单 Quick 动作同门） */}
              {admin && !isReadOnlyAdmin(session) && (
                <CommandGroup heading={t('动作')}>
                  <CommandItem data-testid="palette-item-new-repo-local" onSelect={() => go('/admin/repositories/new?rclass=local')}>
                    {t('新建 Local 仓库')}
                  </CommandItem>
                  <CommandItem data-testid="palette-item-new-repo-remote" onSelect={() => go('/admin/repositories/new?rclass=remote')}>
                    {t('新建 Remote 仓库')}
                  </CommandItem>
                  <CommandItem data-testid="palette-item-new-repo-virtual" onSelect={() => go('/admin/repositories/new?rclass=virtual')}>
                    {t('新建 Virtual 仓库')}
                  </CommandItem>
                  <CommandItem data-testid="palette-item-upload" onSelect={() => go('/artifacts')}>
                    {t('上传制品（Deploy）')}
                  </CommandItem>
                  <CommandItem data-testid="palette-item-new-user" onSelect={() => go('/admin/security/users')}>
                    {t('新建用户')}
                  </CommandItem>
                  <CommandItem data-testid="palette-item-new-group" onSelect={() => go('/admin/security/groups')}>
                    {t('新建组')}
                  </CommandItem>
                  <CommandItem data-testid="palette-item-new-perm" onSelect={() => go('/admin/security/permissions/new')}>
                    {t('新建权限')}
                  </CommandItem>
                </CommandGroup>
              )}

              {/* 导航四分组（侧栏同源——管理资源过滤语义并入：/admin 条目全量在此） */}
              {visibleGroups.map((g) => (
                <CommandGroup key={g.id} heading={g.label}>
                  {g.items.map((item) => (
                    <CommandItem
                      key={item.id}
                      data-testid={`palette-item-nav-${item.id}`}
                      value={`nav-${item.id} ${item.label}`}
                      onSelect={() => go(item.to)}
                    >
                      <item.icon aria-hidden="true" />
                      <span>{item.label}</span>
                      <span className="ml-auto font-mono text-aux text-muted-foreground" aria-hidden="true" lang="en">
                        {item.to}
                      </span>
                    </CommandItem>
                  ))}
                </CommandGroup>
              ))}

              {/* 偏好：主题 / 语言 */}
              <CommandGroup heading={t('偏好')}>
                <CommandItem data-testid="palette-item-theme" onSelect={() => { toggle(); closePalette() }}>
                  {theme === 'dark' ? t('切换亮色主题') : t('切换暗色主题')}
                </CommandItem>
                <CommandItem
                  data-testid="palette-item-locale"
                  onSelect={() => {
                    // i18n 切换 = 持久化 + 整页 reload（内核契约）
                    setLocale(locale === 'zh' ? 'en' : 'zh')
                  }}
                >
                  {locale === 'zh' ? t('Switch to English') : t('切换到中文')}
                </CommandItem>
              </CommandGroup>

              {/* AI 入口（FE-P5 接线：开 ai-store drawer——本地 mock，零后端虚构） */}
              <CommandGroup heading="AI">
                <CommandItem
                  data-testid="palette-ai"
                  onSelect={() => {
                    closePalette()
                    openAiDrawer(deriveAiContext(location.pathname, location.search))
                  }}
                >
                  {t('AI 助手')}
                  <span className="ml-auto font-mono text-aux text-muted-foreground" aria-hidden="true" lang="en">
                    ⌘J
                  </span>
                </CommandItem>
              </CommandGroup>
            </CommandList>
          </Command>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}
