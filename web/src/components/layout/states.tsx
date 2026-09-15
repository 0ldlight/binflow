// 新栈四态三件套（frontend-rewrite-architecture §2 components/layout/）：
// Skeleton / ErrorCard / EmptyState——语义与旧 components/{Skeleton,
// ErrorCard,EmptyState}.tsx 对齐，四态缺省锚（skeleton / error-card +
// error-retry / empty-state）原样保留（console-ux §10.1 冻结锚）。
// 旧三件随 MUI 终验退役；新页面一律消费本层。
// 批 4（§4.2 EmptyState★/ErrorState★ 统一件）：标题档 fs-md（text-h3）、
// 描述 text-dense text-2；ErrorCard 补 danger 图标 + 错误细节 mono 折叠
// （<details>，默认收起——原文不缺席、不喧宾夺主）+ retry 钮。API 与
// testid 锚零变化（40 消费面零迁移）。
import type { ReactNode } from 'react'
import { CircleAlert } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { tr } from '@/i18n'

const t = tr('common')

/** 防闪烁骨架（150ms 延迟与旧 Skeleton 同款语义） */
export function StateSkeleton({ lines = 4, className }: { lines?: number; className?: string }) {
  return (
    <div data-testid="skeleton" aria-hidden="true" className={cn('flex flex-col gap-2 pt-2', className)}>
      {Array.from({ length: lines }, (_, i) => (
        <div
          key={i}
          className="h-4 animate-pulse rounded-sm bg-surface-2"
          style={{ width: `${88 - i * (48 / Math.max(lines, 1))}%` }}
        />
      ))}
    </div>
  )
}

interface ErrorLike {
  status: number
  message: string
}

/** 错误卡（ErrorState 统一件）：人话 + retry + 服务端原文（mono 折叠） */
export function ErrorCard({ error, onRetry, className }: { error: ErrorLike; onRetry?: () => void; className?: string }) {
  return (
    <div
      data-testid="error-card"
      role="alert"
      className={cn(
        'flex flex-col items-start gap-1 rounded-md border border-destructive/40 bg-surface-1 px-4 py-3',
        className,
      )}
    >
      <div className="flex items-center gap-2 text-dense font-medium text-destructive">
        <CircleAlert aria-hidden="true" className="size-4 shrink-0" />
        {t('加载失败（HTTP {v1}）', { v1: error.status })}
      </div>
      {error.message && (
        <details className="w-full">
          <summary className="cursor-pointer text-aux text-muted-foreground">{t('错误详情')}</summary>
          <div className="mt-1 break-all font-mono text-aux text-muted-foreground" lang="en">
            {error.message}
          </div>
        </details>
      )}
      {onRetry && (
        <Button variant="outline" size="sm" className="mt-2" onClick={onRetry}>
          {t('重试')}
        </Button>
      )}
    </div>
  )
}

/** 空态（EmptyState 统一件）：图标 + 标题（fs-md）+ 描述 + 主行动 + 插画槽
 *  （T-388：40px 插图槽 + CTA 在下） */
export function EmptyState({
  message,
  hint,
  action,
  testid = 'empty-state',
  illustration = false,
  className,
}: {
  message: string
  hint?: ReactNode
  action?: ReactNode
  testid?: string
  /** 插画槽（40px 图形位——保持旧 EmptyState 的空态插画语义） */
  illustration?: boolean
  className?: string
}) {
  return (
    <div
      data-testid={testid}
      className={cn('flex flex-col items-start gap-1.5 rounded-md border border-dashed border-border bg-surface-1 px-4 py-6', className)}
    >
      {illustration && (
        <div
          aria-hidden="true"
          data-testid="empty-art"
          className="mb-1 grid size-10 place-items-center rounded-md bg-surface-2 text-muted-foreground"
        >
          <svg viewBox="0 0 20 20" className="size-5" fill="none" stroke="currentColor" strokeWidth="1.4">
            <path d="M3 6.5A1.5 1.5 0 0 1 4.5 5h3l1.5 2h6.5A1.5 1.5 0 0 1 17 8.5v6A1.5 1.5 0 0 1 15.5 16h-11A1.5 1.5 0 0 1 3 14.5z" />
            <path d="M7 11h6M7 13.5h4" strokeLinecap="round" />
          </svg>
        </div>
      )}
      <p className="text-h3 font-medium">{message}</p>
      {hint && <p className="max-w-prose text-dense text-muted-foreground">{hint}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  )
}
