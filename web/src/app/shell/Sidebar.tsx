// 侧栏骨架（可折叠，四分组 + badge + 权限可见性——architecture §4）。
// P1 仅结构不消费：不接 router/不接权限快照（P2 随新壳接线）。
import { PanelLeftClose, PanelLeftOpen } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { cn } from '@/lib/utils'

import { NAV_GROUPS } from './nav-model'

export function Sidebar({ collapsed = false }: { collapsed?: boolean }) {
  return (
    <aside
      data-slot="sidebar"
      data-collapsed={collapsed}
      className={cn(
        'flex h-full flex-col bg-sidebar text-sidebar-foreground transition-[width]',
        collapsed ? 'w-14' : 'w-52',
      )}
    >
      <nav aria-label="Main" className="flex-1 overflow-y-auto px-2 py-2">
        {NAV_GROUPS.map((group) => (
          <div key={group.id} data-slot="sidebar-group" className="mb-2">
            {!collapsed && (
              <div className="px-2 pt-2 pb-1 text-aux font-medium text-sidebar-muted-foreground">{group.label}</div>
            )}
            <ul className="space-y-0.5">
              {group.items.map((item) => (
                <li key={item.id}>
                  <a
                    href={item.to}
                    data-slot="sidebar-item"
                    className={cn(
                      'flex items-center gap-2 rounded-sm px-2 py-1.5 text-dense text-sidebar-foreground hover:bg-sidebar-hover',
                      collapsed && 'justify-center px-0',
                    )}
                  >
                    <item.icon className="size-4 shrink-0" />
                    {!collapsed && <span className="truncate">{item.id}</span>}
                  </a>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </nav>
      <Separator className="bg-sidebar-border" />
      <div className="flex items-center justify-end p-1.5">
        <Button variant="ghost" size="icon" className="text-sidebar-foreground hover:bg-sidebar-hover" aria-label="Toggle sidebar">
          {collapsed ? <PanelLeftOpen /> : <PanelLeftClose />}
        </Button>
      </div>
    </aside>
  )
}
