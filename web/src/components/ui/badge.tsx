// 徽章 primitive：状态/计数小标签（default 实底 / secondary / outline /
// success / warning / info / destructive 语义变体）。
// 批 4（§4.2 Badge☆）：状态族改 *-surface 软底槽（§3.1 批 1 定义的
// --bf-{success,warning,info,danger}-surface 自此进消费缝）+ dot 变体
// （状态点前缀，颜色随文字语义）。
// 批 5（T-UIB5，design-system-plan §6）：bits.tsx Badge 升格并入——旧
// base.css .badge 配方以 tint-* 变体等值承载（color-mix 走桥接层
// badge-* 槽；px-[7px]/border-0/font-normal 覆盖基线，字号 12px/1.4
// 以任意值 token 形自持——twMerge 对 text-* 自定义字号名按色彩组消解，
// 基线 text-aux 与变体色类同串会被吞，故变体自携带）。tier 档复用
// info/warning 配方（M10 T-288 闭集）。与软底族的收敛裁决归批 6。
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
        // tint 族=旧 .badge 配方等值承载（批 5；自持字号/行高/无边框/常规字重）
        'tint-neutral': 'border-0 bg-secondary px-[7px] font-normal [line-height:var(--bf-lh-xs)] text-[length:var(--bf-fs-xs)] text-muted-foreground',
        'tint-info': 'border-0 bg-badge-info-soft px-[7px] font-normal [line-height:var(--bf-lh-xs)] text-[length:var(--bf-fs-xs)] text-badge-info',
        'tint-success': 'border-0 bg-badge-success-soft px-[7px] font-normal [line-height:var(--bf-lh-xs)] text-[length:var(--bf-fs-xs)] text-badge-success',
        'tint-warning': 'border-0 bg-badge-warning-soft px-[7px] font-normal [line-height:var(--bf-lh-xs)] text-[length:var(--bf-fs-xs)] text-badge-warning',
        'tint-danger': 'border-0 bg-badge-danger-soft px-[7px] font-normal [line-height:var(--bf-lh-xs)] text-[length:var(--bf-fs-xs)] text-badge-danger',
        'tint-pro': 'border-0 bg-badge-info-soft px-[7px] font-normal [line-height:var(--bf-lh-xs)] text-[length:var(--bf-fs-xs)] text-badge-info',
        'tint-enterprise': 'border-0 bg-badge-warning-soft px-[7px] font-normal [line-height:var(--bf-lh-xs)] text-[length:var(--bf-fs-xs)] text-badge-warning',
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
  mono = false,
  asChild = false,
  children,
  ...props
}: ComponentProps<'span'> & VariantProps<typeof badgeVariants> & { asChild?: boolean; dot?: boolean; mono?: boolean }) {
  const Comp = asChild ? Slot : 'span'
  return (
    <Comp data-slot="badge" data-variant={variant ?? 'default'} className={cn(badgeVariants({ variant }), mono && 'font-mono', dot && 'gap-1.5', className)} {...props}>
      {/* dot 仅常规形态（asChild 单子元素纪律——Slot 多子会炸） */}
      {!asChild && dot && <span aria-hidden="true" data-slot="badge-dot" className="size-1.5 shrink-0 rounded-full bg-current" />}
      {children}
    </Comp>
  )
}

export { Badge, badgeVariants }
