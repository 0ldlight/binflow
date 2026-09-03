import { useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import TableSortLabel from '@mui/material/TableSortLabel'
import TextField from '@mui/material/TextField'

import { CopyButton } from '../../components/CopyButton'
import type { ColumnDef, ColumnPrefs } from '../../lib/columnPrefs'
import { formatBytes } from '../../lib/format'
import { monoInputSx } from '../../lib/muiAtoms'

import { formatStamp, treeUrl } from './aql'

/** 批量复制选中行路径（CopyButton 同款降级：非安全上下文回退 execCommand） */
async function copySelection(rows: ResultRow[]): Promise<void> {
  const text = rows.map((r) => [r.repo, r.dir, r.name].filter((s) => s !== '').join('/')).join('\n')
  try {
    await navigator.clipboard.writeText(text)
  } catch {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
  }
}

// 搜索结果网格（T-449 / FR-144.6 断言反转②——「列框架收敛」）：基本与 AQL
// 两模式同一张网格，归一承载 parity 三翻正——
//
// - **B-2.11 列集归一**：默认列 = 选择列（固定）+ Artifact(name 链接) |
//   Path | Repository | Modified；大小/sha256 是列选器可选项**不默认在场**
//   （defaultHidden 语义见 lib/columnPrefs）。三源不一致（现行五列 / T-414
//   注释 / console-m8 §6.4）就此归一。
// - **B-3.14 行导航**：行体 inert（无 onClick/tabIndex），深链只在 name
//   单元格（MUI Link；URL = K67-3 路径段规范形——文件是路径末段，
//   ?focus= 发射端退役）。副行语义（maven GAV / property 投影 / virtual
//   成员）随 name 走——它描述的是制品身份不是目录。
// - **B-2.13 网格内快滤**：网格工具行的子串快滤（name/dir/repo 三域，
//   大小写不敏感、客户端过滤当前结果集——SR-01 全量返回无服务端分页，
//   客户端窄化即全量语义）。查询本体在顶栏驻留（AppShell——本表零查询
//   面）；AQL 模式编辑器与快滤共存（编辑器管服务端查询，快滤管已取回
//   行的窄化——两层正交）。
// - **选择列**：Artifactory 对位形态（批量动作入口）；BinFlow 无批量
//   端点，选择面的诚实能力 = 批量复制路径（§7.3 一键拷贝家族的行集版），
//   不伪造批量删除/下载影子入口（E1 / 零端点纪律）。
// - 分页两形态：基本模式 = 客户端切片 + 「加载更多」（pageSize 在场时，
//   search-pager/search-more 既有锚与语义维持）；AQL 模式 = footer 槽
//   （range 尾行 + .offset() 翻页，由 AqlPanel 注入）。

/** 网格行（两模式归一形态：basic 的 E-09 全路径拆 dir+name，AQL 的投影
 *  行缺字段如实 null —— 不伪造）。 */
export interface ResultRow {
  key: string
  repo: string
  /** 父目录（'' = 仓根） */
  dir: string
  name: string
  /** 字节数（basic 的 string 字段 / AQL 的 number） */
  size: number | string | null
  /** ISO 时间（formatStamp 呈现） */
  modified: string | null
  sha256?: string | null
  /** name 单元格下的语义副行（ReactNode——两模式的推导差异在调用方） */
  sub?: ReactNode
}

export interface ResultSort {
  field: string
  dir: 'asc' | 'desc'
}

export function ResultsTable({
  rows,
  columns,
  cols,
  toolbarRight,
  initialFilter = '',
  pageSize,
  footer,
  sort,
  sortFieldOf,
  onSort,
}: {
  rows: ResultRow[]
  columns: ColumnDef[]
  cols: ColumnPrefs
  /** 工具行右槽（列选器 ColumnsMenu——SearchPage 注入，两模式同一份壳；
   *  无结果态由调用方自行渲染同一份壳〔偏好预设定案，T-414〕） */
  toolbarRight?: ReactNode
  /** 快滤初值（?repos= 旧深链的兼容折入——见 SearchPage URL 模型注记） */
  initialFilter?: string
  /** 客户端切片页大小（在场 = 基本模式「加载更多」分页）；缺省 = 无分页 */
  pageSize?: number
  /** 底部槽（AQL 模式 range 尾行；与 pageSize 互斥——分页形态二选一） */
  footer?: ReactNode
  /** AQL 表头排序态（.sort() 注入交互）；缺省 = 表头纯文本（基本模式） */
  sort?: ResultSort | null
  sortFieldOf?: (colId: string) => string | undefined
  onSort?: (field: string) => void
}) {
  // 快滤（B-2.13）：客户端子串窄化——查询面（顶栏/AQL 编辑器）管服务端，
  // 快滤管已取回行；变化即回零切片页
  const [filter, setFilter] = useState(initialFilter)
  const needle = filter.trim().toLowerCase()
  const filtered = useMemo(
    () =>
      needle === ''
        ? rows
        : rows.filter((r) => `${r.name}\n${r.dir}\n${r.repo}`.toLowerCase().includes(needle)),
    [rows, needle],
  )

  // 客户端切片（基本模式；AQL 不传 pageSize 由服务端 .offset/.limit 分页）。
  // 切片回零 = 派生：epoch（快滤词 × 结果集标识）变了即回落第一页——
  // 状态里只存「某 epoch 下用户展开到的行数」，无 effect 回零（re-render
  // 派生口径，加载更多点击时随新 epoch 落盘）。rows 由调用方 useMemo
  // 稳定（结果集不变即同引用）。
  const epoch = useMemo(() => ({ needle, rows }), [needle, rows])
  const baseVisible = pageSize ?? Number.POSITIVE_INFINITY
  const [slice, setSlice] = useState<{ epoch: unknown; visible: number }>(() => ({
    epoch: undefined,
    visible: baseVisible,
  }))
  const visible = slice.epoch === epoch ? slice.visible : baseVisible
  const shown = filtered.slice(0, Number.isFinite(visible) ? visible : filtered.length)

  // 选择列：键 = row.key（repo+path 稳定身份，不受快滤重排影响）
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const [copied, setCopied] = useState(false)
  const selectedRows = rows.filter((r) => selected.has(r.key))
  const pageKeys = shown.map((r) => r.key)
  const allOnPage = pageKeys.length > 0 && pageKeys.every((k) => selected.has(k))
  const someOnPage = pageKeys.some((k) => selected.has(k))

  const toggleRow = (key: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }
  const toggleAllOnPage = () => {
    setSelected((prev) => {
      const next = new Set(prev)
      for (const k of pageKeys) {
        if (allOnPage) next.delete(k)
        else next.add(k)
      }
      return next
    })
  }

  const noMatch = needle !== '' && filtered.length === 0

  const headCell = (c: ColumnDef) => {
    const field = sortFieldOf?.(c.id)
    const hit = sort && field !== undefined && sort.field === field ? sort : null
    return (
      <TableCell key={c.id} component="th" scope="col" aria-sort={hit ? (hit.dir === 'asc' ? 'ascending' : 'descending') : undefined}>
        {onSort && field !== undefined ? (
          <TableSortLabel
            active={Boolean(hit)}
            direction={hit ? hit.dir : undefined}
            onClick={() => onSort(field)}
            data-testid={`search-aql-sort-${c.id}`}
            title="点击注入/切换 .sort() 子句（字段须在查询输出集内——include('*') 时恒可用）"
          >
            {c.label}
          </TableSortLabel>
        ) : (
          c.label
        )}
      </TableCell>
    )
  }

  return (
    <div data-testid="search-grid">
      <div className="search-grid-bar">
        <TextField
          type="search"
          size="small"
          placeholder="快滤结果（名称/路径/仓库）"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          sx={{ ...monoInputSx, width: 240 }}
          slotProps={{
            htmlInput: {
              'data-testid': 'search-quick-filter',
              'aria-label': '快滤当前结果',
              className: 'mono',
            },
          }}
        />
        {needle !== '' && !noMatch && (
          <span className="text-2 search-filter-count" data-testid="search-quick-count">
            快滤命中 {filtered.length} / {rows.length}
          </span>
        )}
        {selectedRows.length > 0 && (
          <span className="search-selection">
            <span className="text-2">已选 {selectedRows.length} 项</span>
            <Button
              variant="outlined"
              size="small"
              data-testid="search-selection-copy"
              color={copied ? 'success' : undefined}
              title="复制全部选中行的 repo 路径（每行一条）"
              onClick={() => {
                void copySelection(selectedRows).then(() => {
                  setCopied(true)
                  window.setTimeout(() => setCopied(false), 1500)
                })
              }}
            >
              {copied ? '已复制 ✓' : `复制路径（${selectedRows.length}）`}
            </Button>
          </span>
        )}
        <span className="filter-tail-actions filter-tail-end" style={{ marginLeft: 'auto' }}>
          {toolbarRight}
        </span>
      </div>

      {noMatch ? (
        <p className="text-2 search-nomatch" data-testid="search-quick-filter-empty">
          快滤「{filter.trim()}」无匹配行——清空快滤恢复 {rows.length} 项结果。
        </p>
      ) : (
        <Table>
          <TableHead>
            <TableRow>
              <TableCell component="th" scope="col" padding="checkbox">
                <Checkbox
                  size="small"
                  checked={allOnPage}
                  indeterminate={!allOnPage && someOnPage}
                  onChange={toggleAllOnPage}
                  data-testid="search-select-all"
                  slotProps={{
                    input: {
                      'aria-label': '全选当前显示的行',
                      // MUI 半选态给原生 input 打 aria-checked="mixed"——
                      // ARIA-in-HTML 对 input[type=checkbox] 禁该值（应用 DOM
                      // indeterminate 本身，MUI 已带 data-indeterminate）；
                      // 显式 undefined 键覆盖 slot 合并，斧子规则清零
                      'aria-checked': undefined,
                    },
                  }}
                />
              </TableCell>
              {columns.filter((c) => cols.isVisible(c.id)).map(headCell)}
            </TableRow>
          </TableHead>
          <TableBody>
            {shown.map((r, i) => {
              const href = treeUrl(r.repo, r.dir, r.name)
              return (
                <TableRow key={r.key} data-testid={`search-result-${i}`}>
                  <TableCell padding="checkbox">
                    <Checkbox
                      size="small"
                      checked={selected.has(r.key)}
                      onChange={() => toggleRow(r.key)}
                      data-testid={`search-row-select-${i}`}
                      slotProps={{ input: { 'aria-label': `选择 ${r.name}` } }}
                    />
                  </TableCell>
                  {cols.isVisible('name') && (
                    <TableCell sx={{ whiteSpace: 'normal', wordBreak: 'break-all' }}>
                      {href ? (
                        <Link
                          to={href}
                          className="mono row-link"
                          lang="en"
                          data-testid={`search-result-link-${i}`}
                          title="在跨仓树中定位（路径段深链）"
                        >
                          {r.name}
                        </Link>
                      ) : (
                        <span className="mono" lang="en">
                          {r.name || '—'}
                        </span>
                      )}
                      {r.sub && <div className="search-result-sub">{r.sub}</div>}
                    </TableCell>
                  )}
                  {cols.isVisible('path') && (
                    <TableCell className="mono" lang="en" sx={{ whiteSpace: 'normal', wordBreak: 'break-all' }}>
                      {r.dir || '—'}
                    </TableCell>
                  )}
                  {cols.isVisible('repo') && (
                    <TableCell className="mono" lang="en">
                      {r.repo || '—'}
                    </TableCell>
                  )}
                  {cols.isVisible('modified') && (
                    <TableCell className="mono" sx={{ whiteSpace: 'nowrap' }}>
                      {formatStamp(r.modified) ?? '—'}
                    </TableCell>
                  )}
                  {cols.isVisible('size') && (
                    <TableCell className="mono">
                      {r.size === null || r.size === undefined || r.size === '' ? '—' : formatBytes(Number(r.size) || 0)}
                    </TableCell>
                  )}
                  {cols.isVisible('sha256') && (
                    <TableCell className="mono" title={r.sha256 ?? ''}>
                      {r.sha256 ? (
                        <>
                          {`${r.sha256.slice(0, 10)}…`}
                          <CopyButton value={r.sha256} label={`sha256 ${r.name}`} />
                        </>
                      ) : (
                        '—'
                      )}
                    </TableCell>
                  )}
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      )}

      {pageSize !== undefined && !noMatch && (
        <div className="search-footer" data-testid="search-pager">
          <span>
            显示 1 – {shown.length} / 共 {filtered.length} 项
            {needle !== '' ? `（快滤自 ${rows.length}）` : ''}
          </span>
          {shown.length < filtered.length && (
            <Button
              variant="outlined"
              size="small"
              data-testid="search-more"
              onClick={() => setSlice({ epoch, visible: (Number.isFinite(visible) ? visible : 0) + (pageSize ?? 0) })}
            >
              加载更多
            </Button>
          )}
        </div>
      )}
      {footer}
    </div>
  )
}
