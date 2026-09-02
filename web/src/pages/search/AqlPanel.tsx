import { useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'

import Alert from '@mui/material/Alert'
import AlertTitle from '@mui/material/AlertTitle'
import Button from '@mui/material/Button'
import MuiSkeleton from '@mui/material/Skeleton'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import TableSortLabel from '@mui/material/TableSortLabel'
import TextField from '@mui/material/TextField'

import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import type { ApiError } from '../../lib/api'
import type { ColumnDef, ColumnPrefs } from '../../lib/columnPrefs'
import { formatBytes } from '../../lib/format'
import { monoInputSx } from '../../lib/muiAtoms'
import { useAsync } from '../../lib/useAsync'

import {
  AQL_RESULT_CAP,
  nextSortClause,
  runAQL,
  semanticOf,
  sortState,
  withTailClause,
  type AQLRow,
} from './aql'

// AQL 模式面板（T-419，FR-135.1——M15-SPLIT §1.2 AC1/AC2）：
//
// - 编辑器 = mono textarea，⌘/Ctrl+Enter 或「执行」提交；查询文本是唯一
//   事实源——分页/排序交互 = 重写文本中的尾缀链段后重放（用户看得见
//   查询变了什么，无影子状态）。
// - 结果表复用既有列框架与列选器（T-414 交付面：同 COLUMNS 闭集 + 同
//   per-page 偏好键）；行锚沿 search-result-<i> 既有族。AQL 行是投影
//   （include 决定字段集）——缺省字段如实呈现 —，不伪造。
// - 语法错内联呈现（AC1）：400 E-01 文案由统一层透传（ApiError.message
//   = errors[0].message 逐字），Alert 就地展开 + 状态相关提示；408/429
//   同一承载（文案同源，提示分流）。
// - range 消费（AC1 分页语义）：start_pos/limit 回显 + 流式语义注记
//   （total = 本页行数，非全量计数——aql.md §3.2）；「还有下一页」的
//   判定只吃两个诚实信号——截断通告（notification）或本页满窗。

/** 列 id → AQL 排序字段（表头排序注入的段值；字段须在查询输出集内，
 *  否则服务端 400「Only the result fields are allowed…」——内联呈现）。 */
const SORT_FIELD: Record<string, string> = {
  repo: 'repo',
  path: 'path',
  size: 'size',
  modified: 'modified',
  sha256: 'sha256',
}

const PLACEHOLDER =
  'items.find({"repo":"libs-release-local"}).include("*").sort({"$desc":["modified"]}).limit(50)'

/** AQL path（父目录，'' = 仓根）+ name → 展示全路径（basic 模式同形） */
function displayPath(r: AQLRow): string {
  const p = r.path ?? ''
  const n = r.name ?? ''
  if (p && n) return `${p}/${n}`
  return n || p
}

/** 语义副行：maven 语义 > property 投影 > virtual 成员关系 > type */
function rowSub(r: AQLRow, full: string): ReactNode {
  const sem = semanticOf(full)
  if (sem) return <span lang="en">{sem}</span>
  if (r.properties && r.properties.length > 0) {
    const head = r.properties
      .slice(0, 3)
      .map((p) => `${p.key}=${p.value}`)
      .join(' · ')
    const more = r.properties.length > 3 ? ` · +${r.properties.length - 3}` : ''
    return (
      <span className="mono" lang="en">
        {head}
        {more}
      </span>
    )
  }
  if (r.virtual_repos && r.virtual_repos.length > 0) {
    return (
      <span className="mono" lang="en">
        virtual ∈ {r.virtual_repos.join(', ')}
      </span>
    )
  }
  if (r.type) return <span lang="en">{r.type}</span>
  return null
}

function errorHeadline(err: ApiError): string {
  if (err.status === 400) return '查询被拒绝（HTTP 400）'
  if (err.status === 408) return '查询超时（HTTP 408）'
  if (err.status === 429) return '并发已满（HTTP 429）'
  if (err.status >= 500 || err.status === 0) return '服务暂不可用'
  return `请求失败（HTTP ${err.status}）`
}

function errorHint(err: ApiError): string | null {
  if (err.status === 400)
    return '文案为服务端逐字回显——检查字段/操作符/域（BinFlow 子集：items + property 域，$not 与 builds/statistics 等域不支持）与链序 include→sort→offset→limit。'
  if (err.status === 408) return '查询超过执行上限（10s）——收窄条件或加 .limit()。'
  if (err.status === 429) return '并发查询已达上限（4），稍后重试（服务端随 429 下发 Retry-After）。'
  return null
}

export function AqlPanel({
  columns,
  cols,
  toolbar,
}: {
  columns: ColumnDef[]
  cols: ColumnPrefs
  /** 工具尾行右侧（列选器——T-414 交付面由 SearchPage 注入共用一份壳） */
  toolbar: ReactNode
}) {
  const navigate = useNavigate()
  const [text, setText] = useState('')
  // null = 尚未提交（引导态）；每次提交换新字符串（同串重提交用 reload 路径）
  const [submitted, setSubmitted] = useState<string | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  const res = useAsync(() => {
    abortRef.current?.abort()
    if (submitted === null) return Promise.resolve(null)
    const ctrl = new AbortController()
    abortRef.current = ctrl
    return runAQL(submitted, ctrl.signal)
  }, [submitted])

  const rows = res.data?.results ?? []
  const range = res.data?.range
  const notRun = submitted === null
  const sort = sortState(text.trim())

  /** 提交一条（可能被交互重写过的）查询——编辑器与执行面同步 */
  const run = (q: string) => {
    const t = q.trim()
    if (t === '') return
    setText(t)
    setSubmitted(t)
  }
  const onSort = (field: string) => {
    run(withTailClause(text.trim(), 'sort', nextSortClause(sort, field)))
  }
  const pageTo = (offset: number) => {
    run(withTailClause(text.trim(), 'offset', String(offset)))
  }

  const gotoNode = (r: AQLRow) => {
    if (!r.repo || !r.name) return // 投影缺 repo/name 的行不给注定 404 的深链
    const segs = (r.path ?? '').split('/').filter((s) => s !== '')
    const enc = segs.map((s) => encodeURIComponent(s)).join('/')
    const dirPart = enc ? `/${enc}` : ''
    navigate(`/artifacts/${encodeURIComponent(r.repo)}${dirPart}?focus=${encodeURIComponent(r.name)}`)
  }

  const pageSize = range?.limit ?? rows.length
  const hasMore = range
    ? range.notification !== undefined ||
      (range.limit !== undefined ? rows.length === range.limit : rows.length >= AQL_RESULT_CAP)
    : false
  const failed = res.status === 'error' || res.status === 'forbidden' ? res.error : null

  return (
    <>
      <div className="aql-bar">
        <TextField
          multiline
          minRows={3}
          maxRows={10}
          placeholder={PLACEHOLDER}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
              e.preventDefault()
              run(text)
            }
          }}
          sx={{ ...monoInputSx, flex: '1 1 480px' }}
          slotProps={{
            htmlInput: {
              'data-testid': 'search-aql-input',
              'aria-label': 'AQL 查询',
              className: 'mono',
              spellCheck: false,
              lang: 'en',
            },
          }}
        />
        <Button
          variant="contained"
          data-testid="search-aql-run"
          title="执行查询（⌘/Ctrl+Enter）"
          disabled={text.trim() === ''}
          onClick={() => run(text)}
          sx={{ alignSelf: 'flex-start', mt: '2px' }}
        >
          执行
        </Button>
      </div>
      <div className="aql-tail">
        <p className="text-2 search-sub">
          BinFlow AQL 子集：items 域 + property 域（{'{'}
          <span className="mono" lang="en">
            &quot;@key&quot;:&quot;value&quot;
          </span>
          {'}'}）；操作符 $eq/$ne/$gt/$gte/$lt/$lte/$match/$nmatch/$and/$or/$msp/$last/$before。
          未支持域（builds/statistics…）与语法错 → 400 逐字文案。分页/排序由查询的
          <span className="mono" lang="en">
            {' '}
            .sort()/.offset()/.limit()
          </span>{' '}
          尾缀承载——表头与翻页钮会改写查询文本。
        </p>
        {toolbar}
      </div>

      {failed && (
        <Alert severity="error" data-testid="search-aql-error" sx={{ mb: 1.5, alignItems: 'flex-start' }}>
          <AlertTitle>{errorHeadline(failed)}</AlertTitle>
          {failed.message && (
            <div className="mono" lang="en" style={{ wordBreak: 'break-all' }}>
              {failed.message}
            </div>
          )}
          {errorHint(failed) && <div className="text-2">{errorHint(failed)}</div>}
        </Alert>
      )}

      {range?.notification && (
        <Alert severity="warning" data-testid="search-aql-notification" sx={{ mb: 1.5 }}>
          <span lang="en">{range.notification}</span>
          <div className="text-2">已按结果上限截断——用 .offset()/.limit() 分页继续取全量。</div>
        </Alert>
      )}

      {!notRun && res.status === 'ok' && (
        <p className="search-count" data-testid="search-count">
          AQL 结果 – {rows.length} 行
        </p>
      )}

      {notRun ? (
        <EmptyState
          illustration
          message="输入 AQL 查询并执行"
          hint={`如 items.find({"repo":"<repo-key>"}).include("*").limit(10)——未命中的仓 key 也回 200 空集（无存在性泄漏）。`}
        />
      ) : res.status === 'loading' ? (
        <div data-testid="skeleton" aria-hidden="true" style={{ paddingTop: 8 }}>
          {Array.from({ length: 8 }, (_, i) => (
            <MuiSkeleton key={i} variant="text" width={`${88 - i * 6}%`} sx={{ my: 0.5 }} />
          ))}
        </div>
      ) : failed ? null : rows.length === 0 || !range ? (
        <EmptyState
          illustration
          message="查询命中 0 行"
          hint="未命中的仓 key / 属性条件也回 200 空集——检查仓 key、路径条件与属性键值；结果按你的权限过滤。"
        />
      ) : (
        <>
          <Table>
            <TableHead>
              <TableRow>
                {columns
                  .filter((c) => cols.isVisible(c.id))
                  .map((c) => {
                    const field = SORT_FIELD[c.id]
                    const active = sort?.field === field
                    return (
                      <TableCell
                        key={c.id}
                        component="th"
                        scope="col"
                        aria-sort={active ? (sort.dir === 'asc' ? 'ascending' : 'descending') : undefined}
                      >
                        <TableSortLabel
                          active={active}
                          direction={active ? sort.dir : undefined}
                          onClick={() => onSort(field)}
                          data-testid={`search-aql-sort-${c.id}`}
                          title="点击注入/切换 .sort() 子句（字段须在查询输出集内——include('*') 时恒可用）"
                        >
                          {c.label}
                        </TableSortLabel>
                      </TableCell>
                    )
                  })}
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.map((r, i) => {
                const full = displayPath(r)
                const sub = rowSub(r, full)
                const modified = r.modified ?? r.updated
                return (
                  <TableRow
                    key={`${r.repo ?? ''}/${full}/${i}`}
                    data-testid={`search-result-${i}`}
                    hover
                    tabIndex={0}
                    onClick={() => gotoNode(r)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') gotoNode(r)
                    }}
                  >
                    {cols.isVisible('repo') && (
                      <TableCell className="mono" lang="en">
                        {r.repo ?? '—'}
                      </TableCell>
                    )}
                    {cols.isVisible('path') && (
                      <TableCell sx={{ whiteSpace: 'normal', wordBreak: 'break-all' }}>
                        <div className="mono row-link" lang="en">
                          {full || '—'}
                        </div>
                        {sub && <div className="search-result-sub">{sub}</div>}
                      </TableCell>
                    )}
                    {cols.isVisible('size') && (
                      <TableCell className="mono">
                        {typeof r.size === 'number' ? formatBytes(r.size) : '—'}
                      </TableCell>
                    )}
                    {cols.isVisible('modified') && (
                      <TableCell className="mono">
                        {modified ? modified.replace('T', ' ').slice(0, 19) : '—'}
                      </TableCell>
                    )}
                    {cols.isVisible('sha256') && (
                      <TableCell className="mono" title={r.sha256 ?? ''}>
                        {r.sha256 ? (
                          <>
                            {`${r.sha256.slice(0, 10)}…`}
                            <CopyButton value={r.sha256} label={`sha256 ${full}`} />
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
          <div className="search-footer" data-testid="search-aql-range">
            <span>
              range：start_pos {range.start_pos} · 本页 {rows.length} 行 · total {range.total}
              （流式语义 = 本页行数，非全量计数）
              {range.limit !== undefined ? ` · limit ${range.limit}` : ''}
            </span>
            <span className="aql-pager">
              <Button
                variant="outlined"
                size="small"
                data-testid="search-aql-prev"
                disabled={range.start_pos === 0}
                onClick={() => pageTo(Math.max(0, range.start_pos - pageSize))}
              >
                上一页
              </Button>
              <Button
                variant="outlined"
                size="small"
                data-testid="search-aql-next"
                disabled={!hasMore}
                title={hasMore ? undefined : '本页未满窗且无截断标记——已到结果末尾'}
                onClick={() => pageTo(range.start_pos + rows.length)}
              >
                下一页
              </Button>
            </span>
          </div>
        </>
      )}
    </>
  )
}
