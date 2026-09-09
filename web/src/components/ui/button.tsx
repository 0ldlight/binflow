// 按钮 primitive：cva 变体（default 实底主操作 / secondary / outline /
// 基座恒挂 text-foreground——Tailwind preflight 关闭期（P2 共存纪律）裸
// button 是 UA 默认黑底灰面，深色主题下 ButtonFace 组合曾触 axe 对比度
// serious（radix-* 按钮、t512 ⑥ 双主题腿实证）；P4 恢复 preflight 后冗余。
// ghost / destructive / link）× 四尺寸；dense 形态（默认档 32px 高，
// text-dense）——控制台按钮从不大写（对齐 Artifactory 观感）。
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import type { ComponentProps } from 'react'

import { cn } from '@/lib/utils'

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-dense font-medium text-foreground transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground shadow-flat hover:bg-primary/90',
        secondary: 'bg-secondary text-secondary-foreground hover:bg-secondary/80',
        outline: 'border border-input bg-surface-1 hover:bg-surface-2 hover:text-foreground',
        ghost: 'hover:bg-surface-2 hover:text-foreground',
        destructive: 'bg-destructive text-destructive-foreground shadow-flat hover:bg-destructive/90',
        link: 'text-primary underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-8 px-3 py-1 has-[>svg]:px-2.5',
        sm: 'h-7 rounded-sm px-2 has-[>svg]:px-2',
        lg: 'h-9 px-4 has-[>svg]:px-3',
        icon: 'size-8',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  },
)

function Button({
  className,
  variant,
  size,
  ...props
}: ComponentProps<'button'> & VariantProps<typeof buttonVariants>) {
  return (
    <button
      data-slot="button"
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  )
}

// asChild 槽位形态（Radix Slot 合成到子元素——链接化按钮等场景）
function ButtonAsChild({ className, variant, size, ...props }: ComponentProps<typeof Slot> & VariantProps<typeof buttonVariants>) {
  return <Slot data-slot="button" className={cn(buttonVariants({ variant, size, className }))} {...props} />
}

export { Button, ButtonAsChild, buttonVariants }
