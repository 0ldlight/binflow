// 侧滑面板 primitive：Radix Dialog 语义的 side 面板（right 默认——
// 详情/配置面 480px 档；top/bottom/left 四向可变）。
// 批 6（§4.2 Drawer★/§6）：滑入滑出走 slide motion token（dur-slow ×
// §3.6「modal/抽屉」档）——方向经 --bf-slide-from（translate 二元组）
// 随 side 变体注入，keyframes 不碰 transform（Radix 定位类零互踩）。
import * as SheetPrimitive from '@radix-ui/react-dialog'
import { X } from 'lucide-react'

import { tr } from '@/i18n'
import type { ComponentProps } from 'react'
import { cva, type VariantProps } from 'class-variance-authority'

import { cn } from '@/lib/utils'

const Sheet = SheetPrimitive.Root
const SheetTrigger = SheetPrimitive.Trigger
const SheetClose = SheetPrimitive.Close
const SheetPortal = SheetPrimitive.Portal

const t = tr('common')

function SheetOverlay({ className, ...props }: ComponentProps<typeof SheetPrimitive.Overlay>) {
  return (
    <SheetPrimitive.Overlay
      data-slot="sheet-overlay"
      className={cn('fixed inset-0 z-[90] bg-scrim data-[state=open]:anim-fade-in data-[state=closed]:anim-fade-out', className)}
      {...props}
    />
  )
}

const sheetVariants = cva(
  'fixed z-[90] flex flex-col gap-4 bg-surface-1 shadow-modal data-[state=open]:anim-slide-in data-[state=closed]:anim-slide-out',
  {
    variants: {
      side: {
        top: 'inset-x-0 top-0 h-auto border-b border-border [--bf-slide-from:0_-100%]',
        bottom: 'inset-x-0 bottom-0 h-auto border-t border-border [--bf-slide-from:0_100%]',
        left: 'inset-y-0 left-0 h-full w-3/4 max-w-[480px] border-r border-border [--bf-slide-from:-100%_0]',
        right: 'inset-y-0 right-0 h-full w-3/4 max-w-[480px] border-l border-border [--bf-slide-from:100%_0]',
      },
    },
    defaultVariants: {
      side: 'right',
    },
  },
)

function SheetContent({
  className,
  children,
  side = 'right',
  ...props
}: ComponentProps<typeof SheetPrimitive.Content> & VariantProps<typeof sheetVariants>) {
  return (
    <SheetPortal>
      <SheetOverlay />
      <SheetPrimitive.Content data-slot="sheet-content" className={cn(sheetVariants({ side }), className)} {...props}>
        {children}
        <SheetPrimitive.Close className="absolute top-3.5 right-3.5 rounded-sm text-muted-foreground opacity-70 transition-opacity hover:opacity-100 focus-visible:outline-2 focus-visible:outline-ring">
          <X className="size-4" />
          <span className="sr-only">{t('关闭')}</span>
        </SheetPrimitive.Close>
      </SheetPrimitive.Content>
    </SheetPortal>
  )
}

function SheetHeader({ className, ...props }: ComponentProps<'div'>) {
  return <div data-slot="sheet-header" className={cn('flex flex-col gap-1 px-4 pt-4', className)} {...props} />
}

function SheetFooter({ className, ...props }: ComponentProps<'div'>) {
  return <div data-slot="sheet-footer" className={cn('mt-auto flex gap-2 px-4 pb-4', className)} {...props} />
}

function SheetTitle({ className, ...props }: ComponentProps<typeof SheetPrimitive.Title>) {
  return <SheetPrimitive.Title data-slot="sheet-title" className={cn('text-h3 font-semibold', className)} {...props} />
}

function SheetDescription({ className, ...props }: ComponentProps<typeof SheetPrimitive.Description>) {
  return (
    <SheetPrimitive.Description
      data-slot="sheet-description"
      className={cn('text-dense text-muted-foreground', className)}
      {...props}
    />
  )
}

export { Sheet, SheetClose, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetOverlay, SheetPortal, SheetTitle, SheetTrigger }
