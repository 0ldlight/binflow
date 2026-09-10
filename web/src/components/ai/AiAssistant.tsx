// AI 助手抽屉（lazy 分片入口——AppShell 首开后常驻挂载，关闭仅隐藏）。
// 右滑 Drawer = Radix Dialog 侧板（ai-drawer 锚——总令 §十五 形态；
// Esc/焦点回归交 Radix FocusScope/DismissableLayer）。
//
// 会话态存活设计：useLocalRuntime 挂本组件顶层（Dialog Content 卸载不随
// 葬——开合往返消息不丢；SPA 刷新才重置，architecture §6「仅 UI 态」纪律）。
//
// 体积纪律（architecture §1 预算分解）：本文件族（assistant-ui runtime +
// react-markdown + 卡片族）整体落独立懒分片——主壳零增量，AI 分片按需
// 加载（首开后缓存）。
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { AssistantRuntimeProvider, useLocalRuntime } from '@assistant-ui/react'

import { ChatDrawerBody } from './ChatPanel'
import { AI_TOOL_CREATE_REPO, mockChatAdapter } from '@/lib/ai/provider'
import { useAiStore } from '@/stores/ai-store'
import { tr } from '@/i18n'

const t = tr('ai')

/**
 * 右滑 AI Drawer。开合态 = ai-store.drawerOpen（palette/顶栏钮/⌘J 三入口
 * 同源）；onOpenChange(false) = Esc/外点/关闭钮 → closeDrawer。
 */
export default function AiAssistant() {
  const open = useAiStore((s) => s.drawerOpen)
  const closeDrawer = useAiStore((s) => s.closeDrawer)
  // mock provider + human-in-the-loop 工具名（create_repository 的
  // ConfirmCard 确认路径——addToolResult 后 runtime 重入 adapter）
  const runtime = useLocalRuntime(mockChatAdapter, {
    unstable_humanToolNames: [AI_TOOL_CREATE_REPO],
  })

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <DialogPrimitive.Root open={open} onOpenChange={(o) => { if (!o) closeDrawer() }}>
        <DialogPrimitive.Portal>
          <DialogPrimitive.Overlay data-slot="ai-overlay" className="fixed inset-0 z-[90] bg-scrim" />
          <DialogPrimitive.Content
            data-testid="ai-drawer"
            aria-labelledby="ai-drawer-title"
            className="fixed inset-y-0 right-0 z-[90] flex w-[min(520px,calc(100vw-48px))] flex-col border-l border-border bg-surface-1 text-foreground shadow-modal focus:outline-none"
          >
            <DialogPrimitive.Title id="ai-drawer-title" className="sr-only">
              {t('AI 助手')}
            </DialogPrimitive.Title>
            <DialogPrimitive.Description className="sr-only">
              {t('上下文感知 Copilot（本地演示——零网络）')}
            </DialogPrimitive.Description>
            <ChatDrawerBody runtime={runtime} />
          </DialogPrimitive.Content>
        </DialogPrimitive.Portal>
      </DialogPrimitive.Root>
    </AssistantRuntimeProvider>
  )
}
