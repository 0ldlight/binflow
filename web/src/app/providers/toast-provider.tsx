// ToastProvider：Sonner Toaster 挂载（右下堆叠，对齐旧 Snackbar 位形）
// + 旧 ToastContext 的 success/error+action 语义约定（error 常驻策略归
// 调用方 duration 传参）。文案一律走 i18n 后传值（本层零硬编码）。
// 批 4（§4.2 Toast★ 主题双谱腿）：theme 从 ThemeProvider 取当前值
// （此前 Toaster 缺省 'system' 与 <html data-theme> 切换脱钩——暗色下
// 亮 toast 的底片漂移）。
import type { ReactNode } from 'react'

import { Toaster } from '@/components/ui/sonner'

import { useTheme } from './theme-provider'

export function ToastProvider({ children }: { children: ReactNode }) {
  const { theme } = useTheme()
  return (
    <>
      {children}
      <Toaster theme={theme} />
    </>
  )
}
