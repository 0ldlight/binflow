// AI ChatPanel（总令 §十五 Context-aware Copilot 的壳实现——P5）：
// 头部（上下文徽章——当前路由派生坐标）/ 消息流（markdown·code·表格·
// ToolCallCard·ToolResultCard·ConfirmCard 分层渲染）/ 输入框（Enter 发送、
// Shift+Enter 换行）。
//
// 状态机 = assistant-ui LocalRuntime（useLocalRuntime + mockChatAdapter——
// lib/ai/provider.ts，零网络组件契约）；消息渲染走 ThreadPrimitive.Messages
// children 形（每条消息裹 MessageByIndexProvider——MessagePrimitive.Parts
// 可用）。上下文注入：发送时把 ai-store 活上下文以哨兵行注入消息首帧
// （「当前在 docker-prod/nginx/1.27」形态徽章 = 用户气泡首帧行解析）。
//
// 四态：缺省（欢迎+建议 chip）/ 加载（运行中 typing 指示）/ 空（线程空态）
// / 错误（message incomplete+error → 错误卡+重试 startRun）。会话态本地
// 保持（drawer 关闭仅隐藏，卸载只随 SPA——architecture §6「仅 UI 态」纪律）。
import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react'
import {
  MessagePrimitive,
  ThreadPrimitive,
} from '@assistant-ui/react'
import type { AssistantRuntime, MessageState, TextMessagePartProps } from '@assistant-ui/react'
import * as DialogPrimitive from '@radix-ui/react-dialog'

import { deriveAiContext } from '@/lib/ai/context'
import type { AiContext } from '@/lib/ai/context'
import { AI_CONTEXT_MARK, AI_DOMAIN_WORD, AI_TOOL_CREATE_REPO, AI_TOOL_STORAGE, parseContextSentinel } from '@/lib/ai/provider'
import { MarkdownText } from './MarkdownText'
import { ConfirmToolPart, StorageToolPart, ToolFallbackPart } from './ToolCards'
import { useAiStore } from '@/stores/ai-store'
import { cn } from '@/lib/utils'
import { tr } from '@/i18n'
import { useLocation } from 'react-router-dom'

const t = tr('ai')

function coordOf(ctx: AiContext | null): string | null {
  if (!ctx) return null
  if (ctx.repoKey) return ctx.path ? `${ctx.repoKey}/${ctx.path}` : ctx.repoKey
  if (ctx.buildName) return ctx.buildNumber ? `${ctx.buildName}/${ctx.buildNumber}` : ctx.buildName
  if (ctx.bundleName) return ctx.bundleVersion ? `${ctx.bundleName}/${ctx.bundleVersion}` : ctx.bundleName
  if (ctx.query) return ctx.query
  return null
}

/** LocalRuntime 状态订阅（subscribe/getState = 文档化状态机面） */
function useThreadRunning(runtime: AssistantRuntime): boolean {
  return useSyncExternalStore(
    (cb) => runtime.thread.subscribe(cb),
    () => runtime.thread.getState().isRunning,
  )
}

// ---- 消息部件分派（module 级稳定引用——tools.by_name + Text 渲染器） ----

function TextPart({ text }: TextMessagePartProps) {
  return <MarkdownText text={text} />
}

const PART_COMPONENTS = {
  Text: TextPart,
  tools: {
    by_name: {
      [AI_TOOL_STORAGE]: StorageToolPart,
      [AI_TOOL_CREATE_REPO]: ConfirmToolPart,
    },
    Fallback: ToolFallbackPart,
  },
}

// ---- 用户气泡（哨兵行 → 上下文徽章；正文 = 转义文本节点） ----

function UserBubble({ message }: { message: MessageState }) {
  const raw = message.content
    .filter((p): p is { type: 'text'; text: string } => p.type === 'text')
    .map((p) => p.text)
    .join('\n')
  const { context, text } = parseContextSentinel(raw)
  const coord = coordOf((context as AiContext | null) ?? null)
  return (
    <div className="flex flex-col items-end gap-1" data-testid={`ai-msg-user-${message.index}`}>
      {coord && (
        <span
          className="max-w-full truncate rounded-sm border border-border bg-surface-2 px-1.5 py-px font-mono text-aux text-muted-foreground"
          data-testid="ai-msg-context-badge"
          title={t('发送时的页面坐标（上下文注入）')}
          lang="en"
        >
          {coord}
        </span>
      )}
      <div className="max-w-[92%] rounded-lg rounded-br-sm bg-primary px-3 py-2 text-dense leading-relaxed text-primary-foreground">
        <span className="whitespace-pre-wrap break-words">{text}</span>
      </div>
    </div>
  )
}

// ---- 助手气泡（parts 分层渲染 + running 指示 + 错误态重试） ----

function AssistantBubble({ message, onRetry }: { message: MessageState; onRetry: (messageId: string, parentId: string | null) => void }) {
  const status = message.status
  const running = status?.type === 'running'
  const hasVisible = message.content.some((p) => p.type === 'text' && p.text !== '')
  const errored = status?.type === 'incomplete' && status.reason === 'error'
  // 抛错详情（toAssistantError 形态 {code, message}——非对象回落 String）
  const errDetail =
    errored && status.error !== undefined
      ? typeof status.error === 'object' && status.error !== null && 'message' in status.error
        ? String((status.error as { message: unknown }).message)
        : String(status.error)
      : ''
  return (
    <div className="flex flex-col items-start gap-1" data-testid={`ai-msg-assistant-${message.index}`}>
      <div className="max-w-full w-full rounded-lg rounded-bl-sm border border-border bg-surface-1 px-3 py-2">
        <MessagePrimitive.Parts components={PART_COMPONENTS} />
        {running && !hasVisible && (
          <span className="inline-flex items-center gap-1 py-0.5" data-testid="ai-typing" role="status" aria-label={t('正在生成')}>
            <span className="size-1.5 animate-pulse rounded-full bg-muted-foreground" />
            <span className="size-1.5 animate-pulse rounded-full bg-muted-foreground [animation-delay:150ms]" />
            <span className="size-1.5 animate-pulse rounded-full bg-muted-foreground [animation-delay:300ms]" />
          </span>
        )}
        {errored && (
          <div className="flex flex-col gap-2 py-1" data-testid="ai-error" role="alert">
            <p className="text-dense text-destructive">
              {t('响应生成失败（本地状态机异常）')}
              {errDetail ? <span className="block font-mono text-aux">{errDetail}</span> : null}
            </p>
            <button
              type="button"
              className="self-start rounded-md border border-border px-2.5 py-1 text-dense hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring"
              data-testid="ai-error-retry"
              onClick={() => onRetry(message.id, message.parentId)}
            >
              {t('重试')}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

// ---- 会话面板（Provider 内——消息流 + 输入框） ----

function ThreadPanel({ runtime }: { runtime: AssistantRuntime }) {
  const isRunning = useThreadRunning(runtime)
  const [draft, setDraft] = useState('')
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const setContextSnapshot = useAiStore((s) => s.setContextSnapshot)
  const messages = runtime.thread.getState().messages
  const isEmpty = messages.length === 0

  const send = useCallback(
    (raw?: string) => {
      const text = (raw ?? draft).trim()
      if (text === '' || runtime.thread.getState().isRunning) return
      // 上下文注入消息首帧：活上下文快照 → 哨兵行（provider 解析 + 用户气泡徽章）
      const ctx = useAiStore.getState().context
      setContextSnapshot(ctx ?? deriveAiContext(window.location.pathname.replace(/^\/binflow\/ui/, ''), window.location.search))
      const sentinel = ctx ? `${AI_CONTEXT_MARK} ${JSON.stringify(ctx)}\n` : ''
      runtime.thread.append({
        role: 'user',
        content: [{ type: 'text', text: `${sentinel}${text}` }],
        startRun: true,
      })
      setDraft('')
    },
    [draft, runtime, setContextSnapshot],
  )

  const retry = useCallback(
    (messageId: string, parentId: string | null) => {
      // 原位重生成（MessageRuntime.reload）；兜底 startRun（分支重建）
      try {
        runtime.thread.getMessageById(messageId)?.reload()
      } catch {
        runtime.thread.startRun({ parentId })
      }
    },
    [runtime],
  )

  const renderMessage = useCallback(
    ({ message }: { message: MessageState }) =>
      message.role === 'user' ? <UserBubble message={message} /> : <AssistantBubble message={message} onRetry={retry} />,
    [retry],
  )

  const onInputKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      send()
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* 消息流（Viewport 自带增长自动滚——ThreadPrimitive.Viewport） */}
      <ThreadPrimitive.Root className="flex min-h-0 flex-1 flex-col">
        {isEmpty ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center" data-testid="ai-empty">
            <p className="text-h3 font-semibold">{t('AI 助手')}</p>
            <p className="max-w-[38ch] text-dense text-muted-foreground">
              {t('上下文感知 Copilot（演示）——当前无后端端点，响应由本地 mock 生成，零网络请求。')}
            </p>
            <div className="flex flex-col gap-2 pt-1">
              <button
                type="button"
                className="rounded-md border border-border px-3 py-1.5 text-dense hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring"
                data-testid="ai-empty-suggest-storage"
                onClick={() => {
                  setDraft(t('查一下存储占用'))
                  inputRef.current?.focus()
                }}
              >
                {t('查一下存储占用')}
              </button>
              <button
                type="button"
                className="rounded-md border border-border px-3 py-1.5 text-dense hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring"
                data-testid="ai-empty-suggest-create"
                onClick={() => {
                  setDraft(t('帮我创建仓库'))
                  inputRef.current?.focus()
                }}
              >
                {t('帮我创建仓库')}
              </button>
            </div>
          </div>
        ) : (
          <ThreadPrimitive.Viewport className="min-h-0 flex-1 overflow-y-auto px-4 py-4" data-testid="ai-thread">
            <div className="flex flex-col gap-4">
              <ThreadPrimitive.Messages>{renderMessage}</ThreadPrimitive.Messages>
            </div>
          </ThreadPrimitive.Viewport>
        )}
      </ThreadPrimitive.Root>

      {/* 输入框（Enter 发送 / Shift+Enter 换行；运行中禁发） */}
      <div className="border-t border-border p-3">
        <div className="flex items-end gap-2 rounded-lg border border-input bg-surface-1 p-2 focus-within:border-ring">
          <textarea
            ref={inputRef}
            rows={2}
            className="min-h-[2.4em] max-h-[9em] flex-1 resize-none bg-transparent text-dense leading-relaxed outline-none placeholder:text-muted-foreground"
            placeholder={t('问点什么…（Enter 发送，Shift+Enter 换行）')}
            aria-label={t('AI 输入框')}
            data-testid="ai-input"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={onInputKeyDown}
          />
          <button
            type="button"
            className={cn(
              'shrink-0 rounded-md px-3 py-1.5 text-dense font-medium transition-colors focus-visible:outline-2 focus-visible:outline-ring',
              draft.trim() === '' || isRunning
                ? 'cursor-not-allowed bg-secondary text-muted-foreground'
                : 'bg-primary text-primary-foreground hover:opacity-90',
            )}
            data-testid="ai-send"
            disabled={draft.trim() === '' || isRunning}
            onClick={() => send()}
          >
            {t('发送')}
          </button>
        </div>
        <p className="mt-1.5 px-1 text-aux text-muted-foreground">{t('本地 mock（零网络）——不发送任何请求到服务端。')}</p>
      </div>
    </div>
  )
}

// ---- Drawer 壳（右滑 Sheet——Radix Dialog 语义；Esc/回焦交 FocusScope） ----
// runtime 由 AiAssistant.tsx 持有（Dialog Content 卸载不随葬——会话态跨
// 开合存活），本组件只做头部/消息流/输入框的渲染面。

export function ChatDrawerBody({ runtime }: { runtime: AssistantRuntime }) {
  const location = useLocation()
  const setContext = useAiStore((s) => s.setContext)

  // 活上下文随路由更新（drawer 开着导航也跟着换坐标——徽章常真）
  useEffect(() => {
    setContext(deriveAiContext(location.pathname, location.search))
  }, [location.pathname, location.search, setContext])

  const context = useAiStore((s) => s.context)
  const coord = coordOf(context)

  return (
    <div className="flex h-full flex-col">
      {/* 头部：标题 + mock 徽章 + 上下文徽章 + 关闭 */}
      <div className="flex items-center gap-2 border-b border-border px-4 py-3">
        <span className="text-h3 font-semibold">{t('AI 助手')}</span>
        <span className="rounded-sm bg-secondary px-1.5 py-px text-aux text-muted-foreground">{t('本地演示')}</span>
        <span className="min-w-0 flex-1" />
        {context && (
          <span
            className="flex min-w-0 items-center gap-1 rounded-sm border border-border bg-surface-2 px-1.5 py-px text-aux"
            data-testid="ai-context-badge"
            title={t('当前页面坐标（发送时注入消息上下文）')}
          >
            <span className="shrink-0 text-muted-foreground">{AI_DOMAIN_WORD[context.domain] ?? context.domain}</span>
            {coord && (
              <span className="max-w-[22ch] truncate font-mono" lang="en">
                {coord}
              </span>
            )}
          </span>
        )}
        <DialogPrimitive.Close
          className="rounded-sm px-1.5 py-1 text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring"
          data-testid="ai-close"
          aria-label={t('关闭 AI 助手')}
          title={t('关闭（Esc）')}
        >
          <span aria-hidden="true">✕</span>
        </DialogPrimitive.Close>
      </div>
      <ThreadPanel runtime={runtime} />
    </div>
  )
}
