import { forwardRef, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent, MouseEvent as ReactMouseEvent } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { useAuth } from '../../app/AuthContext'
import { useConfirm } from '../../components/ConfirmDialog'
import { CopyButton } from '../../components/CopyButton'
import DeployDialog from '../../components/DeployDialog'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import SetMeUpDialog from '../../components/SetMeUpDialog'
import { useToast } from '../../app/ToastContext'
import { ApiError, getRepositories, getStorageStats, isReadOnlyAdmin } from '../../lib/api'
import type { RepoListItem } from '../../lib/api'
import { formatBytes } from '../../lib/format'
import { getRepoDetail } from '../../lib/repos'
import type { PackageType } from '../../lib/repos'
import { useAsync } from '../../lib/useAsync'
import { clientCommands } from '../repositories/commands'

import NodeDetail from './NodeDetail'
import type { DownloadState } from './NodeDetail'
import type { ChildNode } from './lib'
import './browser.css'
// 共享样式（T-100 建立的树/上传/搜索样式族；搜索页仍从原址引入——完整
// 归位随 T-239/T-240 域票收口，本票不越界改 pages/search）
import '../repositories/tree/tree.css'
import {
  ancestorDirs,
  deleteNode,
  downloadArtifact,
  listChildren,
  mkdir,
  saveBlob,
  validateNameSegment,
} from './lib'

// 跨仓制品浏览器（console-m8 §6.3 / FR-72，T-236——Artifactory 树浏览器
// 对齐面；自 repositories/tree/TreePage 迁址重构）：
//
// - 左树：仓库为顶层节点（rclass×packageType 图标区分）+ 懒展开一层 +
//   「过滤仓库」前端过滤（仅已加载集）；URL 即状态（/artifacts/<repo>/
//   <path>，文件选中进 ?focus=），深链自动展开祖先并滚动定位（§4.3）。
//   两个过滤词都按作用域复位（QA-3 / FR-82-AC2）：跨层/跨仓导航清空，
//   同层内（?focus=、树展开）保留——语义论证见过滤状态声明处注释。
// - 右列：详情面板（仓库/目录/文件三形态 Tab：常规 + 有效权限）+ 当前层
//   children 表（目录在前，客户端分页 100/页「加载更多」）。
// - 右键菜单（console-m8 C3 的 BinFlow 对齐面）：文件=复制路径/下载/
//   删除；目录=复制路径/删除/刷新；仓库=复制仓库路径/刷新/在仓库管理中
//   打开。Move/Copy 无端点不建（零影子入口）。Shift+F10 / Menu 键可达。
// - 403 收敛（§2.2/§3.6）：GET /api/repositories 走 CapRepoRead——普通
//   user 403 → 树顶层 L2 无权限卡 + 搜索/直链引导；已知 repo key 的深链
//   按路径 ACL 可达（合成顶层节点）。repo 元数据 403 → generic 降级。
// - readonly_admin（T-218 债收口）：上传/建目录/删除禁用 + 只读注记；
//   普通用户写入口保留（服务端 403 行内呈现，W12d）。
// - 协议特化（P6 随迁）：docker 两级 + tag 徽标 + digest 列（T-134 G32a）；
//   上传入口仅 generic/maven local，docker/npm/pypi 以接入命令块替代。

const PAGE = 100
const BIG_DIR = 2000
/** 单层目录树节点渲染上限（懒加载一层 + content-visibility 之外的保险丝） */
const TREE_LEVEL_CAP = 300

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

/** 跨仓维度的目录缓存键 */
function ck(repoKey: string, dir: string): string {
  return `${repoKey}\n${dir}`
}

/** 右键菜单目标 */
type MenuTarget =
  | { kind: 'repo'; repoKey: string }
  | { kind: 'node'; repoKey: string; node: ChildNode }

export default function ArtifactsBrowser() {
  const { key: repoKeyParam } = useParams<{ key?: string }>()
  const repoKey = repoKeyParam ?? ''
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  // splat 已由 React Router 解码；去掉空段（尾斜杠）即 repo 相对目录
  const dir = (useParams()['*'] ?? '').split('/').filter((s) => s !== '').join('/')
  const focus = params.get('focus')
  const toast = useToast()
  const confirm = useConfirm()
  const { session } = useAuth()
  const admin = session?.admin ?? false
  const readOnly = isReadOnlyAdmin(session)

  // ---- 仓库列表（CapRepoRead：admin/readonly 全量；普通 user 403） ----
  const reposQuery = useAsync(() => getRepositories(), [])
  const repos: RepoListItem[] = useMemo(() => (reposQuery.status === 'ok' ? (reposQuery.data ?? []) : []), [
    reposQuery.status,
    reposQuery.data,
  ])

  // 空实例引导（console-m8 §1.1：创建仓库 + 跳过，跳过状态存 localStorage）
  const [onboardSkipped, setOnboardSkipped] = useState(() => localStorage.getItem('bf-skip-onboarding') === '1')

  // ---- 选中仓库元数据（admin 面）：403 → 未知协议，按 generic 降级 ----
  const meta = useAsync(() => (repoKey ? getRepoDetail(repoKey) : Promise.resolve(null)), [repoKey])
  const repoMeta = meta.status === 'ok' ? meta.data : null
  const packageType = repoMeta ? (repoMeta.packageType as PackageType) : undefined
  const rclass = repoMeta ? repoMeta.rclass : undefined
  const uploadable = repoMeta
    ? rclass === 'local' && (packageType === 'generic' || packageType === 'maven')
    : meta.status === 'forbidden'
  const mkdirable = repoMeta ? rclass === 'local' && packageType === 'generic' : meta.status === 'forbidden'
  const isDockerRepo = repoMeta ? packageType === 'docker' : false

  // ---- 目录加载（缓存 + 去重；键含 repo 维度——跨仓切换不复用脏缓存） ----
  const chain = useMemo(() => ancestorDirs(dir), [dir])
  const chainKey = chain.join('\n')
  const [dirState, setDirState] = useState<Record<string, DirStatus>>({})
  const cacheRef = useRef(new Map<string, ChildNode[]>())
  const inflightRef = useRef(new Set<string>())
  const [tick, setTick] = useState(0)
  /** 手动/深链展开的目录（跨仓复合键） */
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const expandedKey = Array.from(expanded).sort().join('\n')

  const loadDir = useCallback(
    (repo: string, d: string, force = false) => {
      const key = ck(repo, d)
      if (!force && (cacheRef.current.has(key) || inflightRef.current.has(key))) {
        const cached = cacheRef.current.get(key)
        if (cached) {
          setDirState((s) => ({ ...s, [key]: { status: 'ok', nodes: cached } }))
        }
        return
      }
      cacheRef.current.delete(key)
      inflightRef.current.add(key)
      setDirState((s) => ({ ...s, [key]: { status: 'loading' } }))
      // T-134 G32a: docker repo tree passes isDockerRepo for ?docker_tags enrichment
      listChildren(repo, d, undefined, repo === repoKey && isDockerRepo)
        .then((nodes) => {
          cacheRef.current.set(key, nodes)
          inflightRef.current.delete(key)
          setDirState((s) => ({ ...s, [key]: { status: 'ok', nodes } }))
        })
        .catch((err: unknown) => {
          inflightRef.current.delete(key)
          const apiErr = err instanceof ApiError ? err : new ApiError(0, String(err))
          setDirState((s) => ({
            ...s,
            [key]: apiErr.status === 403 ? { status: 'forbidden', error: apiErr } : { status: 'error', error: apiErr },
          }))
        })
    },
    [repoKey, isDockerRepo],
  )

  // T-134 G32a: isDockerRepo 翻转（repoMeta 异步到位）时缓存可能取于无
  // ?docker_tags 的请求——失效重取；切仓同样清缓存（docker 标志是仓级）
  useEffect(() => {
    cacheRef.current.clear()
    inflightRef.current.clear()
    setDirState({})
    setTick((t) => t + 1)
  }, [repoKey, isDockerRepo])

  useEffect(() => {
    if (!repoKey) return
    const wanted = new Set<string>([ck(repoKey, ''), ...chain.map((d) => ck(repoKey, d)), ...expanded])
    for (const key of wanted) {
      const sep = key.indexOf('\n')
      loadDir(key.slice(0, sep), key.slice(sep + 1))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- chain/expanded 以 join key 刻画
  }, [repoKey, chainKey, expandedKey, tick, loadDir])

  const refresh = useCallback(() => {
    cacheRef.current.clear()
    setDirState({})
    setTick((t) => t + 1)
    reposQuery.reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 全量刷新语义
  }, [reposQuery.reload])

  // 切仓/切目录：自动展开祖先链（深链 §4.3）
  useEffect(() => {
    if (!repoKey) return
    setExpanded((prev) => {
      const next = new Set(prev)
      for (const d of chain) next.add(ck(repoKey, d))
      return next
    })
    setVisible(PAGE)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- dir/repo 变化即重置
  }, [dir, repoKey])

  // 深链滚动定位：祖先链全部就绪后把当前节点滚进可视区
  const chainSettled = !!repoKey && chain.every((d) => dirState[ck(repoKey, d)]?.status === 'ok')
  useEffect(() => {
    if (!chainSettled) return
    const sel =
      dir === ''
        ? document.querySelector(`[data-testid="tree-repo-${CSS.escape(repoKey)}"]`)
        : document.querySelector(`[data-testid="tree-node-${CSS.escape(dir)}"]`)
    sel?.scrollIntoView({ block: 'nearest' })
  }, [chainSettled, repoKey, dir])

  // ---- 导航 / 选中（URL 即状态：目录进路径，文件进 ?focus=） ----
  const goTo = useCallback(
    (repo: string, d: string) => {
      navigate(`/artifacts/${encodeURIComponent(repo)}${d ? `/${encodeDir(d)}` : ''}`)
    },
    [navigate],
  )

  const selectFile = useCallback(
    (name: string | null) => {
      const next = new URLSearchParams()
      if (name) next.set('focus', name)
      setParams(next, { replace: false })
    },
    [setParams],
  )

  // ---- 过滤 / 分页（§6.3：只作用于已加载集，提示边界） ----
  const [repoFilter, setRepoFilter] = useState('')
  const [filter, setFilter] = useState('')
  const [filesOnly, setFilesOnly] = useState(false)
  const [visible, setVisible] = useState(PAGE)
  useEffect(() => setVisible(PAGE), [filter, filesOnly, dir])

  // 过滤复位（QA-3 / FR-82-AC2，T-246 终验观察）：「过滤当前层」是对特定
  // (repo, dir) 已加载 children 集合的谓词——词是用户对着那一层敲进去的。
  // 跨层导航（下钻 / 面包屑上跳 / 左树跳兄弟层）或跨仓切换后，旧词落在一
  // 个它从未针对过的新集合上，命中与否纯属巧合，空结果会被误读成「这一层
  // 没有东西」（QA-3 空树误导）；因此 (repo, dir) 任一维变化即清空。同层
  // 内导航（?focus= 换选中文件、树节点展开/收起）不动 children 集合，词
  // 的语义完整——保留。「只看文件」是跨层稳定的视图偏好、不是针对单层的
  // 词，跨层保留（仅它收窄出的空态文案单独呈现，见下方空态分支）。
  useEffect(() => {
    setFilter('')
  }, [repoKey, dir])
  // 「过滤仓库」在跨仓边界同界复位：进入某仓后保留它，当前仓分支会被从
  // 左树上滤掉（所在位置从树里消失——与 QA-3 同款的误导）；切回跨仓根
  // 也回到全量选择语境。
  useEffect(() => {
    setRepoFilter('')
  }, [repoKey])

  const cur = repoKey ? dirState[ck(repoKey, dir)] : undefined
  const rows = useMemo(() => {
    const nodes = cur?.status === 'ok' ? cur.nodes : []
    const f = filter.trim().toLowerCase()
    return nodes.filter((n) => (filesOnly ? !n.folder : true)).filter((n) => (f ? n.name.toLowerCase().includes(f) : true))
  }, [cur, filter, filesOnly])
  const total = cur?.status === 'ok' ? cur.nodes.length : 0

  // 选中文件（?focus= 对账到当前层节点）
  const selectedNode = focus && cur?.status === 'ok' ? cur.nodes.find((n) => n.name === focus) : undefined

  // ---- 操作：上传 / 建目录 / 删除 / 下载 / 右键菜单 ----
  // 对话框族（T-242）：页头动作区 Set Me Up / Deploy（console-m8 §6.3[1]）。
  // T-244 双 Deploy 入口收敛：面包屑位 tree-upload（UploadDialog）退役，
  // 页头 tree-deploy（DeployDialog）是浏览器上传唯一入口（console-m8
  // §6.3[1] 裁定）；空目录 CTA 与目录上下文经 preselectedDir 带入。
  const [smuOpen, setSmuOpen] = useState(false)
  const [deployOpen, setDeployOpen] = useState(false)
  const [deleteError, setDeleteError] = useState<{ path: string; err: ApiError } | null>(null)
  const [download, setDownload] = useState<DownloadState | null>(null)
  const [menu, setMenu] = useState<{ x: number; y: number; target: MenuTarget } | null>(null)
  const menuRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!menu) return
    const onDown = (e: globalThis.MouseEvent) => {
      if (!menuRef.current?.contains(e.target as Node)) setMenu(null)
    }
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === 'Escape') setMenu(null)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [menu])

  const openMenuAt = useCallback((x: number, y: number, target: MenuTarget) => {
    // 视口边缘钳制（菜单宽 ~200px / 高 ~160px）
    const px = Math.min(x, window.innerWidth - 220)
    const py = Math.min(y, window.innerHeight - 170)
    setMenu({ x: Math.max(8, px), y: Math.max(8, py), target })
  }, [])

  const doDownload = useCallback(
    async (repo: string, node: ChildNode, expectedSha: string) => {
      setDownload({ path: node.path, phase: 'loading', sha: '', match: undefined })
      try {
        const { blob, sha256 } = await downloadArtifact(repo, node.path)
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
    [toast],
  )

  const confirmDelete = useCallback(
    async (repo: string, node: ChildNode) => {
      const isCache = repoMeta?.rclass === 'remote'
      const ok = await confirm({
        title: node.folder ? '删除目录' : isCache ? '删除缓存' : '删除制品',
        danger: true,
        confirmLabel: '删除',
        body: (
          <p>
            将永久删除 <b className="mono" lang="en">{repo}/{node.path}</b>
            {node.folder ? '（目录及其全部内容）' : ''}。制品不可变，删除没有撤销。
            {isCache ? ' 这是 remote 缓存——删除后下次请求将重新回源。' : ''}
          </p>
        ),
      })
      if (!ok) return
      try {
        await deleteNode(repo, node.path, node.folder)
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
        setDeleteError({ path: `${repo}/${node.path}`, err: apiErr })
      }
    },
    [confirm, repoMeta, refresh, toast],
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
      await mkdir(repoKey, target)
      toast.success(`已创建目录 ${target}`)
      refresh()
      setExpanded((prev) => new Set(prev).add(ck(repoKey, target.replace(/\/$/, ''))))
    } catch (err) {
      const apiErr = err instanceof ApiError ? err : new ApiError(0, String(err))
      toast.error(`创建目录失败（HTTP ${apiErr.status}）：${apiErr.message}`)
    }
  }

  const commands = repoMeta && packageType && !uploadable ? clientCommands(packageType, repoKey) : []

  // 详情面板目标：选中文件 > 当前目录/仓库 > 无（跨仓根引导）
  const detailTarget = selectedNode
    ? { kind: 'node' as const, repoKey, node: selectedNode }
    : repoKey
      ? dir === ''
        ? { kind: 'repo' as const, repoKey }
        : {
            kind: 'node' as const,
            repoKey,
            node: {
              name: dir.split('/').pop() ?? '',
              path: dir,
              folder: true,
              size: null,
              lastModified: '',
              sha256: '',
            } satisfies ChildNode,
          }
      : null

  // 页脚标语行（§6.3[6]：stats admin 门，非 admin 隐藏）
  const stats = useAsync(() => (admin ? getStorageStats() : Promise.resolve(null)), [admin])

  // ---- 仓库列表（普通 user 403 → 深链合成顶层节点） ----
  const repoNodes: RepoListItem[] = useMemo(() => {
    if (reposQuery.status === 'ok') return repos
    if (reposQuery.status === 'forbidden' && repoKey) {
      return [{ key: repoKey, description: '', type: '', packageType: '', url: '' }]
    }
    return []
  }, [reposQuery.status, repos, repoKey])

  const filteredRepos = useMemo(() => {
    const f = repoFilter.trim().toLowerCase()
    return f ? repoNodes.filter((r) => r.key.toLowerCase().includes(f)) : repoNodes
  }, [repoNodes, repoFilter])

  // ---- 渲染 ----
  const emptyInstance = reposQuery.status === 'ok' && repos.length === 0 && !repoKey && !onboardSkipped

  return (
    <div data-testid="tree-page" className="tree-page browser-page">
      {/* 页头动作区（console-m8 §6.3[1]：Set Me Up / Deploy / 管理仓库） */}
      <div className="browser-toolbar">
        <div className="browser-actions">
          <button
            type="button"
            className="btn"
            data-testid="tree-setmeup"
            title="客户端接入向导（按包类型生成接入命令与令牌）"
            onClick={() => setSmuOpen(true)}
          >
            Set Me Up
          </button>
          <button
            type="button"
            className="btn primary"
            data-testid="tree-deploy"
            disabled={readOnly}
            title={readOnly ? '只读管理员不可写（服务端 403 兜底）' : '浏览器上传（local Generic / Maven 仓）'}
            onClick={() => setDeployOpen(true)}
          >
            ⬆ 部署 Deploy
          </button>
          {admin && (
            <Link className="btn" to="/admin/repositories/local">
              管理仓库 →
            </Link>
          )}
        </div>
        <div className="browser-filter">
          <input
            type="search"
            placeholder="过滤仓库…"
            aria-label="过滤仓库（仅已加载集）"
            data-testid="tree-repo-filter"
            value={repoFilter}
            onChange={(e) => setRepoFilter(e.target.value)}
            disabled={reposQuery.status !== 'ok'}
          />
          {repoFilter && (
            <button type="button" className="btn" data-testid="tree-repo-filter-clear" onClick={() => setRepoFilter('')}>
              清除
            </button>
          )}
        </div>
      </div>

      {emptyInstance ? (
        <section className="card section">
          <h3>这个实例还没有仓库</h3>
          <p className="text-2">创建第一个仓库后，全部制品会以跨仓树的形式展示在这里。</p>
          <p>
            {admin && (
              <Link className="btn primary" to="/admin/repositories/new">
                创建仓库
              </Link>
            )}{' '}
            <button
              type="button"
              className="btn"
              onClick={() => {
                localStorage.setItem('bf-skip-onboarding', '1')
                setOnboardSkipped(true)
              }}
            >
              跳过
            </button>
          </p>
        </section>
      ) : (
        <>
          {readOnly && (
            <p className="browser-readonly-note" data-testid="tree-readonly-note">
              当前会话是只读管理员（readonly_admin）：浏览面全量可见，上传/删除等写操作已禁用——服务端对写面一律 403 兜底。
            </p>
          )}

          <div className="tree-breadcrumb" data-testid="tree-breadcrumb">
            {repoKey ? (
              <>
                <button type="button" className="crumb" onClick={() => goTo(repoKey, '')}>
                  <span className="mono" lang="en">{repoKey}</span>
                </button>
                {dir !== '' &&
                  ancestorDirs(dir).map((d) => (
                    <span key={d} className="crumb-seg">
                      <span className="sep" aria-hidden="true">/</span>
                      <button type="button" className={`crumb${d === dir ? ' cur' : ''}`} onClick={() => goTo(repoKey, d)}>
                        <span className="mono" lang="en">{d.split('/').pop()}</span>
                      </button>
                    </span>
                  ))}
                <CopyButton value={dir === '' ? `${repoKey}/` : `${repoKey}/${dir}/`} label="当前路径" />
              </>
            ) : (
              <span className="text-2">在左侧选择仓库开始浏览</span>
            )}
            <span className="spacer" />
            <button type="button" className="btn" onClick={refresh} title="重新加载当前视图">
              ↻ 刷新
            </button>
            {mkdirable && (
              <button
                type="button"
                className="btn"
                data-testid="tree-mkdir"
                disabled={readOnly}
                title={readOnly ? '只读管理员不可写（服务端 403 兜底）' : undefined}
                onClick={() => void doMkdir()}
              >
                + 目录
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
              {commands.map((c) => (
                <div className="cmd-block" key={c.title}>
                  <header>
                    <span>{c.title}</span>
                    <CopyButton value={c.text} label={c.title} />
                  </header>
                  {/* tabIndex：可滚动区键盘可达（QA-1 同款，树页命令块同形面） */}
                  <pre lang="en" tabIndex={0}>{c.text}</pre>
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
                      <Link to="/admin/security/permissions" target="_blank">
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

          <div className="tree-layout browser-layout">
            {/* 左树：仓库顶层 + 懒展开子树（reverse §3.2/§4.3） */}
            <nav className="tree-pane" aria-label="制品树" data-testid="browser-tree">
              <div role="tree" aria-label="跨仓制品树">
                {reposQuery.status === 'loading' && <TreeSkeleton />}
                {reposQuery.status === 'error' && reposQuery.error && (
                  <ErrorCard error={reposQuery.error} onRetry={reposQuery.reload} />
                )}
                {reposQuery.status === 'forbidden' && !repoKey && (
                  <EmptyState
                    message="无权限列出仓库"
                    hint="仓库清单是管理员/只读管理员视图（HTTP 403）。可以用搜索定位制品，或用已知仓库 key 的链接直达。"
                    testid="tree-root-denied"
                    action={
                      <Link className="btn" to="/search">
                        去搜索
                      </Link>
                    }
                  />
                )}
                {(reposQuery.status === 'ok' || reposQuery.status === 'forbidden') &&
                  (filteredRepos.length === 0 ? (
                    <div className="tree-empty-level">
                      {repoFilter ? `没有匹配「${repoFilter}」的仓库（仅过滤已加载集）` : '（无仓库）'}
                    </div>
                  ) : (
                    filteredRepos.map((r) => (
                      <RepoBranch
                        key={r.key}
                        repo={r}
                        selectedRepo={repoKey}
                        currentDir={dir}
                        dirState={dirState}
                        expanded={expanded}
                        onToggle={(repo, d) =>
                          setExpanded((prev) => {
                            const next = new Set(prev)
                            const key = ck(repo, d)
                            if (next.has(key)) next.delete(key)
                            else next.add(key)
                            return next
                          })
                        }
                        onNavigate={goTo}
                        onMenu={openMenuAt}
                      />
                    ))
                  ))}
              </div>
            </nav>

            {/* 右列：详情面板（右联）+ 当前层 children 表 */}
            <div className="tree-main">
              {detailTarget && repoKey ? (
                <NodeDetail
                  target={detailTarget}
                  download={download}
                  onDownload={(n, sha) => void doDownload(repoKey, n, sha)}
                  onClose={() => selectFile(null)}
                  canDelete={!readOnly}
                  canWriteProps={!readOnly}
                  onDelete={(n) => void confirmDelete(repoKey, n)}
                />
              ) : (
                <section className="card section node-detail">
                  <h3>制品浏览器</h3>
                  <p className="text-2">
                    左侧是全部仓库的树：单击选中（此处联动详情），Enter 或展开箭头进入下一层。选中路径会进入
                    URL——可以直接分享或收藏深链，打开时自动展开定位。
                  </p>
                  <p className="text-2">
                    右键（或 <kbd>Shift+F10</kbd>）打开操作菜单：复制路径 / 下载 / 删除 / 刷新。
                  </p>
                </section>
              )}

              {repoKey && (
                <>
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
                      <input type="checkbox" checked={filesOnly} onChange={(e) => setFilesOnly(e.target.checked)} />
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
                        <button type="button" className="btn" onClick={() => goTo(repoKey, '')}>
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
                          uploadable && !readOnly ? (
                            <button
                              type="button"
                              className="btn primary"
                              onClick={() => {
                                setDeployOpen(true)
                              }}
                            >
                              上传第一个制品
                            </button>
                          ) : undefined
                        }
                      />
                    ) : filter.trim() !== '' ? (
                      // QA-3 / §3.1 空态：过滤后为空 ≠ 这一层没有内容——标准
                      // 文案 + 清除过滤（连「只看文件」一并复位，保证非空回呈现）
                      <EmptyState
                        message={`无匹配「${filter}」的条目`}
                        hint={
                          filesOnly
                            ? '过滤只作用于当前层已加载的条目，且「只看文件」正在一并收窄范围。'
                            : '过滤只作用于当前层已加载的条目。'
                        }
                        action={
                          <button
                            type="button"
                            className="btn"
                            data-testid="tree-filter-clear"
                            onClick={() => {
                              setFilter('')
                              setFilesOnly(false)
                            }}
                          >
                            清除过滤
                          </button>
                        }
                      />
                    ) : (
                      // 仅「只看文件」收窄出的空（无过滤词）：按实际谓词呈现，
                      // 不误报「无匹配」
                      <EmptyState
                        message="当前层没有文件（只有目录）"
                        hint="「只看文件」正在收窄列表。"
                        action={
                          <button
                            type="button"
                            className="btn"
                            data-testid="tree-filter-clear"
                            onClick={() => setFilesOnly(false)}
                          >
                            清除「只看文件」
                          </button>
                        }
                      />
                    )
                  ) : (
                    <>
                      {total > BIG_DIR && (
                        <div className="warn-box">
                          目录过大（{total} 项）：本页为客户端分页（children 契约暂无游标）。建议改用{' '}
                          <Link to="/search">搜索</Link> 定位制品。
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
                              className={focus === n.name ? 'selected' : ''}
                              onClick={() => (n.folder ? goTo(repoKey, n.path) : selectFile(n.name))}
                              onContextMenu={(e: ReactMouseEvent) => {
                                e.preventDefault()
                                openMenuAt(e.clientX, e.clientY, { kind: 'node', repoKey, node: n })
                              }}
                              tabIndex={0}
                              onKeyDown={(e) => {
                                if (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) {
                                  e.preventDefault()
                                  const rect = e.currentTarget.getBoundingClientRect()
                                  openMenuAt(rect.left + 60, rect.top + 14, { kind: 'node', repoKey, node: n })
                                } else if (e.key === 'Enter') {
                                  if (n.folder) goTo(repoKey, n.path)
                                  else selectFile(n.name)
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
                                  <button type="button" className="btn" onClick={() => selectFile(n.name)} title="展开详情面板">
                                    详情
                                  </button>
                                )}
                                {!n.folder && (
                                  <button
                                    type="button"
                                    className="btn"
                                    onClick={() => void doDownload(repoKey, n, n.sha256)}
                                    disabled={download?.path === n.path && download.phase === 'loading'}
                                    title="下载并做 sha256 对账"
                                  >
                                    下载
                                  </button>
                                )}
                                <button
                                  type="button"
                                  className="btn danger"
                                  data-testid="delete-node-button"
                                  disabled={readOnly}
                                  title={readOnly ? '只读管理员不可删（服务端 403 兜底）' : undefined}
                                  onClick={() => void confirmDelete(repoKey, n)}
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
                          <button
                            type="button"
                            className="btn"
                            data-testid="tree-load-more"
                            onClick={() => setVisible((v) => v + PAGE)}
                          >
                            加载更多（{Math.min(visible, rows.length)}/{rows.length}）
                          </button>
                        </div>
                      )}
                    </>
                  )}
                </>
              )}
            </div>
          </div>

          {/* 页脚标语行（§6.3[6]，对齐 Artifactory "Happily serving" 行） */}
          {stats.status === 'ok' && stats.data && (
            <p className="browser-footer">
              已服务 {stats.data.blobs.toLocaleString()} 个 blob · 逻辑容量 {formatBytes(stats.data.logical_bytes)}
            </p>
          )}
        </>
      )}

      {menu && (
        <TreeContextMenu
          ref={menuRef}
          menu={menu}
          readOnly={readOnly}
          canSeeAdmin={admin}
          repoRclass={repoMeta?.rclass}
          onClose={() => setMenu(null)}
          onCopyPath={(value) => {
            void navigator.clipboard.writeText(value).then(
              () => toast.success(`已复制 ${value}`),
              () => toast.error('剪贴板不可用'),
            )
          }}
          onDownload={(repo, node) => void doDownload(repo, node, node.sha256)}
          onDelete={(repo, node) => void confirmDelete(repo, node)}
          onRefresh={(repo) => {
            setMenu(null)
            if (repo === repoKey) refresh()
            else loadDir(repo, '', true)
          }}
          onOpenAdmin={(repo) => {
            setMenu(null)
            navigate(`/admin/repositories/${encodeURIComponent(repo)}`)
          }}
        />
      )}

      {smuOpen && (
        <SetMeUpDialog preselectedRepo={repoKey || undefined} onClose={() => setSmuOpen(false)} />
      )}
      {deployOpen && (
        <DeployDialog
          preselectedRepo={repoKey || undefined}
          preselectedDir={dir || undefined}
          onClose={() => setDeployOpen(false)}
          onUploaded={refresh}
        />
      )}
    </div>
  )
}

// ---- 左树：仓库顶层节点（rclass×packageType 图标区分，console-m8 C2） --------

const RC_ICON: Record<string, string> = { local: '▣', remote: '◈', virtual: '◍' }
const PKG_ICON: Record<string, string> = { generic: '▫', docker: '⬢', maven: '⌬', npm: '⬒', pypi: '⬓' }

function RepoBranch({
  repo,
  selectedRepo,
  currentDir,
  dirState,
  expanded,
  onToggle,
  onNavigate,
  onMenu,
}: {
  repo: RepoListItem
  selectedRepo: string
  currentDir: string
  dirState: Record<string, DirStatus>
  expanded: Set<string>
  onToggle: (repo: string, dir: string) => void
  onNavigate: (repo: string, dir: string) => void
  onMenu: (x: number, y: number, target: MenuTarget) => void
}) {
  const key = ck(repo.key, '')
  const isOpen = expanded.has(key) || selectedRepo === repo.key
  const st = dirState[key]

  return (
    <div>
      <div
        className={`tree-node repo-node${selectedRepo === repo.key ? ' on-chain' : ''}`}
        data-testid={`tree-repo-${repo.key}`}
        data-tree-row=""
        role="treeitem"
        aria-expanded={isOpen}
        aria-level={1}
        tabIndex={0}
        onClick={() => onNavigate(repo.key, '')}
        onContextMenu={(e: ReactMouseEvent) => {
          e.preventDefault()
          onMenu(e.clientX, e.clientY, { kind: 'repo', repoKey: repo.key })
        }}
        onKeyDown={(e) => onTreeKeys(e, { repo: repo.key, dir: '', isOpen }, onToggle, onNavigate, onMenu)}
      >
        <span
          role="button"
          tabIndex={-1}
          aria-label={isOpen ? `收起 ${repo.key}` : `展开 ${repo.key}`}
          className="twisty"
          onClick={(e) => {
            e.stopPropagation()
            onToggle(repo.key, '')
          }}
        >
          {isOpen ? '▾' : '▸'}
        </span>
        <span aria-hidden="true" className="ico" title={`${repo.type || '仓库'} · ${repo.packageType || '未知包类型'}`}>
          {RC_ICON[repo.type] ?? '▣'}
          {(repo.packageType && PKG_ICON[repo.packageType]) || ''}
        </span>
        <span className="mono" lang="en">{repo.key}</span>
      </div>
      {isOpen &&
        (!st || st.status === 'loading' ? (
          <div className="tree-skel" aria-hidden="true">
            {Array.from({ length: 6 }, (_, i) => (
              <div key={i} className="skeleton line" style={{ width: `${70 - i * 6}%` }} />
            ))}
          </div>
        ) : st.status === 'forbidden' ? (
          <div className="tree-denied" title={st.error.message}>
            ⃠ 无权限
          </div>
        ) : st.status === 'error' ? (
          <div className="tree-denied" title={st.error.message}>
            加载失败（HTTP {st.error.status}）
          </div>
        ) : (
          <TreeLevel
            repoKey={repo.key}
            dirPath=""
            depth={1}
            currentRepo={selectedRepo}
            currentDir={currentDir}
            dirState={dirState}
            expanded={expanded}
            onToggle={onToggle}
            onNavigate={onNavigate}
            onMenu={onMenu}
          />
        ))}
    </div>
  )
}

// ---- 左树：目录层（只渲染已展开路径 + 手动展开层——懒加载一层） ----------------

function TreeLevel({
  repoKey,
  dirPath,
  depth,
  currentRepo,
  currentDir,
  dirState,
  expanded,
  onToggle,
  onNavigate,
  onMenu,
}: {
  repoKey: string
  dirPath: string
  depth: number
  currentRepo: string
  currentDir: string
  dirState: Record<string, DirStatus>
  expanded: Set<string>
  onToggle: (repo: string, dir: string) => void
  onNavigate: (repo: string, dir: string) => void
  onMenu: (x: number, y: number, target: MenuTarget) => void
}) {
  const st = dirState[ck(repoKey, dirPath)]
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
  const capped = folders.slice(0, TREE_LEVEL_CAP)
  return (
    <>
      {capped.map((n) => {
        const key = ck(repoKey, n.path)
        const isOpen = expanded.has(key)
        const onChain = currentRepo === repoKey && (currentDir === n.path || currentDir.startsWith(`${n.path}/`))
        const selected = currentRepo === repoKey && currentDir === n.path
        return (
          <div key={n.name}>
            <div
              className={`tree-node${onChain ? ' on-chain' : ''}${selected ? ' selected' : ''}`}
              data-testid={`tree-node-${n.path}`}
              data-tree-row=""
              role="treeitem"
              aria-expanded={isOpen}
              aria-level={depth + 1}
              tabIndex={0}
              style={{ paddingLeft: depth * 14 + 6 }}
              onClick={() => onNavigate(repoKey, n.path)}
              onContextMenu={(e: ReactMouseEvent) => {
                e.preventDefault()
                onMenu(e.clientX, e.clientY, { kind: 'node', repoKey, node: n })
              }}
              onKeyDown={(e) => onTreeKeys(e, { repo: repoKey, dir: n.path, isOpen }, onToggle, onNavigate, onMenu)}
            >
              <span
                role="button"
                tabIndex={-1}
                aria-label={isOpen ? `收起 ${n.name}` : `展开 ${n.name}`}
                className="twisty"
                onClick={(e) => {
                  e.stopPropagation()
                  onToggle(repoKey, n.path)
                }}
              >
                {isOpen ? '▾' : '▸'}
              </span>
              <span aria-hidden="true" className="ico">◻</span>
              <span className="mono" lang="en">{n.name}</span>
            </div>
            {isOpen && (
              <TreeLevel
                repoKey={repoKey}
                dirPath={n.path}
                depth={depth + 1}
                currentRepo={currentRepo}
                currentDir={currentDir}
                dirState={dirState}
                expanded={expanded}
                onToggle={onToggle}
                onNavigate={onNavigate}
                onMenu={onMenu}
              />
            )}
          </div>
        )
      })}
      {folders.length > capped.length && (
        <div className="tree-empty-level">
          … 其余 {folders.length - capped.length} 个目录未渲染（单层上限 {TREE_LEVEL_CAP}）
        </div>
      )}
    </>
  )
}

/** §3.4/§8 树键盘语义：↑↓ 移动、→ 展开、← 收起、Enter 激活、Shift+F10 菜单 */
type TreeKeyPos = { repo: string; dir: string; isOpen: boolean }

function onTreeKeys(
  e: ReactKeyboardEvent<HTMLElement>,
  pos: TreeKeyPos,
  onToggle: (repo: string, dir: string) => void,
  onNavigate: (repo: string, dir: string) => void,
  onMenu: (x: number, y: number, target: MenuTarget) => void,
): void {
  // Shift+F10 在 Chromium 报 e.key === 'F10' + shiftKey（部分平台报
  // 'ContextMenu'）；Menu 键报 'ContextMenu'。三者都收。
  if (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) {
    e.preventDefault()
    const rect = e.currentTarget.getBoundingClientRect()
    if (pos.dir === '') {
      onMenu(rect.left + 40, rect.top + 14, { kind: 'repo', repoKey: pos.repo })
    } else {
      // 目录节点：菜单目标 = 该目录（删除/复制路径/刷新）
      onMenu(rect.left + 40, rect.top + 14, {
        kind: 'node',
        repoKey: pos.repo,
        node: { name: pos.dir.split('/').pop() ?? '', path: pos.dir, folder: true, size: null, lastModified: '', sha256: '' },
      })
    }
    return
  }
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
    if (!pos.isOpen) onToggle(pos.repo, pos.dir)
  } else if (e.key === 'ArrowLeft') {
    e.preventDefault()
    if (pos.isOpen) onToggle(pos.repo, pos.dir)
    else onNavigate(pos.repo, pos.dir.split('/').slice(0, -1).join('/'))
  } else if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    onNavigate(pos.repo, pos.dir)
  }
}

// ---- 右键上下文菜单（console-m8 C3；键盘可达 §3.4） --------------------------

interface MenuItem {
  id: string
  label: string
  disabled?: boolean
  title?: string
  run: () => void
}

const TreeContextMenu = forwardRef<
  HTMLDivElement,
  {
    menu: { x: number; y: number; target: MenuTarget }
    readOnly: boolean
    canSeeAdmin: boolean
    repoRclass?: string
    onClose: () => void
    onCopyPath: (value: string) => void
    onDownload: (repo: string, node: ChildNode) => void
    onDelete: (repo: string, node: ChildNode) => void
    onRefresh: (repo: string) => void
    onOpenAdmin: (repo: string) => void
  }
>(function TreeContextMenu(
  { menu, readOnly, canSeeAdmin, repoRclass, onClose, onCopyPath, onDownload, onDelete, onRefresh, onOpenAdmin },
  ref,
) {
  const t = menu.target
  const readonlyTitle = '只读管理员不可删（服务端 403 兜底）'
  const items: MenuItem[] =
    t.kind === 'repo'
      ? [
          { id: 'copy-repo-path', label: '复制仓库路径', run: () => { onCopyPath(`${t.repoKey}/`); onClose() } },
          { id: 'refresh', label: '刷新', run: () => onRefresh(t.repoKey) },
          ...(canSeeAdmin
            ? [{ id: 'open-admin', label: '在仓库管理中打开', run: () => onOpenAdmin(t.repoKey) }]
            : []),
        ]
      : t.node.folder
        ? [
            { id: 'copy-path', label: '复制路径', run: () => { onCopyPath(`${t.repoKey}/${t.node.path}/`); onClose() } },
            {
              id: 'delete',
              label: '删除',
              disabled: readOnly,
              title: readOnly ? readonlyTitle : undefined,
              run: () => { onClose(); onDelete(t.repoKey, t.node) },
            },
            { id: 'refresh', label: '刷新', run: () => onRefresh(t.repoKey) },
          ]
        : [
            { id: 'copy-path', label: '复制路径', run: () => { onCopyPath(`${t.repoKey}/${t.node.path}`); onClose() } },
            { id: 'download', label: '下载', run: () => { onClose(); onDownload(t.repoKey, t.node) } },
            {
              id: 'delete',
              label: repoRclass === 'remote' ? '删除缓存' : '删除',
              disabled: readOnly,
              title: readOnly ? readonlyTitle : undefined,
              run: () => { onClose(); onDelete(t.repoKey, t.node) },
            },
          ]

  const onMenuKey = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
    e.preventDefault()
    const focusables = Array.from(e.currentTarget.querySelectorAll<HTMLButtonElement>('button:not([disabled])'))
    if (focusables.length === 0) return
    const i = focusables.indexOf(document.activeElement as HTMLButtonElement)
    const next = e.key === 'ArrowDown' ? focusables[(i + 1) % focusables.length] : focusables[(i - 1 + focusables.length) % focusables.length]
    next?.focus()
  }

  return (
    <div
      ref={ref}
      className="context-menu"
      role="menu"
      aria-label="操作菜单"
      data-testid="tree-context-menu"
      style={{ left: menu.x, top: menu.y }}
      onKeyDown={onMenuKey}
    >
      {items.map((item, i) => (
        <button
          key={item.id}
          type="button"
          role="menuitem"
          className={`context-item${item.id === 'delete' && !item.disabled ? ' danger' : ''}`}
          data-testid={`tree-context-${item.id}`}
          disabled={item.disabled}
          title={item.title}
          autoFocus={i === 0}
          onClick={item.run}
        >
          {item.label}
        </button>
      ))}
    </div>
  )
})

function TreeSkeleton() {
  return (
    <div className="tree-skel" aria-hidden="true">
      {Array.from({ length: 8 }, (_, i) => (
        <div key={i} className="skeleton line" style={{ width: `${80 - i * 5}%` }} />
      ))}
    </div>
  )
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
