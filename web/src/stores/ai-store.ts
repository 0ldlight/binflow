// AI 仓（Phase 5 消费面：assistant drawer 开合 + 上下文坐标 + 发送时快照；
// frontend-rewrite-architecture §6 Zustand 五仓之一——仅 UI 态，会话消息
// 由 assistant-ui LocalRuntime 在挂载组件内自持，不入仓）。
//
// 上下文模型 = lib/ai/context.ts 的 AiContext（路由派生坐标）。live 态随
// 路由更新（ChatPanel 内 effect 同步）；contextSnapshot 在用户发送消息
// 时落快照（消息首帧哨兵注入源——「当前在 docker-prod/nginx/1.27」形态
// 徽章与 mock provider 的上下文感知样板同源）。
//
// 不虚构端点（audit 盲区② 裁定）：本仓不持有任何网络面状态。
import { create } from 'zustand'

import type { AiContext } from '@/lib/ai/context'

export interface AiState {
  drawerOpen: boolean
  /** 首开后常驻（懒分片挂载闩——关闭仅隐藏，会话态在挂载组件内存活） */
  mounted: boolean
  /** 活上下文（随路由更新——ChatPanel 头部徽章源；null = 未派生） */
  context: AiContext | null
  /** 最近一次发送时的上下文快照（消息首帧注入源——发送瞬间坐标） */
  contextSnapshot: AiContext | null
  openDrawer: (context?: AiContext) => void
  closeDrawer: () => void
  setContext: (context: AiContext) => void
  setContextSnapshot: (context: AiContext) => void
}

export const useAiStore = create<AiState>()((set) => ({
  drawerOpen: false,
  mounted: false,
  context: null,
  contextSnapshot: null,
  openDrawer: (context) => set({ drawerOpen: true, mounted: true, ...(context ? { context } : {}) }),
  closeDrawer: () => set({ drawerOpen: false }),
  setContext: (context) => set({ context }),
  setContextSnapshot: (context) => set({ contextSnapshot: context }),
}))
