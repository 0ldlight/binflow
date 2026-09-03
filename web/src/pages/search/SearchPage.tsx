import { useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import Button from '@mui/material/Button'
import Divider from '@mui/material/Divider'
import Menu from '@mui/material/Menu'
import MenuItem from '@mui/material/MenuItem'
import MuiSkeleton from '@mui/material/Skeleton'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'

import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { apiJSON } from '../../lib/api'
import { useColumnPrefs } from '../../lib/columnPrefs'
import type { ColumnDef, ColumnPrefs } from '../../lib/columnPrefs'
import { useAsync } from '../../lib/useAsync'

import { AqlPanel } from './AqlPanel'
import { semanticOf } from './aql'
import { ResultsTable } from './ResultsTable'
import type { ResultRow } from './ResultsTable'

import './search.css'

// 搜索页（console-m8 §6.4 / reverse §3.3——T-449 / FR-144.6 搜索栈翻新，
// parity B-2.11/13/14 + B-3.14/15/16 归一承载）：
//
// - **查询位置（B-2.13 翻正）**：查询面 = 顶栏驻留输入（AppShell
//   topbar-search——Enter 提交 → /search?q=<词>，URL 即查询状态）；本页
//   不再有页内关键词/仓库过滤输入（旧行检索表单退役——?repos= 参数随之
//   退役，旧深链仍按 ?q= 查询，仅丢仓库窄化，兼容不猝死）。结果窄化 =
//   网格内快滤（ResultsTable——客户端子串过滤，对位 Artifactory 网格
//   快滤）。AQL 模式编辑器共存形态：编辑器是 AQL 模式的查询面（服务端
//   查询），顶栏驻留输入保持全局基本检索入口，快滤两层正交（编辑器管
//   服务端、快滤管已取回行）。
// - **列集归一（B-2.11 断言反转②）**：结果列 = 选择列（固定）+ Artifact
//   （name 链接）| Path | Repository | Modified——三源不一致（现行五列 /
//   T-414 注释 / console-m8 §6.4）就此归一；大小 + sha256 移入列选器
//   可选项不默认呈现（defaultHidden）。AQL 表头排序沿 search-aql-sort-*
//   族（ResultsTable 注入）。
// - **行导航（B-3.14 翻正）**：行体 inert，深链只在 name 单元格（链接
//   href = K67-3 路径段规范形，?focus= 发射端退役——T-434 兼容重定向
//   维持一轮）。
// - **日期格式（B-3.15 翻正腿①）**：结果表 modified = `dd-MM-yy
//   HH:mm:ss +ZZZZ`（formatStamp——浏览器本地时区 + 显式偏移）。
// - 深链状态：?q=（顶栏 Enter 写入）+ ?mode=aql（AQL 直达；查询文本不
//   入 URL——6,000 字符上限的查询串不宜进地址栏，T-419 定案维持）。
// - 契约注记（沿 T-100）：SR-01 返回 E-09 FileInfo 全量（无分页参数），
//   分页为客户端切片（ResultsTable「加载更多」）；recentSearches 面
//   （reverse §4.5）随页内输入退役归顶栏单承载（AppShell 下拉——同键
//   同语义，空历史占位恒渲染对位「No recent searches yet」）。ACL 过滤
//   由后端保证（T-92 零泄漏）。
// - 漏斗三过滤不建（沿 T-239 裁定）；类型下拉 R2 落地后再现。

const PAGE = 100

/** T-449 断言反转②：列集 = Artifact|Path|Repository|Modified 默认在场，
 *  大小/sha256 为列选器可选项（defaultHidden——lib/columnPrefs T-449 语义）。
 *  label 与表头一致；anchor = 菜单项锚（T-414 族扩 name）。 */
const COLUMNS: ColumnDef[] = [
  { id: 'name', label: '制品', anchor: 'search-columns-item-name' },
  { id: 'path', label: '路径', anchor: 'search-columns-item-path' },
  { id: 'repo', label: '仓库', anchor: 'search-columns-item-repo' },
  { id: 'modified', label: '修改时间', anchor: 'search-columns-item-modified' },
  { id: 'size', label: '大小', anchor: 'search-columns-item-size' },
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

/** E-09 全路径 → 网格行（dir/name 拆分——name 列是链接载体；语义副行
 *  由 semanticOf 推导，两模式共用见 aql.ts）。 */
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
    sub: sub ? <span className="mono" lang="en">{sub}</span> : undefined,
  }
}

type SearchMode = 'basic' | 'aql'

/** 列选器（T-414 交付面；T-449 起复位 = 恢复默认列集非全显）——两模式
 *  共用一份壳（同一时刻仅一模式在场，页内锚唯一）。 */
function ColumnsMenu({ cols }: { cols: ColumnPrefs }) {
  const [colsAnchor, setColsAnchor] = useState<HTMLElement | null>(null)
  const colsOpen = Boolean(colsAnchor)
  // 复位目标 = 默认列集（size/sha256 收回）——「恢复默认」而非「全选」
  const atDefault = DEFAULT_HIDDEN.every((id) => cols.hidden.has(id)) && cols.hidden.size === DEFAULT_HIDDEN.length
  return (
    <span className="filter-tail-actions filter-tail-end">
      <Button
        variant="outlined"
        size="small"
        aria-haspopup="menu"
        aria-expanded={colsOpen}
        data-testid="search-columns"
        title="自定义显示列（偏好保存在本浏览器）"
        onClick={(e) => setColsAnchor(e.currentTarget)}
      >
        <span aria-hidden="true">▤</span> 列 {cols.visibleCount}/{COLUMNS.length}
      </Button>
      <Menu
        open={colsOpen}
        onClose={() => setColsAnchor(null)}
        anchorEl={colsAnchor}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
        transformOrigin={{ vertical: 'top', horizontal: 'right' }}
        data-testid="search-columns-menu"
      >
        {COLUMNS.map((c) => {
          const visible = cols.isVisible(c.id)
          // 至少一列在场：仅剩一列可见时该项不可再弃
          const last = visible && cols.visibleCount === 1
          return (
            <MenuItem
              key={c.id}
              role="menuitemcheckbox"
              aria-checked={visible}
              aria-disabled={last || undefined}
              title={last ? '至少保留一列' : undefined}
              data-testid={c.anchor}
              onClick={() => {
                if (!last) cols.toggle(c.id)
              }}
            >
              <span aria-hidden="true" className="col-check">
                {visible ? '☑' : '☐'}
              </span>
              {c.label}
            </MenuItem>
          )
        })}
        <Divider component="li" />
        <MenuItem
          aria-disabled={atDefault || undefined}
          title={atDefault ? '已是默认列集' : '恢复默认列（大小/sha256 收回）'}
          data-testid="search-columns-reset"
          onClick={() => cols.reset()}
        >
          恢复默认列
        </MenuItem>
      </Menu>
    </span>
  )
}

export default function SearchPage() {
  const navigate = useNavigate()
  // URL 即查询态（顶栏 Enter 写入 ?q=；?mode=aql 切 AQL）——本页零本地
  // 查询状态，replaceState 写回环退役
  const [params] = useSearchParams()
  const q = (params.get('q') ?? '').trim()
  const mode: SearchMode = params.get('mode') === 'aql' ? 'aql' : 'basic'
  const abortRef = useRef<AbortController | null>(null)

  // T-414（FR-135.2）+ T-449 断言反转②：列显隐偏好（defaultHidden =
  // 大小/sha256；T-387 共享层语义扩展，浏览器本地偏好面）——两模式共用
  const cols = useColumnPrefs(COLUMN_IDS, COLS_KEY, DEFAULT_HIDDEN)

  const results = useAsync(() => {
    abortRef.current?.abort()
    if (mode === 'aql' || q === '') return Promise.resolve(null)
    const ctrl = new AbortController()
    abortRef.current = ctrl
    return searchArtifacts(q, ctrl.signal)
  }, [q, mode])

  const rows = useMemo(() => (results.data?.results ?? []).map(toRow), [results.data])
  const count = results.data?.results.length ?? 0

  const switchMode = (next: SearchMode) => {
    if (next === mode) return
    // 模式切换 = URL 重写（replace）：AQL 只带 mode；基本模式不带参数
    //（T-419 语义维持——切回基本不续跑旧 q，顶栏驻留词还在，Enter 即重查）
    navigate(next === 'aql' ? '/search?mode=aql' : '/search', { replace: true })
  }

  return (
    <div data-testid="search-page">
      <h2 className="search-headline">搜索制品</h2>
      {/* T-419（FR-135.1）：模式切换——基本（顶栏驻留查询 + 结果网格）↔
          AQL 编辑器；切模式不丢 AQL 侧已写查询（组件卸载/重挂的 T-419
          行为维持：编辑器文本不跨切换保留，深链 ?mode=aql 是重入通道） */}
      <ToggleButtonGroup
        exclusive
        size="small"
        value={mode}
        onChange={(_, v) => {
          if (v !== null) switchMode(v)
        }}
        aria-label="搜索模式"
        data-testid="search-mode"
        sx={{
          mb: 2,
          // MUI 未选中档位文字默认 54% 黑（亮主题 #f3f5f7 底上 ≈4.2:1 <
          // AA）——钉 text.primary（选中态走 MUI 原生主色对比面，不碰）
          '& .MuiToggleButton-root:not(.Mui-selected)': { color: 'text.primary' },
        }}
      >
        <ToggleButton value="basic" data-testid="search-mode-basic">
          基本
        </ToggleButton>
        <ToggleButton value="aql" data-testid="search-mode-aql">
          AQL
        </ToggleButton>
      </ToggleButtonGroup>

      {mode === 'aql' ? (
        <AqlPanel columns={COLUMNS} cols={cols} toolbar={<ColumnsMenu cols={cols} />} />
      ) : (
        <>
          {q !== '' && results.status === 'ok' && (
            <p className="search-count" data-testid="search-count">
              搜索结果 – {count} 项
            </p>
          )}
          {q === '' && (
            <p className="text-2 search-sub">
              查询在顶栏驻留：上方搜索框输入名称/路径子串并 Enter（⌘K 或 / 可从任意页跳入），结果在此呈现并按你的路径
              ACL 过滤。checksum 反查（sha256/sha1/md5）暂未接入 UI——见 CLI 文档。
            </p>
          )}

          {/* 列选器：结果网格不在场时由独立工具行承载（列选是结果表的
              偏好面不是结果的从属——空态/无结果可预设，T-414 定案维持；
              网格在场时改驻网格工具行，同一时刻仅一处） */}
          {!(q !== '' && results.status === 'ok' && count > 0) && (
            <div className="search-grid-bar">
              <span className="filter-tail-actions filter-tail-end" style={{ marginLeft: 'auto' }}>
                <ColumnsMenu cols={cols} />
              </span>
            </div>
          )}

          {q === '' ? (
            <EmptyState
              illustration
              message="在顶栏输入关键词开始搜索"
              hint="顶栏搜索框（⌘K）输入子串（如 libcore、acme/app、1.0.3）后回车——空关键词不发起查询；网格内快滤可再窄化已取回的结果。"
            />
          ) : results.status === 'loading' ? (
            <div data-testid="skeleton" aria-hidden="true" style={{ paddingTop: 8 }}>
              {Array.from({ length: 8 }, (_, i) => (
                <MuiSkeleton key={i} variant="text" width={`${88 - i * 6}%`} sx={{ my: 0.5 }} />
              ))}
            </div>
          ) : results.status === 'error' && results.error ? (
            <ErrorCard error={results.error} onRetry={results.reload} />
          ) : count === 0 ? (
            <EmptyState
              illustration
              message={`没有匹配「${q}」的制品`}
              hint="检查拼写或换更短的子串；结果按你的权限过滤。"
            />
          ) : (
            <ResultsTable
              rows={rows}
              columns={COLUMNS}
              cols={cols}
              toolbarRight={<ColumnsMenu cols={cols} />}
              pageSize={PAGE}
            />
          )}
        </>
      )}
    </div>
  )
}
