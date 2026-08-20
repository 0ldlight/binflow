import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'

// 危险确认对话框（console-ux §3.5）：居中 modal，焦点圈进对话框、Tab
// 循环、Esc 关闭（= 取消）。后续票的删除仓 / GC apply / 删 manifest 都
// 走这个基座（T-88 R6：共享组件由 T-98 沉淀）；确认按钮的前置条件
// （如输入 repo key）由调用方在 body 里自管。

export interface ConfirmOptions {
  title: string
  body?: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  /** danger 时确认按钮红变体 + modal 红边（P5 危险区语义） */
  danger?: boolean
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
      {opts && (
        <ConfirmDialog key="confirm" opts={opts} onSettle={settle} />
      )}
    </ConfirmContext.Provider>
  )
}

export function useConfirm(): (opts: ConfirmOptions) => Promise<boolean> {
  return useContext(ConfirmContext)
}

const FOCUSABLE = 'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'

function ConfirmDialog({
  opts,
  onSettle,
}: {
  opts: ConfirmOptions
  onSettle: (v: boolean) => void
}) {
  const rootRef = useRef<HTMLDivElement>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)

  // 焦点陷阱：打开即聚焦取消按钮（安全默认）；Tab 在对话框内循环；
  // Esc = 取消（§3.5/§8）。
  useEffect(() => {
    cancelRef.current?.focus()
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onSettle(false)
        return
      }
      if (e.key !== 'Tab') return
      const nodes = Array.from(rootRef.current?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? [])
      if (nodes.length === 0) return
      const first = nodes[0]
      const last = nodes[nodes.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [onSettle])

  return (
    <div
      className="modal-backdrop"
      onClick={(e) => {
        if (e.target === e.currentTarget) onSettle(false)
      }}
    >
      <div
        ref={rootRef}
        className={`modal${opts.danger ? ' danger' : ''}`}
        role="dialog"
        aria-modal="true"
        aria-label={opts.title}
        data-testid="confirm-dialog"
      >
        <h2>{opts.title}</h2>
        {opts.body && <div className="modal-body">{opts.body}</div>}
        <div className="modal-actions">
          <button ref={cancelRef} type="button" className="btn" data-testid="confirm-cancel" onClick={() => onSettle(false)}>
            {opts.cancelLabel ?? '取消'}
          </button>
          <button
            type="button"
            autoFocus={false}
            className={`btn${opts.danger ? ' danger' : ' primary'}`}
            data-testid="confirm-accept"
            onClick={() => onSettle(true)}
          >
            {opts.confirmLabel ?? '确认'}
          </button>
        </div>
      </div>
    </div>
  )
}
