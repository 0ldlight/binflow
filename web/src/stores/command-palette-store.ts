// 命令面板仓（⌘K palette 开闭 + 上下文注入——当前路由/制品域提示，
// Phase 4 消费面；P1 骨架）。
import { create } from 'zustand'

/** palette 上下文注入（当前页面域——动作集过滤依据） */
export interface PaletteContext {
  route?: string
  /** 选中实体提示（如制品路径——「复制路径」类动作的参数源） */
  artifactPath?: string
}

export interface CommandPaletteState {
  open: boolean
  context: PaletteContext
  openPalette: (context?: PaletteContext) => void
  closePalette: () => void
  setContext: (context: PaletteContext) => void
}

export const useCommandPaletteStore = create<CommandPaletteState>()((set) => ({
  open: false,
  context: {},
  openPalette: (context) => set({ open: true, ...(context ? { context } : {}) }),
  closePalette: () => set({ open: false }),
  setContext: (context) => set({ context }),
}))
