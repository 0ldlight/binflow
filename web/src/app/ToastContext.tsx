import { createContext, useCallback, useContext, useRef, useState } from 'react'
import type { ReactNode } from 'react'

// toast（console-ux §3.5）：右下角堆叠；成功 5s 自动消失、错误常驻至
// 手动关闭；可带一个动作链接。aria-live 播报，无需焦点抢占。

export interface ToastAction {
  label: string
  onClick: () => void
}

interface ToastItem {
  id: number
  kind: 'success' | 'error'
  message: string
  action?: ToastAction
}

interface ToastApi {
  success: (message: string, action?: ToastAction) => void
  error: (message: string, action?: ToastAction) => void
}

const ToastContext = createContext<ToastApi>({
  success: () => {},
  error: () => {},
})

const SUCCESS_TTL_MS = 5000

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([])
  const nextId = useRef(1)

  const dismiss = useCallback((id: number) => {
    setItems((cur) => cur.filter((t) => t.id !== id))
  }, [])

  const push = useCallback(
    (kind: ToastItem['kind'], message: string, action?: ToastAction) => {
      const id = nextId.current++
      setItems((cur) => [...cur, { id, kind, message, action }])
      if (kind === 'success') {
        window.setTimeout(() => dismiss(id), SUCCESS_TTL_MS)
      }
    },
    [dismiss],
  )

  const success = useCallback((message: string, action?: ToastAction) => push('success', message, action), [push])
  const error = useCallback((message: string, action?: ToastAction) => push('error', message, action), [push])

  return (
    <ToastContext.Provider value={{ success, error }}>
      {children}
      <div className="toast-stack" data-testid="toast-stack" aria-live="polite">
        {items.map((t) => (
          <div key={t.id} className={`toast ${t.kind}`} data-testid="toast" role={t.kind === 'error' ? 'alert' : 'status'}>
            <span aria-hidden="true">{t.kind === 'success' ? '✓' : '✗'}</span>
            <div className="msg">
              {t.message}
              {t.action && (
                <a
                  href="#"
                  onClick={(e) => {
                    e.preventDefault()
                    t.action?.onClick()
                    dismiss(t.id)
                  }}
                >
                  {t.action.label}
                </a>
              )}
            </div>
            <button type="button" className="close" aria-label="关闭通知" onClick={() => dismiss(t.id)}>
              ×
            </button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}

export function useToast(): ToastApi {
  return useContext(ToastContext)
}
