// 输入框 primitive：13px 表单基线的受控/非受控输入（file 变体透明化
// 交由上层组合）。
import type { ComponentProps } from 'react'

import { cn } from '@/lib/utils'

function Input({ className, type, ...props }: ComponentProps<'input'>) {
  return (
    <input
      type={type}
      data-slot="input"
      className={cn(
        'flex h-8 w-full min-w-0 rounded-sm border border-input bg-surface-3 px-2.5 py-1 text-dense transition-colors outline-none',
        'placeholder:text-muted-foreground focus-visible:border-ring focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-ring',
        'disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive',
        'file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-dense file:font-medium',
        className,
      )}
      {...props}
    />
  )
}

export { Input }
