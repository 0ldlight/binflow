import { useEffect, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent } from 'react'
import { useNavigate } from 'react-router-dom'

import Button from '@mui/material/Button'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import TextField from '@mui/material/TextField'

import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { apiJSON } from '../../lib/api'
import { formatBytes } from '../../lib/format'
import { monoInputSx } from '../../lib/muiAtoms'
import { useAsync } from '../../lib/useAsync'

import './search.css'

// 搜索页（console-m8 §6.4 / reverse §3.3·§4.5——Artifactory 搜索结果页
// 对齐面，T-239 重排）：
//
// - 固定类型「制品」（§1.2 重塑口径：BinFlow 只有制品搜索，单一值不渲染
//   类型下拉——R2 类型化落地后再现，不建影子入口）。
// - 标题 + 计数副标「搜索结果 – N 项」（reverse §3.3；计数与行数一致性
//   为验收项——不对齐 Artifactory 的计数怪癖）；底部计数行「显示 a – b /
//   共 c 项」+ 客户端切片「加载更多」（C1 口径：不采纳页码跳页）。
// - recentSearches（reverse §4.5）：localStorage 最近 8 条，输入聚焦展开、
//   按当前关键词子串过滤、↑↓ 循环 + Enter 应用 + Esc 关闭 + 一键清除。
// - 深链状态：URL 即查询状态（?q= + 可选 ?repos=，replaceState 不污染
//   历史）——直链 /search?q=… 回显关键词并立即查询（「选中态回显」）。
// - 结果行 → 跨仓树深链 /artifacts/<repo>/<父目录>?focus=<文件名>
//   （T-236 深链自动展开祖先并选中；路径段逐段 percent-encode，T-231 矩阵）。
// - 契约注记（沿 T-100）：SR-01 返回 E-09 FileInfo 全量（无分页参数），
//   分页为客户端切片；副行从路径形态推导（maven GAV / metadata），不做
//   逐行二次请求。ACL 过滤由后端保证（T-92 零泄漏）。
// - 漏斗三过滤（仓库/包类型/仓型，console-m8 §6.4 线框）不建：搜索端点
//   仅 name+repos 两参，包类型/仓型无服务端参数，也不做仅 admin 可用的
//   前端伪过滤（repos 清单对普通 user 403）——保持仓 key 过滤单一入口。

const PAGE = 100
const RECENT_KEY = 'binflow-console-recent-searches'
const RECENT_MAX = 8

interface SearchResult {
  repo: string
  path: string
  size: string
  lastModified?: string
  mimeType?: string
  checksums?: { sha1?: string; md5?: string; sha256?: string }
}

function searchArtifacts(name: string, repos: string, signal?: AbortSignal): Promise<{ results: SearchResult[] }> {
  const params = new URLSearchParams()
  params.set('name', name)
  if (repos.trim()) params.set('repos', repos.trim())
  return apiJSON<{ results: SearchResult[] }>(`/search/artifact?${params.toString()}`, { signal })
}

/** 从路径形态推导协议语义副行（§4.8：让工程师不点进去就能判断「是不是它」） */
export function semanticOf(path: string): string | null {
  const segs = path.replace(/^\//, '').split('/')
  const file = segs[segs.length - 1]
  if (file === 'maven-metadata.xml' && segs.length >= 3) {
    return `maven-metadata · ${segs.slice(0, segs.length - 2).join(':')}:${segs[segs.length - 2]}`
  }
  if (segs.length >= 4) {
    const groupId = segs.slice(0, segs.length - 3).join('.')
    const artifactId = segs[segs.length - 3]
    const version = segs[segs.length - 2]
    if (file.startsWith(`${artifactId}-${version}`)) {
      return `maven GAV ${groupId}:${artifactId}:${version}`
    }
  }
  return null
}

// ---- recentSearches（localStorage；隐私模式下降级为会话内不持久） ----

function loadRecent(): string[] {
  try {
    const raw: unknown = JSON.parse(window.localStorage.getItem(RECENT_KEY) ?? '[]')
    if (!Array.isArray(raw)) return []
    return raw.filter((v): v is string => typeof v === 'string' && v.trim() !== '').slice(0, RECENT_MAX)
  } catch {
    return []
  }
}

function persistRecent(list: string[]): void {
  try {
    window.localStorage.setItem(RECENT_KEY, JSON.stringify(list))
  } catch {
    // localStorage 不可用：最近搜索退化为本次会话内有效
  }
}

/** 深链初始态：?q= / ?repos= 直读一次（replaceState 只写不读回环） */
function initialQuery(): { q: string; repos: string } {
  const p = new URLSearchParams(window.location.search)
  return { q: (p.get('q') ?? '').trim(), repos: p.get('repos') ?? '' }
}

export default function SearchPage() {
  const navigate = useNavigate()
  const [initial] = useState(initialQuery)
  const [keyword, setKeyword] = useState(initial.q)
  // 深链带 q 时立即查询（不等防抖——「选中态回显」语义）
  const [debounced, setDebounced] = useState(initial.q)
  const [repoFilter, setRepoFilter] = useState(initial.repos)
  const abortRef = useRef<AbortController | null>(null)
  const [visible, setVisible] = useState(PAGE)
  const [recent, setRecent] = useState<string[]>(() => loadRecent())
  const [recentOpen, setRecentOpen] = useState(false)
  const [recentActive, setRecentActive] = useState(-1)

  // 防抖 300ms（§5.2）；变化即作废在飞请求
  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(keyword.trim()), 300)
    return () => window.clearTimeout(t)
  }, [keyword])

  // 深链状态写回：查询态进 URL（replaceState——搜索历史不逐字母进栈）
  useEffect(() => {
    const params = new URLSearchParams()
    if (debounced) params.set('q', debounced)
    if (repoFilter.trim()) params.set('repos', repoFilter.trim())
    const qs = params.toString()
    const next = `${window.location.pathname}${qs ? `?${qs}` : ''}`
    if (next !== `${window.location.pathname}${window.location.search}`) {
      window.history.replaceState(null, '', next)
    }
  }, [debounced, repoFilter])

  const results = useAsync(() => {
    abortRef.current?.abort()
    if (debounced === '') return Promise.resolve(null)
    const ctrl = new AbortController()
    abortRef.current = ctrl
    return searchArtifacts(debounced, repoFilter, ctrl.signal)
  }, [debounced, repoFilter])
  useEffect(() => setVisible(PAGE), [debounced, repoFilter, results.status])

  const rows = results.data?.results ?? []

  // 最近搜索按当前关键词子串过滤（空关键词 = 全量历史）
  const recentList = recent.filter((q) => q.toLowerCase().includes(keyword.trim().toLowerCase()))

  const commit = (q: string) => {
    const t = q.trim()
    if (t === '') return
    setKeyword(t)
    setDebounced(t)
    setRecentActive(-1)
    setRecentOpen(false)
    const next = [t, ...loadRecent().filter((v) => v !== t)].slice(0, RECENT_MAX)
    persistRecent(next)
    setRecent(next)
  }

  const clearRecent = () => {
    persistRecent([])
    setRecent([])
    setRecentOpen(false)
    setRecentActive(-1)
  }

  const onInputKeyDown = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown' && recentList.length > 0) {
      e.preventDefault() // 光标跳到行首的默认行为让位给历史导航
      if (!recentOpen) setRecentOpen(true)
      setRecentActive((i) => Math.min(i + 1, recentList.length - 1))
      return
    }
    if (e.key === 'ArrowUp' && recentOpen && recentList.length > 0) {
      e.preventDefault()
      setRecentActive((i) => Math.max(i - 1, 0))
      return
    }
    if (e.key === 'Escape' && recentOpen) {
      setRecentOpen(false)
      setRecentActive(-1)
      return
    }
    if (e.key === 'Enter') {
      if (recentOpen && recentActive >= 0 && recentList[recentActive]) {
        e.preventDefault()
        commit(recentList[recentActive])
        return
      }
      // 显式提交：立即查询 + 记入最近搜索（防抖路径只查询不记历史）
      e.preventDefault()
      commit(keyword)
    }
  }

  const gotoNode = (r: SearchResult) => {
    const segs = r.path.replace(/^\//, '').split('/')
    const name = segs.pop() ?? ''
    const enc = segs.map((s) => encodeURIComponent(s)).join('/')
    const dirPart = enc ? `/${enc}` : ''
    navigate(`/artifacts/${encodeURIComponent(r.repo)}${dirPart}?focus=${encodeURIComponent(name)}`)
  }

  const shown = Math.min(visible, rows.length)
  const hasQuery = debounced !== ''

  return (
    <div data-testid="search-page">
      <h2 className="search-headline">搜索制品</h2>
      {hasQuery && results.status === 'ok' && (
        <p className="search-count" data-testid="search-count">
          搜索结果 – {rows.length} 项
        </p>
      )}
      {!hasQuery && (
        <p className="text-2 search-sub">
          按名称/路径子串检索全部可见制品（结果按你的路径 ACL 过滤）。⌘K 或 / 可从任意页跳入。
          checksum 反查（sha256/sha1/md5）暂未接入 UI——见 CLI 文档。
        </p>
      )}

      <div className="filter-bar">
        <div className="search-box">
          {/* T-300 批次二迁 MUI TextField：锚/combobox 语义属性（aria-expanded
              / aria-autocomplete）经 htmlInput 落 input 本体；最近词下拉
              （search-recent 族）保持原生——顶栏同款零变化红线 */}
          <TextField
            type="search"
            size="small"
            autoFocus
            placeholder="搜索制品：名称或路径包含…"
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            onFocus={() => setRecentOpen(true)}
            onBlur={() => {
              setRecentOpen(false)
              setRecentActive(-1)
            }}
            onKeyDown={onInputKeyDown}
            sx={{ ...monoInputSx, width: 420 }}
            slotProps={{
              htmlInput: {
                'data-testid': 'search-input',
                'aria-label': '搜索关键词',
                className: 'mono',
                'aria-expanded': recent.length > 0 ? recentOpen : undefined,
                'aria-autocomplete': 'list',
              },
            }}
          />
          {/* 展开条件：聚焦 + 有历史 +（关键词为空 或 ↑↓ 显式导航中）——
              有关键词且不在导航态时不遮挡结果表 */}
          {recentOpen && recentList.length > 0 && (keyword.trim() === '' || recentActive >= 0) && (
            <div className="search-recent" data-testid="search-recent">
              <div className="search-recent-head">
                <span>最近搜索</span>
                <button
                  type="button"
                  className="copy-btn"
                  data-testid="search-recent-clear"
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={clearRecent}
                >
                  清除历史
                </button>
              </div>
              <ul className="search-recent-list" aria-label="最近搜索">
                {recentList.map((q, i) => (
                  <li key={q}>
                    <button
                      type="button"
                      className={i === recentActive ? 'active' : ''}
                      data-testid={`search-recent-item-${i}`}
                      lang="en"
                      onMouseDown={(e) => e.preventDefault()}
                      onClick={() => commit(q)}
                    >
                      {q}
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
        <TextField
          type="search"
          size="small"
          placeholder="仓库过滤（逗号分隔 key）"
          value={repoFilter}
          onChange={(e) => setRepoFilter(e.target.value)}
          sx={{ ...monoInputSx, width: 240 }}
          slotProps={{ htmlInput: { 'data-testid': 'search-filter-repo', 'aria-label': '按仓库过滤', className: 'mono' } }}
        />
      </div>

      {!hasQuery ? (
        <EmptyState
          message="输入关键词开始搜索"
          hint="子串匹配制品路径（如 libcore、acme/app、1.0.3）。空关键词不发起查询。"
        />
      ) : results.status === 'loading' ? (
        <div data-testid="skeleton" aria-hidden="true" style={{ paddingTop: 8 }}>
          {Array.from({ length: 8 }, (_, i) => (
            <div key={i} className="skeleton line" style={{ width: `${88 - i * 6}%` }} />
          ))}
        </div>
      ) : results.status === 'error' && results.error ? (
        <ErrorCard error={results.error} onRetry={results.reload} />
      ) : rows.length === 0 ? (
        <EmptyState
          message={`没有匹配「${debounced}」的制品`}
          hint="检查拼写、放宽仓库过滤，或换更短的子串；结果按你的权限过滤。"
        />
      ) : (
        <>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell component="th" scope="col">仓库</TableCell>
                <TableCell component="th" scope="col">路径 / 语义</TableCell>
                <TableCell component="th" scope="col">大小</TableCell>
                <TableCell component="th" scope="col">修改时间</TableCell>
                <TableCell component="th" scope="col">sha256</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.slice(0, visible).map((r, i) => {
                const sub = semanticOf(r.path)
                return (
                  <TableRow
                    key={`${r.repo}${r.path}`}
                    data-testid={`search-result-${i}`}
                    hover
                    tabIndex={0}
                    onClick={() => gotoNode(r)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') gotoNode(r)
                    }}
                  >
                    <TableCell className="mono" lang="en">{r.repo}</TableCell>
                    <TableCell sx={{ whiteSpace: 'normal', wordBreak: 'break-all' }}>
                      <div className="mono row-link" lang="en">{r.path}</div>
                      <div className="search-result-sub">
                        {sub ? (
                          <span lang="en">{sub}</span>
                        ) : (
                          r.checksums?.sha256 && (
                            <span className="mono" lang="en" title={r.checksums.sha256}>
                              sha256 {r.checksums.sha256.slice(0, 12)}…
                            </span>
                          )
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="mono">{formatBytes(Number(r.size) || 0)}</TableCell>
                    <TableCell className="mono">{r.lastModified ? r.lastModified.replace('T', ' ').slice(0, 19) : '—'}</TableCell>
                    <TableCell className="mono" title={r.checksums?.sha256 ?? ''}>
                      {r.checksums?.sha256 ? (
                        <>
                          {`${r.checksums.sha256.slice(0, 10)}…`}
                          <CopyButton value={r.checksums.sha256} label={`sha256 ${r.path}`} />
                        </>
                      ) : (
                        '—'
                      )}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
          <div className="search-footer" data-testid="search-pager">
            <span>
              显示 1 – {shown} / 共 {rows.length} 项
            </span>
            {visible < rows.length && (
              <Button
                variant="outlined"
                size="small"
               
                data-testid="search-more"
                onClick={() => setVisible((v) => v + PAGE)}
              >
                加载更多
              </Button>
            )}
          </div>
        </>
      )}
    </div>
  )
}
