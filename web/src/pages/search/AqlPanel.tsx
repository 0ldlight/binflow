import { useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'

import Alert from '@mui/material/Alert'
import AlertTitle from '@mui/material/AlertTitle'
import Button from '@mui/material/Button'
import MuiSkeleton from '@mui/material/Skeleton'
import TextField from '@mui/material/TextField'

import { EmptyState } from '../../components/EmptyState'
import { Pager, PAGER_SIZE_OPTIONS } from '../../components/Pager'
import type { ApiError } from '../../lib/api'
import type { ColumnDef, ColumnPrefs } from '../../lib/columnPrefs'
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
import { ResultsTable } from './ResultsTable'
import type { ResultRow } from './ResultsTable'
import { tr } from '../../i18n'

const tt = tr('search')

// AQL 模式面板（T-419，FR-135.1；T-449 起结果网格归一 ResultsTable）：
//
// - 编辑器 = mono textarea，⌘/Ctrl+Enter 或「执行」提交；查询文本是唯一
//   事实源——分页/排序交互 = 重写文本中的尾缀链段后重放（用户看得见
//   查询变了什么，无影子状态）。
// - **AQL 编辑器共存形态（T-449 / B-2.13 票内设计）**：AQL 模式的查询面
//   = 本编辑器（服务端查询）；顶栏驻留输入保持全局基本检索入口（在 AQL
//   模式 Enter = 切回基本模式跑 ?q=）；网格内快滤窄化已取回行——两层
//   正交，编辑器重放不清快滤（快滤是呈现面偏好非查询段）。
// - 结果表 = 共享 ResultsTable（T-449 列框架收敛：选择列 + Artifact 链接
//   |Path|Repository|Modified 默认，大小/sha256 列选器可选项；行体 inert
//   仅 name 深链）。AQL 行是投影（include 决定字段集）——缺省字段如实
//   呈现 —，不伪造。
// - 语法错内联呈现（T-419 AC1）：400 E-01 文案由统一层透传（ApiError.message
//   = errors[0].message 逐字），Alert 就地展开 + 状态相关提示；408/429
//   同一承载（文案同源，提示分流）。
// - range 消费：start_pos/limit 回显 + 流式语义注记（total = 本页行数，
//   非全量计数——aql.md §3.2）；「还有下一页」的判定只吃两个诚实信号
//   ——截断通告（notification）或本页满窗。T-451 起翻页 = 共享 Pager
//   （页码/首末页/每页行数——E2 翻案 §11.2 形态锚）；offset/limit 仍由
//   尾缀链重写承载（查询文本是唯一事实源，语义 C 注：呈现对齐语义自有）。

/** 列 id → AQL 排序字段（表头排序注入的段值；字段须在查询输出集内，
 *  否则服务端 400「Only the result fields are allowed…」——内联呈现）。 */
const SORT_FIELD: Record<string, string> = {
  name: 'name',
  path: 'path',
  repo: 'repo',
  modified: 'modified',
  size: 'size',
  sha256: 'sha256',
}

const PLACEHOLDER =
  'items.find({"repo":"libs-release-local"}).include("*").sort({"$desc":["modified"]}).limit(50)'

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

/** AQL 投影行 → 网格行（缺 repo/name 的行 name 列如实 — 且无深链） */
function toRow(r: AQLRow, i: number): ResultRow {
  const dir = r.path ?? ''
  const name = r.name ?? ''
  return {
    key: `${r.repo ?? ''}/${dir}${dir ? '/' : ''}${name}/${i}`,
    repo: r.repo ?? '',
    dir,
    name,
    size: typeof r.size === 'number' ? r.size : null,
    modified: r.modified ?? r.updated ?? null,
    sha256: r.sha256 ?? null,
    sub: rowSub(r, dir && name ? `${dir}/${name}` : name || dir),
  }
}

function errorHeadline(err: ApiError): string {
  if (err.status === 400) return tt('查询被拒绝（HTTP 400）')
  if (err.status === 408) return tt('查询超时（HTTP 408）')
  if (err.status === 429) return tt('并发已满（HTTP 429）')
  if (err.status >= 500 || err.status === 0) return tt('服务暂不可用')
  return tt('请求失败（HTTP {v1}）', { v1: err.status })
}

function errorHint(err: ApiError): string | null {
  if (err.status === 400)
    return tt('文案为服务端逐字回显——检查字段/操作符/域（BinFlow 子集：items + property 域，$not 与 builds/statistics 等域不支持）与链序 include→sort→offset→limit。')
  if (err.status === 408) return tt('查询超过执行上限（10s）——收窄条件或加 .limit()。')
  if (err.status === 429) return tt('并发查询已达上限（4），稍后重试（服务端随 429 下发 Retry-After）。')
  return null
}

export function AqlPanel({
  columns,
  cols,
  toolbar,
}: {
  columns: ColumnDef[]
  cols: ColumnPrefs
  /** 工具行右槽（列选器——SearchPage 注入共用一份壳） */
  toolbar: ReactNode
}) {
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

  const rows = useMemo(() => (res.data?.results ?? []).map(toRow), [res.data])
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

  // 页窗语义（T-451 / E2 翻案）：页大小 = 查询声明的 .limit()，未声明 =
  // 引擎上限 1000；页码 N → .offset((N-1)×size) 重写查询文本重放（查询
  // 文本是唯一事实源，无影子状态）。页数 = 前沿 + 1（total 是流式语义
  // = 本页行数非全量计数——末页未知，页码逐页揭示）。
  const pageSize = range?.limit ?? AQL_RESULT_CAP
  const page = range ? Math.floor(range.start_pos / pageSize) + 1 : 1
  const hasMore = range
    ? range.notification !== undefined ||
      (range.limit !== undefined ? rows.length === range.limit : rows.length >= AQL_RESULT_CAP)
    : false
  const pageCount = hasMore ? page + 1 : page
  const pageFrom = range && rows.length > 0 ? range.start_pos + 1 : 0
  const pageTo2 = range ? range.start_pos + rows.length : 0
  // 档位 = 冻结档 ∪ 当前查询声明的 limit（手写非档值〔如 .limit(2)〕如实
  // 在场——选择器不得对当前值显示空白）
  const sizeOptions = useMemo(
    () => [...new Set([...PAGER_SIZE_OPTIONS, pageSize])].sort((a, b) => a - b),
    [pageSize],
  )
  const failed = res.status === 'error' || res.status === 'forbidden' ? res.error : null
  // 结果网格在场 = 已执行且命中（列选器随网格工具行；否则尾行承载）
  const gridOn = !notRun && !failed && res.status === 'ok' && rows.length > 0 && Boolean(range)

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
              'aria-label': tt('AQL 查询'),
              className: 'mono',
              spellCheck: false,
              lang: 'en',
            },
          }}
        />
        <Button
          variant="contained"
          data-testid="search-aql-run"
          title={tt('执行查询（⌘/Ctrl+Enter）')}
          disabled={text.trim() === ''}
          onClick={() => run(text)}
          sx={{ alignSelf: 'flex-start', mt: '2px' }}
        >{tt('执行')}        </Button>
      </div>
      <div className="aql-tail">
        <p className="text-2 search-sub">{tt('BinFlow AQL 子集：items 域 + property 域（')}{'{'}
          <span className="mono" lang="en">
            &quot;@key&quot;:&quot;value&quot;
          </span>
          {'}'}{tt('）；操作符 $eq/$ne/$gt/$gte/$lt/$lte/$match/$nmatch/$and/$or/$msp/$last/$before。 未支持域（builds/statistics…）与语法错 → 400 逐字文案。分页/排序由查询的')}          <span className="mono" lang="en">
            {' '}
            .sort()/.offset()/.limit()
          </span>{' '}{tt('尾缀承载——表头排序与分页控件（页码/每页行数）会改写查询文本。')}        </p>
        {/* 列选器：结果网格不在场时由尾行承载（偏好预设定案，T-414——
            同一份壳，网格在场时改驻网格工具行，同一时刻仅一处） */}
        {!gridOn && toolbar}
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
          <div className="text-2">{tt('已按结果上限截断——用 .offset()/.limit() 分页继续取全量。')}</div>
        </Alert>
      )}

      {!notRun && res.status === 'ok' && (
        <p className="search-count" data-testid="search-count">{tt('AQL 结果 –')} {rows.length} {tt('行')}        </p>
      )}

      {notRun ? (
        <EmptyState
          illustration
          message={tt('输入 AQL 查询并执行')}
          hint={tt('如 items.find({"repo":"<repo-key>"}).include("*").limit(10)——未命中的仓 key 也回 200 空集（无存在性泄漏）。')}
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
          message={tt('查询命中 0 行')}
          hint={tt('未命中的仓 key / 属性条件也回 200 空集——检查仓 key、路径条件与属性键值；结果按你的权限过滤。')}
        />
      ) : (
        <ResultsTable
          rows={rows}
          columns={columns}
          cols={cols}
          toolbarRight={toolbar}
          sort={sort}
          sortFieldOf={(id) => SORT_FIELD[id]}
          onSort={onSort}
          footer={
            <div className="search-footer" data-testid="search-aql-range">
              <span>{tt('range：start_pos')} {range.start_pos} {tt('· 本页')} {rows.length} {tt('行 · total')} {range.total}{tt('（流式语义 = 本页行数，非全量计数）')}                {range.limit !== undefined ? ` · limit ${range.limit}` : ''}
              </span>
              <Pager
                page={page}
                pageCount={pageCount}
                onPageChange={(p) => pageTo((p - 1) * pageSize)}
                from={pageFrom}
                to={pageTo2}
                total={null}
                lastUnknown
                pageSize={pageSize}
                sizeOptions={sizeOptions}
                onPageSizeChange={(n) => {
                  // 换页大小 = 重写 .limit() 并回第 1 页（清 .offset——
                  // 旧 offset 在新页大小下指向错位窗口）
                  run(withTailClause(withTailClause(text.trim(), 'limit', String(n)), 'offset', null))
                }}
              />
            </div>
          }
        />
      )}
    </>
  )
}
