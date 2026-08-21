import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { useAuth } from '../../../app/AuthContext'
import { useConfirm } from '../../../components/ConfirmDialog'
import { CopyButton } from '../../../components/CopyButton'
import { EmptyState } from '../../../components/EmptyState'
import { ErrorCard } from '../../../components/ErrorCard'
import { useToast } from '../../../app/ToastContext'
import { ApiError } from '../../../lib/api'
import { formatBytes } from '../../../lib/format'
import { getRepoDetail } from '../../../lib/repos'
import type { PackageType } from '../../../lib/repos'
import { useAsync } from '../../../lib/useAsync'
import { clientCommands } from '../commands'

import NodeDetail from './NodeDetail'
import type { DownloadState } from './NodeDetail'
import type { ChildNode } from './lib'
import UploadDialog from './UploadDialog'
import './tree.css'
import {
  ancestorDirs,
  deleteNode,
  downloadArtifact,
  listChildren,
  mkdir,
  saveBlob,
  validateNameSegment,
} from './lib'

// 制品树浏览（console-ux §4.6；W12/W12b/W12c/W13/W12d）：
//
// - 左树懒加载一层（只渲染已展开路径 + 手动展开的目录），右表当前层
//   children（目录在前）；骨架屏按 §5.2（树 15 节点占位、表 10 行）。
// - 大目录策略（§6）：children 契约无游标（console-ux R1 兜底）——客户端
//   分页 100/页「加载更多」，>2000 提示改用搜索；行加 content-visibility
//   降渲染成本（真窗口化见遗留）。
// - 403 收敛（§3.6.3）：子树 403 = 该层无权限卡（L2 同款）；操作中 403
//   行内错误卡 + 权限指引（W12d）；repo 元数据（admin 面）403 = 按
//   generic 降级呈现，上传/删除按钮保留（内容面权限与 admin 无关）。
// - 协议差异（P6）：上传入口仅 generic/maven local；docker/npm/pypi 以
//   接入命令块替代（docker login 走 /v2/token 面，E4 勘误口径）。

const PAGE = 100
const BIG_DIR = 2000

type DirStatus =
  | { status: 'loading' }
  | { status: 'ok'; nodes: ChildNode[] }
  | { status: 'forbidden'; error: ApiError }
  | { status: 'error'; error: ApiError }

function encodeDir(dir: string): string {
  return dir
    .split('/')
    .filter((s) => s !== '')
    .map((s) => encodeURIComponent(s))
    .join('/')
}

export default function TreePage() {
  const { key: repoKey } = useParams<{ key: string }>()
  const navigate = useNavigate()
  const [params] = useSearchParams()
  // splat 已由 React Router 解码；去掉空段（尾斜杠）即 repo 相对目录
  const dir = (useParams()['*'] ?? '').split('/').filter((s) => s !== '').join('/')
  const toast = useToast()
  const confirm = useConfirm()
  const { session } = useAuth()
  const admin = session?.admin ?? false

  // 仓库元数据（admin 面）：403 → 未知协议，按 generic 降级（内容面不受影响）
  const meta = useAsync(() => (repoKey ? getRepoDetail(repoKey) : Promise.resolve(null)), [repoKey])
  const repoMeta = meta.status === 'ok' ? meta.data : null
  const packageType = repoMeta ? (repoMeta.packageType as PackageType) : undefined
  const rclass = repoMeta ? repoMeta.rclass : undefined
  const uploadable = repoMeta
    ? rclass === 'local' && (packageType === 'generic' || packageType === 'maven')
    : meta.status === 'forbidden'
  const mkdirable = repoMeta ? rclass === 'local' && packageType === 'generic' : meta.status === 'forbidden'
  const isDockerRepo = repoMeta ? packageType === 'docker' : false

  // ---- 目录加载（缓存 + 去重；链 = 根到当前路径的全部祖先） ----
  const chain = useMemo(() => ancestorDirs(dir), [dir])
  const chainKey = chain.join('\n')
  const [dirState, setDirState] = useState<Record<string, DirStatus>>({})
  const dirStateRef = useRef<Record<string, DirStatus>>({})
  dirStateRef.current = dirState
  const cacheRef = useRef(new Map<string, ChildNode[]>())
  const inflightRef = useRef(new Set<string>())
  const [tick, setTick] = useState(0)
  const [expanded, setExpanded] = useState<Set<string>>(new Set(chain))

  const loadDir = useCallback(
    (d: string, force = false) => {
      if (!repoKey) return
      if (!force && (cacheRef.current.has(d) || inflightRef.current.has(d))) {
        const cached = cacheRef.current.get(d)
        if (cached) {
          setDirState((s) => ({ ...s, [d]: { status: 'ok', nodes: cached } }))
        }
        return
      }
      cacheRef.current.delete(d)
      inflightRef.current.add(d)
      setDirState((s) => ({ ...s, [d]: { status: 'loading' } }))
      // T-134 G32a: docker repo tree passes isDockerRepo for ?docker_tags enrichment
      listChildren(repoKey, d, undefined, isDockerRepo)
        .then((nodes) => {
          cacheRef.current.set(d, nodes)
          inflightRef.current.delete(d)
          setDirState((s) => ({ ...s, [d]: { status: 'ok', nodes } }))
        })
        .catch((err: unknown) => {
          inflightRef.current.delete(d)
          const apiErr = err instanceof ApiError ? err : new ApiError(0, String(err))
          setDirState((s) => ({
            ...s,
            [d]: apiErr.status === 403 ? { status: 'forbidden', error: apiErr } : { status: 'error', error: apiErr },
          }))
        })
    },
    [repoKey, isDockerRepo],
  )

  // T-134 G32a: when isDockerRepo flips (e.g. repoMeta resolves),
  // cached dirs may have been fetched without ?docker_tags.  Invalidate
  // the cache so loadDir re-fetches with the correct querystring.
  useEffect(() => {
    cacheRef.current.clear()
    inflightRef.current.clear()
    setDirState({})
    setTick((t) => t + 1)
  }, [isDockerRepo])

  const expandedKey = Array.from(expanded).sort().join('\n')
  useEffect(() => {
    const wanted = new Set([...chain, ...expanded])
    for (const d of wanted) loadDir(d)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- chain/expanded 以 join key 刻画
  }, [chainKey, expandedKey, tick, loadDir])

  const refresh = useCallback(() => {
    cacheRef.current.clear()
    setDirState({})
    setTick((t) => t + 1)
  }, [])

  // ---- 导航 / 选中 ----
  const goTo = useCallback(
    (d: string) => {
      navigate(`/repositories/${encodeURIComponent(repoKey ?? '')}/tree${d ? `/${encodeDir(d)}` : ''}`)
    },
    [navigate, repoKey],
  )

  const [selected, setSelected] = useState<string | null>(null)
  useEffect(() => {
    setSelected(null)
    setExpanded((prev) => {
      const next = new Set(prev)
      for (const d of chain) next.add(d)
      return next
    })
    setVisible(PAGE)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- dir 变化即重置
  }, [dir, repoKey])

  // 搜索页深链定位（W14b：行点击跳树定位）——?focus=<name> 自动选中
  const focus = params.get('focus')
  const curStatus = dirState[dir]?.status
  useEffect(() => {
    if (!focus || curStatus !== 'ok') return
    const nodes = dirStateRef.current[dir]?.status === 'ok' ? dirStateRef.current[dir].nodes : []
    if (nodes.some((n) => n.name === focus)) setSelected(focus)
  }, [focus, curStatus, dir])

  // ---- 过滤 / 分页（§6.3：只作用于已加载集，提示边界） ----
  const [filter, setFilter] = useState('')
  const [filesOnly, setFilesOnly] = useState(false)
  const [visible, setVisible] = useState(PAGE)
  useEffect(() => setVisible(PAGE), [filter, filesOnly, dir])

  const cur = dirState[dir]
  const rows = useMemo(() => {
    const nodes = cur?.status === 'ok' ? cur.nodes : []
    const f = filter.trim().toLowerCase()
    return nodes.filter((n) => (filesOnly ? !n.folder : true)).filter((n) => (f ? n.name.toLowerCase().includes(f) : true))
  }, [cur, filter, filesOnly])
  const total = cur?.status === 'ok' ? cur.nodes.length : 0

  // ---- 操作：上传 / 建目录 / 删除 / 下载 ----
  const [uploadOpen, setUploadOpen] = useState(false)
  const [deleteError, setDeleteError] = useState<{ path: string; err: ApiError } | null>(null)
  const [download, setDownload] = useState<DownloadState | null>(null)

  const doDownload = useCallback(
    async (node: ChildNode, expectedSha: string) => {
      setDownload({ path: node.path, phase: 'loading', sha: '', match: undefined })
      try {
        const { blob, sha256 } = await downloadArtifact(repoKey ?? '', node.path)
        saveBlob(blob, node.name)
        const match = expectedSha ? expectedSha === sha256 : undefined
        setDownload({ path: node.path, phase: 'done', sha: sha256, match })
        if (match === false) toast.error(`下载完成，但 sha256 与服务端不一致（${node.name}）`)
        else toast.success(`下载完成${match ? '：sha256 与服务端一致' : ''}`)
      } catch (err) {
        setDownload(null)
        const apiErr = err instanceof ApiError ? err : new ApiError(0, String(err))
        toast.error(`下载失败（HTTP ${apiErr.status}）：${apiErr.message}`)
      }
    },
    [repoKey, toast],
  )

  const confirmDelete = useCallback(
    async (node: ChildNode) => {
      const isCache = rclass === 'remote'
      const ok = await confirm({
        title: node.folder ? '删除目录' : isCache ? '删除缓存' : '删除制品',
        danger: true,
        confirmLabel: '删除',
        body: (
          <>
            <p>
              将删除 <b className="mono" lang="en">{repoKey}/{node.path}</b>
              {node.folder ? '（目录及其全部内容）' : ''}。
              {isCache ? '这是 remote 缓存——删除后下次请求将重新回源。' : '制品不可变，删除没有撤销。'}
            </p>
          </>
        ),
      })
      if (!ok) return
      try {
        await deleteNode(repoKey ?? '', node.path, node.folder)
        toast.success(`已删除 ${node.path}`)
        setDeleteError(null)
        refresh()
      } catch (err) {
        const apiErr = err instanceof ApiError ? err : new ApiError(0, String(err))
        if (apiErr.status === 404) {
          // E-14 幂等：路径已不存在 = 目标状态已达成
          toast.success(`已删除 ${node.path}（此前已不存在——删除幂等）`)
          refresh()
          return
        }
        setDeleteError({ path: node.path, err: apiErr })
      }
    },
    [confirm, rclass, repoKey, refresh, toast],
  )

  const doMkdir = async () => {
    const holder = { name: '' }
    const ok = await confirm({
      title: '新建目录',
      body: (
        <>
          <p>
            在 <b className="mono" lang="en">{dir === '' ? `${repoKey}/（根）` : `${repoKey}/${dir}/`}</b> 下创建目录
            （尾斜杠 PUT，E-15 mkdir 语义）。
          </p>
          <div className="field" style={{ marginBottom: 0 }}>
            <input
              className="confirm-input mono"
              autoComplete="off"
              placeholder="目录名"
              data-testid="tree-mkdir-input"
              onChange={(e) => {
                holder.name = e.target.value
              }}
            />
          </div>
        </>
      ),
      confirmDisabled: () => validateNameSegment(holder.name.trim()) !== null,
    })
    if (!ok) return
    const name = holder.name.trim()
    const target = dir === '' ? `${name}/` : `${dir}/${name}/`
    try {
      await mkdir(repoKey ?? '', target)
      toast.success(`已创建目录 ${target}`)
      refresh()
      setExpanded((prev) => new Set(prev).add(target.replace(/\/$/, '')))
    } catch (err) {
      const apiErr = err instanceof ApiError ? err : new ApiError(0, String(err))
      toast.error(`创建目录失败（HTTP ${apiErr.status}）：${apiErr.message}`)
    }
  }

  // ---- 渲染 ----
  if (!repoKey) return null

  const selectedNode = selected && cur?.status === 'ok' ? cur.nodes.find((n) => n.name === selected) : undefined
  const commands = repoMeta && packageType && !uploadable ? clientCommands(packageType, repoKey) : []

  return (
    <div data-testid="tree-page" className="tree-page">
      <p style={{ margin: '0 0 var(--bf-sp-2)' }}>
        <Link to={`/repositories/${encodeURIComponent(repoKey)}`}>← {repoKey}</Link>
        {repoMeta && (
          <>
            {' '}
            <span className="badge neutral">{rclass}</span> <span className="badge neutral">{packageType}</span>
          </>
        )}
      </p>

      <div className="tree-breadcrumb" data-testid="tree-breadcrumb">
        <button type="button" className="crumb" onClick={() => goTo('')}>
          <span className="mono" lang="en">{repoKey}</span>
        </button>
        {dir !== '' &&
          ancestorDirs(dir).map((d) => (
            <span key={d} className="crumb-seg">
              <span className="sep" aria-hidden="true">/</span>
              <button type="button" className={`crumb${d === dir ? ' cur' : ''}`} onClick={() => goTo(d)}>
                <span className="mono" lang="en">{d.split('/').pop()}</span>
              </button>
            </span>
          ))}
        <CopyButton value={dir === '' ? `${repoKey}/` : `${repoKey}/${dir}/`} label="当前路径" />
        <span className="spacer" />
        <button type="button" className="btn" data-testid="tree-refresh" onClick={refresh} title="重新加载当前视图">
          ↻ 刷新
        </button>
        {mkdirable && (
          <button type="button" className="btn" data-testid="tree-mkdir" onClick={() => void doMkdir()}>
            + 目录
          </button>
        )}
        {uploadable && (
          <button type="button" className="btn primary" data-testid="tree-upload" onClick={() => setUploadOpen(true)}>
            ⬆ 上传
          </button>
        )}
      </div>

      {meta.status === 'forbidden' && (
        <div className="warn-box">
          仓库元数据为管理员视图（HTTP 403）——树按 generic 语义呈现；上传/删除权限由内容面按路径 ACL 判定，操作被拒时原因会在此原样呈现。
        </div>
      )}

      {commands.length > 0 && (
        <section className="card section" data-testid="tree-commands">
          <h3>此协议不走浏览器上传</h3>
          <p className="text-2">
            {packageType === 'docker'
              ? 'docker 是 POST/PATCH/PUT 三步会话协议——经 docker 客户端推送；登录经 /v2/token（registry 面，与控制台会话无关）。'
              : `${packageType} 的发布协议是 multipart/packument 形态——请用对应客户端发布。`}
            以下命令与仓库详情页同源：
          </p>
          {commands.map((c, i) => (
            <div className="cmd-block" key={c.title} data-testid={`repo-cmd-${packageType}-${i}`}>
              <header>
                <span>{c.title}</span>
                <CopyButton value={c.text} label={c.title} />
              </header>
              <pre lang="en">{c.text}</pre>
              {c.note && <div className="note">{c.note}</div>}
            </div>
          ))}
        </section>
      )}

      {repoMeta && rclass === 'virtual' && (
        <div className="warn-box">
          virtual 仓是聚合视图：浏览合并各成员；上传/删除不经 virtual（删除走成员仓本身——BinFlow 有意不兼容，RE-08）。
        </div>
      )}
      {repoMeta && rclass === 'remote' && (
        <div className="warn-box">remote 仓浏览的是已缓存内容；首次访问的路径需经客户端拉取后才会出现在树上。</div>
      )}

      {deleteError && (
        <div className="form-error" data-testid="delete-error" role="alert">
          <div className="headline">
            删除 <span className="mono" lang="en">{deleteError.path}</span> 失败（HTTP {deleteError.err.status}）
          </div>
          <div className="raw" lang="en">{deleteError.err.message}</div>
          {deleteError.err.status === 403 && (
            <div>
              当前会话没有对该路径的 delete 权限（read-only）。权限模型按 permission target 的路径 pattern 授予——
              {admin ? (
                <>
                  需要管理员在{' '}
                  <Link to="/security/permissions" target="_blank">
                    权限 target
                  </Link>{' '}
                  里给该路径加 delete 动作。
                </>
              ) : (
                <>请联系管理员为你的账号授予该路径的 delete 动作。</>
              )}
            </div>
          )}
          <div style={{ marginTop: 8 }}>
            <button type="button" className="btn" onClick={() => setDeleteError(null)}>
              知道了
            </button>
          </div>
        </div>
      )}

      <div className="tree-layout">
        <nav className="tree-pane" aria-label="目录树">
          <TreeLevel
            dirPath=""
            depth={0}
            currentDir={dir}
            dirState={dirState}
            expanded={expanded}
            onToggle={(d) =>
              setExpanded((prev) => {
                const next = new Set(prev)
                if (next.has(d)) next.delete(d)
                else next.add(d)
                return next
              })
            }
            onNavigate={goTo}
          />
        </nav>

        <div className="tree-main">
          <div className="filter-bar">
            <input
              type="search"
              placeholder="过滤当前层（仅已加载集）…"
              aria-label="过滤当前层"
              data-testid="tree-filter"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            />
            <label className="check-row">
              <input
                type="checkbox"
                checked={filesOnly}
                onChange={(e) => setFilesOnly(e.target.checked)}
              />
              只看文件
            </label>
            <span className="count">
              {total > 0 && <>共 {total} 项 · 已显示 {Math.min(visible, rows.length)}</>}
            </span>
          </div>

          {cur?.status === 'loading' || !cur ? (
            <TableSkeleton />
          ) : cur.status === 'forbidden' ? (
            <EmptyState
              message="无权限浏览此目录"
              hint={`内容面按路径 ACL 判定（${cur.error.message}）。可回到有权限的层级，或用搜索定位制品。`}
              action={
                <Link className="btn" to="/search">
                  去搜索
                </Link>
              }
            />
          ) : cur.status === 'error' && cur.error.status === 404 ? (
            <EmptyState
              message="路径不存在"
              hint="节点可能已被删除，或链接里的路径有误。"
              action={
                <button type="button" className="btn" onClick={() => goTo('')}>
                  ← 回仓库根
                </button>
              }
            />
          ) : cur.status === 'error' && cur.error ? (
            <ErrorCard error={cur.error} onRetry={refresh} />
          ) : rows.length === 0 ? (
            total === 0 ? (
              <EmptyState
                message="此目录为空"
                hint={uploadable ? '上传第一个制品，或创建子目录组织布局。' : '此仓库尚无内容。'}
                testid="tree-empty-dir"
                action={
                  uploadable ? (
                    <button type="button" className="btn primary" onClick={() => setUploadOpen(true)}>
                      上传第一个制品
                    </button>
                  ) : undefined
                }
              />
            ) : (
              <EmptyState message={`无匹配「${filter}」的条目`} hint="过滤只作用于当前层已加载的条目。" />
            )
          ) : (
            <>
              {total > BIG_DIR && (
                <div className="warn-box">
                  目录过大（{total} 项）：本页为客户端分页（children 契约暂无游标）。建议改用{' '}
                  <Link to="/search">搜索</Link> 或 ?list 前缀列举定位。
                </div>
              )}
              <table className="table tree-table" data-testid="tree-list">
                <thead>
                  <tr>
                    <th>名称</th>
                    {isDockerRepo ? <th>标签</th> : <th>类型</th>}
                    <th>大小</th>
                    <th>修改时间</th>
                    <th>{isDockerRepo ? '摘要' : 'sha256'}</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.slice(0, visible).map((n) => (
                    <tr
                      key={n.name}
                      data-testid={`tree-row-${n.name}`}
                      className={selected === n.name ? 'selected' : ''}
                      onClick={() => (n.folder ? goTo(n.path) : setSelected(n.name))}
                      tabIndex={0}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          if (n.folder) goTo(n.path)
                          else setSelected(n.name)
                        }
                      }}
                    >
                      <td className="wrap">
                        <span aria-hidden="true">{n.folder ? '◻' : '◾'}</span>{' '}
                        <span className={n.folder ? 'row-link mono' : 'mono'} lang="en">
                          {n.name}
                        </span>
                      </td>
                      {isDockerRepo ? (
                        <td>
                          {n.tags && n.tags.length > 0
                            ? n.tags.map((tag) => (
                                <span
                                  key={tag}
                                  className="badge neutral"
                                  data-testid={`tag-badge-${tag}`}
                                  title={`tag: ${tag}`}
                                >
                                  {tag}
                                </span>
                              ))
                            : !n.folder
                              ? <span className="badge warning">untagged</span>
                              : '—'}
                        </td>
                      ) : (
                        <td>{n.folder ? '目录' : '文件'}</td>
                      )}
                      <td className="mono">{n.folder ? '—' : n.size !== null ? formatBytes(n.size) : '—'}</td>
                      <td className="mono">{n.lastModified ? n.lastModified.replace('T', ' ').slice(0, 19) : '—'}</td>
                      <td className="mono" title={n.sha256}>
                        {n.sha256 ? `${n.sha256.slice(0, 10)}…` : '—'}
                      </td>
                      <td onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
                        {!n.folder && (
                          <button
                            type="button"
                            className="btn"
                            onClick={() => setSelected(n.name)}
                            title="展开详情面板"
                          >
                            详情
                          </button>
                        )}
                        {!n.folder && (
                          <button
                            type="button"
                            className="btn"
                            onClick={() => void doDownload(n, n.sha256)}
                            disabled={download?.path === n.path && download.phase === 'loading'}
                            title="下载并做 sha256 对账"
                          >
                            下载
                          </button>
                        )}
                        <button
                          type="button"
                          className="btn danger"
                          data-testid={`delete-node-button`}
                          onClick={() => void confirmDelete(n)}
                        >
                          删除
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {visible < rows.length && (
                <div style={{ marginTop: 12 }}>
                  <button type="button" className="btn" data-testid="tree-load-more" onClick={() => setVisible((v) => v + PAGE)}>
                    加载更多（{Math.min(visible, rows.length)}/{rows.length}）
                  </button>
                </div>
              )}
            </>
          )}

          {selectedNode && (
            <NodeDetail
              repoKey={repoKey}
              node={selectedNode}
              download={download}
              onDownload={(n, sha) => void doDownload(n, sha)}
              onClose={() => setSelected(null)}
            />
          )}
        </div>
      </div>

      {uploadOpen && (
        <UploadDialog
          repoKey={repoKey}
          mode={packageType === 'maven' ? 'maven' : 'generic'}
          dir={dir}
          onClose={() => setUploadOpen(false)}
          onUploaded={refresh}
        />
      )}
    </div>
  )
}

// ---- 左树：只渲染已展开路径 + 手动展开层（懒加载一层） ----

function TreeLevel({
  dirPath,
  depth,
  currentDir,
  dirState,
  expanded,
  onToggle,
  onNavigate,
}: {
  dirPath: string
  depth: number
  currentDir: string
  dirState: Record<string, DirStatus>
  expanded: Set<string>
  onToggle: (dir: string) => void
  onNavigate: (dir: string) => void
}) {
  const st = dirState[dirPath]
  if (!st || st.status === 'loading') {
    return (
      <div className="tree-skel" aria-hidden="true">
        {Array.from({ length: Math.min(6, 15 - depth * 2) }, (_, i) => (
          <div key={i} className="skeleton line" style={{ width: `${76 - depth * 10 - i * 6}%` }} />
        ))}
      </div>
    )
  }
  if (st.status === 'forbidden') {
    return (
      <div className="tree-denied" title={st.error.message}>
        ⃠ 无权限
      </div>
    )
  }
  if (st.status === 'error') {
    return (
      <div className="tree-denied" title={st.error.message}>
        加载失败（HTTP {st.error.status}）
      </div>
    )
  }
  const folders = st.nodes.filter((n) => n.folder)
  if (folders.length === 0) {
    return <div className="tree-empty-level">（空）</div>
  }
  return (
    <>
      {folders.map((n) => {
        const isOpen = expanded.has(n.path)
        const onChain = currentDir === n.path || currentDir.startsWith(`${n.path}/`)
        return (
          <div key={n.name}>
            <div
              className={`tree-node${onChain ? ' on-chain' : ''}`}
              data-testid={`tree-node-${n.path}`}
              data-tree-row=""
              role="treeitem"
              aria-expanded={isOpen}
              tabIndex={0}
              style={{ paddingLeft: depth * 14 + 6 }}
              onClick={() => onNavigate(n.path)}
              onKeyDown={(e) => onTreeKeys(e, n.path, isOpen, onToggle, onNavigate)}
            >
              <span
                role="button"
                tabIndex={-1}
                aria-label={isOpen ? `收起 ${n.name}` : `展开 ${n.name}`}
                className="twisty"
                onClick={(e) => {
                  e.stopPropagation()
                  onToggle(n.path)
                }}
              >
                {isOpen ? '▾' : '▸'}
              </span>
              <span aria-hidden="true" className="ico">◻</span>
              <span className="mono" lang="en">{n.name}</span>
            </div>
            {isOpen && (
              <TreeLevel
                dirPath={n.path}
                depth={depth + 1}
                currentDir={currentDir}
                dirState={dirState}
                expanded={expanded}
                onToggle={onToggle}
                onNavigate={onNavigate}
              />
            )}
          </div>
        )
      })}
    </>
  )
}

/** §8 树键盘语义：↑↓ 移动、→ 展开、← 收起 */
function onTreeKeys(
  e: ReactKeyboardEvent<HTMLElement>,
  path: string,
  isOpen: boolean,
  onToggle: (d: string) => void,
  onNavigate: (d: string) => void,
): void {
  const rows = Array.from(document.querySelectorAll<HTMLElement>('[data-tree-row]'))
  const idx = rows.indexOf(e.currentTarget)
  if (e.key === 'ArrowDown' && rows[idx + 1]) {
    e.preventDefault()
    rows[idx + 1].focus()
  } else if (e.key === 'ArrowUp' && rows[idx - 1]) {
    e.preventDefault()
    rows[idx - 1].focus()
  } else if (e.key === 'ArrowRight') {
    e.preventDefault()
    if (!isOpen) onToggle(path)
  } else if (e.key === 'ArrowLeft') {
    e.preventDefault()
    if (isOpen) onToggle(path)
    else onNavigate(path.split('/').slice(0, -1).join('/'))
  } else if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    onNavigate(path)
  }
}

function TableSkeleton() {
  return (
    <div data-testid="skeleton" aria-hidden="true" style={{ paddingTop: 8 }}>
      {Array.from({ length: 10 }, (_, i) => (
        <div key={i} className="skeleton line" style={{ width: `${90 - i * 5}%` }} />
      ))}
    </div>
  )
}
