// Artifact Explorer（P2 第一优先——总令 §九全项；audit §2.2 语义承接）：
//
// - 左树 TanStack Virtual 虚拟化（懒单层加载语义保留——TREE_LEVEL_CAP
//   300 的保险丝由真虚拟滚动取代）；AG Grid children 面（无限行模型）；
// - URL 即状态：页签段 + 文件末段（?focus= 一次性折入）；深链自动展开
//   祖先链 + 滚动定位；
// - 工具带（facet/rclass/sort/favorites/compacted——localStorage 键
//   bf-tree-favorites / bf-tree-compacted 原样迁移）；
// - 右键上下文菜单（tree-context-* 族 + copy/move 解锁项）+ 多选批量
//   动作（复制/移动/删除）；
// - BIG_DIR 警示 + docker 特化列（AG Grid 列族）；
// - 上传/接入 = 旧 DeployDialog / SetMeUpDialog 经 LegacyDialogHost 挂载
//   （MUI 树——终验强删项）；属性页签 = 旧 PropertiesTab 同款嵌挂。
// - 页根锚 tree-page。
import { lazy, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { Button, ButtonAsChild } from '@/components/ui/button'
import { useAuth } from '@/app/AuthContext'
import { useConfirm } from '@/app/providers'
import { toast } from 'sonner'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState } from '@/components/layout/states'
import { LegacyDialogHost } from '@/app/router/legacy-bridge'
import { ApiError, isReadOnlyAdmin } from '@/lib/api'
import { cfgBool, cfgStrList } from '@/lib/repos'
import type { PackageType } from '@/lib/repos'
import { formatBytes } from '@/lib/format'
import {
  useDirQueries,
  useRepoListInvalidate,
  useRepoMetaQuery,
  useRepositoriesQuery,
  useStorageStatsQuery,
  useTreeCacheClear,
} from '@/features/artifacts/hooks'
import type { DirEntry } from '@/features/artifacts/hooks'
import { ancestorDirs, deleteNode, downloadArtifact, mkdir, saveBlob, validateNameSegment } from '@/pages/artifacts/lib'
import type { ChildNode } from '@/pages/artifacts/lib'
import { clientCommands } from '@/pages/repositories/commands'
import { tr } from '@/i18n'

import { ChildrenGrid } from './ChildrenGrid'
import { CopyMoveDialog } from './CopyMoveDialog'
import { DetailInspector } from './DetailInspector'
import type { DetailTarget } from './DetailInspector'
import { TreePanel, buildTreeRows } from './TreePanel'
import { SLUG_TO_TAB, TAB_SLUG } from './model'
import type { DetailTab, DownloadState, MenuTarget, TreeSort } from './model'

const tt = tr('artifacts')

// 旧 DeployDialog / SetMeUpDialog（MUI 树——终验强删项清单成员；
// LegacyDialogHost 自带 Suspense 回退）
const DeployDialog = lazy(() => import('@/components/DeployDialog'))
const SetMeUpDialog = lazy(() => import('@/components/SetMeUpDialog'))

// ---- localStorage 偏好键（PREF_KEYS 契约——与旧实现逐字一致） ----
const FAV_KEY = 'bf-tree-favorites'
const COMPACT_KEY = 'bf-tree-compacted'

function loadFavorites(): Set<string> {
  try {
    const raw = localStorage.getItem(FAV_KEY)
    const arr = raw ? (JSON.parse(raw) as unknown) : []
    return new Set(Array.isArray(arr) ? arr.filter((s) => typeof s === 'string') : [])
  } catch {
    return new Set()
  }
}

function compareRepos(a: { key: string; packageType?: string; type?: string }, b: { key: string; packageType?: string; type?: string }, sort: TreeSort): number {
  if (sort === 'pkg') {
    const d = (a.packageType || '').localeCompare(b.packageType || '')
    if (d !== 0) return d
  } else if (sort === 'rclass') {
    const d = (a.type || '').localeCompare(b.type || '')
    if (d !== 0) return d
  }
  return a.key.localeCompare(b.key)
}

function buildTreeUrl(tab: DetailTab, repo: string, segs: string[]): string {
  const slug = tab === 'general' ? '' : `${TAB_SLUG[tab]}/`
  if (!repo) return '/artifacts'
  const path = segs
    .join('/')
    .split('/')
    .filter((s) => s !== '')
    .map((s) => encodeURIComponent(s))
    .join('/')
  return `/artifacts/${slug}${encodeURIComponent(repo)}${path ? `/${path}` : ''}`
}

export default function ExplorerPage() {
  const routeParams = useParams<{ tab?: string; key?: string }>()
  const splatRaw = useParams()['*'] ?? ''
  const navigate = useNavigate()
  const location = useLocation()
  const [params] = useSearchParams()
  const { confirm, prompt } = useConfirm()
  const { session } = useAuth()
  const admin = session?.admin ?? false
  const readOnly = isReadOnlyAdmin(session)
  const canSeeAdmin = admin || readOnly
  const invalidateRepoList = useRepoListInvalidate()
  const clearTreeCache = useTreeCacheClear()

  // ---- URL 解析（页签段 + 文件路径段；?focus= 与多段旧形一次性折入）----
  const tabParam = routeParams.tab
  const keyParam = routeParams.key ?? ''
  const focusParam = params.get('focus')
  const splatKey = splatRaw.split('/').filter((s) => s !== '').join('\n')
  const parsed = useMemo(() => {
    let tab: DetailTab = 'general'
    let repo = ''
    let segs: string[] = []
    let legacy = false
    const splatSegs = splatKey === '' ? [] : splatKey.split('\n')
    if (tabParam !== undefined) {
      if (tabParam in SLUG_TO_TAB) {
        tab = SLUG_TO_TAB[tabParam]
        repo = keyParam
        segs = splatSegs
      } else {
        legacy = true
        repo = tabParam
        segs = [keyParam, ...splatSegs].filter((s) => s !== '')
      }
    } else if (keyParam) {
      if (keyParam in SLUG_TO_TAB) tab = SLUG_TO_TAB[keyParam]
      else {
        repo = keyParam
        segs = splatSegs
      }
    }
    if (focusParam) {
      if (repo) segs = [...segs, focusParam]
      legacy = true
    }
    return { tab, repo, segs, legacy }
  }, [tabParam, keyParam, splatKey, focusParam])

  const repoKey = parsed.repo
  const tab = parsed.tab
  const legacyTarget = parsed.legacy ? buildTreeUrl(parsed.tab, parsed.repo, parsed.segs) : null
  useEffect(() => {
    if (legacyTarget && location.pathname !== legacyTarget) navigate(legacyTarget, { replace: true })
  }, [legacyTarget, location.pathname, navigate])

  // ---- 数据面 ----
  const reposQuery = useRepositoriesQuery()
  const repos = useMemo(() => (reposQuery.isSuccess ? (reposQuery.data ?? []) : []), [reposQuery.isSuccess, reposQuery.data])
  const meta = useRepoMetaQuery(repoKey)
  const repoMeta = meta.isSuccess ? meta.data : null
  const packageType = repoMeta ? (repoMeta.packageType as PackageType) : undefined
  const rclass = repoMeta ? repoMeta.rclass : undefined
  const uploadable = repoMeta
    ? repoMeta.rclass === 'local' && (packageType === 'generic' || packageType === 'maven')
    : meta.forbidden
  const mkdirable = repoMeta ? repoMeta.rclass === 'local' && packageType === 'generic' : meta.forbidden
  const isDockerRepo = repoMeta ? packageType === 'docker' : false
  const isVirtual = repoMeta ? repoMeta.rclass === 'virtual' : false
  const virtualMembers = repoMeta ? cfgStrList(repoMeta.configuration, 'repositories') : []
  const remoteBrowseOn =
    repoMeta !== null && repoMeta.rclass === 'remote' && cfgBool(repoMeta.configuration, 'listRemoteFolderItems')

  // ---- 路径末段分类（文件是路径段——父 listing 判别；T-494 分类门）----
  const pathSegs = parsed.segs
  const fullDir = pathSegs.join('/')
  const parentDir = pathSegs.length > 0 ? pathSegs.slice(0, -1).join('/') : ''
  const lastSeg = pathSegs.length > 0 ? pathSegs[pathSegs.length - 1] : ''

  // ---- 展开集 + 想要装载的目录集（懒单层语义）----
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const expandedKey = Array.from(expanded).sort().join('\n')

  // 分类门需要父 listing 状态——先装载「选中仓根 + 乐观链 ∪ 展开集」的
  // 第一轮（不含未决末段），再据分类补装
  const wantedBase = useMemo(() => {
    const wanted: DirEntry[] = []
    if (repoKey) {
      wanted.push({ repo: repoKey, dir: '' })
      for (const d of ancestorDirs(parentDir)) {
        wanted.push({ repo: repoKey, dir: d })
      }
    }
    for (const key of expandedKey === '' ? [] : expandedKey.split('\n')) {
      const sep = key.indexOf('\n')
      wanted.push({ repo: key.slice(0, sep), dir: key.slice(sep + 1) })
    }
    return wanted
  }, [repoKey, parentDir, expandedKey])

  const dirStateBase = useDirQueries(wantedBase, isDockerRepo)
  const parentSt = repoKey ? dirStateBase.get(`${repoKey}\n${parentDir}`) : undefined
  const parentSettled =
    pathSegs.length === 0 ||
    parentSt?.status === 'ok' ||
    parentSt?.status === 'forbidden' ||
    parentSt?.status === 'error'
  const lastChild =
    parentSt?.status === 'ok' && lastSeg !== '' ? parentSt.nodes.find((n) => n.name === lastSeg) : undefined
  const fileSelected = !!(lastChild && !lastChild.folder)
  const dir = fileSelected ? parentDir : fullDir
  const focusFile = fileSelected ? lastSeg : null
  const focusPath = focusFile !== null ? (dir !== '' ? `${dir}/${focusFile}` : focusFile) : null
  const curUncertain = pathSegs.length > 0 && !parentSettled

  // 末段分类落定后：目录链补装（文件段永不装载——T-494 双保险）
  const chain = useMemo(() => ancestorDirs(dir), [dir])
  const chainKey = chain.join('\n')
  const wantedFull = useMemo(() => {
    if (curUncertain || fileSelected) return wantedBase
    const extra: DirEntry[] = chain.map((d) => ({ repo: repoKey, dir: d }))
    const seen = new Set(wantedBase.map((w) => `${w.repo}\n${w.dir}`))
    return [...wantedBase, ...extra.filter((e) => !seen.has(`${e.repo}\n${e.dir}`))]
  }, [wantedBase, chainKey, repoKey, curUncertain, fileSelected])
  const dirState = useDirQueries(wantedFull, isDockerRepo)

  // 深链展开祖先链（目录选中即见其内容——展开态只增不减）
  useEffect(() => {
    if (!repoKey) return
    if (dir !== '' && !curUncertain) {
      setExpanded((prev) => {
        const next = new Set(prev)
        next.add(`${repoKey}\n`)
        for (const d of chain) next.add(`${repoKey}\n${d}`)
        return next
      })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- dir/repo 变化即展开
  }, [dir, repoKey, chainKey, curUncertain])

  // 深链滚动定位：目标行滚进可视区
  const chainSettled = !!repoKey && chain.every((d) => dirState.get(`${repoKey}\n${d}`)?.status === 'ok')
  useEffect(() => {
    if (!chainSettled) return
    const sel =
      focusPath !== null
        ? document.querySelector(`[data-testid="tree-leaf-${CSS.escape(focusPath)}"]`)
        : dir === ''
          ? document.querySelector(`[data-testid="tree-repo-${CSS.escape(repoKey)}"]`)
          : document.querySelector(`[data-testid="tree-node-${CSS.escape(dir)}"]`)
    sel?.scrollIntoView({ block: 'nearest' })
  }, [chainSettled, repoKey, dir, focusPath])

  // ---- 导航 ----
  const goTo = useCallback(
    (repo: string, d: string) => {
      navigate(buildTreeUrl(tab, repo, d === '' ? [] : d.split('/').filter(Boolean)))
    },
    [navigate, tab],
  )
  const selectFile = useCallback(
    (name: string | null) => {
      const segs = dir.split('/').filter(Boolean)
      navigate(buildTreeUrl(tab, repoKey, name ? [...segs, name] : segs))
    },
    [navigate, tab, repoKey, dir],
  )
  const goTab = useCallback(
    (t: DetailTab) => {
      navigate(buildTreeUrl(t, repoKey, pathSegs))
    },
    [navigate, repoKey, pathSegs],
  )

  // ---- 过滤（两个过滤词按作用域复位——QA-3）----
  const [repoFilter, setRepoFilter] = useState('')
  const [filter, setFilter] = useState('')
  const [filesOnly, setFilesOnly] = useState(false)
  useEffect(() => {
    setFilter('')
  }, [repoKey, dir])
  useEffect(() => {
    setRepoFilter('')
  }, [repoKey])

  // ---- 工具带状态 ----
  const [pkgFacets, setPkgFacets] = useState<Set<string>>(new Set())
  const [rclassFacets, setRclassFacets] = useState<Set<string>>(new Set())
  const [sortBy, setSortBy] = useState<TreeSort>('name')
  const [compacted, setCompacted] = useState(() => localStorage.getItem(COMPACT_KEY) === '1')
  const [favorites, setFavorites] = useState<Set<string>>(() => loadFavorites())
  const [favOnly, setFavOnly] = useState(false)
  useEffect(() => {
    localStorage.setItem(COMPACT_KEY, compacted ? '1' : '0')
  }, [compacted])
  useEffect(() => {
    localStorage.setItem(FAV_KEY, JSON.stringify(Array.from(favorites).sort()))
  }, [favorites])
  const toggleFavorite = useCallback((repo: string) => {
    setFavorites((prev) => {
      const next = new Set(prev)
      if (next.has(repo)) next.delete(repo)
      else next.add(repo)
      return next
    })
  }, [])

  // ---- 仓库列表（普通 user 403 → 深链合成顶层节点） ----
  const repoNodes = useMemo(() => {
    if (reposQuery.isSuccess) return repos
    if (reposQuery.forbidden && repoKey) {
      return [{ key: repoKey, description: '', type: '', packageType: '', url: '' }]
    }
    return []
  }, [reposQuery.isSuccess, reposQuery.forbidden, repos, repoKey])

  const pkgTypes = useMemo(
    () => Array.from(new Set(repoNodes.map((r) => r.packageType).filter((t) => t !== ''))).sort(),
    [repoNodes],
  )
  const filteredRepos = useMemo(() => {
    const f = repoFilter.trim().toLowerCase()
    const out = repoNodes
      .filter((r) => (f ? r.key.toLowerCase().includes(f) : true))
      .filter((r) => (pkgFacets.size > 0 ? pkgFacets.has(r.packageType) : true))
      .filter((r) => (rclassFacets.size > 0 ? rclassFacets.has(r.type) : true))
      .filter((r) => (favOnly ? favorites.has(r.key) : true))
    return [...out].sort((a, b) => compareRepos(a, b, sortBy))
  }, [repoNodes, repoFilter, pkgFacets, rclassFacets, favOnly, favorites, sortBy])

  // ---- B-3.2 初始态：首仓库自动选中 ----
  const [onboardSkipped, setOnboardSkipped] = useState(() => localStorage.getItem('bf-skip-onboarding') === '1')
  const hadSelectionRef = useRef(false)
  useEffect(() => {
    if (repoKey !== '') {
      hadSelectionRef.current = true
      return
    }
    if (hadSelectionRef.current) return
    if (legacyTarget !== null && location.pathname !== legacyTarget) return
    if (!reposQuery.isSuccess) return
    if (repoFilter.trim() !== '' || pkgFacets.size > 0 || rclassFacets.size > 0 || favOnly) return
    const first = filteredRepos[0]?.key
    if (!first) return
    navigate(buildTreeUrl(tab, first, []), { replace: true })
  }, [repoKey, legacyTarget, location.pathname, reposQuery.isSuccess, repoFilter, pkgFacets, rclassFacets, favOnly, filteredRepos, tab, navigate])

  // ---- 刷新 ----
  const refresh = useCallback(() => {
    clearTreeCache()
    invalidateRepoList()
    void meta.refetch()
  }, [clearTreeCache, invalidateRepoList, meta])

  // ---- 树行模型 ----
  const treeRows = useMemo(
    () => buildTreeRows(filteredRepos, dirState, expanded, repoKey, dir, focusPath, canSeeAdmin),
    [filteredRepos, dirState, expandedKey, repoKey, dir, focusPath, canSeeAdmin],
  )

  // ---- 操作 ----
  const [smuOpen, setSmuOpen] = useState(false)
  const [deployOpen, setDeployOpen] = useState(false)
  const [deleteError, setDeleteError] = useState<{ path: string; err: ApiError } | null>(null)
  const [download, setDownload] = useState<DownloadState | null>(null)
  const [menu, setMenu] = useState<{ x: number; y: number; target: MenuTarget } | null>(null)
  const [copyMove, setCopyMove] = useState<{ op: 'copy' | 'move'; nodes: ChildNode[] } | null>(null)

  const openMenuAt = useCallback((x: number, y: number, target: MenuTarget) => {
    const px = Math.min(x, window.innerWidth - 220)
    const py = Math.min(y, window.innerHeight - 220)
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
        if (match === false) toast.error(tt('下载完成，但 sha256 与服务端不一致（{v1}）', { v1: node.name }))
        else toast.success(tt('下载完成{v1}', { v1: match ? tt('：sha256 与服务端一致') : '' }))
      } catch (err) {
        setDownload(null)
        const apiErr = err instanceof ApiError ? err : new ApiError(0, String(err))
        toast.error(tt('下载失败（HTTP {v1}）：{v2}', { v1: apiErr.status, v2: apiErr.message }))
      }
    },
    [],
  )

  const doMkdir = async () => {
    const name = await prompt({
      title: tt('新建目录'),
      description: tt('在 {target} 下创建目录（尾斜杠 PUT，E-15 mkdir 语义）。', {
        target: dir === '' ? `${repoKey}/${tt('（根）')}` : `${repoKey}/${dir}/`,
      }),
      placeholder: tt('目录名'),
      inputTestid: 'tree-mkdir-input',
      mono: true,
      validate: (v) => validateNameSegment(v),
      confirmLabel: tt('创建'),
    })
    if (name === null || name === '') return
    const target = dir === '' ? `${name}/` : `${dir}/${name}/`
    try {
      await mkdir(repoKey, target)
      toast.success(tt('已创建目录 {target}', { target }))
      refresh()
      setExpanded((prev) => new Set(prev).add(`${repoKey}\n${name}`))
    } catch (err) {
      const apiErr = err instanceof ApiError ? err : new ApiError(0, String(err))
      toast.error(tt('创建目录失败（HTTP {v1}）：{v2}', { v1: apiErr.status, v2: apiErr.message }))
    }
  }

  const confirmDelete = useCallback(
    async (repo: string, node: ChildNode) => {
      const isCache = repoMeta?.rclass === 'remote'
      const ok = await confirm({
        title: node.folder ? tt('删除目录') : isCache ? tt('删除缓存') : tt('删除制品'),
        danger: true,
        confirmLabel: tt('删除'),
        description: tt('将永久删除 {repo}/{path}{v1}。制品不可变，删除没有撤销。{v2}', {
          repo,
          path: node.path,
          v1: node.folder ? tt('（目录及其全部内容）') : '',
          v2: isCache ? tt(' 这是 remote 缓存——删除后下次请求将重新回源。') : '',
        }),
      })
      if (!ok) return
      const wasSelected = repo === repoKey && (node.folder ? dir === node.path : focusFile === node.name)
      const backToParent = () => {
        const parent = node.path.includes('/') ? node.path.slice(0, node.path.lastIndexOf('/')) : ''
        goTo(repo, parent)
      }
      try {
        await deleteNode(repo, node.path, node.folder)
        toast.success(tt('已删除 {v1}', { v1: node.path }))
        setDeleteError(null)
        if (wasSelected) backToParent()
        refresh()
      } catch (err) {
        const apiErr = err instanceof ApiError ? err : new ApiError(0, String(err))
        if (apiErr.status === 404) {
          toast.success(tt('已删除 {v1}（此前已不存在——删除幂等）', { v1: node.path }))
          if (wasSelected) backToParent()
          refresh()
          return
        }
        setDeleteError({ path: `${repo}/${node.path}`, err: apiErr })
      }
    },
    [confirm, repoMeta, refresh, repoKey, dir, focusFile, goTo],
  )

  const deleteSelected = async (nodes: ChildNode[]) => {
    for (const n of nodes) {
      await confirmDelete(repoKey, n)
    }
  }

  const commands = repoMeta && packageType && !uploadable ? clientCommands(packageType, repoKey) : []

  // ---- 详情面板目标 ----
  const cur = repoKey ? dirState.get(`${repoKey}\n${dir}`) : undefined
  const selectedNode = fileSelected ? lastChild : undefined
  const detailTarget: DetailTarget | null = selectedNode
    ? { kind: 'node', repoKey, node: selectedNode }
    : repoKey
      ? dir === ''
        ? { kind: 'repo', repoKey }
        : {
            kind: 'node',
            repoKey,
            node: {
              name: dir.split('/').pop() ?? '',
              path: dir,
              folder: true,
              size: null,
              lastModified: '',
              sha256: '',
            },
          }
      : null

  // 页脚标语行（stats admin 门）
  const stats = useStorageStatsQuery(admin)
  const emptyInstance = reposQuery.isSuccess && repos.length === 0 && !repoKey && !onboardSkipped

  return (
    <div data-testid="tree-page" className="tree-page browser-page flex min-h-0 flex-1 flex-col gap-3">
      {/* 页头动作区 */}
      <div className="browser-toolbar flex flex-wrap items-center gap-2">
        <div className="browser-actions flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            data-testid="tree-setmeup"
            title={tt('客户端接入向导（按包类型生成接入命令与令牌）')}
            onClick={() => setSmuOpen(true)}
          >
            Set Me Up
          </Button>
          <Button
            size="sm"
            data-testid="tree-deploy"
            disabled={readOnly}
            title={readOnly ? tt('只读管理员不可写（服务端 403 兜底）') : tt('浏览器上传（local Generic / Maven 仓）')}
            onClick={() => setDeployOpen(true)}
          >
            {tt('⬆ 部署 Deploy')}
          </Button>
          {admin && (
            <ButtonAsChild variant="outline" size="sm">
              <Link to="/admin/repositories/local">{tt('管理仓库 →')}</Link>
            </ButtonAsChild>
          )}
        </div>
      </div>

      {emptyInstance ? (
        <section className="card section rounded-md border border-border bg-surface-1 p-4">
          <h3 className="mb-1 font-semibold">{tt('这个实例还没有仓库')}</h3>
          <p className="mb-2 text-dense text-muted-foreground">{tt('创建第一个仓库后，全部制品会以跨仓树的形式展示在这里。')}</p>
          <p className="flex items-center gap-2">
            {admin && (
              <ButtonAsChild size="sm">
                <Link to="/admin/repositories/new">{tt('创建仓库')}</Link>
              </ButtonAsChild>
            )}{' '}
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                localStorage.setItem('bf-skip-onboarding', '1')
                setOnboardSkipped(true)
              }}
            >
              {tt('跳过')}
            </Button>
          </p>
        </section>
      ) : (
        <>
          {readOnly && (
            <p className="browser-readonly-note rounded-md border border-border bg-surface-2 px-3 py-2 text-dense text-muted-foreground" data-testid="tree-readonly-note">
              {tt('当前会话是只读管理员（readonly_admin）：浏览面全量可见，上传/删除等写操作已禁用——服务端对写面一律 403 兜底。')}
            </p>
          )}

          <div className="tree-breadcrumb flex flex-wrap items-center gap-1" data-testid="tree-breadcrumb">
            {repoKey ? (
              <>
                <button type="button" className="crumb rounded-sm px-1.5 py-0.5 font-mono hover:bg-accent" onClick={() => goTo(repoKey, '')}>
                  {repoKey}
                </button>
                {dir !== '' &&
                  ancestorDirs(dir).map((d) => (
                    <span key={d} className="crumb-seg flex items-center gap-1">
                      <span className="sep text-muted-foreground" aria-hidden="true">/</span>
                      <button
                        type="button"
                        className={`crumb rounded-sm px-1.5 py-0.5 font-mono hover:bg-accent ${d === dir ? 'cur font-semibold' : ''}`}
                        onClick={() => goTo(repoKey, d)}
                      >
                        {d.split('/').pop()}
                      </button>
                    </span>
                  ))}
                <CopyButton value={dir === '' ? `${repoKey}/` : `${repoKey}/${dir}/`} label={tt('当前路径')} />
              </>
            ) : (
              <span className="text-dense text-muted-foreground">{tt('在左侧选择仓库开始浏览')}</span>
            )}
            <span className="spacer flex-1" />
            <Button variant="outline" size="sm" onClick={refresh} title={tt('重新加载当前视图')}>
              {tt('↻ 刷新')}
            </Button>
            {mkdirable && (
              <Button variant="outline" size="sm" data-testid="tree-mkdir" disabled={readOnly} title={readOnly ? tt('只读管理员不可写（服务端 403 兜底）') : undefined} onClick={() => void doMkdir()}>
                {tt('+ 目录')}
              </Button>
            )}
          </div>

          {meta.forbidden && !repoMeta && (
            <div className="warn-box rounded-md border border-warning/50 bg-warning/10 px-3 py-2 text-dense">
              {tt('仓库元数据为管理员视图（HTTP 403）——树按 generic 语义呈现；上传/删除权限由内容面按路径 ACL 判定，操作被拒时原因会在此原样呈现。')}
            </div>
          )}

          {commands.length > 0 && (
            <section className="card section rounded-md border border-border bg-surface-1 p-4" data-testid="tree-commands">
              <h3 className="mb-1 font-semibold">{tt('此协议不走浏览器上传')}</h3>
              <p className="mb-2 text-dense text-muted-foreground">
                {packageType === 'docker'
                  ? tt('docker 是 POST/PATCH/PUT 三步会话协议——经 docker 客户端推送；登录经 /v2/token（registry 面，与控制台会话无关）。')
                  : tt('{packageType} 的发布协议是 multipart/packument 形态——请用对应客户端发布。', { packageType: packageType ?? '' })}
                {tt('以下命令与仓库详情页同源：')}
              </p>
              {commands.map((c) => (
                <div className="cmd-block mb-2 rounded-md border border-border bg-surface-2 p-2" key={c.title}>
                  <header className="mb-1 flex items-center justify-between gap-2">
                    <span className="text-dense font-medium">{c.title}</span>
                    <CopyButton value={c.text} label={c.title} />
                  </header>
                  <pre lang="en" tabIndex={0} className="overflow-x-auto font-mono text-aux">
                    {c.text}
                  </pre>
                  {c.note && <div className="note mt-1 text-aux text-muted-foreground">{c.note}</div>}
                </div>
              ))}
            </section>
          )}

          {repoMeta && rclass === 'virtual' && (
            <div className="warn-box rounded-md border border-warning/50 bg-warning/10 px-3 py-2 text-dense">
              {tt('virtual 仓是聚合视图：浏览合并各成员；上传/删除不经 virtual（删除走成员仓本身——BinFlow 有意不兼容，RE-08）。')}
            </div>
          )}
          {repoMeta && rclass === 'remote' && (
            <div className="warn-box rounded-md border border-warning/50 bg-warning/10 px-3 py-2 text-dense" data-testid="tree-remote-note">
              {remoteBrowseOn
                ? tt('远端浏览已开启（listRemoteFolderItems）：树包含上游未缓存的目录与条目（按 metadata TTL 缓存枚举）；点击未缓存条目会回源拉取并落地缓存。')
                : tt('remote 仓浏览的是已缓存内容；首次访问的路径需经客户端拉取后才会出现在树上（可在仓库配置开启远端浏览 listRemoteFolderItems）。')}
            </div>
          )}

          {deleteError && (
            <div data-testid="delete-error" role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-dense">
              <div className="headline">
                {tt('删除')} <span className="font-mono" lang="en">{deleteError.path}</span> {tt('失败（HTTP')} {deleteError.err.status}{tt('）')}
              </div>
              <div className="raw font-mono text-aux" lang="en">{deleteError.err.message}</div>
              {deleteError.err.status === 403 && (
                <div>
                  {tt('当前会话没有对该路径的 delete 权限（read-only）。权限模型按 permission target 的路径 pattern 授予——')}
                  {admin ? (
                    <>
                      {tt('需要管理员在')}{' '}
                      <Link to="/admin/security/permissions" target="_blank" className="text-primary underline">{tt('权限 target')}</Link>
                      {tt('里给该路径加 delete 动作。')}
                    </>
                  ) : (
                    tt('请联系管理员为你的账号授予该路径的 delete 动作。')
                  )}
                </div>
              )}
              <div className="mt-2">
                <Button variant="outline" size="sm" onClick={() => setDeleteError(null)}>{tt('知道了')}</Button>
              </div>
            </div>
          )}

          <div className="tree-layout browser-layout flex min-h-0 flex-1 gap-3">
            <div className="w-[340px] shrink-0 rounded-md border border-border bg-surface-1">
              <TreePanel
                rows={treeRows}
                compacted={compacted}
                loading={reposQuery.isPending}
                error={
                  reposQuery.error instanceof ApiError && !(reposQuery.forbidden && repoKey)
                    ? reposQuery.error
                    : reposQuery.error && !(reposQuery.forbidden && repoKey)
                      ? { status: 0, message: String(reposQuery.error) }
                      : null
                }
                forbidden={reposQuery.forbidden && !repoKey}
                repoFilter={repoFilter}
                onRepoFilter={setRepoFilter}
                pkgTypes={pkgTypes}
                pkgFacets={pkgFacets}
                onTogglePkg={(t) =>
                  setPkgFacets((prev) => {
                    const next = new Set(prev)
                    if (next.has(t)) next.delete(t)
                    else next.add(t)
                    return next
                  })
                }
                onClearPkg={() => setPkgFacets(new Set())}
                rclassFacets={rclassFacets}
                onToggleRclass={(rc) =>
                  setRclassFacets((prev) => {
                    const next = new Set(prev)
                    if (next.has(rc)) next.delete(rc)
                    else next.add(rc)
                    return next
                  })
                }
                sortBy={sortBy}
                onSort={setSortBy}
                onCompacted={setCompacted}
                favCount={favorites.size}
                favOnly={favOnly}
                onFavOnly={setFavOnly}
                onNavigate={goTo}
                onToggle={(repo, d) =>
                  setExpanded((prev) => {
                    const next = new Set(prev)
                    const key = `${repo}\n${d}`
                    if (next.has(key)) next.delete(key)
                    else next.add(key)
                    return next
                  })
                }
                onMenu={openMenuAt}
                onOpenTrash={() => navigate('/admin/governance/trash')}
                onRetry={refresh}
                emptyLabel={repoFilter ? tt('没有匹配「{repoFilter}」的仓库（仅过滤已加载集）', { repoFilter }) : tt('（无仓库）')}
              />
            </div>

            {/* 右列：详情面板 + 当前层 children 表 */}
            <div className="tree-main flex min-w-0 flex-1 flex-col gap-3">
              {curUncertain ? (
                <div data-testid="skeleton" aria-hidden="true" className="flex flex-col gap-2 pt-2">
                  {Array.from({ length: 4 }, (_, i) => (
                    <div key={i} className="h-4 animate-pulse rounded-sm bg-surface-2" style={{ width: `${88 - i * 8}%` }} />
                  ))}
                </div>
              ) : detailTarget && repoKey ? (
                <DetailInspector
                  target={detailTarget}
                  download={download}
                  onDownload={(n, sha) => void doDownload(repoKey, n, sha)}
                  onClose={() => selectFile(null)}
                  canDelete={!readOnly && !isVirtual}
                  canWriteProps={!readOnly}
                  onDelete={(n) => void confirmDelete(repoKey, n)}
                  tab={tab}
                  onTabChange={goTab}
                  childrenNodes={
                    detailTarget.kind === 'node' && detailTarget.node.folder && cur?.status === 'ok' ? cur.nodes : undefined
                  }
                />
              ) : (
                <section className="card section node-detail rounded-md border border-border bg-surface-1 p-4">
                  <h3 className="mb-1 font-semibold">{tt('制品浏览器')}</h3>
                  <p className="mb-1 text-dense text-muted-foreground">
                    {tt('左侧是全部仓库的树：单击选中（此处联动详情），展开箭头（或 → 键）展开下一层——目录与文件都出现在树里。 选中路径与页签都会进入 URL——可以直接分享或收藏深链，打开时自动展开定位。')}
                  </p>
                  <p className="text-dense text-muted-foreground">
                    {tt('右键（或')} <kbd>Shift+F10</kbd>{tt('）打开操作菜单：复制路径 / 下载 / 删除 / 刷新。')}
                  </p>
                </section>
              )}

              {repoKey && (
                <ChildrenGrid
                  repoKey={repoKey}
                  nodes={cur?.status === 'ok' ? cur.nodes : []}
                  total={cur?.status === 'ok' ? cur.nodes.length : 0}
                  loading={!cur || cur.status === 'loading' || curUncertain}
                  isDockerRepo={isDockerRepo}
                  focusFile={focusFile}
                  readOnly={readOnly}
                  filter={filter}
                  onFilter={setFilter}
                  filesOnly={filesOnly}
                  onFilesOnly={setFilesOnly}
                  onNavigateDir={(p) => goTo(repoKey, p)}
                  onSelectFile={selectFile}
                  onMenu={openMenuAt}
                  onCopyMove={(op, nodes) => setCopyMove({ op, nodes })}
                  onDeleteSelected={(nodes) => void deleteSelected(nodes)}
                  remoteDegraded={cur?.status === 'ok' ? cur.remoteDegraded : undefined}
                  emptyState={
                    isVirtual ? (
                      <EmptyState
                        message={dir === '' ? tt('虚拟仓库：暂无聚合内容') : tt('此路径在成员仓库中无内容')}
                        hint={
                          dir === ''
                            ? virtualMembers.length
                              ? tt('成员 {v1} 当前均无内容——成员仓库有制品后会聚合到这里（若成员已被删除，请在仓库管理中更新成员列表）。', { v1: virtualMembers.join(tt('、')) })
                              : tt('未配置成员仓库——在仓库管理中配置成员后，成员内容会聚合到这里。')
                            : tt('虚拟仓库按成员并集浏览，此路径下没有任何成员的内容。')
                        }
                        testid="tree-empty-virtual"
                      />
                    ) : (
                      <EmptyState
                        message={tt('此目录为空')}
                        hint={
                          uploadable
                            ? tt('上传第一个制品，或创建子目录组织布局。')
                            : rclass === 'remote'
                              ? remoteBrowseOn
                                ? tt('远程仓库：缓存与远端枚举在此层均无条目。')
                                : tt('远程仓库：仅展示已缓存的制品（浏览不回源）。')
                              : tt('此仓库尚无内容。')
                        }
                        testid="tree-empty-dir"
                        action={
                          uploadable && !readOnly ? (
                            <Button size="sm" onClick={() => setDeployOpen(true)}>{tt('上传第一个制品')}</Button>
                          ) : undefined
                        }
                      />
                    )
                  }
                />
              )}

              {/* 四态收口（children 表的 forbidden/404/空态——AG Grid 面之外） */}
              {repoKey && cur?.status === 'forbidden' && (
                <EmptyState
                  message={tt('无权限浏览此目录')}
                  hint={tt('内容面按路径 ACL 判定（{v1}）。可回到有权限的层级，或用搜索定位制品。', { v1: cur.error.message })}
                  action={
                    <ButtonAsChild variant="outline" size="sm">
                      <Link to="/search">{tt('去搜索')}</Link>
                    </ButtonAsChild>
                  }
                />
              )}
              {repoKey && cur?.status === 'error' && cur.error.status === 404 && (
                <EmptyState
                  message={tt('路径不存在')}
                  hint={tt('节点可能已被删除，或链接里的路径有误。')}
                  action={
                    <Button variant="outline" size="sm" onClick={() => goTo(repoKey, '')}>
                      {tt('← 回仓库根')}
                    </Button>
                  }
                />
              )}
            </div>
          </div>

          {/* 页脚标语行 */}
          {stats.isSuccess && stats.data && (
            <p className="browser-footer text-aux text-muted-foreground">
              {tt('已服务')} {stats.data.blobs.toLocaleString()} {tt('个 blob · 逻辑容量')} {formatBytes(stats.data.logical_bytes)}
            </p>
          )}
        </>
      )}

      {menu && (
        <TreeContextMenu
          menu={menu}
          readOnly={readOnly}
          canSeeAdmin={admin}
          repoRclass={repoMeta?.rclass}
          targetVirtual={repoNodes.some((r) => r.key === menu.target.repoKey && r.type === 'virtual')}
          targetFavorite={favorites.has(menu.target.repoKey)}
          onToggleFavorite={toggleFavorite}
          onClose={() => setMenu(null)}
          onCopyPath={(value) => {
            void navigator.clipboard.writeText(value).then(
              () => toast.success(tt('已复制 {value}', { value })),
              () => toast.error(tt('剪贴板不可用')),
            )
          }}
          onDownload={(repo, node) => void doDownload(repo, node, node.sha256)}
          onDelete={(repo, node) => void confirmDelete(repo, node)}
          onCopyMove={(op, node) => setCopyMove({ op, nodes: [node] })}
          onRefresh={(repo) => {
            setMenu(null)
            if (repo === repoKey) refresh()
            else clearTreeCache()
          }}
          onOpenAdmin={(repo) => {
            setMenu(null)
            navigate(`/admin/repositories/${encodeURIComponent(repo)}`)
          }}
        />
      )}

      {smuOpen && (
        <LegacyDialogHost>
          <SetMeUpDialog preselectedRepo={repoKey || undefined} onClose={() => setSmuOpen(false)} />
        </LegacyDialogHost>
      )}
      {deployOpen && (
        <LegacyDialogHost>
          <DeployDialog
            preselectedRepo={repoKey || undefined}
            preselectedDir={dir || undefined}
            onClose={() => setDeployOpen(false)}
            onUploaded={refresh}
          />
        </LegacyDialogHost>
      )}
      {copyMove && repoKey && (
        <CopyMoveDialog
          op={copyMove.op}
          repoKey={repoKey}
          nodes={copyMove.nodes}
          onClose={() => setCopyMove(null)}
          onDone={refresh}
        />
      )}
    </div>
  )
}

// ---- 右键上下文菜单（tree-context-* 族 + copy/move 解锁项）----

interface MenuItem2 {
  id: string
  label: string
  disabled?: boolean
  title?: string
  run: () => void
}

function TreeContextMenu({
  menu,
  readOnly,
  canSeeAdmin,
  repoRclass,
  targetVirtual,
  targetFavorite,
  onToggleFavorite,
  onClose,
  onCopyPath,
  onDownload,
  onDelete,
  onCopyMove,
  onRefresh,
  onOpenAdmin,
}: {
  menu: { x: number; y: number; target: MenuTarget }
  readOnly: boolean
  canSeeAdmin: boolean
  repoRclass?: string
  targetVirtual: boolean
  targetFavorite: boolean
  onToggleFavorite: (repo: string) => void
  onClose: () => void
  onCopyPath: (value: string) => void
  onDownload: (repo: string, node: ChildNode) => void
  onDelete: (repo: string, node: ChildNode) => void
  onCopyMove: (op: 'copy' | 'move', node: ChildNode) => void
  onRefresh: (repo: string) => void
  onOpenAdmin: (repo: string) => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const first = ref.current?.querySelector<HTMLButtonElement>('button')
    first?.focus()
    const onDocKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
      }
    }
    const onDocClick = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose()
    }
    document.addEventListener('keydown', onDocKey)
    document.addEventListener('mousedown', onDocClick)
    return () => {
      document.removeEventListener('keydown', onDocKey)
      document.removeEventListener('mousedown', onDocClick)
    }
  }, [onClose])

  const t = menu.target
  const readonlyTitle = tt('只读管理员不可删（服务端 403 兜底）')
  const virtualDeleteTitle = tt('virtual 仓不经手删除（RE-08，服务端 405）——请到持有该制品的成员仓删除')
  const deleteBlocked = readOnly || targetVirtual
  const deleteTitle = readOnly ? readonlyTitle : targetVirtual ? virtualDeleteTitle : undefined
  const items: MenuItem2[] =
    t.kind === 'repo'
      ? [
          { id: 'copy-repo-path', label: tt('复制仓库路径'), run: () => { onCopyPath(`${t.repoKey}/`); onClose() } },
          {
            id: 'favorite',
            label: targetFavorite ? tt('取消收藏（My Favorites）') : tt('加入收藏（My Favorites）'),
            title: tt('收藏是前端态（localStorage 持久）；树头 My Favorites 可只看收藏仓库'),
            run: () => { onToggleFavorite(t.repoKey); onClose() },
          },
          { id: 'refresh', label: tt('刷新'), run: () => onRefresh(t.repoKey) },
          ...(canSeeAdmin ? [{ id: 'open-admin', label: tt('在仓库管理中打开'), run: () => onOpenAdmin(t.repoKey) }] : []),
        ]
      : t.node.folder
        ? [
            { id: 'copy-path', label: tt('复制路径'), run: () => { onCopyPath(`${t.repoKey}/${t.node.path}/`); onClose() } },
            // P2 解锁：copy/move（api/copy|move——pro 域，community 403 如实呈现）
            { id: 'copy-to', label: tt('复制到…'), disabled: readOnly, title: tt('服务端复制（api/copy，pro 域）——目录递归整树'), run: () => { onClose(); onCopyMove('copy', t.node) } },
            { id: 'move-to', label: tt('移动到…'), disabled: readOnly, title: tt('服务端移动（api/move，pro 域）——源删除 + 目标落地'), run: () => { onClose(); onCopyMove('move', t.node) } },
            { id: 'delete', label: tt('删除'), disabled: deleteBlocked, title: deleteTitle, run: () => { onClose(); onDelete(t.repoKey, t.node) } },
            { id: 'refresh', label: tt('刷新'), run: () => onRefresh(t.repoKey) },
          ]
        : [
            { id: 'copy-path', label: tt('复制路径'), run: () => { onCopyPath(`${t.repoKey}/${t.node.path}`); onClose() } },
            { id: 'copy-to', label: tt('复制到…'), disabled: readOnly, title: tt('服务端复制（api/copy，pro 域）'), run: () => { onClose(); onCopyMove('copy', t.node) } },
            { id: 'move-to', label: tt('移动到…'), disabled: readOnly, title: tt('服务端移动（api/move，pro 域）'), run: () => { onClose(); onCopyMove('move', t.node) } },
            { id: 'download', label: tt('下载'), run: () => { onClose(); onDownload(t.repoKey, t.node) } },
            { id: 'delete', label: repoRclass === 'remote' ? tt('删除缓存') : tt('删除'), disabled: deleteBlocked, title: deleteTitle, run: () => { onClose(); onDelete(t.repoKey, t.node) } },
          ]

  return (
    <div
      ref={ref}
      role="menu"
      aria-label={tt('操作菜单')}
      data-testid="tree-context-menu"
      className="fixed z-[90] min-w-[200px] rounded-md border border-border bg-popover p-1 shadow-flat"
      style={{ left: menu.x, top: menu.y }}
    >
      {items.map((item) => (
        <button
          key={item.id}
          type="button"
          role="menuitem"
          className={`context-item block w-full rounded-sm px-2.5 py-1.5 text-left text-dense hover:bg-accent ${item.id === 'delete' && !item.disabled ? 'danger text-destructive' : ''}`}
          data-testid={`tree-context-${item.id}`}
          disabled={item.disabled}
          title={item.title}
          tabIndex={-1}
          onClick={item.run}
        >
          {item.label}
        </button>
      ))}
    </div>
  )
}
