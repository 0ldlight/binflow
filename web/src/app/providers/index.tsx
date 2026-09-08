// Provider 装配（frontend-rewrite-architecture §2 providers/）：Query +
// Theme + Toast + Confirm 组合层。P1 仅模块就位——main.tsx 不动，接线随
// P2 新壳挂载（层序：Theme 最外（data-theme 先于渲染面生效），Confirm
// 在 Router 语境之外亦可 Promise 化）。
import type { ReactNode } from 'react'

import { ConfirmProvider } from './confirm-provider'
import { QueryProvider } from './query-provider'
import { ThemeProvider } from './theme-provider'
import { ToastProvider } from './toast-provider'

export function AppProviders({ children }: { children: ReactNode }) {
  return (
    <ThemeProvider>
      <QueryProvider>
        <ToastProvider>
          <ConfirmProvider>{children}</ConfirmProvider>
        </ToastProvider>
      </QueryProvider>
    </ThemeProvider>
  )
}

export { useConfirm } from './confirm-provider'
export { QueryProvider } from './query-provider'
export { ThemeProvider, useTheme } from './theme-provider'
export { ToastProvider } from './toast-provider'
