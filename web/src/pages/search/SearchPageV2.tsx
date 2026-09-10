// 搜索页（console-ux §6.4 / reverse §3.3——P2 新栈重写：AG Grid 结果面
// + AQL 尾缀链语义平移 + 顶栏驻留查询）。
//
// 语义承接（audit §2.3）：
// - 查询面 = 顶栏驻留输入（?q= URL 即查询状态；本页零本地查询态）；
//   AQL 模式编辑器 = 服务端查询面（⌘/Ctrl+Enter 执行；查询文本是唯一
//   事实源——排序/翻页 = 重写 .sort()/.offset()/.limit() 尾缀链再重放）；
// - 列集：选择列 + Artifact（链接）| Path | Repository | Modified 默认，
//   大小/sha256 列选器可选项（defaultHidden——localStorage per-page）；
// - 网格内快滤（name/dir/repo 客户端子串）；选择列批量复制路径；
// - AQL 表头排序注入（search-aql-sort-* 族）+ Pager 重写 offset；
//   range 流式语义（total = 本页行数）+ K63 截断通告；
// - Builds 范围页签（?scope=builds——AQL builds 入口合成）；
// - 锚族原样：search-page / search-mode(-basic|-aql) / search-scope(-…) /
//   search-aql-input / search-aql-run / search-aql-error / search-aql-
//   notification / search-count / search-grid / search-quick-filter /
//   search-quick-count / search-quick-filter-empty / search-select-all /
//   search-row-select-* / search-result-* / search-result-link-* /
//   search-columns(-menu|-item-*|-reset) / search-pager / search-aql-range
//   / search-builds-results / search-builds-row-*。
import { useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { AllCommunityModule, ModuleRegistry } from 'ag-grid-community'
import type { ColDef, GridApi, GridReadyEvent } from 'ag-grid-community'
import { AgGridReact } from 'ag-grid-react'

// 社区模块注册（v33+ 必需——行选/虚拟滚动都在社区集内）
ModuleRegistry.registerModules([AllCommunityModule])

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { agThemeBridge } from '@/features/aggrid/theme'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { Pager, PAGER_SIZE_OPTIONS, useClientPager } from '@/components/layout/pager'
import { apiJSON } from '@/lib/api'
import { useColumnPrefs } from '@/lib/columnPrefs'
import type { ColumnDef as PrefColumnDef, ColumnPrefs } from '@/lib/columnPrefs'
import { formatBytes } from '@/lib/format'
import { useAsync } from '@/lib/useAsync'
import {
  AQL_RESULT_CAP,
  formatStamp,
  nextSortClause,
  runAQL,
  semanticOf,
  sortState,
  treeUrl,
  withTailClause,
} from './aql'
import type { AQLResult, AQLRow } from './aql'
import { tr } from '@/i18n'


const t = tr('search')

const PAGE = 100

const COLUMNS: PrefColumnDef[] = [
  { id: 'name', label: t('制品'), anchor: 'search-columns-item-name' },
  { id: 'path', label: t('路径'), anchor: 'search-columns-item-path' },
  { id: 'repo', label: t('仓库'), anchor: 'search-columns-item-repo' },
  { id: 'modified', label: t('修改时间'), anchor: 'search-columns-item-modified' },
  { id: 'size', label: t('大小'), anchor: 'search-columns-item-size' },
  { id: 'sha256', label: 'sha256', anchor: 'search-columns-item-sha256' },
]
const COLUMN_IDS = COLUMNS.map((c) => c.id)
const DEFAULT_HIDDEN = ['size', 'sha256']
const COLS_KEY = 'binflow-console-cols-search'

interface SearchResult {
  repo: string
  path: string
  size: string
  lastModified?: string
  mimeType?: string
  checksums?: { sha1?: string; md5?: string; sha256?: string }
}

function searchArtifacts(name: string, signal?: AbortSignal): Promise<{ results: SearchResult[] }> {
  const params = new URLSearchParams()
  params.set('name', name)
  return apiJSON<{ results: SearchResult[] }>(`/search/artifact?${params.toString()}`, { signal })
}

/** 结果行（两模式归一形态） */
export interface ResultRow {
  key: string
  repo: string
  dir: string
  name: string
  size: number | string | null
  modified: string | null
  sha256?: string | null
  sub?: ReactNode
}

function toRow(r: SearchResult): ResultRow {
  const segs = r.path.replace(/^\//, '').split('/')
  const name = segs.pop() ?? ''
  const sub = semanticOf(r.path) ?? (r.checksums?.sha256 ? `sha256 ${r.checksums.sha256.slice(0, 12)}…` : undefined)
  return {
    key: `${r.repo}/${r.path}`,
    repo: r.repo,
    dir: segs.join('/'),
    name,
    size: r.size,
    modified: r.lastModified ?? null,
    sha256: r.checksums?.sha256 ?? null,
    sub: sub ? <span className="font-mono" lang="en">{sub}</span> : undefined,
  }
}

type SearchMode = 'basic' | 'aql'
type SearchScope = 'artifacts' | 'builds'

/** AQL builds 合成（T-512）：名/号 $match 子串，started 倒序，页窗 100 */
function buildsQueryFor(term: string): string {
  const pat = JSON.stringify(`*${term}*`)
  return `builds.find({"$or":[{"name":{"$match":${pat}}},{"number":{"$match":${pat}}}]}).include("name","number","started","repo").sort({"$desc":["started"]}).limit(100)`
}

function buildRowOf(r: AQLRow): { name: string; number: string; started: string; repo: string } {
  return {
    name: typeof r.name === 'string' ? r.name : '',
    number: String(r.number ?? ''),
    started: typeof r.started === 'string' ? r.started : '',
    repo: typeof r.repo === 'string' ? r.repo : '',
  }
}

/** 列选器（T-414/T-449——复位 = 恢复默认列集非全显） */
function ColumnsMenu({ cols }: { cols: ColumnPrefs }) {
  const [open, setOpen] = useState(false)
  const atDefault = DEFAULT_HIDDEN.every((id) => cols.hidden.has(id)) && cols.hidden.size === DEFAULT_HIDDEN.length
  return (
    <span className="filter-tail-actions filter-tail-end">
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button variant="outline" size="sm" aria-haspopup="menu" aria-expanded={open} data-testid="search-columns" title={t('自定义显示列（偏好保存在本浏览器）')}>
            <span aria-hidden="true">▤</span> {t('列')} {cols.visibleCount}/{COLUMNS.length}
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-56 p-1" align="end" data-testid="search-columns-menu">
          {COLUMNS.map((c) => {
            const visible = cols.isVisible(c.id)
            const last = visible && cols.visibleCount === 1
            return (
              <button
                key={c.id}
                type="button"
                role="menuitemcheckbox"
                aria-checked={visible}
                aria-disabled={last || undefined}
                title={last ? t('至少保留一列') : undefined}
                data-testid={c.anchor}
                className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-dense hover:bg-accent aria-disabled:opacity-50"
                onClick={() => {
                  if (!last) cols.toggle(c.id)
                }}
              >
                <span aria-hidden="true" className="col-check">{visible ? '☑' : '☐'}</span>
                {c.label}
              </button>
            )
          })}
          <div className="my-1 border-t border-border" />
          <button
            type="button"
            aria-disabled={atDefault || undefined}
            title={atDefault ? t('已是默认列集') : t('恢复默认列（大小/sha256 收回）')}
            data-testid="search-columns-reset"
            className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-dense hover:bg-accent aria-disabled:opacity-50"
            onClick={() => cols.reset()}
          >
            {t('恢复默认列')}
          </button>
        </PopoverContent>
      </Popover>
    </span>
  )
}

/** AQL 投影行 → 网格行 */
function aqlRowOf(r: AQLRow, i: number): ResultRow {
  const dir = r.path ?? ''
  const name = r.name ?? ''
  let sub: ReactNode = null
  const sem = semanticOf(dir && name ? `${dir}/${name}` : name || dir)
  if (sem) sub = <span lang="en">{sem}</span>
  else if (r.properties && r.properties.length > 0) {
    const head = r.properties.slice(0, 3).map((p) => `${p.key}=${p.value}`).join(' · ')
    const more = r.properties.length > 3 ? ` · +${r.properties.length - 3}` : ''
    sub = <span className="font-mono" lang="en">{head}{more}</span>
  } else if (r.virtual_repos && r.virtual_repos.length > 0) {
    sub = <span className="font-mono" lang="en">virtual ∈ {r.virtual_repos.join(', ')}</span>
  } else if (r.type) sub = <span lang="en">{r.type}</span>
  return {
    key: `${r.repo ?? ''}/${dir}${dir ? '/' : ''}${name}/${i}`,
    repo: r.repo ?? '',
    dir,
    name,
    size: typeof r.size === 'number' ? r.size : null,
    modified: r.modified ?? r.updated ?? null,
    sha256: r.sha256 ?? null,
    sub: sub ?? undefined,
  }
}

function errorHeadline(err: { status: number }): string {
  if (err.status === 400) return t('查询被拒绝（HTTP 400）')
  if (err.status === 408) return t('查询超时（HTTP 408）')
  if (err.status === 429) return t('并发已满（HTTP 429）')
  if (err.status >= 500 || err.status === 0) return t('服务暂不可用')
  return t('请求失败（HTTP {v1}）', { v1: err.status })
}

function errorHint(err: { status: number }): string | null {
  if (err.status === 400)
    return t('文案为服务端逐字回显——检查字段/操作符/域（BinFlow 子集：items + property 域 + build 族三入口 builds/modules/dependencies〔T-511〕，$not 与 statistics/build.promotions/releasebundle 等域不支持）与链序 include→sort→offset→limit。')
  if (err.status === 408) return t('查询超过执行上限（10s）——收窄条件或加 .limit()。')
  if (err.status === 429) return t('并发查询已达上限（4），稍后重试（服务端随 429 下发 Retry-After）。')
  return null
}

export default function SearchPageV2() {
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const q = (params.get('q') ?? '').trim()
  const mode: SearchMode = params.get('mode') === 'aql' ? 'aql' : 'basic'
  const scope: SearchScope = params.get('scope') === 'builds' ? 'builds' : 'artifacts'
  // 两查询各自持 abort 旗（共享单 ref 会让后挂载的空查询误杀前者的在飞
  // 请求——AbortError 假错误态）
  const abortRef = useRef<AbortController | null>(null)
  const buildAbortRef = useRef<AbortController | null>(null)

  const cols = useColumnPrefs(COLUMN_IDS, COLS_KEY, DEFAULT_HIDDEN)

  const results = useAsync(() => {
    abortRef.current?.abort()
    if (mode === 'aql' || scope !== 'artifacts' || q === '') return Promise.resolve(null)
    const ctrl = new AbortController()
    abortRef.current = ctrl
    return searchArtifacts(q, ctrl.signal)
  }, [q, mode, scope])

  const buildResults = useAsync(() => {
    buildAbortRef.current?.abort()
    if (mode === 'aql' || scope !== 'builds' || q === '') return Promise.resolve(null)
    const ctrl = new AbortController()
    buildAbortRef.current = ctrl
    return runAQL(buildsQueryFor(q), ctrl.signal)
  }, [q, mode, scope])

  const rows = useMemo(() => (results.data?.results ?? []).map(toRow), [results.data])
  const count = results.data?.results.length ?? 0
  const buildRows = useMemo(() => (buildResults.data?.results ?? []).map(buildRowOf), [buildResults.data])
  const buildCount = buildRows.length

  const switchMode = (next: SearchMode) => {
    if (next === mode) return
    navigate(next === 'aql' ? '/search?mode=aql' : '/search', { replace: true })
  }
  const switchScope = (next: SearchScope) => {
    if (next === scope) return
    navigate(next === 'builds' ? '/search?scope=builds' : '/search', { replace: true })
  }

  return (
    <div data-testid="search-page" className="flex flex-col gap-3">
      <h2 className="search-headline text-lg font-semibold">{scope === 'builds' ? t('搜索构建') : t('搜索制品')}</h2>
      <div className="flex flex-wrap items-center gap-3">
        {mode === 'basic' && (
          <div className="flex overflow-hidden rounded-md border border-input" role="group" aria-label={t('搜索范围')} data-testid="search-scope">
            {(['artifacts', 'builds'] as const).map((s) => (
              <button
                key={s}
                type="button"
                data-testid={`search-scope-${s}`}
                aria-pressed={scope === s}
                className={`px-3 py-1 text-dense ${scope === s ? 'bg-primary text-primary-foreground' : 'hover:bg-accent'}`}
                onClick={() => switchScope(s)}
              >
                {s === 'artifacts' ? t('制品') : 'Builds'}
              </button>
            ))}
          </div>
        )}
        <div className="flex overflow-hidden rounded-md border border-input" role="group" aria-label={t('搜索模式')} data-testid="search-mode">
          {(['basic', 'aql'] as const).map((m) => (
            <button
              key={m}
              type="button"
              data-testid={`search-mode-${m}`}
              aria-pressed={mode === m}
              className={`px-3 py-1 text-dense ${mode === m ? 'bg-primary text-primary-foreground' : 'hover:bg-accent'}`}
              onClick={() => switchMode(m)}
            >
              {m === 'basic' ? t('基本') : 'AQL'}
            </button>
          ))}
        </div>
      </div>

      {mode === 'aql' ? (
        <AqlPanel columns={COLUMNS} cols={cols} toolbar={<ColumnsMenu cols={cols} />} />
      ) : scope === 'builds' ? (
        <BuildsScopePanel q={q} results={buildResults} count={buildCount} />
      ) : (
        <>
          {q !== '' && results.status === 'ok' && (
            <p className="search-count text-dense text-muted-foreground" data-testid="search-count">
              {t('搜索结果 –')} {count} {t('项')}
            </p>
          )}
          {q === '' && (
            <p className="text-2 search-sub text-dense text-muted-foreground">
              {t('查询在顶栏驻留：上方搜索框输入名称/路径子串并 Enter（⌘K 或 / 可从任意页跳入），结果在此呈现并按你的路径 ACL 过滤。checksum 反查（sha256/sha1/md5）暂未接入 UI——见 CLI 文档。')}
            </p>
          )}
          {!(q !== '' && results.status === 'ok' && count > 0) && (
            <div className="search-grid-bar flex justify-end">
              <ColumnsMenu cols={cols} />
            </div>
          )}
          {q === '' ? (
            <EmptyState
              illustration
              message={t('在顶栏输入关键词开始搜索')}
              hint={t('顶栏搜索框（⌘K）输入子串（如 libcore、acme/app、1.0.3）后回车——空关键词不发起查询；网格内快滤可再窄化已取回的结果。')}
            />
          ) : results.status === 'loading' ? (
            <StateSkeleton lines={8} />
          ) : results.status === 'error' && results.error ? (
            <ErrorCard error={results.error} onRetry={results.reload} />
          ) : count === 0 ? (
            <EmptyState
              illustration
              message={t('没有匹配「{q}」的制品', { q })}
              hint={t('检查拼写或换更短的子串；结果按你的权限过滤。')}
            />
          ) : (
            <ResultsGrid
              rows={rows}
              columns={COLUMNS}
              cols={cols}
              toolbarRight={<ColumnsMenu cols={cols} />}
              pageSize={PAGE}
              sort={null}
              sortFieldOf={undefined}
              onSort={undefined}
            />
          )}
        </>
      )}
    </div>
  )
}

// ---- 结果网格（AG Grid client-side——两模式共用）----

function ResultsGrid({
  rows,
  columns,
  cols,
  toolbarRight,
  pageSize,
  footer,
  sort,
  sortFieldOf,
  onSort,
}: {
  rows: ResultRow[]
  columns: PrefColumnDef[]
  cols: ColumnPrefs
  toolbarRight?: ReactNode
  pageSize?: number
  footer?: ReactNode
  sort?: { field: string; dir: 'asc' | 'desc' } | null
  sortFieldOf?: (colId: string) => string | undefined
  onSort?: (field: string) => void
}) {
  const [filter, setFilter] = useState('')
  const navigate = useNavigate()
  // v36 Theming API：token 桥接主题（features/aggrid/theme——参数全为
  // var(--bf-*) 引用，深浅随 [data-theme] 活动解析）
  const agTheme = agThemeBridge
  const gridApiRef = useRef<GridApi | null>(null)
  const needle = filter.trim().toLowerCase()
  const filtered = useMemo(
    () =>
      needle === ''
        ? rows
        : rows.filter((r) => `${r.name}\n${r.dir}\n${r.repo}`.toLowerCase().includes(needle)),
    [rows, needle],
  )
  const epoch = useMemo(() => ({ needle, rows }), [needle, rows])
  const pager = useClientPager(filtered.length, epoch, pageSize)
  const shown = pageSize === undefined ? filtered : pager.slice(filtered)
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const [copied, setCopied] = useState(false)
  const selectedRows = rows.filter((r) => selected.has(r.key))

  const onGridReady = (e: GridReadyEvent) => {
    gridApiRef.current = e.api
  }

  const columnDefs = useMemo<ColDef[]>(() => {
    const visible = columns.filter((c) => cols.isVisible(c.id))
    return [
      // 选择列（自定义 checkbox——锚族 search-select-all / search-row-select-<i>
      // 自 t449 冻结；AG Grid 内建 selection checkbox 不承载锚，故自渲染）
      {
        colId: '__sel',
        headerName: '',
        pinned: 'left',
        width: 44,
        sortable: false,
        resizable: false,
        filter: false,
        suppressMovable: true,
        headerComponent: SelectAllHeader,
        cellRenderer: RowSelectCell,
      } as ColDef,
      ...visible.map((c) => {
      const def: ColDef = { colId: c.id, headerName: c.label, sortable: false }
      if (sortFieldOf && onSort) {
        const field = sortFieldOf(c.id)
        if (field !== undefined) {
          const hit = sort && sort.field === field ? sort : null
          def.headerName = c.label
          def.headerComponentParams = { innerHeaderComponent: undefined }
          def.sortable = false
          def.headerValueGetter = () => c.label
          // 表头排序 = 尾缀链重写（自定义表头钮）
          def.headerComponent = HeaderSortButton
          def.headerComponentParams = {
            label: c.label,
            active: Boolean(hit),
            dir: hit?.dir,
            onClick: () => onSort(field),
            testid: `search-aql-sort-${c.id}`,
            title: t("点击注入/切换 .sort() 子句（字段须在查询输出集内——include('*') 时恒可用）"),
          }
        }
      }
      switch (c.id) {
        case 'name':
          def.cellRenderer = (p: { data?: ResultRow; node?: { rowIndex?: number | null } }) => {
            const r = p.data
            if (!r) return null
            const href = treeUrl(r.repo, r.dir, r.name)
            // v36 cellRenderer 参数面无 rowIndex（实测恒 undefined → 全行
            // search-result-0 重复锚）——行位经 node?.rowIndex 取
            const idx = p.node?.rowIndex ?? 0
            return (
              <span className="flex min-w-0 flex-col" data-testid={`search-result-${idx}`}>
                {href ? (
                  <a
                    href={href}
                    onClick={(e) => {
                      e.preventDefault()
                      navigate(href)
                    }}
                    className="search-result-link row-link truncate font-mono font-medium text-primary dark:text-info hover:underline"
                    data-testid={`search-result-link-${idx}`}
                    lang="en"
                    title={t('在跨仓树中定位（路径段深链）')}
                  >
                    {r.name}
                  </a>
                ) : (
                  <span className="truncate font-mono" lang="en">{r.name || '—'}</span>
                )}
                {r.sub && <span className="search-result-sub text-aux text-muted-foreground">{r.sub}</span>}
              </span>
            )
          }
          def.flex = 2
          def.minWidth = 220
          break
        case 'path':
          def.valueGetter = (p) => (p.data as ResultRow)?.dir || '—'
          def.cellClass = 'font-mono'
          def.flex = 2
          def.minWidth = 200
          break
        case 'repo':
          def.valueGetter = (p) => (p.data as ResultRow)?.repo || '—'
          def.cellClass = 'font-mono'
          def.width = 160
          break
        case 'modified':
          def.valueGetter = (p) => formatStamp((p.data as ResultRow)?.modified) ?? '—'
          def.cellClass = 'font-mono'
          def.width = 210
          break
        case 'size':
          def.valueGetter = (p) => {
            const r = p.data as ResultRow | undefined
            return r?.size === null || r?.size === undefined || r?.size === '' ? '—' : formatBytes(Number(r.size) || 0)
          }
          def.cellClass = 'font-mono'
          def.width = 110
          break
        case 'sha256':
          def.cellRenderer = (p: { data?: ResultRow; value?: string }) => {
            const r = p.data
            if (!r?.sha256) return <span className="font-mono">—</span>
            return (
              <span className="flex items-center font-mono" title={r.sha256}>
                {`${r.sha256.slice(0, 10)}…`}
                <CopyButton value={r.sha256} label={`sha256 ${r.name}`} />
              </span>
            )
          }
          def.width = 170
          break
      }
      return def
    })]
  }, [columns, cols, sort, sortFieldOf, onSort, navigate])

  const noMatch = needle !== '' && filtered.length === 0

  const copySelection = async () => {
    const text = selectedRows.map((r) => [r.repo, r.dir, r.name].filter((s) => s !== '').join('/')).join('\n')
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
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1500)
  }

  return (
    <div data-testid="search-grid" className="flex flex-col gap-2">
      <div className="search-grid-bar flex flex-wrap items-center gap-2">
        <Input
          type="search"
          placeholder={t('快滤结果（名称/路径/仓库）')}
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="w-[240px] font-mono"
          data-testid="search-quick-filter"
          aria-label={t('快滤当前结果')}
        />
        {needle !== '' && !noMatch && (
          <span className="text-2 search-filter-count text-aux text-muted-foreground" data-testid="search-quick-count">
            {t('快滤命中')} {filtered.length} / {rows.length}
          </span>
        )}
        {selectedRows.length > 0 && (
          <span className="search-selection flex items-center gap-2">
            <span className="text-2 text-aux text-muted-foreground">{t('已选')} {selectedRows.length} {t('项')}</span>
            <Button
              variant="outline"
              size="sm"
              data-testid="search-selection-copy"
              title={t('复制全部选中行的 repo 路径（每行一条）')}
              onClick={() => void copySelection()}
            >
              {copied ? t('已复制 ✓') : t('复制路径（{v1}）', { v1: selectedRows.length })}
            </Button>
          </span>
        )}
        <span className="filter-tail-actions filter-tail-end ml-auto">{toolbarRight}</span>
      </div>

      {noMatch ? (
        <p className="text-2 search-nomatch text-dense text-muted-foreground" data-testid="search-quick-filter-empty">
          {t('快滤「')}{filter.trim()}{t('」无匹配行——清空快滤恢复')} {rows.length} {t('项结果。')}
        </p>
      ) : (
        <div className="h-[560px]">
          <AgGridReact
            theme={agTheme}
            columnDefs={columnDefs}
            rowData={shown}
            onGridReady={onGridReady}
            rowSelection={{ mode: 'multiRow', checkboxes: false, headerCheckbox: false, enableClickSelection: false }}
            selectionColumnDef={{ pinned: 'left', width: 44 }}
            getRowId={(p) => String((p.data as ResultRow)?.key ?? '')}
            headerHeight={36}
            rowHeight={34}
            ensureDomOrder
            enableCellTextSelection
            onSelectionChanged={(e) => {
              setSelected(new Set(e.api.getSelectedRows().map((r) => (r as ResultRow).key)))
            }}
          />
        </div>
      )}

      {pageSize !== undefined && !noMatch && (
        <div className="search-footer" data-testid="search-pager">
          <Pager
            page={pager.page}
            pageCount={pager.pageCount}
            onPageChange={pager.setPage}
            from={pager.from}
            to={pager.to}
            total={filtered.length}
            note={needle !== '' ? t('（快滤自 {v1}）', { v1: rows.length }) : undefined}
            pageSize={pager.size}
            onPageSizeChange={pager.setSize}
          />
        </div>
      )}
      {footer}
    </div>
  )
}

/** 选择列表头（全选 checkbox——锚 search-select-all，t449 冻结） */
function SelectAllHeader(props: { api: GridApi }) {
  const [all, setAll] = useState(false)
  return (
    <input
      type="checkbox"
      data-testid="search-select-all"
      aria-label={t('全选本页结果')}
      checked={all}
      onChange={() => {
        const next = !all
        setAll(next)
        if (next) props.api.selectAllFiltered()
        else props.api.deselectAll()
      }}
    />
  )
}

/** 选择列行 checkbox（锚 search-row-select-<i>，t449 冻结——AG Grid 内建
 * selection checkbox 不承载锚，故自渲染并驱动 node.setSelected） */
function RowSelectCell(props: { node?: { rowIndex?: number; isSelected: () => boolean; setSelected: (v: boolean) => void } }) {
  const node = props.node
  const [, force] = useState(0)
  if (!node) return null
  const idx = node.rowIndex ?? 0
  const sel = node.isSelected()
  return (
    <input
      type="checkbox"
      data-testid={`search-row-select-${idx}`}
      aria-label={t('选择第 {v1} 行', { v1: idx + 1 })}
      checked={sel}
      onChange={() => {
        node.setSelected(!sel)
        force((n) => n + 1)
      }}
      onClick={(e) => e.stopPropagation()}
    />
  )
}

/** AQL 表头排序钮（尾缀链重写交互——自定义表头组件） */
function HeaderSortButton(props: {
  label: string
  active: boolean
  dir?: 'asc' | 'desc'
  onClick: () => void
  testid: string
  title: string
}) {
  return (
    <button
      type="button"
      data-testid={props.testid}
      title={props.title}
      onClick={(e) => {
        e.stopPropagation()
        props.onClick()
      }}
      className="flex items-center gap-1 text-left text-dense font-medium hover:text-foreground"
      aria-sort={props.active ? (props.dir === 'asc' ? 'ascending' : 'descending') : 'none'}
    >
      {props.label}
      <span aria-hidden="true" className={props.active ? '' : 'opacity-30'}>
        {props.dir === 'desc' ? '↓' : '↑'}
      </span>
    </button>
  )
}

// ---- AQL 面板 ----

function AqlPanel({ columns, cols, toolbar }: { columns: PrefColumnDef[]; cols: ColumnPrefs; toolbar: ReactNode }) {
  const [text, setText] = useState('')
  const [submitted, setSubmitted] = useState<string | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  const res = useAsync(() => {
    abortRef.current?.abort()
    if (submitted === null) return Promise.resolve(null)
    const ctrl = new AbortController()
    abortRef.current = ctrl
    return runAQL(submitted, ctrl.signal)
  }, [submitted])

  const rows = useMemo(() => (res.data?.results ?? []).map(aqlRowOf), [res.data])
  const range = res.data?.range
  const notRun = submitted === null
  const sort = sortState(text.trim())

  const run = (query: string) => {
    const t2 = query.trim()
    if (t2 === '') return
    setText(t2)
    setSubmitted(t2)
  }
  const onSort = (field: string) => {
    run(withTailClause(text.trim(), 'sort', nextSortClause(sort, field)))
  }
  const pageTo = (offset: number) => {
    run(withTailClause(text.trim(), 'offset', String(offset)))
  }

  const pageSize = range?.limit ?? AQL_RESULT_CAP
  const page = range ? Math.floor(range.start_pos / pageSize) + 1 : 1
  const hasMore = range
    ? range.notification !== undefined ||
      (range.limit !== undefined ? rows.length === range.limit : rows.length >= AQL_RESULT_CAP)
    : false
  const pageCount = hasMore ? page + 1 : page
  const pageFrom = range && rows.length > 0 ? range.start_pos + 1 : 0
  const pageTo2 = range ? range.start_pos + rows.length : 0
  const sizeOptions = useMemo(() => [...new Set([...PAGER_SIZE_OPTIONS, pageSize])].sort((a, b) => a - b), [pageSize])
  const failed = res.status === 'error' || res.status === 'forbidden' ? res.error : null
  const gridOn = !notRun && !failed && res.status === 'ok' && rows.length > 0 && Boolean(range)

  return (
    <>
      <div className="aql-bar flex items-start gap-2">
        <textarea
          placeholder='items.find({"repo":"libs-release-local"}).include("*").sort({"$desc":["modified"]}).limit(50)'
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
              e.preventDefault()
              run(text)
            }
          }}
          data-testid="search-aql-input"
          aria-label={t('AQL 查询')}
          className="mono form-field min-h-[72px] flex-1 rounded-md border border-input bg-surface-1 p-2 font-mono text-dense outline-none focus-visible:border-ring"
          spellCheck={false}
          lang="en"
          rows={3}
        />
        <Button data-testid="search-aql-run" title={t('执行查询（⌘/Ctrl+Enter）')} disabled={text.trim() === ''} onClick={() => run(text)}>
          {t('执行')}
        </Button>
      </div>
      <div className="aql-tail">
        <p className="text-2 search-sub text-dense text-muted-foreground">
          {t('BinFlow AQL 子集：items 域 + property 域（')}{'{'}
          <span className="font-mono" lang="en">&quot;@key&quot;:&quot;value&quot;</span>
          {'}'}{t('）+ build 族三入口 builds/modules/dependencies（T-511 起）；操作符 $eq/$ne/$gt/$gte/$lt/$lte/$match/$nmatch/$and/$or/$msp/$last/$before。 未支持域（statistics/build.promotions/releasebundle…）与语法错 → 400 逐字文案。分页/排序由查询的')}{' '}
          <span className="font-mono" lang="en">.sort()/.offset()/.limit()</span>{' '}{t('尾缀承载——表头排序与分页控件（页码/每页行数）会改写查询文本。')}
        </p>
        {!gridOn && toolbar}
      </div>

      {failed && (
        <div data-testid="search-aql-error" role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-dense">
          <div className="font-medium text-destructive">{errorHeadline(failed)}</div>
          {failed.message && <div className="mt-1 break-all font-mono text-aux" lang="en">{failed.message}</div>}
          {errorHint(failed) && <div className="mt-1 text-aux text-muted-foreground">{errorHint(failed)}</div>}
        </div>
      )}

      {range?.notification && (
        <div data-testid="search-aql-notification" className="rounded-md border border-warning/50 bg-warning/10 px-3 py-2 text-dense">
          <span lang="en">{range.notification}</span>
          <div className="text-aux text-muted-foreground">{t('已按结果上限截断——用 .offset()/.limit() 分页继续取全量。')}</div>
        </div>
      )}

      {!notRun && res.status === 'ok' && (
        <p className="search-count text-dense text-muted-foreground" data-testid="search-count">
          {t('AQL 结果 –')} {rows.length} {t('行')}
        </p>
      )}

      {notRun ? (
        <EmptyState
          illustration
          message={t('输入 AQL 查询并执行')}
          hint={t('如 items.find({"repo":"<repo-key>"}).include("*").limit(10)——未命中的仓 key 也回 200 空集（无存在性泄漏）。')}
        />
      ) : res.status === 'loading' ? (
        <StateSkeleton lines={8} />
      ) : failed ? null : rows.length === 0 || !range ? (
        <EmptyState
          illustration
          message={t('查询命中 0 行')}
          hint={t('未命中的仓 key / 属性条件也回 200 空集——检查仓 key、路径条件与属性键值；结果按你的权限过滤。')}
        />
      ) : (
        <ResultsGrid
          rows={rows}
          columns={columns}
          cols={cols}
          toolbarRight={toolbar}
          sort={sort}
          sortFieldOf={(id) => ({ name: 'name', path: 'path', repo: 'repo', modified: 'modified', size: 'size', sha256: 'sha256' })[id]}
          onSort={onSort}
          footer={
            <div className="search-footer flex flex-wrap items-center justify-between gap-2" data-testid="search-aql-range">
              <span className="text-aux text-muted-foreground">
                {t('range：start_pos')} {range.start_pos} {t('· 本页')} {rows.length} {t('行 · total')} {range.total}{t('（流式语义 = 本页行数，非全量计数）')}
                {range.limit !== undefined ? ` · limit ${range.limit}` : ''}
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

// ---- Builds 范围面板 ----

function BuildsScopePanel({
  q,
  results,
  count,
}: {
  q: string
  results: { status: string; data: AQLResult | null; error: { status: number; message: string } | null; reload: () => void }
  count: number
}) {
  const rows = (results.data?.results ?? []).map(buildRowOf)
  return (
    <>
      {q !== '' && results.status === 'ok' && (
        <p className="search-count text-dense text-muted-foreground" data-testid="search-count">
          {t('搜索结果 –')} {count} {t('项')}
        </p>
      )}
      {q === '' && (
        <p className="text-2 search-sub text-dense text-muted-foreground">
          {t('Builds 范围：顶栏输入构建名或 run 号子串并 Enter——结果为 build run 行（按你的 build 读权限过滤），点击行进 run 详情。')}
        </p>
      )}
      {q === '' ? (
        <EmptyState
          illustration
          message={t('在顶栏输入关键词开始搜索')}
          hint={t('顶栏搜索框（⌘K）输入构建名/run 号子串（如 myapp、42）后回车——空关键词不发起查询；AQL 模式可手写 builds.find 查询全字段。')}
        />
      ) : results.status === 'loading' ? (
        <StateSkeleton lines={8} />
      ) : (results.status === 'error' || results.status === 'forbidden') && results.error ? (
        <ErrorCard error={results.error} onRetry={results.reload} />
      ) : count === 0 ? (
        <EmptyState
          illustration
          message={t('没有匹配「{q}」的构建', { q })}
          hint={t('检查拼写或换更短的子串；结果按你的 build 读权限过滤（r(buildRepo, buildName)）。')}
        />
      ) : (
        <section className="card section rounded-md border border-border bg-surface-1 p-0" data-testid="search-builds-results">
          <table className="w-full text-dense">
            <thead>
              <tr className="border-b border-border text-left text-aux text-muted-foreground">
                <th scope="col" className="px-3 py-2 font-medium">{t('构建名')}</th>
                <th scope="col" className="px-3 py-2 font-medium">{t('run 号')}</th>
                <th scope="col" className="px-3 py-2 font-medium">{t('启动时间')}</th>
                <th scope="col" className="px-3 py-2 font-medium">{t('构建仓')}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r, i) => {
                const to = `/builds/${encodeURIComponent(r.name)}/${encodeURIComponent(r.number)}${r.started ? `?started=${encodeURIComponent(r.started)}` : ''}`
                return (
                  <tr key={`${r.name}|${r.number}|${r.started}|${i}`} className="border-b border-border/60 hover:bg-accent" data-testid={`search-builds-row-${i}`}>
                    <td className="px-3 py-1.5">
                      <Link className="row-link font-mono text-primary hover:underline" lang="en" to={to}>{r.name}</Link>
                    </td>
                    <td className="px-3 py-1.5">
                      <Link className="row-link font-mono text-primary hover:underline" lang="en" to={to}>{r.number}</Link>
                    </td>
                    <td className="px-3 py-1.5">
                      <span className="font-mono" lang="en" title={r.started}>{formatStamp(r.started) ?? r.started ?? '—'}</span>
                    </td>
                    <td className="px-3 py-1.5 font-mono" lang="en">{r.repo || '—'}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </section>
      )}
    </>
  )
}
