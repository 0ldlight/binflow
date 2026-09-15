// 开关 primitive：Radix Switch（shadcn CLI 再生成 + 批 4 token 微调：
// 轨道高走 spacing 档、焦点环 outline 形态、去 shadow-xs/dark:*）。
// 双档：default（h-4.5×w-8）/ sm（h-3.5×w-6）——表格行内用 sm。
import * as SwitchPrimitive from '@radix-ui/react-switch'
import type { ComponentProps } from 'react'

import { cn } from '@/lib/utils'

function Switch({
  className,
  size = 'default',
  ...props
}: ComponentProps<typeof SwitchPrimitive.Root> & {
  size?: 'sm' | 'default'
}) {
  return (
    <SwitchPrimitive.Root
      data-slot="switch"
      data-size={size}
      className={cn(
        'peer group/switch inline-flex shrink-0 items-center rounded-full border border-transparent transition-colors outline-none',
        'focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-ring',
        'disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive',
        'data-[size=default]:h-4.5 data-[size=default]:w-8 data-[size=sm]:h-3.5 data-[size=sm]:w-6',
        'data-[state=checked]:bg-primary data-[state=unchecked]:bg-surface-3 data-[state=unchecked]:border-border-strong',
        className,
      )}
      {...props}
    >
      <SwitchPrimitive.Thumb
        data-slot="switch-thumb"
        className={cn(
          'pointer-events-none block rounded-full bg-background transition-transform duration-base ease-standard',
          'group-data-[size=default]/switch:size-3.5 group-data-[size=sm]/switch:size-2.5',
          'data-[state=checked]:translate-x-[calc(100%-2px)] data-[state=unchecked]:translate-x-0',
        )}
      />
    </SwitchPrimitive.Root>
  )
}

export { Switch }
