// 徽章 primitive：状态/计数小标签（default 实底 / secondary / outline /
// success / warning / info / destructive 语义变体）。
// 批 4（§4.2 Badge☆）：状态族改 *-surface 软底槽（§3.1 批 1 定义的
// --bf-{success,warning,info,danger}-surface 自此进消费缝）+ dot 变体
// （状态点前缀，颜色随文字语义）。
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import type { ComponentProps } from 'react'

import { cn } from '@/lib/utils'

const badgeVariants = cva(
  'inline-flex w-fit shrink-0 items-center justify-center gap-1 overflow-hidden whitespace-nowrap rounded-sm border px-1.5 py-0.5 text-aux font-medium [&>svg]:size-3',
  {
    variants: {
      variant: {
        default: 'border-transparent bg-primary text-primary-foreground',
        secondary: 'border-transparent bg-secondary text-secondary-foreground',
        outline: 'border-border text-foreground',
        // 状态族=软底（*-surface 槽 + 状态色文字；双谱各配软底值）
        success: 'border-transparent bg-success-surface text-success',
        warning: 'border-transparent bg-warning-surface text-warning',
        info: 'border-transparent bg-info-surface text-info',
        destructive: 'border-transparent bg-destructive text-destructive-foreground',
        'destructive-soft': 'border-transparent bg-destructive-surface text-destructive',
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  },
)

function Badge({
  className,
  variant,
  dot = false,
  asChild = false,
  children,
  ...props
}: ComponentProps<'span'> & VariantProps<typeof badgeVariants> & { asChild?: boolean; dot?: boolean }) {
  const Comp = asChild ? Slot : 'span'
  return (
    <Comp data-slot="badge" className={cn(badgeVariants({ variant }), dot && 'gap-1.5', className)} {...props}>
      {/* dot 仅常规形态（asChild 单子元素纪律——Slot 多子会炸） */}
      {!asChild && dot && <span aria-hidden="true" data-slot="badge-dot" className="size-1.5 shrink-0 rounded-full bg-current" />}
      {children}
    </Comp>
  )
}

export { Badge, badgeVariants }
