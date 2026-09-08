// 骨架 primitive：加载态占位块（四态缺省锚 skeleton 的载体；150ms
// 防闪烁节奏归上层容器）。
import type { ComponentProps } from 'react'

import { cn } from '@/lib/utils'

function Skeleton({ className, ...props }: ComponentProps<'div'>) {
  return <div data-slot="skeleton" data-testid="skeleton" className={cn('animate-pulse rounded-sm bg-surface-2', className)} {...props} />
}

export { Skeleton }
