// Toast primitive：Sonner Toaster 封装（右下角堆叠——对齐旧 Snackbar
// 位形；success 自动消失 / error 常驻的策略归上层 toast() 调用约定）。
import { Toaster as Sonner, toast } from 'sonner'
import type { ComponentProps } from 'react'

type ToasterProps = ComponentProps<typeof Sonner>

function Toaster({ theme = 'system', ...props }: ToasterProps) {
  return (
    <Sonner
      data-slot="toaster"
      theme={theme}
      position="bottom-right"
      className="toaster group"
      toastOptions={{
        classNames: {
          toast: 'group toast rounded-md border border-border bg-surface-1 text-foreground shadow-overlay',
          description: 'text-muted-foreground',
          actionButton: 'bg-primary text-primary-foreground',
          cancelButton: 'bg-surface-2 text-foreground',
        },
      }}
      {...props}
    />
  )
}

export { Toaster, toast }
