import type { ReactNode } from 'react'
// Toast 门面（sonner → 家族锚桥）：全控制台唯一的 toast 出口——每个 toast
// 自动携带 data-testid="toast"（sonner 的 per-toast testId 通道），使
// [data-testid="toast"] 锚族（15+ spec 消费面）在换栈后零迁移。
// 新页面一律 `import { toast } from '@/lib/toast'`，不直连 sonner。
import { toast as sonnerToast } from 'sonner'

type Message = ReactNode
type Opts = Parameters<typeof sonnerToast>[1] & { testId?: string }

function call(kind: 'success' | 'error' | 'info' | 'warning' | 'loading' | 'message', message: Message, data?: Opts) {
  const opts = { testId: 'toast', ...data }
  switch (kind) {
    case 'success':
      return sonnerToast.success(message, opts)
    case 'error':
      return sonnerToast.error(message, opts)
    case 'info':
      return sonnerToast.info(message, opts)
    case 'warning':
      return sonnerToast.warning(message, opts)
    case 'loading':
      return sonnerToast.loading(message, opts)
    default:
      return sonnerToast(message, opts)
  }
}

/** 家族锚版 toast：success/error/info/warning/loading/dismiss 全通道 */
export const toast = Object.assign(
  (message: Message, data?: Opts) => call('message', message, data),
  {
    success: (message: Message, data?: Opts) => call('success', message, data),
    error: (message: Message, data?: Opts) => call('error', message, data),
    info: (message: Message, data?: Opts) => call('info', message, data),
    warning: (message: Message, data?: Opts) => call('warning', message, data),
    loading: (message: Message, data?: Opts) => call('loading', message, data),
    dismiss: (id?: string | number) => sonnerToast.dismiss(id),
    promise: sonnerToast.promise,
    custom: sonnerToast.custom,
  },
)
