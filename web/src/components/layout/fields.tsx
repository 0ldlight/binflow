// 表单控件组合层：所有生产表单入口必须落在 shadcn/ui 原语上。这里只保留
// BinFlow 的 320px 表单密度、mono 形态与既有 SelectField 调用 API；控件
// 本体分别由 ui/input、ui/textarea 与 Radix ui/select 承载。
import type { ChangeEvent, ComponentProps, ReactNode } from 'react'

import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

export function TextInput({ mono, className, ...rest }: ComponentProps<typeof Input> & { mono?: boolean }) {
  return <Input {...rest} className={cn('max-w-[320px]', mono && 'font-mono', className)} />
}

const EMPTY_VALUE = '__binflow_empty_select__'

export interface SelectFieldOption {
  value: string | number
  label: ReactNode
  itemProps?: Omit<ComponentProps<typeof SelectItem>, 'value' | 'children'>
}

export function SelectField({
  options,
  className,
  value,
  defaultValue,
  onChange,
  disabled,
  ...rest
}: Omit<ComponentProps<typeof SelectTrigger>, 'children' | 'onValueChange' | 'value' | 'defaultValue'> & {
    options: SelectFieldOption[]
    value?: string | number
    defaultValue?: string | number
    onChange?: (event: ChangeEvent<HTMLSelectElement>) => void
  }) {
  const wire = (v: string | number | undefined) => (v === undefined || v === null ? undefined : v === '' ? EMPTY_VALUE : String(v))
  return (
    <Select
      disabled={disabled}
      value={wire(value)}
      defaultValue={wire(defaultValue)}
      onValueChange={(next) => {
        onChange?.({ target: { value: next === EMPTY_VALUE ? '' : next } } as ChangeEvent<HTMLSelectElement>)
      }}
    >
      <SelectTrigger disabled={disabled} className={cn('max-w-[320px]', className)} {...rest} data-value={value === undefined || value === null ? '' : String(value)}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {options.map((o) => (
          <SelectItem key={String(o.value)} value={o.value === '' ? EMPTY_VALUE : String(o.value)} {...o.itemProps} data-value={String(o.value)}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

export function TextArea({ mono, className, ...rest }: ComponentProps<typeof Textarea> & { mono?: boolean }) {
  return <Textarea {...rest} className={cn('max-w-[320px]', mono && 'font-mono', className)} />
}
