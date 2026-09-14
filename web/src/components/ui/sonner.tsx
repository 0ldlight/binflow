// Toast primitive：Sonner Toaster 封装（右下角堆叠——对齐旧 Snackbar
// 位形；success 自动消失 / error 常驻的策略归上层 toast() 调用约定——
// closeButton 开启：常驻 error 的手动关闭通道，P4 起旧 Snackbar 退役后
// 的对位能力）。
// 批 4（§4.2 Toast★）：四语义底 = *-surface 软底 + 状态色文字 + 左缘条
// （§3.1 软底槽消费缝）；主题双谱由挂载方（app/providers/toast-provider）
// 传入——本件保持哑件不反向依赖 app 层。
import { Toaster as Sonner, toast } from 'sonner'
import type { ComponentProps } from 'react'

type ToasterProps = ComponentProps<typeof Sonner>

function Toaster({ theme = 'light', ...props }: ToasterProps) {
  return (
    <Sonner
      data-slot="toaster"
      theme={theme}
      position="bottom-right"
      closeButton
      className="toaster group"
      toastOptions={{
        classNames: {
          toast: 'group toast rounded-md border border-border bg-surface-1 text-foreground shadow-overlay',
          description: 'text-muted-foreground',
          actionButton: 'bg-primary text-primary-foreground',
          cancelButton: 'bg-surface-2 text-foreground',
          // 四语义底（§4.2：软底 + 状态色文字 + 左缘条；图标随 currentColor）
          success: 'border-l-2 border-l-success bg-success-surface text-success [&_[data-icon]]:text-success',
          error: 'border-l-2 border-l-destructive bg-destructive-surface text-destructive [&_[data-icon]]:text-destructive',
          warning: 'border-l-2 border-l-warning bg-warning-surface text-warning [&_[data-icon]]:text-warning',
          info: 'border-l-2 border-l-info bg-info-surface text-info [&_[data-icon]]:text-info',
        },
      }}
      {...props}
    />
  )
}

export { Toaster, toast }
