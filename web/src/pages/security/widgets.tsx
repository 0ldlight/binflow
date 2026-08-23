import { useState } from 'react'
import { Link } from 'react-router-dom'

import { PERM_ACTIONS } from './api'
import type { PermAction, PrincipalGrantRow } from './api'

// 用户/组页共用小件（T-237，area 目录内助手——与 TransferBox 同批）：
//   SortTh / useTableSort  C1 列头排序（§4.7：点击循环 asc → desc → none）
//   PermSummaryTable       只读权限矩阵（§6.9[5]/§6.10：r/w/d/m 四列，manage
//                          头带「不隐含读写删」说明——§7.2）

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

/** 排序表头：button 承载点击与键盘，aria-sort 同步（§8 语义标签） */
export function SortTh<K extends string>({
  label,
  sortKey,
  sort,
  onToggle,
  testid,
}: {
  label: string
  sortKey: K
  sort: SortState<K>
  onToggle: (key: K) => void
  testid: string
}) {
  const active = sort.key === sortKey
  const ariaSort = active ? (sort.dir === 'asc' ? 'ascending' : 'descending') : 'none'
  return (
    // 点击承载在 th 上（热区 = 整格；内部 button 的 click 冒泡到 th，
    // 键盘 Enter/Space 仍经 button 触发——同一冒泡路径，不双发）
    <th scope="col" aria-sort={ariaSort} data-testid={testid} onClick={() => onToggle(sortKey)}>
      <button type="button" className="th-sort">
        {label}
        <span className="th-sort-arrow" aria-hidden="true">
          {active ? (sort.dir === 'asc' ? ' ▲' : ' ▼') : ''}
        </span>
      </button>
    </th>
  )
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

/** 只读权限矩阵（§6.9[5]）：Permission Name │ 应用途径 │ r/w/d/m。
 *  行 = target；来源 = 直接 + 经组（用户视角）或直接（组视角）。 */
export function PermSummaryTable({
  rows,
  rowTestidPrefix,
  emptyHint,
}: {
  rows: readonly PrincipalGrantRow[]
  rowTestidPrefix: string
  emptyHint: string
}) {
  if (rows.length === 0) {
    return <p className="text-muted perm-summary-empty">{emptyHint}</p>
  }
  return (
    <table className="table perm-summary" data-testid={`${rowTestidPrefix}-matrix`}>
      <thead>
        <tr>
          <th scope="col">Permission Name</th>
          <th scope="col">应用途径</th>
          {PERM_ACTIONS.map((a) => (
            <th key={a} scope="col" className="th-action" title={a === 'manage' ? 'manage = 仓库配置派生权（不隐含读写删）' : undefined}>
              {a}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((r) => (
          <tr key={r.target} data-testid={`${rowTestidPrefix}-row-${r.target}`}>
            <td>
              <Link className="row-link mono" to={`/admin/security/permissions/${encodeURIComponent(r.target)}`} lang="en">
                {r.target}
              </Link>
            </td>
            <td>
              <span className="sec-chips">
                {r.sources.map((s) =>
                  s === 'direct' ? (
                    <span key="direct" className="badge neutral">
                      直接
                    </span>
                  ) : (
                    <span key={s} className="badge neutral mono" lang="en">
                      {s}
                    </span>
                  ),
                )}
              </span>
            </td>
            {PERM_ACTIONS.map((a) => (
              <td key={a} className="td-mark">
                {r.actions.includes(a as PermAction) ? (
                  <span className="mark-on" aria-label={`${a} 已授予`}>
                    ✓
                  </span>
                ) : (
                  <span className="mark-off" aria-label={`${a} 未授予`}>
                    —
                  </span>
                )}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}
