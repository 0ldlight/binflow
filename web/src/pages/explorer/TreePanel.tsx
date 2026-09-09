// Explorer 左树（TanStack Virtual 虚拟化——architecture §7：TREE_LEVEL_CAP
// +load-more 升级为真虚拟滚动；懒单层加载语义保留）。
//
// 语义平移自旧 ArtifactsBrowser 树面（audit §2.2）：
// - 仓库顶层节点 + 懒展开一层；选择 ≠ 展开（单击 = 纯选中，展开箭头独立）；
// - 文件叶子进树（点击 = 选中出右侧 item view）；
// - 键盘全套：↑↓ 移动 / → 展开 / ← 收起或上跳 / Enter·Space 激活 /
//   Shift+F10·Menu 右键菜单（视口边缘钳制）；
// - 树头工具带：过滤仓库 / 包类型 facet / Local·Remote·Virtual 组 /
//   Sort-by / Compacted·Non-Compacted / My Favorites（localStorage 键
//   bf-tree-favorites / bf-tree-compacted 原样迁移——PREF_KEYS 契约）；
// - 树尾常驻回收站入口（canSeeAdmin 门）。
// - 锚族原样：tree-repo-* / tree-node-* / tree-leaf-* / tree-toolband /
//   tree-repo-filter(-clear) / tree-favorites / tree-facet-* / tree-sort-by
//   / tree-view-* / tree-trash-node / tree-context-menu 族。
import { useRef } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent, MouseEvent as ReactMouseEvent } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { PkgIcon } from '@/components/PkgIcon'
import type { RepoListItem } from '@/lib/api'
import type { ChildNode } from '@/pages/artifacts/lib'
import { tr } from '@/i18n'

import type { DirStatus } from '@/features/artifacts/hooks'
import type { MenuTarget, TreeSort } from './model'

const tt = tr('artifacts')

const RC_ICON: Record<string, string> = { local: '▣', remote: '◈', virtual: '◍' }

/** 树行（虚拟化平铺模型——depth 驱动缩进） */
export interface TreeRow {
  key: string
  kind: 'repo' | 'dir' | 'file' | 'trash'
  repo: string
  /** repo/dir 行的目录路径（'' = 仓根）；file 行的文件路径 */
  dir: string
  depth: number
  node?: ChildNode
  repoItem?: RepoListItem
  isOpen?: boolean
  onChain?: boolean
  selected?: boolean
  testid: string
}

export function buildTreeRows(
  filteredRepos: RepoListItem[],
  dirState: Map<string, DirStatus>,
  expanded: Set<string>,
  repoKey: string,
  dir: string,
  focusPath: string | null,
  canSeeAdmin: boolean,
): TreeRow[] {
  const rows: TreeRow[] = []
  const emitLevel = (repo: string, path: string, depth: number) => {
    const st = dirState.get(`${repo}\n${path}`)
    if (!st || st.status !== 'ok') return
    for (const n of st.nodes) {
      if (!n.folder) {
        rows.push({
          key: `f:${repo}:${n.path}`,
          kind: 'file',
          repo,
          dir: n.path,
          depth,
          node: n,
          selected: repo === repoKey && focusPath === n.path,
          testid: `tree-leaf-${n.path}`,
        })
      } else {
        const isOpen = expanded.has(`${repo}\n${n.path}`)
        const onChain = repo === repoKey && (dir === n.path || dir.startsWith(`${n.path}/`))
        rows.push({
          key: `d:${repo}:${n.path}`,
          kind: 'dir',
          repo,
          dir: n.path,
          depth,
          node: n,
          isOpen,
          onChain,
          selected: repo === repoKey && dir === n.path,
          testid: `tree-node-${n.path}`,
        })
        if (isOpen) emitLevel(repo, n.path, depth + 1)
      }
    }
  }
  for (const r of filteredRepos) {
    const isOpen = expanded.has(`${r.key}\n`)
    rows.push({
      key: `r:${r.key}`,
      kind: 'repo',
      repo: r.key,
      dir: '',
      depth: 0,
      repoItem: r,
      isOpen,
      onChain: r.key === repoKey,
      selected: r.key === repoKey && dir === '',
      testid: `tree-repo-${r.key}`,
    })
    if (isOpen) emitLevel(r.key, '', 1)
  }
  if (canSeeAdmin) {
    rows.push({ key: 'trash', kind: 'trash', repo: '', dir: '', depth: 0, testid: 'tree-trash-node' })
  }
  return rows
}

export function TreePanel({
  rows,
  compacted,
  loading,
  error,
  forbidden,
  repoFilter,
  onRepoFilter,
  pkgTypes,
  pkgFacets,
  onTogglePkg,
  onClearPkg,
  rclassFacets,
  onToggleRclass,
  sortBy,
  onSort,
  onCompacted,
  favCount,
  favOnly,
  onFavOnly,
  onNavigate,
  onToggle,
  onMenu,
  onOpenTrash,
  onRetry,
  emptyLabel,
}: {
  rows: TreeRow[]
  compacted: boolean
  loading: boolean
  error: { status: number; message: string } | null
  forbidden: boolean
  repoFilter: string
  onRepoFilter: (v: string) => void
  pkgTypes: string[]
  pkgFacets: Set<string>
  onTogglePkg: (t: string) => void
  onClearPkg: () => void
  rclassFacets: Set<string>
  onToggleRclass: (rc: string) => void
  sortBy: TreeSort
  onSort: (s: TreeSort) => void
  onCompacted: (v: boolean) => void
  favCount: number
  favOnly: boolean
  onFavOnly: (v: boolean) => void
  onNavigate: (repo: string, dir: string) => void
  onToggle: (repo: string, dir: string) => void
  onMenu: (x: number, y: number, target: MenuTarget) => void
  onOpenTrash: () => void
  onRetry: () => void
  emptyLabel: string
}) {
  const rowH = compacted ? 24 : 28
  const scrollRef = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => rowH,
    overscan: 12,
  })

  // ---- 键盘语义（§3.4/§8：↑↓/→/←/Enter/Space/Shift+F10）----
  const onRowKeys = (e: ReactKeyboardEvent<HTMLElement>, row: TreeRow) => {
    if (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) {
      e.preventDefault()
      const rect = e.currentTarget.getBoundingClientRect()
      if (row.kind === 'repo') onMenu(rect.left + 40, rect.top + 14, { kind: 'repo', repoKey: row.repo })
      else if (row.node) onMenu(rect.left + 40, rect.top + 14, { kind: 'node', repoKey: row.repo, node: row.node })
      else if (row.kind === 'dir') {
        onMenu(rect.left + 40, rect.top + 14, {
          kind: 'node',
          repoKey: row.repo,
          node: { name: row.dir.split('/').pop() ?? '', path: row.dir, folder: true, size: null, lastModified: '', sha256: '' },
        })
      }
      return
    }
    const focusRowAt = (idx: number) => {
      const el = document.querySelectorAll<HTMLElement>('[data-tree-row]')[idx]
      if (el) {
        el.focus()
      } else {
        virtualizer.scrollToIndex(idx)
        window.requestAnimationFrame(() => {
          document.querySelectorAll<HTMLElement>('[data-tree-row]')[idx]?.focus()
        })
      }
    }
    const all = Array.from(document.querySelectorAll<HTMLElement>('[data-tree-row]'))
    const idx = all.indexOf(e.currentTarget)
    if (e.key === 'ArrowDown' && idx >= 0 && idx < rows.length - 1) {
      e.preventDefault()
      focusRowAt(idx + 1)
    } else if (e.key === 'ArrowUp' && idx > 0) {
      e.preventDefault()
      focusRowAt(idx - 1)
    } else if (e.key === 'ArrowRight') {
      e.preventDefault()
      if ((row.kind === 'dir' || row.kind === 'repo') && !row.isOpen) onToggle(row.repo, row.dir)
    } else if (e.key === 'ArrowLeft') {
      e.preventDefault()
      if (row.kind === 'trash') return
      // 叶子/未展开节点 = 上跳父目录；已展开 = 收起
      if (row.isOpen) onToggle(row.repo, row.dir)
      else onNavigate(row.repo, row.dir.split('/').slice(0, -1).join('/'))
    } else if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      if (row.kind === 'trash') onOpenTrash()
      else onNavigate(row.repo, row.dir)
    }
  }

  return (
    <nav className="tree-pane browser-tree flex min-w-0 flex-col" aria-label={tt('制品树')} data-testid="browser-tree">
      {/* ---- 树头工具带（T-434 / FR-142.4——Artifactory 树头对齐面）---- */}
      <div className="tree-toolband border-b border-border p-2" data-testid="tree-toolband">
        <div className="toolband-row flex items-center gap-1.5">
          <Input
            type="search"
            placeholder={tt('过滤仓库…')}
            value={repoFilter}
            onChange={(e) => onRepoFilter(e.target.value)}
            disabled={!loading && error === null && !forbidden ? false : true}
            className="h-7 w-[152px] text-dense"
            data-testid="tree-repo-filter"
            aria-label={tt('过滤仓库（仅已加载集）')}
          />
          {repoFilter && (
            <Button variant="outline" size="sm" data-testid="tree-repo-filter-clear" onClick={() => onRepoFilter('')}>
              {tt('清除')}
            </Button>
          )}
          <Button
            variant="outline"
            size="sm"
            data-testid="tree-favorites"
            aria-pressed={favOnly}
            title={tt('只看收藏的仓库（收藏经仓库右键菜单标记，浏览器本地持久）')}
            onClick={() => onFavOnly(!favOnly)}
          >
            {favOnly ? '★' : '☆'} My Favorites{favCount > 0 ? tt('（{favCount}）', { favCount }) : ''}
          </Button>
        </div>
        <div className="toolband-row mt-1.5 flex flex-wrap items-center gap-1.5">
          <Popover>
            <PopoverTrigger asChild>
              <Button variant="outline" size="sm" aria-haspopup="dialog" title={tt('按包类型过滤仓库树（复选组，空 = 不过滤）')} data-testid="tree-facet-pkg">
                {tt('包类型')}{pkgFacets.size > 0 ? tt('（{v1}）', { v1: pkgFacets.size }) : ''} ▾
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-64 p-2" data-testid="tree-facet-pkg-panel" align="start">
              <div className="toolband-facet-title mb-1 text-aux font-medium" lang="en">
                Filter by Package Type
              </div>
              {pkgTypes.length === 0 ? (
                <p className="text-aux text-muted-foreground">{tt('（已加载集中没有带包类型的仓库）')}</p>
              ) : (
                pkgTypes.map((t) => (
                  <Label key={t} className="flex cursor-pointer items-center gap-1.5 py-0.5 font-normal">
                    <input
                      type="checkbox"
                      className="size-3.5"
                      checked={pkgFacets.has(t)}
                      onChange={() => onTogglePkg(t)}
                      data-testid={`tree-facet-pkg-${t}`}
                    />
                    <span lang="en">{t}</span>
                  </Label>
                ))
              )}
              {pkgFacets.size > 0 && (
                <Button variant="outline" size="sm" className="mt-1.5" data-testid="tree-facet-pkg-clear" onClick={onClearPkg}>
                  {tt('清除（')}{pkgFacets.size}{tt('）')}
                </Button>
              )}
            </PopoverContent>
          </Popover>
          {(['local', 'remote', 'virtual'] as const).map((rc) => (
            <Label key={rc} className="flex cursor-pointer items-center gap-1 font-normal" title={tt('仓库类型 {v1} 过滤（空选 = 不过滤）', { v1: rc })}>
              <input
                type="checkbox"
                className="size-3.5"
                checked={rclassFacets.has(rc)}
                onChange={() => onToggleRclass(rc)}
                data-testid={`tree-facet-rclass-${rc}`}
              />
              <span lang="en">{rc}</span>
            </Label>
          ))}
        </div>
        <div className="toolband-row mt-1.5 flex flex-wrap items-center gap-2">
          <select
            value={sortBy}
            onChange={(e) => onSort(e.target.value as TreeSort)}
            aria-label={tt('树排序（Sort by）')}
            data-testid="tree-sort-by"
            className="h-7 w-[128px] rounded-sm border border-input bg-surface-1 px-2 text-dense"
          >
            <option value="name">{tt('名称')}</option>
            <option value="pkg">{tt('包类型')}</option>
            <option value="rclass">{tt('仓库类型')}</option>
          </select>
          <div className="flex items-center gap-2" role="radiogroup" aria-label={tt('树视图密度（Tree View）')}>
            <Label className="flex cursor-pointer items-center gap-1 font-normal">
              <input
                type="radio"
                name="tree-view-density"
                className="size-3.5"
                checked={compacted}
                onChange={() => onCompacted(true)}
                data-testid="tree-view-compacted"
              />
              <span title={tt('紧凑行高（Compacted）')}>{tt('紧凑')}</span>
            </Label>
            <Label className="flex cursor-pointer items-center gap-1 font-normal">
              <input
                type="radio"
                name="tree-view-density"
                className="size-3.5"
                checked={!compacted}
                onChange={() => onCompacted(false)}
                data-testid="tree-view-normal"
              />
              <span title={tt('标准行高（Non-Compacted）')}>{tt('标准')}</span>
            </Label>
          </div>
        </div>
      </div>

      {/* ---- 树体（虚拟滚动）---- */}
      <div ref={scrollRef} className="browser-tree-scroll min-h-0 flex-1 overflow-y-auto py-1" role="tree" aria-label={tt('跨仓制品树')}>
        {loading && <TreeSkeleton />}
        {error && !forbidden && (
          <div className="px-3 py-2">
            <TreeDenied label={`${tt('加载失败（HTTP')} ${error.status}${tt('）')}`} title={error.message} />
            <button type="button" className="mt-1 text-aux text-primary hover:underline" onClick={onRetry}>
              {tt('重试')}
            </button>
          </div>
        )}
        {/* 403 优先于错误卡（t372/t492 家族契约：L2 收敛「⃠ 无权限列出
            仓库」——先前 error 分支先行令 forbidden 分支不可达，P3 修复） */}
        {!loading && forbidden && (
          <p className="tree-empty-level px-3 py-2 text-aux text-muted-foreground" data-testid="tree-root-denied">{tt('⃠ 无权限列出仓库')}</p>
        )}
        {!loading && !error && !forbidden && rows.length === (canSeeAdminRow(rows) ? 1 : 0) && (
          <div className="tree-empty-level px-3 py-2 text-aux text-muted-foreground">{emptyLabel}</div>
        )}
        {!loading && !error && (
          <div style={{ height: virtualizer.getTotalSize(), position: 'relative', width: '100%' }}>
            {virtualizer.getVirtualItems().map((vi) => {
              const row = rows[vi.index]
              return (
                <div
                  key={row.key}
                  data-index={vi.index}
                  ref={virtualizer.measureElement}
                  style={{
                    position: 'absolute',
                    top: 0,
                    left: 0,
                    width: '100%',
                    transform: `translateY(${vi.start}px)`,
                  }}
                >
                  {row.kind === 'trash' ? (
                    <div
                      className="tree-node repo-node trash-node flex cursor-pointer items-center gap-1 rounded-sm px-1.5 py-1 hover:bg-surface-2"
                      data-testid={row.testid}
                      data-tree-row=""
                      role="treeitem"
                      aria-level={1}
                      tabIndex={0}
                      title={tt('回收站（local 仓删除捕获——恢复 / 永久清除 / 清空，管理页）')}
                      onClick={onOpenTrash}
                      onKeyDown={(e) => onRowKeys(e, row)}
                    >
                      <span aria-hidden="true" className="twisty w-3.5" />
                      <span aria-hidden="true" className="ico">🗑</span>
                      <span className="truncate">{tt('回收站')}</span>
                    </div>
                  ) : (
                    <div
                      className={`tree-node flex cursor-pointer items-center gap-1 rounded-sm px-1.5 py-1 ${
                        row.onChain ? 'on-chain bg-accent' : ''
                      }${row.selected ? ' selected bg-accent font-semibold' : ''} hover:bg-surface-2`}
                      data-testid={row.testid}
                      data-tree-row=""
                      role="treeitem"
                      aria-expanded={row.kind === 'file' ? undefined : row.isOpen}
                      aria-selected={row.selected || undefined}
                      aria-level={row.depth + 1}
                      tabIndex={0}
                      style={{ paddingLeft: row.depth * 14 + 6 }}
                      onClick={() => onNavigate(row.repo, row.dir)}
                      onContextMenu={(e: ReactMouseEvent) => {
                        e.preventDefault()
                        onMenu(e.clientX, e.clientY, menuTargetOf(row))
                      }}
                      onKeyDown={(e) => onRowKeys(e, row)}
                    >
                      {row.kind === 'file' ? (
                        <span aria-hidden="true" className="twisty w-3.5" />
                      ) : (
                        <span
                          role="button"
                          tabIndex={-1}
                          aria-label={row.isOpen ? tt('收起 {v1}', { v1: row.repo || row.dir }) : tt('展开 {v1}', { v1: row.repo || row.dir })}
                          className="twisty w-3.5 shrink-0"
                          onClick={(e) => {
                            e.stopPropagation()
                            onToggle(row.repo, row.dir)
                          }}
                        >
                          {row.isOpen ? '▾' : '▸'}
                        </span>
                      )}
                      {row.kind === 'repo' && row.repoItem ? (
                        <span aria-hidden="true" className="ico flex items-center gap-0.5" title={`${row.repoItem.type || tt('仓库')} · ${row.repoItem.packageType || tt('未知包类型')}`}>
                          {RC_ICON[row.repoItem.type] ?? '▣'}
                          <PkgIcon id={row.repoItem.packageType || 'generic'} variant="mono" size={14} className="tree-pkg" />
                        </span>
                      ) : (
                        <span aria-hidden="true" className="ico">{row.kind === 'file' ? '◾' : '◻'}</span>
                      )}
                      <span className="truncate font-mono" lang="en">
                        {row.kind === 'repo' ? row.repo : row.node?.name ?? ''}
                      </span>
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>
    </nav>
  )
}

function menuTargetOf(row: TreeRow): MenuTarget {
  if (row.kind === 'repo') return { kind: 'repo', repoKey: row.repo }
  if (row.node) return { kind: 'node', repoKey: row.repo, node: row.node }
  return {
    kind: 'node',
    repoKey: row.repo,
    node: { name: row.dir.split('/').pop() ?? '', path: row.dir, folder: true, size: null, lastModified: '', sha256: '' },
  }
}

function canSeeAdminRow(rows: TreeRow[]): boolean {
  return rows.some((r) => r.kind === 'trash')
}

function TreeSkeleton() {
  return (
    <div className="tree-skel px-3 py-2" aria-hidden="true" data-testid="skeleton">
      {Array.from({ length: 8 }, (_, i) => (
        <div key={i} className="mb-1.5 h-3.5 animate-pulse rounded-sm bg-surface-2" style={{ width: `${80 - i * 5}%` }} />
      ))}
    </div>
  )
}

function TreeDenied({ label, title }: { label: string; title?: string }) {
  return (
    <div className="tree-denied px-3 py-2 text-aux text-muted-foreground" title={title}>
      {label}
    </div>
  )
}
