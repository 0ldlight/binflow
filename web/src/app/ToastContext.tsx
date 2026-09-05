import { createContext, useCallback, useContext, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Link from '@mui/material/Link'
import Snackbar from '@mui/material/Snackbar'
import { tr } from '../i18n'

const tt = tr('console')

// toast（console-ux §3.5）：右下角堆叠；成功 5s 自动消失、错误常驻至
// 手动关闭；可带一个动作链接。aria-live 播报，无需焦点抢占。
// T-344 批 B：div.toast-stack + .toast 手作条 → Snackbar + Alert
// （mui-native-visual §4.2）。MUI Snackbar 自带 fixed 锚位与多实例无内建
// 堆叠——堆叠形态由容器 Box 承载，Snackbar 置 static 参与列流并保留
// Slide 入场动效；`toast`/`toast-stack` 类名与 toast 锚原样（§3.8 钩子）。

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
      <Box
        className="toast-stack"
        aria-live="polite"
        sx={{
          position: 'fixed',
          right: 16,
          bottom: 16,
          zIndex: (t) => t.zIndex.snackbar,
          display: 'flex',
          flexDirection: 'column',
          gap: 1,
          maxWidth: 380,
        }}
      >
        {items.map((t) => (
          <Snackbar
            key={t.id}
            open
            sx={{ position: 'static', left: 'auto', right: 'auto', justifyContent: 'flex-start' }}
          >
            <Alert
              className={`toast ${t.kind}`}
              data-testid="toast"
              severity={t.kind}
              role={t.kind === 'error' ? 'alert' : 'status'}
              onClose={() => dismiss(t.id)}
              closeText={tt('关闭通知')}
              sx={{ alignItems: 'flex-start' }}
              action={
                t.action ? (
                  <Link
                    component="button"
                    underline="hover"
                    color="inherit"
                    onClick={(e) => {
                      e.preventDefault()
                      t.action?.onClick()
                      dismiss(t.id)
                    }}
                  >
                    {t.action.label}
                  </Link>
                ) : undefined
              }
            >
              {t.message}
            </Alert>
          </Snackbar>
        ))}
      </Box>
    </ToastContext.Provider>
  )
}

export function useToast(): ToastApi {
  return useContext(ToastContext)
}
