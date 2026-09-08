// UI 仓（frontend-rewrite-architecture §6：仅 UI 态，服务数据入仓=违规）
// ——侧栏折叠/主题面板态等壳层状态。
import { create } from 'zustand'

export interface UiState {
  /** 侧栏折叠（持久化归 preferences-store 的 localStorage 面，此处仅态） */
  sidebarCollapsed: boolean
  toggleSidebar: () => void
  setSidebarCollapsed: (collapsed: boolean) => void
}

export const useUiStore = create<UiState>()((set) => ({
  sidebarCollapsed: false,
  toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
  setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
}))
