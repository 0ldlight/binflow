// 抽屉 primitive：vaul 底部/四向滑出面板（拖拽把手可关）；消费面 =
// SetMe Up 式右向详情抽屉（parity 册 D 系形态）。
// 批 6（§4.2 Drawer★）：vaul 自带 slide keyframes（slideFrom/ToBottom，
// 开合双态都按自身高特异度 data-state 选择器定名）与 .5s 隐式时长——
// 本件不重造动画，只挂 anim-slide-in/anim-fade-in 类作桥接层覆盖钩
// （tailwind.css 的 [data-vaul-*] 抬特异度规则把 duration/timing 压回
// token 档 dur-slow/ease，prefers-reduced-motion 随 token 降 1ms）；
// 拖拽关闭的即时位移仍是 vaul 自有机制。
import { Drawer as DrawerPrimitive } from 'vaul'
import type { ComponentProps } from 'react'

import { cn } from '@/lib/utils'

const Drawer = DrawerPrimitive.Root
const DrawerTrigger = DrawerPrimitive.Trigger
const DrawerPortal = DrawerPrimitive.Portal
const DrawerClose = DrawerPrimitive.Close

function DrawerOverlay({ className, ...props }: ComponentProps<typeof DrawerPrimitive.Overlay>) {
  return (
    <DrawerPrimitive.Overlay
      data-slot="drawer-overlay"
      className={cn('fixed inset-0 z-[90] bg-scrim anim-fade-in', className)}
      {...props}
    />
  )
}

function DrawerContent({ className, children, ...props }: ComponentProps<typeof DrawerPrimitive.Content>) {
  return (
    <DrawerPortal>
      <DrawerOverlay />
      <DrawerPrimitive.Content
        data-slot="drawer-content"
        className={cn(
          'fixed inset-x-0 bottom-0 z-[90] flex h-auto flex-col rounded-t-lg border border-border bg-surface-1 shadow-modal anim-slide-in',
          className,
        )}
        {...props}
      >
        <div className="mx-auto mt-3 h-1.5 w-12 shrink-0 rounded-full bg-border-strong" />
        {children}
      </DrawerPrimitive.Content>
    </DrawerPortal>
  )
}

function DrawerHeader({ className, ...props }: ComponentProps<'div'>) {
  return <div data-slot="drawer-header" className={cn('flex flex-col gap-1 px-4 pt-3', className)} {...props} />
}

function DrawerFooter({ className, ...props }: ComponentProps<'div'>) {
  return <div data-slot="drawer-footer" className={cn('mt-auto flex gap-2 px-4 pb-4', className)} {...props} />
}

function DrawerTitle({ className, ...props }: ComponentProps<typeof DrawerPrimitive.Title>) {
  return <DrawerPrimitive.Title data-slot="drawer-title" className={cn('text-h3 font-semibold', className)} {...props} />
}

function DrawerDescription({ className, ...props }: ComponentProps<typeof DrawerPrimitive.Description>) {
  return (
    <DrawerPrimitive.Description
      data-slot="drawer-description"
      className={cn('text-dense text-muted-foreground', className)}
      {...props}
    />
  )
}

export { Drawer, DrawerClose, DrawerContent, DrawerDescription, DrawerFooter, DrawerHeader, DrawerOverlay, DrawerPortal, DrawerTitle, DrawerTrigger }
