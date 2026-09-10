// 新栈通用小件（P3 管理面域共用——Badge/状态徽章/Alert 盒/复选行）：
// 旧 MUI Chip/Alert/FormControlLabel 的语义等价件，样式走既有语义类
// （base.css .badge 家族 / pages.css .form-error/.warn-box——assert-tokens
// 硬门下的既有 token 消费面），e2e 的类断言（badge neutral 等）零迁移。
import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'
import { tr } from '@/i18n'

const t = tr('common')

/** 徽章（语义色四态 + mono 变体——旧 MUI Chip 语义等价件） */
export function Badge({
  variant = 'neutral',
  mono,
  lang,
  className,
  title,
  testid,
  children,
}: {
  variant?: 'neutral' | 'success' | 'warning' | 'danger'
  mono?: boolean
  lang?: string
  className?: string
  title?: string
  testid?: string
  children: ReactNode
}) {
  return (
    <span
      className={cn('badge', variant, mono && 'mono', className)}
      lang={lang}
      title={title}
      data-testid={testid}
    >
      {children}
    </span>
  )
}

/** 启用/禁用徽章（E2/E3 enabled 真值——users 列与详情 facts 行共用）。
 *  不挂 .badge 基类（行级 .badge 唯一性——ADR-0029 决策 3 沿袭）。 */
export function StatusLabel({ enabled, name }: { enabled: boolean; name?: string }) {
  // 对象键条件展开形态（anchor-audit 扫描词汇表——行级动态锚 T-267 同款）
  const testid = name ? { 'data-testid': `user-status-${name}` } : {}
  return enabled ? (
    <span className="status-pill status-on text-success" {...testid}>
      {t('启用')}
    </span>
  ) : (
    <span className="status-pill status-off text-destructive" {...testid}>
      {t('禁用')}
    </span>
  )
}

/** Alert 盒（error/warning/info/success——旧 MUI Alert 语义等价件；
 *  error 形态带 role=alert） */
export function AlertBox({
  severity = 'info',
  testid,
  className,
  children,
}: {
  severity?: 'error' | 'warning' | 'info' | 'success'
  testid?: string
  className?: string
  children: ReactNode
}) {
  return (
    <div
      role={severity === 'error' ? 'alert' : undefined}
      data-testid={testid}
      className={cn(
        'mb-2 rounded-md border px-3 py-2 text-dense',
        severity === 'error' && 'border-destructive/40 bg-surface-1 text-destructive',
        severity === 'warning' && 'border-warning/40 bg-surface-1 text-warning',
        severity === 'success' && 'border-success/40 bg-surface-1 text-success',
        severity === 'info' && 'border-border bg-surface-2 text-2',
        className,
      )}
    >
      {children}
    </div>
  )
}

/** 复选行（旧 MUI FormControlLabel+Checkbox 的等价件——check-row 类样式；
 *  预留位恒禁用形态可省 onChange） */
export function CheckRow({
  checked,
  onChange,
  disabled,
  label,
  testid,
  className,
}: {
  checked: boolean
  onChange?: (next: boolean) => void
  disabled?: boolean
  label: ReactNode
  testid?: string
  className?: string
}) {
  return (
    <label className={cn('check-row', className)}>
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange?.(e.target.checked)}
        {...(testid ? { 'data-testid': testid } : {})}
      />
      <span>{label}</span>
    </label>
  )
}
