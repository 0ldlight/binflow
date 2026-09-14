// Spinner primitive：加载态载体（§4.2 loading 态——Button loading / 行内
// busy 指示共用；与 RouteFallback 同款 border 旋转型）。装饰件——语义
// （aria-busy / 状态文案）归消费方承载。
import type { ComponentProps } from 'react'

import { cn } from '@/lib/utils'

function Spinner({ className, ...props }: ComponentProps<'span'>) {
  return (
    <span
      aria-hidden="true"
      data-slot="spinner"
      className={cn('inline-block size-4 animate-spin rounded-full border-2 border-border border-t-primary align-[-2px]', className)}
      {...props}
    />
  )
}

export { Spinner }
