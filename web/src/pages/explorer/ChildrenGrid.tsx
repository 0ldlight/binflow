// Explorer children 面（AG Grid 无限行模型——社区版特性集：无限滚动 +
// 行虚拟化 + 多选；企业特性 server-side row model / 区间选择禁用，规避
// 授权——architecture §1 表格行裁定）。
//
// 语义承接（audit §2.2 children 表）：
// - 当前层 children（服务端序：目录在前 + 同组按名）无限滚动页窗
//   （cacheBlockSize=100——「加载更多」升级为真无限行模型）；
// - docker 特化列（tags 徽标 / digest 列头「摘要」）；
// - 远端派生行（remote 标记 + 点击回源）；
// - 多选 + 批量动作（copy/move UI 解锁——api/copy|move 消费 + 批量复制
//   路径 + 批量删除走危险确认）；
// - 行点击 = 目录下钻 / 文件选中（URL 即状态）；Shift+F10 右键菜单。
// - 锚族：tree-list（表根）/ tree-row-<name> / tree-load-more 退役（无限
//   滚动语义取代 load-more——锚随形态退役入册 §10.6 P2 批）。
import { useCallback, useMemo, useRef, useState } from 'react'
import { AllCommunityModule, ModuleRegistry } from 'ag-grid-community'
import type { CellClickedEvent, CellKeyDownEvent, ColDef, GridApi, GridReadyEvent, IDatasource, IGetRowsParams } from 'ag-grid-community'
import { AgGridReact } from 'ag-grid-react'

// 社区模块注册（v33+ 必需——无限行模型/行选/虚拟滚动都在社区集内）
ModuleRegistry.registerModules([AllCommunityModule])

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { EmptyState } from '@/components/layout/states'
import { agThemeBridge } from '@/features/aggrid/theme'
import { formatBytes } from '@/lib/format'
import type { ChildNode } from '@/pages/artifacts/lib'
import { tr } from '@/i18n'

import { BIG_DIR } from './model'
import type { MenuTarget } from './model'


const tt = tr('artifacts')

const PAGE = 100

export function ChildrenGrid({
  repoKey,
  nodes,
  total,
  loading,
  isDockerRepo,
  focusFile,
  readOnly,
  filter,
  onFilter,
  filesOnly,
  onFilesOnly,
  onNavigateDir,
  onSelectFile,
  onMenu,
  onCopyMove,
  onDeleteSelected,
  remoteDegraded,
  emptyState,
}: {
  repoKey: string
  nodes: ChildNode[]
  total: number
  loading: boolean
  isDockerRepo: boolean
  focusFile: string | null
  readOnly: boolean
  filter: string
  onFilter: (v: string) => void
  filesOnly: boolean
  onFilesOnly: (v: boolean) => void
  onNavigateDir: (path: string) => void
  onSelectFile: (name: string | null) => void
  onMenu: (x: number, y: number, target: MenuTarget) => void
  onCopyMove: (op: 'copy' | 'move', nodes: ChildNode[]) => void
  onDeleteSelected: (nodes: ChildNode[]) => void
  remoteDegraded?: string
  /** total=0 的空态（调用方按仓型预组装——tree-empty-dir / tree-empty-virtual 锚） */
  emptyState: React.ReactNode
}) {
  // v36 Theming API：token 桥接主题（features/aggrid/theme——参数全为
  // var(--bf-*) 引用，深浅随 [data-theme] 活动解析）
  const agTheme = agThemeBridge
  const gridApiRef = useRef<GridApi | null>(null)
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())

  const rows = useMemo(() => {
    const f = filter.trim().toLowerCase()
    return nodes
      .filter((n) => (filesOnly ? !n.folder : true))
      .filter((n) => (f ? n.name.toLowerCase().includes(f) : true))
  }, [nodes, filter, filesOnly])

  // 无限行模型数据源：页窗从已装载集切片（children 契约暂无服务端游标）
  const datasource: IDatasource = useMemo(
    () => ({
      getRows: (params: IGetRowsParams) => {
        const slice = rows.slice(params.startRow, params.endRow)
        params.successCallback(slice, rows.length)
      },
    }),
    [rows],
  )

  const onGridReady = useCallback(
    (e: GridReadyEvent) => {
      gridApiRef.current = e.api
      e.api.setGridOption('datasource', datasource)
    },
    [datasource],
  )

  // 数据源变化（切层/过滤）——重置滚动与选区
  const dsKey = `${repoKey}|${rows.length}|${filter}|${filesOnly}`
  const lastDsKey = useRef('')
  if (lastDsKey.current !== dsKey && gridApiRef.current) {
    lastDsKey.current = dsKey
    gridApiRef.current.setGridOption('datasource', datasource)
  }

  const selectedNodes = useMemo(() => nodes.filter((n) => selected.has(n.name)), [nodes, selected])

  const onCellClicked = useCallback(
    (e: CellClickedEvent) => {
      const n = e.data as ChildNode | undefined
      if (!n) return
      if (n.folder) onNavigateDir(n.path)
      else onSelectFile(n.name)
    },
    [onNavigateDir, onSelectFile],
  )

  const onCellKeyDown = useCallback(
    (e: CellKeyDownEvent) => {
      const n = e.data as ChildNode | undefined
      if (!n) return
      if (e.event instanceof KeyboardEvent && (e.event.key === 'ContextMenu' || (e.event.key === 'F10' && e.event.shiftKey))) {
        e.event.preventDefault()
        onMenu(80, (e.event.target as HTMLElement).getBoundingClientRect().top + 14, { kind: 'node', repoKey, node: n })
      } else if (e.event instanceof KeyboardEvent && e.event.key === 'Enter') {
        e.event.preventDefault()
        if (n.folder) onNavigateDir(n.path)
        else onSelectFile(n.name)
      }
    },
    [onMenu, onNavigateDir, onSelectFile, repoKey],
  )

  const columnDefs = useMemo<ColDef[]>(() => {
    const cols: ColDef[] = [
      {
        field: 'name',
        headerName: tt('名称'),
        cellClass: (p) => (p.data as ChildNode)?.folder ? 'ag-tree-dir' : 'ag-tree-file',
        cellRenderer: (p: { value?: string; data?: ChildNode }) => {
          const n = p.data
          if (!n) return null
          return (
            <span
              className="flex h-full w-full min-w-0 cursor-pointer items-center gap-1.5"
              data-testid={`tree-row-${n.name}`}
              onClick={(e) => {
                e.stopPropagation()
                if (n.folder) onNavigateDir(n.path)
                else onSelectFile(n.name)
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  if (n.folder) onNavigateDir(n.path)
                  else onSelectFile(n.name)
                }
              }}
            >
              <span aria-hidden="true">{n.folder ? '◻' : '◾'}</span>
              <span className="truncate font-mono" title={n.name}>{n.name}</span>
              {n.remote && (
                <span
                  className="rounded-sm border border-info px-1 text-[11px] text-info"
                  data-testid="tree-row-uncached"
                  title={tt('上游枚举的未缓存条目——点击将回源拉取（成功后落地缓存）')}
                >
                  {tt('远端')}
                </span>
              )}
            </span>
          )
        },
        flex: 2,
        minWidth: 220,
        sortable: false,
      },
    ]
    if (isDockerRepo) {
      cols.push({
        field: 'tags',
        headerName: tt('标签'),
        cellRenderer: (p: { data?: ChildNode }) => {
          const n = p.data
          if (!n) return null
          if (n.tags && n.tags.length > 0) {
            return (
              <span className="flex flex-wrap items-center gap-1">
                {n.tags.map((tag) => (
                  <span key={tag} className="badge neutral rounded-sm bg-secondary px-1.5 py-px text-[11px]" data-testid={`tag-badge-${tag}`} title={`tag: ${tag}`}>
                    {tag}
                  </span>
                ))}
              </span>
            )
          }
          if (!n.folder) {
            return (
              <span className="rounded-sm border border-warning px-1 text-[11px] text-warning">untagged</span>
            )
          }
          return <span className="text-muted-foreground">—</span>
        },
        width: 180,
        sortable: false,
      })
    } else {
      cols.push({
        field: 'folder',
        headerName: tt('类型'),
        valueFormatter: (p) => (p.data as ChildNode)?.folder ? tt('目录') : tt('文件'),
        width: 90,
        sortable: false,
      })
    }
    cols.push(
      {
        field: 'size',
        headerName: tt('大小'),
        valueFormatter: (p) => {
          const n = p.data as ChildNode | undefined
          return n && !n.folder && n.size !== null ? formatBytes(n.size) : '—'
        },
        cellClass: 'font-mono',
        width: 110,
        sortable: false,
      },
      {
        field: 'lastModified',
        headerName: tt('修改时间'),
        valueFormatter: (p) => {
          const n = p.data as ChildNode | undefined
          return n?.lastModified ? n.lastModified.replace('T', ' ').slice(0, 19) : '—'
        },
        cellClass: 'font-mono',
        width: 180,
        sortable: false,
      },
      {
        field: 'sha256',
        headerName: isDockerRepo ? tt('摘要') : 'sha256',
        valueFormatter: (p) => {
          const n = p.data as ChildNode | undefined
          return n?.sha256 ? `${n.sha256.slice(0, 10)}…` : '—'
        },
        tooltipValueGetter: (p) => (p.data as ChildNode | undefined)?.sha256 ?? '',
        cellClass: 'font-mono',
        flex: 1,
        minWidth: 140,
        sortable: false,
      },
    )
    return cols
  }, [isDockerRepo])

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      {/* ---- 过滤栏 + 批量动作栏 ---- */}
      <div className="filter-bar flex flex-wrap items-center gap-2">
        <Input
          type="search"
          placeholder={tt('过滤当前层（仅已加载集）…')}
          value={filter}
          onChange={(e) => onFilter(e.target.value)}
          className="w-[240px] font-mono"
          data-testid="tree-filter"
          aria-label={tt('过滤当前层')}
        />
        <Label className="flex cursor-pointer items-center gap-1.5 font-normal">
          <input
            type="checkbox"
            className="size-3.5"
            checked={filesOnly}
            onChange={(e) => onFilesOnly(e.target.checked)}
          />
          {tt('只看文件')}
        </Label>
        {total > 0 && (
          <span className="count text-aux text-muted-foreground">
            {tt('共')} {total} {tt('项')}
          </span>
        )}
        {selectedNodes.length > 0 && (
          <span className="search-selection ml-auto flex items-center gap-2">
            <span className="text-aux text-muted-foreground">{tt('已选')} {selectedNodes.length} {tt('项')}</span>
            <Button
              variant="outline"
              size="sm"
              data-testid="tree-bulk-copy"
              title={tt('批量复制选中项到目标仓库/路径（api/copy——pro 域）')}
              onClick={() => onCopyMove('copy', selectedNodes)}
            >
              {tt('复制 {v1} 项', { v1: selectedNodes.length })}
            </Button>
            <Button
              variant="outline"
              size="sm"
              data-testid="tree-bulk-move"
              title={tt('批量移动选中项到目标仓库/路径（api/move——pro 域）')}
              onClick={() => onCopyMove('move', selectedNodes)}
            >
              {tt('移动 {v1} 项', { v1: selectedNodes.length })}
            </Button>
            <Button variant="outline" size="sm" data-testid="tree-bulk-delete" disabled={readOnly} onClick={() => onDeleteSelected([...selectedNodes])}>
              {tt('删除')}
            </Button>
          </span>
        )}
      </div>

      {remoteDegraded && (
        <div className="warn-box rounded-md border border-warning/50 bg-warning/10 px-3 py-2 text-dense" data-testid="tree-remote-degraded" title={remoteDegraded}>
          {tt('⚠ 远端枚举不可用（上游故障或 assumed-offline 静默期）——已缓存条目仍可用；未缓存条目暂不可见。')}
          <span className="ml-1 font-mono text-aux" lang="en">{remoteDegraded}</span>
        </div>
      )}

      {total > BIG_DIR && (
        <div className="warn-box rounded-md border border-warning/50 bg-warning/10 px-3 py-2 text-dense">
          {tt('目录过大（')}{total} {tt('项）：本页为客户端虚拟滚动（children 契约暂无游标）。建议改用')}{' '}
          <a className="text-primary underline" href="/binflow/ui/search">{tt('搜索')}</a> {tt('定位制品。')}
        </div>
      )}

      {loading ? (
        <div data-testid="skeleton" aria-hidden="true" className="flex flex-col gap-2 pt-2">
          {Array.from({ length: 10 }, (_, i) => (
            <div key={i} className="h-5 animate-pulse rounded-sm bg-surface-2" style={{ width: `${90 - i * 5}%` }} />
          ))}
        </div>
      ) : total === 0 ? (
        emptyState
      ) : rows.length === 0 && filter.trim() !== '' ? (
        <EmptyState
          message={tt('无匹配「{filter}」的条目', { filter })}
          hint={filesOnly ? tt('过滤只作用于当前层已加载的条目，且「只看文件」正在一并收窄范围。') : tt('过滤只作用于当前层已加载的条目。')}
          action={
            <Button
              variant="outline"
              size="sm"
              data-testid="tree-filter-clear"
              onClick={() => {
                onFilter('')
                onFilesOnly(false)
              }}
            >
              {tt('清除过滤')}
            </Button>
          }
        />
      ) : rows.length === 0 && filesOnly ? (
        <EmptyState
          message={tt('当前层没有文件（只有目录）')}
          hint={tt('「只看文件」正在收窄列表。')}
          action={
            <Button variant="outline" size="sm" data-testid="tree-filter-clear" onClick={() => onFilesOnly(false)}>
              {tt('清除「只看文件」')}
            </Button>
          }
        />
      ) : (
        <div className="flex min-h-0 flex-1 flex-col" data-testid="tree-children">
        <div className="h-[560px] min-h-0 flex-1" data-testid="tree-list">
          <AgGridReact
            theme={agTheme}
            columnDefs={columnDefs}
            onGridReady={onGridReady}
            onCellClicked={onCellClicked}
            onCellKeyDown={onCellKeyDown}
            rowModelType="infinite"
            cacheBlockSize={PAGE}
            cacheOverflowSize={2}
            maxConcurrentDatasourceRequests={1}
            infiniteInitialRowCount={rows.length === 0 ? 0 : undefined}
            rowSelection={{ mode: 'multiRow', checkboxes: true, headerCheckbox: true, enableClickSelection: false }}
            selectionColumnDef={{ pinned: 'left', width: 44 }}
            getRowId={(p) => String((p.data as ChildNode)?.name ?? '')}
            rowClassRules={{
              'ag-row-selected-file': (p) => focusFile !== null && (p.data as ChildNode)?.name === focusFile,
            }}
            suppressCellFocus={false}
            ensureDomOrder
            headerHeight={36}
            rowHeight={32}
            enableCellTextSelection
            tooltipShowDelay={400}
            onSelectionChanged={(e) => {
              setSelected(new Set(e.api.getSelectedRows().map((r) => (r as ChildNode).name)))
            }}
          />
        </div>
        </div>
      )}
    </div>
  )
}
