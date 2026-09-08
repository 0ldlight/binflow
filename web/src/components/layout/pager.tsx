// 新栈 Pager（T-451 形态锚 parity §11.2 的 shadcn/Tailwind 承载——锚族
// pager-range / pager-size(-<n>) / pager-first / pager-prev / pager-next /
// pager-last / pager-page-<n> 原样；旧 components/Pager.tsx 随 MUI 终验
// 退役）。useClientPager 语义逐行平移（epoch 派生回零）。
import { useState } from 'react'
import type { ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { tr } from '@/i18n'

const t = tr('console')

/** 每页行数档位（票内冻结——与旧 Pager 同值） */
export const PAGER_SIZE_OPTIONS = [20, 50, 100, 200, 1000] as const

/** 缺省页大小 = 家族页窗 100 */
export const PAGER_SIZE_DEFAULT = 100

export interface PagerProps {
  page: number
  pageCount: number
  onPageChange: (page: number) => void
  from: number
  to: number
  total: number | null
  note?: ReactNode
  pageSize?: number
  onPageSizeChange?: (size: number) => void
  sizeOptions?: readonly number[]
  lastUnknown?: boolean
  disabled?: boolean
}

/** 页码序列（siblingCount=1 / boundaryCount=1——MUI Pagination 同款窗口） */
function pageWindow(page: number, pageCount: number): { key: string; n: number | null }[] {
  const out: { key: string; n: number | null }[] = []
  if (pageCount <= 5) {
    for (let i = 1; i <= pageCount; i++) out.push({ key: String(i), n: i })
    return out
  }
  out.push({ key: '1', n: 1 })
  const s = Math.max(2, page - 1)
  const e = Math.min(pageCount - 1, page + 1)
  if (s > 2) out.push({ key: 'el', n: null })
  for (let i = s; i <= e; i++) out.push({ key: String(i), n: i })
  if (e < pageCount - 1) out.push({ key: 'er', n: null })
  out.push({ key: String(pageCount), n: pageCount })
  return out
}

export function Pager({
  page,
  pageCount,
  onPageChange,
  from,
  to,
  total,
  note,
  pageSize,
  onPageSizeChange,
  sizeOptions = PAGER_SIZE_OPTIONS,
  lastUnknown = false,
  disabled = false,
}: PagerProps) {
  const count = Math.max(1, pageCount)
  return (
    <div className="pager flex flex-wrap items-center justify-end gap-3 py-2 text-dense">
      <span className="pager-range text-aux text-muted-foreground" data-testid="pager-range">
        {total === null
          ? t('显示 {from} – {to}（末页未知）', { from, to })
          : t('显示 {from} – {to} / 共 {total} 项', { from, to, total })}
        {note}
      </span>
      {pageSize !== undefined && onPageSizeChange !== undefined && (
        <label className="pager-size flex items-center gap-1.5">
          <span aria-hidden="true">{t('每页')}</span>
          <select
            value={pageSize}
            disabled={disabled}
            data-testid="pager-size"
            aria-label={t('每页行数')}
            onChange={(e) => {
              const n = Number(e.target.value)
              if (Number.isFinite(n) && n > 0) onPageSizeChange(n)
            }}
            className="h-7 rounded-sm border border-input bg-surface-1 px-1.5 text-dense"
          >
            {sizeOptions.map((n) => (
              <option key={n} value={n} data-testid={`pager-size-${n}`}>
                {n}
              </option>
            ))}
          </select>
          <span aria-hidden="true">{t('行')}</span>
        </label>
      )}
      <nav className="flex items-center gap-0.5" aria-label={t('分页')}>
        <Button variant="outline" size="sm" className="h-7 px-2" data-testid="pager-first" disabled={disabled || page <= 1} onClick={() => onPageChange(1)} aria-label={t('首页')}>
          «
        </Button>
        <Button variant="outline" size="sm" className="h-7 px-2" data-testid="pager-prev" disabled={disabled || page <= 1} onClick={() => onPageChange(page - 1)} aria-label={t('上一页')}>
          ‹
        </Button>
        {pageWindow(page, count).map(({ key, n }) =>
          n === null ? (
            <span key={key} className="px-1 text-muted-foreground" aria-hidden="true">
              …
            </span>
          ) : (
            <Button
              key={key}
              variant={n === page ? 'default' : 'outline'}
              size="sm"
              className="h-7 min-w-7 px-1.5"
              data-testid={`pager-page-${n}`}
              disabled={disabled}
              onClick={() => onPageChange(n)}
              aria-current={n === page ? 'page' : undefined}
            >
              {n}
            </Button>
          ),
        )}
        <Button variant="outline" size="sm" className="h-7 px-2" data-testid="pager-next" disabled={disabled || page >= count} onClick={() => onPageChange(page + 1)} aria-label={t('下一页')}>
          ›
        </Button>
        <Button
          variant="outline"
          size="sm"
          className="h-7 px-2"
          data-testid="pager-last"
          disabled={disabled || page >= count}
          onClick={() => onPageChange(count)}
          aria-label={t('末页')}
          title={lastUnknown ? t('流式末页未知——逐页推进（呈现对齐、语义自有 C 注）') : undefined}
        >
          »
        </Button>
      </nav>
    </div>
  )
}

// ---- 客户端页窗（全量已取回面的窗口化）----

export function useClientPager(total: number, epoch: unknown, initialSize = PAGER_SIZE_DEFAULT) {
  const [state, setState] = useState<{ epoch: unknown; page: number; size: number }>(() => ({
    epoch: undefined,
    page: 1,
    size: initialSize,
  }))
  // epoch 未匹配 = 新结果集：派生回落第 1 页（页大小偏好保留）
  const live = state.epoch === epoch ? state : { epoch, page: 1, size: state.size }
  const pageCount = Math.max(1, Math.ceil(total / live.size))
  const page = Math.min(live.page, pageCount)
  const from = total === 0 ? 0 : (page - 1) * live.size + 1
  const to = Math.min(page * live.size, total)
  const setPage = (p: number) => setState({ epoch, page: p, size: live.size })
  const setSize = (n: number) => setState({ epoch, page: 1, size: n })
  const slice = <T,>(rows: readonly T[]): T[] => rows.slice((page - 1) * live.size, page * live.size)
  return { page, size: live.size, pageCount, from, to, setPage, setSize, slice }
}
