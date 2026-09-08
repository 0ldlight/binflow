// 应用壳骨架（可折叠侧栏 + 顶栏 + 主区——architecture §4）。P1 仅结构
// 不消费：P2 随新路由表挂载（认证守卫/面包屑/quick 菜单届时接线）。
import { Outlet } from 'react-router-dom'

import { Topbar } from './Topbar'
import { Sidebar } from './Sidebar'

export function AppShell({ sidebarCollapsed = false }: { sidebarCollapsed?: boolean }) {
  return (
    <div data-slot="app-shell" className="flex h-screen w-full overflow-hidden bg-background text-foreground">
      <Sidebar collapsed={sidebarCollapsed} />
      <div className="flex min-w-0 flex-1 flex-col">
        <Topbar />
        <main data-slot="app-main" className="min-h-0 flex-1 overflow-y-auto p-4">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
