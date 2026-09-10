import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import * as DialogPrimitive from '@radix-ui/react-dialog'

import { tr } from '../i18n'

const tt = tr('console')

// 危险确认对话框（console-ux §3.5）：居中 modal，焦点圈进对话框、Tab
// 循环、Esc 关闭（= 取消）。后续票的删除仓 / GC apply / 删 manifest 都
// 走这个基座（T-88 R6：共享组件由 T-98 沉淀）；确认按钮的前置条件
// （如输入 repo key）由调用方在 body 里自管。
//
// FE-P4 MUI 清场：MUI Dialog → Radix Dialog（shadcn 底座）。锚族与
// 调用方 API 逐字保真：confirm-dialog / confirm-cancel / confirm-accept
// testid、`.modal` 类（artifacts-tree 等 spec 的 fallback 选择器）、
// body 内 `.confirm-input` 规格钩子（pages.css 规则原样生效）、
// confirmDisabled() 重求值缝（body 输入事件冒泡 bump）。焦点圈进/
// Tab 循环/滚动锁定交 Radix FocusScope；Esc 与 backdrop 点击 = 取消
// （旧壳行为原样）；打开即聚焦取消钮（安全默认）由回调 ref + 微任务
// 承载（Radix Portal 同为二段式提交——挂载期 ref 尚空，与 MUI 期同坑）。
// 卸载回焦由 ui/dialog 的 DialogContent 承载——本件直挂 primitive，
// 在 Content 卸载 effect 里显式回焦启动元素（focus() 归位家族契约）。

export interface ConfirmOptions {
  title: string
  body?: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  /** danger 时确认按钮红变体 + modal 红边（P5 危险区语义） */
  danger?: boolean
  /**
   * 确认按钮的前置条件（P5：如「输入 repo key 才可用」，§3.5）。body 里
   * 的输入事件会让对话框重渲染并重新求值（T-99 加的缝：调用方把可变
   * 状态收在闭包/holder 里，onInput/onKeyDown 时在这里读）。可选、向后
   * 兼容——不传即恒可用。
   */
  confirmDisabled?: () => boolean
}

const ConfirmContext = createContext<(opts: ConfirmOptions) => Promise<boolean>>(
  async () => false,
)

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [opts, setOpts] = useState<ConfirmOptions | null>(null)
  const resolverRef = useRef<((v: boolean) => void) | null>(null)

  const confirm = useCallback((next: ConfirmOptions) => {
    return new Promise<boolean>((resolve) => {
      // 一次只允许一个对话框：后来的请求直接否决前一个（保守）。
      resolverRef.current?.(false)
      resolverRef.current = resolve
      setOpts(next)
    })
  }, [])

  const settle = useCallback((v: boolean) => {
    resolverRef.current?.(v)
    resolverRef.current = null
    setOpts(null)
  }, [])

  useEffect(() => () => resolverRef.current?.(false), [])

  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      {opts && <ConfirmDialog key="confirm" opts={opts} onSettle={settle} />}
    </ConfirmContext.Provider>
  )
}

export function useConfirm(): (opts: ConfirmOptions) => Promise<boolean> {
  return useContext(ConfirmContext)
}

function ConfirmDialog({
  opts,
  onSettle,
}: {
  opts: ConfirmOptions
  onSettle: (v: boolean) => void
}) {
  // body 内输入（输入 key 确认等前置）触发重渲染，让 confirmDisabled
  // 重新求值（T-99 缝：body 是静态 ReactNode，靠事件冒泡刷新按钮态）。
  const [, bump] = useState(0)

  // 打开即聚焦取消按钮（安全默认，§3.5）。Radix FocusScope 挂载后会把
  // 焦点放进内容（首焦落 Content）——onOpenAutoFocus preventDefault 让位
  // 后自聚焦取消钮（此刻按钮已随 Content 同 commit 进 DOM；微任务让
  // Portal 焦点链先就位）。回调 ref 仅留作断链兜底。
  const cancelRef = useRef<HTMLButtonElement | null>(null)
  const focusCancel = useCallback((node: HTMLButtonElement | null) => {
    cancelRef.current = node
  }, [])

  // 卸载回焦：捕获场外启动元素，settle 后归位（quick-set-me-up 等菜单链
  // 路的回焦契约——与 ui/dialog 的 DialogContent 同款微任务让位策略）
  const returnFocusRef = useRef<HTMLElement | null>(null)
  useEffect(() => {
    returnFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    return () => {
      queueMicrotask(() => {
        if (document.activeElement === document.body || document.activeElement === null) {
          const el = returnFocusRef.current
          if (el && el.isConnected) el.focus({ preventScroll: true })
        }
      })
    }
  }, [])

  return (
    <DialogPrimitive.Root open onOpenChange={(open) => { if (!open) onSettle(false) }}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay
          data-slot="dialog-overlay"
          className="fixed inset-0 z-[90] bg-scrim"
        />
        <DialogPrimitive.Content
          // §3.8 钩子：.modal 类名留 DOM（artifacts-tree 等 spec 的 fallback
          // 选择器）；pages.css 的 .modal .server-reason 规则续生效。
          className="modal fixed top-1/2 left-1/2 z-[90] grid w-[min(440px,calc(100vw-48px))] -translate-x-1/2 -translate-y-1/2 gap-4 rounded-lg border border-border bg-surface-1 p-4 shadow-modal data-[state=danger]:border-destructive/50"
          data-testid="confirm-dialog"
          data-state={opts.danger ? 'danger' : undefined}
          aria-labelledby="confirm-dialog-title"
          onOpenAutoFocus={(e) => {
            e.preventDefault()
            queueMicrotask(() => cancelRef.current?.focus())
          }}
          onInput={() => bump((t) => t + 1)}
          onClick={() => bump((t) => t + 1)}
        >
          <DialogPrimitive.Title id="confirm-dialog-title" className="text-h3 font-semibold">
            {opts.title}
          </DialogPrimitive.Title>
          {opts.body && <div className="text-dense">{opts.body}</div>}
          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            <Button
              ref={focusCancel}
              variant="outline"
              data-testid="confirm-cancel"
              onClick={() => onSettle(false)}
            >
              {opts.cancelLabel ?? tt('取消')}
            </Button>
            <Button
              variant={opts.danger ? 'destructive' : 'default'}
              data-testid="confirm-accept"
              disabled={opts.confirmDisabled ? opts.confirmDisabled() : false}
              onClick={() => onSettle(true)}
            >
              {opts.confirmLabel ?? tt('确认')}
            </Button>
          </div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}
