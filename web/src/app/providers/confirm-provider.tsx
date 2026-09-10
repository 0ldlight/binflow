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
  /** P3 起允许富体（级联影响摘要等）——Radix DialogDescription 收 ReactNode */
  description?: ReactNode
  /** 危险动作红变体 */
  danger?: boolean
  /** typed 确认：需用户输入该短语才可确认（如 repo key / YES / EMPTY） */
  confirmPhrase?: string
  confirmLabel?: string
  cancelLabel?: string
}

export interface PromptOptions {
  title: string
  description?: ReactNode
  /** 输入框占位 */
  placeholder?: string
  /** 初值 */
  initial?: string
  /** 行内校验（返回错误文案 = 确认禁用 + 行内呈现；null = 通过） */
  validate?: (value: string) => string | null
  confirmLabel?: string
  cancelLabel?: string
  danger?: boolean
  /** 输入框锚（data-testid 落点；对象键字面量形态对 anchor-audit 可见——
   *  与 authconfig sections.ts 的 anchor: 字段册同一扫描词汇表） */
  anchor?: string
  /** mono 输入（路径/键位族） */
  mono?: boolean
  /** 允许空值确认（缺省要求非空——恢复「留空 = 按原位」族的例外位） */
  allowEmpty?: boolean
}

interface ConfirmContextValue {
  confirm: (options: ConfirmOptions) => Promise<boolean>
  /** 带输入框的确认（mkdir / 命名族——旧 ConfirmDialog body-input 语义
   *  的新栈对位：validate 即 confirmDisabled 门） */
  prompt: (options: PromptOptions) => Promise<string | null>
}

const ConfirmContext = createContext<ConfirmContextValue>({ confirm: async () => false, prompt: async () => null })

interface PendingConfirm {
  options: ConfirmOptions
  resolve: (ok: boolean) => void
}

interface PendingPrompt {
  options: PromptOptions
  resolve: (value: string | null) => void
}

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [pending, setPending] = useState<PendingConfirm | null>(null)
  const [typed, setTyped] = useState('')
  const [promptPending, setPromptPending] = useState<PendingPrompt | null>(null)
  const [promptValue, setPromptValue] = useState('')
  // 打开即聚焦取消钮（回调 ref——危险动作默认路径是放弃；confirm 与
  // prompt 双对话框同契约 §3.5，prompt 腿 P2 期缺者 FE-P4 补齐）
  const cancelRef = useRef<HTMLButtonElement>(null)
  const cancelRefCb = useCallback((el: HTMLButtonElement | null) => {
    cancelRef.current = el
    if (el) queueMicrotask(() => el.focus())
  }, [])
  const promptCancelRefCb = useCallback((el: HTMLButtonElement | null) => {
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

  const prompt = useCallback((options: PromptOptions) => {
    return new Promise<string | null>((resolve) => {
      setPromptPending({ options, resolve })
      setPromptValue(options.initial ?? '')
    })
  }, [])

  const settlePrompt = (value: string | null) => {
    promptPending?.resolve(value)
    setPromptPending(null)
    setPromptValue('')
  }

  const phraseGate = pending?.options.confirmPhrase
  const phraseOk = !phraseGate || typed.trim() === phraseGate

  return (
    <ConfirmContext.Provider value={{ confirm, prompt }}>
      {children}
      <Dialog
        open={promptPending !== null}
        onOpenChange={(open) => { if (!open) settlePrompt(null) }}
      >
        {promptPending && (
          <DialogContent
            className={promptPending.options.danger ? 'border-destructive' : undefined}
            onOpenAutoFocus={(e) => e.preventDefault()}
            data-testid="confirm-dialog"
            aria-labelledby="confirm-dialog-title"
          >
            <DialogHeader>
              <DialogTitle id="confirm-dialog-title">{promptPending.options.title}</DialogTitle>
              {promptPending.options.description && (
                <DialogDescription>{promptPending.options.description}</DialogDescription>
              )}
            </DialogHeader>
            <input
              data-testid={promptPending.options.anchor}
              className={`h-8 w-full rounded-sm border border-input bg-surface-3 px-2.5 text-dense outline-none focus-visible:border-ring ${promptPending.options.mono ? 'font-mono' : ''}`}
              value={promptValue}
              autoComplete="off"
              placeholder={promptPending.options.placeholder}
              onChange={(e) => setPromptValue(e.target.value)}
            />
            {promptPending.options.validate && promptValue !== '' && promptPending.options.validate(promptValue.trim()) && (
              <p className="field-error text-aux text-destructive" role="alert">
                {promptPending.options.validate!(promptValue.trim())}
              </p>
            )}
            <DialogFooter>
              <Button ref={promptCancelRefCb} variant="outline" data-testid="confirm-cancel" onClick={() => settlePrompt(null)}>
                {promptPending.options.cancelLabel ?? 'Cancel'}
              </Button>
              <Button
                variant={promptPending.options.danger ? 'destructive' : 'default'}
                disabled={
                  (!promptPending.options.allowEmpty && promptValue.trim() === '') ||
                  (promptPending.options.validate?.(promptValue.trim()) ?? null) !== null
                }
                data-testid="confirm-accept"
                onClick={() => settlePrompt(promptValue.trim())}
              >
                {promptPending.options.confirmLabel ?? 'Confirm'}
              </Button>
            </DialogFooter>
          </DialogContent>
        )}
      </Dialog>
      <Dialog open={pending !== null} onOpenChange={(open) => { if (!open) settle(false) }}>
        {pending && (
          <DialogContent
            className={pending.options.danger ? 'border-destructive' : undefined}
            onOpenAutoFocus={(e) => e.preventDefault()}
            data-testid="confirm-dialog"
            aria-labelledby="confirm-dialog-title"
          >
            <DialogHeader>
              <DialogTitle id="confirm-dialog-title">{pending.options.title}</DialogTitle>
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
              <Button ref={cancelRefCb} variant="outline" data-testid="confirm-cancel" onClick={() => settle(false)}>
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
