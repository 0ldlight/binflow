// ToastProvider：Sonner Toaster 挂载（右下堆叠，对齐旧 Snackbar 位形）
// + 旧 ToastContext 的 success/error+action 语义约定（error 常驻策略归
// 调用方 duration 传参）。文案一律走 i18n 后传值（本层零硬编码）。
// P1 仅模块就位，不接线。
import type { ReactNode } from 'react'

import { Toaster } from '@/components/ui/sonner'

export function ToastProvider({ children }: { children: ReactNode }) {
  return (
    <>
      {children}
      <Toaster />
    </>
  )
}
