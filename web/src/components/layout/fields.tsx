// 新栈表单控件（P3 管理面域共用——.field 网格内的 input/select/textarea
// 统一密度与形态；MUI TextField 系退役）。样式 = Tailwind 语义 token（与
// confirm-provider 输入同款），外层 .field/.field-error/.field-hint 类
// 沿用 base.css/pages.css 既有排版。
import type { ComponentProps, ReactNode, SelectHTMLAttributes, TextareaHTMLAttributes } from 'react'

import { cn } from '@/lib/utils'

/** 密度基类（input/select/textarea 共用形态） */
export const FIELD_CONTROL =
  'h-8 w-full max-w-[320px] rounded-sm border border-input bg-surface-3 px-2.5 text-dense outline-none focus-visible:border-ring disabled:cursor-not-allowed disabled:opacity-60'

/** 文本输入（.field 网格消费——mono/testid/lang/ref 透传；React 19 ref 即
 *  常规 prop，自动聚焦腿经调用方 ref 直接落 input 本体） */
export function TextInput({
  mono,
  className,
  ...rest
}: ComponentProps<'input'> & { mono?: boolean }) {
  return <input {...rest} className={cn(FIELD_CONTROL, mono && 'font-mono', className)} />
}

/** 原生 select（MUI TextField select native 的等价件——宽度调用方定） */
export function NativeSelect({
  options,
  className,
  ...rest
}: SelectHTMLAttributes<HTMLSelectElement> & { options: { value: string; label: ReactNode }[] }) {
  return (
    <select {...rest} className={cn(FIELD_CONTROL, 'pr-6', className)}>
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  )
}

/** 多行文本（rows 透传；mono 供 URL/过滤器族） */
export function TextArea({
  mono,
  className,
  ...rest
}: TextareaHTMLAttributes<HTMLTextAreaElement> & { mono?: boolean }) {
  return (
    <textarea
      {...rest}
      className={cn(FIELD_CONTROL, 'h-auto min-h-16 py-1.5 leading-relaxed', mono && 'font-mono', className)}
    />
  )
}
