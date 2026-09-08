// AI 仓（Phase 5 消费面：assistant drawer 开闭/上下文——route/artifact
// 感知注入；P1 骨架，不虚构端点（audit 盲区②裁定））。
import { create } from 'zustand'

/** AI 面上下文（当前路由/制品——提示词上下文注入源） */
export interface AiContext {
  route?: string
  artifactPath?: string
}

export interface AiState {
  drawerOpen: boolean
  context: AiContext
  openDrawer: (context?: AiContext) => void
  closeDrawer: () => void
  setContext: (context: AiContext) => void
}

export const useAiStore = create<AiState>()((set) => ({
  drawerOpen: false,
  context: {},
  openDrawer: (context) => set({ drawerOpen: true, ...(context ? { context } : {}) }),
  closeDrawer: () => set({ drawerOpen: false }),
  setContext: (context) => set({ context }),
}))
