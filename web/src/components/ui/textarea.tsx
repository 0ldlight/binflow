// 多行输入 primitive（shadcn CLI 再生成 + 批 4 token 微调：密度字号
// text-dense、输入底 surface-3、焦点环 outline 形态、去 shadow-xs/dark:*、
// aria-invalid 边框走 danger——与 Input 同一状态口径）。
import type { ComponentProps } from 'react'

import { cn } from '@/lib/utils'

function Textarea({ className, ...props }: ComponentProps<'textarea'>) {
  return (
    <textarea
      data-slot="textarea"
      className={cn(
        'flex min-h-16 w-full rounded-sm border border-input bg-surface-3 px-2.5 py-1.5 text-dense leading-relaxed transition-colors outline-none',
        'placeholder:text-muted-foreground focus-visible:border-ring focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-ring',
        'disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive',
        className,
      )}
      {...props}
    />
  )
}

export { Textarea }
