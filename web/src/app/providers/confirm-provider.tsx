// ConfirmProvider：Promise 化危险确认层（对齐旧 components/ConfirmDialog
// 语义——danger 红边/打开即聚焦取消/Esc 兜底；typed 确认（输入 repo key/
// YES 等匹配门）由调用方经 confirmPhrase 启用）。P1 仅模块就位，不接线。
import { createContext, useCallback, useContext, useRef, useState } from 'react'
import type { ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

export interface ConfirmOptions {
  title: string
  description?: string
  /** 危险动作红变体 */
  danger?: boolean
  /** typed 确认：需用户输入该短语才可确认（如 repo key / YES / EMPTY） */
  confirmPhrase?: string
  confirmLabel?: string
  cancelLabel?: string
}

interface ConfirmContextValue {
  confirm: (options: ConfirmOptions) => Promise<boolean>
}

const ConfirmContext = createContext<ConfirmContextValue>({ confirm: async () => false })

interface PendingConfirm {
  options: ConfirmOptions
  resolve: (ok: boolean) => void
}

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [pending, setPending] = useState<PendingConfirm | null>(null)
  const [typed, setTyped] = useState('')
  // 打开即聚焦取消钮（回调 ref——危险动作默认路径是放弃）
  const cancelRef = useRef<HTMLButtonElement>(null)
  const cancelRefCb = useCallback((el: HTMLButtonElement | null) => {
    cancelRef.current = el
    if (el) queueMicrotask(() => el.focus())
  }, [])

  const settle = (ok: boolean) => {
    pending?.resolve(ok)
    setPending(null)
    setTyped('')
  }

  const confirm = useCallback((options: ConfirmOptions) => {
    return new Promise<boolean>((resolve) => {
      setPending({ options, resolve })
      setTyped('')
    })
  }, [])

  const phraseGate = pending?.options.confirmPhrase
  const phraseOk = !phraseGate || typed.trim() === phraseGate

  return (
    <ConfirmContext.Provider value={{ confirm }}>
      {children}
      <Dialog open={pending !== null} onOpenChange={(open) => { if (!open) settle(false) }}>
        {pending && (
          <DialogContent
            className={pending.options.danger ? 'border-destructive' : undefined}
            onOpenAutoFocus={(e) => e.preventDefault()}
          >
            <DialogHeader>
              <DialogTitle>{pending.options.title}</DialogTitle>
              {pending.options.description && <DialogDescription>{pending.options.description}</DialogDescription>}
            </DialogHeader>
            {phraseGate && (
              <input
                data-testid="confirm-phrase-input"
                className="flex h-8 w-full rounded-sm border border-input bg-surface-3 px-2.5 text-dense outline-none focus-visible:border-ring"
                onChange={(e) => setTyped(e.target.value)}
                value={typed}
                autoComplete="off"
              />
            )}
            <DialogFooter>
              <Button ref={cancelRefCb} variant="outline" onClick={() => settle(false)}>
                {pending.options.cancelLabel ?? 'Cancel'}
              </Button>
              <Button
                variant={pending.options.danger ? 'destructive' : 'default'}
                disabled={!phraseOk}
                data-testid="confirm-accept"
                onClick={() => settle(true)}
              >
                {pending.options.confirmLabel ?? 'Confirm'}
              </Button>
            </DialogFooter>
          </DialogContent>
        )}
      </Dialog>
    </ConfirmContext.Provider>
  )
}

/** 危险确认钩子：await confirm({...}) → true=用户确认 */
export function useConfirm(): ConfirmContextValue {
  return useContext(ConfirmContext)
}
