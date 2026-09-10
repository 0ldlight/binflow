// 新栈表格共享件（frontend-rewrite P3：security/governance 域列表页的
// 排序表头 + 列选器统一承载——语义平移自旧 pages/security/widgets.tsx
// 的 MUI 实现，锚与 aria-sort 三态不动；旧文件随本批退役）。
//
// - SortTh：th 本体承载点击与 aria-sort（asc → desc → none 循环，§4.7）；
// - useTableSort / applySort：排序状态与稳定副本排序（空值沉底）；
// - ColumnsMenu：列选器 Popover（menuitemcheckbox + 至少一列守卫 +
//   全选重置——users/groups/audit/search 共用形态；按钮/菜单锚由调用方
//   传入）。
import { useState } from 'react'

import { cn } from '@/lib/utils'

export type SortDir = 'asc' | 'desc'

export interface SortState<K extends string> {
  key: K | null
  dir: SortDir
}

/** 列头排序循环：none → asc → desc → none（多列不建，§4.7） */
export function useTableSort<K extends string>(initial: SortState<K> = { key: null, dir: 'asc' }) {
  const [sort, setSort] = useState<SortState<K>>(initial)
  const toggle = (key: K) => {
    setSort((p) => {
      if (p.key !== key) return { key, dir: 'asc' }
      if (p.dir === 'asc') return { key, dir: 'desc' }
      return { key: null, dir: 'asc' }
    })
  }
  return { sort, toggle }
}

/** 排序应用（稳定副本；取值器返回字符串/数字，空值沉底） */
export function applySort<T>(rows: readonly T[], sort: SortState<string>, valueOf: (r: T) => string | number | null): T[] {
  if (!sort.key) return [...rows]
  const dir = sort.dir === 'asc' ? 1 : -1
  return [...rows].sort((a, b) => {
    const va = valueOf(a) ?? ''
    const vb = valueOf(b) ?? ''
    if (va === vb) return 0
    return (va < vb ? -1 : 1) * dir
  })
}

/** 排序表头（新栈 plain th：点击循环三态；锚在 th 本体，aria-sort 同步） */
export function SortTh<K extends string>({
  label,
  sortKey,
  sort,
  onToggle,
  testid,
  className,
}: {
  label: string
  sortKey: K
  sort: SortState<K>
  onToggle: (key: K) => void
  testid?: string
  className?: string
}) {
  const active = sort.key === sortKey
  const ariaSort = active ? (sort.dir === 'asc' ? 'ascending' : 'descending') : 'none'
  return (
    <th
      scope="col"
      aria-sort={ariaSort}
      data-testid={testid}
      className={cn(
        'cursor-pointer whitespace-nowrap px-3 py-2 text-left text-aux font-medium text-muted-foreground hover:text-foreground',
        className,
      )}
      onClick={() => onToggle(sortKey)}
    >
      <span className="inline-flex items-center gap-1">
        {label}
        <span aria-hidden="true" className={active ? '' : 'opacity-30'}>
          {active && sort.dir === 'desc' ? '↓' : '↑'}
        </span>
      </span>
    </th>
  )
}

/** 非排序表头（新栈统一密度：与 SortTh 同 padding/字号） */
export function Th({ label, title, className, children }: { label?: string; title?: string; className?: string; children?: React.ReactNode }) {
  return (
    <th scope="col" title={title} className={cn('whitespace-nowrap px-3 py-2 text-left text-aux font-medium text-muted-foreground', className)}>
      {label ?? children}
    </th>
  )
}
