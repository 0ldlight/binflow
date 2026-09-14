// 输入框 primitive：13px 表单基线的受控/非受控输入（file 变体透明化
// 交由上层组合）。
// 批 4（§4.2 Input★）：error 态=aria-invalid → danger 边（关联错误文案
// 与 aria-describedby 归 FormField 胶水，批 6）；affix 变体=prefix/suffix
// 槽（mono 后缀单位、copy 按钮等）——带 affix 时外包裹 focus-within 壳，
// 裸形态保持原样零迁移。
import type { ComponentProps, ReactNode } from 'react'

import { cn } from '@/lib/utils'

const inputBase =
  'w-full min-w-0 rounded-sm border border-input bg-surface-3 px-2.5 py-1 text-dense transition-colors outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-ring disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive'

function Input({
  className,
  type,
  prefix,
  suffix,
  ...props
}: Omit<ComponentProps<'input'>, 'prefix' | 'suffix'> & {
  /** 前缀槽（单位/图标等装饰件——aria-hidden，纯装饰）。覆写原生
   * prefix 属性（RDFa 遗产，无消费面）以容纳 ReactNode */
  prefix?: ReactNode
  /** 后缀槽（mono 单位、copy 按钮等——可含交互件，不设 aria-hidden） */
  suffix?: ReactNode
}) {
  // 裸形态：无 affix 时输出与既往逐字一致的单 input（消费面零迁移）
  if (!prefix && !suffix) {
    return (
      <input
        type={type}
        data-slot="input"
        className={cn('flex h-8', inputBase, 'file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-dense file:font-medium', className)}
        {...props}
      />
    )
  }
  return (
    <div
      data-slot="input-group"
      className={cn(
        'flex h-8 w-full min-w-0 items-center rounded-sm border border-input bg-surface-3 transition-colors',
        'focus-within:border-ring focus-within:outline-2 focus-within:outline-offset-1 focus-within:outline-ring',
        // error 态边由壳承载（内层 input 无边框）
        "has-[[aria-invalid='true']]:border-destructive",
        className,
      )}
    >
      {prefix && (
        <span aria-hidden="true" className="flex shrink-0 items-center pl-2 text-aux text-muted-foreground [&_svg]:size-3.5">
          {prefix}
        </span>
      )}
      <input
        type={type}
        data-slot="input"
        className="h-full min-w-0 flex-1 grow border-0 bg-transparent px-2 py-1 text-dense outline-none placeholder:text-muted-foreground focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50"
        {...props}
      />
      {suffix && <span className="flex shrink-0 items-center pr-2 text-aux text-muted-foreground [&_svg]:size-3.5">{suffix}</span>}
    </div>
  )
}

export { Input }
