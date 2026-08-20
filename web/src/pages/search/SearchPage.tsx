import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { apiJSON } from '../../lib/api'
import { formatBytes } from '../../lib/format'
import { useAsync } from '../../lib/useAsync'

import '../repositories/tree/tree.css'

// 搜索页（console-ux §4.8；W14b）：防抖 300ms、空关键词引导态、结果表
// repo/path/size + 协议语义副行、行点击跳树定位（?focus=）。
//
// 契约注记：SR-01 返回 E-09 FileInfo 全量（无分页参数、无类型化副行
// 字段——ux R2 的 P2 部分）——分页为客户端切片「加载更多」；副行从路径
// 形态推导（maven GAV / metadata 文档），不做逐行二次请求。ACL 过滤
// 由后端保证（T-92：零泄漏），空结果按「无结果」呈现、不解释被过滤。

const PAGE = 100

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

export default function SearchPage() {
  const navigate = useNavigate()
  const [keyword, setKeyword] = useState('')
  const [repoFilter, setRepoFilter] = useState('')
  const [debounced, setDebounced] = useState('')
  const abortRef = useRef<AbortController | null>(null)
  const [visible, setVisible] = useState(PAGE)

  // 防抖 300ms（§5.2）；变化即作废在飞请求
  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(keyword.trim()), 300)
    return () => window.clearTimeout(t)
  }, [keyword])

  const results = useAsync(() => {
    abortRef.current?.abort()
    if (debounced === '') return Promise.resolve(null)
    const ctrl = new AbortController()
    abortRef.current = ctrl
    return searchArtifacts(debounced, repoFilter, ctrl.signal)
  }, [debounced, repoFilter])
  useEffect(() => setVisible(PAGE), [debounced, repoFilter, results.status])

  const rows = results.data?.results ?? []

  const gotoNode = (r: SearchResult) => {
    const segs = r.path.replace(/^\//, '').split('/')
    const name = segs.pop() ?? ''
    const enc = segs.map((s) => encodeURIComponent(s)).join('/')
    navigate(
      `/repositories/${encodeURIComponent(r.repo)}/tree${enc ? `/${enc}` : ''}?focus=${encodeURIComponent(name)}`,
    )
  }

  return (
    <div data-testid="search-page">
      <h2 className="search-headline">搜索</h2>
      <p className="text-2 search-sub">
        按名称/路径子串检索全部可见制品（结果按你的路径 ACL 过滤）。⌘K 或 / 可从任意页跳入。
        checksum 反查（sha256/sha1/md5）暂未接入 UI——见 CLI 文档。
      </p>

      <div className="filter-bar">
        <input
          type="search"
          placeholder="名称或路径包含…"
          aria-label="搜索关键词"
          data-testid="search-input"
          autoFocus
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
          style={{ minWidth: 320 }}
        />
        <input
          type="search"
          placeholder="仓库过滤（逗号分隔 key）"
          aria-label="按仓库过滤"
          data-testid="search-filter-repo"
          value={repoFilter}
          onChange={(e) => setRepoFilter(e.target.value)}
        />
        <span className="count search-count" data-testid="search-count">
          {results.status === 'ok' && debounced !== '' && <>约 {rows.length} 条结果</>}
        </span>
      </div>

      {debounced === '' ? (
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
          <table className="table" data-testid="search-results">
            <thead>
              <tr>
                <th>仓库</th>
                <th>路径 / 语义</th>
                <th>大小</th>
                <th>修改时间</th>
                <th>sha256</th>
              </tr>
            </thead>
            <tbody>
              {rows.slice(0, visible).map((r, i) => {
                const sub = semanticOf(r.path)
                return (
                  <tr
                    key={`${r.repo}${r.path}`}
                    data-testid={`search-result-${i}`}
                    tabIndex={0}
                    onClick={() => gotoNode(r)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') gotoNode(r)
                    }}
                  >
                    <td className="mono" lang="en">{r.repo}</td>
                    <td className="wrap">
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
                    </td>
                    <td className="mono">{formatBytes(Number(r.size) || 0)}</td>
                    <td className="mono">{r.lastModified ? r.lastModified.replace('T', ' ').slice(0, 19) : '—'}</td>
                    <td className="mono" title={r.checksums?.sha256 ?? ''}>
                      {r.checksums?.sha256 ? `${r.checksums.sha256.slice(0, 10)}…` : '—'}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
          {visible < rows.length && (
            <div style={{ marginTop: 12 }}>
              <button type="button" className="btn" data-testid="search-more" onClick={() => setVisible((v) => v + PAGE)}>
                加载更多（{Math.min(visible, rows.length)}/{rows.length}）
              </button>
            </div>
          )}
        </>
      )}
    </div>
  )
}
