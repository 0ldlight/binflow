// 页签 primitive：Radix Tabs（方向键切换 + aria-tabs 语义）；消费面 =
// 仓库三 Tab / 认证配置三协议段等子路由页签的键盘腿。
// 批 4（§4.2 Tabs☆）：active = accent 下划线指示（motion token：
// duration-base + ease-standard 的 scale/opacity 入场）+ semibold；
// hover = surface-2；键盘左右箭头为 Radix 内建。
import * as TabsPrimitive from '@radix-ui/react-tabs'
import type { ComponentProps } from 'react'

import { cn } from '@/lib/utils'

const Tabs = TabsPrimitive.Root

function TabsList({ className, ...props }: ComponentProps<typeof TabsPrimitive.List>) {
  return (
    <TabsPrimitive.List
      data-slot="tabs-list"
      className={cn('inline-flex h-9 items-center gap-0.5 border-b border-border text-muted-foreground', className)}
      {...props}
    />
  )
}

function TabsTrigger({ className, ...props }: ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      data-slot="tabs-trigger"
      className={cn(
        'relative inline-flex flex-1 items-center justify-center gap-1.5 whitespace-nowrap rounded-xs px-2.5 py-1.5 text-dense font-medium transition-colors duration-base ease-standard',
        'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring disabled:pointer-events-none disabled:opacity-50',
        'hover:bg-surface-2 hover:text-foreground',
        'data-[state=active]:text-foreground data-[state=active]:font-semibold',
        // 下划线指示：absolute 伪元素沿底线，inactive 收为 scale-x-0——
        // 切换时以 motion token（duration-base/ease-standard）滑入
        'after:pointer-events-none after:absolute after:inset-x-1.5 after:-bottom-px after:h-0.5 after:origin-center after:rounded-full after:bg-primary after:transition-transform after:duration-base after:ease-standard data-[state=inactive]:after:scale-x-0 data-[state=active]:after:scale-x-100',
        className,
      )}
      {...props}
    />
  )
}

function TabsContent({ className, ...props }: ComponentProps<typeof TabsPrimitive.Content>) {
  return <TabsPrimitive.Content data-slot="tabs-content" className={cn('flex-1 outline-none', className)} {...props} />
}

export { Tabs, TabsContent, TabsList, TabsTrigger }
